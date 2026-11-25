# Issue 16: Browser bridge terminal menulis ke /tmp bersama

Status: open
Severity: MEDIUM

## Deskripsi
`ensureBrowserBridge` menulis `/tmp/pantau-browser` (error diabaikan) lalu tetap menambahkan `export BROWSER=/tmp/pantau-browser` ke `~/.bashrc`, `~/.zshrc`, `~/.profile` (`internal/web/server.go:2927-2950`). User lokal low-priv bisa menanam file itu lebih dulu → `gh auth login`/`xdg-open` sebagai root menjalankan script penyerang.

## Kriteria Penerimaan
- [ ] Script dibuat di home user (mis. `~/.local/bin`, umask 077).
- [ ] Export hanya ditambahkan bila file berhasil ditulis dan dimiliki user tersebut.
