# 0044. Identitas Klaster Kubernetes, Metrik Pemakaian, dan Pengaman Aksi

Melengkapi [ADR 0043](0043-side-panel-and-kubernetes-k3s-rke2-management.md): satu klaster HA dikenali sebagai satu entitas dengan failover, tab Nodes/Pods menampilkan pemakaian, dan aksi berbahaya diberi pengaman per Host. Rancangan dan verifikasi awal: `.scratch/kubernetes-ops/`.

## Konteks dan Masalah

1. Klaster HA 3 node terdaftar sebagai 3 Host. Memilih satu node yang mati menggagalkan semua permintaan walau klaster sehat.
2. Node hanya menampilkan kapasitas, bukan CPU/memori terpakai.
3. Exec = root shell di pod; scale ke 0, restart, cordon tanpa konfirmasi; Apply tanpa pratinjau.

## Keputusan Arsitektur

1. **Identitas klaster** = UID namespace `kube-system` (`kc get ns kube-system -o jsonpath='{.metadata.uid}'`), dibaca dalam skrip `KubeDetect` yang sama (`===CLUSTER_ID===`, tanpa round-trip SSH tambahan). Terverifikasi identik pada ketiga node klaster k3s uji. Disimpan di kolom `hosts.kube_cluster_id`, hanya diisi server saat `detect` sukses (input klien diabaikan) dan selalu ditimpa agar Host yang dipindah klaster tidak membawa ID lama. Disimpan (bukan dihitung tiap buka halaman) agar peer tetap dikenal saat Host utama mati. Pengisian on-demand lewat `detect` atau tombol *Detect clusters* (paralel terbatas 5); tidak ada auto-discovery.
2. **Failover** di `handleKubeManage`: Host diminta, lalu peer `server` berkluster sama menurut status `healthy` → `degraded` → tak dikenal → `down`, urut `sort_order, id`. Endpoint baca (termasuk `diff`) boleh failover. `action` dan `apply` hanya failover bila dial SSH ke Host utama gagal sebelum perintah apa pun berjalan; setelah tersambung, galat dikembalikan apa adanya (menghindari eksekusi ganda). `/ws/kube/logs` tidak failover (batasan). Setiap respons memuat `served_by_host_id`, `served_by_host_name`, `failover`, `primary_host_id`, dan `primary_error` (aditif); `exec-cmd` memuat `host_id` Host yang melayani agar Terminal Dock tersambung ke sana. UI menampilkan chip *Gateway* dan Toast saat failover.
3. **Metrik pemakaian** dari API mentah `metrics.k8s.io` pada skrip yang sama dengan `get nodes/pods` (bagian `===METRICS===`), bukan `kubectl top` (teks tidak stabil, lebih lambat). Persen dihitung terhadap `allocatable`; pod tanpa persen (tanpa request/limit). Metrics-server absen/belum siap, status 404/503, kosong, atau payload >2 MB: field pemakaian dihilangkan dan UI jatuh ke tampilan kapasitas, bukan galat. Field baru bersifat aditif (`omitempty`). Tanpa polling.
4. **`kube_mode`** per Host: `full` (bawaan) | `no-exec` | `read-only`. Satu enum agar keadaan mustahil tak dapat dibuat. Kolom di `hosts` (kebijakan per Host, bukan `settings`), ikut Snapshot ([ADR 0006](0006-encrypted-system-snapshot-and-github-sync.md)); snapshot lama tanpa kolom dipulihkan sebagai `full`. Nilai tak dikenal ditolak (400). Penegakan di server: `read-only` → 403 untuk `action`/`apply`; `no-exec` dan `read-only` → 403 untuk `exec-cmd`; log dan `diff` selalu boleh. `/ws/terminal` menolak `cmd` (dan perintah preset) yang cocok `kubectl|kc|k3s kubectl … exec` pada Host berkebijakan itu; `docker exec` tidak cocok.
5. **Batas penegakan (jujur)**: Pantau tidak dapat mencegat ketikan pada PTY interaktif biasa (dapat dielak dengan alias, skrip, base64). `no-exec` adalah pagar tingkat aplikasi yang mencegah Pantau memfasilitasi exec. Batas keamanan sebenarnya tetap izin kubeconfig, sudoers, atau RBAC di Host. UI menyatakan ini di dialog Host.
6. **Konfirmasi** (`showConfirm`) dengan dampak konkret: scale ke 0 (jumlah pod berhenti; StatefulSet: PVC tetap ada), restart rollout (replika dibuat ulang bertahap), hapus Pod (node; pod tak terkelola hilang permanen), cordon (workload tetap berjalan, tak ada pod baru).
7. **Pratinjau Apply**: `POST …/kube/diff` menjalankan `apply --dry-run=server` lalu `kc diff -f`. Exit 0 = tak ada perbedaan, **exit 1 = ada perbedaan (bukan galat)**, >1 = galat; output dipotong 256 KB. Tombol *Apply* terkunci sampai diff untuk isi YAML saat ini dijalankan; mengubah teks mengunci kembali; Apply tetap lewat `showConfirm`.

## Konsekuensi

- Dua kolom baru pada `hosts`, migrasi idempoten; tidak ada tabel baru.
- Klaster yang Host-nya belum pernah `detect` tampil sebagai Standalone sampai *Detect clusters* dijalankan.
- Failover membuat panggilan dapat dilayani Host lain; selalu terlihat lewat `served_by_*`. `sudo -n` pada peer bisa berbeda dari Host utama; galat tampil di `primary_error`.
- Belum ada: rule Desired State untuk Kubernetes (tiket 04, memakai failover ini), drain, multi-klaster kubeconfig eksternal.
- Diverifikasi: unit test (matriks mode × endpoint, urutan pemilihan Host, aturan failover tulis, impor snapshot lama, parser, kode keluar diff, regex PTY, parser metrik dengan fixture nyata); pada klaster k3s nyata read-only: UID identik di 3 node, metrik cocok dengan `kubectl top`, `kc diff` exit 1 tanpa membuat objek, failover disimulasikan di DB sementara. Aksi mutasi (scale, restart, delete, cordon, apply) belum dijalankan pada klaster nyata. RKE2 dan perilaku tanpa metrics-server belum diuji pada host nyata.
