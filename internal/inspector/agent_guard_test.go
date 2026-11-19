package inspector

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Risk tiers are strict: read-only actions run immediately, controlled mutations are mutating_safe,
// and irreversible actions are destructive. Compound commands take the most dangerous tier.
func TestClassifyAgentCommand(t *testing.T) {
	cases := []struct {
		cmd  string
		want AgentRiskTier
	}{
		{"uptime", RiskReadOnly},
		{"docker logs --tail 50 web", RiskReadOnly},
		{"fleet_search nginx", RiskReadOnly},
		{"systemctl restart nginx.service", RiskMutatingSafe},
		{"docker restart web", RiskMutatingSafe},
		{"git fetch origin", RiskMutatingSafe},
		{"sudo systemctl stop cron.service; git fetch", RiskMutatingSafe},
		{"rm -rf /var/log", RiskDestructive},
		{"docker system prune -a", RiskDestructive},
		{"kubectl drain node-1 --ignore-daemonsets", RiskDestructive},
		{"terraform apply -auto-approve", RiskDestructive},
		{"terraform destroy", RiskDestructive},
		{"ufw delete allow 22", RiskDestructive},
		{"systemctl restart nginx && rm -rf /tmp/x", RiskDestructive},
		{"reboot", RiskDestructive},
	}
	for _, c := range cases {
		if got := ClassifyAgentCommand(c.cmd); got != c.want {
			t.Errorf("ClassifyAgentCommand(%q) = %s, want %s", c.cmd, got, c.want)
		}
	}
}

// Approval tokens are bound to the exact command so an approval cannot be replayed for another action.
func TestApprovalTokenBindsCommand(t *testing.T) {
	a := ApprovalTokenFor("rm -rf /var/log")
	b := ApprovalTokenFor("rm -rf /tmp")
	if a == b {
		t.Fatal("two different commands must have different approval tokens")
	}
	if a == "" || b == "" {
		t.Fatal("approval tokens must be non-empty")
	}
}

// Redaction keeps secret values out of the LLM context: assignments, bearer tokens and key blocks.
func TestRedactAgentSecrets(t *testing.T) {
	cases := map[string]string{
		"API_TOKEN=supersecret123":           "API_TOKEN=[REDACTED]",
		"password: hunter2":                  "password: [REDACTED]",
		"Authorization: Bearer abcdefgh1234": "Authorization: Bearer [REDACTED]",
		"all is fine":                        "all is fine",
	}
	for in, want := range cases {
		if got := RedactAgentSecrets(in); got != want {
			t.Errorf("RedactAgentSecrets(%q) = %q, want %q", in, got, want)
		}
	}
	key := "-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----"
	if got := RedactAgentSecrets(key); strings.Contains(got, "abc") || !strings.Contains(got, "[REDACTED]") {
		t.Errorf("private key block must be redacted: %q", got)
	}
}

// Output truncation keeps the head and the tail, announces the cut, and respects a short-input no-op.
func TestTruncateAgentOutput(t *testing.T) {
	long := strings.Repeat("H", 2000) + strings.Repeat("M", 1000) + strings.Repeat("T", 2000)
	got := TruncateAgentOutput(long, 3000, 50)
	if len(got) >= len(long) {
		t.Fatalf("oversized output must be cut: %d vs %d", len(got), len(long))
	}
	if !strings.HasPrefix(got, "HH") {
		t.Error("head must be kept: it says what ran")
	}
	if !strings.HasSuffix(got, "TT") {
		t.Error("tail must be kept: it holds the error")
	}
	if !strings.Contains(got, "truncated") || !strings.Contains(got, "chars omitted") {
		t.Fatalf("the cut must be announced: %q", got[:200])
	}
	if len(got) > 3300 {
		t.Errorf("result must stay near the cap: %d", len(got))
	}
	if TruncateAgentOutput("short", 3000, 50) != "short" {
		t.Error("a short result must be untouched")
	}
}

// A destructive command is refused without a matching approval token, even with a reachable runner.
func TestExecShellContextDestructiveRequiresApproval(t *testing.T) {
	ins := &Inspector{} // no runner needed: the gate fires before any SSH is attempted
	_, _, _, err := ins.ExecShellContext(context.Background(), 1, "rm -rf /var/log", time.Second, DefaultAgentExecLimits())
	if err == nil || !strings.Contains(err.Error(), "explicit operator approval") {
		t.Fatalf("destructive command must be blocked without approval: %v", err)
	}
	if !strings.Contains(err.Error(), ApprovalTokenFor("rm -rf /var/log")) {
		t.Error("the error must name the exact approval token the operator card shows")
	}
}

// Control characters in a model-supplied command are refused before a shell ever sees them.
func TestSanitizeAgentCommand(t *testing.T) {
	if err := sanitizeAgentCommand("uptime"); err != nil {
		t.Fatalf("a plain command must pass: %v", err)
	}
	if err := sanitizeAgentCommand("echo a\x00b"); err == nil {
		t.Fatal("a NUL byte must be refused")
	}
	if err := sanitizeAgentCommand("echo a\x01b"); err == nil {
		t.Fatal("a control character must be refused")
	}
	if err := sanitizeAgentCommand("printf 'a\nb'"); err != nil {
		t.Fatalf("newlines are allowed in shell scripts: %v", err)
	}
}
