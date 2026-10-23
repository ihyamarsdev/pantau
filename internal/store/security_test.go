package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSecurityScanStore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-security-store-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hostID, err := db.CreateHost(&Host{
		Name: "Test Host",
		Host: "192.168.1.50",
		Port: 22,
		User: "root",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. GetSecurityScan for unscanned host returns (nil, nil)
	scan, err := db.GetSecurityScan(hostID)
	if err != nil {
		t.Fatalf("unexpected error getting unscanned host: %v", err)
	}
	if scan != nil {
		t.Fatalf("expected nil scan for unscanned host, got %+v", scan)
	}

	// 2. SaveSecurityScan with nil findings/hardening/notes ensures non-nil slices on read
	now := time.Now().Truncate(time.Second)
	initialScan := &SecurityScan{
		HostID:            hostID,
		ScannedAt:         now,
		Status:            "ok",
		PackageManager:    "apt",
		OSFamily:          "ubuntu",
		PendingSecurity:   2,
		PendingTotal:      5,
		Severity:          SecuritySeverity{Important: 1, Low: 1},
		RebootRequired:    true,
		RepoCacheAgeHours: 12,
		Findings: []SecurityFinding{
			{
				Package:          "libssl3",
				InstalledVersion: "3.0.2-0ubuntu1.10",
				FixedVersion:     "3.0.2-0ubuntu1.12",
				Severity:         "important",
				Advisory:         "USN-1234-1",
			},
			{
				Package:          "bash",
				InstalledVersion: "5.0-6ubuntu1.1",
				FixedVersion:     "5.0-6ubuntu1.2",
				Severity:         "low",
				Advisory:         "",
			},
		},
		Hardening: []HardeningCheck{
			{
				ID:     "sshd_permit_root_login",
				Title:  "SSH Root Login",
				Status: "pass",
				Detail: "prohibit-password",
			},
		},
		Notes: []string{"test note"},
	}

	if err := db.SaveSecurityScan(initialScan); err != nil {
		t.Fatalf("save security scan: %v", err)
	}

	fetched, err := db.GetSecurityScan(hostID)
	if err != nil {
		t.Fatalf("get security scan: %v", err)
	}
	if fetched == nil {
		t.Fatal("expected security scan, got nil")
	}
	if fetched.HostID != hostID || fetched.Status != "ok" || fetched.PackageManager != "apt" || fetched.OSFamily != "ubuntu" {
		t.Fatalf("metadata mismatch: %+v", fetched)
	}
	if fetched.PendingSecurity != 2 || fetched.PendingTotal != 5 || !fetched.RebootRequired || fetched.RepoCacheAgeHours != 12 {
		t.Fatalf("counts mismatch: %+v", fetched)
	}
	if fetched.Severity.Important != 1 || fetched.Severity.Low != 1 {
		t.Fatalf("severity mismatch: %+v", fetched.Severity)
	}
	if len(fetched.Findings) != 2 || len(fetched.Hardening) != 1 || len(fetched.Notes) != 1 {
		t.Fatalf("slices length mismatch: %+v", fetched)
	}
	if fetched.Findings[0].Package != "libssl3" || fetched.Findings[0].Advisory != "USN-1234-1" {
		t.Fatalf("finding content mismatch: %+v", fetched.Findings[0])
	}

	// 3. Upsert update
	updatedScan := &SecurityScan{
		HostID:            hostID,
		ScannedAt:         now.Add(1 * time.Hour),
		Status:            "partial",
		PackageManager:    "apt",
		OSFamily:          "ubuntu",
		PendingSecurity:   0,
		PendingTotal:      1,
		Severity:          SecuritySeverity{},
		RebootRequired:    false,
		RepoCacheAgeHours: 200,
		Findings:          nil, // Test nil slices persist as empty slices
		Hardening:         nil,
		Notes:             nil,
	}

	if err := db.SaveSecurityScan(updatedScan); err != nil {
		t.Fatalf("upsert security scan: %v", err)
	}

	fetchedUpdated, err := db.GetSecurityScan(hostID)
	if err != nil {
		t.Fatalf("get updated security scan: %v", err)
	}
	if fetchedUpdated.Status != "partial" || fetchedUpdated.PendingSecurity != 0 || fetchedUpdated.RebootRequired {
		t.Fatalf("upsert failed to update values: %+v", fetchedUpdated)
	}
	if fetchedUpdated.Findings == nil || fetchedUpdated.Hardening == nil || fetchedUpdated.Notes == nil {
		t.Fatal("expected non-nil slices on read even when saved nil")
	}

	// 4. ListSecurityScanTimes
	times, err := db.ListSecurityScanTimes()
	if err != nil {
		t.Fatalf("list scan times: %v", err)
	}
	if len(times) != 1 || times[hostID].IsZero() {
		t.Fatalf("expected 1 scan time for host %d, got %+v", hostID, times)
	}

	// 5. Foreign key cascade delete
	if err := db.DeleteHost(hostID); err != nil {
		t.Fatalf("delete host: %v", err)
	}

	postDeleteScan, err := db.GetSecurityScan(hostID)
	if err != nil {
		t.Fatalf("get scan after host delete: %v", err)
	}
	if postDeleteScan != nil {
		t.Fatalf("expected scan to be deleted via CASCADE, but found %+v", postDeleteScan)
	}

	timesAfterDelete, err := db.ListSecurityScanTimes()
	if err != nil {
		t.Fatalf("list scan times after delete: %v", err)
	}
	if len(timesAfterDelete) != 0 {
		t.Fatalf("expected 0 scan times after host delete, got %+v", timesAfterDelete)
	}
}
