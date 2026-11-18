# Issue 01: Gateway CLI Update Check (Ansible Gate + Terraform Gate)

Status: ready-for-agent

## Deskripsi
Implementasikan **Gateway CLI Update Check** sesuai keputusan ADR 0080 dan spesifikasi di `.scratch/gateway-update-check/spec.md`: pemeriksaan read-only atas versi CLI yang terpasang di server Pantau untuk dua *gateway* — biner `ansible-playbook` di balik **Ansible Gate** dan biner `terraform`/`tofu` di balik **Terraform Gate** — dibandingkan dengan versi rilis terbaru yang dipublikasikan upstream.

## Kriteria Penerimaan

### Paket `internal/gwupdate` (stdlib, tanpa DB)
- [x] `LatestAnsible(ctx)` — versi rilis `ansible-core` terbaru dari PyPI. Body `pypi.org/pypi/ansible-core/json` dibaca sebagai stream (`json.Decoder`: `{` → key `"info"` → struct `{"version": string}`) lalu ditutup, sehingga riwayat rilis tidak pernah diunduh.
- [x] `LatestTerraform(ctx)` — versi rilis `terraform` terbaru dari `api.releases.hashicorp.com`.
- [x] `LatestOpenTofu(ctx)` — versi rilis `tofu` terbaru dari redirect GitHub `releases/latest`; versi diambil dari path `Location`, dengan `CheckRedirect` yang tidak mengikuti redirect.
- [x] `ParseVersion(s)` / `Compare(a, b)` — semver sederhana (inti numerik + pre-release) dengan aturan yang sama seperti `internal/terraform/registry_constraint.go`.
- [x] `ExtractVersion(output)` — membaca versi dari baris keluaran tiap biner (`ansible-playbook [core 2.22.0]`, `Terraform v1.15.9`, `OpenTofu v1.13.9`).
- [x] `Check(ctx, target, installed)` — menghasilkan `Result` dengan status `update`, `current`, `unknown` atau `error`. Versi tak terpasang atau tak dapat diparse → `unknown` (bukan `update`); upstream tidak dapat dihubungi → `error` (bukan "terbaru").
- [x] Timeout 15 detik per permintaan.

### REST API
- [x] `GET /api/ansible/update-check` → hasil pemeriksaan `ansible`.
- [x] `GET /api/terraform/update-check` → hasil pemeriksaan untuk setiap biner yang terpasang (`terraform`, `tofu`).
- [x] Kedua endpoint bekerja saat Gate nonaktif (pemeriksaan bersifat baca-only).
- [x] Kegagalan upstream dijawab HTTP 200 dengan `status: "error"` — bukan 5xx, bukan "terbaru".
- [x] Rute terdaftar pada `internal/web/server.go` dengan `authMiddleware`.

### UI
- [x] **Ansible → Settings → Ansible Gate**: tombol **Check for updates** (`[data-st="update-check"]`), badge status (`#ansibleCliUpdateStatus`), versi terpasang vs terbaru, tautan ke rilis, dan waktu pemeriksaan.
- [x] **Terraform → Terraform Gate → CLI update check**: kartu `#tfCliUpdateCard` dengan tombol `#tfCliUpdateCheck` dan satu baris per biner yang terpasang.
- [x] Kedua panel menyatakan bahwa pemeriksaan hanya membaca dan tidak memasang apa pun.
- [x] Kunci i18n EN/ID lengkap (`ans_rs_cli_*` dan `tfcli_*`).

### Dokumentasi
- [x] `CONTEXT.md`: istilah **Gateway CLI Update Check**.
- [x] `docs/adr/0080-gateway-cli-update-check.md`.
- [x] `CHANGELOG.md`, `README.md`, `README.id.md`, `docs/user-guide.md`, `docs/user-guide.id.md`.

### Verifikasi
- [x] `go test ./internal/gwupdate/` — 17 test lulus (parsing, perbandingan, ekstraksi, sumber rilis, status).
- [x] `go test ./internal/web/ -run 'GatewayUpdateCheck|AnsibleUpdateCheck|TerraformUpdateCheck'` — 7 test handler + 5 test UI lulus.
- [x] `go test ./...` — 1644 test lulus di 23 paket.
- [x] `cd e2e && npx e2e run tests/gateway-update-check.e2e.ts` — 3 test lulus.

## Catatan
- Upstream dibaca dari sisi server, jadi stubbing di browser tidak bisa mengintercept-nya; e2e men-stub endpoint Pantau yang dipanggil UI, sementara sisi server ditutup test Go.

## Lanjutan: memasang pembaruan (ADR 0081)

### Kriteria Penerimaan
- [x] `internal/gwupdate/install.go`: `Install(ctx, target, version, root, log)` — unduh, verifikasi checksum manifest upstream, pasang.
  - [x] `terraform`/`tofu`: arsip zip → satu biner, checksum dari `*_SHA256SUMS`, diekstrak hanya dari root arsip.
  - [x] `ansible`: wheel dari PyPI JSON (sha256 dicek) → `python3 -m venv` → `pip install`, symlink `venv` dipindah paling akhir.
  - [x] Kandidat ditulis ke file unik (`os.CreateTemp`), dijalankan untuk uji versi, baru `os.Rename` ke tempat akhir.
  - [x] Versi non-rilis (`latest`, teks bebas) ditolak sebelum satu request pun dibuat.
- [x] `internal/gwupdate/managed.go`: `SetManagedRoot`, `ResolveBinary`, `ResolveVenvCommand`, `PrependPath`, `ManagedVersion`.
- [x] `internal/terraform`: Detect/lookTool/Apply/Plan/StateOps memakai `ResolveBinary`; `prepareEnv` memakai `PrependPath`; `InvalidateDetectCache`.
- [x] `internal/ansible`: Available/Run/Adhoc/facts/git memakai `ResolveVenvCommand` dan `PrependPath`; `InvalidateAvailableCache`.
- [x] `main.go`: `gwupdate.SetManagedRoot(<data>/cli)`.
- [x] `POST /api/ansible/install`, `POST /api/terraform/install` — NDJSON stream, 400 untuk target/versi tak sah atau saat tidak ada direktori terkelola.
- [x] UI: tombol **Update now** di panel Ansible Gate (muncul saat status `update`) dan per biner di panel Terraform Gate, log instalasi di-stream, i18n EN/ID.
- [x] Docs: ADR 0081, glossary **Gateway CLI Update**, README EN/ID, user-guide EN/ID, CHANGELOG.

### Verifikasi
- [x] `go test ./internal/gwupdate/` — 31 test (termasuk checksum mismatch, manifest tanpa entri, zip tanpa biner, versi salah, symlink, log).
- [x] `go test ./internal/web/ -run 'Gateway|CLIInstall'` — 15 test.
- [x] e2e `tests/gateway-update-check.e2e.ts` — 4 test (termasuk alur update dengan log ter-stream).
- [x] Server sungguhan: `POST /api/terraform/install` memasang terraform 1.17.2 ke `<data>/cli/bin`; `/api/terraform/status` kemudian melaporkan path biner terkelola itu.
- [x] Instalasi nyata ke upstream: terraform 1.17.2, tofu 1.13.9, ansible-core 2.22.0 — ketiganya checksum terverifikasi dan binernya berjalan.
