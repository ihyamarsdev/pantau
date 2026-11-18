## Agent skills

### Issue tracker

Issues are tracked as local markdown files under `.scratch/`. See `docs/agents/issue-tracker.md`.

### Triage labels

Default five canonical roles (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context layout (`CONTEXT.md` + `docs/adr/` at the repo root). See `docs/agents/domain.md`.

### Testing

UI and end-to-end tests use e2e (https://github.com/tester-army/e2e); tests live in `e2e/`. Read the `e2e` skill (`skill://e2e`) before writing or running one. Run with `cd e2e && npm test`. See `docs/agents/e2e.md` for details. Telemetry stays disabled (`E2E_TELEMETRY_DISABLED=1`).

### Deferred & Targeted Testing Policy

Applies to every working session.

- **No mid-iteration checks.** While writing code, do NOT run `go test`, `go vet`, `go build`, or any E2E test (`npm test`, `npx e2e run`). Do not verify file by file after each edit.
- **Finish first, verify once.** Complete 100% of the implementation and write all the tests it needs before running anything. Verification happens one time, at the very end.
- **Targeted final verification only.** Run `go test ./internal/<package>` for each changed package only, plus the single related E2E spec `cd e2e && npx e2e run tests/<feature>.e2e.ts`. Do NOT run the full Go or E2E suite unless the user explicitly asks for it.
- Details and commands: `docs/agents/e2e.md`.

### Code navigation

Always use codebase-memory-mcp first for code exploration: read the `codebase-memory` skill (`skill://codebase-memory`), then `search_graph` for symbols, `trace_path` for callers/callees, `get_code_snippet` for source, `query_graph` for multi-hop, `get_architecture` for overview, and `detect_changes` for impact analysis. Fall back to `grep`/`read` only for literals or when `check_index_coverage` shows a gap. Re-index with `index_repository` if the project is not indexed or stale.

