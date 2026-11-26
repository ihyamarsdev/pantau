package web

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"pantau/internal/vpnnet"
)

// Agent tools for the features that arrived after ADR 0074 (the tool coverage ADR): VPN Networks per
// Host (ADR 0077), the Package Update Check (ADR 0078) and the Gateway CLI Update Check (ADR 0080/0081).
// They follow the same rules as the rest: they call the UI's handlers in-process (loopback), so the
// feature gate, cooldown and in-progress lock are the UI's, and they never take or return a secret.

func init() { agentTools = append(agentTools, recentTools...) }

var recentTools = []agentTool{
	{Name: "vpn_networks", Description: "VPN Networks and the hosts that use them (ADR 0077): each network's driver, state, interface and the number of hosts behind it. what = networks (the default) also lists which hosts point at each network; telemetry adds the live virtual IP, interface, driver state, host count, probe latency, sanitized last error and circuit-breaker state per network; status adds the installed driver binaries; interfaces lists the local network interfaces a driver could bind to. Read-only, and it works whether or not the VPN gate is on.",
		InputSchema: schema(nil, map[string]interface{}{"what": map[string]interface{}{"type": "string", "enum": []string{"networks", "telemetry", "status", "interfaces"}, "description": "default networks"}}), run: toolVPNNetworks},
	{Name: "vpn_network_action", Description: "Act on one VPN Network (ADR 0077) by network_id from vpn_networks. action = probe is a read-only active link health probe (interface, address, latency): it works with the VPN gate off and never changes anything. action = restart brings the tunnel back (down then up if it is up, otherwise up): a controlled change that needs gate 1 approval and the VPN gate enabled. Never treat a host on a down network as a dead host.", gate: gateRoutine,
		InputSchema: schema([]string{"network_id", "action"}, map[string]interface{}{"network_id": map[string]interface{}{"type": "integer", "description": "network id from vpn_networks"}, "action": map[string]interface{}{"type": "string", "enum": []string{"probe", "restart"}, "description": "probe is read-only; restart is a controlled change"}}), run: toolVPNNetworkAction},
	{Name: "vpn_host_status", Description: "Per-host VPN network status and diagnostics (ADR 0077): the VPN Network a host is assigned to (network_id 0 means direct), that network's driver, state, interface, virtual IP, probe latency and circuit-breaker state, and a read-only active link probe. Use it when a host is down to tell a dead host from a down tunnel. what = status (the default) reads the stored state; what = probe runs an active link health probe of the host's network (read-only, works with the VPN gate off). No secret is returned.",
		InputSchema: schema(hostReq, map[string]interface{}{"host_id": pHost, "what": map[string]interface{}{"type": "string", "enum": []string{"status", "probe"}, "description": "default status"}}), run: toolVPNHostStatus},
	{Name: "pkg_updates", Description: "The stored Package Update Check of a host (ADR 0078, no SSH): which installed packages have a newer version, how many updates are security ones, whether a reboot is required, how old the package metadata is, and how long ago the check ran. checked:false means it never ran: run it with check=true (one per minute per host, max 60 s). Nothing is installed; applying updates is pkg_action.",
		InputSchema: schema(hostReq, map[string]interface{}{"host_id": pHost, "check": map[string]interface{}{"type": "boolean", "description": "run the read-only check now instead of reading the stored result"}}), run: toolPkgUpdates},
	{Name: "gateway_cli", Description: "Gateway CLI Update (ADR 0080/0081) for the CLIs behind the Ansible Gate and the Terraform Gate on the Pantau server, and for kubectl: the installed version of each, the newest upstream release, and whether an update is available. target all (the default) checks ansible, every Terraform binary found and kubectl; ansible, terraform, tofu or kubectl checks one. kubectl is the one External Cluster operations run with (not the kubectl of a k3s or RKE2 host). Read-only and no Gate needs to be on: nothing is installed and install is not offered here.",
		InputSchema: schema(nil, map[string]interface{}{"target": map[string]interface{}{"type": "string", "enum": []string{"all", "ansible", "terraform", "tofu", "kubectl"}, "description": "default all"}}), run: toolGatewayCLI},
	{Name: "gateway_cli_install", Description: "Install a newer Gateway CLI (ADR 0081) on the Pantau server: target ansible, terraform, tofu or kubectl, optionally a specific version (default: the newest upstream release). For kubectl the version is also the one External Cluster operations switch to, so it can be pinned to match a cluster's version. The download is verified against the upstream checksum and installed in Pantau's managed directory, never /usr/bin. This is a destructive infrastructure change: it always waits for explicit operator approval, is never auto-approved and is never covered by a session grant. Read gateway_cli first to see what is available.", gate: gateInfra,
		InputSchema: schema([]string{"target"}, map[string]interface{}{"target": map[string]interface{}{"type": "string", "enum": []string{"ansible", "terraform", "tofu", "kubectl"}}, "version": map[string]interface{}{"type": "string", "description": "version to install, default the latest upstream release"}}), run: toolGatewayCLIInstall},
}

func toolVPNNetworks(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	what := argStr(args, "what")
	if what == "" {
		what = "networks"
	}
	switch what {
	case "status":
		m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/vpn/status", nil)
		if err != nil {
			return "", err
		}
		return jsonText(m)
	case "telemetry":
		// The enriched list rows already carry virtual_ip, probe_ms, degraded and breaker; keep the
		// runtime fields and sanitize the stored error so no secret leaks into the conversation.
		list, err := s.loopList(r.Context(), http.MethodGet, "/api/vpn/networks", nil)
		if err != nil {
			return "", err
		}
		rows := make([]interface{}, 0, len(list))
		for _, it := range list {
			row, ok := it.(map[string]interface{})
			if !ok {
				continue
			}
			t := pick(row, "id", "name", "driver", "iface", "last_state", "hosts_count", "virtual_ip", "probe_ms", "degraded", "breaker")
			if e, ok := row["last_error"].(string); ok {
				t["last_error"] = redactSecrets(e)
			}
			rows = append(rows, t)
		}
		return jsonText(map[string]interface{}{"telemetry": rows})
	case "networks", "interfaces":
		// Both endpoints answer a bare JSON array, so loopJSON (objects only) does not fit.
		list, err := s.loopList(r.Context(), http.MethodGet, "/api/vpn/"+what, nil)
		if err != nil {
			return "", err
		}
		return jsonText(map[string]interface{}{what: list})
	default:
		return "", fmt.Errorf("what must be networks, telemetry, status or interfaces")
	}
}

func toolVPNNetworkAction(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id := argInt(args, "network_id")
	if id < 1 {
		return "", fmt.Errorf("network_id (integer) is required")
	}
	action := argStr(args, "action")
	switch action {
	case "probe":
		// Read-only and gate-independent: call the manager directly so no approval gate is involved.
		if s.vpnMgr == nil {
			return "", fmt.Errorf("vpn manager unavailable")
		}
		st, err := s.vpnMgr.Probe(r.Context(), id)
		if err != nil && st.Err == "" {
			st.Err = err.Error()
		}
		return jsonText(st)
	case "restart":
		// Same feature gate the UI's up/down use, plus the agent's own approval gate.
		if !s.vpnGateOn() {
			return "", fmt.Errorf("VPN gate is disabled; enable it first")
		}
		if s.vpnMgr == nil {
			return "", fmt.Errorf("vpn manager unavailable")
		}
		n, err := s.db.GetVPNNetwork(id)
		if err != nil || n == nil {
			return "", fmt.Errorf("vpn network %d not found", id)
		}
		if err := s.agentGateAs(r, 0, n.Name, "vpn_network_action", "restart VPN network "+n.Name, gateRoutine); err != nil {
			return "", err
		}
		ctx := r.Context()
		if s.vpnMgr.Status(id).State == vpnnet.StateUp {
			if err := s.vpnMgr.Down(ctx, id); err != nil {
				return "", err
			}
		}
		st, err := s.vpnMgr.Up(ctx, id)
		if err != nil {
			return "", err
		}
		s.invalidateGlobe()
		return jsonText(st)
	default:
		return "", fmt.Errorf("action must be probe or restart")
	}
}

func toolPkgUpdates(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	path := fmt.Sprintf("/api/hosts/%d/packages/updates", id)
	method := http.MethodGet
	if b, _ := args["check"].(bool); b {
		// The check runs a package manager query over SSH and stores its result, so it goes through the gate.
		if err := s.agentGate(r, id, "pkg_updates", "run the read-only package update check", gateRoutine); err != nil {
			return "", err
		}
		path, method = path+"/check", http.MethodPost
	}
	m, err := s.loopJSON(r.Context(), method, path, nil)
	if err != nil {
		return "", err
	}
	// One host can have hundreds of pending packages; the counts stay, the list is cut.
	if c, ok := m["check"].(map[string]interface{}); ok {
		capList(c, "packages", secCap)
	}
	return jsonText(m)
}

func toolGatewayCLI(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	target := argStr(args, "target")
	if target == "" {
		target = "all"
	}
	// Both endpoints are read-only and work while the Gate is off, so no approval is asked.
	switch target {
	case "ansible":
		m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/ansible/update-check", nil)
		if err != nil {
			return "", err
		}
		return jsonText(map[string]interface{}{"results": []interface{}{m}})
	case "kubectl":
		m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/kube/cli/update-check", nil)
		if err != nil {
			return "", err
		}
		return jsonText(map[string]interface{}{"results": []interface{}{m}})
	case "terraform", "tofu", "all":
		m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/terraform/update-check", nil)
		if err != nil {
			return "", err
		}
		results, _ := m["results"].([]interface{})
		if target == "all" {
			ans, err := s.loopJSON(r.Context(), http.MethodGet, "/api/ansible/update-check", nil)
			if err != nil {
				return "", err
			}
			kube, err := s.loopJSON(r.Context(), http.MethodGet, "/api/kube/cli/update-check", nil)
			if err != nil {
				return "", err
			}
			results = append([]interface{}{ans}, results...)
			results = append(results, kube)
		} else {
			kept := make([]interface{}, 0, 1)
			for _, it := range results {
				if row, ok := it.(map[string]interface{}); ok && row["target"] == target {
					kept = append(kept, row)
				}
			}
			results = kept
		}
		return jsonText(map[string]interface{}{"results": results})
	default:
		return "", fmt.Errorf("target must be all, ansible, terraform or tofu")
	}
}

// toolVPNHostStatus reports the VPN path of one host (ADR 0077). It reads the host's network_id from the
// store, then the network's stored state; a probe asks the manager for an active link check. No secret
// (credentials, private keys) is ever returned.
func toolVPNHostStatus(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	h, err := s.db.GetHost(id)
	if err != nil || h == nil {
		return "", fmt.Errorf("host not found")
	}
	out := map[string]interface{}{"host_id": h.ID, "host_name": h.Name, "network_id": h.NetworkID}
	if h.NetworkID == 0 {
		out["network"] = "direct"
		out["diagnosis"] = "this host uses a direct connection, not a VPN Network"
		return jsonText(out)
	}
	n, err := s.db.GetVPNNetwork(h.NetworkID)
	if err != nil || n == nil {
		return "", fmt.Errorf("VPN network %d assigned to this host no longer exists", h.NetworkID)
	}
	// The network row carries driver/state/iface; keep only diagnostic fields, never credentials.
	out["network"] = map[string]interface{}{
		"id": n.ID, "name": n.Name, "driver": n.Driver, "iface": n.Iface,
	}
	what := argStr(args, "what")
	if what == "" {
		what = "status"
	}
	switch what {
	case "status":
		if s.vpnMgr != nil {
			st := s.vpnMgr.Status(n.ID)
			out["state"] = st.State
			if st.VirtualIP != "" {
				out["virtual_ip"] = st.VirtualIP
			}
			if st.ProbeMs >= 0 {
				out["probe_ms"] = st.ProbeMs
			}
			if st.Err != "" {
				out["last_error"] = redactSecrets(st.Err)
			}
		}
		return jsonText(out)
	case "probe":
		if s.vpnMgr == nil {
			return "", fmt.Errorf("vpn manager unavailable")
		}
		st, err := s.vpnMgr.Probe(r.Context(), n.ID)
		if err != nil && st.Err == "" {
			st.Err = err.Error()
		}
		out["probe"] = st
		return jsonText(out)
	default:
		return "", fmt.Errorf("what must be status or probe")
	}
}

// toolGatewayCLIInstall installs a newer Gateway CLI (ADR 0081). It calls the UI's install handler
// in-process, so checksum verification and managed-directory rules stay in one place. The install
// handler answers an NDJSON stream; the final {"done":true[,"error":...]} frame is the outcome.
func toolGatewayCLIInstall(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	target := argStr(args, "target")
	if target != "ansible" && target != "terraform" && target != "tofu" && target != "kubectl" {
		return "", fmt.Errorf("target must be ansible, terraform, tofu or kubectl")
	}
	body := map[string]string{"target": target}
	if v := argStr(args, "version"); v != "" {
		body["version"] = v
	}
	// gate 3 (infrastructure): always asks, never auto-approved, never covered by a session grant.
	if err := s.agentGateAs(r, 0, "Pantau server", "gateway_cli_install",
		fmt.Sprintf("install Gateway CLI %s (%s)", target, body["version"]), gateInfra); err != nil {
		return "", err
	}
	path := "/api/ansible/install"
	switch target {
	case "terraform", "tofu":
		path = "/api/terraform/install"
	case "kubectl":
		// The kubectl endpoint takes the version alone; the target names the CLI in the path.
		path = "/api/kube/cli/install"
		delete(body, "target")
	}
	code, b := s.loopback(r.Context(), http.MethodPost, path, body)
	if code/100 != 2 {
		return "", fmt.Errorf("install failed (HTTP %d): %s", code, clipTo(strings.TrimSpace(string(b)), 300))
	}
	// Keep the last log lines and the outcome; a failed install reports its error.
	var logs []string
	var installErr string
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		var frame map[string]interface{}
		if json.Unmarshal(sc.Bytes(), &frame) != nil {
			continue
		}
		if line, ok := frame["log"].(string); ok {
			logs = append(logs, line)
		}
		if e, ok := frame["error"].(string); ok {
			installErr = e
		}
	}
	out := strings.Join(logs, "\n")
	if len(out) > 4000 {
		out = out[len(out)-4000:]
	}
	if installErr != "" {
		return out, fmt.Errorf("install failed: %s", installErr)
	}
	return strings.TrimSpace(out + "\nGateway CLI " + target + " installed"), nil
}
