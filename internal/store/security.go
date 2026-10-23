package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type SecuritySeverity struct {
	Critical  int `json:"critical"`
	Important int `json:"important"`
	Moderate  int `json:"moderate"`
	Low       int `json:"low"`
	Unknown   int `json:"unknown"`
}

type SecurityFinding struct {
	Package          string `json:"package"`
	InstalledVersion string `json:"installed_version"`
	FixedVersion     string `json:"fixed_version"`
	Severity         string `json:"severity"` // critical|important|moderate|low|unknown
	Advisory         string `json:"advisory"` // USN/DSA/RHSA/CVE ids when the package manager exposes them, else ""
}

type HardeningCheck struct {
	ID     string `json:"id"`     // sshd_permit_root_login, sshd_password_auth, auto_security_updates, firewall_active
	Title  string `json:"title"`  // English title; UI shows it as-is
	Status string `json:"status"` // pass|warn|fail|unknown
	Detail string `json:"detail"`
}

type SecurityScan struct {
	HostID            int64             `json:"host_id"`
	ScannedAt         time.Time         `json:"scanned_at"`
	Status            string            `json:"status"`          // ok|partial|unsupported|error
	PackageManager    string            `json:"package_manager"` // apt|dnf|yum|apk|unknown
	OSFamily          string            `json:"os_family"`       // debian|ubuntu|rhel|alpine|unknown (from /etc/os-release ID / ID_LIKE)
	PendingSecurity   int               `json:"pending_security"`
	PendingTotal      int               `json:"pending_total"`
	Severity          SecuritySeverity  `json:"severity"`
	RebootRequired    bool              `json:"reboot_required"`
	RepoCacheAgeHours int               `json:"repo_cache_age_hours"` // -1 = unknown
	Findings          []SecurityFinding `json:"findings"`             // never nil in JSON ([])
	Hardening         []HardeningCheck  `json:"hardening"`            // never nil in JSON ([])
	Notes             []string          `json:"notes"`                // coverage / limitation messages, never nil ([])
	Error             string            `json:"error,omitempty"`
}

// SaveSecurityScan upserts a host's latest security scan result.
func (d *DB) SaveSecurityScan(scan *SecurityScan) error {
	if scan == nil {
		return fmt.Errorf("security scan cannot be nil")
	}

	findings := scan.Findings
	if findings == nil {
		findings = []SecurityFinding{}
	}
	findingsJSON, err := json.Marshal(findings)
	if err != nil {
		return fmt.Errorf("marshal findings: %w", err)
	}

	hardening := scan.Hardening
	if hardening == nil {
		hardening = []HardeningCheck{}
	}
	hardeningJSON, err := json.Marshal(hardening)
	if err != nil {
		return fmt.Errorf("marshal hardening: %w", err)
	}

	notes := scan.Notes
	if notes == nil {
		notes = []string{}
	}
	notesJSON, err := json.Marshal(notes)
	if err != nil {
		return fmt.Errorf("marshal notes: %w", err)
	}

	severityJSON, err := json.Marshal(scan.Severity)
	if err != nil {
		return fmt.Errorf("marshal severity: %w", err)
	}

	rebootInt := 0
	if scan.RebootRequired {
		rebootInt = 1
	}

	scannedAt := scan.ScannedAt
	if scannedAt.IsZero() {
		scannedAt = time.Now()
	}

	query := `
INSERT INTO security_scans (
	host_id, scanned_at, status, package_manager, os_family,
	pending_security, pending_total, severity_json, reboot_required,
	repo_cache_age_hours, findings_json, hardening_json, notes_json, error
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(host_id) DO UPDATE SET
	scanned_at = excluded.scanned_at,
	status = excluded.status,
	package_manager = excluded.package_manager,
	os_family = excluded.os_family,
	pending_security = excluded.pending_security,
	pending_total = excluded.pending_total,
	severity_json = excluded.severity_json,
	reboot_required = excluded.reboot_required,
	repo_cache_age_hours = excluded.repo_cache_age_hours,
	findings_json = excluded.findings_json,
	hardening_json = excluded.hardening_json,
	notes_json = excluded.notes_json,
	error = excluded.error
`
	_, err = d.Exec(query,
		scan.HostID,
		scannedAt,
		scan.Status,
		scan.PackageManager,
		scan.OSFamily,
		scan.PendingSecurity,
		scan.PendingTotal,
		string(severityJSON),
		rebootInt,
		scan.RepoCacheAgeHours,
		string(findingsJSON),
		string(hardeningJSON),
		string(notesJSON),
		scan.Error,
	)
	return err
}

// GetSecurityScan retrieves the latest security scan for a host, or (nil, nil) if none exists.
func (d *DB) GetSecurityScan(hostID int64) (*SecurityScan, error) {
	query := `
SELECT host_id, scanned_at, status, package_manager, os_family,
       pending_security, pending_total, severity_json, reboot_required,
       repo_cache_age_hours, findings_json, hardening_json, notes_json, error
FROM security_scans
WHERE host_id = ?
`
	var (
		scan          SecurityScan
		severityJSON  string
		rebootInt     int
		findingsJSON  string
		hardeningJSON string
		notesJSON     string
		scanErr       sql.NullString
	)

	err := d.QueryRow(query, hostID).Scan(
		&scan.HostID,
		&scan.ScannedAt,
		&scan.Status,
		&scan.PackageManager,
		&scan.OSFamily,
		&scan.PendingSecurity,
		&scan.PendingTotal,
		&severityJSON,
		&rebootInt,
		&scan.RepoCacheAgeHours,
		&findingsJSON,
		&hardeningJSON,
		&notesJSON,
		&scanErr,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query security scan: %w", err)
	}

	scan.RebootRequired = rebootInt == 1
	if scanErr.Valid {
		scan.Error = scanErr.String
	}

	if severityJSON != "" {
		_ = json.Unmarshal([]byte(severityJSON), &scan.Severity)
	}
	if findingsJSON != "" {
		_ = json.Unmarshal([]byte(findingsJSON), &scan.Findings)
	}
	if scan.Findings == nil {
		scan.Findings = []SecurityFinding{}
	}
	if hardeningJSON != "" {
		_ = json.Unmarshal([]byte(hardeningJSON), &scan.Hardening)
	}
	if scan.Hardening == nil {
		scan.Hardening = []HardeningCheck{}
	}
	if notesJSON != "" {
		_ = json.Unmarshal([]byte(notesJSON), &scan.Notes)
	}
	if scan.Notes == nil {
		scan.Notes = []string{}
	}

	return &scan, nil
}

// ListSecurityScanTimes returns a map of host_id to scanned_at for all hosts with recorded security scans.
func (d *DB) ListSecurityScanTimes() (map[int64]time.Time, error) {
	rows, err := d.Query(`SELECT host_id, scanned_at FROM security_scans`)
	if err != nil {
		return nil, fmt.Errorf("list security scan times: %w", err)
	}
	defer rows.Close()

	res := make(map[int64]time.Time)
	for rows.Next() {
		var (
			hostID    int64
			scannedAt time.Time
		)
		if err := rows.Scan(&hostID, &scannedAt); err != nil {
			return nil, fmt.Errorf("scan security scan time: %w", err)
		}
		res[hostID] = scannedAt
	}
	return res, rows.Err()
}
