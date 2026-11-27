# Agent procedural memory, milestone 3 (H3)

## What changed

- **Store** (`internal/store/agent_knowledge.go`). Tables `agent_runbooks` (runbook or blueprint, domain, trigger, steps JSON, tags, usage count) and `agent_custom_skills` (name, domain, enabled, content). One FTS5 table `agent_knowledge_fts` (`unicode61`) indexes runbooks and the existing `agent_memory` notes. The index is written in the same call as each change: memory add, update and delete, and runbook create and delete.
- **Query safety.** `FTSMatchQuery` splits input into word tokens and quotes each one, joined with OR. Operators, quotes, dashes and colons in the input are plain text. Tested with `gcp-dev`, `"quoted`, `foo:bar`, `OR NOT`, `NEAR(a b)`, `*`.
- **Ranking and domains.** `bm25` order. A domain filter keeps that domain plus the global entries.
- **Snapshot.** `SnapshotPayload` gains `agent_runbooks` and `agent_custom_skills`; both are in the import wipe list. The index is rebuilt after an import.
- **Prompt retrieval.** Each turn adds up to 4 matching runbooks and notes, capped at 1500 bytes. A used runbook counts one use. Enabled custom skills for the active domain (and global) follow, capped at 3000 bytes. Both pass `agentRedactText`.
- **`search_knowledge`** (read-only tool, core domain). Searches runbooks, blueprints and notes, with an optional domain.
- **Distill.** `POST /api/agent/threads/{id}/distill` sends the thread transcript (capped at 9000 bytes) to the fast model, with the primary model as fallback. The answer must contain a JSON object; it is parsed, cleaned and redacted, and returned as a draft. Nothing is saved. A non-JSON answer returns 502.
- **Save.** `POST /api/agent/knowledge` saves a reviewed draft (validated by the store). `GET /api/agent/knowledge?q=&domain=` searches or lists. `DELETE /api/agent/knowledge/runbooks/{id}` deletes one runbook.
- **Custom skills CRUD.** `GET|POST /api/agent/skills/custom`, `PUT|DELETE /api/agent/skills/custom/{id}`.
- **UI.** A "Save as Runbook / Blueprint" button in the chat header opens a review dialog with every draft field editable. Settings → AI Agent gains a "Knowledge & Skills" card: a search box, the runbook list with delete, the skill list with enable toggles and delete, and an add form. EN and ID strings added. `node --check` passes.

## Decisions

- **Redaction happens at write time, not in the snapshot export.** `inspector` imports `store`, so `store` cannot call `RedactAgentSecrets`. Every write path (distill draft, save, custom skill) redacts through `agentRedactText` in the web layer. Rows written any other way would bypass this.
- **The index covers `agent_memory`.** The spec names `agent_memory_notes`; the real table is `agent_memory`, and that is the one indexed.
- **Knowledge & Skills is a card in the AI Agent settings, not a separate sub-tab.** The settings have no sub-tab system to extend.
- **Plan steps stay in memory**, as in milestone 2. Runbooks and skills persist.

## Verification

- `go vet ./...` clean. `go test -race -count=1 ./...`: all packages pass. `make sec`: exit 0, no vulnerabilities, gosec 0 issues.
- Store: FTS match table; hostile input never errors; ranking and domain filter; index follows memory update and delete and runbook delete; validation table; skill CRUD; snapshot round trip with the index rebuilt on import.
- Web: retrieval injects and counts usage, and filters by domain; search tool; skills injected per domain and disabled ones excluded; distill redacts a bearer token and keeps `{{ vault.KEY }}`, and saves nothing; a non-JSON answer is a 502; save and search over HTTP; skill CRUD over HTTP.
- E2E: new `e2e/tests/agent-knowledge.e2e.ts` passes (search finds a saved runbook; a skill is added and toggled off; API cleanup). The existing agent suites pass: `agent.e2e.ts` 14 tests and `cross-domain-agent.e2e.ts` 1 test.

## Not done / caveats

- No E2E for the plan card or the distill button. The spec asked for both. Distill is covered at the API level only.
- Redaction at write time depends on every writer going through the web layer. A future writer in `store` would need the same care.
- Nothing has been checked against a real LLM's distill output. The fake model returns the shape the parser expects.
