# 0059. UI Ansible: Tabel Playbook Padat dan Side Drawer

Merancang ulang seluruh bagian Ansible pada dashboard: daftar playbook menjadi tabel padat, semua tugas per playbook (jalankan, edit, riwayat, jadwal) berpindah dari lima modal terpisah ke satu **Side Drawer**, dan fitur operasional ADR 0057 yang sebelumnya hanya ada di backend (Play Recap, Task Tree, Diff, Survey, Host Explorer, Ping, Facts, Ad-hoc) mendapat antarmuka.

Melengkapi [ADR 0049](0049-ansible-playbooks-semaphore-style.md), [ADR 0057](0057-ansible-ops-recap-tasktree-diff-survey-adhoc-facts-explorer.md), dan [ADR 0058](0058-git-edit-commit-push-for-ansible-and-terraform.md). Tidak mengubah API selain penambahan kolom ringkasan (di bawah) dan model keamanan Ansible Gate.

## Konteks dan Masalah

1. Daftar playbook berupa kartu besar yang hanya memuat nama, deskripsi, dan badge `never_run`. Hasil run terakhir, durasi, jadwal berikutnya, dan ringkasan recap tidak terlihat tanpa membuka riwayat.
2. Satu playbook memerlukan lima modal (edit, launch, viewer, jadwal, impor) dan enam tab tingkat atas. Schedule terpisah dari playbook yang dijadwalkan; Templates, Files, dan Library adalah sumber daya pendukung yang tersebar di tiga tab.
3. Form Launch kosong setiap kali dibuka: target dan opsi terakhir tidak diingat dan tidak ada ringkasan sebelum run.
4. ADR 0057 menetapkan UI untuk Recap, Task Tree, Diff, Survey, Explorer, Ping, Facts, dan Ad-hoc, tetapi hanya backend-nya yang masuk. `index.html` tidak memiliki satu pun fungsi untuk itu.

## Keputusan

1. **Tabel padat + Side Drawer**: tab Playbooks menampilkan tabel (status, nama, grup, run terakhir, hasil recap, jadwal) yang dapat diurutkan, difilter, dan dikelompokkan. Klik baris membuka Side Drawer di sisi kanan (720 px, mode lebar 1180 px, layar penuh di bawah 900 px) dengan tab **Overview | Edit | Runs | Schedule**. Run Viewer (Log, Recap, Tasks, Diff) membuka tampilan berikutnya di drawer yang sama dengan tombol Kembali. Modal hanya dipertahankan untuk dialog formulir pendek (impor folder) dan konfirmasi.
2. **Tab tingkat atas**: Playbooks, Runs (feed global + jadwal), Hosts (Explorer, Ping, Facts, Ad-hoc), Resources (Templates, Files, Library), Settings (Ansible Gate dan `ansible.cfg`).
3. **Modul front-end terpisah**: UI Ansible dipindahkan keluar dari `index.html` ke `internal/web/static/app/ansible/*.js` dan `*.css` (disematkan lewat `embed`), dilayani sebagai satu bundel `/app/ansible.js` dan `/app/ansible.css` (urutan nama berkas, `ETag` + gzip, `Cache-Control: no-cache`). Setiap modul adalah IIFE dengan namespace `window.Ans`, i18n sendiri (`ans_*`, EN dan ID), CSS sendiri berawalan `ans-`, tanpa handler `onclick` inline, dan seluruh teks pengguna melewati `Ans.esc`. Kontrak antarmodul didokumentasikan di `.scratch/ansible-ui-redesign/spec.md`.
4. **Ringkasan daftar di backend**: `GET /api/ansible/playbooks` menambah `last_run_id`, `last_duration_ms`, `last_targets`, `last_recap`, `schedule_count`, `next_run_at` dalam satu query (tanpa N+1); `GET /api/ansible/runs` menerima filter `playbook_id` dan `status`. Perubahan hanya penambahan.
5. **Launch yang mengingat**: pengaturan launch terakhir per playbook (mode, host, opsi lanjutan, jawaban survey) disimpan di `localStorage`; nilai bertipe `password` tidak pernah disimpan.

## Konsekuensi

- Satu tempat per playbook untuk menjalankan, menyunting, melihat riwayat, dan menjadwalkan; hasil run terakhir terlihat langsung di tabel.
- `index.html` berkurang sekitar 3.400 baris. Pengujian UI Ansible berpindah ke kontrak bundel (id tab/panel, paritas kunci i18n EN/ID, tidak ada handler inline, fungsi murni parser log).
- Git Editor ADR 0058 untuk playbook Ansible kini berada di panel Files pada tab Edit di dalam drawer (bukan Workspace Modal); aturan keamanan dan concurrency ADR 0058 tidak berubah.
- Ketergantungan baru: komponen drawer dan host picker bersama di `00-core.js`; modul lain hanya bergantung pada kontrak `Ans.*`.

## Addendum: Playbook Parts

- Editor playbook inline memecah satu play menjadi **Pre-tasks / Roles / Tasks / Post-tasks / Handlers / Raw**. YAML mentah tetap satu-satunya sumber kebenaran (kolom `content` tidak berubah); bagian hanyalah tampilan turunan.
- Pemecahan dan penggabungan dilakukan di server dengan `yaml.v3` Node (`POST /api/ansible/playbooks/outline` dan `/compose`) agar komentar dan gaya kutip per node terjaga. Browser tidak punya parser YAML. Penggabungan hanya dijalankan bila sebuah bagian diubah, karena menggabung merapikan indentasi; sekadar membuka bagian tidak menulis ulang Raw.
- Playbook dengan beberapa play atau YAML yang tidak bisa di-parse hanya menyediakan Raw. Key play yang tidak punya tab sendiri (`name`, `hosts`, `become`, `vars`, ...) tidak punya tab: diedit di Raw dan dibawa apa adanya saat penggabungan. Urutan hasil gabungan: pre_tasks, roles, tasks, post_tasks, handlers.
- Role dipilih dari `GET /api/ansible/roles`: role Library, role koleksi Library (`namespace.collection.role`), dan path role sistem, urutan sama dengan `ANSIBLE_ROLES_PATH` saat run. Role yang belum terpasang boleh ditambahkan tetapi ditandai.
- Hasil analisis variabel memuat `role` (role yang mendefinisikan, atau satu-satunya role yang merujuk) sehingga tab Variabel dikelompokkan per role dan punya pencarian.
- Save mempertahankan posisi pengguna (tab drawer, tab editor, instance editor). Playbook inline diperiksa dan diformat lebih dulu oleh `POST /api/ansible/playbooks/format` (daftar play, tiap play punya `hosts` atau `import_playbook`, tanpa key ganda; indentasi 2 spasi lewat `yaml.v3` Node sehingga komentar terjaga). Pemeriksaan gagal = tidak ada yang disimpan, editor pindah ke Raw dengan pesan beserta nomor baris. Pemeriksaan ini sengaja hanya struktur YAML, bukan `ansible-playbook --syntax-check` (butuh inventory dan role terpasang).

## Addendum: Terraform Workspace Drawer

- Workspace Terraform tidak lagi dibuka sebagai modal (85vw × 84vh), melainkan sebagai side drawer yang menempel di tepi kanan dengan pola yang sama seperti drawer Ansible: di bawah header 56 px, di atas footer 36 px, dengan backdrop, tombol perlebar (pilihan diingat di `localStorage`), tombol Esc menutup, dan layar penuh di bawah 900 px. Tab-nya: **Overview | Runs | State | Variables | Drift | Settings**; tab Overview menampilkan kartu Overview/Outputs/Recent Runs berdampingan mengikuti lebar drawer (container query).
- CSS-nya berada di bundel `internal/web/static/app/terraform/10-drawer.css` yang dilayani sebagai `/app/terraform.css` (penggabungan nama berkas sama seperti `/app/ansible.css`), terhubung dari `index.html`. Drawer z-index 42 (di bawah `.modal` 50) sehingga dialog aksi `tfDlg1`/`tfDlg2` tetap menumpuk di atasnya; drawer juga memuat kembali aturan bersama `.ans-section-panel`/`.ans-sched-tbl` yang hilang saat redesign Ansible dan merusak tata letak panel Terraform.
- Logika Terraform di `index.html` tidak berubah secara semantik: fungsi `tf*`, polling run, dan id elemen di dalam workspace view (`tfWsModalName`, `tfWsHeadBadges`, `tfWsTabs`, `tfWsBody`) tetap sama; hanya pembuka/penutup (dulu `openModal`/`closeModal`) yang berganti ke drawer.
- Tab Configuration dan Settings dipasang lewat registry `tfTabRenderers` dari bundel `/app/terraform.js` (modul 2x-config, 3x-settings); `tfDirtyGuard` mencegah kehilangan edit saat pindah tab/tutup drawer.
