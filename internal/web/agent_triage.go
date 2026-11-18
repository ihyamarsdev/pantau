package web

// The web layer implements inspector.TriageLLM with the in-process provider
// client of the AI agent (ADR 0073) and exposes the triage/briefing data to the
// floating banner and the Daily Briefing card. One call, no tools, terse JSON.

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pantau/internal/inspector"
)

// Triage implements inspector.TriageLLM. It deliberately builds a bare
// llmRequest with no Tools and a hard MaxTokens cap, then parses the JSON answer.
func (s *Server) Triage(ctx context.Context, model, system, prompt string, maxTokens int) (inspector.TriageDiagnosis, int, error) {
	var out inspector.TriageDiagnosis
	c := s.agentConfig()
	if c.Provider == "" {
		return out, 0, context.Canceled
	}
	if model == "" {
		model = c.Model // lightweight override falls back to the primary agent model
	}
	if model == "" {
		return out, 0, context.Canceled
	}
	client, err := s.agentLLMClient(c)
	if err != nil {
		return out, 0, err
	}
	if maxTokens <= 0 || maxTokens > 300 {
		maxTokens = 300
	}
	req := llmRequest{
		Model:     model,
		System:    system,
		Messages:  []llmMessage{{Role: llmRoleUser, Text: prompt}},
		Tools:     nil, // zero tool definitions: a single-turn call, no tool loop
		MaxTokens: maxTokens,
	}
	callCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	res, err := client.Stream(callCtx, req, nil)
	if err != nil {
		return out, 0, err
	}
	// Parse the terse JSON, tolerating a markdown fence around it.
	text := strings.TrimSpace(res.Text)
	if i := strings.Index(text, "```"); i >= 0 {
		text = strings.TrimPrefix(text[i:], "```json")
		text = strings.TrimPrefix(text, "```")
		if j := strings.Index(text, "```"); j >= 0 {
			text = text[:j]
		}
		text = strings.TrimSpace(text)
	}
	if start := strings.IndexByte(text, '{'); start >= 0 {
		if end := strings.LastIndexByte(text, '}'); end > start {
			text = text[start : end+1]
		}
	}
	var parsed struct {
		RootCause         string `json:"root_cause"`
		Impact            string `json:"impact"`
		Remediation       string `json:"remediation"`
		RemediationAction string `json:"remediation_action"`
	}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return out, res.InputTokens + res.OutputTokens, err
	}
	// Only allow the two deterministic, low-risk remediation actions.
	action := parsed.RemediationAction
	if action != "systemd_restart" && action != "docker_restart" {
		action = ""
	}
	return inspector.TriageDiagnosis{
		RootCause:         parsed.RootCause,
		Impact:            parsed.Impact,
		Remediation:       parsed.Remediation,
		RemediationAction: action,
	}, res.InputTokens + res.OutputTokens, nil
}

func (s *Server) registerTriageRoutes() {
	s.mux.HandleFunc("/api/agent/triage", s.authMiddleware(s.handleAgentTriage))
	s.mux.HandleFunc("/api/agent/briefing", s.authMiddleware(s.handleAgentBriefing))
	s.mux.HandleFunc("/api/agent/briefing/generate", s.authMiddleware(s.handleBriefingGenerate))
}

// handleAgentTriage lists recent diagnoses or returns the diagnosis of one alert.
func (s *Server) handleAgentTriage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if alertID := strings.TrimSpace(r.URL.Query().Get("alert_id")); alertID != "" {
		id, err := strconv.ParseInt(alertID, 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, http.StatusOK, nil)
			return
		}
		d, err := s.db.LatestTriageForAlert(id)
		if err != nil {
			writeJSON(w, http.StatusOK, nil)
			return
		}
		writeJSON(w, http.StatusOK, d)
		return
	}
	list, err := s.db.ListRecentTriage(50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleAgentBriefing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b, err := s.db.LatestBriefing()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) handleBriefingGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	svc := s.briefing
	if svc == nil {
		http.Error(w, "briefing service unavailable", http.StatusServiceUnavailable)
		return
	}
	b, err := svc.Run(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// StartAITriage wires the in-process LLM client into the inspector's triage
// worker and starts the optional daily briefing scheduler. Called from main.go
// after the server exists, because the Server is the TriageLLM implementation.
func (s *Server) StartAITriage(ctx context.Context) {
	if s == nil || s.db == nil {
		return
	}
	settings := func(k string) string {
		v, _ := s.db.GetSetting(k)
		return strings.TrimSpace(v)
	}
	if w := s.inspector.TriageWorker(); w != nil {
		w.SetLLM(s)
		w.SetSettingsReader(settings)
	}
	svc := inspector.NewBriefingService(s.db, s)
	svc.SetSettingsReader(settings)
	s.briefing = svc
	go svc.StartBriefingScheduler(ctx)
}
