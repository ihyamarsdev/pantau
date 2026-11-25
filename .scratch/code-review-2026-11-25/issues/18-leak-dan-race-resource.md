# Issue 18: Kebocoran resource & race kecil

Status: open
Severity: LOW

## Deskripsi
- Tunnel transport: check-then-store race membocorkan SSH runner; `lastUsed` tak pernah dipakai (tak ada idle close) — `internal/web/handlers_tunnel.go:28-96`.
- Port-forward reconnect race dengan stop → runner bocor — `internal/web/handlers_portfwd.go:393-427`.
- Forwarder WebSocket pkgupgrade/kube tidak ditunggu → chunk terakhir `[pantau] exit=N` hilang — `handlers_pkgupgrade.go:126-161`, `handlers_kube.go:603-633`.
- Settings cache bisa menyimpan nilai basi saat Get/Set bersamaan — `internal/store/settings.go:63-82`.
- Lock instance dihapus setelah unlock → dua instance di satu DB — `internal/instance/guard.go:52-61`.
- Map rate-limit webhook tumbuh tanpa batas dari token tak terautentikasi; tiap request scan semua workspace — `internal/web/terraform_webhook.go:36-54`.
- SMTP tanpa I/O deadline → goroutine bocor per event — `internal/notify/smtp.go:74,111,150`.

## Kriteria Penerimaan
- [ ] `LoadOrStore` + tutup runner yang kalah; sweep idle berdasarkan `lastUsed`.
- [ ] `setRunner` cek `f.stopped` di bawah `f.mu`.
- [ ] Tunggu goroutine forwarder sebelum `ws.Close()`.
- [ ] Cache setting pakai mutex/generation counter.
- [ ] Hapus lock file sebelum unlock (atau jangan dihapus).
- [ ] Rate-limit webhook setelah token cocok, atau LRU per IP terbatas.
- [ ] `conn.SetDeadline(now+30s)` setelah dial SMTP.
