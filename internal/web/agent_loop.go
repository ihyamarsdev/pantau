package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"pantau/internal/logger"
)

// The in-process AI agent: Pantau calls the LLM provider itself, runs the tool-call loop and keeps the
// conversation. The operator configures the provider in Settings -> AI Agent.

const (
	agentMaxSteps      = 25
	agentCompactTokens = 60000
	agentMaxConvs      = 20
	agentConvTTL       = 2 * time.Hour
	agentToolResultMax = 24 << 10 // one tool result in the history
	agentStaleKeep     = 4        // newest tool results kept whole; older ones are trimmed before a model call
	agentStaleBytes    = 2 << 10  // bytes of an older tool result that stay in the history
	agentLabelMax      = 80
)

// agentConv is the conversation of one chat session. History lives in memory only.
type agentConv struct {
	mu         sync.Mutex
	msgs       []llmMessage
	lastUse    time.Time
	running    bool
	lastTokens int        // the prompt size of the last model call, 0 when unknown
	domain     toolDomain // active tool domain, set from the UI page and mutable via set_tool_domain
}

type agentToolEvent struct {
	Tool       string `json:"tool"`
	Label      string `json:"label"`
	Status     string `json:"status"`
	ToolCallID string `json:"toolCallId"`
	Domain     string `json:"domain,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
}

// agentSSE writes the browser stream: text deltas, tool progress and errors.
type agentSSE struct {
	w  http.ResponseWriter
	rc *http.ResponseController
}

func newAgentSSE(w http.ResponseWriter) *agentSSE {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	return &agentSSE{w: w, rc: http.NewResponseController(w)}
}

func (a *agentSSE) frame(prefix, payload string) {
	_, _ = fmt.Fprintf(a.w, "%s%s\n\n", prefix, payload)
	_ = a.rc.Flush()
}

func (a *agentSSE) textDelta(s string) {
	p, _ := json.Marshal(map[string]interface{}{
		"choices": []map[string]interface{}{{"delta": map[string]string{"content": s}, "finish_reason": nil}},
	})
	a.frame("data: ", string(p))
}

func (a *agentSSE) toolEvent(e agentToolEvent) {
	p, _ := json.Marshal(e)
	a.frame("event: agent.tool\ndata: ", string(p))
}

func (a *agentSSE) errorEvent(msg string) {
	p, _ := json.Marshal(map[string]string{"error": msg})
	a.frame("event: agent.error\ndata: ", string(p))
}

func (a *agentSSE) done() { a.frame("data: ", "[DONE]") }

// ---------------------------------------------------------------- conversations

// acquireConv returns the conversation of a session and marks it as running. A second turn on a running
// session is refused, so one chat stream never mixes with another.
func (s *Server) acquireConv(id string) (*agentConv, bool) {
	s.agent.mu.Lock()
	defer s.agent.mu.Unlock()
	if s.agent.convs == nil {
		s.agent.convs = map[string]*agentConv{}
	}
	c, ok := s.agent.convs[id]
	if ok {
		c.mu.Lock()
		busy, idle := c.running, time.Since(c.lastUse) > agentConvTTL
		c.mu.Unlock()
		if idle && !busy {
			delete(s.agent.convs, id) // an idle conversation is forgotten
			ok = false
		}
	}
	if !ok {
		c = &agentConv{}
		s.agent.convs[id] = c
	}
	c.mu.Lock()
	busy := c.running
	c.running = true
	c.lastUse = time.Now()
	c.mu.Unlock()
	s.evictConvsLocked()
	if busy {
		return nil, false
	}
	return c, true
}

func (s *Server) endConv(c *agentConv) {
	c.mu.Lock()
	c.running = false
	c.lastUse = time.Now()
	c.mu.Unlock()
}

// evictConvsLocked drops conversations that were idle too long and, beyond the cap, the least recently used
// one. A running conversation is never dropped.
func (s *Server) evictConvsLocked() {
	now := time.Now()
	for id, c := range s.agent.convs {
		c.mu.Lock()
		idle, busy := now.Sub(c.lastUse), c.running
		c.mu.Unlock()
		if !busy && idle > agentConvTTL {
			delete(s.agent.convs, id)
		}
	}
	for len(s.agent.convs) > agentMaxConvs {
		oldID, oldUse := "", time.Time{}
		for id, c := range s.agent.convs {
			c.mu.Lock()
			busy, lu := c.running, c.lastUse
			c.mu.Unlock()
			if busy {
				continue
			}
			if oldID == "" || lu.Before(oldUse) {
				oldID, oldUse = id, lu
			}
		}
		if oldID == "" {
			return
		}
		delete(s.agent.convs, oldID)
	}
}

// ---------------------------------------------------------------- the turn

// runAgentTurn runs one chat turn: model call, tool calls, model call again. It streams to the browser and
// keeps the conversation in the store. A failed turn keeps the completed tool batches: the agent must still
// know that it restarted a service, or it would repeat the action. The history is never left with an
// assistant tool_use without its tool result, because that breaks every later call at the provider.
func (s *Server) runAgentTurn(w http.ResponseWriter, r *http.Request, c agentCfg, client llmClient, conv *agentConv, message string) bool {
	conv.mu.Lock()
	lastTokens := conv.lastTokens
	domain := conv.domain
	history := append([]llmMessage{}, conv.msgs...)
	conv.mu.Unlock()
	// The domain pointer travels with the request so set_tool_domain can widen it mid-turn.
	rctx := r.WithContext(context.WithValue(r.Context(), agentDomainKey{}, &domain))
	history = append(history, llmMessage{Role: llmRoleUser, Text: message})
	// saved is what a failed turn keeps: the history up to the last complete tool batch. Without it the agent
	// would forget that it already restarted a service and would repeat the action.
	var saved []llmMessage
	success := false
	defer func() {
		conv.mu.Lock()
		switch {
		case success:
			conv.msgs = history
		case saved != nil:
			conv.msgs = saved
		}
		conv.domain = domain
		conv.mu.Unlock()
	}()

	sse := newAgentSSE(w)
	// The system prompt is rebuilt every turn and never stored in the history.
	system := c.Soul + "\n\n" + s.agentMemoryPrompt(message)

	// A long text-only exchange never reaches a tool batch, so the history is compacted here too.
	if lastTokens > 0 && lastTokens+len(message)/3 > agentCompactTokens {
		history = s.compactHistory(r.Context(), client, c, history)
	}
	for step := range agentMaxSteps {
		if err := rctx.Context().Err(); err != nil {
			sse.errorEvent("the chat was cancelled")
			return false
		}
		req := llmRequest{Model: c.Model, System: system, Messages: shrinkOldToolResults(history), Tools: agentToolSpecsFor(domain)}
		modelStart := time.Now()
		res, err := client.Stream(rctx.Context(), req, sse.textDelta)
		logger.Info("agent", "", "model call finished", "step", step+1, "model", c.Model, "domain", string(domain), "tools", len(req.Tools),
			"prompt_size", res.InputTokens, "reply_size", res.OutputTokens, "duration_ms", time.Since(modelStart).Milliseconds(), "status", map[bool]string{true: "error", false: "success"}[err != nil])
		if err != nil {
			sse.errorEvent(redactSecret(err.Error(), c.Key))
			return false
		}
		// The size is measured on what was sent (after the trim), so a trimmed history is not compacted twice.
		tokens := agentPromptTokens(system, req.Messages, res.InputTokens)
		conv.mu.Lock()
		conv.lastTokens = tokens
		conv.mu.Unlock()
		calls := make([]llmToolCall, 0, len(res.ToolCalls))
		for _, tc := range res.ToolCalls {
			if tc.ID == "" {
				tc.ID = "call_" + randHex(6)
			}
			calls = append(calls, tc)
		}
		history = append(history, llmMessage{Role: llmRoleAssistant, Text: res.Text, ToolCalls: calls})
		if len(calls) == 0 {
			success = true
			sse.done()
			return true
		}
		// The calls of one batch run one after the other; a call that did not run still gets a result, so the
		// history stays valid and the batch can be kept.
		for i, tc := range calls {
			if err := rctx.Context().Err(); err != nil {
				for _, rest := range calls[i:] {
					history = append(history, llmMessage{Role: llmRoleTool, ToolCallID: rest.ID, Text: "not run: the chat was cancelled", IsError: true})
				}
				saved = append([]llmMessage{}, history...)
				sse.errorEvent("the chat was cancelled")
				return false
			}
			args, argErr := agentToolArgs(tc.Args)
			label := agentToolLabel(args)
			toolDomainName := string(agentToolDomain(tc.Name))
			sse.toolEvent(agentToolEvent{Tool: tc.Name, Label: label, Status: "running", ToolCallID: tc.ID, Domain: toolDomainName})
			callStart := time.Now()
			text, isErr := "", true
			if argErr != nil {
				text = "the tool arguments are not valid JSON"
			} else {
				text, isErr = s.callAgentTool(rctx, tc.Name, args)
			}
			status := "done"
			if isErr {
				status = "error"
			}
			sse.toolEvent(agentToolEvent{Tool: tc.Name, Label: label, Status: status, ToolCallID: tc.ID, Domain: toolDomainName, DurationMS: time.Since(callStart).Milliseconds()})
			history = append(history, llmMessage{Role: llmRoleTool, ToolCallID: tc.ID, Text: truncateToolResult(tc.Name, text), IsError: isErr})
		}
		// The batch is complete: it is kept even if the turn fails later.
		saved = append([]llmMessage{}, history...)
		if tokens > agentCompactTokens {
			history = s.compactHistory(r.Context(), client, c, history)
			saved = append([]llmMessage{}, history...)
		}
	}
	sse.errorEvent(fmt.Sprintf("the agent stopped after %d steps without finishing the answer", agentMaxSteps))
	return false
}

// agentPromptTokens is the prompt size of a call. Providers that ignore stream_options report no usage, so
// the size is estimated from the text: about three bytes per token.
func agentPromptTokens(system string, history []llmMessage, reported int) int {
	if reported > 0 {
		return reported
	}
	n := len(system)
	for _, m := range history {
		n += len(m.Text)
		for _, tc := range m.ToolCalls {
			n += len(tc.Name) + len(tc.Args)
		}
	}
	return n / 3
}

// agentToolArgs parses the raw arguments of a tool call.
func agentToolArgs(raw string) (map[string]interface{}, error) {
	args := map[string]interface{}{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return args, nil
	}
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, err
	}
	return args, nil
}

// agentToolLabel is the short hint the browser shows next to a running tool.
func agentToolLabel(args map[string]interface{}) string {
	for _, k := range []string{"unit", "container", "command", "path", "query", "host_id"} {
		v, ok := args[k]
		if !ok || v == nil {
			continue
		}
		label := strings.TrimSpace(fmt.Sprintf("%v", v))
		if label == "" {
			continue
		}
		return clipTo(label, agentLabelMax)
	}
	return ""
}

// toolResultHints say, per tool, how to get the part of an oversized result that was cut. A tool without a hint
// falls back to a generic line, so the model always knows the result was cut and how to narrow it.
var toolResultHints = map[string]string{
	"docker_logs":       "read the tail only: call docker_logs again with a smaller tail, or exec_command with `docker logs --tail 200 <container>`",
	"kube_logs":         "read the tail only: call kube_logs again with a smaller tail",
	"read_file":         "read the rest: call read_file again with offset set past the bytes already shown",
	"list_dir":          "narrow it: list a subdirectory instead of the whole tree",
	"disk_usage":        "drill down: call disk_usage again on one child directory",
	"systemd_detail":    "read the journal of one unit only: repeat systemd_detail with errors_only=true",
	"ansible_resources": "read one part: call ansible_resources with a single action (facts, library, adhoc, ...)",
	"ansible_runs":      "read one run: call ansible_runs with run_id for its status and recap",
	"tf_runs":           "read one run: call tf_runs with run_id",
	"vuln_status":       "narrow it: call vuln_status with unfixed=false, or fleet_search for one package",
	"docker_security":   "read one section: the audit findings or one image, not both",
	"iac_findings":      "read one source: call iac_findings with kind and id",
	"fleet_search":      "narrow the query, or raise limit",
	"host_info":          "ask for one part: host_info with what=resources, updates, firewall, domains or databases",
	"list_vault_keys":    "narrow it: call list_vault_keys with a scope filter",
	"trigger_env_sync":   "sync one target at a time: call trigger_env_sync with a single target_id",
}

// truncateToolResult cuts an oversized tool result to a head and a tail joined by a structured notice, so the
// model keeps the beginning (what the command was) and the end (how it failed, the last lines of a log) and is
// told how to fetch the middle. A cut is never silent: an unnotified truncation would make the model believe it
// saw the whole output.
func truncateToolResult(tool, s string) string {
	if len(s) <= agentToolResultMax {
		return s
	}
	// Half the budget to the head, half to the tail: an error and a summary usually sit at one end or the other.
	head := agentToolResultMax / 2
	tail := agentToolResultMax - head
	hint, ok := toolResultHints[tool]
	if !ok {
		hint = "narrow the call (fewer lines, one item or one host at a time) to see the rest"
	}
	cut := len(s) - head - tail
	return fmt.Sprintf("%s\n\n[tool result truncated: %d of %d bytes omitted from the middle. %s]\n\n%s",
		s[:head], cut, len(s), hint, s[len(s)-tail:])
}

// ---------------------------------------------------------------- compaction

// shrinkOldToolResults is the cheap, deterministic pass that runs before any model call: a tool result older
// than the newest agentStaleKeep tool messages is cut to its head, because the model already acted on it. It
// costs no tokens and no request, and it keeps ten log dumps from filling the window on their own.
func shrinkOldToolResults(msgs []llmMessage) []llmMessage {
	tools := make([]int, 0, len(msgs))
	for i, m := range msgs {
		if m.Role == llmRoleTool {
			tools = append(tools, i)
		}
	}
	if len(tools) <= agentStaleKeep {
		return msgs
	}
	for _, i := range tools[:len(tools)-agentStaleKeep] {
		if len(msgs[i].Text) > agentStaleBytes {
			msgs[i].Text = fmt.Sprintf("%s\n[older tool result trimmed: %d of %d bytes kept; ask again for the rest]",
				msgs[i].Text[:agentStaleBytes], agentStaleBytes, len(msgs[i].Text))
		}
	}
	return msgs
}

// agentCompactCut returns the index where the history may be cut: the second newest user message. The cut is
// always at a user message, so an assistant tool_use is never separated from its tool results, and the two
// newest user turns stay verbatim. -1 means there is nothing to compact.
func agentCompactCut(msgs []llmMessage) int {
	users := []int{}
	for i, m := range msgs {
		if m.Role == llmRoleUser && len(m.ToolCalls) == 0 {
			users = append(users, i)
		}
	}
	if len(users) < 3 {
		return -1
	}
	return users[len(users)-2]
}

// agentSummarySystem tells the model what a compaction summary must keep.
const agentSummarySystem = "You summarise an earlier infrastructure-assistant conversation so the assistant can " +
	"continue it. Keep the host names and ids, the findings, the decisions taken, the actions still waiting for " +
	"an operator approval and the operator's preferences. Be factual and compact, drop tool noise."

// compactHistory replaces the oldest part of the history with one summary message. If the summary call fails
// nothing is dropped: the turn continues with the full history.
func (s *Server) compactHistory(ctx context.Context, client llmClient, c agentCfg, history []llmMessage) []llmMessage {
	cut := agentCompactCut(history)
	if cut < 0 {
		return history
	}
	req := llmRequest{
		Model:     c.Model,
		System:    agentSummarySystem,
		Messages:  []llmMessage{{Role: llmRoleUser, Text: clipTo(agentTranscript(history[:cut]), 64<<10)}},
		MaxTokens: 2000,
	}
	res, err := client.Stream(ctx, req, nil)
	if err != nil {
		logger.Warn("agent", "llm", "history compaction failed, the full history is kept: "+err.Error())
		return history
	}
	summary := strings.TrimSpace(res.Text)
	if summary == "" {
		logger.Warn("agent", "llm", "history compaction returned nothing, the full history is kept")
		return history
	}
	out := make([]llmMessage, 0, len(history)-cut+1)
	out = append(out, llmMessage{Role: llmRoleUser, Text: "[Summary of the earlier conversation]\n" + summary})
	out = append(out, history[cut:]...)
	return out
}

// agentTranscript renders messages as plain text for the summary call.
func agentTranscript(msgs []llmMessage) string {
	var b strings.Builder
	for _, m := range msgs {
		switch m.Role {
		case llmRoleUser:
			b.WriteString("User: " + m.Text + "\n")
		case llmRoleAssistant:
			b.WriteString("Assistant: " + m.Text + "\n")
			for _, tc := range m.ToolCalls {
				b.WriteString("Assistant called the tool " + tc.Name + " with " + tc.Args + "\n")
			}
		case llmRoleTool:
			b.WriteString("Tool result: " + m.Text + "\n")
		}
	}
	return b.String()
}

// redactSecret keeps the provider key out of anything the user or the browser sees.
func redactSecret(s, key string) string {
	if key == "" {
		return s
	}
	return strings.ReplaceAll(s, key, "[redacted]")
}
