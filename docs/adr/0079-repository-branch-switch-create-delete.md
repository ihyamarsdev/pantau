# 0079. Branch pada Cloned Repository: pilih, buat, dan hapus branch lokal

Melengkapi bagian **Repositories** ([ADR-0067](0067-cloned-git-repositories.md)) dengan pengelolaan branch: Repositori yang di-clone sebelumnya terkunci pada satu branch — branch yang dipilih saat clone (atau branch bawaan remote) — karena tidak ada cara berpindah branch tanpa menyentuh folder clone lewat SSH ke server Pantau. Sekarang kartu repositori punya dialog **Branches** dan agen AI punya `repo_branch`.

## Konteks dan Masalah

1. **Clone penuh tapi satu branch.** ADR-0067 memakai `git clone` penuh (bukan dangkal), jadi semua branch remote sebenarnya sudah ada di `refs/remotes/origin/*`; yang tidak ada hanyalah cara membuat branch lokal dari sana dan melakukan checkout. Tanpa itu operator harus masuk ke server dan menjalankan `git switch` sendiri, padahal seluruh alur edit–commit–push sudah ada di dashboard.
2. **`Commit & Push` hanya menawarkan dua target.** Dialog commit dapat mem-push ke branch aktif atau ke branch baru (`new_branch`), tetapi tidak bisa memilih branch yang sudah ada. Branch yang dibuat lewat `new_branch` lalu berakhir sebagai satu-satunya branch lokal, tanpa jalan kembali ke `main` atau ke branch teman satu tim.
3. **Branch yang hanya ada di remote tidak terlihat.** `LocalRepoStatus` hanya membaca branch dari baris `## ` `git status`, jadi UI tidak tahu branch apa saja yang tersedia.
4. **Bahaya yang harus dihindari.** Perpindahan branch yang menimpa edit yang belum di-commit dapat membuang pekerjaan operator; penghapusan branch dapat membuang commit yang belum di-merge; dan nama branch berasal dari API sehingga bisa disalahgunakan sebagai opsi git (`--force`) atau path (`../x`).
5. **Istilah.** Istilah baru **Repository Branch** dicatat di [CONTEXT.md](../../CONTEXT.md) agar tidak tertukar dengan **Repository Sync** dan **Git Editor** playbook Ansible.

## Keputusan Arsitektur

1. **Mesin di `internal/ansible/local_repo.go`**, satu paket dengan operasi clone lain sehingga memakai ulang `openLocalGit` (kredensial tidak pernah menyentuh clone, hook dimatikan), `sanitizeGitError`, dan `getBranchLock(dir, "")` — mutex per folder yang sama dengan Sync/Commit/Push/Discard, jadi perpindahan branch tidak bisa berlomba dengan commit.

2. **Tiga operasi, tiga perintah git.**

   | Operasi | Perintah | Perilaku |
   |---|---|---|
   | List | `for-each-ref refs/heads`, `for-each-ref refs/remotes/origin` | Branch lokal (nama, commit pendek, penanda aktif) + nama branch yang hanya ada di `origin` (`HEAD` dan yang sudah punya branch lokal dibuang) |
   | Switch / create | `git checkout <name>` / `git checkout -b <name>` | Branch yang hanya ada di remote otomatis dibuat dan tracking `origin/<name>` oleh git; `-b` membuat branch baru di HEAD |
   | Delete | `git branch -d -- <name>` | Hanya branch lokal; git menolak branch aktif dan branch yang commit-nya belum tergabung |

   Delete sengaja **`-d`, bukan `-D`**: branch dengan commit yang belum di-merge adalah pekerjaan yang belum tersimpan di mana pun, dan tidak ada force delete di UI. Branch di remote tidak pernah disentuh (tidak ada `git push --delete`).

3. **Validasi nama branch sebelum menyentuh git.** `ValidateGitBranch` yang sudah dipakai saat clone menolak nama kosong, karakter aneh, `..`, akhiran `/` atau `.lock`; parameter tambahan `--` pada `git branch -d` mencegah nama yang terbaca sebagai opsi. Handler mengembalikan `400` untuk nama yang tidak valid, `404` untuk branch yang tidak ada, dan pesan git yang disanitasi (`502`) untuk kegagalan lain — termasuk **penolakan checkout karena edit yang belum di-commit**, yang tidak pernah ditimpa paksa oleh Pantau.

4. **REST API** di bawah repo yang sudah ada: `GET /api/repos/{id}/branches` → `{branches:[{name,commit,current}], remote:[name]}`, `POST /api/repos/{id}/branches {name,create}` → status baru (branch, commit, ahead/behind), `DELETE /api/repos/{id}/branches?name=`. Tidak ada perubahan skema: branch hidup di folder clone, dan `git_repos.branch` tetap "branch saat clone" (dipakai hanya untuk keperluan dokumentasi entri).

5. **UI di kartu repositori**, bukan di editor: tombol **Branches** membuka dialog kecil dengan daftar branch lokal (commit pendek, badge *current*, tombol **Switch** dan hapus), bagian *on remote* untuk branch yang belum lokal, dan satu kolom untuk membuat branch baru di HEAD. Dialog menampilkan peringatan bila repositori punya perubahan belum di-commit, dan pesan git tampil apa adanya di dalam dialog saat perpindahan ditolak. Setelah berhasil, kartu disegarkan sehingga badge branch mengikuti.

6. **Agen AI: `repo_branch`.** Satu tool dengan argumen `action` (`list`, `switch`, `create`, `delete`) yang memanggil handler yang sama lewat loopback, sehingga validasi dan kunci folder tetap milik UI. `switch`/`create` adalah gate 1 (tidak menghilangkan data; checkout yang berbahaya sudah ditolak git), `delete` adalah gate 3 yang selalu meminta persetujuan. SOUL menyebut tool ini dan aturannya (jangan membuang edit operator hanya agar perpindahan berhasil).

## Konsekuensi

- Perpindahan branch mengubah isi working tree: file yang belum di-commit tetap ada bila tidak bertabrakan dengan branch tujuan (perilaku git), dan edit yang bertabrakan membuat operasi gagal tanpa kehilangan apa pun.
- Tidak ada pembuatan branch dari branch lain selain HEAD, tidak ada rename, tag, detached HEAD, rebase/merge, dan tidak ada penghapusan branch remote. Semua itu tetap pekerjaan terminal.
- Branch yang dibuat di sini menjadi branch lokal biasa: `repo_commit`/`Commit & Push` mem-push ke branch aktif dan membuatkannya di remote (`push -u`), sama seperti `new_branch` di ADR-0067.
- Bila dua browser membuka dialog yang sama, yang kedua dapat `404`/penolakan git untuk branch yang sudah berubah; tidak ada state bersama yang perlu disinkronkan.
