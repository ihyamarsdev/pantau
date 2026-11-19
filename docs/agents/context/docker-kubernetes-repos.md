# Docker, Kubernetes & Git Repositories

Domain reference for Docker, Kubernetes and cloned Git repositories. Read this when working in Docker/Kubernetes handlers or `internal/store/git_repos.go`.

**Docker Stack**:
Satu proyek Docker Compose pada Host (kumpulan container berlabel `com.docker.compose.project`) yang dikelola sebagai satu unit di sub-tab Stacks: status agregat, up/down/start/stop/restart/pull/recreate/update, serta penyuntingan atau deploy berkas compose. Stack baru dari Pantau disimpan di `~/.pantau/stacks/<nama>/` pada Host.
_Avoid_: Compose app, docker service, swarm stack

**Docker Privilege Fallback**:
Aturan bahwa perintah Docker dijalankan sebagai akun SSH biasa dan baru memakai `sudo -n docker` bila socket Docker menolak akses (*permission denied*) dan sudo tanpa kata sandi tersedia; kata sandi tidak pernah dipakai atau disimpan.
_Avoid_: Sudo docker mode, root docker, privileged docker

**Docker Hub Browser**:
Modal pencarian repositori publik Docker Hub di tab Docker sebuah Host (hasil pencarian, ringkasan/README, dan daftar tag beserta platform, ukuran, dan waktu push) yang datanya diambil server Pantau tanpa kredensial; aksi Pull dan Run pada sebuah tag dijalankan di Host yang sedang dibuka.
_Avoid_: Image marketplace, registry browser, Docker search

**Image Update Check**:
Pemeriksaan on-demand di sub-tab Images yang membandingkan digest registry image lokal setiap `repo:tag` dari Docker Hub dengan digest yang kini disajikan Docker Hub untuk tag yang sama, menghasilkan status `up_to_date`, `update_available`, `not_found`, `unknown`, atau `error`; tidak menarik image dan tidak memakai kuota pull.
_Avoid_: Image scan, auto-update, Watchtower, vulnerability check

**Kubernetes Cluster Host**:
Host yang menjalankan control plane (server) k3s atau RKE2 dan dikelola Pantau lewat `kubectl` di atas SSH; klaster dideteksi per permintaan dan tidak disimpan. Node agent saja tidak didukung.
_Avoid_: K8s node, cluster entry, kubeconfig target

**Kubernetes Privilege Fallback**:
Aturan bahwa `kubectl` dijalankan sebagai akun SSH biasa dan baru memakai `sudo -n` (dengan `KUBECONFIG` k3s/RKE2) bila kubeconfig ditolak (*permission denied*) dan sudo tanpa kata sandi tersedia; kata sandi tidak pernah dipakai atau disimpan.
_Avoid_: Sudo kubectl mode, root kubectl, privileged kubectl

**Kubernetes Cluster**:
Satu klaster k3s/RKE2 yang dikenali dari UID namespace `kube-system` dan dapat dilayani oleh beberapa Host berperan server; Pantau mengelompokkan Host-nya menjadi satu entri dan beralih ke peer sehat bila Host yang diminta tidak dapat dijangkau.
_Avoid_: Cluster entry, k8s group, node group

**Gateway Host**:
Host yang sebenarnya melayani sebuah permintaan Kubernetes (Host diminta atau peer hasil failover); dilaporkan di setiap respons dan ditampilkan sebagai chip *Gateway*.
_Avoid_: Proxy host, master node, entry node

**Kube Mode**:
Kebijakan per Host atas aksi Kubernetes yang difasilitasi Pantau: `full`, `no-exec` (tanpa exec pod), atau `read-only` (tanpa mutasi dan exec). Pagar tingkat aplikasi, bukan batas keamanan Host.
_Avoid_: Permission level, RBAC role, access flag

**External Cluster**:
Klaster Kubernetes pihak ketiga atau terkelola (EKS, GKE, AKS, atau klaster mandiri) yang didaftarkan ke Pantau melalui berkas atau teks kubeconfig; dieksekusi secara lokal menggunakan `kubectl` pada server Pantau tanpa koneksi SSH ke Host control-plane.
_Avoid_: Remote cluster, cloud cluster, non-host cluster

**Kubeconfig Profile**:
Konfigurasi konteks tunggal hasil normalisasi dan minifikasi dari berkas kubeconfig yang disimpan terenkripsi di database Pantau untuk menghubungkan klaster eksternal secara aman; hanya mengizinkan kredensial inline dan plugin `exec` yang masuk daftar putih.
_Avoid_: Cluster config, kube profile, raw kubeconfig

**Git Editor**:
Komponen antarmuka untuk menjelajah pohon berkas repositori, membuat berkas baru, menghapus berkas, menyunting konten dengan CodeMirror, serta melakukan commit dan push langsung ke repositori remote. Pada Playbook Ansible tampil sebagai bagian **Files** di tab Edit pada Side Drawer; pada Workspace Terraform tampil di Workspace Modal.
_Avoid_: Web IDE, file manager, repo manager

**Repository Commit**:
Aksi merekam perubahan berkas ke riwayat Git lokal hasil clone dangkal sementara dan mendorongnya (*push*) ke branch upstream pada remote repositori tanpa opsi force push.
_Avoid_: Save file, repo patch, sync commit

**Base Commit**:
Pengenal SHA commit repositori yang sedang dilihat pengguna saat membuka atau memuat ulang Git Editor; digunakan untuk validasi *optimistic concurrency* agar perubahan ditolak jika commit remote telah berganti (409 Conflict).
_Avoid_: Parent commit, last commit, head commit

**Cloned Repository**:
Repositori Git yang di-clone penuh (SSH atau HTTPS) ke subfolder di **Repositories Folder** pada server Pantau dan dikelola dari bagian Repositories (navigasi utama): dijelajahi, diedit langsung di working tree, lalu di-commit dan di-push. Berbeda dari **Git Editor** yang clone dangkalnya sementara per request. Tanpa gate opt-in; kredensial (kunci SSH Library atau token) wajib berizin tulis untuk push.
_Avoid_: Git sync, checkout, workspace source, repo mirror

**Repository Folder**:
Pengelompokan opsional untuk Cloned Repository (dimodelkan dari Workspace Group) yang juga direktori sungguhan `<Repositories Folder>/<folder>/<repo>`: daftar menampilkan satu bagian yang dapat dilipat per folder dengan jumlah repo; memindahkan repositori ke folder lain memindahkan direktorinya.
_Avoid_: Repo group tag, project, subfolder label

**Repositories Folder** (lokasi):
Folder induk di server Pantau (pengaturan `repos_dir`, bawaan `<data>/repos`) tempat setiap Cloned Repository dibuat sebagai subfolder bernama repositori; mengubahnya hanya berlaku untuk clone baru.
_Avoid_: Workspace folder, repo root, data dir

**Repository Sync**:
Aksi **Sync** pada Cloned Repository: `fetch` branch aktif lalu `merge --ff-only`; tidak pernah merge atau rebase, dan gagal bila riwayat lokal dan remote bercabang.
_Avoid_: Git Sync (verifikasi playbook Ansible), pull

**Repository Branch**:
Branch lokal sebuah Cloned Repository yang dapat dipilih: perpindahan (*switch*) melakukan checkout branch di clone (branch yang hanya ada di `origin` dibuat darinya), pembuatan membuat branch baru di HEAD, dan penghapusan hanya menghapus branch lokal (bukan branch aktif, bukan branch di remote) setelah git mengizinkannya. Perpindahan yang akan menimpa perubahan belum di-commit ditolak.
_Avoid_: Checkout branch (sebagai istilah UI), remote branch deletion
