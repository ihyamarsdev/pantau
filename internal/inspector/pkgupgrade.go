package inspector

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"pantau/internal/store"
)

var (
	// UpgradeInFlight guards against concurrent package upgrades on the same host.
	upgradeInFlight sync.Map
)

// ErrUpgradeInProgress indicates an upgrade is already running on the host.
var ErrUpgradeInProgress = fmt.Errorf("package upgrade already in progress for this host")

// TryLockUpgrade attempts to acquire the upgrade lock for hostID.
func TryLockUpgrade(hostID int64) (func(), error) {
	if _, loaded := upgradeInFlight.LoadOrStore(hostID, struct{}{}); loaded {
		return nil, ErrUpgradeInProgress
	}
	return func() {
		upgradeInFlight.Delete(hostID)
	}, nil
}

// UpgradePreviewPackage represents a pending package item in the preview.
type UpgradePreviewPackage struct {
	Name     string `json:"name"`
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"`
	Security bool   `json:"security"`
}

// UpgradePreview represents the result of package upgrade preview inspection.
type UpgradePreview struct {
	OK              bool                    `json:"ok"`
	Supported       bool                    `json:"supported"`
	Manager         string                  `json:"manager,omitempty"`
	PendingTotal    int                     `json:"pending_total"`
	PendingSecurity int                     `json:"pending_security"`
	Packages        []UpgradePreviewPackage `json:"packages"`
	RebootRequired  bool                    `json:"reboot_required"`
	// RepoCacheAgeHours is the age of the local package metadata in hours; -1 when unknown. A stale
	// cache can hide a newer release, so the caller says so instead of claiming "up to date".
	RepoCacheAgeHours int    `json:"repo_cache_age_hours"`
	Note              string `json:"note,omitempty"`
}

// UpgradePreviewDetectionScript returns the batch discovery shell script for package upgrade preview.
const UpgradePreviewDetectionScript = `echo "===OS_RELEASE==="
cat /etc/os-release /usr/lib/os-release 2>/dev/null || cat /etc/redhat-release /etc/debian_version 2>/dev/null
echo "===REBOOT==="
if [ -f /var/run/reboot-required ] || [ -f /run/reboot-required ]; then
    echo "REBOOT_REQUIRED"
fi
if command -v needs-restarting >/dev/null 2>&1; then
    needs-restarting -r >/dev/null 2>&1
    echo "NEEDS_RESTARTING:$?"
fi
echo "===CACHE_AGE==="
date +%s 2>/dev/null
echo "---APT---"
stat -c %Y /var/lib/apt/periodic/update-success-stamp 2>/dev/null || stat -c %Y /var/lib/apt/lists 2>/dev/null
echo "---DNF---"
stat -c %Y /var/cache/dnf 2>/dev/null || stat -c %Y /var/cache/yum 2>/dev/null
echo "---APK---"
stat -c %Y /var/cache/apk 2>/dev/null || stat -c %Y /etc/apk/repositories 2>/dev/null
echo "---PACMAN---"
stat -c %Y /var/cache/pacman/pkg 2>/dev/null || stat -c %Y /var/lib/pacman/sync 2>/dev/null
echo "===UPDATES==="
D=""
if [ "$(id -u)" != "0" ] && sudo -n true 2>/dev/null; then D="sudo -n"; fi
if command -v apt-get >/dev/null 2>&1; then
    echo "MGR:apt"
    apt-get -s dist-upgrade 2>&1 || apt-get -s upgrade 2>&1
elif command -v dnf >/dev/null 2>&1; then
    echo "MGR:dnf"
    echo "---SEC---"
    $D dnf -q updateinfo list security </dev/null 2>&1
    echo "---TOTAL---"
    $D dnf check-update -q </dev/null 2>&1
    echo "---RPM_QA---"
    rpm -qa --qf '%{NAME}\t%{VERSION}-%{RELEASE}\n' 2>/dev/null
elif command -v yum >/dev/null 2>&1; then
    echo "MGR:yum"
    echo "---SEC---"
    $D yum updateinfo list security </dev/null 2>&1 || $D yum --security check-update </dev/null 2>&1
    echo "---TOTAL---"
    $D yum check-update -q </dev/null 2>&1
    echo "---RPM_QA---"
    rpm -qa --qf '%{NAME}\t%{VERSION}-%{RELEASE}\n' 2>/dev/null
elif [ -f /lib/apk/db/installed ] || command -v apk >/dev/null 2>&1; then
    echo "MGR:apk"
    echo "---TOTAL---"
    apk version -l "<" 2>&1
elif command -v pacman >/dev/null 2>&1; then
    echo "MGR:pacman"
    echo "---TOTAL---"
    pacman -Qu 2>&1
else
    echo "MGR:unknown"
fi
`

// ParseUpgradePreview parses the output of UpgradePreviewDetectionScript.
func ParseUpgradePreview(stdout string) *UpgradePreview {
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" {
		return &UpgradePreview{
			OK:        false,
			Supported: false,
			Packages:  []UpgradePreviewPackage{},
			Note:      "no output returned from remote host",
		}
	}

	osSec := extractSection(stdout, "===OS_RELEASE===", "===REBOOT===")
	rebootSec := extractSection(stdout, "===REBOOT===", "===CACHE_AGE===")
	cacheSec := extractSection(stdout, "===CACHE_AGE===", "===UPDATES===")
	updatesSec := extractSection(stdout, "===UPDATES===", "")

	osFamily, detectedMgr := parseOSFamily(osSec)
	if osFamily == "macos" {
		return &UpgradePreview{
			OK:        true,
			Supported: false,
			Manager:   "macos",
			Packages:  []UpgradePreviewPackage{},
			Note:      "macOS software updates are not supported for agentless upgrade",
		}
	}

	mgr := detectedMgr
	if strings.Contains(updatesSec, "MGR:apt") {
		mgr = "apt"
	} else if strings.Contains(updatesSec, "MGR:dnf") {
		mgr = "dnf"
	} else if strings.Contains(updatesSec, "MGR:yum") {
		mgr = "yum"
	} else if strings.Contains(updatesSec, "MGR:apk") {
		mgr = "apk"
	} else if strings.Contains(updatesSec, "MGR:pacman") {
		mgr = "pacman"
	}

	reboot := parseRebootRequired(rebootSec)

	preview := &UpgradePreview{}
	switch mgr {
	case "apt":
		preview = parseAptUpgradePreview(updatesSec, reboot)
	case "dnf", "yum":
		secSub := extractSection(updatesSec, "---SEC---", "---TOTAL---")
		totSub := extractSection(updatesSec, "---TOTAL---", "---RPM_QA---")
		rpmQaSub := extractSection(updatesSec, "---RPM_QA---", "")
		preview = parseRpmUpgradePreview(mgr, secSub, totSub, rpmQaSub, reboot)
	case "apk":
		preview = parseApkUpgradePreview(extractSection(updatesSec, "---TOTAL---", ""), reboot)
	case "pacman":
		preview = parsePacmanUpgradePreview(extractSection(updatesSec, "---TOTAL---", ""), reboot)
	default:
		return &UpgradePreview{
			OK:        true,
			Supported: false,
			Manager:   mgr,
			Packages:  []UpgradePreviewPackage{},
			Note:      fmt.Sprintf("package manager %q is not supported for upgrade", mgr),
		}
	}

	// A stale local metadata cache can hide a newer release, so "0 pending" must not read as "up to date".
	ageHours, cacheNotes := parseRepoCacheAge(cacheSec, mgr)
	preview.RepoCacheAgeHours = ageHours
	if len(cacheNotes) > 0 {
		if preview.Note != "" {
			preview.Note += "; "
		}
		preview.Note += strings.Join(cacheNotes, "; ")
	}
	return preview
}

func parseAptUpgradePreview(updatesSec string, reboot bool) *UpgradePreview {
	findings, pendingSec, pendingTot, notes := parseAptUpdates(updatesSec)
	secMap := make(map[string]store.SecurityFinding)
	for _, f := range findings {
		secMap[f.Package] = f
	}

	var pkgs []UpgradePreviewPackage
	lines := strings.Split(updatesSec, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "Inst ") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}
		pkgName := fields[1]

		var installedVer, fixedVer string
		startParen := strings.Index(trimmed, "(")
		endParen := strings.LastIndex(trimmed, ")")
		if startParen != -1 && endParen > startParen {
			inside := trimmed[startParen+1 : endParen]
			insideFields := strings.Fields(inside)
			if len(insideFields) > 0 {
				fixedVer = insideFields[0]
			}
		}
		if startBracket := strings.Index(trimmed, "["); startBracket != -1 && startBracket < startParen {
			if endBracket := strings.Index(trimmed[startBracket:], "]"); endBracket != -1 {
				installedVer = trimmed[startBracket+1 : startBracket+endBracket]
			}
		}

		_, isSec := secMap[pkgName]
		pkgs = append(pkgs, UpgradePreviewPackage{
			Name:     pkgName,
			From:     installedVer,
			To:       fixedVer,
			Security: isSec,
		})
		if len(pkgs) >= 200 {
			break
		}
	}

	if pkgs == nil {
		pkgs = []UpgradePreviewPackage{}
	}

	// apt prints "E: ..." when it could not read the package lists. Reporting that as "0 pending" would
	// tell the operator the host is up to date, so it must be a failed check, not a clean one.
	if len(notes) > 0 {
		for _, n := range notes {
			if strings.HasPrefix(n, "apt: E:") || strings.Contains(n, "Permission denied") {
				return &UpgradePreview{
					OK:        false,
					Supported: true,
					Manager:   "apt",
					Packages:  []UpgradePreviewPackage{},
					Note:      strings.Join(notes, "; "),
				}
			}
		}
	}

	var note string
	if len(notes) > 0 {
		note = strings.Join(notes, "; ")
	}

	return &UpgradePreview{
		OK:              true,
		Supported:       true,
		Manager:         "apt",
		PendingTotal:    pendingTot,
		PendingSecurity: pendingSec,
		Packages:        pkgs,
		RebootRequired:  reboot,
		Note:            note,
	}
}

func parseRpmUpgradePreview(mgr, secSub, totSub, rpmQaSub string, reboot bool) *UpgradePreview {
	// dnf/yum print "Error: ..." (e.g. repo metadata or GPG failure) instead of a package list: that must not read as "0 pending".
	for _, sub := range []string{totSub, secSub} {
		for _, l := range strings.Split(sub, "\n") {
			if i := strings.Index(l, "Error:"); i >= 0 {
				return &UpgradePreview{OK: false, Supported: true, Manager: mgr, Packages: []UpgradePreviewPackage{}, RebootRequired: reboot, Note: strings.TrimSpace(l[i:])}
			}
		}
	}
	findings, _, _, notes := parseRpmUpdates(secSub, totSub, rpmQaSub)
	secMap := make(map[string]store.SecurityFinding)
	for _, f := range findings {
		secMap[f.Package] = f
	}

	// parseRpmUpdates counts advisory lines (one package can appear under many advisories, so "security 545" vs
	// 148 real packages); the operator needs distinct packages. "python3.12.x86_64" -> "python3.12".
	pkgBase := func(nameArch string) string {
		if i := strings.LastIndex(nameArch, "."); i > 0 {
			return nameArch[:i]
		}
		return nameArch
	}
	secSet := make(map[string]bool)
	totSet := make(map[string]bool)
	for _, f := range findings {
		secSet[f.Package] = true
		totSet[f.Package] = true
	}
	for _, line := range strings.Split(totSub, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.Contains(fields[0], ".") && !strings.HasPrefix(fields[0], "Obsoleting") {
			totSet[pkgBase(fields[0])] = true
		}
	}

	installedMap := make(map[string]string)
	for _, line := range strings.Split(rpmQaSub, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "\t", 2)
		if len(parts) == 2 {
			installedMap[parts[0]] = parts[1]
		}
	}

	var pkgs []UpgradePreviewPackage
	seen := make(map[string]bool)

	// Add security packages first
	for _, f := range findings {
		if !seen[f.Package] {
			seen[f.Package] = true
			pkgs = append(pkgs, UpgradePreviewPackage{
				Name:     f.Package,
				From:     f.InstalledVersion,
				To:       f.FixedVersion,
				Security: true,
			})
			if len(pkgs) >= 200 {
				break
			}
		}
	}

	// Add other packages from total lines
	if len(pkgs) < 200 {
		totalLines := strings.Split(totSub, "\n")
		for _, line := range totalLines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "Loaded plugins") ||
				strings.HasPrefix(trimmed, "Obsoleting Packages") ||
				strings.HasPrefix(trimmed, "Last metadata") {
				continue
			}
			fields := strings.Fields(trimmed)
			if len(fields) >= 2 && strings.Contains(fields[0], ".") {
				pkgName := pkgBase(fields[0])
				if !seen[pkgName] {
					seen[pkgName] = true
					fixedVer := fields[1]
					pkgs = append(pkgs, UpgradePreviewPackage{
						Name:     pkgName,
						From:     installedMap[pkgName],
						To:       fixedVer,
						Security: false,
					})
					if len(pkgs) >= 200 {
						break
					}
				}
			}
		}
	}

	if pkgs == nil {
		pkgs = []UpgradePreviewPackage{}
	}

	var note string
	if len(notes) > 0 {
		note = strings.Join(notes, "; ")
	}

	return &UpgradePreview{
		OK:              true,
		Supported:       true,
		Manager:         mgr,
		PendingTotal:    len(totSet),
		PendingSecurity: len(secSet),
		Packages:        pkgs,
		RebootRequired:  reboot,
		Note:            note,
	}
}

// parseApkUpgradePreview parses `apk version -l "<"`, whose rows are
// "<installed-version> < <available-version>" after the "Installed: Available:" header.
// Alpine does not separate security from ordinary updates, so every pending package is
// reported without a security flag.
func parseApkUpgradePreview(totSub string, reboot bool) *UpgradePreview {
	var pkgs []UpgradePreviewPackage
	for _, line := range strings.Split(totSub, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "Installed:") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 3 || fields[1] != "<" {
			continue
		}
		// "pkg-1.2.3-r0 < 1.2.4-r0": the name is the package name without its version suffix.
		name := apkPackageName(fields[0])
		if name == "" {
			continue
		}
		// The version columns hold versions only ("1.2.3-r0"), like apt and rpm do; the package name
		// belongs in the name column, not repeated inside the version.
		installedVer := strings.TrimPrefix(fields[0], name)
		installedVer = strings.TrimPrefix(installedVer, "-")
		pkgs = append(pkgs, UpgradePreviewPackage{
			Name: name,
			From: installedVer,
			To:   fields[2],
		})
		if len(pkgs) >= 200 {
			break
		}
	}
	if pkgs == nil {
		pkgs = []UpgradePreviewPackage{}
	}
	return &UpgradePreview{
		OK:             true,
		Supported:      true,
		Manager:        "apk",
		PendingTotal:   len(pkgs),
		Packages:       pkgs,
		RebootRequired: reboot,
		Note:           "alpine (apk) does not separate security updates from ordinary ones",
	}
}

// apkPackageName strips the version suffix from an Alpine package spec ("musl-1.2.5-r4" -> "musl").
func apkPackageName(spec string) string {
	s := strings.TrimSpace(spec)
	if i := strings.LastIndex(s, "-"); i > 0 {
		if j := strings.LastIndex(s[:i], "-"); j > 0 {
			return s[:j]
		}
		return s[:i]
	}
	return s
}

// parsePacmanUpgradePreview parses `pacman -Qu`, whose rows are
// "<name> <installed> -> <available>", optionally followed by "[ignored]".
// An ignored package is a hold the administrator set, so it is not a pending update.
func parsePacmanUpgradePreview(totSub string, reboot bool) *UpgradePreview {
	var pkgs []UpgradePreviewPackage
	for _, line := range strings.Split(totSub, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.Contains(trimmed, "[ignored]") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 4 || fields[2] != "->" {
			continue
		}
		pkgs = append(pkgs, UpgradePreviewPackage{
			Name: fields[0],
			From: fields[1],
			To:   fields[3],
		})
		if len(pkgs) >= 200 {
			break
		}
	}
	if pkgs == nil {
		pkgs = []UpgradePreviewPackage{}
	}
	return &UpgradePreview{
		OK:             true,
		Supported:      true,
		Manager:        "pacman",
		PendingTotal:   len(pkgs),
		Packages:       pkgs,
		RebootRequired: reboot,
		Note:           "arch (pacman) does not separate security updates from ordinary ones",
	}
}

// BuildUpgradeCommand validates mode and builds the wrapped upgrade command string.
// Privilege fallback: runs as SSH user; if not root, uses sudo -n (never password).
// If sudo is required but not available (passwordless), the command fails with a clear error.
func BuildUpgradeCommand(manager, mode string) (string, error) {
	var innerCmd string
	switch manager {
	case "apt":
		if mode != "all" {
			return "", fmt.Errorf("apt does not have a reliable security-only upgrade mode (only 'all' is supported)")
		}
		innerCmd = "DEBIAN_FRONTEND=noninteractive apt-get update && apt-get -y -o Dpkg::Options::=--force-confold upgrade"
	case "dnf":
		switch mode {
		case "security":
			innerCmd = "dnf -y upgrade --security"
		case "all":
			innerCmd = "dnf -y upgrade"
		default:
			return "", fmt.Errorf("invalid mode %q for dnf (must be 'all' or 'security')", mode)
		}
	case "yum":
		switch mode {
		case "security":
			innerCmd = "yum -y upgrade --security"
		case "all":
			innerCmd = "yum -y upgrade"
		default:
			return "", fmt.Errorf("invalid mode %q for yum (must be 'all' or 'security')", mode)
		}
	case "apk":
		if mode != "all" {
			return "", fmt.Errorf("apk does not have a reliable security-only upgrade mode (only 'all' is supported)")
		}
		innerCmd = "apk update && apk upgrade --no-interactive"
	case "pacman":
		if mode != "all" {
			return "", fmt.Errorf("pacman does not have a reliable security-only upgrade mode (only 'all' is supported)")
		}
		innerCmd = "pacman -Syu --noconfirm"
	default:
		return "", fmt.Errorf("unsupported package manager: %s", manager)
	}

	return wrapPrivilegedCommand(innerCmd), nil
}

// pkgNameRe accepts distro package names (letters, digits, + . _ -, and ":arch" qualifiers). The leading
// alphanumeric keeps a name from ever being read as a package-manager option.
var pkgNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9+._:-]{0,127}$`)

// ParsePackageNames splits a space/comma separated list and validates every name (at most 20).
func ParsePackageNames(raw string) ([]string, error) {
	names := strings.FieldsFunc(raw, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' || r == '\n' })
	if len(names) == 0 {
		return nil, fmt.Errorf("no package name given")
	}
	if len(names) > 20 {
		return nil, fmt.Errorf("too many packages (max 20)")
	}
	for _, n := range names {
		if !pkgNameRe.MatchString(n) {
			return nil, fmt.Errorf("invalid package name: %q", n)
		}
	}
	return names, nil
}

// BuildPackageCommand builds the wrapped command for acting on specific packages: "upgrade" (only upgrade the
// named packages, never install them), "install" or "remove". Same privilege fallback as BuildUpgradeCommand.
func BuildPackageCommand(manager, action string, pkgs []string) (string, error) {
	if len(pkgs) == 0 {
		return "", fmt.Errorf("no package name given")
	}
	quoted := make([]string, len(pkgs))
	for i, p := range pkgs {
		if !pkgNameRe.MatchString(p) {
			return "", fmt.Errorf("invalid package name: %q", p)
		}
		quoted[i] = shellQuote(p)
	}
	list := strings.Join(quoted, " ")

	var innerCmd string
	switch manager {
	case "apt":
		const apt = "DEBIAN_FRONTEND=noninteractive "
		switch action {
		case "upgrade":
			innerCmd = apt + "apt-get update && " + apt + "apt-get -y -o Dpkg::Options::=--force-confold install --only-upgrade " + list
		case "install":
			innerCmd = apt + "apt-get update && " + apt + "apt-get -y -o Dpkg::Options::=--force-confold install " + list
		case "remove":
			innerCmd = apt + "apt-get -y remove " + list
		}
	case "dnf", "yum":
		switch action {
		case "upgrade", "install":
			innerCmd = manager + " -y " + action + " " + list
		case "remove":
			innerCmd = manager + " -y remove " + list
		}
	case "apk":
		switch action {
		case "upgrade":
			innerCmd = "apk add --upgrade --no-interactive " + list
		case "install":
			innerCmd = "apk add --no-interactive " + list
		case "remove":
			innerCmd = "apk del --no-interactive " + list
		}
	case "pacman":
		switch action {
		case "upgrade", "install":
			innerCmd = "pacman -S --noconfirm " + list
		case "remove":
			innerCmd = "pacman -R --noconfirm " + list
		}
	default:
		return "", fmt.Errorf("unsupported package manager: %s", manager)
	}
	if innerCmd == "" {
		return "", fmt.Errorf("invalid package action %q (must be 'upgrade', 'install' or 'remove')", action)
	}
	return wrapPrivilegedCommand(innerCmd), nil
}

// wrapPrivilegedCommand runs innerCmd with a 900 s timeout. If the SSH user is not root it uses passwordless
// `sudo -n` (never a password) and fails with a clear error when that is unavailable. It prints a
// "[pantau] exit=N" marker at the end.
func wrapPrivilegedCommand(innerCmd string) string {
	script := fmt.Sprintf(`T=""
if command -v timeout >/dev/null 2>&1; then
  T="timeout 900"
fi
S=""
if [ "$(id -u)" != "0" ]; then
  if sudo -n true 2>/dev/null; then
    S="sudo -n"
  else
    echo "sudo without password required but not available" >&2
    echo "[pantau] exit=1"
    exit 1
  fi
fi
$T $S sh -c %s
rc=$?
echo "[pantau] exit=$rc"
exit $rc
`, shellQuote(innerCmd))

	return "sh -c " + shellQuote(script)
}

// upgradeExecResult carries one ExecTimeout outcome for the ctx-aware wrapper.
type upgradeExecResult struct {
	stdout string
	err    error
}

// GetUpgradePreview executes the discovery script on host and parses the preview.
// It returns ctx.Err() promptly when the caller cancels.
func (ins *Inspector) GetUpgradePreview(ctx context.Context, hostID int64) (*UpgradePreview, error) {
	host, err := ins.db.GetHost(hostID)
	if err != nil || host == nil {
		return nil, fmt.Errorf("host not found")
	}

	runner, err := ins.GetRunnerForHost(host)
	if err != nil {
		return nil, fmt.Errorf("ssh connect failed: %w", err)
	}
	defer runner.Close()

	// ponytail: exec has no ctx-aware variant; close the runner on cancel to unblock the goroutine.
	resCh := make(chan upgradeExecResult, 1)
	go func() {
		stdout, _, _, err := runner.ExecTimeout("sh -c "+shellQuote(UpgradePreviewDetectionScript), 60*time.Second)
		resCh <- upgradeExecResult{stdout: stdout, err: err}
	}()

	select {
	case res := <-resCh:
		if res.err != nil {
			return nil, fmt.Errorf("exec preview script failed: %w", res.err)
		}
		return ParseUpgradePreview(res.stdout), nil
	case <-ctx.Done():
		// Closing the client errors the in-flight exec (LiveSSHRunner); mocks finish later into the buffered chan.
		_ = runner.Close()
		return nil, ctx.Err()
	}
}

// ErrPackageUpdateCheckInProgress indicates another update check is already running on the host.
var ErrPackageUpdateCheckInProgress = fmt.Errorf("package update check already in progress for this host")

// CheckPackageUpdates runs the read-only package update detection on a host and stores the result.
// It is the Package Update Check: it never installs anything.
func (ins *Inspector) CheckPackageUpdates(ctx context.Context, hostID int64) (*store.PackageUpdateCheck, error) {
	if _, loaded := ins.packageUpdateInFlight.LoadOrStore(hostID, struct{}{}); loaded {
		return nil, ErrPackageUpdateCheckInProgress
	}
	defer ins.packageUpdateInFlight.Delete(hostID)

	preview, err := ins.GetUpgradePreview(ctx, hostID)
	if err != nil {
		return nil, err
	}

	check := &store.PackageUpdateCheck{
		HostID:            hostID,
		CheckedAt:         time.Now().UTC(),
		Supported:         preview.Supported,
		Manager:           preview.Manager,
		PendingTotal:      preview.PendingTotal,
		PendingSecurity:   preview.PendingSecurity,
		RebootRequired:    preview.RebootRequired,
		RepoCacheAgeHours: preview.RepoCacheAgeHours,
		Packages:          []store.PackageUpdate{},
		Note:              preview.Note,
	}
	for _, p := range preview.Packages {
		check.Packages = append(check.Packages, store.PackageUpdate{
			Name: p.Name, From: p.From, To: p.To, Security: p.Security,
		})
	}
	if !preview.OK {
		check.Error = preview.Note
		if check.Error == "" {
			check.Error = "the package manager did not report its pending updates"
		}
	}

	if err := ins.db.SavePackageUpdateCheck(check); err != nil {
		return nil, err
	}
	return check, nil
}
