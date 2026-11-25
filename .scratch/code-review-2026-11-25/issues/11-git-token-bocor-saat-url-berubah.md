# Issue 11: Git token tersimpan terkirim ke host baru saat git_url diubah

Status: open
Severity: MEDIUM

## Deskripsi
PUT playbook dengan `git_token` kosong mempertahankan token lama walau `git_url` pindah host (`internal/web/ansible_playbook_req.go:103`, `internal/store/ansible.go:233`). Sync berikutnya mengirim token ke host penyerang. Bisa dipicu agent LLM via prompt injection.

## Kriteria Penerimaan
- [ ] Bila host URL berubah dan `git_token` kosong: tolak request atau hapus token tersimpan.
- [ ] Cek pola yang sama untuk Terraform workspace / repo lain yang menyimpan token write-only.
