package web

import (
	"io"
	"net"
	"net/http"
	"path"
	"strings"
)

// webhookPrefix is the public Terraform webhook route. It authenticates with
// token + HMAC and may arrive through a reverse tunnel with an arbitrary Host,
// so it bypasses both checks below.
const webhookPrefix = "/api/terraform/webhook/"

// mcpPath is the AI agent's MCP endpoint. Hermes reaches it by service name (http://pantau:8080/mcp), which the
// Host check would reject. It carries no cookie auth (rebinding and CSRF target ambient browser credentials) and
// requires the agent bearer token.
const mcpPath = "/mcp"

// Guard rejects requests that a local browser tab did not originate: DNS
// rebinding (Host is a hostname other than localhost) and cross-site requests
// (another app on a different local port posting to Pantau).
//
// shortcut: only IP literals and "localhost" are accepted, so named hosts
// (reverse proxy, mDNS) are rejected; add a config allowlist flag if that is
// ever needed.
func Guard(next http.Handler) http.Handler {
	protection := http.NewCrossOriginProtection()
	protection.AddInsecureBypassPattern(webhookPrefix)
	protection.AddInsecureBypassPattern(mcpPath)
	csrf := protection.Handler(next)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The webhook is reached through a reverse tunnel with an arbitrary Host.
		if !isWebhookPath(r.URL.Path) && path.Clean(r.URL.Path) != mcpPath && !hostAllowed(r.Host) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, "Pantau only answers requests addressed to localhost or an IP address.\nOpen Pantau via http://localhost:<port> or via its IP address.\n")
			return
		}
		csrf.ServeHTTP(w, r)
	})
}

// hostAllowed reports whether the Host header names the local machine rather
// than a rebindable hostname.
func hostAllowed(host string) bool {
	h := host
	if hp, _, err := net.SplitHostPort(host); err == nil {
		h = hp
	}
	h = strings.Trim(h, "[]")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	return net.ParseIP(h) != nil
}

// isWebhookPath reports whether the request targets the public webhook route.
// The path is cleaned first so that "/api/terraform/webhook/../secret" cannot
// smuggle its way past the bypass.
func isWebhookPath(p string) bool {
	c := path.Clean(p)
	return c == "/api/terraform/webhook" || strings.HasPrefix(c, webhookPrefix)
}
