package terraform

import (
	"fmt"
	"strconv"
	"strings"
)

// semver is a parsed version; segs is how many numeric segments were written
// (needed by the `~>` operator).
type semver struct {
	n    [3]int
	segs int
	pre  string
}

func parseSemver(s string) (semver, error) {
	var v semver
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i] // build metadata never affects precedence
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		s, v.pre = s[:i], s[i+1:]
		if v.pre == "" {
			return v, fmt.Errorf("malformed version")
		}
	}
	parts := strings.Split(s, ".")
	if len(parts) > 3 {
		return v, fmt.Errorf("malformed version")
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p[0] == '+' {
			return v, fmt.Errorf("malformed version")
		}
		v.n[i] = n
	}
	v.segs = len(parts)
	return v, nil
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// cmpPre orders prerelease strings per semver; a release outranks any prerelease.
func cmpPre(a, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return 1
	}
	if b == "" {
		return -1
	}
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		na, ea := strconv.Atoi(pa[i])
		nb, eb := strconv.Atoi(pb[i])
		switch {
		case ea == nil && eb == nil:
			if c := cmpInt(na, nb); c != 0 {
				return c
			}
		case ea == nil:
			return -1 // numeric < alphanumeric
		case eb == nil:
			return 1
		default:
			if c := strings.Compare(pa[i], pb[i]); c != 0 {
				return c
			}
		}
	}
	return cmpInt(len(pa), len(pb))
}

func (v semver) cmp(o semver) int {
	for i := range v.n {
		if c := cmpInt(v.n[i], o.n[i]); c != 0 {
			return c
		}
	}
	return cmpPre(v.pre, o.pre)
}

// CompareVersions returns -1/0/1. An unparsable version sorts below any valid one.
func CompareVersions(a, b string) int {
	va, ea := parseSemver(a)
	vb, eb := parseSemver(b)
	switch {
	case ea != nil && eb != nil:
		return strings.Compare(a, b)
	case ea != nil:
		return -1
	case eb != nil:
		return 1
	}
	return va.cmp(vb)
}

// ConstraintSatisfied reports whether version meets a comma-joined constraint
// (=, !=, >, >=, <, <=, ~>; a bare version means =). An empty constraint
// matches everything. Malformed input is an error.
// shortcut: prerelease versions are compared as plain semver, not excluded
// unless pinned exactly like terraform does.
func ConstraintSatisfied(constraint, version string) (bool, error) {
	v, err := parseSemver(version)
	if err != nil {
		return false, fmt.Errorf("version %q: %w", version, err)
	}
	for _, part := range strings.Split(constraint, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			if strings.TrimSpace(constraint) == "" {
				continue
			}
			return false, fmt.Errorf("empty constraint element")
		}
		op := "="
		for _, o := range []string{"~>", ">=", "<=", "!=", "=", ">", "<"} {
			if strings.HasPrefix(part, o) {
				op, part = o, strings.TrimSpace(part[len(o):])
				break
			}
		}
		c, err := parseSemver(part)
		if err != nil {
			return false, fmt.Errorf("constraint %q: %w", part, err)
		}
		d := v.cmp(c)
		ok := false
		switch op {
		case "=":
			ok = d == 0
		case "!=":
			ok = d != 0
		case ">":
			ok = d > 0
		case ">=":
			ok = d >= 0
		case "<":
			ok = d < 0
		case "<=":
			ok = d <= 0
		case "~>":
			// upper bound bumps the segment before the last one written
			up := c
			up.pre = ""
			k := max(c.segs-2, 0)
			up.n[k]++
			for i := k + 1; i < 3; i++ {
				up.n[i] = 0
			}
			ok = d >= 0 && v.cmp(up) < 0
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}
