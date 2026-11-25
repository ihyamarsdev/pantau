package web

import (
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/logger"
)

// The native AI agent: Pantau calls the LLM provider itself (agent_llm.go), runs the tool-call loop
// (agent_loop.go) and offers its own tools to the model (agent_tools.go). Mutating tool calls wait here
// for an operator decision before they run.

//go:embed agent_soul.md
var defaultAgentSoul string

const (
	agentApprovalWait = 170 * time.Second // an unattended approval card is denied after this long
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
	mu         sync.Mutex
	pending    map[string]*agentApproval
	grants     map[int64]agentGrant
	convs      map[string]*agentConv                                     // chat sessions of the in-process agent
	llmFactory func(provider, baseURL, apiKey string) (llmClient, error) // nil: the real provider client
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
	Enabled       bool
	Provider      string // "", "anthropic" or "openai" (any OpenAI-compatible endpoint)
	BaseURL       string // empty: the provider default
	Key           string
	Model         string
	Approval      string // ask | auto
	Soul          string
	TriageEnabled bool
	TriageModel   string
	TriageBudget  int
	BriefingTime  string
	CompactTokens int // conversation compaction threshold in prompt tokens
}

func (s *Server) agentConfig() agentCfg {
	get := func(k string) string { v, _ := s.db.GetSetting(k); return strings.TrimSpace(v) }
	c := agentCfg{
		Enabled:  get("agent_enabled") == "1",
		Provider: get("agent_llm_provider"),
		BaseURL:  get("agent_llm_base_url"),
		Key:      get("agent_llm_api_key"),
		Model:    get("agent_llm_model"),
		Approval: get("agent_approval"),
	}
	if c.Approval != "auto" {
		c.Approval = "ask"
	}
	if v, _ := s.db.GetSetting("agent_soul"); strings.TrimSpace(v) != "" {
		c.Soul = v
	} else {
		c.Soul = defaultAgentSoul
	}
	c.TriageEnabled = get("agent_triage_enabled") != "0"
	c.TriageModel = get("agent_triage_model")
	c.TriageBudget = 10000
	if v, err := strconv.Atoi(get("agent_triage_daily_budget")); err == nil && v >= 0 {
		c.TriageBudget = v
	}
	c.BriefingTime = get("agent_briefing_time")
	// Compaction threshold is configurable but clamped to a safe range: too small and the agent loses
	// context mid-task, too large and the token cost grows without bound.
	c.CompactTokens = agentCompactTokens
	if v, err := strconv.Atoi(get("agent_compact_tokens")); err == nil && v >= 20000 && v <= 200000 {
		c.CompactTokens = v
	}
	return c
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// normalizeLLMBaseURL checks the base URL of a native provider. An empty URL means the provider default.
func normalizeLLMBaseURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.User != nil {
		return "", fmt.Errorf("the LLM base URL must be http:// or https:// with a host")
	}
	return raw, nil
}

func (s *Server) registerAgentRoutes() {
	s.mux.HandleFunc("/api/agent/settings", s.authMiddleware(s.handleAgentSettings))
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
			"enabled": c.Enabled, "provider": c.Provider, "base_url": c.BaseURL, "api_key_set": c.Key != "",
			"model": c.Model, "approval": c.Approval, "soul": c.Soul, "soul_is_default": c.Soul == defaultAgentSoul,
			"triage_enabled": c.TriageEnabled, "triage_model": c.TriageModel,
			"triage_daily_budget": c.TriageBudget, "briefing_time": c.BriefingTime, "compact_tokens": c.CompactTokens,
		})
	case http.MethodPost:
		var req struct {
			Enabled           *bool   `json:"enabled"`
			Provider          *string `json:"provider"`
			BaseURL           *string `json:"base_url"`
			APIKey            *string `json:"api_key"` // nil or "" keeps the stored key
			Model             *string `json:"model"`
			Approval          *string `json:"approval"`
			Soul              *string `json:"soul"`
			TriageEnabled     *bool   `json:"triage_enabled"`
			TriageModel       *string `json:"triage_model"`
			TriageDailyBudget *int    `json:"triage_daily_budget"`
			BriefingTime      *string `json:"briefing_time"`
			CompactTokens     *int    `json:"compact_tokens"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		set := map[string]string{}
		if req.Enabled != nil {
			set["agent_enabled"] = map[bool]string{true: "1", false: "0"}[*req.Enabled]
		}
		if req.Provider != nil {
			p := strings.ToLower(strings.TrimSpace(*req.Provider))
			switch p {
			case "", llmProviderAnthropic, llmProviderOpenAI:
			default:
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the LLM provider must be anthropic, openai or empty"})
				return
			}
			set["agent_llm_provider"] = p
		}
		if req.BaseURL != nil {
			u, err := normalizeLLMBaseURL(*req.BaseURL)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			set["agent_llm_base_url"] = u
		}
		if req.APIKey != nil && strings.TrimSpace(*req.APIKey) != "" {
			set["agent_llm_api_key"] = strings.TrimSpace(*req.APIKey)
		}
		if req.Model != nil {
			set["agent_llm_model"] = strings.TrimSpace(*req.Model)
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
		if req.TriageEnabled != nil {
			set["agent_triage_enabled"] = map[bool]string{true: "1", false: "0"}[*req.TriageEnabled]
		}
		if req.TriageModel != nil {
			set["agent_triage_model"] = strings.TrimSpace(*req.TriageModel)
		}
		if req.TriageDailyBudget != nil {
			b := *req.TriageDailyBudget
			if b < 0 {
				b = 0
			}
			set["agent_triage_daily_budget"] = strconv.Itoa(b)
		}
		if req.BriefingTime != nil {
			tm := strings.TrimSpace(*req.BriefingTime)
			if tm != "" {
				if _, err := time.Parse("15:04", tm); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": "briefing time must be HH:MM"})
					return
				}
			}
			set["agent_briefing_time"] = tm
		}
		if req.CompactTokens != nil {
			v := *req.CompactTokens
			if v < 20000 {
				v = 20000
			} else if v > 200000 {
				v = 200000
			}
			set["agent_compact_tokens"] = strconv.Itoa(v)
		}
		for k, v := range set {
			_ = s.db.SetSetting(k, v)
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "settings_saved"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAgentTest checks that the configured provider answers with one tiny model call.
func (s *Server) handleAgentTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	c := s.agentConfig()
	if c.Model == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": "no model is configured in Settings"})
		return
	}
	client, err := s.agentLLMClient(c)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": redactSecret(err.Error(), c.Key)})
		return
	}
	ctx, cancel := contextTimeout(r, 20*time.Second)
	defer cancel()
	req := llmRequest{Model: c.Model, Messages: []llmMessage{{Role: llmRoleUser, Text: "ping"}}, MaxTokens: 16}
	if _, err := client.Stream(ctx, req, nil); err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": redactSecret(err.Error(), c.Key)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

// handleAgentChat streams one chat turn to the browser as SSE.
func (s *Server) handleAgentChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Message   string `json:"message"`
		SessionID string `json:"session_id"`
		Page      string `json:"page"` // active UI page; narrows the tools offered to the model
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
	if c.Provider == "" || c.Model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the AI agent is not configured: choose a provider and a model in Settings -> AI Agent"})
		return
	}
	s.chatNative(w, r, c, req.Message, req.SessionID, req.Page)
}

// chatNative runs one turn of the in-process agent: Pantau calls the provider itself and streams the answer.
func (s *Server) chatNative(w http.ResponseWriter, r *http.Request, c agentCfg, message, sessionID, page string) {
	if c.Model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no model is configured in Settings (AI agent)"})
		return
	}
	if c.Provider == llmProviderAnthropic && c.Key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no API key is configured in Settings (AI agent)"})
		return
	}
	client, err := s.agentLLMClient(c)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSecret(err.Error(), c.Key)})
		return
	}
	conv, ok := s.acquireConv(sessionID)
	if !ok {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "the agent is still answering"})
		return
	}
	defer s.endConv(conv)
	// A page context narrows the tools. An empty page (API callers) keeps all tools; a set_tool_domain
	// meta-tool can still widen it later in the conversation.
	if d := agentDomainForContext(page); d != domainAll {
		conv.mu.Lock()
		conv.domain = d
		conv.mu.Unlock()
	}
	s.runAgentTurn(w, r, c, client, conv, message)
}

// agentLLMClient builds the provider client of the in-process agent. Tests replace it with a fake.
func (s *Server) agentLLMClient(c agentCfg) (llmClient, error) {
	if s.agent.llmFactory != nil {
		return s.agent.llmFactory(c.Provider, c.BaseURL, c.Key)
	}
	return newLLMClient(c.Provider, c.BaseURL, c.Key)
}

// ---------------------------------------------------------------- approvals

// agentApprove blocks until the operator decides on ap (or ctx/timeout ends, which denies). In "auto" mode
// gate-1 tools pass at once; gate-2 tools (arbitrary shell) always wait.
func (s *Server) agentApprove(r *http.Request, ap *agentApproval, gate int) (bool, string) {
	if gate == 1 && s.agentConfig().Approval == "auto" {
		return true, ""
	}
	// Gate 3 (terraform apply, ansible run, kubectl apply) and scopes without a host (HostID 0) are never granted.
	// A destructive shell command is never covered by a session grant: it always asks the operator.
	destructive := (ap.Tool == "exec_command" || ap.Tool == "ssh_batch_inspect") &&
		inspector.ClassifyAgentCommand(ap.Summary) == riskDestructive
	if gate < 3 && ap.HostID != 0 && !destructive && s.grantCovers(ap.HostID, gate) {
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
