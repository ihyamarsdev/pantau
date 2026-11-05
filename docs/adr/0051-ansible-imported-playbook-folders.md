# 0051. Impor Folder Proyek Ansible

Melengkapi [ADR 0049](0049-ansible-playbooks-semaphore-style.md) dan [ADR 0050](0050-ansible-git-playbooks-and-cron-schedules.md). **Digantikan oleh [ADR 0054](0054-ansible-copy-import-merged-editor-and-variables.md):** sumber bundle, arsip ZIP, dan proyek bersama dihapus; impor kini menyalin teks YAML menjadi playbook inline yang dapat diedit. Dokumen ini dipertahankan sebagai riwayat.

## Konteks

Playbook lokal sering bergantung pada `roles/`, `templates/`, `files/`, `group_vars/`, `host_vars/`, dan collections yang sudah terpasang di `.ansible/collections/`. Menyalin satu YAML ke editor inline menghilangkan dependensi tersebut; mengakses path lokal controller juga tidak bekerja bila browser dan server Pantau berada pada mesin berbeda.

## Keputusan

- Tambahkan **Impor Folder** menggunakan pemilih direktori browser. Pengguna memilih YAML utama; ADR 0053 memperluasnya menjadi pilihan jamak dengan deteksi playbook, tetap tanpa menganggap semua YAML sebagai playbook.
- `POST /api/ansible/playbooks/import` menerima multipart dengan part `files` berfilename relatif proyek (field metadata: lihat ADR 0053, yang menggantikan `name`/`bundle_path` tunggal dengan daftar `playbooks`). Route memakai autentikasi serta Ansible Gate yang sama dengan CRUD/run. Pembacaan multipart streaming mendukung proyek dengan collections yang melebihi batas default 1.000 part `ParseMultipartForm`.
- Server menyusun ZIP menggunakan pustaka standar dan menyimpannya dengan `source=bundle` dan `bundle_path` (awalnya pada `ansible_playbooks.bundle_data`, kini pada tabel `ansible_projects`; lihat ADR 0053). API tidak menyerialisasi arsip, dan list tidak membacanya dari database.
- Batas impor: 10.000 berkas, 64 MiB total isi, 64 MiB ZIP, dan YAML utama 1 byte–256 KiB (sejak ADR 0053 UI hanya mengunggah berkas YAML yang dipilih, jadi batas ini jarang tercapai). Body HTTP dibatasi 80 MiB untuk menampung overhead multipart. Metadata dibatasi 64 KiB per field; nama tetap 1–64 byte.
- Nama berkas harus relatif dan bersih: traversal, path absolut, backslash, drive/colon, tanda kutip ganda (diubah browser saat serialisasi multipart), karakter kontrol, `.git`, duplikasi, file dengan parent non-direktori, symlink dan special file ditolak. ZIP diperiksa ukuran hasil dekompresi dan checksum sebelum eksekusi; ekstraksi memakai `os.Root` ke subdirektori workspace privat. Direktori berizin `0700`, berkas privat `0600`. Browser tidak mengirim mode berkas, jadi server memberi `0700` pada berkas yang diawali shebang `#!` agar skrip tetap dapat dieksekusi.
- Setiap run mengekstrak arsip, menjalankan YAML terpilih dengan working directory root proyek, lalu membersihkan workspace. Role dan collection search path mencakup root proyek serta `.ansible/`. Generated Inventory bernama acak ditempatkan di root proyek agar root `group_vars/` dan `host_vars/` tetap ditemukan untuk YAML di subfolder; tidak menimpa berkas unggahan. Kunci SSH dan konfigurasi tetap dibuat di workspace controller di luar proyek.
- Inventory unggahan serta `ansible.cfg` tidak menggantikan Generated Inventory dan konfigurasi aman Pantau; pengaturan `ansible.cfg` global kini diatur di Ansible Settings ([ADR 0053](0053-ansible-shared-projects-library-and-config.md)). Tidak ada pemasangan otomatis `ansible-galaxy` saat run atau dukungan path lokal di luar unggahan. Alias/pola `hosts:` harus cocok dengan Host dan Host Group Pantau.
- Editor dapat memperbarui metadata dan path ke YAML lain di arsip yang sama melalui PUT biasa. Membuat sumber bundle lewat JSON tanpa impor ditolak. Beralih ke inline/Git menghapus tautan ke bundle. Isi folder diganti lewat `POST /api/ansible/playbooks/{id}/import` (hanya untuk playbook `source=bundle`; field dan semantik terkini di ADR 0053): ID, Schedule, dan riwayat run dipertahankan; run yang sudah berjalan memakai salinannya sendiri.
- Arsip ikut **System Snapshot** terenkripsi dan restore (sejak ADR 0053 sebagai proyek bersama, bukan per playbook).

## Keamanan dan Batasan

Folder dapat berisi `.env`, token, private key, atau vault; UI memperingatkan pengguna sebelum upload. Isi tersimpan plaintext di SQLite seperti SSH Key Library dan baru terenkripsi dalam System Snapshot. Berkas tidak dipublikasikan melalui API atau activity log, kecuali berkas kode Ansible yang dapat dilihat melalui endpoint `contents` ([ADR 0052](0052-ansible-playbook-contents-and-variables.md)); `.env`, kunci, dan berkas non-teks tidak pernah dikembalikan. Hanya unggah proyek tepercaya: Ansible dan plugin lokal tetap berjalan dengan hak proses Pantau, bukan sandbox.

Pemilih browser menyalin berkas biasa, bukan struktur symlink/izin Unix asli. `.git`, `__pycache__`, dan `.DS_Store` dikecualikan oleh UI; `.ansible/` dan `collections/` kini dikecualikan secara default (opsi UI untuk menyertakannya) karena collections dan role dikelola lewat Library ([ADR 0053](0053-ansible-shared-projects-library-and-config.md)). Import tidak membaca server filesystem dari path yang dikirim klien dan tidak otomatis menjalankan playbook.

Salinan per playbook semula dipilih untuk menjaga CRUD, scheduling, dan backup sederhana; deduplikasi ditunda hingga ada kebutuhan nyata. Kebutuhan itu muncul (satu folder, banyak playbook): [ADR 0053](0053-ansible-shared-projects-library-and-config.md) menggantikannya dengan proyek bersama.
