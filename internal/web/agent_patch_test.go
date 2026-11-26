package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pantau/internal/store"
)

func TestAgentPatchPrepareAndApplyAnsibleAndTerraform(t *testing.T) {
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()

	pbID, err := db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{
		Name:    "test-pb",
		Source:  "inline",
		Content: "- hosts: all\n  tasks:\n    - ping:\n",
	})
	if err != nil {
		t.Fatalf("create playbook: %v", err)
	}

	ws := &store.TFWorkspace{
		Name:        "test-ws",
		Environment: "dev",
		Binary:      "terraform",
		Source:      "inline",
		Files:       `{"main.tf":"resource \"null_resource\" \"a\" {}\n"}`,
	}
	wsID, err := db.CreateTFWorkspace(ws)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	ws.ID = wsID

	// Prepare resolves live "before" for ansible_playbook
	w := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]any{
		"kind":        "ansible_playbook",
		"playbook_id": pbID,
		"after":       "- hosts: all\n  tasks:\n    - debug:\n        msg: ok\n",
	})
	srv.handleAgentPatchPrepare(w, httptest.NewRequest(http.MethodPost, "/api/agent/patch/prepare", strings.NewReader(string(body))))
	if w.Code != http.StatusOK {
		t.Fatalf("prepare ansible status %d: %s", w.Code, w.Body.String())
	}
	var prepResp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &prepResp)
	if !strings.Contains(prepResp["before"].(string), "ping:") {
		t.Fatalf("expected live playbook before content, got %v", prepResp["before"])
	}

	// Apply updates ansible_playbook in DB
	w = httptest.NewRecorder()
	srv.handleAgentPatchApply(w, httptest.NewRequest(http.MethodPost, "/api/agent/patch/apply", strings.NewReader(string(body))))
	if w.Code != http.StatusOK {
		t.Fatalf("apply ansible status %d: %s", w.Code, w.Body.String())
	}
	pb, _ := db.GetAnsiblePlaybook(pbID)
	if pb == nil || !strings.Contains(pb.Content, "msg: ok") {
		t.Fatalf("playbook not updated: %+v", pb)
	}

	// Prepare resolves live "before" for terraform_hcl
	w = httptest.NewRecorder()
	tfBody, _ := json.Marshal(map[string]any{
		"kind":         "terraform_hcl",
		"workspace_id": ws.ID,
		"target":       "main.tf",
		"after":        "resource \"null_resource\" \"b\" {}\n",
	})
	srv.handleAgentPatchPrepare(w, httptest.NewRequest(http.MethodPost, "/api/agent/patch/prepare", strings.NewReader(string(tfBody))))
	if w.Code != http.StatusOK {
		t.Fatalf("prepare terraform status %d: %s", w.Code, w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &prepResp)
	if !strings.Contains(prepResp["before"].(string), `"a"`) {
		t.Fatalf("expected live tf before content, got %v", prepResp["before"])
	}

	// Apply updates terraform_hcl file in DB
	w = httptest.NewRecorder()
	srv.handleAgentPatchApply(w, httptest.NewRequest(http.MethodPost, "/api/agent/patch/apply", strings.NewReader(string(tfBody))))
	if w.Code != http.StatusOK {
		t.Fatalf("apply terraform status %d: %s", w.Code, w.Body.String())
	}
	gotWs, _ := db.GetTFWorkspace(ws.ID)
	if gotWs == nil || !strings.Contains(gotWs.Files, `\"b\"`) {
		t.Fatalf("workspace files not updated: %+v", gotWs)
	}
}

func TestAgentPatchGuardsProtectedAndReadOnly(t *testing.T) {
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()

	// Protected SFTP path must be rejected on both dry-run and apply
	for _, fn := range []func(http.ResponseWriter, *http.Request){srv.handleAgentPatchDryRun, srv.handleAgentPatchApply} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/agent/patch", strings.NewReader(`{"kind":"sftp_file","host_id":1,"target":"/etc/passwd","after":"root::0:0::/root:/bin/sh"}`))
		fn(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for protected path, got %d: %s", w.Code, w.Body.String())
		}
	}

	// Invalid JSON dry-run for sftp_file .json returns valid:false
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/agent/patch/dry-run", strings.NewReader(`{"kind":"sftp_file","host_id":1,"target":"/opt/app/config.json","after":"{bad json"}`))
	srv.handleAgentPatchDryRun(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"valid":false`) {
		t.Fatalf("expected valid:false for bad json, got %d: %s", w.Code, w.Body.String())
	}

	// Read-only Kubernetes host blocks apply
	hid, err := db.CreateHost(&store.Host{Name: "k8s-ro", Host: "10.0.0.10", Port: 22, User: "root", KubeMode: "read-only"})
	if err != nil {
		t.Fatalf("add host: %v", err)
	}
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/agent/patch/apply", strings.NewReader(`{"kind":"k8s_yaml","host_id":`+jsonNumber(hid)+`,"after":"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm1\n"}`))
	srv.handleAgentPatchApply(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for read-only k8s host, got %d: %s", w.Code, w.Body.String())
	}
}

func jsonNumber(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}
