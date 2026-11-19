# 0090. 3D Globe Network Radar

Visualisasi armada Pantau sebagai bola 3D ortografik berbasis Canvas 2D murni:
Host ditempatkan menurut geolokasi IP publiknya, dengan bus lintas-server
beranimasi yang menunjukkan jalur SSH, proxy, VPN, Kubernetes, dan Cloudflare.
Tanpa Three.js, Mapbox, WebGL, atau CDN — seluruhnya vanilla HTML/CSS/Canvas JS
sesuai [ADR 0013](0013-airgapped-self-contained-web-assets.md).

## Konteks dan Masalah

1. Radar konsentris [ADR (topology)] menampilkan topologi makro dalam
   koordinat polar buatan (sector/sectors); ia tidak menjawab "di mana server
   saya secara geografis" maupun "bagaimana lalu lintas mengalir antar benua".
2. Pustaka globe populer (Three.js, Mapbox GL, Cesium) berukuran besar,
   memerlukan WebGL, dan lazimnya menarik tile peta dari CDN — melanggar
   komitmen *zero-dependency* dan operasi airgapped Pantau.
3. Lookup GeoIP publik tidak boleh dipanggil pada jalur baca HTTP: ia lambat,
   dikenakan kuota, dan gagal total saat jaringan terputus. IP privat / Host
   VPN ([ADR 0077](0077-jalur-jaringan-per-host-driver-vpn-dan-dial-terikat-interface.md))
   tidak memiliki geolokasi yang dapat di-resolve sama sekali.
4. Latensi antar-Host tidak pernah dipersisten: `netdiag` ([ADR 0033](0033-agentless-network-diagnostics-and-hop-analysis.md))
   hanya mengalirkan hasil lewat SSE lalu membuangnya.

## Keputusan Arsitektur

### 1. Resolver GeoIP + Cache SQLite 30 Hari (Backend)

- Tabel baru `geoip_cache` di `internal/store/` menyimpan
  `(ip, lat, lon, city, country_code, asn_org, source, updated_at)` dengan
  `ip` sebagai PRIMARY KEY. TTL 30 hari (`store.GeoIPCacheTTL`); entri basi
  tetap disajikan bila refresh gagal, sehingga offline tidak pernah kehilangan
  posisi pin yang sudah diketahui.
- `internal/globe.Resolver` melakukan lookup HTTP dengan *deadline* 3 detik dan
  *rate limit* 2 detik antar-panggilan. Ia dijalankan dari goroutine latar
  (`Server.StartGeoResolver`, dipanggil dari `main.go`) sekali saat start dan
  setiap 6 jam — tidak pernah dari handler baca. Fungsi lookup dapat
  disuntikkan (`LookupFn`) agar unit test berjalan 100% offline.
- Prioritas penentuan posisi Host: **(1)** override manual `geo_lat`,
  `geo_lon`, `geo_label` (kolom baru di `hosts`, API
  `POST /api/network-radar/globe/geo`); **(2)** cache GeoIP untuk IP publik;
  **(3)** fallback lokasi Gateway `(0,0)` untuk IP privat / air-gapped / offline.
- IP privat (RFC1918, CGNAT `100.64.0.0/10`, loopback, link-local) tidak pernah
  dikirim ke layanan GeoIP eksternal.

### 2. Agregasi Geo-Topologi & Flow (`GET /api/network-radar/globe`)

- Paket `internal/globe` memproyeksikan inventori tersimpan menjadi `nodes`
  (Gateway + Host + marker Cloudflare/K8s) dan `flows` berarah. Cache in-memory
  TTL 20 detik, diinvalidasi oleh `invalidateTopology()` setelah penulisan
  konfigurasi Host/Jaringan/Klaster.
- Setiap node memuat `id`, `name`, `ip`, `public_ip`, `lat`, `lon`, `city`,
  `country_code`, `status`, `cpu_pct`, `mem_pct`, `active_alerts`, `group_id`,
  `network_id`, `kind`, dan `geo_source`.
- Flow disusun dari data yang sudah ada, tanpa panggilan jaringan baru:
  1. **Gateway → Host** (`ssh`, atau `vpn` bila `network_id != 0`), latensi dari
     tabel `probe_results` (`source_host_id = 0` = Gateway).
  2. **Host → Host** `proxy` dari `host_domains.Upstreams`
     (`internal/inspector/webproxy*.go`); `k8s` dari Host dengan
     `kube_cluster_id` sama; `vpn` dari peer satu VPN Network (ADR 0077).
  3. **Cloudflare → Origin** `cloudflare` dari kecocokan nama akun dengan
     domain tersimpan. Tunnel/Zone tidak dipersisten, sehingga tidak ada flow
     yang direkayasa tanpa bukti tersimpan.
- `netdiag` menyimpan hasil ping ke `probe_results` saat event `ping_stats`
  dipancarkan; ini satu-satunya sumber latensi persisten baru.

### 3. Renderer Canvas 2D Ortografik (Frontend)

- Bundle mandiri `static/app/globe/` terdiri dari:
  - `00-landdots.js` — 2.352 titik daratan pada grid 3° (~9 KB raw), tiap titik
    dikodekan `base36(latIdx<<8 | lonIdx)`. Data ini di-embed dan dikompresi
    otomatis oleh lapisan gzip in-memory [ADR 0018](0018-in-memory-gzip-compression-and-etag-caching.md).
  - `01-globe.css` — Workspace Modal 75vw × 80vh [ADR 0007](0007-standardized-workspace-modal-layout.md),
    seluruh warna memakai token [ADR 0039](0039-modern-visual-design-system-and-icon-sprite.md).
  - `02-globe.js` — proyeksi ortografik: rotasi yaw/pitch, culling belahan
    belakang (dot redup), graticule, bus *great-circle* dengan lengkung ketinggian,
    dan paket beranimasi berwarna menurut tipe flow.
- Interaksi: drag-to-rotate dengan inersia, wheel zoom (clamp 0.6–3),
  auto-rotasi yang bisa dimatikan, fly-to halus saat memilih Host dari sidebar,
  toggle tipe flow, filter grup, dan HUD kartu dengan pintasan Terminal /
  Diagnostik / Host Workspace serta form override geo.
- Siklus hidup: `requestAnimationFrame` dihentikan saat modal ditutup
  (`closeModal` → `GlobeRadar.onClose`) atau `document.hidden === true`;
  DPR dibatasi `Math.min(devicePixelRatio, 2)`; SSE `/api/logs/stream`
  ([ADR 0030](0030-in-app-activity-console-and-live-event-stream.md))
  memperbarui warna status dan pulse secara in-place tanpa mengambil ulang
  seluruh dataset.

## Konsekuensi

- Globe bekerja penuh di jaringan terisolasi: peta, font, dan ikon tersemat;
  tidak ada permintaan ke pihak ketiga.
- Backend baca tetap murah — satu cache TTL, tanpa SSH/API call — sementara
  biaya GeoIP dibayar sekali per IP per 30 hari di latar.
- Host privat tanpa override tampil di titik Gateway dengan penanda
  `geo_source: gateway`, bukan posisi yang mengarang.
- Paket `internal/globe` hanya bergantung pada `internal/store`; tidak
  menambah dependensi `go.mod`.

## Alternatif yang Dipertimbangkan

1. **Three.js / WebGL globe** — ditolak: ukuran besar, butuh WebGL, dan
   ketergantungan peta eksternal bertentangan dengan ADR 0013.
2. **SVG orthographic (seperti radar konsentris)** — ditolak untuk kasus ini:
   ribuan elemen DOM dot membuat interaksi dan animasi paket lambat; Canvas 2D
   menggambar seluruh bola dalam satu elemen.
3. **GeoIP pada jalur baca** — ditolak: latensi pengguna dan kuota API; cache
   latar + TTL 30 hari lebih dapat diprediksi.

## Verifikasi

- Unit: cache GeoIP SQLite (fresh/stale/upsert), fallback IP privat & override
  manual, agregator flow (ssh/proxy/vpn/k8s/cloudflare) dengan store palsu.
- E2E: `e2e/tests/network-globe.e2e.ts` — API globe mengembalikan node/flow
  dengan status yang di-stub, modal terbuka, filter flow bekerja, dan HUD
  menampilkan detail Host.
