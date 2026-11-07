## Agent skills

### Issue tracker

Issues are tracked as local markdown files under `.scratch/`. See `docs/agents/issue-tracker.md`.

### Triage labels

Default five canonical roles (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context layout (`CONTEXT.md` + `docs/adr/` at the repo root). See `docs/agents/domain.md`.

### Testing

UI and end-to-end tests use e2e (https://github.com/tester-army/e2e); tests live in `e2e/`. Read the `e2e` skill (`skill://e2e`) before writing or running one. Run with `cd e2e && npm test`. See `docs/agents/e2e.md` for details. Telemetry stays disabled (`E2E_TELEMETRY_DISABLED=1`).
