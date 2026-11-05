package ansible

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestValidateGitURL(t *testing.T) {
	good := []string{
		"https://github.com/acme/infra.git",
		"ssh://git@gitlab.example.com:2222/acme/infra.git",
		"git@github.com:acme/infra.git",
	}
	for _, u := range good {
		if err := ValidateGitURL(u); err != nil {
			t.Errorf("ValidateGitURL(%q) = %v, want nil", u, err)
		}
	}
	bad := []string{
		"", "file:///etc", "/srv/repo", "../repo", "http://example.com/r.git", "git://example.com/r.git",
		"ext::sh -c id", "-uorigin", "--upload-pack=id", "https://user:pw@example.com/r.git",
		"https://example.com/r.git?x=1", "https://example.com/", "https://exa mple.com/r.git", "ftp://example.com/r",
		"git@host:-oProxyCommand=id", "https://example.com/r.git\nfoo",
	}
	for _, u := range bad {
		if err := ValidateGitURL(u); err == nil {
			t.Errorf("ValidateGitURL(%q) = nil, want error", u)
		}
	}
}

func TestGitSourceValidate(t *testing.T) {
	ok := GitSource{URL: "https://github.com/acme/infra.git", Branch: "release/1.2", Path: "playbooks/site.yml"}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid source rejected: %v", err)
	}
	cases := map[string]GitSource{
		"branch option":  {URL: ok.URL, Branch: "--upload-pack=x", Path: ok.Path},
		"branch dotdot":  {URL: ok.URL, Branch: "a..b", Path: ok.Path},
		"path absolute":  {URL: ok.URL, Path: "/etc/passwd.yml"},
		"path traversal": {URL: ok.URL, Path: "../x.yml"},
		"path unclean":   {URL: ok.URL, Path: "a/../b.yml"},
		"path not yaml":  {URL: ok.URL, Path: "site.sh"},
		"path empty":     {URL: ok.URL},
		"token over ssh": {URL: "git@github.com:a/b.git", Path: "s.yml", Token: "t"},
		"backslash path": {URL: ok.URL, Path: `a\b.yml`},
	}
	for name, g := range cases {
		if err := g.Validate(); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	// A git source needs no inline playbook; an inline source does.
	spec := RunSpec{Mode: "syntax", Git: &ok}
	if err := spec.Validate(); err != nil {
		t.Errorf("git spec without inline content rejected: %v", err)
	}
	if err := (RunSpec{Mode: "syntax"}).Validate(); err == nil {
		t.Error("inline spec without content accepted")
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestCloneGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	origin := t.TempDir()
	gitRun(t, origin, "init", "-q", "-b", "main")
	if err := os.MkdirAll(filepath.Join(origin, "pb"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(origin, "pb", "site.yml"), []byte("- hosts: all\n  tasks: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/hostname.yml", filepath.Join(origin, "escape.yml")); err != nil {
		t.Fatal(err)
	}
	gitRun(t, origin, "add", "-A")
	gitRun(t, origin, "commit", "-q", "-m", "init")
	gitRun(t, origin, "checkout", "-q", "-b", "dev")
	if err := os.WriteFile(filepath.Join(origin, "pb", "dev.yml"), []byte("- hosts: all\n  tasks: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, origin, "add", "-A")
	gitRun(t, origin, "commit", "-q", "-m", "dev")

	clone := func(g GitSource) (string, string, error) {
		g.URL = origin
		work := t.TempDir()
		var log strings.Builder
		_, pbPath, sha, err := cloneGit(context.Background(), g, work, "file", &log)
		if err == nil && !strings.Contains(log.String(), "[pantau] git commit "+sha) {
			t.Errorf("log lacks commit line: %q", log.String())
		}
		return pbPath, sha, err
	}

	p, sha, err := clone(GitSource{Branch: "main", Path: "pb/site.yml"})
	if err != nil || len(sha) != 40 {
		t.Fatalf("clone main: sha=%q err=%v", sha, err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("playbook not on disk: %v", err)
	}
	if _, _, err := clone(GitSource{Branch: "dev", Path: "pb/dev.yml"}); err != nil {
		t.Fatalf("clone dev branch: %v", err)
	}
	if _, _, err := clone(GitSource{Branch: "main", Path: "pb/dev.yml"}); err == nil {
		t.Error("dev-only playbook found on main")
	}
	if _, _, err := clone(GitSource{Branch: "main", Path: "escape.yml"}); err == nil {
		t.Error("symlink escaping the repository was accepted")
	}
	if _, _, err := clone(GitSource{Branch: "nope", Path: "pb/site.yml"}); err == nil {
		t.Error("clone of a missing branch succeeded")
	}
	// Production protocol allowlist must refuse a local path.
	g := GitSource{URL: origin, Path: "pb/site.yml"}
	if _, _, _, err := cloneGit(context.Background(), g, t.TempDir(), "https:ssh", nil); err == nil {
		t.Error("local path clone allowed under https:ssh")
	}
}

func TestParseCronAndNext(t *testing.T) {
	loc := time.UTC
	at := func(y int, mo time.Month, d, h, mi int) time.Time { return time.Date(y, mo, d, h, mi, 0, 0, loc) }
	cases := []struct {
		expr  string
		after time.Time
		want  time.Time
	}{
		{"* * * * *", at(2026, 1, 1, 10, 0), at(2026, 1, 1, 10, 1)},
		{"*/15 * * * *", at(2026, 1, 1, 10, 14), at(2026, 1, 1, 10, 15)},
		{"0 3 * * *", at(2026, 1, 1, 3, 0), at(2026, 1, 2, 3, 0)},
		{"30 2 1 * *", at(2026, 1, 1, 2, 30), at(2026, 2, 1, 2, 30)},
		{"0 9 * * 1-5", at(2026, 11, 6, 10, 0), at(2026, 11, 9, 9, 0)}, // Fri -> Mon
		{"0 0 * * 7", at(2026, 11, 4, 0, 0), at(2026, 11, 8, 0, 0)},    // 7 == Sunday
		{"5,35 * * * *", at(2026, 1, 1, 10, 6), at(2026, 1, 1, 10, 35)},
		{"10-12/2 * * * *", at(2026, 1, 1, 10, 10), at(2026, 1, 1, 10, 12)},
		{"@daily", at(2026, 1, 1, 0, 0), at(2026, 1, 2, 0, 0)},
		{"0 0 29 2 *", at(2026, 3, 1, 0, 0), at(2028, 2, 29, 0, 0)},
		{"0 12 13 * 5", at(2026, 11, 1, 0, 0), at(2026, 11, 6, 12, 0)}, // dom OR dow when both restricted
	}
	for _, c := range cases {
		cr, err := ParseCron(c.expr)
		if err != nil {
			t.Errorf("%q: %v", c.expr, err)
			continue
		}
		if got := cr.Next(c.after); !got.Equal(c.want) {
			t.Errorf("%q after %v: got %v want %v", c.expr, c.after, got, c.want)
		}
	}
	for _, bad := range []string{"", "* * * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *", "a * * * *", "*/0 * * * *", "5-1 * * * *", "* * * * * *"} {
		if _, err := ParseCron(bad); err == nil {
			t.Errorf("ParseCron(%q) accepted", bad)
		}
	}
	if cr, _ := ParseCron("0 0 31 2 *"); !cr.Next(at(2026, 1, 1, 0, 0)).IsZero() {
		t.Error("impossible date should yield zero time")
	}
}

// Run with a git source, driven by fake git/ansible-playbook binaries (no network): proves the wiring between
// clone, working directory, playbook path, commit callback, token transport and workspace cleanup.
func TestRunWithGitSourceFakeBinaries(t *testing.T) {
	bin := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("git", `
if [ "$1" = "-C" ]; then echo 0123456789abcdef0123456789abcdef01234567; exit 0; fi
echo "GIT ARGS: $@"
[ -n "$GIT_CONFIG_VALUE_0" ] && echo "AUTH HEADER SET for $GIT_CONFIG_KEY_0"
echo "ALLOW: $GIT_ALLOW_PROTOCOL"
for a in "$@"; do last="$a"; done
mkdir -p "$last/pb" && echo "- hosts: all" > "$last/pb/site.yml"
`)
	write("ansible-playbook", `
echo "PWD: $(pwd)"
echo "ARGS: $@"
`)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))

	var sha string
	var log strings.Builder
	code, err := Run(context.Background(), RunSpec{
		Mode:     "check",
		Targets:  []Target{{ID: 1, Name: "h1", Address: "10.0.0.1"}},
		Git:      &GitSource{URL: "https://github.com/acme/infra.git", Branch: "main", Path: "pb/site.yml", Token: "ghp_SECRETTOKEN"},
		OnCommit: func(s string) { sha = s },
	}, &log)
	if err != nil || code != 0 {
		t.Fatalf("Run: code=%d err=%v\n%s", code, err, log.String())
	}
	out := log.String()
	if sha != "0123456789abcdef0123456789abcdef01234567" {
		t.Errorf("OnCommit sha = %q", sha)
	}
	if strings.Contains(out, "SECRETTOKEN") {
		t.Errorf("token leaked into the run log:\n%s", out)
	}
	for _, want := range []string{"--depth 1", "--branch main", "AUTH HEADER SET for http.https://github.com/.extraHeader", "ALLOW: https:ssh", "[pantau] git commit 0123456789abcdef"} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q:\n%s", want, out)
		}
	}
	m := regexp.MustCompile(`PWD: (\S+)`).FindStringSubmatch(out)
	if m == nil || !strings.HasSuffix(m[1], "/repo") {
		t.Fatalf("ansible-playbook must run inside the clone, got %v\n%s", m, out)
	}
	if !strings.Contains(out, "/repo/pb/site.yml") {
		t.Errorf("playbook path not passed:\n%s", out)
	}
	if _, err := os.Stat(filepath.Dir(m[1])); !os.IsNotExist(err) {
		t.Errorf("workspace %s was not removed", filepath.Dir(m[1]))
	}
}
