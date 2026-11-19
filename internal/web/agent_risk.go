package web

// Risk-tier routing for the in-process agent. The numeric gates (1 routine, 2 high impact, 3
// infrastructure) decide *whether* an operator is asked; the three strict tiers below decide *how* a
// command is executed inside the inspector (immediately, logged as mutating, or blocked behind an
// explicit approval token). They mirror ADR 0044's confirmation pattern and feed ADR 0030 telemetry.

import (
	"context"

	"pantau/internal/inspector"
)

// agentRiskTier is one of the three strict execution tiers.
type agentRiskTier = inspector.AgentRiskTier

const (
	riskReadOnly     = inspector.RiskReadOnly
	riskMutatingSafe = inspector.RiskMutatingSafe
	riskDestructive  = inspector.RiskDestructive
)

// riskTierOfTool classifies an agent tool by name (and, for shell tools, by its concrete command).
// Read tools and fleet search execute immediately; restart/fetch/refresh tools are mutating_safe and are
// logged to the Activity Console; shell, firewall, kube drain, Terraform apply/destroy and file deletion
// are destructive and require an explicit human-in-the-loop approval token.
func riskTierOfTool(name string, args map[string]interface{}) agentRiskTier {
	switch name {
	case "exec_command", "firewall_action", "file_action", "tf_apply", "tf_run", "kube_apply":
		return riskDestructive
	case "kube_action":
		if argStr(args, "action") == "drain" {
			return riskDestructive
		}
		return riskMutatingSafe
	}
	if cmd, ok := args["command"].(string); ok && inspector.ClassifyAgentCommand(cmd) == riskDestructive {
		return riskDestructive
	}
	// Mutating-safe tool families: service/container restart, git fetch, cache refresh, scans, sync.
	switch {
	case name == "systemd_action", name == "docker_action", name == "repo_sync",
		name == "inspect_host", name == "security_scan", name == "vpn_network_action",
		name == "pkg_updates", name == "docker_stack_action", name == "firewall_action":
		return riskMutatingSafe
	}
	return riskReadOnly
}

// withDestructiveApproval attaches the explicit human approval token to a request context. It is called
// only after agentApprove returned a positive operator decision for a destructive command.
func withDestructiveApproval(ctx context.Context, cmd string) context.Context {
	return inspector.WithAgentApproval(ctx, inspector.ApprovalTokenFor(cmd))
}
