package inspector

// Smart, context-aware SSH execution for the in-process AI agent (ADR 0086). Every model-supplied
// command passes through: VPN fast-fail (ADR 0077), interactive-command linting, non-interactive sudo,
// the 3-tier safety gate, a strict context deadline, OS-aware Darwin PATH prelude (ADR 0038), secret
// redaction and token-saving output compression (ADR 0031/0037).

import (
	"context"
	"fmt"
	"strings"
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
	return CompressAgentOutput(stdout, 3000, 50), nil
}

// ExecShell runs one shell command as the host's SSH user. It performs no policy check: the AI agent gate
// (operator approval) lives in the web layer and every caller must pass it first.
func (ins *Inspector) ExecShell(hostID int64, cmd string, timeout time.Duration) (stdout, stderr string, exitCode int, err error) {
	return ins.ExecShellContext(context.Background(), hostID, cmd, timeout, DefaultAgentExecLimits())
}

// ExecShellContext runs one shell command with the agent's full guardrail chain (ADR 0086):
//
//  1. VPN fast-fail: a host bound to a down network is refused before any dial (ADR 0077).
//  3. Pre-flight lint: interactive TUI/pager commands are refused with an actionable hint.
//  4. Risk tier: destructive commands require the exact approval token (ADR 0044), checked against the
//     original command so it matches the operator's approval card.
//  5. Non-interactive sudo: a bare `sudo` is rewritten to `sudo -n` (ADR 0037).
//  6. Strict deadline: the shorter of the caller timeout, limits and context deadline.
//  7. OS-aware execution: Darwin PATH prelude (ADR 0038) and per-session cwd (when a state is set).
//  8. Output: secrets redacted, consecutive duplicates collapsed, head+tail capped with metadata.
//
// A non-zero exit code is returned with exitCode and a recovery hint appended to stderr so the model
// can choose a better next call instead of repeating a failing command.
func (ins *Inspector) ExecShellContext(ctx context.Context, hostID int64, cmd string, timeout time.Duration, limits AgentExecLimits) (stdout, stderr string, exitCode int, err error) {
	// 1. VPN fast-fail — metadata only, never dials.
	if err := ins.checkVPNDown(hostID); err != nil {
		return "", "", -1, err
	}
	// 2. Argument sanitization: NUL bytes and C0/C1 controls other than tab/newline are never valid.
	if err := sanitizeAgentCommand(cmd); err != nil {
		return "", "", -1, err
	}
	// 3. Pre-flight anti-hang lint.
	if hint := LintInteractiveCommand(cmd); hint != "" {
		return "", "", -1, fmt.Errorf("pre-flight check refused the command: %s", hint)
	}
	// 4. Risk tier and the destructive gate. The token is derived from the *original* command so it
	//    matches the approval card the operator saw in the web layer (which also uses the original text).
	//    The sudo rewrite below does not change the tier: destructive patterns still match.
	tier := ClassifyAgentCommand(cmd)
	if tier == RiskDestructive {
		token, _ := ctx.Value(agentApprovalKey{}).(string)
		if token != ApprovalTokenFor(cmd) {
			return "", "", -1, fmt.Errorf("%w: %s (approval token %s)", ErrDestructiveNeedsApproval, cmd, ApprovalTokenFor(cmd))
		}
	}
	// 5. Non-interactive sudo (ADR 0037): rewrite only after the approval token was checked.
	cmd = sudoNoInteractive(cmd)

	// 6. Strict context execution timeout: the shorter of the caller's timeout and the context deadline.
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

	// 7. OS-aware execution: Darwin PATH prelude, then the session cwd if one was recorded.
	prefix := DarwinPathPrelude
	if st := sessionFromContext(ctx); st != nil {
		prefix += st.cwdPrefix(hostID)
	}
	stdout, stderr, exitCode, err = runner.ExecTimeout("sh -c "+shellQuote(prefix+cmd), runTimeout)

	// 8. Compress both streams: dedupe + redact + head/tail cap with metadata.
	max := limits.MaxOutput
	if max <= 0 {
		max = DefaultAgentExecLimits().MaxOutput
	}
	stdout = CompressAgentOutput(stdout, max, limits.HeadFraction)
	stderr = CompressAgentOutput(stderr, max, limits.HeadFraction)

	// 9. Structured recovery hints for non-zero exits / transport errors.
	if exitCode != 0 || err != nil {
		stderr += "\n[hint] " + exitHint(exitCode, err, cmd)
	}
	return stdout, stderr, exitCode, err
}

// WithAgentSession attaches a per-chat execution state (cwd tracking) to a context.
type agentSessionKey struct{}

// WithAgentSessionState returns a context carrying the given session execution state.
func WithAgentSessionState(ctx context.Context, st *AgentSessionState) context.Context {
	return context.WithValue(ctx, agentSessionKey{}, st)
}

// sessionFromContext reads the session state from ctx.
func sessionFromContext(ctx context.Context) *AgentSessionState {
	st, _ := ctx.Value(agentSessionKey{}).(*AgentSessionState)
	return st
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

// TrackSessionCD records a successful `cd` into the session state attached to ctx. Call it after an
// exec so subsequent commands in the same chat run relative to the new directory.
func TrackSessionCD(ctx context.Context, hostID int64, cmd string, exitCode int) {
	st := sessionFromContext(ctx)
	if st == nil || exitCode != 0 {
		return
	}
	dir := extractCD(strings.TrimSpace(cmd))
	if dir != "" && !strings.HasPrefix(dir, "/") {
		if cur := st.GetCWD(hostID); cur != "" {
			dir = strings.TrimSuffix(cur, "/") + "/" + dir
		}
	}
	if dir != "" {
		_ = st.SetCWD(hostID, dir)
	}
}
