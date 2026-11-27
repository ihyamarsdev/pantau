package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"pantau/internal/store"
)

func dockerOverviewServer(t *testing.T) (*Server, *http.Cookie, *store.DB) {
	t.Helper()
	db, err := store.Open(testDBCopy(t))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	srv := NewServer(db, nil, nil)
	token := "docker_overview_test_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	return srv, &http.Cookie{Name: "pantau_session", Value: token}, db
}

func TestDockerOverviewRequiresAuth(t *testing.T) {
	t.Parallel()
	srv, _, _ := dockerOverviewServer(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest("GET", "/api/docker/overview", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", w.Code)
	}
}

func TestDockerOverviewEndpoint(t *testing.T) {
	t.Parallel()
	srv, cookie, db := dockerOverviewServer(t)
	h, err := db.CreateHost(&store.Host{Name: "web-1", Host: "10.0.0.1", GroupName: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceHostContainers(h, []store.HostContainer{{HostID: h, Name: "nginx", Image: "nginx:1", State: "running", SeenAt: time.Now().UTC()}}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/api/docker/overview", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var ov store.DockerOverview
	if err := json.Unmarshal(w.Body.Bytes(), &ov); err != nil {
		t.Fatal(err)
	}
	if ov.HostCount != 1 || ov.HostsWithDocker != 1 || ov.TotalContainers != 1 || ov.RunningContainers != 1 {
		t.Fatalf("overview = %+v", ov)
	}
	if ov.Hosts == nil || ov.GeneratedAt.IsZero() {
		t.Fatalf("overview fields incomplete: %+v", ov)
	}

	req = httptest.NewRequest("GET", "/api/docker/overview?host_id=abc", nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid host_id, got %d", w.Code)
	}
}
