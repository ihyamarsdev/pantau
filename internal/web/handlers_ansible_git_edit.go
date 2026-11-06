package web

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"regexp"
	"strings"
	"time"

	"pantau/internal/ansible"
	"pantau/internal/logger"
	"pantau/internal/store"
)

const (
	gitEditReadTimeout   = 60 * time.Second
	gitEditCommitTimeout = 120 * time.Second
)

// handleAnsiblePlaybookGitFiles: GET /api/ansible/playbooks/{id}/git/files
func (s *Server) handleAnsiblePlaybookGitFiles(w http.ResponseWriter, r *http.Request, id int64) {
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "git files are only available for Git playbooks"})
		return
	}
	if !gitAvailable() {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "git binary not found in PATH"})
		return
	}
	g, err := s.ansibleGitSource(pb)
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

// handleAnsiblePlaybookGitFile: GET /api/ansible/playbooks/{id}/git/file?path=
func (s *Server) handleAnsiblePlaybookGitFile(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path parameter is required"})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "git file is only available for Git playbooks"})
		return
	}
	if !gitAvailable() {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "git binary not found in PATH"})
		return
	}
	g, err := s.ansibleGitSource(pb)
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

// handleAnsiblePlaybookGitCommit: POST /api/ansible/playbooks/{id}/git/commit
func (s *Server) handleAnsiblePlaybookGitCommit(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodPost {
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "git commit is only available for Git playbooks"})
		return
	}
	if !gitAvailable() {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "git binary not found in PATH"})
		return
	}
	g, err := s.ansibleGitSource(pb)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	var spec ansible.CommitSpec
	if !decodeAnsibleBody(w, r, &spec) {
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
	// If committed to the resource's own branch (spec.NewBranch is empty), update last sync.
	if spec.NewBranch == "" {
		_ = s.db.SetAnsiblePlaybookGitSync(id, time.Now().UTC(), res.Commit)
	}

	logger.Info("ansible", pb.Name, fmt.Sprintf("Committed and pushed %s on branch %s (%d files changed)", res.Commit, res.Branch, res.Changed))

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"commit":  res.Commit,
		"branch":  res.Branch,
		"pushed":  res.Pushed,
		"changed": res.Changed,
	})
}

var tokenLeakRegex = regexp.MustCompile(`(?i)(x-access-token:|token:?|bearer\s+|authorization:\s*(?:basic|bearer)\s*)[^\s@/:]+(@|\b)`)

// sanitizeErrorMessage strips credentials / tokens from error messages before sending to client.
func sanitizeErrorMessage(msg string) string {
	if i := strings.Index(msg, "Authorization:"); i >= 0 {
		msg = msg[:i] + "Authorization: <redacted>"
	}
	msg = tokenLeakRegex.ReplaceAllString(msg, "$1<redacted>$2")
	return msg
}

// writeGitError maps errors returned by git tree/file/commit operations to appropriate HTTP status codes.
func writeGitError(w http.ResponseWriter, err error) {
	msg := sanitizeErrorMessage(err.Error())
	switch {
	case errors.Is(err, ansible.ErrNothingToCommit):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no changes to commit"})
	case errors.Is(err, ansible.ErrRepoChanged):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "repository changed since opened; reload"})
	case errors.Is(err, ansible.ErrPushRejected):
		writeJSON(w, http.StatusConflict, map[string]string{"error": msg})
	case errors.Is(err, ansible.ErrNotText):
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "file is not valid UTF-8 text"})
	case errors.Is(err, fs.ErrNotExist):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
	case errors.Is(err, ansible.ErrTooLarge):
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": msg})
	case errors.Is(err, ansible.ErrInvalidInput):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
	default:
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": msg})
	}
}
