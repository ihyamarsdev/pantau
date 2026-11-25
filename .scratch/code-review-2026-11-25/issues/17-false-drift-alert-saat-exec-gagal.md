# Issue 17: Alert drift palsu saat inspeksi timeout / koneksi putus

Status: open
Severity: MEDIUM

## Deskripsi
1. `evaluateRule` mengabaikan error `runner.Exec` (`internal/inspector/desired_rules.go:146`); saat budget 45 s habis runner ditutup (`inspector.go:240-248`) → sisa rule membaca output kosong sebagai stopped/missing/closed → alert critical, incident, notifikasi dibuat sebelum `ctx.Err()` dicek.
2. `timeout -k 2s 5s df -Pk X || df -Pk X` menjalankan ulang df tanpa timeout pada mount hang; cabang "unresponsive" (exit 124) tak pernah tercapai (`desired_rules.go:276`).

## Kriteria Penerimaan
- [ ] Error exec / runner tertutup / ctx batal → status `unknown`, loop berhenti sebelum alerting.
- [ ] Fallback df hanya bila perintah `timeout` tidak tersedia.
