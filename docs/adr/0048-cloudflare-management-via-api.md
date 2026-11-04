# 0048. Manajemen Cloudflare via REST API v4

Menambahkan pengelolaan akun Cloudflare langsung dari Pantau lewat Cloudflare REST API v4 resmi (`https://api.cloudflare.com/client/v4`): manajemen Zone, pengelolaan DNS Record, pembersihan cache (*Purge Cache*), dan konfigurasi zone (*Zone Settings*).

Melengkapi [ADR 0035](0035-ssh-key-library.md) (pola penyimpanan kredensial aman), [ADR 0006](0006-encrypted-system-snapshot-and-github-sync.md) (cadangan snapshot terenkripsi), dan [ADR 0047](0047-external-kubernetes-clusters-from-kubeconfig.md) (pengelolaan layanan infrastruktur eksternal).

## Konteks dan Masalah

1. Pengguna dan administrator infrastruktur yang mengelola armada server Linux dengan Pantau umumnya mengarahkan domain dan lalu lintas publik mereka melalui Cloudflare untuk kebutuhan DNS manajemen, CDN/caching, dan terminasi SSL/TLS.
2. Saat melakukan pemeliharaan server, migrasi host, penerapan sertifikat SSL, atau deployment rilis aplikasi baru, operator sering kali harus berpindah antar-tab untuk memperbarui DNS A/AAAA/CNAME record atau membersihkan cache (*purge cache*) di dashboard web Cloudflare.
3. Kebutuhan operasional ini membutuhkan integrasi terpusat pada dasbor Pantau tanpa membebani operator dengan keharusan mengonfigurasi perkakas baris perintah terpisah.
4. Di sisi lain, kredensial penyedia eksternal seperti token API Cloudflare merupakan aset sensitif dengan risiko tinggi jika terekspos atau disalahgunakan. Integrasi harus menjamin batas keamanan yang ketat dan menerapkan prinsip hak akses terendah (*least privilege*).

## Keputusan Arsitektur

1. **Klien REST API Mandiri Menggunakan Standar Library Go (`internal/cloudflare`)**:
   Pantau mengimplementasikan klien HTTP bawaan menggunakan paket standar `net/http` tanpa menambahkan dependensi pustaka pihak ketiga. Klien ini menangani paginasi otomatis (`per_page=50` untuk Zone, `per_page=100` untuk DNS Record), pembatasan timeout (20 detik), serta penanganan galat terstruktur berdasarkan format envelope Cloudflare API v4 `{success, errors, result}`.
2. **Penyimpanan Kredensial di SQLite Lokal**:
   - Akun Cloudflare disimpan pada tabel `cloudflare_accounts` di SQLite lokal (`id`, `name`, `account_id`, `api_token`, `created_at`).
   - Token API disimpan dalam format teks biasa (*plaintext*) di database lokal, setara dengan pola penyimpanan kunci privat SSH pada [ADR 0035](0035-ssh-key-library.md).
   - Struct Go menggunakan tag `json:"-"` untuk `APIToken`, dan **tidak ada endpoint API yang mengembalikan nilai token** setelah tersimpan. Antarmuka web hanya menampilkan nama dan ID akun, serta field sandi kosong jika pengguna ingin mengganti token.
3. **Pencadangan Terenkripsi via System Snapshot**:
   Seluruh rekaman akun Cloudflare (termasuk token) disertakan dalam pencadangan **System Snapshot** terenkripsi (AES-256-GCM / Argon2id, [ADR 0006](0006-encrypted-system-snapshot-and-github-sync.md)) dan otomatis dipulihkan saat proses impor snapshot dijalankan.
4. **Verifikasi Token Pra-Penyimpanan**:
   Sebelum akun disimpan atau diperbarui dengan token baru, Pantau melakukan panggilan verifikasi ke endpoint `/user/tokens/verify` (atau `/accounts/{id}/tokens/verify` jika Account ID disediakan). Jika token tidak valid, kadaluwarsa, atau tidak memiliki izin yang cukup, pembuatan/pembaruan akun ditolak dengan pesan kesalahan yang jelas.
5. **Daftar Putih Pengaturan Zone (*Allowlist Zone Settings*)**:
   Tidak semua pengaturan Cloudflare dibuka demi menjaga kestabilan dan keamanan. Pantau hanya mengizinkan modifikasi subset pengaturan yang aman dan bernilai operasional tinggi melalui `EditableSettings`:
   - `ssl`: `off`, `flexible`, `full`, `strict`
   - `always_use_https`: `on`, `off`
   - `security_level`: `off`, `essentially_off`, `low`, `medium`, `high`, `under_attack`
   - `development_mode`: `on`, `off`
   - `min_tls_version`: `1.0`, `1.1`, `1.2`, `1.3`
   - `automatic_https_rewrites`: `on`, `off`
6. **Batasan Non-Goals**:
   Pantau secara sadar membatasi cakupan pada kebutuhan esensial DNS, cache, dan pengaturan zone dasar. Fitur kompleks seperti Cloudflare Workers, R2 Storage, Cloudflare Tunnels (cloudflared), aturan WAF/Firewall custom, Page Rules, analitik GraphQL, dan integrasi Global API Key **dikecualikan dari cakupan**.

## Model Keamanan

1. **Perlindungan Token & Isolasi Log**:
   - Nilai token API Cloudflare tidak pernah dikembalikan ke peramban web melalui respons JSON apa pun.
   - Token tidak pernah dicatat (*logged*) pada standard logger, console activity stream, maupun pesan galat (`APIError`).
2. **Mitigasi SSRF (Server-Side Request Forgery)**:
   Base URL klien Cloudflare dipatok secara tetap pada konstanta `https://api.cloudflare.com/client/v4` dalam lingkungan produksi. Pengguna tidak dapat mengarahkan endpoint ke domain sembarang.
3. **Pencegahan Injeksi Jalur (*Path Injection*)**:
   Semua identifier dinamis di URL (`zoneID`, `recordID`, `accountID`) divalidasi ketat menggunakan fungsi `cloudflare.ValidID` dengan pola regex `^[0-9a-f]{32}$`. Input yang tidak memenuhi pola heksadesimal 32 karakter ditolak seketika dengan HTTP 400.
4. **Pembatasan Ukuran Muatan (*Payload Limiting*)**:
   - Ukuran body request masuk dibatasi (maksimal 64 KB untuk operasi JSON).
   - Ukuran respons body dari upstream Cloudflare dibatasi maksimal 4 MiB untuk mencegah serangan kehabisan memori (*memory exhaustion*).
5. **Rekomendasi Hak Akses Minimal (*Least Privilege Token Scopes*)**:
   Pantau menyarankan administrator membuat **User API Token** (bukan Global API Key) dengan batasan izin minimal pada zone target:
   - `Zone - Zone - Read` (melihat daftar zone dan status)
   - `Zone - DNS - Edit` (membaca, membuat, mengubah, dan menghapus record DNS)
   - `Zone - Cache Purge - Purge` (melakukan purge everything atau purge URL)
   - `Zone - Zone Settings - Edit` (opsional: mengubah pengaturan SSL, TLS, development mode)

## Alternatif yang Dipertimbangkan

1. **Menggunakan Pustaka Resmi `cloudflare-go` SDK**:
   Ditolak karena pustaka resmi membawa puluhan dependensi transitif pihak ketiga yang besar, memperbesar ukuran biner kompilasi, serta memperlambat waktu build. Implementasi mandiri berbasis `net/http` hanya membutuhkan ratusan baris kode yang mudah diaudit dan bebas risiko ketergantungan eksternal.
2. **Mendukung Autentikasi Global API Key**:
   Ditolak karena Global API Key memiliki hak istimewa tak terbatas (*unscoped full root access*) ke seluruh akun Cloudflare pemilik tanpa batasan IP atau zona. Mengizinkan Global API Key bertentangan dengan prinsip keamanan pertahanan berlapis (*defense-in-depth*). Pantau hanya menerima Cloudflare API Token berlingkup scoped.
3. **Pengelolaan via CLI `wrangler` atau `cloudflared`**:
   Ditolak karena mewajibkan instalasi dependensi runtime eksternal (Node.js atau biner Go terpisah) pada server host Pantau, berlawanan dengan filosofi single-binary mandiri Pantau.

## Konsekuensi

- Operator dapat mengarahkan DNS dan membersihkan cache secara langsung tanpa membuka dasbor pihak ketiga.
- Tidak ada dependensi Go baru yang ditambahkan ke `go.mod`.
- Jika API Token ditarik atau kadaluwarsa di sisi Cloudflare, Pantau menampilkan galat upstream yang ramah pengguna dan meminta pembaruan token.
- Fitur tingkat lanjut seperti pembuatan zone baru, manajemen firewall rule WAF, dan manajemen Workers tetap harus dilakukan di dasbor resmi Cloudflare.
