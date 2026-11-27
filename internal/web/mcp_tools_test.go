package web

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"pantau/internal/inspector"
	"pantau/internal/store"
	"pantau/internal/teleport"
)

// The summary tools are read-only MCP tools, and their live queries are package variables. These tests replace
// them, so they must not run in parallel.

func TestMCPK8sSummaryToolsAreListed(t *testing.T) {
	for _, name := range []string{"pantau_k8s_workload_summary", "pantau_k8s_cluster_summary"} {
		tool, ok := mcpToolByName(name)
		if !ok {
			t.Fatalf("%s is not registered", name)
		}
		if tool.InputSchema["additionalProperties"] != false {
			t.Errorf("%s must refuse unknown arguments", name)
		}
	}
}

// Summaries answer for every registered cluster. One Teleport cluster with an expired session reports
// TELEPORT_SESSION_EXPIRED on its own row, and the healthy cluster still returns its counts.
func TestMCPK8sWorkloadSummaryDegradesPerCluster(t *testing.T) {
	srv, db, _ := mcpTestServer(t)
	addTeleportCluster(t, srv, db, t.TempDir(), false) // no isolated kubeconfig file: the session is expired
	okID, err := db.CreateKubeCluster(&store.KubeCluster{Name: "prod-eks", Provider: "eks", KubeMode: "full", Kubeconfig: "apiVersion: v1"})
	if err != nil {
		t.Fatal(err)
	}
	okTarget := inspector.KubeClusterTarget(okID)
	real := mcpKubeWorkloadFn
	mcpKubeWorkloadFn = func(s *Server, target int64, opts inspector.KubeWorkloadOpts) (*inspector.KubeWorkloadSummary, error) {
		if target == okTarget {
			return &inspector.KubeWorkloadSummary{Totals: inspector.KubeWorkloadTotals{TotalWorkloads: 7}}, nil
		}
		return real(s, target, opts) // the expired Teleport cluster runs the real code path
	}
	t.Cleanup(func() { mcpKubeWorkloadFn = real })

	text, isErr := mcpCallTool(t, srv, "pantau_k8s_workload_summary", `{}`)
	if isErr {
		t.Fatalf("summary failed as a whole: %s", text)
	}
	var out struct {
		Clusters []struct {
			Name    string `json:"name"`
			Summary struct {
				Totals struct {
					Total int `json:"total_workloads"`
				} `json:"totals"`
			} `json:"summary"`
			LiveError     string `json:"live_error"`
			LiveErrorCode string `json:"live_error_code"`
		} `json:"clusters"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	if len(out.Clusters) != 2 {
		t.Fatalf("clusters = %d, want 2 rows: %s", len(out.Clusters), text)
	}
	var sawExpired, sawHealthy bool
	for _, c := range out.Clusters {
		switch {
		case c.LiveErrorCode == teleport.CodeSessionExpired:
			sawExpired = true
		case c.Name == "prod-eks" && c.Summary.Totals.Total == 7 && c.LiveError == "":
			sawHealthy = true
		}
	}
	if !sawExpired || !sawHealthy {
		t.Errorf("rows = %s, want one expired Teleport row and one healthy row", text)
	}
}

func TestMCPK8sWorkloadSummaryRedactsSecrets(t *testing.T) {
	srv, db, _ := mcpTestServer(t)
	cid, err := db.CreateKubeCluster(&store.KubeCluster{Name: "prod-eks", Provider: "eks", KubeMode: "full", Kubeconfig: "apiVersion: v1"})
	if err != nil {
		t.Fatal(err)
	}
	real := mcpKubeWorkloadFn
	mcpKubeWorkloadFn = func(*Server, int64, inspector.KubeWorkloadOpts) (*inspector.KubeWorkloadSummary, error) {
		return &inspector.KubeWorkloadSummary{
			Unhealthy: []inspector.KubeObjectRef{{Kind: "deployment", Namespace: "shop", Name: "web password=s3cret", Reason: "NotReady"}},
		}, nil
	}
	t.Cleanup(func() { mcpKubeWorkloadFn = real })

	text, isErr := mcpCallTool(t, srv, "pantau_k8s_workload_summary", `{"cluster_id":`+itoa(cid)+`}`)
	if isErr {
		t.Fatalf("summary failed: %s", text)
	}
	if strings.Contains(text, "s3cret") || !strings.Contains(text, "[REDACTED]") {
		t.Errorf("secret not redacted in %s", text)
	}
}

func TestMCPK8sSummaryRejectsBadArguments(t *testing.T) {
	srv, db, _ := mcpTestServer(t)
	cid, err := db.CreateKubeCluster(&store.KubeCluster{Name: "prod-eks", Provider: "eks", KubeMode: "full", Kubeconfig: "apiVersion: v1"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		tool string
		args string
	}{
		{name: "unknown cluster", tool: "pantau_k8s_workload_summary", args: `{"cluster_id":999999}`},
		{name: "host that is not in a cluster", tool: "pantau_k8s_workload_summary", args: `{"host_id":999999}`},
		{name: "namespace injection", tool: "pantau_k8s_workload_summary", args: `{"cluster_id":` + itoa(cid) + `,"namespace":"x;rm"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, isErr := mcpCallTool(t, srv, tt.tool, tt.args); !isErr {
				t.Errorf("%s with %s succeeded, want a tool error", tt.tool, tt.args)
			}
		})
	}
}

func TestMCPK8sClusterSummaryReturnsInventory(t *testing.T) {
	srv, db, _ := mcpTestServer(t)
	cid, err := db.CreateKubeCluster(&store.KubeCluster{Name: "prod-eks", Provider: "eks", KubeMode: "full", Kubeconfig: "apiVersion: v1"})
	if err != nil {
		t.Fatal(err)
	}
	real := mcpKubeInventoryFn
	mcpKubeInventoryFn = func(_ *Server, target int64, _ bool) (*inspector.KubeClusterInventory, error) {
		if target != inspector.KubeClusterTarget(cid) {
			return nil, errors.New("unexpected target")
		}
		return &inspector.KubeClusterInventory{Nodes: inspector.KubeNodeCounts{Total: 3, Ready: 2, NotReady: 1}, AnomaliesTotal: 1}, nil
	}
	t.Cleanup(func() { mcpKubeInventoryFn = real })

	text, isErr := mcpCallTool(t, srv, "pantau_k8s_cluster_summary", `{"cluster_id":`+itoa(cid)+`}`)
	if isErr {
		t.Fatalf("inventory failed: %s", text)
	}
	var out struct {
		Clusters []struct {
			Summary struct {
				Nodes struct {
					NotReady int `json:"not_ready"`
				} `json:"nodes"`
				AnomaliesTotal int `json:"anomalies_total"`
			} `json:"summary"`
		} `json:"clusters"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	if len(out.Clusters) != 1 || out.Clusters[0].Summary.Nodes.NotReady != 1 || out.Clusters[0].Summary.AnomaliesTotal != 1 {
		t.Errorf("inventory = %s", text)
	}
}
