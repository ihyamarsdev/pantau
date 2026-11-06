# Git Edit, Commit & Push (Ansible Git playbooks + Terraform Git workspaces)

Status: ready-for-agent

Saat ini Git Sync (Ansible) dan VCS (Terraform) hanya membaca repositori. Tambahkan kemampuan **menjelajah file repo, mengedit, commit, dan push** dari dashboard, untuk playbook Ansible `source=git` dan workspace Terraform `source=git`. Tetap stateless: setiap request meng-clone dangkal ke direktori sementara (pola `CloneGitRepo`/`SyncGit`), tanpa checkout permanen. Semua di balik Gate masing-masing (`checkAnsibleGate` / `checkTerraformGate`).

## Paket `internal/ansible` (git_edit.go; stdlib; dipakai juga Terraform, preseden `ansible.CloneGitRepo`)

```go
type RepoFile struct{ Path string `json:"path"`; Size int64 `json:"size"` }
type RepoTree struct{ Commit, Branch string; Files []RepoFile; Truncated bool }
// Clone dangkal -> semua file reguler (bukan symlink, bukan .git/), urut path, maks 2000 entri (Truncated=true bila lebih).
func ReadRepoTree(ctx context.Context, g GitSource) (*RepoTree, error)
// path divalidasi; file <= 1 MiB, UTF-8 valid, tanpa NUL (biner ditolak: ErrNotText); symlink ditolak.
func ReadRepoFile(ctx context.Context, g GitSource, path string) (content, commit string, err error)

type CommitChange struct{ Path, Content string; Delete bool }
type CommitSpec struct {
    Message, AuthorName, AuthorEmail string
    BaseCommit string // commit yang dilihat pengguna saat membuka editor; != HEAD hasil clone => ErrRepoChanged
    NewBranch  string // "" = push ke branch g.Branch (atau branch default remote bila g.Branch kosong); selain itu buat branch baru dari HEAD dan push ke situ
    Changes    []CommitChange
}
type CommitResult struct{ Commit, Branch string; Changed int; Pushed bool }
var ErrRepoChanged, ErrNothingToCommit, ErrPushRejected error
func CommitAndPush(ctx context.Context, g GitSource, spec CommitSpec) (*CommitResult, error)
```
Aturan wajib:
- Path: relatif, `/` saja, tanpa `..`, `.`, kosong, awalan `/`, komponen `.git` (case-insensitive), NUL/kontrol, panjang <= 255; tulis lewat `os.Lstat` per komponen: tolak jika ada komponen berupa symlink (cegah tulis keluar clone). Hapus hanya file reguler. Buat direktori induk bila perlu (0755), file 0644.
- Batas: <= 50 perubahan, 1 MiB per konten, 5 MiB total, konten UTF-8 tanpa NUL; Message 1..500 karakter (trim), tanpa NUL; AuthorName 1..100 tanpa `<>` dan newline; AuthorEmail regex sederhana `^[^\s@<>]+@[^\s@<>]+$` <= 120; kosong -> default `Pantau <pantau@localhost>`. NewBranch lewat `ValidateGitBranch` dan tidak boleh sama dengan branch sekarang.
- Alur: clone dangkal (`CloneGitRepo`, protocols "https:ssh"; injectable `commitAndPush(..., protocols)` untuk tes dengan repo lokal/bare) -> bandingkan HEAD dengan BaseCommit (jika BaseCommit != "" dan beda => ErrRepoChanged) -> tentukan nama branch sekarang (`rev-parse --abbrev-ref HEAD`; detached/kosong => error jelas) -> bila NewBranch `checkout -b` -> tulis/hapus file -> `git add -A -- <path...>` (hanya path yang diubah) -> bila tidak ada diff staged => ErrNothingToCommit -> `commit` dengan env `GIT_AUTHOR_NAME/EMAIL`, `GIT_COMMITTER_NAME/EMAIL`, flag `-c commit.gpgsign=false -c core.hooksPath=/dev/null -c credential.helper=` (pesan lewat `-F -` stdin atau `-m` argumen terpisah, tanpa shell) -> `git push origin HEAD:refs/heads/<branch>` (tanpa `--force` SELALU; ditolak non-fast-forward / auth => ErrPushRejected dengan pesan git yang disanitasi) -> kembalikan SHA baru.
- Env git, kredensial token (`http.<host>.extraHeader`) dan SSH key identik dengan `CloneGitRepo` (refaktor kecil: ekstrak pembentuk env/ssh-cmd dari `CloneGitRepo` agar dipakai ulang oleh push, tanpa mengubah perilaku clone). Token TIDAK PERNAH muncul di error/log: gunakan `sanitizeGitError`.
- Mutex per `url+branch` (map global) agar dua commit bersamaan ke branch yang sama diserialkan.
- Tes: repo bare lokal + clone via protocols "file" (seam injectable, jangan melemahkan validasi produksi): tree/read, commit tambah/ubah/hapus, BaseCommit usang => ErrRepoChanged, tanpa perubahan => ErrNothingToCommit, push ke branch baru, push non-ff ditolak (repo diubah di antara dua panggilan tanpa BaseCommit), path traversal/symlink/.git/biner/ukuran ditolak, token tidak bocor di error.

## REST

Ansible (di `handleAnsiblePlaybookRoute`, `case "git"` yang sudah ada; hanya `source=git`, selain itu 400; `git` tidak ada di PATH => 502):
| Method + path | Body -> respons |
|---|---|
| `GET /api/ansible/playbooks/{id}/git/files` | `{commit, branch, files:[{path,size}], truncated}` |
| `GET /api/ansible/playbooks/{id}/git/file?path=` | `{path, content, size, commit}`; biner 415; tak ada 404 |
| `POST /api/ansible/playbooks/{id}/git/commit` | `{message, author_name, author_email, base_commit, new_branch, changes:[{path, content, delete}]}` -> `{ok, commit, branch, pushed, changed}` |

Terraform (`handleTerraformWorkspaceRoute`, `case "git"` baru; hanya `source=git`):
`GET /api/terraform/workspaces/{id}/git/files`, `GET .../git/file?path=`, `POST .../git/commit` — bentuk sama persis. Path relatif ke ROOT repo (bukan ke `git_path`).

Kode status: 400 validasi/`ErrNothingToCommit` ("no changes to commit"), 409 `ErrRepoChanged` (pesan: repositori berubah sejak dibuka, muat ulang) dan `ErrPushRejected`, 413 melebihi batas, 415 bukan teks, 502 clone/push gagal (pesan tersanitasi). Timeout: tree/file 60 dtk, commit 120 dtk. Token/URL berkredensial tidak pernah di body error. Log aktivitas (`logger.Info`) mencatat commit (nama resource, branch, sha) tanpa isi file.
Setelah commit sukses ke branch yang dipakai resource (bukan `new_branch`): Ansible memanggil `SetAnsiblePlaybookGitSync(id, now, sha)`.
Komentar kode & dokumen wajib menyebut: token HTTPS/kunci SSH harus punya izin TULIS pada repo; token baca-saja menghasilkan push gagal 502 dengan pesan git.
Seam tes: `Server` punya field fungsi (seperti `ansibleSync`) agar tes handler tidak butuh jaringan: pakai repo bare lokal lewat seam tersebut.

## UI (index.html, id+en i18n; hanya agent UI yang menyentuh file ini)
Satu komponen JS generik "Git Editor" (modal Workspace Modal ukuran tetap sesuai ADR 0007) yang diparametrisasi base URL, dipakai di dua tempat:
- Ansible: di editor playbook (blok Git yang sudah ada, dekat tombol Sync Now) tombol **Edit Repository**; Terraform: tab **Settings** workspace git, kartu VCS, tombol **Edit Repository**.
- Layout: kiri pohon/daftar file (pencarian, folder collapsible, penanda file diubah/baru/dihapus), kanan editor teks (pakai CodeMirror yang sudah ada dan mode yang tersedia: yaml/shell/clike untuk .tf bila cocok, selain itu plain; fallback textarea), toolbar: **New File**, **Delete**, **Discard changes**, **Reload** (peringatan bila ada perubahan belum di-commit, via Confirmation Dialog), daftar perubahan tertunda, tombol **Commit & Push** (aktif bila ada perubahan).
- Dialog Commit & Push (Action Dialog): pesan commit (wajib), nama & email author (diingat di localStorage), pilihan **Push ke branch {branch}** atau **Buat branch baru** (input nama), ringkasan jumlah file; peringatan bahwa token/kunci perlu izin tulis. Hasil: toast + commit sha pendek; error 409 `base_commit` tampilkan tombol Reload; error 502 tampilkan pesan apa adanya.
- File biner/terlalu besar (415/413) tampil pesan, tidak bisa diedit. Escape semua string. Tanpa CDN/aset baru. Semua string di I18N en dan id.

## Non-goal
Force push, merge/rebase/pull, resolusi konflik, multi-commit, riwayat/diff commit, pull request, GPG signing, hook, submodule, upload biner, rename file (hapus + buat baru).
