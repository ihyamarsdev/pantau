package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"pantau/internal/ansible"
	"pantau/internal/store"
)

func createTestBareRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	bareDir := t.TempDir()
	cmd := exec.Command("git", "init", "--bare", "-b", "main")
	cmd.Dir = bareDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init bare: %v\n%s", err, out)
	}

	seedDir := t.TempDir()
	c1 := exec.Command("git", "clone", bareDir, ".")
	c1.Dir = seedDir
	if out, err := c1.CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v\n%s", err, out)
	}

	files := map[string]string{
		"main.tf":     "resource \"null_resource\" \"x\" {}\n",
		"site.yml":    "- hosts: all\n",
		"README.md":   "# Test\n",
		"bad_bin.dat": "bin\x00ary",
	}
	for name, body := range files {
		full := filepath.Join(seedDir, name)
		_ = os.MkdirAll(filepath.Dir(full), 0755)
		_ = os.WriteFile(full, []byte(body), 0644)
	}

	runGitIn := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = seedDir
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGitIn("add", "-A")
	runGitIn("commit", "-m", "init")
	runGitIn("push", "origin", "main")

	return bareDir
}

func setupLocalBareSeam(srv *Server) {
	srv.gitEditTree = func(ctx context.Context, g ansible.GitSource) (*ansible.RepoTree, error) {
		return ansible.ReadRepoTreeTest(ctx, g, "file")
	}
	srv.gitEditFile = func(ctx context.Context, g ansible.GitSource, path string) (string, string, error) {
		return ansible.ReadRepoFileTest(ctx, g, path, "file")
	}
	srv.gitEditCommit = func(ctx context.Context, g ansible.GitSource, spec ansible.CommitSpec) (*ansible.CommitResult, error) {
		return ansible.CommitAndPushTest(ctx, g, spec, "file")
	}
}

func TestAnsibleGitEditHandlers(t *testing.T) {
	srv, auth := setupAnsibleTestServer(t)
	setupLocalBareSeam(srv)

	// 1. Gate off (409 Conflict per checkAnsibleGate)
	rec := doRequest(t, srv, auth, http.MethodGet, "/api/ansible/playbooks/1/git/files", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("gate off: got %d, want 409", rec.Code)
	}

	_ = srv.db.SetAnsibleEnabled(true)

	// 2. Non-git source (inline) returns 400
	inlineID, err := srv.db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{
		Name:    "Inline Playbook",
		Source:  "inline",
		Content: "---",
	})
	if err != nil {
		t.Fatal(err)
	}

	rec = doRequest(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/ansible/playbooks/%d/git/files", inlineID), nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("inline git files: got %d, want 400", rec.Code)
	}

	rec = doRequest(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/ansible/playbooks/%d/git/file?path=site.yml", inlineID), nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("inline git file: got %d, want 400", rec.Code)
	}

	rec = doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/git/commit", inlineID), map[string]interface{}{
		"message": "commit",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("inline git commit: got %d, want 400", rec.Code)
	}

	// 3. Create a Git playbook pointing to bare repo
	bare := createTestBareRepo(t)
	gitPBID, err := srv.db.CreateAnsiblePlaybook(&store.AnsiblePlaybook{
		Name:      "Git Playbook",
		Source:    "git",
		GitURL:    bare,
		GitBranch: "main",
		GitPath:   "site.yml",
		GitAuth:   "none",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 4. GET /git/files success
	rec = doRequest(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/ansible/playbooks/%d/git/files", gitPBID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /git/files: got %d %s", rec.Code, rec.Body.String())
	}
	treeBody := bodyOf(t, rec)
	commitSHA, ok := treeBody["commit"].(string)
	if !ok || len(commitSHA) != 40 {
		t.Fatalf("unexpected tree commit SHA: %v", treeBody["commit"])
	}
	filesList := treeBody["files"].([]interface{})
	if len(filesList) == 0 {
		t.Fatalf("expected files in tree")
	}

	// 5. GET /git/file success & errors
	rec = doRequest(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/ansible/playbooks/%d/git/file?path=site.yml", gitPBID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /git/file: got %d %s", rec.Code, rec.Body.String())
	}
	fileBody := bodyOf(t, rec)
	if fileBody["content"] != "- hosts: all\n" {
		t.Fatalf("unexpected content: %v", fileBody["content"])
	}

	// Missing path param: 400
	rec = doRequest(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/ansible/playbooks/%d/git/file", gitPBID), nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing path param: got %d, want 400", rec.Code)
	}

	// Not found file: 404
	rec = doRequest(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/ansible/playbooks/%d/git/file?path=nonexistent.yml", gitPBID), nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("nonexistent file: got %d, want 404", rec.Code)
	}

	// Binary file: 415
	rec = doRequest(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/ansible/playbooks/%d/git/file?path=bad_bin.dat", gitPBID), nil)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("binary file: got %d, want 415", rec.Code)
	}

	// 6. POST /git/commit
	// Validation error: empty message -> 400
	rec = doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/git/commit", gitPBID), map[string]interface{}{
		"message":     "",
		"base_commit": commitSHA,
		"changes": []map[string]interface{}{
			{"path": "site.yml", "content": "- hosts: all\n  tasks: []\n"},
		},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty message: got %d, want 400", rec.Code)
	}

	// Validation error: no changes -> 400
	rec = doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/git/commit", gitPBID), map[string]interface{}{
		"message":     "no changes",
		"base_commit": commitSHA,
		"changes":     []map[string]interface{}{},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no changes: got %d, want 400", rec.Code)
	}

	// Successful commit
	rec = doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/git/commit", gitPBID), map[string]interface{}{
		"message":      "Update site.yml from dashboard",
		"author_name":  "Pantau Dev",
		"author_email": "dev@pantau.local",
		"base_commit":  commitSHA,
		"changes": []map[string]interface{}{
			{"path": "site.yml", "content": "- hosts: all\n  tasks: []\n"},
			{"path": "new_playbook.yml", "content": "- hosts: web\n"},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("commit success: got %d %s", rec.Code, rec.Body.String())
	}
	commitBody := bodyOf(t, rec)
	newCommitSHA := commitBody["commit"].(string)
	if newCommitSHA == commitSHA {
		t.Fatalf("commit SHA should change after commit")
	}

	// Check that SetAnsiblePlaybookGitSync was called (sync updated in DB)
	pbUpdated, err := srv.db.GetAnsiblePlaybook(gitPBID)
	if err != nil {
		t.Fatal(err)
	}
	if pbUpdated.LastSyncCommit != newCommitSHA {
		t.Errorf("LastSyncCommit not updated: got %s, want %s", pbUpdated.LastSyncCommit, newCommitSHA)
	}
	if pbUpdated.LastSyncAt == nil || pbUpdated.LastSyncAt.IsZero() {
		t.Errorf("LastSyncAt not set")
	}

	// 7. Stale base_commit -> 409
	rec = doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/git/commit", gitPBID), map[string]interface{}{
		"message":     "Stale commit",
		"base_commit": commitSHA, // stale
		"changes": []map[string]interface{}{
			{"path": "site.yml", "content": "hello"},
		},
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale base_commit: got %d, want 409", rec.Code)
	}

	// 8. Nothing to commit -> 400
	rec = doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/git/commit", gitPBID), map[string]interface{}{
		"message":     "Nothing to commit",
		"base_commit": newCommitSHA,
		"changes": []map[string]interface{}{
			{"path": "site.yml", "content": "- hosts: all\n  tasks: []\n"},
		},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("nothing to commit: got %d, want 400", rec.Code)
	}

	// 9. Error messages must never leak tokens
	srv.gitEditCommit = func(ctx context.Context, g ansible.GitSource, spec ansible.CommitSpec) (*ansible.CommitResult, error) {
		return nil, errors.New("authentication failed for https://x-access-token:secret123@example.com/repo.git")
	}
	rec = doRequest(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/git/commit", gitPBID), map[string]interface{}{
		"message": "test token leak",
		"changes": []map[string]interface{}{{"path": "a.txt", "content": "1"}},
	})
	if strings.Contains(rec.Body.String(), "secret123") {
		t.Fatalf("error body leaked token: %s", rec.Body.String())
	}
}

func TestTerraformGitEditHandlers(t *testing.T) {
	srv, db, _, auth := setupTerraformTestServer(t)
	setupLocalBareSeam(srv)

	// 1. Gate off (403 Forbidden per checkTerraformGate)
	rec := doTFReq(t, srv, auth, http.MethodGet, "/api/terraform/workspaces/1/git/files", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("gate off: got %d, want 403", rec.Code)
	}

	_ = doTFReq(t, srv, auth, http.MethodPut, "/api/terraform/status", map[string]bool{"enabled": true})

	// 2. Non-git workspace (inline) returns 400
	wsInlineID, err := db.CreateTFWorkspace(&store.TFWorkspace{
		Name:        "ws-inline",
		Environment: "dev",
		Binary:      "terraform",
		Source:      "inline",
		Files:       "{}",
	})
	if err != nil {
		t.Fatal(err)
	}

	rec = doTFReq(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/terraform/workspaces/%d/git/files", wsInlineID), nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("inline git files: got %d, want 400", rec.Code)
	}

	rec = doTFReq(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/terraform/workspaces/%d/git/file?path=main.tf", wsInlineID), nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("inline git file: got %d, want 400", rec.Code)
	}

	rec = doTFReq(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/terraform/workspaces/%d/git/commit", wsInlineID), map[string]interface{}{
		"message": "commit",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("inline git commit: got %d, want 400", rec.Code)
	}

	// 3. Git workspace pointing to bare repo
	bare := createTestBareRepo(t)
	wsGitID, err := db.CreateTFWorkspace(&store.TFWorkspace{
		Name:        "ws-git",
		Environment: "prod",
		Binary:      "terraform",
		Source:      "git",
		GitURL:      bare,
		GitBranch:   "main",
		GitAuth:     "none",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 4. GET /git/files success
	rec = doTFReq(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/terraform/workspaces/%d/git/files", wsGitID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /git/files: got %d %s", rec.Code, rec.Body.String())
	}
	var treeBody map[string]interface{}
	decodeBody(t, rec, &treeBody)
	commitSHA, ok := treeBody["commit"].(string)
	if !ok || len(commitSHA) != 40 {
		t.Fatalf("unexpected tree commit SHA: %v", treeBody["commit"])
	}

	// 5. GET /git/file success
	rec = doTFReq(t, srv, auth, http.MethodGet, fmt.Sprintf("/api/terraform/workspaces/%d/git/file?path=main.tf", wsGitID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /git/file: got %d %s", rec.Code, rec.Body.String())
	}
	var fileBody map[string]interface{}
	decodeBody(t, rec, &fileBody)
	if !strings.Contains(fileBody["content"].(string), "null_resource") {
		t.Fatalf("unexpected content: %v", fileBody["content"])
	}

	// 6. POST /git/commit success
	rec = doTFReq(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/terraform/workspaces/%d/git/commit", wsGitID), map[string]interface{}{
		"message":     "Add variables.tf",
		"base_commit": commitSHA,
		"changes": []map[string]interface{}{
			{"path": "variables.tf", "content": "variable \"region\" { default = \"us-east-1\" }\n"},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("commit success: got %d %s", rec.Code, rec.Body.String())
	}
	var commitBody map[string]interface{}
	decodeBody(t, rec, &commitBody)
	if commitBody["ok"] != true || commitBody["pushed"] != true {
		t.Fatalf("unexpected commit body: %+v", commitBody)
	}

	// 7. Stale base_commit -> 409
	rec = doTFReq(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/terraform/workspaces/%d/git/commit", wsGitID), map[string]interface{}{
		"message":     "Stale commit",
		"base_commit": commitSHA, // stale
		"changes": []map[string]interface{}{
			{"path": "main.tf", "content": "# comment"},
		},
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale commit: got %d, want 409", rec.Code)
	}

	// 8. Nothing to commit -> 400
	newCommitSHA := commitBody["commit"].(string)
	rec = doTFReq(t, srv, auth, http.MethodPost, fmt.Sprintf("/api/terraform/workspaces/%d/git/commit", wsGitID), map[string]interface{}{
		"message":     "No changes",
		"base_commit": newCommitSHA,
		"changes": []map[string]interface{}{
			{"path": "variables.tf", "content": "variable \"region\" { default = \"us-east-1\" }\n"},
		},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("nothing to commit: got %d, want 400", rec.Code)
	}
}

// Status codes follow the error type, never the message text: a clone failure whose text happens to contain
// "invalid" (a host such as example.invalid) is a gateway problem, not a bad request.
func TestWriteGitErrorMapsByType(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{errors.New("git clone failed: could not resolve host example.invalid; limit exceeded, cannot connect"), http.StatusBadGateway},
		{fmt.Errorf("change 0: %w", ansible.ErrInvalidInput), http.StatusBadRequest},
		{fmt.Errorf("file: %w", ansible.ErrTooLarge), http.StatusRequestEntityTooLarge},
		{fmt.Errorf("x: %w", ansible.ErrNotText), http.StatusUnsupportedMediaType},
		{fmt.Errorf("read: %w", os.ErrNotExist), http.StatusNotFound},
		{ansible.ErrRepoChanged, http.StatusConflict},
		{ansible.ErrNothingToCommit, http.StatusBadRequest},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		writeGitError(rec, c.err)
		if rec.Code != c.want {
			t.Errorf("%v: status %d, want %d", c.err, rec.Code, c.want)
		}
	}
}
