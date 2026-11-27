package teleport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"
)

// Result is the captured output of one command.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Runner runs one binary with an argv slice and an explicit environment. Tests substitute a fake, so the
// package's unit tests never start a real tsh or kubectl.
type Runner interface {
	Run(ctx context.Context, env []string, bin string, args ...string) (Result, error)
}

// ExecRunner is the production Runner. It calls exec.CommandContext directly: no shell, stdin is closed,
// and a stuck child is killed when ctx expires.
type ExecRunner struct{}

// Run returns an error only when the binary could not start or ctx ended. A non-zero exit is reported in
// Result.ExitCode so the caller can read the output.
func (ExecRunner) Run(ctx context.Context, env []string, bin string, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = env
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if err == nil {
		return res, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return res, fmt.Errorf("%s stopped: %w", filepath.Base(bin), ctxErr)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		return res, nil
	}
	return res, fmt.Errorf("start %s: %w", filepath.Base(bin), err)
}
