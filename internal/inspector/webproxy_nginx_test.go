package inspector

import (
	"testing"

	"pantau/internal/store"
)

func siteByDomain(sites []store.WebSite, domain string) *store.WebSite {
	for i := range sites {
		if sites[i].Domain == domain {
			return &sites[i]
		}
	}
	return nil
}

// A :80 block with a root (ACME) that redirects to https is the same site as the TLS block.
func TestParseWebScanMergesRedirectWithRoot(t *testing.T) {
	out := `__PANTAU_SECTION__ nginx host
METHOD nginx -T
# configuration file /etc/nginx/conf.d/dev.conf:
http {
    server {
        listen 80;
        server_name dev.example.com;
        root /usr/share/nginx/html;
        location /.well-known/acme-challenge/ { allow all; }
        location / { return 301 https://$host$request_uri; }
    }
    server {
        listen 443 ssl;
        server_name dev.example.com;
        location / { proxy_pass https://127.0.0.1:8443; }
    }
}
`
	scan := parseWebScan(1, out)
	if len(scan.Sites) != 1 || !scan.Sites[0].TLS || len(scan.Sites[0].Upstreams) != 1 {
		t.Fatalf("expected one merged TLS site, got %+v", scan.Sites)
	}
}

func TestParseNginxConfigMarkersAndServerNames(t *testing.T) {
	out := `# configuration file /etc/nginx/nginx.conf:
user www-data;
http {
    # a comment with a ; and a {
    server {
        listen 80;
        listen 443 ssl;
        server_name api.example.com www.example.com;
        root /var/www/html;
        location / {
            proxy_pass http://127.0.0.1:3000;
        }
    }
    server {
        listen 80 default_server;
        server_name _;
        return 301 https://example.com;
    }
}
`
	sites := parseNginxConfig(out, "host", "")
	if len(sites) != 2 {
		t.Fatalf("expected 2 sites, got %d: %+v", len(sites), sites)
	}
	api := siteByDomain(sites, "api.example.com")
	if api == nil {
		t.Fatalf("api.example.com not found: %+v", sites)
	}
	if !api.TLS {
		t.Error("expected TLS on the 443 ssl listener")
	}
	if len(api.Listen) != 2 || api.Listen[0] != "80" {
		t.Errorf("unexpected listen list: %+v", api.Listen)
	}
	if api.Root != "/var/www/html" {
		t.Errorf("unexpected root: %q", api.Root)
	}
	if len(api.Upstreams) != 1 || api.Upstreams[0] != "http://127.0.0.1:3000" {
		t.Errorf("unexpected upstreams: %+v", api.Upstreams)
	}
	if api.Source != "/etc/nginx/nginx.conf:4" {
		t.Errorf("unexpected source: %q", api.Source)
	}
	if siteByDomain(sites, "_") != nil {
		t.Error("the default server name _ must be skipped")
	}
}

func TestParseNginxConfigUpstreamResolution(t *testing.T) {
	out := `# configuration file /etc/nginx/sites-enabled/api.conf:
http {
    upstream backend {
        server 10.0.0.5:8080;
        server 10.0.0.6:8080;
    }
    server {
        listen 443 ssl;
        server_name api.example.com;
        location /api {
            proxy_pass http://backend;
        }
    }
}
`
	sites := parseNginxConfig(out, "host", "")
	if len(sites) != 1 {
		t.Fatalf("expected 1 site, got %d: %+v", len(sites), sites)
	}
	s := sites[0]
	if len(s.Upstreams) != 2 {
		t.Fatalf("expected the upstream block to be resolved, got %+v", s.Upstreams)
	}
	if s.Upstreams[0] != "http://10.0.0.5:8080" || s.Upstreams[1] != "http://10.0.0.6:8080" {
		t.Errorf("unexpected upstreams: %+v", s.Upstreams)
	}
	if s.Source != "/etc/nginx/sites-enabled/api.conf:6" {
		t.Errorf("unexpected source: %q", s.Source)
	}
}

func TestParseNginxConfigQuotesAndComments(t *testing.T) {
	out := `# configuration file /etc/nginx/conf.d/odd.conf:
server {
    # comment ; { }
    server_name "quoted.example.com";
    location / {
        proxy_pass http://backend; # trailing comment {
    }
    location ~ "^/a{b}$" {
        proxy_pass http://other;
    }
}
`
	sites := parseNginxConfig(out, "host", "")
	if len(sites) != 1 {
		t.Fatalf("expected 1 site, got %d: %+v", len(sites), sites)
	}
	if sites[0].Domain != "quoted.example.com" {
		t.Errorf("unexpected domain: %q", sites[0].Domain)
	}
	if len(sites[0].Upstreams) != 2 {
		t.Errorf("expected both proxy_pass targets, got %+v", sites[0].Upstreams)
	}
}

func TestParseNginxConfigSkipsStreamServers(t *testing.T) {
	out := `# configuration file /etc/nginx/nginx.conf:
stream {
    server {
        listen 5432;
        proxy_pass 127.0.0.1:5432;
    }
}
http {
    server {
        listen 80;
        server_name web.example.com;
    }
}
`
	sites := parseNginxConfig(out, "host", "")
	if len(sites) != 1 || sites[0].Domain != "web.example.com" {
		t.Fatalf("the stream server must be skipped, got %+v", sites)
	}
}

func TestParseNginxConfigWildcardAndRegex(t *testing.T) {
	out := `# configuration file /etc/nginx/conf.d/wild.conf:
server {
    listen 80;
    server_name *.example.com .other.com;
}
server {
    listen 80;
    server_name ~^www\..+$
}
`
	sites := parseNginxConfig(out, "host", "")
	wild := siteByDomain(sites, "*.example.com")
	if wild == nil || !wild.Wildcard {
		t.Fatalf("expected a wildcard site, got %+v", sites)
	}
	if siteByDomain(sites, ".other.com") == nil {
		t.Errorf("expected .other.com, got %+v", sites)
	}
	re := siteByDomain(sites, `~^www\..+$`)
	if re == nil || !re.Regex {
		t.Fatalf("expected a regex site, got %+v", sites)
	}
}

func TestParseNginxConfigDefaultServer(t *testing.T) {
	out := `# configuration file /etc/nginx/conf.d/def.conf:
server {
    listen 80 default_server;
    server_name example.com;
}
`
	sites := parseNginxConfig(out, "host", "")
	if len(sites) != 1 || !sites[0].DefaultServer {
		t.Fatalf("expected the default server flag, got %+v", sites)
	}
}
