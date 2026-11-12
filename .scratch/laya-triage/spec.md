# Laya Sidecar — Klasifikasi Insiden Opsional

## Tujuan

Pantau dapat memberi label otomatis pada drift yang terdeteksi: **kategori insiden**, **tingkat urgensi**, dan **perlu tindakan manusia atau tidak**. Label diambil dari [Laya](https://github.com/NandhaKishorM/laya), mesin keputusan non-autoregressive yang berjalan sebagai proses terpisah (sidecar HTTP).

Label bersifat **advisory**: Aturan Desired State dan regex diagnosis tetap satu-satunya penentu drift. Laya tidak pernah menahan, memblokir, atau mengubah keputusan inspeksi.

## Batasan yang tidak boleh dilanggar

- Biner Pantau tetap satu binary Go tanpa dependensi runtime; seluruh target rilis tetap `CGO_ENABLED=0` (ADR 0002).
- Aset web tetap tertanam penuh; tidak ada CDN atau aset eksternal baru (ADR 0013).
- Diagnosis `systemd` tetap regex murni tanpa jaringan (ADR 0041).
- Tidak ada fitur yang bisa membuat alert hilang karena Laya.

## Cakupan

| Bagian | Isi |
|---|---|
| Klien | `internal/triage` — POST `POST /v1/systemone` ke `laya_url` |
| Pengaturan | `laya_url` (kosong = mati), `laya_timeout_ms` (bawaan 15000, rentang 500..60000) |
| Notifikasi | `triage_category`, `triage_urgency`, `triage_escalate`, `triage_confidence` pada webhook; baris *Category* + *Human action needed* pada Telegram |
| Compose | service `laya` pada profile `laya` (tidak ikut `docker compose up` biasa) |

Pertanyaan yang dikirim:

- `category` (choice): `service_down`, `resource_exhaustion`, `network_connectivity`, `security_exposure`, `package_drift`, `backup_failure`, `unknown`
- `urgency` (score): 0..3
- `escalate` (noul): P(true) ≥ 0,5 → perlu tindakan manusia

Label choice tidak boleh berupa kata boolean; Laya mengikuti artikulasi label.

## Perilaku kegagalan

Kegagalan apa pun (sidecar mati, timeout, HTTP ≥ 300, JSON rusak, jawaban tanpa kategori) → label dibuang, alert tetap dikirim seperti sebelum fitur ini. Alert menunggu label sampai batas timeout lalu dikirim apakah labelnya tiba atau tidak.

## Di luar cakupan

- Mengganti regex diagnosis dengan Laya.
- Menambahkan kanal notifikasi baru.
- Menyimpan label ke database atau ke Incident/Alert.
- Fine-tuning checkpoint.
