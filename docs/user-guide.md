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
   - [Repositories (Cloned Git Folders)](#repositories-cloned-git-folders)
   - [Evaluating Server Longevity with Lifecycle Assessment](#evaluating-server-longevity-with-lifecycle-assessment)
   - [Managing External Kubernetes Clusters (EKS, GKE, AKS & Custom Kubeconfig)](#managing-external-kubernetes-clusters-eks-gke-aks--custom-kubeconfig)
   - [Managing Cloudflare via REST API v4](#managing-cloudflare-via-rest-api-v4)
   - [Ansible Playbooks (Semaphore-Style, Agentless)](#ansible-playbooks-semaphore-style-agentless)
   - [Reading a Run: Log, Task Tree, Recap & Diff](#reading-a-run-log-task-tree-recap--diff)
   - [Survey (Typed Launch Questions)](#survey-typed-launch-questions)
   - [Ad-Hoc Runs (One Module, No Playbook)](#ad-hoc-runs-one-module-no-playbook)
   - [Host & Group Explorer (Ping and Facts)](#host--group-explorer-ping-and-facts)
   - [Git Sync (Verifying a Git Playbook)](#git-sync-verifying-a-git-playbook)
   - [Git Repository Editor (Edit, Commit & Push)](#git-repository-editor-edit-commit--push)
   - [Terraform Management (Terraform Cloud-Style, Local CLI)](#terraform-management-terraform-cloud-style-local-cli)
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
3. Choose the **SSH key for the imported hosts** under the preview: the **Global key** (default) or any key of the SSH Key Library; it is applied to every host of this import (import in several batches when hosts use different keys). Pantau does **not** read `IdentityFile` lines, so a host that logs in with its own key only works when you pick that key here (a 🔑 hint marks such hosts). Then click **Import Selected**: hosts are created without a group and each one is inspected right away (3 at a time), so you see its real state instead of `unknown`; a host the chosen key cannot log into shows as down with the SSH error in its last inspection, and **Edit** can change its key later.
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
- **Host Group**: Categorize hosts by functional tiers or environments (e.g., `Database Cluster`, `Edge Proxies`, `Staging`). Groups render as distinct visual partitions in both Grid and List **View Mode**. To rename a group, hover its header and click the pencil: the new name is applied to every host of the group at once (typing the name of another group, even with different letter case, merges into that group). Ansible schedules that target the group follow the new name; playbooks that name the group in `hosts:` do not and must be edited.
- **Manual Host Order**: Click and drag or prioritize mission-critical hosts to the top of the dashboard.
- **Host Note & Global Note**: Use **Host Note** for server-specific maintenance checklists (e.g., `"Primary PostgreSQL; failover standby is db-02"`). Use **Global Note** for cluster-wide operational announcements and shift handovers.
- **Delete several hosts at once**: click **Select** in the dashboard toolbar, tick hosts (checkbox, a card's empty area, or a list row) or use **Select all shown** (it follows the current search, group and status filters), then **Delete selected**. Pantau asks once, listing the names, and removes the hosts with their rules, history and stored scan data; nothing changes on the servers. Hosts that could not be deleted stay selected so you can retry. **Done** leaves selection mode.

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
  5. `sshd_empty_passwords`, `sshd_max_auth_tries`, `sshd_x11_forwarding`, `sshd_weak_crypto`: the same sshd checks as on Linux (see the Security tab section). The Linux-only checks (`mac_enforced`, `kernel_sysctl`, `bruteforce_protection`, `auditd_active`) are not reported on macOS.
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

Pantau supports 10 Desired State rule types evaluated on every inspection cycle:
| Kind | Target Identifier | Expected State | Examples | Drift Criteria & Root Cause Excerpt |
| :--- | :--- | :--- | :--- | :--- |
| **`container`** | Docker container name | `running` | `nginx` &rarr; `running` | Container exited, dead, or crash loop. Captures exit code, OOMKilled flag, and last 50 container log lines. |
| **`service`** | Linux daemon name | `active` | `mariadb` &rarr; `active` | Service inactive or failed. Captures last 50 log lines via `journalctl` or syslog. |
| **`port`** | TCP port or `ip:port` | `listening` (or `closed`) | `80` &rarr; `listening` | Port closed or listening daemon halted. Verified via `ss`/`netstat`. |
| **`disk`** | Partition mount point | `<N%` (usage ceiling) | `/` &rarr; `<85%` | Partition usage >= threshold or storage IO timed out after 5s (hung NFS protection). |
| **`cron`** | Keyword or command substring | `configured` | `backup.sh` &rarr; `configured` | Specified cron command not found in user's `crontab -l`. |
| **`backup`** | Absolute backup file path or glob pattern | `fresh_<N>h[,verify=<gzip\|tar\|zstd\|xz\|sha256>]` (default: 24h) | `/backup/*.sql.gz` &rarr; `fresh_24h,verify=gzip` | File is missing, 0 bytes empty, outdated, or content verification check fails (`gzip -t`, `tar -tf`, etc.). Evaluates the newest matching file. |
| **`http`** | Full HTTP/HTTPS URL | Status token and optional latency ceiling | `https://api.internal/health` &rarr; `2xx,<1500ms` | Endpoint unreachable from Host via `curl`, HTTP status code mismatch, or latency exceeds threshold. Evaluated from host's perspective via SSH. |
| **`tls`** | Hostname/port or HTTPS URL | `>=Nd` (minimum days until cert expiration) | `api.example.com:443` &rarr; `>=14d` | Certificate expires within N days or TLS handshake fails. Evaluated from host's perspective via `openssl`. |
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
- **Enforced Hard Timeout Budget**: Each host inspection cycle is constrained to an enforced 45-second total timeout budget. Stalled or hung SSH connections are terminated on timeout and marked down to preserve queue availability. A TCP connect that times out is retried on a fresh connection (up to 4 attempts of at most 4 s), so a reachable host on a lossy route is not marked down; a refused or unreachable port fails at once.
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
  - **Top CPU**: Tracks PID, user, `%cpu`, memory footprint, and process command. On Linux, CPU is current usage sampled across a ~1 s two-sample window per inspection (not the lifetime average of `ps %cpu`; multi-threaded processes can exceed 100% since values represent percentage of one core). On macOS, recent usage is tracked via `ps -Aro`. On hosts where `ps` lacks `--sort` (e.g. BusyBox/Alpine), Pantau sorts the processes itself to ensure the real top 5 are captured.
  - **Top RAM (Physical RSS)**: Tracks PID, user, `%mem`, RSS memory footprint, and process name (sorted natively or via Pantau on minimal environments like BusyBox/Alpine).
  - **Top Swap (Virtual Memory)**: Conditionally extracted only when `SwapUsedBytes > 0` by parsing `VmSwap:` in `/proc/[0-9]*/status`, resolving usernames from `/etc/passwd` (with fallback to numeric UID only if unresolvable) to identify virtual memory hogs without remote daemon overhead.
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


### Fleet Search (Packages & Containers Across Hosts)

Answer questions like "which hosts still have `openssl` 1.1?" or "who runs `postgres:13`?" without logging in anywhere: open the Command Palette (`Ctrl+K`), type at least 2 characters and pick **Search packages & containers across hosts**.
- Results are grouped per package version or container image, each with the hosts that have it (group, version or container name and state) and how old that host's data is. Click a host to open it on its Packages or Docker tab. Filter with **All / Packages / Containers**.
- Nothing is scanned while you search: packages come from the last package scan of each host (run **Rescan Packages** to refresh), containers from the last inspection (running containers and their images are recorded on every inspection, and when the Docker tab is opened) plus `container` desired rules and remembered Compose projects. Hosts without data are counted in a note.
- Up to 500 results per search (`GET /api/search?q=&kind=all|package|container&limit=`).

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
- **Security Findings Table**: Lists affected packages, currently installed versions, available fixed versions, severity badges, security advisory IDs (e.g., `RHSA-*`, `ALAS-*`) and, on dnf/yum hosts, the advisory's CVE IDs (linked to NVD, from `dnf updateinfo list --security --with-cve` / `yum updateinfo list cves`). The filter also matches CVE IDs.
- **Hardening Checks**: Evaluates 8 read-only security configuration baselines on every host, plus 4 Linux-only ones. A check is `pass` only when the setting that is actually in force was verified; a file or rule merely existing is not enough.
  - `sshd_permit_root_login`: Reads the effective OpenSSH config from `sshd -G` (OpenSSH 9.3+, no root needed) or `sshd -T` (root or passwordless `sudo -n`), which already applies `Include`, `Match` and the distro's compiled-in defaults. Without either, it reads `/etc/ssh/sshd_config` the way sshd does: an `Include` pulls the matching `sshd_config.d/*.conf` drop-ins in at its own line (so a drop-in included at the top overrides the main file, and drop-ins that are never included are ignored), first match wins, `Match` blocks are skipped. `no` / `prohibit-password` / `forced-commands-only` → `pass`, `yes` → `fail`. When unset, the upstream default (`prohibit-password`) is assumed only on Debian/Ubuntu/Alpine/Arch/macOS; elsewhere (RHEL/CentOS ≤ 8 default to `yes`) it reports `unknown`.
  - `sshd_password_auth`: `pass` only if `PasswordAuthentication no` and keyboard-interactive cannot ask for a password (`KbdInteractiveAuthentication`/`ChallengeResponseAuthentication no`, or `UsePAM` off); otherwise, or when unset, `warn`.
  - `auto_security_updates`: Debian/Ubuntu use the effective `apt-config dump` values: `pass` only if `APT::Periodic::Unattended-Upgrade` is not `0`, the `unattended-upgrades` package is installed and `apt-daily-upgrade.timer` is not disabled or masked (`Update-Package-Lists` alone installs nothing → `warn`). RHEL: `dnf-automatic-install.timer` → `pass`; `dnf-automatic.timer` or `yum-cron` → `pass` only with `apply_updates = yes` in `/etc/dnf/automatic.conf` / `/etc/yum/yum-cron.conf` (their default only downloads → `warn`).
  - `firewall_active`: `pass` if ufw or firewalld is active, or an input chain really filters: an nft base chain on the `input` hook with `policy drop` or an unconditional `drop`/`reject` rule, or an iptables `INPUT` chain with policy `DROP` or an unconditional `-j DROP|REJECT`. Tables that only Docker, kube-proxy or fail2ban create (policy accept) report `warn`. Firewall tables need root: the scan reads them with passwordless `sudo -n` when the SSH user is not root (shared 24 h backoff after a refusal). If neither root nor sudo is available, it reports `unknown` with explanatory detail.
  - `sshd_empty_passwords`: `PermitEmptyPasswords yes` → `fail`; otherwise `pass` (OpenSSH default `no`).
  - `sshd_max_auth_tries`: `pass` if `MaxAuthTries` ≤ 4 (CIS); higher, or unset (OpenSSH default 6) → `warn`.
  - `sshd_x11_forwarding`: `X11Forwarding yes` → `warn`; otherwise `pass`.
  - `sshd_weak_crypto`: `Ciphers`/`MACs`/`KexAlgorithms` containing CBC, 3DES, arcfour, blowfish, MD5, SHA-1, `-96` or `umac-64` → `warn`. If any list is unset in the files (compiled-in defaults not visible) → `unknown`; `pass` only when all three lists are read (from `sshd -G`/`-T` or the files) with nothing weak.
  - Linux only (no root needed): `mac_enforced` (SELinux enforcing or AppArmor enabled), `kernel_sysctl` (`kernel.randomize_va_space=2`, `kernel.kptr_restrict≥1`, `net.ipv4.tcp_syncookies=1`, `net.ipv4.conf.all.accept_redirects=0`, `net.ipv4.conf.all.accept_source_route=0` from `/proc/sys`; partly unreadable → `unknown`), `bruteforce_protection` (fail2ban, sshguard or crowdsec active), `auditd_active`. Not active → `warn`; no `systemctl`/`pgrep` → `unknown`.
- **Authentication & Exposure**: failed SSH logins (`Failed password`, `Failed keyboard-interactive/pam`, `Invalid user`) counted over the last 24 h from journald, or over the whole `/var/log/auth.log` / `/var/log/secure` on hosts without journald. When neither is readable by the SSH user (needs the `adm`/`systemd-journal` group), it shows "Not readable" instead of 0. Ports listening on all interfaces whose service should never face the internet (databases, caches, Docker/etcd/kubelet APIs, SMB/NFS/RPC, FTP/telnet, RDP/VNC, LDAP, SNMP, …) are `HIGH RISK`; other ports show "Not flagged", which is not a safety verdict.
- **Coverage Notes**: Surfaces any environment limitations, permission barriers, or cache staleness notices.
- **Firewall** (read/write, unlike the rest of the tab): a card at the bottom reads **ufw**, **firewalld** and **iptables** live over SSH when you click **Load firewall** (never on tab open, so no SSH connection or sudo probe happens unless you ask). Per backend you can **Set up** (install if missing, then a safe baseline: ufw = deny incoming / allow outgoing + SSH + enable; firewalld = start/enable + SSH port; iptables = loopback + established + SSH, `INPUT` policy `DROP`, then save), **Enable/Disable** (ufw, firewalld), **Add**, **Edit** and **Delete** rules, create a **New zone** (firewalld, via the Add zone button or within the rule dialog), and **Save to disk** (iptables; runtime-only otherwise). For firewalld, the rule dialog breaks down inputs into dedicated fields per type (service name with common datalist options, port and protocol, or structured rich rules with a raw rule fallback). Commands run as root, else with passwordless `sudo -n`, else as the SSH user (usually refused: the card shows the mode). Rules the edit form cannot express (conntrack, comments, app profiles, `(v6)` twins) show a lock: delete only. Safety: a rule that allows or blocks the SSH port Pantau connects on (unconditionally, from any source) cannot be deleted, edited away or blocked from here (HTTP 403, use a terminal), and Enable/Set up always allow that port first. Edit and Delete re-check that the listed rule is still at that position and are refused if it changed. IPv6 `iptables` (`ip6tables`) and the nftables native ruleset are not managed.

#### Honest Coverage Principle
Pantau strictly enforces an **Honesty Rule**: a host is never presented as "clean" or "safe" without complete coverage.
- If an operating system is not supported for package vulnerability checks (such as Alpine Linux or Arch Linux), the scan reports `status: unsupported` with an explanatory note (hardening checks still run).
- If the update commands themselves are restricted by user permissions or if the local package cache is older than 7 days (> 168 hours), the scan reports `status: partial` with a notice that results may miss recent advisories.
- Hardening checks that cannot be evaluated (e.g. the firewall status needs root, or a config file is absent) are shown as `unknown` and listed in the scan notes; they do not change `status`, since the update data itself was fully collected.
- When the status is `unsupported` or `error`, the UI shows pending updates, severity breakdown and reboot state as **not assessed** instead of zero.
- Distros with legacy tools (CentOS 6/7, Debian 7+) are handled gracefully without failing the inspection cycle.

#### Exploit Intelligence (CISA KEV + EPSS) & Vulnerability Databases
Severity says how bad a flaw could be, not whether attackers use it. Turn on **CISA KEV and EPSS** in **Settings → General** (off by default) and the Security tab marks each CVE of a finding ([ADR-0064](adr/0064-exploit-intelligence-kev-epss.md)):
- **Exploited (KEV)**: the CVE is in the CISA Known Exploited Vulnerabilities catalog (hover for the vulnerability name, the date it was added and the US federal remediation due date; `⚠` marks known ransomware use).
- **EPSS x%**: FIRST's probability that the CVE is exploited in the next 30 days (amber from 10%), with its percentile on hover.
- Sort by **Exploitation: KEV, then EPSS** to patch exploited packages first. The **Exploit intelligence** card summarizes how many CVEs are in KEV or have a high EPSS, shows the KEV catalog and EPSS dates, and reports feed errors.
- The Pantau server downloads the KEV catalog at most once a day and asks EPSS only for the CVEs it has not scored in the last day (100 per request); nothing is stored in the database, and with the setting off nothing leaves the server. If a feed is unreachable, the last good KEV catalog is still used and the error is shown.
- Only dnf/yum hosts report CVE IDs today. On apt, apk, pacman and macOS hosts the card says **not assessed** instead of implying the host is safe.
- **Vulnerability databases** (button on the card, also before a first scan) lists 45 vulnerability databases and feeds worldwide, grouped by category with a text filter and badges for API access, free use and **Used by Pantau**. The list is a copy embedded in Pantau of [haxdoggy/vulnerability-databases](https://github.com/haxdoggy/vulnerability-databases) ([CC BY 4.0](https://creativecommons.org/licenses/by/4.0/)), shown unmodified with its snapshot date, so it also works airgapped.

#### DevSecOps Tools
- **Known vulnerabilities in installed packages** (Security tab card, [ADR-0068](adr/0068-known-vulnerabilities-osv-arch-tracker.md)): turn on **Known vulnerabilities in installed packages** in **Settings → General** (off by default). The Pantau server then sends only package names, versions and the OS release to `api.osv.dev` (Debian, Ubuntu, Alpine) or `security.archlinux.org` (pacman) and the card lists CVE, severity and fixing version; **Include unfixed** also shows findings without a fix. Needs a package inventory (Packages tab). Releases the feeds do not cover show **not assessed**.
- **Accounts & SSH keys** (Security tab card, [ADR-0072](adr/0072-authorized-keys-inventory-and-account-audit.md)): **Scan** reads accounts, `authorized_keys`, password state and sudo grants. Keys are matched to the SSH Key Library by fingerprint; unknown, weak (DSA, RSA < 2048) keys, extra UID 0 accounts, empty passwords and passwordless sudo are findings. `/etc/shadow` and sudoers need root or passwordless `sudo -n`, otherwise those parts show **unknown**. **Fleet keys** lists every key across hosts.
- **Container security** (Docker tab → Security, [ADR-0069](adr/0069-container-security-audit-and-image-vulnerability-scan.md)): **Run audit** checks container configuration; **Scan image** in the Images list uses `trivy` or `grype` on the host; if neither is there, the first scan installs trivy (needs root or passwordless sudo, curl or wget, and internet access on the host).
- **File integrity** ([ADR-0071](adr/0071-file-integrity-monitoring-desired-state.md)): add a Desired State rule of kind `file_integrity`, enter absolute paths (or `preset:auth`, or `suid`), press **Capture baseline**, review the preview and confirm. Drift lists modified, removed and added files.
- **IaC and secret scan** ([ADR-0070](adr/0070-iac-and-secret-scanning.md)): **Security scan** on a repository, Terraform workspace or Ansible playbook runs the built-in scanner. Turn on the **IaC gate** in **Settings → General** to refuse Terraform apply and Ansible run while critical or high findings are open (the dialog offers **Run anyway**). Scheduled auto-apply and the Ansible scheduler are not gated.

#### Dashboard Security Chip
Every scanned host shows a shield chip next to the Lifecycle Score on its card, and in the **Security** column of the list view (`GET /api/hosts` carries a `security` digest: `status`, `scanned_at`, `pending_security`, `reboot_required`, `hardening_fail`, `hardening_warn`). The chip shows the number of pending security updates when there are any (`N/A` when the scan is `unsupported` or `error`); the color is red for pending security updates or a failed hardening check, amber for a partial scan, a required reboot or a hardening warning, green otherwise. Hover for the breakdown and scan age; click to open the host's **Security** tab. Hosts never scanned show no chip.

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
- **Zero Agent, minimal sudo**: The Security Scan itself is read-only collection with the SSH user's permissions: it never runs `sudo` (except the optional `sudo -n` probe described above), never installs packages and never updates package caches (`apt-get update` / `dnf makecache`). Pantau installs software on a host only for two on-demand features, always with root or passwordless `sudo -n` and never a password prompt: `traceroute` (Network diagnostics) and `trivy` (the first Docker image scan). No daemon or agent stays running on the host.
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
- **Toolbar Ports Dropdown (`🌐 Ports`)**: Access private web applications running on the target host by opening the `🌐 Ports` dropdown on the terminal toolbar. Enter any remote port number (1-65535) and click **Buka** to launch a reverse proxy tunnel session. Frequently accessed ports are saved automatically to local storage for quick access. Tunnels open on Pantau's separate tunnel port (`-tunnel-port`, default: the first free port after the UI port, printed at startup), a different browser origin from the UI, so a remote web app cannot call Pantau's API; Pantau's session cookie is not forwarded to the remote app.
- **Zero-Configuration Remote Browser Bridge**: CLI utilities running on remote servers (`gh auth login`, `xdg-open`, Python `webbrowser`, AWS SSO, `npm login`) automatically launch real desktop browser tabs on your client computer via terminal escape protocols (OSC 1337 / OSC 777), completely eliminating unwanted console text-browsers (`w3m`/`lynx`). When the requested URL is a loopback address on the remote host (`localhost`, `127.x`, `::1`), Pantau first asks for confirmation (host and port shown) before opening it through the tunnel.
- **Working Directory Synchronization**: Synchronizes active working directory via shell window titles and OSC 7 ANSI escape sequences.
- **Port Forward to This Computer** (like `ssh -L`): for non-HTTP services (Postgres, Redis, MySQL, …) open **Port Forward** from the host card menu, **Forward port to this computer…** in the `🌐 Ports` dropdown, or the Command Palette. Pick the remote port (and optionally a remote host as seen from that server, default `127.0.0.1`); Pantau listens on the first free local port of the `-forward-ports` range (default `15000-15019`) and shows the address to use, e.g. `psql -h 127.0.0.1 -p 15000`. The dialog lists all active forwards with connections, traffic and last error, and **Stop** ends one. Forwards bind on the same address as the UI (`-listen`), live in memory only (they end when Pantau stops), and are stopped when their host is deleted. Docker: the compose file publishes the range on `127.0.0.1` only.
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
  - **Port Probe (TCP)**: Tests TCP port connectivity via native bash socket `/dev/tcp/<target>/<port>`; on hosts without bash (Alpine/BusyBox) it falls back to `nc -z`.
  - **DNS Resolve**: Resolves any domain, including global/public ones like `google.com`, to IPv4 and IPv6 addresses via the host's system resolver (`getent ahosts`, `dscacheutil` on macOS, or `nslookup`). If the system resolver returns nothing, Pantau retries against the public resolver `1.1.1.1` (`dig`, `nslookup`, or `host`) and notes it in the summary.
  - **Dig (DNS Records)**: Runs `dig` on the source host for a chosen record type (`A`, `AAAA`, `CNAME`, `MX`, `NS`, `TXT`, `SOA`, or `PTR` reverse lookup on an IP) against the system resolver or an optional DNS server (for example `1.1.1.1` or `8.8.8.8`). It shows the answer records (name, TTL, type, value), the response code (`NOERROR`, `NXDOMAIN`, `SERVFAIL`), the answering server, and the query time. Requires `dig` on the source host (`dnsutils` / `bind-utils` / `bind-tools`).
- **Intelligent Hop Analysis**: Automatically parses traceroute hops into structured topology cards:
  - **Zone Classification**: Identifies whether each hop belongs to `Local/Private VPC` (RFC 1918 / CGNAT), `ISP Transit`, or `Destination`.
  - **Blackhole Detection**: Pins down the exact network hop where traffic drops if destination is unreachable.
  - **Latency Spike Alerts**: Highlights significant latency jumps (>60ms) between consecutive hops.
- **Dual Launch Points**:
  - Open from the **Network Diagnostics** tab in any Host Workspace Modal (with source host locked).
  - Open globally via the **`⚡ NetDiag`** button in the top navigation header cluster (with host picker).
- **Concurrency & Safety Guards**: Employs an in-memory singleflight lock per host to prevent SSH session saturation. Offline hosts are automatically disabled in inter-host target dropdowns.

### Databases Tab: Database Backup

The **Databases** tab of a host finds its database servers (MySQL/MariaDB, PostgreSQL, MongoDB), both installed on the host and running in Docker containers, and downloads a full backup of one with **Backup**. The dump is compressed and streamed straight to your browser (`<host>-<container>-<engine>-<timestamp>.sql.gz`, MongoDB `.archive.gz`, restorable with `mongorestore --archive`); nothing is written on the host or the Pantau server. One backup per host at a time.
- **On the host**: the server must be running outside Docker and its dump tool installed (`mysqldump`/`mariadb-dump`, `pg_dumpall`, `mongodump`; the tab says which one is missing). Pantau uses the SSH user's local login: root, or passwordless `sudo`, over the unix socket for MySQL/MariaDB, and the `postgres` account for PostgreSQL. **MongoDB** is dumped over its network port: Pantau reads `/etc/mongod.conf` (via `sudo` if needed) for the port, bind address and, when TLS is required, the CA and key files, and **Backup** asks for a username, password and authentication database (leave the username empty for a server without authentication). The login is sent in the request body, handed to `mongodump` on stdin through a config descriptor (never on a command line), and not stored.
- **In Docker**: running containers whose image is MySQL, MariaDB, Percona, PostgreSQL (also PostGIS, TimescaleDB) or MongoDB. The dump runs inside the container with the credentials of the official image variables (`MYSQL_ROOT_PASSWORD`/`MARIADB_ROOT_PASSWORD`, `POSTGRES_USER`/`POSTGRES_PASSWORD`, `MONGO_INITDB_ROOT_USERNAME`/`PASSWORD`, and their `_FILE` variants), so no password is sent over SSH; for MongoDB a login typed in the dialog replaces them. Containers set up another way may fail with an authentication error, which is shown as a message.
- A backup always covers **every database** of the server. A dump that stops midway leaves a truncated gzip file that `gunzip` reports as corrupt.

### Domains Tab: nginx, Caddy & Traefik Domain Inventory

The **Domains** tab of a host lists every domain its reverse proxies serve, with TLS, listen ports, upstream targets and where each one is configured. The first time the tab is opened Pantau scans the host; **Scan again** refreshes it. Results are stored, so the fleet view and Fleet Search work without connecting again.
- **nginx** (host and containers): `nginx -T` (falling back to `sudo -n nginx -T`, then to the readable files in `/etc/nginx`); one row per `server_name`, `upstream` blocks are resolved to their servers, `_` catch-all servers are skipped, and the source is shown as `file:line` with **Open** to edit it.
- **Remove an nginx config**: the trash button next to **Open** deletes that config file from the host (files under `/etc/nginx/` on the host only, never `nginx.conf` or a container's config) after a confirmation. Every domain defined in the file goes with it. Pantau moves the file aside, runs `nginx -t`, then reloads nginx and drops the file; if the test fails the file is put back and the nginx error is shown. Needs root or passwordless `sudo`. The tab rescans afterwards.
- **Caddy** (host and containers): the running config from the admin API (`localhost:2019`), else `caddy adapt` of `/etc/caddy/Caddyfile`, else the Caddyfile site addresses.
- **Traefik**: router rules from Docker labels (`Host(...)`, `HostRegexp(...)`, v1 `Host:`) and the Traefik API on `127.0.0.1:8080` when it is reachable; also **caddy-docker-proxy** labels and **nginx-proxy** `VIRTUAL_HOST` containers.
- Docker needs the SSH user to run `docker` (or passwordless `sudo docker`); at most 10 proxy containers are read per scan. Problems (no permission, admin API off) are shown as red proxy badges and warnings instead of failing the scan. Kubernetes ingresses are not included.
- **Domains across hosts** (Command Palette, or **All hosts** in the tab) lists every stored domain with its host, filterable, and **Scan all hosts** refreshes the whole fleet (3 hosts at a time). Fleet Search also finds domains (filter **Domains**).
- **Domain Shortcuts**: the star next to a domain (in the tab or in **Domains across hosts**) pins it as a tile above the host dashboard, showing the domain and its host. Click a tile to open the domain in a new tab (`https://` when it is served with TLS); the **×** on a tile, or the star again, removes it. Wildcard and regex domains have no address to open, so they have no star. The shortcuts are saved in the browser: they survive a restart of Pantau or the browser, and another browser starts with none.

### Docker Tab: Containers, Stats, Volumes & Networks

The **Docker** tab of a host reads the Docker engine live over SSH (read-only; the SSH user needs permission to run `docker`). It has four views:

- **Containers**: all containers, running or not, with image, status and ports, plus the `⋮` menu (Logs, Start, Restart, Stop).
- **Stats**: a one-shot `docker stats --no-stream` of running containers sorted by CPU: CPU %, memory used / limit, network and block I/O, PIDs, with totals. Tick **Auto-refresh (10s)** to keep it live; the table updates in place and a failed refresh keeps the last snapshot. CPU can exceed 100% on multi-core hosts.
- **Volumes**: named volumes with driver, size, mountpoint, and the containers using them. Volumes no container mounts are flagged `unused`; anonymous volumes (hash names created implicitly by containers) are listed after named ones. The size comes from `docker system df -v`; engines that cannot report it (older Docker versions) or a scan that exceeds 40 seconds leave the size as `—`.
- **Networks**: networks with driver, scope, subnets, gateways and attached containers (with their IPs on hover); user-defined networks come first and `bridge`, `host` and `none` are marked built-in.

Views are fetched on demand through `GET /api/hosts/{id}/docker/stats`, `/docker/volumes` and `/docker/networks`. If Docker cannot be read (not installed, permission denied) the tab shows Docker's own error message.

#### Docker Hub: Browse, Pull & Image Update Check

The **Images** sub-tab has two Docker Hub buttons ([ADR-0063](adr/0063-docker-hub-integration.md)). Docker Hub is queried by the **Pantau server** (public data, no login, cached 5 minutes); pulls run on the **host**, with the host's network and Docker Hub pull quota.

- **Docker Hub** opens a search over public repositories (official images listed first, with stars, pull counts and description). **Details** shows the repository: **Tags** (newest push first, filterable, 50 per page with **Load more**; platforms, compressed size and push age per tag) and **Overview** (the repository README, rendered safely: raw HTML dropped, images not loaded). Each tag has **Pull** (streams `docker pull` into the log viewer) and **Run** (opens **Add container** with the image filled in). **Open on Docker Hub** links to hub.docker.com.
- **Check updates** compares every local Docker Hub image (`nginx:1.28`, `bitnami/redis:7`, `docker.io/…`) with the digest Docker Hub currently serves for its tag, and adds a badge to the Status column: **Update available** (the tag now points to a different image; hover for its push date, and **Pull update** pulls it), **Up to date**, **Not on Docker Hub** (the tag does not exist there, e.g. a local build), **Not comparable** (the engine recorded no registry digest, e.g. `docker load`), or **Check failed** (with the reason). Images from other registries (ghcr.io, a private registry) get no badge. The summary line shows how many updates are available. The check pulls nothing and does not count against the pull quota; it stops at the first Docker Hub refusal (rate limit, unreachable) instead of spending the API limit, and asks at most 100 tags per check.
- A pulled update does not change running containers: recreate them (for Compose projects: **Stacks → Update**).
- Not supported: private repositories and `docker login`, other registries, scheduled checks or notifications. A local build that reuses a Docker Hub name (e.g. `nginx:latest`) shows **Update available**, and **Pull update** replaces that local tag.

### Command Palette (Ctrl+K)

Press `Ctrl+K` (`Cmd+K` on macOS) or click the search icon in the header to jump anywhere without scrolling the dashboard:
- **Hosts**: type part of a host name, address or group (fuzzy, all words must match) and press `Enter` to open it. The last 5 opened hosts are listed first when the query is empty (stored in the browser).
- **Host actions**: the chevron on a host row (or `→`) opens Overview, Terminal, Files, Docker, Security, Packages, Inspect now, Port forward and Edit host for that host. `Esc` or `Backspace` on an empty query goes back.
- **Commands**: Inspect all hosts, Bulk exec, Add host, Import hosts, Settings, Documentation, switch section (Hosts, Docker, Kubernetes, Cloudflare, Ansible, Terraform), Port forwards, and with 2+ characters **Search packages & containers across hosts** (Fleet Search).
- `Ctrl+K` is not intercepted inside the terminal (where it is bash kill-line) or the code editor.

### Global Side Panel, Docker & Kubernetes Management

Pantau provides a persistent left-hand **Side Panel** navigation bar positioned between the fixed header and footer:

- **Navigation & Collapsible Icon Rail**: Switch between **Hosts** (default fleet dashboard), **Docker** (dedicated multi-host container overview), and **Kubernetes** (lightweight k3s/rke2 management). Collapse the panel into a compact icon rail via the collapse toggle at the bottom; the collapsed state is persisted in `localStorage`. On mobile screens (<768px), the panel becomes an off-canvas drawer toggled via the menu icon button in the header.
- **Docker Section**: A fleet-wide host picker with live status and search filter. Selecting a host seamlessly launches the Portainer-style Docker management interface with container lifecycles, image pulling (including the Docker Hub browser and image update check), volumes, networks, compose stacks, and terminal console access.
- **Shortcuts Section**: A start page of your own links to external web addresses (dashboards, admin panels, documentation). **Add shortcut** takes a name, an address and an optional group; an address without `http://` or `https://` is saved as `https://`, and anything that is not a web address is refused. Tiles are listed per group (shortcuts without a group first) and open in a new tab; the pencil edits a shortcut and the trash button deletes it after a confirmation. The list is stored on the Pantau server, so every browser shows the same shortcuts and they are included in System Snapshots (at most 200). A tile shows the favicon of its site (`/favicon.ico`, loaded by your browser straight from that site), or the first letter of the name when the site has none or cannot be reached, as on an airgapped network; Domain Shortcut tiles on the dashboard do the same.
- **Kubernetes Section (k3s / rke2)**: Provides agentless cluster control plane inspection and operations for lightweight Kubernetes installations.
  - **Cluster-Grouped Host Picker**: Displays high-availability clusters as single entries grouped by cluster ID with member counts and aggregate health status; standalone nodes appear separately. A **Detect clusters** button scans fleet hosts with bounded concurrency (5).
  - **Cluster Workspaces**: The section opens on a list of workspace cards, one per host or cluster you picked (name, node count or provider, health, mode). Pick a host or cluster in the picker to add its card and open it; click a card (or **Open**) to see its Nodes, Workloads, Pods and the other tabs, and **Workspaces** to go back to the list. A workspace opens on the tab and namespace you left it on, without detecting the cluster again while the page stays open. The **×** on a card removes the card only, never the cluster. The cards are saved in the browser, so they are still there after Pantau or the browser restarts; another browser starts with an empty list.
  - **Gateway Host & Seamless Failover**: Cluster requests execute via a designated Gateway Host. If the primary host is unreachable, Pantau automatically fails over to a healthy server peer, displaying a non-blocking notification and Gateway badge (`Gateway: thor (failover from odin)`).
  - **Kubernetes Modes**: Configurable per-host operational policy (`full`, `no-exec`, or `read-only`):
    - `full`: Complete read, write, scale, rollout, delete, and container exec access.
    - `no-exec`: Disables interactive container exec terminals while permitting workload lifecycle and log inspection. *Note: this is an application-level guardrail; primary security boundaries remain kubeconfig, sudoers, and cluster RBAC on the target host.*
    - `read-only`: Prohibits exec, scaling, restarts, node cordoning, deletions, and manifest application.
  - **Action Safety & Concrete Impact Confirmation**: Destructive and disruptive actions require confirmation detailing operational consequences:
    - **Scale to 0**: Warns of the exact count of terminated pods while clarifying that StatefulSet PersistentVolumeClaims (PVCs) remain preserved.
    - **Restart Rollout**: Confirms rolling reconstruction across replica sets.
    - **Delete Pod**: Displays target node and warns that unmanaged pods (not backed by a Deployment/StatefulSet/DaemonSet) will be permanently lost.
    - **Cordon & Drain Node**: Explains that cordon keeps workloads running while blocking new pods. Node **Drain** features a two-step flow: a server dry-run preview listing evictable pods and PodDisruptionBudget blockers, configurable flags (Ignore DaemonSets, Delete EmptyDir data, Force unmanaged pods), concrete high-availability/etcd quorum warnings for control-plane nodes, and an immediate **Uncordon** action button in the completion/timeout toast notification.
  - **Cluster Telemetry Tabs**:
    - **Nodes**: Real-time node inventory with node-to-registered-Host mapping, readiness status, roles, installed version, CPU/memory usage with visual progress bars (`dockerBar`), allocatable capacity, drain, and cordon/describe actions.
    - **Workloads**: Deployments, StatefulSets, and DaemonSets with replica tracking, on-demand replica scaling (0–1000), rollout restarts, aggregated **Workload Logs** streaming with container prefixing, row-level **Edit YAML** (clean manifests with preserved resourceVersion and diff gate), and resource describe dialogs.
    - **Pods**: Pod statuses across namespaces with problem pods prioritized at the top and highlighted, an optional **Only problems** filter, restart counters, CPU and memory usage, interactive shell **Exec** console in the Terminal Dock, live WebSocket **Logs** streaming with client-side text search and match counting, a **Timestamps** toggle, previous container instance toggle, auto-scroll, and pod deletion.
    - **Services & Ingresses**: Service definitions, cluster/external IPs, ports, ingress hosts, backend targets, row-level **Edit YAML**, and describe views.
    - **Configuration & Storage**: ConfigMaps, Secrets (strictly zero data value retrieval and no edit option), PersistentVolumeClaims (PVCs) with **Edit YAML**, and cluster-scoped PersistentVolumes (PVs).
    - **Batch Jobs**: Jobs and CronJobs with completion tracking, schedules, suspension status, row-level **Edit YAML**, and describe diagnostics.
    - **Events**: Cluster-wide informational and warning events with relative age and a dedicated **Warning only** filter.
    - **Opt-in Auto-refresh**: Configurable per-tab polling interval (Off / 10s / 30s / 60s) that automatically suspends when the browser tab is hidden, when changing sections/tabs, or when any modal is open, and never overlaps in-flight requests.
    - **Apply Manifest & Diff Preview**: In-app YAML editor with mandatory **Preview Diff** execution (`kc diff` dry-run against live cluster state). The **Apply** button remains locked until the current manifest text has been diffed, and mutations undergo confirmation before applying.

#### Managing External Kubernetes Clusters (EKS, GKE, AKS & Custom Kubeconfig)

Pantau allows monitoring and managing external or managed Kubernetes clusters (AWS EKS, Google Cloud GKE, Azure AKS, or self-hosted clusters) without SSH access to control-plane nodes. For external clusters, commands are executed locally using `kubectl` directly on the Pantau server.

##### Adding an External Cluster
1. In the **Side Panel**, navigate to the **Kubernetes** section and click **Add Cluster**.
2. **Paste or Upload Kubeconfig**: Provide your kubeconfig file (maximum 128 KB).
3. **Select Context**: Pantau reads available contexts; pick the desired target context.
4. **Set Cluster Name & Mode**: Assign an identifiable name and choose a **Kubernetes Mode** (`full`, `no-exec`, or `read-only`).
5. **Test & Save**: Pantau normalizes the configuration, tests connectivity via `kubectl get ns kube-system`, saves the cluster record, and retrieves cluster telemetry. If connectivity verification fails, the cluster is rejected and not saved.

##### Cloud Provider Authentication & CLI Plugins
When connecting to managed cloud clusters, Pantau relies on native `kubectl` credential plugins executed on the Pantau server:
- **AWS EKS**: Requires `aws` CLI or `aws-iam-authenticator` installed on the Pantau server `PATH`. AWS credentials are read from the Pantau process environment (e.g. `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION`, or host `~/.aws`).
- **Google Cloud GKE**: Requires `gke-gcloud-auth-plugin` or `gcloud` on `PATH`. Google credentials are read from `GOOGLE_APPLICATION_CREDENTIALS` or `~/.config/gcloud`.
- **Azure AKS**: Requires `kubelogin` or `az` on `PATH`.

> [!TIP]
> **Security & RBAC Best Practice**:
> - Prefer short-lived tokens or `exec` credential plugins over static credentials.
> - For read-only observability, create a dedicated Kubernetes `ServiceAccount` bounded by minimal RBAC `ClusterRole` permissions (`get`, `list`, `watch` on standard resources) and export a token-based kubeconfig.
> - In Docker deployments, mount cloud credentials read-only into the Pantau container. For example:
>   ```yaml
>   volumes:
>     - ./data:/app/data
>     - ${HOME}/.aws:/root/.aws:ro
>   ```

##### Security Model & Boundaries
- **Credential Rules**: To protect the Pantau host from arbitrary file read attacks, Pantau rejects kubeconfigs with file-path credentials (`certificate-authority`, `client-certificate`, `client-key`, `tokenFile`). Credentials must be inlined (`*-data` base64) or provided as direct tokens.
- **Plugin Command Allowlist**: Only approved CLI tools are permitted in `exec` configuration blocks: `aws`, `aws-iam-authenticator`, `gke-gcloud-auth-plugin`, `gcloud`, `kubelogin`, `az`, `doctl`, and `oci`. Path separators (`/` or `\`) and command-based `auth-provider` configurations are rejected.
- **Zero API Exposure**: Kubeconfig data is stored in the database like private SSH keys, backed up in encrypted System Snapshots, and never exposed in REST API responses.

##### Operational Limits
- **Interactive Pod Exec Unsupported**: The interactive Terminal Dock shell relies on an SSH PTY connection to a host. Because external clusters operate via local `kubectl`, interactive container exec is disabled and hidden in the UI.
- **Live WebSocket Logs Supported**: Real-time log streaming (`/ws/kube/logs`) is fully supported for pods and workloads.
- **Single Endpoint**: External clusters do not feature host failover.

##### Troubleshooting
- **`kubectl not found`**: Ensure `kubectl` is installed and reachable in `PATH` of the Pantau process (official Pantau Docker images include `kubectl` out of the box).
- **`plugin not allowed`**: The `exec` command in your kubeconfig is not on the allowed list or contains directory paths. Use one of the supported plugins (`aws`, `kubelogin`, etc.).
- **`file-path credential rejected`**: The kubeconfig points to local files on your machine (e.g. `certificate-authority: /path/to/ca.crt`). Flatten the file using `kubectl config view --raw --flatten` before pasting.
- **`cluster verification failed`**: The Pantau server could not authenticate or reach the cluster API endpoint. Verify network egress, firewall rules, and cloud credentials in the Pantau process environment.

### Managing Cloudflare via REST API v4

Pantau allows operators to connect multiple Cloudflare accounts and manage domain zones, DNS records, cache invalidation, and essential security settings directly from the web interface without juggling multiple browser tabs or installing third-party command-line utilities.

#### Connecting a Cloudflare Account

1. In the **Side Panel** or main navigation, select **Cloudflare**.
2. Click **Add Account**.
3. Provide an identifiable name (e.g., `Production Cloudflare`, `Personal Projects`), an optional **Account ID** (required only if using account-scoped tokens), and your **API Token**.
4. Pantau automatically performs pre-flight verification against `https://api.cloudflare.com/client/v4/user/tokens/verify` (or `/accounts/{id}/tokens/verify`) before persisting the record in SQLite.

> [!IMPORTANT]
> **Recommended Least-Privilege API Token Permissions**:
> Never use unscoped Global API Keys. In your Cloudflare dashboard under **My Profile → API Tokens**, create a custom token limited to your specific zones with the following minimum permissions:
> - `Zone - Zone - Read`: view zone list and status.
> - `Zone - DNS - Edit`: view, add, update, and delete DNS records.
> - `Zone - Cache Purge - Purge`: purge entire zone cache or specific URLs.
> - `Zone - Zone Settings - Edit`: modify SSL mode, HTTPS redirects, security levels, and TLS versions.

#### Zone & DNS Records Management

- **Zones Overview**: Select an account to view all assigned domains with real-time status (`active`, `pending`), plan type, paused state, and authoritative name servers. Use the instant search bar to filter zones.
- **DNS Records Table**: View records by name, type (`A`, `AAAA`, `CNAME`, `TXT`, `MX`, `NS`, `SRV`, `CAA`), content, and TTL (Auto / custom seconds).
- **Add & Edit Records**: Modal forms validate required names and contents (bounded up to 255/2048 characters), enforce priority for MX records, and provide a toggle for Cloudflare proxying (orange cloud).
- **Delete Confirmation**: Deleting a record triggers a confirmation modal to avoid accidental record loss.

#### Cache Invalidation (Purge Cache)

The **Cache** tab offers two purging modes:
1. **Purge Everything**: Purges the entire Cloudflare edge cache for the active zone. Guarded by a confirmation dialog warning of temporary backend origin server load spikes.
2. **Purge by URL**: Specify exact URLs line by line (up to 30 files per request, complying with Cloudflare API thresholds) to invalidate targeted assets without evicting the entire edge cache.

#### Curated Zone Settings Allowlist

Pantau exposes an allowlist of high-impact zone settings via the **Settings** tab:
- **SSL / TLS Encryption**: `off`, `flexible`, `full`, `strict`.
- **Always Use HTTPS**: redirect all HTTP requests to HTTPS (`on` / `off`).
- **Security Level**: adjust challenge thresholds (`off`, `essentially_off`, `low`, `medium`, `high`, `under_attack`).
- **Development Mode**: bypass edge cache for 3 hours for real-time origin asset updates (`on` / `off`).
- **Minimum TLS Version**: enforce legacy cipher deprecation (`1.0`, `1.1`, `1.2`, `1.3`).
- **Automatic HTTPS Rewrites**: upgrade mixed content links automatically (`on` / `off`).

#### Security Model & Non-Goals

- **Credential Protection**: API tokens are stored in the local SQLite database similarly to SSH private keys, excluded from all REST API responses (`json:"-"`), redacted in logs, and included in encrypted **System Snapshots** (AES-256-GCM).
- **Hardcoded Endpoint & Input Validation**: All outbound requests strictly target `https://api.cloudflare.com/client/v4` (mitigating SSRF), and URL identifiers (`zoneID`, `recordID`) are validated against `^[0-9a-f]{32}$` to prevent path injection.
- **Explicit Non-Goals**: Cloudflare Workers, R2 Storage, Cloudflare Tunnels (`cloudflared`), custom WAF/Firewall rules, and Page Rules remain out of scope and must be managed via Cloudflare's dashboard.

### Ansible Playbooks (Semaphore-Style, Agentless)

Pantau includes an agentless Ansible automation subsystem inspired by Semaphore UI. Operators can write and store Ansible Playbooks in Pantau's local database, execute them against registered hosts, stream real-time task output, and review complete run histories—with **zero separate inventory files or external key vaults**.

The **Ansible** section has five tabs: **Playbooks** (dense table with search, filters, sorting, grouping and a per-row menu; a playbook opens in a side drawer with Overview, Edit, Runs and Schedule), **Runs** (History of every run with status filters, and Scheduled), **Hosts** (Group > Host explorer with Ping and Facts, plus Ad-hoc), **Resources** (Templates, Files and Library) and **Settings** (Ansible Gate, `ansible.cfg`). The gate status stays visible in the header; click it to open Settings. Related decisions: [ADR 0049](adr/0049-ansible-playbooks-semaphore-style.md), [ADR 0050](adr/0050-ansible-git-playbooks-and-cron-schedules.md), [ADR 0057](adr/0057-ansible-ops-recap-tasktree-diff-survey-adhoc-facts-explorer.md) and the layout in [ADR 0059](adr/0059-ansible-ui-playbook-table-and-side-drawer.md).

#### The Ansible Gate (Security & Enablement)

Ansible Playbooks run **directly on the Pantau server with the privileges of the Pantau process**. Because Ansible features such as `connection: local`, `delegate_to: localhost`, `local_action`, and lookup plugins execute commands or read files on the master controller, playbooks cannot be safely sandboxed.

To prevent unauthorized execution:
1. The **Ansible Gate** (`ansible_enabled`) is **disabled by default**.
2. Before any playbook can run, an administrator must enable the gate with the **Enable Gate** button (shown on every Ansible tab while the gate is off, and in **Ansible → Settings**). Until then only **Settings** is usable.
3. Enabling the gate requires confirming an explicit security prompt acknowledging that admin access in Pantau with Ansible enabled is equivalent to local process execution on the server host.
4. The system also verifies that the `ansible-playbook` binary is installed and reachable in `PATH`.

#### Writing & Storing Playbooks

1. In the **Side Panel**, navigate to **Ansible → Playbooks** and click **New Playbook**.
2. Provide a descriptive **Name** (1–64 characters, unique case-insensitive) and an optional **Description**.
3. (Optional) Set a **Group** (`group_name`, free text up to 64 characters, suggestions appear from existing groups). Leave empty for an ungrouped playbook.
4. Write the YAML playbook content (up to 256 KiB) in the integrated CodeMirror YAML editor.
5. (Optional) Provide default **Extra Vars** formatted as a valid JSON object (up to 64 KiB).
6. Save the playbook. Playbook definitions are stored in SQLite and automatically included in encrypted **System Snapshots**.

#### Playbook Grouping & Organization

Playbooks can be organized into visual groups modeled after Host Groups:
- **Setting a Group**: Enter any group name (trimmed, up to 64 characters) in the playbook editor. Groups are implicit—created whenever a playbook uses them and removed when none do.
- **Group by**: In the Playbooks table, the **Group by** toggle renders rows under collapsible group headers with playbook counts. Click a header to collapse or expand it; collapse state persists across reloads.
- **Filtering**: Use the search box (press `/` to focus it) and the **All / Failed / Never run / Scheduled / Git** chips to narrow the table; columns sort on click.
- **Cosmetic Labeling**: Groups are organizational labels only; they do not alter execution behavior, inventory generation, runner credentials, or permissions.
#### Running a Playbook

Click **Run** on a playbook row, or in its drawer, to open the launch panel. It remembers your last targets and options per playbook, and **Ctrl+Enter** starts the run:

1. **Target Selection**:
   - **Specific Hosts**: Select individual target hosts from the searchable host picker.
   - **Host Groups**: Select one or more Host Groups via quick chips. Target hosts are dynamically calculated as the union of chosen individual hosts plus all hosts belonging to selected groups.
   - Up to 200 target hosts are supported per execution run.
2. **Execution Modes**:
   - **Run (`run`)**: Normal playbook execution against targets.
   - **Check (`check`)**: Dry-run simulation using `--check` (no actual host changes applied).
   - **Diff (`diff`)**: Change comparison using `--check --diff` to preview configuration drifts.
   - **Syntax (`syntax`)**: Validates YAML syntax and playbook structure using `--syntax-check` (can be run with 0 target hosts).
3. **Runtime Parameters**:
   - **Extra Vars**: Override default JSON variables for this run.
   - **Tags & Skip Tags**: Comma-separated tag filters matching `^[A-Za-z0-9_.,:-]{1,256}$`.
   - **Limit**: optional Ansible host pattern (`--limit`) that narrows the run to a subset of the selected targets, e.g. `web01,db03` or `&web:!web02`. Allowed characters are letters, digits, `_ . , : ! & * [ ] -` (1–256 characters); spaces, `;`, `$` and backticks are rejected and only Generated Inventory names (sanitised Host names and Host Groups) can be matched. Empty means no limit.
   - **Verbosity**: `0` (normal), `1` (`-v`), `2` (`-vv`), `3` (`-vvv`) or `4` (`-vvvv`). Higher levels add task results and connection debugging; they also make the log much longer.
   - **Presentation**: Runs in mode `run` print ANSI-colored output, because the log keeps the raw escape codes and the viewer colorizes them. The **Log | Task Tree | Recap | Diff** switch at the top of the run viewer lets you read the same run the other three ways (see below).
   - **Forks**: Number of parallel host connections (1–20, default 5).
   - **Become (Sudo)**: Toggle `--become` for passwordless sudo privilege escalation.

#### Generated Inventory & Clean Child Environment

When a run starts, Pantau performs the following automated steps:
1. **Dynamic JSON Inventory**: Generates a temporary `inventory.json` file structuring hosts into `all.hosts` and `all.children.<group>.hosts`. Connection variables (`ansible_host`, `ansible_port`, `ansible_user`, and `ansible_python_interpreter=auto_silent`) are automatically injected.
2. **Isolated Key Files**: Resolves the effective SSH private key for each target host from the **SSH Key Library** or host custom key, writes them to `0600` files inside a temporary `0700` workspace directory (`os.MkdirTemp`), and references them via `ansible_ssh_private_key_file`.
3. **Sterile Environment**: The child `ansible-playbook` process inherits only essential variables (`PATH`, `LANG=C.UTF-8`, `HOME=<workspace>`, proxy settings, and `ANSIBLE_*`). A dedicated temporary `ansible.cfg` disables host key checking (`host_key_checking = False`), prevents loading unexpected configurations from `/etc/ansible` or `~/.ansible.cfg`, and runs `ssh` with `-F /dev/null` so a bind-mounted `~/.ssh/config` (common in Docker) can never break the connection (`Bad owner or permissions on /root/.ssh/config`).
4. **Guaranteed Workspace Cleanup**: The entire temporary workspace—including private key files and inventory—is unconditionally deleted when the run finishes, is canceled, or encounters a fatal crash.

#### Playbooks from a Git Repository

Set **Source** to **Git repository** in the playbook editor instead of writing YAML inline:

| Field | Notes |
|---|---|
| Repository URL | `https://host/org/repo.git`, `ssh://git@host:port/org/repo.git` or `git@host:org/repo.git`. Credentials inside the URL, `file://`, `git://`, `http://` and local paths are rejected. |
| Branch or tag | Optional; empty uses the remote's default branch. |
| Playbook path | Relative path to a `.yml`/`.yaml` file inside the repository, e.g. `playbooks/site.yml`. Symlinks leaving the repository are rejected. |
| Authentication | **None** (public repo); **Access token** (`https://` only, e.g. GitHub/GitLab personal access token with read access); **SSH key from library** (`ssh://` / `user@host:path` only, key from the *SSH Key Library*). |

On every run Pantau shallow-clones (`--depth 1`) the repository into the private run workspace, runs `ansible-playbook` from the clone (so `roles/`, `group_vars/`, `host_vars/` and `files/` next to the playbook work) and deletes it afterwards. The repository's own `ansible.cfg` is ignored. The checked-out **commit** is printed at the top of the log (`[pantau] git commit <sha>`) and shown next to the run in the history. Notes:

- The access token is write-only: it is stored like an SSH key, never returned by any API, never put in command-line arguments or logs, and leaving the field empty on edit keeps the stored token.
- `git` must be installed on the Pantau server (included in the Docker image). The editor warns when it is missing.
- Submodules and `ansible-galaxy` requirements are not fetched; keep roles inside the repository or install them in the **Library** tab.
- Deleting an SSH key that a playbook uses is not blocked; the next run then fails with a clear message.
- A repository's plugins (`library/`, `action_plugins/`, `lookup`) run on the Pantau server like any playbook content. Only point Pantau at repositories you trust.

#### Importing Playbooks from a Folder

Use **Ansible → Playbooks → Import Folder** to copy playbook YAML files from a local project into Pantau. Imported playbooks are ordinary inline playbooks: Pantau stores the text, you edit it in Pantau, and nothing of the folder is kept or linked.

1. Select the folder in your browser (not a path on the Pantau server). `.git`, `__pycache__`, `.DS_Store`, `.ansible/`, and `collections/` are skipped.
2. Pantau lists the YAML files it finds and **preselects the ones that are playbooks** (a top-level `hosts:` or `import_playbook:`), like the host import preview. Tick the playbooks you want (select all/none is available) and adjust their names; names must be unique. Each file may be 1 byte–256 KiB; up to 100 playbooks per import.
3. Click **Import Folder**. If any name is already taken, nothing is imported. Open a new playbook to edit it and fill in its variables, and start with **Syntax Check** before the first run. The playbook's `hosts:` patterns must match aliases and Host Groups in Pantau's Generated Inventory.

The YAML is stored in SQLite (included in encrypted **System Snapshots**): check it for secrets first. Only the ticked file is copied: files a playbook references next to itself (templates, `vars_files`, `include_tasks`, `import_playbook` targets) are **not** imported, so import such targets as playbooks of their own or paste their content in.

**Inventory, roles, collections, and `ansible.cfg` always come from Pantau, never from the folder**: Pantau generates its own inventory and SSH keys, the **Library** provides roles and collections, and `ansible.cfg` settings such as `forks`, `pipelining`, `interpreter_python`, or `become_method` go in **Ansible → Settings → Ansible Configuration**. Absolute Git-key paths, files outside the playbook, and executables installed only on your laptop do not become available on the Pantau server. Only import trusted playbooks: playbooks and local plugins can still execute code with Pantau process privileges.

#### Ansible Configuration (`ansible.cfg`)

**Ansible → Settings → Ansible Configuration** holds global `ansible.cfg` additions applied to every run, as plain INI text:

```ini
[defaults]
forks = 10
interpreter_python = auto_silent

[ssh_connection]
pipelining = True
```

Allowed sections: `defaults`, `ssh_connection`, `privilege_escalation`; one `key = value` per line, no multi-line values. Keys Pantau manages are rejected with the offending line number: `inventory`, `private_key_file`, `ssh_args`, `host_key_checking`, `retry_files_enabled`, `nocows`, `roles_path`, `collections_path(s)`, interactive password prompts (`ask_pass`, `ask_vault_pass`, `become_ask_pass`) and password-file keys. Without an explicit forks option on the run, `forks` from this configuration applies (Ansible default: 5). The configuration is included in System Snapshots.

#### Library: Roles and Collections

The **Library** sub-tab of **Ansible → Resources** manages roles and collections stored on the Pantau server and available to **every** playbook (inline, Git, or imported folder), so projects no longer need to vendor them:

- **Install from Galaxy**: choose Collection or Role, enter `namespace.name` (roles: the Galaxy name, e.g. `geerlingguy.nginx`) and an optional version. Collections install their dependencies. The installer output streams into the panel below the form while it runs (the command line first, then a `… still working (Ns)` line every 5 seconds while `ansible-galaxy` is silent); it can take a few minutes. The install finishes on the server even if you close the tab.
- **Install from requirements.yml**: paste a `requirements.yml` (`collections:` and/or `roles:`). Only names and versions are accepted; Git/URL sources are rejected.
- **Import Local**: upload a collection folder (with `galaxy.yml` at its root) or a collection tarball (`.tar.gz`, e.g. from `ansible-galaxy collection build`/`download`), or a role folder (give it a name). Local installs do not fetch dependencies, so they work offline.
- Use the trash icon to remove an item.

The library lives in `ansible-library/` next to the Pantau database (in Docker: keep it on the same volume). It is **not** part of System Snapshots: after restoring to a new machine, install the needed items again. This needs `ansible-galaxy` (installed together with ansible-core); the tab says so when it is missing. Only install content you trust: collection and role plugins run on the Pantau server with its privileges.

#### Templates and Files (for `template:` and `copy:`)

An inline playbook is a single YAML text, so a task such as `template: src=site.conf.j2` or `copy: src=ca.pem` would otherwise find nothing to read. The **Templates** and **Files** sub-tabs of **Ansible → Resources** store those supporting files in Pantau:

- **Templates**: type a name (subfolders allowed, e.g. `nginx/site.conf.j2`) and the Jinja content, then **Save template**. Saving an existing name replaces it; the pencil icon loads a template back into the editor. Templates must be UTF-8 text, up to 256 KiB.
- **Files**: choose one or more files (any type, binary included), optionally a **Subfolder** such as `certs/`, and **Upload**. Uploading an existing name replaces it. Up to 5 MiB per file.
- Use them from the playbook exactly as in a normal Ansible project:

  ```yaml
  - ansible.builtin.template:
      src: nginx/site.conf.j2     # Templates tab
      dest: /etc/nginx/conf.d/site.conf
  - ansible.builtin.copy:
      src: certs/ca.pem           # Files tab (subfolder certs/)
      dest: /etc/ssl/certs/ca.pem
      mode: "0644"
  ```

  Template variables come from the playbook's `vars:`, the default extra vars, or the extra vars of the run.
- Every run of an **inline** playbook (manual or scheduled) gets all stored templates and files in its temporary workspace. They are shared by all inline playbooks; deleting one makes the playbooks that reference it fail. Playbooks that come from **Git** use the `templates/` and `files/` folders of their own repository instead.
- Limits: 32 MiB in total. Templates and files are stored in the database and included in encrypted System Snapshots, so do not upload secrets you would not keep in a backup. Uploaded files cannot be downloaded again.
- Jinja is not checked when you save; mistakes show up in the run log. Use **Check** or **Diff** mode to try a template without changing hosts.

#### Editing Playbooks and Variables

Click a playbook row (or choose **Edit** in its `⋯` menu) to open the side drawer. Its tabs are **Overview**, **Edit**, **Runs** and **Schedule**; the **Edit** tab holds the editor:

- **Pre-tasks / Roles / Tasks / Post-tasks / Handlers / Raw** tabs (inline playbooks with exactly one play): the same playbook cut into parts, in the order Ansible runs them. **Pre-tasks**, **Tasks**, **Post-tasks** and **Handlers** are YAML lists, **Roles** is a picker over the roles installed in the Library (Library roles, roles of Library collections as `namespace.collection.role`, and system roles) with per-role parameters (`vars`, `tags`, `when`) and ordering, and **Raw** is the full YAML. Name, hosts, become, vars and every other play key have no tab: edit them in Raw, parts carry them through unchanged. Parts are joined into the raw text when you leave a part tab for a non-part tab or Save, and the raw text is split again when you open a part tab after editing Raw. Comments and quoting inside each part are kept, but joining re-indents the YAML, so only edit parts when you want that. A part that is not valid YAML (for example Tasks that is not a list) keeps you on that tab with the reason. Playbooks with several plays, or YAML that does not parse, only offer **Raw**; a role that is not installed is flagged *not installed* and the run fails unless it exists.
- **Variables** tab: the variables the playbook references, found by scanning the YAML as you type (unsaved edits included) together with the roles it uses: roles in the repository, and roles installed in **Resources → Library** or the system role paths (`/usr/share/ansible/roles`, `/etc/ansible/roles`), including `namespace.collection.role` roles of Library collections. A role's `defaults/`, `vars/`, tasks, handlers, templates and `meta` dependencies are read, so a variable the role defines itself (`ansistrano_repo_dir`) or sets while it runs (`set_fact`/`register`, e.g. `ansistrano_release_path`) is not something you have to supply. **Required** = used but defined nowhere (it needs a value from you or from the inventory); **Optional** = only used with `default()`/`is defined`; **Defined** = has a value in the YAML (`vars:`), a role or the playbook's default extra vars. Variables produced at run time and Ansible magic variables are skipped. This is a static heuristic: dynamically built names and variables supplied by your own inventory can be missed. Task files a role includes through a templated name (`include_tasks: "{{ mode }}.yml"`) are only mined for the facts they set; what they reference is ignored because the file that runs is not known.
  - The **Value** column is the playbook's **default extra vars**: type a value to set or override a variable, click the trash icon to delete it (the variable then falls back to what the YAML defines). **Add variable** creates one the scan cannot see. Values are parsed as JSON when possible (`8080` is a number, `true` a boolean, `["a","b"]` a list); wrap a value in quotes (`"8080"`) to force a string. Required variables without a value are highlighted.
  - A variable defined in the YAML's `vars:` block (with no override) also has a trash icon: it removes that entry from the YAML in the editor (Cancel discards it). Only a plain `vars:` block is recognised; otherwise edit in the Raw tab. A variable that is only referenced and defined nowhere cannot be deleted (the icon is disabled).
  - Changes are stored with **Save**, like the rest of the editor. A run can still override any of them in the launch dialog.
  - **Save keeps you where you are**: same drawer tab, same editor tab, same editor. For a new playbook the first Save turns the drawer into that playbook's Edit tab, on the same editor tab.
  - **Save checks and formats inline YAML first**: the playbook must be a list of plays, each with `hosts` or `import_playbook`, with no duplicate keys in a mapping, and it is re-indented to 2 spaces (comments and quoting are kept; spacing before a trailing `#` comment is normalised to one space). When the check fails nothing is saved, the editor switches to **Raw** and the footer shows the reason with its line number. The check is about YAML structure only; it does not run `ansible-playbook --syntax-check`, so unknown modules or missing roles still surface at run time (use the *syntax* mode of the launch panel for that).
  - Variables are listed under **Playbook variables** and one collapsible section per role (click a header to fold it; a role's own defaults and the variables only its files use). The search box above the table filters by name, value, source, role or file and opens every group while active. The table uses the whole drawer: expand the drawer to see the Source and Used-in columns side by side, a narrow drawer hides them.
- **Files** section (Git playbooks only, after the first Save): browse, edit, create and delete files of a fresh shallow clone and commit them back, see *Git Repository Editor* below. `.env`, keys, binaries, `.ansible/`, and `collections/` are never shown. Limits: 1,000 files, 4 MiB total, 256 KiB per file.


#### Schedules (Cron)

Schedules live in two places: the **Schedule** tab of a playbook's drawer (create, edit, pause and delete that playbook's schedules, with a preview of the next occurrences) and **Ansible → Runs → Scheduled**, which lists every schedule. Both need the Ansible Gate; while it is off every Ansible tab except **Settings** explains that instead of loading data:

1. Click **New Schedule**, pick the playbook, an optional name, and a **cron expression** (5 fields: minute, hour, day-of-month, month, day-of-week). Presets: every 5 minutes (`*/5 * * * *`), hourly (`0 * * * *`), daily 03:00 (`0 3 * * *`), Sunday 03:00 (`0 3 * * 0`), monthly (`0 3 1 * *`). Supported syntax: `*`, numbers, ranges `a-b`, lists `a,b`, steps `*/n` and `a-b/n`, and `@hourly`, `@daily`, `@weekly`, `@monthly`, `@yearly`. Month/day names are not supported. If both day-of-month and day-of-week are restricted, either may match (Vixie cron).
2. Choose the targets (a **Host Group** and/or individual hosts), mode (`run`, `check`, `diff`, `syntax`), tags, extra vars, **Limit**, verbosity (0–4) and become. Targets are resolved when the schedule fires, so hosts added to the group later are included, while the limit narrows that resolved set the same way it does for a manual run.
3. Times use the **Pantau server time zone** (shown in the dialog; in Docker set `TZ`, e.g. `TZ=Asia/Jakarta`, otherwise UTC).
4. The table shows the next run, the last run (click the run number to open its log) and the last error. Use the switch to pause/resume a schedule without deleting it.

Rules: scheduled runs use the same limits as manual runs (1 active run per playbook, 3 globally); a schedule that finds the playbook busy or the limit reached is **not queued**: it records `not started: …` and waits for the next occurrence. Occurrences that are more than **5 minutes late** (Pantau was stopped, or the Ansible Gate was off) are **skipped, never replayed**, and the last error shows `missed occurrence … skipped`. Runs started by a schedule are tagged *scheduled* in the history. Schedules are included in the encrypted System Snapshot (run history is not).

#### Real-Time Log Streaming & Cancellation

1. **Live Monospace Viewer**: Output from stdout and stderr is combined and streamed live into the run viewer (side drawer) using 1-second polling (`GET /api/ansible/runs/{id}/log?offset=N`) with search and a **Follow** toggle for auto-scroll.
2. **Log Limits**: Active run logs are buffered in memory up to 2 MiB. If output exceeds 2 MiB, the head of the log is truncated and marked with `[pantau] log truncated`.
3. **Cancellation**: Click **Cancel** in the viewer to terminate an in-flight run. Pantau sends SIGTERM to the process group, followed by SIGKILL after 5 seconds if the process does not exit cleanly.
4. **Timeout**: Runs are bounded by a default 1-hour timeout.
5. **Interrupted Runs**: If the Pantau daemon restarts or terminates while runs are `queued` or `running`, they are automatically marked as `interrupted` upon server startup.

#### Run History, Retention & Concurrency Limits

- **Concurrency Limits**:
  - Maximum **1 active run per playbook** (additional requests rejected with HTTP 409).
  - Maximum **3 active runs globally** across the entire Pantau instance (further concurrent runs rejected with HTTP 429).
- **Retention Policy**:
  - Pantau automatically retains the latest **100 finished runs per playbook**. Older runs are pruned automatically.
  - Run histories are stored locally in SQLite (`ansible_runs`) and are excluded from system snapshots to preserve compact backup sizes.

#### Reading a Run: Log, Task Tree, Recap & Diff

Click a row in **Ansible → Runs**, or a run in a playbook's **Runs** tab or schedule history, to open the run viewer in the side drawer. A run that is still active updates live and has a **Cancel** button. The viewer offers four views of the same output. They are all derived from the log that Ansible printed, so a run whose log was truncated at 2 MiB shows an incomplete tree; the recap is different (see below).

- **Log**: the raw stream, with ANSI colors rendered safely (the text is HTML-escaped before any color is applied, so nothing from the target host can inject markup).
- **Task Tree**: a collapsible **Play > Task > Host** tree where each host line carries its status — `ok`, `changed`, `fatal`, `failed`, `skipping`, `unreachable`, `ignored`. Use the search box to find a task by name and the status filter pills to hide the noise (e.g. show only `failed` and `unreachable`). Raising verbosity gives the tree more to work with.
- **Recap**: the `PLAY RECAP` of the run, parsed by the server when the run finished and stored with it. Each host gets a colored bar (ok green, changed orange, unreachable dark red, failed bright red, skipped grey/blue) plus the totals. The history list shows a mini recap bar per run, so a failed run is visible without opening it.
- **Diff**: only meaningful for runs in mode `diff`; the `--- before` / `+++ after` / `@@` blocks are split per file with added lines in green and removed lines in red. Modes `check` and `run` never produce a diff block.

#### Survey (Typed Launch Questions)

A playbook can carry a **Survey**: typed questions that are asked every time the playbook is launched. Define it in the playbook editor's **Survey** section (add, remove, reorder fields) or through `PUT /api/ansible/playbooks/{id}/survey`:

| Field | Notes |
|---|---|
| Key | Name of the extra variable, `^[A-Za-z_][A-Za-z0-9_]{0,63}$`, unique within the survey. |
| Type | `text`, `textarea`, `number`, `integer`, `select`, `multiselect`, `boolean`, `password`. |
| Options | For `select` / `multiselect`: 1–50 unique values. |
| Min / Max | Numeric bounds for `number` / `integer`; character length for `text`, `textarea`, `password` (whole, non-negative numbers). |
| Default | Pre-filled answer; also used when a non-required field is left empty. |
| Required | The run is rejected server-side (not only in the browser) when an answer is missing. |

Limits: at most 30 fields, keys are unique. The server returns HTTP 400 for an answer with an unknown key (answers go straight into the run's `-e` file) and for a **required** field left without an answer and without a default. When a playbook has a survey, the Run dialog replaces the free-form JSON box with the form (the JSON box stays available for anything the survey does not cover).

**Which value wins.** At launch Pantau merges, from weakest to strongest: the playbook's default extra vars, then the survey answers (including defaults for unanswered optional fields), then the extra vars typed explicitly in the launch dialog. So a survey answer overrides the playbook's `8000` default, and an operator who types `{"port": 9000}` in the launch extra vars still gets `9000`.

**Passwords.** A `password` field is passed to Ansible for the run but never written to the run record — `extra_vars` shows `(hidden)` — and never returned by the API. It is **not** scrubbed from the log: a task that prints it (`debug: var=secret`, `-vvv` output, a failing command that echoes it) will show the value in the run log, which the whole admin team can read. Prefer passphrase-free mechanisms (SSH keys, files from the **Files** tab) and treat a printed password as exposed.

The survey definition is stored with the playbook in SQLite, included in encrypted System Snapshots, and exposed to the UI as `survey` (fields) and `has_survey` on the playbook GET/list responses.

#### Ad-Hoc Runs (One Module, No Playbook)

**Ansible → Hosts → Ad-hoc** runs a single Ansible module against one or more Hosts or a Host Group. Use it for quick checks and one-off operations that do not deserve a stored playbook: `ping`, `setup`, `shell: uptime`, `command: df -h`, `service: name=nginx state=restarted`, `apt: update_cache=yes`, and so on.

1. Pick a **Module** from the common list or type one (`ansible.builtin.ping` and `ping` are both accepted) and optionally its **Arguments** (one `-a` string, up to 4 KiB; leave empty for modules without arguments, like `ping`).
2. Choose the **targets**: individual Hosts, one Host Group, and/or an extra **Pattern** (default `all`, same rules as the run **Limit**).
3. Optionally toggle **Become**, set **Forks**, **Verbosity** (0–4) and extra vars (a JSON object, up to 64 KiB).
4. Run it: the log streams live below the form and a per-host result table fills in as hosts answer (success green, changed orange, unreachable dark red, failed red).

Ad-hoc runs are real: `shell`, `command` and `raw` execute on the target with the same SSH access Pantau already has, which is equivalent to the terminal. They are still confined to the Generated Inventory: the pattern can only address Pantau Host aliases and Host Group names, so `connection: local`, `delegate_to` and `local_action` cannot be used to escape to another machine through the pattern itself.

**Concurrency and history**: at most **3 ad-hoc runs** may be active at once across the installation; the fourth request is rejected (HTTP 429). This quota is counted **separately** from playbook runs, so a quick `ping` never waits behind a scheduled playbook and vice versa. History keeps the latest 100 ad-hoc runs in `ansible_adhoc_runs`; like playbook runs they are excluded from System Snapshots, and any run still active when Pantau stops is marked `interrupted` at the next start. An active run can be cancelled like a playbook run.

#### Host & Group Explorer (Ping and Facts)

**Ansible → Hosts** shows the fleet exactly as Ansible sees it: a collapsible tree of **Host Group > Host** plus an *Ungrouped* list. Every row shows the group the host belongs to through Pantau's Host Groups, the host's Pantau inspection badge (healthy, warning, drift, unknown), and the host's **Ansible alias** — the sanitised name generated inventory actually uses, so you can copy it straight into a playbook's `hosts:` line or the run **Limit**.

- **Ping**: click **Ping** (whole tree or selected hosts) to start an ad-hoc `ansible … -m ping`. Each host then shows **Reachable**, **Unreachable**, **Failed**, or *not checked* until the next ping. This tests the Ansible path specifically: SSH reachability, credentials from the SSH Key Library, and the Python interpreter on the target — a host Pantau can inspect may still be unreachable to Ansible if `ansible_python_interpreter` is missing.
- **Facts**: the Facts button opens the **Facts viewer** for one host: cards for OS (distribution, version, kernel, architecture), hostname/FQDN, CPU (count and model), memory and swap, mounts, network interfaces (IPv4/IPv6/MAC), default IPv4, uptime, Python interpreter and virtualisation, plus a **Raw JSON** tab with search over the full `ansible_facts`.

Facts are gathered by running `setup` on the host (up to 90 seconds, 2 MiB of output) and are **cached per host**: the viewer shows when they were gathered, and **Refresh facts** collects them again. Only one gather per host runs at a time (a second request gets HTTP 409), a failure or unreachable host returns a short error instead of a partial summary, and the cache is deleted together with the Host. Facts are operational data, not configuration: they are not included in System Snapshots.

#### Git Sync (Verifying a Git Playbook)

For playbooks whose source is a Git repository, the editor shows the commit and time of the last successful sync plus a **Sync Now** button. Sync Now shallow-clones the repository with the stored credentials, checks that the configured playbook path still exists, and reports the **commit**, **branch** and the **sorted `.yml`/`.yaml` paths** found in the repository (dot directories such as `.git` or `.ansible/` and `node_modules` are skipped; at most 1,000 entries). The temporary clone is always deleted. The result is remembered as `last_sync_at` and `last_sync_commit`, so you can tell whether the branch moved since you last looked. A clone, network or authentication failure returns HTTP 502 with a short message that keeps the last lines of git's output but replaces the repository URL with `<repo>` and strips the credential, so a token never appears in the response or the server log. A repository that clones fine but does not contain the configured playbook path returns HTTP 400 instead.

Sync does **not** store the repository content and does not change the playbook: every run still clones fresh. Use it after changing the branch or path, and after rotating a token or key, to catch typos before a schedule finds them. It applies to Git sources only; other sources return HTTP 400.

#### Git Repository Editor (Edit, Commit & Push)

For playbooks with `source = "git"`, the playbook drawer's **Edit** tab has a **Files** section (the in-browser **Git Editor**). It loads a fresh shallow clone the first time you open it (this can take a few seconds); a clone failure shows the git error with a **Retry** button. Save the playbook first: Files is only available for stored playbooks.
- **Commit & Push** is in the drawer footer of the Files section and stays disabled until a file is modified.

- **Browse & Search**: Navigate the repository tree with collapsible folders, instant file search, and indicators for modified (`M`), added (`A`), or deleted (`D`) files.
- **Edit & Syntax Highlighting**: Edit files with full syntax highlighting via CodeMirror (YAML, shell, HCL/C-like, plain text).
- **File Operations**: Use the toolbar to create **New File**, **Delete** files, **Discard changes**, or **Reload** repository contents. If unsaved edits exist, reloading triggers a confirmation prompt.
- **Commit & Push Dialog**:
  - Enter a descriptive commit message (required, 1–500 chars).
  - Author name and email are remembered in `localStorage` (defaults to `Pantau <pantau@localhost>` if left blank).
  - Destination choice: **Push to branch `{branch}`** or **Create new branch** (enter a valid Git branch name).
  - Strictly non-force push: `--force` is never executed.
- **Stale-Repo Optimistic Concurrency (HTTP 409)**: If the remote repository has advanced (`base_commit` does not match remote HEAD), the push is rejected with HTTP 409 (`ErrRepoChanged`). The dialog prompts you to reload and reapply changes safely without silently clobbering upstream commits.
- **Write Permission Requirements**:
  - **GitHub Fine-Grained Personal Access Tokens**: Requires **Repository permissions -> Contents: Read and write**.
  - **GitHub Classic Tokens**: Requires `repo` scope (or `public_repo`).
  - **GitLab Access Tokens**: Requires `write_repository` scope.
  - **SSH Deploy Keys**: The key added to the repository must have **Allow write access** checked.
  - Read-only tokens will fail at push time with an HTTP 502 error displaying the sanitized git error output.
- **Side Effects**: Successfully pushing a commit to the playbook's active branch automatically updates the playbook's `last_sync_at` and `last_sync_commit`.
- **Limits & Boundaries**: Maximum 50 modified files per commit, 1 MiB per file, 5 MiB total payload, valid UTF-8 text only (binaries return 415), no symlink traversal, and no `..` or `.git` paths.

### Terraform Management (Terraform Cloud-Style, Local CLI)

Pantau includes a self-hosted, centralized Terraform and OpenTofu management subsystem inspired by HCP Terraform (formerly Terraform Cloud). Operators can manage isolated **Workspaces**, run full execution pipelines (`plan` → `policy check` → `cost estimate` → `approval gate` → `apply`), track immutable **State Versions** with visual diffs and rollback, manage shared **Variable Sets**, enforce built-in **Policy as Code**, view **Private Module** documentation, schedule automated **Drift Detection**, and integrate with Git VCS and webhooks—with **zero remote backend dependencies**.

#### Enabling the Terraform Gate

Terraform configurations can execute arbitrary commands on the controller via `provisioner "local-exec"`, `data "external"`, and third-party modules. Because Terraform cannot be safely sandboxed without heavy virtualization:
1. The **Terraform Gate** (`terraform_enabled`) is **disabled by default**.
2. Before any workspace or run can be created or executed, an administrator must navigate to **Terraform → Settings** and toggle the gate on.
3. Enabling the gate requires confirming an explicit security prompt acknowledging that admin access in Pantau with Terraform enabled is equivalent to local process execution on the server host.
4. Pantau automatically inspects the server for local `terraform` and `tofu` binaries (as well as optional `infracost`). The active binaries and versions are displayed in the status banner.

#### Creating and Configuring a Workspace

A Workspace represents an isolated infrastructure environment (configuration, variables, state, and run history):
1. In the **Side Panel**, navigate to **Terraform → Workspaces** and click **New Workspace**.
2. Configure the workspace settings:
   - **Name**: Unique case-insensitive identifier (`^[A-Za-z0-9_-]{1,64}$`).
   - **Group**: (Optional) Free-text label (`group_name`, up to 64 characters) to categorize the workspace. Suggestions appear from existing workspace groups; leave blank for ungrouped.
   - **Environment**: Logical tag (`production`, `staging`, `development`).
   - **Binary**: Choose whether to run plans and applies using `terraform` or `tofu`.
   - **Tags**: Comma-separated labels for organizing and filtering workspaces.
   - **Configuration Source**:
     - **Inline Files**: Store up to 50 files (max 256 KiB per file; `.tf`, `.tfvars`, `.json`, `.yaml`, etc.) directly in Pantau's database.
     - **Git Repository**: Provide a repository URL (`https://` or `ssh://`), target branch, working directory within the repository (default root), and credentials (token or SSH Key from the Library).
3. Workspaces are allocated a private working directory on the server (`<DataRoot>/terraform/<workspaceID>`) with `0700` filesystem permissions.
4. **Templates**: the New Workspace modal has a template picker (Hello world, AWS S3 bucket, AWS EC2 instance, GCP storage bucket, Azure resource group, DigitalOcean droplet, Hetzner server, Cloudflare DNS record, Kubernetes namespace; `GET /api/terraform/templates`). Provider-specific templates need the matching **Cloud Account** linked (configured in Settings). Some templates have required variables without default (`bucket_name`, `zone_id`, `name`, `content`), and the first run needs network access to download the provider.

Clicking a workspace card or row opens the workspace in a **side drawer** docked on the right (same pattern as the Ansible drawer) with the tabs **Overview**, **Configuration**, **Runs**, **State**, **Variables**, **Drift** and **Settings**. Files and settings are edited in place: the **Configuration** tab edits inline files in a code editor (add/delete files, `Ctrl+S`, Save & plan; Git workspaces open the Git editor) and the **Settings** tab edits name, group, environment, tags, binary, drift interval and Git connection directly. The expand button widens the drawer to nearly the full screen (the choice is remembered); **Esc**, the backdrop or the close button closes it.

The **Configuration** tab editor has HCL syntax highlighting and two tools for inline workspaces (they work on the current, unsaved editor content):
- **Format**: runs `terraform fmt` / `tofu fmt` (`Ctrl+Shift+F`) on the file (`POST /api/terraform/workspaces/{id}/tools/fmt`).
- **Validate**: runs `init -backend=false` + `validate` (`POST /api/terraform/workspaces/{id}/tools/validate`), up to 2 minutes, max 2 concurrently. Diagnostics appear in a panel as `file:line`. Offline, validation only works with providers already cached or built in.

#### Workspace Grouping & Organization

Workspaces can be organized into visual groups modeled after Host Groups:
- **Setting a Group**: Enter a group name (trimmed, up to 64 characters) when creating or editing a workspace. Groups are implicit and created automatically from distinct values.
- **Collapsible Sections & View Modes**: Workspaces render under collapsible group headers in both Grid and List view modes with counts. Toggle headers to collapse or expand sections.
- **Filtering**: Use the Group dropdown to filter by group, show all, or isolate **Ungrouped** workspaces.
- **Cosmetic Labeling**: Groups are organizational labels only; they do not affect Terraform runs, state files, locking, or variable set inheritance.
#### Running a Pipeline: Plan, Approve, and Apply

Pantau enforces an auditable, multi-stage run lifecycle:

1. **Triggering a Run**:
   Click **New Run** on a workspace:
   - **Type**: Standard `plan` (forward plan to apply), `destroy` (plan destruction of managed resources), or `refresh` (refresh-only plan to reconcile state).
   - **Speculative Plan**: Run a temporary dry-run plan (useful for testing branch changes) that will not proceed to apply.
   - **Target / Replace**: Restrict execution to specific resource addresses using standard target syntax.
   - **Message**: Optional commit or execution note.
2. **Planning & Pre-Flight Checks**:
   Pantau materializes the latest state from the database, sets up clean environment variables, writes variables to `pantau.auto.tfvars.json` / `pantau-hcl.auto.tfvars`, and executes `init` and `plan -out=tfplan`.
   - Real-time stdout/stderr streams into the Console Log viewer.
   - A structured visual plan diff is generated showing resource additions (`+`), modifications (`~`), replacements (`-/+`), and deletions (`-`).
3. **Policy as Code Evaluation**:
   If policies are configured, Pantau automatically evaluates the plan JSON against active rules:
   - `advisory`: Informational warning; does not block approval.
   - `soft`: Blocks standard approval until explicitly acknowledged via **Override Policy** checkbox.
   - `hard`: Strictly rejects the run; apply cannot be executed (`policy_failed`).
4. **Cost Estimation (Infracost)**:
   If the `infracost` CLI binary is installed on the Pantau server and an `INFRACOST_API_KEY` is provided in environment variables, Pantau calculates the estimated monthly cost before and after the run, reporting the net cost delta. If Infracost is not installed, the step is skipped cleanly without failing the run.
5. **The Approval Gate**:
   When a run introduces infrastructure changes (`has_changes = true`), execution pauses at `awaiting_approval`:
   - **Confirm & Apply**: Applies the plan. An operator comment is **mandatory** (minimum 3 characters) for auditing.
   - **Discard**: Discards the planned changes. An operator comment is **mandatory**.
   - If policy checks generated `soft` warnings, the operator must tick **Override Policy** to enable the confirm button.
6. **Applying Changes**:
   When confirmed, Pantau runs `apply tfplan` using the retained execution workspace. Once finished, a new immutable **State Version** is recorded in SQLite, outputs are captured, and the temporary run directory is cleaned up.

#### State Versions, Diff, Rollback, and Workspace Locking

Pantau uses SQLite as the authoritative source of truth for Terraform state, eliminating the need for remote S3 backends:
- **State Versions**: Every successful apply, refresh, or manual import creates a new sequential state entry recording the serial number, lineage, resource count, and SHA-256 checksum.
- **Visual State Diff**: Compare any two historical state versions side-by-side to review added, removed, or modified resources and attributes.
- **State Rollback**: Roll back to a previous state version with one click. Pantau preserves lineage, increments the serial number (`current.serial + 1`), and records the rollback as a new state version, preventing serial number collisions.
- **Raw Download & Import**: Administrators can download the full `.tfstate` JSON (with security warnings regarding sensitive data) or import existing state files up to 16 MiB.
- **Workspace Locking**:
  - **Automatic Lock**: Workspaces are automatically locked (`locked_by = "run:<id>"`) while a run is queued, planning, awaiting approval, or applying.
  - **Manual Lock**: Administrators can lock a workspace with a custom reason to halt automated triggers during maintenance.
  - **Force Unlock**: If a run crashes or hangs, the administrator can perform an emergency Force Unlock, which cancels the active run, unlocks the database, and clears residual `.terraform.tfstate.lock.info` files on disk.
- **State Operations** (State tab): **Remove** (`state rm`), **Move** (`state mv`) and **Import resource** (`POST /api/terraform/workspaces/{id}/state/ops`). Each operation creates a **new** state version (the previous one is kept; the source is recorded, e.g. `state rm x`). The workspace is locked during the operation (HTTP 423 if it is already locked, 409 if a run is active). A confirmation dialog shows the exact command. Import runs `init` and uses the linked Cloud Account credentials. Action buttons are visible in each resource row.
- **Run backend**: runs no longer pass the deprecated `-state` flag. Pantau writes `pantau_backend_override.tf` (a `local` backend with its path in the run root, replacing any backend declared in your code) and runs `init -reconfigure`. Do not use the file name `pantau_backend_override.tf` in your own workspace files; it is overwritten.

#### Variables and Variable Sets

Manage configuration variables cleanly without hardcoding values in `.tf` files:
- **Categories**:
  - `terraform`: Injected into execution as Terraform input variables. Non-HCL variables are safely formatted into `pantau.auto.tfvars.json`; HCL variables are formatted into `pantau-hcl.auto.tfvars`.
  - `env`: Injected as process environment variables (e.g. `AWS_ACCESS_KEY_ID`, `ARM_CLIENT_SECRET`).
- **Sensitive Values**: Sensitive variables are strictly **write-only**. Their values are masked as `(sensitive)` in execution logs, never returned in API responses (`json:"-"`), and hidden in the UI.
- **Variable Sets**: Group related variables together (e.g., *Production Cloud Credentials*, *Global DNS Configuration*) and attach them to multiple workspaces, or mark the set as **Global** to apply across all workspaces automatically.
- **Resolution Precedence**: Workspace variables take precedence over attached Variable Sets, which take precedence over Global Variable Sets.
- **Detect from configuration**: on inline workspaces, the **Variables** tab button **Detect from configuration** (`GET /api/terraform/workspaces/{id}/variables/detect`) parses the `variable` blocks of the `.tf` files and lists them. Variables with a default are unchecked, required ones (no default) are checked, and variables that already exist are shown disabled. The chosen ones are added as `terraform` variables. Git workspaces are not supported; the scanner is not a full HCL parser and reads only `.tf` files.
- **Linked outputs** (Variables tab card): feed an output of another workspace's **latest state** into a Terraform variable (`GET/PUT /api/terraform/workspaces/{id}/output-links`). The value is resolved at run start and passed as HCL JSON; sensitive outputs stay masked. An explicit workspace or variable-set variable with the same name wins over the link. Cycles between workspaces are rejected, and an output that is unavailable (no state, missing output) fails the run start with a clear message. Links are included in backups.

#### Cloud Accounts

Register provider credentials once and let every run of a workspace use them, instead of copying keys into workspace variables.
1. Open **Terraform → Cloud Accounts** and click **New account**. Pick a provider (AWS, Google Cloud, Microsoft Azure, DigitalOcean, Hetzner, Cloudflare, Kubernetes), name it, and fill in the credential fields. Cloudflare takes an `api_token` and an optional `account_id`; Kubernetes takes a kubeconfig and an optional context. For Kubernetes, the kubeconfig is written to a `0600` temporary file per run, removed after the run, and its content is redacted from logs.
2. Click **Test connection** on the account. Pantau verifies the credentials against the provider API: AWS `sts:GetCallerIdentity` (and `AssumeRole` when a role is set), Google Cloud service-account token + project lookup, Azure client-credentials token, DigitalOcean `/v2/account`, Hetzner `/v1/servers`, Cloudflare token verify API. Kubernetes only parses the kubeconfig (no network call). The result (and the identity found) is shown on the account.
3. Open a workspace drawer → **Settings** → **Cloud accounts** card and tick the accounts to link. A workspace can link several accounts; deleting an account unlinks it everywhere.
4. Every run of a linked workspace (plan, apply, drift, webhook, state operations, cost estimate) receives the credentials as environment variables:

| Provider | Environment variables |
|---|---|
| AWS | `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`, `AWS_DEFAULT_REGION` + `AWS_REGION` (default `us-east-1`) |
| Google Cloud | `GOOGLE_PROJECT` + `GOOGLE_CLOUD_PROJECT`, `GOOGLE_REGION`, `GOOGLE_CREDENTIALS` (service-account JSON) |
| Microsoft Azure | `ARM_SUBSCRIPTION_ID`, `ARM_TENANT_ID`, `ARM_CLIENT_ID`, `ARM_CLIENT_SECRET` |
| DigitalOcean | `DIGITALOCEAN_TOKEN` |
| Hetzner | `HCLOUD_TOKEN` |
| Cloudflare | `CLOUDFLARE_API_TOKEN`, `CLOUDFLARE_ACCOUNT_ID` (optional) |
| Kubernetes | `KUBE_CONFIG_PATH` + `KUBECONFIG` (path of a temporary kubeconfig), `KUBE_CTX` (optional context) |

- **Precedence**: with several linked accounts, later accounts (by id) win on conflicts; workspace and Variable Set variables of category `env` override cloud-account values.
- **Security**: credentials are stored in the Pantau database, are never returned by the API (secret fields show as "stored"; leave a secret field empty when editing to keep it), and are included in the encrypted snapshot backups. Only static credentials (or AWS assume role, see below) are supported (no OIDC / workload identity). Credentials remain stored as plain values in SQLite (no at-rest encryption).
- **Log redaction**: run logs mask only secrets. Non-secret cloud fields (region, access key id, tenant / subscription / client id, project id) stay visible so runs are easier to debug; secret fields (secret keys, session tokens, API tokens, service-account JSON, client secrets, `external_id`, kubeconfig) and user `env` variables stay masked.

##### AWS Assume Role

An AWS account can optionally assume an IAM role instead of using the base keys directly: set **Role ARN** (`role_arn`), optionally **External ID** (`external_id`, secret) and **Session name** (`session_name`, default `pantau`). At the start of every run Pantau calls STS `AssumeRole` (1 hour) with the base keys and injects the temporary credentials (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`). If the call fails, the run fails with a clear error. **Test connection** also assumes the role.

##### Importing from Existing Pantau Accounts

Instead of typing credentials again, **New account** can import from an existing Pantau **Cloudflare** account or **Kubernetes** cluster. The credentials are **copied at import time** (not a live link): later changes or deletion of the source do not affect the Cloud Account. The picker is fed by `GET /api/terraform/cloud-accounts/importable`, which returns names only (never credentials); creation uses `import_from: {type, id}` on `POST /api/terraform/cloud-accounts`.

#### Outputs

Inspect outputs produced by the latest applied state in the **Outputs** panel:
- Displays output name, JSON value, and type.
- Sensitive outputs (marked `sensitive = true` in HCL) are masked by default (`null`, `value_hidden: true`) and can be revealed on demand via the **Reveal** toggle by authenticated administrators.
- Includes quick 1-click clipboard copy for connection strings and IP addresses.

#### Private Module Catalog

Maintain an internal catalog of reusable Terraform and OpenTofu modules:
1. Register a module with its name and provider (e.g. `vpc / aws`, `cluster / kubernetes`).
2. Upload module versions using inline files or connect a Git repository.
3. Pantau's built-in HCL parser automatically inspects the code and renders:
   - Comprehensive input variable tables (names, types, descriptions, default values, required status).
   - Output declarations and descriptions.
   - Declared resource types and required provider versions.
   - Formatted `README.md` documentation.
   - Ready-to-copy `module "name" { ... }` configuration snippet.

#### Scheduled Drift Detection

Automate vigilance against out-of-band changes:
1. Set a **Drift Check Interval** (e.g., every 60, 360, or 1440 minutes) in workspace settings.
2. Pantau's background scheduler automatically triggers speculative `-refresh-only` runs without applying any changes.
3. If deviations are discovered between real infrastructure and the state file, the workspace status flags **Drift Detected**, recording the drifted resources for operator review.

#### Policies and Cost Estimation

Enforce governance and track infrastructure spending before applying changes:
- **Built-in Policies**: Configure guardrails without complex policy languages:
  - `deny_destroy`: Rejects any run that plans resource deletions.
  - `max_deletes`: Caps the number of permitted resource deletions.
  - `max_changes`: Caps total resource modifications.
  - `deny_resource_types`: Denies creation of specified cloud resource types.
  - `require_tags`: Validates that mandatory tags are assigned to resources.
  - `deny_open_cidr`: Prevents security groups with open `0.0.0.0/0` ingress rules.
- **Infracost Integration**:
  Install the `infracost` CLI on the Pantau server host. Provide your `INFRACOST_API_KEY` in environment variables. Every planned run will automatically evaluate cost breakdowns, reporting monthly project costs and net deltas directly in the run summary.

#### Git VCS and Webhook Setup

Automate planning when code is pushed to your version control system:
1. In Workspace Settings, enable **Webhook Integration** and copy the generated webhook URL (`/api/terraform/webhook/{token}`).
2. (Optional) Generate or configure a **Webhook Secret** for cryptographic signature validation.
3. Configure your Git repository:
   - **GitHub**: Under **Repository Settings → Webhooks**, set the Payload URL, choose `application/json`, select the **Push** event, and paste the webhook secret (validated via `X-Hub-Signature-256`).
   - **GitLab**: Under **Settings → Webhooks**, set the URL, paste the secret token into **Secret token** (validated via `X-Gitlab-Token`), and enable **Push events**.
4. **Safety Guarantee**: Webhooks **only trigger speculative or standard Plan runs** when a commit is pushed to the configured workspace branch. Webhooks **never execute an Apply automatically**, not even on a workspace with Auto-apply enabled. All production changes remain guarded behind the Approval Gate.

#### Editing Workspace Repositories (Git Editor)

Workspaces using a Git repository source (`source = "git"`) include an **Edit Repository** button on the VCS card inside the **Settings** tab.

- **Full Workspace Tree**: Files are displayed relative to the repository root (not bounded to the workspace's sub-directory `git_path`), allowing cross-directory edits (e.g. root modules, reusable `.tf` configurations, or variable definition files).
- **Code Editing & File Management**: Add new configuration files, edit existing `.tf` / `.tfvars` / markdown files, delete files, and discard unsaved modifications.
- **Commit & Push Flow**:
  - Enter commit message, author name, and author email.
  - Choose to commit directly to the workspace's active branch or create a **New branch**.
  - Non-force push ensures changes never overwrite upstream history.
- **Optimistic Concurrency (HTTP 409)**: If another commit landed on the branch since opening the editor (`base_commit` mismatch), Pantau rejects the push with HTTP 409 (`ErrRepoChanged`), protecting remote state.
- **Write Permission Token Requirement**:
  - The configured Git access token or SSH Deploy Key must possess write privileges (e.g., GitHub Fine-Grained `Contents: Read and write`, GitHub classic `repo`, GitLab `write_repository`, or writable SSH key).
  - Tokens with read-only access will fail on push with an HTTP 502 error displaying sanitized git error details.
- **Webhook Integration Side Effect**: When committing and pushing directly to the workspace's monitored branch, the newly pushed commit will trigger the configured Git webhook (if enabled), initiating an automated speculative plan run.
- **Limits**: Maximum 50 changed files per commit, 1 MiB per file, 5 MiB total payload, valid UTF-8 text files only (binaries return 415), and strict path sanitization.

#### Notifications

The **Notifications** card on the Terraform **Gate** tab selects which Terraform events are sent. Delivery uses the channels already configured in Pantau **Settings** (Telegram, webhook, ntfy, Slack, Discord, SMTP); there is no separate channel setup.
- **Events**: awaiting approval, errored, applied, drift. Defaults: all except applied.
- `GET/PUT /api/terraform/notifications` read and change the selection; `POST /api/terraform/notifications/test` sends a sample event.
- **Drift** notifies only on the absent → present transition (no repeated message while drift persists).

#### Scheduled Plans and Auto-apply

Two settings in **Workspace Settings**:
- **Scheduled plan interval (minutes)** (`plan_interval_minutes`): creates `schedule` plan runs at that interval (0 = off).
- **Auto-apply** (`auto_apply`): applies a plan that has changes and whose policy status is `passed`, with the comment `auto-apply`. It covers only **scheduled** and **manually started** plan runs on non-production workspaces; **webhook-triggered runs are never auto-applied** (holders of the webhook secret must not cause unattended applies) and always wait at the Approval Gate. It is **never available for production environments**: saving returns HTTP 400, and a workspace whose environment later becomes production ignores the flag at runtime. Destroy and refresh runs, and runs with policy warnings, still need manual approval at the Approval Gate. If the automatic apply fails to start, the run stays at the Approval Gate and a notification is sent.

#### Audit Tab

The **Audit** tab shows a persistent trail of Terraform events (table `tf_audit`, newest 5000 kept; `GET /api/terraform/audit`). Entries are written from Pantau's log stream. There is no RBAC because Pantau has a single admin password. A burst of more than ~64 lines within one write window may drop lines.

#### Terraform Registry

The top-level Terraform **Registry** tab browses and searches the public Terraform Registry (modules and providers) without leaving Pantau.
- **Modules**: `GET /api/terraform/registry/modules` lists/searches modules. A module's detail view shows versions, inputs, outputs, and the README as plain text. Searching needs a non-empty query.
- **Providers**: `GET /api/terraform/registry/providers` lists/searches providers. Provider search uses the registry's **undocumented v2 search endpoint**, which may change without notice.
- **From the Configuration tab**: the **Registry** button opens the same browser with **Insert at cursor** or **Add as file**. Add as file creates `module-<name>.tf` or `provider-<type>.tf`, and can optionally append the module's required-input variables to `variables.tf`. Snippets are generated server-side, pin with a `~> MAJOR.MINOR` constraint, and use placeholders chosen by input type.
- **Check versions** (inline workspaces only): `GET /api/terraform/workspaces/{id}/registry/check` reads the **saved** files (save before checking) and lists every module and provider with a status: `up_to_date`, `outdated` (exact pin below latest), `newer_outside_constraint`, `unpinned`, or `unknown` (non-registry source). **Apply** rewrites only the version literal in the editor; the change stays unsaved until you click **Save**.
- **Registry base URL**: the **Registry** card on the Terraform **Gate** tab sets the base URL (setting `tf_registry_url`; `GET/PUT /api/terraform/registry/settings`, test with `POST /api/terraform/registry/settings/test`) for mirrors or air-gapped proxies. It must be `https`, except `http` on loopback, and must not contain credentials.
- **Safety**: Pantau fetches server-side with a 15 s timeout, a 4 MiB response cap, a 5-minute in-memory cache, same-host redirects only, and validated path arguments. Registry text is untrusted and rendered escaped.
- **Limits**: only the public Terraform Registry protocol (no private registry tokens, no OpenTofu-specific registry search); runs download modules/providers themselves during `terraform init` and need network access from the server; Git workspaces are not checked; prerelease versions are compared as plain semver.

#### Honest Limitations and Non-Goals

To maintain a compact, agentless, and maintainable architecture, several enterprise features are intentionally out of scope:
- **Credentials Still Plain at Rest**: Cloud Account credentials and variable values are stored unencrypted in SQLite (protection relies on DB file permissions and snapshot encryption).
- **Import Untested Against Real Providers**: The **import resource** state operation is implemented but has not been verified against real cloud providers.
- **Provider Templates Not Init-Validated**: Besides Hello world, the provider templates have not been validated with `init` against the real provider registries.
- **No Remote/S3 State Backend**: Pantau uses local SQLite storage. Remote backend configurations (`backend "s3"` / `backend "remote"`) in workspace code are not used.
- **Auto-Apply Is Opt-In and Limited**: Auto-apply exists only as an explicit per-workspace setting (see [Scheduled Plans and Auto-apply](#scheduled-plans-and-auto-apply)); it covers only scheduled and manual plans, never webhook-triggered runs, and production environments cannot use it. Destroy, refresh and policy-warning runs always need manual approval via the Approval Gate.
- **No OPA / Rego / Sentinel**: Policies are restricted to built-in Go evaluators.
- **No Dynamic OIDC Credentials**: Cloud credentials must be passed as sensitive `env` variables; dynamic STS/OIDC token exchanges are not natively managed.
- **No Public Registry Protocol**: The module catalog is a documentation and exploration interface, not an HTTP `terraform init` registry endpoint.
- **No Windows Controller**: While Pantau can run on Windows, Terraform/OpenTofu workspace execution is disabled on Windows hosts (`Detect()` reports unsupported).
- **Binaries**: The official Docker image ships `terraform`, `tofu` (OpenTofu) and `infracost`; on non-Docker installs put `terraform` or `tofu` in `PATH`. Infracost needs an `INFRACOST_API_KEY` env variable on the Workspace or Variable Set.
- **Concurrency Bounded**: Maximum 1 active run per workspace and 3 active runs globally. Excess requests receive HTTP 409 or 429.
### Repositories (Cloned Git Folders)

**Repositories** (side navigation) keeps Git repositories cloned on the Pantau server and lets you browse, edit, commit and push them. It is independent of Terraform and Ansible (no gate); Git-source workspaces and playbooks still clone briefly on every request.

- **Location**: the server path that holds everything (default `<data>/repos`; **Change location** sets another absolute path, empty = default). Existing clones stay where they were cloned.
- **Folders (groups)**: give a repository a **Folder** (for example `Torcons`) when you clone it, or later with **Folder** on its card. It works like a workspace group: the list shows a collapsible section per folder with the number of repos (and pending changes), a folder filter, and **No folder** for the rest. The folder is also a real directory, `<location>/Torcons/<repo>`; moving a repository to another folder moves its directory, and a folder that becomes empty is removed. Folder names use letters, digits, `.`, `_`, `-` (no spaces) and are not case-sensitive in Pantau (`torcons` joins `Torcons`); repository names stay unique across all folders.
- **Clone Repository**: URL (`git@host:org/repo.git`, `ssh://…` or `https://…`), optional folder name and branch, and authentication: an SSH key from the SSH Key Library, an HTTPS access token, or none for public repositories. The dialog then shows git's live output (counting, receiving, resolving…) with **Cancel clone**; you can close it and the clone continues on the server (the card appears when it finishes), and a failed clone shows the git error with **Try again**. **Credentials** on a card changes them later; a token is write-only (leave empty to keep it).
- **Open**: a file tree (folders are collapsed and load one level at a time, so big repositories list completely; `+` new / `M` modified / `−` deleted markers) and a code editor with syntax colors chosen by file name. The **search box** finds files anywhere in the repository by path (every word must match; file-name matches come first, up to 200 results), not only in opened folders. **Save** (`Ctrl+S`) writes the file to the clone only; **Discard** restores a file to HEAD (or removes a new one); **New file** and **Delete** change the working tree. Binary or non-UTF-8 files and files over 1 MiB are listed but cannot be edited here.
- **Commit & Push**: unsaved edits are saved first, then tick the files to commit (all by default), write the message, set author name and email (remembered in the browser) and choose the current branch or **Create a new branch**. Pantau commits only those files and pushes without force.
- **Sync** fetches the current branch and fast-forwards it; it never merges or rebases, and fails if local and remote history diverged. If a push is rejected (read-only credential, or the remote moved) the commit stays in the clone: fix the cause (use **Sync** if the remote moved) and press **Push**.
- **Copy to host** (card): sends the repository to a directory of any managed Host over SSH. Choose the Host, the absolute destination directory (default `/opt/<repo>`, remembered per repository) and whether to include `.git` (full history). The files are the working tree as the editor shows it: uncommitted edits are included (the dialog tells you how many), ignored files are not, and symlinks and submodule folders are skipped (the Transfer Queue notes how many). The directory is created if missing, files with the same name are overwritten and nothing else is deleted on the host; permissions (including the executable bit) are kept and the owner is the host's SSH user. System directories (`/etc`, `/usr`, `/root`, …) are refused as in the File Manager. Progress, speed and **Cancel** are in the **Transfer Queue**; a failure shows the host's own message (for example permission denied or quota).
- **Remove** deletes the repository entry **and its folder** on the server, including uncommitted changes and unpushed commits; the remote repository is untouched.
- The SSH key or token needs **write** permission on the repository (e.g. GitHub fine-grained `Contents: Read and write`, GitLab `write_repository`, a deploy key with write access). Host keys are not verified. Entries and credentials are not part of System Snapshots.

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

### Bulk Command Execution (Action Dialog)

When administering multiple servers simultaneously:
- **Action Dialog**: Open via the **Bulk Exec** button in the dashboard header.
- **Host Selection**: Filter hosts by search query, select by Host Group, or use **Select All (Max 20)** to choose up to 20 target hosts.
- **Safe Execution**: Prompts for confirmation via `showConfirm` displaying the exact host count and command preview.
- **Streaming Results**: Executes concurrently (up to 5 workers) streaming real-time NDJSON output per host, displaying status pills, exit codes, execution duration, expandable terminal outputs, and 1-click **Copy All** summary.
- **Sudo Support**: Optional non-interactive `sudo -n` elevation (fails safely without password prompt if elevation requires authentication).

### Package Upgrades & Streaming Upgrade Logs

Pantau enables agentless, controlled package updates directly from the **Packages** or **Security** tabs in Host Detail:
- **Upgrade Preview**: Inspect pending updates, breakdown of total vs security advisories, package manager (`apt`, `dnf`, `yum`), and reboot requirement banners.
- **Mode Selection**: Choose between full system upgrade (`all`) or security-only updates (`--security` on dnf/yum).
- **Safety Warning**: Confirms operations with explicit warnings: remote services may restart and no automatic rollback is available.
- **Real-Time Streaming Modal**: Streams terminal output live via WebSocket (`/ws/pkg/upgrade`), tracking standard output, errors, and terminal exit codes (`[pantau] exit=N`). Background package and security scans automatically refresh upon completion.

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

### Upload to S3 (File Manager)

Upload a file or folder from a host straight to an S3-compatible bucket (AWS S3, MinIO, Cloudflare R2, Wasabi, Ceph). Data is streamed through Pantau without a temporary file on the Pantau server.

1. Open **Settings > S3 Storage**, click **Add S3 Account** and fill in a **name**, the **Bucket**, **Access Key ID** and **Secret Access Key**. For anything other than AWS S3 also set the **Endpoint** (for example `https://minio.example.com`); Cloudflare R2 uses region `auto`. Click **Save**, then **Test** on the account row. Add as many accounts as you need (for example one per provider or per environment); edit or delete them from the same list.
2. In the File Manager click the cloud button on a file or folder row, pick the **S3 account**, check or change the destination folder inside the bucket (it defaults to the account's folder), and click **Upload**. The last account you used is preselected.
3. Follow the job in **Cross-Host Transfers** (progress, speed, ETA, cancel).

- A file is uploaded as is. A folder is packed on the host and stored as one `<name>.tar.gz` object.
- The secret key is stored in the Pantau database in plain text, like the other credentials (see [ADR-0035](adr/0035-ssh-key-library.md)), and is never returned by the API. Use a key limited to the one bucket (`s3:PutObject`, `s3:AbortMultipartUpload`, `s3:ListBucket`).
- Files over 16 MB use multipart upload. There is no retry of a failed part: run the upload again after a network failure. A failed multipart upload is aborted so no orphan parts stay in the bucket.
- Existing objects with the same key are overwritten.

### On-The-Fly Folder Streaming Download

To download an entire remote folder structure to your local workstation:
- Select the folder in the SFTP manager and click **Download as Archive**.
- Pantau streams the compressed archive on-the-fly (`zip` if installed on remote host, or `tar.gz`) directly to your browser's HTTP download stream without creating temporary files on the Pantau server disk.

To back up several folders into one archive:
- Tick the checkbox in front of each folder (or file) you want, in any directory; the selection is kept while you navigate (up to 50 items).
- Click **Backup** in the toolbar, review the list, and click **Download .tar.gz**. The archive (`<host>-backup-<timestamp>.tar.gz`, paths kept relative to `/`) is streamed to your browser like a folder archive. **Clear selection** in the dialog empties the list.

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

### Disk Usage Explorer

Click **Disk usage** in the File Manager toolbar to see which folders take space in the current directory (like `ncdu`):
- Runs `du -x -k` one level deep on that host, on demand only (nothing is scheduled or cached), stays on the same filesystem, and uses the File Manager's current access user (run as another user via `sudo -n -u`).
- Folders are sorted largest first with a proportional bar and share of the total; click a row to drill down, use the up button to go back, and **Open in file manager** for a row or the current path.
- Limits: one scan per host at a time (a second one is refused until the first finishes), at most 500 entries per level, and a 55 s time limit. Unreadable folders (permission denied), a scan that hit the time limit and a truncated list are flagged with a warning; their sizes may be incomplete.

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

### Multi-Channel Notifications & Alert Debounce (Settings → Notifications)

Pantau dispatches instant alerts when drift is detected or an incident resolves across multiple channels:
- **Supported Channels**:
  - **Telegram**: Bot Token and Chat ID.
  - **Generic Webhook**: HTTP POST with JSON payload.
  - **ntfy**: Base URL (e.g. `https://ntfy.sh/my-topic`), optional secret Bearer Token, high priority alerts.
  - **Slack**: Incoming Webhook URL.
  - **Discord**: Webhook URL (truncated to 1,800 characters to prevent API rate/length drops).
  - **SMTP**: Email dispatcher supporting STARTTLS (default port 587), direct TLS (465), or plaintext, multiple comma-separated recipients, and standard authentication.
- **Secret Field Protection**: Sensitive tokens and passwords (`ntfy_token`, `smtp_pass`) are never exposed in GET responses (only a `*_set` boolean flag is returned). Submitting empty retains the existing secret, while blanking it explicitly clears the credential.
- **Alert Debounce**: Configure `alert_debounce_runs` (1..10, default 1). Requires N consecutive drift inspections before generating an Incident and dispatching notifications, preventing alerts from transient network blips.
- **Per-Channel Test**: Click **Test Notification** to trigger test messages across all configured channels simultaneously and inspect per-channel status badges.
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

