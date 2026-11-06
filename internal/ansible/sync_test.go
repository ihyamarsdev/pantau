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

func TestSyncGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	origin := t.TempDir()
	gitRun(t, origin, "init", "-q", "-b", "main")
	files := map[string]string{
		"site.yml":                  "- hosts: all\n",
		"playbooks/web.yaml":        "- hosts: web\n",
		"roles/app/tasks/main.yml":  "- debug: msg=hi\n",
		"README.md":                 "not a playbook\n",
		".github/workflows/ci.yml":  "- name: ci\n",
		"roles/app/files/a.conf.j2": "x\n",
	}
	for name, body := range files {
		full := filepath.Join(origin, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, origin, "add", "-A")
	gitRun(t, origin, "commit", "-q", "-m", "init")

	res, err := syncGit(context.Background(), GitSource{URL: origin, Branch: "main", Path: "site.yml"}, "file")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Commit) != 40 || res.Branch != "main" {
		t.Fatalf("unexpected result: %+v", res)
	}
	want := []string{"playbooks/web.yaml", "roles/app/tasks/main.yml", "site.yml"}
	if strings.Join(res.Files, ",") != strings.Join(want, ",") {
		t.Fatalf("files = %v, want %v (hidden dirs excluded, .j2/.md ignored, sorted)", res.Files, want)
	}

	// A missing branch must fail with git's own message.
	if _, err := syncGit(context.Background(), GitSource{URL: origin, Branch: "nope", Path: "site.yml"}, "file"); err == nil ||
		!strings.Contains(err.Error(), "nope") {
		t.Fatalf("want the unknown-branch error, got %v", err)
	}

	// A rejected URL never reaches git.
	if _, err := SyncGit(context.Background(), GitSource{URL: "file:///etc", Path: "site.yml"}); err == nil {
		t.Fatal("SyncGit must apply GitSource.Validate")
	}
}

func TestSyncGitDoesNotLeakURL(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	secret := "https://x-access-token:sup3rsecret@127.0.0.1:1/acme/infra.git"
	_, err := SyncGit(context.Background(), GitSource{URL: secret, Path: "site.yml"})
	if err == nil {
		t.Skip("clone unexpectedly succeeded")
	}
	// git repeats the host in its connect message (it is in the playbook config anyway), never the credential.
	if strings.Contains(err.Error(), "sup3rsecret") || strings.Contains(err.Error(), "x-access-token") {
		t.Fatalf("error leaks the credential: %v", err)
	}
	if err := ValidateGitURL(secret); err == nil {
		t.Fatal("ValidateGitURL is expected to reject embedded credentials")
	}
}

func TestListPlaybookFilesCap(t *testing.T) {
	dir := t.TempDir()
	for i := range maxSyncFiles + 5 {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("p%04d.yml", i)), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := listPlaybookFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != maxSyncFiles {
		t.Fatalf("got %d files, want the %d cap", len(files), maxSyncFiles)
	}
}

func TestSyncGitRejectsMissingPlaybookPath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	origin := t.TempDir()
	gitRun(t, origin, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(origin, "site.yml"), []byte("- hosts: all\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, origin, "add", "-A")
	gitRun(t, origin, "commit", "-q", "-m", "init")

	_, err := syncGit(context.Background(), GitSource{URL: origin, Branch: "main", Path: "playbooks/gone.yml"}, "file")
	if !errors.Is(err, ErrPlaybookPathMissing) {
		t.Fatalf("expected ErrPlaybookPathMissing, got %v", err)
	}

	// An empty path is the inline/whole-repo case: the clone alone is the check.
	if _, err := syncGit(context.Background(), GitSource{URL: origin, Branch: "main"}, "file"); err != nil {
		t.Fatalf("empty path: %v", err)
	}
}
