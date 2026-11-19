package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"pantau/internal/globe"
	"pantau/internal/store"
)

func globeTestServer(t *testing.T) (*Server, *http.Cookie) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	srv := NewServer(db, nil, nil)
	token := "globe_test_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	return srv, &http.Cookie{Name: "pantau_session", Value: token}
}

func TestGlobeRequiresAuth(t *testing.T) {
	srv, _ := globeTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/network-radar/globe", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", w.Code)
	}
}

func TestGlobeRejectsPost(t *testing.T) {
	srv, cookie := globeTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/network-radar/globe", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for POST, got %d", w.Code)
	}
}

func TestGlobeReturnsNodesAndFlows(t *testing.T) {
	srv, cookie := globeTestServer(t)
	db := srv.db
	id, err := db.CreateHost(&store.Host{Name: "web", Host: "203.0.113.10", Status: "healthy"})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.UpsertGeoIP(store.GeoIPEntry{IP: "203.0.113.10", Lat: 40, Lon: -74, City: "NYC", Source: "geoip"}, time.Now())
	_ = db.UpsertProbeResult(store.ProbeResult{SourceHostID: 0, TargetHostID: id, LatencyMs: 15}, time.Now())

	req := httptest.NewRequest(http.MethodGet, "/api/network-radar/globe", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var g globe.Graph
	if err := json.Unmarshal(w.Body.Bytes(), &g); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(g.Nodes) < 2 { // gateway + host
		t.Fatalf("expected at least 2 nodes, got %d", len(g.Nodes))
	}
	found := false
	for _, n := range g.Nodes {
		if n.Kind == "host" && n.Name == "web" && n.City == "NYC" && n.LatencyMs == 15 {
			found = true
		}
	}
	if !found {
		t.Fatalf("host node with geo + latency missing: %+v", g.Nodes)
	}
	if len(g.Flows) == 0 {
		t.Fatal("expected at least one gateway->host flow")
	}
}

func TestGlobeSetGeoOverride(t *testing.T) {
	srv, cookie := globeTestServer(t)
	id, err := srv.db.CreateHost(&store.Host{Name: "airgap", Host: "10.0.0.5"})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"id":` + strconv.FormatInt(id, 10) + `,"lat":-7.25,"lon":112.75,"label":"Surabaya"}`
	req := httptest.NewRequest(http.MethodPost, "/api/network-radar/globe/geo", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	h, _ := srv.db.GetHost(id)
	if h.GeoLat != -7.25 || h.GeoLabel != "Surabaya" {
		t.Fatalf("geo override not persisted: %+v", h)
	}

	// Invalid coordinates are rejected.
	req2 := httptest.NewRequest(http.MethodPost, "/api/network-radar/globe/geo", strings.NewReader(`{"id":`+strconv.FormatInt(id, 10)+`,"lat":999,"lon":0}`))
	req2.Header.Set("Content-Type", "application/json")
	req2.AddCookie(cookie)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid coords, got %d", w2.Code)
	}
}
