package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/store"
	"pantau/internal/teleport"
)

// Live MCP queries run under hard deadlines. A slow or offline cluster degrades to an error entry for that
// cluster; it never hangs the MCP request. Every tool here is read-only: kubectl get/logs, docker inspect/logs,
// and GET requests to the Ansible and Terraform run APIs.
// Variables, not constants, so tests can shorten them. Production never changes them.
var (
	mcpLiveTimeout     = 10 * time.Second
	mcpDiagnoseTimeout = 20 * time.Second
)

// mcpKubeHealthLive is the live cluster query. It is a variable so tests can stand in for kubectl.
var mcpKubeHealthLive = func(s *Server, target int64) (*inspector.KubeClusterHealth, error) {
	return s.inspector.KubeClusterHealthLive(target)
}

type mcpLiveResult struct {
	id      int64
	health  *inspector.KubeClusterHealth
	errText string
	// code is a structured reason for errText, such as teleport.CodeSessionExpired. Empty for other failures.
	code string
}

// mcpTeleportExpiredText replaces tsh and kubectl output for an expired Teleport session. The client gets the code
// and the profile, never the raw command output.
const mcpTeleportExpiredText = "teleport session expired: log in again for this profile"

// mcpErrorPayload is a tool error that carries structured fields, so a client can branch on a code instead of
// parsing text. Message is also the plain error text.
type mcpErrorPayload struct {
	Message string
	Fields  map[string]any
}

func (e *mcpErrorPayload) Error() string { return e.Message }

// mcpClusterLive queries every cluster in parallel under one shared deadline. Clusters that answer in time report
// health; the rest report live_error, so one dead cluster cannot hide the others.
func mcpClusterLive(ctx context.Context, s *Server, clusters []store.KubeCluster) map[int64]mcpLiveResult {
	ctx, cancel := context.WithTimeout(ctx, mcpLiveTimeout)
	defer cancel()
	ch := make(chan mcpLiveResult, len(clusters)) // buffered: a late answer never blocks its goroutine
	query := mcpKubeHealthLive                    // read once here: goroutines below must not touch the package variable
	for _, c := range clusters {
		go func(id int64) {
			h, err := query(s, inspector.KubeClusterTarget(id))
			r := mcpLiveResult{id: id, health: h}
			if err != nil {
				r.errText = err.Error()
				if _, expired := teleportSessionFailure(err); expired {
					r.errText, r.code = mcpTeleportExpiredText, teleport.CodeSessionExpired
				}
			}
			ch <- r
		}(c.ID)
	}
	out := make(map[int64]mcpLiveResult, len(clusters))
	for len(out) < len(clusters) {
		select {
		case r := <-ch:
			out[r.id] = r
		case <-ctx.Done():
			for _, c := range clusters {
				if _, ok := out[c.ID]; !ok {
					out[c.ID] = mcpLiveResult{id: c.ID, errText: fmt.Sprintf("live query did not finish within %s", mcpLiveTimeout)}
				}
			}
			return out
		}
	}
	return out
}

// mcpWithin runs fn and stops waiting at the deadline. fn keeps running until its own bounded kubectl or HTTP call
// ends, and its result goes into a buffered channel, so nothing blocks or leaks past that point.
func mcpWithin(ctx context.Context, d time.Duration, fn func() (any, error)) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	type result struct {
		v   any
		err error
	}
	ch := make(chan result, 1)
	go func() {
		v, err := fn()
		ch <- result{v, err}
	}()
	select {
	case r := <-ch:
		return r.v, r.err
	case <-ctx.Done():
		return nil, fmt.Errorf("did not finish within %s", d)
	}
}

// mcpDiagnoseTools are the composite read-only diagnoses exposed over /mcp. They reuse the same pre-parsers as the
// in-process AI agent, so the answer and its size match what the agent sees.
var mcpDiagnoseTools = []mcpTool{
	{
		Name:        "pantau_diagnose_k8s_pod",
		Description: "Read-only diagnosis of one Kubernetes pod in one call: phase, conditions, container lastState/exitCode/OOMKilled, filtered Warning events, and up to 40 log lines (--previous after restarts). Uses a registered cluster id.",
		InputSchema: mcpSchema(map[string]any{
			"cluster_id": map[string]any{"type": "integer", "description": "Registered cluster id (from pantau_get_k8s_overview)"},
			"namespace":  map[string]any{"type": "string"},
			"pod":        map[string]any{"type": "string"},
		}, "cluster_id", "namespace", "pod"),
		run: mcpDiagnoseK8sPod,
	},
	{
		Name:        "pantau_diagnose_docker",
		Description: "Read-only diagnosis of one Docker container: docker inspect summary (state, OOMKilled, restart count, mounts, networks, limits), recent error log tail, and the stored misconfiguration audit and Trivy/Grype image findings.",
		InputSchema: mcpSchema(map[string]any{
			"host_id":   map[string]any{"type": "integer"},
			"container": map[string]any{"type": "string"},
		}, "host_id", "container"),
		run: mcpDiagnoseDocker,
	},
	{
		Name:        "pantau_diagnose_iac",
		Description: "Read-only diagnosis of one IaC run. kind=ansible: only failed and unreachable tasks with their errors, joined to cached facts of the affected hosts. kind=terraform: the +create/~update/-destroy/-/+replace summary with sensitive values masked, plus policy and scan findings. Never runs, applies or approves anything.",
		InputSchema: mcpSchema(map[string]any{
			"kind":   map[string]any{"type": "string", "enum": []string{"ansible", "terraform"}},
			"run_id": map[string]any{"type": "integer"},
		}, "kind", "run_id"),
		run: mcpDiagnoseIaC,
	},
}

func init() { mcpTools = append(mcpTools, mcpDiagnoseTools...) }

func mcpDiagnoseK8sPod(s *Server, ctx context.Context, args map[string]any) (any, error) {
	id, err := mcpArgInt(args, "cluster_id")
	if err != nil {
		return nil, err
	}
	ns, pod := mcpArgString(args, "namespace"), mcpArgString(args, "pod")
	return mcpWithin(ctx, mcpDiagnoseTimeout, func() (any, error) {
		res, err := s.inspector.KubeDiagnosePod(inspector.KubeClusterTarget(id), ns, pod)
		if profileID, expired := teleportSessionFailure(err); expired {
			return nil, &mcpErrorPayload{Message: mcpTeleportExpiredText, Fields: map[string]any{
				"ok": false, "error": mcpTeleportExpiredText, "live_error_code": teleport.CodeSessionExpired,
				"cluster_id": id, "teleport_profile_id": profileID,
			}}
		}
		return res, err
	})
}

func mcpDiagnoseDocker(s *Server, ctx context.Context, args map[string]any) (any, error) {
	return mcpWithin(ctx, mcpDiagnoseTimeout, func() (any, error) {
		text, err := toolDockerDiagnoseContainer(s, nil, args)
		if err != nil {
			return nil, err
		}
		return json.RawMessage(text), nil
	})
}

func mcpDiagnoseIaC(s *Server, ctx context.Context, args map[string]any) (any, error) {
	kind := mcpArgString(args, "kind")
	if kind != "ansible" && kind != "terraform" {
		return nil, fmt.Errorf("kind must be ansible or terraform")
	}
	id, err := mcpArgInt(args, "run_id")
	if err != nil {
		return nil, err
	}
	in := map[string]interface{}{"run_id": float64(id)}
	return mcpWithin(ctx, mcpDiagnoseTimeout, func() (any, error) {
		req := (&http.Request{}).WithContext(ctx)
		var text string
		var err error
		if kind == "ansible" {
			text, err = toolAnsibleDiagnoseRun(s, req, in)
		} else {
			text, err = toolTerraformAnalyzePlan(s, req, in)
		}
		if err != nil {
			return nil, err
		}
		return json.RawMessage(text), nil
	})
}

// mcpKubeTarget is one cluster an MCP summary can ask about: a registered cluster, or a k3s/RKE2 host cluster.
type mcpKubeTarget struct {
	name      string
	target    int64 // inspector target id
	clusterID int64 // registered cluster id, 0 for a host cluster
	hostID    int64 // host id of a host cluster, 0 for a registered cluster
}

// mcpKubeRow is one cluster in a summary answer. A cluster that fails keeps its row, with live_error.
type mcpKubeRow struct {
	Name          string `json:"name"`
	ClusterID     int64  `json:"cluster_id,omitempty"`
	HostID        int64  `json:"host_id,omitempty"`
	Summary       any    `json:"summary,omitempty"`
	LiveError     string `json:"live_error,omitempty"`
	LiveErrorCode string `json:"live_error_code,omitempty"`
}

// mcpKubeWorkloadFn and mcpKubeInventoryFn are the live queries behind the two summary tools. Tests replace them.
var (
	mcpKubeWorkloadFn = func(s *Server, target int64, opts inspector.KubeWorkloadOpts) (*inspector.KubeWorkloadSummary, error) {
		return s.inspector.SummarizeWorkloads(context.Background(), target, opts)
	}
	mcpKubeInventoryFn = func(s *Server, target int64, refresh bool) (*inspector.KubeClusterInventory, error) {
		return s.inspector.SummarizeClusterInventory(context.Background(), target, refresh)
	}
)

// mcpKubeTargets resolves the optional cluster_id and host_id arguments. With neither, it returns every cluster.
func mcpKubeTargets(s *Server, args map[string]any) ([]mcpKubeTarget, error) {
	clusterID, err := mcpOptionalInt(args, "cluster_id")
	if err != nil {
		return nil, err
	}
	hostID, err := mcpOptionalInt(args, "host_id")
	if err != nil {
		return nil, err
	}
	switch {
	case clusterID > 0:
		c, err := s.db.GetKubeCluster(clusterID)
		if err != nil {
			return nil, err
		}
		if c == nil {
			return nil, fmt.Errorf("no registered cluster with id %d", clusterID)
		}
		return []mcpKubeTarget{{name: c.Name, target: inspector.KubeClusterTarget(c.ID), clusterID: c.ID}}, nil
	case hostID > 0:
		h, err := s.db.GetHost(hostID)
		if err != nil || h == nil {
			return nil, fmt.Errorf("no host with id %d", hostID)
		}
		if h.KubeClusterID == "" {
			return nil, fmt.Errorf("host %d is not part of a Kubernetes cluster", hostID)
		}
		return []mcpKubeTarget{{name: h.Name, target: h.ID, hostID: h.ID}}, nil
	}
	clusters, err := s.db.ListKubeClusters()
	if err != nil {
		return nil, err
	}
	var out []mcpKubeTarget
	for _, c := range clusters {
		out = append(out, mcpKubeTarget{name: c.Name, target: inspector.KubeClusterTarget(c.ID), clusterID: c.ID})
	}
	for _, c := range s.kubeHostClusters() {
		out = append(out, mcpKubeTarget{name: c.Name, target: c.target().ID, hostID: c.target().ID})
	}
	return out, nil
}

// mcpOptionalInt reads an integer argument that may be absent; absent reads as 0.
func mcpOptionalInt(args map[string]any, key string) (int64, error) {
	if _, ok := args[key]; !ok {
		return 0, nil
	}
	return mcpArgInt(args, key)
}

// mcpKubeFanOut runs fn for every target in parallel under one mcpLiveTimeout deadline and keeps the target order.
// A slow or failed cluster reports live_error, and the rest still return.
func mcpKubeFanOut(ctx context.Context, targets []mcpKubeTarget, fn func(mcpKubeTarget) (any, error)) []mcpKubeRow {
	ctx, cancel := context.WithTimeout(ctx, mcpLiveTimeout)
	defer cancel()
	type result struct {
		i   int
		row mcpKubeRow
	}
	ch := make(chan result, len(targets)) // buffered: a late answer never blocks its goroutine
	for i, t := range targets {
		go func(i int, t mcpKubeTarget) {
			v, err := fn(t)
			row := mcpKubeRow{Name: t.name, ClusterID: t.clusterID, HostID: t.hostID, Summary: v}
			if err != nil {
				row.Summary = nil
				row.LiveError = err.Error()
				if _, expired := teleportSessionFailure(err); expired {
					row.LiveError, row.LiveErrorCode = mcpTeleportExpiredText, teleport.CodeSessionExpired
				}
			}
			ch <- result{i, row}
		}(i, t)
	}
	rows := make([]mcpKubeRow, len(targets))
	done := make([]bool, len(targets))
	for n := 0; n < len(targets); {
		select {
		case r := <-ch:
			rows[r.i], done[r.i] = r.row, true
			n++
		case <-ctx.Done():
			for i, t := range targets {
				if !done[i] {
					rows[i] = mcpKubeRow{Name: t.name, ClusterID: t.clusterID, HostID: t.hostID,
						LiveError: fmt.Sprintf("live query did not finish within %s", mcpLiveTimeout)}
				}
			}
			return rows
		}
	}
	return rows
}

func mcpK8sWorkloadSummary(s *Server, ctx context.Context, args map[string]any) (any, error) {
	targets, err := mcpKubeTargets(s, args)
	if err != nil {
		return nil, err
	}
	ns := mcpArgString(args, "namespace")
	if ns == "*" || ns == "all" {
		ns = ""
	}
	if ns != "" && !inspector.ValidKubeNamespace(ns) {
		return nil, fmt.Errorf("invalid namespace")
	}
	opts := inspector.KubeWorkloadOpts{Namespace: ns}
	opts.IncludePods, _ = args["include_pods"].(bool)
	opts.IncludeNames, _ = args["include_names"].(bool)
	opts.Refresh, _ = args["refresh"].(bool)
	rows := mcpKubeFanOut(ctx, targets, func(t mcpKubeTarget) (any, error) {
		sum, err := mcpKubeWorkloadFn(s, t.target, opts)
		if err != nil {
			return nil, err
		}
		return sum, nil
	})
	return struct {
		Clusters []mcpKubeRow `json:"clusters"`
	}{rows}, nil
}

func mcpK8sClusterSummary(s *Server, ctx context.Context, args map[string]any) (any, error) {
	targets, err := mcpKubeTargets(s, args)
	if err != nil {
		return nil, err
	}
	refresh, _ := args["refresh"].(bool)
	rows := mcpKubeFanOut(ctx, targets, func(t mcpKubeTarget) (any, error) {
		inv, err := mcpKubeInventoryFn(s, t.target, refresh)
		if err != nil {
			return nil, err
		}
		return inv, nil
	})
	return struct {
		Clusters []mcpKubeRow `json:"clusters"`
	}{rows}, nil
}
