package store

import (
	"path/filepath"
	"testing"
)

func TestUpsertActualItemsBatch(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "tx.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hostID, err := db.CreateHost(&Host{Name: "h", Host: "10.0.0.1", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}

	if err := db.UpsertActualItems([]*ActualItem{
		{HostID: hostID, Kind: "service", Target: "nginx", Current: "active", Status: "ok", Details: "d1"},
		{HostID: hostID, Kind: "disk", Target: "/", Current: "42%", Status: "ok"},
	}); err != nil {
		t.Fatal(err)
	}
	items, err := db.ListActualItems(hostID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 rows after batch insert, got %d", len(items))
	}
	byTarget := map[string]ActualItem{}
	for _, it := range items {
		byTarget[it.Kind+"\x00"+it.Target] = it
	}
	if it := byTarget["service\x00nginx"]; it.Current != "active" || it.Status != "ok" || it.Details != "d1" {
		t.Fatalf("nginx insert mismatch: %+v", it)
	}

	// Second call updates the existing row in place (no duplicate).
	if err := db.UpsertActualItems([]*ActualItem{
		{HostID: hostID, Kind: "service", Target: "nginx", Current: "inactive", Status: "drift", Details: "down"},
	}); err != nil {
		t.Fatal(err)
	}
	items, err = db.ListActualItems(hostID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected still 2 rows after batch update, got %d", len(items))
	}
	for _, it := range items {
		if it.Kind == "service" && it.Target == "nginx" {
			if it.Current != "inactive" || it.Status != "drift" || it.Details != "down" {
				t.Fatalf("nginx update mismatch: %+v", it)
			}
		}
	}

	// Empty batch is a no-op, not an error.
	if err := db.UpsertActualItems(nil); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
}
