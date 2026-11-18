## Agent skills

### Issue tracker

Issues are tracked as local markdown files under `.scratch/`. See `docs/agents/issue-tracker.md`.

### Triage labels

Default five canonical roles (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context layout (`CONTEXT.md` + `docs/adr/` at the repo root). See `docs/agents/domain.md`.

### Testing

UI and end-to-end tests use e2e (https://github.com/tester-army/e2e); tests live in `e2e/`. Read the `e2e` skill (`skill://e2e`) before writing or running one. Run with `cd e2e && npm test`. See `docs/agents/e2e.md` for details. Telemetry stays disabled (`E2E_TELEMETRY_DISABLED=1`).

### Code navigation

Always use codebase-memory-mcp first for code exploration: read the `codebase-memory` skill (`skill://codebase-memory`), then `search_graph` for symbols, `trace_path` for callers/callees, `get_code_snippet` for source, `query_graph` for multi-hop, `get_architecture` for overview, and `detect_changes` for impact analysis. Fall back to `grep`/`read` only for literals or when `check_index_coverage` shows a gap. Re-index with `index_repository` if the project is not indexed or stale.

