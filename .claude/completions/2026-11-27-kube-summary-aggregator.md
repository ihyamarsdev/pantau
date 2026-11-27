# Kubernetes single-call summary aggregator

Commits (branch feat/teleport-tsh-managed):
- 7aafdf3 feat(kube): single-call workload and cluster summaries, multi-kind kube_get
- 8f890e4 feat(kube): cluster summary tools for the agent and /mcp
- cd93821 feat(agent): cap repeated kubernetes lookups within one turn

## What changed
- Inspector: SummarizeWorkloads and SummarizeClusterInventory run one read-only kubectl call, parse it in Go, and
  cache the snapshot for 25s with singleflight (kube_summary.go). KubeList backs multi-kind kube_get (kube_list.go).
- Agent: kube_get takes kind lists, namespace '*'/'all', summary_only, unhealthy_only, selectors, limit, and returns
  total_count/truncated. New kube_workload_summary and kube_cluster_summary are in the core domain.
  find_tools ranks multi-word queries. The Kubernetes skill has anti-loop rules.
- Agent loop: per-turn guard (agent_guard.go). The 5th kube_get gets a hint; read-only calls beyond 12 per turn are
  refused; calls beyond 60 per turn are refused.
- MCP: pantau_k8s_workload_summary and pantau_k8s_cluster_summary (registered clusters and k3s/RKE2 host clusters,
  parallel, 10s deadline, live_error_code TELEPORT_SESSION_EXPIRED, output redacted by the existing /mcp path).

## Verification
- go test -race -count=1 ./... green at each milestone (26 packages ok).
- make check: exit 0 (vet, race tests, govulncheck: no vulnerabilities, gosec: 0 issues).
- BenchmarkSummarizeWorkloads (75 workloads, refresh each iteration): 334954 ns/op, 91680 B/op, 246 allocs/op.

## Known limits (shortcuts)
- kube_get legacy `what` path still returns the old endpoint shapes, not compact rows.
- Summary cache entries are never evicted (keys = targets x namespaces).
- by_namespace grows with namespace count; the < 4 KB payload was checked only on the 15-namespace fixture.
- Singleflight followers report cached=true.
- Context cancel only stops waiting; kubectl itself is bounded by the 15s ExecTimeout.
