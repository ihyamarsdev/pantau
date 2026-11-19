## Agent skills

### Issue tracker

Issues are tracked as local markdown files under `.scratch/`. See `docs/agents/issue-tracker.md`.

### Triage labels

Default five canonical roles (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs (token-efficient)

Single-context layout. `CONTEXT.md` is a **map** (<6 KB) with a lookup table. Read **only** the targeted
`docs/agents/context/*.md` slice for your task, plus the relevant ADR(s) from `docs/adr/` (see
`docs/agents/context/adr-index.md`). Do **not** read all of `CONTEXT.md`'s former content in one go, and do
**not** read `docs/user-guide*.md` (~200 KB) unless the task is about end-user documentation. Use the glossary
vocabulary from the slice; flag ADR conflicts explicitly. See `docs/agents/domain.md`.

### Testing

UI and end-to-end tests use e2e (https://github.com/tester-army/e2e); tests live in `e2e/`. Read the `e2e` skill (`skill://e2e`) before writing or running one. Run with `cd e2e && npm test`. See `docs/agents/e2e.md` for details. Telemetry stays disabled (`E2E_TELEMETRY_DISABLED=1`).

### Deferred & Targeted Testing Policy

Applies to every working session. Single-pass execution: finish all edits, then verify once.

- **No mid-iteration checks.** While writing code, do NOT run `go test`, `go vet`, `go build`, or any E2E test
  (`npm test`, `npx e2e run`, `e2e-inc/e2e`). Do not verify file by file after each edit. `gofmt` for syntax is fine.
- **Finish first, verify once.** Complete 100% of the implementation (backend, migrations, UI hooks, docs, tests)
  before running anything. Verification happens one time, at the very end.
- **Targeted final verification only.** Run `go test ./internal/<package>` for each changed package only, plus the
  single related E2E spec `cd e2e && npx e2e run tests/<feature>.e2e.ts`. Do NOT run the full Go or E2E suite unless
  the user explicitly asks for it. Telemetry stays disabled (`E2E_TELEMETRY_DISABLED=1`).
- Details and commands: `docs/agents/e2e.md`.

### Code navigation

Always use codebase-memory-mcp first for code exploration: read the `codebase-memory` skill (`skill://codebase-memory`), then `search_graph` for symbols, `trace_path` for callers/callees, `get_code_snippet` for source, `query_graph` for multi-hop, `get_architecture` for overview, and `detect_changes` for impact analysis. Fall back to `grep`/`read` only for literals or when `check_index_coverage` shows a gap. Re-index with `index_repository` if the project is not indexed or stale.

### Go profiling (pprof)

Four composable skills from `rumstead/golang-pprof-skills` are installed project-wide under `.agents/skills/` (also symlinked into `.claude/skills/`); versions are pinned in `skills-lock.json`. Use them for performance work on the Pantau binary.

- `skill://golang-pprof-collect` — collect a profile from a running process (or a Kubernetes pod via port-forward) into a `.pb.gz` file. Start here when no profile file exists yet.
- `skill://golang-profile-cpu` — analyze CPU profiles: hot functions, call graphs, contention indicators.
- `skill://golang-profile-heap` — analyze heap profiles: memory consumers, allocation paths, GC pressure.
- `skill://golang-profile-goroutine` — analyze goroutine profiles: leaks, blocking stacks, state breakdown.

The three analysis skills depend on `golang-pprof-collect` and delegate collection to it. Read the relevant `SKILL.md` before running `go tool pprof`. `// turbo` annotations mark steps safe to auto-run. Profiles are written to the workspace root — keep them out of git (`.gitignore`).

