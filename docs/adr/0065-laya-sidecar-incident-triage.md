# 0065. Klasifikasi Insiden dengan Laya sebagai Sidecar Opsional

Pantau mendapat klasifikasi insiden dari [Laya](https://github.com/NandhaKishorM/laya) — mesin keputusan non-autoregressive yang memberi label pada teks bebas: kategori, tingkat urgensi, dan "perlu tindakan manusia". Laya hanya berjalan sebagai **sidecar HTTP opsional**; Pantau tetap bekerja penuh tanpanya.

Rancangan: `.scratch/laya-triage/spec.md`.

## Keputusan Arsitektur

1. **Sidecar HTTP, bukan ONNX dalam biner.** `internal/triage` memanggil `POST /v1/systemone` pada `laya-serve` (Python) yang berjalan terpisah. Modul Go murni (`laya-go`) tidak dipakai: sub-paket `onnx`-nya memakai cgo dan membutuhkan ONNX Runtime native, yang mematahkan `CGO_ENABLED=0` pada `Dockerfile` dan seluruh target rilis ([ADR 0002](0002-golang-single-binary-sqlite.md)). Port Go juga belum punya tag rilis, sehingga `go get` berdasarkan versi tidak mungkin tanpa fork/vendor.
2. **Fitur mati secara bawaan.** Dua pengaturan baru: `laya_url` (kosong = nonaktif) dan `laya_timeout_ms` (bawaan 15000 ms, maksimum 60000). Tanpa `laya_url`, Pantau tidak pernah menyentuh jaringan tambahan.
3. **Klasifikasi bersifat advisory.** Laya hanya *menyarankan* label; Aturan Desired State dan regex diagnosis tetap satu-satunya penentu drift. Label masuk ke payload notifikasi (webhook: `triage_category`, `triage_urgency`, `triage_escalate`, `triage_confidence`; Telegram: baris *Category* dan penanda *Human action needed*), bukan ke keputusan inspeksi.
4. **Kegagalan Laya tidak pernah menggagalkan apa pun.** Sidecar tidak terjangkau, timeout, HTTP ≥ 300, atau jawaban tanpa kategori → label dibuang, alert tetap dikirim seperti sebelumnya. Alert menunggu label sampai batas timeout (`laya_timeout_ms`, bawaan 15 detik) lalu dikirim apakah labelnya tiba atau tidak; tidak ada gate di atas confidence, sebab label adalah saran, bukan keputusan.
5. **Cakupan sempit.** Satu question set berisi tiga pertanyaan (`category` = choice, `urgency` = score 0..3, `escalate` = noul). Label choice bersifat semantik (`service_down`, `resource_exhaustion`, …), bukan kata boolean (`true`/`false`), sebab pilihan Laya mengikuti artikulasi label dan akan menjawab label alih-alih teks.
6. **Checkpoint English, sidecar terpisah.** `laya-multilingual` hanya macro-avg 0,227 di 51 bahasa; teks log server pada dasarnya Inggris. `docker-compose.yml` punya service `laya` pada **profile `laya`** (`docker compose --profile laya up -d`), jadi tidak ikut jalan pada `docker compose up` biasa.

## Revisi terhadap ADR sebelumnya

- **[ADR 0002](0002-golang-single-binary-sqlite.md)** — biner Pantau sendiri tetap tanpa dependensi runtime. Sidecar adalah proses terpisah yang tidak tertanam ke biner Pantau: ia tidak menambah dependensi pada biner yang diunduh pengguna, tidak menambah footprint RAM biner, dan tidak ada di jalur rilis (`release.yml`). `CGO_ENABLED=0` tetap berlaku untuk seluruh target. Satu-satunya konsekuensi: bila sidecar diaktifkan, Pantau melakukan satu HTTP request ke `laya_url` saat drift terdeteksi; `laya_url` divalidasi sebagai `http`/`https` dan tidak pernah dipanggil pada jalur selain notifikasi drift.
- **[ADR 0013](0013-airgapped-self-contained-web-assets.md)** — aset web tetap tertanam penuh; tidak ada aset eksternal baru di biner. Sidecar mengunduh bobot modelnya sendiri dari Hugging Face pada build/pull; itu di luar biner Pantau dan di luar cakupan biner.
- **[ADR 0041](0041-systemd-service-actions-and-error-diagnosis.md)** — diagnosis `systemd` tetap murni regex dan tanpa jaringan. Laya hanya melihat *Root Cause Excerpt* yang sudah terbentuk dan mengembalikannya sebagai label, bukan sebagai teks diagnosis.

## Konsekuensi

- Operator yang mau memakai fitur ini menjalankan sidecar tambahan (Python + PyTorch + bobot model, kebutuhan RAM substansial pada CPU) pada host yang sama dengan Pantau.
- Zero-shot checkpoint dasar tidak akurat pada domain ini (0,362 vs baseline majority-class 0,461 pada typed-decisions); label pada fase awal harus diperlakukan sebagai indikasi kasar, bukan klasifikasi tepercaya.
- Implementasi: `internal/triage` (klien + question set), hook pada `internal/notify/notify.go` (`SendAlert`), pengaturan pada `internal/web/server.go` (`handleSettings`), UI pada tab *Notification Dispatcher*.
- Latensi sidecar diukur terhadap `qida/laya:latest` di CPU: sekitar 9–10 detik per permintaan setelah model ter-cache (unduhan model pertama memakan sekitar 150 detik). Karena itu `laya_timeout_ms` bawaannya 15 detik; batas 1,5 detik akan menjatuhkan setiap label.
