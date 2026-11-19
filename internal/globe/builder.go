package globe

// Geo-topology and inter-server flow aggregation. Everything here reads stored
// inventory; no network call happens on this path. Latency comes from persisted
// netdiag ping results (source_host_id 0 = Gateway).

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"pantau/internal/store"
)

// Store is the narrow read-only slice the globe builder needs. *store.DB
// satisfies it; tests use a small fake.
type Store interface {
	ListHosts() ([]store.Host, error)
	ListVPNNetworks() ([]store.VPNNetwork, error)
	ListCloudflareAccounts() ([]store.CloudflareAccount, error)
	ListKubeClusters() ([]store.KubeCluster, error)
	ListActiveAlerts() ([]store.Alert, error)
	ListHostDomains() ([]store.HostDomainSite, error)
	GetProbeLatency(sourceHostID, targetHostID int64) (*store.ProbeResult, error)
	GetGeoIP(ip string, now time.Time) (*store.GeoIPEntry, bool, error)
}

// Cache is a small TTL cache for the globe payload (15-30s), invalidated on
// config writes so the map never shows stale structure for long.
type Cache struct {
	mu      sync.Mutex
	ttl     time.Duration
	graph   *Graph
	expires time.Time
	store   Store
}

func NewCache(s Store, ttl time.Duration) *Cache { return &Cache{store: s, ttl: ttl} }

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

func (c *Cache) Invalidate() {
	c.mu.Lock()
	c.graph = nil
	c.expires = time.Time{}
	c.mu.Unlock()
}

// Build projects stored inventory into geo-located nodes and directed flows.
func Build(s Store, now time.Time) (*Graph, error) {
	g := &Graph{Nodes: []Node{}, Flows: []Flow{}, GeneratedAt: now}

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
	domains, err := s.ListHostDomains()
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

	// Gateway node at (0,0).
	g.Nodes = append(g.Nodes, Node{
		ID: "gateway", Name: "Pantau Gateway", Kind: "gateway",
		Status: "healthy", GeoSource: "default",
	})

	// Host nodes, positioned by manual override or cached GeoIP.
	hostByID := map[int64]store.Host{}
	for _, h := range hosts {
		hostByID[h.ID] = h
		g.Nodes = append(g.Nodes, hostNode(s, h, alertCounts[h.ID], now))
	}

	// 1. Gateway -> Host (control/SSH path), with persisted latency.
	for _, h := range hosts {
		status := flowStatus(h.Status)
		lat := gatewayLatency(s, h.ID)
		flowType := "ssh"
		if h.NetworkID != 0 {
			flowType = "vpn"
		}
		g.Flows = append(g.Flows, Flow{
			ID:     "gw-host-" + strconv.FormatInt(h.ID, 10),
			Source: "gateway", Target: "host:" + strconv.FormatInt(h.ID, 10),
			Type: flowType, LatencyMs: lat, Status: status, Active: status != "down",
		})
	}

	// Kubernetes cluster markers.
	for _, c := range clusters {
		g.Nodes = append(g.Nodes, Node{
			ID: "kube:" + strconv.FormatInt(c.ID, 10), Name: c.Name, Kind: "kube",
			Status: "unknown", GeoSource: "gateway", IP: c.Server,
		})
	}

	// --- Flows --------------------------------------------------------------

	// 2a. Host -> Host proxy flows inferred from reverse-proxy upstream targets.
	//     An upstream IP that matches a managed host's address creates a flow.
	addrToHost := map[string]int64{}
	for _, h := range hosts {
		for _, addr := range []string{h.Host, h.PublicIP} {
			addr = strings.TrimSpace(addr)
			if addr != "" {
				addrToHost[addr] = h.ID
			}
		}
	}
	seen := map[string]bool{}
	for _, d := range domains {
		for _, up := range d.Upstreams {
			targetID, ok := resolveUpstream(up, addrToHost, hosts)
			if !ok || targetID == d.HostID {
				continue
			}
			key := "proxy-" + strconv.FormatInt(d.HostID, 10) + "-" + strconv.FormatInt(targetID, 10)
			if seen[key] {
				continue
			}
			seen[key] = true
			src := hostByID[d.HostID]
			tgt := hostByID[targetID]
			lat := hostLatency(s, d.HostID, targetID)
			g.Flows = append(g.Flows, Flow{
				ID: key, Source: "host:" + strconv.FormatInt(d.HostID, 10),
				Target: "host:" + strconv.FormatInt(targetID, 10),
				Type:   "proxy", LatencyMs: lat,
				Status: combinedStatus(src.Status, tgt.Status), Active: true,
			})
		}
	}

	// 2b. Kubernetes control-plane -> worker flows. Hosts share a kube_cluster_id;
	//     the host whose kube_mode is 'full' is treated as the control plane and
	//     flows to the others (worker/read-only members).
	clusterHosts := map[string][]store.Host{}
	for _, h := range hosts {
		if h.KubeClusterID != "" {
			clusterHosts[h.KubeClusterID] = append(clusterHosts[h.KubeClusterID], h)
		}
	}
	for _, members := range clusterHosts {
		if len(members) < 2 {
			continue
		}
		sort.Slice(members, func(i, j int) bool { return members[i].ID < members[j].ID })
		control := members[0]
		for _, w := range members[1:] {
			key := "k8s-" + strconv.FormatInt(control.ID, 10) + "-" + strconv.FormatInt(w.ID, 10)
			g.Flows = append(g.Flows, Flow{
				ID: key, Source: "host:" + strconv.FormatInt(control.ID, 10),
				Target: "host:" + strconv.FormatInt(w.ID, 10),
				Type:   "k8s", Status: combinedStatus(control.Status, w.Status), Active: true,
			})
		}
	}

	// 2c. Shared VPN mesh peers: hosts on the same VPN Network form a mesh ring.
	hostsByNet := map[int64][]int64{}
	for _, h := range hosts {
		if h.NetworkID != 0 {
			hostsByNet[h.NetworkID] = append(hostsByNet[h.NetworkID], h.ID)
		}
	}
	for _, ids := range hostsByNet {
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for i := 0; i+1 < len(ids); i++ {
			key := "vpn-" + strconv.FormatInt(ids[i], 10) + "-" + strconv.FormatInt(ids[i+1], 10)
			lat := hostLatency(s, ids[i], ids[i+1])
			g.Flows = append(g.Flows, Flow{
				ID: key, Source: "host:" + strconv.FormatInt(ids[i], 10),
				Target: "host:" + strconv.FormatInt(ids[i+1], 10),
				Type:   "vpn", LatencyMs: lat, Status: "healthy", Active: true,
			})
		}
	}

	// 3. Cloudflare -> Origin Host. Tunnels/zones are not persisted, so a
	//    Cloudflare account links to hosts whose stored domains point at it by
	//    name; otherwise no origin flow is invented.
	for _, a := range accounts {
		for _, d := range domains {
			if strings.Contains(strings.ToLower(d.Domain), strings.ToLower(a.Name)) && a.Name != "" {
				key := "cf-" + strconv.FormatInt(a.ID, 10) + "-" + strconv.FormatInt(d.HostID, 10)
				if seen[key] {
					continue
				}
				seen[key] = true
				g.Flows = append(g.Flows, Flow{
					ID: key, Source: "cf:" + strconv.FormatInt(a.ID, 10),
					Target: "host:" + strconv.FormatInt(d.HostID, 10),
					Type:   "cloudflare", Status: flowStatus(hostByID[d.HostID].Status), Active: true,
				})
			}
		}
	}

	return g, nil
}

func hostNode(s Store, h store.Host, alerts int, now time.Time) Node {
	ip := strings.TrimSpace(h.PublicIP)
	if ip == "" {
		ip = strings.TrimSpace(h.Host)
	}
	n := Node{
		ID: "host:" + strconv.FormatInt(h.ID, 10), Name: h.Name, Kind: "host",
		IP: ip, PublicIP: strings.TrimSpace(h.PublicIP),
		Status: normalizeStatus(h.Status), GroupID: h.GroupName, NetworkID: h.NetworkID,
		HostID: h.ID, ActiveAlerts: alerts,
		LatencyMs: gatewayLatency(s, h.ID),
		CPUPct:    cpuPercent(h), MemPct: memPercent(h),
	}
	switch {
	case h.GeoLat != 0 || h.GeoLon != 0 || h.GeoLabel != "":
		// Manual override wins for air-gapped and private-IP hosts (ADR 0077).
		n.Lat, n.Lon = h.GeoLat, h.GeoLon
		n.City = h.GeoLabel
		n.GeoSource = "manual"
	default:
		// Read the cached resolution; a stale entry still positions the pin.
		if entry, _, err := s.GetGeoIP(ip, now); err == nil && entry != nil {
			n.Lat, n.Lon = entry.Lat, entry.Lon
			n.City = entry.City
			n.CountryCode = entry.CountryCode
			n.GeoSource = entry.Source
		} else {
			// Gateway fallback: private IP, air-gapped, or offline with no cache.
			n.Lat, n.Lon = 0, 0
			n.City = "Pantau Gateway"
			n.GeoSource = "gateway"
		}
	}
	return n
}

// resolveUpstream maps an upstream token (IP or host) to a managed host ID.
func resolveUpstream(up string, addrToHost map[string]int64, hosts []store.Host) (int64, bool) {
	up = strings.TrimSpace(up)
	// Strip scheme, port, and path: proxy_pass values can be "http://10.0.0.5:8080".
	if i := strings.Index(up, "://"); i >= 0 {
		up = up[i+3:]
	}
	if i := strings.IndexAny(up, "/?;"); i >= 0 {
		up = up[:i]
	}
	if h, _, err := netSplitHostPort(up); err == nil {
		up = h
	}
	if id, ok := addrToHost[up]; ok {
		return id, true
	}
	// Also match by host name.
	for _, h := range hosts {
		if strings.EqualFold(h.Name, up) {
			return h.ID, true
		}
	}
	return 0, false
}

func netSplitHostPort(s string) (string, string, error) {
	// Minimal net.SplitHostPort replacement without importing net just for this.
	if i := strings.LastIndex(s, ":"); i > 0 && !strings.Contains(s[i+1:], ":") {
		return s[:i], s[i+1:], nil
	}
	return s, "", nil
}

func gatewayLatency(s Store, hostID int64) int64 {
	p, err := s.GetProbeLatency(0, hostID)
	if err != nil || p == nil {
		return 0
	}
	return int64(p.LatencyMs)
}

func hostLatency(s Store, srcID, tgtID int64) int64 {
	p, err := s.GetProbeLatency(srcID, tgtID)
	if err != nil || p == nil {
		return 0
	}
	return int64(p.LatencyMs)
}

func normalizeStatus(s string) string {
	switch strings.ToLower(s) {
	case "healthy", "degraded", "down":
		return strings.ToLower(s)
	default:
		return "unknown"
	}
}

func flowStatus(s string) string {
	switch normalizeStatus(s) {
	case "healthy":
		return "healthy"
	case "degraded":
		return "degraded"
	case "down":
		return "down"
	default:
		return "degraded"
	}
}

func combinedStatus(a, b string) string {
	sa, sb := normalizeStatus(a), normalizeStatus(b)
	if sa == "down" || sb == "down" {
		return "down"
	}
	if sa == "degraded" || sb == "degraded" || sa == "unknown" || sb == "unknown" {
		return "degraded"
	}
	return "healthy"
}

func cpuPercent(h store.Host) float64 {
	// CPULoad is the raw 1-minute load; normalize per core to 0-100.
	var v float64
	_, _ = fmt.Sscanf(h.CPULoad, "%f", &v)
	cores := h.CPUCores
	if cores < 1 {
		cores = 1
	}
	pct := v / float64(cores) * 100
	if pct > 100 {
		pct = 100
	}
	if pct < 0 {
		pct = 0
	}
	return pct
}

func memPercent(h store.Host) float64 {
	if h.RAMTotalBytes <= 0 {
		return 0
	}
	return float64(h.RAMUsedBytes) / float64(h.RAMTotalBytes) * 100
}
