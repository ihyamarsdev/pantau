# Pantau

Sistem pemantauan dan manajemen infrastruktur server berbasis agentless SSH dengan verifikasi kondisi seharusnya (desired state), diagnosis deviasi otomatis, dan manajemen file/terminal.

## Language

**Host**:
Mesin server fisik atau virtual (VPS) Linux yang dikelola dan dipantau melalui koneksi SSH.
_Avoid_: Node, instance, machine, windows target

**macOS Host**:
Mesin server fisik atau virtual berbasis sistem operasi macOS (Darwin) yang dikelola dan dipantau secara agentless melalui koneksi SSH standar.
_Avoid_: Mac node, Darwin instance, OSX target, Apple workstation

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

**AI Agent**:
Asisten operasi opsional (Hermes Agent, proses terpisah) yang mengobrol lewat chat melayang di pojok kanan bawah dan memanggil *Agent Tool* Pantau melalui MCP (`/mcp`) untuk menyelidiki dan memperbaiki Host. Identitas dan aturannya ditentukan oleh **SOUL**.
_Avoid_: Bot, copilot, auto-fixer

**Agent Tool**:
Satu fungsi Pantau yang dapat dipanggil AI Agent (baca: `list_hosts`, `systemd_detail`, `docker_logs`, dst.; ubah: `systemd_action`, `docker_action`, `inspect_host`, `security_scan`, `exec_command`). Setiap tool ubah menunggu **Agent Approval** dari operator.
_Avoid_: Plugin, skill, command

**Agent Approval**:
Keputusan operator (setujui/tolak) atas satu aksi pengubah yang diminta AI Agent, ditampilkan sebagai kartu persetujuan. Mode `ask` meminta untuk semua perubahan; mode `auto` melewati aksi service/container/scan/inspeksi, tetapi `exec_command` selalu meminta.
_Avoid_: Confirmation dialog, permission prompt

**Agent Memory**:
Kumpulan catatan permanen (maks. 200, masing-masing maks. 500 karakter; lingkup seluruh fleet atau satu Host) yang disimpan AI Agent lewat `memory_save` dan operator lewat Settings, lalu disisipkan ke pesan sistem setiap chat agar agent makin memahami infrastruktur dari waktu ke waktu. Catatan diperlakukan sebagai data, bukan instruksi.
_Avoid_: Chat history, knowledge base, agent training

**Disk I/O Rate**:
Kecepatan transfer data baca dan tulis pada disk fisik/virtual utama Host (dihitung dalam bytes per detik) yang diekstrak secara berkala dari `/proc/diskstats`.
_Avoid_: Disk speed, disk activity, raw io count

**Disk I/O Load Status**:
Klasifikasi kualitatif terhadap tingkat beban laju throughput I/O disk (Optimal < 10 MB/s, Aktif 10–80 MB/s, Beban I/O Tinggi ≥ 80 MB/s) untuk mendeteksi bottleneck penyimpanan secara cepat.
_Avoid_: Disk health, io score, disk pressure

**Notification Channel**:
Saluran pengiriman peringatan saat terdeteksi Drift (seperti Telegram bot, webhook, atau in-app dashboard).
_Avoid_: Alert sink, message publisher

**Key Provisioning**:
Proses penyisipan otomatis SSH Public Key Pantau (menggunakan kunci publik dari Library Key yang dipilih, atau Global SSH Key default jika tidak ditentukan) ke dalam `~/.ssh/authorized_keys` di Host target menggunakan autentikasi password sementara (one-time).
_Avoid_: Password sync, credential push

**SSH Key Library**:
Katalog penyimpanan kunci privat SSH terpusat di Pantau yang memungkinkan pendaftaran dan pengelolaan kunci sekali untuk digunakan berulang kali oleh banyak Host melalui pilihan dropdown.
_Avoid_: Key manager, credential vault, ssh keychain

**Library Key**:
Satu entri kunci privat SSH terdaftar di dalam SSH Key Library yang memiliki pengenal unik (*ID*), nama alias (*Name*), sidik jari SHA-256 (*Fingerprint*), serta kunci publik terderivasi (*Public Key*).
_Avoid_: Shared key, preset key, stored credential
**Cross-Host Transfer**:
Proses pemindahan file atau folder langsung dari Host sumber ke Host tujuan melalui stream pipa relay Pantau tanpa penyimpanan file sementara pada disk master.
_Avoid_: File sync, remote copy, direct scp

**Transfer Job**:
Tugas pemindahan asinkron di latar belakang yang melacak status, akumulasi byte terkirim, kecepatan (MB/s), dan estimasi waktu tersisa (ETA).
_Avoid_: Copy task, sync process

**Folder Streaming Download**:
Proses pengaliran arsip direktori secara on-the-fly langsung dari Host ke response browser pengguna (menggunakan zip jika terpasang di remote atau tar.gz) tanpa menyimpan file arsip di disk master Pantau.
_Avoid_: Folder export, zip dump

**Network Egress**:
Status keterhubungan keluar Host ke internet publik dan pengukuran latensi roundtrip (ms) ke resolver global.
_Avoid_: Internet status, ping check

**Port Exposure**:
Klasifikasi tingkat keterbukaan listening port pada Host (apakah terikat pada interface publik `0.0.0.0`/`*` atau terisolasi lokal `127.0.0.1`) beserta penilaian risiko keamanan service sensitif.
_Avoid_: Open port list, firewall rule

**Socket Profile**:
Ringkasan koneksi TCP/UDP aktif pada Host yang memetakan alamat IP klien eksternal teratas, port tujuan, dan proses yang melayaninya.
_Avoid_: Netstat dump, raw connections

**System Snapshot**:
Arsip mandiri terenkripsi yang memuat seluruh basis data Pantau (konfigurasi Host, kredensial SSH, aturan Desired State, dan pengaturan aplikasi).
_Avoid_: Database dump, backup archive, config export

**Snapshot Passphrase**:
Kunci rahasia berbasis frasa sandi dari pengguna yang digunakan untuk derivasi kunci enkripsi (AES-256-GCM) sebelum System Snapshot disimpan atau dipulihkan.
_Avoid_: Master password, backup pin, encryption key

**Remote Storage Provider**:
Konektor penyimpanan eksternal (GitHub Private Repository) untuk sinkronisasi System Snapshot secara berkala melalui protokol HTTPS API.
_Avoid_: Cloud storage, backup destination, git sync


**Workspace Modal**:
Kontainer modal layar lebar berdimensi tetap (75vw × 80vh di desktop) untuk tugas interaktif observabilitas dan data (Host Detail, Editor, Transfer Queue) tanpa perubahan ukuran layout saat navigasi antar-tab.
_Avoid_: Popup window, dynamic modal, fluid dialog

**Action Dialog**:
Kontainer modal terpusat berdimensi ringkas (lebar ~520px - 740px) untuk formulir input sekuensial, dialog pengaturan (Settings), atau konfirmasi aksi tunggal (Add Host, Direct Transfer, Restore Wizard).
_Avoid_: Small modal, mini popup, submodal

**Documentation Modal**:
Kontainer Workspace Modal interaktif berdimensi tetap (75vw × 80vh) untuk membaca panduan operasional Pantau (README, User Guide, dan Changelog) secara mandiri (airgapped) tanpa ketergantungan jaringan eksternal.
_Avoid_: Help popup, docs reader, guide dialog, manual tab, external wiki

**Changelog**:
Catatan historis kronologis terstruktur (berbasis standar Keep a Changelog) yang mendokumentasikan penambahan fitur, perubahan perilaku, dan perbaikan bug di setiap rilis Pantau, disematkan langsung ke dalam biner aplikasi untuk akses in-app pada lingkungan terisolasi (airgapped).
_Avoid_: Release notes, commit log, version summary, update history

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

**Protected Path**:
Lokasi direktori sistem operasi atau file inti pada Host Linux yang dibatasi secara permanen dari operasi manipulasi (tulis, ubah, unggah, dan hapus) melalui antarmuka manajemen file.
_Avoid_: Blocked folder, blacklisted path, system lock

**Remote Web Port Tunnel**:
Reverse proxy HTTP dan WebSocket on-demand yang memanfaatkan channel OpenSSH RFC 4254 `direct-tcpip` untuk menjembatani akses browser klien ke service web internal pada Host target (`127.0.0.1:<port>`) secara *agentless*.
_Avoid_: Port forwarding, ssh tunnel proxy, local forwarder

**Terminal Link Provider**:
Komponen pendeteksi URL interaktif pada xterm Pantau yang memvalidasi link URL (`Ctrl+Click` / `Cmd+Click`) dan membedakan rute URL publik ke tab browser baru atau URL loopback remote ke Remote Web Port Tunnel.
_Avoid_: URL detector, link clicker, terminal hyperlinker

**Remote Browser Bridge**:
Mekanisme otomatisasi protokol escape sequence (OSC 1337 / OSC 777) dan penyediaan helper `$BROWSER` pada Host target yang memungkinkan perintah CLI remote (`gh auth login`, `xdg-open`, Python `webbrowser`, AWS SSO, dsb.) membuka browser desktop asli klien secara otomatis tanpa memicu text-browser headless (`w3m`/`lynx`).
_Avoid_: Web launcher, remote browser opener, headless bridge

**Terminal Preset**:
Konfigurasi pintasan perintah terminal interaktif (nama label, perintah shell, target Host opsional, dan preferensi persistensi sesi) yang tersimpan di basis data Pantau untuk eksekusi instan melalui antarmuka web.
_Avoid_: Terminal shortcut, terminal macro, saved command, quick command

**Toast Notification**:
Pemberitahuan mengambang non-blocking di pojok antarmuka Pantau dengan ikon status (sukses, peringatan, error) dan penghilangan otomatis (*auto-dismiss*) untuk mengeliminasi pemblokiran UI thread dari modal dialog bawaan peramban.
_Avoid_: Alert popup, banner alert, notification snackbar

**Confirmation Dialog**:
Dialog aksi ringkas berbasis Promise yang menyajikan konfirmasi eksplisit sebelum eksekusi aksi destruktif atau peringatan batas sistem, menggantikan dialog konfirmasi bawaan peramban.
_Avoid_: Confirm popup, prompt dialog, native confirmation

**Host Group**:
Pengelompokan logis untuk satu atau lebih Host pada dashboard Pantau berdasarkan fungsi server, lingkungan (environment), atau lokasi (misal: "Server Utama", "Testing", "Database Cluster").
_Avoid_: Server cluster, host tag, machine folder

**Terminal Maximize**:
Mode pembesaran antarmuka terminal interaktif hingga memenuhi seluruh area pandang peramban (full-viewport) untuk ruang operasional shell yang lebih luas tanpa memutus sesi SSH atau merusak layout modal.
_Avoid_: Native fullscreen, modal expand, popout window

**Port Auto-Scan**:
Mekanisme deteksi ketersediaan port jaringan lokal secara berurutan saat server web Pantau diinisialisasi dengan port default, untuk menghindari kegagalan proses akibat port yang telah terpakai.
_Avoid_: Port hopping, dynamic port binding

**Self-Contained Web Assets**:
Koleksi seluruh pustaka antarmuka web Pantau (stylesheet CSS, modul terminal xterm, dan editor teks CodeMirror) yang disematkan langsung ke dalam binary aplikasi melalui sistem file tersemat (embedded filesystem) dan disajikan secara lokal, menghilangkan seluruh ketergantungan jaringan eksternal ke CDN publik untuk menjamin operasional penuh pada lingkungan terisolasi (airgapped).
_Avoid_: CDN dependencies, online scripts, external assets

**Terminal Dock**:
Kontainer antarmuka terminal global multi-tab di level aplikasi Pantau yang dapat diminimalkan menjadi bilah dok mengambang (dock bar) atau dimaksimalkan ke seluruh viewport tanpa memutus sesi koneksi shell aktif.
_Avoid_: Terminal popup, floating window, modal shell

**Terminal Tab**:
Entitas sesi terminal independen di dalam Terminal Dock yang merepresentasikan satu koneksi PTY remote ke Host tertentu dengan dukungan penamaan judul dinamis (inline rename).
_Avoid_: Terminal window, shell pane, subterminal

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

**Terminal Header Launcher**:
Tombol aksi terminal global terpadu pada header utama Pantau yang memicu tampilan Terminal Dock, dilengkapi badge indikator jumlah sesi PTY aktif secara real-time.
_Avoid_: Floating pill, session pill, dock launcher, terminal shortcut

**Terminal Session Badge**:
Indikator numerik reaktif pada tombol terminal kartu Host dan Terminal Header Launcher yang merefleksikan jumlah sesi shell aktif untuk masing-masing Host atau seluruh sistem.
_Avoid_: Tab counter, active count pill, connection tag

**2FA Lockout**:
Penangguhan sementara proses verifikasi login (jeda pendinginan 30 detik setelah 3 kali kegagalan berturut-turut memasukkan token TOTP atau Recovery Code) untuk memitigasi upaya serangan tebak token otomatis (brute-force).
_Avoid_: Account ban, login block, brute-force penalty

**Stale Inspection**:
Kondisi di mana Actual State sebuah Host belum berhasil diperbarui melampaui ambang batas toleransi (2× interval inspeksi normal), menandakan risiko koneksi SSH terputus, jaringan bermasalah, atau mesin target tidak responsif. Penanda peringatan ini otomatis disupresi ketika Background Inspection dinonaktifkan secara sengaja oleh administrator.
_Avoid_: Outdated metrics, expired telemetry, laggy check

**Background Inspection**:
Mekanisme eksekusi Inspeksi berkala di latar belakang untuk seluruh Host secara otomatis sesuai interval waktu yang dikonfigurasi, yang dapat dinonaktifkan (diatur ke 0 detik) untuk beralih sepenuhnya ke mode inspeksi manual (*On-Demand Inspection*).
_Avoid_: Background polling, auto scrape worker, periodic cron

**Inspect All**:
Mekanisme eksekusi Inspeksi serentak secara asinkron ke seluruh Host yang terdaftar dalam satu tindakan operasional terpadu.
_Avoid_: Bulk ping, refresh all, mass scan

**Update Checker**:
Mekanisme deteksi periodik asinkron terhadap ketersediaan rilis versi resmi terbaru Pantau di repositori upstream tanpa memblokir operasional utama sistem.
_Avoid_: Version polling, release scraper, update sniffer

**Self-Update**:
Proses pengunduhan paket rilis, validasi integritas checksum, dan penggantian mandiri (in-place binary replacement) file executable Pantau yang dipicu atas konfirmasi eksplisit administrator.
_Avoid_: Auto patch, silent updater, live patch, background updater

**Resource Metrics**:
Rangkuman metrik kapasitas komputasi riil sebuah Host yang mencakup kuantitas core CPU, beban antrian proses (*load average*), kapasitas memori fisik (RAM), memori virtual (Swap), dan utilisasi penyimpanan root (Disk).
_Avoid_: Machine specs, server performance counters, hardware telemetry

**Inspection Concurrency Guard**:
Mekanisme penguncian in-flight pada level Host untuk mencegah penumpukan inspeksi paralel (*inspection stampede*) saat inspeksi latar belakang dan inspeksi manual dipicu bersamaan atau ketika server lambat merespons.
_Avoid_: Inspection lock, polling mutex, stampede blocker

**Inspection Run**:
Catatan riwayat satu kali eksekusi Inspeksi pada Host, mendokumentasikan timestamp, durasi eksekusi (ms), status hasil (sukses/gagal/drift), dan rincian diagnostik saat terjadi deviasi atau kegagalan.
_Avoid_: Task log, execution audit, polling history, health log

**Inspection Cooldown**:
Jeda waktu minimum (15 detik) yang diwajibkan antar eksekusi Inspeksi manual pada Host yang sama guna mencegah pembanjiran koneksi SSH (*connection flooding*) dan lonjakan beban CPU target.
_Avoid_: Rate limit window, inspect debounce, retry wait

**Instance Guard**:
Mekanisme proteksi proses tunggal berbasis file lock kernel OS yang terikat pada file database untuk mendeteksi instance Pantau yang sedang berjalan, mencegah eksekusi ganda pada database yang sama, memberikan toleransi serah-terima lock (*graceful lock handover*) saat restart/self-update, dan memberikan notifikasi lokasi port serta PID aktif kepada pengguna.
_Avoid_: App lock, multi-instance blocker, process mutex

**Directory Filter**:
Penyaringan reaktif instan pada entri berkas dan folder di direktori aktif SFTP File Manager berdasarkan kata kunci nama berkas atau filter tipe entri (semua, folder, berkas) tanpa pemuatan ulang jaringan.
_Avoid_: File search, search bar, deep finder

**File Sorting**:
Pengorganisasian urutan entri berkas dan folder pada antarmuka SFTP File Manager berdasarkan atribut kolom (nama, ukuran, tanggal modifikasi) dengan prioritas entri folder di atas berkas (*folders first*).
_Avoid_: File order, column ordering

**Alert Banner**:
Bilah ringkasan peringatan deviasi sistem pada dashboard Pantau yang menyajikan kuantitas alert aktif dan host terdampak secara non-blocking dengan panel rincian mengambang (*floating overlay*) dan pembersihan massal (*dismiss all*).
_Avoid_: Warning list, alert strip, popup banner

**Alert Auto-Resolution**:
Mekanisme penutupan otomatis terhadap status peringatan aktif pada Host ketika hasil pembacaan inspeksi berikutnya memverifikasi bahwa kondisi sistem telah pulih sepenuhnya ke Desired State tanpa deviasi (*healthy*).
_Avoid_: Auto clear, alert wipe, drift expiry

**Dual-Bucket Pruning**:
Kebijakan penyimpanan riwayat inspeksi pada basis data internal yang mengalokasikan kuota retensi bergulir terpisah antara eksekusi normal dan eksekusi bermasalah (*drift/error/down*) guna menjamin ketersediaan jejak audit deviasi historis tanpa memicu pembengkakan ukuran file.
_Avoid_: Multi-retention, split purge, error archiving

**Workspace Shelf**:
Bilah navigasi horizontal adaptif tepat di bawah header utama yang menampung sesi Workspace Modal dan Code Editor yang diminimalkan untuk pemulihan instan tanpa polusi visual mengambang.
_Avoid_: Taskbar, minimized bar, bottom dock, modal tray

**Workspace Session**:
Entitas status kerja in-memory yang mencatat identitas host, tab aktif, direktori SFTP, atau buffer editor saat sebuah modal diminimalkan.
_Avoid_: Background tab, suspended window, open process

**Smart Resume**:
Mekanisme pemulihan otomatis konteks kerja dan navigasi terakhir saat pengguna membuka kembali Host Detail dari kartu server pada dashboard.
_Avoid_: State reload, session restore, auto reopen

**Workspace Modal Maximize**:
Mode pembesaran kontainer Workspace Modal (Host Detail) hingga memenuhi seluruh area pandang peramban (*CSS full-viewport* 100vw × 100vh) untuk ruang kerja inspeksi dan manajemen berkas yang maksimal tanpa merusak penanganan tombol keyboard interaktif atau memicu izin fullscreen native peramban.
_Avoid_: Native fullscreen, modal expand, browser fullscreen, popout dialog

**Terminal From Path**:
Aksi peluncuran sesi Terminal Tab baru di dalam Terminal Dock yang secara otomatis bernavigasi ke path direktori target atau folder induk berkas dari antarmuka SFTP File Manager.
_Avoid_: Folder shell, path terminal, file terminal, quick shell

**Per-Host Tab State**:
Mekanisme persistensi dan isolasi state tab aktif pada Workspace Modal secara independen untuk masing-masing Host berbasis penyimpanan sesi peramban (*sessionStorage*), memastikan navigasi tab pada satu host tidak mengubah tab default host lainnya.
_Avoid_: Global active tab, modal tab memory, shared tab state

**Terminal Clipboard Bridge**:
Mekanisme penanganan pintasan papan ketik dan papan klip (clipboard) dwiarah pada Terminal Tab yang menyelaraskan konvensi PTY Unix (`SIGINT`, *readline*) dengan integrasi *clipboard* peramban, mencakup penyalinan kontekstual (*context-aware copy*), penempelan multi-baris aman (*multiline paste confirmation*) dengan opsi perataan perintah menjadi satu baris (*single-line flatten*), dan penyalinan otomatis berbasis seleksi kursor (*copy-on-select*).
_Avoid_: Terminal copy paste, shell clipboard, browser paste hack

**Terminal Tab Host Filter**:
Penyaringan reaktif instan pada daftar pilihan Host di menu dropdown pembuatan Terminal Tab baru berdasarkan kata kunci Host Name atau Host Address (IP/FQDN).
_Avoid_: Terminal host search, host picker filter, terminal autocomplete

**File Duplication**:
Proses penggandaan berkas atau direktori secara lokal pada Host yang sama melalui antarmuka SFTP File Manager dengan penamaan default berbasis timestamp, validasi kecukupan ruang penyimpanan (*Disk Space Safety Check*), dialog konfirmasi penamaan, dan penolakan tabrakan nama untuk mencegah kehilangan data.
_Avoid_: Remote clone, file copy local, dup item

**Disk Space Safety Check**:
Pemeriksaan kapasitas penyimpanan riil pada partisi target Host sebelum eksekusi duplikasi untuk memastikan ketersediaan ruang disk mencukupi ukuran berkas atau direktori ditambah batas aman (*safety buffer* 100 MB).
_Avoid_: Disk quota check, storage probe, free space validator

**Interactive Breadcrumb**:
Komponen navigasi jalur direktori pada SFTP File Manager yang memecah path aktif menjadi segmen-segmen hierarki yang dapat diklik langsung untuk berpindah ke direktori induk tanpa perlu menekan tombol navigasi atas berulang kali.
_Avoid_: Path clicker, folder tree strip, breadcrumb trail

**Editor Modal Maximize**:
Mode pembesaran kontainer Editor Modal hingga memenuhi seluruh area pandang peramban (*CSS full-viewport* 100vw × 100vh) dengan penyelarasan ulang ukuran editor teks (CodeMirror refresh) untuk kenyamanan penelaahan dan penyuntingan kode secara luas.
_Avoid_: Editor fullscreen, native editor expand, full window editor

**Live Event Stream**:
Langganan streaming Server-Sent Events (`/api/logs/stream`) yang mendistribusikan telemetri runtime terstruktur secara real-time dari Diagnostic Logger ke sesi web pengguna menggunakan standar Go `http.Flusher` dan browser native `EventSource`.
_Avoid_: Log socket, polling feed, stdout pipe, log websocket

**Activity Console**:
Drawer diagnostik collapsible di bagian bawah antarmuka pengguna Pantau yang menyajikan running table aktivitas runtime aplikasi secara interaktif, pausable, filterable, dan exportable.
_Avoid_: Log window, debug popup, terminal tab, output box

**Telemetry Ring Buffer**:
Penyimpanan buffer sirkular in-memory berkapasitas tetap (100 entri) yang menyimpan rekaman telemetri terbaru untuk memberikan konteks operasional instan saat sesi web terhubung.
_Avoid_: Memory dump, unconstrained queue, persistent log table

**Smart Auto-Scroll**:
Perilaku pelacakan viewport pada Activity Console yang secara otomatis menangguhkan auto-scroll saat pengguna menggulir ke atas untuk meninjau log historis dan melanjutkannya kembali saat digulir ke entri terbaru.
_Avoid_: Forced scroll, jump lock

**Redaction Filter**:
Filter batas deterministik yang mencegat pesan dan atribut log telemetri untuk menyensor password SSH, private key, token autentikasi, recovery code, dan passphrase menjadi `[REDACTED]` sebelum dipancarkan ke Live Event Stream.
_Avoid_: Secret hiding, raw dump, password censor

**TOFU (Trust On First Use)**:
Mekanisme verifikasi host key SSH otomatis pada koneksi pertama yang mencatat fingerprint host untuk memvalidasi koneksi berikutnya dan mendeteksi serangan Machine-in-the-Middle (MITM) tanpa memerlukan konfigurasi manual `known_hosts`.
_Avoid_: Insecure ignore host key, manual known hosts, strict pinning

**Shell Quote Escaping**:
Mekanisme pembungkusan argumen perintah jarak jauh menggunakan tanda kutip tunggal POSIX (`'...'`) universal yang menetralkan ekspansi variabel shell (`$`, backtick, operator piping) di seluruh versi Linux dari kernel lawas (CentOS 5) hingga modern.
_Avoid_: Double quote interpolation, shell sanitization, cmd escape

**Resource Hog**:
Proses aktif pada Host yang mengonsumsi kapasitas komputasi (CPU), memori fisik (RAM/RSS), atau memori virtual (Swap) dalam porsi signifikan atau melampaui batas ambang evaluasi.
_Avoid_: Heavy process, ram eater, bottleneck app, resource killer

**Package Inventory**:
Daftar inventaris paket perangkat lunak terpasang pada Host hasil inspeksi manajer paket sistem (APT, DNF/RPM, Pacman, Snap) yang mendokumentasikan nama paket, versi, estimasi ukuran disk, tanggal instalasi, dan klasifikasi pemasangan.
_Avoid_: App list, installed software, system programs, app manager

**Manual Install**:
Klasifikasi paket perangkat lunak pada Package Inventory yang dipasang secara eksplisit oleh administrator melalui perintah manajer paket, bukan dependensi otomatis atau bootstrap awal distro.
_Avoid_: User app, custom package, non-system package

**Base System**:
Klasifikasi paket perangkat lunak pada Package Inventory yang merupakan bagian dari instalasi awal sistem operasi (distro bootstrap) atau pustaka/dependensi inti yang dibutuhkan sistem.
_Avoid_: Default app, system bundle, OS packages

**Process Telemetry**:
Rangkuman data proses-proses aktif pemakan kapasitas komputasi teratas (Top CPU, Top RAM/RSS, dan Top Swap) yang diekstrak secara berkala pada setiap siklus inspeksi untuk mendeteksi lonjakan beban atau kebocoran memori secara instan.
_Avoid_: Process list, ps dump, task manager, top monitor

**Dormant Package**:
Paket perangkat lunak atau unit layanan pada Host yang terpasang namun teridentifikasi tidak memiliki aktivitas eksekusi proses atau layanan aktif dalam kurun waktu observasi inspeksi.
_Avoid_: Unused app, dead software, zombie program, orphan app

**Network Diagnostic**:
Modul perkakas interaktif pengujian jaringan on-demand berbasis SSH untuk memverifikasi keterjangkauan, perutean paket, port layanan, dan resolusi nama domain langsung dari Host tanpa instalasi agen remote.
_Avoid_: Net test, network tool, ping utility, network troubleshooter

**Diagnostic Probe**:
Satu sesi eksekusi uji jaringan spesifik (`ping`, `traceroute`, `port_probe`, atau `dns_resolve`) yang dijalankan secara asinkron dari Host sumber ke Probe Target.
_Avoid_: Network job, probe command, test runner

**Probe Target**:
Sasaran tujuan pengujian jaringan yang dapat berupa Host terkelola lain dalam inventaris Pantau (*Inter-Host*) atau alamat IP/FQDN eksternal publik (*External Target*).
_Avoid_: Destination, remote endpoint, probe address

**Hop Analysis**:
Hasil interpretasi heuristik terhadap rantai hop perutean traceroute yang secara otomatis mengklasifikasikan gateway privat, mendeteksi titik putus firewall, dan menyoroti lonjakan latensi tanpa interpretasi manual.
_Avoid_: Traceroute summary, route report, hop parser

**Host Comparison**:
Fitur evaluasi komparatif multi-host (2–4 Host) secara berdampingan (side-by-side) pada antarmuka Workspace Modal untuk membandingkan kapasitas komputasi, beban I/O, latensi jaringan, status deviasi aturan, dan skor siklus hidup sistem.
_Avoid_: Server comparison, machine diff, host benchmark tool

**Comparison Category**:
Dimensi evaluasi terstandarisasi dalam Host Comparison (Kapasitas & Beban Komputasi, Efisiensi Penyimpanan & I/O, Keandalan Jaringan, serta Higienitas Sistem & Lifecycle) yang menjadi acuan penilaian head-to-head.
_Avoid_: Metric group, scoring bucket, comparison tab

**Comparison Winner**:
Host yang memperoleh skor atau nilai keunggulan tertinggi pada suatu Comparison Category atau secara akumulatif dalam satu sesi Host Comparison.
_Avoid_: Best server, benchmark winner, top machine

**File Pin**:
Sematkan berkas atau direktori penting per host lengkap dengan catatan operasional (*notes*) untuk akses instan di pengelola berkas.
_Avoid_: Bookmarked file, favorite path, saved item

**Global Bookmarks**:
Modal penjelajah global di header utama untuk menelusuri, memfilter, mengedit, atau membuka sesi terminal ke seluruh File Pin di semua host terkelola.
_Avoid_: Saved files list, pinned manager, favorites modal

**Terminal Preset Live Search**:
Fitur pemfilteran instan pada dropdown preset terminal berdasarkan judul preset, perintah, atau tag tmux (`t:tag`) untuk eksekusi perintah diagnostik cepat.
_Avoid_: Preset finder, terminal filter, command search

**Terminal Pinned Paths Dropdown**:
Akses cepat berkas dan direktori tersemat (File Pin) langsung dari bilah alat terminal (`📌`) untuk navigasi `cd` cepat atau penyisipan path ke sesi shell.
_Avoid_: Terminal bookmarks, pin menu, path picker

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

**Fleet KPI Strip**:
Deretan ubin ringkasan armada di atas daftar Host pada dashboard (Total, Healthy, Drift, Offline, Stale, rata-rata RAM beserta puncak disk). Setiap ubin dapat diklik untuk memfilter daftar Host berdasarkan status; hitungannya memakai definisi tunggal yang sama dengan telemetri agregat di footer (Drift = status `degraded`, Offline = `down`, Stale mengikuti ambang Stale Inspection dan disupresi saat Background Inspection dinonaktifkan).
_Avoid_: Summary bar, stats header, status counter

**Design Token**:
Variabel CSS pada `:root` (permukaan, teks, aksen, warna semantik status, bentuk, bayangan, tipografi, gerak) yang menjadi satu-satunya sumber nilai visual antarmuka; kode antarmuka tidak boleh memakai warna literal sehingga tema dapat diganti hanya dengan mendefinisikan ulang token.
_Avoid_: Theme variable, color constant, style config

**Icon Sprite**:
Kumpulan ikon garis Lucide yang disematkan sebagai satu `<svg id="iconSprite">` inline di awal `<body>` dan dipakai lewat `<use href="#i-NAME">` atau helper `svgIcon('NAME')`; menggantikan seluruh emoji sebagai ikon antarmuka agar tampilan konsisten antar-OS dan tetap berjalan airgapped.
_Avoid_: Emoji icon, icon font, icon CDN

**Docker Stack**:
Satu proyek Docker Compose pada Host (kumpulan container berlabel `com.docker.compose.project`) yang dikelola sebagai satu unit di sub-tab Stacks: status agregat, up/down/start/stop/restart/pull/recreate/update, serta penyuntingan atau deploy berkas compose. Stack baru dari Pantau disimpan di `~/.pantau/stacks/<nama>/` pada Host.
_Avoid_: Compose app, docker service, swarm stack

**Docker Privilege Fallback**:
Aturan bahwa perintah Docker dijalankan sebagai akun SSH biasa dan baru memakai `sudo -n docker` bila socket Docker menolak akses (*permission denied*) dan sudo tanpa kata sandi tersedia; kata sandi tidak pernah dipakai atau disimpan.
_Avoid_: Sudo docker mode, root docker, privileged docker

**Docker Hub Browser**:
Modal pencarian repositori publik Docker Hub di tab Docker sebuah Host (hasil pencarian, ringkasan/README, dan daftar tag beserta platform, ukuran, dan waktu push) yang datanya diambil server Pantau tanpa kredensial; aksi Pull dan Run pada sebuah tag dijalankan di Host yang sedang dibuka.
_Avoid_: Image marketplace, registry browser, Docker search

**Image Update Check**:
Pemeriksaan on-demand di sub-tab Images yang membandingkan digest registry image lokal setiap `repo:tag` dari Docker Hub dengan digest yang kini disajikan Docker Hub untuk tag yang sama, menghasilkan status `up_to_date`, `update_available`, `not_found`, `unknown`, atau `error`; tidak menarik image dan tidak memakai kuota pull.
_Avoid_: Image scan, auto-update, Watchtower, vulnerability check

**Exploit Intelligence**:
Penanda opt-in (`vuln_intel_enabled`) pada Security Finding yang CVE-nya tercantum di katalog CISA Known Exploited Vulnerabilities (sedang dieksploitasi) beserta skor EPSS (peluang dieksploitasi dalam 30 hari), diambil server Pantau sekali sehari dari feed publik dan tidak disimpan; host yang tidak melaporkan ID CVE (saat ini selain dnf/yum) berstatus tidak dinilai.
_Avoid_: Threat feed, CVE scan, vulnerability score, risk score

**Vulnerability Database List**:
Daftar referensi database dan feed kerentanan dunia (salinan tak diubah dari haxdoggy/vulnerability-databases, CC BY 4.0) yang disematkan di biner dan ditampilkan dari tab Security dengan atribusi serta penanda sumber yang dipakai Pantau.
_Avoid_: CVE source list, intel catalog, feed directory

**Side Panel**:
Bilah navigasi tetap di sisi kiri antarmuka Pantau (di bawah header, di atas footer) dengan bagian Hosts, Docker, dan Kubernetes; dapat diciutkan menjadi rel ikon, dan menjadi off-canvas pada layar sempit.
_Avoid_: Sidebar, menu samping, navigation drawer

**Kubernetes Cluster Host**:
Host yang menjalankan control plane (server) k3s atau RKE2 dan dikelola Pantau lewat `kubectl` di atas SSH; klaster dideteksi per permintaan dan tidak disimpan. Node agent saja tidak didukung.
_Avoid_: K8s node, cluster entry, kubeconfig target

**Kubernetes Privilege Fallback**:
Aturan bahwa `kubectl` dijalankan sebagai akun SSH biasa dan baru memakai `sudo -n` (dengan `KUBECONFIG` k3s/RKE2) bila kubeconfig ditolak (*permission denied*) dan sudo tanpa kata sandi tersedia; kata sandi tidak pernah dipakai atau disimpan.
_Avoid_: Sudo kubectl mode, root kubectl, privileged kubectl

**Kubernetes Cluster**:
Satu klaster k3s/RKE2 yang dikenali dari UID namespace `kube-system` dan dapat dilayani oleh beberapa Host berperan server; Pantau mengelompokkan Host-nya menjadi satu entri dan beralih ke peer sehat bila Host yang diminta tidak dapat dijangkau.
_Avoid_: Cluster entry, k8s group, node group

**Gateway Host**:
Host yang sebenarnya melayani sebuah permintaan Kubernetes (Host diminta atau peer hasil failover); dilaporkan di setiap respons dan ditampilkan sebagai chip *Gateway*.
_Avoid_: Proxy host, master node, entry node

**Kube Mode**:
Kebijakan per Host atas aksi Kubernetes yang difasilitasi Pantau: `full`, `no-exec` (tanpa exec pod), atau `read-only` (tanpa mutasi dan exec). Pagar tingkat aplikasi, bukan batas keamanan Host.
_Avoid_: Permission level, RBAC role, access flag


**External Cluster**:
Klaster Kubernetes pihak ketiga atau terkelola (EKS, GKE, AKS, atau klaster mandiri) yang didaftarkan ke Pantau melalui berkas atau teks kubeconfig; dieksekusi secara lokal menggunakan `kubectl` pada server Pantau tanpa koneksi SSH ke Host control-plane.
_Avoid_: Remote cluster, cloud cluster, non-host cluster

**Kubeconfig Profile**:
Konfigurasi konteks tunggal hasil normalisasi dan minifikasi dari berkas kubeconfig yang disimpan terenkripsi di database Pantau untuk menghubungkan klaster eksternal secara aman; hanya mengizinkan kredensial inline dan plugin `exec` yang masuk daftar putih.
_Avoid_: Cluster config, kube profile, raw kubeconfig

**Cloudflare Account**:
Entri kredensial (nama, `account_id` opsional, API Token) yang terdaftar di Pantau untuk mengelola Zone Cloudflare via API.
_Avoid_: CF profile, API key entry

**Cloud Account**:
Kredensial provider cloud (AWS, Google Cloud, Azure, DigitalOcean, Hetzner, Cloudflare, Kubernetes) yang terdaftar di Pantau dan dihubungkan ke Terraform Workspace; saat Run, nilainya disuntikkan sebagai variabel lingkungan (variabel `env` Workspace menimpanya). Dapat dibuat dengan menyalin kredensial dari akun Cloudflare/cluster Kubernetes Pantau (salinan saat impor, bukan tautan langsung).
_Avoid_: Cloud profile, credential set

**Cloudflare Zone**:
Domain yang dikelola di akun Cloudflare (nama, status, plan, name server).
_Avoid_: Site, domain entry

**Zone Setting**:
Pengaturan zone yang boleh diubah Pantau (allowlist: ssl, always_use_https, security_level, development_mode, min_tls_version, automatic_https_rewrites).
_Avoid_: CF option, domain configuration

**Ansible Playbook**:
playbook YAML (nama, deskripsi, isi, extra vars bawaan) yang tersimpan di basis data Pantau.
_Avoid_: Script, template task, job definition

**Playbook Group**:
Pengelompokan logis untuk satu atau lebih Ansible Playbook pada antarmuka Pantau berbasis teks bebas `group_name` (dimodelkan dari Host Group) untuk pemisahan visual dan filter tanpa mengubah eksekusi atau hak akses.
_Avoid_: Playbook folder, playbook tag, playbook category

**Playbook Run**:
satu eksekusi Ansible Playbook terhadap sekumpulan Host (status, mode, target, exit code, log, durasi).
_Avoid_: Task, job, build

**Generated Inventory**:
inventory Ansible sementara yang dibangun otomatis dari Host terpilih (alamat, port, user, kunci SSH efektif) dan Host Group sebagai group Ansible; dihapus setelah run.
_Avoid_: Static inventory, hosts file

**Ansible Gate**:
saklar opt-in administrator (`ansible_enabled`, default mati) yang wajib aktif sebelum Playbook dapat dijalankan; dipasangkan dengan deteksi biner `ansible-playbook`.
_Avoid_: Feature flag, ansible switch

**Ansible Template**:
teks Jinja bernama (boleh bersubfolder, ≤256 KiB) yang disimpan di SQLite dan ditulis ke `templates/` di workspace setiap Playbook Run inline sehingga `template: src=` dapat merujuknya; dipakai bersama oleh semua playbook inline dan ikut System Snapshot terenkripsi.
_Avoid_: Playbook template, jinja file, task template

**Ansible File**:
berkas apa pun (termasuk biner, ≤5 MiB) yang diunggah ke Pantau dan ditulis ke `files/` di workspace setiap Playbook Run inline sehingga `copy: src=` dapat merujuknya; tidak dapat diunduh kembali dan ikut System Snapshot terenkripsi.
_Avoid_: Attachment, asset bundle, project file

**Git Playbook Source**:
Sumber Ansible Playbook berupa file YAML di repositori Git (URL https/ssh, branch/tag, path berkas, autentikasi none/token/kunci SSH Library) yang di-clone dangkal ke workspace sementara pada setiap Playbook Run; commit yang dijalankan dicatat pada run.
_Avoid_: Repository entity, git sync, playbook import

**Imported Playbook Folder**:
Salinan folder proyek Ansible yang diunggah melalui browser dan disimpan sebagai arsip ZIP privat dalam SQLite (`source=bundle`), dengan satu path YAML utama dan seluruh berkas pendukung; diekstrak sementara pada setiap Playbook Run dan ikut System Snapshot terenkripsi. Tidak mengimpor Host atau konfigurasi inventory dari proyek.
_Avoid_: Local path source, repository sync, inventory import

**Ansible Schedule**:
Jadwal cron 5-field (waktu lokal server Pantau) yang menjalankan Ansible Playbook otomatis terhadap Host/Host Group tetap; kemunculan yang terlewat lebih dari 5 menit dilewati, bukan dijalankan susulan.
_Avoid_: Cron job, timer, automation rule

**Play Recap**:
Ringkasan `PLAY RECAP` per host (ok, changed, unreachable, failed, skipped, rescued, ignored) yang diparse server saat Playbook Run selesai, disimpan bersama run, dan dikembalikan API sebagai objek.
_Avoid_: Run summary, result table, stats

**Task Tree**:
Pohon Play > Task > Host beserta status per host yang dirakit antarmuka dari log run tanpa ANSI; fitur tampilan, bukan data tersimpan.
_Avoid_: Execution graph, DAG view, job steps

**Diff Viewer**:
Tampilan per berkas hasil blok diff (`--- before`/`+++ after`/`@@`) pada run mode `diff`, dirakit antarmuka dari log tanpa ANSI.
_Avoid_: Patch view, change log, file diff

**Survey**:
Formulir variabel terstruktur milik sebuah Ansible Playbook (tipe, opsi, default, wajib) yang dijawab saat launch dan digabung ke extra vars; jawaban menimpa extra vars bawaan playbook, sedangkan extra vars eksplisit pada launch menimpa keduanya.
_Avoid_: Form vars, parameter set, input schema

**Ad-Hoc Run**:
Satu eksekusi modul Ansible (`ping`, `shell`, `setup`, …) ke Host/Host Group terpilih tanpa playbook, dengan log live dan riwayat sendiri; maksimal 3 aktif secara global, terpisah dari kuota Playbook Run.
_Avoid_: One-off task, quick command, temp playbook

**Ansible Facts**:
Hasil `setup` (`ansible_facts`) yang dikumpulkan untuk satu Host dan di-cache per host sebagai ringkasan + JSON mentah; bukan data deklarasi dan tidak ikut System Snapshot.
_Avoid_: Inventory data, host metadata, machine info

**Host & Group Explorer**:
Tab Ansible yang menampilkan pohon Host Group > Host memakai alias Generated Inventory, lengkap dengan status inspeksi Pantau, status Ping ad-hoc, dan pintasan Facts.
_Avoid_: Inventory manager, host list, group editor

**Git Sync**:
Verifikasi manual playbook bersumber Git dengan clone dangkal sementara yang melaporkan commit, branch, dan daftar YAML, lalu menyimpan waktu serta commit sync terakhir.
_Avoid_: Repo refresh, git pull, repository checkout

**Alert Debounce**:
Pengaturan (`alert_debounce_runs`, 1–10) yang menahan Incident, Alert, dan notifikasi sebuah Drift sampai terlihat pada N Inspection berturut-turut untuk rule yang sama; kembali sehat selalu menutup langsung.
_Avoid_: Alert delay, grace period, flap filter

**Bulk Command**:
Satu perintah shell non-interaktif yang dijalankan serentak ke beberapa Host terpilih (maks 20) dengan hasil per Host dialirkan ke antarmuka; tanpa riwayat dan tanpa TTY.
_Avoid_: Mass exec, fleet command, multi-host terminal

**Package Upgrade**:
Aksi menerapkan pembaruan paket (`all`, atau `security` pada dnf/yum) di sebuah Host setelah pratinjau, mengikuti Docker Privilege Fallback; tanpa reboot otomatis dan tanpa rollback.
_Avoid_: Patch run, system update, auto-update

**Terraform Workspace**:
Unit terisolasi (config + variabel + state + riwayat run) untuk satu environment.
_Avoid_: Stack, project, environment folder

**Workspace Group**:
Pengelompokan logis untuk satu atau lebih Terraform Workspace pada antarmuka Pantau berbasis teks bebas `group_name` (dimodelkan dari Host Group) untuk pemisahan visual dan filter tanpa mengubah eksekusi, state, atau hak akses.
_Avoid_: Workspace folder, stack group, workspace tag

**Terraform Run**:
Satu siklus eksekusi (plan → policy check → cost estimate → approval → apply) pada Workspace.
_Avoid_: Job, pipeline, build

**Approval Gate**:
Tahap wajib di mana Run dengan perubahan menunggu Confirm & Apply / Discard dengan komentar wajib.
_Avoid_: Manual step, review

**State Version**:
Salinan immutable `terraform.tfstate` yang dicatat di DB setiap apply/refresh/rollback/import. DB = sumber kebenaran; file di disk dimaterialisasi sebelum tiap run.
_Avoid_: State backup, snapshot

**Workspace Lock**:
Kunci Pantau yang mencegah Run baru (manual atau otomatis selama Run aktif); Force Unlock untuk darurat.
_Avoid_: Mutex, state lock file

**Variable Set**:
Kumpulan variabel yang dibagikan ke banyak Workspace (atau global).
_Avoid_: Shared vars, env group

**Detect Variables**:
Tombol di tab Variables Workspace inline yang memindai blok `variable` pada berkas `.tf` (bukan parser HCL penuh) dan menambahkan yang dipilih sebagai variabel `terraform`; tidak untuk Workspace Git.
_Avoid_: Auto-import variables

**Linked Output**:
Tautan di tab Variables Workspace yang menyuntikkan sebuah output dari State Version terbaru Workspace lain ke variabel `terraform`; nilai diselesaikan saat Run dimulai (bukan `terraform_remote_state`), output sensitif tetap tersamarkan, variabel eksplisit bernama sama menang, siklus ditolak, dan output yang tidak tersedia menggagalkan awal Run.
_Avoid_: Remote state, data source antar workspace

**State Operation**:
Aksi di tab State (`state rm`, `state mv`, import resource) yang berjalan di bawah Workspace Lock dan selalu menghasilkan State Version baru (versi lama tetap ada, sumber tercatat mis. `state rm x`); dikonfirmasi lewat dialog yang menampilkan perintah persisnya.
_Avoid_: State edit, state surgery

**Auto-apply**:
Pengaturan opt-in per Workspace (`auto_apply`) yang meng-apply otomatis plan berstatus kebijakan `passed` dan memiliki perubahan, dengan komentar `auto-apply`. Tidak pernah untuk environment produksi; Run destroy/refresh dan Run dengan peringatan kebijakan tetap melalui Approval Gate manual.
_Avoid_: Auto-deploy, continuous apply

**Terraform Drift**:
Perbedaan infrastruktur riil vs state yang terdeteksi plan `-refresh-only` (beda dari **Drift** inspeksi Host).
_Avoid_: Config drift, mismatch

**Policy Check**:
Evaluasi aturan bawaan (Go) atas plan JSON dengan level `advisory` / `soft` / `hard`. Bukan OPA/Sentinel.
_Avoid_: Compliance scan

**Cost Estimate**:
Estimasi delta biaya bulanan via biner `infracost` bila terpasang.
_Avoid_: Billing

**Terraform Gate**:
Saklar opt-in admin (`terraform_enabled`, default mati) sebelum Run dapat dijalankan.
_Avoid_: Feature flag

**Terraform Registry**:
Penjelajah modul dan provider dari Terraform Registry publik (atau mirror lewat `tf_registry_url`) yang diambil Pantau di sisi server (cache 5 menit); dipakai untuk menyisipkan snippet ke konfigurasi dan memeriksa versi (`Check versions`) pada workspace inline. Bukan registry privat dan bukan endpoint untuk `terraform init`.
_Avoid_: Module marketplace, Private Module Catalog

**Git Editor**:
Komponen antarmuka untuk menjelajah pohon berkas repositori, membuat berkas baru, menghapus berkas, menyunting konten dengan CodeMirror, serta melakukan commit dan push langsung ke repositori remote. Pada Playbook Ansible tampil sebagai bagian **Files** di tab Edit pada Side Drawer; pada Workspace Terraform tampil di Workspace Modal.
_Avoid_: Web IDE, file manager, repo manager

**Side Drawer**:
Panel yang menempel di tepi kanan area konten Ansible (di bawah header, di atas footer) untuk membuka satu Playbook atau satu Run tanpa meninggalkan tabel. Mendukung tumpukan (push/back), perlebar, dan penjagaan perubahan belum disimpan. Pola yang sama dipakai view workspace Terraform (`#tfDrawer`), dengan backdrop dan tab Overview/Runs/State/Variables/Drift/Settings.
_Avoid_: Modal, popup, flyout

**Run Viewer**:
Tampilan satu Run Ansible di dalam Side Drawer dengan empat sudut pandang atas keluaran yang sama: Log, Recap, Tasks, dan Diff; memperbarui diri selama Run aktif dan menyediakan Cancel.
_Avoid_: Log modal, console

**Repository Commit**:
Aksi merekam perubahan berkas ke riwayat Git lokal hasil clone dangkal sementara dan mendorongnya (*push*) ke branch upstream pada remote repositori tanpa opsi force push.
_Avoid_: Save file, repo patch, sync commit

**Base Commit**:
Pengenal SHA commit repositori yang sedang dilihat pengguna saat membuka atau memuat ulang Git Editor; digunakan untuk validasi *optimistic concurrency* agar perubahan ditolak jika commit remote telah berganti (409 Conflict).
_Avoid_: Parent commit, last commit, head commit

**Local Port Forward**:
Penerusan port TCP dari Host ke komputer operator (setara `ssh -L`): Pantau mendengarkan di port lokal dari rentang `-forward-ports` dan meneruskan setiap koneksi lewat channel SSH `direct-tcpip` ke `remote_host:remote_port` dilihat dari Host. Untuk klien non-browser (DBeaver, `psql`, `redis-cli`); hanya di memori, hilang saat Pantau restart.
_Avoid_: Tunnel, Remote Web Port Tunnel, SSH proxy

**Command Palette**:
Kotak pencarian global (`Ctrl+K` / `Cmd+K`, kecuali di dalam terminal atau editor) untuk melompat ke Host dan aksinya (terminal, file, Docker, inspeksi, Local Port Forward) atau menjalankan perintah global dashboard.
_Avoid_: Quick switcher, spotlight, omnibox

**Fleet Search**:
Pencarian paket dan container lintas seluruh Host dari data yang sudah tersimpan hasil scan terakhir (tanpa koneksi SSH saat mencari), lengkap dengan umur data per Host.
_Avoid_: Global grep, inventory query, live search

**Disk Usage Explorer**:
Penelusuran pemakaian disk per folder (satu tingkat, satu filesystem, seperti `ncdu`) yang dijalankan on-demand dari File Manager untuk menemukan folder penyebab disk penuh.
_Avoid_: du scan, storage audit, disk report

**Domain Inventory**:
Daftar domain yang dilayani reverse proxy di sebuah Host (nginx, Caddy, Traefik, termasuk yang berjalan di container Docker), lengkap dengan TLS, port listen, upstream tujuan, dan lokasi konfigurasinya; dikumpulkan on-demand lewat SSH lalu disimpan agar bisa dilihat per Host dan lintas Host tanpa koneksi ulang.
_Avoid_: DNS records, vhost list, site map
