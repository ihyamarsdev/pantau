# ADR Index

Architecture decisions live in `docs/adr/`. Read only the ADRs relevant to your task.

## Entry points by area

- **Agent / AI**: 0066 (Hermes, superseded in part), 0073 (in-process agent), 0074 (tool coverage), 0082 guardrails (this repo has several 0082s; use the title), 0082 scoped memory, 0082 proactive triage.

- **Activity Console / telemetry**: 0030.

- **Kubernetes**: 0043, 0044 (identity, usage metrics, action safety), 0045, 0047.

- **Cloudflare**: 0048, 0075 (access rules, tunnels, DNS export, token permissions).

- **VPN / network**: 0033 (network diagnostics), 0077 (per-host VPN path, bound dial), 0085 (supervisor circuit breaker).

- **Packages / gateway CLI**: 0078 (package update check), 0080, 0081 (gateway CLI update).

- **Git / repositories**: 0067 (cloned repos), 0079 (branch switch/create/delete).

- **Secrets vault**: 0084.

- **UI / design**: 0007 (workspace modal), 0013 (airgapped assets), 0039 (design system), 0083 (workspace modal maximize).

- **Single binary / zero dependency**: 0002.

For the full list, list `docs/adr/`; filenames are self-describing.
