package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pantau/internal/ansible"
	"pantau/internal/store"
)

type ansibleImportFile struct{ path, content string }

// ansibleFolderRequest builds a multipart request from raw form fields and `files` parts (library uploads).
func ansibleFolderRequest(t *testing.T, url string, fields map[string]string, files []ansibleImportFile) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range files {
		part, err := writer.CreateFormFile("files", file.path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(part, file.content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, url, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func (e *ansibleTestEnv) post(t *testing.T, url string, fields map[string]string, files []ansibleImportFile) *httptest.ResponseRecorder {
	t.Helper()
	req := ansibleFolderRequest(t, url, fields, files)
	e.auth(req)
	rec := httptest.NewRecorder()
	e.srv.mux.ServeHTTP(rec, req)
	return rec
}

type ansibleTestEnv struct {
	srv  *Server
	auth func(*http.Request)
}

func newAnsibleEnv(t *testing.T) *ansibleTestEnv {
	t.Helper()
	srv, auth := setupAnsibleTestServer(t)
	if err := srv.db.SetAnsibleEnabled(true); err != nil {
		t.Fatal(err)
	}
	return &ansibleTestEnv{srv, auth}
}

type importItem struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	ExtraVars   string `json:"extra_vars,omitempty"`
	Content     string `json:"content"`
}

func (e *ansibleTestEnv) importPlaybooks(t *testing.T, items ...importItem) *httptest.ResponseRecorder {
	t.Helper()
	return doRequest(t, e.srv, e.auth, http.MethodPost, "/api/ansible/playbooks/import", map[string]interface{}{"playbooks": items})
}

func decodePlaybooks(t *testing.T, rec *httptest.ResponseRecorder) []store.AnsiblePlaybook {
	t.Helper()
	var response struct{ Playbooks []store.AnsiblePlaybook }
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &response) != nil {
		t.Fatalf("import: %d: %s", rec.Code, rec.Body.String())
	}
	return response.Playbooks
}

func TestAnsibleImportCopiesPlaybooks(t *testing.T) {
	env := newAnsibleEnv(t)
	srv, auth := env.srv, env.auth
	deploy := "- hosts: all\n  roles: [web]\n"
	pbs := decodePlaybooks(t, env.importPlaybooks(t,
		importItem{Name: "Imported deploy", Description: "from a folder", ExtraVars: `{"app_port":8080}`, Content: deploy},
		importItem{Name: "Imported site", Content: "- import_playbook: deploy.yml\n"},
	))
	if len(pbs) != 2 || pbs[0].ID == 0 || pbs[0].ID == pbs[1].ID || pbs[0].Source != "inline" {
		t.Fatalf("import must create two inline playbooks: %+v", pbs)
	}
	got, err := srv.db.GetAnsiblePlaybook(pbs[0].ID)
	if err != nil || got.Content != deploy || got.ExtraVars != `{"app_port":8080}` || got.Description != "from a folder" || got.Source != "inline" {
		t.Fatalf("stored playbook: %+v %v", got, err)
	}

	// An imported playbook is edited like any other.
	edit := doRequest(t, srv, auth, http.MethodPut, fmt.Sprintf("/api/ansible/playbooks/%d", got.ID), map[string]string{
		"name": got.Name, "source": "inline", "content": "- hosts: web\n", "extra_vars": `{"app_port":9090}`,
	})
	if edit.Code != http.StatusOK {
		t.Fatalf("edit: %d %s", edit.Code, edit.Body.String())
	}
	if edited, _ := srv.db.GetAnsiblePlaybook(got.ID); edited.Content != "- hosts: web\n" || edited.ExtraVars != `{"app_port":9090}` {
		t.Fatalf("edit not stored: %+v", edited)
	}

	// A taken name aborts the whole import.
	dup := env.importPlaybooks(t, importItem{Name: "Fresh", Content: "x"}, importItem{Name: "imported DEPLOY", Content: "y"})
	if dup.Code != http.StatusConflict || !strings.Contains(dup.Body.String(), "imported DEPLOY") {
		t.Fatalf("duplicate import: %d %s", dup.Code, dup.Body.String())
	}
	if all, _ := srv.db.ListAnsiblePlaybooks(); len(all) != 2 {
		t.Fatalf("conflicting import left %d playbooks", len(all))
	}

	// A run gets the stored text plus the global ansible config and the library.
	if err := srv.db.SetSetting(ansibleConfigSetting, "[defaults]\nforks = 9\n"); err != nil {
		t.Fatal(err)
	}
	srv.SetAnsibleLibraryDir("/srv/library")
	launched := make(chan ansible.RunSpec, 1)
	srv.ansibleMgr = NewAnsibleRunManager(srv.db, func(ctx context.Context, spec ansible.RunSpec, log io.Writer) (int, error) {
		launched <- spec
		return 0, nil
	})
	run := doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/run", got.ID), map[string]string{"mode": "syntax"})
	if run.Code != http.StatusCreated && run.Code != http.StatusAccepted {
		t.Fatalf("launch: %d: %s", run.Code, run.Body.String())
	}
	select {
	case spec := <-launched:
		if spec.Playbook != "- hosts: web\n" || spec.Git != nil {
			t.Fatalf("run did not receive the edited text: %+v", spec)
		}
		if spec.Config != "[defaults]\nforks = 9\n" || spec.LibraryDir != "/srv/library" {
			t.Fatalf("run did not receive ansible config and library: %q %q", spec.Config, spec.LibraryDir)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("run was not launched")
	}
	deadline := time.Now().Add(3 * time.Second)
	for srv.ansibleMgr.HasActiveRunForPlaybook(got.ID) {
		if time.Now().After(deadline) {
			t.Fatal("run did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestAnsibleImportRejected(t *testing.T) {
	env := newAnsibleEnv(t)
	ok := importItem{Name: "Imported", Content: "- hosts: all\n"}
	many := make([]importItem, maxImportPlaybooks+1)
	for i := range many {
		many[i] = importItem{Name: fmt.Sprintf("p%d", i), Content: "x"}
	}
	for _, tc := range []struct {
		name  string
		items []importItem
	}{
		{"no-playbooks", nil},
		{"too-many-playbooks", many},
		{"missing-name", []importItem{{Content: "x"}}},
		{"long-name", []importItem{{Name: strings.Repeat("a", 65), Content: "x"}}},
		{"duplicate-name", []importItem{{Name: "Same", Content: "x"}, {Name: "same", Content: "y"}}},
		{"empty-content", []importItem{{Name: "Empty"}}},
		{"oversized-content", []importItem{{Name: "Big", Content: strings.Repeat("a", maxPlaybookContentSize+1)}}},
		{"invalid-vars", []importItem{{Name: "V", ExtraVars: "[]", Content: ok.Content}}},
		{"null-vars", []importItem{{Name: "V", ExtraVars: "null", Content: ok.Content}}},
		{"vars-not-json", []importItem{{Name: "V", ExtraVars: "{bad", Content: ok.Content}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if rec := env.importPlaybooks(t, tc.items...); rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
	if rec := doRequest(t, env.srv, env.auth, http.MethodPost, "/api/ansible/playbooks/import", "not an object"); rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed JSON: %d", rec.Code)
	}
	if list, err := env.srv.db.ListAnsiblePlaybooks(); err != nil || len(list) != 0 {
		t.Fatal("rejected imports must not create partial records")
	}
}

func TestAnsibleImportAuthorizationAndRemovedBundleSource(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	body := map[string]interface{}{"playbooks": []importItem{{Name: "Import", Content: "- hosts: all\n"}}}
	req := httptest.NewRequest(http.MethodPost, "/api/ansible/playbooks/import", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated: %d", rec.Code)
	}
	if rec := doRequest(t, srv, auth, http.MethodPost, "/api/ansible/playbooks/import", body); rec.Code != http.StatusConflict {
		t.Fatalf("gate disabled: %d", rec.Code)
	}
	srv.db.SetAnsibleEnabled(true)
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		if rec := doRequest(t, srv, auth, method, "/api/ansible/playbooks/import", nil); rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s: expected 405, got %d", method, rec.Code)
		}
	}
	// Folder imports are gone: no bundle source, and the old replace route does not exist.
	if rec := doRequest(t, srv, auth, http.MethodPost, "/api/ansible/playbooks", map[string]string{"name": "b", "source": "bundle", "content": "x"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bundle source must be rejected: %d", rec.Code)
	}
	pb := decodeOne(t, doRequest(t, srv, auth, http.MethodPost, "/api/ansible/playbooks", map[string]string{"name": "p", "source": "inline", "content": "- hosts: all\n"}))
	if rec := doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/import", pb.ID), nil); rec.Code != http.StatusNotFound {
		t.Fatalf("replace route must be gone: %d", rec.Code)
	}
	if rec := doRequest(t, srv, auth, http.MethodPut, fmt.Sprintf("/api/ansible/playbooks/%d", pb.ID), map[string]string{"name": "p", "source": "bundle", "content": "x"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("switching to bundle must be rejected: %d", rec.Code)
	}
}

func decodeOne(t *testing.T, rec *httptest.ResponseRecorder) store.AnsiblePlaybook {
	t.Helper()
	var out struct{ Playbook store.AnsiblePlaybook }
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
		t.Fatalf("create: %d: %s", rec.Code, rec.Body.String())
	}
	return out.Playbook
}

func TestAnsiblePlaybookAnalyze(t *testing.T) {
	env := newAnsibleEnv(t)
	type response struct {
		Vars  []ansible.Var `json:"vars"`
		Error string        `json:"error"`
	}
	analyze := func(content, vars string) (int, response) {
		rec := doRequest(t, env.srv, env.auth, http.MethodPost, "/api/ansible/playbooks/analyze", map[string]string{"content": content, "extra_vars": vars})
		var out response
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	play := "- hosts: all\n  vars:\n    app: demo\n  tasks:\n    - debug: msg=\"{{ app }} {{ owner }} {{ port | default(80) }}\"\n"
	code, out := analyze(play, `{"owner":"me"}`)
	status := map[string]string{}
	source := map[string]string{}
	for _, v := range out.Vars {
		status[v.Name], source[v.Name] = v.Status, v.Source
	}
	if code != http.StatusOK || status["app"] != "defined" || status["owner"] != "defined" || source["owner"] != "default extra vars" || status["port"] != "optional" {
		t.Fatalf("analysis of unsaved text: %d %+v", code, out)
	}
	if _, out := analyze(play, ""); func() string {
		for _, v := range out.Vars {
			if v.Name == "owner" {
				return v.Status
			}
		}
		return ""
	}() != "required" {
		t.Fatal("a variable with no value anywhere must be required")
	}
	if code, out := analyze("not: [valid", ""); code != http.StatusOK || len(out.Vars) != 0 {
		t.Fatalf("malformed YAML yields no variables, not an error: %d %+v", code, out)
	}
	for _, bad := range []struct{ content, vars string }{{play, "[]"}, {play, "{bad"}, {strings.Repeat("a", maxPlaybookContentSize+1), ""}} {
		if code, out := analyze(bad.content, bad.vars); code != http.StatusBadRequest || out.Error == "" {
			t.Errorf("invalid request accepted: %d %+v", code, out)
		}
	}
	if rec := doRequest(t, env.srv, env.auth, http.MethodGet, "/api/ansible/playbooks/analyze", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET analyze: %d", rec.Code)
	}
	env.srv.db.SetAnsibleEnabled(false)
	if code, _ := analyze(play, ""); code != http.StatusConflict {
		t.Errorf("gate off: %d", code)
	}
}

func TestAnsiblePlaybookContentsOnlyForGit(t *testing.T) {
	env := newAnsibleEnv(t)
	pb := decodeOne(t, doRequest(t, env.srv, env.auth, http.MethodPost, "/api/ansible/playbooks", map[string]string{"name": "Inline", "source": "inline", "content": "- hosts: all\n"}))
	if rec := doRequest(t, env.srv, env.auth, http.MethodGet, fmt.Sprintf("/api/ansible/playbooks/%d/contents", pb.ID), nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("inline contents: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doRequest(t, env.srv, env.auth, http.MethodGet, "/api/ansible/playbooks/9999/contents", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id: %d", rec.Code)
	}
}
