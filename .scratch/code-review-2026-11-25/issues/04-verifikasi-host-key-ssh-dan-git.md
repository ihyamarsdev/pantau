# Issue 04: Verifikasi host key SSH (runner & git) tidak persisten / dimatikan

Status: partially resolved
Severity: HIGH (MITM → kredensial & eksekusi kode)

## Deskripsi
1. TOFU known-hosts hanya di memori (`internal/sshrunner/runner.go:24-57`, dipakai :144, :290). Setelah restart, koneksi pertama ke tiap host menerima key apa pun — termasuk saat `DefaultKeyProvisioner` mengirim password SSH. Host yang di-reinstall sah justru ditolak sampai restart, tanpa tombol reset.
2. Git over SSH memakai `StrictHostKeyChecking=no` + `UserKnownHostsFile=/dev/null` (`internal/ansible/git.go:213`) untuk clone/push playbook, terraform, repo editor → server git palsu bisa menyuplai playbook/`local-exec` berbahaya.

## Kriteria Penerimaan
- [x] Host key SSH runner disimpan di DB (`ssh_host_keys`) dan diverifikasi ulang setelah restart. Gagal mencatat = koneksi ditolak.
- [x] Endpoint reset: `POST /api/hosts/{id}/hostkey-reset`. Tombol di UI: BELUM.
- [x] Git over SSH: `StrictHostKeyChecking=accept-new` dengan file known_hosts persisten (`ansible.GitSSHCommand`), bukan `no` + `/dev/null`.
- [x] Test: `sshrunner/hostkey_store_test.go`, `store/ssh_host_keys_test.go`, `ansible/git_ssh_hostkey_test.go`.
