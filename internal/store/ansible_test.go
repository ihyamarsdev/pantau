package store_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"pantau/internal/store"
)

func TestAnsibleGateSettings(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// 1. Default should be false
	if db.GetAnsibleEnabled() {
		t.Fatalf("expected ansible_enabled to default to false")
	}

	// 2. Set to true
	if err := db.SetAnsibleEnabled(true); err != nil {
		t.Fatalf("SetAnsibleEnabled(true): %v", err)
	}
	if !db.GetAnsibleEnabled() {
		t.Fatalf("expected ansible_enabled to be true after setting")
	}

	// 3. Set to false
	if err := db.SetAnsibleEnabled(false); err != nil {
		t.Fatalf("SetAnsibleEnabled(false): %v", err)
	}
	if db.GetAnsibleEnabled() {
		t.Fatalf("expected ansible_enabled to be false after disabling")
	}
}

func TestAnsiblePlaybook_CRUDAndUniqueName(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create playbook 1
	p1 := &store.AnsiblePlaybook{
		Name:        "Deploy Web",
		Description: "Deploys nginx and configures site",
		Content:     "- hosts: all\n  tasks:\n    - ping:\n",
		ExtraVars:   `{"port": 80}`,
	}
	id1, err := db.CreateAnsiblePlaybook(p1)
	if err != nil {
		t.Fatalf("CreateAnsiblePlaybook failed: %v", err)
	}
	if id1 <= 0 {
		t.Fatalf("expected positive ID, got %d", id1)
	}

	// Duplicate case-insensitive name should fail with ErrAnsiblePlaybookNameTaken
	pDup := &store.AnsiblePlaybook{
		Name:    "deploy web",
		Content: "- hosts: all\n",
	}
	_, err = db.CreateAnsiblePlaybook(pDup)
	if !errors.Is(err, store.ErrAnsiblePlaybookNameTaken) {
		t.Fatalf("expected ErrAnsiblePlaybookNameTaken for duplicate name, got: %v", err)
	}

	// Get playbook 1
	got1, err := db.GetAnsiblePlaybook(id1)
	if err != nil {
		t.Fatalf("GetAnsiblePlaybook: %v", err)
	}
	if got1.Name != "Deploy Web" || got1.Description != "Deploys nginx and configures site" || got1.Content != p1.Content || got1.ExtraVars != p1.ExtraVars {
		t.Fatalf("unexpected playbook data: %+v", got1)
	}
	if got1.LastStatus != "" || got1.LastRunAt != nil {
		t.Fatalf("expected empty LastStatus and nil LastRunAt for playbook without runs, got status=%q, run_at=%v", got1.LastStatus, got1.LastRunAt)
	}

	// Update playbook 1
	got1.Name = "Deploy Web App"
	got1.Description = "Updated description"
	got1.Content = "- hosts: all\n  tasks:\n    - debug:\n"
	got1.ExtraVars = `{"port": 8080}`
	if err := db.UpdateAnsiblePlaybook(got1); err != nil {
		t.Fatalf("UpdateAnsiblePlaybook: %v", err)
	}

	// Verify update
	updated, err := db.GetAnsiblePlaybook(id1)
	if err != nil {
		t.Fatalf("GetAnsiblePlaybook after update: %v", err)
	}
	if updated.Name != "Deploy Web App" || updated.Description != "Updated description" || updated.Content != got1.Content || updated.ExtraVars != `{"port": 8080}` {
		t.Fatalf("playbook fields not updated properly: %+v", updated)
	}

	// Create second playbook
	p2 := &store.AnsiblePlaybook{
		Name:    "Database Backup",
		Content: "- hosts: db\n",
	}
	id2, err := db.CreateAnsiblePlaybook(p2)
	if err != nil {
		t.Fatalf("create playbook 2: %v", err)
	}

	// Update with duplicate name of p2 should fail
	updated.Name = "database backup"
	err = db.UpdateAnsiblePlaybook(updated)
	if !errors.Is(err, store.ErrAnsiblePlaybookNameTaken) {
		t.Fatalf("expected ErrAnsiblePlaybookNameTaken on duplicate name update, got: %v", err)
	}

	// Get non-existent playbook
	_, err = db.GetAnsiblePlaybook(999999)
	if !errors.Is(err, store.ErrAnsiblePlaybookNotFound) {
		t.Fatalf("expected ErrAnsiblePlaybookNotFound, got: %v", err)
	}

	// Update non-existent playbook
	nonExistent := &store.AnsiblePlaybook{ID: 999999, Name: "Ghost"}
	err = db.UpdateAnsiblePlaybook(nonExistent)
	if !errors.Is(err, store.ErrAnsiblePlaybookNotFound) {
		t.Fatalf("expected ErrAnsiblePlaybookNotFound on update, got: %v", err)
	}

	// Delete playbook
	if err := db.DeleteAnsiblePlaybook(id2); err != nil {
		t.Fatalf("DeleteAnsiblePlaybook: %v", err)
	}
	_, err = db.GetAnsiblePlaybook(id2)
	if !errors.Is(err, store.ErrAnsiblePlaybookNotFound) {
		t.Fatalf("expected ErrAnsiblePlaybookNotFound after delete, got: %v", err)
	}

	// Delete non-existent
	err = db.DeleteAnsiblePlaybook(999999)
	if !errors.Is(err, store.ErrAnsiblePlaybookNotFound) {
		t.Fatalf("expected ErrAnsiblePlaybookNotFound on deleting non-existent ID, got: %v", err)
	}
}

func TestAnsiblePlaybook_ListOmitsContentAndReflectsLastStatus(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	p1 := &store.AnsiblePlaybook{
		Name:        "Playbook Alpha",
		Description: "Alpha desc",
		Content:     "large content alpha",
		ExtraVars:   "{}",
	}
	id1, err := db.CreateAnsiblePlaybook(p1)
	if err != nil {
		t.Fatalf("create p1: %v", err)
	}

	p2 := &store.AnsiblePlaybook{
		Name:        "Playbook Beta",
		Description: "Beta desc",
		Content:     "large content beta",
		ExtraVars:   "{}",
	}
	id2, err := db.CreateAnsiblePlaybook(p2)
	if err != nil {
		t.Fatalf("create p2: %v", err)
	}

	// Initially list
	list, err := db.ListAnsiblePlaybooks()
	if err != nil {
		t.Fatalf("ListAnsiblePlaybooks: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 playbooks, got %d", len(list))
	}
	for _, pb := range list {
		if pb.Content != "" {
			t.Fatalf("expected ListAnsiblePlaybooks to omit Content for economy, got: %q", pb.Content)
		}
		if pb.LastStatus != "" || pb.LastRunAt != nil {
			t.Fatalf("expected empty LastStatus and nil LastRunAt initially, got %q / %v", pb.LastStatus, pb.LastRunAt)
		}
	}

	// Create run 1 for Alpha: queued -> running -> failed
	exitCodeFailed := 2
	run1 := &store.AnsibleRun{
		PlaybookID:   id1,
		PlaybookName: "Playbook Alpha",
		Mode:         "run",
	}
	r1ID, err := db.CreateAnsibleRun(run1)
	if err != nil {
		t.Fatalf("create run 1: %v", err)
	}
	_ = db.StartAnsibleRun(r1ID)
	_ = db.FinishAnsibleRun(r1ID, "failed", &exitCodeFailed, "some error log", "")

	// Verify Alpha has LastStatus == "failed"
	pAlpha, err := db.GetAnsiblePlaybook(id1)
	if err != nil {
		t.Fatalf("GetAnsiblePlaybook(Alpha): %v", err)
	}
	if pAlpha.LastStatus != "failed" || pAlpha.LastRunAt == nil {
		t.Fatalf("expected LastStatus='failed' and non-nil LastRunAt, got status=%q, run_at=%v", pAlpha.LastStatus, pAlpha.LastRunAt)
	}

	// Create run 2 for Alpha: queued -> running -> success
	exitCodeSuccess := 0
	run2 := &store.AnsibleRun{
		PlaybookID:   id1,
		PlaybookName: "Playbook Alpha",
		Mode:         "run",
	}
	r2ID, err := db.CreateAnsibleRun(run2)
	if err != nil {
		t.Fatalf("create run 2: %v", err)
	}
	_ = db.StartAnsibleRun(r2ID)
	_ = db.FinishAnsibleRun(r2ID, "success", &exitCodeSuccess, "all tasks completed ok", "")

	// Last status on Alpha should now be "success" (from run 2, newest run)
	pAlpha, _ = db.GetAnsiblePlaybook(id1)
	if pAlpha.LastStatus != "success" {
		t.Fatalf("expected LastStatus='success' on Alpha after run 2, got %q", pAlpha.LastStatus)
	}

	// Verify in ListAnsiblePlaybooks
	list, err = db.ListAnsiblePlaybooks()
	if err != nil {
		t.Fatalf("ListAnsiblePlaybooks: %v", err)
	}
	foundAlpha := false
	foundBeta := false
	for _, pb := range list {
		if pb.ID == id1 {
			foundAlpha = true
			if pb.LastStatus != "success" || pb.LastRunAt == nil {
				t.Fatalf("expected Alpha LastStatus='success' in list, got %q", pb.LastStatus)
			}
		}
		if pb.ID == id2 {
			foundBeta = true
			if pb.LastStatus != "" || pb.LastRunAt != nil {
				t.Fatalf("expected Beta LastStatus='' in list, got %q", pb.LastStatus)
			}
		}
	}
	if !foundAlpha || !foundBeta {
		t.Fatalf("failed to find both playbooks in list")
	}
}

func TestAnsibleRun_LifecycleAndLogNeverInJSON(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	pID, err := db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{
		Name:    "Web Setup",
		Content: "- hosts: all\n",
	})
	if err != nil {
		t.Fatalf("create playbook: %v", err)
	}

	// 1. Create run (queued)
	r := &store.AnsibleRun{
		PlaybookID:   pID,
		PlaybookName: "Web Setup",
		Mode:         "diff",
		Targets:      `[{"id":1,"name":"srv-1"}]`,
		ExtraVars:    `{"env":"prod"}`,
		Tags:         "deploy,web",
		SkipTags:     "slow",
		Limit:        "web-1,web-2",
		Verbosity:    4,
		Become:       true,
		Log:          "initial log segment",
	}
	runID, err := db.CreateAnsibleRun(r)
	if err != nil {
		t.Fatalf("CreateAnsibleRun: %v", err)
	}

	gotR, err := db.GetAnsibleRun(runID)
	if err != nil {
		t.Fatalf("GetAnsibleRun: %v", err)
	}
	if gotR.Status != "queued" {
		t.Fatalf("expected status 'queued', got %q", gotR.Status)
	}
	if !gotR.Become || gotR.Verbosity != 4 || gotR.Tags != "deploy,web" || gotR.SkipTags != "slow" ||
		gotR.Limit != "web-1,web-2" || gotR.Recap != "" {
		t.Fatalf("run fields mismatch: %+v", gotR)
	}
	if gotR.Log != "initial log segment" {
		t.Fatalf("GetAnsibleRun must return log: got %q", gotR.Log)
	}
	if gotR.StartedAt != nil || gotR.FinishedAt != nil || gotR.ExitCode != nil {
		t.Fatalf("queued run should have nil started_at, finished_at, exit_code")
	}

	// Verify Log is omitted when serialized to JSON
	jsonBytes, err := json.Marshal(gotR)
	if err != nil {
		t.Fatalf("json.Marshal(AnsibleRun): %v", err)
	}
	jsonStr := string(jsonBytes)
	if strings.Contains(jsonStr, "initial log segment") || strings.Contains(jsonStr, `"log"`) {
		t.Fatalf("AnsibleRun.Log must never appear in JSON serialization, got: %s", jsonStr)
	}

	// 2. Start run -> running
	if err := db.StartAnsibleRun(runID); err != nil {
		t.Fatalf("StartAnsibleRun: %v", err)
	}
	runningRun, err := db.GetAnsibleRun(runID)
	if err != nil {
		t.Fatalf("GetAnsibleRun after start: %v", err)
	}
	if runningRun.Status != "running" || runningRun.StartedAt == nil {
		t.Fatalf("expected status='running' and non-nil StartedAt, got status=%q, started_at=%v", runningRun.Status, runningRun.StartedAt)
	}

	// 3. Finish run -> success with exit code 0
	ec := 0
	fullLog := "PLAY [all] ****\nTASK [ping] ****\nok: [srv-1]\nPLAY RECAP ****\nsrv-1: ok=1\n"
	recap := `{"srv-1":{"ok":1,"changed":0,"unreachable":0,"failed":0,"skipped":0,"rescued":0,"ignored":0}}`
	if err := db.FinishAnsibleRun(runID, "success", &ec, fullLog, recap); err != nil {
		t.Fatalf("FinishAnsibleRun: %v", err)
	}
	finishedRun, err := db.GetAnsibleRun(runID)
	if err != nil {
		t.Fatalf("GetAnsibleRun after finish: %v", err)
	}
	if finishedRun.Status != "success" || finishedRun.FinishedAt == nil || finishedRun.ExitCode == nil || *finishedRun.ExitCode != 0 {
		t.Fatalf("expected status='success', non-nil FinishedAt, exitCode=0; got %+v", finishedRun)
	}
	if finishedRun.Log != fullLog {
		t.Fatalf("expected finished log %q, got %q", fullLog, finishedRun.Log)
	}
	if finishedRun.Recap != recap {
		t.Fatalf("expected recap %q, got %q", recap, finishedRun.Recap)
	}

	// 4. ListAnsibleRuns omits Log
	runsList, err := db.ListAnsibleRuns(pID, "", 10)
	if err != nil {
		t.Fatalf("ListAnsibleRuns: %v", err)
	}
	if len(runsList) != 1 {
		t.Fatalf("expected 1 run in list, got %d", len(runsList))
	}
	if runsList[0].Log != "" {
		t.Fatalf("ListAnsibleRuns must omit Log, got %q", runsList[0].Log)
	}
	if runsList[0].ExitCode == nil || *runsList[0].ExitCode != 0 {
		t.Fatalf("expected ExitCode 0 in list, got %v", runsList[0].ExitCode)
	}

	// Non-existent run
	_, err = db.GetAnsibleRun(999999)
	if !errors.Is(err, store.ErrAnsibleRunNotFound) {
		t.Fatalf("expected ErrAnsibleRunNotFound, got %v", err)
	}
	err = db.StartAnsibleRun(999999)
	if !errors.Is(err, store.ErrAnsibleRunNotFound) {
		t.Fatalf("expected ErrAnsibleRunNotFound on start, got %v", err)
	}
	err = db.FinishAnsibleRun(999999, "failed", &ec, "err", "")
	if !errors.Is(err, store.ErrAnsibleRunNotFound) {
		t.Fatalf("expected ErrAnsibleRunNotFound on finish, got %v", err)
	}
}

func TestAnsibleRun_PruningSemantic(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	pID, err := db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{
		Name:    "Worker Playbook",
		Content: "- hosts: all\n",
	})
	if err != nil {
		t.Fatalf("create playbook: %v", err)
	}

	ec := 0
	// Create 5 finished runs
	for i := 1; i <= 5; i++ {
		rID, err := db.CreateAnsibleRun(&store.AnsibleRun{
			PlaybookID:   pID,
			PlaybookName: "Worker Playbook",
			Mode:         "run",
		})
		if err != nil {
			t.Fatalf("create run: %v", err)
		}
		_ = db.StartAnsibleRun(rID)
		_ = db.FinishAnsibleRun(rID, "success", &ec, "log", "")
	}

	// Create 1 queued run and 1 running run
	qID, err := db.CreateAnsibleRun(&store.AnsibleRun{
		PlaybookID:   pID,
		PlaybookName: "Worker Playbook",
		Mode:         "run",
	})
	if err != nil {
		t.Fatalf("create queued run: %v", err)
	}

	rID, err := db.CreateAnsibleRun(&store.AnsibleRun{
		PlaybookID:   pID,
		PlaybookName: "Worker Playbook",
		Mode:         "run",
	})
	if err != nil {
		t.Fatalf("create running run: %v", err)
	}
	_ = db.StartAnsibleRun(rID)

	// Total runs: 7 (5 finished, 1 queued, 1 running)
	allRuns, err := db.ListAnsibleRuns(pID, "", 50)
	if err != nil || len(allRuns) != 7 {
		t.Fatalf("expected 7 runs initially, got %d (err: %v)", len(allRuns), err)
	}

	// Prune keeping newest 2 finished runs
	if err := db.PruneAnsibleRuns(pID, 2); err != nil {
		t.Fatalf("PruneAnsibleRuns: %v", err)
	}

	// Remaining should be: 2 newest finished + 1 queued + 1 running = 4 runs
	remaining, err := db.ListAnsibleRuns(pID, "", 50)
	if err != nil {
		t.Fatalf("ListAnsibleRuns after prune: %v", err)
	}
	if len(remaining) != 4 {
		t.Fatalf("expected 4 runs remaining (2 finished + 1 queued + 1 running), got %d", len(remaining))
	}

	// Ensure the queued and running runs were never touched
	queuedFound := false
	runningFound := false
	for _, r := range remaining {
		if r.ID == qID && r.Status == "queued" {
			queuedFound = true
		}
		if r.ID == rID && r.Status == "running" {
			runningFound = true
		}
	}
	if !queuedFound || !runningFound {
		t.Fatalf("running or queued runs were pruned incorrectly! remaining: %+v", remaining)
	}
}

func TestAnsibleRun_MarkInterrupted(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	pID, err := db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{
		Name:    "App Deploy",
		Content: "- hosts: all\n",
	})
	if err != nil {
		t.Fatalf("create playbook: %v", err)
	}

	// 1 queued run
	qID, _ := db.CreateAnsibleRun(&store.AnsibleRun{
		PlaybookID:   pID,
		PlaybookName: "App Deploy",
		Status:       "queued",
	})

	// 1 running run
	rID, _ := db.CreateAnsibleRun(&store.AnsibleRun{
		PlaybookID:   pID,
		PlaybookName: "App Deploy",
	})
	_ = db.StartAnsibleRun(rID)

	// 1 finished success run
	ec := 0
	fID, _ := db.CreateAnsibleRun(&store.AnsibleRun{
		PlaybookID:   pID,
		PlaybookName: "App Deploy",
	})
	_ = db.StartAnsibleRun(fID)
	_ = db.FinishAnsibleRun(fID, "success", &ec, "done", "")

	// Call MarkAnsibleRunsInterrupted
	affected, err := db.MarkAnsibleRunsInterrupted()
	if err != nil {
		t.Fatalf("MarkAnsibleRunsInterrupted: %v", err)
	}
	if affected != 2 {
		t.Fatalf("expected 2 runs affected, got %d", affected)
	}

	// Verify qID and rID are now "interrupted" with FinishedAt set
	qRun, _ := db.GetAnsibleRun(qID)
	if qRun.Status != "interrupted" || qRun.FinishedAt == nil {
		t.Fatalf("expected queued run to become interrupted with finished_at set, got %+v", qRun)
	}

	rRun, _ := db.GetAnsibleRun(rID)
	if rRun.Status != "interrupted" || rRun.FinishedAt == nil {
		t.Fatalf("expected running run to become interrupted with finished_at set, got %+v", rRun)
	}

	// Verify fID remains "success"
	fRun, _ := db.GetAnsibleRun(fID)
	if fRun.Status != "success" {
		t.Fatalf("expected finished run to remain 'success', got %q", fRun.Status)
	}
}

func TestAnsiblePlaybook_DeleteCascadesRuns(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	pID, err := db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{
		Name:    "To Be Deleted",
		Content: "- hosts: all\n",
	})
	if err != nil {
		t.Fatalf("create playbook: %v", err)
	}

	// Create 2 runs for this playbook
	r1ID, _ := db.CreateAnsibleRun(&store.AnsibleRun{PlaybookID: pID, PlaybookName: "To Be Deleted"})
	r2ID, _ := db.CreateAnsibleRun(&store.AnsibleRun{PlaybookID: pID, PlaybookName: "To Be Deleted"})

	// Delete playbook
	if err := db.DeleteAnsiblePlaybook(pID); err != nil {
		t.Fatalf("DeleteAnsiblePlaybook: %v", err)
	}

	// Playbook must not exist
	_, err = db.GetAnsiblePlaybook(pID)
	if !errors.Is(err, store.ErrAnsiblePlaybookNotFound) {
		t.Fatalf("expected ErrAnsiblePlaybookNotFound, got %v", err)
	}

	// Runs must be removed
	_, err = db.GetAnsibleRun(r1ID)
	if !errors.Is(err, store.ErrAnsibleRunNotFound) {
		t.Fatalf("expected run 1 to be deleted, got %v", err)
	}
	_, err = db.GetAnsibleRun(r2ID)
	if !errors.Is(err, store.ErrAnsibleRunNotFound) {
		t.Fatalf("expected run 2 to be deleted, got %v", err)
	}
}

func TestAnsiblePlaybooks_SnapshotRoundtripAndLegacy(t *testing.T) {
	db1, cleanup1 := setupTestDB(t)
	defer cleanup1()

	// Create playbook in db1
	pID, err := db1.CreateAnsiblePlaybook(&store.AnsiblePlaybook{
		Name:        "Snapshot Playbook",
		Description: "Saved in backup",
		Content:     "- hosts: all\n  tasks:\n    - ping:\n",
		ExtraVars:   `{"test": true}`,
	})
	if err != nil {
		t.Fatalf("create playbook: %v", err)
	}

	// Create a run in db1
	_, err = db1.CreateAnsibleRun(&store.AnsibleRun{
		PlaybookID:   pID,
		PlaybookName: "Snapshot Playbook",
		Status:       "success",
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	// Export snapshot
	payload, err := db1.ExportSnapshot()
	if err != nil {
		t.Fatalf("ExportSnapshot: %v", err)
	}
	if len(payload.AnsiblePlaybooks) != 1 {
		t.Fatalf("expected 1 playbook in snapshot payload, got %d", len(payload.AnsiblePlaybooks))
	}
	snapP := payload.AnsiblePlaybooks[0]
	if snapP.ID != pID || snapP.Name != "Snapshot Playbook" || snapP.Content != "- hosts: all\n  tasks:\n    - ping:\n" {
		t.Fatalf("unexpected snapshot playbook data: %+v", snapP)
	}

	// Import into fresh db2
	db2, cleanup2 := setupTestDB(t)
	defer cleanup2()

	// In db2, add a dummy run to ensure restore clears ansible_runs table
	dummyPID, _ := db2.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: "Old Local Playbook"})
	dummyRID, _ := db2.CreateAnsibleRun(&store.AnsibleRun{PlaybookID: dummyPID, PlaybookName: "Old Local Playbook"})

	if err := db2.ImportSnapshot(payload); err != nil {
		t.Fatalf("ImportSnapshot: %v", err)
	}

	// Playbook should be restored
	restoredP, err := db2.GetAnsiblePlaybook(pID)
	if err != nil {
		t.Fatalf("GetAnsiblePlaybook on restored db: %v", err)
	}
	if restoredP.Name != "Snapshot Playbook" || restoredP.Content != snapP.Content || restoredP.ExtraVars != snapP.ExtraVars {
		t.Fatalf("restored playbook mismatch: %+v", restoredP)
	}

	// Runs should NOT be restored (empty) and old dummy run wiped
	restoredRuns, err := db2.ListAnsibleRuns(0, "", 100)
	if err != nil {
		t.Fatalf("ListAnsibleRuns on restored db: %v", err)
	}
	if len(restoredRuns) != 0 {
		t.Fatalf("expected 0 runs after restore (runs not snapshotted and table cleared), got %d", len(restoredRuns))
	}
	_, err = db2.GetAnsibleRun(dummyRID)
	if !errors.Is(err, store.ErrAnsibleRunNotFound) {
		t.Fatalf("expected old dummy run to be wiped by restore, got %v", err)
	}

	// Legacy snapshot test: payload with AnsiblePlaybooks omitted/nil should still restore cleanly
	legacyPayload := &store.SnapshotPayload{
		Version:   1,
		CreatedAt: time.Now().UTC(),
		Settings: map[string]string{
			"site_name": "Legacy Pantau",
		},
	}
	if err := db2.ImportSnapshot(legacyPayload); err != nil {
		t.Fatalf("legacy snapshot restore failed: %v", err)
	}

	// Ansible playbooks should now be cleared by restore wipe list
	pbsAfterLegacy, err := db2.ListAnsiblePlaybooks()
	if err != nil {
		t.Fatalf("ListAnsiblePlaybooks: %v", err)
	}
	if len(pbsAfterLegacy) != 0 {
		t.Fatalf("expected 0 playbooks after restoring legacy payload, got %d", len(pbsAfterLegacy))
	}
}
