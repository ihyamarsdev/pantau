package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"pantau/internal/store"
)

func accountAuditServer(t *testing.T) (*Server, *http.Cookie, *store.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	srv := NewServer(db, nil, nil)
	token := "account_audit_test_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	return srv, &http.Cookie{Name: "pantau_session", Value: token}, db
}

func TestFleetKeysRequiresAuth(t *testing.T) {
	srv, _, _ := accountAuditServer(t)
	req := httptest.NewRequest("GET", "/api/security/keys", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", w.Code)
	}
}

func TestHostAccountAuditNotAudited(t *testing.T) {
	srv, cookie, db := accountAuditServer(t)
	hostID, err := db.CreateHost(&store.Host{Name: "srv1", Host: "10.0.0.1", Port: 22, User: "root"})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	req := httptest.NewRequest("GET", "/api/hosts/1/security/accounts", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["audited"] != false {
		t.Errorf("expected audited=false, got %v", out["audited"])
	}
	_ = hostID
}

func TestFleetKeysAggregatesHosts(t *testing.T) {
	srv, cookie, db := accountAuditServer(t)

	h1, err := db.CreateHost(&store.Host{Name: "web-1", Host: "10.0.0.1", Port: 22, User: "root"})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	h2, err := db.CreateHost(&store.Host{Name: "web-2", Host: "10.0.0.2", Port: 22, User: "root"})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	h3, err := db.CreateHost(&store.Host{Name: "db-1", Host: "10.0.0.3", Port: 22, User: "root"})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}

	fpShared := "SHA256:shared"
	fpWeak := "SHA256:weak"
	for _, a := range []*store.AccountAudit{
		{
			HostID: h1,
			Status: "ok",
			Users:  []store.AccountUser{{Name: "alice", UID: 1000}},
			Keys: []store.AccountKey{
				{Fingerprint: fpShared, Type: "ssh-ed25519", Valid: true, LibraryKey: "Ops Key", User: "alice"},
				{Fingerprint: fpWeak, Type: "ssh-rsa", Bits: 1024, Valid: true, Weak: true, Unknown: true, User: "alice"},
			},
		},
		{
			HostID: h2,
			Status: "ok",
			Users:  []store.AccountUser{{Name: "bob", UID: 1001}},
			Keys: []store.AccountKey{
				{Fingerprint: fpShared, Type: "ssh-ed25519", Valid: true, LibraryKey: "Ops Key", User: "bob"},
			},
		},
	} {
		a.ScannedAt = time.Now()
		if err := db.SaveAccountAudit(a); err != nil {
			t.Fatalf("save audit: %v", err)
		}
	}

	req := httptest.NewRequest("GET", "/api/security/keys", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var out struct {
		Keys []struct {
			Fingerprint string `json:"fingerprint"`
			Type        string `json:"type"`
			Bits        int    `json:"bits"`
			LibraryKey  string `json:"library_key"`
			Unknown     bool   `json:"unknown"`
			Weak        bool   `json:"weak"`
			Hosts       []struct {
				HostID int64  `json:"host_id"`
				Name   string `json:"name"`
				User   string `json:"user"`
			} `json:"hosts"`
		} `json:"keys"`
		Total           int `json:"total"`
		UnknownCount    int `json:"unknown_count"`
		WeakCount       int `json:"weak_count"`
		HostsNotAudited int `json:"hosts_not_audited"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Total != 2 {
		t.Fatalf("total = %d, want 2", out.Total)
	}
	if out.UnknownCount != 1 || out.WeakCount != 1 {
		t.Errorf("unknown/weak counts = %d/%d, want 1/1", out.UnknownCount, out.WeakCount)
	}
	if out.HostsNotAudited != 1 {
		t.Errorf("hosts not audited = %d, want 1 (db-1)", out.HostsNotAudited)
	}
	byFP := map[string]int{}
	for i, k := range out.Keys {
		byFP[k.Fingerprint] = i
	}
	shared, ok := byFP[fpShared]
	if !ok {
		t.Fatalf("shared key missing: %+v", out.Keys)
	}
	if len(out.Keys[shared].Hosts) != 2 {
		t.Errorf("shared key hosts = %d, want 2", len(out.Keys[shared].Hosts))
	}
	if out.Keys[shared].LibraryKey != "Ops Key" {
		t.Errorf("library key = %q, want Ops Key", out.Keys[shared].LibraryKey)
	}
	names := []string{out.Keys[shared].Hosts[0].Name, out.Keys[shared].Hosts[1].Name}
	if names[0] != "web-1" || names[1] != "web-2" {
		t.Errorf("shared key hosts = %v, want web-1 and web-2", names)
	}
	// The weak key sorts first.
	if out.Keys[0].Fingerprint != fpWeak {
		t.Errorf("first key = %q, want the weak key", out.Keys[0].Fingerprint)
	}

	// Filters.
	req = httptest.NewRequest("GET", "/api/security/keys?unknown=1", nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	out.Keys = nil
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Keys) != 1 || out.Keys[0].Fingerprint != fpWeak {
		t.Errorf("unknown filter returned %+v", out.Keys)
	}

	req = httptest.NewRequest("GET", "/api/security/keys?weak=1", nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	out.Keys = nil
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Keys) != 1 || out.Keys[0].Fingerprint != fpWeak {
		t.Errorf("weak filter returned %+v", out.Keys)
	}

	// The host audit endpoint returns the stored result.
	req = httptest.NewRequest("GET", "/api/hosts/1/security/accounts", nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	var hostOut map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &hostOut); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if hostOut["audited"] != true {
		t.Errorf("expected audited=true, got %v", hostOut["audited"])
	}
	_ = h3
}
