package web

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"pantau/internal/inspector"
	"pantau/internal/store"
)

// The MCP tool registry. Every tool only reads: it queries the store and returns metadata. There is no tool for
// running commands, restarting, applying, writing SFTP or revealing a secret, and a name outside this list is refused.

type mcpTool struct {
	Name        string
	Description string
	InputSchema map[string]any
	run         func(s *Server, ctx context.Context, args map[string]any) (any, error)
}

// mcpMaxFieldBytes bounds raw JSON fields (scan findings, plan summaries) so one tool result stays readable.
const mcpMaxFieldBytes = 8 << 10

var mcpTools = []mcpTool{
	{
		Name:        "pantau_list_hosts",
		Description: "List managed hosts: group, status (online/down), OS, load, memory and disk use, and the number of active drift items.",
		InputSchema: mcpSchema(map[string]any{"group": map[string]any{"type": "string", "description": "Only hosts in this group"}}),
		run:         mcpListHosts,
	},
	{
		Name:        "pantau_get_host_detail",
		Description: "One host: last inspection, Desired State rules, active drift items, recent incidents with root-cause excerpts, top processes and the security digest.",
		InputSchema: mcpSchema(map[string]any{"host_id": map[string]any{"type": "integer"}}, "host_id"),
		run:         mcpGetHostDetail,
	},
	{
		Name:        "pantau_get_k8s_overview",
		Description: "Registered Kubernetes clusters (external kubeconfig clusters and k3s/RKE2 hosts) with their mode and identity. With include_live=true, adds a live read-only summary per cluster: node Ready state and unhealthy pods (CrashLoopBackOff, OOMKilled, Pending, Failed). Each live query has a 10-second limit; a cluster that is offline or slow reports live_error and the rest still return.",
		InputSchema: mcpSchema(map[string]any{"include_live": map[string]any{"type": "boolean", "description": "Query each cluster live (10s limit per cluster)"}}),
		run:         mcpGetK8sOverview,
	},
	{
		Name:        "pantau_get_docker_overview",
		Description: "Docker overview across hosts: container counts by state, stacks and the stored security issue total per host.",
		InputSchema: mcpSchema(map[string]any{}),
		run:         mcpGetDockerOverview,
	},
	{
		Name:        "pantau_get_devsecops_findings",
		Description: "Security digest per host (pending security updates, reboot flag, hardening failures), stored IaC and secret scan results, and file integrity drift.",
		InputSchema: mcpSchema(map[string]any{}),
		run:         mcpGetDevsecopsFindings,
	},
	{
		Name:        "pantau_get_iac_runs",
		Description: "The latest Ansible runs (status, mode, recap) and Terraform runs (status, type, plan summary).",
		InputSchema: mcpSchema(map[string]any{}),
		run:         mcpGetIacRuns,
	},
	{
		Name:        "pantau_list_vault_keys",
		Description: "Secrets Vault key names with scope, version and expiry. Metadata only: no values are returned.",
		InputSchema: mcpSchema(map[string]any{}),
		run:         mcpListVaultKeys,
	},
}

// mcpSchema builds a JSON Schema object for a tool's arguments, with the required names listed.
func mcpSchema(props map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

func mcpToolByName(name string) (mcpTool, bool) {
	for _, t := range mcpTools {
		if t.Name == name {
			return t, true
		}
	}
	return mcpTool{}, false
}

func mcpToolList() []map[string]any {
	out := make([]map[string]any, 0, len(mcpTools))
	for _, t := range mcpTools {
		out = append(out, map[string]any{"name": t.Name, "description": t.Description, "inputSchema": t.InputSchema})
	}
	return out
}

// mcpRedact applies the same secret filter the AI agent uses before text leaves Pantau.
func mcpRedact(s string) string { return inspector.RedactAgentSecrets(s) }

// mcpTruncate keeps a raw JSON field to a bounded size and marks the cut.
func mcpTruncate(raw string) string {
	if len(raw) <= mcpMaxFieldBytes {
		return raw
	}
	return raw[:mcpMaxFieldBytes] + "...[truncated]"
}

func mcpArgInt(args map[string]any, key string) (int64, error) {
	v, ok := args[key]
	if !ok {
		return 0, fmt.Errorf("%s is required", key)
	}
	switch n := v.(type) {
	case float64:
		if n != float64(int64(n)) {
			return 0, fmt.Errorf("%s must be an integer", key)
		}
		return int64(n), nil
	case string:
		return strconv.ParseInt(n, 10, 64)
	}
	return 0, fmt.Errorf("%s must be an integer", key)
}

func mcpArgString(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return s
}

type mcpHostRow struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	Host           string `json:"host"`
	Group          string `json:"group"`
	Online         bool   `json:"online"`
	OS             string `json:"os"`
	CPULoad        string `json:"cpu_load"`
	RAMUsedBytes   int64  `json:"ram_used_bytes"`
	RAMTotalBytes  int64  `json:"ram_total_bytes"`
	DiskUsedBytes  int64  `json:"disk_used_bytes"`
	DiskTotalBytes int64  `json:"disk_total_bytes"`
	DriftCount     int    `json:"drift_count"`
}

func mcpHostRowOf(h store.Host, drift int) mcpHostRow {
	return mcpHostRow{
		ID: h.ID, Name: h.Name, Host: h.Host, Group: h.GroupName, Online: h.InternetOnline,
		OS: h.OSInfo, CPULoad: h.CPULoad, RAMUsedBytes: h.RAMUsedBytes, RAMTotalBytes: h.RAMTotalBytes,
		DiskUsedBytes: h.DiskUsedBytes, DiskTotalBytes: h.DiskTotalBytes, DriftCount: drift,
	}
}

func (s *Server) driftCount(hostID int64) (int, error) {
	items, err := s.db.ListActualItems(hostID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, it := range items {
		if it.Status == "drift" {
			n++
		}
	}
	return n, nil
}

func mcpListHosts(s *Server, _ context.Context, args map[string]any) (any, error) {
	hosts, err := s.db.ListHosts()
	if err != nil {
		return nil, err
	}
	group := mcpArgString(args, "group")
	rows := make([]mcpHostRow, 0, len(hosts))
	for _, h := range hosts {
		if group != "" && h.GroupName != group {
			continue
		}
		drift, err := s.driftCount(h.ID)
		if err != nil {
			return nil, err
		}
		rows = append(rows, mcpHostRowOf(h, drift))
	}
	return map[string]any{"hosts": rows}, nil
}

func mcpGetHostDetail(s *Server, _ context.Context, args map[string]any) (any, error) {
	id, err := mcpArgInt(args, "host_id")
	if err != nil {
		return nil, err
	}
	h, err := s.db.GetHost(id)
	if err != nil {
		return nil, err
	}
	if h == nil {
		return nil, fmt.Errorf("host %d not found", id)
	}
	items, err := s.db.ListActualItems(id)
	if err != nil {
		return nil, err
	}
	drift := make([]store.ActualItem, 0)
	for _, it := range items {
		if it.Status == "drift" {
			drift = append(drift, it)
		}
	}
	rules, err := s.db.ListDesiredRules(id)
	if err != nil {
		return nil, err
	}
	incidents, err := s.db.ListIncidents(id, 5)
	if err != nil {
		return nil, err
	}
	summaries, err := s.db.ListSecuritySummaries()
	if err != nil {
		return nil, err
	}
	var topCPU, topRAM []store.ProcessInfo
	_ = json.Unmarshal([]byte(h.TopCPUProcesses), &topCPU)
	_ = json.Unmarshal([]byte(h.TopRAMProcesses), &topRAM)
	var security any
	if sum, ok := summaries[id]; ok {
		security = sum
	}
	return map[string]any{
		"host":           mcpHostRowOf(*h, len(drift)),
		"last_inspected": h.LastInspected,
		"rules":          rules,
		"drift":          drift,
		"incidents":      incidents,
		"top_cpu":        topCPU,
		"top_ram":        topRAM,
		"security":       security,
	}, nil
}

func mcpGetK8sOverview(s *Server, ctx context.Context, args map[string]any) (any, error) {
	clusters, err := s.db.ListKubeClusters()
	if err != nil {
		return nil, err
	}
	hosts, err := s.db.ListHosts()
	if err != nil {
		return nil, err
	}
	type clusterRow struct {
		ID        int64                        `json:"id"`
		Name      string                       `json:"name"`
		Provider  string                       `json:"provider"`
		Server    string                       `json:"server"`
		Mode      string                       `json:"kube_mode"`
		Live      *inspector.KubeClusterHealth `json:"live,omitempty"`
		LiveError string                       `json:"live_error,omitempty"`
		// LiveErrorCode is set for a structured failure such as an expired Teleport session.
		LiveErrorCode string `json:"live_error_code,omitempty"`
	}
	type hostRow struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		Mode      string `json:"kube_mode"`
		ClusterID string `json:"kube_cluster_id"`
	}
	out := struct {
		Clusters []clusterRow `json:"clusters"`
		Hosts    []hostRow    `json:"host_clusters"`
	}{Clusters: []clusterRow{}, Hosts: []hostRow{}}
	var live map[int64]mcpLiveResult
	if v, _ := args["include_live"].(bool); v {
		live = mcpClusterLive(ctx, s, clusters)
	}
	for _, c := range clusters {
		row := clusterRow{ID: c.ID, Name: c.Name, Provider: c.Provider, Server: c.Server, Mode: c.KubeMode}
		if r, ok := live[c.ID]; ok {
			row.Live, row.LiveError, row.LiveErrorCode = r.health, r.errText, r.code
		}
		out.Clusters = append(out.Clusters, row)
	}
	for _, h := range hosts {
		if h.KubeClusterID != "" {
			out.Hosts = append(out.Hosts, hostRow{ID: h.ID, Name: h.Name, Mode: h.KubeMode, ClusterID: h.KubeClusterID})
		}
	}
	return out, nil
}

func mcpGetDockerOverview(s *Server, _ context.Context, _ map[string]any) (any, error) {
	ov, err := s.db.GetDockerOverview(0, "")
	if err != nil {
		return nil, err
	}
	return ov, nil
}

func mcpGetDevsecopsFindings(s *Server, _ context.Context, _ map[string]any) (any, error) {
	summaries, err := s.db.ListSecuritySummaries()
	if err != nil {
		return nil, err
	}
	hosts, err := s.db.ListHosts()
	if err != nil {
		return nil, err
	}
	type fimRow struct {
		HostID  int64  `json:"host_id"`
		Host    string `json:"host"`
		Target  string `json:"target"`
		Details string `json:"details"`
	}
	fim := []fimRow{}
	for _, h := range hosts {
		items, err := s.db.ListActualItems(h.ID)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			if it.Kind == "file_integrity" && it.Status == "drift" {
				fim = append(fim, fimRow{HostID: h.ID, Host: h.Name, Target: it.Target, Details: mcpTruncate(it.Details)})
			}
		}
	}
	scans, err := s.db.ListIacScans()
	if err != nil {
		return nil, err
	}
	type scanRow struct {
		SourceKind string `json:"source_kind"`
		SourceID   int64  `json:"source_id"`
		Status     string `json:"status"`
		Files      int    `json:"files_scanned"`
		Summary    string `json:"summary"`
		Findings   string `json:"findings"`
	}
	iac := make([]scanRow, 0, len(scans))
	for _, sc := range scans {
		iac = append(iac, scanRow{
			SourceKind: sc.SourceKind, SourceID: sc.SourceID, Status: sc.Status, Files: sc.Files,
			Summary: mcpTruncate(string(sc.Summary)), Findings: mcpTruncate(string(sc.Findings)),
		})
	}
	return map[string]any{"hosts": summaries, "iac_scans": iac, "file_integrity_drift": fim}, nil
}

func mcpGetIacRuns(s *Server, _ context.Context, _ map[string]any) (any, error) {
	ansible, err := s.db.ListAnsibleRuns(0, "", 20)
	if err != nil {
		return nil, err
	}
	type ansibleRow struct {
		ID           int64  `json:"id"`
		PlaybookName string `json:"playbook"`
		Status       string `json:"status"`
		Mode         string `json:"mode"`
		Limit        string `json:"limit"`
		Recap        string `json:"recap"`
		CreatedAt    string `json:"created_at"`
	}
	arows := make([]ansibleRow, 0, len(ansible))
	for _, r := range ansible {
		arows = append(arows, ansibleRow{
			ID: r.ID, PlaybookName: r.PlaybookName, Status: r.Status, Mode: r.Mode, Limit: r.Limit,
			Recap: mcpTruncate(r.Recap), CreatedAt: r.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	tf, err := s.db.ListTFRunsFeed(store.TFRunFeedFilter{Limit: 20})
	if err != nil {
		return nil, err
	}
	type tfRow struct {
		ID          int64  `json:"id"`
		Workspace   string `json:"workspace"`
		Type        string `json:"type"`
		Status      string `json:"status"`
		Message     string `json:"message"`
		PlanSummary string `json:"plan_summary"`
	}
	trows := make([]tfRow, 0, len(tf))
	for _, r := range tf {
		trows = append(trows, tfRow{
			ID: r.Run.ID, Workspace: r.WorkspaceName, Type: r.Run.Type, Status: r.Run.Status,
			Message: r.Run.Message, PlanSummary: mcpTruncate(r.Run.PlanSummary),
		})
	}
	return map[string]any{"ansible": arows, "terraform": trows}, nil
}

func mcpListVaultKeys(s *Server, _ context.Context, _ map[string]any) (any, error) {
	secrets, err := s.db.ListVaultSecrets()
	if err != nil {
		return nil, err
	}
	return map[string]any{"keys": secrets}, nil
}
