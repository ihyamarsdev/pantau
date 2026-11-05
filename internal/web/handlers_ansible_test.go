package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"pantau/internal/ansible"
	"pantau/internal/inspector"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

func setupAnsibleTestServer(t *testing.T) (*Server, func(*http.Request)) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_ansible.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to init test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	ins := inspector.New(db, func(host string, port int, user, key string) (sshrunner.Runner, error) {
		mock := sshrunner.NewMockRunner()
		mock.DefaultExec = func(cmd string) (string, string, int, error) {
			return "ok", "", 0, nil
		}
		return mock, nil
	}, nil)

	srv := NewServer(db, ins, nil)
	// Default mock availability
	srv.ansibleAvail = func() (string, string, error) {
		return "/usr/bin/ansible-playbook", "ansible-playbook [core 2.15.0]", nil
	}
	// Default mock run
	srv.ansibleRun = func(ctx context.Context, spec ansible.RunSpec, log io.Writer) (int, error) {
		_, _ = io.WriteString(log, "PLAY [all] ***\nTASK [Gathering Facts] ***\nok: [web-1]\nPLAY RECAP ***\nweb-1 : ok=1\n")
		return 0, nil
	}
	// Re-init run manager with mock runner
	srv.ansibleMgr = NewAnsibleRunManager(db, srv.ansibleRun)

	token := "ansible_test_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	authReq := func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: "pantau_session", Value: token})
	}
	return srv, authReq
}

func doRequest(t *testing.T, srv *Server, authReq func(*http.Request), method, url string, body interface{}) *httptest.ResponseRecorder {
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
func TestAnsibleUnauthenticated(t *testing.T) {
	srv, _ := setupAnsibleTestServer(t)
	rec := doRequest(t, srv, nil, http.MethodGet, "/api/ansible/status", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", rec.Code)
	}
}

// 2. Gate disabled -> 409
func TestAnsibleGateDisabled(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)

	// Gate is disabled by default
	rec := doRequest(t, srv, auth, http.MethodGet, "/api/ansible/playbooks", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for disabled gate, got %d", rec.Code)
	}

	// Status endpoint works even when gate is disabled
	rec = doRequest(t, srv, auth, http.MethodGet, "/api/ansible/status", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for status, got %d", rec.Code)
	}
	var st map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if st["enabled"] != false || st["available"] != true {
		t.Fatalf("unexpected status body: %+v", st)
	}
}

// 3. Enable fails when unavailable
func TestAnsibleEnableFailsWhenUnavailable(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)

	// Mock unavailable binary
	srv.ansibleAvail = func() (string, string, error) {
		return "", "", fmt.Errorf("ansible-playbook not found")
	}

	rec := doRequest(t, srv, auth, http.MethodPut, "/api/ansible/status", map[string]bool{"enabled": true})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 when enabling unavailable ansible, got %d", rec.Code)
	}
}

// 4. Playbook CRUD + duplicate name 409
func TestAnsiblePlaybookCRUD(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)

	// Create playbook
	rec := doRequest(t, srv, auth, http.MethodPost, "/api/ansible/playbooks", map[string]string{
		"name":        "Web Deploy",
		"description": "Deploy web server",
		"content":     "---\n- hosts: all\n  tasks:\n    - ping:\n",
		"extra_vars":  `{"env":"prod"}`,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}
	var res map[string]store.AnsiblePlaybook
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse created playbook: %v", err)
	}
	pbID := res["playbook"].ID

	// Duplicate name -> 409
	rec = doRequest(t, srv, auth, http.MethodPost, "/api/ansible/playbooks", map[string]string{
		"name":    "web deploy", // case-insensitive check
		"content": "---\n- hosts: all\n",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate name, got %d", rec.Code)
	}

	// List playbooks
	rec = doRequest(t, srv, auth, http.MethodGet, "/api/ansible/playbooks", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for list, got %d", rec.Code)
	}

	// Get detail
	rec = doRequest(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/ansible/playbooks/%d", pbID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for detail, got %d", rec.Code)
	}

	// Update
	rec = doRequest(t, srv, auth, http.MethodPut, fmt.Sprintf("/api/ansible/playbooks/%d", pbID), map[string]string{
		"name":        "Web Deploy Updated",
		"description": "Updated description",
		"content":     "---\n- hosts: all\n  tasks: []\n",
		"extra_vars":  `{"env":"staging"}`,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for update, got %d", rec.Code)
	}

	// Delete
	rec = doRequest(t, srv, auth, http.MethodDelete, fmt.Sprintf("/api/ansible/playbooks/%d", pbID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for delete, got %d", rec.Code)
	}
}

// 5. Run happy path -> poll log with offsets until finished + assert final status success
func TestAnsibleRunHappyPathAndLogPoll(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)

	// Create Host
	hostID, err := srv.db.CreateHost(&store.Host{
		Name:      "web-01",
		Host:      "192.168.1.10",
		Port:      22,
		User:      "ubuntu",
		CustomKey: "-----BEGIN RSA PRIVATE KEY-----\nMIIEfake\n-----END RSA PRIVATE KEY-----",
	})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}

	// Playbook
	pbID, err := srv.db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{
		Name:    "Ping Playbook",
		Content: "---\n- hosts: all\n  tasks:\n    - ping:\n",
	})
	if err != nil {
		t.Fatalf("create playbook: %v", err)
	}

	// Mock slow runner that writes incremental log chunks
	srv.ansibleRun = func(ctx context.Context, spec ansible.RunSpec, log io.Writer) (int, error) {
		for i := 1; i <= 3; i++ {
			time.Sleep(20 * time.Millisecond)
			_, _ = io.WriteString(log, fmt.Sprintf("chunk %d\n", i))
		}
		return 0, nil
	}
	srv.ansibleMgr = NewAnsibleRunManager(srv.db, srv.ansibleRun)

	rec := doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/run", pbID), map[string]interface{}{
		"host_ids": []int64{hostID},
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d: %s", rec.Code, rec.Body.String())
	}
	var runRes map[string]store.AnsibleRun
	if err := json.Unmarshal(rec.Body.Bytes(), &runRes); err != nil {
		t.Fatalf("unmarshal run: %v", err)
	}
	runID := runRes["run"].ID

	// Poll log with offsets
	var fullLog strings.Builder
	offset := 0
	finished := false

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !finished {
		rec = doRequest(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/ansible/runs/%d/log?offset=%d", runID, offset), nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("get log failed with %d: %s", rec.Code, rec.Body.String())
		}
		var logResp struct {
			Data     string `json:"data"`
			Offset   int    `json:"offset"`
			Status   string `json:"status"`
			Finished bool   `json:"finished"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &logResp); err != nil {
			t.Fatalf("parse log response: %v", err)
		}
		fullLog.WriteString(logResp.Data)
		offset = logResp.Offset
		finished = logResp.Finished
		if !finished {
			time.Sleep(25 * time.Millisecond)
		}
	}

	if !finished {
		t.Fatalf("run did not finish within timeout")
	}

	if !strings.Contains(fullLog.String(), "chunk 1\nchunk 2\nchunk 3\n") {
		t.Fatalf("unexpected polled log: %q", fullLog.String())
	}

	// Verify run status in DB is success
	r, err := srv.db.GetAnsibleRun(runID)
	if err != nil {
		t.Fatalf("get run from db: %v", err)
	}
	if r.Status != "success" || *r.ExitCode != 0 {
		t.Fatalf("expected status=success, exit=0, got status=%s, exit=%v", r.Status, r.ExitCode)
	}
}

// 6. Non-zero exit code -> status failed
func TestAnsibleRunNonZeroExit(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)

	hid, _ := srv.db.CreateHost(&store.Host{Name: "h1", Host: "10.0.0.1", Port: 22, User: "root", CustomKey: "KEY"})
	pid, _ := srv.db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: "Fail PB", Content: "---\n- hosts: all\n"})

	srv.ansibleRun = func(ctx context.Context, spec ansible.RunSpec, log io.Writer) (int, error) {
		_, _ = io.WriteString(log, "fatal: [h1]: UNREACHABLE!\n")
		return 2, nil
	}
	srv.ansibleMgr = NewAnsibleRunManager(srv.db, srv.ansibleRun)

	rec := doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/run", pid), map[string]interface{}{
		"host_ids": []int64{hid},
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rec.Code)
	}
	var runRes map[string]store.AnsibleRun
	_ = json.Unmarshal(rec.Body.Bytes(), &runRes)
	runID := runRes["run"].ID

	// Wait for completion
	time.Sleep(100 * time.Millisecond)
	r, _ := srv.db.GetAnsibleRun(runID)
	if r.Status != "failed" || *r.ExitCode != 2 {
		t.Fatalf("expected status=failed exit=2, got status=%s exit=%v", r.Status, r.ExitCode)
	}
}

// 7. Cancel -> cancelled and fake sees ctx cancel
func TestAnsibleRunCancel(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)

	hid, _ := srv.db.CreateHost(&store.Host{Name: "h1", Host: "10.0.0.1", Port: 22, User: "root", CustomKey: "KEY"})
	pid, _ := srv.db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: "Cancel PB", Content: "---\n- hosts: all\n"})

	sawCancel := make(chan bool, 1)
	srv.ansibleRun = func(ctx context.Context, spec ansible.RunSpec, log io.Writer) (int, error) {
		select {
		case <-ctx.Done():
			sawCancel <- true
			return 0, ctx.Err()
		case <-time.After(2 * time.Second):
			sawCancel <- false
			return 0, nil
		}
	}
	srv.ansibleMgr = NewAnsibleRunManager(srv.db, srv.ansibleRun)

	rec := doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/run", pid), map[string]interface{}{
		"host_ids": []int64{hid},
	})
	var runRes map[string]store.AnsibleRun
	_ = json.Unmarshal(rec.Body.Bytes(), &runRes)
	runID := runRes["run"].ID

	// Cancel it
	time.Sleep(20 * time.Millisecond)
	cancelRec := doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/runs/%d/cancel", runID), nil)
	if cancelRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on cancel, got %d", cancelRec.Code)
	}

	select {
	case sc := <-sawCancel:
		if !sc {
			t.Fatalf("runner did not see cancellation")
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for runner cancellation")
	}

	time.Sleep(50 * time.Millisecond)
	r, _ := srv.db.GetAnsibleRun(runID)
	if r.Status != "cancelled" {
		t.Fatalf("expected status=cancelled, got %s", r.Status)
	}
}

// 8. Second run on same playbook while running -> 409
func TestAnsibleRunSecondOnSamePlaybook409(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)

	hid, _ := srv.db.CreateHost(&store.Host{Name: "h1", Host: "10.0.0.1", Port: 22, User: "root", CustomKey: "KEY"})
	pid, _ := srv.db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: "Block PB", Content: "---\n- hosts: all\n"})

	block := make(chan struct{})
	srv.ansibleRun = func(ctx context.Context, spec ansible.RunSpec, log io.Writer) (int, error) {
		<-block
		return 0, nil
	}
	srv.ansibleMgr = NewAnsibleRunManager(srv.db, srv.ansibleRun)
	defer func() {
		close(block)
		time.Sleep(20 * time.Millisecond)
	}()
	rec1 := doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/run", pid), map[string]interface{}{
		"host_ids": []int64{hid},
	})
	if rec1.Code != http.StatusAccepted {
		t.Fatalf("run 1 expected 202, got %d", rec1.Code)
	}

	// Run 2 immediately -> 409
	rec2 := doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/run", pid), map[string]interface{}{
		"host_ids": []int64{hid},
	})
	if rec2.Code != http.StatusConflict {
		t.Fatalf("run 2 expected 409, got %d", rec2.Code)
	}
}

// 9. 4th concurrent run -> 429
func TestAnsibleRunMaxConcurrency429(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)

	hid, _ := srv.db.CreateHost(&store.Host{Name: "h1", Host: "10.0.0.1", Port: 22, User: "root", CustomKey: "KEY"})

	block := make(chan struct{})
	srv.ansibleRun = func(ctx context.Context, spec ansible.RunSpec, log io.Writer) (int, error) {
		<-block
		return 0, nil
	}
	srv.ansibleMgr = NewAnsibleRunManager(srv.db, srv.ansibleRun)
	defer func() {
		close(block)
		time.Sleep(20 * time.Millisecond)
	}()
	// Launch 3 runs on 3 distinct playbooks
	for i := 1; i <= 3; i++ {
		pid, _ := srv.db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: fmt.Sprintf("PB %d", i), Content: "---\n- hosts: all\n"})
		rec := doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/run", pid), map[string]interface{}{
			"host_ids": []int64{hid},
		})
		if rec.Code != http.StatusAccepted {
			t.Fatalf("run %d expected 202, got %d", i, rec.Code)
		}
	}

	// 4th run -> 429
	pid4, _ := srv.db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: "PB 4", Content: "---\n- hosts: all\n"})
	rec4 := doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/run", pid4), map[string]interface{}{
		"host_ids": []int64{hid},
	})
	if rec4.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 for 4th concurrent run, got %d", rec4.Code)
	}
}

// 10. Run with no targets -> 400 (syntax mode allowed)
func TestAnsibleRunNoTargets(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)

	pid, _ := srv.db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: "PB", Content: "---\n- hosts: all\n"})

	// Default run mode without targets -> 400
	rec := doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/run", pid), map[string]interface{}{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 with no targets in run mode, got %d", rec.Code)
	}

	// Syntax mode without targets -> 202 accepted
	rec = doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/run", pid), map[string]interface{}{
		"mode": "syntax",
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 for syntax mode without targets, got %d: %s", rec.Code, rec.Body.String())
	}
	time.Sleep(50 * time.Millisecond)
}

// 11. Unknown host id -> 400
func TestAnsibleRunUnknownHostID(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)

	pid, _ := srv.db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: "PB", Content: "---\n- hosts: all\n"})

	rec := doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/run", pid), map[string]interface{}{
		"host_ids": []int64{999999},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown host id, got %d", rec.Code)
	}
}

// 12. Group targeting resolves hosts
func TestAnsibleRunGroupTargeting(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)

	_, _ = srv.db.CreateHost(&store.Host{Name: "app1", Host: "10.0.0.1", Port: 22, User: "root", GroupName: "webservers", CustomKey: "KEY1"})
	_, _ = srv.db.CreateHost(&store.Host{Name: "app2", Host: "10.0.0.2", Port: 22, User: "root", GroupName: "webservers", CustomKey: "KEY2"})
	pid, _ := srv.db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: "PB", Content: "---\n- hosts: all\n"})

	var capturedSpec ansible.RunSpec
	var specMu sync.Mutex
	srv.ansibleRun = func(ctx context.Context, spec ansible.RunSpec, log io.Writer) (int, error) {
		specMu.Lock()
		capturedSpec = spec
		specMu.Unlock()
		return 0, nil
	}
	srv.ansibleMgr = NewAnsibleRunManager(srv.db, srv.ansibleRun)

	rec := doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/run", pid), map[string]interface{}{
		"group": "webservers",
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rec.Code)
	}

	time.Sleep(50 * time.Millisecond)
	specMu.Lock()
	targetCount := len(capturedSpec.Targets)
	specMu.Unlock()

	if targetCount != 2 {
		t.Fatalf("expected 2 targets resolved from group, got %d", targetCount)
	}
}

// 13. Extra_vars override precedence
func TestAnsibleRunExtraVarsPrecedence(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)

	hid, _ := srv.db.CreateHost(&store.Host{Name: "h1", Host: "10.0.0.1", Port: 22, User: "root", CustomKey: "KEY"})
	pid, _ := srv.db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{
		Name:      "PB",
		Content:   "---\n- hosts: all\n",
		ExtraVars: `{"foo":"default"}`,
	})

	var capturedSpec ansible.RunSpec
	srv.ansibleRun = func(ctx context.Context, spec ansible.RunSpec, log io.Writer) (int, error) {
		capturedSpec = spec
		return 0, nil
	}
	srv.ansibleMgr = NewAnsibleRunManager(srv.db, srv.ansibleRun)

	// Run without extra_vars -> defaults to playbook extra_vars
	rec1 := doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/run", pid), map[string]interface{}{
		"host_ids": []int64{hid},
	})
	if rec1.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rec1.Code)
	}
	time.Sleep(20 * time.Millisecond)
	if capturedSpec.ExtraVars != `{"foo":"default"}` {
		t.Fatalf("expected default extra_vars, got %s", capturedSpec.ExtraVars)
	}

	// Run with override extra_vars
	rec2 := doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/run", pid), map[string]interface{}{
		"host_ids":   []int64{hid},
		"extra_vars": `{"foo":"override"}`,
	})
	if rec2.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rec2.Code)
	}
	time.Sleep(20 * time.Millisecond)
	if capturedSpec.ExtraVars != `{"foo":"override"}` {
		t.Fatalf("expected overridden extra_vars, got %s", capturedSpec.ExtraVars)
	}
}

// 14. Log offset beyond length -> empty data
func TestAnsibleRunLogOffsetBeyondLength(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)

	hid, _ := srv.db.CreateHost(&store.Host{Name: "h1", Host: "10.0.0.1", Port: 22, User: "root", CustomKey: "KEY"})
	pid, _ := srv.db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: "PB", Content: "---\n- hosts: all\n"})

	srv.ansibleRun = func(ctx context.Context, spec ansible.RunSpec, log io.Writer) (int, error) {
		_, _ = io.WriteString(log, "hello\n")
		return 0, nil
	}
	srv.ansibleMgr = NewAnsibleRunManager(srv.db, srv.ansibleRun)

	rec := doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/run", pid), map[string]interface{}{
		"host_ids": []int64{hid},
	})
	var runRes map[string]store.AnsibleRun
	_ = json.Unmarshal(rec.Body.Bytes(), &runRes)
	runID := runRes["run"].ID

	time.Sleep(50 * time.Millisecond)

	rec = doRequest(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/ansible/runs/%d/log?offset=9999", runID), nil)
	var logResp struct {
		Data   string `json:"data"`
		Offset int    `json:"offset"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &logResp)
	if logResp.Data != "" {
		t.Fatalf("expected empty data when offset beyond length, got %q", logResp.Data)
	}
}

// 15. Log of finished run read from DB after manager forgets it
func TestAnsibleRunLogReadFromDBAfterManagerForgets(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)

	hid, _ := srv.db.CreateHost(&store.Host{Name: "h1", Host: "10.0.0.1", Port: 22, User: "root", CustomKey: "KEY"})
	pid, _ := srv.db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: "PB", Content: "---\n- hosts: all\n"})

	srv.ansibleRun = func(ctx context.Context, spec ansible.RunSpec, log io.Writer) (int, error) {
		_, _ = io.WriteString(log, "output line from execution\n")
		return 0, nil
	}
	srv.ansibleMgr = NewAnsibleRunManager(srv.db, srv.ansibleRun)

	rec := doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/run", pid), map[string]interface{}{
		"host_ids": []int64{hid},
	})
	var runRes map[string]store.AnsibleRun
	_ = json.Unmarshal(rec.Body.Bytes(), &runRes)
	runID := runRes["run"].ID

	// Wait for execution to finish and goroutine cleanup
	time.Sleep(50 * time.Millisecond)

	// Ensure manager no longer has active run
	if srv.ansibleMgr.HasActiveRun(runID) {
		t.Fatalf("expected manager to have completed the run")
	}

	// Read log via HTTP endpoint
	rec = doRequest(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/ansible/runs/%d/log?offset=0", runID), nil)
	var logResp struct {
		Data     string `json:"data"`
		Status   string `json:"status"`
		Finished bool   `json:"finished"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &logResp)
	if !logResp.Finished || logResp.Status != "success" || !strings.Contains(logResp.Data, "output line from execution") {
		t.Fatalf("unexpected log from DB: %+v", logResp)
	}
}

// 16. Deleting playbook with active run -> 409
func TestAnsibleDeletePlaybookWithActiveRun409(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)

	hid, _ := srv.db.CreateHost(&store.Host{Name: "h1", Host: "10.0.0.1", Port: 22, User: "root", CustomKey: "KEY"})
	pid, _ := srv.db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: "Active PB", Content: "---\n- hosts: all\n"})

	block := make(chan struct{})
	srv.ansibleRun = func(ctx context.Context, spec ansible.RunSpec, log io.Writer) (int, error) {
		<-block
		return 0, nil
	}
	srv.ansibleMgr = NewAnsibleRunManager(srv.db, srv.ansibleRun)
	defer func() {
		close(block)
		time.Sleep(20 * time.Millisecond)
	}()
	doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/run", pid), map[string]interface{}{
		"host_ids": []int64{hid},
	})

	// Attempt delete -> 409
	rec := doRequest(t, srv, auth, http.MethodDelete, fmt.Sprintf("/api/ansible/playbooks/%d", pid), nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 when deleting playbook with active run, got %d", rec.Code)
	}
}

// 17. Private key content never appears in any response body
func TestAnsiblePrivateKeyNeverExposed(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	_ = srv.db.SetAnsibleEnabled(true)

	fakeKeyMarker := "SUPER_SECRET_PRIVATE_KEY_MARKER_DO_NOT_LEAK"
	hid, _ := srv.db.CreateHost(&store.Host{
		Name:      "secret-host",
		Host:      "10.0.0.1",
		Port:      22,
		User:      "root",
		CustomKey: fmt.Sprintf("-----BEGIN OPENSSH PRIVATE KEY-----\n%s\n-----END OPENSSH PRIVATE KEY-----", fakeKeyMarker),
	})

	pid, _ := srv.db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: "Secret PB", Content: "---\n- hosts: all\n"})

	srv.ansibleRun = func(ctx context.Context, spec ansible.RunSpec, log io.Writer) (int, error) {
		_, _ = io.WriteString(log, "Execution finished\n")
		return 0, nil
	}
	srv.ansibleMgr = NewAnsibleRunManager(srv.db, srv.ansibleRun)

	// Run playbook
	rec := doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/run", pid), map[string]interface{}{
		"host_ids": []int64{hid},
	})
	if strings.Contains(rec.Body.String(), fakeKeyMarker) {
		t.Fatalf("private key marker leaked in run response!")
	}

	var runRes map[string]store.AnsibleRun
	_ = json.Unmarshal(rec.Body.Bytes(), &runRes)
	runID := runRes["run"].ID

	time.Sleep(50 * time.Millisecond)

	// Check run detail
	rec = doRequest(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/ansible/runs/%d", runID), nil)
	if strings.Contains(rec.Body.String(), fakeKeyMarker) {
		t.Fatalf("private key marker leaked in run detail response!")
	}

	// Check run log
	rec = doRequest(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/ansible/runs/%d/log?offset=0", runID), nil)
	if strings.Contains(rec.Body.String(), fakeKeyMarker) {
		t.Fatalf("private key marker leaked in run log response!")
	}
}
