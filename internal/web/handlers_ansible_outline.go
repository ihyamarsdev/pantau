package web

import (
	"net/http"

	"pantau/internal/ansible"
)

// handleAnsiblePlaybookOutline: POST /api/ansible/playbooks/outline {"content"} cuts a single-play playbook into
// play settings, pre_tasks, roles and tasks (see ansible.SplitPlaybook). Nothing is stored.
func (s *Server) handleAnsiblePlaybookOutline(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, ansible.SplitPlaybook(req.Content))
}

// handleAnsiblePlaybookCompose: POST /api/ansible/playbooks/compose {play, pre_tasks, tasks, roles} is the inverse
// of outline. Invalid parts answer 400 with the reason.
func (s *Server) handleAnsiblePlaybookCompose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !s.checkAnsibleGate(w) {
		return
	}
	var req ansible.Outline
	if !decodeAnsibleBody(w, r, &req) {
		return
	}
	content, err := ansible.JoinPlaybook(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if len(content) > maxPlaybookContentSize {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "playbook content exceeds 256 KiB"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": content})
}

// handleAnsibleRoles: GET /api/ansible/roles lists the roles a playbook can name: Library roles, roles of Library
// collections and system roles.
func (s *Server) handleAnsibleRoles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !s.checkAnsibleGate(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"roles": ansible.ListRoleChoices(s.ansibleLibDir)})
}
