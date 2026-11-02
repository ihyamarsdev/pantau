# Spec: Ops & Monitoring (prioritas menengah)

Status: implemented (ADR 0046); live-verified except real package upgrade execution
Dasar: [ADR 0025](../../docs/adr/0025-desired-state-rule-taxonomy-and-evaluators.md), [ADR 0036](../../docs/adr/0036-agentless-security-scan.md), [ADR 0042](../../docs/adr/0042-docker-management-portainer-style.md) (Privilege Fallback), [ADR 0044](../../docs/adr/0044-kubernetes-cluster-identity-usage-metrics-and-action-safety.md), CONTEXT.md.

Lima fitur dari daftar saran Pantau: (A) kanal notifikasi tambahan + debounce, (B) rule `http` dan `tls`, (C) perintah massal ke beberapa Host, (D) aksi pembaruan paket, (E) verifikasi isi backup. Semua agentless lewat SSH, tanpa dependensi baru (stdlib saja: `net/smtp`, `net/http`, `crypto/tls`).

## A. Kanal notifikasi + debounce

Pengaturan datar di `/api/settings` (pola yang ada; `internal/notify`, `handleSettings`, `handleTestNotify`):
- `ntfy_url`, `ntfy_token` (rahasia, opsional), `slack_webhook_url`, `discord_webhook_url`.
- SMTP: `smtp_host`, `smtp_port` (bawaan 587), `smtp_user`, `smtp_pass` (rahasia), `smtp_from`, `smtp_to` (daftar dipisah koma), `smtp_security` = `starttls` (bawaan) | `tls` | `none`.
- `alert_debounce_runs` (1..10, bawaan 1): Drift baru memicu Incident, Alert, dan notifikasi hanya setelah terlihat pada N Inspection berturut-turut untuk rule yang sama; kembali sehat langsung menutup (Alert Auto-Resolution tidak berubah). Penghitung in-memory per (host, rule) boleh hilang saat restart.
- Rahasia (`ntfy_token`, `smtp_pass`): GET **tidak pernah** mengembalikan nilainya, hanya `ntfy_token_set` / `smtp_pass_set` (bool). POST: kunci tidak dikirim = pertahankan, string kosong = hapus. Tambahkan ke Redaction Filter logger bila belum tercakup. Nilai Telegram yang sudah ada tidak diubah perilakunya.
- URL (`ntfy_url`, Slack, Discord): wajib `http(s)://`, ditolak (400) selain itu.
- `POST /api/settings/test-notify`: kirim ke SEMUA kanal terkonfigurasi dan kembalikan `{ok, error?, results:[{channel, ok, error?}]}` (kompatibel mundur: `ok` tetap ada; `ok` = semua kanal berhasil, dan galat bila tidak ada kanal sama sekali).
- Pengiriman: Slack `{"text":…}`, Discord `{"content":…}` (potong ≤1800 karakter), ntfy `POST url` isi teks dengan header `Title`, `Priority: high`, `Tags: rotating_light` (+ `Authorization: Bearer` bila token), SMTP teks biasa dengan Subject `[Pantau] <host>: <ringkasan>`. Timeout HTTP/SMTP 10 dtk, galat dicatat ke log (tanpa rahasia) dan tidak menghalangi kanal lain. Isi pesan memakai sumber yang sama dengan Telegram (host, ringkasan, Root Cause Excerpt terpotong).

## B. Rule `http` dan `tls` (Desired Rule Kind baru)

Dijalankan **dari Host lewat SSH** (perspektif Host, sama seperti Network Egress), bukan dari server Pantau (hindari SSRF dari server). Tanpa migrasi DB (`kind/target/expected` adalah `TEXT`). Dievaluasi di `evaluateRule` (`internal/inspector/inspector.go`); letakkan logika di file baru `internal/inspector/rules_net.go` dengan fungsi murni yang mudah diuji, dan hanya tambahkan `case` satu baris di switch.
- `http`: Target = URL `^https?://[^\s'"`$\\;&|<>]{1,512}$`. Expected = token dipisah koma: status (`200`, `2xx`, `3xx`) dan opsional batas latensi `<1500ms`; bawaan `2xx`. Perintah: `curl -sS -o /dev/null -m 10 -w '%{http_code} %{time_total}' -- URL` (tanpa `-k`; galat TLS/koneksi dilaporkan sebagai Drift dengan pesan curl). `curl` tidak ada → Drift jelas ("curl not found on host"). Current contoh: `200 in 143ms`.
- `tls`: Target = `host[:port]` (port bawaan 443) atau `https://host[:port]`; host `^[A-Za-z0-9.-]{1,253}$`. Expected = `>=Nd` (sisa hari, bawaan `>=14d`). Perintah: `openssl s_client -connect H:P -servername H </dev/null 2>/dev/null | openssl x509 -noout -enddate` (dengan `timeout 10` bila tersedia); sisa hari dihitung di Go dari `notAfter`. Hanya masa berlaku (bukan validasi rantai). `openssl` tidak ada atau handshake gagal → Drift dengan sebab. Current contoh: `42d left (expires 2027-01-02)`.
- Validasi sisi server saat menyimpan rule (`server.go`, handler rules): pola Target per kind di atas, Expected dapat diurai; selain itu 400. Kind lain tidak berubah.
- Root Cause Excerpt: keluaran curl/openssl terpotong. Opsi di form rule: tambahkan kind ke `<select id="ruleKind">` dan `ruleKindHints`.

## C. Perintah massal

`POST /api/exec/bulk` (auth biasa) body `{host_ids:[int] (1..20), command:string (1..4096 byte, tanpa NUL), timeout_sec:int (bawaan 30, maks 120), sudo:bool}`.
- Respons **NDJSON stream** (`application/x-ndjson`, `http.Flusher`): satu baris per Host begitu selesai `{host_id, host_name, ok, exit_code, output, truncated, duration_ms, error?}` dan baris akhir `{done:true, total, failed}`. Konkurensi 5. `output` maks 64 KB per Host (`truncated:true` bila dipotong).
- Non-interaktif (tanpa TTY): `sh -c <command>` dengan `shellQuote`, `DarwinPathPrelude` untuk macOS. `sudo:true` → hanya `sudo -n` (tidak pernah kata sandi); bila sudo butuh kata sandi, galat per Host.
- Konsisten dengan Kube Mode: Host berkebijakan `no-exec`/`read-only` melewati perintah yang cocok `isKubeExecCommand` (galat per Host "blocked by Kube Mode"). Host tak dikenal/ID ganda ditolak 400.
- Dicatat ke Activity Console (modul `exec`: jumlah Host, 200 karakter pertama perintah, hasil ringkas) lewat logger yang ada (Redaction Filter berlaku).
- Tidak ada penyimpanan riwayat. Penutupan koneksi klien membatalkan Host yang belum mulai.

## D. Pembaruan paket

Mengikuti Docker Privilege Fallback: berjalan sebagai akun SSH; bila bukan root pakai `sudo -n` (tidak pernah kata sandi); tanpa sudo → galat jelas.
- `GET /api/hosts/{id}/packages/upgrade-preview` → `{ok, supported, manager:"apt"|"dnf"|"yum", pending_total, pending_security, packages:[{name, from?, to?, security}] (maks 200), reboot_required, note?}`; memakai ulang parser/skrip pemindaian keamanan yang ada (`security.go`: `parseAptUpdates`, dst.) bila memadai. macOS atau pengelola paket tak dikenal → `supported:false`.
- `GET /ws/pkg/upgrade?host_id=&mode=security|all` (WebSocket, auth biasa; bingkai biner seperti `/ws/docker/logs`): apt → hanya `all` (`mode=security` ditolak dengan pesan: apt tidak punya mode keamanan-saja yang andal); dnf/yum → `all` atau `--security`. Perintah noninteraktif: apt `DEBIAN_FRONTEND=noninteractive apt-get update && apt-get -y -o Dpkg::Options::=--force-confold upgrade`; dnf/yum `-y upgrade [--security]`; dibungkus `timeout 900`. Satu proses per Host (guard in-flight seperti `packageInFlight`; yang kedua ditolak). Baris akhir `[pantau] exit=N`. Setelah selesai, picu `ScanSecurity` dan `ScanPackages` best-effort di latar belakang dan catat ke Activity Console (modul `packages`). **Tidak ada reboot otomatis.**
- Tidak ada rollback; UI harus menyatakannya.

## E. Verifikasi isi backup

Memperluas rule `backup` yang ada: `Expected = fresh_24h[,verify=<gzip|tar|zstd|xz|sha256>]`.
- Berkas yang diverifikasi = terbaru yang cocok dengan Target. Catatan: kode saat ini men-`shellQuote` Target sehingga pola glob (`db-*.sql.gz`) tidak pernah terekspansi; perbaiki dengan memilih berkas terbaru lewat `ls -t -- PATTERN | head -n 1` setelah memvalidasi pola dengan ketat (`^[A-Za-z0-9_./*?+@%:,=~-]+$`, mutlak, tanpa `..`).
- Verifikasi: `gzip -t`, `tar -tf` (tar/tgz/tar.gz), `zstd -t`, `xz -t`, `sha256` = `sha256sum -c` terhadap berkas sampingan `<berkas>.sha256` (bila tidak ada → Drift "no checksum file"). Alat tak tersedia → Drift jelas. Verifikasi bersifat baca-saja, dengan timeout (60 dtk) dan hanya dijalankan bila berkas ada dan tidak kosong dan segar. Sebab Drift memuat keluaran verifikasi terpotong. Current contoh: `fresh (3.2h ago, 1.2 MB), gzip ok`.
- Rule `backup` lama tanpa `verify` berperilaku persis seperti sekarang (selain perbaikan glob).

## UI (satu agen, `internal/web/static/index.html` + panduan pengguna)

- **Settings → notifikasi**: bagian ntfy, Slack, Discord, SMTP, `alert_debounce_runs`; kolom rahasia menampilkan "tersimpan" (bukan nilainya) dan menghapus saat dikosongkan secara eksplisit; tombol Test menampilkan hasil per kanal.
- **Form rule**: kind `http` dan `tls` (+ petunjuk target/expected), petunjuk `verify=` pada `backup`.
- **Perintah massal**: tombol di header dashboard membuka Action Dialog: pilih Host (kotak cari, pilih per Host Group, pilih semua; maks 20), textarea perintah, timeout, checkbox sudo; *Run* → `showConfirm` (danger) menyebut jumlah Host dan perintah; hasil mengalir ke tabel per Host (badge status, exit code, durasi, keluaran dapat dibuka, salin semua).
- **Pembaruan paket**: tombol *Upgrade packages* di Host Detail (tab Packages/Security) → modal pratinjau (jumlah total/keamanan, daftar, reboot required), pilih mode (`security` hanya untuk dnf/yum), `showConfirm` (peringatan: layanan dapat restart, tanpa rollback, pertimbangkan Snapshot) → modal log streaming (pola log Docker/Kubernetes yang ada) dengan exit code akhir; tombol dinonaktifkan untuk Host tak didukung.
- Anggaran ukuran: `TestIndexGzipAndETagRevalidation` membatasi gzip `index.html` 220000 byte dan saat ini ±219,6 KB. Fitur ini pasti melampauinya: naikkan batas **sekali** ke 260000 dan jelaskan (tulis nilai sebelum/sesudah). Gunakan ulang komponen yang ada, jangan duplikasi.
- i18n EN|ID semua string; bagian ringkas di `docs/user-guide.md` dan `docs/user-guide.id.md`.

## Di luar cakupan

Eskalasi/penjadwalan alert, templat pesan kustom, pemantauan HTTP dari server Pantau, pemeriksaan rantai sertifikat, penjadwalan perintah massal, riwayat perintah massal, reboot otomatis setelah upgrade, rollback paket, `brew`/macOS upgrade, verifikasi pemulihan basis data penuh.

## Aturan pengujian

Agen mengimplementasikan dan menulis unit test (`go test`), tetapi **tidak** melakukan verifikasi langsung (browser, klaster, SSH ke Host nyata, SMTP/webhook nyata): itu dikerjakan lead setelahnya.
