package store_test

import (
	"errors"
	"strings"
	"testing"

	"pantau/internal/store"
)

func TestAgentMemory_ScopeDedupeAndLimits(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	h1, _ := db.CreateHost(&store.Host{Name: "a", Host: "10.0.0.1", Port: 22, User: "root"})
	h2, _ := db.CreateHost(&store.Host{Name: "b", Host: "10.0.0.2", Port: 22, User: "root"})

	fleet, err := db.AddAgentMemory(nil, "Backups run at 02:00", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddAgentMemory(&h1, "nginx is behind Cloudflare", "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddAgentMemory(&h2, "db host", "agent"); err != nil {
		t.Fatal(err)
	}

	// Same text in the same scope is refreshed, not duplicated; in another scope it is a new note.
	again, _ := db.AddAgentMemory(nil, "backups RUN at 02:00", "agent")
	if again.ID != fleet.ID {
		t.Fatalf("duplicate stored as #%d, want #%d", again.ID, fleet.ID)
	}
	if _, err := db.AddAgentMemory(&h1, "Backups run at 02:00", "agent"); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		host int64
		q    string
		want int
	}{{0, "", 4}, {h1, "", 3}, {h2, "", 2}, {0, "cloudflare", 1}, {0, "100%", 0}} {
		got, _ := db.ListAgentMemory(c.host, c.q)
		if len(got) != c.want {
			t.Errorf("list host=%d q=%q: %d notes, want %d", c.host, c.q, len(got), c.want)
		}
	}

	if _, err := db.AddAgentMemory(nil, "  ", "agent"); !errors.Is(err, store.ErrAgentMemoryEmpty) {
		t.Errorf("empty: %v", err)
	}
	if _, err := db.AddAgentMemory(nil, strings.Repeat("x", store.AgentMemoryMaxLen+1), "agent"); !errors.Is(err, store.ErrAgentMemoryTooLong) {
		t.Errorf("too long: %v", err)
	}
	if err := db.UpdateAgentMemory(999, "x"); !errors.Is(err, store.ErrAgentMemoryNotFound) {
		t.Errorf("update missing: %v", err)
	}

	// Deleting a host removes its notes but keeps the fleet-wide ones.
	if err := db.DeleteHost(h1); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.ListAgentMemory(0, ""); len(got) != 2 {
		t.Errorf("after host delete: %d notes, want 2", len(got))
	}

	// The store refuses a note beyond the cap.
	for i := range store.AgentMemoryMax {
		if _, err := db.AddAgentMemory(nil, "note "+strings.Repeat("n", i%7)+string(rune('a'+i%26))+strings.Repeat("z", i), "agent"); err != nil && !errors.Is(err, store.ErrAgentMemoryFull) {
			t.Fatal(err)
		}
	}
	if _, err := db.AddAgentMemory(nil, "one more distinct note", "agent"); !errors.Is(err, store.ErrAgentMemoryFull) {
		t.Errorf("full: %v", err)
	}
}

func TestAgentMemory_SnapshotRoundTrip(t *testing.T) {
	src, c1 := setupTestDB(t)
	defer c1()
	h, _ := src.CreateHost(&store.Host{Name: "a", Host: "10.0.0.1", Port: 22, User: "root"})
	_, _ = src.AddAgentMemory(nil, "fleet note", "agent")
	_, _ = src.AddAgentMemory(&h, "host note", "operator")
	payload, err := src.ExportSnapshot()
	if err != nil {
		t.Fatal(err)
	}

	dst, c2 := setupTestDB(t)
	defer c2()
	_, _ = dst.AddAgentMemory(nil, "stale", "agent")
	if err := dst.ImportSnapshot(payload); err != nil {
		t.Fatal(err)
	}
	got, _ := dst.ListAgentMemory(0, "")
	if len(got) != 2 {
		t.Fatalf("restored: %+v", got)
	}
	for _, n := range got {
		if n.Content == "host note" && (n.HostID == nil || n.Source != "operator") {
			t.Errorf("host note lost its scope or source: %+v", n)
		}
	}
}
