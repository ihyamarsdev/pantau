package inspector

import (
	"encoding/json"
	"strconv"
	"strings"

	"pantau/internal/store"
)

// Caddy parser: reads the Caddy admin API JSON (or the JSON of `caddy adapt`,
// which has the same shape) and falls back to the raw Caddyfile text.
//
// The JSON is walked generically (maps, not a fixed struct) so a route tree of
// any depth (routes, subroutes, grouped handlers) yields its hosts and upstreams.

// parseCaddyConfig returns the site rows of a Caddy configuration. method is
// the collection method ("caddy admin API", "caddy adapt" or "Caddyfile").
func parseCaddyConfig(body, where, container, method string) []store.WebSite {
	if method == "Caddyfile" {
		return parseCaddyfile(body, where, container)
	}
	jsonStart := strings.Index(body, "{")
	if jsonStart < 0 {
		return nil
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(body[jsonStart:]), &cfg); err != nil {
		return nil
	}

	apps, _ := cfg["apps"].(map[string]any)
	httpApp, _ := apps["http"].(map[string]any)
	servers, _ := httpApp["servers"].(map[string]any)

	// TLS is on when a server listens on the HTTPS port (default 443) or has TLS
	// connection policies, unless automatic HTTPS is disabled for that server.
	httpsPort := "443"
	if p, ok := httpApp["https_port"].(float64); ok && p > 0 {
		httpsPort = strconv.Itoa(int(p))
	}

	var sites []store.WebSite
	for _, srvAny := range servers {
		srv, _ := srvAny.(map[string]any)
		if srv == nil {
			continue
		}
		listens := caddyStringList(srv["listen"])
		_, tls := srv["tls_connection_policies"]
		for _, l := range listens {
			if strings.HasSuffix(l, ":"+httpsPort) {
				tls = true
			}
		}
		if ah, ok := srv["automatic_https"].(map[string]any); ok {
			if dis, _ := ah["disable"].(bool); dis {
				tls = false
			}
		}
		routes, _ := srv["routes"].([]any)
		for _, r := range routes {
			hosts, ups, root := collectCaddyRoute(r)
			for _, h := range hosts {
				sites = append(sites, store.WebSite{
					Domain:    h,
					Proxy:     "caddy",
					Where:     where,
					Container: container,
					TLS:       tls,
					Listen:    listens,
					Upstreams: ups,
					Root:      root,
					Source:    method,
				})
			}
		}
	}
	return sites
}

// collectCaddyRoute returns the hosts one top-level route matches and the upstream
// dials and file root of its handler tree, so each site keeps its own targets.
// Nested routes without their own host match belong to the enclosing route.
func collectCaddyRoute(route any) ([]string, []string, string) {
	var hosts, ups []string
	root := ""
	var walk func(node any)
	walk = func(node any) {
		switch n := node.(type) {
		case []any:
			for _, item := range n {
				walk(item)
			}
		case map[string]any:
			if match, ok := n["match"].([]any); ok {
				for _, m := range match {
					mm, _ := m.(map[string]any)
					hosts = append(hosts, caddyStringList(mm["host"])...)
				}
			}
			if handle, ok := n["handle"].([]any); ok {
				for _, h := range handle {
					hm, _ := h.(map[string]any)
					if hm == nil {
						continue
					}
					switch hm["handler"] {
					case "subroute":
						walk(hm["routes"])
					case "reverse_proxy":
						if list, ok := hm["upstreams"].([]any); ok {
							for _, u := range list {
								um, _ := u.(map[string]any)
								ups = append(ups, caddyStringList(um["dial"])...)
							}
						}
					case "file_server", "vars": // `root * /dir` adapts to a vars handler
						if r := caddyStringList(hm["root"]); len(r) > 0 && root == "" {
							root = r[0]
						}
					}
				}
			}
			if r, ok := n["routes"].([]any); ok {
				walk(r)
			}
		}
	}
	walk(route)
	return dedupeStrings(hosts), dedupeStrings(ups), root
}

// caddyStringList reads a JSON value that is a string or a list of strings.
func caddyStringList(v any) []string {
	switch t := v.(type) {
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// parseCaddyfile reads the site addresses of a raw Caddyfile: the addresses on
// the top level (before the opening brace), skipping the global options block
// and the snippets.
func parseCaddyfile(body, where, container string) []store.WebSite {
	var sites []store.WebSite
	address := []string{}
	flush := func() {
		for _, h := range address {
			h = strings.TrimSpace(h)
			if h == "" || strings.HasPrefix(h, "(") {
				continue // snippet
			}
			if !looksLikeCaddyHost(h) {
				continue // global option, not a site address
			}
			sites = append(sites, store.WebSite{
				Domain:    h,
				Proxy:     "caddy",
				Where:     where,
				Container: container,
				Listen:    []string{},
				Upstreams: []string{},
				Source:    "Caddyfile",
			})
		}
		address = nil
	}
	inSnippet := false
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "(") {
			inSnippet = true
			flush()
			continue
		}
		if inSnippet {
			if line == "}" {
				inSnippet = false
			}
			continue
		}
		if line == "{" {
			flush() // global options block
			continue
		}
		if strings.HasSuffix(line, "{") {
			addr := strings.TrimSpace(strings.TrimSuffix(line, "{"))
			for _, h := range strings.Split(addr, ",") {
				h = strings.TrimSpace(h)
				if h != "" {
					address = append(address, h)
				}
			}
			continue
		}
		flush()
	}
	flush()
	return sites
}

// looksLikeCaddyHost reports whether a Caddyfile token is a site address and
// not a global option (debug, email, ...).
func looksLikeCaddyHost(s string) bool {
	return strings.Contains(s, ".") || strings.Contains(s, ":")
}
