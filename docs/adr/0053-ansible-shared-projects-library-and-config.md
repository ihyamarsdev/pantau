# 0053. Proyek Bersama, Library Role/Collection, dan Konfigurasi Ansible

Menggantikan bagian proyek bersama dengan [ADR 0054](0054-ansible-copy-import-merged-editor-and-variables.md); Library dan konfigurasi di bawah tetap berlaku. Mengubah sebagian [ADR 0051](0051-ansible-imported-playbook-folders.md); melengkapi [ADR 0049](0049-ansible-playbook-semaphore-style.md).

## Konteks

Impor Folder membuat satu playbook per folder dengan salinan ZIP penuh, termasuk collections yang di-vendor (`.ansible/collections`, ribuan berkas). Mengimpor beberapa playbook dari satu folder berarti beberapa salinan; role/collection tidak bisa dipakai ulang antar playbook; `ansible.cfg` unggahan diabaikan tanpa tempat lain untuk mengaturnya.

## Keputusan

### Proyek bersama dan impor multi-pilih (digantikan)
Bagian ini (tabel `ansible_projects`, impor multi-pilih sebagai bundle, Perbarui Berkas dari Folder) digantikan oleh [ADR 0054](0054-ansible-copy-import-merged-editor-and-variables.md): impor kini menyalin teks playbook menjadi playbook inline yang dapat diedit, dan proyek bersama serta sumber bundle dihapus. Deteksi playbook di browser dan pemilihan jamak dengan tabel (centang, nama unik, `.ansible/` dan `collections/` dilewati) tetap seperti dijelaskan di sini.

### Library role dan collection
- Disimpan di disk server: `<direktori database>/ansible-library/{collections,roles}` (layout `ansible-galaxy`), bukan di database dan tidak ikut System Snapshot; sumber kebenarannya adalah isi direktori (daftar memindai disk). Di Docker direktori ini harus berada di volume yang sama dengan database. Dipakai semua sumber playbook lewat `ANSIBLE_ROLES_PATH`/`ANSIBLE_COLLECTIONS_PATH` (proyek lebih dulu, lalu library, lalu bawaan Ansible); tidak ada ekstraksi per run.
- `ansible-galaxy` (bagian dari ansible-core) dipanggil dengan HOME/`ANSIBLE_HOME`/`ANSIBLE_CONFIG` sementara, tanpa mewarisi environment server selain PATH, locale, dan proxy; timeout 10 menit; satu mutex untuk semua mutasi. Outputnya dialirkan langsung ke UI; karena `ansible-galaxy` diam saat me-resolve dan mengunduh, Pantau menampilkan baris perintah (`$ ansible-galaxy …`) di awal dan `… still working (Ns)` setiap 5 detik.
- API: `GET /api/ansible/library`; `POST .../galaxy` (`kind`, `name`, `version`); `POST .../requirements` (requirements.yml); `POST .../local` (folder atau tarball koleksi `.tar.gz`, atau folder role dengan nama); `DELETE .../{collections|roles}/{name}`. Ketiga endpoint instalasi menjawab sebagai NDJSON (`application/x-ndjson`): baris `{"log":"…"}` selama `ansible-galaxy` berjalan, diakhiri `{"done":true}` atau `{"done":true,"error":"…"}`; kegagalan sebelum apa pun berjalan (validasi) tetap berupa JSON 400 biasa. Instalasi berjalan terlepas dari koneksi klien (context tidak dibatalkan saat tab ditutup) agar koleksi tidak setengah terpasang.
- Nama koleksi (`namespace.name`), nama role, dan versi divalidasi regex yang selalu diawali karakter non-opsi sebelum menyentuh baris perintah atau filesystem. Requirements hanya menerima `name`/`version` (dan `src` berupa nama Galaxy): sumber git/URL ditolak sehingga library hanya berisi konten Galaxy atau unggahan lokal. Instalasi lokal tidak mengambil dependensi (`--no-deps`), jadi bekerja offline. Server Galaxy privat/token tidak didukung.

### Konfigurasi Ansible global
- Pengaturan `ansible_config` (teks INI ≤ 16 KiB) diedit di Ansible Settings (`GET/PUT /api/ansible/config`) dan dipakai setiap run. Hanya seksi `defaults`, `ssh_connection`, `privilege_escalation`; satu `key = value` per baris, tanpa nilai multibaris. Kunci yang dikelola Pantau ditolak dengan nomor baris: `inventory`, `private_key_file`, `ssh_args`, `host_key_checking`, `retry_files_enabled`, `nocows`, `roles_path`, `collections_path(s)`, `ask_*`, password file, `home`.
- `ansible.cfg` run = konfigurasi dasar Pantau + tambahan pengguna (dirender terurut). Tanpa opsi forks eksplisit, `-f` tidak lagi dipaksakan sehingga `forks` dari konfigurasi (default Ansible 5) berlaku. Validasi diulang di `RunSpec.Validate`. Pengaturan ikut System Snapshot (tabel `settings`).

## Konsekuensi

- Satu folder dengan banyak playbook menyimpan satu arsip; membuka playbook mana pun dari folder yang sama memakai arsip yang sama, dan mengganti folder berlaku untuk semuanya.
- Library tidak ter-backup: setelah restore ke mesin baru, instal ulang dari Galaxy (atau impor ulang lokal). Dependensi yang hilang muncul sebagai kegagalan run Ansible biasa.
- Menginstal konten Galaxy berarti menjalankan kode pihak ketiga di server saat playbook memakainya, dengan hak proses Pantau; pasang hanya dari sumber tepercaya.
