package ansible

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildConfig(t *testing.T) {
	out, err := BuildConfig("# comment\n[defaults]\nforks = 12\ninterpreter_python: auto_silent\n\n[ssh_connection]\npipelining = True\n[privilege_escalation]\nbecome_method = sudo\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"forks = 12", "interpreter_python = auto_silent", "pipelining = True", "become_method = sudo",
		"host_key_checking = False", "ssh_args = -F /dev/null -o UserKnownHostsFile=/dev/null"} {
		if !strings.Contains(out, want) {
			t.Errorf("config lacks %q:\n%s", want, out)
		}
	}
	if base, _ := BuildConfig(""); !strings.Contains(base, "nocows = 1") || strings.Contains(base, "forks") {
		t.Errorf("empty user config must yield only the base config:\n%s", base)
	}
}

func TestParseConfigRejects(t *testing.T) {
	for text, want := range map[string]string{
		"forks = 3":                                             "outside",
		"[inventory]\nenable_plugins = x":                       "not allowed",
		"[defaults]\ninventory = /etc/hosts":                    "managed",
		"[ssh_connection]\nssh_args = -o X":                     "managed",
		"[defaults]\nhost_key_checking = True":                  "managed",
		"[defaults]\nroles_path = /tmp":                         "managed",
		"[defaults]\nforks = 1\nforks = 2":                      "duplicate",
		"[defaults]\nforks = 1\n  continued":                    "multi-line",
		"[defaults\nforks = 1":                                  "section header",
		"[defaults]\nBad-Key = 1":                               "invalid key",
		"[defaults]\njust a line":                               "key = value",
		"[defaults]\nx = " + strings.Repeat("a", MaxConfigSize): "exceeds",
	} {
		if _, err := ParseConfig(text); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%.40q: got %v, want error containing %q", text, err, want)
		}
	}
	if _, err := ParseConfig("[defaults]\nforks = 1\ntimeout = 20"); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
	if err := (RunSpec{Mode: "syntax", Playbook: "- hosts: all", Config: "[defaults]\ninventory = x"}).Validate(); err == nil {
		t.Error("RunSpec.Validate accepted a managed key")
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLibraryListAndRemove(t *testing.T) {
	dir := t.TempDir()
	if lib, err := ListLibrary(filepath.Join(dir, "missing")); err != nil || len(lib.Collections)+len(lib.Roles) != 0 {
		t.Fatalf("missing dir must be an empty library: %+v %v", lib, err)
	}
	col := filepath.Join(LibraryCollectionsDir(dir), "ansible_collections", "community", "general")
	write(t, filepath.Join(col, "MANIFEST.json"), `{"collection_info":{"version":"9.1.0"}}`)
	write(t, filepath.Join(LibraryCollectionsDir(dir), "ansible_collections", "community.general-9.1.0.info", "GALAXY.yml"), "x")
	write(t, filepath.Join(LibraryRolesDir(dir), "geerlingguy.nginx", "meta", ".galaxy_install_info"), "install_date: x\nversion: 3.2.0\n")
	write(t, filepath.Join(LibraryRolesDir(dir), "local_role", "tasks", "main.yml"), "[]")

	lib, err := ListLibrary(dir)
	if err != nil || len(lib.Collections) != 1 || lib.Collections[0] != (LibraryItem{"community.general", "9.1.0"}) ||
		len(lib.Roles) != 2 || lib.Roles[0] != (LibraryItem{"geerlingguy.nginx", "3.2.0"}) || lib.Roles[1].Version != "" {
		t.Fatalf("unexpected library: %+v %v", lib, err)
	}

	for _, bad := range [][2]string{{"collections", "../x"}, {"collections", "nodot"}, {"roles", "../../etc"}, {"roles", "a/b"}, {"other", "x"}} {
		if err := RemoveLibraryItem(dir, bad[0], bad[1]); err == nil {
			t.Errorf("RemoveLibraryItem(%v) accepted", bad)
		}
	}
	if err := RemoveLibraryItem(dir, "collections", "community.general"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(LibraryCollectionsDir(dir), "ansible_collections", "community")); !os.IsNotExist(err) {
		t.Error("empty namespace and .info directories must be removed with the collection")
	}
	if err := RemoveLibraryItem(dir, "roles", "local_role"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveLibraryItem(dir, "roles", "local_role"); !os.IsNotExist(err) {
		t.Errorf("removing twice: %v", err)
	}
}

func TestInstallLocalRole(t *testing.T) {
	dir, src := t.TempDir(), t.TempDir()
	write(t, filepath.Join(src, "tasks", "main.yml"), "- debug: msg=hi\n")
	write(t, filepath.Join(src, "files", "run.sh"), "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(src, "files", "run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := InstallLocalRole(dir, "../evil", src); err == nil {
		t.Error("path-like role name accepted")
	}
	if err := InstallLocalRole(dir, "web", t.TempDir()); err == nil {
		t.Error("folder without role layout accepted")
	}
	for range 2 { // second pass replaces the first
		if err := InstallLocalRole(dir, "web", src); err != nil {
			t.Fatal(err)
		}
	}
	st, err := os.Stat(filepath.Join(LibraryRolesDir(dir), "web", "files", "run.sh"))
	if err != nil || st.Mode().Perm()&0o100 == 0 {
		t.Fatalf("role not installed or exec bit lost: %v", err)
	}
	if entries, _ := os.ReadDir(LibraryRolesDir(dir)); len(entries) != 1 {
		t.Errorf("staging directory left behind: %v", entries)
	}
}

func TestParseRequirements(t *testing.T) {
	cols, roles, err := parseRequirements("collections:\n  - name: community.general\n    version: '>=9.0.0'\n  - ansible.posix\nroles:\n  - src: geerlingguy.nginx\n    version: 3.2.0\n  - name: local.role\n")
	if err != nil || len(cols) != 2 || cols[0] != (requirementItem{"community.general", ">=9.0.0"}) || len(roles) != 2 || roles[0].Name != "geerlingguy.nginx" {
		t.Fatalf("unexpected parse: %v %v %v", cols, roles, err)
	}
	if _, r, err := parseRequirements("- geerlingguy.nginx\n"); err != nil || len(r) != 1 {
		t.Errorf("bare role list: %v %v", r, err)
	}
	for _, bad := range []string{
		"roles:\n  - src: https://github.com/x/y\n", "roles:\n  - src: git+https://x/y.git\n    scm: git\n",
		"collections:\n  - name: not_a_collection\n", "collections:\n  - name: a.b\n    version: '--x'\n",
		"collections:\n  - name: a.b\n    source: https://evil\n", "other: [a]\n", "", "just a string",
	} {
		if _, _, err := parseRequirements(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestInstallGalaxyRejectsBadInput(t *testing.T) {
	for _, c := range []struct{ kind, name, version string }{
		{"collection", "--help", ""}, {"collection", "a.b", "--x"}, {"collection", "nodot", ""},
		{"role", "-x", ""}, {"role", "a b", ""}, {"other", "a.b", ""},
	} {
		if err := InstallGalaxy(t.Context(), t.TempDir(), c.kind, c.name, c.version, nil); err == nil {
			t.Errorf("accepted %+v", c)
		}
	}
}
