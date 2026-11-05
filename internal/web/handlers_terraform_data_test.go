package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"pantau/internal/store"
)

func setupTerraformTestDataServer(t *testing.T) (*Server, func(*http.Request)) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_tf_data.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to init test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	srv := NewServer(db, nil, nil)
	// Enable terraform gate by default for data tests
	if err := db.SetTerraformEnabled(true); err != nil {
		t.Fatalf("failed to enable terraform gate: %v", err)
	}

	token := "tf_test_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	authReq := func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: "pantau_session", Value: token})
	}
	return srv, authReq
}

func doTFRequest(t *testing.T, srv *Server, authReq func(*http.Request), method, url string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		switch b := body.(type) {
		case string:
			bodyReader = bytes.NewBufferString(b)
		case []byte:
			bodyReader = bytes.NewBuffer(b)
		default:
			data, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal body: %v", err)
			}
			bodyReader = bytes.NewReader(data)
		}
	}
	req := httptest.NewRequest(method, url, bodyReader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if authReq != nil {
		authReq(req)
	}
	rec := httptest.NewRecorder()

	// Direct router helper for testing workspace data endpoints:
	// If path starts with /api/terraform/workspaces/{id}/...
	if len(req.URL.Path) > len("/api/terraform/workspaces/") && req.URL.Path[:len("/api/terraform/workspaces/")] == "/api/terraform/workspaces/" {
		restPath := req.URL.Path[len("/api/terraform/workspaces/"):]
		parts := splitClean(restPath)
		if len(parts) >= 2 {
			var wsID int64
			fmt.Sscanf(parts[0], "%d", &wsID)
			ws, _ := srv.db.GetTFWorkspace(wsID)
			if ws == nil {
				http.NotFound(rec, req)
				return rec
			}
			srv.handleTerraformWorkspaceData(rec, req, ws, parts[1:])
			return rec
		}
	}

	// Varset route
	if req.URL.Path == "/api/terraform/varsets" {
		srv.handleTerraformVarsets(rec, req)
		return rec
	}
	if len(req.URL.Path) > len("/api/terraform/varsets/") && req.URL.Path[:len("/api/terraform/varsets/")] == "/api/terraform/varsets/" {
		srv.handleTerraformVarsetRoute(rec, req)
		return rec
	}

	return rec
}

func splitClean(p string) []string {
	var res []string
	for _, part := range bytes.Split([]byte(p), []byte("/")) {
		if len(part) > 0 {
			res = append(res, string(part))
		}
	}
	return res
}

func TestTerraformVariables_SensitiveAndEffective(t *testing.T) {
	srv, auth := setupTerraformTestDataServer(t)

	// Create workspace
	ws := &store.TFWorkspace{Name: "ws-vars-test"}
	wsID, err := srv.db.CreateTFWorkspace(ws)
	if err != nil {
		t.Fatalf("create ws: %v", err)
	}

	// 1. Create a sensitive variable
	rec := doTFRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/terraform/workspaces/%d/variables", wsID), map[string]interface{}{
		"key":       "SECRET_KEY",
		"value":     "initial-secret",
		"category":  "env",
		"sensitive": true,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var createdVar map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &createdVar)
	if createdVar["value"] != "" {
		t.Fatalf("expected sensitive value to be blank, got %v", createdVar["value"])
	}
	if createdVar["value_set"] != true {
		t.Fatalf("expected value_set=true, got %v", createdVar["value_set"])
	}
	vid := int64(createdVar["id"].(float64))

	// Verify in DB that it is stored
	storedVar, err := srv.db.GetTFVariable(vid)
	if err != nil || storedVar.Value != "initial-secret" {
		t.Fatalf("expected initial-secret stored in db, got %v, err=%v", storedVar, err)
	}

	// 2. PUT with empty value must keep old sensitive value
	rec = doTFRequest(t, srv, auth, http.MethodPut, fmt.Sprintf("/api/terraform/workspaces/%d/variables/%d", wsID, vid), map[string]interface{}{
		"key":         "SECRET_KEY",
		"value":       "", // empty value
		"category":    "env",
		"sensitive":   true,
		"description": "updated desc",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	storedVar, _ = srv.db.GetTFVariable(vid)
	if storedVar.Value != "initial-secret" {
		t.Fatalf("expected value to be preserved as initial-secret, got %q", storedVar.Value)
	}
	if storedVar.Description != "updated desc" {
		t.Fatalf("expected desc updated, got %q", storedVar.Description)
	}

	// 3. Varset global & workspace effective variables test
	// Create global varset with a variable
	rec = doTFRequest(t, srv, auth, http.MethodPost, "/api/terraform/varsets", map[string]interface{}{
		"name":   "global-varset",
		"global": true,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var gvs store.TFVarset
	_ = json.Unmarshal(rec.Body.Bytes(), &gvs)

	doTFRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/terraform/varsets/%d/variables", gvs.ID), map[string]interface{}{
		"key":      "REGION",
		"value":    "us-east-1",
		"category": "terraform",
	})
	// Variable that gets overridden
	doTFRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/terraform/varsets/%d/variables", gvs.ID), map[string]interface{}{
		"key":      "SECRET_KEY",
		"value":    "global-secret",
		"category": "env",
	})

	// Query ?effective=1
	rec = doTFRequest(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/terraform/workspaces/%d/variables?effective=1", wsID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var effResp struct {
		Variables []map[string]interface{} `json:"variables"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &effResp)

	// REGION should have origin "varset:global-varset", SECRET_KEY should have origin "workspace"
	originMap := make(map[string]string)
	for _, v := range effResp.Variables {
		originMap[v["key"].(string)] = v["origin"].(string)
	}
	if originMap["REGION"] != "varset:global-varset" {
		t.Errorf("expected REGION origin 'varset:global-varset', got %q", originMap["REGION"])
	}
	if originMap["SECRET_KEY"] != "workspace" {
		t.Errorf("expected SECRET_KEY origin 'workspace', got %q", originMap["SECRET_KEY"])
	}
}

func TestTerraformState_ImportVersionsDiffRollback(t *testing.T) {
	srv, auth := setupTerraformTestDataServer(t)

	ws := &store.TFWorkspace{Name: "ws-state-test"}
	wsID, err := srv.db.CreateTFWorkspace(ws)
	if err != nil {
		t.Fatalf("create ws: %v", err)
	}

	stateV1 := `{
		"version": 4,
		"terraform_version": "1.5.0",
		"serial": 1,
		"lineage": "lineage-abc-123",
		"resources": [
			{
				"mode": "managed",
				"type": "null_resource",
				"name": "foo",
				"provider": "provider[\"registry.terraform.io/hashicorp/null\"]",
				"instances": [
					{
						"schema_version": 0,
						"attributes": {
							"id": "123",
							"triggers": null
						}
					}
				]
			}
		],
		"outputs": {
			"db_password": {
				"value": "supersecret",
				"type": "string",
				"sensitive": true
			},
			"endpoint": {
				"value": "db.example.com",
				"type": "string",
				"sensitive": false
			}
		}
	}`

	stateV2 := `{
		"version": 4,
		"terraform_version": "1.5.0",
		"serial": 2,
		"lineage": "lineage-abc-123",
		"resources": [
			{
				"mode": "managed",
				"type": "null_resource",
				"name": "foo",
				"provider": "provider[\"registry.terraform.io/hashicorp/null\"]",
				"instances": [
					{
						"schema_version": 0,
						"attributes": {
							"id": "456",
							"triggers": null
						}
					}
				]
			},
			{
				"mode": "managed",
				"type": "null_resource",
				"name": "bar",
				"provider": "provider[\"registry.terraform.io/hashicorp/null\"]",
				"instances": [
					{
						"schema_version": 0,
						"attributes": {
							"id": "789",
							"triggers": null
						}
					}
				]
			}
		]
	}`

	// 1. Import state V1
	rec := doTFRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/terraform/workspaces/%d/state/import", wsID), map[string]string{
		"state": stateV1,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var sv1 store.TFStateVersion
	_ = json.Unmarshal(rec.Body.Bytes(), &sv1)
	if sv1.Serial != 1 || sv1.Source != "import" {
		t.Fatalf("expected serial 1 and source import, got %d, %s", sv1.Serial, sv1.Source)
	}

	// 2. Import state V2
	rec = doTFRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/terraform/workspaces/%d/state/import", wsID), map[string]string{
		"state": stateV2,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var sv2 store.TFStateVersion
	_ = json.Unmarshal(rec.Body.Bytes(), &sv2)
	if sv2.Serial != 2 {
		t.Fatalf("expected serial 2, got %d", sv2.Serial)
	}

	// 3. Diff between sv1 and sv2
	rec = doTFRequest(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/terraform/workspaces/%d/state/diff?from=%d&to=%d", wsID, sv1.ID, sv2.ID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var diffResp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &diffResp)
	added, _ := diffResp["added"].([]interface{})
	if len(added) != 1 || added[0] != "null_resource.bar" {
		t.Fatalf("expected null_resource.bar added in diff, got %v", diffResp)
	}

	// 4. Rollback while locked should return 423
	_ = srv.db.SetTFWorkspaceLock(wsID, "maintenance", "manual")
	rec = doTFRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/terraform/workspaces/%d/state/versions/%d/rollback", wsID, sv1.ID), nil)
	if rec.Code != http.StatusLocked {
		t.Fatalf("expected 423 locked on rollback, got %d: %s", rec.Code, rec.Body.String())
	}

	// Unlock workspace
	_ = srv.db.UnlockTFWorkspace(wsID)

	// 5. Rollback to sv1 produces serial + 1 (serial = 2 + 1 = 3)
	rec = doTFRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/terraform/workspaces/%d/state/versions/%d/rollback", wsID, sv1.ID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 rollback, got %d: %s", rec.Code, rec.Body.String())
	}
	var rbResp struct {
		Version store.TFStateVersion `json:"version"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &rbResp)
	if rbResp.Version.Serial != 3 {
		t.Fatalf("expected rollback serial=3, got %d", rbResp.Version.Serial)
	}
	if rbResp.Version.Source != "rollback" {
		t.Fatalf("expected source=rollback, got %s", rbResp.Version.Source)
	}

	// 6. Test Outputs reveal rule
	// Rollback made state like sv1 (which had outputs)
	// Without reveal
	rec = doTFRequest(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/terraform/workspaces/%d/outputs", wsID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var outResp struct {
		Outputs []map[string]interface{} `json:"outputs"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &outResp)
	foundSensitive := false
	for _, o := range outResp.Outputs {
		if o["name"] == "db_password" {
			foundSensitive = true
			if o["value"] != nil {
				t.Fatalf("expected db_password value to be null without reveal, got %v", o["value"])
			}
			if o["value_hidden"] != true {
				t.Fatalf("expected value_hidden=true, got %v", o["value_hidden"])
			}
		}
	}
	if !foundSensitive {
		t.Fatalf("expected db_password in outputs")
	}

	// With reveal=1
	rec = doTFRequest(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/terraform/workspaces/%d/outputs?reveal=1", wsID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &outResp)
	for _, o := range outResp.Outputs {
		if o["name"] == "db_password" {
			if o["value"] != "supersecret" {
				t.Fatalf("expected revealed supersecret, got %v", o["value"])
			}
			if o["value_hidden"] != false {
				t.Fatalf("expected value_hidden=false with reveal, got %v", o["value_hidden"])
			}
		}
	}
}
