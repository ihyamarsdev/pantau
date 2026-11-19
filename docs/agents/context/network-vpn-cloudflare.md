# Network, VPN & Cloudflare

Domain reference for VPN networks, network diagnostics and Cloudflare management. Read this when working in `internal/vpnnet/`, `internal/cloudflare/` or network handlers.

**Network Egress**:
Status keterhubungan keluar Host ke internet publik dan pengukuran latensi roundtrip (ms) ke resolver global.
_Avoid_: Internet status, ping check

**Private IP**:
Kumpulan alamat IPv4 privat Host (RFC 1918 dan CGNAT `100.64.0.0/10`, tanpa loopback) yang dikumpulkan setiap Inspeksi dari interface aktif, dengan entri pertama sebagai alamat utama; dipakai sebagai tujuan Probe Target *Inter-Host* saat jalur privat dipilih alih-alih alamat SSH Host.
_Avoid_: LAN IP, internal address, local IP

**Port Exposure**:
Klasifikasi tingkat keterbukaan listening port pada Host (apakah terikat pada interface publik `0.0.0.0`/`*` atau terisolasi lokal `127.0.0.1`) beserta penilaian risiko keamanan service sensitif.
_Avoid_: Open port list, firewall rule

**Socket Profile**:
Ringkasan koneksi TCP/UDP aktif pada Host yang memetakan alamat IP klien eksternal teratas, port tujuan, dan proses yang melayaninya.
_Avoid_: Netstat dump, raw connections

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

**VPN Network**:
Satu definisi jalur jaringan (driver, kredensial, interface, kebijakan auto-up) yang dipakai sekumpulan Host privat untuk keluar dari mesin master Pantau; dirujuk Host lewat `network_id` (`0` = langsung).
_Avoid_: Gateway Host, tunnel entry, connection profile

**Network Driver**:
Jenis implementasi sebuah VPN Network: `netbird`, `netmaker`, `openvpn`, `wireguard`, `tailscale`, atau `custom`.
_Avoid_: Connector, plugin, provider

**Network Path**:
Hasil resolusi sebuah VPN Network saat koneksi dibuat: nama interface untuk bind dan fungsi dial.
_Avoid_: Route, gateway, hop

**Bound Dial**:
Dial TCP yang diikat ke satu interface (`SO_BINDTODEVICE` di Linux, `IP_UNICAST_IF` di Windows, `IP_BOUND_IF` di macOS) sehingga paket keluar lewat tunnel itu tanpa mengubah tabel rute master.
_Avoid_: Route override, forced route

**Tunnel State**:
Status satu VPN Network: `down`, `starting`, `up`, atau `error`.
_Avoid_: Health, status link

**Network Down**:
Kegagalan yang berasal dari jalur VPN (tunnel mati atau interface tidak dapat di-bind), bukan dari Host; Host tetap ditandai `down` karena SSH-nya tidak bisa dibuka; catatan inspeksi menyebut jalurnya sebagai penyebab.
_Avoid_: Offline, unreachable

**Remote Web Port Tunnel**:
Reverse proxy HTTP dan WebSocket on-demand yang memanfaatkan channel OpenSSH RFC 4254 `direct-tcpip` untuk menjembatani akses browser klien ke service web internal pada Host target (`127.0.0.1:<port>`) secara *agentless*.
_Avoid_: Port forwarding, ssh tunnel proxy, local forwarder

**Local Port Forward**:
Penerusan port TCP dari Host ke komputer operator (setara `ssh -L`): Pantau mendengarkan di port lokal dari rentang `-forward-ports` dan meneruskan setiap koneksi lewat channel SSH `direct-tcpip` ke `remote_host:remote_port` dilihat dari Host. Untuk klien non-browser (DBeaver, `psql`, `redis-cli`); hanya di memori, hilang saat Pantau restart.
_Avoid_: Tunnel, Remote Web Port Tunnel, SSH proxy

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
Pengaturan zone yang boleh diubah Pantau (allowlist: ssl, always_use_https, security_level, development_mode, min_tls_version, automatic_https_rewrites, tls_1_3, http3, 0rtt, ipv6, websockets, brotli, email_obfuscation, hotlink_protection, always_online).

**IP Access Rule**:
Aturan tingkat zone Cloudflare yang memblokir, menantang (*challenge*), atau mengizinkan satu IP, rentang IP (CIDR), ASN, atau negara; dikelola dari tab Access Rules sebuah Cloudflare Zone.
_Avoid_: Firewall rule, WAF rule, ban

**Cloudflare Tunnel**:
Koneksi `cloudflared` antara origin dan edge Cloudflare milik sebuah akun; Pantau hanya menampilkan statusnya (*healthy*, *degraded*, *down*, *inactive*), tidak membuat atau mengubahnya.
_Avoid_: VPN, proxy

**Token Permission**:
Izin yang harus dimiliki API Token Cloudflare agar satu fitur Pantau berfungsi (misal `Zone → DNS → Edit`); dicatat per fitur di Panduan Pengguna dan di dialog Tambah Akun, dan izin yang kurang dilaporkan dengan namanya.
_Avoid_: Scope, role
_Avoid_: CF option, domain configuration
