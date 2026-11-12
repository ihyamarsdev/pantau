package netdiag

import (
	"time"
)

// ProbeType defines the specific diagnostic probe kind.
type ProbeType string

const (
	ProbeTypePing       ProbeType = "ping"
	ProbeTypeTraceroute ProbeType = "traceroute"
	ProbeTypePort       ProbeType = "port_probe"
	ProbeTypeDNS        ProbeType = "dns_resolve"
	ProbeTypeDig        ProbeType = "dig"
)

// ProbeRequest specifies parameters for a network diagnostic run.
type ProbeRequest struct {
	SourceHostID  int64     `json:"source_host_id"`
	Type          ProbeType `json:"type"`
	TargetType    string    `json:"target_type"` // "host" or "custom"
	TargetHostID  int64     `json:"target_host_id,omitempty"`
	TargetAddress string    `json:"target_address"`
	Port          int       `json:"port,omitempty"`        // For port_probe
	RecordType    string    `json:"record_type,omitempty"` // For dig: A, AAAA, CNAME, MX, NS, TXT, SOA, PTR
	DNSServer     string    `json:"dns_server,omitempty"`  // For dig: optional resolver, empty = host's system resolver
}

// HopInfo represents an individual hop in a traceroute path.
type HopInfo struct {
	Hop    int     `json:"hop"`
	Host   string  `json:"host"`
	IP     string  `json:"ip"`
	RTTMs  float64 `json:"rtt_ms"`
	Zone   string  `json:"zone"`   // "private", "transit", "destination", "timeout"
	Status string  `json:"status"` // "ok", "warning", "timeout"
}

// HopAnalysis represents the automated heuristic diagnosis of a traceroute run.
type HopAnalysis struct {
	Summary          string    `json:"summary"`
	SummaryID        string    `json:"summary_id"` // Indonesian translation
	Status           string    `json:"status"`     // "optimal", "degraded", "failed"
	TotalHops        int       `json:"total_hops"`
	FinalRTTMs       float64   `json:"final_rtt_ms"`
	DestinationFound bool      `json:"destination_found"`
	DropAtHop        int       `json:"drop_at_hop,omitempty"`
	LatencySpikeHop  int       `json:"latency_spike_hop,omitempty"`
	GeneratedAt      time.Time `json:"generated_at"`
}

// PingStats captures parsed ICMP ping statistics.
type PingStats struct {
	PacketsTransmitted int     `json:"packets_transmitted"`
	PacketsReceived    int     `json:"packets_received"`
	PacketLossPct      float64 `json:"packet_loss_pct"`
	MinMs              float64 `json:"min_ms"`
	AvgMs              float64 `json:"avg_ms"`
	MaxMs              float64 `json:"max_ms"`
	Summary            string  `json:"summary"`
	SummaryID          string  `json:"summary_id"`
	Status             string  `json:"status"` // "healthy", "degraded", "down"
}

// PortStats captures TCP port reachability status.
type PortStats struct {
	Port      int    `json:"port"`
	Open      bool   `json:"open"`
	Summary   string `json:"summary"`
	SummaryID string `json:"summary_id"`
	Status    string `json:"status"` // "healthy", "down"
}

// DNSStats captures DNS resolution results.
type DNSStats struct {
	Target      string   `json:"target"`
	ResolvedIPs []string `json:"resolved_ips"`
	Summary     string   `json:"summary"`
	SummaryID   string   `json:"summary_id"`
	Status      string   `json:"status"` // "healthy", "down"
}

// DigRecord is one resource record from the dig answer section.
type DigRecord struct {
	Name  string `json:"name"`
	TTL   int    `json:"ttl"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

// DigStats captures a parsed dig query.
type DigStats struct {
	Target      string      `json:"target"`
	RecordType  string      `json:"record_type"`
	Server      string      `json:"server"` // resolver that answered, e.g. "1.1.1.1"
	RCode       string      `json:"rcode"`  // NOERROR, NXDOMAIN, SERVFAIL, ...
	QueryTimeMs int         `json:"query_time_ms"`
	Answers     []DigRecord `json:"answers"`
	Summary     string      `json:"summary"`
	SummaryID   string      `json:"summary_id"`
	Status      string      `json:"status"` // "healthy", "degraded", "down"
}

// ProbeEvent represents an SSE event streamed to the browser.
type ProbeEvent struct {
	Type     string       `json:"type"` // "progress", "raw", "hop", "ping_stats", "port_stats", "dns_stats", "dig_stats", "hop_analysis", "error", "done"
	RawLine  string       `json:"raw_line,omitempty"`
	Hop      *HopInfo     `json:"hop,omitempty"`
	Ping     *PingStats   `json:"ping,omitempty"`
	Port     *PortStats   `json:"port,omitempty"`
	DNS      *DNSStats    `json:"dns,omitempty"`
	Dig      *DigStats    `json:"dig,omitempty"`
	Analysis *HopAnalysis `json:"analysis,omitempty"`
	Message  string       `json:"message,omitempty"`
}
