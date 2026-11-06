# 0058. Git Edit, Commit, dan Push untuk Playbook Ansible dan Workspace Terraform

Menambahkan kemampuan menjelajah berkas repositori, menyunting, membuat berkas baru, menghapus berkas, melakukan commit, dan push langsung dari dashboard Pantau untuk Playbook Ansible bersumber Git (`source=git`) dan Workspace Terraform bersumber Git (`source=git`). Seluruh operasi berjalan stateless via clone dangkal sementara per permintaan dan berada di balik gate otorisasi masing-masing (`ansible_enabled` dan `terraform_enabled`).

Melengkapi [ADR 0050](0050-ansible-git-playbooks-and-cron-schedules.md) (Playbook Ansible dari Git), [ADR 0056](0056-terraform-management-terraform-cloud-style.md) (Workspace Terraform dari Git dan Webhook VCS), dan [ADR 0057](0057-ansible-ops-recap-tasktree-diff-survey-adhoc-facts-explorer.md) (Git Sync Playbook).

## Konteks dan Masalah

1. Sebelumnya, integrasi Git pada playbook Ansible ([ADR 0050](0050-ansible-git-playbooks-and-cron-schedules.md), [ADR 0057](0057-ansible-ops-recap-tasktree-diff-survey-adhoc-facts-explorer.md)) dan workspace Terraform ([ADR 0056](0056-terraform-management-terraform-cloud-style.md)) beroperasi secara **hanya-baca (read-only)**: Pantau meng-clone repositori saat run, sync, atau webhook plan dieksekusi, tetapi operator tidak dapat mengubah berkas langsung dari antarmuka Pantau.
2. Saat terjadi insiden atau kebutuhan perbaikan konfigurasi kecil (misalnya memperbaiki kesalahan ketik pada task Ansible, memperbarui versi image container di Terraform, atau memodifikasi variabel HCL), operator terpaksa keluar dari Pantau menuju antarmuka web Git eksternal atau terminal lokal untuk mengedit, commit, dan push, kemudian kembali ke Pantau untuk sinkronisasi ulang.
3. Repositori remote dapat berubah di server upstream saat operator sedang membuka editor di browser. Menyimpan perubahan secara membabi-buta tanpa validasi commit dasar (*optimistic concurrency*) dapat menimpa pekerjaan tim lain atau merusak riwayat git.
4. Operasi commit dan push memerlukan interaksi dengan sistem remote melalui kredensial (HTTPS personal access token atau kunci SSH). Kredensial yang sebelumnya hanya memerlukan izin baca kini wajib memiliki izin tulis, dan pesan kesalahan otentikasi maupun URL berkredensial tidak boleh bocor ke log atau antarmuka.
5. Menjalankan `git commit` dan `git push` pada server controller berpotensi memicu eksekusi hook lokal (`core.hooksPath`, `pre-commit`, `post-commit`), GPG signing tak terduga, atau benturan push non-fast-forward dan push paralel ke branch yang sama.

## Keputusan Arsitektur

1. **Komponen Backend Bersama di `internal/ansible` (`git_edit.go`)**:
   - Fungsi penjelajahan pohon repositori (`ReadRepoTree`), pembacaan berkas (`ReadRepoFile`), dan eksekusi commit/push (`CommitAndPush`) diimplementasikan di paket `internal/ansible` menggunakan pustaka standar Go dan biner `git` lokal.
   - Mengikuti preseden `ansible.CloneGitRepo` yang juga dipakai ulang oleh modul Terraform ([ADR 0056](0056-terraform-management-terraform-cloud-style.md)), komponen ini dipakai bersama oleh endpoint Ansible dan Terraform tanpa duplikasi logika git.

2. **Stateless Shallow Clone per Permintaan**:
   - Pantau tidak menyimpan checkout git permanen di disk server.
   - Setiap permintaan baca daftar berkas (`ReadRepoTree`), baca isi berkas (`ReadRepoFile`), maupun commit & push (`CommitAndPush`) melakukan clone dangkal (`git clone --depth 1 --single-branch --no-tags`) ke dalam direktori kerja sementara berizin `0700` (`os.MkdirTemp`).
   - Direktori sementara dijamin selalu dihapus bersih segera setelah operasi selesai (`defer os.RemoveAll`).

3. **Operasi Berkas dan Penjelajahan Pohon**:
   - `ReadRepoTree`: Mengembalikan commit HEAD, nama branch aktif, daftar berkas reguler terurut path, dan flag `truncated` bila jumlah berkas melebihi 2.000 entri. Symlink dan berkas/direktori di bawah `.git/` diabaikan.
   - `ReadRepoFile`: Membaca berkas reguler UTF-8 valid tanpa karakter NUL dengan ukuran maksimal 1 MiB. Berkas non-teks atau biner ditolak dengan galat `ErrNotText` (HTTP 415), dan symlink ditolak. Path relatif terhadap root repositori (pada Terraform, path adalah relatif terhadap root repo, bukan sub-direktori `git_path`).

4. **Optimistic Concurrency Control (`BaseCommit`)**:
   - `CommitSpec` menerima `BaseCommit` (SHA commit yang dilihat operator saat membuka atau memuat ulang Git Editor).
   - Setelah clone dangkal dilakukan, Pantau membandingkan HEAD hasil clone dengan `BaseCommit`. Bila `BaseCommit != ""` dan HEAD berbeda, operasi ditolak seketika dengan galat `ErrRepoChanged` (HTTP 409 Conflict) sebelum ada modifikasi berkas apa pun.
   - UI menampilkan pesan ramah bahwa repositori telah diperbarui oleh pihak lain beserta tombol Muat Ulang.

5. **Pilihan Branch Baru (`NewBranch`) alih-alih Pull Request**:
   - `CommitSpec.NewBranch`: Jika string kosong, commit di-push langsung ke branch kerja yang sedang dikonfigurasi (`g.Branch` atau branch default remote).
   - Jika `NewBranch` diisi, branch baru divalidasi dengan `ValidateGitBranch` dan dipastikan tidak sama dengan branch aktif saat ini, dibuat dari HEAD via `git checkout -b <NewBranch>`, dan di-push ke `refs/heads/<NewBranch>`.
   - Fitur Pull Request (PR/MR) secara sadar ditetapkan sebagai **non-goal**; operator dapat membuat branch baru dan membuka PR melalui antarmuka Git hosting masing-masing.

6. **Tanpa Force Push**:
   - Perintah push dijalankan secara ketat tanpa opsi `--force`:
     `git push origin HEAD:refs/heads/<branch>`
   - Jika remote menolak push (misalnya karena fast-forward rejection), operasi menghasilkan galat `ErrPushRejected` (HTTP 409 Conflict).

7. **Serialisasi Mutex per `URL + Branch`**:
   - Mutex global dalam memori diserialkan berdasarkan kombinasi URL repositori dan nama branch tujuan (`url + "@" + branch`).
   - Mencegah dua request commit paralel ke branch yang sama saling berbenturan saat melakukan clone, commit, dan push.

8. **Restriksi Path dan Batasan Ukuran**:
   - Path berkas: path relatif bersih, hanya pemisah `/`, tanpa `..`, `.`, kosong, tanpa awalan `/`, panjang maksimal 255 karakter, dan tidak boleh mengandung komponen `.git` (case-insensitive) maupun karakter kontrol/NUL.
   - Pencegahan traversal via symlink: setiap komponen direktori divalidasi melalui `os.Lstat` untuk memastikan tidak ada komponen yang merupakan symlink yang dapat menulis ke luar direktori clone. Hapus hanya berlaku untuk berkas reguler.
   - Batasan kuota payload: maksimal 50 perubahan berkas per commit, maksimal 1 MiB per konten berkas, total ukuran seluruh konten maksimal 5 MiB, konten wajib teks UTF-8 valid tanpa byte NUL.
   - Pesan commit: 1–500 karakter (setelah dipangkas), tanpa byte NUL.
   - Author: `author_name` 1–100 karakter tanpa karakter `<>` dan newline; `author_email` 1–120 karakter tervalidasi format email; jika kosong menggunakan default `Pantau <pantau@localhost>`.

9. **Pembersihan Lingkungan Eksekusi Git dan Pencegahan Hook**:
   - Eksekusi `commit` mematikan penandatanganan GPG dan hook secara mutlak menggunakan argumen:
     `-c commit.gpgsign=false -c core.hooksPath=/dev/null -c credential.helper=`
   - Pesan commit dikirimkan melalui stdin (`-F -`) atau argumen terpisah (`-m`) tanpa melewati shell, mencegah injeksi perintah shell.
   - Identitas author dan committer disuntikkan melalui variabel lingkungan terisolasi `GIT_AUTHOR_NAME`, `GIT_AUTHOR_EMAIL`, `GIT_COMMITTER_NAME`, dan `GIT_COMMITTER_EMAIL`.

10. **Autentikasi dan Penanganan Sanitasi Token**:
    - Push menggunakan mekanisme kredensial yang identik dengan `CloneGitRepo`: header `http.<host>.extraHeader` untuk token HTTPS dan `GIT_SSH_COMMAND` dengan flag isolasi untuk kunci SSH Library.
    - Kredensial token HTTPS atau kunci SSH wajib memiliki **izin TULIS (write permission)** pada repositori remote.
    - Token dan URL berkredensial tidak pernah dicatat ke log sistem maupun dikembalikan dalam respons API: semua keluaran error git disaring melalui `sanitizeGitError`.

11. **Efek Samping Sinkronisasi dan Webhook**:
    - Ansible: Setelah commit berhasil di-push ke branch yang dipakai playbook (bukan `new_branch`), Pantau otomatis memanggil `SetAnsiblePlaybookGitSync(id, now, sha)` untuk memperbarui catatan sync terakhir.
    - Terraform: Push ke branch yang dipantau workspace secara natural memicu webhook VCS jika diaktifkan ([ADR 0056](0056-terraform-management-terraform-cloud-style.md)), menghasilkan antrean Plan spekulatif otomatis.

## Model Keamanan

1. **Gerbang Otorisasi (Gate Guards)**:
   - Akses endpoint commit Ansible dilindungi `checkAnsibleGate` (`ansible_enabled = 1`).
   - Akses endpoint commit Terraform dilindungi `checkTerraformGate` (`terraform_enabled = 1`).
   - Endpoint hanya melayani resource dengan konfigurasi `source = "git"`; permintaan pada playbook/workspace inline ditolak HTTP 400.
2. **Kebutuhan Izin Tulis Token dan Kunci SSH**:
   - Penggunaan Git sebelumnya di Pantau bersifat read-only. Untuk menggunakan fitur edit/commit/push, token HTTPS harus memiliki hak tulis:
     - **GitHub Fine-Grained Personal Access Token**: Membutuhkan izin **Repository permissions -> Contents: Read and write**.
     - **GitHub Personal Access Token (classic)**: Membutuhkan scope **`repo`** (atau `public_repo` untuk repositori publik).
     - **GitLab Access Token**: Membutuhkan role Developer/Maintainer dengan scope **`write_repository`**.
     - **SSH Deploy Key**: Kunci SSH yang dipasang di repositori harus memiliki opsi **Allow write access** aktif.
   - Token dengan izin baca-saja (*read-only*) akan berhasil saat membaca file tetapi gagal saat push dengan status HTTP 502 dan pesan sanitasi git.
3. **Pemberangusan Hook dan Eksekusi Tak Diinginkan**:
   - Opsi `core.hooksPath=/dev/null` dan `commit.gpgsign=false` menjamin biner `git` tidak akan mengeksekusi skrip lokal apa pun yang mungkin ada di dalam repositori hasil clone saat membuat commit.
4. **Isolasi Filesystem dan Pencegahan Path Traversal**:
   - Pemeriksaan `os.Lstat` per segmen jalur menjamin pembuatan dan penghapusan berkas tidak dapat mengeksploitasi symlink untuk menulis berkas di luar ruang clone sementara server Pantau.

## Alternatif yang Dipertimbangkan

1. **Menyimpan Checkout Repositori Permanen di Server**:
   - Ditolak. Mempertahankan klon lokal permanen memerlukan manajemen state disk, penanganan merge conflict saat pull, penguncian filesystem yang rumit, dan berisiko meninggalkan data repositori tidak sinkron dengan remote. Pola clone dangkal sementara per request terbukti stateless, aman, dan selaras dengan filosofi Pantau.
2. **Mekanisme Pull Request Otomatis**:
   - Ditolak. Mengintegrasikan pembuatan PR/MR memerlukan keterikatan erat dengan API spesifik vendor (GitHub API, GitLab API, Bitbucket API, Gitea) beserta token OAuth atau API token khusus platform. Menyediakan opsi `NewBranch` mempertahankan sifat agnostik git standar.
3. **Pustaka Go Pure Git (`go-git`)**:
   - Ditolak. Tetap konsisten dengan [ADR 0050](0050-ansible-git-playbooks-and-cron-schedules.md); biner `git` bawaan sistem sudah teruji menangani seluruh transport protokol HTTPS/SSH dan kredensial header tanpa menambah dependensi dependensi pihak ketiga yang besar.
4. **Mendukung Force Push**:
   - Ditolak demi keselamatan operasional. Pantau adalah alat manajemen dan operasional; mengizinkan force push dari antarmuka web berisiko menghapus riwayat commit tim lain secara tidak sengaja.

## Konsekuensi

1. **Operasional Tanpa Keluar Konteks**: Operator dapat menavigasi berkas, melakukan perbaikan berkas konfigurasi, serta melakukan commit dan push langsung dari dashboard tanpa memerlukan perkakas git eksternal.
2. **Kebutuhan Izin Kredensial yang Lebih Tinggi**: Administrator yang mengaktifkan fitur ini wajib memastikan token HTTPS atau SSH Deploy Key yang disimpan di Pantau memiliki izin tulis.
3. **Beban Komputasi dan Jaringan**: Setiap operasi penjelajahan pohon, pembacaan berkas, atau commit melakukan satu panggilan `git clone --depth 1` ke server remote. Operasi ini dibatasi oleh timeout (60 detik untuk tree/file, 120 detik untuk commit).
4. **Pembaruan terhadap ADR Sebelumnya**: Menegaskan pembaruan atas pernyataan bahwa Git di Pantau beroperasi secara hanya-baca pada [ADR 0050](0050-ansible-git-playbooks-and-cron-schedules.md), [ADR 0054](0054-ansible-copy-import-merged-editor-and-variables.md), [ADR 0056](0056-terraform-management-terraform-cloud-style.md), dan [ADR 0057](0057-ansible-ops-recap-tasktree-diff-survey-adhoc-facts-explorer.md).
