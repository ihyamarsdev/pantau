package inspector

import (
	"testing"
)

func TestParseTraefikRuleVariants(t *testing.T) {
	cases := []struct {
		rule  string
		hosts []string
		regex bool
	}{
		{"Host(`a.example.com`, `b.example.com`)", []string{"a.example.com", "b.example.com"}, false},
		{"Host(`a.example.com`) || Host(`b.example.com`)", []string{"a.example.com", "b.example.com"}, false},
		{"HostRegexp(`{sub:[a-z]+}.example.com`)", []string{"{sub:[a-z]+}.example.com"}, true},
		{"Host:a.example.com,b.example.com", []string{"a.example.com", "b.example.com"}, false},
	}
	for _, c := range cases {
		hosts, regex := parseTraefikRule(c.rule)
		if len(hosts) != len(c.hosts) {
			t.Errorf("%s: expected %v, got %v", c.rule, c.hosts, hosts)
			continue
		}
		for i := range c.hosts {
			if hosts[i] != c.hosts[i] {
				t.Errorf("%s: expected %v, got %v", c.rule, c.hosts, hosts)
			}
		}
		if regex != c.regex {
			t.Errorf("%s: expected regex %v, got %v", c.rule, c.regex, regex)
		}
	}
}

func TestParseContainerLabelsTraefik(t *testing.T) {
	containers := []dockerContainerInfo{{
		Name:  "web",
		Image: "traefik:v3",
		Labels: map[string]string{
			"traefik.http.routers.api.rule":                          "Host(`api.example.com`)",
			"traefik.http.routers.api.tls":                           "true",
			"traefik.http.routers.api.entrypoints":                   "websecure",
			"traefik.http.routers.api.service":                       "api-svc",
			"traefik.http.services.api-svc.loadbalancer.server.port": "8080",
		},
		Env: map[string]string{},
	}}
	sites, proxies, _ := parseContainerLabels(containers)
	if len(sites) != 1 {
		t.Fatalf("expected 1 site, got %d: %+v", len(sites), sites)
	}
	s := sites[0]
	if s.Domain != "api.example.com" || s.Proxy != "traefik" || s.Container != "web" {
		t.Fatalf("unexpected site: %+v", s)
	}
	if !s.TLS {
		t.Error("expected TLS from the router label")
	}
	if len(s.Upstreams) != 1 || s.Upstreams[0] != "web:8080" {
		t.Errorf("unexpected upstreams: %+v", s.Upstreams)
	}
	if s.Source != "docker label traefik.http.routers.api.rule" {
		t.Errorf("unexpected source: %q", s.Source)
	}
	if len(proxies) != 1 || proxies[0].Method != "docker labels" {
		t.Errorf("unexpected proxies: %+v", proxies)
	}
}

// A router with only tls.certresolver (no tls=true) is still served over TLS.
func TestParseContainerLabelsTraefikCertResolverMeansTLS(t *testing.T) {
	containers := []dockerContainerInfo{{
		Name: "app",
		Labels: map[string]string{
			"traefik.http.routers.app.rule":             "Host(`app.example.com`)",
			"traefik.http.routers.app.tls.certresolver": "le",
		},
		Env: map[string]string{},
	}}
	sites, _, _ := parseContainerLabels(containers)
	if len(sites) != 1 || !sites[0].TLS {
		t.Fatalf("expected one TLS site, got %+v", sites)
	}
}

func TestParseContainerLabelsTraefikDisabled(t *testing.T) {
	containers := []dockerContainerInfo{{
		Name:   "web",
		Labels: map[string]string{"traefik.enable": "false", "traefik.http.routers.api.rule": "Host(`api.example.com`)"},
		Env:    map[string]string{},
	}}
	sites, _, _ := parseContainerLabels(containers)
	if len(sites) != 0 {
		t.Fatalf("expected no site for a disabled container, got %+v", sites)
	}
}

func TestParseContainerLabelsCaddyDockerProxy(t *testing.T) {
	containers := []dockerContainerInfo{{
		Name: "caddy",
		Labels: map[string]string{
			"caddy":                 "app.example.com",
			"caddy.reverse_proxy":   "{{upstreams 80}}",
			"caddy_1":               "second.example.com",
			"caddy_1.reverse_proxy": "{{upstreams 8080}}",
		},
		Env: map[string]string{},
	}}
	sites, _, _ := parseContainerLabels(containers)
	if len(sites) != 2 {
		t.Fatalf("expected 2 sites, got %d: %+v", len(sites), sites)
	}
	first := siteByDomain(sites, "app.example.com")
	if first == nil || len(first.Upstreams) != 1 || first.Upstreams[0] != "caddy:80" {
		t.Fatalf("unexpected first site: %+v", first)
	}
	second := siteByDomain(sites, "second.example.com")
	if second == nil || len(second.Upstreams) != 1 || second.Upstreams[0] != "caddy:8080" {
		t.Fatalf("unexpected second site: %+v", second)
	}
}

func TestParseContainerLabelsNginxProxyEnv(t *testing.T) {
	containers := []dockerContainerInfo{{
		Name:   "nginx-proxy",
		Labels: map[string]string{},
		Env: map[string]string{
			"VIRTUAL_HOST":     "one.example.com,two.example.com",
			"VIRTUAL_PORT":     "3000",
			"LETSENCRYPT_HOST": "one.example.com",
		},
	}}
	sites, proxies, _ := parseContainerLabels(containers)
	if len(sites) != 2 {
		t.Fatalf("expected 2 sites, got %d: %+v", len(sites), sites)
	}
	one := siteByDomain(sites, "one.example.com")
	if one == nil || !one.TLS {
		t.Fatalf("expected TLS from LETSENCRYPT_HOST, got %+v", one)
	}
	if len(one.Upstreams) != 1 || one.Upstreams[0] != "nginx-proxy:3000" {
		t.Errorf("unexpected upstreams: %+v", one.Upstreams)
	}
	if len(proxies) != 1 || proxies[0].Method != "VIRTUAL_HOST env" {
		t.Errorf("unexpected proxies: %+v", proxies)
	}
}

func TestParseDockerContainers(t *testing.T) {
	body := `CONTAINER_NAME web
CONTAINER_IMAGE traefik:v3
CONTAINER_LABELS {"traefik.enable":"true"}
ENV VIRTUAL_HOST=api.example.com
ENV VIRTUAL_PORT=8080
CONTAINER_NAME db
CONTAINER_IMAGE postgres:16
CONTAINER_LABELS null
`
	containers := parseDockerContainers(body)
	if len(containers) != 2 {
		t.Fatalf("expected 2 containers, got %d: %+v", len(containers), containers)
	}
	if containers[0].Name != "web" || containers[0].Image != "traefik:v3" {
		t.Errorf("unexpected container: %+v", containers[0])
	}
	if containers[0].Labels["traefik.enable"] != "true" {
		t.Errorf("unexpected labels: %+v", containers[0].Labels)
	}
	if containers[0].Env["VIRTUAL_HOST"] != "api.example.com" {
		t.Errorf("unexpected env: %+v", containers[0].Env)
	}
	if containers[1].Name != "db" || len(containers[1].Labels) != 0 {
		t.Errorf("unexpected container: %+v", containers[1])
	}
}

func TestParseTraefikAPI(t *testing.T) {
	body := "{\"routers\":[{\"rule\":\"Host(`api.example.com`)\",\"tls\":true,\"service\":\"api@docker\",\"provider\":\"docker\",\"name\":\"api@docker\"}]}"
	sites, _ := parseTraefikAPI(body, "host")
	if len(sites) != 1 {
		t.Fatalf("expected 1 site, got %d: %+v", len(sites), sites)
	}
	if sites[0].Domain != "api.example.com" || !sites[0].TLS {
		t.Fatalf("unexpected site: %+v", sites[0])
	}
	if sites[0].Source != "traefik API" {
		t.Errorf("unexpected source: %q", sites[0].Source)
	}
}
