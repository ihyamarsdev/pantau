# 09 — Inspeksi: `network down` bukan `down`, dan loop status

Status: ready-for-agent

Blocked by: 05

## Lingkup

1. `internal/inspector/inspector.go` `InspectHostWithTimeout`/jalur galat: bila `errors.Is(err, sshrunner.ErrNetworkUnavailable)` → Host `status="stale"`, pesan `network down: <name>` disimpan pada mekanisme galat inspeksi yang sudah ada (jangan menambah tabel), dan sisa pemeriksaan dilewati. Metrik terakhir dipertahankan.
2. Notifikasi: `network down` **tidak** memicu alert "Host down" (debouncer/alert yang ada tidak disentuh untuk status `stale`); cukup tampil di UI.
3. UI: baris diagnostik/`/api/hosts/{id}/test` menampilkan `network down: <name>` dengan tautan ke Settings → Networks.
4. Manager: `Refresh` status dipanggil sebelum inspeksi host ber-Network (sekali per siklus, bukan per Host) agar badge tidak basi.

## Penerimaan

- Tes: resolver mengembalikan `ErrNetworkUnavailable` → `InspectHostWithTimeout` tidak menandai `down`, tidak mengirim alert, menulis alasan `network down`.
- Tes: satu siklus inspeksi 3 Host di Network yang sama → `Ensure` dipanggil sekali (verifikasi lewat driver palsu).

## Catatan

Status `stale` sudah ada di UI; jangan menambah status baru.
