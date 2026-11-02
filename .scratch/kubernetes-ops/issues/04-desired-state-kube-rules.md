# 04 — Rule Desired State `kube`

Type: task
Status: ready-for-agent
Blocked by: 01

## Tujuan

Kondisi klaster (node NotReady, pod CrashLoopBackOff/restart berlebih, workload belum siap, PVC Pending) menjadi Drift yang memicu Alert dan Notification Channel yang sudah ada. Rancangan: [spec.md](../spec.md) bagian D. Diblokir 01 agar klaster HA tidak memicu Alert palsu saat satu node reboot.

## Perubahan

Tanpa migrasi (`desired_rules.kind/target/expected` adalah `TEXT`; Snapshot sudah generik).

Backend (`internal/inspector/inspector.go`, `kube.go`)
- Evaluator untuk `Kind=="kube"` di `evaluateRule`. Sebelum loop rule: bila tidak ada rule `kube` aktif, **tidak ada** perintah `kc` yang berjalan.
- Satu kueri per Inspection: `timeout 10s` + `kubePrelude` + `kc get nodes,pods,deployments,statefulsets,daemonsets,pvc -A -o json` lewat runner yang sama; diurai sekali dengan parser yang ada (`parseKubeNodes/Pods/Workloads` + struct PVC), dipakai semua rule. Timeout/kontrol-plane tak merespons → satu Drift `Kubernetes control plane unresponsive`. Gunakan pemilihan Host efektif dari 01 untuk klaster HA.
- Sintaks (`Target` → `Expected`): `node/<nama|*>` → `Ready`; `pod/<ns>/<prefiks|*>` → `running, restarts<=N`; `<deployment|statefulset|daemonset>/<ns>/<nama>` → `ready`; `pvc/<ns>/<nama|*>` → `Bound`. Pod milik Job `Completed/Succeeded` dianggap sehat.
- Wildcard → **satu** Drift teragregasi per rule yang menyebut semua anggota gagal.
- Root Cause Excerpt hanya saat transisi ke drift: node `describe node | tail -n 25`; pod alasan+exit code, `logs --previous --tail 30` (fallback tanpa `--previous`), events 10 baris; workload/PVC `describe | tail`.
- Validasi target (server): pola di atas; tolak sisanya (tetap melewati cek metakarakter shell yang ada di `server.go`).
- `GenerateBaseline`: bila `KubeDetect` sukses → `node/*`, `pod/*` (`running, restarts<=10`), `pvc/*`, dan rule per Deployment di namespace non-sistem (`kube-system`, `kube-public`, `cattle-system` dikecualikan).
- Batas: bila payload >N MB, evaluasi hanya node/workload/PVC dan catat Drift informatif; tetapkan N dan uji.

Frontend (`index.html`)
- Opsi `kube` pada `#ruleKind` dan `ruleKindHints` (placeholder target/expected), i18n EN|ID; baris rule menampilkan Actual teragregasi.

## Acceptance

- Unit test: tiap jenis rule (ok dan drift), wildcard teragregasi, Job `Completed`, tanpa rule `kube` → nol perintah `kc` (assert pada runner palsu), timeout → satu Drift, Alert Auto-Resolution saat pulih, validasi target.
- Smoke terhadap `odin` (read-only): rule `node/*`, `pvc/*`, `deployment/<ns>/<nama>` bernilai `ok`; satu rule dengan `Expected` yang sengaja salah menghasilkan Drift dengan Root Cause terisi (tanpa mengubah klaster). Ukur durasi satu Inspection dengan rule `kube` dan catat di ADR.
- Hitung jejak `sudo` per Inspection; dokumentasikan interval/mitigasi (spec bagian Risiko).
- `go build ./... && go vet ./internal/... && go test ./internal/inspector/ ./internal/web/`.

## Catatan

Perbarui ADR 0025 (taksonomi: tambah `kube`) dan ADR 0044. CONTEXT.md: Desired Rule Kind `kube`.
