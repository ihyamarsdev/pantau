# Issue 10: Nama playbook → path traversal & IaC gate fail-open

Status: open
Severity: MEDIUM

## Deskripsi
Nama playbook hanya dicek panjang (`internal/web/ansible_playbook_req.go:37-39`, `handlers_ansible_import.go:65-67`). `iacAnsibleRoot` menulis ke `filepath.Join(tmp, name)` (`internal/web/handlers_iacscan.go:281-289`):
- `../../../home/pantau/.config/x` → file ditulis di luar temp dir (bisa via agent `agent_tools_iac.go:178`).
- `ops/site` → WriteFile gagal → gate dianggap `not_assessed` dan tidak memblok → scan IaC/secret selalu terlewati.

## Kriteria Penerimaan
- [ ] Tulis ke nama tetap (`playbook.yml`) atau `filepath.Base` seperti cabang Terraform (:252).
- [ ] Tolak `/`, `\`, `..` di nama playbook (create, update, import).
- [ ] Error tulis di gate = block, bukan not_assessed.
