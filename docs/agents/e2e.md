# E2E Tests (e2e)

Pantau's UI is tested end-to-end with [e2e](https://e2e.com) (`e2e` + `@e2e-dev/web`), running the real app in headless Chromium.

## Layout

```
e2e/
├── e2e.config.ts        # web engine, starts the app itself
├── scripts/start-app.sh # go build + throwaway temp DB, port 18199
├── tests/
│   ├── helpers.ts       # shared login + first-run modal handling
│   ├── smoke.e2e.ts     # app opens, starts fresh, login, dashboard
│   └── ansible.e2e.ts   # gate blocked state, enable gate, create playbook, invalid YAML
└── package.json
```

## Run

```bash
cd e2e && npm test          # = e2e run (all tests)
npx e2e run tests/smoke.e2e.ts:5   # one test, from the failing line in the report
npx e2e run --headed        # watch the browser
E2E_TELEMETRY_DISABLED=1 npx e2e doctor   # environment check
```

The runner builds the binary and starts Pantau itself (`app.command` → `scripts/start-app.sh`): fresh temp DB, port 18199, `-q -disable-update-check`. No server to start by hand.

## Conventions

- Deterministic locators only (`screen.getByRole/getByText`, element ids as fallback). No AI model is configured; `agent.*` steps would need one (see e2e docs) — don't add them.
- One atomic check per line, no sleeps. Auto-waiting locators + `expect` handle timing.
- `tests/helpers.ts` `initApp()` runs at the start of every test: log in (`admin` — the fresh-DB default) and dismiss the first-run "Restore Pantau from Backup" modal via Start Fresh. Each test gets a fresh page, so both may reappear.
- Prefer stable element ids (`#ansiblePlaybookName`, `[data-sub="tasks"]`, …) over text that changes with the UI language.

## Failures

Read `.e2e/results/<test>/trace.md` (kept for failures): per-step screen snapshots, the app log, and a final screen dump. `--action-timeout 5000` speeds up iteration on a single test.

## CI

Not wired up yet.
