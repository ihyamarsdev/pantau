package osv

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// archIssue is one entry of the Arch Linux Security Tracker feed.
type archIssue struct {
	Name       string   `json:"name"`     // AVG-1234
	Packages   []string `json:"packages"` // affected package names
	Status     string   `json:"status"`   // Fixed | Vulnerable | Not affected | Unknown
	Severity   string   `json:"severity"` // Critical | High | Medium | Low | Unknown
	Type       string   `json:"type"`     // vulnerability type
	Affected   string   `json:"affected"` // affected version
	Fixed      *string  `json:"fixed"`    // fixed version, null when no fix exists
	Ticket     *string  `json:"ticket"`
	Issues     []string `json:"issues"` // CVE ids
	Advisories []string `json:"advisories"`
}

type archFeed struct {
	issues    []archIssue
	fetchedAt time.Time
}

// checkArch matches the installed packages against the Arch Linux Security Tracker.
//
// Matching rule (deliberately narrow and honest): an installed package is reported only when its
// version is exactly the version the tracker lists as affected, or when the tracker lists a fixed
// version and the installed version is lower. Pantau does not implement pacman's vercmp, so the
// "lower than fixed" comparison uses a plain numeric/lexicographic comparison of the version
// strings and is only used when both versions share the same format. Anything else is not claimed.
func (c *Client) checkArch(ctx context.Context, pkgs []Package) ([]Finding, []string, error) {
	feed, err := c.archFeed(ctx)
	if err != nil {
		return nil, nil, err
	}
	notes := []string{}
	byPkg := map[string][]archIssue{}
	for _, is := range feed.issues {
		for _, p := range is.Packages {
			byPkg[p] = append(byPkg[p], is)
		}
	}

	var findings []Finding
	for _, p := range pkgs {
		for _, is := range byPkg[p.Name] {
			if is.Status == "Not affected" {
				continue
			}
			fixed := ""
			if is.Fixed != nil {
				fixed = *is.Fixed
			}
			if !archVersionAffected(p.Version, is.Affected, fixed) {
				continue
			}
			f := Finding{
				Package:          p.Name,
				InstalledVersion: p.Version,
				FixedVersion:     fixed,
				IDs:              sortedUnique(append([]string{is.Name}, is.Issues...)),
				CVEs:             sortedUnique(filterCVEs(is.Issues)),
				Severity:         strings.ToLower(archSeverity(is.Severity)),
				Summary:          archSummary(is),
			}
			findings = append(findings, f)
		}
	}
	notes = append(notes, "Arch matching: only packages whose installed version equals the version listed as affected, or is below the listed fixed version, are reported")
	return findings, notes, nil
}

// archVersionAffected decides whether an installed version is covered by an issue.
//
//   - exact match on the affected version is always affected;
//   - otherwise the installed version is affected when a fixed version exists and the two versions
//     have the same shape (same number of dot/colon separated components) and the installed one
//     sorts lower. Pacman's own vercmp is not reimplemented here, so versions whose shape differs
//     are not claimed to be affected.
func archVersionAffected(installed, affected, fixed string) bool {
	if affected != "" && installed == affected {
		return true
	}
	if fixed == "" || installed == "" {
		return false
	}
	si, ok1 := compareArchVersions(installed, fixed)
	if !ok1 {
		return false
	}
	return si < 0
}

// compareArchVersions compares two pacman-style versions. It returns the comparison and whether
// the comparison is reliable (both versions have the same component structure).
func compareArchVersions(a, b string) (int, bool) {
	as := strings.Split(a, "-")
	bs := strings.Split(b, "-")
	if len(as) != len(bs) {
		return 0, false
	}
	for i := range as {
		c, ok := compareArchComponent(as[i], bs[i])
		if !ok {
			return 0, false
		}
		if c != 0 {
			return c, true
		}
	}
	return 0, true
}

// compareArchComponent compares one dot-separated component of a pacman version. Segments are
// compared numerically when both are numeric, otherwise alphanumerically.
func compareArchComponent(a, b string) (int, bool) {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	if len(as) != len(bs) {
		return 0, false
	}
	for i := range as {
		an, aNum := allDigits(as[i])
		bn, bNum := allDigits(bs[i])
		switch {
		case aNum && bNum:
			if an != bn {
				if an < bn {
					return -1, true
				}
				return 1, true
			}
		case aNum != bNum:
			// a numeric segment is newer than an alphanumeric one (pacman's rule)
			if aNum {
				return 1, true
			}
			return -1, true
		default:
			if as[i] != bs[i] {
				if as[i] < bs[i] {
					return -1, true
				}
				return 1, true
			}
		}
	}
	return 0, true
}

func allDigits(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

func filterCVEs(in []string) []string {
	var out []string
	for _, s := range in {
		if cveRe.MatchString(s) {
			out = append(out, s)
		}
	}
	return out
}

func archSeverity(s string) string {
	switch strings.ToLower(s) {
	case "critical":
		return "critical"
	case "high":
		return "high"
	case "medium":
		return "medium"
	case "low":
		return "low"
	default:
		return "unknown"
	}
}

func archSummary(is archIssue) string {
	s := fmt.Sprintf("%s (%s)", is.Name, is.Type)
	if is.Status != "" && is.Status != "Unknown" {
		s += " - " + is.Status
	}
	return s
}

// archFeed downloads (and caches for a day) the Arch Linux Security Tracker feed.
func (c *Client) archFeed(ctx context.Context) (*archFeed, error) {
	c.mu.Lock()
	if c.arch != nil && time.Since(c.arch.fetchedAt) < cacheTTL {
		f := c.arch
		c.mu.Unlock()
		return f, nil
	}
	c.mu.Unlock()

	var issues []archIssue
	if err := c.getJSON(ctx, c.ArchURL, archCap, &issues); err != nil {
		return nil, fmt.Errorf("Arch security tracker download failed: %w", err)
	}
	f := &archFeed{issues: issues, fetchedAt: time.Now()}
	c.mu.Lock()
	c.arch = f
	c.mu.Unlock()
	return f, nil
}
