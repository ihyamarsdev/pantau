package inspector

// ssh_batch_inspect: several read-only diagnostic probes in one SSH round-trip (ADR 0086). The
// commands are built into one POSIX script with labeled sections and executed through the same
// guardrail chain as exec_command, then parsed back into labeled results for the model.

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// BatchInspect runs several read-only commands on one host in a single SSH round-trip and returns
// cleanly labeled sections. Every command is linted for interactive hangs and forced read-only by
// convention: the caller (web layer) supplies only diagnostic probes. The whole batch is classified
// with ClassifyAgentCommand; a destructive command anywhere in the batch requires approval.
func (ins *Inspector) BatchInspect(ctx context.Context, hostID int64, commands, labels []string, timeout time.Duration, limits AgentExecLimits) (string, error) {
	if len(commands) == 0 {
		return "", fmt.Errorf("batch needs at least one command")
	}
	const maxBatch = 20
	if len(commands) > maxBatch {
		return "", fmt.Errorf("batch is limited to %d commands", maxBatch)
	}
	for _, cmd := range commands {
		if hint := LintInteractiveCommand(cmd); hint != "" {
			return "", fmt.Errorf("pre-flight check refused a batch command: %s", hint)
		}
	}
	script, names := BuildBatchScript(commands, labels)
	stdout, stderr, code, err := ins.ExecShellContext(ctx, hostID, script, timeout, limits)
	if err != nil && code == -1 {
		return "", err
	}
	sections := ParseBatchOutput(stdout)
	var b strings.Builder
	fmt.Fprintf(&b, "batch: %d probes on host %d (exit %d)\n", len(commands), hostID, code)
	for _, name := range names {
		r, ok := sections[name]
		if !ok {
			fmt.Fprintf(&b, "\n--- %s ---\n(no output)\n", name)
			continue
		}
		fmt.Fprintf(&b, "\n--- %s (exit %d) ---\n%s\n", name, r.ExitCode, strings.TrimSpace(r.Stdout))
	}
	if strings.TrimSpace(stderr) != "" {
		fmt.Fprintf(&b, "\n--- batch stderr ---\n%s\n", strings.TrimSpace(stderr))
	}
	return b.String(), nil
}
