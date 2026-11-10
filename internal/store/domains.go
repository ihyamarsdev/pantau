package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Web domains: the reverse proxies a host serves (nginx, Caddy, Traefik), stored
// per host so the fleet view and the fleet search answer without SSH.
//
// Two tables:
//   - web_scans   : one row per host, when it was last scanned and what the scan found (proxies, warnings)
//   - host_domains: one row per domain served by a host (the full site as JSON)
//
// Both tables are NOT part of the System Snapshot: they are re-derivable by a
// fresh scan and must not bloat the encrypted backup.

// WebProxy is one reverse proxy found on a host.
type WebProxy struct {
	Kind      string `json:"kind"`      // nginx | caddy | traefik
	Where     string `json:"where"`     // host | container
	Container string `json:"container"` // container name, empty for the host
	Version   string `json:"version"`
	Method    string `json:"method"`
	Error     string `json:"error"`
}

// WebSite is one domain served by a reverse proxy.
type WebSite struct {
	Domain        string   `json:"domain"`
	Proxy         string   `json:"proxy"` // nginx | caddy | traefik
	Where         string   `json:"where"` // host | container
	Container     string   `json:"container"`
	TLS           bool     `json:"tls"`
	Listen        []string `json:"listen"`
	Upstreams     []string `json:"upstreams"`
	Root          string   `json:"root"`
	Source        string   `json:"source"`
	Wildcard      bool     `json:"wildcard"`
	Regex         bool     `json:"regex"`
	DefaultServer bool     `json:"default_server"`
}

// WebScan is the result of one domain scan of a host.
type WebScan struct {
	HostID    int64      `json:"host_id"`
	ScannedAt time.Time  `json:"scanned_at"`
	Proxies   []WebProxy `json:"proxies"`
	Sites     []WebSite  `json:"sites"`
	Warnings  []string   `json:"warnings"`
}

// HostDomainSite is one stored site of a host with the host context.
type HostDomainSite struct {
	WebSite
	HostID   int64  `json:"host_id"`
	HostName string `json:"host_name"`
	Group    string `json:"group"`
}

// HostDomainSummary is the per-host summary of the fleet domain view.
type HostDomainSummary struct {
	HostID     int64  `json:"host_id"`
	HostName   string `json:"host_name"`
	Group      string `json:"group"`
	ScannedAt  string `json:"scanned_at"`
	SiteCount  int    `json:"site_count"`
	ProxyCount int    `json:"proxy_count"`
	ErrorCount int    `json:"error_count"`
}

// ReplaceWebScan stores the result of one domain scan of a host in one
// transaction: the scan row, the proxies and the domains are replaced together
// so the stored data is never half updated.
func (d *DB) ReplaceWebScan(hostID int64, scan *WebScan) error {
	if scan == nil {
		return fmt.Errorf("nil web scan")
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM host_domains WHERE host_id = ?`, hostID); err != nil {
		return err
	}

	scannedAt := scan.ScannedAt
	if scannedAt.IsZero() {
		scannedAt = time.Now()
	}
	proxies, err := json.Marshal(nonNilProxies(scan.Proxies))
	if err != nil {
		return err
	}
	warnings, err := json.Marshal(nonNilStrings(scan.Warnings))
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO web_scans (host_id, scanned_at, proxies, warnings) VALUES (?, ?, ?, ?)
		ON CONFLICT(host_id) DO UPDATE SET scanned_at = excluded.scanned_at, proxies = excluded.proxies, warnings = excluded.warnings`,
		hostID, scannedAt, string(proxies), string(warnings)); err != nil {
		return err
	}

	stmt, err := tx.Prepare(`INSERT INTO host_domains (host_id, domain, proxy, location, container, tls, data)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, s := range scan.Sites {
		data, err := json.Marshal(s)
		if err != nil {
			return err
		}
		if _, err := stmt.Exec(hostID, s.Domain, s.Proxy, s.Where, s.Container, boolToInt(s.TLS), string(data)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetWebScan returns the stored scan of a host. The second return value is
// false when the host was never scanned.
func (d *DB) GetWebScan(hostID int64) (*WebScan, bool, error) {
	var (
		scannedAt time.Time
		proxies   string
		warnings  string
	)
	err := d.QueryRow(`SELECT scanned_at, proxies, warnings FROM web_scans WHERE host_id = ?`, hostID).
		Scan(&scannedAt, &proxies, &warnings)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	scan := &WebScan{HostID: hostID, ScannedAt: scannedAt}
	if proxies != "" {
		_ = json.Unmarshal([]byte(proxies), &scan.Proxies)
	}
	if warnings != "" {
		_ = json.Unmarshal([]byte(warnings), &scan.Warnings)
	}
	return scan, true, nil
}

// GetHostDomains returns the stored sites of one host, sorted by domain.
func (d *DB) GetHostDomains(hostID int64) ([]WebSite, error) {
	rows, err := d.Query(`SELECT data FROM host_domains WHERE host_id = ? ORDER BY domain, proxy, container`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WebSite
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var s WebSite
		if err := json.Unmarshal([]byte(data), &s); err != nil {
			continue
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListHostDomains returns the stored sites of every host, sorted by domain
// then host name.
func (d *DB) ListHostDomains() ([]HostDomainSite, error) {
	rows, err := d.Query(`SELECT d.host_id, d.data, h.name, h.group_name
		FROM host_domains d JOIN hosts h ON h.id = d.host_id
		ORDER BY d.domain, h.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HostDomainSite
	for rows.Next() {
		var (
			hostID    int64
			data      string
			hostName  string
			groupName string
		)
		if err := rows.Scan(&hostID, &data, &hostName, &groupName); err != nil {
			return nil, err
		}
		var s WebSite
		if err := json.Unmarshal([]byte(data), &s); err != nil {
			continue
		}
		out = append(out, HostDomainSite{WebSite: s, HostID: hostID, HostName: hostName, Group: groupName})
	}
	return out, rows.Err()
}

// ListHostDomainSummaries returns one row per host with its domain coverage.
// Hosts that were never scanned are included with an empty scanned_at.
func (d *DB) ListHostDomainSummaries() ([]HostDomainSummary, error) {
	rows, err := d.Query(`SELECT h.id, h.name, h.group_name, s.scanned_at, s.proxies,
		(SELECT COUNT(*) FROM host_domains d WHERE d.host_id = h.id)
		FROM hosts h LEFT JOIN web_scans s ON s.host_id = h.id
		ORDER BY h.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HostDomainSummary
	for rows.Next() {
		var (
			id        int64
			name      string
			groupName string
			scannedAt sql.NullTime
			proxies   sql.NullString
			sites     int
		)
		if err := rows.Scan(&id, &name, &groupName, &scannedAt, &proxies, &sites); err != nil {
			return nil, err
		}
		s := HostDomainSummary{
			HostID:    id,
			HostName:  name,
			Group:     groupName,
			SiteCount: sites,
		}
		if scannedAt.Valid {
			s.ScannedAt = scannedAt.Time.UTC().Format(time.RFC3339)
		}
		var list []WebProxy
		if proxies.Valid && proxies.String != "" {
			_ = json.Unmarshal([]byte(proxies.String), &list)
		}
		s.ProxyCount = len(list)
		for _, p := range list {
			if p.Error != "" {
				s.ErrorCount++
			}
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CountUnscannedHosts returns the number of hosts that were never scanned for domains.
func (d *DB) CountUnscannedHosts() (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM hosts h WHERE NOT EXISTS (SELECT 1 FROM web_scans s WHERE s.host_id = h.id)`).Scan(&n)
	return n, err
}

// CountHostsWithoutDomains returns the number of hosts that have no stored domain.
func (d *DB) CountHostsWithoutDomains() (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM hosts h WHERE NOT EXISTS (SELECT 1 FROM host_domains d WHERE d.host_id = h.id)`).Scan(&n)
	return n, err
}

// fleetSearchDomains matches the stored domains. Every token must match the
// domain name. The hosts of a domain are listed with a short detail line
// (proxy · where → first upstream).
func (d *DB) fleetSearchDomains(b *fleetGroupBuilder, tokens []string) error {
	where, args := tokenCondition([]string{"d.domain"}, tokens)
	rows, err := d.Query(`SELECT d.host_id, d.domain, d.proxy, d.location, d.container, d.data, s.scanned_at, h.name, h.group_name
		FROM host_domains d JOIN hosts h ON h.id = d.host_id
		LEFT JOIN web_scans s ON s.host_id = d.host_id
		WHERE `+where+` ORDER BY d.domain, h.name`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			hostID    int64
			domain    string
			proxy     string
			location  string
			container string
			data      string
			scannedAt sql.NullTime
			hostName  string
			groupName string
		)
		if err := rows.Scan(&hostID, &domain, &proxy, &location, &container, &data, &scannedAt, &hostName, &groupName); err != nil {
			return err
		}
		var age *time.Time
		if scannedAt.Valid {
			t := scannedAt.Time.UTC()
			age = &t
		}
		b.addKeyed(FleetSearchDomain, FleetSearchDomain+"\x00"+domain, domain, "", FleetSearchHost{
			HostID: hostID, Name: hostName, Group: groupName, DataAge: age,
			Detail: domainDetail(proxy, location, container, data),
		})
	}
	return rows.Err()
}

// domainDetail renders the short detail line of a domain host row:
// "nginx · host → http://127.0.0.1:3000".
func domainDetail(proxy, location, container, data string) string {
	where := location
	if where == "" {
		where = "host"
	}
	if container != "" {
		where = where + ":" + container
	}
	detail := proxy + " · " + where
	var s WebSite
	if data != "" && json.Unmarshal([]byte(data), &s) == nil && len(s.Upstreams) > 0 {
		detail += " → " + s.Upstreams[0]
	}
	return detail
}

func nonNilProxies(p []WebProxy) []WebProxy {
	if p == nil {
		return []WebProxy{}
	}
	return p
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
