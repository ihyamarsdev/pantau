package inspector

// macOS softwareupdate output parsers for the agentless security scan.

import (
	"fmt"
	"strings"

	"pantau/internal/store"
)

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
	var raw strings.Builder

	flush := func() {
		if cur == nil {
			return
		}
		cur.rawText = raw.String()
		if cur.title == "" {
			cur.title = cur.label
		}
		entries = append(entries, *cur)
		cur = nil
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "* Label:") || (strings.HasPrefix(trimmed, "* ") && !strings.Contains(trimmed, "* Title:")) {
			flush()
			label := strings.TrimSpace(strings.TrimPrefix(trimmed, "* Label:"))
			if strings.HasPrefix(trimmed, "* ") && !strings.HasPrefix(trimmed, "* Label:") {
				label = strings.TrimSpace(strings.TrimPrefix(trimmed, "* "))
			}
			cur = &softwareUpdateEntry{label: label}
			raw.Reset()
			raw.WriteString(trimmed)
			continue
		}

		if cur != nil {
			raw.WriteByte(' ')
			raw.WriteString(trimmed)
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
	flush()
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
