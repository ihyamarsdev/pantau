# Gateway CLI Update Check (Ansible Gate + Terraform Gate)

Status: ready-for-agent

Pemeriksaan versi read-only atas CLI yang dipakai dua *gateway* Pantau — biner `ansible-playbook` di balik **Ansible Gate** dan biner `terraform` / `tofu` di balik **Terraform Gate** — untuk menunjukkan apakah versi yang terpasang di server Pantau sudah versi terbaru yangreleased upstream. Pemeriksaan ini **hanya membaca**; tidak memasang, tidak mengunduh, tidak mengubah biner apa pun.

## Istilah domain

**Gateway CLI Update Check**:
Pemeriksaan read-only on-demand atas versi CLI yang terpasang di server Pantau untuk sebuah *gateway* (Ansible atau Terraform), yang membandingkan versi terpasang dengan versi rilis terbaru yang dipublikasikan upstream dan melaporkan hasilnya: ada versi lebih baru, sudah terbaru, atau tidak dapat dibandingkan.
_Avoid_: Update Checker (dipakai untuk rilis Pantau sendiri), Self-Update, package update, gateway update

## Konteks dan Masalah

1. **Versi terpasang tidak pernah dibandingkan dengan upstream.** Panel **Ansible Gate** (Ansible → Settings) menampilkan versi `ansible-playbook` yang terpasang, dan panel **Terraform Gate** (Terraform → Terraform Gate) menampilkan versi `terraform`/`tofu`. Keduanya hanya menampilkan apa yang terpasang. Operator tidak pernah diberi tahu bahwa server Pantau menjalankan CLI yang sudah punya rilis baru — termasuk rilis yang menutup bug atau celah keamanan.
2. **Dua sumber kebenaran sudah ada di repo, tapi tidak untuk CLI ini.** `internal/updater` memeriksa rilis Pantau sendiri lewat GitHub Releases (ADR 0019), dan `internal/ansible/galaxy_api.go` memeriksa versi koleksi/role di Ansible Galaxy (Library → Check for updates). Keduanya adalah pola yang bisa diikuti persis: bandingkan, laporkan, jangan pasang.
3. **Biner yang dipakai bisa berbeda nama.** Terraform Gate menerima dua biner: `terraform` dan `tofu` (OpenTofu). Keduanya harus dicek, dan masing-masing punya kanal rilis berbeda.
4. **P-installasi sumber upstream harus ringan dan read-only.** PyPI `pypi.org/pypi/ansible-core/json` berukuran ~540 KiB (117 KiB gzip) dan memuat seluruh riwayat rilis. GitHub `releases/latest` memuat ~330 KiB untuk OpenTofu. Cek berkala tidak boleh menarikouerabytes itu berulang-ulang.
5. **Kegagalan jaringan harus jujur, bukan "terbaru".** Di lingkungan airgapped, permintaan ke upstream gagal. Melaporkan "Anda sudah terbaru" saat upstream tidak bisa dihubungi adalah klaim palsu (pola yang sama seperti Package Update Check di ADR 0078).
6. **Istilah.** "Update Checker" di [CONTEXT.md](../../CONTEXT.md) sudah dipakai untuk deteksi rilis Pantau sendiri; "Package Update Check" untuk pembaruan paket sistem (ADR 0078); "Image Update Check" untuk digest image Docker. Ketiganya tidak boleh dipakai untuk versi CLI gateway.

## Keputusan Arsitektur

### 1. Satu istilah: *Gateway CLI Update Check*

Definisi glossary di [CONTEXT.md](../../CONTEXT.md) ditambahkan; `_Avoid_`: Update Checker, Self-Update, package update.

### 2. Paket baru `internal/gwupdate` (stdlib saja, tanpa DB)

```go
// Target adalah satu CLI yang bisa dicek: "ansible", "terraform" atau "tofu".
const (
    TargetAnsible   = "ansible"
    TargetTerraform = "terraform"
    TargetOpenTofu  = "tofu"
)

// Source adalah sumber rilis upstream untuk satu target.
type Source struct {
    Name    string        // "ansible-core", "terraform", "opentofu"
    Latest  func(ctx context.Context) (string, error) // versi rilis terbaru
}

// Status hasil perbandingan.
const (
    StatusUpdate  = "update"   // ada versi lebih baru
    StatusCurrent = "current"  // sama dengan yang terpasang
    StatusUnknown = "unknown"  // tidak bisa dibandingkan (versi tak bisa diparse / CLI tak terpasang)
    StatusError   = "error"    // upstream gagal dihubungi
)

// Result adalah hasil pemeriksaan satu target.
type Result struct {
    Target   string `json:"target"`   // "ansible" | "terraform" | "tofu"
    Installed string `json:"installed"` // versi terpasang, "" bila tidak terpasang
    Latest   string `json:"latest"`    // versi rilis terbaru, "" bila tidak diketahui
    Status   string `json:"status"`
    Error    string `json:"error,omitempty"`
    ReleaseURL string `json:"release_url,omitempty"`
    CheckedAt time.Time `json:"checked_at"`
}
```

### 3. Sumber rilis: yang paling ringan per upstream

| Target | Endpoint | Bentuk | Ukuran |
|---|---|---|---|
| `ansible` | `https://pypi.org/pypi/ansible-core/json` | JSON, field `info.version` | decode streaming; ~16 KiB dibaca dari body ~540 KiB |
| `terraform` | `https://api.releases.hashicorp.com/v1/releases/terraform/latest` | JSON, field `version` | ~2,9 KiB |
| `tofu` | `https://github.com/opentofu/opentofu/releases/latest` | redirect 302 ke tag rilis | header saja, tanpa body JSON |

**PyPI tanpa menarik seluruh riwayat.** `pypi.org/pypi/ansible-core/json` menempatkan `info` sebagai objek pertama, dan `info.version` adalah field pertama di dalamnya. Decoder streaming (`json.Decoder`) membaca `{`, key `"info"`, lalu mendecode struct `{"version": string}` dan **menutup body** — sisa payload tidak pernah diunduh (yang terkompresi ~117 KiB tidak terpakai). Timeout 15 detik.

**HashiCorp** memakai API rilis resmi yang sama dengan yang dipakai `terraform version` sendiri.

**OpenTofu** memakai redirect GitHub `releases/latest`: HTTP 302 ke `https://github.com/opentofu/opentofu/releases/tag/v1.13.9`. Versi diambil dari path tag, tanpa mengunduh JSON rilis 330 KiB. Client memakai `CheckRedirect` yang **tidak** mengikuti redirect (kita butuh `Location`-nya).

### 4. Perbandingan versi

Semver sederhana: inti numerik (1..3 segmen) + pre-release opsional. Aturan:
- Numeric core dibandingkan per segmen; segmen yang hilang dihitung 0 (`1.2` == `1.2.0`).
- Rilis (tanpa pre-release) lebih baru daripada pre-release-nya sendiri (`1.0.0` > `1.0.0-rc1`).
- Pre-release dibandingkan sebagai teks (`rc1` < `rc2`).
- Versi yang tidak bisa diparse → `StatusUnknown`, **bukan** "update". CLI yang melaporkan versi tak lazim (mis. `ansible-playbook [core 2.22.0]`, `Terraform v1.15.9`) harus diekstrak dulu oleh pemanggil, bukan dianggap tak terrestrial.

Ekstraksi versi terpasang dilakukan di sisi pemanggil (web handler), karena format keluaran tiap biner berbeda:
- `ansible-playbook --version` → baris pertama `ansible-playbook [core 2.22.0]` → ambil angka setelah `core `.
- `terraform version` → baris pertama `Terraform v1.15.9` → ambil setelah `v`.
- `tofu version` → `OpenTofu v1.13.9` → ambil setelah `v`.

### 5. Status di API

```json
{
  "target": "terraform",
  "installed": "1.15.9",
  "latest": "1.17.2",
  "status": "update",
  "release_url": "https://github.com/hashicorp/terraform/releases/tag/v1.17.2",
  "checked_at": "2026-11-17T12:00:00Z"
}
```

### 6. REST API

- `GET /api/ansible/update-check` → hasil pemeriksaan `ansible` (dari `ansible.Available()`).
- `GET /api/terraform/update-check` → hasil pemeriksaan untuk **kedua** biner yang terpasang: `terraform` dan `tofu`, dalam satu array `{"results":[...]}`.

Kedua endpoint **tidak** memerlukan Gate aktif: pemeriksaan versi bersifat baca-only dan harus tetap bisa dipakai saat Gate mati (operator justru ingin tahu sebelum mengaktifkan). Status `unknown` (biner tidak terpasang) dijawab 200 dengan `error` yang menjelaskan, bukan 4xx.

Gagal menghubungi upstream → HTTP 200 dengan `status: "error"` dan pesan errornya. **Tidak pernah** 5xx: kegagalan upstream bukan kegagalan Pantau, dan UI harus bisa menampilkannya apa adanya (pola ADR 0078).

### 7. UI

**Ansible → Settings → kartu Ansible Gate**: tombol **Check for updates** di baris versi. Hasil ditampilkan di bawah: badge status (Update available / Up to date / Version unknown / Check failed), versi terpasang vs versi terbaru, tautan ke halaman rilis, dan waktu pemeriksaan.

**Terraform → Terraform Gate → kartu "Terraform Gate & CLI Detection"**: tombol **Check for updates** di panel status. Setiap biner (`terraform`, `tofu`) mendapat baris hasilnya sendiri; biner yang tidak terpasang tidak diikutkan.

Kedua panel menyatakan bahwa pemeriksaan **hanya membaca** dan tidak memasang apa pun.

### 8. Batas yang disengaja

- Tidak memasang, tidak mengunduh, tidak mengubah biner. Pemasangan CLI tetap di tangan operator (atau image Docker).
- Tidak ada pemeriksaan otomatis berkala; pemicunya manual (tombol). Ini konsisten dengan Package Update Check (ADR 0078) yang juga manual.
- Tidak ada endpoint yang menulis ke disk.
- Tidak menambah jenis Desired Rule baru.

## Konsekuensi

- Operator melihat bahwa CLI gateway di server Pantau sudah usang, lengkap dengan versi dan tautan rilis, sebelum menjalankan plan/apply/playbook.
- Pemeriksaan ringan: PyPI dibaca sebagian, OpenTofu tanpa body JSON, HashiCorp ~3 KiB.
- Kegagalan jaringan tampil apa adanya sebagai "Check failed", tidak pernah sebagai "up to date".
- CLI yang terpasang di luar jalur standar (biner custom) tetap bisa dicek selama versinya bisa diparse.
