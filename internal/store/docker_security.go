package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Docker security storage: the container misconfiguration audit and the per-image vulnerability scans of
// the Docker tab. Like security_scans these are telemetry, not configuration, so they stay out of the
// encrypted system snapshot.

// DockerSeverityCounts counts the misconfiguration findings per severity.
type DockerSeverityCounts struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
	Total    int `json:"total"`
}

// DockerSecurityFinding is one container misconfiguration finding.
type DockerSecurityFinding struct {
	Container   string `json:"container"`
	Image       string `json:"image"`
	RuleID      string `json:"rule_id"`
	Severity    string `json:"severity"` // critical|high|medium|low
	Title       string `json:"title"`
	Detail      string `json:"detail"`
	Remediation string `json:"remediation"`
}

// DockerVulnCounts counts the image vulnerabilities per severity.
type DockerVulnCounts struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
	Unknown  int `json:"unknown"`
	Total    int `json:"total"`
}

// DockerImageVuln is one vulnerability of an image.
type DockerImageVuln struct {
	ID        string `json:"id"` // CVE or vendor advisory id
	Package   string `json:"package"`
	Installed string `json:"installed"`
	Fixed     string `json:"fixed"`
	Severity  string `json:"severity"` // critical|high|medium|low|unknown
	Title     string `json:"title"`
}

// DockerSecurityScan is one stored Docker security result: kind `misconfig` (one row per host, the
// container audit) or kind `image` (one row per scanned image, subject = image reference).
type DockerSecurityScan struct {
	ID         int64                   `json:"id"`
	HostID     int64                   `json:"host_id"`
	Kind       string                  `json:"kind"`    // misconfig|image
	Subject    string                  `json:"subject"` // image reference for kind=image
	ScannedAt  time.Time               `json:"scanned_at"`
	Scanner    string                  `json:"scanner"` // trivy|grype (empty for the audit)
	Status     string                  `json:"status"`  // ok|partial|unsupported|error
	Note       string                  `json:"note"`
	Counts     DockerSeverityCounts    `json:"counts"`
	VulnCounts DockerVulnCounts        `json:"vuln_counts"`
	Findings   []DockerSecurityFinding `json:"findings"`
	Vulns      []DockerImageVuln       `json:"vulns"`
	Truncated  bool                    `json:"truncated"`
	Error      string                  `json:"error"`
}

// dockerSecuritySummary is what goes into summary_json.
type dockerSecuritySummary struct {
	Status     string               `json:"status"`
	Note       string               `json:"note"`
	Counts     DockerSeverityCounts `json:"counts"`
	VulnCounts DockerVulnCounts     `json:"vuln_counts"`
	Truncated  bool                 `json:"truncated"`
}

// SaveDockerSecurityScan upserts one Docker security result. The key is (host_id, kind, subject), so
// the audit of a host replaces its previous audit and every image keeps its own row.
func (d *DB) SaveDockerSecurityScan(scan *DockerSecurityScan) error {
	if scan == nil {
		return fmt.Errorf("docker security scan cannot be nil")
	}
	findings := scan.Findings
	if findings == nil {
		findings = []DockerSecurityFinding{}
	}
	findingsJSON, err := json.Marshal(findings)
	if err != nil {
		return fmt.Errorf("marshal docker security findings: %w", err)
	}
	if scan.Kind == "image" {
		vulns := scan.Vulns
		if vulns == nil {
			vulns = []DockerImageVuln{}
		}
		findingsJSON, err = json.Marshal(vulns)
		if err != nil {
			return fmt.Errorf("marshal docker image vulns: %w", err)
		}
	}
	summaryJSON, err := json.Marshal(dockerSecuritySummary{
		Status:     scan.Status,
		Note:       scan.Note,
		Counts:     scan.Counts,
		VulnCounts: scan.VulnCounts,
		Truncated:  scan.Truncated,
	})
	if err != nil {
		return fmt.Errorf("marshal docker security summary: %w", err)
	}
	scannedAt := scan.ScannedAt
	if scannedAt.IsZero() {
		scannedAt = time.Now()
	}
	query := `
INSERT INTO docker_security_scans (
	host_id, kind, subject, scanned_at, scanner, summary_json, findings_json, error
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(host_id, kind, subject) DO UPDATE SET
	scanned_at = excluded.scanned_at,
	scanner = excluded.scanner,
	summary_json = excluded.summary_json,
	findings_json = excluded.findings_json,
	error = excluded.error
`
	_, err = d.Exec(query, scan.HostID, scan.Kind, scan.Subject, scannedAt, scan.Scanner,
		string(summaryJSON), string(findingsJSON), scan.Error)
	return err
}

// GetDockerSecurityScan returns one stored result (host, kind, subject), or (nil, nil) when there is none.
func (d *DB) GetDockerSecurityScan(hostID int64, kind, subject string) (*DockerSecurityScan, error) {
	query := `
SELECT id, host_id, kind, subject, scanned_at, scanner, summary_json, findings_json, error
FROM docker_security_scans
WHERE host_id = ? AND kind = ? AND subject = ?
`
	row := d.QueryRow(query, hostID, kind, subject)
	scan, err := scanDockerSecurityRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return scan, nil
}

// ListDockerSecurityScans returns every stored Docker security result of a host (audit and image scans).
func (d *DB) ListDockerSecurityScans(hostID int64) ([]DockerSecurityScan, error) {
	query := `
SELECT id, host_id, kind, subject, scanned_at, scanner, summary_json, findings_json, error
FROM docker_security_scans
WHERE host_id = ?
ORDER BY kind, subject
`
	rows, err := d.Query(query, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	scans := []DockerSecurityScan{}
	for rows.Next() {
		scan, err := scanDockerSecurityRow(rows)
		if err != nil {
			return nil, err
		}
		scans = append(scans, *scan)
	}
	return scans, rows.Err()
}

// dockerSecRow is implemented by *sql.Row and *sql.Rows.
type dockerSecRow interface {
	Scan(dest ...any) error
}

func scanDockerSecurityRow(row dockerSecRow) (*DockerSecurityScan, error) {
	var (
		scan         DockerSecurityScan
		summaryJSON  string
		findingsJSON string
	)
	if err := row.Scan(&scan.ID, &scan.HostID, &scan.Kind, &scan.Subject, &scan.ScannedAt,
		&scan.Scanner, &summaryJSON, &findingsJSON, &scan.Error); err != nil {
		return nil, err
	}
	var summary dockerSecuritySummary
	if summaryJSON != "" {
		_ = json.Unmarshal([]byte(summaryJSON), &summary)
	}
	scan.Status = summary.Status
	scan.Note = summary.Note
	scan.Counts = summary.Counts
	scan.VulnCounts = summary.VulnCounts
	scan.Truncated = summary.Truncated
	scan.Findings = []DockerSecurityFinding{}
	scan.Vulns = []DockerImageVuln{}
	if findingsJSON == "" {
		return &scan, nil
	}
	if scan.Kind == "image" {
		if err := json.Unmarshal([]byte(findingsJSON), &scan.Vulns); err != nil {
			return nil, fmt.Errorf("decode docker image vulns: %w", err)
		}
		return &scan, nil
	}
	if err := json.Unmarshal([]byte(findingsJSON), &scan.Findings); err != nil {
		return nil, fmt.Errorf("decode docker security findings: %w", err)
	}
	return &scan, nil
}
