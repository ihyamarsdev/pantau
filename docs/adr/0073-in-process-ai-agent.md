# 0073. In-process AI agent (native LLM providers, Hermes removed)

Replaces the external [Hermes Agent](https://github.com/NousResearch/hermes-agent) service of
[ADR-0066](0066-hermes-ai-agent-and-mcp-tools.md) with an agent that runs **inside the Pantau binary** and calls an
LLM provider directly over HTTP. The tools, gates, approvals, session grants, memory and SOUL of ADR-0066 stay; the
transport between Pantau and the agent does not.

## Context

ADR-0066 put the agent in a second container. That worked, but it cost a lot for what it delivered:

1. **A second service to deploy.** `docker compose up` started a `hermes` service with its own image
   (`sbx/hermes-agent`), its own volume (`hermes-data`) and its own `start.sh`. Pantau is a single binary
   ([ADR-0002](0002-golang-single-binary-sqlite.md)); the agent broke that promise.
2. **Two secrets before anything worked.** `HERMES_API_KEY` and `PANTAU_AGENT_TOKEN` both had to exist in `.env`
   (16+ characters each), and `HERMES_LLM_*` chose the model. The LLM key lived in a second container, so the operator
   had to keep two places in sync and the compose file was the only documentation of that contract.
3. **Two HTTP hops in a local network.** Pantau proxied one chat turn to Hermes' OpenAI-compatible API and Hermes
   called Pantau back at `/mcp` with a bearer token. Two plain-HTTP hops had to be reachable from each other, so the
   stack was only safe on loopback or a trusted network.
4. **Limits that came from the other process.** The approval wait was 170 s so that wait + a 120 s `exec_command` stayed
   below Hermes' 300 s tool timeout; `kube_action` drain can run 310 s, so the agent could see a timeout for an action
   that had actually finished. The compaction threshold was a Hermes config key (`compression.threshold_tokens`) written
   by `docker/hermes/start.sh`.
5. **Tools had to be re-described.** Hermes listed Pantau's tools as `mcp__pantau__<tool>`; the model saw a namespaced
   name for a Pantau function.
6. **The MCP endpoint had exactly one consumer.** `POST /mcp` existed only so Hermes could call Pantau. With the
   agent in-process there is no second party, so the endpoint, the token, the rotation endpoint and the env variable
   have no reason to exist.

## Decision

1. **The agent runs in-process, with no LLM SDK.**
   - Pantau speaks two wire formats itself over `net/http` and streams the answer as SSE:
     - **Anthropic Messages** (`POST /v1/messages`, header `anthropic-version: 2023-06-01`) for `provider: anthropic`;
     - **OpenAI-compatible chat completions** (`POST /v1/chat/completions`) for `provider: openai`, which covers
       OpenAI, OpenRouter, LiteLLM, vLLM, Ollama and any gateway reachable through `base_url`.
   - No vendor SDK and no new Go module: the request body, the SSE frames, the tool-call blocks and the usage counters
     are parsed by hand in `internal/web/agent_llm.go`. `provider` is `""` (not configured), `anthropic` or `openai`;
     `base_url` empty means the provider default.
2. **The tool loop is Pantau's own.** `internal/web/agent_loop.go` runs the turn: the model asks for tools, Pantau runs
   one tool at a time (the tool names are exposed as they are, e.g. `list_hosts`, not `mcp__pantau__list_hosts`), and the
   results go back to the model. At most **25 model calls per turn**.
3. **The conversation lives in memory.** History is held per chat session in the Pantau process (at most 20 sessions,
   2 h idle), not in the database, and is lost when Pantau restarts. Compaction happens at **60 000 prompt tokens** with
   one summary call; the newest user turns stay verbatim. A failed or cancelled turn keeps the tool calls that already
   completed.
4. **The tools, gates, approvals, grants, memory and SOUL of ADR-0066 are unchanged.** The same read and change tools
   run, every change still waits for an approval card, `exec_command` still always asks, session grants still end on
   **New chat**, **Revoke**, after 2 hours or a restart, agent memory notes stay in `agent_memory` and are part of
   System Snapshots, and the SOUL is still the system message of every turn.
5. **`/mcp` is removed.** With it go `/api/agent/rotate-token`, the `mcp_token` setting and the env variable
   `PANTAU_AGENT_TOKEN`. There is no second party that could call Pantau's tools.
6. **Settings and the one-time migration.** The agent settings live in the same settings table as everything else:
   `agent_enabled`, `agent_llm_provider`, `agent_llm_base_url`, `agent_llm_api_key`, `agent_llm_model`, `agent_approval`,
   `agent_soul`. A one-time migration copies `hermes_enabled` to `agent_enabled` (only when `agent_enabled` is absent)
   and deletes the obsolete rows `hermes_url`, `hermes_key`, `hermes_model`, `agent_mcp_token` and `hermes_enabled`.
   An operator who had Hermes enabled therefore finds the agent **enabled but unconfigured**: no provider, no model, so
   the chat answers HTTP 400 with "the AI agent is not configured: choose a provider and a model in Settings -> AI Agent".
7. **The settings API is rewritten.** `GET /api/agent/settings` returns
   `{enabled, provider, base_url, api_key_set, model, approval, soul, soul_is_default}`; `POST` accepts
   `{enabled, provider, base_url, api_key, model, approval, soul}` (all optional; an empty `api_key` keeps the stored
   key, and the key is never returned). `POST /api/agent/test` does one tiny model call and answers `{ok:true}` or
   `{ok:false,error}`. `POST /api/agent/chat` (body `{message, session_id}`), `/api/agent/approvals`,
   `/api/agent/approvals/{id}`, `/api/agent/grants` and `/api/agent/memory` keep their paths.
8. **The chat event format is unchanged.** Text arrives as `data: {"choices":[{"delta":{"content":"..."},"finish_reason":null}]}`,
   a tool as `event: agent.tool` with `data: {"tool","label","status":"running"|"done"|"error","toolCallId"}`, an error as
   `event: agent.error` with `data: {"error":"..."}`, and the turn ends with `data: [DONE]`. The old
   `hermes.tool.progress` event is gone.
9. **Compose is Pantau only.** The `hermes` service, the `hermes-data` volume, `docker/hermes/` and the `HERMES_*`
   variables are removed from `docker-compose.yml` and from the environment.

## Alternatives

1. **Keep Hermes Agent.** Zero new code, but every cost above stays: a second container, two more secrets, two HTTP
   hops, a tool timeout Pantau has to design around and a namespaced tool name for the model. The operator has to run
   and secure a second service for a chat window.
2. **Google ADK for Go v2 (`google/adk-go`).** Measured on a probe binary with `llmagent` + `runner` + the Anthropic and
   OpenAI models: **68 MB stripped**, against **1.9 MB** for a hello-world in the same toolchain and **23.9 MB** for
   Pantau itself. It pulls in **648 packages**, **80** of them gRPC, and `go.mod` would need **Go 1.26.10** and about
   **40** more modules. The native engine adds roughly **2 000 lines of Go** and **zero** dependencies, which fits
   Pantau's single-binary, zero-dependency shape (ADR-0002) and keeps the release artifact at 23.9 MB.
3. **Keep the MCP endpoint for a future external agent.** Rejected for now: it has no consumer, and it is a
   bearer-token-protected write surface aimed at a program outside Pantau. The tools can be exposed again if a real
   consumer appears.

## Consequences

- The LLM key now lives in **Pantau's own database** (`agent_llm_api_key`) instead of a second container's
  environment. It is a write-only setting: the API returns only `api_key_set`, and it is never rendered back into the
  settings form.
- That key is part of the **System Snapshot**: the snapshot export copies the whole `settings` table and the snapshot is
  encrypted with AES-256-GCM under the operator's passphrase before it is downloaded or pushed to GitHub. So the LLM key
  travels with the snapshot, like the other secrets Pantau already stores there (the SSH private keys, the Cloudflare
  and S3 tokens, the snapshot passphrase). It is never stored in cleartext in the snapshot file, but a snapshot
  passphrase is now a credential that also protects the LLM key.
- The **conversation history is lost on restart** (as before, when Hermes held it in its own container and Pantau kept
  only the approvals in memory). The agent memory notes survive, because they are in the database.
- Pantau now **maintains the provider adapters**: when Anthropic or an OpenAI-compatible gateway changes its request or
  stream format, the fix is ours. In exchange there is no upstream agent framework that decides which tools the model
  may call.
- There is **no web search tool**. The model can only use Pantau's tools, so it cannot look anything up on the internet;
  a question about the world outside the fleet is answered from the operator's own data.
- The **approval wait is Pantau's own choice now**: the 170 s cap no longer exists to keep the wait plus a 120 s
  `exec_command` below a foreign tool timeout, and a long action (a Kubernetes drain, for instance) can no longer be
  reported to the model as a timeout the operator did not cause.
- The compose file no longer starts a second service, and `.env` no longer needs `HERMES_*` or `PANTAU_AGENT_TOKEN`.
  An operator who upgrades in place keeps a stale `.env`: the variables are simply ignored.
- The agent is now reachable only through the Pantau session cookie, which removes the class of problems that came with
  a bearer token in a second container (a token in `.env`, a rotation endpoint, a token that had to be identical on both
  sides).
