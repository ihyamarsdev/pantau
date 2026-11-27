package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pantau/internal/store"
)

// A session survives a restart: the browser keeps its cookie and the next run of
// Pantau still knows the token, so the operator is not asked for the password
// again after an update or a reboot.
func TestSessionSurvivesRestart(t *testing.T) {
	t.Parallel()
	db, err := store.Open(testDBCopy(t))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	first := NewServer(db, nil, nil)
	w := httptest.NewRecorder()
	token := first.issueSession(w, httptest.NewRequest(http.MethodPost, "/api/login", nil))
	if token == "" {
		t.Fatal("no session token issued")
	}
	cookie := &http.Cookie{Name: "pantau_session", Value: token}

	// The cookie is long lived: a year from now, not a week.
	res := w.Result()
	var set *http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == "pantau_session" {
			set = c
		}
	}
	if set == nil {
		t.Fatal("no pantau_session cookie set")
	}
	if set.MaxAge != int(sessionTTL/time.Second) {
		t.Errorf("cookie MaxAge = %d, want %d", set.MaxAge, int(sessionTTL/time.Second))
	}
	if time.Until(set.Expires) < 300*24*time.Hour {
		t.Errorf("cookie expires too soon: %v", set.Expires)
	}

	// A restart: a brand new Server on the same database.
	second := NewServer(db, nil, nil)
	if !meAuthenticated(t, second, cookie) {
		t.Fatal("the session was forgotten on restart")
	}

	// Logout ends it on disk too, so a later restart cannot revive it.
	req := httptest.NewRequest("POST", "/api/logout", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	second.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout: expected 200, got %d", rec.Code)
	}
	if meAuthenticated(t, NewServer(db, nil, nil), cookie) {
		t.Fatal("a logged out session came back after a restart")
	}
	saved, err := db.LoadSessions(time.Now())
	if err != nil {
		t.Fatalf("load sessions: %v", err)
	}
	if _, ok := saved[token]; ok {
		t.Error("the logged out token is still stored")
	}
}

// An expired session is not restored from the database.
func TestExpiredSessionIsDropped(t *testing.T) {
	t.Parallel()
	db, err := store.Open(testDBCopy(t))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := db.SaveSession("stale", time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("save: %v", err)
	}
	srv := NewServer(db, nil, nil)
	cookie := &http.Cookie{Name: "pantau_session", Value: "stale"}
	if meAuthenticated(t, srv, cookie) {
		t.Fatal("an expired session was restored")
	}
	saved, err := db.LoadSessions(time.Now())
	if err != nil {
		t.Fatalf("load sessions: %v", err)
	}
	if _, ok := saved["stale"]; ok {
		t.Error("the expired row was not purged")
	}
}

func meAuthenticated(t *testing.T, srv *Server, cookie *http.Cookie) bool {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/me", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/me: expected 200, got %d", rec.Code)
	}
	return strings.Contains(rec.Body.String(), `"authenticated":true`)
}
