# 0080. Gateway CLI Update Check (Ansible Gate & Terraform Gate)

Menambahkan **Gateway CLI Update Check**: pemeriksaan read-only on-demand atas versi CLI yang terpasang di server Pantau untuk dua *gateway* — biner `ansible-playbook` di balik **Ansible Gate** dan biner `terraform`/`tofu` di balik **Terraform Gate** — yang membandingkan versi terpasang dengan versi rilis terbaru yang dipublikasikan upstream. Hasilnya hanya dibaca; tidak ada yang dipasang, diunduh, atau diubah.

## Konteks dan Masalah

1. **Versi terpasang tidak pernah dibandingkan dengan upstream.** Panel Ansible Gate (Ansible → Settings) menampilkan versi `ansible-playbook` yang terpasang; panel Terraform Gate menampilkan versi `terraform`/`tofu`. Keduanya hanya menampilkan apa yang terpasang, sehingga operator tidak pernah diberi tahu bahwa server Pantau menjalankan CLI yang sudah punya rilis baru — termasuk rilis yang menutup bug atau celah keamanan.

2. **Pola precedents sudah ada di repo.** `internal/updater` memeriksa rilis Pantau sendiri lewat GitHub Releases (ADR 0019) dan `internal/ansible/galaxy_api.go` memeriksa versi koleksi/role di Ansible Galaxy (Library → Check for updates). Keduanya adalah pola yang bisa diikuti persis: bandingkan, laporkan, jangan pasang.

3. **Biner yang dipakai bisa berbeda nama.** Terraform Gate menerima dua biner, `terraform` dan `tofu` (OpenTofu). Keduanya harus dicek, dan masing-masing punya kanal rilis berbeda.

4. **Sumber upstream harus ringan.** PyPI `pypi.org/pypi/ansible-core/json` berukuran ~540 KiB (117 KiB gzip) dan memuat seluruh riwayat rilis; GitHub `releases/latest` untuk OpenTofu ~330 KiB. Pemeriksaan yang menarik payload sebesar itu berulang-ulang boros.

5. **Kegagalan jaringan harus jujur, bukan "terbaru".** Di lingkungan airgapped permintaan ke upstream gagal. Melaporkan "Anda sudah terbaru" saat upstream tidak bisa dihubungi adalah klaim palsu — pola yang sama seperti Package Update Check (ADR 0078).

6. **Istilah.** "Update Checker" di [CONTEXT.md](../../CONTEXT.md) sudah dipakai untuk deteksi rilis Pantau sendiri, "Package Update Check" untuk pembaruan paket sistem (ADR 0078), dan "Image Update Check" untuk digest image Docker. Ketiganya tidak boleh dipakai untuk versi CLI gateway.

## Keputusan Arsitektur

1. **Satu istilah: *Gateway CLI Update Check*.** Definisi glossary di [CONTEXT.md](../../CONTEXT.md): pemeriksaan read-only on-demand atas versi CLI yang terpasang di server Pantau untuk sebuah *gateway* (Ansible atau Terraform), yang membandingkan versi terpasang dengan versi rilis terbaru yang dipublikasikan upstream dan melaporkan hasilnya — ada versi lebih baru, sudah terbaru, atau tidak dapat dibandingkan. _Avoid_: Update Checker, Self-Update, package update.

2. **Paket baru `internal/gwupdate`** (pustaka standar saja, tanpa akses DB), mengikuti preseden `internal/ansible/galaxy_api.go` dan `internal/updater`:
   - `LatestAnsible(ctx)`, `LatestTerraform(ctx)`, `LatestOpenTofu(ctx)` — masing-masing mengembalikan versi rilis terbaru dan URL rilis.
   - `Check(ctx, target, installed string) Result` — membandingkan versi terpasang dengan versi upstream.
   - `ParseVersion(s string) (Version, bool)` dan `Compare(a, b string) int` — semver sederhana (inti numerik + pre-release) dengan aturan yang sama seperti `internal/terraform/registry_constraint.go`, diulang di paket ini agar `gwupdate` tidak mengimpor `internal/terraform`.

3. **Status hasil: `update`, `current`, `unknown`, `error`.** Versi yang tidak bisa diparse menghasilkan `unknown`, **bukan** `update`: CLI yang melaporkan versi tak lazim harus diekstrak lebih dulu, bukan dianggap usang.

4. **Sumber rilis yang paling ringan per upstream:**

   | Target | Endpoint | Cara baca | Ukuran |
   |---|---|---|---|
   | `ansible` | `https://pypi.org/pypi/ansible-core/json` | `json.Decoder` streaming; `info` adalah objek pertama dan `version` field pertamanya, jadi decoder membaca `{` → key `"info"` → struct `{"version": string}` lalu **menutup body** | ~16 KiB dibaca dari body ~540 KiB |
   | `terraform` | `https://api.releases.hashicorp.com/v1/releases/terraform/latest` | JSON penuh, field `version` | ~2,9 KiB |
   | `tofu` | `https://github.com/opentofu/opentofu/releases/latest` | redirect HTTP 302 ke tag rilis; versi diambil dari path `Location` | header saja, tanpa body JSON |

   Client OpenTofu memakai `CheckRedirect` yang **tidak** mengikuti redirect, karena yang dibutuhkan justru `Location`-nya. Timeout 15 detik untuk semua permintaan.

5. **Ekstraksi versi terpasang di sisi pemanggil** (web handler), karena format keluaran tiap biner berbeda: `ansible-playbook --version` → `ansible-playbook [core 2.22.0]`, `terraform version` → `Terraform v1.15.9`, `tofu version` → `OpenTofu v1.13.9`.

6. **REST API.**
   - `GET /api/ansible/update-check` → hasil pemeriksaan `ansible`.
   - `GET /api/terraform/update-check` → hasil pemeriksaan untuk kedua biner yang terpasang (`terraform` dan `tofu`) dalam satu array.

   Kedua endpoint **tidak** memerlukan Gate aktif: pemeriksaan versi bersifat baca-only dan harus tetap bisa dipakai saat Gate mati — operator justru ingin tahu sebelum mengaktifkan. Biner yang tidak terpasang dijawab `unknown`, bukan 4xx. Kegagalan menghubungi upstream dijawab HTTP 200 dengan `status: "error"` beserta pesannya: kegagalan upstream bukan kegagalan Pantau, dan UI harus menampilkannya apa adanya (pola ADR 0078).

7. **UI.** Tombol **Check for updates** di panel Ansible Gate (Ansible → Settings) dan di panel "Terraform Gate & CLI Detection" (Terraform → Terraform Gate). Hasil menampilkan status (Update available / Up to date / Version unknown / Check failed), versi terpasang vs versi terbaru, tautan ke halaman rilis, dan waktu pemeriksaan. Setiap biner Terraform mendapat baris hasilnya sendiri. Kedua panel menyatakan bahwa pemeriksaan hanya membaca dan tidak memasang apa pun.

8. **Batas yang disengaja.** Tidak memasang, tidak mengunduh, tidak mengubah biner; pemasangan CLI tetap milik operator atau image Docker. Tidak ada pemeriksaan otomatis berkala — pemicunya manual, konsisten dengan Package Update Check (ADR 0078). Tidak ada endpoint yang menulis ke disk dan tidak ada jenis Desired Rule baru.

## Konsekuensi

- Operator melihat bahwa CLI gateway di server Pantau sudah usang, lengkap dengan versi dan tautan rilis, sebelum menjalankan plan/apply/playbook.
- Pemeriksaan tetap ringan: PyPI dibaca sebagian, OpenTofu tanpa body JSON, HashiCorp ~3 KiB.
- Kegagalan jaringan tampil apa adanya sebagai "Check failed" dan tidak pernah sebagai "up to date".
- CLI di luar jalur standar tetap bisa dicek selama versinya dapat diparse.
