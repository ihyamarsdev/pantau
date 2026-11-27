package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// llmCapture records what a fake provider received.
type llmCapture struct {
	mu      sync.Mutex
	bodies  []string
	headers []http.Header
	calls   int
}

func (c *llmCapture) record(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bodies = append(c.bodies, string(body))
	c.headers = append(c.headers, r.Header.Clone())
	c.calls++
}

func (c *llmCapture) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func (c *llmCapture) body(i int) map[string]interface{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	if i >= len(c.bodies) {
		return nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(c.bodies[i]), &out); err != nil {
		return nil
	}
	return out
}

func (c *llmCapture) header(i int) http.Header {
	c.mu.Lock()
	defer c.mu.Unlock()
	if i >= len(c.headers) {
		return nil
	}
	return c.headers[i]
}

// llmShortRetry makes the retry backoff short for the tests.
func llmShortRetry(t *testing.T) {
	t.Helper()
	old := llmRetryBase
	llmRetryBase = time.Millisecond
	t.Cleanup(func() { llmRetryBase = old })
}

// llmShortIdle shortens the stream watchdog for the tests.
func llmShortIdle(t *testing.T, d time.Duration) {
	t.Helper()
	old := llmIdleTimeout
	llmIdleTimeout = d
	t.Cleanup(func() { llmIdleTimeout = old })
}

// llmSSE writes an SSE stream.
func llmSSEWrite(w http.ResponseWriter, events ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	fl, _ := w.(http.Flusher)
	for _, e := range events {
		io.WriteString(w, e)
		if fl != nil {
			fl.Flush()
		}
	}
}

const llmAnthropicTextStream = `event: message_start
data: {"type":"message_start","message":{"usage":{"input_tokens":10,"cache_creation_input_tokens":2,"cache_read_input_tokens":3}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","usage":{"output_tokens":7}}

event: ping
data: {"type":"ping"}

event: message_stop
data: {"type":"message_stop"}

`

func llmAnthropicToolStream(name, id, frag1, frag2 string) string {
	return fmt.Sprintf(`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":%q,"name":%q}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":%q}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":%q}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","usage":{"output_tokens":5}}

event: message_stop
data: {"type":"message_stop"}

`, id, name, frag1, frag2)
}

// ---------------------------------------------------------------- Anthropic

func TestLLMAnthropicTextStream(t *testing.T) {
	t.Parallel()
	cap := &llmCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		llmSSEWrite(w, llmAnthropicTextStream)
	}))
	defer srv.Close()

	c, err := newLLMClient("anthropic", srv.URL, "sk-secret")
	if err != nil {
		t.Fatalf("newLLMClient: %v", err)
	}
	var deltas []string
	res, err := c.Stream(context.Background(), llmRequest{
		Model:  "claude-x",
		System: "you are Pantau",
		Messages: []llmMessage{
			{Role: llmRoleUser, Text: "hi"},
		},
	}, func(s string) { deltas = append(deltas, s) })
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if res.Text != "Hello world" {
		t.Errorf("text = %q, want %q", res.Text, "Hello world")
	}
	if len(deltas) != 2 || deltas[0] != "Hello" || deltas[1] != " world" {
		t.Errorf("deltas = %q", deltas)
	}
	if res.InputTokens != 15 {
		t.Errorf("input tokens = %d, want 15 (10 + 2 cache creation + 3 cache read)", res.InputTokens)
	}
	if res.OutputTokens != 7 {
		t.Errorf("output tokens = %d, want 7", res.OutputTokens)
	}
	if len(res.ToolCalls) != 0 {
		t.Errorf("tool calls = %v, want none", res.ToolCalls)
	}
	h := cap.header(0)
	if h.Get("x-api-key") != "sk-secret" {
		t.Errorf("x-api-key = %q", h.Get("x-api-key"))
	}
	if h.Get("anthropic-version") != "2023-06-01" {
		t.Errorf("anthropic-version = %q", h.Get("anthropic-version"))
	}
	if h.Get("Content-Type") != "application/json" {
		t.Errorf("content-type = %q", h.Get("Content-Type"))
	}
	body := cap.body(0)
	if body["stream"] != true {
		t.Errorf("stream = %v", body["stream"])
	}
	if body["max_tokens"] != float64(8192) {
		t.Errorf("max_tokens = %v, want 8192", body["max_tokens"])
	}
	sys, ok := body["system"].([]interface{})
	if !ok || len(sys) != 1 {
		t.Fatalf("system = %v, want one block", body["system"])
	}
	block := sys[0].(map[string]interface{})
	if block["type"] != "text" || block["text"] != "you are Pantau" {
		t.Errorf("system block = %v", block)
	}
	cc, ok := block["cache_control"].(map[string]interface{})
	if !ok || cc["type"] != "ephemeral" {
		t.Errorf("system cache_control = %v", block["cache_control"])
	}
}

func TestLLMAnthropicMaxTokensOverride(t *testing.T) {
	t.Parallel()
	cap := &llmCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		llmSSEWrite(w, llmAnthropicTextStream)
	}))
	defer srv.Close()
	c, _ := newLLMClient("anthropic", srv.URL, "k")
	if _, err := c.Stream(context.Background(), llmRequest{Model: "m", MaxTokens: 42}, nil); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if got := cap.body(0)["max_tokens"]; got != float64(42) {
		t.Errorf("max_tokens = %v, want 42", got)
	}
}

func TestLLMAnthropicToolCallRequestAndStream(t *testing.T) {
	t.Parallel()
	cap := &llmCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		llmSSEWrite(w, llmAnthropicToolStream("systemd_detail", "toolu_1", `{"unit":`, `"nginx.service"}`))
	}))
	defer srv.Close()

	c, _ := newLLMClient("anthropic", srv.URL, "sk-secret")
	res, err := c.Stream(context.Background(), llmRequest{
		Model:  "claude-x",
		System: "soul",
		Messages: []llmMessage{
			{Role: llmRoleUser, Text: "check nginx"},
			{Role: llmRoleAssistant, ToolCalls: []llmToolCall{{ID: "toolu_0", Name: "systemd_detail", Args: `{"unit":"a.service"}`}}},
			{Role: llmRoleTool, ToolCallID: "toolu_0", Text: "running"},
			{Role: llmRoleTool, ToolCallID: "toolu_1", Text: "failed", IsError: true},
		},
		Tools: []llmToolSpec{{
			Name:        "systemd_detail",
			Description: "unit detail",
			Schema:      map[string]interface{}{"type": "object", "properties": map[string]interface{}{"unit": map[string]interface{}{"type": "string"}}},
		}},
	}, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("tool calls = %v, want 1", res.ToolCalls)
	}
	tc := res.ToolCalls[0]
	if tc.ID != "toolu_1" || tc.Name != "systemd_detail" {
		t.Errorf("tool call = %+v", tc)
	}
	if tc.Args != `{"unit":"nginx.service"}` {
		t.Errorf("args = %q, want the reassembled JSON", tc.Args)
	}

	body := cap.body(0)
	tools, ok := body["tools"].([]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %v", body["tools"])
	}
	tool := tools[0].(map[string]interface{})
	if tool["name"] != "systemd_detail" || tool["description"] != "unit detail" {
		t.Errorf("tool = %v", tool)
	}
	if _, ok := tool["input_schema"]; !ok {
		t.Errorf("tool has no input_schema: %v", tool)
	}

	msgs, ok := body["messages"].([]interface{})
	if !ok {
		t.Fatalf("messages = %v", body["messages"])
	}
	if len(msgs) != 3 {
		t.Fatalf("messages = %d, want 3 (user, assistant, tool results)", len(msgs))
	}
	user := msgs[0].(map[string]interface{})
	if user["role"] != "user" {
		t.Errorf("first message role = %v", user["role"])
	}
	if blocks := user["content"].([]interface{}); len(blocks) != 1 || blocks[0].(map[string]interface{})["type"] != "text" {
		t.Errorf("first message content = %v", user["content"])
	}
	assistant := msgs[1].(map[string]interface{})
	if assistant["role"] != "assistant" {
		t.Errorf("second message role = %v", assistant["role"])
	}
	ab := assistant["content"].([]interface{})
	use := ab[0].(map[string]interface{})
	if use["type"] != "tool_use" || use["id"] != "toolu_0" || use["name"] != "systemd_detail" {
		t.Errorf("tool_use block = %v", use)
	}
	input, ok := use["input"].(map[string]interface{})
	if !ok || input["unit"] != "a.service" {
		t.Errorf("tool_use input = %v", use["input"])
	}
	// Both tool results sit in one user message after the assistant turn.
	results := msgs[2].(map[string]interface{})
	if results["role"] != "user" {
		t.Errorf("third message role = %v", results["role"])
	}
	blocks := results["content"].([]interface{})
	if len(blocks) != 2 {
		t.Fatalf("tool result blocks = %d, want 2 merged in one message", len(blocks))
	}
	tr1 := blocks[0].(map[string]interface{})
	tr2 := blocks[1].(map[string]interface{})
	if tr1["type"] != "tool_result" || tr1["tool_use_id"] != "toolu_0" || tr1["content"] != "running" {
		t.Errorf("tool result 1 = %v", tr1)
	}
	if tr2["type"] != "tool_result" || tr2["tool_use_id"] != "toolu_1" || tr2["content"] != "failed" {
		t.Errorf("tool result 2 = %v", tr2)
	}
	if tr2["is_error"] != true {
		t.Errorf("failed tool result has no is_error: %v", tr2)
	}
}

func TestLLMAnthropicMultipleToolCalls(t *testing.T) {
	t.Parallel()
	stream := `event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_a","name":"a"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"x\":1}"}}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_b","name":"b"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"y\":2}"}}

event: message_stop
data: {"type":"message_stop"}

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmSSEWrite(w, stream)
	}))
	defer srv.Close()
	c, _ := newLLMClient("anthropic", srv.URL, "k")
	res, err := c.Stream(context.Background(), llmRequest{Model: "m"}, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(res.ToolCalls) != 2 {
		t.Fatalf("tool calls = %v, want 2", res.ToolCalls)
	}
	if res.ToolCalls[0].ID != "toolu_a" || res.ToolCalls[0].Name != "a" || res.ToolCalls[0].Args != `{"x":1}` {
		t.Errorf("first tool call = %+v", res.ToolCalls[0])
	}
	if res.ToolCalls[1].ID != "toolu_b" || res.ToolCalls[1].Name != "b" || res.ToolCalls[1].Args != `{"y":2}` {
		t.Errorf("second tool call = %+v", res.ToolCalls[1])
	}
}

func TestLLMAnthropicErrorStatusKeepsProviderMessage(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens is too large"}}`)
	}))
	defer srv.Close()
	c, _ := newLLMClient("anthropic", srv.URL, "sk-secret")
	_, err := c.Stream(context.Background(), llmRequest{Model: "m"}, nil)
	var le *llmError
	if !errors.As(err, &le) {
		t.Fatalf("err = %v (%T), want *llmError", err, err)
	}
	if le.Status != 400 {
		t.Errorf("status = %d, want 400", le.Status)
	}
	if le.Message != "max_tokens is too large" {
		t.Errorf("message = %q, want the provider message", le.Message)
	}
	if strings.Contains(le.Error(), "sk-secret") {
		t.Errorf("error leaks the API key: %v", le)
	}
}

func TestLLMAnthropicErrorBodyIsClipped(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, strings.Repeat("x", 2000))
	}))
	defer srv.Close()
	c, _ := newLLMClient("anthropic", srv.URL, "k")
	_, err := c.Stream(context.Background(), llmRequest{Model: "m"}, nil)
	var le *llmError
	if !errors.As(err, &le) {
		t.Fatalf("err = %v, want *llmError", err)
	}
	if len(le.Message) > llmMaxErrorBody {
		t.Errorf("message length = %d, want <= %d", len(le.Message), llmMaxErrorBody)
	}
}

func TestLLMAnthropicRetriesOn429(t *testing.T) {
	llmShortRetry(t)
	cap := &llmCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		if cap.count() == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			io.WriteString(w, `{"error":{"message":"slow down"}}`)
			return
		}
		llmSSEWrite(w, llmAnthropicTextStream)
	}))
	defer srv.Close()
	c, _ := newLLMClient("anthropic", srv.URL, "k")
	res, err := c.Stream(context.Background(), llmRequest{Model: "m"}, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if res.Text != "Hello world" {
		t.Errorf("text = %q", res.Text)
	}
	if cap.count() != 2 {
		t.Errorf("calls = %d, want 2", cap.count())
	}
}

// llmAbortServer sends one text delta and then drops the connection: the stream fails after the
// answer already started, which must not be retried.
func llmAbortServer(t *testing.T, delta string) (*httptest.Server, *llmCapture) {
	t.Helper()
	cap := &llmCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\""+delta+"\"}}\n\n")
		w.(http.Flusher).Flush()
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, err := hj.Hijack()
			if err == nil {
				conn.Close()
			}
		}
	}))
	return srv, cap
}

func TestLLMAnthropicNoRetryAfterDelta(t *testing.T) {
	llmShortRetry(t)
	srv, cap := llmAbortServer(t, "partial")
	defer srv.Close()
	c, _ := newLLMClient("anthropic", srv.URL, "k")
	var deltas []string
	_, err := c.Stream(context.Background(), llmRequest{Model: "m"}, func(s string) { deltas = append(deltas, s) })
	if err == nil {
		t.Fatal("want an error when the stream breaks")
	}
	if len(deltas) != 1 || deltas[0] != "partial" {
		t.Errorf("deltas = %q, want the delivered text once", deltas)
	}
	if cap.count() != 1 {
		t.Errorf("calls = %d, want 1 (a broken stream after a delta is not retried)", cap.count())
	}
}

func TestLLMAnthropicNoRetryOn400(t *testing.T) {
	llmShortRetry(t)
	cap := &llmCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"message":"bad"}}`)
	}))
	defer srv.Close()
	c, _ := newLLMClient("anthropic", srv.URL, "k")
	if _, err := c.Stream(context.Background(), llmRequest{Model: "m"}, nil); err == nil {
		t.Fatal("want an error")
	}
	if cap.count() != 1 {
		t.Errorf("calls = %d, want 1 (400 is not retried)", cap.count())
	}
}

func TestLLMAnthropicContextCancel(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"a\"}}\n\n")
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()
	c, _ := newLLMClient("anthropic", srv.URL, "k")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()
	_, err := c.Stream(ctx, llmRequest{Model: "m"}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestLLMAnthropicStalledStream(t *testing.T) {
	llmShortIdle(t, 100*time.Millisecond)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	c, _ := newLLMClient("anthropic", srv.URL, "k")
	_, err := c.Stream(context.Background(), llmRequest{Model: "m"}, nil)
	var le *llmError
	if !errors.As(err, &le) {
		t.Fatalf("err = %v, want *llmError", err)
	}
	if le.Status != 0 || le.Message != "stream stalled" {
		t.Errorf("err = %+v, want status 0 and \"stream stalled\"", le)
	}
}

// ---------------------------------------------------------------- OpenAI-compatible

const llmOpenAITextStream = `data: {"choices":[{"delta":{"content":"Hello"}}]}

data: {"choices":[{"delta":{"content":" world"}}]}

data: {"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":4}}

data: [DONE]

`

func TestLLMOpenAITextStream(t *testing.T) {
	t.Parallel()
	cap := &llmCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		llmSSEWrite(w, llmOpenAITextStream)
	}))
	defer srv.Close()
	c, err := newLLMClient("openai", srv.URL+"/v1", "sk-openai")
	if err != nil {
		t.Fatalf("newLLMClient: %v", err)
	}
	var deltas []string
	res, err := c.Stream(context.Background(), llmRequest{
		Model:  "gpt-x",
		System: "soul",
		Messages: []llmMessage{
			{Role: llmRoleUser, Text: "hi"},
		},
	}, func(s string) { deltas = append(deltas, s) })
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if res.Text != "Hello world" {
		t.Errorf("text = %q", res.Text)
	}
	if len(deltas) != 2 {
		t.Errorf("deltas = %q", deltas)
	}
	if res.InputTokens != 11 || res.OutputTokens != 4 {
		t.Errorf("tokens = %d/%d, want 11/4", res.InputTokens, res.OutputTokens)
	}
	h := cap.header(0)
	if h.Get("Authorization") != "Bearer sk-openai" {
		t.Errorf("authorization = %q", h.Get("Authorization"))
	}
	body := cap.body(0)
	if body["stream"] != true {
		t.Errorf("stream = %v", body["stream"])
	}
	so, ok := body["stream_options"].(map[string]interface{})
	if !ok || so["include_usage"] != true {
		t.Errorf("stream_options = %v", body["stream_options"])
	}
	if _, ok := body["max_tokens"]; ok {
		t.Errorf("max_tokens sent although MaxTokens is 0: %v", body["max_tokens"])
	}
	msgs := body["messages"].([]interface{})
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want 2", len(msgs))
	}
	sys := msgs[0].(map[string]interface{})
	if sys["role"] != "system" || sys["content"] != "soul" {
		t.Errorf("first message = %v, want the system prompt", sys)
	}
	usr := msgs[1].(map[string]interface{})
	if usr["role"] != "user" || usr["content"] != "hi" {
		t.Errorf("second message = %v", usr)
	}
}

func TestLLMOpenAIEmptyKeyHasNoAuthHeader(t *testing.T) {
	t.Parallel()
	cap := &llmCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		llmSSEWrite(w, llmOpenAITextStream)
	}))
	defer srv.Close()
	c, _ := newLLMClient("openai", srv.URL+"/v1", "")
	if _, err := c.Stream(context.Background(), llmRequest{Model: "m"}, nil); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if h := cap.header(0); h.Get("Authorization") != "" {
		t.Errorf("authorization = %q, want none (Ollama needs no key)", h.Get("Authorization"))
	}
}

func TestLLMOpenAIToolCallStream(t *testing.T) {
	t.Parallel()
	stream := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"systemd_detail","arguments":""}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"unit\":"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"nginx.service\"}"}}]}}]}

data: [DONE]

`
	cap := &llmCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		llmSSEWrite(w, stream)
	}))
	defer srv.Close()
	c, _ := newLLMClient("openai", srv.URL+"/v1", "sk-openai")
	res, err := c.Stream(context.Background(), llmRequest{
		Model: "m",
		Messages: []llmMessage{
			{Role: llmRoleUser, Text: "check"},
			{Role: llmRoleAssistant, ToolCalls: []llmToolCall{{ID: "call_0", Name: "systemd_detail", Args: `{"unit":"x"}`}}},
			{Role: llmRoleTool, ToolCallID: "call_0", Text: "running"},
		},
		Tools: []llmToolSpec{{Name: "systemd_detail", Description: "d", Schema: map[string]interface{}{"type": "object"}}},
	}, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("tool calls = %v, want 1", res.ToolCalls)
	}
	tc := res.ToolCalls[0]
	if tc.ID != "call_abc" || tc.Name != "systemd_detail" {
		t.Errorf("tool call = %+v", tc)
	}
	if tc.Args != `{"unit":"nginx.service"}` {
		t.Errorf("args = %q", tc.Args)
	}
	body := cap.body(0)
	tools := body["tools"].([]interface{})
	tool := tools[0].(map[string]interface{})
	if tool["type"] != "function" {
		t.Errorf("tool type = %v", tool["type"])
	}
	fn := tool["function"].(map[string]interface{})
	if fn["name"] != "systemd_detail" || fn["description"] != "d" {
		t.Errorf("function = %v", fn)
	}
	if _, ok := fn["parameters"]; !ok {
		t.Errorf("function has no parameters: %v", fn)
	}
	msgs := body["messages"].([]interface{})
	if len(msgs) != 3 {
		t.Fatalf("messages = %d, want 3", len(msgs))
	}
	assistant := msgs[1].(map[string]interface{})
	if assistant["role"] != "assistant" {
		t.Errorf("assistant role = %v", assistant["role"])
	}
	calls := assistant["tool_calls"].([]interface{})
	if len(calls) != 1 {
		t.Fatalf("tool_calls = %v", calls)
	}
	call := calls[0].(map[string]interface{})
	if call["id"] != "call_0" || call["type"] != "function" {
		t.Errorf("tool call = %v", call)
	}
	cfn := call["function"].(map[string]interface{})
	if cfn["name"] != "systemd_detail" || cfn["arguments"] != `{"unit":"x"}` {
		t.Errorf("call function = %v", cfn)
	}
	toolMsg := msgs[2].(map[string]interface{})
	if toolMsg["role"] != "tool" || toolMsg["tool_call_id"] != "call_0" || toolMsg["content"] != "running" {
		t.Errorf("tool message = %v", toolMsg)
	}
}

func TestLLMOpenAIToolCallWithoutID(t *testing.T) {
	t.Parallel()
	stream := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"noop","arguments":"{}"}}]}}]}

data: [DONE]

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmSSEWrite(w, stream)
	}))
	defer srv.Close()
	c, _ := newLLMClient("openai", srv.URL+"/v1", "k")
	res, err := c.Stream(context.Background(), llmRequest{Model: "m"}, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("tool calls = %v, want 1", res.ToolCalls)
	}
	if res.ToolCalls[0].ID != "call_0" {
		t.Errorf("id = %q, want the synthesised call_0", res.ToolCalls[0].ID)
	}
}

func TestLLMOpenAIMultipleToolCalls(t *testing.T) {
	t.Parallel()
	stream := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"name":"a","arguments":"{\"x\":1}"}},{"index":1,"id":"call_b","function":{"name":"b","arguments":"{\"y\":2}"}}]}}]}

data: [DONE]

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmSSEWrite(w, stream)
	}))
	defer srv.Close()
	c, _ := newLLMClient("openai", srv.URL+"/v1", "k")
	res, err := c.Stream(context.Background(), llmRequest{Model: "m"}, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(res.ToolCalls) != 2 {
		t.Fatalf("tool calls = %v, want 2", res.ToolCalls)
	}
	if res.ToolCalls[0].ID != "call_a" || res.ToolCalls[0].Args != `{"x":1}` {
		t.Errorf("first = %+v", res.ToolCalls[0])
	}
	if res.ToolCalls[1].ID != "call_b" || res.ToolCalls[1].Args != `{"y":2}` {
		t.Errorf("second = %+v", res.ToolCalls[1])
	}
}

func TestLLMOpenAIErrorStatus(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":{"message":"invalid api key","type":"auth"}}`)
	}))
	defer srv.Close()
	c, _ := newLLMClient("openai", srv.URL+"/v1", "sk-secret")
	_, err := c.Stream(context.Background(), llmRequest{Model: "m"}, nil)
	var le *llmError
	if !errors.As(err, &le) {
		t.Fatalf("err = %v, want *llmError", err)
	}
	if le.Status != 401 || le.Message != "invalid api key" {
		t.Errorf("err = %+v", le)
	}
	if strings.Contains(le.Error(), "sk-secret") {
		t.Errorf("error leaks the API key: %v", le)
	}
}

func TestLLMOpenAIErrorObjectMidStream(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmSSEWrite(w, "data: {\"error\":{\"message\":\"model overloaded\"}}\n\n")
	}))
	defer srv.Close()
	c, _ := newLLMClient("openai", srv.URL+"/v1", "k")
	_, err := c.Stream(context.Background(), llmRequest{Model: "m"}, nil)
	var le *llmError
	if !errors.As(err, &le) {
		t.Fatalf("err = %v, want *llmError", err)
	}
	if le.Message != "model overloaded" {
		t.Errorf("message = %q", le.Message)
	}
}

func TestLLMOpenAIRetriesOn429(t *testing.T) {
	llmShortRetry(t)
	cap := &llmCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		if cap.count() == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			io.WriteString(w, `{"error":{"message":"slow down"}}`)
			return
		}
		llmSSEWrite(w, llmOpenAITextStream)
	}))
	defer srv.Close()
	c, _ := newLLMClient("openai", srv.URL+"/v1", "k")
	res, err := c.Stream(context.Background(), llmRequest{Model: "m"}, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if res.Text != "Hello world" {
		t.Errorf("text = %q", res.Text)
	}
	if cap.count() != 2 {
		t.Errorf("calls = %d, want 2", cap.count())
	}
}

func TestLLMOpenAINoRetryOn400(t *testing.T) {
	llmShortRetry(t)
	cap := &llmCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"message":"bad"}}`)
	}))
	defer srv.Close()
	c, _ := newLLMClient("openai", srv.URL+"/v1", "k")
	if _, err := c.Stream(context.Background(), llmRequest{Model: "m"}, nil); err == nil {
		t.Fatal("want an error")
	}
	if cap.count() != 1 {
		t.Errorf("calls = %d, want 1", cap.count())
	}
}

func TestLLMOpenAIContextCancel(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n")
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()
	c, _ := newLLMClient("openai", srv.URL+"/v1", "k")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()
	_, err := c.Stream(ctx, llmRequest{Model: "m"}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestLLMOpenAIStalledStream(t *testing.T) {
	llmShortIdle(t, 100*time.Millisecond)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	c, _ := newLLMClient("openai", srv.URL+"/v1", "k")
	_, err := c.Stream(context.Background(), llmRequest{Model: "m"}, nil)
	var le *llmError
	if !errors.As(err, &le) {
		t.Fatalf("err = %v, want *llmError", err)
	}
	if le.Status != 0 || le.Message != "stream stalled" {
		t.Errorf("err = %+v", le)
	}
}

// ---------------------------------------------------------------- base URLs and provider

func TestLLMBaseURLNormalisation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		provider string
		base     string
		want     string
	}{
		{"anthropic", "https://api.anthropic.com", "https://api.anthropic.com/v1/messages"},
		{"anthropic", "https://api.anthropic.com/v1", "https://api.anthropic.com/v1/messages"},
		{"anthropic", "https://proxy.example.com/v1/", "https://proxy.example.com/v1/messages"},
		{"anthropic", "https://proxy.example.com", "https://proxy.example.com/v1/messages"},
		{"openai", "https://api.openai.com/v1", "https://api.openai.com/v1/chat/completions"},
		{"openai", "https://api.openai.com/v1/", "https://api.openai.com/v1/chat/completions"},
		{"openai", "http://127.0.0.1:11434/v1", "http://127.0.0.1:11434/v1/chat/completions"},
		{"openai", "http://127.0.0.1:11434", "http://127.0.0.1:11434/chat/completions"},
	}
	for _, tc := range cases {
		c, err := newLLMClient(tc.provider, tc.base, "k")
		if err != nil {
			t.Fatalf("newLLMClient(%s, %s): %v", tc.provider, tc.base, err)
		}
		be, ok := c.(llmBackend)
		if !ok {
			t.Fatalf("client %T does not implement llmBackend", c)
		}
		if got := be.endpoint(); got != tc.want {
			t.Errorf("%s %q endpoint = %q, want %q", tc.provider, tc.base, got, tc.want)
		}
	}
}

func TestLLMDefaultBaseURLs(t *testing.T) {
	t.Parallel()
	a, _ := newLLMClient("anthropic", "", "k")
	if got := a.(llmBackend).endpoint(); got != "https://api.anthropic.com/v1/messages" {
		t.Errorf("anthropic default endpoint = %q", got)
	}
	o, _ := newLLMClient("openai", "", "k")
	if got := o.(llmBackend).endpoint(); got != "https://api.openai.com/v1/chat/completions" {
		t.Errorf("openai default endpoint = %q", got)
	}
}

func TestLLMUnknownProvider(t *testing.T) {
	t.Parallel()
	if _, err := newLLMClient("gemini", "https://x", "k"); err == nil {
		t.Fatal("want an error for an unknown provider")
	}
	if _, err := newLLMClient("", "", "k"); err == nil {
		t.Fatal("want an error for an empty provider")
	}
}

func TestLLMOpenAINoRetryAfterDelta(t *testing.T) {
	llmShortRetry(t)
	cap := &llmCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
		w.(http.Flusher).Flush()
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, err := hj.Hijack()
			if err == nil {
				conn.Close()
			}
		}
	}))
	defer srv.Close()
	c, _ := newLLMClient("openai", srv.URL+"/v1", "k")
	var deltas []string
	_, err := c.Stream(context.Background(), llmRequest{Model: "m"}, func(s string) { deltas = append(deltas, s) })
	if err == nil {
		t.Fatal("want an error when the stream breaks")
	}
	if len(deltas) != 1 || deltas[0] != "partial" {
		t.Errorf("deltas = %q, want the delivered text once", deltas)
	}
	if cap.count() != 1 {
		t.Errorf("calls = %d, want 1 (a broken stream after a delta is not retried)", cap.count())
	}
}

// An answer without text and without tool calls is not an error of this layer.
func TestLLMEmptyResponseIsNoError(t *testing.T) {
	t.Parallel()
	asrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmSSEWrite(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1}}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer asrv.Close()
	ac, _ := newLLMClient("anthropic", asrv.URL, "k")
	res, err := ac.Stream(context.Background(), llmRequest{Model: "m"}, nil)
	if err != nil {
		t.Fatalf("anthropic Stream: %v", err)
	}
	if res.Text != "" || len(res.ToolCalls) != 0 {
		t.Errorf("anthropic result = %+v, want empty", res)
	}

	osrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmSSEWrite(w, "data: {\"choices\":[]}\n\ndata: [DONE]\n\n")
	}))
	defer osrv.Close()
	oc, _ := newLLMClient("openai", osrv.URL+"/v1", "k")
	res, err = oc.Stream(context.Background(), llmRequest{Model: "m"}, nil)
	if err != nil {
		t.Fatalf("openai Stream: %v", err)
	}
	if res.Text != "" || len(res.ToolCalls) != 0 {
		t.Errorf("openai result = %+v, want empty", res)
	}
}

// Many gateways close the stream after a chunk with a finish_reason and never send [DONE].
func TestLLMOpenAIFinishReasonWithoutDone(t *testing.T) {
	t.Parallel()
	stream := `data: {"choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"choices":[{"delta":{"content":" world"},"finish_reason":"stop"}]}

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmSSEWrite(w, stream)
	}))
	defer srv.Close()
	c, _ := newLLMClient("openai", srv.URL+"/v1", "k")
	var deltas []string
	res, err := c.Stream(context.Background(), llmRequest{Model: "m"}, func(s string) { deltas = append(deltas, s) })
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if res.Text != "Hello world" {
		t.Errorf("text = %q, want %q", res.Text, "Hello world")
	}
	if len(deltas) != 2 {
		t.Errorf("deltas = %q", deltas)
	}
}

func TestLLMOpenAIFinishReasonWithUsageAfterIt(t *testing.T) {
	t.Parallel()
	stream := `data: {"choices":[{"delta":{"content":"Hi"},"finish_reason":"stop"}]}

data: {"choices":[],"usage":{"prompt_tokens":9,"completion_tokens":3}}

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmSSEWrite(w, stream)
	}))
	defer srv.Close()
	c, _ := newLLMClient("openai", srv.URL+"/v1", "k")
	res, err := c.Stream(context.Background(), llmRequest{Model: "m"}, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if res.Text != "Hi" {
		t.Errorf("text = %q", res.Text)
	}
	if res.InputTokens != 9 || res.OutputTokens != 3 {
		t.Errorf("tokens = %d/%d, want 9/3 (the usage chunk arrives after the finish_reason)", res.InputTokens, res.OutputTokens)
	}
}

func TestLLMOpenAIFinishReasonWithToolCall(t *testing.T) {
	t.Parallel()
	stream := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_x","function":{"name":"noop","arguments":"{}"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmSSEWrite(w, stream)
	}))
	defer srv.Close()
	c, _ := newLLMClient("openai", srv.URL+"/v1", "k")
	res, err := c.Stream(context.Background(), llmRequest{Model: "m"}, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("tool calls = %v, want 1", res.ToolCalls)
	}
	if res.ToolCalls[0].ID != "call_x" || res.ToolCalls[0].Name != "noop" {
		t.Errorf("tool call = %+v", res.ToolCalls[0])
	}
}

// A stream that ends in the middle of the text without a finish_reason is still truncated.
func TestLLMOpenAITruncatedWithoutFinishReason(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmSSEWrite(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"},\"finish_reason\":null}]}\n\n")
	}))
	defer srv.Close()
	c, _ := newLLMClient("openai", srv.URL+"/v1", "k")
	_, err := c.Stream(context.Background(), llmRequest{Model: "m"}, nil)
	if err == nil {
		t.Fatal("want an error for a truncated stream")
	}
	var le *llmError
	if !errors.As(err, &le) {
		t.Fatalf("err = %v, want *llmError", err)
	}
	if le.Status != 0 {
		t.Errorf("status = %d, want 0", le.Status)
	}
}
