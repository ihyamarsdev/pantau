# Issue 09: Kebocoran secret (log, vault scope, agent, file sync)

Status: open
Severity: MEDIUM

## Deskripsi
1. Redaksi logger hanya untuk broadcast UI; stderr/journald dapat plaintext (`internal/logger/logger.go:206-209`, redaksi di :74-76).
2. Satu baris vault yang gagal decrypt membuat `refreshVaultRedactor` tidak memasang redactor sama sekali (`internal/store/vault.go:252-276`, `internal/web/handlers_vault.go:34-37`).
3. Semua secret vault (semua scope) diinjeksi ke setiap run Terraform/Ansible (`internal/web/ansible_launch.go:95-107`, `terraform_runs.go:173-176`, `handlers_vault.go:44`); log Ansible tidak di-redact vault (`internal/ansible/run.go:202`), disimpan di DB dan dikirim ke LLM via `log_tail` (`agent_tools_infra.go:604-606`).
4. `repo_read_file` tidak memakai `agentSecretPathRe` → `id_rsa`/`.env` terkirim ke LLM (`internal/web/agent_tools_repo.go:126-150`).
5. Vault sync ke host: `sftp.Create` (0644) lalu tulis secret sebelum chmod; exit code fallback `chmod 600` diabaikan tapi tetap lapor 0600 (`handlers_vault.go` `syncVaultToHost`).

## Kriteria Penerimaan
- [ ] Record di-redact sebelum `primary.Handle`.
- [ ] Baris gagal decrypt di-skip (log ID), redactor selalu terpasang.
- [ ] Injeksi hanya secret dalam scope run; writer log Ansible dibungkus `vaultRedactor`.
- [ ] `repo_read_file` menerapkan cek path secret + redaksi blok PEM.
- [ ] `OpenFile(O_WRONLY|O_CREATE|O_TRUNC)` + `Chmod(0600)` sebelum write; exit code non-zero = error.
