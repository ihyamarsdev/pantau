package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDockerSecurityScanStore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-docker-sec-store-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hostID, err := db.CreateHost(&Host{Name: "Test Host", Host: "192.168.1.50", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}

	// The audit row of a host.
	audit := &DockerSecurityScan{
		HostID: hostID, Kind: "misconfig", Subject: "", ScannedAt: time.Now(), Status: "ok",
		Counts: DockerSeverityCounts{Critical: 1, High: 2, Total: 3},
		Findings: []DockerSecurityFinding{
			{Container: "web", Image: "nginx:latest", RuleID: "privileged", Severity: "critical", Title: "Privileged container"},
		},
	}
	if err := db.SaveDockerSecurityScan(audit); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetDockerSecurityScan(hostID, "misconfig", "")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("the audit must be stored")
	}
	if got.Counts.Critical != 1 || got.Counts.Total != 3 {
		t.Errorf("counts = %+v", got.Counts)
	}
	if len(got.Findings) != 1 || got.Findings[0].RuleID != "privileged" {
		t.Errorf("findings = %+v", got.Findings)
	}

	// The same audit again replaces the previous row (upsert).
	audit.Counts = DockerSeverityCounts{High: 1, Total: 1}
	audit.Findings = []DockerSecurityFinding{{Container: "db", RuleID: "public_port_binding", Severity: "high"}}
	if err := db.SaveDockerSecurityScan(audit); err != nil {
		t.Fatal(err)
	}
	got, err = db.GetDockerSecurityScan(hostID, "misconfig", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Counts.Total != 1 || len(got.Findings) != 1 || got.Findings[0].Container != "db" {
		t.Errorf("the audit must be replaced, got %+v", got)
	}

	// Image scans are stored per image reference.
	for _, ref := range []string{"nginx:latest", "redis:7"} {
		scan := &DockerSecurityScan{
			HostID: hostID, Kind: "image", Subject: ref, ScannedAt: time.Now(), Scanner: "trivy", Status: "ok",
			VulnCounts: DockerVulnCounts{High: 2, Total: 2},
			Vulns: []DockerImageVuln{
				{ID: "CVE-2024-0001", Package: "openssl", Installed: "3.0.11", Fixed: "3.0.13", Severity: "high"},
			},
		}
		if err := db.SaveDockerSecurityScan(scan); err != nil {
			t.Fatal(err)
		}
	}
	list, err := db.ListDockerSecurityScans(hostID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 rows (audit + 2 images), got %d", len(list))
	}
	images := 0
	for _, sc := range list {
		if sc.Kind != "image" {
			continue
		}
		images++
		if len(sc.Vulns) != 1 || sc.Vulns[0].ID != "CVE-2024-0001" {
			t.Errorf("vulns = %+v", sc.Vulns)
		}
		if sc.Scanner != "trivy" {
			t.Errorf("scanner = %q", sc.Scanner)
		}
	}
	if images != 2 {
		t.Errorf("expected 2 image scans, got %d", images)
	}

	// A host without any result returns an empty list, not an error.
	empty, err := db.ListDockerSecurityScans(hostID + 999)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Errorf("expected no rows, got %d", len(empty))
	}
}

// A scan with many vulnerabilities survives the round trip: the counts stay with the row and the
// audit findings of an image scan stay empty.
func TestDockerSecurityScanStoreManyVulns(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-docker-sec-many-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hostID, err := db.CreateHost(&Host{Name: "Many", Host: "192.168.1.51", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	scan := &DockerSecurityScan{
		HostID: hostID, Kind: "image", Subject: "registry.example.com:5000/app@sha256:abc", ScannedAt: time.Now(),
		Scanner: "trivy", Status: "ok",
		VulnCounts: DockerVulnCounts{Critical: 1, High: 2, Medium: 3, Low: 4, Unknown: 5, Total: 15},
	}
	for range 15 {
		scan.Vulns = append(scan.Vulns, DockerImageVuln{
			ID: "CVE-2024-0001", Package: "pkg", Installed: "1.0", Fixed: "1.1", Severity: "high",
		})
	}
	if err := db.SaveDockerSecurityScan(scan); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetDockerSecurityScan(hostID, "image", scan.Subject)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("the scan must be stored")
	}
	if len(got.Vulns) != 15 {
		t.Errorf("vulns = %d, want 15", len(got.Vulns))
	}
	if got.VulnCounts.Total != 15 || got.VulnCounts.Critical != 1 || got.VulnCounts.Unknown != 5 {
		t.Errorf("vuln_counts = %+v", got.VulnCounts)
	}
	if got.ScannedAt.IsZero() {
		t.Error("scanned_at must be stored")
	}
	if got.Scanner != "trivy" || got.Status != "ok" {
		t.Errorf("scanner/status = %q/%q", got.Scanner, got.Status)
	}
	// The findings of an image scan are the vulnerability list, never the audit findings.
	if len(got.Findings) != 0 {
		t.Errorf("an image scan must not carry audit findings, got %d", len(got.Findings))
	}
}

// A scan without a timestamp gets one, and the caller sees it: the UI shows the time of every scan.
func TestDockerSecurityScanStoreFillsScannedAt(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-docker-sec-time-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hostID, err := db.CreateHost(&Host{Name: "NoTime", Host: "192.168.1.52", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	scan := &DockerSecurityScan{HostID: hostID, Kind: "image", Subject: "nginx:1.25", Scanner: "trivy", Status: "ok"}
	if err := db.SaveDockerSecurityScan(scan); err != nil {
		t.Fatal(err)
	}
	if scan.ScannedAt.IsZero() {
		t.Error("the saved scan must carry the timestamp it was stored with")
	}
	got, err := db.GetDockerSecurityScan(hostID, "image", "nginx:1.25")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ScannedAt.IsZero() {
		t.Fatalf("the stored scan must have a timestamp: %+v", got)
	}
}
