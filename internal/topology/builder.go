package topology

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"pantau/internal/store"
)

// Store is the narrow read-only slice of the store DB the macro graph needs.
// *store.DB satisfies it; tests use a small fake.
type Store interface {
	ListHosts() ([]store.Host, error)
	ListVPNNetworks() ([]store.VPNNetwork, error)
	ListCloudflareAccounts() ([]store.CloudflareAccount, error)
	ListKubeClusters() ([]store.KubeCluster, error)
	ListActiveAlerts() ([]store.Alert, error)
}

// Cache is a tiny TTL cache for the macro graph. It avoids repeated SQLite
// joins on every open of the topology view and is invalidated on config writes.
type Cache struct {
	mu      sync.Mutex
	ttl     time.Duration
	graph   *Graph
	expires time.Time
	store   Store
}

// NewCache creates a graph cache with the given TTL (e.g. 20s).
func NewCache(s Store, ttl time.Duration) *Cache { return &Cache{store: s, ttl: ttl} }

// Get returns the cached graph, rebuilding it when expired or absent.
func (c *Cache) Get(now time.Time) (*Graph, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.graph != nil && now.Before(c.expires) {
		return c.graph, nil
	}
	g, err := Build(c.store, now)
	if err != nil {
		return nil, err
	}
	c.graph = g
	c.expires = now.Add(c.ttl)
	return g, nil
}

// Invalidate drops the cached graph. Called after host/network/cluster config
// writes so the map does not show stale structure for the rest of the TTL.
func (c *Cache) Invalidate() {
	c.mu.Lock()
	c.graph = nil
	c.expires = time.Time{}
	c.mu.Unlock()
}

// Build projects stored inventory into a tiny layered connectivity graph.
// It never dials SSH or a vendor API, and never loads containers or ports.
func Build(s Store, now time.Time) (*Graph, error) {
	g := &Graph{
		Nodes:       make([]Node, 0),
		Edges:       make([]Edge, 0),
		Counts:      map[string]int{},
		GeneratedAt: now,
	}

	hosts, err := s.ListHosts()
	if err != nil {
		return nil, err
	}
	networks, err := s.ListVPNNetworks()
	if err != nil {
		return nil, err
	}
	accounts, err := s.ListCloudflareAccounts()
	if err != nil {
		return nil, err
	}
	clusters, err := s.ListKubeClusters()
	if err != nil {
		return nil, err
	}
	alerts, err := s.ListActiveAlerts()
	if err != nil {
		return nil, err
	}

	alertCounts := map[int64]int{}
	for _, a := range alerts {
		if a.HostID != 0 {
			alertCounts[a.HostID]++
		}
	}
	netByID := map[int64]store.VPNNetwork{}
	for _, n := range networks {
		netByID[n.ID] = n
	}

	// Tier 1: gateway + VPN networks + Cloudflare accounts.
	g.add(Node{ID: "gateway", Kind: NodeGateway, Label: "Pantau Gateway", Tier: TierEdge, Status: "healthy"})
	for _, n := range networks {
		g.add(Node{
			ID: "vpn:" + strconv.FormatInt(n.ID, 10), Kind: NodeVPN, Tier: TierEdge,
			Label: n.Name, Status: vpnStatus(n), Network: n.Driver,
		})
		g.connect("gateway", "vpn:"+strconv.FormatInt(n.ID, 10), "tunnel", 0, 1, n.Enabled && n.LastState == "up")
	}
	for _, a := range accounts {
		g.add(Node{
			ID: "cf:" + strconv.FormatInt(a.ID, 10), Kind: NodeCloudflare, Tier: TierEdge,
			Label: a.Name, Status: "unknown", Network: "cloudflare",
		})
		g.connect("gateway", "cf:"+strconv.FormatInt(a.ID, 10), "tunnel", 0, 1, true)
	}

	// Tier 2: hosts. Edges connect strictly to tier-1 infrastructure.
	for _, h := range hosts {
		hostID := strconv.FormatInt(h.ID, 10)
		hostNodeID := "host:" + hostID
		status := normalizeStatus(h.Status)
		networkName := ""
		if h.NetworkID != 0 {
			if n, ok := netByID[h.NetworkID]; ok {
				networkName = n.Name
			}
		}
		g.add(Node{
			ID: hostNodeID, Kind: NodeHost, Tier: TierCompute, Label: h.Name,
			Status: status, Group: h.GroupName, Network: networkName, HostID: h.ID,
			Alerts:   alertCounts[h.ID],
			Metrics:  Metrics{CPU: h.CPULoad, CPUPct: cpuPercent(h), RAMPct: ramPercent(h)},
			DetailID: hostID,
		})

		// One connectivity edge per host: via its VPN, or direct from gateway.
		if h.NetworkID != 0 {
			vpnID := "vpn:" + strconv.FormatInt(h.NetworkID, 10)
			if _, ok := netByID[h.NetworkID]; ok {
				g.connect(vpnID, hostNodeID, "vpn", h.InternetLatencyMs, 2, status != "down")
			} else {
				g.connect("gateway", hostNodeID, "direct", h.InternetLatencyMs, 1, false)
			}
		} else {
			g.connect("gateway", hostNodeID, "direct", h.InternetLatencyMs, 1, status != "down")
		}

	}

	// Tier 3: Kubernetes clusters linked to member hosts by kube_cluster_id.
	for _, c := range clusters {
		nodeID := "kube:" + strconv.FormatInt(c.ID, 10)
		g.add(Node{
			ID: nodeID, Kind: NodeKube, Tier: TierCluster, Label: c.Name,
			Status: "unknown", Network: c.Provider,
		})
		linked := false
		for _, h := range hosts {
			if c.KubeClusterID != "" && h.KubeClusterID == c.KubeClusterID {
				g.connect("host:"+strconv.FormatInt(h.ID, 10), nodeID, "cluster", 0, 3, normalizeStatus(h.Status) != "down")
				linked = true
			}
		}
		if !linked {
			g.connect("gateway", nodeID, "cluster", 0, 1, true)
		}
	}
	// Assign deterministic angular slots per ring. Hosts are ordered by group so
	// members of the same Host Group occupy adjacent sectors; the frontend only
	// reads sector/sectors and does polar math — no physics, no DOM measurement.
	assignSectors(g)
	return g, nil
}

// assignSectors gives every non-hub node a stable (sector, sectors) pair within
// its ring. Ordering is deterministic: inner ring by kind then id, outer ring
// by group then id, so a given inventory always produces the same layout.
func assignSectors(g *Graph) {
	inner := make([]*Node, 0)
	outer := make([]*Node, 0)
	for i := range g.Nodes {
		n := &g.Nodes[i]
		switch n.Tier {
		case TierEdge:
			if n.Kind == NodeGateway {
				n.Ring, n.Sector, n.Sectors = RingHub, 0, 1
			} else {
				n.Ring = RingInner
				inner = append(inner, n)
			}
		case TierCompute, TierCluster:
			n.Ring = RingOuter
			outer = append(outer, n)
		}
	}
	sort.Slice(inner, func(i, j int) bool {
		if inner[i].Kind != inner[j].Kind {
			return inner[i].Kind < inner[j].Kind
		}
		return inner[i].ID < inner[j].ID
	})
	sort.Slice(outer, func(i, j int) bool {
		if outer[i].Group != outer[j].Group {
			return outer[i].Group < outer[j].Group
		}
		return outer[i].ID < outer[j].ID
	})
	for i, n := range inner {
		n.Sector, n.Sectors = i, len(inner)
	}
	for i, n := range outer {
		n.Sector, n.Sectors = i, len(outer)
	}
}

func (g *Graph) add(n Node) {
	g.Nodes = append(g.Nodes, n)
	g.Counts[string(n.Kind)]++
}

func (g *Graph) connect(source, target, kind string, latency int64, hops int, active bool) {
	g.Edges = append(g.Edges, Edge{
		ID: source + ">" + target + ">" + kind, Source: source, Target: target,
		Kind: kind, LatencyMs: latency, Hops: hops, Active: active,
	})
}

func normalizeStatus(s string) string {
	switch s {
	case "healthy", "degraded", "down":
		return s
	default:
		return "unknown"
	}
}

func vpnStatus(n store.VPNNetwork) string {
	if !n.Enabled {
		return "unknown"
	}
	switch n.LastState {
	case "up":
		return "healthy"
	case "starting":
		return "degraded"
	case "error", "down":
		return "down"
	default:
		return "unknown"
	}
}

func cpuPercent(h store.Host) float64 {
	fields := strings.FieldsFunc(h.CPULoad, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
	if len(fields) == 0 {
		return 0
	}
	load, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || load <= 0 {
		return 0
	}
	cores := h.CPUCores
	if cores < 1 {
		cores = 1
	}
	pct := load / float64(cores) * 100
	if pct > 100 {
		pct = 100
	}
	return pct
}

func ramPercent(h store.Host) float64 {
	if h.RAMTotalBytes <= 0 {
		return 0
	}
	pct := float64(h.RAMUsedBytes) / float64(h.RAMTotalBytes) * 100
	if pct > 100 {
		pct = 100
	}
	return pct
}

// ParseListening converts a host's stored listening_ports JSON into lazy-detail rows.
func ParseListening(raw string) []ServiceRow {
	var ports []store.ListeningPort
	_ = json.Unmarshal([]byte(raw), &ports)
	rows := make([]ServiceRow, 0, len(ports))
	for _, p := range ports {
		name := p.Port
		if p.Process != "" {
			name = p.Process
		}
		rows = append(rows, ServiceRow{Name: name, Detail: p.Proto + " " + p.Address, Status: p.Risk, Public: p.Public})
	}
	return rows
}
