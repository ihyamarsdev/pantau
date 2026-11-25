# Issue 08: Hardening login, 2FA, dan session

Status: open
Severity: MEDIUM

## Deskripsi
1. Brute force: `/api/login` tanpa throttle (default password `admin`, min 4 char); tiap login benar membuat `temp_token` baru tanpa batas; `ch.attempts++` tidak di-lock (`internal/web/server.go:616-662`, `685-695`).
2. `handle2FASetup`/`handle2FAEnable` menimpa `totp_secret` hanya dengan cookie session, tanpa password/TOTP, walau 2FA sudah aktif (`server.go:756-786`, `811-851`).
3. Ganti password/2FA tidak mencabut session lain; TTL session 365 hari (`server.go:936-968`, `557`).
4. Error tulis DB diabaikan; `totp_enabled=true` ditulis sebelum secret; ganti password tetap balas `password_updated` (`server.go:843-847`, `965`).

## Kriteria Penerimaan
- [ ] Counter gagal + backoff (per IP/global) untuk `/api/login` dan `/api/login/2fa`; batasi challenge terbuka; increment atomik.
- [ ] Setup/enable 2FA saat sudah aktif butuh password + TOTP/recovery code.
- [ ] `DeleteAllSessionsExcept(token)` (memori + SQLite) dipanggil saat ganti password/2FA.
- [ ] Secret & recovery code ditulis dulu, `totp_enabled` terakhir (idealnya satu transaksi); error → 500.
