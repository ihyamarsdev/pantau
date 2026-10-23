# 0036. Pemindaian Keamanan Host dan Pemeriksaan Hardening Berbasis Agentless

Menyediakan kapabilitas pemindaian keamanan host (*Host Security Scan*) fase 1 secara agentless dan mandiri (*airgapped-compatible*): mendeteksi pembaruan keamanan tertunda (*pending security updates*) pada paket sistem (apt, dnf, yum), kebutuhan reboot (*reboot-required*), kesegaran cache repositori lokal, serta 4 pemeriksaan pengerasan sistem (*hardening checks*) pasif tanpa memerlukan instalasi agen remote, tanpa hak akses root/sudo, dan tanpa ketergantungan koneksi internet atau basis data kerentanan eksternal. Hasil pemindaian ditampilkan pada tab antarmuka "Security", dapat dijadwalkan secara berkala, serta dapat ditegakkan sebagai aturan kepatuhan (*Desired Rule*) berjenis `security_updates`.

## Konteks dan Masalah

Sebelum arsitektur ini diterapkan, Pantau belum memiliki visibilitas terpadu mengenai postur keamanan sistem operasi host:
1. **Ketiadaan Visibilitas Patch Keamanan**: Administrator tidak mengetahui apakah paket penting pada host remote memiliki pembaruan keamanan mendesak (*security advisories*) tanpa login manual satu per satu ke setiap server.
2. **Kebutuhan Reboot yang Tersembunyi**: Setelah kernel atau pustaka inti diperbarui, server sering kali membutuhkan reboot (`reboot-required`), namun hal ini kerap terabaikan hingga menimbulkan risiko operasional atau kerentanan aktif.
3. **Konfigurasi SSH dan Firewall yang Lemah**: Kesalahan umum seperti mengizinkan login root langsung dengan kata sandi pada OpenSSH atau menonaktifkan firewall host sering kali luput dari pemantauan berkala.
4. **Kendala Lingkungan Terisolasi (Airgapped & Zero-Agent)**: Solusi scanner keamanan konvensional (misal: OpenVAS, Nessus, Trivy, Wazuh) umumnya mengharuskan pemasangan agen berat di remote host, menjalankan daemon latar belakang, atau memerlukan koneksi keluar ke database CVE publik di internet. Hal ini melanggar prinsip desain Pantau yang beroperasi 100% agentless via SSH standar dan harus berfungsi penuh pada jaringan tertutup (*airgapped/isolated VPC*, merujuk ADR-0013).
5. **Bahaya "Ilusi Rasa Aman" (*False Sense of Security*)**: Banyak alat monitoring menampilkan status hijau ("aman/0 kerentanan") ketika perintah gagal dieksekusi atau repositori paket sudah kedaluwarsa berbulan-bulan. Pantau membutuhkan aturan kejujuran (*honesty rule*): sistem dilarang mengklaim aman apabila cakupan evaluasi tidak lengkap.

## Keputusan Arsitektur

1. **Prinsip Agentless, Read-Only, dan Airgapped (Zero-CVE Network Call)**:
   - Pemindaian dieksekusi murni melalui sesi SSH non-interaktif standar menggunakan user SSH terkonfigurasi.
   - **Perintah Baca Saja (Read-Only)**: Seluruh perintah remote bersifat non-destruktif. Sistem **dilarang menggunakan `sudo`**, dilarang memasang paket baru, dan dilarang menjalankan perintah pembaruan repositori jaringan seperti `apt-get update` atau `dnf makecache`.
   - **Mandiri & Offline (Airgapped)**: Evaluasi sepenuhnya bersandar pada metadata paket lokal yang telah tersedia di host target. Tidak ada pencarian API CVE eksternal atau koneksi internet publik. Pemetaan database kerentanan eksternal hanya dimungkinkan sebagai modul opsional di masa mendatang (*future opt-in*), bukan bagian dari rilis ini.

2. **Deteksi Pembaruan Keamanan per Package Manager**:
   - **Debian / Ubuntu (`apt`)**:
     - Menjalankan simulasi peningkatan paket: `apt-get -s dist-upgrade` atau `apt-get -s upgrade`.
     - Paket diklasifikasikan sebagai pembaruan keamanan apabila suite asal rilis (*origin/suite*) mengandung substring `-security` (seperti Debian `*-security`, Ubuntu `*-security`, atau saluran `ESM` pada Ubuntu Pro).
     - Tingkat keparahan (*severity*) untuk `apt` dikategorikan sebagai `unknown` karena utilitas `apt-get` bawaan tidak memaparkan metadata keparahan secara eksplisit tanpa advisory identifier.
   - **RHEL / CentOS / Rocky / AlmaLinux / Fedora / Amazon Linux (`dnf` / `yum`)**:
     - Menggunakan utilitas native `dnf -q updateinfo list security`, `yum updateinfo list security`, atau `yum --security check-update`.
     - Tingkat keparahan diekstraksi langsung dari klasifikasi upstream vendor: `Critical`, `Important`, `Moderate`, dan `Low`.
     - Identifier advisori (seperti `RHSA-*`, `ALAS-*`, atau ID CVE) diparsing dan disimpan pada field `advisory`.
   - **Alpine Linux / Arch Linux / Distro Lain**:
     - Ditetapkan sebagai `status: unsupported` dengan catatan informatif (*note*), namun pemeriksaan hardening tetap dieksekusi.
   - **Distro Legacy (CentOS 6/7, Debian 7+)**:
     - Parser toleran terhadap ketiadaan `/etc/os-release`, ketiadaan `dnf`, maupun variasi format output lawas sehingga tidak menggagalkan siklus inspeksi.

3. **Indikator Reboot Required dan Kesegaran Cache Repositori**:
   - **Reboot Required**: Mendeteksi keberadaan berkas `/var/run/reboot-required` (Debian/Ubuntu) atau mengevaluasi kode keluar dari perintah `needs-restarting -r` (keluarga RHEL).
   - **Kesegaran Cache Repositori (`repo_cache_age_hours`)**:
     - `apt`: Memeriksa mtime berkas `/var/lib/apt/periodic/update-success-stamp` atau direktori `/var/lib/apt/lists`.
     - `dnf` / `yum`: Memeriksa mtime direktori `/var/cache/dnf` atau `/var/cache/yum`.
     - Jika usia cache lebih dari 168 jam (7 hari), sistem menambahkan catatan: `"package metadata is N days old; results may miss recent advisories"` dan menurunkan status keseluruhan pemindaian menjadi `partial`.

4. **Empat Pemeriksaan Hardening Pasif (Read-Only)**:
   - Evaluasi menghasilkan status `pass`, `warn`, `fail`, atau `unknown`:
     1. `sshd_permit_root_login`: Membaca konfigurasi efektif OpenSSH dari `/etc/ssh/sshd_config` dan berkas drop-in `/etc/ssh/sshd_config.d/*.conf` (menerapkan aturan *first-match-wins* OpenSSH). Nilai default OpenSSH (`prohibit-password`) atau `no` menghasilkan `pass`; nilai eksplisit `yes` menghasilkan `fail`.
     2. `sshd_password_auth`: Autentikasi sandi SSH. Nilai `no` menghasilkan `pass`; nilai `yes` atau default tidak terkonfigurasi menghasilkan `warn`.
     3. `auto_security_updates`: Mendeteksi keaktifan pembaruan otomatis (paket `unattended-upgrades` aktif pada Debian/Ubuntu, atau systemd timer `dnf-automatic` / `yum-cron` aktif pada RHEL).
     4. `firewall_active`: Memeriksa keaktifan `ufw`, daemon `firewalld`, atau aturan filter `nftables`/`iptables`. Jika izin akses non-root tidak memungkinkan pembacaan aturan firewall, status dilaporkan sebagai `unknown` disertai rincian detail.

5. **Prinsip Kejujuran Cakupan (*Honesty Rule / Honest Coverage*)**:
   - Pantau pantang menampilkan status "aman / hijau" palsu jika pemeriksaan tidak memiliki cakupan penuh.
   - Jika sistem operasi tidak didukung (`unsupported`), pembacaan perintah pengecekan pembaruan terhambat oleh izin akses non-root (`partial`), atau cache repositori kedaluwarsa lebih dari 7 hari (`partial`), kondisi ini wajib dimunculkan secara transparan pada kolom `status` dan daftar `notes`, bukan disamarkan sebagai 0 temuan.
   - Pemeriksaan hardening yang tidak dapat dievaluasi (misalnya status firewall butuh root) berstatus `unknown` dan dicatat di `notes`, tetapi tidak menurunkan `status` pemindaian karena data pembaruan tetap lengkap. Untuk `unsupported`/`error`, antarmuka menampilkan angka pembaruan, severity, dan status reboot sebagai "tidak dinilai", bukan 0.
   - Status pemindaian terdiri atas 4 nilai tegas: `ok`, `partial`, `unsupported`, dan `error`.

6. **Model Data SQLite dan Penyimpanan Hasil Pemindaian**:
   - Tabel `security_scans` pada basis data `pantau.db`:
     ```sql
     CREATE TABLE IF NOT EXISTS security_scans (
         host_id INTEGER PRIMARY KEY REFERENCES hosts(id) ON DELETE CASCADE,
         scanned_at DATETIME NOT NULL,
         status TEXT,
         package_manager TEXT,
         os_family TEXT,
         pending_security INTEGER,
         pending_total INTEGER,
         severity_json TEXT,
         reboot_required INTEGER,
         repo_cache_age_hours INTEGER,
         findings_json TEXT,
         hardening_json TEXT,
         notes_json TEXT,
         error TEXT
     );
     ```
   - Setiap host hanya menyimpan 1 baris rekaman hasil pemindaian keamanan terbaru (*latest scan*).
   - Data pemindaian keamanan disimpan dalam bentuk teks/JSON terstruktur standar di SQLite dan **tidak disertakan dalam snapshot terenkripsi Pantau** karena bersifat data turunan (*derived telemetry*) yang dapat diproduksi ulang melalui pemindaian ulang.

7. **Penegakan Aturan Kepatuhan (*Desired Rule: `security_updates`*)**:
   - Mendukung Desired Rule baru:
     - `Kind`: `security_updates`
     - `Target`: `security` (tetap/fixed)
     - `Expected`: `none` atau `<=N` (dengan `none` setara dengan `<=0`)
   - **Evaluasi Berbasis Cache**: Evaluasi aturan ini murni membaca baris hasil pemindaian terbaru di database lokal tanpa melakukan panggilan SSH tambahan saat siklus evaluasi berlangsung.
   - **Kriteria Kepatuhan & Drift**:
     - Berstatus `ok` jika `pending_security <= N`.
     - Berstatus `drift` jika `pending_security > N`, dengan `rootCause` memuat daftar paket keamanan tertunda (nama paket, versi terpasang -> versi perbaikan, dan advisori).
     - Berstatus `drift` jika belum pernah dipindai (`current: not_scanned`), data pemindaian kedaluwarsa lebih dari 72 jam (`current: stale`), atau status pemindaian terakhir adalah `unsupported` / `error`, disertai pesan alasan yang jelas. Kepatuhan tidak dapat diklaim tanpa data valid yang segar.

8. **Penjadwalan Otomatis dan Proteksi Anti-Flood**:
   - Konfigurasi interval pemindaian berkala `security_scan_interval_hours` (default `"24"` jam, dapat diatur `1`–`720` jam, atau diisi `0` untuk menonaktifkan pemindaian latar belakang).
   - Pekerja latar belakang (`main.go`) berjalan setiap 10 menit untuk memindai host yang belum pernah dipindai atau yang scan terakhirnya telah melampaui interval, dibatasi maksimal 2 pemindaian konkruen dengan jeda awal 2 menit saat startup.
   - Pemindaian manual via UI/API dilindungi batas jeda (*cooldown*) 60 detik (`HTTP 429 Too Many Requests`) dan kunci konkurensi host tunggal (`HTTP 409 Conflict`).

9. **REST API Endpoints**:
   - `GET /api/hosts/{id}/security`: Mengembalikan status pemindaian terakhir. Menghasilkan `{"scanned": false}` jika belum pernah dipindai, atau `{"scanned": true, "scan": <SecurityScan>}`. Menyertakan header `X-Scan-Cooldown-Remaining: <sec>` jika jeda cooldown masih aktif.
   - `POST /api/hosts/{id}/security/scan`: Memicu pemindaian sinkron dengan batas timeout 90 detik. Mengembalikan HTTP 429 jika dalam cooldown, HTTP 409 jika sedang berlangsung, atau HTTP 200 dengan data scan terbaru saat berhasil.

## Konsekuensi

- **Visibilitas Patch Tanpa Agen Tambahan**: Administrator dapat memantau postur patch keamanan dan urgensi reboot di seluruh kluster server Linux langsung dari antarmuka web Pantau tanpa beban daemon pihak ketiga.
- **Transparansi Kepatuhan Penuh**: Menghilangkan false positive status "aman" berkat *honesty rule* yang secara eksplisit melaporkan keterbatasan izin baca, cache basi, atau distro yang tidak didukung.
- **Aman untuk Lingkungan Tertutup (Airgapped)**: Tidak memerlukan pembukaan akses internet keluar menuju repositori atau database CVE eksternal.
- **Beban Remote Minimal**: Seluruh operasi berlangsung read-only tanpa `sudo`, tidak mengubah berkas di host target, dan dibatasi waktu eksekusi ketat.
