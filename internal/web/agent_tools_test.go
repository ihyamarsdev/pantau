package web

import (
	"strings"
	"testing"
)

// findToolNames returns the tool names listed by find_tools, skipping the header row.
func findToolNames(out string) []string {
	var names []string
	for i, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if i == 0 {
			continue
		}
		names = append(names, strings.SplitN(line, "\t", 2)[0])
	}
	return names
}

func TestFindToolsRanksClusterSummariesFirst(t *testing.T) {
	t.Parallel()
	queries := []string{
		"kubernetes namespace summary count workloads",
		"workload",
		"count objects summary cluster inventory",
	}
	for _, q := range queries {
		t.Run(q, func(t *testing.T) {
			t.Parallel()
			out, err := toolFindTools(nil, nil, map[string]interface{}{"query": q})
			if err != nil {
				t.Fatal(err)
			}
			names := findToolNames(out)
			if len(names) < 2 {
				t.Fatalf("find_tools(%q) = %q, want at least two tools", q, out)
			}
			top := names[:2]
			if !(strings.Contains(strings.Join(top, " "), "kube_workload_summary") &&
				strings.Contains(strings.Join(top, " "), "kube_cluster_summary")) {
				t.Errorf("find_tools(%q) top two = %v, want kube_workload_summary and kube_cluster_summary", q, top)
			}
		})
	}
}

func TestFindToolsKeepsSingleTermBehaviour(t *testing.T) {
	t.Parallel()
	out, err := toolFindTools(nil, nil, map[string]interface{}{"query": "dns record"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "cf_dns_list") {
		t.Errorf("find_tools(dns record) = %q, want the cross-domain cf_dns_list", out)
	}
	if out, _ := toolFindTools(nil, nil, map[string]interface{}{"query": "zzz-no-such-tool"}); !strings.HasPrefix(out, "no tool matches") {
		t.Errorf("unmatched query = %q", out)
	}
}

func TestClusterSummariesAreAlwaysLoaded(t *testing.T) {
	t.Parallel()
	if got := agentToolDomain("kube_workload_summary"); got != domainCore {
		t.Errorf("kube_workload_summary domain = %q, want core", got)
	}
	if got := agentToolDomain("kube_cluster_summary"); got != domainCore {
		t.Errorf("kube_cluster_summary domain = %q, want core", got)
	}
	// Host-domain conversations must see them without find_tools.
	specs := agentToolSpecsFor(domainHost)
	have := map[string]bool{}
	for _, s := range specs {
		have[s.Name] = true
	}
	if !have["kube_workload_summary"] || !have["kube_cluster_summary"] {
		t.Errorf("host domain is missing the cluster summaries")
	}
}

func TestKubeDomainRouterAndSkillRules(t *testing.T) {
	t.Parallel()
	if got := detectDomainIntent("ada berapa workloads di cluster verihubs-staging-gke?"); got != domainKubernetes {
		t.Errorf("detectDomainIntent = %q, want kubernetes", got)
	}
	prompt := agentSkillPrompts[domainKubernetes]
	for _, want := range []string{"kube_workload_summary", "kube_cluster_summary", "Never call `kube_get` once per namespace"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("kubernetes skill is missing %q", want)
		}
	}
}
