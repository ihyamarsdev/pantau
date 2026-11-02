# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]
### Added
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
