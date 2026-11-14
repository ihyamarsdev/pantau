# 0072. Inventaris authorized_keys dan Audit Akun Host

Menambahkan **Audit Akun & Kunci SSH** pada tab Security: inventaris read-only seluruh akun lokal host beserta berkas `authorized_keys`/`authorized_keys2` miliknya, status kata sandi dari `/etc/shadow`, hak `sudo`, keanggotaan grup, dan inventaris kunci SSH lintas fleet yang dikelompokkan menurut sidik jari. Melengkapi [ADR 0035](0035-ssh-key-library.md) (library kunci, yang hanya knew kunci milik Pantau sendiri) dengan pandangan tentang kunci yang benar-benar diotorisasi di host.

## Konteks dan Masalah

1. **Library kunci hanya mencerminkan satu sisi.** [ADR 0035](0035-ssh-key-library.md) menyimpan kunci privat yang didaftarkan administrator di Pantau. Kunci publiknya memang diketahui Pantau, tetapi isi `authorized_keys` di host tidak pernah diperiksa: kunci lama yang dibiarkan, kunci yang tidak terdaftar di library, dan kunci tim yang sudah tidak dipakai seluruhnya tidak terlihat.
2. **Risiko akun tidak terlihat.** Akun dengan UID 0 tambahan, akun dengan kata sandi kosong, akun terkunci yang masih memiliki shell login, dan akun non-root dengan `NOPASSWD: ALL` adalah temuan standar audit akun, tetapi semuanya berada di luar model *agentless* (tanpa instalasi) Pantau.
3. **Cakupan yang tidak jujur menghasilkan rasa aman palsu.** `/etc/shadow` dan `/etc/sudoers` hanya dapat dibaca sebagai root atau dengan `sudo -n`. Pemindai yang tidak membedakan "tidak ada temuan" dari "tidak dapat dibaca" akan melaporkan host bersih padahal separuh datanya tidak pernah dilihat (prinsip *honesty rule*, [ADR 0036](0036-agentless-security-scan.md) butir 5).
4. **Visibilitas lintas host.** Satu kunci yang diotorisasi pada puluhan host adalah jejaki risiko yang tidak terlihat bila hanya ada inventaris per host.
5. **macOS berbeda secara struktural.** `/etc/passwd`, `/etc/shadow`, dan sudoers pada Darwin tidak sama dengan Linux; modul harus menurun ke `partial`/`unsupported` dengan catatan, bukan gagal atau diam-diam melaporkan nol temuan.

## Keputusan Arsitektur

1. **Satu skrip polyglot read-only (`AccountAuditCmd` di `internal/inspector/accounts.go`).** Perintah dijalankan lewat SSH non-interaktif dengan `PANTAU_TRY_SUDO=1` hanya bila probe `sudo -n` diizinkan (backoff 24 jam bersama, sama seperti [ADR 0036](0036-agentless-security-scan.md)). Skrip tidak memasang apa pun dan tidak menulis apa pun di host. Hasilnya berupa seksi berlabel `===PASSWD===`, `===GROUP===`, `===AKFILE===`, `===SHADOW===`, `===SUDO===`, `===LASTLOG===`, `===SSHD===`:
   - **Akun login-capable**: `/etc/passwd` disaring sehingga shell bukan `nologin`/`false`, dengan home directory; UID 0 selain `root` tetap selalu ditampilkan;
   - **Berkas kunci**: `~/.ssh/authorized_keys` dan `~/.ssh/authorized_keys2` setiap akun; dibaca biasa, dan bila tidak terbaca dicoba dengan `sudo -n cat`; status `readable`/`unreadable` dicatat per berkas;
   - **Status kata sandi**: field kedua `/etc/shadow` dipetakan menjadi `set` (hash), `empty` (kosong), atau `locked` (`!`/`*`); bila `/etc/shadow` tidak terbaca tanpa root/sudo, hasilnya `unknown`;
   - **Hak sudo**: aturan non-komentar dari `/etc/sudoers` dan `/etc/sudoers.d/*`; bila tidak terbaca tanpa root/sudo, hasilnya `unknown`;
   - **Grup**: `/etc/group` untuk keanggotaan `sudo`/`wheel`/`admin`/`docker`;
   - **Login terakhir**: `lastlog` bila tersedia (opsional; `never`/`hidden`/`unknown`);
   - **`AuthorizedKeysFile`**: nilai efektif dari `sshd -T` bila ada, atau baris konfigurasi dari `sshd_config`; nilai di luar `.ssh/authorized_keys` dicatat sebagai catatan cakupan.
   - Pada Darwin skrip berhenti setelah `===DARWIN===` dan modul berstatus `unsupported` dengan catatan eksplisit.

2. **Parsing di Go dengan pustaka standar (`golang.org/x/crypto/ssh`).** Setiap baris `authorized_keys` diparse dengan `ssh.ParseAuthorizedKey`, yang menghasilkan kunci publik, komentar, dan daftar opsi. Modul menyimpan **hanya metadata kunci publik**: sidik jari SHA-256 (`ssh.FingerprintSHA256`), tipe kunci, jumlah bit modulus RSA (lewat `ssh.CryptoPublicKey`), komentar, dan opsi (`command=`, `from=`, `no-pty`, dst.). Baris yang gagal diparse dihitung sebagai `invalid_lines` dan **tidak** menghasilkan kunci semu. Kunci dengan `command=` ditandai `restricted`; kunci tanpa `from=` ditandai `source_restricted: false`. Tidak ada materi kunci privat yang pernah dibaca, disimpan, atau dikembalikan.

3. **Cross-reference dengan SSH Key Library.** Sidik jari setiap kunci yang ditemukan dicocokkan dengan sidik jari kunci publik pada tabel `ssh_keys`. Hasilnya `library_key: <nama>` bila cocok, selain itu `unknown`. Kunci privat tidak pernah dibaca oleh modul ini.

4. **Aturan temuan dan severity.** `ssh-dss` → `critical`; RSA < 2048 bit → `high`; RSA 2048 → tanpa temuan; kunci `unknown` (tidak ada di library) → `medium`; satu kunci yang diotorisasi untuk banyak akun pada satu host → `info`; akun UID 0 selain `root` → `critical`; akun login-capable dengan kata sandi kosong → `critical`; `NOPASSWD: ALL` untuk non-root → `high`; `ALL` biasa → `medium`; akun layanan (UID < 1000) terkunci yang masih punya shell login → `low`; kunci tanpa `from=` → `info`.

5. **Kejujuran cakupan.** Bagian yang tidak terbaca (shadow, sudoers, berkas kunci milik root) tidak pernah dilaporkan sebagai "bersih": status field menjadi `unknown`, temuan terkait tidak dibuat, dan `notes` menjelaskan bagian mana yang tidak terlihat. Status modul hanya `ok`, `partial`, `unsupported`, atau `error`; `partial` berarti ada bagian yang tidak dapat dinilai.

6. **Penyimpanan (`account_audits`).** Tabel baru di `pantau.db`:
   ```sql
   CREATE TABLE IF NOT EXISTS account_audits (
       host_id INTEGER PRIMARY KEY REFERENCES hosts(id) ON DELETE CASCADE,
       scanned_at DATETIME NOT NULL,
       status TEXT,
       summary_json TEXT,
       users_json TEXT,
       keys_json TEXT,
       findings_json TEXT,
       notes_json TEXT,
       error TEXT
   );
   ```
   Satu baris per host (hasil audit terbaru), dibuat lewat `CREATE TABLE IF NOT EXISTS` di `migrate()` dan bersifat aditif. Tabel ini berisi **telemetri turunan** (dapat diproduksi ulang dengan audit ulang) sehingga **tidak disertakan dalam snapshot terenkripsi**, sama seperti `security_scans`.

7. **REST API.**
   - `GET /api/hosts/{id}/security/accounts`: hasil audit tersimpan, atau `{"audited": false}` bila belum pernah diaudit. Menyertakan header `X-Scan-Cooldown-Remaining` bila jeda masih aktif.
   - `POST /api/hosts/{id}/security/accounts/scan`: menjalankan audit sinkron (batas 90 detik) dengan jeda 60 detik (HTTP 429) dan kunci konkurensi per host (HTTP 409), seperti pemindaian keamanan.
   - `GET /api/security/keys`: inventaris kunci lintas fleet. Setiap fingerprint dikelompokkan menjadi `{fingerprint, type, bits, library_key?, unknown, weak, shared, hosts:[{host_id, name, user, options}]}`, dengan `total`, `unknown_count`, `weak_count`, dan `hosts_not_audited`. Filter `?unknown=1` dan `?weak=1` menyaring daftar. **Hanya metadata kunci publik** yang dikembalikan; tidak ada kunci privat maupun isi berkas `authorized_keys`.

8. **UI (modul `static/app/accounts/`, bundel `/app/accounts.js`, namespace `AC`, i18n EN/ID sendiri).** Kartu **Accounts & SSH keys** di bawah kartu Firewall pada tab Security berisi tombol audit, chip ringkasan (akun, kunci, kunci unik, kunci unknown, kunci lemah, baris tak terbaca, UID 0 tambahan), tabel akun (shell, grup, hak sudo, status kata sandi, jumlah kunci, login terakhir), tabel kunci (sidik jari pendek + tombol salin, tipe/bit, komentar, nama library atau badge **Unknown**, badge opsi), daftar temuan menurut severity, dan catatan cakupan. Modal **Fleet keys** (terjangkau dari kartu dan dari header area Security) menampilkan inventaris fleet dengan filter **All / Unknown only / Weak only** dan jumlah host yang belum diaudit. `index.html` hanya menyisipkan kartu, satu tag `<script>`, dan satu tombol header.

9. **Batas yang disengaja.** Modul ini **tidak** mengubah semantik chip keamanan dashboard dan **tidak** menambah jenis Desired Rule baru; konsekuensi audit ditampilkan di dalam kartu. Modul juga tidak memverifikasi keaslian private key (yang mustahil secara kriptografis dari public key) dan tidak menghapus atau mengubah berkas apa pun di host.

## Konsekuensi

- Administrator memperoleh daftar siapa saja yang dapat masuk ke host dan kunci mana yang sah, tanpa memasang agen dan tanpa membuka terminal per host.
- Kunci yang tidak terdaftar di library menjadi terlihat, sehingga *blast radius* kunci yang dibagikan ke banyak host dapat dinilai di satu modal.
- Pemeriksaan tetap sepenuhnya read-only dan *airgapped*: tidak ada panggilan keluar dari server Pantau dan tidak ada pemasangan di host.
- Kerentanan data `/etc/shadow` dan `/etc/sudoers` tetap menjadi batas cakupan: tanpa root atau `sudo -n`, modul melaporkan `unknown`/`partial`, bukan "aman".
- Biaya satu audit per host adalah satu perintah SSH; untuk fleet besar, jadwal audit berkala belum tersedia (saat ini dipicu manual atau dari kartu).
