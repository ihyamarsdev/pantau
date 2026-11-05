# 0049. Ansible Playbooks (Semaphore-style) Agentless

Menyimpan Ansible Playbook, mengeksekusinya terhadap armada server, melihat aliran log langsung, dan melacak riwayat run dengan inventory yang dihasilkan otomatis dari Host, Host Group, dan SSH Key Library Pantau via subproses `ansible-playbook` lokal di server Pantau.

Melengkapi [ADR 0035](0035-ssh-key-library.md) (SSH Key Library), [ADR 0006](0006-encrypted-system-snapshot-and-github-sync.md) (cadangan snapshot sistem terenkripsi), [ADR 0047](0047-external-kubernetes-clusters-from-kubeconfig.md) (eksekusi biner lokal server), dan [ADR 0048](0048-cloudflare-management-via-api.md) (integrasi manajemen operasional eksternal).

## Konteks dan Masalah

1. Pengguna Pantau mengelola puluhan hingga ratusan server Linux dan macOS menggunakan arsitektur pemantauan tanpa agen (*agentless*) berbasis SSH. Selama pemeliharaan berkala, instalasi paket massal, orkestrasi perbaikan sistem, dan deployment konfigurasi, operator membutuhkan kemampuan otomatisasi deklaratif berbasis Ansible Playbook.
2. Solusi terpisah seperti Semaphore UI atau AWX/Tower menyediakan fitur serupa, namun membutuhkan infrastruktur pendukung yang berat: database Postgres/MySQL terpisah, daemon antrian pesan, integrasi repositori eksternal, dan pengelolaan inventori/kunci SSH duplikat di luar Pantau.
3. Pantau telah memiliki basis data lengkap mengenai Host, Host Group, port, pengguna SSH, dan SSH Key Library. Duplikasi inventori dan kunci SSH ke sistem terpisah memicu ketidaksinkronan konfigurasi dan memperbesar bidang kerentanan keamanan (*security surface*).
4. Namun, eksekusi Ansible Playbook memiliki risiko keamanan kritis: playbook tidak dapat diisolasi (*sandboxed*) secara sempurna dari lingkungan proses controller lokal karena modul bawaan Ansible seperti `connection: local`, `delegate_to: localhost`, `local_action`, dan fungsi lookup (`pipe`, `file`) berjalan langsung pada server controller dengan hak akses pengguna proses Pantau.

## Keputusan Arsitektur

1. **Subproses `ansible-playbook` Lokal**:
   Pantau tidak menggunakan pustaka pihak ketiga dan tidak mengimplementasikan runner YAML mandiri di Go. Pantau menjalankan biner `ansible-playbook` lokal yang terpasang pada sistem server Pantau (`/usr/bin/ansible-playbook` atau jalur di `PATH`). Biner ini disertakan dalam image Docker resmi Pantau (`ansible-core` dan `openssh-client` via Alpine apk).

2. **Generated Inventory Otomatis**:
   Tidak ada inventori statis atau berkas host terpisah. Saat Playbook dijalankan, Pantau menyusun inventori JSON sementara (`inventory.json`) secara dinamis langsung dari objek Host terpilih dan Host Group yang bersangkutan (`all.hosts` dan `all.children.<group>.hosts`). Parameter koneksi seperti `ansible_host`, `ansible_port`, `ansible_user`, `ansible_ssh_private_key_file`, dan `ansible_python_interpreter=auto_silent` disuntikkan secara otomatis.

3. **Penyimpanan di SQLite Lokal**:
   - Playbook disimpan pada tabel `ansible_playbooks` (`id`, `name`, `description`, `content`, `extra_vars`, `created_at`, `updated_at`). Nama playbook unik *case-insensitive*.
   - Setiap run dicatat pada tabel `ansible_runs` (`id`, `playbook_id`, `playbook_name`, `status`, `mode`, `targets`, `extra_vars`, `tags`, `skip_tags`, `verbosity`, `become`, `exit_code`, `log`, `created_at`, `started_at`, `finished_at`).
   - Log eksekusi dialirkan secara *real-time* lewat buffer memori hingga 2 MiB per run aktif, lalu disimpan ke basis data saat run tuntas.
   - Retensi riwayat run: Pantau secara otomatis mempertahankan 100 run selesai terbaru per playbook (`PruneAnsibleRuns`) saat proses run selesai.

4. **Pencadangan System Snapshot Terenkripsi**:
   Hanya data deklarasi playbook (`ansible_playbooks`) yang disertakan dalam cadangan **System Snapshot** terenkripsi (AES-256-GCM / Argon2id, [ADR 0006](0006-encrypted-system-snapshot-and-github-sync.md)). Riwayat eksekusi (`ansible_runs`) dan log tidak disertakan dalam snapshot untuk menjaga ukuran snapshot tetap ringkas, dan tabel runs dikosongkan bersama restore.

5. **Mode Eksekusi Beragam**:
   Pantau mendukung 4 mode eksekusi run:
   - `run`: Eksekusi playbook normal terhadap target.
   - `check`: Mode *dry-run* simulasi menggunakan opsi CLI `--check`.
   - `diff`: Mode perbandingan perubahan menggunakan opsi CLI `--check --diff`.
   - `syntax`: Validasi sintaksis YAML playbook menggunakan opsi CLI `--syntax-check` (dapat dijalankan dengan 0 target host).

6. **Batas Non-Goals (v1)**:
   Fitur-fitur berikut secara sadar tidak dimasukkan ke dalam v1 demi menjaga kesederhanaan dan keandalan sistem:
   - ~~Repositori Git sebagai sumber playbook~~ dan ~~penjadwalan berkala~~: ditambahkan kemudian pada [ADR 0050](0050-ansible-git-playbooks-and-cron-schedules.md).
   - Ansible Vault dan rahasia variabel lingkungan dinamis.
   - Instalasi otomatis modul/koleksi pihak ketiga (`ansible-galaxy`).
   - Autentikasi password SSH atau become password interaktif (hanya mendukung kunci privat SSH dan `sudo` tanpa kata sandi via switch `become`).
   - Notifikasi webhook/alert hasil run.
   - RBAC multi-user per playbook.
   - Inventory kustom atau parser berkas `hosts` statis.
   - Antrian eksekusi tak terbatas (antrian melebihi `MaxConcurrentRuns` langsung ditolak dengan HTTP 429).

## Model Keamanan

Model keamanan Ansible Playbooks dirancang dengan prinsip keterbukaan risiko (*honest risk assessment*) dan pertahanan berlapis:

1. **Eksekusi Lokal Tanpa Sandbox**:
   Ansible Playbook dijalankan **di server Pantau dengan hak akses proses Pantau**. Karena arsitektur internal Ansible memungkinkan pemanggilan modul lokal (`connection: local`, `delegate_to: localhost`, `local_action`, lookup filter `pipe`/`file`), playbook tidak dapat disandbox secara andal dari sistem master. Siapa pun yang dapat menulis atau menjalankan playbook memiliki hak eksekusi setara dengan proses Pantau pada server host master.

2. **Ansible Gate (Opt-In Administrator)**:
   Fitur eksekusi Ansible dinonaktifkan secara default melalui pengaturan saklar `ansible_enabled` (Ansible Gate). Pengaktifan hanya dapat dilakukan oleh administrator melalui `PUT /api/ansible/status` dengan peringatan keamanan eksplisit pada dialog konfirmasi antarmuka. Hak admin Pantau (+2FA bila aktif) dipahami setara dengan akses eksekusi di mesin master.

3. **Pembersihan Variabel Lingkungan (*Clean Environment*)**:
   Proses anak `ansible-playbook` dieksekusi dengan variabel lingkungan yang disterilkan secara ketat: hanya mewariskan `PATH`, `LANG=C.UTF-8`, `HOME=<tmp_workspace>`, `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY` (jika ada), serta variabel `ANSIBLE_*` buatan Pantau. Variabel lingkungan Pantau lainnya tidak diwariskan. `ANSIBLE_CONFIG` secara eksplisit diarahkan ke berkas `ansible.cfg` sementara di dalam workspace sementara untuk mencegah pemuatan konfigurasi berbahaya dari `/etc/ansible`, `~/.ansible.cfg`, atau direktori kerja saat ini.

4. **Isolasi dan Pembersihan Kunci Privat SSH**:
   Kunci privat SSH efektif Host (yang diekstrak dari SSH Key Library atau kunci custom host) ditulis ke berkas berizin `0600` di dalam direktori kerja sementara bertingkat izin `0700` (`os.MkdirTemp`). Seluruh direktori kerja beserta berkas kunci privat di dalamnya **dijamin selalu dihapus seketika** seusai proses selesai, dibatalkan, gagal, maupun saat terjadi panic runtime. Kunci privat tidak pernah ditulis ke log, basis data riwayat run, maupun respons API.

5. **Penyusunan Inventory Aman Bebas Injeksi**:
   Seluruh entri host, grup, variabel koneksi, dan alias disusun menggunakan serialisasi `encoding/json` murni, bukan penggabungan string. Karakter alias host dan nama group disanitasi ketat menggunakan pola `[A-Za-z0-9_.-]`, serta dideduplikasi dengan akhiran numerik `_2`, `_3` bila terjadi benturan nama.

6. **Validasi Ketat Parameter CLI**:
   Seluruh argumen perintah divalidasi ketat di sisi server:
   - `tags` dan `skip_tags` wajib mematuhi regex `^[A-Za-z0-9_.,:-]{1,256}$`.
   - `verbosity` dibatasi pada rentang `0..3` (memetakan ke `-v`, `-vv`, `-vvv`).
   - `forks` dibatasi pada rentang `1..20` (default 5 bila 0).
   - `extra_vars` wajib berupa objek JSON valid dengan ukuran maksimal 64 KiB, ditulis ke berkas sementara `extra.json` dan diteruskan melalui argumen `-e @file` tanpa interpolasi shell.
   - Perintah dieksekusi menggunakan `exec.CommandContext` langsung tanpa melewati shell pembungkus (`/bin/sh -c`).

7. **Batas Muatan dan Konkurensi Sistem**:
   - Ukuran konten playbook dibatasi maksimal 256 KiB.
   - Ukuran buffer log run dibatasi maksimal 2 MiB; log yang melebihi batas dipotong dari bagian awal (*head truncation*) dengan penanda `[pantau] log truncated`.
   - Batas waktu eksekusi (*timeout*) default adalah 1 jam.
   - Maksimal 1 run aktif per playbook (permintaan paralel ke playbook yang sama ditolak HTTP 409).
   - Maksimal 3 run aktif secara global di seluruh sistem (permintaan ke-4 ditolak HTTP 429).
   - Maksimal 200 target host per run (kecuali mode `syntax` yang mengizinkan 0 target).

8. **Penonaktifan Host Key Checking**:
   Pemeriksaan kunci host SSH (`host_key_checking = False`) dinonaktifkan dalam konfigurasi `ansible.cfg` sementara, dan klien SSH dijalankan dengan `-F /dev/null -o UserKnownHostsFile=/dev/null` (config pengguna tidak dibaca; lihat [ADR 0050](0050-ansible-git-playbooks-and-cron-schedules.md)). Hal ini konsisten dengan arsitektur koneksi SSH tanpa agen Pantau yang tidak menggunakan berkas `known_hosts`. Risiko *man-in-the-middle* (MITM) pada jaringan tidak tepercaya dicatat sebagai batasan yang diterima.

9. **Penanganan Interupsi Eksekusi**:
   Proses run yang masih berstatus `queued` atau `running` saat server Pantau di-restart atau mati mendadak akan secara otomatis ditandai sebagai `interrupted` saat startup daemon (`MarkAnsibleRunsInterrupted`).

## Alternatif yang Dipertimbangkan

1. **Menjalankan Semaphore UI sebagai Kontainer Sidecar**:
   Ditolak karena mewajibkan pengguna menjalankan kontainer kedua, basis data kedua (Postgres atau MySQL), serta integrasi jaringan internal yang rumit. Selain itu, inventori server dan kunci SSH harus dikonfigurasi ulang secara ganda di Semaphore, bertentangan dengan filosofi single-binary mandiri Pantau.
2. **Menggunakan Pustaka Go Pihak Ketiga untuk Ansible**:
   Ditolak karena tidak ada pustaka resmi Go yang mampu mengeksekusi Ansible Playbook secara lengkap dan stabil. Pustaka pembungkus komunitas yang ada cenderung tidak terawat dan tetap membutuhkan biner `ansible-playbook` di baliknya.
3. **Mengimplementasikan Parser & Engine Eksekusi YAML Mandiri di Go**:
   Ditolak karena orkestrasi modul-modul Ansible sangat kaya dan dinamis (ribuan modul Python, filter Jinja2, loop, conditional, task blocks). Mengembangkan ulang mesin eksekusi tersebut di Go membutuhkan ribuan baris kode rawan bug dan mustahil mengejar kompatibilitas playbook komunitas.
4. **Repositori Git Eksternal sebagai Sumber Playbook**:
   Ditunda pada v1 dan diimplementasikan di [ADR 0050](0050-ansible-git-playbooks-and-cron-schedules.md) (bersama jadwal cron).

## Konsekuensi

1. **Kebutuhan Dependensi Host & Ukuran Image**:
   - Host yang menjalankan Pantau wajib memiliki biner `ansible-playbook` terpasang di `PATH`.
   - Image Docker resmi Pantau diperbarui dengan menambahkan paket Alpine `ansible-core` dan `openssh-client`, yang menambahkan sekitar ~40-60 MB pada ukuran layer image akhir karena menyertakan runtime Python pendukung pada master.
2. **Ketiadaan Dukungan Server Pantau di Windows**:
   Ansible controller tidak mendukung sistem operasi Windows secara native. Biner Pantau yang dikompilasi untuk target Windows tetap dapat dibangun berkat build tags (`proc_windows.go`), namun endpoint deteksi ketersediaan akan selalu melaporkan galat `"ansible controller is not supported on windows"`.
3. **Batasan Server SSH Lama (Legacy SSH Servers)**:
   Karena eksekusi SSH didelegasikan langsung ke biner `ansible-playbook` dan klien OpenSSH sistem host, negosiasi cipher dan key-exchange mengikuti konfigurasi OpenSSH lokal server Pantau. Target host dengan daemon SSH sangat usang mungkin memerlukan konfigurasi opsi cipher khusus.
