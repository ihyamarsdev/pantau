package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func mcpCall(t *testing.T, srv *Server, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	return w
}

func TestAgentMCPAuthAndProtocol(t *testing.T) {
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	init := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`

	if w := mcpCall(t, srv, "", init); w.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", w.Code)
	}
	tok := srv.agentToken()
	if w := mcpCall(t, srv, "wrong", init); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", w.Code)
	}
	if w := mcpCall(t, srv, tok, init); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled agent must refuse, got %d", w.Code)
	}

	_ = db.SetSetting("hermes_enabled", "1")
	w := mcpCall(t, srv, tok, init)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), mcpProtocolVersion) {
		t.Fatalf("initialize: %d %s", w.Code, w.Body.String())
	}
	if w := mcpCall(t, srv, tok, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); w.Code != http.StatusAccepted {
		t.Fatalf("notification must be 202, got %d", w.Code)
	}

	w = mcpCall(t, srv, tok, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	var list struct {
		Result struct {
			Tools []struct {
				Name        string                 `json:"name"`
				InputSchema map[string]interface{} `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Result.Tools) != len(agentTools) {
		t.Fatalf("tools/list: %v %s", err, w.Body.String())
	}
	for _, tl := range list.Result.Tools {
		if tl.InputSchema["type"] != "object" {
			t.Fatalf("%s has no object schema", tl.Name)
		}
	}
	if strings.Contains(w.Body.String(), `"gate"`) {
		t.Fatal("internal gate field leaked into tools/list")
	}
}

func TestAgentMCPToolValidation(t *testing.T) {
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	_ = db.SetSetting("hermes_enabled", "1")
	tok := srv.agentToken()

	call := func(name, args string) string {
		w := mcpCall(t, srv, tok, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+name+`","arguments":`+args+`}}`)
		return w.Body.String()
	}
	if out := call("systemd_action", `{"host_id":1,"unit":"x;reboot.service","action":"restart"}`); !strings.Contains(out, `"isError":true`) {
		t.Fatalf("bad unit must be an error: %s", out)
	}
	if out := call("docker_action", `{"host_id":1,"container":"a b","action":"restart"}`); !strings.Contains(out, "invalid container name") {
		t.Fatalf("bad container: %s", out)
	}
	if out := call("docker_action", `{"host_id":1,"container":"web","action":"remove"}`); !strings.Contains(out, "action must be") {
		t.Fatalf("remove must not be offered: %s", out)
	}
	if out := call("read_file", `{"host_id":1,"path":"/etc/shadow"}`); !strings.Contains(out, "secret file") {
		t.Fatalf("shadow must be refused: %s", out)
	}
	if out := call("nope", `{}`); !strings.Contains(out, "unknown tool") {
		t.Fatalf("unknown tool: %s", out)
	}
}

func TestAgentSecretPathRe(t *testing.T) {
	for _, p := range []string{"/etc/shadow", "/etc/sudoers.d/x", "/root/.ssh/id_ed25519", "/home/a/.ssh/authorized_keys", "/srv/app/.env", "/etc/ssl/private/site.key", "/home/a/.aws/credentials", "/home/a/.docker/config.json"} {
		if !agentSecretPathRe.MatchString(p) {
			t.Errorf("%s must be treated as secret", p)
		}
	}
	for _, p := range []string{"/etc/nginx/nginx.conf", "/var/log/syslog", "/etc/ssh/sshd_config", "/srv/app/.env.example"} {
		if agentSecretPathRe.MatchString(p) {
			t.Errorf("%s must be readable", p)
		}
	}
}

func approvalReq() *http.Request { return httptest.NewRequest(http.MethodPost, "/mcp", nil) }

func TestAgentApprovalFlow(t *testing.T) {
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()

	decide := func(approve bool) {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			srv.agent.mu.Lock()
			var id string
			for k := range srv.agent.pending {
				id = k
			}
			srv.agent.mu.Unlock()
			if id != "" {
				b, _ := json.Marshal(map[string]bool{"approve": approve})
				w := httptest.NewRecorder()
				srv.handleAgentApprovalDecide(w, httptest.NewRequest(http.MethodPost, "/api/agent/approvals/"+id, bytes.NewReader(b)))
				if w.Code != http.StatusOK {
					t.Errorf("decide: %d", w.Code)
				}
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Error("approval never became pending")
	}

	// ask mode: gate 1 waits; approve lets it through, deny blocks it.
	for _, want := range []bool{true, false} {
		go decide(want)
		if ok, _ := srv.agentApprove(approvalReq(), &agentApproval{Tool: "x"}, 1); ok != want {
			t.Fatalf("approve=%v got %v", want, ok)
		}
	}

	// auto mode skips gate 1 but never gate 2.
	_ = db.SetSetting("agent_approval", "auto")
	if ok, _ := srv.agentApprove(approvalReq(), &agentApproval{Tool: "x"}, 1); !ok {
		t.Fatal("auto mode must pass gate 1")
	}
	go decide(false)
	if ok, _ := srv.agentApprove(approvalReq(), &agentApproval{Tool: "exec_command"}, 2); ok {
		t.Fatal("gate 2 must wait for the operator even in auto mode")
	}
}

func TestAgentSettingsKeyIsWriteOnly(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()
	post := func(body string) int {
		w := httptest.NewRecorder()
		srv.handleAgentSettings(w, httptest.NewRequest(http.MethodPost, "/api/agent/settings", strings.NewReader(body)))
		return w.Code
	}
	if post(`{"enabled":true,"url":"http://127.0.0.1:8642/","key":"S3CRET","approval":"auto"}`) != http.StatusOK {
		t.Fatal("save failed")
	}
	if post(`{"url":"file:///etc/passwd"}`) != http.StatusBadRequest {
		t.Fatal("non-http URL must be rejected")
	}
	post(`{"key":""}`) // empty key keeps the stored one
	w := httptest.NewRecorder()
	srv.handleAgentSettings(w, httptest.NewRequest(http.MethodGet, "/api/agent/settings", nil))
	if strings.Contains(w.Body.String(), "S3CRET") || !strings.Contains(w.Body.String(), `"key_set":true`) {
		t.Fatalf("key must be write-only and kept: %s", w.Body.String())
	}
	if c := srv.agentConfig(); c.URL != "http://127.0.0.1:8642" || c.Key != "S3CRET" || c.Approval != "auto" {
		t.Fatalf("config: %+v", c)
	}
}

func TestAgentChatProxiesStreamWithSessionHeaders(t *testing.T) {
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	var gotAuth, gotSession, gotBody string
	hermes := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotSession = r.Header.Get("Authorization"), r.Header.Get("X-Hermes-Session-Id")
		b := new(bytes.Buffer)
		_, _ = b.ReadFrom(r.Body)
		gotBody = b.String()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer hermes.Close()
	_ = db.SetSetting("hermes_enabled", "1")
	_ = db.SetSetting("hermes_url", hermes.URL)
	_ = db.SetSetting("hermes_key", "k1")

	chat := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		srv.handleAgentChat(w, httptest.NewRequest(http.MethodPost, "/api/agent/chat", strings.NewReader(body)))
		return w
	}
	w := chat(`{"message":"why is nginx down?","session_id":"pantau-abc"}`)
	if !strings.Contains(w.Body.String(), `"content":"hi"`) || gotAuth != "Bearer k1" || gotSession != "pantau-abc" {
		t.Fatalf("proxy: auth=%q session=%q body=%s", gotAuth, gotSession, w.Body.String())
	}
	if !strings.Contains(gotBody, "Pantau Operator") || !strings.Contains(gotBody, "why is nginx down?") {
		t.Fatalf("SOUL or message missing upstream: %s", gotBody)
	}
	if w := chat(`{"message":"x","session_id":"bad id\n"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("session id must be validated: %d", w.Code)
	}
}

func TestAgentSessionGrantScope(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Waits for the next pending approval and decides it with the session flag.
	decideSession := func() {
		for i := 0; i < 400; i++ {
			srv.agent.mu.Lock()
			var id string
			for k := range srv.agent.pending {
				id = k
			}
			srv.agent.mu.Unlock()
			if id != "" {
				w := httptest.NewRecorder()
				srv.handleAgentApprovalDecide(w, httptest.NewRequest(http.MethodPost, "/api/agent/approvals/"+id, strings.NewReader(`{"approve":true,"session":true}`)))
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Error("nothing pending")
	}
	ask := func(host int64, gate int) bool {
		ctx, cancel := contextTimeout(approvalReq(), 150*time.Millisecond)
		defer cancel()
		ok, _ := srv.agentApprove(approvalReq().WithContext(ctx), &agentApproval{HostID: host, Tool: "t"}, gate)
		return ok
	}

	go decideSession()
	if ok, _ := srv.agentApprove(approvalReq(), &agentApproval{HostID: 1, HostName: "a", Tool: "docker_action"}, 1); !ok {
		t.Fatal("session approve must run the action")
	}
	if !ask(1, 1) {
		t.Fatal("gate 1 on the same host must now pass without asking")
	}
	if ask(1, 2) {
		t.Fatal("a gate-1 grant must not cover shell commands")
	}
	if ask(2, 1) {
		t.Fatal("a grant must not leak to another host")
	}

	go decideSession()
	if ok, _ := srv.agentApprove(approvalReq(), &agentApproval{HostID: 1, HostName: "a", Tool: "exec_command"}, 2); !ok {
		t.Fatal("session approve of a shell command must run it")
	}
	if !ask(1, 2) || !ask(1, 1) {
		t.Fatal("a gate-2 grant covers shell and tools on that host")
	}

	srv.handleAgentGrants(httptest.NewRecorder(), httptest.NewRequest(http.MethodDelete, "/api/agent/grants?host=1", nil))
	if ask(1, 1) {
		t.Fatal("revoked grant must ask again")
	}

	srv.agent.mu.Lock()
	srv.agent.grants = map[int64]agentGrant{1: {HostID: 1, Gate: 2, Expires: time.Now().Add(-time.Second)}}
	srv.agent.mu.Unlock()
	if ask(1, 1) {
		t.Fatal("expired grant must not approve")
	}
}
