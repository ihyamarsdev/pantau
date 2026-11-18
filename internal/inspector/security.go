package inspector

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"pantau/internal/store"
)

var (
	ErrSecurityScanInProgress = errors.New("security scan already in progress for this host")
)

var (
	// rpmAdvisoryRe matches advisory ids at the start of `updateinfo list` rows (RHSA-2023:1234, ALAS2023-2023-123, FEDORA-2023-456).
	rpmAdvisoryRe = regexp.MustCompile(`^[A-Za-z]+[0-9]*-[0-9]{4}[-:][0-9]+`)
	// rpmArchSuffixRe matches `name.arch` tokens that start `yum --security check-update` rows.
	rpmArchSuffixRe = regexp.MustCompile(`\.(x86_64|noarch|i[3-6]86|aarch64|ppc64le|s390x|armv7hl)$`)
	// macAutoCriticalRe matches CriticalUpdateInstall = 1 in SoftwareUpdate plist.
	macAutoCriticalRe = regexp.MustCompile(`(?i)CriticalUpdateInstall\s*[:=]\s*1\b`)
	// macAutoConfigRe matches ConfigDataInstall = 1 in SoftwareUpdate plist.
	macAutoConfigRe = regexp.MustCompile(`(?i)ConfigDataInstall\s*[:=]\s*1\b`)
)

// rpmArchSuffixes are stripped from an RPM name before splitting name/version.
var rpmArchSuffixes = []string{".x86_64", ".noarch", ".aarch64", ".i686", ".i386", ".ppc64le", ".s390x", ".armv7hl"}

func countUnknownHardening(checks []store.HardeningCheck) int {
	n := 0
	for _, c := range checks {
		if c.Status == "unknown" {
			n++
		}
	}
	return n
}

// SecurityScanBatchCmd is a polyglot, agentless, read-only script executed on the remote host.
// It detects pending security updates, cache freshness, reboot state, and hardening posture.
const SecurityScanBatchCmd = `if [ "$(uname -s 2>/dev/null)" = "Darwin" ]; then
echo "===OS_RELEASE==="
echo "ID=macos"
echo "NAME=\"macOS\""
echo "VERSION_ID=\"$(sw_vers -productVersion 2>/dev/null)\""
echo "PRETTY_NAME=\"macOS $(sw_vers -productVersion 2>/dev/null)\""

echo "===CACHE_AGE==="
date +%s 2>/dev/null

echo "===REBOOT==="

echo "===UPDATES==="
echo "MGR:softwareupdate"
if command -v perl >/dev/null 2>&1; then
    perl -e 'alarm 60; exec @ARGV' softwareupdate -l 2>&1
else
    softwareupdate -l 2>&1
fi

echo "===SSHD_CONFIG==="
if [ -r /etc/ssh/sshd_config ]; then
    echo "FILE:/etc/ssh/sshd_config"
    cat /etc/ssh/sshd_config 2>/dev/null
    for f in /etc/ssh/sshd_config.d/*; do
        if [ -r "$f" ]; then
            echo "FILE:$f"
            cat "$f" 2>/dev/null
        fi
    done
elif [ -d /etc/ssh/sshd_config.d ]; then
    for f in /etc/ssh/sshd_config.d/*; do
        if [ -r "$f" ]; then
            echo "FILE:$f"
            cat "$f" 2>/dev/null
        fi
    done
else
    echo "SSHD_UNREADABLE"
fi

echo "===AUTO_UPDATES==="
defaults read /Library/Preferences/com.apple.SoftwareUpdate 2>&1 || echo "DEFAULTS_UNREADABLE"

echo "===FIREWALL==="
if [ -x /usr/libexec/ApplicationFirewall/socketfilterfw ]; then
    /usr/libexec/ApplicationFirewall/socketfilterfw --getglobalstate 2>&1
else
    echo "SOCKETFILTERFW_UNREADABLE"
fi
else
PATH="$PATH:/usr/local/sbin:/usr/sbin:/sbin"; export PATH
S=""
if [ "$PANTAU_TRY_SUDO" = "1" ] && [ "$(id -u 2>/dev/null)" != "0" ]; then
    if sudo -n true 2>/dev/null; then S="sudo -n"; echo "---SUDO:ok---"; else echo "---SUDO:denied---"; fi
fi
echo "===OS_RELEASE==="
cat /etc/os-release /usr/lib/os-release 2>/dev/null || cat /etc/redhat-release /etc/debian_version 2>/dev/null

echo "===CACHE_AGE==="
date +%s 2>/dev/null
echo "---APT---"
stat -c %Y /var/lib/apt/periodic/update-success-stamp 2>/dev/null || stat -c %Y /var/lib/apt/lists 2>/dev/null
echo "---DNF---"
stat -c %Y /var/cache/dnf 2>/dev/null || stat -c %Y /var/cache/yum 2>/dev/null

echo "===REBOOT==="
if [ -f /var/run/reboot-required ] || [ -f /run/reboot-required ]; then
    echo "REBOOT_REQUIRED"
fi
if command -v needs-restarting >/dev/null 2>&1; then
    needs-restarting -r >/dev/null 2>&1
    echo "NEEDS_RESTARTING:$?"
fi

echo "===UPDATES==="
if command -v apt-get >/dev/null 2>&1; then
    echo "MGR:apt"
    apt-get -s dist-upgrade 2>&1 || apt-get -s upgrade 2>&1
elif command -v dnf >/dev/null 2>&1; then
    echo "MGR:dnf"
    echo "---SEC---"
    dnf -q updateinfo list security 2>&1
    echo "---TOTAL---"
    dnf check-update -q 2>&1
    echo "---RPM_QA---"
    rpm -qa --qf '%{NAME}\t%{VERSION}-%{RELEASE}\n' 2>/dev/null
    echo "---CVE---"
    dnf -q updateinfo list --security --with-cve 2>&1
elif command -v yum >/dev/null 2>&1; then
    echo "MGR:yum"
    echo "---SEC---"
    yum updateinfo list security 2>&1 || yum --security check-update 2>&1
    echo "---TOTAL---"
    yum check-update -q 2>&1
    echo "---RPM_QA---"
    rpm -qa --qf '%{NAME}\t%{VERSION}-%{RELEASE}\n' 2>/dev/null
    echo "---CVE---"
    yum updateinfo list cves 2>&1
elif [ -f /lib/apk/db/installed ] || command -v apk >/dev/null 2>&1; then
    echo "MGR:apk"
elif command -v pacman >/dev/null 2>&1; then
    echo "MGR:pacman"
else
    echo "MGR:unknown"
fi

echo "===SSHD_CONFIG==="
SSHD_T=""
if command -v sshd >/dev/null 2>&1; then
    SSHD_T=$($S sshd -G 2>/dev/null || $S sshd -T 2>/dev/null)
fi
if [ -n "$SSHD_T" ]; then
    echo "SSHD_EFFECTIVE"
    printf '%s\n' "$SSHD_T"
elif $S test -r /etc/ssh/sshd_config; then
    echo "FILE:/etc/ssh/sshd_config"
    $S cat /etc/ssh/sshd_config 2>/dev/null
    for f in /etc/ssh/sshd_config.d/*.conf; do
        if $S test -r "$f"; then
            echo "FILE:$f"
            $S cat "$f" 2>/dev/null
        fi
    done
else
    echo "SSHD_UNREADABLE"
fi

echo "===AUTO_UPDATES==="
if command -v systemctl >/dev/null 2>&1; then
    echo "APT_TIMER_ENABLED:$(systemctl is-enabled apt-daily-upgrade.timer 2>/dev/null)"
    echo "DNF_TIMER_ENABLED:$(systemctl is-enabled dnf-automatic.timer 2>/dev/null)"
    echo "DNF_TIMER_ACTIVE:$(systemctl is-active dnf-automatic.timer 2>/dev/null)"
    echo "DNF_INSTALL_TIMER_ENABLED:$(systemctl is-enabled dnf-automatic-install.timer 2>/dev/null)"
    echo "DNF_INSTALL_TIMER_ACTIVE:$(systemctl is-active dnf-automatic-install.timer 2>/dev/null)"
    echo "YUM_CRON_ENABLED:$(systemctl is-enabled yum-cron 2>/dev/null)"
    echo "YUM_CRON_ACTIVE:$(systemctl is-active yum-cron 2>/dev/null)"
fi
if command -v apt-config >/dev/null 2>&1; then
    echo "---APT_CONFIG---"
    command -v unattended-upgrade >/dev/null 2>&1 && echo "UU_INSTALLED"
    apt-config dump 2>/dev/null | grep -E '^APT::Periodic::(Update-Package-Lists|Unattended-Upgrade) '
fi
grep -H -s -E '^[[:space:]]*apply_updates[[:space:]]*=' /etc/dnf/automatic.conf /etc/yum/yum-cron.conf

echo "===FIREWALL==="
if command -v ufw >/dev/null 2>&1; then
    echo "---UFW---"
    $S ufw status 2>&1
fi
if command -v systemctl >/dev/null 2>&1; then
    echo "---FIREWALLD---"
    systemctl is-active firewalld 2>&1
fi
if command -v nft >/dev/null 2>&1; then
    echo "---NFT---"
    $S nft list ruleset 2>&1
fi
if command -v iptables >/dev/null 2>&1; then
    echo "---IPTABLES---"
    $S iptables -S 2>&1 || $S iptables -L -n 2>&1
fi

echo "===EXTRA_HARDENING==="
echo "---MAC---"
[ -r /sys/fs/selinux/enforce ] && echo "SELINUX_ENFORCE:$(cat /sys/fs/selinux/enforce 2>/dev/null)"
[ -r /sys/module/apparmor/parameters/enabled ] && echo "APPARMOR_ENABLED:$(cat /sys/module/apparmor/parameters/enabled 2>/dev/null)"
echo "---SYSCTL---"
for k in kernel/randomize_va_space kernel/kptr_restrict kernel/dmesg_restrict net/ipv4/tcp_syncookies net/ipv4/conf/all/accept_redirects net/ipv4/conf/all/accept_source_route; do
    [ -r "/proc/sys/$k" ] && echo "$k=$(cat "/proc/sys/$k" 2>/dev/null)"
done
echo "---SERVICES---"
for u in fail2ban sshguard crowdsec auditd; do
    if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
        echo "$u:$(systemctl is-active "$u" 2>/dev/null)"
    elif command -v pgrep >/dev/null 2>&1; then
        p=$u; [ "$u" = "fail2ban" ] && p=fail2ban-server
        if pgrep -x "$p" >/dev/null 2>&1; then echo "$u:active"; else echo "$u:inactive"; fi
    else
        echo "$u:noprobe"
    fi
done
fi`

// IsSecurityScanning reports whether a security scan is currently running for hostID.
func (ins *Inspector) IsSecurityScanning(hostID int64) bool {
	_, loaded := ins.securityInFlight.Load(hostID)
	return loaded
}

// ScanSecurity runs the agentless security scan with a 90-second timeout budget and singleflight guard.
func (ins *Inspector) ScanSecurity(hostID int64) error {
	return ins.ScanSecurityWithTimeout(hostID, 90*time.Second)
}

// ScanSecurityWithTimeout executes the security scan with a given timeout budget.
func (ins *Inspector) ScanSecurityWithTimeout(hostID int64, timeout time.Duration) error {
	if _, loaded := ins.securityInFlight.LoadOrStore(hostID, struct{}{}); loaded {
		return ErrSecurityScanInProgress
	}
	defer ins.securityInFlight.Delete(hostID)

	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	host, err := ins.db.GetHost(hostID)
	if err != nil {
		return err
	}
	if host == nil {
		return fmt.Errorf("host %d not found", hostID)
	}

	runner, err := ins.GetRunnerForHost(host)
	if err != nil {
		// Connection failure returns error WITHOUT overwriting the previous scan
		return fmt.Errorf("connect host %d: %w", hostID, err)
	}
	defer runner.Close()

	doneChan := make(chan struct{})
	defer close(doneChan)
	go func() {
		select {
		case <-doneChan:
		case <-ctx.Done():
			_ = runner.Close()
		}
	}()

	// Firewall tables and the effective sshd config need root. Passwordless sudo is tried unless it was refused recently (shared backoff).
	trySudo := "0"
	if ins.sudoProbeAllowed(hostID) {
		trySudo = "1"
	}
	stdout, _, _, err := runner.Exec("PANTAU_TRY_SUDO=" + trySudo + "\n" + SecurityScanBatchCmd)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("exec security scan timed out: %w", ctx.Err())
		}
		return fmt.Errorf("exec security scan: %w", err)
	}

	switch {
	case strings.Contains(stdout, "---SUDO:ok---"):
		ins.noteSudo(hostID, false)
	case strings.Contains(stdout, "---SUDO:denied---"):
		ins.noteSudo(hostID, true)
	}
	scan := parseSecurityScanOutput(stdout, hostID)
	scan.HostID = hostID
	scan.ScannedAt = time.Now()

	return ins.db.SaveSecurityScan(scan)
}

func extractSection(raw, header, nextHeader string) string {
	idx := strings.Index(raw, header)
	if idx == -1 {
		return ""
	}
	body := raw[idx+len(header):]
	if nextHeader != "" {
		if nIdx := strings.Index(body, nextHeader); nIdx != -1 {
			body = body[:nIdx]
		}
	}
	return strings.TrimSpace(body)
}

func parseSecurityScanOutput(stdout string, hostID int64) *store.SecurityScan {
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" {
		return &store.SecurityScan{
			HostID:            hostID,
			Status:            "error",
			PackageManager:    "unknown",
			OSFamily:          "unknown",
			RepoCacheAgeHours: -1,
			Findings:          []store.SecurityFinding{},
			Hardening:         []store.HardeningCheck{},
			Notes:             []string{"no output returned from remote host"},
			Error:             "empty output from remote host",
		}
	}

	osSec := extractSection(stdout, "===OS_RELEASE===", "===CACHE_AGE===")
	cacheSec := extractSection(stdout, "===CACHE_AGE===", "===REBOOT===")
	rebootSec := extractSection(stdout, "===REBOOT===", "===UPDATES===")
	updatesSec := extractSection(stdout, "===UPDATES===", "===SSHD_CONFIG===")
	sshdSec := extractSection(stdout, "===SSHD_CONFIG===", "===AUTO_UPDATES===")
	autoSec := extractSection(stdout, "===AUTO_UPDATES===", "===FIREWALL===")
	firewallSec := extractSection(stdout, "===FIREWALL===", "===EXTRA_HARDENING===")
	extraSec := extractSection(stdout, "===EXTRA_HARDENING===", "")

	osFamily, detectedMgr := parseOSFamily(osSec)

	// Check MGR from updates section
	packageManager := detectedMgr
	if strings.Contains(updatesSec, "MGR:apt") {
		packageManager = "apt"
	} else if strings.Contains(updatesSec, "MGR:dnf") {
		packageManager = "dnf"
	} else if strings.Contains(updatesSec, "MGR:yum") {
		packageManager = "yum"
	} else if strings.Contains(updatesSec, "MGR:apk") {
		packageManager = "apk"
	} else if strings.Contains(updatesSec, "MGR:pacman") {
		packageManager = "pacman"
	} else if strings.Contains(updatesSec, "MGR:softwareupdate") {
		packageManager = "softwareupdate"
	}

	var osProductVersion string
	if osFamily == "macos" {
		for _, line := range strings.Split(osSec, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "VERSION_ID=") {
				osProductVersion = strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), `"'`)
				break
			}
			if strings.HasPrefix(line, "ProductVersion:") {
				osProductVersion = strings.TrimSpace(strings.TrimPrefix(line, "ProductVersion:"))
				break
			}
		}
	}

	var (
		findings        []store.SecurityFinding
		pendingSecurity int
		pendingTotal    int
		rebootRequired  bool
		notes           []string
		updateNotes     []string
	)

	switch packageManager {
	case "apt":
		findings, pendingSecurity, pendingTotal, updateNotes = parseAptUpdates(updatesSec)
	case "dnf", "yum":
		secSub := extractSection(updatesSec, "---SEC---", "---TOTAL---")
		totSub := extractSection(updatesSec, "---TOTAL---", "---RPM_QA---")
		rpmQaSub := extractSection(updatesSec, "---RPM_QA---", "---CVE---")
		findings, pendingSecurity, pendingTotal, updateNotes = parseRpmUpdates(secSub, totSub, rpmQaSub)
		attachRpmCVEs(findings, extractSection(updatesSec, "---CVE---", ""))
	case "softwareupdate":
		var swReboot bool
		findings, pendingSecurity, pendingTotal, swReboot, updateNotes = parseSoftwareUpdate(updatesSec, osProductVersion)
		if swReboot {
			rebootRequired = true
		}
	default:
		// Unsupported package manager
		notes = append(notes, fmt.Sprintf("package manager '%s' is not supported for security updates detection", packageManager))
	}
	notes = append(notes, updateNotes...)

	// Reboot required
	if parseRebootRequired(rebootSec) {
		rebootRequired = true
	}

	// Repo cache age
	cacheAgeHours, cacheNotes := parseRepoCacheAge(cacheSec, packageManager)
	notes = append(notes, cacheNotes...)

	// Hardening checks
	hardening, hardeningNotes := parseHardeningChecks(osFamily, sshdSec, autoSec, firewallSec, extraSec)
	notes = append(notes, hardeningNotes...)

	// Severity tally
	var severity store.SecuritySeverity
	for _, f := range findings {
		switch strings.ToLower(f.Severity) {
		case "critical":
			severity.Critical++
		case "important":
			severity.Important++
		case "moderate":
			severity.Moderate++
		case "low":
			severity.Low++
		default:
			severity.Unknown++
		}
	}

	// Status derivation per honesty rules
	status := "ok"
	if packageManager == "softwareupdate" {
		status = "partial"
	} else if packageManager != "apt" && packageManager != "dnf" && packageManager != "yum" {
		status = "unsupported"
	} else {
		// If supported, check for partial coverage
		if cacheAgeHours > 168 {
			status = "partial"
		}
		// If any note mentions permission denied / unreadable / partial, set status to partial
		for _, n := range notes {
			nLower := strings.ToLower(n)
			if strings.Contains(nLower, "permission denied") ||
				strings.Contains(nLower, "unable to read") ||
				strings.Contains(nLower, "without root") ||
				strings.Contains(nLower, "metadata is") {
				status = "partial"
				break
			}
		}
	}

	// Unevaluable hardening checks do not make the update scan partial (updates were fully collected),
	// but they must never be silent: surface them as a coverage note.
	if unknownChecks := countUnknownHardening(hardening); unknownChecks > 0 {
		notes = append(notes, fmt.Sprintf("%d hardening check(s) could not be evaluated (this SSH user lacks permission or the file is absent)", unknownChecks))
	}

	if findings == nil {
		findings = []store.SecurityFinding{}
	}
	if hardening == nil {
		hardening = []store.HardeningCheck{}
	}
	if notes == nil {
		notes = []string{}
	}

	return &store.SecurityScan{
		HostID:            hostID,
		Status:            status,
		PackageManager:    packageManager,
		OSFamily:          osFamily,
		PendingSecurity:   pendingSecurity,
		PendingTotal:      pendingTotal,
		Severity:          severity,
		RebootRequired:    rebootRequired,
		RepoCacheAgeHours: cacheAgeHours,
		Findings:          findings,
		Hardening:         hardening,
		Notes:             notes,
	}
}

// parseOSFamily extracts normalized osFamily and default packageManager.
func parseOSFamily(content string) (osFamily, pkgManager string) {
	id := ""
	idLike := ""
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ID=") {
			id = strings.Trim(strings.TrimPrefix(line, "ID="), `"'`)
		} else if strings.HasPrefix(line, "ID_LIKE=") {
			idLike = strings.Trim(strings.TrimPrefix(line, "ID_LIKE="), `"'`)
		}
	}

	idLower := strings.ToLower(id)
	idLikeLower := strings.ToLower(idLike)

	switch {
	case idLower == "macos" || idLower == "darwin":
		return "macos", "softwareupdate"
	case idLower == "ubuntu":
		return "ubuntu", "apt"
	case idLower == "debian":
		return "debian", "apt"
	case idLower == "alpine":
		return "alpine", "apk"
	case idLower == "arch":
		return "arch", "pacman"
	case idLower == "rhel" || idLower == "centos" || idLower == "rocky" || idLower == "almalinux" ||
		idLower == "fedora" || idLower == "amzn" || idLower == "ol" ||
		strings.Contains(idLikeLower, "rhel") || strings.Contains(idLikeLower, "centos") || strings.Contains(idLikeLower, "fedora"):
		if idLower == "centos" || strings.Contains(content, "CentOS release 6") || strings.Contains(content, "CentOS Linux release 7") {
			return "rhel", "yum"
		}
		return "rhel", "dnf"
	default:
		// Fallback check on content
		cLower := strings.ToLower(content)
		if strings.Contains(cLower, "macos") || strings.Contains(cLower, "darwin") {
			return "macos", "softwareupdate"
		} else if strings.Contains(cLower, "ubuntu") {
			return "ubuntu", "apt"
		} else if strings.Contains(cLower, "debian") {
			return "debian", "apt"
		} else if strings.Contains(cLower, "centos") || strings.Contains(cLower, "red hat") || strings.Contains(cLower, "fedora") {
			return "rhel", "yum"
		}
		return "unknown", "unknown"
	}
}
