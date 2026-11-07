package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"pantau/internal/ansible"
	"pantau/internal/store"
)

// ansibleGitSource resolves the git source of pb, including its token or SSH Key Library key.
func (s *Server) ansibleGitSource(pb *store.AnsiblePlaybook) (*ansible.GitSource, error) {
	g := &ansible.GitSource{URL: pb.GitURL, Branch: pb.GitBranch, Path: pb.GitPath}
	switch pb.GitAuth {
	case "token":
		g.Token = pb.GitToken
	case "ssh_key":
		key, err := s.db.GetSSHKey(pb.GitKeyID)
		if err != nil || key == nil {
			return nil, fmt.Errorf("git ssh key %d no longer exists in the SSH Key Library", pb.GitKeyID)
		}
		g.PrivateKey = key.PrivateKey
	}
	return g, nil
}

// handleAnsiblePlaybookContents: GET /api/ansible/playbooks/{id}/contents (Git playbooks only)
// Returns the readable files of a fresh shallow clone and the variables the playbook references.
func (s *Server) handleAnsiblePlaybookContents(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	pb, err := s.db.GetAnsiblePlaybook(id)
	if err != nil {
		if errors.Is(err, store.ErrAnsiblePlaybookNotFound) {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if pb.Source != "git" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "contents are only available for Git playbooks; the YAML of an inline playbook is in the playbook itself"})
		return
	}
	g, err := s.ansibleGitSource(pb)
	var proj *ansible.Project
	if err == nil {
		proj, err = ansible.LoadGit(r.Context(), *g)
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"project": proj,
		"vars":    ansible.AnalyzeVars(proj.Files, proj.Entry, pb.ExtraVars, s.ansibleLibDir),
	})
}

// handleAnsiblePlaybookAnalyze: POST /api/ansible/playbooks/analyze {"content","extra_vars"}
// Scans the YAML as currently typed (saved or not) for the variables it references; see ansible.AnalyzeVars.
func (s *Server) handleAnsiblePlaybookAnalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !s.checkAnsibleGate(w) {
		return
	}
	var req struct {
		Content   string `json:"content"`
		ExtraVars string `json:"extra_vars"`
	}
	if !decodeAnsibleBody(w, r, &req) {
		return
	}
	if len(req.Content) > maxPlaybookContentSize {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "playbook content exceeds 256 KiB"})
		return
	}
	if !validExtraVars(strings.TrimSpace(req.ExtraVars)) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "extra_vars must be a valid JSON object"})
		return
	}
	files := []ansible.ProjectFile{{Path: "playbook.yml", Content: req.Content}}
	writeJSON(w, http.StatusOK, map[string]interface{}{"vars": ansible.AnalyzeVars(files, "playbook.yml", req.ExtraVars, s.ansibleLibDir)})
}
