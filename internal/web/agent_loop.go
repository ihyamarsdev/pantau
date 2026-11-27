package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"pantau/internal/inspector"
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

// agentConv is the conversation of one chat session. Its history is held here while the turn runs and mirrored
// into agent_messages, so a restart resumes it (agent_threads.go).
type agentConv struct {
	mu         sync.Mutex
	msgs       []llmMessage
	lastUse    time.Time
	running    bool
	lastTokens int                          // the prompt size of the last model call, 0 when unknown
	domain     toolDomain                   // active tool domain, set from the UI page and mutable via set_tool_domain
	exec       *inspector.AgentSessionState // per-session SSH cwd state (ADR 0086)
	threadID   int64                        // stored thread this conversation belongs to, 0 = not stored
	stored     int                          // history entries already in the database
	loaded     bool                         // history was read from the thread (never re-read mid-turn)
}

type agentToolEvent struct {
	Tool       string `json:"tool"`
	Label      string `json:"label"`
	Status     string `json:"status"`
	ToolCallID string `json:"toolCallId"`
	Domain     string `json:"domain,omitempty"`
	RiskTier   string `json:"risk_tier,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	PlanID     string `json:"plan_id,omitempty"` // set on the done event of agent_propose_plan: the chat shows its card
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
		c = &agentConv{exec: inspector.NewAgentSessionState()}
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
	threadID, stored := conv.threadID, conv.stored
	conv.mu.Unlock()
	// The domain pointer and the per-session cwd state travel with the request so tools can widen the
	// domain and run relative to the session's working directory (ADR 0086).
	rctx := r.WithContext(context.WithValue(r.Context(), agentDomainKey{}, &domain))
	if conv.exec == nil {
		conv.exec = inspector.NewAgentSessionState()
	}
	rctx = rctx.WithContext(inspector.WithAgentSessionState(rctx.Context(), conv.exec))
	history = append(history, llmMessage{Role: llmRoleUser, Text: message})
	// saved is what a failed turn keeps: the history up to the last complete tool batch. Without it the agent
	// would forget that it already restarted a service and would repeat the action.
	var saved []llmMessage
	success := false
	// rewrite is set by a compaction: the oldest messages are gone from the history, so the stored rows are
	// replaced instead of appended, or they would come back on the next resume.
	rewrite := false
	defer func() {
		conv.mu.Lock()
		switch {
		case success:
			conv.msgs = history
		case saved != nil:
			conv.msgs = saved
		}
		final := conv.msgs
		conv.domain = domain
		conv.mu.Unlock()
		if threadID == 0 {
			return
		}
		s.saveAgentHistory(threadID, final, stored, rewrite)
		conv.mu.Lock()
		conv.stored = len(final)
		conv.mu.Unlock()
	}()

	sse := newAgentSSE(w)
	// The system prompt is rebuilt every turn and never stored in the history. Dynamic skill injection keeps
	// only the active domain's instructions and scoped memory notes, while a host fingerprint gives the model
	// OS-correct execution context before it writes any shell command (ADR 0086).
	system := agentSystemPromptFor(c.Soul, domain) + "\n\n" + s.agentMemoryPromptForDomain(message, domain) + "\n" + s.agentFingerprintPrompt(message, threadID) +
		"\n" + s.agentKnowledgePrompt(message, domain) + s.agentCustomSkillsPrompt(domain)

	// A long text-only exchange never reaches a tool batch, so the history is compacted here too.
	if lastTokens > 0 && lastTokens+len(message)/3 > c.CompactTokens {
		before := len(history)
		history = s.compactHistory(r.Context(), client, c, history)
		rewrite = rewrite || len(history) < before
	}
	guard := newAgentTurnGuard(agentGuardHintAfterKubeGet, agentGuardRepeatLimit, agentGuardTotalLimit)
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
			logger.Warn("agent", "llm", "model call failed: "+redactSecret(err.Error(), c.Key))
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
			// Auto-generate a concise thread title on the first completed turn using the Fast/Utility model.
			if threadID > 0 && stored == 0 && s.agent.llmFactory == nil {
				s.maybeGenerateThreadTitle(c, threadID, message, res.Text)
			}
			return true
		}
		// Pass 1, in call order: every call is announced and the turn guard decides it. A call that does not run
		// still gets a result, so the history stays valid and the batch can be kept.
		plans := make([]agentPlanned, len(calls))
		for i, tc := range calls {
			args, argErr := agentToolArgs(tc.Args)
			tier := riskTierOfTool(tc.Name, args)
			p := agentPlanned{
				tc: tc, args: args, argErr: argErr, label: agentToolLabel(args),
				domain: string(agentToolDomain(tc.Name)), tier: string(tier),
				// set_tool_domain writes the turn's shared domain, so it never runs beside another call.
				parallel: tier == riskReadOnly && tc.Name != "set_tool_domain" && argErr == nil,
			}
			sse.toolEvent(agentToolEvent{Tool: tc.Name, Label: p.label, Status: "running", ToolCallID: tc.ID, Domain: p.domain, RiskTier: p.tier})
			p.refuse, p.hint = guard.before(tc.Name, tier == riskReadOnly)
			plans[i] = p
		}
		// Pass 2: a run of independent read-only calls executes at once (bounded); every other call runs alone,
		// behind the approval gate where it needs one. Results are appended in call order either way.
		for i := 0; i < len(plans); {
			if err := rctx.Context().Err(); err != nil {
				for _, rest := range plans[i:] {
					history = append(history, llmMessage{Role: llmRoleTool, ToolCallID: rest.tc.ID, Text: "not run: the chat was cancelled", IsError: true})
				}
				saved = append([]llmMessage{}, history...)
				sse.errorEvent("the chat was cancelled")
				return false
			}
			end := i + 1
			if plans[i].parallel {
				for end < len(plans) && plans[end].parallel {
					end++
				}
			}
			outs := s.execPlanned(rctx, plans[i:end])
			for k, p := range plans[i:end] {
				status := "done"
				if outs[k].isErr {
					status = "error"
				}
				sse.toolEvent(agentToolEvent{Tool: p.tc.Name, Label: p.label, Status: status, ToolCallID: p.tc.ID, Domain: p.domain, RiskTier: p.tier, DurationMS: outs[k].durationMS, PlanID: planIDFromResult(p.tc.Name, outs[k])})
				// The hint follows truncation so a long result cannot cut it off.
				history = append(history, llmMessage{Role: llmRoleTool, ToolCallID: p.tc.ID, ToolName: p.tc.Name, Text: truncateToolResult(p.tc.Name, outs[k].text) + p.hint, IsError: outs[k].isErr})
			}
			i = end
		}
		// The batch is complete: it is kept even if the turn fails later.
		saved = append([]llmMessage{}, history...)
		if tokens > c.CompactTokens {
			before := len(history)
			history = s.compactHistory(r.Context(), client, c, history)
			rewrite = rewrite || len(history) < before
			saved = append([]llmMessage{}, history...)
		}
	}
	sse.errorEvent(fmt.Sprintf("the agent stopped after %d steps without finishing the answer", agentMaxSteps))
	return false
}

// agentParallelReadMax bounds how many read-only tool calls of one model batch run at the same time.
const agentParallelReadMax = 4

// agentPlanned is one tool call of a model batch, decided before anything runs.
type agentPlanned struct {
	tc       llmToolCall
	args     map[string]interface{}
	argErr   error
	label    string
	domain   string
	tier     string
	refuse   string // non-empty: the guard refused the call; this is its result
	hint     string // appended to the result of a call that runs
	parallel bool   // a read-only call that may run beside other read-only calls
}

// agentOutcome is what one planned call produced.
type agentOutcome struct {
	text       string
	isErr      bool
	durationMS int64
}

// execPlanned runs a segment of planned calls. A segment of parallel calls runs concurrently, at most
// agentParallelReadMax at once; the outcomes keep the order of the segment. A panicking tool becomes an error
// result, since a panic in a worker goroutine would otherwise take the whole process down.
func (s *Server) execPlanned(r *http.Request, seg []agentPlanned) []agentOutcome {
	outs := make([]agentOutcome, len(seg))
	run := func(k int) {
		started := time.Now()
		p := seg[k]
		switch {
		case p.argErr != nil:
			outs[k] = agentOutcome{text: "the tool arguments are not valid JSON", isErr: true}
		case p.refuse != "":
			outs[k] = agentOutcome{text: p.refuse, isErr: true}
		default:
			text, isErr := s.callAgentTool(r, p.tc.Name, p.args)
			outs[k] = agentOutcome{text: text, isErr: isErr}
		}
		outs[k].durationMS = time.Since(started).Milliseconds()
	}
	if len(seg) == 1 || !seg[0].parallel {
		for k := range seg {
			run(k)
		}
		return outs
	}
	sem := make(chan struct{}, agentParallelReadMax)
	var wg sync.WaitGroup
	for k := range seg {
		sem <- struct{}{}
		wg.Add(1)
		go func(k int) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("agent", seg[k].tc.Name, fmt.Sprintf("tool panicked: %v", rec))
					outs[k] = agentOutcome{text: "the tool failed unexpectedly", isErr: true}
				}
				<-sem
				wg.Done()
			}()
			run(k)
		}(k)
	}
	wg.Wait()
	return outs
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
	if tasks, ok := args["tasks"].([]interface{}); ok && len(tasks) > 0 {
		doms := make([]string, 0, len(tasks))
		for _, t := range tasks {
			if obj, ok := t.(map[string]interface{}); ok {
				doms = append(doms, argStr(obj, "domain"))
			}
		}
		return clipTo(fmt.Sprintf("Investigating %d domains in parallel: %s", len(tasks), strings.Join(doms, ", ")), agentLabelMax)
	}
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
	"host_info":         "ask for one part: host_info with what=resources, updates, firewall, domains or databases",
	"list_vault_keys":   "narrow it: call list_vault_keys with a scope filter",
	"trigger_env_sync":  "sync one target at a time: call trigger_env_sync with a single target_id",
}

// truncateToolResult cuts an oversized tool result to a head and a tail joined by a structured notice, so the
// model keeps the beginning (what the command was) and the end (how it failed, the last lines of a log) and is
// told how to fetch the middle. A cut is never silent: an unnotified truncation would make the model believe it
// saw the whole output.
//
// Structured JSON results carrying diagnostics / init_error / error_excerpt are NEVER sliced mid-JSON:
// non-essential fields and warnings are trimmed first so all errors (first 15) and init_error stay 100% intact.
func truncateToolResult(tool, s string) string {
	if len(s) <= agentToolResultMax {
		return s
	}
	if out, ok := truncateStructuredToolResult(s); ok {
		return out
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

func hasStructuredDiagnostics(m map[string]interface{}) bool {
	for _, k := range []string{"diagnostics", "validation", "init_error", "init_error_tail", "error_excerpt"} {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}

func trimDiagnosticsObj(obj map[string]interface{}, maxWarnings, maxErrors int) {
	raw, ok := obj["diagnostics"].([]interface{})
	if !ok {
		return
	}
	var errs, warns []interface{}
	for _, item := range raw {
		d, _ := item.(map[string]interface{})
		if sev, _ := d["severity"].(string); sev == "error" {
			errs = append(errs, item)
		} else {
			warns = append(warns, item)
		}
	}
	if maxErrors >= 0 && len(errs) > maxErrors {
		obj["errors_omitted"] = len(errs) - maxErrors
		errs = errs[:maxErrors]
	}
	if maxWarnings >= 0 && len(warns) > maxWarnings {
		obj["warnings_omitted"] = len(warns) - maxWarnings
		warns = warns[:maxWarnings]
	}
	combined := make([]interface{}, 0, len(errs)+len(warns))
	combined = append(combined, errs...)
	combined = append(combined, warns...)
	obj["diagnostics"] = combined
}

func truncateStructuredToolResult(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	if !strings.HasPrefix(trimmed, "{") {
		return "", false
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &m); err != nil || !hasStructuredDiagnostics(m) {
		return "", false
	}

	// Step 1: drop bulky non-diagnostic fields first.
	for _, drop := range []string{"files_written", "files", "metadata", "workspace", "variables"} {
		delete(m, drop)
	}
	if _, hasExcerpt := m["error_excerpt"]; hasExcerpt {
		delete(m, "log_tail")
	}
	if b, err := json.Marshal(m); err == nil && len(b) <= agentToolResultMax {
		return string(b), true
	}

	// Step 2: trim warnings while keeping up to 15 error diagnostics 100% intact.
	diagTarget := m
	if val, ok := m["validation"].(map[string]interface{}); ok {
		diagTarget = val
	}
	for _, warnCap := range []int{10, 3, 0} {
		trimDiagnosticsObj(diagTarget, warnCap, -1)
		if b, err := json.Marshal(m); err == nil && len(b) <= agentToolResultMax {
			return string(b), true
		}
	}

	// Step 3: if >15 huge errors still exceed budget, keep first 15 errors intact.
	trimDiagnosticsObj(diagTarget, 0, 15)
	if b, err := json.Marshal(m); err == nil {
		return string(b), true
	}
	return "", false
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
		// A host list is small and foundational: trimming it to 2 KB silently drops the hosts at the
		// end, so a later turn can no longer see that "macmini-4" exists. Keep these whole.
		if msgs[i].ToolName == "list_hosts" || msgs[i].ToolName == "fleet_hosts" {
			continue
		}
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

// compactHistory replaces the oldest part of the history with one summary message using the Fast / Utility
// model (falling back to the primary Reasoning model). If the summary call fails nothing is dropped: the turn
// continues with the full history.
func (s *Server) compactHistory(ctx context.Context, client llmClient, c agentCfg, history []llmMessage) []llmMessage {
	cut := agentCompactCut(history)
	if cut < 0 {
		return history
	}
	fastClient, fastModel, ferr := s.agentFastLLMClient(c)
	if ferr != nil || fastClient == nil || fastModel == "" {
		fastClient, fastModel = client, c.Model
	}
	req := llmRequest{
		Model:     fastModel,
		System:    agentSummarySystem,
		Messages:  []llmMessage{{Role: llmRoleUser, Text: clipTo(agentTranscript(history[:cut]), 64<<10)}},
		MaxTokens: 2000,
	}
	res, err := fastClient.Stream(ctx, req, nil)
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
