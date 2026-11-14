package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pantau/internal/store"
)

// iacTestServer builds a server with an authenticated session.
func iacTestServer(t *testing.T) (*Server, *store.DB, *http.Cookie, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "pantau-iac-web-test-*")
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(db, nil, nil)
	token := "iac-test-token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	cookie := &http.Cookie{Name: "pantau_session", Value: token}
	return srv, db, cookie, func() {
		db.Close()
		os.RemoveAll(tmpDir)
	}
}

// iacDo performs an authenticated API call.
func iacDo(t *testing.T, srv *Server, cookie *http.Cookie, method, path string, body map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body == nil {
		rdr = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

// TestIacScanEndpointScansInlineWorkspace checks the scan endpoint and the fleet summary.
func TestIacScanEndpointScansInlineWorkspace(t *testing.T) {
	srv, db, cookie, cleanup := iacTestServer(t)
	defer cleanup()

	wsID, err := db.CreateTFWorkspace(&store.TFWorkspace{
		Name:   "prod",
		Binary: "terraform",
		Source: "inline",
		Files:  `{"main.tf":"provider \"aws\" {\n  access_key = \"AKIAIOSFODNN7EXAMPLE\"\n}\n"}`,
	})
	if err != nil {
		t.Fatal(err)
	}

	w := iacDo(t, srv, cookie, http.MethodPost, "/api/iac-scan/terraform/"+itoa(wsID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	scan, _ := resp["scan"].(map[string]interface{})
	if scan == nil {
		t.Fatalf("expected a scan in the response: %s", w.Body.String())
	}
	summary, _ := scan["summary"].(map[string]interface{})
	if summary["total"] == nil || summary["total"].(float64) == 0 {
		t.Fatalf("expected findings, got %v", summary)
	}
	if bytes.Contains(w.Body.Bytes(), []byte("AKIAIOSFODNN7EXAMPLE")) {
		t.Fatal("the raw secret leaked into the API response")
	}

	// The stored scan is returned by GET.
	w = iacDo(t, srv, cookie, http.MethodGet, "/api/iac-scan/terraform/"+itoa(wsID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("AKIAIOSFODNN7EXAMPLE")) {
		t.Fatal("the raw secret leaked into the stored scan")
	}

	// The fleet summary lists the source.
	w = iacDo(t, srv, cookie, http.MethodGet, "/api/iac-scan", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"source_kind":"terraform"`)) {
		t.Fatalf("expected the source in the fleet summary: %s", w.Body.String())
	}
}

// TestIacGateBlocksAnsibleRun checks the gate: 409 with findings_summary when it is on, and no change when it is off.
func TestIacGateBlocksAnsibleRun(t *testing.T) {
	srv, db, cookie, cleanup := iacTestServer(t)
	defer cleanup()
	srv.ansibleAvail = func() (string, string, error) {
		return "/usr/bin/ansible-playbook", "ansible-playbook [core 2.15.0]", nil
	}
	if err := db.SetAnsibleEnabled(true); err != nil {
		t.Fatal(err)
	}

	pbID, err := db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{
		Name:    "site",
		Source:  "inline",
		Content: "- hosts: all\n  tasks:\n    - name: t\n      ansible.builtin.user:\n        password: sup3rS3cretValue!\n",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Gate off: the run is not blocked by the scan.
	w := iacDo(t, srv, cookie, http.MethodPost, "/api/ansible/playbooks/"+itoa(pbID)+"/run", map[string]interface{}{"mode": "check"})
	if w.Code == http.StatusConflict && bytes.Contains(w.Body.Bytes(), []byte("findings_summary")) {
		t.Fatalf("the gate must be off by default, got 409: %s", w.Body.String())
	}

	// Gate on: the run is refused with 409 and a findings summary.
	if err := db.SetSetting("iac_gate_enabled", "1"); err != nil {
		t.Fatal(err)
	}
	w = iacDo(t, srv, cookie, http.MethodPost, "/api/ansible/playbooks/"+itoa(pbID)+"/run", map[string]interface{}{"mode": "check"})
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	summary, _ := resp["findings_summary"].(map[string]interface{})
	if summary == nil {
		t.Fatalf("expected findings_summary in the response: %s", w.Body.String())
	}
	if _, ok := summary["findings"]; !ok {
		t.Fatalf("expected the blocking findings in the summary: %v", summary)
	}
	if bytes.Contains(w.Body.Bytes(), []byte("sup3rS3cretValue!")) {
		t.Fatal("the raw secret leaked into the gate response")
	}

	// ignore_iac_gate bypasses the gate.
	w = iacDo(t, srv, cookie, http.MethodPost, "/api/ansible/playbooks/"+itoa(pbID)+"/run", map[string]interface{}{"mode": "check", "ignore_iac_gate": true})
	if w.Code == http.StatusConflict && bytes.Contains(w.Body.Bytes(), []byte("findings_summary")) {
		t.Fatalf("ignore_iac_gate must bypass the gate, got 409: %s", w.Body.String())
	}
}

// TestIacGateOffChangesNothing checks that the default settings do not block a Terraform apply.
func TestIacGateOffChangesNothing(t *testing.T) {
	srv, db, cookie, cleanup := iacTestServer(t)
	defer cleanup()
	if err := db.SetTerraformEnabled(true); err != nil {
		t.Fatal(err)
	}
	srv.SetTerraformDataDir(filepath.Join(t.TempDir(), "terraform"))

	wsID, err := db.CreateTFWorkspace(&store.TFWorkspace{
		Name:   "prod",
		Binary: "terraform",
		Source: "inline",
		Files:  `{"main.tf":"provider \"aws\" {\n  access_key = \"AKIAIOSFODNN7EXAMPLE\"\n}\n"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	newRun := func() int64 {
		t.Helper()
		id, err := db.CreateTFRun(&store.TFRun{WorkspaceID: wsID, Type: "plan", Status: "awaiting_approval", TriggerKind: "manual", Targets: "[]", Replace: "[]", Policy: "{}"})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}

	// Gate off (default): the apply is not refused by the scan.
	w := iacDo(t, srv, cookie, http.MethodPost, "/api/terraform/runs/"+itoa(newRun())+"/apply", map[string]interface{}{"comment": "ship it"})
	if w.Code == http.StatusConflict && bytes.Contains(w.Body.Bytes(), []byte("findings_summary")) {
		t.Fatalf("the gate must be off by default, got 409: %s", w.Body.String())
	}

	// Gate on: the apply is refused with 409 and a findings summary.
	if err := db.SetSetting("iac_gate_enabled", "1"); err != nil {
		t.Fatal(err)
	}
	w = iacDo(t, srv, cookie, http.MethodPost, "/api/terraform/runs/"+itoa(newRun())+"/apply", map[string]interface{}{"comment": "ship it"})
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("findings_summary")) {
		t.Fatalf("expected findings_summary in the response: %s", w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("AKIAIOSFODNN7EXAMPLE")) {
		t.Fatal("the raw secret leaked into the gate response")
	}

	// ignore_iac_gate bypasses the gate.
	w = iacDo(t, srv, cookie, http.MethodPost, "/api/terraform/runs/"+itoa(newRun())+"/apply", map[string]interface{}{"comment": "ship it", "ignore_iac_gate": true})
	if w.Code == http.StatusConflict && bytes.Contains(w.Body.Bytes(), []byte("findings_summary")) {
		t.Fatalf("ignore_iac_gate must bypass the gate, got 409: %s", w.Body.String())
	}
}
