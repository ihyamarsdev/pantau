# 06 — Log yang lebih berguna

Type: task
Status: resolved
Blocked by:

## Tujuan

Mencari di dalam log, melihat timestamp, dan melihat log satu workload tanpa memilih Pod satu per satu.

## Perubahan

Backend
- `/ws/kube/logs` menerima `timestamps=1` (→ `--timestamps`) dan `workload=<deployment|statefulset|daemonset>/<nama>` sebagai pengganti `pod` (→ `kc logs <kind>/<nama> -n NS --all-pods --prefix --all-containers --tail=N -f --max-log-requests=20`; kind dan nama divalidasi). Verifikasi dulu di `odin` (read-only) bahwa `--all-pods` dan `--max-log-requests` tersedia di kubectl 1.36 dan perilakunya (awalan `[pod/x/container]`); bila tidak ada, pakai `-l` dengan selector dari `.spec.selector.matchLabels` workload (diambil di Go, divalidasi, di-quote). `previous=1` untuk workload ditolak (400). Tidak ada failover di WS (sudah dicatat di ADR 0044).
- Respons `describe`/aksi tidak berubah.

Frontend
- Di modal log Pod: kotak pencarian/filter (klien, menyorot atau menyembunyikan baris tak cocok, jumlah cocok), toggle *Timestamps* (mengulang aliran), batas buffer yang ada tetap.
- Menu baris Workloads: *Logs* membuka modal yang sama untuk workload (awalan pod terlihat, filter berguna untuk memilah). i18n EN|ID.

## Acceptance

- Unit test: perakitan perintah (workload/pod/timestamps/previous), penolakan injeksi (`a;rm`, `$(x)`, kind tak dikenal), `previous` + workload → 400.
- Verifikasi read-only di `odin`: aliran log workload `coredns` (kube-system) menghasilkan baris berawalan; filter klien bekerja di browser throwaway.
- `go build ./... && go vet ./internal/... && go test ./internal/inspector/ ./internal/web/`.
