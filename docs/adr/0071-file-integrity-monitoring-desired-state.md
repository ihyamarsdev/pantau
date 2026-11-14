# 0071. Pemantauan Integritas Berkas (File Integrity Monitoring) sebagai Aturan Desired State

Menambahkan jenis aturan Desired State baru, `file_integrity`, yang membandingkan sidik jari (sha256) sekumpulan berkas pada host remote dengan baseline yang telah disetujui operator, dan melaporkan `drift` bila ada berkas yang berubah, hilang, atau ditambahkan.

## Konteks dan Masalah

1. [ADR 0025](0025-desired-state-rule-taxonomy-and-evaluators.md) membakukan tujuh jenis aturan Desired State (`container`, `service`, `port`, `disk`, `cron`, `backup`, `process`) plus `http`, `tls`, dan `security_updates`. Taksonomi itu/status proses host, port yang listening, kapasitas disk, crontab, dan kesegaran cadangan; **tidak ada** yang memverifikasi isi berkas konfigurasi penting. Perubahan pada `/etc/passwd`, `/etc/sudoers`, atau `/etc/ssh/sshd_config` — penyebab klasik insiden — tidak pernah terdeteksi.
2. Pemantauan integritas berkas biasanya berarti memasang agent (AIDE, Tripwire, Wazuh). Prinsip Pantau adalah *agentless*: tidak boleh memasang apa pun pada host remote.
3. Baseline harus berasal dari **kondisi host saat operator conscious**, bukan diambil diam-diam saat evaluasi pertama. Jika baseline diambil otomatis, setiap penyimpangan pertama (termasuk perubahan yang sudah berjalan lama sebelum Pantau dipakai) akan lolos sebagai "aman", dan setiap instalasi baru Pantau akan owes baseline semu.
4. Cakupan harus jujur: bila ada berkas yang tidak bisa dibaca (izin, `sudo -n` tidak tersedia), hasil **bukan** "ok". Menampilkan "sesuai" padahal ada berkas yang tidak diperiksa melanggar prinsip *honest coverage* Pantau.

## Keputusan Arsitektur

1. **Jenis aturan baru `file_integrity`.**
   - `target`: (a) daftar path absolut yang dipisahkan koma atau baris baru, maksimum 20 path, tanpa glob dan tanpa `..`; (b) preset `preset:auth` (`/etc/passwd`, `/etc/group`, `/etc/sudoers`, `/etc/ssh/sshd_config`, `/root/.ssh/authorized_keys`, plus `/etc/sudoers.d/*` dan `/etc/ssh/sshd_config.d/*`); (c) target khusus `suid` (daftar biner SUID/SGID dari `find / -xdev -type f \( -perm -4000 -o -perm -2000 \)`, dibatasi timeout 90 detik). Semua nilai yang disisipkan ke perintah shell remote di-*quote* dengan `shellQuote`, dan `ValidateFIMTarget` menolak karakter kontrol, metakarakter shell, path relatif, dan traversal.
   - `expected`: baseline yang telah disetujui, dengan format `sha256:<digest>|m:<path>=<hash>,<path>=<hash>,…`. Digest dihitung atas manifes kanonik (terurut berdasarkan path), sehingga manipulasi manifes ditolak saat parsing. Manifes ikut tersimpan di kolom `expected` yang sudah ada, jadi **tetap ikut dalam snapshot** bersama aturan (baseline adalah konfigurasi, bukan hasil scan) dan **tidak** memerlukan tabel atau kolom baru.
   - Nilai khusus `baseline` (atau kosong) **ditolak**. Baseline hanya boleh disimpan lewat aksi eksplisit "Ambil baseline" (lihat butir 3), sehingga operator secara sadar menerima kondisi host saat itu sebagai keadaan yang desirable.
2. **Evaluator** (`internal/inspector/fim.go`, kasus `file_integrity` di `evaluateRule`). Satu perintah SSH read-only (`sha256sum`, dengan fallback `shasum -a 256` untuk macOS) terhadap seluruh target, menghasilkan manifes `path=sha256`. Fallback `sudo -n` hanya dicoba bila pembacaan biasa gagal **dan** probe sudo mengizinkan (`sudoProbeAllowed`/`noteSudo`, sesuai [ADR 0037](0037-elevated-file-manager-access.md)); `sudo -n` tidak pernah meminta kata sandi. Perbandingan terhadap baseline menghasilkan `ok` atau `drift`; `details`/`rootCause` menyebut path yang **diubah**, **hilang**, dan **ditambahkan** (daftar dibatasi 20 entri). Berkas yang ada dalam baseline tetapi hilang → `drift`. Berkas yang tidak bisa dibaca → status `not_assessed` (bukan `ok`), dengan nama path yang tidak terbaca disebutkan. Host tanpa `sha256sum`/`shasum`, atau perintah yang timeout, juga `not_assessed`.
3. **API.**
   - `POST /api/hosts/{id}/rules/fim/baseline` dengan body `{"target": "..."}` menjalankan perintah manifes dan **mengembalikan pratinjau tanpa menyimpan apa pun**: `expected` (baseline yang siap disalin), daftar `files` (path + hash), serta `missing`/`denied`. Pratinjau ini hanya dapat diterima melalui tombol konfirmasi di UI.
   - Penyimpanan tetap memakai jalur aturan yang sudah ada: `POST /api/hosts/{id}/rules` (dengan validasi `file_integrity` yang sama seperti jenis lain). Dengan begitu penyimpanan aturan, snapshot, debounce alert, dan notifikasi drift tetap di satu tempat.
   - `GET /api/hosts/{id}/rules/fim/diff?rule_id=<id>` membandingkan state host saat ini dengan baseline yang tersimpan (opsional; berguna untuk melihat detail tanpa menunggu inspeksi).
4. **UI.** Bundel baru `static/app/fim/` (`/app/fim.js`, namespace `FIM`, i18n EN/ID) yang menyisipkan panel FIM ke dalam dialog aturan Desired State yang sudah ada: pilihan preset, textarea path, tombol **Ambil baseline** yang menampilkan pratinjau (path + hash singkat) dan meminta konfirmasi sebelum dipakai, serta daftar berkas yang berubah pada tampilan drift. Kolom Expected pada tabel aturan diringkas menjadi "Baseline: N berkas" untuk jenis `file_integrity`, dan status `not_assessed` memakai badge tersendiri, bukan "Drift".
5. **Konvensi bash.** Perintah manifes memakai pola `FIM:OK <path> <hash>`, `FIM:MISSING <path>`, `FIM:DENIED <path>`, `FIM:NO_HASH_TOOL`, `FIM:SUDO_DENIED` agar parsing deterministik dan bebas dari output localize.

## Konsekuensi

- Operator dapat memantau integritas berkas penting (kredensial, konfigurasi SSH, biner SUID) tanpa memasang agent apa pun pada host, cukup satu perintah read-only per evaluasi.
- manifes baseline tersimpan di kolom `expected` aturan. manifes panjang (misalnya `suid` pada host besar) membuat baris `desired_rules` besar; batasnya 500 entri dan 90 detik per evaluasi, dan تجاوزnya menghasilkan `not_assessed`, bukan drift palsu.
- File yang tidak bisa dibaca menghasilkan `not_assessed`, bukan `ok`, sehingga drift yang sebenarnya masih tertutup tidak pernah ditampilkan sebagai "aman" — namun aturan seperti ini tidak akan pernah berstatus `ok` sampai seluruh target dapat dibaca.
- `suid` hanya melihat satu filesystem (`-xdev`); biner SUID pada mount terpisah tidak masuk manifes.
- Perubahan nilai `expected` secara manual di luar UI akan ditolak server (digest tidak cocok atau format salah); satu-satunya cara meng baselines ulang adalah aksi "Ambil baseline" di UI.
- Pemeriksaan integritas basis data internal (`data/`, `pantau`) tidak termasuk cakupan fitur ini; hanya berkas yang dipilih operator yang dipantau.
