# 01 — Identitas klaster dan failover

Type: task
Status: resolved
Blocked by:

## Tujuan

Satu klaster HA tampil sebagai satu entri di bagian Kubernetes, dan permintaan tetap berhasil bila Host yang dipilih mati selama ada peer `server` yang sehat. Rancangan: [spec.md](../spec.md) bagian A.

## Perubahan

Backend
- `internal/inspector/kube.go`: tambah bagian `===CLUSTER_ID===` (`kc get ns kube-system -o jsonpath='{.metadata.uid}'`) pada skrip `KubeDetect`; `KubeClusterInfo.ClusterID` (`json:"cluster_id"`). Selalu timpa nilai lama saat `detect` sukses (Host dapat dipindah klaster).
- `internal/store/db.go`: kolom `hosts.kube_cluster_id TEXT DEFAULT ''` lewat `alterAddColumn` di `migrate()`; tambahkan ke `Host`, `hostSelectCols`, `scanHost`, `ImportSnapshot` (nilai kosong untuk snapshot lama); `SetHostKubeClusterID(id, uid)`.
- `internal/web/handlers_kube.go`: simpan ID saat `detect` sukses; pemilih Host efektif: Host diminta → peer berkluster sama dengan `status` `healthy` lalu `degraded` (urut `sort_order, id`) → `down` terakhir; hanya Host dengan peran `server`. Baca: failover boleh. Tulis (`action`, `apply`): hanya jika dial SSH ke Host utama gagal sebelum perintah berjalan. Sisipkan `served_by_host_id`, `served_by_host_name`, `failover`, `primary_host_id`, `primary_error` ke setiap respons JSON (aditif). `exec-cmd` mengembalikan juga `host_id` yang melayani agar Terminal Dock terhubung ke Host itu.
- Anggap `etcdserver: request timed out` / timeout API >10 s sebagai kegagalan Host (coba peer berikut).

Frontend (`internal/web/static/index.html`)
- Picker Kubernetes: kelompokkan menurut `kube_cluster_id` (satu entri per klaster: nama, jumlah node, status gabungan; sisanya "Standalone"). Nama = Host Group bersama anggota, selain itu nama Host pertama.
- Tombol *Detect clusters* (paralel terbatas 5) untuk mengisi ID pada semua Host.
- Chip *Gateway* di bilah klaster; bila `failover` true → Toast non-blocking dan chip `Gateway: thor (failover dari odin)`.
- Tab Nodes: petakan node ke Host terdaftar lewat `InternalIP`/nama.
- i18n EN|ID; panduan pengguna (EN+ID) satu paragraf.

## Acceptance

- Unit test: pemilihan Host efektif (urutan, `down` terakhir, agent-only dikecualikan, tulis tidak failover setelah perintah berjalan); migrasi kolom + impor snapshot lama; parser `===CLUSTER_ID===`.
- Smoke terhadap `odin`/`thor`/`loki` (read-only): ID sama; mematikan jalur ke Host utama secara simulasi (alamat salah pada Host uji) memunculkan failover dengan `served_by_*` benar. **Jangan** mematikan node asli.
- `go build ./... && go vet ./internal/... && go test ./internal/inspector/ ./internal/store/ ./internal/web/`.
- Kontrak lama `/api/hosts/{id}/kube/*` tetap berjalan untuk klien yang mengabaikan field baru.

## Catatan

CONTEXT.md: istilah **Kubernetes Cluster** dan **Gateway Host**. ADR 0044 mencatat identitas, failover, dan batas tulis.
