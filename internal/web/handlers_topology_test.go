package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"pantau/internal/store"
	"pantau/internal/topology"
)

func topologyTestServer(t *testing.T) (*Server, *http.Cookie) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	srv := NewServer(db, nil, nil)
	token := "topology_test_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	return srv, &http.Cookie{Name: "pantau_session", Value: token}
}

func TestTopologyRequiresAuth(t *testing.T) {
	srv, _ := topologyTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/topology", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", w.Code)
	}
}

func TestTopologyRejectsPost(t *testing.T) {
	srv, cookie := topologyTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/topology", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestTopologyGraphFromStore(t *testing.T) {
	srv, cookie := topologyTestServer(t)
	hostID, err := srv.db.CreateHost(&store.Host{Name: "web-01", Host: "10.0.0.5", Port: 22, User: "root", Status: "healthy", GroupName: "Prod"})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	_, err = srv.db.CreateVPNNetwork(&store.VPNNetwork{Name: "lab", Driver: "wireguard", Enabled: true, LastState: "up"})
	if err != nil {
		t.Fatalf("create network: %v", err)
	}
	if _, err := srv.db.CreateAlert(&store.Alert{HostID: hostID, Level: "warning", Message: "drift"}); err != nil {
		t.Fatalf("create alert: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/topology", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var g topology.Graph
	if err := json.Unmarshal(w.Body.Bytes(), &g); err != nil {
		t.Fatalf("decode graph: %v", err)
	}
	if len(g.Nodes) < 3 || len(g.Edges) < 1 {
		t.Fatalf("graph too small: %+v", g)
	}
	var hostFound bool
	for _, n := range g.Nodes {
		if n.ID == "host:"+strconv.FormatInt(hostID, 10) {
			hostFound = true
			if n.Alerts != 1 || n.Group != "Prod" || n.Status != "unknown" {
				t.Fatalf("host node wrong: %+v", n)
			}
		}
	}
	if !hostFound {
		t.Fatal("host node missing from graph")
	}
	if g.Counts["host"] != 1 || g.Counts["gateway"] != 1 {
		t.Fatalf("counts wrong: %+v", g.Counts)
	}
}

func TestTopologyCacheInvalidation(t *testing.T) {
	srv, cookie := topologyTestServer(t)
	get := func() topology.Graph {
		req := httptest.NewRequest(http.MethodGet, "/api/topology", nil)
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		var g topology.Graph
		if err := json.Unmarshal(w.Body.Bytes(), &g); err != nil {
			t.Fatalf("decode graph: %v", err)
		}
		return g
	}
	if got := get(); got.Counts["host"] != 0 {
		t.Fatalf("empty fleet should have 0 hosts: %+v", got.Counts)
	}
	_, err := srv.db.CreateHost(&store.Host{Name: "cached", Host: "10.0.0.6", Port: 22, User: "root"})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	// Still cached — no host node yet.
	if got := get(); got.Counts["host"] != 0 {
		t.Fatalf("cache should still be warm: %+v", got.Counts)
	}
	// Explicit invalidation (as config writes do) refreshes the graph.
	srv.invalidateTopology()
	if got := get(); got.Counts["host"] != 1 {
		t.Fatalf("after invalidate expected 1 host: %+v", got.Counts)
	}
}

func TestTopologyHostDetailLazy(t *testing.T) {
	srv, cookie := topologyTestServer(t)
	hostID, err := srv.db.CreateHost(&store.Host{
		Name: "web", Host: "10.0.0.7", Port: 22, User: "root",
		ListeningPorts: `[{"proto":"tcp","port":"443","address":"0.0.0.0","public":true,"process":"nginx","risk":"high"}]`,
	})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	// CreateHost does not persist inspection-only fields; set them as an inspection would.
	h, _ := srv.db.GetHost(hostID)
	h.ListeningPorts = `[{"proto":"tcp","port":"443","address":"0.0.0.0","public":true,"process":"nginx","risk":"high"}]`
	if err := srv.db.UpdateHostInspection(h); err != nil {
		t.Fatalf("set inspection: %v", err)
	}
	if err := srv.db.ReplaceHostContainers(hostID, []store.HostContainer{{HostID: hostID, Name: "app", Image: "nginx:latest", State: "running"}}); err != nil {
		t.Fatalf("replace containers: %v", err)
	}

	// The global graph must NOT carry container/port nodes.
	greq := httptest.NewRequest(http.MethodGet, "/api/topology", nil)
	greq.AddCookie(cookie)
	gw := httptest.NewRecorder()
	srv.ServeHTTP(gw, greq)
	var g topology.Graph
	_ = json.Unmarshal(gw.Body.Bytes(), &g)
	for _, n := range g.Nodes {
		if n.Kind == "service" || n.Kind == "proxy" {
			t.Fatalf("global graph must stay macro-only, found %q", n.Kind)
		}
	}

	// Lazy detail carries containers and listening ports.
	req := httptest.NewRequest(http.MethodGet, "/api/topology/host?id="+strconv.FormatInt(hostID, 10), nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var d topology.HostDetail
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if len(d.Containers) != 1 || d.Containers[0].Name != "app" || d.Containers[0].Status != "running" {
		t.Fatalf("containers wrong: %+v", d.Containers)
	}
	if len(d.Listening) != 1 || d.Listening[0].Name != "nginx" {
		t.Fatalf("listening wrong: %+v", d.Listening)
	}
}

func TestTopologyHostDetailRequiresValidID(t *testing.T) {
	srv, cookie := topologyTestServer(t)
	for _, q := range []string{"", "?id=abc", "?id=0"} {
		req := httptest.NewRequest(http.MethodGet, "/api/topology/host"+q, nil)
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for %q, got %d", q, w.Code)
		}
	}
}
