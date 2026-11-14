package osv

import (
	"strconv"
	"strings"
)

// severityOf derives a severity label for a vulnerability.
//
// Order of preference: a database_specific.severity string (the distro's own rating), then a
// CVSS base score. Nothing found means "unknown" — Pantau never invents a severity.
func severityOf(d vulnDetail) string {
	if d.DatabaseSpecific != nil {
		if s, ok := d.DatabaseSpecific["severity"].(string); ok && s != "" {
			return normalizeSeverity(s)
		}
	}
	for _, s := range d.Severity {
		switch strings.ToUpper(s.Type) {
		case "CVSS_V3", "CVSS_V4":
			if lbl := cvssLabel(s.Score); lbl != "" {
				return lbl
			}
		}
	}
	for _, s := range d.Severity {
		switch strings.ToUpper(s.Type) {
		case "UBUNTU", "ALPINE", "DEBIAN", "SEVERITY":
			if lbl := normalizeSeverity(s.Score); lbl != "" {
				return lbl
			}
		}
	}
	return "unknown"
}

// cvssLabel maps a CVSS base score to a severity label. A CVSS vector carries no base score, so it
// yields no label and the next severity source is used instead.
func cvssLabel(score string) string {
	s := strings.TrimSpace(score)
	if s == "" || strings.HasPrefix(strings.ToUpper(s), "CVSS:") {
		return ""
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return ""
	}
	switch {
	case v >= 9.0:
		return "critical"
	case v >= 7.0:
		return "high"
	case v >= 4.0:
		return "medium"
	case v > 0:
		return "low"
	default:
		return "unknown"
	}
}

// normalizeSeverity maps the severity words used by the different feeds to Pantau's labels.
func normalizeSeverity(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical":
		return "critical"
	case "high", "important":
		return "high"
	case "medium", "moderate":
		return "medium"
	case "low", "negligible":
		return "low"
	default:
		return "unknown"
	}
}
