package ansible

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// maxSyncFiles caps the .yml/.yaml list SyncGit returns; a huge repository must not blow up the JSON response.
const maxSyncFiles = 1000

var commitSHAMatch = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// GitSyncResult is what a Sync Now verification answers: the checked-out commit, the branch it came from and the
// playbook files of the repository.
type GitSyncResult struct {
	Commit string   `json:"commit"`
	Branch string   `json:"branch"`
	Files  []string `json:"files"`
}

// ErrPlaybookPathMissing means the cloned repository does not contain the configured playbook path.
var ErrPlaybookPathMissing = errors.New("configured playbook path not found in the repository")

// SyncGit shallow-clones g into a throwaway workspace to verify it and collects its .yml/.yaml files. The clone
// errors keep only git's last lines and never include the clone URL, so a token in the URL or the credential
// header cannot leak into an API response.
func SyncGit(ctx context.Context, g GitSource) (*GitSyncResult, error) {
	return syncGit(ctx, g, "https:ssh")
}

// syncGit is SyncGit with the GIT_ALLOW_PROTOCOL value injectable, so tests can clone a local repository.
func syncGit(ctx context.Context, g GitSource, protocols string) (*GitSyncResult, error) {
	work, err := os.MkdirTemp("", "pantau-ansible-sync-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary workspace: %w", err)
	}
	defer os.RemoveAll(work)

	var log strings.Builder
	repo, sha, err := CloneGitRepo(ctx, g.URL, g.Branch, g.Token, g.PrivateKey, work, protocols, &log)
	if err != nil {
		return nil, sanitizeGitError(err, log.String(), g.URL)
	}
	if !commitSHAMatch.MatchString(sha) {
		return nil, errors.New("git returned an unexpected commit id")
	}
	// Sync Now exists to catch a broken configuration, so a cloned repository without the configured playbook
	// is a failure, not a success.
	if g.Path != "" {
		if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(g.Path))); err != nil {
			return nil, fmt.Errorf("%w: %q", ErrPlaybookPathMissing, g.Path)
		}
	}
	files, err := listPlaybookFiles(repo)
	if err != nil {
		return nil, err
	}
	return &GitSyncResult{Commit: sha, Branch: g.Branch, Files: files}, nil
}

// listPlaybookFiles walks the clone in path order and returns relative .yml/.yaml paths, capped at maxSyncFiles.
func listPlaybookFiles(repo string) ([]string, error) {
	files := make([]string, 0, 32)
	err := filepath.WalkDir(repo, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(repo, p)
		if d.IsDir() {
			if rel != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || len(files) >= maxSyncFiles {
			return nil
		}
		switch strings.ToLower(filepath.Ext(rel)) {
		case ".yml", ".yaml":
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// sanitizeGitError turns a failed clone into a message that says what git said but nothing about where it was
// told to go.
func sanitizeGitError(err error, gitLog, repoURL string) error {
	msg := err.Error()
	if tail := strings.TrimSpace(gitLog); tail != "" {
		lines := strings.Split(tail, "\n")
		msg += ": " + strings.Join(lines[max(0, len(lines)-3):], " | ")
	}
	msg = strings.ReplaceAll(msg, repoURL, "<repo>")
	// git echoes the URL it was given, including the trailing slash it appends.
	if u, err := url.Parse(repoURL); err == nil {
		bare := *u
		bare.User = nil
		for _, variant := range []string{bare.String(), strings.TrimRight(bare.String(), "/") + "/"} {
			msg = strings.ReplaceAll(msg, variant, "<repo>")
		}
	}
	// A credential header would only show up in a verbose git log; strip any password-ish fragment anyway.
	if i := strings.Index(msg, "Authorization:"); i >= 0 {
		msg = msg[:i] + "Authorization: <redacted>"
	}
	if len(msg) > 500 {
		msg = msg[:500] + "..."
	}
	return errors.New(msg)
}
