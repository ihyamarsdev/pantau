# Ansible UI/UX redesign (whole section)

Decisions (user): scope = whole Ansible section; layout = dense table + side drawer.
Backend features of ADR 0049-0058 that have NO UI today and must be exposed: Play Recap, Task Tree, Diff viewer,
ANSI log, Survey (launch form + editor), Host Explorer + Ping + Facts, Ad-hoc runner, Git browse/edit/commit, `--limit`, verbosity 0..4.

## Information architecture
Section `#sectionAnsible` (mount only; everything inside is rendered by `static/app/ansible/*.js`).
Header: title, gate status button, Refresh. Slim gate banner above tabs when Ansible Gate is off / binary missing.
Tabs (ids; panel id `ansiblePanel<Id>`, tab button id `tabAnsible<Id>`, container `#ansibleTabs`, role=tablist/tab/tabpanel):

| id | label | content |
|---|---|---|
| playbooks | Playbooks | toolbar + dense table; row click opens the playbook drawer |
| runs | Runs | global run feed (filters) + sub-view "Scheduled" (all schedules) |
| hosts | Hosts | Explorer (Group > Host, Pantau badge, Ansible reachability, Ping, Facts) + Ad-hoc |
| resources | Resources | segmented: Templates / Files / Library |
| settings | Settings | Ansible Gate card + ansible.cfg editor |

## Playbooks tab
Toolbar: search (`/` focuses), group select, status chips (All, Failed, Never run, Scheduled, Git), group-by toggle, count, Import Folder, New Playbook (primary).
Table columns: status dot | Name (+description, badges: git, survey) | Group | Last run (relative time + duration + N targets) | Result (recap ok/changed/failed pips) | Schedule (next run) | Run button + overflow menu (Edit, History, Delete).
Sortable (name, group, last run), sort + group-by + collapsed groups persisted in localStorage. Row = keyboard focusable, Enter opens drawer, `r` launches.
Empty state, loading skeleton, error state with retry.

## Side drawer (new component, see CONTEXT.md "Side Drawer")
Right-anchored, width min(720px, 92vw), wide mode min(1180px, 96vw), backdrop, Esc closes, focus trap, returns focus. Full-screen < 900px.
Playbook drawer: header (name, status badge, Run split-button), tabs **Overview | Edit | Runs | Schedule**.
- Overview: Launch panel (targets picker: group chips + searchable host list, max 200; mode segmented Run/Check/Diff/Syntax; Advanced collapsible: verbosity 0-4, limit, tags, skip tags, extra vars JSON, become; Survey form when playbook has survey; plain-language summary line; Launch). Last launch settings remembered per playbook (localStorage). Below: last run card (status, duration, per-host recap table), 10-run pip timeline, schedule summary.
- Edit: metadata (name, description, group), source (Inline | Git), YAML editor (CodeMirror), sub-panels Variables (analyzer), Survey (editor), Files (git tree view/edit/commit), dirty tracking, Save/Discard bar, unsaved-changes guard on close.
- Runs: history table; click pushes the Run viewer view onto the drawer (Back button).
- Schedule: this playbook's schedules, inline add/edit form (cron presets, targets, options), enable toggle.
Run viewer (drawer view, also opened from Runs tab): header badges (status, mode, duration, exit code), Cancel, tabs **Log | Recap | Tasks | Diff**, live follow (poll 1 s), ANSI colors (HTML-escape first, never innerHTML raw log), search in log, copy, download, autoscroll lock.

## Visual rules
Reuse tokens in index.html `:root` (--card-bg, --surface-2/3, --card-border, --primary, --accent-*, --success/--warning/--danger/--info and -soft/-border, --radius*, --font-*, --fs-*). Status colour = state only. Icons: `Ans.icon(name)` -> `<svg class="icon"><use href="#i-name"/></svg>` (symbols already in index.html; do not add new symbols; if one is missing use an existing close name or text). All classes prefixed `ans-`. Respect `prefers-reduced-motion`. No inline `onclick="..."` strings: attach listeners with data-attributes + event delegation per component root. User text MUST go through `Ans.esc`.

## Runtime architecture
Files (all embedded, served as ONE bundle each by the server: `/app/ansible.js`, `/app/ansible.css`, concatenated in lexical order of filename, `00-core.js` first):
```
static/app/ansible/00-core.js        namespace Ans, api, state, events, fmt, i18n, tabs registry, shell, drawer, gate, refresh
static/app/ansible/10-playbooks.js   Playbooks tab (dense table) + playbook drawer shell (Overview/Edit/Runs/Schedule tabs) + import-folder dialog
static/app/ansible/15-launch.js      Ans.launch: launch panel + survey form
static/app/ansible/20-editor.js      Ans.editor
static/app/ansible/30-runs.js        Runs tab + Run viewer + history  (Ans.runs)
static/app/ansible/40-schedules.js   Ans.schedules
static/app/ansible/50-hosts.js       Hosts tab (explorer, ping, facts, ad-hoc)
static/app/ansible/60-resources.js   Resources tab + Settings tab
static/app/ansible/00-core.css  10-playbooks.css  15-launch.css  20-editor.css  30-runs.css  40-schedules.css  50-hosts.css  60-resources.css
```
Each module file is an IIFE, owns its i18n (`Ans.addI18n({en:{...}, id:{...}})`, keys prefixed `ans_`, BOTH languages mandatory), owns its CSS, never edits another module's file. Cross-module calls only through the contract below and MUST be guarded (`Ans.runs && Ans.runs.openViewer(...)`), because modules load independently.

### Contract (core 00-core.js provides)
```
Ans.t(key, fallback, vars)          i18n via global t(); {name} interpolation
Ans.addI18n({en, id})               merge into global I18N
Ans.esc(s) / Ans.icon(name, cls)    escapeHtml / svg icon
Ans.api.get|post|put|del(path, body, opts) -> parsed JSON; throws Ans.ApiError{status,message,data}; opts.raw returns Response
Ans.toast(msg, type) / Ans.confirm({title,message,confirmText,danger}) -> Promise<bool>  (wrap showToast/showConfirm)
Ans.on(evt, fn) / Ans.off / Ans.emit  events: status, playbooks, hosts, schedules, runs, lang
Ans.state = {status, playbooks, hosts, schedules, tab}
Ans.load.status()|playbooks()|hosts()|schedules(playbookId?) -> Promise (updates Ans.state, emits)
Ans.fmt.dur(ms) .ago(iso) .dt(iso) .bytes(n) .pips(recap)
Ans.ui.badge(status) -> html (queued|running|success|failed|cancelled|interrupted|never_run)   Ans.ui.dot(status)  Ans.ui.error(msg, retryAttr)
Ans.hostPicker.mount(container, {ids, max=200, onChange(ids)}) -> {getIds, setIds, destroy}  (search + group chips + select all; reads Ans.state.hosts)
Ans.store.get(key, default) / set(key, value)   localStorage namespace pantau_ans_*
Ans.ui.empty({icon,title,desc,actionHtml}) .ui.skeleton(rows)
Ans.registerTab({id, icon, labelKey, order, render(panel), onShow(panel), onHide()})
Ans.every(fn, ms) -> stop()         interval that only ticks while the section is visible
Ans.drawer.open(view) / push(view) / back() / close() / current()
   view = { key, title, subtitle, wide, actions(html|el), mount(body, ctrl), unmount(), canClose() -> bool|Promise<bool> }
   ctrl = { setTitle, setSubtitle, setActions, setWide, push, back, close }
Ans.refresh()                        reload status + active tab (global refreshAnsibleSection() delegates here)
```
### Contract (modules provide)
```
Ans.playbooks.open(id, subtab?)  openNew()  reload()                 [10-playbooks]
Ans.launch.mount(container, playbook, {onLaunched(run)}) -> {destroy}  [15-launch]  (launch form + survey form; remembers last settings per playbook)
Ans.editor.mount(container, {playbook|null, onSaved(pb), onDirty(bool), onClose()}) -> {destroy, isDirty, save}   [20-editor]
Ans.runs.openViewer(runId, {playbookId?})  mountHistory(container, {playbookId, limit}) -> {destroy}   [30-runs]
Ans.schedules.mount(container, {playbookId}) -> {destroy}   mountAll(container) -> {destroy}   [40-schedules]
Ans.hosts.openFacts(hostId)  openAdhoc({hostIds?})            [50-hosts]
```

## Backend additions (internal/store + internal/web)
`GET /api/ansible/playbooks` items gain: `last_run_id`, `last_duration_ms`, `last_targets` (int), `last_recap` ({ok,changed,unreachable,failed,skipped} summed over hosts, absent if no recap), `schedule_count`, `next_run_at`. `GET /api/ansible/runs` accepts optional `playbook_id` and `status` filters besides `limit`.

## Tests
`internal/web/ansible_ui_test.go` covers the new contract (mount point, bundle served + gzip + ETag/304, tab contract, i18n key parity EN/ID for every `ans_` key, no inline `onclick=` handlers, `node --check` syntax of the bundle). `ansible_ui_logs_test.go` runs the pure helpers (ANSI, task tree, diff, cron preview, `vars:` removal) under node and skips when node is missing. A headless-browser pass was done by hand.

## Non-goals
No backend API redesign beyond the additions above. No new vendor libraries. No change to the Ansible Gate semantics or security model.
