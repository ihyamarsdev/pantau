package terraform

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	regIdentRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)
	regTypeRe  = regexp.MustCompile(`^[A-Za-z0-9_(),.:=?{}\[\]" -]+$`)
)

// balanced reports whether brackets close in order, so a type expression cannot end the variable block early.
func balanced(s string) bool {
	var stack []rune
	pairs := map[rune]rune{')': '(', ']': '[', '}': '{'}
	for _, r := range s {
		switch r {
		case '(', '[', '{':
			stack = append(stack, r)
		case ')', ']', '}':
			if len(stack) == 0 || stack[len(stack)-1] != pairs[r] {
				return false
			}
			stack = stack[:len(stack)-1]
		}
	}
	return len(stack) == 0
}

// hclStr quotes s as an HCL string literal with interpolation disabled.
func hclStr(s string) string {
	q := strconv.Quote(s)
	return strings.NewReplacer("${", "$${", "%{", "%%{").Replace(q)
}

func firstLine(s string, max int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if r := []rune(s); len(r) > max {
		s = string(r[:max]) + "..."
	}
	return s
}

func regPlaceholder(typ string) string {
	t := strings.TrimSpace(typ)
	switch {
	case t == "string":
		return `""`
	case t == "number":
		return "0"
	case t == "bool":
		return "false"
	case strings.HasPrefix(t, "list("), strings.HasPrefix(t, "set("), strings.HasPrefix(t, "tuple("):
		return "[]"
	}
	return "{}" // map, object, any, unknown
}

func regModIdent(s string) string {
	s = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, s)
	if s == "" || s[0] >= '0' && s[0] <= '9' || s[0] == '-' {
		s = "m" + s
	}
	return s
}

// ModuleSnippet renders a module block with its required inputs as placeholders; with declareVars it also
// renders a matching variable block per required input. version empty = info's latest.
func ModuleSnippet(info RegistryModuleDetail, version, moduleName string, declareVars bool) (string, string) {
	m := info.Module
	if version == "" {
		version = m.Version
	}
	if moduleName == "" {
		moduleName = m.Name
	}
	var req []RegistryInput
	for _, in := range info.Inputs {
		if in.Required && regIdentRe.MatchString(in.Name) {
			req = append(req, in)
		}
	}
	sort.Slice(req, func(i, j int) bool { return req[i].Name < req[j].Name })

	var b strings.Builder
	b.WriteString("module " + hclStr(regModIdent(moduleName)) + " {\n")
	b.WriteString("  source  = " + hclStr(m.Namespace+"/"+m.Name+"/"+m.Provider) + "\n")
	if c := SuggestConstraint(version); c != "" {
		b.WriteString("  version = " + hclStr(c) + "\n")
	}
	w := 0
	for _, in := range req {
		w = max(w, len(in.Name))
	}
	if len(req) > 0 {
		b.WriteString("\n")
	}
	for _, in := range req {
		b.WriteString("  " + in.Name + strings.Repeat(" ", w-len(in.Name)) + " = " + regPlaceholder(in.Type))
		if d := firstLine(in.Description, 100); d != "" {
			b.WriteString("  # " + d)
		}
		b.WriteString("\n")
	}
	b.WriteString("}\n")

	var vars strings.Builder
	if declareVars {
		for _, in := range req {
			vars.WriteString("variable " + hclStr(in.Name) + " {\n")
			if t := strings.Join(strings.Fields(in.Type), " "); t != "" && regTypeRe.MatchString(t) && balanced(t) { // one line: no block break-out
				vars.WriteString("  type        = " + t + "\n")
			}
			if d := firstLine(in.Description, 200); d != "" {
				vars.WriteString("  description = " + hclStr(d) + "\n")
			}
			vars.WriteString("}\n\n")
		}
	}
	if vars.Len() == 0 {
		return b.String(), ""
	}
	return b.String(), strings.TrimRight(vars.String(), "\n") + "\n"
}

// ProviderSnippet renders a terraform { required_providers } block; empty version omits the constraint.
func ProviderSnippet(ns, typ, version string) string {
	s := "terraform {\n  required_providers {\n    " + regModIdent(typ) + " = {\n      source  = " + hclStr(ns+"/"+typ) + "\n"
	if c := SuggestConstraint(version); c != "" {
		s += "      version = " + hclStr(c) + "\n"
	}
	return s + "    }\n  }\n}\n"
}
