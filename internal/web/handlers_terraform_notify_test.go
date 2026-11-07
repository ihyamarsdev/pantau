package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"pantau/internal/notify"
	"pantau/internal/store"
	"pantau/internal/terraform"
)

type tfEventLog struct {
	mu  sync.Mutex
	got [][3]string
}

func (e *tfEventLog) add(source, summary, detail string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.got = append(e.got, [3]string{source, summary, detail})
}

func (e *tfEventLog) all() [][3]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([][3]string(nil), e.got...)
}

func changesPlanner(ctx context.Context, spec terraform.PlanSpec, log io.Writer) (*terraform.PlanResult, error) {
	_, _ = io.WriteString(log, "Plan: 1 to add, 0 to change, 0 to destroy.\n")
	return &terraform.PlanResult{
		ExitCode: 2, HasChanges: true,
		Summary:  terraform.PlanSummary{Create: 1},
		PlanJSON: []byte(`{"resource_changes":[{"address":"terraform_data.foo","type":"terraform_data","name":"foo","change":{"actions":["create"]}}]}`),
	}, nil
}

func okApplier(ctx context.Context, spec terraform.ApplySpec, log io.Writer) (*terraform.ApplyResult, error) {
	_, _ = io.WriteString(log, "Apply complete!\n")
	return &terraform.ApplyResult{ExitCode: 0}, nil
}

// tfEnv is a gate-enabled server whose manager uses the given runners and records events.
func tfEnv(t *testing.T, p PlannerFunc, a ApplierFunc) (*Server, *store.DB, *TerraformRunManager, *tfEventLog, func(*http.Request)) {
	t.Helper()
	srv, db, root, auth := setupTerraformTestServer(t)
	if err := db.SetTerraformEnabled(true); err != nil {
		t.Fatal(err)
	}
	mgr := NewTerraformRunManager(db, root, p, a)
	ev := &tfEventLog{}
	mgr.events = ev.add
	srv.SetTerraformRunManager(mgr)
	return srv, db, mgr, ev, auth
}

func tfMkWS(t *testing.T, db *store.DB, name, env string, autoApply bool) int64 {
	t.Helper()
	id, err := db.CreateTFWorkspace(&store.TFWorkspace{Name: name, Environment: env, Source: "inline", Files: `{"main.tf":"x"}`, AutoApply: autoApply})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func tfWaitRun(t *testing.T, db *store.DB, runID int64, status string) *store.TFRun {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if r, err := db.GetTFRun(runID); err == nil && r.Status == status {
			time.Sleep(100 * time.Millisecond) // let deferred bookkeeping and hooks finish
			return r
		}
		time.Sleep(20 * time.Millisecond)
	}
	r, _ := db.GetTFRun(runID)
	t.Fatalf("run %d did not reach %s (now %+v)", runID, status, r)
	return nil
}

func tfStartRun(t *testing.T, db *store.DB, mgr *TerraformRunManager, wsID int64) int64 {
	t.Helper()
	ws, _ := db.GetTFWorkspace(wsID)
	run := &store.TFRun{WorkspaceID: wsID, Type: "plan", Status: "queued", TriggerKind: "manual"}
	run.ID, _ = db.CreateTFRun(run)
	if ok, _ := db.TryLockTFWorkspace(wsID, fmt.Sprintf("run:%d", run.ID), "t"); !ok {
		t.Fatal("lock")
	}
	if err := mgr.StartPlan(ws, run); err != nil {
		t.Fatal(err)
	}
	return run.ID
}

func TestTerraformNotifyEventSettings(t *testing.T) {
	if got := tfNotifyParse(""); !got["awaiting_approval"] || !got["errored"] || !got["drift"] || got["applied"] {
		t.Fatalf("defaults: %v", got)
	}
	if got := tfNotifyParse("none"); got["errored"] || got["drift"] {
		t.Fatalf("none: %v", got)
	}
	if got := tfNotifyParse("applied, bogus"); !got["applied"] || got["errored"] || len(got) != 4 {
		t.Fatalf("custom: %v", got)
	}
}

func TestTerraformNotifyNotificationsEndpoints(t *testing.T) {
	srv, db, _, _, auth := tfEnv(t, nil, nil)
	read := func(rec *httptest.ResponseRecorder) map[string]bool {
		var out struct {
			Events map[string]bool `json:"events"`
		}
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
			t.Fatalf("bad response %d %s", rec.Code, rec.Body.String())
		}
		return out.Events
	}
	if ev := read(doTFReq(t, srv, auth, "GET", "/api/terraform/notifications", nil)); !ev["drift"] || ev["applied"] {
		t.Fatalf("default events: %v", ev)
	}
	ev := read(doTFReq(t, srv, auth, "PUT", "/api/terraform/notifications", map[string]any{"events": map[string]bool{"applied": true, "drift": false}}))
	if !ev["applied"] || ev["drift"] || !ev["errored"] {
		t.Fatalf("after put: %v", ev)
	}
	if v, _ := db.GetSetting("tf_notify_events"); v != "awaiting_approval,errored,applied" {
		t.Fatalf("stored %q", v)
	}
	if rec := doTFReq(t, srv, auth, "PUT", "/api/terraform/notifications", map[string]any{"events": map[string]bool{"nope": true}}); rec.Code != 400 {
		t.Fatalf("unknown event: %d", rec.Code)
	}
	read(doTFReq(t, srv, auth, "PUT", "/api/terraform/notifications", map[string]any{"events": map[string]bool{"awaiting_approval": false, "errored": false, "applied": false}}))
	if v, _ := db.GetSetting("tf_notify_events"); v != "none" {
		t.Fatalf("all off stored %q", v)
	}

	// Test endpoint: no channel -> ok:false; webhook -> ok:true.
	srv.dispatcher = notify.New(db)
	rec := doTFReq(t, srv, auth, "POST", "/api/terraform/notifications/test", nil)
	if !strings.Contains(rec.Body.String(), `"ok":false`) {
		t.Fatalf("expected ok:false without channels: %s", rec.Body.String())
	}
	hits := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer ts.Close()
	_ = db.SetSetting("webhook_url", ts.URL)
	rec = doTFReq(t, srv, auth, "POST", "/api/terraform/notifications/test", nil)
	if !strings.Contains(rec.Body.String(), `"ok":true`) || hits != 1 {
		t.Fatalf("expected ok:true and one hit, got %s hits=%d", rec.Body.String(), hits)
	}
}

func TestTerraformNotifyAutoApplyRefusedForProduction(t *testing.T) {
	srv, db, _, _, auth := tfEnv(t, nil, nil)
	body := map[string]any{"name": "prod-ws", "environment": " Production ", "auto_apply": true, "files": map[string]string{"main.tf": "x"}}
	if rec := doTFReq(t, srv, auth, "POST", "/api/terraform/workspaces", body); rec.Code != 400 || !strings.Contains(rec.Body.String(), "production") {
		t.Fatalf("POST production: %d %s", rec.Code, rec.Body.String())
	}
	id := tfMkWS(t, db, "dev-ws", "dev", false)
	url := fmt.Sprintf("/api/terraform/workspaces/%d", id)
	rec := doTFReq(t, srv, auth, "PUT", url, map[string]any{"environment": "dev", "auto_apply": true, "plan_interval_minutes": 15})
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"auto_apply":true`) || !strings.Contains(rec.Body.String(), `"plan_interval_minutes":15`) {
		t.Fatalf("PUT dev: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doTFReq(t, srv, auth, "PUT", url, map[string]any{"environment": "PRODUCTION", "auto_apply": true}); rec.Code != 400 {
		t.Fatalf("PUT production: %d", rec.Code)
	}
	// Omitted fields keep stored values (older UI clients).
	rec = doTFReq(t, srv, auth, "PUT", url, map[string]any{"environment": "staging"})
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"auto_apply":true`) || !strings.Contains(rec.Body.String(), `"plan_interval_minutes":15`) {
		t.Fatalf("omitted fields reset: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doTFReq(t, srv, auth, "PUT", url, map[string]any{"plan_interval_minutes": -1}); rec.Code != 400 {
		t.Fatalf("negative interval: %d", rec.Code)
	}
}

func TestTerraformNotifyHooksFireOncePerTransition(t *testing.T) {
	srv, db, mgr, ev, _ := tfEnv(t, changesPlanner, okApplier)
	_ = srv
	_ = db.SetSetting("tf_notify_events", "awaiting_approval,errored,applied,drift")
	id := tfMkWS(t, db, "web", "dev", false)

	runID := tfStartRun(t, db, mgr, id)
	tfWaitRun(t, db, runID, "awaiting_approval")
	if got := ev.all(); len(got) != 1 || got[0][0] != "Terraform web (dev)" || got[0][1] != "Plan awaiting approval" || !strings.Contains(got[0][2], fmt.Sprintf("run %d, +1 ~0 -0", runID)) {
		t.Fatalf("awaiting_approval events: %v", got)
	}

	ws, _ := db.GetTFWorkspace(id)
	run, _ := db.GetTFRun(runID)
	if err := mgr.StartApply(ws, run, "approved by test"); err != nil {
		t.Fatal(err)
	}
	tfWaitRun(t, db, runID, "applied")
	if got := ev.all(); len(got) != 2 || got[1][1] != "Apply succeeded" {
		t.Fatalf("applied events: %v", got)
	}
}

func TestTerraformNotifyDisabledEventIsSilent(t *testing.T) {
	_, db, mgr, ev, _ := tfEnv(t, changesPlanner, okApplier)
	_ = db.SetSetting("tf_notify_events", "errored")
	id := tfMkWS(t, db, "quiet", "dev", false)
	tfWaitRun(t, db, tfStartRun(t, db, mgr, id), "awaiting_approval")
	if got := ev.all(); len(got) != 0 {
		t.Fatalf("awaiting_approval is disabled, got %v", got)
	}
}

func TestTerraformNotifyErroredExcerptHasNoSecret(t *testing.T) {
	const secret = "hunter2-very-secret"
	failing := func(ctx context.Context, spec terraform.PlanSpec, log io.Writer) (*terraform.PlanResult, error) {
		_, _ = io.WriteString(log, strings.Repeat("noise line\n", 100)+"Error: bad credentials "+secret+" rejected\n")
		return nil, errors.New("exit status 1")
	}
	_, db, mgr, ev, _ := tfEnv(t, failing, okApplier)
	id := tfMkWS(t, db, "err", "dev", false)
	_, _ = db.CreateTFVariable(&store.TFVariable{WorkspaceID: id, Key: "token", Value: secret, Sensitive: true})

	tfWaitRun(t, db, tfStartRun(t, db, mgr, id), "errored")
	got := ev.all()
	if len(got) != 1 || got[0][1] != "Plan errored" {
		t.Fatalf("errored events: %v", got)
	}
	if strings.Contains(got[0][2], secret) || !strings.Contains(got[0][2], "[REDACTED]") {
		t.Fatalf("secret leaked or not masked: %q", got[0][2])
	}
	if len(got[0][2]) > 300+len("run 99999\n...") {
		t.Fatalf("detail too long: %d", len(got[0][2]))
	}
}

func TestTerraformNotifyDriftOnlyOnTransition(t *testing.T) {
	drifting := func(ctx context.Context, spec terraform.PlanSpec, log io.Writer) (*terraform.PlanResult, error) {
		return &terraform.PlanResult{ExitCode: 2, HasChanges: true, Summary: terraform.PlanSummary{Drift: []terraform.ResourceChange{{Address: "a.b"}}}}, nil
	}
	_, db, mgr, ev, _ := tfEnv(t, drifting, okApplier)
	id := tfMkWS(t, db, "drifty", "dev", false)
	for range 2 {
		ws, _ := db.GetTFWorkspace(id)
		run := &store.TFRun{WorkspaceID: id, Type: "drift", Speculative: true, Status: "queued", TriggerKind: "drift"}
		run.ID, _ = db.CreateTFRun(run)
		_, _ = db.TryLockTFWorkspace(id, "run:x", "t")
		if err := mgr.StartPlan(ws, run); err != nil {
			t.Fatal(err)
		}
		tfWaitRun(t, db, run.ID, "planned")
	}
	if got := ev.all(); len(got) != 1 || got[0][1] != "Drift detected" {
		t.Fatalf("expected exactly one drift event, got %v", got)
	}
}
