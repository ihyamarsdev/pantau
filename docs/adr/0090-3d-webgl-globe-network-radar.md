# 0090. 3D WebGL Globe Network Radar

Visualisasi armada Pantau sebagai **bola Bumi 3D berpeta negara**, digambar
GPU-akselerasi dengan **WebGL2 + GLSL native** (tanpa Three.js, Mapbox, Cesium,
atau CDN). Server diposisikan menurut geolokasi IP publiknya, negara tempat
armada berada disorot, dan aliran lalu lintas *dwiarah* beranimasi antar-server.
Seluruh aset tersemat di binary Go melalui `embed.FS` dan dikompresi memori
sesuai [ADR 0013](0013-airgapped-self-contained-web-assets.md)
dan [ADR 0018](0018-in-memory-gzip-compression-and-etag-caching.md).

## Konteks dan Masalah

1. Radar konsentris pendahulu menampilkan topologi makro dalam koordinat polar
   buatan; ia tidak menjawab "di mana server saya secara geografis" maupun
   "bagaimana lalu lintas mengalir antar-benua", sehingga kini dihapus.
2. Renderer ortografik Canvas 2D pendahulu memproyeksikan bola ke 2D: tidak ada
   pencahayaan 3D, atmosfer Fresnel, kedalaman nyata, maupun bus yang melengkung
   di atas permukaan bola — batasan mendasar proyeksi datar.
3. Pustaka globe populer (Three.js, Mapbox GL, Cesium) berukuran besar dan
   lazimnya menarik tile peta dari CDN, melanggar komitmen *zero-dependency* dan
   operasi airgapped Pantau ([ADR 0013](0013-airgapped-self-contained-web-assets.md)).
4. Lookup GeoIP publik tidak boleh dipanggil pada jalur baca HTTP: lambat,
   dikenakan kuota, dan gagal saat jaringan terputus. IP privat / Host VPN
   ([ADR 0077](0077-jalur-jaringan-per-host-driver-vpn-dan-dial-terikat-interface.md))
   tidak memiliki geolokasi yang dapat di-resolve.
5. Latensi antar-Host tidak pernah dipersisten: `netdiag`
   ([ADR 0033](0033-agentless-network-diagnostics-and-hop-analysis.md))
   hanya mengalirkan hasil lewat SSE lalu membuangnya.
6. Titik daratan grid 3° (iterasi pertama) hanya menunjukkan bentuk benua: tidak
   ada negara, batas, atau nama. Peta harus berupa **negara** yang bisa
   ditunjuk, tetap tanpa tile peta dari luar, dan tampilannya harus sederhana:
   bola bersih, tanpa bintang atau graticule yang mengganggu.

## Keputusan Arsitektur

### 1. Resolver GeoIP + Cache SQLite 30 Hari (Backend)

- Tabel `geoip_cache` di `internal/store/` menyimpan
  `(ip, lat, lon, region, city, country_code, asn_org, source, updated_at)` dengan
  `ip` sebagai PRIMARY KEY. TTL 30 hari (`store.GeoIPCacheTTL`); entri basi tetap
  disajikan bila refresh gagal, sehingga offline tidak pernah kehilangan posisi
  pin yang sudah diketahui.
- `internal/globe.Resolver` melakukan lookup HTTP dengan *deadline* 3 detik dan
  *rate limit* 2 detik antar-panggilan, dijalankan dari goroutine latar
  (`Server.StartGeoResolver`, dipanggil dari `main.go`) sekali saat start dan
  setiap 6 jam — tidak pernah dari handler baca. Fungsi lookup dapat
  disuntikkan (`LookupFn`) agar unit test berjalan 100% offline.
- Prioritas penentuan posisi Host: **(1)** override manual `geo_lat`, `geo_lon`,
  `geo_region`, `geo_label` (kolom di `hosts`, API
  `POST /api/network-radar/globe/geo`); **(2)** cache GeoIP untuk IP publik;
  **(3)** fallback lokasi Gateway `(0,0)` untuk IP privat / air-gapped / offline.
- IP privat (RFC1918, CGNAT `100.64.0.0/10`, loopback, link-local) tidak pernah
  dikirim ke layanan GeoIP eksternal.

### 2. Agregasi Topologi & Link (`GET /api/network-radar/globe`)

- Paket `internal/globe` memproyeksikan inventori tersimpan menjadi `nodes`
  (Gateway + Host + marker Cloudflare/K8s) dan `links` berarah/dwiarah. Cache
  in-memory TTL 20 detik, diinvalidasi setelah penulisan konfigurasi.
- Setiap node memuat `id`, `name`, `ip`, `public_ip`, `lat`, `lon`, `region`,
  `city`, `country_code`, `asn_org`, `status`, `cpu_pct`, `mem_pct`,
  `active_alerts`, `group_id`, `network_id`, `kind`, `geo_source`.
- **Micro-spiral deterministik**: Host yang berbagi koordinat `(lat,lon)` persis
  (satu datacenter) di-offset pada spiral sudut emas ~0,02° per peringkat,
  diurutkan berdasarkan ID, sehingga pin 3D tidak saling menutupi dan layout
  stabil antar-build.
- Link disusun dari data tersimpan, tanpa panggilan jaringan baru:
  1. **Gateway ↔ Host** (`ssh`, atau `vpn` bila `network_id != 0`), latensi dari
     `probe_results` (`source_host_id = 0` = Gateway).
  2. **Host ↔ Host `proxy`/`app`** dari `host_domains.Upstreams`
     (`internal/inspector/webproxy*.go`) dan dari socket TCP established
     (`TopConnections.RemoteIP` yang cocok dengan IP publik/privat/VPN Host lain).
  3. **`k8s`** dari Host dengan `kube_cluster_id` sama (control-plane ↔ worker).
  4. **`vpn`** dari peer satu VPN Network (ADR 0077).
  5. **`cloudflare`** dari kecocokan nama akun dengan domain tersimpan.
- Link antar-server ditandai `bidirectional: true` dan membawa `label` tipe;
  `netdiag` menyimpan hasil ping ke `probe_results` sebagai satu-satunya sumber
  latensi persisten baru.

### 3. Renderer WebGL2 + GLSL Native (Frontend)

- Bundle mandiri `static/app/globe/` (dikonkatenasi lexical oleh
  `serveAppBundle`, di-gzip in-memory + ETag):
  - `00-countries.js` — 225 negara Natural Earth 1:50m admin-0 (domain publik),
    disederhanakan Douglas–Peucker 0,06° dan dikuantisasi 0,05°, lalu disimpan
    sebagai delta varint (~58 KB mentah, ~39 KB gzip). Dibangkitkan oleh
    `tools/globe-countries/gen.py` (hanya library standar Python); jangan
    disunting tangan. Ring dari satu negara digambar dengan aturan even-odd,
    sehingga lubang (Lesotho di Afrika Selatan) tidak butuh data arah putaran.
  - `02-globe-geo.js` — lat/lon → bola, dekode dataset, `countryAt(lat, lon)`
    (point-in-polygon even-odd per negara, bbox lebih dulu), `countryName`
    (nama terlokalisasi dari `Intl.DisplayNames`, nama Inggris dataset sebagai
    cadangan), dan dua pelukis Canvas 2D (peta dasar dan lapisan sorot).
  - `03-globe-render.js` — `WebGlobe.Renderer`: bola 128×64 dengan UV per verteks
    (turunan tekstur tetap kontinu di antimeridian), halo atmosfer, pin, busur.
  - `04-globe.js`–`06-globe-ui.js` — modal Workspace
    ([ADR 0007](0007-standardized-workspace-modal-layout.md), token
    [ADR 0039](0039-modern-visual-design-system-and-icon-sprite.md)), sidebar,
    filter, kamera, picking, HUD dengan pintasan Terminal / Diagnostik /
    Workspace, dan form override geo.
- **Peta**: saat modal dibuka, poligon negara dilukis sekali di Canvas 2D menjadi
  tekstur ekuirektangular 4096×2048 (samudra, isi negara dalam empat nada biru
  agar tetangga berbeda, lalu satu goresan batas — sehingga batas bersama dan
  garis pantai sama tebal; sekitar 10 ms), diunggah ber-mipmap dengan
  anisotropik, dan kanvasnya dibebaskan. Segmen yang berada di potongan
  antimeridian dan di kutub tidak digoresi.
- **Lapisan sorot**: tekstur premultiplied 2048×1024 terpisah, dilukis ulang
  hanya saat himpunan sorotan berubah: negara yang memuat Host (warna aksen
  lembut), negara di bawah kursor, dan negara Host terpilih (lebih kuat +
  garis tepi). Nama negara dan jumlah Host-nya tampil di tooltip.
- **Tampilan**: satu bola dengan cahaya lembut dari kiri atas, rim light tipis,
  dan halo atmosfer yang dihitung dari jarak terdekat sinar kamera ke bola
  (satu segitiga layar penuh, tanpa mesh). Tanpa starfield dan tanpa graticule.
  Latar hanya gradien CSS dari token tema.
- **Pin dan busur**: pin adalah *point sprite* berukuran piksel tetap (inti,
  cincin pemisah gelap, cahaya, cincin berdenyut untuk alert, cincin seleksi).
  Busur adalah pita yang dilebarkan di ruang layar, mengikuti lingkaran besar
  (slerp) dan terangkat sebanding panjang tali busur; denyut "komet" (dua arah
  untuk link dwiarah) dihitung di fragment shader dari waktu, jadi geometri
  statis dan hanya dibangun ulang saat graf, filter, atau seleksi berubah —
  tidak ada alokasi buffer per frame.
- **Kamera**: kamera tetap di +Z; bola diputar `Rx(lat)·Ry(−lon)`. Zoom
  menyempitkan lensa (tanpa dolly), jadi perspektif tidak berubah. Konversi
  posisi: `x = r·cos(lat)·sin(lon)`, `y = r·sin(lat)`, `z = r·cos(lat)·cos(lon)`
  (timur di kanan). `project` memproyeksikan pin ke layar dan `unproject`
  memotong sinar kursor dengan bola untuk hover negara.
- **Interaksi**: drag dengan inersia berbasis waktu, roda untuk zoom,
  *fly-to* halus saat Host dipilih (dari sidebar atau pin), putar-otomatis yang
  berhenti selama sebuah Host dipilih agar pinnya tidak hanyut dari HUD. Filter
  tipe link, grup, dan pencarian memengaruhi apa yang digambar, bukan hanya
  hitungan.
- **Siklus hidup & performa**: loop `requestAnimationFrame` dihentikan saat
  modal ditutup (`closeModal` → `GlobeRadar.onClose`) atau `document.hidden`;
  DPR dibatasi `Math.min(devicePixelRatio, 2)`; seluruh objek GL dan kanvas
  sorot di-`dispose` saat ditutup (konteks hanya dilepas bila kanvas diganti,
  mis. saat ganti bahasa). SSE `/api/logs/stream`
  ([ADR 0030](0030-in-app-activity-console-and-live-event-stream.md))
  memperbarui warna status dan menaikkan kecerlangan denyut secara in-place
  tanpa memuat ulang graf.

### 4. Fallback WebGL2

Jika `canvas.getContext('webgl2')` gagal atau shader tidak terkompilasi, panggung
menampilkan pesan `gl_webgl_failed` dan tidak pernah memuat pustaka eksternal —
menjaga prinsip airgapped meski pada browser tanpa dukungan WebGL2. Daftar Host
di sidebar tetap berfungsi.

## Konsekuensi

- Globe berjalan penuh di jaringan terisolasi: peta negara, font, ikon, dan
  shader tersemat; nol permintaan ke pihak ketiga. Tidak ada vendoring Three.js
  yang diperlukan karena renderer ditulis langsung dengan WebGL2 + GLSL.
- Data peta menambah ~39 KB (gzip) pada bundel globe, dimuat hanya bersama
  bundel itu. Pembaruan data = menjalankan `tools/globe-countries/gen.py`.
- Batas negara mengikuti Natural Earth (termasuk wilayah sengketa sebagaimana
  digambarnya); Pantau tidak menetapkan posisi politik.
- Memori GPU ±55 MB saat modal terbuka (peta 4096×2048 ber-mipmap + lapisan
  sorot), dibebaskan saat ditutup.
- Backend baca tetap murah — satu cache TTL, tanpa SSH/API call — sementara
  biaya GeoIP dibayar sekali per IP per 30 hari di latar.
- Host privat tanpa override tampil di titik Gateway dengan penanda
  `geo_source: gateway`, bukan posisi mengarang.
- Paket `internal/globe` hanya bergantung pada `internal/store`; `go.mod`
  tetap bersih.

## Alternatif yang Dipertimbangkan

1. **Vendoring Three.js minified** — ditolak: ~600 KB aset pihak ketiga,
   permukaan serangan & siklus update tambahan, dan tidak diperlukan karena
   kebutuhan renderer tercakup WebGL2 native.
2. **Canvas 2D ortografik (versi pendahulu)** — ditolak: tanpa
   kedalaman/pencahayaan 3D nyata dan bus tidak benar-benar melengkung di atas
   bola.
3. **Titik daratan grid 3° (iterasi pertama)** — digantikan: tidak ada negara,
   batas, atau nama; dan data base36-nya tidak pernah didekode oleh renderer
   WebGL sehingga bola tampil gundul.
4. **Natural Earth 1:110m (~10 ribu verteks)** — ditolak: lebih kecil, tetapi
   garis pantainya kasar saat di-zoom dan negara kecil tempat hosting umum
   (Singapura, Malta, Bahrain) tidak ada, sehingga pinnya jatuh di laut.
   1:50m yang disederhanakan hanya ~39 KB gzip.
5. **Batas negara sebagai garis GL / segitiga-kan poligon di bola** — ditolak:
   butuh triangulasi poligon cekung pada bola dan shader garis tebal untuk
   hasil yang sama dengan satu tekstur yang dilukis Canvas 2D (anti-alias dan
   mipmap gratis).
6. **Peta ID negara per-piksel untuk hover** — ditolak: ID tidak boleh
   diinterpolasi, jadi tepi sorotan berkedip saat bola berputar; point-in-polygon
   di CPU (225 bbox + beberapa ring) cukup murah per gerakan kursor dan sorotan
   memakai kanvas Canvas 2D yang ter-anti-alias.
7. **SVG orthographic** — ditolak: ribuan elemen DOM membuat animasi lambat.
8. **GeoIP pada jalur baca** — ditolak: latensi pengguna dan kuota API; cache
   latar + TTL 30 hari lebih dapat diprediksi.

## Verifikasi

- Unit: cache GeoIP SQLite (fresh/stale/upsert + region), fallback IP privat &
  override manual, micro-spread koordinat sama, agregasi link
  (ssh/proxy/app-TCP/vpn/k8s/cloudflare) dengan store palsu, ETag handler.
- E2E: `e2e/tests/network-globe.e2e.ts` — payload di-stub; modal WebGL terbuka
  dan keadaan kosong tersembunyi; dataset negara terdekode (`countryAt`); piksel
  daratan lebih terang daripada samudra pada kanvas GL; hover menyebut nama
  negara; filter tipe link mengubah jumlah busur yang digambar; HUD Host
  menampilkan detail, nama negara, dan pintasan.
