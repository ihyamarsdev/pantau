package inspector

import (
	"context"
	"errors"
	"fmt"
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
const SecurityScanBatchCmd = `echo "===OS_RELEASE==="
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
if [ -r /etc/ssh/sshd_config ]; then
    echo "FILE:/etc/ssh/sshd_config"
    cat /etc/ssh/sshd_config 2>/dev/null
    for f in /etc/ssh/sshd_config.d/*.conf; do
        if [ -r "$f" ]; then
            echo "FILE:$f"
            cat "$f" 2>/dev/null
        fi
    done
else
    echo "SSHD_UNREADABLE"
fi

echo "===AUTO_UPDATES==="
if command -v systemctl >/dev/null 2>&1; then
    echo "UNATTENDED_ENABLED:$(systemctl is-enabled unattended-upgrades 2>/dev/null)"
    echo "UNATTENDED_ACTIVE:$(systemctl is-active unattended-upgrades 2>/dev/null)"
    echo "DNF_TIMER_ENABLED:$(systemctl is-enabled dnf-automatic.timer 2>/dev/null)"
    echo "DNF_TIMER_ACTIVE:$(systemctl is-active dnf-automatic.timer 2>/dev/null)"
    echo "YUM_CRON_ENABLED:$(systemctl is-enabled yum-cron 2>/dev/null)"
    echo "YUM_CRON_ACTIVE:$(systemctl is-active yum-cron 2>/dev/null)"
fi
grep -h -s -E 'APT::Periodic::(Update-Package-Lists|Unattended-Upgrade)' /etc/apt/apt.conf.d/* 2>/dev/null

echo "===FIREWALL==="
if command -v ufw >/dev/null 2>&1; then
    echo "---UFW---"
    ufw status 2>&1
fi
if command -v systemctl >/dev/null 2>&1; then
    echo "---FIREWALLD---"
    systemctl is-active firewalld 2>&1
fi
if command -v nft >/dev/null 2>&1; then
    echo "---NFT---"
    nft list ruleset 2>&1
fi
if command -v iptables >/dev/null 2>&1; then
    echo "---IPTABLES---"
    iptables -S 2>&1 || iptables -L -n 2>&1
fi
`

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

	stdout, _, _, err := runner.Exec(SecurityScanBatchCmd)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("exec security scan timed out: %w", ctx.Err())
		}
		return fmt.Errorf("exec security scan: %w", err)
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
	}

	var (
		findings        []store.SecurityFinding
		pendingSecurity int
		pendingTotal    int
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
	default:
		// Unsupported package manager
		notes = append(notes, fmt.Sprintf("package manager '%s' is not supported for security updates detection", packageManager))
	}
	notes = append(notes, updateNotes...)

	// Reboot required
	rebootRequired := parseRebootRequired(rebootSec)

	// Repo cache age
	cacheAgeHours, cacheNotes := parseRepoCacheAge(cacheSec, packageManager)
	notes = append(notes, cacheNotes...)

	// Hardening checks
	hardening, hardeningNotes := parseHardeningChecks(sshdSec, autoSec, firewallSec)
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
	if packageManager != "apt" && packageManager != "dnf" && packageManager != "yum" {
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
		if strings.Contains(cLower, "ubuntu") {
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
func parseHardeningChecks(sshdSec, autoSec, firewallSec string) (checks []store.HardeningCheck, notes []string) {
	permitRoot, passAuth, sshdNotes := parseSshdConfig(sshdSec)
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

// parseSshdConfig computes effective PermitRootLogin and PasswordAuthentication.
func parseSshdConfig(content string) (permitRoot, passAuth store.HardeningCheck, notes []string) {
	permitRoot = store.HardeningCheck{
		ID:     "sshd_permit_root_login",
		Title:  "SSH Root Login",
		Status: "pass",
		Detail: "PermitRootLogin is unset (OpenSSH default: prohibit-password)",
	}
	passAuth = store.HardeningCheck{
		ID:     "sshd_password_auth",
		Title:  "SSH Password Authentication",
		Status: "warn",
		Detail: "PasswordAuthentication is unset (OpenSSH default: yes)",
	}

	if strings.Contains(content, "SSHD_UNREADABLE") || strings.TrimSpace(content) == "" {
		permitRoot.Status = "unknown"
		permitRoot.Detail = "unable to read sshd_config (permission denied)"
		passAuth.Status = "unknown"
		passAuth.Detail = "unable to read sshd_config (permission denied)"
		notes = append(notes, "sshd_config is not readable without elevated permissions")
		return permitRoot, passAuth, notes
	}

	rootFound := false
	passFound := false
	inMatch := false

	lines := strings.Split(content, "\n")
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "FILE:") {
			inMatch = false
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		key := strings.ToLower(fields[0])
		if key == "match" {
			inMatch = true
			continue
		}

		if inMatch {
			// Directives inside Match block do not affect the global default
			continue
		}

		if !rootFound && key == "permitrootlogin" && len(fields) >= 2 {
			rootFound = true
			val := strings.ToLower(fields[1])
			switch val {
			case "no":
				permitRoot.Status = "pass"
				permitRoot.Detail = "PermitRootLogin is disabled ('no')"
			case "prohibit-password", "without-password":
				permitRoot.Status = "pass"
				permitRoot.Detail = fmt.Sprintf("PermitRootLogin is restricted ('%s')", val)
			case "forced-commands-only":
				permitRoot.Status = "pass"
				permitRoot.Detail = "PermitRootLogin is restricted to forced commands"
			case "yes":
				permitRoot.Status = "fail"
				permitRoot.Detail = "PermitRootLogin is enabled ('yes')"
			default:
				permitRoot.Status = "warn"
				permitRoot.Detail = fmt.Sprintf("PermitRootLogin set to '%s'", val)
			}
		}

		if !passFound && key == "passwordauthentication" && len(fields) >= 2 {
			passFound = true
			val := strings.ToLower(fields[1])
			if val == "no" {
				passAuth.Status = "pass"
				passAuth.Detail = "PasswordAuthentication is disabled ('no')"
			} else {
				passAuth.Status = "warn"
				passAuth.Detail = fmt.Sprintf("PasswordAuthentication is enabled ('%s')", val)
			}
		}
	}

	return permitRoot, passAuth, notes
}

// parseAutoUpdates checks if automatic updates are enabled / active.
func parseAutoUpdates(content string) (store.HardeningCheck, []string) {
	cLower := strings.ToLower(content)

	switch {
	case strings.Contains(cLower, "unattended_enabled:enabled") || strings.Contains(cLower, "unattended_active:active"):
		return store.HardeningCheck{
			ID:     "auto_security_updates",
			Title:  "Automatic Security Updates",
			Status: "pass",
			Detail: "unattended-upgrades is active/enabled",
		}, nil
	case strings.Contains(cLower, "dnf_timer_enabled:enabled") || strings.Contains(cLower, "dnf_timer_active:active"):
		return store.HardeningCheck{
			ID:     "auto_security_updates",
			Title:  "Automatic Security Updates",
			Status: "pass",
			Detail: "dnf-automatic timer is active/enabled",
		}, nil
	case strings.Contains(cLower, "yum_cron_enabled:enabled") || strings.Contains(cLower, "yum_cron_active:active") || strings.Contains(cLower, "yum-cron 0:off 1:off 2:on 3:on"):
		return store.HardeningCheck{
			ID:     "auto_security_updates",
			Title:  "Automatic Security Updates",
			Status: "pass",
			Detail: "yum-cron is active/enabled",
		}, nil
	case strings.Contains(content, `APT::Periodic::Unattended-Upgrade "1"`) || strings.Contains(content, `APT::Periodic::Update-Package-Lists "1"`):
		return store.HardeningCheck{
			ID:     "auto_security_updates",
			Title:  "Automatic Security Updates",
			Status: "pass",
			Detail: "APT periodic unattended upgrades configured",
		}, nil
	case strings.Contains(cLower, "permission denied") || strings.Contains(cLower, "failed to connect to bus"):
		return store.HardeningCheck{
			ID:     "auto_security_updates",
			Title:  "Automatic Security Updates",
			Status: "unknown",
			Detail: "unable to determine automatic update status",
		}, []string{"automatic update service status could not be read"}
	default:
		return store.HardeningCheck{
			ID:     "auto_security_updates",
			Title:  "Automatic Security Updates",
			Status: "warn",
			Detail: "automatic security updates are not active or enabled",
		}, nil
	}
}

// parseFirewall inspects ufw, firewalld, nftables, and iptables.
func parseFirewall(content string) (store.HardeningCheck, []string) {
	cLower := strings.ToLower(content)

	if strings.Contains(cLower, "status: active") {
		return store.HardeningCheck{
			ID:     "firewall_active",
			Title:  "Host Firewall",
			Status: "pass",
			Detail: "ufw firewall is active",
		}, nil
	}

	fwSec := extractSection(content, "---FIREWALLD---", "---NFT---")
	if strings.TrimSpace(fwSec) == "active" || strings.TrimSpace(fwSec) == "running" {
		return store.HardeningCheck{
			ID:     "firewall_active",
			Title:  "Host Firewall",
			Status: "pass",
			Detail: "firewalld is active",
		}, nil
	}

	nftSec := extractSection(content, "---NFT---", "---IPTABLES---")
	if strings.Contains(nftSec, "table ") && !strings.Contains(strings.ToLower(nftSec), "permission denied") {
		return store.HardeningCheck{
			ID:     "firewall_active",
			Title:  "Host Firewall",
			Status: "pass",
			Detail: "nftables rules are active",
		}, nil
	}

	iptSec := extractSection(content, "---IPTABLES---", "")
	if (strings.Contains(iptSec, "-A ") || (strings.Contains(iptSec, "Chain ") && !strings.Contains(iptSec, "Chain INPUT (policy ACCEPT)"))) &&
		!strings.Contains(strings.ToLower(iptSec), "permission denied") {
		return store.HardeningCheck{
			ID:     "firewall_active",
			Title:  "Host Firewall",
			Status: "pass",
			Detail: "iptables rules are configured",
		}, nil
	}

	if strings.Contains(cLower, "permission denied") ||
		strings.Contains(cLower, "must be root") ||
		strings.Contains(cLower, "you must be root") ||
		strings.Contains(cLower, "need to be root") {
		return store.HardeningCheck{
			ID:     "firewall_active",
			Title:  "Host Firewall",
			Status: "unknown",
			Detail: "unable to read firewall status without root privileges",
		}, []string{"firewall status could not be read without root privileges"}
	}

	if strings.Contains(cLower, "status: inactive") || strings.TrimSpace(fwSec) == "inactive" {
		return store.HardeningCheck{
			ID:     "firewall_active",
			Title:  "Host Firewall",
			Status: "warn",
			Detail: "firewall is inactive",
		}, nil
	}

	return store.HardeningCheck{
		ID:     "firewall_active",
		Title:  "Host Firewall",
		Status: "warn",
		Detail: "no active firewall detected (ufw, firewalld, nftables, iptables)",
	}, nil
}
