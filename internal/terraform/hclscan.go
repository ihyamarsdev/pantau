package terraform

import "strings"

// hclScanner is a minimal comment/string/heredoc-aware cursor over HCL text.
// shortcut: not a real HCL parser (no expression evaluation), enough to find
// top-level blocks and attribute text; upgrade to hclsyntax if more is needed.
type hclScanner struct {
	s string
	i int
}

func (c *hclScanner) eof() bool { return c.i >= len(c.s) }

func isHCLIdent(ch byte) bool {
	return ch == '_' || ch == '-' || ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z'
}

// skipComment consumes a comment at the cursor, reporting whether there was one.
func (c *hclScanner) skipComment() bool {
	rest := c.s[c.i:]
	switch {
	case rest[0] == '#', strings.HasPrefix(rest, "//"):
		if j := strings.IndexByte(rest, '\n'); j >= 0 {
			c.i += j
		} else {
			c.i = len(c.s)
		}
		return true
	case strings.HasPrefix(rest, "/*"):
		if j := strings.Index(rest[2:], "*/"); j >= 0 {
			c.i += j + 4
		} else {
			c.i = len(c.s)
		}
		return true
	}
	return false
}

// skipTrivia skips whitespace (newlines included) and comments.
func (c *hclScanner) skipTrivia() {
	for !c.eof() {
		switch c.s[c.i] {
		case ' ', '\t', '\r', '\n':
			c.i++
		default:
			if !c.skipComment() {
				return
			}
		}
	}
}

func (c *hclScanner) ident() string {
	start := c.i
	for !c.eof() && isHCLIdent(c.s[c.i]) {
		c.i++
	}
	return c.s[start:c.i]
}

// skipString consumes a quoted string at the cursor, including ${...} / %{...}
// templates. An unterminated string stops at end of line.
func (c *hclScanner) skipString() {
	c.i++
	for c.i < len(c.s) {
		ch := c.s[c.i]
		switch {
		case ch == '\\':
			c.i = min(c.i+2, len(c.s))
			continue
		case ch == '"':
			c.i++
			return
		case ch == '\n':
			return
		case (ch == '$' || ch == '%') && strings.HasPrefix(c.s[c.i+1:], string(ch)+"{"):
			c.i += 3 // $${ and %%{ are escapes for a literal
			continue
		case (ch == '$' || ch == '%') && strings.HasPrefix(c.s[c.i+1:], "{"):
			c.i += 2
			c.nest(1)
			continue
		}
		c.i++
	}
}

// nest consumes brackets until the depth returns to zero. Start at an opening
// bracket with depth 0, or just after a consumed opener with depth 1.
func (c *hclScanner) nest(depth int) bool {
	for c.i < len(c.s) {
		switch ch := c.s[c.i]; {
		case ch == '"':
			c.skipString()
			continue
		case ch == '#' || ch == '/':
			if c.skipComment() {
				continue
			}
		case ch == '<':
			if c.skipHeredoc() {
				continue
			}
		case ch == '{' || ch == '[' || ch == '(':
			depth++
		case ch == '}' || ch == ']' || ch == ')':
			depth--
			if depth <= 0 {
				c.i++
				return true
			}
		}
		c.i++
	}
	return false
}

// skipHeredoc consumes a <<ID / <<-ID heredoc through its terminator line
// (the newline after it is left in place). It reports whether one started here.
func (c *hclScanner) skipHeredoc() bool {
	if !strings.HasPrefix(c.s[c.i:], "<<") {
		return false
	}
	j := c.i + 2
	if j < len(c.s) && c.s[j] == '-' {
		j++
	}
	idStart := j
	for j < len(c.s) && isHCLIdent(c.s[j]) && c.s[j] != '-' {
		j++
	}
	id := c.s[idStart:j]
	nl := strings.IndexByte(c.s[j:], '\n')
	if id == "" || nl < 0 || strings.TrimSpace(c.s[j:j+nl]) != "" {
		return false
	}
	pos := j + nl + 1
	for pos <= len(c.s) {
		end := strings.IndexByte(c.s[pos:], '\n')
		line := c.s[pos:]
		if end >= 0 {
			line = c.s[pos : pos+end]
		}
		if strings.TrimSpace(line) == id {
			c.i = pos + len(line)
			return true
		}
		if end < 0 {
			break
		}
		pos += end + 1
	}
	c.i = len(c.s)
	return true
}
