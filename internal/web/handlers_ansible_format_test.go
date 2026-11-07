package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestAnsiblePlaybookFormatHandler(t *testing.T) {
	env := newAnsibleEnv(t)
	play := "- hosts: web\n  tasks:\n    - debug: msg=hi\n"
	rec := doRequest(t, env.srv, env.auth, http.MethodPost, "/api/ansible/playbooks/format", map[string]string{"content": play})
	var out struct {
		Content string `json:"content"`
		Changed bool   `json:"changed"`
		Error   string `json:"error"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out.Content == "" || out.Changed {
		t.Fatalf("format: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.HasSuffix(out.Content, "\n") || strings.HasPrefix(out.Content, "---") {
		t.Fatalf("unexpected shape:\n%s", out.Content)
	}

	four := "-   hosts: web\n    vars:\n        a: 1\n"
	rec = doRequest(t, env.srv, env.auth, http.MethodPost, "/api/ansible/playbooks/format", map[string]string{"content": four})
	if json.Unmarshal(rec.Body.Bytes(), &out) != nil || !out.Changed || !strings.Contains(out.Content, "  vars:") {
		t.Fatalf("re-indent: %d %s", rec.Code, rec.Body.String())
	}

	bad := map[string]string{
		"not a list":  "hosts: web",
		"broken yaml": "- hosts: web\n tasks: [x",
		"duplicate":   "- hosts: web\n  hosts: all",
		"no hosts":    "- name: p",
		"empty":       "",
		"oversized":   strings.Repeat("a", 256*1024+1),
	}
	for name, content := range bad {
		rec := doRequest(t, env.srv, env.auth, http.MethodPost, "/api/ansible/playbooks/format", map[string]string{"content": content})
		var e struct{ Error string }
		if rec.Code != http.StatusBadRequest || json.Unmarshal(rec.Body.Bytes(), &e) != nil || e.Error == "" {
			t.Errorf("%s: got %d %s, want 400 with error", name, rec.Code, rec.Body.String())
		}
	}
	if rec := doRequest(t, env.srv, env.auth, http.MethodPost, "/api/ansible/playbooks/format", map[string]string{"content": "- hosts: web\n tasks: [x"}); !strings.Contains(rec.Body.String(), "line") {
		t.Errorf("broken yaml error must mention a line: %s", rec.Body.String())
	}
	if rec := doRequest(t, env.srv, env.auth, http.MethodPost, "/api/ansible/playbooks/format", map[string]string{"content": "- hosts: web\n  hosts: all"}); !strings.Contains(rec.Body.String(), "line") {
		t.Errorf("duplicate key error must mention a line: %s", rec.Body.String())
	}

	if rec := doRequest(t, env.srv, env.auth, http.MethodGet, "/api/ansible/playbooks/format", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: %d", rec.Code)
	}
	env.srv.db.SetAnsibleEnabled(false)
	if rec := doRequest(t, env.srv, env.auth, http.MethodPost, "/api/ansible/playbooks/format", map[string]string{"content": play}); rec.Code != http.StatusConflict {
		t.Errorf("gate off: %d", rec.Code)
	}
}
