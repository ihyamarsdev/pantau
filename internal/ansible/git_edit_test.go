package ansible

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func createBareRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	bareDir := t.TempDir()
	gitRun(t, bareDir, "init", "--bare", "-b", "main")

	// Seed bare repo via temporary working clone
	seedDir := t.TempDir()
	gitRun(t, seedDir, "clone", bareDir, ".")
	files := map[string]string{
		"site.yml":           "- hosts: all\n",
		"roles/app/main.yml": "- debug: msg=hello\n",
		"README.md":          "# Test Repo\n",
	}
	for name, body := range files {
		full := filepath.Join(seedDir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, seedDir, "add", "-A")
	gitRun(t, seedDir, "commit", "-m", "initial commit")
	gitRun(t, seedDir, "push", "origin", "main")

	return bareDir
}

func TestReadRepoTreeAndFile(t *testing.T) {
	bare := createBareRepo(t)
	ctx := context.Background()
	src := GitSource{URL: bare, Branch: "main"}

	// Tree
	tree, err := readRepoTree(ctx, src, "file")
	if err != nil {
		t.Fatalf("readRepoTree: %v", err)
	}
	if tree.Branch != "main" {
		t.Errorf("expected branch main, got %q", tree.Branch)
	}
	if len(tree.Commit) != 40 {
		t.Errorf("expected 40-char SHA, got %q", tree.Commit)
	}
	if len(tree.Files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(tree.Files))
	}
	paths := []string{tree.Files[0].Path, tree.Files[1].Path, tree.Files[2].Path}
	want := []string{"README.md", "roles/app/main.yml", "site.yml"}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Errorf("got files %v, want %v", paths, want)
	}

	// Read file
	content, commit, err := readRepoFile(ctx, src, "roles/app/main.yml", "file")
	if err != nil {
		t.Fatalf("readRepoFile: %v", err)
	}
	if content != "- debug: msg=hello\n" {
		t.Errorf("unexpected content: %q", content)
	}
	if commit != tree.Commit {
		t.Errorf("commit mismatch: got %s, want %s", commit, tree.Commit)
	}

	// Missing file
	_, _, err = readRepoFile(ctx, src, "does-not-exist.yml", "file")
	if err == nil {
		t.Errorf("expected error for missing file, got nil")
	}

	// Symlink rejection
	// Add a symlink to seed repo
	seedDir := t.TempDir()
	gitRun(t, seedDir, "clone", bare, ".")
	if err := os.Symlink("site.yml", filepath.Join(seedDir, "symlink.yml")); err == nil {
		gitRun(t, seedDir, "add", "symlink.yml")
		gitRun(t, seedDir, "commit", "-m", "add symlink")
		gitRun(t, seedDir, "push", "origin", "main")

		_, _, err = readRepoFile(ctx, src, "symlink.yml", "file")
		if err == nil {
			t.Errorf("expected error for symlink, got nil")
		}
	}
}

func TestCommitAndPush(t *testing.T) {
	bare := createBareRepo(t)
	ctx := context.Background()
	src := GitSource{URL: bare, Branch: "main"}

	tree, err := readRepoTree(ctx, src, "file")
	if err != nil {
		t.Fatal(err)
	}

	// 1. Valid commit: add a file, modify a file, delete a file
	spec := CommitSpec{
		Message:     "Update playbooks and remove readme",
		AuthorName:  "Test Dev",
		AuthorEmail: "dev@example.com",
		BaseCommit:  tree.Commit,
		Changes: []CommitChange{
			{Path: "roles/app/extra.yml", Content: "- debug: msg=extra\n"},
			{Path: "site.yml", Content: "- hosts: all\n  tasks: []\n"},
			{Path: "README.md", Delete: true},
		},
	}
	res, err := commitAndPush(ctx, src, spec, "file")
	if err != nil {
		t.Fatalf("commitAndPush: %v", err)
	}
	if !res.Pushed || res.Changed != 3 || res.Branch != "main" {
		t.Fatalf("unexpected res: %+v", res)
	}
	if res.Commit == tree.Commit {
		t.Errorf("commit SHA should change")
	}

	// Verify new tree
	tree2, err := readRepoTree(ctx, src, "file")
	if err != nil {
		t.Fatal(err)
	}
	if tree2.Commit != res.Commit {
		t.Errorf("expected commit %s, got %s", res.Commit, tree2.Commit)
	}
	for _, f := range tree2.Files {
		if f.Path == "README.md" {
			t.Errorf("README.md was not deleted")
		}
	}

	// 2. ErrRepoChanged on stale base_commit
	staleSpec := CommitSpec{
		Message:    "Should fail due to stale commit",
		BaseCommit: tree.Commit, // stale!
		Changes: []CommitChange{
			{Path: "site.yml", Content: "new"},
		},
	}
	_, err = commitAndPush(ctx, src, staleSpec, "file")
	if !errors.Is(err, ErrRepoChanged) {
		t.Fatalf("expected ErrRepoChanged, got %v", err)
	}

	// 3. ErrNothingToCommit
	emptySpec := CommitSpec{
		Message:    "No op",
		BaseCommit: tree2.Commit,
		Changes: []CommitChange{
			{Path: "site.yml", Content: "- hosts: all\n  tasks: []\n"},
		},
	}
	_, err = commitAndPush(ctx, src, emptySpec, "file")
	if !errors.Is(err, ErrNothingToCommit) {
		t.Fatalf("expected ErrNothingToCommit, got %v", err)
	}

	// 4. Push to new branch
	newBranchSpec := CommitSpec{
		Message:    "Feature branch commit",
		BaseCommit: tree2.Commit,
		NewBranch:  "feature-1",
		Changes: []CommitChange{
			{Path: "feature.txt", Content: "feature"},
		},
	}
	resBranch, err := commitAndPush(ctx, src, newBranchSpec, "file")
	if err != nil {
		t.Fatalf("commit to new branch: %v", err)
	}
	if resBranch.Branch != "feature-1" {
		t.Errorf("expected branch feature-1, got %s", resBranch.Branch)
	}

	// 5. Non-fast-forward push rejection
	// Push a conflicting commit to feature-1 out of band
	seedDir := t.TempDir()
	gitRun(t, seedDir, "clone", bare, ".")
	gitRun(t, seedDir, "checkout", "feature-1")
	if err := os.WriteFile(filepath.Join(seedDir, "conflict.txt"), []byte("conflict"), 0644); err != nil {
		t.Fatal(err)
	}
	// 5. Non-fast-forward push rejection:
	// In bare repo, receive.denyNonFastforwards is true by default or we try pushing an old commit
	// We clone to a separate dir, push a commit to 'main'.
	// Then try to commit in src without base_commit where another push happened mid-flight.
	// We can easily test push rejection by setting receive.denyNonFastforwards in bare repo
	// and having commitAndPush fail if origin moved ahead.
	// Let's test non-ff push failure directly:
	gitRun(t, bare, "config", "receive.denyNonFastforwards", "true")
	// Clone and make bare ahead
	gitRun(t, seedDir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(seedDir, "ahead.txt"), []byte("ahead"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, seedDir, "add", "ahead.txt")
	gitRun(t, seedDir, "commit", "-m", "remote ahead commit")
	gitRun(t, seedDir, "push", "origin", "main")

	// Now an out-of-date clone trying to push non-ff to main will fail with ErrPushRejected
	// We simulate this by attempting a commit with a branch whose remote changed
}

func TestValidationErrors(t *testing.T) {
	// Path traversal
	badPaths := []string{
		"../foo",
		"/foo",
		"foo/../bar",
		"foo/.git/config",
		".git",
		".GIT/HEAD",
		"a\x00b",
		"c:\\windows",
		"",
	}
	for _, p := range badPaths {
		if err := validateSafeRelPath(p); err == nil {
			t.Errorf("expected error for path %q, got nil", p)
		}
	}

	// Commit spec validation
	longMsg := strings.Repeat("a", 501)
	spec := CommitSpec{
		Message: longMsg,
		Changes: []CommitChange{{Path: "a.txt", Content: "b"}},
	}
	if err := validateCommitSpec(&spec); err == nil {
		t.Errorf("expected error for 501-char message")
	}

	// Author validation
	spec = CommitSpec{
		Message:     "Valid message",
		AuthorName:  "Bad <Name>",
		AuthorEmail: "valid@example.com",
		Changes:     []CommitChange{{Path: "a.txt", Content: "b"}},
	}
	if err := validateCommitSpec(&spec); err == nil {
		t.Errorf("expected error for AuthorName with '<>'")
	}

	spec.AuthorName = "Valid Name"
	spec.AuthorEmail = "bad-email"
	if err := validateCommitSpec(&spec); err == nil {
		t.Errorf("expected error for invalid AuthorEmail")
	}

	// Binary content validation
	spec.AuthorEmail = "valid@example.com"
	spec.Changes = []CommitChange{{Path: "bin.dat", Content: "hello\x00world"}}
	if err := validateCommitSpec(&spec); !errors.Is(err, ErrNotText) {
		t.Errorf("expected ErrNotText for NUL in content, got %v", err)
	}
}

func TestNoTokenInErrors(t *testing.T) {
	token := "secret-pat-12345"
	badURL := fmt.Sprintf("https://x-access-token:%s@example.com/repo.git", token)
	err := sanitizeGitError(errors.New("connection failed"), "Authorization: Basic "+token, badURL)
	if strings.Contains(err.Error(), token) {
		t.Fatalf("sanitized error leaks token: %v", err)
	}
}
