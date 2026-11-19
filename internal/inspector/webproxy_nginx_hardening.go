package inspector

// Static Nginx hardening checks (ADR 0088). These inspect the already-parsed nginx directive tree
// for common RCE/exposure patterns and produce radar findings. Everything is read-only.

import (
	"fmt"
	"strings"

	"pantau/internal/store"
)

// NginxDumpCmd dumps the effective nginx config for static hardening analysis (read-only).
const NginxDumpCmd = `echo "===NGINX_DUMP==="
if command -v nginx >/dev/null 2>&1; then
  nginx -T 2>/dev/null || sudo -n nginx -T 2>/dev/null || true
fi
echo "===NGINX_DUMP_END==="
`

// NginxHardeningFinding is one static hardening issue with a stable rule id.
type NginxHardeningFinding struct {
	RuleID   string
	Severity string
	Title    string
	Detail   string
	Source   string // file:line
}

// nginxHardeningChecks runs the static checks over an nginx config dump. Exported for tests.
func nginxHardeningChecks(out string) []NginxHardeningFinding {
	files := splitNginxFiles(out)
	var servers []nginxServerRef
	for _, f := range files {
		nodes := parseNginxNodes(tokenizeNginx(f.body))
		collectNginxServers(nodes, f.path, &servers)
	}
	var findings []NginxHardeningFinding
	for _, f := range files {
		nodes := parseNginxNodes(tokenizeNginx(f.body))
		findings = append(findings, checkServerTokens(nodes, f.path)...)
	}
	for _, s := range servers {
		findings = append(findings, checkServerBlockHardening(s)...)
	}
	return findings
}

// checkServerTokens flags an explicit "server_tokens on" (version disclosure). Absence is not a
// finding: nginx's default is "on", but we cannot verify the compiled default, so we only report
// an explicit on to stay honest (ADR 0036 honesty rule).
func checkServerTokens(nodes []*nginxNode, file string) []NginxHardeningFinding {
	var out []NginxHardeningFinding
	var walk func(n *nginxNode)
	walk = func(n *nginxNode) {
		for _, k := range n.kids {
			if k.name == "server_tokens" && len(k.args) > 0 && strings.EqualFold(k.args[0], "on") {
				out = append(out, NginxHardeningFinding{
					RuleID: "nginx_server_tokens_on", Severity: store.ExposureLow,
					Title:  "Nginx server_tokens enabled",
					Detail: "Nginx advertises its exact version in responses and error pages; set server_tokens off.",
					Source: fmt.Sprintf("%s:%d", file, k.line),
				})
			}
			walk(k)
		}
	}
	for _, n := range nodes {
		walk(n)
	}
	return out
}

// checkServerBlockHardening runs per-server checks: alias traversal, PHP execution in writable
// upload dirs, risky fastcgi_split_path_info, and missing request-size/rate boundaries.
func checkServerBlockHardening(s nginxServerRef) []NginxHardeningFinding {
	var out []NginxHardeningFinding
	src := s.file
	var walk func(n *nginxNode, inLocation bool, locationPath string)
	walk = func(n *nginxNode, inLocation bool, locationPath string) {
		for _, k := range n.kids {
			switch k.name {
			case "location":
				path := ""
				if len(k.args) > 0 {
					path = k.args[len(k.args)-1]
				}
				walk(k, true, path)
				continue
			case "alias":
				if inLocation && isUploadLikePath(locationPath) && hasTraversalRisk(locationPath, k.args) {
					out = append(out, NginxHardeningFinding{
						RuleID: "nginx_alias_traversal", Severity: store.ExposureHigh,
						Title:  "Unsafe alias in upload-like location",
						Detail: fmt.Sprintf("location %q uses alias %q; a missing trailing slash or variable can allow path traversal. Prefer root with a fixed path.", locationPath, strings.Join(k.args, " ")),
						Source: fmt.Sprintf("%s:%d", src, k.line),
					})
				}
			case "fastcgi_split_path_info":
				out = append(out, NginxHardeningFinding{
					RuleID: "nginx_fastcgi_split_path_info", Severity: store.ExposureMedium,
					Title:  "Risky fastcgi_split_path_info regex",
					Detail: "A permissive fastcgi_split_path_info regex can enable PATH_INFO-based PHP code execution; restrict it and avoid passing user-controlled paths to the PHP interpreter.",
					Source: fmt.Sprintf("%s:%d", src, k.line),
				})
			case "fastcgi_pass":
				if inLocation && (isUploadLikePath(locationPath) || isPHPLocation(locationPath)) {
					out = append(out, NginxHardeningFinding{
						RuleID: "nginx_php_in_writable_dir", Severity: store.ExposureCritical,
						Title:  "PHP execution inside an upload/writable location",
						Detail: fmt.Sprintf("location %q passes requests to fastcgi; if this directory is writable, an uploaded file can be executed as PHP (webshell RCE). Add 'location ~ \\.php$ { deny all; }' inside upload paths.", locationPath),
						Source: fmt.Sprintf("%s:%d", src, k.line),
					})
				}
			case "client_max_body_size":
				// presence of an explicit bound is good; recorded only to avoid a false "missing" below
			}
			walk(k, inLocation, locationPath)
		}
	}
	walk(s.node, false, "")

	// Missing request boundaries: no client_max_body_size anywhere in the server block.
	if !nodeHasDirective(s.node, "client_max_body_size") {
		out = append(out, NginxHardeningFinding{
			RuleID: "nginx_no_body_limit", Severity: store.ExposureLow,
			Title:  "No client_max_body_size boundary",
			Detail: "The server block sets no request body size limit; large uploads can exhaust disk/memory. Set an explicit client_max_body_size.",
			Source: src,
		})
	}
	if !nodeHasDirective(s.node, "limit_req") && !nodeHasDirective(s.node, "limit_conn") {
		out = append(out, NginxHardeningFinding{
			RuleID: "nginx_no_rate_limit", Severity: store.ExposureLow,
			Title:  "No request rate/connection boundary",
			Detail: "The server block has no limit_req or limit_conn zone; brute-force and request floods are unbounded. Add rate limiting to public locations.",
			Source: src,
		})
	}
	return out
}

// isUploadLikePath recognises common writable/upload location names.
func isUploadLikePath(p string) bool {
	p = strings.ToLower(p)
	for _, part := range []string{"upload", "uploads", "wp-content/uploads", "storage", "tmp", "temp", "writable", "data"} {
		if strings.Contains(p, part) {
			return true
		}
	}
	return false
}

func isPHPLocation(p string) bool {
	return strings.Contains(strings.ToLower(p), ".php")
}

// hasTraversalRisk flags aliases whose target lacks a trailing slash or contains a variable.
func hasTraversalRisk(locationPath string, aliasArgs []string) bool {
	if len(aliasArgs) == 0 {
		return false
	}
	a := aliasArgs[0]
	return !strings.HasSuffix(a, "/") || strings.Contains(a, "$")
}

func nodeHasDirective(n *nginxNode, name string) bool {
	for _, k := range n.kids {
		if k.name == name {
			return true
		}
		if nodeHasDirective(k, name) {
			return true
		}
	}
	return false
}

// NginxDumpHardeningFindings is the exported conversion used by the scan orchestration and tests.
func NginxDumpHardeningFindings(host *store.Host, dump string, domains []store.WebSite, cfProxied map[string]bool) []store.ExposureFinding {
	var out []store.ExposureFinding
	for _, hf := range nginxHardeningChecks(dump) {
		f := store.ExposureFinding{
			ID: "nginx:" + hf.RuleID + ":" + hf.Source, Source: store.ExposureSourceNginx,
			Severity: hf.Severity, Title: hf.Title, Summary: hf.Detail,
			Component: "nginx", Location: hf.Source, CVEs: []string{},
			Mitigations: []string{"firewall_restrict", "cf_lockdown"},
		}
		f.Reachability = CorrelateReachability(host, "nginx", nil, domains, cfProxied)
		out = append(out, f)
	}
	return out
}
