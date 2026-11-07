# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]
### Added
- **Host firewall management** ([ADR-0062](docs/adr/0062-host-firewall-management.md)): the Security tab has a **Firewall** card that reads, sets up, adds, edits and deletes rules of **ufw**, **firewalld** and **iptables** on a host (`GET /api/hosts/{id}/security/firewall`, `POST /api/hosts/{id}/security/firewall/action` with `setup | enable | disable | add | edit | delete | persist | add-zone`). Loaded on demand; root or passwordless sudo; SSH-port lock-out guard (HTTP 403); edit/delete verify the rule is unchanged on the host first; Linux only. firewalld Add/Edit rule provides structured form fields for services, ports and rich rules (with a raw-rule fallback for unsupported options) and supports creating new firewalld zones via `add-zone` or directly from the rule dialog.
- **Terraform Registry**: a new top-level Terraform **Registry** tab browses and searches public modules (versions, inputs, outputs, README as plain text) and providers (`/api/terraform/registry/modules`, `/providers`; provider search uses the registry's undocumented v2 endpoint). The Configuration tab's **Registry** button inserts a snippet at the cursor or adds `module-<name>.tf` / `provider-<type>.tf` (optionally appending required-input variables to `variables.tf`) with a `~> MAJOR.MINOR` constraint. **Check versions** (`GET /api/terraform/workspaces/{id}/registry/check`, inline workspaces, saved files) reports `up_to_date` / `outdated` / `newer_outside_constraint` / `unpinned` / `unknown`, and **Apply** rewrites only the version literal in the editor. The Gate tab **Registry** card sets the base URL (`tf_registry_url`; https only except loopback http, no credentials) for mirrors and air-gapped proxies. Fetched server-side (15 s timeout, 4 MiB cap, 5-minute cache, same-host redirects only); registry text rendered escaped. Public registry protocol only; Git workspaces are not checked.
- **Terraform Cloud Accounts: Cloudflare and Kubernetes providers**. Cloudflare takes `api_token` and an optional `account_id` (`CLOUDFLARE_API_TOKEN` / `CLOUDFLARE_ACCOUNT_ID`; verified with the Cloudflare token verify API). Kubernetes takes a kubeconfig written as a `0600` temp file per run (`KUBE_CONFIG_PATH` + `KUBECONFIG`, optional context as `KUBE_CTX`), removed after the run and redacted from logs; verification only parses the kubeconfig (no network).
- **Terraform Cloud Accounts: import from existing Pantau accounts**: a new account can be created from an existing Pantau Cloudflare account or Kubernetes cluster. Credentials are **copied at import time** (not a live link). `GET /api/terraform/cloud-accounts/importable` returns names only; `import_from: {type, id}` on `POST /api/terraform/cloud-accounts`.
- **Terraform AWS assume role**: optional `role_arn`, `external_id` (secret) and `session_name` (default `pantau`) on AWS Cloud Accounts. At the start of each run Pantau calls STS `AssumeRole` (1 h) with the base keys and injects the temporary credentials; a failure fails the run with a clear error. Test connection also assumes the role.
- **Terraform Variables tab: Detect from configuration**: parses the `variable` blocks of inline workspaces (`GET /api/terraform/workspaces/{id}/variables/detect`) and lists them (defaults unchecked, required checked, existing disabled); the chosen ones are added as `terraform` variables. Git workspaces are not supported; the scanner is not a full HCL parser and reads only `.tf` files.
- **Terraform Configuration tab**: HCL syntax highlighting, **Format** (`terraform fmt` / `tofu fmt`, `Ctrl+Shift+F`) and **Validate** (`init -backend=false` + `validate`, up to 2 min, 2 concurrent; offline only works with providers already cached or built in) with a `file:line` diagnostics panel. Endpoints `POST /api/terraform/workspaces/{id}/tools/fmt` and `/tools/validate`; inline workspaces only, using the current unsaved editor content.
- **Terraform notifications**: the Terraform Gate tab has a **Notifications** card that selects which events are sent (awaiting approval, errored, applied, drift; defaults: all but applied) through the channels already configured in Pantau Settings (Telegram, webhook, ntfy, Slack, Discord, SMTP). `GET/PUT /api/terraform/notifications` and `POST /api/terraform/notifications/test`. Drift notifies only on the absent → present transition.
- **Terraform scheduled plans and Auto-apply**: Workspace Settings gained **Scheduled plan interval (minutes)** (`plan_interval_minutes`, creates `schedule` plan runs) and **Auto-apply** (`auto_apply`): a plan that has changes and policy status `passed` is applied with the comment `auto-apply`. Only `schedule` and `manual` plans are eligible; webhook-triggered runs are never auto-applied. Never for production environments (HTTP 400 at save, ignored at runtime); destroy, refresh and policy-warning runs still need manual approval.
- **Terraform workspace templates**: the New Workspace modal has a template picker (Hello world, AWS S3 bucket, AWS EC2 instance, GCP storage bucket, Azure resource group, DigitalOcean droplet, Hetzner server, Cloudflare DNS record, Kubernetes namespace); `GET /api/terraform/templates`. Provider-specific templates need the matching Cloud Account linked; some have required variables without default (`bucket_name`, `zone_id`, `name`, `content`); the provider download needs network on the first run.
- **Terraform linked outputs** (Variables tab): feed an output of another workspace's latest state into a Terraform variable (`GET/PUT /api/terraform/workspaces/{id}/output-links`). The value is HCL JSON, sensitive outputs stay masked, an explicit workspace/varset variable with the same name wins, cycles are rejected, and an unavailable output fails the run start with a clear message. Included in backups.
- **Terraform state operations** (State tab): remove (`state rm`), move (`state mv`) and import a resource (`POST /api/terraform/workspaces/{id}/state/ops`). Each creates a **new** state version (the old one is kept; source recorded e.g. `state rm x`); the workspace is locked during the operation (423 if already locked, 409 with an active run); a confirmation dialog shows the exact command. Import runs `init` and uses the linked Cloud Account credentials.
- **Terraform Audit tab**: persistent trail of Terraform events (table `tf_audit`, newest 5000 kept, written from the log stream; `GET /api/terraform/audit`). No RBAC (Pantau has a single admin password); a burst of more than ~64 lines within one write window may drop lines.

### Fixed
- **Security scan firewall check on non-root hosts**: `firewall_active` read `ufw status` / `iptables -S` as the plain SSH user, so hosts where the user has passwordless sudo still showed `unknown` and a `partial` scan. The firewall section now uses `sudo -n` (same 24 h refusal backoff as the other sudo probes) when the user is not root.
- **Terraform UI**: workspace tab strips scroll horizontally on narrow screens, and the state action buttons are visible in each resource row instead of being clipped.
- Docker: Stack actions (up, pull, update, recreate, …), Stack deploy and Image pull now **stream their output live** into the log viewer (final `[pantau] exit=N` marker decides success/failure) instead of showing it only after the command finishes.
- **Ansible Variables tab**: variables that a role provides itself were listed as **Required**. The analyzer now also reads roles installed in the Library (`roles/` and Library collections) and the system role paths: role `defaults/`/`vars/` count as defined (`ansistrano_repo_dir`), facts a role sets (`set_fact`/`register`, e.g. `ansistrano_release_path`) are not inputs, including those set in task files reached by a templated `include_tasks`.
- **Top Resource Consumers (Process Telemetry)**: Linux Top CPU now measures current usage over a ~1 s window per inspection (% of one core, up to >100% for multi-threaded processes) instead of the lifetime average of `ps %cpu`, Top Swap resolves the username from `/etc/passwd` (numeric UID fallback only if unresolvable), and hosts whose `ps` lacks `--sort` (Alpine/BusyBox) are sorted by Pantau itself to reliably return the real top 5 processes.
- **Ansible over SSH in Docker**: runs failed with `Bad owner or permissions on /root/.ssh/config` when the host's `~/.ssh/config` was bind-mounted (OpenSSH reads the passwd-home config and ignores `$HOME`). The generated `ansible.cfg` now runs ssh with `-F /dev/null -o UserKnownHostsFile=/dev/null`.
- **Security**: the Host API (`GET /api/hosts`, `GET /api/hosts/{id}`, host create/update/inspect responses) no longer returns the stored per-host private key (`custom_key`); it returns `has_custom_key` instead. Editing a host with a custom key and leaving the key field blank (new `keep_custom_key`) keeps the stored key. System Snapshots still contain the key.
- Server: `ReadHeaderTimeout` (10 s) and `IdleTimeout` (120 s) on the HTTP server; `DefaultBroadcaster` data race fixed.
- Kubernetes: `kubectl logs -f` is terminated when the log WebSocket closes; `get pods` output is capped at 32 MiB; multi-node failover stops after a 20 s budget; package upgrade preview honors request cancellation.
- SSH: the handshake is now bounded by the connect timeout (a host that accepts TCP but never answers no longer hangs an inspection).
- Terraform tab panels lost their column layout after the Ansible UI redesign (shared `.ans-section-panel`/`.ans-sched-tbl` rules had been removed).

### Changed
- **Terraform runs no longer pass the deprecated `-state` flag** ([ADR-0061](docs/adr/0061-terraform-state-ops-links-audit.md)): Pantau writes `pantau_backend_override.tf` (a `local` backend whose path is inside the run root; it replaces any backend declared by the user) and runs `init -reconfigure`. Do not use that file name in your own workspace files: it is overwritten.
- **Terraform "no auto-apply" limitation narrowed**: auto-apply now exists as an opt-in per workspace (see **Terraform scheduled plans and Auto-apply** under Added) for scheduled and manually started plans only; webhooks still never apply. It is still refused for production environments, and destroy, refresh and policy-warning runs always need manual approval.
- **Terraform dialogs**: the Run dialog is widened to 720 px and all modals are capped to 96vw.
- **Terraform run logs mask only secrets** ([ADR-0060](docs/adr/0060-terraform-cloud-accounts.md)): non-secret Cloud Account fields (region, access key id, tenant / subscription / client id, project id) now stay visible in run logs; secrets (secret keys, session tokens, API tokens, service-account JSON, client secret, external id, kubeconfig) and user `env` variables stay masked.
- **Terraform Runs tab**: the Start Run button is auto-width and centered, and the action rows (Approval Gate Confirm & Apply / Discard Run, Overview quick actions) are centered.
- **Performance**: inspections evaluate rules with one prior-status snapshot, one `crontab -l`, one batched `systemctl is-active` for service rules (systemd Linux), and a single transaction for actual-state writes; background inspections are capped at 5 concurrent; parsed SSH keys and settings are cached in memory; SQLite runs with `synchronous=NORMAL`; incidents/alerts are pruned to 500 per host (unacknowledged alerts are kept) and indexed; `/vendor` JS/CSS and `/api/hosts` are gzip-compressed (the latter with ETag/304); the security scan lists nftables tables instead of the full ruleset; the Ansible availability probe is cached for 60 s.
- **Ansible UI redesign** ([ADR-0059](docs/adr/0059-ansible-ui-playbook-table-and-side-drawer.md)): the whole Ansible section is rebuilt around a dense **Playbooks table** and a **side drawer**. Tabs are now Playbooks, Runs, Hosts, Resources and Settings.
  - **Playbooks table**: search (`/` focuses it), All/Failed/Never run/Scheduled/Git chips, sortable columns, group-by, last run status with recap bar, next schedule, per-row menu.
  - **Drawer**: a playbook opens with Overview, Edit, Runs and Schedule tabs; runs open the **Run Viewer** (Log with ANSI and search, Recap, Task tree, Diff, live updates, Cancel). Unsaved edits are guarded on close.
  - **Launch panel**: host picker, modes, survey form, advanced options and a summary; remembers the last settings per playbook; Ctrl+Enter starts the run.
  - **Git Editor** moved into the drawer's Edit tab (Files section) with commit and push in the footer.
  - **Runs tab** with History and Scheduled sub-views; **Hosts** tab with Explorer, Ping, Facts and Ad-hoc; **Resources** tab with Templates, Files and Library; **Settings** with the Gate and `ansible.cfg`.
  - Features that already existed in the API but had no UI are now reachable: recap, task tree, diff, survey editor and form, host explorer, ping, facts, ad-hoc, git file edit.
  - While the Ansible Gate is off (or `ansible-playbook` is missing) every tab except Settings explains it with an **Enable Gate** button instead of showing API errors.
  - The UI is split into modules under `internal/web/static/app/ansible/` and served as the bundles `/app/ansible.js` and `/app/ansible.css` (ETag, gzip) instead of growing the 1.5 MB `index.html`.
  - **Playbook parts**: inline single-play playbooks are edited as **Pre-tasks / Roles / Tasks / Post-tasks / Handlers / Raw**. Roles are picked from the roles installed in the Library, Library collections and system paths (`GET /api/ansible/roles`), with per-role parameters and ordering. Raw stays the source of truth: parts are split and joined server-side (`POST /api/ansible/playbooks/outline` and `/compose`, comments and quoting kept) and only when a part was edited; name, hosts, become, vars and other play keys are edited in Raw and carried through unchanged. Multi-play or unparsable playbooks stay Raw-only.
  - **Variables tab**: grouped into the playbook's own variables and one collapsible section per role (new `role` field of the analyzer result), with a search box; the table uses the full drawer width and drops the Source and Used-in columns by the drawer's own width, so expanding the drawer shows everything.
  - **Save**: Save keeps you where you are (same drawer tab and editor tab, same editor instance; a new playbook's first save turns the drawer into that playbook's Edit tab on the same editor tab instead of Overview). Inline playbooks are checked and auto-formatted first by the new `POST /api/ansible/playbooks/format`: the YAML must be a list of plays (each with `hosts` or `import_playbook`) without duplicate keys, and is re-indented to 2 spaces (comments and quoting kept). A failed check saves nothing, switches to Raw and shows the reason with its line number.
- **Ansible API**: playbook list entries gain `last_run_id`, `last_duration_ms`, `last_targets`, `last_recap`, `schedule_count` and `next_run_at`; `GET /api/ansible/runs` accepts `playbook_id` and `status` filters (HTTP 400 on invalid values).
- **Terraform UI**: the workspace view is a wide side drawer (Overview/Runs/State/Variables/Drift/Settings) instead of a modal: docked, expandable to full width (remembered), Esc/backdrop closes, Overview shows Overview/Outputs/Recent Runs cards side by side; workspace list toolbar and Variable Sets/Policies lists use the full width; CSS lives in `/app/terraform.css`.

### Added
- **Terraform Cloud Accounts** ([ADR-0060](docs/adr/0060-terraform-cloud-accounts.md)): new **Terraform → Cloud Accounts** tab to register provider credentials (AWS, Google Cloud, Microsoft Azure, DigitalOcean, Hetzner) once and link them to workspaces (workspace drawer → Settings → Cloud accounts; `cloud_account_ids`). Linked accounts' credentials are injected as environment variables into every run (plan, apply, drift, webhook, state operations, cost; `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY`/`AWS_SESSION_TOKEN`/`AWS_DEFAULT_REGION`+`AWS_REGION`, `GOOGLE_PROJECT`/`GOOGLE_CLOUD_PROJECT`/`GOOGLE_REGION`/`GOOGLE_CREDENTIALS`, `ARM_*`, `DIGITALOCEAN_TOKEN`, `HCLOUD_TOKEN`); workspace/Variable Set `env` variables override them and all values are redacted in run logs. **Test connection** verifies against the provider API (AWS STS `GetCallerIdentity`, GCP service-account token + project, Azure client-credentials token, DigitalOcean `/v2/account`, Hetzner `/v1/servers`). Credentials live in the Pantau DB, are never returned by the API (shown as "stored"), and are included in encrypted snapshot backups. API: `/api/terraform/cloud-accounts` (+ `/{id}`, `/{id}/test`). Workspace Overview lists the linked accounts.
- **Terraform workspace editing in place**: new Configuration tab (inline files with a CodeMirror editor, file list, add/delete, Ctrl+S, Save & plan, unsaved-changes guard; Git workspaces open the Git editor) and an editable Settings tab (name, group, environment, tags, binary, drift interval, Git connection) in the workspace drawer; modules in `/app/terraform.js`.
- **Playbook & Workspace Grouping** (Addendum to [ADR-0057](docs/adr/0057-ansible-ops-recap-tasktree-diff-survey-adhoc-facts-explorer.md) & [ADR-0056](docs/adr/0056-terraform-management-terraform-cloud-style.md)): organize Ansible playbooks and Terraform workspaces with free-text groups (`group_name`, up to 64 chars, empty = ungrouped) modeled after Host Groups:
  - **Implicit Groups & Sections**: groups are derived implicitly from distinct loaded `group_name` values, rendered as collapsible group sections in grid and list views with collapse state persistence.
  - **Filtering & Search**: filter playbooks and workspaces by group via dropdown/datalist, with quick filtering for Ungrouped items.
  - **Cosmetic & Snapshot**: cosmetic labeling only (no changes to execution, permissions, locks, or state); included in encrypted System Snapshots.
- **Git Edit, Commit & Push for Ansible and Terraform** ([ADR-0058](docs/adr/0058-git-edit-commit-push-for-ansible-and-terraform.md)): browse repository trees, view, create, edit, delete files, and commit & push changes directly from the Pantau dashboard for Git-backed Ansible playbooks and Terraform workspaces:
  - **Stateless Shallow Clone**: each tree listing, file read, and commit operation performs a transient shallow clone (`--depth 1 --single-branch --no-tags`) in an isolated temporary `0700` directory, immediately cleaned up afterward.
  - **Git Editor Modal**: unified Workspace Modal featuring file tree navigation, search, collapsible directories, CodeMirror syntax highlighting (YAML, shell, HCL/C-like, plain text), file creation, deletion, dirty state indicators, and discard changes.
  - **Optimistic Concurrency Control (`base_commit`)**: prevents overwriting upstream changes by comparing HEAD against `base_commit`; returns `409 Conflict` (`ErrRepoChanged`) with a reload prompt if the repository was updated upstream.
  - **Commit & Push Options**: supports pushing directly to the configured branch or pushing to a newly created branch (`new_branch`) with branch name validation. Push commands strictly forbid force pushing (`--force` is never used).
  - **Safety & Sanitization**: enforces path validation (no `..`, `.git`, or symlink traversals), 1 MiB per file limit, 5 MiB total change limit, max 50 changed files, UTF-8 text enforcement (binaries return 415), and safe environment flags (`-c commit.gpgsign=false -c core.hooksPath=/dev/null -c credential.helper=`). Stored credentials and tokens are always sanitized from error messages.
  - **Write Permission Requirements**: Git credentials (HTTPS access token or SSH Deploy Key) must possess write permissions on the remote repository (e.g. GitHub `Contents: Read and write` or GitLab `write_repository`). Read-only credentials return 502 with sanitized push failure details.
  - **Side Effects**: successful Ansible commits update last sync time and commit SHA; pushing to a watched Terraform branch naturally triggers speculative webhook plans if configured.
- **Ansible Ops: recap, task tree, diff, survey, ad-hoc, facts, explorer, git sync** ([ADR-0057](docs/adr/0057-ansible-ops-recap-tasktree-diff-survey-adhoc-facts-explorer.md)): the day-to-day Ansible operational layer, without breaking the non-goals of ADR 0049 (no custom/static inventory, no Vault, no become password, no dynamic inventory, no workflow or execution environments):
  - **Verbosity 0..4 and `--limit`**: runs and schedules accept `-vvvv` and an optional host pattern (`^[A-Za-z0-9_.,:!&*\[\]-]{1,256}$`) validated server-side by `ansible.ValidatePattern` and passed as a single `--limit` argument; stored in `ansible_runs.limit_pattern` / `ansible_schedules.limit_pattern`.
  - **Play Recap**: playbook runs now run with `ANSIBLE_FORCE_COLOR=1`, so their logs are ANSI-colored (`ansible-galaxy` and library installs stay plain). Raw ANSI is kept in DB/API; the UI escapes then colorizes. `ParseRecap` reads `PLAY RECAP` into per-host counts saved to `ansible_runs.recap` and returned as an object by the run API (run list carries `recap_totals`).
  - **Task Tree & Diff Viewer**: client-side views over the log (Play > Task > Host with search and status filter; per-file diff blocks for `diff` runs). No backend change.
  - **Survey**: per-playbook typed questionnaire (`text`, `textarea`, `number`, `integer`, `select`, `multiselect`, `boolean`, `password`; max 30 fields) stored in `ansible_playbooks.survey`, edited in the playbook editor, rendered in the launch dialog and validated server-side (`ValidateSurvey` / `ApplySurvey`). Precedence: survey answers override the playbook's default extra vars, explicit launch extra vars win over both. `password` answers are passed to the runner but shown as `(hidden)` in the run record and are **not** filtered from logs; unknown answer keys and missing required answers are rejected with 400 at launch.
  - **Ad-Hoc runner**: run a single module against Hosts/Host Group without a playbook, with live log, per-host result table and its own history (`ansible_adhoc_runs`, latest 100, marked `interrupted` on startup). Separate manager with **3 active ad-hoc runs globally**, independent of the playbook run quota.
  - **Ansible Facts viewer**: gather `setup` for one Host (90 s timeout, 2 MiB cap), cache summary + raw JSON in `ansible_host_facts` (cascade delete with the Host) and show OS/CPU/memory/disk/network cards plus searchable raw JSON.
  - **Host & Group Explorer**: new Ansible tab showing the Group > Host tree with Pantau inspection badges, Generated Inventory aliases, a **Ping** button (ad-hoc `ping`, Reachable/Unreachable/Failed) and a **Facts** shortcut per host.
  - **Git Sync**: `POST /api/ansible/playbooks/{id}/git/sync` shallow-clones the repo (temp dir always removed), verifies the playbook path and reports commit/branch/sorted YAML list (dot dirs and `node_modules` skipped, max 1,000), saving `last_sync_at` / `last_sync_commit`. Git sources only (400 otherwise); a clone/network/auth failure returns 502 with the repo URL masked as `<repo>`, while a cloned repo without the configured playbook path returns 400.
  - Snapshot: `survey` travels with playbooks and `limit` with schedules; ad-hoc history and host facts are excluded. Columns are added with `alterAddColumn`, so old snapshots still restore.
- **Terraform Management (Terraform Cloud-style, local CLI)** ([ADR-0056](docs/adr/0056-terraform-management-terraform-cloud-style.md)): manage Terraform and OpenTofu workspaces with a complete execution lifecycle (plan, policy check, cost estimation, mandatory approval gate, apply) via local `terraform` or `tofu` CLI on the Pantau server. Features include:
  - **Workspace & Execution Engine**: create workspaces with inline files (up to 50 files, 256 KiB each) or Git repository sources (shallow clone per run, branch, sub-working directory, token/SSH key auth). Execution runs with isolated `0700` workspaces, sanitized environment variables, and auto-generated `pantau.auto.tfvars.json` / `pantau-hcl.auto.tfvars`.
  - **Approval Gate & Mandatory Comments**: plans with changes require explicit administrative approval (`Confirm & Apply` or `Discard`) with mandatory reason comments (>=3 characters) and policy override guards before changes can be applied.
  - **State Versioning, Visual Diff & Rollback**: immutable `terraform.tfstate` versions stored in SQLite and materialized to disk before runs. Built-in v4 state parser, resource inventory, structural state diff, and non-destructive state rollback (`current.serial + 1`).
  - **Workspace Locks & Concurrency Guards**: automatic workspace locking during active runs (`locked_by = "run:<id>"`), manual administrative locking, and emergency Force Unlock with file lock cleanup. Max 1 active run per workspace (409) and 3 active runs globally (429).
  - **Variables & Variable Sets**: manage `terraform` and `env` variables with scalar or HCL values and write-only sensitive redaction (`(sensitive)` in logs, `json:"-"` in API). Create reusable Variable Sets assigned to multiple workspaces or applied globally with deterministic hierarchy resolution.
  - **Built-in Policy as Code & Cost Estimation**: evaluate guardrails against plan JSON (`deny_destroy`, `max_deletes`, `max_changes`, `deny_resource_types`, `require_tags`, `deny_open_cidr`) with `advisory`, `soft`, and `hard` enforcement levels. Automatic monthly cost delta estimation via optional `infracost` CLI.
  - **Drift Detection**: background scheduler runs periodic speculative `-refresh-only` plans to detect discrepancies between real-world infrastructure and state files.
  - **Private Module Catalog**: internal documentation catalog parsing module inputs, outputs, resources, providers, and README documentation from inline files or Git repos with interactive usage snippets.
  - **VCS Webhook Integration**: public webhook endpoint (`/api/terraform/webhook/{token}`) with 32-byte hex tokens, constant-time HMAC / secret token verification, and automated speculative plan triggers on Git pushes (apply is never automated).
  - **Encrypted System Snapshot**: workspaces, variables, variable sets, policies, modules, and the latest state version per workspace are backed up in AES-256-GCM / Argon2id snapshots.
- **Ansible: Templates and Files tabs** ([ADR-0055](docs/adr/0055-ansible-templates-and-uploaded-files.md)): store Jinja templates (text editor) and upload files (any type, including binary) in Pantau and reference them from inline playbooks with `template: src=<name>` and `copy: src=<name>`. Each run writes them to `templates/` and `files/` next to the playbook in its temporary workspace, so no playbook changes are needed. They are shared by all inline playbooks (Git playbooks keep using their repository's own folders), may use subfolders (`nginx/site.conf.j2`), are limited to 256 KiB per template, 5 MiB per file and 32 MiB in total, are included in encrypted System Snapshots, and uploaded files are never returned by the API. New endpoints `GET|PUT /api/ansible/assets` and `GET|DELETE /api/ansible/assets/{id}`.
- **Ansible: multi-select Import Folder**: Import Folder lists the YAML files of the chosen folder, preselects the real playbooks (top-level `hosts:`/`import_playbook:`), and creates one playbook per ticked entry in a single all-or-nothing import; `.ansible/` and `collections/` are skipped. Roles and collections come from the Library, the inventory is generated by Pantau, and `ansible.cfg` is set in Ansible Settings. See [ADR-0053](docs/adr/0053-ansible-shared-projects-library-and-config.md) (project storage superseded by ADR-0054).
- **Ansible Library**: new Library tab to install collections and roles from Ansible Galaxy (name + version, or a `requirements.yml` limited to names/versions) or import them from local folders and collection tarballs, stored on the Pantau server (`ansible-library/` next to the database, not in System Snapshots) and used by every playbook run through `ANSIBLE_ROLES_PATH`/`ANSIBLE_COLLECTIONS_PATH`. Names and versions are validated before reaching the command line; `ansible-galaxy` runs with a throwaway HOME.
- **Ansible Library: live install output**: Galaxy, `requirements.yml`, and local installs now stream the `ansible-galaxy` output into the Library tab while they run (NDJSON responses), with the command echoed first and a heartbeat while the installer is silent. Installs finish on the server even if the browser disconnects. The install endpoints no longer return a single JSON body with `output`.
- **Ansible Configuration**: global `ansible.cfg` additions (sections `defaults`, `ssh_connection`, `privilege_escalation`) edited in Ansible Settings, validated with line numbers, merged into every run's generated configuration, and included in System Snapshots. Keys Pantau manages (inventory, SSH key/args, host key checking, search paths, interactive prompts) are rejected. Runs no longer force `-f 5`; `forks` from the configuration applies.
- **Ansible: editor with editable variables** ([ADR-0054](docs/adr/0054-ansible-copy-import-merged-editor-and-variables.md), replaces the separate "View contents" dialog of [ADR-0052](docs/adr/0052-ansible-playbook-contents-and-variables.md)): one editor per playbook with **Playbook**, **Variables**, and (Git only, read-only) **Files** tabs. The Variables tab scans the YAML as you type (`POST /api/ansible/playbooks/analyze`, unsaved edits included) and lists the variables it references as required / optional / defined; the value column is the playbook's default extra vars, where values can be set, edited, deleted, and added. Adds `gopkg.in/yaml.v3`. `GET /api/ansible/playbooks/{id}/contents` is now Git-only.
- **Ansible Import Folder copies the playbooks into Pantau** ([ADR-0054](docs/adr/0054-ansible-copy-import-merged-editor-and-variables.md), replaces [ADR-0051](docs/adr/0051-ansible-imported-playbook-folders.md)): the browser reads the ticked YAML files and `POST /api/ansible/playbooks/import` (JSON) creates ordinary editable inline playbooks, all or nothing. Nothing of the folder is stored or linked. The bundle source, `ansible_projects`, and the replace/update-files flow are removed. **Migration:** existing imported-folder playbooks become inline playbooks holding their main YAML; the other files of their archive (roles, templates, `group_vars`, ...) are not kept. Old snapshots are converted the same way on restore.
- **Ansible Playbooks (Semaphore-style, agentless)**: store YAML playbooks in SQLite, run them against managed hosts via local `ansible-playbook` CLI, and stream live stdout/stderr execution logs into the UI. Inventory is generated on the fly from targeted Hosts, Host Groups, and the SSH Key Library, with dynamic temporary connection parameters (`ansible_host`, `ansible_port`, `ansible_user`, `ansible_ssh_private_key_file`, and `ansible_python_interpreter=auto_silent`). Features an administrator-controlled opt-in **Ansible Gate** (`ansible_enabled`), support for multiple execution modes (`run`, `check`, `diff`, `syntax`), extra vars JSON, tags/skip-tags, verbosity (0..3), and passwordless become switch. Private keys are written to `0600` files inside isolated `0700` workspaces and guaranteed deleted immediately upon run completion/cancellation. Retains up to 100 runs per playbook with a 2 MiB memory ring buffer for live log streaming. Playbook definitions are backed up inside encrypted System Snapshots. See [ADR-0049](docs/adr/0049-ansible-playbooks-semaphore-style.md).
- **Ansible: Git playbook source and cron schedules** ([ADR-0050](docs/adr/0050-ansible-git-playbooks-and-cron-schedules.md)): a playbook can now come from a Git repository (`https://`, `ssh://` or `user@host:path`; branch/tag, playbook path, none/access-token/SSH-Key-Library auth) that is shallow-cloned per run next to `roles/` and `group_vars/` (the repository's own `ansible.cfg` is ignored; the commit that ran is recorded and shown in history). **Schedules** run a playbook on a 5-field cron expression (server local time, 15 s scheduler tick) against fixed Hosts/Host Group with mode, tags, extra vars, verbosity and become; the last run/error and next run are shown, missed occurrences (>5 min late) are skipped instead of replayed, and the Ansible Gate and the usual 1-per-playbook / 3-global run limits apply. Tokens are write-only and never returned by any API. Playbooks (incl. Git settings) and schedules are part of the encrypted System Snapshot. The Docker image now includes `git`.
- **Cloudflare Management via REST API v4**: manage multiple Cloudflare accounts directly from Pantau using official scoped API tokens (without Global API Keys or external SDK dependencies). View and filter Cloudflare Zones, manage DNS records (A, AAAA, CNAME, TXT, MX, NS, SRV, CAA with proxied toggle and live record count), perform Cache Purges (Purge Everything with confirmation or Purge by URL up to 30 files), and adjust Zone Settings (SSL mode, Always Use HTTPS, Security Level, Development Mode, Minimum TLS Version, and Automatic HTTPS Rewrites). Tokens are stored in SQLite like SSH private keys, never returned by the API, and backed up inside encrypted System Snapshots. See [ADR-0048](docs/adr/0048-cloudflare-management-via-api.md).
- **External Kubernetes clusters from Kubeconfig** (AWS EKS, GCP GKE, Azure AKS, or any remote/managed cluster): add clusters by pasting or uploading a kubeconfig, picking a context, and choosing a Kubernetes Mode (`full`, `no-exec`, `read-only`). Commands run via a local `kubectl` subprocess on the Pantau server, reusing all existing views (nodes, workloads, pods, services, events, describe, live WebSocket logs, YAML editor with diff gate, scale, restart, delete, cordon, drain). Secure input pipeline: minified to single-context JSON via `kubectl config view`, 128 KB size limit, rejection of file-path credentials (`certificate-authority`, `client-certificate`, `client-key`, `tokenFile`), command-based `auth-provider` rejection, and strict allowlist for `exec` plugins (`aws`, `aws-iam-authenticator`, `gke-gcloud-auth-plugin`, `gcloud`, `kubelogin`, `az`, `doctl`, `oci`). Pod Exec in Terminal Dock is unsupported for external clusters (requires SSH PTY). Kubeconfig is never returned over API, stored plaintext in DB like SSH keys, and backed up in encrypted System Snapshots. New REST endpoints under `/api/kube/clusters`. See [ADR-0047](docs/adr/0047-external-kubernetes-clusters-from-kubeconfig.md).
- **Delete several Desired State rules at once**: a checkbox per rule (plus select-all) and a *Delete selected (N)* button with a single confirmation.
- **Side Panel** (left navigation) with *Hosts*, *Docker* and *Kubernetes* sections; collapsible to an icon rail (remembered), off-canvas under 768px, EN/ID. The Docker section is a searchable Host picker that opens Host Detail on the Docker tab.
- **Kubernetes management for k3s / RKE2** (agentless, over SSH, control-plane hosts only): cluster detection plus *Nodes · Workloads · Pods · Services · Events · Apply* views with a namespace selector. Actions: scale and rollout-restart (Deployment/StatefulSet/DaemonSet), delete Pod, cordon/uncordon Node, describe, one-shot Pod logs, and apply YAML (≤64 KB, server-side dry-run first). `kubectl` runs as the SSH user and falls back to passwordless `sudo -n` only when the root-only k3s/RKE2 kubeconfig is refused (never a password). Names, namespaces, kinds and operations are validated and shell-quoted. New endpoints below `/api/hosts/{id}/kube/`. See [ADR-0043](docs/adr/0043-side-panel-and-kubernetes-k3s-rke2-management.md).
- Kubernetes: **node memory in GB**, **searchable host picker**, more read-only resources (*Ingresses · ConfigMaps · Secrets · PVCs · PVs · Jobs · CronJobs*, one generic table; Secret values and key names never leave the host), **Pod Exec** (interactive shell in a Terminal Dock tab, `bash` else `sh`) and **live Pod logs** over WebSocket (container choice, tail, *Previous*, copy). New endpoints `GET /api/hosts/{id}/kube/resources`, `GET …/kube/exec-cmd` and `/ws/kube/logs`.
- Kubernetes **cluster identity and failover**: Hosts of one HA cluster are grouped into a single entry (cluster ID = `kube-system` namespace UID, stored in `hosts.kube_cluster_id`, filled by *Detect clusters* or `detect`). Reads fail over to a healthy peer server Host; `action`/`apply` fail over only when the SSH connection to the requested Host failed before any command ran. Responses report `served_by_host_*`/`failover`; the UI shows a *Gateway* chip and a toast.
- Kubernetes **usage metrics**: CPU and memory in use (against allocatable, GB) with bars in Nodes and usage columns in Pods, from the `metrics.k8s.io` API; falls back to capacity when metrics-server is absent.
- Kubernetes **action safety**: per-Host *Kubernetes Mode* (`full`/`no-exec`/`read-only`, enforced server-side incl. `/ws/terminal` for `kubectl exec`; an application guardrail, not a host security boundary), confirmations with concrete impact for scale-to-0, restart rollout, delete Pod and cordon, and **Apply preview**: `POST …/kube/diff` (server dry-run + `kubectl diff`) with Apply locked until the current YAML has been diffed. See [ADR-0044](docs/adr/0044-kubernetes-cluster-identity-usage-metrics-and-action-safety.md).
- Kubernetes **YAML editor** (Workloads, Services, Ingresses, ConfigMaps, PVCs, Jobs, CronJobs; never Secrets): clean editable YAML with `resourceVersion` kept for conflict detection, Preview diff then confirm then Apply (view-only in `read-only` mode). **Workload logs** (all pods of a Deployment/StatefulSet/DaemonSet with `[pod/…]` prefixes), per-line **log filter** and **Timestamps**. **Drain node** in two steps (server dry-run preview of evicted/blocked pods, then a confirmation that warns about downtime, emptyDir/force and etcd quorum on control-plane nodes; bounded by `--timeout`, node stays cordoned on timeout with an *Uncordon* shortcut). **Warning-only Events filter**, problem Pods listed first, and opt-in **auto-refresh** (10/30/60 s; paused while the tab is hidden or a dialog is open). Pod memory below 1 GB shows two decimals. New endpoints `GET …/kube/yaml` and `POST …/kube/drain-preview`; `/ws/kube/logs` accepts `workload` and `timestamps`. See [ADR-0045](docs/adr/0045-kubernetes-yaml-editor-workload-logs-drain-and-auto-refresh.md).
- **More notification channels**: ntfy, Slack, Discord and SMTP (`starttls`/`tls`/`none`) next to Telegram and the generic webhook; *Test Notification* reports each channel separately; secrets (`ntfy_token`, `smtp_pass`) are never returned by `GET /api/settings` (only `*_set` flags). **Alert Debounce** (`alert_debounce_runs`, 1–10): a Drift alerts only after N consecutive Inspections; recovery still closes immediately.
- **Desired State rules `http` and `tls`**, evaluated from the Host over SSH: HTTP status/latency (`2xx,<1500ms`) and TLS certificate days left (`>=14d`). **Backup rules can verify contents** (`fresh_24h,verify=gzip|tar|zstd|xz|sha256`; sha256 against `<file>.sha256`), and glob targets such as `db-*.sql.gz` now really pick the newest match.
- **Bulk Command Execution** (header button, `POST /api/exec/bulk`): run one command on up to 20 Hosts (concurrency 5, per-Host timeout, 64 KB output cap, optional `sudo -n`, streamed results table, confirmation first); honours Kube Mode for `kubectl exec`.
- **Package upgrades** in Host Detail (Packages/Security): preview of pending packages (distinct packages, security count; apt/dnf/yum, needs `sudo -n` for accurate dnf data) then a streamed upgrade (`all`, or `--security` on dnf/yum); no automatic reboot or rollback. See [ADR-0046](docs/adr/0046-notification-channels-http-tls-rules-bulk-exec-package-upgrade-backup-verify.md).
- **Docker management (Portainer-style)** in Host Detail → Docker, now with sub-tabs *Containers · Images · Volumes · Networks · Stacks · Stats · System*. Containers: add container form (image, name, restart policy, ports, env, volumes, network, command, memory/CPU limits), **Inspect** dialog (state, restarts, limits, ports, mounts, networks, env with values hidden until revealed, labels, OOM-kill and exit-code hints), start/stop/restart/pause/resume/kill/rename/remove, logs and an interactive **Console** (`docker exec` in a Terminal Dock tab). Images: list with in-use/dangling flags, pull, remove, run from image. Volumes and Networks: create, remove, connect/disconnect containers. **Stacks** (Docker Compose projects): status per project, up/down/start/stop/restart/pull/recreate/update, view and edit the compose file of single-file projects and deploy new stacks (validated with `docker compose config`, stored under `~/.pantau/stacks/<name>/` on the host, previous file kept as `.pantau-bak`). **System**: engine/host info, disk usage with reclaimable space, and targeted prune actions (stopped containers, dangling/all unused images, unused volumes, unused networks, build cache) each behind a confirmation dialog; there is deliberately no prune-everything action. Endpoints under `/api/hosts/{id}/docker/` (`images`, `system`, `stacks`, `stack/file`, `inspect`, and POST `action`, `run`, `image`, `volume`, `network`, `stack`, `stack/deploy`, `prune`). When the SSH user cannot reach the Docker socket but has passwordless sudo, commands transparently fall back to `sudo -n docker` (never a password). Every name/image/project is validated and shell-quoted. See [ADR 0042](docs/adr/0042-docker-management-portainer-style.md).
- **Services (systemd) tab** in Host Detail: read-only list of every systemd service unit with state (active/failed/inactive/activating), sub-state, enabled-at-boot state and description, summary tiles (running, failed, enabled, total), filter pills and search; failed units sort first. New endpoint `GET /api/hosts/{id}/systemd` (`systemctl list-units` + `list-unit-files`, no sudo needed). Hosts that do not run systemd (macOS, Alpine/OpenRC, SysV) show an explanatory empty state.
- Services tab **status, lifecycle actions and error debugging**: each unit has a *Status & debug* dialog (state, last result and exit code, running/stopped since, PID, restarts, memory, unit file and boot state, `systemctl status` output, recent journal with an *Errors only* switch and copy) and a **Probable causes** panel that explains failures from the journal/properties (port already in use, missing executable, bad `User=`/`WorkingDirectory=`/`EnvironmentFile=`, start-limit hit, OOM kill, configuration error, failed dependency, timeout, permission denied, restart loop, masked unit, …) quoting the evidence line. Actions: **Start, Stop, Restart, Reload, Enable/Disable at boot, Mask/Unmask, Reset failed state** per service and **Reload systemd** (`daemon-reload`) for the host. They run as root, else through passwordless `sudo -n`, else plain `systemctl` (polkit, whose real refusal message is shown); Restart/Stop/Disable/Mask ask for confirmation, a unit that fails right after being started is reported as failed instead of successful, stop/disable/mask of the SSH daemon are blocked server-side, and every action is written to the Activity Console. The journal is read with `sudo -n` only when the SSH user cannot read it; after a refusal sudo is not retried automatically for 24 hours (shared with the port-owner probe) and the dialog offers **Retry with sudo**. New endpoints `GET /api/hosts/{id}/systemd/detail?unit=…[&errors=1][&retry_sudo=1]` and `POST /api/hosts/{id}/systemd/action`; unit names are validated (`*.service`, no shell metacharacters or leading `-`). ADR-0041.
- Listening ports now show the **process behind each port** (e.g. `sshd`, `nginx`, `postgres`) in the Security tab's open-ports table. Resolution order: (1) owner visible to the SSH user via `ss -tlpn` / `netstat -tlpn` (`pid/name`); (2) for owners the user cannot see, **passwordless `sudo -n`** is used automatically when the host allows it (never a password; a host where sudo is refused is not probed again for 24 hours so at most one failed `sudo` attempt reaches its auth log per day); (3) otherwise a well-known-port name (`ssh`, `mysql`, `redis`, …) shown in italics with a "guessed from port" label and never presented as the running process; unknown ports stay `—`. macOS hosts use `lsof` for the same purpose. Stored in the existing `process` field plus a new `process_source` (`process` or `port_hint`). Hosts inspected before this change keep their stored value until the next inspection (raw `ss` columns are normalized in the UI).
- Docker tab: **Stats** (live `docker stats --no-stream` per running container with CPU/memory bars, network/block I/O, PIDs, totals and optional 10s auto-refresh), **Volumes** (driver, size, mountpoint, containers using each volume, `unused` and anonymous flags) and **Networks** (driver, scope, subnets, gateways, attached containers with IPs) next to the existing Containers view. New read-only endpoints `GET /api/hosts/{id}/docker/stats`, `/docker/volumes` and `/docker/networks`; Homebrew/Docker Desktop PATH is applied on macOS hosts.
- Bulk Host Import from SSH Config: new **Import** button on the dashboard imports hosts from an OpenSSH client config (`~/.ssh/config`) pasted into the dialog, chosen as a file, or read from the Pantau server user's `~/.ssh/config`. A preview lists every concrete `Host` alias with its resolved `HostName`, `User` and `Port` (OpenSSH first-value-wins, so `Host *` defaults apply; wildcard and negated patterns are skipped; `Match` and `Include` are reported but not evaluated). Hosts already registered (same address, port and user) and repeated endpoints inside the file are flagged and cannot be selected.
- Post-Import Group Form: after importing, a form lists the new hosts with one Host Group field each (suggesting existing groups and groups already typed in the form); hosts left empty stay ungrouped. Saved atomically through `PUT /api/hosts/groups`.
- REST API Endpoints: `POST /api/hosts/import/preview`, `POST /api/hosts/import` and `PUT /api/hosts/groups`.
- Centralized SSH Key Library: dedicated management panel under Settings → SSH Keys to register and maintain reusable private keys once, eliminating repetitive copy-pasting across individual host forms.
- Dropdown Key Selection: host creation and edit dialogs now support selecting from registered library keys (`key_id`), alongside custom private keys and the universal global SSH key.
- Deterministic Key Resolution Hierarchy: unified precedence across all SSH operations (inspection, file transfer, terminal, tunnel, and network diagnostics): Library Key (`key_id > 0`) → Custom Key (`custom_key`) → Global SSH Key (`settings.ssh_private_key`), with explicit error reporting if a referenced library key is missing.
- Automated Public Key & Fingerprint Derivation: server-side parsing with `golang.org/x/crypto/ssh` automatically extracts OpenSSH public key (`authorized_keys` format) and computes SHA-256 fingerprint (`SHA256:...`) with trailing newline normalization.
- Active Key Deletion Guard: deleting a library key is strictly rejected (`HTTP 409 Conflict`) while any host still references it (`used_by > 0`), preventing unexpected connection failures.
- Passphrase-Protected Key Rejection: early validation rejecting passphrase-protected private keys (`HTTP 400 Bad Request`) to ensure non-blocking daemon operation.
- Zero-Exposure Credential Safety: private key field omitted from JSON serialization (`json:"-"`), blocked from settings API, and deterministically redacted in logs (`[REDACTED]`).
- Context-Aware Key Provisioning & Key Injection: one-time password provisioning and the `inject-key` host action automatically inject the public key of the host's selected library key when configured, falling back to the global public key when unassigned.
- Encrypted System Snapshot Integration: `ssh_keys` records included in AES-256-GCM snapshots with backward compatibility and consistent key ID restoration.
- REST API Endpoints: introduced `GET /api/ssh-keys`, `POST /api/ssh-keys`, and `DELETE /api/ssh-keys/{id}`, with `key_id` support on `POST /api/hosts` and host update routes.
- Architectural Decision Record: [ADR-0035](docs/adr/0035-ssh-key-library.md).
- Agentless Host Security Scan: on-demand and scheduled security assessment identifying pending package security updates on Debian/Ubuntu (`apt`) and RHEL/CentOS/AlmaLinux/Rocky/Fedora/Amazon Linux (`dnf`/`yum`) via read-only SSH execution without requiring root/sudo privileges or remote agents.
- Severity & Advisory Classification: extracts upstream advisory identifiers (RHSA, ALAS, CVE) and package manager severities (Critical, Important, Moderate, Low) on dnf/yum, with transparent `unknown` severity tracking for apt updates.
- Reboot Required & Cache Freshness Detection: automatically checks `/var/run/reboot-required` and `needs-restarting -r` exit codes, while tracking package manager repository cache age and flagging stale caches (>7 days) with `partial` status warnings.
- Passive System Hardening Checks: audits 4 essential configuration controls: SSH root login policy (`sshd_permit_root_login`), SSH password authentication (`sshd_password_auth`), automated security update services (`auto_security_updates`), and firewall daemon/filtering rules (`firewall_active`).
- Honest Coverage Principle: strictly reports partial coverage, permission boundaries, or unsupported operating systems (Alpine, Arch, etc.) as `partial` or `unsupported` status with detailed notes, preventing misleading clean bills of health.
- Dedicated Security Workspace Tab: new Security tab in the Host Workspace Modal displaying pending security updates, total upgradable packages, severity breakdown badges, reboot-required indicator, findings table, hardening checks, and coverage notes with a 60-second cooldown on-demand rescan button.
- Desired State Security Rules: new `security_updates` rule kind (fixed target `security`, expected `none` or `<=N`) evaluated against the latest cached SQLite scan without live SSH overhead, generating drift alerts when unapplied security updates accumulate, scans exceed 72 hours, or status is unverified.
- Automated Background Worker & Settings: configurable `security_scan_interval_hours` setting (default 24 hours, range 1–720, or 0 to pause) evaluated by a 10-minute scheduler pool running up to 2 concurrent host scans.
- REST API Endpoints: introduced `GET /api/hosts/{id}/security` (with `X-Scan-Cooldown-Remaining` header) and `POST /api/hosts/{id}/security/scan` with anti-flood cooldown (`HTTP 429`) and concurrency guard (`HTTP 409`).
- Architectural Decision Record: [ADR-0036](docs/adr/0036-agentless-security-scan.md).
- Elevated SFTP File Access: execute remote SFTP server sessions as `root` or any valid local user account via passwordless `sudo -n -u <user>` over an SSH exec channel, granting full read/write capabilities (listing, file read/write, uploads, disk space-checked duplication, streaming archive downloads, deletion, and chmod) across restricted directories and other users' home directories.
- Zero-Password Credential Security: non-interactive invocation using strictly `sudo -n`, guaranteeing passwords are never requested, transmitted, or stored on the server or client.
- Sftp-Server Binary Auto-Discovery: automatic detection of standard OpenSSH sftp-server paths across Debian/Ubuntu, RHEL/CentOS, Arch/SUSE, and BSD distributions (`/usr/lib/openssh`, `/usr/libexec/openssh`, `/usr/lib/ssh`, `/usr/libexec`, `/usr/lib64/ssh`), with clear rejection for hosts using OpenSSH built-in `internal-sftp`.
- Elevation Failure Fallback & 1-Click Root Retry: graceful HTTP 403 error reporting with contextual toast notifications reverting to default SSH access if sudo requires a password or binary is absent, coupled with a 1-click 'Open as root' suggestion when standard directory listings encounter permission-denied errors.
- Visual Indicator & Activity Console Audit Logging: dedicated UI badge indicating active elevation user context, with every elevated file action and refusal deterministically logged to the in-app Activity Console.
- REST API Elevation Query Parameter: support for `?as=<username>` across `/api/hosts/{id}/files/*` endpoints (list, read, write, upload, size, duplicate, download, delete, chmod) with POSIX username validation (`^[A-Za-z_][A-Za-z0-9_.-]{0,31}$`).
- Architectural Decision Record: [ADR-0037](docs/adr/0037-elevated-file-manager-access.md).
- First-Class macOS (Darwin) Host Support: agentless monitoring and management for macOS hosts (Apple silicon Mac mini, Mac Studio, Mac Pro) via native OpenSSH commands without background agents or remote daemons.
- Activity Monitor Memory Calculation: physical RAM used extracted via `(anonymous - purgeable + wired + compressor) * pagesize` from `vm_stat` matching macOS Activity Monitor, paired with `sysctl vm.swapusage` for swap telemetry.
- APFS Shared Container Disk Metrics: evaluates `/System/Volumes/Data` via `df -lPk` (`used = total - available`) reporting actual container usage rather than the sealed root volume.
- IOKit Cumulative Disk I/O: extracts and aggregates cumulative `Bytes (Read)` and `Bytes (Write)` from `ioreg -rd1 -c IOBlockStorageDriver` block storage drivers.
- Physical Network Filtering: isolates physical `en*` interfaces from `netstat -ibn` `<Link#` rows, filtering out internal, virtual, and bridge interfaces (`lo`, `bridge`, `utun`, `awdl`, `anpi`, `gif`, `stf`).
- Hardware & CPU Identification: captures hardware model, identifier, and chip name via `system_profiler SPHardwareDataType` (fallback `sysctl -n hw.model` / `machdep.cpu.brand_string`), CPU cores via `sysctl -n hw.ncpu`, and OS install epoch via `/var/db/.AppleSetupDone`.
- Privacy-Enforcing Process Telemetry: collects top processes via `ps -Aro pid,user,%cpu,%mem,rss,ucomm` and `ps -Amo ...` strictly using `ucomm` to prevent command-line arguments and sensitive tokens from being stored or displayed.
- macOS Security Scan & Hardening Checks: non-root system update checks via `/usr/sbin/softwareupdate -l` with heuristic security classification (`macOS` and `Safari`), honest `partial` reporting on network timeouts, and 4 native hardening audits (`sshd_permit_root_login`, `sshd_password_auth`, `com.apple.SoftwareUpdate` auto-update keys, and application firewall state).
- Portable Diagnostics & Desired State Rules: native Darwin ping (`-t <seconds>`), pre-installed `/usr/sbin/traceroute` integration (never auto-installed), APFS disk volume rules, `launchd` service checks, `lsof` port checks, and `stat -f` backup freshness rules.
- Extended PATH Prelude: automatic PATH extension (`/usr/local/bin:/opt/homebrew/bin:/opt/homebrew/sbin:/Applications/Docker.app/Contents/Resources/bin`) for third-party CLI tooling discovery.
- Architectural Decision Record: [ADR-0038](docs/adr/0038-macos-host-support.md).

### Changed
- Host detail tabs: the security-related parts of the former "Network & Security" tab moved to the **Security** tab (new "Authentication & Exposure" section with the failed-login/brute-force counter and the open listening ports with exposure and risk). The tab is now named **Network** (egress, live bandwidth, disk I/O, active sockets, top remote IPs). On macOS hosts the failed-login counter shows "Not collected on this OS" instead of a misleading zero.
- **Visual redesign of the whole web UI** ([ADR-0039](docs/adr/0039-modern-visual-design-system-and-icon-sprite.md)): modern, clean look with a layered neutral dark palette, a single accent color, semantic colors reserved for status, Inter + JetBrains Mono typography and Lucide line icons. Covers the dashboard, Host Detail (all tabs), Host Comparison, Network Diagnostics, Terminal Dock, Activity Console, File Manager, Code Editor, Transfer Queue, Settings (now with a sidebar navigation), Documentation reader, and every dialog, toast and confirmation. Behavior, element ids and i18n keys are unchanged.
- Dashboard: new clickable **fleet KPI strip** (Hosts, Healthy, Drift, Offline, Stale, average RAM / peak disk) that filters the host list; **Grid View** cards are now compact for healthy hosts (3 gauges, live traffic sparkline, one meta line) and louder for problem hosts (semantic accent edge and a short "what is wrong" summary); **List View** is a real sticky-header table with mini gauges, sparklines, issue counts and last inspection. Grid and List remain switchable from the View Mode control. The empty "Add note" box was removed from cards without a note (use the card menu).
- Self-contained web assets: Inter and JetBrains Mono variable fonts (OFL) and the Lucide icon set (ISC) are embedded under `internal/web/static/vendor/fonts/` and an inline icon sprite; emoji are no longer used as interface icons. Design tokens in `:root` are the single source for colors, radius, shadows, typography and motion.
- Embedded HTML gzip size budget in `TestIndexGzipAndETagRevalidation` raised from 150 KB to 200 KB for the icon sprite and the new style system.

### Fixed
- **"Desired State Drift Detected" stayed after the rule was deleted** (or disabled): removing a rule deleted only the rule, while the Drift banner and the Host status come from the stored evaluation results (`actual_items`), which were never cleaned up. Deleting or disabling a rule now removes its result; if no Drift is left on a degraded Host it becomes healthy and its alerts close immediately instead of waiting for the next inspection, and every inspection also removes leftovers of rules deleted earlier.
- **Deleting several rules in a row kept asking to wait** (and the list did not update): after every delete the UI ran an inspection, the 15 s Inspection Cooldown refused every one after the first, and the Desired State list only refreshed when an inspection succeeded, so deleted rules stayed listed. Deleting a rule now refreshes the list directly (the server already clears its result). Adding a rule had the same problem (the new rule did not appear while the cooldown was active, because the refresh compared a string id with a number); it now appears at once and the follow-up inspection no longer shows a "please wait" warning.
- Network Diagnostics Traceroute returned "No hops detected" (or `timeout: failed to run command 'traceroute': No such file or directory`) on hosts where `tracepath` was present: `tracepath` block-buffers its output when piped, so when the 15s probe deadline killed it no hop ever reached Pantau, and the old `tracepath && … || traceroute` chain fell through to a missing `traceroute` whenever `tracepath` exited non-zero. Pantau now prefers `traceroute`, installs it first when missing (apt, dnf, yum, apk, pacman, zypper; installer output streams to the raw viewer; apt runs with `NEEDRESTART_MODE=l` so no services are restarted), and falls back to line-buffered `tracepath` (`stdbuf -oL`) when installation is not possible. Installing requires root or passwordless `sudo`; if neither tool exists and installation is impossible, the probe stops with a message naming what to install manually.
- RAM, swap, cores, and network unreadable on macOS hosts: Linux-only polyglot commands (`free`, `/proc`, `nproc`, `ss`, `timeout`, `ps --sort`) caused metric collection failures on Darwin; resolved via runtime `uname -s` Darwin inspection branch while keeping existing Linux behavior byte-for-byte unchanged.

## [0.25.0] - 2026-10-21
### Added
- Interactive Terminal Link Detection: native `term.registerLinkProvider` integration in xterm.js matching RFC 3986 URLs and remote loopback addresses, with `Ctrl+Click` (Linux/Windows) and `Cmd+Click` (macOS) interaction to prevent accidental terminal text selection conflicts.
- Remote Web Port Tunneling: agentless HTTP and WebSocket reverse proxy (`/api/tunnel/:hostId/:port/*`) streaming over OpenSSH RFC 4254 `direct-tcpip` channels (`golang.org/x/crypto/ssh` `DialContext`) without requiring public firewall port openings or remote daemons.
- High-Performance Connection Pooling: per-host SSH transport caching eliminating 100–300ms connection handshakes across sequential asset requests (CSS, JS, images), featuring transparent auto-reconnect fallback on idle connection dropouts.
- Native WebSocket Hijacking: full bidirectional streaming over SSH channels supporting protocol status `101 Switching Protocols` for real-time development servers and interactive web services.
- SSRF & Port Security Enforcement: strict hardcoded remote loopback binding (`127.0.0.1:<port>`), mandatory `pantau_session` authentication, port range boundaries (1-65535), and blacklisting of the target host's own active SSH port.
- Interactive Remote Web Service Modal: confirmation dialog on clicking remote localhost URLs in terminal output with 1-click tunnel launch and link copying.
- Terminal Ports Dropdown Menu: dedicated `🌐 Ports` toolbar menu in the terminal dock for manual port forwarding entry and persistent recent port history (`localStorage`).
- Zero-Configuration Remote Browser Bridge: automated OSC 1337 (`OpenURL`) and OSC 777 escape sequence receiver routing CLI tool browser requests (`gh auth login`, `xdg-open`, Python `webbrowser`, AWS SSO, `npm login`) directly to the client's desktop browser without headless text-browser (`w3m`/`lynx`) traps.
- Silent Shell Environment Auto-Provisioning: non-intrusive provisioning of `/tmp/pantau-browser` and silent environment injection (`BROWSER`, `GH_BROWSER`) across login shells with zero console echo.
- Architectural Decision Record: [ADR-0034](docs/adr/0034-terminal-link-detection-remote-browser-bridge-and-port-tunnel.md).

## [0.24.0] - 2026-10-16
### Added
- Live Search in Terminal Presets: real-time search filtering in the terminal presets dropdown by preset title, command, or tmux session tag (`t:tag`), enabling instant command dispatch across extensive preset catalogs.
- Terminal Pinned Paths Dropdown: quick-access dropdown (`📌`) in the terminal toolbar displaying all pinned files and directories for the active host with real-time query filtering by path and operator notes.
- Instant Path Actions: single-click directory navigation (`cd '<path>'`) and quoted path injection into the active terminal shell session without executing extraneous commands.
- Quick Jump to File Manager: one-click toolbar button (`📁 Files`) that switches context seamlessly from the active terminal tab to the Host File Manager focused on the current working directory.
- Working Directory Synchronization: automated tracking of remote shell working directory via terminal window titles and OSC 7 ANSI escape sequences.

## [0.23.0] - 2026-10-16
### Added
- Multi-Host Side-by-Side Comparison: interactive evaluation matrix comparing 2 to 4 hosts simultaneously inside a Workspace Modal (75vw × 80vh) without additional backend endpoints.
- Best-in-Class Head-to-Head Scoring: transparent scoring across 4 standardized dimensions (Compute, Storage & I/O, Network Health, System Hygiene & Lifecycle) with a 5% deadband noise filter and soft penalty for unreachable/stale hosts.
- Executive Decision Narrative Generator: automated decision recommendation synthesis highlighting winner strengths, workload placement recommendations, and risk warnings for saturated hosts.
- Markdown Report Export: one-click copy to clipboard formatting full comparative scoreboard, standings, and takeaway summary for documentation and stakeholder meetings.
- Global Header Launcher: dedicated ⚖️ Host Comparison button in header navigation cluster.
- File Pinning & Operational Notes: pin mission-critical configuration files and log directories per host with custom operational notes and note badges in file manager.
- Quick Access Bar: visual pinned toolbar positioned above file manager directory listing for instant 1-click navigation to pinned files and folders.
- Global Bookmarks Modal: dedicated `🔖 Bookmarks` modal accessible globally from header navigation cluster with real-time search, file viewer/editor launcher, inline note editing, and one-click Terminal Dock launch into parent directories.
- File Pins Persistence & REST API: backed by SQLite `host_file_pins` table and dedicated endpoints (`GET/POST/DELETE /api/hosts/{id}/pins`, `GET /api/pins`) with automatic cascade cleanup on host removal.

## [0.22.0] - 2026-10-15
### Added
- Agentless Network Diagnostics: on-demand diagnostic probe tools over SSH (Ping, Traceroute with intelligent tracepath fallback, TCP Port Probe, and DNS Resolve) with real-time Server-Sent Events (SSE) streaming.
- Intelligent Hop Analysis: heuristic categorization of network hops into Local/Private VPC, ISP Transit, and Target Destination zones, with automated blackhole packet drop pinpointing and latency spike alerts (>60ms).
- Zero-Dependency TCP Port Probe: direct `/dev/tcp/<target>/<port>` socket testing in remote bash, eliminating external utility dependencies like `nc`, `telnet`, or `nmap`.
- Singleflight Concurrency Guard: per-host diagnostic locking using `sync.Map` to prevent command stacking and redundant SSH connection exhaustion.
- Dual Access UI: dedicated "Network Diagnostics" tab in Host Workspace Modal and global quick-access launcher in header navbar cluster with offline target protection.
- Architectural Decision Record: [ADR-0033](docs/adr/0033-agentless-network-diagnostics-and-hop-analysis.md).

## [0.21.0] - 2026-10-15
### Added
- Process Telemetry: real-time agentless extraction of top 5 resource-consuming processes (Top CPU, Top RAM/RSS, and Top Swap) on every inspection cycle without remote daemon overhead.
- Top Resource Consumers Widget: instant multi-column process observability card in the Host Detail Overview tab with direct one-click Terminal Dock launcher.
- Agentless Package Inventory: polyglot multi-distro software inventory discovery across Debian/Ubuntu (`dpkg`), RHEL/CentOS/AlmaLinux/Rocky (`rpm`), Alpine (`apk`), and Arch (`pacman`).
- Software Hygiene & Dormant Package Detection: automatic classification of Manual Install vs Base System packages and heuristic detection of dormant packages with inactive/failed/disabled systemd services.
- Packages Tab in Workspace Modal: dedicated searchable package management tab with instant filtering (`Manual Install`, `Dormant Packages`, `All Packages`), disk size sorting, and on-demand rescan trigger.
- Architectural Decision Record: [ADR-0032](docs/adr/0032-process-telemetry-and-package-inventory.md).
- TOFU (Trust On First Use) Host Key Verification: implemented `TOFUHostKeyCallback` in SSH runner, automatically accepting host keys upon initial connection while strictly verifying fingerprints on subsequent connections to prevent active Machine-in-the-Middle (MITM) attacks.
- OpenSSH 4.x & CentOS 5 Host Key Support: added `ssh-dss` (`ssh.KeyAlgoDSA`) to supported host key algorithms in `configureLegacyCompatibleSSH` alongside `ssh-rsa` and legacy ciphers/kex (`3des-cbc`, `diffie-hellman-group1-sha1`).
- Universal POSIX Single-Quoting (`shellQuote`): standard Bourne shell escaping (`'...'`) across all remote command interpolations (file manager, cross-host transfer, container inspection, service diagnostics, and backup freshness), eliminating command injection vulnerabilities across all Linux kernel and Bash versions.
- Architectural Decision Record: [ADR-0031](docs/adr/0031-security-hardening-and-legacy-os-compatibility.md).

### Changed
- CLI Console Log Level & Noise Reduction: defaulted terminal console output to `ERROR` level (`-log-level=error`), keeping runtime CLI output completely quiet while preserving full live telemetry in Web UI Activity Console.
- CLI Logging Flags: added `-log-level` (`PANTAU_LOG_LEVEL`) supporting `debug`, `info`, `warn`, `error`, `off` and `-q`/`-quiet` shorthand for error-only output.
- Startup & Shutdown Log Deduplication: removed redundant standard library `log.Printf` statements that duplicated `logger.Info` events at startup, shutdown, and scheduled backup.

### Fixed
- Swap Telemetry Overhead: conditioned per-process swap probe strictly on actual swap utilization (`SwapTotal - SwapFree > 0`), eliminating `/proc/*/status` scanning on zero-swap hosts.
- Multi-Distro Package Inventory: expanded package parser to support 3-column outputs for Alpine Linux (`apk`) and Arch Linux (`pacman`) with human-readable unit conversion.
- Systemd Service Status Parsing: handled bullet and symbol prefixes (`●`, `*`) in `systemctl list-units` to guarantee accurate dormant package identification for failed or inactive units.

### Security
- WebSocket Authentication & CSWSH Protection: wrapped `/ws/terminal` and `/ws/docker/logs` with `authMiddleware` and enforced same-origin matching (`strings.EqualFold(u.Host, r.Host)`) in `websocket.Upgrader.CheckOrigin`, preventing Cross-Site WebSocket Hijacking while preserving full plain HTTP accessibility.
- Cryptographic Token Generation (`crypto/rand`): replaced timestamp-based token derivation with 32-byte hex cryptographic random tokens in `issueSession` and 16-byte hex tokens for 2FA challenges. Retained `Secure: false` to allow seamless operation over plain HTTP and private LAN IPs.
- Container Name Regex Sanitization: enforced strict regex validation (`^[a-zA-Z0-9][a-zA-Z0-9_.-]+$`) and shell quoting on container parameters in `/ws/docker/logs` and inspector actions.
- Desired Rule Metacharacter Filtering: added strict rejection of shell metacharacters (`;`, `&`, `|`, `$`, `` ` ``, `\n`, `\r`) in `POST /api/hosts/{id}/rules` and port number range validation (`1-65535`).
- Bootstrap & Snapshot Protection: updated `IsFresh()` in store to verify whether `admin_password_hash` has been customized from default, preventing unauthenticated callers from triggering snapshot import or database wipe on initialized instances.

## [0.20.0] - 2026-10-14
### Added
- In-App Activity Console & Live Event Stream: real-time in-app visibility of all daemon and subsystem runtime activities (`inspect`, `ssh`, `transfer`, `terminal`, `snapshot`, `system`, and `auth`) delivered via zero-dependency Go stdlib `log/slog` and Server-Sent Events (`GET /api/logs/stream`).
- Telemetry Ring Buffer: in-memory fixed-capacity ring buffer (100 records) and non-blocking broadcaster providing instant context upon client connection.
- Redaction Filter: deterministic boundary filter automatically sanitizing SSH passwords, private keys, authentication tokens, passphrases, and 2FA secrets into `[REDACTED]`.
- Interactive Activity Console Drawer: collapsible bottom drawer docked above footer with 5-column running table (`Time`, `Level`, `Module`, `Host`, `Message & Details`), Smart Auto-Scroll, pause/resume freeze toggle, severity level filtering (`ALL`, `ERROR`, `WARN`, `INFO`), instant search, and client-side `.log` / `.json` export.
- Footer Status Trigger & Shortcut: footer button `⚡ Activity` with live pulse indicator, unread error badge counter, and global keyboard shortcut `Ctrl + \`` (backtick).
- Rendering Throttling: 100% skipped DOM rendering when drawer is closed and coalesced updates via `requestAnimationFrame` when open to eliminate CPU waste and layout thrashing.
- CLI Update Checker Release Notes Display: `pantau -check-update` displays formatted release notes extracted from upstream release payload in terminal output.
- Automated Release Notes Extraction: GitHub Actions release workflow (`release.yml`) automatically extracts tag-specific release notes directly from `CHANGELOG.md` for GitHub Releases distribution.
- Architectural Decision Record: [ADR-0030](docs/adr/0030-in-app-activity-console-and-live-event-stream.md).
### Fixed
- Instance Guard Graceful Handover: added `AcquireWithTimeout` (up to 3 seconds retry window) and explicit lock cleanup/database closure on server shutdown to allow graceful lock handover during restart without premature child exit.
### Removed
- Project Sponsorship Elements: removed Saweria funding badge, sponsor button in footer, and Settings sponsorship card to maintain a clean, distraction-free interface.


## [0.19.1] - 2026-10-12
### Fixed
- Port Auto-Scan IPv4 Loopback Probe: actively probe `127.0.0.1` via `net.DialTimeout` and verify IPv4 loopback binding (`tcp4`) before binding wildcard listeners, preventing dual-stack IPv6 phantom bindings on Windows Winsock when an existing IPv4 service occupies the port.

## [0.19.0] - 2026-10-11
### Added
- Real-Time Disk I/O Observability & Speedometer: periodic extraction of host-level disk read/write throughput rates (bytes/s) via agentless POSIX `/proc/diskstats` whitelist filtering (`sd*`, `vd*`, `nvme*n*`, `xvd*`, `mmcblk*`) multiplied by 512-byte sector size.
- Reboot Protection & Counter Rollover Guard: automatically detects host reboot/rollover counter drops (`delta < 0`), resetting rate to 0 bps to prevent erroneous negative spikes and establishing the new reading as baseline.
- Disk I/O Load Status Badges: qualitative color-coded throughput status pills (Optimal `< 10 MB/s`, Active `10–80 MB/s`, High I/O Load `≥ 80 MB/s`) on Host Cards, Table View, and Host Detail modals with bilingual support (EN/ID).
- Architectural Decision Record: [ADR-0029](docs/adr/0029-realtime-disk-io-observability-and-speedometer.md).

## [0.18.0] - 2026-10-05
### Added
- Local File & Folder Duplication: instant cloning of files and directories on the same host via native remote `cp -a` with default timestamp naming (`<name>_<YYYYMMDDHHmmss>.<ext>` or `<name>_<YYYYMMDDHHmmss>`).
- Disk Space Safety Check: pre-execution verification against remote partition free space via `df -PB1` with a 100 MB safety buffer to protect host disks from exhaustion (HTTP 507 Insufficient Storage).
- Duplicate Confirmation Modal: dialog displaying item size, partition available disk, 500 MB threshold warning badge, pre-filled editable target name, and collision prevention rejecting existing target names (HTTP 409 Conflict).
- Editor Modal Maximize: full-viewport maximize toggle button (`⛶` / `🗗`) and double-click modal header shortcut with automatic CodeMirror canvas and gutter re-alignment.
- Modern VS Code Dark+ Theme: embedded self-contained dark theme for CodeMirror (`cm-s-vscode-dark`) with modern monospace font stack (`JetBrains Mono`, `Fira Code`, `Consolas`) and dynamic syntax highlight mode detection by file extension (`getEditorModeForPath`).
- Interactive Breadcrumb Navigation: clickable hierarchical path bar (`file-breadcrumb-bar`) in SFTP File Manager for instant jumping to any parent directory.
- File Manager UI Polish: contextual file icons by extension (`getFileIcon`), streamlined 6-column action grid with tooltips, and sticky table headers (`files-table-container`).
- Saweria Project Sponsorship: added Saweria funding configuration (`.github/FUNDING.yml`), sponsor badges in READMEs, sponsor footer pill, and settings modal card with bilingual support (EN/ID).
- Architectural Decision Record: [ADR-0028](docs/adr/0028-file-duplication-safety-editor-maximize-and-ui-polish.md).
### Changed
- Preset Manager Layout: widened preset modal dialog to `modal-dialog-lg` with horizontal scroll containers to prevent action and scope column squishing.
### Refactored
- Simplified version parsing in `updater.CompareVersions` using standard integer conversions without redundant character scans.
- Replaced manual buffer read/write loops in `MockRunner.PipeCommand` and `MockRunner.Terminal` with standard library `io.Copy`.
- Enforced dark theme consistency across CodeMirror session restoration.

## [0.17.0] - 2026-09-30
### Added
- Port and Process Desired State Evaluators: automated agentless compliance checks for listening network ports and running daemon processes with portable OS command fallbacks (`ss`, `netstat`, `lsof`, `fuser`, `pgrep`, `pidof`, `ps`).
- Desired State Rule Creation Hints: dynamic form guidance, validation hints, and default values for `port`, `process`, `cron`, and `backup` rules in the manual rule editor.
- Sub-Header Workspace Shelf: adaptive navigation shelf beneath main header holding minimized Host Detail and Code Editor sessions for instant 1-click restore without layout shifts or floating clutter.
- Smart Resume: automatically restores last navigated tab and SFTP File Manager directory when reopening hosts from dashboard host cards.
- Code Editor In-Memory Retention & Dirty Indicator: preserves unsaved file edits across minimization with visual dirty dot (`•`) and explicit close confirmation dialog.
- Window Modal Controls: standardized minimize button (`—`) alongside close button (`✕`) on Workspace Modals with safe `Escape` minimization and `Alt+1` to `Alt+5` quick-switching shortcuts.
- Workspace Modal Maximize: full-viewport maximize toggle button (`⛶` / `🗗`) and modal header double-click shortcut.
- Contextual Terminal Launcher from SFTP: launch interactive terminal session directly into the active directory from SFTP File Manager toolbar and table rows via `dir` query parameter.
- Terminal Clipboard Bridge & Multiline Paste Safety: context-aware `Ctrl+C` (copy on selection, SIGINT on idle), `Ctrl+Shift+C`, `Ctrl+V` bridge for non-HTTPS/IP origins, and 3-way multiline paste safety modal with single-line flattening (`Paste as Single Line`).
- Terminal Tab Host Filter: instant search box in Terminal Dock new tab menu with host name and IP filtering, auto-focus, and keyboard navigation (`Escape`, `Enter`).
- Architectural Decision Records: [ADR-0025](docs/adr/0025-desired-state-rule-taxonomy-and-evaluators.md), [ADR-0026](docs/adr/0026-sub-header-workspace-shelf-and-smart-resume.md), and [ADR-0027](docs/adr/0027-terminal-clipboard-bridge-and-multiline-paste-safety.md).
### Changed
- Static HTML compression: utilizes `gzip.BestCompression` for in-memory HTML pre-compression at startup.
- SFTP File Manager layout polish: consistent action button grid column widths and download button styling.
### Refactored
- Deduplicated 40-column SQL queries in host store and terminal preset scan logic.
- Replaced external terminal tty check dependency with standard library `os.Stdout.Stat`.
- Replaced custom HTML escaping helper with standard library `html.EscapeString`.

## [0.16.0] - 2026-09-26
### Added
- Hardware Commission Date Override: manual configuration of server commission date (`commission_date`) on Host entities to accurately assess Productive Lifespan for New Old Stock (NOS) or refurbished physical hardware with older motherboard BIOS dates.
- Immediate Lifecycle Recalculation: instant re-evaluation of Lifecycle Score and diagnostic breakdown upon updating host commission dates without requiring a new SSH inspection run.
- Commission Date validation: enforces non-future date bounds and prevents dates earlier than the physical motherboard BIOS release date.
- Dedicated quick modal and action shortcut (`openCommissionDateModal`) directly on the Lifecycle Assessment card in Host Detail Workspace Modal.
- Architectural Decision Record [ADR-0024](docs/adr/0024-hardware-commission-date-override.md) and domain term preservation in `CONTEXT.md`.

## [0.15.0] - 2026-09-25
### Added
- Floating Alert Summary Bar: non-blocking summary banner (`⚠️ {count} Active Alerts ({hostCount} Hosts)`) with zero layout shift, collapsible floating overlay panel, full ISO datetime tooltips, inline Root Cause Excerpts, and direct `[Open Host]` shortcuts.
- Alert Bulk Dismissal & Auto-Resolution: `Dismiss All` action button (`POST /api/alerts/ack-all`) and automatic resolution of active alerts for a host when inspection verifies system returned to healthy desired state.
- Dual-Bucket Inspection Runs Retention: SQLite inline auto-pruning preserving up to 100 latest OK runs and 100 latest issue runs (`drift`, `down`, `error`, `degraded`) per host, guaranteeing long-term audit trail preservation without disk bloat.
- Segmented Audit Trail Sub-Tabs: interactive filter switcher in Workspace Modal Inspection Runs tab (`[ 📋 All History ]` vs `[ ⚠️ Last 100 Issues (Audit Trail) ]`) with auto-expanded diagnostic details and precise timestamps.
- Background Inspection Disable Switch: setting `poll_interval_sec` to `0` in Settings > General halts the background inspection worker for on-demand only operations, while preserving manual "Inspect" and "Inspect All" functionality.
- UI status indicators for paused background inspection: amber pause badge in the dashboard toolbar (`⏸️ Background Inspection Paused`) and engine status indicator in the application footer (`Engine Active (Inspection Paused)`).
- Automatic suppression of Stale Inspection warnings: suppresses false-positive `⚠️ Stale Data (>10m)` warnings across host cards, list views, and details when background inspection is intentionally disabled.

## [0.14.0] - 2026-09-22
### Added
- Instance Guard: single-instance protection mechanism using OS kernel advisory locks (`flock` on Unix / `LockFileEx` on Windows) bound to the database file, preventing duplicate workers, reporting active port & PID, and launching the active browser URL before exiting gracefully.
- Directory Filter: reactive real-time client-side search and category filtering (`All`, `Folders`, `Files`) in SFTP File Manager with keyboard shortcuts (`Escape` to clear).
- File Sorting: interactive column sorting (Name, Size, Modified) with visual direction indicators (`▲` / `▼`) and persistent `Folders First` hierarchy across folder navigation in SFTP File Manager.

## [0.13.0] - 2026-09-20
### Added
- Structured Inspection Runs: execution audit history tracking started timestamp, duration (ms), status, human-readable summary, and failure diagnostics.
- Automatic inline rolling prune maintaining strict 100 runs limit per Host in SQLite without background scheduler overhead.
- Inspection Runs history tab in Host Detail Workspace Modal with interactive status badges and diagnostic expanders.
- Real-time execution roundtrip duration display (`last_duration_ms`) on Host cards, list view, and overview detail.
- Anti-flood inspection cooldown: 15-second minimum wait window per host for manual inspections returning `HTTP 429 Too Many Requests`.
- Explicit in-flight concurrency guard returning `HTTP 409 Conflict` when an inspection is already actively running on the target host.
- Stale and hung network mount (NFS/CIFS) protection isolating local partitions (`df -lPk /`) and enforcing 5s hard timeouts on disk checks.
- Bounded concurrency worker pool (maximum 5 concurrent hosts) and global in-flight lock for mass inspection (`Inspect All`).
- Enforced 45-second total hard timeout budget per inspection run.
- In-app Changelog viewer integrated into Documentation Modal.
- Footer version link and Settings update card link to view Changelog.
### Changed
- Polished dashboard header with responsive Inspect All button layout and distinct amber warning toast styling.

## [0.12.1] - 2026-09-18
### Fixed
- Terminal preset scope host binding and modal layering z-index.
- Synchronized bilingual documentation and user guides with v0.12 architecture.

## [0.12.0] - 2026-09-18
### Added
- Hardware specs and capacity metrics: CPU cores, load average, absolute RAM (GB/MB), Swap capacity, and root disk capacity.
- Terminal Header Launcher: global action button in header with real-time reactive active shell session count badges.
- Reactive host session badges on host cards.

## [0.11.0] - 2026-09-18
### Added
- Multi-arch Docker images (`linux/amd64` and `linux/arm64`) automatically built and pushed to GitHub Container Registry (GHCR).
### Changed
- Inspector I/O optimization: removed heavy periodic recursive `du` scanning to prevent disk thrashing.
- Added SSH command execution timeout guards and inspection concurrency guard to eliminate inspection stampedes.

## [0.10.0] - 2026-09-17
### Added
- Application version display on initial setup and login screens.
### Changed
- Reorganized Settings dialog layout for clearer navigation.

## [0.9.1] - 2026-09-17
### Fixed
- Release test update pipeline and verification assets.

## [0.9.0] - 2026-09-17
### Added
- Semi-automatic Update Checker and in-place Self-Update with SHA-256 integrity verification.
- Release checksums generation via GitHub Actions.

## [0.8.0] - 2026-09-17
### Added
- Mass host inspection ("Inspect All") for asynchronous bulk health refreshes.
- Multi-instance snapshot file path isolation.
- Windows executable icon embedding and favicon.
### Changed
- In-memory gzip compression and ETag conditional validation for web assets.

## [0.7.0] - 2026-09-16
### Added
- Airgapped TOTP two-factor authentication (2FA) with emergency recovery codes and CLI bypass flag.
- Global multi-tab Terminal Dock with collapsible floating dock bar.
- Embedded in-app Documentation Modal for airgapped bilingual guides (README and User Guide).
- Transparent 6-factor Lifecycle Assessment, productive lifespan estimation, and hardware refresh recommendations.
### Security
- Self-contained web assets with strict Content Security Policy (CSP), eliminating all external CDN dependencies.

## [0.6.0] - 2026-09-09
### Added
- Host grouping by logical environment/function.
- Terminal maximize mode for full-viewport shell observability.
- Port auto-scan during server startup to automatically select next available port if default is busy.
- CLI startup banner and `-v` version flag.
- Toast notifications and Promise-based confirmation dialogs replacing native browser popups.

## [0.5.1] - 2026-09-09
### Added
- Automated Windows binary release builds (`amd64` and `arm64`).

## [0.5.0] - 2026-09-09
### Added
- Bilingual interface support (English and Indonesian).
- Windows portable server compatibility with POSIX remote path enforcement.
- Encrypted System Snapshot backup (AES-256-GCM) with optional GitHub Private Repository sync.
- Terminal Presets for reusable quick command shortcuts.
- Protected filesystem paths prevention in file manager.
- Host notes, global operational notes, and custom manual host ordering.

## [0.4.0] - 2026-09-09
### Added
- Legacy Linux server compatibility (CentOS 6, Debian 7, pre-systemd environments) and universal SSH key support.

## [0.3.0] - 2026-09-09
### Added
- Agentless Network & Security Observability: bandwidth rate, egress latency, socket connection profiling, and port exposure auditing.

## [0.2.0] - 2026-09-08
### Added
- Piped cross-host direct file/directory transfers without intermediate disk caching.
- On-the-fly streaming folder downloads (`zip` / `tar.gz`).

## [0.1.0] - 2026-09-08
### Added
- Initial release of Pantau: agentless SSH server monitoring and management system.
- Desired state verification, automated drift detection, root cause excerpts, and terminal file manager.
