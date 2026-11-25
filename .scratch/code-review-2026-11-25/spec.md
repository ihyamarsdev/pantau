# Code Review Pantau — 2026-11-25

Metode: skill **open-code-review-delegate** (alibaba/open-code-review, CLI `ocr` v1.14.3). Rule review diambil via `ocr delegate rule` (Go & JS), lalu seluruh codebase (~90k baris Go non-test + ~44k baris JS) direview per area oleh 7 reviewer paralel. Prinsip OCR: presisi di atas recall — hanya temuan yang diverifikasi dari kode dan pemanggilnya.

Cakupan: main.go, internal/web (core, handlers, agent, terraform, ansible), internal/inspector, internal/store + vault/snapshot/s3/instance/logger/sshconfig/sshrunner, paket integrasi (ansible, terraform, iacscan, osv, vulnintel, cloudflare, dockerhub, gwupdate, updater, notify, transfer, netdiag, globe, vpnnet), dan frontend `internal/web/static/app/**`. Tidak direview: `*_test.go`, vendor JS, e2e. Sebagian file besar hanya di-scan via grep (lihat catatan tiap issue).

## Daftar Task

| # | Severity | Status | Task |
|---|----------|--------|------|
| 01 | HIGH | resolved | [Shell injection pada target desired-state rule](issues/01-shell-injection-target-desired-rule.md) |
| 02 | HIGH | partial | [Agent LLM bisa jalankan perintah destruktif tanpa approval](issues/02-agent-batch-inspect-dan-destructive-classifier.md) |
| 03 | HIGH | resolved | [Tunnel origin bersama → session admin ke port mana pun](issues/03-tunnel-origin-bersama-session-admin.md) |
| 04 | HIGH | partial | [Verifikasi host key SSH & git](issues/04-verifikasi-host-key-ssh-dan-git.md) |
| 05 | HIGH | resolved | [Snapshot restore merusak vault & export parsial](issues/05-snapshot-restore-vault-dan-export-parsial.md) |
| 06 | HIGH | resolved | [Terraform apply: state hilang & double apply](issues/06-terraform-apply-state-dan-race.md) |
| 07 | HIGH | resolved | [Git-edit delete mengikuti symlink](issues/07-git-edit-delete-symlink.md) |
| 08 | MEDIUM | open | [Hardening login, 2FA, session](issues/08-hardening-autentikasi-dan-2fa.md) |
| 09 | MEDIUM | open | [Kebocoran secret (log, vault, agent, file sync)](issues/09-kebocoran-secret-log-vault-agent.md) |
| 10 | MEDIUM | open | [Nama playbook path traversal & IaC gate fail-open](issues/10-playbook-name-path-traversal.md) |
| 11 | MEDIUM | open | [Git token bocor saat git_url berubah](issues/11-git-token-bocor-saat-url-berubah.md) |
| 12 | MEDIUM | open | [Plan agent ter-auto-apply](issues/12-agent-plan-auto-apply.md) |
| 13 | MEDIUM | open | [Stored XSS frontend](issues/13-xss-frontend.md) |
| 14 | MEDIUM | open | [Option injection netdiag](issues/14-netdiag-option-injection.md) |
| 15 | MEDIUM | open | [Self-update signature & restart handover](issues/15-self-update-signature-dan-restart.md) |
| 16 | MEDIUM | open | [Browser bridge di /tmp](issues/16-browser-bridge-tmp.md) |
| 17 | MEDIUM | open | [Alert drift palsu saat exec gagal](issues/17-false-drift-alert-saat-exec-gagal.md) |
| 18 | LOW | open | [Kebocoran resource & race kecil](issues/18-leak-dan-race-resource.md) |
| 19 | LOW | open | [Koreksi kecil backend](issues/19-koreksi-kecil-backend.md) |
| 20 | LOW | open | [Error handling fetch frontend](issues/20-error-handling-frontend.md) |

Urutan pengerjaan yang disarankan: 01 → 02 → 03 → 07 → 05 → 06 → 04, lalu MEDIUM. Issue 01, 02, 10, 11, 12 bisa dipicu lewat prompt injection ke agent LLM — prioritaskan.
