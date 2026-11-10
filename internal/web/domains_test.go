package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"strconv"

	"pantau/internal/inspector"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

const domainsScanOutput = `__PANTAU_SECTION__ nginxversion host
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
`

// domainsServer returns a server with one host and a mock runner that answers
// the domain scan script with the canned output.
func domainsServer(t *testing.T, output string) (*Server, *http.Cookie, int64) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	hostID, err := db.CreateHost(&store.Host{Name: "alpha", Host: "10.0.0.1", Port: 22, User: "root", GroupName: "prod"})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	runner := sshrunner.NewMockRunner()
	runner.DefaultExec = func(cmd string) (string, string, int, error) {
		if strings.Contains(cmd, "PANTAU_SECTION") {
			return output, "", 0, nil
		}
		return "", "", 0, nil
	}
	ins := inspector.New(db, func(host string, port int, user, key string) (sshrunner.Runner, error) {
		return runner, nil
	}, nil)
	srv := NewServer(db, ins, nil)
	token := "domains_test_token"
	srv.sessions.Store(token, time.Now().Add(time.Hour))
	return srv, &http.Cookie{Name: "pantau_session", Value: token}, hostID
}

func TestDomainsRequiresAuth(t *testing.T) {
	srv, _, _ := domainsServer(t, domainsScanOutput)
	req := httptest.NewRequest("GET", "/api/domains", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", w.Code)
	}
}

func TestDomainsUnknownHost(t *testing.T) {
	srv, cookie, _ := domainsServer(t, domainsScanOutput)
	req := httptest.NewRequest("GET", "/api/domains?host_id=9999", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown host, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDomainsNeverScannedHost(t *testing.T) {
	srv, cookie, hostID := domainsServer(t, domainsScanOutput)
	req := httptest.NewRequest("GET", "/api/domains?host_id="+itoa(hostID), nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		HostID    int64            `json:"host_id"`
		ScannedAt string           `json:"scanned_at"`
		Proxies   []store.WebProxy `json:"proxies"`
		Sites     []store.WebSite  `json:"sites"`
		Warnings  []string         `json:"warnings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.HostID != hostID || res.ScannedAt != "" {
		t.Fatalf("unexpected response: %+v", res)
	}
	if res.Proxies == nil || res.Sites == nil || res.Warnings == nil {
		t.Fatalf("expected empty arrays, got %+v", res)
	}
	if len(res.Proxies) != 0 || len(res.Sites) != 0 {
		t.Fatalf("expected empty arrays, got %+v", res)
	}
}

func TestDomainsScanStoresAndReturns(t *testing.T) {
	srv, cookie, hostID := domainsServer(t, domainsScanOutput)
	body := strings.NewReader(`{"host_id":` + itoa(hostID) + `}`)
	req := httptest.NewRequest("POST", "/api/domains/scan", body)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		HostID    int64            `json:"host_id"`
		ScannedAt string           `json:"scanned_at"`
		Proxies   []store.WebProxy `json:"proxies"`
		Sites     []store.WebSite  `json:"sites"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.ScannedAt == "" {
		t.Error("expected a scan timestamp")
	}
	if len(res.Sites) != 1 || res.Sites[0].Domain != "api.example.com" {
		t.Fatalf("unexpected sites: %+v", res.Sites)
	}
	if len(res.Proxies) != 1 || res.Proxies[0].Kind != "nginx" || res.Proxies[0].Version != "1.24.0" {
		t.Fatalf("unexpected proxies: %+v", res.Proxies)
	}

	// The stored scan is served by the GET endpoint.
	req = httptest.NewRequest("GET", "/api/domains?host_id="+itoa(hostID), nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var stored struct {
		ScannedAt string          `json:"scanned_at"`
		Sites     []store.WebSite `json:"sites"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &stored); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if stored.ScannedAt == "" || len(stored.Sites) != 1 {
		t.Fatalf("the scan was not stored: %+v", stored)
	}
}

func TestDomainsScanBadRequest(t *testing.T) {
	srv, cookie, _ := domainsServer(t, domainsScanOutput)
	for _, body := range []string{`not json`, `{}`, `{"host_id":0}`} {
		req := httptest.NewRequest("POST", "/api/domains/scan", strings.NewReader(body))
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for body %q, got %d", body, w.Code)
		}
	}
}

func TestDomainsScanUnknownHost(t *testing.T) {
	srv, cookie, _ := domainsServer(t, domainsScanOutput)
	req := httptest.NewRequest("POST", "/api/domains/scan", strings.NewReader(`{"host_id":9999}`))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown host, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDomainsFleetView(t *testing.T) {
	srv, cookie, hostID := domainsServer(t, domainsScanOutput)
	req := httptest.NewRequest("POST", "/api/domains/scan", strings.NewReader(`{"host_id":`+itoa(hostID)+`}`))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("scan failed: %d %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("GET", "/api/domains", nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Hosts []struct {
			HostID     int64  `json:"host_id"`
			HostName   string `json:"host_name"`
			Group      string `json:"group"`
			ScannedAt  string `json:"scanned_at"`
			SiteCount  int    `json:"site_count"`
			ProxyCount int    `json:"proxy_count"`
		} `json:"hosts"`
		Sites []struct {
			Domain  string `json:"domain"`
			HostID  int64  `json:"host_id"`
			HostNam string `json:"host_name"`
		} `json:"sites"`
		UnscannedHosts int `json:"unscanned_hosts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Hosts) != 1 || res.Hosts[0].HostID != hostID || res.Hosts[0].HostName != "alpha" {
		t.Fatalf("unexpected hosts: %+v", res.Hosts)
	}
	if res.Hosts[0].SiteCount != 1 || res.Hosts[0].ProxyCount != 1 || res.Hosts[0].ScannedAt == "" {
		t.Fatalf("unexpected host summary: %+v", res.Hosts[0])
	}
	if len(res.Sites) != 1 || res.Sites[0].Domain != "api.example.com" {
		t.Fatalf("unexpected sites: %+v", res.Sites)
	}
	if res.UnscannedHosts != 0 {
		t.Errorf("unexpected unscanned hosts: %d", res.UnscannedHosts)
	}
}

func TestFleetSearchKindDomain(t *testing.T) {
	srv, cookie, hostID := domainsServer(t, domainsScanOutput)
	req := httptest.NewRequest("POST", "/api/domains/scan", strings.NewReader(`{"host_id":`+itoa(hostID)+`}`))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("scan failed: %d %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("GET", "/api/search?q=api.example.com&kind=domain", nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Groups []struct {
			Kind      string `json:"kind"`
			Name      string `json:"name"`
			HostCount int    `json:"host_count"`
			Hosts     []struct {
				HostID int64  `json:"host_id"`
				Name   string `json:"name"`
				Detail string `json:"detail"`
			} `json:"hosts"`
		} `json:"groups"`
		HostsWithoutDomains int `json:"hosts_without_domains"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Groups) != 1 {
		t.Fatalf("expected 1 group, got %d: %+v", len(res.Groups), res.Groups)
	}
	g := res.Groups[0]
	if g.Kind != "domain" || g.Name != "api.example.com" || g.HostCount != 1 {
		t.Fatalf("unexpected group: %+v", g)
	}
	if len(g.Hosts) != 1 || g.Hosts[0].Name != "alpha" {
		t.Fatalf("unexpected hosts: %+v", g.Hosts)
	}
	if g.Hosts[0].Detail != "nginx · host → http://127.0.0.1:3000" {
		t.Errorf("unexpected detail: %q", g.Hosts[0].Detail)
	}
	if res.HostsWithoutDomains != 0 {
		t.Errorf("unexpected hosts without domains: %d", res.HostsWithoutDomains)
	}
}

func itoa(i int64) string {
	return strconv.FormatInt(i, 10)
}
