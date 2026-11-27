//go:build unix

package teleport

import (
	"errors"
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

// ptyRun is a process attached to a PTY master. pty.StartWithSize makes the child a session leader, so its process
// group id equals its pid and Kill reaches tsh and anything tsh started.
type ptyRun struct {
	ptmx *os.File
	cmd  *exec.Cmd
}

// startPTYRun runs bin with args (no shell) in a new PTY of the given size.
func startPTYRun(bin string, args, env []string, cols, rows uint16) (*ptyRun, error) {
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: cols, Rows: rows})
	if err != nil {
		return nil, err
	}
	return &ptyRun{ptmx: ptmx, cmd: cmd}, nil
}

func (r *ptyRun) Read(b []byte) (int, error)  { return r.ptmx.Read(b) }
func (r *ptyRun) Write(b []byte) (int, error) { return r.ptmx.Write(b) }
func (r *ptyRun) Close() error                { return r.ptmx.Close() }

func (r *ptyRun) Resize(cols, rows uint16) error {
	return pty.Setsize(r.ptmx, &pty.Winsize{Cols: cols, Rows: rows})
}

// Wait blocks until the process exits and returns its exit code. Call it once.
func (r *ptyRun) Wait() (int, error) {
	err := r.cmd.Wait()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return -1, err
}

// Kill sends SIGKILL to the whole process group.
func (r *ptyRun) Kill() {
	if r.cmd.Process != nil {
		_ = syscall.Kill(-r.cmd.Process.Pid, syscall.SIGKILL)
	}
}
