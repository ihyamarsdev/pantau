# 0091. Agent chat history in SQLite (per-resource threads)

The AI agent's conversation used to live **in memory only** ([ADR-0073](0073-in-process-ai-agent.md) §3: at most 20
sessions, 2 h idle, lost on restart). It now lives in SQLite, grouped under the resource a conversation is about, so a
page reload or a Pantau restart resumes it.

## Context

1. **Every restart lost the conversation.** An operator who asked the agent to diagnose a drift, then restarted Pantau
   for an update, came back to an empty chat — with the diagnosis and the half-approved plan gone. The agent's *memory
   notes* survived (they are in `agent_memory`), but the actual exchange did not.
2. **Every reload lost it too.** The conversation was keyed by a browser session id, and the panel kept its messages in
   `localStorage`-less JS state, so closing the tab dropped it.
3. **Nothing tied a conversation to its subject.** A chat about `nginx` on host 7 and a chat about a Terraform
   workspace were both just "session `pantau-ab12`". There was no way to list "what did we discuss about this host".
4. **Unbounded growth was the reason memory was chosen.** Storing every tool result (log dumps of tens of KB) would
   bloat the database, and the first design had no pruning.

## Decision

1. **Two tables, `agent_threads` and `agent_messages`**, created by the idempotent schema block in `db_migrate.go`
   (`CREATE TABLE IF NOT EXISTS`): a thread carries `scope_type` (`global`, `host`, `cluster`, `docker`, `ansible`,
   `terraform`), `scope_id`, `session`, `title`, `created_at`, `updated_at`; a message carries `thread_id`, `role`,
   `content`, `tool_calls_json`, `approval_status`, the tool call id/name and the failure flag. `agent_messages`
   references `agent_threads` with `ON DELETE CASCADE`, and both foreign keys are indexed.
2. **The scope is the grouping key.** A new thread is opened under the resource the chat is about: the open host, else
   the active page (`docker`/`kube` → docker, `ansible`, `terraform`), else `global`. `GET /api/agent/threads?
   scope_type=host&scope_id=7` lists that resource's conversations.
3. **Rolling pruning keeps the database bounded, in the same transaction as the write.** After an append, a thread
   keeps its newest **200** messages and the database keeps its **50** most recently used threads (their messages go
   with them). The message cut is then moved forward to the first `user` row: a stored history that starts with a tool
   result would be replayed to the provider as a `tool_result` with no `tool_use`, which every provider rejects.
4. **Redaction runs before the write.** `agentRedactText` applies the vault redactor (every stored secret value) and
   `inspector.RedactAgentSecrets` (assignments, bearer tokens, private key blocks) to the message text, the tool
   calls JSON and every tool result — so a token the agent read from a `.env` is stored as `[REDACTED]`. A
   `{{ vault.KEY }}` placeholder is split out first and kept **verbatim**: a blind secret is a reference, and masking
   it would destroy the operator's template.
5. **A tool result is clipped to 8 KB for storage.** `truncateToolResult` already caps what the model sees at 24 KB;
   the stored row keeps 8 KB with an announced cut, so one `docker logs` dump cannot grow the file. A user message is
   never clipped.
6. **The in-memory conversation mirrors the thread.** `chatNative` resolves the thread (named id → the session's newest
   → a new one), reads its history once into the conversation, and each turn appends what it added. A compaction
   replaces the stored rows instead, because the dropped messages must not return on the next resume. If the database
   refuses, the chat still runs without history.
7. **The history is not part of a System Snapshot.** Like `auth_sessions` and `exposure_scans`, it is local and
   re-creatable, and `ExportSnapshot`/`ImportSnapshot` never listed it. A restored snapshot therefore leaves the
   receiving instance's own chats alone.
8. **A light CRUD API**: `GET/POST/DELETE /api/agent/threads` (list by scope or by session, create, clear a scope or
   all) and `GET/DELETE /api/agent/threads/{id}` (messages, drop one). `POST /api/agent/chat` accepts `thread_id`,
   `host_id` and `cluster_id` and answers the resolved thread in `X-Agent-Thread`; the values are optional, so an API
   caller that sends none keeps the old behaviour.

## Alternatives

1. **Keep memory only and replay from the browser.** The panel could send its transcript back on every turn, but then
   the model's history is whatever the client claims, and the tool results (truncated for the model, not for the
   browser) would be re-sent in full.
2. **One flat `agent_messages` table with a session column.** Simplest, but no title, no resource grouping, and a list
   of "conversations about this host" would not exist.
3. **Store the history in the System Snapshot.** Tempting for portability, but a chat transcript is not configuration,
   it carries the model's summaries of secret-adjacent output, and a snapshot restore would overwrite the receiving
   instance's conversations.
4. **No pruning, just a size warning.** Rejected: a log-heavy chat writes megabytes, and SQLite does not shrink on its
   own.

## Consequences

- A conversation now survives a reload and a restart: the panel hydrates from `GET /api/agent/threads?session=`.
- A snapshot no longer round-trips chat history, which is a **deliberate behaviour change** from "everything the binary
  stores is in the snapshot".
- Redaction is applied at the storage boundary as a second layer: the tool output the model sees was already redacted
  by `CompressAgentOutput`, and a value the model *wrote* (in an assistant message) is redacted now too.
- The pruning bounds are constants (`store.AgentThreadMax`, `store.AgentMessagesPerThread`), not settings: an operator
  who wants a longer history changes one line, and the 2 h idle eviction in memory stays as it was.
- `agent_approval` is recorded as `denied` for a tool row whose result says the operator refused. An approved call is
  not marked separately: it ran, and its own result is the record.
