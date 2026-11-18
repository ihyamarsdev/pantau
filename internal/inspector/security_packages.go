package inspector

// Package-update parsers for the agentless security scan: apt, dnf/yum and repo cache age.

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"pantau/internal/store"
)

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
	for _, a := range rpmArchSuffixes {
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

// attachRpmCVEs sets each finding's CVE IDs from `dnf updateinfo list --security --with-cve` (yum: `updateinfo
// list cves`), whose rows are "CVE-2026-8932 Moderate/Sec. curl-minimal-7.76.1-40.el9_8.9.x86_64", matched on
// the package name and fixed version of the finding.
func attachRpmCVEs(findings []store.SecurityFinding, cveOut string) {
	byPkg := map[string][]string{}
	for _, line := range strings.Split(cveOut, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || !strings.HasPrefix(f[0], "CVE-") {
			continue
		}
		name, ver := splitRpmNEVRA(f[len(f)-1])
		k := name + "\x00" + ver
		if !slices.Contains(byPkg[k], f[0]) {
			byPkg[k] = append(byPkg[k], f[0])
		}
	}
	for i := range findings {
		if ids := byPkg[findings[i].Package+"\x00"+findings[i].FixedVersion]; len(ids) > 0 {
			slices.Sort(ids)
			findings[i].CVEs = ids
		}
	}
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

// firstEpoch returns the first positive epoch in a section of the cache-age probe, or 0.
func firstEpoch(content, start, end string) int64 {
	for _, l := range strings.Split(extractSection(content, start, end), "\n") {
		if n, err := strconv.ParseInt(strings.TrimSpace(l), 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// parseRepoCacheAge computes cache age in hours and issues notes if stale (>168h).
func parseRepoCacheAge(content, mgr string) (ageHours int, notes []string) {
	if mgr == "softwareupdate" {
		return -1, nil
	}
	// The first bare number of the section is `date +%s`; the section then carries the cache stamp.
	nowEpoch := firstEpoch(content, "", "---APT---")

	var cacheEpoch int64
	switch mgr {
	case "apt":
		cacheEpoch = firstEpoch(content, "---APT---", "---DNF---")
	case "dnf", "yum":
		cacheEpoch = firstEpoch(content, "---DNF---", "---APK---")
	case "apk":
		cacheEpoch = firstEpoch(content, "---APK---", "---PACMAN---")
	case "pacman":
		cacheEpoch = firstEpoch(content, "---PACMAN---", "")
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
