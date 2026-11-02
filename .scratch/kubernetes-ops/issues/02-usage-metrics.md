# 02 — Metrik pemakaian node dan pod

Type: task
Status: resolved
Blocked by:

## Tujuan

Tab Nodes dan Pods menampilkan CPU dan memori yang **terpakai**, bukan hanya kapasitas. Rancangan: [spec.md](../spec.md) bagian B.

## Perubahan

Backend (`internal/inspector/kube.go`)
- `KubeNodes`/`KubePods`: skrip yang sama menambah bagian `===METRICS===` berisi `kc get --raw /apis/metrics.k8s.io/v1beta1/nodes` (dan `…/pods`, atau `…/namespaces/{ns}/pods` bila `ns` diberikan); `2>/dev/null || true`.
- Helper `parseKubeCPUMilli` (`142m`, `3142857n`, `1`) dan `parseKubeMemoryBytes` (`Ki`/`Mi`/`Gi`/byte). Pod = jumlah `containers[].usage`.
- `rawNodeList` juga membaca `status.allocatable`. Persen dihitung terhadap **allocatable**.
- Field aditif `omitempty`: node `usageCPU`, `usageMemory`, `cpuPercent`, `memoryPercent`, `allocatableCPU`, `allocatableMem`; pod `usageCPU`, `usageMemory`. Pod tanpa persen.
- 404/503/kosong dari metrics API bersifat **non-fatal**: field kosong, tidak ada galat. Batasi payload metrik pod (mis. 2 MB; di atasnya abaikan metrik pod).

Frontend (`index.html`)
- Nodes: dua kolom `CPU` dan `Memory` (nilai terpakai + persen + `dockerBar(pct, 70, 90)`); tanpa metrik → tampilan kapasitas sekarang.
- Pods: dua kolom `CPU` dan `Memory` (`formatKubeMemory`, `-` bila kosong).
- Manual refresh saja, tanpa polling (ADR 0043 butir 7). i18n EN|ID.

## Acceptance

- Unit test: kedua helper (semua satuan, nilai tak valid), parser metrik dengan fixture nyata, kasus 404/503/items kosong, node tanpa `allocatable`.
- Smoke terhadap `odin` (read-only): angka tampil dan wajar dibanding `kc top nodes`; GB konsisten dengan kolom kapasitas.
- Tidak ada perubahan respons bagi klien yang tidak membaca field baru.
- Catat dalam ADR bahwa perilaku tanpa metrics-server dan RKE2 (`rke2-metrics-server`) **belum diuji** bila memang belum.
