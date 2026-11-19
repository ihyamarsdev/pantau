package store

// Persistence for CVE Exposure Radar results (ADR 0088). One row per host, like security_scans.
// Derived telemetry: stays out of the encrypted System Snapshot.

import (
	"database/sql"
	"encoding/json"
	"time"
)

// SaveExposureScan upserts a host's latest exposure radar result.
func (d *DB) SaveExposureScan(scan *ExposureScan) error {
	findings, _ := json.Marshal(scan.Findings)
	notes, _ := json.Marshal(scan.Notes)
	kernel := []byte("")
	if scan.Kernel != nil {
		kernel, _ = json.Marshal(scan.Kernel)
	}
	enabled := 0
	if scan.Enabled {
		enabled = 1
	}
	_, err := d.Exec(`INSERT INTO exposure_scans
		(host_id, scanned_at, status, enabled, findings_json, kernel_json, critical_exposed, notes_json, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(host_id) DO UPDATE SET
		scanned_at=excluded.scanned_at, status=excluded.status, enabled=excluded.enabled,
		findings_json=excluded.findings_json, kernel_json=excluded.kernel_json,
		critical_exposed=excluded.critical_exposed, notes_json=excluded.notes_json, error=excluded.error`,
		scan.HostID, scan.ScannedAt, scan.Status, enabled, string(findings), string(kernel),
		scan.CriticalExposed, string(notes), scan.Error)
	return err
}

// GetExposureScan retrieves a host's latest exposure radar result, or (nil, nil) if none exists.
func (d *DB) GetExposureScan(hostID int64) (*ExposureScan, error) {
	var (
		s         ExposureScan
		enabled   int
		findings  string
		kernel    string
		notes     string
		scannedAt time.Time
	)
	err := d.QueryRow(`SELECT host_id, scanned_at, status, enabled, findings_json, kernel_json, critical_exposed, notes_json, error
		FROM exposure_scans WHERE host_id = ?`, hostID).Scan(
		&s.HostID, &scannedAt, &s.Status, &enabled, &findings, &kernel, &s.CriticalExposed, &notes, &s.Error)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.ScannedAt = scannedAt
	s.Enabled = enabled == 1
	s.Findings = []ExposureFinding{}
	_ = json.Unmarshal([]byte(findings), &s.Findings)
	s.Notes = []string{}
	_ = json.Unmarshal([]byte(notes), &s.Notes)
	if kernel != "" {
		s.Kernel = &KernelDrift{}
		_ = json.Unmarshal([]byte(kernel), s.Kernel)
	}
	return &s, nil
}

// ListExposureScans returns every host's stored exposure radar result (for fleet-wide views and
// the AI agent's get_exposed_cves tool). Hosts without a scan are omitted.
func (d *DB) ListExposureScans() ([]ExposureScan, error) {
	rows, err := d.Query(`SELECT host_id, scanned_at, status, enabled, findings_json, kernel_json, critical_exposed, notes_json, error
		FROM exposure_scans ORDER BY critical_exposed DESC, scanned_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExposureScan
	for rows.Next() {
		var s ExposureScan
		var enabled int
		var findings, kernel, notes string
		if err := rows.Scan(&s.HostID, &s.ScannedAt, &s.Status, &enabled, &findings, &kernel, &s.CriticalExposed, &notes, &s.Error); err != nil {
			return nil, err
		}
		s.Enabled = enabled == 1
		s.Findings = []ExposureFinding{}
		_ = json.Unmarshal([]byte(findings), &s.Findings)
		s.Notes = []string{}
		_ = json.Unmarshal([]byte(notes), &s.Notes)
		if kernel != "" {
			s.Kernel = &KernelDrift{}
			_ = json.Unmarshal([]byte(kernel), s.Kernel)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
