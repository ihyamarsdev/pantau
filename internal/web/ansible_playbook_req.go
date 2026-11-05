package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"pantau/internal/ansible"
	"pantau/internal/store"
)

// decodePlaybookRequest parses and validates a create/update body. base is the stored playbook on update (nil on
// create); the returned playbook is a copy of base with the request applied. On failure it has already answered.
func (s *Server) decodePlaybookRequest(w http.ResponseWriter, r *http.Request, base *store.AnsiblePlaybook) (*store.AnsiblePlaybook, bool) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Content     string `json:"content"`
		ExtraVars   string `json:"extra_vars"`
		Source      string `json:"source"`
		GitURL      string `json:"git_url"`
		GitBranch   string `json:"git_branch"`
		GitPath     string `json:"git_path"`
		GitAuth     string `json:"git_auth"`
		GitToken    string `json:"git_token"`
		GitKeyID    int64  `json:"git_key_id"`
	}
	if !decodeAnsibleBody(w, r, &req) {
		return nil, false
	}
	bad := func(msg string) (*store.AnsiblePlaybook, bool) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return nil, false
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > maxPlaybookNameLength {
		return bad("playbook name must be 1-64 characters")
	}

	req.ExtraVars = strings.TrimSpace(req.ExtraVars)
	if req.ExtraVars != "" {
		if len(req.ExtraVars) > maxExtraVarsLength {
			return bad("extra_vars must not exceed 64 KiB")
		}
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(req.ExtraVars), &obj); err != nil {
			return bad("extra_vars must be a valid JSON object")
		}
	}

	pb := &store.AnsiblePlaybook{}
	if base != nil {
		*pb = *base
	}
	pb.Name, pb.Description, pb.ExtraVars = req.Name, req.Description, req.ExtraVars

	switch req.Source {
	case "", "inline":
		if len(req.Content) == 0 || len(req.Content) > maxPlaybookContentSize {
			return bad("playbook content must be 1-256 KiB")
		}
		pb.Source, pb.Content = "inline", req.Content
		pb.GitURL, pb.GitBranch, pb.GitPath, pb.GitAuth, pb.GitToken, pb.GitKeyID = "", "", "", "none", "", 0

	case "git":
		req.GitURL, req.GitBranch, req.GitPath = strings.TrimSpace(req.GitURL), strings.TrimSpace(req.GitBranch), strings.TrimSpace(req.GitPath)
		if req.GitAuth == "" {
			req.GitAuth = "none"
		}
		if err := (ansible.GitSource{URL: req.GitURL, Branch: req.GitBranch, Path: req.GitPath}).Validate(); err != nil {
			return bad(err.Error())
		}
		scheme := ansible.GitURLScheme(req.GitURL)
		switch req.GitAuth {
		case "none":
		case "token":
			if scheme != "https" {
				return bad("token authentication requires an https:// git url")
			}
			if req.GitToken == "" && (base == nil || base.GitAuth != "token" || base.GitToken == "") {
				return bad("git token is required for token authentication")
			}
		case "ssh_key":
			if scheme != "ssh" {
				return bad("ssh key authentication requires an ssh:// or user@host:path git url")
			}
			if key, err := s.db.GetSSHKey(req.GitKeyID); err != nil || key == nil {
				return bad("git ssh key not found in the SSH Key Library")
			}
		default:
			return bad("git_auth must be none, token or ssh_key")
		}
		pb.Source, pb.Content = "git", ""
		pb.GitURL, pb.GitBranch, pb.GitPath, pb.GitAuth, pb.GitKeyID = req.GitURL, req.GitBranch, req.GitPath, req.GitAuth, req.GitKeyID
		pb.GitToken = req.GitToken // "" keeps the stored token (store.UpdateAnsiblePlaybook)

	default:
		return bad("source must be inline or git")
	}
	return pb, true
}
