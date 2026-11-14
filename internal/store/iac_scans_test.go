package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIacScanStore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-iac-store-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// An unscanned source has no scan.
	sc, err := db.GetIacScan("repo", 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sc != nil {
		t.Fatalf("expected nil scan, got %+v", sc)
	}

	summary := json.RawMessage(`{"total":2,"critical":1,"high":1,"medium":0,"low":0}`)
	findings := json.RawMessage(`[{"rule_id":"secret.aws-access-key"}]`)
	if err := db.SaveIacScan(&IacScan{
		SourceKind: "repo",
		SourceID:   7,
		ScannedAt:  time.Now().UTC(),
		Summary:    summary,
		Findings:   findings,
		Status:     "assessed",
		Files:      3,
		Truncated:  false,
		Root:       "/data/repos/app",
		Notes:      json.RawMessage(`[]`),
	}); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	sc, err = db.GetIacScan("repo", 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sc == nil {
		t.Fatal("expected a scan")
	}
	if sc.Status != "assessed" || sc.Files != 3 || sc.Root != "/data/repos/app" {
		t.Fatalf("unexpected scan %+v", sc)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(sc.Summary, &got); err != nil {
		t.Fatal(err)
	}
	if got["total"] != float64(2) {
		t.Fatalf("unexpected summary %v", got)
	}

	// A second scan of the same source replaces the first one (upsert, latest wins).
	if err := db.SaveIacScan(&IacScan{
		SourceKind: "repo",
		SourceID:   7,
		ScannedAt:  time.Now().UTC(),
		Summary:    json.RawMessage(`{"total":0}`),
		Findings:   json.RawMessage(`[]`),
		Status:     "assessed",
	}); err != nil {
		t.Fatalf("second save failed: %v", err)
	}
	sc, err = db.GetIacScan("repo", 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := json.Unmarshal(sc.Summary, &got); err != nil {
		t.Fatal(err)
	}
	if got["total"] != float64(0) {
		t.Fatalf("expected the latest scan to win, got %v", got)
	}

	// A different source kind is a different row.
	if err := db.SaveIacScan(&IacScan{SourceKind: "terraform", SourceID: 7, Summary: json.RawMessage(`{"total":9}`), Findings: json.RawMessage(`[]`)}); err != nil {
		t.Fatal(err)
	}
	all, err := db.ListIacScans()
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 scans, got %d", len(all))
	}

	// Delete removes only the row of that source.
	if err := db.DeleteIacScan("repo", 7); err != nil {
		t.Fatal(err)
	}
	sc, err = db.GetIacScan("repo", 7)
	if err != nil {
		t.Fatal(err)
	}
	if sc != nil {
		t.Fatalf("expected the scan to be deleted, got %+v", sc)
	}
	sc, err = db.GetIacScan("terraform", 7)
	if err != nil {
		t.Fatal(err)
	}
	if sc == nil {
		t.Fatal("expected the terraform scan to survive")
	}
}
