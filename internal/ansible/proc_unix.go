//go:build !windows

package ansible

import (
	"os/exec"
	"syscall"
	"time"
)

func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup sends SIGTERM to the process group, waits up to 5s,
// and escalates to SIGKILL if the group is still alive.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pgid := cmd.Process.Pid
	if pgid <= 0 {
		return
	}
	// Kill negative pid targets the entire process group.
	_ = syscall.Kill(-pgid, syscall.SIGTERM)

	done := make(chan struct{})
	go func() {
		// Poll briefly to see if process exits, or wait up to 5s.
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			// syscall.Kill with signal 0 checks if process group exists.
			if err := syscall.Kill(-pgid, 0); err != nil {
				close(done)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}
