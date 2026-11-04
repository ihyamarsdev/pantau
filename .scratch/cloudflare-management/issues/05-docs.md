# 05 — Dokumentasi & Istilah Arsitektur Cloudflare Management

Type: task
Status: ready-for-agent
Blocked by:

## Tujuan

Menyusun dokumentasi arsitektur dan operasional fitur Cloudflare Management: Architectural Decision Record (ADR 0048), penambahan istilah domain di `CONTEXT.md`, catatan rilis `CHANGELOG.md`, ringkasan fitur di `README.md` dan `README.id.md`, panduan lengkap di `docs/user-guide.md` dan `docs/user-guide.id.md`, serta tiket-tiket pelacakan isu di `.scratch/cloudflare-management/issues/`.

## Perubahan

- `docs/adr/0048-cloudflare-management-via-api.md`:
  - Format mengikuti ADR 0047 dalam Bahasa Indonesia (Konteks dan Masalah, Keputusan Arsitektur, Model Keamanan, Alternatif yang Dipertimbangkan [penolakan SDK cloudflare-go & Global API Key], Konsekuensi).
  - Dokumentasi rekomendasi least privilege token scopes, penyimpanan token plaintext lokal dengan enkripsi snapshot, dan non-goals (Workers, R2, Tunnels, WAF).
- `CONTEXT.md`:
  - Penambahan 3 istilah domain baru sesuai spec (`Cloudflare Account`, `Cloudflare Zone`, `Zone Setting`) dengan gaya kamus eksisting (definisi bahasa Indonesia, baris `_Avoid_:`).
- `CHANGELOG.md`:
  - Pencatatan fitur baru di bagian `[Unreleased]` mengikuti format Keep a Changelog.
- `README.md` & `README.id.md`:
  - Penambahan seksi fitur bernomor baru (Cloudflare Management via REST API v4) mencakup kemampuan utama, keamanan kredensial, dan kemudahan operasional.
- `docs/user-guide.md` & `docs/user-guide.id.md`:
  - Penambahan bab panduan operasional langkah-demi-langkah: pendaftaran akun, pembuatan token di dasbor Cloudflare (least-privilege scopes), pengelolaan DNS record, pembersihan cache, pengaturan zone, dan pertimbangan keamanan.
- `.scratch/cloudflare-management/issues/`:
  - Tiket pelacakan `01-client.md` hingga `05-docs.md` berstatus `ready-for-agent`.

## Acceptance

- Semua berkas dokumentasi valid, tautan markdown internal terselesaikan.
- Uji embedding dokumen `go test -v ./internal/web/ -run 'Doc|Changelog|Readme'` lolos.
