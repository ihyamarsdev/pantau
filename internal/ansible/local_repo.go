package ansible

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Persistent working clones (the Repositories section). Unlike git_edit.go, which shallow-clones into a temp dir on
// every request, these clones live on disk: edits are written straight to the working tree, and commit/push act on it.
// The HTTPS token or SSH key in GitSource must have WRITE permission on the remote; a read-only credential makes push
// fail with ErrPushRejected (the local commit is kept, so it can be pushed again after fixing the credential).
// Credentials never touch the clone: they are injected through the environment of each git process and the SSH key
// is written to a temp dir that is removed right after the call.

// protocols is the GIT_ALLOW_PROTOCOL value ("https:ssh" in production; tests pass "file").

var ErrRepoMissing = invalidf("repository folder is missing or is not a git clone")

const maxLocalCommitPaths = 500

// LocalChange is one working-tree change; Status is "new", "modified" or "deleted".
type LocalChange struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

// LocalStatus is the working-tree state of a clone. Ahead/Behind are relative to the last fetched upstream.
type LocalStatus struct {
	Branch    string        `json:"branch"`
	Commit    string        `json:"commit"`
	Ahead     int           `json:"ahead"`
	Behind    int           `json:"behind"`
	Changes   []LocalChange `json:"changes"`
	Truncated bool          `json:"truncated"`
}

// LocalCommitSpec commits Paths (empty = every change) and pushes to the current branch, or to NewBranch.
type LocalCommitSpec struct {
	Message     string   `json:"message"`
	AuthorName  string   `json:"author_name"`
	AuthorEmail string   `json:"author_email"`
	NewBranch   string   `json:"new_branch"`
	Paths       []string `json:"paths"`
}

type localGit struct {
	bin, dir, url, work string
	env                 []string
}

// openLocalGit prepares a git runner for the clone at dir. g == nil means no credentials (offline commands).
func openLocalGit(dir string, g *GitSource, protocols string) (*localGit, error) {
	if st, err := os.Stat(filepath.Join(dir, ".git")); err != nil || !st.IsDir() {
		return nil, ErrRepoMissing
	}
	bin, err := exec.LookPath("git")
	if err != nil {
		return nil, errors.New("git not found in PATH")
	}
	work, err := os.MkdirTemp("", "pantau-repo-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary workspace: %w", err)
	}
	var src GitSource
	if g != nil {
		src = *g
	}
	env, err := setupGitEnv(src.URL, src.Token, src.PrivateKey, work, protocols)
	if err != nil {
		os.RemoveAll(work)
		return nil, err
	}
	// Paths come from API clients: never let git read them as pathspec magic.
	env = append(env, "GIT_LITERAL_PATHSPECS=1")
	url := src.URL
	if url == "" {
		url = "\x00" // sanitizeGitError must not replace the empty string
	}
	return &localGit{bin: bin, dir: dir, url: url, work: work, env: env}, nil
}

func (l *localGit) close() { os.RemoveAll(l.work) }

// run executes `git <args>` in the clone and returns stdout; args[0] must be the subcommand. Errors are sanitized.
func (l *localGit) run(ctx context.Context, extraEnv []string, args ...string) (string, error) {
	full := append([]string{"-C", l.dir, "-c", "credential.helper=", "-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false"}, args...)
	cmd := exec.Command(l.bin, full...)
	cmd.Env = append(append([]string(nil), l.env...), extraEnv...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	code, err := runCmd(ctx, cmd)
	if err == nil && code != 0 {
		err = fmt.Errorf("git %s failed (exit %d)", args[0], code)
	}
	if err != nil {
		return "", sanitizeGitError(err, errb.String(), l.url)
	}
	return out.String(), nil
}

// MoveLocalRepo renames the clone at oldDir to newDir (same filesystem, parent folders created). newDir must not exist.
func MoveLocalRepo(oldDir, newDir string) error {
	lock := getBranchLock(oldDir, "")
	lock.Lock()
	defer lock.Unlock()
	if err := requireLocalRepo(oldDir); err != nil {
		return err
	}
	if _, err := os.Lstat(newDir); err == nil {
		return invalidf("folder %s already exists", newDir)
	}
	if err := os.MkdirAll(filepath.Dir(newDir), 0755); err != nil {
		return err
	}
	return os.Rename(oldDir, newDir)
}

// CloneLocalRepo clones g (full history) into dir, which must not exist yet, and returns the checked-out branch and commit.
// git's progress output (carriage-return updates included) is written to log, which may be nil.
func CloneLocalRepo(ctx context.Context, g GitSource, dir string, log io.Writer) (branch, sha string, err error) {
	if err := ValidateGitURL(g.URL); err != nil {
		return "", "", invalidf("%v", err)
	}
	if err := ValidateGitBranch(g.Branch); err != nil {
		return "", "", invalidf("%v", err)
	}
	if g.Token != "" && GitURLScheme(g.URL) != "https" {
		return "", "", invalidf("token authentication requires an https:// git url")
	}
	return cloneLocalRepo(ctx, g, dir, "https:ssh", log)
}

func cloneLocalRepo(ctx context.Context, g GitSource, dir, protocols string, log io.Writer) (branch, sha string, err error) {
	if _, err := os.Lstat(dir); err == nil {
		return "", "", invalidf("folder %s already exists", dir)
	}
	bin, err := exec.LookPath("git")
	if err != nil {
		return "", "", errors.New("git not found in PATH")
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0755); err != nil {
		return "", "", err
	}
	work, err := os.MkdirTemp("", "pantau-repo-*")
	if err != nil {
		return "", "", fmt.Errorf("failed to create temporary workspace: %w", err)
	}
	defer os.RemoveAll(work)
	env, err := setupGitEnv(g.URL, g.Token, g.PrivateKey, work, protocols)
	if err != nil {
		return "", "", err
	}
	args := []string{"-c", "credential.helper=", "clone", "--progress"} // --progress: git only reports progress on a terminal otherwise
	if g.Branch != "" {
		args = append(args, "--branch", g.Branch)
	}
	args = append(args, "--", g.URL, dir)
	cmd := exec.Command(bin, args...)
	cmd.Dir = work
	cmd.Env = env
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if log != nil {
		cmd.Stderr = io.MultiWriter(&errb, log)
	}
	code, err := runCmd(ctx, cmd)
	if err == nil && code != 0 {
		err = fmt.Errorf("git clone failed (exit %d)", code)
	}
	if err != nil {
		os.RemoveAll(dir) // partial clone; dir did not exist before this call
		return "", "", sanitizeGitError(err, errb.String(), g.URL)
	}
	st, err := LocalRepoStatus(ctx, dir)
	if err != nil {
		return "", "", err
	}
	return st.Branch, st.Commit, nil
}

var aheadRe, behindRe = regexp.MustCompile(`ahead (\d+)`), regexp.MustCompile(`behind (\d+)`)

// LocalRepoStatus reads branch, HEAD, ahead/behind (vs the last fetched upstream) and working-tree changes.
func LocalRepoStatus(ctx context.Context, dir string) (*LocalStatus, error) {
	l, err := openLocalGit(dir, nil, "https:ssh")
	if err != nil {
		return nil, err
	}
	defer l.close()
	out, err := l.run(ctx, nil, "status", "--porcelain=v1", "-z", "-b", "--no-renames", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	st := &LocalStatus{Changes: []LocalChange{}}
	for i, e := range strings.Split(out, "\x00") {
		if e == "" {
			continue
		}
		if i == 0 && strings.HasPrefix(e, "## ") {
			h := strings.TrimPrefix(e, "## ")
			if m := aheadRe.FindStringSubmatch(h); m != nil {
				st.Ahead, _ = strconv.Atoi(m[1])
			}
			if m := behindRe.FindStringSubmatch(h); m != nil {
				st.Behind, _ = strconv.Atoi(m[1])
			}
			h = strings.TrimPrefix(h, "No commits yet on ")
			h, _, _ = strings.Cut(h, " [")
			st.Branch, _, _ = strings.Cut(h, "...")
			if strings.HasPrefix(st.Branch, "HEAD (") {
				st.Branch = "HEAD"
			}
			continue
		}
		if len(e) < 4 {
			continue
		}
		if len(st.Changes) >= maxTreeEntries {
			st.Truncated = true
			break
		}
		xy, p := e[:2], e[3:]
		kind := "modified"
		switch {
		case xy == "??" || xy[0] == 'A':
			kind = "new"
		case strings.ContainsRune(xy, 'D'):
			kind = "deleted"
		}
		st.Changes = append(st.Changes, LocalChange{Path: p, Status: kind})
	}
	sort.Slice(st.Changes, func(i, j int) bool { return st.Changes[i].Path < st.Changes[j].Path })
	if sha, err := l.run(ctx, nil, "rev-parse", "HEAD"); err == nil { // fails only on a repository without commits
		st.Commit = strings.TrimSpace(sha)
	}
	return st, nil
}

// LocalDirEntry is one entry of a repository folder as committed in HEAD.
type LocalDirEntry struct {
	Name string `json:"name"`
	Dir  bool   `json:"dir"`
	Size int64  `json:"size"`
}

// ListLocalRepoDir lists one folder of HEAD ("" = repository root): its sub-folders and files (symlinks and submodules
// left out), folders first, up to maxTreeEntries. Working-tree changes are not applied: the caller overlays
// LocalStatus.Changes, so a big repository costs one cheap `ls-tree` per opened folder. A folder that exists only in the
// working tree lists as empty.
func ListLocalRepoDir(ctx context.Context, dir, rel string) ([]LocalDirEntry, bool, error) {
	rev := "HEAD"
	if rel != "" {
		if err := validateSafeRelPath(rel); err != nil {
			return nil, false, err
		}
		rev = "HEAD:" + rel
	}
	l, err := openLocalGit(dir, nil, "https:ssh")
	if err != nil {
		return nil, false, err
	}
	defer l.close()
	out, err := l.run(ctx, nil, "ls-tree", "-z", "-l", rev)
	if err != nil {
		// No such folder in HEAD (new in the working tree) or a repository without commits.
		if m := err.Error(); strings.Contains(m, "Not a valid object name") || strings.Contains(m, "not a tree object") {
			return []LocalDirEntry{}, false, nil
		}
		return nil, false, err
	}
	entries := []LocalDirEntry{}
	for _, rec := range strings.Split(out, "\x00") {
		meta, name, ok := strings.Cut(rec, "\t")
		if !ok {
			continue
		}
		f := strings.Fields(meta) // mode type hash size
		if len(f) < 4 || f[0] == "120000" || f[0] == "160000" {
			continue
		}
		e := LocalDirEntry{Name: name, Dir: f[1] == "tree"}
		if !e.Dir {
			e.Size, _ = strconv.ParseInt(f[3], 10, 64)
		}
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Dir != entries[j].Dir {
			return entries[i].Dir
		}
		return entries[i].Name < entries[j].Name
	})
	if len(entries) > maxTreeEntries {
		return entries[:maxTreeEntries], true, nil
	}
	return entries, false, nil
}

// SearchLocalRepoFiles finds regular files (tracked or untracked, not ignored) whose path contains every
// space-separated term of query, case-insensitively. Names that match come before paths that only match in a folder
// name; at most limit results are returned and truncated says more existed.
func SearchLocalRepoFiles(ctx context.Context, dir, query string, limit int) ([]RepoFile, bool, error) {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return []RepoFile{}, false, nil
	}
	l, err := openLocalGit(dir, nil, "https:ssh")
	if err != nil {
		return nil, false, err
	}
	defer l.close()
	out, err := l.run(ctx, nil, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, false, err
	}
	type hit struct {
		path string
		base bool // every term is in the file name itself
	}
	var hits []hit
	prev := ""
	names := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	sort.Strings(names)
	for _, n := range names {
		if n == "" || n == prev {
			continue
		}
		prev = n
		low := strings.ToLower(n)
		all, base := true, true
		for _, t := range terms {
			if !strings.Contains(low, t) {
				all = false
				break
			}
			if !strings.Contains(low[strings.LastIndexByte(low, '/')+1:], t) {
				base = false
			}
		}
		if all {
			hits = append(hits, hit{n, base})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].base && !hits[j].base })
	res := make([]RepoFile, 0, min(len(hits), limit))
	truncated := len(hits) > limit
	for _, h := range hits {
		if len(res) >= limit {
			break
		}
		fi, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(h.path)))
		if err != nil || !fi.Mode().IsRegular() { // tracked but deleted, or a symlink
			continue
		}
		res = append(res, RepoFile{Path: h.path, Size: fi.Size()})
	}
	return res, truncated, nil
}

func requireLocalRepo(dir string) error {
	if st, err := os.Stat(filepath.Join(dir, ".git")); err != nil || !st.IsDir() {
		return ErrRepoMissing
	}
	return nil
}

// ReadLocalRepoFile reads a regular, non-symlink, <= 1 MiB UTF-8 file of the working tree.
func ReadLocalRepoFile(dir, relPath string) (string, error) {
	if err := validateSafeRelPath(relPath); err != nil {
		return "", err
	}
	if err := requireLocalRepo(dir); err != nil {
		return "", err
	}
	return readTextFile(dir, relPath)
}

// WriteLocalRepoFile writes (creating parent folders) a <= 1 MiB UTF-8 file into the working tree; nothing is committed.
func WriteLocalRepoFile(dir, relPath, content string) error {
	if err := validateSafeRelPath(relPath); err != nil {
		return err
	}
	if len(content) > maxFileSize {
		return tooLargef("file %q exceeds 1 MiB limit", relPath)
	}
	if strings.IndexByte(content, 0) != -1 || !utf8.ValidString(content) {
		return ErrNotText
	}
	if err := requireLocalRepo(dir); err != nil {
		return err
	}
	if err := checkNoSymlinks(dir, relPath); err != nil {
		return err
	}
	full := filepath.Join(dir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(content), 0644)
}

// DeleteLocalRepoFile removes a regular file from the working tree; nothing is committed.
func DeleteLocalRepoFile(dir, relPath string) error {
	if err := validateSafeRelPath(relPath); err != nil {
		return err
	}
	if err := requireLocalRepo(dir); err != nil {
		return err
	}
	if err := checkNoSymlinks(dir, relPath); err != nil {
		return err
	}
	full := filepath.Join(dir, filepath.FromSlash(relPath))
	fi, err := os.Lstat(full)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return invalidf("not a regular file")
	}
	return os.Remove(full)
}

// DiscardLocalRepoPath drops the uncommitted change of one path: tracked files return to HEAD, new files are removed.
func DiscardLocalRepoPath(ctx context.Context, dir, relPath string) error {
	if err := validateSafeRelPath(relPath); err != nil {
		return err
	}
	if err := checkNoSymlinks(dir, relPath); err != nil {
		return err
	}
	lock := getBranchLock(dir, "")
	lock.Lock()
	defer lock.Unlock()
	l, err := openLocalGit(dir, nil, "https:ssh")
	if err != nil {
		return err
	}
	defer l.close()
	tracked, err := l.run(ctx, nil, "ls-files", "-z", "--", relPath)
	if err != nil {
		return err
	}
	if tracked != "" {
		_, err = l.run(ctx, nil, "checkout", "HEAD", "--", relPath)
		return err
	}
	return DeleteLocalRepoFile(dir, relPath)
}

// currentBranch returns the checked-out branch or an error on detached HEAD.
func (l *localGit) currentBranch(ctx context.Context) (string, error) {
	out, err := l.run(ctx, nil, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	b := strings.TrimSpace(out)
	if b == "" || b == "HEAD" {
		return "", errors.New("repository is in detached HEAD state")
	}
	return b, nil
}

// PullLocalRepo fetches the current branch and fast-forwards to it; it never merges or rebases.
func PullLocalRepo(ctx context.Context, dir string, g GitSource, protocols string) (*LocalStatus, error) {
	lock := getBranchLock(dir, "")
	lock.Lock()
	defer lock.Unlock()
	l, err := openLocalGit(dir, &g, protocols)
	if err != nil {
		return nil, err
	}
	defer l.close()
	branch, err := l.currentBranch(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := l.run(ctx, nil, "fetch", "--no-tags", "origin", "refs/heads/"+branch); err != nil {
		return nil, err
	}
	if _, err := l.run(ctx, nil, "merge", "--ff-only", "FETCH_HEAD"); err != nil {
		return nil, err
	}
	return LocalRepoStatus(ctx, dir)
}

func (l *localGit) push(ctx context.Context, branch string) error {
	if _, err := l.run(ctx, nil, "push", "-u", "origin", "HEAD:refs/heads/"+branch); err != nil {
		return fmt.Errorf("%w: %s", ErrPushRejected, err.Error())
	}
	return nil
}

// PushLocalRepo pushes the current branch (no force). Used to retry after a commit whose push failed.
func PushLocalRepo(ctx context.Context, dir string, g GitSource, protocols string) (*CommitResult, error) {
	lock := getBranchLock(dir, "")
	lock.Lock()
	defer lock.Unlock()
	l, err := openLocalGit(dir, &g, protocols)
	if err != nil {
		return nil, err
	}
	defer l.close()
	branch, err := l.currentBranch(ctx)
	if err != nil {
		return nil, err
	}
	if err := l.push(ctx, branch); err != nil {
		return nil, err
	}
	sha, err := l.run(ctx, nil, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	return &CommitResult{Commit: strings.TrimSpace(sha), Branch: branch, Pushed: true}, nil
}

// CommitLocalRepo stages spec.Paths (all changes when empty), commits only those, and pushes (no force) to the current
// branch or spec.NewBranch. If the push fails the commit stays local and ErrPushRejected is returned.
func CommitLocalRepo(ctx context.Context, dir string, g GitSource, spec LocalCommitSpec, protocols string) (*CommitResult, error) {
	meta := CommitSpec{Message: spec.Message, AuthorName: spec.AuthorName, AuthorEmail: spec.AuthorEmail, NewBranch: spec.NewBranch}
	if err := validateCommitMeta(&meta); err != nil {
		return nil, err
	}
	if len(spec.Paths) > maxLocalCommitPaths {
		return nil, tooLargef("too many paths: %d exceeds maximum %d", len(spec.Paths), maxLocalCommitPaths)
	}
	for _, p := range spec.Paths {
		if err := validateSafeRelPath(p); err != nil {
			return nil, fmt.Errorf("path %q: %w", p, err)
		}
	}

	lock := getBranchLock(dir, "")
	lock.Lock()
	defer lock.Unlock()
	l, err := openLocalGit(dir, &g, protocols)
	if err != nil {
		return nil, err
	}
	defer l.close()
	branch, err := l.currentBranch(ctx)
	if err != nil {
		return nil, err
	}
	if meta.NewBranch != "" && meta.NewBranch == branch {
		return nil, invalidf("new branch %q cannot be the same as current branch %q", meta.NewBranch, branch)
	}

	addArgs, diffArgs := []string{"add", "-A"}, []string{"diff", "--cached", "--name-only", "-z"}
	if len(spec.Paths) > 0 {
		addArgs = append(addArgs, "--")
		addArgs = append(addArgs, spec.Paths...)
		diffArgs = append(diffArgs, "--")
		diffArgs = append(diffArgs, spec.Paths...)
	}
	if _, err := l.run(ctx, nil, addArgs...); err != nil {
		return nil, err
	}
	staged, err := l.run(ctx, nil, diffArgs...)
	if err != nil {
		return nil, err
	}
	changed := strings.Count(staged, "\x00")
	if changed == 0 {
		return nil, ErrNothingToCommit
	}

	pushBranch := branch
	if meta.NewBranch != "" {
		pushBranch = meta.NewBranch
		if _, err := l.run(ctx, nil, "checkout", "-b", meta.NewBranch); err != nil {
			return nil, err
		}
	}
	commitArgs := []string{"commit", "-m", meta.Message}
	if len(spec.Paths) > 0 { // --only semantics: files staged by someone else outside the selection stay out
		commitArgs = append(commitArgs, "--")
		commitArgs = append(commitArgs, spec.Paths...)
	}
	if _, err := l.run(ctx, []string{
		"GIT_AUTHOR_NAME=" + meta.AuthorName, "GIT_AUTHOR_EMAIL=" + meta.AuthorEmail,
		"GIT_COMMITTER_NAME=" + meta.AuthorName, "GIT_COMMITTER_EMAIL=" + meta.AuthorEmail,
	}, commitArgs...); err != nil {
		return nil, err
	}
	sha, err := l.run(ctx, nil, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	if err := l.push(ctx, pushBranch); err != nil {
		return nil, err
	}
	return &CommitResult{Commit: strings.TrimSpace(sha), Branch: pushBranch, Changed: changed, Pushed: true}, nil
}
