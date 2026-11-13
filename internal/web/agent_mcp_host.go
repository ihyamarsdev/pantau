package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"pantau/internal/inspector"
	"pantau/internal/netdiag"
)

// Agent tools for the host features beyond systemd and Docker (ADR-0066): stored telemetry, pending upgrades,
// firewall, domains, databases, directory listing, disk usage, network probes, alert acknowledgement and inventory
// rescans. They use the inspector or, through loopback, the handlers of the matching UI tab.

func init() { agentTools = append(agentTools, hostTools...) }

var pAbsPath = map[string]interface{}{"type": "string", "description": "absolute path"}

var hostTools = []agentTool{
	{Name: "host_info", Description: "More about one host. what = resources (CPU, RAM, swap, disk and network rates, top processes, listening ports with their owners, top connections; from the last inspection, no SSH), updates (pending package upgrades, live), firewall (ufw/firewalld/iptables state and rules, live), domains (reverse-proxy domains with TLS and upstreams, from the last domain scan) or databases (MySQL/PostgreSQL/MongoDB servers on the host or in containers, live).",
		InputSchema: schema([]string{"host_id", "what"}, map[string]interface{}{"host_id": pHost, "what": map[string]interface{}{"type": "string", "enum": []string{"resources", "updates", "firewall", "domains", "databases"}}}), run: toolHostInfo},
	{Name: "list_dir", Description: "List a directory on a host: mode, size, modification time and name per entry (directories end with /).",
		InputSchema: schema([]string{"host_id", "path"}, map[string]interface{}{"host_id": pHost, "path": pAbsPath}), run: toolListDir},
	{Name: "disk_usage", Description: "Sizes of the direct children of a directory, largest first (du, one level, up to 55 s). Use it to find what fills a disk; drill down by calling it again on a child.",
		InputSchema: schema([]string{"host_id", "path"}, map[string]interface{}{"host_id": pHost, "path": pAbsPath}), run: toolDiskUsage},
	{Name: "net_diag", Description: "Network probe run FROM a host: ping, port_probe (TCP connect, needs port), dns_resolve, or dig (record_type A, AAAA, CNAME, MX, NS, TXT, SOA or PTR; optional dns_server). The target is target (hostname or IP) or target_host_id (another managed host).",
		InputSchema: schema([]string{"host_id", "type"}, map[string]interface{}{"host_id": pHost, "type": map[string]interface{}{"type": "string", "enum": []string{"ping", "port_probe", "dns_resolve", "dig"}}, "target": str, "target_host_id": map[string]interface{}{"type": "integer", "description": "host id from list_hosts"}, "port": map[string]interface{}{"type": "integer"}, "record_type": str, "dns_server": str}), run: toolNetDiag},
	{Name: "alert_ack", Description: "Acknowledge one active alert (id from list_alerts) once its cause is fixed and verified. May wait for operator approval.", gate: gateRoutine,
		InputSchema: schema([]string{"alert_id"}, map[string]interface{}{"alert_id": map[string]interface{}{"type": "integer"}}), run: toolAlertAck},
	{Name: "inventory_scan", Description: "Rescan a host's package inventory (what = packages) or reverse-proxy domains (what = domains) so fleet_search and host_info domains are fresh. One scan per minute per host. May wait for operator approval.", gate: gateRoutine,
		InputSchema: schema([]string{"host_id", "what"}, map[string]interface{}{"host_id": pHost, "what": map[string]interface{}{"type": "string", "enum": []string{"packages", "domains"}}}), run: toolInventoryScan},
	{Name: "firewall_action", Description: "Change a host firewall: setup, enable, disable, add, edit, delete or persist on backend ufw, firewalld or iptables. rule is the new rule (add, edit), old the rule as listed by host_info firewall (edit, delete); both use that listing's field names. A rule that touches the SSH port Pantau uses is refused. Always waits for operator approval.", gate: gateHigh,
		InputSchema: schema([]string{"host_id", "backend", "action"}, map[string]interface{}{"host_id": pHost, "backend": map[string]interface{}{"type": "string", "enum": []string{"ufw", "firewalld", "iptables"}}, "action": map[string]interface{}{"type": "string", "enum": []string{"setup", "enable", "disable", "add", "edit", "delete", "persist"}}, "rule": map[string]interface{}{"type": "object"}, "old": map[string]interface{}{"type": "object"}}), run: toolFirewallAction},
}

func toolHostInfo(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	var v interface{}
	switch argStr(args, "what") {
	case "resources":
		h, err := s.db.GetHost(id)
		if err != nil || h == nil {
			return "", fmt.Errorf("host not found")
		}
		list := func(stored string) interface{} { // the inspector stores these lists as JSON text
			if json.Valid([]byte(stored)) {
				return json.RawMessage(stored)
			}
			return nil
		}
		v = map[string]interface{}{
			"last_inspected": h.LastInspected, "cpu_cores": h.CPUCores, "cpu_load": h.CPULoad,
			"ram_used_bytes": h.RAMUsedBytes, "ram_total_bytes": h.RAMTotalBytes, "swap_used_bytes": h.SwapUsedBytes, "swap_total_bytes": h.SwapTotalBytes,
			"disk_used_bytes": h.DiskUsedBytes, "disk_total_bytes": h.DiskTotalBytes, "disk_read_bps": h.DiskReadSpeedBps, "disk_write_bps": h.DiskWriteSpeedBps,
			"net_rx_bps": h.NetRxSpeedBps, "net_tx_bps": h.NetTxSpeedBps, "internet_online": h.InternetOnline, "internet_latency_ms": h.InternetLatencyMs,
			"public_ip": h.PublicIP, "active_connections": h.ActiveConnCount, "top_connections": list(h.TopConnections), "listening_ports": list(h.ListeningPorts),
			"top_cpu_processes": list(h.TopCPUProcesses), "top_ram_processes": list(h.TopRAMProcesses), "top_swap_processes": list(h.TopSwapProcesses),
		}
	case "updates":
		v, err = s.inspector.GetUpgradePreview(r.Context(), id)
	case "firewall":
		v, err = s.inspector.FirewallState(id)
	case "domains":
		v, err = s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/domains?host_id=%d", id), nil)
	case "databases":
		v, err = s.inspector.ScanDatabases(id)
	default:
		return "", fmt.Errorf("what must be resources, updates, firewall, domains or databases")
	}
	if err != nil {
		return "", err
	}
	return jsonText(v)
}

func toolListDir(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	p := argStr(args, "path")
	if !strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("path must be absolute")
	}
	m, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/hosts/%d/files/list?%s", id, url.Values{"path": {p}}.Encode()), nil)
	if err != nil {
		return "", err
	}
	files, _ := m["files"].([]interface{})
	if len(files) == 0 {
		return "empty directory", nil
	}
	var b strings.Builder // one line per entry: a third of the JSON size
	for _, it := range files {
		f, _ := it.(map[string]interface{})
		name, _ := f["name"].(string)
		if f["is_dir"] == true {
			name += "/"
		}
		size, _ := f["size"].(float64)
		fmt.Fprintf(&b, "%v %12.0f %v %s\n", f["mode"], size, f["mod_time"], name)
	}
	return b.String(), nil
}

func toolDiskUsage(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	q := url.Values{"host_id": {fmt.Sprint(id)}, "path": {argStr(args, "path")}}
	m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/du?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	return jsonText(map[string]interface{}{"path": m["path"], "total_bytes": m["total_bytes"], "entries": pickAll(m["entries"], "name", "bytes"),
		"permission_denied": m["denied"], "timed_out": m["timed_out"], "truncated": m["truncated"]})
}

func toolNetDiag(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	req := netdiag.ProbeRequest{SourceHostID: id, Type: netdiag.ProbeType(argStr(args, "type")), TargetType: "custom", TargetAddress: argStr(args, "target"),
		TargetHostID: argInt(args, "target_host_id"), Port: int(argInt(args, "port")), RecordType: argStr(args, "record_type"), DNSServer: argStr(args, "dns_server")}
	switch req.Type {
	case netdiag.ProbeTypePing, netdiag.ProbeTypePort, netdiag.ProbeTypeDNS, netdiag.ProbeTypeDig:
	default: // traceroute is left out: Pantau installs the tool when it is missing, which is a change
		return "", fmt.Errorf("type must be ping, port_probe, dns_resolve or dig")
	}
	if req.TargetHostID > 0 {
		req.TargetType = "host"
	}
	var results []netdiag.ProbeEvent
	err = s.netdiagService.ExecuteProbe(r.Context(), req, func(e netdiag.ProbeEvent) {
		if e.Type != "raw" && e.Type != "progress" && e.Type != "done" {
			results = append(results, e)
		}
	})
	if err != nil {
		return "", err
	}
	return jsonText(results)
}

func toolAlertAck(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id := argInt(args, "alert_id")
	alerts, err := s.db.ListActiveAlerts()
	if err != nil {
		return "", err
	}
	for _, a := range alerts {
		if a.ID != id {
			continue
		}
		if err := s.agentGate(r, a.HostID, "alert_ack", fmt.Sprintf("acknowledge alert #%d (%s): %s", a.ID, a.Level, clipTo(a.Message, 300)), gateRoutine); err != nil {
			return "", err
		}
		if err := s.db.AcknowledgeAlert(id); err != nil {
			return "", err
		}
		return fmt.Sprintf("alert #%d acknowledged", id), nil
	}
	return "", fmt.Errorf("no active alert #%d; see list_alerts", id)
}

func toolInventoryScan(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	what := argStr(args, "what")
	if what != "packages" && what != "domains" {
		return "", fmt.Errorf("what must be packages or domains")
	}
	if err := s.agentGate(r, id, "inventory_scan", "rescan "+what, gateRoutine); err != nil {
		return "", err
	}
	if what == "domains" {
		m, err := s.loopJSON(r.Context(), http.MethodPost, "/api/domains/scan", map[string]int64{"host_id": id})
		if err != nil {
			return "", err
		}
		return jsonText(m)
	}
	// The package scan answers with a bare JSON array, so loopJSON (objects only) does not fit.
	code, b := s.loopback(r.Context(), http.MethodPost, fmt.Sprintf("/api/hosts/%d/packages/scan", id), nil)
	if code != http.StatusOK {
		return "", fmt.Errorf("%s (HTTP %d)", clipTo(strings.TrimSpace(string(b)), 300), code)
	}
	var pkgs []json.RawMessage
	_ = json.Unmarshal(b, &pkgs)
	return fmt.Sprintf("package inventory rescanned: %d packages; query it with fleet_search", len(pkgs)), nil
}

func toolFirewallAction(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	h, err := s.db.GetHost(id)
	if err != nil || h == nil {
		return "", fmt.Errorf("host not found")
	}
	var req inspector.FirewallRequest
	raw, _ := json.Marshal(args)
	if err := json.Unmarshal(raw, &req); err != nil {
		return "", fmt.Errorf("rule and old must use the field names shown by host_info firewall: %v", err)
	}
	// Checked before asking, so the operator only sees a request that can run.
	if err := inspector.ValidateFirewallRequest(req, h.Port); err != nil {
		return "", err
	}
	card, _ := json.MarshalIndent(req, "", " ")
	if err := s.agentGateAs(r, id, h.Name, "firewall_action", "firewall change:\n"+string(card), gateHigh); err != nil {
		return "", err
	}
	res, err := s.inspector.FirewallApply(id, req)
	if err != nil {
		return "", err
	}
	out, _ := jsonText(res)
	if !res.Succeeded() {
		return out, fmt.Errorf("firewall %s failed", req.Action)
	}
	return out, nil
}
