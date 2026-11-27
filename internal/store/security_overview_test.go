package store

import (
	"encoding/json"
	"testing"
	"time"
)

func TestScoreSecurityPosture(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		b     SecurityBreakdown
		score int
		grade string
	}{
		{"clean", SecurityBreakdown{}, 100, "A"},
		{"one kev", SecurityBreakdown{KEVCritical: 1}, 92, "A"},
		{"mixed", SecurityBreakdown{KEVCritical: 2, FIMDrift: 3, LeakedSecrets: 1, PendingSecUpdates: 5, ContainerIssues: 2, FirewallWarnings: 1}, 56, "F"},
		{"floor", SecurityBreakdown{KEVCritical: 20, LeakedSecrets: 20, FIMDrift: 20, ContainerIssues: 20, FirewallWarnings: 20, PendingSecUpdates: 40}, 0, "F"},
		{"grade b", SecurityBreakdown{FIMDrift: 6, FirewallWarnings: 1}, 80, "B"},
		{"grade c", SecurityBreakdown{FIMDrift: 10}, 70, "C"},
		{"grade d", SecurityBreakdown{FIMDrift: 10, FirewallWarnings: 5}, 60, "D"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			score, grade := ScoreSecurityPosture(tc.b)
			if score != tc.score || grade != tc.grade {
				t.Fatalf("ScoreSecurityPosture(%+v) = (%d,%s), want (%d,%s)", tc.b, score, grade, tc.score, tc.grade)
			}
		})
	}
}

func TestGetSecurityOverviewAggregatesFleet(t *testing.T) {
	t.Parallel()
	db, err := Open(TemplateCopy(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	h1, err := db.CreateHost(&Host{Name: "web-1", Host: "10.0.0.1", GroupName: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	h2, err := db.CreateHost(&Host{Name: "web-2", Host: "10.0.0.2", GroupName: "prod"})
	if err != nil {
		t.Fatal(err)
	}

	// Security scan: 3 pending updates, firewall warning, one critical finding with a CVE.
	hardening, _ := json.Marshal([]HardeningCheck{{ID: "firewall_active", Status: "warn", Detail: "no firewall detected"}})
	findings, _ := json.Marshal([]SecurityFinding{{Package: "openssl", Severity: "critical", CVEs: []string{"CVE-2026-0001"}}})
	if err := db.SaveSecurityScan(&SecurityScan{
		HostID: h1, ScannedAt: time.Now().UTC(), Status: "ok", PendingSecurity: 3,
		Hardening: []HardeningCheck{{ID: "firewall_active", Status: "warn", Detail: "no firewall detected"}},
	}); err != nil {
		t.Fatal(err)
	}
	// Overwrite the JSON columns with the seeded payloads (SaveSecurityScan marshals from structs).
	if _, err := db.Exec(`UPDATE security_scans SET hardening_json=?, findings_json=? WHERE host_id=?`, string(hardening), string(findings), h1); err != nil {
		t.Fatal(err)
	}

	// KEV / critical-exposed finding on h2.
	expFindings, _ := json.Marshal([]ExposureFinding{{
		ID: "osv:nginx:GHSA-x", Source: ExposureSourceOSV, Severity: ExposureCriticalExposed,
		Title: "nginx RCE", CVEs: []string{"CVE-2026-1234"}, KEV: true,
	}})
	if err := db.SaveExposureScan(&ExposureScan{HostID: h2, ScannedAt: time.Now().UTC(), Status: "ok", Enabled: true, CriticalExposed: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE exposure_scans SET findings_json=? WHERE host_id=?`, string(expFindings), h2); err != nil {
		t.Fatal(err)
	}

	// FIM drift on h1.
	if _, err := db.Exec(`INSERT INTO actual_items(host_id, kind, target, current, status, details) VALUES (?, 'file_integrity', '/etc/passwd', 'changed', 'drift', 'hash mismatch')`, h1); err != nil {
		t.Fatal(err)
	}

	// Container critical finding on h1.
	dsSummary, _ := json.Marshal(dockerSecuritySummary{Status: "ok", Counts: DockerSeverityCounts{Critical: 1, Total: 1}})
	if _, err := db.Exec(`INSERT INTO docker_security_scans(host_id, kind, subject, scanned_at, summary_json, findings_json) VALUES (?, 'misconfig', 'app', ?, ?, '[]')`, h1, time.Now().UTC(), string(dsSummary)); err != nil {
		t.Fatal(err)
	}

	// Secret finding in an IaC scan (fleet scope only).
	iacSummary, _ := json.Marshal(map[string]int{"critical": 1, "high": 0})
	iacFindings, _ := json.Marshal([]map[string]any{{
		"rule_id": "aws_key", "category": "secret", "severity": "critical",
		"file": "main.tf", "line": 12, "title": "AWS access key", "remediation": "rotate key",
	}})
	if err := db.SaveIacScan(&IacScan{SourceKind: "repo", SourceID: 1, Summary: iacSummary, Findings: iacFindings, Status: "assessed"}); err != nil {
		t.Fatal(err)
	}

	ov, err := db.GetSecurityOverview(0, "")
	if err != nil {
		t.Fatal(err)
	}
	if ov.HostCount != 2 {
		t.Errorf("host_count = %d, want 2", ov.HostCount)
	}
	if ov.Breakdown.PendingSecUpdates != 3 {
		t.Errorf("pending_sec_updates = %d, want 3", ov.Breakdown.PendingSecUpdates)
	}
	if ov.Breakdown.FirewallWarnings != 1 {
		t.Errorf("firewall_warnings = %d, want 1", ov.Breakdown.FirewallWarnings)
	}
	if ov.Breakdown.KEVCritical != 1 {
		t.Errorf("kev_critical = %d, want 1", ov.Breakdown.KEVCritical)
	}
	if ov.Breakdown.FIMDrift != 1 {
		t.Errorf("fim_drift = %d, want 1", ov.Breakdown.FIMDrift)
	}
	if ov.Breakdown.ContainerIssues != 1 {
		t.Errorf("container_issues = %d, want 1", ov.Breakdown.ContainerIssues)
	}
	if ov.Breakdown.LeakedSecrets != 1 {
		t.Errorf("leaked_secrets = %d, want 1", ov.Breakdown.LeakedSecrets)
	}
	if ov.Pillars[PillarVulnerabilities] < 3 {
		t.Errorf("vulnerabilities pillar = %d, want >= 3", ov.Pillars[PillarVulnerabilities])
	}
	if ov.Pillars[PillarCode] != 1 {
		t.Errorf("code pillar = %d, want 1", ov.Pillars[PillarCode])
	}
	if len(ov.TopActions) == 0 || len(ov.TopActions) > 10 {
		t.Fatalf("top_actions length = %d, want 1..10", len(ov.TopActions))
	}
	// The KEV item must outrank the secret and the firewall warning.
	if ov.TopActions[0].Pillar != PillarVulnerabilities {
		t.Errorf("top action pillar = %s, want vulnerabilities", ov.TopActions[0].Pillar)
	}
	if ov.Score < 0 || ov.Score > 100 || ov.Grade == "" {
		t.Errorf("score/grade = %d/%s", ov.Score, ov.Grade)
	}
}

func TestGetSecurityOverviewScopeFilters(t *testing.T) {
	t.Parallel()
	db, err := Open(TemplateCopy(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	h1, _ := db.CreateHost(&Host{Name: "a", Host: "10.0.0.1", GroupName: "prod"})
	h2, _ := db.CreateHost(&Host{Name: "b", Host: "10.0.0.2", GroupName: "dev"})
	for _, id := range []int64{h1, h2} {
		if err := db.SaveSecurityScan(&SecurityScan{HostID: id, ScannedAt: time.Now().UTC(), Status: "ok", PendingSecurity: 2}); err != nil {
			t.Fatal(err)
		}
	}

	byHost, err := db.GetSecurityOverview(h1, "")
	if err != nil {
		t.Fatal(err)
	}
	if byHost.HostCount != 1 || byHost.Breakdown.PendingSecUpdates != 2 {
		t.Errorf("host scope = %+v", byHost)
	}

	byGroup, err := db.GetSecurityOverview(0, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if byGroup.HostCount != 1 || byGroup.Breakdown.PendingSecUpdates != 2 {
		t.Errorf("group scope = %+v", byGroup)
	}

	// IaC secrets are fleet-only: a host scope must not see them.
	iacSummary, _ := json.Marshal(map[string]int{"critical": 1})
	iacFindings, _ := json.Marshal([]map[string]any{{"category": "secret", "severity": "critical", "file": "x", "line": 1}})
	if err := db.SaveIacScan(&IacScan{SourceKind: "repo", SourceID: 1, Summary: iacSummary, Findings: iacFindings, Status: "assessed"}); err != nil {
		t.Fatal(err)
	}
	hostOV, _ := db.GetSecurityOverview(h1, "")
	if hostOV.Breakdown.LeakedSecrets != 0 {
		t.Errorf("host scope leaked secrets = %d, want 0", hostOV.Breakdown.LeakedSecrets)
	}
	fleetOV, _ := db.GetSecurityOverview(0, "")
	if fleetOV.Breakdown.LeakedSecrets != 1 {
		t.Errorf("fleet scope leaked secrets = %d, want 1", fleetOV.Breakdown.LeakedSecrets)
	}
}
