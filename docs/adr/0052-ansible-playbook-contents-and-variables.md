# 0052. Lihat Isi Playbook dan Deteksi Variabel

Melengkapi [ADR 0049](0049-ansible-playbooks-semaphore-style.md) dan [ADR 0050](0050-ansible-git-playbooks-and-cron-schedules.md). Tampilan terpisah dan dukungan sumber bundle di sini digantikan oleh [ADR 0054](0054-ansible-copy-import-merged-editor-and-variables.md): isi dan variabel kini ada di editor playbook, `contents` hanya untuk Git, dan analisis YAML yang belum disimpan memakai endpoint `analyze`.

## Konteks

Playbook Git dan Impor Folder tidak tampil di editor: YAML-nya ada di repositori atau di dalam ZIP. Pengguna lupa isi playbook dan variabel apa yang harus disediakan lewat extra vars sebelum run.

## Keputusan

- `GET /api/ansible/playbooks/{id}/contents` (autentikasi dan Ansible Gate yang sama) mengembalikan `project` (entry, commit, daftar `{path, content}`, `truncated`) dan `vars`. Berlaku untuk sumber inline, bundle, dan Git.
- Sumber bundle dibaca dari ZIP tersimpan. Sumber Git di-clone dangkal ke workspace sementara per permintaan (kode `cloneGit` yang sama dengan run, termasuk token/SSH Key Library), dibaca, lalu dihapus. Tidak ada cache: isi selalu sesuai ujung branch saat ini, dengan konsekuensi beberapa detik latensi dan satu clone per pembukaan dialog. Kegagalan clone dikembalikan sebagai 502 dengan beberapa baris terakhir stderr git.
- Hanya berkas teks kode yang dikembalikan (`.yml/.yaml/.j2/.jinja2/.json/.ini/.cfg/.conf/.md/.txt/.sh/.py/.toml/.service`, serta berkas tanpa ekstensi di `group_vars/`/`host_vars/`). `.env`, kunci, berkas biner, `.git`, `.ansible/`, dan `collections/` tidak pernah dikembalikan. Batas 1.000 berkas, 4 MiB total, 256 KiB per berkas; YAML utama didahulukan, sisanya urut path, `truncated=true` bila ada yang dilewati.
- Variabel dianalisis statis di `internal/ansible/vars.go`: YAML utama diurai (termasuk `import_playbook`, `roles:`, `include_role`/`import_role`, `import_tasks`/`include_tasks`, `vars_files`/`include_vars` statis, dependensi `meta/main.yml`) dan semua string serta ekspresi (`when`, `failed_when`, `until`, `{{ }}`, `{% %}`, template role) dipindai pengenalnya. Hasil: `required` (dipakai, tidak didefinisikan, tidak dijaga `default()`/`is defined`), `optional` (hanya dipakai dengan penjaga), `defined` (nilai dan sumbernya dari `vars:`, `defaults/`, `vars/`, parameter role, `group_vars`/`host_vars`, `vars_files`, atau extra vars default). `register`, `set_fact`, variabel loop/`{% set %}`, dan magic variable (`item`, `ansible_*`, `inventory_hostname`, ...) dikecualikan.
- `group_vars`/`host_vars` hanya dihitung bila berada di root proyek atau di folder YAML utama, karena hanya itu yang dimuat Ansible pada run Pantau (Generated Inventory di root, ADR 0051). Inventory unggahan dan folder playbook lain diabaikan.
- Menambah dependency `gopkg.in/yaml.v3` (Go murni, tanpa dependensi transitif): mengurai YAML dengan regex tidak dapat diandalkan untuk anchor, flow mapping, dan blok bersarang.

## Batasan

Hasil adalah heuristik: Jinja dipindai, bukan dirender, sehingga nama yang dibangun dinamis (`vars[x]`), variabel dari inventory sendiri, `lookup` yang mengisi variabel, serta berkas vault tidak terdeteksi. Isi group_vars yang tidak dienkripsi vault akan tampil apa adanya di dialog; endpoint hanya untuk pengguna terautentikasi dengan Gate aktif.
