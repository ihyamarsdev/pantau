package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"pantau/internal/store"
)

func TestTerraformRunsFeedEndpoint(t *testing.T) {
	t.Parallel()
	srv, db, _, auth := setupTerraformTestServer(t)
	_ = doTFReq(t, srv, auth, http.MethodPut, "/api/terraform/status", map[string]bool{"enabled": true})

	wsID, err := db.CreateTFWorkspace(&store.TFWorkspace{Name: "feed-ws", Environment: "staging"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateTFRun(&store.TFRun{WorkspaceID: wsID, Type: "plan", Status: "awaiting_approval", TriggerKind: "manual"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateTFRun(&store.TFRun{WorkspaceID: wsID, Type: "plan", Status: "applied", TriggerKind: "webhook"}); err != nil {
		t.Fatal(err)
	}

	rec := doTFReq(t, srv, auth, http.MethodGet, "/api/terraform/runs?status=awaiting_approval", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Runs []map[string]interface{} `json:"runs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Runs) != 1 {
		t.Fatalf("want 1 awaiting run, got %d", len(res.Runs))
	}
	r := res.Runs[0]
	if r["workspace_name"] != "feed-ws" || r["workspace_environment"] != "staging" || r["status"] != "awaiting_approval" {
		t.Fatalf("unexpected run payload: %v", r)
	}
	if _, ok := r["counts"]; !ok {
		t.Fatalf("summary counts missing: %v", r)
	}

	if rec := doTFReq(t, srv, auth, http.MethodPost, "/api/terraform/runs", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST should be 405, got %d", rec.Code)
	}
}
