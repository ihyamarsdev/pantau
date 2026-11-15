# 0075. Cloudflare: IP Access Rules, Tunnels (baca), ekspor DNS, dan panduan izin token

Memperluas [ADR 0048](0048-cloudflare-management-via-api.md) (pengelolaan Cloudflare via REST API v4) dan tool agent Cloudflare di [ADR 0066](0066-hermes-ai-agent-and-mcp-tools.md). Endpoint dan nama izin diperiksa terhadap dokumentasi Cloudflare API (`developers.cloudflare.com/api`) lewat Context7.

## Konteks dan Masalah

1. ADR 0048 membatasi cakupan pada Zone, DNS, purge cache, dan delapan pengaturan zone. Saat insiden (pemindai, brute force, trafik dari satu ASN) operator tetap harus membuka dasbor Cloudflare untuk memblokir sebuah IP, dan untuk melihat apakah `cloudflared` di host yang dipantau Pantau masih terhubung.
2. Panduan izin token hanya memuat empat izin. Fitur baru butuh izin lain, dan token yang kurang izin gagal dengan galat Cloudflare mentah (`10000: Authentication error`) yang tidak menyebut izin mana yang kurang.

## Keputusan Arsitektur

1. **IP Access Rules tingkat zone** (`internal/cloudflare/access.go`): `GET/POST /zones/{id}/firewall/access_rules/rules` dan `DELETE .../{rule_id}`. Mode: `block`, `challenge`, `js_challenge`, `managed_challenge`, `whitelist`. Target: `ip`, `ip_range`, `asn`, `country`. `AccessRuleInput.Validate` memeriksa nilai sesuai target (IP, CIDR, nomor ASN dinormalkan ke `AS<n>`, kode negara dua huruf dinormalkan ke huruf besar; catatan maks. 200 karakter) sebelum ada permintaan keluar. Tingkat akun (aturan untuk semua zone) sengaja tidak dibuka: dampaknya melebihi satu zone.
2. **Cloudflare Tunnels, hanya baca** (`internal/cloudflare/tunnels.go`): `GET /accounts/{id}/cfd_tunnel?is_deleted=false`. Pantau menampilkan nama, status, tipe, dan waktu; tidak membuat, mengubah, atau menghapus tunnel, dan tidak mengambil token tunnel. ID akun diambil dari `account_id` yang tersimpan; bila kosong, dari ID akun pemilik zone yang terlihat oleh token (harus tepat satu). Karena itu `Zone.AccountID` ditambahkan. Mengisi Account ID memicu verifikasi token di `/accounts/{id}/tokens/verify` yang hanya berlaku untuk token milik Account, jadi untuk token User kolom itu sebaiknya kosong.
3. **Ekspor DNS** (`GET /zones/{id}/dns_records/export`): respons teks BIND, bukan envelope JSON, jadi klien mendapat `doText` di samping `do` (keduanya memakai `send`: batas 4 MiB dan header yang sama). Handler mengirimnya sebagai lampiran `text/plain` dengan `X-Content-Type-Options: nosniff`.
4. **Allowlist Zone Settings bertambah** dengan sembilan pengaturan bernilai string yang aman: `tls_1_3` (`on`, `off`, `zrt`), `http3`, `0rtt`, `ipv6`, `websockets`, `brotli`, `email_obfuscation`, `hotlink_protection`, `always_online` (`on`, `off`).
5. **Galat izin dibedakan**: respons upstream 401/403 tetap dipetakan ke HTTP 400 (agar peramban tidak mengira sesi Pantau habis, ADR 0048), tetapi kini dengan `"forbidden": true`. UI memakainya untuk menampilkan peringatan yang menyebut izin tepat yang kurang untuk panel itu.
6. **Panduan izin** (Panduan Pengguna EN/ID, dialog Tambah Akun, README): tabel fitur -> izin -> lingkup resource, token awal yang disarankan, cara membatasi token (zone, akun, IP klien, TTL), dan catatan Account ID.
7. **Tool agent**: `cf_access_rules` dan `cf_tunnels` (baca), `cf_access_rule_change` (create/delete, `gateHigh`: selalu meminta persetujuan operator, tanpa izin sesi). Kartu persetujuan menyebut mode, target, dan nilai; aturan yang tidak valid ditolak sebelum kartu muncul. SOUL melarang membuat aturan `block` untuk IP, rentang, ASN, atau negara yang tidak disebut operator.

## Izin token per fitur

| Fitur | Izin | Lingkup |
|---|---|---|
| Daftar zone | `Zone → Zone → Read` | Zone |
| DNS: lihat, ekspor | `Zone → DNS → Read` | Zone |
| DNS: ubah | `Zone → DNS → Edit` | Zone |
| Purge cache | `Zone → Cache Purge → Purge` | Zone |
| Zone Settings: lihat / ubah | `Zone → Zone Settings → Read` / `Edit` | Zone |
| Access Rules: lihat / ubah | `Zone → Firewall Services → Read` / `Edit` | Zone |
| Tunnels: lihat | `Account → Cloudflare Tunnel → Read` | Account |

## Model Keamanan

1. Semua yang berlaku di ADR 0048 tetap berlaku: token tidak pernah dikembalikan atau dicatat, base URL tetap, ID di path divalidasi `^[0-9a-f]{32}$` (termasuk ID aturan), body masuk dibatasi.
2. Aturan `block`/`challenge` langsung memengaruhi trafik nyata dan dapat mengunci operator sendiri. Penghapusan meminta konfirmasi di UI; di agent semua perubahan meminta persetujuan. Mutasi dicatat ke activity log (mode, target, nilai; tanpa token).
3. Tunnels hanya baca: izin `Cloudflare Tunnel Read` tidak dapat mengubah tunnel, dan Pantau tidak pernah memanggil endpoint tulis atau endpoint token tunnel.

## Alternatif yang Dipertimbangkan

1. **Aturan firewall/WAF kustom (Rulesets API)**: ditolak untuk saat ini; bahasa ekspresi dan urutan evaluasi aturan membuat kesalahan konfigurasi mahal. IP Access Rules sederhana, dapat dibatalkan, dan cukup untuk pemblokiran insiden.
2. **Pengelolaan tunnel penuh (buat, konfigurasi ingress, hapus)**: ditolak; ingress mengubah rute produksi dan token tunnel adalah rahasia baru.
3. **Memeriksa izin token saat menyimpan** (`GET /user/tokens/{id}` lalu membaca `policies`): ditolak; endpoint itu sendiri butuh izin `API Tokens Read`, yang tidak boleh diwajibkan. Izin yang kurang dilaporkan saat dipakai.

## Konsekuensi

- Operator dapat memblokir sumber serangan dan memantau tunnel tanpa meninggalkan Pantau.
- Token lama tanpa izin baru tetap berfungsi untuk fitur lama; tab baru menampilkan peringatan izin.
- Tidak ada dependensi Go baru.
