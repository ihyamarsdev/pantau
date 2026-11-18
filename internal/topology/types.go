// Package topology builds a tiny macro-level fleet graph from data Pantau
// already stores. No SSH, no vendor API, no per-container/per-port rows: the
// global graph carries only infrastructure tiers. Host-level service detail is
// fetched lazily on click via a separate endpoint.
package topology

import "time"

// Tier is the column of the 3-column hybrid CSS-grid layout.
type Tier int

const (
	TierEdge    Tier = 0 // Pantau Gateway, VPN Networks, Cloudflare Tunnels/Zones
	TierCompute Tier = 1 // Hosts / VPS
	TierCluster Tier = 2 // Kubernetes Clusters
)

// NodeKind identifies the macro role of a node.
type NodeKind string

const (
	NodeGateway    NodeKind = "gateway"
	NodeVPN        NodeKind = "vpn"
	NodeCloudflare NodeKind = "cloudflare"
	NodeHost       NodeKind = "host"
	NodeKube       NodeKind = "kube"
)

// Metrics is the tiny CPU/RAM summary shown on a host card.
type Metrics struct {
	CPU    string  `json:"cpu,omitempty"`     // raw 1-minute load, e.g. "0.42, 0.30"
	CPUPct float64 `json:"cpu_pct,omitempty"` // normalized per core, 0-100
	RAMPct float64 `json:"ram_pct,omitempty"` // 0-100
}

// Node is one macro vertex.
type Node struct {
	ID       string   `json:"id"`
	Kind     NodeKind `json:"kind"`
	Label    string   `json:"label"`
	Tier     Tier     `json:"tier"`
	Status   string   `json:"status"` // healthy, degraded, down, unknown
	Group    string   `json:"group,omitempty"`
	Network  string   `json:"network,omitempty"` // VPN name, empty = direct
	HostID   int64    `json:"host_id,omitempty"`
	Alerts   int      `json:"alerts"`
	Metrics  Metrics  `json:"metrics,omitempty"`
	DetailID string   `json:"detail_id,omitempty"` // stable id for lazy detail lookups
}

// Edge connects two macro tiers. Latency/hops come from the persisted probe.
type Edge struct {
	ID        string `json:"id"`
	Source    string `json:"source"`
	Target    string `json:"target"`
	Kind      string `json:"kind"` // direct, vpn, tunnel, cluster
	LatencyMs int64  `json:"latency_ms,omitempty"`
	Hops      int    `json:"hops,omitempty"`
	Active    bool   `json:"active"`
}

// Graph is the entire GET /api/topology payload — intentionally small.
type Graph struct {
	Nodes       []Node         `json:"nodes"`
	Edges       []Edge         `json:"edges"`
	Counts      map[string]int `json:"counts"`
	GeneratedAt time.Time      `json:"generated_at"`
}

// HostDetail is the lazy payload for one host card click. Containers and
// listening ports are loaded only here, never in the global graph.
type HostDetail struct {
	HostID     int64        `json:"host_id"`
	Containers []ServiceRow `json:"containers"`
	Listening  []ServiceRow `json:"listening"`
}

// ServiceRow is one container or listening port in the lazy detail payload.
type ServiceRow struct {
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
	Status string `json:"status,omitempty"`
	Public bool   `json:"public,omitempty"`
}
