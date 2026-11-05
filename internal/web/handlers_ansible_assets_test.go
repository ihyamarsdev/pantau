package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"pantau/internal/ansible"
	"pantau/internal/store"
)

func putAsset(t *testing.T, srv *Server, auth func(*http.Request), kind, name string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	u := "/api/ansible/assets?kind=" + url.QueryEscape(kind) + "&name=" + url.QueryEscape(name)
	req := httptest.NewRequest(http.MethodPut, u, bytes.NewReader(body))
	auth(req)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	return rec
}

func listAssets(t *testing.T, srv *Server, auth func(*http.Request), kind string) []store.AnsibleAsset {
	t.Helper()
	rec := doRequest(t, srv, auth, http.MethodGet, "/api/ansible/assets?kind="+kind, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Assets []store.AnsibleAsset `json:"assets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	return res.Assets
}

func TestAnsibleAssetsGateAndAuth(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	if rec := doRequest(t, srv, nil, http.MethodGet, "/api/ansible/assets", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated: %d", rec.Code)
	}
	if rec := doRequest(t, srv, auth, http.MethodGet, "/api/ansible/assets", nil); rec.Code != http.StatusConflict {
		t.Fatalf("gate disabled: %d", rec.Code)
	}
	if rec := putAsset(t, srv, auth, "file", "a", []byte("x")); rec.Code != http.StatusConflict {
		t.Fatalf("gate disabled PUT: %d", rec.Code)
	}
}

func TestAnsibleAssetsCRUD(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)

	if rec := putAsset(t, srv, auth, "template", "nginx/site.conf.j2", []byte("listen {{ port }};\n")); rec.Code != http.StatusOK {
		t.Fatalf("create template: %d %s", rec.Code, rec.Body.String())
	}
	if rec := putAsset(t, srv, auth, "file", "certs/ca.der", []byte{0xff, 0x00}); rec.Code != http.StatusOK {
		t.Fatalf("create file: %d %s", rec.Code, rec.Body.String())
	}
	// Same name again replaces the content instead of failing.
	if rec := putAsset(t, srv, auth, "template", "nginx/site.conf.j2", []byte("listen 8080;\n")); rec.Code != http.StatusOK {
		t.Fatalf("replace template: %d", rec.Code)
	}

	tpls := listAssets(t, srv, auth, "template")
	if len(tpls) != 1 || tpls[0].Name != "nginx/site.conf.j2" || tpls[0].Size != 13 {
		t.Fatalf("templates: %+v", tpls)
	}
	files := listAssets(t, srv, auth, "file")
	if len(files) != 1 || files[0].Size != 2 {
		t.Fatalf("files: %+v", files)
	}
	if rec := doRequest(t, srv, auth, http.MethodGet, "/api/ansible/assets?kind=bogus", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bogus kind: %d", rec.Code)
	}

	// A template's text can be read back for editing; an uploaded file never leaves the server.
	rec := doRequest(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/ansible/assets/%d", tpls[0].ID), nil)
	var got struct{ Name, Content string }
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK || got.Content != "listen 8080;\n" {
		t.Fatalf("get template: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doRequest(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/ansible/assets/%d", files[0].ID), nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("file download must be refused: %d", rec.Code)
	}

	if rec := doRequest(t, srv, auth, http.MethodDelete, fmt.Sprintf("/api/ansible/assets/%d", files[0].ID), nil); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d", rec.Code)
	}
	if rec := doRequest(t, srv, auth, http.MethodDelete, fmt.Sprintf("/api/ansible/assets/%d", files[0].ID), nil); rec.Code != http.StatusNotFound {
		t.Fatalf("delete again: %d", rec.Code)
	}
	if rec := doRequest(t, srv, auth, http.MethodGet, "/api/ansible/assets/abc", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("bad id: %d", rec.Code)
	}
}

func TestAnsibleAssetsValidation(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)

	for _, tc := range []struct {
		name       string
		kind, path string
		body       []byte
		want       int
	}{
		{"traversal", "file", "../etc/passwd", []byte("x"), http.StatusBadRequest},
		{"absolute", "file", "/etc/passwd", []byte("x"), http.StatusBadRequest},
		{"no name", "file", "", []byte("x"), http.StatusBadRequest},
		{"bad kind", "role", "a", []byte("x"), http.StatusBadRequest},
		{"binary template", "template", "a.j2", []byte{0xff, 0xfe}, http.StatusBadRequest},
		{"template too big", "template", "a.j2", bytes.Repeat([]byte("a"), ansible.MaxTemplateBytes+1), http.StatusBadRequest},
		{"file too big", "file", "a", bytes.Repeat([]byte("a"), ansible.MaxAssetFileBytes+1), http.StatusRequestEntityTooLarge},
	} {
		if rec := putAsset(t, srv, auth, tc.kind, tc.path, tc.body); rec.Code != tc.want {
			t.Errorf("%s: got %d want %d (%s)", tc.name, rec.Code, tc.want, rec.Body.String())
		}
	}
	if n := len(listAssets(t, srv, auth, "")); n != 0 {
		t.Fatalf("rejected uploads must not be stored, have %d", n)
	}
}

func TestAnsibleAssetsTotalLimit(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)

	chunk := bytes.Repeat([]byte("a"), ansible.MaxAssetFileBytes)
	for i := 0; i < 6; i++ { // 30 MiB
		if rec := putAsset(t, srv, auth, "file", fmt.Sprintf("f%d", i), chunk); rec.Code != http.StatusOK {
			t.Fatalf("upload %d: %d", i, rec.Code)
		}
	}
	rec := putAsset(t, srv, auth, "file", "f6", chunk)
	if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), "32 MiB") {
		t.Fatalf("total limit: %d %s", rec.Code, rec.Body.String())
	}
	// Replacing an existing file is measured against its old size, so it still fits.
	if rec := putAsset(t, srv, auth, "file", "f0", chunk); rec.Code != http.StatusOK {
		t.Fatalf("replace within limit: %d", rec.Code)
	}
}

func TestAnsibleRunReceivesAssets(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)
	hid, _ := srv.db.CreateHost(&store.Host{Name: "h1", Host: "10.0.0.1", Port: 22, User: "root", CustomKey: "KEY"})
	_ = srv.db.SaveAnsibleAsset("template", "a.j2", []byte("hello"))
	_ = srv.db.SaveAnsibleAsset("file", "dir/b.bin", []byte{1, 2})

	specs := make(chan ansible.RunSpec, 2)
	srv.ansibleRun = func(ctx context.Context, spec ansible.RunSpec, log io.Writer) (int, error) {
		specs <- spec
		return 0, nil
	}
	srv.ansibleMgr = NewAnsibleRunManager(srv.db, srv.ansibleRun)

	inline, _ := srv.db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: "inline", Content: "- hosts: all\n"})
	rec := doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/run", inline), map[string]interface{}{"host_ids": []int64{hid}})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("run: %d %s", rec.Code, rec.Body.String())
	}
	select {
	case spec := <-specs:
		if len(spec.Assets) != 2 {
			t.Fatalf("inline run must receive both assets, got %+v", spec.Assets)
		}
		byName := map[string]ansible.Asset{}
		for _, a := range spec.Assets {
			byName[a.Name] = a
		}
		if byName["a.j2"].Kind != ansible.AssetTemplate || string(byName["a.j2"].Data) != "hello" ||
			byName["dir/b.bin"].Kind != ansible.AssetFile || len(byName["dir/b.bin"].Data) != 2 {
			t.Fatalf("assets mangled: %+v", byName)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("run never started")
	}
}
