# 0083. Live Fleet Topology Map — Hybrid CSS-Grid + Minimal SVG Overlay

Peta topologi fleet yang **ringan**: kartu HTML di dalam Workspace Modal ([ADR 0007](0007-standardized-workspace-modal-layout.md)) ditata dengan CSS Grid 3 kolom, dan satu lapisan `<svg>` `pointer-events:none` menggambar konektor Bezier antar kartu. Tanpa pustaka graf, tanpa physics loop, tanpa rendering SVG kanvas penuh, tanpa SSH/API vendor saat memuat. Selaras dengan [ADR 0013](0013-airgapped-self-contained-web-assets.md) dan [ADR 0039](0039-modern-visual-design-system-and-icon-sprite.md).

> Catatan: nomor 0083 diminta eksplisit. ADR lama `0083-workspace-modal-maximize-per-host-tab-state-and-terminal-from-path.md` tetap ada sebagai dokumen terpisah; ADR pendahulu `0084-live-fleet-topology-svg-map.md` (graf SVG berat) dicabut dan digantikan oleh keputusan ringan ini.

## Konteks dan Masalah

1. **Graf SVG penuh mahal**. Pendekatan sebelumnya merender seluruh node/edge sebagai elemen SVG dengan auto-layout + dragging + animasi aliran pada setiap edge. Untuk fleet besar ini berarti ratusan elemen SVG, reflow saat drag, dan animasi yang berjalan terus — bertentangan dengan tujuan peta sebagai ikhtisar.
2. **Detail workload tidak perlu global**. Container dan listening port berubah setiap inspeksi; memuat semuanya ke graf global membuat payload besar dan peta penuh sesak. Detail hanya dibutuhkan saat operator mengklik satu host.
3. **SSE seharusnya tidak membangun ulang DOM**. [ADR 0030](0030-in-app-activity-console-and-live-event-stream.md) sudah menyediakan stream peristiwa; membangun ulang seluruh graf pada setiap event membuang kerja dan mengganggu interaksi.

## Keputusan Arsitektur

### 1. API makro dengan cache TTL (`internal/topology`, `GET /api/topology`)

- Antarmuka `topology.Store` sempit (5 metode pembaca). `topology.Build` menghasilkan **hanya node makro**:
  - **Tier 1 (Edge/Network)**: Pantau Gateway, VPN Networks ([ADR 0077](0077-jalur-jaringan-per-host-driver-vpn-dan-dial-terikat-interface.md)), Cloudflare Tunnels/Zones (`internal/cloudflare/`).
  - **Tier 2 (Compute)**: Host/VPS dengan `group`, `status`, `alerts`, dan ringkasan `cpu_pct`/`ram_pct`.
  - **Tier 3 (Clusters)**: Kubernetes Cluster (`internal/inspector/kube.go`) yang dihubungkan ke host anggota lewat `kube_cluster_id`.
- **Edge hanya antar tier makro**: `gateway→vpn`, `vpn→host` (bound dial, bawa `latency_ms`/`hops` dari probe tersimpan), `gateway→direct→host`, `gateway→cf` (tunnel), `host→kube` (cluster). Tidak ada node container/port/domain di graf global.
- **Cache TTL 20 detik** (`topology.Cache`) melindungi SQLite dari join berulang. Cache di-invalidate secara eksplisit pada write konfigurasi: host create/update/delete/import, VPN create/update/delete/up/down. Status inspeksi juga masuk lewat SSE (lihat bawah), jadi tidak perlu menunggu TTL untuk perubahan status.
- **Detail malas**: `GET /api/topology/host?id=N` mengembalikan container (`host_containers`) + listening ports (`hosts.listening_ports`) **hanya saat kartu host diklik**. `topology.ParseListening` mengubah JSON tersimpan; tidak ada query tambahan di graf global.

### 2. Tata letak hybrid (vanilla HTML/CSS/JS)

- Modal Workspace berisi tiga kolom CSS Grid: `Edge / Network` → `Compute Hosts` → `Clusters`. Tiap node adalah kartu HTML biasa memakai token ADR 0039 (`--surface-2`, `--card-border`, `--success`/`--warning`/`--danger`, `--radius`, `--fs-*`).
- **Satu `<svg id="tpLines">`** dengan `pointer-events:none` menggambar `<path>` Bezier dari tepi kanan kartu sumber ke tepi kiri kartu target. Koordinat dihitung dari `getBoundingClientRect()` relatif terhadap kontainer peta. Dihitung ulang **hanya** saat `load`, filter change, window resize, atau scroll peta — memakai debounce 60 ms. **Tidak ada dragging, tidak ada physics loop, tidak ada `requestAnimationFrame` berulang.**
- Filter sisi klien: host group, network/VPN, dan health status; pencarian nama/group menyaring kartu (bukan mengubah data).
- **Animasi selektif**: jalur sehat statis. Pulse/flow hanya pada node/edge yang memiliki alert aktif, dan pada `:hover`/`:focus-visible`. `prefers-reduced-motion` mematikan semuanya. Ini mengikuti aturan warna status CONTEXT.md (hijau tenang, kuning berdenyut, merah cepat, abu-abu diam).

### 3. SSE tanpa re-render (ADR 0030)

- `EventSource('/api/logs/stream')` dibuka saat modal terbuka, ditutup lewat `Topology.onClose()` di `closeModal` (sumber yang sama dengan Activity Console, bukan koneksi baru).
- Event dengan `host` + `attrs.status` melakukan **update bedah**: `card.dataset.status = ...`, ganti kelas `tp-status-*`, dan teks label status pada kartu yang cocok. Tidak ada `innerHTML` rebuild, tidak ada refetch penuh.
- Event inspeksi/drift/alert/network-down memicu `load()` debounce (1,2 s) — cache TTL membuat ini murah. Event lain hanya memberi flash singkat pada kartu terkait.
- Klik kartu membuka drawer ringkas dengan pintasan Terminal (`openHostTerminal`), Activity Logs (`toggleActivityConsole`), dan Host Workspace (`openHostDetail`), plus detail layanan malas dari `/api/topology/host?id=N`.

## Konsekuensi

- Payload graf kecil dan stabil; jumlah elemen DOM sebanding dengan jumlah node makro, bukan jumlah workload.
- Tidak ada biaya CPU saat idle (tanpa loop animasi global); connector dihitung hanya saat layout benar-benar berubah.
- Status host terasa langsung lewat SSE, sementara perubahan struktural (host/network baru) muncul setelah invalidate atau maksimal TTL 20 detik.
- Cloudflare/Kubernetes sengaja `unknown` sampai ada sinyal kesehatan lokal — tidak mengarang status.
- Detail container/port tidak pernah masuk graf global; selalu segar dari endpoint malas saat diklik.

## Pengujian

- Unit Go: `internal/topology/builder_test.go` (node makro only, tier, jalur VPN, metrik, cluster link, fail-closed, cache hit/expire/invalidate, `ParseListening`) dan `internal/web/handlers_topology_test.go` (auth, 405, bentuk graf dari DB, cache invalidate, endpoint detail malas, validasi id).
- E2E: `e2e/tests/topology.e2e.ts` — API makro + cache, UI grid/SVG overlay, filter, dan drawer detail malas.
