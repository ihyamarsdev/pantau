package ansible

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func varsByName(vars []Var) map[string]Var {
	m := map[string]Var{}
	for _, v := range vars {
		m[v.Name] = v
	}
	return m
}

func TestAnalyzeVars(t *testing.T) {
	files := []ProjectFile{
		{"deploy/site.yml", `
- hosts: web
  vars:
    app_name: demo
  roles:
    - role: web
      web_port: 8080
    - common
  tasks:
    - name: "Hello {{ app_name }} for {{ customer }}"
      debug: msg="{{ greeting | default('hi') }} {{ missing_opt is defined }}"
    - shell: uptime
      register: uptime_out
      when: uptime_out.rc == 0 and feature_flag
    - set_fact:
        computed: "{{ uptime_out.stdout }}"
    - debug: msg="{{ computed }} {{ item.name }} {{ ansible_hostname }} {{ lookup('env', 'HOME') }}"
      loop: "{{ users }}"
      loop_control: {loop_var: u}
    - include_tasks: extra.yml
    - debug: msg="{{ item }} {{ from_group }} {{ sibling_only }}"
      with_items:
        - literal-thing
- import_playbook: other.yml
`},
		{"deploy/extra.yml", "- debug: msg=\"{{ from_included }}\"\n"},
		{"deploy/other.yml", "- hosts: all\n  tasks:\n    - debug: msg=\"{{ in_other }}\"\n"},
		{"deploy/roles/web/defaults/main.yml", "web_root: /var/www\nweb_port: 80\n"},
		{"deploy/roles/web/tasks/main.yml", "- template: src=a.conf.j2 dest=/etc/a.conf\n- debug: msg=\"{{ web_root }} {{ web_port }}\"\n"},
		{"deploy/roles/web/templates/a.conf.j2", "{# {{ commented }} #}listen {{ web_port }};\n{% for s in servers %}{{ s.host | upper }} {{ tls_cert }}{% endfor %}\n{% if enable_ssl is defined %}x{% endif %}"},
		{"deploy/roles/common/vars/main.yml", "ntp_server: pool.ntp.org\n"},
		{"deploy/roles/common/tasks/main.yaml", "- debug: msg=\"{{ ntp_server }}\"\n"},
		{"deploy/roles/unused/defaults/main.yml", "never_listed: 1\n"},
		{"deploy/group_vars/all.yml", "customer: acme\nusers: [a]\n"},
		{"deploy/group_vars/web", "from_group: 1\n"},
		{"other/group_vars/all.yml", "sibling_only: 1\n"},
	}
	vars := varsByName(AnalyzeVars(files, "deploy/site.yml", `{"feature_flag": true}`))

	want := map[string]struct{ status, def, source string }{
		"app_name":      {"defined", "demo", "deploy/site.yml vars"},
		"customer":      {"defined", "acme", "deploy/group_vars/all.yml"},
		"from_group":    {"defined", "1", "deploy/group_vars/web"},
		"sibling_only":  {"required", "", ""}, // group_vars of another playbook directory do not apply
		"users":         {"defined", `["a"]`, "deploy/group_vars/all.yml"},
		"feature_flag":  {"defined", "true", "default extra vars"},
		"web_port":      {"defined", "8080", "role params"},
		"web_root":      {"defined", "/var/www", "role web defaults"},
		"ntp_server":    {"defined", "pool.ntp.org", "role common vars"},
		"greeting":      {"optional", "", ""},
		"missing_opt":   {"optional", "", ""},
		"enable_ssl":    {"optional", "", ""},
		"from_included": {"required", "", ""},
		"in_other":      {"required", "", ""},
		"servers":       {"required", "", ""},
		"tls_cert":      {"required", "", ""},
	}
	for name, w := range want {
		got, ok := vars[name]
		if !ok || got.Status != w.status || got.Default != w.def || got.Source != w.source {
			t.Errorf("%s: got %+v, want %+v", name, got, w)
		}
	}
	for _, name := range []string{"uptime_out", "computed", "u", "s", "item", "ansible_hostname", "lookup", "commented", "never_listed", "upper", "rc", "name", "host", "literal", "thing"} {
		if _, ok := vars[name]; ok {
			t.Errorf("%s must not be reported", name)
		}
	}
	if vars["web_port"].UsedIn[0] != "deploy/roles/web/tasks/main.yml" {
		t.Errorf("web_port used_in: %v", vars["web_port"].UsedIn)
	}
	if first := AnalyzeVars(files, "deploy/site.yml", "")[0]; first.Status != "required" {
		t.Errorf("required vars must sort first, got %+v", first)
	}
}

func TestAnalyzeVarsListsDefinedButUnused(t *testing.T) {
	files := []ProjectFile{{Path: "p.yml", Content: "- hosts: all\n  vars:\n    idle: 1\n  tasks:\n    - debug: msg=x\n"}}
	v, ok := varsByName(AnalyzeVars(files, "p.yml", ""))["idle"]
	if !ok || v.Status != "defined" || len(v.UsedIn) != 0 {
		t.Fatalf("an unused defined variable must be listed so it can be removed: %+v", v)
	}
}

func TestLoadGitProject(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	origin := t.TempDir()
	gitRun(t, origin, "init", "-q", "-b", "main")
	for name, body := range map[string]string{
		"pb/site.yml": "- hosts: all\n  roles: [web]\n", "roles/web/tasks/main.yml": "- debug: msg=\"{{ web_name }}\"\n", ".env": "SECRET=1",
	} {
		full := filepath.Join(origin, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, origin, "add", "-A")
	gitRun(t, origin, "commit", "-q", "-m", "init")

	p, err := loadGit(context.Background(), GitSource{URL: origin, Branch: "main", Path: "pb/site.yml"}, "file")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Commit) != 40 || len(p.Files) != 2 || p.Files[0].Path != "pb/site.yml" {
		t.Fatalf("unexpected project: commit=%q files=%v", p.Commit, p.Files)
	}
	if v := varsByName(AnalyzeVars(p.Files, p.Entry, "")); v["web_name"].Status != "required" {
		t.Fatalf("role under repo root not resolved from nested playbook: %+v", v)
	}
	if _, err := loadGit(context.Background(), GitSource{URL: origin, Branch: "nope", Path: "pb/site.yml"}, "file"); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("clone error should carry git's message, got %v", err)
	}
}
