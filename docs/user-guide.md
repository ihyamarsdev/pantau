# Pantau Administrator Guide & Operational Playbook

[**English**](user-guide.md) • [**Bahasa Indonesia**](user-guide.id.md)

Welcome to the **Pantau Administrator Guide**. This document provides an operational playbook designed for system administrators, DevOps engineers, and site reliability teams managing remote Linux infrastructure with Pantau.

Pantau operates **100% agentless** over standard SSH (`port 22`). It requires zero daemons or telemetry agents on target machines. All state evaluation, file streaming, and diagnostic extraction occur directly through standard POSIX utilities.

---

## 📑 Table of Contents

1. [Architecture & Trust Boundaries](#1-architecture--trust-boundaries)
2. [Phase 1: Onboarding & Inventory Management](#2-phase-1-onboarding--inventory-management)
   - [Adding a Host & Key Provisioning](#adding-a-host--key-provisioning)
   - [Organizing with Host Groups & Manual Host Order](#organizing-with-host-groups--manual-host-order)
   - [macOS Hosts — Prerequisites and What is Measured](#macos-hosts--prerequisites-and-what-is-measured)
3. [Phase 2: Desired State Baselines & System Guardrails](#3-phase-2-desired-state-baselines--system-guardrails)
   - [Capturing a 1-Click Desired State Baseline](#capturing-a-1-click-desired-state-baseline)
   - [Configuring Backup Freshness & Protected Paths](#configuring-backup-freshness--protected-paths)
4. [Phase 3: Observability, Drift Triage & Incident Response](#4-phase-3-observability-drift-triage--incident-response)
   - [Inspection Lifecycle, Mass Inspection (Inspect All) & Recency](#inspection-lifecycle-mass-inspection-inspect-all--recency)
   - [Resource Metrics, Process Telemetry & Hardware Observability](#resource-metrics-process-telemetry--hardware-observability)
   - [Package Inventory & Software Hygiene Audit](#package-inventory--software-hygiene-audit)
   - [Host Security Scan & Hardening Checks](#host-security-scan--hardening-checks)
   - [Detecting Drift & Root Cause Excerpts](#detecting-drift--root-cause-excerpts)
   - [Interactive Diagnostics via Terminal Dock & Terminal Presets](#interactive-diagnostics-via-terminal-dock--terminal-presets)
   - [In-App Activity Console & Live Telemetry Stream](#in-app-activity-console--live-telemetry-stream)
   - [Agentless Network Diagnostics & Hop Analysis](#agentless-network-diagnostics--hop-analysis)
   - [Evaluating Server Longevity with Lifecycle Assessment](#evaluating-server-longevity-with-lifecycle-assessment)
   - [Multi-Host Side-by-Side Comparison & Scoring Matrix](#multi-host-side-by-side-comparison--scoring-matrix)
5. [Phase 4: File Operations & Cross-Host Data Transfer](#5-phase-4-file-operations--cross-host-data-transfer)
   - [Cross-Host Transfer (Zero-Disk Memory Pipes)](#cross-host-transfer-zero-disk-memory-pipes)
   - [On-The-Fly Folder Streaming Download](#on-the-fly-folder-streaming-download)
   - [File Pinning with Notes & Global Bookmarks](#file-pinning-with-notes--global-bookmarks)
6. [Phase 5: Hardening, Snapshots & Disaster Recovery](#6-phase-5-hardening-snapshots--disaster-recovery)
   - [Two-Factor Authentication (2FA) & Emergency Bypass](#two-factor-authentication-2fa--emergency-bypass)
   - [Encrypted System Snapshots & GitHub Disaster Recovery](#encrypted-system-snapshots--github-disaster-recovery)
7. [Phase 6: Maintenance & Updates (Update Checker & Self-Update)](#7-phase-6-maintenance--updates-update-checker--self-update)
   - [Update Checker & Release Discovery](#update-checker--release-discovery)
   - [One-Click Self-Update & SHA-256 Verification](#one-click-self-update--sha-256-verification)
   - [Docker Container vs Standalone Environments](#docker-container-vs-standalone-environments)
   - [Airgapped Deployments](#airgapped-deployments)

---

## 1. Architecture & Trust Boundaries

Pantau executes remote commands and inspects telemetry via standard SSH connections.

- **Control Plane**: A single Go binary running on your management machine or bastion host, backed by an embedded SQLite database (`pantau.db`) in WAL mode.
- **Remote Host**: Any physical or virtual Linux machine (Ubuntu, Debian, RHEL, CentOS 6–9) accessible via SSH.
- **Trust Boundary**: Pantau uses an RSA 4096-bit keypair generated on first startup as its global default key, and also supports dedicated keys registered in the **SSH Key Library** or custom keys per host. Target hosts must grant access to the relevant public key inside `~/.ssh/authorized_keys` for the designated management user (`root` or a dedicated user with passwordless `sudo`).

```
+-------------------+             SSH Port 22 (Agentless)             +-----------------------+
|  Pantau Control   | ==============================================> |   Remote Linux Host   |
|  Plane (Go + DB)  | < - - - - - - - - - - - - - - - - - - - - - - - |  (POSIX df, ps, logs) |
+-------------------+          Actual State & Telemetry               +-----------------------+
```

---

## 2. Phase 1: Onboarding & Inventory Management

### Adding a Host & Key Provisioning

When connecting a new **Host** to Pantau:

1. Click **Add Host** on the main dashboard to open the **Action Dialog**.
2. Enter the connection parameters:
   - **Label**: A distinct identifier (e.g., `prod-db-primary`).
   - **IP / Hostname**: Resolvable hostname or IPv4/IPv6 address.
   - **SSH Port**: Standard port `22` or custom SSH daemon port.
   - **SSH User**: `root` or a sudo-privileged user.
3. Choose the authentication method:
   - **SSH Key Library (Dropdown)**: Select a pre-registered private key from the library (managed via **Settings** → **SSH Keys**). Recommended when managing clusters or environments sharing a common keypair.
   - **Custom Private Key**: Paste a host-specific OpenSSH private key directly if not using the library.
   - **Global SSH Key**: Leave unassigned to use Pantau's universal default SSH key.
   - **Key Provisioning (One-Time Password)**: If onboarding a server without pre-configured keys, enter the root/sudo password once in the provisioning field. Pantau connects via SSH, appends the corresponding public key (the selected Library Key's derived public key, or the universal global public key) to `~/.ssh/authorized_keys` idempotently, and immediately discards the password from memory.
4. Click **Save & Test Connection**. Pantau performs an initial handshake to verify connectivity.

### Importing Hosts from an SSH Config

To onboard many hosts at once, click **Import** next to **Add Host**:

1. Provide the config: paste your `~/.ssh/config`, click **Choose File**, or click **Read Server ~/.ssh/config** to read the file of the user running Pantau (not available inside Docker unless the file is mounted). Then click **Parse**.
2. Review the preview. Each concrete `Host` alias becomes one host with its `HostName` (defaults to the alias), `User` (defaults to `root`) and `Port` (defaults to `22`). Wildcard (`Host *`) blocks are never imported but supply defaults, exactly like OpenSSH. Entries already in Pantau, or repeating an earlier address/port/user in the same file, are greyed out and cannot be selected.
3. Click **Import Selected**. Hosts are created without a group, using the **Global SSH Key**; an `IdentityFile` in the config is not imported (a 🔑 hint marks such hosts, so assign a Library Key afterwards if needed).
4. In the **Set Group** form that follows, type a Host Group per host (existing groups are suggested) and click **Save Groups**. Leave a field empty to keep that host ungrouped; **Skip** closes the form and groups can still be set later from each host's edit dialog.

> [!NOTE]
> `Include` and `Match` directives are not followed or evaluated. Pantau connects directly, so `ProxyJump`/`ProxyCommand` settings are ignored.

### Key Resolution Hierarchy & Precedence

When authenticating against a host for background inspection, interactive terminal sessions, SFTP file operations, port tunneling, or network diagnostics, Pantau resolves SSH credentials in strict deterministic order:

1. **Library Key (`key_id > 0`)**: If a registered key from the SSH Key Library is assigned to the host, Pantau loads and authenticates using that private key. If the referenced `key_id` no longer exists in the library, **Pantau aborts immediately with an explicit error**—it never falls back silently to other keys.
2. **Custom Key (`custom_key`)**: If no library key is selected and a host-specific private key is stored, Pantau uses that custom key. When a host is updated to use a Library Key (`key_id > 0`), any previously stored `custom_key` is automatically cleared.
3. **Global SSH Key (`settings.ssh_private_key`)**: If neither a library key nor a custom key is assigned (`key_id == 0` and `custom_key` is empty), Pantau defaults to the system's global private key.

> [!TIP]
> The **Inject Key** action in the host Workspace Modal follows the same logic: if a Library Key is assigned, it injects that key's derived public key; otherwise, it injects the global default public key.

> [!NOTE]
> For legacy Linux systems (CentOS 5/6, Debian 7, or systems running OpenSSH 4.x/5.x), Pantau automatically enables legacy cipher and host key fallbacks (`aes128-cbc`, `3des-cbc`, `diffie-hellman-group1-sha1`, `ssh-dss`, `ssh-rsa`) during the SSH handshake. Pantau applies TOFU (Trust-On-First-Use) host key verification, seamlessly recording the target host's fingerprint on initial connection while actively protecting subsequent sessions against Machine-in-the-Middle (MITM) tampering.

### Organizing with Host Groups & Manual Host Order

As your fleet grows:
- **Host Group**: Categorize hosts by functional tiers or environments (e.g., `Database Cluster`, `Edge Proxies`, `Staging`). Groups render as distinct visual partitions in both Grid and List **View Mode**.
- **Manual Host Order**: Click and drag or prioritize mission-critical hosts to the top of the dashboard.
- **Host Note & Global Note**: Use **Host Note** for server-specific maintenance checklists (e.g., `"Primary PostgreSQL; failover standby is db-02"`). Use **Global Note** for cluster-wide operational announcements and shift handovers.

### macOS Hosts — Prerequisites and What is Measured

Pantau provides first-class, agentless observability for **macOS (Darwin)** hosts (such as Apple silicon Mac mini, Mac Studio, and Mac Pro servers) over standard OpenSSH connections.

#### Host Detection & Compatibility Scope
- **Automatic Detection**: Pantau identifies Darwin systems at runtime via `uname -s`. When `Darwin` is detected, Pantau automatically executes tailored macOS telemetry and diagnostic commands while preserving standard Linux routines byte-for-byte unchanged.
- **Compatibility Scope**: Operates using standard POSIX tooling; verified on **macOS 26 Tahoe on Apple silicon** (validated against real-world captures from Apple M4 Pro and M3 Ultra machines). Intel Macs and legacy OS releases operate via standard POSIX fallbacks without specialized kernel guarantees.

#### Prerequisites & Permissions
1. **Remote Login**: Enable SSH access under **System Settings > General > Sharing > Remote Login**.
2. **SSH Credentials**: Connect using an administrative or standard macOS user account with an SSH key (managed via the SSH Key Library or host-specific keys).
3. **Sudo Privileges**: Pantau inspects telemetry, runs diagnostics, and evaluates security posture **completely unprivileged (zero sudo required)**. Passwordless sudo (`sudo -n`) is **only** required if you use **Elevated File Access** in the File Manager (`sudo -n -u <user> /usr/libexec/sftp-server`).
4. **Extended Tooling PATH**: macOS non-interactive SSH sessions default strictly to `/usr/bin:/bin:/usr/sbin:/sbin`. Pantau automatically prepends an extended PATH prelude:
   ```bash
   case "$(uname -s)" in Darwin) PATH="$PATH:/usr/local/bin:/opt/homebrew/bin:/opt/homebrew/sbin:/Applications/Docker.app/Contents/Resources/bin"; export PATH;; esac;
   ```
   This ensures Homebrew CLI tools and Docker Desktop binaries are discoverable without requiring manual profile hacking.

#### Collected Metrics & Measurement Definitions
- **RAM Usage**: Total physical memory is retrieved via `sysctl -n hw.memsize`. Used memory strictly adheres to Apple's **Activity Monitor "Memory Used"** calculation:
  $$\text{Memory Used} = (\text{Anonymous pages} - \text{Pages purgeable} + \text{Pages wired down} + \text{Pages occupied by compressor}) \times \text{pagesize}$$
  The memory page size is extracted dynamically from `vm_stat` (16,384 bytes on Apple silicon).
- **Swap Memory**: Extracted directly from `sysctl vm.swapusage` (`total`, `used`, `free`), properly parsing MB/GB/KB unit designations.
- **Disk Capacity**: On modern macOS systems, `/` is a read-only sealed system volume (~12 GB). Pantau queries the shared APFS container via `df -lPk /System/Volumes/Data` (with `/` fallback) and computes:
  $$\text{Used} = \text{Total} - \text{Available}$$
  This guarantees reported disk usage accurately reflects actual user and application data storage.
- **Network Totals & Throughput**: Parsed from `netstat -ibn` rows matching `<Link#`. Only physical interfaces matching `en[0-9]+` are accumulated; virtual bridges, tunnels, loopbacks, and internal interfaces (`lo`, `bridge`, `utun`, `awdl`, `anpi`, `gif`, `stf`) are filtered out to prevent artificial double-counting.
- **Disk I/O Rate**: Extracted from IOKit registry driver metrics via `ioreg -rd1 -c IOBlockStorageDriver`, parsing and summing cumulative `Bytes (Read)` and `Bytes (Write)` across active block storage drivers.
- **CPU Cores & Hardware Model**: Core counts are read via `sysctl -n hw.ncpu` (combining performance and efficiency cores). Hardware model and chip details are captured via `system_profiler SPHardwareDataType` (e.g. `Apple Mac Studio (Mac15,14) - Apple M3 Ultra`), with graceful fallback to `sysctl -n hw.model` / `machdep.cpu.brand_string`.
- **OS Version & Lifecycle EOL**: Extracted from system properties and mapped to official marketing names (macOS 11 Big Sur, 12 Monterey, 13 Ventura, 14 Sonoma, 15 Sequoia, 26 Tahoe). Server lifecycle follows Apple's standard **current + 2 prior major releases** policy (macOS 26, 15, and 14 supported; macOS 13 and older flagged as EOL). Installation birthtime is captured via `stat -f %B /var/db/.AppleSetupDone`.
- **Privacy-Safe Process Telemetry (`ucomm`)**: Top CPU and Top RAM processes are collected using `ps -Aro pid,user,%cpu,%mem,rss,ucomm` and `ps -Amo ...`. Pantau strictly uses `ucomm` (process short name) instead of `comm` or `args`, ensuring confidential arguments, API keys, and `--token` parameters are never transmitted or stored.

#### Platform Differences & Known Boundaries
- **No Per-Process Swap**: macOS does not expose per-process virtual memory swap usage in `/proc`; swap columns for individual processes remain blank.
- **No Failed-Login Counter**: macOS lacks standard POSIX `/var/log/btmp` tracking; failed login telemetry is not assessed.
- **No Kernel I/O Error Detection Without Root**: Running `dmesg` requires root privileges on macOS; disk I/O error log inspection is omitted on non-root connections.
- **Docker Desktop Visibility**: Docker containers are visible if Docker Desktop is installed and the SSH user has permission to execute the `docker` binary.
- **Packages Tab**: The Packages tab remains intentionally empty on macOS. Pantau's package inventory engine audits native Linux system package managers (`dpkg`, `rpm`, `apk`, `pacman`). User-installed Homebrew formula/casks are not cataloged as core OS packages.

#### Network Diagnostics on macOS
- **Portable Timeouts**: Darwin environments do not bundle GNU `timeout`; Pantau utilizes portable subshell watchdogs and native command deadlines.
- **Ping Metrics**: Ping probes run with `-t <seconds>` (avoiding `-W` which specifies milliseconds on BSD ping). The parser seamlessly digests Darwin ping summaries (`round-trip min/avg/max/stddev`).
- **Native Traceroute**: Leverages the system's pre-installed `/usr/sbin/traceroute` directly. Traceroute is **never auto-installed** on macOS.

#### Desired State Rules on macOS
- **Disk Rules**: Evaluated against `/System/Volumes/Data` (or `/`).
- **Service Rules**: Validated against `launchd` service units via `launchctl list` or process presence via `pgrep`.
- **Port Rules**: Verified via native `lsof -iTCP -sTCP:LISTEN -P -n`.
- **Backup Freshness**: Validated using BSD timestamp extraction via `stat -f %m <path>` or `stat -f %B`.

#### Security Scan & Hardening Checks on macOS
- **Update Checks**: Inspects pending system patches via non-blocking `/usr/sbin/softwareupdate -l`.
- **Heuristic Categorization**: Updates labeled with `macOS` or `Safari` are classified as **Security Updates**; other pending packages are accounted for in the total upgradable count.
- **Honest Partial Status**: If Apple's software update catalog is unreachable or times out, Pantau reports an honest `partial` status rather than a misleading zero-update report.
- **Native Hardening Checks**:
  1. `sshd_permit_root_login`: Audits `/etc/ssh/sshd_config` and drop-ins.
  2. `sshd_password_auth`: Validates OpenSSH password authentication policy.
  3. `auto_security_updates`: Audits automatic update keys in `/Library/Preferences/com.apple.SoftwareUpdate` (`AutomaticDownload`, `AutomaticallyInstallMacOSUpdates`, `CriticalUpdateInstall`, `ConfigDataInstall`).
  4. `firewall_active`: Validates Application Firewall state via `socketfilterfw --getglobalstate` or system defaults.
---

## 3. Phase 2: Desired State Baselines & System Guardrails

Pantau does not merely display metrics; it enforces continuous adherence to your declared configuration baseline.

### Capturing a 1-Click Desired State Baseline

1. Open the host's **Workspace Modal** by clicking on its card.
2. Navigate to the **Desired State** tab.
3. Click **1-Click Baseline**:
   - Pantau triggers an immediate SSH command sequence to inspect running Docker containers, root disk partition, active database/web services, and configured cron jobs.
   - These discovered resources are saved as the host's authoritative **Desired State**.
4. Customize or add manual rules based on your operational needs.

### Desired State Rules Reference Table

Pantau supports 8 Desired State rule types evaluated on every inspection cycle:
| Kind | Target Identifier | Expected State | Examples | Drift Criteria & Root Cause Excerpt |
| :--- | :--- | :--- | :--- | :--- |
| **`container`** | Docker container name | `running` | `nginx` &rarr; `running` | Container exited, dead, or crash loop. Captures exit code, OOMKilled flag, and last 50 container log lines. |
| **`service`** | Linux daemon name | `active` | `mariadb` &rarr; `active` | Service inactive or failed. Captures last 50 log lines via `journalctl` or syslog. |
| **`port`** | TCP port or `ip:port` | `listening` (or `closed`) | `80` &rarr; `listening` | Port closed or listening daemon halted. Verified via `ss`/`netstat`. |
| **`disk`** | Partition mount point | `<N%` (usage ceiling) | `/` &rarr; `<85%` | Partition usage >= threshold or storage IO timed out after 5s (hung NFS protection). |
| **`cron`** | Keyword or command substring | `configured` | `backup.sh` &rarr; `configured` | Specified cron command not found in user's `crontab -l`. |
| **`backup`** | Absolute backup file path | `fresh_<N>h` (default: 24h) | `/backup/db.sql.gz` &rarr; `fresh_24h` | File is missing, 0 bytes empty, or modification time older than N hours. |
| **`process`** | Process name or pattern | `running` (or `stopped`) | `celery` &rarr; `running` | Process not currently active on system (verified via `pgrep`/`ps`). |
| **`security_updates`** | `security` (fixed) | `none` or `<=N` (ceiling) | `security` &rarr; `none` | Pending security updates exceed threshold N, no scan recorded, scan older than 72h, or status is `unsupported`/`error`. Evaluated from latest cached scan. |

### Configuring Backup Freshness & Protected Paths

- **Backup Freshness**: Define expected backup target paths (e.g., `/backup/postgres/daily.sql.gz`) and max age thresholds (e.g., must be updated within 24 hours and non-zero in byte size). Pantau marks a **Drift** if the backup file is missing, empty, or outdated.
- **Protected Path**: Protect critical operational directories (e.g., `/etc/`, `/boot/`, `/var/lib/docker/`) from inadvertent modification, deletion, or unauthorized file uploads via the integrated SFTP file manager.

---

## 4. Phase 3: Observability, Drift Triage & Incident Response

### Inspection Lifecycle, Mass Inspection (Inspect All) & Recency

Pantau collects actual state telemetry and validates desired state rules through periodic background cycles or on-demand triggers:

- **Mass Inspection (Inspect All)**: Click the `⚡ Inspect All` button in the header toolbar to trigger asynchronous, concurrent inspections across all configured hosts. Bulk executions are queued through a bounded worker pool (maximum 5 concurrent workers) with an anti-flood cooldown to prevent bandwidth saturation and CPU spikes.
- **Per-Host Immediate Inspection**: Click the quick `⚡` inspect button on any host card or list row, or click the compact refresh icon in the Host Detail modal header (next to the status badge).
- **Anti-Flood Inspection Cooldown**: To protect remote hosts from connection flooding, manual inspections enforce a 15-second per-host cooldown. Triggering inspections during this window returns `HTTP 429 Too Many Requests` and displays an amber toast notification indicating remaining cooldown seconds.
- **In-Flight Concurrency Guard**: An active in-flight guard locks ongoing inspections per host. Attempting to inspect a host while an inspection is already actively processing returns `HTTP 409 Conflict` and triggers a spinning state on the action button until execution finishes.
- **Structured Inspection Runs & Execution History**: Every inspection automatically persists an audit record in the `inspection_runs` SQLite store (started timestamp, roundtrip duration in milliseconds, status badge, summary, and root cause diagnostic excerpts). Operators can review historical execution traces under the **Riwayat Inspeksi (Inspection Runs)** tab in the Host Detail Workspace Modal.
- **Automatic Inline Rolling Prune**: Zero-configuration inline pruning automatically maintains a strict 100 runs limit per host in SQLite, ensuring predictable database storage footprints without requiring background scheduler cron jobs.
- **Hung NFS & Network Mount Protection**: Disk evaluations strictly isolate local filesystems (`df -lPk /`) to prevent remote storage locks, and wrap partition checks in a 5-second execution timeout. Stale or unresponsive network storage mounts (NFS, CIFS) are flagged with actionable drift diagnostics instead of freezing the inspection worker.
- **Enforced Hard Timeout Budget**: Each host inspection cycle is constrained to an enforced 45-second total timeout budget. Stalled or hung SSH connections are terminated on timeout and marked down to preserve queue availability.
- **Inspection Recency & Roundtrip Duration Indicators**: Host cards and list rows show dynamic relative timestamps (e.g. `🕒 2m ago`, `🕒 just now`) alongside precise execution roundtrip duration badges (e.g. `⚡ 320ms`).
- **Stale Inspection Detection**: When a host fails inspection or telemetry remains unrefreshed beyond tolerance (> 10 minutes or 2× the normal interval), an amber `⚠️ Stale Data` warning alerts operators to potential SSH disconnection or unresponsive nodes. When Background Inspection is intentionally disabled (set to `0`), stale warnings are automatically suppressed to eliminate false alarms.
- **Dashboard Auto-Refresh**: The browser UI automatically synchronizes host metrics and status every 30 seconds without requiring manual page reload.
- **Configurable Inspection Interval & Disable Switch**: Administrators can adjust background SSH inspection frequency (default: 300 seconds / 5 minutes, minimum 30 seconds, or set to `0` to disable background inspection completely for on-demand manual inspection only) via **Settings** > **General** tab. When disabled, an indicator badge (`⏸️ Background Inspection Paused`) appears on the dashboard toolbar and engine footer.

### Resource Metrics, Process Telemetry & Hardware Observability

Telemetry collection continuously extracts granular system capacity:
- **Real-Time Disk I/O Rates (Speedometer)**: Evaluates dynamic disk read and write speeds (B/s) and cumulative byte counters from `/proc/diskstats`. Evaluates qualitative throughput load status pills (Optimal `< 10 MB/s`, Active `10–80 MB/s`, High I/O Load `≥ 80 MB/s`) on Host Cards and Host Detail modals.
- **Absolute Capacity Indicators**: RAM and Disk progress bars render inline `used / total` metrics (e.g. `4.2 GB / 15.8 GB`) without layout displacement.
- **Swap Telemetry**: A permanent Swap indicator row on each host card shows active memory paging, or displays a dimmed `Disabled` badge when swap is unconfigured on the machine.
- **Normalized CPU Load**: Load averages are evaluated against the target's physical and logical CPU cores (`nproc`), rendering color-coded threshold alerts when process run-queues exceed core capacity.
- **Sys Info Card**: The Host Detail modal presents hardware model, CPU core count, virtualization layer, and storage device topology.
- **Top Resource Consumers (Process Telemetry)**: Every routine inspection cycle extracts the top 5 resource-consuming processes across three dimensions:
  - **Top CPU**: Tracks PID, user, `%cpu`, and CPU execution time via `ps -eo pid,user,%cpu,cputime,comm --sort=-%cpu`.
  - **Top RAM (Physical RSS)**: Tracks PID, user, `%mem`, RSS memory footprint, and process name via `ps -eo pid,user,%mem,rss,comm --sort=-rss`.
  - **Top Swap (Virtual Memory)**: Conditionally extracted only when `SwapUsedBytes > 0` by parsing `VmSwap:` in `/proc/[0-9]*/status`, identifying virtual memory hogs without remote daemon overhead.
  - **One-Click Terminal Launcher**: The Top Resource Consumers card in the Overview tab provides an **Open Terminal** action button to immediately jump into an interactive troubleshooting session on the host.

### Package Inventory & Software Hygiene Audit

Pantau provides agentless software inventory visibility across multiple Linux distributions:
- **Polyglot Distro Support**: Native detection across Debian/Ubuntu (`dpkg-query`), RHEL/CentOS/AlmaLinux/Rocky (`rpm`), Alpine (`apk`), and Arch Linux (`pacman`).
- **Software Hygiene Categorization**:
  - **Manual Install (`⭐ Manual Install`)**: Packages installed explicitly by system administrators via package managers (e.g., `apt-mark showmanual`, `dnf history`, `/etc/apk/world`, `pacman -Qqe`), separating operational tools from base distributions.
  - **Base System**: Foundational OS bootstrap packages and core runtime libraries.
  - **Dormant Packages (`💤 Dormant Packages`)**: Manually installed software units whose corresponding `systemd` services are inactive, disabled, or failed during the observation period, pinpointing potential security attack surfaces or obsolete daemons.
- **Packages Workspace Tab**:
  - View package name, version, architecture, source repository, and installation timestamp.
  - Search packages instantly via the real-time filter bar.
  - Category filters for **Manual Install**, **Dormant Packages**, and **All Packages**.
  - **Rescan Packages**: On-demand manual package re-inspection with confirmation dialog, protected by a 30-second anti-flood cooldown timer (`HTTP 429 Too Many Requests`) and background concurrency lock (`HTTP 409 Conflict`).
  - **Non-Destructive Philosophy**: Pantau avoids destructive web-based package uninstalls that could trigger cascading dependency failures. Operators are encouraged to use the built-in Terminal Dock or establish Desired State rules for compliance.


### Host Security Scan & Hardening Checks

Pantau provides agentless, read-only security visibility across remote hosts, discovering pending security updates, reboot requirements, repository cache freshness, and 4 essential system hardening checks—operating 100% airgapped without third-party agents, without root/sudo privileges, and without outbound CVE database queries.

#### The Security Workspace Tab
Open any host's Workspace Modal and navigate to the **Security** tab:
- **Summary Metrics**:
  - **Pending Security Updates**: Total count of packages with unapplied security fixes.
  - **Total Pending Updates**: Total count of upgradable system packages.
  - **Severity Breakdown**: Color-coded badges categorizing updates into **Critical**, **Important**, **Moderate**, **Low**, and **Unknown** (Debian/Ubuntu packages are classified as `Unknown` because `apt` does not report upstream severities).
  - **Reboot Required**: Alerts whether the kernel or core libraries require a system restart (`/var/run/reboot-required` on Debian/Ubuntu or `needs-restarting -r` on RHEL families).
  - **Repo Cache Age**: Tracks when the local package manager metadata was last updated.
- **Security Findings Table**: Lists affected packages, currently installed versions, available fixed versions, severity badges, and security advisory IDs (e.g., `RHSA-*`, `ALAS-*`, or CVE numbers).
- **Hardening Checks**: Evaluates 4 read-only security configuration baselines:
  - `sshd_permit_root_login`: Evaluates OpenSSH root login policy across `/etc/ssh/sshd_config` and `/etc/ssh/sshd_config.d/*.conf` (first-match-wins). Reports `pass` for OpenSSH default (`prohibit-password`) or `no`, and `fail` if explicitly set to `yes`.
  - `sshd_password_auth`: Evaluates SSH password authentication. Reports `pass` if disabled (`no`), or `warn` if enabled (`yes`) or left at default.
  - `auto_security_updates`: Detects whether automated security updates are enabled (`unattended-upgrades` on Debian/Ubuntu, or `dnf-automatic` / `yum-cron` timer on RHEL).
  - `firewall_active`: Verifies active firewall status (`ufw`, `firewalld`, or `nftables`/`iptables` rules). If permissions prevent reading firewall tables, reports `unknown` with explanatory detail.
- **Coverage Notes**: Surfaces any environment limitations, permission barriers, or cache staleness notices.

#### Honest Coverage Principle
Pantau strictly enforces an **Honesty Rule**: a host is never presented as "clean" or "safe" without complete coverage.
- If an operating system is not supported for package vulnerability checks (such as Alpine Linux or Arch Linux), the scan reports `status: unsupported` with an explanatory note (hardening checks still run).
- If the update commands themselves are restricted by user permissions or if the local package cache is older than 7 days (> 168 hours), the scan reports `status: partial` with a notice that results may miss recent advisories.
- Hardening checks that cannot be evaluated (e.g. the firewall status needs root, or a config file is absent) are shown as `unknown` and listed in the scan notes; they do not change `status`, since the update data itself was fully collected.
- When the status is `unsupported` or `error`, the UI shows pending updates, severity breakdown and reboot state as **not assessed** instead of zero.
- Distros with legacy tools (CentOS 6/7, Debian 7+) are handled gracefully without failing the inspection cycle.

#### On-Demand Scanning & Cooldown Guard
- Click **Scan Security Now** inside the Security tab to trigger an immediate synchronous audit.
- **Cooldown & Concurrency Guards**: Manual scans enforce a 60-second cooldown timer (`HTTP 429 Too Many Requests` returning remaining seconds in `X-Scan-Cooldown-Remaining`) and an in-flight concurrency lock (`HTTP 409 Conflict`).

#### Background Scan Scheduling
- **Interval Setting**: Configure `security_scan_interval_hours` in **Settings** → **General** (default: `24` hours; accepts `1`–`720` hours).
- **Disable Background Scans**: Setting the interval to `0` disables automated background security scanning, allowing strictly on-demand manual audits.
- **Scheduler Worker**: A background worker runs every 10 minutes to scan hosts whose last scan exceeds the configured interval or has never run (clamped to a maximum of 2 concurrent scans, delayed ~2 minutes after startup).

#### Desired State Enforcement (`security_updates`)
Enforce security update compliance automatically through Desired State rules:
- **Rule Kind**: `security_updates`
- **Target**: `security` (fixed)
- **Expected State**: `none` or `<=N` (where `none` requires 0 pending security updates; `<=N` permits up to N updates).
- **Cache-Based Evaluation**: Evaluated against the latest stored scan in SQLite without executing live SSH commands during routine inspection cycles.
- **Compliance & Drift Conditions**:
  - `ok`: Pending security updates are within the allowed ceiling (`<= N`).
  - `drift`: Pending security updates exceed threshold `N` (root cause excerpts list top pending packages, versions, and advisories).
  - `drift`: The host has never been scanned (`not_scanned`), the scan data is older than 72 hours (`stale`), or the latest scan status is `unsupported` or `error`. Pantau will never claim compliance without fresh, verifiable data.

#### REST API Endpoints
All security endpoints require valid session authentication (`authMiddleware`):
- `GET /api/hosts/{id}/security`: Returns `{"scanned": false}` if no scan exists, or `{"scanned": true, "scan": <SecurityScan>}`. Emits `X-Scan-Cooldown-Remaining: <sec>` header when cooldown is active.
- `POST /api/hosts/{id}/security/scan`: Executes a synchronous security scan. Returns `200 OK` with updated scan results, `429 Too Many Requests` if cooldown is active, `409 Conflict` if a scan is already running, or `500 Internal Server Error` on connection failure (without overwriting previous scan data).

#### Operational Boundaries & Airgapped Design
- **Zero Sudo / Zero Agent**: Read-only collection using standard SSH user permissions. Never runs `sudo`, never installs packages, and never updates package caches (`apt-get update` / `dnf makecache`).
- **No External Vulnerability Database**: Fully compatible with airgapped environments (ADR-0013). Operates strictly on upstream package manager metadata. External CVE database matching is not performed in Phase 1 (any future CVE feed integration would be an optional future opt-in).
- **Ephemeral Telemetry**: Scan results are persisted in the `security_scans` SQLite table and excluded from encrypted System Snapshots as reproducible telemetry.

### Detecting Drift & Root Cause Excerpts

During every scheduled **Inspection** cycle (default: every 5 minutes) or upon manual trigger:

1. Pantau compares the remote **Actual State** against the defined **Desired State**.
2. If any discrepancy exists (e.g., a critical container exited, or disk capacity exceeded its threshold), the host status changes to **Drift**.
3. **Automated Root Cause Excerpt**: Pantau immediately collects diagnostic evidence without administrator intervention:
   - Container exit code and `OOMKilled` status flag.
   - Last 50 lines of crash output from `docker logs --tail 50 <container>` or `journalctl -u <service> -n 50 --no-pager`.
4. The **Root Cause Excerpt** is rendered directly in the **Workspace Modal** incident timeline and dispatched to configured **Notification Channels** (Telegram bot / Webhooks).

### Interactive Diagnostics via Terminal Dock & Terminal Presets

When a drift or alert requires hands-on investigation:

- **Terminal Dock**: Open a multi-tab interactive shell powered by `xterm.js` over WebSocket SSH PTY sessions.
- **Terminal Header Launcher**: Access and toggle the global dock from the application header. A reactive session counter badge tracks all active PTY sessions system-wide. When empty, clicking the launcher presents a host selector dropdown to open new sessions instantly.
- **Reactive Host Session Badges**: Host cards in Grid and List views show contextual session count badges whenever active shells are connected to that target.
- **Split-Pane View (`Alt+\`)**: Split the terminal view to compare logs or configurations across two hosts side by side in real time.
- **Terminal Presets & Live Search**: Execute pre-configured one-click diagnostic commands (e.g., `htop`, `journalctl -f`, `docker stats`) bound globally or scoped to the active terminal tab's host context. Includes real-time search filtering in the presets dropdown matching by title, command, or tmux session tag (`t:tag`).
- **Terminal Pinned Paths Quick-Access (`📌`)**: Access pinned files and directories for the active host directly from the terminal toolbar. Features real-time search across paths and operational notes. Clicking a directory runs `cd '<path>'`; clicking a file injects its quoted path directly into the terminal prompt. Unpin items inline via the `✕` button.
- **Direct Switch to File Manager (`📁 Files`)**: Seamlessly jump from the active terminal tab to the Host File Manager focused on the current working directory while minimizing the terminal dock.
- **Font Zoom Controls**: Quick adjust terminal text scaling directly from the toolbar (`+`, `-`, and `0` to reset).
- **Interactive Terminal Links (`Ctrl+Click` / `Cmd+Click`)**: URLs appearing in terminal output are automatically detected. Pressing `Ctrl+Click` (Linux/Windows) or `Cmd+Click` (macOS) opens public websites directly in a new browser tab without breaking standard drag-to-select and copy operations.
- **Remote Web Port Tunneling (`localhost:port`)**: When a terminal command prints a remote loopback address (e.g. `http://localhost:3000` or `127.0.0.1:8080`), clicking it triggers a dedicated confirmation dialog (*Remote Web Service*) offering to open the service via Pantau's SSH direct-tcpip reverse proxy (`/api/tunnel/:hostId/:port/`) in a new browser tab.
- **Toolbar Ports Dropdown (`🌐 Ports`)**: Access private web applications running on the target host by opening the `🌐 Ports` dropdown on the terminal toolbar. Enter any remote port number (1-65535) and click **Buka** to launch a reverse proxy tunnel session. Frequently accessed ports are saved automatically to local storage for quick access.
- **Zero-Configuration Remote Browser Bridge**: CLI utilities running on remote servers (`gh auth login`, `xdg-open`, Python `webbrowser`, AWS SSO, `npm login`) automatically launch real desktop browser tabs on your client computer via terminal escape protocols (OSC 1337 / OSC 777), completely eliminating unwanted console text-browsers (`w3m`/`lynx`).
- **Working Directory Synchronization**: Synchronizes active working directory via shell window titles and OSC 7 ANSI escape sequences.
- **Modal Stacking & Escape Key**: Child action dialogs (preset forms, rule modals, notes) dynamically stack above parent modals and maximized terminal docks. Pressing `Escape` hierarchically dismisses the top-most active dialog before passing keyboard input to lower layers.

### In-App Activity Console & Live Telemetry Stream

To provide transparent operational feedback, Pantau includes a real-time diagnostic console that exposes what the daemon and its subsystems are doing at any given moment:

- **Monitored Subsystems**: Emits structured telemetry across 7 core modules:
  - `inspect`: Host inspection cycles, roundtrip latencies, healthy status, down states, and detected drift summaries.
  - `ssh`: Key provisioning attempts, runner initializations, and connection errors.
  - `transfer`: SFTP file uploads and background cross-host transfer progress.
  - `terminal`: Opening, resizing, and closing of interactive PTY shell sessions.
  - `snapshot`: Automated encrypted database backups and GitHub synchronizations.
  - `system`: Daemon startup, port auto-scanning, instance locking, and graceful shutdown.
  - `auth`: Administrator login attempts, credential validations, and 2FA verifications.
- **Toggling the Activity Console**: Click the `⚡ Activity` button on the footer status bar, or press the global keyboard shortcut **`Ctrl + \``** (backtick) from anywhere in the application.
- **Status Indicators & Unread Badges**: The footer button features a pulsing green dot when connected to the live stream. If the console is closed when warnings or errors occur, an unread error badge (`99+`) highlights new issues until the drawer is opened.
- **Smart Auto-Scroll & Freeze Control**: New logs stream continuously to the bottom of the table. Scrolling up automatically pauses auto-scrolling so you can inspect historical records without view jumping (*Smart Auto-Scroll*). You can also click **Pause / Resume** to explicitly freeze the feed.
- **Severity Level & Instant Search Filters**: Filter visible entries by severity (`ALL`, `ERROR`, `WARN`, `INFO`), or use the search box to filter across log messages, hostnames, and module names in real time.
- **Click-to-Inspect Metadata**: Clicking any table row expands its structured attribute metadata (`duration_ms`, `port`, `drift_rules`, error details) in a formatted JSON card.
- **Exporting Logs**: Click **Export** to instantly download the captured events as `.log` (plain text) or `.json` for debugging, external auditing, or incident reports.
- **Security & Redaction Filter**: Sensitive values—including SSH passwords, private keys, authentication tokens, session cookies, and 2FA secrets—are deterministically sanitized into `[REDACTED]` before leaving the server.
- **Zero-Waste Performance Throttling**: When the Activity Console is closed, DOM rendering operations are 100% bypassed to eliminate unnecessary CPU usage and browser layout recalculations.

### Agentless Network Diagnostics & Hop Analysis

When troubleshooting server connectivity issues, firewall blocking, or cross-datacenter routing anomalies:

- **Diagnostic Modes**: Execute non-destructive diagnostics directly from the remote host:
  - **Ping**: Probes host reachability and roundtrip times via `timeout 14 ping -c 4 -W 2 <target>`.
  - **Traceroute**: Inspects network route hops with `traceroute` (preferred, streams hop by hop). If it is missing on the source host, Pantau installs it first (apt, dnf, yum, apk, pacman, zypper; apt runs without restarting services) and streams the installer output to the raw viewer before probing. Installing requires the SSH user to be root or have passwordless `sudo`; otherwise Pantau falls back to `tracepath` (run line-buffered via `stdbuf -oL`) and notes the fallback in the raw viewer. If neither tool exists and installation is impossible, the probe stops with a message telling you what to install manually.
  - **Port Probe (TCP)**: Tests TCP port connectivity via native bash socket `/dev/tcp/<target>/<port>`, bypassing the need for `nc`, `telnet`, or `nmap`.
  - **DNS Resolve**: Verifies DNS resolution path and name resolution latency via `getent hosts` and `nslookup`.
- **Intelligent Hop Analysis**: Automatically parses traceroute hops into structured topology cards:
  - **Zone Classification**: Identifies whether each hop belongs to `Local/Private VPC` (RFC 1918 / CGNAT), `ISP Transit`, or `Destination`.
  - **Blackhole Detection**: Pins down the exact network hop where traffic drops if destination is unreachable.
  - **Latency Spike Alerts**: Highlights significant latency jumps (>60ms) between consecutive hops.
- **Dual Launch Points**:
  - Open from the **Network Diagnostics** tab in any Host Workspace Modal (with source host locked).
  - Open globally via the **`⚡ NetDiag`** button in the top navigation header cluster (with host picker).
- **Concurrency & Safety Guards**: Employs an in-memory singleflight lock per host to prevent SSH session saturation. Offline hosts are automatically disabled in inter-host target dropdowns.

### Evaluating Server Longevity with Lifecycle Assessment

Pantau continuously calculates an auditable **Lifecycle Score** (0–100) based on six weighted factors:

1. **OS End-of-Life (EOL)**: Checks OS distribution release and kernel version against official vendor support dates.
2. **Productive Lifespan**: Assesses hardware age against standard industry amortization (3–5 years) using bare-metal BIOS release dates or cloud VM deployment timestamps.
3. **RAM Pressure**: Sustained memory exhaustion and swap thrashing.
4. **CPU Core-to-Load Saturation**: Load averages normalized against physical/virtual core counts.
5. **Disk Partition Capacity**: I/O saturation and storage exhaustion risk.
6. **Kernel I/O & Hardware Errors**: Scans kernel ring buffers (`dmesg`) for disk sector faults, filesystem remounts (read-only), or hardware resets.

When a host drops into degraded lifecycle thresholds, Pantau generates concrete **Hardware Refresh** justifications for capacity planning and hardware replacement.

### Multi-Host Side-by-Side Comparison & Scoring Matrix

When evaluating infrastructure performance discrepancies, selecting optimal hosts for workload migrations, or auditing cluster consistency:

- **Interactive Comparison Matrix**: Open via the **`⚖️ Compare`** button in the global header or within any Host Workspace Modal. Select 2 to 4 hosts simultaneously to inspect high-density comparative telemetry inside a generous modal view (75vw × 80vh).
- **Standardized 4-Dimension Scoring**:
  - **Compute Capacity & Load**: Physical/virtual cores, clock speeds, architecture, and core-normalized load saturation.
  - **Storage & Disk I/O**: Partition utilization, filesystem health, and active I/O speed.
  - **Network Health**: Latency jitter, packet reliability, and reachability.
  - **System Hygiene & Lifecycle**: Configuration drift status, dormant package accumulation, uptime longevity, and lifecycle score.
- **5% Deadband Noise Filter**: Minor differences within a ±5% margin are treated as ties to eliminate metric jitter and prevent false winner rankings. Stale or unreachable hosts receive soft score penalties.
- **Executive Decision Narrative**: Automatically synthesizes human-readable operational takeaways, highlighting category champions, recommended migration destinations, and warning about saturated hosts.
- **Markdown Export**: Copy formatted Markdown scoreboard with full standings and executive takeaways to clipboard with one click (supported even in plain HTTP LAN environments via standard fallback).

---

## 5. Phase 4: File Operations & Cross-Host Data Transfer

### Cross-Host Transfer (Zero-Disk Memory Pipes)

Transferring large databases, directory trees, or archive bundles between two remote hosts typically requires intermediate storage or manual `scp` setup. Pantau streams data across hosts **without writing any intermediate bytes to Pantau's local disk**.

```
+-------------------+        Piped Stream (In-Memory Relay)         +-------------------+
|  Remote Host A    | ============================================> |  Remote Host B    |
|  (Source: tar)    |                 via Pantau                    |  (Dest: untar)    |
+-------------------+                                               +-------------------+
```

1. Open the file explorer in the **Workspace Modal** of the source Host.
2. Select the file or folder and click **Cross-Host Transfer**.
3. Select the target destination Host and destination path.
4. Choose the transfer mode:
   - **Fast Stream Mode**: High-throughput in-memory piping using standard `tar` streams.
   - **Verified Mode**: Calculates and validates SHA256 hashes end-to-end on both source and destination hosts before concluding the transfer.
5. Track progress in the background **Transfer Job** queue (real-time MB/s throughput, transferred bytes, and ETA).

### On-The-Fly Folder Streaming Download

To download an entire remote folder structure to your local workstation:
- Select the folder in the SFTP manager and click **Download as Archive**.
- Pantau streams the compressed archive on-the-fly (`zip` if installed on remote host, or `tar.gz`) directly to your browser's HTTP download stream without creating temporary files on the Pantau server disk.

### Safe File & Folder Duplication (Disk Space Safety Check)

Pantau allows immediate in-place duplication of files or folders on the remote server without downloading or re-uploading bytes:

1. Click the **📋 Duplicate** action button on any file or folder row.
2. The modal pre-fills a timestamped target name:
   - For files with extensions: `<filename>_<YYYYMMDDHHmmss>.<ext>` (e.g., `nginx_20261002043000.conf`), keeping file extensions and syntax highlighters intact.
   - For directories or extensionless files: `<dirname>_<YYYYMMDDHHmmss>`.
3. **Disk Space Safety Check**: Pantau automatically checks the available free space on the destination partition via `df -PB1`. If available space is less than the item size plus a 100 MB safety buffer, the duplication action is blocked with an error banner to protect the host from disk exhaustion.
4. **Collision Prevention**: If a file or folder with the target name already exists, Pantau strictly refuses to overwrite it to prevent accidental data loss.
5. **Native Execution**: Cloning runs on the remote host via `cp -a`, preserving original Unix file modes, permissions, and timestamps. Items $\ge$ 500 MB display a warning badge indicating the background copy may take a few seconds.

### Interactive Breadcrumb Navigation & Polish

The SFTP File Manager includes enhanced navigation and visual ergonomics:
- **Interactive Breadcrumb Bar**: Click any folder segment in the breadcrumb path (e.g., `📁 /` &rarr; `var` &rarr; `www` &rarr; `html`) to jump directly to any parent directory without pressing "Up" repeatedly.
- **Contextual File Icons**: File rows display type-aware icons based on extension (🐚 scripts, ⚙️ configs/json/yaml, 📝 logs/text, 🗜️ archives, 🖼️ media, 💻 source code).
- **Sticky Table Header**: Column headers and sort controls remain visible while scrolling through long directories.

### Code Editor Maximize & VS Code Dark+ Theme

When editing scripts, configurations, or environment files in the integrated CodeMirror editor:
- **Workspace Modal Maximize**: Click the `⛶` maximize button or double-click the modal header to expand the editor into a full-viewport canvas (100vw &times; 100vh) for distraction-free code review.
- **VS Code Dark+ Palette**: Features an embedded modern dark theme (`#1e1e1e` background, high-contrast syntax highlighting for keywords, functions, variables, and strings) with monospace font styling (`JetBrains Mono`, `Fira Code`, `Consolas`).
- **Dynamic Syntax Detection**: Automatically sets syntax highlighting mode for PHP, JavaScript, Shell, YAML, CSS, HTML, XML, and C-like languages based on file extension.

### File Pinning with Notes & Global Bookmarks

To streamline access to frequent configuration files, application secrets, and active service log directories:

- **Pinning Files & Directories**: Click the pin icon (`📌`) on any row in the SFTP File Manager. Attach an optional operational note (e.g. `Main production nginx config` or `Postgres WAL archive directory`). Pinned rows display an accent border and note badges.
- **Quick Access Toolbar**: Pinned paths for the current host appear as quick-jump chips in the Quick Access bar directly above the file listing. Clicking a chip navigates directly to the directory or selects the file.
- **Global Bookmarks Modal**: Access all pinned files across all hosts simultaneously by clicking **`🔖 Bookmarks`** in the global navigation header:
  - **Instant Search**: Filter bookmarks across hosts, paths, and notes with real-time text matching.
  - **Integrated Code Editor**: Open any pinned text or configuration file directly in the CodeMirror editor.
  - **Direct Terminal Dock Jump**: Launch an interactive terminal tab already positioned in the parent directory of the pinned file (`openTerminalForHost` with target path).
  - **Inline Note Updates & Unpinning**: Update operational notes or remove pins directly from the modal.


### Elevated File Access (Run as root or Another User)

By default, the SFTP File Manager operates using the SSH credentials configured for the host. When attempting to navigate into another user's home directory (such as `/home/otheruser`) or restricted system locations (such as `/root` or `/var/log/audit`), the remote host denies access with a `permission denied` error.

Pantau allows you to elevate file operations to `root` or any other local system account via passwordless `sudo`:

#### How It Works
- **Access as Selector**: Use the **Access as** dropdown in the File Manager toolbar to select `default` (the configured SSH user), `root`, or enter another specific username.
- **Sudo SFTP Execution**: Rather than opening the default SSH subsystem, Pantau locates the remote host's `sftp-server` executable and launches it through an SSH exec channel using `sudo -n -u <user> <sftp-server>`. Pantau then communicates standard SFTP over this channel.
- **Complete Feature Parity**: Every File Manager capability runs under the target user's permissions: directory listings, file viewing and editing, uploads, calculating directory sizes, in-place duplication (with disk space verification), downloading files or streaming folder archives (`zip`/`tar.gz`), deleting, and changing permissions (`chmod`).

#### Prerequisites & Limitations
- **Passwordless Sudo**: The SSH user configured for the host must have passwordless `sudo` privileges (`sudo -n`) to run commands as the target user (e.g. configured with `NOPASSWD` in `/etc/sudoers`).
- **Standalone `sftp-server` Binary**: The host must have an OpenSSH `sftp-server` binary located at one of the standard system search paths:
  - `/usr/lib/openssh/sftp-server` (Debian, Ubuntu)
  - `/usr/libexec/openssh/sftp-server` (RHEL, CentOS, Fedora, Rocky, AlmaLinux)
  - `/usr/lib/ssh/sftp-server` (Arch Linux, openSUSE)
  - `/usr/libexec/sftp-server` (Gentoo, Alpine, BSD)
  - `/usr/lib64/ssh/sftp-server` (Alternative 64-bit paths)
- **Internal SFTP Incompatible**: Remote hosts configured exclusively with OpenSSH's built-in `internal-sftp` subsystem cannot be elevated, as there is no standalone binary on disk for `sudo` to execute.

#### Zero Password Exposure
- Pantau never prompts for, transmits, or stores passwords for `sudo` or target accounts.
- All elevation attempts strictly use `sudo -n` (non-interactive). If `sudo` requires a password or the user is not permitted in sudoers, the command fails immediately and safely.

#### Failure Handling & 1-Click Root Retry
- **Elevation Failure (HTTP 403)**: If elevation is unavailable (sudo requires a password, the user lacks sudo permissions, or the `sftp-server` binary is missing), the backend responds with `HTTP 403 Forbidden` and a plain-text message describing the reason. Pantau displays this error in a toast notification and reverts back to default SSH user access.
- **One-Click 'Open as root'**: When browsing under default access, if a directory listing fails with a `permission denied` error (`HTTP 400 Bad Request`), the UI presents a 1-click **Open as root** action button to instantly retry opening the restricted folder with elevated root permissions.

#### Security & Auditing
- **Full Privilege Context**: Elevated access grants full read and write capabilities of the target account. Exercise appropriate caution when modifying or deleting system files.
- **Prominent Visual Badge**: A distinct visual badge is displayed in the File Manager header whenever elevated mode is active, clearly indicating the active username.
- **Activity Console Logging**: Every elevated action (list, read, write, upload, duplicate, download, delete, chmod) and elevation refusal is recorded in Pantau's Activity Console log stream (`logger.Info` / `logger.Warn` with category `files`).
- **Trust Boundary**: Elevated file access shares the same trust level as an administrator running commands with `sudo` in an interactive web terminal session on hosts with sudo configured.

#### REST API Reference
- All endpoints under `/api/hosts/{id}/files/*` (including `list`, `read`, `write`, `upload`, `size`, `duplicate`, `download`, `delete`, and `chmod` — but *not* `pins`) accept an optional query parameter `as=<username>`.
- Usernames are validated against POSIX regex `^[A-Za-z_][A-Za-z0-9_.-]{0,31}$`. Invalid usernames return `HTTP 400 Bad Request`.
- Download links use standard `<a href>` GET requests, with the `as` query parameter appended to the download URL and authenticated via the active session cookie.
---

## 6. Phase 5: Hardening, Snapshots & Disaster Recovery

### Two-Factor Authentication (2FA) & Emergency Bypass

To protect management actions against credential theft:

1. Open **Settings** → **Two-Factor Authentication (2FA)**.
2. Pantau displays an RFC 6238 compliant **TOTP Secret** and QR code generated **100% offline** in your browser (zero external API calls).
3. Scan the QR code using your authenticator app (Google Authenticator, Aegis, 1Password, Bitwarden).
4. Save the **8 Emergency Recovery Codes** in a secure offline vault.
5. Enter the 6-digit code to activate 2FA.

#### Security Policies & Emergency Bypass:
- **2FA Lockout**: 3 consecutive failed verification attempts automatically trigger a 30-second cooldown lock to prevent automated brute-force attacks.
- **2FA Bypass Flag**: If mobile devices and recovery codes are completely lost, an administrator with shell access to the Pantau server can restart the binary with the emergency flag:
  ```bash
  ./pantau -port 8080 -db /data/pantau.db -disable-2fa
  ```
  This immediately clears active TOTP configurations from SQLite and allows administrative login.

### Centralized SSH Key Library (Settings → SSH Keys)

To avoid manually pasting private keys across multiple host forms, Pantau provides a centralized **SSH Key Library** under **Settings** → **SSH Keys**.

#### Adding & Managing Keys
1. Open **Settings** → **SSH Keys**.
2. Provide a unique **Name** (e.g., `aws-staging-cluster`, `hetzner-infra`, or `db-ansible-key`) and paste the unencrypted OpenSSH private key in PEM format.
3. Click **Add Key**. Pantau parses the key server-side using `golang.org/x/crypto/ssh`:
   - Automatically derives and stores the OpenSSH public key (`authorized_keys` format).
   - Computes and displays the SHA-256 fingerprint (`SHA256:...`).
   - Normalizes trailing newlines.
   - **Passphrase-Protected Keys**: Keys requiring a passphrase are not supported for automated agentless monitoring; attempting to add one returns an explicit error (`400 Bad Request`).

#### Safety Rules & Credential Protection
- **Zero API Credential Leakage**: The REST API (`GET /api/ssh-keys`) returns key metadata (`id`, `name`, `public_key`, `fingerprint`, `created_at`, `used_by`), but the `private_key` field is never exposed in JSON responses (`json:"-"`).
- **Deletion Protection for Active Keys**: Attempting to delete a key currently assigned to one or more hosts (`used_by > 0`) is strictly rejected with `HTTP 409 Conflict`. You must first reassign or detach the hosts before deleting the key to prevent accidental connection dropouts.
- **Log Masking**: Pantau's telemetry logger deterministically redacts private keys and authentication secrets into `[REDACTED]`.
- **Encrypted Snapshots**: All library keys are included in System Snapshots and sealed with AES-256-GCM. When restoring on a new server, key IDs and host mappings are preserved.

#### Automation REST API Reference
All endpoints require valid session authentication (`authMiddleware`):
- `GET /api/ssh-keys`: Returns `[SSHKey...]` metadata array with active usage counts (`used_by`).
- `POST /api/ssh-keys`: Creates a new key with `{"name":"...","private_key":"..."}`. Returns `201 Created` with key metadata. Returns `400` on parsing/passphrase failure, or `409` on duplicate name.
- `DELETE /api/ssh-keys/{id}`: Deletes a key if unreferenced. Returns `200` on success, `404` if not found, or `409` if still used by hosts.
- `POST /api/hosts` and update routes: Accept `key_id`. Specifying an invalid `key_id` returns `400 Bad Request`.

### Encrypted System Snapshots & GitHub Disaster Recovery

A **System Snapshot** is a self-contained, encrypted archive containing the complete Pantau database (host inventories, encrypted SSH keys, Desired State rules, and application settings).

#### Creating & Encrypting Snapshots:
1. Open **Settings** → **Backup & Disaster Recovery**.
2. Enter a strong **Snapshot Passphrase**. Pantau derives an encryption key using **Argon2id** and seals the snapshot with **AES-256-GCM**.
3. Download the resulting `.enc` file for offline storage.

#### Automated Remote Storage Sync:
- Configure a GitHub Personal Access Token (PAT with `repo` scope) and a private GitHub repository (`username/pantau-backups`).
- Set an automated sync interval (e.g., daily). Pantau pushes the encrypted `.enc` file to the remote repository automatically.
- **Multi-Server Tip**: If running multiple Pantau instances (e.g., office server and home lab) against the same GitHub repository, customize the **File Path in Repo** on each instance (e.g., `snapshots/office.enc` and `snapshots/homelab.enc`) so backups do not overwrite each other.


#### Bare-Metal Recovery (Disaster Recovery Wizard):
If the Pantau server is completely lost or destroyed:
1. Deploy a clean Pantau binary on a new host.
2. Launch the application; on first boot, click **Restore from System Snapshot**.
3. Select your local `.enc` file or enter your GitHub repository credentials + token.
4. Input your **Snapshot Passphrase**. Pantau decrypts the database, verifies integrity, reinstates the WAL SQLite database, and resumes background host inspection immediately.

---

## 7. Phase 6: Maintenance & Updates (Update Checker & Self-Update)

Pantau provides a robust, zero-data-loss update lifecycle ensuring administrators can update the control plane safely without disrupting ongoing monitoring jobs.

### Update Checker & Release Discovery
- **Background Release Detection**: Scans upstream GitHub Releases every 12 hours (with a 6-hour local cache) to avoid hitting GitHub API IP rate limits.
- **Visual Status Badges**: If a newer release is published, update badges and release notes appear non-obtrusively in the dashboard footer and Settings modal.
- **On-Demand Checking**: Administrators can trigger immediate checks via the **Check for Updates** button in Settings.
- **CLI Release Discovery**: Running `pantau -check-update` directly in the terminal queries the upstream release endpoint, displays the latest version and release URL, and outputs formatted release notes to help operators review changes prior to upgrading.

### One-Click Self-Update & SHA-256 Verification
For standalone binary installations on Linux and Windows:
1. Navigate to **Settings** → **System Updates** and select **Update Now**.
2. **SHA-256 Verification**: Pantau downloads the official `checksums.txt` manifest from upstream and verifies archive integrity before extraction.
3. **Pre-flight Smoke Testing**: Extracts candidate binary to a temporary path and runs `pantau.tmp -v`. If execution fails (e.g., incompatible architecture or corrupt download), the update is aborted with zero changes made (*automatic rollback*).
4. **Cross-Platform Atomic Swap**:
   - On Linux: Atomic swap via `os.Rename`.
   - On Windows: Renames running locked binary (`pantau.exe` -> `pantau.exe.old`) and installs the new binary. The `.old` artifact is purged on subsequent launches.
5. **Graceful Restart & Lock Handover**: Clicking **Restart Pantau Now** triggers an orderly shutdown (finishing pending requests, releasing the instance guard, and closing the SQLite WAL safely) and spawns the new binary with identical CLI parameters. The child process utilizes `instance.AcquireWithTimeout` (up to 3 seconds retry) to wait for the terminating parent process to release the database lock without conflict, and the web interface automatically re-establishes connection.

### Docker Container vs Standalone Environments
- Official Docker images published to GitHub Container Registry (`ghcr.io/herliansyah/pantau:latest`) are multi-architecture builds providing native support for both `linux/amd64` and `linux/arm64`.
- When executing inside Docker (`/.dockerenv`), self-replacement of the binary file is automatically disabled to preserve container immutability.
- The UI instead presents recommended update commands:
  ```bash
  docker compose pull && docker compose up -d
  ```

### Airgapped Deployments
- In isolated enterprise intranets without public internet egress, the Update Checker operates *fail-silent* with a 3-second timeout, ensuring no UI errors or boot latency occur.
- Automated update checks can be explicitly disabled using the `-disable-update-check` CLI flag or `PANTAU_DISABLE_UPDATE_CHECK=true` environment variable.

---

## 8. Embedded Documentation & In-App Changelog

Pantau ships with complete, self-contained documentation embedded directly inside the compiled binary via `go:embed`:

- **Airgapped-Ready Workspace Modal**: Access full operational guides offline without internet connectivity via the **Documentation Modal** (`75vw × 80vh`).
- **Multiple Document Tabs**: Read the project overview (**README**), operational handbook (**User Guide**), and chronological release history (**Changelog**) directly within the app.
- **Bilingual Support & In-Modal Switcher**: Seamlessly switch between English and Bahasa Indonesia using the in-modal language button `🌐 EN` / `🌐 ID`.
- **Dynamic Table of Contents**: Automatically generates heading-based navigation for smooth-scrolling directly to specific sections or release versions.
- **Universal Entry Points**: Open the documentation modal prior to authentication (via help links on the Initial Setup and Disaster Recovery screens) or after logging in (via the header `(?)` button, clicking the version badge in the dashboard footer, or clicking the Changelog link in the Settings modal).

