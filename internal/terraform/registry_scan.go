package terraform

import (
	"regexp"
	"sort"
	"strings"
)

// RegistryRef is a module or provider requirement found in the configuration.
type RegistryRef struct {
	Kind       string `json:"kind"` // "module" | "provider"
	Name       string `json:"name"`
	Source     string `json:"source"`
	Constraint string `json:"constraint"`
	File       string `json:"file"`
	Line       int    `json:"line"` // the version literal's line, or the block/entry line when unpinned
	// Coord is "ns/name/provider" (module) or "ns/type" (provider) for sources
	// served by the registry; "" for git, local, http and foreign hosts.
	Coord string `json:"-"`
}

var scanSegRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// hclAttr is one entry of a block body: an attribute (raw value text) or a
// nested block (labels, body offsets). All offsets are absolute in the source.
type hclAttr struct {
	key      string
	labels   []string
	off      int // key offset
	raw      string
	valOff   int
	block    bool
	bodyFrom int
	bodyTo   int
}

// walkBody lists the top-level entries of src[from:to].
func walkBody(src string, from, to int) []hclAttr {
	var out []hclAttr
	c := &hclScanner{s: src[:to], i: from}
	for {
		c.skipTrivia()
		if c.eof() {
			return out
		}
		ch := c.s[c.i]
		if !isHCLIdent(ch) {
			switch {
			case ch == '"':
				c.skipString()
			case ch == '{' || ch == '[' || ch == '(':
				c.nest(0)
			default:
				c.i++
			}
			continue
		}
		a := hclAttr{off: c.i, key: c.ident()}
		c.skipBlanks()
		if !c.eof() && c.s[c.i] == '=' && !strings.HasPrefix(c.s[c.i:], "==") {
			c.i++
			c.skipBlanks()
			a.valOff = c.i
			c.value()
			a.raw = strings.TrimSpace(c.s[a.valOff:c.i])
			out = append(out, a)
			continue
		}
		for !c.eof() && (c.s[c.i] == '"' || isHCLIdent(c.s[c.i])) {
			a.labels = append(a.labels, c.label())
			c.skipBlanks()
		}
		if c.eof() || c.s[c.i] != '{' {
			continue
		}
		open := c.i
		if !c.nest(0) {
			return out
		}
		a.block, a.bodyFrom, a.bodyTo = true, open+1, c.i-1
		out = append(out, a)
	}
}

func (c *hclScanner) skipBlanks() {
	for !c.eof() && (c.s[c.i] == ' ' || c.s[c.i] == '\t') {
		c.i++
	}
}

// value consumes one attribute value: a string, a bracketed expression, or
// bare text up to a comma, closing brace, newline or comment.
func (c *hclScanner) value() {
	for !c.eof() {
		switch ch := c.s[c.i]; {
		case ch == '\n' || ch == ',' || ch == '}' || ch == '#' || strings.HasPrefix(c.s[c.i:], "//"):
			return
		case ch == '"':
			c.skipString()
		case ch == '{' || ch == '[' || ch == '(':
			c.nest(0)
		default:
			c.i++
		}
	}
}

// literal returns the decoded string when raw is a plain quoted literal.
func literal(raw string) (string, bool) {
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' || strings.Contains(raw, "${") {
		return "", false
	}
	return hclString(raw), true
}

func attrLiteral(attrs []hclAttr, key string) (a hclAttr, val string, found bool) {
	for _, x := range attrs {
		if x.key == key && !x.block {
			v, _ := literal(x.raw)
			return x, v, true
		}
	}
	return a, "", false
}

// moduleCoord maps a module source to its registry coordinates.
func moduleCoord(source, host string) string {
	if strings.Contains(source, "//") || strings.Contains(source, "::") {
		return ""
	}
	p := strings.Split(source, "/")
	if len(p) == 4 && p[0] == host {
		p = p[1:]
	}
	if len(p) != 3 || p[0] == "github.com" || p[0] == "bitbucket.org" {
		return ""
	}
	for _, s := range p {
		if !scanSegRe.MatchString(s) {
			return ""
		}
	}
	return strings.Join(p, "/")
}

// providerCoord maps a required_providers source to "ns/type".
func providerCoord(source, host string) string {
	p := strings.Split(source, "/")
	if len(p) == 3 && p[0] == host {
		p = p[1:]
	}
	if len(p) != 2 || !scanSegRe.MatchString(p[0]) || !scanSegRe.MatchString(p[1]) {
		return ""
	}
	return strings.Join(p, "/")
}

func lineOf(src string, off int) int { return 1 + strings.Count(src[:off], "\n") }

// ScanRegistryRefs lists module and required_providers requirements from the
// .tf files, ordered by file name then source order. host is the registry
// host whose 4-part module addresses (host/ns/name/provider) count as registry
// sources. Entries without a literal source or version are skipped.
func ScanRegistryRefs(files map[string]string, host string) []RegistryRef {
	names := make([]string, 0, len(files))
	for n := range files {
		if strings.HasSuffix(n, ".tf") {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	out := []RegistryRef{}
	for _, n := range names {
		src := files[n]
		for _, b := range walkBody(src, 0, len(src)) {
			if !b.block {
				continue
			}
			switch {
			case b.key == "module" && len(b.labels) == 1:
				if r, ok := scanModule(src, n, b, host); ok {
					out = append(out, r)
				}
			case b.key == "terraform":
				for _, rp := range walkBody(src, b.bodyFrom, b.bodyTo) {
					if rp.block && rp.key == "required_providers" {
						out = append(out, scanProviders(src, n, rp, host)...)
					}
				}
			}
		}
	}
	return out
}

func scanModule(src, file string, b hclAttr, host string) (RegistryRef, bool) {
	attrs := walkBody(src, b.bodyFrom, b.bodyTo)
	_, source, ok := attrLiteral(attrs, "source")
	if !ok || source == "" {
		return RegistryRef{}, false
	}
	r := RegistryRef{Kind: "module", Name: b.labels[0], Source: source, File: file, Line: lineOf(src, b.off), Coord: moduleCoord(source, host)}
	if va, v, found := attrLiteral(attrs, "version"); found {
		if _, lit := literal(va.raw); !lit {
			return RegistryRef{}, false
		}
		r.Constraint, r.Line = v, lineOf(src, va.valOff)
	}
	return r, true
}

// scanProviders reads a required_providers block: `name = { source, version }`
// or the legacy `name = "constraint"` (meaning hashicorp/<name>).
func scanProviders(src, file string, rp hclAttr, host string) []RegistryRef {
	var out []RegistryRef
	for _, e := range walkBody(src, rp.bodyFrom, rp.bodyTo) {
		if e.block {
			continue
		}
		r := RegistryRef{Kind: "provider", Name: e.key, Source: "hashicorp/" + e.key, File: file, Line: lineOf(src, e.off)}
		switch {
		case strings.HasPrefix(e.raw, "{"):
			attrs := walkBody(src, e.valOff+1, e.valOff+len(e.raw)-1)
			if _, s, ok := attrLiteral(attrs, "source"); ok && s != "" {
				r.Source = s
			}
			if va, v, found := attrLiteral(attrs, "version"); found {
				if _, lit := literal(va.raw); !lit {
					continue
				}
				r.Constraint, r.Line = v, lineOf(src, va.valOff)
			}
		default:
			v, lit := literal(e.raw)
			if !lit {
				continue
			}
			r.Constraint, r.Line = v, lineOf(src, e.valOff)
		}
		r.Coord = providerCoord(r.Source, host)
		out = append(out, r)
	}
	return out
}
