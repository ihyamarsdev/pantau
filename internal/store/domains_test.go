package store

import (
	"testing"
	"time"
)

func domainsDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(t.TempDir() + "/domains.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func domainsHost(t *testing.T, db *DB, name string) int64 {
	t.Helper()
	id, err := db.CreateHost(&Host{Name: name, Host: name + ".example.net", Port: 22, User: "root", GroupName: "prod"})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	return id
}

func TestReplaceWebScanStoresAndReplaces(t *testing.T) {
	db := domainsDB(t)
	hostID := domainsHost(t, db, "alpha")

	now := time.Now().UTC().Truncate(time.Second)
	if err := db.ReplaceWebScan(hostID, &WebScan{
		HostID:    hostID,
		ScannedAt: now,
		Proxies:   []WebProxy{{Kind: "nginx", Where: "host", Version: "1.24.0", Method: "nginx -T"}},
		Sites: []WebSite{
			{Domain: "api.example.com", Proxy: "nginx", Where: "host", TLS: true, Listen: []string{"443 ssl"}, Upstreams: []string{"http://127.0.0.1:3000"}},
			{Domain: "www.example.com", Proxy: "nginx", Where: "host"},
		},
		Warnings: []string{},
	}); err != nil {
		t.Fatalf("replace web scan: %v", err)
	}

	scan, ok, err := db.GetWebScan(hostID)
	if err != nil || !ok {
		t.Fatalf("get web scan: %v ok=%v", err, ok)
	}
	if len(scan.Proxies) != 1 || scan.Proxies[0].Version != "1.24.0" {
		t.Fatalf("unexpected proxies: %+v", scan.Proxies)
	}
	sites, err := db.GetHostDomains(hostID)
	if err != nil {
		t.Fatalf("get host domains: %v", err)
	}
	if len(sites) != 2 {
		t.Fatalf("expected 2 sites, got %d: %+v", len(sites), sites)
	}
	if sites[0].Domain != "api.example.com" || sites[1].Domain != "www.example.com" {
		t.Fatalf("expected the sites sorted by domain, got %+v", sites)
	}
	if !sites[0].TLS || len(sites[0].Upstreams) != 1 {
		t.Fatalf("unexpected site: %+v", sites[0])
	}

	// A second scan replaces the sites of the host.
	if err := db.ReplaceWebScan(hostID, &WebScan{
		HostID:    hostID,
		ScannedAt: now.Add(time.Hour),
		Sites:     []WebSite{{Domain: "new.example.com", Proxy: "caddy", Where: "host"}},
	}); err != nil {
		t.Fatalf("replace web scan: %v", err)
	}
	sites, err = db.GetHostDomains(hostID)
	if err != nil {
		t.Fatalf("get host domains: %v", err)
	}
	if len(sites) != 1 || sites[0].Domain != "new.example.com" {
		t.Fatalf("expected the old sites to be replaced, got %+v", sites)
	}
}

func TestGetWebScanUnknownHost(t *testing.T) {
	db := domainsDB(t)
	scan, ok, err := db.GetWebScan(42)
	if err != nil {
		t.Fatalf("get web scan: %v", err)
	}
	if ok || scan != nil {
		t.Fatalf("expected no scan, got %+v ok=%v", scan, ok)
	}
}

func TestListHostDomainSummariesAndCounts(t *testing.T) {
	db := domainsDB(t)
	scanned := domainsHost(t, db, "alpha")
	_ = domainsHost(t, db, "beta")

	if err := db.ReplaceWebScan(scanned, &WebScan{
		HostID:    scanned,
		ScannedAt: time.Now().UTC(),
		Proxies:   []WebProxy{{Kind: "nginx", Where: "host"}},
		Sites:     []WebSite{{Domain: "api.example.com", Proxy: "nginx", Where: "host"}},
	}); err != nil {
		t.Fatalf("replace web scan: %v", err)
	}

	summaries, err := db.ListHostDomainSummaries()
	if err != nil {
		t.Fatalf("list summaries: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries (scanned and unscanned), got %d: %+v", len(summaries), summaries)
	}
	if summaries[0].HostName != "alpha" || summaries[0].SiteCount != 1 || summaries[0].ScannedAt == "" {
		t.Fatalf("unexpected summary: %+v", summaries[0])
	}
	if summaries[1].HostName != "beta" || summaries[1].ScannedAt != "" || summaries[1].SiteCount != 0 {
		t.Fatalf("unexpected summary: %+v", summaries[1])
	}

	n, err := db.CountUnscannedHosts()
	if err != nil {
		t.Fatalf("count unscanned: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 unscanned host, got %d", n)
	}
	n, err = db.CountHostsWithoutDomains()
	if err != nil {
		t.Fatalf("count hosts without domains: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 host without domains, got %d", n)
	}
}

func TestListHostDomainsSortedByDomainThenHost(t *testing.T) {
	db := domainsDB(t)
	h1 := domainsHost(t, db, "alpha")
	h2 := domainsHost(t, db, "beta")
	for _, h := range []int64{h1, h2} {
		if err := db.ReplaceWebScan(h, &WebScan{
			HostID:    h,
			ScannedAt: time.Now().UTC(),
			Sites:     []WebSite{{Domain: "shared.example.com", Proxy: "nginx", Where: "host"}},
		}); err != nil {
			t.Fatalf("replace web scan: %v", err)
		}
	}
	sites, err := db.ListHostDomains()
	if err != nil {
		t.Fatalf("list host domains: %v", err)
	}
	if len(sites) != 2 {
		t.Fatalf("expected 2 sites, got %d: %+v", len(sites), sites)
	}
	if sites[0].HostName != "alpha" || sites[1].HostName != "beta" {
		t.Fatalf("expected the sites sorted by host name, got %+v", sites)
	}
	if sites[0].HostID != h1 || sites[0].Group != "prod" {
		t.Fatalf("unexpected host context: %+v", sites[0])
	}
}

func TestSearchFleetDomains(t *testing.T) {
	db := domainsDB(t)
	h1 := domainsHost(t, db, "alpha")
	h2 := domainsHost(t, db, "beta")
	if err := db.ReplaceWebScan(h1, &WebScan{
		HostID:    h1,
		ScannedAt: time.Now().UTC(),
		Sites:     []WebSite{{Domain: "api.example.com", Proxy: "nginx", Where: "host", Upstreams: []string{"http://127.0.0.1:3000"}}},
	}); err != nil {
		t.Fatalf("replace web scan: %v", err)
	}
	if err := db.ReplaceWebScan(h2, &WebScan{
		HostID:    h2,
		ScannedAt: time.Now().UTC(),
		Sites:     []WebSite{{Domain: "api.example.com", Proxy: "traefik", Where: "container", Container: "edge"}},
	}); err != nil {
		t.Fatalf("replace web scan: %v", err)
	}

	res, err := db.SearchFleet("api.example.com", FleetSearchDomain, 200)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Groups) != 1 {
		t.Fatalf("expected 1 group, got %d: %+v", len(res.Groups), res.Groups)
	}
	g := res.Groups[0]
	if g.Kind != FleetSearchDomain || g.Name != "api.example.com" || g.HostCount != 2 {
		t.Fatalf("unexpected group: %+v", g)
	}
	if len(g.Hosts) != 2 || g.Hosts[0].Name != "alpha" {
		t.Fatalf("unexpected hosts: %+v", g.Hosts)
	}
	if g.Hosts[0].Detail != "nginx · host → http://127.0.0.1:3000" {
		t.Errorf("unexpected detail: %q", g.Hosts[0].Detail)
	}
	if g.Hosts[1].Detail != "traefik · container:edge" {
		t.Errorf("unexpected detail: %q", g.Hosts[1].Detail)
	}
	if res.HostsWithoutDomains != 0 {
		t.Errorf("unexpected hosts without domains: %d", res.HostsWithoutDomains)
	}

	// The "all" search includes the domains too.
	res, err = db.SearchFleet("api.example.com", FleetSearchAll, 200)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	found := false
	for _, g := range res.Groups {
		if g.Kind == FleetSearchDomain {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the domain group in the all search, got %+v", res.Groups)
	}
}
