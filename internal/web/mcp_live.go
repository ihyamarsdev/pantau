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
