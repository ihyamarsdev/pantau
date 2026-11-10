# Panduan Administrator & Playbook Operasional Pantau

[**English**](user-guide.md) • [**Bahasa Indonesia**](user-guide.id.md)

Selamat datang di **Panduan Administrator Pantau**. Dokumen ini menyajikan panduan operasional (*operational playbook*) yang dirancang khusus bagi administrator sistem, insinyur DevOps, dan tim reliabilitas infrastruktur (SRE) yang mengelola kluster server Linux menggunakan Pantau.

Pantau beroperasi **100% agentless** melalui protokol standar SSH (`port 22`). Sistem target sama sekali **tidak memerlukan instalasi daemon atau agen telemetri latar belakang**. Seluruh evaluasi status, pengaliran berkas, dan penarikan diagnostik dieksekusi secara langsung melalui utilitas standar POSIX.

---

## 📑 Daftar Isi

1. [Arsitektur & Batas Kepercayaan (Trust Boundaries)](#1-arsitektur--batas-kepercayaan-trust-boundaries)
2. [Fase 1: Onboarding & Manajemen Inventaris Host](#2-fase-1-onboarding--manajemen-inventaris-host)
   - [Menambahkan Host & Key Provisioning](#menambahkan-host--key-provisioning)
   - [Pengelompokan Host Group & Manual Host Order](#pengelompokan-host-group--manual-host-order)
   - [Host macOS — Prasyarat dan Metrik yang Diukur](#host-macos--prasyarat-dan-metrik-yang-diukur)
3. [Fase 2: Baseline Desired State & Proteksi Sistem](#3-fase-2-baseline-desired-state--proteksi-sistem)
   - [Membuat 1-Click Desired State Baseline](#membuat-1-click-desired-state-baseline)
   - [Konfigurasi Backup Freshness & Protected Path](#konfigurasi-backup-freshness--protected-path)
4. [Fase 3: Observabilitas, Analisis Drift & Respons Insiden](#4-fase-3-observabilitas-analisis-drift--respons-insiden)
   - [Siklus Inspeksi, Inspeksi Massal & Kesegaran Data](#siklus-inspeksi-inspeksi-massal-inspect-all--kesegaran-data)
   - [Resource Metrics, Process Telemetry & Observabilitas Perangkat Keras](#resource-metrics-process-telemetry--observabilitas-perangkat-keras)
   - [Inventarisasi Paket & Audit Higienitas Software](#inventarisasi-paket--audit-higienitas-software)
   - [Pemindaian Keamanan Host & Pemeriksaan Hardening](#pemindaian-keamanan-host--pemeriksaan-hardening)
   - [Mendeteksi Drift & Root Cause Excerpt](#mendeteksi-drift--root-cause-excerpt)
   - [Investigasi Interaktif via Terminal Dock & Terminal Preset](#investigasi-interaktif-via-terminal-dock--terminal-preset)
   - [In-App Activity Console & Live Telemetry Stream](#in-app-activity-console--live-telemetry-stream)
   - [Diagnostik Jaringan Agentless & Analisis Hop](#diagnostik-jaringan-agentless--analisis-hop)
   - [Evaluasi Kelayakan Server dengan Lifecycle Assessment](#evaluasi-kelayakan-server-dengan-lifecycle-assessment)
   - [Pengelolaan Klaster Kubernetes Eksternal (EKS, GKE, AKS & Kubeconfig Mandiri)](#pengelolaan-klaster-kubernetes-eksternal-eks-gke-aks--kubeconfig-mandiri)
   - [Pengelolaan Cloudflare via REST API v4](#pengelolaan-cloudflare-via-rest-api-v4)
   - [Ansible Playbooks (Gaya Semaphore, Agentless)](#ansible-playbooks-gaya-semaphore-agentless)
   - [Membaca Run: Log, Task Tree, Recap & Diff](#membaca-run-log-task-tree-recap--diff)
   - [Survey (Pertanyaan Launch Bertipe)](#survey-pertanyaan-launch-bertipe)
   - [Run Ad-Hoc (Satu Modul, Tanpa Playbook)](#run-ad-hoc-satu-modul-tanpa-playbook)
   - [Explorer Host & Group (Ping dan Facts)](#explorer-host--group-ping-dan-facts)
   - [Git Sync (Memverifikasi Playbook Git)](#git-sync-memverifikasi-playbook-git)
   - [Git Repository Editor (Edit, Commit & Push)](#git-repository-editor-edit-commit--push)
   - [Manajemen Terraform (Gaya Terraform Cloud, CLI Lokal)](#manajemen-terraform-gaya-terraform-cloud-cli-lokal)
   - [Evaluasi Komparatif Multi-Host & Matriks Penilaian](#evaluasi-komparatif-multi-host--matriks-penilaian)
5. [Fase 4: Operasi Berkas & Aliran Data Lintas-Host](#5-fase-4-operasi-berkas--aliran-data-lintas-host)
   - [Cross-Host Transfer (Pipa Memori Tanpa Disk Lokal)](#cross-host-transfer-pipa-memori-tanpa-disk-lokal)
   - [Folder Streaming Download Langsung ke Browser](#folder-streaming-download-langsung-ke-browser)
   - [Pin Berkas dengan Catatan Operasional & Global Bookmarks](#pin-berkas-dengan-catatan-operasional--global-bookmarks)
6. [Fase 5: Pengerasan Keamanan, Snapshot & Pemulihan Bencana](#6-fase-5-pengerasan-keamanan-snapshot--pemulihan-bencana)
   - [Two-Factor Authentication (2FA) & 2FA Bypass Flag](#two-factor-authentication-2fa--2fa-bypass-flag)
   - [System Snapshot Terenkripsi & Disaster Recovery GitHub](#system-snapshot-terenkripsi--disaster-recovery-github)
7. [Fase 6: Pemeliharaan Sistem & Pembaruan (Update Checker & Self-Update)](#7-fase-6-pemeliharaan-sistem--pembaruan-update-checker--self-update)
   - [Update Checker & Siklus Deteksi Rilis](#update-checker--siklus-deteksi-rilis)
   - [Proses One-Click Self-Update & Verifikasi Integritas SHA-256](#proses-one-click-self-update--verifikasi-integritas-sha-256)
   - [Diferensiasi Lingkungan Docker vs Standalone Binary](#diferensiasi-lingkungan-docker-vs-standalone-binary)
   - [Kesiapan Lingkungan Airgapped](#kesiapan-lingkungan-airgapped)

---

## 1. Arsitektur & Batas Kepercayaan (Trust Boundaries)

Pantau mengeksekusi perintah jarak jauh dan membaca telemetri server melalui koneksi SSH standar.

- **Control Plane**: Berupa binary tunggal Go yang berjalan di server manajemen atau mesin bastion administrator, didukung basis data tersemat SQLite (`pantau.db`) dengan mode WAL (Write-Ahead Logging).
- **Remote Host**: Setiap mesin fisik atau virtual (VPS) Linux (Ubuntu, Debian, RHEL, CentOS 6–9) yang dapat dijangkau melalui jaringan SSH.
- **Batas Kepercayaan**: Pantau membuat pasangan kunci privat/publik RSA 4096-bit saat pertama kali dijalankan sebagai kunci global bawaan, dan juga mendukung kunci khusus yang didaftarkan melalui **SSH Key Library** atau kunci kustom per-host. Host target memberikan izin akses dengan menempatkan kunci publik yang sesuai ke dalam berkas `~/.ssh/authorized_keys` milik pengguna manajemen (`root` atau pengguna khusus dengan privilese `sudo` tanpa kata sandi).

```
+-------------------+             SSH Port 22 (Agentless)             +-----------------------+
|  Pantau Control   | ==============================================> |   Remote Linux Host   |
|  Plane (Go + DB)  | < - - - - - - - - - - - - - - - - - - - - - - - |  (POSIX df, ps, logs) |
+-------------------+          Actual State & Telemetri               +-----------------------+
```

---

## 2. Fase 1: Onboarding & Manajemen Inventaris Host

### Menambahkan Host & Key Provisioning

Untuk menghubungkan **Host** baru ke dalam Pantau:

1. Klik tombol **Add Host** pada dashboard utama untuk memunculkan **Action Dialog**.
2. Masukkan parameter koneksi:
   - **Label**: Nama penanda yang unik (contoh: `prod-db-primary`).
   - **IP / Hostname**: Alamat IP publik/privat atau hostname yang dapat di-resolve.
   - **SSH Port**: Port standar `22` atau port kustom daemon SSH target.
   - **SSH User**: Akun `root` atau pengguna dengan hak akses sudo.
3. Pilih metode autentikasi:
   - **SSH Key Library (Dropdown)**: Pilih kunci privat terdaftar dari library (dikelola melalui **Settings** → **SSH Keys**). Sangat direkomendasikan untuk mengelola klaster atau lingkungan server yang berbagi pasangan kunci yang sama.
   - **Custom Private Key**: Tempelkan (*paste*) teks kunci privat OpenSSH spesifik host secara langsung jika tidak menggunakan library.
   - **Global SSH Key**: Kosongkan pilihan untuk menggunakan kunci SSH universal bawaan Pantau.
   - **Key Provisioning (One-Time Password)**: Jika menghubungkan server baru tanpa kunci yang terpasang sebelumnya, masukkan kata sandi target sekali saja pada kolom provisioning. Pantau akan menghubungkan via SSH, menyisipkan kunci publik yang sesuai (kunci publik terderivasi dari Library Key yang dipilih, atau kunci publik global bawaan) ke `~/.ssh/authorized_keys` secara idempoten, lalu langsung menghapus kata sandi dari memori RAM.
4. Klik **Save & Test Connection**. Pantau akan melakukan uji koneksi (*handshake*) untuk memvalidasi akses.

### Mengimpor Host dari SSH Config

Untuk menambahkan banyak host sekaligus, klik **Import** di sebelah **Add Host**:

1. Sediakan konfigurasi: tempel isi `~/.ssh/config`, klik **Choose File**, atau klik **Read Server ~/.ssh/config** untuk membaca file milik user yang menjalankan Pantau (tidak tersedia di dalam Docker kecuali file tersebut di-mount). Lalu klik **Parse**.
2. Periksa pratinjau. Setiap alias `Host` yang konkret menjadi satu host dengan `HostName` (default: alias), `User` (default: `root`), dan `Port` (default: `22`). Blok wildcard (`Host *`) tidak pernah diimpor tetapi menyediakan nilai default, persis seperti OpenSSH. Entri yang sudah ada di Pantau, atau mengulang alamat/port/user dari entri sebelumnya di file yang sama, ditampilkan redup dan tidak bisa dipilih.
3. Klik **Import Selected**. Host dibuat tanpa grup dan memakai **Global SSH Key**; `IdentityFile` pada config tidak ikut diimpor (host tersebut diberi petunjuk 🔑, jadi tetapkan Library Key sesudahnya bila perlu).
4. Pada form **Set Group** berikutnya, isi Host Group untuk tiap host (grup yang sudah ada disarankan) lalu klik **Save Groups**. Kosongkan kolom agar host tetap tanpa grup; **Skip** menutup form dan grup tetap bisa diatur nanti lewat dialog edit host.

> [!NOTE]
> Direktif `Include` dan `Match` tidak diikuti atau dievaluasi. Pantau terhubung langsung, sehingga pengaturan `ProxyJump`/`ProxyCommand` diabaikan.

### Hirarki Resolusi Kunci & Prioritas Autentikasi

Saat melakukan autentikasi ke host untuk inspeksi latar belakang, sesi terminal interaktif, operasi file SFTP, tunneling port, atau diagnostik jaringan, Pantau menentukan kunci privat berdasarkan urutan deterministik yang ketat:

1. **Library Key (`key_id > 0`)**: Jika host dikonfigurasi dengan kunci terdaftar dari SSH Key Library, Pantau memuat dan mengautentikasi dengan kunci privat tersebut. Jika `key_id` yang dirujuk tidak ditemukan di library, **Pantau langsung membatalkan koneksi dengan error eksplisit**—sistem tidak pernah melakukan fallback senyap ke kunci lain.
2. **Custom Key (`custom_key`)**: Jika tidak ada library key yang dipilih dan host memiliki kunci privat kustom tersimpan, Pantau menggunakan custom key tersebut. Saat host diubah untuk menggunakan Library Key (`key_id > 0`), nilai `custom_key` sebelumnya otomatis dibersihkan dari basis data.
3. **Global SSH Key (`settings.ssh_private_key`)**: Jika tidak ada library key maupun custom key (`key_id == 0` dan `custom_key` kosong), Pantau menggunakan kunci privat global bawaan sistem.

> [!TIP]
> Aksi **Inject Key** pada Workspace Modal host mengikuti aturan yang sama: jika host terhubung ke Library Key, Pantau menyuntikkan kunci publik dari library key tersebut; jika tidak, kunci publik global yang disuntikkan.

> [!NOTE]
> Untuk server Linux lawas (*legacy*) seperti CentOS 5/6, Debian 7, atau OpenSSH 4.x/5.x, Pantau secara otomatis mengaktifkan fallback cipher dan algoritma host key warisan (`aes128-cbc`, `3des-cbc`, `diffie-hellman-group1-sha1`, `ssh-dss`, `ssh-rsa`) saat negosiasi SSH. Pantau menerapkan verifikasi host key TOFU (Trust-On-First-Use) yang otomatis mencatat fingerprint host pada koneksi awal sekaligus melindungi sesi berikutnya dari manipulasi Machine-in-the-Middle (MITM).

### Pengelompokan Host Group & Manual Host Order

Ketika jumlah server bertambah banyak:
- **Host Group**: Kelompokkan host berdasarkan fungsi sistem atau lingkungan (contoh: `Database Cluster`, `Edge Proxies`, `Testing Environment`). Grup akan dipisahkan secara visual baik pada **View Mode** Grid maupun List.
- **Manual Host Order**: Atur urutan prioritas server utama agar selalu tampil di posisi teratas dashboard melalui pengurutan manual.
- **Host Note & Global Note**: Gunakan **Host Note** untuk mencatat catatan operasional spesifik per server (misal: `"Master PostgreSQL; node replikasi ada di db-02"`). Gunakan **Global Note** untuk pengumuman sistem bersama atau catatan serah-terima giliran tugas antar-administrator.

### Host macOS — Prasyarat dan Metrik yang Diukur

Pantau menyediakan observabilitas kelas satu (*first-class support*) berbasis agentless untuk **Host macOS (Darwin)** (seperti server workstation Apple silicon Mac mini, Mac Studio, dan Mac Pro) melalui koneksi OpenSSH standar.

#### Deteksi Host & Cakupan Kompatibilitas
- **Deteksi Otomatis**: Pantau mendeteksi sistem Darwin secara dinamis saat runtime melalui `uname -s`. Apabila terdeteksi `Darwin`, Pantau secara otomatis mengeksekusi skrip telemetri dan diagnostik khusus macOS dengan tetap menjaga alur inspeksi Linux berjalan 100% identik tanpa perubahan.
- **Cakupan Kompatibilitas**: Beroperasi mengandalkan perkakas standar POSIX; diverifikasi langsung pada **macOS 26 Tahoe berbasis Apple silicon** (divalidasi menggunakan data riil mesin Apple M4 Pro dan M3 Ultra). Host Mac berbasis prosesor Intel dan versi macOS lama beroperasi via fallback POSIX standar tanpa jaminan fitur kernel spesifik.

#### Prasyarat & Hak Akses
1. **Remote Login**: Aktifkan akses SSH remote melalui menu **System Settings > General > Sharing > Remote Login**.
2. **Kredensial SSH**: Hubungkan menggunakan akun admin atau pengguna standar macOS dengan autentikasi kunci SSH (dikelola via SSH Key Library atau kunci spesifik host).
3. **Hak Akses Sudo**: Inspeksi telemetri berkala, diagnostik jaringan, dan pemindaian keamanan berjalan murni **tanpa memerlukan sudo (zero sudo)**. Hak sudo tanpa kata sandi (`sudo -n`) **hanya** dibutuhkan jika Anda memanfaatkan fitur **Elevated File Access** pada File Manager (`sudo -n -u <user> /usr/libexec/sftp-server`).
4. **Penyelarasan PATH Lingkungan**: Sesi SSH non-interaktif di macOS secara bawaan hanya memiliki `PATH=/usr/bin:/bin:/usr/sbin:/sbin`. Pantau secara otomatis menyisipkan prelude PATH:
   ```bash
   case "$(uname -s)" in Darwin) PATH="$PATH:/usr/local/bin:/opt/homebrew/bin:/opt/homebrew/sbin:/Applications/Docker.app/Contents/Resources/bin"; export PATH;; esac;
   ```
   Hal ini menjamin biner Homebrew dan Docker Desktop dapat ditemukan tanpa perlu konfigurasi profil shell manual.

#### Metrik yang Dikumpulkan & Definisi Pengukuran
- **Penggunaan RAM**: Kapasitas total memori fisik diambil via `sysctl -n hw.memsize`. Nilai RAM terpakai secara ketat mengikuti rumusan resmi **Activity Monitor "Memory Used"**:
  $$\text{Memory Used} = (\text{Anonymous pages} - \text{Pages purgeable} + \text{Pages wired down} + \text{Pages occupied by compressor}) \times \text{pagesize}$$
  Ukuran halaman memori (*page size*) diekstrak secara dinamis dari baris pertama `vm_stat` (16.384 byte pada Apple silicon).
- **Memori Swap**: Diekstrak langsung dari perintah `sysctl vm.swapusage` (`total`, `used`, `free`) dengan toleransi penanganan satuan MB/GB/KB.
- **Kapasitas Disk**: Pada arsitektur macOS modern, volume root `/` adalah sealed system volume yang terkunci (~12 GB). Pantau membaca APFS shared container melalui `df -lPk /System/Volumes/Data` (fallback `/`) dan menghitung:
  $$\text{Used} = \text{Total} - \text{Available}$$
  Langkah ini memastikan persentase dan kapasitas disk yang dilaporkan merefleksikan penyimpanan data pengguna dan aplikasi secara nyata.
- **Throughput & Total Egress Jaringan**: Diparsing dari baris keluaran `netstat -ibn` berawalan `<Link#`. Hanya antarmuka fisik berawalan `en[0-9]+` yang dijumlahkan; antarmuka virtual bridge, tunnel, loopback, dan internal (`lo`, `bridge`, `utun`, `awdl`, `anpi`, `gif`, `stf`) disaring untuk mencegah duplikasi transfer data artifisial.
- **Laju Disk I/O**: Diekstrak dari properti IOKit registry via `ioreg -rd1 -c IOBlockStorageDriver`, menjumlahkan byte kumulatif `Bytes (Read)` dan `Bytes (Write)` pada seluruh driver penyimpanan blok aktif.
- **Core CPU & Model Perangkat Keras**: Jumlah core dibaca melalui `sysctl -n hw.ncpu` (menggabungkan performance core dan efficiency core). Identitas chip dan model hardware diambil via `system_profiler SPHardwareDataType` (misal: `Apple Mac Studio (Mac15,14) - Apple M3 Ultra`), dengan fallback ke `sysctl -n hw.model` / `machdep.cpu.brand_string`.
- **Versi OS & Siklus Hidup EOL**: Dipetakan ke nama pemasaran resmi (macOS 11 Big Sur, 12 Monterey, 13 Ventura, 14 Sonoma, 15 Sequoia, 26 Tahoe). Evaluasi siklus hidup mengikuti kebijakan resmi Apple: **versi rilis terkini + 2 rilis mayor sebelumnya** (macOS 26, 15, dan 14 aktif didukung; macOS 13 dan lebih lama diklasifikasikan sebagai EOL). Waktu instalasi sistem diekstrak dari epoch birthtime `/var/db/.AppleSetupDone`.
- **Telemetri Proses Aman (`ucomm`)**: Proses teratas dikumpulkan menggunakan `ps -Aro pid,user,%cpu,%mem,rss,ucomm` (Top CPU) dan `ps -Amo ...` (Top RAM). Pantau secara ketat mewajibkan kolom `ucomm` (nama pendek biner proses), melarang penggunaan `comm` atau `args` agar token rahasia, kunci otorisasi, atau parameter sensitif pada argumen baris perintah tidak pernah terekam.

#### Perbedaan Platform & Batasan Operasional
- **Tanpa Swap per Proses**: Kernel macOS tidak mengekspos alokasi VmSwap per proses; kolom swap per proses tetap kosong secara aman.
- **Tanpa Penghitung Login Gagal**: macOS tidak memiliki `/var/log/btmp` standar POSIX; telemetri insiden login gagal ditiadakan.
- **Tanpa Deteksi Error I/O Kernel Tanpa Root**: Perintah `dmesg` memerlukan hak akses root pada Darwin; inspeksi error log I/O disk dilewati pada akun non-root.
- **Visibilitas Daemon Docker Desktop**: Container Docker terdeteksi jika Docker Desktop terpasang dan akun SSH memiliki izin menjalankan perintah CLI `docker`.
- **Tab Packages Kosong**: Tab Packages sengaja dikosongkan pada host macOS karena audit paket Pantau dirancang untuk manajer paket distribusi Linux (`dpkg`, `rpm`, `apk`, `pacman`). Paket pihak ketiga dari Homebrew tidak dicatat sebagai paket sistem inti.

#### Diagnostik Jaringan pada macOS
- **Timeout Portabel**: macOS tidak memiliki GNU `timeout`; Pantau menggunakan subshell watchdog dan deadline bawaan perintah.
- **Metrik Ping**: Prosedur ping menggunakan argumen `-t <detik>` (bukan `-W` yang bernilai milidetik pada BSD ping). Parser ringkasan latensi disesuaikan untuk format baris Darwin (`round-trip min/avg/max/stddev`).
- **Traceroute Native**: Menggunakan biner pre-installed `/usr/sbin/traceroute` secara langsung. Traceroute **tidak pernah diinstal otomatis** pada host macOS.

#### Aturan Desired State pada macOS
- **Aturan Disk**: Dievaluasi pada volume APFS `/System/Volumes/Data` (atau `/`).
- **Aturan Service**: Diverifikasi terhadap unit layanan `launchd` via `launchctl list` atau deteksi proses via `pgrep`.
- **Aturan Port**: Diverifikasi via utilitas native `lsof -iTCP -sTCP:LISTEN -P -n`.
- **Aturan Cadangan (Backup Freshness)**: Diverifikasi menggunakan ekstraksi timestamp BSD via `stat -f %m <path>` atau `stat -f %B`.

#### Pemindaian Keamanan & Pemeriksaan Hardening macOS
- **Pengecekan Pembaruan Sistem**: Memeriksa patch sistem tertunda via `/usr/sbin/softwareupdate -l`.
- **Kategorisasi Heuristik**: Pembaruan dengan label `macOS` atau `Safari` dikategorikan sebagai **Pembaruan Keamanan**; pembaruan lainnya dihitung dalam total pembaruan yang tersedia.
- **Status Transparan Partial**: Apabila katalog pembaruan Apple tidak dapat dihubungi atau mengalami timeout jaringan, Pantau melaporkan status `partial` yang jujur alih-alih klaim aman palsu.
- **Pemeriksaan Hardening Native**:
  1. `sshd_permit_root_login`: Mengaudit file `/etc/ssh/sshd_config` dan direktori drop-in.
  2. `sshd_password_auth`: Memvalidasi kebijakan autentikasi password OpenSSH.
  3. `auto_security_updates`: Mengaudit kunci pembaruan otomatis di `/Library/Preferences/com.apple.SoftwareUpdate` (`AutomaticDownload`, `AutomaticallyInstallMacOSUpdates`, `CriticalUpdateInstall`, `ConfigDataInstall`).
  4. `firewall_active`: Memvalidasi status Application Firewall macOS via `socketfilterfw --getglobalstate` atau properti sistem.
---

## 3. Fase 2: Baseline Desired State & Proteksi Sistem

Pantau tidak hanya menampilkan metrik pasif; sistem ini secara aktif memverifikasi keselarasan sistem terhadap aturan acuan yang telah Anda tentukan.

### Membuat 1-Click Desired State Baseline

1. Buka **Workspace Modal** host dengan mengklik kartu server pada dashboard.
2. Beralih ke tab **Desired State**.
3. Klik tombol **1-Click Baseline**:
   - Pantau mengeksekusi inspeksi kilat melalui SSH untuk mendeteksi container Docker yang sedang berjalan, partisi disk root, service database/web aktif, dan entri cron job yang terpasang.
   - Sumber daya yang terdeteksi secara otomatis disimpan sebagai acuan baku (**Desired State**) mesin tersebut.
4. Kustomisasi atau tambah aturan manual sesuai kebutuhan operasional.

### Tabel Referensi Aturan Desired State

Pantau mendukung 10 jenis aturan Desired State yang dievaluasi secara otomatis pada setiap siklus inspeksi:
| Jenis (`Kind`) | Target Identifier | Kondisi Diharapkan (`Expected`) | Contoh Target & Expected | Kriteria Drift & Root Cause Excerpt |
| :--- | :--- | :--- | :--- | :--- |
| **`container`** | Nama kontainer Docker | `running` | `nginx` &rarr; `running` | Kontainer `exited`, `dead`, crash. Menyajikan exit code, OOMKilled, dan 50 baris log kontainer. |
| **`service`** | Nama daemon Linux | `active` | `mariadb` &rarr; `active` | Service `inactive` atau `failed`. Menyajikan 50 baris log dari `journalctl` atau syslog. |
| **`port`** | Port TCP atau `ip:port` | `listening` (atau `closed`) | `80` &rarr; `listening` | Port tidak terbuka / proses mati. Memeriksa `ss`/`netstat`. |
| **`disk`** | Mount point partisi | `<N%` (ambang batas) | `/` &rarr; `<85%` | Utilisasi partisi >= ambang batas atau storage IO timeout 5s (hung NFS). |
| **`cron`** | Kata kunci / perintah cron | `configured` | `backup.sh` &rarr; `configured` | Baris perintah tidak ditemukan dalam `crontab -l` user SSH. |
| **`backup`** | Path absolut berkas backup atau pola glob | `fresh_<N>h[,verify=<gzip\|tar\|zstd\|xz\|sha256>]` (default: 24h) | `/backup/*.sql.gz` &rarr; `fresh_24h,verify=gzip` | Berkas tidak ditemukan, berukuran 0 byte, usang, atau verifikasi integritas isi gagal (`gzip -t`, `tar -tf`, dll). Mengevaluasi berkas terbaru yang cocok. |
| **`http`** | URL HTTP/HTTPS lengkap | Token status dan batas latensi opsional | `https://api.internal/health` &rarr; `2xx,<1500ms` | Endpoint tidak dapat dihubungi dari Host via `curl`, kode status tidak cocok, atau latensi melampaui batas. Dievaluasi dari perspektif Host via SSH. |
| **`tls`** | Hostname/port atau URL HTTPS | `>=Nd` (sisa hari minimum sebelum sertifikat kedaluwarsa) | `api.example.com:443` &rarr; `>=14d` | Sertifikat kedaluwarsa dalam N hari atau handshake TLS gagal. Dievaluasi dari perspektif Host via `openssl`. |
| **`process`** | Pola atau nama proses | `running` (atau `stopped`) | `celery` &rarr; `running` | Proses tidak aktif pada sistem (memeriksa `pgrep`/`ps`). |
| **`security_updates`** | `security` (tetap) | `none` atau `<=N` (ambang batas) | `security` &rarr; `none` | Pembaruan keamanan melebihi ambang batas N, belum pernah dipindai, hasil scan >72 jam, atau berstatus `unsupported`/`error`. Dievaluasi dari cache scan terbaru. |
### Konfigurasi Backup Freshness & Protected Path

- **Backup Freshness**: Daftarkan lokasi file backup target (contoh: `/backup/postgres/daily.sql.gz`) beserta batas kedaluwarsa waktu (misal: harus diperbarui dalam 24 jam terakhir dan ukuran berkas `> 0 byte`). Pantau akan menandai **Drift** jika berkas backup hilang, kosong, atau usang.
- **Protected Path**: Daftarkan path direktori sistem inti (seperti `/etc/`, `/boot/`, `/var/lib/docker/`) ke dalam daftar terproteksi agar terhindar dari ketidaksengajaan operasi ubah, timpa, atau hapus berkas melalui modul SFTP file manager.

---

## 4. Fase 3: Observabilitas, Analisis Drift & Respons Insiden

### Siklus Inspeksi, Inspeksi Massal (Inspect All) & Kesegaran Data

Pantau mengumpulkan telemetri actual state dan memvalidasi desired state melalui siklus inspeksi terjadwal maupun on-demand:

- **Inspeksi Massal (Inspect All)**: Klik tombol `⚡ Inspect All` di bilah atas untuk memicu pembacaan kondisi seluruh armada server secara serentak (asinkron). Eksekusi massal diproses melalui antrean worker pool terbatas (maksimal 5 host bersamaan) dengan proteksi anti-flood cooldown guna mencegah saturasi bandwidth dan lonjakan beban CPU.
- **Inspeksi Mandiri Per-Host**: Klik tombol petir `⚡` pada masing-masing kartu server atau baris tabel, atau gunakan ikon refresh kompak pada header modal detail host (di samping badge status).
- **Anti-Flood Inspection Cooldown**: Guna mencegah pembanjiran koneksi (*connection flooding*), inspeksi manual menerapkan jeda waktu minimum 15 detik per host. Pemicuan inspeksi dalam rentang jeda ini mengembalikan `HTTP 429 Too Many Requests` disertai notifikasi toast peringatan sisa detik tunggu.
- **In-Flight Concurrency Guard**: Mekanisme penguncian aktif pada level host mencegah inspeksi ganda (*in-flight conflict*). Meminta inspeksi ketika proses sebelumnya masih berjalan akan menghasilkan `HTTP 409 Conflict` dan menampilkan animasi spinner pada tombol tindakan hingga proses tuntas.
- **Structured Inspection Runs & Riwayat Audit**: Setiap siklus inspeksi secara otomatis merekam data riwayat audit terstruktur di basis data SQLite internal `inspection_runs` (waktu mulai, durasi roundtrip dalam milidetik, badge status, ringkasan manusiawi, dan rincian diagnostik root cause saat anomali). Operator dapat memantau riwayat ini pada tab **Riwayat Inspeksi** di Workspace Modal Host Detail.
- **Pembersihan Otomatis (Inline Rolling Prune)**: Fitur auto-pruning sebaris tanpa konfigurasi secara otomatis mempertahankan kuota 100 riwayat terbaru per host di SQLite, menjaga ukuran database tetap efisien dan stabil tanpa membebani scheduler latar belakang.
- **Proteksi Hung NFS & Stale Storage Mounts**: Evaluasi partisi disk secara ketat mengisolasi filesystem lokal (`df -lPk /`) untuk melompati remote storage, serta membungkus pengecekan disk dengan batas waktu 5 detik. Mount network storage yang macet (NFS, CIFS) ditandai sebagai indikasi *drift* diagnostik yang jelas tanpa membuat proses inspeksi membeku.
- **Hard Timeout Budget Total 45 Detik**: Setiap siklus inspeksi dibatasi batas waktu absolut maksimum 45 detik. Sesi SSH yang gantung akan diputus paksa di sisi Pantau dan dicatat berstatus *down* untuk menjaga ketersediaan antrean worker. Koneksi TCP yang timeout dicoba ulang dengan koneksi baru (maksimal 4 percobaan, masing-masing paling lama 4 detik), sehingga host yang sebenarnya bisa dijangkau lewat jalur jaringan yang sering kehilangan paket tidak ditandai *down*; port yang menolak koneksi atau tidak terjangkau langsung gagal.
- **Indikator Kesegaran & Durasi Eksekusi**: Setiap kartu dan baris tabel menyajikan waktu relatif inspeksi terakhir (contoh: `🕒 2m lalu`, `🕒 baru saja`) berdampingan dengan badge durasi roundtrip riil (contoh: `⚡ 320ms`).
- **Pendeteksian Data Usang (Stale Inspection)**: Jika sebuah host tidak berhasil diinspeksi melebihi batas waktu toleransi (> 10 menit atau 2× interval normal), Pantau menampilkan penanda peringatan oranye `⚠️ Data Usang` untuk mencegah false-confidence pada data telemetri lama. Ketika Background Inspection sengaja dinonaktifkan (diisi `0`), peringatan data usang otomatis disupresi untuk menghindari false alarm.
- **Auto-Refresh Dashboard**: Tampilan browser secara otomatis menyinkronkan status kartu host setiap 30 detik tanpa memerlukan reload halaman manual.
- **Konfigurasi Interval Inspeksi & Saklar Nonaktif**: Administrator dapat mengubah frekuensi inspeksi background (default 300 detik / 5 menit, minimal 30 detik, atau setel ke `0` untuk menonaktifkan inspeksi latar belakang agar murni berjalan manual/on-demand) melalui modal **Settings** > tab **General**. Saat dinonaktifkan, badge indikator (`⏸️ Background Inspection Paused`) muncul pada toolbar dashboard dan footer engine.

### Resource Metrics, Process Telemetry & Observabilitas Perangkat Keras

Pengambilan telemetri mengekstrak metrik kapasitas komputasi riil secara berkelanjutan:
- **Laju Disk I/O Real-Time (Speedometer)**: Mengevaluasi kecepatan baca dan tulis disk secara dinamis (B/s) dan akumulasi volume byte dari `/proc/diskstats`. Menghadirkan pill status beban throughput kualitatif (Optimal `< 10 MB/s`, Aktif `10–80 MB/s`, Beban I/O Tinggi `≥ 80 MB/s`) pada Kartu Host dan Modal Detail Host.
- **Kapasitas Absolut Inline**: Bar progress RAM dan Disk menyajikan angka riil `terpakai / total` (misal `4.2 GB / 15.8 GB`) langsung secara inline tanpa penalti layout vertikal.
- **Telemetri Swap**: Baris Swap permanen pada setiap kartu host menyajikan pemakaian memori virtual aktif, atau menampilkan penanda redup `Disabled` jika swap belum dikonfigurasi di server.
- **Beban CPU Ternormalisasi**: Beban antrian sistem (*load average*) dievaluasi terhadap jumlah core CPU fisik dan logis (`nproc`), menampilkan aksen warna peringatan ketika antrian melampaui kapasitas core.
- **Kartu Sys Info**: Modal Detail Host menyajikan tipe model hardware, lapisan virtualisasi, kuantitas core CPU, dan topologi partisi penyimpanan.
- **Top Resource Consumers (Process Telemetry)**: Di setiap siklus inspeksi rutin, Pantau mengekstrak 5 proses teratas pemakan sumber daya dalam tiga dimensi:
  - **Top CPU**: Melacak PID, user, `%cpu`, jejak memori, dan nama proses. Pada Linux, utilisasi CPU diukur secara aktual dalam jendela sampel ~1 detik per inspeksi (bukan rata-rata seumur hidup proses dari `ps %cpu`; proses multi-threaded dapat melebihi 100% karena nilai dihitung per satu core). Pada macOS, penggunaan terkini dipantau via `ps -Aro`. Pada host yang perintah `ps`-nya tidak mendukung `--sort` (seperti BusyBox/Alpine), Pantau melakukan pengurutan sendiri agar 5 proses teratas tetap akurat.
  - **Top RAM (Physical RSS)**: Melacak PID, user, `%mem`, jejak memori RSS, dan nama proses (diurutkan langsung atau diproses oleh Pantau pada lingkungan minimal seperti BusyBox/Alpine).
  - **Top Swap (Virtual Memory)**: Diekstrak secara kondisional hanya saat `SwapUsedBytes > 0` dengan mem-parsing baris `VmSwap:` di `/proc/[0-9]*/status`, memetakan nama user dari `/etc/passwd` (fallback ke UID numerik hanya jika nama tidak dapat diuraikan) guna mendeteksi pemborosan swap tanpa overhead daemon jarak jauh.
  - **Pintasan Terminal Satu-Klik**: Kartu Top Resource Consumers di tab Overview menyediakan tombol **Open Terminal** untuk langsung membuka sesi shell interaktif ke host terkait guna investigasi cepat.

### Inventarisasi Paket & Audit Higienitas Software

Pantau menyediakan visibilitas inventaris paket perangkat lunak secara agentless lintas berbagai distribusi Linux:
- **Dukungan Polyglot Multi-Distro**: Deteksi bawaan untuk Debian/Ubuntu (`dpkg-query`), RHEL/CentOS/AlmaLinux/Rocky (`rpm`), Alpine (`apk`), dan Arch Linux (`pacman`).
- **Kategorisasi Higienitas Perangkat Lunak**:
  - **Manual Install (`⭐ Manual Install`)**: Paket yang dipasang secara eksplisit oleh administrator melalui manajer paket (misal `apt-mark showmanual`, `dnf history`, `/etc/apk/world`, `pacman -Qqe`), membedakan utilitas operasional dari paket bawaan distro.
  - **Base System**: Paket bawaan instalasi awal sistem operasi (distro bootstrap) dan pustaka dependensi inti.
  - **Dormant Packages (`💤 Dormant Packages`)**: Unit aplikasi/paket manual install yang unit layanan `systemd`-nya berada dalam status inactive, disabled, atau failed selama periode observasi, mempermudah identifikasi potensi celah keamanan atau daemon usang.
- **Tab Packages di Workspace Modal**:
  - Menampilkan nama paket, versi, arsitektur, repositori sumber, dan tanggal instalasi.
  - Fitur pencarian instan via bilah filter real-time.
  - Filter kategori cepat: **Manual Install**, **Dormant Packages**, dan **All Packages**.
  - **Rescan Packages**: Tombol pemindaian ulang on-demand dengan dialog konfirmasi, dilindungi timer cooldown anti-flood 30 detik (`HTTP 429 Too Many Requests`) dan kunci konkurensi latar belakang (`HTTP 409 Conflict`).
  - **Prinsip Non-Destruktif**: Pantau tidak menyediakan tombol uninstall destruktif di web UI untuk mencegah risiko rusaknya dependensi berantai (*cascading broken dependencies*). Administrator diarahkan memanfaatkan Terminal Dock bawaan atau merumuskan aturan Desired State.


### Fleet Search (Paket & Container di Semua Host)

Jawab pertanyaan seperti "host mana yang masih memakai `openssl` 1.1?" atau "siapa yang menjalankan `postgres:13`?" tanpa login ke mana pun: buka Command Palette (`Ctrl+K`), ketik minimal 2 karakter lalu pilih **Cari paket & container di semua host**.
- Hasil dikelompokkan per versi paket atau image container, masing-masing dengan host yang memilikinya (grup, versi atau nama container dan statusnya) serta umur data host tersebut. Klik host untuk membukanya di tab Packages atau Docker. Saring dengan **All / Packages / Containers**.
- Tidak ada scan saat mencari: paket berasal dari scan paket terakhir tiap host (jalankan **Rescan Packages** untuk memperbarui), container dari inspeksi terakhir (container dan image-nya dicatat setiap inspeksi dan saat tab Docker dibuka) ditambah desired rule `container` dan proyek Compose yang tersimpan. Host yang belum punya data dihitung di catatan.
- Maksimal 500 hasil per pencarian (`GET /api/search?q=&kind=all|package|container&limit=`).

### Pemindaian Keamanan Host & Pemeriksaan Hardening

Pantau menyediakan visibilitas keamanan sistem operasi secara agentless dan pasif (read-only), mengidentifikasi pembaruan patch keamanan yang tertunda, kebutuhan reboot, kesegaran metadata repositori, serta 4 pemeriksaan pengerasan sistem (*hardening checks*) krusial—beroperasi 100% mandiri (*airgapped*) tanpa agen pihak ketiga, tanpa hak akses root/sudo, dan tanpa permintaan data keluar ke database CVE publik di internet.

#### Tab Security di Workspace Modal
Buka Workspace Modal pada host mana pun dan pilih tab **Security**:
- **Ringkasan Metrik**:
  - **Pending Security Updates**: Jumlah paket dengan pembaruan perbaikan keamanan yang belum diterapkan.
  - **Total Pending Updates**: Jumlah total paket sistem yang dapat ditingkatkan (*upgradable*).
  - **Rincian Severity**: Lencana warna mengelompokkan paket ke dalam **Critical**, **Important**, **Moderate**, **Low**, dan **Unknown** (paket Debian/Ubuntu diklasifikasikan sebagai `Unknown` karena utilitas `apt` bawaan tidak memaparkan tingkat keparahan upstream).
  - **Reboot Required**: Menandai apakah kernel atau pustaka inti sistem memerlukan reboot (`/var/run/reboot-required` pada Debian/Ubuntu atau `needs-restarting -r` pada keluarga RHEL).
  - **Repo Cache Age**: Mengukur waktu sejak terakhir kali metadata paket lokal diperbarui.
- **Tabel Security Findings**: Menampilkan daftar paket terdampak, versi terpasang saat ini, versi perbaikan yang tersedia, lencana keparahan (*severity*), ID advisori keamanan (seperti `RHSA-*`, `ALAS-*`) dan, pada host dnf/yum, ID CVE advisori tersebut (bertaut ke NVD, dari `dnf updateinfo list --security --with-cve` / `yum updateinfo list cves`). Filter juga mencocokkan ID CVE.
- **Hardening Checks**: Memverifikasi 4 kontrol pengerasan konfigurasi sistem berbasis read-only. Sebuah check hanya `pass` bila pengaturan yang benar-benar berlaku terverifikasi; keberadaan berkas atau aturan saja tidak cukup.
  - `sshd_permit_root_login`: Membaca konfigurasi OpenSSH efektif dari `sshd -G` (OpenSSH 9.3+, tanpa root) atau `sshd -T` (root atau `sudo -n` tanpa kata sandi), yang sudah menerapkan `Include`, `Match`, dan default bawaan build distro. Bila keduanya tidak tersedia, `/etc/ssh/sshd_config` dibaca seperti sshd membacanya: `Include` menyisipkan drop-in `sshd_config.d/*.conf` yang cocok di baris tempatnya berada (drop-in yang di-include di atas mengalahkan berkas utama, drop-in yang tidak pernah di-include diabaikan), *first-match-wins*, blok `Match` dilewati. `no` / `prohibit-password` / `forced-commands-only` → `pass`, `yes` → `fail`. Bila tidak diatur, default upstream (`prohibit-password`) hanya diasumsikan pada Debian/Ubuntu/Alpine/Arch/macOS; distro lain (RHEL/CentOS ≤ 8 ber-default `yes`) dilaporkan `unknown`.
  - `sshd_password_auth`: `pass` hanya bila `PasswordAuthentication no` dan keyboard-interactive tidak dapat meminta kata sandi (`KbdInteractiveAuthentication`/`ChallengeResponseAuthentication no`, atau `UsePAM` nonaktif); selain itu, atau bila tidak diatur, `warn`.
  - `auto_security_updates`: Debian/Ubuntu memakai nilai efektif `apt-config dump`: `pass` hanya bila `APT::Periodic::Unattended-Upgrade` bukan `0`, paket `unattended-upgrades` terpasang, dan `apt-daily-upgrade.timer` tidak dinonaktifkan atau di-mask (`Update-Package-Lists` saja tidak memasang apa pun → `warn`). RHEL: `dnf-automatic-install.timer` → `pass`; `dnf-automatic.timer` atau `yum-cron` → `pass` hanya dengan `apply_updates = yes` di `/etc/dnf/automatic.conf` / `/etc/yum/yum-cron.conf` (default-nya hanya mengunduh → `warn`).
  - `firewall_active`: `pass` bila ufw atau firewalld aktif, atau rantai input benar-benar memfilter: chain nft ber-hook `input` dengan `policy drop` atau aturan `drop`/`reject` tanpa syarat, atau rantai iptables `INPUT` dengan kebijakan `DROP` atau aturan `-j DROP|REJECT` tanpa syarat. Tabel yang hanya dibuat Docker, kube-proxy, atau fail2ban (kebijakan accept) dilaporkan `warn`. Tabel firewall membutuhkan root: pemindaian membacanya dengan `sudo -n` tanpa kata sandi bila pengguna SSH bukan root (backoff 24 jam bersama setelah penolakan). Jika root maupun sudo tidak tersedia, dilaporkan sebagai `unknown` disertai rincian penjelasan.
- **Catatan Cakupan (*Coverage Notes*)**: Memaparkan batasan lingkungan operasional, kendala izin akses, atau peringatan cache repositori yang kedaluwarsa.
- **Firewall** (baca/tulis, berbeda dari bagian tab lainnya): kartu di bagian bawah membaca **ufw**, **firewalld**, dan **iptables** secara langsung melalui SSH saat **Muat firewall** diklik (tidak pernah saat tab dibuka, sehingga tidak ada koneksi SSH atau probe sudo tanpa permintaan Anda). Per backend Anda dapat **Siapkan** (pasang bila belum ada, lalu baseline aman: ufw = tolak masuk / izinkan keluar + SSH + aktifkan; firewalld = jalankan/aktifkan + port SSH; iptables = loopback + established + SSH, kebijakan `INPUT` `DROP`, lalu simpan), **Aktifkan/Nonaktifkan** (ufw, firewalld), **Tambah**, **Ubah**, dan **Hapus** aturan, membuat **Zona baru** (firewalld, via tombol Tambah zona atau langsung dari dialog aturan), serta **Simpan ke disk** (iptables; selain itu hanya berlaku di runtime). Untuk firewalld, dialog aturan memecah isian menjadi kolom khusus per tipe (nama service dengan datalist populer, port dan protokol, atau rich rule terstruktur dengan fallback aturan mentah). Perintah dijalankan sebagai root, atau `sudo -n` tanpa kata sandi, atau pengguna SSH apa adanya (biasanya ditolak: kartu menampilkan modenya). Aturan yang tidak dapat diungkapkan formulir ubah (conntrack, komentar, profil aplikasi, pasangan `(v6)`) diberi ikon gembok: hanya dapat dihapus. Keamanan: aturan yang mengizinkan atau memblokir port SSH yang dipakai Pantau (tanpa syarat, dari sumber mana pun) tidak dapat dihapus, diubah, maupun diblokir dari sini (HTTP 403, gunakan terminal), dan Aktifkan/Siapkan selalu mengizinkan port itu lebih dulu. Ubah dan Hapus memeriksa ulang bahwa aturan yang tampil masih berada di posisi yang sama dan ditolak bila berubah. `ip6tables` (IPv6) dan ruleset native nftables tidak dikelola.

#### Prinsip Kejujuran Cakupan (*Honest Coverage Principle*)
Pantau memegang teguh aturan integritas (**Honesty Rule**): host tidak pernah dinyatakan "bersih" atau "aman" jika evaluasi tidak memiliki cakupan penuh.
- Jika sistem operasi belum didukung untuk analisis paket keamanan (seperti Alpine Linux atau Arch Linux), hasil pemindaian berstatus `status: unsupported` dengan catatan penjelasan (pemeriksaan hardening tetap dijalankan).
- Jika perintah pengecekan pembaruan itu sendiri terhambat batas izin pengguna, atau jika cache paket lokal lebih dari 7 hari (> 168 jam), status pemindaian menjadi `status: partial` disertai peringatan bahwa hasil mungkin belum mencakup advisori terbaru.
- Pemeriksaan hardening yang tidak dapat dievaluasi (misalnya status firewall butuh root, atau berkas konfigurasi tidak ada) ditampilkan sebagai `unknown` dan dicantumkan di catatan pemindaian; status tidak berubah karena data pembaruan tetap terkumpul penuh.
- Ketika status `unsupported` atau `error`, UI menampilkan pembaruan tertunda, rincian severity, dan status reboot sebagai **tidak dinilai**, bukan nol.
- Distro dengan utilitas versi lama (CentOS 6/7, Debian 7+) ditangani secara adaptif tanpa menggagalkan siklus inspeksi.

#### Intelijen Eksploitasi (CISA KEV + EPSS) & Database Kerentanan
Severity menyatakan seberapa parah sebuah celah, bukan apakah penyerang memakainya. Aktifkan **CISA KEV dan EPSS** di **Pengaturan → Umum** (default nonaktif) dan tab Security menandai setiap CVE pada temuan ([ADR-0064](adr/0064-exploit-intelligence-kev-epss.md)):
- **Dieksploitasi (KEV)**: CVE tercantum di katalog CISA Known Exploited Vulnerabilities (arahkan kursor untuk nama kerentanan, tanggal masuk katalog, dan tenggat perbaikan federal AS; `⚠` menandai penggunaan dalam ransomware).
- **EPSS x%**: peluang CVE dieksploitasi dalam 30 hari ke depan menurut FIRST (kuning mulai 10%), dengan persentilnya saat kursor diarahkan.
- Urutkan dengan **Eksploitasi: KEV, lalu EPSS** untuk menambal paket yang sedang dieksploitasi lebih dulu. Kartu **Intelijen eksploitasi** merangkum jumlah CVE yang ada di KEV atau ber-EPSS tinggi, menampilkan tanggal katalog KEV dan skor EPSS, serta galat feed.
- Server Pantau mengunduh katalog KEV paling sering sekali sehari dan meminta EPSS hanya untuk CVE yang belum diberi skor dalam sehari terakhir (100 per permintaan); tidak ada yang disimpan di basis data, dan selama pengaturan nonaktif tidak ada data yang keluar dari server. Bila feed tidak terjangkau, katalog KEV terakhir yang berhasil tetap dipakai dan galatnya ditampilkan.
- Saat ini hanya host dnf/yum yang melaporkan ID CVE. Pada host apt, apk, pacman, dan macOS kartu menampilkan **tidak dinilai**, bukan seolah-olah host aman.
- **Database kerentanan** (tombol di kartu, juga sebelum scan pertama) menampilkan 45 database dan feed kerentanan dunia, dikelompokkan per kategori dengan filter teks serta badge akses API, gratis, dan **Dipakai Pantau**. Daftar ini salinan tersemat dari [haxdoggy/vulnerability-databases](https://github.com/haxdoggy/vulnerability-databases) ([CC BY 4.0](https://creativecommons.org/licenses/by/4.0/)), ditampilkan tanpa perubahan beserta tanggal snapshot-nya, sehingga tetap tersedia *airgapped*.

#### Chip Keamanan di Dashboard
Setiap host yang sudah dipindai menampilkan chip perisai di samping Lifecycle Score pada kartunya, dan di kolom **Keamanan** pada tampilan daftar (`GET /api/hosts` menyertakan ringkasan `security`: `status`, `scanned_at`, `pending_security`, `reboot_required`, `hardening_fail`, `hardening_warn`). Chip menampilkan jumlah pembaruan keamanan tertunda bila ada (`N/A` bila pemindaian `unsupported` atau `error`); warnanya merah bila ada pembaruan keamanan tertunda atau hardening check yang gagal, kuning untuk pemindaian parsial, perlu reboot, atau peringatan hardening, dan hijau bila semuanya lolos. Arahkan kursor untuk rincian dan usia pemindaian; klik untuk membuka tab **Keamanan** host tersebut. Host yang belum pernah dipindai tidak menampilkan chip.

#### Pemindaian On-Demand & Proteksi Cooldown
- Klik tombol **Scan Security Now** di tab Security untuk memicu evaluasi sinkron secara instan.
- **Proteksi Cooldown & Konkruensi**: Pemindaian manual menerapkan batas jeda 60 detik (`HTTP 429 Too Many Requests` menyertakan sisa detik jeda pada header `X-Scan-Cooldown-Remaining`) dan kunci konkruensi host tunggal (`HTTP 409 Conflict`).

#### Penjadwalan Pemindaian Latar Belakang
- **Konfigurasi Interval**: Atur parameter `security_scan_interval_hours` di menu **Settings** → **General** (default: `24` jam; nilai yang diterima: `1`–`720` jam).
- **Menonaktifkan Pemindaian Latar Belakang**: Mengisi nilai `0` akan menonaktifkan pemindaian otomatis berkala, sehingga pemindaian hanya berjalan secara manual atas permintaan administrator.
- **Pekerja Latar Belakang**: Scheduler berjalan setiap 10 menit untuk memindai host yang scan terakhirnya telah melampaui interval atau belum pernah dipindai (dibatasi maksimal 2 pemindaian konkuren, dimulai ~2 menit setelah aplikasi dijalankan).

#### Penegakan Desired State (`security_updates`)
Pantau kepatuhan pembaruan keamanan secara otomatis melalui aturan Desired State:
- **Jenis Aturan (`Kind`)**: `security_updates`
- **Sasaran Target (`Target`)**: `security` (tetap/fixed)
- **Kondisi Diharapkan (`Expected`)**: `none` atau `<=N` (dengan `none` mengharuskan 0 paket keamanan tertunda; `<=N` menoleransi maksimal N paket).
- **Evaluasi Berbasis Cache**: Dievaluasi langsung dari hasil pemindaian terakhir yang tersimpan di basis data SQLite tanpa melakukan koneksi SSH langsung saat siklus inspeksi rutin.
- **Kondisi Kepatuhan & Drift**:
  - `ok`: Jumlah pembaruan keamanan tertunda berada di bawah atau sama dengan ambang batas (`<= N`).
  - `drift`: Jumlah pembaruan keamanan melebihi ambang batas `N` (potongan bukti root cause memuat daftar paket teratas, versi, dan advisori).
  - `drift`: Host belum pernah dipindai (`not_scanned`), data pemindaian lebih dari 72 jam (`stale`), atau status pemindaian terakhir adalah `unsupported` atau `error`. Pantau pantang mengklaim kepatuhan tanpa data yang segar dan terverifikasi.

#### Referensi REST API
Seluruh endpoint keamanan memerlukan autentikasi sesi yang valid (`authMiddleware`):
- `GET /api/hosts/{id}/security`: Menghasilkan `{"scanned": false}` jika belum pernah dipindai, atau `{"scanned": true, "scan": <SecurityScan>}`. Menyertakan header `X-Scan-Cooldown-Remaining: <sec>` saat masa jeda aktif.
- `POST /api/hosts/{id}/security/scan`: Menjalankan pemindaian keamanan sinkron. Menghasilkan status `200 OK` dengan data hasil terbaru, `429 Too Many Requests` jika masih dalam masa jeda, `409 Conflict` jika pemindaian sedang berlangsung, atau `500 Internal Server Error` saat terjadi kegagalan koneksi (tanpa menimpa hasil scan sebelumnya).

#### Batasan Operasional & Desain Airgapped
- **Tanpa Sudo / Tanpa Agen**: Pengumpulan data dilakukan murni sebagai pembacaan (read-only) menggunakan izin akun pengguna SSH standar. Tidak pernah mengeksekusi `sudo`, tidak memasang paket baru, dan tidak memperbarui cache jaringan (`apt-get update` / `dnf makecache`).
- **Bebas Database Kerentanan Eksternal**: Kompatibel penuh dengan jaringan tertutup (*airgapped*, merujuk ADR-0013). Analisis murni mengandalkan metadata yang dipaparkan oleh manajer paket distro host. Pencocokan database CVE eksternal tidak disertakan dalam Fase 1 (integrasi feed CVE di masa mendatang hanya dirancang sebagai opsi tambahan opsional / *opt-in*).
- **Telemetri Turunan**: Hasil pemindaian disimpan di tabel SQLite `security_scans` dan dikecualikan dari System Snapshot terenkripsi karena merupakan telemetri yang dapat direproduksi melalui pemindaian ulang.

### Mendeteksi Drift & Root Cause Excerpt

Pada setiap siklus **Inspection** berkala (standar: setiap 5 menit) atau saat dipicu manual:

1. Pantau membandingkan kondisi riil (**Actual State**) target terhadap acuan baku (**Desired State**).
2. Jika ditemukan anomali atau deviasi (misal: container aplikasi mendadak berhenti atau partisi disk melampaui ambang batas), status host akan berubah menjadi **Drift**.
3. **Automated Root Cause Excerpt**: Pantau secara otonom menarik potongan bukti diagnostik penyebab gangguan:
   - Exit code container beserta indikator terminasi kehabisan memori (`OOMKilled`).
   - Potongan 50 baris log terakhir penyebab crash melalui `docker logs --tail 50 <container>` atau `journalctl -u <service> -n 50 --no-pager`.
4. **Root Cause Excerpt** langsung disajikan di lini masa insiden pada **Workspace Modal** dan dikirimkan ke **Notification Channel** (Telegram bot / Webhook).

### Investigasi Interaktif via Terminal Dock & Terminal Preset

Saat insiden memerlukan penanganan terminal langsung:

- **Terminal Dock**: Buka sesi shell interaktif multi-tab berbasis WebSocket PTY (`xterm.js`).
- **Terminal Header Launcher**: Akses dan kontrol dok terminal dari header navigasi utama. Lencana sesi aktif menampilkan akumulasi sesi PTY di seluruh sistem. Saat dok kosong, mengklik launcher akan langsung membuka dropdown pemilih host untuk memulai sesi baru.
- **Reactive Host Session Badges**: Kartu host pada Grid View dan List View menyematkan badge angka reaktif ketika terdapat sesi shell yang aktif untuk server tersebut.
- **Split-Pane View (`Alt+\`)**: Belah area kerja terminal menjadi dua jendela berdampingan untuk mengamati log dari dua host berbeda secara sinkron.
- **Terminal Preset & Pencarian Live**: Jalankan perintah investigasi rutin yang telah disimpan (misal: `htop`, `journalctl -f`, `docker stats`) baik berlingkup global maupun terikat secara dinamis pada host aktif di tab terminal. Dilengkapi kolom pencarian instan pada dropdown yang menyaring berdasarkan judul preset, perintah, atau tag sesi tmux (`t:tag`).
- **Akses Cepat Pinned Paths Terminal (`📌`)**: Akses daftar berkas dan direktori tersemat untuk host aktif langsung dari bilah alat terminal. Dilengkapi pencarian real-time lintas path dan catatan operasional. Mengklik direktori akan mengeksekusi `cd '<path>'`; mengklik berkas akan menyisipkan path bertanda kutip ke sesi terminal. Lepas sematan secara instan lewat tombol `✕`.
- **Pindah Langsung ke Pengelola Berkas (`📁 Files`)**: Lompat mulus dari tab terminal aktif ke tab Pengelola Berkas host yang langsung membuka direktori kerja terminal saat itu sekaligus meminimalkan dok terminal.
- **Kontrol Zoom Font**: Sesuaikan ukuran teks terminal langsung dari toolbar (`+`, `-`, dan `0` untuk reset).
- **Deteksi Link Terminal Interaktif (`Ctrl+Click` / `Cmd+Click`)**: Link URL yang tercetak di output terminal otomatis terdeteksi. Menekan kombinasi `Ctrl+Click` (Linux/Windows) atau `Cmd+Click` (macOS) langsung membuka situs web publik di tab peramban baru tanpa mengganggu seleksi teks log dan operasi copy-paste terminal.
- **Remote Web Port Tunneling (`localhost:port`)**: Ketika perintah terminal menghasilkan URL loopback remote (misal `http://localhost:3000` atau `127.0.0.1:8080`), mengklik link tersebut memicu dialog konfirmasi modal (*Remote Web Service*) dengan tombol 1-klik untuk membuka layanan via reverse proxy SSH `direct-tcpip` Pantau (`/api/tunnel/:hostId/:port/`) di tab browser baru.
- **Menu Toolbar Dropdown Ports (`🌐 Ports`)**: Akses aplikasi web privat yang berjalan di server remote dengan membuka dropdown `🌐 Ports` di toolbar terminal. Masukkan nomor port (1-65535) dan klik **Buka** untuk meluncurkan sesi tunnel. Port yang baru saja dibuka otomatis tersimpan di memori browser lokal (*localStorage*). Tunnel dibuka di port tunnel tersendiri (`-tunnel-port`, default: port kosong pertama setelah port UI, dicetak saat startup), yaitu origin peramban yang berbeda dari UI, sehingga aplikasi web remote tidak bisa memanggil API Pantau; cookie sesi Pantau tidak diteruskan ke aplikasi remote.
- **Zero-Configuration Remote Browser Bridge**: Perintah CLI di server remote (`gh auth login`, `xdg-open`, Python `webbrowser`, AWS SSO, `npm login`) secara otomatis membuka tab peramban desktop asli di komputer klien via protokol escape terminal (OSC 1337 / OSC 777), mengeliminasi jebakan browser teks headless (`w3m`/`lynx`). Jika URL yang diminta adalah alamat loopback di host remote (`localhost`, `127.x`, `::1`), Pantau meminta konfirmasi dulu (host dan port ditampilkan) sebelum membukanya lewat tunnel.
- **Sinkronisasi Direktori Kerja**: Memantau direktori kerja aktif secara otomatis melalui pembaruan judul jendela terminal dan sekuens escape ANSI OSC 7.
- **Port Forward ke Komputer Ini** (seperti `ssh -L`): untuk service non-HTTP (Postgres, Redis, MySQL, …) buka **Port Forward** dari menu kartu host, **Forward port to this computer…** di dropdown `🌐 Ports`, atau Command Palette. Pilih port remote (dan opsional host tujuan dilihat dari server itu, default `127.0.0.1`); Pantau mendengarkan di port lokal kosong pertama dari rentang `-forward-ports` (default `15000-15019`) dan menampilkan alamat yang dipakai, misalnya `psql -h 127.0.0.1 -p 15000`. Dialog menampilkan semua forward aktif beserta jumlah koneksi, trafik, dan error terakhir; **Stop** menghentikan satu forward. Forward memakai alamat bind yang sama dengan UI (`-listen`), hanya disimpan di memori (berhenti saat Pantau berhenti), dan dihentikan saat host-nya dihapus. Docker: file compose mempublikasikan rentang ini hanya di `127.0.0.1`.
- **Modal Stacking & Tombol Escape**: Dialog aksi turunan (form preset, aturan desired state, catatan) menumpuk di atas modal induk secara dinamis dengan layer z-index bertingkat. Penekanan tombol `Escape` menutup dialog paling atas secara berurutan tanpa merusak sesi terminal di bawahnya.

### In-App Activity Console & Live Telemetry Stream

Untuk memberikan visibilitas operasional yang transparan, Pantau dilengkapi dengan konsol diagnostik real-time yang menampilkan aktivitas yang sedang dijalankan daemon dan subsistem setiap saat:

- **Subsistem yang Dimonitor**: Menyiarkan telemetri terstruktur dari 7 modul inti:
  - `inspect`: Siklus inspeksi host, latensi roundtrip, status sehat, host down, dan ringkasan deviasi drift.
  - `ssh`: Upaya injeksi kunci SSH (*key provisioning*), inisialisasi runner, dan kegagalan koneksi.
  - `transfer`: Unggah berkas SFTP dan progres pemindahan berkas lintas-host di latar belakang.
  - `terminal`: Pembukaan, perubahan ukuran, dan penutupan sesi shell PTY interaktif.
  - `snapshot`: Pencadangan basis data terenkripsi otomatis dan sinkronisasi ke repositori GitHub.
  - `system`: Inisialisasi daemon, pemindaian port otomatis, instance file lock, dan graceful shutdown.
  - `auth`: Upaya login administrator, validasi kredensial, dan verifikasi token 2FA.
- **Membuka/Menutup Activity Console**: Klik tombol `⚡ Activity` di bilah footer, atau tekan pintasan keyboard global **`Ctrl + \``** (backtick) dari mana saja di dashboard.
- **Indikator Status & Badge Unread**: Tombol footer menampilkan dot hijau yang berdenyut (*pulsing*) saat terhubung ke live stream. Jika laci tertutup saat terjadi peringatan atau error, badge merah unread counter (`99+`) akan menandai masalah baru hingga laci dibuka.
- **Smart Auto-Scroll & Freeze Feed**: Log baru mengalir secara dinamis ke baris terbawah tabel. Menggulir ke atas akan otomatis menangguhkan auto-scroll agar Anda dapat memeriksa baris historis tanpa terganggu lonjakan tampilan (*Smart Auto-Scroll*). Anda juga dapat menekan tombol **Pause / Resume** untuk membekukan stream secara eksplisit.
- **Filter Severity Level & Pencarian Instan**: Saring baris yang tampil berdasarkan tingkat keparahan (`ALL`, `ERROR`, `WARN`, `INFO`), atau gunakan kotak pencarian untuk menyaring teks pesan, nama host, dan modul secara instan.
- **Detail Metadata JSON Interaktif**: Mengklik baris mana pun pada tabel akan membuka kartu detail metadata atribut terstruktur (`duration_ms`, `port`, `drift_rules`, rincian error) dalam format JSON yang rapi.
- **Ekspor Riwayat Log**: Klik **Export** untuk langsung mengunduh rekaman peristiwa yang ada di buffer ke format berkas teks mentah `.log` atau format `.json` untuk kebutuhan audit, debugging, atau pelaporan insiden.
- **Keamanan & Redaction Filter**: Data kredensial sensitif—termasuk password SSH, private key, token autentikasi, cookie sesi, dan rahasia 2FA—secara deterministik disensor menjadi `[REDACTED]` sebelum dikirimkan ke stream browser.
- **Throttling Performa Ringan**: Saat Activity Console tertutup, operasi perenderan DOM diabaikan 100% untuk menjaga penggunaan CPU dan memori browser tetap minimal.

### Diagnostik Jaringan Agentless & Analisis Hop

Saat menangani kendala konektivitas antar-server, pemblokiran firewall, atau anomali rute data center:

- **Mode Diagnostik**: Eksekusi pengujian non-destruktif langsung dari host jarak jauh:
  - **Ping**: Menguji keterjangkauan host dan roundtrip time via `timeout 14 ping -c 4 -W 2 <target>`.
  - **Traceroute**: Memeriksa hop rute jaringan dengan `traceroute` (diutamakan, hop tampil satu per satu). Jika belum ada di host sumber, Pantau memasangnya lebih dulu (apt, dnf, yum, apk, pacman, zypper; apt berjalan tanpa me-restart service) dan menampilkan output instalasi di viewer mentah sebelum probe dijalankan. Pemasangan membutuhkan user SSH berstatus root atau memiliki `sudo` tanpa password; jika tidak, Pantau beralih ke `tracepath` (dijalankan line-buffered via `stdbuf -oL`) dan mencatat fallback itu di viewer mentah. Jika kedua tool tidak ada dan tidak bisa dipasang, probe berhenti dengan pesan yang menjelaskan paket yang perlu dipasang manual.
  - **Port Probe (TCP)**: Menguji konektivitas port TCP via socket bash native `/dev/tcp/<target>/<port>`, tanpa butuh `nc`, `telnet`, atau `nmap`.
  - **DNS Resolve**: Memverifikasi jalur resolusi DNS dan latensi nama domain via `getent hosts` dan `nslookup`.
- **Analisis Hop Cerdas**: Memetakan hop rute traceroute secara otomatis ke dalam kartu topologi terstruktur:
  - **Klasifikasi Zona**: Mengidentifikasi apakah tiap hop merupakan `Lokal/VPC Privat` (RFC 1918 / CGNAT), `Transit ISP`, atau `Destinasi`.
  - **Deteksi Titik Terhenti (*Blackhole Drop*)**: Menunjukkan persis hop jaringan tempat paket terhenti jika target tidak tercapai.
  - **Peringatan Lonjakan Latensi**: Menandai lonjakan latensi signifikan (>60ms) antar dua hop berurutan.
- **Dua Titik Akses UI**:
  - Buka melalui tab **Network Diagnostics** di Workspace Modal (dengan host sumber terkunci).
  - Buka secara global via tombol **`⚡ NetDiag`** di header navbar atas (dengan pemilih host sumber aktif).
- **Proteksi Konkurensi & Keamanan**: Menggunakan kunci singleflight in-memory per host untuk mencegah kehabisan sesi SSH. Host yang sedang offline secara otomatis dinonaktifkan dari daftar target antar-host.

### Tab Docker: Container, Statistik, Volume & Jaringan

Tab **Docker** pada sebuah host membaca engine Docker secara langsung lewat SSH (hanya baca; user SSH harus boleh menjalankan `docker`). Ada empat tampilan:

- **Container**: semua container, berjalan maupun tidak, dengan image, status, dan port, serta menu `⋮` (Logs, Start, Restart, Stop).
- **Statistik**: hasil `docker stats --no-stream` untuk container yang berjalan, diurutkan menurut CPU: CPU %, memori terpakai / batas, I/O jaringan dan blok, PID, beserta total. Centang **Segarkan otomatis (10 dtk)** agar tampil langsung; tabel diperbarui di tempat dan refresh yang gagal mempertahankan data terakhir. CPU bisa lebih dari 100% pada host multi-core.
- **Volume**: named volume dengan driver, ukuran, mountpoint, dan container yang memakainya. Volume yang tidak di-mount container mana pun ditandai `tidak dipakai`; volume anonim (nama hash yang dibuat otomatis oleh container) ditampilkan setelah volume bernama. Ukuran berasal dari `docker system df -v`; engine yang tidak bisa melaporkannya (versi Docker lama) atau pemindaian yang melebihi 40 detik membuat ukuran tampil `—`.
- **Jaringan**: jaringan dengan driver, scope, subnet, gateway, dan container terhubung (beserta IP saat kursor diarahkan); jaringan buatan pengguna tampil lebih dulu dan `bridge`, `host`, `none` ditandai bawaan.

Tampilan diambil sesuai kebutuhan lewat `GET /api/hosts/{id}/docker/stats`, `/docker/volumes`, dan `/docker/networks`. Jika Docker tidak bisa dibaca (belum terpasang, izin ditolak), tab menampilkan pesan error dari Docker sendiri.

#### Docker Hub: Jelajah, Pull & Cek Pembaruan Image

Sub-tab **Images** punya dua tombol Docker Hub ([ADR-0063](adr/0063-docker-hub-integration.md)). Docker Hub dihubungi oleh **server Pantau** (data publik, tanpa login, cache 5 menit); pull berjalan di **host**, memakai jaringan host dan kuota pull Docker Hub milik host.

- **Docker Hub** membuka pencarian repositori publik (image resmi tampil lebih dulu, beserta jumlah bintang, jumlah pull, dan deskripsi). **Detail** menampilkan repositori: **Tag** (push terbaru lebih dulu, dapat disaring, 50 per halaman dengan **Muat tag lainnya**; platform, ukuran terkompresi, dan usia push per tag) dan **Ringkasan** (README repositori, dirender dengan aman: HTML mentah dibuang, gambar tidak dimuat). Setiap tag punya **Pull** (mengalirkan `docker pull` ke log viewer) dan **Jalankan** (membuka **Add container** dengan image terisi). **Buka di Docker Hub** menaut ke hub.docker.com.
- **Cek pembaruan** membandingkan setiap image lokal dari Docker Hub (`nginx:1.28`, `bitnami/redis:7`, `docker.io/…`) dengan digest yang kini disajikan Docker Hub untuk tag-nya, lalu menambahkan badge di kolom Status: **Ada pembaruan** (tag kini menunjuk image lain; arahkan kursor untuk tanggal push-nya, dan **Pull pembaruan** menariknya), **Terbaru**, **Tidak ada di Docker Hub** (tag tidak ada di sana, mis. hasil build lokal), **Tidak dapat dibandingkan** (engine tidak mencatat digest registry, mis. `docker load`), atau **Cek gagal** (beserta alasannya). Image dari registry lain (ghcr.io, registry privat) tidak diberi badge. Baris ringkasan menampilkan jumlah pembaruan yang tersedia. Cek ini tidak menarik apa pun dan tidak memotong kuota pull; cek berhenti pada penolakan pertama Docker Hub (batas laju, tak terjangkau) agar tidak menghabiskan batas API, dan menanyakan maksimal 100 tag per cek.
- Pembaruan yang sudah di-pull tidak mengubah container yang berjalan: buat ulang container-nya (untuk proyek Compose: **Stacks → Update**).
- Belum didukung: repositori privat dan `docker login`, registry lain, cek terjadwal atau notifikasi. Hasil build lokal yang memakai nama repositori Docker Hub (mis. `nginx:latest`) tampil **Ada pembaruan**, dan **Pull pembaruan** akan menimpa tag lokal itu.

### Command Palette (Ctrl+K)

Tekan `Ctrl+K` (`Cmd+K` di macOS) atau klik ikon pencarian di header untuk melompat ke mana saja tanpa menggulir dashboard:
- **Host**: ketik sebagian nama host, alamat, atau grup (pencocokan longgar, semua kata harus cocok) lalu tekan `Enter` untuk membukanya. Saat kotak pencarian kosong, 5 host terakhir yang dibuka tampil paling atas (disimpan di browser).
- **Aksi host**: tanda panah di baris host (atau `→`) membuka Overview, Terminal, Files, Docker, Security, Packages, Inspect now, Port forward, dan Edit host untuk host itu. `Esc` atau `Backspace` saat kotak kosong kembali ke daftar sebelumnya.
- **Perintah**: Inspect all hosts, Bulk exec, Add host, Import hosts, Settings, Documentation, pindah bagian (Hosts, Docker, Kubernetes, Cloudflare, Ansible, Terraform), Port forwards, dan jika mengetik minimal 2 karakter, **Cari paket & container di semua host** (Fleet Search).
- `Ctrl+K` tidak diambil alih di dalam terminal (di bash itu kill-line) maupun di editor kode.

### Side Panel Global, Manajemen Docker & Kubernetes

Pantau menyediakan bilah navigasi kiri (**Side Panel**) permanen di antara header dan footer:

- **Navigasi & Rel Ikon Ciut (*Icon Rail*)**: Beralih antara **Host** (dashboard armada utama), **Docker** (ringkasan kontainer multi-host), dan **Kubernetes** (pengelolaan klaster k3s/rke2 ringan). Bilah dapat diciutkan menjadi rel ikon ramping via tombol ciut di bagian bawah; status simpanan tersimpan di `localStorage`. Pada layar mobile (<768px), panel beralih menjadi laci *off-canvas* yang dibuka lewat tombol ikon menu di header.
- **Bagian Docker**: Pemilih host dengan indikator status live dan filter pencarian. Memilih host langsung membuka antarmuka manajemen Docker ala Portainer: siklus hidup container, penarikan image, volume, jaringan, stack compose, serta konsol terminal.
- **Bagian Kubernetes (k3s / rke2)**: Menyediakan inspeksi dan operasional control plane klaster secara agentless untuk instalasi Kubernetes ringan.
  - **Pemilih Host Terkelompok Klaster**: Menampilkan klaster dengan ketersediaan tinggi (*high-availability*) sebagai satu entri terkelompok berdasarkan ID klaster dengan jumlah anggota dan status kesehatan gabungan; host mandiri (*standalone*) ditampilkan terpisah. Tombol **Deteksi klaster** memindai host armada dengan konkurensi terbatas (5).
  - **Gateway Host & Failover Mulus**: Permintaan klaster dieksekusi melalui Host Gateway terpilih. Jika host utama tidak dapat dihubungi, Pantau secara otomatis mengalihkan permintaan (*failover*) ke peer server yang sehat, menampilkan notifikasi non-blocking dan lencana Gateway (`Gateway: thor (failover dari odin)`).
  - **Mode Kubernetes**: Kebijakan operasional per-host (`full`, `no-exec`, atau `read-only`):
    - `full`: Akses penuh untuk baca, tulis, ubah skala, rollout, hapus, dan exec ke kontainer.
    - `no-exec`: Menonaktifkan konsol terminal exec interaktif sambil tetap mengizinkan siklus hidup workload dan inspeksi log. *Catatan: ini adalah pembatas tingkat aplikasi; batas keamanan sesungguhnya tetap berada pada izin kubeconfig, sudoers, dan RBAC pada host target.*
    - `read-only`: Melarang exec, pengubahan skala, restart, cordon node, penghapusan, dan penerapan manifest.
  - **Keamanan Aksi & Konfirmasi Berdampak Konkret**: Aksi berbahaya memerlukan konfirmasi dengan penjelasan dampak operasional secara rinci:
    - **Ubah Skala ke 0**: Memperingatkan jumlah pod yang akan dihentikan dan menegaskan bahwa PersistentVolumeClaims (PVC) StatefulSet tetap dipertahankan.
    - **Restart Rollout**: Mengonfirmasi pembuatan ulang replika secara bertahap (*rolling update*).
    - **Hapus Pod**: Menampilkan node target dan memperingatkan bahwa pod yang tidak dikelola oleh controller (Deployment/StatefulSet/DaemonSet) akan hilang permanen.
    - **Cordon & Drain Node**: Cordon menjaga workload tetap berjalan sambil memblokir pod baru. Node **Drain** menyediakan alur dua langkah: pratinjau dry-run server menampilkan pod terevakuasi dan hambatan PodDisruptionBudget, opsi flag (abaikan DaemonSet, hapus data emptyDir, evict pod mandiri), peringatan kuorum etcd/HA untuk control-plane, dan tombol cepat **Uncordon** pada notifikasi toast.
  - **Tab Telemetri Klaster**:
    - **Node**: Inventaris node dengan pemetaan ke Host Pantau, status Ready, peran, metrik pemakaian CPU dan RAM dengan progress bar (`dockerBar`), kapasitas allocatable, aksi drain, serta cordon/describe.
    - **Workload**: Deployment, StatefulSet, dan DaemonSet dengan pelacakan replika, pengubahan skala (0–1000), restart rollout, streaming teragregasi **Log Workload** berawalan kontainer, menu baris **Edit YAML** (manifest bersih, resourceVersion dipertahankan, dan diff gate), serta dialog describe.
    - **Pod**: Status pod lintas namespace dengan pod bermasalah diprioritaskan di atas dan diberi penanda visual, filter **Hanya bermasalah**, hitungan restart, konsol **Exec** di Terminal Dock, streaming **Log** langsung dengan kotak cari/filter teks klien, penghitung kecocokan, toggle **Timestamp**, riwayat kontainer lama, dan penghapusan pod.
    - **Service & Ingress**: Definisi service, IP klaster/eksternal, port, host ingress, target backend, menu **Edit YAML**, dan dialog describe.
    - **Konfigurasi & Penyimpanan**: ConfigMap, Secret (tanpa akses intip rahasia dan tanpa Edit YAML), PersistentVolumeClaims (PVC) dengan **Edit YAML**, dan PersistentVolumes (PV).
    - **Batch Job**: Job dan CronJob dengan pemantauan penyelesaian, jadwal, status penangguhan, menu **Edit YAML**, dan inspeksi describe.
    - **Event**: Catatan event informasional dan peringatan dengan filter khusus **Hanya Warning**.
    - **Auto-refresh Opsional**: Pembaruan data otomatis per-tab (Mati / 10 dtk / 30 dtk / 60 dtk) yang berhenti saat tab browser tidak aktif, saat berpindah bagian/tab, atau saat modal terbuka, dan tidak menumpuk permintaan in-flight.
    - **Terapkan Manifest & Pratinjau Diff**: Editor YAML terintegrasi dengan kewajiban menjalankan **Pratinjau Diff** (`kc diff` dry-run terhadap status klaster aktif). Tombol **Terapkan** terkunci hingga isi manifest saat ini telah selesai ditinjau perubahannya, dan mutasi meminta konfirmasi sebelum diterapkan.

#### Pengelolaan Klaster Kubernetes Eksternal (EKS, GKE, AKS & Kubeconfig Mandiri)

Pantau memungkinkan pemantauan dan pengelolaan klaster Kubernetes eksternal atau terkelola di cloud (AWS EKS, Google Cloud GKE, Azure AKS, maupun klaster mandiri) tanpa memerlukan akses SSH ke node control-plane. Untuk klaster eksternal, perintah dieksekusi secara lokal menggunakan `kubectl` langsung pada server Pantau.

##### Menambahkan Klaster Eksternal
1. Pada **Side Panel**, buka bagian **Kubernetes** dan klik tombol **Tambah Klaster**.
2. **Tempel atau Unggah Kubeconfig**: Masukkan berkas kubeconfig Anda (ukuran maksimum 128 KB).
3. **Pilih Konteks**: Pantau membaca daftar konteks yang tersedia di berkas; pilih konteks klaster target.
4. **Beri Nama & Pilih Mode**: Masukkan nama klaster yang mudah dikenali dan tentukan **Mode Kubernetes** (`full`, `no-exec`, atau `read-only`).
5. **Uji & Simpan**: Pantau menormalisasi konfigurasi, menguji konektivitas via `kubectl get ns kube-system`, menyimpan data klaster, dan mengambil telemetri awal. Jika verifikasi koneksi gagal, klaster ditolak dan tidak akan disimpan.

##### Autentikasi Penyedia Cloud & Plugin CLI
Saat terhubung ke klaster cloud terkelola, Pantau memanfaatkan plugin kredensial bawaan `kubectl` yang dijalankan di server Pantau:
- **AWS EKS**: Membutuhkan CLI `aws` atau `aws-iam-authenticator` pada `PATH` server Pantau. Kredensial AWS dibaca dari variabel lingkungan proses Pantau (seperti `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION`, atau berkas `~/.aws`).
- **Google Cloud GKE**: Membutuhkan `gke-gcloud-auth-plugin` atau `gcloud` pada `PATH`. Kredensial dibaca dari `GOOGLE_APPLICATION_CREDENTIALS` atau `~/.config/gcloud`.
- **Azure AKS**: Membutuhkan `kubelogin` atau `az` pada `PATH`.

> [!TIP]
> **Praktik Terbaik Keamanan & RBAC**:
> - Gunakan token berumur pendek (*short-lived*) atau plugin kredensial `exec` dibanding kredensial statis.
> - Untuk kebutuhan pemantauan read-only, buat `ServiceAccount` khusus di klaster dengan hak akses minimal via `ClusterRole` RBAC (`get`, `list`, `watch` pada resource standar) lalu ekspor kubeconfig berbasis token.
> - Pada penyebaran Docker, pasang (*mount*) direktori kredensial cloud secara read-only ke kontainer Pantau. Contoh:
>   ```yaml
>   volumes:
>     - ./data:/app/data
>     - ${HOME}/.aws:/root/.aws:ro
>   ```

##### Model Keamanan & Batasan Validasi
- **Aturan Kredensial**: Untuk melindungi server Pantau dari serangan pembacaan berkas sembarang (*arbitrary file read*), Pantau menolak kubeconfig yang memuat kredensial berbasis jalur berkas lokal (`certificate-authority`, `client-certificate`, `client-key`, `tokenFile`). Kredensial harus berupa data inline (`*-data` dalam base64) atau token langsung.
- **Daftar Putih Plugin `exec`**: Hanya perkakas CLI terdaftar yang diizinkan pada blok `exec`: `aws`, `aws-iam-authenticator`, `gke-gcloud-auth-plugin`, `gcloud`, `kubelogin`, `az`, `doctl`, dan `oci`. Penggunaan pemisah jalur (`/` atau `\`) serta konfigurasi `auth-provider` berbasis perintah ditolak.
- **Tanpa Paparan API**: Kubeconfig disimpan di database lokal seperti kunci privat SSH, dicadangkan dalam System Snapshot terenkripsi, dan tidak pernah diekspos melalui respons REST API.

##### Batasan Operasional
- **Pod Exec Interaktif Tidak Didukung**: Konsol interaktif Terminal Dock mengandalkan koneksi SSH PTY ke host. Karena klaster eksternal berjalan via `kubectl` lokal tanpa SSH, fitur exec kontainer dinonaktifkan dan disembunyikan pada UI.
- **Streaming Log WebSocket Didukung**: Aliran log pod dan workload secara real-time (`/ws/kube/logs`) tetap berfungsi penuh.
- **Endpoint Tunggal**: Klaster eksternal tidak memiliki fitur failover antar-host.

##### Pemecahan Masalah (*Troubleshooting*)
- **`kubectl not found`**: Pastikan biner `kubectl` terpasang dan berada pada `PATH` proses server Pantau (image Docker resmi Pantau telah memuat `kubectl`).
- **`plugin not allowed`**: Perintah `exec` pada kubeconfig tidak termasuk dalam daftar putih yang diizinkan atau menggunakan direktori path. Gunakan plugin yang didukung (`aws`, `kubelogin`, dll.).
- **`file-path credential rejected`**: Kubeconfig merujuk ke path berkas di mesin lokal Anda (misal `certificate-authority: /path/to/ca.crt`). Gabungkan menjadi inline data sebelum menempel menggunakan perintah `kubectl config view --raw --flatten`.
- **`cluster verification failed`**: Server Pantau tidak dapat melakukan autentikasi atau gagal menghubungi API server klaster. Periksa akses jaringan keluar, firewall, dan ketersediaan kredensial cloud di lingkungan proses Pantau.

### Pengelolaan Cloudflare via REST API v4

Pantau memungkinkan operator mengelola multi-akun Cloudflare, memeriksa status domain (zone), mengelola DNS record, membersihkan cache, dan mengubah pengaturan keamanan zone secara langsung dari antarmuka web tanpa berpindah-pindah tab dasbor eksternal atau menjalankan script manual.

#### Menghubungkan Akun Cloudflare

1. Pada **Side Panel** atau navigasi utama, klik menu **Cloudflare**.
2. Klik tombol **Tambah Akun**.
3. Masukkan nama akun yang deskriptif (misal `Cloudflare Produksi`, `Domain Pribadi`), **Account ID** (opsional, hanya jika menggunakan token tingkat akun), serta **API Token**.
4. Pantau akan memverifikasi token terlebih dahulu ke endpoint Cloudflare (`/user/tokens/verify` atau `/accounts/{id}/tokens/verify`) sebelum menyimpannya ke database SQLite lokal.

> [!IMPORTANT]
> **Rekomendasi Hak Akses Minimal (*Least Privilege Token Scopes*)**:
> Hindari penggunaan Global API Key. Pada dashboard Cloudflare di bawah **My Profile → API Tokens**, buat API Token kustom dengan izin minimal pada zone target:
> - `Zone - Zone - Read`: membaca daftar zone dan informasi statusnya.
> - `Zone - DNS - Edit`: melihat, menambah, mengubah, dan menghapus entri DNS.
> - `Zone - Cache Purge - Purge`: melakukan purge cache seluruh zone atau URL tertentu.
> - `Zone - Zone Settings - Edit`: mengatur konfigurasi SSL, pengalihan HTTPS, tingkat keamanan, dan versi TLS.

#### Manajemen Zone & Record DNS

- **Ringkasan Zone**: Pilih akun untuk menampilkan daftar domain yang dikelola, status (`active`, `pending`), paket langganan (*plan*), status *paused*, serta daftar authoritative name server. Gunakan bilah pencarian cepat untuk memfilter zone.
- **Tabel DNS Records**: Menampilkan seluruh record DNS beserta nama host, jenis tipe (`A`, `AAAA`, `CNAME`, `TXT`, `MX`, `NS`, `SRV`, `CAA`), nilai konten, dan nilai TTL (Auto atau nilai detik).
- **Tambah & Edit Record**: Dialog formulir memvalidasi batasan nama dan konten (maksimal 255/2048 karakter), mewajibkan prioritas untuk record MX, serta menyediakan sakelar proxy Cloudflare (*orange cloud*).
- **Konfirmasi Hapus**: Penghapusan record DNS dilindungi dialog konfirmasi untuk mencegah hilangnya record secara tidak disengaja.

#### Pembersihan Cache (Purge Cache)

Tab **Cache** menyediakan dua opsi pembersihan:
1. **Purge Everything**: Menghapus seluruh cache edge Cloudflare pada zone terkait. Aksi ini dilindungi dialog konfirmasi karena dapat meningkatkan lonjakan beban sesaat pada server origin.
2. **Purge by URL**: Masukkan daftar URL spesifik baris demi baris (maksimal 30 berkas per permintaan sesuai ambang batas API Cloudflare) untuk membuang aset tertentu tanpa mengosongkan seluruh cache.

#### Daftar Putih Pengaturan Zone (*Zone Settings*)

Tab **Settings** menyajikan pengaturan terpilih dari daftar izin (*allowlist*):
- **Enkripsi SSL / TLS**: `off`, `flexible`, `full`, `strict`.
- **Always Use HTTPS**: mengalihkan seluruh lalu lintas HTTP ke HTTPS (`on` / `off`).
- **Security Level**: mengatur tingkat tantangan proteksi bot/DDoS (`off`, `essentially_off`, `low`, `medium`, `high`, `under_attack`).
- **Development Mode**: melewati cache Cloudflare selama 3 jam untuk keperluan uji coba origin secara live (`on` / `off`).
- **Minimum TLS Version**: membatasi versi minimal protokol TLS (`1.0`, `1.1`, `1.2`, `1.3`).
- **Automatic HTTPS Rewrites**: memperbaiki tautan konten campuran (*mixed-content*) secara otomatis (`on` / `off`).

#### Model Keamanan & Non-Goals

- **Perlindungan Token**: API Token disimpan di basis data SQLite lokal setara dengan kunci privat SSH, tidak pernah diekspos melalui respons REST API (`json:"-"`), disensor dari logging, serta terenkripsi di dalam **System Snapshot** (AES-256-GCM).
- **Pencegahan SSRF & Path Injection**: Panggilan keluar dipatok tetap ke `https://api.cloudflare.com/client/v4`, dan identifier URL divalidasi dengan regex heksadesimal 32 karakter `^[0-9a-f]{32}$`.
- **Batas Non-Goals**: Cloudflare Workers, R2 Storage, Cloudflare Tunnels (`cloudflared`), aturan WAF/Firewall kustom, dan Page Rules berada di luar cakupan Pantau dan tetap dikelola melalui dasbor resmi Cloudflare.

### Ansible Playbooks (Gaya Semaphore, Agentless)

Pantau menyediakan subsistem otomatisasi Ansible terintegrasi yang terinspirasi dari Semaphore UI. Administrator dapat menyimpan Ansible Playbook langsung di basis data lokal SQLite Pantau, mengeksekusinya terhadap sekumpulan Host atau Host Group, memantau aliran log output tugas secara langsung, dan meninjau riwayat eksekusi—**tanpa memerlukan berkas inventori terpisah atau brankas kunci eksternal**.

Bagian **Ansible** memiliki lima tab: **Playbook** (tabel padat dengan pencarian, filter, pengurutan, pengelompokan, dan menu per baris; playbook dibuka di side drawer dengan Overview, Edit, Runs, dan Schedule), **Runs** (History semua run dengan filter status, dan Scheduled), **Hosts** (explorer Group > Host dengan Ping dan Facts, plus Ad-hoc), **Sumber Daya** (Templates, Files, dan Library), dan **Pengaturan** (Ansible Gate, `ansible.cfg`). Status gate selalu terlihat di header; klik status untuk membuka Pengaturan. Keputusan terkait: [ADR 0049](adr/0049-ansible-playbooks-semaphore-style.md), [ADR 0050](adr/0050-ansible-git-playbooks-and-cron-schedules.md), [ADR 0057](adr/0057-ansible-ops-recap-tasktree-diff-survey-adhoc-facts-explorer.md), dan tata letak di [ADR 0059](adr/0059-ansible-ui-playbook-table-and-side-drawer.md).

#### Ansible Gate (Keamanan & Aktivasi)

Ansible Playbook dijalankan **langsung pada server Pantau dengan hak akses proses Pantau**. Karena fitur bawaan Ansible seperti `connection: local`, `delegate_to: localhost`, `local_action`, dan modul lookup mengeksekusi perintah atau membaca berkas di mesin master controller, playbook tidak dapat disandbox secara sempurna.

Untuk mencegah eksekusi yang tidak diinginkan:
1. **Ansible Gate** (`ansible_enabled`) berada dalam kondisi **nonaktif secara default**.
2. Sebelum playbook apa pun dapat dieksekusi, administrator harus mengaktifkan gate dengan tombol **Aktifkan Gate** (tampil di setiap tab Ansible saat gate nonaktif, dan di **Ansible → Pengaturan**). Sampai saat itu hanya **Pengaturan** yang dapat dipakai.
3. Pengaktifan gerbang mewajibkan konfirmasi dialog keamanan eksplisit yang menegaskan bahwa sesi admin Pantau dengan Ansible aktif setara dengan akses eksekusi di server master.
4. Sistem juga memverifikasi keberadaan biner `ansible-playbook` di `PATH` server Pantau.

#### Menulis & Menyimpan Playbook

1. Pada **Side Panel**, pilih **Ansible → Playbook** lalu klik tombol **Playbook Baru**.
2. Masukkan **Nama** yang deskriptif (1–64 karakter, unik *case-insensitive*) dan **Deskripsi** opsional.
3. (Opsional) Masukkan **Grup** (`group_name`, teks bebas hingga 64 karakter; saran muncul dari grup playbook yang sudah ada). Kosongkan untuk playbook tanpa grup (*ungrouped*).
4. Tulis isi playbook YAML (hingga 256 KiB) menggunakan editor CodeMirror mode YAML yang terintegrasi.
5. (Opsional) Masukkan nilai **Extra Vars** bawaan dalam format objek JSON yang valid (hingga 64 KiB).
6. Simpan playbook. Definisi playbook disimpan di SQLite dan otomatis diikutsertakan dalam cadangan **System Snapshot** terenkripsi.

#### Pengelompokan & Organisasi Playbook

Playbook dapat dikelompokkan ke dalam grup visual yang dimodelkan mengikuti Host Group:
- **Mengatur Grup**: Masukkan nama grup (dipangkas, hingga 64 karakter) pada editor playbook. Grup bersifat implisit—dibuat otomatis saat digunakan dan hilang jika tidak ada playbook yang memakainya.
- **Group by**: Pada tabel Playbook, saklar **Group by** menampilkan baris di bawah header grup yang dapat dilipat lengkap dengan jumlah playbook. Klik header untuk melipat/membuka; status lipatan tersimpan di browser.
- **Pemfilteran**: Gunakan kotak pencarian (tekan `/` untuk fokus) dan chip **All / Failed / Never run / Scheduled / Git** untuk menyaring tabel; kolom dapat diurutkan dengan klik.
- **Label Kosmetik Murni**: Grup hanyalah label pengorganisasian visual; grup tidak memengaruhi eksekusi run, pembentukan inventory, kredensial, maupun izin akses.
#### Menjalankan Playbook

Klik **Run** pada baris playbook, atau di drawer-nya, untuk membuka panel peluncuran. Panel mengingat target dan opsi terakhir per playbook, dan **Ctrl+Enter** memulai run:

1. **Pemilihan Target**:
   - **Host Spesifik**: Pilih host individual dari daftar pencarian host.
   - **Host Group**: Pilih satu atau beberapa grup via tombol chip. Target eksekusi dihitung secara otomatis sebagai gabungan (*union*) dari host individual terpilih dan seluruh host anggota grup yang dipilih.
   - Maksimal 200 target host didukung dalam satu proses eksekusi run.
2. **Pilihan Mode Eksekusi**:
   - **Run (`run`)**: Eksekusi playbook normal terhadap target.
   - **Check (`check`)**: Mode simulasi *dry-run* menggunakan opsi `--check` (tanpa menerapkan perubahan nyata pada host).
   - **Diff (`diff`)**: Pratinjau perbandingan perubahan menggunakan opsi `--check --diff`.
   - **Syntax (`syntax`)**: Pemeriksaan sintaksis dan struktur YAML menggunakan `--syntax-check` (dapat dijalankan dengan 0 target host).
3. **Parameter Eksekusi**:
   - **Extra Vars**: Timpa nilai variabel JSON bawaan khusus untuk run ini.
   - **Tags & Skip Tags**: Filter tag dipisahkan koma yang mematuhi regex `^[A-Za-z0-9_.,:-]{1,256}$`.
   - **Limit**: Pola host Ansible opsional (`--limit`) yang mempersempit run ke sebagian target terpilih, mis. `web01,db03` atau `&web:!web02`. Karakter yang diizinkan: huruf, angka, `_ . , : ! & * [ ] -` (1–256 karakter); spasi, `;`, `$`, dan backtick ditolak, dan hanya nama Generated Inventory (nama Host tersanitasi dan Host Group) yang dapat dicocokkan. Kosong berarti tanpa batas.
   - **Verbosity**: `0` (normal), `1` (`-v`), `2` (`-vv`), `3` (`-vvv`), atau `4` (`-vvvv`). Tingkat lebih tinggi menambah hasil per task dan debug koneksi, sekaligus memperpanjang log.
   - **Penyajian**: Run mode `run` mencetak keluaran berwarna ANSI; log menyimpan kode escape mentah dan penampil mewarnainya. Saklar **Log | Task Tree | Recap | Diff** di atas penampil run memungkinkan membaca run yang sama dengan tiga cara lain (lihat di bawah).
   - **Forks**: Jumlah koneksi host simultan (1–20, bawaan 5).
   - **Become (Sudo)**: Aktifkan saklar `--become` untuk eskalasi hak akses sudo tanpa kata sandi.

#### Generated Inventory & Lingkungan Anak yang Steril

Saat eksekusi dimulai, Pantau melakukan langkah-langkah otomatis berikut:
1. **Inventory JSON Dinamis**: Membangun berkas sementara `inventory.json` dengan struktur grup Ansible `all.hosts` dan `all.children.<group>.hosts`. Variabel koneksi seperti `ansible_host`, `ansible_port`, `ansible_user`, dan `ansible_python_interpreter=auto_silent` disuntikkan secara otomatis.
2. **Isolasi Berkas Kunci**: Mengambil kunci privat SSH efektif tiap target host dari **SSH Key Library** atau custom key host, menulisnya ke berkas berizin `0600` di dalam direktori kerja sementara `0700` (`os.MkdirTemp`), lalu merujuknya via `ansible_ssh_private_key_file`.
3. **Lingkungan Steril**: Proses anak `ansible-playbook` hanya mewariskan variabel penting (`PATH`, `LANG=C.UTF-8`, `HOME=<workspace>`, proxy, dan `ANSIBLE_*`). Berkas `ansible.cfg` sementara menonaktifkan pemeriksaan host key (`host_key_checking = False`), mencegah pemuatan konfigurasi tak terduga dari `/etc/ansible` atau `~/.ansible.cfg`, dan menjalankan `ssh` dengan `-F /dev/null` sehingga `~/.ssh/config` yang di-mount (umum di Docker) tidak dapat merusak koneksi (`Bad owner or permissions on /root/.ssh/config`).
4. **Pembersihan Workspace Terjamin**: Seluruh direktori kerja sementara—termasuk berkas kunci privat dan inventori—selalu dihapus seketika seusai proses selesai, dibatalkan, atau mengalami kegagalan/crash.

#### Playbook dari Repositori Git

Ubah **Sumber** menjadi **Repositori Git** di editor playbook alih-alih menulis YAML inline:

| Field | Keterangan |
|---|---|
| URL Repositori | `https://host/org/repo.git`, `ssh://git@host:port/org/repo.git` atau `git@host:org/repo.git`. Kredensial di dalam URL, `file://`, `git://`, `http://`, dan jalur lokal ditolak. |
| Branch atau tag | Opsional; kosong memakai branch bawaan remote. |
| Path playbook | Path relatif ke berkas `.yml`/`.yaml` di repositori, mis. `playbooks/site.yml`. Symlink yang keluar dari repositori ditolak. |
| Autentikasi | **Tanpa** (repo publik); **Access token** (hanya `https://`, mis. personal access token GitHub/GitLab dengan akses baca); **Kunci SSH dari library** (hanya `ssh://` / `user@host:path`, kunci dari *SSH Key Library*). |

Pada setiap run Pantau meng-clone dangkal (`--depth 1`) repositori ke workspace run yang privat, menjalankan `ansible-playbook` dari dalam clone (sehingga `roles/`, `group_vars/`, `host_vars/`, dan `files/` di samping playbook berfungsi), lalu menghapusnya. `ansible.cfg` milik repositori diabaikan. **Commit** yang di-checkout dicetak di awal log (`[pantau] git commit <sha>`) dan ditampilkan di riwayat run. Catatan:

- Access token bersifat write-only: disimpan seperti kunci SSH, tidak pernah dikembalikan API, tidak pernah ada di argumen perintah maupun log, dan mengosongkan field saat mengubah berarti mempertahankan token lama.
- `git` harus terpasang di server Pantau (sudah ada di image Docker). Editor memberi peringatan bila tidak ada.
- Submodule dan requirement `ansible-galaxy` tidak diambil; simpan role di dalam repositori atau pasang di tab **Library**.
- Menghapus kunci SSH yang dipakai sebuah playbook tidak dicegah; run berikutnya gagal dengan pesan yang jelas.
- Plugin repositori (`library/`, `action_plugins/`, `lookup`) berjalan di server Pantau seperti isi playbook lainnya. Arahkan Pantau hanya ke repositori yang Anda percaya.

#### Impor Playbook dari Folder

Gunakan **Ansible → Playbook → Impor Folder** untuk menyalin berkas YAML playbook dari proyek lokal ke Pantau. Playbook hasil impor adalah playbook inline biasa: Pantau menyimpan teksnya, Anda mengeditnya di Pantau, dan tidak ada bagian folder yang disimpan atau ditautkan.

1. Pilih folder di browser (bukan path di server Pantau). `.git`, `__pycache__`, `.DS_Store`, `.ansible/`, dan `collections/` dilewati.
2. Pantau menampilkan daftar YAML yang ditemukan dan **memilih otomatis yang merupakan playbook** (`hosts:` atau `import_playbook:` di level atas), seperti pratinjau impor Host. Centang playbook yang diinginkan (tersedia pilih semua/tidak ada) dan sesuaikan namanya; nama harus unik. Setiap berkas berukuran 1 byte–256 KiB; maksimal 100 playbook per impor.
3. Klik **Impor Folder**. Bila ada nama yang sudah dipakai, tidak ada yang diimpor. Buka playbook baru untuk mengeditnya dan mengisi variabelnya, lalu mulai dengan **Syntax Check** sebelum run pertama. Pola `hosts:` playbook harus cocok dengan alias dan Host Group pada Generated Inventory Pantau.

YAML disimpan di SQLite (ikut **System Snapshot** terenkripsi): periksa dari rahasia terlebih dahulu. Hanya berkas yang dicentang yang disalin: berkas yang dirujuk playbook di sebelahnya (template, `vars_files`, `include_tasks`, target `import_playbook`) **tidak** diimpor, jadi impor target tersebut sebagai playbook tersendiri atau tempel isinya.

**Inventory, role, koleksi, dan `ansible.cfg` selalu berasal dari Pantau, bukan dari folder**: Pantau menghasilkan inventory dan kunci SSH sendiri, **Library** menyediakan role dan koleksi, dan pengaturan `ansible.cfg` seperti `forks`, `pipelining`, `interpreter_python`, atau `become_method` diatur di **Ansible → Pengaturan → Konfigurasi Ansible**. Path absolut ke kunci Git, berkas di luar playbook, dan executable yang hanya ada di laptop tidak tersedia di server Pantau. Hanya impor playbook tepercaya: playbook dan plugin lokal tetap dapat mengeksekusi kode dengan hak proses Pantau.

#### Konfigurasi Ansible (`ansible.cfg`)

**Ansible → Pengaturan → Konfigurasi Ansible** berisi tambahan `ansible.cfg` global yang diterapkan ke setiap run, berupa teks INI biasa:

```ini
[defaults]
forks = 10
interpreter_python = auto_silent

[ssh_connection]
pipelining = True
```

Bagian yang diizinkan: `defaults`, `ssh_connection`, `privilege_escalation`; satu `key = value` per baris, tanpa nilai multibaris. Kunci yang dikelola Pantau ditolak beserta nomor barisnya: `inventory`, `private_key_file`, `ssh_args`, `host_key_checking`, `retry_files_enabled`, `nocows`, `roles_path`, `collections_path(s)`, prompt password interaktif (`ask_pass`, `ask_vault_pass`, `become_ask_pass`) dan kunci berkas password. Bila run tidak menetapkan forks secara eksplisit, `forks` dari konfigurasi ini berlaku (default Ansible: 5). Konfigurasi ikut System Snapshot.

#### Library: Role dan Koleksi

Sub-tab **Library** di **Ansible → Resources** mengelola role dan koleksi yang tersimpan di server Pantau dan tersedia untuk **semua** playbook (inline, Git, atau folder impor), sehingga proyek tidak perlu lagi menyertakannya:

- **Pasang dari Galaxy**: pilih Koleksi atau Role, isi `namespace.nama` (role: nama di Galaxy, mis. `geerlingguy.nginx`) dan versi opsional. Koleksi memasang dependensinya. Output instalasi mengalir langsung ke panel di bawah form selama berjalan (baris perintah lebih dulu, lalu baris `… still working (Ns)` tiap 5 detik saat `ansible-galaxy` diam); prosesnya bisa memakan beberapa menit. Instalasi tetap selesai di server walau tab ditutup.
- **Pasang dari requirements.yml**: tempel `requirements.yml` (`collections:` dan/atau `roles:`). Hanya nama dan versi yang diterima; sumber Git/URL ditolak.
- **Impor Lokal**: unggah folder koleksi (dengan `galaxy.yml` di root), tarball koleksi (`.tar.gz`, mis. dari `ansible-galaxy collection build`/`download`), atau folder role (beri nama). Pemasangan lokal tidak mengambil dependensi sehingga berfungsi offline.
- Gunakan ikon tempat sampah untuk menghapus item.

Library berada di `ansible-library/` di samping database Pantau (di Docker: simpan di volume yang sama). Library **tidak** ikut System Snapshot: setelah restore ke mesin baru, pasang ulang item yang dibutuhkan. Fitur ini membutuhkan `ansible-galaxy` (terpasang bersama ansible-core); tab memberi tahu bila tidak ada. Pasang hanya konten yang Anda percaya: plugin koleksi dan role berjalan di server Pantau dengan hak prosesnya.

#### Template dan Berkas (untuk `template:` dan `copy:`)

Playbook inline hanyalah satu teks YAML, sehingga task seperti `template: src=site.conf.j2` atau `copy: src=ca.pem` tidak menemukan berkas untuk dibaca. Sub-tab **Templates** dan **Files** di **Ansible → Resources** menyimpan berkas pendukung itu di Pantau:

- **Templates**: ketik nama (boleh bersubfolder, mis. `nginx/site.conf.j2`) dan isi Jinja, lalu **Simpan template**. Menyimpan nama yang sudah ada akan menggantinya; ikon pensil memuat template kembali ke editor. Template harus teks UTF-8, maksimal 256 KiB.
- **Files**: pilih satu atau beberapa berkas (jenis apa pun, termasuk biner), opsional **Subfolder** seperti `certs/`, lalu **Unggah**. Mengunggah nama yang sudah ada akan menggantinya. Maksimal 5 MiB per berkas.
- Pakai dari playbook persis seperti proyek Ansible biasa:

  ```yaml
  - ansible.builtin.template:
      src: nginx/site.conf.j2     # tab Templates
      dest: /etc/nginx/conf.d/site.conf
  - ansible.builtin.copy:
      src: certs/ca.pem           # tab Files (subfolder certs/)
      dest: /etc/ssl/certs/ca.pem
      mode: "0644"
  ```

  Variabel template berasal dari `vars:` playbook, extra vars bawaan, atau extra vars saat run.
- Setiap run playbook **inline** (manual maupun terjadwal) menerima semua template dan berkas tersimpan di workspace sementaranya. Isinya dipakai bersama oleh semua playbook inline; menghapus satu item membuat playbook yang merujuknya gagal. Playbook dari **Git** memakai folder `templates/` dan `files/` di repositorinya sendiri.
- Batas: total 32 MiB. Template dan berkas disimpan di basis data dan ikut System Snapshot terenkripsi, jadi jangan unggah rahasia yang tidak ingin Anda simpan di cadangan. Berkas yang diunggah tidak dapat diunduh kembali.
- Jinja tidak diperiksa saat menyimpan; kesalahan muncul di log run. Gunakan mode **Check** atau **Diff** untuk mencoba template tanpa mengubah host.

#### Mengedit Playbook dan Variabel

Klik baris playbook (atau pilih **Edit** di menu `⋯`-nya) untuk membuka side drawer. Tab-nya: **Overview**, **Edit**, **Runs**, dan **Schedule**; tab **Edit** berisi editor:

- Tab **Pre-tasks / Roles / Tasks / Post-tasks / Handlers / Raw** (playbook inline dengan tepat satu play): playbook yang sama dipecah menjadi bagian, sesuai urutan Ansible menjalankannya. **Pre-tasks**, **Tasks**, **Post-tasks**, dan **Handlers** berupa daftar YAML, **Roles** adalah pemilih dari role yang terpasang di Library (role Library, role dari koleksi Library sebagai `namespace.collection.role`, dan role sistem) dengan parameter per role (`vars`, `tags`, `when`) dan pengurutan, dan **Raw** adalah YAML lengkap. Name, hosts, become, vars, dan key play lainnya tidak punya tab: edit di Raw, bagian membawanya apa adanya. Bagian digabung ke teks raw saat Anda meninggalkan tab bagian ke tab non-bagian atau Simpan, dan teks raw dipecah lagi saat Anda membuka tab bagian setelah mengedit Raw. Komentar dan tanda kutip di dalam tiap bagian dipertahankan, tetapi penggabungan merapikan indentasi YAML, jadi edit bagian hanya bila Anda memang mau itu. Bagian yang bukan YAML valid (mis. Tasks yang bukan daftar) menahan Anda di tab itu beserta alasannya. Playbook dengan beberapa play, atau YAML yang tidak bisa di-parse, hanya menyediakan **Raw**; role yang belum terpasang ditandai *belum terpasang* dan run gagal kecuali role itu ada.
- Tab **Variabel**: variabel yang dirujuk playbook, dideteksi dengan memindai YAML saat Anda mengetik (termasuk edit yang belum disimpan) beserta role yang dipakainya: role di repositori, serta role yang terpasang di **Resources → Library** atau path role sistem (`/usr/share/ansible/roles`, `/etc/ansible/roles`), termasuk role `namespace.collection.role` dari koleksi Library. `defaults/`, `vars/`, task, handler, template, dan dependensi `meta` role ikut dibaca, sehingga variabel yang didefinisikan role sendiri (`ansistrano_repo_dir`) atau diisi role saat berjalan (`set_fact`/`register`, mis. `ansistrano_release_path`) tidak perlu Anda isi. **Wajib** = dipakai tetapi tidak didefinisikan di mana pun (perlu nilai dari Anda atau dari inventory); **Opsional** = hanya dipakai dengan `default()`/`is defined`; **Terdefinisi** = punya nilai di YAML (`vars:`), di role, atau di extra vars default playbook. Variabel yang dibuat saat run dan magic variable Ansible dilewati. Ini heuristik statis: nama yang dibangun dinamis dan variabel dari inventory Anda sendiri bisa terlewat. File task yang di-include role lewat nama bertemplate (`include_tasks: "{{ mode }}.yml"`) hanya diambil fakta yang diset-nya; rujukannya diabaikan karena file yang berjalan tidak diketahui.
  - Kolom **Nilai** adalah **extra vars default** playbook: ketik nilai untuk menetapkan atau menimpa variabel, klik ikon tempat sampah untuk menghapusnya (variabel kembali ke apa yang didefinisikan YAML). **Tambah variabel** membuat variabel yang tidak terdeteksi pemindaian. Nilai di-parse sebagai JSON bila memungkinkan (`8080` angka, `true` boolean, `["a","b"]` daftar); bungkus dengan tanda kutip (`"8080"`) untuk memaksa string. Variabel wajib tanpa nilai disorot.
  - Variabel yang didefinisikan di blok `vars:` YAML (belum ada override) juga punya ikon tempat sampah: ikon itu menghapus barisnya dari YAML di editor (batalkan dengan Cancel). Hanya blok `vars:` biasa yang dikenali; selain itu ubah langsung di tab Raw. Variabel yang hanya dirujuk tanpa definisi tidak bisa dihapus (ikon nonaktif).
  - Perubahan disimpan dengan **Simpan**, seperti bagian editor lainnya. Run masih dapat menimpa semuanya di dialog peluncuran.
  - **Simpan mempertahankan posisi Anda**: tab drawer, tab editor, dan editor yang sama. Untuk playbook baru, Simpan pertama mengubah drawer menjadi tab Edit playbook itu, di tab editor yang sama.
  - **Simpan memeriksa dan memformat YAML inline lebih dulu**: playbook harus berupa daftar play, tiap play punya `hosts` atau `import_playbook`, tanpa key ganda dalam satu mapping, lalu di-indentasi ulang menjadi 2 spasi (komentar dan tanda kutip dipertahankan; spasi sebelum komentar `#` di akhir baris dinormalkan menjadi satu spasi). Bila pemeriksaan gagal, tidak ada yang disimpan, editor pindah ke **Raw**, dan footer menampilkan alasannya beserta nomor baris. Pemeriksaan hanya untuk struktur YAML, tidak menjalankan `ansible-playbook --syntax-check`, jadi modul yang tidak dikenal atau role yang hilang baru terlihat saat run (pakai mode *syntax* di panel peluncuran untuk itu).
  - Variabel ditampilkan di bawah **Variabel playbook** dan satu bagian per role yang bisa dilipat (klik header untuk melipat; default role itu sendiri dan variabel yang hanya dipakai berkas role tersebut). Kotak pencarian di atas tabel menyaring berdasarkan nama, nilai, sumber, role, atau berkas dan membuka semua grup selama aktif. Tabel memakai seluruh lebar drawer: perlebar drawer untuk melihat kolom Sumber dan Dipakai di berdampingan, drawer sempit menyembunyikannya.
- Bagian **Files** (hanya playbook Git, setelah Simpan pertama): telusuri, edit, buat, dan hapus berkas dari clone dangkal terbaru lalu commit kembali, lihat *Git Repository Editor* di bawah. `.env`, kunci, berkas biner, `.ansible/`, dan `collections/` tidak pernah ditampilkan. Batas: 1.000 berkas, total 4 MiB, 256 KiB per berkas.


#### Jadwal (Cron)

Jadwal ada di dua tempat: tab **Schedule** pada drawer playbook (buat, ubah, jeda, dan hapus jadwal playbook itu, lengkap dengan pratinjau kemunculan berikutnya) dan **Ansible → Runs → Scheduled** yang mendaftar semua jadwal. Keduanya membutuhkan Ansible Gate; saat gate nonaktif, setiap tab Ansible selain **Pengaturan** menampilkan penjelasan alih-alih memuat data:

1. Klik **Jadwal Baru**, pilih playbook, nama opsional, dan **ekspresi cron** (5 field: menit, jam, hari-dalam-bulan, bulan, hari-dalam-minggu). Preset: tiap 5 menit (`*/5 * * * *`), tiap jam (`0 * * * *`), harian 03:00 (`0 3 * * *`), Minggu 03:00 (`0 3 * * 0`), bulanan (`0 3 1 * *`). Sintaks yang didukung: `*`, angka, rentang `a-b`, daftar `a,b`, langkah `*/n` dan `a-b/n`, serta `@hourly`, `@daily`, `@weekly`, `@monthly`, `@yearly`. Nama bulan/hari belum didukung. Bila hari-dalam-bulan dan hari-dalam-minggu sama-sama dibatasi, salah satunya cukup cocok (cron Vixie).
2. Pilih target (**Host Group** dan/atau Host tertentu), mode (`run`, `check`, `diff`, `syntax`), tags, extra vars, **Limit**, verbosity (0–4), dan become. Target ditentukan saat jadwal berjalan, jadi Host yang ditambahkan ke grup belakangan ikut tercakup, sedangkan limit mempersempit himpunan hasil resolusi itu sama seperti pada run manual.
3. Waktu mengikuti **zona waktu server Pantau** (ditampilkan di dialog; di Docker atur `TZ`, mis. `TZ=Asia/Jakarta`, jika tidak maka UTC).
4. Tabel menampilkan run berikutnya, run terakhir (klik nomor run untuk membuka lognya), dan galat terakhir. Gunakan saklar untuk menjeda/melanjutkan jadwal tanpa menghapusnya.

Aturan: run terjadwal memakai batas yang sama dengan run manual (1 run aktif per playbook, 3 global); jadwal yang mendapati playbook sibuk atau batas tercapai **tidak diantrikan**: mencatat `not started: …` lalu menunggu kemunculan berikutnya. Kemunculan yang terlambat lebih dari **5 menit** (Pantau mati atau Ansible Gate nonaktif) **dilewati, tidak pernah dijalankan susulan**, dan galat terakhir menampilkan `missed occurrence … skipped`. Run yang dimulai jadwal diberi label *terjadwal* di riwayat. Jadwal ikut System Snapshot terenkripsi (riwayat run tidak).

#### Pemantauan Log Real-Time & Pembatalan Run

1. **Penampil Log Monospace Live**: Keluaran stdout dan stderr digabungkan dan dialirkan secara langsung ke penampil run (side drawer) via polling setiap 1 detik (`GET /api/ansible/runs/{id}/log?offset=N`) dengan pencarian dan saklar **Follow** untuk auto-scroll.
2. **Batas Ukuran Log**: Log run aktif disimpan di buffer memori hingga 2 MiB. Jika keluaran melebihi 2 MiB, bagian awal log akan dipotong (*head truncation*) dan diberi penanda `[pantau] log truncated`.
3. **Pembatalan**: Klik tombol **Cancel** pada penampil log untuk membatalkan proses run yang sedang berjalan. Pantau mengirim sinyal SIGTERM ke grup proses, disusul SIGKILL setelah 5 detik jika proses belum berhenti.
4. **Batas Waktu (Timeout)**: Eksekusi dibatasi waktu maksimal bawaan selama 1 jam.
5. **Penanganan Interupsi**: Jika daemon Pantau dimatikan saat run masih berstatus `queued` atau `running`, run tersebut akan otomatis ditandai `interrupted` saat startup berikutnya.

#### Riwayat Run, Retensi & Batas Konkurensi

- **Batas Konkurensi**:
  - Maksimal **1 run aktif per playbook** (permintaan bersamaan ditolak HTTP 409).
  - Maksimal **3 run aktif global** di seluruh sistem Pantau (permintaan melebihi batas ditolak HTTP 429).
- **Kebijakan Retensi**:
  - Pantau secara otomatis mempertahankan **100 run selesai terbaru per playbook**. Riwayat yang lebih lama akan dihapus (*pruned*).
  - Riwayat run disimpan lokal di tabel `ansible_runs` dan dikecualikan dari System Snapshot agar ukuran backup tetap kecil.

#### Membaca Run: Log, Task Tree, Recap & Diff

Klik baris di **Ansible → Runs**, atau run di tab **Runs** playbook maupun riwayat jadwal, untuk membuka penampil run di side drawer. Run yang masih aktif diperbarui langsung dan punya tombol **Cancel**. Penampil menyediakan empat tampilan atas keluaran yang sama. Semuanya diturunkan dari log yang dicetak Ansible, sehingga run yang lognya terpotong di 2 MiB menampilkan pohon yang tidak lengkap; recap berbeda (lihat di bawah).

- **Log**: aliran mentah dengan warna ANSI yang dirender dengan aman (teks di-escape HTML lebih dulu sebelum warna diterapkan, sehingga host target tidak dapat menyisipkan markup).
- **Task Tree**: pohon **Play > Task > Host** yang dapat dilipat, setiap baris host membawa statusnya — `ok`, `changed`, `fatal`, `failed`, `skipping`, `unreachable`, `ignored`. Gunakan kotak pencarian untuk menemukan task berdasarkan nama dan pil filter status untuk menyembunyikan kebisingan (mis. hanya `failed` dan `unreachable`). Verbosity lebih tinggi memberi pohon lebih banyak bahan.
- **Recap**: `PLAY RECAP` dari run, diparse server saat run selesai dan disimpan bersamanya. Setiap host mendapat bilah berwarna (ok hijau, changed oranye, unreachable merah tua, failed merah terang, skipped abu/biru) beserta totalnya. Daftar riwayat menampilkan bilah recap mini per run, sehingga run bermasalah terlihat tanpa membukanya.
- **Diff**: hanya bermakna untuk run mode `diff`; blok `--- before` / `+++ after` / `@@` dipisah per berkas dengan baris tambahan hijau dan baris terhapus merah. Mode `check` dan `run` tidak pernah menghasilkan blok diff.

#### Survey (Pertanyaan Launch Bertipe)

Sebuah playbook dapat membawa **Survey**: pertanyaan bertipe yang ditanyakan setiap kali playbook dijalankan. Definisikan pada bagian **Survey** di editor playbook (tambah, hapus, urutkan field) atau melalui `PUT /api/ansible/playbooks/{id}/survey`:

| Field | Keterangan |
|---|---|
| Key | Nama variabel extra var, `^[A-Za-z_][A-Za-z0-9_]{0,63}$`, unik dalam satu survey. |
| Tipe | `text`, `textarea`, `number`, `integer`, `select`, `multiselect`, `boolean`, `password`. |
| Opsi | Untuk `select` / `multiselect`: 1–50 nilai unik. |
| Min / Max | Batas numerik untuk `number` / `integer`; panjang karakter untuk `text`, `textarea`, `password` (bilangan bulat, tidak negatif). |
| Default | Jawaban terisi otomatis; juga dipakai bila field tidak wajib dibiarkan kosong. |
| Wajib | Run ditolak di sisi server (bukan hanya di browser) bila jawaban tidak ada. |

Batas: maksimal 30 field dan key unik. Server mengembalikan HTTP 400 untuk jawaban dengan key tak dikenal (jawaban langsung masuk ke berkas `-e` run) dan untuk field **wajib** yang dibiarkan tanpa jawaban maupun default. Bila playbook memiliki survey, dialog Run mengganti kotak JSON bebas dengan formulir tersebut (kotak JSON tetap tersedia untuk hal yang tidak dicakup survey).

**Nilai mana yang menang.** Saat launch Pantau menggabungkan, dari terlemah ke terkuat: extra vars bawaan playbook, lalu jawaban survey (termasuk default untuk field opsional yang tidak dijawab), lalu extra vars yang diketik eksplisit di dialog launch. Jadi jawaban survey menimpa default `8000` milik playbook, dan operator yang mengetik `{"port": 9000}` pada extra vars launch tetap mendapat `9000`.

**Kata sandi.** Field `password` diteruskan ke Ansible saat run tetapi tidak pernah ditulis ke record run — `extra_vars` menampilkan `(hidden)` — dan tidak pernah dikembalikan API. Nilainya **tidak** disaring dari log: task yang mencetaknya (`debug: var=secret`, keluaran `-vvv`, perintah gagal yang menggemanya) akan menampilkan nilai itu di log run, yang dapat dibaca seluruh tim admin. Utamakan mekanisme tanpa frasa sandi (kunci SSH, berkas dari tab **Berkas**) dan anggap kata sandi yang tercetak sudah bocor.

Definisi survey disimpan bersama playbook di SQLite, diikutsertakan dalam System Snapshot terenkripsi, dan dikirim ke UI sebagai `survey` (daftar field) serta `has_survey` pada respons GET/list playbook.

#### Run Ad-Hoc (Satu Modul, Tanpa Playbook)

**Ansible → Hosts → Ad-hoc** menjalankan satu modul Ansible terhadap satu atau beberapa Host atau Host Group. Gunakan untuk pemeriksaan cepat dan operasi sekali jalan yang tidak layak disimpan sebagai playbook: `ping`, `setup`, `shell: uptime`, `command: df -h`, `service: name=nginx state=restarted`, `apt: update_cache=yes`, dan seterusnya.

1. Pilih **Modul** dari daftar umum atau ketik sendiri (`ansible.builtin.ping` dan `ping` sama-sama diterima) dan opsional **Argumen**-nya (satu string `-a`, hingga 4 KiB; kosongkan untuk modul tanpa argumen seperti `ping`).
2. Pilih **target**: Host individual, satu Host Group, dan/atau **Pattern** tambahan (bawaan `all`, aturan sama dengan **Limit** run).
3. Opsional: aktifkan **Become**, atur **Forks**, **Verbosity** (0–4), dan extra vars (objek JSON, hingga 64 KiB).
4. Jalankan: log dialirkan langsung di bawah formulir dan tabel hasil per host terisi saat host menjawab (success hijau, changed oranye, unreachable merah tua, failed merah).

Run ad-hoc bersifat nyata: `shell`, `command`, dan `raw` mengeksekusi di target dengan akses SSH yang sudah dimiliki Pantau, setara terminal. Targetnya tetap terbatas pada Generated Inventory: pola hanya dapat mengalamatkan alias Host dan nama Host Group Pantau, sehingga `connection: local`, `delegate_to`, dan `local_action` tidak dapat dipakai untuk keluar ke mesin lain lewat pola itu sendiri.

**Konkurensi dan riwayat**: maksimal **3 run ad-hoc** aktif sekaligus di seluruh instalasi; permintaan keempat ditolak (HTTP 429). Kuota ini dihitung **terpisah** dari run playbook, sehingga `ping` singkat tidak pernah menunggu di belakang playbook terjadwal dan sebaliknya. Riwayat menyimpan 100 run ad-hoc terbaru di tabel `ansible_adhoc_runs`; seperti run playbook, riwayat ini dikecualikan dari System Snapshot, dan run yang masih aktif saat Pantau berhenti ditandai `interrupted` pada start berikutnya. Run aktif dapat dibatalkan seperti run playbook.

#### Explorer Host & Group (Ping dan Facts)

**Ansible → Hosts** menampilkan armada persis seperti yang dilihat Ansible: pohon **Host Group > Host** yang dapat dilipat plus daftar *Ungrouped*. Setiap baris menampilkan grup tempat host berada menurut Host Group Pantau, badge inspeksi Pantau (sehat, peringatan, drift, tidak diketahui), dan **alias Ansible** host — nama tersanitasi yang benar-benar dipakai inventory, sehingga dapat disalin langsung ke baris `hosts:` playbook atau **Limit** run.

- **Ping**: klik **Ping** (seluruh pohon atau host terpilih) untuk memulai `ansible … -m ping` ad-hoc. Setiap host lalu menampilkan **Reachable**, **Unreachable**, **Failed**, atau *belum dicek* hingga ping berikutnya. Ini menguji jalur Ansible secara khusus: keterjangkauan SSH, kredensial dari SSH Key Library, dan interpreter Python di target — host yang bisa diinspeksi Pantau tetap dapat unreachable bagi Ansible bila `ansible_python_interpreter` hilang.
- **Facts**: tombol Facts membuka **penampil Facts** untuk satu host: kartu OS (distribusi, versi, kernel, arsitektur), hostname/FQDN, CPU (jumlah dan model), memori dan swap, mount, interface jaringan (IPv4/IPv6/MAC), IPv4 default, uptime, interpreter Python, dan virtualisasi, plus tab **JSON Mentah** dengan pencarian atas seluruh `ansible_facts`.

Facts dikumpulkan dengan menjalankan `setup` di host (hingga 90 detik, keluaran 2 MiB) dan **di-cache per host**: penampil menunjukkan waktu pengumpulannya, dan **Refresh facts** mengumpulkan ulang. Hanya satu pengumpulan per host berjalan sekaligus (permintaan kedua mendapat HTTP 409), host gagal atau unreachable mengembalikan galat singkat alih-alih ringkasan parsial, dan cache dihapus bersama Host. Facts adalah data operasional, bukan konfigurasi: tidak diikutsertakan dalam System Snapshot.

#### Git Sync (Memverifikasi Playbook Git)

Untuk playbook yang bersumber repositori Git, editor menampilkan commit dan waktu sync terakhir yang berhasil plus tombol **Sync Now**. Sync Now melakukan clone dangkal dengan kredensial tersimpan, memastikan path playbook yang dikonfigurasi masih ada, lalu melaporkan **commit**, **branch**, dan **jalur `.yml`/`.yaml` terurut** yang ditemukan di repositori (direktori titik seperti `.git` atau `.ansible/` serta `node_modules` dilewati; maksimal 1.000 entri). Clone sementara selalu dihapus. Hasilnya diingat sebagai `last_sync_at` dan `last_sync_commit`, sehingga Anda tahu apakah branch bergerak sejak terakhir dilihat. Kegagalan clone, jaringan, atau autentikasi mengembalikan HTTP 502 dengan pesan singkat yang mempertahankan baris terakhir keluaran git tetapi mengganti URL repositori dengan `<repo>` dan membuang kredensial, sehingga token tidak pernah muncul di respons maupun log server. Repositori yang berhasil di-clone tetapi tidak memuat path playbook yang dikonfigurasi mengembalikan HTTP 400.

Sync **tidak** menyimpan isi repositori dan tidak mengubah playbook: setiap run tetap melakukan clone baru. Gunakan setelah mengubah branch atau path, dan setelah merotasi token atau kunci, untuk menangkap salah ketik sebelum ditemukan oleh jadwal. Fitur ini hanya berlaku untuk sumber Git; sumber lain mengembalikan HTTP 400.

#### Git Repository Editor (Edit, Commit & Push)

Untuk playbook dengan `source = "git"`, tab **Edit** pada drawer playbook memiliki bagian **Files** (**Git Editor** di dalam peramban). Bagian ini memuat clone dangkal terbaru saat pertama kali dibuka (bisa butuh beberapa detik); bila clone gagal, galat git ditampilkan dengan tombol **Retry**. Simpan playbook lebih dulu: Files hanya tersedia untuk playbook yang sudah tersimpan.
- **Commit & Push** ada di footer drawer pada bagian Files dan tetap nonaktif sampai ada berkas yang diubah.

- **Penjelajahan & Pencarian Berkas**: Telusuri pohon berkas repositori dengan folder collapsible, pencarian berkas instan, serta penanda visual untuk berkas yang diubah (`M`), ditambah (`A`), atau dihapus (`D`).
- **Penyuntingan Kode & Syntax Highlighting**: Sunting berkas dengan penyorotan sintaksis CodeMirror (YAML, shell, HCL/C-like, teks biasa).
- **Operasi Berkas**: Gunakan toolbar untuk membuat berkas baru (**New File**), menghapus berkas (**Delete**), membatalkan perubahan (**Discard changes**), atau memuat ulang repositori (**Reload**). Bila ada perubahan belum disimpan, reload meminta konfirmasi dialog.
- **Dialog Commit & Push**:
  - Masukkan pesan commit yang deskriptif (wajib, 1–500 karakter).
  - Nama dan email author diingat di `localStorage` peramban (bawaan: `Pantau <pantau@localhost>` jika dikosongkan).
  - Pilihan target: **Push ke branch `{branch}`** atau **Buat branch baru** (masukkan nama branch baru yang sah).
  - Tanpa force push: perintah dijalankan murni non-force (`--force` tidak pernah digunakan).
- **Konkurensi Optimistis (HTTP 409 Conflict)**: Jika repositori remote telah diperbarui oleh commit lain sejak editor dibuka (`base_commit` tidak cocok dengan HEAD remote), permintaan ditolak dengan HTTP 409 (`ErrRepoChanged`). Dialog menampilkan tombol Reload agar operator dapat menyegarkan berkas dengan aman tanpa menimpa pekerjaan pihak lain.
- **Kebutuhan Izin Tulis Token & Kunci SSH**:
  - **GitHub Fine-Grained Personal Access Token**: Memerlukan izin **Repository permissions -> Contents: Read and write**.
  - **GitHub Classic Token**: Memerlukan scope **`repo`** (atau `public_repo`).
  - **GitLab Access Token**: Memerlukan scope **`write_repository`**.
  - **Kunci SSH Deploy**: Kunci SSH yang dipasang di repositori remote wajib dicentang opsi **Allow write access**.
  - Token baca-saja (*read-only*) akan berhasil saat membaca berkas namun gagal saat push dengan status HTTP 502 dan pesan keluaran git tersanitasi.
- **Efek Samping Sinkronisasi**: Push commit yang sukses ke branch aktif playbook secara otomatis memperbarui catatan `last_sync_at` dan `last_sync_commit`.
- **Batas & Validasi**: Maksimal 50 perubahan berkas per commit, 1 MiB per berkas, total ukuran payload 5 MiB, berkas wajib teks UTF-8 valid (berkas biner ditolak 415), tanpa symlink traversal, dan menolak jalur `..` atau `.git`.


### Manajemen Terraform (Gaya Terraform Cloud, CLI Lokal)

Pantau menyediakan subsistem manajemen Terraform dan OpenTofu mandiri yang terinspirasi dari HCP Terraform (Terraform Cloud). Administrator dapat mengelola **Workspace** terisolasi, mengeksekusi pipeline run lengkap (`plan` → `policy check` → `cost estimate` → `approval gate` → `apply`), melacak versi state (**State Versions**) immutable dengan diff visual dan rollback, mengelola variabel bersama (**Variable Sets**), menegakkan kebijakan tata kelola bawaan (**Policy as Code**), melihat dokumentasi **Private Module Catalog**, menjadwalkan deteksi drift otomatis (**Drift Detection**), serta mengintegrasikan webhook VCS Git—**tanpa memerlukan remote backend eksternal**.

#### Mengaktifkan Terraform Gate

Konfigurasi Terraform dapat menjalankan perintah sembarang di mesin controller melalui `provisioner "local-exec"`, `data "external"`, maupun modul pihak ketiga dari Git. Karena proses Terraform tidak dapat disandbox secara andal:
1. **Terraform Gate** (`terraform_enabled`) berada dalam kondisi **nonaktif secara default**.
2. Sebelum workspace atau run dapat dibuat atau dijalankan, administrator harus membuka **Terraform → Pengaturan** dan mengaktifkan saklar gerbang.
3. Pengaktifan gerbang mewajibkan konfirmasi dialog keamanan eksplisit yang menegaskan bahwa sesi admin Pantau dengan Terraform aktif setara dengan akses eksekusi proses di server host.
4. Pantau secara otomatis memeriksa ketersediaan biner `terraform` dan `tofu` lokal (serta biner opsional `infracost`). Status biner dan versinya ditampilkan pada banner status.

#### Membuat dan Mengonfigurasi Workspace

Workspace merepresentasikan satu unit environment infrastruktur yang terisolasi (konfigurasi, variabel, state, dan riwayat run):
1. Pada **Side Panel**, pilih menu **Terraform → Workspaces** lalu klik tombol **Workspace Baru**.
2. Atur parameter workspace:
   - **Nama**: Identifier unik *case-insensitive* (`^[A-Za-z0-9_-]{1,64}$`).
   - **Grup**: (Opsional) Label teks bebas (`group_name`, hingga 64 karakter) untuk mengelompokkan workspace. Saran otomatis muncul dari grup yang ada; kosongkan untuk tanpa grup.
   - **Environment**: Label lingkungan kerja (`production`, `staging`, `development`).
   - **Biner**: Pilih apakah run akan dieksekusi menggunakan biner `terraform` atau `tofu`.
   - **Tag**: Label dipisahkan koma untuk pengelompokan dan pemfilteran workspace.
   - **Sumber Konfigurasi**:
     - **Berkas Inline**: Simpan hingga 50 berkas (maksimal 256 KiB per berkas; `.tf`, `.tfvars`, `.json`, `.yaml`, dll.) langsung di basis data Pantau.
     - **Repositori Git**: Masukkan URL repositori (`https://` atau `ssh://`), nama branch target, direktori kerja di dalam repositori (bawaan: root), serta kredensial (token atau kunci SSH dari Library).
3. Workspace dialokasikan direktori kerja privat di server Pantau (`<DataRoot>/terraform/<workspaceID>`) dengan izin direktori ketat `0700`.
4. **Template**: modal Workspace Baru memiliki pemilih template (Hello world, AWS S3 bucket, AWS EC2 instance, GCP storage bucket, Azure resource group, DigitalOcean droplet, Hetzner server, Cloudflare DNS record, Kubernetes namespace; `GET /api/terraform/templates`). Template khusus provider membutuhkan **Akun Cloud** yang sesuai tertaut (diatur di Settings). Beberapa template punya variabel wajib tanpa default (`bucket_name`, `zone_id`, `name`, `content`), dan run pertama membutuhkan jaringan untuk mengunduh provider.

Mengklik kartu atau baris workspace membuka workspace tersebut di **side drawer** yang menempel di sisi kanan (pola yang sama dengan drawer Ansible) dengan tab **Overview**, **Configuration**, **Runs**, **State**, **Variables**, **Drift**, dan **Settings**. Berkas dan pengaturan disunting langsung di tempat: tab **Configuration** menyunting berkas inline di editor kode (tambah/hapus berkas, `Ctrl+S`, Save & plan; workspace Git membuka Git editor) dan tab **Settings** menyunting nama, grup, environment, tag, biner, interval drift, dan koneksi Git secara langsung. Tombol perluas memperlebar drawer hingga hampir memenuhi layar (pilihannya diingat); **Esc**, latar belakang (backdrop), atau tombol tutup menutupnya.

Editor tab **Configuration** memiliki penyorotan sintaks HCL dan dua alat untuk workspace inline (bekerja pada isi editor saat ini, termasuk yang belum disimpan):
- **Format**: menjalankan `terraform fmt` / `tofu fmt` (`Ctrl+Shift+F`) pada berkas (`POST /api/terraform/workspaces/{id}/tools/fmt`).
- **Validate**: menjalankan `init -backend=false` + `validate` (`POST /api/terraform/workspaces/{id}/tools/validate`), maksimal 2 menit dan 2 proses bersamaan. Diagnostik tampil di panel sebagai `file:line`. Saat offline, validasi hanya berfungsi dengan provider yang sudah ada di cache atau bawaan.

#### Pengelompokan & Organisasi Workspace

Workspace dapat dikelompokkan ke dalam bagian visual yang dimodelkan mengikuti Host Group:
- **Mengatur Grup**: Masukkan nama grup (dipangkas, hingga 64 karakter) saat membuat atau mengedit workspace. Grup bersifat implisit dari kumpulan nilai unik `group_name`.
- **Bagian yang Dapat Dilipat**: Kartu workspace ditampilkan di bawah header grup yang dapat diciutkan/dibentangkan baik pada mode Grid maupun List, lengkap dengan indikator jumlah workspace.
- **Pemfilteran**: Gunakan dropdown filter Grup untuk menyaring berdasarkan grup, menampilkan seluruh grup, atau mengisolasi workspace **Tanpa Grup (Ungrouped)**.
- **Label Kosmetik Murni**: Grup murni merupakan pembatas tampilan; pengelompokan tidak memengaruhi siklus run Terraform, file state, mekanisme penguncian (*locking*), maupun pembagian variable set.

#### Alur Eksekusi Run: Plan, Approve, dan Apply

Pantau menerapkan siklus hidup eksekusi multi-tahap yang aman dan terinspeksi:

1. **Memicu Run**:
   Klik tombol **Run Baru** pada workspace:
   - **Tipe**: Standar `plan` (plan untuk kemudian diapply), `destroy` (plan penghancuran resource yang dikelola), atau `refresh` (refresh-only plan untuk rekonsiliasi state).
   - **Speculative Plan**: Eksekusi plan simulasi sementara (berguna untuk menguji perubahan branch) yang tidak akan dilanjutkan ke tahap apply.
   - **Target / Replace**: Batasi eksekusi pada alamat resource spesifik menggunakan sintaksis target standar.
   - **Pesan**: Catatan commit atau tujuan eksekusi opsional.
2. **Tahap Planning & Pemeriksaan Awal**:
   Pantau mematerialisasikan state terbaru dari basis data, menyiapkan variabel lingkungan yang steril, menulis variabel ke `pantau.auto.tfvars.json` / `pantau-hcl.auto.tfvars`, lalu menjalankan `init` dan `plan -out=tfplan`.
   - Log stdout/stderr dialirkan secara *real-time* ke penampil Console Log.
   - Pratinjau visual plan diff terstruktur dibuat untuk memperlihatkan penambahan resource (`+`), pembaruan (`~`), penggantian (*replace* `-/+`), dan penghapusan (`-`).
3. **Evaluasi Policy as Code**:
   Bila terdapat kebijakan yang aktif, Pantau mengevaluasi representasi JSON plan terhadap aturan:
   - `advisory`: Catatan peringatan informasional; tidak memblokir approval.
   - `soft`: Memblokir approval standar hingga administrator mencentang opsi **Override Policy**.
   - `hard`: Menolak run secara mutlak; apply dilarang keras (`policy_failed`).
4. **Estimasi Biaya (Infracost)**:
   Bila biner `infracost` terpasang di server Pantau dan variabel `INFRACOST_API_KEY` dikonfigurasi di environment, Pantau menghitung estimasi biaya bulanan sebelum dan sesudah perubahan, lalu menyajikan selisih biaya (*delta*) di ringkasan run. Bila Infracost tidak terpasang, tahap ini dilewati secara transparan tanpa menggagalkan run.
5. **Approval Gate Mandatori**:
   Ketika sebuah run menghasilkan perubahan infrastruktur (`has_changes = true`), alur eksekusi berhenti pada status `awaiting_approval`:
   - **Confirm & Apply**: Menyetujui dan menerapkan plan. Komentar alasan dari operator bersifat **wajib** (minimal 3 karakter) untuk kebutuhan audit.
   - **Discard**: Membuang perubahan yang direncanakan. Komentar alasan bersifat **wajib**.
   - Bila terdapat peringatan kebijakan bertingkat `soft`, operator wajib mencentang **Override Policy** untuk mengaktifkan tombol konfirmasi.
6. **Menerapkan Perubahan (Apply)**:
   Setelah disetujui, Pantau menjalankan `apply tfplan` pada direktori eksekusi yang dipertahankan. Setelah tuntas, versi state immutable baru (**State Version**) dicatat di SQLite, nilai output disimpan, dan direktori kerja sementara run dibersihkan.

#### State Versions, Diff, Rollback, dan Workspace Locking

Pantau menggunakan SQLite sebagai sumber kebenaran tunggal (*single source of truth*) state Terraform, mengeliminasi kebutuhan remote state S3 eksternal:
- **State Versions**: Setiap kali apply, refresh, atau impor manual selesai dengan sukses, Pantau membuat entri versi state baru dengan nomor serial, lineage, jumlah resource, dan checksum SHA-256.
- **Visual State Diff**: Bandingkan dua versi state historis mana pun secara berdampingan untuk melihat resource dan atribut yang ditambah, dihapus, atau dimodifikasi.
- **State Rollback**: Kembalikan state ke versi sebelumnya dalam satu klik. Pantau menjaga konsistensi lineage, menaikkan nomor serial (`current.serial + 1`), dan menyimpan rollback sebagai versi state baru untuk mencegah konflik serial di masa mendatang.
- **Unduh & Impor Mentah**: Administrator dapat mengunduh berkas mentah `.tfstate` (disertai peringatan bahwa berkas dapat mengandung rahasia infrastruktur) atau mengimpor state yang sudah ada hingga ukuran 16 MiB.
- **Penguncian Workspace (Workspace Locking)**:
  - **Kunci Otomatis**: Workspace dikunci otomatis (`locked_by = "run:<id>"`) selama run berstatus antre, planning, menunggu approval, atau applying.
  - **Kunci Manual**: Administrator dapat mengunci workspace secara manual dengan alasan tertentu guna mencegah pemicuan otomatis selama pemeliharaan.
  - **Force Unlock**: Jika run terhenti atau bermasalah, administrator dapat memicu Force Unlock darurat untuk membatalkan run aktif, membuka kunci database, dan membersihkan berkas lock `.terraform.tfstate.lock.info` di disk.
- **State Operations** (tab State): **Remove** (`state rm`), **Move** (`state mv`) dan **Import resource** (`POST /api/terraform/workspaces/{id}/state/ops`). Tiap operasi membuat versi state **baru** (versi sebelumnya disimpan; sumbernya dicatat, mis. `state rm x`). Workspace dikunci selama operasi (HTTP 423 jika sudah terkunci, 409 jika ada run aktif). Dialog konfirmasi menampilkan perintah persisnya. Import menjalankan `init` dan memakai kredensial Akun Cloud yang tertaut. Tombol aksi terlihat pada setiap baris resource.
- **Backend run**: run tidak lagi memakai flag `-state` yang deprecated. Pantau menulis `pantau_backend_override.tf` (backend `local` dengan path di root run, menggantikan backend apa pun di kode Anda) dan menjalankan `init -reconfigure`. Jangan memakai nama berkas `pantau_backend_override.tf` pada berkas workspace Anda; berkas itu ditimpa.

#### Variabel dan Variable Sets

Kelola variabel konfigurasi secara teratur tanpa memasukkan nilai langsung ke dalam berkas `.tf`:
- **Kategori Variabel**:
  - `terraform`: Diteruskan ke proses Terraform sebagai variabel input. Variabel bertipe non-HCL diformat secara aman ke `pantau.auto.tfvars.json`; variabel bertipe HCL diformat ke `pantau-hcl.auto.tfvars`.
  - `env`: Disuntikkan sebagai variabel lingkungan proses anak (misal: `AWS_ACCESS_KEY_ID`, `ARM_CLIENT_SECRET`).
- **Nilai Sensitif**: Variabel yang ditandai sensitif bersifat *write-only*. Nilainya disensor sebagai string `(sensitive)` pada log eksekusi, tidak pernah dikembalikan ke respons API JSON (`json:"-"`), dan disembunyikan di antarmuka web.
- **Variable Sets**: Kelompokkan variabel terkait (misal: *Kredensial Cloud Produksi*, *Konfigurasi DNS Global*) lalu kaitkan ke beberapa workspace sekaligus, atau jadikan **Global** agar berlaku otomatis ke seluruh workspace.
- **Prioritas Resolusi**: Variabel level Workspace menimpa variabel dari Variable Set yang terhubung, yang mana menimpa Variable Set Global.
- **Deteksi dari konfigurasi**: pada workspace inline, tombol **Detect from configuration** di tab **Variables** (`GET /api/terraform/workspaces/{id}/variables/detect`) mem-parse blok `variable` pada berkas `.tf` lalu mendaftarkannya. Variabel ber-default tidak dicentang, yang wajib (tanpa default) dicentang, dan yang sudah ada ditampilkan nonaktif. Yang dipilih ditambahkan sebagai variabel `terraform`. Workspace Git tidak didukung; pemindai bukan parser HCL penuh dan hanya membaca berkas `.tf`.
- **Linked outputs** (kartu tab Variables): menyuntikkan output dari **state terbaru** workspace lain ke variabel Terraform (`GET/PUT /api/terraform/workspaces/{id}/output-links`). Nilai diselesaikan saat run dimulai dan diteruskan sebagai HCL JSON; output sensitif tetap tersamarkan. Variabel workspace atau variable set eksplisit dengan nama yang sama menang atas tautan. Siklus antar workspace ditolak, dan output yang tidak tersedia (tanpa state, output tidak ada) menggagalkan awal run dengan pesan jelas. Tautan ikut dalam backup.

#### Akun Cloud

Daftarkan kredensial provider sekali, lalu setiap run workspace memakainya tanpa menyalin kunci ke variabel workspace.
1. Buka **Terraform → Akun Cloud** dan klik **Akun baru**. Pilih provider (AWS, Google Cloud, Microsoft Azure, DigitalOcean, Hetzner, Cloudflare, Kubernetes), beri nama, lalu isi kolom kredensial. Cloudflare memakai `api_token` dan `account_id` opsional; Kubernetes memakai kubeconfig dan context opsional. Untuk Kubernetes, kubeconfig ditulis ke berkas sementara `0600` per run, dihapus setelah run selesai, dan isinya disensor dari log.
2. Klik **Test connection** pada akun. Pantau memverifikasi kredensial ke API provider: AWS `sts:GetCallerIdentity` (dan `AssumeRole` bila role diisi), token service account Google Cloud + pengecekan project, token client-credentials Azure, DigitalOcean `/v2/account`, Hetzner `/v1/servers`, API verifikasi token Cloudflare. Kubernetes hanya mem-parse kubeconfig (tanpa panggilan jaringan). Hasil (dan identitas yang ditemukan) tampil pada akun.
3. Buka drawer workspace → **Settings** → kartu **Cloud accounts** lalu centang akun yang ingin dihubungkan. Satu workspace dapat menghubungkan beberapa akun; menghapus akun otomatis melepas tautannya di semua workspace.
4. Setiap run workspace yang terhubung (plan, apply, drift, webhook, operasi state, estimasi biaya) menerima kredensial sebagai variabel lingkungan:

| Provider | Variabel lingkungan |
|---|---|
| AWS | `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`, `AWS_DEFAULT_REGION` + `AWS_REGION` (default `us-east-1`) |
| Google Cloud | `GOOGLE_PROJECT` + `GOOGLE_CLOUD_PROJECT`, `GOOGLE_REGION`, `GOOGLE_CREDENTIALS` (JSON service account) |
| Microsoft Azure | `ARM_SUBSCRIPTION_ID`, `ARM_TENANT_ID`, `ARM_CLIENT_ID`, `ARM_CLIENT_SECRET` |
| DigitalOcean | `DIGITALOCEAN_TOKEN` |
| Hetzner | `HCLOUD_TOKEN` |
| Cloudflare | `CLOUDFLARE_API_TOKEN`, `CLOUDFLARE_ACCOUNT_ID` (opsional) |
| Kubernetes | `KUBE_CONFIG_PATH` + `KUBECONFIG` (path kubeconfig sementara), `KUBE_CTX` (context opsional) |

- **Prioritas**: bila beberapa akun terhubung, akun dengan id lebih besar menang saat bentrok; variabel `env` milik workspace dan Variable Set menimpa nilai dari akun cloud.
- **Keamanan**: kredensial disimpan di database Pantau, tidak pernah dikembalikan oleh API (kolom rahasia tampil "stored"; kosongkan kolom rahasia saat mengedit untuk mempertahankannya), dan ikut dalam backup snapshot terenkripsi. Hanya kredensial statis (atau AWS assume role, lihat di bawah) yang didukung (tanpa OIDC / workload identity). Kredensial tetap tersimpan sebagai nilai polos di SQLite (tanpa enkripsi at-rest).
- **Penyensoran log**: log run hanya menyensor rahasia. Kolom cloud non-rahasia (region, access key id, tenant / subscription / client id, project id) tetap terlihat agar run lebih mudah di-debug; kolom rahasia (secret key, session token, API token, JSON service account, client secret, `external_id`, kubeconfig) dan variabel `env` milik pengguna tetap disensor.

##### AWS Assume Role

Akun AWS dapat opsional meng-assume IAM role alih-alih memakai kunci dasar secara langsung: isi **Role ARN** (`role_arn`), opsional **External ID** (`external_id`, rahasia) dan **Session name** (`session_name`, default `pantau`). Di awal setiap run Pantau memanggil STS `AssumeRole` (1 jam) dengan kunci dasar lalu menyuntikkan kredensial sementara (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`). Jika pemanggilan gagal, run gagal dengan pesan error yang jelas. **Test connection** juga meng-assume role tersebut.

##### Impor dari Akun Pantau yang Ada

Alih-alih mengetik kredensial lagi, **Akun baru** dapat mengimpor dari akun **Cloudflare** atau cluster **Kubernetes** Pantau yang sudah ada. Kredensial **disalin saat impor** (bukan tautan langsung): perubahan atau penghapusan sumbernya tidak memengaruhi Akun Cloud. Daftar pilihan berasal dari `GET /api/terraform/cloud-accounts/importable` yang hanya mengembalikan nama (tanpa kredensial); pembuatan memakai `import_from: {type, id}` pada `POST /api/terraform/cloud-accounts`.

#### Panel Outputs

Periksa nilai output yang dihasilkan oleh state aktif terbaru pada tab **Outputs**:
- Menampilkan nama output, nilai JSON, dan tipe data.
- Output sensitif (dideklarasikan `sensitive = true` pada HCL) disembunyikan secara default (`null`, `value_hidden: true`) dan dapat dibuka dengan tombol **Reveal** oleh administrator terautentikasi.
- Tombol salin satu-klik mempermudah penyalinan connection string atau alamat IP.

#### Katalog Modul Privat (Private Module Catalog)

Kelola katalog dokumentasi modul Terraform dan OpenTofu internal yang dapat digunakan kembali:
1. Daftarkan modul dengan nama dan penyedianya (misal: `vpc / aws`, `cluster / kubernetes`).
2. Unggah versi modul menggunakan berkas inline atau tautkan repositori Git.
3. Pemindai HCL bawaan Pantau secara otomatis menguraikan kode modul dan menampilkan:
   - Tabel parameter input lengkap (nama, tipe, deskripsi, nilai default, status wajib).
   - Deklarasi dan deskripsi output.
   - Tipe resource yang dideklarasikan dan versi provider minimum yang dibutuhkan.
   - Dokumentasi pratinjau `README.md`.
   - Cuplikan siap pakai `module "nama" { ... }`.

#### Deteksi Drift Terjadwal (Drift Detection)

Otomatisasi pengawasan terhadap perubahan di luar kendali (*out-of-band changes*):
1. Atur interval pemeriksaan drift (**Drift Check Interval**, misal setiap 60, 360, atau 1440 menit) di pengaturan workspace.
2. Scheduler latar belakang Pantau secara berkala menjalankan plan spekulatif dengan opsi `-refresh-only` tanpa menerapkan perubahan apa pun.
3. Bila ditemukan perbedaan antara infrastruktur riil dan state file, status workspace ditandai **Drift Detected**, dan rincian perubahan dicatat agar dapat ditinjau oleh administrator.

#### Aturan Kebijakan (Policies) dan Estimasi Biaya

Tegakkan tata kelola dan pantau estimasi pengeluaran infrastruktur sebelum perubahan diterapkan:
- **Kebijakan Bawaan**: Konfigurasikan batasan keamanan tanpa bahasa DSL rumit:
  - `deny_destroy`: Menolak run yang merencanakan penghapusan resource.
  - `max_deletes`: Membatasi jumlah maksimum resource yang boleh dihapus.
  - `max_changes`: Membatasi jumlah maksimum perubahan resource.
  - `deny_resource_types`: Melarang pembuatan tipe resource cloud tertentu.
  - `require_tags`: Memastikan tag wajib disertakan pada resource yang dibuat.
  - `deny_open_cidr`: Mencegah security group dengan aturan ingress terbuka `0.0.0.0/0`.
- **Integrasi Infracost**:
  Pasang biner `infracost` di server host Pantau. Berikan `INFRACOST_API_KEY` pada variabel lingkungan. Setiap plan yang dijalankan akan otomatis menghitung rincian biaya proyek bulanan dan selisih bersihnya langsung di ringkasan run.

#### Integrasi Git VCS dan Pengaturan Webhook

Otomatisasi tahap planning saat kode di-push ke repositori Git:
1. Pada Pengaturan Workspace, aktifkan **Integrasi Webhook** dan salin URL webhook yang dibuat (`/api/terraform/webhook/{token}`).
2. (Opsional) Buat atau masukkan **Webhook Secret** untuk validasi tanda tangan kriptografi.
3. Konfigurasikan pada penyedia Git Anda:
   - **GitHub**: Pada **Repository Settings → Webhooks**, isi Payload URL, pilih `application/json`, pilih event **Push**, dan tempelkan webhook secret (divalidasi melalui `X-Hub-Signature-256`).
   - **GitLab**: Pada **Settings → Webhooks**, isi URL, masukkan secret token pada kolom **Secret token** (divalidasi melalui `X-Gitlab-Token`), dan aktifkan **Push events**.
4. **Jaminan Keamanan**: Webhook **hanya memicu proses Plan spekulatif atau standar** saat terjadi push ke branch yang dikonfigurasi. Webhook **tidak pernah mengeksekusi Apply secara otomatis**, termasuk pada workspace yang mengaktifkan Auto-apply. Semua penerapan perubahan ke infrastruktur produksi tetap wajib melalui persetujuan manual di Approval Gate.

#### Menyunting Repositori Workspace (Git Editor)

Workspace yang menggunakan repositori Git (`source = "git"`) memiliki tombol **Edit Repository** pada kartu VCS di dalam tab **Settings**.

- **Pohon Berkas Penuh**: Daftar berkas ditampilkan relatif terhadap root repositori (bukan dibatasi sub-direktori `git_path`), memungkinkan penyuntingan berkas di luar folder kerja utama (misalnya modul root, konfigurasi `.tf` pendukung, atau berkas definisi variabel).
- **Penyuntingan Kode & Manajemen Berkas**: Tambah berkas konfigurasi baru, sunting berkas `.tf` / `.tfvars` / dokumentasi, hapus berkas, dan batalkan perubahan lokal.
- **Alur Commit & Push**:
  - Masukkan pesan commit, nama pembuat, dan email.
  - Pilih untuk push langsung ke branch aktif workspace atau buat **Branch baru**.
  - Penegakan non-force push mencegah penimpaan riwayat remote secara tidak disengaja.
- **Konkurensi Optimistis (HTTP 409 Conflict)**: Bila branch remote telah menerima commit baru sejak editor dibuka (`base_commit` tidak cocok dengan HEAD remote), Pantau menolak push dengan HTTP 409 (`ErrRepoChanged`) untuk melindungi integritas kode.
- **Prasyarat Izin Tulis Token**:
  - Token akses Git atau Kunci SSH Deploy wajib memiliki hak tulis (misal: GitHub Fine-Grained `Contents: Read and write`, GitHub classic `repo`, GitLab `write_repository`, atau SSH key dengan izin tulis).
  - Token baca-saja (*read-only*) akan ditolak saat push dengan galat HTTP 502 disertai rincian git yang disanitasi.
- **Efek Samping Webhook**: Melakukan commit dan push langsung ke branch yang dipantau workspace akan memicu webhook VCS Git (jika aktif), yang secara otomatis memulai antrean proses Plan spekulatif.
- **Batas Operasional**: Maksimal 50 perubahan berkas per commit, 1 MiB per berkas, total 5 MiB payload, hanya berkas teks UTF-8 valid (berkas biner ditolak 415), dan sanitasi path ketat.

#### Notifikasi

Kartu **Notifications** di tab **Gate** Terraform memilih event Terraform yang dikirim. Pengiriman memakai kanal yang sudah dikonfigurasi di **Settings** Pantau (Telegram, webhook, ntfy, Slack, Discord, SMTP); tidak ada pengaturan kanal terpisah.
- **Event**: menunggu approval, error, applied, drift. Bawaan: semua kecuali applied.
- `GET/PUT /api/terraform/notifications` membaca dan mengubah pilihan; `POST /api/terraform/notifications/test` mengirim event contoh.
- **Drift** hanya memberi notifikasi saat transisi tidak ada → ada (tidak berulang selama drift berlanjut).

#### Plan Terjadwal dan Auto-apply

Dua pengaturan di **Workspace Settings**:
- **Scheduled plan interval (minutes)** (`plan_interval_minutes`): membuat run plan bertipe `schedule` pada interval tersebut (0 = nonaktif).
- **Auto-apply** (`auto_apply`): meng-apply plan yang memiliki perubahan dan berstatus kebijakan `passed`, dengan komentar `auto-apply`. Hanya mencakup run plan **terjadwal** dan **yang dimulai manual** pada workspace non-produksi; **run yang dipicu webhook tidak pernah di-auto-apply** (pemegang secret webhook tidak boleh memicu apply tanpa pengawasan) dan selalu menunggu di Approval Gate. **Tidak pernah tersedia untuk environment produksi**: saat disimpan dijawab HTTP 400, dan workspace yang environment-nya kemudian menjadi produksi mengabaikan flag ini saat runtime. Run destroy dan refresh, serta run dengan peringatan kebijakan, tetap butuh persetujuan manual di Approval Gate. Jika apply otomatis gagal dimulai, run tetap di Approval Gate dan notifikasi dikirim.

#### Tab Audit

Tab **Audit** menampilkan jejak permanen event Terraform (tabel `tf_audit`, 5000 terbaru disimpan; `GET /api/terraform/audit`). Entri ditulis dari aliran log Pantau. Tidak ada RBAC karena Pantau hanya memiliki satu kata sandi admin. Lonjakan lebih dari ~64 baris dalam satu jendela penulisan dapat membuang baris.

#### Terraform Registry

Tab **Registry** tingkat atas di Terraform menjelajah dan mencari Terraform Registry publik (modul dan provider) tanpa meninggalkan Pantau.
- **Modul**: `GET /api/terraform/registry/modules` menampilkan/mencari modul. Detail modul memuat versi, input, output, dan README sebagai teks biasa. Pencarian membutuhkan kueri yang tidak kosong.
- **Provider**: `GET /api/terraform/registry/providers` menampilkan/mencari provider. Pencarian provider memakai **endpoint pencarian v2 registry yang tidak terdokumentasi**, yang dapat berubah sewaktu-waktu.
- **Dari tab Configuration**: tombol **Registry** membuka penjelajah yang sama dengan **Insert at cursor** atau **Add as file**. Add as file membuat `module-<name>.tf` atau `provider-<type>.tf`, dan opsional menambahkan variabel input wajib modul ke `variables.tf`. Snippet dibuat di sisi server, memakai batasan versi `~> MAJOR.MINOR`, dan placeholder dipilih menurut tipe input.
- **Check versions** (hanya workspace inline): `GET /api/terraform/workspaces/{id}/registry/check` membaca berkas yang **tersimpan** (simpan dulu sebelum memeriksa) dan mendaftar setiap modul dan provider beserta status: `up_to_date`, `outdated` (pin eksak di bawah versi terbaru), `newer_outside_constraint`, `unpinned`, atau `unknown` (sumber bukan registry). Tombol **Apply** hanya menulis ulang literal versi di editor; perubahan belum tersimpan sampai Anda menekan **Save**.
- **Base URL registry**: kartu **Registry** di tab **Gate** Terraform mengatur base URL (setting `tf_registry_url`; `GET/PUT /api/terraform/registry/settings`, uji dengan `POST /api/terraform/registry/settings/test`) untuk mirror atau proxy air-gapped. Harus `https`, kecuali `http` pada loopback, dan tidak boleh memuat kredensial.
- **Keamanan**: Pantau mengambil data di sisi server dengan batas waktu 15 detik, batas respons 4 MiB, cache memori 5 menit, hanya pengalihan ke host yang sama, dan argumen path yang divalidasi. Teks dari registry tidak tepercaya dan ditampilkan dengan di-escape.
- **Batasan**: hanya protokol Terraform Registry publik (tanpa token registry privat, tanpa pencarian registry khusus OpenTofu); Run mengunduh modul/provider sendiri saat `terraform init` dan membutuhkan akses jaringan dari server; workspace Git tidak diperiksa; versi prerelease dibandingkan sebagai semver biasa.

#### Batasan dan Non-Goals yang Transparan

Demi mempertahankan arsitektur mandiri, ringan, dan mudah dirawat, sejumlah fitur sengaja tidak dimasukkan ke dalam ruang lingkup:
- **Kredensial Masih Polos di Penyimpanan**: Kredensial Akun Cloud dan nilai variabel disimpan tanpa enkripsi di SQLite (perlindungan bergantung pada izin berkas DB dan enkripsi snapshot).
- **Import Belum Diuji ke Provider Nyata**: Operasi state **import resource** sudah diimplementasikan tetapi belum diverifikasi terhadap provider cloud sungguhan.
- **Template Provider Belum Divalidasi `init`**: Selain Hello world, template provider belum divalidasi dengan `init` terhadap registry provider sungguhan.
- **Tanpa Remote State Backend / S3**: Pantau menggunakan basis data SQLite lokal. Konfigurasi remote backend (`backend "s3"` / `backend "remote"`) di kode workspace tidak digunakan.
- **Auto-Apply Hanya Opt-In dan Terbatas**: Auto-apply hanya ada sebagai pengaturan eksplisit per workspace (lihat [Plan Terjadwal dan Auto-apply](#plan-terjadwal-dan-auto-apply)); hanya mencakup plan terjadwal dan manual, tidak pernah run yang dipicu webhook, dan environment produksi tidak dapat memakainya. Run destroy, refresh, serta run dengan peringatan kebijakan selalu butuh persetujuan manual di Approval Gate.
- **Tanpa OPA / Rego / Sentinel**: Kebijakan tata kelola dibatasi pada evaluator bawaan Go.
- **Tanpa Kredensial Dinamis OIDC**: Kredensial cloud diteruskan melalui variabel sensitif berkategori `env`; pertukaran token dinamis STS/OIDC tidak dikelola otomatis oleh Pantau.
- **Tanpa Protokol Registry Publik**: Katalog modul privat berfungsi sebagai penampil dokumentasi internal, bukan endpoint HTTP registry untuk `terraform init`.
- **Tanpa Controller Windows**: Meski biner Pantau dapat berjalan di Windows, eksekusi workspace Terraform/OpenTofu dinonaktifkan di Windows (`Detect()` mengembalikan status tidak didukung).
- **Biner**: Image Docker resmi menyertakan `terraform`, `tofu` (OpenTofu) dan `infracost`; pada instalasi non-Docker pasang `terraform` atau `tofu` di `PATH`. Infracost membutuhkan variabel `env` `INFRACOST_API_KEY` pada Workspace atau Variable Set.
- **Batas Konkurensi**: Maksimal 1 run aktif per workspace dan 3 run aktif secara global. Permintaan yang melebihi batas menerima respons HTTP 409 atau 429.
### Evaluasi Kelayakan Server dengan Lifecycle Assessment

Pantau menghitung nilai kelayakan sistem secara transparan melalui **Lifecycle Score** (0–100) berdasarkan 6 faktor evaluasi:

1. **OS End-of-Life (EOL)**: Evaluasi masa dukungan resmi rilis distribusi Linux dan versi kernel target.
2. **Productive Lifespan**: Mengukur usia operasional perangkat keras terhadap batas wajar industri (3–5 tahun) melalui tanggal rilis BIOS bare-metal atau waktu deploy OS pada VM cloud.
3. **Saturasi RAM**: Frekuensi kehabisan memori fisik dan aktivitas swap berlebih (*swap thrashing*).
4. **Saturasi Beban CPU**: Rata-rata beban sistem (load average) dinormalisasi terhadap jumlah inti prosesor.
5. **Kapasitas Partisi Disk**: Risiko kepenuhan penyimpanan lokal dan hambatan I/O disk.
6. **Integritas I/O Kernel**: Pemindaian log ring buffer kernel (`dmesg`) terhadap kerusakan sektor disk, remount read-only, atau hardware reset.

Ketika skor memasuki batas kritis, Pantau menghasilkan rekomendasi formal **Hardware Refresh** sebagai dasar pengajuan peremajaan infrastruktur.

### Evaluasi Komparatif Multi-Host & Matriks Penilaian

Saat mengevaluasi disparitas performa infrastruktur, memilih server tujuan migrasi beban kerja, atau mengaudit konsistensi klaster:

- **Matriks Komparasi Interaktif**: Buka melalui tombol **`⚖️ Bandingkan`** di header global atau melalui tab Host Workspace Modal. Pilih 2 hingga 4 host secara bersamaan untuk meninjau telemetri perbandingan berkepadatan tinggi dalam tampilan modal luas (75vw × 80vh).
- **Penilaian 4 Dimensi Terstandarisasi**:
  - **Kapasitas & Beban Komputasi**: Core fisik/virtual, clock speed, arsitektur, dan saturasi beban sistem ternormalisasi per core.
  - **Penyimpanan & I/O Disk**: Utilisasi partisi, integritas berkas sistem, dan kecepatan throughput I/O aktif.
  - **Keandalan Jaringan**: Jitter latensi, reliabilitas transmisi paket, dan status keterjangkauan.
  - **Higienitas Sistem & Siklus Hidup**: Status deviasi aturan konfigurasi, akumulasi paket dorman, masa aktif sistem (*uptime*), dan skor siklus hidup (*lifecycle score*).
- **Filter Derau Deadband 5%**: Selisih performa tipis dalam rentang toleransi ±5% diperlakukan setara/imbang (*tie*) untuk meredam fluktuasi metrik dan mencegah pemeringkatan semu. Host yang stale atau offline dikenakan penalti skor proporsional.
- **Sintesis Keputusan Eksekutif**: Merangkum kesimpulan operasional secara otomatis dalam bahasa manusia, menyoroti juara tiap kategori (*category champions*), rekomendasi penempatan beban kerja, dan peringatan risiko server tersaturasi.
- **Ekspor Markdown Sekali Klik**: Salin papan skor matriks perbandingan dan ringkasan eksekutif ke clipboard dalam format Markdown rapi (tetap berfungsi pada koneksi LAN HTTP polos via mekanisme fallback browser).

### Eksekusi Perintah Massal (Action Dialog)

Saat mengelola banyak server secara bersamaan:
- **Action Dialog**: Buka melalui tombol **Perintah Massal** di bilah header dashboard.
- **Pemilihan Host**: Saring host via kotak pencarian, pilih per Host Group, atau gunakan **Pilih Semua (Maks 20)** untuk memilih hingga 20 server tujuan.
- **Eksekusi Aman**: Meminta konfirmasi via `showConfirm` yang mencantumkan jumlah server dan cuplikan perintah yang akan dijalankan.
- **Streaming Hasil**: Menjalankan perintah secara konkuren (hingga 5 proses bersamaan) dengan hasil NDJSON yang mengalir secara langsung per server (badge status, exit code, durasi, detail output yang dapat dibuka, dan tombol **Salin Semua**).
- **Dukungan Sudo**: Opsi elevasi `sudo -n` tanpa interaksi kata sandi (langsung gagal dengan aman bila memerlukan kata sandi).

### Pembaruan Paket & Log Streaming Pembaruan

Pantau mendukung pembaruan paket perangkat lunak secara agentless langsung dari tab **Packages** atau **Security** di Host Detail:
- **Pratinjau Pembaruan**: Menampilkan daftar paket tertunda, rincian paket umum vs keamanan, pengelola paket (`apt`, `dnf`, `yum`), serta banner peringatan kebutuhan reboot sistem.
- **Pilihan Mode**: Mendukung pembaruan penuh seluruh paket (`all`) atau hanya pembaruan keamanan (`--security` pada dnf/yum).
- **Peringatan Keselamatan**: Konfirmasi tindakan dengan peringatan tegas bahwa layanan remote dapat restart dan tidak tersedia mekanisme rollback otomatis.
- **Modal Log Streaming**: Mengalirkan keluaran log terminal pembaruan secara real-time melalui WebSocket (`/ws/pkg/upgrade`), memantau baris log dan kode keluar akhir (`[pantau] exit=N`). Pemindaian paket dan keamanan akan otomatis diperbarui setelah proses tuntas.

## 5. Fase 4: Operasi Berkas & Aliran Data Lintas-Host

### Cross-Host Transfer (Pipa Memori Tanpa Disk Lokal)

Pemindahan basis data berukuran besar atau arsip direktori antar-dua server Linux umumnya membutuhkan penyimpanan perantara atau setup manual `scp`. Pantau mengalirkan data antar-host **tanpa menyimpan data perantara di disk lokal Pantau**.

```
+-------------------+        Piped Stream (Aliran RAM)              +-------------------+
|  Remote Host A    | ============================================> |  Remote Host B    |
|  (Sumber: tar)    |                 via Pantau                    |  (Tujuan: untar)  |
+-------------------+                                               +-------------------+
```

1. Buka file explorer pada **Workspace Modal** milik Host sumber.
2. Pilih file atau folder yang hendak dipindahkan, lalu klik **Cross-Host Transfer**.
3. Tentukan Host tujuan beserta path direktori target.
4. Pilih mode transfer:
   - **Fast Stream Mode**: Aliran data berkecepatan tinggi memanfaatkan pipa *tar stream*.
   - **Verified Mode**: Menghitung dan mencocokkan checksum SHA256 secara end-to-end di kedua host sebelum transfer dinyatakan selesai.
5. Pantau progres pemindahan pada antrean **Transfer Job** latar belakang (menampilkan kecepatan transfer MB/s, akumulasi byte, dan estimasi waktu tersisa).

### Folder Streaming Download Langsung ke Browser

Untuk mengunduh satu struktur folder dari server target ke komputer kerja Anda:
- Pilih folder pada manajer berkas SFTP, lalu klik **Download as Archive**.
- Pantau akan memampatkan folder secara instan (*on-the-fly* menggunakan `zip` jika terpasang di target, atau `tar.gz`) dan mengalirkannya langsung ke antarmuka unduhan browser Anda tanpa menyisakan sampah arsip di server Pantau.

### Duplikasi Berkas & Folder Aman (Disk Space Safety Check)

Pantau menyediakan fitur penggandaan (*cloning*) lokal untuk berkas atau folder langsung di server remote tanpa proses unduh dan unggah ulang:

1. Klik tombol aksi **📋 Duplikat** pada baris file atau folder yang ingin digandakan.
2. Modal akan menampilkan rekomendasi nama ber-timestamp otomatis:
   - Untuk berkas berektensi: `<nama_file>_<YYYYMMDDHHmmss>.<ekstensi>` (misal: `nginx_20261002043000.conf`), menjaga asosiasi tipe berkas dan *syntax highlighting* tetap aktif.
   - Untuk direktori atau berkas tanpa ekstensi: `<nama_folder>_<YYYYMMDDHHmmss>`.
3. **Disk Space Safety Check**: Pantau secara otomatis memeriksa ketersediaan ruang penyimpanan riil pada partisi target via `df -PB1`. Jika sisa ruang disk kurang dari ukuran item ditambah batas aman (*safety buffer*) 100 MB, proses duplikasi dicegah dengan pesan peringatan merah untuk melindungi server dari risiko kepenuhan disk (*disk exhaustion*).
4. **Pencegahan Tabrakan Nama (Collision Prevention)**: Jika berkas atau folder dengan nama target sudah ada, sistem menolak eksekusi secara ketat untuk mencegah penimpaan data secara tidak sengaja.
5. **Eksekusi Native Remote**: Duplikasi dijalankan langsung di host remote via `cp -a`, menjaga hak izin (*permissions*), kepemilikan, dan timestamp asli. Item berukuran $\ge$ 500 MB menampilkan lencana peringatan bahwa proses mungkin membutuhkan waktu beberapa detik.

### Navigasi Breadcrumb Interaktif & Estetika Antarmuka

SFTP File Manager dilengkapi navigasi dan ergonomi visual yang telah disempurnakan:
- **Interactive Breadcrumb Bar**: Klik segmen folder mana saja pada bilah jalur direktori (contoh: `📁 /` &rarr; `var` &rarr; `www` &rarr; `html`) untuk melompat langsung ke direktori induk tanpa perlu menekan tombol navigasi atas berulang kali.
- **Ikon Berkas Kontekstual**: Baris berkas menampilkan ikon spesifik berdasarkan ekstensi (🐚 script shell, ⚙️ konfigurasi/json/yaml, 📝 dokumen log/teks, 🗜️ arsip, 🖼️ media gambar, 💻 kode pemrograman).
- **Sticky Table Header**: Header kolom dan tombol sorting tetap terpancang di posisi atas (*sticky*) saat menggulir daftar direktori yang panjang.

### Disk Usage Explorer

Klik **Disk usage** di toolbar File Manager untuk melihat folder mana yang paling banyak memakai ruang di direktori saat ini (seperti `ncdu`):
- Menjalankan `du -x -k` satu tingkat di host tersebut, hanya saat diminta (tidak dijadwalkan dan tidak di-cache), tetap di filesystem yang sama, dan memakai user akses File Manager yang sedang aktif (sebagai user lain lewat `sudo -n -u`).
- Folder diurutkan dari yang terbesar dengan bar proporsional dan persentase dari total; klik baris untuk masuk ke folder itu, tombol naik untuk kembali, dan **Open in file manager** untuk baris atau path saat ini.
- Batasan: satu scan per host pada satu waktu (scan kedua ditolak sampai yang pertama selesai), maksimal 500 entri per tingkat, dan batas waktu 55 detik. Folder yang tidak bisa dibaca (permission denied), scan yang terkena batas waktu, dan daftar yang terpotong ditandai peringatan; ukurannya bisa tidak lengkap.

### Editor Modal Maximize & Tema VS Code Dark+

Saat menyunting skrip, konfigurasi, atau berkas `.env` pada editor CodeMirror terintegrasi:
- **Workspace Modal Maximize**: Klik tombol maximize `⛶` atau klik dua kali (*double-click*) pada header modal untuk memperbesar area editor hingga memenuhi seluruh area layar peramban (100vw &times; 100vh) untuk kenyamanan penelaahan kode yang luas.
- **Palet Tema VS Code Dark+**: Dilengkapi tema gelap modern tersemat (latar belakang `#1e1e1e`, pewarnaan sintaks kontras tinggi untuk *keywords*, *functions*, *variables*, dan *strings*) dengan tipografi monospace modern (`JetBrains Mono`, `Fira Code`, `Consolas`).
- **Deteksi Sintaks Dinamis**: Menyesuaikan mode *syntax highlighting* secara otomatis untuk berkas PHP, JavaScript, Shell, YAML, CSS, HTML, XML, dan C-like berdasarkan ekstensi berkas.

### Pin Berkas dengan Catatan Operasional & Global Bookmarks

Untuk mempermudah akses ke berkas konfigurasi penting, rahasia aplikasi, dan folder log layanan yang aktif ditinjau:

- **Penyematan Berkas & Direktori**: Klik ikon pin (`📌`) pada baris mana pun di File Manager SFTP. Tambahkan catatan operasional opsional (contoh: `Konfigurasi utama nginx produksi` atau `Direktori arsip WAL Postgres`). Baris yang tersemat akan ditandai dengan aksen border dan lencana catatan visual.
- **Bilah Quick Access**: Path berkas/folder yang disematkan untuk host aktif akan muncul sebagai tombol akses cepat (*chips*) pada bilah Quick Access tepat di atas tabel berkas. Mengklik chip akan langsung membuka folder atau memilih berkas terkait.
- **Modal Global Bookmarks**: Akses seluruh berkas tersemat dari semua host secara terpusat dengan mengklik **`🔖 Bookmarks`** pada header navigasi global:
  - **Pencarian Real-Time**: Saring bookmark lintas host, path, dan catatan secara instan.
  - **Editor Terintegrasi**: Buka berkas teks atau konfigurasi tersemat langsung di editor CodeMirror.
  - **Loncat ke Terminal Dock**: Buka tab terminal baru yang secara otomatis telah berada di direktori induk berkas tersemat (`openTerminalForHost` dengan path tujuan).
  - **Sunting Catatan & Lepas Pin**: Perbarui catatan operasional atau lepas sematan berkas langsung dari jendela modal.


### Elevated File Access (Akses Berkas sebagai root atau Pengguna Lain)

Secara default, SFTP File Manager beroperasi menggunakan akun SSH yang terkonfigurasi pada Host. Ketika Anda mencoba menelusuri direktori *home* milik pengguna lain (seperti `/home/pengguna_lain`) atau direktori sistem terproteksi (seperti `/root` atau `/var/log/audit`), host remote akan menolak akses dengan galat `permission denied`.

Pantau memungkinkan eskalasi hak akses berkas ke pengguna `root` atau akun pengguna lokal lain melalui mekanisme `sudo` tanpa kata sandi:

#### Cara Kerja
- **Selektor Access as**: Gunakan menu dropdown **Access as** pada bilah alat File Manager untuk memilih `default` (pengguna SSH bawaan), `root`, atau masukkan nama pengguna target lainnya.
- **Eksekusi SFTP via Sudo**: Alih-alih membuka subsistem SSH standar, Pantau mendeteksi lokasi biner `sftp-server` pada host dan mengeksekusinya melalui SSH *exec channel* dengan perintah `sudo -n -u <user> <sftp-server>`. Pantau kemudian berkomunikasi menggunakan protokol SFTP standar di atas kanal tersebut.
- **Paritas Fitur Penuh**: Seluruh kapabilitas File Manager berjalan di bawah izin pengguna yang dituju: penelusuran direktori, pembacaan dan penyuntingan berkas, pengunggahan, kalkulasi ukuran folder, duplikasi lokal (dilengkapi verifikasi sisa ruang disk), pengunduhan berkas maupun arsip folder (`zip`/`tar.gz`), penghapusan, dan pengubahan hak akses (`chmod`).

#### Prasyarat & Batasan
- **Sudo Tanpa Kata Sandi (*Passwordless Sudo*)**: Pengguna SSH pada host target harus memiliki izin `sudo` tanpa kata sandi (`sudo -n`) untuk beralih ke pengguna target (contoh: konfigurasi `NOPASSWD` di `/etc/sudoers`).
- **Biner Mandiri `sftp-server`**: Host target harus memiliki biner OpenSSH `sftp-server` di salah satu jalur pencarian sistem standar:
  - `/usr/lib/openssh/sftp-server` (Debian, Ubuntu)
  - `/usr/libexec/openssh/sftp-server` (RHEL, CentOS, Fedora, Rocky, AlmaLinux)
  - `/usr/lib/ssh/sftp-server` (Arch Linux, openSUSE)
  - `/usr/libexec/sftp-server` (Gentoo, Alpine, BSD)
  - `/usr/lib64/ssh/sftp-server` (Varian jalur 64-bit)
- **Tidak Kompatibel dengan Internal SFTP**: Server yang konfigurasi OpenSSH-nya hanya menggunakan `Subsystem sftp internal-sftp` bawaan daemon tanpa menyediakan berkas biner mandiri `sftp-server` di disk tidak dapat dielevasi.

#### Prinsip Keamanan Zero-Password
- Pantau tidak pernah meminta, mengirimkan, maupun menyimpan kata sandi untuk `sudo` atau akun target.
- Seluruh upaya eskalasi strictly menggunakan opsi `sudo -n` (*non-interactive*). Jika `sudo` membutuhkan input kata sandi atau pengguna tidak berhak di sudoers, eksekusi perintah seketika dibatalkan secara aman.

#### Penanganan Kegagalan & Pintasan 1-Klik Buka sebagai Root
- **Kegagalan Eskalasi (HTTP 403)**: Jika eskalasi tidak dapat dilakukan (sudo meminta kata sandi, pengguna tidak diizinkan, atau biner `sftp-server` tidak ditemukan), backend merespons dengan status `HTTP 403 Forbidden` disertai pesan teks penjelas. Pantau menampilkan pesan galat tersebut melalui notifikasi *toast* dan secara otomatis mengembalikan mode akses ke pengguna SSH default.
- **Pintasan 1-Klik 'Open as root'**: Saat menelusuri direktori dengan akses default dan terjadi penolakan izin `permission denied` (`HTTP 400 Bad Request`), antarmuka File Manager menyediakan tombol aksi 1-klik **Open as root** untuk langsung mencoba membuka kembali direktori tersebut menggunakan hak akses root.

#### Keamanan & Jejak Audit
- **Hak Akses Baca/Tulis Penuh**: Mode terelevasi memberikan hak penuh akun target (termasuk root). Harap berhati-hati saat memodifikasi atau menghapus berkas sistem kritis.
- **Lencana Visual Aktif**: Lencana visual (*badge*) warna ditampilkan secara jelas pada header File Manager saat mode terelevasi aktif, menunjukkan akun pengguna yang sedang digunakan.
- **Pencatatan Jejak Audit di Activity Console**: Setiap aksi file terelevasi (list, read, write, upload, duplicate, download, delete, chmod) maupun penolakan eskalasi dicatat secara otomatis ke stream log Activity Console (`logger.Info` / `logger.Warn` dengan kategori `files`).
- **Batasan Kepercayaan (*Trust Level*)**: Hak akses berkas terelevasi memiliki tingkat kepercayaan yang setara dengan administrator yang menjalankan perintah `sudo` pada sesi terminal interaktif di host yang memiliki hak sudo.

#### Referensi REST API
- Seluruh rute di bawah `/api/hosts/{id}/files/*` (`list`, `read`, `write`, `upload`, `size`, `duplicate`, `download`, `delete`, dan `chmod` — tidak termasuk `pins`) menerima parameter kueri opsional `as=<username>`.
- Nama pengguna divalidasi dengan regex POSIX `^[A-Za-z_][A-Za-z0-9_.-]{0,31}$`. Nama pengguna yang tidak valid akan ditolak dengan `HTTP 400 Bad Request`.
- Tautan unduhan menggunakan request GET native browser `<a href>`, dengan parameter `as` disertakan langsung pada URL dan diautentikasi melalui cookie sesi aktif.
---

## 6. Fase 5: Pengerasan Keamanan, Snapshot & Pemulihan Bencana

### Two-Factor Authentication (2FA) & 2FA Bypass Flag

Untuk melindungi akses administratif dari kebocoran kata sandi:

1. Masuk ke **Settings** → **Two-Factor Authentication (2FA)**.
2. Pantau akan menampilkan **TOTP Secret** (RFC 6238) beserta kode QR yang di-generate **100% offline** langsung di browser Anda (tanpa dependensi API pihak ketiga).
3. Pindai kode QR menggunakan aplikasi authenticator standar (Google Authenticator, Aegis, 1Password, Bitwarden).
4. Simpan **8 Recovery Code** yang diberikan ke tempat penyimpanan dokumen rahasia offline yang aman.
5. Masukkan 6-digit token untuk mengaktifkan proteksi 2FA.

#### Mitigasi & Akses Darurat:
- **2FA Lockout**: Jika terjadi 3 kali kegagalan verifikasi berturut-turut, sistem otomatis mengunci proses login selama 30 detik untuk menangkis serangan tebak otomatis (*brute-force*).
- **2FA Bypass Flag**: Jika ponsel authenticator dan seluruh Recovery Code hilang, administrator yang memiliki akses shell ke mesin host Pantau dapat me-restart biner dengan flag darurat:
  ```bash
  ./pantau -port 8080 -db /data/pantau.db -disable-2fa
  ```
  Perintah ini akan langsung menonaktifkan konfigurasi TOTP dari SQLite dan memulihkan akses login administrator.

### SSH Key Library Terpusat (Settings → SSH Keys)

Untuk mengeliminasi proses penempelan (*paste*) kunci privat berulang kali di form host, Pantau menyediakan katalog **SSH Key Library** terpusat di menu **Settings** → **SSH Keys**.

#### Menambahkan & Mengelola Kunci
1. Buka **Settings** → **SSH Keys**.
2. Masukkan **Nama** unik (contoh: `aws-staging-cluster`, `hetzner-infra`, atau `db-ansible-key`) dan tempelkan kunci privat OpenSSH tanpa enkripsi dalam format PEM.
3. Klik **Add Key**. Pantau memproses dan memvalidasi kunci di sisi server menggunakan `golang.org/x/crypto/ssh`:
   - Menurunkan (*derive*) dan menyimpan format kunci publik OpenSSH (`authorized_keys`).
   - Menghitung dan menampilkan sidik jari (*fingerprint*) SHA-256 (`SHA256:...`).
   - Menormalisasi karakter baris baru (*trailing newline*).
   - **Kunci dengan Passphrase**: Kunci privat yang diproteksi frasa sandi (*passphrase*) tidak didukung untuk kebutuhan otomasi daemon agentless; mencoba mendaftarkannya akan menghasilkan pesan kesalahan eksplisit (`400 Bad Request`).

#### Aturan Keamanan & Perlindungan Kredensial
- **Zero API Credential Leakage**: Endpoint REST API (`GET /api/ssh-keys`) mengembalikan metadata kunci (`id`, `name`, `public_key`, `fingerprint`, `created_at`, `used_by`), tetapi kolom `private_key` tidak pernah diekspos dalam respons JSON (`json:"-"`).
- **Proteksi Penghapusan Kunci Aktif**: Percobaan menghapus kunci yang masih digunakan oleh satu atau lebih host (`used_by > 0`) akan ditolak secara tegas dengan `HTTP 409 Conflict`. Anda wajib melepaskan atau mengalihkan host terkait terlebih dahulu sebelum menghapus kunci, guna mencegah pemutusan koneksi yang tidak disengaja.
- **Penyensoran Log**: Pencatat log telemetri Pantau menyensor kunci privat dan token rahasia secara deterministik menjadi `[REDACTED]`.
- **Snapshot Terenkripsi**: Seluruh library key disertakan dalam System Snapshot dan disegel dengan enkripsi AES-256-GCM. Saat dipulihkan di server baru, ID kunci dan relasi pemetaan host dipertahankan secara utuh.

#### Referensi REST API Otomasi
Seluruh rute membutuhkan cookie sesi autentikasi yang valid (`authMiddleware`):
- `GET /api/ssh-keys`: Mengembalikan daftar array metadata `[SSHKey...]` beserta jumlah host pengguna (`used_by`).
- `POST /api/ssh-keys`: Mendaftarkan kunci baru dengan body `{"name":"...","private_key":"..."}`. Mengembalikan status `201 Created` beserta metadata kunci. Mengembalikan `400` jika parsing/passphrase gagal, atau `409` jika nama sudah digunakan.
- `DELETE /api/ssh-keys/{id}`: Menghapus kunci jika tidak lagi digunakan. Mengembalikan `200` jika berhasil, `404` jika ID tidak ditemukan, atau `409` jika masih aktif dirujuk oleh host.
- `POST /api/hosts` dan rute update: Menerima parameter `key_id`. Mengisi `key_id` yang tidak valid menghasilkan respons `400 Bad Request`.

### Kanal Notifikasi Multi-Kanal & Debounce Alert (Settings → Notifikasi)

Pantau mengirimkan notifikasi instan ketika terjadi deteksi drift atau pemulihan insiden ke berbagai kanal:
- **Kanal yang Didukung**:
  - **Telegram**: Bot Token dan Chat ID.
  - **Webhook Umum**: HTTP POST dengan muatan payload JSON.
  - **ntfy**: URL Topik (contoh: `https://ntfy.sh/my-topic`), token otorisasi Bearer opsional, dan prioritas tinggi.
  - **Slack**: Incoming Webhook URL.
  - **Discord**: Webhook URL (dipotong otomatis ke 1.800 karakter agar sesuai batas API).
  - **SMTP**: Pengirim email dengan dukungan STARTTLS (port standar 587), TLS langsung (465), atau plaintext, banyak penerima (dipisah koma), dan autentikasi standar.
- **Perlindungan Kolom Rahasia**: Token dan password sensitif (`ntfy_token`, `smtp_pass`) tidak pernah dikembalikan dalam respons GET (hanya flag `*_set` boolean). Pengiriman tanpa kunci mempertahankan rahasia lama, sedangkan nilai kosong menghapusnya.
- **Debounce Alert**: Atur parameter `alert_debounce_runs` (1..10, default 1). Insiden dan notifikasi hanya dipicu bila drift bertahan selama N siklus inspeksi berturut-turut, mencegah alarm palsu akibat anomali jaringan sesaat.
- **Uji Kanal Notifikasi**: Klik **Uji Notifikasi** untuk memicu pengiriman pesan pengujian ke seluruh kanal aktif secara bersamaan dan meninjau badge status keberhasilan masing-masing kanal.
### System Snapshot Terenkripsi & Disaster Recovery GitHub

**System Snapshot** adalah berkas arsip mandiri terenkripsi yang memuat seluruh basis data Pantau (inventaris host, kredensial SSH terenkripsi, aturan Desired State, dan preferensi aplikasi).

#### Membuat & Mengenkripsi Snapshot:
1. Masuk ke **Settings** → **Backup & Disaster Recovery**.
2. Masukkan **Snapshot Passphrase** yang kuat. Pantau akan melakukan derivasi kunci menggunakan algoritma **Argon2id** dan mengenkripsi seluruh database menggunakan **AES-256-GCM**.
3. Unduh berkas `.enc` yang dihasilkan untuk cadangan offline.

#### Sinkronisasi Otomatis ke Remote Storage Provider (GitHub):
- Daftarkan GitHub Personal Access Token (PAT dengan izin `repo`) dan target repositori privat (`username/pantau-backups`).
- Tentukan jadwal sinkronisasi berkala (misal: harian). Pantau akan mengunggah berkas `.enc` terenkripsi secara otomatis ke repositori GitHub privat tersebut.
- **Tips Multi-Server**: Jika Anda menjalankan Pantau di beberapa server (misal: di kantor dan di home lab) dengan repositori GitHub yang sama, ubah kolom **File Path in Repo** di masing-masing server (misal: `snapshots/kantor.enc` dan `snapshots/homelab.enc`) agar backup tidak saling menimpa.


#### Rekonstruksi Kluster dari Nol (Disaster Recovery Wizard):
Jika server master Pantau mengalami kerusakan total:
1. Siapkan binary Pantau baru di mesin baru.
2. Jalankan aplikasi; pada layar inisialisasi awal, pilih opsi **Restore from System Snapshot**.
3. Unggah file `.enc` cadangan Anda atau masukkan kredensial repositori privat GitHub Anda.
4. Masukkan **Snapshot Passphrase**. Pantau akan memverifikasi integritas data, mendekripsi basis data SQLite WAL, dan langsung melanjutkan pemantauan seluruh Host secara otomatis.

---

## 7. Fase 6: Pemeliharaan Sistem & Pembaruan (Update Checker & Self-Update)

Pantau menyediakan siklus pemeliharaan semi-otomatis yang aman untuk menjaga ketersediaan fitur terbaru dan perbaikan keamanan tanpa merusak stabilitas pemantauan.

### Update Checker & Siklus Deteksi Rilis
- **Pengecekan Periodik Latar Belakang**: Secara otomatis memeriksa rilis terbaru di upstream GitHub Releases setiap 12 jam sekali, dengan hasil disimpan dalam cache selama 6 jam untuk mencegah pembatasan kuota rate limit GitHub API.
- **Indikator Status UI**: Jika versi baru terdeteksi, badge status dan catatan rilis (*release notes*) akan ditampilkan secara elegan pada footer dashboard dan modal Settings.
- **Pengecekan Manual**: Administrator dapat memicu pengecekan seketika dengan menekan tombol **Periksa Pembaruan** di menu Pengaturan.
- **Pengecekan via CLI**: Menjalankan `pantau -check-update` di terminal akan meminta metadata rilis upstream, menampilkan nomor versi terbaru, URL rilis, serta mencetak catatan rilis (*release notes*) terformat langsung ke output terminal.

### Proses One-Click Self-Update & Verifikasi Integritas SHA-256
Untuk instalasi binary mandiri (Linux dan Windows):
1. Buka menu **Settings** → **System Updates** dan klik **Perbarui Sekarang**.
2. **Validasi Hash SHA-256**: Pantau mengunduh manifest resmi `checksums.txt` dari rilis upstream dan memverifikasi integritas arsip biner sebelum ekstraksi.
3. **Pre-flight Smoke Test**: Sebelum biner lama diganti, Pantau mengekstrak biner baru ke berkas sementara dan mengeksekusi tes smoke (`pantau.tmp -v`). Jika terjadi kegagalan (misal: binary korup atau ketidaksesuaian arsitektur), operasi dibatalkan dan sistem dikembalikan ke kondisi semula (*automatic rollback*).
4. **Penggantian Biner Aman (Atomic Swap)**:
   - Pada Linux: file aktif ditimpa secara atomik menggunakan `os.Rename`.
   - Pada Windows: file aktif yang sedang terkunci di-rename menjadi `pantau.exe.old`, lalu biner baru dipindahkan ke `pantau.exe`. File `.old` akan dibersihkan pada startup berikutnya.
5. **Graceful Restart & Lock Handover**: Klik tombol **Restart Pantau Sekarang**. Server akan menyelesaikan proses yang sedang berjalan, melepaskan instance guard, dan menutup koneksi database SQLite secara aman (*graceful shutdown*). Biner baru di-spawn dengan argumen konfigurasi yang identik dan memanfaatkan `instance.AcquireWithTimeout` (jeda toleransi hingga 3 detik) untuk menunggu pelepasan file lock oleh proses lama secara mulus, lalu antarmuka web akan otomatis memuat ulang saat server aktif kembali.

### Diferensiasi Lingkungan Docker vs Standalone Binary
- Image Docker resmi di GitHub Container Registry (`ghcr.io/herliansyah/pantau:latest`) mendukung multi-arsitektur secara native untuk platform `linux/amd64` dan `linux/arm64`.
- Jika Pantau dijalankan di dalam container Docker, tombol penggantian biner otomatis dinonaktifkan demi menjaga integritas pola kontainerisasi.
- Antarmuka web akan menampilkan panduan penarikan image resmi:
  ```bash
  docker compose pull && docker compose up -d
  ```

### Kesiapan Lingkungan Airgapped
- Pada instalasi server tertutup tanpa akses internet publik (intranet/airgapped), Update Checker beroperasi secara *fail-silent* dengan batas waktu request pendek (3 detik) sehingga tidak memblokir server maupun memunculkan alarm palsu.
- Administrator juga dapat menonaktifkan fitur pengecekan pembaruan sepenuhnya dengan menyertakan flag startup `-disable-update-check` atau variabel lingkungan `PANTAU_DISABLE_UPDATE_CHECK=true`.

---

## 8. Dokumentasi In-App & Viewer Changelog

Pantau dilengkapi dokumentasi operasional mandiri yang disematkan langsung ke dalam biner berekstensi tunggal via `go:embed`:

- **Workspace Modal Ramah Airgapped**: Akses panduan operasional lengkap secara offline tanpa koneksi internet publik melalui **Documentation Modal** (`75vw × 80vh`).
- **Navigasi Multi-Tab Dokumen**: Membaca ringkasan proyek (**README**), buku panduan teknis (**Panduan Pengguna**), serta catatan rilis historis (**Changelog**) langsung dari dalam antarmuka web.
- **Dukungan Dwibahasa & Pengalih Bahasa**: Beralih antara Bahasa Indonesia dan Bahasa Inggris secara instan menggunakan tombol bahasa `🌐 ID` / `🌐 EN` di dalam modal.
- **Daftar Isi Dinamis (Dynamic Table of Contents)**: Menghasilkan navigasi heading otomatis untuk *smooth-scrolling* langsung ke bab bahasan atau nomor versi rilis yang dituju.
- **Titik Akses Universal**: Modal dokumentasi dapat dibuka sebelum login (via tautan bantuan di layar Initial Setup dan Disaster Recovery) maupun setelah login (melalui tombol `(?)` di header utama, klik badge nomor versi di footer dashboard, atau tautan Changelog di modal Settings).

