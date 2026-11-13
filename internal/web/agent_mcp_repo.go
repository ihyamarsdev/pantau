package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"pantau/internal/store"
)

// Agent tools for the cloned Git repositories (ADR-0067). They call the Repositories handlers in-process (loopback),
// so path validation, symlink checks, the per-folder lock, credential handling and protected paths are the UI's.
// Not offered: clone, delete, credentials, folders (administration) and force or branch operations (the UI has none).

func init() { agentTools = append(agentTools, repoTools...) }

const (
	agentRepoAuthor = "Pantau AI agent"
	agentRepoEmail  = "agent@pantau.local"
)

var (
	pRepo     = map[string]interface{}{"type": "integer", "description": "Repository id from repo_list"}
	pRepoPath = map[string]interface{}{"type": "string", "description": "path relative to the repository root, no leading /"}
)

var repoTools = []agentTool{
	{Name: "repo_list", Description: "List the Git repositories cloned on the Pantau server with branch, commit, ahead/behind the remote and number of uncommitted files.",
		InputSchema: schema(nil, map[string]interface{}{}), run: toolRepoList},
	{Name: "repo_status", Description: "One repository: branch, commit, ahead/behind and the uncommitted files (status per file).",
		InputSchema: schema([]string{"repo_id"}, map[string]interface{}{"repo_id": pRepo}), run: toolRepoStatus},
	{Name: "repo_tree", Description: "List one folder of the repository's HEAD (path empty = root); entries are files and folders.",
		InputSchema: schema([]string{"repo_id"}, map[string]interface{}{"repo_id": pRepo, "path": map[string]interface{}{"type": "string", "description": "folder, default the root"}}), run: toolRepoTree},
	{Name: "repo_search", Description: "Find files by path across the whole repository (tracked and untracked, not ignored); every word of query must match.",
		InputSchema: schema([]string{"repo_id", "query"}, map[string]interface{}{"repo_id": pRepo, "query": str}), run: toolRepoSearch},
	{Name: "repo_read_file", Description: "Read a text file of the working tree (uncommitted edits included), 20 KiB per call starting at byte offset (default 0); the result says when more follows. Secret-looking values are masked.",
		InputSchema: schema([]string{"repo_id", "path"}, map[string]interface{}{"repo_id": pRepo, "path": pRepoPath, "offset": map[string]interface{}{"type": "integer", "description": "byte offset, default 0"}}), run: toolRepoReadFile},
	{Name: "repo_sync", Description: "Fetch the current branch and fast-forward it (no merge, no rebase; refused when it cannot fast-forward). May wait for operator approval.", gate: gateRoutine,
		InputSchema: schema([]string{"repo_id"}, map[string]interface{}{"repo_id": pRepo}), run: toolRepoSync},
	{Name: "repo_write_file", Description: "Write a whole text file (max 1 MiB, folders are created) into the working tree. Nothing is committed: use repo_commit, or repo_discard to undo. May wait for operator approval.", gate: gateRoutine,
		InputSchema: schema([]string{"repo_id", "path", "content"}, map[string]interface{}{"repo_id": pRepo, "path": pRepoPath, "content": map[string]interface{}{"type": "string", "description": "the complete new file content"}}), run: toolRepoWriteFile},
	{Name: "repo_discard", Description: "Drop the uncommitted change of one file (an untracked file is deleted). Always waits for operator approval.", gate: gateHigh,
		InputSchema: schema([]string{"repo_id", "path"}, map[string]interface{}{"repo_id": pRepo, "path": pRepoPath}), run: toolRepoDiscard},
	{Name: "repo_commit", Description: "Commit the given paths (omit for all uncommitted changes) and PUSH to the remote, on the current branch or on new_branch (created from here). No force push. A failed push keeps the commit local: fix the cause, then repo_push. Always waits for operator approval.", gate: gateHigh,
		InputSchema: schema([]string{"repo_id", "message"}, map[string]interface{}{"repo_id": pRepo, "message": str, "paths": map[string]interface{}{"type": "array", "items": str, "description": "files to commit, default all changes"}, "new_branch": str}), run: toolRepoCommit},
	{Name: "repo_push", Description: "Push the current branch (no force), to retry after a failed push. Always waits for operator approval.", gate: gateHigh,
		InputSchema: schema([]string{"repo_id"}, map[string]interface{}{"repo_id": pRepo}), run: toolRepoPush},
	{Name: "repo_copy_to_host", Description: "Copy the repository files (tracked and untracked, not ignored; .git only with include_git) to an absolute folder on a managed host as a Transfer Queue job; symlinks and submodules are skipped, protected system paths refused. Always waits for operator approval.", gate: gateHigh,
		InputSchema: schema([]string{"repo_id", "host_id", "path"}, map[string]interface{}{"repo_id": pRepo, "host_id": pHost, "path": pAbsPath, "include_git": map[string]interface{}{"type": "boolean"}}), run: toolRepoCopy},
}

// agentRepo resolves repo_id and returns the repository with its API path prefix.
func (s *Server) agentRepo(args map[string]interface{}) (*store.GitRepo, string, error) {
	id := argInt(args, "repo_id")
	if id < 1 {
		return nil, "", fmt.Errorf("repo_id (integer) is required")
	}
	repo, err := s.db.GetGitRepo(id)
	if err != nil {
		return nil, "", err
	}
	if repo == nil {
		return nil, "", fmt.Errorf("no repository #%d; see repo_list", id)
	}
	return repo, fmt.Sprintf("/api/repos/%d", id), nil
}

func repoLabel(r *store.GitRepo) string {
	if r.Group != "" {
		return r.Group + "/" + r.Name
	}
	return r.Name
}

func toolRepoList(s *Server, r *http.Request, _ map[string]interface{}) (string, error) {
	m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/repos", nil)
	if err != nil {
		return "", err
	}
	return jsonText(pickAll(m["repos"], "id", "name", "group", "url", "branch", "status", "error", "synced_at"))
}

func toolRepoStatus(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	repo, base, err := s.agentRepo(args)
	if err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodGet, base, nil)
	if err != nil {
		return "", err
	}
	out := pick(m, "branch", "commit", "ahead", "behind", "changes", "changes_truncated")
	out["name"] = repoLabel(repo)
	return jsonText(out)
}

func toolRepoTree(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	_, base, err := s.agentRepo(args)
	if err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodGet, base+"/tree?"+url.Values{"path": {argStr(args, "path")}}.Encode(), nil)
	if err != nil {
		return "", err
	}
	return jsonText(pick(m, "path", "entries", "truncated"))
}

func toolRepoSearch(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	_, base, err := s.agentRepo(args)
	if err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodGet, base+"/search?"+url.Values{"q": {argStr(args, "query")}}.Encode(), nil)
	if err != nil {
		return "", err
	}
	return jsonText(pick(m, "files", "truncated"))
}

func toolRepoReadFile(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	_, base, err := s.agentRepo(args)
	if err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodGet, base+"/file?"+url.Values{"path": {argStr(args, "path")}}.Encode(), nil)
	if err != nil {
		return "", err
	}
	content, _ := m["content"].(string)
	off := argInt(args, "offset")
	if off < 0 || off > int64(len(content)) {
		return "", fmt.Errorf("offset must be between 0 and %d", len(content))
	}
	chunk := content[off:]
	more := len(chunk) > agentReadChunk
	if more {
		chunk = chunk[:agentReadChunk]
	}
	out := redactSecrets(chunk)
	if more {
		out += fmt.Sprintf("\n…[more: call again with offset=%d]", off+agentReadChunk)
	}
	return out, nil
}

func toolRepoSync(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	repo, base, err := s.agentRepo(args)
	if err != nil {
		return "", err
	}
	if err := s.agentGateAs(r, 0, repoLabel(repo), "repo_sync", "fetch and fast-forward repository "+repoLabel(repo), gateRoutine); err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodPost, base+"/sync", nil)
	if err != nil {
		return "", err
	}
	return jsonText(pick(m, "branch", "commit", "ahead", "behind"))
}

func toolRepoWriteFile(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	repo, base, err := s.agentRepo(args)
	if err != nil {
		return "", err
	}
	p := argStr(args, "path")
	content, ok := args["content"].(string) // not argStr: a file's whitespace is content
	if !ok || p == "" {
		return "", fmt.Errorf("path and content are required")
	}
	if err := s.agentGateAs(r, 0, repoLabel(repo), "repo_write_file", fmt.Sprintf("write %s (%d bytes) in repository %s:\n%s", p, len(content), repoLabel(repo), content), gateRoutine); err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodPut, base+"/file", map[string]string{"path": p, "content": content})
	if err != nil {
		return "", err
	}
	return jsonText(pick(m, "path", "size"))
}

func toolRepoDiscard(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	repo, base, err := s.agentRepo(args)
	if err != nil {
		return "", err
	}
	p := argStr(args, "path")
	if err := s.agentGateAs(r, 0, repoLabel(repo), "repo_discard", fmt.Sprintf("discard the uncommitted change of %s in repository %s", p, repoLabel(repo)), gateHigh); err != nil {
		return "", err
	}
	if _, err := s.loopJSON(r.Context(), http.MethodPost, base+"/discard", map[string]string{"path": p}); err != nil {
		return "", err
	}
	return "discarded " + p, nil
}

func toolRepoCommit(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	repo, base, err := s.agentRepo(args)
	if err != nil {
		return "", err
	}
	msg := argStr(args, "message")
	if msg == "" {
		return "", fmt.Errorf("message is required")
	}
	paths := []string{}
	raw, _ := args["paths"].([]interface{})
	for _, v := range raw {
		p, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("paths must be strings")
		}
		paths = append(paths, p)
	}
	branch := argStr(args, "new_branch")
	what := "all uncommitted changes"
	if len(paths) > 0 {
		what = strings.Join(paths, ", ")
	}
	target := "the current branch"
	if branch != "" {
		target = "new branch " + branch
	}
	summary := fmt.Sprintf("commit %s in repository %s and push to %s of the remote\nmessage: %s", what, repoLabel(repo), target, msg)
	if err := s.agentGateAs(r, 0, repoLabel(repo), "repo_commit", summary, gateHigh); err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodPost, base+"/commit", map[string]interface{}{
		"message": msg, "author_name": agentRepoAuthor, "author_email": agentRepoEmail, "new_branch": branch, "paths": paths})
	if err != nil {
		return "", err
	}
	return jsonText(pick(m, "commit", "branch", "pushed", "changed"))
}

func toolRepoPush(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	repo, base, err := s.agentRepo(args)
	if err != nil {
		return "", err
	}
	if err := s.agentGateAs(r, 0, repoLabel(repo), "repo_push", "push the current branch of repository "+repoLabel(repo)+" to the remote", gateHigh); err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodPost, base+"/push", nil)
	if err != nil {
		return "", err
	}
	return jsonText(pick(m, "commit", "branch", "pushed"))
}

func toolRepoCopy(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	repo, base, err := s.agentRepo(args)
	if err != nil {
		return "", err
	}
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	p := argStr(args, "path")
	git, _ := args["include_git"].(bool)
	summary := fmt.Sprintf("copy repository %s to %s (include .git: %t)", repoLabel(repo), p, git)
	if err := s.agentGate(r, id, "repo_copy_to_host", summary, gateHigh); err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodPost, base+"/copy", map[string]interface{}{"host_id": id, "path": p, "include_git": git})
	if err != nil {
		return "", err
	}
	job, _ := m["job"].(map[string]interface{})
	return jsonText(map[string]interface{}{"files": m["files"], "skipped": m["skipped"], "job": pick(job, "id", "status", "dest_host_name", "dest_path", "note")})
}
