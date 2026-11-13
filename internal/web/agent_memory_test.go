package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pantau/internal/store"
)

func TestAgentMemoryToolsAndPrompt(t *testing.T) {
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	_ = db.SetSetting("hermes_enabled", "1")
	tok := srv.agentToken()
	hostID, _ := db.CreateHost(&store.Host{Name: "web-1", Host: "10.0.0.1", Port: 22, User: "root"})
	call := func(name, args string) string {
		return mcpCall(t, srv, tok, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+name+`","arguments":`+args+`}}`).Body.String()
	}

	if p := srv.agentMemoryPrompt(); !strings.Contains(p, "No notes saved yet") || !strings.Contains(p, "memory_save") {
		t.Fatalf("empty prompt must still teach the tools: %s", p)
	}

	if out := call("memory_save", `{"content":"Backups run at 02:00"}`); !strings.Contains(out, "saved as note #1") {
		t.Fatalf("save: %s", out)
	}
	if out := call("memory_save", `{"content":"nginx sits behind Cloudflare","host_id":`+itoa(hostID)+`}`); !strings.Contains(out, "note #2") {
		t.Fatalf("save host note: %s", out)
	}
	for _, bad := range []string{
		`{"content":"db password: hunter2"}`,
		`{"content":"-----BEGIN OPENSSH PRIVATE KEY-----"}`,
		`{"content":"x","host_id":9999}`,
		`{"content":"x","replaces":9999}`,
		`{"content":""}`,
	} {
		if out := call("memory_save", bad); !strings.Contains(out, `"isError":true`) {
			t.Errorf("must be refused: %s -> %s", bad, out)
		}
	}

	p := srv.agentMemoryPrompt()
	if !strings.Contains(p, `[#1] (fleet) Backups run at 02:00`) || !strings.Contains(p, `[#2] (host "web-1" id `+itoa(hostID)+`) nginx sits behind Cloudflare`) {
		t.Fatalf("notes missing from prompt: %s", p)
	}

	if out := call("memory_save", `{"content":"Backups run at 03:00","replaces":1}`); !strings.Contains(out, "rewritten") {
		t.Fatalf("replace: %s", out)
	}
	if out := call("memory_list", `{"host_id":`+itoa(hostID)+`}`); !strings.Contains(out, "03:00") || !strings.Contains(out, "Cloudflare") {
		t.Fatalf("list host: %s", out)
	}
	if out := call("memory_forget", `{"id":2}`); !strings.Contains(out, "deleted") {
		t.Fatalf("forget: %s", out)
	}
	if p := srv.agentMemoryPrompt(); strings.Contains(p, "Cloudflare") || strings.Contains(p, "02:00") {
		t.Fatalf("forgotten/rewritten text still in prompt: %s", p)
	}
}

func TestAgentMemoryPromptRespectsBudget(t *testing.T) {
	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	for i := range 40 {
		_, _ = db.AddAgentMemory(nil, strings.Repeat("w", 400)+itoa(int64(i)), "agent")
	}
	p := srv.agentMemoryPrompt()
	if len(p) > agentMemoryBudget+300 || !strings.Contains(p, "more notes not shown") {
		t.Fatalf("prompt not bounded (%d bytes)", len(p))
	}
}

func TestAgentMemoryAPI(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()
	token := "memory_api_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	do := func(method, url, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "pantau_session", Value: token})
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		return w
	}
	if w := httptest.NewRecorder(); true {
		srv.ServeHTTP(w, httptest.NewRequest("GET", "/api/agent/memory", nil))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("needs a session: %d", w.Code)
		}
	}
	if w := do("POST", "/api/agent/memory", `{"content":"Staging is host b"}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"source":"operator"`) {
		t.Fatalf("add: %d %s", w.Code, w.Body.String())
	}
	if w := do("POST", "/api/agent/memory", `{"content":"x","host_id":999}`); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown host: %d", w.Code)
	}
	if w := do("POST", "/api/agent/memory", `{"id":1,"content":"Staging is host c"}`); w.Code != 200 {
		t.Fatalf("edit: %d", w.Code)
	}
	if w := do("GET", "/api/agent/memory", ""); !strings.Contains(w.Body.String(), "host c") {
		t.Fatalf("list: %s", w.Body.String())
	}
	if w := do("DELETE", "/api/agent/memory?id=1", ""); w.Code != 200 {
		t.Fatalf("delete: %d", w.Code)
	}
	if w := do("DELETE", "/api/agent/memory?id=1", ""); w.Code != http.StatusNotFound {
		t.Fatalf("delete missing: %d", w.Code)
	}
}
