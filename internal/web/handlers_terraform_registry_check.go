package web

import (
	"encoding/json"
	"net/http"
	"net/url"

	"pantau/internal/store"
	"pantau/internal/terraform"
)

// handleWorkspaceRegistry: GET /api/terraform/workspaces/{id}/registry/check
// compares the workspace's registry modules/providers with the latest published versions.
func (s *Server) handleWorkspaceRegistry(w http.ResponseWriter, r *http.Request, ws *store.TFWorkspace, rest []string) {
	if len(rest) != 1 || rest[0] != "check" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		tfJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if ws.Source != "inline" {
		writeJSON(w, http.StatusOK, map[string]any{"supported": false, "items": []any{}, "errors": []string{}})
		return
	}
	files := map[string]string{}
	if ws.Files != "" {
		if err := json.Unmarshal([]byte(ws.Files), &files); err != nil {
			tfJSONError(w, http.StatusInternalServerError, "invalid workspace files")
			return
		}
	}
	c := s.registryClient()
	host := ""
	if u, err := url.Parse(c.BaseURL); err == nil {
		host = u.Host
	}
	items, errs := terraform.CheckRegistryRefs(r.Context(), c, terraform.ScanRegistryRefs(files, host))
	writeJSON(w, http.StatusOK, map[string]any{"supported": true, "items": items, "errors": errs})
}
