package ansible

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// A repo that commits "conf" -> <outside dir> must not let a delete reach files outside the clone.
func TestCommitDeleteThroughSymlinkedDirIsRefused(t *testing.T) {
	bare := createBareRepo(t)
	outside := t.TempDir()
	keep := filepath.Join(outside, "pantau.db")
	if err := os.WriteFile(keep, []byte("db"), 0644); err != nil {
		t.Fatal(err)
	}

	seed := t.TempDir()
	gitRun(t, seed, "clone", bare, ".")
	if err := os.Symlink(outside, filepath.Join(seed, "conf")); err != nil {
		t.Fatal(err)
	}
	gitRun(t, seed, "add", "-A")
	gitRun(t, seed, "commit", "-m", "add symlink")
	gitRun(t, seed, "push", "origin", "main")

	ctx := context.Background()
	src := GitSource{URL: bare, Branch: "main"}
	tree, err := readRepoTree(ctx, src, "file")
	if err != nil {
		t.Fatal(err)
	}
	spec := CommitSpec{
		Message: "delete via symlink", AuthorName: "Test Dev", AuthorEmail: "dev@example.com",
		BaseCommit: tree.Commit,
		Changes:    []CommitChange{{Path: "conf/pantau.db", Delete: true}},
	}
	if _, err := commitAndPush(ctx, src, spec, "file"); err == nil {
		t.Fatal("expected delete through a symlinked directory to be refused")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("file outside the clone was removed: %v", err)
	}
}
