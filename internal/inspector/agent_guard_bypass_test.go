package inspector

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Destructive commands that used to be classified read-only because the pattern was not at a command start.
func TestClassifyAgentCommand_DestructiveBypasses(t *testing.T) {
	for _, cmd := range []string{
		"{ rm -rf /var/lib/mysql; }",
		"sudo rm -rf /var/lib/docker",
		"sudo -n reboot",
		"sudo dd if=/dev/zero of=/dev/sda",
		"true\nrm -rf /srv/data",
		"xargs rm -rf",
		"( reboot )",
		"env FOO=1 shutdown now",
	} {
		if got := ClassifyAgentCommand(cmd); got != RiskDestructive {
			t.Errorf("ClassifyAgentCommand(%q) = %v, want destructive", cmd, got)
		}
	}
}

func TestClassifyAgentCommand_ReadOnlyStillReadOnly(t *testing.T) {
	for _, cmd := range []string{"df -h", "systemctl status nginx", "cat /etc/hosts | grep rm"} {
		if got := ClassifyAgentCommand(cmd); got != RiskReadOnly {
			t.Errorf("ClassifyAgentCommand(%q) = %v, want read_only", cmd, got)
		}
	}
}

// A destructive command is refused before any SSH work starts, even inside an approved batch.
func TestBatchInspectRefusesDestructiveCommand(t *testing.T) {
	var ins *Inspector // nil: the refusal must happen before the inspector is touched
	_, err := ins.BatchInspect(context.Background(), 1, []string{"df -h", "sudo rm -rf /var/lib/x"}, nil, time.Second, DefaultAgentExecLimits())
	if err == nil || !strings.Contains(err.Error(), "destructive") {
		t.Fatalf("destructive command accepted in batch: %v", err)
	}
}
