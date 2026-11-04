# Cloudflare Management (spec)

Status: ready-for-agent

Kelola akun Cloudflare dari Pantau lewat REST API v4 (https://developers.cloudflare.com/api/): Zone, DNS record, purge cache, dan beberapa zone setting. Pola mengikuti ADR 0047 (klaster Kubernetes eksternal) dan ADR 0035 (SSH Key Library): kredensial disimpan di SQLite, **tidak pernah dikembalikan oleh API**, ikut System Snapshot.

## Istilah domain (tambahkan ke CONTEXT.md)

- **Cloudflare Account**: entri kredensial (nama, `account_id` opsional, API Token) yang terdaftar di Pantau untuk mengelola Zone Cloudflare via API.
  _Avoid_: CF profile, API key entry
- **Cloudflare Zone**: domain yang dikelola di akun Cloudflare (nama, status, plan, name server).
  _Avoid_: Site, domain entry
- **Zone Setting**: pengaturan zone yang boleh diubah Pantau (allowlist di bawah).

## Non-goals

Workers, R2, Tunnels, WAF/rules, Page Rules, analytics GraphQL, Global API Key (hanya API Token Bearer), multi-user RBAC.

## Kontrak

### 1. Paket `internal/cloudflare` (stdlib `net/http` saja, tanpa dependensi baru)

```go
package cloudflare

const DefaultBaseURL = "https://api.cloudflare.com/client/v4"

type Client struct {
    BaseURL string       // default DefaultBaseURL; diganti di test dengan httptest
    HTTP    *http.Client // Timeout 20s
    // token private
}
func New(token string) *Client

// APIError: non-2xx atau success=false. Error() = pesan Cloudflare digabung ("code: message; ...")
// bila ada, kalau tidak "cloudflare: HTTP <status>". Status = HTTP status upstream.
type APIError struct { Status int; Errors []APIErrorItem }
type APIErrorItem struct { Code int `json:"code"`; Message string `json:"message"` }

type TokenInfo struct { ID string `json:"id"`; Status string `json:"status"`; ExpiresOn string `json:"expires_on,omitempty"` }
// accountID != "" -> GET /accounts/{id}/tokens/verify (token milik akun); kosong -> GET /user/tokens/verify
func (c *Client) VerifyToken(ctx context.Context, accountID string) (*TokenInfo, error)

type Zone struct {
    ID          string   `json:"id"`
    Name        string   `json:"name"`
    Status      string   `json:"status"`
    Paused      bool     `json:"paused"`
    Type        string   `json:"type"`
    NameServers []string `json:"name_servers"`
    Plan        string   `json:"plan"`          // dari plan.name
    AccountName string   `json:"account_name"`  // dari account.name
}
// accountID != "" memfilter ?account.id=. Paginasi per_page=50, maksimum 20 halaman.
func (c *Client) ListZones(ctx context.Context, accountID string) ([]Zone, error)
func (c *Client) GetZone(ctx context.Context, zoneID string) (*Zone, error)

type DNSRecord struct {
    ID         string `json:"id"`
    Type       string `json:"type"`
    Name       string `json:"name"`
    Content    string `json:"content"`
    TTL        int    `json:"ttl"`               // 1 = auto
    Proxied    *bool  `json:"proxied,omitempty"`
    Proxiable  bool   `json:"proxiable"`
    Priority   *int   `json:"priority,omitempty"`
    Comment    string `json:"comment,omitempty"`
    ModifiedOn string `json:"modified_on,omitempty"`
}
type DNSRecordInput struct {
    Type     string `json:"type"`
    Name     string `json:"name"`
    Content  string `json:"content"`
    TTL      int    `json:"ttl"`                 // 1 = auto
    Proxied  *bool  `json:"proxied,omitempty"`
    Priority *int   `json:"priority,omitempty"`
    Comment  string `json:"comment,omitempty"`
}
func (in DNSRecordInput) Validate() error // type dalam {A,AAAA,CNAME,TXT,MX,NS,SRV,CAA}; name & content wajib, <= 255 / 2048; TTL 1 atau 60..86400 (ttl 0 -> dianggap 1); priority wajib untuk MX
func (c *Client) ListDNSRecords(ctx context.Context, zoneID string) ([]DNSRecord, error) // paginasi per_page=100, maks 50 halaman
func (c *Client) CreateDNSRecord(ctx context.Context, zoneID string, in DNSRecordInput) (*DNSRecord, error)
func (c *Client) UpdateDNSRecord(ctx context.Context, zoneID, recordID string, in DNSRecordInput) (*DNSRecord, error) // PUT
func (c *Client) DeleteDNSRecord(ctx context.Context, zoneID, recordID string) error

type PurgeRequest struct {
    Everything bool     `json:"purge_everything,omitempty"`
    Files      []string `json:"files,omitempty"`
    Hosts      []string `json:"hosts,omitempty"`
    Tags       []string `json:"tags,omitempty"`
    Prefixes   []string `json:"prefixes,omitempty"`
}
func (r PurgeRequest) Validate() error // tepat satu mode terisi; Files maks 30 entri (batas CF free)
func (c *Client) PurgeCache(ctx context.Context, zoneID string, req PurgeRequest) error

type Setting struct {
    ID       string      `json:"id"`
    Value    interface{} `json:"value"`
    Editable bool        `json:"editable"`
}
// Allowlist yang boleh diubah (nilai string): ssl{off,flexible,full,strict}, always_use_https{on,off},
// security_level{off,essentially_off,low,medium,high,under_attack}, development_mode{on,off},
// min_tls_version{1.0,1.1,1.2,1.3}, automatic_https_rewrites{on,off}
var EditableSettings map[string][]string
func ValidateSetting(id, value string) error
// GetZoneSettings mengembalikan HANYA setting yang ada di EditableSettings (urut stabil per id).
func (c *Client) GetZoneSettings(ctx context.Context, zoneID string) ([]Setting, error)
func (c *Client) SetZoneSetting(ctx context.Context, zoneID, id, value string) (*Setting, error) // validasi lalu PATCH /zones/{id}/settings/{id} {"value":...}

// ValidID: true bila s cocok ^[0-9a-f]{32}$ (format ID zone/record/account Cloudflare). Dipakai
// paket web SEBELUM menyusun path, dan di dalam client sebagai pertahanan kedua (cegah path injection).
func ValidID(s string) bool
```

Semua request: header `Authorization: Bearer <token>`, `Content-Type: application/json`; body respons dibatasi 4 MiB; envelope `{success, errors[], result, result_info{page,total_pages}}`. Token TIDAK PERNAH muncul di error, log, atau JSON keluar.

### 2. Store (`internal/store/cloudflare_accounts.go`, tabel `cloudflare_accounts`)

```go
type CloudflareAccount struct {
    ID        int64     `json:"id"`
    Name      string    `json:"name"`
    AccountID string    `json:"account_id,omitempty"`
    APIToken  string    `json:"-"`
    CreatedAt time.Time `json:"created_at"`
}
var ErrCloudflareAccountNameTaken = errors.New("a cloudflare account with this name already exists")
func (d *DB) CreateCloudflareAccount(a *CloudflareAccount) (int64, error) // nama unik case-insensitive
func (d *DB) GetCloudflareAccount(id int64) (*CloudflareAccount, error)   // termasuk APIToken
func (d *DB) ListCloudflareAccounts() ([]CloudflareAccount, error)        // APIToken boleh terisi; JSON tag "-" mencegah bocor
func (d *DB) UpdateCloudflareAccount(a *CloudflareAccount) error          // selalu update name, account_id; api_token hanya bila a.APIToken != ""
func (d *DB) DeleteCloudflareAccount(id int64) error
```
Ikut System Snapshot (`SnapshotCloudflareAccount`, field `CloudflareAccounts ... json:"cloudflare_accounts,omitempty"`, ikut daftar tabel restore) mengikuti pola `kube_clusters`.

### 3. HTTP API (`internal/web/handlers_cloudflare.go`, semua dibungkus `s.authMiddleware`)

| Method & path | Body | Respons |
|---|---|---|
| `GET /api/cloudflare/accounts` | – | `{"accounts":[CloudflareAccount...]}` |
| `POST /api/cloudflare/accounts` | `{name, account_id?, api_token}` | 201 `{"account":..., "token":TokenInfo}`. Token diverifikasi ke Cloudflare SEBELUM disimpan; gagal -> 400 `{"error":...}` |
| `PUT /api/cloudflare/accounts/{id}` | `{name, account_id?, api_token?}` | 200 `{"account":...}`; `api_token` kosong = token lama dipertahankan; bila diisi diverifikasi dulu |
| `DELETE /api/cloudflare/accounts/{id}` | – | 200 `{"ok":true}` |
| `GET /api/cloudflare/accounts/{id}/zones` | – | `{"zones":[Zone...]}` |
| `GET /api/cloudflare/accounts/{id}/zones/{zid}/dns` | – | `{"records":[DNSRecord...]}` |
| `POST /api/cloudflare/accounts/{id}/zones/{zid}/dns` | DNSRecordInput | 201 `{"record":...}` |
| `PUT /api/cloudflare/accounts/{id}/zones/{zid}/dns/{rid}` | DNSRecordInput | 200 `{"record":...}` |
| `DELETE /api/cloudflare/accounts/{id}/zones/{zid}/dns/{rid}` | – | 200 `{"ok":true}` |
| `POST /api/cloudflare/accounts/{id}/zones/{zid}/purge` | PurgeRequest | 200 `{"ok":true}` |
| `GET /api/cloudflare/accounts/{id}/zones/{zid}/settings` | – | `{"settings":[Setting...], "options": EditableSettings}` |
| `PUT /api/cloudflare/accounts/{id}/zones/{zid}/settings` | `{"id":"ssl","value":"full"}` | 200 `{"setting":...}` |

Aturan: `{zid}`/`{rid}` wajib lolos `cloudflare.ValidID` (400 jika tidak); `{id}` int64 -> 404 bila akun tak ada; method lain -> 405. Galat upstream `*cloudflare.APIError` -> HTTP 502 (kecuali upstream 401/403 -> 400 dengan pesan Cloudflare, agar UI tidak dianggap sesi Pantau habis) dengan `{"error": <pesan CF>}`; token tidak pernah ada di respons/log. Mutasi (akun, DNS, purge, setting) dicatat ke activity log melalui mekanisme logger yang sama dengan handler lain (tanpa token). Server memiliki field `cfClient func(token string) *cloudflare.Client` (default `cloudflare.New`) agar test dapat mengarahkan ke `httptest`.

### 4. UI (`internal/web/static/index.html`, i18n id/en yang sudah ada)

Entri navigasi "Cloudflare" di tempat yang sejajar dengan Docker/Kubernetes (side panel atau header, ikuti pola yang ada), memakai **Workspace Modal**/**Action Dialog**, **Toast Notification** dan **Confirmation Dialog** (bukan `alert/confirm` bawaan). Fitur: daftar/tambah/edit/hapus Cloudflare Account (field token bertipe password, tidak pernah diisi ulang dari server); pilih akun -> daftar Zone (cari/filter); pilih zone -> tab DNS (tabel, cari, tambah/ubah/hapus dengan konfirmasi hapus, toggle proxied), Cache (Purge Everything dengan konfirmasi, Purge by URL: textarea satu URL per baris), Settings (select per setting dari `options`). Tetap airgapped-asset: tanpa CDN/script eksternal baru.

### 5. Dokumen

ADR `docs/adr/0048-cloudflare-management-via-api.md` (bahasa Indonesia, format seperti 0047), istilah di `CONTEXT.md`, `CHANGELOG.md` (Keep a Changelog, bagian Unreleased/versi berjalan sesuai berkas), `README.md`, `README.id.md`, `docs/user-guide.md`, `docs/user-guide.id.md`. Panduan dokumen in-app disematkan ke binary — periksa apakah berkas-berkas ini di-embed dan pastikan tetap konsisten.

## Model keamanan

1. API Token = rahasia: `json:"-"`, tidak ada endpoint yang mengembalikannya, tidak ada di log/error; tersimpan plaintext di SQLite seperti `ssh_keys.private_key` (ADR 0035), terenkripsi di System Snapshot.
2. Host tujuan tetap `api.cloudflare.com` (BaseURL tidak bisa diubah dari input pengguna) -> tidak ada SSRF.
3. Semua ID di path divalidasi `^[0-9a-f]{32}$` -> tidak ada path injection.
4. Body request dibatasi ukurannya (ikuti `decodeKubeBody`/helper yang ada); respons upstream dibatasi 4 MiB.
5. Izin sebenarnya dibatasi oleh scope API Token yang dibuat operator di Cloudflare (disarankan least privilege: Zone:Read, DNS:Edit, Cache Purge, Zone Settings:Edit).
