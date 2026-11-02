# 05 — Lihat dan edit YAML resource

Type: task
Status: resolved
Blocked by:

## Tujuan

Dari baris resource, buka YAML bersih, ubah, lalu lewat alur yang sudah ada: Preview diff → konfirmasi → Apply. Hasil riset (ResYamlEdit, terverifikasi di klaster nyata, read-only):

- `kc get -o yaml --show-managed-fields=false` hanya membuang `managedFields`; sisanya harus dibersihkan sendiri.
- Hapus: `status`, `metadata.{uid,creationTimestamp,generation,managedFields,selfLink,ownerReferences}`, anotasi `kubectl.kubernetes.io/last-applied-configuration` (hapus `annotations` bila kosong), dan untuk Service `spec.{clusterIP,clusterIPs,ipFamilies,ipFamilyPolicy}`.
- **Pertahankan `metadata.resourceVersion`**: `kc apply` yang basi gagal dengan "object has been modified" (konkurensi optimistis, terverifikasi). `kc diff` mengabaikan RV, jadi pratinjau tidak memberi konflik palsu.
- Manifest bersih: `kc diff -f -` → tanpa beda; `kc apply --dry-run=server` → sukses.
- go.mod tidak punya pustaka YAML dan tidak boleh menambah dependensi.

## Perubahan

Backend
- `GET /api/hosts/{id}/kube/yaml?kind=&ns=&name=` → `{ok, yaml, kind, ns, name}` (envelope + failover baca seperti `describe`).
- Daftar putih `kubeYAMLKinds`: deployment, statefulset, daemonset, service, ingress, configmap, pvc, job, cronjob, namespace. **Secret tidak boleh dilihat atau diedit** (ditolak 400 sebelum perintah dirakit); pod, node, pv juga ditolak. Pertahanan berlapis: bila `kind: Secret` lolos, buang `data`/`stringData`.
- Ambil `-o json`, bersihkan dengan `encoding/json` ke `map[string]any`, lalu emit **YAML sungguhan** lewat emitter kecil buatan sendiri (kunci terurut; skalar sebagai string JSON bila tidak aman-polos; string multibaris sebagai blok `|`; daftar/peta bersarang). Emitter harus lolos uji round-trip: YAML hasil emit di-`kc apply --dry-run=server`/`kc diff` memberi hasil sama dengan JSON asal. Sertakan komentar `# resourceVersion dipertahankan: konflik berarti objek berubah; buka ulang` TIDAK perlu di YAML; letakkan sebagai teks UI.
- Validasi: `ValidKubeName`, `ValidKubeNamespace`, kind whitelist. Edit memakai `POST diff` dan `POST apply` yang sudah ada tanpa perubahan (`read-only` tetap memblokir apply, `diff` tetap boleh).

Frontend
- Item *Edit YAML* di menu baris Workloads, Services, Ingresses, ConfigMaps, PVCs, Jobs, CronJobs (bukan Secrets). Modal besar memakai CodeMirror yang sudah ada (mode `yaml` sudah di-vendor, tema `vscode-dark`), tombol *Preview diff* → Apply terkunci sampai diff untuk teks saat ini dijalankan (sama seperti tab Apply) → `showConfirm` → apply. Pesan konflik dijelaskan ("objek berubah sejak dibuka, buka ulang YAML"). Mode `read-only`: tampil/lihat saja, Apply nonaktif dengan tooltip. Komentar YAML tidak dipertahankan (peringatkan jika relevan). i18n EN|ID.

## Acceptance

- Unit test: whitelist (secret/pod/node ditolak), pembersihan per jenis (Service, Deployment), emitter (skalar khusus: `true`, `null`, `123`, string berawalan `*`/`&`/`:`/spasi, multibaris, daftar kosong, peta kosong) dan round-trip.
- Verifikasi read-only di `odin`: YAML Deployment `coredns` dan Service `kube-dns` dari endpoint, lalu `kc diff -f -` dan `kc apply --dry-run=server -f -` (tidak menyimpan) bersih. Konflik: ubah RV pada salinan lalu `apply --dry-run=server` gagal.
- `go build ./... && go vet ./internal/... && go test ./internal/inspector/ ./internal/store/ ./internal/web/`.
