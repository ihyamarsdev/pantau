package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"pantau/internal/store"
)

func securityOverviewServer(t *testing.T) (*Server, *http.Cookie, *store.DB) {
	t.Helper()
	db, err := store.Open(testDBCopy(t))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	srv := NewServer(db, nil, nil)
	token := "security_overview_test_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	return srv, &http.Cookie{Name: "pantau_session", Value: token}, db
}

func TestSecurityOverviewRequiresAuth(t *testing.T) {
	t.Parallel()
	srv, _, _ := securityOverviewServer(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest("GET", "/api/security/overview", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", w.Code)
	}
}

func TestSecurityOverviewEndpoint(t *testing.T) {
	t.Parallel()
	srv, cookie, db := securityOverviewServer(t)
	hostID, err := db.CreateHost(&store.Host{Name: "srv1", Host: "10.0.0.1", GroupName: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveSecurityScan(&store.SecurityScan{HostID: hostID, ScannedAt: time.Now().UTC(), Status: "ok", PendingSecurity: 4}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/api/security/overview", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var ov store.SecurityOverview
	if err := json.Unmarshal(w.Body.Bytes(), &ov); err != nil {
		t.Fatal(err)
	}
	if ov.HostCount != 1 || ov.Breakdown.PendingSecUpdates != 4 {
		t.Fatalf("fleet overview = %+v", ov)
	}
	if ov.Score < 0 || ov.Score > 100 || ov.Grade == "" || ov.Pillars == nil || ov.TopActions == nil {
		t.Fatalf("overview fields incomplete: %+v", ov)
	}

	req = httptest.NewRequest("GET", "/api/security/overview?host_id="+strconv.FormatInt(hostID, 10), nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("host scope expected 200, got %d", w.Code)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &ov); err != nil {
		t.Fatal(err)
	}
	if ov.HostCount != 1 || ov.Breakdown.PendingSecUpdates != 4 {
		t.Fatalf("host overview = %+v", ov)
	}

	req = httptest.NewRequest("GET", "/api/security/overview?group_id=prod", nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("group scope expected 200, got %d", w.Code)
	}

	req = httptest.NewRequest("GET", "/api/security/overview?host_id=abc", nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid host_id, got %d", w.Code)
	}
}
