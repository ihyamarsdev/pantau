# Frontend: Vanilla UI & Terminal

Domain reference for the vanilla HTML/CSS/JS UI, terminal, modals, shortcuts and the Activity Console. Read this when working in `internal/web/static/` or UI handlers.

**Workspace Modal**:
Kontainer modal layar lebar berdimensi tetap (75vw × 80vh di desktop) untuk tugas interaktif observabilitas dan data (Host Detail, Editor, Transfer Queue) tanpa perubahan ukuran layout saat navigasi antar-tab.
_Avoid_: Popup window, dynamic modal, fluid dialog

**Action Dialog**:
Kontainer modal terpusat berdimensi ringkas (lebar ~520px - 740px) untuk formulir input sekuensial, dialog pengaturan (Settings), atau konfirmasi aksi tunggal (Add Host, Direct Transfer, Restore Wizard).
_Avoid_: Small modal, mini popup, submodal

**Documentation Modal**:
Kontainer Workspace Modal interaktif berdimensi tetap (75vw × 80vh) untuk membaca panduan operasional Pantau (README, User Guide, dan Changelog) secara mandiri (airgapped) tanpa ketergantungan jaringan eksternal.
_Avoid_: Help popup, docs reader, guide dialog, manual tab, external wiki

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

**Per-Host Tab State**:
Mekanisme persistensi dan isolasi state tab aktif pada Workspace Modal secara independen untuk masing-masing Host berbasis penyimpanan sesi peramban (*sessionStorage*), memastikan navigasi tab pada satu host tidak mengubah tab default host lainnya.
_Avoid_: Global active tab, modal tab memory, shared tab state

**Editor Modal Maximize**:
Mode pembesaran kontainer Editor Modal hingga memenuhi seluruh area pandang peramban (*CSS full-viewport* 100vw × 100vh) dengan penyelarasan ulang ukuran editor teks (CodeMirror refresh) untuk kenyamanan penelaahan dan penyuntingan kode secara luas.
_Avoid_: Editor fullscreen, native editor expand, full window editor

**Side Panel**:
Bilah navigasi tetap di sisi kiri antarmuka Pantau (di bawah header, di atas footer) dengan bagian Hosts, Docker, dan Kubernetes; dapat diciutkan menjadi rel ikon, dan menjadi off-canvas pada layar sempit.
_Avoid_: Sidebar, menu samping, navigation drawer

**Side Drawer**:
Panel yang menempel di tepi kanan area konten Ansible (di bawah header, di atas footer) untuk membuka satu Playbook atau satu Run tanpa meninggalkan tabel. Mendukung tumpukan (push/back), perlebar, dan penjagaan perubahan belum disimpan. Pola yang sama dipakai view workspace Terraform (`#tfDrawer`), dengan backdrop dan tab Overview/Runs/State/Variables/Drift/Settings.
_Avoid_: Modal, popup, flyout

**Run Viewer**:
Tampilan satu Run Ansible di dalam Side Drawer dengan empat sudut pandang atas keluaran yang sama: Log, Recap, Tasks, dan Diff; memperbarui diri selama Run aktif dan menyediakan Cancel.
_Avoid_: Log modal, console

**Toast Notification**:
Pemberitahuan mengambang non-blocking di pojok antarmuka Pantau dengan ikon status (sukses, peringatan, error) dan penghilangan otomatis (*auto-dismiss*) untuk mengeliminasi pemblokiran UI thread dari modal dialog bawaan peramban.
_Avoid_: Alert popup, banner alert, notification snackbar

**Confirmation Dialog**:
Dialog aksi ringkas berbasis Promise yang menyajikan konfirmasi eksplisit sebelum eksekusi aksi destruktif atau peringatan batas sistem, menggantikan dialog konfirmasi bawaan peramban.
_Avoid_: Confirm popup, prompt dialog, native confirmation

**Design Token**:
Variabel CSS pada `:root` (permukaan, teks, aksen, warna semantik status, bentuk, bayangan, tipografi, gerak) yang menjadi satu-satunya sumber nilai visual antarmuka; kode antarmuka tidak boleh memakai warna literal sehingga tema dapat diganti hanya dengan mendefinisikan ulang token.
_Avoid_: Theme variable, color constant, style config

**Icon Sprite**:
Kumpulan ikon garis Lucide yang disematkan sebagai satu `<svg id="iconSprite">` inline di awal `<body>` dan dipakai lewat `<use href="#i-NAME">` atau helper `svgIcon('NAME')`; menggantikan seluruh emoji sebagai ikon antarmuka agar tampilan konsisten antar-OS dan tetap berjalan airgapped.
_Avoid_: Emoji icon, icon font, icon CDN

**Terminal Dock**:
Kontainer antarmuka terminal global multi-tab di level aplikasi Pantau yang dapat diminimalkan menjadi bilah dok mengambang (dock bar) atau dimaksimalkan ke seluruh viewport tanpa memutus sesi koneksi shell aktif.
_Avoid_: Terminal popup, floating window, modal shell

**Terminal Tab**:
Entitas sesi terminal independen di dalam Terminal Dock yang merepresentasikan satu koneksi PTY remote ke Host tertentu dengan dukungan penamaan judul dinamis (inline rename).
_Avoid_: Terminal window, shell pane, subterminal

**Terminal Maximize**:
Mode pembesaran antarmuka terminal interaktif hingga memenuhi seluruh area pandang peramban (full-viewport) untuk ruang operasional shell yang lebih luas tanpa memutus sesi SSH atau merusak layout modal.
_Avoid_: Native fullscreen, modal expand, popout window

**Terminal From Path**:
Aksi peluncuran sesi Terminal Tab baru di dalam Terminal Dock yang secara otomatis bernavigasi ke path direktori target atau folder induk berkas dari antarmuka SFTP File Manager.
_Avoid_: Folder shell, path terminal, file terminal, quick shell

**Terminal Clipboard Bridge**:
Mekanisme penanganan pintasan papan ketik dan papan klip (clipboard) dwiarah pada Terminal Tab yang menyelaraskan konvensi PTY Unix (`SIGINT`, *readline*) dengan integrasi *clipboard* peramban, mencakup penyalinan kontekstual (*context-aware copy*), penempelan multi-baris aman (*multiline paste confirmation*) dengan opsi perataan perintah menjadi satu baris (*single-line flatten*), dan penyalinan otomatis berbasis seleksi kursor (*copy-on-select*).
_Avoid_: Terminal copy paste, shell clipboard, browser paste hack

**Terminal Tab Host Filter**:
Penyaringan reaktif instan pada daftar pilihan Host di menu dropdown pembuatan Terminal Tab baru berdasarkan kata kunci Host Name atau Host Address (IP/FQDN).
_Avoid_: Terminal host search, host picker filter, terminal autocomplete

**Terminal Header Launcher**:
Tombol aksi terminal global terpadu pada header utama Pantau yang memicu tampilan Terminal Dock, dilengkapi badge indikator jumlah sesi PTY aktif secara real-time.
_Avoid_: Floating pill, session pill, dock launcher, terminal shortcut

**Terminal Session Badge**:
Indikator numerik reaktif pada tombol terminal kartu Host dan Terminal Header Launcher yang merefleksikan jumlah sesi shell aktif untuk masing-masing Host atau seluruh sistem.
_Avoid_: Tab counter, active count pill, connection tag

**Terminal Preset**:
Konfigurasi pintasan perintah terminal interaktif (nama label, perintah shell, target Host opsional, dan preferensi persistensi sesi) yang tersimpan di basis data Pantau untuk eksekusi instan melalui antarmuka web.
_Avoid_: Terminal shortcut, terminal macro, saved command, quick command

**Terminal Preset Live Search**:
Fitur pemfilteran instan pada dropdown preset terminal berdasarkan judul preset, perintah, atau tag tmux (`t:tag`) untuk eksekusi perintah diagnostik cepat.
_Avoid_: Preset finder, terminal filter, command search

**Terminal Pinned Paths Dropdown**:
Akses cepat berkas dan direktori tersemat (File Pin) langsung dari bilah alat terminal (`📌`) untuk navigasi `cd` cepat atau penyisipan path ke sesi shell.
_Avoid_: Terminal bookmarks, pin menu, path picker

**Terminal Link Provider**:
Komponen pendeteksi URL interaktif pada xterm Pantau yang memvalidasi link URL (`Ctrl+Click` / `Cmd+Click`) dan membedakan rute URL publik ke tab browser baru atau URL loopback remote ke Remote Web Port Tunnel.
_Avoid_: URL detector, link clicker, terminal hyperlinker

**Remote Browser Bridge**:
Mekanisme otomatisasi protokol escape sequence (OSC 1337 / OSC 777) dan penyediaan helper `$BROWSER` pada Host target yang memungkinkan perintah CLI remote (`gh auth login`, `xdg-open`, Python `webbrowser`, AWS SSO, dsb.) membuka browser desktop asli klien secara otomatis tanpa memicu text-browser headless (`w3m`/`lynx`).
_Avoid_: Web launcher, remote browser opener, headless bridge

**Port Auto-Scan**:
Mekanisme deteksi ketersediaan port jaringan lokal secara berurutan saat server web Pantau diinisialisasi dengan port default, untuk menghindari kegagalan proses akibat port yang telah terpakai.
_Avoid_: Port hopping, dynamic port binding

**Directory Filter**:
Penyaringan reaktif instan pada entri berkas dan folder di direktori aktif SFTP File Manager berdasarkan kata kunci nama berkas atau filter tipe entri (semua, folder, berkas) tanpa pemuatan ulang jaringan.
_Avoid_: File search, search bar, deep finder

**File Sorting**:
Pengorganisasian urutan entri berkas dan folder pada antarmuka SFTP File Manager berdasarkan atribut kolom (nama, ukuran, tanggal modifikasi) dengan prioritas entri folder di atas berkas (*folders first*).
_Avoid_: File order, column ordering

**File Duplication**:
Proses penggandaan berkas atau direktori secara lokal pada Host yang sama melalui antarmuka SFTP File Manager dengan penamaan default berbasis timestamp, validasi kecukupan ruang penyimpanan (*Disk Space Safety Check*), dialog konfirmasi penamaan, dan penolakan tabrakan nama untuk mencegah kehilangan data.
_Avoid_: Remote clone, file copy local, dup item

**Disk Space Safety Check**:
Pemeriksaan kapasitas penyimpanan riil pada partisi target Host sebelum eksekusi duplikasi untuk memastikan ketersediaan ruang disk mencukupi ukuran berkas atau direktori ditambah batas aman (*safety buffer* 100 MB).
_Avoid_: Disk quota check, storage probe, free space validator

**Interactive Breadcrumb**:
Komponen navigasi jalur direktori pada SFTP File Manager yang memecah path aktif menjadi segmen-segmen hierarki yang dapat diklik langsung untuk berpindah ke direktori induk tanpa perlu menekan tombol navigasi atas berulang kali.
_Avoid_: Path clicker, folder tree strip, breadcrumb trail

**File Pin**:
Sematkan berkas atau direktori penting per host lengkap dengan catatan operasional (*notes*) untuk akses instan di pengelola berkas.
_Avoid_: Bookmarked file, favorite path, saved item

**Global Bookmarks**:
Modal penjelajah global di header utama untuk menelusuri, memfilter, mengedit, atau membuka sesi terminal ke seluruh File Pin di semua host terkelola.
_Avoid_: Saved files list, pinned manager, favorites modal

**Command Palette**:
Kotak pencarian global (`Ctrl+K` / `Cmd+K`, kecuali di dalam terminal atau editor) untuk melompat ke Host dan aksinya (terminal, file, Docker, inspeksi, Local Port Forward) atau menjalankan perintah global dashboard.
_Avoid_: Quick switcher, spotlight, omnibox

**Domain Inventory**:
Daftar domain yang dilayani reverse proxy di sebuah Host (nginx, Caddy, Traefik, termasuk yang berjalan di container Docker), lengkap dengan TLS, port listen, upstream tujuan, dan lokasi konfigurasinya; dikumpulkan on-demand lewat SSH lalu disimpan agar bisa dilihat per Host dan lintas Host tanpa koneksi ulang.
_Avoid_: DNS records, vhost list, site map

**Domain Shortcut**:
Pintasan satu klik ke sebuah domain dari Domain Inventory yang ditandai bintang oleh administrator, ditampilkan sebagai tile (domain dan Host-nya) di atas dashboard Host dan membuka domain tersebut di tab browser baru; disimpan di browser administrator, bukan di basis data Pantau.
_Avoid_: Bookmark, favorite domain, pinned site, service tile

**Custom Shortcut**:
Tautan buatan administrator ke alamat web eksternal (nama, alamat `http(s)`, dan grup opsional) yang ditampilkan sebagai tile per grup pada bagian Shortcuts di Side Panel dan dibuka di tab browser baru; disimpan di basis data Pantau sehingga berlaku untuk semua browser dan ikut System Snapshot. Berbeda dari Domain Shortcut, yang berasal dari Domain Inventory dan disimpan per browser.
_Avoid_: Bookmark, external link, service tile, homepage entry

**Shortcut Auto Login**:
Kredensial opsional pada sebuah Custom Shortcut (nama pengguna, kata sandi, alamat formulir login, nama kolom pengguna dan sandi, serta kolom tambahan) yang membuat tile-nya menaut ke halaman Pantau `/shortcut-login/<id>`; halaman itu mem-POST kredensial ke formulir login situs tujuan sebagai navigasi biasa, sehingga satu klik berakhir di situs dalam keadaan sudah masuk. Kata sandi hanya bisa ditulis (API mengembalikan `login_pass_set`, bukan nilainya) dan halaman itu tidak di-cache serta tidak mengirim referrer.
_Avoid_: Saved password, credential store, password manager, SSO

**Activity Console**:
Drawer diagnostik collapsible di bagian bawah antarmuka pengguna Pantau yang menyajikan running table aktivitas runtime aplikasi secara interaktif, pausable, filterable, dan exportable.
_Avoid_: Log window, debug popup, terminal tab, output box

**Live Event Stream**:
Langganan streaming Server-Sent Events (`/api/logs/stream`) yang mendistribusikan telemetri runtime terstruktur secara real-time dari Diagnostic Logger ke sesi web pengguna menggunakan standar Go `http.Flusher` dan browser native `EventSource`.
_Avoid_: Log socket, polling feed, stdout pipe, log websocket

**Telemetry Ring Buffer**:
Penyimpanan buffer sirkular in-memory berkapasitas tetap (100 entri) yang menyimpan rekaman telemetri terbaru untuk memberikan konteks operasional instan saat sesi web terhubung.
_Avoid_: Memory dump, unconstrained queue, persistent log table

**Smart Auto-Scroll**:
Perilaku pelacakan viewport pada Activity Console yang secara otomatis menangguhkan auto-scroll saat pengguna menggulir ke atas untuk meninjau log historis dan melanjutkannya kembali saat digulir ke entri terbaru.
_Avoid_: Forced scroll, jump lock

**Redaction Filter**:
Filter batas deterministik yang mencegat pesan dan atribut log telemetri untuk menyensor password SSH, private key, token autentikasi, recovery code, dan passphrase menjadi `[REDACTED]` sebelum dipancarkan ke Live Event Stream.
_Avoid_: Secret hiding, raw dump, password censor

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
