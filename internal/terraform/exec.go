package terraform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// runCmd executes cmd inside its own process group, killing it when ctx is cancelled.
func runCmd(ctx context.Context, cmd *exec.Cmd) (int, error) {
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("failed to start %s: %w", filepath.Base(cmd.Path), err)
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	select {
	case <-ctx.Done():
		killProcessGroup(cmd)
		<-waitDone
		return 0, ctx.Err()
	case waitErr := <-waitDone:
		if waitErr == nil {
			return 0, nil
		}
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		return 0, waitErr
	}
}

// redactionWriter replaces occurrences of sensitive values with "(sensitive)" in written logs.
type redactionWriter struct {
	w       io.Writer
	mu      sync.Mutex
	secrets []string
}

func newRedactionWriter(w io.Writer, secrets []string) io.Writer {
	if w == nil {
		return io.Discard
	}
	var clean []string
	for _, s := range secrets {
		s = strings.TrimSpace(s)
		if len(s) >= 3 {
			clean = append(clean, s)
		}
	}
	if len(clean) == 0 {
		return w
	}
	return &redactionWriter{w: w, secrets: clean}
}

func (r *redactionWriter) Write(p []byte) (n int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	text := string(p)
	for _, s := range r.secrets {
		text = strings.ReplaceAll(text, s, "(sensitive)")
	}
	_, err = r.w.Write([]byte(text))
	if err != nil {
		return 0, err
	}
	return len(p), nil
}
