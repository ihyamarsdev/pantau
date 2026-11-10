package web

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"pantau/internal/store"
)

// Fleet search over the data Pantau already stores (package scans, container
// drift records, remembered Compose projects). No SSH happens during a search.
//
//	GET /api/search?q=<text>&kind=all|package|container&limit=200
func (s *Server) registerFleetSearchRoutes() {
	s.mux.HandleFunc("/api/search", s.authMiddleware(s.handleFleetSearch))
}

// handleFleetSearch answers a fleet-wide search over the stored inventory.
func (s *Server) handleFleetSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if n := utf8.RuneCountInString(q); n < 2 || n > 100 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"ok": false, "error": "query must be 2 to 100 characters",
		})
		return
	}
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	if kind == "" {
		kind = store.FleetSearchAll
	}
	if kind != store.FleetSearchAll && kind != store.FleetSearchPackage && kind != store.FleetSearchContainer {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"ok": false, "error": "kind must be all, package or container",
		})
		return
	}
	limit := 200
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"ok": false, "error": "limit must be a positive number",
			})
			return
		}
		if n > 500 {
			n = 500
		}
		limit = n
	}

	res, err := s.db.SearchFleet(q, kind, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"ok": false, "error": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":                     true,
		"query":                  res.Query,
		"kind":                   res.Kind,
		"groups":                 res.Groups,
		"truncated":              res.Truncated,
		"total_hosts":            res.TotalHosts,
		"hosts_without_packages": res.HostsWithoutPackages,
		"hosts_without_docker":   res.HostsWithoutDocker,
	})
}
