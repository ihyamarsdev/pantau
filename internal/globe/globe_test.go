package globe

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
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
		return store.GeoIPEntry{IP: ip, Lat: 1.5, Lon: 2.5, City: "Fresh", Region: "West", Source: "geoip"}, nil
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
	if got.City != "Fresh" || got.Region != "West" || calls != 1 {
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

// A relocated host (new public IP) must be resolved at its new IP; the old cached IP must never position it.
func TestResolveHostRelocationUsesCurrentIP(t *testing.T) {
	db := geoTestDB(t)
	r := NewResolver(db)
	r.minSpace = 0
	// Old IP is cached in Singapore; the new IP resolves in Jakarta.
	_ = db.UpsertGeoIP(store.GeoIPEntry{IP: "203.0.113.10", Lat: 1.35, Lon: 103.8, City: "Singapore", Source: "geoip"}, time.Now())
	resolved := map[string]store.GeoIPEntry{
		"198.51.100.20": {IP: "198.51.100.20", Lat: -6.2, Lon: 106.8, City: "Jakarta", Source: "geoip"},
	}
	r.lookup = func(ctx context.Context, ip string) (store.GeoIPEntry, error) {
		if e, ok := resolved[ip]; ok {
			return e, nil
		}
		return store.GeoIPEntry{}, errors.New("unexpected IP " + ip)
	}
	h := store.Host{Name: "relocated", Host: "198.51.100.20", PublicIP: "198.51.100.20"}
	got := r.ResolveHost(context.Background(), h)
	if got.City != "Jakarta" || got.Lat != -6.2 || got.Lon != 106.8 {
		t.Fatalf("relocated host should use new IP resolution, got %+v", got)
	}
	if e, _, _ := db.GetGeoIP("198.51.100.20", time.Now()); e == nil || e.City != "Jakarta" {
		t.Fatalf("new IP should be cached, got %+v", e)
	}
}

// ResolveIP must never send a private / CGNAT / loopback address to the external provider, even when called directly.
func TestResolveIPNeverLooksUpPrivateIPs(t *testing.T) {
	db := geoTestDB(t)
	r := NewResolver(db)
	r.minSpace = 0
	calls := 0
	r.lookup = func(ctx context.Context, ip string) (store.GeoIPEntry, error) {
		calls++
		return store.GeoIPEntry{IP: ip, Source: "geoip"}, nil
	}
	for _, ip := range []string{"10.0.0.5", "100.64.0.1", "127.0.0.1", "169.254.1.1", "192.168.1.1", "not-an-ip"} {
		got := r.ResolveIP(context.Background(), ip)
		if got.Source != "gateway" {
			t.Errorf("private/invalid IP %s should fall back to gateway, got %+v", ip, got)
		}
	}
	if calls != 0 {
		t.Fatalf("no external lookup allowed for private/invalid IPs, got %d calls", calls)
	}
}

// A manual override (including a region-only override) must be skipped by ResolveHost without any external call.
func TestResolveHostSkipsManualOverride(t *testing.T) {
	db := geoTestDB(t)
	r := NewResolver(db)
	r.minSpace = 0
	calls := 0
	r.lookup = func(ctx context.Context, ip string) (store.GeoIPEntry, error) {
		calls++
		return store.GeoIPEntry{IP: ip, Source: "geoip"}, nil
	}
	got := r.ResolveHost(context.Background(), store.Host{Name: "manual", Host: "8.8.8.8", GeoRegion: "Jawa Barat"})
	if got.IP != "" || calls != 0 {
		t.Fatalf("manual override should skip lookup (calls=%d, got=%+v)", calls, got)
	}
}

// RunOnce must preserve stale cache rows so an offline deployment never loses a known pin.
func TestRunOncePreservesStaleCache(t *testing.T) {
	db := geoTestDB(t)
	_, _ = db.CreateHost(&store.Host{Name: "offline", Host: "10.0.0.5"})
	_ = db.UpsertGeoIP(store.GeoIPEntry{IP: "8.8.8.8", Lat: 1, Lon: 2, City: "Known", Source: "geoip"}, time.Now().Add(-40*24*time.Hour))
	r := NewResolver(db)
	r.minSpace = 0
	r.lookup = func(ctx context.Context, ip string) (store.GeoIPEntry, error) {
		return store.GeoIPEntry{}, errors.New("offline")
	}
	if err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}
	if e, _, _ := db.GetGeoIP("8.8.8.8", time.Now()); e == nil {
		t.Fatal("stale cache entry must be preserved when offline")
	}
}

func TestResolveGatewayEgress(t *testing.T) {
	db := geoTestDB(t)
	r := NewResolver(db)
	r.minSpace = 0
	r.egressURL = "http://fake-egress"
	// Mock egress query
	r.http = &http.Client{
		Transport: &roundTripFn{fn: func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.String(), "fake-egress") {
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(strings.NewReader("198.51.100.50")),
				}, nil
			}
			return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(""))}, nil
		}},
	}
	r.lookup = func(ctx context.Context, ip string) (store.GeoIPEntry, error) {
		if ip == "198.51.100.50" {
			return store.GeoIPEntry{IP: ip, Lat: -7.79, Lon: 110.36, City: "Yogyakarta", Region: "DI Yogyakarta", Source: "geoip"}, nil
		}
		return store.GeoIPEntry{}, errors.New("unexpected IP")
	}

	got := r.ResolveGateway(context.Background())
	if got.City != "Yogyakarta" || got.Lat != -7.79 || got.Source != "egress" {
		t.Fatalf("ResolveGateway expected Yogyakarta egress, got %+v", got)
	}
	// Check cached under "gateway"
	e, _, err := db.GetGeoIP("gateway", time.Now())
	if err != nil || e == nil || e.City != "Yogyakarta" {
		t.Fatalf("gateway should be cached in SQLite: %+v", e)
	}
}

type roundTripFn struct {
	fn func(req *http.Request) (*http.Response, error)
}

func (f *roundTripFn) RoundTrip(req *http.Request) (*http.Response, error) {
	return f.fn(req)
}

// --- Link aggregator --------------------------------------------------------

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

func TestBuildGatewayHostLinks(t *testing.T) {
	fs := &fakeStore{
		hosts: []store.Host{
			{ID: 1, Name: "web", Host: "203.0.113.10", Status: "healthy"},
			{ID: 2, Name: "db", Host: "10.0.0.5", Status: "degraded", GeoLat: -6, GeoLon: 106, GeoLabel: "DC", GeoRegion: "Jawa Timur", NetworkID: 3},
		},
		probes: map[[2]int64]*store.ProbeResult{
			{0, 1}: {LatencyMs: 12},
		},
		geo: map[string]*store.GeoIPEntry{
			"203.0.113.10": {Lat: 40, Lon: -74, City: "NYC", Region: "New York", ASNOrg: "Example ISP", Source: "geoip"},
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
	if web.Lat != 40 || web.City != "NYC" || web.Region != "New York" || web.ASNOrg != "Example ISP" || web.GeoSource != "geoip" {
		t.Fatalf("geoip positioning failed: %+v", web)
	}
	if db.GeoSource != "manual" || db.City != "DC" || db.Region != "Jawa Timur" {
		t.Fatalf("manual override failed: %+v", db)
	}
	if web.LatencyMs != 12 {
		t.Fatalf("gateway latency not attached: %+v", web)
	}

	// Gateway->host links: one ssh, one vpn.
	var ssh, vpn int
	for _, f := range g.Links {
		if f.SourceID != "gateway" {
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
		t.Fatalf("expected 1 ssh + 1 vpn gateway link, got ssh=%d vpn=%d (links=%+v)", ssh, vpn, g.Links)
	}
}

func TestBuildInterServerLinks(t *testing.T) {
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
	for _, f := range g.Links {
		counts[f.Type]++
		// Only inter-server links (both endpoints are hosts) must be
		// bidirectional and labeled; gateway->host control links are directed.
		if !strings.HasPrefix(f.SourceID, "host:") || !strings.HasPrefix(f.TargetID, "host:") {
			continue
		}
		if f.Type == "proxy" || f.Type == "k8s" || f.Type == "vpn" {
			if !f.Bidirectional {
				t.Errorf("inter-server link %s should be bidirectional", f.ID)
			}
			if f.Label == "" {
				t.Errorf("inter-server link %s should carry a label", f.ID)
			}
		}
	}

	if counts["proxy"] != 1 {
		t.Errorf("expected 1 proxy link, got %d", counts["proxy"])
	}
	if counts["k8s"] != 1 {
		t.Errorf("expected 1 k8s link, got %d", counts["k8s"])
	}
	if counts["vpn"] < 3 { // 2 gateway vpn links + 1 mesh link
		t.Errorf("expected >=3 vpn links, got %d", counts["vpn"])
	}
}

func TestBuildTCPPeerLink(t *testing.T) {
	fs := &fakeStore{
		hosts: []store.Host{
			{ID: 1, Name: "api", Host: "203.0.113.1", Status: "healthy", TopConnections: `[{"remote_ip":"203.0.113.2","port":"5432","count":4}]`},
			{ID: 2, Name: "db", Host: "203.0.113.2", Status: "healthy"},
		},
	}
	g, err := Build(fs, time.Now())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	found := false
	for _, l := range g.Links {
		if l.Type == "app" && l.Bidirectional && l.Label == "TCP peer" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected inferred TCP peer link, got %+v", g.Links)
	}
}

func TestBuildCloudflareLink(t *testing.T) {
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
	for _, f := range g.Links {
		if f.Type == "cloudflare" && f.SourceID == "cf:9" && f.TargetID == "host:1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected cloudflare->origin link, got %+v", g.Links)
	}
}

func TestSpreadCoLocated(t *testing.T) {
	nodes := []Node{
		{ID: "gateway", Kind: "gateway", Lat: 0, Lon: 0},
		{ID: "host:9", Kind: "host", Lat: 0, Lon: 0},
		{ID: "host:3", Kind: "host", Lat: 40, Lon: -74},
		{ID: "host:1", Kind: "host", Lat: 40, Lon: -74},
		{ID: "host:2", Kind: "host", Lat: 40, Lon: -74},
	}
	spreadCoLocated(nodes)
	seen := map[[2]float64]bool{}
	for _, n := range nodes {
		key := [2]float64{round4(n.Lat), round4(n.Lon)}
		if seen[key] {
			t.Fatalf("co-located pins still stack at %v: %+v", key, nodes)
		}
		seen[key] = true
	}
	// The gateway keeps the fallback coordinates even when a host shares them.
	if nodes[0].Lat != 0 || nodes[0].Lon != 0 {
		t.Fatalf("gateway should keep exact (0,0), got %+v", nodes[0])
	}
	// The lowest-ID host keeps the original coordinates.
	if nodes[3].Lat != 40 || nodes[3].Lon != -74 {
		t.Fatalf("first host should keep exact coordinates, got %+v", nodes[3])
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

func TestHTTPLookupIP2LocationSuccess(t *testing.T) {
	db := geoTestDB(t)
	r := NewResolver(db)
	r.http = &http.Client{
		Transport: &roundTripFn{fn: func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.String(), "api.ip2location.io") {
				body := `{"ip":"157.85.212.174","country_code":"ID","country_name":"Indonesia","region_name":"Yogyakarta","city_name":"Yogyakarta","latitude":-7.78278,"longitude":110.36083,"as":"PT XL Axiata Tbk"}`
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			}
			return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("fail"))}, nil
		}},
	}
	entry, err := r.httpLookup(context.Background(), "157.85.212.174")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.City != "Yogyakarta" || entry.Region != "Yogyakarta" || entry.Lat != -7.78278 || entry.Lon != 110.36083 {
		t.Fatalf("unexpected entry: %+v", entry)
	}
}

func TestHTTPLookupFallbackToIPAPI(t *testing.T) {
	db := geoTestDB(t)
	r := NewResolver(db)
	r.http = &http.Client{
		Transport: &roundTripFn{fn: func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.String(), "api.ip2location.io") {
				return &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader("rate limited"))}, nil
			}
			if strings.Contains(req.URL.String(), "ip-api.com") {
				body := `{"status":"success","countryCode":"ID","regionName":"Jakarta","city":"Jakarta","lat":-6.2,"lon":106.8,"as":"AS139994"}`
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			}
			return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("fail"))}, nil
		}},
	}
	entry, err := r.httpLookup(context.Background(), "157.85.212.174")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.City != "Jakarta" || entry.Lat != -6.2 {
		t.Fatalf("expected fallback to ip-api, got: %+v", entry)
	}
}
