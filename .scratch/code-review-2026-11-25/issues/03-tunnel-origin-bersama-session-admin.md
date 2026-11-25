# Issue 03: Web app yang di-tunnel bisa memakai session admin ke port mana pun di host mana pun

Status: resolved
Severity: HIGH

## Deskripsi
Semua tunnel berbagi satu origin (`http://<host>:<tunnelPort>/api/tunnel/...`) yang hanya dilindungi `authMiddleware`. Cookie tidak dipisah per port, jadi JS dari aplikasi yang di-tunnel (mis. Grafana yang terkompromi) bisa `fetch('/api/tunnel/7/2375/containers/create', …)` — same-origin, lolos `CrossOriginProtection`, terautentikasi cookie admin → akses Docker API/etcd/panel loopback di host mana pun.

- `internal/web/handlers_tunnel.go:248` (`TunnelHandler`), `:127` (`handleTunnel`)

## Kriteria Penerimaan
- [x] Token per-tunnel: cookie `pantau_tunnel` = HMAC(host,port), Path ter-scope ke tunnel itu, HttpOnly, diterbitkan oleh endpoint utama.
- [x] Request tunnel tanpa token yang cocok ditolak (`tunnelAuth`). `pantau_session` tidak lagi diterima di origin tunnel.
- [x] Navigasi lintas-site (`Sec-Fetch-Site: same-site`) tidak bisa menerbitkan cookie.
- [x] Test: `handlers_tunnel_auth_test.go`, dan `tunnel_test.go` diperbarui ke kontrak baru.
