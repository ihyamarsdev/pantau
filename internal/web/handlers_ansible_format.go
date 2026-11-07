package web

import (
	"net/http"

	"pantau/internal/ansible"
)

// handleAnsiblePlaybookFormat: POST /api/ansible/playbooks/format {"content"} checks a playbook and answers the
// formatted YAML (2-space indent). Nothing is stored.
func (s *Server) handleAnsiblePlaybookFormat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !s.checkAnsibleGate(w) {
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if !decodeAnsibleBody(w, r, &req) {
		return
	}
	if len(req.Content) > maxPlaybookContentSize {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "playbook content exceeds 256 KiB"})
		return
	}
	content, err := ansible.FormatPlaybook(req.Content)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"content": content, "changed": content != req.Content})
}
