package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"pantau/internal/ansible"
	"pantau/internal/inspector"
	"pantau/internal/store"
	"pantau/internal/terraform"
)

func init() { agentTools = append(agentTools, diagnoseTools...) }

var diagnoseTools = []agentTool{
	{
		Name:        "k8s_diagnose_pod",
		Description: "Composite read-only Kubernetes pod diagnosis in one call: pod phase/conditions, container lastState/exitCode/OOMKilled, filtered Warning events, and up to 40 relevant log lines (--previous when restarts > 0). Prefer this over raw kube_get + kube_describe + kube_logs.",
		InputSchema: schema([]string{"namespace", "pod"}, map[string]interface{}{
			"cluster":   pCluster,
			"target_id": pTarget,
			"namespace": pNS,
			"pod":       str,
		}),
		run: toolKubeDiagnosePod,
	},
	{
		Name:        "docker_diagnose_container",
		Description: "Composite read-only Docker container diagnosis in one call: docker inspect summary (state, OOMKilled, restart_count, mounts, networks, resource limits), recent error log tail, and stored misconfiguration audit + Trivy/Grype image vulnerability findings.",
		InputSchema: schema([]string{"host_id", "container"}, map[string]interface{}{
			"host_id":   pHost,
			"container": pName,
		}),
		run: toolDockerDiagnoseContainer,
	},
	{
		Name:        "docker_diagnose_stack",
		Description: "Composite read-only Docker Compose stack diagnosis in one call: per-container inspect state/limits/mounts/networks, error log tails for unhealthy or restarting containers, and stored misconfiguration audit + Trivy/Grype image findings.",
		InputSchema: schema([]string{"host_id", "stack"}, map[string]interface{}{
			"host_id": pHost,
			"stack":   str,
		}),
		run: toolDockerDiagnoseStack,
	},
	{
		Name:        "ansible_diagnose_run",
		Description: "Composite read-only Ansible run diagnosis: extracts only failed and unreachable tasks (with play, task, host and error message) from the run log and joins them with cached Ansible Facts for the affected hosts.",
		InputSchema: schema([]string{"run_id"}, map[string]interface{}{
			"run_id": pRunID,
		}),
		run: toolAnsibleDiagnoseRun,
	},
	{
		Name:        "terraform_analyze_plan",
		Description: "Read-only Terraform/OpenTofu plan analyzer: parses `terraform show -json` (or a stored run_id) into a compact +create/~update/-destroy/-/replace resource list with sensitive attributes masked, combined with Policy-as-Code findings, IaC/Secret scan findings, and Infracost delta.",
		InputSchema: schema(nil, map[string]interface{}{
			"run_id":       pRunID,
			"workspace_id": pWS,
			"plan_json":    map[string]interface{}{"type": "string", "description": "optional raw `terraform show -json` output to parse directly"},
		}),
		run: toolTerraformAnalyzePlan,
	},
}

// ---------------------------------------------------------------- Kubernetes

func toolKubeDiagnosePod(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	t, err := s.kubeTargetOf(args)
	if err != nil {
		return "", err
	}
	q := url.Values{"ns": {argStr(args, "namespace")}, "pod": {argStr(args, "pod")}}
	m, err := s.loopJSON(r.Context(), http.MethodGet, kubePath(t, "pod-diagnose", q), nil)
	if err != nil {
		return "", err
	}
	return jsonText(m["diagnosis"])
}

// ---------------------------------------------------------------- Docker

type dockerContainerDiagSummary struct {
	ID            string                             `json:"id"`
	Name          string                             `json:"name"`
	Image         string                             `json:"image"`
	Status        string                             `json:"status"`
	Running       bool                               `json:"running"`
	Paused        bool                               `json:"paused,omitempty"`
	Restarting    bool                               `json:"restarting,omitempty"`
	OOMKilled     bool                               `json:"oom_killed"`
	ExitCode      int                                `json:"exit_code"`
	Error         string                             `json:"error,omitempty"`
	RestartCount  int                                `json:"restart_count"`
	Health        string                             `json:"health,omitempty"`
	HealthLog     string                             `json:"health_log,omitempty"`
	RestartPolicy string                             `json:"restart_policy"`
	MemoryLimit   int64                              `json:"memory_limit"`
	CPUs          float64                            `json:"cpus"`
	Privileged    bool                               `json:"privileged,omitempty"`
	ComposeStack  string                             `json:"compose_stack,omitempty"`
	ComposeSvc    string                             `json:"compose_service,omitempty"`
	Mounts        []inspector.DockerMount            `json:"mounts,omitempty"`
	Networks      []inspector.DockerContainerNetwork `json:"networks,omitempty"`
	Ports         []inspector.DockerPortBinding      `json:"ports,omitempty"`
}

func summarizeDockerInspect(d *inspector.DockerContainerDetail) dockerContainerDiagSummary {
	return dockerContainerDiagSummary{
		ID:            d.ID,
		Name:          d.Name,
		Image:         d.Image,
		Status:        d.Status,
		Running:       d.Running,
		Paused:        d.Paused,
		Restarting:    d.Restarting,
		OOMKilled:     d.OOMKilled,
		ExitCode:      d.ExitCode,
		Error:         d.Error,
		RestartCount:  d.RestartCount,
		Health:        d.Health,
		HealthLog:     d.HealthLog,
		RestartPolicy: d.RestartPolicy,
		MemoryLimit:   d.MemoryLimit,
		CPUs:          d.CPUs,
		Privileged:    d.Privileged,
		ComposeStack:  d.Labels["com.docker.compose.project"],
		ComposeSvc:    d.Labels["com.docker.compose.service"],
		Mounts:        d.Mounts,
		Networks:      d.Networks,
		Ports:         d.Ports,
	}
}

func dockerSecurityFor(scans []store.DockerSecurityScan, containerNames map[string]bool, images map[string]bool) (misconfig []store.DockerSecurityFinding, imageScans []map[string]interface{}) {
	for _, sc := range scans {
		if sc.Kind == "misconfig" {
			for _, f := range sc.Findings {
				if containerNames[f.Container] {
					misconfig = append(misconfig, f)
				}
			}
			continue
		}
		if images[sc.Subject] {
			vulns := sc.Vulns
			if len(vulns) > 20 {
				vulns = vulns[:20]
			}
			imageScans = append(imageScans, map[string]interface{}{
				"image":       sc.Subject,
				"scanner":     sc.Scanner,
				"scanned_at":  sc.ScannedAt,
				"vuln_counts": sc.VulnCounts,
				"top_vulns":   vulns,
			})
		}
	}
	return misconfig, imageScans
}

func toolDockerDiagnoseContainer(s *Server, _ *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	container := argStr(args, "container")
	d, err := s.inspector.DockerInspectContainer(id, container)
	if err != nil {
		return "", err
	}

	var errLogs []string
	if rawLogs, lerr := s.inspector.DockerLogs(id, container, 200); lerr == nil {
		errLogs = inspector.ExtractDockerErrorLogs(rawLogs, 40)
	}

	scans, _ := s.db.ListDockerSecurityScans(id)
	misconfig, imgScans := dockerSecurityFor(scans, map[string]bool{d.Name: true, container: true}, map[string]bool{d.Image: true})

	return jsonText(map[string]interface{}{
		"inspect":            summarizeDockerInspect(d),
		"error_logs":         errLogs,
		"misconfig_findings": misconfig,
		"image_vulns":        imgScans,
	})
}

func toolDockerDiagnoseStack(s *Server, _ *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	stack := argStr(args, "stack")
	if !inspector.ValidDockerName(stack) {
		return "", fmt.Errorf("invalid stack name")
	}

	all, err := s.inspector.ListDockerContainers(id)
	if err != nil {
		return "", err
	}

	var stackContainers []map[string]interface{}
	names := map[string]bool{}
	images := map[string]bool{}

	for _, c := range all {
		d, ierr := s.inspector.DockerInspectContainer(id, c.Name)
		if ierr != nil {
			continue
		}
		if d.Labels["com.docker.compose.project"] != stack {
			continue
		}
		names[d.Name] = true
		images[d.Image] = true
		entry := map[string]interface{}{
			"inspect": summarizeDockerInspect(d),
		}
		if !d.Running || d.RestartCount > 0 || d.OOMKilled || d.ExitCode != 0 || d.Health == "unhealthy" {
			if rawLogs, lerr := s.inspector.DockerLogs(id, d.Name, 200); lerr == nil {
				entry["error_logs"] = inspector.ExtractDockerErrorLogs(rawLogs, 30)
			}
		}
		stackContainers = append(stackContainers, entry)
	}

	if len(stackContainers) == 0 {
		return "", fmt.Errorf("no containers found for compose stack %q", stack)
	}

	scans, _ := s.db.ListDockerSecurityScans(id)
	misconfig, imgScans := dockerSecurityFor(scans, names, images)

	return jsonText(map[string]interface{}{
		"stack":              stack,
		"containers":         stackContainers,
		"misconfig_findings": misconfig,
		"image_vulns":        imgScans,
	})
}

// ---------------------------------------------------------------- Ansible

func toolAnsibleDiagnoseRun(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	runID := argInt(args, "run_id")
	if runID < 1 {
		return "", fmt.Errorf("run_id is required")
	}
	m, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/ansible/runs/%d", runID), nil)
	if err != nil {
		return "", err
	}
	run, _ := m["run"].(map[string]interface{})
	l, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/ansible/runs/%d/log?offset=0", runID), nil)
	if err != nil {
		return "", err
	}
	logData, _ := l["data"].(string)
	failedTasks := ansible.ParseFailedTasks(logData)
	recap := ansible.ParseRecap(logData)

	affected := map[string]bool{}
	for _, ft := range failedTasks {
		affected[ft.Host] = true
	}
	for host, rec := range recap {
		if rec.Failed > 0 || rec.Unreachable > 0 {
			affected[host] = true
		}
	}

	hostFacts := map[string]interface{}{}
	if len(affected) > 0 && s.db != nil {
		hosts, _ := s.db.ListHosts()
		for _, h := range hosts {
			if !affected[h.Name] && !affected[strconv.FormatInt(h.ID, 10)] {
				continue
			}
			f, ferr := s.db.GetAnsibleHostFacts(h.ID)
			if ferr != nil || f == nil {
				continue
			}
			var sum interface{}
			if f.Summary != "" && f.Summary != "{}" {
				var m map[string]interface{}
				if err := json.Unmarshal([]byte(f.Summary), &m); err == nil && len(m) > 0 {
					sum = m
				}
			}
			if sum == nil && f.Facts != "" {
				var raw map[string]interface{}
				if err := json.Unmarshal([]byte(f.Facts), &raw); err == nil && len(raw) > 0 {
					sum = ansible.SummarizeFacts(raw)
				}
			}
			if sum != nil {
				hostFacts[h.Name] = map[string]interface{}{
					"host_id":     h.ID,
					"gathered_at": f.GatheredAt,
					"facts":       sum,
				}
			}
		}
	}

	return jsonText(map[string]interface{}{
		"run":          pick(run, "id", "playbook_id", "playbook_name", "status", "mode", "targets", "exit_code", "started_at", "finished_at"),
		"recap":        recap,
		"failed_tasks": failedTasks,
		"host_facts":   hostFacts,
	})
}

// checkAnsibleWorkflowRuns inspects recent runs of a playbook (since pbUpdatedAt) to verify
// whether syntax check and check --diff have passed.
func checkAnsibleWorkflowRuns(rawRuns interface{}, pbUpdatedAt string) (hasSyntax bool, diffRunID int64, diffRecap interface{}) {
	arr, _ := rawRuns.([]interface{})
	for _, it := range arr {
		rm, ok := it.(map[string]interface{})
		if !ok {
			continue
		}
		createdAt, _ := rm["created_at"].(string)
		if pbUpdatedAt != "" && createdAt != "" && createdAt < pbUpdatedAt {
			continue
		}
		status, _ := rm["status"].(string)
		mode, _ := rm["mode"].(string)
		if status != "success" {
			continue
		}
		switch mode {
		case "syntax":
			hasSyntax = true
		case "diff":
			if diffRunID == 0 {
				if fid, ok := rm["id"].(float64); ok {
					diffRunID = int64(fid)
				}
				diffRecap = rm["recap"]
			}
		}
	}
	return hasSyntax, diffRunID, diffRecap
}

func (s *Server) ansibleDiffSummary(ctx context.Context, diffRunID int64, diffRecap interface{}) string {
	var parts []string
	if diffRecap != nil {
		if b, err := json.Marshal(diffRecap); err == nil && string(b) != "null" {
			parts = append(parts, "recap: "+string(b))
		}
	}
	if l, err := s.loopJSON(ctx, http.MethodGet, fmt.Sprintf("/api/ansible/runs/%d/log?offset=0", diffRunID), nil); err == nil {
		if data, _ := l["data"].(string); data != "" {
			parts = append(parts, redactSecrets(tailOf(data, 2000)))
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("diff run #%d passed", diffRunID)
	}
	return strings.Join(parts, "\n")
}

// ---------------------------------------------------------------- Terraform

func toolTerraformAnalyzePlan(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	runID := argInt(args, "run_id")
	wsID := argInt(args, "workspace_id")
	planJSON := argStr(args, "plan_json")

	if runID < 1 && strings.TrimSpace(planJSON) == "" {
		return "", fmt.Errorf("run_id or plan_json is required")
	}

	out := map[string]interface{}{}
	if strings.TrimSpace(planJSON) != "" {
		analysis, err := terraform.AnalyzePlanJSON([]byte(planJSON))
		if err != nil {
			return "", err
		}
		out["plan_analysis"] = analysis
	}

	if runID > 0 {
		m, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/terraform/runs/%d", runID), nil)
		if err != nil {
			return "", err
		}
		run, _ := m["run"].(map[string]interface{})
		if wsID < 1 {
			if wid, ok := run["workspace_id"].(float64); ok {
				wsID = int64(wid)
			}
		}
		out["run"] = pick(run, "id", "workspace_id", "type", "status", "has_changes", "exit_code")
		if _, hasAnalysis := out["plan_analysis"]; !hasAnalysis {
			out["plan_summary"] = run["plan"]
		}
		out["policy"] = run["policy"]
		out["cost"] = run["cost"]
	}

	if wsID > 0 {
		if iac, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/iac-scan/terraform/%d", wsID), nil); err == nil {
			if sc, ok := iac["scan"].(map[string]interface{}); ok {
				capList(sc, "findings", secCap)
				out["iac_scan"] = sc
			}
		}
	}

	return jsonText(out)
}
