package ansible

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// GitSource describes a playbook stored in a Git repository. The repository is shallow-cloned into the run
// workspace with the local `git` binary; the clone never leaves the workspace.
type GitSource struct {
	URL        string // https://host/path, ssh://[user@]host[:port]/path or user@host:path
	Branch     string // branch or tag; "" = remote default branch
	Path       string // playbook file inside the repository, e.g. "site.yml" or "playbooks/web.yml"
	Token      string // https only: sent as a Basic credential (GitHub/GitLab personal access token)
	PrivateKey string // ssh only: PEM private key, written 0600 into the workspace
}

var (
	gitBranchRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`)
	gitSCPRe    = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:[A-Za-z0-9._~/][A-Za-z0-9._~/-]*$`)
)

// ValidateGitURL accepts only https://, ssh:// and scp-like URLs without embedded credentials. Everything else
// (file://, ext::, local paths, git://, http://, option-looking values) is rejected: the URL is attacker-shaped input
// handed to `git clone`.
func ValidateGitURL(raw string) error {
	if raw == "" {
		return errors.New("git url is required")
	}
	if len(raw) > 512 {
		return errors.New("git url exceeds 512 characters")
	}
	for _, r := range raw {
		if r <= ' ' || r == 0x7f {
			return errors.New("git url must not contain whitespace or control characters")
		}
	}
	switch {
	case strings.HasPrefix(raw, "https://"), strings.HasPrefix(raw, "ssh://"):
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" {
			return errors.New("invalid git url")
		}
		if u.Scheme == "https" && u.User != nil {
			return errors.New("git url must not embed credentials; use the token field")
		}
		if u.RawQuery != "" || u.Fragment != "" || strings.Trim(u.Path, "/") == "" {
			return errors.New("invalid git url")
		}
	case gitSCPRe.MatchString(raw):
	default:
		return errors.New("git url must start with https://, ssh:// or be of the form user@host:path")
	}
	return nil
}

// GitURLScheme returns "https" or "ssh" for a URL accepted by ValidateGitURL.
func GitURLScheme(raw string) string {
	if strings.HasPrefix(raw, "https://") {
		return "https"
	}
	return "ssh"
}

func ValidateGitBranch(b string) error {
	if b == "" {
		return nil
	}
	if !gitBranchRe.MatchString(b) || strings.Contains(b, "..") || strings.HasSuffix(b, "/") || strings.HasSuffix(b, ".lock") {
		return fmt.Errorf("invalid git branch %q", b)
	}
	return nil
}

func ValidateGitPath(p string) error {
	if p == "" || len(p) > 256 {
		return errors.New("git playbook path must be 1-256 characters")
	}
	if strings.ContainsAny(p, "\\\x00") || strings.HasPrefix(p, "/") || path.Clean(p) != p || p == "." || strings.HasPrefix(p, "../") {
		return errors.New("git playbook path must be a clean relative path inside the repository")
	}
	if ext := strings.ToLower(path.Ext(p)); ext != ".yml" && ext != ".yaml" {
		return errors.New("git playbook path must end with .yml or .yaml")
	}
	return nil
}

func (g GitSource) Validate() error {
	if err := ValidateGitURL(g.URL); err != nil {
		return err
	}
	if err := ValidateGitBranch(g.Branch); err != nil {
		return err
	}
	if err := ValidateGitPath(g.Path); err != nil {
		return err
	}
	if g.Token != "" && GitURLScheme(g.URL) != "https" {
		return errors.New("token authentication requires an https:// git url")
	}
	return nil
}

func gitEnv(workDir, protocols string) []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"LANG=C.UTF-8",
		"HOME=" + workDir,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_ALLOW_PROTOCOL=" + protocols,
	}
	for _, k := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// cloneGit shallow-clones g into workDir/repo and returns the repository dir, the absolute playbook path and the
// checked-out commit. protocols is the GIT_ALLOW_PROTOCOL value ("https:ssh" in production).
func cloneGit(ctx context.Context, g GitSource, workDir, protocols string, log io.Writer) (repoDir, playbookPath, sha string, err error) {
	if log == nil {
		log = io.Discard
	}
	gitBin, err := exec.LookPath("git")
	if err != nil {
		return "", "", "", errors.New("git not found in PATH")
	}
	repoDir = filepath.Join(workDir, "repo")
	env := gitEnv(workDir, protocols)

	// -F /dev/null: same reason as ansible.cfg (never read the passwd-home ssh config).
	sshCmd := "ssh -F /dev/null -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o IdentitiesOnly=yes"
	if strings.TrimSpace(g.PrivateKey) != "" {
		kp := filepath.Join(workDir, "git_key")
		if err := os.WriteFile(kp, []byte(strings.TrimSpace(g.PrivateKey)+"\n"), 0600); err != nil {
			return "", "", "", fmt.Errorf("failed to write git key: %w", err)
		}
		sshCmd += " -i '" + kp + "'"
	}
	env = append(env, "GIT_SSH_COMMAND="+sshCmd)

	if g.Token != "" {
		u, perr := url.Parse(g.URL)
		if perr != nil || u.Scheme != "https" {
			return "", "", "", errors.New("token authentication requires an https:// git url")
		}
		// Scoped to the repository host and passed via env so the token never shows up in argv or the log.
		cred := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + g.Token))
		env = append(env, "GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=http.https://"+u.Host+"/.extraHeader",
			"GIT_CONFIG_VALUE_0=Authorization: Basic "+cred)
	}

	args := []string{"-c", "credential.helper=", "clone", "--depth", "1", "--single-branch", "--no-tags"}
	ref := ""
	if g.Branch != "" {
		args = append(args, "--branch", g.Branch)
		ref = " (" + g.Branch + ")"
	}
	args = append(args, "--", g.URL, repoDir)

	fmt.Fprintf(log, "[pantau] git clone %s%s\n", g.URL, ref)
	cmd := exec.Command(gitBin, args...)
	cmd.Dir = workDir
	cmd.Env = env
	cmd.Stdout = log
	cmd.Stderr = log
	code, err := runCmd(ctx, cmd)
	if err != nil {
		return "", "", "", err
	}
	if code != 0 {
		return "", "", "", fmt.Errorf("git clone failed (exit %d)", code)
	}

	rev := exec.Command(gitBin, "-C", repoDir, "rev-parse", "HEAD")
	rev.Env = env
	out, err := rev.Output()
	if err != nil {
		return "", "", "", fmt.Errorf("git rev-parse failed: %w", err)
	}
	sha = strings.TrimSpace(string(out))
	fmt.Fprintf(log, "[pantau] git commit %s\n", sha)

	// The playbook must be a regular file that stays inside the clone even after resolving symlinks.
	playbookPath = filepath.Join(repoDir, filepath.FromSlash(g.Path))
	root, err := filepath.EvalSymlinks(repoDir)
	if err != nil {
		return "", "", "", err
	}
	real, err := filepath.EvalSymlinks(playbookPath)
	if err != nil {
		return "", "", "", fmt.Errorf("playbook %q not found in repository", g.Path)
	}
	if rel, err := filepath.Rel(root, real); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", "", fmt.Errorf("playbook %q resolves outside the repository", g.Path)
	}
	if st, err := os.Stat(real); err != nil || !st.Mode().IsRegular() {
		return "", "", "", fmt.Errorf("playbook %q is not a regular file", g.Path)
	}
	return repoDir, playbookPath, sha, nil
}
