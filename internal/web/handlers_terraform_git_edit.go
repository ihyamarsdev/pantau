package web

import (
	"context"
	"fmt"
	"net/http"
	"pantau/internal/ansible"
	"pantau/internal/logger"
	"pantau/internal/store"
	"strings"
)

// terraformGitSource converts a Git-backed workspace into an ansible.GitSource, resolving SSH Key Library keys.
func (s *Server) terraformGitSource(ws *store.TFWorkspace) (*ansible.GitSource, error) {
	g := &ansible.GitSource{
		URL:    ws.GitURL,
		Branch: ws.GitBranch,
		Path:   "",
	}
	switch ws.GitAuth {
	case "token":
		g.Token = ws.GitToken
	case "ssh":
		if ws.GitKeyID > 0 {
			key, err := s.db.GetSSHKey(ws.GitKeyID)
			if err != nil || key == nil {
				return nil, fmt.Errorf("git ssh key %d no longer exists in the SSH Key Library", ws.GitKeyID)
			}
			g.PrivateKey = strings.TrimSpace(key.PrivateKey)
		}
	}
	return g, nil
}

// handleTerraformWorkspaceGitFiles: GET /api/terraform/workspaces/{id}/git/files
func (s *Server) handleTerraformWorkspaceGitFiles(w http.ResponseWriter, r *http.Request, ws *store.TFWorkspace) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if ws.Source != "git" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "git files are only available for Git workspaces"})
		return
	}
	if !gitAvailable() {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "git binary not found in PATH"})
		return
	}
	g, err := s.terraformGitSource(ws)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), gitEditReadTimeout)
	defer cancel()

	treeFn := s.gitEditTree
	if treeFn == nil {
		treeFn = ansible.ReadRepoTree
	}
	tree, err := treeFn(ctx, *g)
	if err != nil {
		writeGitError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"commit":    tree.Commit,
		"branch":    tree.Branch,
		"files":     tree.Files,
		"truncated": tree.Truncated,
	})
}

// handleTerraformWorkspaceGitFile: GET /api/terraform/workspaces/{id}/git/file?path=
func (s *Server) handleTerraformWorkspaceGitFile(w http.ResponseWriter, r *http.Request, ws *store.TFWorkspace) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path parameter is required"})
		return
	}
	if ws.Source != "git" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "git file is only available for Git workspaces"})
		return
	}
	if !gitAvailable() {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "git binary not found in PATH"})
		return
	}
	g, err := s.terraformGitSource(ws)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), gitEditReadTimeout)
	defer cancel()

	fileFn := s.gitEditFile
	if fileFn == nil {
		fileFn = ansible.ReadRepoFile
	}
	content, commit, err := fileFn(ctx, *g, filePath)
	if err != nil {
		writeGitError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"path":    filePath,
		"content": content,
		"size":    len(content),
		"commit":  commit,
	})
}

// handleTerraformWorkspaceGitCommit: POST /api/terraform/workspaces/{id}/git/commit
func (s *Server) handleTerraformWorkspaceGitCommit(w http.ResponseWriter, r *http.Request, ws *store.TFWorkspace) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if ws.Source != "git" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "git commit is only available for Git workspaces"})
		return
	}
	if !gitAvailable() {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "git binary not found in PATH"})
		return
	}
	g, err := s.terraformGitSource(ws)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	var spec ansible.CommitSpec
	if !decodeTerraformBody(w, r, &spec) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), gitEditCommitTimeout)
	defer cancel()

	commitFn := s.gitEditCommit
	if commitFn == nil {
		commitFn = ansible.CommitAndPush
	}
	res, err := commitFn(ctx, *g, spec)
	if err != nil {
		writeGitError(w, err)
		return
	}

	// HTTPS tokens or SSH keys must have write permissions on the remote repository.
	// Read-only credentials will fail the push with a 502 Bad Gateway response.
	logger.Info("terraform", ws.Name, fmt.Sprintf("Committed and pushed %s on branch %s (%d files changed)", res.Commit, res.Branch, res.Changed))

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"commit":  res.Commit,
		"branch":  res.Branch,
		"pushed":  res.Pushed,
		"changed": res.Changed,
	})
}
