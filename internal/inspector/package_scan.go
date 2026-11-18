package inspector

// Package inventory: the batched remote command and the per package manager
// parsers that turn its output into host packages.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"pantau/internal/store"
)

const PackageScanBatchCmd = `if [ "$(uname -s 2>/dev/null)" = "Darwin" ]; then
    echo "TYPE:MACOS"
elif command -v dpkg-query >/dev/null 2>&1; then
    echo "TYPE:DPKG"
    echo "===PKGS==="
    dpkg-query -W -f='${Package}\t${Version}\t${Installed-Size}\t${db:Status-Status}\t${source:Package}\t${source:Version}\n' 2>/dev/null
    echo "===MANUAL==="
    apt-mark showmanual 2>/dev/null
elif command -v rpm >/dev/null 2>&1; then
    echo "TYPE:RPM"
    echo "===PKGS==="
    rpm -qa --qf '%{NAME}\t%{VERSION}-%{RELEASE}\t%{SIZE}\t%{INSTALLTIME}\n' 2>/dev/null
    echo "===MANUAL==="
    (dnf history userinstalled 2>/dev/null || (command -v yumdb >/dev/null 2>&1 && yumdb search reason user 2>/dev/null | awk '!/reason =/ && !/Loaded plugins/ && NF {sub(/\/reason:?$/, ""); print $1}') || awk '/Installed:/ {print $NF}' /var/log/yum.log* 2>/dev/null)
elif [ -f /lib/apk/db/installed ]; then
    echo "TYPE:APK"
    echo "===PKGS==="
    awk -F: '/^P:/ { if(p!="") print p "\t" v "\t" s "\t" o; p=$2; v=""; s=""; o="" } /^V:/ {v=$2} /^I:/ {s=$2} /^o:/ {o=$2} END { if(p!="") print p "\t" v "\t" s "\t" o }' /lib/apk/db/installed 2>/dev/null
    echo "===MANUAL==="
    cat /etc/apk/world 2>/dev/null
elif command -v pacman >/dev/null 2>&1; then
    echo "TYPE:PACMAN"
    echo "===PKGS==="
    pacman -Qi 2>/dev/null | awk '/^Name/ {n=$3} /^Version/ {v=$3} /^Installed Size/ {s=$4 " " $5; print n "\t" v "\t" s}'
    echo "===MANUAL==="
    pacman -Qqe 2>/dev/null
fi
echo "===SERVICES==="
if command -v systemctl >/dev/null 2>&1; then
    (systemctl list-units --type=service --all --no-legend --no-pager --plain 2>/dev/null || systemctl list-units --type=service --all --no-legend --no-pager 2>/dev/null) | awk '{ if ($1 ~ /^[^a-zA-Z0-9_]/) { print $2, $4, $5 } else { print $1, $3, $4 } }'
elif command -v chkconfig >/dev/null 2>&1; then
    chkconfig --list 2>/dev/null | awk '{print $1, ($5 ~ /3:on/ ? "active running" : "inactive")}'
fi
`

func parseServicesMap(out string) map[string]string {
	m := make(map[string]string)
	lines := strings.Split(out, "\n")
	for _, l := range lines {
		fields := strings.Fields(l)
		if len(fields) >= 2 {
			unit := fields[0]
			statusIdx := 1
			if (unit == "●" || unit == "*" || strings.HasPrefix(unit, "●")) && len(fields) >= 3 {
				unit = fields[1]
				statusIdx = 2
			}
			status := strings.Join(fields[statusIdx:], " ")
			m[unit] = status
			if strings.HasSuffix(unit, ".service") {
				m[strings.TrimSuffix(unit, ".service")] = status
			}
		}
	}
	return m
}

// normalizeRpmPackageName extracts the base package name from NEVRA or path strings.
// e.g. "nginx-1.18.0-1.el6.ngx.x86_64" -> "nginx", "openssh-server-5.3p1-124.el6_10.x86_64" -> "openssh-server"
func normalizeRpmPackageName(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	s = strings.TrimSuffix(s, "/reason")
	s = strings.TrimSuffix(s, "/reason:user")
	if idx := strings.LastIndex(s, "/"); idx != -1 {
		s = s[idx+1:]
	}
	if s == "reason" || strings.HasPrefix(s, "reason") {
		return ""
	}
	// Strip optional transaction hex hash prefix (e.g. 1a2b3c4d-nginx-...)
	if dashIdx := strings.Index(s, "-"); dashIdx > 0 && dashIdx <= 64 {
		isHex := true
		for _, ch := range s[:dashIdx] {
			if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')) {
				isHex = false
				break
			}
		}
		if isHex && len(s) > dashIdx+1 {
			s = s[dashIdx+1:]
		}
	}
	// Strip arch extension if present (.x86_64, .noarch, .i686, etc.)
	if dotIdx := strings.LastIndex(s, "."); dotIdx != -1 {
		lastDash := strings.LastIndex(s, "-")
		if dotIdx > lastDash {
			s = s[:dotIdx]
		}
	}
	// Now s is name-version-release or name
	parts := strings.Split(s, "-")
	if len(parts) >= 3 {
		// last part is release, second-to-last is version, rest is name
		return strings.Join(parts[:len(parts)-2], "-")
	} else if len(parts) == 2 {
		return parts[0]
	}
	return s
}

func parseManualMap(out string) map[string]bool {
	m := make(map[string]bool)
	lines := strings.Split(out, "\n")
	for _, l := range lines {
		pkg := strings.TrimSpace(l)
		if pkg != "" {
			m[pkg] = true
			if norm := normalizeRpmPackageName(pkg); norm != "" {
				m[norm] = true
			}
		}
	}
	return m
}

func parseDpkgOutput(out string, manualMap map[string]bool, servicesMap map[string]string, hostID int64) []*store.HostPackage {
	var list []*store.HostPackage
	lines := strings.Split(out, "\n")
	now := time.Now()
	for _, l := range lines {
		parts := strings.Split(strings.TrimSpace(l), "\t")
		if len(parts) < 4 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		version := strings.TrimSpace(parts[1])
		sizeKB, _ := strconv.ParseInt(strings.TrimSpace(parts[2]), 10, 64)
		status := strings.TrimSpace(parts[3])
		if status != "installed" {
			continue
		}
		// Columns 5/6 hold the source package name and version (OSV tracks source packages).
		source, sourceVersion := name, version
		if len(parts) > 4 && strings.TrimSpace(parts[4]) != "" {
			source = strings.TrimSpace(parts[4])
		}
		if len(parts) > 5 && strings.TrimSpace(parts[5]) != "" {
			sourceVersion = strings.TrimSpace(parts[5])
		}
		isManual := manualMap[name]
		svcStatus := ""
		svcName := ""
		if s, ok := servicesMap[name]; ok {
			svcName = name + ".service"
			svcStatus = s
		}
		list = append(list, &store.HostPackage{
			HostID:        hostID,
			Name:          name,
			Version:       version,
			Source:        source,
			SourceVersion: sourceVersion,
			SizeBytes:     sizeKB * 1024,
			IsManual:      isManual,
			ServiceName:   svcName,
			ServiceStatus: svcStatus,
			LastScannedAt: now,
		})
	}
	return list
}

func parsePackageSize(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if val, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return val
	}
	fields := strings.Fields(raw)
	if len(fields) >= 2 {
		num, _ := strconv.ParseFloat(fields[0], 64)
		unit := strings.ToUpper(fields[1])
		switch {
		case strings.HasPrefix(unit, "G"):
			return int64(num * 1024 * 1024 * 1024)
		case strings.HasPrefix(unit, "M"):
			return int64(num * 1024 * 1024)
		case strings.HasPrefix(unit, "K"):
			return int64(num * 1024)
		case strings.HasPrefix(unit, "B"):
			return int64(num)
		}
	}
	return 0
}

func parseRpmOutput(out string, manualMap map[string]bool, servicesMap map[string]string, hostID int64) []*store.HostPackage {
	var list []*store.HostPackage
	lines := strings.Split(out, "\n")
	now := time.Now()
	for _, l := range lines {
		parts := strings.Split(strings.TrimSpace(l), "\t")
		if len(parts) < 3 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		version := strings.TrimSpace(parts[1])
		sizeBytes := parsePackageSize(parts[2])
		var installEpoch int64
		if len(parts) >= 4 {
			installEpoch, _ = strconv.ParseInt(strings.TrimSpace(parts[3]), 10, 64)
		}
		isManual := manualMap[name] || manualMap[name+"-"+version] || manualMap[normalizeRpmPackageName(name)]
		svcStatus := ""
		svcName := ""
		if s, ok := servicesMap[name]; ok {
			svcName = name + ".service"
			svcStatus = s
		}
		list = append(list, &store.HostPackage{
			HostID:        hostID,
			Name:          name,
			Version:       version,
			SizeBytes:     sizeBytes,
			InstallEpoch:  installEpoch,
			IsManual:      isManual,
			ServiceName:   svcName,
			ServiceStatus: svcStatus,
			LastScannedAt: now,
		})
	}
	return list
}

// parseApkOutput parses the Alpine package inventory (name, version, size, origin package).
// The origin package is what OSV tracks for Alpine, so it is stored next to the binary package.
func parseApkOutput(out string, manualMap map[string]bool, servicesMap map[string]string, hostID int64) []*store.HostPackage {
	var list []*store.HostPackage
	lines := strings.Split(out, "\n")
	now := time.Now()
	for _, l := range lines {
		parts := strings.Split(strings.TrimSpace(l), "\t")
		if len(parts) < 3 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		version := strings.TrimSpace(parts[1])
		sizeBytes := parsePackageSize(parts[2])
		source := name
		if len(parts) > 3 && strings.TrimSpace(parts[3]) != "" {
			source = strings.TrimSpace(parts[3])
		}
		isManual := manualMap[name] || manualMap[source]
		svcStatus := ""
		svcName := ""
		if s, ok := servicesMap[name]; ok {
			svcName = name + ".service"
			svcStatus = s
		}
		list = append(list, &store.HostPackage{
			HostID:        hostID,
			Name:          name,
			Version:       version,
			Source:        source,
			SourceVersion: version,
			SizeBytes:     sizeBytes,
			IsManual:      isManual,
			ServiceName:   svcName,
			ServiceStatus: svcStatus,
			LastScannedAt: now,
		})
	}
	return list
}

// ScanPackages executes the agentless package inventory scanner on the host and stores the result.
func (ins *Inspector) ScanPackages(hostID int64) error {
	return ins.ScanPackagesWithTimeout(hostID, 45*time.Second)
}

// ScanPackagesWithTimeout executes the package inventory scan with a timeout budget and concurrency guard.
func (ins *Inspector) ScanPackagesWithTimeout(hostID int64, timeout time.Duration) error {
	if _, loaded := ins.packageInFlight.LoadOrStore(hostID, struct{}{}); loaded {
		// ponytail: Concurrency guard / singleflight prevents overlapping package scans.
		return ErrPackageScanInProgress
	}
	defer ins.packageInFlight.Delete(hostID)

	if timeout <= 0 {
		timeout = 45 * time.Second
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
		return err
	}
	defer runner.Close()

	// Ensure runner connection is immediately aborted if context timeout expires, unblocking hung commands.
	doneChan := make(chan struct{})
	defer close(doneChan)
	go func() {
		select {
		case <-doneChan:
		case <-ctx.Done():
			_ = runner.Close()
		}
	}()

	stdout, _, _, err := runner.Exec(PackageScanBatchCmd)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("exec package scan timed out: %w", ctx.Err())
		}
		return fmt.Errorf("exec package scan: %w", err)
	}

	pkgType := ""
	trimmedStdout := strings.TrimSpace(stdout)
	if strings.HasPrefix(trimmedStdout, "TYPE:") {
		lines := strings.SplitN(trimmedStdout, "\n", 2)
		pkgType = strings.TrimSpace(strings.TrimPrefix(lines[0], "TYPE:"))
		if len(lines) > 1 {
			trimmedStdout = lines[1]
		}
	}

	var pkgsSec, manualSec, servicesSec string
	if idx := strings.Index(trimmedStdout, "===PKGS==="); idx != -1 {
		rest := trimmedStdout[idx+len("===PKGS==="):]
		if mIdx := strings.Index(rest, "===MANUAL==="); mIdx != -1 {
			pkgsSec = strings.TrimSpace(rest[:mIdx])
			rest = rest[mIdx+len("===MANUAL==="):]
		}
		if sIdx := strings.Index(rest, "===SERVICES==="); sIdx != -1 {
			manualSec = strings.TrimSpace(rest[:sIdx])
			servicesSec = strings.TrimSpace(rest[sIdx+len("===SERVICES==="):])
		} else {
			manualSec = strings.TrimSpace(rest)
		}
	}

	manualMap := parseManualMap(manualSec)
	servicesMap := parseServicesMap(servicesSec)

	var pkgs []*store.HostPackage
	switch pkgType {
	case "MACOS":
		// macOS host does not have a supported system package inventory scanner.
		// Return cleanly without error so packages tab shows an empty inventory.
		return nil
	case "DPKG":
		pkgs = parseDpkgOutput(pkgsSec, manualMap, servicesMap, hostID)
	case "APK":
		pkgs = parseApkOutput(pkgsSec, manualMap, servicesMap, hostID)
	case "RPM", "PACMAN":
		pkgs = parseRpmOutput(pkgsSec, manualMap, servicesMap, hostID)
	default:
		if strings.Contains(pkgsSec, "\tinstalled") {
			pkgs = parseDpkgOutput(pkgsSec, manualMap, servicesMap, hostID)
		} else {
			pkgs = parseRpmOutput(pkgsSec, manualMap, servicesMap, hostID)
		}
	}

	if len(pkgs) == 0 {
		return fmt.Errorf("no packages discovered or package manager output was empty; keeping existing scan results")
	}

	return ins.db.ReplaceHostPackages(hostID, pkgs)
}
