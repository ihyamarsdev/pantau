package store

import "testing"

// Operators type host names loosely. The normalizer must make "macmini 4", "macmini-4" and
// "MacMini_4" the same key, while keeping digits and not merging distinct hosts.
func TestNormalizeHostName(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"macmini-4":   "macmini-4",
		"macmini 4":   "macmini-4",
		"MacMini_4":   "macmini-4",
		" macmini.4 ": "macmini-4",
		"web-01":      "web-01",
	}
	for in, want := range cases {
		if got := NormalizeHostName(in); got != want {
			t.Errorf("NormalizeHostName(%q) = %q, want %q", in, got, want)
		}
	}
}

// The exact bug: a question with a space must find the host stored with a hyphen.
func TestHostNameMatchesLooseSeparators(t *testing.T) {
	t.Parallel()
	cases := []struct {
		question string
		name     string
		want     bool
	}{
		{"adakah host macmini 4", "macmini-4", true},
		{"is there a host macmini-4", "macmini-4", true},
		{"status of MacMini_4 please", "macmini-4", true},
		{"check web-01", "web-01", true},
		{"check the web server", "web-01", false},
		{"macmini-40 is down", "macmini-4", false}, // must not match a longer host as a prefix
		{"", "macmini-4", false},
	}
	for _, c := range cases {
		if got := HostNameMatches(c.question, c.name); got != c.want {
			t.Errorf("HostNameMatches(%q, %q) = %v, want %v", c.question, c.name, got, c.want)
		}
	}
}
