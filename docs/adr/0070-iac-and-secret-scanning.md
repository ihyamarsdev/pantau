# ADR-0070: Pemindaian IaC dan Rahasia pada Proyek Terraform, Ansible, dan Repository Git Hasil Clone

## Konteks dan Masalah

Pantau sudah memegang banyak sumber konfigurasi di disk server-nya sendiri:

- repository Git hasil clone (`data/repos`, lihat ADR-0067),
- workspace Terraform (ADR-0049 s.d. ADR-0061),
- proyek dan playbook Ansible (ADR-0051 s.d. ADR-0063).

Semua sumber itu dibaca oleh server, tetapi tidak pernah diperiksa. Akibatnya, rahasia yang ikut ter-commit (kunci AWS, token GitHub, `aws_secret_access_key`, kata sandi di inventory Ansible) dan konfigurasi infrastruktur yang berbahaya (security group terbuka ke `0.0.0.0/0`, bucket S3 publik, database tanpa enkripsi, `validate_certs: no`) baru ketahuan setelah insiden, kalau tidak sama sekali.

Di sisi lain, ada beberapa batasan yang melekat pada Pantau yang harus dihormati:

1. **Pantau adalah binary tunggal dan harus tetap bisa operate di jaringan terisolasi (air-gapped).** Solusi yang berbasis tool eksternal (gitleaks, trivy, checkov, semgrep) atau yang memanggil registry/servis daring tidak dapat dipakai: binary tambahan tidak tersedia di server Pantau, dan mengunduhnya melanggar prinsip air-gapped.
2. **`go.mod` tidak punya parser HCL.** Menambah dependensi HCL untuk mem-parse `.tf` berarti menambah dependensi ke binary tunggal Pantau, yang tidak желаati.
3. **Prinsip read-only default Pantau.** Pemindaian tidak boleh mengubah berkas sumber, tidak boleh memasang apa pun pada host yang dipindai, dan tidak boleh mengubah apa pun pada server selain menyimpan hasil pindaian.
4. **Prinsip coverage jujur.** Kalau pindaian tidak bisa membaca seluruh sumber, hasilnya harus dilaporkan sebagai *partial* atau *not assessed*, tidak boleh ditampilkan sebagai "bersih".
5. **Rahasia tidak boleh bocor.** Nilai rahasia yang ditemukan tidak boleh masuk ke log, API, atau basis data dalam bentuk mentah.

## Keputusan Arsitektur

### 1. Pemindai bawaan di dalam binary (`internal/iacscan`)

Pindaian dilakukan oleh paket Go baru `internal/iacscan` yang hanya memakai pustaka standar. Tidak ada binary eksternal, tidak ada jaringan, tidak ada dependensi baru.

`ScanDir(root string, opts Options) Result` adalah fungsi murni terhadap direktori:

- menelusuri berkas secara rekursif, melewati `.git`, `.terraform`, `node_modules`, `vendor`;
- **tidak pernah mengikuti symlink**, sehingga pindaian tidak dapat keluar dari root;
- melewati berkas lebih besar dari 1 MiB (batas default, dapat diatur lewat `Options.MaxFileSize`);
- melewati berkas biner;
- membatasi jumlah temuan (default 1000) dan menandai `truncated` bila batas terlampaui;
- melaporkan status `assessed`, `partial` (ada berkas/symlink yang dilewati), atau `not_assessed` (direktori tidak terbaca).

Struktur berkas:

| Berkas | Isi |
| --- | --- |
| `internal/iacscan/iacscan.go` | tipe `Finding`/`Summary`/`Result`/`Options`, walker, paginasi temuan |
| `internal/iacscan/files.go` | dispatch jenis berkas dan komentar pengegen `pantau:ignore` |
| `internal/iacscan/secrets.go` | aturan rahasia, pendeteksian placeholder, redaksi |
| `internal/iacscan/terraform.go` | aturan Terraform (parser blok berbasis kurung kurawal) |
| `internal/iacscan/ansible.go` | aturan Ansible (per baris dan per task) |
| `internal/iacscan/docker.go` | aturan Dockerfile dan docker compose |

### 2. Aturan

**Rahasia** (`category: secret`): blok kunci privat PEM; AWS access key id (`AKIA`/`ASIA`); atribusi `aws_secret_access_key`; token GitHub (`ghp_`/`gho_`/`ghs_`/`github_pat_`); token GitLab (`glpat-`); token Slack (`xox[baprs]-`); Google API key (`AIza…`); Stripe live key (`sk_live_`); JWT; atribusi `password`/`secret`/`token`/`api_key`/`access_key`/`credential` berkode keras dengan nilai ber-entropi cukup; berkas `.env` yang ter-commit; `terraform.tfstate`/`*.tfstate` yang ter-commit; berkas `*.tfvars` dengan nilai rahasia; nama berkas kunci privat (`id_rsa`, `*.pem`, `*.key`).

**Terraform** (`category: terraform`): security group/firewall dengan ingress `0.0.0.0/0` atau `::/0` (makin kritis bila porta 22/3389/porta basis data terbuka); ACL bucket S3 `public-read`/`public-read-write`; public access block S3 dinonaktifkan; `aws_ebs_volume`/`aws_db_instance` tanpa enkripsi; `publicly_accessible = true` pada basis data; kredensial provider yang ditulis literal; `skip_tls_verify`/`insecure = true`; kebijakan IAM dengan `Action "*"` sekaligus `Resource "*"`; bucket S3 tanpa versioning (rendah); versi provider/modul yang tidak dipin (rendah).

**Ansible** (`category: ansible`): `validate_certs: no/false`; perintah `curl|sh` atau `wget|bash`; `mode: '0777'`/`'0666'`; `state: latest` (rendah); `ignore_errors` pada task `become` + `shell`; task yang menulis kata sandi tanpa `no_log` (rendah); kata sandi plaintext di inventory (`ansible_ssh_pass`/`ansible_become_pass`); `StrictHostKeyChecking=no`.

**Dockerfile / compose** (`category: dockerfile`): `FROM x:latest`; tanpa `USER`; `ADD` dari URL; `curl|sh`; rahasia di `ENV`/`ARG`; `privileged: true`; mount `/var/run/docker.sock`.

### 3. Redaksi

Setiap nilai rahasia pada `snippet` dan `detail` disamarkan: empat karakter pertama dipertahankan, sisanya menjadi `****` (`AKIA****`). Nilai yang tidak cocok dengan pola token apa pun tetap disamarkan melalui redaksi berbasis nilai yang ditemukan pada baris tersebut. Hasil pindaian tidak pernah memuat nilai rahasia mentah.

### 4. Placeholder, Vault, dan penegen

Tidak dilaporkan sebagai rahasia: nilai bermotif variabel (`${var}`, `{{ jinja }}`, `var.x`), placeholder (`changeme`, `example`, `your_*`, `xxx`, `TODO`, dan sejenisnya), serta blob `ansible-vault` (`$ANSIBLE_VAULT;`). Baris mana pun dapat ditebus dengan komentar baris `# pantau:ignore`.

### 5. Penyimpanan

Tabel baru `iac_scans(source_kind, source_id, scanned_at, summary_json, findings_json, status, files_scanned, truncated, root, notes_json)` dengan kunci utama komposit `(source_kind, source_id)`; pindaian terakhir per sumber menimpa yang sebelumnya (upsert). Tabel ini berisi **hasil pindaian, bukan konfigurasi**, sehingga tidak ikut dalam System Snapshot (setara `security_scans` pada ADR-0036). Akses basis data berada di `internal/store/iac_scans.go`.

### 6. Endpoint

| Metode | Path | Fungsi |
| --- | --- | --- |
| `GET` | `/api/iac-scan` | ringkasan pindaian terakhir seluruh sumber (fleet) + status gate |
| `GET` | `/api/iac-scan/{kind}/{id}` | hasil pindaian terakhir satu sumber (`kind` = `repo`\|`terraform`\|`ansible`) |
| `POST` | `/api/iac-scan/{kind}/{id}` | jalankan pindaian sekarang dan simpan hasilnya |

Resolusi akar sumber memakai penyimpanan yang sudah ada, tanpa menduplikasi logika path: repository memakai direktori clone yang tercatat, workspace dan playbook inline dimaterialisasi ke direktori sementara, sumber Git di-*clone* dangkal ke direktori sementara. Pindaian pada sumber Git hanya membaca *work dir* yang benar-benar dipakai run (dengan proteksi path-traversal), sedangkan proyek Ansible memindai seluruh repository karena playbook dapat menarik role, `vars`, dan `group_vars` dari mana saja.

### 7. Gate opsional (default mati)

Pengaturan `iac_gate_enabled` (default `0`) mengaktifkan penjaga:

- **Apply Terraform** (`POST /api/terraform/runs/{id}/apply`) dan **run Ansible** (`POST /api/ansible/playbooks/{id}/run`) memindai sumber lebih dulu. Bila masih ada temuan `critical` atau `high`, permintaan ditolak dengan **HTTP 409** dan badan `{error, findings_summary}`. Permintaan dapat melewati gate dengan `ignore_iac_gate: true`; tombol "Run anyway"/"Apply anyway" pada dialog di UI mengirim parameter tersebut.
- Gate mati secara bawaan, sehingga perilaku bawaan Pantau tidak berubah sama sekali.
- Sumber yang tidak dapat dinilai (`not_assessed`) **tidak** memblokir: pindaian yang tidak lengkap tidak boleh menghentikan run yang sah.
- Hasil pindaian disimpan setiap kali gate berjalan, sehingga UI selalu menampilkan hasil terbaru.

### 8. UI

Bundel baru `internal/web/static/app/iacscan/` (JS + CSS) dengan i18n EN/ID sendiri:

- tombol "Security scan" dan chip hasil pada tiap repository hasil clone, tiap workspace Terraform, dan tiap proyek Ansible;
- modal hasil: jumlah per tingkat keparahan, filter kategori/tingkat/teks, tabel `file:line` + judul + remediasi (rahasia sudah tersamar);
- dialog gate-blocked dengan aksi "Run anyway"/"Apply anyway".

## Konsekuensi

**Positif**

- Rahasia dan konfigurasi berbahaya pada sumber yang sudah dipegang Pantau terdeteksi tanpa menambah dependensi, tanpa binary eksternal, dan tanpa keluar dari server (tetap air-gapped).
- Pola baru cukup untuk ditambahkan: satu berkas per keluarga aturan di `internal/iacscan`, tanpa abstraksi berlapis.
- Gate opsional memberi tegakan nyata pada apply dan run tanpa mengubah perilaku bawaan.
- Nilai rahasia tidak pernah masuk ke log, API, maupun basis data.

**Negatif / batasan**

- **Parser Terraform dan Ansible bersifat pragmatic.** Aturan berbasis teks dan kurung kurawal, tanpa parser HCL/YAML penuh. Temuan adalah petunjuk untuk manusia, bukan vonis; ada konfigurasi valid yang tidak dikenali, dan beberapa blok HCL kompleks dapat salah dibaca.
- **Cakupan tidak pernah "bersih" secara mutlak.** Berkas lebih besar dari 1 MiB, symlink, dan berkas biner dilewati; hasilnya ditandai `partial`. Symlink sengaja tidak pernah diikuti, sehingga isi di luar root tidak pernah dipindai.
- **Auto-apply Terraform terjadwal dan scheduler Ansible tidak diberi gate.** Keduanya menjalankan run di luar jalur permintaan HTTP, sehingga tidak ada tempat untuk parameter `ignore_iac_gate`; gate hanya berlaku pada apply dan run yang dipicu manual lewat API/UI. Ini limitasi yang diketahui, bukan hal yang tersembunyi.
- **Pindaian sumber Git memerlukan kredensial.** Bila token atau kunci SSH sudah tidak valid, sumber tidak dapat di-*clone* dan hasilnya `not_assessed` (tidak memblokir).
- **Gate hanya memblokir temuan `critical` dan `high`.** Temuan `medium` dan `low` hanya dilaporkan.
- Karena hasil pindaian bukan konfigurasi, tabel `iac_scans` tidak ikut dalam System Snapshot; pindaian harus dijalankan ulang setelah pemulihan dari cadangan.
