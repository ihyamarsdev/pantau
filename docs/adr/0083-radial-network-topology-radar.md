# 0083. Concentric Radial Network Radar — single-SVG deterministic topology

Peta topologi fleet dirender sebagai **satu `<svg viewBox="0 0 1000 700">` mandiri** di dalam Workspace Modal ([ADR 0007](0007-standardized-workspace-modal-layout.md)): satu hub di tengah, dua cincin konsentris, dan edge radial melengkung. Posisi node dihitung dengan polar-ke-kartesius sederhana (`Math.cos`/`Math.sin`) dari `sector`/`sectors` yang dikirim API — **tanpa physics loop, tanpa `getBoundingClientRect()` alignment, tanpa HTML grid, tanpa pustaka eksternal**. Selaras dengan [ADR 0013](0013-airgapped-self-contained-web-assets.md) dan [ADR 0039](0039-modern-visual-design-system-and-icon-sprite.md).

> Dokumen ini menggantikan pendekatan hybrid CSS-grid + SVG overlay pada `0083-lightweight-fleet-topology-map.md`. API makro, cache TTL, dan endpoint detail malas dipertahankan apa adanya; yang berubah hanya tata letak dan interaksi frontend.

## Konteks dan Masalah

1. **Overlay hybrid bergantung pada kotak layout HTML.** Kartu CSS Grid harus diukur (`getBoundingClientRect`) lalu konektor SVG digambar ulang pada `load`, filter, resize, dan scroll. Ini adalah box-alignment yang rapuh: scroll-sync dan debounce diperlukan agar garis tidak meleset.
2. **Peta butuh geometri yang stabil.** Operator membaca topologi sebagai hub-and-spoke: Gateway di tengah, jaringan di cincin dalam, compute di cincin luar. Grid tiga kolom tidak mengekspresikan kedekatan jaringan secara visual.
3. **Biaya idle harus nol.** Animasi aliran pada semua edge dan physics loop membuat CPU sibuk terus-menerus, bertentangan dengan tujuan peta sebagai ikhtisar statis.

## Keputusan Arsitektur

### 1. API makro dengan cache TTL (`internal/topology`, `GET /api/topology`)

Tidak berubah dari ADR pendahulu: `topology.Store` (5 metode pembaca), `topology.Build` menghasilkan node makro (Gateway, VPN Network [ADR 0077](0077-jalur-jaringan-per-host-driver-vpn-dan-dial-terikat-interface.md), Cloudflare Tunnel/Zone, Host/VPS, Kubernetes Cluster), edge antar tier, health status, ringkasan `cpu_pct`/`ram_pct`, dan jumlah alert. Cache TTL 20 detik (`topologyCacheTTL`) melindungi SQLite; config write memanggil `invalidateTopology()`. `GET /api/topology/host?id=N` memuat container + listening port secara malas saat node diklik.

**Tambahan:** `Node` kini membawa `ring` (0 hub, 1 inner, 2 outer), `sector`, dan `sectors`. `assignSectors` di `Build` menetapkan slot sudut yang deterministik: hub tunggal di ring 0; VPN/Cloudflare di ring 1 diurutkan by kind lalu id; Host/Cluster di ring 2 diurutkan **by group lalu id** sehingga anggota Host Group yang sama menempati sektor bersebelahan. Frontend hanya membaca tiga angka ini — semua keputusan layout ada di backend, sehingga dua build atas inventori yang sama selalu menghasilkan geometri identik.

### 2. Tata letak konsentris deterministik (vanilla JS/CSS)

- Modal Workspace memuat `<svg id="tpRadar" viewBox="0 0 1000 700">` yang mengisi canvas. Empat `<circle>` range ring + crosshair menjadi latar radar; label cincin memakai token `--text-faint`.
- `nodePos(n)` = `CENTER + radius(ring) * (cos θ, sin θ)` dengan `θ = -π/2 + ((sector + 0.5)/sectors) * 2π`. Radius: inner 170, outer 292, center `(500, 350)`. **Tidak ada loop iteratif, tidak ada pengukuran DOM, tidak ada scroll listener untuk geometri.**
- Edge radial berupa `<path d="M … Q …">` dengan satu titik kontrol yang dibengkokkan menjauh dari pusat. Edge sehat statis dan subtle; hanya edge yang tersambung ke node ber-alert yang memakai `stroke-dasharray` + animasi `tpFlow` (dimatikan oleh `prefers-reduced-motion`).
- Warna status mengikuti CONTEXT.md: hijau tenang (`--success`), kuning berdenyut (`--warning`), merah cepat (`--danger`), abu-abu diam (`--text-muted`). Pulse cincin (`tpRadarPulse`) hanya pada node hover/focus atau node dengan `alerts > 0` — bukan animasi global.

### 3. Zoom/pan pada viewBox, filter pill di header

- Zoom (tombol `+`/`−`, wheel) dan pan (pointer drag) mengubah **atribut `viewBox`** saja — bukan transform DOM, bukan scroll container. `zoom()` mempertahankan titik kursor dengan memetakan koordinat klien lewat `getScreenCTM().inverse()`.
- Header memuat pill filter grup (`tpPills`) yang dibangun dari `group` node; pencarian nama/grup menyaring node tanpa mengubah data. Keduanya memanggil `render()` penuh yang murah (jumlah elemen ≈ jumlah node makro).

### 4. SSE tanpa re-render + HUD mengambang (ADR 0030)

- `EventSource('/api/logs/stream')` dibuka saat modal terbuka, ditutup via `Topology.onClose()` di `closeModal`.
- Event dengan `host` + `attrs.status` melakukan update bedah: `dataset.status` dan kelas `tp-status-*` pada `<g class="tp-node">` yang cocok — **tanpa `innerHTML` rebuild**. Event inspeksi/drift/alert/network-down memicu `load()` debounce 1,2 s (cache TTL membuatnya murah).
- Klik node menampilkan HUD card di pojok kanan canvas: status, CPU/RAM, jumlah alert, ringkasan group/network, detail layanan malas (`/api/topology/host?id=N` — container + listening port/reverse proxy), dan tombol langsung Terminal (`openHostTerminal`), Activity Logs (`toggleActivityConsole`), Host Workspace (`openHostDetail`). Tombol dinonaktifkan untuk node non-Host.
- **Animasi berjalan terus**: ikon setiap node bernapas (`tpIconBreathe`) tanpa perlu hover, dan rute ke Gateway disorot bergilir lewat satu timer `setInterval` (2,6 s) yang memilih node outer secara deterministik — bukan `requestAnimationFrame`. Hover/fokus/klik mengambil alih dan menjeda cycle; saat pointer keluar dan tidak ada seleksi, cycle lanjut. Timer dihentikan di `Topology.onClose()`. `prefers-reduced-motion` mematikan semua animasi.
## Konsekuensi

- Geometri peta sepenuhnya deterministik dan dapat diuji di Go (`TestBuildRadialRingsAndSectors`) maupun di browser (posisi `<g>` stabil antar render).
- Tidak ada biaya CPU saat idle: tanpa physics loop, tanpa animasi edge sehat, tanpa perhitungan layout pada scroll/resize.
- Perubahan struktural (host/network baru) muncul setelah invalidate atau maksimal TTL 20 detik; perubahan status host terasa langsung lewat SSE.
- Cloudflare/Kubernetes sengaja `unknown` sampai ada sinyal kesehatan lokal — tidak mengarang status.
- Detail container/port tidak pernah masuk graf global; selalu segar dari endpoint malas saat diklik.

## Pengujian

- Unit Go: `internal/topology/builder_test.go` — node makro only, tier, jalur VPN, metrik, cluster link, fail-closed, cache hit/expire/invalidate, `ParseListening`, dan `TestBuildRadialRingsAndSectors` (hub tunggal, inner/outer ring, sektor unik, group bersebelahan, determinisme). `internal/web/handlers_topology_test.go` — auth, 405, bentuk graf dari DB, cache invalidate, endpoint detail malas, validasi id.
- E2E: `e2e/tests/topology.e2e.ts` — API makro + cache, render `<svg viewBox>` dengan node `<g>` pada posisi polar, filter pill, zoom/pan viewBox, SSE status update, dan HUD detail malas.
