package inspector

import (
	"encoding/json"
	"regexp"
	"strings"

	"pantau/internal/store"
)

// Traefik and container label sources: Traefik router labels, caddy-docker-proxy
// labels and the nginx-proxy VIRTUAL_HOST environment.

// dockerContainerInfo is one container of the Docker inventory section.
type dockerContainerInfo struct {
	Name   string
	Image  string
	Labels map[string]string
	Env    map[string]string
}

// parseDockerContainers reads the container inventory section.
func parseDockerContainers(body string) []dockerContainerInfo {
	var out []dockerContainerInfo
	var cur *dockerContainerInfo
	flush := func() {
		if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(body, "\n") {
		switch {
		case strings.HasPrefix(line, "CONTAINER_NAME "):
			flush()
			cur = &dockerContainerInfo{Name: strings.TrimSpace(strings.TrimPrefix(line, "CONTAINER_NAME ")), Labels: map[string]string{}, Env: map[string]string{}}
		case strings.HasPrefix(line, "CONTAINER_IMAGE "):
			if cur == nil {
				cur = &dockerContainerInfo{Labels: map[string]string{}, Env: map[string]string{}}
			}
			cur.Image = strings.TrimSpace(strings.TrimPrefix(line, "CONTAINER_IMAGE "))
		case strings.HasPrefix(line, "CONTAINER_LABELS "):
			if cur == nil {
				cur = &dockerContainerInfo{Labels: map[string]string{}, Env: map[string]string{}}
			}
			raw := strings.TrimSpace(strings.TrimPrefix(line, "CONTAINER_LABELS "))
			if raw == "" || raw == "null" {
				continue
			}
			var labels map[string]string
			if err := json.Unmarshal([]byte(raw), &labels); err == nil {
				cur.Labels = labels
			}
		case strings.HasPrefix(line, "ENV "):
			if cur == nil {
				continue
			}
			kv := strings.TrimSpace(strings.TrimPrefix(line, "ENV "))
			if idx := strings.Index(kv, "="); idx > 0 {
				cur.Env[kv[:idx]] = kv[idx+1:]
			}
		}
	}
	flush()
	return out
}

// traefikService is the load balancer of a Traefik service.
type traefikService struct {
	Container string
	Port      string
}

// parseContainerLabels returns the sites, the proxies and the warnings of the
// label based sources: Traefik router labels, caddy-docker-proxy labels and
// the nginx-proxy VIRTUAL_HOST environment.
func parseContainerLabels(containers []dockerContainerInfo) ([]store.WebSite, []store.WebProxy, []string) {
	var (
		sites    []store.WebSite
		proxies  []store.WebProxy
		warnings []string
	)
	services := collectTraefikServices(containers)
	for _, c := range containers {
		if v, ok := c.Labels["traefik.enable"]; ok && strings.EqualFold(strings.TrimSpace(v), "false") {
			continue
		}
		if s, p := parseTraefikLabels(c, services); len(s) > 0 {
			sites = append(sites, s...)
			proxies = append(proxies, p)
		}
		if s, p := parseCaddyDockerProxyLabels(c); len(s) > 0 {
			sites = append(sites, s...)
			proxies = append(proxies, p)
		}
		if s, p := parseNginxProxyEnv(c); len(s) > 0 {
			sites = append(sites, s...)
			proxies = append(proxies, p)
		}
	}
	return sites, proxies, warnings
}

// collectTraefikServices maps the Traefik service names to their container and
// port (the service label may live on another container than the router).
func collectTraefikServices(containers []dockerContainerInfo) map[string]traefikService {
	services := map[string]traefikService{}
	for _, c := range containers {
		for k, v := range c.Labels {
			if !strings.HasPrefix(k, "traefik.http.services.") {
				continue
			}
			rest := strings.TrimPrefix(k, "traefik.http.services.")
			idx := strings.Index(rest, ".")
			if idx <= 0 {
				continue
			}
			name, field := rest[:idx], rest[idx+1:]
			if field != "loadbalancer.server.port" {
				continue
			}
			services[name] = traefikService{Container: c.Name, Port: v}
		}
	}
	return services
}

// parseTraefikLabels reads the traefik.http.routers.*.rule labels of a container.
func parseTraefikLabels(c dockerContainerInfo, services map[string]traefikService) ([]store.WebSite, store.WebProxy) {
	proxy := store.WebProxy{Kind: "traefik", Where: "container", Container: c.Name, Method: "docker labels"}
	var sites []store.WebSite

	// Group the router labels by router name.
	routers := map[string]map[string]string{}
	for k, v := range c.Labels {
		if !strings.HasPrefix(k, "traefik.http.routers.") {
			continue
		}
		rest := strings.TrimPrefix(k, "traefik.http.routers.")
		idx := strings.Index(rest, ".")
		if idx <= 0 {
			continue
		}
		name, field := rest[:idx], rest[idx+1:]
		if routers[name] == nil {
			routers[name] = map[string]string{}
		}
		routers[name][field] = v
	}

	for name, fields := range routers {
		rule := fields["rule"]
		if rule == "" {
			continue
		}
		hosts, regex := parseTraefikRule(rule)
		if len(hosts) == 0 {
			continue
		}
		// tls=true, or any tls.* option (certresolver, domains, options) enables TLS on the router.
		tls := strings.EqualFold(fields["tls"], "true")
		for f := range fields {
			if strings.HasPrefix(f, "tls.") && !strings.EqualFold(fields["tls"], "false") {
				tls = true
			}
		}
		service := fields["service"]
		if service == "" {
			service = name // the router name is the service name by default
		}
		service = strings.SplitN(service, "@", 2)[0]
		upstream := traefikUpstream(c.Name, service, services)
		var listens []string
		if ep := fields["entrypoints"]; ep != "" {
			listens = strings.Split(ep, ",")
		}
		for _, h := range hosts {
			sites = append(sites, store.WebSite{
				Domain:    h,
				Proxy:     "traefik",
				Where:     "container",
				Container: c.Name,
				TLS:       tls,
				Listen:    listens,
				Upstreams: upstream,
				Source:    "docker label traefik.http.routers." + name + ".rule",
				Regex:     regex,
			})
		}
	}
	if len(sites) == 0 {
		return nil, proxy
	}
	return sites, proxy
}

// traefikUpstream resolves the upstream of a router: the container of its
// service and the load balancer port. Without a known service the upstream is
// unknown.
func traefikUpstream(container, service string, services map[string]traefikService) []string {
	svc, ok := services[service]
	if !ok || svc.Port == "" {
		return nil
	}
	if svc.Container == "" {
		svc.Container = container
	}
	return []string{svc.Container + ":" + svc.Port}
}

// parseTraefikRule extracts the hosts of a Traefik rule. It supports
// Host(`a`, `b`), Host(`a`) || Host(`b`), HostRegexp and the v1 syntax
// "Host:a,b".
func parseTraefikRule(rule string) ([]string, bool) {
	regex := false
	var hosts []string
	// The v1 syntax: "Host:a,b".
	if !strings.Contains(rule, "Host(") {
		if m := regexp.MustCompile(`Host:([^\s]+)`).FindStringSubmatch(rule); m != nil {
			for _, h := range strings.Split(m[1], ",") {
				h = strings.TrimSpace(h)
				if h != "" {
					hosts = append(hosts, h)
				}
			}
			return hosts, regex
		}
	}
	// The v2/v3 syntax: Host(`a`) and HostRegexp(`...`).
	re := regexp.MustCompile(`Host(Regexp)?\(([^)]*)\)`)
	for _, m := range re.FindAllStringSubmatch(rule, -1) {
		if m[1] != "" {
			regex = true
		}
		for _, h := range strings.Split(m[2], ",") {
			h = strings.TrimSpace(h)
			h = strings.Trim(h, "`")
			if h != "" {
				hosts = append(hosts, h)
			}
		}
	}
	return dedupeStrings(hosts), regex
}

// parseTraefikAPI reads the router list of the Traefik API.
func parseTraefikAPI(body, where string) ([]store.WebSite, string) {
	jsonStart := strings.Index(body, "{")
	if jsonStart < 0 {
		return nil, ""
	}
	var payload struct {
		Routers []struct {
			Rule     string `json:"rule"`
			TLS      bool   `json:"tls"`
			Service  string `json:"service"`
			Provider string `json:"provider"`
			Name     string `json:"name"`
		} `json:"routers"`
	}
	if err := json.Unmarshal([]byte(body[jsonStart:]), &payload); err != nil {
		return nil, ""
	}
	var sites []store.WebSite
	for _, r := range payload.Routers {
		hosts, regex := parseTraefikRule(r.Rule)
		if len(hosts) == 0 {
			continue
		}
		for _, h := range hosts {
			sites = append(sites, store.WebSite{
				Domain: h,
				Proxy:  "traefik",
				Where:  where,
				TLS:    r.TLS,
				Source: "traefik API",
				Regex:  regex,
			})
		}
	}
	return sites, ""
}

// parseCaddyDockerProxyLabels reads the caddy and caddy_N labels of a container.
func parseCaddyDockerProxyLabels(c dockerContainerInfo) ([]store.WebSite, store.WebProxy) {
	proxy := store.WebProxy{Kind: "caddy", Where: "container", Container: c.Name, Method: "docker labels"}
	var sites []store.WebSite

	// The plain "caddy" label is the address list of the default site.
	if addr, ok := c.Labels["caddy"]; ok {
		ups := caddyLabelUpstream(c.Labels, c.Name, "caddy.reverse_proxy")
		for _, d := range splitCaddyAddresses(addr) {
			sites = append(sites, store.WebSite{
				Domain:    d,
				Proxy:     "caddy",
				Where:     "container",
				Container: c.Name,
				Upstreams: ups,
				Source:    "docker label caddy",
			})
		}
	}
	// The numbered labels "caddy_1", "caddy_1.reverse_proxy", ...
	for k, v := range c.Labels {
		if !strings.HasPrefix(k, "caddy_") || strings.Contains(k, ".") {
			continue
		}
		ups := caddyLabelUpstream(c.Labels, c.Name, k+".reverse_proxy")
		for _, d := range splitCaddyAddresses(v) {
			sites = append(sites, store.WebSite{
				Domain:    d,
				Proxy:     "caddy",
				Where:     "container",
				Container: c.Name,
				Upstreams: ups,
				Source:    "docker label " + k,
			})
		}
	}
	if len(sites) == 0 {
		return nil, proxy
	}
	return sites, proxy
}

// caddyLabelUpstream resolves the reverse_proxy label of a caddy-docker-proxy
// site: "{{upstreams 80}}" means the container on port 80.
func caddyLabelUpstream(labels map[string]string, container, key string) []string {
	val, ok := labels[key]
	if !ok {
		return nil
	}
	if !strings.Contains(val, "{{upstreams") {
		return []string{val}
	}
	rest := strings.TrimSpace(val[strings.Index(val, "{{upstreams")+len("{{upstreams"):])
	rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(rest, "{{"), "upstreams"))
	port := ""
	if end := strings.IndexAny(rest, "}"); end >= 0 {
		port = strings.TrimSpace(rest[:end])
	}
	if port == "" {
		return nil
	}
	// shortcut: the label does not name the upstream container, the proxy
	// container is used; resolve the real one when the app containers are known.
	return []string{container + ":" + port}
}

// splitCaddyAddresses splits a caddy-docker-proxy address list.
func splitCaddyAddresses(v string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' }) {
		part = strings.TrimSpace(part)
		part = strings.TrimPrefix(part, "https://")
		part = strings.TrimPrefix(part, "http://")
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// parseNginxProxyEnv reads the VIRTUAL_HOST environment of an nginx-proxy
// container.
func parseNginxProxyEnv(c dockerContainerInfo) ([]store.WebSite, store.WebProxy) {
	proxy := store.WebProxy{Kind: "nginx", Where: "container", Container: c.Name, Method: "VIRTUAL_HOST env"}
	vhost, ok := c.Env["VIRTUAL_HOST"]
	if !ok || strings.TrimSpace(vhost) == "" {
		return nil, proxy
	}
	port := c.Env["VIRTUAL_PORT"]
	if port == "" {
		port = "80"
	}
	tls := strings.TrimSpace(c.Env["LETSENCRYPT_HOST"]) != ""
	var sites []store.WebSite
	for _, d := range strings.Split(vhost, ",") {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		sites = append(sites, store.WebSite{
			Domain:    d,
			Proxy:     "nginx",
			Where:     "container",
			Container: c.Name,
			TLS:       tls,
			Upstreams: []string{c.Name + ":" + port},
			Source:    "VIRTUAL_HOST env",
		})
	}
	if len(sites) == 0 {
		return nil, proxy
	}
	return sites, proxy
}

// mergeWebVersions collects the version of one proxy from its version section.
func mergeWebVersions(versions map[string]string, tool, body string) {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch tool {
		case "nginx":
			if strings.HasPrefix(line, "nginx version:") {
				v := strings.TrimSpace(strings.TrimPrefix(line, "nginx version:"))
				versions["nginx"] = strings.TrimPrefix(v, "nginx/")
			}
		case "caddy":
			if strings.HasPrefix(line, "v") {
				versions["caddy"] = strings.TrimSpace(strings.TrimPrefix(line, "v"))
			}
		case "traefik":
			if strings.HasPrefix(line, "v") {
				versions["traefik"] = strings.TrimSpace(strings.TrimPrefix(line, "v"))
			}
		}
	}
}
