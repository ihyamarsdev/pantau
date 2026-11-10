package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"pantau/internal/store"
)

func fleetSearchServer(t *testing.T) (*Server, *http.Cookie) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	srv := NewServer(db, nil, nil)
	token := "fleet_search_test_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	return srv, &http.Cookie{Name: "pantau_session", Value: token}
}

func TestFleetSearchRequiresAuth(t *testing.T) {
	srv, _ := fleetSearchServer(t)
	req := httptest.NewRequest("GET", "/api/search?q=openssl", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", w.Code)
	}
}

func TestFleetSearchRejectsShortQuery(t *testing.T) {
	srv, cookie := fleetSearchServer(t)
	for _, q := range []string{"", "o", " ", "x"} {
		req := httptest.NewRequest("GET", "/api/search?q="+url.QueryEscape(q), nil)
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for query %q, got %d", q, w.Code)
		}
	}
	long := ""
	for range 101 {
		long += "x"
	}
	req := httptest.NewRequest("GET", "/api/search?q="+long, nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a 101 character query, got %d", w.Code)
	}
}

func TestFleetSearchJSONShape(t *testing.T) {
	srv, cookie := fleetSearchServer(t)
	hostID, err := srv.db.CreateHost(&store.Host{Name: "alpha", Host: "10.0.0.1", Port: 22, User: "root", GroupName: "prod"})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	if err := srv.db.ReplaceHostPackages(hostID, []*store.HostPackage{
		{HostID: hostID, Name: "openssl", Version: "1.1.1"},
	}); err != nil {
		t.Fatalf("replace packages: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/search?q=openssl&kind=package", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		OK     bool   `json:"ok"`
		Query  string `json:"query"`
		Kind   string `json:"kind"`
		Groups []struct {
			Kind      string `json:"kind"`
			Name      string `json:"name"`
			Version   string `json:"version"`
			HostCount int    `json:"host_count"`
			Hosts     []struct {
				HostID int64  `json:"host_id"`
				Name   string `json:"name"`
				Group  string `json:"group"`
				Age    string `json:"data_age"`
			} `json:"hosts"`
		} `json:"groups"`
		Truncated            bool `json:"truncated"`
		TotalHosts           int  `json:"total_hosts"`
		HostsWithoutPackages int  `json:"hosts_without_packages"`
		HostsWithoutDocker   int  `json:"hosts_without_docker"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !res.OK || res.Query != "openssl" || res.Kind != "package" {
		t.Fatalf("unexpected envelope: %+v", res)
	}
	if len(res.Groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(res.Groups))
	}
	g := res.Groups[0]
	if g.Name != "openssl" || g.Version != "1.1.1" || g.HostCount != 1 || g.Kind != "package" {
		t.Fatalf("unexpected group: %+v", g)
	}
	if len(g.Hosts) != 1 || g.Hosts[0].HostID != hostID || g.Hosts[0].Name != "alpha" || g.Hosts[0].Group != "prod" {
		t.Fatalf("unexpected hosts: %+v", g.Hosts)
	}
	if g.Hosts[0].Age == "" {
		t.Error("expected a data age for the host")
	}
	if res.TotalHosts != 1 || res.HostsWithoutPackages != 0 || res.HostsWithoutDocker != 1 {
		t.Fatalf("unexpected coverage counts: %+v", res)
	}
}

func TestFleetSearchRejectsUnknownKind(t *testing.T) {
	srv, cookie := fleetSearchServer(t)
	req := httptest.NewRequest("GET", "/api/search?q=openssl&kind=nope", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unknown kind, got %d", w.Code)
	}
}

func TestFleetSearchFindsContainerImage(t *testing.T) {
	srv, cookie := fleetSearchServer(t)
	hostID, err := srv.db.CreateHost(&store.Host{Name: "alpha", Host: "10.0.0.1", Port: 22, User: "root", GroupName: "prod"})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	if err := srv.db.ReplaceHostContainers(hostID, []store.HostContainer{
		{HostID: hostID, Name: "db-main", Image: "postgres:13", State: "running"},
	}); err != nil {
		t.Fatalf("replace containers: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/search?q=postgres&kind=container", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Groups []struct {
			Kind  string `json:"kind"`
			Name  string `json:"name"`
			Hosts []struct {
				Name       string `json:"name"`
				Containers []struct {
					Name  string `json:"name"`
					State string `json:"state"`
				} `json:"containers"`
			} `json:"hosts"`
		} `json:"groups"`
		HostsWithoutDocker int `json:"hosts_without_docker"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Groups) != 1 || res.Groups[0].Name != "postgres:13" || res.Groups[0].Kind != "container" {
		t.Fatalf("unexpected groups: %+v", res.Groups)
	}
	hosts := res.Groups[0].Hosts
	if len(hosts) != 1 || hosts[0].Name != "alpha" {
		t.Fatalf("unexpected hosts: %+v", hosts)
	}
	if len(hosts[0].Containers) != 1 || hosts[0].Containers[0].Name != "db-main" || hosts[0].Containers[0].State != "running" {
		t.Fatalf("unexpected containers: %+v", hosts[0].Containers)
	}
	if res.HostsWithoutDocker != 0 {
		t.Errorf("expected the host to count as having container data, got %d without", res.HostsWithoutDocker)
	}
}
