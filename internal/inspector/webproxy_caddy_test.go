package inspector

import (
	"testing"
)

func TestParseCaddyConfigJSONWithSubroute(t *testing.T) {
	body := `METHOD caddy admin API
{"apps":{"http":{"servers":{"srv0":{
  "listen":[":443",":80"],
  "routes":[{"match":[{"host":["a.example.com","b.example.com"]}],
    "handle":[{"handler":"subroute","routes":[
       {"match":[{"host":["a.example.com"]}],"handle":[{"handler":"reverse_proxy","upstreams":[{"dial":"127.0.0.1:3000"}]}]},
       {"handle":[{"handler":"file_server","root":"/srv"}]}
    ]}]}
]}}},"tls":{"automatic_https":{"disable":false}}}}`
	sites := parseCaddyConfig(body, "host", "", "caddy admin API")
	if len(sites) != 2 {
		t.Fatalf("expected 2 sites (one per host), got %d: %+v", len(sites), sites)
	}
	a := siteByDomain(sites, "a.example.com")
	if a == nil {
		t.Fatalf("a.example.com not found: %+v", sites)
	}
	if !a.TLS {
		t.Error("expected TLS on the :443 listener")
	}
	if len(a.Upstreams) != 1 || a.Upstreams[0] != "127.0.0.1:3000" {
		t.Errorf("unexpected upstreams: %+v", a.Upstreams)
	}
	if a.Root != "/srv" {
		t.Errorf("unexpected root: %q", a.Root)
	}
	if a.Source != "caddy admin API" {
		t.Errorf("unexpected source: %q", a.Source)
	}
	if siteByDomain(sites, "b.example.com") == nil {
		t.Errorf("b.example.com not found: %+v", sites)
	}
}

func TestParseCaddyConfigJSONNoTLSWhenDisabled(t *testing.T) {
	body := `{"apps":{"http":{"servers":{"srv0":{"listen":[":443"],"automatic_https":{"disable":true},"routes":[
  {"match":[{"host":["a.example.com"]}],"handle":[{"handler":"reverse_proxy","upstreams":[{"dial":"127.0.0.1:3000"}]}]}
]}}}}}`
	sites := parseCaddyConfig(body, "host", "", "caddy adapt")
	if len(sites) != 1 {
		t.Fatalf("expected 1 site, got %d: %+v", len(sites), sites)
	}
	if sites[0].TLS {
		t.Error("expected no TLS when automatic_https is disabled")
	}
	if sites[0].Source != "caddy adapt" {
		t.Errorf("source should be the collection method, got %q", sites[0].Source)
	}
}

// Each top-level route keeps its own upstreams/root, and a custom https_port counts as TLS
// (shape of a real `caddy adapt` of a Caddyfile with two sites and `https_port 9443`).
func TestParseCaddyConfigPerRouteTargets(t *testing.T) {
	body := `{"apps":{"http":{"https_port":9443,"servers":{"srv0":{"listen":[":9443"],"routes":[
  {"match":[{"host":["static.example.test"]}],"handle":[{"handler":"subroute","routes":[
     {"handle":[{"handler":"vars","root":"/srv/static"},{"handler":"file_server","hide":["/etc/caddy/Caddyfile"]}]}]}],"terminal":true},
  {"match":[{"host":["blog.example.test"]}],"handle":[{"handler":"subroute","routes":[
     {"handle":[{"handler":"reverse_proxy","upstreams":[{"dial":"127.0.0.1:5000"}]}]}]}],"terminal":true}
]}}}}}`
	sites := parseCaddyConfig(body, "host", "", "caddy admin API")
	static, blog := siteByDomain(sites, "static.example.test"), siteByDomain(sites, "blog.example.test")
	if static == nil || blog == nil {
		t.Fatalf("missing sites: %+v", sites)
	}
	if len(static.Upstreams) != 0 || static.Root != "/srv/static" {
		t.Errorf("static site must have root and no upstream, got %+v", static)
	}
	if len(blog.Upstreams) != 1 || blog.Upstreams[0] != "127.0.0.1:5000" || blog.Root != "" {
		t.Errorf("blog site must only have its own upstream, got %+v", blog)
	}
	if !static.TLS || !blog.TLS {
		t.Error("listening on https_port 9443 must count as TLS")
	}
}

func TestParseCaddyfileFallback(t *testing.T) {
	body := `{
    debug
}

example.com, www.example.com {
    reverse_proxy 127.0.0.1:3000
}

(frontend) {
    reverse_proxy frontend:80
}

api.example.com {
    reverse_proxy api:8080
}
`
	sites := parseCaddyfile(body, "host", "")
	if len(sites) != 3 {
		t.Fatalf("expected 3 sites, got %d: %+v", len(sites), sites)
	}
	for _, want := range []string{"example.com", "www.example.com", "api.example.com"} {
		if siteByDomain(sites, want) == nil {
			t.Errorf("%s not found: %+v", want, sites)
		}
	}
	if siteByDomain(sites, "frontend") != nil {
		t.Errorf("the snippet name must be skipped: %+v", sites)
	}
}
