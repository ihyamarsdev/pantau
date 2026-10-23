// Package sshconfig extracts connectable hosts from an OpenSSH client
// configuration (~/.ssh/config). It implements only what Pantau needs to
// register a host: HostName, User and Port resolved with OpenSSH's
// "first obtained value wins" rule, so wildcard blocks such as "Host *"
// behave as defaults exactly like they do for the ssh client.
package sshconfig

import (
	"fmt"
	"path"
	"strconv"
	"strings"
)

// Entry is one concrete (non-wildcard) Host alias with its resolved options.
type Entry struct {
	Alias        string `json:"alias"`
	HostName     string `json:"host"`
	User         string `json:"user"`
	Port         int    `json:"port"`
	IdentityFile string `json:"identity_file,omitempty"`
}

// Result is the outcome of Parse.
type Result struct {
	Entries  []Entry
	Warnings []string
}

type block struct {
	patterns []string
	hostName string
	user     string
	port     string
	identity string
}

// Parse reads an OpenSSH client config and returns every concrete Host alias in
// file order. Wildcard patterns (*, ?, [..]) and negations (!pat) are never
// returned as entries; they only contribute defaults. Match blocks and Include
// directives are not evaluated and are reported through Warnings.
func Parse(content string) Result {
	var (
		res         Result
		blocks      []*block
		cur         *block
		matchSeen   bool
		includeSeen bool
	)

	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val := splitKeyValue(line)
		switch strings.ToLower(key) {
		case "host":
			cur = &block{patterns: splitFields(val)}
			blocks = append(blocks, cur)
		case "match":
			// Conditions are not evaluated: options below belong to no Host.
			cur = nil
			matchSeen = true
		case "include":
			includeSeen = true
		case "hostname":
			if cur != nil && cur.hostName == "" {
				cur.hostName = unquote(val)
			}
		case "user":
			if cur != nil && cur.user == "" {
				cur.user = unquote(val)
			}
		case "port":
			if cur != nil && cur.port == "" {
				cur.port = unquote(val)
			}
		case "identityfile":
			if cur != nil && cur.identity == "" {
				cur.identity = unquote(val)
			}
		}
	}

	if matchSeen {
		res.Warnings = append(res.Warnings, "Match blocks are not evaluated and were ignored")
	}
	if includeSeen {
		res.Warnings = append(res.Warnings, "Include directives are not followed; paste the included files too")
	}

	seen := map[string]bool{}
	for _, b := range blocks {
		for _, p := range b.patterns {
			if !isConcrete(p) || seen[p] {
				continue
			}
			seen[p] = true
			e, warn := resolve(p, blocks)
			res.Entries = append(res.Entries, e)
			res.Warnings = append(res.Warnings, warn...)
		}
	}
	return res
}

// resolve applies OpenSSH's first-value-wins rule for alias across all blocks
// whose Host patterns match it, in file order.
func resolve(alias string, blocks []*block) (Entry, []string) {
	var hostName, user, port, identity string
	for _, b := range blocks {
		if !matches(b.patterns, alias) {
			continue
		}
		if hostName == "" {
			hostName = b.hostName
		}
		if user == "" {
			user = b.user
		}
		if port == "" {
			port = b.port
		}
		if identity == "" {
			identity = b.identity
		}
	}

	var warns []string
	if hostName == "" {
		hostName = alias
	}
	hostName = strings.ReplaceAll(hostName, "%h", alias)

	p := 22
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			warns = append(warns, fmt.Sprintf("%s: invalid Port %q, using 22", alias, port))
		} else {
			p = n
		}
	}
	return Entry{Alias: alias, HostName: hostName, User: user, Port: p, IdentityFile: identity}, warns
}

// matches reports whether alias is selected by a Host line's patterns: it must
// match at least one positive pattern and no negated (!) pattern.
func matches(patterns []string, alias string) bool {
	positive := false
	for _, p := range patterns {
		if strings.HasPrefix(p, "!") {
			if ok, _ := path.Match(p[1:], alias); ok {
				return false
			}
			continue
		}
		if ok, _ := path.Match(p, alias); ok {
			positive = true
		}
	}
	return positive
}

func isConcrete(p string) bool {
	return p != "" && !strings.HasPrefix(p, "!") && !strings.ContainsAny(p, "*?[")
}

// splitKeyValue splits "Key value", "Key=value" and "Key = value".
func splitKeyValue(line string) (string, string) {
	i := strings.IndexAny(line, " \t=")
	if i < 0 {
		return line, ""
	}
	key := line[:i]
	rest := strings.TrimLeft(line[i:], " \t")
	rest = strings.TrimPrefix(rest, "=")
	return key, strings.TrimSpace(rest)
}

// splitFields splits on whitespace while keeping quoted tokens together.
func splitFields(s string) []string {
	var out []string
	var b strings.Builder
	quoted := false
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
		case (r == ' ' || r == '\t') && !quoted:
			flush()
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return out
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
