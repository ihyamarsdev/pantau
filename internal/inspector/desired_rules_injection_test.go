package inspector

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A rule target must never be parsed as shell code on the remote login shell (bash).
func TestServiceBatchCmd_TargetIsNotShellCode(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "pwned")
	cmd := serviceBatchCmd([]string{"nginx", "x >(touch " + marker + ")"})
	_ = exec.Command("bash", "-c", cmd).Run()
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("target executed as shell code: %s", cmd)
	}
}
