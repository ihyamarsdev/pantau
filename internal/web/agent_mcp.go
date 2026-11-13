package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"pantau/internal/inspector"
	"pantau/internal/logger"
	"pantau/internal/store"
)

// Pantau as an MCP server (Model Context Protocol, streamable HTTP, JSON responses only) for Hermes Agent.
// Config on the Hermes side (~/.hermes/config.yaml):
//
//	mcp_servers:
//	  pantau: {url: "http://127.0.0.1:<port>/mcp", headers: {Authorization: "Bearer <token>"}, timeout: 300}
//
// Tool gates: 0 read-only, 1 mutating with a dedicated validated tool (auto-approvable), 2 arbitrary shell
// (always needs the operator).

const (
	mcpProtocolVersion = "2025-03-26"
	mcpMaxResultBytes  = 24 << 10
	mcpExecTimeout     = 120 * time.Second
)

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type agentTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
	gate        int
	run         func(s *Server, r *http.Request, args map[string]interface{}) (string, error)
}

func schema(required []string, props map[string]interface{}) map[string]interface{} {
	if required == nil {
		required = []string{} // JSON Schema: "required" is an array, never null (LLM APIs reject null)
	}
	return map[string]interface{}{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

var (
	pHost   = map[string]interface{}{"type": "integer", "description": "Host id from list_hosts"}
	pUnit   = map[string]interface{}{"type": "string", "description": "systemd unit name ending in .service"}
	pName   = map[string]interface{}{"type": "string", "description": "Docker container name or id"}
	hostReq = []string{"host_id"}
)

var agentTools = []agentTool{
	{Name: "list_hosts", Description: "List managed hosts with status, OS, load and resource use.", InputSchema: schema(nil, map[string]interface{}{}), run: toolListHosts},
	{Name: "get_host_state", Description: "Host overview: Desired State rules, current Drift items and recent incidents with root-cause excerpts.", InputSchema: schema(hostReq, map[string]interface{}{"host_id": pHost}), run: toolHostState},
	{Name: "list_alerts", Description: "Active (unacknowledged) alerts across all hosts, with root-cause excerpts.", InputSchema: schema(nil, map[string]interface{}{}), run: toolListAlerts},
	{Name: "systemd_list", Description: "List systemd service units on a host. Set only_problems to show just failed units.", InputSchema: schema(hostReq, map[string]interface{}{"host_id": pHost, "only_problems": map[string]interface{}{"type": "boolean"}}), run: toolSystemdList},
	{Name: "systemd_detail", Description: "systemctl status, properties, recent journal and Pantau's automatic diagnosis for one unit.", InputSchema: schema([]string{"host_id", "unit"}, map[string]interface{}{"host_id": pHost, "unit": pUnit, "errors_only": map[string]interface{}{"type": "boolean", "description": "journal lines of priority error and worse"}}), run: toolSystemdDetail},
	{Name: "docker_list", Description: "List all Docker containers on a host with state and status.", InputSchema: schema(hostReq, map[string]interface{}{"host_id": pHost}), run: toolDockerList},
	{Name: "docker_logs", Description: "Last lines of a container's logs.", InputSchema: schema([]string{"host_id", "container"}, map[string]interface{}{"host_id": pHost, "container": pName, "tail": map[string]interface{}{"type": "integer", "description": "lines, 1-1000, default 100"}}), run: toolDockerLogs},
	{Name: "security_status", Description: "Latest security scan: pending security updates by severity, reboot required, failing hardening checks.", InputSchema: schema(hostReq, map[string]interface{}{"host_id": pHost}), run: toolSecurityStatus},
	{Name: "read_file", Description: "Read a text file on a host (absolute path, first 64 KiB). Private keys, /etc/shadow and similar secret files are refused.", InputSchema: schema([]string{"host_id", "path"}, map[string]interface{}{"host_id": pHost, "path": map[string]interface{}{"type": "string"}}), run: toolReadFile},
	{Name: "fleet_search", Description: "Search packages, containers (name or image) and domains across ALL hosts at once, from Pantau's stored inventory (no SSH, so data may be stale: see data_age). Returns each match with the hosts that have it. Use it to find which hosts run a package/version, container or domain; hosts_without_* counts hosts never scanned.", InputSchema: schema([]string{"query"}, map[string]interface{}{"query": map[string]interface{}{"type": "string", "description": "2-100 characters, case-insensitive substring"}, "kind": map[string]interface{}{"type": "string", "enum": []string{"all", "package", "container", "domain"}, "description": "default all"}, "limit": map[string]interface{}{"type": "integer", "description": "max matched items, 1-200, default 50"}}), run: toolFleetSearch},

	{Name: "systemd_action", Description: "Run a systemctl action on one unit (start, stop, restart, reload, enable, disable, mask, unmask, reset-failed) or daemon-reload (no unit). May wait for operator approval. Stopping or disabling the SSH daemon is refused.", gate: 1,
		InputSchema: schema([]string{"host_id", "action"}, map[string]interface{}{"host_id": pHost, "unit": pUnit, "action": map[string]interface{}{"type": "string", "enum": []string{"start", "stop", "restart", "reload", "enable", "disable", "mask", "unmask", "reset-failed", "daemon-reload"}}}), run: toolSystemdAction},
	{Name: "docker_action", Description: "start, stop or restart a container. May wait for operator approval.", gate: 1,
		InputSchema: schema([]string{"host_id", "container", "action"}, map[string]interface{}{"host_id": pHost, "container": pName, "action": map[string]interface{}{"type": "string", "enum": []string{"start", "stop", "restart"}}}), run: toolDockerAction},
	{Name: "inspect_host", Description: "Re-run Pantau's inspection now so Desired State/Drift and host status are fresh. Use it to verify a fix.", gate: 1, InputSchema: schema(hostReq, map[string]interface{}{"host_id": pHost}), run: toolInspect},
	{Name: "security_scan", Description: "Run a security scan now (pending updates, hardening checks); read the result with security_status.", gate: 1, InputSchema: schema(hostReq, map[string]interface{}{"host_id": pHost}), run: toolSecurityScan},
	{Name: "exec_command", Description: "Run a shell command on a host as the SSH user (use `sudo -n` for root). Needed for editing config, package changes, etc. ALWAYS waits for explicit operator approval. Prefer the dedicated tools.", gate: 2,
		InputSchema: schema([]string{"host_id", "command"}, map[string]interface{}{"host_id": pHost, "command": map[string]interface{}{"type": "string"}}), run: toolExec},
}

func (s *Server) handleAgentMCP(w http.ResponseWriter, r *http.Request) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if tok == r.Header.Get("Authorization") || !s.agentTokenValid(tok) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.agentConfig().Enabled {
		http.Error(w, `{"error":"agent disabled"}`, http.StatusServiceUnavailable)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var req rpcReq
	if json.Unmarshal(raw, &req) != nil || req.JSONRPC != "2.0" {
		writeJSON(w, http.StatusBadRequest, rpcErr(nil, -32700, "parse error"))
		return
	}
	if len(req.ID) == 0 { // notification (notifications/initialized, ...)
		w.WriteHeader(http.StatusAccepted)
		return
	}
	switch req.Method {
	case "initialize":
		writeJSON(w, http.StatusOK, rpcOK(req.ID, map[string]interface{}{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo":      map[string]string{"name": "pantau", "version": "1"},
			"instructions":    "Pantau infrastructure console. Read first, change little, verify after. Mutating tools may wait for a human approval.",
		}))
	case "ping":
		writeJSON(w, http.StatusOK, rpcOK(req.ID, map[string]interface{}{}))
	case "tools/list":
		writeJSON(w, http.StatusOK, rpcOK(req.ID, map[string]interface{}{"tools": agentTools}))
	case "tools/call":
		var p struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		}
		if json.Unmarshal(req.Params, &p) != nil {
			writeJSON(w, http.StatusOK, rpcErr(req.ID, -32602, "invalid params"))
			return
		}
		var tool *agentTool
		for i := range agentTools {
			if agentTools[i].Name == p.Name {
				tool = &agentTools[i]
			}
		}
		if tool == nil {
			writeJSON(w, http.StatusOK, rpcErr(req.ID, -32602, "unknown tool: "+p.Name))
			return
		}
		out, err := tool.run(s, r, p.Arguments)
		res := map[string]interface{}{"content": []map[string]string{{"type": "text", "text": clip(out)}}}
		if err != nil {
			res["isError"] = true
			res["content"] = []map[string]string{{"type": "text", "text": clip(strings.TrimSpace(out + "\nERROR: " + err.Error()))}}
		}
		writeJSON(w, http.StatusOK, rpcOK(req.ID, res))
	default:
		writeJSON(w, http.StatusOK, rpcErr(req.ID, -32601, "method not found"))
	}
}

func rpcOK(id json.RawMessage, result interface{}) map[string]interface{} {
	return map[string]interface{}{"jsonrpc": "2.0", "id": id, "result": result}
}

func rpcErr(id json.RawMessage, code int, msg string) map[string]interface{} {
	return map[string]interface{}{"jsonrpc": "2.0", "id": id, "error": map[string]interface{}{"code": code, "message": msg}}
}

func clip(s string) string {
	if len(s) <= mcpMaxResultBytes {
		return s
	}
	return s[:mcpMaxResultBytes] + "\n…[truncated]"
}

// ---------------------------------------------------------------- argument helpers

func argHost(args map[string]interface{}) (int64, error) {
	f, ok := args["host_id"].(float64)
	if !ok || f < 1 || f != float64(int64(f)) {
		return 0, fmt.Errorf("host_id (integer) is required")
	}
	return int64(f), nil
}

func argStr(args map[string]interface{}, k string) string {
	v, _ := args[k].(string)
	return strings.TrimSpace(v)
}

func jsonText(v interface{}) (string, error) {
	b, err := json.MarshalIndent(v, "", " ")
	return string(b), err
}

// ---------------------------------------------------------------- read tools

func toolListHosts(s *Server, _ *http.Request, _ map[string]interface{}) (string, error) {
	hosts, err := s.db.ListHosts()
	if err != nil {
		return "", err
	}
	type row struct {
		ID       int64  `json:"id"`
		Name     string `json:"name"`
		Address  string `json:"address"`
		Status   string `json:"status"`
		OS       string `json:"os"`
		Uptime   string `json:"uptime"`
		CPULoad  string `json:"cpu_load"`
		RAMUsed  int64  `json:"ram_used_bytes"`
		RAMTotal int64  `json:"ram_total_bytes"`
		Disk     int64  `json:"disk_used_bytes"`
		DiskTot  int64  `json:"disk_total_bytes"`
	}
	out := make([]row, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, row{h.ID, h.Name, fmt.Sprintf("%s@%s:%d", h.User, h.Host, h.Port), h.Status, h.OSInfo, h.Uptime, h.CPULoad, h.RAMUsedBytes, h.RAMTotalBytes, h.DiskUsedBytes, h.DiskTotalBytes})
	}
	return jsonText(out)
}

func toolHostState(s *Server, _ *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	h, err := s.db.GetHost(id)
	if err != nil || h == nil {
		return "", fmt.Errorf("host not found")
	}
	rules, _ := s.db.ListDesiredRules(id)
	actual, _ := s.db.ListActualItems(id)
	incidents, _ := s.db.ListIncidents(id, 10)
	return jsonText(map[string]interface{}{
		"host":             map[string]interface{}{"id": h.ID, "name": h.Name, "status": h.Status, "os": h.OSInfo, "kernel": h.Kernel, "uptime": h.Uptime, "cpu_load": h.CPULoad, "failed_logins": h.FailedLoginsCount, "last_inspected": h.LastInspected},
		"rules":            rules,
		"actual":           actual,
		"recent_incidents": incidents,
	})
}

func toolListAlerts(s *Server, _ *http.Request, _ map[string]interface{}) (string, error) {
	a, err := s.db.ListActiveAlerts()
	if err != nil {
		return "", err
	}
	if len(a) > 50 {
		a = a[:50]
	}
	return jsonText(a)
}

// toolFleetSearch answers from stored inventory only (no SSH), the same data as GET /api/search.
func toolFleetSearch(s *Server, _ *http.Request, args map[string]interface{}) (string, error) {
	q := argStr(args, "query")
	if n := utf8.RuneCountInString(q); n < 2 || n > 100 {
		return "", fmt.Errorf("query must be 2 to 100 characters")
	}
	kind := argStr(args, "kind")
	if kind == "" {
		kind = store.FleetSearchAll
	}
	switch kind {
	case store.FleetSearchAll, store.FleetSearchPackage, store.FleetSearchContainer, store.FleetSearchDomain:
	default:
		return "", fmt.Errorf("kind must be all, package, container or domain")
	}
	limit := 50
	if f, ok := args["limit"].(float64); ok {
		if f < 1 {
			return "", fmt.Errorf("limit must be a positive number")
		}
		limit = int(min(f, 200))
	}
	res, err := s.db.SearchFleet(q, kind, limit)
	if err != nil {
		return "", err
	}
	return jsonText(res)
}

func toolSystemdList(s *Server, _ *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	svcs, supported, err := s.inspector.ListSystemdServices(id)
	if err != nil {
		return "", err
	}
	if !supported {
		return "systemd is not the init system on this host", nil
	}
	only, _ := args["only_problems"].(bool)
	out := svcs[:0:0]
	for _, v := range svcs {
		if !only || v.Active == "failed" || v.Load == "error" {
			out = append(out, v)
		}
	}
	return jsonText(out)
}

func toolSystemdDetail(s *Server, _ *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	eo, _ := args["errors_only"].(bool)
	d, err := s.inspector.SystemdDetail(id, argStr(args, "unit"), eo, false)
	if err != nil {
		return "", err
	}
	return jsonText(d)
}

func toolDockerList(s *Server, _ *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	c, err := s.inspector.ListDockerContainers(id)
	if err != nil {
		return "", err
	}
	return jsonText(c)
}

func toolDockerLogs(s *Server, _ *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	tail, _ := args["tail"].(float64)
	return s.inspector.DockerLogs(id, argStr(args, "container"), int(tail))
}

func toolSecurityStatus(s *Server, _ *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	scan, err := s.db.GetSecurityScan(id)
	if err != nil {
		return "", err
	}
	if scan == nil {
		return "no security scan yet; run security_scan", nil
	}
	findings := scan.Findings
	if len(findings) > 25 {
		findings = findings[:25]
	}
	var bad []interface{}
	for _, c := range scan.Hardening {
		if c.Status != "pass" {
			bad = append(bad, c)
		}
	}
	return jsonText(map[string]interface{}{
		"scanned_at": scan.ScannedAt, "status": scan.Status, "package_manager": scan.PackageManager,
		"pending_security": scan.PendingSecurity, "pending_total": scan.PendingTotal, "severity": scan.Severity,
		"reboot_required": scan.RebootRequired, "findings_first_25": findings, "hardening_not_passing": bad, "notes": scan.Notes, "error": scan.Error,
	})
}

// agentSecretPathRe matches files whose content is a credential. shortcut: name based, so a secret stored under
// an unremarkable name can still be read; exec_command is gated by the operator for anything beyond this.
var agentSecretPathRe = regexp.MustCompile(`(?i)(^/etc/(shadow|gshadow|sudoers)(\.|$|/)|/\.ssh/|/id_(rsa|dsa|ecdsa|ed25519)|\.(pem|key|p12|pfx|kdbx)$|/\.(env|netrc|pgpass|git-credentials)$|/\.aws/|/\.docker/config\.json$|/\.gnupg/)`)

func toolReadFile(s *Server, _ *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	p := argStr(args, "path")
	if !strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\x00\n") {
		return "", fmt.Errorf("path must be absolute")
	}
	if agentSecretPathRe.MatchString(path.Clean(p)) {
		return "", fmt.Errorf("refusing to read a secret file")
	}
	// Resolve symlinks first so a link cannot point past the check.
	stdout, stderr, code, err := s.inspector.ExecShell(id, "readlink -f -- "+shq(p), 15*time.Second)
	real := strings.TrimSpace(stdout)
	if err != nil || code != 0 || real == "" {
		return "", fmt.Errorf("cannot resolve %s: %s", p, strings.TrimSpace(stderr))
	}
	if agentSecretPathRe.MatchString(real) {
		return "", fmt.Errorf("refusing to read a secret file")
	}
	stdout, stderr, code, err = s.inspector.ExecShell(id, "head -c 65536 -- "+shq(real), 20*time.Second)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("%s", strings.TrimSpace(stderr))
	}
	return stdout, nil
}

func shq(s string) string { return shellQuote(s) }

// ---------------------------------------------------------------- mutating tools

func (s *Server) agentGate(r *http.Request, hostID int64, tool, summary string, gate int) error {
	h, err := s.db.GetHost(hostID)
	if err != nil || h == nil {
		return fmt.Errorf("host not found")
	}
	return s.agentGateAs(r, hostID, h.Name, tool, summary, gate)
}

// agentGateAs asks (or skips, per mode and grants) for one action. hostID 0 means a scope without a host
// (Terraform, Ansible); label is what the approval card shows.
func (s *Server) agentGateAs(r *http.Request, hostID int64, label, tool, summary string, gate int) error {
	ap := &agentApproval{HostID: hostID, HostName: label, Tool: tool, Summary: summary}
	ok, why := s.agentApprove(r, ap, gate)
	if !ok {
		logger.Warn("agent", label, fmt.Sprintf("%s not run (%s): %s", tool, why, summary))
		return fmt.Errorf("%s", why)
	}
	logger.Info("agent", label, fmt.Sprintf("running %s: %s", tool, summary))
	return nil
}

func toolSystemdAction(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	unit, action := argStr(args, "unit"), argStr(args, "action")
	summary := "systemctl " + action + " " + unit
	if action != "daemon-reload" && !inspector.ValidSystemdUnit(unit) {
		return "", inspector.ErrSystemdInvalidUnit
	}
	if err := s.agentGate(r, id, "systemd_action", strings.TrimSpace(summary), 1); err != nil {
		return "", err
	}
	res, err := s.inspector.SystemdAction(id, unit, action)
	if err != nil {
		return "", err
	}
	out, _ := jsonText(res)
	if !res.Succeeded() {
		return out, fmt.Errorf("%s %s failed", action, unit)
	}
	return out, nil
}

func toolDockerAction(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	name, action := argStr(args, "container"), argStr(args, "action")
	if action != "start" && action != "stop" && action != "restart" {
		return "", fmt.Errorf("action must be start, stop or restart")
	}
	if !inspector.ValidDockerName(name) {
		return "", fmt.Errorf("invalid container name")
	}
	if err := s.agentGate(r, id, "docker_action", "docker "+action+" "+name, 1); err != nil {
		return "", err
	}
	return s.inspector.DockerContainerOp(id, name, action, inspector.DockerContainerOpts{})
}

func toolInspect(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	if err := s.agentGate(r, id, "inspect_host", "re-run inspection", 1); err != nil {
		return "", err
	}
	if err := s.inspector.InspectHost(id); err != nil {
		return "", err
	}
	return toolHostState(s, r, args)
}

func toolSecurityScan(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	if err := s.agentGate(r, id, "security_scan", "run security scan", 1); err != nil {
		return "", err
	}
	if err := s.inspector.ScanSecurity(id); err != nil {
		return "", err
	}
	return toolSecurityStatus(s, r, args)
}

func toolExec(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	cmd := strings.TrimSpace(argStr(args, "command"))
	if cmd == "" || len(cmd) > 8192 {
		return "", fmt.Errorf("command is required (max 8192 bytes)")
	}
	if err := s.agentGate(r, id, "exec_command", cmd, 2); err != nil {
		return "", err
	}
	stdout, stderr, code, err := s.inspector.ExecShell(id, cmd, mcpExecTimeout)
	if err != nil {
		return "", err
	}
	out := fmt.Sprintf("exit code: %d\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	if code != 0 {
		return out, fmt.Errorf("command exited %d", code)
	}
	return out, nil
}

func contextTimeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}
