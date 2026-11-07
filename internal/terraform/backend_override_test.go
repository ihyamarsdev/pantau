package terraform

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteBackendOverride(t *testing.T) {
	dir := t.TempDir()
	if err := writeBackendOverride(dir, filepath.Join(dir, `r${x}"`)); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, backendOverrideFile))
	s := string(b)
	if !strings.Contains(s, `backend "local"`) || !strings.Contains(s, `$${x}\"/terraform.tfstate`) {
		t.Fatalf("unexpected override: %s", s)
	}
}

func TestBackendOverridePlanApply(t *testing.T) {
	const main = `resource "terraform_data" "x" {
  input = "v"
}
`
	cases := map[string]string{
		"plain":      main,
		"s3_backend": "terraform {\n  backend \"s3\" {}\n}\n\n" + main,
	}
	for _, bin := range []string{"terraform", "tofu"} {
		for name, src := range cases {
			t.Run(bin+"/"+name, func(t *testing.T) {
				if _, err := exec.LookPath(bin); err != nil {
					t.Skipf("%s not in PATH", bin)
				}
				dataRoot := t.TempDir()
				cfg := Config{
					Binary:   bin,
					DataRoot: dataRoot,
					Root:     filepath.Join(dataRoot, "ws-1"),
					Files:    map[string]string{"main.tf": src},
				}
				ctx := context.Background()
				var log bytes.Buffer
				p1, err := Plan(ctx, PlanSpec{Config: cfg, RunID: 1, Timeout: time.Minute}, &log)
				if err != nil || !p1.HasChanges {
					t.Fatalf("plan 1: %v changes=%v\n%s", err, p1 != nil && p1.HasChanges, log.String())
				}
				a, err := Apply(ctx, ApplySpec{Config: cfg, RunID: 1, Timeout: time.Minute}, &log)
				if err != nil || a.ExitCode != 0 || len(a.State) == 0 {
					t.Fatalf("apply: %v\n%s", err, log.String())
				}
				// State persists in the file and between runs: second plan sees no changes.
				if _, err := os.Stat(filepath.Join(cfg.Root, "terraform.tfstate")); err != nil {
					t.Fatalf("state file missing: %v", err)
				}
				cfg.State = a.State
				p2, err := Plan(ctx, PlanSpec{Config: cfg, RunID: 2, Timeout: time.Minute}, &log)
				if err != nil || p2.HasChanges || p2.ExitCode != 0 {
					t.Fatalf("plan 2: %v\n%s", err, log.String())
				}
				if strings.Contains(log.String(), "Deprecated flag") {
					t.Fatalf("log contains deprecation warning:\n%s", log.String())
				}
			})
		}
	}
}
