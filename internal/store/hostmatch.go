package store

// Host-name matching for the agent's local recall. Operators type host names loosely — "macmini 4",
// "macmini-4", "MacMini_4" — while the stored name has one canonical spelling. Comparing raw strings
// makes a question miss the host it names, so all comparisons go through one normalizer.

import "strings"

// NormalizeHostName folds a host name (or a fragment of a question) to a comparable key: lowercase,
// with every run of separators (space, hyphen, underscore, dot) collapsed to a single hyphen. Digits
// are kept, so "macmini 4" and "macmini-4" become the same key "macmini-4".
func NormalizeHostName(s string) string {
	var b strings.Builder
	sep := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			if sep && b.Len() > 0 {
				b.WriteByte('-')
			}
			sep = false
			b.WriteRune(r)
		default:
			// Any other character (space, -, _, ., unicode punctuation) is a separator.
			sep = true
		}
	}
	return b.String()
}

// HostNameMatches reports whether a question mentions the given host name. It matches whole normalized
// segments, so "macmini 4" finds "macmini-4" but "macmini-40" does not match "macmini-4".
func HostNameMatches(question, name string) bool {
	n := NormalizeHostName(name)
	if n == "" {
		return false
	}
	nParts := strings.Split(n, "-")
	qParts := strings.Split(NormalizeHostName(question), "-")
	// Try every aligned window of the question whose normalized segments equal the name's segments.
	for start := 0; start+len(nParts) <= len(qParts); start++ {
		match := true
		for j, part := range nParts {
			if qParts[start+j] != part {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
