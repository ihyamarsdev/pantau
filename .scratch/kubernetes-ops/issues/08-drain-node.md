# 08 — Drain node dengan konfirmasi dua langkah

Type: task
Status: resolved
Blocked by:

## Temuan riset (ResDrain, terverifikasi di klaster nyata, dry-run)

- `kc drain NODE --ignore-daemonsets --delete-emptydir-data --dry-run=server` mencetak `node/X cordoned (server dry run)`, daftar `evicting pod NS/NAME (server dry run)`, dan peringatan DaemonSet yang diabaikan; **tidak mengubah apa pun** (semua node tetap Ready, tanpa SchedulingDisabled).
- Bila ada pod yang diblokir PodDisruptionBudget, drain **mengulang tiap 5 detik tanpa henti** (terbukti: >300 detik). `--timeout` wajib pada pratinjau dan eksekusi. Di klaster uji penyebabnya PDB `torcons-chat/torcons-db-primary` (disruptions diizinkan 0).
- Ketiga node berperan `control-plane,etcd`: men-drain satu node mengurangi satu suara etcd; satu node lain gagal saat itu → kuorum hilang.
- `ExecTimeout` mematikan SSH di batas waktu → drain terhenti di tengah, node tetap cordoned, pod mungkin sebagian tereviksi. Uncordon sudah ada sebagai pemulihan.

## Perubahan (keputusan konservatif; sinkron, tanpa registri job async)

Backend
- `POST …/kube/drain-preview {name}` (baca; boleh failover): `kc drain NAME --ignore-daemonsets --delete-emptydir-data --dry-run=server --timeout=15s 2>&1`; parser: pod unik dari `evicting pod`, `Warning: ignoring DaemonSet-managed Pods`, baris `error when evicting`/PDB. Respons `{ok, node, pods:[{ns,name}], pod_count, daemonset_skipped:[…], blocked:[…]}`.
- `op:"drain"` pada `POST action` (kind node), flag bool pada `KubeActionReq`: `ignoreDaemonsets` (bawaan true), `deleteEmptyDirData` (bawaan false), `force` (bawaan false), `timeoutSeconds` (bawaan 120, maks 300). Perintah: `kc drain NAME [--ignore-daemonsets] [--delete-emptydir-data] [--force] --timeout=Ns 2>&1`. `kubeDrainTimeout` baru; guard in-flight per Host (pola `inFlight.LoadOrStore`) agar dua drain tak berbalapan. Pesan saat batas waktu: "drain timeout; node tetap cordoned, pod mungkin sebagian tereviksi; jalankan uncordon". Tidak ada auto-uncordon.
- `read-only` memblokir `drain` lewat jalur `action` yang ada; preview boleh (baca). Validasi nama node `ValidKubeName`; semua nilai di-quote.

Frontend
- Item *Drain* di menu Node. Langkah 1: panggil pratinjau, tampilkan daftar pod, jumlah, pod terblokir PDB. Langkah 2: `showConfirm` (danger) dengan kata-kata konkret: nama node; "N pod akan dievakuasi dan dijadwalkan ulang; workload satu replika akan mengalami downtime"; bila `deleteEmptyDirData`: data emptyDir hilang permanen; bila `force`: pod tanpa controller mungkin tak kembali; bila node berlabel control-plane/etcd: "anggota control-plane dan etcd klaster HA; men-drain mengurangi satu suara etcd, bila satu node lain gagal kuorum hilang dan semua workload berhenti"; bila ada pod terblokir: "N pod terblokir PDB, drain dapat berjalan sampai batas waktu". Checkbox untuk tiga flag. Setelah selesai/timeout, toast dengan tombol *Uncordon*. i18n EN|ID.

## Acceptance

- Unit test: parser pratinjau (fixture dari keluaran nyata di atas), perakitan perintah (flag on/off, timeout dibatasi), penolakan injeksi, guard in-flight, blokir `read-only` pada `drain` tetapi bukan pratinjau.
- Verifikasi di `odin`: HANYA `--dry-run=server`, pratinjau memunculkan daftar pod dan baris PDB; setelahnya `kc get nodes` tanpa SchedulingDisabled. **Jangan pernah menjalankan drain nyata** di klaster itu.
- `go build ./... && go vet ./internal/... && go test ./internal/inspector/ ./internal/web/`.
