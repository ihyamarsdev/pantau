# Issue 02: Agent LLM bisa menjalankan perintah destruktif tanpa approval

Status: partially resolved
Severity: HIGH (prompt injection dari output host → aksi destruktif)

## Deskripsi
1. `ssh_batch_inspect` terdaftar dengan `gate: 0` (`internal/web/agent_tools.go:74`, `toolBatchInspect` :780-810) — tidak pernah memanggil `agentGate`. "Read-only" hanya klaim di deskripsi.
2. Satu-satunya pengaman adalah `ClassifyAgentCommand` pada script gabungan. `BuildBatchScript` membungkus tiap perintah sebagai `{ cmd; }` (`internal/inspector/agent_session.go:110-113`, `agent_batch.go:31`), sementara regex destruktif (`internal/inspector/agent_guard.go:58-69`) hanya match di awal string atau setelah `;&|` → `{ rm -rf …`, `sudo reboot`, `sudo dd` lolos. `curl … | sh`, `sed -i`, `apt-get remove`, append ke `authorized_keys` tidak match pola apa pun.
3. Session grant gate-2 meng-auto-approve `exec_command` destruktif dan melampirkan `withDestructiveApproval` (`internal/web/agent_tools.go:755-762`, `internal/web/agent.go:367`).

## Kriteria Penerimaan
- [x] `ssh_batch_inspect` di-gate 2 (approval operator untuk seluruh batch).
- [x] Perintah destruktif ditolak di dalam batch (`BatchInspect`), walau batch disetujui; harus lewat `exec_command`.
- [ ] Allowlist biner read-only: BELUM. `curl | sh`, `sed -i` tetap tergolong read-only oleh klasifier; approval yang menutup celahnya.
- [x] Pola destruktif cocok setelah `sudo`, `xargs`, `env`, `nohup`, `{`, `(`, newline, dan assignment env.
- [x] Perintah destruktif tidak tercakup session grant (`agentApprove`).
- [x] Test: `agent_guard_bypass_test.go`.
