package inspector

// Execution safety guardrails for the in-process AI agent's remote shell. These are defence in depth:
// the approval gate lives in the web layer (gate 1/2/3 via agentGate), but the inspector is the last
// place that turns a model-supplied string into a command on a managed host. It classifies the command
// into a risk tier, refuses commands that are destructive beyond an explicit operator request, redacts
// secret-looking values, and caps output before it can reach the LLM context.

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// AgentRiskTier is the strict classification of a remote execution action.
// Mirrors the agent's numeric gates (ADR 0066/0073): read_only -> gate 0,
// mutating_safe -> gate 1, destructive -> gate 2 (explicit human approval, never auto/granted).
type AgentRiskTier string

const (
	RiskReadOnly     AgentRiskTier = "read_only"
	RiskMutatingSafe AgentRiskTier = "mutating_safe"
	RiskDestructive  AgentRiskTier = "destructive"
)

// ErrDestructiveNeedsApproval is returned when a destructive command is executed without an explicit
// human-in-the-loop confirmation token. The web layer supplies that token only after an approval card.
var ErrDestructiveNeedsApproval = errors.New("destructive command requires explicit operator approval")

// agentApprovalKey is the context key carrying the explicit approval token for one destructive command.
type agentApprovalKey struct{}

// WithAgentApproval marks a context as carrying an explicit human approval for a destructive command.
// The token must be non-empty and must match the command it approves (see ExecShellGuard).
func WithAgentApproval(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, agentApprovalKey{}, token)
}

type AgentExecLimits struct {
	Timeout      time.Duration // hard execution timeout
	MaxOutput    int           // max chars per stream (stdout/stderr), head+tail
	HeadFraction int           // share of MaxOutput kept from the start (0..100), default 50
}

// DefaultAgentExecLimits returns the standard limits for an agent shell command.
func DefaultAgentExecLimits() AgentExecLimits {
	return AgentExecLimits{Timeout: 120 * time.Second, MaxOutput: 3000, HeadFraction: 50}
}

// destructiveCommandPatterns are shell fragments whose effect cannot be undone by a restart. Matching is
// conservative: a false positive only asks the operator for approval, a false negative would run a
// destructive command without it. Patterns are lowercased before matching.
var destructiveCommandPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(^|[;&|]\s*|\$\(\s*)rm\s+-[a-z]*[rRfF]`), // rm -rf, rm -fr, rm -f -r
	regexp.MustCompile(`(^|[;&|]\s*)mkfs\.`),
	regexp.MustCompile(`(^|[;&|]\s*)dd\s+`),
	regexp.MustCompile(`(^|[;&|]\s*)(shutdown|reboot|halt|poweroff)(\s|$)`),
	regexp.MustCompile(`(^|[;&|]\s*)docker\s+(system\s+prune|volume\s+(rm|prune)|image\s+rm|container\s+prune|rm\s+-f)`),
	regexp.MustCompile(`(^|[;&|]\s*)kubectl\s+(delete|drain|cordon|uncordon)\b`),
	regexp.MustCompile(`\bterraform\s+(apply|destroy)\b`),
	regexp.MustCompile(`\b(ufw|firewall-cmd|iptables|nft)\s+`),
	regexp.MustCompile(`\b(chmod|chown)\s+-R\b`),
	regexp.MustCompile(`\b(mv|cp)\s+.*\s+/dev/`),
	regexp.MustCompile(`(^|[;&|]\s*):\s*\(\s*\)\s*\{`), // fork bomb
}

// mutatingCommandPatterns are controlled changes that are safe to execute and log (ADR 0030) after the
// routine gate: service/container restart, git fetch, cache refresh.
var mutatingCommandPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\b(systemctl|service)\s+(start|stop|restart|reload|enable|disable|daemon-reload)\b`),
	regexp.MustCompile(`\bdocker\s+(start|stop|restart|pull)\b`),
	regexp.MustCompile(`\bgit\s+fetch\b`),
	regexp.MustCompile(`\b(cache|cache_flush|flush-cache|refresh)\b`),
}

// ClassifyAgentCommand assigns one of the three strict risk tiers to a shell command. Destructive wins
// over mutating, which wins over read-only, so a compound command is treated as its most dangerous part.
func ClassifyAgentCommand(cmd string) AgentRiskTier {
	s := strings.ToLower(strings.TrimSpace(cmd))
	if s == "" {
		return RiskReadOnly
	}
	for _, re := range destructiveCommandPatterns {
		if re.MatchString(s) {
			return RiskDestructive
		}
	}
	for _, re := range mutatingCommandPatterns {
		if re.MatchString(s) {
			return RiskMutatingSafe
		}
	}
	return RiskReadOnly
}

// ApprovalTokenFor returns the deterministic token the approval card shows for a destructive command.
// It is derived from the exact command, so an approval for one command cannot be replayed for another.
func ApprovalTokenFor(cmd string) string {
	return fmt.Sprintf("approve:%x", []byte(strings.TrimSpace(cmd)))
}

// RedactAgentSecrets replaces secret-looking values with [REDACTED] before output reaches the model.
// It covers KEY=value / key: value assignments, bearer tokens, private key blocks and common secret paths.
var (
	agentSecretAssign = regexp.MustCompile(`(?i)([a-z0-9_.-]*(?:pass(word|wd)?|secret|token|api[_-]?key|private[_-]?key|credential)[a-z0-9_.-]*)(\s*[:=]\s*)([^\s,;"']+)`)
	agentBearerRe     = regexp.MustCompile(`(?i)(bearer\s+)[a-z0-9._~+/=-]{8,}`)
	agentKeyBlockRe   = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)
)

func RedactAgentSecrets(s string) string {
	s = agentKeyBlockRe.ReplaceAllString(s, "[REDACTED]")
	s = agentBearerRe.ReplaceAllString(s, "${1}[REDACTED]")
	s = agentSecretAssign.ReplaceAllString(s, "${1}${3}[REDACTED]")
	return s
}

// TruncateAgentOutput caps a stream at max chars, keeping a head and a tail joined by an announced
// marker. A cap <= 0 or a short input is returned unchanged. The marker makes a cut never silent.
func TruncateAgentOutput(s string, max, headFraction int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	if headFraction <= 0 || headFraction >= 100 {
		headFraction = 50
	}
	head := max * headFraction / 100
	tail := max - head
	marker := fmt.Sprintf("\n…[agent output truncated: %d chars omitted]…\n", len(s)-head-tail)
	// Work in runes at the boundaries so a multi-byte character is never cut in half.
	h := head
	for h < len(s) && !utf8.RuneStart(s[h]) {
		h++
	}
	t := len(s) - tail
	for t > 0 && !utf8.RuneStart(s[t]) {
		t--
	}
	return s[:h] + marker + s[t:]
}
