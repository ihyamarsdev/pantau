# 0037. Akses File Manager Terelevasi Melalui Sudo SFTP Server Tanpa Kata Sandi

Menyediakan kapabilitas eskalasi hak akses pada pengelola berkas (*Elevated File Access*) di antarmuka SFTP File Manager Pantau: memungkinkan penelusuran, pembacaan, penyuntingan, pengunggahan, pengecekan ukuran, duplikasi lokal, pengunduhan berkas/arsip folder, penghapusan, dan pengubahan izin (*chmod*) sebagai pengguna `root` atau akun pengguna lokal lain secara aman melalui perintah `sudo -n -u <user>` pada subsistem `sftp-server` remote tanpa mentransmisikan atau menyimpan kata sandi apa pun.

## Konteks dan Masalah

Sebelum arsitektur ini diterapkan, antarmuka SFTP File Manager Pantau memiliki keterbatasan hak akses bawaan:
1. **Kegagalan Akses Direktori Pengguna Lain (*Permission Denied*)**: Sesi SFTP dijalankan menggunakan kredensial akun SSH yang terkonfigurasi pada Host. Akun non-root standar tidak memiliki izin akses berkas untuk membaca direktori *home* pengguna lain (misalnya `/home/<pengguna_lain>`) atau direktori sistem terproteksi (seperti `/root`, `/etc/ssl/private`, atau `/var/log/audit`), memunculkan pesan kesalahan *permission denied*.
2. **Ketiadaan Eskalasi Hak Akses Tanpa Sesi Terminal**: Pengguna yang perlu memeriksa konfigurasi atau log milik layanan berhak istimewa terpaksa beralih membuka sesi Terminal PTY interaktif dan menjalankan perintah manual (`sudo su`, `sudo cat`, `sudo nano`), menghilangkan kemudahan navigasi grafis, breadcrumb, duplikasi aman, dan unduhan arsip pada File Manager.
3. **Risiko Keamanan Pengelolaan Kata Sandi**: Mekanisme eskalasi tidak boleh meminta, mentransmisikan, atau menyimpan kata sandi `sudo` di server Pantau maupun di peramban pengguna. Sistem harus bersandar pada aturan otorisasi `sudo` tanpa kata sandi (*passwordless sudo*) yang telah ditetapkan oleh administrator server remote.
4. **Variasi Lokasi Binary `sftp-server` pada Linux**: OpenSSH mendistribusikan berkas biner pembantu `sftp-server` di berbagai path sistem operasi bergantung pada distribusi Linux (seperti Debian/Ubuntu, keluarga RHEL/CentOS, Arch, SUSE, atau BSD), atau mengandalkan implementasi terintegrasi `internal-sftp` di dalam proses `sshd`.
5. **Kebutuhan Audit dan Visibilitas Keamanan**: Operasi file dengan hak istimewa tinggi (`root`) membawa risiko modifikasi atau penghapusan berkas sistem kritis. Diperlukan penanda visual yang tegas pada antarmuka pengguna serta pencatatan jejak audit runtime di Activity Console untuk setiap tindakan yang dieksekusi dalam mode terelevasi.

## Keputusan Arsitektur

1. **Mekanisme Peluncuran SFTP Terelevasi via `sudo -n -u <user>`**:
   - Alih-alih membuka subsistem SFTP standar SSH (`session.RequestSubsystem("sftp")`), Pantau membuka SSH *exec channel* baru dan menjalankan biner `sftp-server` target yang dibungkus dengan perintah:
     ```bash
     sudo -n -u <user> <path-to-sftp-server>
     ```
   - Pantau kemudian menghubungkan pipa `stdin` dan `stdout` dari kanal exec tersebut ke pustaka klien SFTP (`sftp.NewClientPipe(stdout, stdin)`).
   - Seluruh operasi SFTP selanjutnya (listing direktori, pembacaan berkas, penulisan konten, pengunggahan, kalkulasi ukuran, duplikasi lokal via remote shell, pengunduhan berkas tunggal maupun arsip folder `zip`/`tar.gz`, penghapusan, dan `chmod`) berjalan di bawah konteks hak akses pengguna yang dituju.

2. **Prinsip Keamanan Zero-Password (Tanpa Penyimpanan Kata Sandi)**:
   - Pantau memanfaatkan opsi `-n` (*non-interactive*) pada `sudo` yang secara ketat menolak prompt kata sandi interaktif.
   - Pantau **sama sekali tidak pernah meminta, mengirimkan, maupun menyimpan kata sandi pengguna atau kata sandi sudo**.
   - Jika konfigurasi `sudo` pada host membutuhkan kata sandi atau pengguna SSH tidak diizinkan menjalankan `sudo` sebagai akun target, perintah seketika gagal secara aman.

3. **Prasyarat dan Deteksi Biner `sftp-server`**:
   - **Prasyarat Otorisasi**: Pengguna SSH pada host target harus memiliki hak `sudo` tanpa kata sandi (*passwordless sudo*) untuk beralih ke pengguna target (misalnya konfigurasi `/etc/sudoers` dengan `NOPASSWD`).
   - **Deteksi Biner**: Pantau mencari biner executable `sftp-server` pada jalur baku lintas distribusi Linux berikut:
     - `/usr/lib/openssh/sftp-server` (Debian, Ubuntu)
     - `/usr/libexec/openssh/sftp-server` (RHEL, CentOS, Fedora, AlmaLinux, Rocky)
     - `/usr/lib/ssh/sftp-server` (Arch Linux, openSUSE)
     - `/usr/libexec/sftp-server` (Gentoo, Alpine, BSD)
     - `/usr/lib64/ssh/sftp-server` (Distribusi 64-bit varian)
   - **Batasan `internal-sftp`**: Host yang konfigurasi OpenSSH daemon-nya (`sshd_config`) hanya menggunakan modul bawaan `Subsystem sftp internal-sftp` tanpa memasang biner mandiri `sftp-server` tidak dapat menggunakan fitur eskalasi ini.

4. **Validasi Format Akun Pengguna (*Username Validation*)**:
   - Parameter nama pengguna target divalidasi secara ketat di sisi server menggunakan ekspresi reguler standar POSIX:
     ```regexp
     ^[A-Za-z_][A-Za-z0-9_.-]{0,31}$
     ```
   - Karakter berbahaya, spasi, atau injeksi argumen shell akan langsung ditolak dengan kode respon `HTTP 400 Bad Request`.

5. **Spesifikasi REST API (`/api/hosts/{id}/files/*`)**:
   - Seluruh rute manajemen berkas di bawah `/api/hosts/{id}/files/*` (meliputi `list`, `read`, `write`, `upload`, `size`, `duplicate`, `download`, `delete`, dan `chmod`) menerima parameter kueri opsional `as=<username>` (contoh: `?as=root` atau `?as=deploy`).
   - Jika parameter `as` kosong atau tidak disertakan, sistem menggunakan sesi SFTP standar akun SSH terkonfigurasi.
   - Rute penyematan berkas (`/api/hosts/{id}/files/pins`) **tidak** menggunakan parameter `as` karena pin dikelola secara lokal pada basis data Pantau.
   - Karena tautan unduhan berkas menggunakan elemen native peramban `<a href>` GET langsung, parameter `as` disertakan pada URL kueri dan diautentikasi melalui cookie sesi (`pantau_session`).

6. **Penanganan Kegagalan (*Failure Fallback*) dan Pintasan 'Open as root'**:
   - **Kegagalan Eskalasi (HTTP 403)**: Jika eskalasi gagal (misalnya karena `sudo` memerlukan kata sandi, pengguna tidak diizinkan di sudoers, atau biner `sftp-server` tidak ditemukan), backend mengembalikan status `HTTP 403 Forbidden` dengan pesan teks satu baris yang jelas (contoh: `elevated access as "root" is unavailable on this host: sudo requires a password or the user is not permitted`). Antarmuka web menampilkan pesan tersebut dalam bentuk notifikasi *toast* dan otomatis mengembalikan mode akses ke pengguna default.
   - **Pintasan 1-Klik Saat Izin Ditolak**: Pada sesi default non-terelevasi, jika navigasi direktori gagal dengan status `HTTP 400 Bad Request` yang mengandung pesan `permission denied`, antarmuka pengguna menyediakan tombol aksi satu klik untuk membuka direktori tersebut sebagai `root` (*Open as root*).

7. **Visibilitas Antarmuka dan Jejak Audit (*Activity Console*)**:
   - **Selektor Mode Akses**: Bilah navigasi File Manager menyediakan pemilih *Access as* (Default / root / pengguna kustom).
   - **Lencana Visual Aktif**: Ketika mode terelevasi aktif, antarmuka menampilkan lencana visual (*badge*) warna yang jelas mengindikasikan akun pengguna yang sedang digunakan.
   - **Jejak Audit Real-Time**: Setiap aksi berkas yang dieksekusi dalam mode terelevasi maupun kegagalan penolakan eskalasi dicatat ke dalam log runtime Pantau (`logger.Info` / `logger.Warn` pada kategori `files`) dan disiarkan langsung ke Activity Console.
   - **Tingkat Kepercayaan (*Trust Level*)**: Mode ini memiliki tingkat kepercayaan yang identik dengan hak administrator yang membuka terminal shell dan menjalankan perintah `sudo` pada host terkait.

## Konsekuensi

- **Akses Pengelolaan Berkas Tanpa Batas**: Administrator dapat meninjau, mengedit, dan mengunduh berkas sistem sensitif atau berkas antar pengguna langsung dari UI web tanpa hambatan izin akses.
- **Keamanan Kredensial Maksimal**: Tidak ada risiko kebocoran kata sandi karena Pantau murni mengandalkan otorisasi lokal non-interaktif `sudo -n`.
- **Transparansi Operasional**: Semua tindakan dengan hak istimewa tinggi tercatat pada Activity Console dan ditandai oleh lencana visual di antarmuka.
- **Ketergantungan Konfigurasi Host Target**: Fitur ini membutuhkan hak `sudo` tanpa kata sandi pada user SSH target serta ketersediaan berkas biner `sftp-server` di sistem remote; server yang hanya mengonfigurasi `internal-sftp` tidak dapat memanfaatkan fitur ini.
