# 02 — Label pada Notifikasi

Status: resolved

## Ticket

Sambungkan label Laya ke notifikasi drift: webhook membawa `triage_category`, `triage_urgency`, `triage_escalate`, `triage_confidence`; Telegram menampilkan baris *Category* dan penanda *Human action needed*. Sidecar yang gagal tidak boleh menahan alert.

## Acceptance

- `SendAlert` tetap mengirim alert ketika sidecar mati atau lambat.
- Tidak ada kanal lain (ntfy, Slack, Discord, SMTP) yang berubah.
- Ditutupi unit test.

## Answer

`internal/notify/notify.go`: `SendAlert` memanggil `classify` pada goroutine bila `laya_url` terisi, lalu `dispatch(..., labels, hasLabels)`. `sendTelegram` dan `sendWebhook` menerima label; `hasLabels=false` berarti field label tidak dikirim sama sekali.

`internal/notify/notify_test.go`: 3 test baru (label masuk webhook + Telegram, sidecar tidak terjangkau tetap mengirim alert tanpa label, `laya_url` kosong tidak menyentuh sidecar).

Catatan: label tidak disimpan ke basis data dan tidak tampil pada catatan Incident/Alert.
