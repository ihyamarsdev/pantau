package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"pantau/internal/logger"
	"pantau/internal/store"
)

// loopList runs a request through the real router and decodes a JSON array answer (loopJSON only reads objects).
func (s *Server) loopList(ctx context.Context, method, path string, body interface{}) ([]interface{}, error) {
	code, b := s.loopback(ctx, method, path, body)
	if code/100 != 2 {
		return nil, fmt.Errorf("%s (HTTP %d)", clipTo(strings.TrimSpace(string(b)), 300), code)
	}
	var list []interface{}
	_ = json.Unmarshal(b, &list)
	return list, nil
}

// Agent tools for Pantau's own fleet and configuration (ADR-0066): hosts and host groups, Desired State rules,
// alerts, notes, terminal presets, shortcuts, port forwards, the SSH Key Library, the registries and the
// non-security settings. They call the UI's HTTP handlers in-process (loopback), so the UI's validation, cooldown
// and audit rules apply unchanged.
//
// Gates: 1 routine (a note, a label), 2 destructive on one host (stop a port forward), 3 Pantau-wide (create or
// delete a host, a group, a rule, a shortcut, a preset; anything without a single host). Gate 3 is never
// auto-approved and never covered by a session grant.
//
// No tool takes or returns a credential: a key, a token or a passphrase never crosses the model. Where the UI needs
// one, the tool works with the id of something the operator already stored (an SSH Key Library entry, an S3 account)
// or the action is not offered.

func init() { agentTools = append(agentTools, fleetTools...) }

var (
	pHostName   = map[string]interface{}{"type": "string", "description": "Host name exactly as listed by fleet_hosts"}
	pFwdKeyID   = map[string]interface{}{"type": "integer", "description": "SSH Key Library entry id from ssh_keys"}
	pFwdRuleID  = map[string]interface{}{"type": "integer", "description": "Desired State rule id from fleet_rules"}
	pFwdPreset  = map[string]interface{}{"type": "integer", "description": "Terminal preset id from fleet_presets"}
	pFwdForward = map[string]interface{}{"type": "integer", "description": "Port forward id from fleet_forwards"}
	pFwdS3ID    = map[string]interface{}{"type": "integer", "description": "S3 account id from fleet_s3_accounts"}
)

// fleetRuleKindKnown keeps the rule kinds the UI accepts in one place.
var fleetRuleKindKnown = map[string]bool{
	"container": true, "service": true, "port": true, "disk": true, "cron": true,
	"backup": true, "process": true, "http": true, "tls": true, "file_integrity": true,
}

var fleetTools = []agentTool{
	// ---- hosts
	{Name: "fleet_hosts", Description: "Every managed host with its id, address, group, notes, key id, hardware, lifecycle score and last inspection. Use it to find a host id, its group or its lifecycle state.",
		InputSchema: schema(nil, map[string]interface{}{}), run: toolFleetHosts},
	{Name: "fleet_host_action", Description: "Change one host: create it (new_name, address, port, user, group, notes, commission date, kube mode, key_id from the SSH Key Library), update it (the same fields), delete it, set its notes, set its group, set its commission date, or inspect it now. An existing host is named by host_id or by name (its current name); new_name renames it. Always waits for operator approval.",
		InputSchema: schema([]string{"action"}, map[string]interface{}{"host_id": pHost, "name": pHostName, "action": map[string]interface{}{"type": "string", "enum": []string{"create", "update", "delete", "notes", "group", "commission_date", "inspect"}},
			"new_name": map[string]interface{}{"type": "string", "description": "the name of the host (create) or its new name (update)"}, "address": str, "port": map[string]interface{}{"type": "integer"}, "user": str, "group": str, "notes": str, "commission_date": map[string]interface{}{"type": "string", "description": "YYYY-MM-DD"}, "kube_mode": map[string]interface{}{"type": "string", "enum": []string{"full", "no-exec", "read-only"}}, "key_id": pFwdKeyID}), gate: gateInfra, run: toolFleetHostAction},
	{Name: "fleet_host_groups", Description: "Host Groups: rename one (from, to) on every host that has it, or set the group of one host (host_id or name, group). Always waits for operator approval.",
		InputSchema: schema([]string{"action"}, map[string]interface{}{"action": map[string]interface{}{"type": "string", "enum": []string{"rename", "assign"}}, "from": str, "to": str, "host_id": pHost, "name": pHostName, "group": str}), gate: gateInfra, run: toolFleetHostGroups},
	{Name: "fleet_host_import", Description: "Preview an OpenSSH config (action preview) and register the hosts it lists (action commit). key_id is an SSH Key Library entry used for every imported host; 0 uses Pantau's global key. Always waits for operator approval.",
		InputSchema: schema([]string{"action"}, map[string]interface{}{"action": map[string]interface{}{"type": "string", "enum": []string{"preview", "commit"}}, "content": map[string]interface{}{"type": "string", "description": "ssh config text (preview only)"}, "hosts": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object"}, "description": "hosts to create: name, host, port, user (commit only)"}, "key_id": pFwdKeyID}), gate: gateInfra, run: toolFleetHostImport},
	{Name: "fleet_inspect_all", Description: "Re-run the inspection on every host at once (the UI's Inspect All). May wait for operator approval.",
		InputSchema: schema(nil, map[string]interface{}{}), gate: gateRoutine, run: toolFleetInspectAll},

	// ---- Desired State rules
	{Name: "fleet_rules", Description: "Desired State rules of a host (host_id) or of the whole fleet, with kind, target, expected and enabled state.",
		InputSchema: schema(nil, map[string]interface{}{"host_id": pHost}), run: toolFleetRules},
	{Name: "fleet_rule_action", Description: "Change a Desired State rule: save (create or update by host_id, kind, target, expected, enabled), enable, disable or delete (rule_id). Kinds: container, service, port, disk, cron, backup, process, http, tls, file_integrity. Always waits for operator approval.",
		InputSchema: schema([]string{"action"}, map[string]interface{}{"action": map[string]interface{}{"type": "string", "enum": []string{"save", "enable", "disable", "delete"}}, "rule_id": pFwdRuleID, "host_id": pHost,
			"kind":   map[string]interface{}{"type": "string", "enum": []string{"container", "service", "port", "disk", "cron", "backup", "process", "http", "tls", "file_integrity"}},
			"target": str, "expected": str, "enabled": map[string]interface{}{"type": "boolean"}}), gate: gateInfra, run: toolFleetRuleAction},

	// ---- alerts and notes
	{Name: "fleet_alerts_ack", Description: "Acknowledge every active alert at once. For a single alert use alert_ack (alert_id from list_alerts). May wait for operator approval.",
		InputSchema: schema(nil, map[string]interface{}{}), gate: gateRoutine, run: toolFleetAlertsAck},
	{Name: "fleet_notes", Description: "Pantau's global note (a note every operator sees). May wait for operator approval when changing it.",
		InputSchema: schema(nil, map[string]interface{}{"notes": map[string]interface{}{"type": "string", "description": "the new global note; omit to read it"}}), gate: gateInfra, run: toolFleetNotes},

	// ---- terminal presets and shortcuts
	{Name: "fleet_presets", Description: "Terminal presets (name, command, host scope): list them (host_id, or all for every host), create, update or delete one. Every change always waits for operator approval.",
		InputSchema: schema(nil, map[string]interface{}{"action": map[string]interface{}{"type": "string", "enum": []string{"list", "create", "update", "delete"}, "description": "list is the default"}, "host_id": pHost, "all": map[string]interface{}{"type": "boolean"}, "preset_id": pFwdPreset, "name": str, "command": str, "use_tmux": map[string]interface{}{"type": "boolean"}}), gate: gateInfra, run: toolFleetPresets},
	{Name: "fleet_shortcuts", Description: "The operator's shortcut links (name, url, group): read the list, or replace it whole. Replacing always waits for operator approval.",
		InputSchema: schema(nil, map[string]interface{}{"shortcuts": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object"}, "description": "the whole new list: name, url, group; omit to read"}}), gate: gateInfra, run: toolFleetShortcuts},

	// ---- port forwards
	{Name: "fleet_forwards", Description: "Live port forwards (local port, remote address, host, bytes): list them, start one (host_id, remote_host, remote_port, optional local_port) or stop one (forward_id). Always waits for operator approval.",
		InputSchema: schema(nil, map[string]interface{}{"action": map[string]interface{}{"type": "string", "enum": []string{"list", "start", "stop"}, "description": "list is the default"}, "host_id": pHost, "remote_host": str, "remote_port": map[string]interface{}{"type": "integer"}, "local_port": map[string]interface{}{"type": "integer"}, "forward_id": pFwdForward}), gate: gateHigh, run: toolFleetForwards},

	// ---- SSH Key Library
	{Name: "ssh_keys", Description: "SSH Key Library entries with name, fingerprint, public key and the hosts that use them: list them, or delete one (key_id). Private keys are never shown and never accepted. Every change always waits for operator approval.",
		InputSchema: schema(nil, map[string]interface{}{"action": map[string]interface{}{"type": "string", "enum": []string{"list", "delete"}, "description": "list is the default"}, "key_id": pFwdKeyID}), gate: gateInfra, run: toolSSHKeys},

	// ---- registries and settings
	{Name: "fleet_registry", Description: "Pantau's registries: what = domains (the reverse-proxy domains of every host), databases (the database servers found on a host) or activity (the recent Pantau events).",
		InputSchema: schema([]string{"what"}, map[string]interface{}{"what": map[string]interface{}{"type": "string", "enum": []string{"domains", "databases", "activity"}}, "host_id": pHost, "limit": map[string]interface{}{"type": "integer"}}), run: toolFleetRegistry},
	{Name: "fleet_settings", Description: "Pantau's general settings (inspection interval, backup interval, security scan interval, alert debounce, feature flags): read them, or change the ones that are listed as writable. It never shows a secret and never changes an authentication, 2FA, agent, update or secret setting. Changing always waits for operator approval.",
		InputSchema: schema(nil, map[string]interface{}{"settings": map[string]interface{}{"type": "object", "description": "the settings to change, e.g. {\"poll_interval_sec\":\"300\"}; omit to read"}}), gate: gateInfra, run: toolFleetSettings},
	{Name: "fleet_s3_accounts", Description: "The S3 accounts Pantau stores (name, endpoint, bucket, access key): list them, or test one existing account (account_id) with its stored credentials. Secret keys are never shown and never accepted. Testing always waits for operator approval.",
		InputSchema: schema(nil, map[string]interface{}{"action": map[string]interface{}{"type": "string", "enum": []string{"list", "test"}, "description": "list is the default"}, "account_id": pFwdS3ID}), gate: gateInfra, run: toolFleetS3Accounts},
	{Name: "fleet_notify_channels", Description: "The notification channels Pantau is configured with (telegram, webhook, ntfy, slack, discord, smtp) and whether each one is set, or a test message through all of them. Tokens and passwords are never shown. The test always waits for operator approval.",
		InputSchema: schema(nil, map[string]interface{}{"action": map[string]interface{}{"type": "string", "enum": []string{"list", "test"}, "description": "list is the default"}}), gate: gateInfra, run: toolFleetNotifyChannels},

	// ---- system snapshot
	{Name: "fleet_snapshot", Description: "The System Snapshot state: whether Pantau is fresh, the host count, the GitHub backup settings and the last backup time and status. May wait for operator approval when starting a backup.",
		InputSchema: schema(nil, map[string]interface{}{"action": map[string]interface{}{"type": "string", "enum": []string{"status", "backup"}, "description": "backup pushes the encrypted snapshot to the configured GitHub repository; status only reads"}}), gate: gateInfra, run: toolFleetSnapshot},
}

// ---------------------------------------------------------------- hosts

func toolFleetHosts(s *Server, _ *http.Request, _ map[string]interface{}) (string, error) {
	hosts, err := s.db.ListHosts()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	tsvLine(&b, "id", "name", "address", "group", "status", "os", "hardware", "lifecycle", "commission_date", "key_id", "notes", "last_inspected")
	for _, h := range hosts {
		tsvLine(&b, h.ID, h.Name, fmt.Sprintf("%s@%s:%d", h.User, h.Host, h.Port), h.GroupName, h.Status, h.OSInfo, h.HardwareModel, h.LifecycleScore, h.CommissionDate, h.KeyID, clipTo(h.Notes, 200), h.LastInspected)
	}
	return b.String(), nil
}

// fleetHostOf resolves a host by id, or by name when the operator names it.
func (s *Server) fleetHostOf(args map[string]interface{}) (*store.Host, error) {
	if f, ok := args["host_id"].(float64); ok && f > 0 {
		h, err := s.db.GetHost(int64(f))
		if err != nil {
			return nil, err
		}
		if h == nil {
			return nil, fmt.Errorf("host %d not found; see fleet_hosts", int64(f))
		}
		return h, nil
	}
	name := argStr(args, "name")
	if name == "" {
		return nil, fmt.Errorf("host_id or name is required; see fleet_hosts")
	}
	hosts, err := s.db.ListHosts()
	if err != nil {
		return nil, err
	}
	for i := range hosts {
		if strings.EqualFold(hosts[i].Name, name) {
			return &hosts[i], nil
		}
	}
	return nil, fmt.Errorf("no host named %q; see fleet_hosts", name)
}

func toolFleetHostAction(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	if argStr(args, "action") == "create" {
		return s.fleetHostCreate(r, args)
	}
	h, err := s.fleetHostOf(args)
	if err != nil {
		return "", err
	}
	switch argStr(args, "action") {
	case "update":
		return s.fleetHostUpdate(r, h, args)
	case "delete":
		if err := s.agentGateAs(r, h.ID, h.Name, "fleet_host_action", fmt.Sprintf("delete host %q (%s): its rules, alerts, inspection history and scan data go with it", h.Name, h.Host), gateInfra); err != nil {
			return "", err
		}
		if _, err := s.loopJSON(r.Context(), http.MethodDelete, fmt.Sprintf("/api/hosts/%d", h.ID), nil); err != nil {
			return "", err
		}
		return fmt.Sprintf("host %q deleted", h.Name), nil
	case "notes":
		notes, _ := args["notes"].(string)
		if err := s.agentGateAs(r, h.ID, h.Name, "fleet_host_action", fmt.Sprintf("set the note of host %q to:\n%s", h.Name, clipTo(notes, 2000)), gateInfra); err != nil {
			return "", err
		}
		if _, err := s.loopJSON(r.Context(), http.MethodPut, fmt.Sprintf("/api/hosts/%d/notes", h.ID), map[string]string{"notes": notes}); err != nil {
			return "", err
		}
		return fmt.Sprintf("note of host %q saved", h.Name), nil
	case "group":
		group := argStr(args, "group")
		if err := s.agentGateAs(r, h.ID, h.Name, "fleet_host_action", fmt.Sprintf("move host %q to group %q", h.Name, group), gateInfra); err != nil {
			return "", err
		}
		if _, err := s.loopJSON(r.Context(), http.MethodPost, "/api/hosts/groups", map[string]interface{}{"assignments": []map[string]interface{}{{"id": h.ID, "group_name": group}}}); err != nil {
			return "", err
		}
		return fmt.Sprintf("host %q is now in group %q", h.Name, group), nil
	case "commission_date":
		date := argStr(args, "commission_date")
		if err := s.agentGateAs(r, h.ID, h.Name, "fleet_host_action", fmt.Sprintf("set the commission date of host %q to %q", h.Name, date), gateInfra); err != nil {
			return "", err
		}
		if _, err := s.loopJSON(r.Context(), http.MethodPut, fmt.Sprintf("/api/hosts/%d/commission-date", h.ID), map[string]string{"commission_date": date}); err != nil {
			return "", err
		}
		return fmt.Sprintf("commission date of host %q set to %q", h.Name, date), nil
	case "inspect":
		if err := s.agentGateAs(r, h.ID, h.Name, "fleet_host_action", fmt.Sprintf("re-run the inspection of host %q", h.Name), gateRoutine); err != nil {
			return "", err
		}
		if _, err := s.loopJSON(r.Context(), http.MethodPost, fmt.Sprintf("/api/hosts/%d/inspect", h.ID), nil); err != nil {
			return "", err
		}
		return fmt.Sprintf("inspection of host %q done", h.Name), nil
	default:
		return "", fmt.Errorf("action must be create, update, delete, notes, group, commission_date or inspect")
	}
}

// fleetHostCreate registers a new host. The SSH key comes from the SSH Key Library (key_id), never from the model:
// the UI also accepts a one-time password for key provisioning, and that is the operator's action alone.
func (s *Server) fleetHostCreate(r *http.Request, args map[string]interface{}) (string, error) {
	name, address := argStr(args, "new_name"), argStr(args, "address")
	if name == "" {
		name = argStr(args, "name")
	}
	if name == "" || address == "" {
		return "", fmt.Errorf("name and address are required")
	}
	port := 22
	if f, ok := args["port"].(float64); ok && f > 0 {
		port = int(f)
	}
	user := argStr(args, "user")
	if user == "" {
		user = "root"
	}
	keyID := int64(0)
	if f, ok := args["key_id"].(float64); ok {
		keyID = int64(f)
	}
	body := map[string]interface{}{"name": name, "host": address, "port": port, "user": user, "key_id": keyID}
	if v := argStr(args, "group"); v != "" {
		body["group_name"] = v
	}
	if v, ok := args["notes"].(string); ok {
		body["notes"] = v
	}
	if v := argStr(args, "commission_date"); v != "" {
		body["commission_date"] = v
	}
	if v := argStr(args, "kube_mode"); v != "" {
		body["kube_mode"] = v
	}
	summary := fmt.Sprintf("register the host %q (%s@%s:%d, key_id %d):\n%s", name, user, address, port, keyID, fleetJSON(body))
	if err := s.agentGateAs(r, 0, "Hosts", "fleet_host_action", summary, gateInfra); err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodPost, "/api/hosts", body)
	if err != nil {
		return "", err
	}
	return jsonText(pick(m, "id", "name", "host", "port", "user", "group_name"))
}

// fleetHostUpdate changes one host. The API replaces the whole record, so the fields the agent does not mention are
// taken from the current host.
func (s *Server) fleetHostUpdate(r *http.Request, h *store.Host, args map[string]interface{}) (string, error) {
	body := map[string]interface{}{
		"name": h.Name, "host": h.Host, "port": h.Port, "user": h.User,
		"notes": h.Notes, "group_name": h.GroupName, "commission_date": h.CommissionDate,
		"kube_mode": h.KubeMode, "key_id": h.KeyID,
	}
	if v := argStr(args, "new_name"); v != "" {
		body["name"] = v
	}
	if v := argStr(args, "address"); v != "" {
		body["host"] = v
	}
	if f, ok := args["port"].(float64); ok && f > 0 {
		body["port"] = int(f)
	}
	if v := argStr(args, "user"); v != "" {
		body["user"] = v
	}
	if v, ok := args["notes"].(string); ok {
		body["notes"] = v
	}
	if v := argStr(args, "group"); v != "" {
		body["group_name"] = v
	}
	if v := argStr(args, "commission_date"); v != "" {
		body["commission_date"] = v
	}
	if v := argStr(args, "kube_mode"); v != "" {
		body["kube_mode"] = v
	}
	if f, ok := args["key_id"].(float64); ok {
		body["key_id"] = int64(f)
	} else if strings.TrimSpace(h.CustomKey) != "" {
		// The API never returns the stored key, so a blank one plus this flag leaves it unchanged. Without it the
		// update would silently drop the host's own SSH key.
		body["keep_custom_key"] = true
	}
	summary := fmt.Sprintf("update host %q:\n%s", h.Name, fleetJSON(body))
	if err := s.agentGateAs(r, h.ID, h.Name, "fleet_host_action", summary, gateInfra); err != nil {
		return "", err
	}
	if _, err := s.loopJSON(r.Context(), http.MethodPut, fmt.Sprintf("/api/hosts/%d", h.ID), body); err != nil {
		return "", err
	}
	return fmt.Sprintf("host %q updated", h.Name), nil
}

func toolFleetHostGroups(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	switch argStr(args, "action") {
	case "rename":
		from, to := argStr(args, "from"), argStr(args, "to")
		if from == "" || to == "" {
			return "", fmt.Errorf("from and to are required")
		}
		if err := s.agentGateAs(r, 0, "Host Groups", "fleet_host_groups", fmt.Sprintf("rename host group %q to %q", from, to), gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPut, "/api/hosts/groups/rename", map[string]string{"from": from, "to": to})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("host group %q renamed to %q (%v hosts)", from, to, m["hosts"]), nil
	case "assign":
		h, err := s.fleetHostOf(args)
		if err != nil {
			return "", err
		}
		group := argStr(args, "group")
		if err := s.agentGateAs(r, h.ID, h.Name, "fleet_host_groups", fmt.Sprintf("move host %q to group %q", h.Name, group), gateInfra); err != nil {
			return "", err
		}
		if _, err := s.loopJSON(r.Context(), http.MethodPost, "/api/hosts/groups", map[string]interface{}{"assignments": []map[string]interface{}{{"id": h.ID, "group_name": group}}}); err != nil {
			return "", err
		}
		return fmt.Sprintf("host %q is now in group %q", h.Name, group), nil
	default:
		return "", fmt.Errorf("action must be rename or assign")
	}
}

func toolFleetHostImport(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	switch argStr(args, "action") {
	case "preview":
		content := argStr(args, "content")
		if content == "" {
			return "", fmt.Errorf("content (the ssh config text) is required")
		}
		// The preview is a POST, but a server-side dry run: it changes nothing, so it needs no gate.
		preview := true
		m, err := s.loopJSON(context.WithValue(r.Context(), gateKey{}, &preview), http.MethodPost, "/api/hosts/import/preview", map[string]string{"content": content})
		if err != nil {
			return "", err
		}
		return jsonText(pickAll(m["entries"], "alias", "host", "user", "port", "duplicate", "reason", "same_as"))
	case "commit":
		list, _ := args["hosts"].([]interface{})
		if len(list) == 0 {
			return "", fmt.Errorf("hosts is required for a commit")
		}
		keyID := int64(0)
		if f, ok := args["key_id"].(float64); ok {
			keyID = int64(f)
		}
		summary := fmt.Sprintf("import %d host(s) into Pantau with key_id %d:\n%s", len(list), keyID, fleetJSON(list))
		if err := s.agentGateAs(r, 0, "Host Import", "fleet_host_import", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPost, "/api/hosts/import", map[string]interface{}{"hosts": list, "key_id": keyID})
		if err != nil {
			return "", err
		}
		return jsonText(map[string]interface{}{"created": pickAll(m["created"], "id", "name", "host", "port", "user"), "skipped": m["skipped"]})
	default:
		return "", fmt.Errorf("action must be preview or commit")
	}
}

func toolFleetInspectAll(s *Server, r *http.Request, _ map[string]interface{}) (string, error) {
	if err := s.agentGateAs(r, 0, "Fleet", "fleet_inspect_all", "re-run the inspection on every host", gateRoutine); err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodPost, "/api/hosts/inspect-all", nil)
	if err != nil {
		return "", err
	}
	return jsonText(m)
}

// ---------------------------------------------------------------- Desired State rules

func toolFleetRules(s *Server, _ *http.Request, args map[string]interface{}) (string, error) {
	if f, ok := args["host_id"].(float64); ok && f > 0 {
		rules, err := s.db.ListDesiredRules(int64(f))
		if err != nil {
			return "", err
		}
		return jsonText(rules)
	}
	hosts, err := s.db.ListHosts()
	if err != nil {
		return "", err
	}
	out := []interface{}{}
	for _, h := range hosts {
		rules, err := s.db.ListDesiredRules(h.ID)
		if err != nil {
			return "", err
		}
		for _, r := range rules {
			out = append(out, map[string]interface{}{"id": r.ID, "host_id": r.HostID, "host": h.Name, "kind": r.Kind, "target": r.Target, "expected": r.Expected, "enabled": r.Enabled})
		}
	}
	return jsonText(out)
}

// fleetRuleOf finds a rule by id, so the agent can name its host and target in the approval card.
func (s *Server) fleetRuleOf(id int64) (*store.DesiredRule, string, error) {
	hosts, err := s.db.ListHosts()
	if err != nil {
		return nil, "", err
	}
	for _, h := range hosts {
		rules, err := s.db.ListDesiredRules(h.ID)
		if err != nil {
			return nil, "", err
		}
		for i := range rules {
			if rules[i].ID == id {
				r := rules[i]
				return &r, h.Name, nil
			}
		}
	}
	return nil, "", fmt.Errorf("no rule #%d; see fleet_rules", id)
}

func toolFleetRuleAction(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	switch argStr(args, "action") {
	case "save":
		hostID := argInt(args, "host_id")
		if hostID <= 0 {
			return "", fmt.Errorf("host_id is required to save a rule")
		}
		h, err := s.db.GetHost(hostID)
		if err != nil {
			return "", err
		}
		if h == nil {
			return "", fmt.Errorf("host %d not found; see fleet_hosts", hostID)
		}
		kind, target, expected := argStr(args, "kind"), argStr(args, "target"), argStr(args, "expected")
		if kind == "" || target == "" || expected == "" {
			return "", fmt.Errorf("kind, target and expected are required")
		}
		if !fleetRuleKindKnown[kind] {
			return "", fmt.Errorf("unknown rule kind %q", kind)
		}
		enabled := true
		if v, ok := args["enabled"].(bool); ok {
			enabled = v
		}
		body := map[string]interface{}{"kind": kind, "target": target, "expected": expected, "enabled": enabled}
		summary := fmt.Sprintf("save Desired State rule on host %q: %s %s expected %q (enabled=%v)", h.Name, kind, target, expected, enabled)
		if err := s.agentGateAs(r, h.ID, h.Name, "fleet_rule_action", summary, gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPost, fmt.Sprintf("/api/hosts/%d/rules", hostID), body)
		if err != nil {
			return "", err
		}
		return jsonText(m)
	case "enable", "disable":
		id := argInt(args, "rule_id")
		if id <= 0 {
			return "", fmt.Errorf("rule_id is required")
		}
		rule, hostName, err := s.fleetRuleOf(id)
		if err != nil {
			return "", err
		}
		enabled := argStr(args, "action") == "enable"
		summary := fmt.Sprintf("%s Desired State rule #%d on host %q (%s %s expected %q)", argStr(args, "action"), id, hostName, rule.Kind, rule.Target, rule.Expected)
		if err := s.agentGateAs(r, rule.HostID, hostName, "fleet_rule_action", summary, gateInfra); err != nil {
			return "", err
		}
		body := map[string]interface{}{"kind": rule.Kind, "target": rule.Target, "expected": rule.Expected, "enabled": enabled}
		if _, err := s.loopJSON(r.Context(), http.MethodPost, fmt.Sprintf("/api/hosts/%d/rules", rule.HostID), body); err != nil {
			return "", err
		}
		return fmt.Sprintf("rule #%d %sd", id, argStr(args, "action")), nil
	case "delete":
		id := argInt(args, "rule_id")
		if id <= 0 {
			return "", fmt.Errorf("rule_id is required")
		}
		rule, hostName, err := s.fleetRuleOf(id)
		if err != nil {
			return "", err
		}
		summary := fmt.Sprintf("delete Desired State rule #%d on host %q (%s %s expected %q)", id, hostName, rule.Kind, rule.Target, rule.Expected)
		if err := s.agentGateAs(r, rule.HostID, hostName, "fleet_rule_action", summary, gateInfra); err != nil {
			return "", err
		}
		if _, err := s.loopJSON(r.Context(), http.MethodDelete, fmt.Sprintf("/api/rules/%d", id), nil); err != nil {
			return "", err
		}
		return fmt.Sprintf("rule #%d deleted", id), nil
	default:
		return "", fmt.Errorf("action must be save, enable, disable or delete")
	}
}

// ---------------------------------------------------------------- alerts and notes

// toolFleetAlertsAck acknowledges every active alert. A single alert is acknowledged with alert_ack.
func toolFleetAlertsAck(s *Server, r *http.Request, _ map[string]interface{}) (string, error) {
	if err := s.agentGateAs(r, 0, "Alerts", "fleet_alerts_ack", "acknowledge every active alert", gateRoutine); err != nil {
		return "", err
	}
	if _, err := s.loopJSON(r.Context(), http.MethodPost, "/api/alerts/ack-all", nil); err != nil {
		return "", err
	}
	return "every active alert acknowledged", nil
}

func toolFleetNotes(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	if _, ok := args["notes"]; !ok {
		notes, err := s.db.GetGlobalNotes()
		if err != nil {
			return "", err
		}
		return jsonText(map[string]string{"notes": notes})
	}
	notes, _ := args["notes"].(string)
	if err := s.agentGateAs(r, 0, "Pantau", "fleet_notes", "set the global note:\n"+clipTo(notes, 4000), gateInfra); err != nil {
		return "", err
	}
	if _, err := s.loopJSON(r.Context(), http.MethodPut, "/api/notes", map[string]string{"notes": notes}); err != nil {
		return "", err
	}
	return "global note saved", nil
}

// ---------------------------------------------------------------- presets and shortcuts

func toolFleetPresets(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	switch argStr(args, "action") {
	case "", "list":
		q := url.Values{}
		if args["all"] == true {
			q.Set("host_id", "all")
		} else if f, ok := args["host_id"].(float64); ok && f > 0 {
			q.Set("host_id", strconv.FormatInt(int64(f), 10))
		}
		list, err := s.loopList(r.Context(), http.MethodGet, "/api/presets?"+q.Encode(), nil)
		if err != nil {
			return "", err
		}
		return jsonText(pickAll(list, "id", "host_id", "name", "command", "sort_order", "use_tmux"))
	case "create", "update":
		name, command := argStr(args, "name"), argStr(args, "command")
		if name == "" || command == "" {
			return "", fmt.Errorf("name and command are required")
		}
		body := map[string]interface{}{"name": name, "command": command, "use_tmux": args["use_tmux"] == true}
		if f, ok := args["host_id"].(float64); ok && f > 0 {
			body["host_id"] = int64(f)
		}
		path, method := "/api/presets", http.MethodPost
		if argStr(args, "action") == "update" {
			id := argInt(args, "preset_id")
			if id <= 0 {
				return "", fmt.Errorf("preset_id is required to update a preset")
			}
			path, method = fmt.Sprintf("/api/presets/%d", id), http.MethodPut
		}
		if err := s.agentGateAs(r, 0, "Terminal Presets", "fleet_presets", fmt.Sprintf("%s terminal preset:\n%s", argStr(args, "action"), fleetJSON(body)), gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), method, path, body)
		if err != nil {
			return "", err
		}
		return jsonText(pick(m, "id", "host_id", "name", "command", "use_tmux"))
	case "delete":
		id := argInt(args, "preset_id")
		if id <= 0 {
			return "", fmt.Errorf("preset_id is required")
		}
		if err := s.agentGateAs(r, 0, "Terminal Presets", "fleet_presets", fmt.Sprintf("delete terminal preset #%d", id), gateInfra); err != nil {
			return "", err
		}
		if _, err := s.loopJSON(r.Context(), http.MethodDelete, fmt.Sprintf("/api/presets/%d", id), nil); err != nil {
			return "", err
		}
		return fmt.Sprintf("preset #%d deleted", id), nil
	default:
		return "", fmt.Errorf("action must be list, create, update or delete")
	}
}

func toolFleetShortcuts(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	if _, ok := args["shortcuts"]; !ok {
		m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/shortcuts", nil)
		if err != nil {
			return "", err
		}
		return jsonText(m)
	}
	list, _ := args["shortcuts"].([]interface{})
	if err := s.agentGateAs(r, 0, "Shortcuts", "fleet_shortcuts", "replace the shortcut list with:\n"+fleetJSON(list), gateInfra); err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodPut, "/api/shortcuts", map[string]interface{}{"shortcuts": list})
	if err != nil {
		return "", err
	}
	return jsonText(m)
}

// ---------------------------------------------------------------- port forwards

func toolFleetForwards(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	switch argStr(args, "action") {
	case "", "list":
		code, b := s.loopback(r.Context(), http.MethodGet, "/api/forwards", nil)
		if code != http.StatusOK {
			return "", fmt.Errorf("%s (HTTP %d)", clipTo(strings.TrimSpace(string(b)), 300), code)
		}
		var list []map[string]interface{}
		if err := json.Unmarshal(b, &list); err != nil {
			return "", err
		}
		var out strings.Builder
		tsvLine(&out, "id", "host", "host_id", "remote", "local_port", "bytes_in", "bytes_out", "connections", "error")
		for _, f := range list {
			tsvLine(&out, f["id"], f["host_name"], f["host_id"], fmt.Sprintf("%v:%v", f["remote_host"], f["remote_port"]), f["local_port"], f["bytes_in"], f["bytes_out"], f["active_conns"], f["last_error"])
		}
		return out.String(), nil
	case "start":
		hostID := argInt(args, "host_id")
		if hostID <= 0 {
			return "", fmt.Errorf("host_id is required")
		}
		h, err := s.db.GetHost(hostID)
		if err != nil {
			return "", err
		}
		if h == nil {
			return "", fmt.Errorf("host %d not found; see fleet_hosts", hostID)
		}
		remotePort := int(argInt(args, "remote_port"))
		if remotePort < 1 || remotePort > 65535 {
			return "", fmt.Errorf("remote_port must be 1-65535")
		}
		remoteHost := argStr(args, "remote_host")
		if remoteHost == "" {
			remoteHost = "127.0.0.1"
		}
		body := map[string]interface{}{"host_id": hostID, "remote_host": remoteHost, "remote_port": remotePort, "local_port": int(argInt(args, "local_port"))}
		summary := fmt.Sprintf("open a port forward on host %q: local port -> %s:%d", h.Name, remoteHost, remotePort)
		if err := s.agentGateAs(r, h.ID, h.Name, "fleet_forwards", summary, gateHigh); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPost, "/api/forwards", body)
		if err != nil {
			return "", err
		}
		return jsonText(pick(m, "id", "local_port", "remote_host", "remote_port", "host_name"))
	case "stop":
		id := argInt(args, "forward_id")
		if id <= 0 {
			return "", fmt.Errorf("forward_id is required")
		}
		if err := s.agentGateAs(r, 0, "Port Forwards", "fleet_forwards", fmt.Sprintf("stop port forward #%d", id), gateHigh); err != nil {
			return "", err
		}
		if _, err := s.loopJSON(r.Context(), http.MethodDelete, fmt.Sprintf("/api/forwards/%d", id), nil); err != nil {
			return "", err
		}
		return fmt.Sprintf("port forward #%d stopped", id), nil
	default:
		return "", fmt.Errorf("action must be list, start or stop")
	}
}

// ---------------------------------------------------------------- SSH Key Library

func toolSSHKeys(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	if argStr(args, "action") != "delete" {
		keys, err := s.db.ListSSHKeys()
		if err != nil {
			return "", err
		}
		var b strings.Builder
		tsvLine(&b, "id", "name", "fingerprint", "used_by", "public_key", "created_at")
		for _, k := range keys {
			tsvLine(&b, k.ID, k.Name, k.Fingerprint, k.UsedBy, k.PublicKey, k.CreatedAt)
		}
		return b.String(), nil
	}
	id := argInt(args, "key_id")
	if id <= 0 {
		return "", fmt.Errorf("key_id is required")
	}
	key, err := s.db.GetSSHKey(id)
	if err != nil {
		return "", err
	}
	if key == nil {
		return "", fmt.Errorf("no ssh key #%d; see ssh_keys", id)
	}
	if err := s.agentGateAs(r, 0, "SSH Key Library", "ssh_keys", fmt.Sprintf("delete ssh key %q (#%d); %d host(s) use it", key.Name, id, key.UsedBy), gateInfra); err != nil {
		return "", err
	}
	if _, err := s.loopJSON(r.Context(), http.MethodDelete, fmt.Sprintf("/api/ssh-keys/%d", id), nil); err != nil {
		return "", err
	}
	return fmt.Sprintf("ssh key %q deleted", key.Name), nil
}

// ---------------------------------------------------------------- registries and settings

func toolFleetRegistry(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	switch argStr(args, "what") {
	case "domains":
		m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/domains", nil)
		if err != nil {
			return "", err
		}
		return jsonText(map[string]interface{}{"hosts": m["hosts"], "sites": m["sites"], "unscanned_hosts": m["unscanned_hosts"]})
	case "databases":
		id := argInt(args, "host_id")
		if id <= 0 {
			return "", fmt.Errorf("host_id is required for the databases registry")
		}
		m, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/databases?host_id=%d", id), nil)
		if err != nil {
			return "", err
		}
		return jsonText(m)
	case "activity":
		return toolFleetActivity(r, args)
	default:
		return "", fmt.Errorf("what must be domains, databases or activity")
	}
}

func toolFleetSettings(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	if _, ok := args["settings"]; !ok {
		// The UI's settings API answers in cleartext for a few secrets (telegram token, snapshot passphrase),
		// so the agent reads the general keys from the store instead.
		out := map[string]interface{}{}
		for _, k := range fleetSettingKeys {
			v, err := s.db.GetSetting(k)
			if err != nil {
				return "", err
			}
			out[k] = v
		}
		return jsonText(out)
	}
	set, _ := args["settings"].(map[string]interface{})
	if len(set) == 0 {
		return "", fmt.Errorf("settings is empty")
	}
	for k := range set {
		if !fleetSettingWritable(k) {
			return "", fmt.Errorf("setting %q cannot be changed by the agent", k)
		}
	}
	if err := s.agentGateAs(r, 0, "Pantau", "fleet_settings", "change the Pantau settings:\n"+fleetJSON(set), gateInfra); err != nil {
		return "", err
	}
	if _, err := s.loopJSON(r.Context(), http.MethodPost, "/api/settings", set); err != nil {
		return "", err
	}
	return "settings saved", nil
}

// fleetSettingKeys are the general settings the agent may read. A secret, an authentication, 2FA, agent or update
// setting is never among them.
var fleetSettingKeys = []string{
	"poll_interval_sec", "security_scan_interval_hours", "backup_interval_hours", "alert_debounce_runs",
	"vuln_intel_enabled", "osv_enabled", "iac_gate_enabled", "ansible_enabled", "terraform_enabled",
	"github_backup_enabled", "github_repo", "github_branch", "github_file_path",
	"last_backup_time", "last_backup_status",
}

// fleetSettingWritable lists the settings the agent may change: the general ones of the Settings tab, never a secret,
// never an authentication, 2FA, agent or update setting.
func fleetSettingWritable(key string) bool {
	switch key {
	case "poll_interval_sec", "security_scan_interval_hours", "backup_interval_hours", "alert_debounce_runs",
		"vuln_intel_enabled", "osv_enabled", "iac_gate_enabled", "ansible_enabled", "terraform_enabled",
		"github_backup_enabled", "github_repo", "github_branch", "github_file_path":
		return true
	}
	return false
}

func toolFleetS3Accounts(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	if argStr(args, "action") != "test" {
		list, err := s.db.ListS3Accounts()
		if err != nil {
			return "", err
		}
		var b strings.Builder
		tsvLine(&b, "id", "name", "endpoint", "region", "bucket", "access_key", "prefix", "path_style")
		for _, a := range list {
			tsvLine(&b, a.ID, a.Name, a.Endpoint, a.Region, a.Bucket, a.AccessKey, a.Prefix, a.PathStyle)
		}
		return b.String(), nil
	}
	id := argInt(args, "account_id")
	if id <= 0 {
		return "", fmt.Errorf("account_id is required")
	}
	acct, err := s.db.GetS3Account(id)
	if err != nil {
		return "", err
	}
	if acct == nil {
		return "", fmt.Errorf("no s3 account #%d; see fleet_s3_accounts", id)
	}
	if err := s.agentGateAs(r, 0, "S3", "fleet_s3_accounts", fmt.Sprintf("test the s3 account %q (endpoint %s, bucket %s) with the stored credentials", acct.Name, acct.Endpoint, acct.Bucket), gateInfra); err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodPost, fmt.Sprintf("/api/s3/accounts/%d/test", id), nil)
	if err != nil {
		return "", err
	}
	return jsonText(m)
}

func toolFleetSnapshot(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	if argStr(args, "action") != "backup" {
		m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/snapshot/status", nil)
		if err != nil {
			return "", err
		}
		return jsonText(m)
	}
	repo, _ := s.db.GetSetting("github_repo")
	if err := s.agentGateAs(r, 0, "System Snapshot", "fleet_snapshot", fmt.Sprintf("push the encrypted system snapshot to the GitHub repository %q", repo), gateInfra); err != nil {
		return "", err
	}
	if _, err := s.loopJSON(r.Context(), http.MethodPost, "/api/snapshot/github/sync", map[string]string{}); err != nil {
		return "", err
	}
	return "system snapshot pushed to github", nil
}

// toolFleetActivity reads the activity log (the same ring buffer the UI's live event stream shows).
func toolFleetActivity(_ *http.Request, args map[string]interface{}) (string, error) {
	b := logger.DefaultBroadcaster()
	if b == nil {
		return "no activity log available", nil
	}
	limit := 50
	if f, ok := args["limit"].(float64); ok && f > 0 {
		limit = int(min(f, 200))
	}
	events := b.History()
	if len(events) > limit {
		events = events[len(events)-limit:]
	}
	var out strings.Builder
	tsvLine(&out, "time", "level", "module", "host", "message")
	for _, e := range events {
		tsvLine(&out, e.Timestamp.Format(time.RFC3339), e.Level, e.Module, e.Host, clipTo(e.Message, 300))
	}
	return out.String(), nil
}

// fleetNotifyChannels reads the notification channels Pantau has, or sends a test message through them. The
// channels are settings, not rows: each one is a set of keys, and the secret of each (token, webhook URL, password)
// is never shown.
func toolFleetNotifyChannels(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	if argStr(args, "action") == "test" {
		if s.dispatcher == nil {
			return "", fmt.Errorf("the notification dispatcher is not available")
		}
		if err := s.agentGateAs(r, 0, "Notifications", "fleet_notify_channels", "send a test message through every configured notification channel", gateInfra); err != nil {
			return "", err
		}
		m, err := s.loopJSON(r.Context(), http.MethodPost, "/api/settings/test-notify", nil)
		if err != nil {
			return "", err
		}
		return jsonText(m)
	}
	channels := []struct {
		name string
		keys []string
	}{
		{"telegram", []string{"telegram_token", "telegram_chat_id"}},
		{"webhook", []string{"webhook_url"}},
		{"ntfy", []string{"ntfy_url", "ntfy_token"}},
		{"slack", []string{"slack_webhook_url"}},
		{"discord", []string{"discord_webhook_url"}},
		{"smtp", []string{"smtp_host", "smtp_port", "smtp_user", "smtp_pass", "smtp_from", "smtp_to"}},
	}
	out := []interface{}{}
	for _, c := range channels {
		set := false
		for _, k := range c.keys {
			v, err := s.db.GetSetting(k)
			if err != nil {
				return "", err
			}
			if strings.TrimSpace(v) != "" {
				set = true
			}
		}
		out = append(out, map[string]interface{}{"channel": c.name, "configured": set})
	}
	return jsonText(out)
}

// fleetJSON renders a value for an approval card.
func fleetJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
