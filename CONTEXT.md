# Pantau

Agentless SSH-based server monitoring and management: desired-state verification, automatic drift diagnosis, and file/terminal management. Single Go binary, zero runtime dependencies, SQLite storage.

## How to read this repo (token-efficient)

`CONTEXT.md` is a map, not a dump. **Read only the slice your task needs**, plus the relevant ADR(s). Do not read `docs/user-guide*.md` (~200 KB) unless the task is about end-user documentation.

| Your task touches… | Read this slice |
| --- | --- |
| Inspection engine, desired state, metrics, alerts, host lifecycle, packages | [backend-inspector.md](docs/agents/context/backend-inspector.md) |
| SQLite schema, migrations, snapshots, fleet search, host groups | [store-sqlite.md](docs/agents/context/store-sqlite.md) |
| Ansible, Terraform, gateway CLI updates, IaC | [automation-ansible-terraform.md](docs/agents/context/automation-ansible-terraform.md) |
| VPN networks, network diagnostics, Cloudflare, ports | [network-vpn-cloudflare.md](docs/agents/context/network-vpn-cloudflare.md) |
| Security scan, firewall, hardening, 2FA, secrets vault | [security-devsecops.md](docs/agents/context/security-devsecops.md) |
| Vanilla UI, terminal, modals, shortcuts, Activity Console | [frontend-vanilla-ui.md](docs/agents/context/frontend-vanilla-ui.md) |
| In-process AI agent, tools, gates, memory, SOUL | [ai-agent.md](docs/agents/context/ai-agent.md) |
| Docker, Kubernetes, cloned Git repositories | [docker-kubernetes-repos.md](docs/agents/context/docker-kubernetes-repos.md) |
| Finding the right architecture decision | [adr-index.md](docs/agents/context/adr-index.md) |

Full ADR list: `docs/adr/` (filenames are self-describing). Agent conventions: `AGENTS.md`, `docs/agents/`.

## Core vocabulary (used everywhere)

- **Host**: physical or virtual Linux/macOS server managed over agentless SSH. _Avoid_: node, instance, machine.
- **Desired State**: rules defining the expected condition of a Host (service active, disk under threshold, container running). _Avoid_: baseline, blueprint.
- **Desired Rule**: one expectation (Kind: `container`/`service`/`port`/`disk`/`cron`/`backup`/`process`/`security_updates`/`file_integrity`, Target, Expected, Enabled). _Avoid_: config check, health probe.
- **Actual State**: real condition of a Host from the latest Inspection. _Avoid_: current status, telemetry snapshot.
- **Inspection**: periodic agentless SSH read of Actual State. _Avoid_: polling, health check run.
- **Drift**: mismatch between Actual State and Desired State. _Avoid_: divergence, error state.
- **Host Status**: `healthy` (all rules met), `degraded` (SSH reachable but drift/failure), `down` (SSH could not be opened, incl. a dead VPN path), `unknown` (never inspected). _Avoid_: online/offline.
- **AI Agent**: assistant running inside the Pantau process (not a separate service); calls Agent Tools directly; identity/rules from SOUL, model from LLM Provider. _Avoid_: bot, copilot.
- **Agent Tool**: one Pantau function the agent calls by plain name; every mutating call waits for Agent Approval. _Avoid_: plugin, skill.

Detailed glossary terms live in the domain slices above. Use the vocabulary defined there; flag ADR conflicts rather than silently overriding them.
