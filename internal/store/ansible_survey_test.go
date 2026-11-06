package store_test

import (
	"testing"
	"time"

	"pantau/internal/store"
)

func TestAnsiblePlaybookSurveyAndGitSync(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	const survey = `[{"key":"env","type":"select","options":["dev","prod"]}]`
	id, err := db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: "Survey PB", Survey: survey})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	pb, err := db.GetAnsiblePlaybook(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if pb.Survey != survey || pb.LastSyncCommit != "" || pb.LastSyncAt != nil {
		t.Fatalf("unexpected defaults: %+v", pb)
	}

	// The survey setter must not touch updated_at: editing a form is not a playbook change.
	updatedBefore := pb.UpdatedAt
	time.Sleep(2 * time.Millisecond)
	const edited = `[{"key":"name","type":"text"}]`
	if err := db.SetAnsiblePlaybookSurvey(id, edited); err != nil {
		t.Fatalf("SetAnsiblePlaybookSurvey: %v", err)
	}
	pb, _ = db.GetAnsiblePlaybook(id)
	if pb.Survey != edited || !pb.UpdatedAt.Equal(updatedBefore) {
		t.Fatalf("survey setter changed updated_at or lost the survey: %+v", pb)
	}

	// Sync records commit and time.
	at := time.Now().UTC().Truncate(time.Second)
	if err := db.SetAnsiblePlaybookGitSync(id, at, "0123456789abcdef0123456789abcdef01234567"); err != nil {
		t.Fatalf("SetAnsiblePlaybookGitSync: %v", err)
	}
	pb, _ = db.GetAnsiblePlaybook(id)
	if pb.LastSyncCommit != "0123456789abcdef0123456789abcdef01234567" || pb.LastSyncAt == nil || !pb.LastSyncAt.Equal(at) {
		t.Fatalf("sync not recorded: %+v", pb)
	}

	// List carries both too.
	list, err := db.ListAnsiblePlaybooks()
	if err != nil || len(list) != 1 || list[0].Survey != edited || list[0].LastSyncAt == nil {
		t.Fatalf("list: %+v err=%v", list, err)
	}

	// UpdateAnsiblePlaybook persists a changed survey.
	pb.Survey = survey
	if err := db.UpdateAnsiblePlaybook(pb); err != nil {
		t.Fatalf("update: %v", err)
	}
	if again, _ := db.GetAnsiblePlaybook(id); again.Survey != survey {
		t.Fatalf("update lost the survey: %+v", again)
	}

	// Unknown ids report not-found rather than a silent no-op.
	if err := db.SetAnsiblePlaybookSurvey(9999, survey); err != store.ErrAnsiblePlaybookNotFound {
		t.Fatalf("survey setter on missing id: %v", err)
	}
	if err := db.SetAnsiblePlaybookGitSync(9999, at, "x"); err != store.ErrAnsiblePlaybookNotFound {
		t.Fatalf("sync setter on missing id: %v", err)
	}
}

func TestAnsiblePlaybookSnapshotIncludesSurvey(t *testing.T) {
	src, cleanup := setupTestDB(t)
	defer cleanup()

	const survey = `[{"key":"env","type":"select","options":["dev"]}]`
	id, err := src.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Source: "git", GitURL: "https://example.com/a/b.git", GitPath: "site.yml", Survey: survey})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	at := time.Now().UTC().Truncate(time.Second)
	if err := src.SetAnsiblePlaybookGitSync(id, at, "abcdef0123456789abcdef0123456789abcdef01"); err != nil {
		t.Fatalf("sync: %v", err)
	}

	payload, err := src.ExportSnapshot()
	if err != nil {
		t.Fatalf("ExportSnapshot: %v", err)
	}
	if len(payload.AnsiblePlaybooks) != 1 || payload.AnsiblePlaybooks[0].Survey != survey ||
		payload.AnsiblePlaybooks[0].LastSyncCommit == "" || payload.AnsiblePlaybooks[0].LastSyncAt == nil {
		t.Fatalf("snapshot lost survey/sync: %+v", payload.AnsiblePlaybooks)
	}

	dst, cleanup2 := setupTestDB(t)
	defer cleanup2()
	if err := dst.ImportSnapshot(payload); err != nil {
		t.Fatalf("ImportSnapshot: %v", err)
	}
	restored, err := dst.GetAnsiblePlaybook(id)
	if err != nil {
		t.Fatalf("get restored: %v", err)
	}
	if restored.Survey != survey || restored.LastSyncCommit != "abcdef0123456789abcdef0123456789abcdef01" || restored.LastSyncAt == nil {
		t.Fatalf("restore lost survey/sync: %+v", restored)
	}

	// A snapshot taken before these columns existed still restores (zero values).
	legacy := *payload
	legacy.AnsiblePlaybooks = []store.SnapshotAnsiblePlaybook{{
		ID: 7, Name: "Legacy", Content: "- hosts: all\n", Source: "inline",
	}}
	dst2, cleanup3 := setupTestDB(t)
	defer cleanup3()
	if err := dst2.ImportSnapshot(&legacy); err != nil {
		t.Fatalf("ImportSnapshot legacy: %v", err)
	}
	old, err := dst2.GetAnsiblePlaybook(7)
	if err != nil {
		t.Fatalf("get legacy: %v", err)
	}
	if old.Survey != "" || old.LastSyncAt != nil || old.LastSyncCommit != "" {
		t.Fatalf("legacy restore should leave empty sync/survey: %+v", old)
	}
}
