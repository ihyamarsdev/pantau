# 0082. Agentic AI guardrails, domain routing and scoped memory

Extends [ADR-0073](0073-in-process-ai-agent.md) (in-process agent), [ADR-0066](0066-hermes-ai-agent-and-mcp-tools.md)
(tools, gates, approvals, memory, SOUL) and [ADR-0074](0074-agent-tool-coverage-of-pantau-actions.md) (tool coverage).
It hardens the agent's remote execution path, groups its tools into domain modules, closes the ADR 0075–0081 tool
coverage gaps, and makes memory and context compaction scoped and configurable.

> Note: this repository already has other ADRs numbered 0082 (scoped memory / proactive triage). The number is kept
> as requested; the title disambiguates it.

## Context

1. **One shell path, no execution guardrails.** `internal/inspector/agent_exec.go` turns a model-supplied string into
   a command on a managed host. The approval gate lives in the web layer, but the inspector had no notion of risk:
   a large log could reach the LLM context uncut, secret-looking values were not redacted at that boundary, and a
   destructive command relied solely on the operator reading the approval card.
2. **Tool bloat.** ADR 0074 grew the tool list to well over a hundred tools. Sending every definition on every model
   call costs tokens. The agent already narrows tools by page (domains), but the domain names were short internal
   labels, and two ADR 0075–0081 features had no tool at all.
3. **Memory and compaction.** ADR-0082 (scoped memory) already added `host_id`/`cluster_id`/`domain` scoping and
   `RecallContext` local FleetSearch lookups. The compaction threshold, however, was a hardcoded constant, so an
   operator could not trade token cost for context retention.
4. **Telemetry.** ADR-0030's Activity Console received generic "tool call" events; they did not carry the risk tier
   or the numeric gate that decided whether an operator was asked.

## Decision

1. **Three strict risk tiers.** `internal/inspector/agent_guard.go` classifies every remote command into:
   - `read_only` (metrics, logs, status, fleet search) — executes immediately;
   - `mutating_safe` (service/container restart, `git fetch`, cache refresh) — executes and is logged to the
     Activity Console;
   - `destructive` (firewall changes, `kubectl drain`, `terraform apply`/`destroy`, file deletion, arbitrary
     `rm -rf`, `reboot`, Docker prune/volume removal) — blocked unless the context carries an explicit approval
     token bound to the exact command (`ApprovalTokenFor`), mirroring ADR-0044's confirmation pattern. The token is
     granted only after the operator approves the gate-2 card; auto mode and session grants never produce it.
   The tiers complement, not replace, the numeric gates (1 routine, 2 high impact, 3 infrastructure): gates decide
   *whether* to ask, tiers decide *how* to execute.
2. **Execution hardening.** `ExecShellContext` enforces a strict `context.Context` deadline (the shorter of the
   caller timeout, the configured limit and the context deadline), refuses NUL/control characters in model-supplied
   commands, redacts secret-looking values (`[REDACTED]`) and caps stdout and stderr at 3000 chars each with an
   announced head+tail truncation, so a large log never blows up the LLM context. `DockerLogs` applies the same
   redaction and cap. The legacy `ExecShell` remains as a compatibility wrapper with the same defaults.
3. **Domain modules.** Tools are grouped into five modules — `compute_systemd`, `docker_kubernetes`,
   `network_vpn_cloudflare`, `automation_git_iac`, `security_audit` — plus the always-on core and the
   `find_tools`/`set_tool_domain` meta-tools. The UI page loads only its module's tools; `set_tool_domain` accepts
   either the descriptive module name or the legacy short alias and widens the domain mid-chat. Classification
   stays in one place (`agentToolDomain`), so a new tool needs no changes in the registration files.
4. **ADR 0075–0081 parity.** Two missing tools are added: `vpn_host_status` (per-host VPN path status and read-only
   link probe, ADR 0077) and `gateway_cli_install` (checksum-verified CLI install, ADR 0081, gate 3 — always asks,
   never auto-approved, never grant-covered). The remaining features already had tools: Cloudflare access rules,
   tunnels, DNS export and token verification (ADR 0075), package update checks (ADR 0078), gateway CLI version
   checks (ADR 0080), and repository branch list/switch/create/delete (ADR 0079).
5. **Scoped memory and configurable compaction.** Memory scoping and FleetSearch-backed `RecallContext` come from
   ADR-0082 (scoped memory). The compaction threshold is now the setting `agent_compact_tokens` (clamped to
   20 000–200 000, default 60 000), read into `agentCfg.CompactTokens` and used for both the pre-tool and post-batch
   compaction checks. A failed summary call keeps the full history.
6. **Structured telemetry.** Every tool invocation emits `risk_tier`, `gate`, `domain`, `params`, `duration_ms` and
   `status` to the ADR-0030 Activity Console. Tool progress SSE frames carry `risk_tier`, which the chat UI shows
   as a badge (destructive actions get a warning border) so the operator sees the risk before the approval card.

## Consequences

- A destructive command cannot reach a host from the agent without a human decision; the approval token is bound to
  the exact command string, so replaying a card for a different command fails.
- Output at the shell boundary is capped at ~3000 chars per stream, below the 24 KiB history truncation, so the
  model sees a bounded result and the announced marker still survives.
- The tool list per chat turn is smaller; cross-domain work costs one `find_tools`/`set_tool_domain` round trip.
- The compaction threshold is operator-tunable; a value that is too low loses context mid-task, too high costs tokens.
- The approval card and the Activity Console remain the source of truth for what the agent did; the risk tier is
  advisory metadata, not a second approval system.
