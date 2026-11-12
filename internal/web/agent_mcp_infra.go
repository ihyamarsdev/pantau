package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"pantau/internal/inspector"
)

// Agent tools for Docker, Kubernetes, Ansible and Terraform (ADR-0066).
//
// Docker goes straight to the inspector, like the host tools. Kubernetes, Ansible and Terraform call the same HTTP
// handlers the UI uses, in-process (loopback below), so their rules apply unchanged: Kubernetes read-only mode,
// Ansible/Terraform gates, workspace locks, plan policy checks, approval comments.
//
// Gates: 1 routine, 2 high impact (drain, compose down), 3 infrastructure change (terraform apply, ansible real run,
// kubectl apply): gate 3 is never auto-approved and never covered by a session grant.

func init() { agentTools = append(agentTools, infraTools...) }

const (
	gateRoutine = 1
	gateHigh    = 2
	gateInfra   = 3
	logTail     = 12 << 10
)

var (
	pTarget = map[string]interface{}{"type": "integer", "description": "Kubernetes target: a host id from list_hosts that runs kubectl, or the negative target_id of a cluster from kube_targets"}
	pNS     = map[string]interface{}{"type": "string", "description": "Kubernetes namespace"}
	pRunID  = map[string]interface{}{"type": "integer"}
	pWS     = map[string]interface{}{"type": "integer", "description": "Terraform workspace id from tf_workspaces"}
	pPB     = map[string]interface{}{"type": "integer", "description": "Ansible playbook id from ansible_playbooks"}
	str     = map[string]interface{}{"type": "string"}
)

var infraTools = []agentTool{
	// ---- Docker
	{Name: "docker_inspect", Description: "Detailed state of one container: exit code, OOM, restarts, health, ports, mounts, networks. Secret-looking env values are masked.",
		InputSchema: schema([]string{"host_id", "container"}, map[string]interface{}{"host_id": pHost, "container": pName}), run: toolDockerInspect},
	{Name: "docker_info", Description: "Docker overview of a host: what = stats (CPU/memory per container), images, or stacks (Compose projects with status).",
		InputSchema: schema([]string{"host_id", "what"}, map[string]interface{}{"host_id": pHost, "what": map[string]interface{}{"type": "string", "enum": []string{"stats", "images", "stacks"}}}), run: toolDockerInfo},
	{Name: "docker_stack_file", Description: "The compose file of a Compose stack (from docker_info stacks). Secret-looking values are masked.",
		InputSchema: schema([]string{"host_id", "stack"}, map[string]interface{}{"host_id": pHost, "stack": str}), run: toolDockerStackFile},
	{Name: "docker_stack_action", Description: "Compose action on a stack: up, start, stop, restart, pull (routine) or down, recreate, update (high impact). May wait for operator approval.", gate: gateRoutine,
		InputSchema: schema([]string{"host_id", "stack", "action"}, map[string]interface{}{"host_id": pHost, "stack": str, "action": map[string]interface{}{"type": "string", "enum": []string{"up", "start", "stop", "restart", "pull", "down", "recreate", "update"}}}), run: toolDockerStackAction},

	// ---- Kubernetes
	{Name: "kube_targets", Description: "Kubernetes targets: registered clusters (negative target_id) and hosts that run kubectl (their host id), with access mode (full, no-exec, read-only).",
		InputSchema: schema(nil, map[string]interface{}{}), run: toolKubeTargets},
	{Name: "kube_get", Description: "List Kubernetes objects: what = nodes, namespaces, workloads, pods, services or events. namespace is optional (empty = all).",
		InputSchema: schema([]string{"target_id", "what"}, map[string]interface{}{"target_id": pTarget, "what": map[string]interface{}{"type": "string", "enum": []string{"nodes", "namespaces", "workloads", "pods", "services", "events"}}, "namespace": pNS}), run: toolKubeGet},
	{Name: "kube_describe", Description: "kubectl describe for one object (secret-looking values masked).",
		InputSchema: schema([]string{"target_id", "kind", "name"}, map[string]interface{}{"target_id": pTarget, "kind": map[string]interface{}{"type": "string", "description": "pod, deployment, node, service, ..."}, "namespace": pNS, "name": str}), run: toolKubeDescribe},
	{Name: "kube_logs", Description: "Last lines of a pod's logs.",
		InputSchema: schema([]string{"target_id", "namespace", "pod"}, map[string]interface{}{"target_id": pTarget, "namespace": pNS, "pod": str, "container": str, "tail": map[string]interface{}{"type": "integer", "description": "lines, default 100"}}), run: toolKubeLogs},
	{Name: "kube_diff", Description: "Preview what kubectl apply of a manifest would change (read-only).",
		InputSchema: schema([]string{"target_id", "yaml"}, map[string]interface{}{"target_id": pTarget, "yaml": str}), run: toolKubeDiff},
	{Name: "kube_action", Description: "scale (deployment/statefulset, needs replicas), restart (rollout restart of deployment/statefulset/daemonset), delete (kind pod only), cordon, uncordon, drain (node). Drain is high impact. Refused when the target is read-only. May wait for operator approval.", gate: gateRoutine,
		InputSchema: schema([]string{"target_id", "op", "kind", "name"}, map[string]interface{}{"target_id": pTarget, "op": map[string]interface{}{"type": "string", "enum": []string{"scale", "restart", "delete", "cordon", "uncordon", "drain"}}, "kind": str, "namespace": pNS, "name": str, "replicas": map[string]interface{}{"type": "integer"}}), run: toolKubeAction},
	{Name: "kube_apply", Description: "kubectl apply a YAML manifest. Always waits for explicit operator approval, every time. Run kube_diff first. Refused when the target is read-only.", gate: gateInfra,
		InputSchema: schema([]string{"target_id", "yaml"}, map[string]interface{}{"target_id": pTarget, "yaml": str}), run: toolKubeApply},

	// ---- Ansible
	{Name: "ansible_playbooks", Description: "List Ansible playbooks, or with playbook_id show one (inline source, extra vars, survey).",
		InputSchema: schema(nil, map[string]interface{}{"playbook_id": pPB}), run: toolAnsiblePlaybooks},
	{Name: "ansible_run", Description: "Run a playbook on hosts. mode: syntax, check (dry run), diff (dry run with diff) or run (changes hosts). Real runs always wait for explicit operator approval, every time: do a check run first. Returns a run id; read progress with ansible_runs.", gate: gateRoutine,
		InputSchema: schema([]string{"playbook_id", "mode"}, map[string]interface{}{"playbook_id": pPB, "mode": map[string]interface{}{"type": "string", "enum": []string{"syntax", "check", "diff", "run"}}, "host_ids": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "integer"}}, "group": str, "limit": str, "tags": str, "skip_tags": str, "extra_vars": map[string]interface{}{"type": "object"}, "become": map[string]interface{}{"type": "boolean"}}), run: toolAnsibleRun},
	{Name: "ansible_runs", Description: "Recent Ansible runs, or with run_id the status, recap and the end of the log of one run.",
		InputSchema: schema(nil, map[string]interface{}{"run_id": pRunID, "playbook_id": pPB, "limit": map[string]interface{}{"type": "integer"}}), run: toolAnsibleRuns},

	// ---- Terraform
	{Name: "tf_workspaces", Description: "List Terraform workspaces, or with workspace_id show one including its configuration files (secret-looking values masked).",
		InputSchema: schema(nil, map[string]interface{}{"workspace_id": pWS}), run: toolTFWorkspaces},
	{Name: "tf_runs", Description: "Recent runs of a workspace (workspace_id), or one run (run_id) with plan summary, policy result and the end of its log.",
		InputSchema: schema(nil, map[string]interface{}{"workspace_id": pWS, "run_id": pRunID}), run: toolTFRuns},
	{Name: "tf_plan", Description: "Start a plan (type plan, or destroy to plan a destroy; nothing is changed). It locks the workspace until applied or discarded. Read the result with tf_runs.", gate: gateRoutine,
		InputSchema: schema([]string{"workspace_id"}, map[string]interface{}{"workspace_id": pWS, "type": map[string]interface{}{"type": "string", "enum": []string{"plan", "destroy"}}, "message": str}), run: toolTFPlan},
	{Name: "tf_apply", Description: "Apply a finished plan (run in status awaiting_approval) to real infrastructure. Always waits for explicit operator approval, every time. A plan whose policy check warns or fails is refused: tell the operator.", gate: gateInfra,
		InputSchema: schema([]string{"run_id", "comment"}, map[string]interface{}{"run_id": pRunID, "comment": map[string]interface{}{"type": "string", "description": "why, at least 3 characters"}}), run: toolTFApply},
}

// ---------------------------------------------------------------- loopback

// memResp is a minimal http.ResponseWriter that keeps the body in memory.
type memResp struct {
	h    http.Header
	code int
	buf  bytes.Buffer
}

func (m *memResp) Header() http.Header         { return m.h }
func (m *memResp) Write(b []byte) (int, error) { return m.buf.Write(b) }
func (m *memResp) WriteHeader(c int)           { m.code = c }

// loopback runs a request through the real router with a short-lived session, so every handler rule applies.
// Paths are built by the tools from integers and url-encoded values only.
func (s *Server) loopback(ctx context.Context, method, path string, body interface{}) (int, []byte) {
	tok := randHex(16)
	s.sessions.Store(tok, time.Now().Add(2*time.Minute))
	defer s.sessions.Delete(tok)
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, path, rd)
	if err != nil {
		return http.StatusBadRequest, []byte(err.Error())
	}
	req.Host = "127.0.0.1"
	req.RemoteAddr = "127.0.0.1:0"
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.AddCookie(&http.Cookie{Name: "pantau_session", Value: tok})
	rec := &memResp{h: http.Header{}, code: http.StatusOK}
	s.mux.ServeHTTP(rec, req)
	return rec.code, rec.buf.Bytes()
}

// loopJSON decodes a successful JSON answer into a map. A non-2xx status, or Kubernetes' {"ok":false}, is an error.
func (s *Server) loopJSON(ctx context.Context, method, path string, body interface{}) (map[string]interface{}, error) {
	code, b := s.loopback(ctx, method, path, body)
	var m map[string]interface{}
	_ = json.Unmarshal(b, &m)
	if code/100 != 2 || m["ok"] == false {
		msg, _ := m["error"].(string)
		if msg == "" {
			msg = strings.TrimSpace(string(b))
			if len(msg) > 300 {
				msg = msg[:300]
			}
		}
		return nil, fmt.Errorf("%s (HTTP %d)", msg, code)
	}
	if m == nil {
		return nil, fmt.Errorf("unexpected answer from %s", path)
	}
	return m, nil
}

// ---------------------------------------------------------------- helpers

func argInt(args map[string]interface{}, k string) int64 {
	f, _ := args[k].(float64)
	return int64(f)
}

func pick(m map[string]interface{}, keys ...string) map[string]interface{} {
	out := make(map[string]interface{}, len(keys))
	for _, k := range keys {
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	return out
}

func pickAll(list interface{}, keys ...string) []interface{} {
	arr, _ := list.([]interface{})
	out := make([]interface{}, 0, len(arr))
	for _, it := range arr {
		if m, ok := it.(map[string]interface{}); ok {
			out = append(out, pick(m, keys...))
		}
	}
	return out
}

// secretLineRe matches "NAME=value" or "name: value" lines whose name looks like a credential.
// shortcut: name-based masking, a secret under an ordinary name is not caught; upgrade with a real secret scanner.
var secretLineRe = regexp.MustCompile(`(?im)^(\s*-?\s*["']?[A-Za-z0-9_.-]*(?:pass(?:word|wd)?|secret|token|api_?key|private_?key|credential|auth)[A-Za-z0-9_.-]*["']?\s*[:=]\s*).+$`)

func redactSecrets(s string) string { return secretLineRe.ReplaceAllString(s, "${1}***") }

func tailOf(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

func clipTo(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ---------------------------------------------------------------- Docker

func toolDockerInspect(s *Server, _ *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	d, err := s.inspector.DockerInspectContainer(id, argStr(args, "container"))
	if err != nil {
		return "", err
	}
	for i, e := range d.Env {
		d.Env[i] = redactSecrets(e)
	}
	return jsonText(d)
}

func toolDockerInfo(s *Server, _ *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	var v interface{}
	switch argStr(args, "what") {
	case "stats":
		v, err = s.inspector.ListDockerStats(id)
	case "images":
		v, err = s.inspector.DockerListImages(id)
	case "stacks":
		v, err = s.inspector.DockerListStacks(id)
	default:
		return "", fmt.Errorf("what must be stats, images or stacks")
	}
	if err != nil {
		return "", err
	}
	return jsonText(v)
}

func toolDockerStackFile(s *Server, _ *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	p, content, err := s.inspector.DockerStackFile(id, argStr(args, "stack"))
	if err != nil {
		return "", err
	}
	return p + "\n" + redactSecrets(content), nil
}

func toolDockerStackAction(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	stack, action := argStr(args, "stack"), argStr(args, "action")
	gate := gateRoutine
	switch action {
	case "up", "start", "stop", "restart", "pull":
	case "down", "recreate", "update":
		gate = gateHigh
	default:
		return "", fmt.Errorf("unsupported action %q", action)
	}
	if !inspector.ValidDockerName(stack) {
		return "", fmt.Errorf("invalid stack name")
	}
	if err := s.agentGate(r, id, "docker_stack_action", "docker compose "+action+" (project "+stack+")", gate); err != nil {
		return "", err
	}
	return s.inspector.DockerStackOp(id, stack, action, nil)
}

// ---------------------------------------------------------------- Kubernetes

type kubeTarget struct {
	ID   int64
	Name string
	Mode string // full | no-exec | read-only
}

func (s *Server) kubeTargetOf(args map[string]interface{}) (*kubeTarget, error) {
	id := argInt(args, "target_id")
	if id == 0 {
		return nil, fmt.Errorf("target_id is required (host id, or negative cluster target_id from kube_targets)")
	}
	if inspector.IsKubeClusterTarget(id) {
		c, err := s.db.GetKubeCluster(-id)
		if err != nil || c == nil {
			return nil, fmt.Errorf("cluster not found")
		}
		return &kubeTarget{id, c.Name, c.KubeMode}, nil
	}
	h, err := s.db.GetHost(id)
	if err != nil || h == nil {
		return nil, fmt.Errorf("host not found")
	}
	return &kubeTarget{id, h.Name, h.KubeMode}, nil
}

func (s *Server) kubeWritable(args map[string]interface{}) (*kubeTarget, error) {
	t, err := s.kubeTargetOf(args)
	if err != nil {
		return nil, err
	}
	if t.Mode == "read-only" {
		return nil, fmt.Errorf("kubernetes target %q is in read-only mode; the operator must change it in Pantau", t.Name)
	}
	return t, nil
}

func kubePath(t *kubeTarget, sub string, q url.Values) string {
	p := fmt.Sprintf("/api/hosts/%d/kube/%s", t.ID, sub)
	if len(q) > 0 {
		p += "?" + q.Encode()
	}
	return p
}

func toolKubeTargets(s *Server, _ *http.Request, _ map[string]interface{}) (string, error) {
	clusters, err := s.db.ListKubeClusters()
	if err != nil {
		return "", err
	}
	type row struct {
		TargetID int64  `json:"target_id"`
		Name     string `json:"name"`
		Kind     string `json:"kind"`
		Mode     string `json:"mode"`
	}
	var out []row
	for _, c := range clusters {
		out = append(out, row{-c.ID, c.Name, "cluster (" + c.Provider + ")", c.KubeMode})
	}
	hosts, _ := s.db.ListHosts()
	for _, h := range hosts {
		if h.KubeClusterID != "" {
			out = append(out, row{h.ID, h.Name, "host with kubectl", h.KubeMode})
		}
	}
	if len(out) == 0 {
		return "no Kubernetes targets known; run Detect on a host in the Pantau Kubernetes tab or add a cluster", nil
	}
	return jsonText(out)
}

func toolKubeGet(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	t, err := s.kubeTargetOf(args)
	if err != nil {
		return "", err
	}
	what := argStr(args, "what")
	switch what {
	case "nodes", "namespaces", "workloads", "pods", "services", "events":
	default:
		return "", fmt.Errorf("what must be nodes, namespaces, workloads, pods, services or events")
	}
	q := url.Values{}
	if ns := argStr(args, "namespace"); ns != "" && what != "nodes" && what != "namespaces" {
		q.Set("ns", ns)
	}
	m, err := s.loopJSON(r.Context(), http.MethodGet, kubePath(t, what, q), nil)
	if err != nil {
		return "", err
	}
	return jsonText(m[what])
}

func toolKubeDescribe(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	t, err := s.kubeTargetOf(args)
	if err != nil {
		return "", err
	}
	q := url.Values{"kind": {argStr(args, "kind")}, "ns": {argStr(args, "namespace")}, "name": {argStr(args, "name")}}
	m, err := s.loopJSON(r.Context(), http.MethodGet, kubePath(t, "describe", q), nil)
	if err != nil {
		return "", err
	}
	out, _ := m["output"].(string)
	return redactSecrets(out), nil
}

func toolKubeLogs(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	t, err := s.kubeTargetOf(args)
	if err != nil {
		return "", err
	}
	tail := argInt(args, "tail")
	if tail < 1 || tail > 1000 {
		tail = 100
	}
	q := url.Values{"ns": {argStr(args, "namespace")}, "pod": {argStr(args, "pod")}, "container": {argStr(args, "container")}, "tail": {strconv.FormatInt(tail, 10)}}
	m, err := s.loopJSON(r.Context(), http.MethodGet, kubePath(t, "logs", q), nil)
	if err != nil {
		return "", err
	}
	out, _ := m["output"].(string)
	return out, nil
}

func toolKubeDiff(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	t, err := s.kubeTargetOf(args)
	if err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodPost, kubePath(t, "diff", nil), map[string]string{"yaml": argStr(args, "yaml")})
	if err != nil {
		return "", err
	}
	out, _ := m["output"].(string)
	if m["changed"] == false {
		return "no changes", nil
	}
	return redactSecrets(out), nil
}

func toolKubeAction(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	t, err := s.kubeWritable(args)
	if err != nil {
		return "", err
	}
	req := inspector.KubeActionReq{Op: argStr(args, "op"), Kind: argStr(args, "kind"), Namespace: argStr(args, "namespace"), Name: argStr(args, "name")}
	if f, ok := args["replicas"].(float64); ok {
		n := int(f)
		req.Replicas = &n
	}
	if err := inspector.ValidateKubeAction(req); err != nil {
		return "", err
	}
	gate := gateRoutine
	if strings.EqualFold(req.Op, "drain") {
		gate = gateHigh
	}
	summary := fmt.Sprintf("kubectl %s %s/%s", req.Op, req.Kind, req.Name)
	if req.Namespace != "" {
		summary += " -n " + req.Namespace
	}
	if req.Replicas != nil {
		summary += fmt.Sprintf(" --replicas=%d", *req.Replicas)
	}
	if err := s.agentGateAs(r, t.ID, t.Name, "kube_action", summary, gate); err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodPost, kubePath(t, "action", nil), req)
	if err != nil {
		return "", err
	}
	out, _ := m["output"].(string)
	return out, nil
}

func toolKubeApply(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	t, err := s.kubeWritable(args)
	if err != nil {
		return "", err
	}
	y := argStr(args, "yaml")
	if err := inspector.ValidateKubeApply(y); err != nil {
		return "", err
	}
	if err := s.agentGateAs(r, t.ID, t.Name, "kube_apply", "kubectl apply -f - on "+t.Name+":\n"+clipTo(y, 3000), gateInfra); err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodPost, kubePath(t, "apply", nil), map[string]string{"yaml": y})
	if err != nil {
		return "", err
	}
	out, _ := m["output"].(string)
	return out, nil
}

// ---------------------------------------------------------------- Ansible

func toolAnsiblePlaybooks(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	if id := argInt(args, "playbook_id"); id > 0 {
		m, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/ansible/playbooks/%d", id), nil)
		if err != nil {
			return "", err
		}
		pb, _ := m["playbook"].(map[string]interface{})
		out := pick(pb, "id", "name", "description", "source", "git_url", "git_branch", "git_path", "extra_vars", "survey", "group_name", "last_sync_commit", "updated_at")
		if c, ok := pb["content"].(string); ok {
			out["content"] = redactSecrets(clipTo(c, 16<<10))
		}
		return jsonText(out)
	}
	m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/ansible/playbooks", nil)
	if err != nil {
		return "", err
	}
	return jsonText(pickAll(m["playbooks"], "id", "name", "description", "source", "group_name", "updated_at"))
}

func toolAnsibleRun(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id := argInt(args, "playbook_id")
	if id < 1 {
		return "", fmt.Errorf("playbook_id is required")
	}
	mode := argStr(args, "mode")
	switch mode {
	case "syntax", "check", "diff", "run":
	default:
		return "", fmt.Errorf("mode must be syntax, check, diff or run")
	}
	pbm, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/ansible/playbooks/%d", id), nil)
	if err != nil {
		return "", err
	}
	pb, _ := pbm["playbook"].(map[string]interface{})
	body := map[string]interface{}{"mode": mode, "group": argStr(args, "group"), "limit": argStr(args, "limit"), "tags": argStr(args, "tags"), "skip_tags": argStr(args, "skip_tags")}
	if b, ok := args["become"].(bool); ok {
		body["become"] = b
	}
	if ids, ok := args["host_ids"].([]interface{}); ok {
		var hs []int64
		for _, v := range ids {
			if f, ok := v.(float64); ok {
				hs = append(hs, int64(f))
			}
		}
		body["host_ids"] = hs
	}
	if ev, ok := args["extra_vars"].(map[string]interface{}); ok && len(ev) > 0 {
		b, _ := json.Marshal(ev)
		body["extra_vars"] = string(b)
	}
	gate := gateRoutine
	if mode == "run" {
		gate = gateInfra
	}
	b, _ := json.Marshal(body)
	name, _ := pb["name"].(string)
	if err := s.agentGateAs(r, 0, "Ansible", "ansible_run", fmt.Sprintf("ansible playbook %q (id %d), mode %s\n%s", name, id, mode, clipTo(string(b), 1500)), gate); err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodPost, fmt.Sprintf("/api/ansible/playbooks/%d/run", id), body)
	if err != nil {
		return "", err
	}
	run, _ := m["run"].(map[string]interface{})
	return jsonText(pick(run, "id", "status", "mode", "targets", "playbook_name"))
}

func toolAnsibleRuns(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	if id := argInt(args, "run_id"); id > 0 {
		m, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/ansible/runs/%d", id), nil)
		if err != nil {
			return "", err
		}
		run, _ := m["run"].(map[string]interface{})
		out := map[string]interface{}{"run": pick(run, "id", "playbook_name", "status", "mode", "targets", "limit", "tags", "exit_code", "created_at", "started_at", "finished_at"), "recap": m["recap"]}
		if l, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/ansible/runs/%d/log?offset=0", id), nil); err == nil {
			data, _ := l["data"].(string)
			out["log_tail"] = tailOf(data, logTail)
		}
		return jsonText(out)
	}
	q := url.Values{"limit": {"20"}}
	if l := argInt(args, "limit"); l > 0 && l <= 100 {
		q.Set("limit", strconv.FormatInt(l, 10))
	}
	if p := argInt(args, "playbook_id"); p > 0 {
		q.Set("playbook_id", strconv.FormatInt(p, 10))
	}
	m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/ansible/runs?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	return jsonText(pickAll(m["runs"], "id", "playbook_name", "status", "mode", "trigger", "exit_code", "created_at", "finished_at"))
}

// ---------------------------------------------------------------- Terraform

func toolTFWorkspaces(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	if id := argInt(args, "workspace_id"); id > 0 {
		m, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/terraform/workspaces/%d", id), nil)
		if err != nil {
			return "", err
		}
		ws, _ := m["workspace"].(map[string]interface{})
		out := pick(ws, "id", "name", "description", "environment", "binary", "source", "git_url", "git_branch", "git_path", "auto_apply", "status", "locked", "lock_reason", "drift_detected", "drift_checked_at", "tf_version", "resource_count", "last_run")
		if files, ok := ws["files"].(map[string]interface{}); ok {
			red := map[string]interface{}{}
			for n, c := range files {
				if txt, ok := c.(string); ok {
					red[n] = redactSecrets(clipTo(txt, 8<<10))
				}
			}
			out["files"] = red
		}
		return jsonText(out)
	}
	m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/terraform/workspaces", nil)
	if err != nil {
		return "", err
	}
	// Picked field by field: the raw workspace JSON carries the webhook URL, which embeds a secret token.
	return jsonText(pickAll(m["workspaces"], "id", "name", "environment", "status", "locked", "lock_reason", "drift_detected", "tf_version", "resource_count", "last_run"))
}

func toolTFRuns(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	if id := argInt(args, "run_id"); id > 0 {
		m, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/terraform/runs/%d", id), nil)
		if err != nil {
			return "", err
		}
		run, _ := m["run"].(map[string]interface{})
		out := pick(run, "id", "workspace_id", "type", "status", "message", "has_changes", "plan", "policy", "cost", "targets", "replace", "decision", "decision_comment", "exit_code", "created_at", "finished_at")
		if l, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/terraform/runs/%d/log?offset=0", id), nil); err == nil {
			data, _ := l["log"].(string)
			out["log_tail"] = tailOf(data, logTail)
		}
		return jsonText(out)
	}
	ws := argInt(args, "workspace_id")
	if ws < 1 {
		return "", fmt.Errorf("workspace_id or run_id is required")
	}
	m, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/terraform/workspaces/%d/runs?limit=10", ws), nil)
	if err != nil {
		return "", err
	}
	return jsonText(pickAll(m["runs"], "id", "type", "status", "message", "has_changes", "plan", "policy_status", "created_at", "finished_at"))
}

func (s *Server) tfWorkspaceName(ctx context.Context, id int64) (string, bool, error) {
	m, err := s.loopJSON(ctx, http.MethodGet, fmt.Sprintf("/api/terraform/workspaces/%d", id), nil)
	if err != nil {
		return "", false, err
	}
	ws, _ := m["workspace"].(map[string]interface{})
	name, _ := ws["name"].(string)
	locked, _ := ws["locked"].(bool)
	return name, locked, nil
}

func toolTFPlan(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id := argInt(args, "workspace_id")
	if id < 1 {
		return "", fmt.Errorf("workspace_id is required")
	}
	typ := argStr(args, "type")
	if typ == "" {
		typ = "plan"
	}
	if typ != "plan" && typ != "destroy" {
		return "", fmt.Errorf("type must be plan or destroy")
	}
	name, locked, err := s.tfWorkspaceName(r.Context(), id)
	if err != nil {
		return "", err
	}
	if locked {
		return "", fmt.Errorf("workspace %q is locked (another run is active or awaiting apply); check tf_runs", name)
	}
	if err := s.agentGateAs(r, 0, "Terraform", "tf_plan", fmt.Sprintf("terraform %s on workspace %q (plan only, nothing is changed)", typ, name), gateRoutine); err != nil {
		return "", err
	}
	msg := "AI agent"
	if m := argStr(args, "message"); m != "" {
		msg += ": " + clipTo(m, 200)
	}
	m, err := s.loopJSON(r.Context(), http.MethodPost, fmt.Sprintf("/api/terraform/workspaces/%d/runs", id), map[string]interface{}{"type": typ, "message": msg})
	if err != nil {
		return "", err
	}
	run, _ := m["run"].(map[string]interface{})
	return jsonText(pick(run, "id", "type", "status"))
}

func toolTFApply(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id := argInt(args, "run_id")
	if id < 1 {
		return "", fmt.Errorf("run_id is required")
	}
	comment := argStr(args, "comment")
	if len(comment) < 3 {
		return "", fmt.Errorf("comment is required (at least 3 characters)")
	}
	m, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/terraform/runs/%d", id), nil)
	if err != nil {
		return "", err
	}
	run, _ := m["run"].(map[string]interface{})
	if st, _ := run["status"].(string); st != "awaiting_approval" {
		return "", fmt.Errorf("run %d is %q; only a finished plan (awaiting_approval) can be applied", id, st)
	}
	if pol, ok := run["policy"].(map[string]interface{}); ok {
		if ps, _ := pol["status"].(string); ps == "failed" || ps == "warning" {
			return "", fmt.Errorf("the policy check is %q; the operator must review and apply this run in Pantau", ps)
		}
	}
	wsID, _ := run["workspace_id"].(float64)
	name, _, _ := s.tfWorkspaceName(r.Context(), int64(wsID))
	plan, _ := json.Marshal(run["plan"])
	summary := fmt.Sprintf("terraform APPLY run #%d on workspace %q (%s)\nplan: %s\nreason: %s", id, name, run["type"], clipTo(string(plan), 400), clipTo(comment, 200))
	if err := s.agentGateAs(r, 0, "Terraform", "tf_apply", summary, gateInfra); err != nil {
		return "", err
	}
	res, err := s.loopJSON(r.Context(), http.MethodPost, fmt.Sprintf("/api/terraform/runs/%d/apply", id), map[string]interface{}{"comment": "AI agent, approved by operator: " + comment, "override_policy": false})
	if err != nil {
		return "", err
	}
	out, _ := res["run"].(map[string]interface{})
	text, _ := jsonText(pick(out, "id", "status"))
	return text + "\napply started; follow it with tf_runs run_id=" + strconv.FormatInt(id, 10), nil
}
