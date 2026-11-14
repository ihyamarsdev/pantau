package iacscan

import "strings"

// add appends a finding, keeping the cap. The cap is enforced per scan (opts.maxFindings).
func add(res *Result, f Finding) {
	if res == nil {
		return
	}
	if len(res.Findings) >= res.maxFindings() {
		res.Truncated = true
		return
	}
	res.Findings = append(res.Findings, f)
}

// maxFindings is the cap of the current scan; ScanDir sets it before scanning.
func (r *Result) maxFindings() int {
	if r.cap > 0 {
		return r.cap
	}
	return DefaultMaxFindings
}

// containsFold reports whether s contains sub, ignoring case.
func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
