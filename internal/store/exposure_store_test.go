package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExposureScanRoundTrip(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-exposure-store-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)
	db, err := Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hostID, err := db.CreateHost(&Host{Name: "exposure-host", Host: "127.0.0.1"})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}

	if got, err := db.GetExposureScan(hostID); err != nil || got != nil {
		t.Fatalf("fresh host should have no scan, got %+v err=%v", got, err)
	}

	scan := &ExposureScan{
		HostID: hostID, ScannedAt: time.Now().UTC(), Status: "ok", Enabled: true,
		CriticalExposed: 1,
		Findings: []ExposureFinding{
			{ID: "osv:nginx:GHSA-x", Source: ExposureSourceOSV, Severity: ExposureCriticalExposed,
				Component: "nginx", CVEs: []string{"CVE-2026-1234"}, KEV: true, EPSS: 0.42},
		},
		Kernel: &KernelDrift{Running: "5.15.0-91", InstalledLatest: "5.15.0-97", RebootRequired: true},
		Notes:  []string{"some coverage note"},
	}
	if err := db.SaveExposureScan(scan); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := db.GetExposureScan(hostID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected stored scan")
	}
	if !got.Enabled || got.Status != "ok" || got.CriticalExposed != 1 {
		t.Errorf("scan metadata = %+v", got)
	}
	if len(got.Findings) != 1 || got.Findings[0].Severity != ExposureCriticalExposed || !got.Findings[0].KEV {
		t.Errorf("findings = %+v", got.Findings)
	}
	if got.Kernel == nil || !got.Kernel.RebootRequired {
		t.Errorf("kernel drift = %+v", got.Kernel)
	}
	if len(got.Notes) != 1 {
		t.Errorf("notes = %+v", got.Notes)
	}

	// Upsert: a second save replaces rather than duplicates.
	scan.CriticalExposed = 0
	scan.Findings = []ExposureFinding{}
	if err := db.SaveExposureScan(scan); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, _ = db.GetExposureScan(hostID)
	if got.CriticalExposed != 0 || len(got.Findings) != 0 {
		t.Errorf("upsert did not replace: %+v", got)
	}

	all, err := db.ListExposureScans()
	if err != nil || len(all) != 1 {
		t.Errorf("list = %d err=%v, want 1", len(all), err)
	}
}
