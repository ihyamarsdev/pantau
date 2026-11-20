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
- IP privat (RFC1918, CGNAT `100.64.0.0/10`, loopback, link-local) tidak pernah
  dikirim ke layanan GeoIP eksternal. `ResolveIP` memeriksa ini secara langsung
  (bukan hanya di `RunOnce`), sehingga jalur mana pun yang memanggilnya tetap aman.
- Respons provider divalidasi: status `success` dan koordinat latitude/longitude
  harus berada dalam rentang serta bukan `(0,0)`; respons rusak ditolak sehingga
  tidak pernah menaruh pin di luar bola atau di Gateway dengan label `geoip`.
- **Host baru / relokasi**: `POST /api/hosts` dan `PUT /api/hosts/{id}` memicu
  `resolveHostGeo` di goroutine latar yang memanggil `Resolver.ResolveHost` untuk
  IP publik host saat ini, lalu membatalkan cache globe. Karena cache GeoIP
  ber-primary-key `ip`, IP baru mendapat resolusi baru dan IP lama yang tersimpan
  tidak pernah menimpa posisi host yang sudah pindah. Rate limiter memakai mutex
  agar panggilan konkuren tidak menghasilkan ledakan request. Entri basi sengaja
  **dipertahankan**: penghapusan basi sebelumnya menghilangkan pin yang sudah
  diketahui saat refresh gagal (bertentangan dengan aturan offline di bawah).

### 2. Agregasi Topologi & Link (`GET /api/network-radar/globe`)

- Paket `internal/globe` memproyeksikan inventori tersimpan menjadi `nodes`
  (Gateway + Host + marker Cloudflare/K8s) dan `links` berarah/dwiarah. Cache
  in-memory TTL 20 detik, diinvalidasi setelah penulisan konfigurasi.
- Setiap node memuat `id`, `name`, `ip`, `public_ip`, `lat`, `lon`, `region`,
  `city`, `country_code`, `asn_org`, `status`, `cpu_pct`, `mem_pct`,
  `active_alerts`, `group_id`, `network_id`, `kind`, `geo_source`.
- **Micro-spiral deterministik**: node yang berbagi koordinat `(lat,lon)` persis
  — Host satu datacenter, Host privat/fallback di Gateway `(0,0)`, dan marker
  K8s — di-offset pada spiral sudut emas ~0,02° per peringkat, diurutkan
  berdasarkan ID (ID terendah tetap di tempat), sehingga pin 3D tidak saling
  menutupi dan layout stabil antar-build.
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
  - `globe-admin1.js` — bundel same-origin terpisah (~234 KB gzip) berisi garis
    admin-1 provinsi/negara Natural Earth 1:10m (domain publik), dibangkitkan
    oleh `tools/globe-countries/admin1.py`; dimuat malas lewat *dynamic import*
    hanya saat zoom melewati 2,5x, jadi tidak menambah ukuran bundel awal.
  - `02-globe-geo.js` — lat/lon → bola, dekode dataset, `countryAt(lat, lon)`
    (point-in-polygon even-odd per negara, bbox lebih dulu), `countryName`
    (nama terlokalisasi dari `Intl.DisplayNames`, nama Inggris dataset sebagai
    cadangan), dan pelukis Canvas 2D (peta dasar, lapisan sorot, dan tekstur
    detail close-zoom).
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
- **Seleksi negara → seluruh provinsi**: pemilih negara di toolbar (atau klik
  pada negara di bola tanpa pin) memilih sebuah negara dan menerbangkan kamera
  ke bbox-nya. Bundle admin-1 memuat indeks `iso` (kode ISO alpha-2 → indeks
  baris) yang dibangkitkan dari `ADM0_A3` Natural Earth dipetakan ke ISO alpha-2
  memakai properti `ISO_A2_EH`/`ISO_A2` admin-0; jadi `ID` → seluruh 218 garis
  provinsi Indonesia. Saat sebuah negara dipilih, tekstur detail dilukis ulang
  dengan **semua** garis admin-1 negara itu di bbox penuhnya, tanpa bergantung
  pada viewport atau zoom — kepulauan seperti Indonesia menampilkan semua
  provinsinya sekaligus (Sumatra sampai Papua), bukan hanya garis di dekat kursor.
  Array garis dipetakan sekali saat dekode dan tekstur hanya dilukis ulang bila
  pilihan/ukuran berubah, sehingga tidak ada alokasi per-frame. Interaksi pin
  dan HUD Host tetap seperti semula (pin menang di atas klik negara).
- **Tekstur detail close-zoom**: saat zoom melewati 2,5x tanpa negara terpilih,
  garis admin-1 provinsi/negara Natural Earth 1:10m dilukis ke tekstur Canvas 2D
  lalu dicampur di shader di atas peta dasar, dengan sampling per-fragmen memakai
  lat/lon eksak dari arah sinar (bukan UV mesh yang meleset ~2 km di dekat
  permukaan). Garis pantai tetap Natural Earth 1:50m. Tanpa dependensi baru;
  sifat airgapped terjaga.
- **Hierarki Negara → Provinsi → Kabupaten/Kota (Indonesia)**: toolbar memiliki tiga pemilih bertingkat.
  Memilih Indonesia (`ID`) mengaktifkan pemilih **Provinsi**, lalu memilih sebuah provinsi mengaktifkan pemilih
  **Kabupaten/Kota**. Data berasal dari bundel same-origin `globe-admin2.js` yang dimuat malas saat Indonesia
  dipilih — bukan garis admin-1 tanpa nama, melainkan **poligon bernama nyata** dengan bbox asli, sehingga tiap
  fitur bisa difokuskan dan disorot. Sumber: `cahyadsn/wilayah_boundaries` ([MIT](https://github.com/cahyadsn/wilayah_boundaries/blob/main/LICENSE),
  batas sesuai Kepmendagri No. 300.2.2-2430 Tahun 2025), di-pin pada commit
  `2c0d8c791af7ca55ee06d17b8f34813e8e6bf1b1`. Generator `tools/globe-countries/admin2.py` (hanya library standar
  Python; `--fetch` mengunduh commit yang di-pin) membaca dump SQL `db/prov/` dan `db/kab/`, menyederhanakan
  Douglas–Peucker 0,003°, mengkuantisasi 1/1000° (~110 m), lalu menyandikan delta varint. Cakupan terverifikasi:
  **38 provinsi** dan **514 kabupaten/kota** (416 kabupaten + 98 kota), setiap kabupaten memiliki kode induk
  2-digit yang cocok dengan provinsinya (nol yatim), termasuk Kota administrasi seperti Jakarta Pusat. Kode induk
  berasal dari data, bukan daftar nama buatan tangan. Negara lain tidak menawarkan pemilih ini bila tidak ada data
  admin-2 — tidak ada selector kosong dan tidak ada klaim kelengkapan palsu; pengalaman admin-1 Natural Earth mereka
  tetap seperti semula. Memilih provinsi menerbangkan kamera ke bbox provinsinya dan menggambar **semua**
  kabupaten/kota di dalamnya sebagai garis orientasi; memilih satu kabupaten/kota memfokuskan kamera ke bbox unit
  itu, mengisi poligonnya dengan warna aksen dan menggarisbawatinya, dengan provinsi dan kabupaten/kota lain tetap
  tampil untuk orientasi. Pin dan HUD Host tidak berubah (klik pin tetap menang).
- **Ketajaman zoom yang ditingkatkan (bounded)**: tekstur detail biasa dibatasi 3072 texel per sisi. Saat sebuah
  provinsi/kabupaten difokuskan, tekstur detail disupersampel sampai ~3840 texel (target `max(3840, 2×cssPx×dpr)`),
  sehingga garis pantai dan batas 110 m benar-benar lebih tajam. Batasnya keras: `MAX_TEXTURE_SIZE` GPU dan
  anggaran memori tetap `DETAIL_BUDGET_MB = 96` (RGBA8 + mipmap ≈ 1,34×sisi² byte, ≈4333 texel maksimum) — bukan
  kanvas seluruh dunia, bukan alokasi per-frame, dan tidak melebihi batas GPU. Sampling per-fragmen, mipmap, DPR
  ≤2, dan zoom maksimum 200x tidak berubah.
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
  menyempitkan lensa (tanpa dolly), jadi perspektif tidak berubah, sampai 200x
  (≈95 km pada panggung 960 px). Konversi posisi: `x = r·cos(lat)·sin(lon)`,
  `y = r·sin(lat)`, `z = r·cos(lat)·cos(lon)`
  (timur di kanan). `project` memproyeksikan pin ke layar dan `unproject`
  memotong sinar kursor dengan bola untuk hover negara.
- **Interaksi**: drag dengan inersia berbasis waktu (1:1 di bawah kursor pada
  setiap zoom), roda untuk zoom yang berjangkar pada kursor (titik di bawah
  kursor tetap diam), *fly-to* halus saat Host dipilih (dari sidebar atau pin),
  putar-otomatis yang berhenti selama sebuah Host dipilih agar pinnya tidak
  hanyut dari HUD dan saat zoom di atas 2x. Pin yang bertumpukan di satu titik
  menampilkan daftar semua Host saat di-hover; mengkliknya melakukan zoom-in
  sampai pin-pin terpisah, dan HUD baru dibuka bila sudah tidak bisa dipisah
  lagi (di zoom maksimum). Filter tipe link, grup, dan pencarian memengaruhi
  apa yang digambar, bukan hanya hitungan.
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
- Bundel admin-1 menambah ~234 KB (gzip), dimuat malas lewat *dynamic import*
  saat zoom melewati 2,5x **atau** saat sebuah negara dipilih; tidak ada
  permintaan ke luar, sifat airgapped tetap terjaga. Indeks `iso` yang kecil
  (ISO alpha-2 → baris) memetakan setiap negara ke garis provinsinya tanpa
  membawa data tambahan per permintaan.
- Bundel admin-2 menambah **~340 KB mentah / ~229 KB gzip** (`globe-admin2.js`), dimuat malas hanya saat Indonesia
  dipilih — bukan di bundel awal. Berisi 38 provinsi + 514 kabupaten/kota bernama dengan poligon nyata; data
  dihasilkan ulang dengan `python3 tools/globe-countries/admin2.py --fetch <dst>` (commit hulu di-pin). Lisensi MIT
  dengan atribusi tertanam di header berkas hasil. Tidak ada panggilan peta/tile pihak ketiga saat runtime.
- Batas negara mengikuti Natural Earth (termasuk wilayah sengketa sebagaimana
  digambarnya); Pantau tidak menetapkan posisi politik.
- Memori GPU ±55 MB saat modal terbuka (peta 4096×2048 ber-mipmap + lapisan
  sorot); lapisan detail menambah sampai ±50 MB saat zoom di atas 2,5x (total
  sampai ~105 MB), keduanya dibebaskan saat modal ditutup.
  Saat provinsi/kabupaten difokuskan, tekstur detail dapat tumbuh sampai ~3840 sisi (≈79 MB dengan mipmap) —
  dibatasi anggaran 96 MB dan `MAX_TEXTURE_SIZE`, tidak pernah alokasi per-frame; total puncak ≈135 MB saat fokus
  admin, dibebaskan saat modal ditutup.
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
  override manual, resolusi ulang host relokasi memakai IP baru (IP lama tidak
  menimpa), `ResolveIP` tidak pernah lookup IP privat/CGNAT/loopback, override
  manual dilewati, entri basi dipertahankan saat offline, validasi koordinat
  respons, micro-spread koordinat sama, agregasi link
  (ssh/proxy/app-TCP/vpn/k8s/cloudflare) dengan store palsu, ETag handler.
- E2E: `e2e/tests/network-globe.e2e.ts` — payload di-stub; modal WebGL terbuka
  dan keadaan kosong tersembunyi; dataset negara terdekode (`countryAt`); piksel
  daratan lebih terang daripada samudra pada kanvas GL; hover menyebut nama
  negara; filter tipe link mengubah jumlah busur yang digambar; HUD Host
  menampilkan detail, nama negara, dan pintasan.
- E2E lanjutan pada berkas yang sama: hover tumpukan pin mencantumkan semua
  Host di titik itu; klik memisahkan pin sampai jarak pasangan terdekat ≥10 px;
  garis provinsi admin-1 dimuat malas dan tergambar (`detailLines > 0`); zoom
  roda berjangkar menjaga titik geografis di bawah kursor tetap dalam 0,1°;
  zoom maksimum 200x.
- E2E seleksi negara pada berkas yang sama: memilih Indonesia dari pemilih
  negara menampilkan **seluruh** 218 garis provinsi admin-1-nya di tekstur detail
  yang dipasang pada bbox penuh (Sumatra–Papua), tanpa zoom manual; pin/HUD Host
  tetap berfungsi setelah seleksi dibatalkan.
- E2E hierarki admin pada berkas yang sama: pemilih provinsi/kabupaten awalnya nonaktif; memilih Indonesia
  memuat malas bundel admin-2 (hanya same-origin, nol permintaan eksternal) dan mengisi **38 provinsi**;
  memilih DKI Jakarta mengisi tepat **6 kabupaten/kota** (1 kabupaten + 5 kota, termasuk Jakarta Pusat) dan
  menerbangkan kamera ke bbox provinsi; memilih Jakarta Pusat memfokuskan bbox unit itu, menandai poligon aslinya
  (`admin.kab.name`), menggambar 6 garis kabupaten/kota untuk orientasi, dan menaikkan sisi tekstur detail di atas
  3072 (dibatasi `MAX_TEXTURE_SIZE`/anggaran) — bukti ketajaman yang teramati pemakai, bukan pernyataan sumber.
  Pin/HUD Host tetap berfungsi saat fokus admin, dan membersihkan negara mengosongkan seluruh hierarki.

### 5. Ketajaman di Semua Zoom & Tier Geometri (2026-11-20)

**Masalah.** Lapisan detail lama membeku saat zoom (mode admin/negara tidak melukis ulang), hanya
menggambar ulang setelah toleransi 1.4x/25% di mode umum, dan tidak pernah saat kamera bergerak.
Kanvasnya persegi dengan batas 3072 texel sehingga pada panggung 1440 px (DPR 2) texel sudah
diperbesar ~1.5x. Garis pantai dan isi daratan berasal dari poligon Natural Earth 1:50m yang
dikuantisasi 0.05° (~5.5 km), jadi pecah saat zoom dekat, dan pulau kecil hilang.

**Keputusan.** Raster detail sekarang berupa **persegi panjang viewport-tight** yang dihitung dari
kamera saat ini (1,25× sisi terbesar panggung sebagai margin). Setiap kali kamera diam (≤300 ms),
detail dilukis ulang bila persegi panjang yang sudah dilukis tidak lagi mencakup tampilan pada
kepadatan 1 texel per piksel perangkat — di **semua** mode (tanpa seleksi, negara, provinsi,
kabupaten). Bila masih bergerak (drag/fly/inersia/roda), satu pass murah diizinkan paling cepat
tiap 300 ms; pass penuh saat diam. Objek tekstur dan kanvas **digunakan kembali** (tanpa alokasi
atau rekreasi per-frame). Ukuran raster = rentang geografis / derajat-per-piksel-perangkat,
dibatasi `MAX_TEXTURE_SIZE` dan anggaran `DETAIL_BUDGET_MB = 96` (RGBA8 + mipmap 4/3);
dengan panggung 1440×900 DPR 2 ini memuncak di **3840 texel** sisi terbesar (~50 MB detail).
Detail mulai aktif pada **zoom 1.3**, lebih awal dari 2.5 lama, tepat saat peta dasar 4096 mulai
diperbesar. Modalitas ini dipilih daripada tile/GL-vektor karena satu kanvas 2D yang di-upload
ulang adalah desain paling konservatif: tidak ada sistem tile, tidak ada triangulasi poligon
cekung di bola, dan antialias + mipmap gratis.

**Tier geometri** (sumber, lisensi, pin, presisi — semua generator stdlib-only dan reproducible):

| Tier | Sumber | Lisensi / pin | Presisi | Dipakai untuk |
| --- | --- | --- | --- | --- |
| Dasar | Natural Earth 1:50m admin-0 (`gen.py`) | domain publik | TOL 0,06°, Q 20 (0,05° ≈ 5,5 km) | bola world zoom, sorotan, hover negara |
| Fine | Natural Earth 1:10m admin-0 (`admin0fine.py`) | domain publik, pin `ca96624a56bd078437bca8184e78163e5039ad19` | TOL 0,001° (~110 m), Q 1000 | daratan + garis pantai + batas luar di luar Indonesia, malas dimuat saat zoom ≥3 |
| Admin-1 | Natural Earth 1:10m admin-1 (`admin1.py`) | domain publik | TOL 0,02°, Q 50 | garis provinsi/state di luar Indonesia |
| Indonesia (kode/nama) | `cahyadsn/wilayah_boundaries` (`admin2.py`) | MIT, Kepmendagri 300.2.2-2430/2025, pin `2c0d8c791af7ca55ee06d17b8f34813e8e6bf1b1` | TOL 0,0005°, Q 10000 | **hanya** kode/nama 38 provinsi + 514 kab/kota + polygon cadangan |
| Indonesia (geometri close-zoom) | geoBoundaries Open IDN ADM2 (`admin2_hires.py`) | **CC BY 3.0 IGO**, BPS/WFP/OCHA ROAP, pin rilis `9469f09`, tahun 2020 | TOL 0,0001° (~11 m), Q 10000; **median segmen 122 m**, p90 406 m | isi daratan, pantai, garis kab/kota, malas per-provinsi |

Di Indonesia, poligon resmi menggantikan garis admin-1 Natural Earth yang bertabrakan (garis NE
untuk ID **ditekan** saat data resmi digambar), sehingga batas provinsi/kabupaten sejajar dengan
pantai dan pulau kecil tetap ada. **LOD otomatis** (tanpa pemilih): data admin-2 dimuat malas saat
pusat pandangan berada di Indonesia pada zoom ≥2,5; garis provinsi muncul pada zoom ≥2,5 dan garis
kabupaten/kota pada zoom ≥12. Pemilih dan sorotan tetap berfungsi; pin tetap menang di atas area.

**Geometri Indonesia resolusi tinggi & topologi (2026-11-20, revisi).** Sumber cahyadsn ternyata
sangat tergeneralisasi (Jakarta Pusat hanya 16 verteks, segmen median ~2,5 km), sehingga pada 600×
kota tampak sebagai poligon 16 sisi dan retakan gelap muncul antar-tetangga. Ia kini dipakai **hanya**
untuk kode/nama resmi 38+514 dan polygon cadangan. Geometri close-zoom berasal dari **geoBoundaries
Open IDN ADM2** (BPS/WFP/OCHA ROAP, **CC BY 3.0 IGO**, rilis pin `9469f09`, tahun 2020, 519 unit).
Atribusi tertanam di header setiap chunk dan di sini: **© BPS / WFP / OCHA ROAP, geoBoundaries,
CC BY 3.0 IGO**. Data 2020 didistribusikan sebagai TopoJSON ber-**arc bersama**; generator
`tools/globe-countries/admin2_hires.py` menyederhanakan **setiap arc satu kali** (TOL 0,0001°,
snap 1/10000°) lalu menyusun poligon dari arc-arc itu — sehingga tetangga berbagi verteks identik,
**tanpa retakan/overlap** (bukan TopoJSON → GeoJSON per fitur yang memecah topologi). Kode/nama
Kepmendagri-2025 dipertahankan: 514/514 kab/kota dipetakan ke 519 unit 2020 lewat normalisasi nama plus
tabel alias terverifikasi; **1 unit tanpa padanan** (Kepulauan Tanimbar, hasil pecahan pasca-2020)
menggunakan polygon cadangan dari bundle lama. Pencocokan membedakan kota vs kabupaten (indeks
`kota_by_name` terpisah) — tanpa ini "Kota Bandung" dan "Kabupaten Bandung" salah menjadi geometri yang
sama. Generator **mempertahankan struktur cincin TopoJSON** (Polygon/MultiPolygon → `[ring,...]`, tiap
ring → arc refs); dekoder membangun satu loop tertutup per ring, sehingga pulau, eksklave dan lubang tidak
berubah menjadi chord atau even-odd fill yang salah (269 dari 513 fitur sebelumnya terdampak). Presisi
terukur: **median segmen 122 m, p90 406 m** (target ≤150 m terpenuhi). Chunk per-provinsi: **38 berkas,
~2,26 MB mentah / ~1,5 MB gzip total** (target ≤8 MB); hanya provinsi yang bbox-nya beririsan dengan raster
yang diimpor malas lewat `/app/globe-admin2-hires/<kode>.js` (handler `serveGlobeHiresChunk`, gzip + ETag
in-memory). Sebelum sebuah chunk selesai, provinsi itu diisi dari poligon cadangan lengkap sehingga
daratan tidak pernah tampak sebagai laut. Isian garis sorotan dan outline provinsi terpilih juga memakai
geometri halus (bukan poligon 1:50m), menghilangkan garis lurus kasar di atas laut.

**Tint sorotan.** Pada zoom dekat, isian sorotan negara/host diambil dari geometri halus (poligon
Indonesia resmi atau tier 1:10m), **bukan** poligon negara 1:50m — yang terakhir menciptakan pita
cahaya lurus di atas laut (mis. utara Jawa). E2E memeriksa piksel laut tetap berwarna samudra saat
Jawa Barat dipilih.

**Cakupan raster di semua mode.** Persegi panjang raster selalu **menutupi viewport penuh** pada
setiap mode (tanpa seleksi, negara, provinsi, kabupaten). Bbox seleksi hanya menggerakkan kamera
(`fly-to`) dan menentukan sorotan; ia **tidak pernah memotong raster** — memotongnya membuat sisi
panggung menampilkan peta dasar yang diperbesar (blur ungu muda) dengan jahitan yang terlihat.
Rentang lintang dihitung dari `ch` dan bujur dari `cw` (keduanya CSS px), dengan margin 1,25×.
Repaint terjadi bila cakupan, kepadatan, LOD, mode, ukuran, atau kesiapan data berubah, atau bila
pusat pandangan bergeser lebih dari ¼ raster (lompatan pan besar); drift putar-otomatis kecil tidak
memicu repaint.

**Supresi tier kasar & clipping.** Begitu tier fine 1:10m (di luar Indonesia) atau poligon resmi
admin-2 (di Indonesia) siap pada zoom itu, poligon negara 1:50m **tidak lagi diisi atau digaris**
di atasnya — garis lurus "berlian" dari kuantisasi 0,05° akan menimpa pantai yang sudah halus.
Garis pantai/batas berasal dari tier halus itu sendiri (`coarseTraced = 0` dapat diverifikasi).
Garis admin-1 Natural Earth yang masih 0,02° (~2,2 km) **di-clip ke poligon daratan fine** memakai
`CanvasRenderingContext2D.clip('evenodd')` sebelum digores, supaya tidak memotong pantai halus
keluar ke laut. Alternatif tier admin-1 yang lebih halus ditolak karena menggembungkan bundel malas
di luar anggaran; clipping sudah cukup dan gratis.

**Framing seleksi.** `flyToBbox` memakai half-extent asli fitur (bukan lantai 0,25° lama) dan
`zoomForRegion` memiliki guard 0,0005°, sehingga kabupaten/kota kecil seperti Kota Sibolga
(0,042° × 0,037°) terbingkai ≥35% tinggi panggung atau mencapai plafon 600× bila fit-nya
melebihi. Fly-to tetap di-clamp ke 600×.

**Satu batas zoom** = **600×** untuk roda, tombol, dan fly-to/seleksi (sebelumnya fly-to tidak
di-clamp dan bisa melampaui 200×). Batas ini dipilih dari data: kota terkecil, **Kota Sibolga**
(bbox 0,042° × 0,037°), pada panggung 1080×720 memerlukan `zoomForRegion` ≈**79×** untuk terbingkai
penuh; karena itu jauh di bawah plafon, batas 600× dipilih sebagai plafon seragam. Pada 600× Sibolga
menempati ≈168×148 piksel CSS — terbaca jelas. Presisi tier admin-2 (TOL 0,0005°) memberi galat
≈0,0005° × 4011 px/° ≈**2 px CSS** pada 600× (sedikit di atas target 1,5 px; ini batas data mentah
yang tersedia). Tier fine (TOL 0,001°) memberi galat ≈**3,9 px CSS** pada panggung ~700 px di 600×;
galat ini adalah batas resolusi tier global di luar Indonesia, dan diterima dengan jujur karena
mendapatkan presisi lebih baik memerlukan bundel jauh lebih besar di luar anggaran malas.

**Anggaran & ukuran (memori GPU aktual, RGBA8 + mipmap 4/3).** Peta dasar 4096×2048 =
8.388.608 texel × 4 B × 4/3 ≈**42,7 MB**; lapisan sorot 2048×1024 ≈**10,7 MB**; detail memuncak
3840² texel (dibatasi anggaran 96 MB) ≈**78,6 MB**, tetapi pada panggung uji (~1080×720, DPR 2)
raster aktual ≈2160×1440 ≈**16,6 MB**. Total puncak pada panggung uji ≈**70 MB**; batas teoretis
dengan detail penuh ≈132 MB, masih di bawah anggaran lama ~135 MB. Semua dibebaskan saat modal
ditutup. Bundel awal globe tidak berubah: tier fine (~1,32 MB mentah / ~910 KB gzip) dan admin-2
(~688 KB / ~483 KB gzip) keduanya malas. Bundel admin-2 diperbarui dari 0,05°/0,003° menjadi
0,0001°/0,0005°.

**Waktu repaint terukur:** dekode + bbox-prefilter 514 kabupaten/kota untuk tampilan se-Jawa
(bbox 105,2–114,6 BT, 9,1–5,9 LS; 349 fitur, 8.926 verteks) ≈29 ms pada Node; dekode penuh
98.459 verteks ≈12 ms. Repaint terjadi hanya saat cakupan, kepadatan, LOD, mode, atau ukuran
benar-benar berubah — bukan per-frame, dan tidak pada perubahan pusat kecil yang masih tercakup.
**Verifikasi tambahan** di `e2e/tests/network-globe.e2e.ts`: kepadatan texel per piksel perangkat
≥0,99 setelah diam pada zoom 3, 20, dan 600 (mode umum), gambar provinsi/kabupaten otomatis
tanpa pemilih, hover menyebut Jakarta Pusat + Jakarta, fly-to kota terkecil tidak melebihi 600×,
detail dilukis ulang saat zoom dengan negara terpilih, nol permintaan pihak ketiga, plus empat
screenshot (Indonesia, Jawa Barat, Jakarta Pusat maks, Singapura maks). Kontrak mode negara
Indonesia diperbarui: 38 provinsi resmi + 514 kab/kota menggantikan 218 garis NE, dengan
`d.lines` kosong (garis NE ID ditekan).

### 6. "Whole world" mengembalikan kamera ke tampilan dunia (2026-11-20)

**Masalah.** Memilih opsi **Whole world** pada pemilih negara hanya mengosongkan hierarki
(negara/provinsi/kabupaten) tanpa mengembalikan kamera. Setelah fly-to ke sebuah negara/provinsi/kota,
label pemilih berbunyi "Whole world" sementara kanvas masih menampilkan tambalan daratan/pantai datar
dari zoom dalam — gambar tidak sesuai dengan pilihan.

**Keputusan.** `GL.selectCountry(-1)` kini memanggil `GL.flyHome()` (primitif kamera baru di
`05-globe-interact.js`) yang menolak kecepatan inersia dan menerbangkan kamera ke HOME
(100 BT/15 LU, zoom 1). Tombol reset memakai jalur yang sama tanpa rekursi. Zoom roda/geser manual
tampa seleksi sengaja tidak diubah: hanya pemilihan eksplisit "Whole world" yang mereset kamera.
Pilih negara/provinsi/kabupaten tetap melakukan fly-to seperti biasa; pin dan HUD Host tidak berubah.

**Verifikasi** di `e2e/tests/network-globe.e2e.ts`: setelah memilih Indonesia → DKI Jakarta →
Jakarta Pusat (zoom dalam), memilih "Whole world" mengosongkan ketiga pemilih, mengembalikan zoom
ke ~1 dan pusat ke HOME, serta kanvas kembali menampilkan bola bulat (disc buram di tengah, sudut
transparan) — dibuktikan dengan pembacaan piksel WebGL nyata dan screenshot. Tombol reset, zoom manual,
dan fly-to negara tetap diverifikasi.
