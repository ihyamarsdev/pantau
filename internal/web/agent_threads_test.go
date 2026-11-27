package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pantau/internal/inspector"
	"pantau/internal/store"
)

// agentTurn runs one chat turn and returns the recorder.
func agentTurn(srv *Server, session, message string) *httptest.ResponseRecorder {
	return chatPost(srv, session, message)
}

// A conversation survives a restart: the stored thread is read back and the model sees the earlier turns.
func TestAgentHistorySurvivesRestart(t *testing.T) {
	t.Parallel()
	f := &fakeLLM{results: []llmResult{{Text: "first answer"}}}
	srv, db := nativeAgent(t, f)

	if w := agentTurn(srv, "s1", "why is nginx down?"); !strings.Contains(w.Body.String(), "data: [DONE]") {
		t.Fatalf("first turn: %s", w.Body.String())
	}
	th, err := db.AgentThreadBySession("s1")
	if err != nil || th == nil {
		t.Fatalf("the turn must open a thread: %v", err)
	}
	if th.Title != "why is nginx down?" {
		t.Fatalf("the first question must title the thread: %q", th.Title)
	}

	// Restart: the in-memory conversations are gone, the database is not.
	srv.agent.mu.Lock()
	srv.agent.convs = nil
	srv.agent.mu.Unlock()
	f.mu.Lock()
	f.calls, f.results = nil, []llmResult{{Text: "second answer"}}
	f.mu.Unlock()

	agentTurn(srv, "s1", "and again?")
	msgs := f.call(0).Messages
	if len(msgs) < 3 {
		t.Fatalf("the resumed history must hold the earlier turn: %+v", msgs)
	}
	if msgs[0].Role != llmRoleUser || msgs[0].Text != "why is nginx down?" {
		t.Fatalf("first stored message: %+v", msgs[0])
	}
	if msgs[1].Role != llmRoleAssistant || msgs[1].Text != "first answer" {
		t.Fatalf("second stored message: %+v", msgs[1])
	}
	if last := msgs[len(msgs)-1]; last.Role != llmRoleUser || last.Text != "and again?" {
		t.Fatalf("the new question must come last: %+v", last)
	}

	// Nothing new was stored for an unchanged question: four rows for two turns.
	rows, err := db.ListAgentMessages(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("two turns store four messages, got %d", len(rows))
	}
}

// A tool result is redacted before it is stored, while a vault placeholder stays a placeholder.
func TestAgentHistoryRedaction(t *testing.T) {
	t.Parallel()
	secret := "API_TOKEN=supersecret123"
	if got := agentRedactText("run " + secret); strings.Contains(got, "supersecret123") || !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("a secret assignment must be redacted: %q", got)
	}
	if got := agentRedactText("token: hunter2"); strings.Contains(got, "hunter2") {
		t.Fatalf("a token assignment must be redacted: %q", got)
	}
	blind := "env: TOKEN={{ vault.DB_PASSWORD }} and API_KEY={{vault.API_TOKEN}}"
	if got := agentRedactText(blind); got != blind {
		t.Fatalf("a blind secret must stay intact: %q", got)
	}
	// A stored value the vault redactor knows about is masked too, even inside prose.
	vaultRedactor.AddValue("hunter2")
	defer vaultRedactor.SetValues(nil)
	if got := agentRedactText("the value is hunter2"); strings.Contains(got, "hunter2") {
		t.Fatalf("a known vault value must be redacted: %q", got)
	}

	f := &fakeLLM{results: []llmResult{{Text: "fine"}}}
	srv, db := nativeAgent(t, f)
	th, err := db.CreateAgentThread("host", 3, "s9", "")
	if err != nil {
		t.Fatal(err)
	}
	srv.saveAgentHistory(th.ID, []llmMessage{
		{Role: llmRoleUser, Text: "check the env file"},
		{Role: llmRoleTool, ToolCallID: "c1", ToolName: "read_file", Text: "DB_PASSWORD=hunter2\nURL={{ vault.URL }}"},
	}, 0, false)
	rows, err := db.ListAgentMessages(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows: %+v", rows)
	}
	if strings.Contains(rows[1].Content, "hunter2") || !strings.Contains(rows[1].Content, "[REDACTED]") {
		t.Fatalf("the stored tool output must be redacted: %q", rows[1].Content)
	}
	if !strings.Contains(rows[1].Content, "{{ vault.URL }}") {
		t.Fatalf("the blind secret must survive: %q", rows[1].Content)
	}

	// A denied change is marked in the stored row.
	srv.saveAgentHistory(th.ID, []llmMessage{
		{Role: llmRoleUser, Text: "restart it"},
		{Role: llmRoleTool, ToolCallID: "c2", ToolName: "systemd_action", Text: "the operator denied this action", IsError: true},
	}, 0, false)
	rows, _ = db.ListAgentMessages(th.ID)
	if last := rows[len(rows)-1]; last.Approval != "denied" {
		t.Fatalf("a denied action must be marked: %+v", last)
	}
}

// A long tool result is clipped for storage, so one log dump cannot grow the database without bound.
func TestAgentStoredToolClipped(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", agentStoredToolMax+500)
	m := agentMessageFromLLM(llmMessage{Role: llmRoleTool, ToolCallID: "c", ToolName: "docker_logs", Text: long})
	if len(m.Content) >= len(long) {
		t.Fatalf("an oversized tool result must be clipped: %d", len(m.Content))
	}
	if !strings.Contains(m.Content, "truncated") {
		t.Fatalf("the clip must be announced: %q", m.Content[max(0, len(m.Content)-80):])
	}
	// A user message is stored whole: the operator's own words are never cut.
	user := strings.Repeat("y", agentStoredToolMax)
	if got := agentMessageFromLLM(llmMessage{Role: llmRoleUser, Text: user}).Content; got != user {
		t.Fatal("a user message must be stored whole")
	}
}

// The scope of a new thread follows the resource the chat is opened from.
func TestAgentScopeFor(t *testing.T) {
	t.Parallel()
	cases := []struct {
		page     string
		host     int64
		cluster  int64
		wantType string
		wantID   int64
	}{
		{"", 0, 0, "global", 0},
		{"hosts", 4, 0, "host", 4},
		{"kube", 0, 2, "cluster", 2},
		{"docker", 0, 0, "docker", 0},
		{"ansible", 0, 0, "ansible", 0},
		{"terraform", 0, 0, "terraform", 0},
	}
	for _, c := range cases {
		gotType, gotID := agentScopeFor(c.page, c.host, c.cluster)
		if gotType != c.wantType || gotID != c.wantID {
			t.Errorf("agentScopeFor(%q,%d,%d) = %s/%d, want %s/%d", c.page, c.host, c.cluster, gotType, gotID, c.wantType, c.wantID)
		}
	}
}

func TestAgentThreadEndpoints(t *testing.T) {
	t.Parallel()
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()

	do := func(method, target, body string) *httptest.ResponseRecorder {
		var r *http.Request
		if body == "" {
			r = httptest.NewRequest(method, target, nil)
		} else {
			r = httptest.NewRequest(method, target, strings.NewReader(body))
		}
		w := httptest.NewRecorder()
		if strings.HasPrefix(target, "/api/agent/threads/") {
			srv.handleAgentThread(w, r)
		} else {
			srv.handleAgentThreads(w, r)
		}
		return w
	}

	w := do(http.MethodPost, "/api/agent/threads", `{"scope_type":"host","scope_id":5,"session_id":"s1","title":"nginx"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var th struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &th)
	if th.ID == 0 {
		t.Fatalf("no thread id: %s", w.Body.String())
	}

	if w := do(http.MethodPost, "/api/agent/threads", `{"scope_type":"banana"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("a bad scope must be refused: %d", w.Code)
	}

	w = do(http.MethodGet, "/api/agent/threads?scope_type=host&scope_id=5", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"title":"nginx"`) {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	if w := do(http.MethodGet, "/api/agent/threads?scope_type=host&scope_id=6", ""); strings.Contains(w.Body.String(), `"title":"nginx"`) {
		t.Fatalf("another scope must not list it: %s", w.Body.String())
	}
	w = do(http.MethodGet, "/api/agent/threads?session=s1", "")
	if !strings.Contains(w.Body.String(), `"title":"nginx"`) {
		t.Fatalf("session lookup: %s", w.Body.String())
	}

	if err := srv.db.AppendAgentMessages(th.ID, []store.AgentMessage{{Role: "user", Content: "hi"}}); err != nil {
		t.Fatal(err)
	}
	w = do(http.MethodGet, "/api/agent/threads/"+jsonNum(th.ID), "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"content":"hi"`) {
		t.Fatalf("thread detail: %d %s", w.Code, w.Body.String())
	}

	if w := do(http.MethodDelete, "/api/agent/threads/"+jsonNum(th.ID), ""); w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	if w := do(http.MethodGet, "/api/agent/threads/"+jsonNum(th.ID), ""); w.Code != http.StatusNotFound {
		t.Fatalf("a deleted thread must be gone: %d", w.Code)
	}

	_ = do(http.MethodPost, "/api/agent/threads", `{"scope_type":"docker","session_id":"s2"}`)
	w = do(http.MethodDelete, "/api/agent/threads?scope_type=docker", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"deleted":1`) {
		t.Fatalf("clear by scope: %d %s", w.Code, w.Body.String())
	}
}

// jsonNum renders an id for a URL path without importing strconv into the test twice.
func jsonNum(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// The redaction runs through the same guard the tools use, so a private key block never lands in the database.
func TestAgentStoredPrivateKeyRedacted(t *testing.T) {
	t.Parallel()
	key := "-----BEGIN PRIVATE KEY-----\nMIIabc\n-----END PRIVATE KEY-----"
	got := agentRedactText("cat id_rsa: " + key)
	if strings.Contains(got, "MIIabc") {
		t.Fatalf("a private key block must be redacted: %q", got)
	}
	if got != inspector.RedactAgentSecrets(got) && !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("unexpected redaction: %q", got)
	}
}
