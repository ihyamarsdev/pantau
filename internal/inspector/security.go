package inspector

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strconv"
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
elif command -v yum >/dev/null 2>&1; then
    echo "MGR:yum"
    echo "---SEC---"
    yum updateinfo list security 2>&1 || yum --security check-update 2>&1
    echo "---TOTAL---"
    yum check-update -q 2>&1
    echo "---RPM_QA---"
    rpm -qa --qf '%{NAME}\t%{VERSION}-%{RELEASE}\n' 2>/dev/null
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
	firewallSec := extractSection(stdout, "===FIREWALL===", "")

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
		rpmQaSub := extractSection(updatesSec, "---RPM_QA---", "")
		findings, pendingSecurity, pendingTotal, updateNotes = parseRpmUpdates(secSub, totSub, rpmQaSub)
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
	hardening, hardeningNotes := parseHardeningChecks(osFamily, sshdSec, autoSec, firewallSec)
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

// parseAptUpdates parses apt-get -s simulate upgrade output.
func parseAptUpdates(output string) (findings []store.SecurityFinding, pendingSec, pendingTot int, notes []string) {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "E:") || strings.Contains(trimmed, "Permission denied") {
			notes = append(notes, fmt.Sprintf("apt: %s", trimmed))
		}

		if !strings.HasPrefix(trimmed, "Inst ") {
			continue
		}
		pendingTot++

		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}
		pkgName := fields[1]

		var installedVer, fixedVer, suite string
		startParen := strings.Index(trimmed, "(")
		endParen := strings.LastIndex(trimmed, ")")
		if startParen != -1 && endParen > startParen {
			inside := trimmed[startParen+1 : endParen]
			insideFields := strings.Fields(inside)
			if len(insideFields) > 0 {
				fixedVer = insideFields[0]
			}
			if len(insideFields) > 1 {
				suite = strings.Join(insideFields[1:], " ")
			}
		}

		if startBracket := strings.Index(trimmed, "["); startBracket != -1 && startBracket < startParen {
			if endBracket := strings.Index(trimmed[startBracket:], "]"); endBracket != -1 {
				installedVer = trimmed[startBracket+1 : startBracket+endBracket]
			}
		}

		// Security update detection per contract: suite contains -security or ESM
		suiteLower := strings.ToLower(suite)
		isSecurity := strings.Contains(suiteLower, "-security") || strings.Contains(strings.ToUpper(suite), "ESM")

		if isSecurity {
			pendingSec++
			findings = append(findings, store.SecurityFinding{
				Package:          pkgName,
				InstalledVersion: installedVer,
				FixedVersion:     fixedVer,
				Severity:         "unknown",
				Advisory:         "",
			})
		}
	}

	return findings, pendingSec, pendingTot, notes
}

// parseRpmUpdates parses dnf/yum updateinfo list security and check-update output.
func parseRpmUpdates(secOut, totalOut, rpmQaOut string) (findings []store.SecurityFinding, pendingSec, pendingTot int, notes []string) {
	// Parse installed packages
	installedMap := make(map[string]string)
	for _, line := range strings.Split(rpmQaOut, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "\t", 2)
		if len(parts) == 2 {
			installedMap[parts[0]] = parts[1]
		}
	}

	// Parse security updates
	secLines := strings.Split(secOut, "\n")
	for _, line := range secLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "Loaded plugins") ||
			strings.HasPrefix(trimmed, "Update Information") ||
			strings.HasPrefix(trimmed, "Last metadata") ||
			strings.HasPrefix(trimmed, "Limiting package") ||
			strings.HasPrefix(trimmed, "Needed ") {
			continue
		}
		if strings.Contains(trimmed, "Permission denied") || strings.HasPrefix(trimmed, "Error:") {
			notes = append(notes, fmt.Sprintf("rpm: %s", trimmed))
			continue
		}

		fields := strings.Fields(trimmed)
		if len(fields) >= 3 && (rpmAdvisoryRe.MatchString(fields[0]) || rpmArchSuffixRe.MatchString(fields[0])) {
			// Case A: Advisory Severity Package (e.g. RHSA-2023:1234 Important/Sec. openssl-1:1.1.1k-9.el8_7.x86_64)
			advisory := fields[0]
			sevRaw := strings.ToLower(fields[1])
			pkgRaw := fields[2]

			// Verify if fields[0] looks like advisory or if fields[1] looks like severity
			var sev string
			switch {
			case strings.Contains(sevRaw, "critical"):
				sev = "critical"
			case strings.Contains(sevRaw, "important"):
				sev = "important"
			case strings.Contains(sevRaw, "moderate"):
				sev = "moderate"
			case strings.Contains(sevRaw, "low"):
				sev = "low"
			default:
				sev = "unknown"
			}

			// If fields[0] doesn't look like advisory but fields[0] is package (e.g. yum --security check-update)
			if strings.Contains(fields[0], ".") && !strings.Contains(fields[0], "-") && (strings.HasSuffix(fields[0], ".x86_64") || strings.HasSuffix(fields[0], ".noarch")) {
				// yum --security check-update format: pkg.arch version repo
				pkgName := strings.Split(fields[0], ".")[0]
				fixedVer := fields[1]
				findings = append(findings, store.SecurityFinding{
					Package:          pkgName,
					InstalledVersion: installedMap[pkgName],
					FixedVersion:     fixedVer,
					Severity:         "unknown",
					Advisory:         "",
				})
				continue
			}

			// Extract package name and version from NEVRA string
			pkgName, fixedVer := splitRpmNEVRA(pkgRaw)
			installedVer := installedMap[pkgName]

			findings = append(findings, store.SecurityFinding{
				Package:          pkgName,
				InstalledVersion: installedVer,
				FixedVersion:     fixedVer,
				Severity:         sev,
				Advisory:         advisory,
			})
		}
	}

	pendingSec = len(findings)

	// Parse total pending updates from check-update output
	totalLines := strings.Split(totalOut, "\n")
	for _, line := range totalLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "Loaded plugins") ||
			strings.HasPrefix(trimmed, "Obsoleting Packages") ||
			strings.HasPrefix(trimmed, "Last metadata") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) >= 2 && strings.Contains(fields[0], ".") {
			pendingTot++
		}
	}

	if pendingTot < pendingSec {
		pendingTot = pendingSec
	}

	return findings, pendingSec, pendingTot, notes
}

// splitRpmNEVRA splits an RPM string (e.g. openssl-1:1.1.1k-9.el8_7.x86_64) into package name and fixed version.
func splitRpmNEVRA(s string) (pkgName, fixedVer string) {
	// Strip known arch suffix
	arches := []string{".x86_64", ".noarch", ".aarch64", ".i686", ".i386", ".ppc64le", ".s390x", ".armv7hl"}
	for _, a := range arches {
		if strings.HasSuffix(s, a) {
			s = strings.TrimSuffix(s, a)
			break
		}
	}

	lastDash := strings.LastIndex(s, "-")
	if lastDash == -1 {
		return s, ""
	}
	secondLastDash := strings.LastIndex(s[:lastDash], "-")
	if secondLastDash == -1 {
		return s[:lastDash], s[lastDash+1:]
	}
	return s[:secondLastDash], s[secondLastDash+1:]
}

// parseRebootRequired checks Debian reboot-required flag and RHEL needs-restarting exit code.
func parseRebootRequired(content string) bool {
	if strings.Contains(content, "REBOOT_REQUIRED") {
		return true
	}
	if strings.Contains(content, "NEEDS_RESTARTING:1") {
		return true
	}
	return false
}

// parseRepoCacheAge computes cache age in hours and issues notes if stale (>168h).
func parseRepoCacheAge(content, mgr string) (ageHours int, notes []string) {
	if mgr == "softwareupdate" {
		return -1, nil
	}
	var nowEpoch, cacheEpoch int64
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "---") {
			continue
		}
		if nowEpoch == 0 {
			if n, err := strconv.ParseInt(l, 10, 64); err == nil && n > 0 {
				nowEpoch = n
				continue
			}
		}
	}

	if mgr == "apt" {
		aptSub := extractSection(content, "---APT---", "---DNF---")
		for _, l := range strings.Split(aptSub, "\n") {
			if n, err := strconv.ParseInt(strings.TrimSpace(l), 10, 64); err == nil && n > 0 {
				cacheEpoch = n
				break
			}
		}
	} else if mgr == "dnf" || mgr == "yum" {
		dnfSub := extractSection(content, "---DNF---", "")
		for _, l := range strings.Split(dnfSub, "\n") {
			if n, err := strconv.ParseInt(strings.TrimSpace(l), 10, 64); err == nil && n > 0 {
				cacheEpoch = n
				break
			}
		}
	}

	if nowEpoch <= 0 || cacheEpoch <= 0 {
		return -1, notes
	}

	diff := nowEpoch - cacheEpoch
	if diff < 0 {
		diff = 0
	}
	ageHours = int(diff / 3600)

	if ageHours > 168 {
		days := ageHours / 24
		notes = append(notes, fmt.Sprintf("package metadata is %d days old; results may miss recent advisories", days))
	}

	return ageHours, notes
}

// parseHardeningChecks parses sshd, automatic updates, and firewall checks.
func parseHardeningChecks(osFamily, sshdSec, autoSec, firewallSec string) (checks []store.HardeningCheck, notes []string) {
	permitRoot, passAuth, sshdNotes := parseSshdConfig(sshdSec, osFamily)
	checks = append(checks, permitRoot, passAuth)
	notes = append(notes, sshdNotes...)

	autoCheck, autoNotes := parseAutoUpdates(autoSec)
	checks = append(checks, autoCheck)
	notes = append(notes, autoNotes...)

	firewallCheck, fwNotes := parseFirewall(firewallSec)
	checks = append(checks, firewallCheck)
	notes = append(notes, fwNotes...)

	return checks, notes
}

// sshdSettings returns sshd's global directives (lowercased, first value wins, Match blocks skipped).
// A SSHD_EFFECTIVE dump is `sshd -G`/`sshd -T` output, which already resolves Include, Match and the distro's
// compiled-in defaults. Otherwise the FILE: dumps are read the way sshd does: an Include pulls the matching
// drop-ins in at its own line, so drop-ins included at the top of sshd_config override it.
func sshdSettings(content string) map[string]string {
	settings := map[string]string{}
	fields := func(line string) []string {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			return nil
		}
		return strings.FieldsFunc(line, func(r rune) bool { return r == ' ' || r == '\t' || r == '=' })
	}

	if rest, ok := strings.CutPrefix(content, "SSHD_EFFECTIVE"); ok {
		for _, line := range strings.Split(rest, "\n") {
			if f := fields(line); len(f) >= 2 {
				settings[strings.ToLower(f[0])] = strings.ToLower(f[1])
			}
		}
		// Before OpenSSH 8.7 ChallengeResponseAuthentication was a separate switch that also enabled keyboard-interactive.
		if settings["challengeresponseauthentication"] == "yes" {
			settings["kbdinteractiveauthentication"] = "yes"
		}
		return settings
	}

	files := map[string][]string{}
	var order []string // shell glob order, which is sorted like sshd's own glob(3)
	cur := ""
	for _, line := range strings.Split(content, "\n") {
		if p, ok := strings.CutPrefix(strings.TrimSpace(line), "FILE:"); ok {
			cur = p
			order = append(order, p)
			continue
		}
		files[cur] = append(files[cur], line)
	}

	var walk func(file string, depth int, inMatch bool)
	walk = func(file string, depth int, inMatch bool) {
		for _, line := range files[file] {
			f := fields(line)
			if len(f) == 0 {
				continue
			}
			key := strings.ToLower(f[0])
			switch {
			case key == "match":
				inMatch = len(f) < 2 || !strings.EqualFold(f[1], "all")
			case key == "include":
				if depth >= 16 {
					continue
				}
				for _, pat := range f[1:] {
					if !strings.HasPrefix(pat, "/") {
						pat = "/etc/ssh/" + pat
					}
					for _, p := range order {
						if ok, _ := path.Match(pat, p); ok {
							walk(p, depth+1, inMatch)
						}
					}
				}
			case !inMatch && len(f) >= 2:
				if key == "challengeresponseauthentication" { // alias of KbdInteractiveAuthentication since OpenSSH 8.7
					key = "kbdinteractiveauthentication"
				}
				if _, seen := settings[key]; !seen {
					settings[key] = strings.ToLower(f[1])
				}
			}
		}
	}
	if _, ok := files["/etc/ssh/sshd_config"]; ok {
		walk("/etc/ssh/sshd_config", 0, false)
	} else {
		// Main file unreadable but drop-ins readable: read them in order.
		for _, p := range order {
			walk(p, 0, false)
		}
	}
	return settings
}

// parseSshdConfig computes effective PermitRootLogin and PasswordAuthentication.
func parseSshdConfig(content, osFamily string) (permitRoot, passAuth store.HardeningCheck, notes []string) {
	permitRoot = store.HardeningCheck{ID: "sshd_permit_root_login", Title: "SSH Root Login"}
	passAuth = store.HardeningCheck{ID: "sshd_password_auth", Title: "SSH Password Authentication"}

	if strings.Contains(content, "SSHD_UNREADABLE") || strings.TrimSpace(content) == "" {
		permitRoot.Status = "unknown"
		permitRoot.Detail = "unable to read sshd_config (permission denied)"
		passAuth.Status = "unknown"
		passAuth.Detail = "unable to read sshd_config (permission denied)"
		notes = append(notes, "sshd_config is not readable without elevated permissions")
		return permitRoot, passAuth, notes
	}

	cfg := sshdSettings(content)

	root, rootSet := cfg["permitrootlogin"]
	switch {
	case !rootSet && osFamily != "debian" && osFamily != "ubuntu" && osFamily != "alpine" && osFamily != "arch" && osFamily != "macos":
		// Upstream defaults to prohibit-password, but RHEL/CentOS builds up to 8 default to yes.
		permitRoot.Status = "unknown"
		permitRoot.Detail = "PermitRootLogin is unset and this distribution's compiled-in default differs between releases; root or passwordless sudo lets Pantau read it with sshd -T"
	case !rootSet:
		permitRoot.Status = "pass"
		permitRoot.Detail = "PermitRootLogin is unset (OpenSSH default: prohibit-password)"
	case root == "no":
		permitRoot.Status = "pass"
		permitRoot.Detail = "PermitRootLogin is disabled ('no')"
	case root == "prohibit-password" || root == "without-password":
		permitRoot.Status = "pass"
		permitRoot.Detail = fmt.Sprintf("PermitRootLogin is restricted ('%s')", root)
	case root == "forced-commands-only":
		permitRoot.Status = "pass"
		permitRoot.Detail = "PermitRootLogin is restricted to forced commands"
	case root == "yes":
		permitRoot.Status = "fail"
		permitRoot.Detail = "PermitRootLogin is enabled ('yes')"
	default:
		permitRoot.Status = "warn"
		permitRoot.Detail = fmt.Sprintf("PermitRootLogin set to '%s'", root)
	}

	pw, pwSet := cfg["passwordauthentication"]
	kbd, kbdSet := cfg["kbdinteractiveauthentication"]
	switch {
	case !pwSet:
		passAuth.Status = "warn"
		passAuth.Detail = "PasswordAuthentication is unset (OpenSSH default: yes)"
	case pw != "no":
		passAuth.Status = "warn"
		passAuth.Detail = fmt.Sprintf("PasswordAuthentication is enabled ('%s')", pw)
	case cfg["usepam"] == "yes" && (!kbdSet || kbd != "no"):
		// PAM's keyboard-interactive prompt asks for the same password (OpenSSH default: KbdInteractiveAuthentication yes).
		passAuth.Status = "warn"
		passAuth.Detail = "PasswordAuthentication is 'no', but KbdInteractiveAuthentication with UsePAM still accepts passwords"
	default:
		passAuth.Status = "pass"
		passAuth.Detail = "PasswordAuthentication is disabled ('no')"
	}

	return permitRoot, passAuth, notes
}

// parseAutoUpdates checks if automatic updates are enabled / active.
func parseAutoUpdates(content string) (store.HardeningCheck, []string) {
	cLower := strings.ToLower(content)

	// macOS SoftwareUpdate defaults check
	if strings.Contains(cLower, "defaults_unreadable") || strings.Contains(cLower, "com.apple.softwareupdate does not exist") {
		return store.HardeningCheck{
			ID:     "auto_security_updates",
			Title:  "Automatic Security Updates",
			Status: "unknown",
			Detail: "unable to read macOS SoftwareUpdate preferences",
		}, []string{"automatic update preferences could not be read"}
	}
	if strings.Contains(content, "CriticalUpdateInstall") || strings.Contains(content, "ConfigDataInstall") ||
		strings.Contains(content, "AutomaticDownload") || strings.Contains(content, "AutomaticallyInstallMacOSUpdates") {
		hasCritical := macAutoCriticalRe.MatchString(content)
		hasConfig := macAutoConfigRe.MatchString(content)
		if hasCritical && hasConfig {
			return store.HardeningCheck{
				ID:     "auto_security_updates",
				Title:  "Automatic Security Updates",
				Status: "pass",
				Detail: "macOS automatic security and configuration updates are enabled",
			}, nil
		}
		return store.HardeningCheck{
			ID:     "auto_security_updates",
			Title:  "Automatic Security Updates",
			Status: "warn",
			Detail: "macOS automatic security updates are disabled or incomplete",
		}, nil
	}

	check := func(status, detail string) store.HardeningCheck {
		return store.HardeningCheck{ID: "auto_security_updates", Title: "Automatic Security Updates", Status: status, Detail: detail}
	}
	// value returns the rest of the first line starting with prefix, without APT's quotes and semicolon.
	value := func(prefix string) string {
		for _, line := range strings.Split(content, "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(line), prefix); ok {
				return strings.Trim(strings.TrimSpace(v), `";`)
			}
		}
		return ""
	}
	unitOn := func(unit string) bool {
		return value(unit+"_ENABLED:") == "enabled" || value(unit+"_ACTIVE:") == "active"
	}
	// applyOn reads `apply_updates = yes` from a `grep -H` line; dnf-automatic and yum-cron default to download-only.
	applyOn := func(file string) bool {
		_, v, _ := strings.Cut(value(file+":"), "=")
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "yes", "true", "1", "on":
			return true
		}
		return false
	}

	switch {
	case unitOn("DNF_INSTALL_TIMER"):
		return check("pass", "dnf-automatic-install.timer installs updates automatically"), nil
	case unitOn("DNF_TIMER") && applyOn("/etc/dnf/automatic.conf"):
		return check("pass", "dnf-automatic.timer is active and apply_updates is on"), nil
	case unitOn("DNF_TIMER"):
		return check("warn", "dnf-automatic.timer is active, but apply_updates is off in /etc/dnf/automatic.conf, so updates are only downloaded"), nil
	case unitOn("YUM_CRON") && applyOn("/etc/yum/yum-cron.conf"):
		return check("pass", "yum-cron is active and apply_updates is on"), nil
	case unitOn("YUM_CRON"):
		return check("warn", "yum-cron is active, but apply_updates is off in /etc/yum/yum-cron.conf, so updates are only downloaded"), nil
	case strings.Contains(content, "---APT_CONFIG---"):
		// Effective `apt-config dump` values, so a later file that turns a setting off is honored.
		uu := value("APT::Periodic::Unattended-Upgrade ")
		lists := value("APT::Periodic::Update-Package-Lists ")
		timer := value("APT_TIMER_ENABLED:")
		switch {
		case (uu == "" || uu == "0") && lists != "" && lists != "0":
			return check("warn", "APT refreshes package lists, but APT::Periodic::Unattended-Upgrade is off, so nothing is installed"), nil
		case uu == "" || uu == "0":
			return check("warn", "APT::Periodic::Unattended-Upgrade is not enabled"), nil
		case !strings.Contains(content, "UU_INSTALLED"):
			return check("warn", "APT::Periodic::Unattended-Upgrade is on, but the unattended-upgrades package is not installed"), nil
		case timer == "disabled" || timer == "masked":
			return check("warn", fmt.Sprintf("apt-daily-upgrade.timer is %s, so unattended upgrades never run", timer)), nil
		default:
			return check("pass", fmt.Sprintf("unattended-upgrades runs daily (APT::Periodic::Unattended-Upgrade \"%s\")", uu)), nil
		}
	case strings.Contains(cLower, "permission denied") || strings.Contains(cLower, "failed to connect to bus"):
		return check("unknown", "unable to determine automatic update status"), []string{"automatic update service status could not be read"}
	default:
		return check("warn", "automatic security updates are not active or enabled"), nil
	}
}

// nftFiltersInput reports whether an nft base chain on the input hook drops by policy or ends in an
// unconditional drop/reject. Tables that only Docker, kube-proxy or fail2ban create (policy accept) do not count.
func nftFiltersInput(ruleset string) bool {
	inInput := false
	for _, line := range strings.Split(ruleset, "\n") {
		l := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(l, "chain ") || l == "}":
			inInput = false
		case strings.HasPrefix(l, "type filter hook input "):
			if strings.Contains(l, "policy drop") {
				return true
			}
			inInput = true
		case inInput:
			f := strings.Fields(l)
			for len(f) > 0 && f[0] == "counter" {
				f = f[1:]
				if len(f) >= 4 && f[0] == "packets" && f[2] == "bytes" {
					f = f[4:]
				}
			}
			if len(f) > 0 && (f[0] == "drop" || f[0] == "reject") {
				return true
			}
		}
	}
	return false
}

// iptablesFiltersInput reports whether the INPUT chain drops by policy or has an unconditional DROP/REJECT rule.
// Rules elsewhere (Docker's FORWARD/DOCKER chains, fail2ban jumps) leave inbound connections open.
func iptablesFiltersInput(rules string) bool {
	for _, line := range strings.Split(rules, "\n") {
		f := strings.Fields(line)
		switch {
		case len(f) == 3 && f[0] == "-P" && f[1] == "INPUT" && f[2] == "DROP":
			return true
		case len(f) >= 4 && f[0] == "-A" && f[1] == "INPUT" && f[2] == "-j" && (f[3] == "DROP" || f[3] == "REJECT"):
			return true
		case strings.HasPrefix(strings.TrimSpace(line), "Chain INPUT (policy DROP"): // `iptables -L -n` fallback
			return true
		}
	}
	return false
}

// parseFirewall inspects ufw, firewalld, nftables, and iptables.
func parseFirewall(content string) (store.HardeningCheck, []string) {
	cLower := strings.ToLower(content)

	if strings.Contains(cLower, "socketfilterfw_unreadable") {
		return store.HardeningCheck{
			ID:     "firewall_active",
			Title:  "Host Firewall",
			Status: "unknown",
			Detail: "Application Firewall status could not be read",
		}, []string{"firewall status could not be read"}
	}
	if strings.Contains(cLower, "state = 1") || strings.Contains(cLower, "state = 2") ||
		(strings.Contains(cLower, "firewall is enabled") && !strings.Contains(cLower, "disabled")) {
		return store.HardeningCheck{
			ID:     "firewall_active",
			Title:  "Host Firewall",
			Status: "pass",
			Detail: "Application Firewall is enabled",
		}, nil
	}
	if strings.Contains(cLower, "state = 0") || strings.Contains(cLower, "firewall is disabled") {
		return store.HardeningCheck{
			ID:     "firewall_active",
			Title:  "Host Firewall",
			Status: "warn",
			Detail: "Application Firewall is disabled",
		}, nil
	}

	check := func(status, detail string) store.HardeningCheck {
		return store.HardeningCheck{ID: "firewall_active", Title: "Host Firewall", Status: status, Detail: detail}
	}

	if strings.Contains(cLower, "status: active") {
		return check("pass", "ufw firewall is active"), nil
	}

	fwState := firstLines(extractSection(content, "---FIREWALLD---", "---NFT---"), 1)
	if fwState == "active" || fwState == "running" {
		return check("pass", "firewalld is active"), nil
	}

	nftSec := extractSection(content, "---NFT---", "---IPTABLES---")
	if nftFiltersInput(nftSec) {
		return check("pass", "nftables input chain drops unmatched traffic"), nil
	}
	iptSec := extractSection(content, "---IPTABLES---", "")
	if iptablesFiltersInput(iptSec) {
		return check("pass", "iptables INPUT chain drops unmatched traffic"), nil
	}
	if strings.Contains(nftSec, "hook input") || strings.Contains(iptSec, "-P INPUT") || strings.Contains(iptSec, "Chain INPUT") {
		return check("warn", "rules are loaded, but no input chain drops unmatched traffic (policy accept); Docker or fail2ban rules alone do not filter inbound connections"), nil
	}

	if strings.Contains(cLower, "permission denied") ||
		strings.Contains(cLower, "operation not permitted") ||
		strings.Contains(cLower, "must be root") ||
		strings.Contains(cLower, "need to be root") {
		return check("unknown", "unable to read firewall status without root privileges"), []string{"firewall status could not be read without root privileges"}
	}

	if strings.Contains(cLower, "status: inactive") || fwState == "inactive" {
		return check("warn", "firewall is inactive"), nil
	}

	return check("warn", "no active firewall detected (ufw, firewalld, nftables, iptables)"), nil
}

type softwareUpdateEntry struct {
	label   string
	title   string
	version string
	action  string
	rawText string
}

func parseSoftwareUpdateEntries(output string) []softwareUpdateEntry {
	lines := strings.Split(output, "\n")
	var entries []softwareUpdateEntry
	var cur *softwareUpdateEntry

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "* Label:") || (strings.HasPrefix(trimmed, "* ") && !strings.Contains(trimmed, "* Title:")) {
			if cur != nil {
				if cur.title == "" {
					cur.title = cur.label
				}
				entries = append(entries, *cur)
			}
			label := strings.TrimSpace(strings.TrimPrefix(trimmed, "* Label:"))
			if strings.HasPrefix(trimmed, "* ") && !strings.HasPrefix(trimmed, "* Label:") {
				label = strings.TrimSpace(strings.TrimPrefix(trimmed, "* "))
			}
			cur = &softwareUpdateEntry{
				label:   label,
				rawText: trimmed,
			}
			continue
		}

		if cur != nil {
			cur.rawText += " " + trimmed
			if idx := strings.Index(trimmed, "Title:"); idx != -1 {
				rest := trimmed[idx+len("Title:"):]
				if cIdx := strings.Index(rest, ","); cIdx != -1 {
					cur.title = strings.TrimSpace(rest[:cIdx])
				} else {
					cur.title = strings.TrimSpace(rest)
				}
			}
			if idx := strings.Index(trimmed, "Version:"); idx != -1 {
				rest := trimmed[idx+len("Version:"):]
				if cIdx := strings.Index(rest, ","); cIdx != -1 {
					cur.version = strings.TrimSpace(rest[:cIdx])
				} else {
					cur.version = strings.TrimSpace(rest)
				}
			}
			if idx := strings.Index(trimmed, "Action:"); idx != -1 {
				rest := trimmed[idx+len("Action:"):]
				if cIdx := strings.Index(rest, ","); cIdx != -1 {
					cur.action = strings.TrimSpace(rest[:cIdx])
				} else {
					cur.action = strings.TrimSpace(rest)
				}
			}
		}
	}
	if cur != nil {
		if cur.title == "" {
			cur.title = cur.label
		}
		entries = append(entries, *cur)
	}
	return entries
}

func isMacOSSecurityUpdate(title, label string) bool {
	tLower := strings.ToLower(title)
	lLower := strings.ToLower(label)
	return strings.Contains(tLower, "safari") || strings.Contains(lLower, "safari") ||
		strings.Contains(tLower, "macos") || strings.Contains(lLower, "macos") ||
		strings.Contains(tLower, "mac os") || strings.Contains(lLower, "mac os") ||
		strings.Contains(tLower, "security update") || strings.Contains(lLower, "security update")
}

// parseSoftwareUpdate parses macOS softwareupdate -l output.
func parseSoftwareUpdate(output string, osProductVersion string) (findings []store.SecurityFinding, pendingSec, pendingTot int, rebootRequired bool, notes []string) {
	trimmed := strings.TrimSpace(output)
	tLower := strings.ToLower(trimmed)

	isClean := strings.Contains(trimmed, "No new software available.")
	entries := parseSoftwareUpdateEntries(trimmed)

	if len(entries) > 0 {
		pendingTot = len(entries)
		for _, entry := range entries {
			if strings.EqualFold(entry.action, "restart") || strings.Contains(strings.ToLower(entry.rawText), "action: restart") {
				rebootRequired = true
			}

			if isMacOSSecurityUpdate(entry.title, entry.label) {
				pendingSec++
				installedVer := ""
				if (strings.Contains(strings.ToLower(entry.title), "macos") || strings.Contains(strings.ToLower(entry.label), "macos")) && osProductVersion != "" {
					installedVer = osProductVersion
				}
				findings = append(findings, store.SecurityFinding{
					Package:          entry.title,
					InstalledVersion: installedVer,
					FixedVersion:     entry.version,
					Severity:         "unknown",
					Advisory:         "",
				})
			}
		}

		if osProductVersion != "" {
			notes = append(notes, fmt.Sprintf("macOS %s does not label security updates; macOS and Safari updates are counted as security updates", osProductVersion))
		} else {
			notes = append(notes, "macOS does not label security updates; macOS and Safari updates are counted as security updates")
		}
		return findings, pendingSec, pendingTot, rebootRequired, notes
	}

	if isClean {
		if osProductVersion != "" {
			notes = append(notes, fmt.Sprintf("macOS %s does not label security updates; macOS and Safari updates are counted as security updates", osProductVersion))
		} else {
			notes = append(notes, "macOS does not label security updates; macOS and Safari updates are counted as security updates")
		}
		return findings, 0, 0, false, notes
	}

	// Check for unreachable / network failure / timeout
	if strings.Contains(tLower, "can't connect") ||
		strings.Contains(tLower, "cannot connect") ||
		strings.Contains(tLower, "network connection was lost") ||
		strings.Contains(tLower, "timed out") ||
		strings.Contains(tLower, "could not resolve") ||
		strings.Contains(tLower, "error:") ||
		strings.Contains(tLower, "failed") ||
		trimmed == "" ||
		trimmed == "MGR:softwareupdate" {
		notes = append(notes, "softwareupdate failed to reach Apple update servers or timed out; pending updates could not be determined")
		return findings, 0, 0, false, notes
	}

	notes = append(notes, "softwareupdate output could not be parsed; pending updates could not be determined")
	return findings, 0, 0, false, notes
}
