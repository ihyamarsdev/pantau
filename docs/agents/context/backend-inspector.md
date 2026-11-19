# Backend & Inspector

Domain reference for the inspection engine, desired-state evaluation, metrics, alerts and host lifecycle. Read this when working in `internal/inspector/` or inspection-related store code.

**Desired State**:
Kumpulan aturan dan ekspektasi yang didefinisikan untuk sebuah Host (misal: service harus aktif, disk di bawah ambang batas, container tertentu harus running).
_Avoid_: Baseline, blueprint, config template

**Desired Rule**:
Satu butir aturan spesifikasi kondisi yang diharapkan pada Host, terdiri atas jenis aturan (*Kind*: `container`, `service`, `port`, `disk`, `cron`, `backup`, `process`, `security_updates`), pengenal target (*Target*), nilai ambang atau status yang diharapkan (*Expected*), serta status keaktifan (*Enabled*).
_Avoid_: Config check, monitor item, health probe

**Actual State**:
Kondisi riil sebuah Host yang didapatkan dari hasil pembacaan inspeksi berkala.
_Avoid_: Current status, telemetry snapshot

**Inspection**:
Proses pengambilan Actual State dari Host secara periodik melalui koneksi SSH tanpa menginstal agen di target.
_Avoid_: Polling, health check run, telemetry scrape

**Drift**:
Ketidaksesuaian atau deviasi antara Actual State dan Desired State pada Host.
_Avoid_: Mismatch, divergence, error state

**Host Status**:
Vonis satu kata atas hasil Inspection terakhir sebuah Host — `healthy` (semua Desired Rule sesuai), `degraded` (SSH terjangkau tetapi ada Drift, kegagalan aturan, atau budget inspeksi habis), `down` (Inspection tidak bisa membuka SSH ke Host, apa pun sebabnya: port menolak, autentikasi gagal, host tak menjawab, atau jalur VPN-nya mati; alasannya tertulis di catatan inspeksi, lihat [ADR-0077](../../../docs/adr/0077-jalur-jaringan-per-host-driver-vpn-dan-dial-terikat-interface.md)), `unknown` (belum pernah diinspeksi). Warna dan animasi indikator mengikuti status ini: hijau tenang, kuning berdenyut, merah cepat, abu-abu diam.
_Avoid_: Online/offline, health badge, connectivity state

**Root Cause Excerpt**:
Potongan log diagnostik terakhir (misal: exit code, potongan pesan journalctl atau file log sistem `/var/log/*`) yang diambil otomatis saat Drift terdeteksi.
_Avoid_: Crash dump, debug output

**Lifecycle Assessment**:
Proses evaluasi multi-faktor transparan terhadap Host untuk mengukur kelayakan siklus hidup sistem melalui analisis komparatif status OS EOL, saturasi memori/CPU/disk, integritas fisik I/O kernel, dan batas masa pakai produktif hardware.
_Avoid_: Server evaluation, health audit

**Lifecycle Score**:
Nilai kelayakan sebuah Host (0-100) hasil akhir dari Lifecycle Assessment yang merefleksikan kesiapan perangkat keras dan sistem operasi untuk terus beroperasi atau memerlukan penggantian/upgrade.
_Avoid_: Server grade, health rating

**Productive Lifespan**:
Rentang masa pakai produktif perangkat keras server (standar industri: 3–5 tahun) sebelum memasuki kurva penurunan MTBF (Mean Time Between Failures) dan risiko kegagalan fisik tinggi.
_Avoid_: Hardware durability, server lifetime

**Hardware Refresh**:
Rekomendasi penggantian atau peremajaan server yang telah melampaui masa pakai produktif atau terdepresiasi penuh untuk mencegah kegagalan perangkat keras tak terduga.
_Avoid_: Hardware upgrade, server replacement

**Hardware Commission Date**:
Tanggal resmi dimulainya masa operasional atau penempatan server oleh administrator yang diinput secara manual sebagai acuan utama evaluasi masa pakai produktif hardware, mengesampingkan pembacaan otomatis tanggal BIOS atau deployment OS pada kasus unit stok lama (New Old Stock).
_Avoid_: Purchase date, install date, server birthday, warranty start

**Backup Freshness**:
Status kevalidan backup berdasarkan keberadaan file di path tujuan, timestamp perubahan terbaru (recency), dan ukuran file yang wajar (> 0 byte).
_Avoid_: Backup validation, dump check

**Stale Inspection**:
Kondisi di mana Actual State sebuah Host belum berhasil diperbarui melampaui ambang batas toleransi (2× interval inspeksi normal), menandakan risiko koneksi SSH terputus, jaringan bermasalah, atau mesin target tidak responsif. Penanda peringatan ini otomatis disupresi ketika Background Inspection dinonaktifkan secara sengaja oleh administrator.
_Avoid_: Outdated metrics, expired telemetry, laggy check

**Background Inspection**:
Mekanisme eksekusi Inspeksi berkala di latar belakang untuk seluruh Host secara otomatis sesuai interval waktu yang dikonfigurasi, yang dapat dinonaktifkan (diatur ke 0 detik) untuk beralih sepenuhnya ke mode inspeksi manual (*On-Demand Inspection*).
_Avoid_: Background polling, auto scrape worker, periodic cron

**Inspect All**:
Mekanisme eksekusi Inspeksi serentak secara asinkron ke seluruh Host yang terdaftar dalam satu tindakan operasional terpadu.
_Avoid_: Bulk ping, refresh all, mass scan

**Inspection Concurrency Guard**:
Mekanisme penguncian in-flight pada level Host untuk mencegah penumpukan inspeksi paralel (*inspection stampede*) saat inspeksi latar belakang dan inspeksi manual dipicu bersamaan atau ketika server lambat merespons.
_Avoid_: Inspection lock, polling mutex, stampede blocker

**Inspection Run**:
Catatan riwayat satu kali eksekusi Inspeksi pada Host, mendokumentasikan timestamp, durasi eksekusi (ms), status hasil (sukses/gagal/drift), dan rincian diagnostik saat terjadi deviasi atau kegagalan.
_Avoid_: Task log, execution audit, polling history, health log

**Inspection Cooldown**:
Jeda waktu minimum (15 detik) yang diwajibkan antar eksekusi Inspeksi manual pada Host yang sama guna mencegah pembanjiran koneksi SSH (*connection flooding*) dan lonjakan beban CPU target.
_Avoid_: Rate limit window, inspect debounce, retry wait

**Resource Metrics**:
Rangkuman metrik kapasitas komputasi riil sebuah Host yang mencakup kuantitas core CPU, beban antrian proses (*load average*), kapasitas memori fisik (RAM), memori virtual (Swap), dan utilisasi penyimpanan root (Disk).
_Avoid_: Machine specs, server performance counters, hardware telemetry

**Process Telemetry**:
Rangkuman data proses-proses aktif pemakan kapasitas komputasi teratas (Top CPU, Top RAM/RSS, dan Top Swap) yang diekstrak secara berkala pada setiap siklus inspeksi untuk mendeteksi lonjakan beban atau kebocoran memori secara instan.
_Avoid_: Process list, ps dump, task manager, top monitor

**Disk I/O Rate**:
Kecepatan transfer data baca dan tulis pada disk fisik/virtual utama Host (dihitung dalam bytes per detik) yang diekstrak secara berkala dari `/proc/diskstats`.
_Avoid_: Disk speed, disk activity, raw io count

**Disk I/O Load Status**:
Klasifikasi kualitatif terhadap tingkat beban laju throughput I/O disk (Optimal < 10 MB/s, Aktif 10–80 MB/s, Beban I/O Tinggi ≥ 80 MB/s) untuk mendeteksi bottleneck penyimpanan secara cepat.
_Avoid_: Disk health, io score, disk pressure

**Resource Hog**:
Proses aktif pada Host yang mengonsumsi kapasitas komputasi (CPU), memori fisik (RAM/RSS), atau memori virtual (Swap) dalam porsi signifikan atau melampaui batas ambang evaluasi.
_Avoid_: Heavy process, ram eater, bottleneck app, resource killer

**Dormant Package**:
Paket perangkat lunak atau unit layanan pada Host yang terpasang namun teridentifikasi tidak memiliki aktivitas eksekusi proses atau layanan aktif dalam kurun waktu observasi inspeksi.
_Avoid_: Unused app, dead software, zombie program, orphan app

**Alert Debounce**:
Pengaturan (`alert_debounce_runs`, 1–10) yang menahan Incident, Alert, dan notifikasi sebuah Drift sampai terlihat pada N Inspection berturut-turut untuk rule yang sama; kembali sehat selalu menutup langsung.
_Avoid_: Alert delay, grace period, flap filter

**Alert Banner**:
Bilah ringkasan peringatan deviasi sistem pada dashboard Pantau yang menyajikan kuantitas alert aktif dan host terdampak secara non-blocking dengan panel rincian mengambang (*floating overlay*) dan pembersihan massal (*dismiss all*).
_Avoid_: Warning list, alert strip, popup banner

**Alert Auto-Resolution**:
Mekanisme penutupan otomatis terhadap status peringatan aktif pada Host ketika hasil pembacaan inspeksi berikutnya memverifikasi bahwa kondisi sistem telah pulih sepenuhnya ke Desired State tanpa deviasi (*healthy*).
_Avoid_: Auto clear, alert wipe, drift expiry

**Dual-Bucket Pruning**:
Kebijakan penyimpanan riwayat inspeksi pada basis data internal yang mengalokasikan kuota retensi bergulir terpisah antara eksekusi normal dan eksekusi bermasalah (*drift/error/down*) guna menjamin ketersediaan jejak audit deviasi historis tanpa memicu pembengkakan ukuran file.
_Avoid_: Multi-retention, split purge, error archiving

**Host Comparison**:
Fitur evaluasi komparatif multi-host (2–4 Host) secara berdampingan (side-by-side) pada antarmuka Workspace Modal untuk membandingkan kapasitas komputasi, beban I/O, latensi jaringan, status deviasi aturan, dan skor siklus hidup sistem.
_Avoid_: Server comparison, machine diff, host benchmark tool

**Comparison Category**:
Dimensi evaluasi terstandarisasi dalam Host Comparison (Kapasitas & Beban Komputasi, Efisiensi Penyimpanan & I/O, Keandalan Jaringan, serta Higienitas Sistem & Lifecycle) yang menjadi acuan penilaian head-to-head.
_Avoid_: Metric group, scoring bucket, comparison tab

**Comparison Winner**:
Host yang memperoleh skor atau nilai keunggulan tertinggi pada suatu Comparison Category atau secara akumulatif dalam satu sesi Host Comparison.
_Avoid_: Best server, benchmark winner, top machine

**Fleet KPI Strip**:
Deretan ubin ringkasan armada di atas daftar Host pada dashboard (Total, Healthy, Drift, Offline, Stale, rata-rata RAM beserta puncak disk). Setiap ubin dapat diklik untuk memfilter daftar Host berdasarkan status; hitungannya memakai definisi tunggal yang sama dengan telemetri agregat di footer (Drift = status `degraded`, Offline = `down`, Stale mengikuti ambang Stale Inspection dan disupresi saat Background Inspection dinonaktifkan).
_Avoid_: Summary bar, stats header, status counter

**Package Inventory**:
Daftar inventaris paket perangkat lunak terpasang pada Host hasil inspeksi manajer paket sistem (APT, DNF/RPM, Pacman, Snap) yang mendokumentasikan nama paket, versi, estimasi ukuran disk, tanggal instalasi, dan klasifikasi pemasangan.
_Avoid_: App list, installed software, system programs, app manager

**Manual Install**:
Klasifikasi paket perangkat lunak pada Package Inventory yang dipasang secara eksplisit oleh administrator melalui perintah manajer paket, bukan dependensi otomatis atau bootstrap awal distro.
_Avoid_: User app, custom package, non-system package

**Base System**:
Klasifikasi paket perangkat lunak pada Package Inventory yang merupakan bagian dari instalasi awal sistem operasi (distro bootstrap) atau pustaka/dependensi inti yang dibutuhkan sistem.
_Avoid_: Default app, system bundle, OS packages

**Host Note**:
Catatan bebas berbasis teks yang disematkan pada Host oleh administrator untuk mencatat konteks operasional atau panduan pemeliharaan spesifik mesin tersebut.
_Avoid_: Host description, server memo, tag

**Global Note**:
Catatan operasional bersama tingkat sistem pada dashboard Pantau untuk pengumuman tim, prosedur operasional darurat, atau catatan serah-terima tugas antar-administrator.
_Avoid_: System message, announcement banner, global memo

**Manual Host Order**:
Urutan urut prioritas tampilan kartu Host pada dashboard Pantau yang diatur secara manual oleh administrator untuk mengelompokkan server utama pada posisi teratas.
_Avoid_: Custom sort, priority list, pinned hosts

**View Mode**:
Format tata letak penyajian daftar Host pada dashboard Pantau, dalam bentuk kartu visual (Grid View) atau baris tabel kompak (List View).
_Avoid_: Layout style, display type, presentation format

**Package Update Check**:
Pemeriksaan read-only on-demand atas versi paket terpasang pada sebuah Host yang menghasilkan daftar paket yang punya versi lebih baru beserta versi terpasang dan versi yang tersedia; tidak memasang, tidak mengubah, dan tidak me-restart apa pun.
_Avoid_: Image Update Check, Update Checker, Package Upgrade

**Package Upgrade**:
Aksi menerapkan pembaruan paket (`all`, atau `security` pada dnf/yum) di sebuah Host setelah pratinjau, mengikuti Docker Privilege Fallback; tanpa reboot otomatis dan tanpa rollback.
_Avoid_: Patch run, system update, auto-update

**Bulk Command**:
Satu perintah shell non-interaktif yang dijalankan serentak ke beberapa Host terpilih (maks 20) dengan hasil per Host dialirkan ke antarmuka; tanpa riwayat dan tanpa TTY.
_Avoid_: Mass exec, fleet command, multi-host terminal

**Update Checker**:
Mekanisme deteksi periodik asinkron terhadap ketersediaan rilis versi resmi terbaru Pantau di repositori upstream tanpa memblokir operasional utama sistem.
_Avoid_: Version polling, release scraper, update sniffer

**Self-Update**:
Proses pengunduhan paket rilis, validasi integritas checksum, dan penggantian mandiri (in-place binary replacement) file executable Pantau yang dipicu atas konfirmasi eksplisit administrator.
_Avoid_: Auto patch, silent updater, live patch, background updater

**Instance Guard**:
Mekanisme proteksi proses tunggal berbasis file lock kernel OS yang terikat pada file database untuk mendeteksi instance Pantau yang sedang berjalan, mencegah eksekusi ganda pada database yang sama, memberikan toleransi serah-terima lock (*graceful lock handover*) saat restart/self-update, dan memberikan notifikasi lokasi port serta PID aktif kepada pengguna.
_Avoid_: App lock, multi-instance blocker, process mutex
