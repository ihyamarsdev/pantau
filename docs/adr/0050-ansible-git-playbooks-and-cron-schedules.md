# 0050. Playbook Ansible dari Repositori Git dan Jadwal Cron

Menambahkan dua kemampuan yang di [ADR 0049](0049-ansible-playbooks-semaphore-style.md) ditetapkan sebagai non-goals v1: **sumber playbook dari repositori Git** dan **jadwal (cron) untuk Playbook Run**. Keduanya tetap berjalan lewat subproses lokal (`git`, `ansible-playbook`) dan berada di balik Ansible Gate.

Melengkapi [ADR 0035](0035-ssh-key-library.md) (SSH Key Library), [ADR 0006](0006-encrypted-system-snapshot-and-github-sync.md) (System Snapshot terenkripsi), dan [ADR 0047](0047-external-kubernetes-clusters-from-kubeconfig.md) (eksekusi biner lokal).

## Konteks dan Masalah

1. Playbook yang hanya hidup di editor inline tidak ikut alur review/versioning tim. Operator sudah menyimpan playbook dan role di Git dan ingin menjalankannya apa adanya, termasuk `roles/`, `group_vars/`, dan `files/` relatif terhadap playbook.
2. Tugas pemeliharaan rutin (patching, rotasi, verifikasi konfigurasi) harus berjalan tanpa operator menekan tombol Run.
3. URL repositori dan nama branch adalah masukan yang berpotensi berbahaya: `git clone` menerima URL `ext::`, `file://`, jalur lokal, dan nilai yang tampak seperti opsi (`--upload-pack=...`). Isi repositori juga tidak tepercaya (symlink yang menunjuk keluar clone).
4. Eksekusi terjadwal adalah aksi mutasi tanpa pengawasan; pemutaran ulang tumpukan jadwal yang terlewat (Pantau mati, gate nonaktif) setelah bangun kembali akan berbahaya.

## Keputusan Arsitektur

1. **Sumber Playbook: `inline` atau `git`**:
   - Kolom baru pada `ansible_playbooks`: `source` (`inline` default | `git`), `git_url`, `git_branch`, `git_path`, `git_auth` (`none` | `token` | `ssh_key`), `git_token`, `git_key_id`.
   - Konfigurasi Git disematkan langsung pada Playbook (satu CRUD, tanpa entitas "Repository" terpisah). Konsekuensinya URL yang sama dapat terduplikasi antar playbook; dipilih sebagai jalan pintas sadar (*shortcut*) demi kesederhanaan. Entitas Repository dapat diperkenalkan nanti tanpa mengubah kontrak run.
   - Playbook bersumber Git tidak menyimpan `content`; berkas dibaca dari clone pada setiap run.
2. **Clone Dangkal per Run lewat `git` Lokal**:
   - `git clone --depth 1 --single-branch --no-tags [--branch B] -- URL <workspace>/repo` ke dalam workspace sementara `0700` yang sama dengan run Ansible; dihapus bersama workspace.
   - `ansible-playbook` dijalankan dengan direktori kerja = hasil clone dan path playbook absolut di dalamnya, sehingga `roles/`, `group_vars/`, dan `files/` relatif ikut bekerja. `ANSIBLE_CONFIG` tetap menunjuk `ansible.cfg` buatan Pantau, sehingga `ansible.cfg` di dalam repositori **diabaikan**.
   - Commit yang di-checkout (`git rev-parse HEAD`) dicatat pada run (`commit_sha`), ditulis ke log (`[pantau] git commit <sha>`), dan ditampilkan di riwayat.
   - Submodule tidak diambil. `ansible-galaxy` tetap non-goal; role harus berada di dalam repositori.
3. **Autentikasi Git**:
   - `none`: repositori publik.
   - `token` (khusus `https://`): token dikirim sebagai header `Authorization: Basic` melalui konfigurasi git berbasis **variabel lingkungan** (`GIT_CONFIG_COUNT/KEY_0/VALUE_0`, dibatasi ke host repositori), sehingga token tidak pernah muncul di argumen proses, log run, maupun respons API. Cocok untuk personal access token GitHub/GitLab.
   - `ssh_key` (khusus `ssh://` atau `user@host:path`): memakai kunci dari **SSH Key Library** yang ditulis ke berkas `0600` di workspace; `GIT_SSH_COMMAND` memakai `-F /dev/null -o IdentitiesOnly=yes -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null`.
   - Token disimpan plaintext di SQLite seperti `ssh_keys.private_key` ([ADR 0035](0035-ssh-key-library.md)), `json:"-"` di semua API (hanya `git_token_set` boolean yang tampil), dan ikut System Snapshot yang terenkripsi. Mengosongkan field token saat menyunting mempertahankan token lama; mengganti `git_auth` menjadi bukan `token` menghapusnya.
4. **Jadwal (`ansible_schedules`)**:
   - Satu jadwal = playbook + ekspresi cron 5 field + target tetap (`host_ids` dan/atau `group`) + mode/tags/skip_tags/extra_vars/verbosity/become + `enabled`. Hasil terakhir dicatat (`last_run_at`, `last_run_id`, `last_error`) beserta `next_run_at`.
   - Parser cron ditulis sendiri di `internal/ansible/cron.go` (tanpa dependensi): `*`, angka, rentang, daftar, langkah (`*/n`, `a-b/n`), dan alias `@hourly/@daily/@weekly/@monthly/@yearly`. Semantik hari: bila kedua field hari-dalam-bulan dan hari-dalam-minggu dibatasi, keduanya di-OR (gaya Vixie). Nama bulan/hari belum didukung.
   - Zona waktu: **waktu lokal server Pantau** (di Docker umumnya UTC; atur `TZ`). UI menampilkan zona yang dipakai.
   - Penjadwal adalah satu goroutine dengan ticker 15 detik (`StartAnsibleScheduler`), memeriksa semua jadwal aktif. Hanya berjalan bila Ansible Gate aktif dan `ansible-playbook` tersedia.
5. **Kebijakan Jadwal Terlewat**: kemunculan yang terlambat lebih dari **5 menit** (Pantau mati, gate nonaktif) **dilewati**, bukan dijalankan susulan; `last_error` mencatat `missed occurrence ... skipped` dan `next_run_at` dimajukan.
6. **Konkurensi**: jadwal memakai jalur peluncuran yang sama dengan Run manual (`launchAnsibleRun`) sehingga aturan 1 run aktif per playbook (409) dan maks 3 run global (429) berlaku; jadwal yang tertolak tidak diantrikan, hanya dicatat sebagai `not started: ...` dan jadwal tetap maju. Run hasil jadwal diberi `trigger=schedule` dan `schedule_id`.
7. **Snapshot**: `ansible_playbooks` (termasuk field Git dan token) dan `ansible_schedules` ikut System Snapshot terenkripsi; `ansible_runs` tetap tidak ikut.
8. **Perubahan pada ADR 0049**: `ansible.cfg` sementara kini memuat `[ssh_connection] ssh_args = -F /dev/null -o UserKnownHostsFile=/dev/null`. OpenSSH membaca `~/.ssh/config` dari home pada `/etc/passwd` (bukan `$HOME`) dan menolak berkas yang pemiliknya bukan pengguna proses (kasus `Bad owner or permissions on /root/.ssh/config` pada Docker dengan `~/.ssh/config` host yang di-mount). Seluruh parameter koneksi sudah ada di inventory buatan Pantau, jadi config pengguna tidak diperlukan. `ControlPersist` sengaja tidak dipakai agar tidak ada proses mux yang hidup melewati workspace.

## Model Keamanan

1. **Validasi URL Git (`ValidateGitURL`)**: hanya `https://`, `ssh://`, dan bentuk scp `user@host:path`; menolak `file://`, `ext::`, `git://`, `http://`, jalur lokal, nilai berawalan `-`, spasi/karakter kontrol, kredensial tertanam (`https://user:pw@...`), query/fragment, dan URL > 512 karakter. `GIT_ALLOW_PROTOCOL=https:ssh` menjadi lapis kedua di dalam proses `git`.
2. **Validasi branch dan path**: branch `^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$` tanpa `..`; path playbook relatif, bersih (`path.Clean`), berekstensi `.yml/.yaml`, tanpa `..`, `\`, atau awalan `/`. Setelah clone, path di-`EvalSymlinks` dan harus tetap berada di dalam clone dan berupa berkas biasa; symlink keluar repositori ditolak.
3. **Lingkungan `git` dibersihkan**: hanya `PATH`, `LANG`, `HOME=<workspace>`, proxy, `GIT_TERMINAL_PROMPT=0`, `GIT_CONFIG_GLOBAL=/dev/null`, `GIT_CONFIG_NOSYSTEM=1`, `credential.helper=` kosong. Hook repositori tidak dijalankan oleh `clone`.
4. **Isi repositori tetap kode yang dijalankan di master** (plugin `library/`, `action_plugins/`, `lookup`): konsekuensi yang sama dengan playbook inline dan dijaga oleh **Ansible Gate**. Hanya arahkan Pantau ke repositori yang dipercaya; tidak ada verifikasi tanda tangan commit.
5. **Jadwal adalah eksekusi tanpa pengawasan** dan mutasi pada jadwal (buat/ubah/hapus/aktif) dicatat ke Activity Console tanpa token atau extra_vars. Jadwal tidak dapat dibuat/diubah/dijalankan bila Ansible Gate mati (endpoint mengembalikan 409), dan penjadwal tidak menembak saat gate mati.
6. `host_key_checking` Git SSH dimatikan (`StrictHostKeyChecking=no`), konsisten dengan akses SSH agentless Pantau; risiko MITM pada jaringan tidak tepercaya diterima.

## Alternatif yang Dipertimbangkan

1. **Pustaka Go Git (`go-git`)**: ditolak karena menambah dependensi besar; biner `git` lokal sudah teruji dan menangani semua transport/otentikasi.
2. **Entitas Repository terpisah (seperti Semaphore)**: ditunda; memperbesar CRUD, API, dan UI tanpa kebutuhan mendesak. Embedding pada Playbook menyisakan jalur migrasi.
3. **Menyisipkan token di URL (`https://token@host/...`)**: ditolak karena bocor ke argumen proses, log, dan pesan error git.
4. **Cron dari pustaka pihak ketiga (`robfig/cron`)**: ditolak untuk menjaga `go.mod` tetap bersih; kebutuhan 5 field standar cukup ~100 baris dan diuji tabel.
5. **Menjalankan jadwal terlewat saat bangun (*catch-up*)**: ditolak karena risiko mutasi massal tak terduga setelah mati lama; dipilih "lewati dan majukan".
6. **Webhook/trigger dari push Git**: di luar cakupan; jadwal dan Run manual cukup untuk v1.

## Konsekuensi

- Biner `git` wajib ada di PATH server Pantau; image Docker resmi menambahkan paket `git`. UI dan `GET /api/ansible/status` (`git_available`) memperingatkan bila tidak ada.
- Setiap run Git meng-clone ulang (dangkal). Repositori sangat besar atau jaringan lambat memperpanjang run; batas waktu run 1 jam (termasuk clone) tetap berlaku.
- Menghapus kunci SSH dari library yang dipakai sebuah playbook tidak dicegah; run berikutnya gagal dengan pesan jelas (`git ssh key N no longer exists`). Penghitung `used_by` kunci tetap hanya menghitung Host.
- Tanpa `ControlPersist`, tiap task membuka koneksi SSH baru sehingga playbook dengan banyak task sedikit lebih lambat; pipelining dapat diaktifkan lewat `ansible_ssh_pipelining` di extra vars bila perlu.
- Jadwal mengikuti zona waktu server; perubahan `TZ` mengubah arti ekspresi cron yang sudah tersimpan (`next_run_at` dihitung ulang pada edit atau saat penjadwal melihatnya kosong).
- Lompatan waktu sistem (NTP, DST) dapat melewati/menggandakan satu kemunculan pada jam yang tidak ada/berulang; diterima.
- Tabel runs menyimpan `trigger_kind`, `schedule_id`, `commit_sha`; migrasi `ALTER TABLE` idempoten pada DB lama.
