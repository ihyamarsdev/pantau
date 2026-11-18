package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestTriageResultPersistenceAndSignatureCache(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sig := "signature-abc"
	id, err := db.SaveTriageResult(&TriageResult{
		Signature: sig, AlertID: 7, HostID: 3, AlertType: "service", Unit: "nginx.service",
		Source: "fast_path", RootCause: "OOM", Impact: "down", Remediation: "restart",
		RemediationAction: "systemd_restart", TokensUsed: 0,
	})
	if err != nil || id <= 0 {
		t.Fatalf("SaveTriageResult = %d, %v", id, err)
	}

	byAlert, err := db.LatestTriageForAlert(7)
	if err != nil {
		t.Fatal(err)
	}
	if byAlert.Unit != "nginx.service" || byAlert.RemediationAction != "systemd_restart" {
		t.Errorf("byAlert = %+v", byAlert)
	}

	// A cache hit within the window reuses the row and is reported as "cache".
	cached, err := db.LatestTriageBySignature(sig, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if cached.Source != "cache" || cached.RootCause != "OOM" {
		t.Errorf("cached = %+v", cached)
	}

	// Outside the window it is a miss.
	if _, err := db.LatestTriageBySignature(sig, time.Now().Add(time.Hour)); err != sql.ErrNoRows {
		t.Errorf("expected cache miss (ErrNoRows), got %v", err)
	}

	list, err := db.ListRecentTriage(10)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListRecentTriage = %d rows, err=%v", len(list), err)
	}
}

func TestTriageBudgetAccumulatesPerDay(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	day := "2026-11-18"
	if used, _ := db.TriageBudgetDay(day); used != 0 {
		t.Fatalf("initial budget = %d, want 0", used)
	}
	if err := db.AddTriageBudget(day, 120); err != nil {
		t.Fatal(err)
	}
	if err := db.AddTriageBudget(day, 80); err != nil {
		t.Fatal(err)
	}
	used, _ := db.TriageBudgetDay(day)
	if used != 200 {
		t.Errorf("budget = %d, want 200", used)
	}
	other, _ := db.TriageBudgetDay("2026-11-19")
	if other != 0 {
		t.Errorf("other day budget = %d, want 0", other)
	}
}

func TestBriefingUpsertPerDay(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if b, err := db.LatestBriefing(); err != nil || b != nil {
		t.Fatalf("fresh DB briefing = %+v, err=%v", b, err)
	}
	b := &Briefing{
		Day: "2026-11-18", Summary: "All systems nominal", AnomalyCount: 0,
		Items: []string{}, TokensUsed: 0,
	}
	if err := db.SaveBriefing(b); err != nil {
		t.Fatal(err)
	}
	b.Summary = "3 anomalies"
	b.AnomalyCount = 3
	b.Items = []string{"ALERT h1", "UPDATES h2"}
	b.TokensUsed = 90
	if err := db.SaveBriefing(b); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetBriefing("2026-11-18")
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != "3 anomalies" || got.AnomalyCount != 3 || len(got.Items) != 2 || got.TokensUsed != 90 {
		t.Errorf("briefing = %+v", got)
	}
	latest, _ := db.LatestBriefing()
	if latest == nil || latest.Day != "2026-11-18" {
		t.Errorf("latest = %+v", latest)
	}
}

func TestAgentTriageMigrationIdempotent(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Running the migration twice must not fail (CREATE IF NOT EXISTS + alterAddColumn).
	if err := db.migrateAgentTriage(); err != nil {
		t.Fatal(err)
	}
	if err := db.migrateAgentTriage(); err != nil {
		t.Fatalf("second migration failed: %v", err)
	}
	// The unit column exists after migration, even on a DB created before it was added.
	if _, err := db.Exec(`INSERT INTO agent_triage(signature, unit) VALUES ('s', 'nginx.service')`); err != nil {
		t.Fatalf("unit column missing: %v", err)
	}
}
