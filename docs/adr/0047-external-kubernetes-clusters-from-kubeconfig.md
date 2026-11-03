# 0047. Klaster Kubernetes Eksternal dari Kubeconfig

Menambahkan dukungan klaster Kubernetes eksternal (AWS EKS, GCP GKE, Azure AKS, atau klaster terkelola/remote mana pun) dengan menempelkan atau mengunggah kubeconfig, dieksekusi melalui `kubectl` lokal pada server Pantau.

Melengkapi [ADR 0043](0043-side-panel-and-kubernetes-k3s-rke2-management.md), [ADR 0044](0044-kubernetes-cluster-identity-usage-metrics-and-action-safety.md), dan [ADR 0045](0045-kubernetes-yaml-editor-workload-logs-drain-and-auto-refresh.md).

## Konteks dan Masalah

1. Pengelolaan Kubernetes di Pantau sebelumnya murni berbasis Host control-plane k3s/RKE2 melalui SSH ([ADR 0043](0043-side-panel-and-kubernetes-k3s-rke2-management.md)).
2. Banyak klaster produksi berjalan di lingkungan cloud terkelola (AWS EKS, Google Cloud GKE, Azure AKS) atau di balik firewall tanpa akses SSH langsung ke node control-plane. Operator berinteraksi dengan klaster-klaster ini melalui `kubeconfig` standar.
3. Kubeconfig adalah masukan yang berpotensi tidak tepercaya: format berkas dapat merujuk ke berkas lokal sembarang pada server Pantau (`certificate-authority`, `client-key`, `tokenFile`), atau menjalankan perintah eksternal (`exec`, `auth-provider`) dengan hak akses proses Pantau.

## Keputusan Arsitektur

1. **Mesin Eksekusi: Subproses `kubectl` Lokal**:
   Untuk klaster eksternal, Pantau tidak menggunakan SSH ke Host target dan tidak menggunakan `client-go` (menghindari penambahan dependensi biner dan kompleksitas siklus hidup client-go). Pantau menjalankan `kubectl` lokal sebagai subproses pada server Pantau itu sendiri. Biner `kubectl` harus tersedia di `PATH` server Pantau (disertakan dalam image Docker Alpine Pantau).
2. **Autentikasi Cloud lewat Exec Credential Plugin**:
   Autentikasi cloud (AWS IAM, GCP, Azure AD) dieksekusi secara lokal melalui plugin kredensial kubeconfig (`aws`, `gke-gcloud-auth-plugin`, `kubelogin`, dll.) yang mewarisi variabel lingkungan proses Pantau (seperti `~/.aws`, `AWS_REGION`, `GOOGLE_APPLICATION_CREDENTIALS`, atau token Azure). Plugin harus terpasang di `PATH` host server Pantau.
3. **Penyimpanan dan Normalisasi Kubeconfig**:
   - Kubeconfig dinormalisasi saat disimpan menggunakan `kubectl config view --raw --minify --context=<ctx> -o json` menjadi JSON konteks tunggal.
   - Disimpan di tabel `kube_clusters` pada SQLite lokal (kolom `kubeconfig`, plaintext seperti halnya `ssh_keys.private_key` pada [ADR 0035](0035-ssh-key-library.md)).
   - Kubeconfig mentah **tidak pernah dikembalikan oleh API mana pun** (hanya metadata seperti nama, server, context, provider).
   - Seluruh data klaster eksternal disertakan dalam pencadangan terenkripsi **System Snapshot** (AES-256-GCM / Argon2id, [ADR 0006](0006-encrypted-system-snapshot-and-github-sync.md)).
4. **Pengalamatan Target di Lapisan Inspector (Jalur Pintas ID Negatif)**:
   - Sebagian besar lapisan antarmuka dan inspector menerima `hostID int64`. Untuk menggunakan kembali seluruh logika tampilan dan tindakan Kubernetes yang ada tanpa menduplikasi ratusan fungsi, klaster eksternal dengan ID `N` dialamatkan di lapisan inspector sebagai `-N` (bilangan negatif).
   - Pembantu: `inspector.KubeClusterTarget(clusterID int64) int64` (menghasilkan `-clusterID`) dan `inspector.IsKubeClusterTarget(id int64) bool` (`id < 0`).
   - Ini adalah jalan pintas yang disengaja (*conscious shortcut*): Host fisik selalu memiliki `id > 0`.
5. **Kebijakan Failover & Eksekusi**:
   - Klaster eksternal memiliki satu endpoint/server API; **tidak ada failover** ke Host lain (berbeda dari klaster HA k3s/RKE2 pada [ADR 0044](0044-kubernetes-cluster-identity-usage-metrics-and-action-safety.md)).
   - `kube_mode` (`full`, `no-exec`, `read-only`) berlaku sama persis pada klaster eksternal dan ditegakkan di sisi server.
6. **Batasan Pod Exec**:
   - Fitur interaktif Pod Exec di Terminal Dock berbasis PTY SSH ke Host remote ([ADR 0043](0043-side-panel-and-kubernetes-k3s-rke2-management.md)). Karena klaster eksternal tidak memiliki koneksi SSH, **Pod Exec tidak didukung** untuk klaster eksternal. Endpoint `/api/kube/clusters/{id}/exec-cmd` menolak permintaan dengan galat HTTP 400, dan menu Exec disembunyikan/dinonaktifkan di antarmuka web.
   - Aliran log pod langsung via WebSocket (`/ws/kube/logs`) **tetap didukung** menggunakan `runner.Stream` lokal.

## Model Keamanan

Kubeconfig diperlakukan sebagai masukan berisiko keamanan tinggi:
1. **Validasi Ukuran**: Ukuran kubeconfig dibatasi maksimal 128 KB.
2. **Penolakan Kredensial Berbasis Jalur Berkas**:
   Ditolak (`ErrKubeInvalid`) jika mengandung referensi jalur berkas lokal: `certificate-authority`, `client-certificate`, `client-key`, atau `tokenFile`. Hanya data inline (`*-data` dalam base64) atau `token` langsung yang diizinkan. Ini mencegah arbitrary file read dari host server Pantau.
3. **Penolakan `auth-provider` Berbasis Perintah**:
   Konfigurasi `auth-provider` yang memuat `cmd-path` atau argumen perintah eksekusi ditolak.
4. **Daftar Putih Perintah `exec`**:
   Jika konteks menggunakan plugin `exec`:
   - Nama perintah tidak boleh mengandung pemisah jalur (`/` atau `\`).
   - Nama perintah harus berada dalam daftar putih (*allowlist*): `aws`, `aws-iam-authenticator`, `gke-gcloud-auth-plugin`, `gcloud`, `kubelogin`, `az`, `doctl`, `oci`.
   - Plugin dijalankan secara lokal di server Pantau dengan hak akses pengguna proses Pantau.

## Alternatif yang Dipertimbangkan

1. **Menggunakan `client-go`**:
   Ditolak karena menambah ukuran biner Go secara signifikan (~30+ MB), memperumit dependency tree, serta membutuhkan pengelolaan pool klien TLS/REST kompleks di dalam memori. Menggunakan biner `kubectl` lokal memanfaatkan parsing, plugin eksternal, dan penanganan protokol Kubernetes upstream yang telah teruji.
2. **Hanya Melalui Bastion Host SSH**:
   Ditolak karena mewajibkan pengguna menyediakan VM Linux perantara untuk memantau klaster managed cloud (seperti EKS/GKE/AKS), bertentangan dengan kemudahan operasional Pantau.

## Konsekuensi

- Biner `kubectl` wajib terpasang di host/kontainer Pantau (disertakan dalam Dockerfile resmi via repositori Alpine community).
- Plugin autentikasi cloud pihak ketiga (`aws-cli`, `gke-gcloud-auth-plugin`, `kubelogin`) tidak dibundel di dalam image Docker untuk menjaga ukuran image tetap ramping. Bila diperlukan, pengguna dapat memasang plugin tersebut ke container atau volume PATH.
- Kredensial cloud (file `~/.aws`, `~/.config/gcloud`, atau env vars) harus dapat diakses oleh proses Pantau jika menggunakan autentikasi plugin.
- Tidak ada akses Pod Exec interaktif ke pod di klaster eksternal.
