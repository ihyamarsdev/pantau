# 06 — API `/api/vpn/*` dengan redaksi rahasia

Status: ready-for-agent

Blocked by: 01, 04

## Lingkup

Handler baru `internal/web/handlers_vpn.go`, rute di `server.go` (`authMiddleware`):

```
GET    /api/vpn/status
PUT    /api/vpn/gate
GET    /api/vpn/networks
POST   /api/vpn/networks
PUT    /api/vpn/networks/{id}
DELETE /api/vpn/networks/{id}[?force=1]
POST   /api/vpn/networks/{id}/up
POST   /api/vpn/networks/{id}/down
GET    /api/vpn/networks/{id}/log
```

- Validasi sesuai spec § Safety (nama, driver, iface, ukuran config/perintah, maks 20 Network).
- `GET` mengganti setiap nilai rahasia (`setup_key`, `token`, `config`, `username`/`password`, `authkey`, `up`/`down`/`status`) dengan `""` + `<key>_set: true`; `PUT` dengan bidang rahasia kosong mempertahankan nilai lama.
- `hosts_count` per Network lewat satu query agregat (`SELECT network_id, COUNT(*) ... GROUP BY`).
- `DELETE` tanpa `force` saat masih dipakai → `409 {error, hosts: n}`; `force=1` melepas Host (`network_id=0`).
- `up`/`down` → `409` bila gate mati; galat driver dikembalikan sebagai `last_error` yang sudah tersaring.
- Log: 200 baris terakhir dari ring manager.

## Penerimaan

- Tes: rahasia tidak pernah muncul di `GET` (cek body mentah untuk nilai uji); `PUT` tanpa rahasia mempertahankan nilai tersimpan; `409` pada delete ber-Host; `409` pada `up` saat gate mati.
- Tidak ada satu pun rahasia di `logger.*` (tes dengan buffer log).

## Catatan

Bentuk respons memakai `writeJSON` yang sudah ada; jangan menambah pola respons baru. Jangan menambahkan `vpn_enabled` ke allowlist setting milik AI Agent (`internal/web/agent_tools_fleet.go:800`) — jalur ini belum diekspos ke agent (non-goal v1).
