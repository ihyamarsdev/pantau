# 0055. Template Jinja dan Berkas Unggahan untuk Playbook Inline

Melengkapi [ADR 0054](0054-ansible-copy-import-merged-editor-and-variables.md) dan [ADR 0049](0049-ansible-playbooks-semaphore-style.md). ADR 0054 menyimpan playbook inline sebagai teks saja, sehingga `template: src=` dan `copy: src=` tidak punya berkas untuk dirujuk. ADR ini menambah tempat penyimpanan berkas pendukung tanpa mengembalikan mekanisme bundle/ZIP yang dihapus di sana.

## Konteks

Playbook inline dijalankan dari workspace sementara yang hanya berisi `playbook.yml`, inventory, `ansible.cfg`, `extra.json`, dan kunci. Task `ansible.builtin.template` dan `ansible.builtin.copy` mencari `src` relatif terhadap folder playbook (`templates/` dan `files/`), jadi playbook inline yang memakainya gagal dengan "Could not find or access". Playbook Git tidak bermasalah karena seluruh repositori di-clone; role dari Library membawa `templates/` dan `files/` sendiri.

## Keputusan

- **Dua tab baru di bagian Ansible: Templates dan Files**, sejajar dengan Playbooks, Schedules, Library, dan Settings. Templates adalah editor teks Jinja (nama + isi); Files adalah unggahan berkas apa pun, termasuk biner.
- **Global, bukan per playbook.** Satu tabel `ansible_assets (kind, name, data BLOB, size)` dengan `UNIQUE(kind, name)`; `kind` bernilai `template` atau `file`. Semua playbook inline berbagi isinya, seperti Library. Nama adalah path relatif bersih yang boleh berisi subfolder (`nginx/site.conf.j2`) dan memakai aturan yang sama dengan `ValidateBundleFilePath` (tanpa `..`, path absolut, backslash, titik dua, tanda kutip, karakter kontrol, atau segmen `.git`), maksimal 200 karakter.
- **Pemakaian saat run.** `RunSpec.Assets` ditulis ke `<workspace>/templates/<nama>` dan `<workspace>/files/<nama>` di sebelah `playbook.yml` (mode `0600`, direktori `0700`, `O_EXCL` agar nama yang sekaligus berkas dan folder ditolak). Ansible menemukannya lewat pencarian bawaan, jadi `template: src=nginx/site.conf.j2` dan `copy: src=certs/ca.pem` bekerja tanpa jalur khusus. Dimuat dari satu tempat (`launchAnsibleRun`), sehingga run manual dan jadwal sama. Workspace tetap selalu dihapus seusai run.
- **Hanya sumber inline.** Playbook Git memakai `templates/` dan `files/` dari repositorinya sendiri; aset global tidak ditulis ke dalam clone supaya tidak menimpa atau diam-diam bercampur dengan isi repositori.
- **API** (autentikasi dan Ansible Gate yang sama dengan rute Ansible lain):
  - `GET /api/ansible/assets[?kind=]` → daftar `{id, kind, name, size, updated_at}` tanpa isi.
  - `PUT /api/ansible/assets?kind=&name=` dengan isi mentah sebagai body: membuat atau mengganti (idempoten). Tidak memakai multipart sehingga berkas biner dikirim apa adanya dari `fetch`.
  - `GET /api/ansible/assets/{id}` → isi teks **hanya untuk template** (untuk diedit). Berkas unggahan tidak pernah dikirim kembali (400).
  - `DELETE /api/ansible/assets/{id}`.
- **Batas**: template 256 KiB dan wajib UTF-8; berkas 5 MiB; total 32 MiB. Divalidasi di handler dan diulang di `RunSpec.Validate`, sehingga data hasil restore snapshot yang tidak valid gagal saat run, bukan menulis di luar workspace.
- **Ikut System Snapshot terenkripsi.** Snapshot membawa `ansible_assets` (base64 di JSON); restore mengganti seluruh isi tabel seperti tabel lain.
- Tidak ada validasi sintaks Jinja saat menyimpan: hanya `ansible-playbook` yang dapat merender dengan benar (filter dan lookup Ansible). Kesalahan template muncul di log run.

## Konsekuensi

- Template dan berkas tersimpan plaintext di SQLite (seperti kunci SSH dan isi playbook) dan terenkripsi hanya dalam System Snapshot. UI memperingatkan agar tidak mengunggah rahasia yang tidak ingin berada di cadangan. Tidak ada endpoint unduh untuk berkas.
- Setiap run inline menulis seluruh aset ke workspace, meskipun playbook hanya memakai sebagian (hingga 32 MiB). Bila itu menjadi masalah, pilih aset per playbook atau tulis hanya yang dirujuk.
- Aset bersifat global: menghapus atau mengubah satu aset memengaruhi semua playbook yang merujuknya, dan Pantau tidak memeriksa rujukan sebelum menghapus.
- `copy`/`template` tanpa `mode:` mengikuti perilaku Ansible biasa; set `mode:` secara eksplisit bila izin berkas tujuan penting.
- Berkas lain yang biasa menyertai proyek (`vars_files`, `include_tasks`, `import_playbook`) tetap tidak didukung pada playbook inline; gunakan sumber Git.
