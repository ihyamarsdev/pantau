package ansible

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Var is a variable the playbook references. Status:
//
//	"required" - referenced, never defined in the project and not guarded by default()/is defined;
//	"optional" - referenced only behind default()/is defined;
//	"defined"  - has a value in the project (play vars, role defaults/vars, group_vars, vars_files, extra vars).
type Var struct {
	Name    string   `json:"name"`
	Status  string   `json:"status"`
	Default string   `json:"default,omitempty"`
	Source  string   `json:"source,omitempty"`
	Role    string   `json:"role,omitempty"` // the role the variable belongs to; empty = the playbook's own
	UsedIn  []string `json:"used_in"`
}

// AnalyzeVars statically reads the playbook, the roles it uses (project roles and roles installed in libDir or the
// system role paths), included task files and templates. It is a heuristic: no inventory is evaluated and Jinja is
// scanned, not rendered, so dynamically built names are missed.
func AnalyzeVars(files []ProjectFile, entry, extraVars, libDir string) []Var {
	a := &analyzer{
		files:    make(map[string]string, len(files)),
		entryDir: path.Dir(entry),
		defined:  map[string]definition{},
		local:    map[string]bool{},
		used:     map[string]*usage{},
		seen:     map[string]bool{},
		lib:      libDir,
		ext:      map[string]string{},
	}
	for _, f := range files {
		a.files[f.Path] = f.Content
		a.paths = append(a.paths, f.Path)
	}
	sort.Strings(a.paths)

	var extra map[string]any
	if json.Unmarshal([]byte(extraVars), &extra) == nil {
		for k, v := range extra {
			a.define(k, v, "default extra vars")
		}
	}
	a.playbook(entry)
	a.templatesIn(path.Join(a.entryDir, "templates"))
	for _, p := range a.paths {
		if a.isVarsFile(p) {
			a.defineFrom(p, p)
		}
	}
	return a.result()
}

type definition struct{ value, source string }

type usage struct {
	guarded bool // every reference sits behind default()/is defined
	files   map[string]bool
}

type analyzer struct {
	files    map[string]string
	paths    []string
	entryDir string
	defined  map[string]definition
	local    map[string]bool // register, set_fact, loop vars, {% set %}: produced at run time
	used     map[string]*usage
	seen     map[string]bool
	lib      string            // shared library directory, "" = none
	ext      map[string]string // role name -> virtual dir of roles loaded from outside the project
}

var (
	jinjaCommentRe = regexp.MustCompile(`(?s)\{#.*?#\}`)
	jinjaRe        = regexp.MustCompile(`(?s)\{\{(.*?)\}\}|\{%[-+]?(.*?)[-+]?%\}`)
	jinjaStringRe  = regexp.MustCompile(`'[^']*'|"[^"]*"`)
	identRe        = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)
	guardRe        = regexp.MustCompile(`^(?:(?:\.[A-Za-z_]\w*|\[[^\]]*\])*)\s*(?:\|\s*(?:default|d)\s*\(|is\s+(?:not\s+)?defined\b)`)
)

// Names Ansible/Jinja provide themselves; never user inputs.
var jinjaBuiltin = map[string]bool{
	"true": true, "false": true, "none": true, "True": true, "False": true, "None": true,
	"and": true, "or": true, "not": true, "in": true, "is": true, "if": true, "else": true, "defined": true,
	"undefined": true, "item": true, "loop": true, "omit": true, "hostvars": true, "groups": true,
	"group_names": true, "play_hosts": true, "playbook_dir": true, "inventory_dir": true, "inventory_file": true,
	"inventory_hostname": true, "inventory_hostname_short": true, "role_path": true, "role_name": true,
	"vars": true, "environment": true, "namespace": true,
}

// isVarsFile reports whether p is a group_vars/host_vars file Ansible loads for this run. Pantau's generated
// inventory sits in the project root and the playbook in entryDir, so only those two parents count; vars of
// sibling playbooks or uploaded inventories are not applied.
func (a *analyzer) isVarsFile(p string) bool {
	if ext := strings.ToLower(path.Ext(p)); ext != "" && ext != ".yml" && ext != ".yaml" && ext != ".json" {
		return false // Ansible also reads extensionless group_vars/all
	}
	segs := strings.Split(p, "/")
	for i, seg := range segs[:len(segs)-1] {
		if seg == "group_vars" || seg == "host_vars" {
			parent := path.Join(segs[:i]...) // "" at the project root
			return parent == "" || parent == a.entryDir
		}
	}
	return false
}

func render(v any) string {
	b, err := json.Marshal(v)
	s := string(b)
	if err != nil {
		s = fmt.Sprint(v)
	} else if str, ok := v.(string); ok {
		s = str
	}
	if r := []rune(s); len(r) > 120 {
		s = string(r[:120]) + "…"
	}
	return s
}

func (a *analyzer) define(name string, v any, source string) {
	if _, ok := a.defined[name]; !ok {
		a.defined[name] = definition{render(v), source}
	}
}

func (a *analyzer) parse(file string) (any, bool) {
	var doc any
	if err := yaml.Unmarshal([]byte(a.files[file]), &doc); err != nil {
		return nil, false // vault-encrypted or malformed files are skipped
	}
	return doc, true
}

// defineFrom reads a vars file: top-level keys become defined, string values are scanned for references.
func (a *analyzer) defineFrom(file, source string) {
	doc, ok := a.parse(file)
	m, isMap := doc.(map[string]any)
	if !ok || !isMap {
		return
	}
	for k, v := range m {
		a.define(k, v, source)
		a.strings(v, file)
	}
}

func (a *analyzer) strings(n any, file string) {
	switch v := n.(type) {
	case string:
		a.template(v, file)
	case []any:
		for _, x := range v {
			a.strings(x, file)
		}
	case map[string]any:
		for _, x := range v {
			a.strings(x, file)
		}
	}
}

func (a *analyzer) playbook(file string) {
	if a.seen["pb:"+file] {
		return
	}
	a.seen["pb:"+file] = true
	doc, ok := a.parse(file)
	if !ok {
		return
	}
	plays, _ := doc.([]any)
	for _, p := range plays {
		if m, ok := p.(map[string]any); ok {
			if imp, ok := m["import_playbook"].(string); ok && !strings.Contains(imp, "{{") {
				a.playbook(path.Join(path.Dir(file), imp))
				continue
			}
		}
		a.walk(p, file)
	}
}

func (a *analyzer) tasks(file string) {
	if a.seen["t:"+file] {
		return
	}
	a.seen["t:"+file] = true
	if doc, ok := a.parse(file); ok {
		a.walk(doc, file)
	}
}

func asString(v any) (string, bool) { s, ok := v.(string); return s, ok && !strings.Contains(s, "{{") }

// walk visits every YAML node: strings are scanned for references; keys with special meaning also define
// variables or pull in roles and files.
func (a *analyzer) walk(n any, file string) {
	switch v := n.(type) {
	case string:
		a.template(v, file)
	case []any:
		for _, x := range v {
			a.walk(x, file)
		}
	case map[string]any:
		for k, x := range v {
			switch {
			case k == "vars":
				if m, ok := x.(map[string]any); ok {
					for name, val := range m {
						a.define(name, val, file+" vars")
					}
				}
			case k == "register":
				if s, ok := x.(string); ok {
					a.local[s] = true
				}
			case k == "set_fact":
				if m, ok := x.(map[string]any); ok {
					for name := range m {
						a.local[name] = true
					}
				}
			case k == "loop_control":
				if m, ok := x.(map[string]any); ok {
					for _, key := range []string{"loop_var", "index_var"} {
						if s, ok := m[key].(string); ok {
							a.local[s] = true
						}
					}
				}
			case k == "vars_files":
				if l, ok := x.([]any); ok {
					for _, f := range l {
						if s, ok := asString(f); ok {
							a.includeVars(s, file)
						}
					}
				}
			case k == "include_vars":
				if s, ok := asString(x); ok {
					a.includeVars(s, file)
				} else if m, ok := x.(map[string]any); ok {
					if s, ok := asString(m["file"]); ok {
						a.includeVars(s, file)
					}
				}
			case k == "roles":
				if l, ok := x.([]any); ok {
					for _, r := range l {
						a.roleRef(r, file)
					}
				}
			case k == "include_role" || k == "import_role":
				a.roleRef(x, file)
			case k == "import_tasks" || k == "include_tasks":
				s, ok := asString(x)
				if m, isMap := x.(map[string]any); isMap {
					s, ok = asString(m["file"])
				}
				if ok {
					a.tasks(path.Join(path.Dir(file), s))
				}
			case k == "when" || k == "failed_when" || k == "changed_when" || k == "until" || k == "that":
				a.exprNode(x, file)
			case strings.HasPrefix(k, "with_"):
				// a string is an expression; a list holds literal items
				if s, ok := x.(string); ok {
					a.exprNode(s, file)
				}
			}
			a.walk(x, file)
		}
	}
}

// exprNode scans bare Jinja expressions (when: foo is defined) given as a string or a list of strings.
func (a *analyzer) exprNode(x any, file string) {
	switch v := x.(type) {
	case string:
		if !strings.Contains(v, "{{") {
			a.expr(v, file)
		}
	case []any:
		for _, e := range v {
			a.exprNode(e, file)
		}
	}
}

func (a *analyzer) includeVars(name, from string) {
	for _, p := range []string{path.Join(path.Dir(from), name), path.Join(a.entryDir, name), path.Join(path.Dir(from), "..", "vars", name)} {
		if _, ok := a.files[p]; ok {
			a.defineFrom(p, p)
			return
		}
	}
}

// reserved keys of a role entry that are not role parameters.
var roleEntryKeys = map[string]bool{
	"role": true, "name": true, "tags": true, "when": true, "vars": true, "become": true, "become_user": true,
	"become_method": true, "delegate_to": true, "environment": true, "any_errors_fatal": true, "ignore_errors": true,
	"no_log": true, "check_mode": true, "diff": true, "run_once": true, "throttle": true, "connection": true,
	"tasks_from": true, "vars_from": true, "defaults_from": true, "handlers_from": true, "public": true, "allow_duplicates": true,
}

func (a *analyzer) roleRef(n any, file string) {
	switch v := n.(type) {
	case string:
		a.role(v, file)
	case map[string]any:
		name, ok := asString(v["role"])
		if !ok {
			name, ok = asString(v["name"])
		}
		for k, val := range v {
			if !roleEntryKeys[k] {
				a.define(k, val, "role params")
			}
		}
		if ok {
			a.role(name, file)
		}
	}
}

func (a *analyzer) hasDir(d string) bool {
	i := sort.SearchStrings(a.paths, d+"/")
	return i < len(a.paths) && strings.HasPrefix(a.paths[i], d+"/")
}

// mainFiles returns <dir>/<kind>/main.y(a)ml and everything below <dir>/<kind>/main/.
func (a *analyzer) mainFiles(dir, kind string) []string {
	base := path.Join(dir, kind, "main")
	var out []string
	for _, p := range a.paths {
		ext := strings.ToLower(path.Ext(p))
		if p == base+".yml" || p == base+".yaml" || (strings.HasPrefix(p, base+"/") && (ext == ".yml" || ext == ".yaml")) {
			out = append(out, p)
		}
	}
	return out
}

func (a *analyzer) role(name, from string) {
	name = path.Clean(name)
	if name == "." || strings.HasPrefix(name, "..") || strings.HasPrefix(name, "/") || strings.Contains(name, "{{") {
		return
	}
	dir := ""
	for _, base := range []string{path.Join(path.Dir(from), "roles"), path.Join(a.entryDir, "roles"), "roles"} {
		if d := path.Join(base, name); a.hasDir(d) {
			dir = d
			break
		}
	}
	if dir == "" {
		dir = a.externalRole(name)
	}
	if dir == "" || a.seen["r:"+dir] {
		return
	}
	a.seen["r:"+dir] = true
	for _, kind := range []string{"defaults", "vars"} {
		for _, p := range a.mainFiles(dir, kind) {
			a.defineFrom(p, "role "+name+" "+kind)
		}
	}
	for _, kind := range []string{"tasks", "handlers"} {
		for _, p := range a.mainFiles(dir, kind) {
			a.tasks(p)
		}
	}
	a.roleProducers(dir)
	a.templatesIn(path.Join(dir, "templates"))
	for _, p := range a.mainFiles(dir, "meta") {
		if doc, ok := a.parse(p); ok {
			if m, ok := doc.(map[string]any); ok {
				if deps, ok := m["dependencies"].([]any); ok {
					for _, d := range deps {
						a.roleRef(d, p)
					}
				}
			}
		}
	}
}

func (a *analyzer) templatesIn(dir string) {
	for _, p := range a.paths {
		if !strings.HasPrefix(p, dir+"/") || a.seen["tpl:"+p] {
			continue
		}
		if ext := strings.ToLower(path.Ext(p)); ext == ".j2" || ext == ".jinja2" {
			a.seen["tpl:"+p] = true
			a.template(a.files[p], p)
		}
	}
}

// template scans {{ expr }} and {% stmt %} blocks of s.
func (a *analyzer) template(s, file string) {
	if !strings.Contains(s, "{") {
		return
	}
	s = jinjaCommentRe.ReplaceAllString(s, "")
	for _, m := range jinjaRe.FindAllStringSubmatch(s, -1) {
		if strings.HasPrefix(m[0], "{{") {
			a.expr(m[1], file)
			continue
		}
		stmt := strings.TrimSpace(m[2])
		word, rest, _ := strings.Cut(stmt, " ")
		switch word {
		case "for":
			names, iter, _ := strings.Cut(rest, " in ")
			for _, n := range identRe.FindAllString(names, -1) {
				a.local[n] = true
			}
			a.expr(iter, file)
		case "set":
			name, val, _ := strings.Cut(rest, "=")
			for _, n := range identRe.FindAllString(name, -1) {
				a.local[n] = true
			}
			a.expr(val, file)
		case "if", "elif":
			a.expr(rest, file)
		}
	}
}

// expr extracts the variable names of one Jinja expression.
func (a *analyzer) expr(s, file string) {
	s = jinjaStringRe.ReplaceAllString(s, `""`)
	var prev, prev2 string
	for _, loc := range identRe.FindAllStringIndex(s, -1) {
		start, end := loc[0], loc[1]
		word := s[start:end]
		before := strings.TrimRight(s[:start], " \t\r\n")
		after := strings.TrimLeft(s[end:], " \t\r\n")
		skip := jinjaBuiltin[word] || strings.HasPrefix(word, "ansible_") ||
			(start > 0 && s[start-1] >= '0' && s[start-1] <= '9') ||
			strings.HasSuffix(before, ".") || strings.HasSuffix(before, "|") || // attribute or filter name
			prev == "is" || (prev == "not" && prev2 == "is") || // test name: is match(..), is not none
			strings.HasPrefix(after, "(") || // function call: lookup(..), range(..)
			(strings.HasPrefix(after, "=") && !strings.HasPrefix(after, "==")) // keyword argument
		prev2, prev = prev, word
		if skip {
			continue
		}
		u := a.used[word]
		if u == nil {
			u = &usage{guarded: true, files: map[string]bool{}}
			a.used[word] = u
		}
		u.files[file] = true
		if !guardRe.MatchString(after) {
			u.guarded = false
		}
	}
}

func (a *analyzer) result() []Var {
	rank := map[string]int{"required": 0, "optional": 1, "defined": 2}
	out := make([]Var, 0, len(a.used))
	for name, u := range a.used {
		if a.local[name] {
			continue
		}
		v := Var{Name: name, UsedIn: make([]string, 0, len(u.files))}
		for f := range u.files {
			v.UsedIn = append(v.UsedIn, f)
		}
		sort.Strings(v.UsedIn)
		switch d, ok := a.defined[name]; {
		case ok:
			v.Status, v.Default, v.Source = "defined", d.value, d.source
		case u.guarded:
			v.Status = "optional"
		default:
			v.Status = "required"
		}
		v.Role = roleOf(v)
		out = append(out, v)
	}
	// Variables that are defined but never referenced are still listed, so they can be edited or removed.
	for name, d := range a.defined {
		if _, used := a.used[name]; !used && !a.local[name] {
			v := Var{Name: name, Status: "defined", Default: d.value, Source: d.source, UsedIn: []string{}}
			v.Role = roleOf(v)
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if ri, rj := rank[out[i].Status], rank[out[j].Status]; ri != rj {
			return ri < rj
		}
		return out[i].Name < out[j].Name
	})
	return out
}

var (
	roleSourceRe = regexp.MustCompile(`^role (\S+) (?:defaults|vars)$`)
	rolePathRe   = regexp.MustCompile(`(?:^|/)roles/([^/]+)/`)
)

// roleOf names the role a variable belongs to: the role that defines it, or else the single role whose files are
// the only ones that reference it. Anything the playbook itself defines or uses stays the playbook's own.
func roleOf(v Var) string {
	if m := roleSourceRe.FindStringSubmatch(v.Source); m != nil {
		return m[1]
	}
	if v.Source != "" || len(v.UsedIn) == 0 {
		return ""
	}
	role := ""
	for _, f := range v.UsedIn {
		m := rolePathRe.FindStringSubmatch(f)
		if m == nil || (role != "" && role != m[1]) {
			return ""
		}
		role = m[1]
	}
	return role
}
