package web

import (
	"strings"
	"testing"
	"time"

	"pantau/internal/store"
)

func tfScheduleRuns(t *testing.T, db *store.DB, wsID int64) []store.TFRun {
	t.Helper()
	runs, err := db.ListTFRuns(wsID, 50)
	if err != nil {
		t.Fatal(err)
	}
	var out []store.TFRun
	for _, r := range runs {
		if r.TriggerKind == "schedule" {
			out = append(out, r)
		}
	}
	return out
}

func TestSchedulerPlanTickCreatesScheduleRunWhenOverdue(t *testing.T) {
	srv, db, _, _, _ := tfEnv(t, changesPlanner, okApplier)
	id := tfMkWS(t, db, "sched", "dev", false)
	ws, _ := db.GetTFWorkspace(id)
	ws.PlanIntervalMinutes = 30
	if err := db.UpdateTFWorkspace(ws); err != nil {
		t.Fatal(err)
	}

	srv.terraformPlanSchedulerTick(time.Now())
	runs := tfScheduleRuns(t, db, id)
	if len(runs) != 1 || runs[0].Type != "plan" || runs[0].Speculative {
		t.Fatalf("expected one non-speculative schedule plan, got %+v", runs)
	}
	tfWaitRun(t, db, runs[0].ID, "awaiting_approval")

	// Not overdue (and now locked awaiting approval): no second run.
	srv.terraformPlanSchedulerTick(time.Now().Add(time.Minute))
	if got := tfScheduleRuns(t, db, id); len(got) != 1 {
		t.Fatalf("expected still 1 run, got %d", len(got))
	}
}

func TestSchedulerPlanTickRespectsIntervalGateLockAndActiveRun(t *testing.T) {
	srv, db, mgr, _, _ := tfEnv(t, changesPlanner, okApplier)
	setInterval := func(id int64, n int) {
		ws, _ := db.GetTFWorkspace(id)
		ws.PlanIntervalMinutes = n
		if err := db.UpdateTFWorkspace(ws); err != nil {
			t.Fatal(err)
		}
	}
	off := tfMkWS(t, db, "off", "dev", false) // interval 0
	recent := tfMkWS(t, db, "recent", "dev", false)
	locked := tfMkWS(t, db, "locked", "dev", false)
	active := tfMkWS(t, db, "active", "dev", false)
	for _, id := range []int64{recent, locked, active} {
		setInterval(id, 60)
	}
	// recent: a schedule run was created just now.
	if _, err := db.CreateTFRun(&store.TFRun{WorkspaceID: recent, Type: "plan", Status: "no_changes", TriggerKind: "schedule"}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := db.TryLockTFWorkspace(locked, "manual", "held"); !ok {
		t.Fatal("lock")
	}
	mgr.byWorkspace[active] = 999 // an active run

	srv.terraformPlanSchedulerTick(time.Now().Add(5 * time.Minute))
	for name, id := range map[string]int64{"off": off, "locked": locked, "active": active} {
		if got := tfScheduleRuns(t, db, id); len(got) != 0 {
			t.Fatalf("%s must not be scheduled, got %+v", name, got)
		}
	}
	if got := tfScheduleRuns(t, db, recent); len(got) != 1 {
		t.Fatalf("recent must not be re-scheduled, got %d", len(got))
	}

	// Two hours later `recent` is overdue.
	srv.terraformPlanSchedulerTick(time.Now().Add(2 * time.Hour))
	if got := tfScheduleRuns(t, db, recent); len(got) != 2 {
		t.Fatalf("recent overdue should be scheduled, got %d", len(got))
	}

	// Gate off: nothing happens.
	_ = db.SetTerraformEnabled(false)
	setInterval(off, 1)
	srv.terraformPlanSchedulerTick(time.Now().Add(time.Hour))
	if got := tfScheduleRuns(t, db, off); len(got) != 0 {
		t.Fatalf("gate off must not schedule, got %+v", got)
	}
}

func tfApplyDecision(t *testing.T, db *store.DB, runID int64) *store.TFRun {
	t.Helper()
	r := tfWaitRun(t, db, runID, "applied")
	if r.Decision != "applied" || r.DecisionComment != "auto-apply" {
		t.Fatalf("decision not recorded as auto-apply: %q / %q", r.Decision, r.DecisionComment)
	}
	return r
}

func TestAutoApplyAppliesCleanPlanWithChanges(t *testing.T) {
	_, db, mgr, ev, _ := tfEnv(t, changesPlanner, okApplier)
	_ = db.SetSetting("tf_notify_events", "awaiting_approval,applied")
	id := tfMkWS(t, db, "auto", "staging", true)

	runID := tfStartRun(t, db, mgr, id)
	tfApplyDecision(t, db, runID)
	if ws, _ := db.GetTFWorkspace(id); ws.Locked {
		t.Fatal("workspace must be unlocked after auto-apply")
	}
	got := ev.all()
	if len(got) != 1 || got[0][1] != "Apply succeeded" {
		t.Fatalf("auto-applied run should only emit 'applied', got %v", got)
	}
}

func TestAutoApplySkippedOnPolicyWarningAndFailure(t *testing.T) {
	_, db, mgr, _, _ := tfEnv(t, changesPlanner, okApplier)
	id := tfMkWS(t, db, "pol", "dev", true)
	pid, err := db.CreateTFPolicy(&store.TFPolicy{Name: "no-td", Kind: "deny_resource_types", Params: `{"types":["terraform_data"]}`, Enforcement: "soft", Enabled: true, WorkspaceID: id})
	if err != nil {
		t.Fatal(err)
	}
	// Soft violation -> warning -> manual approval required.
	runID := tfStartRun(t, db, mgr, id)
	tfWaitRun(t, db, runID, "awaiting_approval")
	if r, _ := db.GetTFRun(runID); strings.Contains(r.Decision, "applied") {
		t.Fatalf("must not auto-apply on warning: %+v", r)
	}
	mgr.DiscardRun(id, runID, "cleanup")

	// Hard violation -> policy_failed.
	p, _ := db.GetTFPolicy(pid)
	p.Enforcement = "hard"
	if err := db.UpdateTFPolicy(p); err != nil {
		t.Fatal(err)
	}
	tfWaitRun(t, db, tfStartRun(t, db, mgr, id), "policy_failed")
}

func TestAutoApplyIgnoredWhenEnvironmentBecomesProduction(t *testing.T) {
	_, db, mgr, _, _ := tfEnv(t, changesPlanner, okApplier)
	id := tfMkWS(t, db, "later-prod", "dev", true)
	ws, _ := db.GetTFWorkspace(id)
	ws.Environment = "Production"
	if err := db.UpdateTFWorkspace(ws); err != nil { // store layer has no guard; the runtime must
		t.Fatal(err)
	}
	runID := tfStartRun(t, db, mgr, id)
	tfWaitRun(t, db, runID, "awaiting_approval")
	time.Sleep(200 * time.Millisecond)
	if r, _ := db.GetTFRun(runID); r.Status != "awaiting_approval" {
		t.Fatalf("production run must stay awaiting approval, got %s", r.Status)
	}
}

func TestAutoApplyOnlyForPlanRuns(t *testing.T) {
	_, db, mgr, _, _ := tfEnv(t, changesPlanner, okApplier)
	id := tfMkWS(t, db, "destroyer", "dev", true)
	ws, _ := db.GetTFWorkspace(id)
	run := &store.TFRun{WorkspaceID: id, Type: "destroy", Status: "queued", TriggerKind: "manual"}
	run.ID, _ = db.CreateTFRun(run)
	_, _ = db.TryLockTFWorkspace(id, "run:x", "t")
	if err := mgr.StartPlan(ws, run); err != nil {
		t.Fatal(err)
	}
	tfWaitRun(t, db, run.ID, "awaiting_approval")
	time.Sleep(200 * time.Millisecond)
	if r, _ := db.GetTFRun(run.ID); r.Status != "awaiting_approval" {
		t.Fatalf("destroy must never auto-apply, got %s", r.Status)
	}
}

func TestAutoApplyNeverForWebhookTrigger(t *testing.T) {
	_, db, mgr, _, _ := tfEnv(t, changesPlanner, okApplier)
	for _, tc := range []struct{ trigger, want string }{{"webhook", "awaiting_approval"}, {"schedule", "applied"}} {
		id := tfMkWS(t, db, "trig-"+tc.trigger, "dev", true)
		ws, _ := db.GetTFWorkspace(id)
		run := &store.TFRun{WorkspaceID: id, Type: "plan", Status: "queued", TriggerKind: tc.trigger}
		run.ID, _ = db.CreateTFRun(run)
		_, _ = db.TryLockTFWorkspace(id, "run:x", "t")
		if err := mgr.StartPlan(ws, run); err != nil {
			t.Fatal(err)
		}
		tfWaitRun(t, db, run.ID, tc.want)
		time.Sleep(200 * time.Millisecond)
		if r, _ := db.GetTFRun(run.ID); r.Status != tc.want {
			t.Fatalf("%s-triggered plan: want %s, got %s", tc.trigger, tc.want, r.Status)
		}
	}
}
