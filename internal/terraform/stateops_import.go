package terraform

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"pantau/internal/ansible"
)

// runImport prepares a throwaway work dir like Plan does (files or git clone, var files,
// backend override), runs init -reconfigure, then `import`. A non-zero exit leaves the
// code in the caller's result; only infrastructure failures return an error.
func (s StateOpSpec) runImport(ctx context.Context, log io.Writer, run func(dir string, args ...string) (bool, error)) error {
	base := filepath.Join(s.Root, "stateops")
	if err := os.MkdirAll(base, 0700); err != nil {
		return fmt.Errorf("failed to create stateops dir: %w", err)
	}
	dir, err := os.MkdirTemp(base, "op-")
	if err != nil {
		return fmt.Errorf("failed to create stateops dir: %w", err)
	}
	defer os.RemoveAll(dir)

	workDir := filepath.Join(dir, "work")
	if s.Files != nil {
		if err := os.MkdirAll(workDir, 0700); err != nil {
			return err
		}
		for name, content := range s.Files {
			if err := os.WriteFile(filepath.Join(workDir, name), []byte(content), 0600); err != nil {
				return fmt.Errorf("failed to write inline file %q: %w", name, err)
			}
		}
	} else {
		g := s.Git
		repoDir, _, err := ansible.CloneGitRepo(ctx, g.URL, g.Branch, g.Token, g.PrivateKey, dir, "https:ssh", log)
		if err != nil {
			return fmt.Errorf("git clone failed: %w", err)
		}
		workDir = repoDir
		if s.WorkDir != "" {
			workDir = filepath.Join(repoDir, filepath.FromSlash(s.WorkDir))
			rootReal, err := filepath.EvalSymlinks(repoDir)
			if err != nil {
				return err
			}
			targetReal, err := filepath.EvalSymlinks(workDir)
			if err != nil {
				return fmt.Errorf("workdir %q not found in repo: %w", s.WorkDir, err)
			}
			if rel, err := filepath.Rel(rootReal, targetReal); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return fmt.Errorf("workdir resolves outside repository")
			}
		}
	}

	if err := writeVarFiles(workDir, s.Vars); err != nil {
		return err
	}
	if err := writeBackendOverride(workDir, s.Root); err != nil {
		return err
	}
	if ok, err := run(workDir, "init", "-input=false", "-no-color", "-reconfigure"); !ok {
		return err
	}
	_, err = run(workDir, "import", "-input=false", "-no-color", s.Address, s.ImportID)
	return err
}
