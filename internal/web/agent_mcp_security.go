package web

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"

	"pantau/internal/inspector"
)

// Agent tools for the DevSecOps features (ADR-0068..0072): known vulnerabilities of installed packages, Docker
// security (container audit, image scan), account and authorized_keys audit, file integrity diff and IaC/secret
// scans. They call the UI handlers in-process (loopback), so the opt-in settings, cooldowns, in-progress locks and
// secret masking are the UI's. Reads use stored results (no SSH) unless noted. Not offered: creating a File
// Integrity baseline or any Desired State rule (the agent must not redefine "healthy", ADR-0066) and the IaC gate setting.

func init() { agentTools = append(agentTools, securityTools...) }

const secCap = 40 // entries of one findings/vulns list kept in a result; the rest is counted in <key>_total

var (
	pSecKind = map[string]interface{}{"type": "string", "enum": []string{iacSourceRepo, iacSourceTerraform, iacSourceAnsible}, "description": "source kind: repo (Repositories), terraform (workspace) or ansible (playbook)"}
	pSecID   = map[string]interface{}{"type": "integer", "description": "repo_id, workspace_id or playbook_id, as listed by repo_list, tf_workspaces or ansible_playbooks"}
)

var securityTools = []agentTool{
	{Name: "vuln_status", Description: "Known vulnerabilities (OSV, or Arch Security Tracker) of the packages installed on a host, from the stored package inventory. Answers enabled:false when the operator has not turned the feature on; run inventory_scan first when the inventory is empty. Only fixable ones unless unfixed is true; unfixed_count says how many more exist.",
		InputSchema: schema([]string{"host_id"}, map[string]interface{}{"host_id": pHost, "unfixed": map[string]interface{}{"type": "boolean", "description": "include vulnerabilities without a fixed version"}}), run: toolVulnStatus},
	{Name: "docker_security", Description: "Stored Docker security results of a host (no SSH): the container misconfiguration audit (findings with remediation) and every scanned image (vulnerabilities per severity). assessed:false means the audit never ran: use docker_security_scan.",
		InputSchema: schema(hostReq, map[string]interface{}{"host_id": pHost}), run: toolDockerSecurity},
	{Name: "docker_security_scan", Description: "Run a Docker security scan now: what = audit (container misconfigurations of every container) or image (vulnerability scan of one image, needs image, with the trivy/grype installed on the host). Read the stored result with docker_security. May wait for operator approval.", gate: gateRoutine,
		InputSchema: schema([]string{"host_id", "what"}, map[string]interface{}{"host_id": pHost, "what": map[string]interface{}{"type": "string", "enum": []string{"audit", "image"}}, "image": map[string]interface{}{"type": "string", "description": "image reference as listed by docker_info images (what = image)"}}), run: toolDockerSecurityScan},
	{Name: "account_audit", Description: "Stored account audit of a host (no SSH): login accounts, extra UID 0, empty passwords, sudo grants, authorized_keys (fingerprints only) with unknown or weak keys, and findings. audited:false means it never ran: use account_audit_scan.",
		InputSchema: schema(hostReq, map[string]interface{}{"host_id": pHost}), run: toolAccountAudit},
	{Name: "account_audit_scan", Description: "Run the read-only account and authorized_keys audit on a host now (one per minute per host). Read the result with account_audit. May wait for operator approval.", gate: gateRoutine,
		InputSchema: schema(hostReq, map[string]interface{}{"host_id": pHost}), run: toolAccountAuditScan},
	{Name: "fleet_keys", Description: "authorized_keys of every audited host grouped by fingerprint (public key metadata only): which hosts and accounts accept each key, whether it is unknown to the SSH key library, weak or shared. hosts_not_audited counts hosts without an account audit.",
		InputSchema: schema(nil, map[string]interface{}{"unknown_only": map[string]interface{}{"type": "boolean"}, "weak_only": map[string]interface{}{"type": "boolean"}}), run: toolFleetKeys},
	{Name: "fim_diff", Description: "File Integrity Monitoring: compare a host's files with the baseline of one file_integrity Desired State rule (rule id from get_host_state) now, over SSH, read-only. Lists added, removed and modified files and the paths that were denied or missing. The agent cannot create or change the baseline.",
		InputSchema: schema([]string{"host_id", "rule_id"}, map[string]interface{}{"host_id": pHost, "rule_id": map[string]interface{}{"type": "integer", "description": "id of the file_integrity rule in get_host_state"}}), run: toolFIMDiff},
	{Name: "iac_findings", Description: "Infrastructure-as-code and secret scan results, stored. Without arguments: the latest scan digest of every source (repository, Terraform workspace, Ansible playbook) and whether the IaC gate is on. With kind and id: that source's findings (file, line, severity, remediation; secret values already masked).",
		InputSchema: schema(nil, map[string]interface{}{"kind": pSecKind, "id": pSecID}), run: toolIacFindings},
	{Name: "iac_scan", Description: "Scan a repository, Terraform workspace or Ansible playbook for misconfigurations and committed secrets now and store the result; nothing is changed in the source. Read it with iac_findings. May wait for operator approval.", gate: gateRoutine,
		InputSchema: schema([]string{"kind", "id"}, map[string]interface{}{"kind": pSecKind, "id": pSecID}), run: toolIacScan},
}

// capList keeps the first n entries of m[key] and records the full count under key+"_total" when it cut.
func capList(m map[string]interface{}, key string, n int) {
	if l, ok := m[key].([]interface{}); ok && len(l) > n {
		m[key] = l[:n]
		m[key+"_total"] = len(l)
	}
}

// capScan caps the lists of one Docker security scan (the audit or one image).
func capScan(v interface{}) {
	if m, ok := v.(map[string]interface{}); ok {
		capList(m, "findings", secCap)
		capList(m, "vulns", secCap)
	}
}

// capIntel cuts the CVE map of the exploit intelligence (one entry per CVE) like the other lists.
// The kept entries are the first ones by CVE id, so the answer is the same every time.
func capIntel(v interface{}) {
	m, ok := v.(map[string]interface{})
	if !ok {
		return
	}
	cves, ok := m["cves"].(map[string]interface{})
	if !ok || len(cves) <= secCap {
		return
	}
	ids := make([]string, 0, len(cves))
	for id := range cves {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	cut := map[string]interface{}{}
	for _, id := range ids[:secCap] {
		cut[id] = cves[id]
	}
	m["cves"] = cut
	m["cves_total"] = len(cves)
}

func toolVulnStatus(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	path := fmt.Sprintf("/api/hosts/%d/security/vulns", id)
	if b, _ := args["unfixed"].(bool); b {
		path += "?unfixed=1"
	}
	m, err := s.loopJSON(r.Context(), http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	capList(m, "vulns", secCap)
	return jsonText(m)
}

func toolDockerSecurity(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/hosts/%d/docker/security", id), nil)
	if err != nil {
		return "", err
	}
	capScan(m["audit"])
	if imgs, ok := m["images"].([]interface{}); ok {
		for _, im := range imgs {
			capScan(im)
		}
		capList(m, "images", 10)
	}
	// The exploit intelligence has its own endpoint (the stored results are answered from the database only).
	if im, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/hosts/%d/docker/security/intel", id), nil); err == nil {
		capIntel(im["intel"])
		m["intel"] = im["intel"]
	}
	return jsonText(m)
}

func toolDockerSecurityScan(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	base := fmt.Sprintf("/api/hosts/%d/docker/security/", id)
	var path, summary string
	var body interface{}
	switch argStr(args, "what") {
	case "audit":
		path, summary = base+"audit", "Docker container security audit"
	case "image":
		img := argStr(args, "image")
		if !inspector.ValidDockerImage(img) {
			return "", fmt.Errorf("image must be a valid image reference, see docker_info images")
		}
		path, summary, body = base+"scan-image", "Docker image vulnerability scan: "+img, map[string]string{"image": img}
	default:
		return "", fmt.Errorf("what must be audit or image")
	}
	if err := s.agentGate(r, id, "docker_security_scan", summary, gateRoutine); err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodPost, path, body)
	if err != nil {
		return "", err
	}
	capScan(m["audit"])
	capScan(m["scan"])
	return jsonText(m)
}

func toolAccountAudit(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/hosts/%d/security/accounts", id), nil)
	if err != nil {
		return "", err
	}
	capAccountAudit(m)
	return jsonText(m)
}

func capAccountAudit(m map[string]interface{}) {
	if a, ok := m["audit"].(map[string]interface{}); ok {
		capList(a, "findings", secCap)
		capList(a, "users", secCap)
		capList(a, "keys", secCap)
	}
}

func toolAccountAuditScan(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	if err := s.agentGate(r, id, "account_audit_scan", "account and authorized_keys audit (read-only)", gateRoutine); err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodPost, fmt.Sprintf("/api/hosts/%d/security/accounts/scan", id), nil)
	if err != nil {
		return "", err
	}
	capAccountAudit(m)
	return jsonText(m)
}

func toolFleetKeys(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	q := url.Values{}
	if b, _ := args["unknown_only"].(bool); b {
		q.Set("unknown", "1")
	}
	if b, _ := args["weak_only"].(bool); b {
		q.Set("weak", "1")
	}
	m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/security/keys?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	capList(m, "keys", secCap)
	return jsonText(m)
}

func toolFIMDiff(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	id, err := argHost(args)
	if err != nil {
		return "", err
	}
	rule := argInt(args, "rule_id")
	if rule < 1 {
		return "", fmt.Errorf("rule_id (integer) is required")
	}
	m, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/hosts/%d/rules/fim/diff?rule_id=%d", id, rule), nil)
	if err != nil {
		return "", err
	}
	if d, ok := m["diff"].(map[string]interface{}); ok {
		for _, k := range []string{"added", "removed", "modified"} {
			capList(d, k, secCap)
		}
	}
	capList(m, "denied", secCap)
	capList(m, "missing", secCap)
	return jsonText(m)
}

func toolIacFindings(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	kind, id := argStr(args, "kind"), argInt(args, "id")
	if kind == "" && id == 0 {
		m, err := s.loopJSON(r.Context(), http.MethodGet, "/api/iac-scan", nil)
		if err != nil {
			return "", err
		}
		return jsonText(m)
	}
	if err := checkIacSource(kind, id); err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodGet, fmt.Sprintf("/api/iac-scan/%s/%d", kind, id), nil)
	if err != nil {
		return "", err
	}
	if sc, ok := m["scan"].(map[string]interface{}); ok {
		capList(sc, "findings", secCap)
	}
	return jsonText(m)
}

func toolIacScan(s *Server, r *http.Request, args map[string]interface{}) (string, error) {
	kind, id := argStr(args, "kind"), argInt(args, "id")
	if err := checkIacSource(kind, id); err != nil {
		return "", err
	}
	label := fmt.Sprintf("%s #%d", kind, id)
	if err := s.agentGateAs(r, 0, label, "iac_scan", "IaC and secret scan of "+label, gateRoutine); err != nil {
		return "", err
	}
	m, err := s.loopJSON(r.Context(), http.MethodPost, fmt.Sprintf("/api/iac-scan/%s/%d", kind, id), nil)
	if err != nil {
		return "", err
	}
	if sc, ok := m["scan"].(map[string]interface{}); ok {
		capList(sc, "findings", secCap)
	}
	return jsonText(m)
}

// checkIacSource keeps the loopback path built from a known kind and a positive id.
func checkIacSource(kind string, id int64) error {
	if (kind != iacSourceRepo && kind != iacSourceTerraform && kind != iacSourceAnsible) || id < 1 {
		return fmt.Errorf("kind (repo, terraform or ansible) and id (integer) are required together")
	}
	return nil
}
