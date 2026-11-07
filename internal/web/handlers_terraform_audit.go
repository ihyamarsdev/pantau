package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"pantau/internal/logger"
)

const (
	tfAuditDefaultLimit = 100
	tfAuditMaxLimit     = 500
)

type tfAuditEvent struct {
	Time      string `json:"time"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	Workspace string `json:"workspace"`
}

// handleTerraformAudit serves GET /api/terraform/audit?workspace_id=&limit=100.
// Events are the `terraform`-module entries of the in-memory log ring buffer
// (the same source as the Activity panel), newest first.
func (s *Server) handleTerraformAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !s.checkTerraformGate(w) {
		return
	}

	limit := tfAuditDefaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"})
			return
		}
		limit = min(n, tfAuditMaxLimit)
	}

	// The filter token is the workspace name; a numeric id is resolved to its name.
	token := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if id, err := strconv.ParseInt(token, 10, 64); err == nil {
		if ws, _ := s.db.GetTFWorkspace(id); ws != nil {
			token = ws.Name
		}
	}

	events := make([]tfAuditEvent, 0)
	if rows, err := s.db.ListTFAudit(token, limit); err == nil {
		for _, e := range rows {
			events = append(events, tfAuditEvent{
				Time:      e.Time.UTC().Format(time.RFC3339),
				Level:     strings.ToLower(e.Level),
				Message:   e.Message,
				Workspace: e.Host,
			})
		}
	} else {
		events = tfAuditFromRing(token, limit)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"events": events})
}

// tfAuditFromRing is the fallback used only when the DB read fails.
func tfAuditFromRing(token string, limit int) []tfAuditEvent {
	history := logger.DefaultBroadcaster().History()
	events := make([]tfAuditEvent, 0, min(limit, len(history)))
	for i := len(history) - 1; i >= 0 && len(events) < limit; i-- {
		e := history[i]
		if e.Module != "terraform" {
			continue
		}
		if token != "" && e.Host != token && !strings.Contains(e.Message, token) {
			continue
		}
		events = append(events, tfAuditEvent{
			Time:      e.Timestamp.UTC().Format(time.RFC3339),
			Level:     strings.ToLower(e.Level),
			Message:   e.Message,
			Workspace: e.Host,
		})
	}
	return events
}
