package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"pantau/internal/ansible"
	"pantau/internal/logger"
	"pantau/internal/store"
	"pantau/internal/transfer"
)

// Repositories section: Git repositories cloned (full clone, e.g. over SSH) into a folder on the Pantau server,
// browsed, edited, committed and pushed from the dashboard. The clone is persistent; edits go to the working tree.
// The HTTPS token / SSH key must have WRITE permission on the remote: a read-only credential makes the push fail with
// 409 and the git message, and the commit stays local (push can be retried).

const (
	reposDirSetting   = "repos_dir"
	repoCloneTimeout  = 5 * time.Minute
	repoSyncTimeout   = 2 * time.Minute
	repoLocalTimeout  = 30 * time.Second
	repoFileBodyLimit = 2 << 20
)

var repoNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func (s *Server) repoProtocols() string {
	if s.gitProtocols != "" {
		return s.gitProtocols
	}
	return "https:ssh"
}

// reposRoot is the folder new clones go into: the repos_dir setting, else the default set by SetReposDir (<data>/repos).
func (s *Server) reposRoot() (string, error) {
	if v, _ := s.db.GetSetting(reposDirSetting); v != "" {
		return v, nil
	}
	if s.reposDir == "" {
		return "", errors.New("repositories folder is not configured")
	}
	return s.reposDir, nil
}

func (s *Server) repoGitSource(r *store.GitRepo) (*ansible.GitSource, error) {
	g := &ansible.GitSource{URL: r.URL, Branch: r.Branch}
	switch r.Auth {
	case "token":
		g.Token = r.Token
	case "ssh":
		key, err := s.db.GetSSHKey(r.KeyID)
		if err != nil || key == nil {
			return nil, fmt.Errorf("ssh key %d no longer exists in the SSH Key Library", r.KeyID)
		}
		g.PrivateKey = strings.TrimSpace(key.PrivateKey)
	}
	return g, nil
}

func repoJSON(r *store.GitRepo) map[string]interface{} {
	return map[string]interface{}{
		"id": r.ID, "name": r.Name, "url": r.URL, "branch": r.Branch, "group": r.Group, "dir": r.Dir,
		"auth": r.Auth, "token_set": r.Token != "", "key_id": r.KeyID,
		"synced_at": r.SyncedAt, "created_at": r.CreatedAt,
	}
}

func decodeRepoBody(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, repoFileBodyLimit)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return false
	}
	return true
}

func methodNotAllowed(w http.ResponseWriter) {
	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
}

func badRequest(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
}

// checkRepoCredentials validates auth/token/key_id of a request and normalizes unused fields.
func (s *Server) checkRepoCredentials(auth, token string, keyID int64, gitURL string) error {
	switch auth {
	case "none":
	case "token":
		if token == "" {
			return nil // empty keeps the stored token on update; create checks it separately
		}
		if ansible.GitURLScheme(gitURL) != "https" {
			return errors.New("token authentication requires an https:// git url")
		}
	case "ssh":
		if k, err := s.db.GetSSHKey(keyID); err != nil || k == nil {
			return errors.New("select an SSH key from the SSH Key Library")
		}
	default:
		return errors.New("auth must be none, token or ssh")
	}
	return nil
}

// handleRepos: GET /api/repos (list with working-tree summary), POST (start a clone job, 202 {job}).
func (s *Server) handleRepos(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		repos, err := s.db.ListGitRepos()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		root, _ := s.reposRoot()
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		out := make([]map[string]interface{}, 0, len(repos))
		for i := range repos {
			j := repoJSON(&repos[i])
			if st, err := ansible.LocalRepoStatus(ctx, repos[i].Dir); err != nil {
				j["error"] = sanitizeErrorMessage(err.Error())
			} else {
				j["status"] = map[string]interface{}{"branch": st.Branch, "commit": st.Commit, "ahead": st.Ahead, "behind": st.Behind, "dirty": len(st.Changes)}
			}
			out = append(out, j)
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"repos": out, "folder": root})

	case http.MethodPost:
		s.createRepo(w, r)

	default:
		methodNotAllowed(w)
	}
}

type repoReq struct {
	Name   string `json:"name"`
	Group  string `json:"group"`
	URL    string `json:"url"`
	Branch string `json:"branch"`
	Auth   string `json:"auth"`
	Token  string `json:"token"`
	KeyID  int64  `json:"key_id"`
}

// repoFolderName validates a folder (group) name and reuses the spelling of an existing folder that differs only in case,
// so "torcons" and "Torcons" never become two folders (they would collide on case-insensitive filesystems).
func repoFolderName(raw string, existing []store.GitRepo) (string, error) {
	g := strings.TrimSpace(raw)
	if g == "" {
		return "", nil
	}
	if !repoNameRe.MatchString(g) {
		return "", errors.New("folder must be 1-64 characters: letters, digits, '.', '_' or '-', starting with a letter or digit")
	}
	for _, e := range existing {
		if strings.EqualFold(e.Group, g) {
			return e.Group, nil
		}
	}
	return g, nil
}

// repoLocation is the location folder a repository's folder (group) sits in.
func repoLocation(r *store.GitRepo) string {
	d := filepath.Dir(r.Dir)
	if r.Group != "" {
		d = filepath.Dir(d)
	}
	return d
}

// checkFolderFree refuses a folder that is itself a clone: a repository must never be created inside another one.
func checkFolderFree(location, group string) error {
	if group == "" {
		return nil
	}
	if _, err := os.Stat(filepath.Join(location, group, ".git")); err == nil {
		return fmt.Errorf("%q is already a repository at this location, choose another folder name", group)
	}
	return nil
}

func (s *Server) createRepo(w http.ResponseWriter, r *http.Request) {
	var req repoReq
	if !decodeRepoBody(w, r, &req) {
		return
	}
	req.URL, req.Branch, req.Name = strings.TrimSpace(req.URL), strings.TrimSpace(req.Branch), strings.TrimSpace(req.Name)
	if req.Auth == "" {
		req.Auth = "none"
	}
	if err := ansible.ValidateGitURL(req.URL); err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := ansible.ValidateGitBranch(req.Branch); err != nil {
		badRequest(w, err.Error())
		return
	}
	if req.Name == "" { // default: last URL path segment without .git
		p := req.URL
		if u, err := url.Parse(req.URL); err == nil && u.Path != "" {
			p = u.Path
		}
		req.Name = strings.TrimSuffix(path.Base(strings.ReplaceAll(strings.TrimRight(p, "/"), ":", "/")), ".git")
	}
	if !repoNameRe.MatchString(req.Name) {
		badRequest(w, "name must be 1-64 characters: letters, digits, '.', '_' or '-', starting with a letter or digit")
		return
	}
	if req.Auth == "token" && req.Token == "" {
		badRequest(w, "token is required for token authentication")
		return
	}
	if err := s.checkRepoCredentials(req.Auth, req.Token, req.KeyID, req.URL); err != nil {
		badRequest(w, err.Error())
		return
	}
	if !gitAvailable() {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "git binary not found in PATH"})
		return
	}
	existing, err := s.db.ListGitRepos()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	for _, e := range existing {
		if strings.EqualFold(e.Name, req.Name) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": store.ErrGitRepoNameTaken.Error()})
			return
		}
	}
	root, err := s.reposRoot()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	group, err := repoFolderName(req.Group, existing)
	if err == nil {
		err = checkFolderFree(root, group)
	}
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	repo := &store.GitRepo{Name: req.Name, URL: req.URL, Branch: req.Branch, Group: group, Dir: filepath.Join(root, group, req.Name), Auth: req.Auth, Token: req.Token, KeyID: req.KeyID}
	g, err := s.repoGitSource(repo)
	if err != nil {
		badRequest(w, err.Error())
		return
	}

	job, ok := s.repoJobs.start(repo.Name)
	if !ok {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "this repository is already being cloned"})
		return
	}
	// The clone outlives the request: the browser follows it through GET /api/repos/clone/{job}.
	ctx, cancel := context.WithTimeout(context.Background(), repoCloneTimeout)
	job.cancel = cancel
	go s.runRepoClone(ctx, cancel, job, repo, *g)
	writeJSON(w, http.StatusAccepted, map[string]interface{}{"job": job.id})
}

// handleRepoFolder: GET/PUT /api/repos/folder: where new clones are created ("" = default).
func (s *Server) handleRepoFolder(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
	case http.MethodPut:
		var req struct {
			Folder string `json:"folder"`
		}
		if !decodeRepoBody(w, r, &req) {
			return
		}
		f := strings.TrimSpace(req.Folder)
		if f == "" {
			if err := s.db.DeleteSetting(reposDirSetting); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			break
		}
		f = filepath.Clean(f)
		if len(f) > 512 || strings.ContainsRune(f, 0) || !filepath.IsAbs(f) || f == filepath.Dir(f) {
			badRequest(w, "folder must be an absolute path other than the filesystem root")
			return
		}
		if err := os.MkdirAll(f, 0755); err != nil {
			badRequest(w, "folder is not usable: "+err.Error())
			return
		}
		if err := s.db.SetSetting(reposDirSetting, f); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		logger.Info("repos", "repositories", "Repositories folder set to "+f)
	default:
		methodNotAllowed(w)
		return
	}
	root, err := s.reposRoot()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	custom, _ := s.db.GetSetting(reposDirSetting)
	writeJSON(w, http.StatusOK, map[string]interface{}{"folder": root, "custom": custom != ""})
}

// handleRepoRoute serves /api/repos/{folder | id[/sub]}.
func (s *Server) handleRepoRoute(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/repos/"), "/"), "/")
	if parts[0] == "folder" && len(parts) == 1 {
		s.handleRepoFolder(w, r)
		return
	}
	if parts[0] == "clone" && len(parts) == 2 {
		s.handleRepoCloneJob(w, r, parts[1])
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 || len(parts) > 2 {
		http.NotFound(w, r)
		return
	}
	repo, err := s.db.GetGitRepo(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if repo == nil {
		http.NotFound(w, r)
		return
	}
	sub := ""
	if len(parts) == 2 {
		sub = parts[1]
	}
	switch sub {
	case "":
		s.handleRepo(w, r, repo)
	case "sync":
		s.handleRepoSync(w, r, repo)
	case "group":
		s.handleRepoGroup(w, r, repo)
	case "copy":
		s.handleRepoCopy(w, r, repo)
	case "tree":
		s.handleRepoTree(w, r, repo)
	case "search":
		s.handleRepoSearch(w, r, repo)
	case "file":
		s.handleRepoFile(w, r, repo)
	case "discard":
		s.handleRepoDiscard(w, r, repo)
	case "commit":
		s.handleRepoCommit(w, r, repo)
	case "push":
		s.handleRepoPush(w, r, repo)
	default:
		http.NotFound(w, r)
	}
}

// handleRepo: GET detail (status + file list), PUT credentials, DELETE (also removes the folder).
func (s *Server) handleRepo(w http.ResponseWriter, r *http.Request, repo *store.GitRepo) {
	switch r.Method {
	case http.MethodGet:
		ctx, cancel := context.WithTimeout(r.Context(), repoLocalTimeout)
		defer cancel()
		st, err := ansible.LocalRepoStatus(ctx, repo.Dir)
		if err != nil {
			writeGitError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"repo": repoJSON(repo), "branch": st.Branch, "commit": st.Commit, "ahead": st.Ahead, "behind": st.Behind,
			"changes": st.Changes, "changes_truncated": st.Truncated,
		})

	case http.MethodPut:
		var req repoReq
		if !decodeRepoBody(w, r, &req) {
			return
		}
		if req.Auth == "" {
			req.Auth = "none"
		}
		if err := s.checkRepoCredentials(req.Auth, req.Token, req.KeyID, repo.URL); err != nil {
			badRequest(w, err.Error())
			return
		}
		if req.Auth == "token" && req.Token == "" && repo.Token == "" {
			badRequest(w, "token is required for token authentication")
			return
		}
		repo.Auth, repo.Token, repo.KeyID = req.Auth, req.Token, req.KeyID
		if err := s.db.UpdateGitRepoAuth(repo); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		updated, _ := s.db.GetGitRepo(repo.ID)
		if updated == nil {
			updated = repo
		}
		logger.Info("repos", repo.Name, "Updated repository credentials ("+updated.Auth+")")
		writeJSON(w, http.StatusOK, repoJSON(updated))

	case http.MethodDelete:
		if err := removeRepoDir(repo); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if err := s.db.DeleteGitRepo(repo.ID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		logger.Info("repos", repo.Name, fmt.Sprintf("Removed repository %s and its folder %s", repo.Name, repo.Dir))
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})

	default:
		methodNotAllowed(w)
	}
}

// removeRepoDir deletes the clone folder, but only a real clone folder named after the repository.
func removeRepoDir(repo *store.GitRepo) error {
	d := filepath.Clean(repo.Dir)
	if !filepath.IsAbs(d) || filepath.Base(d) != repo.Name {
		return fmt.Errorf("refusing to remove unexpected folder %q", repo.Dir)
	}
	fi, err := os.Lstat(d)
	if os.IsNotExist(err) {
		return nil // already gone
	}
	if err != nil {
		return err
	}
	if st, err := os.Stat(filepath.Join(d, ".git")); !fi.IsDir() || err != nil || !st.IsDir() {
		return fmt.Errorf("refusing to remove %q: not a git clone", repo.Dir)
	}
	if err := os.RemoveAll(d); err != nil {
		return err
	}
	if repo.Group != "" {
		_ = os.Remove(filepath.Dir(d)) // the folder (group) directory, only when this was its last repository
	}
	return nil
}

// handleRepoGroup: PUT {group} moves the repository to another folder (group) under the same location; "" = no folder.
func (s *Server) handleRepoGroup(w http.ResponseWriter, r *http.Request, repo *store.GitRepo) {
	if r.Method != http.MethodPut {
		methodNotAllowed(w)
		return
	}
	var req struct {
		Group string `json:"group"`
	}
	if !decodeRepoBody(w, r, &req) {
		return
	}
	all, err := s.db.ListGitRepos()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	group, err := repoFolderName(req.Group, all)
	location := repoLocation(repo)
	if err == nil && group != repo.Group {
		err = checkFolderFree(location, group)
	}
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if group == repo.Group {
		writeJSON(w, http.StatusOK, repoJSON(repo))
		return
	}
	oldDir, newDir := repo.Dir, filepath.Join(location, group, repo.Name)
	if err := ansible.MoveLocalRepo(oldDir, newDir); err != nil {
		writeGitError(w, err)
		return
	}
	if err := s.db.UpdateGitRepoFolder(repo.ID, group, newDir); err != nil {
		_ = os.Rename(newDir, oldDir) // keep disk and database in step
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if repo.Group != "" {
		_ = os.Remove(filepath.Dir(oldDir)) // the old folder, only when it is now empty
	}
	repo.Group, repo.Dir = group, newDir
	logger.Info("repos", repo.Name, fmt.Sprintf("Moved repository %s to folder %q (%s)", repo.Name, group, newDir))
	writeJSON(w, http.StatusOK, repoJSON(repo))
}

// handleRepoCopy: POST {host_id, path, include_git} copies the working tree (uncommitted edits included, ignored files not)
// into a directory of a Host as a Transfer Job of kind "repo"; existing files with the same name are overwritten and
// nothing is deleted on the host. System directories are refused like in the File Manager (Protected Path).
func (s *Server) handleRepoCopy(w http.ResponseWriter, r *http.Request, repo *store.GitRepo) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		HostID     int64  `json:"host_id"`
		Path       string `json:"path"`
		IncludeGit bool   `json:"include_git"`
	}
	if !decodeRepoBody(w, r, &req) {
		return
	}
	dst, err := transfer.ValidateRepoCopyPath(strings.TrimSpace(req.Path))
	if err == nil && isProtectedPathString(dst) {
		err = fmt.Errorf("%s is a protected system path", dst)
	}
	if err == nil && req.HostID <= 0 {
		err = errors.New("host is required")
	}
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), repoLocalTimeout)
	defer cancel()
	files, total, skipped, err := ansible.CollectLocalRepoCopy(ctx, repo.Dir, req.IncludeGit)
	if err != nil {
		writeGitError(w, err)
		return
	}
	if len(files) == 0 {
		badRequest(w, "the repository has no files to copy")
		return
	}
	note := ""
	if skipped > 0 {
		note = fmt.Sprintf("%d symlink(s) or submodule folder(s) were not copied", skipped)
	}
	label := repo.Name
	if repo.Group != "" {
		label = repo.Group + "/" + repo.Name
	}
	dir := repo.Dir
	job, err := s.transferMgr.StartRepoCopy(transfer.RepoCopy{
		Label: label, Total: total, Note: note, DstHostID: req.HostID, DstPath: dst,
		Write: func(ctx context.Context, w io.Writer) error { return ansible.WriteLocalRepoTar(ctx, w, dir, files) },
	})
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	logger.Info("repos", repo.Name, fmt.Sprintf("Copying repository %s (%d files) to %s on %s", label, len(files), dst, job.DestHostName))
	writeJSON(w, http.StatusAccepted, map[string]interface{}{"job": job, "files": len(files), "skipped": skipped})
}

// repoSource returns the clone's git source or writes the error response.
func (s *Server) repoSource(w http.ResponseWriter, repo *store.GitRepo) (*ansible.GitSource, bool) {
	g, err := s.repoGitSource(repo)
	if err != nil {
		badRequest(w, err.Error())
		return nil, false
	}
	return g, true
}

// handleRepoSync: POST .../sync fetches the current branch and fast-forwards (no merge, no rebase).
func (s *Server) handleRepoSync(w http.ResponseWriter, r *http.Request, repo *store.GitRepo) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	g, ok := s.repoSource(w, repo)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), repoSyncTimeout)
	defer cancel()
	st, err := ansible.PullLocalRepo(ctx, repo.Dir, *g, s.repoProtocols())
	if err != nil {
		writeGitError(w, err)
		return
	}
	_ = s.db.MarkGitRepoSynced(repo.ID)
	logger.Info("repos", repo.Name, fmt.Sprintf("Synced repository %s (branch %s, commit %s)", repo.Name, st.Branch, st.Commit))
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "branch": st.Branch, "commit": st.Commit, "ahead": st.Ahead, "behind": st.Behind})
}

// handleRepoTree: GET ?path= lists one folder of HEAD ("" = root); the browser overlays the working-tree changes.
func (s *Server) handleRepoTree(w http.ResponseWriter, r *http.Request, repo *store.GitRepo) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), repoLocalTimeout)
	defer cancel()
	p := strings.Trim(r.URL.Query().Get("path"), "/")
	entries, truncated, err := ansible.ListLocalRepoDir(ctx, repo.Dir, p)
	if err != nil {
		writeGitError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"path": p, "entries": entries, "truncated": truncated})
}

const repoSearchLimit = 200

// handleRepoSearch: GET ?q= finds files by path across the whole repository, not only the folders that were opened.
func (s *Server) handleRepoSearch(w http.ResponseWriter, r *http.Request, repo *store.GitRepo) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), repoLocalTimeout)
	defer cancel()
	files, truncated, err := ansible.SearchLocalRepoFiles(ctx, repo.Dir, r.URL.Query().Get("q"), repoSearchLimit)
	if err != nil {
		writeGitError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"files": files, "truncated": truncated})
}

// handleRepoFile: GET ?path= read, PUT {path,content} write, DELETE ?path= remove (working tree only).
func (s *Server) handleRepoFile(w http.ResponseWriter, r *http.Request, repo *store.GitRepo) {
	switch r.Method {
	case http.MethodGet:
		p := r.URL.Query().Get("path")
		content, err := ansible.ReadLocalRepoFile(repo.Dir, p)
		if err != nil {
			writeGitError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"path": p, "content": content, "size": len(content)})
	case http.MethodPut:
		var req struct{ Path, Content string }
		if !decodeRepoBody(w, r, &req) {
			return
		}
		if err := ansible.WriteLocalRepoFile(repo.Dir, req.Path, req.Content); err != nil {
			writeGitError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "path": req.Path, "size": len(req.Content)})
	case http.MethodDelete:
		if err := ansible.DeleteLocalRepoFile(repo.Dir, r.URL.Query().Get("path")); err != nil {
			writeGitError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
	default:
		methodNotAllowed(w)
	}
}

// handleRepoDiscard: POST {path} drops the uncommitted change of one file.
func (s *Server) handleRepoDiscard(w http.ResponseWriter, r *http.Request, repo *store.GitRepo) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct{ Path string }
	if !decodeRepoBody(w, r, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), repoLocalTimeout)
	defer cancel()
	if err := ansible.DiscardLocalRepoPath(ctx, repo.Dir, req.Path); err != nil {
		writeGitError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

// handleRepoCommit: POST commits the selected paths (all when empty) and pushes.
func (s *Server) handleRepoCommit(w http.ResponseWriter, r *http.Request, repo *store.GitRepo) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	g, ok := s.repoSource(w, repo)
	if !ok {
		return
	}
	var spec ansible.LocalCommitSpec
	if !decodeRepoBody(w, r, &spec) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), gitEditCommitTimeout)
	defer cancel()
	res, err := ansible.CommitLocalRepo(ctx, repo.Dir, *g, spec, s.repoProtocols())
	if err != nil {
		writeGitError(w, err)
		return
	}
	logger.Info("repos", repo.Name, fmt.Sprintf("Committed and pushed %s on branch %s (%d files changed)", res.Commit, res.Branch, res.Changed))
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "commit": res.Commit, "branch": res.Branch, "pushed": res.Pushed, "changed": res.Changed})
}

// handleRepoPush: POST pushes the current branch, to retry after a rejected push.
func (s *Server) handleRepoPush(w http.ResponseWriter, r *http.Request, repo *store.GitRepo) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	g, ok := s.repoSource(w, repo)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), gitEditCommitTimeout)
	defer cancel()
	res, err := ansible.PushLocalRepo(ctx, repo.Dir, *g, s.repoProtocols())
	if err != nil {
		writeGitError(w, err)
		return
	}
	logger.Info("repos", repo.Name, fmt.Sprintf("Pushed %s on branch %s", res.Commit, res.Branch))
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "commit": res.Commit, "branch": res.Branch, "pushed": true})
}
