package web

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"pantau/internal/logger"
)

// AI agent (Hermes Agent, https://github.com/NousResearch/hermes-agent) integration, ADR-0066.
//
// Two directions: the floating chat proxies to Hermes' OpenAI-compatible API server (/v1/chat/completions), and
// Hermes calls back into Pantau as an MCP server at /mcp (agent_mcp.go). Mutating tool calls wait here for an
// operator decision before they run.

//go:embed agent_soul.md
var defaultAgentSoul string

const (
	agentDefaultURL   = "http://127.0.0.1:8642"
	agentDefaultModel = "hermes-agent"
	agentApprovalWait = 170 * time.Second // plus mcpExecTimeout (120 s) stays below Hermes' default 300 s tool timeout
	agentMaxPending   = 20
	agentGrantTTL     = 2 * time.Hour // "approve for this session" lapses after this, or on New chat / Pantau restart
)

var agentSessionRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// agentHTTP has no overall timeout: chat turns stream for as long as the agent works. Redirects are refused so
// the bearer key cannot be forwarded to another host.
var agentHTTP = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

type agentApproval struct {
	ID       string    `json:"id"`
	HostID   int64     `json:"host_id"`
	HostName string    `json:"host_name"`
	Tool     string    `json:"tool"`
	Summary  string    `json:"summary"` // exact action, e.g. "systemctl restart nginx.service" or the full shell command
	Gate     int       `json:"gate"`    // 1 routine action, 2 arbitrary shell/high impact, 3 infrastructure change: never auto or granted
	Created  time.Time `json:"created"`
	done     chan bool
}

// agentState is the zero-value-usable runtime state of the agent integration.
type agentState struct {
	mu      sync.Mutex
	token   string
	pending map[string]*agentApproval
	grants  map[int64]agentGrant
}

func (s *Server) grantCovers(hostID int64, gate int) bool {
	s.agent.mu.Lock()
	defer s.agent.mu.Unlock()
	g, ok := s.agent.grants[hostID]
	if ok && time.Now().After(g.Expires) {
		delete(s.agent.grants, hostID)
		return false
	}
	return ok && g.Gate >= gate
}

// agentGrant is a session-long approval for one host: gate 1 covers dedicated tools, gate 2 also covers shell.
type agentGrant struct {
	HostID   int64     `json:"host_id"`
	HostName string    `json:"host_name"`
	Gate     int       `json:"gate"`
	Expires  time.Time `json:"expires"`
}

type agentCfg struct {
	Enabled  bool
	URL      string
	Key      string
	Model    string
	Approval string // ask | auto
	Soul     string
}

func (s *Server) agentConfig() agentCfg {
	get := func(k string) string { v, _ := s.db.GetSetting(k); return strings.TrimSpace(v) }
	c := agentCfg{Enabled: get("hermes_enabled") == "1", URL: get("hermes_url"), Key: get("hermes_key"), Model: get("hermes_model"), Approval: get("agent_approval")}
	if c.URL == "" {
		c.URL = agentDefaultURL
	}
	if c.Model == "" {
		c.Model = agentDefaultModel
	}
	if c.Approval != "auto" {
		c.Approval = "ask"
	}
	if v, _ := s.db.GetSetting("agent_soul"); strings.TrimSpace(v) != "" {
		c.Soul = v
	} else {
		c.Soul = defaultAgentSoul
	}
	return c
}

// agentToken returns the bearer token Hermes must present at /mcp, creating it on first use.
func (s *Server) agentToken() string {
	s.agent.mu.Lock()
	defer s.agent.mu.Unlock()
	if s.agent.token != "" {
		return s.agent.token
	}
	// PANTAU_AGENT_TOKEN pins the token (compose passes the same value to Hermes); otherwise one is generated.
	tok := strings.TrimSpace(os.Getenv("PANTAU_AGENT_TOKEN"))
	if tok != "" && len(tok) < 16 {
		tok = "" // too weak to be a bearer secret: ignore the env value
	}
	if tok == "" {
		tok, _ = s.db.GetSetting("agent_mcp_token")
	}
	if tok == "" {
		tok = randHex(24)
		_ = s.db.SetSetting("agent_mcp_token", tok)
	}
	s.agent.token = tok
	return tok
}

func (s *Server) agentTokenValid(got string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.agentToken())) == 1
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func normalizeAgentURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return agentDefaultURL, nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.User != nil {
		return "", fmt.Errorf("Hermes URL must be http:// or https:// with a host")
	}
	return raw, nil
}

func (s *Server) registerAgentRoutes() {
	s.mux.HandleFunc("/mcp", s.handleAgentMCP) // bearer token, not the session cookie: Hermes is not a browser
	s.mux.HandleFunc("/api/agent/settings", s.authMiddleware(s.handleAgentSettings))
	s.mux.HandleFunc("/api/agent/rotate-token", s.authMiddleware(s.handleAgentRotateToken))
	s.mux.HandleFunc("/api/agent/test", s.authMiddleware(s.handleAgentTest))
	s.mux.HandleFunc("/api/agent/chat", s.authMiddleware(s.handleAgentChat))
	s.mux.HandleFunc("/api/agent/approvals", s.authMiddleware(s.handleAgentApprovals))
	s.mux.HandleFunc("/api/agent/approvals/", s.authMiddleware(s.handleAgentApprovalDecide))
	s.mux.HandleFunc("/api/agent/grants", s.authMiddleware(s.handleAgentGrants))
	s.mux.HandleFunc("/api/agent/memory", s.authMiddleware(s.handleAgentMemory))
}

func (s *Server) handleAgentSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		c := s.agentConfig()
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"enabled": c.Enabled, "url": c.URL, "key_set": c.Key != "", "model": c.Model, "approval": c.Approval,
			"soul": c.Soul, "soul_is_default": c.Soul == defaultAgentSoul, "mcp_token": s.agentToken(),
		})
	case http.MethodPost:
		var req struct {
			Enabled  *bool   `json:"enabled"`
			URL      *string `json:"url"`
			Key      *string `json:"key"` // nil or "" keeps the stored key
			Model    *string `json:"model"`
			Approval *string `json:"approval"`
			Soul     *string `json:"soul"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		set := map[string]string{}
		if req.URL != nil {
			u, err := normalizeAgentURL(*req.URL)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			set["hermes_url"] = u
		}
		if req.Enabled != nil {
			set["hermes_enabled"] = map[bool]string{true: "1", false: "0"}[*req.Enabled]
		}
		if req.Key != nil && strings.TrimSpace(*req.Key) != "" {
			set["hermes_key"] = strings.TrimSpace(*req.Key)
		}
		if req.Model != nil {
			set["hermes_model"] = strings.TrimSpace(*req.Model)
		}
		if req.Approval != nil {
			set["agent_approval"] = map[bool]string{true: "auto", false: "ask"}[*req.Approval == "auto"]
		}
		if req.Soul != nil {
			soul := strings.TrimSpace(*req.Soul)
			if soul == strings.TrimSpace(defaultAgentSoul) {
				soul = ""
			}
			set["agent_soul"] = soul
		}
		for k, v := range set {
			_ = s.db.SetSetting(k, v)
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "settings_saved"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAgentRotateToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if os.Getenv("PANTAU_AGENT_TOKEN") != "" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "the token is set by PANTAU_AGENT_TOKEN; change it there"})
		return
	}
	tok := randHex(24)
	_ = s.db.SetSetting("agent_mcp_token", tok)
	s.agent.mu.Lock()
	s.agent.token = tok
	s.agent.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"mcp_token": tok})
}

// handleAgentTest checks that Hermes' API server answers with the configured key.
func (s *Server) handleAgentTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	c := s.agentConfig()
	ctx, cancel := contextTimeout(r, 8*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.URL+"/v1/models", nil)
	if c.Key != "" {
		req.Header.Set("Authorization", "Bearer "+c.Key)
	}
	resp, err := agentHTTP.Do(req)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": "Hermes unreachable: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": "Hermes rejected the API key (HTTP " + fmt.Sprint(resp.StatusCode) + ")"})
		return
	}
	if resp.StatusCode >= 300 {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": fmt.Sprintf("Hermes answered HTTP %d", resp.StatusCode)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

// handleAgentChat streams one chat turn from Hermes to the browser as SSE. Hermes keeps the conversation under
// X-Hermes-Session-Id, so only the new message and the SOUL travel on each turn.
func (s *Server) handleAgentChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Message   string `json:"message"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil || strings.TrimSpace(req.Message) == "" || !agentSessionRe.MatchString(req.SessionID) {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	c := s.agentConfig()
	if !c.Enabled {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "AI agent is disabled in Settings"})
		return
	}
	body, _ := json.Marshal(map[string]interface{}{
		"model": c.Model, "stream": true,
		"messages": []map[string]string{{"role": "system", "content": c.Soul + "\n\n" + s.agentMemoryPrompt()}, {"role": "user", "content": req.Message}},
	})
	hreq, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, c.URL+"/v1/chat/completions", bytes.NewReader(body))
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Accept", "text/event-stream")
	hreq.Header.Set("X-Hermes-Session-Id", req.SessionID)
	hreq.Header.Set("X-Hermes-Session-Key", "pantau")
	if c.Key != "" {
		hreq.Header.Set("Authorization", "Bearer "+c.Key)
	}
	resp, err := agentHTTP.Do(hreq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Hermes unreachable: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("Hermes answered HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	rc := http.NewResponseController(w)
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			_ = rc.Flush()
		}
		if err != nil {
			return
		}
	}
}

// ---------------------------------------------------------------- approvals

// agentApprove blocks until the operator decides on ap (or ctx/timeout ends, which denies). In "auto" mode
// gate-1 tools pass at once; gate-2 tools (arbitrary shell) always wait.
func (s *Server) agentApprove(r *http.Request, ap *agentApproval, gate int) (bool, string) {
	if gate == 1 && s.agentConfig().Approval == "auto" {
		return true, ""
	}
	// Gate 3 (terraform apply, ansible run, kubectl apply) and scopes without a host (HostID 0) are never granted.
	if gate < 3 && ap.HostID != 0 && s.grantCovers(ap.HostID, gate) {
		logger.Info("agent", ap.HostName, fmt.Sprintf("auto-approved by session grant, %s: %s", ap.Tool, ap.Summary))
		return true, ""
	}
	ap.Gate = gate
	ap.ID, ap.Created, ap.done = randHex(8), time.Now(), make(chan bool, 1)
	s.agent.mu.Lock()
	if s.agent.pending == nil {
		s.agent.pending = map[string]*agentApproval{}
	}
	if len(s.agent.pending) >= agentMaxPending {
		s.agent.mu.Unlock()
		return false, "too many actions are waiting for approval"
	}
	s.agent.pending[ap.ID] = ap
	s.agent.mu.Unlock()
	defer func() {
		s.agent.mu.Lock()
		delete(s.agent.pending, ap.ID)
		s.agent.mu.Unlock()
	}()
	t := time.NewTimer(agentApprovalWait)
	defer t.Stop()
	select {
	case ok := <-ap.done:
		if !ok {
			return false, "the operator denied this action"
		}
		return true, ""
	case <-t.C:
		return false, "no operator decision within " + agentApprovalWait.String() + ", action not executed"
	case <-r.Context().Done():
		return false, "request cancelled"
	}
}

func (s *Server) handleAgentApprovals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.agent.mu.Lock()
	list := make([]*agentApproval, 0, len(s.agent.pending))
	for _, a := range s.agent.pending {
		list = append(list, a)
	}
	s.agent.mu.Unlock()
	sort.Slice(list, func(i, j int) bool { return list[i].Created.Before(list[j].Created) })
	writeJSON(w, http.StatusOK, list)
}

// handleAgentApprovalDecide: POST /api/agent/approvals/{id} {"approve": bool}.
func (s *Server) handleAgentApprovalDecide(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/agent/approvals/")
	var req struct {
		Approve bool `json:"approve"`
		Session bool `json:"session"` // with approve: also approve the rest of this session on this host
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var also []*agentApproval
	s.agent.mu.Lock()
	ap := s.agent.pending[id]
	delete(s.agent.pending, id)
	if ap != nil && req.Approve && req.Session && ap.Gate < 3 && ap.HostID != 0 {
		if s.agent.grants == nil {
			s.agent.grants = map[int64]agentGrant{}
		}
		g := s.agent.grants[ap.HostID]
		if ap.Gate > g.Gate || time.Now().After(g.Expires) {
			g.Gate = ap.Gate
		}
		g.HostID, g.HostName, g.Expires = ap.HostID, ap.HostName, time.Now().Add(agentGrantTTL)
		s.agent.grants[ap.HostID] = g
		for k, p := range s.agent.pending { // calls already waiting on this host that the grant covers
			if p.HostID == ap.HostID && p.Gate <= g.Gate {
				also = append(also, p)
				delete(s.agent.pending, k)
			}
		}
	}
	s.agent.mu.Unlock()
	if ap == nil {
		http.Error(w, "not found (already decided or expired)", http.StatusNotFound)
		return
	}
	verb := map[bool]string{true: "approved", false: "denied"}[req.Approve]
	if req.Approve && req.Session && ap.Gate < 3 && ap.HostID != 0 {
		verb = fmt.Sprintf("approved for the session (%s, up to gate %d)", agentGrantTTL, ap.Gate)
	}
	logger.Info("agent", ap.HostName, fmt.Sprintf("operator %s %s: %s", verb, ap.Tool, ap.Summary))
	ap.done <- req.Approve
	for _, p := range also {
		logger.Info("agent", p.HostName, fmt.Sprintf("auto-approved by session grant, %s: %s", p.Tool, p.Summary))
		p.done <- true
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleAgentGrants: GET lists live session grants; DELETE revokes all, or one host with ?host=ID.
func (s *Server) handleAgentGrants(w http.ResponseWriter, r *http.Request) {
	s.agent.mu.Lock()
	defer s.agent.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		list := []agentGrant{}
		for id, g := range s.agent.grants {
			if time.Now().After(g.Expires) {
				delete(s.agent.grants, id)
				continue
			}
			list = append(list, g)
		}
		sort.Slice(list, func(i, j int) bool { return list[i].HostID < list[j].HostID })
		writeJSON(w, http.StatusOK, list)
	case http.MethodDelete:
		if h := r.URL.Query().Get("host"); h != "" {
			id, err := strconv.ParseInt(h, 10, 64)
			if err != nil {
				http.Error(w, "bad host", http.StatusBadRequest)
				return
			}
			delete(s.agent.grants, id)
		} else {
			s.agent.grants = nil
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
