package store_test

import (
	"encoding/json"
	"testing"

	"pantau/internal/store"
)

func TestTFWorkspace_PlanIntervalAutoApplyRoundTrip(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	id, err := db.CreateTFWorkspace(&store.TFWorkspace{Name: "sched", PlanIntervalMinutes: 30, AutoApply: true})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	ws, err := db.GetTFWorkspace(id)
	if err != nil || ws.PlanIntervalMinutes != 30 || !ws.AutoApply {
		t.Fatalf("after create: %+v err=%v", ws, err)
	}

	ws.PlanIntervalMinutes, ws.AutoApply = 0, false
	if err := db.UpdateTFWorkspace(ws); err != nil {
		t.Fatalf("update: %v", err)
	}
	ws, _ = db.GetTFWorkspace(id)
	if ws.PlanIntervalMinutes != 0 || ws.AutoApply {
		t.Fatalf("after update: %+v", ws)
	}
}

func TestTFWorkspace_PlanIntervalLastScheduledRunAt(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	id, _ := db.CreateTFWorkspace(&store.TFWorkspace{Name: "sched-runs"})
	if at, err := db.LastTFScheduledRunAt(id); err != nil || at != nil {
		t.Fatalf("expected nil before any run, got %v err=%v", at, err)
	}
	// Non-schedule runs are ignored.
	if _, err := db.CreateTFRun(&store.TFRun{WorkspaceID: id, Type: "plan", Status: "queued", TriggerKind: "manual"}); err != nil {
		t.Fatal(err)
	}
	if at, _ := db.LastTFScheduledRunAt(id); at != nil {
		t.Fatalf("manual run must not count, got %v", at)
	}
	if _, err := db.CreateTFRun(&store.TFRun{WorkspaceID: id, Type: "plan", Status: "queued", TriggerKind: "schedule"}); err != nil {
		t.Fatal(err)
	}
	at, err := db.LastTFScheduledRunAt(id)
	if err != nil || at == nil || at.IsZero() {
		t.Fatalf("expected schedule run time, got %v err=%v", at, err)
	}
}

func TestTFWorkspace_SnapshotRoundTripPlanAutoApplyAndLinks(t *testing.T) {
	db1, cleanup1 := setupTestDB(t)
	defer cleanup1()
	a, _ := db1.CreateTFWorkspace(&store.TFWorkspace{Name: "consumer", Environment: "dev", PlanIntervalMinutes: 45, AutoApply: true})
	b, _ := db1.CreateTFWorkspace(&store.TFWorkspace{Name: "source"})
	if err := db1.ReplaceTFOutputLinks(a, []store.TFOutputLink{{VarName: "vpc_id", SourceWorkspaceID: b, OutputName: "id"}}); err != nil {
		t.Fatal(err)
	}
	payload, err := db1.ExportSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(payload)
	var decoded store.SnapshotPayload
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}

	db2, cleanup2 := setupTestDB(t)
	defer cleanup2()
	if err := db2.ImportSnapshot(&decoded); err != nil {
		t.Fatalf("import: %v", err)
	}
	ws, err := db2.GetTFWorkspace(a)
	if err != nil || ws.PlanIntervalMinutes != 45 || !ws.AutoApply {
		t.Fatalf("restored workspace: %+v err=%v", ws, err)
	}
	links, _ := db2.ListTFOutputLinks(a)
	if len(links) != 1 || links[0].VarName != "vpc_id" || links[0].SourceWorkspaceID != b || links[0].OutputName != "id" {
		t.Fatalf("restored links: %+v", links)
	}
}

func TestTFWorkspace_SnapshotRestoreOldPayloadWithoutNewFields(t *testing.T) {
	old := `{"terraform_workspaces":[{"id":7,"name":"legacy","binary":"terraform","source":"inline","files":"{}","tags":"[]","drift_interval_minutes":10,"created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z"}]}`
	var p store.SnapshotPayload
	if err := json.Unmarshal([]byte(old), &p); err != nil {
		t.Fatal(err)
	}
	db, cleanup := setupTestDB(t)
	defer cleanup()
	if err := db.ImportSnapshot(&p); err != nil {
		t.Fatalf("import old payload: %v", err)
	}
	ws, err := db.GetTFWorkspace(7)
	if err != nil || ws.PlanIntervalMinutes != 0 || ws.AutoApply || ws.DriftIntervalMinutes != 10 {
		t.Fatalf("legacy workspace: %+v err=%v", ws, err)
	}
	if links, _ := db.ListTFOutputLinks(7); len(links) != 0 {
		t.Fatalf("unexpected links: %+v", links)
	}
}
