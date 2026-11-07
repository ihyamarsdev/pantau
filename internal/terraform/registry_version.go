package terraform

import (
	"regexp"
	"strings"
)

var regSemverRe = regexp.MustCompile(`^v?(\d+)(?:\.(\d+))?(?:\.\d+)?(-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

// SuggestConstraint returns a pessimistic constraint for a version:
// ~> MAJOR.MINOR, exact "= v" for prereleases, "" for unparseable input.
func SuggestConstraint(version string) string {
	m := regSemverRe.FindStringSubmatch(strings.TrimSpace(version))
	if m == nil {
		return ""
	}
	if m[3] != "" {
		return "= " + strings.TrimPrefix(strings.TrimSpace(version), "v")
	}
	minor := m[2]
	if minor == "" {
		minor = "0"
	}
	return "~> " + m[1] + "." + minor
}
