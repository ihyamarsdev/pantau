package ansible

import (
	"strings"
	"testing"
)

// Git over SSH must verify host keys: no StrictHostKeyChecking=no, and a recorded file, not /dev/null.
func TestGitSSHCommandVerifiesHostKeys(t *testing.T) {
	SetGitKnownHostsFile("/var/lib/pantau/git_known_hosts")
	t.Cleanup(func() { SetGitKnownHostsFile("") })
	cmd := GitSSHCommand()
	if strings.Contains(cmd, "StrictHostKeyChecking=no") || strings.Contains(cmd, "UserKnownHostsFile=/dev/null") {
		t.Fatalf("host key verification disabled: %s", cmd)
	}
	if !strings.Contains(cmd, "StrictHostKeyChecking=accept-new") || !strings.Contains(cmd, "/var/lib/pantau/git_known_hosts") {
		t.Fatalf("expected accept-new with the recorded file: %s", cmd)
	}
}
