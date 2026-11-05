package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"pantau/internal/logger"
	"pantau/internal/store"
)

const (
	maxImportPlaybooks = 100
	// maxPlaybookImportBody holds 100 playbooks of up to 256 KiB plus JSON overhead.
	maxPlaybookImportBody = 32 << 20
)

func validExtraVars(s string) bool {
	if s == "" {
		return true
	}
	var object map[string]interface{}
	return len(s) <= maxExtraVarsLength && json.Unmarshal([]byte(s), &object) == nil && object != nil
}

// handleAnsibleImport: POST /api/ansible/playbooks/import
// Copies playbook YAML (read from a local folder by the browser) into Pantau as ordinary inline playbooks, all or
// nothing. After the import they are edited like any other playbook; nothing of the folder is kept.
func (s *Server) handleAnsibleImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !s.checkAnsibleGate(w) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPlaybookImportBody)
	var req struct {
		Playbooks []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			ExtraVars   string `json:"extra_vars"`
			Content     string `json:"content"`
		} `json:"playbooks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		status := http.StatusBadRequest
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		writeJSON(w, status, map[string]string{"error": "invalid import request"})
		return
	}
	bad := func(msg string) { writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg}) }
	if len(req.Playbooks) == 0 || len(req.Playbooks) > maxImportPlaybooks {
		bad(fmt.Sprintf("select 1-%d playbooks to import", maxImportPlaybooks))
		return
	}
	names := make(map[string]bool, len(req.Playbooks))
	pbs := make([]*store.AnsiblePlaybook, 0, len(req.Playbooks))
	for _, it := range req.Playbooks {
		name := strings.TrimSpace(it.Name)
		if name == "" || len(name) > maxPlaybookNameLength {
			bad("playbook name must be 1-64 characters")
			return
		}
		if names[strings.ToLower(name)] {
			bad(fmt.Sprintf("duplicate playbook name %q", name))
			return
		}
		names[strings.ToLower(name)] = true
		extra := strings.TrimSpace(it.ExtraVars)
		if !validExtraVars(extra) {
			bad(fmt.Sprintf("%s: extra_vars must be a valid JSON object", name))
			return
		}
		if it.Content == "" || len(it.Content) > maxPlaybookContentSize {
			bad(fmt.Sprintf("%s: playbook content must be 1 byte to 256 KiB", name))
			return
		}
		pbs = append(pbs, &store.AnsiblePlaybook{Name: name, Description: it.Description, ExtraVars: extra, Content: it.Content})
	}
	if err := s.db.ImportAnsiblePlaybooks(pbs); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrAnsiblePlaybookNameTaken) {
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	for _, p := range pbs {
		logger.Info("ansible", p.Name, fmt.Sprintf("Imported playbook (id %d)", p.ID))
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"playbooks": pbs})
}
