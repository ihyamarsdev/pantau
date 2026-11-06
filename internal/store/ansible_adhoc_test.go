package store_test

import (
	"errors"
	"testing"

	"pantau/internal/store"
)

func TestAnsibleAdhocRuns_CRUD(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	if _, err := db.GetAnsibleAdhocRun(1); !errors.Is(err, store.ErrAnsibleAdhocRunNotFound) {
		t.Fatalf("GetAnsibleAdhocRun(1) err = %v, want not found", err)
	}

	id, err := db.CreateAnsibleAdhocRun(&store.AnsibleAdhocRun{
		Module: "ping", Pattern: "web-*", Targets: `[{"id":1,"name":"web-1"}]`, Verbosity: 2, Become: true,
	})
	if err != nil {
		t.Fatalf("CreateAnsibleAdhocRun: %v", err)
	}
	if id <= 0 {
		t.Fatalf("id = %d", id)
	}
	if err := db.StartAnsibleAdhocRun(id); err != nil {
		t.Fatalf("StartAnsibleAdhocRun: %v", err)
	}
	exit := 2
	if err := db.FinishAnsibleAdhocRun(id, "failed", &exit, "log line", `[{"alias":"web-1","status":"failed","summary":"boom"}]`); err != nil {
		t.Fatalf("FinishAnsibleAdhocRun: %v", err)
	}

	got, err := db.GetAnsibleAdhocRun(id)
	if err != nil {
		t.Fatalf("GetAnsibleAdhocRun: %v", err)
	}
	if got.Status != "failed" || got.ExitCode == nil || *got.ExitCode != 2 || !got.Become || got.Verbosity != 2 ||
		got.Module != "ping" || got.Pattern != "web-*" || got.Log != "log line" || got.StartedAt == nil || got.FinishedAt == nil {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if got.Results == "" {
		t.Fatal("results not persisted")
	}

	list, err := db.ListAnsibleAdhocRuns(0)
	if err != nil {
		t.Fatalf("ListAnsibleAdhocRuns: %v", err)
	}
	if len(list) != 1 || list[0].ID != id || list[0].Log != "" {
		t.Fatalf("list = %+v", list)
	}

	if err := db.FinishAnsibleAdhocRun(999, "failed", nil, "", ""); !errors.Is(err, store.ErrAnsibleAdhocRunNotFound) {
		t.Fatalf("finish unknown err = %v", err)
	}
}

func TestAnsibleAdhocRuns_PruneAndInterrupt(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	for i := 0; i < 5; i++ {
		id, err := db.CreateAnsibleAdhocRun(&store.AnsibleAdhocRun{Module: "ping", Targets: "[]"})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.FinishAnsibleAdhocRun(id, "success", nil, "", "[]"); err != nil {
			t.Fatal(err)
		}
	}
	running, err := db.CreateAnsibleAdhocRun(&store.AnsibleAdhocRun{Module: "ping", Targets: "[]"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.StartAnsibleAdhocRun(running); err != nil {
		t.Fatal(err)
	}

	if err := db.PruneAnsibleAdhocRuns(2); err != nil {
		t.Fatalf("PruneAnsibleAdhocRuns: %v", err)
	}
	list, err := db.ListAnsibleAdhocRuns(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 { // two kept finished + the running one
		t.Fatalf("after prune got %d runs, want 3: %+v", len(list), list)
	}

	n, err := db.MarkAnsibleAdhocRunsInterrupted()
	if err != nil || n != 1 {
		t.Fatalf("MarkAnsibleAdhocRunsInterrupted = %d, %v; want 1", n, err)
	}
	got, err := db.GetAnsibleAdhocRun(running)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "interrupted" || got.FinishedAt == nil {
		t.Fatalf("running run after interrupt = %+v", got)
	}
}

func TestAnsibleHostFacts_Upsert(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	hostID, err := db.CreateHost(&store.Host{Name: "web-1", Host: "10.0.0.1", Port: 22, User: "root"})
	if err != nil {
		t.Fatalf("CreateHost: %v", err)
	}

	if _, err := db.GetAnsibleHostFacts(hostID); !errors.Is(err, store.ErrAnsibleHostFactsNotFound) {
		t.Fatalf("GetAnsibleHostFacts before gather err = %v", err)
	}
	first, err := db.SetAnsibleHostFacts(hostID, `{"ansible_hostname":"web-1"}`, `{"hostname":"web-1"}`)
	if err != nil {
		t.Fatalf("SetAnsibleHostFacts: %v", err)
	}
	if _, err := db.SetAnsibleHostFacts(hostID, `{"ansible_hostname":"web-2"}`, `{"hostname":"web-2"}`); err != nil {
		t.Fatalf("SetAnsibleHostFacts (upsert): %v", err)
	}

	got, err := db.GetAnsibleHostFacts(hostID)
	if err != nil {
		t.Fatalf("GetAnsibleHostFacts: %v", err)
	}
	if got.HostID != hostID || got.Summary != `{"hostname":"web-2"}` {
		t.Fatalf("facts = %+v", got)
	}
	if got.GatheredAt.Before(first) {
		t.Fatalf("gathered_at went backwards: %v < %v", got.GatheredAt, first)
	}

	at, err := db.ListAnsibleHostFactsGatheredAt()
	if err != nil || len(at) != 1 || at[hostID].IsZero() {
		t.Fatalf("ListAnsibleHostFactsGatheredAt = %+v, %v", at, err)
	}

	// Facts follow the host lifecycle: deleting the host cascades.
	if err := db.DeleteHost(hostID); err != nil {
		t.Fatalf("DeleteHost: %v", err)
	}
	if _, err := db.GetAnsibleHostFacts(hostID); !errors.Is(err, store.ErrAnsibleHostFactsNotFound) {
		t.Fatalf("facts survived host deletion: %v", err)
	}
}
