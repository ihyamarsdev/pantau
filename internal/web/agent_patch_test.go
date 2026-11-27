package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pantau/internal/store"
)

func TestAgentPatchPrepareAndApplyAnsibleAndTerraform(t *testing.T) {
	t.Parallel()
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	_ = db.SetAnsibleEnabled(true)
	_ = db.SetTerraformEnabled(true)

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
	t.Parallel()
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

func TestAgentPatchGatesAndBaseConcurrency(t *testing.T) {
	t.Parallel()
	const target = "/srv/app/motd.txt"
	liveBase := patchSHA256("hello\n")
	staleBase := patchSHA256("old\n")
	readLive := func(int64, string) (string, error) { return "hello\n", nil }
	readDown := func(int64, string) (string, error) { return "", errors.New("ssh unreachable") }

	tests := []struct {
		name     string
		kind     string
		apply    bool
		prep     func(t *testing.T, db *store.DB) map[string]any // side effects + extra body fields
		read     func(int64, string) (string, error)
		base     string
		wantCode int
	}{
		{
			name: "ansible dry-run refused while ansible is disabled", kind: "ansible_playbook",
			prep: func(t *testing.T, db *store.DB) map[string]any {
				_ = db.SetAnsibleEnabled(false)
				return map[string]any{"playbook_id": 1}
			},
			wantCode: http.StatusForbidden,
		},
		{
			name: "terraform dry-run refused while terraform is disabled", kind: "terraform_hcl",
			prep: func(t *testing.T, db *store.DB) map[string]any {
				_ = db.SetTerraformEnabled(false)
				return map[string]any{"workspace_id": 1}
			},
			wantCode: http.StatusForbidden,
		},
		{
			name: "terraform apply refused while iac gate is on", kind: "terraform_hcl", apply: true,
			prep: func(t *testing.T, db *store.DB) map[string]any {
				_ = db.SetTerraformEnabled(true)
				_ = db.SetSetting("iac_gate_enabled", "1")
				return map[string]any{"workspace_id": 1}
			},
			wantCode: http.StatusConflict,
		},
		{
			name: "terraform apply passes the gate when iac gate is off", kind: "terraform_hcl", apply: true,
			prep: func(t *testing.T, db *store.DB) map[string]any {
				_ = db.SetTerraformEnabled(true)
				return map[string]any{"workspace_id": 999}
			},
			wantCode: http.StatusNotFound, // reaches the workspace lookup
		},
		{
			name: "k8s dry-run refused on read-only host", kind: "k8s_yaml",
			prep: func(t *testing.T, db *store.DB) map[string]any {
				hid, err := db.CreateHost(&store.Host{Name: "k8s-ro-dry", Host: "10.0.0.11", Port: 22, User: "root", KubeMode: "read-only"})
				if err != nil {
					t.Fatalf("add host: %v", err)
				}
				return map[string]any{"host_id": hid}
			},
			wantCode: http.StatusForbidden,
		},
		{
			name: "sftp dry-run without base is refused", kind: "sftp_file", read: readLive,
			wantCode: http.StatusBadRequest,
		},
		{
			name: "sftp apply with stale base conflicts", kind: "sftp_file", apply: true, read: readLive, base: staleBase,
			wantCode: http.StatusConflict,
		},
		{
			name: "sftp apply with unreadable host fails closed", kind: "sftp_file", apply: true, read: readDown, base: liveBase,
			wantCode: http.StatusBadGateway,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, db, cleanup := setupTestServer(t)
			defer cleanup()
			body := map[string]any{"kind": tc.kind, "host_id": 1, "target": target, "after": "new content\n", "base_sha256": tc.base}
			if tc.prep != nil {
				for k, v := range tc.prep(t, db) {
					body[k] = v
				}
			}
			srv.readPatchFileFn = tc.read

			handler := srv.handleAgentPatchDryRun
			if tc.apply {
				handler = srv.handleAgentPatchApply
			}
			raw, _ := json.Marshal(body)
			w := httptest.NewRecorder()
			handler(w, httptest.NewRequest(http.MethodPost, "/api/agent/patch", strings.NewReader(string(raw))))
			if w.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.wantCode, w.Body.String())
			}
		})
	}
}

func TestAgentPatchSFTPBaseMatchPasses(t *testing.T) {
	t.Parallel()
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()
	srv.readPatchFileFn = func(int64, string) (string, error) { return "hello\n", nil }

	w := httptest.NewRecorder()
	req := agentPatchReq{HostID: 1, Target: "/srv/app/motd.txt", BaseSHA256: patchSHA256("hello\n")}
	if srv.sftpBaseConflict(w, req) {
		t.Fatalf("matching base was refused: %d %s", w.Code, w.Body.String())
	}
	if w.Body.Len() != 0 {
		t.Fatalf("expected no response written on match, got %s", w.Body.String())
	}
}

func jsonNumber(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}
