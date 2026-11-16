# 0076. Sesi administrator yang bertahan restart, dan semboyan "sekali klik sudah masuk" pada Pintasan

## Konteks dan Masalah

1. Sesi administrator disimpan di memori (`Server.sessions`, `sync.Map`) dengan cookie `pantau_session` berumur 7 hari. Setiap restart proses — pembaruan mandiri, restart systemd, reboot mesin — mengosongkan peta itu, sehingga cookie yang masih dipegang browser menjadi tidak berarti dan operator harus mengetik kata sandi lagi. Pada instalasi yang memperbarui diri sendiri, ini terjadi beberapa kali sehari.
2. Halaman **Pintasan** (ADR sebelumnya di CHANGELOG, setting `shortcuts`) hanya berisi tautan. Panel admin di belakang pintasan tetap meminta login, sehingga "satu klik" berakhir pada formulir login situs lain.
3. 2FA (opsional) membuat auto-login berbasis kata sandi saja tidak mungkin: kode TOTP tetap diminta. Karena itu penyelesaian masalah 1 tidak boleh bergantung pada penyimpanan kata sandi.

## Keputusan Arsitektur

1. **Sesi disimpan di SQLite** (`auth_sessions`: `token`, `expires_at`) dan dimuat ke `sessions` saat `NewServer`. Tabel ini sengaja **tidak** termasuk tabel yang diekspor `ExportSnapshot`, jadi berkas backup (termasuk cadangan GitHub) tidak dapat dipakai melanjutkan sebuah sesi. Baris kedaluwarsa dibersihkan saat start (`LoadSessions`) dan saat token kedaluwarsa terlihat di `authMiddleware`.
2. **Umur sesi satu tahun** (`sessionTTL`, cookie `Expires` + `MaxAge`, tetap `HttpOnly` + `SameSite=Lax`). Logout menghapus baris di memori dan di SQLite (`dropSession`). Ini menjawab keluhan "setiap klik harus login" untuk Pantau sendiri, tanpa menyimpan kata sandi di klien dan tetap kompatibel dengan 2FA.
3. **Pintasan dapat menyimpan kredensial** (`login_user`, `login_pass`, `login_action`, `login_user_field`, `login_pass_field`, `login_extra`). Tile-nya menaut ke `/shortcut-login/<id>` — halaman Pantau (butuh sesi) yang memuat formulir tersembunyi dan mengirimkannya sendiri ke formulir login situs tujuan sebagai navigasi POST lintas-origin biasa, lalu situs mengalihkan seperti biasanya. Halaman itu `Cache-Control: no-store`, `Referrer-Policy: no-referrer`, dan tidak pernah memuat kredensial lewat JavaScript: nilainya ada di DOM halaman sekali pakai.
4. **Kata sandi hanya bisa ditulis** (`login_pass`), browser hanya menerima `login_pass_set`. Karena itu `PUT` dengan `login_pass` kosong **mempertahankan** kata sandi tersimpan untuk `id` yang sama — dengan syarat alamat pintasan tidak berubah, agar pintasan yang dipindah ke situs lain tidak membawa kredensial situs lama. `login_clear` menghapusnya secara eksplisit. Setiap pintasan mendapat `id` heksadesimal 16 karakter yang stabil sebagai pegangan tautan.
5. **Kredensial mengikuti aturan data rahasia yang sudah ada**: tersimpan plaintext di SQLite bersama kata sandi SSH key, token Cloudflare, dan kunci S3 yang sudah diekspor System Snapshot; basis data dan sidecar-nya sudah 0600. Nama kolom formulir divalidasi `^[A-Za-z0-9_.\[\]]{1,64}$` dan semua nilai di-escape, sehingga kredensial tidak dapat menyuntik markup atau nama atribut.

## Batasan yang Disengaja

1. Ini **POST formulir biasa**. Situs yang login lewat JavaScript, API JSON (mis. `POST /login` dengan body JSON), token CSRF dinamis, atau langkah 2FA kedua tidak dapat dilayani; klik tetap membuka formulir login situs seperti sebelumnya. Dokumentasi pengguna menyebut batas ini.
2. Tidak ada penyimpanan kata sandi di sisi browser dan tidak ada integrasi password manager: satu sumber kebenaran (server), sama seperti daftar pintasan itu sendiri.
3. Alamat login harus `http(s)` absolut; alamat relatif ditolak agar tidak ambigu terhadap alamat pintasan.

## Alternatif yang Dipertimbangkan

1. **Autofill lewat ekstensi browser / password manager**: di luar kendali Pantau dan tidak dapat disiapkan dari UI Pantau.
2. **Script yang menulis ke cookie situs tujuan**: diblokir same-origin policy.
3. **Menyimpan kata sandi di localStorage**: tetap plaintext, tetapi per-browser, tidak ikut backup, dan tidak muncul di snapshot; ditolak karena memecah satu sumber kebenaran dan tidak menjawab "semua browser menampilkan pintasan yang sama".
4. **Memperpanjang cookie tanpa menyimpan sesi**: tidak menolong, karena restart tetap mengosongkan peta di memori.

## Verifikasi

- `internal/web/session_persist_test.go`: sesi bertahan pada `NewServer` kedua, token kedaluwarsa dibuang, logout menghapus baris.
- `internal/web/shortcuts_test.go`: default kredensial, penyamaran `login_pass`, pertahankan kata sandi saat nama diubah, buang saat alamat berubah, `login_clear`, penolakan alamat login/kolom/extra yang tidak sah, dan halaman auto-login meng-escape nilai.
- `e2e/tests/shortcuts-login.e2e.ts`: pintasan berkata sandi menghasilkan tile `/shortcut-login/<id>`, daftar API tidak memuat kata sandi, halaman itu benar-benar mem-POST `username`/`password` ke formulir situs, dan "Lupakan kata sandi tersimpan" mengembalikan tile menjadi tautan biasa.
