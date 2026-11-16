# 04 — Supervisor `Ensure`/`Up`/`Down` + gate `vpn_enabled`

Status: ready-for-agent

Blocked by: 03

## Lingkup

`internal/vpnnet/manager.go`:

- `Ensure(ctx, networkID)`: `0` → `nil,nil`; `!Enabled` → `ErrNetworkDisabled`; state `up` dan interface masih ada → `Path`; `auto_up` → bring-up singleflight; selain itu `ErrNetworkDown`.
- Singleflight per Network; backoff 30 detik setelah galat; 3 galat berturut → state `error` (auto-up berhenti sampai `Up` manual atau 5 menit).
- Timeout: `Up` 60 detik (`openvpn` 90), `Down` 20, `Status` 10. Maksimum 3 proses anak bersamaan (409 di lapisan API).
- Ring log 200 baris per Network untuk `GET /api/vpn/networks/{id}/log`.
- Gate `vpn_enabled` di `store.settings` (default `0`): `Up`/`Down`/`Ensure` menolak `ErrGateDisabled` saat mati.
- Loop status latar 30 detik untuk Network `enabled` (tanpa `Up`); `Shutdown()` mematikan proses anak — dipanggil `main.go` saat sinyal berhenti.

## Penerimaan

- Tes dengan driver palsu: dua `Ensure` bersamaan → `Up` hanya sekali; galat lalu `Ensure` lagi < 30 detik → tidak memanggil `Up`; tiga galat → state `error`; gate mati → `Up` tidak menyentuh driver.
- `Shutdown` mematikan proses anak (driver palsu mencatat `Down`).

## Catatan

Cegah kebocoran goroutine: loop status berhenti lewat context, bukan flag global.
