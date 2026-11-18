package gwupdate

import (
	"regexp"
	"strings"
)

// The CLIs behind the Pantau gateways report their version in their own format:
//
//	ansible-playbook [core 2.22.0]
//	Terraform v1.15.9
//	OpenTofu v1.13.9
//
// ExtractVersion pulls the version out of such a line so it can be compared with the upstream
// release. It returns "" when the line holds no version that can be read.

var (
	// versionToken splits a version line into candidate tokens.
	versionToken = regexp.MustCompile(`[\w.+\-]+`)
	// versionInside finds a version inside a token that carries extra characters (e.g. "core2.22.0").
	versionInside = regexp.MustCompile(`v?[0-9]+(?:\.[0-9]+){0,2}(?:-[0-9A-Za-z][0-9A-Za-z.-]*)?`)
)

// ExtractVersion reads the version out of the first line of a CLI's version output. The whole line is
// searched, so "ansible-playbook [core 2.22.0]" yields "2.22.0" and "Terraform v1.15.9" yields "1.15.9".
func ExtractVersion(output string) string {
	line := output
	if i := strings.IndexAny(line, "\r\n"); i >= 0 {
		line = line[:i]
	}
	tokens := versionToken.FindAllString(line, -1)

	// A token that is a version on its own wins: "Terraform v1.15.9" is read straight off the word.
	for _, tok := range tokens {
		if v, ok := ParseVersion(tok); ok {
			return format(v, tok)
		}
	}

	// Otherwise look for a version inside a token, but only where it is not glued to a word
	// ("python3.11" holds no version, "core2.22.0" holds "2.22.0").
	for _, tok := range tokens {
		for _, m := range versionInside.FindAllStringIndex(tok, -1) {
			before, after := "", ""
			if m[0] > 0 {
				before = tok[m[0]-1 : m[0]]
			}
			if m[1] < len(tok) {
				after = tok[m[1] : m[1]+1]
			}
			if isWordByte(before) || isWordByte(after) || after == "." {
				continue
			}
			if v, ok := ParseVersion(tok[m[0]:m[1]]); ok {
				return format(v, tok[m[0]:m[1]])
			}
		}
	}
	return ""
}

// format returns the version as it should be shown: without the leading "v" the tokenizer may have
// kept, and without a pre-release tag that the comparison cannot order.
func format(v Version, raw string) string {
	if !strings.ContainsAny(raw, "-+") {
		return strings.TrimLeft(raw, "vV")
	}
	// A pre-release is only meaningful with its tag; rebuild it from the parsed core.
	s := ""
	for i, n := range v.core {
		if i > 0 {
			s += "."
		}
		s += itoa(n)
	}
	if v.pre != "" {
		s += "-" + v.pre
	}
	return s
}

// isWordByte reports whether b is a letter or a digit.
func isWordByte(b string) bool {
	if b == "" {
		return false
	}
	c := b[0]
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// itoa renders a small non-negative int.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
