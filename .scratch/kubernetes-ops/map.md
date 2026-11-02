# Map: Kubernetes Ops

Status: in-progress (01-03, 05, 06, 08, 09 done; 04 open; 07 deferred)

## Destination

Kubernetes di Pantau menjadi pemantauan, bukan hanya klien kubectl: klaster dikenali sebagai satu entitas, dipantau lewat Desired State/Drift, menampilkan pemakaian sumber daya, dan aksi berbahayanya diberi pengaman.

## Notes

- Domain: Desired State, Drift, Inspection, Host, Kubernetes Cluster Host, Kubernetes Privilege Fallback (CONTEXT.md).
- Standing preferences: ponytail — pakai ulang `kubePrelude`/`kc`, rules engine, `showConfirm`, `dockerBar`, `formatKubeMemory`; tanpa dependensi baru; kontrak API lama tetap kompatibel (field aditif).
- Skills: `/domain-modeling` (istilah baru: Kubernetes Cluster, Gateway Host, Kube Mode), tulis ADR 0044 saat implementasi.

## Decisions so far

- Spec: [spec.md](spec.md) — empat bagian dirancang dan diverifikasi sebagian pada klaster nyata (UID `kube-system` identik di 3 node; metrik mentah ≈0,14 s; `kc diff` exit 1 read-only).

## Tickets

- [01 — Identitas klaster dan failover](issues/01-cluster-identity-and-failover.md)
- [02 — Metrik pemakaian node/pod](issues/02-usage-metrics.md)
- [03 — Keamanan aksi: kube_mode, konfirmasi, pratinjau diff](issues/03-action-safety-kube-mode-confirm-diff.md)
- [04 — Rule Desired State `kube`](issues/04-desired-state-kube-rules.md) (blocked by 01)
- [05 — Lihat dan edit YAML](issues/05-view-edit-yaml.md)
- [06 — Log yang lebih berguna](issues/06-better-logs.md)
- [07 — Port-forward](issues/07-port-forward.md) (needs-info: ditunda, memperluas permukaan serangan)
- [08 — Drain node dengan konfirmasi dua langkah](issues/08-drain-node.md)
- [09 — Kenyamanan UI](issues/09-ui-convenience.md)

## Not yet specified

- Evaluasi rule `kube` pada klaster >500 pod (batas payload, kemungkinan hanya node/workload/PVC).
- Retensi/riwayat tren metrik (di luar snapshot saat ini).

## Out of scope

Drain, edit resource sembarang, multi-klaster kubeconfig eksternal, port-forward, intersepsi PTY.
