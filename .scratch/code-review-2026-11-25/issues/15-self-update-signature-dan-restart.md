# Issue 15: Self-update tanpa signature & handover restart bentrok port/DB

Status: open
Severity: MEDIUM

## Deskripsi
1. `checksums.txt` berasal dari release yang sama dan tidak ditandatangani (`internal/updater/updater.go:328-366`) — hanya mendeteksi korupsi, bukan aset yang diganti.
2. `updater.Restart` menjalankan child sebelum parent melepas port/DB (`updater.go:528-550`; `main.go:227`, `:501`) → child bind ke port+1 (default) atau exit (port eksplisit); SQLite dibuka dua proses.

## Kriteria Penerimaan
- [ ] CI menandatangani `checksums.txt` (minisign/cosign), public key di-pin di biner, diverifikasi sebelum hash dipercaya.
- [ ] Listener ditutup (`shutdownFn`) sebelum `cmd.Start()`, atau FD listener diwariskan via `ExtraFiles`.
