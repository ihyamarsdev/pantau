# 01 — Klien HTTP Cloudflare v4 (internal/cloudflare)

Type: task
Status: ready-for-agent
Blocked by:

## Tujuan

Menyediakan paket Go klien HTTP (`internal/cloudflare`) untuk berinteraksi dengan Cloudflare REST API v4 (Zone, DNS records, Purge Cache, Zone Settings, Verify Token) tanpa dependensi pihak ketiga, hanya menggunakan library standar `net/http`.

## Perubahan

- Implementasi `internal/cloudflare/client.go` dan subberkas terkait:
  - `DefaultBaseURL = "https://api.cloudflare.com/client/v4"`.
  - `Client` dengan `BaseURL`, `HTTP` (*timeout 20s*), dan token bearer privat.
  - Penanganan galat `APIError` dengan parsing envelope `{success, errors[], result, result_info}`.
  - `ValidID(s string) bool` mencocokkan `^[0-9a-f]{32}$`.
  - `VerifyToken(ctx, accountID)` via `/accounts/{id}/tokens/verify` atau `/user/tokens/verify`.
  - `ListZones(ctx, accountID)` (paginasi per_page=50, maks 20 halaman) dan `GetZone(ctx, zoneID)`.
  - `ListDNSRecords`, `CreateDNSRecord`, `UpdateDNSRecord`, `DeleteDNSRecord` beserta validasi `DNSRecordInput` (tipe, name/content <= 255/2048, TTL 1 atau 60..86400, priority untuk MX).
  - `PurgeCache(ctx, zoneID, req PurgeRequest)` dengan validasi `PurgeRequest` (tepat satu mode, file maks 30).
  - `EditableSettings`, `ValidateSetting(id, value)`, `GetZoneSettings`, dan `SetZoneSetting`.
  - Pembatasan ukuran respons body upstream maksimal 4 MiB.
  - Sanitasi token: token tidak pernah muncul dalam format error atau logging.
- Unit test komprehensif menggunakan `httptest.Server` di `internal/cloudflare/*_test.go`.

## Acceptance

- `go test -v ./internal/cloudflare/...` lolos dengan coverage tinggi.
- Tanpa dependensi eksternal baru (`go.mod` bersih).
