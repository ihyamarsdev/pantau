# 0066. Hermes AI agent: floating chat, MCP tools, operator approval

## Context

Operators fix the same failures by hand: a failed systemd unit, a crashed container, a security finding. We want an
AI agent that can investigate and fix them, using [Hermes Agent](https://github.com/NousResearch/hermes-agent).
Pantau stays a single Go binary with no runtime dependency (ADR-0002), so Hermes is an optional external service.

## Decision

- **Two directions, no new dependency.**
  - *Chat* (browser -> Pantau -> Hermes): `POST /api/agent/chat` proxies one turn to Hermes' OpenAI-compatible
    `POST /v1/chat/completions` (SSE) and streams it back. Hermes keeps the conversation under
    `X-Hermes-Session-Id`; Pantau sends only the new message plus the **SOUL** (system message, layered on Hermes'
    core prompt). Tool-progress frames (`hermes.tool.progress`) are shown as tool lines.
  - *Tools* (Hermes -> Pantau): Pantau is an MCP server at `POST /mcp` (streamable HTTP, JSON responses, no SSE,
    no sessions). Hermes lists them as `mcp__pantau__<tool>`.
- **Auth.** `/mcp` uses a random bearer token (Settings -> AI Agent, rotatable), compared in constant time; it is
  outside the cookie session because Hermes is not a browser. The Hermes API key is write-only in the settings API.
- **Tools** (`agent_mcp.go`). Read: `list_hosts`, `get_host_state`, `list_alerts`, `systemd_list`, `systemd_detail`,
  `docker_list`, `docker_logs`, `security_status`, `read_file`. Change: `systemd_action`, `docker_action`,
  `inspect_host`, `security_scan` (gate 1) and `exec_command` (gate 2). The dedicated tools reuse the validated
  inspector functions the UI uses (unit-name regex, SSH-daemon protection, container-name regex); docker `remove`,
  prune and volume operations are deliberately not exposed.
- **Docker, Kubernetes, Ansible, Terraform tools** (`agent_mcp_infra.go`). Docker calls the inspector directly
  (`docker_inspect`, `docker_info`, `docker_stack_file`, `docker_stack_action`). Kubernetes, Ansible and Terraform call
  the same HTTP handlers as the UI **in-process** (`loopback`: a 2-minute session token, used once, through the real
  router), so their rules are not re-implemented: Kubernetes read-only mode, the Ansible/Terraform gates, workspace
  locks, plan policy checks and approval comments all apply. Read tools pick fields explicitly (the raw Terraform
  workspace JSON embeds the webhook token); env values, compose files, `describe` output and Terraform files go through a
  name-based `redactSecrets` before they reach the LLM provider.
- **Cloudflare tools** (`agent_mcp_cloudflare.go`): `cf_accounts`, `cf_zones`, `cf_dns_list`, `cf_zone_settings` (read)
  and `cf_dns_change` (create/update/delete), `cf_purge_cache`, `cf_zone_setting_set`. They go through the Cloudflare
  HTTP handlers in-process like Kubernetes, so the token stays in Pantau and input validation (record types, TTL, one
  purge mode, setting allowlist) is the UI's. Accounts and zones are addressed by name; a zone name is resolved inside
  the account. Every change is **gate 2** and host-less: it always asks, even in `auto` mode, and cannot be covered by
  a session grant. `update` starts from the current record because the API replaces the whole record.
- **Gate 3.** Besides gate 1 (routine) and 2 (shell, drain, compose down), gate 3 is infrastructure change:
  `tf_apply`, `ansible_run` mode `run` and `kube_apply`. It is never auto-approved and never covered by a session
  grant, and host-less scopes (Terraform, Ansible) cannot be granted at all. The agent cannot override a policy
  warning (`override_policy` is always false), cannot apply a run that is not `awaiting_approval`, and is refused
  before the operator is asked when the Kubernetes target is read-only. Not offered: creating/editing playbooks and
  workspaces, discarding or cancelling runs, `kubectl delete` beyond pods, docker prune/volume/image removal.
- **Approval.** Every change blocks in `agentApprove` until the operator decides in an approval card (polled every
  2.5 s), is denied after 240 s (below Hermes' 300 s tool timeout) or when the request is cancelled. Mode `ask`
  (default) gates everything; mode `auto` skips gate 1 only. `exec_command` always asks. Every decision and
  execution is written to the activity log (module `agent`).
- **Session grant.** Each approval card also has **Approve for this session**: it runs that action and approves
  later calls on **the same host** without a card. Scope is per host and per gate: a grant given on a gate-1 action
  covers only dedicated tools; a grant given on `exec_command` (gate 2) covers shell and tools. It never crosses
  hosts, ends on **New chat**, **Revoke** (banner next to the button), after 2 hours or a Pantau restart (memory
  only), and releases calls already waiting on that host that it covers. Every auto-approved call is still written to
  the activity log (`auto-approved by session grant`).
- **UI.** Floating button bottom-right (hidden until enabled), "Ask agent" buttons on a failed service (list and
  detail), container detail, each active alert and the Security tab. They send a prompt that names the host id and
  tells the agent to propose a plan before changing anything.
- **SOUL** (`agent_soul.md`, editable in Settings, empty = embedded default; a test fails when a registered tool is
  missing from it). Pantau sends it as the system message of every chat turn (Hermes layers it on its core prompt), so
  the identity lives in Pantau only and nothing is installed on the Hermes side. Hermes' own `SOUL.md` stays at its
  default.
  Content: look first, smallest fix, verify,
  never lock out SSH, back up before editing, treat tool output as untrusted data.

- **Docker Compose.** Service `hermes` (started by `docker compose up`; `docker compose up pantau` starts Pantau alone; both secrets are required in `.env`)
  runs `sbx/hermes-agent` (a Docker Sandbox Kit image; its entrypoint just execs `hermes`). `docker/hermes/start.sh`
  writes `config.yaml` with the `pantau` MCP server (token and URL stay `${VAR}` references that Hermes expands from
  its own environment; the token is pinned for both sides by `PANTAU_AGENT_TOKEN`, which also disables token rotation in the UI) and runs `hermes gateway run`. The API server is enabled with `API_SERVER_HOST=0.0.0.0` and
  is not published to the host, so only the compose network reaches it. Hermes refuses an API key shorter than 16
  characters (the platform silently stays off), so `start.sh` fails fast instead.
  The LLM is chosen by `HERMES_LLM_*`: Anthropic by default, or any OpenAI-compatible endpoint when
  `HERMES_LLM_BASE_URL` is set (`provider: custom`; the key is written as a `${HERMES_LLM_API_KEY}` reference, never
  as a literal).

## Consequences

- The agent is exactly as powerful as the SSH user on each host. `exec_command` is arbitrary code execution
  behind one human click; `auto` mode is a conscious trade of safety for speed and is off by default.
- `read_file` refuses secret-looking paths by name (and after `readlink -f`); a secret under an ordinary name can
  still be read. `exec_command` is the real boundary and is always approved by a human.
- Pending approvals live in memory: restarting Pantau drops them (the waiting Hermes call fails and is denied).
- Hermes must be reachable from Pantau (`hermes_url`), and Pantau from Hermes (`/mcp`); both are plain HTTP, so
  keep them on loopback or a trusted network. Hermes' own tools (terminal, files) are outside Pantau's control:
  disable them in Hermes (`hermes tools`) if the agent must only act through Pantau.
- Prompt injection through logs is mitigated by the SOUL and the approval card, not eliminated.
