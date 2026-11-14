package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// IacScan is the latest IaC/secret scan of one source (a cloned repo, a Terraform workspace or an Ansible project).
// It holds scan results only; nothing here is configuration, so it stays out of the System Snapshot.
type IacScan struct {
	SourceKind string          `json:"source_kind"` // "repo" | "terraform" | "ansible"
	SourceID   int64           `json:"source_id"`
	ScannedAt  time.Time       `json:"scanned_at"`
	Summary    json.RawMessage `json:"summary"`
	Findings   json.RawMessage `json:"findings"`
	Status     string          `json:"status"`
	Files      int             `json:"files_scanned"`
	Truncated  bool            `json:"truncated"`
	Root       string          `json:"root"`
	Notes      json.RawMessage `json:"notes"`
}

// ErrIacScanNotFound is returned when a source has no scan yet.
var ErrIacScanNotFound = errors.New("iac scan not found")

// SaveIacScan upserts the latest scan of a source.
func (d *DB) SaveIacScan(s *IacScan) error {
	if s == nil {
		return errors.New("iac scan cannot be nil")
	}
	if s.SourceKind == "" || s.SourceID <= 0 {
		return errors.New("iac scan needs a source kind and id")
	}
	at := s.ScannedAt
	if at.IsZero() {
		at = time.Now().UTC()
	}
	_, err := d.Exec(`INSERT INTO iac_scans (source_kind, source_id, scanned_at, summary_json, findings_json, status, files_scanned, truncated, root, notes_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source_kind, source_id) DO UPDATE SET
			scanned_at = excluded.scanned_at,
			summary_json = excluded.summary_json,
			findings_json = excluded.findings_json,
			status = excluded.status,
			files_scanned = excluded.files_scanned,
			truncated = excluded.truncated,
			root = excluded.root,
			notes_json = excluded.notes_json`,
		s.SourceKind, s.SourceID, at, string(s.Summary), string(s.Findings), s.Status, s.Files, s.Truncated, s.Root, string(s.Notes))
	return err
}

// GetIacScan returns the latest scan of a source, or (nil, nil) when there is none.
func (d *DB) GetIacScan(kind string, id int64) (*IacScan, error) {
	var s IacScan
	var summary, findings, notes string
	err := d.QueryRow(`SELECT source_kind, source_id, scanned_at, summary_json, findings_json, status, files_scanned, truncated, root, notes_json
		FROM iac_scans WHERE source_kind = ? AND source_id = ?`, kind, id).
		Scan(&s.SourceKind, &s.SourceID, &s.ScannedAt, &summary, &findings, &s.Status, &s.Files, &s.Truncated, &s.Root, &notes)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query iac scan: %w", err)
	}
	s.Summary = json.RawMessage(summary)
	s.Findings = json.RawMessage(findings)
	s.Notes = json.RawMessage(notes)
	return &s, nil
}

// ListIacScans returns the latest scan of every source, newest first.
func (d *DB) ListIacScans() ([]IacScan, error) {
	rows, err := d.Query(`SELECT source_kind, source_id, scanned_at, summary_json, findings_json, status, files_scanned, truncated, root, notes_json
		FROM iac_scans ORDER BY scanned_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list iac scans: %w", err)
	}
	defer rows.Close()
	out := []IacScan{}
	for rows.Next() {
		var s IacScan
		var summary, findings, notes string
		if err := rows.Scan(&s.SourceKind, &s.SourceID, &s.ScannedAt, &summary, &findings, &s.Status, &s.Files, &s.Truncated, &s.Root, &notes); err != nil {
			return nil, err
		}
		s.Summary = json.RawMessage(summary)
		s.Findings = json.RawMessage(findings)
		s.Notes = json.RawMessage(notes)
		out = append(out, s)
	}
	return out, rows.Err()
}

// DeleteIacScan removes the scan of a source (used when the source is deleted).
func (d *DB) DeleteIacScan(kind string, id int64) error {
	_, err := d.Exec(`DELETE FROM iac_scans WHERE source_kind = ? AND source_id = ?`, kind, id)
	return err
}
