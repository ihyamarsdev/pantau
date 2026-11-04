# 02 — Penyimpanan Akun Cloudflare & Integrasi Snapshot (internal/store)

Type: task
Status: ready-for-agent
Blocked by:

## Tujuan

Menyediakan persistensi data akun Cloudflare pada SQLite lokal di `internal/store/cloudflare_accounts.go` dan integrasi penuh dengan pencadangan terenkripsi System Snapshot di `internal/store/db.go`.

## Perubahan

- Migrasi skema database di `internal/store/db.go`:
  - Tabel `cloudflare_accounts` (`id INTEGER PRIMARY KEY AUTOINCREMENT`, `name TEXT UNIQUE COLLATE NOCASE`, `account_id TEXT DEFAULT ''`, `api_token TEXT NOT NULL`, `created_at DATETIME DEFAULT CURRENT_TIMESTAMP`).
- Implementasi `internal/store/cloudflare_accounts.go`:
  - Model struct `CloudflareAccount` dengan tag `APIToken string json:"-"`.
  - Fungsi CRUD: `CreateCloudflareAccount` (nama unik case-insensitive dengan `ErrCloudflareAccountNameTaken`), `GetCloudflareAccount`, `ListCloudflareAccounts`, `UpdateCloudflareAccount` (hanya memperbarui token jika non-kosong), `DeleteCloudflareAccount`.
- Integrasi System Snapshot:
  - Ekspor dan impor field `CloudflareAccounts` pada struct snapshot terenkripsi (AES-256-GCM / Argon2id).
  - Penambahan tabel `cloudflare_accounts` ke daftar tabel pemulihan snapshot dan migrasi.
- Unit test di `internal/store/cloudflare_accounts_test.go` dan pengujian siklus hidup snapshot.

## Acceptance

- `go test -v ./internal/store/ -run 'Cloudflare|Snapshot'` berhasil.
- Token tersimpan aman (tidak bocor via serialisasi JSON struct) dan tersinkronisasi saat snapshot export/import.
