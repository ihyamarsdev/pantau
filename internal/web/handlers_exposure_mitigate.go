package web

// Rapid mitigation actions for the CVE Exposure Radar (ADR 0088). Every mutating action requires
// an explicit `confirm: true` in the request body, which the UI only sends after a danger dialog.
// Actions delegate to the existing, already-validated subsystems (firewall, Cloudflare, Ansible,
// package upgrade) rather than reimplementing their validation or privilege handling.

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"pantau/internal/ansible"
	"pantau/internal/cloudflare"
	"pantau/internal/inspector"
	"pantau/internal/logger"
	"pantau/internal/store"
)

type mitigateRequest struct {
	Action      string `json:"action"`        // upgrade_package | ansible_patch | firewall_restrict | cf_lockdown
	Confirm     bool   `json:"confirm"`       // must be true; UI sets it only after explicit confirmation
	Packages    string `json:"packages"`      // upgrade_package: comma/space separated package names
	Backend     string `json:"backend"`       // firewall_restrict: ufw|firewalld|iptables
	Port        string `json:"port"`          // firewall_restrict: port to deny
	Source      string `json:"source"`        // firewall_restrict: optional CIDR source
	Zone        string `json:"zone"`          // firewall_restrict: firewalld zone (default public)
	ZoneID      string `json:"zone_id"`       // cf_lockdown: Cloudflare zone id
	CFAccountID int64  `json:"cf_account_id"` // cf_lockdown: stored Cloudflare account id
	CFMode      string `json:"cf_mode"`       // cf_lockdown: challenge|block|security_level
	CFValue     string `json:"cf_value"`      // cf_lockdown: IP/CIDR for block, or security level value
	Module      string `json:"module"`        // ansible_patch: ansible module (default apt/dnf shell)
	Args        string `json:"args"`          // ansible_patch: module args
}

// handleExposureMitigate dispatches POST /api/hosts/{id}/exposure/mitigate.
func (s *Server) handleExposureMitigate(w http.ResponseWriter, r *http.Request, hostID int64) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	host, err := s.db.GetHost(hostID)
	if err != nil || host == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "host not found"})
		return
	}
	var req mitigateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "bad request"})
		return
	}
	if !req.Confirm {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "error": "explicit confirmation is required (confirm: true)",
		})
		return
	}

	switch req.Action {
	case "upgrade_package":
		s.mitigateUpgradePackage(w, r, host, req)
	case "firewall_restrict":
		s.mitigateFirewall(w, hostID, req)
	case "cf_lockdown":
		s.mitigateCloudflare(w, r, req)
	case "ansible_patch":
		s.mitigateAnsible(w, hostID, req)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "unknown action: " + req.Action})
	}
}

// upgrade_package validates the targeted packages and returns the existing upgrade WebSocket URL
// the UI opens. The WebSocket path owns the in-flight lock, privilege fallback and post-upgrade rescan.
func (s *Server) mitigateUpgradePackage(w http.ResponseWriter, r *http.Request, host *store.Host, req mitigateRequest) {
	names, err := inspector.ParsePackageNames(req.Packages)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	preview, err := s.inspector.GetUpgradePreview(r.Context(), host.ID)
	if err != nil || preview == nil || !preview.Supported {
		msg := "package upgrade is not supported for this host"
		if err != nil {
			msg = err.Error()
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": msg})
		return
	}
	if _, err := inspector.BuildPackageCommand(preview.Manager, "upgrade", names); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	logger.Info("exposure", host.Name, "Prepared targeted package upgrade: "+strings.Join(names, ","))
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "action": "upgrade_package", "manager": preview.Manager,
		"websocket": "/ws/pkg/upgrade?host_id=" + strconv.FormatInt(host.ID, 10) +
			"&action=upgrade&packages=" + url.QueryEscape(strings.Join(names, ",")),
		"packages": names,
	})
}

// firewall_restrict applies an emergency deny rule through the validated firewall subsystem.
func (s *Server) mitigateFirewall(w http.ResponseWriter, hostID int64, req mitigateRequest) {
	rule := &inspector.FirewallRule{Port: strings.TrimSpace(req.Port), Source: strings.TrimSpace(req.Source)}
	switch strings.ToLower(req.Backend) {
	case "ufw":
		rule.Action, rule.Direction, rule.Proto = "deny", "in", "tcp"
	case "firewalld":
		// A port-kind rule is simple, validated by buildFirewallSteps and easy to undo.
		port := strings.TrimSpace(req.Port)
		if port == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "port is required"})
			return
		}
		zone := strings.TrimSpace(req.Zone)
		if zone == "" {
			zone = "public"
		}
		rule.Kind, rule.Zone, rule.Value = "port", zone, port+"/tcp"
	case "iptables":
		rule.Chain, rule.Action, rule.Proto = "INPUT", "DROP", "tcp"
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "backend must be ufw, firewalld or iptables"})
		return
	}
	if strings.TrimSpace(req.Port) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "port is required"})
		return
	}
	fwReq := inspector.FirewallRequest{Backend: strings.ToLower(req.Backend), Action: "add", Rule: rule}
	res, err := s.inspector.FirewallApply(hostID, fwReq)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, inspector.ErrFirewallProtected):
			status = http.StatusForbidden
		case errors.Is(err, inspector.ErrFirewallInvalid):
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	logger.Info("exposure", strconv.FormatInt(hostID, 10), "Emergency firewall deny applied via "+req.Backend)
	writeJSON(w, http.StatusOK, map[string]any{"ok": res.Succeeded(), "action": "firewall_restrict", "result": res})
}

// cf_lockdown sets an emergency zone security level or creates a blocking access rule.
func (s *Server) mitigateCloudflare(w http.ResponseWriter, r *http.Request, req mitigateRequest) {
	account, err := s.db.GetCloudflareAccount(req.CFAccountID)
	if err != nil || account == nil || account.APIToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "Cloudflare account not found or has no token"})
		return
	}
	if !cloudflare.ValidID(req.ZoneID) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid zone id"})
		return
	}
	cf := s.cfClient(account.APIToken)
	mode := strings.ToLower(strings.TrimSpace(req.CFMode))
	switch mode {
	case "security_level", "":
		value := strings.TrimSpace(req.CFValue)
		if value == "" {
			value = "under_attack"
		}
		if err := cloudflare.ValidateSetting("security_level", value); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		setting, err := cf.SetZoneSetting(r.Context(), req.ZoneID, "security_level", value)
		if err != nil {
			s.writeCFError(w, err)
			return
		}
		logger.Info("exposure", req.ZoneID, "Emergency Cloudflare security level set to "+value)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "action": "cf_lockdown", "setting": setting})
	case "block", "challenge", "managed_challenge":
		in := cloudflare.AccessRuleInput{Mode: mode, Target: "ip", Value: strings.TrimSpace(req.CFValue),
			Notes: "Pantau CVE Exposure Radar emergency lockdown"}
		if strings.Contains(req.CFValue, "/") {
			in.Target = "ip_range"
		}
		if err := in.Validate(); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		rule, err := cf.CreateAccessRule(r.Context(), req.ZoneID, in)
		if err != nil {
			s.writeCFError(w, err)
			return
		}
		logger.Info("exposure", req.ZoneID, "Emergency Cloudflare "+mode+" access rule created")
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "action": "cf_lockdown", "rule": rule})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "cf_mode must be security_level, block, challenge or managed_challenge"})
	}
}

// ansible_patch queues an emergency ad-hoc patch run on the host through the existing manager.
func (s *Server) mitigateAnsible(w http.ResponseWriter, hostID int64, req mitigateRequest) {
	if s.ansibleAdhocMgr == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "Ansible is not available"})
		return
	}
	targets, lerr := s.resolveAdhocTargets([]int64{hostID}, "")
	if lerr != nil {
		writeJSON(w, lerr.Status, map[string]any{"ok": false, "error": lerr.Msg})
		return
	}
	module := strings.TrimSpace(req.Module)
	if module == "" {
		module = "ansible.builtin.shell"
	}
	spec := ansible.AdhocSpec{Module: module, Args: req.Args, Pattern: "all", Targets: targets,
		Become: true, LibraryDir: s.ansibleLibDir}
	if err := spec.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	run := &store.AnsibleAdhocRun{Module: spec.Module, Args: spec.Args, Pattern: "all",
		Targets: "[]", Become: true, Results: "[]"}
	if _, err := s.db.CreateAnsibleAdhocRun(run); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.ansibleAdhocMgr.Start(run, spec); err != nil {
		_ = s.db.FinishAnsibleAdhocRun(run.ID, "failed", nil, "", "[]")
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	logger.Info("exposure", strconv.FormatInt(hostID, 10), "Queued emergency Ansible patch run "+strconv.FormatInt(run.ID, 10))
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "action": "ansible_patch", "run": adhocRunToView(*run)})
}
