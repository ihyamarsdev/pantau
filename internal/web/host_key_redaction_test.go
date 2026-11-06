package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/store"
)

const redactionSecret = "-----BEGIN OPENSSH PRIVATE KEY-----SECRETBODY"

func TestHostAPINeverReturnsCustomKey(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	srv := NewServer(db, inspector.New(db, nil, nil), nil)
	token := "redaction_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))

	do := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.AddCookie(&http.Cookie{Name: "pantau_session", Value: token})
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec
	}

	id, err := db.CreateHost(&store.Host{Name: "h", Host: "10.0.0.1", Port: 22, User: "root", CustomKey: redactionSecret})
	if err != nil {
		t.Fatal(err)
	}
	idPath := "/api/hosts/" + strconv.FormatInt(id, 10)

	for _, p := range []string{"/api/hosts", idPath} {
		rec := do(http.MethodGet, p, "")
		if strings.Contains(rec.Body.String(), "SECRETBODY") {
			t.Fatalf("GET %s leaked the custom key", p)
		}
		if !strings.Contains(rec.Body.String(), `"has_custom_key":true`) {
			t.Fatalf("GET %s: expected has_custom_key true, body %s", p, rec.Body.String())
		}
	}

	// keep_custom_key with a blank key must leave the stored key untouched.
	rec := do(http.MethodPut, idPath, `{"name":"renamed","host":"10.0.0.1","port":22,"user":"root","key_id":0,"custom_key":"","keep_custom_key":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT keep: %d %s", rec.Code, rec.Body.String())
	}
	if h, _ := db.GetHost(id); h.CustomKey != redactionSecret || h.Name != "renamed" {
		t.Fatalf("expected key kept and name updated, got name=%q key=%q", h.Name, h.CustomKey)
	}

	// Without the flag a blank key clears it (user switched to the global key).
	do(http.MethodPut, idPath, `{"name":"renamed","host":"10.0.0.1","port":22,"user":"root","key_id":0,"custom_key":""}`)
	if h, _ := db.GetHost(id); h.CustomKey != "" {
		t.Fatalf("expected key cleared, got %q", h.CustomKey)
	}
}
