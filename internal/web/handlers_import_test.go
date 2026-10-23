package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/store"
)

type importTestEnv struct {
	t   *testing.T
	db  *store.DB
	srv *Server
}

func newImportTestEnv(t *testing.T) *importTestEnv {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	srv := NewServer(db, inspector.New(db, nil, nil), nil)
	srv.sessions.Store("import_token", time.Now().Add(time.Hour))
	return &importTestEnv{t: t, db: db, srv: srv}
}

func (e *importTestEnv) do(method, path, body string, authed bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if authed {
		req.AddCookie(&http.Cookie{Name: "pantau_session", Value: "import_token"})
	}
	w := httptest.NewRecorder()
	e.srv.ServeHTTP(w, req)
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder, v interface{}) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
}

type previewResp struct {
	Entries []struct {
		Alias     string `json:"alias"`
		Host      string `json:"host"`
		User      string `json:"user"`
		Port      int    `json:"port"`
		Duplicate bool   `json:"duplicate"`
		Reason    string `json:"reason"`
		SameAs    string `json:"same_as"`
	} `json:"entries"`
	Warnings []string `json:"warnings"`
}

func mustJSON(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestImportEndpointsRequireAuth(t *testing.T) {
	e := newImportTestEnv(t)
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/hosts/import/preview"},
		{"POST", "/api/hosts/import"},
		{"PUT", "/api/hosts/groups"},
	} {
		if w := e.do(c.method, c.path, `{}`, false); w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without session = %d, want 401", c.method, c.path, w.Code)
		}
	}
}

func TestImportPreviewFlagsDuplicates(t *testing.T) {
	e := newImportTestEnv(t)
	if _, err := e.db.CreateHost(&store.Host{Name: "Existing Box", Host: "10.0.0.1", Port: 22, User: "root"}); err != nil {
		t.Fatal(err)
	}

	cfg := "Host already\n  HostName 10.0.0.1\n  User root\n" + // same endpoint as Existing Box
		"Host fresh\n  HostName 10.0.0.2\n  User ihya\n  Port 65522\n" +
		"Host fresh-twin\n  HostName 10.0.0.2\n  User ihya\n  Port 65522\n" + // same endpoint as fresh
		"Host other-port\n  HostName 10.0.0.2\n  User ihya\n  Port 22\n" // different port: not a duplicate

	w := e.do("POST", "/api/hosts/import/preview", mustJSON(t, map[string]string{"content": cfg}), true)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var resp previewResp
	decodeBody(t, w, &resp)

	got := map[string]struct {
		dup    bool
		reason string
		sameAs string
	}{}
	for _, en := range resp.Entries {
		got[en.Alias] = struct {
			dup    bool
			reason string
			sameAs string
		}{en.Duplicate, en.Reason, en.SameAs}
	}
	if g := got["already"]; !g.dup || g.reason != "exists" || g.sameAs != "Existing Box" {
		t.Errorf("already = %+v, want duplicate of existing host", g)
	}
	if g := got["fresh"]; g.dup {
		t.Errorf("fresh must not be a duplicate: %+v", g)
	}
	if g := got["fresh-twin"]; !g.dup || g.reason != "duplicate_in_file" || g.sameAs != "fresh" {
		t.Errorf("fresh-twin = %+v, want duplicate of earlier file entry", g)
	}
	if g := got["other-port"]; g.dup {
		t.Errorf("a different port is a different endpoint: %+v", g)
	}
}

func TestImportPreviewRejectsEmptyContent(t *testing.T) {
	e := newImportTestEnv(t)
	if w := e.do("POST", "/api/hosts/import/preview", `{"content":"  \n"}`, true); w.Code != http.StatusBadRequest {
		t.Errorf("empty content = %d, want 400", w.Code)
	}
}

func TestImportPreviewReadsServerSSHConfig(t *testing.T) {
	e := newImportTestEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	w := e.do("POST", "/api/hosts/import/preview", `{"source":"server"}`, true)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing ~/.ssh/config = %d, want 404 (%s)", w.Code, w.Body.String())
	}

	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := "Host lab\n  HostName 194.233.67.83\n  User ihya\n  Port 65522\n"
	if err := os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	w = e.do("POST", "/api/hosts/import/preview", `{"source":"server"}`, true)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var resp previewResp
	decodeBody(t, w, &resp)
	if len(resp.Entries) != 1 || resp.Entries[0].Alias != "lab" || resp.Entries[0].Port != 65522 {
		t.Errorf("entries = %+v", resp.Entries)
	}
}

func TestImportCommitCreatesHostsWithDefaultsAndSkipsDuplicates(t *testing.T) {
	e := newImportTestEnv(t)
	if _, err := e.db.CreateHost(&store.Host{Name: "Existing", Host: "10.0.0.1", Port: 22, User: "root"}); err != nil {
		t.Fatal(err)
	}

	body := mustJSON(t, map[string]interface{}{"hosts": []map[string]interface{}{
		{"name": "new-a", "host": "10.0.0.5", "port": 65522, "user": "ihya"},
		{"name": "defaults", "host": "10.0.0.6"},                     // port 22, user root
		{"name": "dup-existing", "host": "10.0.0.1", "user": "root"}, // exists in DB
		{"name": "dup-batch", "host": "10.0.0.5", "port": 65522, "user": "ihya"},
		{"name": "bad-port", "host": "10.0.0.7", "port": 70000},
		{"name": "", "host": "10.0.0.8"},
	}})
	w := e.do("POST", "/api/hosts/import", body, true)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Created []store.Host `json:"created"`
		Skipped []struct {
			Name   string `json:"name"`
			Reason string `json:"reason"`
			SameAs string `json:"same_as"`
		} `json:"skipped"`
	}
	decodeBody(t, w, &resp)

	if len(resp.Created) != 2 {
		t.Fatalf("created %d hosts, want 2: %+v", len(resp.Created), resp.Created)
	}
	if resp.Created[0].ID == 0 || resp.Created[0].Name != "new-a" || resp.Created[0].GroupName != "" {
		t.Errorf("created[0] = %+v", resp.Created[0])
	}
	if d := resp.Created[1]; d.Port != 22 || d.User != "root" {
		t.Errorf("defaults host = port %d user %q, want 22/root", d.Port, d.User)
	}

	reasons := map[string]string{}
	for _, s := range resp.Skipped {
		reasons[s.Name] = s.Reason
	}
	want := map[string]string{
		"dup-existing": "exists",
		"dup-batch":    "exists", // collides with new-a created earlier in the same request
		"bad-port":     "invalid",
		"":             "invalid",
	}
	for name, reason := range want {
		if reasons[name] != reason {
			t.Errorf("skipped[%q] = %q, want %q (all: %v)", name, reasons[name], reason, reasons)
		}
	}

	hosts, err := e.db.ListHosts()
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 3 {
		t.Errorf("db has %d hosts, want 3 (1 existing + 2 imported)", len(hosts))
	}

	// Re-running the same import must be a no-op.
	w = e.do("POST", "/api/hosts/import", body, true)
	decodeBody(t, w, &resp)
	if len(resp.Created) != 0 {
		t.Errorf("second import created %d hosts, want 0", len(resp.Created))
	}
}

func TestHostGroupsBulkAssignAndRollback(t *testing.T) {
	e := newImportTestEnv(t)
	a, _ := e.db.CreateHost(&store.Host{Name: "a", Host: "10.0.0.1", Port: 22, User: "root", GroupName: "Old"})
	b, _ := e.db.CreateHost(&store.Host{Name: "b", Host: "10.0.0.2", Port: 22, User: "root"})

	ok := mustJSON(t, map[string]interface{}{"assignments": []map[string]interface{}{
		{"id": a, "group_name": "  Torcons Staging "},
		{"id": b, "group_name": "AICARE"},
	}})
	if w := e.do("PUT", "/api/hosts/groups", ok, true); w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	ha, _ := e.db.GetHost(a)
	hb, _ := e.db.GetHost(b)
	if ha.GroupName != "Torcons Staging" || hb.GroupName != "AICARE" {
		t.Errorf("groups = %q / %q", ha.GroupName, hb.GroupName)
	}

	// An unknown id must fail the whole request and leave earlier rows untouched.
	bad := mustJSON(t, map[string]interface{}{"assignments": []map[string]interface{}{
		{"id": a, "group_name": "Changed"},
		{"id": 9999, "group_name": "X"},
	}})
	if w := e.do("PUT", "/api/hosts/groups", bad, true); w.Code != http.StatusNotFound {
		t.Fatalf("unknown host = %d, want 404", w.Code)
	}
	ha, _ = e.db.GetHost(a)
	if ha.GroupName != "Torcons Staging" {
		t.Errorf("failed request must roll back, group = %q", ha.GroupName)
	}

	// Empty group name clears the group.
	clear := mustJSON(t, map[string]interface{}{"assignments": []map[string]interface{}{{"id": b, "group_name": ""}}})
	e.do("PUT", "/api/hosts/groups", clear, true)
	if hb, _ = e.db.GetHost(b); hb.GroupName != "" {
		t.Errorf("group should be cleared, got %q", hb.GroupName)
	}
}
