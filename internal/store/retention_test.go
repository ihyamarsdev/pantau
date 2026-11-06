package store

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestIncidentRetention(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hostID, err := db.CreateHost(&Host{Name: "Inc Host", Host: "10.0.0.1", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}

	for i := 1; i <= 600; i++ {
		if err := db.RecordIncident(&Incident{
			HostID:    hostID,
			Kind:      "drift",
			Target:    "nginx",
			EventType: "open",
			Summary:   fmt.Sprintf("incident #%d", i),
		}); err != nil {
			t.Fatalf("failed recording incident: %v", err)
		}
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM incidents WHERE host_id = ?`, hostID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 500 {
		t.Fatalf("expected 500 incidents after pruning, got %d", count)
	}

	var summary string
	if err := db.QueryRow(`SELECT summary FROM incidents WHERE host_id = ? ORDER BY id DESC LIMIT 1`, hostID).Scan(&summary); err != nil {
		t.Fatal(err)
	}
	if summary != "incident #600" {
		t.Fatalf("expected newest incident kept, got %q", summary)
	}
}

func TestAlertRetentionKeepsUnacknowledged(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hostID, err := db.CreateHost(&Host{Name: "Alert Host", Host: "10.0.0.2", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}

	// 550 alerts, then acknowledge them all: pruning must keep only the latest 500 acknowledged.
	for i := range 550 {
		if _, err := db.CreateAlert(&Alert{HostID: hostID, Level: "warning", Message: fmt.Sprintf("alert #%d", i)}); err != nil {
			t.Fatalf("failed creating alert: %v", err)
		}
	}
	if err := db.AcknowledgeHostAlerts(hostID); err != nil {
		t.Fatal(err)
	}

	for range 100 {
		if _, err := db.CreateAlert(&Alert{HostID: hostID, Level: "warning", Message: "acked burst"}); err != nil {
			t.Fatalf("failed creating alert: %v", err)
		}
	}
	if err := db.AcknowledgeHostAlerts(hostID); err != nil {
		t.Fatal(err)
	}
	// Unacknowledged alerts are never pruned.
	for range 20 {
		if _, err := db.CreateAlert(&Alert{HostID: hostID, Level: "critical", Message: "live"}); err != nil {
			t.Fatalf("failed creating alert: %v", err)
		}
	}

	var ackCount, total int
	if err := db.QueryRow(`SELECT COALESCE(SUM(acknowledged), 0), COUNT(*) FROM alerts WHERE host_id = ?`, hostID).Scan(&ackCount, &total); err != nil {
		t.Fatal(err)
	}
	if ackCount != 500 {
		t.Fatalf("expected 500 acknowledged alerts kept, got %d", ackCount)
	}
	if total-ackCount != 20 {
		t.Fatalf("expected 20 unacknowledged alerts kept, got %d", total-ackCount)
	}
}
