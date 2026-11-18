package topology

import (
	"testing"
	"time"

	"pantau/internal/store"
)

type fakeStore struct {
	hosts    []store.Host
	networks []store.VPNNetwork
	accounts []store.CloudflareAccount
	clusters []store.KubeCluster
	alerts   []store.Alert
	calls    int // counts ListHosts calls, to prove cache hits
}

func (f *fakeStore) ListHosts() ([]store.Host, error)             { f.calls++; return f.hosts, nil }
func (f *fakeStore) ListVPNNetworks() ([]store.VPNNetwork, error) { return f.networks, nil }
func (f *fakeStore) ListCloudflareAccounts() ([]store.CloudflareAccount, error) {
	return f.accounts, nil
}
func (f *fakeStore) ListKubeClusters() ([]store.KubeCluster, error) { return f.clusters, nil }
func (f *fakeStore) ListActiveAlerts() ([]store.Alert, error)       { return f.alerts, nil }

func indexNodes(g *Graph) map[string]Node {
	m := map[string]Node{}
	for _, n := range g.Nodes {
		m[n.ID] = n
	}
	return m
}

func TestBuildMacroOnly(t *testing.T) {
	f := &fakeStore{
		hosts: []store.Host{
			{ID: 1, Name: "private-vps", Host: "10.0.0.9", Status: "degraded", NetworkID: 7,
				CPULoad: "0.8, 0.5", CPUCores: 2, RAMUsedBytes: 500, RAMTotalBytes: 1000,
				InternetLatencyMs: 42, GroupName: "Lab",
				ListeningPorts: `[{"proto":"tcp","port":"443","address":"0.0.0.0","public":true,"process":"nginx","risk":"high"}]`},
			{ID: 2, Name: "direct-vps", Host: "1.2.3.4", Status: "down"},
		},
		networks: []store.VPNNetwork{{ID: 7, Name: "netbird-lab", Driver: "netbird", Enabled: true, LastState: "up"}},
		accounts: []store.CloudflareAccount{{ID: 3, Name: "CF Prod"}},
		alerts:   []store.Alert{{ID: 1, HostID: 1, Level: "critical"}},
	}
	g, err := Build(f, time.Now())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	nodes := indexNodes(g)

	// Only macro kinds exist — no proxy/service nodes for containers or ports.
	for _, n := range g.Nodes {
		switch n.Kind {
		case NodeGateway, NodeVPN, NodeCloudflare, NodeHost, NodeKube:
		default:
			t.Fatalf("unexpected macro kind %q in global graph", n.Kind)
		}
	}
	if len(g.Nodes) != 5 { // gateway + vpn + cf + 2 hosts
		t.Fatalf("expected 5 macro nodes, got %d: %+v", len(g.Nodes), g.Nodes)
	}

	// Tiers are correct.
	if nodes["gateway"].Tier != TierEdge || nodes["vpn:7"].Tier != TierEdge || nodes["cf:3"].Tier != TierEdge {
		t.Fatal("tier-1 nodes wrong")
	}
	if nodes["host:1"].Tier != TierCompute || nodes["host:2"].Tier != TierCompute {
		t.Fatal("tier-2 nodes wrong")
	}

	// Host health, group, metrics and alert count.
	host := nodes["host:1"]
	if host.Status != "degraded" || host.Group != "Lab" || host.Network != "netbird-lab" || host.Alerts != 1 {
		t.Fatalf("host node wrong: %+v", host)
	}
	if host.Metrics.CPUPct != 40 || host.Metrics.RAMPct != 50 {
		t.Fatalf("host metrics wrong: %+v", host.Metrics)
	}
	if nodes["host:2"].Status != "down" {
		t.Fatalf("down host should be down: %+v", nodes["host:2"])
	}
	if nodes["vpn:7"].Status != "healthy" {
		t.Fatalf("up tunnel should be healthy: %+v", nodes["vpn:7"])
	}
	if nodes["cf:3"].Status != "unknown" {
		t.Fatalf("cloudflare should be unknown: %+v", nodes["cf:3"])
	}

	// Edges only connect macro tiers; host reached via VPN, not directly.
	var vpnEdge, directEdge bool
	for _, e := range g.Edges {
		if e.Kind == "vpn" && e.Source == "vpn:7" && e.Target == "host:1" && e.LatencyMs == 42 && e.Hops == 2 && e.Active {
			vpnEdge = true
		}
		if e.Kind == "direct" && e.Target == "host:1" {
			directEdge = true
		}
	}
	if !vpnEdge || directEdge {
		t.Fatalf("vpn/direct edges wrong: %+v", g.Edges)
	}
}

func TestBuildKubeClusterLink(t *testing.T) {
	f := &fakeStore{
		hosts:    []store.Host{{ID: 1, Name: "node-a", Status: "healthy", KubeClusterID: "uid-1"}},
		clusters: []store.KubeCluster{{ID: 5, Name: "prod-eks", KubeClusterID: "uid-1", Provider: "eks"}},
	}
	g, err := Build(f, time.Now())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	nodes := indexNodes(g)
	if nodes["kube:5"].Tier != TierCluster || nodes["kube:5"].Kind != NodeKube {
		t.Fatalf("kube node wrong: %+v", nodes["kube:5"])
	}
	var linked bool
	for _, e := range g.Edges {
		if e.Kind == "cluster" && e.Source == "host:1" && e.Target == "kube:5" {
			linked = true
		}
	}
	if !linked {
		t.Fatalf("cluster must link to member host: %+v", g.Edges)
	}
}

func TestBuildMissingNetworkFailsClosed(t *testing.T) {
	f := &fakeStore{hosts: []store.Host{{ID: 1, Name: "orphan", Status: "healthy", NetworkID: 99}}}
	g, err := Build(f, time.Now())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, e := range g.Edges {
		if e.Target == "host:1" {
			if e.Kind != "direct" || e.Active {
				t.Fatalf("missing network must produce inactive direct edge: %+v", e)
			}
			return
		}
	}
	t.Fatal("no edge to orphan host")
}

func TestCacheHitAndInvalidate(t *testing.T) {
	f := &fakeStore{hosts: []store.Host{{ID: 1, Name: "a", Status: "healthy"}}}
	c := NewCache(f, 20*time.Second)
	now := time.Now()
	if _, err := c.Get(now); err != nil {
		t.Fatalf("get: %v", err)
	}
	if f.calls != 1 {
		t.Fatalf("expected 1 build, got %d", f.calls)
	}
	// Cache hit within TTL: no new ListHosts call.
	if _, err := c.Get(now.Add(5 * time.Second)); err != nil {
		t.Fatalf("get: %v", err)
	}
	if f.calls != 1 {
		t.Fatalf("cache hit should not rebuild, calls=%d", f.calls)
	}
	// After TTL: rebuild.
	if _, err := c.Get(now.Add(25 * time.Second)); err != nil {
		t.Fatalf("get: %v", err)
	}
	if f.calls != 2 {
		t.Fatalf("expired cache should rebuild, calls=%d", f.calls)
	}
	// Invalidate forces a rebuild.
	c.Invalidate()
	if _, err := c.Get(now.Add(26 * time.Second)); err != nil {
		t.Fatalf("get: %v", err)
	}
	if f.calls != 3 {
		t.Fatalf("invalidate should rebuild, calls=%d", f.calls)
	}
}

func TestParseListening(t *testing.T) {
	rows := ParseListening(`[{"proto":"tcp","port":"443","address":"0.0.0.0","public":true,"process":"nginx","risk":"high"}]`)
	if len(rows) != 1 || rows[0].Name != "nginx" || !rows[0].Public || rows[0].Detail != "tcp 0.0.0.0" {
		t.Fatalf("parse listening wrong: %+v", rows)
	}
}

func TestNormalizeStatus(t *testing.T) {
	for in, want := range map[string]string{
		"healthy": "healthy", "degraded": "degraded", "down": "down", "stale": "unknown", "": "unknown",
	} {
		if got := normalizeStatus(in); got != want {
			t.Errorf("normalizeStatus(%q) = %q, want %q", in, got, want)
		}
	}
}
