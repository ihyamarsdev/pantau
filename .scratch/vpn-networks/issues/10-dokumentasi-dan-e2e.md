# 10 — Dokumentasi pengguna, CONTEXT.md, dan e2e

Status: ready-for-agent

Blocked by: 07, 08, 09

## Lingkup

1. `CONTEXT.md`: tambahkan istilah dari spec § Istilah domain (VPN Network, Network Driver, Network Path, Bound Dial, Tunnel State, Network Down) dengan `_Avoid_` masing-masing — dan jangan memakai "Gateway Host" (sudah dipakai Kubernetes).
2. `README.md` + `README.id.md` + `CHANGELOG.md`: bagian VPN/Tunnel Networks — prasyarat (biner vendor terpasang di master, root/`CAP_NET_RAW`), contoh per driver (netbird setup key, `.ovpn`, Netmaker token, `wg-quick`, tailscale authkey), batas (tanpa SSO, satu Network per Host, tanpa jump/SOCKS), dan cara memverifikasi (`/api/hosts/{id}/test`).
3. `e2e/tests/vpn-networks.e2e.ts`: gate default mati → `POST .../up` menampilkan penolakan; buat Network `custom` + Host; `/test` menjawab `network down: <name>`; badge Network di grid Host; hapus Network yang masih dipakai → dialog `409`. Ikuti `skill://e2e` dan `E2E_TELEMETRY_DISABLED=1`.

## Penerimaan

- `cd e2e && npm test` hijau (tes baru + yang lama).
- Dokumen tidak menjanjikan fitur di luar v1 (tanpa API vendor, tanpa SSO, tanpa jump host).

## Catatan

e2e tidak boleh bergantung pada VPN sungguhan; semua lewat driver `custom` yang sengaja gagal.
