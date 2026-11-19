package inspector

import (
	"testing"

	"pantau/internal/store"
)

func TestNginxHardeningServerTokens(t *testing.T) {
	dump := `# configuration file /etc/nginx/nginx.conf:
http {
  server_tokens on;
  server { listen 80; server_name a.example.com; }
}
`
	findings := nginxHardeningChecks(dump)
	found := false
	for _, f := range findings {
		if f.RuleID == "nginx_server_tokens_on" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected server_tokens_on finding, got %+v", findings)
	}
}

func TestNginxHardeningPHPUpload(t *testing.T) {
	dump := `# configuration file /etc/nginx/sites-enabled/site.conf:
server {
  listen 80;
  server_name app.example.com;
  root /var/www/app;
  location /uploads/ {
    alias /var/www/app/uploads;
    location ~ \.php$ {
      fastcgi_pass unix:/run/php/php8.1-fpm.sock;
    }
  }
  fastcgi_split_path_info ^(.+\.php)(/.+)$;
}
`
	findings := nginxHardeningChecks(dump)
	byRule := map[string]bool{}
	for _, f := range findings {
		byRule[f.RuleID] = true
	}
	for _, want := range []string{"nginx_php_in_writable_dir", "nginx_alias_traversal", "nginx_fastcgi_split_path_info"} {
		if !byRule[want] {
			t.Errorf("missing %s finding in %+v", want, findings)
		}
	}
}

func TestNginxHardeningMissingBoundaries(t *testing.T) {
	dump := `# configuration file /etc/nginx/nginx.conf:
server {
  listen 80;
  server_name bare.example.com;
  root /var/www/bare;
}
`
	findings := nginxHardeningChecks(dump)
	byRule := map[string]bool{}
	for _, f := range findings {
		byRule[f.RuleID] = true
	}
	if !byRule["nginx_no_body_limit"] || !byRule["nginx_no_rate_limit"] {
		t.Errorf("expected missing-boundary findings, got %+v", findings)
	}
}

func TestNginxHardeningBoundariesPresent(t *testing.T) {
	dump := `# configuration file /etc/nginx/nginx.conf:
server {
  listen 80;
  server_name good.example.com;
  root /var/www/good;
  client_max_body_size 2m;
  limit_req zone=reqlimit burst=20;
}
`
	findings := nginxHardeningChecks(dump)
	for _, f := range findings {
		if f.RuleID == "nginx_no_body_limit" || f.RuleID == "nginx_no_rate_limit" {
			t.Errorf("unexpected missing-boundary finding when limits are set: %+v", f)
		}
	}
}

func TestNginxDumpHardeningFindingsCorrelates(t *testing.T) {
	dump := `# configuration file /etc/nginx/nginx.conf:
server { listen 80; server_name a.example.com; root /var/www/a; server_tokens on; }
`
	host := &store.Host{}
	domains := []store.WebSite{{Domain: "a.example.com", Listen: []string{"80"}}}
	cf := map[string]bool{"a.example.com": true}
	findings := NginxDumpHardeningFindings(host, dump, domains, cf)
	if len(findings) == 0 {
		t.Fatal("expected at least the server_tokens finding")
	}
	found := false
	for _, f := range findings {
		if f.Source == "nginx" && f.Severity == store.ExposureLow {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a low-severity nginx finding, got %+v", findings)
	}
}
