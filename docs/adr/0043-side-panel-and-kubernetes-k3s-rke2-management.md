# 0043. Side Panel dan Manajemen Kubernetes (k3s / RKE2)

Menambahkan navigasi samping global (**Side Panel**) dengan bagian Hosts, Docker, dan Kubernetes, serta manajemen klaster k3s/RKE2 secara agentless melalui SSH.

## Konteks dan Masalah

1. Docker hanya dapat dicapai dari tab Docker di Host Detail setelah memilih Host; tidak ada pintu masuk lintas-Host, dan tidak ada tempat alami bagi fitur lintas-Host baru.
2. Host dengan k3s atau RKE2 masih memerlukan terminal untuk `kubectl`.
3. Pantau agentless ([ADR 0001](0001-agentless-ssh-architecture.md)): tidak ada agen atau kubeconfig yang disimpan; semua lewat SSH tanpa kata sandi ([ADR 0042](0042-docker-management-portainer-style.md)).

## Keputusan Arsitektur

1. **Side Panel** adalah bilah tetap di kiri (di bawah header, di atas footer; selaras [ADR 0008](0008-fixed-shell-header-and-footer.md)) dengan entri Hosts (dashboard), Docker, Kubernetes. Dapat diciutkan menjadi rel ikon (disimpan di `localStorage`); di bawah 768px menjadi off-canvas yang dibuka dari tombol header. Hanya memakai Design Token dan Icon Sprite ([ADR 0039](0039-modern-visual-design-system-and-icon-sprite.md)); string lewat i18n EN|ID ([ADR 0009](0009-client-side-i18n-and-domain-glossary-preservation.md)).
2. **Bagian Docker** adalah pemilih Host yang dapat difilter; memilih Host membuka Host Detail langsung di tab Docker lewat jalur Smart Resume ([ADR 0026](0026-sub-header-workspace-shelf-and-smart-resume.md)). UI Docker tidak disalin atau dipindahkan karena terikat erat pada Workspace Modal Host Detail; tidak ada kode Docker baru.
3. **Satu lapisan perintah Kubernetes** (`internal/inspector/kube.go`), meniru `dockerShell`: setiap operasi adalah skrip `sh -c` dengan prelude yang mendefinisikan fungsi `kc()`. Prelude mendeteksi k3s (`k3s kubectl`, `/etc/rancher/k3s/k3s.yaml`) lalu RKE2 (`/var/lib/rancher/rke2/bin/kubectl`, `/etc/rancher/rke2/rke2.yaml`); selain itu galat `no k3s/rke2 control plane found` (misalnya node agent saja). Hanya control-plane (server) yang didukung; kubectl generik dan kubeconfig pengguna tidak dibaca.
3a. **Kubernetes Privilege Fallback**: sama seperti Docker Privilege Fallback. `kc` berjalan sebagai akun SSH biasa; hanya bila kubeconfig ditolak (*permission denied*) dan `sudo -n true` berhasil, dipakai `sudo -n env KUBECONFIG=… kubectl`. Kata sandi tidak pernah dipakai atau disimpan. Kubeconfig k3s/RKE2 bawaan hanya dapat dibaca root, sehingga umumnya jalur sudo yang dipakai.
4. **Validasi dan quoting**: nama dan namespace harus DNS-1123, `kind`/`op` memakai daftar putih, `replicas` 0–1000, `tail` 1–2000; pelanggaran menghasilkan `ErrKubeInvalid` → HTTP 400. Setiap nilai tetap melewati `shellQuote` dan perintah dirakit sebagai argumen.
5. **Cakupan aksi** sengaja sempit: `scale` (Deployment/StatefulSet), `restart` (rollout restart Deployment/StatefulSet/DaemonSet), `delete` (hanya Pod), `cordon`/`uncordon` (Node), ditambah exec dan log Pod (butir 10). Tidak ada drain atau penghapusan resource sembarang. Pod dikelola ulang oleh controller-nya, sehingga `delete pod` dapat dipulihkan.
6. **Apply** menerima YAML maksimal 64 KB (via base64), menjalankan `kubectl apply --dry-run=server` lebih dulu, dan baru menerapkan bila dry-run lolos; YAML tidak valid ditolak tanpa mengubah klaster.
7. **Pembacaan** memakai `kubectl get -o json` yang diurai di Go menjadi struct ringkas (siap/diinginkan, restart, status dihitung di server). Tidak ada polling di UI; muat ulang manual.
8. **API**: `GET /api/hosts/{id}/kube/{detect|nodes|namespaces|workloads|pods|services|events|describe|logs}`, `POST …/{action|apply}`, ditangani `handleKubeManage` (`internal/web/handlers_kube.go`). Kegagalan baca → HTTP 200 `ok:false`; masukan ditolak → 400. Hasil dicatat ke Activity Console (modul `kube`).
9. **Resource tambahan (read-only)**: `GET …/kube/resources?kind=` untuk `ingresses|configmaps|secrets|pvcs|pvs|jobs|cronjobs`, satu bentuk respons generik (`columns` + `rows`) sehingga UI memakai satu renderer tabel. **Secret**: kubectl dipanggil dengan `-o go-template` yang hanya mencetak metadata dan `len .data`; nilai dan nama kunci tidak pernah meninggalkan Host, dan `describe` untuk secret ditolak. `describe` diperluas ke ingress, configmap, pvc, pv, job, cronjob.
10. **Exec dan log langsung**: `GET …/kube/exec-cmd` mengembalikan satu baris perintah (prelude + `kc exec -it … -- sh -c 'bash atau sh'`) yang sudah divalidasi dan di-quote di Go; UI mengetikkannya ke tab Terminal Dock memakai jembatan PTY SSH yang sama dengan console Docker. Log mengalir lewat WebSocket `/ws/kube/logs` (`kc logs -f`, opsi `previous`), meniru `/ws/docker/logs`.
11. **Memori ditampilkan dalam GB** (basis 1024, label `GB`, satu desimal) lewat satu helper UI, selaras dengan `formatBytes()`; backend tetap mengirim nilai kubectl mentah.

## Konsekuensi

- Tidak ada skema database baru; klaster dideteksi per permintaan dan tidak disimpan.
- Host tanpa sudo tanpa kata sandi dan tanpa kubeconfig terbaca menampilkan galat asli `kubectl`.
- Deteksi memanggil `sudo -n test` bila berkas kubeconfig tidak terbaca oleh akun SSH, sehingga tiap panggilan dapat meninggalkan satu catatan sudo di log auth Host.
- Aksi mengubah klaster produksi tanpa riwayat/undo; `restart` memulai ulang Pod, `scale` ke 0 menghentikan beban kerja.
- Belum ada: drain, multi-klaster, kubeconfig eksternal, edit/hapus resource selain Pod.
- Exec memerlukan `bash` atau `sh` di dalam image container; image *scratch*/distroless tanpa shell gagal dengan galat dari container.
- Diverifikasi: unit test offline (validasi, perakitan perintah, parser, keamanan template secret), UI di browser terhadap klaster k3s nyata `odin` secara read-only (3 node HA, RHEL 10, kubeconfig root-only, `sudo -n`): deteksi, node, resource, log streaming, exec `echo ok`. Temuan: `secure_path` sudo di RHEL tidak memuat `/usr/local/bin`, sehingga `k3s` dipanggil dengan path absolut (`command -v k3s`). Aksi yang mengubah klaster (scale, restart, delete, cordon, apply) belum dijalankan pada klaster nyata. **RKE2 belum diuji pada host nyata.**
