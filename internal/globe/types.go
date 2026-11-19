// Package globe builds the geo-topology payload for the 3D WebGL Globe Network
// Radar (ADR 0090). It reuses stored inventory only: no SSH dial and no vendor
// API call happens on the HTTP read path. GeoIP lookups run in a background
// resolver with strict timeouts and rate limiting; results are cached in SQLite
// with a 30-day TTL so an air-gapped Pantau never repeats a lookup.
package globe

import "time"

// Node is one vertex on the globe: the Gateway, a managed Host, or an optional
// Cloudflare/Kubernetes marker.
type Node struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	IP           string  `json:"ip"`
	PublicIP     string  `json:"public_ip"`
	Lat          float64 `json:"lat"`
	Lon          float64 `json:"lon"`
	Region       string  `json:"region,omitempty"`
	City         string  `json:"city"`
	CountryCode  string  `json:"country_code"`
	ASNOrg       string  `json:"asn_org,omitempty"`
	Status       string  `json:"status"` // healthy, degraded, down, unknown
	CPUPct       float64 `json:"cpu_pct"`
	MemPct       float64 `json:"mem_pct"`
	ActiveAlerts int     `json:"active_alerts"`
	LatencyMs    int64   `json:"latency_ms,omitempty"`
	GroupID      string  `json:"group_id"`
	NetworkID    int64   `json:"network_id"`
	Kind         string  `json:"kind"` // gateway, host, cloudflare, kube
	HostID       int64   `json:"host_id,omitempty"`
	GeoSource    string  `json:"geo_source,omitempty"` // manual, geoip, gateway, default
}

// Link is a directed (or bidirectional) inter-server connection drawn as a
// great-circle arc on the globe. SourceID/TargetID reference Node.ID.
type Link struct {
	ID            string `json:"id"`
	SourceID      string `json:"source_id"`
	TargetID      string `json:"target_id"`
	Bidirectional bool   `json:"bidirectional"`
	Type          string `json:"type"` // ssh, app, proxy, vpn, k8s, cloudflare
	LatencyMs     int64  `json:"latency_ms,omitempty"`
	Status        string `json:"status"` // healthy, degraded, down
	Label         string `json:"label,omitempty"`
	Active        bool   `json:"active"`
}

// Graph is the entire GET /api/network-radar/globe payload.
type Graph struct {
	Nodes       []Node    `json:"nodes"`
	Links       []Link    `json:"links"`
	GeneratedAt time.Time `json:"generated_at"`
}
