package globe

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

	"pantau/internal/store"
)

func geoTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// --- Resolver fallback logic ------------------------------------------------

func TestIsPrivateIP(t *testing.T) {
	cases := map[string]bool{
		"10.0.0.5": true, "172.16.0.1": true, "192.168.1.1": true,
		"100.64.0.1": true, "127.0.0.1": true, "169.254.1.1": true,
		"8.8.8.8": false, "203.0.113.9": false,
	}
	for ip, want := range cases {
		if got := isPrivateIP(net.ParseIP(ip)); got != want {
			t.Errorf("isPrivateIP(%s)=%v want %v", ip, got, want)
		}
	}
}

func TestResolverFallbackOffline(t *testing.T) {
	db := geoTestDB(t)
	r := NewResolver(db)
	// Force every external lookup to fail, simulating an air-gapped deployment.
	r.lookup = func(ctx context.Context, ip string) (store.GeoIPEntry, error) {
		return store.GeoIPEntry{}, errors.New("offline")
	}
	r.minSpace = 0

	got := r.ResolveIP(context.Background(), "8.8.8.8")
	if got.Source != "gateway" || got.Lat != 0 || got.Lon != 0 {
		t.Fatalf("offline lookup should fall back to gateway, got %+v", got)
	}
}

func TestResolverUsesCacheAndRefreshesStale(t *testing.T) {
	db := geoTestDB(t)
	r := NewResolver(db)
	r.minSpace = 0
	calls := 0
	r.lookup = func(ctx context.Context, ip string) (store.GeoIPEntry, error) {
		calls++
		return store.GeoIPEntry{IP: ip, Lat: 1.5, Lon: 2.5, City: "Fresh", Source: "geoip"}, nil
	}

	// Fresh cache hit: no external call.
	_ = db.UpsertGeoIP(store.GeoIPEntry{IP: "9.9.9.9", Lat: 1, Lon: 2, City: "Cached", Source: "geoip"}, time.Now())
	got := r.ResolveIP(context.Background(), "9.9.9.9")
	if got.City != "Cached" || calls != 0 {
		t.Fatalf("fresh cache should avoid lookup: got=%+v calls=%d", got, calls)
	}

	// Stale entry: refresh happens.
	_ = db.UpsertGeoIP(store.GeoIPEntry{IP: "9.9.9.9", Lat: 1, Lon: 2, City: "Old", Source: "geoip"}, time.Now().Add(-31*24*time.Hour))
	got = r.ResolveIP(context.Background(), "9.9.9.9")
	if got.City != "Fresh" || calls != 1 {
		t.Fatalf("stale cache should refresh: got=%+v calls=%d", got, calls)
	}
}

func TestResolverRunOnceSkipsPrivateIPs(t *testing.T) {
	db := geoTestDB(t)
	_, _ = db.CreateHost(&store.Host{Name: "private", Host: "10.0.0.5"})
	_, _ = db.CreateHost(&store.Host{Name: "override", Host: "8.8.8.8", GeoLat: -6, GeoLon: 106, GeoLabel: "Jakarta"})
	r := NewResolver(db)
	r.minSpace = 0
	calls := 0
	r.lookup = func(ctx context.Context, ip string) (store.GeoIPEntry, error) {
		calls++
		return store.GeoIPEntry{IP: ip, Source: "geoip"}, nil
	}
	if err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}
	if calls != 0 {
		t.Fatalf("private IP and manual override should not trigger lookups, got %d calls", calls)
	}
}

// --- Flow aggregator --------------------------------------------------------

type fakeStore struct {
	hosts    []store.Host
	networks []store.VPNNetwork
	accounts []store.CloudflareAccount
	clusters []store.KubeCluster
	alerts   []store.Alert
	domains  []store.HostDomainSite
	probes   map[[2]int64]*store.ProbeResult
	geo      map[string]*store.GeoIPEntry
}

func (f *fakeStore) ListHosts() ([]store.Host, error)             { return f.hosts, nil }
func (f *fakeStore) ListVPNNetworks() ([]store.VPNNetwork, error) { return f.networks, nil }
func (f *fakeStore) ListCloudflareAccounts() ([]store.CloudflareAccount, error) {
	return f.accounts, nil
}
func (f *fakeStore) ListKubeClusters() ([]store.KubeCluster, error)   { return f.clusters, nil }
func (f *fakeStore) ListActiveAlerts() ([]store.Alert, error)         { return f.alerts, nil }
func (f *fakeStore) ListHostDomains() ([]store.HostDomainSite, error) { return f.domains, nil }
func (f *fakeStore) GetProbeLatency(a, b int64) (*store.ProbeResult, error) {
	return f.probes[[2]int64{a, b}], nil
}
func (f *fakeStore) GetGeoIP(ip string, now time.Time) (*store.GeoIPEntry, bool, error) {
	return f.geo[ip], true, nil
}

func TestBuildGatewayHostFlows(t *testing.T) {
	fs := &fakeStore{
		hosts: []store.Host{
			{ID: 1, Name: "web", Host: "203.0.113.10", Status: "healthy"},
			{ID: 2, Name: "db", Host: "10.0.0.5", Status: "degraded", GeoLat: -6, GeoLon: 106, GeoLabel: "DC", NetworkID: 3},
		},
		probes: map[[2]int64]*store.ProbeResult{
			{0, 1}: {LatencyMs: 12},
		},
		geo: map[string]*store.GeoIPEntry{
			"203.0.113.10": {Lat: 40, Lon: -74, City: "NYC", Source: "geoip"},
		},
	}
	g, err := Build(fs, time.Now())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(g.Nodes) != 3 { // gateway + 2 hosts
		t.Fatalf("expected 3 nodes, got %d", len(g.Nodes))
	}
	var web, db *Node
	for i := range g.Nodes {
		if g.Nodes[i].Name == "web" {
			web = &g.Nodes[i]
		}
		if g.Nodes[i].Name == "db" {
			db = &g.Nodes[i]
		}
	}
	if web == nil || db == nil {
		t.Fatal("missing host nodes")
	}
	if web.Lat != 40 || web.City != "NYC" || web.GeoSource != "geoip" {
		t.Fatalf("geoip positioning failed: %+v", web)
	}
	if db.GeoSource != "manual" || db.City != "DC" {
		t.Fatalf("manual override failed: %+v", db)
	}
	if web.LatencyMs != 12 {
		t.Fatalf("gateway latency not attached: %+v", web)
	}

	// Gateway->host flows: one ssh, one vpn.
	var ssh, vpn int
	for _, f := range g.Flows {
		if f.Source != "gateway" {
			continue
		}
		if f.Type == "ssh" {
			ssh++
		}
		if f.Type == "vpn" {
			vpn++
		}
	}
	if ssh != 1 || vpn != 1 {
		t.Fatalf("expected 1 ssh + 1 vpn gateway flow, got ssh=%d vpn=%d (flows=%+v)", ssh, vpn, g.Flows)
	}
}

func TestBuildInterServerFlows(t *testing.T) {
	fs := &fakeStore{
		hosts: []store.Host{
			{ID: 1, Name: "proxy", Host: "203.0.113.1", Status: "healthy"},
			{ID: 2, Name: "app", Host: "203.0.113.2", Status: "healthy"},
			{ID: 3, Name: "cp", Host: "203.0.113.3", Status: "healthy", KubeClusterID: "c1"},
			{ID: 4, Name: "worker", Host: "203.0.113.4", Status: "healthy", KubeClusterID: "c1"},
			{ID: 5, Name: "vpnA", Host: "10.0.0.5", Status: "healthy", NetworkID: 7},
			{ID: 6, Name: "vpnB", Host: "10.0.0.6", Status: "healthy", NetworkID: 7},
		},
		domains: []store.HostDomainSite{
			{HostID: 1, WebSite: store.WebSite{Upstreams: []string{"http://203.0.113.2:8080"}}},
		},
	}
	g, err := Build(fs, time.Now())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	counts := map[string]int{}
	for _, f := range g.Flows {
		counts[f.Type]++
	}
	if counts["proxy"] != 1 {
		t.Errorf("expected 1 proxy flow, got %d", counts["proxy"])
	}
	if counts["k8s"] != 1 {
		t.Errorf("expected 1 k8s flow, got %d", counts["k8s"])
	}
	if counts["vpn"] < 3 { // 2 gateway vpn flows + 1 mesh flow
		t.Errorf("expected >=3 vpn flows, got %d", counts["vpn"])
	}
}

func TestBuildCloudflareFlow(t *testing.T) {
	fs := &fakeStore{
		hosts:    []store.Host{{ID: 1, Name: "origin", Host: "203.0.113.1", Status: "healthy"}},
		accounts: []store.CloudflareAccount{{ID: 9, Name: "example.com", AccountID: "acct"}},
		domains:  []store.HostDomainSite{{HostID: 1, WebSite: store.WebSite{Domain: "www.example.com"}}},
	}
	g, err := Build(fs, time.Now())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	found := false
	for _, f := range g.Flows {
		if f.Type == "cloudflare" && f.Source == "cf:9" && f.Target == "host:1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected cloudflare->origin flow, got %+v", g.Flows)
	}
}

func TestCacheInvalidation(t *testing.T) {
	fs := &fakeStore{hosts: []store.Host{{ID: 1, Name: "a", Host: "8.8.8.8"}}}
	c := NewCache(fs, time.Minute)
	g1, err := c.Get(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	g2, err := c.Get(time.Now())
	if err != nil || g1 != g2 {
		t.Fatal("cache should return same graph within TTL")
	}
	c.Invalidate()
	g3, err := c.Get(time.Now())
	if err != nil || g3 == g1 {
		t.Fatal("cache should rebuild after invalidation")
	}
}
