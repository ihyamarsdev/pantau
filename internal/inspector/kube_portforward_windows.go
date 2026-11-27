//go:build windows

package inspector

import (
	"os/exec"
)

// setProcessGroup is a no-op on Windows: kubectl is stopped through its process handle.
func setProcessGroup(cmd *exec.Cmd) {}

// terminateProcessGroup kills the process and waits for the waiter to report the exit.
func terminateProcessGroup(cmd *exec.Cmd, exited <-chan struct{}) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
	<-exited
}
