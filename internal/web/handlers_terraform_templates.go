package web

import (
	"net/http"

	"pantau/internal/terraform"
)

// handleTerraformTemplates serves the built-in workspace templates.
func (s *Server) handleTerraformTemplates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.checkTerraformGate(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"templates": terraform.Templates()})
}
