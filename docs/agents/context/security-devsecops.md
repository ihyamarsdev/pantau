# Security & DevSecOps

Domain reference for security scanning, hardening, firewall, 2FA and the secrets vault. Read this when working in security handlers, `internal/vault/` or hardening code.

**Security Scan**:
Proses evaluasi keamanan berkala atau on-demand pada Host secara agentless dan pasif (read-only) untuk mengidentifikasi pembaruan keamanan paket sistem tertunda, kebutuhan reboot, kesegaran metadata repositori, serta postur pengerasan konfigurasi server tanpa ketergantungan database CVE eksternal.
_Avoid_: Vulnerability assessment, CVE scan, pen-test run, antivirus scan

**Security Finding**:
Satu butir temuan paket perangkat lunak sistem yang memiliki pembaruan patch keamanan yang belum diterapkan, memuat nama paket, versi terpasang saat ini, versi perbaikan yang tersedia, tingkat keparahan (*severity*), serta pengenal advisori keamanan (seperti RHSA, ALAS, atau CVE).
_Avoid_: Vulnerability item, CVE finding, security bug, alert ticket

**Hardening Check**:
Satu butir verifikasi pasif terhadap praktik terbaik keamanan konfigurasi sistem Host (seperti pembatasan login root SSH, autentikasi sandi SSH, keaktifan update keamanan otomatis, dan status firewall aktif) yang menghasilkan status kelulusan (*pass*, *warn*, *fail*, atau *unknown*).
_Avoid_: Security compliance rule, CIS benchmark check, audit rule

**Firewall Rule**:
Satu aturan penyaringan lalu lintas pada firewall host (ufw, firewalld, atau iptables) yang dibaca dan dikelola Pantau secara langsung melalui SSH dari tab Security: dibaca, ditambah, diubah, dihapus. Berbeda dari **Hardening Check** `firewall_active` yang hanya menilai keaktifan firewall secara pasif, dan dari **Port Exposure** yang hanya mengklasifikasikan keterbukaan port. Aturan tanpa syarat pada port SSH yang dipakai Pantau dilindungi dari penghapusan dan pemblokiran.
_Avoid_: Security group, ACL entry, NAT rule

**Elevated File Access**:
Akses manajemen berkas SFTP dengan eskalasi hak istimewa pengguna remote (seperti `root` atau akun pengguna lain) yang dijalankan melalui `sudo -n -u <user>` pada subsistem server SFTP remote tanpa transmisi atau penyimpanan kata sandi, memungkinkan penelusuran direktori, pembacaan, penulisan, pengunggahan, pengecekan ukuran, duplikasi, pengunduhan berkas dan arsip folder, penghapusan, serta chmod pada berkas yang dibatasi oleh izin akun SSH standar.
_Avoid_: Sudo SFTP, root file manager, admin file browse, privileged explorer

**TOFU (Trust On First Use)**:
Mekanisme verifikasi host key SSH otomatis pada koneksi pertama yang mencatat fingerprint host untuk memvalidasi koneksi berikutnya dan mendeteksi serangan Machine-in-the-Middle (MITM) tanpa memerlukan konfigurasi manual `known_hosts`.
_Avoid_: Insecure ignore host key, manual known hosts, strict pinning

**Shell Quote Escaping**:
Mekanisme pembungkusan argumen perintah jarak jauh menggunakan tanda kutip tunggal POSIX (`'...'`) universal yang menetralkan ekspansi variabel shell (`$`, backtick, operator piping) di seluruh versi Linux dari kernel lawas (CentOS 5) hingga modern.
_Avoid_: Double quote interpolation, shell sanitization, cmd escape

**Exploit Intelligence**:
Penanda opt-in (`vuln_intel_enabled`) pada Security Finding yang CVE-nya tercantum di katalog CISA Known Exploited Vulnerabilities (sedang dieksploitasi) beserta skor EPSS (peluang dieksploitasi dalam 30 hari), diambil server Pantau sekali sehari dari feed publik dan tidak disimpan; host yang tidak melaporkan ID CVE (saat ini selain dnf/yum) berstatus tidak dinilai.
_Avoid_: Threat feed, CVE scan, vulnerability score, risk score

**Vulnerability Database List**:
Daftar referensi database dan feed kerentanan dunia (salinan tak diubah dari haxdoggy/vulnerability-databases, CC BY 4.0) yang disematkan di biner dan ditampilkan dari tab Security dengan atribusi serta penanda sumber yang dipakai Pantau.
_Avoid_: CVE source list, intel catalog, feed directory

**Container Misconfiguration Audit**:
Penilaian read-only atas konfigurasi container yang berjalan di sebuah Host terhadap katalog aturan tetap (privileged, socket Docker, jalur host sensitif, kapabilitas, namespace, profil keamanan, user, port publik, tag image, batas sumber daya), menghasilkan temuan per container dengan tingkat keparahan dan perbaikan yang disarankan.
_Avoid_: Vulnerability scan, Image scan, Container hardening, Baseline check

**Image Vulnerability Scan**:
Pemindaian kerentanan paket pada sebuah image Docker memakai pemindai yang sudah terpasang di Host (trivy atau grype; Pantau tidak pernah memasangnya), menghasilkan jumlah kerentanan per tingkat keparahan beserta daftar CVE, versi terpasang, dan versi yang sudah diperbaiki.
_Avoid_: Container misconfiguration audit, Image update check, Package upgrade, Registry scan

**File Integrity Monitoring (FIM)**:
Aturan Desired State `file_integrity` yang membandingkan sha256 berkas terpilih di Host (path absolut, `preset:auth`, atau semua biner SUID/SGID lewat target `suid`) dengan Baseline yang secara sadar disetujui administrator; berkas berubah, hilang, atau bertambah dilaporkan sebagai drift beserta namanya, dan berkas yang tidak terbaca berstatus *not assessed*, bukan ok.
_Avoid_: Checksum monitor, Hash watchdog, Integrity scanner

**FIM Baseline**:
Kumpulan path dan digest sha256 yang diambil dari Host lewat aksi *Capture baseline* dan dikonfirmasi administrator sebagai keadaan yang diinginkan; tidak pernah dibuat diam-diam pada evaluasi pertama.
_Avoid_: Implicit baseline, Auto-accept, First-run baseline

**Account Audit**:
Inventaris read-only akun login lokal, berkas `authorized_keys`, status kata sandi, hak sudo, dan keanggotaan grup sebuah Host yang dicocokkan dengan SSH Key Library lewat sidik jari SHA-256; bagian yang tidak terbaca berstatus unknown, bukan bersih.
_Avoid_: User scan, Key list, Access review

**Unknown Key**:
Kunci publik di `authorized_keys` Host yang sidik jarinya tidak ada di SSH Key Library Pantau.
_Avoid_: Foreign key, Rogue key

**Fleet Keys**:
Tampilan lintas Host yang mengelompokkan kunci `authorized_keys` yang sama beserta Host dan akun yang memakainya, dengan filter unknown-only dan weak-only.
_Avoid_: Global keys, Key registry

**Known Package Vulnerability**:
Kerentanan yang diketahui pada paket terpasang di sebuah Host, diperiksa (opt-in `osv_enabled`) terhadap OSV (Debian/Ubuntu/Alpine) atau Arch Linux Security Tracker (pacman) dari inventaris paket, beserta ID advisory, CVE, severity, dan versi perbaikan. Berbeda dari Security Finding (pembaruan keamanan tertunda); rilis OS yang tidak tercakup feed berstatus *not assessed*, bukan bersih.
_Avoid_: Vulnerability scan, CVE scan, Package audit

**IaC Scan**:
Pemeriksaan statis bawaan Pantau (tanpa biner eksternal dan tanpa jaringan) atas sumber di server Pantau (repositori Git, workspace Terraform, proyek Ansible) untuk rahasia yang ter-hardcode (selalu disamarkan) dan konfigurasi Terraform, Ansible, Dockerfile/compose yang tidak aman.
_Avoid_: SAST, Secret scanning, Vulnerability scan

**IaC Gate**:
Penjaga opsional (`iac_gate_enabled`, default mati) yang membuat apply Terraform dan run Ansible ditolak dengan HTTP 409 selama ada temuan IaC Scan critical atau high, kecuali permintaan membawa `ignore_iac_gate`.
_Avoid_: Policy gate, Approval gate

**Two-Factor Authentication (2FA)**:
Mekanisme pengamanan lapis kedua berbasis TOTP (Time-based One-Time Password) opsional untuk autentikasi sesi administrator Pantau.
_Avoid_: Multi-factor authentication, MFA, second password

**TOTP Secret**:
Kunci rahasia bersama (shared secret Base32) yang disimpan di basis data Pantau dan dipasangkan ke aplikasi authenticator pengguna untuk menghasilkan token 6-digit periodik (RFC 6238).
_Avoid_: 2FA key, auth seed, pairing token

**Recovery Code**:
Kumpulan kode acak satu-kali-pakai yang di-generate saat aktivasi 2FA untuk memulihkan akses login ketika administrator tidak dapat mengakses perangkat authenticator.
_Avoid_: Backup pin, emergency password, rescue code

**2FA Bypass Flag**:
Opsi argumen baris perintah (`-disable-2fa`) pada binary Pantau untuk menonaktifkan 2FA secara darurat melalui akses terminal host master.
_Avoid_: Emergency unlock, force reset flag

**2FA Lockout**:
Penangguhan sementara proses verifikasi login (jeda pendinginan 30 detik setelah 3 kali kegagalan berturut-turut memasukkan token TOTP atau Recovery Code) untuk memitigasi upaya serangan tebak token otomatis (brute-force).
_Avoid_: Account ban, login block, brute-force penalty

**Secrets Vault**:
Fasilitas terpusat di Pantau untuk menyimpan, memversi, memutar, dan menyuntikkan rahasia (token, kata sandi, kunci API) dengan enkripsi AES-256-GCM *at rest* di bawah kunci master 32 byte, disensor dari seluruh log, dan tidak pernah diekspos sebagai plaintext kecuali lewat endpoint reveal eksplisit; kunci master disimpan di tabel `settings` (`vault_master_key`) sehingga ikut terlindungi dalam System Snapshot terenkripsi (lihat [ADR-0084](../../../docs/adr/0084-universal-secrets-vault-and-env-sync.md)).
_Avoid_: Password manager, credential store, keychain, env file store

**Vault Secret**:
Satu entri rahasia di Secrets Vault yang memiliki kunci unik, nilai terenkripsi, cakupan, nomor versi, metadata rotasi (`expires_at`/`rotation_days`), dan riwayat perubahan; nilai mentahnya hanya bisa dibaca lewat aksi reveal yang tercatat di audit.
_Avoid_: Config value, environment variable, plain credential

**Secret Scope**:
Cakupan berlaku sebuah Vault Secret — `global` (seluruh sistem), `group` (satu Host Group), `host` (satu Host), atau `cluster` (satu Kubernetes Cluster) — yang menentukan ke mana rahasia disuntikkan saat sinkronisasi.
_Avoid_: Visibility level, permission tier, access group

**.env Template**:
Teks dengan placeholder `{{ vault.KEY }}` yang disimpan di Secrets Vault dan dirender menjadi berkas `.env` atau `Secret` Kubernetes; kunci placeholder divalidasi terhadap daftar rahasia yang dikenal dan nilai yang dirender selalu ditampilkan sebagai `[REDACTED]` di pratinjau.
_Avoid_: Config file, env dump, dotenv snapshot

**Sync Target**:
Definisi tujuan sinkronisasi sebuah `.env Template`: Host (penulisan berkas `.env` lewat SFTP mode 0600 dengan verifikasi SHA-256 dan *post-sync hook* opsional) atau Kubernetes (`v1/Secret` lewat `KubeApply` dengan id klaster negatif), lengkap dengan status drift terakhir.
_Avoid_: Destination, deploy target, remote path

**Drift Status (Vault)**:
Status hasil perbandingan antara nilai rahasia yang tersimpan dan nilai aktual di Sync Target — `unknown` (belum pernah disinkronisasi), `in-sync` (SHA-256 cocok), `drift` (berbeda), atau `error` (target tidak terjangkau); ditampilkan sebagai badge pada workspace vault.
_Avoid_: Sync state, mismatch flag, health badge

**Secret Rotation**:
Proses memperbarui nilai Vault Secret yang mengarsipkan ciphertext lama sebagai versi baru; rollback juga menciptakan versi baru sehingga riwayat tetap lengkap, dengan penanda `expires_at` dan `rotation_days` yang memicu peringatan kedaluwarsa.
_Avoid_: Key reset, password change, token refresh

**Blind Injection**:
Mekanisme penyuntikan rahasia ke proses (Terraform `TF_VAR_*`/env, *extra vars* Ansible, env Docker `-e`) hanya lewat memori tanpa berkas plaintext di disk, dan tanpa pernah memasukkan nilai ke konteks AI Agent — tool agent hanya mengembalikan metadata (`list_vault_keys`) dan status (`trigger_env_sync`) agar model bekerja dengan placeholder.
_Avoid_: Secret injection, env export, credential passthrough

**Protected Path**:
Lokasi direktori sistem operasi atau file inti pada Host Linux yang dibatasi secara permanen dari operasi manipulasi (tulis, ubah, unggah, dan hapus) melalui antarmuka manajemen file.
_Avoid_: Blocked folder, blacklisted path, system lock
