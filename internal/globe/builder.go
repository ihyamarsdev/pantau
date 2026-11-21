package globe

// Geo-topology and inter-server link aggregation. Everything here reads stored
// inventory; no network call happens on this path. Latency comes from persisted
// netdiag ping results (source_host_id 0 = Gateway).

import (
	"encoding/json"
	"fmt"
	"math"
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

// Build projects stored inventory into geo-located nodes and directed links.
func Build(s Store, now time.Time) (*Graph, error) {
	g := &Graph{Nodes: []Node{}, Links: []Link{}, GeneratedAt: now}

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

	// Gateway node positioned at detected egress location or fallback (0,0).
	gwNode := Node{
		ID: "gateway", Name: "Pantau Gateway", Kind: "gateway",
		Status: "healthy", GeoSource: "default",
	}
	if entry, _, err := s.GetGeoIP("gateway", now); err == nil && entry != nil && (entry.Lat != 0 || entry.Lon != 0) {
		gwNode.Lat, gwNode.Lon = entry.Lat, entry.Lon
		gwNode.City = entry.City
		gwNode.Region = entry.Region
		gwNode.CountryCode = entry.CountryCode
		gwNode.ASNOrg = entry.ASNOrg
		gwNode.GeoSource = entry.Source
	}
	g.Nodes = append(g.Nodes, gwNode)

	// Host nodes, positioned by manual override or cached GeoIP.
	hostByID := map[int64]store.Host{}
	for _, h := range hosts {
		hostByID[h.ID] = h
		g.Nodes = append(g.Nodes, hostNode(s, h, alertCounts[h.ID], now))
	}

	// Kubernetes cluster markers.
	for _, c := range clusters {
		g.Nodes = append(g.Nodes, Node{
			ID: "kube:" + strconv.FormatInt(c.ID, 10), Name: c.Name, Kind: "kube",
			Status: "unknown", GeoSource: "gateway", IP: c.Server,
		})
	}

	// Deterministic micro-spiral spreading: hosts sharing the exact same
	// (lat,lon) are offset by a few hundredths of a degree so their 3D pins
	// never stack invisibly. Sorting by ID makes the layout stable across builds.
	spreadCoLocated(g.Nodes)

	// --- Links --------------------------------------------------------------

	// 1. Gateway -> Host (control/SSH path), with persisted latency.
	for _, h := range hosts {
		status := flowStatus(h.Status)
		lat := gatewayLatency(s, h.ID)
		linkType := "ssh"
		label := "SSH telemetry"
		if h.NetworkID != 0 {
			linkType = "vpn"
			label = vpnLabel(netByID[h.NetworkID].Name)
		}
		g.Links = append(g.Links, Link{
			ID:       "gw-host-" + strconv.FormatInt(h.ID, 10),
			SourceID: "gateway", TargetID: "host:" + strconv.FormatInt(h.ID, 10),
			Type: linkType, Label: label, LatencyMs: lat, Status: status, Active: status != "down",
		})
	}

	seen := map[string]bool{}

	// 2a. Host -> Host proxy links inferred from reverse-proxy upstream targets.
	addrToHost := map[string]int64{}
	for _, h := range hosts {
		for _, addr := range []string{h.Host, h.PublicIP} {
			addr = strings.TrimSpace(addr)
			if addr != "" {
				addrToHost[addr] = h.ID
			}
		}
	}
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
			g.Links = append(g.Links, Link{
				ID: key, SourceID: "host:" + strconv.FormatInt(d.HostID, 10),
				TargetID: "host:" + strconv.FormatInt(targetID, 10),
				Type:     "proxy", Label: "Reverse proxy", Bidirectional: true,
				LatencyMs: lat, Status: combinedStatus(src.Status, tgt.Status), Active: true,
			})
		}
	}

	// 2b. Host -> Host active TCP peer links from inspection telemetry
	// (TopConnections.RemoteIP). An established socket whose remote address
	// matches another managed host's public/private/VPN IP yields a link.
	peerLinks(s, hosts, g, seen)

	// 2c. Kubernetes control-plane -> worker links. Hosts share a
	// kube_cluster_id; the lowest-ID host is treated as the control plane.
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
			g.Links = append(g.Links, Link{
				ID: key, SourceID: "host:" + strconv.FormatInt(control.ID, 10),
				TargetID: "host:" + strconv.FormatInt(w.ID, 10),
				Type:     "k8s", Label: "Cluster mesh", Bidirectional: true,
				Status: combinedStatus(control.Status, w.Status), Active: true,
			})
		}
	}

	// 2d. Shared VPN mesh peers: hosts on the same VPN Network form a mesh ring.
	hostsByNet := map[int64][]int64{}
	for _, h := range hosts {
		if h.NetworkID != 0 {
			hostsByNet[h.NetworkID] = append(hostsByNet[h.NetworkID], h.ID)
		}
	}
	for netID, ids := range hostsByNet {
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for i := 0; i+1 < len(ids); i++ {
			key := "vpn-" + strconv.FormatInt(ids[i], 10) + "-" + strconv.FormatInt(ids[i+1], 10)
			lat := hostLatency(s, ids[i], ids[i+1])
			g.Links = append(g.Links, Link{
				ID: key, SourceID: "host:" + strconv.FormatInt(ids[i], 10),
				TargetID: "host:" + strconv.FormatInt(ids[i+1], 10),
				Type:     "vpn", Label: vpnLabel(netByID[netID].Name), Bidirectional: true,
				LatencyMs: lat, Status: "healthy", Active: true,
			})
		}
	}

	// 3. Cloudflare -> Origin Host. Tunnels/zones are not persisted, so a
	// Cloudflare account links to hosts whose stored domains point at it by
	// name; otherwise no origin link is invented.
	for _, a := range accounts {
		for _, d := range domains {
			if strings.Contains(strings.ToLower(d.Domain), strings.ToLower(a.Name)) && a.Name != "" {
				key := "cf-" + strconv.FormatInt(a.ID, 10) + "-" + strconv.FormatInt(d.HostID, 10)
				if seen[key] {
					continue
				}
				seen[key] = true
				g.Links = append(g.Links, Link{
					ID: key, SourceID: "cf:" + strconv.FormatInt(a.ID, 10),
					TargetID: "host:" + strconv.FormatInt(d.HostID, 10),
					Type:     "cloudflare", Label: "Cloudflare edge", Bidirectional: true,
					Status: flowStatus(hostByID[d.HostID].Status), Active: true,
				})
			}
		}
	}

	return g, nil
}

// spreadCoLocated nudges nodes that share exact coordinates onto a
// deterministic micro-spiral. This includes hosts and kube markers sitting on
// the gateway fallback at (0,0). The lowest-ID node stays put; subsequent ones
// get progressively larger angular offsets (~0.02° steps), keeping pins
// visually distinct on a globe without moving them off their datacenter region.
func spreadCoLocated(nodes []Node) {
	// Group by exact coordinate.
	groups := map[[2]float64][]int{}
	for i := range nodes {
		n := &nodes[i]
		key := [2]float64{round4(n.Lat), round4(n.Lon)}
		groups[key] = append(groups[key], i)
	}
	for _, idxs := range groups {
		if len(idxs) < 2 {
			continue
		}
		// Stable order by node ID regardless of map iteration order.
		sort.Slice(idxs, func(a, b int) bool { return nodes[idxs[a]].ID < nodes[idxs[b]].ID })
		for rank, idx := range idxs {
			if rank == 0 {
				continue
			}
			angle := float64(rank) * 2.399963 // golden angle
			radius := 0.02 * math.Sqrt(float64(rank))
			nodes[idx].Lat += radius * math.Sin(angle)
			nodes[idx].Lon += radius * math.Cos(angle)
		}
	}
}

func round4(v float64) float64 { return math.Round(v*10000) / 10000 }
func vpnLabel(name string) string {
	if strings.TrimSpace(name) == "" {
		return "VPN mesh"
	}
	return name
}

// peerLinks infers inter-server links from stored established-socket telemetry.
func peerLinks(s Store, hosts []store.Host, g *Graph, seen map[string]bool) {
	// Build a map from every known host address to host ID (public, SSH, private,
	// and VPN interface addresses all count as valid peer endpoints).
	addrToHost := map[string]int64{}
	for _, h := range hosts {
		for _, addr := range []string{h.Host, h.PublicIP} {
			addr = strings.TrimSpace(addr)
			if addr != "" {
				addrToHost[addr] = h.ID
			}
		}
		for _, addr := range h.PrivateIPList() {
			addr = strings.TrimSpace(addr)
			if addr != "" {
				addrToHost[addr] = h.ID
			}
		}
	}
	for _, src := range hosts {
		var conns []store.TopConn
		_ = json.Unmarshal([]byte(src.TopConnections), &conns)
		for _, c := range conns {
			remote := strings.TrimSpace(c.RemoteIP)
			targetID, ok := addrToHost[remote]
			if !ok || targetID == src.ID {
				continue
			}
			// Canonical key so A->B and B->A collapse into one link.
			lo, hi := src.ID, targetID
			if lo > hi {
				lo, hi = hi, lo
			}
			key := "tcp-" + strconv.FormatInt(lo, 10) + "-" + strconv.FormatInt(hi, 10)
			if seen[key] {
				continue
			}
			seen[key] = true
			lat := hostLatency(s, lo, hi)
			g.Links = append(g.Links, Link{
				ID: key, SourceID: "host:" + strconv.FormatInt(lo, 10),
				TargetID: "host:" + strconv.FormatInt(hi, 10),
				Type:     "app", Label: "TCP peer", Bidirectional: true,
				LatencyMs: lat, Status: combinedStatus(src.Status, hostByIDStatus(hosts, targetID)), Active: true,
			})
		}
	}
}

func hostByIDStatus(hosts []store.Host, id int64) string {
	for _, h := range hosts {
		if h.ID == id {
			return h.Status
		}
	}
	return "unknown"
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
	case h.GeoLat != 0 || h.GeoLon != 0 || h.GeoLabel != "" || h.GeoRegion != "":
		// Manual override wins for air-gapped and private-IP hosts (ADR 0077).
		n.Lat, n.Lon = h.GeoLat, h.GeoLon
		n.City = h.GeoLabel
		n.Region = h.GeoRegion
		n.GeoSource = "manual"
	default:
		// Read the cached resolution; a stale entry still positions the pin.
		if entry, _, err := s.GetGeoIP(ip, now); err == nil && entry != nil {
			n.Lat, n.Lon = entry.Lat, entry.Lon
			n.City = entry.City
			n.CountryCode = entry.CountryCode
			n.ASNOrg = entry.ASNOrg
			n.Region = entry.Region
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
