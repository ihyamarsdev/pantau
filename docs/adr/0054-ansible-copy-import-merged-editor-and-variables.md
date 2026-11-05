# 0054. Impor sebagai Salinan, Editor Gabungan, dan Variabel yang Dapat Diedit

Menggantikan mekanisme bundle/proyek bersama dari [ADR 0051](0051-ansible-imported-playbook-folders.md) dan [ADR 0053](0053-ansible-shared-projects-library-and-config.md) (bagian Library dan konfigurasi tetap berlaku), serta tampilan terpisah di [ADR 0052](0052-ansible-playbook-contents-and-variables.md).

## Konteks

Impor Folder menyimpan ZIP (kemudian proyek bersama) dan playbook hasil impor tidak bisa diedit: pengguna harus mengimpor ulang. Setelah role dan collection dipindah ke Library, inventory dibuat Pantau, dan `ansible.cfg` diatur di Ansible Settings, yang tersisa dari folder hanyalah teks playbook. Tampilan isi dan variabel juga terpisah dari editor, dan variabel hanya bisa dilihat.

## Keputusan

- **Impor = salinan.** Browser membaca teks YAML yang dicentang dan `POST /api/ansible/playbooks/import` (JSON, bukan multipart) membuat satu playbook **inline** per berkas, dalam satu transaksi (nama bentrok membatalkan semuanya, 409). Batas: 1–100 playbook, 1 byte–256 KiB per berkas, body 32 MiB. Setelah impor tidak ada tautan ke folder; playbook diedit seperti playbook lain.
- **Sumber hanya `inline` dan `git`.** Sumber `bundle`, tabel `ansible_projects`, kolom `bundle_path`/`project_id`, `RunSpec.Bundle`, endpoint Ganti/Perbarui Berkas, dan pemuatan isi ZIP dihapus. Utilitas arsip (`ValidateArchive`, `ExtractArchive`) tetap hanya untuk impor lokal Library.
- **Migrasi otomatis saat startup.** Playbook `bundle` dikonversi menjadi `inline` berisi YAML utamanya (dari `ansible_projects`, atau dari `bundle_data` per playbook di versi lama), lalu kolom bundle dan tabel proyek dihapus (idempoten). Berkas lain di dalam arsip (role, template, `group_vars`, ...) **tidak dipertahankan**; role/collection kini berasal dari Library. YAML utama yang tidak terbaca menjadi komentar penjelas agar playbook tetap valid dan dapat diedit. Snapshot lama (arsip pada playbook atau di `ansible_projects`) dikonversi dengan cara yang sama saat restore; snapshot baru tidak membawa arsip.
- **Editor gabungan.** Satu editor per playbook: tab **Playbook** (nama, deskripsi, sumber, YAML/Git), **Variabel**, dan **Berkas** (hanya Git, read-only, dimuat saat tab dibuka lewat `GET .../contents`, yang kini khusus Git). Tampilan "Lihat isi" terpisah dihapus.
- **Variabel dapat diedit dan dihapus.** Tab Variabel memakai `POST /api/ansible/playbooks/analyze` (`{content, extra_vars}` → `{vars}`) pada YAML yang sedang diketik, termasuk yang belum disimpan, memakai pemindai statis yang sama (ADR 0052). Nilai variabel adalah **extra vars default** playbook (`extra_vars`): mengisi nilai menetapkan/menimpa, ikon hapus menghapus kuncinya (variabel kembali ke definisi di YAML), dan baris "Tambah variabel" membuat yang tidak terdeteksi. Tidak ada penyuntingan teks YAML otomatis: variabel yang didefinisikan di `vars:` diubah di tab Playbook atau ditimpa lewat extra vars, sehingga komentar dan format YAML pengguna tidak pernah ditulis ulang oleh Pantau.

## Konsekuensi

- Playbook yang bergantung pada berkas di sebelahnya (`template: src=`, `vars_files`, `include_tasks`, target `import_playbook`) tidak lengkap setelah impor: pengguna mengimpor target sebagai playbook tersendiri atau menempelkan isinya. (Khusus `template:` dan `copy:`, [ADR 0055](0055-ansible-templates-and-uploaded-files.md) kemudian menambah tab Templates dan Files.) Ini disengaja (permintaan pengguna: hanya teks playbook; role, collection, inventory, dan konfigurasi dari Pantau).
- Playbook hasil migrasi dari bundle kehilangan berkas pendampingnya; isi arsip lama tidak disimpan di mana pun setelah migrasi.
- Satu mekanisme penyimpanan (teks di SQLite) menyederhanakan snapshot, run, jadwal, dan CRUD.
