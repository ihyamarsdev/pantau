# 0090. Ringkasan Docker Armada (Fleet Docker Overview)

Menambahkan agregasi Docker lintas-Host yang ringan ke bagian Docker global: endpoint `GET /api/docker/overview` dan strip ringkasan di atas pemilih Host yang sudah ada. Data dibaca dari telemetri Docker yang sudah tersimpan (`host_containers`, `docker_stacks`, `docker_security_scans`) dalam satu lintasan SQLite — tanpa SSH, tanpa jaringan, tanpa data baru. Pemilih Host dan alur manajemen per-Host tetap dipertahankan persis seperti sebelumnya.

## Konteks dan Masalah

1. ADR 0043 keputusan 2 menetapkan bagian Docker global sebagai *pemilih Host yang dapat difilter* dan melarang duplikasi UI Docker. Konsekuensinya: tidak ada pintu masuk agregat — administrator harus membuka tab Docker setiap Host satu per satu untuk mengetahui kondisi Docker armada.
2. Data Docker yang sudah dikumpulkan (`host_containers` dari inspeksi berkala, `docker_stacks`, `docker_security_scans`) di-key per `host_id` dan hanya dibaca lewat endpoint `/api/hosts/{id}/docker/*` atau pencarian fleet generik `/api/search`. Tidak ada endpoint agregat (`/api/docker/overview` sebelumnya tidak ada).
3. Kartu Host di pemilih Docker hanya menampilkan kolom generik dari `hosts` (nama, alamat, `os_info`, status) — tidak ada indikator "punya Docker", jumlah container, atau hasil audit.
4. Mengambil data Docker live dari semua Host lewat SSH akan mahal (N koneksi) dan melanggar pola on-demand ADR 0042. Telemetri tersimpan sudah cukup untuk ringkasan yang jujur.

## Keputusan Arsitektur

1. **Endpoint agregat** `GET /api/docker/overview` (rute di `internal/web/server.go`, handler `handleDockerOverview` di `internal/web/handlers_docker_overview.go`). Menerima `?host_id=` dan/atau `?group_id=`; `host_id` tidak valid → HTTP 400.
2. **Logika di `internal/store/docker_overview.go`**, fungsi `GetDockerOverview(hostID, groupName)`. Satu lintasan SQLite atas tabel yang sudah ada:
   - `host_containers` → jumlah container, running/stopped, `seen_at` terbaru per Host;
   - `docker_stacks` → jumlah Compose project per Host;
   - `docker_security_scans` → `critical+high` misconfig + image vulns, dan status audit terakhir.
   - Respons: `host_count`, `hosts_with_docker`, `hosts_without_docker`, `total_containers`, `running_containers`, `stopped_containers`, `total_stacks`, `total_security_issues`, `hosts[]` (ringkasan per Host, diurutkan: yang punya Docker dulu, lalu jumlah isu, lalu nama), `generated_at`.
   - Host tanpa baris Docker apa pun dilaporkan sebagai `hosts_without_docker`, **bukan** "0 container = aman" (prinsip *honest coverage* ADR 0036/0069).
3. **UI**: strip ringkasan `#dockerFleetStrip` ditambahkan di atas `#dockerHostsGrid` pada `sectionDocker`. `loadDockerFleetOverview()` (di `08-metrics-systemd-docker.js`) dipanggil dari `switchSideNav('docker')` dan `updateSideNavData()`, memuat endpoint dan merender 6 stat (Host dengan Docker, Container, Running, Stopped, Stack, Security issues). Pemilih Host, pencarian, dan tombol "Manage Docker" tidak berubah — pengguna tetap masuk ke tab Docker Host Detail seperti ADR 0043.
4. **Airgapped & token**: tanpa pustaka eksternal (ADR 0013), hanya design token (ADR 0039), i18n EN/ID (ADR 0009) lewat kunci `dk_fleet_*` di `00-state.js`. Tidak ada skema baru, tidak ada migrasi, tidak ikut System Snapshot (telemetri turunan).

## Hubungan dengan ADR lain

- **Merevisi ADR 0043 keputusan 2**: bagian Docker global bukan lagi *hanya* pemilih Host, melainkan pemilih Host **dengan ringkasan agregat di atasnya**. Alasan: data telemetri tersimpan kini bisa dibaca tanpa SSH, sehingga biaya agregasi hampir nol. UI manajemen Docker itu sendiri tetap tidak disalin — keputusan "tidak ada kode Docker baru" masih dipegang untuk tab per-Host.
- Selaras ADR 0042 (per-Host live SSH tetap on-demand) dan ADR 0069 (audit/image scan tetap per-Host; ringkasan hanya membaca hasil tersimpan).

## Konsekuensi

- Administrator melihat postur Docker armada sekilas tanpa membuka Host satu per satu, dengan biaya satu query SQLite.
- Ringkasan seakurat data tersimpan terakhir: Host yang belum pernah diinspeksi Docker ditandai "tanpa data", bukan "kosong/aman". Audit/image scan hanya muncul setelah dijalankan.
- Tidak ada koneksi SSH baru dari halaman global; tidak ada dependensi atau perubahan skema.

## Verifikasi

- Unit test Go `internal/store/docker_overview_test.go`: agregasi lintas Host (container running/stopped, stack, isu security, urutan, hosts_without_docker) dan filter host/group.
- Handler test: autentikasi, `host_id` invalid → 400, bentuk respons lengkap.
- E2E `e2e/tests/docker-fleet.e2e.ts`: strip ringkasan merender angka dari endpoint yang di-stub dan pemilih Host tetap berfungsi.
