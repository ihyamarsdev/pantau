# Issue 07: Git-edit delete mengikuti direktori symlink → hapus file di luar clone

Status: resolved
Severity: HIGH

## Deskripsi
Cabang delete di `commitAndPush` hanya `os.Lstat(fullPath)` lalu `os.Remove(fullPath)` (`internal/ansible/git_edit.go:304-318`), tanpa `checkNoSymlinks` seperti cabang write (:320) dan `DeleteLocalRepoFile` (`local_repo.go:421`). Repo dengan `conf -> /home/pantau/.pantau` + request `{path:"conf/pantau.db", delete:true}` (via `handlers_ansible_git_edit.go:167` / `handlers_terraform_git_edit.go:152`) menghapus DB Pantau.

## Kriteria Penerimaan
- [x] `checkNoSymlinks` dipanggil sebelum Lstat di cabang delete.
- [x] Test: `git_edit_symlink_test.go` (gagal di kode lama, lulus sekarang).
