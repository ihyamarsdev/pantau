package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"pantau/internal/ansible"
)

func TestAnsibleOutlineComposeRoundTrip(t *testing.T) {
	env := newAnsibleEnv(t)
	play := "- name: p\n  hosts: all\n  roles:\n    - common\n  tasks:\n    - debug: msg=hi\n"
	rec := doRequest(t, env.srv, env.auth, http.MethodPost, "/api/ansible/playbooks/outline", map[string]string{"content": play})
	var o ansible.Outline
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &o) != nil || !o.Structured || len(o.Roles) != 1 {
		t.Fatalf("outline: %d %s", rec.Code, rec.Body.String())
	}
	o.Roles = append(o.Roles, ansible.OutlineRole{Name: "web", Params: "tags: [x]"})
	rec = doRequest(t, env.srv, env.auth, http.MethodPost, "/api/ansible/playbooks/compose", o)
	var c struct{ Content, Error string }
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &c) != nil || c.Content == "" {
		t.Fatalf("compose: %d %s", rec.Code, rec.Body.String())
	}
	if again := ansible.SplitPlaybook(c.Content); len(again.Roles) != 2 || again.Roles[1].Name != "web" {
		t.Fatalf("composed playbook lost the role: %s", c.Content)
	}

	o.Tasks = "not a list"
	if rec := doRequest(t, env.srv, env.auth, http.MethodPost, "/api/ansible/playbooks/compose", o); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid part must be 400: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doRequest(t, env.srv, env.auth, http.MethodGet, "/api/ansible/playbooks/outline", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET outline: %d", rec.Code)
	}
	env.srv.db.SetAnsibleEnabled(false)
	if rec := doRequest(t, env.srv, env.auth, http.MethodPost, "/api/ansible/playbooks/outline", map[string]string{"content": play}); rec.Code != http.StatusConflict {
		t.Fatalf("gate off: %d", rec.Code)
	}
}

func TestAnsibleRoleChoices(t *testing.T) {
	env := newAnsibleEnv(t)
	lib := t.TempDir()
	mk := func(p, body string) {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(lib, p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(lib, p), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("roles/ansistrano.deploy/meta/main.yml", "galaxy_info:\n  description: Deploy with  releases\n")
	mk("roles/ansistrano.deploy/meta/.galaxy_install_info", "version: 3.1.0\n")
	mk("collections/ansible_collections/acme/web/roles/nginx/tasks/main.yml", "- debug: msg=x\n")
	env.srv.SetAnsibleLibraryDir(lib)

	rec := doRequest(t, env.srv, env.auth, http.MethodGet, "/api/ansible/roles", nil)
	var out struct{ Roles []ansible.RoleChoice }
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
		t.Fatalf("roles: %d %s", rec.Code, rec.Body.String())
	}
	got := map[string]ansible.RoleChoice{}
	for _, r := range out.Roles {
		got[r.Name] = r
	}
	if r := got["ansistrano.deploy"]; r.Origin != "library" || r.Version != "3.1.0" || r.Description != "Deploy with releases" {
		t.Fatalf("library role: %+v", r)
	}
	if r := got["acme.web.nginx"]; r.Origin != "collection" || r.Collection != "acme.web" {
		t.Fatalf("collection role: %+v", r)
	}
}
