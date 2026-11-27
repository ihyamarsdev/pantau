package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

// fakeLLM is a scripted llmClient: one result per call, in order.
type fakeLLM struct {
	mu      sync.Mutex
	results []llmResult
	errs    []error
	calls   []llmRequest
}

func (f *fakeLLM) Stream(ctx context.Context, req llmRequest, onText func(string)) (llmResult, error) {
	f.mu.Lock()
	i := len(f.calls)
	f.calls = append(f.calls, req)
	f.mu.Unlock()
	if i >= len(f.results) {
		return llmResult{}, fmt.Errorf("fakeLLM: no scripted result for call %d", i+1)
	}
	if err := ctx.Err(); err != nil {
		return llmResult{}, err
	}
	if i < len(f.errs) && f.errs[i] != nil {
		return llmResult{}, f.errs[i]
	}
	res := f.results[i]
	if onText != nil && res.Text != "" {
		onText(res.Text)
	}
	return res, nil
}

func (f *fakeLLM) call(n int) llmRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	if n >= len(f.calls) {
		return llmRequest{}
	}
	return f.calls[n]
}

func (f *fakeLLM) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// useFakeLLM makes the server use the scripted client instead of a real provider.
func useFakeLLM(srv *Server, f *fakeLLM) {
	srv.agent.llmFactory = func(provider, baseURL, apiKey string) (llmClient, error) { return f, nil }
}

// nativeAgent prepares a server with the native provider configured.
func nativeAgent(t *testing.T, f *fakeLLM) (*Server, *store.DB) {
	t.Helper()
	srv, db, cleanup := setupTestServer(t)
	t.Cleanup(cleanup)
	_ = db.SetSetting("agent_enabled", "1")
	_ = db.SetSetting("agent_llm_provider", "openai")
	_ = db.SetSetting("agent_llm_model", "test-model")
	_ = db.SetSetting("agent_llm_api_key", "sk-test")
	useFakeLLM(srv, f)
	return srv, db
}

func chatPost(srv *Server, session, message string) *httptest.ResponseRecorder {
	return chatPostPage(srv, session, message, "")
}

func chatPostPage(srv *Server, session, message, page string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"message": message, "session_id": session, "page": page})
	w := httptest.NewRecorder()
	srv.handleAgentChat(w, httptest.NewRequest(http.MethodPost, "/api/agent/chat", strings.NewReader(string(body))))
	return w
}

// sseFrames splits an SSE body into its frames.
func sseFrames(body string) []string {
	out := []string{}
	for _, part := range strings.Split(body, "\n\n") {
		if strings.TrimSpace(part) != "" {
			out = append(out, part)
		}
	}
	return out
}

func TestAgentLoopTextOnlyTurn(t *testing.T) {
	t.Parallel()
	f := &fakeLLM{results: []llmResult{{Text: "nginx is up"}}}
	srv, _ := nativeAgent(t, f)

	w := chatPost(srv, "s1", "is nginx up?")
	body := w.Body.String()
	if !strings.Contains(body, `"content":"nginx is up"`) {
		t.Fatalf("no text delta: %s", body)
	}
	if !strings.HasSuffix(strings.TrimSpace(body), "data: [DONE]") {
		t.Fatalf("no [DONE]: %s", body)
	}
	if f.callCount() != 1 {
		t.Fatalf("one model call expected, got %d", f.callCount())
	}
	req := f.call(0)
	if req.Model != "test-model" {
		t.Fatalf("model: %q", req.Model)
	}
	if !strings.Contains(req.System, "Pantau Operator") || !strings.Contains(req.System, "## Memory") {
		t.Fatalf("system prompt must carry the SOUL and the memory: %s", req.System)
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != llmRoleUser || req.Messages[0].Text != "is nginx up?" {
		t.Fatalf("messages: %+v", req.Messages)
	}
	if len(req.Tools) != len(agentTools) {
		t.Fatalf("tools must be offered: %d", len(req.Tools))
	}
}

func TestAgentLoopToolRoundTrip(t *testing.T) {
	t.Parallel()
	f := &fakeLLM{results: []llmResult{
		{Text: "let me look", ToolCalls: []llmToolCall{{ID: "c1", Name: "list_hosts", Args: "{}"}}},
		{Text: "one host"},
	}}
	srv, _ := nativeAgent(t, f)

	w := chatPost(srv, "s1", "list the hosts")
	body := w.Body.String()
	if !strings.Contains(body, "event: agent.tool") || !strings.Contains(body, `"tool":"list_hosts"`) {
		t.Fatalf("no tool event: %s", body)
	}
	if !strings.Contains(body, `"status":"running"`) || !strings.Contains(body, `"status":"done"`) {
		t.Fatalf("tool progress missing: %s", body)
	}
	if f.callCount() != 2 {
		t.Fatalf("two model calls expected, got %d", f.callCount())
	}
	second := f.call(1)
	if len(second.Messages) != 3 {
		t.Fatalf("history must hold user, assistant and tool result: %+v", second.Messages)
	}
	if second.Messages[1].Role != llmRoleAssistant || len(second.Messages[1].ToolCalls) != 1 || second.Messages[1].ToolCalls[0].ID != "c1" {
		t.Fatalf("assistant message: %+v", second.Messages[1])
	}
	if second.Messages[2].Role != llmRoleTool || second.Messages[2].ToolCallID != "c1" || second.Messages[2].IsError {
		t.Fatalf("tool result: %+v", second.Messages[2])
	}
	if !strings.Contains(second.Messages[2].Text, "id") {
		t.Fatalf("tool result text: %q", second.Messages[2].Text)
	}
}

func TestAgentLoopToolErrorContinues(t *testing.T) {
	t.Parallel()
	f := &fakeLLM{results: []llmResult{
		{ToolCalls: []llmToolCall{{ID: "c1", Name: "read_file", Args: `{"host_id":1,"path":"etc/passwd"}`}}},
		{Text: "I could not read it"},
	}}
	srv, _ := nativeAgent(t, f)

	w := chatPost(srv, "s1", "check nginx")
	body := w.Body.String()
	if !strings.Contains(body, `"status":"error"`) {
		t.Fatalf("a failing tool must be reported as an error: %s", body)
	}
	if f.callCount() != 2 {
		t.Fatalf("the loop must continue after a tool error, got %d calls", f.callCount())
	}
	second := f.call(1)
	last := second.Messages[len(second.Messages)-1]
	if last.Role != llmRoleTool || !last.IsError {
		t.Fatalf("the model must see the failure as an error result: %+v", last)
	}
	if !strings.Contains(last.Text, "ERROR:") {
		t.Fatalf("error text: %q", last.Text)
	}
}

func TestAgentLoopInvalidToolJSON(t *testing.T) {
	t.Parallel()
	f := &fakeLLM{results: []llmResult{
		{ToolCalls: []llmToolCall{{ID: "c1", Name: "list_hosts", Args: "{not json"}}},
		{Text: "sorry"},
	}}
	srv, _ := nativeAgent(t, f)

	w := chatPost(srv, "s1", "list the hosts")
	if !strings.Contains(w.Body.String(), "data: [DONE]") {
		t.Fatalf("the turn must finish: %s", w.Body.String())
	}
	if f.callCount() != 2 {
		t.Fatalf("the loop must continue after bad JSON, got %d calls", f.callCount())
	}
	last := f.call(1).Messages
	lastMsg := last[len(last)-1]
	if lastMsg.Role != llmRoleTool || !lastMsg.IsError || !strings.Contains(lastMsg.Text, "not valid JSON") {
		t.Fatalf("bad arguments must be an error result: %+v", lastMsg)
	}
}

func TestAgentLoopUnknownToolIsAnError(t *testing.T) {
	t.Parallel()
	f := &fakeLLM{results: []llmResult{
		{ToolCalls: []llmToolCall{{ID: "c1", Name: "drop_database", Args: "{}"}}},
		{Text: "I cannot do that"},
	}}
	srv, _ := nativeAgent(t, f)
	chatPost(srv, "s1", "drop everything")
	msgs := f.call(1).Messages
	last := msgs[len(msgs)-1]
	if !last.IsError || !strings.Contains(last.Text, "unknown tool") {
		t.Fatalf("unknown tool must be an error result: %+v", last)
	}
}

func TestAgentLoopStepLimit(t *testing.T) {
	t.Parallel()
	f := &fakeLLM{}
	for range agentMaxSteps + 5 {
		f.results = append(f.results, llmResult{ToolCalls: []llmToolCall{{ID: "c", Name: "list_hosts", Args: "{}"}}})
	}
	srv, _ := nativeAgent(t, f)

	w := chatPost(srv, "s1", "loop forever")
	body := w.Body.String()
	if !strings.Contains(body, "event: agent.error") || !strings.Contains(body, "25 steps") {
		t.Fatalf("the step limit must end the turn with an error: %s", body)
	}
	if strings.Contains(body, "data: [DONE]") {
		t.Fatalf("a failed turn must not end with [DONE]: %s", body)
	}
	if f.callCount() != agentMaxSteps {
		t.Fatalf("%d model calls expected, got %d", agentMaxSteps, f.callCount())
	}
}

func TestAgentLoopContextCancelRollsHistoryBack(t *testing.T) {
	t.Parallel()
	f := &fakeLLM{results: []llmResult{
		{Text: "first answer"},
	}}
	srv, _ := nativeAgent(t, f)

	if w := chatPost(srv, "s1", "hello"); !strings.Contains(w.Body.String(), "data: [DONE]") {
		t.Fatalf("first turn: %s", w.Body.String())
	}
	// The second turn is cancelled after the model asked for a tool: the tool ran, so the history must keep
	// its tool_use and its result, and never leave a dangling tool_use.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv.agent.llmFactory = func(provider, baseURL, apiKey string) (llmClient, error) {
		return &cancellingLLM{cancel: cancel}, nil
	}
	body, _ := json.Marshal(map[string]string{"message": "again", "session_id": "s1"})
	r := httptest.NewRequest(http.MethodPost, "/api/agent/chat", strings.NewReader(string(body))).WithContext(ctx)
	srv.handleAgentChat(httptest.NewRecorder(), r)

	// The next turn must not see a dangling assistant tool_use.
	useFakeLLM(srv, f)
	f.mu.Lock()
	f.calls = nil
	f.results = []llmResult{{Text: "third"}}
	f.mu.Unlock()
	chatPost(srv, "s1", "third question")
	req := f.call(0)
	assertNoDanglingToolUse(t, req.Messages)
	if !hasToolPair(req.Messages, "c9") {
		t.Fatalf("the tool that ran must stay in the history: %+v", req.Messages)
	}
}

// assertNoDanglingToolUse fails when an assistant tool_use has no tool result.
func assertNoDanglingToolUse(t *testing.T, msgs []llmMessage) {
	t.Helper()
	open := map[string]bool{}
	for _, m := range msgs {
		for _, tc := range m.ToolCalls {
			open[tc.ID] = true
		}
		if m.Role == llmRoleTool {
			delete(open, m.ToolCallID)
		}
	}
	if len(open) != 0 {
		t.Fatalf("the history kept a dangling tool_use: %+v", msgs)
	}
}

// hasToolPair reports whether the history holds the tool_use of that id and its result.
func hasToolPair(msgs []llmMessage, id string) bool {
	used, done := false, false
	for _, m := range msgs {
		for _, tc := range m.ToolCalls {
			if tc.ID == id {
				used = true
			}
		}
		if m.Role == llmRoleTool && m.ToolCallID == id {
			done = true
		}
	}
	return used && done
}

// TestAgentLoopKeepsDoneToolAfterProviderFailure: the provider fails on the second model call, after a tool
// ran. The next turn must still know that the tool ran, or it would repeat the action.
func TestAgentLoopKeepsDoneToolAfterProviderFailure(t *testing.T) {
	t.Parallel()
	f := &fakeLLM{results: []llmResult{
		{ToolCalls: []llmToolCall{{ID: "c1", Name: "list_hosts", Args: "{}"}}},
		{Text: "never reached"},
	}}
	f.errs = []error{nil, fmt.Errorf("provider down")}
	srv, _ := nativeAgent(t, f)

	w := chatPost(srv, "s1", "list the hosts")
	if !strings.Contains(w.Body.String(), "event: agent.error") {
		t.Fatalf("the provider failure must be reported: %s", w.Body.String())
	}

	useFakeLLM(srv, f)
	f.mu.Lock()
	f.calls = nil
	f.errs = nil
	f.results = []llmResult{{Text: "third"}}
	f.mu.Unlock()
	chatPost(srv, "s1", "and now?")
	req := f.call(0)
	assertNoDanglingToolUse(t, req.Messages)
	if !hasToolPair(req.Messages, "c1") {
		t.Fatalf("the tool that ran must stay in the history: %+v", req.Messages)
	}
}

// TestAgentLoopMidBatchCancelClosesTheBatch: the chat is cancelled between two calls of one batch. The second
// call must not run, but it still gets a result, so the history stays valid and the batch is kept.
func TestAgentLoopMidBatchCancelClosesTheBatch(t *testing.T) {
	f := &fakeLLM{results: []llmResult{
		{ToolCalls: []llmToolCall{{ID: "c1", Name: "test_cancel", Args: "{}"}, {ID: "c2", Name: "list_hosts", Args: "{}"}}},
		{Text: "never reached"},
	}}
	srv, _ := nativeAgent(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// A test tool that cancels the chat while it runs.
	agentTools = append(agentTools, agentTool{Name: "test_cancel", Description: "test", InputSchema: schema(nil, map[string]interface{}{}),
		run: func(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
			cancel()
			return "the action ran", nil
		}})
	defer func() { agentTools = agentTools[:len(agentTools)-1] }()

	body, _ := json.Marshal(map[string]string{"message": "do it", "session_id": "s1"})
	r := httptest.NewRequest(http.MethodPost, "/api/agent/chat", strings.NewReader(string(body))).WithContext(ctx)
	srv.handleAgentChat(httptest.NewRecorder(), r)

	useFakeLLM(srv, f)
	f.mu.Lock()
	f.calls = nil
	f.results = []llmResult{{Text: "third"}}
	f.mu.Unlock()
	chatPost(srv, "s1", "and now?")
	msgs := f.call(0).Messages
	assertNoDanglingToolUse(t, msgs)
	if !hasToolPair(msgs, "c1") || !hasToolPair(msgs, "c2") {
		t.Fatalf("both calls of the batch must have a result: %+v", msgs)
	}
	var second llmMessage
	for _, m := range msgs {
		if m.Role == llmRoleTool && m.ToolCallID == "c2" {
			second = m
		}
	}
	if !second.IsError || !strings.Contains(second.Text, "not run") {
		t.Fatalf("the call that did not run must be an error result: %+v", second)
	}
}

// cancellingLLM returns one tool call and then cancels the request: the turn fails with an assistant tool_use
// in the history, which the loop must roll back.
type cancellingLLM struct {
	cancel context.CancelFunc
	mu     sync.Mutex
	calls  int
}

func (c *cancellingLLM) Stream(ctx context.Context, req llmRequest, onText func(string)) (llmResult, error) {
	c.mu.Lock()
	c.calls++
	n := c.calls
	c.mu.Unlock()
	if n == 1 {
		return llmResult{ToolCalls: []llmToolCall{{ID: "c9", Name: "list_hosts", Args: "{}"}}}, nil
	}
	c.cancel()
	<-ctx.Done()
	return llmResult{}, ctx.Err()
}

func TestAgentLoopConcurrentTurnIsRefused(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	started := make(chan struct{})
	srv, _ := nativeAgent(t, &fakeLLM{})
	srv.agent.llmFactory = func(provider, baseURL, apiKey string) (llmClient, error) {
		return &blockingLLM{release: release, started: started, res: llmResult{Text: "hi"}}, nil
	}
	go chatPost(srv, "s1", "first")
	<-started
	w := chatPost(srv, "s1", "second")
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "still answering") {
		t.Fatalf("a second turn on a running session must be refused: %d %s", w.Code, w.Body.String())
	}
	close(release)
}

// blockingLLM waits until it is released.
type blockingLLM struct {
	release chan struct{}
	started chan struct{}
	res     llmResult
	once    sync.Once
}

func (b *blockingLLM) Stream(ctx context.Context, req llmRequest, onText func(string)) (llmResult, error) {
	b.once.Do(func() { close(b.started) })
	select {
	case <-b.release:
	case <-ctx.Done():
		return llmResult{}, ctx.Err()
	}
	if onText != nil {
		onText(b.res.Text)
	}
	return b.res, nil
}

func TestAgentLoopHistoryKeepsTurns(t *testing.T) {
	t.Parallel()
	f := &fakeLLM{results: []llmResult{{Text: "a"}, {Text: "b"}, {Text: "c"}}}
	srv, _ := nativeAgent(t, f)
	for _, m := range []string{"one", "two", "three"} {
		chatPost(srv, "s1", m)
	}
	req := f.call(2)
	if len(req.Messages) != 5 {
		t.Fatalf("three turns must be kept: %+v", req.Messages)
	}
	for _, msg := range req.Messages {
		if msg.Role == llmRoleUser && strings.Contains(msg.Text, "Pantau Operator") {
			t.Fatalf("the system prompt must not be stored in the history: %+v", msg)
		}
	}
}

func TestAgentCompactCutNeverSplitsToolPairs(t *testing.T) {
	t.Parallel()
	msgs := []llmMessage{
		{Role: llmRoleUser, Text: "u1"},
		{Role: llmRoleAssistant, ToolCalls: []llmToolCall{{ID: "c1", Name: "list_hosts"}}},
		{Role: llmRoleTool, ToolCallID: "c1"},
		{Role: llmRoleAssistant, Text: "a1"},
		{Role: llmRoleUser, Text: "u2"},
		{Role: llmRoleAssistant, ToolCalls: []llmToolCall{{ID: "c2", Name: "list_hosts"}}},
		{Role: llmRoleTool, ToolCallID: "c2"},
		{Role: llmRoleAssistant, Text: "a2"},
		{Role: llmRoleUser, Text: "u3"},
	}
	cut := agentCompactCut(msgs)
	if cut < 0 {
		t.Fatal("the history must be compactable")
	}
	if msgs[cut].Role != llmRoleUser {
		t.Fatalf("the cut must be at a user message: %+v", msgs[cut])
	}
	// The two newest user turns stay verbatim.
	if cut > 4 {
		t.Fatalf("the cut must keep the two newest user turns: %d", cut)
	}
	// No tool_use without its tool_result before the cut.
	ids := map[string]bool{}
	for _, m := range msgs[:cut] {
		for _, tc := range m.ToolCalls {
			ids[tc.ID] = true
		}
		if m.Role == llmRoleTool {
			delete(ids, m.ToolCallID)
		}
	}
	if len(ids) != 0 {
		t.Fatalf("the cut splits a tool pair: %v", ids)
	}
	if agentCompactCut(msgs[:2]) != -1 {
		t.Fatal("a short history must not be compacted")
	}
}

func TestAgentLoopCompactionHappens(t *testing.T) {
	t.Parallel()
	f := &fakeLLM{results: []llmResult{
		{Text: "a1"},
		{Text: "a2"},
		{Text: "a3", ToolCalls: []llmToolCall{{ID: "c1", Name: "list_hosts", Args: "{}"}}, InputTokens: agentCompactTokens + 1},
		{Text: "the summary"},
		{Text: "the answer"},
	}}
	srv, _ := nativeAgent(t, f)
	chatPost(srv, "s1", "one")
	chatPost(srv, "s1", "two")
	chatPost(srv, "s1", "three")
	// calls: two turns, then the third turn: model, summary, model.
	if f.callCount() != 5 {
		t.Fatalf("a summary call was expected: %d calls", f.callCount())
	}
	summaryCall := f.call(3)
	if len(summaryCall.Tools) != 0 {
		t.Fatalf("the summary call must not offer tools: %+v", summaryCall.Tools)
	}
	if len(summaryCall.Messages) != 1 || !strings.Contains(summaryCall.Messages[0].Text, "User:") {
		t.Fatalf("the summary call must carry the old history: %+v", summaryCall.Messages)
	}
	last := f.call(4)
	if last.Messages[0].Role != llmRoleUser || !strings.Contains(last.Messages[0].Text, "[Summary of the earlier conversation]") {
		t.Fatalf("the summary must replace the old history: %+v", last.Messages[0])
	}
}

func TestAgentLoopCompactionFailureKeepsHistory(t *testing.T) {
	t.Parallel()
	f := &fakeLLM{results: []llmResult{
		{Text: "a1"},
		{Text: "a2"},
		{Text: "a3", ToolCalls: []llmToolCall{{ID: "c1", Name: "list_hosts", Args: "{}"}}, InputTokens: agentCompactTokens + 1},
		{Text: "the answer"},
	}}
	f.errs = []error{nil, nil, nil, fmt.Errorf("provider down")}
	srv, _ := nativeAgent(t, f)
	chatPost(srv, "s1", "one")
	chatPost(srv, "s1", "two")
	chatPost(srv, "s1", "three")
	if f.callCount() != 5 {
		t.Fatalf("the failed summary must not break the turn: %d calls", f.callCount())
	}
	last := f.call(4)
	if len(last.Messages) != 7 {
		t.Fatalf("the full history must be kept: %+v", last.Messages)
	}
}

// TestAgentLoopCompactionWithoutUsage covers providers that report no token usage: the prompt size is then
// estimated, so a long history is compacted anyway.
func TestAgentLoopCompactionWithoutUsage(t *testing.T) {
	t.Parallel()
	f := &fakeLLM{results: []llmResult{
		{Text: "a1", ToolCalls: []llmToolCall{{ID: "c1", Name: "list_hosts", Args: "{}"}}},
		{Text: "the summary"},
		{Text: "the answer"},
	}}
	srv, _ := nativeAgent(t, f)
	// A long history without any reported usage.
	big := strings.Repeat("x", 80<<10)
	conv := &agentConv{lastUse: time.Now(), msgs: []llmMessage{
		{Role: llmRoleUser, Text: big},
		{Role: llmRoleAssistant, Text: big},
		{Role: llmRoleUser, Text: big},
		{Role: llmRoleAssistant, Text: big},
		{Role: llmRoleUser, Text: big},
	}}
	srv.agent.mu.Lock()
	srv.agent.convs = map[string]*agentConv{"s1": conv}
	srv.agent.mu.Unlock()

	chatPost(srv, "s1", "and now?")
	if f.callCount() != 3 {
		t.Fatalf("the estimated size must trigger a summary call: %d calls", f.callCount())
	}
	summaryCall := f.call(1)
	if len(summaryCall.Tools) != 0 {
		t.Fatalf("the summary call must not offer tools: %+v", summaryCall.Tools)
	}
	last := f.call(2)
	if !strings.Contains(last.Messages[0].Text, "[Summary of the earlier conversation]") {
		t.Fatalf("the summary must replace the old history: %+v", last.Messages[0])
	}
}

// TestAgentLoopCompactsAtTurnStart: a long text-only exchange never reaches a tool batch, so the next turn
// must compact before its first model call.
func TestAgentLoopCompactsAtTurnStart(t *testing.T) {
	t.Parallel()
	f := &fakeLLM{results: []llmResult{
		{Text: "the summary"},
		{Text: "the answer"},
	}}
	srv, _ := nativeAgent(t, f)
	conv := &agentConv{lastUse: time.Now(), lastTokens: agentCompactTokens + 1, msgs: []llmMessage{
		{Role: llmRoleUser, Text: "u1"},
		{Role: llmRoleAssistant, Text: "a1"},
		{Role: llmRoleUser, Text: "u2"},
		{Role: llmRoleAssistant, Text: "a2"},
		{Role: llmRoleUser, Text: "u3"},
	}}
	srv.agent.mu.Lock()
	srv.agent.convs = map[string]*agentConv{"s1": conv}
	srv.agent.mu.Unlock()

	chatPost(srv, "s1", "and now?")
	if f.callCount() != 2 {
		t.Fatalf("the summary must run before the first model call: %d calls", f.callCount())
	}
	summaryCall := f.call(0)
	if len(summaryCall.Tools) != 0 {
		t.Fatalf("the summary call must not offer tools: %+v", summaryCall.Tools)
	}
	if !strings.Contains(summaryCall.Messages[0].Text, "User:") {
		t.Fatalf("the summary call must carry the old history: %+v", summaryCall.Messages)
	}
	last := f.call(1)
	if !strings.Contains(last.Messages[0].Text, "[Summary of the earlier conversation]") {
		t.Fatalf("the summary must replace the old history: %+v", last.Messages[0])
	}
}

// TestAgentChatNativeOverHTTP drives the whole path with the real provider client against a fake OpenAI
// endpoint: the settings, the chat handler and the stream format.
func TestAgentChatNativeOverHTTP(t *testing.T) {
	t.Parallel()
	var gotAuth, gotBody string
	calls := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b := new(bytes.Buffer)
		_, _ = b.ReadFrom(r.Body)
		if calls == 0 {
			// The chat request is the first call; the conversation title request that follows is tool-less.
			gotBody = b.String()
		}
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		if calls == 1 {
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi \"}}]}\n\n" +
				"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c1\",\"function\":{\"name\":\"list_hosts\",\"arguments\":\"{}\"}}]}}]}\n\n" +
				"data: [DONE]\n\n"))
			return
		}
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"there\"}}]}\n\n" +
			"data: [DONE]\n\n"))
	}))
	defer provider.Close()

	srv, db, cleanup := setupTestServer(t)
	defer cleanup()
	_ = db.SetSetting("agent_enabled", "1")
	_ = db.SetSetting("agent_llm_provider", "openai")
	_ = db.SetSetting("agent_llm_base_url", provider.URL)
	_ = db.SetSetting("agent_llm_api_key", "sk-test")
	_ = db.SetSetting("agent_llm_model", "test-model")

	w := chatPost(srv, "s1", "hello")
	body := w.Body.String()
	if !strings.Contains(body, `"content":"hi "`) || !strings.Contains(body, `"content":"there"`) {
		t.Fatalf("the text deltas are missing: %s", body)
	}
	if !strings.Contains(body, "event: agent.tool") || !strings.Contains(body, `"tool":"list_hosts"`) {
		t.Fatalf("the tool event is missing: %s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("the stream must end with [DONE]: %s", body)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("the key must be sent: %q", gotAuth)
	}
	if !strings.Contains(gotBody, `"tools"`) || !strings.Contains(gotBody, "list_hosts") {
		t.Fatalf("the tools must be offered: %s", gotBody)
	}
}

func TestAgentPromptTokensEstimate(t *testing.T) {
	t.Parallel()
	msgs := []llmMessage{{Role: llmRoleUser, Text: strings.Repeat("y", 300)}}
	if got := agentPromptTokens("system", msgs, 4242); got != 4242 {
		t.Fatalf("the reported usage wins: %d", got)
	}
	if got := agentPromptTokens("system", msgs, 0); got != (len("system")+300)/3 {
		t.Fatalf("the estimate is wrong: %d", got)
	}
}

func TestAgentConvEviction(t *testing.T) {
	t.Parallel()
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()

	srv.agent.mu.Lock()
	srv.agent.convs = map[string]*agentConv{}
	srv.agent.mu.Unlock()
	for i := range agentMaxConvs + 5 {
		id := fmt.Sprintf("s%d", i)
		c, ok := srv.acquireConv(id)
		if !ok {
			t.Fatalf("%s must be free", id)
		}
		srv.endConv(c)
	}
	srv.agent.mu.Lock()
	n := len(srv.agent.convs)
	_, oldest := srv.agent.convs["s0"]
	srv.agent.mu.Unlock()
	if n > agentMaxConvs {
		t.Fatalf("too many conversations: %d", n)
	}
	if oldest {
		t.Fatal("the least recently used conversation must be dropped")
	}

	// An idle conversation is dropped and starts over.
	srv.agent.mu.Lock()
	c := srv.agent.convs["s24"]
	c.mu.Lock()
	c.lastUse = time.Now().Add(-agentConvTTL - time.Minute)
	c.mu.Unlock()
	srv.agent.mu.Unlock()
	if _, ok := srv.acquireConv("s24"); !ok {
		t.Fatal("s24 must be free")
	}
	srv.agent.mu.Lock()
	c = srv.agent.convs["s24"]
	c.mu.Lock()
	fresh := len(c.msgs) == 0
	c.mu.Unlock()
	srv.agent.mu.Unlock()
	if !fresh {
		t.Fatal("an idle conversation must start over")
	}
}

func TestAgentSettingsNativeRoundTrip(t *testing.T) {
	t.Parallel()
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()

	post := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		srv.handleAgentSettings(w, httptest.NewRequest(http.MethodPost, "/api/agent/settings", strings.NewReader(body)))
		return w
	}
	if w := post(`{"provider":"anthropic","model":"claude-x","api_key":"sk-secret"}`); w.Code != http.StatusOK {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	if w := post(`{"provider":"gemini"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("an unknown provider must be refused: %d %s", w.Code, w.Body.String())
	}
	if w := post(`{"base_url":"file:///etc/passwd"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("a non-http base URL must be refused: %d", w.Code)
	}
	w := httptest.NewRecorder()
	srv.handleAgentSettings(w, httptest.NewRequest(http.MethodGet, "/api/agent/settings", nil))
	body := w.Body.String()
	if strings.Contains(body, "sk-secret") {
		t.Fatalf("the API key must never be returned: %s", body)
	}
	if !strings.Contains(body, `"api_key_set":true`) || !strings.Contains(body, `"provider":"anthropic"`) {
		t.Fatalf("native settings missing: %s", body)
	}
	c := srv.agentConfig()
	if c.Provider != "anthropic" || c.Model != "claude-x" || c.Key != "sk-secret" {
		t.Fatalf("config: %+v", c)
	}
	// An empty key keeps the stored one.
	post(`{"api_key":""}`)
	if c := srv.agentConfig(); c.Key != "sk-secret" {
		t.Fatalf("the key must be kept: %+v", c)
	}
}

func TestAgentTestEndpointNative(t *testing.T) {
	t.Parallel()
	f := &fakeLLM{results: []llmResult{{Text: "pong"}}}
	srv, _ := nativeAgent(t, f)

	w := httptest.NewRecorder()
	srv.handleAgentTest(w, httptest.NewRequest(http.MethodPost, "/api/agent/test", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatalf("test: %d %s", w.Code, w.Body.String())
	}
	if f.callCount() != 1 {
		t.Fatalf("one model call expected, got %d", f.callCount())
	}
	if req := f.call(0); req.MaxTokens != 16 {
		t.Fatalf("the test call must be tiny: %+v", req)
	}

	f2 := &fakeLLM{errs: []error{fmt.Errorf("provider refused the key sk-test")}}
	srv2, _ := nativeAgent(t, f2)
	w2 := httptest.NewRecorder()
	srv2.handleAgentTest(w2, httptest.NewRequest(http.MethodPost, "/api/agent/test", nil))
	if !strings.Contains(w2.Body.String(), `"ok":false`) {
		t.Fatalf("a failing provider must be reported: %s", w2.Body.String())
	}
	if strings.Contains(w2.Body.String(), "sk-test") {
		t.Fatalf("the key must not leak into the error: %s", w2.Body.String())
	}
}

func TestAgentChatNativeNeedsModelAndKey(t *testing.T) {
	t.Parallel()
	f := &fakeLLM{results: []llmResult{{Text: "hi"}}}
	srv, db := nativeAgent(t, f)
	_ = db.SetSetting("agent_llm_model", "")
	w := chatPost(srv, "s1", "hello")
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "model") {
		t.Fatalf("a missing model must be refused: %d %s", w.Code, w.Body.String())
	}
	_ = db.SetSetting("agent_llm_model", "test-model")
	_ = db.SetSetting("agent_llm_provider", "anthropic")
	_ = db.SetSetting("agent_llm_api_key", "")
	w = chatPost(srv, "s1", "hello")
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "key") {
		t.Fatalf("a missing key must be refused: %d %s", w.Code, w.Body.String())
	}
}

// TestAgentLoopGate2WaitsForApproval drives a gate-2 tool through the approval endpoints.
func TestAgentLoopGate2WaitsForApproval(t *testing.T) {
	t.Parallel()
	for _, approve := range []bool{true, false} {
		f := &fakeLLM{results: []llmResult{
			{ToolCalls: []llmToolCall{{ID: "c1", Name: "exec_command", Args: `{"host_id":1,"command":"uptime"}`}}},
			{Text: "done"},
		}}
		srv, db := nativeAgent(t, f)
		mock := sshrunner.NewMockRunner()
		mock.DefaultExec = func(cmd string) (string, string, int, error) { return "ok\n", "", 0, nil }
		ins := inspector.New(db, func(t sshrunner.Target) (sshrunner.Runner, error) { return mock, nil }, nil)
		srv.inspector = ins
		id, err := db.CreateHost(&store.Host{Name: "h1", Host: "127.0.0.1", Port: 22, User: "root", Status: "healthy"})
		if err != nil {
			t.Fatalf("host: %v", err)
		}
		f.results[0].ToolCalls[0].Args = fmt.Sprintf(`{"host_id":%d,"command":"uptime"}`, id)

		go func() {
			for range 200 {
				srv.agent.mu.Lock()
				var apID string
				for k := range srv.agent.pending {
					apID = k
				}
				srv.agent.mu.Unlock()
				if apID != "" {
					w := httptest.NewRecorder()
					b, _ := json.Marshal(map[string]bool{"approve": approve})
					srv.handleAgentApprovalDecide(w, httptest.NewRequest(http.MethodPost, "/api/agent/approvals/"+apID, strings.NewReader(string(b))))
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
		}()

		w := chatPost(srv, "s1", "run uptime")
		if !strings.Contains(w.Body.String(), "data: [DONE]") {
			t.Fatalf("approve=%v: the turn must finish: %s", approve, w.Body.String())
		}
		msgs := f.call(1).Messages
		last := msgs[len(msgs)-1]
		if approve {
			if last.IsError {
				t.Fatalf("an approved command must not be an error: %+v", last)
			}
		} else {
			if !last.IsError || !strings.Contains(last.Text, "denied") {
				t.Fatalf("a denied command must reach the model as an error: %+v", last)
			}
		}
	}
}

// Domain-aware filtering: a page context narrows the tools, an empty context keeps all of them, and a
// set_tool_domain meta-tool widens the domain for the next model call in the same turn.
func TestAgentDomainFiltering(t *testing.T) {
	t.Parallel()
	has := func(specs []llmToolSpec, name string) bool {
		for _, s := range specs {
			if s.Name == name {
				return true
			}
		}
		return false
	}
	all := agentToolSpecsFor(domainAll)
	if len(all) != len(agentTools) {
		t.Fatalf("no context must offer all tools: %d vs %d", len(all), len(agentTools))
	}
	host := agentToolSpecsFor(domainHost)
	if !has(host, "systemd_list") || !has(host, "list_hosts") || !has(host, "find_tools") {
		t.Fatalf("host domain must carry host + core + meta tools")
	}
	if has(host, "kube_get") || has(host, "cf_dns_list") {
		t.Fatalf("host domain must not carry infra/network tools")
	}
	net := agentToolSpecsFor(domainNetwork)
	if !has(net, "cf_dns_list") || has(net, "systemd_list") {
		t.Fatalf("network domain must carry only network tools")
	}
	if len(host) >= len(all) || len(net) >= len(all) {
		t.Fatalf("a domain must be smaller than the full list")
	}
	if agentDomainForContext("cloudflare") != domainNetwork || agentDomainForContext("") != domainAll {
		t.Fatal("page-to-domain mapping is wrong")
	}
}

func TestAgentMetaToolCrossDomainFallback(t *testing.T) {
	t.Parallel()
	f := &fakeLLM{results: []llmResult{
		{Text: "looking", ToolCalls: []llmToolCall{{ID: "c1", Name: "find_tools", Args: `{"query":"dns record"}`}}},
		{Text: "switching", ToolCalls: []llmToolCall{{ID: "c2", Name: "set_tool_domain", Args: `{"domain":"network"}`}}},
		{Text: "done"},
	}}
	srv, _ := nativeAgent(t, f)
	w := chatPostPage(srv, "s1", "check dns", "hosts")
	if !strings.Contains(w.Body.String(), "data: [DONE]") {
		t.Fatalf("turn must finish: %s", w.Body.String())
	}
	found := f.call(1).Messages
	last := found[len(found)-1]
	if last.IsError || !strings.Contains(last.Text, "cf_dns_list") {
		t.Fatalf("find_tools must list a cross-domain tool: %+v", last)
	}
	// After set_tool_domain, the next model call must offer the network tools.
	switched := f.call(2)
	var hasCF bool
	for _, s := range switched.Tools {
		if s.Name == "cf_dns_list" {
			hasCF = true
		}
	}
	if !hasCF {
		t.Fatal("set_tool_domain must widen the advertised tools for the next call")
	}
}

// The Test Fast Model button posts target "fast". With no fast model set, it must fall back to the primary model
// and report the answer, and a fast-only failure must come back as ok:false without echoing the fast key.
func TestAgentTestEndpointFastTarget(t *testing.T) {
	t.Parallel()
	f := &fakeLLM{results: []llmResult{{Text: "pong"}}}
	srv, _ := nativeAgent(t, f)

	w := httptest.NewRecorder()
	srv.handleAgentTest(w, httptest.NewRequest(http.MethodPost, "/api/agent/test", strings.NewReader(`{"target":"fast"}`)))
	if !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatalf("fast target must fall back to the primary model: %s", w.Body.String())
	}
	if f.callCount() != 1 {
		t.Fatalf("one fallback call expected, got %d", f.callCount())
	}
}
