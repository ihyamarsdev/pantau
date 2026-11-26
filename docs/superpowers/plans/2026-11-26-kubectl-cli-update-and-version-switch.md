# `kubectl` CLI Update & Version Switch Implementation Plan

**Goal:** Menambahkan fitur pemeriksaan pembaruan (*update check*), pembaruan otomatis ke rilis terbaru, serta pergantian/pinning versi (`upgrade`, `downgrade`, dan *revert* ke biner sistem) untuk biner `kubectl` lokal yang dikelola Pantau, mengikuti pola **Gateway CLI Update** (`internal/gwupdate`, ADR 0080 & ADR 0081).

**Architecture:** Memperluas paket `internal/gwupdate` dengan target `TargetKubectl = "kubectl"` yang mengunduh biner resmi `kubectl` dari `dl.k8s.io`, memverifikasi hash SHA-256 dari `kubectl.sha256` upstream, mengujinya secara offline dengan `kubectl version --client`, menyimpannya per versi di `<data>/cli/versions/kubectl-<versi>`, dan mengaktifkan versi terpilih secara atomik melalui symlink `<data>/cli/bin/kubectl`. Lapisan Eksekusi External Cluster di `internal/inspector/kube_cluster.go` membaca biner melalui `gwupdate.ResolveBinary("kubectl")` dan `gwupdate.PrependPath(...)` sehingga pergantian versi langsung berlaku tanpa restart server dan tanpa menyentuh `/usr/bin/kubectl`.

**Tech Stack:** Go `1.26.8` (stdlib only di `internal/gwupdate`), `internal/inspector`, `internal/web` (REST + NDJSON stream), Vanilla JS + CSS (embedded assets, i18n EN|ID).

**Related Specs & ADRs:**
- [ADR 0047](file:///home/ihya/project/pantau/docs/adr/0047-external-kubernetes-clusters-from-kubeconfig.md) (External Kubernetes Clusters via `kubectl` lokal)
- [ADR 0080](file:///home/ihya/project/pantau/docs/adr/0080-gateway-cli-update-check.md) (Gateway CLI Update Check)
- [ADR 0081](file:///home/ihya/project/pantau/docs/adr/0081-gateway-cli-update.md) (Gateway CLI Update)
- [docker-kubernetes-repos.md](file:///home/ihya/project/pantau/docs/agents/context/docker-kubernetes-repos.md) & [automation-ansible-terraform.md](file:///home/ihya/project/pantau/docs/agents/context/automation-ansible-terraform.md)

---

## Cakupan & Batasan Desain (Jujur Secara Operasional)

1. **Berlaku untuk `kubectl` Lokal di Server Pantau (External Cluster):**
   - Versi `kubectl` yang dikelola Pantau dipakai oleh seluruh operasi **External Cluster** (EKS, GKE, AKS, atau kubeconfig mandiri) serta proses parsing/validasi kubeconfig (`ListKubeconfigContexts`, `PrepareKubeconfig`, `localKubeRunner`).
   - **Tidak mengubah `k3s`/`rke2` di Host SSH:** Untuk **Kubernetes Cluster Host** ([ADR 0043](file:///home/ihya/project/pantau/docs/adr/0043-side-panel-and-kubernetes-k3s-rke2-management.md)), Pantau tetap mengeksekusi `k3s kubectl` atau `/var/lib/rancher/rke2/bin/kubectl` yang terpasang di Host remote agar selalu cocok dengan versi control-plane Host tersebut. UI harus menyatakan cakupan ini secara eksplisit.
2. **Alasan Praktis Ganti Versi (*Kubernetes Version Skew Policy*):**
   - Kubernetes mendukung perbedaan versi `kubectl` maksimal `±1` minor version terhadap `kube-apiserver`. Operator yang mengelola klaster lama (mis. EKS `1.29`) sekaligus klaster baru (`1.32`) perlu bisa melakukan *downgrade* / *pin* ke `1.30.x` atau berpindah cepat antar versi `kubectl` yang sudah terpasang.
3. **Tanpa Menyentuh Biner Sistem (`/usr/bin/kubectl`):**
   - Mengikuti [ADR 0081](file:///home/ihya/project/pantau/docs/adr/0081-gateway-cli-update.md), Pantau tidak pernah menimpa biner sistem milik `apk`/`apt` (tidak butuh `root`/`sudo`). Operator selalu dapat melakukan *revert* ke `kubectl` bawaan sistem.
4. **Kejujuran Jaringan & Verifikasi Wajib:**
   - Kegagalan mengakses upstream `dl.k8s.io` (mis. lingkungan airgapped) dilaporkan sebagai `status: "error"` (*Check failed*), bukan *up to date* (ADR 0080).
   - Setiap unduhan biner wajib lolos verifikasi SHA-256 terhadap `kubectl.sha256` upstream dan lolos uji jalan (`kubectl version --client`) sebelum symlink aktif dipindahkan (ADR 0081).
   - Versi yang diunduh wajib berupa nomor rilis valid (`ParseVersion`, mis. `1.31.4` atau `v1.31.4`); string bebas seperti `latest`, path traversal, atau karakter non-rilis ditolak sebelum permintaan HTTP dibuat.

---

## Layout Penyimpanan Managed `kubectl`

Memperluas struktur `<data>/cli/` milik `gwupdate.ManagedDir`:

```text
<data>/cli/
├── bin/
│   ├── terraform                       (existing)
│   ├── tofu                            (existing)
│   └── kubectl -> /<data>/cli/versions/kubectl-1.31.4   (symlink atomik ke versi aktif, target absolut)
├── versions/
│   ├── kubectl-1.30.8                  (biner tersimpan lokal, chmod 0755)
│   └── kubectl-1.31.4                  (biner tersimpan lokal, chmod 0755)
├── venv -> venvs/ansible-2.22.0        (existing)
└── venvs/                              (existing)
```

- **Switch instan & offline:** Jika `versions/kubectl-<versi>` sudah ada dan lolos smoke test, perpindahan versi cukup memindahkan symlink `<data>/cli/bin/kubectl` secara atomik (`pointSymlink`) tanpa mengunduh ulang.
- **Revert ke sistem:** Menghapus symlink `<data>/cli/bin/kubectl` membuat `gwupdate.ResolveBinary("kubectl")` otomatis jatuh kembali ke `exec.LookPath("kubectl")` (salinan sistem di `/usr/bin/kubectl`).

---

## Sumber Upstream Resmi `kubectl`

| Kebutuhan | Endpoint | Ukuran & Perilaku |
| --- | --- | --- |
| Versi stabil terbaru | `https://dl.k8s.io/release/stable.txt` | Plain text (~10 byte, contoh `v1.32.1`), timeout 15s |
| Versi terbaru per kanal minor *(opsional untuk saran versi)* | `https://dl.k8s.io/release/stable-<major>.<minor>.txt` | Plain text (~10 byte), contoh `stable-1.31.txt` -> `v1.31.5` |
| Biner `kubectl` | `https://dl.k8s.io/release/v<version>/bin/<os>/<arch>/kubectl` | Single executable binary (~50–65 MiB, di bawah `downloadCap` 200 MiB) |
| Manifest checksum SHA-256 | `https://dl.k8s.io/release/v<version>/bin/<os>/<arch>/kubectl.sha256` | Plain text 64-char hex SHA-256 |
| URL catatan rilis | `https://github.com/kubernetes/kubernetes/releases/tag/v<version>` | Ditampilkan sebagai tautan rilis di UI |

*Catatan:* Semua base URL (`KubectlStableURL`, `KubectlBaseURL`) dideklarasikan sebagai `var` paket di `internal/gwupdate` agar unit test dapat mengarahkannya ke `httptest.NewServer` lokal.

---

## Rencana Eksekusi Bertahap (Task-by-Task)

### Task 1: Perluasan `internal/gwupdate` untuk `TargetKubectl` (Check, Install, Switch, Revert)

**Files:**
- Modify: `internal/gwupdate/check.go`
- Modify: `internal/gwupdate/upstream.go`
- Modify: `internal/gwupdate/managed.go`
- Modify: `internal/gwupdate/install.go`
- Modify: `internal/gwupdate/version.go` *(pastikan `ExtractVersion` mem-parse `Client Version: v1.31.0` dan JSON `gitVersion` bila perlu)*
- Test: `internal/gwupdate/check_test.go`, `internal/gwupdate/upstream_test.go`, `internal/gwupdate/install_test.go`, `internal/gwupdate/version_test.go`

**Steps:**
- [ ] **Target & Upstream Check (`check.go`, `upstream.go`):**
  - Tambahkan konstanta `TargetKubectl = "kubectl"` di `check.go` dan daftarkan di `Latest(ctx, target)`.
  - Tambahkan `LatestKubectl(ctx)` di `upstream.go` yang membaca `KubectlStableURL`, memvalidasi dengan `ParseVersion` (menormalisasi prefix `v`), dan mengembalikan `Release{Version, URL}`.
  - Tambahkan `LatestKubectlChannel(ctx, majorMinor string)` (atau `KubectlMinorChannels(ctx, currentMajorMinor)` untuk memeriksa `stable-1.xx.txt` dari beberapa minor terbaru, mis. `latestMinor`, `-1`, `-2`, `-3`) agar UI dapat menawarkan pilihan cepat versi patch terbaru untuk tiap rilis minor Kubernetes!
- [ ] **Versioned Storage, Switch, & Revert (`managed.go`):**
  - Tambahkan `VersionsDir() string` (`<root>/versions`) pada `ManagedDir`.
  - `ManagedBinary` sudah memakai `os.Stat` (`install.go:78-93`) sehingga symlink `<root>/bin/kubectl` terdeteksi tanpa perubahan; tidak perlu diubah.
  - Tambahkan helper:
    - `ListKubectlVersions() ([]string, string, error)`: memindai `<root>/versions/kubectl-*`, mengurutkan dengan `Compare` (terbaru di atas), dan mengembalikan versi aktif yang sedang ditunjuk symlink `<root>/bin/kubectl` (jika ada).
    - `UseSystemKubectl(root string) error`: menghapus symlink `<root>/bin/kubectl` (hanya jika ada; jangan sentuh `/usr/bin/kubectl`), membiarkan cache di `versions/` tetap ada atau sesuai pilihan.
    - `DeleteKubectlVersion(root, version string) error`: menghapus `<root>/versions/kubectl-<ver>`, **menolak** jika versi tersebut sedang menjadi target aktif symlink `<root>/bin/kubectl`.
  - Perbarui `ManagedVersion(TargetKubectl)` untuk memanggil `kubectl version --client`.
- [ ] **Download, Checksum, Smoke Test, & Atomic Switch (`install.go`):**
  - Tambahkan `case TargetKubectl:` di `Install(ctx, target, version, root, log)`.
  - Implementasikan `installKubectl(ctx, version, root, log)`:
    1. Normalisasi `version` (buang prefix `v`/`V`), pastikan valid via `ParseVersion` (menolak string dengan `/`, `..`, spasi, atau `latest`).
    2. Cek apakah `<root>/versions/kubectl-<version>` sudah ada: jika sudah ada dan lolos `smokeTestKubectl`, langsung aktifkan lewat `pointSymlink(binLink, versionedPath)` (instant offline switch!) tanpa mengunduh ulang.
    3. Jika belum ada: unduh `kubectl.sha256` (`manifestCap`), validasi 64-char hex (`isHex`), unduh biner `kubectl` (`downloadCap`), cocokkan SHA-256.
    4. Tulis ke file kandidat unik via `writeCandidate(m.VersionsDir(), "kubectl", data)` (`chmod 0755`).
    5. **Smoke Test:** Pakai helper khusus `smokeTestKubectl` (bukan `smokeTest` yang ada: fungsi itu tidak menerima argumen dan akan memanggil `--version` setelah `version` gagal). Jalankan `version --client` (wajib `--client`, karena `kubectl version` tanpa `--client` mencoba menghubungi API server `localhost:8080` dan keluar dengan exit code non-zero saat offline). Pastikan versi yang dilaporkan cocok dengan versi yang diminta, bukan sekadar teks `version`.
    6. `os.Rename(candidate, versionedPath)`, lalu `pointSymlink(filepath.Join(m.BinDir(), "kubectl"), versionedPath)` dengan target **path absolut** (`pointSymlink` sudah membuat direktori link).
- [ ] **Unit Tests (`internal/gwupdate/*_test.go`):**
  - Uji `ExtractVersion("Client Version: v1.31.4\nKustomize Version: v5.4.2")` -> `"1.31.4"`.
  - Uji `LatestKubectl` (sukses, status 500, versi tidak valid, timeout).
  - Uji `installKubectl`: sukses + verifikasi SHA-256 + symlink menunjuk ke `versions/kubectl-<ver>`, checksum tidak cocok (tidak menulis apa pun), smoke test gagal (`--client` gagal -> versi lama tetap utuh), instant switch ke versi yang sudah ter-cache tanpa hit HTTP, `UseSystemKubectl`, dan `DeleteKubectlVersion` (menolak hapus versi aktif).

---

### Task 2: Integrasi Eksekusi `kubectl` di `internal/inspector`

**Files:**
- Modify: `internal/inspector/kube_cluster.go`
- Test: `internal/inspector/kube_cluster_test.go`

**Steps:**
- [ ] **Resolusi Biner Terpadu:**
  - Buat helper `resolveKubectlBinary() (string, error)` di `internal/inspector/kube_cluster.go` yang memanggil `gwupdate.ResolveBinary("kubectl")` (prioritas: `<data>/cli/bin/kubectl`, fallback: `PATH` sistem). Jika gagal, kembalikan error `"kubectl is not installed on the Pantau server"`.
  - Ganti `exec.LookPath("kubectl")` di `ensureKubectlInstalled()` agar memakai `resolveKubectlBinary()`.
  - Ganti `exec.Command("kubectl", ...)` di `ListKubeconfigContexts` dan `PrepareKubeconfig` agar memakai `kubectlPath` dari `resolveKubectlBinary()`.
- [ ] **Injeksi `PATH` di `localKubeRunner`:**
  - Pada `localKubeRunner.ExecTimeout`, `localKubeRunner.Stream`, dan `localKubeRunner.PipeCommand`, ganti `cmd.Env = append(os.Environ(), "KUBECONFIG="+r.kubeconfigPath)` dengan `cmd.Env = append(os.Environ(), "PATH="+gwupdate.PrependPath(os.Getenv("PATH")), "KUBECONFIG="+r.kubeconfigPath)`. Go memakai entri terakhir bila kunci env ganda, jadi `PATH` baru menggantikan yang lama. `PrependPath` sudah ada di `managed.go` dan juga menaruh bin venv Ansible di depan (tidak berbahaya) sehingga `kc() { kubectl "$@"; }` selalu memakai versi aktif yang dikelola Pantau, sambil tetap mewarisi variabel env cloud (`AWS_*`, `GOOGLE_APPLICATION_CREDENTIALS`, dll.).
- [ ] **Deteksi Status `kubectl` Lokal:**
  - Tambahkan `DetectLocalKubectl() KubectlStatus` (dengan `sync.RWMutex` cache singkat & `InvalidateKubectlDetectCache()` seperti `terraform.InvalidateDetectCache()`):
    - Melaporkan `Path`, `Version` (dari `kubectl version --client`), `Source` (`"managed"` jika berada di bawah `gwupdate.Dir().BinDir()`, `"system"` jika dari `PATH`, atau `"missing"`), `SystemAvailable` & `SystemVersion` (apakah `/usr/bin/kubectl` di luar managed dir ada, agar UI tahu apakah tombol *Use system kubectl* tersedia).
- [ ] **Unit Tests:**
  - Uji bahwa ketika `gwupdate.SetManagedRoot(tempDir)` memiliki biner managed `kubectl`, `resolveKubectlBinary()` dan `localKubeRunner` memakainya lebih dulu daripada `kubectl` di `PATH` sistem; setelah `UseSystemKubectl`, kembali memakai `PATH` sistem.

---

### Task 3: REST API Endpoint `kubectl` CLI Management

**Files:**
- Create: `internal/web/handlers_kube_cli.go`
- Modify: `internal/web/server.go`
- Create: `internal/web/handlers_kube_cli_test.go`

**Desain Endpoint (mengikuti ADR 0080 & ADR 0081):**
1. **`GET /api/kube/cli/update-check`** (dilindungi `authMiddleware`):
   - Memanggil `inspector.DetectLocalKubectl()`, `gwupdate.Check(ctx, gwupdate.TargetKubectl, installed)`, `gwupdate.Dir().ListKubectlVersions()`, dan opsional daftar kanal minor tersedia (`channels`).
   - Respons JSON (selalu HTTP 200 bila handler berjalan normal, `status: "error"` jika upstream gagal):
     ```json
     {
       "ok": true,
       "target": "kubectl",
       "installed": "1.30.8",
       "latest": "1.32.1",
       "status": "update",
       "source": "managed",
       "path": "/app/data/cli/bin/kubectl",
       "system_installed": "1.31.2",
       "managed_versions": ["1.31.4", "1.30.8"],
       "active_managed_version": "1.30.8",
       "channels": [{"minor": "1.32", "latest": "1.32.1"}, {"minor": "1.31", "latest": "1.31.5"}],
       "release_url": "https://github.com/kubernetes/kubernetes/releases/tag/v1.32.1",
       "checked_at": "..."
     }
     ```
2. **`POST /api/kube/cli/install`** (dilindungi `authMiddleware`):
   - Badan JSON: `{"version": "1.31.4"}` (opsional; jika kosong, otomatis mengambil `gwupdate.Latest(ctx, gwupdate.TargetKubectl)`).
   - Menggunakan ulang struct `installStream` (metode `emit`, `Write`, `finish`) dari `handlers_gateway_install.go` (`Content-Type: application/x-ndjson`) untuk mengirim `{"log":...}` lalu `{"done":true}` / `{"done":true,"error":...}`.
   - Mengembalikan JSON `400` jika `ManagedRoot() == ""` atau format `version` tidak valid sebelum streaming dimulai; memanggil `inspector.InvalidateKubectlDetectCache()` dan `logger.Info("kube", "kubectl", ...)` saat sukses.
3. **`POST /api/kube/cli/switch`** (dilindungi `authMiddleware`):
   - Mendukung dua mode:
     - `{"mode": "system"}`: memanggil `gwupdate.UseSystemKubectl(root)` (menolak `400` jika biner sistem tidak ditemukan di `PATH` agar operator tidak sengaja melumpuhkan fitur External Cluster!), invalidate cache, dan catat ke Activity Console.
     - `{"version": "1.30.8"}`: melakukan *switch* ke versi `kubectl` tertentu (jika sudah ter-cache di `versions/`, aktifkan instan; jika belum, unduh + verifikasi checksum via `installStream` NDJSON).
4. **`DELETE /api/kube/cli/versions/{version}`** (dilindungi `authMiddleware`):
   - Validasi `version` dengan `gwupdate.ParseVersion`, hapus dari `<data>/cli/versions/kubectl-<version>` (HTTP `409` jika versi sedang aktif).

**Steps:**
- [ ] Implementasikan `handlers_kube_cli.go` dan daftarkan rute di `internal/web/server.go`.
- [ ] Tambahkan `Activity Console` logging (`logger.Info`/`Warn` modul `"kube"`) untuk install, switch, revert ke sistem, dan penghapusan versi cache.
- [ ] Tulis `handlers_kube_cli_test.go`: auth check (401 tanpa session), validasi input (`latest` / path traversal ditolak 400), update-check (update/current/error saat upstream mati/unknown), NDJSON install + switch, penolakan `mode:"system"` bila tidak ada biner sistem, serta penolakan hapus versi aktif.

---

### Task 4: Perluasan AI Agent Tools (`gateway_cli` & `gateway_cli_install`)

**Files:**
- Modify: `internal/web/agent_tools_recent.go`
- Modify: `internal/web/agent_infra_test.go` / test tool terkait

**Steps:**
- [ ] Tambahkan `"kubectl"` pada schema enum `gateway_cli` (`all`, `ansible`, `terraform`, `tofu`, `kubectl`) di `agent_tools_recent.go`:
  - `target == "kubectl"` memanggil `GET /api/kube/cli/update-check`.
  - `target == "all"` ikut menyertakan hasil `kubectl`.
- [ ] Tambahkan `"kubectl"` pada schema enum `gateway_cli_install` (`ansible`, `terraform`, `tofu`, `kubectl`), mengarahkan ke `POST /api/kube/cli/install` dan tetap di bawah `gateInfra` (Gate 3 — selalu meminta persetujuan eksplisit operator).
- [ ] Perbarui deskripsi tool agar agent mengetahui bahwa `kubectl` versi spesifik bisa dipasang/diganti untuk mencocokkan versi klaster External Cluster.

---

### Task 5: Antarmuka Web (`kubectl` CLI Manager di Section Kubernetes)

**Files:**
- Modify: `internal/web/static/index.html`
- Modify: `internal/web/static/app/core/09-kube.js`
- Modify: kamus i18n EN & ID di `internal/web/static/app/core/01-i18n-auth.js` *(cek lokasi pasti kamus `kube_*` di `static/app/core/`)*
- Modify/Create: `internal/web/gateway_update_ui_test.go` (atau test UI khusus)

**Desain UI (mengikuti Design System Pantau, ADR 0039 & pola panel ADR 0080/0081):**
- Di header section **Kubernetes Management** (`#sectionKube`), tambahkan tombol ringkas **`kubectl` CLI** (`#btnKubeCliManager`, menampilkan chip versi singkat jika sudah dimuat) yang membuka modal `#kubeCliModal`.
- Isi `#kubeCliModal`:
  1. **Info Cakupan (Banner/Hint):** Menjelaskan bahwa versi `kubectl` ini dipakai oleh server Pantau untuk **External Cluster & validasi kubeconfig**; host `k3s`/`rke2` tetap memakai `kubectl` bawaan di masing-masing host. Juga menyatakan bahwa pemeriksaan versi bersifat *read-only*.
  2. **Status Biner Aktif:**
     - Versi aktif, badge sumber (`Managed` vs `System`), path efektif, dan tombol **Check for updates** (`GET /api/kube/cli/update-check`).
     - Badge status: *Update available* (`badge-warning`), *Up to date* (`badge-healthy`), *Version unknown* / *Check failed* (`badge-danger`, tampilkan error apa adanya bila offline).
     - Tombol cepat **Update to `<latest>`** saat status `update`.
  3. **Ganti / Pin Versi (`Switch Version`):**
     - Dropdown pilihan cepat dari **kanal minor Kubernetes** (mis. `v1.32.1`, `v1.31.5`, `v1.30.9`) + versi yang sudah ter-cache lokal (ditandai `[cached]`), atau input teks versi kustom (mis. `1.29.12`) untuk *downgrade* / mencocokkan *±1 minor skew* klaster tertentu.
     - Tombol **Install / Switch** yang menjalankan `POST /api/kube/cli/switch` dan menampilkan output stream NDJSON (`#kubeCliInstallLog`, gaya sama dengan `#tfCliUpdateLog` di `70-governance.js`).
  4. **Versi Tersimpan Lokal & Reset Sistem:**
     - Daftar versi yang ter-cache di `<data>/cli/versions/` dengan tombol **Activate** (switch instan) atau ikon hapus (nonaktif untuk versi yang sedang dipakai).
     - Tombol **Use system `kubectl`** (aktif bila sedang memakai versi managed dan biner sistem tersedia).

**Steps:**
- [ ] Tambahkan markup `#btnKubeCliManager` dan modal `#kubeCliModal` di `index.html` menggunakan Design Token dan Icon Sprite (`#i-terminal`, `#i-refresh-cw`, `#i-download`, `#i-trash-2`, dsb.).
- [ ] Implementasikan logika fetch, NDJSON streaming reader, switch version, revert system, dan delete cache di `09-kube.js`.
- [ ] Tambahkan key i18n EN & ID untuk seluruh label, pesan error, dan konfirmasi (`showConfirm` untuk aksi switch/revert/delete).
- [ ] Tambahkan uji konsistensi UI/bundle di `internal/web/` (memastikan seluruh key i18n, endpoint, dan label status jujur tersedia di bundle, pola `gateway_update_ui_test.go`).

---

### Task 6: Pembuatan ADR Baru, Pemutakhiran Konteks, & Verifikasi Akhir

**Files:**
- Create: `docs/adr/0091-kubectl-cli-update-and-version-switch.md` *(0086 sudah dipakai; nomor berikutnya yang kosong adalah 0091. Catatan: `0090` ganda sudah ada sebelumnya, di luar cakupan task ini)*
- Modify: `docs/agents/context/adr-index.md`
- Modify: `docs/agents/context/docker-kubernetes-repos.md`

**Steps:**
- [ ] Tulis ADR baru yang mendokumentasikan perluasan Gateway CLI Update (ADR 0080/0081) untuk `kubectl` (termasuk penyimpanan `versions/kubectl-<versi>`, symlink atomik, `version --client` smoke test, serta cakupan External Cluster vs k3s/RKE2).
- [ ] Perbarui `adr-index.md` dan `docker-kubernetes-repos.md` sesuai istilah domain resmi.

---

## Rencana Verifikasi

Jalankan verifikasi satu kali setelah 100% implementasi selesai (sesuai *Deferred & Targeted Testing Policy* di `AGENTS.md`):

1. **Unit & Handler Tests (Go):**
   ```bash
   go test ./internal/gwupdate/... ./internal/inspector/... ./internal/web/...
   ```
2. **Build Check:**
   ```bash
   go build -o /dev/null .
   ```
3. **E2E Verification:**
   ```bash
   cd e2e && E2E_TELEMETRY_DISABLED=1 npx e2e run tests/kube-workspaces.e2e.ts
   ```
   *(Tambahkan skenario E2E atau verifikasi browser terhadap `#kubeCliModal`: buka modal, check updates dengan route mock, switch/install versi via mock NDJSON stream, dan verifikasi penanganan error saat offline).*
