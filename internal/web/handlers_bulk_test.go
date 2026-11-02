package web

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"pantau/internal/inspector"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

func TestBuildBulkCommand(t *testing.T) {
	cmd := BuildBulkCommand("uptime; uname -a", false)
	if !strings.Contains(cmd, "uptime; uname -a") {
		t.Errorf("expected command in plain script: %s", cmd)
	}
	if strings.Contains(cmd, "sudo -n") {
		t.Errorf("plain script should not contain sudo -n: %s", cmd)
	}

	sudoCmd := BuildBulkCommand("apt-get update", true)
	if !strings.Contains(sudoCmd, "sudo -n") {
		t.Errorf("sudo script must contain sudo -n: %s", sudoCmd)
	}
	if !strings.Contains(sudoCmd, "sudo without password required but not available") {
		t.Errorf("sudo script must contain error message on sudo fail: %s", sudoCmd)
	}
}

func setupBulkTestServer(t *testing.T, runner *sshrunner.MockRunner) (*Server, *store.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_bulk.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	ins := inspector.New(db, func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return runner, nil
	}, nil)

	srv := NewServer(db, ins, nil)
	return srv, db
}

func createBulkAuthCookie(srv *Server) *http.Cookie {
	token := "test-session-bulk"
	srv.sessions.Store(token, time.Now().Add(1*time.Hour))
	return &http.Cookie{
		Name:     "pantau_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
	}
}

func TestExecBulkEndpointValidation(t *testing.T) {
	runner := sshrunner.NewMockRunner()
	srv, db := setupBulkTestServer(t, runner)
	authCookie := createBulkAuthCookie(srv)

	h1, _ := db.CreateHost(&store.Host{Name: "host-1", Host: "10.0.0.1", Port: 22, User: "root"})
	h2, _ := db.CreateHost(&store.Host{Name: "host-2", Host: "10.0.0.2", Port: 22, User: "root"})

	tests := []struct {
		name       string
		req        bulkExecRequest
		wantStatus int
	}{
		{
			name:       "empty host_ids",
			req:        bulkExecRequest{HostIDs: []int64{}, Command: "uptime"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "duplicate host_ids",
			req:        bulkExecRequest{HostIDs: []int64{h1, h1}, Command: "uptime"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown host_id",
			req:        bulkExecRequest{HostIDs: []int64{999999}, Command: "uptime"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty command",
			req:        bulkExecRequest{HostIDs: []int64{h1}, Command: ""},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "command with NUL",
			req:        bulkExecRequest{HostIDs: []int64{h1}, Command: "ls\x00-la"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "command exceeding 4096 bytes",
			req:        bulkExecRequest{HostIDs: []int64{h1}, Command: strings.Repeat("a", 4097)},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "too many host_ids",
			req:        bulkExecRequest{HostIDs: make([]int64, 21), Command: "uptime"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "valid request",
			req:        bulkExecRequest{HostIDs: []int64{h1, h2}, Command: "uptime", TimeoutSec: 30},
			wantStatus: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, _ := json.Marshal(tc.req)
			req := httptest.NewRequest(http.MethodPost, "/api/exec/bulk", bytes.NewReader(b))
			req.AddCookie(authCookie)
			w := httptest.NewRecorder()

			srv.mux.ServeHTTP(w, req)
			if w.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d: %s", tc.wantStatus, w.Code, w.Body.String())
			}
		})
	}
}

func TestExecBulkExecutionAndStream(t *testing.T) {
	runner := sshrunner.NewMockRunner()
	runner.DefaultExec = func(cmd string) (string, string, int, error) {
		return "Linux test 6.1.0\n", "", 0, nil
	}

	srv, db := setupBulkTestServer(t, runner)
	authCookie := createBulkAuthCookie(srv)

	h1, _ := db.CreateHost(&store.Host{Name: "host-1", Host: "10.0.0.1", Port: 22, User: "root"})
	h2, _ := db.CreateHost(&store.Host{Name: "host-2", Host: "10.0.0.2", Port: 22, User: "root", KubeMode: "no-exec"})

	// h2 has KubeMode="no-exec", if we run kubectl exec it must be blocked
	reqBody := bulkExecRequest{
		HostIDs: []int64{h1, h2},
		Command: "kubectl exec -it mypod -- sh",
	}
	b, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/exec/bulk", bytes.NewReader(b))
	req.AddCookie(authCookie)
	w := httptest.NewRecorder()

	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/x-ndjson") {
		t.Errorf("expected Content-Type application/x-ndjson, got %s", contentType)
	}

	scanner := bufio.NewScanner(w.Body)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if len(lines) != 3 {
		t.Fatalf("expected 3 NDJSON lines (2 hosts + 1 done), got %d: %v", len(lines), lines)
	}

	var h1Result, h2Result bulkHostResult
	var doneResult bulkDoneResult

	// Decode host lines
	for _, l := range lines[:2] {
		var res bulkHostResult
		_ = json.Unmarshal([]byte(l), &res)
		if res.HostID == h1 {
			h1Result = res
		} else if res.HostID == h2 {
			h2Result = res
		}
	}

	_ = json.Unmarshal([]byte(lines[2]), &doneResult)

	if !h1Result.OK {
		t.Errorf("expected h1Result OK=true, got %+v", h1Result)
	}
	if !strings.Contains(h1Result.Output, "Linux test") {
		t.Errorf("expected output on h1Result: %+v", h1Result)
	}

	if h2Result.OK {
		t.Errorf("expected h2Result to be blocked by Kube Mode: %+v", h2Result)
	}
	if h2Result.Error != "blocked by Kube Mode" {
		t.Errorf("expected 'blocked by Kube Mode', got %q", h2Result.Error)
	}

	if !doneResult.Done || doneResult.Total != 2 || doneResult.Failed != 1 {
		t.Errorf("unexpected done line: %+v", doneResult)
	}
}

func TestExecBulkOutputTruncation64KB(t *testing.T) {
	runner := sshrunner.NewMockRunner()
	hugeOutput := strings.Repeat("X", 70*1024)
	runner.DefaultExec = func(cmd string) (string, string, int, error) {
		return hugeOutput, "", 0, nil
	}

	srv, db := setupBulkTestServer(t, runner)
	authCookie := createBulkAuthCookie(srv)

	h1, _ := db.CreateHost(&store.Host{Name: "host-big", Host: "10.0.0.1", Port: 22, User: "root"})

	reqBody := bulkExecRequest{
		HostIDs: []int64{h1},
		Command: "cat /dev/zero",
	}
	b, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/exec/bulk", bytes.NewReader(b))
	req.AddCookie(authCookie)
	w := httptest.NewRecorder()

	srv.mux.ServeHTTP(w, req)
	scanner := bufio.NewScanner(w.Body)
	buf := make([]byte, 128*1024)
	scanner.Buffer(buf, len(buf))
	var firstLine string
	if scanner.Scan() {
		firstLine = scanner.Text()
	}
	var res bulkHostResult
	_ = json.Unmarshal([]byte(firstLine), &res)

	if !res.Truncated {
		t.Errorf("expected Truncated=true")
	}
	if len(res.Output) != 64*1024 {
		t.Errorf("expected output len 65536, got %d", len(res.Output))
	}
}

func TestUpgradePreviewAndWSEndpoint(t *testing.T) {
	runner := sshrunner.NewMockRunner()
	runner.DefaultExec = func(cmd string) (string, string, int, error) {
		if strings.Contains(cmd, "===OS_RELEASE===") {
			return `===OS_RELEASE===
NAME="Ubuntu"
ID=ubuntu
VERSION_ID="22.04"
===REBOOT===
===UPDATES===
MGR:apt
Inst curl [7.81.0-1ubuntu1.14] (7.81.0-1ubuntu1.15 Ubuntu:22.04/jammy-updates [amd64])
`, "", 0, nil
		}
		return "", "", 0, nil
	}

	srv, db := setupBulkTestServer(t, runner)
	authCookie := createBulkAuthCookie(srv)

	h1, _ := db.CreateHost(&store.Host{Name: "host-apt", Host: "10.0.0.1", Port: 22, User: "root"})

	// 1. GET /api/hosts/{id}/packages/upgrade-preview
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/hosts/%d/packages/upgrade-preview", h1), nil)
	req.AddCookie(authCookie)
	w := httptest.NewRecorder()

	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var preview inspector.UpgradePreview
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatalf("unmarshal preview failed: %v", err)
	}

	if !preview.OK || !preview.Supported || preview.Manager != "apt" || preview.PendingTotal != 1 {
		t.Fatalf("unexpected preview: %+v", preview)
	}

	// 2. Test WS /ws/pkg/upgrade via httptest server
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + fmt.Sprintf("/ws/pkg/upgrade?host_id=%d&mode=security", h1)
	header := http.Header{}
	header.Add("Cookie", authCookie.String())

	// Connecting with mode=security on apt must fail with rejection message
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("ws dial failed: %v", err)
	}
	defer conn.Close()

	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ws read failed: %v", err)
	}
	if !strings.Contains(string(msg), "apt does not have a reliable security-only upgrade mode") {
		t.Fatalf("expected apt security mode rejection, got: %s", string(msg))
	}
}
