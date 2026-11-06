package ansible

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

// RepoFile describes a single regular file in the cloned git repository.
type RepoFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// RepoTree represents the directory tree and metadata of a shallow clone.
type RepoTree struct {
	Commit    string     `json:"commit"`
	Branch    string     `json:"branch"`
	Files     []RepoFile `json:"files"`
	Truncated bool       `json:"truncated"`
}

// CommitChange describes a file modification, creation, or deletion.
type CommitChange struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Delete  bool   `json:"delete"`
}

// CommitSpec specifies the commit metadata and file changes to apply.
type CommitSpec struct {
	Message     string         `json:"message"`
	AuthorName  string         `json:"author_name"`
	AuthorEmail string         `json:"author_email"`
	BaseCommit  string         `json:"base_commit"`
	NewBranch   string         `json:"new_branch"`
	Changes     []CommitChange `json:"changes"`
}

// CommitResult is returned upon a successful commit and push.
type CommitResult struct {
	Commit  string `json:"commit"`
	Branch  string `json:"branch"`
	Changed int    `json:"changed"`
	Pushed  bool   `json:"pushed"`
}

var (
	ErrNotText         = errors.New("file is not valid UTF-8 text")
	ErrRepoChanged     = errors.New("repository changed since opened; reload")
	ErrNothingToCommit = errors.New("no changes to commit")
	ErrPushRejected    = errors.New("push rejected by remote")
	// ErrInvalidInput marks a request the caller can fix (bad path, message, author, branch); ErrTooLarge is the
	// size-limit subset of it. Handlers map them with errors.Is instead of matching message text.
	ErrInvalidInput = errors.New("invalid input")
	ErrTooLarge     = errors.New("size limit exceeded")

	authorEmailRe = regexp.MustCompile(`^[^\s@<>]+@[^\s@<>]+$`)

	branchLockMu sync.Mutex
	branchLocks  = make(map[string]*sync.Mutex)
)

type inputError struct {
	msg      string
	tooLarge bool
}

func (e *inputError) Error() string { return e.msg }
func (e *inputError) Is(target error) bool {
	return target == ErrInvalidInput || (e.tooLarge && target == ErrTooLarge)
}

func invalidf(format string, a ...any) error { return &inputError{msg: fmt.Sprintf(format, a...)} }
func tooLargef(format string, a ...any) error {
	return &inputError{msg: fmt.Sprintf(format, a...), tooLarge: true}
}

const (
	maxTreeEntries  = 2000
	maxFileSize     = 1 * 1024 * 1024 // 1 MiB
	maxTotalChanges = 5 * 1024 * 1024 // 5 MiB
	maxChangeCount  = 50
)

func getBranchLock(url, branch string) *sync.Mutex {
	branchLockMu.Lock()
	defer branchLockMu.Unlock()
	key := url + "#" + branch
	mu, ok := branchLocks[key]
	if !ok {
		mu = &sync.Mutex{}
		branchLocks[key] = mu
	}
	return mu
}

// ReadRepoTree clones g shallowly and returns all regular files up to maxTreeEntries (sorted).
func ReadRepoTree(ctx context.Context, g GitSource) (*RepoTree, error) {
	return readRepoTree(ctx, g, "https:ssh")
}

func readRepoTree(ctx context.Context, g GitSource, protocols string) (*RepoTree, error) {
	work, err := os.MkdirTemp("", "pantau-git-tree-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary workspace: %w", err)
	}
	defer os.RemoveAll(work)

	var log strings.Builder
	repo, sha, err := CloneGitRepo(ctx, g.URL, g.Branch, g.Token, g.PrivateKey, work, protocols, &log)
	if err != nil {
		return nil, sanitizeGitError(err, log.String(), g.URL)
	}

	branch, err := currentBranchName(ctx, repo, work, protocols)
	if err != nil {
		return nil, err
	}

	files := make([]RepoFile, 0, 64)
	truncated := false

	err = filepath.WalkDir(repo, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(repo, p)
		if d.IsDir() {
			if rel != "." && (strings.EqualFold(d.Name(), ".git") || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if len(files) >= maxTreeEntries {
			truncated = true
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		files = append(files, RepoFile{
			Path: filepath.ToSlash(rel),
			Size: info.Size(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	return &RepoTree{
		Commit:    sha,
		Branch:    branch,
		Files:     files,
		Truncated: truncated,
	}, nil
}

// ReadRepoFile shallow-clones g and reads path. Path must point to a regular file <= 1 MiB of valid UTF-8.
func ReadRepoFile(ctx context.Context, g GitSource, relPath string) (content, commit string, err error) {
	return readRepoFile(ctx, g, relPath, "https:ssh")
}

func readRepoFile(ctx context.Context, g GitSource, relPath string, protocols string) (content, commit string, err error) {
	if err := validateSafeRelPath(relPath); err != nil {
		return "", "", err
	}

	work, err := os.MkdirTemp("", "pantau-git-file-*")
	if err != nil {
		return "", "", fmt.Errorf("failed to create temporary workspace: %w", err)
	}
	defer os.RemoveAll(work)

	var log strings.Builder
	repo, sha, err := CloneGitRepo(ctx, g.URL, g.Branch, g.Token, g.PrivateKey, work, protocols, &log)
	if err != nil {
		return "", "", sanitizeGitError(err, log.String(), g.URL)
	}

	fullPath := filepath.Join(repo, filepath.FromSlash(relPath))
	if err := checkNoSymlinks(repo, relPath); err != nil {
		return "", "", err
	}

	fi, err := os.Lstat(fullPath)
	if err != nil {
		return "", "", err
	}
	if !fi.Mode().IsRegular() {
		return "", "", invalidf("not a regular file")
	}
	if fi.Size() > maxFileSize {
		return "", "", tooLargef("file size %d exceeds 1 MiB limit", fi.Size())
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", "", err
	}
	if bytes.IndexByte(data, 0) != -1 || !utf8.Valid(data) {
		return "", "", ErrNotText
	}

	return string(data), sha, nil
}

// CommitAndPush validates spec, clones g, applies file changes, commits, and pushes to remote.
func CommitAndPush(ctx context.Context, g GitSource, spec CommitSpec) (*CommitResult, error) {
	return commitAndPush(ctx, g, spec, "https:ssh")
}

func commitAndPush(ctx context.Context, g GitSource, spec CommitSpec, protocols string) (*CommitResult, error) {
	if err := validateCommitSpec(&spec); err != nil {
		return nil, err
	}

	// Serialise commits to the same URL and target branch
	targetBranch := g.Branch
	if spec.NewBranch != "" {
		targetBranch = spec.NewBranch
	}
	lock := getBranchLock(g.URL, targetBranch)
	lock.Lock()
	defer lock.Unlock()

	work, err := os.MkdirTemp("", "pantau-git-commit-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary workspace: %w", err)
	}
	defer os.RemoveAll(work)

	var log strings.Builder
	repo, sha, err := CloneGitRepo(ctx, g.URL, g.Branch, g.Token, g.PrivateKey, work, protocols, &log)
	if err != nil {
		return nil, sanitizeGitError(err, log.String(), g.URL)
	}

	if spec.BaseCommit != "" && spec.BaseCommit != sha {
		return nil, ErrRepoChanged
	}

	currBranch, err := currentBranchName(ctx, repo, work, protocols)
	if err != nil {
		return nil, err
	}
	if currBranch == "" || currBranch == "HEAD" {
		return nil, errors.New("repository is in a detached HEAD state")
	}

	if spec.NewBranch != "" && spec.NewBranch == currBranch {
		return nil, invalidf("new branch %q cannot be the same as current branch %q", spec.NewBranch, currBranch)
	}

	gitBin, err := exec.LookPath("git")
	if err != nil {
		return nil, errors.New("git not found in PATH")
	}
	env, err := setupGitEnv(g.URL, g.Token, g.PrivateKey, work, protocols)
	if err != nil {
		return nil, err
	}

	pushBranch := currBranch
	if spec.NewBranch != "" {
		pushBranch = spec.NewBranch
		cmd := exec.Command(gitBin, "-C", repo, "checkout", "-b", spec.NewBranch)
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("failed to checkout new branch: %s", strings.TrimSpace(string(out)))
		}
	}

	// Apply changes
	pathsToStage := make([]string, 0, len(spec.Changes))
	for _, ch := range spec.Changes {
		fullPath := filepath.Join(repo, filepath.FromSlash(ch.Path))
		if ch.Delete {
			fi, err := os.Lstat(fullPath)
			if err != nil {
				if os.IsNotExist(err) {
					pathsToStage = append(pathsToStage, ch.Path)
					continue
				}
				return nil, err
			}
			if !fi.Mode().IsRegular() {
				return nil, invalidf("cannot delete %q: not a regular file", ch.Path)
			}
			if err := os.Remove(fullPath); err != nil {
				return nil, err
			}
		} else {
			if err := checkNoSymlinks(repo, ch.Path); err != nil {
				return nil, err
			}
			dir := filepath.Dir(fullPath)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(fullPath, []byte(ch.Content), 0644); err != nil {
				return nil, err
			}
		}
		pathsToStage = append(pathsToStage, ch.Path)
	}

	// git add -A -- <path...>
	addArgs := append([]string{"-C", repo, "add", "-A", "--"}, pathsToStage...)
	addCmd := exec.Command(gitBin, addArgs...)
	addCmd.Env = env
	if out, err := addCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git add failed: %s", strings.TrimSpace(string(out)))
	}

	// Check if there is staged diff
	diffCmd := exec.Command(gitBin, "-C", repo, "diff", "--cached", "--quiet")
	diffCmd.Env = env
	diffCode, _ := runCmd(ctx, diffCmd)
	if diffCode == 0 {
		return nil, ErrNothingToCommit
	}

	// Commit
	commitEnv := append(env,
		"GIT_AUTHOR_NAME="+spec.AuthorName,
		"GIT_AUTHOR_EMAIL="+spec.AuthorEmail,
		"GIT_COMMITTER_NAME="+spec.AuthorName,
		"GIT_COMMITTER_EMAIL="+spec.AuthorEmail,
	)
	commitArgs := []string{
		"-C", repo,
		"-c", "commit.gpgsign=false",
		"-c", "core.hooksPath=/dev/null",
		"-c", "credential.helper=",
		"commit",
		"-m", spec.Message,
	}
	commitCmd := exec.Command(gitBin, commitArgs...)
	commitCmd.Env = commitEnv
	if out, err := commitCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git commit failed: %s", strings.TrimSpace(string(out)))
	}

	// Rev-parse new HEAD commit
	revCmd := exec.Command(gitBin, "-C", repo, "rev-parse", "HEAD")
	revCmd.Env = env
	newSHABuf, err := revCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get new commit sha: %w", err)
	}
	newSHA := strings.TrimSpace(string(newSHABuf))

	// Push
	pushRef := fmt.Sprintf("HEAD:refs/heads/%s", pushBranch)
	pushArgs := []string{
		"-C", repo,
		"-c", "credential.helper=",
		"push", "origin", pushRef,
	}
	pushCmd := exec.Command(gitBin, pushArgs...)
	pushCmd.Env = env
	var pushOut strings.Builder
	pushCmd.Stdout = &pushOut
	pushCmd.Stderr = &pushOut
	pCode, pErr := runCmd(ctx, pushCmd)
	if pErr != nil || pCode != 0 {
		cleanErr := sanitizeGitError(fmt.Errorf("git push failed (exit %d)", pCode), pushOut.String(), g.URL)
		return nil, fmt.Errorf("%w: %s", ErrPushRejected, cleanErr.Error())
	}

	return &CommitResult{
		Commit:  newSHA,
		Branch:  pushBranch,
		Changed: len(spec.Changes),
		Pushed:  true,
	}, nil
}

func currentBranchName(ctx context.Context, repo, workDir, protocols string) (string, error) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		return "", errors.New("git not found in PATH")
	}
	cmd := exec.Command(gitBin, "-C", repo, "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Env = gitEnv(workDir, protocols)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to determine git branch: %w", err)
	}
	b := strings.TrimSpace(string(out))
	if b == "" || b == "HEAD" {
		return "", errors.New("repository is in detached HEAD state")
	}
	return b, nil
}

func validateSafeRelPath(p string) error {
	if p == "" {
		return invalidf("path cannot be empty")
	}
	if len(p) > 255 {
		return invalidf("path exceeds 255 characters")
	}
	if strings.ContainsRune(p, 0) {
		return invalidf("path cannot contain NUL bytes")
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			return invalidf("path cannot contain control characters")
		}
	}
	if strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
		return invalidf("path must be a clean relative path with '/' separator")
	}
	cleaned := filepath.ToSlash(filepath.Clean(p))
	if cleaned != p || cleaned == "." || strings.HasPrefix(cleaned, "../") {
		return invalidf("path must not contain '..' or '.' segments")
	}
	parts := strings.Split(p, "/")
	for _, part := range parts {
		if strings.EqualFold(part, ".git") {
			return invalidf("path cannot contain '.git' component")
		}
	}
	return nil
}

func checkNoSymlinks(root, relPath string) error {
	parts := strings.Split(relPath, "/")
	curr := root
	for _, part := range parts {
		curr = filepath.Join(curr, part)
		fi, err := os.Lstat(curr)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			return invalidf("symlinks are not permitted")
		}
	}
	return nil
}

func validateCommitSpec(spec *CommitSpec) error {
	spec.Message = strings.TrimSpace(spec.Message)
	if spec.Message == "" {
		return invalidf("commit message is required")
	}
	if len(spec.Message) > 500 {
		return invalidf("commit message exceeds 500 characters")
	}
	if strings.ContainsRune(spec.Message, 0) {
		return invalidf("commit message cannot contain NUL bytes")
	}

	spec.AuthorName = strings.TrimSpace(spec.AuthorName)
	spec.AuthorEmail = strings.TrimSpace(spec.AuthorEmail)
	if spec.AuthorName == "" && spec.AuthorEmail == "" {
		spec.AuthorName = "Pantau"
		spec.AuthorEmail = "pantau@localhost"
	} else {
		if spec.AuthorName == "" || len(spec.AuthorName) > 100 || strings.ContainsAny(spec.AuthorName, "<>\r\n\x00") {
			return invalidf("author name must be 1-100 characters and cannot contain '<>', newlines, or NUL")
		}
		if spec.AuthorEmail == "" || len(spec.AuthorEmail) > 120 || !authorEmailRe.MatchString(spec.AuthorEmail) {
			return invalidf("author email is invalid")
		}
	}

	if spec.NewBranch != "" {
		if err := ValidateGitBranch(spec.NewBranch); err != nil {
			return invalidf("invalid new branch: %v", err)
		}
	}

	if len(spec.Changes) == 0 {
		return ErrNothingToCommit
	}
	if len(spec.Changes) > maxChangeCount {
		return tooLargef("too many changes: %d exceeds maximum %d", len(spec.Changes), maxChangeCount)
	}

	var totalSize int64
	seen := make(map[string]bool, len(spec.Changes))
	for i, ch := range spec.Changes {
		if err := validateSafeRelPath(ch.Path); err != nil {
			return fmt.Errorf("change %d (%q): %w", i, ch.Path, err)
		}
		if seen[ch.Path] {
			return invalidf("duplicate change path: %q", ch.Path)
		}
		seen[ch.Path] = true

		if !ch.Delete {
			if len(ch.Content) > maxFileSize {
				return tooLargef("file %q exceeds 1 MiB limit", ch.Path)
			}
			totalSize += int64(len(ch.Content))
			if totalSize > maxTotalChanges {
				return tooLargef("total change size exceeds 5 MiB limit")
			}
			if strings.IndexByte(ch.Content, 0) != -1 || !utf8.ValidString(ch.Content) {
				return fmt.Errorf("file %q: %w", ch.Path, ErrNotText)
			}
		}
	}

	return nil
}

// ReadRepoTreeTest allows tests to specify custom protocols (e.g. "file").
func ReadRepoTreeTest(ctx context.Context, g GitSource, protocols string) (*RepoTree, error) {
	return readRepoTree(ctx, g, protocols)
}

// ReadRepoFileTest allows tests to specify custom protocols (e.g. "file").
func ReadRepoFileTest(ctx context.Context, g GitSource, relPath, protocols string) (string, string, error) {
	return readRepoFile(ctx, g, relPath, protocols)
}

// CommitAndPushTest allows tests to specify custom protocols (e.g. "file").
func CommitAndPushTest(ctx context.Context, g GitSource, spec CommitSpec, protocols string) (*CommitResult, error) {
	return commitAndPush(ctx, g, spec, protocols)
}
