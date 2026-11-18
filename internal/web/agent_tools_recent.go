package web

import (
	"fmt"
	"net/http"
)

// Agent tools for the features that arrived after ADR 0074 (the tool coverage ADR): VPN Networks per
// Host (ADR 0077), the Package Update Check (ADR 0078) and the Gateway CLI Update Check (ADR 0080/0081).
// They follow the same rules as the rest: they call the UI's handlers in-process (loopback), so the
// feature gate, cooldown and in-progress lock are the UI's, and they never take or return a secret.

func init() { agentTools = append(agentTools, recentTools...) }

var recentTools = []agentTool{
	{Name: "vpn_networks", Description: "VPN Networks and the hosts that use them (ADR 0077): each network's driver, state, interface and the number of hosts behind it. what = networks (the default) also lists which hosts point at each network; status adds the installed driver binaries; interfaces lists the local network interfaces a driver could bind to. Read-only, and it works whether or not the VPN gate is on.",
		InputSchema: schema(nil, map[string]interface{}{"what": map[string]interface{}{"type": "string", "enum": []string{"networks", "status", "interfaces"}, "description": "default networks"}}), run: toolVPNNetworks},
	{Name: "pkg_updates", Description: "The stored Package Update Check of a host (ADR 0078, no SSH): which installed packages have a newer version, how many updates are security ones, whether a reboot is required, how old the package metadata is, and how long ago the check ran. checked:false means it never ran: run it with check=true (one per minute per host, max 60 s). Nothing is installed; applying updates is pkg_action.",
		InputSchema: schema(hostReq, map[string]interface{}{"host_id": pHost, "check": map[string]interface{}{"type": "boolean", "description": "run the read-only check now instead of reading the stored result"}}), run: toolPkgUpdates},
	{Name: "gateway_cli", Description: "Gateway CLI Update (ADR 0080/0081) for the CLIs behind the Ansible Gate and the Terraform Gate on the Pantau server: the installed version of each, the newest upstream release, and whether an update is available. target all (the default) checks ansible and every Terraform binary found; ansible, terraform or tofu checks one. Read-only and no Gate needs to be on: nothing is installed and install is not offered here.",
		InputSchema: schema(nil, map[string]interface{}{"target": map[string]interface{}{"type": "string", "enum": []string{"all", "ansible", "terraform", "tofu"}, "description": "default all"}}), run: toolGatewayCLI},
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
	case "networks", "interfaces":
		// Both endpoints answer a bare JSON array, so loopJSON (objects only) does not fit.
		list, err := s.loopList(r.Context(), http.MethodGet, "/api/vpn/"+what, nil)
		if err != nil {
			return "", err
		}
		return jsonText(map[string]interface{}{what: list})
	default:
		return "", fmt.Errorf("what must be networks, status or interfaces")
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
			results = append([]interface{}{ans}, results...)
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
