# 0040. Pemilik Proses pada Listening Port dan Tab Layanan systemd

Menampilkan proses pemilik setiap listening port pada tabel Port Exposure (tab Security) dan menyediakan tab **Services** terpisah di Host Detail yang mendaftar seluruh unit layanan systemd, tetap secara agentless dan tanpa pernah meminta atau menyimpan kata sandi.

## Konteks dan Masalah

1. **Pemilik port tidak terlihat**: `ss -tlpn` / `netstat -tlpn` oleh akun SSH non-root hanya menampilkan proses milik akun itu sendiri. Soket milik `root` atau pengguna layanan lain (nginx, postgres, sshd) tampil tanpa nama sehingga administrator tidak tahu layanan apa yang membuka port tersebut. Kolom `process` pada `ListeningPort` sudah ada tetapi tidak pernah diisi dengan nama yang dapat dibaca dan tidak ditampilkan.
2. **macOS tanpa pemilik**: batch metrik Darwin memakai `netstat` yang tidak melaporkan proses pemilik.
3. **Tidak ada pandangan layanan systemd**: tab Docker hanya mencakup container; layanan host (nginx, postgresql, unit yang gagal) hanya dapat dilihat lewat terminal.

## Keputusan Arsitektur

1. **Urutan resolusi pemilik port** (`internal/inspector/listening.go`), hanya dijalankan bila batch utama menyisakan port tanpa nama:
   1. Pemilik yang terlihat oleh akun SSH (`ss -p` → `users:(("nginx",pid=…))`, `netstat -p` → `pid/nama`; kolom ke-7 `netstat` kini ikut dicetak oleh awk pada batch Linux).
   2. `sudo -n` tanpa kata sandi (prinsip yang sama dengan [ADR 0037](0037-elevated-file-manager-access.md)). Bila `id -u` = 0 langkah ini dilewati karena batch utama sudah berjalan sebagai root. Bila sudo ditolak, host tidak diprobe lagi selama **24 jam** (status di memori per Host) sehingga paling banyak satu percobaan sudo gagal per hari tercatat pada log autentikasi host. Tidak ada kata sandi yang dikirim atau disimpan.
   3. **Petunjuk nomor port umum** (`ssh`, `mysql`, `redis`, …) sebagai pilihan terakhir. Field baru `process_source` membedakan `process` (pemilik nyata) dari `port_hint` (tebakan); UI menampilkan tebakan dengan huruf miring dan label "guessed from port", dan port yang tidak dikenal tetap `—` (tidak pernah mengarang nama).
2. **macOS**: skrip yang sama memakai `lsof +c 0 -nP -iTCP -sTCP:LISTEN` (akun sendiri, root, atau `sudo -n`).
3. **Skrip berjalan di bawah `sh -c`** (di-quote) agar tidak bergantung pada login shell (fish/csh/zsh) host.
4. **Tab Services (`GET /api/hosts/{id}/systemd`)**: `systemctl list-units --type=service --all` digabung dengan `list-unit-files` (status enabled; instance template `nama@x.service` mewarisi status `nama@.service`). Hanya baca dan tidak butuh sudo. Host tanpa systemd (macOS, OpenRC, SysV, `/run/systemd/system` tidak ada) menghasilkan `supported: false` dan UI menampilkan penjelasan. Urutan: failed, activating, active, lalu inactive.
5. **Tidak ada aksi start/stop/restart** pada tab ini; tab sengaja hanya pengamatan.

## Konsekuensi

- Nama proses lengkap untuk semua port bila SSH user root atau memiliki sudo tanpa kata sandi; bila tidak, hanya proses milik akun SSH yang bernama dan sisanya berupa tebakan berlabel atau `—`.
- Satu perintah SSH tambahan per inspeksi hanya saat ada port tanpa pemilik.
- Nama yang ditampilkan adalah nama proses, bukan unit systemd; hubungan port ke unit tidak dibuat.
- Data lama tersimpan apa adanya sampai inspeksi berikutnya (UI menormalkan kolom `ss` mentah).
