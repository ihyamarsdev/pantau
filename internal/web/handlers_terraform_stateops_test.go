package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"pantau/internal/store"
	"pantau/internal/terraform"
)

// stateOpsEnv creates an enabled terraform server with one inline workspace.
func stateOpsEnv(t *testing.T, files map[string]string) (*Server, func(*http.Request), *TerraformRunManager, int64) {
	t.Helper()
	srv, db, root, auth := setupTerraformTestServer(t)
	if err := db.SetTerraformEnabled(true); err != nil {
		t.Fatal(err)
	}
	mgr := NewTerraformRunManager(db, root, nil, nil)
	srv.SetTerraformRunManager(mgr)
	fj, _ := json.Marshal(files)
	id, err := db.CreateTFWorkspace(&store.TFWorkspace{Name: "ops-ws", Source: "inline", Files: string(fj)})
	if err != nil {
		t.Fatal(err)
	}
	return srv, auth, mgr, id
}

func stateOpsPost(t *testing.T, srv *Server, auth func(*http.Request), id int64, body map[string]string) (int, map[string]interface{}) {
	t.Helper()
	rec := doTFReq(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/terraform/workspaces/%d/state/ops", id), body)
	var out map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestStateOpsRequestValidation(t *testing.T) {
	srv, auth, _, id := stateOpsEnv(t, map[string]string{"main.tf": ""})
	for name, body := range map[string]map[string]string{
		"bad op":        {"op": "taint", "address": "a.b"},
		"empty address": {"op": "rm"},
		"leading dash":  {"op": "rm", "address": "-force"},
		"whitespace":    {"op": "rm", "address": "a.b c"},
		"metachar":      {"op": "rm", "address": "a.b;ls"},
		"mv no target":  {"op": "mv", "address": "a.b"},
		"mv bad target": {"op": "mv", "address": "a.b", "to": "a.c|x"},
		"import no id":  {"op": "import", "address": "a.b"},
		"import bad id": {"op": "import", "address": "a.b", "import_id": "-x"},
	} {
		code, out := stateOpsPost(t, srv, auth, id, body)
		if code != http.StatusBadRequest || out["error"] == nil {
			t.Errorf("%s: code %d %v", name, code, out)
		}
	}
	if rec := doTFReq(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/terraform/workspaces/%d/state/ops", id), nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET code %d", rec.Code)
	}
	if ws, _ := srv.db.GetTFWorkspace(id); ws.Locked {
		t.Error("rejected requests must not lock the workspace")
	}
}

func TestStateOpsLockHandling(t *testing.T) {
	srv, auth, mgr, id := stateOpsEnv(t, map[string]string{"main.tf": ""})
	body := map[string]string{"op": "rm", "address": "a.b"}

	// Manually locked -> 423 and the lock is left alone.
	if ok, err := srv.db.TryLockTFWorkspace(id, "manual", "maintenance"); err != nil || !ok {
		t.Fatal(err)
	}
	if code, _ := stateOpsPost(t, srv, auth, id, body); code != 423 {
		t.Fatalf("locked: code %d", code)
	}
	if ws, _ := srv.db.GetTFWorkspace(id); !ws.Locked || ws.LockedBy != "manual" {
		t.Fatalf("manual lock was disturbed: %+v", ws)
	}
	_ = srv.db.UnlockTFWorkspace(id)

	// Active run -> 409.
	mgr.mu.Lock()
	mgr.activeRuns[99] = &activeTFRun{runID: 99, workspaceID: id}
	mgr.byWorkspace[id] = 99
	mgr.mu.Unlock()
	if code, _ := stateOpsPost(t, srv, auth, id, body); code != http.StatusConflict {
		t.Fatalf("active run: code %d", code)
	}
	mgr.mu.Lock()
	delete(mgr.activeRuns, 99)
	delete(mgr.byWorkspace, id)
	mgr.mu.Unlock()

	// Failure (workspace has no state) -> 422 and the lock is released.
	code, out := stateOpsPost(t, srv, auth, id, body)
	if code != http.StatusUnprocessableEntity || out["ok"] != false {
		t.Fatalf("error path: code %d %v", code, out)
	}
	if ws, _ := srv.db.GetTFWorkspace(id); ws.Locked {
		t.Fatal("lock not released after error")
	}
}

func TestStateOpsRealRmMv(t *testing.T) {
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform not installed")
	}
	srv, auth, mgr, id := stateOpsEnv(t, map[string]string{
		"main.tf": "resource \"terraform_data\" \"a\" {\n  input = \"x\"\n}\n\nresource \"terraform_data\" \"b\" {\n  input = \"y\"\n}\n",
	})
	ws, _ := srv.db.GetTFWorkspace(id)
	cfg, err := mgr.BuildRunConfig(ws)
	if err != nil {
		t.Fatal(err)
	}
	var sink strings.Builder
	if _, err := terraform.Plan(context.Background(), terraform.PlanSpec{Config: cfg, RunID: 1, Timeout: time.Minute}, &sink); err != nil {
		t.Fatalf("plan: %v %s", err, sink.String())
	}
	ar, err := terraform.Apply(context.Background(), terraform.ApplySpec{Config: cfg, RunID: 1, Timeout: time.Minute}, &sink)
	if err != nil || ar.ExitCode != 0 {
		t.Fatalf("apply: %v %s", err, sink.String())
	}
	if _, err := srv.db.CreateTFStateVersion(&store.TFStateVersion{WorkspaceID: id, Serial: 1, Source: "apply", State: ar.State, ResourceCount: 2}); err != nil {
		t.Fatal(err)
	}

	code, out := stateOpsPost(t, srv, auth, id, map[string]string{"op": "rm", "address": "terraform_data.a"})
	if code != 200 || out["ok"] != true || out["state_version"] == nil {
		t.Fatalf("rm: %d %v", code, out)
	}
	versions, _ := srv.db.ListTFStateVersions(id, 10)
	if len(versions) != 2 {
		t.Fatalf("want old version kept + new, got %d", len(versions))
	}
	latest, _ := srv.db.LatestTFStateVersion(id)
	if info, _ := terraform.ParseState(latest.State); len(info.Resources) != 1 || info.Resources[0].Address != "terraform_data.b" {
		t.Fatalf("after rm: %+v", info)
	}
	if latest.Source != "state rm terraform_data.a" || float64(latest.ID) != out["state_version"] {
		t.Fatalf("version meta: %+v / %v", latest, out["state_version"])
	}

	code, out = stateOpsPost(t, srv, auth, id, map[string]string{"op": "mv", "address": "terraform_data.b", "to": "terraform_data.c"})
	if code != 200 {
		t.Fatalf("mv: %d %v", code, out)
	}
	latest, _ = srv.db.LatestTFStateVersion(id)
	if info, _ := terraform.ParseState(latest.State); len(info.Resources) != 1 || info.Resources[0].Address != "terraform_data.c" {
		t.Fatalf("after mv: %+v", info)
	}
	if versions, _ = srv.db.ListTFStateVersions(id, 10); len(versions) != 3 {
		t.Fatalf("want 3 versions, got %d", len(versions))
	}
	if w, _ := srv.db.GetTFWorkspace(id); w.Locked {
		t.Fatal("lock not released")
	}
}
