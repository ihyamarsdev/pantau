package store

// Offline-safe GeoIP cache (ADR 0090). External lookups are persisted with a
// 30-day TTL so an air-gapped Pantau never repeats a lookup it already made.
// The resolver itself lives in internal/globe; this file is only the store.

import (
	"database/sql"
	"strings"
	"time"
)

// GeoIPEntry is one cached IP -> location resolution.
type GeoIPEntry struct {
	IP          string    `json:"ip"`
	Lat         float64   `json:"lat"`
	Lon         float64   `json:"lon"`
	City        string    `json:"city"`
	CountryCode string    `json:"country_code"`
	ASNOrg      string    `json:"asn_org"`
	Source      string    `json:"source"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// GeoIPCacheTTL is how long a cached resolution stays fresh before a refresh
// is attempted. A refresh that fails keeps serving the stale entry.
const GeoIPCacheTTL = 30 * 24 * time.Hour

// GetGeoIP returns a cached entry and whether it is still fresh (age < TTL).
// A stale entry is still returned so callers can degrade gracefully offline.
func (d *DB) GetGeoIP(ip string, now time.Time) (*GeoIPEntry, bool, error) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return nil, false, nil
	}
	var e GeoIPEntry
	err := d.QueryRow(`SELECT ip, lat, lon, city, country_code, asn_org, source, updated_at FROM geoip_cache WHERE ip = ?`, ip).
		Scan(&e.IP, &e.Lat, &e.Lon, &e.City, &e.CountryCode, &e.ASNOrg, &e.Source, &e.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &e, now.Sub(e.UpdatedAt) < GeoIPCacheTTL, nil
}

// UpsertGeoIP inserts or replaces a cached resolution. updated_at is set to now
// so a successful lookup resets the 30-day TTL.
func (d *DB) UpsertGeoIP(e GeoIPEntry, now time.Time) error {
	e.IP = strings.TrimSpace(e.IP)
	if e.IP == "" {
		return nil
	}
	_, err := d.Exec(`INSERT INTO geoip_cache (ip, lat, lon, city, country_code, asn_org, source, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(ip) DO UPDATE SET lat=excluded.lat, lon=excluded.lon, city=excluded.city,
		country_code=excluded.country_code, asn_org=excluded.asn_org, source=excluded.source, updated_at=excluded.updated_at`,
		e.IP, e.Lat, e.Lon, e.City, e.CountryCode, e.ASNOrg, e.Source, now.UTC())
	return err
}

// DeleteStaleGeoIP removes resolutions older than the TTL. Called occasionally
// by the background resolver; a failed refresh leaves the stale entry in place
// until this runs, so stale rows are never silently dropped while offline.
func (d *DB) DeleteStaleGeoIP(now time.Time) error {
	_, err := d.Exec(`DELETE FROM geoip_cache WHERE updated_at < ?`, now.Add(-GeoIPCacheTTL).UTC())
	return err
}

// --- Persisted netdiag probe results ---------------------------------------

// ProbeResult is one measured latency between a source and a target host.
// source_host_id = 0 means the Pantau Gateway itself.
type ProbeResult struct {
	SourceHostID int64     `json:"source_host_id"`
	TargetHostID int64     `json:"target_host_id"`
	LatencyMs    float64   `json:"latency_ms"`
	Status       string    `json:"status"`
	ProbedAt     time.Time `json:"probed_at"`
}

// GetProbeLatency returns the last measured latency and status for a pair.
func (d *DB) GetProbeLatency(sourceHostID, targetHostID int64) (*ProbeResult, error) {
	var p ProbeResult
	err := d.QueryRow(`SELECT source_host_id, target_host_id, latency_ms, status, probed_at FROM probe_results WHERE source_host_id=? AND target_host_id=?`, sourceHostID, targetHostID).
		Scan(&p.SourceHostID, &p.TargetHostID, &p.LatencyMs, &p.Status, &p.ProbedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpsertProbeResult stores a measured latency, replacing any previous value.
func (d *DB) UpsertProbeResult(p ProbeResult, now time.Time) error {
	_, err := d.Exec(`INSERT INTO probe_results (source_host_id, target_host_id, latency_ms, status, probed_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(source_host_id, target_host_id) DO UPDATE SET latency_ms=excluded.latency_ms, status=excluded.status, probed_at=excluded.probed_at`,
		p.SourceHostID, p.TargetHostID, p.LatencyMs, p.Status, now.UTC())
	return err
}
