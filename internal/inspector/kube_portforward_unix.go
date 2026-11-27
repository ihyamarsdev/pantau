//go:build !windows

package inspector

import (
	"os/exec"
	"syscall"
	"time"
)

// setProcessGroup starts cmd in its own process group, so a stop reaches kubectl and anything it spawned.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// terminateProcessGroup sends SIGTERM to the whole group, escalates to SIGKILL after a grace period, and returns once
// the process has exited (exited is closed by the waiter).
func terminateProcessGroup(cmd *exec.Cmd, exited <-chan struct{}) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pgid := cmd.Process.Pid
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	grace := time.NewTimer(5 * time.Second)
	defer grace.Stop()
	select {
	case <-exited:
		return
	case <-grace.C:
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	<-exited
}
