package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/store"
	"pantau/internal/terraform"
)

func setupTerraformTestServer(t *testing.T) (*Server, *store.DB, string, func(*http.Request)) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test_tf.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to init test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	tfDataRoot := filepath.Join(dir, "terraform")
	_ = os.MkdirAll(tfDataRoot, 0700)

	ins := inspector.New(db, nil, nil)
	srv := NewServer(db, ins, nil)
	srv.SetTerraformDataDir(tfDataRoot)

	token := "tf_test_session_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	authReq := func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: "pantau_session", Value: token})
	}

	return srv, db, tfDataRoot, authReq
}

func doTFReq(t *testing.T, srv *Server, authReq func(*http.Request), method, url string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal body: %v", err)
		}
		bodyReader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, url, bodyReader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if authReq != nil {
		authReq(req)
	}
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	return rec
}

// 1. Unauthenticated request rejected
func TestTerraformUnauthenticated(t *testing.T) {
	srv, _, _, _ := setupTerraformTestServer(t)
	rec := doTFReq(t, srv, nil, http.MethodGet, "/api/terraform/workspaces", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", rec.Code)
	}
}

// 2. Gate disabled -> 403 {"error":"terraform is disabled"}
func TestTerraformGateDisabled(t *testing.T) {
	srv, _, _, auth := setupTerraformTestServer(t)

	// Gate is disabled by default
	rec := doTFReq(t, srv, auth, http.MethodGet, "/api/terraform/workspaces", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for disabled gate, got %d", rec.Code)
	}

	// Status endpoint is accessible without gate
	rec = doTFReq(t, srv, auth, http.MethodGet, "/api/terraform/status", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for status, got %d", rec.Code)
	}

	var st map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if st["enabled"] != false {
		t.Fatalf("expected enabled=false, got %v", st["enabled"])
	}

	// Enable gate via PUT /api/terraform/status
	rec = doTFReq(t, srv, auth, http.MethodPut, "/api/terraform/status", map[string]bool{"enabled": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for put status, got %d", rec.Code)
	}

	// Now workspaces endpoint succeeds
	rec = doTFReq(t, srv, auth, http.MethodGet, "/api/terraform/workspaces", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 after enabling gate, got %d", rec.Code)
	}
}

// 3. Workspace CRUD + duplicate name conflict + manual lock / unlock / force-unlock
func TestTerraformWorkspaceLifecycle(t *testing.T) {
	srv, _, _, auth := setupTerraformTestServer(t)
	_ = doTFReq(t, srv, auth, http.MethodPut, "/api/terraform/status", map[string]bool{"enabled": true})

	// Create workspace
	body := map[string]interface{}{
		"name":        "prod-app",
		"description": "Production Application",
		"environment": "production",
		"tags":        []string{"web", "prod"},
		"binary":      "terraform",
		"source":      "inline",
		"files": map[string]string{
			"main.tf": `output "hello" { value = "world" }`,
		},
	}
	rec := doTFReq(t, srv, auth, http.MethodPost, "/api/terraform/workspaces", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}

	var res struct {
		Workspace map[string]interface{} `json:"workspace"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	wsID := int64(res.Workspace["id"].(float64))
	if wsID <= 0 {
		t.Fatalf("invalid workspace id: %v", wsID)
	}

	// Duplicate name -> 409
	recDup := doTFReq(t, srv, auth, http.MethodPost, "/api/terraform/workspaces", body)
	if recDup.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict for duplicate name, got %d", recDup.Code)
	}

	// Get detail
	rec = doTFReq(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/terraform/workspaces/%d", wsID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for detail, got %d", rec.Code)
	}

	// Lock workspace
	rec = doTFReq(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/terraform/workspaces/%d/lock", wsID), map[string]string{"reason": "maintenance"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for lock, got %d", rec.Code)
	}

	// Verify locked
	rec = doTFReq(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/terraform/workspaces/%d", wsID), nil)
	var wsDetail struct {
		Workspace map[string]interface{} `json:"workspace"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &wsDetail)
	if wsDetail.Workspace["locked"] != true || wsDetail.Workspace["status"] != "locked" {
		t.Fatalf("expected locked workspace, got %+v", wsDetail.Workspace)
	}

	// Starting a run while locked -> 423
	recRun := doTFReq(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/terraform/workspaces/%d/runs", wsID), map[string]string{"type": "plan"})
	if recRun.Code != 423 {
		t.Fatalf("expected 423 Locked for run while workspace locked, got %d", recRun.Code)
	}

	// Unlock
	rec = doTFReq(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/terraform/workspaces/%d/unlock", wsID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for unlock, got %d", rec.Code)
	}

	// Force-unlock
	rec = doTFReq(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/terraform/workspaces/%d/force-unlock", wsID), map[string]string{"comment": "emergency"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for force-unlock, got %d", rec.Code)
	}

	// Webhook rotate
	rec = doTFReq(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/terraform/workspaces/%d/webhook/rotate", wsID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for webhook rotate, got %d", rec.Code)
	}
	var rot struct {
		WebhookURL    string `json:"webhook_url"`
		WebhookSecret string `json:"webhook_secret"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rot); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if !strings.HasPrefix(rot.WebhookURL, "/api/terraform/webhook/") || rot.WebhookSecret == "" {
		t.Fatalf("invalid rotate response: %+v", rot)
	}
}

// 4. Mocked Run pipeline: plan -> awaiting_approval -> policy/cost -> apply -> applied
func TestTerraformRunPipelineMocked(t *testing.T) {
	srv, db, tfDataRoot, auth := setupTerraformTestServer(t)
	_ = doTFReq(t, srv, auth, http.MethodPut, "/api/terraform/status", map[string]bool{"enabled": true})

	// Inject custom planner and applier to test pipeline mechanics without invoking terraform
	planCalled := false
	applyCalled := false

	mockPlanner := func(ctx context.Context, spec terraform.PlanSpec, log io.Writer) (*terraform.PlanResult, error) {
		planCalled = true
		_, _ = io.WriteString(log, "Plan: 1 to add, 0 to change, 0 to destroy.\n")
		return &terraform.PlanResult{
			ExitCode:   2,
			HasChanges: true,
			Summary: terraform.PlanSummary{
				Create: 1,
				Changes: []terraform.ResourceChange{
					{
						Address:  "terraform_data.foo",
						Type:     "terraform_data",
						Name:     "foo",
						Action:   "create",
						Provider: "provider[\"registry.terraform.io/hashicorp/terraform\"]",
					},
				},
			},
			PlanJSON: []byte(`{"resource_changes":[{"address":"terraform_data.foo","type":"terraform_data","name":"foo","change":{"actions":["create"]}}]}`),
		}, nil
	}

	mockApplier := func(ctx context.Context, spec terraform.ApplySpec, log io.Writer) (*terraform.ApplyResult, error) {
		applyCalled = true
		_, _ = io.WriteString(log, "Apply complete! Resources: 1 added, 0 changed, 0 destroyed.\n")
		fakeState := []byte(`{"version":4,"serial":1,"lineage":"abc-123","terraform_version":"1.5.0","outputs":{},"resources":[{"type":"terraform_data","name":"foo","provider":"provider[\"registry.terraform.io/hashicorp/terraform\"]","instances":[{"attributes":{"id":"foo-1"}}]}]}`)
		return &terraform.ApplyResult{
			ExitCode: 0,
			State:    fakeState,
		}, nil
	}

	mgr := NewTerraformRunManager(db, tfDataRoot, mockPlanner, mockApplier)
	srv.SetTerraformRunManager(mgr)

	// Create workspace
	body := map[string]interface{}{
		"name":   "mock-ws",
		"binary": "terraform",
		"source": "inline",
		"files": map[string]string{
			"main.tf": `resource "terraform_data" "foo" {}`,
		},
	}
	rec := doTFReq(t, srv, auth, http.MethodPost, "/api/terraform/workspaces", body)
	var wsRes struct {
		Workspace map[string]interface{} `json:"workspace"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &wsRes)
	wsID := int64(wsRes.Workspace["id"].(float64))

	// Create plan run
	rec = doTFReq(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/terraform/workspaces/%d/runs", wsID), map[string]string{
		"type":    "plan",
		"message": "Deploy foo",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for run, got %d: %s", rec.Code, rec.Body.String())
	}
	var runRes struct {
		Run map[string]interface{} `json:"run"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &runRes)
	runID := int64(runRes.Run["id"].(float64))

	// Wait for plan to reach awaiting_approval
	deadline := time.Now().Add(5 * time.Second)
	var finalRun *store.TFRun
	for time.Now().Before(deadline) {
		r, err := db.GetTFRun(runID)
		if err == nil && r.Status == "awaiting_approval" {
			finalRun = r
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if finalRun == nil {
		t.Fatalf("run failed to reach awaiting_approval status in time")
	}
	if !planCalled {
		t.Fatalf("mockPlanner was never called")
	}

	// Verify workspace is locked by run:<id>
	ws, _ := db.GetTFWorkspace(wsID)
	if !ws.Locked || ws.LockedBy != fmt.Sprintf("run:%d", runID) {
		t.Fatalf("expected workspace locked by run:%d, got locked=%v, by=%q", runID, ws.Locked, ws.LockedBy)
	}

	// Check log endpoint
	recLog := doTFReq(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/terraform/runs/%d/log?offset=0", runID), nil)
	if recLog.Code != http.StatusOK {
		t.Fatalf("expected 200 for run log, got %d", recLog.Code)
	}
	var logData map[string]interface{}
	_ = json.Unmarshal(recLog.Body.Bytes(), &logData)
	if !strings.Contains(logData["log"].(string), "Plan: 1 to add") {
		t.Fatalf("expected plan text in log, got %q", logData["log"])
	}

	// Attempt apply with comment < 3 chars -> 400
	recApplyBad := doTFReq(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/terraform/runs/%d/apply", runID), map[string]string{
		"comment": "ok",
	})
	if recApplyBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for short comment, got %d", recApplyBad.Code)
	}

	// Apply with valid comment
	recApply := doTFReq(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/terraform/runs/%d/apply", runID), map[string]string{
		"comment": "Approved for production",
	})
	if recApply.Code != http.StatusOK {
		t.Fatalf("expected 200 for apply, got %d: %s", recApply.Code, recApply.Body.String())
	}

	// Wait for run to finish as applied
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r, err := db.GetTFRun(runID)
		if err == nil && r.Status == "applied" {
			finalRun = r
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if finalRun.Status != "applied" {
		t.Fatalf("expected run status applied, got %s", finalRun.Status)
	}
	if !applyCalled {
		t.Fatalf("mockApplier was never called")
	}

	// Verify state version was created in DB
	latestState, err := db.LatestTFStateVersion(wsID)
	if err != nil || latestState == nil {
		t.Fatalf("expected latest state version in DB, got err=%v", err)
	}
	if latestState.Source != "apply" {
		t.Fatalf("expected state source 'apply', got %s", latestState.Source)
	}

	// Verify workspace is unlocked now
	ws, _ = db.GetTFWorkspace(wsID)
	if ws.Locked {
		t.Fatalf("expected workspace to be unlocked after applied run, got locked=true")
	}
}

// 5. Public webhook trigger (bypasses authMiddleware)
func TestTerraformWebhookEndpoint(t *testing.T) {
	srv, db, tfDataRoot, _ := setupTerraformTestServer(t)

	// Create a workspace directly in DB
	token := store.GenerateTFWebhookToken()
	secret := "webhook-secret-123"
	ws := &store.TFWorkspace{
		Name:          "git-ws",
		Binary:        "terraform",
		Source:        "git",
		GitURL:        "https://github.com/org/repo.git",
		GitBranch:     "main",
		WebhookToken:  token,
		WebhookSecret: secret,
	}
	_, err := db.CreateTFWorkspace(ws)
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	// Mock planner
	mockPlanner := func(ctx context.Context, spec terraform.PlanSpec, log io.Writer) (*terraform.PlanResult, error) {
		return &terraform.PlanResult{
			ExitCode:   0,
			HasChanges: false,
		}, nil
	}
	mgr := NewTerraformRunManager(db, tfDataRoot, mockPlanner, nil)
	srv.SetTerraformRunManager(mgr)

	// POST to webhook endpoint WITHOUT auth cookie
	payload := `{"ref":"refs/heads/main"}`
	req := httptest.NewRequest(http.MethodPost, "/api/terraform/webhook/"+token, strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gitlab-Token", secret)
	rec := httptest.NewRecorder()

	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted for webhook, got %d: %s", rec.Code, rec.Body.String())
	}

	var res map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res["run_id"] == nil {
		t.Fatalf("expected run_id in webhook response, got %+v", res)
	}
}

// 6. Real integration test with /usr/bin/terraform if installed
func TestTerraformIntegrationReal(t *testing.T) {
	tfPath, err := exec.LookPath("terraform")
	if err != nil {
		t.Skip("terraform not installed in PATH, skipping real smoke integration test")
	}

	srv, db, tfDataRoot, auth := setupTerraformTestServer(t)
	_ = doTFReq(t, srv, auth, http.MethodPut, "/api/terraform/status", map[string]bool{"enabled": true})

	mgr := NewTerraformRunManager(db, tfDataRoot, nil, nil) // Real terraform.Plan & terraform.Apply
	srv.SetTerraformRunManager(mgr)

	// Create workspace with built-in terraform_data resource (no provider download required)
	body := map[string]interface{}{
		"name":   "real-smoke-ws",
		"binary": filepath.Base(tfPath),
		"source": "inline",
		"files": map[string]string{
			"main.tf": `
resource "terraform_data" "greeting" {
  input = "hello-pantau"
}
output "message" {
  value = terraform_data.greeting.output
}
`,
		},
	}
	rec := doTFReq(t, srv, auth, http.MethodPost, "/api/terraform/workspaces", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create workspace failed: %s", rec.Body.String())
	}
	var wsRes struct {
		Workspace map[string]interface{} `json:"workspace"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &wsRes)
	wsID := int64(wsRes.Workspace["id"].(float64))

	// 1. Trigger Plan
	rec = doTFReq(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/terraform/workspaces/%d/runs", wsID), map[string]string{
		"type":    "plan",
		"message": "Initial plan with terraform_data",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create plan run failed: %s", rec.Body.String())
	}
	var runRes struct {
		Run map[string]interface{} `json:"run"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &runRes)
	runID := int64(runRes.Run["id"].(float64))

	// Wait for plan to reach awaiting_approval
	deadline := time.Now().Add(60 * time.Second)
	var finalRun *store.TFRun
	for time.Now().Before(deadline) {
		r, err := db.GetTFRun(runID)
		if err == nil && (r.Status == "awaiting_approval" || r.Status == "errored") {
			finalRun = r
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if finalRun == nil || finalRun.Status != "awaiting_approval" {
		t.Fatalf("expected run to reach awaiting_approval, got status=%v, log=%s", finalRun, finalRun.Log)
	}

	// 2. Apply
	recApply := doTFReq(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/terraform/runs/%d/apply", runID), map[string]string{
		"comment": "Apply terraform_data resource",
	})
	if recApply.Code != http.StatusOK {
		t.Fatalf("apply failed: %s", recApply.Body.String())
	}

	// Wait for apply to reach applied
	deadline = time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		r, err := db.GetTFRun(runID)
		if err == nil && (r.Status == "applied" || r.Status == "errored") {
			finalRun = r
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if finalRun.Status != "applied" {
		t.Fatalf("expected run to be applied, got status=%s, log=%s", finalRun.Status, finalRun.Log)
	}

	// 3. Verify state version was created
	latestState, err := db.LatestTFStateVersion(wsID)
	if err != nil || latestState == nil {
		t.Fatalf("expected state version in DB after apply, err=%v", err)
	}

	// 4. Second plan should have no changes
	recSecond := doTFReq(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/terraform/workspaces/%d/runs", wsID), map[string]string{
		"type":    "plan",
		"message": "Second plan check",
	})
	if recSecond.Code != http.StatusCreated {
		t.Fatalf("second plan run create failed: %s", recSecond.Body.String())
	}
	var secondRunRes struct {
		Run map[string]interface{} `json:"run"`
	}
	_ = json.Unmarshal(recSecond.Body.Bytes(), &secondRunRes)
	secondRunID := int64(secondRunRes.Run["id"].(float64))

	deadline = time.Now().Add(60 * time.Second)
	var secondFinalRun *store.TFRun
	for time.Now().Before(deadline) {
		r, err := db.GetTFRun(secondRunID)
		if err == nil && (r.Status == "no_changes" || r.Status == "errored" || r.Status == "awaiting_approval") {
			secondFinalRun = r
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if secondFinalRun == nil || secondFinalRun.Status != "no_changes" {
		t.Fatalf("expected second plan to be no_changes, got status=%v, log=%s", secondFinalRun, secondFinalRun.Log)
	}
}
