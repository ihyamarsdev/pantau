package web

import (
	"testing"
)

// Multi-action infrastructure tools are classed per action: a read is read_only, every write is mutating_safe,
// and apply or destroy is destructive. Before this, a write such as a workspace update showed as read_only.
func TestRiskTierOfIaCActions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		action string
		want   agentRiskTier
	}{
		{"tf_workspace_action", "create", riskMutatingSafe},
		{"tf_workspace_action", "update", riskMutatingSafe},
		{"tf_workspace_action", "lock", riskMutatingSafe},
		{"tf_workspace_scaffold", "", riskMutatingSafe},
		{"tf_workspace_data", "state", riskReadOnly},
		{"tf_workspace_data", "validate", riskReadOnly},
		{"tf_workspace_data", "set_variable", riskMutatingSafe},
		{"tf_workspace_data", "rollback", riskMutatingSafe},
		{"tf_catalog", "templates", riskReadOnly},
		{"tf_catalog", "policies", riskReadOnly},
		{"tf_catalog", "create_policy", riskMutatingSafe},
		{"tf_catalog", "test_cloud_account", riskMutatingSafe},
		{"tf_run_action", "plan", riskMutatingSafe},
		{"tf_run_action", "apply", riskDestructive},
		{"tf_run_action", "destroy", riskDestructive},
		{"ansible_resources", "status", riskReadOnly},
		{"ansible_resources", "gather_facts", riskMutatingSafe},
		{"ansible_schedule_action", "list", riskReadOnly},
		{"ansible_schedule_action", "delete", riskMutatingSafe},
		{"kube_cluster_action", "rename", riskMutatingSafe},
		{"repo_action", "folder", riskMutatingSafe},
		{"list_hosts", "", riskReadOnly},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/"+tc.action, func(t *testing.T) {
			t.Parallel()
			args := map[string]interface{}{}
			if tc.action != "" {
				args["action"] = tc.action
			}
			if got := riskTierOfTool(tc.name, args); got != tc.want {
				t.Fatalf("riskTierOfTool(%s, %q) = %s, want %s", tc.name, tc.action, got, tc.want)
			}
		})
	}
}

// Risk-tier routing maps tools to the three strict execution tiers. Read-only tools execute immediately;
// controlled mutations are mutating_safe; shell, firewall, drain, Terraform apply/destroy and file
// deletion are destructive and require the human approval token.
func TestRiskTierOfTool(t *testing.T) {
	t.Parallel()
	cases := []struct {
		tool string
		args map[string]interface{}
		want agentRiskTier
	}{
		{"list_hosts", nil, riskReadOnly},
		{"fleet_search", map[string]interface{}{"query": "nginx"}, riskReadOnly},
		{"docker_logs", nil, riskReadOnly},
		{"systemd_action", map[string]interface{}{"action": "restart"}, riskMutatingSafe},
		{"docker_action", map[string]interface{}{"action": "restart"}, riskMutatingSafe},
		{"repo_sync", nil, riskMutatingSafe},
		{"exec_command", map[string]interface{}{"command": "uptime"}, riskDestructive},
		{"firewall_action", nil, riskDestructive},
		{"file_action", nil, riskDestructive},
		{"tf_apply", nil, riskDestructive},
		{"kube_action", map[string]interface{}{"action": "drain"}, riskDestructive},
		{"kube_action", map[string]interface{}{"action": "restart"}, riskMutatingSafe},
	}
	for _, c := range cases {
		if got := riskTierOfTool(c.tool, c.args); got != c.want {
			t.Errorf("riskTierOfTool(%q) = %s, want %s", c.tool, got, c.want)
		}
	}
}

// A shell tool whose concrete command is destructive is classified as destructive even though the tool
// itself is the generic exec_command.
func TestRiskTierOfDestructiveShellCommand(t *testing.T) {
	t.Parallel()
	if got := riskTierOfTool("exec_command", map[string]interface{}{"command": "rm -rf /var/log"}); got != riskDestructive {
		t.Fatalf("a destructive shell command must be destructive, got %s", got)
	}
}

// The descriptive domain names resolve to the same tool slices as the short aliases, so enabling a
// domain by its module name loads exactly one module's tools (plus core/meta).
func TestCanonicalDomainAliases(t *testing.T) {
	t.Parallel()
	cases := map[string]toolDomain{
		"compute_systemd":        domainHost,
		"docker_kubernetes":      domainInfra,
		"network_vpn_cloudflare": domainNetwork,
		"automation_git_iac":     domainAutomation,
		"security_audit":         domainSecurity,
		"host":                   domainHost,
		"infra":                  domainInfra,
		"network":                domainNetwork,
		"automation":             domainAutomation,
		"security":               domainSecurity,
		"repo":                   domainRepo,
		"not_a_domain":           "",
	}
	for name, want := range cases {
		if got := canonicalDomain(name); got != want {
			t.Errorf("canonicalDomain(%q) = %q, want %q", name, got, want)
		}
	}
	// A descriptive name advertises the same tools as its short alias.
	byModule := agentToolSpecsFor(canonicalDomain("network_vpn_cloudflare"))
	byAlias := agentToolSpecsFor(domainNetwork)
	if len(byModule) != len(byAlias) {
		t.Fatalf("domain alias must advertise the same tools: %d vs %d", len(byModule), len(byAlias))
	}
}

// The two new ADR 0075-0081 parity tools are registered and reachable in their domains.
func TestRecentParityToolsRegistered(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"vpn_host_status", "gateway_cli_install"} {
		if findAgentTool(name) == nil {
			t.Errorf("%s must be registered", name)
		}
	}
	if agentToolDomain("vpn_host_status") != domainNetwork {
		t.Error("vpn_host_status must be in the network domain")
	}
	if agentToolDomain("gateway_cli_install") != domainAutomation {
		t.Error("gateway_cli_install must be in the automation domain")
	}
	// gateway_cli_install is gate 3: never auto-approved, never grant-covered.
	tl := findAgentTool("gateway_cli_install")
	if tl == nil || tl.gate != gateInfra {
		t.Error("gateway_cli_install must be gate 3 (infrastructure)")
	}
}
