# 0056. Terraform Management (Terraform Cloud-Style, Local CLI)

Mengelola workspace Terraform/OpenTofu, menjalankan alur eksekusi (plan → policy check → cost estimate → approval gate → apply), melacak riwayat state version dengan diff dan rollback, menyusun variabel terpusat (Variable Sets), mengelola katalog modul privat, mendeteksi drift infrastruktur, serta mengintegrasikan webhook Git via subproses `terraform` / `tofu` lokal di server Pantau.

Melengkapi [ADR 0049](0049-ansible-playbooks-semaphore-style.md) (Ansible Playbooks via subproses lokal), [ADR 0047](0047-external-kubernetes-clusters-from-kubeconfig.md) (manajemen cluster Kubernetes eksternal), [ADR 0048](0048-cloudflare-management-via-api.md) (integrasi manajemen operasional eksternal), dan [ADR 0006](0006-encrypted-system-snapshot-and-github-sync.md) (cadangan snapshot sistem terenkripsi).

## Konteks dan Masalah

1. Pengguna Pantau mengelola infrastruktur hybrid (server bare-metal, VM cloud, Kubernetes, DNS Cloudflare) menggunakan arsitektur tanpa agen (*agentless*). Dalam siklus hidup infrastruktur modern, provisi dan deklarasi sumber daya cloud/server dikelola menggunakan Infrastructure as Code (IaC) berbasis Terraform atau OpenTofu.
2. Solusi manajemen Terraform terpusat seperti Terraform Cloud / HCP Terraform atau Spacelift menyediakan alur kerja yang baik (state locking, visual plan diff, approval gate, variabel terenkripsi, integrasi VCS). Namun, layanan tersebut berbasis SaaS berbayar, mengirimkan state file yang berisi rahasia ke cloud pihak ketiga, atau membutuhkan deployment on-premise berskala enterprise yang berat (membutuhkan Kubernetes, PostgreSQL, Redis, dan object storage S3 terpisah).
3. Pantau beroperasi dengan filosofi biner tunggal (*single-binary*) mandiri berbasis Go dan SQLite (`modernc.org/sqlite`). Menambahkan dependensi remote backend (seperti AWS S3 + DynamoDB lock atau Terraform Cloud remote state backend) bertentangan dengan prinsip mandiri dan operasional *airgapped*.
4. Eksekusi Terraform memiliki risiko keamanan kritis: konfigurasi Terraform dapat mengeksekusi kode lokal sembarang di server host controller melalui `provisioner "local-exec"`, `data "external"`, `data "local_file"`, serta modul komunitas dari Git. Tidak ada mekanisme sandbox yang andal untuk mengisolasi Terraform tanpa virtualisasi berat. Siapa pun yang dapat membuat atau memodifikasi workspace memiliki hak eksekusi setara dengan proses Pantau pada server host.

## Keputusan Arsitektur

1. **Subproses `terraform` / `tofu` Lokal**:
   Pantau tidak menggunakan pustaka RPC Terraform remote dan tidak menjalankan daemon terpisah. Pantau mengeksekusi biner `terraform` atau `tofu` yang terpasang secara lokal di sistem server Pantau (`/usr/bin/terraform`, `/usr/bin/tofu`, atau yang ditemukan di `$PATH`).
   - Pantau mendeteksi kedua biner secara otomatis (`Detect()`) dengan caching 60 detik.
   - Pengguna dapat memilih biner default (`terraform` atau `tofu`) per Workspace.
   - Pada build Windows, fungsi deteksi mengembalikan galat eksplisit `"terraform is not supported on windows"`, konsisten dengan runner Ansible ([ADR 0049](0049-ansible-playbooks-semaphore-style.md)).

2. **Backend Lokal dengan State Dimaterialisasi dari Database**:
   - Konfigurasi Terraform dieksekusi menggunakan backend standar `local`. Tidak ada remote state backend.
   - Sumber kebenaran (*single source of truth*) untuk state Terraform berada di basis data SQLite Pantau (tabel `tf_state_versions`).
   - Sebelum setiap eksekusi Plan atau Apply, Pantau mematerialisasikan state terbaru dari DB ke berkas `terraform.tfstate` di direktori kerja workspace.
   - Seusai eksekusi Apply atau Refresh berhasil, bila checksum SHA-256 state berubah, Pantau menyimpan salinan immutable `terraform.tfstate` baru sebagai versi state berikutnya ke tabel `tf_state_versions`.

3. **Workspace dan Pipeline Eksekusi (Run Lifecycle)**:
   - **Workspace**: Unit terisolasi yang menampung konfigurasi (inline files atau Git source), variabel, state, dan riwayat run untuk satu environment (`dev`, `staging`, `prod`). Direktori kerja berada di `<DataRoot>/<workspaceID>` dengan izin direktori `0700`.
   - **Alur Run**:
     `queued` → `planning` (menjalankan `init` dan `plan -out=tfplan`, lalu mengekstrak `show -json`) → evaluasi Policy as Code bawaan → estimasi biaya via `infracost` (opsional bila terpasang) → `awaiting_approval` (atau `no_changes` jika plan bersih, atau `policy_failed` bila policy `hard` melanggar) → `applying` (menjalankan `apply tfplan` pada direktori kerja yang dipertahankan) → `applied` / `errored`.
   - Run spekulatif (*speculative plan*) atau mode `drift` (`-refresh-only`) berhenti pada status `planned` tanpa menyimpan artifact plan untuk apply.
   - Direktori `runs/<RunID>` dibersihkan otomatis seusai apply selesai atau dibatalkan (`CleanupRun`).

4. **Approval Gate Mandatori**:
   - Setiap Run yang menghasilkan perubahan (`has_changes = true`) wajib melewati tahap **Approval Gate** sebelum apply dapat dijalankan.
   - Tindakan **Confirm & Apply** atau **Discard** mewajibkan input komentar alasan (minimal 3 karakter) untuk auditabilitas.
   - Jika terdapat pelanggaran policy bertingkat `soft` atau `advisory`, pengguna wajib mencentang opsi override eksplisit (`override_policy: true`) saat konfirmasi apply. Pelanggaran policy bertingkat `hard` menolak apply secara mutlak (HTTP 422).

5. **State Versioning, Diff, dan Rollback**:
   - Setiap apply, refresh, import, atau rollback mencatat entri baru pada tabel `tf_state_versions` (`id`, `workspace_id`, `serial`, `lineage`, `state`, `source`, `resource_count`, `sha256`, `created_at`).
   - Pantau mengimplementasikan parser `terraform.tfstate` v4 murni di Go tanpa memanggil CLI Terraform, untuk menyajikan inventori resource, metadata serial/lineage, dan output.
   - Fitur **State Diff**: Menghitung perbandingan struktural antara dua versi state (resource ditambah, dihapus, atau atribut diubah).
   - Fitur **Rollback State**: Mengembalikan state ke versi sebelumnya dengan menerbitkan state version baru yang mempertahankan lineage saat ini dan menaikkan serial (`current.serial + 1`), mencegah konflik state serial pada eksekusi run berikutnya.

6. **Workspace Lock & Concurrency Control**:
   - Mencegah benturan eksekusi: sebuah workspace otomatis dikunci (`locked_by = "run:<id>"`) sejak run masuk status `queued` hingga terminal. Status `awaiting_approval` tetap menahan kunci workspace sampai run diterapkan (*applied*) atau dibuang (*discarded*).
   - Administrator juga dapat mengaktifkan **Lock Manual** dengan alasan tertentu untuk membekukan perubahan.
   - Tombol darurat **Force Unlock**: Membatalkan run aktif yang memegang kunci, menghapus sisa file lock lokal (`.terraform.tfstate.lock.info`), dan melepas status lock pada database.
   - Konkurensi: Maksimal 1 run aktif per workspace (permintaan paralel ditolak HTTP 409). Maksimal 3 run aktif secara global di seluruh instalasi Pantau (permintaan ke-4 ditolak HTTP 429).

7. **Variables & Variable Sets**:
   - Variabel dibagi dalam dua kategori: `terraform` (diteruskan ke Terraform CLI) dan `env` (disuntikkan sebagai variabel lingkungan proses anak, misal `AWS_ACCESS_KEY_ID`, `TFC_TOKEN`).
   - Nilai variabel mendukung format skalar biasa maupun HCL (`hcl = true`).
   - **Variable Set**: Kumpulan variabel bersama yang dapat dikaitkan ke beberapa Workspace sekaligus atau berlaku secara global (`global = true`).
   - Hierarki resolusi (`EffectiveTFVariables`): Variabel level Workspace menimpa variabel dari Variable Set spesifik, yang menimpa Variable Set global.

8. **Policy as Code Bawaan & Estimasi Biaya (Infracost)**:
   - Pantau tidak menyematkan engine OPA/Rego atau HashiCorp Sentinel. Sebagai gantinya, Pantau menyediakan mesin evaluasi aturan bawaan berbasis Go atas representasi JSON dari plan (`show -json`):
     - `deny_destroy`: Mencegah penghapusan resource secara tidak sengaja.
     - `max_deletes`: Membatasi jumlah maksimum resource yang boleh dihapus.
     - `max_changes`: Membatasi total modifikasi resource dalam satu run.
     - `deny_resource_types`: Melarang tipe resource tertentu (misal instance berukuran besar).
     - `require_tags`: Memastikan tag wajib ada pada resource yang dibuat.
     - `deny_open_cidr`: Mencegah security group dengan CIDR terbuka (`0.0.0.0/0`).
   - Tingkat penegakan (*enforcement*): `advisory` (hanya catatan peringatan), `soft` (peringatan yang dapat di-override dengan izin saat approval), dan `hard` (kegagalan fatal yang memblokir apply).
   - **Cost Estimation**: Bila biner `infracost` terpasang di server Pantau, Pantau menjalankan `infracost breakdown --path <plan.json> --format json` untuk menghitung selisih biaya bulanan (*monthly delta*). Bila biner tidak tersedia, evaluasi biaya dilewati secara transparan (*fail-open*) tanpa memblokir alur run.

9. **Deteksi Drift Infrastruktur (Drift Detection)**:
   - Scheduler latar belakang (berjalan tiap 60 detik) memeriksa workspace yang memiliki konfigurasi `drift_interval_minutes > 0`.
   - Menjalankan plan spekulatif dengan opsi `-refresh-only` untuk mendeteksi deviasi antara infrastruktur riil dan state file.
   - Bila terdeteksi perbedaan, status workspace diperbarui menjadi `drifted` (`drift_detected = true`) dan daftar perubahan disimpan pada DB untuk ditinjau administrator.

10. **Katalog Modul Privat (Private Module Registry)**:
    - Menyediakan katalog dokumentasi modul internal (bukan endpoint publik registry protocol untuk `terraform init source`).
    - Membaca berkas modul (inline atau via Git shallow-clone), mem-parsing deklarasi input, output, resource, provider, dan versi minimum yang dibutuhkan menggunakan pemindai blok HCL murni di Go.
    - Menghasilkan dokumentasi interaktif, petunjuk penggunaan (*usage snippet* `module "x" { ... }`), dan pratinjau README.md.

11. **Integrasi Git & Webhook VCS**:
    - Workspace dapat bersumber dari berkas inline (disimpan di DB) atau repositori Git remote (HTTPS/SSH, branch, sub-direktori kerja, token/kunci SSH Library).
    - Endpoint webhook publik tanpa sesi: `POST /api/terraform/webhook/{token}` dengan token unik 32-byte hex per workspace.
    - Mendukung verifikasi signature HMAC (`X-Hub-Signature-256` untuk GitHub) atau token rahasia (`X-Gitlab-Token` untuk GitLab).
    - Webhook push yang cocok dengan branch workspace **hanya memicu Plan spekulatif/standar**, dan **tidak pernah menjalankan Apply otomatis**.

12. **Pencadangan System Snapshot Terenkripsi**:
    - Data deklarasi workspace (`tf_workspaces`), variabel (`tf_variables`), variable set (`tf_varsets` & membership), policies (`tf_policies`), modul & versi (`tf_modules`, `tf_module_versions`), serta **versi state terbaru per workspace** (sebagai versi `import`) disertakan dalam cadangan **System Snapshot** terenkripsi (AES-256-GCM / Argon2id, [ADR 0006](0006-encrypted-system-snapshot-and-github-sync.md)).
    - Riwayat eksekusi (`tf_runs`) dan log streaming tidak disertakan dalam snapshot untuk menjaga portabilitas ukuran cadangan.

## Model Keamanan

Model keamanan Terraform Management dirancang mengikuti standar pertahanan berlapis seperti fitur Ansible ([ADR 0049](0049-ansible-playbooks-semaphore-style.md)):

1. **Eksekusi Lokal Tanpa Sandbox**:
   Terraform dieksekusi di server Pantau dengan hak akses pengguna proses Pantau. Modul eksternal atau konfigurasi HCL dapat memanfaatkan `provisioner "local-exec"` atau `data "external"` untuk menjalankan perintah sembarang di mesin host. Akses administrator Pantau dengan Terraform aktif setara dengan akses shell di mesin master.

2. **Terraform Gate (Opt-In Administrator)**:
   Fitur eksekusi Terraform dinonaktifkan secara default melalui parameter `terraform_enabled` (Terraform Gate). Pengaktifan hanya dapat dilakukan oleh administrator melalui `PUT /api/terraform/status` disertai konfirmasi dialog dengan peringatan keamanan eksplisit. Seluruh endpoint operasional Terraform (kecuali `status` dan `webhook`) mengembalikan kode HTTP 403 `{"error":"terraform is disabled"}` bila gerbang ini mati.

3. **Pembersihan Variabel Lingkungan (*Clean Environment*)**:
   Proses anak Terraform dijalankan dengan lingkungan yang disterilkan secara ketat:
   - Hanya mewariskan: `PATH`, `LANG=C.UTF-8`, `HOME=<WorkspaceRoot>/home`, `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY` (bila ada), `TF_IN_AUTOMATION=1`, `TF_INPUT=0`, `CHECKPOINT_DISABLE=1`, `TF_PLUGIN_CACHE_DIR=<DataRoot>/plugin-cache`, `TF_DATA_DIR=<WorkspaceRoot>/data`, serta variabel kategori `env` dari workspace/varset.
   - Variabel lingkungan internal Pantau lainnya tidak pernah dibocorkan ke proses anak.

4. **Pencegahan Injeksi Argumen & File Variabel Aman**:
   - Variabel Terraform kategori `terraform` bertipe non-HCL ditulis ke berkas aman `pantau.auto.tfvars.json` menggunakan `encoding/json`. Variabel bertipe HCL ditulis ke `pantau-hcl.auto.tfvars`.
   - Nama kunci variabel divalidasi ketat menggunakan ekspresi reguler `^[A-Za-z_][A-Za-z0-9_-]{0,63}$`.
   - Perintah dieksekusi menggunakan `exec.CommandContext` dengan argumen terpisah langsung tanpa melewati shell (`sh -c`).
   - Berkas konfigurasi dan variabel dibuat dengan izin `0600` di dalam folder berizin `0700`.

5. **Penanganan Data Sensitif (*Sensitive Redaction*)**:
   - Variabel yang ditandai `sensitive = true` bersifat *write-only*: nilainya tidak pernah dikembalikan ke respons API JSON (`json:"-"`).
   - Log eksekusi disaring secara aktif: nilai variabel sensitif diganti dengan string `(sensitive)`.
   - Nilai output dan atribut state sensitif disembunyikan secara default pada API (`null`, `value_hidden: true`) dan hanya ditampilkan dengan parameter eksplisit `reveal=1` oleh administrator terautentikasi.
   - Unduhan berkas `.tfstate` mentah hanya dapat diakses oleh administrator dan diberi peringatan bahwa state mentah dapat mengandung rahasia infrastruktur.

6. **Validasi Batasan Ukuran & Konkurensi**:
   - Berkas inline dibatasi maksimal 50 berkas, ukuran maksimal 256 KiB per berkas, dengan nama berkas mematuhi `^[A-Za-z0-9_][A-Za-z0-9_.-]{0,99}$` dan ekstensi yang diizinkan (`.tf`, `.tf.json`, `.tfvars`, `.tftpl`, `.json`, `.yaml`, `.yml`, `.txt`, `.md`).
   - Impor state file dibatasi maksimal 16 MiB.
   - Buffer log eksekusi dibatasi maksimal 2 MiB dengan pemotongan di awal (*head truncation*).
   - Batas waktu eksekusi: Plan maksimal 30 menit, Apply maksimal 1 jam.
   - Retensi riwayat: Otomatis mempertahankan 100 run dan 100 state version terbaru per workspace.
   - Penanganan crash: Run yang masih berstatus `queued`, `planning`, atau `applying` saat server Pantau mati mendadak akan ditandai sebagai `interrupted` saat startup.

7. **Keamanan Webhook Publik**:
   - Endpoint webhook diproteksi dengan token acak berkekuatan tinggi (32 byte heksadesimal).
   - Verifikasi payload secret (HMAC SHA-256 untuk GitHub, secret token untuk GitLab) dievaluasi menggunakan perbandingan *constant-time* (`subtle.ConstantTimeCompare`) untuk mencegah serangan *timing attack*.
   - Ukuran payload webhook dibatasi maksimal 1 MiB.
   - Webhook **hanya pernah memicu proses Plan spekulatif atau standar**, dan **tidak pernah memicu Apply**.

## Alternatif yang Dipertimbangkan

1. **Integrasi Remote Backend (S3 + DynamoDB / Consul / Terraform Cloud)**:
   Ditolak karena menambah dependensi eksternal, membutuhkan konfigurasi cloud tambahan di sisi pengguna, dan merusak kesederhanaan deployment single-binary mandiri Pantau. Penyimpanan state di SQLite dengan materialisasi disk lokal memberikan performa cepat dan keandalan penuh.
2. **Menjalankan Engine OPA/Rego atau Sentinel**:
   Ditolak karena menyematkan runtime Rego ke dalam binary Pantau akan membengkakkan ukuran biner dan mengharuskan pengguna mempelajari bahasa DSL kebijakan baru. Aturan kebijakan bawaan Go mencakup 95% kebutuhan tata kelola infrastruktur umum (larangan destroy, batas perubahan, tag wajib, sanitasi CIDR).
3. **Penyimpanan Variabel Terenkripsi Terpisah di DB**:
   Ditolak demi konsistensi arsitektur. Kunci SSH, token Git, dan token Cloudflare di Pantau disimpan langsung di tabel SQLite yang dilindungi izin berkas lokal sistem operasi (0600), dan baru dienkripsi saat diekspor ke System Snapshot terenkripsi (Argon2id + AES-256-GCM).
4. **Auto-Apply Otomatis via Webhook Git**:
   Ditolak secara tegas. Eksekusi infrastruktur produksi tanpa pengawasan manusia (*unattended apply*) memiliki risiko kehancuran sumber daya (*unintended destruction*). Penerapan perubahan infrastruktur wajib melalui telaah visual plan dan konfirmasi eksplisit di Approval Gate.

## Konsekuensi

1. **Kebutuhan Dependensi Host & Ukuran Image**:
   - Server Pantau membutuhkan biner `terraform` atau `tofu` terpasang di jalur `$PATH`.
   - Image Docker resmi menyertakan `terraform` (rilis resmi HashiCorp, versi dipin lewat build arg `TERRAFORM_VERSION`, checksum SHA256 diverifikasi saat build), `opentofu` (paket community Alpine, menambah sekitar ~120 MB), dan `infracost` (diunduh dari rilis GitHub saat build, sesuai `TARGETARCH`). Pengguna non-Docker memasang `terraform` atau `tofu` sendiri di `$PATH`. `infracost` memerlukan `INFRACOST_API_KEY` (variabel `env` pada Workspace atau Variable Set).
2. **Ketiadaan Dukungan Eksekusi di Windows**:
   Meskipun biner Pantau di Windows dapat dikompilasi dengan sukses, subsistem runner Terraform dinonaktifkan di Windows (`Detect()` mengembalikan galat tidak didukung), konsisten dengan batasan runner Ansible.
3. **Tanggung Jawab Pengelolaan State**:
   Karena backend beroperasi secara lokal, administrator bertanggung jawab menjaga keutuhan basis data `pantau.db` dan rutin melakukan pencadangan melalui fitur System Snapshot terenkripsi.
