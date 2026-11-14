package web

// Native LLM providers of the in-process AI agent: Anthropic and any OpenAI-compatible endpoint
// (OpenAI, OpenRouter, LiteLLM, vLLM, Ollama, gateways). agent_loop.go drives the tool-call loop.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	llmProviderAnthropic = "anthropic"
	llmProviderOpenAI    = "openai"

	llmAnthropicVersion     = "2023-06-01"
	llmAnthropicDefaultBase = "https://api.anthropic.com"
	llmOpenAIDefaultBase    = "https://api.openai.com/v1"
	llmAnthropicMaxTokens   = 8192
	llmMaxErrorBody         = 500
)

var (
	// llmRetryBase is the first backoff between retries; tests shorten it.
	llmRetryBase = time.Second
	// llmIdleTimeout aborts a stream that stops delivering bytes.
	llmIdleTimeout = 90 * time.Second
)

// errLLMStalled marks a stream that delivered nothing for llmIdleTimeout.
var errLLMStalled = errors.New("stream stalled")

// errLLMMalformed marks a provider frame that is not valid JSON.
var errLLMMalformed = errors.New("malformed provider stream")

// errLLMTruncated marks a stream that ended in the middle of the answer.
var errLLMTruncated = errors.New("provider stream ended early")

// newLLMClient builds the client of one provider. An empty baseURL means the provider default.
func newLLMClient(provider, baseURL, apiKey string) (llmClient, error) {
	switch provider {
	case llmProviderAnthropic:
		return &anthropicLLM{base: anthropicBase(baseURL), key: apiKey}, nil
	case llmProviderOpenAI:
		return &openaiLLM{base: openaiBase(baseURL), key: apiKey}, nil
	}
	return nil, fmt.Errorf("unknown LLM provider %q", provider)
}

// anthropicBase returns the endpoint root including the version, without doubling /v1.
func anthropicBase(raw string) string {
	b := strings.TrimRight(strings.TrimSpace(raw), "/")
	if b == "" {
		b = llmAnthropicDefaultBase
	}
	if !strings.HasSuffix(b, "/v1") {
		b += "/v1"
	}
	return b
}

// openaiBase returns the endpoint root as given; the default already carries /v1.
func openaiBase(raw string) string {
	b := strings.TrimRight(strings.TrimSpace(raw), "/")
	if b == "" {
		b = llmOpenAIDefaultBase
	}
	return b
}

// llmBackend is what the two providers share: where to POST, which headers, the request body and the
// parsing of the answer stream.
type llmBackend interface {
	endpoint() string
	headers(h *http.Request)
	body(req llmRequest) ([]byte, error)
	parse(body io.Reader, onText func(string)) (llmResult, error)
}

// llmStream runs one model call and retries what is safe to retry: 429, 5xx and network failures, but
// only while no text delta was delivered to onText.
func llmStream(ctx context.Context, be llmBackend, req llmRequest, onText func(string)) (llmResult, error) {
	for attempt := 0; ; attempt++ {
		out := llmAttempt(ctx, be, req, onText)
		if out.err == nil {
			return out.res, nil
		}
		var le *llmError
		if !errors.As(out.err, &le) {
			return out.res, out.err
		}
		retryable := out.network || le.Status == http.StatusTooManyRequests || le.Status >= 500
		if attempt >= 2 || !retryable || out.delivered {
			return out.res, out.err
		}
		delay := llmBackoff(attempt, out.retryAfter)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return out.res, ctx.Err()
		case <-timer.C:
		}
	}
}

// llmBackoff is 1x, then 2x llmRetryBase; a Retry-After header wins but never exceeds 10s.
func llmBackoff(attempt int, retryAfter time.Duration) time.Duration {
	d := llmRetryBase << attempt
	if retryAfter > d {
		d = retryAfter
	}
	if d > 10*time.Second {
		d = 10 * time.Second
	}
	return d
}

// llmOutcome is the result of one attempt against the provider.
type llmOutcome struct {
	res        llmResult
	err        error
	retryAfter time.Duration
	network    bool
	delivered  bool // a text delta already reached onText
}

// llmAttempt performs one HTTP call and reads the answer stream.
func llmAttempt(ctx context.Context, be llmBackend, req llmRequest, onText func(string)) llmOutcome {
	var out llmOutcome
	body, err := be.body(req)
	if err != nil {
		out.err = err
		return out
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, be.endpoint(), bytes.NewReader(body))
	if err != nil {
		out.err = &llmError{Status: 0, Message: "cannot build request: " + err.Error()}
		return out
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Accept", "text/event-stream")
	be.headers(hreq)
	resp, err := agentHTTP.Do(hreq)
	if err != nil {
		if ctx.Err() != nil {
			out.err = ctx.Err()
			return out
		}
		out.err = &llmError{Status: 0, Message: err.Error()}
		out.network = true
		return out
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		out.err = &llmError{Status: resp.StatusCode, Message: llmErrorMessage(raw)}
		out.retryAfter = llmRetryAfter(resp.Header.Get("Retry-After"))
		return out
	}
	delivered := false
	cb := func(s string) {
		delivered = true
		if onText != nil {
			onText(s)
		}
	}
	done := make(chan struct{})
	defer close(done)
	res, perr := be.parse(newLLMStreamReader(ctx, resp.Body, llmIdleTimeout, done), cb)
	out.res = res
	out.delivered = delivered
	if perr != nil {
		var le *llmError
		switch {
		case errors.As(perr, &le):
			out.err = le
		case errors.Is(perr, errLLMStalled):
			out.err = &llmError{Status: 0, Message: "stream stalled"}
		case ctx.Err() != nil:
			out.err = ctx.Err()
		case errors.Is(perr, errLLMMalformed):
			out.err = &llmError{Status: 0, Message: perr.Error()}
		default:
			out.err = &llmError{Status: 0, Message: "provider stream failed: " + perr.Error()}
			out.network = true
		}
	}
	return out
}

// llmStreamReader turns the response body into a reader that fails when the provider stops sending bytes.
type llmStreamReader struct {
	ch      chan []byte
	cur     []byte
	timeout time.Duration
	ctx     context.Context
	done    chan struct{}
}

func newLLMStreamReader(ctx context.Context, body io.Reader, timeout time.Duration, done chan struct{}) *llmStreamReader {
	r := &llmStreamReader{ch: make(chan []byte, 4), timeout: timeout, ctx: ctx, done: done}
	go func() {
		defer close(r.ch)
		buf := make([]byte, 32*1024)
		for {
			n, err := body.Read(buf)
			if n > 0 {
				c := make([]byte, n)
				copy(c, buf[:n])
				select {
				case r.ch <- c:
				case <-ctx.Done():
					return
				case <-done:
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	return r
}

func (r *llmStreamReader) Read(p []byte) (int, error) {
	if len(r.cur) > 0 {
		n := copy(p, r.cur)
		r.cur = r.cur[n:]
		return n, nil
	}
	timer := time.NewTimer(r.timeout)
	defer timer.Stop()
	select {
	case c, ok := <-r.ch:
		if !ok {
			return 0, io.EOF
		}
		n := copy(p, c)
		r.cur = c[n:]
		return n, nil
	case <-timer.C:
		return 0, errLLMStalled
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	}
}

// llmErrorMessage takes the provider's own error text out of its JSON error body, else the raw body.
func llmErrorMessage(raw []byte) string {
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err == nil {
		if msg := llmErrorText(payload["error"]); msg != "" {
			return llmClip(msg)
		}
		if msg, ok := payload["message"].(string); ok && msg != "" {
			return llmClip(msg)
		}
	}
	return llmClip(strings.TrimSpace(string(raw)))
}

// llmErrorText reads the message of a provider error, which is an object or a plain string.
func llmErrorText(v interface{}) string {
	switch e := v.(type) {
	case string:
		return e
	case map[string]interface{}:
		if msg, ok := e["message"].(string); ok {
			return msg
		}
	}
	return ""
}

// llmClip keeps an error message at most llmMaxErrorBody characters.
func llmClip(s string) string {
	if len(s) <= llmMaxErrorBody {
		return s
	}
	// The ellipsis counts towards the limit, so the message stays under llmMaxErrorBody bytes.
	return s[:llmMaxErrorBody-3] + "…"
}

// llmRetryAfter reads a numeric Retry-After header (seconds).
func llmRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

// llmArgsObject parses tool arguments into the object the providers expect; anything unusable becomes {}.
func llmArgsObject(raw string) map[string]interface{} {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]interface{}{}
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &obj); err != nil || obj == nil {
		return map[string]interface{}{}
	}
	return obj
}

// llmSSE reads an SSE stream and calls fn for every event with its name and data payload.
func llmSSE(r io.Reader, fn func(event, data string) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	event, data := "", ""
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" {
			if data != "" {
				if err := fn(event, data); err != nil {
					return err
				}
			}
			event, data = "", ""
			continue
		}
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(line[len("event:"):])
		case strings.HasPrefix(line, "data:"):
			part := strings.TrimPrefix(line[len("data:"):], " ")
			if data != "" {
				data += "\n"
			}
			data += part
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if data != "" {
		return fn(event, data)
	}
	return nil
}

// ---------------------------------------------------------------- Anthropic

type anthropicLLM struct {
	base string
	key  string
}

func (c *anthropicLLM) endpoint() string { return c.base + "/messages" }

func (c *anthropicLLM) headers(h *http.Request) {
	if c.key != "" {
		h.Header.Set("x-api-key", c.key)
	}
	h.Header.Set("anthropic-version", llmAnthropicVersion)
}

// llmBlock is one Anthropic content block.
type llmBlock map[string]interface{}

func (c *anthropicLLM) body(req llmRequest) ([]byte, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = llmAnthropicMaxTokens
	}
	out := map[string]interface{}{
		"model":      req.Model,
		"max_tokens": maxTokens,
		"stream":     true,
	}
	if req.System != "" {
		// The stable prefix is cached: the SOUL and the tool schemas change rarely.
		out["system"] = []llmBlock{{
			"type":          "text",
			"text":          req.System,
			"cache_control": map[string]string{"type": "ephemeral"},
		}}
	}
	out["messages"] = anthropicMessages(req.Messages)
	if len(req.Tools) > 0 {
		tools := make([]map[string]interface{}, 0, len(req.Tools))
		for _, t := range req.Tools {
			schema := t.Schema
			if schema == nil {
				schema = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
			}
			tools = append(tools, map[string]interface{}{
				"name":         t.Name,
				"description":  t.Description,
				"input_schema": schema,
			})
		}
		out["tools"] = tools
	}
	return json.Marshal(out)
}

// anthropicMessages converts the neutral messages. Anthropic refuses two user messages in a row and
// wants the tool_result blocks at the start of the user turn that follows the tool use, so all
// consecutive user content is merged into one message.
func anthropicMessages(msgs []llmMessage) []map[string]interface{} {
	type userTurn struct {
		results []llmBlock
		texts   []llmBlock
	}
	out := []map[string]interface{}{}
	var pending *userTurn
	flush := func() {
		if pending == nil {
			return
		}
		blocks := append([]llmBlock{}, pending.results...)
		blocks = append(blocks, pending.texts...)
		out = append(out, map[string]interface{}{"role": "user", "content": blocks})
		pending = nil
	}
	for _, m := range msgs {
		switch m.Role {
		case llmRoleAssistant:
			flush()
			blocks := []llmBlock{}
			if m.Text != "" {
				blocks = append(blocks, llmBlock{"type": "text", "text": m.Text})
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, llmBlock{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Name,
					"input": llmArgsObject(tc.Args),
				})
			}
			if len(blocks) == 0 {
				continue
			}
			out = append(out, map[string]interface{}{"role": "assistant", "content": blocks})
		case llmRoleTool:
			if pending == nil {
				pending = &userTurn{}
			}
			block := llmBlock{
				"type":        "tool_result",
				"tool_use_id": m.ToolCallID,
				"content":     m.Text,
			}
			if m.IsError {
				block["is_error"] = true
			}
			pending.results = append(pending.results, block)
		default:
			if pending == nil {
				pending = &userTurn{}
			}
			pending.texts = append(pending.texts, llmBlock{"type": "text", "text": m.Text})
		}
	}
	flush()
	return out
}

// llmAnthropicState collects the state of one Anthropic stream.
type llmAnthropicState struct {
	blocks map[int]*llmBlockState
	order  []int
	result llmResult
}

type llmBlockState struct {
	id    string
	name  string
	kind  string // "text" or "tool_use"
	parts string
}

func (c *anthropicLLM) parse(r io.Reader, onText func(string)) (llmResult, error) {
	st := &llmAnthropicState{blocks: map[int]*llmBlockState{}}
	ended := false
	err := llmSSE(r, func(event, data string) error {
		if data == "[DONE]" {
			return nil
		}
		var ev map[string]interface{}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return fmt.Errorf("%w: %v", errLLMMalformed, err)
		}
		typ, _ := ev["type"].(string)
		if typ == "" {
			typ = event
		}
		switch typ {
		case "message_start":
			if msg, ok := ev["message"].(map[string]interface{}); ok {
				if usage, ok := msg["usage"].(map[string]interface{}); ok {
					st.result.InputTokens = llmTokenSum(usage)
				}
			}
		case "content_block_start":
			idx := llmInt(ev["index"])
			bs := &llmBlockState{}
			if cb, ok := ev["content_block"].(map[string]interface{}); ok {
				bs.kind, _ = cb["type"].(string)
				bs.id, _ = cb["id"].(string)
				bs.name, _ = cb["name"].(string)
			}
			if _, dup := st.blocks[idx]; !dup {
				st.order = append(st.order, idx)
			}
			st.blocks[idx] = bs
		case "content_block_delta":
			idx := llmInt(ev["index"])
			bs := st.blocks[idx]
			if bs == nil {
				bs = &llmBlockState{kind: "text"}
				st.blocks[idx] = bs
				st.order = append(st.order, idx)
			}
			delta, _ := ev["delta"].(map[string]interface{})
			if delta == nil {
				return nil
			}
			dtype, _ := delta["type"].(string)
			switch dtype {
			case "text_delta":
				text, _ := delta["text"].(string)
				if text != "" {
					st.result.Text += text
					if onText != nil {
						onText(text)
					}
				}
			case "input_json_delta":
				part, _ := delta["partial_json"].(string)
				bs.parts += part
			}
		case "message_delta":
			if usage, ok := ev["usage"].(map[string]interface{}); ok {
				if n, ok := usage["output_tokens"].(float64); ok {
					st.result.OutputTokens = int(n)
				}
			}
		case "error":
			msg := ""
			if e, ok := ev["error"].(map[string]interface{}); ok {
				msg, _ = e["message"].(string)
			}
			if msg == "" {
				msg = "provider error"
			}
			return &llmError{Status: 0, Message: msg}
		case "message_stop":
			ended = true
		case "ping", "content_block_stop":
			// nothing to do
		}
		return nil
	})
	if err != nil {
		return st.result, err
	}
	if !ended {
		// The provider closed the stream in the middle of the answer.
		return st.result, fmt.Errorf("%w: stream ended without message_stop", errLLMTruncated)
	}
	for _, idx := range st.order {
		bs := st.blocks[idx]
		if bs == nil || bs.kind != "tool_use" {
			continue
		}
		st.result.ToolCalls = append(st.result.ToolCalls, llmToolCall{
			ID:   bs.id,
			Name: bs.name,
			Args: llmToolArgs(bs.parts),
		})
	}
	return st.result, nil
}

// llmToolArgs turns the accumulated tool arguments into a JSON object; empty becomes "{}".
func llmToolArgs(parts string) string {
	trimmed := strings.TrimSpace(parts)
	if trimmed == "" {
		return "{}"
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
		// Invalid JSON is passed through raw; the loop reports it.
		return trimmed
	}
	if obj == nil {
		return "{}"
	}
	return trimmed
}

// llmTokenSum sums the prompt tokens of an Anthropic usage block, cache writes and reads included.
func llmTokenSum(usage map[string]interface{}) int {
	total := 0
	for _, key := range []string{"input_tokens", "cache_creation_input_tokens", "cache_read_input_tokens"} {
		if n, ok := usage[key].(float64); ok {
			total += int(n)
		}
	}
	return total
}

// llmInt reads a JSON number as int.
func llmInt(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}

// ---------------------------------------------------------------- OpenAI-compatible

type openaiLLM struct {
	base string
	key  string
}

func (c *openaiLLM) endpoint() string { return c.base + "/chat/completions" }

func (c *openaiLLM) headers(h *http.Request) {
	// Ollama and other local gateways need no key.
	if c.key != "" {
		h.Header.Set("Authorization", "Bearer "+c.key)
	}
}

func (c *openaiLLM) body(req llmRequest) ([]byte, error) {
	out := map[string]interface{}{
		"model":          req.Model,
		"stream":         true,
		"stream_options": map[string]interface{}{"include_usage": true},
	}
	if req.MaxTokens > 0 {
		out["max_tokens"] = req.MaxTokens
	}
	msgs := []map[string]interface{}{}
	if req.System != "" {
		msgs = append(msgs, map[string]interface{}{"role": "system", "content": req.System})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case llmRoleAssistant:
			msg := map[string]interface{}{"role": "assistant", "content": m.Text}
			if len(m.ToolCalls) > 0 {
				calls := make([]map[string]interface{}, 0, len(m.ToolCalls))
				for _, tc := range m.ToolCalls {
					calls = append(calls, map[string]interface{}{
						"id":   tc.ID,
						"type": "function",
						"function": map[string]interface{}{
							"name":      tc.Name,
							"arguments": llmRawArgs(tc.Args),
						},
					})
				}
				msg["tool_calls"] = calls
			}
			msgs = append(msgs, msg)
		case llmRoleTool:
			msgs = append(msgs, map[string]interface{}{
				"role":         "tool",
				"tool_call_id": m.ToolCallID,
				"content":      m.Text,
			})
		default:
			msgs = append(msgs, map[string]interface{}{"role": "user", "content": m.Text})
		}
	}
	out["messages"] = msgs
	if len(req.Tools) > 0 {
		tools := make([]map[string]interface{}, 0, len(req.Tools))
		for _, t := range req.Tools {
			params := t.Schema
			if params == nil {
				params = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
			}
			tools = append(tools, map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  params,
				},
			})
		}
		out["tools"] = tools
	}
	return json.Marshal(out)
}

// llmRawArgs keeps the arguments as they were; empty becomes "{}".
func llmRawArgs(args string) string {
	if strings.TrimSpace(args) == "" {
		return "{}"
	}
	return args
}

// llmOpenAICall is the state of one streamed tool call.
type llmOpenAICall struct {
	id   string
	name string
	args string
}

func (c *openaiLLM) parse(r io.Reader, onText func(string)) (llmResult, error) {
	var res llmResult
	calls := map[int]*llmOpenAICall{}
	ended := false
	finished := false
	err := llmSSE(r, func(event, data string) error {
		data = strings.TrimSpace(data)
		if data == "" {
			return nil
		}
		if data == "[DONE]" {
			ended = true
			return nil
		}
		var ev map[string]interface{}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return fmt.Errorf("%w: %v", errLLMMalformed, err)
		}
		if e, ok := ev["error"]; ok && e != nil {
			msg := llmErrorText(e)
			if msg == "" {
				msg = "provider error"
			}
			return &llmError{Status: 0, Message: msg}
		}
		if usage, ok := ev["usage"].(map[string]interface{}); ok && usage != nil {
			if n, ok := usage["prompt_tokens"].(float64); ok {
				res.InputTokens = int(n)
			}
			if n, ok := usage["completion_tokens"].(float64); ok {
				res.OutputTokens = int(n)
			}
		}
		choices, _ := ev["choices"].([]interface{})
		for _, ci := range choices {
			choice, _ := ci.(map[string]interface{})
			if choice == nil {
				continue
			}
			// Many gateways close the stream after a chunk with a finish_reason and never send [DONE].
			if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
				finished = true
			}
			delta, _ := choice["delta"].(map[string]interface{})
			if delta == nil {
				continue
			}
			if text, ok := delta["content"].(string); ok && text != "" {
				res.Text += text
				if onText != nil {
					onText(text)
				}
			}
			fragments, _ := delta["tool_calls"].([]interface{})
			for _, fi := range fragments {
				frag, _ := fi.(map[string]interface{})
				if frag == nil {
					continue
				}
				idx := llmInt(frag["index"])
				call, ok := calls[idx]
				if !ok {
					call = &llmOpenAICall{}
					calls[idx] = call
				}
				if id, ok := frag["id"].(string); ok && id != "" && call.id == "" {
					call.id = id
				}
				if fn, ok := frag["function"].(map[string]interface{}); ok {
					if name, ok := fn["name"].(string); ok && name != "" && call.name == "" {
						call.name = name
					}
					if args, ok := fn["arguments"].(string); ok {
						call.args += args
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return res, err
	}
	if !ended && !finished {
		return res, fmt.Errorf("%w: stream ended without [DONE] or finish_reason", errLLMTruncated)
	}
	idxs := make([]int, 0, len(calls))
	for idx := range calls {
		idxs = append(idxs, idx)
	}
	sort.Ints(idxs)
	for _, idx := range idxs {
		call := calls[idx]
		id := call.id
		if id == "" {
			// Some servers never send an id.
			id = "call_" + strconv.Itoa(idx)
		}
		res.ToolCalls = append(res.ToolCalls, llmToolCall{
			ID:   id,
			Name: call.name,
			Args: llmToolArgs(call.args),
		})
	}
	return res, nil
}

// Stream runs one Anthropic model call.
func (c *anthropicLLM) Stream(ctx context.Context, req llmRequest, onText func(string)) (llmResult, error) {
	return llmStream(ctx, c, req, onText)
}

// Stream runs one OpenAI-compatible model call.
func (c *openaiLLM) Stream(ctx context.Context, req llmRequest, onText func(string)) (llmResult, error) {
	return llmStream(ctx, c, req, onText)
}
