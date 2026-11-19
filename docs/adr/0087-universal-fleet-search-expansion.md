# 0087. Universal Fleet Search: multi-domain coverage, prefix scopes, ranking and deep links

Fleet Search berubah dari pencarian tiga sumber (paket, container, domain) menjadi pencarian universal atas
seluruh entitas infrastruktur yang **sudah tersimpan** di SQLite Pantau — untuk pengguna UI maupun AI Agent
in-process ([ADR 0073](0073-in-process-ai-agent.md)). Tidak ada koneksi SSH saat mencari; data dapat basi dan
setiap hasil menyertakan `data_age`.

## Konteks dan Masalah

1. `SearchFleet` hanya mengindeks `host_packages`, `host_containers`/`actual_items`/`docker_stacks`, dan
   `host_domains`. Operator tidak bisa menemukan grup host, port, VPN, cluster Kubernetes, workspace Terraform,
   kunci SSH, atau temuan CVE lewat satu kolom pencarian.
2. Command Palette ([ADR 0039](0039-modern-visual-design-system-and-icon-sprite.md)) dan AI Agent memanggil
   `fleet_search`, tetapi keduanya terikat pada empat `kind` lama.
3. Hasil tidak memiliki metadata navigasi, sehingga UI tidak bisa membuka hasil langsung ke tab Host Workspace
   ([ADR 0007](0007-standardized-workspace-modal-layout.md)) atau workspace domain tertentu.
4. Urutan hasil tidak deterministik: pencarian yang sama bisa menampilkan entri dalam urutan berbeda.

## Keputusan Arsitektur

1. **Cakupan penuh dari data tersimpan.** Satu `SearchFleet(query, kind, limit)` kini mendispatch 17 kategori
   lewat `searchFleetCategory`, masing-masing satu query SQL terindeks dengan `LIMIT` per kategori (menghindari
   N+1 dan starve). Kategori: `folder`, `host`, `package`, `container`, `port`, `service`, `domain`, `kube`,
   `vpn`, `cf`, `git`, `ansible`, `tf`, `pkg`, `cve`, `sshkey`, `account`.
   - Folders = grup host. [ADR 0011](0011-host-grouping-terminal-maximize-and-port-autoscan.md) menyimpan grup
     sebagai kolom `hosts.group_name`; jumlah anggota dihitung dengan `COUNT(*)`, bukan kolom tersimpan. Tidak
     ada deskripsi/tag grup di skema.
   - Host = `name` (alias SSH hasil import menjadi `hosts.name`, [ADR 0035](0035-ssh-key-library.md)), `host`,
     `public_ip`, `private_ips`, `os_info`, `hardware_model`, `notes`, `group_name`.
   - Port = JSON `hosts.listening_ports` (`proto`, `port`, `address`, `process`, `process_source`, `risk`);
     Service = `actual_items` kind `service`/`port`.
   - Kube = `kube_clusters`; VPN = `vpn_networks` ([ADR 0077](0077-jalur-jaringan-per-host-driver-vpn-dan-dial-terikat-interface.md),
     kolom `config` **tidak pernah** dicari/dikembalikan karena berisi rahasia); CF = `cloudflare_accounts`;
     Git = `git_repos`; Ansible = `ansible_playbooks`; TF = `tf_workspaces`; Pkg = `package_update_checks.packages_json`;
     CVE = `docker_security_scans` kind `image` (`findings_json`); SSHKey = `ssh_keys` (kunci privat tidak pernah
     dicari/dikembalikan); Account = `account_audits` (`users_json`, `keys_json`, `findings_json`).
   - Kolom JSON diparsing di Go hanya pada baris yang lolos prafilter SQL, dengan batas `limit` per kategori.
2. **Tidak ada entitas yang tidak dipersistenkan yang dicari.** Systemd unit (live via SSH), temuan OSV/CVE
   host (live via `internal/osv`), zone/tunnel/DNS Cloudflare (live via API Cloudflare), dan cabang Git (dari
   clone on-disk) tidak memiliki tabel SQLite dan tidak diindeks. Ini dinyatakan eksplisit di deskripsi tool
   agent dan di ADR ini — bukan fallback diam-diam.
3. **Prefix scope & peringkat deterministik.** `parseFleetQuery` memisahkan prefiks (`folder:`, `host:`, `port:`,
   `domain:`, `container:`, `service:`, `kube:`, `vpn:`, `cf:`, `git:`, `ansible:`, `tf`/`terraform:`, `pkg:`,
   `cve:`, `sshkey:`, `account:`; `group:` alias `folder:`). Prefiks mengikat `kind` bila `kind` tidak diberikan.
   `fleetMatchRank` memberi peringkat `exact (3) > prefix (2) > substring (1)`; `fleetRankGroups` mengurutkan
   grup dan host secara stabil (peringkat, lalu nama, lalu kind).
4. **Payload deep-link terstruktur.** `FleetSearchGroup` membawa `category`, `entity_id`, `host_id` (per host),
   `group_id`, `target_tab`, `subtitle`, `status_badge`. API `/api/search` mengembalikan kolom `scope` dan
   `groups` yang sama; agent `fleet_search` menerima enum `kind` yang diperluas dan deskripsi kategori.
5. **UI vanilla (tanpa dependensi baru).** Dialog Fleet Search dirender dengan token desain dan sprite ikon
   ([ADR 0039](0039-modern-visual-design-system-and-icon-sprite.md), [ADR 0013](0013-airgapped-self-contained-web-assets.md)).
   Bagian hasil dikelompokkan per kategori, menyertakan bagian "Folders / Host Groups", petunjuk prefiks, dan
   navigasi keyboard `Up`/`Down`/`Enter`. Klik hasil memakai metadata: folder → filter dashboard
   (`groupFilterSelect` + `loadHosts` + `renderHosts`); host/port/service/container → Host Workspace pada
   `target_tab`; kube/cf/git/ansible/tf/sshkey → `switchSideNav` ke workspace terkait (VPN/SSH key → tab
   Settings yang sesuai). Fungsi navigasi diakses lewat `window` seperti yang sudah dipakai Command Palette.
6. **Batas dan kompatibilitas.** `limit` tetap 1–200 (API) dan 1–200 per kategori (agent). Field lama
   (`kind`, `groups`, `hosts_without_*`, `data_age`) tidak berubah, sehingga pemanggil lama
   (`agent_recall.go`, pengujian web yang ada) tetap bekerja.

## Alternatif yang Ditolak

1. **Tabel `fleet_index` materialized yang di-refresh periodik.** Menambah duplikasi data, migrasi, dan mekanisme
   invalide; query langsung ke tabel sumber sudah cukup cepat untuk skala satu binary SQLite.
2. **FTS5 virtual table.** Butuh trigger dan migrasi skema serta tidak menyelesaikan kategori tanpa kolom teks
   (port/account dari JSON). Pencarian substring `LIKE` + peringkat di Go sudah deterministik dan airgapped.
3. **Menambah tabel `host_groups`, `systemd_units`, `cf_zones`, `osv_findings`.** Merusak prinsip "data turunan
   tidak dipersistenkan" dan menambah beban sinkronisasi; ADR 0011 secara eksplisit memilih satu kolom
   `group_name`.

## Konsekuensi

- Operator dan AI Agent mendapat satu titik pencarian untuk seluruh inventaris yang tersimpan, dengan urutan
  yang bisa diprediksi dan tautan langsung ke konteks kerja.
- Kategori tanpa penyimpanan (systemd, OSV, Cloudflare live, cabang Git) tetap tidak bisa ditemukan lewat
  Fleet Search; pengguna harus membuka tampilan live masing-masing.
- `SearchFleet` kini mengerjakan hingga 17 query per pencarian `all`; masing-masing dibatasi `limit`, dan
  `fleetRankGroups` memotong hasil akhir ke `limit` agar respons tetap terikat.
