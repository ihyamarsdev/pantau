# 0086. Smart, context-aware agent SSH execution

Date: 2026-11-19

## Status

Accepted

## Context

The in-process AI agent (ADR 0073) reaches managed hosts through `internal/inspector/agent_exec.go` and
`internal/sshrunner/`. The first hardening pass added a strict context deadline, argument sanitization,
secret redaction, head+tail truncation and a three-tier safety gate (read_only / mutating_safe /
destructive). That pass made execution *safe* but not *fast* or *adaptive*:

1. The model writes shell commands without knowing the target OS. On macOS (ADR 0038) it reaches for GNU
   flags that do not exist; on legacy CentOS 5/6 (ADR 0005/0031) it reaches for `systemctl` where SysV
   is the init system. Each wrong guess is a failed SSH round-trip and a wasted model turn.
2. Interactive TUI/pager commands (`vim`, `less`, `htop`, bare `top`) hang a non-interactive SSH exec
   channel waiting for a TTY that never comes. The agent then waits out the full execution deadline.
3. A host bound to a VPN network that is down (ADR 0077) is only discovered after `sshrunner` dials and
   times out. The metadata already says the path is down, so the wait is avoidable.
4. Multi-step troubleshooting sends one command per tool call and one SSH connection per command.
5. Non-zero exit codes return a bare number, so the model often repeats the same failing command.
6. Large, repetitive log output floods the LLM context even after head+tail truncation.

## Decision

1. **Host fingerprint context injection.** A compact fingerprint is derived only from stored SQLite
   metadata — no extra SSH round-trip: OS family (linux/darwin from `SecurityScan.OSFamily`, `Host.Kernel`,
   `Host.OSInfo`), distro, architecture, init system (systemd / launchd / openrc / sysv, including the
   legacy CentOS 5/6 SysV case), package manager (apt/dnf/yum/pacman/apk/brew), sudo capability
   (passwordless `sudo -n`, ADR 0037) and bound VPN `network_id` state (ADR 0077). When the question
   names a host, `FormatHostFingerprint` emits one `key=value` line into the system prompt. A host whose
   bound network is `down`/`error` is called out inline and refused by `ExecShellContext` before any
   dial, with a diagnostic naming the network — fail closed, never fall back to the default route.

2. **Pre-flight SSH command lint & anti-hang guardrails.** Before a command reaches `sshrunner`,
   `LintInteractiveCommand` refuses interactive TUI/pager commands with an actionable hint pointing to a
   non-interactive alternative or a structured Pantau tool. `top` is allowed with `-b`/`-l` batch flags.
   `sudoNoInteractive` rewrites a bare `sudo` to `sudo -n` (ADR 0037) so no TTY password prompt can stall.
   The three-tier safety classification is unchanged: a destructive command still requires the exact
   human-in-the-loop approval token (ADR 0044 / ADR 0082 guardrails).

3. **Stateful session cwd & compound batch probes.** `AgentSessionState` tracks the working directory per
   `host_id` in memory, attached to the chat session's request context (ADR 0073: history is in memory
   and lost on restart). A successful `cd` in `exec_command` updates it via `TrackSessionCD`; subsequent
   commands run relative to it, failing closed if the directory no longer exists. `ssh_batch_inspect`
   runs up to 20 read-only probes in one SSH round-trip through a POSIX script with distinct labeled
   markers and per-probe exit codes, parsed back into labeled sections. It is gate 0 (read-only) and
   shares the same interactive-command lint.

4. **Exit-code hints & output compression.** `exitHint` maps 127 (command not found → PATH/OS binary),
   126/1 (permission denied → elevated tool if permitted), 124/context deadline (narrow the filter) and
   transport errors to a structured `[hint]` appended to stderr. `CompressAgentOutput` collapses
   consecutive duplicate lines to `[repeated Nx]`, redacts credentials (ADR 0031), and applies
   head+tail truncation at a ~2,500-char default cap with total line/byte metadata so a cut is never
   silent. `DockerLogs` reuses the same compressor.

5. **Verification.** Go unit tests cover fingerprint formatting/derivation, interactive blocking, sudo
   rewriting, cwd state, exit hints, dedup/truncation, batch round-trip and the retained destructive
   gate. One e2e spec covers the gate-0 batch tool and the fingerprint context injection.

## Consequences

- Fewer failed SSH round-trips and model turns: the model picks OS-correct commands on the first try and
  gathers related probes in one call.
- Interactive hangs are impossible through the agent shell path; a dead VPN path fails immediately.
- The fingerprint is derived from metadata that may be stale after a host changes OS — it is a hint, not
  a live probe; the model must still verify state after a change (SOUL rule 3).
- Session cwd is in-memory only and disappears on restart, consistent with ADR 0073 conversation history.
- The interactive blocklist is conservative: an exotic interactive command not listed can still hang and
  is bounded by the existing execution deadline. The list is the place to add new ones.
