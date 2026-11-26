package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"pantau/internal/inspector"
	"pantau/internal/logger"
	"pantau/internal/store"
)

// Agent chat history in SQLite: one thread per conversation, grouped by the resource the conversation is about
// (a host, a cluster, docker, ansible, terraform or the whole fleet). Every turn is stored, so a page reload or
// a Pantau restart resumes the same conversation instead of starting over. Text is redacted on its way in.

const (
	agentStoredToolMax = 8 << 10 // bytes of one tool result kept in the stored history
	agentThreadBodyMax = 4 << 10 // request body of the thread endpoints
)

var vaultPlaceholderRe = regexp.MustCompile(`\{\{\s*vault\.[A-Za-z_][A-Za-z0-9_]*\s*\}\}`)

// agentDeniedRe spots a change the operator refused, so a stored tool row says so.
var agentDeniedRe = regexp.MustCompile(`the operator denied|no operator decision|too many actions are waiting for approval|request cancelled`)

// agentRedactText masks secrets in text before it reaches SQLite. A vault placeholder is kept verbatim: a blind
// secret is a reference, not a value, and redacting it would turn an operator template into noise.
func agentRedactText(s string) string {
	if s == "" {
		return s
	}
	locs := vaultPlaceholderRe.FindAllStringIndex(s, -1)
	if locs == nil {
		return agentRedactSegment(s)
	}
	var b strings.Builder
	prev := 0
	for _, loc := range locs {
		b.WriteString(agentRedactSegment(s[prev:loc[0]]))
		b.WriteString(s[loc[0]:loc[1]])
		prev = loc[1]
	}
	b.WriteString(agentRedactSegment(s[prev:]))
	return b.String()
}

func agentRedactSegment(s string) string {
	return vaultRedactor.Redact(inspector.RedactAgentSecrets(s))
}

// clipBytes cuts to at most n bytes without splitting a character.
func clipBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return fmt.Sprintf("%s\n[stored history truncated: %d bytes omitted]", s[:cut], len(s)-cut)
}

// agentMessageFromLLM is one history entry as a stored row: redacted, with the tool calls of an assistant
// message as JSON and a clipped tool result.
func agentMessageFromLLM(m llmMessage) store.AgentMessage {
	out := store.AgentMessage{
		Role:       m.Role,
		Content:    agentRedactText(m.Text),
		ToolCallID: m.ToolCallID,
		ToolName:   m.ToolName,
		IsError:    m.IsError,
	}
	if m.Role == llmRoleTool {
		out.Content = clipBytes(out.Content, agentStoredToolMax)
		if m.IsError && agentDeniedRe.MatchString(out.Content) {
			out.Approval = "denied"
		}
	}
	if len(m.ToolCalls) > 0 {
		if b, err := json.Marshal(m.ToolCalls); err == nil {
			out.ToolCalls = agentRedactText(string(b))
		}
	}
	return out
}

// agentHistoryFromStore rebuilds the model history of a thread.
func agentHistoryFromStore(msgs []store.AgentMessage) []llmMessage {
	out := make([]llmMessage, 0, len(msgs))
	for _, m := range msgs {
		lm := llmMessage{Role: m.Role, Text: m.Content, ToolCallID: m.ToolCallID, ToolName: m.ToolName, IsError: m.IsError}
		if m.ToolCalls != "" {
			var calls []llmToolCall
			if err := json.Unmarshal([]byte(m.ToolCalls), &calls); err == nil {
				lm.ToolCalls = calls
			}
		}
		out = append(out, lm)
	}
	return out
}

// loadAgentHistory reads a stored thread back into the history the model sees.
func (s *Server) loadAgentHistory(threadID int64) []llmMessage {
	msgs, err := s.db.ListAgentMessages(threadID)
	if err != nil {
		logger.Warn("agent", "history", "could not read the stored conversation: "+err.Error())
		return nil
	}
	return agentHistoryFromStore(msgs)
}

// saveAgentHistory stores the history of a turn. With rewrite the rows are replaced (a compaction dropped the
// oldest messages, and they must not come back on the next resume); otherwise only the entries past stored are
// appended, which is what a normal turn produced.
func (s *Server) saveAgentHistory(threadID int64, msgs []llmMessage, stored int, rewrite bool) {
	if threadID == 0 || (!rewrite && stored >= len(msgs)) {
		return
	}
	rows := func(from int) []store.AgentMessage {
		out := make([]store.AgentMessage, 0, len(msgs)-from)
		for _, m := range msgs[from:] {
			out = append(out, agentMessageFromLLM(m))
		}
		return out
	}
	var err error
	if rewrite {
		err = s.db.ReplaceAgentMessages(threadID, rows(0))
	} else {
		err = s.db.AppendAgentMessages(threadID, rows(stored))
	}
	if err != nil {
		logger.Warn("agent", "history", "could not store the conversation: "+err.Error())
	}
}

// agentScopeFor names the resource a new thread belongs to: an explicit cluster or host wins, then the UI page.
func agentScopeFor(page string, hostID, clusterID int64) (string, int64) {
	switch {
	case clusterID > 0:
		return "cluster", clusterID
	case hostID > 0:
		return "host", hostID
	}
	switch strings.ToLower(strings.TrimSpace(page)) {
	case "docker", "kube":
		return "docker", 0
	case "ansible":
		return "ansible", 0
	case "terraform":
		return "terraform", 0
	default:
		return "global", 0
	}
}

// agentThreadFor resolves the thread of this turn: the one named, else the newest of the browser session when it
// belongs to the same scope, else a new thread under the scope the request carries. It returns nil when the
// database refuses, so a chat still runs without history instead of failing.
func (s *Server) agentThreadFor(session, scopeType string, scopeID, threadID int64) *store.AgentThread {
	if threadID > 0 {
		if t, err := s.db.GetAgentThread(threadID); err == nil {
			return t
		}
	}
	if t, err := s.db.AgentThreadBySession(session); err == nil && t != nil {
		// A chat opened about another resource starts its own thread; a global question continues the one the
		// operator already sees in the panel.
		if scopeType == "" || scopeType == "global" || (t.ScopeType == scopeType && t.ScopeID == scopeID) {
			return t
		}
	}
	t, err := s.db.CreateAgentThread(scopeType, scopeID, session, "")
	if err != nil {
		logger.Warn("agent", "history", "could not open a conversation thread: "+err.Error())
		return nil
	}
	return t
}

// attachAgentThread points the in-memory conversation at a stored thread and loads its history once, so a turn
// after a restart continues where the conversation stopped. A conversation that already holds messages (the
// process never restarted) keeps them and adopts the thread for storage instead of dropping them.
func (s *Server) attachAgentThread(conv *agentConv, th *store.AgentThread) {
	if th == nil {
		return
	}
	conv.mu.Lock()
	defer conv.mu.Unlock()
	if conv.threadID == th.ID && conv.loaded {
		return
	}
	if len(conv.msgs) > 0 {
		// A live history the database does not have yet: it is written to this thread on the next save.
		conv.threadID, conv.stored, conv.loaded = th.ID, 0, true
		return
	}
	conv.threadID = th.ID
	conv.msgs = s.loadAgentHistory(th.ID)
	conv.stored = len(conv.msgs)
	conv.loaded = true
}

// ---------------------------------------------------------------- endpoints

func (s *Server) handleAgentThreads(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	scopeType, scopeID := q.Get("scope_type"), queryInt64(q.Get("scope_id"))
	switch r.Method {
	case http.MethodGet:
		l := []store.AgentThread{}
		if session := q.Get("session"); session != "" && scopeType == "" {
			th, err := s.db.AgentThreadBySession(session)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			if th != nil {
				l = append(l, *th)
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"threads": l})
			return
		}
		list, err := s.db.ListAgentThreads(scopeType, scopeID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"threads": list})
	case http.MethodPost:
		var req struct {
			ScopeType string `json:"scope_type"`
			ScopeID   int64  `json:"scope_id"`
			SessionID string `json:"session_id"`
			Title     string `json:"title"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, agentThreadBodyMax)).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		th, err := s.db.CreateAgentThread(req.ScopeType, req.ScopeID, req.SessionID, req.Title)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, th)
	case http.MethodDelete:
		n, err := s.db.ClearAgentThreads(scopeType, scopeID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"status": "ok", "deleted": n})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAgentThread: /api/agent/threads/{id} -> GET returns the thread with its messages, DELETE drops it.
func (s *Server) handleAgentThread(w http.ResponseWriter, r *http.Request) {
	id := queryInt64(strings.TrimPrefix(r.URL.Path, "/api/agent/threads/"))
	if id <= 0 {
		http.Error(w, "bad thread id", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		th, err := s.db.GetAgentThread(id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		msgs, err := s.db.ListAgentMessages(id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"thread": th, "messages": msgs})
	case http.MethodDelete:
		if err := s.db.DeleteAgentThread(id); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func queryInt64(v string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if n < 0 {
		return 0
	}
	return n
}
