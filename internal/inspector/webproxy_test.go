package inspector

import (
	"testing"

	"pantau/internal/store"
)

func TestDedupeWebSitesMergesTLSAndPlain(t *testing.T) {
	site := func(tls bool, listen, src, root string, ups ...string) store.WebSite {
		return store.WebSite{Domain: "a.example.com", Proxy: "nginx", Where: "host", TLS: tls, Listen: []string{listen}, Upstreams: ups, Root: root, Source: src}
	}
	// redirect-only :80 block + https block, same domain: one row
	got := dedupeWebSites([]store.WebSite{site(false, "80", "a:1", ""), site(true, "443 ssl", "a:9", "", "app:3000")})
	if len(got) != 1 || !got[0].TLS || got[0].Source != "a:9" || len(got[0].Upstreams) != 1 || len(got[0].Listen) != 2 {
		t.Fatalf("expected one merged TLS row, got %+v", got)
	}
	// plain and TLS serving different upstreams: config differs, both stay
	got = dedupeWebSites([]store.WebSite{site(false, "80", "a:1", "", "old:80"), site(true, "443 ssl", "a:9", "", "app:3000")})
	if len(got) != 2 {
		t.Fatalf("different config must stay separate, got %+v", got)
	}
}

func TestSplitWebSections(t *testing.T) {
	out := `noise before the first marker
__PANTAU_SECTION__ nginx host
METHOD nginx -T
server { }
__PANTAU_SECTION__ nginx container web
METHOD nginx -T
server { }
__PANTAU_SECTION__ caddy host
METHOD caddy admin API
{}
`
	sections := splitWebSections(out)
	if len(sections) != 3 {
		t.Fatalf("expected 3 sections, got %d: %+v", len(sections), sections)
	}
	if sections[0].Kind != "nginx" || sections[0].Where != "host" || sections[0].Container != "" {
		t.Errorf("unexpected first section: %+v", sections[0])
	}
	if sections[1].Kind != "nginx" || sections[1].Where != "container" || sections[1].Container != "web" {
		t.Errorf("unexpected second section: %+v", sections[1])
	}
	if sections[2].Kind != "caddy" || sections[2].Where != "host" {
		t.Errorf("unexpected third section: %+v", sections[2])
	}
	if sections[0].Body == "" || sections[1].Body == "" {
		t.Errorf("expected the section bodies to be filled: %+v", sections)
	}
}

func TestSplitWebSectionsIgnoresLeadingNoise(t *testing.T) {
	sections := splitWebSections("just noise\n")
	if len(sections) != 0 {
		t.Fatalf("expected no sections, got %+v", sections)
	}
}

func TestWebSectionMethod(t *testing.T) {
	method, errText := webSectionMethod("METHOD sudo nginx -T\nsome output\n")
	if method != "sudo nginx -T" {
		t.Errorf("unexpected method: %q", method)
	}
	if errText != "" {
		t.Errorf("unexpected error: %q", errText)
	}
	_, errText = webSectionMethod("METHOD config files\nERROR no readable nginx configuration found\n")
	if errText != "no readable nginx configuration found" {
		t.Errorf("unexpected error: %q", errText)
	}
}

func TestParseWebScanCombinesSources(t *testing.T) {
	out := `__PANTAU_SECTION__ nginxversion host
nginx version: nginx/1.24.0
__PANTAU_SECTION__ nginx host
METHOD nginx -T
# configuration file /etc/nginx/sites-enabled/api.conf:
http {
    server {
        listen 443 ssl;
        server_name api.example.com;
        location / { proxy_pass http://127.0.0.1:3000; }
    }
}
__PANTAU_SECTION__ docker host
CONTAINER_NAME proxy
CONTAINER_IMAGE nginx
CONTAINER_LABELS {}
ENV VIRTUAL_HOST=web.example.com
ENV VIRTUAL_PORT=8080
__PANTAU_SECTION__ caddy container caddybox
METHOD caddy admin API
{"apps":{"http":{"servers":{"s":{"listen":[":443"],"routes":[{"match":[{"host":["caddy.example.com"]}],"handle":[{"handler":"reverse_proxy","upstreams":[{"dial":"10.0.0.9:80"}]}]}]}}}}}
`
	scan := parseWebScan(1, out)
	if len(scan.Sites) != 3 {
		t.Fatalf("expected 3 sites, got %d: %+v", len(scan.Sites), scan.Sites)
	}
	if siteByDomain(scan.Sites, "api.example.com") == nil {
		t.Errorf("the nginx site is missing: %+v", scan.Sites)
	}
	if siteByDomain(scan.Sites, "web.example.com") == nil {
		t.Errorf("the nginx-proxy site is missing: %+v", scan.Sites)
	}
	if siteByDomain(scan.Sites, "caddy.example.com") == nil {
		t.Errorf("the caddy site is missing: %+v", scan.Sites)
	}
	found := false
	for _, p := range scan.Proxies {
		if p.Kind == "nginx" && p.Where == "host" && p.Version == "1.24.0" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected the host nginx proxy with its version, got %+v", scan.Proxies)
	}
}
