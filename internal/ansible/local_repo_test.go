package ansible

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func cloneForTest(t *testing.T, bare string) (string, GitSource) {
	t.Helper()
	g := GitSource{URL: bare, Branch: "main"}
	dir := filepath.Join(t.TempDir(), "repos", "r1")
	if _, _, err := cloneLocalRepo(context.Background(), g, dir, "file", nil); err != nil {
		t.Fatalf("clone: %v", err)
	}
	return dir, g
}

func bareLog(t *testing.T, bare, ref string) string {
	t.Helper()
	out, err := exec.Command("git", "--git-dir", bare, "log", "--format=%an <%ae> %s", "-1", ref).CombinedOutput()
	if err != nil {
		t.Fatalf("bare log %s: %v\n%s", ref, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestLocalRepoEditCommitPush(t *testing.T) {
	bare := createBareRepo(t)
	dir, g := cloneForTest(t, bare)
	ctx := context.Background()

	st, err := LocalRepoStatus(ctx, dir)
	if err != nil || st.Branch != "main" || len(st.Commit) != 40 || len(st.Changes) != 0 {
		t.Fatalf("clean status = %+v, %v", st, err)
	}

	if err := WriteLocalRepoFile(dir, "new/dir/a.tf", "a\n"); err != nil {
		t.Fatal(err)
	}
	if err := WriteLocalRepoFile(dir, "README.md", "# changed\n"); err != nil {
		t.Fatal(err)
	}
	if err := DeleteLocalRepoFile(dir, "site.yml"); err != nil {
		t.Fatal(err)
	}
	if err := WriteLocalRepoFile(dir, "left.txt", "stays dirty\n"); err != nil {
		t.Fatal(err)
	}
	st, _ = LocalRepoStatus(ctx, dir)
	got := map[string]string{}
	for _, c := range st.Changes {
		got[c.Path] = c.Status
	}
	want := map[string]string{"new/dir/a.tf": "new", "README.md": "modified", "site.yml": "deleted", "left.txt": "new"}
	for p, s := range want {
		if got[p] != s {
			t.Errorf("change %s = %q, want %q (all: %v)", p, got[p], s, got)
		}
	}

	// Folders list from HEAD (the caller overlays status): the deleted site.yml is still listed, untracked files are not.
	names := func(es []LocalDirEntry) string {
		var out []string
		for _, e := range es {
			if e.Dir {
				out = append(out, e.Name+"/")
			} else {
				out = append(out, e.Name)
			}
		}
		return strings.Join(out, ",")
	}
	root, trunc, err := ListLocalRepoDir(ctx, dir, "")
	if err != nil || trunc || names(root) != "roles/,README.md,site.yml" {
		t.Errorf("root = %s trunc=%v err=%v", names(root), trunc, err)
	}
	if sub, _, err := ListLocalRepoDir(ctx, dir, "roles/app"); err != nil || names(sub) != "main.yml" || sub[0].Size == 0 {
		t.Errorf("roles/app = %v err=%v", sub, err)
	}
	if sub, _, err := ListLocalRepoDir(ctx, dir, "new/dir"); err != nil || len(sub) != 0 {
		t.Errorf("folder only in the working tree must list empty, got %v err=%v", sub, err)
	}
	if _, _, err := ListLocalRepoDir(ctx, dir, "../x"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("unsafe folder: got %v", err)
	}
	found, _, err := SearchLocalRepoFiles(ctx, dir, "APP main", 10)
	if err != nil || len(found) != 1 || found[0].Path != "roles/app/main.yml" {
		t.Errorf("search APP main = %v err=%v", found, err)
	}
	if found, _, _ := SearchLocalRepoFiles(ctx, dir, "left", 10); len(found) != 1 || found[0].Path != "left.txt" {
		t.Errorf("untracked files must be searchable, got %v", found)
	}
	if found, _, _ := SearchLocalRepoFiles(ctx, dir, "site", 10); len(found) != 0 {
		t.Errorf("a file deleted from the working tree must not be found, got %v", found)
	}
	if body, err := ReadLocalRepoFile(dir, "README.md"); err != nil || body != "# changed\n" {
		t.Errorf("read = %q, %v", body, err)
	}

	// Only the selected paths are committed; left.txt stays uncommitted.
	res, err := CommitLocalRepo(ctx, dir, g, LocalCommitSpec{
		Message: "edit", AuthorName: "Ann", AuthorEmail: "ann@x.io",
		Paths: []string{"new/dir/a.tf", "README.md", "site.yml"},
	}, "file")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if res.Branch != "main" || !res.Pushed || res.Changed != 3 || len(res.Commit) != 40 {
		t.Errorf("result = %+v", res)
	}
	if l := bareLog(t, bare, "main"); l != "Ann <ann@x.io> edit" {
		t.Errorf("remote head = %q", l)
	}
	st, _ = LocalRepoStatus(ctx, dir)
	if len(st.Changes) != 1 || st.Changes[0].Path != "left.txt" || st.Ahead != 0 {
		t.Errorf("after commit status = %+v", st)
	}

	// Empty paths = everything; default author applies.
	if _, err := CommitLocalRepo(ctx, dir, g, LocalCommitSpec{Message: "rest"}, "file"); err != nil {
		t.Fatalf("commit all: %v", err)
	}
	if l := bareLog(t, bare, "main"); l != "Pantau <pantau@localhost> rest" {
		t.Errorf("remote head = %q", l)
	}
	if _, err := CommitLocalRepo(ctx, dir, g, LocalCommitSpec{Message: "again"}, "file"); !errors.Is(err, ErrNothingToCommit) {
		t.Errorf("want ErrNothingToCommit, got %v", err)
	}
}

func TestLocalRepoNewBranchAndDiscard(t *testing.T) {
	bare := createBareRepo(t)
	dir, g := cloneForTest(t, bare)
	ctx := context.Background()

	_ = WriteLocalRepoFile(dir, "README.md", "x\n")
	_ = WriteLocalRepoFile(dir, "tmp.txt", "x\n")
	if err := DiscardLocalRepoPath(ctx, dir, "README.md"); err != nil {
		t.Fatal(err)
	}
	if err := DiscardLocalRepoPath(ctx, dir, "tmp.txt"); err != nil {
		t.Fatal(err)
	}
	if st, _ := LocalRepoStatus(ctx, dir); len(st.Changes) != 0 {
		t.Fatalf("discard left %+v", st.Changes)
	}
	_ = os.Remove(filepath.Join(dir, "site.yml"))
	if err := DiscardLocalRepoPath(ctx, dir, "site.yml"); err != nil {
		t.Fatalf("restore deleted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "site.yml")); err != nil {
		t.Errorf("deleted file not restored: %v", err)
	}

	_ = WriteLocalRepoFile(dir, "feature.tf", "f\n")
	if _, err := CommitLocalRepo(ctx, dir, g, LocalCommitSpec{Message: "m", NewBranch: "main"}, "file"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("same branch must be invalid input, got %v", err)
	}
	res, err := CommitLocalRepo(ctx, dir, g, LocalCommitSpec{Message: "feat", NewBranch: "feature/x"}, "file")
	if err != nil || res.Branch != "feature/x" {
		t.Fatalf("new branch commit = %+v, %v", res, err)
	}
	if l := bareLog(t, bare, "feature/x"); !strings.HasSuffix(l, "feat") {
		t.Errorf("remote feature head = %q", l)
	}
	if st, _ := LocalRepoStatus(ctx, dir); st.Branch != "feature/x" || st.Ahead != 0 {
		t.Errorf("status on new branch = %+v (upstream must be set by push -u)", st)
	}
}

func TestLocalRepoPullAndRejectedPush(t *testing.T) {
	bare := createBareRepo(t)
	dir, g := cloneForTest(t, bare)
	ctx := context.Background()

	// Someone else pushes first.
	other := t.TempDir()
	gitRun(t, other, "clone", bare, ".")
	_ = os.WriteFile(filepath.Join(other, "remote.txt"), []byte("r\n"), 0644)
	gitRun(t, other, "add", "-A")
	gitRun(t, other, "commit", "-m", "remote change")
	gitRun(t, other, "push", "origin", "main")

	st, err := PullLocalRepo(ctx, dir, g, "file")
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "remote.txt")); err != nil || st.Behind != 0 {
		t.Errorf("pull did not fast-forward: %v %+v", err, st)
	}

	// Remote moves again; our commit then diverges and the push is rejected, but the commit is kept locally.
	_ = os.WriteFile(filepath.Join(other, "remote2.txt"), []byte("r\n"), 0644)
	gitRun(t, other, "add", "-A")
	gitRun(t, other, "commit", "-m", "remote change 2")
	gitRun(t, other, "push", "origin", "main")

	_ = WriteLocalRepoFile(dir, "mine.txt", "m\n")
	_, err = CommitLocalRepo(ctx, dir, g, LocalCommitSpec{Message: "mine"}, "file")
	if !errors.Is(err, ErrPushRejected) {
		t.Fatalf("want ErrPushRejected, got %v", err)
	}
	if st, _ := LocalRepoStatus(ctx, dir); st.Ahead != 1 || len(st.Changes) != 0 {
		t.Errorf("rejected push must keep the local commit: %+v", st)
	}
	if _, err := PushLocalRepo(ctx, dir, g, "file"); !errors.Is(err, ErrPushRejected) {
		t.Errorf("retry push: want ErrPushRejected, got %v", err)
	}
	if _, err := PullLocalRepo(ctx, dir, g, "file"); err == nil {
		t.Error("diverged pull must fail, never merge")
	}
}

func TestLocalRepoRejectsUnsafePaths(t *testing.T) {
	bare := createBareRepo(t)
	dir, g := cloneForTest(t, bare)
	ctx := context.Background()

	for _, p := range []string{"../x", "/etc/passwd", ".git/config", "a/../b", "a/.GIT/x", ""} {
		if err := WriteLocalRepoFile(dir, p, "x"); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("write %q: want ErrInvalidInput, got %v", p, err)
		}
		if _, err := ReadLocalRepoFile(dir, p); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("read %q: want ErrInvalidInput, got %v", p, err)
		}
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Skip("symlinks unavailable")
	}
	if err := WriteLocalRepoFile(dir, "link/evil.txt", "x"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("write through symlink: got %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "evil.txt")); err == nil {
		t.Error("file escaped the clone through a symlink")
	}
	if err := WriteLocalRepoFile(dir, "bin", "a\x00b"); !errors.Is(err, ErrNotText) {
		t.Errorf("NUL content: got %v", err)
	}
	if err := WriteLocalRepoFile(dir, "big", strings.Repeat("x", maxFileSize+1)); !errors.Is(err, ErrTooLarge) {
		t.Errorf("oversize: got %v", err)
	}
	// Pathspec magic must be literal, not interpreted.
	_ = WriteLocalRepoFile(dir, "real.txt", "x\n")
	if _, err := CommitLocalRepo(ctx, dir, g, LocalCommitSpec{Message: "m", Paths: []string{":(glob)**"}}, "file"); err == nil {
		t.Error("magic pathspec was interpreted instead of taken literally")
	}
	if st, _ := LocalRepoStatus(ctx, dir); st.Ahead != 0 || len(st.Changes) != 2 {
		t.Errorf("magic pathspec committed something: %+v", st)
	}

	missing := filepath.Join(t.TempDir(), "nope")
	if _, err := LocalRepoStatus(ctx, missing); !errors.Is(err, ErrRepoMissing) {
		t.Errorf("missing repo: got %v", err)
	}
	// A plain folder inside another repo is not a clone either: git would otherwise walk up to the parent repo.
	sub := filepath.Join(dir, "sub")
	_ = os.MkdirAll(sub, 0755)
	if _, err := LocalRepoStatus(ctx, sub); !errors.Is(err, ErrRepoMissing) {
		t.Errorf("subfolder of a repo: got %v", err)
	}
}

func TestCloneLocalRepoValidationAndCleanup(t *testing.T) {
	ctx := context.Background()
	for _, g := range []GitSource{
		{URL: "/etc"}, {URL: "file:///tmp/x"}, {URL: "https://h/r.git", Branch: "--upload-pack=x"},
		{URL: "git@h:o/r.git", Token: "t"},
	} {
		if _, _, err := CloneLocalRepo(ctx, g, filepath.Join(t.TempDir(), "x"), nil); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%+v: want ErrInvalidInput, got %v", g, err)
		}
	}
	dir := filepath.Join(t.TempDir(), "gone")
	if _, _, err := cloneLocalRepo(ctx, GitSource{URL: filepath.Join(t.TempDir(), "no-such-bare")}, dir, "file", nil); err == nil {
		t.Fatal("clone of a missing repo must fail")
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("failed clone left a partial folder behind")
	}
}

func TestCloneLocalRepoStreamsProgress(t *testing.T) {
	bare := createBareRepo(t)
	var log strings.Builder
	dir := filepath.Join(t.TempDir(), "r")
	if _, _, err := cloneLocalRepo(context.Background(), GitSource{URL: bare, Branch: "main"}, dir, "file", &log); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "Cloning into") {
		t.Errorf("clone output not streamed to the log writer: %q", log.String())
	}
}

func TestLocalRepoBigFolderIsListedAndSearchedBeyondTheCap(t *testing.T) {
	bare := createBareRepo(t)
	dir, g := cloneForTest(t, bare)
	ctx := context.Background()
	for i := 0; i < maxTreeEntries+100; i++ {
		if err := WriteLocalRepoFile(dir, fmt.Sprintf("big/f%04d.txt", i), "x\n"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := CommitLocalRepo(ctx, dir, g, LocalCommitSpec{Message: "many"}, "file"); err != nil {
		t.Fatal(err)
	}
	es, truncated, err := ListLocalRepoDir(ctx, dir, "big")
	if err != nil || len(es) != maxTreeEntries || !truncated {
		t.Fatalf("big folder: %d entries truncated=%v err=%v", len(es), truncated, err)
	}
	found, _, err := SearchLocalRepoFiles(ctx, dir, "f2099", 10)
	if err != nil || len(found) != 1 || found[0].Path != "big/f2099.txt" {
		t.Errorf("a file past the folder cap must still be found, got %v err=%v", found, err)
	}
	if found, truncated, _ := SearchLocalRepoFiles(ctx, dir, "f0", 5); len(found) != 5 || !truncated {
		t.Errorf("search cap: %d results truncated=%v", len(found), truncated)
	}
}

func TestLocalRepoCopyArchive(t *testing.T) {
	bare := createBareRepo(t)
	dir, _ := cloneForTest(t, bare)
	ctx := context.Background()
	_ = WriteLocalRepoFile(dir, "new/dir/a.tf", "a\n")     // untracked: copied
	_ = WriteLocalRepoFile(dir, "README.md", "# edited\n") // uncommitted edit: copied as edited
	_ = DeleteLocalRepoFile(dir, "site.yml")               // deleted: left out
	_ = os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored.txt\n"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("x"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/sh\n"), 0755)
	if err := os.Symlink("/etc", filepath.Join(dir, "link")); err != nil {
		t.Skip("symlinks unavailable")
	}

	read := func(includeGit bool) (map[string]string, map[string]int64, int, int64) {
		files, total, skipped, err := CollectLocalRepoCopy(ctx, dir, includeGit)
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if err := WriteLocalRepoTar(ctx, &buf, dir, files); err != nil {
			t.Fatal(err)
		}
		if total < int64(buf.Len())-1024 || total > int64(buf.Len())+1024 {
			t.Errorf("progress total %d is far from the archive size %d", total, buf.Len())
		}
		got, modes := map[string]string{}, map[string]int64{}
		tr := tar.NewReader(&buf)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(tr)
			got[h.Name], modes[h.Name] = string(b), h.Mode
		}
		return got, modes, skipped, total
	}

	got, modes, skipped, _ := read(false)
	if got["README.md"] != "# edited\n" || got["new/dir/a.tf"] != "a\n" || got["roles/app/main.yml"] == "" {
		t.Errorf("archive content = %v", got)
	}
	if _, ok := got["site.yml"]; ok {
		t.Error("a file deleted from the working tree must not be copied")
	}
	if _, ok := got["ignored.txt"]; ok {
		t.Error("ignored files must not be copied")
	}
	if _, ok := got["link"]; ok || skipped != 1 {
		t.Errorf("the symlink must be skipped and counted: skipped=%d", skipped)
	}
	if modes["run.sh"] != 0755 {
		t.Errorf("exec bit lost: %o", modes["run.sh"])
	}
	for name := range got {
		if strings.HasPrefix(name, ".git/") {
			t.Errorf("%s: .git is only copied when asked for", name)
		}
	}
	withGit, _, _, _ := read(true)
	if withGit[".git/HEAD"] == "" {
		t.Error("includeGit must carry .git/HEAD")
	}

	// A file that disappears between listing and sending aborts the copy.
	files, _, _, _ := CollectLocalRepoCopy(ctx, dir, false)
	_ = os.Remove(filepath.Join(dir, "README.md"))
	if err := WriteLocalRepoTar(ctx, io.Discard, dir, files); err == nil {
		t.Error("a vanished file must abort the archive")
	}
}
