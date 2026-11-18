# 0084. Secrets Vault Universal dan Sinkronisasi Environment

Menyediakan *Secrets Vault* terpusat di Pantau untuk menyimpan, memversi, memutar (*rotate*), dan menyuntikkan rahasia (token, kata sandi, kunci API, dan nilai sensitif lainnya) ke berbagai target — berkas `.env` Host, Kubernetes `Secret`, serta variabel lingkungan Terraform, Ansible, dan Docker. Rahasia dienkripsi *at rest*, disensor secara deterministik dari seluruh log (termasuk *Activity Console* [ADR-0030](0030-in-app-activity-console-and-live-event-stream.md)), dan tidak pernah masuk ke konteks AI Agent ([ADR-0073](0073-in-process-ai-agent.md)) sebagai nilai mentah.

## Konteks dan Masalah

Sebelum arsitektur ini diterapkan, pengelolaan rahasia di Pantau memiliki keterbatasan signifikan:

1. **Rahasia Tersebar dan Tidak Terpusat**: Token, kata sandi, dan kunci API tersebar di berkas `.env` pada tiap Host, variabel playbook Ansible, variabel Terraform, dan form manual. Tidak ada inventaris tunggal, tidak ada kepemilikan yang jelas, dan tidak ada cara untuk mengetahui rahasia mana yang digunakan di mana.
2. **Kebocoran Nilai Melalui Log dan Stream Aktivitas**: Nilai rahasia dapat tercetak pada output Ansible, log eksekusi Terraform, stream SSH runner, dan *Activity Console* [ADR-0030](0030-in-app-activity-console-and-live-event-stream.md). Tanpa penyensoran terpusat, satu baris log yang terdokumentasi dapat membocorkan kredensial produksi.
3. **Tanpa Riwayat Versi, Rollback, atau Rotasi**: Mengubah rahasia bersifat destruktif — nilai lama hilang tanpa jejak, tidak ada audit trail atas siapa mengubah apa dan kapan, serta tidak ada penanda kedaluwarsa (*expiry*) atau jadwal rotasi yang dapat diperingatkan.
4. **Sinkronisasi ke Target Beragam dan Manual**: Injeksi ke `.env` Host (lewat SFTP), `Secret` Kubernetes, dan variabel lingkungan proses (Terraform `TF_VAR_*`, *extra vars* Ansible, `-e` Docker) dilakukan satu per satu secara manual. Tidak ada verifikasi integritas, tidak ada deteksi *drift* antara nilai tersimpan dan nilai aktual di target, dan tidak ada *post-sync hook* untuk memuat ulang service.
5. **Risiko Eksfiltrasi oleh AI Agent**: AI Agent [ADR-0073](0073-in-process-ai-agent.md) memanggil *Agent Tool* untuk menyelidiki dan memperbaiki Host. Jika tool mengembalikan nilai rahasia mentah, nilai itu masuk ke riwayat percakapan dan dikirim ke penyedia LLM pihak ketiga — satu panggilan saja dapat membocorkan seluruh vault.
6. **Tanpa Workspace Pengelolaan**: Tidak ada antarmuka untuk melihat daftar rahasia secara aman (*masked by default*), membuka nilai secara eksplisit, menyunting *template*, memantau *drift*, atau melihat riwayat audit.

## Keputusan Arsitektur

1. **Package `internal/vault` dan Skema SQLite dengan Enkripsi Envelope**:
   - Membuat package mandiri `internal/vault` yang **hanya mengimpor pustaka standar** (tanpa `pantau/internal/*`) untuk menghindari siklus impor. Package ini mengekspor tipe `Scope`, `SecretMeta`, `Secret`, `Version`, `Template`, `SyncTarget`, `AuditEntry`, dan `TemplateIssue`.
   - Properti nilai mentah `Secret.Value` tidak diberi tag JSON (`json:"-"` pada struct tersimpan), sehingga plaintext **tidak pernah diserialisasi** ke respons API. Satu-satunya jalur plaintext adalah endpoint *reveal* eksplisit.
   - Menambahkan lima tabel melalui migrasi idempoten `CREATE TABLE IF NOT EXISTS` di `internal/store/db_migrate.go`:
     ```sql
     vault_secrets(id, key UNIQUE COLLATE NOCASE, scope, scope_ref, ciphertext BLOB,
                   current_version INT, expires_at DATETIME NULL, rotation_days INT DEFAULT 0,
                   created_at, updated_at)
     vault_versions(id, secret_id, version, ciphertext BLOB, created_at, UNIQUE(secret_id,version))
     vault_templates(id, name UNIQUE, body TEXT, created_at, updated_at)
     vault_targets(id, name, kind, template_id, target_ref, path, namespace, post_sync_hook,
                   last_sync_sha, last_sync_at, drift_status DEFAULT 'unknown', created_at)
     vault_audit(id, ts, action, detail)
     ```
   - Enkripsi mengikuti pola manajemen kunci ADR-0035 ([ADR-0035](0035-ssh-key-library.md)): kunci rahasia disimpan di basis data dan tidak pernah diserialisasi. Alih-alih frasa sandi, vault memakai **kunci master 32 byte** yang disimpan di tabel `settings` dengan kunci `vault_master_key` (Base64), dibuat otomatis pada pemakaian pertama oleh `DB.VaultMasterKey()`. Karena `SnapshotPayload` menyalin seluruh peta `settings`, kunci master ikut terbawa di dalam *System Snapshot* terenkripsi — tetapi **rekaman vault itu sendiri belum termasuk** dalam ekspor/impor snapshot pada iterasi ini (lihat Konsekuensi).
   - Format *envelope*: magic `PANTAU_VAULT_V1\n` + nonce 12 byte + ciphertext AES-256-GCM. `Cipher.Encrypt`/`Cipher.Decrypt` menolak kunci bukan 32 byte dan menghasilkan `ErrCorrupt` untuk blob yang rusak. Ini sejajar dengan mekanisme enkripsi snapshot yang sudah ada, dengan perbedaan sumber kunci (kunci tersimpan, bukan derivasi passphrase).
   - Setiap mutasi rahasia dan setiap *reveal* ditulis ke `vault_audit` (aksi `secret.create`, `secret.update`, `secret.reveal`, `secret.rollback`, `secret.delete`, `target.sync`).

2. **Scoping, Riwayat Versi, Rotasi, dan Interpolasi Template**:
   - Setiap rahasia memiliki `Scope` (`global`, `group`, `host`, `cluster`) dan `scope_ref` pengenal target cakupannya. `ValidScope` memvalidasi nilai yang diterima API.
   - Pembaruan nilai menyimpan ciphertext lama sebagai baris baru di `vault_versions` sebelum menimpa `current_version`. **Rollback menciptakan versi baru** (bukan menghapus versi setelahnya), sehingga riwayat tetap lengkap dan dapat diaudit.
   - Metadata rotasi `expires_at` dan `rotation_days` menghasilkan penanda `needs_rotation`; daftar rahasia dan strip status UI menampilkan peringatan kedaluwarsa.
   - *Template* menyimpan teks dengan placeholder `{{ vault.KEY }}` (spasi di sekitar nama diizinkan; `KEY` berpola `[A-Za-z_][A-Za-z0-9_]*`). `Render` mengganti placeholder dari peta nilai dan **menolak kunci yang tidak dikenal** dengan pesan menyebut nama kunci; `Placeholders` mengembalikan kunci unik sesuai urutan kemunculan; `ValidateTemplate` menghasilkan `TemplateIssue` per baris untuk autocomplete dan validasi UI. Endpoint render mengembalikan nilai yang sudah dirender dengan `[REDACTED]`, bukan plaintext.

3. **Redactor Thread-Safe yang Terhubung ke Logger**:
   - `vault.Redactor` membungkus `RWMutex` dan daftar nilai aktif. `SetValues` mengganti seluruh daftar (mengabaikan string kosong), `AddValue`/`Clear` mengelola daftar, dan `Redact(s)` mengganti **setiap** nilai yang cocok dengan `[REDACTED]`, diurutkan **terpanjang lebih dulu** agar nilai yang merupakan substring dari nilai lain tetap tersensor.
   - `Redactor.Writer(w)` mengembalikan `io.Writer` yang menyensor tiap `Write`, sehingga aliran output dapat dibungkus tanpa mengubah pemanggil.
   - `internal/logger` **tidak mengimpor** `internal/vault`. Sebagai gantinya, ditambahkan hook `SetSecretRedactor(fn func(string) string)` dan `ClearSecretRedactor()`; `RedactString(s)` memanggil hook lebih dulu sebelum pemeriksaan `PRIVATE KEY` yang sudah ada.
   - Server memasang hook ke `*vault.Redactor` paket-level saat `NewServer` dan setelah setiap mutasi rahasia (`refreshVaultRedactor`: `ListVaultSecretValues()` → `SetValues` → `logger.SetSecretRedactor`). Dengan demikian seluruh nilai aktif tersensor secara seragam di log Ansible, Terraform, SSH runner, dan stream [ADR-0030](0030-in-app-activity-console-and-live-event-stream.md).

4. **Sinkronisasi Multi-Target**:
   - **Host `.env`**: `sshrunner.SFTP()` menulis berkas dengan mode `0600`. Setelah tulis, SHA-256 berkas di target diverifikasi terhadap nilai yang dikirim; hasilnya disimpan sebagai `last_sync_sha` dan `last_sync_at`, dengan `drift_status` (`unknown`/`in-sync`/`drift`/`error`). *Post-sync hook* opsional menjalankan aksi [ADR-0041](0041-systemd-service-actions-and-error-diagnosis.md) (`systemd:<unit>:<restart|reload>`) atau restart/recreate container Docker. Target yang tidak terjangkau menghasilkan `502` dan menandai `drift_status = "error"`.
   - **Kubernetes**: target `kind = "kube"` membangun manifest `v1/Secret` dari template yang sudah dirender lalu memanggil `inspector.KubeApply(hostID, yaml)` dengan **id klaster negatif** untuk klaster eksternal.
   - **Injeksi Ephemeral (tanpa plaintext di disk)**: `vault.InjectEnv(base, secrets, prefix)` mengembalikan peta **baru** — salinan `base` ditambah `prefix + strings.ToUpper(k) → v`. Nilai hanya masuk ke memori: Terraform `cfg.Env` (`TF_VAR_*` dan tanpa prefiks), *extra vars* Ansible (objek JSON yang digabung ke `RunSpec.ExtraVars`/`AdhocSpec.ExtraVars`), dan slice env Docker (`-e`) yang diteruskan ke `cmd.Env`. Tidak ada berkas rahasia plaintext yang ditulis; satu-satunya berkas sementara adalah `extra.json` Ansible yang sudah ada di workspace 0600 dan bersifat ephemeral.

5. **Blind Injection untuk AI Agent** ([ADR-0073](0073-in-process-ai-agent.md)):
   - `list_vault_keys` — *Agent Tool* baca **tanpa gate**, menerima argumen `{scope?}` dan mengembalikan TSV metadata (key, scope, version, expires_at, needs_rotation). **Nilai tidak pernah dikembalikan**, sehingga model hanya dapat merujuk rahasia lewat placeholder `{{ vault.KEY }}`.
   - `trigger_env_sync` — *Agent Tool* ubah dengan **gate 1** (persetujuan operator): memanggil `s.agentGateAs(r, "vault", "trigger_env_sync", summary, 1)` lalu `loopJSON` `POST /api/vault/targets/<id>/sync`. Hasil hanya berisi status, SHA-256, dan drift — **tanpa nilai**.
   - Keduanya terdaftar di `agentToolDomain` sebagai `domainAutomation`, memiliki entri `toolResultHints`, dan ditambahkan ke daftar tool di `agent_soul.md`. Dengan ini LLM dapat menyusun template dan memicu sinkronisasi tanpa satu pun rahasia mentah memasuki konteksnya.

6. **Workspace UI Vanilla, Aman, dan Auditabel**:
   - Modul `internal/web/static/app/vault/00-vault.js` (IIFE `window.Vault`) dan `00-vault.css` disajikan sebagai bundle lokal `/app/vault.js` dan `/app/vault.css` (tanpa aset eksternal, sesuai [ADR-0013](0013-airgapped-self-contained-web-assets.md)). Tombol navigasi `navItemSecrets` dan bagian `sectionSecrets` ditambahkan ke side panel; `switchSideNav('secrets')` memuat workspace.
   - Menggunakan **token desain [ADR-0039](0039-modern-visual-design-system-and-icon-sprite.md)** secara eksklusif, ikon sprite `<svg class="icon"><use href="#i-..."/></svg>`, tanpa emoji. Modal bergaya Workspace Modal ([ADR-0007](0007-standardized-workspace-modal-layout.md)).
   - Daftar rahasia **masked by default** (`••••••••`); tombol Reveal memanggil endpoint reveal dan dicatat di audit. Editor template memiliki autocomplete dan validasi `{{ vault.* }}` langsung dengan pratinjau `[REDACTED]`; daftar target menampilkan badge drift; strip status menampilkan jumlah kunci, jumlah yang kedaluwarsa, dan jumlah target. Kunci i18n `nav_secrets` dan `vault_*` ditambahkan ke `en` dan `id`.

7. **Permukaan REST API**:
   - Seluruh endpoint `/api/vault/*` berada di bawah `s.authMiddleware` dan menggunakan `writeJSON`. Endpoint mencakup CRUD rahasia (`GET/POST /secrets`, `PUT /secrets/{id}`, `DELETE /secrets/{id}`), reveal eksplisit (`GET /secrets/{id}/reveal`), versi dan rollback (`GET /secrets/{id}/versions`, `POST /secrets/{id}/rollback/{version}`), template (CRUD + `GET /templates/{id}/render`), target (CRUD + `POST /targets/{id}/sync`), `GET /audit`, dan `GET /status`. Duplikat kunci menghasilkan `409`; placeholder tidak dikenal dan payload tidak valid menghasilkan `400`.

## Konsekuensi

- **Rahasia Terlindungi *At Rest* dan Di Log**: Nilai dienkripsi AES-256-GCM di basis data, tidak pernah muncul di respons API atau stream log (Ansible, Terraform, SSH runner, *Activity Console*), dan tersensor otomatis lewat hook logger yang memakai daftar nilai aktif terbaru.
- **Rollback yang Dapat Diaudit**: Rollback menambah versi baru alih-alih menimpa sejarah, dan setiap mutasi/reveal/sinkronisasi tercatat di `vault_audit`, sehingga perubahan rahasia dapat ditelusuri kembali secara lengkap.
- **Agent Tidak Dapat Mengekspor Nilai**: Tool agent hanya mengembalikan metadata dan status; tidak ada jalur bagi LLM untuk membaca plaintext, sehingga nilai rahasia tidak pernah dikirim ke penyedia LLM.
- **Deteksi Drift dan Rotasi**: SHA-256 pasca-sinkronisasi dan penanda `expires_at`/`rotation_days` memberi visibilitas cepat terhadap target yang menyimpang atau rahasia yang perlu diputar.
- **Konsekuensi — Kunci Master Berada di Basis Data yang Sama**: Kunci master `vault_master_key` disimpan di tabel `settings` pada berkas basis data yang sama dengan rahasia. File basis data dilindungi izin `0600` (lihat `internal/store/db.go`), tetapi **siapa pun yang mendapat akses baca ke berkas basis data effectively mendapat akses ke seluruh vault**. Ini adalah tradeoff yang disengaja demi operasi non-interaktif tanpa passphrase.
- **Kunci Master Ikut Snapshot, Rekaman Vault Belum (Keterbatasan Diketahui)**: `SnapshotPayload` (`internal/store/snapshot.go`) menyalin seluruh peta `settings`, sehingga `vault_master_key` ikut terenkripsi di dalam *System Snapshot* ([ADR-0035](0035-ssh-key-library.md)) — passphrase snapshot menjadi pelindung tidak langsung bagi kunci itu. Namun kelima tabel `vault_secrets`, `vault_versions`, `vault_templates`, `vault_targets`, dan `vault_audit` **belum menjadi bagian dari ekspor/impor snapshot pada iterasi ini**; dengan demikian ini **bukan cadangan penuh vault**. Konsekuensinya, memulihkan snapshot ke basis data yang sudah memuat rekaman vault akan menimpa kunci master dengan kunci dari snapshot dan membuat ciphertext yang tersimpan tidak lagi dapat didekripsi (rekaman menjadi tidak terbaca). Pemulihan semacam itu harus dilakukan dengan hati-hati: hapus rekaman vault lama sebelum restore, atau anggap kunci master lama hilang dan isi ulang rahasia. Dukungan ekspor/impor rekaman vault secara penuh menjadi pekerjaan lanjutan.
- **Arsitektur Tetap Sederhana**: Tidak ada layanan, dependensi, atau aset eksternal baru — paket `internal/vault` murni pustaka standar dan UI memakai bundle lokal, menjaga sifat *single binary* dan airgapped Pantau.
