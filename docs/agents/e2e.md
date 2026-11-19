# E2E Tests (e2e)

Pantau's UI is tested end-to-end with [e2e](https://e2e.com) (`e2e` + `@e2e-dev/web`), running the real app in headless Chromium.

## Layout

```
e2e/
├── e2e.config.ts        # web engine, starts the app itself
├── scripts/start-app.sh # go build + throwaway temp DB, port 18199
├── tests/
│   ├── helpers.ts       # shared login + first-run modal handling, host cleanup, unique host names
│   ├── smoke.e2e.ts     # app opens, starts fresh, login, dashboard
│   ├── ansible.e2e.ts   # gate blocked state, enable gate, create playbook, invalid YAML
│   ├── kube-workspaces.e2e.ts # Kubernetes: saved workspace cards, open/back, reload (kube API stubbed with browser.route)
│   ├── domain-shortcuts.e2e.ts # Domain Shortcuts: star in the fleet dialog, dashboard tile, reload, remove (fleet domains API stubbed with browser.route)
│   ├── shortcuts.e2e.ts # Shortcuts section: add, refused address, reload, edit, cross-origin favicon, delete (real /api/shortcuts)
│   ├── terraform.e2e.ts # Terraform drawer: gate, workspace, runs, tabs
│   ├── terraform-edit.e2e.ts # Configuration/Settings tabs: in-place editing, unsaved-changes guard
│   ├── repos.e2e.ts # Repositories section: clone validation + folder setting (real server), editor save/commit/push (repos API stubbed with browser.route)
│   ├── host-groups.e2e.ts # Host group rename from the dashboard header
│   ├── hosts-import.e2e.ts # Import hosts dialog sends the chosen SSH key (key library and import API stubbed with browser.route)
│   ├── private-ip.e2e.ts # Private IP shown on the host, Use private IP sends the inter-host probe down the private path (host APIs stubbed)
│   ├── docker-security.e2e.ts # Docker Security: summary strip, grouped findings, image table, chunked CVE detail (security API stubbed with browser.route)
│   ├── agent.e2e.ts    # AI Agent: Settings panel, chat tool line, exec_command approval/deny, session grant, read tools, memory note; multi-step troubleshooting checks domain-filtered tools, Activity Console telemetry and the approval gate (LLM stubbed with a fake OpenAI-compatible server)
│   ├── gateway-update-check.e2e.ts # Gateway CLI Update Check + Update: installed/latest version, failure state, the install streaming its steps
│   ├── network-globe.e2e.ts # Network Radar Globe: embedded country data, land drawn on the WebGL canvas, hover names a country, link filter changes the drawn arcs, host HUD (globe API stubbed with browser.route)
│   └── terraform-helpers.ts  # shared Terraform workspace/runner helpers
└── package.json
```

## Deferred & Targeted Testing Policy

Binding for every session.

1. **No mid-iteration runs.** Do not run `go test`, `go vet`, `go build`, or any E2E test while code is still being written. Never test after each freshly edited file.
2. **Finish, then verify once.** Complete the whole implementation and write all the tests first. Verify a single time, at the very end.
3. **Targeted verification only.** At the final check run:
   - `go test ./internal/<package>` for each changed package only;
   - one related E2E spec: `cd e2e && npx e2e run tests/<feature>.e2e.ts`.

   Do not run the full Go suite (`go test ./...`) or the full E2E suite (`npm test`) unless the user explicitly asks.

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
- **The whole run shares one database.** `scripts/start-app.sh` starts Pantau on a throwaway temp DB, and that DB lives for the whole run: a host one test adds is still there for the next test. A test that adds hosts must remove them again — `deleteHosts(browser, ['name-a', 'name-b'])` in `tests/helpers.ts` does that, in a `finally` or in an `afterEach` (an `afterEach` runs after a failure too, so it covers a failing test). Hosts are removed by the name the test gave them, so a test never touches a host another test is using.
- **A test that picks a host by name needs a name no other test uses.** Use `uniqueHostName('alpha')`; a fixed name collides with a host another test left behind, and the lookup then matches two rows.
- **A host added through the API is not in the page yet.** The page keeps its own host list, refreshed only by `loadHosts()`. Call it (`await browser.evaluate(async () => { (window as unknown as { loadHosts(): Promise<void> }).loadHosts(); return null; })`) after adding a host, or a dialog that lists the hosts shows the old list.
- Prefer stable element ids (`#ansiblePlaybookName`, `[data-sub="tasks"]`, …) over text that changes with the UI language.

## Failures

Read `.e2e/results/<test>/trace.md` (kept for failures): per-step screen snapshots, the app log, and a final screen dump. `--action-timeout 5000` speeds up iteration on a single test.

## CI

Not wired up yet.
