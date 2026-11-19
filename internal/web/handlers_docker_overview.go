package web

// GET /api/docker/overview: aggregated fleet Docker posture (ADR 0090). Reads persisted
// telemetry only; no SSH, no network. Optional ?host_id= and ?group_id= filters.

import (
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) handleDockerOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var hostID int64
	if raw := r.URL.Query().Get("host_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, "invalid host_id", http.StatusBadRequest)
			return
		}
		hostID = id
	}
	groupName := strings.TrimSpace(r.URL.Query().Get("group_id"))
	ov, err := s.db.GetDockerOverview(hostID, groupName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, ov)
}
