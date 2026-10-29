# 0038. Dukungan Host macOS (Darwin) Berbasis Agentless

Menyediakan kapabilitas pemantauan dan pengelolaan host macOS (Darwin) kelas satu (*first-class support*) secara agentless melalui SSH standar: mengumpulkan metrik performa sistem (RAM berdasar kalkulasi Activity Monitor, swap `vm.swapusage`, kapasitas disk APFS volume Data, throughput I/O disk via `ioreg`, egress jaringan via antarmuka fisik `en*`, core CPU, model perangkat, nama versi OS), mengaudit telemetri proses aman via `ucomm`, mengevaluasi aturan Desired State (disk APFS, service launchd, port lsof, backup stat), menjalankan diagnostik jaringan portabel, serta menyediakan audit keamanan pasif (pembaruan via `softwareupdate` dan 4 pemeriksaan hardening macOS) tanpa memerlukan instalasi agen remote, tanpa merusak perilaku inspeksi Linux yang ada, dan diverifikasi langsung pada perangkat Apple silicon (macOS 26 Tahoe).

## Konteks dan Masalah

Sebelum arsitektur ini diterapkan, seluruh mekanisme pengumpulan telemetri dan inspeksi Pantau dirancang secara eksklusif untuk sistem operasi Linux:
1. **Kegagalan Eksekusi Skrip Poliglota Linux pada Darwin**: Skrip batch pengumpulan metrik sistem Pantau (`SystemMetricsBatchCmd`) mengandalkan perkakas dan antarmuka khusus kernel Linux seperti `free`, `/proc/meminfo`, `/proc/diskstats`, `/proc/net/dev`, `/proc/cpuinfo`, `nproc`, `ss`, dan `/etc/os-release`. Saat terhubung ke host macOS, seluruh perintah tersebut tidak ditemukan (*command not found*), menyebabkan metrik RAM, swap, core prosesor, model hardware, throughput jaringan, dan disk I/O bernilai kosong atau gagal terbaca.
2. **Keterbatasan Lingkungan Non-Interactive SSH pada macOS**: Sesi SSH non-interaktif di macOS menggunakan default `PATH=/usr/bin:/bin:/usr/sbin:/sbin`. Direktori perkakas pihak ketiga seperti Homebrew (`/opt/homebrew/bin`, `/usr/local/bin`) dan Docker Desktop (`/Applications/Docker.app/Contents/Resources/bin`) tidak berada di dalam PATH. Selain itu, shell bawaan `/bin/bash` di macOS masih mempertahankan versi lawas 3.2.57 yang tidak mendukung array asosiatif maupun ekspansi string modern seperti `${var,,}`.
3. **Perbedaan Fundamental Paradigma Sistem Operasi macOS**:
   - **Kalkulasi Memori Riil**: Utilitas `vm_stat` di Darwin menyajikan metrik halaman memori (*pages*) dengan ukuran 16384 byte pada Apple silicon. Perhitungan memori terpakai yang akurat harus merefleksikan definisi Activity Monitor Apple, bukan rumus Linux `MemTotal - MemAvailable`.
   - **Struktur Penyimpanan APFS**: Volume root `/` pada macOS modern adalah volume sistem tersegel (*sealed system volume*) yang berukuran statis (~12 GB). Seluruh data operasional pengguna dan aplikasi disimpan di dalam APFS container bersama pada `/System/Volumes/Data`. Pengukuran disk pada `/` memberikan persentase kapasitas yang keliru.
   - **Trafik Jaringan dan Perangkat Virtual**: Utilitas `netstat -ibn` pada Darwin mencantumkan banyak antarmuka internal jembatan atau virtual (`bridge`, `utun`, `awdl`, `anpi`, `lo`, `gif`, `stf`) yang menduplikasi byte transfer atau membiaskan total egress.
   - **Disk I/O**: Tidak ada `/proc/diskstats`; utilitas `iostat` tidak memaparkan byte transfer kumulatif yang konsisten, sementara data riil tersimpan dalam properti IOKit registry (`ioreg -rd1 -c IOBlockStorageDriver`).
   - **Perbedaan Parameter Diagnostik**: Opsi `-W` pada utilitas `ping` macOS bermakna milidetik (bukan detik seperti di Linux), format ringkasan keluaran ping berbeda (`round-trip min/avg/max/stddev`), dan perintah `timeout` atau `gtimeout` tidak tersedia secara bawaan.
4. **Risiko Kebocoran Rahasia pada Argumen Perintah (`ps -o comm`)**: Pada implementasi BSD/Darwin `ps`, format `-o comm` sering kali mencetak baris perintah lengkap beserta seluruh argumen (*full command-line string*). Hal ini berisiko merekam dan membocorkan token autentikasi, kata sandi, atau rahasia yang diteruskan pada argumen perintah ke dalam basis data dan antarmuka pemantauan Pantau.
5. **Keharusan Nol Regresi Linux (*Zero Regression*)**: Seluruh penyesuaian untuk mendukung macOS sama sekali tidak boleh mengubah perilaku, performa, atau format keluaran telemetri pada host Linux yang telah berjalan.

## Keputusan Arsitektur

1. **Deteksi Sistem Operasi Target via `uname -s`**:
   - Skrip batch inspeksi utama (`SystemMetricsBatchCmd`) menggunakan percabangan shell POSIX standar:
     ```sh
     if [ "$(uname -s)" = "Darwin" ]; then
         # Skrip koleksi telemetri native macOS
     else
         # Skrip koleksi telemetri Linux (UTUH & TIDAK BERUBAH)
     fi
     ```
   - Skrip macOS memancarkan 20 bagian data terpisah pembatas `---` yang identik dengan kontrak Linux, menghasilkan data terstruktur yang langsung kompatibel dengan parser backend Pantau.

2. **Prasyarat Host macOS**:
   - **Akses Remote SSH**: Pengguna mengaktifkan Remote Login melalui menu *System Settings > General > Sharing > Remote Login*.
   - **Autentikasi Kunci SSH**: Menggunakan pasangan kunci SSH untuk akun administrator atau akun pengguna standar macOS.
   - **Batas Kebutuhan Sudo**: Fitur inspeksi metrik, telemetri proses, dan pemindaian keamanan beroperasi murni secara **read-only tanpa membutuhkan hak akses sudo**. Hak `sudo` tanpa kata sandi (*passwordless sudo*) **hanya diperlukan jika administrator mengaktifkan fitur Elevated File Access** (`sudo -n -u <user> /usr/libexec/sftp-server`).
   - **Penyelarasan PATH Lingkungan (`DarwinPathPrelude`)**:
     - Pantau menyediakan konstanta prelude PATH untuk perintah remote yang membutuhkan perkakas pihak ketiga:
       ```sh
       case "$(uname -s)" in Darwin) PATH="$PATH:/usr/local/bin:/opt/homebrew/bin:/opt/homebrew/sbin:/Applications/Docker.app/Contents/Resources/bin"; export PATH;; esac;
       ```
     - Prelude ini memungkinkan penemuan otomatis biner Homebrew dan Docker Desktop CLI jika terpasang pada host macOS.

3. **Definisi dan Formula Pengumpulan Metrik Sistem macOS**:
   - **Memori Fisik (RAM)**:
     - Kapasitas total diperoleh dari `sysctl -n hw.memsize`.
     - Ukuran halaman (*page size*) diekstrak dinamis dari baris pertama `vm_stat` (16.384 byte pada Apple silicon).
     - Memori terpakai (*RAM Used*) dihitung sesuai rumusan resmi Activity Monitor:
       $$\text{RAM Used} = (\text{Anonymous pages} - \text{Pages purgeable} + \text{Pages wired down} + \text{Pages occupied by compressor}) \times \text{pagesize}$$
     - Nilai dipancarkan dalam format baris `Mem:` yang kompatibel dengan output `free -b`.
   - **Memori Virtual (Swap)**:
     - Diekstrak langsung dari `sysctl vm.swapusage` yang menyajikan nilai `total`, `used`, dan `free` (toleran terhadap satuan M, G, atau K).
   - **Penyimpanan Disk**:
     - Pantau mengabaikan ukuran root sealed volume `/` dan secara eksplisit mengevaluasi ruang simpan kontainer data bersama APFS: `df -lPk /System/Volumes/Data` (dengan fallback `/`).
     - Kapasitas terpakai dilaporkan secara akurat sebagai: $\text{Used} = \text{Total} - \text{Available}$.
   - **Throughput Jaringan (Egress/Ingress)**:
     - Menggunakan `netstat -ibn` dengan memfilter baris yang memiliki kolom Network berawalan `<Link#`.
     - Hanya menjumlahkan antarmuka fisik berawalan `en[0-9]+` untuk mencegah duplikasi statistik transfer dari antarmuka virtual atau jembatan internal.
   - **Statistik Disk I/O**:
     - Mengekstrak metrik transfer blok kumulatif dari IOKit registry: `ioreg -rd1 -c IOBlockStorageDriver`.
     - Mengambil nilai `Bytes (Read)` dan `Bytes (Write)`, lalu mengakumulasikannya ke seluruh driver penyimpanan fisik untuk menghasilkan throughput kecepatan baca/tulis yang konsisten.
   - **Spesifikasi Prosesor & Model Hardware**:
     - Jumlah core: `sysctl -n hw.ncpu` (mencakup seluruh performance core dan efficiency core).
     - Model perangkat & Chip: `system_profiler SPHardwareDataType` mengekstrak `Model Name`, `Model Identifier`, dan `Chip` (misal: `Apple Mac Studio (Mac15,14) - Apple M3 Ultra`), dengan fallback ke `sysctl -n hw.model` / `machdep.cpu.brand_string`.
     - Epoch instalasi OS: diekstrak dari birthtime berkas penanda setup via `stat -f %B /var/db/.AppleSetupDone`.
     - Parsing Uptime: parser mendukung variasi teks uptime Darwin (`up 13 days, 20:09`, `up 3 hrs`, `up 45 mins`, `up 1 day, 2:03`).
     - Pemetaan nama rilis macOS: versi 11 Big Sur, 12 Monterey, 13 Ventura, 14 Sonoma, 15 Sequoia, dan 26 Tahoe.

4. **Keamanan Privasi Proses Telemetri via `ucomm`**:
   - Pantau mengekstrak proses teratas menggunakan `ps -Aro pid,user,%cpu,%mem,rss,ucomm` (Top CPU) dan `ps -Amo ...` (Top Memory).
   - Kolom `ucomm` secara ketat hanya menyajikan nama biner proses (*process short name*). Penggunaan format `comm` atau `args` **dilarang keras** pada Darwin untuk mencegah terekamnya token sesi, kunci API, atau kredensial yang diteruskan melalui argumen baris perintah.

5. **Perbedaan Fitur dan Batasan Operasional pada macOS**:
   - **Ketiadaan Swap per Proses**: macOS tidak menyediakan representasi VmSwap per proses di sistem berkas; kolom alokasi swap pada tabel proses macOS dilaporkan kosong secara aman.
   - **Ketiadaan Penghitung Login Gagal**: macOS tidak memiliki `/var/log/btmp` standar; telemetri insiden login gagal tidak dinilai.
   - **Ketiadaan Deteksi Error I/O Kernel Tanpa Root**: Perintah `dmesg` di macOS membutuhkan hak akses root; evaluasi log error disk kernel ditiadakan pada sesi non-root.
   - **Visibilitas Daemon Docker Desktop**: Daemon Docker pada macOS berjalan di dalam VM lightweight xhyve/Virtualization framework. Pantau hanya menampilkan container Docker jika akun SSH memiliki izin menjalankan CLI `docker` (ditemukan via extended PATH).
   - **Inventaris Paket (Tab Packages)**: Tab inventaris paket dikosongkan secara terencana pada macOS. Fitur inventaris paket Pantau dikhususkan untuk manajer paket distribusi Linux (`dpkg`, `rpm`, `apk`, `pacman`). Utilitas pihak ketiga seperti Homebrew sengaja tidak dimasukkan ke dalam basis data paket sistem inti.

6. **Diagnostik Jaringan Portabel**:
   - **Pengganti Timeout**: macOS tidak memiliki utilitas GNU `timeout` bawaan. Diagnostik jaringan mengandalkan timeout portabel berbasis parameter native atau subshell watchdog.
   - **Perilaku Ping**: Pengujian latensi menggunakan opsi `-t <detik>` (bukan `-W` yang bernilai milidetik pada BSD ping). Parser ringkasan latensi disesuaikan untuk mendukung baris `round-trip min/avg/max/stddev = a/b/c/d ms` dan format `N packets transmitted, N packets received, X% packet loss`.
   - **Traceroute Native**: macOS menyertakan `/usr/sbin/traceroute` bawaan (tanpa `tracepath`). Pantau memanfaatkan biner traceroute native tersebut dan **tidak pernah mencoba menginstal paket baru** di macOS.

7. **Aturan Desired State pada macOS**:
   - **Aturan Disk**: Mendukung pengecekan kapasitas pada volume `/System/Volumes/Data` maupun `/`.
   - **Aturan Service**: Memverifikasi keaktifan layanan melalui `launchctl list` atau pencocokan nama proses via `pgrep`.
   - **Aturan Port Listening**: Memvalidasi port terbuka melalui `lsof -iTCP -sTCP:LISTEN -P -n`.
   - **Aturan Kesegaran Cadangan (Backup)**: Memeriksa timestamp perubahan berkas menggunakan perintah BSD `stat -f %m <path>` atau `stat -f %B`.

8. **Pemindaian Keamanan (Security Tab) dan Hardening pada macOS**:
   - **Pemeriksaan Pembaruan Sistem via `softwareupdate`**:
     - Menjalankan `/usr/sbin/softwareupdate -l` secara pasif tanpa root.
     - Menggunakan heuristik cerdas: pembaruan yang memuat kata kunci `macOS` atau `Safari` dikelompokkan sebagai pembaruan patch keamanan (*Security Updates*), sementara pembaruan lainnya dihitung dalam total pembaruan yang tersedia.
     - **Prinsip Kejujuran (Honest Coverage)**: Apabila server Apple tidak dapat dihubungi atau perintah mengalami kegagalan koneksi/timeout, hasil pemindaian secara transparan dilaporkan berstatus `partial` (bukan 0 temuan palsu).
   - **Empat Pemeriksaan Pengerasan Sistem (Hardening Checks) macOS**:
     1. `sshd_permit_root_login`: Membaca `/etc/ssh/sshd_config` dan direktori `/etc/ssh/sshd_config.d/`.
     2. `sshd_password_auth`: Memeriksa konfigurasi autentikasi password OpenSSH.
     3. `auto_security_updates`: Mengevaluasi preferensi pembaruan otomatis macOS melalui pembacaan kunci `defaults read /Library/Preferences/com.apple.SoftwareUpdate` (`AutomaticDownload`, `AutomaticallyInstallMacOSUpdates`, `CriticalUpdateInstall`, dan `ConfigDataInstall`).
     4. `firewall_active`: Memeriksa keaktifan Application Firewall bawaan melalui utilitas `/usr/libexec/ApplicationFirewall/socketfilterfw --getglobalstate` atau properti `com.apple.alf globalstate`.

9. **Kebijakan Siklus Hidup OS (Lifecycle EOL Policy)**:
   - Evaluasi siklus hidup macOS mengikuti kebijakan dukungan Apple standar: versi rilis terkini ditambah dua versi major sebelumnya (*current + 2 major versions*).
   - Sebagai contoh pada rilis macOS 26 Tahoe, versi 26 (Tahoe), 15 (Sequoia), dan 14 (Sonoma) berstatus aktif didukung, sedangkan versi 13 (Ventura) dan yang lebih lama diklasifikasikan sebagai End-of-Life (EOL).

10. **Batasan Cakupan Hardware**:
    - Seluruh kapabilitas diverifikasi secara nyata pada perangkat keras Apple silicon (macOS 26 Tahoe pada Mac mini M4 Pro dan Mac Studio M3 Ultra).
    - Host berbasis arsitektur Intel atau versi macOS lawas didukung berdasarkan ketersediaan perkakas POSIX standar tanpa jaminan fitur spesifik di luar yang telah divalidasi.

## Konsekuensi

- **Ekspansi Infrastruktur Kelas Satu**: Pantau kini dapat memantau armada mini-server dan workstation Mac (Mac mini, Mac Studio, Mac Pro) dengan fidelitas metrik setara server Linux.
- **Integritas Metrik Apple**: Memori, disk APFS, dan antarmuka fisik diukur secara presisi sesuai arsitektur macOS internal tanpa angka artifisial.
- **Perlindungan Privasi Terjamin**: Penggunaan `ucomm` melindungi token dan argumen rahasia proses agar tidak bocor ke log atau tampilan dasbor.
- **Isolasi Mutlak terhadap Alur Linux**: Kode koleksi Linux tetap berjalan byte-for-byte identik dan tidak terpengaruh oleh penambahan logika Darwin.
