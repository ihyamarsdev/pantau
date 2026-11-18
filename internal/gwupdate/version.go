// Package gwupdate compares the CLI versions installed on the Pantau server with the latest
// release published upstream, for the CLIs behind the Ansible Gate and the Terraform Gate.
//
// Nothing here writes to disk or installs anything: it only reads release metadata over HTTP and
// compares versions. See ADR 0080.
package gwupdate

import (
	"strconv"
	"strings"
)

// Version is a parsed semantic version: a numeric core of up to three segments plus an optional
// pre-release tag. It mirrors the rules of internal/terraform's semver so a version written the
// same way compares the same way in both places.
type Version struct {
	core []int
	pre  string
}

// ParseVersion accepts "1.2.3", "v1.2", "1.0.0-rc1" and "1.0.0-rc1+build"; anything else is not a
// version. A missing core segment counts as zero, so "1.2" and "1.2.0" are the same version.
func ParseVersion(s string) (Version, bool) {
	var v Version
	s = strings.TrimSpace(s)
	s = strings.TrimLeft(s, "vV")
	if s == "" {
		return Version{}, false
	}
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i] // build metadata never affects precedence
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		s, v.pre = s[:i], s[i+1:]
		if v.pre == "" {
			return Version{}, false
		}
	}
	for _, part := range strings.Split(s, ".") {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return Version{}, false
		}
		v.core = append(v.core, n)
	}
	if len(v.core) == 0 || len(v.core) > 3 {
		return Version{}, false
	}
	return v, true
}

// Compare orders two versions: -1 when a is older than b, 0 when they are equal, 1 when a is newer.
// An unparsable version sorts below any parsable one.
func Compare(a, b string) int {
	va, okA := ParseVersion(a)
	vb, okB := ParseVersion(b)
	switch {
	case !okA && !okB:
		return 0
	case !okA:
		return -1
	case !okB:
		return 1
	}
	return va.cmp(vb)
}

// cmp orders two parsed versions.
func (v Version) cmp(o Version) int {
	for i := range 3 {
		x, y := v.segment(i), o.segment(i)
		if x != y {
			if x > y {
				return 1
			}
			return -1
		}
	}
	switch {
	case v.pre == o.pre:
		return 0
	case v.pre == "":
		return 1 // a release outranks any pre-release of the same core
	case o.pre == "":
		return -1
	}
	return strings.Compare(v.pre, o.pre)
}

// segment returns the i-th numeric segment, counting a missing one as zero.
func (v Version) segment(i int) int {
	if i < len(v.core) {
		return v.core[i]
	}
	return 0
}
