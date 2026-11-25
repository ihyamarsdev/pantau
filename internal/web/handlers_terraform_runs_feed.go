package web

import (
	"net/http"
	"strconv"
	"strings"

	"pantau/internal/store"
)

// handleTerraformRunsFeed serves GET /api/terraform/runs: runs across all workspaces for the global Runs tab.
// Query: status (comma-separated), type, trigger, q, limit (default 100, max 500).
func (s *Server) handleTerraformRunsFeed(w http.ResponseWriter, r *http.Request) {
	if !s.checkTerraformGate(w) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query()
	f := store.TFRunFeedFilter{
		Type:    strings.TrimSpace(q.Get("type")),
		Trigger: strings.TrimSpace(q.Get("trigger")),
		Query:   q.Get("q"),
	}
	for _, st := range strings.Split(q.Get("status"), ",") {
		if st = strings.TrimSpace(st); st != "" {
			f.Statuses = append(f.Statuses, st)
		}
	}
	if l, err := strconv.Atoi(q.Get("limit")); err == nil {
		f.Limit = l
	}

	rows, err := s.db.ListTFRunsFeed(f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	out := make([]map[string]interface{}, 0, len(rows))
	for i := range rows {
		item := formatRunSummary(&rows[i].Run)
		item["workspace_name"] = rows[i].WorkspaceName
		item["workspace_environment"] = rows[i].WorkspaceEnvironment
		item["workspace_group_name"] = rows[i].WorkspaceGroupName
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"runs": out})
}
