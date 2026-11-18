# 0082. Proactive AI Auto-Triage dan Delta-Only Daily Briefing

Menambahkan diagnosis otomatis berbasis LLM yang **hemat token** untuk setiap Drift yang terdeteksi, serta kartu *Daily Briefing* yang hanya merangkum anomali 24 jam terakhir. LLM dipanggil **paling banyak sekali per insiden**, tanpa *tool definitions*, tanpa *multi-turn loop*, dan dengan batas `max_tokens` yang ketat. Mayoritas insiden diselesaikan di Go dengan **nol token**.

## Konteks dan Masalah

1. **AI Agent (ADR 0073) hanya reaktif.** Model dipanggil hanya ketika operator membuka chat dan mengetik. Setiap Drift baru menunggu operator menyadari banner (ADR 0023), membuka chat, dan meminta diagnosis — sementara pola kegagalan yang sama (OOMKilled, disk penuh, sertifikat expired, backup basi) berulang setiap hari.
2. **Biaya tool-loop tidak cocok untuk triage massal.** Putaran chat ADR 0073 membawa hingga 25 panggilan model, riwayat percakapan, dan puluhan *tool definitions*. Memakainya untuk setiap alert akan menghabiskan kuota LLM hanya untuk menyimpulkan penyebab yang sudah bisa ditebak secara deterministik.
3. **Tidak ada ringkasan harian yang ringkas.** Operator harus membaca semua alert, pembaruan paket, dan status TLS/backup satu per satu untuk mengetahui apakah fleet berubah sejak kemarin.

## Keputusan Arsitektur

### 1. Worker triage non-blocking (Go)

- `internal/inspector/alert_debounce.go` dan `internal/notify/event.go` tetap menjadi titik masuk alert; `internal/inspector/desired_rules.go` mengirim `TriageJob` ke `TriageWorker` **setelah** alert dibuat dan notifikasi dikirim. `Submit` memakai `select` dengan `default`: antrean penuh → alert dilewati dengan peringatan, inspeksi tidak pernah menunggu LLM.
- `TriageWorker` berjalan sebagai goroutine tunggal dengan antrean 128 job. Hasilnya disimpan di SQLite dan dipancarkan ke *Activity Console* SSE (ADR 0030) dengan modul `triage`.

### 2. Fast-path deterministik (0 token)

`FastPathTriage` mengenali pola yang tidak perlu model: `OOMKilled`/exit 137, disk penuh (>95% atau `ENOSPC`), sertifikat TLS expired, dan backup basi. Aturan regex berjalan berurutan spesifik→generik dan menghasilkan templat `root_cause`/`impact`/`remediation` yang stabil. Otomasi remediasi dibatasi pada dua aksi: `systemd_restart` dan `docker_restart`.

### 3. Cache deduplikasi signature (0 token)

`ErrorSignature` menghitung SHA-256 dari `(alert_type + unit/container + stripped_error_line)`. Angka, path, UUID, dan nilai dalam tanda kutik dibuang agar kegagalan sejenis jatuh ke satu signature. `LatestTriageBySignature(signature, now-24h)` menjawab cache hit dalam satu `SELECT` terindeks; hasilnya disalin dengan `source = "cache"`.

### 4. Satu panggilan LLM, tanpa tool, terse JSON

Untuk kegagalan yang tidak dikenali, Go menyusun satu *diagnostic bundle*: 20 baris log terakhir yang mengandung kata kunci error (maks. 1.500 karakter), `exit_status`, dan tiga konsumen sumber daya teratas. Satu `llmRequest` dikirim dengan `Tools: nil`, `max_tokens <= 300`, dan instruksi JSON `{root_cause, impact, remediation, remediation_action}`. Tidak ada riwayat, tidak ada tool loop, tidak ada panggilan kedua.

### 5. Kontrol biaya

- Settings → AI Agent: `agent_triage_enabled`, `agent_triage_model` (opsional, jatuh ke model utama ADR 0073), `agent_triage_daily_budget` (default 10.000 token/hari), dan `agent_briefing_time` (HH:MM, kosong = nonaktif).
- `TriageBudgetDay` / `AddTriageBudget` mencatat pemakaian per hari UTC secara atomik; worker berhenti memanggil model ketika kuota habis. Fast-path dan cache hit tidak menyentuh kuota.
- Model ringan memakai provider/key yang sama dengan agent; hanya nama model yang bisa di-override.

### 6. Persistensi dan SSE

Migrasi `internal/store/agent_migrate.go` membuat tabel `agent_triage`, `agent_briefing`, dan `agent_triage_budget` (idempoten, `CREATE TABLE IF NOT EXISTS` + `alterAddColumn` untuk kolom `unit`). Endpoint: `GET /api/agent/triage`, `GET /api/agent/triage?alert_id=…`, `GET /api/agent/briefing`, `POST /api/agent/briefing/generate`.

### 7. Delta-only Daily Briefing

`BriefingService` mengagregasi **hanya** anomali 24 jam terakhir: alert aktif, pembaruan paket kritis (`PendingSecurity > 0` atau reboot required), sertifikat TLS yang akan expired, dan backup basi dari `actual_items`. Jika nol anomali, ringkasan "All systems nominal" dibuat di Go tanpa memanggil LLM (0 token). Jika ada anomali, satu panggilan tool-free dengan `max_tokens <= 400` merangkum daftar tersebut. Penjadwal harian berjalan sesuai `agent_briefing_time`.

### 8. UI (vanilla, tanpa pustaka eksternal)

- Banner alert (ADR 0023) menambahkan accordion **AI Auto-Diagnosis** per alert, menampilkan sumber (`fast_path`/`cache`/`llm`), root cause, impact, dan remediation. Tombol *Apply remediation* memakai `showConfirm` — manusia selalu menyetujui aksi `restart` sebelum request dikirim.
- Kartu **Daily Briefing** di dashboard memakai CSS design token (ADR 0039) dan ikon sprite bawaan; tidak ada CDN atau dependency baru (ADR 0013).

## Alternatif

- **Memakai tool-loop ADR 0073 untuk triage.** Ditolak: 25 panggilan model per turn dan puluhan tool definitions membuat biaya per alert tidak dapat diprediksi, padahal sebagian besar insiden bersifat deterministik.
- **Mengirim seluruh log mentah ke model.** Ditolak: bundle dibatasi 20 baris error-filtered dan 1.500 karakter agar prompt tetap murah dan respons tetap terse.
- **Auto-remediation tanpa konfirmasi.** Ditolak: mode `auto` ADR 0073 hanya berlaku untuk tool agent; triage tetap meminta konfirmasi eksplisit karena `restart` adalah aksi mutasi.

## Konsekuensi

- Mayoritas insiden (fast-path + cache hit) tidak memakan token sama sekali; kuota harian hanya dipakai untuk insiden baru yang kompleks.
- Diagnosa hilang saat alert di-acknowledge? Tidak — baris `agent_triage` tetap tersimpan dan dapat dibaca lewat `alert_id`; hanya baris `alerts` yang di-acknowledge.
- Karena signature membuang angka dan path, dua insiden berbeda pada unit yang sama dalam 24 jam dapat berbagi cache entry; ini disengaja untuk menekan biaya dan dapat diubah dengan membersihkan tabel `agent_triage`.
- Kartu briefing hanya muncul setelah ada briefing tersimpan; operator dapat memicu regenerasi kapan saja lewat tombol refresh.
