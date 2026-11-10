package store

import (
	"testing"
)

func TestReplaceHostContainersReplacesAndCascades(t *testing.T) {
	db := fleetSearchDB(t)
	h1 := fleetSearchHost(t, db, "alpha", "")

	if err := db.ReplaceHostContainers(h1, []HostContainer{
		{Name: "web", Image: "nginx:1.25", State: "running"},
		{Name: "db", Image: "postgres:13", State: "running"},
	}); err != nil {
		t.Fatalf("replace containers: %v", err)
	}
	if err := db.ReplaceHostContainers(h1, []HostContainer{
		{Name: "db2", Image: "postgres:13", State: "exited"},
	}); err != nil {
		t.Fatalf("replace containers: %v", err)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM host_containers WHERE host_id = ?`, h1).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected the rows to be replaced (1 left), got %d", n)
	}

	if err := db.DeleteHost(h1); err != nil {
		t.Fatalf("delete host: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM host_containers WHERE host_id = ?`, h1).Scan(&n); err != nil {
		t.Fatalf("count after delete: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected the rows to be deleted with the host, got %d", n)
	}
}

func TestSearchFleetHostContainerImages(t *testing.T) {
	db := fleetSearchDB(t)
	h1 := fleetSearchHost(t, db, "alpha", "prod")
	h2 := fleetSearchHost(t, db, "beta", "prod")
	if err := db.ReplaceHostContainers(h1, []HostContainer{
		{Name: "db-main", Image: "postgres:13", State: "running"},
		{Name: "db-replica", Image: "postgres:13", State: "exited"},
	}); err != nil {
		t.Fatalf("replace containers: %v", err)
	}
	if err := db.ReplaceHostContainers(h2, []HostContainer{
		{Name: "pg", Image: "postgres:13", State: "running"},
	}); err != nil {
		t.Fatalf("replace containers: %v", err)
	}

	res, err := db.SearchFleet("postgres", FleetSearchContainer, 200)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Groups) != 1 {
		t.Fatalf("expected 1 group, got %+v", res.Groups)
	}
	g := res.Groups[0]
	if g.Name != "postgres:13" || g.HostCount != 2 {
		t.Fatalf("unexpected group: %+v", g)
	}
	if len(g.Hosts[0].Containers) != 2 {
		t.Fatalf("expected 2 containers on the first host, got %+v", g.Hosts[0].Containers)
	}
	if g.Hosts[0].Containers[0].Name != "db-main" || g.Hosts[0].Containers[0].State != "running" {
		t.Fatalf("unexpected containers: %+v", g.Hosts[0].Containers)
	}
	if g.Hosts[0].DataAge == nil {
		t.Error("expected a data age for the host")
	}
	if res.HostsWithoutDocker != 0 {
		t.Errorf("expected every host to have container data, got %d without", res.HostsWithoutDocker)
	}
}

func TestSearchFleetHostContainerWildcardsAreLiteral(t *testing.T) {
	db := fleetSearchDB(t)
	h1 := fleetSearchHost(t, db, "alpha", "")
	if err := db.ReplaceHostContainers(h1, []HostContainer{
		{Name: "odd", Image: "pg%1_x", State: "running"},
	}); err != nil {
		t.Fatalf("replace containers: %v", err)
	}

	res, err := db.SearchFleet("pg%", FleetSearchContainer, 200)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Groups) != 1 || res.Groups[0].Name != "pg%1_x" {
		t.Fatalf("expected the literal percent match, got %+v", res.Groups)
	}
	res, err = db.SearchFleet("pg_x", FleetSearchContainer, 200)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Groups) != 0 {
		t.Fatalf("expected no match for an underscore wildcard, got %+v", res.Groups)
	}
	res, err = db.SearchFleet("%", FleetSearchContainer, 200)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Groups) != 1 || res.Groups[0].Name != "pg%1_x" {
		t.Fatalf("expected the literal percent match, got %+v", res.Groups)
	}
}
