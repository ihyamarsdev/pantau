# 03 — Pengaturan dan UI

Status: resolved

## Ticket

Tambahkan pengaturan `laya_url` dan `laya_timeout_ms` di API serta UI tab *Notification Dispatcher*.

## Acceptance

- `GET /api/settings` mengembalikan `laya_url` dan `laya_timeout_ms` (bawaan 15000).
- `POST /api/settings` menolak `laya_url` non-http/https dengan 400 dan meng-clamp `laya_timeout_ms` ke 500..60000 (nilai tak valid → 15000).
- Form UI menampilkan, memuat, dan menyimpan kedua field, dengan label i18n EN + ID.
- Ditutupi unit test.

## Answer

`internal/web/server.go` (`handleSettings`): validasi URL `laya_url` memakai pola yang sama dengan `ntfy_url`/`slack_webhook_url`; `laya_timeout_ms` di-clamp ke 500..60000 (di luar rentang atau bukan angka → 15000).

`internal/web/static/index.html`: field `settingLayaUrl` dan `settingLayaTimeout` di bawah *Alert Debounce Runs*; dimuat di `openSettingsModal` dan dikirim di `saveSettings`; kunci i18n `laya_url_label`, `laya_url_desc`, `laya_timeout_label` di kamus EN dan ID.

`internal/web/settings_test.go`: 3 test (validasi URL, clamp timeout, nilai bawaan saat belum diset).

Diverifikasi di browser (e2e `tests/laya-settings.e2e.ts`): kedua field tampil di tab *Notifications*, tersimpan, dan nilainya kembali utuh setelah Pengaturan ditutup dan dibuka lagi. Suite e2e penuh: 9 berkas, 26 test lolos.