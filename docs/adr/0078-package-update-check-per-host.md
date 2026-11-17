# 0078. Pemeriksaan Pembaruan Paket (Package Update Check) per Host

Menambahkan **Pemeriksaan Pembaruan Paket** (*Package Update Check*): pemeriksaan read-only on-demand yang menanyakan ke manajer paket host berapa paket terpasang yang punya versi lebih baru, menampilkan daftar paket beserta versi terpasang dan versi yang tersedia, dan menandai paket yang pembaruan keamanannya.ESULT ini tidak memasang apa pun. Sekaligus menutup dua celah: host Alpine dan Arch sama sekali tidak bisa melaporkan pembaruan tertunda, dan operator tidak pernah bisa melihat "paket ini punya versi baru" tanpa membuka pratinjau Upgrade.

## Konteks dan Masalah

1. **Alpine dan Arch tidak dilaporkan sama sekali.** Skrip deteksi `UpgradePreviewDetectionScript` (`internal/inspector/pkgupgrade.go`) hanya mengimplementasikan `apt`, `dnf`, dan `yum`. Untuk `apk` dan `pacman` skrip hanya mencetak `MGR:apk` / `MGR:pacman`, sehingga `ParseUpgradePreview` mengembalikan `supported: false` dengan nota "package manager is not supported for upgrade". Padahal kedua manajer paket dapat melaporkan daftar pembaruan secara read-only dalam satu perintah (`apk version -l "<"`, `pacman -Qu`). Akibatnya operator di host Alpine/Arch tidak pernah tahu ada paket tertunda, dan `BuildUpgradeCommand` juga menolak kedua manajer tersebut.
2. **"Paket punya versi baru" tidak pernah terlihat per paket.** `GET /api/hosts/{id}/packages/upgrade-preview` mengembalikan daftar paket tertunda, tetapi itu hanya dipakai di modal pratinjau Upgrade yang muncul tepat sebelum menjalankan `apt-get upgrade`. Di tab Packages, inventaris hanya menampilkan versi terpasang; tidak ada kolom yang menunjukkan versi yang tersedia. Pemeriksaan pembaruan yang *hanya bisa dilihat sebagai dialog pratinjau upgrade* tidak bisa dipakai sebagai alat Pemantauan harian.
3. **Hasil pemeriksaan tidak tersimpan.** Setiap pemeriksaan membuka koneksi SSH baru. Tidak ada hasil yang bisa dilihat lagi, tidak ada usia data, dan tidak ada cooldown yang mencegah spam pemeriksaan.
4. **Cache metadata bisa menipu.** Manajer paket bekerja dari metadata lokal yang disegarkan terjadwal (`apt update`, `dnf makecache`). Kalau metadata host sudah lebih dari 7 hari, "0 pending" berarti "tidak ada pembaruan dalam metadata lama", bukan "host sudah terbaru". Melaporkan ini sebagai "up to date" adalah klaim palsu.
5. **Kegagalan manajer paket tidak boleh terlihat seperti.host bersih.** ADR 0046 sudah menetapkan aturan untuk dnf: `Error: ...` harus menjadi `ok:false`, bukan "0 pending". Jalur apt belum ikut aturan itu: `apt-get -s` yang gagal membaca daftar paket menghasilkan baris `E: ...` yang hanya jadi catatan, sementara hasil tetap `ok: true` dengan 0 paket tertunda.
6. **Istilah.** "Update Checker" di [CONTEXT.md](../../CONTEXT.md) sudah dipakai untuk deteksi rilis Pantau sendiri, dan "Image Update Check" untuk digest image Docker. Keduanya tidak boleh dipakai untuk pembaruan paket sistem.

## Keputusan Arsitektur

1. **Satu istilah: *Package Update Check*.** Definisi glossary di [CONTEXT.md](../../CONTEXT.md): pemeriksaan read-only on-demand atas versi paket terpasang pada sebuah Host yang menghasilkan daftar paket yang punya versi lebih baru beserta versi terpasang dan versi yang tersedia; tidak memasang, tidak mengubah, dan tidak me-restart apa pun. _Avoid_: Image Update Check, Update Checker, package upgrade.

2. **Satu skrip deteksi, dua manager baru.** `UpgradePreviewDetectionScript` diperluas dengan dua cabang (`internal/inspector/pkgupgrade.go`):
   - `apk`: `apk version -l "<"` menghasilkan tabel `Installed: Available:` lalu baris `<versi-terpasang> < <versi-tersedia>`. Nama paket dipisah dari sufiks versinya (`musl-1.2.5-r3` → `musl`).
   - `pacman`: `pacman -Qu` menghasilkan baris `<nama> <terpasang> -> <tersedia>`, kadang dengan akhiran `[ignored]`. Paket yang `[ignored]` adalah *hold* yang memang sengaja dikunci administrator, jadi **bukan** pembaruan tertunda dan tidak dihitung.
   Kedua manajer tidak memisahkan pembaruan keamanan dari pembaruan biasa, jadi setiap paket ditanpa penanda keamanan, dan nota检查 itu menyatakan hal tersebut secara terbuka. Keduanya tidak mendukung mode "hanya keamanan" (`--security`), jadi `BuildUpgradeCommand` menolak mode itu dengan pesan jelas — sama seperti apt.

3. **Usia cache metadata.** Skrip menambahkan seksi `===CACHE_AGE===` (jam `date +%s` + stempel `stat -c %Y` dari `/var/lib/apt/periodic/update-success-stamp`, `/var/cache/dnf`, `/var/cache/yum`, `/var/cache/apk`, `/var/cache/pacman/pkg`). `parseRepoCacheAge` (`internal/inspector/security.go`) dikecilkan untuk membaca keempat sumber itu; hasilnya masuk ke `UpgradePreview.RepoCacheAgeHours` (`-1` bila tidak diketahui). Cache yang lebih tua dari 168 jam (7 hari) menghasilkan catatan eksplisit, karena hasil daftar bisa misses rilis yang lebih baru.

4. **Jujur saat manajer paket gagal.** Jalur apt sekarang mengikuti aturan dnf yang sudah ada di ADR 0046: baris `E: ...` atau `Permission denied` membuat hasil `ok: false` dengan pesan aslinya, bukan "0 pending". Preview dnf yang sudah demikian tidak berubah.

5. **Penyimpanan hasil (`package_update_checks`).** Tabel baru di `pantau.db`:
   ```sql
   CREATE TABLE IF NOT EXISTS package_update_checks (
       host_id INTEGER PRIMARY KEY REFERENCES hosts(id) ON DELETE CASCADE,
       checked_at DATETIME NOT NULL,
       supported INTEGER NOT NULL DEFAULT 0,
       manager TEXT NOT NULL DEFAULT '',
       pending_total INTEGER NOT NULL DEFAULT 0,
       pending_security INTEGER NOT NULL DEFAULT 0,
       reboot_required INTEGER NOT NULL DEFAULT 0,
       repo_cache_age_hours INTEGER NOT NULL DEFAULT -1,
       packages_json TEXT NOT NULL DEFAULT '[]',
       note TEXT NOT NULL DEFAULT '',
       error TEXT NOT NULL DEFAULT ''
   );
   ```
   Satu baris per host (hasil pemeriksaan terbaru), dibuat lewat `CREATE TABLE IF NOT EXISTS` di `migrate()` dan bersifat aditif. Tabel ini berisi **telemetri turunan** (dapat diproduksi ulang dengan pemeriksaan ulang) sehingga **tidak disertakan dalam snapshot terenkripsi**, sama seperti `security_scans`.

6. **REST API.**
   - `GET /api/hosts/{id}/packages/updates`: hasil pemeriksaan tersimpan, atau `{"checked": false}` bila host belum pernah diperiksa. Menyertakan header `X-Cooldown-Remaining` bila jeda masih aktif.
   - `POST /api/hosts/{id}/packages/updates/check`: menjalankan pemeriksaan sinkron (batas 60 detik) dengan jeda 60 detik (HTTP 429) dan kunci konkurensi per host (HTTP 409), seperti pemindaian keamanan dan pemindaian paket.

7. **UI (tab Packages).** Tombol **Check for updates** di toolbar tab Packages menjalankan pemeriksaan; hasilnya muncul sebagai kartu di bawah tabel inventaris: jumlah paket tertunda, jumlah pembaruan keamanan, penanda **reboot required**, waktu pemeriksaan terakhir, dan tabel paket (nama, versi terpasang, versi tersedia, jenis). Kolom **Available** baru pada tabel inventaris paket menampilkan versi yang tersedia untuk paket yang punya pembaruan. Hasil yang gagal ditampilkan apa adanya sebagai **Update check failed** beserta alasannya — bukan "up to date". Cache metadata yang lebih tua dari 7 hari ditampilkan sebagai catatan. Panel menyatakan dengan tegas bahwa pemeriksaan ini hanya membaca dan tidak memasang apa pun; menerapkannya tetap lewat tombol **Upgrade packages** yang sudah ada beserta konfirmasi danalog lognya.

8. **Batas yang disengaja.** Pemeriksaan ini **tidak** memasang, **tidak** memperbarui, dan **tidak** me-restart apa pun; itu tetap milik Package Upgrade. Pemeriksaan juga **tidak** mengirim apa pun ke luar (tetap *airgapped*). Ia tidak menambah jenis Desired Rule baru: aturan `security_updates` yang sudah ada tetap dievaluasi dari cache pemindaian keamanan, bukan dari pemeriksaan ini.

## Konsekuensi

- Host Alpine dan Arch akhirnya bisa melaporkan pembaruan tertunda; kedua manajer juga bisa menjalankan Package Upgrade.
- Operator dapat melihat paket yang punya versi baru langsung di tab Packages, tanpa membuka dialog pratinjau upgrade.
- Hasil pemeriksaan tersimpan, sehingga usia data terlihat dan tidak perlu koneksi SSH berulang untuk menampilkannya.
- Pemeriksaan yang gagal dan cache metadata yang basi dinyatakan terbuka, sehingga "0 pembaruan" tidak pernah berarti "aman" tanpa bukti.
- APK dan pacman tidak membedakan pembaruan keamanan; untuk kedua distro itu jumlah pembaruan keamanan pada panel ini tetap 0 dan nota menyatakan alasannya.
- Satu pemeriksaan per host adalah satu perintah SSH; pemeriksaan berkala otomatis untuk seluruh fleet belum ada (saat ini dipicu manual).
