package inspector

import (
	"context"
	"fmt"
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
	Note            string                  `json:"note,omitempty"`
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
elif command -v pacman >/dev/null 2>&1; then
    echo "MGR:pacman"
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
	rebootSec := extractSection(stdout, "===REBOOT===", "===UPDATES===")
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

	switch mgr {
	case "apt":
		return parseAptUpgradePreview(updatesSec, reboot)
	case "dnf", "yum":
		secSub := extractSection(updatesSec, "---SEC---", "---TOTAL---")
		totSub := extractSection(updatesSec, "---TOTAL---", "---RPM_QA---")
		rpmQaSub := extractSection(updatesSec, "---RPM_QA---", "")
		return parseRpmUpgradePreview(mgr, secSub, totSub, rpmQaSub, reboot)
	default:
		return &UpgradePreview{
			OK:        true,
			Supported: false,
			Manager:   mgr,
			Packages:  []UpgradePreviewPackage{},
			Note:      fmt.Sprintf("package manager %q is not supported for upgrade", mgr),
		}
	}
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
	default:
		return "", fmt.Errorf("unsupported package manager: %s", manager)
	}

	// Wrapper:
	// - timeout 900
	// - privilege fallback: if id -u != 0, prefix sudo -n if available, else error
	// - exit marker at the end
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

	return "sh -c " + shellQuote(script), nil
}

// GetUpgradePreview executes the discovery script on host and parses the preview.
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

	stdout, _, _, err := runner.ExecTimeout("sh -c "+shellQuote(UpgradePreviewDetectionScript), 60*time.Second)
	if err != nil {
		return nil, fmt.Errorf("exec preview script failed: %w", err)
	}

	return ParseUpgradePreview(stdout), nil
}
