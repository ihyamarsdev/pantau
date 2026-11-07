package terraform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type ApplySpec struct {
	Config
	RunID   int64
	Timeout time.Duration
}

type Output struct {
	Name      string          `json:"name"`
	Value     json.RawMessage `json:"value"`
	Type      json.RawMessage `json:"type"`
	Sensitive bool            `json:"sensitive"`
}

type ApplyResult struct {
	ExitCode int      `json:"exit_code"`
	State    []byte   `json:"state"`
	Outputs  []Output `json:"outputs"`
}

func (s ApplySpec) Validate() error {
	if err := s.Config.Validate(); err != nil {
		return err
	}
	if s.RunID <= 0 {
		return errors.New("run_id must be > 0")
	}
	return nil
}

// Apply executes `apply tfplan` in the maintained workdir of the Run, cleans up the run dir, and returns the resulting state and outputs.
func Apply(ctx context.Context, spec ApplySpec, log io.Writer) (*ApplyResult, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}

	binPath, err := exec.LookPath(spec.Binary)
	if err != nil {
		return nil, fmt.Errorf("%s binary not found in PATH: %w", spec.Binary, err)
	}

	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = 1 * time.Hour
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	defer CleanupRun(spec.Root, spec.RunID)

	runDir := filepath.Join(spec.Root, "runs", fmt.Sprintf("%d", spec.RunID))
	planPath := filepath.Join(runDir, "tfplan")
	if _, err := os.Stat(planPath); err != nil {
		return nil, fmt.Errorf("plan file tfplan not found for run %d: %w", spec.RunID, err)
	}

	// Workdir is runDir/work (or subdir in git clone)
	workDir := filepath.Join(runDir, "work")
	if spec.Git != nil && spec.WorkDir != "" {
		workDir = filepath.Join(runDir, "repo", filepath.FromSlash(spec.WorkDir))
	} else if spec.Git != nil {
		workDir = filepath.Join(runDir, "repo")
	}

	stateFilePath := filepath.Join(spec.Root, "terraform.tfstate")

	rLog := newRedactionWriter(log, spec.logSecrets())

	envCfg, cleanupEnv, err := spec.Config.withEnvFiles()
	if err != nil {
		return nil, err
	}
	defer cleanupEnv()
	env := envCfg.prepareEnv()

	// terraform apply -input=false -no-color tfplan (backend path comes from the override file in workDir)
	cmd := exec.Command(binPath, "apply", "-input=false", "-no-color", planPath)
	cmd.Dir = workDir
	cmd.Env = env
	cmd.Stdout = rLog
	cmd.Stderr = rLog

	exitCode, err := runCmd(runCtx, cmd)
	if err != nil {
		return nil, err
	}

	// Read state file
	var stateBytes []byte
	if sb, rErr := os.ReadFile(stateFilePath); rErr == nil {
		stateBytes = sb
	}

	var outputs []Output
	if len(stateBytes) > 0 {
		info, parseErr := ParseState(stateBytes)
		if parseErr == nil && info != nil {
			outputs = info.Outputs
		}
	}

	return &ApplyResult{
		ExitCode: exitCode,
		State:    stateBytes,
		Outputs:  outputs,
	}, nil
}
