# 01 — Skema `vpn_networks`, `hosts.network_id`, dan snapshot

Status: ready-for-agent

## Lingkup

`internal/store/db.go`:

1. Tabel `vpn_networks` persis seperti spec § Model data (termasuk `last_state`, `last_error`, `last_checked`, `last_up_at`).
2. `d.alterAddColumn("hosts", "network_id", "INTEGER NOT NULL DEFAULT 0")` di `migrate()`.
3. `store.Host.NetworkID int64` + `hostSelectCols` + `CreateHost`/`UpdateHost` + `scanHost` (urutan kolom harus cocok dengan scan).
4. `type VPNNetwork struct { ID int64; Name, Driver string; Enabled, AutoUp bool; Iface, Config, LastState, LastError string; LastChecked, LastUpAt *time.Time; CreatedAt, UpdatedAt time.Time }` + CRUD: `CreateVPNNetwork`, `UpdateVPNNetwork`, `DeleteVPNNetwork`, `GetVPNNetwork`, `ListVPNNetworks`, `SetVPNNetworkState(id, state, detail)`, `CountHostsByNetwork(id)`.
5. `SnapshotPayload.VPNNetworks []SnapshotVPNNetwork` (+ `SnapshotVPNNetwork` memuat `Config` mentah) dan restore: Network dibuat sebelum Host; `hosts.network_id` ikut dipulihkan.

## Penerimaan

- `go test ./internal/store/...` hijau; tes baru: migrasi pada DB lama (tanpa kolom) menghasilkan `network_id=0` untuk semua Host; snapshot round-trip membawa Network + `hosts.network_id`.
- Tidak ada query yang lupa kolom baru (kompilasi + tes snapshot menangkapnya).

## Catatan

`NetworkID` tidak perlu validasi FK di SQLite (delete memakai `410/409` di handler), cukup indeks `CREATE INDEX IF NOT EXISTS idx_hosts_network ON hosts(network_id)`.
