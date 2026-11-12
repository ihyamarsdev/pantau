# 01 — Klien Laya (internal/triage)

Status: resolved

## Ticket

Buat paket `internal/triage` yang memanggil `POST /v1/systemone` pada sidecar `laya-serve` dan mengembalikan label insiden: kategori (choice), urgensi (score 0..3), dan perlu tindakan manusia (noul).

## Acceptance

- `Classify` mengembalikan `(Result, bool)`; `bool` false saat dimatikan, tidak terjangkau, timeout, HTTP >= 300, JSON rusak, atau jawaban tanpa kategori.
- State dipotong 40000 karakter.
- Label choice bersifat semantik, bukan boolean.
- Ditutupi unit test.

## Answer

`internal/triage/triage.go` (klien + question set) dan `internal/triage/triage_test.go` (8 test: disabled, unreachable, timeout, HTTP error, kategori hilang, sukses, skor di-clamp, state dipotong).

Batas yang diketahui: `DefaultTimeout` 15 detik karena sidecar CPU yang sudah panas menjawab sekitar 10 detik (diukur terhadap `qida/laya:latest` di `localhost`). Timeout yang lebih pendek selalu menjatuhkan label.

Verifikasi tambahan: `laya-serve` dari `qida/laya:latest` dijawab dengan `{"answers":{"category":{"type":"choice","choice":"service_down",...},"urgency":{"type":"score","score":1.364,...},"escalate":{"type":"noul","noul":0.0933,...}}}` untuk teks `Unit nginx.service is inactive`; `internal/triage` mem-parsing jawaban itu menjadi `service_down (urgency 1/3)`.
