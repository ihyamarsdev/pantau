package store_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"pantau/internal/store"
)

func TestTerraformWorkspace_UniqueNameNocase(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	w1 := &store.TFWorkspace{
		Name:        "Production-Infra",
		Description: "Main VPC",
		Environment: "production",
	}
	id, err := db.CreateTFWorkspace(w1)
	if err != nil {
		t.Fatalf("CreateTFWorkspace failed: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}

	// Case-insensitive duplicate name
	w2 := &store.TFWorkspace{
		Name:        "production-infra",
		Description: "Duplicate name with lower case",
	}
	_, err = db.CreateTFWorkspace(w2)
	if !errors.Is(err, store.ErrTFWorkspaceNameTaken) {
		t.Fatalf("expected ErrTFWorkspaceNameTaken, got %v", err)
	}
}

func TestTerraformWorkspace_AtomicLockContention(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ws := &store.TFWorkspace{
		Name: "Lock-Contention-WS",
	}
	id, err := db.CreateTFWorkspace(ws)
	if err != nil {
		t.Fatalf("CreateTFWorkspace: %v", err)
	}

	// First lock attempt succeeds
	ok, err := db.TryLockTFWorkspace(id, "run:101", "Plan in progress")
	if err != nil {
		t.Fatalf("TryLockTFWorkspace error: %v", err)
	}
	if !ok {
		t.Fatalf("expected first TryLockTFWorkspace to succeed")
	}

	// Second lock attempt from another runner must fail atomically (contention)
	ok, err = db.TryLockTFWorkspace(id, "run:102", "Concurrent plan attempt")
	if err != nil {
		t.Fatalf("TryLockTFWorkspace second attempt error: %v", err)
	}
	if ok {
		t.Fatalf("expected second TryLockTFWorkspace to fail while locked")
	}

	// Verify locked_by is still run:101
	w, err := db.GetTFWorkspace(id)
	if err != nil {
		t.Fatalf("GetTFWorkspace: %v", err)
	}
	if w.LockedBy != "run:101" || !w.Locked {
		t.Fatalf("expected locked_by run:101 and Locked=true, got locked_by=%q locked=%v", w.LockedBy, w.Locked)
	}

	// Unlock
	if err := db.UnlockTFWorkspace(id); err != nil {
		t.Fatalf("UnlockTFWorkspace: %v", err)
	}

	// Lock should now succeed
	ok, err = db.TryLockTFWorkspace(id, "manual", "Admin maintenance")
	if err != nil || !ok {
		t.Fatalf("expected lock after unlock to succeed, ok=%v, err=%v", ok, err)
	}
}

func TestTerraformVariables_EffectivePrecedence(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// 1. Create 2 workspaces
	ws1ID, err := db.CreateTFWorkspace(&store.TFWorkspace{Name: "app-prod"})
	if err != nil {
		t.Fatal(err)
	}
	ws2ID, err := db.CreateTFWorkspace(&store.TFWorkspace{Name: "app-staging"})
	if err != nil {
		t.Fatal(err)
	}

	// 2. Global varset: region="us-east-1", env="global"
	vsGlobalID, err := db.CreateTFVarset(&store.TFVarset{
		Name:   "Global Defaults",
		Global: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = db.CreateTFVariable(&store.TFVariable{VarsetID: vsGlobalID, Key: "region", Value: "us-east-1", Category: "terraform"})
	_, _ = db.CreateTFVariable(&store.TFVariable{VarsetID: vsGlobalID, Key: "tier", Value: "standard", Category: "terraform"})

	// 3. Connected varset linked to ws1: region="us-west-2", team="devops"
	vsConnID, err := db.CreateTFVarset(&store.TFVarset{
		Name:         "Prod Override",
		Global:       false,
		WorkspaceIDs: []int64{ws1ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = db.CreateTFVariable(&store.TFVariable{VarsetID: vsConnID, Key: "region", Value: "us-west-2", Category: "terraform"})
	_, _ = db.CreateTFVariable(&store.TFVariable{VarsetID: vsConnID, Key: "team", Value: "devops", Category: "env"})

	// 4. Workspace-level variable for ws1: region="ap-southeast-1" (highest precedence), app="pantau"
	_, _ = db.CreateTFVariable(&store.TFVariable{WorkspaceID: ws1ID, Key: "region", Value: "ap-southeast-1", Category: "terraform"})
	_, _ = db.CreateTFVariable(&store.TFVariable{WorkspaceID: ws1ID, Key: "app", Value: "pantau", Category: "terraform"})

	// Verify EffectiveTFVariables for ws1
	vars1, err := db.EffectiveTFVariables(ws1ID)
	if err != nil {
		t.Fatalf("EffectiveTFVariables ws1: %v", err)
	}

	byKey := make(map[string]store.TFVariable)
	for _, v := range vars1 {
		byKey[v.Key] = v
	}

	// "region" should resolve to workspace ("ap-southeast-1") with origin "workspace"
	if reg, ok := byKey["region"]; !ok || reg.Value != "ap-southeast-1" || reg.Origin != "workspace" {
		t.Fatalf("expected region=ap-southeast-1 origin=workspace, got %+v", reg)
	}
	// "tier" should resolve to global varset with origin "varset:Global Defaults"
	if tier, ok := byKey["tier"]; !ok || tier.Value != "standard" || tier.Origin != "varset:Global Defaults" {
		t.Fatalf("expected tier=standard origin=varset:Global Defaults, got %+v", tier)
	}
	// "team" should resolve to connected varset with origin "varset:Prod Override"
	if team, ok := byKey["team"]; !ok || team.Value != "devops" || team.Origin != "varset:Prod Override" {
		t.Fatalf("expected team=devops origin=varset:Prod Override, got %+v", team)
	}
	// "app" should resolve to workspace
	if app, ok := byKey["app"]; !ok || app.Value != "pantau" || app.Origin != "workspace" {
		t.Fatalf("expected app=pantau origin=workspace, got %+v", app)
	}

	// Verify ws2 (not connected to Prod Override): region should come from global ("us-east-1")
	vars2, err := db.EffectiveTFVariables(ws2ID)
	if err != nil {
		t.Fatalf("EffectiveTFVariables ws2: %v", err)
	}
	byKey2 := make(map[string]store.TFVariable)
	for _, v := range vars2 {
		byKey2[v.Key] = v
	}
	if reg, ok := byKey2["region"]; !ok || reg.Value != "us-east-1" || reg.Origin != "varset:Global Defaults" {
		t.Fatalf("expected ws2 region=us-east-1 from global, got %+v", reg)
	}
	if _, ok := byKey2["team"]; ok {
		t.Fatalf("ws2 should not have team varset override")
	}
}

func TestTerraformPruning_RunsAndStateVersions(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	wsID, err := db.CreateTFWorkspace(&store.TFWorkspace{Name: "prune-test"})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Create 5 finished runs and 1 active run
	for i := 1; i <= 5; i++ {
		_, err := db.CreateTFRun(&store.TFRun{
			WorkspaceID: wsID,
			Type:        "plan",
			Status:      "applied",
			Message:     fmt.Sprintf("Run %d", i),
		})
		if err != nil {
			t.Fatalf("CreateTFRun %d: %v", i, err)
		}
	}
	activeRunID, err := db.CreateTFRun(&store.TFRun{
		WorkspaceID: wsID,
		Type:        "plan",
		Status:      "planning",
		Message:     "Active planning run",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Prune runs, keep newest 2
	if err := db.PruneTFRuns(wsID, 2); err != nil {
		t.Fatalf("PruneTFRuns: %v", err)
	}

	runs, err := db.ListTFRuns(wsID, 100)
	if err != nil {
		t.Fatal(err)
	}
	// Expected 3 runs: 1 active run (never pruned) + 2 newest finished runs
	if len(runs) != 3 {
		t.Fatalf("expected 3 runs after pruning (1 active + 2 finished), got %d", len(runs))
	}
	foundActive := false
	for _, r := range runs {
		if r.ID == activeRunID {
			foundActive = true
		}
	}
	if !foundActive {
		t.Fatalf("active run was pruned unexpectedly")
	}

	// 2. Create 5 state versions
	for i := 1; i <= 5; i++ {
		_, err := db.CreateTFStateVersion(&store.TFStateVersion{
			WorkspaceID:   wsID,
			Serial:        int64(i),
			Lineage:       "test-lineage",
			Source:        "apply",
			Size:          100,
			ResourceCount: i,
			SHA256:        fmt.Sprintf("sha-%d", i),
			State:         []byte(fmt.Sprintf(`{"serial": %d}`, i)),
		})
		if err != nil {
			t.Fatalf("CreateTFStateVersion %d: %v", i, err)
		}
	}

	// Prune state versions, keep newest 2
	if err := db.PruneTFStateVersions(wsID, 2); err != nil {
		t.Fatalf("PruneTFStateVersions: %v", err)
	}

	states, err := db.ListTFStateVersions(wsID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 2 {
		t.Fatalf("expected 2 state versions, got %d", len(states))
	}

	// Latest state version must still be serial 5
	latest, err := db.LatestTFStateVersion(wsID)
	if err != nil || latest == nil {
		t.Fatalf("LatestTFStateVersion: %v", err)
	}
	if latest.Serial != 5 {
		t.Fatalf("expected latest serial 5, got %d", latest.Serial)
	}

	// Prune with keep 0 must still keep the newest state version
	if err := db.PruneTFStateVersions(wsID, 0); err != nil {
		t.Fatalf("PruneTFStateVersions with keep 0: %v", err)
	}
	statesAfterZero, err := db.ListTFStateVersions(wsID, 100)
	if err != nil || len(statesAfterZero) != 1 {
		t.Fatalf("expected keep 0 to retain 1 latest version, got %d", len(statesAfterZero))
	}
	if statesAfterZero[0].Serial != 5 {
		t.Fatalf("expected retained version to be serial 5, got %d", statesAfterZero[0].Serial)
	}
}

func TestTerraformSensitiveFields_NeverJSONMarshalled(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// 1. Workspace with git_token and webhook_secret
	ws := &store.TFWorkspace{
		Name:          "Secret-WS",
		GitAuth:       "token",
		GitToken:      "super-secret-token",
		WebhookSecret: "hmac-secret-xyz",
	}
	wsID, err := db.CreateTFWorkspace(ws)
	if err != nil {
		t.Fatal(err)
	}

	loadedWS, err := db.GetTFWorkspace(wsID)
	if err != nil {
		t.Fatal(err)
	}
	wsBytes, err := json.Marshal(loadedWS)
	if err != nil {
		t.Fatal(err)
	}
	wsStr := string(wsBytes)
	if strings.Contains(wsStr, "super-secret-token") || strings.Contains(wsStr, `"git_token"`) {
		t.Fatalf("git_token must NEVER be in JSON serialization: %s", wsStr)
	}
	if strings.Contains(wsStr, "hmac-secret-xyz") || strings.Contains(wsStr, `"webhook_secret"`) {
		t.Fatalf("webhook_secret must NEVER be in JSON serialization: %s", wsStr)
	}
	if !loadedWS.GitTokenSet {
		t.Fatalf("GitTokenSet must be true when token is present")
	}

	// 2. Run with Log
	runID, err := db.CreateTFRun(&store.TFRun{
		WorkspaceID: wsID,
		Type:        "plan",
		Status:      "errored",
		Log:         "terraform output contains secrets and stacktraces",
	})
	if err != nil {
		t.Fatal(err)
	}
	loadedRun, err := db.GetTFRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	runBytes, err := json.Marshal(loadedRun)
	if err != nil {
		t.Fatal(err)
	}
	runStr := string(runBytes)
	if strings.Contains(runStr, "secrets and stacktraces") || strings.Contains(runStr, `"log"`) {
		t.Fatalf("run Log must NEVER appear in JSON serialization: %s", runStr)
	}

	// 3. StateVersion with State
	svID, err := db.CreateTFStateVersion(&store.TFStateVersion{
		WorkspaceID: wsID,
		Serial:      1,
		State:       []byte(`{"sensitive_data": "secret_state_blob"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	loadedSV, err := db.GetTFStateVersion(svID)
	if err != nil {
		t.Fatal(err)
	}
	svBytes, err := json.Marshal(loadedSV)
	if err != nil {
		t.Fatal(err)
	}
	svStr := string(svBytes)
	if strings.Contains(svStr, "secret_state_blob") || strings.Contains(svStr, `"state"`) {
		t.Fatalf("state BLOB must NEVER appear in JSON serialization of TFStateVersion: %s", svStr)
	}
}

func TestTerraformWorkspace_DeleteCascadesAll(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	wsID, err := db.CreateTFWorkspace(&store.TFWorkspace{Name: "Cascade-WS"})
	if err != nil {
		t.Fatal(err)
	}

	// Attach variable
	vID, err := db.CreateTFVariable(&store.TFVariable{WorkspaceID: wsID, Key: "foo", Value: "bar"})
	if err != nil {
		t.Fatal(err)
	}

	// Attach run
	rID, err := db.CreateTFRun(&store.TFRun{WorkspaceID: wsID, Type: "plan", Status: "applied"})
	if err != nil {
		t.Fatal(err)
	}

	// Attach state version
	svID, err := db.CreateTFStateVersion(&store.TFStateVersion{WorkspaceID: wsID, Serial: 1})
	if err != nil {
		t.Fatal(err)
	}

	// Delete workspace
	if err := db.DeleteTFWorkspace(wsID); err != nil {
		t.Fatalf("DeleteTFWorkspace: %v", err)
	}

	// Variable must be deleted
	_, err = db.GetTFVariable(vID)
	if !errors.Is(err, store.ErrTFVariableNotFound) {
		t.Fatalf("expected ErrTFVariableNotFound, got %v", err)
	}

	// Run must be deleted
	_, err = db.GetTFRun(rID)
	if !errors.Is(err, store.ErrTFRunNotFound) {
		t.Fatalf("expected ErrTFRunNotFound, got %v", err)
	}

	// State version must be deleted
	_, err = db.GetTFStateVersion(svID)
	if !errors.Is(err, store.ErrTFStateVersionNotFound) {
		t.Fatalf("expected ErrTFStateVersionNotFound, got %v", err)
	}
}

func TestTerraformRun_MarkInterrupted(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	wsID, _ := db.CreateTFWorkspace(&store.TFWorkspace{Name: "interrupt-test"})

	rQueued, _ := db.CreateTFRun(&store.TFRun{WorkspaceID: wsID, Status: "queued"})
	rPlanning, _ := db.CreateTFRun(&store.TFRun{WorkspaceID: wsID, Status: "planning"})
	rApplying, _ := db.CreateTFRun(&store.TFRun{WorkspaceID: wsID, Status: "applying"})
	rAwaiting, _ := db.CreateTFRun(&store.TFRun{WorkspaceID: wsID, Status: "awaiting_approval"})
	rApplied, _ := db.CreateTFRun(&store.TFRun{WorkspaceID: wsID, Status: "applied"})

	n, err := db.MarkTFRunsInterrupted()
	if err != nil {
		t.Fatalf("MarkTFRunsInterrupted: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3 runs interrupted, got %d", n)
	}

	// Verify queued, planning, applying are now interrupted
	for _, rid := range []int64{rQueued, rPlanning, rApplying} {
		r, err := db.GetTFRun(rid)
		if err != nil || r.Status != "interrupted" || r.FinishedAt == nil {
			t.Fatalf("run %d should be interrupted with finished_at set: %+v", rid, r)
		}
	}

	// awaiting_approval must NOT be touched
	rAw, err := db.GetTFRun(rAwaiting)
	if err != nil || rAw.Status != "awaiting_approval" {
		t.Fatalf("awaiting_approval run should remain awaiting_approval, got %+v", rAw)
	}

	// applied must NOT be touched
	rAp, err := db.GetTFRun(rApplied)
	if err != nil || rAp.Status != "applied" {
		t.Fatalf("applied run should remain applied, got %+v", rAp)
	}
}

func TestTerraformSnapshot_RoundTrip(t *testing.T) {
	db1, cleanup1 := setupTestDB(t)
	defer cleanup1()

	// 1. Setup workspace in db1
	wsID, err := db1.CreateTFWorkspace(&store.TFWorkspace{
		Name:                 "prod-vpc",
		Description:          "Primary VPC",
		Environment:          "production",
		Tags:                 `["aws","vpc"]`,
		Binary:               "tofu",
		Source:               "git",
		GitURL:               "git@github.com:example/infra.git",
		GitBranch:            "main",
		GitPath:              "envs/prod",
		GitAuth:              "token",
		GitToken:             "ghp_secretToken123",
		WebhookToken:         store.GenerateTFWebhookToken(),
		WebhookSecret:        store.GenerateTFWebhookToken(),
		DriftIntervalMinutes: 60,
	})
	if err != nil {
		t.Fatalf("create ws: %v", err)
	}

	// 2. Setup variable
	_, err = db1.CreateTFVariable(&store.TFVariable{
		WorkspaceID: wsID,
		Key:         "db_password",
		Value:       "supersecretpass",
		Category:    "terraform",
		Sensitive:   true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 3. Setup varset
	vsID, err := db1.CreateTFVarset(&store.TFVarset{
		Name:         "Shared AWS Keys",
		Global:       false,
		WorkspaceIDs: []int64{wsID},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db1.CreateTFVariable(&store.TFVariable{
		VarsetID:  vsID,
		Key:       "AWS_REGION",
		Value:     "us-east-1",
		Category:  "env",
		Sensitive: false,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 4. Setup policy
	_, err = db1.CreateTFPolicy(&store.TFPolicy{
		Name:        "deny-delete-all",
		Description: "Prevent destroy",
		Kind:        "deny_destroy",
		Enforcement: "hard",
		Enabled:     true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 5. Setup module & version
	mID, err := db1.CreateTFModule(&store.TFModule{
		Name:        "vpc",
		Provider:    "aws",
		Description: "Reusable VPC module",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db1.CreateTFModuleVersion(&store.TFModuleVersion{
		ModuleID:  mID,
		Version:   "1.0.0",
		Files:     `{"main.tf":"resource \"null_resource\" \"x\" {}"}`,
		Doc:       `{"readme":"VPC Module"}`,
		SourceURL: "https://github.com/example/vpc",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 6. Setup runs (these must NOT be in snapshot)
	runID, err := db1.CreateTFRun(&store.TFRun{
		WorkspaceID: wsID,
		Type:        "plan",
		Status:      "applied",
		Log:         "sensitive apply log that should be excluded",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 7. Setup state version 1 and 2 (only latest version 2 should be in snapshot as 'import')
	_, _ = db1.CreateTFStateVersion(&store.TFStateVersion{
		WorkspaceID:   wsID,
		RunID:         runID,
		Serial:        1,
		Lineage:       "lin-1",
		Source:        "apply",
		Size:          100,
		ResourceCount: 1,
		SHA256:        "sha-1",
		State:         []byte(`{"version": 4, "serial": 1}`),
	})
	_, _ = db1.CreateTFStateVersion(&store.TFStateVersion{
		WorkspaceID:   wsID,
		RunID:         runID,
		Serial:        2,
		Lineage:       "lin-1",
		Source:        "apply",
		Size:          200,
		ResourceCount: 2,
		SHA256:        "sha-2",
		State:         []byte(`{"version": 4, "serial": 2}`),
	})

	// Export Snapshot
	payload, err := db1.ExportSnapshot()
	if err != nil {
		t.Fatalf("ExportSnapshot: %v", err)
	}

	// Assert payload contents
	if len(payload.TerraformWorkspaces) != 1 {
		t.Fatalf("expected 1 workspace in snapshot, got %d", len(payload.TerraformWorkspaces))
	}
	if len(payload.TerraformVariables) != 2 {
		t.Fatalf("expected 2 variables in snapshot, got %d", len(payload.TerraformVariables))
	}
	if len(payload.TerraformVarsets) != 1 {
		t.Fatalf("expected 1 varset in snapshot, got %d", len(payload.TerraformVarsets))
	}
	if len(payload.TerraformPolicies) != 1 {
		t.Fatalf("expected 1 policy in snapshot, got %d", len(payload.TerraformPolicies))
	}
	if len(payload.TerraformModules) != 1 || len(payload.TerraformModules[0].Versions) != 1 {
		t.Fatalf("expected 1 module with 1 version, got %+v", payload.TerraformModules)
	}
	if len(payload.TerraformStates) != 1 {
		t.Fatalf("expected 1 latest state per workspace in snapshot, got %d", len(payload.TerraformStates))
	}
	if payload.TerraformStates[0].Serial != 2 {
		t.Fatalf("expected latest state serial 2, got %d", payload.TerraformStates[0].Serial)
	}

	// 8. Restore into db2
	db2, cleanup2 := setupTestDB(t)
	defer cleanup2()

	// In db2, add pre-existing dummy workspace and run to test clear-on-restore
	dummyWSID, _ := db2.CreateTFWorkspace(&store.TFWorkspace{Name: "Old-WS"})
	dummyWS2ID, _ := db2.CreateTFWorkspace(&store.TFWorkspace{Name: "Old-WS-2"})
	dummyRID, _ := db2.CreateTFRun(&store.TFRun{WorkspaceID: dummyWSID, Status: "applied"})

	if err := db2.ImportSnapshot(payload); err != nil {
		t.Fatalf("ImportSnapshot: %v", err)
	}

	// Check that dummy entries were cleared
	foundOldWS, err := db2.GetTFWorkspace(dummyWS2ID)
	if !errors.Is(err, store.ErrTFWorkspaceNotFound) {
		t.Fatalf("expected old workspace to be cleared on restore, err=%v, foundOldWS=%+v", err, foundOldWS)
	}
	foundRun, err := db2.GetTFRun(dummyRID)
	if !errors.Is(err, store.ErrTFRunNotFound) {
		t.Fatalf("expected old run to be cleared on restore, err=%v, foundRun=%+v", err, foundRun)
	}

	// Runs from db1 should NOT be restored
	runs, err := db2.ListTFRuns(wsID, 100)
	if err != nil || len(runs) != 0 {
		t.Fatalf("runs must NOT be restored by snapshot, got %d runs", len(runs))
	}

	// Declarations should be restored
	wsRestored, err := db2.GetTFWorkspace(wsID)
	if err != nil {
		t.Fatalf("GetTFWorkspace on db2: %v", err)
	}
	if wsRestored.Name != "prod-vpc" || wsRestored.GitToken != "ghp_secretToken123" {
		t.Fatalf("restored workspace mismatch: %+v", wsRestored)
	}

	// Latest state should be restored as source 'import'
	latestRestored, err := db2.LatestTFStateVersion(wsID)
	if err != nil || latestRestored == nil {
		t.Fatalf("LatestTFStateVersion on db2: %v", err)
	}
	if latestRestored.Serial != 2 || latestRestored.Source != "import" || string(latestRestored.State) != `{"version": 4, "serial": 2}` {
		t.Fatalf("restored state mismatch: %+v", latestRestored)
	}

	// Effective variables in db2 should work as before
	effVars, err := db2.EffectiveTFVariables(wsID)
	if err != nil || len(effVars) != 2 {
		t.Fatalf("EffectiveTFVariables on db2 failed: %v, len=%d", err, len(effVars))
	}
}
