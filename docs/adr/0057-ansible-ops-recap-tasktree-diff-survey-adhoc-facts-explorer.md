# 0057. Ansible Ops: Play Recap, Task Tree, Diff Viewer, Survey, Ad-Hoc, Facts, Explorer, Git Sync

Menambahkan lapisan operasional harian di atas subsistem Ansible Pantau: verbosity 0..4 (`-vvvv`), batas target `--limit`, log run playbook berwarna ANSI, **Play Recap** tersimpan per run, **Task Tree** dan **Diff Viewer** dari log, **Survey** (formulir variabel terstruktur saat menjalankan playbook), **Ad-Hoc Run** (satu modul ke sekumpulan Host tanpa playbook), **Ansible Facts** yang di-cache per Host, **Host & Group Explorer** dengan tombol Ping, serta **Git Sync** untuk playbook bersumber repositori.

Melengkapi [ADR 0049](0049-ansible-playbooks-semaphore-style.md) (Ansible Playbooks via subproses lokal), [ADR 0050](0050-ansible-git-playbooks-and-cron-schedules.md) (sumber Git dan jadwal cron), [ADR 0053](0053-ansible-shared-projects-library-and-config.md) (Library dan `ansible.cfg`), dan [ADR 0056](0056-terraform-management-terraform-cloud-style.md) (pola penyimpanan data deklarasi + riwayat eksekusi yang tidak ikut snapshot).

## Konteks dan Masalah

1. Setelah playbook dapat disimpan, dijalankan, dijadwalkan, dan diambil dari Git (ADR 0049-0055), kebutuhan operator bergeser dari *menjalankan* ke *memahami dan mengoperasikan*: apakah run ini berhasil di tiap host, task mana yang gagal, file apa yang berubah, host mana yang bisa dijangkau, dan apa isi mesin target.
2. Log mentah `ansible-playbook` sudah cukup untuk diagnosis manual, tetapi operator harus membaca ribuan baris untuk menjawab pertanyaan sederhana ("host mana yang unreachable?", "berapa `changed`?"). Ansible sendiri mencetak ringkasan `PLAY RECAP`, namun selama ini hanya menjadi bagian teks log yang ikut terpangkas bila buffer 2 MiB penuh.
3. Playbook tim biasanya membutuhkan beberapa parameter keputusan saat dijalankan (environment, versi rilis, jumlah worker). Menyuruh operator mengetik JSON extra vars bebas rawan salah dan tidak dapat divalidasi server; Semaphore UI memecahkan ini dengan **Survey**.
4. Operasi rutin seperti "ping semua host", "jalankan `apt update` di grup web", atau "cek spesifikasi mesin ini" tidak sepadan dengan membuat, menyimpan, dan memelihara playbook. Menulis YAML sementara untuk satu modul juga meninggalkan sampah di daftar playbook.
5. Data `ansible_facts` (`setup`) adalah informasi operasional paling sering dibutuhkan (OS, CPU, memori, disk, jaringan), tetapi mengambilnya berarti membuka terminal atau menjalankan playbook ad-hoc setiap kali.
6. Operator tidak memiliki satu tempat untuk melihat pemetaan Host Group > Host seperti yang dilihat Ansible, sehingga pola `hosts:` di playbook sering salah tebak.
7. Untuk playbook bersumber Git, isi repositori hanya diketahui saat run berjalan; tidak ada cara memverifikasi bahwa URL/branch/path masih valid sebelum menjadwalkan run.

## Keputusan Arsitektur

1. **Verbosity 0..4 dan `--limit`**:
   - `RunSpec.Verbosity` diperluas ke `0..4` (memetakan ke tanpa opsi, `-v`, `-vv`, `-vvv`, `-vvvv`), dan berlaku sama untuk handler run, handler schedule, serta dialog UI.
   - Field opsional `limit` diteruskan sebagai satu argumen `--limit <pola>`. Pola divalidasi di server oleh `ansible.ValidatePattern` dengan regex `^[A-Za-z0-9_.,:!&*\[\]-]{1,256}$` (string kosong diizinkan). Karakter spasi, `;`, `$`, dan backtick ditolak sehingga pola tidak dapat memecah argumen atau menyisipkan perintah.
   - Alias yang sah untuk `--limit` adalah alias Generated Inventory: nama Host yang disanitasi dan nama Host Group Pantau. Tidak ada namespace lain.
   - Nilai disimpan pada `ansible_runs.limit_pattern` dan `ansible_schedules.limit_pattern` (`TEXT NOT NULL DEFAULT ''`) sehingga riwayat dan jadwal menampilkan batas yang dipakai.

2. **Log Playbook Berwarna ANSI**:
   - Proses `ansible-playbook` untuk mode run dijalankan dengan `ANSIBLE_FORCE_COLOR=1` (bukan `NOCOLOR`) sehingga keluaran memuat kode escape warna. Proses `ansible-galaxy` dan `Install Library` tetap `NOCOLOR`.
   - Log di database dan API tetap menyimpan escape ANSI mentah (tidak ada transformasi server). Penyaringan ANSI dan konversi ke HTML dilakukan di UI, dengan **seluruh teks di-escape HTML terlebih dahulu**; tidak pernah ada `innerHTML` dari potongan log mentah.

3. **Play Recap Tersimpan**:
   - Paket `internal/ansible` menyediakan `ParseRecap(log string) map[string]RecapHost` yang menyaring ANSI dan membaca blok `PLAY RECAP` menjadi angka per alias host (`ok`, `changed`, `unreachable`, `failed`, `skipped`, `rescued`, `ignored`). Log tanpa `PLAY RECAP` menghasilkan map kosong, bukan galat.
   - Saat run selesai (termasuk gagal, dibatalkan, atau timeout), hasil parse disimpan ke `ansible_runs.recap` (`TEXT`, JSON). `GET /api/ansible/runs/{id}` mengembalikan objek `recap`, dan daftar run menyertakan `recap_totals` ringkas.
   - Ringkasan disimpan saat run berakhir, bukan dihitung ulang setiap kali dibaca: log dapat terpangkas (`[pantau] log truncated`) sehingga parse ulang kehilangan bagian awal.

4. **Task Tree dan Diff Viewer Sepenuhnya di Klien**:
   - Tidak ada endpoint atau parser Go baru. UI mem-parsing log tanpa ANSI menjadi hierarki Play > Task > Host dengan status `ok` / `changed` / `fatal` / `failed` / `skipping` / `unreachable` / `ignored`, dilengkapi pencarian teks dan filter status.
   - Untuk run mode `diff`, UI mem-parsing blok `--- before`, `+++ after`, `@@`, `+`, `-` menjadi tampilan per berkas berwarna.
   - Konsekuensinya, log yang dipangkas menghasilkan pohon yang tidak lengkap; kejujuran ini ditampilkan apa adanya, sedangkan Play Recap tetap utuh dari sisi server.

5. **Survey (Formulir Variabel Terstruktur)**:
   - Sebuah playbook dapat memiliki definisi survey: array `SurveyField` (`key`, `label`, `type`, `description`, `default`, `required`, `options`, `min`, `max`). `key` adalah nama variabel extra var dan wajib mematuhi `^[A-Za-z_][A-Za-z0-9_]{0,63}$`.
   - Tipe yang didukung: `text`, `textarea`, `number`, `integer`, `select`, `multiselect`, `boolean`, `password`. `select`/`multiselect` memerlukan 1..50 opsi unik; `min`/`max` adalah batas numerik untuk `number`/`integer` dan panjang karakter untuk `text`/`textarea`/`password` (bilangan bulat, tidak negatif).
   - `ValidateSurvey` membatasi maksimal 30 field, key unik, dan aturan per tipe. `ApplySurvey` memvalidasi jawaban (tipe cocok, nilai ada di `options`, rentang terpenuhi) dan mengisi `default` untuk field yang tidak dijawab. Handler launch menolak `400` jawaban dengan key yang tidak dikenal (jawaban masuk ke `-e`) dan field `required` tanpa jawaban maupun default; hasilnya digabung ke extra vars oleh `web.mergeExtraVars`.
   - Definisi survey disimpan pada `ansible_playbooks.survey` (`TEXT`, JSON array), ikut System Snapshot, dan dikembalikan sebagai `survey` + `has_survey` pada GET/list playbook. Endpoint `GET|PUT /api/ansible/playbooks/{id}/survey` mengelola definisinya, dan editor playbook menyediakan UI tambah/hapus/urut field.

6. **Presedensi Survey terhadap Extra Vars**:
   - Urutan penggabungan saat sebuah run dimulai, dari yang paling lemah ke paling kuat:
     1. Extra vars bawaan playbook (disimpan di `ansible_playbooks.extra_vars`).
     2. Jawaban survey yang tervalidasi (`ApplySurvey`), termasuk nilai `default` untuk field yang tidak dijawab.
     3. Extra vars eksplisit yang dikirim pada permintaan launch.
   - Artinya jawaban survey **menimpa** extra vars bawaan playbook, dan extra vars launch eksplisit **menimpa** keduanya. Operator yang mengetik JSON bebas di dialog launch tetap menjadi kata akhir. Penggabungan dilakukan `launchAnsibleRun` melalui `web.mergeExtraVars` dan hanya berlaku untuk launch manual; run terjadwal mengirim `extra_vars` milik jadwal apa adanya (plus `limit`) karena jawaban survey tidak dapat diberikan tanpa manusia, sehingga `SurveyAnswers` hanya ada pada jalur launch.
   - Field survey bertipe `password` **tidak pernah disimpan**: nilainya hanya diteruskan ke runner dan diganti `(hidden)` pada record (`ansible_runs.extra_vars`). Nilai tersebut **tidak disaring dari log**: task yang mencetak variabelnya (mis. `debug: var=`) akan menampilkannya di log run, dan UI memperingatkan hal ini pada form survey.

7. **Ad-Hoc Run**:
   - Endpoint `POST /api/ansible/adhoc` menjalankan satu modul (`ping`, `shell`, `command`, `apt`, `service`, `setup`, `ansible.builtin.*`, …) terhadap Host/Host Group terpilih tanpa playbook. `Module` divalidasi `^[A-Za-z0-9_.]{1,80}$` dan tidak boleh berawalan `-`; `Args` dibatasi 4 KiB sebagai satu argumen `-a` (boleh kosong) dan tidak boleh mengandung NUL; `Pattern` memakai regex yang sama dengan `--limit` (default `all`).
   - `Pattern` selalu ditafsirkan terhadap Generated Inventory, sehingga target terbatas pada alias Host dan nama Group Pantau yang eksis saat run dimulai. Tidak ada inventori di luar itu.
   - `RunAdhoc` memakai jalur yang identik dengan `Run`: Ansible Gate, workspace sementara `0700`, kunci privat `0600`, lingkungan bersih, `ansible.cfg` sementara, dan pembersihan workspace yang dijamin.
   - Riwayat disimpan pada tabel `ansible_adhoc_runs` (modul, args, pattern, snapshot target, become, verbosity, status `queued|running|success|failed|cancelled|interrupted`, exit code, hasil per host, log, waktu). Retensi 100 run terakhir; run yang masih aktif saat Pantau mati ditandai `interrupted` oleh `MarkAnsibleAdhocInterrupted` saat startup.
   - `ParseAdhoc` membaca baris hasil per host (`alias | SUCCESS | rc=0 >>`, `CHANGED`, `UNREACHABLE!`, `FAILED!`) menjadi `{alias, status, summary}`.
   - **Konkurensi**: manager ad-hoc (`AnsibleAdhocManager`) terpisah dari manager run playbook dengan batas **3 ad-hoc aktif global**, tidak berbagi kuota `MaxConcurrentRuns` playbook. Keduanya dapat berjalan bersamaan; batas playbook tetap sebagaimana ADR 0049.

8. **Ansible Facts (Cache per Host)**:
   - `GatherFacts` menjalankan `ansible <alias> -m setup` terhadap satu host pada Generated Inventory, dengan timeout 90 detik dan batas hasil 2 MiB.
   - Hasil diurai menjadi `FactsSummary` (OS, kernel, arsitektur, hostname/FQDN, CPU, memori/swap, mount, interface, IP default, uptime, Python, virtualisasi) dan disimpan bersama JSON mentah pada `ansible_host_facts` (PK `host_id` → `hosts` `ON DELETE CASCADE`, `facts`, `summary`, `gathered_at`).
   - `POST /api/ansible/hosts/{hostId}/facts` melakukan pengumpulan sinkron dan mengembalikan hasil yang sama dengan `GET`; kegagalan atau host unreachable menghasilkan 502 dengan pesan ringkas. Satu pengumpulan per host sekaligus (409).
   - Facts adalah **cache**, bukan data deklarasi: dikumpulkan ulang lewat tombol Refresh dan **tidak** ikut System Snapshot.

9. **Host & Group Explorer**:
   - `GET /api/ansible/explorer` mengembalikan struktur `groups[{name, hosts[]}]` dan `ungrouped[]`, dengan setiap host memuat identitas Pantau, badge status inspeksi, `ansible_alias` (persis alias Generated Inventory), dan timestamp facts terakhir.
   - Karena alias sama dengan inventori, hasil ad-hoc `ping` dapat dicocokkan langsung ke Host Pantau untuk menampilkan status **Reachable / Unreachable / Failed / belum dicek**.
   - Tab **Hosts** di menu Ansible menampilkan pohon Group > Host yang dapat dilipat, tombol **Ping** (membuat ad-hoc `ping`) dan tombol **Facts** per host. Explorer tidak mendefinisikan inventori baru; ia hanya menampilkan inventori yang sudah dihasilkan Pantau.

10. **Git Sync**:
    - Untuk playbook dengan `source=git`, tombol **Sync Now** memanggil `POST /api/ansible/playbooks/{id}/git/sync`. Server melakukan shallow clone sementara dengan kredensial yang sama seperti run, memverifikasi bahwa path playbook ada, dan mengembalikan `{ok, commit, branch, files[]}` (daftar jalur `.yml`/`.yaml` di repositori).
    - Respons memuat `files[]` berupa jalur `.yml`/`.yaml` terurut, tidak termasuk direktori titik (`.git`, `.ansible/`, …) dan `node_modules`, dibatasi 1000 entri.
    - Waktu sync terakhir dan commit disimpan pada `ansible_playbooks.last_sync_at` dan `last_sync_commit`, lalu ditampilkan pada detail playbook. Sync tidak menyimpan isi repositori dan tidak mengubah definisi playbook.
    - Kegagalan clone menghasilkan `502`; pesannya hanya memuat baris terakhir keluaran `git` dengan URL repositori diganti `<repo>` dan kredensial dibuang, sehingga token tidak pernah mencapai API maupun baris log. Repositori yang berhasil di-clone tetapi tidak memuat path playbook yang dikonfigurasi menghasilkan `400` (sentinel `ansible.ErrPlaybookPathMissing`), bukan `502`.
    - Endpoint ini hanya berlaku untuk `source=git`; sumber lain ditolak 400.

11. **Penyimpanan dan Snapshot**:
    - Kolom baru ditambahkan dengan pola `alterAddColumn` yang sudah dipakai kolom Ansible lain, sehingga restore snapshot lama tetap berjalan.
    - Ikut System Snapshot (terenkripsi): definisi `survey` pada playbook, `limit` pada jadwal. **Tidak** ikut snapshot: riwayat ad-hoc (`ansible_adhoc_runs`) dan cache facts (`ansible_host_facts`), konsisten dengan perlakuan riwayat run playbook.

## Model Keamanan

Model keamanan mengikuti ADR 0049 tanpa pelonggaran:

1. **Ansible Gate Tetap Satu-satunya Gerbang**: Seluruh endpoint baru (adhoc, facts, explorer, survey, git sync) berada di balik `authMiddleware` dan `checkAnsibleGate`. Tidak ada jalur yang melewati gate.
2. **Eksekusi Ad-Hoc Setara Terminal**: Modul `shell`, `command`, dan `raw` menjalankan perintah di host target dengan akses SSH yang sudah dimiliki Pantau, yaitu setara Terminal yang memang tersedia bagi admin. Tidak ada penambahan hak baru. `connection: local`, `delegate_to`, dan `local_action` tidak dapat dipakai untuk keluar dari inventori karena target selalu dibatasi alias Generated Inventory; namun `-e` dan modul itu sendiri tetap dapat memuat lookup lokal, risiko yang sama dengan ADR 0049 dan diterima secara terbuka.
3. **Tidak Ada Injeksi Argumen**: Modul, pola host, dan `--limit` divalidasi regex; tidak ada perakitan string shell, dan semua proses dijalankan via `exec.CommandContext` dengan argumen terpisah tanpa `/bin/sh`.
4. **Data Sensitif Survey**: Nilai `password` diteruskan ke runner tetapi tidak masuk ke record run (ditulis `(hidden)` pada `extra_vars`) dan tidak pernah dikembalikan API. **Batasannya jujur**: log tidak disaring, sehingga task yang mencetak variabel itu (`debug: var=…`, keluaran `-vvv`) membocorkannya — kelas risiko yang sama dengan ADR 0049 dan diterima secara terbuka. Nilai survei bertipe lain disimpan seperti extra vars biasa.
5. **Escaping Log**: Log berisi escape ANSI dan teks dari mesin target. UI wajib meng-escape HTML sebelum memberi warna; tidak ada jalur render log mentah ke DOM.
6. **Fakta Host Bukan Rahasia, Tapi Sensitif**: `facts` memuat detail perangkat, jaringan, dan versi paket. Akses dibatasi admin terautentikasi yang sama (Gate + sesi) dan data tidak ikut snapshot, namun tetap dapat dibaca siapa pun dengan sesi admin Pantau.
7. **Batas Ukuran**: args ad-hoc 4 KiB, extra vars 64 KiB, definisi survey 30 field, hasil facts 2 MiB, timeout facts 90 detik, timeout ad-hoc default 10 menit.

## Non-Goal ADR 0049 yang Tetap Dihormati

Daftar non-goal [ADR 0049](0049-ansible-playbooks-semaphore-style.md) tidak diubah. Fitur-fitur berikut tetap **tidak** dibangun, dan keputusan-keputusan di atas tidak boleh dibaca sebagai pembuka jalan ke sana:

1. **Inventori kustom atau parser berkas `hosts` statis** — Explorer, `--limit`, dan ad-hoc memakai Generated Inventory yang sama; tidak ada impor inventori.
2. **Ansible Vault** dan rahasia variabel lingkungan dinamis — survey `password` bukan Vault; nilainya tidak dienkripsi dan hanya tidak disimpan pada record.
3. **Become password interaktif** — ad-hoc hanya mengekspos switch `become` tanpa kata sandi, sama seperti run playbook.
4. **Workflow / pipeline multi-tahap dan Execution Environment (container Ansible)** — tidak ada rantai playbook, approval gate, atau image terisolasi; semuanya tetap subproses langsung di server Pantau.
5. **Dynamic inventory dari sumber eksternal** (cloud, CMDB, plugin inventori) — tidak ada plugin inventori yang dimuat.

Selain itu, hal berikut **sengaja tidak dibangun** pada iterasi ini: notifikasi hasil run, RBAC per playbook, antrean tak terbatas, WebSocket untuk log (tetap polling offset), penyimpanan riwayat ad-hoc atau facts ke System Snapshot, dan retensi riwayat ad-hoc yang dapat dikonfigurasi (tetap 100).

## Alternatif yang Dipertimbangkan

1. **Menyimpan hasil parse Task Tree di server**: Ditolak karena pohon task bergantung pada detail format log Ansible yang berubah antar versi (`-v` menambah baris). Mem-parsing di klien membuat perbaikan dapat dikirim tanpa migrasi DB, dan Play Recap — satu-satunya bagian yang benar-benar dibutuhkan untuk pelaporan — sudah diparse dan disimpan di server.
2. **Membuat playbook sementara otomatis untuk ad-hoc**: Ditolak karena akan membanjiri daftar playbook dan retensi, serta mengaburkan perbedaan antara otomasi terdeklarasi dan operasi satu kali. Tabel terpisah dengan manager sendiri lebih jujur terhadap model konkurensi.
3. **Berbagi kuota konkurensi ad-hoc dengan run playbook**: Ditolak. Operasi ad-hoc bersifat singkat dan interaktif; bila berbagi kuota, ping yang cepat dapat memblokir jadwal produksi (atau sebaliknya). Batas terpisah 3/3 menjaga keduanya independen.
4. **Menyimpan nilai survey `password` (terenkripsi) agar dapat dipakai ulang**: Ditolak karena Pantau belum memiliki Vault/penyimpanan rahasia terenkripsi untuk variabel (lihat non-goal ADR 0049); menyimpannya di `extra_vars` akan membuat rahasia masuk ke respons API dan snapshot.
5. **Menyertakan facts dan riwayat ad-hoc dalam System Snapshot**: Ditolak karena facts adalah cache yang cepat basi dan riwayat ad-hoc hanya diagnostik; keduanya memperbesar snapshot tanpa nilai pemulihan.
6. **`--limit` diteruskan mentah tanpa validasi**: Ditolak karena pola dengan spasi/`;` dapat memecah argumen atau menyuntikkan opsi `ansible`.
7. **Taruh `--limit` hanya di klien**: Ditolak karena jadwal cron juga membutuhkannya, dan riwayat harus merekam batas yang benar-benar dipakai.

## Konsekuensi

1. **Log Lebih Besar dan Berwarna**: `ANSIBLE_FORCE_COLOR=1` menambah escape ANSI pada log tersimpan, memperbesar ukuran log dan menuntut semua konsumen log (UI, ekspor) menyaring ANSI. CLI pihak ketiga yang membaca log run akan melihat kode escape.
2. **Riwayat Ad-Hoc Bertambah**: Setiap ping ke sekumpulan host membuat entri `ansible_adhoc_runs`; dengan batas 3 aktif dan retensi 100, ini membatasi laju penulisan tetapi tetap menghasilkan banyak baris bila tombol Ping sering ditekan.
3. **Ketergantungan pada Format Log Ansible**: Parse `PLAY RECAP`, hasil ad-hoc, dan Task Tree bergantung pada format keluaran `ansible-core`. Perubahan format pada versi Ansible mendatang dapat membuat ringkasan kosong; behavior-nya degradasi (kosong), bukan galat.
4. **Facts Berpotensi Mahal**: `setup` pada host dengan banyak interface atau paket dapat mendekati batas 2 MiB dan 90 detik. Karena itu facts di-cache per host dan hanya dikumpulkan ulang saat diminta.
5. **Beban Server pada Git Sync**: Sync Now melakukan clone dangkal penuh per permintaan; endpoint ini tidak dijadwalkan otomatis sehingga operator mengendalikan frekuensinya.
6. **Migrasi Ringan**: Kolom `limit_pattern`, `recap`, `survey`, `last_sync_at`, `last_sync_commit` ditambahkan dengan `alterAddColumn`; snapshot lama tetap dapat dipulihkan tanpa transformasi.
7. **Penyuntingan dan Push Git**: Git Sync memverifikasi repositori secara hanya-baca. Kemampuan penjelajahan berkas, penyuntingan, commit, dan push repositori Git ditambahkan pada [ADR 0058](0058-git-edit-commit-push-for-ansible-and-terraform.md).

## Addendum: Grouping

Pengelompokan Ansible Playbook dimodelkan mengikuti Host Group pada dashboard Hosts, menggunakan kolom teks bebas `group_name` (string, maksimal 64 karakter, tanpa karakter kontrol, dipangkas; kosong berarti tanpa grup).

1. **Grup Implisit & Tanpa Hirarki**: Grup tidak dikelola sebagai entitas tersendiri, melainkan diturunkan secara implisit dari nilai `group_name` non-kosong unik pada daftar playbook. Tidak ada pengelompokan bersarang (*no nesting*).
2. **System Snapshot**: Field `group_name` disertakan dalam System Snapshot terenkripsi; data lama otomatis bernilai kosong (`""` / *ungrouped*).
3. **Kosmetik Murni**: Pengelompokan ini hanya mengatur pemisahan visual (bagian grup yang dapat dilipat / *collapsible* dan filter grup di antarmuka) dan sama sekali **tidak mengubah perilaku eksekusi, inventori, hak akses, atau izin** playbook.
