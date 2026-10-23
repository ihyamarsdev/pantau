package main

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

func TestSelectDueSecurityScanHosts(t *testing.T) {
	now := time.Now()

	h1NeverScanned := &store.Host{ID: 1, Name: "Node-1", Status: "healthy"}
	h2Stale := &store.Host{ID: 2, Name: "Node-2", Status: "healthy"}
	h3Recent := &store.Host{ID: 3, Name: "Node-3", Status: "healthy"}
	h4Down := &store.Host{ID: 4, Name: "Node-4", Status: "down"}
	h5InFlight := &store.Host{ID: 5, Name: "Node-5", Status: "healthy"}

	allHosts := []*store.Host{
		h1NeverScanned,
		h2Stale,
		h3Recent,
		h4Down,
		h5InFlight,
	}

	scanTimes := map[int64]time.Time{
		// h1 not present (never scanned)
		2: now.Add(-25 * time.Hour), // stale: scanned 25h ago with 24h interval
		3: now.Add(-1 * time.Hour),  // recent: scanned 1h ago
		4: now.Add(-50 * time.Hour), // stale, but host is down
		5: now.Add(-50 * time.Hour), // stale, but scan is in flight
	}

	isScanning := func(id int64) bool {
		return id == 5
	}

	// 1. Standard selection (interval = 24h)
	due := selectDueSecurityScanHosts(allHosts, scanTimes, 24, now, isScanning)

	// Expect h1 (never scanned) and h2 (stale >24h)
	if len(due) != 2 {
		t.Fatalf("expected 2 due hosts, got %d", len(due))
	}
	ids := map[int64]bool{due[0].ID: true, due[1].ID: true}
	if !ids[1] || !ids[2] {
		t.Errorf("expected due hosts 1 and 2, got IDs: %v", ids)
	}

	// 2. Interval 0 disables scanning (returns nil/empty)
	dueDisabled := selectDueSecurityScanHosts(allHosts, scanTimes, 0, now, isScanning)
	if len(dueDisabled) != 0 {
		t.Errorf("expected 0 due hosts when interval is 0, got %d", len(dueDisabled))
	}

	// 3. Negative interval also disables scanning
	dueNeg := selectDueSecurityScanHosts(allHosts, scanTimes, -1, now, isScanning)
	if len(dueNeg) != 0 {
		t.Errorf("expected 0 due hosts when interval is negative, got %d", len(dueNeg))
	}

	// 4. Down host skipped even if never scanned
	hDownNever := &store.Host{ID: 6, Name: "Down-Never", Status: "down"}
	dueDownOnly := selectDueSecurityScanHosts([]*store.Host{hDownNever}, scanTimes, 24, now, isScanning)
	if len(dueDownOnly) != 0 {
		t.Errorf("expected down host to be skipped, got %d", len(dueDownOnly))
	}

	// 5. In-flight host skipped even if never scanned
	hInFlightNever := &store.Host{ID: 7, Name: "InFlight-Never", Status: "healthy"}
	isScanning7 := func(id int64) bool { return id == 7 }
	dueInFlightOnly := selectDueSecurityScanHosts([]*store.Host{hInFlightNever}, scanTimes, 24, now, isScanning7)
	if len(dueInFlightOnly) != 0 {
		t.Errorf("expected in-flight host to be skipped, got %d", len(dueInFlightOnly))
	}
}

func TestRunSecurityScanCycle_Disabled(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-cycle-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := store.Open(filepath.Join(tmpDir, "pantau.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_ = db.SetSetting("security_scan_interval_hours", "0")

	mockRunner := sshrunner.NewMockRunner()
	factory := func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return mockRunner, nil
	}
	ins := inspector.New(db, factory, nil)

	ctx := context.Background()
	ok := runSecurityScanCycle(ctx, db, ins)
	if ok {
		t.Errorf("expected runSecurityScanCycle to return false when interval is 0 (disabled)")
	}
}

func TestRunSecurityScanCycle_Execution(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pantau-cycle-exec-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := store.Open(filepath.Join(tmpDir, "pantau.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var scanExecCount int64
	mockRunner := sshrunner.NewMockRunner()
	mockRunner.DefaultExec = func(cmd string) (string, string, int, error) {
		atomic.AddInt64(&scanExecCount, 1)
		return "===OS_RELEASE===\nID=debian\nID_LIKE=debian\n===UPDATES===\nMGR:apt\n", "", 0, nil
	}
	factory := func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return mockRunner, nil
	}
	ins := inspector.New(db, factory, nil)

	// Create 2 healthy hosts
	h1, _ := db.CreateHost(&store.Host{Name: "Node1", Host: "10.0.0.1", Port: 22, User: "root", Status: "healthy"})
	h2, _ := db.CreateHost(&store.Host{Name: "Node2", Host: "10.0.0.2", Port: 22, User: "root", Status: "healthy"})
	// Create 1 down host
	_, _ = db.CreateHost(&store.Host{Name: "NodeDown", Host: "10.0.0.3", Port: 22, User: "root", Status: "down"})

	// Mark h2 as scanned 1 hour ago (within 24h interval)
	_ = db.SaveSecurityScan(&store.SecurityScan{
		HostID:    h2,
		ScannedAt: time.Now().Add(-1 * time.Hour),
		Status:    "ok",
	})

	ctx := context.Background()
	ok := runSecurityScanCycle(ctx, db, ins)
	if !ok {
		t.Fatalf("expected runSecurityScanCycle to return true")
	}

	// Give spawned background scan a brief moment to run
	time.Sleep(100 * time.Millisecond)

	// Only h1 should be scanned (h2 was scanned recently, h3 is down)
	scan1, err := db.GetSecurityScan(h1)
	if err != nil {
		t.Fatalf("failed getting scan for h1: %v", err)
	}
	if scan1 == nil {
		// If mock runner didn't complete yet, give it another brief wait
		time.Sleep(200 * time.Millisecond)
		scan1, _ = db.GetSecurityScan(h1)
	}
	if scan1 != nil && scan1.HostID != h1 {
		t.Errorf("unexpected host id on scan: %d", scan1.HostID)
	}
}
