package store

// Unified Security Center overview (ADR 0089). One bounded SQLite pass computes the posture
// score, the six breakdown counters, per-pillar badge counts and a prioritised action queue.
// No SSH, no network, no external feeds: every number comes from already-stored telemetry.

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// Security pillar identifiers. Stable across i18n; the UI maps them to labels.
const (
	PillarOverview        = "overview"
	PillarVulnerabilities = "vulnerabilities"
	PillarNetwork         = "network"
	PillarContainers      = "containers"
	PillarIdentity        = "identity"
	PillarCode            = "code"
	PillarExposure        = "exposure"
)

// SecurityBreakdown holds the six headline counters of the posture strip.
type SecurityBreakdown struct {
	KEVCritical       int `json:"kev_critical"`
	FIMDrift          int `json:"fim_drift"`
	LeakedSecrets     int `json:"leaked_secrets"`
	PendingSecUpdates int `json:"pending_sec_updates"`
	ContainerIssues   int `json:"container_issues"`
	FirewallWarnings  int `json:"firewall_warnings"`
}

// SecurityAction is one item of the prioritised Top Action Queue.
type SecurityAction struct {
	ID          string          `json:"id"`
	Pillar      string          `json:"pillar"`
	Severity    string          `json:"severity"`
	Title       string          `json:"title"`
	Detail      string          `json:"detail"`
	HostID      int64           `json:"host_id"`
	HostName    string          `json:"host_name"`
	Priority    float64         `json:"priority"`
	Evidence    json.RawMessage `json:"evidence,omitempty"`
	Remediation string          `json:"remediation,omitempty"`
}

// SecurityOverview is the whole aggregated response of GET /api/security/overview.
type SecurityOverview struct {
	Score       int               `json:"score"`
	Grade       string            `json:"grade"`
	Breakdown   SecurityBreakdown `json:"breakdown"`
	Pillars     map[string]int    `json:"pillars"`
	TopActions  []SecurityAction  `json:"top_actions"`
	HostCount   int               `json:"host_count"`
	GeneratedAt time.Time         `json:"generated_at"`
}

// minInt is a tiny helper kept local to avoid pulling math into the store package.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ScoreSecurityPosture maps the six breakdown counters to a 0-100 score and a letter grade.
// It is a pure function so the weighting is easy to unit-test and reason about. KEV and leaked
// secrets dominate; stale updates and firewall warnings are lighter signals.
func ScoreSecurityPosture(b SecurityBreakdown) (int, string) {
	deduction := minInt(b.KEVCritical, 10)*8 +
		minInt(b.LeakedSecrets, 10)*6 +
		minInt(b.FIMDrift, 10)*3 +
		minInt(b.ContainerIssues, 10)*3 +
		minInt(b.FirewallWarnings, 10)*2 +
		minInt(b.PendingSecUpdates, 20)*1
	score := 100 - deduction
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	grade := "F"
	switch {
	case score >= 90:
		grade = "A"
	case score >= 80:
		grade = "B"
	case score >= 70:
		grade = "C"
	case score >= 60:
		grade = "D"
	}
	return score, grade
}

// severityRank orders finding severities from most to least urgent.
func severityRank(s string) int {
	switch strings.ToLower(s) {
	case "critical", "critical_exposed":
		return 5
	case "high", "important":
		return 4
	case "medium", "moderate":
		return 3
	case "low":
		return 2
	default:
		return 1
	}
}

// scopeClause builds the shared WHERE fragment that limits every query to the requested scope.
// hostID == 0 means all hosts; groupName == "" means all groups. Both may be combined.
func scopeClause(hostID int64, groupName string) (string, []any) {
	var b strings.Builder
	b.WriteString("SELECT id FROM hosts WHERE 1=1")
	args := []any{}
	if hostID > 0 {
		b.WriteString(" AND id = ?")
		args = append(args, hostID)
	}
	if groupName != "" {
		b.WriteString(" AND group_name = ?")
		args = append(args, groupName)
	}
	return b.String(), args
}

// GetSecurityOverview aggregates all security telemetry for a scope in one bounded SQLite pass.
// hostID == 0 means the whole fleet; groupName == "" means all groups. IaC/secret findings live
// on the Pantau server and are only folded in for the fleet scope (they have no host id).
func (d *DB) GetSecurityOverview(hostID int64, groupName string) (*SecurityOverview, error) {
	scopeSQL, scopeArgs := scopeClause(hostID, groupName)

	var hostCount int
	if err := d.QueryRow(`SELECT COUNT(*) FROM (`+scopeSQL+`)`, scopeArgs...).Scan(&hostCount); err != nil {
		return nil, err
	}

	ov := &SecurityOverview{
		Pillars:     map[string]int{PillarOverview: 0, PillarVulnerabilities: 0, PillarNetwork: 0, PillarContainers: 0, PillarIdentity: 0, PillarCode: 0, PillarExposure: 0},
		TopActions:  []SecurityAction{},
		HostCount:   hostCount,
		GeneratedAt: time.Now().UTC(),
	}
	var actions []SecurityAction
	hostNames := map[int64]string{}
	rows, err := d.Query(`SELECT id, name FROM (`+scopeSQL+`)`, scopeArgs...)
	if err == nil {
		for rows.Next() {
			var id int64
			var name string
			if rows.Scan(&id, &name) == nil {
				hostNames[id] = name
			}
		}
		rows.Close()
	}

	// --- Pending security updates + firewall hardening warnings (security_scans) ---
	scanRows, err := d.Query(`SELECT s.host_id, s.pending_security, COALESCE(s.hardening_json,'[]'), COALESCE(s.findings_json,'[]')
		FROM security_scans s WHERE s.host_id IN (`+scopeSQL+`)`, scopeArgs...)
	if err != nil {
		return nil, err
	}
	for scanRows.Next() {
		var hid int64
		var pending int
		var hardeningJSON, findingsJSON string
		if err := scanRows.Scan(&hid, &pending, &hardeningJSON, &findingsJSON); err != nil {
			scanRows.Close()
			return nil, err
		}
		ov.Breakdown.PendingSecUpdates += pending
		if pending > 0 {
			ov.Pillars[PillarVulnerabilities] += pending
			actions = append(actions, SecurityAction{
				ID: "updates:" + itoa64(hid), Pillar: PillarVulnerabilities, Severity: "medium",
				Title: "Pending security updates", Detail: itoa(pending) + " security package updates pending",
				HostID: hid, HostName: hostNames[hid], Priority: float64(pending),
				Remediation: "upgrade",
			})
		}
		var checks []HardeningCheck
		if json.Unmarshal([]byte(hardeningJSON), &checks) == nil {
			for _, c := range checks {
				if c.ID == "firewall_active" && (c.Status == "warn" || c.Status == "fail") {
					ov.Breakdown.FirewallWarnings++
					ov.Pillars[PillarNetwork]++
					actions = append(actions, SecurityAction{
						ID: "firewall:" + itoa64(hid), Pillar: PillarNetwork, Severity: "high",
						Title: "Firewall warning", Detail: c.Detail, HostID: hid, HostName: hostNames[hid],
						Priority: 40 + float64(severityRank("high")), Remediation: "firewall",
					})
				}
			}
		}
		// KEV-tagged findings are stored with a CVE id; the exposure radar carries the KEV flag.
		var findings []SecurityFinding
		if json.Unmarshal([]byte(findingsJSON), &findings) == nil {
			for _, f := range findings {
				if severityRank(f.Severity) >= 5 && len(f.CVEs) > 0 {
					ov.Pillars[PillarVulnerabilities]++
				}
			}
		}
	}
	scanRows.Close()

	// --- KEV / critical-exposed findings (exposure_scans) ---
	expRows, err := d.Query(`SELECT host_id, critical_exposed, findings_json FROM exposure_scans
		WHERE host_id IN (`+scopeSQL+`)`, scopeArgs...)
	if err == nil {
		for expRows.Next() {
			var hid int64
			var crit int
			var findingsJSON string
			if expRows.Scan(&hid, &crit, &findingsJSON) != nil {
				continue
			}
			var findings []ExposureFinding
			if json.Unmarshal([]byte(findingsJSON), &findings) != nil {
				continue
			}
			for _, f := range findings {
				ov.Pillars[PillarExposure]++
				if !f.KEV && !f.IsCriticalExposed() {
					continue
				}
				ov.Breakdown.KEVCritical++
				ov.Pillars[PillarVulnerabilities]++
				prio := 90 + float64(severityRank(f.Severity))
				if f.KEV {
					prio += 10
				}
				evidence, _ := json.Marshal(f)
				actions = append(actions, SecurityAction{
					ID: "kev:" + f.ID, Pillar: PillarVulnerabilities, Severity: "critical",
					Title: f.Title, Detail: strings.Join(f.CVEs, ", "), HostID: hid, HostName: hostNames[hid],
					Priority: prio, Evidence: evidence, Remediation: "upgrade",
				})
			}
		}
		expRows.Close()
	}

	// --- FIM drift (actual_items) ---
	fimRows, err := d.Query(`SELECT host_id, target, details FROM actual_items
		WHERE kind = 'file_integrity' AND status = 'drift' AND host_id IN (`+scopeSQL+`)`, scopeArgs...)
	if err == nil {
		for fimRows.Next() {
			var hid int64
			var target, details string
			if fimRows.Scan(&hid, &target, &details) != nil {
				continue
			}
			ov.Breakdown.FIMDrift++
			ov.Pillars[PillarIdentity]++
			actions = append(actions, SecurityAction{
				ID: "fim:" + itoa64(hid) + ":" + target, Pillar: PillarIdentity, Severity: "high",
				Title: "FIM baseline drift", Detail: target, HostID: hid, HostName: hostNames[hid],
				Priority: 70 + float64(severityRank("high")), Evidence: json.RawMessage(`{"target":` + quote(target) + `,"details":` + quote(details) + `}`),
				Remediation: "accept_fim",
			})
		}
		fimRows.Close()
	}

	// --- Container misconfiguration + image CVEs (docker_security_scans) ---
	dockerRows, err := d.Query(`SELECT host_id, kind, subject, summary_json, findings_json FROM docker_security_scans
		WHERE host_id IN (`+scopeSQL+`)`, scopeArgs...)
	if err == nil {
		for dockerRows.Next() {
			var hid int64
			var kind, subject, summaryJSON, findingsJSON string
			if dockerRows.Scan(&hid, &kind, &subject, &summaryJSON, &findingsJSON) != nil {
				continue
			}
			var summary dockerSecuritySummary
			_ = json.Unmarshal([]byte(summaryJSON), &summary)
			high := summary.Counts.Critical + summary.Counts.High + summary.VulnCounts.Critical + summary.VulnCounts.High
			ov.Breakdown.ContainerIssues += high
			ov.Pillars[PillarContainers] += high
			if high > 0 {
				evidence, _ := json.Marshal(map[string]any{"kind": kind, "subject": subject, "summary": summary})
				actions = append(actions, SecurityAction{
					ID: "container:" + itoa64(hid) + ":" + kind + ":" + subject, Pillar: PillarContainers,
					Severity: "high", Title: "Container security finding", Detail: subject,
					HostID: hid, HostName: hostNames[hid], Priority: 60 + float64(high),
					Evidence: evidence, Remediation: "upgrade",
				})
			}
		}
		dockerRows.Close()
	}

	// --- Identity: critical/high account audit findings (account_audits) ---
	acctRows, err := d.Query(`SELECT host_id, COALESCE(summary_json,'{}'), COALESCE(findings_json,'[]') FROM account_audits
		WHERE host_id IN (`+scopeSQL+`)`, scopeArgs...)
	if err == nil {
		for acctRows.Next() {
			var hid int64
			var summaryJSON, findingsJSON string
			if acctRows.Scan(&hid, &summaryJSON, &findingsJSON) != nil {
				continue
			}
			var summary AccountAuditSummary
			_ = json.Unmarshal([]byte(summaryJSON), &summary)
			high := summary.Critical + summary.High + summary.UnknownKeys + summary.WeakKeys + summary.UID0Accounts
			ov.Pillars[PillarIdentity] += high
			if high > 0 {
				var findings []AccountFinding
				_ = json.Unmarshal([]byte(findingsJSON), &findings)
				for _, f := range findings {
					if severityRank(f.Severity) < 4 {
						continue
					}
					evidence, _ := json.Marshal(f)
					actions = append(actions, SecurityAction{
						ID: "identity:" + itoa64(hid) + ":" + f.ID, Pillar: PillarIdentity, Severity: f.Severity,
						Title: f.Title, Detail: f.Detail, HostID: hid, HostName: hostNames[hid],
						Priority: 50 + float64(severityRank(f.Severity)), Evidence: evidence, Remediation: "open_source",
					})
				}
			}
		}
		acctRows.Close()
	}

	// --- Code / IaC / secrets (iac_scans) — fleet scope only, sources live on the Pantau server ---
	if hostID == 0 {
		iacRows, err := d.Query(`SELECT summary_json, findings_json FROM iac_scans`)
		if err == nil {
			for iacRows.Next() {
				var summaryJSON, findingsJSON string
				if iacRows.Scan(&summaryJSON, &findingsJSON) != nil {
					continue
				}
				var summary struct {
					Critical int `json:"critical"`
					High     int `json:"high"`
				}
				_ = json.Unmarshal([]byte(summaryJSON), &summary)
				high := summary.Critical + summary.High
				ov.Pillars[PillarCode] += high
				var findings []struct {
					RuleID      string `json:"rule_id"`
					Category    string `json:"category"`
					Severity    string `json:"severity"`
					File        string `json:"file"`
					Line        int    `json:"line"`
					Title       string `json:"title"`
					Remediation string `json:"remediation"`
				}
				if json.Unmarshal([]byte(findingsJSON), &findings) == nil {
					for _, f := range findings {
						if f.Category != "secret" || severityRank(f.Severity) < 4 {
							continue
						}
						ov.Breakdown.LeakedSecrets++
						evidence, _ := json.Marshal(f)
						actions = append(actions, SecurityAction{
							ID:     "secret:" + f.RuleID + ":" + f.File + ":" + itoa(f.Line),
							Pillar: PillarCode, Severity: f.Severity, Title: f.Title,
							Detail: f.File + ":" + itoa(f.Line), Priority: 80 + float64(severityRank(f.Severity)),
							Evidence: evidence, Remediation: f.Remediation,
						})
					}
				}
			}
			iacRows.Close()
		}
	}

	// Overview pillar mirrors the total actionable surface.
	for _, p := range []string{PillarVulnerabilities, PillarNetwork, PillarContainers, PillarIdentity, PillarCode, PillarExposure} {
		ov.Pillars[PillarOverview] += ov.Pillars[p]
	}

	sort.SliceStable(actions, func(i, j int) bool { return actions[i].Priority > actions[j].Priority })
	if len(actions) > 10 {
		actions = actions[:10]
	}
	ov.TopActions = actions
	ov.Score, ov.Grade = ScoreSecurityPosture(ov.Breakdown)
	return ov, nil
}

// itoa / itoa64 / quote are small local helpers so this file has no fmt dependency in hot paths.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func itoa64(n int64) string { return itoa(int(n)) }

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
