package inspector

import (
	"context"
	"fmt"
	"time"
)

// DockerLogs returns the last tail lines (1..1000, default 100) of a container's output, stdout and stderr merged.
func (ins *Inspector) DockerLogs(hostID int64, name string, tail int) (string, error) {
	if !ValidDockerName(name) {
		return "", dockerInvalid("invalid container name: %s", name)
	}
	if tail < 1 || tail > 1000 {
		tail = 100
	}
	stdout, stderr, code, err := ins.dockerExec(hostID, fmt.Sprintf("docker logs --tail %d %s 2>&1", tail, shellQuote(name)), dockerOpTimeout)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", dockerCommandError("docker logs", stdout, stderr, code)
	}
	// Large logs must never blow up the LLM context window. Keep the head and the tail.
	return TruncateAgentOutput(RedactAgentSecrets(stdout), 3000, 50), nil
}

// ExecShell runs one shell command as the host's SSH user. It performs no policy check: the AI agent gate
// (operator approval) lives in the web layer and every caller must pass it first.
func (ins *Inspector) ExecShell(hostID int64, cmd string, timeout time.Duration) (stdout, stderr string, exitCode int, err error) {
	return ins.ExecShellContext(context.Background(), hostID, cmd, timeout, DefaultAgentExecLimits())
}

// ExecShellContext runs one shell command with the agent's execution guardrails: strict context timeout,
// argument sanitization, risk-tier classification, secret redaction and head+tail output truncation.
//
// A read_only or mutating_safe command runs immediately. A destructive command is refused unless ctx
// carries the exact approval token from ApprovalTokenFor (an explicit human-in-the-loop decision, mirroring
// ADR 0044). The returned stdout/stderr are redacted and capped; the exit code and errors are preserved.
func (ins *Inspector) ExecShellContext(ctx context.Context, hostID int64, cmd string, timeout time.Duration, limits AgentExecLimits) (stdout, stderr string, exitCode int, err error) {
	// 1. Argument sanitization: NUL bytes and control characters other than tab/newline are never valid in
	//    a shell command coming from the model; they are a common injection vector.
	if err := sanitizeAgentCommand(cmd); err != nil {
		return "", "", -1, err
	}
	// 2. Risk tier classification and the destructive gate.
	tier := ClassifyAgentCommand(cmd)
	if tier == RiskDestructive {
		token, _ := ctx.Value(agentApprovalKey{}).(string)
		if token != ApprovalTokenFor(cmd) {
			return "", "", -1, fmt.Errorf("%w: %s (approval token %s)", ErrDestructiveNeedsApproval, cmd, ApprovalTokenFor(cmd))
		}
	}
	// 3. Strict context execution timeout: the shorter of the caller's timeout and the context deadline.
	runTimeout := limits.Timeout
	if runTimeout <= 0 {
		runTimeout = DefaultAgentExecLimits().Timeout
	}
	if timeout > 0 && timeout < runTimeout {
		runTimeout = timeout
	}
	if deadline, ok := ctx.Deadline(); ok {
		if remain := time.Until(deadline); remain > 0 && remain < runTimeout {
			runTimeout = remain
		} else if remain <= 0 {
			return "", "", -1, context.DeadlineExceeded
		}
	}
	runner, err := ins.dockerRunner(hostID)
	if err != nil {
		return "", "", -1, err
	}
	defer runner.Close()
	stdout, stderr, exitCode, err = runner.ExecTimeout("sh -c "+shellQuote(DarwinPathPrelude+cmd), runTimeout)
	// 4. Redaction and head+tail truncation keep large, secret-bearing output out of the LLM context.
	max := limits.MaxOutput
	if max <= 0 {
		max = DefaultAgentExecLimits().MaxOutput
	}
	stdout = TruncateAgentOutput(RedactAgentSecrets(stdout), max, limits.HeadFraction)
	stderr = TruncateAgentOutput(RedactAgentSecrets(stderr), max, limits.HeadFraction)
	return stdout, stderr, exitCode, err
}

// sanitizeAgentCommand refuses control characters and NUL bytes in a model-supplied command. Newlines and
// tabs are allowed (multi-line shell scripts are legitimate); other C0/C1 controls are not.
func sanitizeAgentCommand(cmd string) error {
	for _, r := range cmd {
		if r == '\n' || r == '\t' || r == '\r' {
			continue
		}
		if r < 0x20 || (r >= 0x7f && r < 0xa0) {
			return fmt.Errorf("command contains a disallowed control character U+%04X", r)
		}
	}
	return nil
}
