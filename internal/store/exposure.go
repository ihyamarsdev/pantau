package store

// CVE Exposure Radar data model (ADR 0088). All types are JSON-serialisable so the whole radar
// result is persisted in one row per host, like security_scans. Nothing here is configuration, so
// the table stays out of the encrypted System Snapshot (re-derivable by a fresh exposure scan).

import "time"

// ExposureSeverity is a radar priority. CRITICAL_EXPOSED is the top tier: a vulnerability on a
// service reachable from the internet (public bind or Cloudflare-proxied).
const (
	ExposureCriticalExposed = "CRITICAL_EXPOSED"
	ExposureCritical        = "critical"
	ExposureHigh            = "high"
	ExposureMedium          = "medium"
	ExposureLow             = "low"
	ExposureUnknown         = "unknown"
)

// ExposureSource identifies where a finding came from.
const (
	ExposureSourceOSV      = "osv"      // OSV / Arch (installed packages)
	ExposureSourceLockfile = "lockfile" // composer.lock / npm lockfile in a repo or webroot
	ExposureSourceRuntime  = "runtime"  // php -v / node -v / nginx -V probe
	ExposureSourceKernel   = "kernel"   // running vs installed kernel drift
	ExposureSourceNginx    = "nginx"    // nginx static hardening check
	ExposureSourceWebshell = "webshell" // parent-child process anomaly
)

// Reachability says how a vulnerable component can be reached from outside.
type Reachability struct {
	PublicBind        bool     `json:"public_bind"`        // listening on 0.0.0.0 / :: / *
	CloudflareProxied bool     `json:"cloudflare_proxied"` // domain routed through a Cloudflare zone
	Domains           []string `json:"domains,omitempty"`
	Upstreams         []string `json:"upstreams,omitempty"`
	Ports             []string `json:"ports,omitempty"`
}

// IsExposed reports whether any internet-facing path reaches the component.
func (r Reachability) IsExposed() bool { return r.PublicBind || r.CloudflareProxied }

// ExposureFinding is one prioritised item on the radar.
type ExposureFinding struct {
	ID             string       `json:"id"`                      // stable key (source + package + advisory)
	Source         string       `json:"source"`                  // ExposureSource*
	Severity       string       `json:"severity"`                // CRITICAL_EXPOSED | critical | high | medium | low | unknown
	BaseSeverity   string       `json:"base_severity,omitempty"` // severity before reachability elevation
	Title          string       `json:"title"`
	Summary        string       `json:"summary,omitempty"`
	Component      string       `json:"component"` // package / runtime / config name
	Version        string       `json:"version,omitempty"`
	FixedVersion   string       `json:"fixed_version,omitempty"`
	CVEs           []string     `json:"cves,omitempty"`
	KEV            bool         `json:"kev,omitempty"`  // in CISA KEV catalogue
	EPSS           float64      `json:"epss,omitempty"` // 0..1
	Reachability   Reachability `json:"reachability"`
	Location       string       `json:"location,omitempty"`        // repo/webroot path, config file:line, etc.
	RebootRequired bool         `json:"reboot_required,omitempty"` // kernel drift: patched kernel installed but not booted
	Mitigations    []string     `json:"mitigations,omitempty"`     // available one-click action ids
}

// IsCriticalExposed reports the top radar tier.
func (f ExposureFinding) IsCriticalExposed() bool { return f.Severity == ExposureCriticalExposed }

// KernelDrift describes a running-vs-installed kernel mismatch.
type KernelDrift struct {
	Running         string `json:"running"`
	InstalledLatest string `json:"installed_latest"`
	RebootRequired  bool   `json:"reboot_required"`
	Note            string `json:"note,omitempty"`
}

// ExposureScan is the whole radar result for one host.
type ExposureScan struct {
	HostID          int64             `json:"host_id"`
	ScannedAt       time.Time         `json:"scanned_at"`
	Status          string            `json:"status"`  // ok | partial | not_assessed | error
	Enabled         bool              `json:"enabled"` // osv_enabled opt-in
	Findings        []ExposureFinding `json:"findings"`
	Kernel          *KernelDrift      `json:"kernel,omitempty"`
	CriticalExposed int               `json:"critical_exposed"`
	Notes           []string          `json:"notes"`
	Error           string            `json:"error,omitempty"`
}
