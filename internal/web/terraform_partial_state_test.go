package web

import (
	"context"
	"io"
	"testing"

	"pantau/internal/terraform"
)

// A failed apply that already wrote state must keep that state, so the next plan does not rebuild from an old copy.
func TestTerraformFailedApplyKeepsPartialState(t *testing.T) {
	partialState := []byte(`{"version":4,"serial":7,"lineage":"l","resources":[{"type":"terraform_data","name":"made"}]}`)
	failingApplier := func(ctx context.Context, spec terraform.ApplySpec, log io.Writer) (*terraform.ApplyResult, error) {
		_, _ = io.WriteString(log, "Error: boom\n")
		return &terraform.ApplyResult{ExitCode: 1, State: partialState}, nil
	}
	_, db, mgr, _, _ := tfEnv(t, changesPlanner, failingApplier)
	id := tfMkWS(t, db, "partial", "dev", false)
	runID := tfStartRun(t, db, mgr, id)
	tfWaitRun(t, db, runID, "awaiting_approval")

	ws, _ := db.GetTFWorkspace(id)
	run, _ := db.GetTFRun(runID)
	if err := mgr.StartApply(ws, run, "approved by test"); err != nil {
		t.Fatal(err)
	}
	tfWaitRun(t, db, runID, "errored")

	latest, err := db.LatestTFStateVersion(id)
	if err != nil || latest == nil {
		t.Fatalf("partial state was not stored: %v", err)
	}
	if string(latest.State) != string(partialState) {
		t.Fatalf("stored state differs from partial state: %s", latest.State)
	}
}

// A second apply of the same run is refused while the first is still running.
func TestTerraformSecondApplyOfSameRunRefused(t *testing.T) {
	release := make(chan struct{})
	slowApplier := func(ctx context.Context, spec terraform.ApplySpec, log io.Writer) (*terraform.ApplyResult, error) {
		<-release
		return &terraform.ApplyResult{ExitCode: 0}, nil
	}
	_, db, mgr, _, _ := tfEnv(t, changesPlanner, slowApplier)
	id := tfMkWS(t, db, "double", "dev", false)
	runID := tfStartRun(t, db, mgr, id)
	tfWaitRun(t, db, runID, "awaiting_approval")

	ws, _ := db.GetTFWorkspace(id)
	run, _ := db.GetTFRun(runID)
	if err := mgr.StartApply(ws, run, "first"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.StartApply(ws, run, "second"); err == nil {
		t.Fatal("second apply of the same run was accepted")
	}
	close(release)
	tfWaitRun(t, db, runID, "applied")
}
