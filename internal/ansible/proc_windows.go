//go:build windows

package ansible

import (
	"os/exec"
)

func setProcessGroup(cmd *exec.Cmd) {
	// No process group setup needed on Windows stub
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
