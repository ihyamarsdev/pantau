package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

const mcpLoopback = "127.0.0.1:50505"

// mcpTestServer returns a server with the MCP enabled and a fresh token. authReq logs into the session UI.
func mcpTestServer(t *testing.T) (*Server, *store.DB, string) {
	t.Helper()
	srv, db, _ := setupRunnerTestServer(t, sshrunner.NewMockRunner())
	srv.SetMCPEnabled(true)
	token, err := srv.generateMCPToken()
	if err != nil {
		t.Fatal(err)
	}
	return srv, db, token
}

func mcpPost(t *testing.T, srv *Server, remote, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.RemoteAddr = remote
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

// The endpoint answers only loopback clients with a valid token, and only over POST.
func TestMCPAccessControl(t *testing.T) {
	t.Parallel()
	srv, _, token := mcpTestServer(t)
	const initBody = `{"jsonrpc":"2.0","id":1,"method":"initialize"}`

	tests := []struct {
		name   string
		remote string
		origin string
		token  string
		method string
		want   int
	}{
		{name: "loopback with token", remote: mcpLoopback, token: token, method: http.MethodPost, want: http.StatusOK},
		{name: "ipv6 loopback with token", remote: "[::1]:50505", token: token, method: http.MethodPost, want: http.StatusOK},
		{name: "non-loopback refused even with token", remote: "192.0.2.10:50505", token: token, method: http.MethodPost, want: http.StatusForbidden},
		{name: "foreign origin refused (DNS rebinding)", remote: mcpLoopback, origin: "https://evil.example", token: token, method: http.MethodPost, want: http.StatusForbidden},
		{name: "missing token", remote: mcpLoopback, method: http.MethodPost, want: http.StatusUnauthorized},
		{name: "wrong token", remote: mcpLoopback, token: "pmcp_wrong", method: http.MethodPost, want: http.StatusUnauthorized},
		{name: "GET not allowed", remote: mcpLoopback, token: token, method: http.MethodGet, want: http.StatusMethodNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tt.method, "/mcp", strings.NewReader(initBody))
			req.RemoteAddr = tt.remote
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)
			if w.Code != tt.want {
				t.Fatalf("status = %d, want %d: %s", w.Code, tt.want, w.Body.String())
			}
		})
	}
}

// With the setting off and no flag, /mcp does not exist at all.
func TestMCPDisabledIsNotFound(t *testing.T) {
	t.Parallel()
	srv, _, _ := setupRunnerTestServer(t, sshrunner.NewMockRunner())
	w := mcpPost(t, srv, mcpLoopback, "pmcp_any", `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func decodeMCP(t *testing.T, body string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("response is not JSON: %v: %s", err, body)
	}
	return m
}

func rpcErrorCode(t *testing.T, m map[string]any) int {
	t.Helper()
	e, ok := m["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error in response: %v", m)
	}
	return int(e["code"].(float64))
}

// Protocol shape: initialize, notifications, ping, tools/list and the JSON-RPC error codes.
func TestMCPProtocol(t *testing.T) {
	t.Parallel()
	srv, _, token := mcpTestServer(t)
	tests := []struct {
		name     string
		body     string
		status   int
		check    func(t *testing.T, m map[string]any)
		wantCode int
	}{
		{name: "initialize", body: `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`, status: 200, check: func(t *testing.T, m map[string]any) {
			res := m["result"].(map[string]any)
			if res["protocolVersion"] != mcpProtocolVersion {
				t.Fatalf("protocolVersion = %v", res["protocolVersion"])
			}
		}},
		{name: "initialized notification gets 202 and no body", body: `{"jsonrpc":"2.0","method":"notifications/initialized"}`, status: 202},
		{name: "ping", body: `{"jsonrpc":"2.0","id":"abc","method":"ping"}`, status: 200, check: func(t *testing.T, m map[string]any) {
			if m["id"] != "abc" {
				t.Fatalf("id = %v, want the string id echoed", m["id"])
			}
		}},
		{name: "tools/list has exactly the read-only tools", body: `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, status: 200, check: func(t *testing.T, m map[string]any) {
			tools := m["result"].(map[string]any)["tools"].([]any)
			want := map[string]bool{
				"pantau_list_hosts": true, "pantau_get_host_detail": true, "pantau_get_k8s_overview": true,
				"pantau_get_docker_overview": true, "pantau_get_devsecops_findings": true,
				"pantau_get_iac_runs": true, "pantau_list_vault_keys": true,
				"pantau_diagnose_k8s_pod": true, "pantau_diagnose_docker": true, "pantau_diagnose_iac": true,
				"pantau_k8s_workload_summary": true, "pantau_k8s_cluster_summary": true,
			}
			got := map[string]bool{}
			for _, raw := range tools {
				got[raw.(map[string]any)["name"].(string)] = true
			}
			if len(got) != len(want) || len(tools) != len(mcpTools) {
				t.Fatalf("tools = %d (registry %d), want %d", len(tools), len(mcpTools), len(want))
			}
			for name := range want {
				if !got[name] {
					t.Errorf("tools/list is missing %s", name)
				}
			}
		}},
		{name: "unknown method", body: `{"jsonrpc":"2.0","id":3,"method":"resources/list"}`, status: 200, wantCode: rpcMethodNotFound},
		{name: "batch refused", body: `[{"jsonrpc":"2.0","id":4,"method":"ping"}]`, status: 400, wantCode: rpcInvalidRequest},
		{name: "invalid JSON", body: `{"jsonrpc":`, status: 400, wantCode: rpcParseError},
		{name: "wrong jsonrpc version", body: `{"jsonrpc":"1.0","id":5,"method":"ping"}`, status: 400, wantCode: rpcInvalidRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := mcpPost(t, srv, mcpLoopback, token, tt.body)
			if w.Code != tt.status {
				t.Fatalf("status = %d, want %d: %s", w.Code, tt.status, w.Body.String())
			}
			if tt.status == http.StatusAccepted {
				if w.Body.Len() != 0 {
					t.Fatalf("202 must have no body, got %q", w.Body.String())
				}
				return
			}
			m := decodeMCP(t, w.Body.String())
			if tt.wantCode != 0 {
				if got := rpcErrorCode(t, m); got != tt.wantCode {
					t.Fatalf("error code = %d, want %d", got, tt.wantCode)
				}
			}
			if tt.check != nil {
				tt.check(t, m)
			}
		})
	}
}

// Mutating and command tools do not exist, so calling them is refused like any unknown name.
func TestMCPRefusesMutationTools(t *testing.T) {
	t.Parallel()
	srv, _, token := mcpTestServer(t)
	for _, name := range []string{"exec_command", "systemd_action", "restart_host", "apply", "sftp_write", "reveal_secret", "docker_security_scan"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + name + `","arguments":{}}}`
			m := decodeMCP(t, mcpPost(t, srv, mcpLoopback, token, body).Body.String())
			if got := rpcErrorCode(t, m); got != rpcInvalidParams {
				t.Fatalf("error code = %d, want invalid params for %s", got, name)
			}
		})
	}
}

// Tool results go through the redaction filter: a secret stored in an incident never reaches the client.
func TestMCPToolResultsAreRedacted(t *testing.T) {
	t.Parallel()
	srv, db, token := mcpTestServer(t)
	hostID, err := db.CreateHost(&store.Host{Name: "db-1", Host: "10.0.0.8", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	inc := &store.Incident{HostID: hostID, Kind: "service", Target: "postgres", EventType: "drift_detected",
		Summary: "drift", RootCauseExcerpt: "started with password=hunter2supersecret and Authorization: Bearer abcdefgh12345678", CreatedAt: time.Now()}
	if err := db.RecordIncident(inc); err != nil {
		t.Fatal(err)
	}

	body := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"pantau_get_host_detail","arguments":{"host_id":` + itoa64(hostID) + `}}}`
	w := mcpPost(t, srv, mcpLoopback, token, body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	text := w.Body.String()
	if !strings.Contains(text, "[REDACTED]") {
		t.Fatalf("result has no redaction marker: %s", text)
	}
	for _, secret := range []string{"hunter2supersecret", "abcdefgh12345678"} {
		if strings.Contains(text, secret) {
			t.Fatalf("secret %q leaked to the MCP client", secret)
		}
	}
}

// pantau_list_hosts returns host rows for the stored hosts.
func TestMCPListHostsTool(t *testing.T) {
	t.Parallel()
	srv, db, token := mcpTestServer(t)
	id, err := db.CreateHost(&store.Host{Name: "web-1", Host: "10.0.0.9", Port: 22, User: "root", GroupName: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"pantau_list_hosts","arguments":{"group":"prod"}}}`
	m := decodeMCP(t, mcpPost(t, srv, mcpLoopback, token, body).Body.String())
	res := m["result"].(map[string]any)
	if res["isError"] != false {
		t.Fatalf("isError = %v: %v", res["isError"], res)
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, `"name": "web-1"`) || !strings.Contains(text, itoa64(id)) {
		t.Fatalf("host row missing: %s", text)
	}
}

func TestMCPRedactHelper(t *testing.T) {
	t.Parallel()
	got := mcpRedact("db password=s3cret and token=abcdef123456")
	if strings.Contains(got, "s3cret") || !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("mcpRedact = %q", got)
	}
}

func itoa64(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}
