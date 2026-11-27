package web

// Risk-tier routing for the in-process agent. The numeric gates (1 routine, 2 high impact, 3
// infrastructure) decide *whether* an operator is asked; the three strict tiers below decide *how* a
// command is executed inside the inspector (immediately, logged as mutating, or blocked behind an
// explicit approval token). They mirror ADR 0044's confirmation pattern and feed ADR 0030 telemetry.

import (
	"context"
	"slices"

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
	if tier, ok := iacActionTier(name, args); ok {
		return tier
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

// iacReadActions lists, per multi-action tool, the actions that only read. Any other action of these tools
// changes state, so the trace shows a write for it instead of read_only.
var iacReadActions = map[string][]string{
	"tf_workspace_data":       {"state", "outputs", "variables", "versions", "drift", "diff", "audit", "fmt", "validate"},
	"tf_catalog":              {"policies", "modules", "varsets", "templates", "registry_modules", "registry_providers", "inspect", "status", "varset_variables", "cloud_accounts", "importable_accounts"},
	"ansible_playbook_action": {"format", "analyze", "outline"},
	"ansible_schedule_action": {"list"},
	"ansible_resources":       {"status", "assets", "library", "roles", "config", "explorer", "facts"},
}

// iacWriteTools have no read action at all: every call changes state.
var iacWriteTools = map[string]bool{
	"tf_workspace_action":   true,
	"tf_workspace_scaffold": true,
	"tf_run_action":         true,
	"kube_cluster_action":   true,
	"repo_action":           true,
}

// iacActionTier classifies a multi-action infrastructure tool per action. apply and destroy change real
// infrastructure and are destructive; the other writes are mutating_safe. ok is false for tools that are not
// in these tables, which keep their default tier.
func iacActionTier(name string, args map[string]interface{}) (agentRiskTier, bool) {
	reads, multi := iacReadActions[name]
	if !multi && !iacWriteTools[name] {
		return "", false
	}
	action := argStr(args, "action")
	if name == "tf_run_action" && (action == "apply" || action == "destroy") {
		return riskDestructive, true
	}
	if slices.Contains(reads, action) {
		return riskReadOnly, true
	}
	return riskMutatingSafe, true
}

// withDestructiveApproval attaches the explicit human approval token to a request context. It is called
// only after agentApprove returned a positive operator decision for a destructive command.
func withDestructiveApproval(ctx context.Context, cmd string) context.Context {
	return inspector.WithAgentApproval(ctx, inspector.ApprovalTokenFor(cmd))
}
