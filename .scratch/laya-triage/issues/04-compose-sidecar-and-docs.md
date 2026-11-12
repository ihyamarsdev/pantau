# 04 — Sidecar Compose dan Dokumentasi

Status: resolved

## Ticket

Sediakan service `laya` opsional di `docker-compose.yml` dan dokumentasikan fiturnya.

## Acceptance

- Service `laya` tidak ikut jalan pada `docker compose up` biasa (profile `laya`).
- `docker compose --profile laya up -d` menjalankan `laya-serve` yang menjawab `POST /v1/systemone`.
- ADR, CHANGELOG, CONTEXT.md, README (EN/ID), dan user guide (EN/ID) diperbarui.

## Answer

`docker-compose.yml`: service `laya` (image `qida/laya:latest`, `command: ["laya-serve"]`, `profiles: ["laya"]`, `LAYA_DEVICE=cpu`, `LAYA_MODELS=english`, `LAYA_PRELOAD=1`, `LAYA_PORT=8000`, volume cache model `laya-model-cache`). `docker compose config -q` lolos dan `docker compose --profile laya config` menampilkan service sebagai profile-gated.

Dokumentasi: `docs/adr/0065-laya-sidecar-incident-triage.md` (mengesampingkan ADR 0002/0013/0041), `CHANGELOG.md` (Unreleased → Added), `CONTEXT.md` (glosarium: Laya Sidecar, Incident Category, Urgency Level, Escalation Flag), `README.md` + `README.id.md` (fitur 32), `docs/user-guide.md` + `docs/user-guide.id.md` (bagian Triage Insiden Laya).

Batas yang diketahui: bobot model diunduh dari Hugging Face pada container pertama (sekitar 150 detik untuk unduhan + pemuatan awal); `LAYA_PRELOAD=1` memuatnya di awal sehingga startup container lebih lama.
