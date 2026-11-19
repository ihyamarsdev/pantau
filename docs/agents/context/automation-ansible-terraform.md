# Automation: Ansible & Terraform

Domain reference for Ansible and Terraform automation, gateway CLI updates and the IaC pipeline. Read this when working in `internal/ansible/`, `internal/terraform/` or `internal/gwupdate/`.

**Ansible Playbook**:
playbook YAML (nama, deskripsi, isi, extra vars bawaan) yang tersimpan di basis data Pantau.
_Avoid_: Script, template task, job definition

**Playbook Group**:
Pengelompokan logis untuk satu atau lebih Ansible Playbook pada antarmuka Pantau berbasis teks bebas `group_name` (dimodelkan dari Host Group) untuk pemisahan visual dan filter tanpa mengubah eksekusi atau hak akses.
_Avoid_: Playbook folder, playbook tag, playbook category

**Playbook Run**:
satu eksekusi Ansible Playbook terhadap sekumpulan Host (status, mode, target, exit code, log, durasi).
_Avoid_: Task, job, build

**Generated Inventory**:
inventory Ansible sementara yang dibangun otomatis dari Host terpilih (alamat, port, user, kunci SSH efektif) dan Host Group sebagai group Ansible; dihapus setelah run.
_Avoid_: Static inventory, hosts file

**Ansible Gate**:
saklar opt-in administrator (`ansible_enabled`, default mati) yang wajib aktif sebelum Playbook dapat dijalankan; dipasangkan dengan deteksi biner `ansible-playbook`.
_Avoid_: Feature flag, ansible switch

**Ansible Template**:
teks Jinja bernama (boleh bersubfolder, ≤256 KiB) yang disimpan di SQLite dan ditulis ke `templates/` di workspace setiap Playbook Run inline sehingga `template: src=` dapat merujuknya; dipakai bersama oleh semua playbook inline dan ikut System Snapshot terenkripsi.
_Avoid_: Playbook template, jinja file, task template

**Ansible File**:
berkas apa pun (termasuk biner, ≤5 MiB) yang diunggah ke Pantau dan ditulis ke `files/` di workspace setiap Playbook Run inline sehingga `copy: src=` dapat merujuknya; tidak dapat diunduh kembali dan ikut System Snapshot terenkripsi.
_Avoid_: Attachment, asset bundle, project file

**Git Playbook Source**:
Sumber Ansible Playbook berupa file YAML di repositori Git (URL https/ssh, branch/tag, path berkas, autentikasi none/token/kunci SSH Library) yang di-clone dangkal ke workspace sementara pada setiap Playbook Run; commit yang dijalankan dicatat pada run.
_Avoid_: Repository entity, git sync, playbook import

**Imported Playbook Folder**:
Salinan folder proyek Ansible yang diunggah melalui browser dan disimpan sebagai arsip ZIP privat dalam SQLite (`source=bundle`), dengan satu path YAML utama dan seluruh berkas pendukung; diekstrak sementara pada setiap Playbook Run dan ikut System Snapshot terenkripsi. Tidak mengimpor Host atau konfigurasi inventory dari proyek.
_Avoid_: Local path source, repository sync, inventory import

**Ansible Schedule**:
Jadwal cron 5-field (waktu lokal server Pantau) yang menjalankan Ansible Playbook otomatis terhadap Host/Host Group tetap; kemunculan yang terlewat lebih dari 5 menit dilewati, bukan dijalankan susulan.
_Avoid_: Cron job, timer, automation rule

**Play Recap**:
Ringkasan `PLAY RECAP` per host (ok, changed, unreachable, failed, skipped, rescued, ignored) yang diparse server saat Playbook Run selesai, disimpan bersama run, dan dikembalikan API sebagai objek.
_Avoid_: Run summary, result table, stats

**Task Tree**:
Pohon Play > Task > Host beserta status per host yang dirakit antarmuka dari log run tanpa ANSI; fitur tampilan, bukan data tersimpan.
_Avoid_: Execution graph, DAG view, job steps

**Diff Viewer**:
Tampilan per berkas hasil blok diff (`--- before`/`+++ after`/`@@`) pada run mode `diff`, dirakit antarmuka dari log tanpa ANSI.
_Avoid_: Patch view, change log, file diff

**Survey**:
Formulir variabel terstruktur milik sebuah Ansible Playbook (tipe, opsi, default, wajib) yang dijawab saat launch dan digabung ke extra vars; jawaban menimpa extra vars bawaan playbook, sedangkan extra vars eksplisit pada launch menimpa keduanya.
_Avoid_: Form vars, parameter set, input schema

**Ad-Hoc Run**:
Satu eksekusi modul Ansible (`ping`, `shell`, `setup`, …) ke Host/Host Group terpilih tanpa playbook, dengan log live dan riwayat sendiri; maksimal 3 aktif secara global, terpisah dari kuota Playbook Run.
_Avoid_: One-off task, quick command, temp playbook

**Ansible Facts**:
Hasil `setup` (`ansible_facts`) yang dikumpulkan untuk satu Host dan di-cache per host sebagai ringkasan + JSON mentah; bukan data deklarasi dan tidak ikut System Snapshot.
_Avoid_: Inventory data, host metadata, machine info

**Host & Group Explorer**:
Tab Ansible yang menampilkan pohon Host Group > Host memakai alias Generated Inventory, lengkap dengan status inspeksi Pantau, status Ping ad-hoc, dan pintasan Facts.
_Avoid_: Inventory manager, host list, group editor

**Git Sync**:
Verifikasi manual playbook bersumber Git dengan clone dangkal sementara yang melaporkan commit, branch, dan daftar YAML, lalu menyimpan waktu serta commit sync terakhir.
_Avoid_: Repo refresh, git pull, repository checkout

**Terraform Workspace**:
Unit terisolasi (config + variabel + state + riwayat run) untuk satu environment.
_Avoid_: Stack, project, environment folder

**Workspace Group**:
Pengelompokan logis untuk satu atau lebih Terraform Workspace pada antarmuka Pantau berbasis teks bebas `group_name` (dimodelkan dari Host Group) untuk pemisahan visual dan filter tanpa mengubah eksekusi, state, atau hak akses.
_Avoid_: Workspace folder, stack group, workspace tag

**Terraform Run**:
Satu siklus eksekusi (plan → policy check → cost estimate → approval → apply) pada Workspace.
_Avoid_: Job, pipeline, build

**Approval Gate**:
Tahap wajib di mana Run dengan perubahan menunggu Confirm & Apply / Discard dengan komentar wajib.
_Avoid_: Manual step, review

**State Version**:
Salinan immutable `terraform.tfstate` yang dicatat di DB setiap apply/refresh/rollback/import. DB = sumber kebenaran; file di disk dimaterialisasi sebelum tiap run.
_Avoid_: State backup, snapshot

**Workspace Lock**:
Kunci Pantau yang mencegah Run baru (manual atau otomatis selama Run aktif); Force Unlock untuk darurat.
_Avoid_: Mutex, state lock file

**Variable Set**:
Kumpulan variabel yang dibagikan ke banyak Workspace (atau global).
_Avoid_: Shared vars, env group

**Detect Variables**:
Tombol di tab Variables Workspace inline yang memindai blok `variable` pada berkas `.tf` (bukan parser HCL penuh) dan menambahkan yang dipilih sebagai variabel `terraform`; tidak untuk Workspace Git.
_Avoid_: Auto-import variables

**Linked Output**:
Tautan di tab Variables Workspace yang menyuntikkan sebuah output dari State Version terbaru Workspace lain ke variabel `terraform`; nilai diselesaikan saat Run dimulai (bukan `terraform_remote_state`), output sensitif tetap tersamarkan, variabel eksplisit bernama sama menang, siklus ditolak, dan output yang tidak tersedia menggagalkan awal Run.
_Avoid_: Remote state, data source antar workspace

**State Operation**:
Aksi di tab State (`state rm`, `state mv`, import resource) yang berjalan di bawah Workspace Lock dan selalu menghasilkan State Version baru (versi lama tetap ada, sumber tercatat mis. `state rm x`); dikonfirmasi lewat dialog yang menampilkan perintah persisnya.
_Avoid_: State edit, state surgery

**Auto-apply**:
Pengaturan opt-in per Workspace (`auto_apply`) yang meng-apply otomatis plan berstatus kebijakan `passed` dan memiliki perubahan, dengan komentar `auto-apply`. Tidak pernah untuk environment produksi; Run destroy/refresh dan Run dengan peringatan kebijakan tetap melalui Approval Gate manual.
_Avoid_: Auto-deploy, continuous apply

**Terraform Drift**:
Perbedaan infrastruktur riil vs state yang terdeteksi plan `-refresh-only` (beda dari **Drift** inspeksi Host).
_Avoid_: Config drift, mismatch

**Policy Check**:
Evaluasi aturan bawaan (Go) atas plan JSON dengan level `advisory` / `soft` / `hard`. Bukan OPA/Sentinel.
_Avoid_: Compliance scan

**Cost Estimate**:
Estimasi delta biaya bulanan via biner `infracost` bila terpasang.
_Avoid_: Billing

**Terraform Gate**:
Saklar opt-in admin (`terraform_enabled`, default mati) sebelum Run dapat dijalankan.
_Avoid_: Feature flag

**Terraform Registry**:
Penjelajah modul dan provider dari Terraform Registry publik (atau mirror lewat `tf_registry_url`) yang diambil Pantau di sisi server (cache 5 menit); dipakai untuk menyisipkan snippet ke konfigurasi dan memeriksa versi (`Check versions`) pada workspace inline. Bukan registry privat dan bukan endpoint untuk `terraform init`.
_Avoid_: Module marketplace, Private Module Catalog

**Gateway CLI Update Check**:
Pemeriksaan read-only on-demand atas versi CLI yang terpasang di server Pantau untuk sebuah *gateway* (Ansible atau Terraform), yang membandingkan versi terpasang dengan versi rilis terbaru yang dipublikasikan upstream dan melaporkan hasilnya — ada versi lebih baru, sudah terbaru, atau tidak dapat dibandingkan. Tidak memasang, tidak mengunduh, dan tidak mengubah biner apa pun.
_Avoid_: Update Checker, Self-Update, package update

**Gateway CLI Update**:
Pemasangan versi CLI gateway yang lebih baru — `ansible-playbook`, `terraform` atau `tofu` — ke direktori yang dikelola Pantau, setelah unduhannya diverifikasi terhadap checksum yang dipublikasikan upstream, tanpa menyentuh salinan sistem di `/usr/bin`. Biner yang dikelola dipakai lebih dulu, sehingga pembaruan langsung berlaku.
_Avoid_: Self-Update, package update, apt upgrade
