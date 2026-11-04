# 03 — HTTP Handlers & REST API Cloudflare (internal/web)

Type: task
Status: ready-for-agent
Blocked by: 01, 02

## Tujuan

Menyediakan endpoint REST API di bawah `/api/cloudflare/...` yang terautentikasi (`authMiddleware`) untuk mengelola akun Cloudflare, Zone, DNS Records, Cache Purge, dan Zone Settings.

## Perubahan

- Pendaftaran handler dan field factory klien di `internal/web/server.go`:
  - Field `cfClient func(token string) *cloudflare.Client` dengan nilai bawaan `cloudflare.New`.
  - Pendaftaran rute `/api/cloudflare/accounts` dan sub-rute terkait.
- Implementasi handler di `internal/web/handlers_cloudflare.go`:
  - `GET /api/cloudflare/accounts`: daftar akun tanpa membuka token.
  - `POST /api/cloudflare/accounts`: verifikasi token ke Cloudflare sebelum menyimpan entri baru ke basis data.
  - `PUT /api/cloudflare/accounts/{id}`: perbarui nama, ID akun, atau ganti token (verifikasi jika ada token baru).
  - `DELETE /api/cloudflare/accounts/{id}`: hapus akun Cloudflare.
  - `GET /api/cloudflare/accounts/{id}/zones`: ambil daftar zone dari Cloudflare API.
  - `GET /api/cloudflare/accounts/{id}/zones/{zid}/dns`: daftar DNS record.
  - `POST /api/cloudflare/accounts/{id}/zones/{zid}/dns`: buat DNS record baru dengan validasi input.
  - `PUT /api/cloudflare/accounts/{id}/zones/{zid}/dns/{rid}`: ubah DNS record.
  - `DELETE /api/cloudflare/accounts/{id}/zones/{zid}/dns/{rid}`: hapus DNS record.
  - `POST /api/cloudflare/accounts/{id}/zones/{zid}/purge`: eksekusi purge cache.
  - `GET /api/cloudflare/accounts/{id}/zones/{zid}/settings`: ambil daftar pengaturan zone yang boleh diubah.
  - `PUT /api/cloudflare/accounts/{id}/zones/{zid}/settings`: perbarui nilai pengaturan zone.
- Validasi keamanan:
  - Pemeriksaan `cloudflare.ValidID` pada `{zid}` dan `{rid}` (HTTP 400 jika tidak cocok).
  - Pembatasan ukuran body request mengikuti helper `decodeKubeBody` / `io.LimitReader`.
  - Pemetaan galat upstream: status 401/403 dari Cloudflare diteruskan sebagai HTTP 400 agar sesi web Pantau tidak dianggap hangus; galat upstream lainnya dikembalikan sebagai HTTP 502.
  - Audit logging via structured activity logger tanpa mencatat token.
- Unit test komprehensif di `internal/web/handlers_cloudflare_test.go` menggunakan mock `cfClient`.

## Acceptance

- `go test -v ./internal/web/ -run 'Cloudflare'` lolos.
- Rute mengembalikan status HTTP dan skema JSON sesuai spesifikasi kontrak.
