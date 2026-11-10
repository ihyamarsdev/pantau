package store

import (
	"path/filepath"
	"testing"
	"time"
)

func fleetSearchDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func fleetSearchHost(t *testing.T, db *DB, name, group string) int64 {
	t.Helper()
	id, err := db.CreateHost(&Host{Name: name, Host: name + ".example.net", Port: 22, User: "root", GroupName: group})
	if err != nil {
		t.Fatalf("create host %s: %v", name, err)
	}
	return id
}

func TestSearchFleetPackagesGroupedByItem(t *testing.T) {
	db := fleetSearchDB(t)
	h1 := fleetSearchHost(t, db, "alpha", "prod")
	h2 := fleetSearchHost(t, db, "beta", "prod")
	h3 := fleetSearchHost(t, db, "gamma", "dev")

	old := time.Now().Add(-72 * time.Hour)
	if err := db.ReplaceHostPackages(h1, []*HostPackage{
		{HostID: h1, Name: "openssl", Version: "1.1.1", LastScannedAt: old},
	}); err != nil {
		t.Fatalf("replace packages: %v", err)
	}
	if err := db.ReplaceHostPackages(h2, []*HostPackage{
		{HostID: h2, Name: "openssl", Version: "1.1.1", LastScannedAt: old},
	}); err != nil {
		t.Fatalf("replace packages: %v", err)
	}
	if err := db.ReplaceHostPackages(h3, []*HostPackage{
		{HostID: h3, Name: "openssl", Version: "3.0.2", LastScannedAt: time.Now()},
		{HostID: h3, Name: "curl", Version: "8.0"},
	}); err != nil {
		t.Fatalf("replace packages: %v", err)
	}
	res, err := db.SearchFleet("openssl", FleetSearchAll, 200)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Groups) != 2 {
		t.Fatalf("expected 2 groups (one per version), got %d: %+v", len(res.Groups), res.Groups)
	}
	g := res.Groups[0]
	if g.Name != "openssl" || g.Version != "1.1.1" || g.HostCount != 2 {
		t.Fatalf("unexpected group: %+v", g)
	}
	if g.Hosts[0].Name != "alpha" || g.Hosts[1].Name != "beta" {
		t.Fatalf("unexpected hosts: %+v", g.Hosts)
	}
	if g.Hosts[0].DataAge == nil || g.Hosts[0].DataAge.IsZero() {
		t.Error("expected data age for host")
	}
	if g.Hosts[0].Group != "prod" {
		t.Errorf("expected host group prod, got %q", g.Hosts[0].Group)
	}
	if res.TotalHosts != 3 {
		t.Errorf("expected 3 hosts, got %d", res.TotalHosts)
	}
	if res.HostsWithoutPackages != 0 {
		t.Errorf("expected every host to have package data, got %d without", res.HostsWithoutPackages)
	}
}

func TestSearchFleetCaseInsensitive(t *testing.T) {
	db := fleetSearchDB(t)
	h := fleetSearchHost(t, db, "alpha", "")
	if err := db.ReplaceHostPackages(h, []*HostPackage{{HostID: h, Name: "OpenSSL", Version: "1.1.1"}}); err != nil {
		t.Fatalf("replace packages: %v", err)
	}
	for _, q := range []string{"openssl", "OPENSSL", "OpenSsL"} {
		res, err := db.SearchFleet(q, FleetSearchPackage, 200)
		if err != nil {
			t.Fatalf("search %q: %v", q, err)
		}
		if len(res.Groups) != 1 {
			t.Fatalf("query %q: expected 1 group, got %d", q, len(res.Groups))
		}
	}
}

func TestSearchFleetWildcardsAreLiteral(t *testing.T) {
	db := fleetSearchDB(t)
	h := fleetSearchHost(t, db, "alpha", "")
	if err := db.ReplaceHostPackages(h, []*HostPackage{{HostID: h, Name: "a_b", Version: "1"}}); err != nil {
		t.Fatalf("replace packages: %v", err)
	}
	if err := db.ReplaceHostPackages(h, []*HostPackage{
		{HostID: h, Name: "a_b", Version: "1"},
		{HostID: h, Name: "axb", Version: "1"},
	}); err != nil {
		t.Fatalf("replace packages: %v", err)
	}

	// "_" must match the literal underscore only.
	res, err := db.SearchFleet("a_b", FleetSearchPackage, 200)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Groups) != 1 || res.Groups[0].Name != "a_b" {
		t.Fatalf("expected only a_b, got %+v", res.Groups)
	}
	// "%" must match a literal percent sign, so nothing matches.
	res, err = db.SearchFleet("%", FleetSearchPackage, 200)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Groups) != 0 {
		t.Fatalf("expected no match for literal %% sign, got %+v", res.Groups)
	}
}

func TestSearchFleetLimitAndTruncated(t *testing.T) {
	db := fleetSearchDB(t)
	h := fleetSearchHost(t, db, "alpha", "")
	pkgs := []*HostPackage{
		{HostID: h, Name: "pkg-a", Version: "1"},
		{HostID: h, Name: "pkg-b", Version: "1"},
		{HostID: h, Name: "pkg-c", Version: "1"},
	}
	if err := db.ReplaceHostPackages(h, pkgs); err != nil {
		t.Fatalf("replace packages: %v", err)
	}
	res, err := db.SearchFleet("pkg", FleetSearchPackage, 2)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(res.Groups))
	}
	if !res.Truncated {
		t.Error("expected truncated to be true")
	}
}

func TestSearchFleetContainers(t *testing.T) {
	db := fleetSearchDB(t)
	h1 := fleetSearchHost(t, db, "alpha", "")
	h2 := fleetSearchHost(t, db, "beta", "")
	if err := db.UpsertActualItem(&ActualItem{HostID: h1, Kind: "container", Target: "nginx", Current: "running", Status: "ok"}); err != nil {
		t.Fatalf("upsert actual item: %v", err)
	}
	if err := db.UpsertActualItem(&ActualItem{HostID: h2, Kind: "container", Target: "nginx", Current: "running", Status: "ok"}); err != nil {
		t.Fatalf("upsert actual item: %v", err)
	}
	if err := db.SaveDockerStack(h1, "web", `{"name":"web","services":["nginx","php"],"status":"running"}`); err != nil {
		t.Fatalf("save stack: %v", err)
	}

	res, err := db.SearchFleet("nginx", FleetSearchContainer, 200)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Groups) != 1 {
		t.Fatalf("expected 1 group, got %+v", res.Groups)
	}
	if res.Groups[0].HostCount != 2 {
		t.Fatalf("expected nginx on 2 hosts, got %d", res.Groups[0].HostCount)
	}
	res, err = db.SearchFleet("php", FleetSearchContainer, 200)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Groups) != 1 || res.Groups[0].Name != "php" || res.Groups[0].HostCount != 1 {
		t.Fatalf("expected the compose service php on 1 host, got %+v", res.Groups)
	}
	// A package search must not return container groups.
	res, err = db.SearchFleet("nginx", FleetSearchPackage, 200)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Groups) != 0 {
		t.Fatalf("expected no package groups, got %+v", res.Groups)
	}
}

func TestSearchFleetCoverageCounts(t *testing.T) {
	db := fleetSearchDB(t)
	h1 := fleetSearchHost(t, db, "alpha", "")
	fleetSearchHost(t, db, "beta", "")
	if err := db.ReplaceHostPackages(h1, []*HostPackage{{HostID: h1, Name: "openssl", Version: "1"}}); err != nil {
		t.Fatalf("replace packages: %v", err)
	}
	res, err := db.SearchFleet("openssl", FleetSearchAll, 200)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if res.HostsWithoutPackages != 1 {
		t.Errorf("expected 1 host without package data, got %d", res.HostsWithoutPackages)
	}
	if res.HostsWithoutDocker != 2 {
		t.Errorf("expected 2 hosts without docker data, got %d", res.HostsWithoutDocker)
	}
}
