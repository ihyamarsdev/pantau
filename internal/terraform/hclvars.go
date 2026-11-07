package terraform

import (
	"encoding/json"
	"sort"
	"strings"
)

// DeclaredVar is a `variable "name" {}` block found in the configuration.
type DeclaredVar struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Default     string `json:"default"` // raw HCL text, "" when none
	HasDefault  bool   `json:"has_default"`
	Sensitive   bool   `json:"sensitive"`
	File        string `json:"file"`
}

// ParseVariables lists the variables declared in the .tf files, sorted by file
// name then source order. Malformed blocks are skipped, never an error.
func ParseVariables(files map[string]string) []DeclaredVar {
	names := make([]string, 0, len(files))
	for n := range files {
		if strings.HasSuffix(n, ".tf") {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	out := []DeclaredVar{}
	for _, n := range names {
		out = append(out, parseFileVars(n, files[n])...)
	}
	return out
}

func parseFileVars(file, src string) []DeclaredVar {
	var out []DeclaredVar
	c := &hclScanner{s: src}
	for {
		c.skipTrivia()
		if c.eof() {
			return out
		}
		switch ch := c.s[c.i]; {
		case isHCLIdent(ch):
			if c.ident() != "variable" {
				continue
			}
			c.skipTrivia()
			name := c.label()
			c.skipTrivia()
			if name == "" || c.eof() || c.s[c.i] != '{' {
				continue
			}
			open := c.i
			if c.nest(0) {
				v := parseVarBody(c.s[open+1 : c.i-1])
				v.Name, v.File = name, file
				out = append(out, v)
			}
		case ch == '"':
			c.skipString()
		case ch == '{' || ch == '[' || ch == '(':
			c.nest(0)
		default:
			if !c.skipHeredoc() {
				c.i++
			}
		}
	}
}

// label reads a block label, quoted or bare; "" when malformed.
func (c *hclScanner) label() string {
	if c.eof() {
		return ""
	}
	if c.s[c.i] != '"' {
		return c.ident()
	}
	start := c.i
	c.skipString()
	if c.i-start < 3 || c.s[c.i-1] != '"' {
		return ""
	}
	return c.s[start+1 : c.i-1]
}

func parseVarBody(body string) DeclaredVar {
	var v DeclaredVar
	c := &hclScanner{s: body}
	for {
		c.skipTrivia()
		if c.eof() {
			return v
		}
		switch ch := c.s[c.i]; {
		case isHCLIdent(ch):
			key := c.ident()
			c.skipTrivia()
			if c.eof() || c.s[c.i] != '=' || strings.HasPrefix(c.s[c.i:], "==") {
				continue
			}
			c.i++
			raw := c.expr()
			switch key {
			case "type":
				v.Type = raw
			case "description":
				v.Description = hclString(raw)
			case "default":
				v.Default, v.HasDefault = raw, true
			case "sensitive":
				v.Sensitive = raw == "true"
			}
		case ch == '"':
			c.skipString()
		case ch == '{' || ch == '[' || ch == '(':
			c.nest(0)
		default:
			c.i++
		}
	}
}

// expr returns the raw text of an attribute value: up to the end of line at
// bracket depth 0, a trailing comment, or the end of a heredoc.
func (c *hclScanner) expr() string {
	for !c.eof() && (c.s[c.i] == ' ' || c.s[c.i] == '\t') {
		c.i++
	}
	start := c.i
	for !c.eof() {
		ch := c.s[c.i]
		if ch == '\n' || ch == '#' || strings.HasPrefix(c.s[c.i:], "//") {
			break
		}
		switch {
		case strings.HasPrefix(c.s[c.i:], "/*"):
			c.skipComment()
		case ch == '"':
			c.skipString()
		case ch == '{' || ch == '[' || ch == '(':
			c.nest(0)
		case c.skipHeredoc():
			return strings.TrimSpace(c.s[start:c.i])
		default:
			c.i++
		}
	}
	return strings.TrimSpace(c.s[start:c.i])
}

// hclString decodes a quoted or heredoc literal; other text is returned as is.
func hclString(raw string) string {
	switch {
	case strings.HasPrefix(raw, `"`):
		var s string
		if json.Unmarshal([]byte(raw), &s) == nil {
			return s
		}
		return strings.Trim(raw, `"`)
	case strings.HasPrefix(raw, "<<"):
		lines := strings.Split(raw, "\n")
		if len(lines) < 2 {
			return ""
		}
		lines = lines[1 : len(lines)-1]
		for i, l := range lines {
			lines[i] = strings.TrimSpace(l)
		}
		return strings.TrimSpace(strings.Join(lines, "\n"))
	}
	return raw
}
