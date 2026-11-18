package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"pantau/internal/cloudflare"
)

// Agent tools for Cloudflare (ADR-0066). Like Kubernetes, Ansible and Terraform they call the UI's HTTP handlers
// in-process (loopback), so token handling and input validation stay in one place. The API token never leaves Pantau.
// Every change is gate 2: DNS, cache and zone settings affect live traffic, so it always waits for the operator and,
// being host-less, cannot be covered by a session grant.

func init() { agentTools = append(agentTools, cloudflareTools...) }

var (
	pCFAccount = map[string]interface{}{"type": "string", "description": "Cloudflare account name from cf_accounts; optional when only one account is registered"}
	pCFZone    = map[string]interface{}{"type": "string", "description": "Zone name (example.com) or zone id from cf_zones"}
	pCFList    = map[string]interface{}{"type": "array", "items": str}
)

var cloudflareTools = []agentTool{
	{Name: "cf_accounts", Description: "Cloudflare accounts registered in Pantau (name). API tokens are never shown.",
		InputSchema: schema(nil, map[string]interface{}{}), run: toolCFAccounts},
	{Name: "cf_zones", Description: "Zones (domains) of a Cloudflare account with status, plan and name servers.",
		InputSchema: schema(nil, map[string]interface{}{"account": pCFAccount}), run: toolCFZones},
	{Name: "cf_dns_list", Description: "DNS records of a zone. type and name (substring) are optional filters.",
		InputSchema: schema([]string{"zone"}, map[string]interface{}{"account": pCFAccount, "zone": pCFZone, "type": str, "name": str}), run: toolCFDNSList},
	{Name: "cf_zone_settings", Description: "The zone settings Pantau can change (ssl, always_use_https, security_level, development_mode, min_tls_version, tls_1_3, http3, ipv6, brotli, ...) with current values and allowed values.",
		InputSchema: schema([]string{"zone"}, map[string]interface{}{"account": pCFAccount, "zone": pCFZone}), run: toolCFZoneSettings},
	{Name: "cf_dns_change", Description: "create, update or delete one DNS record. create needs type, name and content (MX also priority); update needs record_id from cf_dns_list and only the fields to change (the rest is kept); delete needs record_id. ttl 1 = auto. Always waits for operator approval.", gate: gateHigh,
		InputSchema: schema([]string{"zone", "action"}, map[string]interface{}{"account": pCFAccount, "zone": pCFZone, "action": map[string]interface{}{"type": "string", "enum": []string{"create", "update", "delete"}}, "record_id": str, "type": map[string]interface{}{"type": "string", "enum": []string{"A", "AAAA", "CNAME", "TXT", "MX", "NS", "SRV", "CAA"}}, "name": str, "content": str, "ttl": map[string]interface{}{"type": "integer"}, "proxied": map[string]interface{}{"type": "boolean"}, "priority": map[string]interface{}{"type": "integer"}, "comment": str}), run: toolCFDNSChange},
	{Name: "cf_purge_cache", Description: "Purge a zone's cache: exactly one of everything, files (full URLs, max 30), hosts, tags or prefixes. Always waits for operator approval.", gate: gateHigh,
		InputSchema: schema([]string{"zone"}, map[string]interface{}{"account": pCFAccount, "zone": pCFZone, "everything": map[string]interface{}{"type": "boolean"}, "files": pCFList, "hosts": pCFList, "tags": pCFList, "prefixes": pCFList}), run: toolCFPurge},
	{Name: "cf_zone_setting_set", Description: "Change one zone setting listed by cf_zone_settings to one of its allowed values. Always waits for operator approval.", gate: gateHigh,
		InputSchema: schema([]string{"zone", "setting", "value"}, map[string]interface{}{"account": pCFAccount, "zone": pCFZone, "setting": str, "value": str}), run: toolCFZoneSettingSet},
	{Name: "cf_access_rules", Description: "IP Access Rules of a zone (mode, target ip/ip_range/asn/country, value, notes).",
		InputSchema: schema([]string{"zone"}, map[string]interface{}{"account": pCFAccount, "zone": pCFZone}), run: toolCFAccessRules},
	{Name: "cf_access_rule_change", Description: "create or delete one IP Access Rule. create needs mode (block, challenge, js_challenge, managed_challenge, whitelist), target (ip, ip_range, asn, country) and value (203.0.113.7, 203.0.113.0/24, AS13335, ID); notes optional. delete needs rule_id from cf_access_rules. Always waits for operator approval.", gate: gateHigh,
		InputSchema: schema([]string{"zone", "action"}, map[string]interface{}{"account": pCFAccount, "zone": pCFZone, "action": map[string]interface{}{"type": "string", "enum": []string{"create", "delete"}}, "rule_id": str, "mode": map[string]interface{}{"type": "string", "enum": cloudflare.AccessRuleModes}, "target": map[string]interface{}{"type": "string", "enum": cloudflare.AccessRuleTargets}, "value": str, "notes": str}), run: toolCFAccessRuleChange},
	{Name: "cf_tunnels", Description: "Cloudflare Tunnels (cloudflared) of an account with their health status (healthy, degraded, down, inactive). Read-only.",
		InputSchema: schema(nil, map[string]interface{}{"account": pCFAccount}), run: toolCFTunnels},
	{Name: "cf_dns_export", Description: "Export a zone's DNS records as a BIND zone file (the same text Export downloads). Read-only; pass the zone whose records the operator wants to keep or diff.",
		InputSchema: schema([]string{"zone"}, map[string]interface{}{"account": pCFAccount, "zone": pCFZone}), run: toolCFDNSExport},
	{Name: "cf_token_verify", Description: "Verify the API token of a Cloudflare account against Cloudflare: its status and expiry. A token that Cloudflare refuses says which permission is missing, so use it when another cf_* tool answers forbidden. The token itself is never shown.",
		InputSchema: schema(nil, map[string]interface{}{"account": pCFAccount}), run: toolCFTokenVerify},
}

type cfZone struct {
	account  int64
	zoneID   string
	zoneName string
}

func (z cfZone) path(sub string) string {
	return fmt.Sprintf("/api/cloudflare/accounts/%d/zones/%s%s", z.account, url.PathEscape(z.zoneID), sub)
}

func (s *Server) cfAccountOf(args map[string]interface{}) (int64, error) {
	list, err := s.db.ListCloudflareAccounts()
	if err != nil {
		return 0, err
	}
	name := argStr(args, "account")
	if name == "" {
		switch len(list) {
		case 0:
			return 0, fmt.Errorf("no Cloudflare account is registered in Pantau")
		case 1:
			return list[0].ID, nil
		}
		return 0, fmt.Errorf("account is required (several accounts exist; see cf_accounts)")
	}
	for _, a := range list {
		if strings.EqualFold(a.Name, name) {
			return a.ID, nil
		}
	}
	return 0, fmt.Errorf("no Cloudflare account named %q; see cf_accounts", name)
}

// cfZoneOf resolves zone by name or id inside the account, so the agent needs no id lookup first.
func (s *Server) cfZoneOf(r *http.Request, args map[string]interface{}) (*cfZone, error) {
	acc, err := s.cfAccountOf(args)
	if err != nil {
		return nil, err
	}
	want := argStr(args, "zone")
	if want == "" {
		return nil, fmt.Errorf("zone is required (name or id from cf_zones)")
	}
	m, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/cloudflare/accounts/%d/zones", acc), nil)
	if err != nil {
		return nil, err
	}
	zones, _ := m["zones"].([]interface{})
	for _, it := range zones {
		z, _ := it.(map[string]interface{})
		id, _ := z["id"].(string)
		name, _ := z["name"].(string)
		if id == want || strings.EqualFold(name, want) {
			return &cfZone{acc, id, name}, nil
		}
	}
	return nil, fmt.Errorf("zone %q not found in this account; see cf_zones", want)
}

func (s *Server) cfRecords(r *http.Request, z *cfZone) ([]interface{}, error) {
	m, err := s.loopJSON(r.Context(), http.MethodGet, z.path("/dns"), nil)
	if err != nil {
		return nil, err
	}
	recs, _ := m["records"].([]interface{})
	return recs, nil
}

func toolCFAccounts(s *Server, _ *http.Request, _ map[string]interface{}) (string, error) {
	list, err := s.db.ListCloudflareAccounts()
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "no Cloudflare account is registered; the operator adds one in the Pantau Cloudflare section", nil
	}
	type row struct {
		Name string `json:"name"`
	}
	out := make([]row, 0, len(list))
	for _, a := range list {
		out = append(out, row{a.Name})
	}
	return jsonText(out)
}

func toolCFZones(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	acc, err := s.cfAccountOf(args)
	if err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/cloudflare/accounts/%d/zones", acc), nil)
	if err != nil {
		return "", err
	}
	return jsonText(pickAll(m["zones"], "id", "name", "status", "paused", "plan", "name_servers"))
}

func toolCFDNSList(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	z, err := s.cfZoneOf(r, args)
	if err != nil {
		return "", err
	}
	recs, err := s.cfRecords(r, z)
	if err != nil {
		return "", err
	}
	typ, name := argStr(args, "type"), strings.ToLower(argStr(args, "name"))
	var kept []interface{}
	for _, it := range recs {
		rec, _ := it.(map[string]interface{})
		if t, _ := rec["type"].(string); typ != "" && !strings.EqualFold(t, typ) {
			continue
		}
		if n, _ := rec["name"].(string); name != "" && !strings.Contains(strings.ToLower(n), name) {
			continue
		}
		kept = append(kept, it)
	}
	return jsonText(pickAll(kept, "id", "type", "name", "content", "ttl", "proxied", "priority", "comment"))
}

func toolCFZoneSettings(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	z, err := s.cfZoneOf(r, args)
	if err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodGet, z.path("/settings"), nil)
	if err != nil {
		return "", err
	}
	return jsonText(map[string]interface{}{"zone": z.zoneName, "settings": pickAll(m["settings"], "id", "value"), "allowed_values": m["options"]})
}

func toolCFDNSChange(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	action := argStr(args, "action")
	if action != "create" && action != "update" && action != "delete" {
		return "", fmt.Errorf("action must be create, update or delete")
	}
	z, err := s.cfZoneOf(r, args)
	if err != nil {
		return "", err
	}

	var cur map[string]interface{}
	rid := argStr(args, "record_id")
	if action != "create" {
		if rid == "" {
			return "", fmt.Errorf("record_id is required for %s (see cf_dns_list)", action)
		}
		recs, err := s.cfRecords(r, z)
		if err != nil {
			return "", err
		}
		for _, it := range recs {
			if m, _ := it.(map[string]interface{}); m["id"] == rid {
				cur = m
			}
		}
		if cur == nil {
			return "", fmt.Errorf("record %q not found in zone %s", rid, z.zoneName)
		}
	}

	if action == "delete" {
		summary := fmt.Sprintf("Cloudflare %s: delete DNS record %v %v -> %v", z.zoneName, cur["type"], cur["name"], cur["content"])
		if err := s.agentGateAs(r, 0, "Cloudflare", "cf_dns_change", summary, gateHigh); err != nil {
			return "", err
		}
		if _, err := s.loopJSON(r.Context(), http.MethodDelete, z.path("/dns/"+url.PathEscape(rid)), nil); err != nil {
			return "", err
		}
		return "deleted", nil
	}

	// PUT replaces the whole record, so an update starts from the current one and overlays the given fields.
	in := map[string]interface{}{}
	for _, k := range []string{"type", "name", "content", "ttl", "proxied", "priority", "comment"} {
		if v, ok := cur[k]; ok {
			in[k] = v
		}
		if v, ok := args[k]; ok {
			in[k] = v
		}
	}
	summary := fmt.Sprintf("Cloudflare %s: %s DNS record %v %v -> %v (ttl %v, proxied %v)", z.zoneName, action, in["type"], in["name"], in["content"], in["ttl"], in["proxied"])
	if cur != nil {
		summary += fmt.Sprintf("\nwas: %v %v -> %v (ttl %v, proxied %v)", cur["type"], cur["name"], cur["content"], cur["ttl"], cur["proxied"])
	}
	if err := s.agentGateAs(r, 0, "Cloudflare", "cf_dns_change", summary, gateHigh); err != nil {
		return "", err
	}
	method, path := http.MethodPost, z.path("/dns")
	if action == "update" {
		method, path = http.MethodPut, z.path("/dns/"+url.PathEscape(rid))
	}
	m, err := s.loopJSON(r.Context(), method, path, in)
	if err != nil {
		return "", err
	}
	rec, _ := m["record"].(map[string]interface{})
	return jsonText(pick(rec, "id", "type", "name", "content", "ttl", "proxied", "priority"))
}

func cfStrings(args map[string]interface{}, k string) []string {
	var out []string
	arr, _ := args[k].([]interface{})
	for _, v := range arr {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

func toolCFPurge(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	z, err := s.cfZoneOf(r, args)
	if err != nil {
		return "", err
	}
	body := map[string]interface{}{}
	what := "everything"
	if all, _ := args["everything"].(bool); all {
		body["purge_everything"] = true
	}
	for _, k := range []string{"files", "hosts", "tags", "prefixes"} {
		if v := cfStrings(args, k); len(v) > 0 {
			body[k] = v
			what = k + ": " + strings.Join(v, ", ")
		}
	}
	// The handler enforces "exactly one mode"; checking here keeps the approval card for requests that can run.
	if len(body) != 1 {
		return "", fmt.Errorf("exactly one purge mode must be given: everything, files, hosts, tags or prefixes")
	}
	if err := s.agentGateAs(r, 0, "Cloudflare", "cf_purge_cache", fmt.Sprintf("Cloudflare %s: purge cache (%s)", z.zoneName, what), gateHigh); err != nil {
		return "", err
	}
	if _, err := s.loopJSON(r.Context(), http.MethodPost, z.path("/purge"), body); err != nil {
		return "", err
	}
	return "purge requested", nil
}

func toolCFZoneSettingSet(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	z, err := s.cfZoneOf(r, args)
	if err != nil {
		return "", err
	}
	id, val := argStr(args, "setting"), argStr(args, "value")
	if err := cloudflare.ValidateSetting(id, val); err != nil {
		return "", err
	}
	if err := s.agentGateAs(r, 0, "Cloudflare", "cf_zone_setting_set", fmt.Sprintf("Cloudflare %s: set %s = %s", z.zoneName, id, val), gateHigh); err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodPut, z.path("/settings"), map[string]string{"id": id, "value": val})
	if err != nil {
		return "", err
	}
	return jsonText(m["setting"])
}

func toolCFAccessRules(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	z, err := s.cfZoneOf(r, args)
	if err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodGet, z.path("/access-rules"), nil)
	if err != nil {
		return "", err
	}
	return jsonText(pickAll(m["rules"], "id", "mode", "target", "value", "notes"))
}

func toolCFAccessRuleChange(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	action := argStr(args, "action")
	if action != "create" && action != "delete" {
		return "", fmt.Errorf("action must be create or delete")
	}
	z, err := s.cfZoneOf(r, args)
	if err != nil {
		return "", err
	}

	if action == "delete" {
		rid := argStr(args, "rule_id")
		if rid == "" {
			return "", fmt.Errorf("rule_id is required for delete (see cf_access_rules)")
		}
		m, err := s.loopJSON(r.Context(), http.MethodGet, z.path("/access-rules"), nil)
		if err != nil {
			return "", err
		}
		var cur map[string]interface{}
		rules, _ := m["rules"].([]interface{})
		for _, it := range rules {
			if rule, _ := it.(map[string]interface{}); rule["id"] == rid {
				cur = rule
			}
		}
		if cur == nil {
			return "", fmt.Errorf("access rule %q not found in zone %s", rid, z.zoneName)
		}
		summary := fmt.Sprintf("Cloudflare %s: delete access rule %v %v=%v", z.zoneName, cur["mode"], cur["target"], cur["value"])
		if err := s.agentGateAs(r, 0, "Cloudflare", "cf_access_rule_change", summary, gateHigh); err != nil {
			return "", err
		}
		if _, err := s.loopJSON(r.Context(), http.MethodDelete, z.path("/access-rules/"+url.PathEscape(rid)), nil); err != nil {
			return "", err
		}
		return "deleted", nil
	}

	in := cloudflare.AccessRuleInput{Mode: argStr(args, "mode"), Target: argStr(args, "target"), Value: argStr(args, "value"), Notes: argStr(args, "notes")}
	// The handler validates again; checking here keeps the approval card for requests that can run.
	if err := in.Validate(); err != nil {
		return "", err
	}
	summary := fmt.Sprintf("Cloudflare %s: create access rule %s %s=%s", z.zoneName, in.Mode, in.Target, in.Value)
	if in.Notes != "" {
		summary += " (" + in.Notes + ")"
	}
	if err := s.agentGateAs(r, 0, "Cloudflare", "cf_access_rule_change", summary, gateHigh); err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodPost, z.path("/access-rules"), in)
	if err != nil {
		return "", err
	}
	rule, _ := m["rule"].(map[string]interface{})
	return jsonText(pick(rule, "id", "mode", "target", "value", "notes"))
}

func toolCFTunnels(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	acc, err := s.cfAccountOf(args)
	if err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/cloudflare/accounts/%d/tunnels", acc), nil)
	if err != nil {
		return "", err
	}
	return jsonText(pickAll(m["tunnels"], "name", "status", "tun_type", "created_at", "conns_active_at", "conns_inactive_at"))
}

// toolCFDNSExport returns the zone's BIND file. The endpoint answers text, not JSON, so it reads the raw body.
func toolCFDNSExport(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	z, err := s.cfZoneOf(r, args)
	if err != nil {
		return "", err
	}
	code, b := s.loopback(r.Context(), http.MethodGet, z.path("/dns/export"), nil)
	if code/100 != 2 {
		return "", fmt.Errorf("%s (HTTP %d)", clipTo(strings.TrimSpace(string(b)), 300), code)
	}
	return clipTo(string(b), 16<<10), nil
}

func toolCFTokenVerify(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	acc, err := s.cfAccountOf(args)
	if err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/cloudflare/accounts/%d", acc), nil)
	if err != nil {
		// A refused token is the answer to this tool, not a failure of it: report it as the finding.
		return "", fmt.Errorf("%v (the token is refused: check its permissions and expiry in Cloudflare)", err)
	}
	account, _ := m["account"].(map[string]interface{})
	out := pick(account, "name", "account_id")
	out["token"] = m["token"]
	return jsonText(out)
}
