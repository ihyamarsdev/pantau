package store_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"pantau/internal/store"
)

func TestAgentMemory_ScopeDedupeAndLimits(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTestDB(t)
	defer cleanup()
	h1, _ := db.CreateHost(&store.Host{Name: "a", Host: "10.0.0.1", Port: 22, User: "root"})
	h2, _ := db.CreateHost(&store.Host{Name: "b", Host: "10.0.0.2", Port: 22, User: "root"})

	fleet, err := db.AddAgentMemory(nil, "Backups run at 02:00", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddAgentMemory(&h1, "nginx is behind Cloudflare", "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddAgentMemory(&h2, "db host", "agent"); err != nil {
		t.Fatal(err)
	}

	// Same text in the same scope is refreshed, not duplicated; in another scope it is a new note.
	again, _ := db.AddAgentMemory(nil, "backups RUN at 02:00", "agent")
	if again.ID != fleet.ID {
		t.Fatalf("duplicate stored as #%d, want #%d", again.ID, fleet.ID)
	}
	if _, err := db.AddAgentMemory(&h1, "Backups run at 02:00", "agent"); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		host int64
		q    string
		want int
	}{{0, "", 4}, {h1, "", 3}, {h2, "", 2}, {0, "cloudflare", 1}, {0, "100%", 0}} {
		got, _ := db.ListAgentMemory(c.host, c.q)
		if len(got) != c.want {
			t.Errorf("list host=%d q=%q: %d notes, want %d", c.host, c.q, len(got), c.want)
		}
	}

	if _, err := db.AddAgentMemory(nil, "  ", "agent"); !errors.Is(err, store.ErrAgentMemoryEmpty) {
		t.Errorf("empty: %v", err)
	}
	if _, err := db.AddAgentMemory(nil, strings.Repeat("x", store.AgentMemoryMaxLen+1), "agent"); !errors.Is(err, store.ErrAgentMemoryTooLong) {
		t.Errorf("too long: %v", err)
	}
	if err := db.UpdateAgentMemory(999, "x"); !errors.Is(err, store.ErrAgentMemoryNotFound) {
		t.Errorf("update missing: %v", err)
	}

	// Deleting a host removes its notes but keeps the fleet-wide ones.
	if err := db.DeleteHost(h1); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.ListAgentMemory(0, ""); len(got) != 2 {
		t.Errorf("after host delete: %d notes, want 2", len(got))
	}

	// The store refuses a note beyond the cap.
	for i := range store.AgentMemoryMax {
		if _, err := db.AddAgentMemory(nil, "note "+strings.Repeat("n", i%7)+string(rune('a'+i%26))+strings.Repeat("z", i), "agent"); err != nil && !errors.Is(err, store.ErrAgentMemoryFull) {
			t.Fatal(err)
		}
	}
	if _, err := db.AddAgentMemory(nil, "one more distinct note", "agent"); !errors.Is(err, store.ErrAgentMemoryFull) {
		t.Errorf("full: %v", err)
	}
}

func TestAgentMemory_SnapshotRoundTrip(t *testing.T) {
	t.Parallel()
	src, c1 := setupTestDB(t)
	defer c1()
	h, _ := src.CreateHost(&store.Host{Name: "a", Host: "10.0.0.1", Port: 22, User: "root"})
	cl, err := src.CreateKubeCluster(&store.KubeCluster{Name: "prod", Context: "p", Server: "https://k"})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = src.AddAgentMemory(nil, "fleet note", "agent")
	_, _ = src.AddAgentMemory(&h, "host note", "operator")
	_, _ = src.AddAgentMemoryScoped(store.AgentMemoryScope{ClusterID: &cl}, "cluster note", "agent")
	_, _ = src.AddAgentMemoryScoped(store.AgentMemoryScope{Domain: "docker"}, "domain note", "agent")
	payload, err := src.ExportSnapshot()
	if err != nil {
		t.Fatal(err)
	}

	dst, c2 := setupTestDB(t)
	defer c2()
	_, _ = dst.AddAgentMemory(nil, "stale", "agent")
	if err := dst.ImportSnapshot(payload); err != nil {
		t.Fatal(err)
	}
	got, _ := dst.ListAgentMemory(0, "")
	if len(got) != 4 {
		t.Fatalf("restored: %+v", got)
	}
	for _, n := range got {
		switch n.Content {
		case "host note":
			if n.HostID == nil || n.Source != "operator" {
				t.Errorf("host note lost its scope or source: %+v", n)
			}
		case "cluster note":
			if n.ClusterID == nil {
				t.Errorf("cluster note lost its scope: %+v", n)
			}
		case "domain note":
			if n.Domain != "docker" {
				t.Errorf("domain note lost its scope: %+v", n)
			}
		}
	}
}

// A note carries one scope: a host, a cluster or a domain. Two at once is an error, and the domain spelling
// is normalised so "Security" and "security" are one bucket.
func TestAgentMemory_ScopedToClusterAndDomain(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTestDB(t)
	defer cleanup()
	h, _ := db.CreateHost(&store.Host{Name: "a", Host: "10.0.0.1", Port: 22, User: "root"})
	c, err := db.CreateKubeCluster(&store.KubeCluster{Name: "prod-eks", Context: "prod", Server: "https://k8s"})
	if err != nil {
		t.Fatal(err)
	}

	fleet, err := db.AddAgentMemoryScoped(store.AgentMemoryScope{}, "Backups run at 02:00", "agent")
	if err != nil {
		t.Fatal(err)
	}
	clusterNote, err := db.AddAgentMemoryScoped(store.AgentMemoryScope{ClusterID: &c}, "The ingress controller is traefik", "agent")
	if err != nil {
		t.Fatal(err)
	}
	domainNote, err := db.AddAgentMemoryScoped(store.AgentMemoryScope{Domain: "Security"}, "fail2ban bans after 5 tries", "operator")
	if err != nil {
		t.Fatal(err)
	}
	if domainNote.Domain != "security" {
		t.Fatalf("domain = %q, want the normalised security", domainNote.Domain)
	}

	// The scopes come back from the row, not from the input.
	byID := map[int64]store.AgentMemory{}
	for _, n := range mustList(t, db) {
		byID[n.ID] = n
	}
	if byID[fleet.ID].HostID != nil || byID[fleet.ID].ClusterID != nil || byID[fleet.ID].Domain != "" {
		t.Errorf("a fleet note must carry no scope: %+v", byID[fleet.ID])
	}
	if byID[clusterNote.ID].ClusterID == nil || *byID[clusterNote.ID].ClusterID != c {
		t.Errorf("the cluster scope was lost: %+v", byID[clusterNote.ID])
	}
	if byID[domainNote.ID].Domain != "security" {
		t.Errorf("the domain scope was lost: %+v", byID[domainNote.ID])
	}

	// Exactly one scope may be set, and the domain must be one Pantau knows.
	if _, err := db.AddAgentMemoryScoped(store.AgentMemoryScope{HostID: &h, Domain: "network"}, "x", "agent"); !errors.Is(err, store.ErrAgentMemoryScope) {
		t.Errorf("two scopes: %v", err)
	}
	if _, err := db.AddAgentMemoryScoped(store.AgentMemoryScope{Domain: "nonsense"}, "x", "agent"); !errors.Is(err, store.ErrAgentMemoryDomain) {
		t.Errorf("unknown domain: %v", err)
	}

	// The same text in the same scope is refreshed; in another scope it is a new note.
	again, err := db.AddAgentMemoryScoped(store.AgentMemoryScope{Domain: "security"}, "fail2ban bans after 5 tries", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != domainNote.ID {
		t.Errorf("same text in the same domain stored as #%d, want #%d", again.ID, domainNote.ID)
	}
	other, err := db.AddAgentMemoryScoped(store.AgentMemoryScope{Domain: "network"}, "fail2ban bans after 5 tries", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if other.ID == domainNote.ID {
		t.Error("the same text in another domain must be a separate note")
	}

	// A note tied to a host disappears with the host; the cluster and domain notes stay.
	if _, err := db.AddAgentMemory(&h, "host bound note", "agent"); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteHost(h); err != nil {
		t.Fatal(err)
	}
	for _, n := range mustList(t, db) {
		if n.Content == "host bound note" {
			t.Error("a host-scoped note must go with its host")
		}
	}
}

// Recall returns the notes a question is about, best first, and never the notes of another host.
func TestRecallAgentMemory_PicksRelevantNotes(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTestDB(t)
	defer cleanup()
	h1, _ := db.CreateHost(&store.Host{Name: "db-1", Host: "10.0.0.1", Port: 22, User: "root"})
	h2, _ := db.CreateHost(&store.Host{Name: "web-1", Host: "10.0.0.2", Port: 22, User: "root"})

	_, _ = db.AddAgentMemoryScoped(store.AgentMemoryScope{HostID: &h1}, "postgres replication lags when the archiver runs", "agent")
	_, _ = db.AddAgentMemoryScoped(store.AgentMemoryScope{HostID: &h2}, "nginx caches static assets for one hour", "agent")
	_, _ = db.AddAgentMemoryScoped(store.AgentMemoryScope{Domain: "database"}, "the db-1 primary is the only writer", "agent")
	_, _ = db.AddAgentMemoryScoped(store.AgentMemoryScope{}, "the office VPN gateway is 10.9.0.1", "agent")

	// A question about postgres on db-1: the host note and the database note, never the nginx one.
	got, err := db.RecallAgentMemory(store.AgentMemoryScope{HostID: &h1}, "why does postgres replication lag on db-1?", 12)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("no note recalled")
	}
	found := false
	for _, n := range got {
		if strings.Contains(n.Content, "nginx") {
			t.Errorf("a note about another host must not be recalled: %+v", n)
		}
		if strings.Contains(n.Content, "postgres replication") {
			found = true
			if n.Score <= 0 {
				t.Errorf("a matching note must carry a positive score: %+v", n)
			}
		}
	}
	if !found {
		t.Fatalf("the postgres note was not recalled: %+v", got)
	}

	// A question that names nothing stored recalls nothing rather than everything.
	if got, _ := db.RecallAgentMemory(store.AgentMemoryScope{}, "how is the weather today?", 12); len(got) != 0 {
		t.Errorf("an unrelated question must not pull notes: %+v", got)
	}
	// The limit is honoured.
	if got, _ := db.RecallAgentMemory(store.AgentMemoryScope{}, "postgres replication nginx database vpn office", 1); len(got) != 1 {
		t.Errorf("limit ignored: %d notes", len(got))
	}
}

// The fleet search bridge turns a question into inventory facts, so the prompt gets a summary instead of the
// whole inventory.
func TestRecallContext_SummarisesStoredInventory(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTestDB(t)
	defer cleanup()
	old := time.Now().Add(-time.Hour)
	var ids []int64
	for _, name := range []string{"alpha", "beta"} {
		id, err := db.CreateHost(&store.Host{Name: name, Host: name + ".example.net", Port: 22, User: "root", GroupName: "prod"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		if err := db.ReplaceHostPackages(id, []*store.HostPackage{{HostID: id, Name: "nginx", Version: "1.24.0", LastScannedAt: old}}); err != nil {
			t.Fatal(err)
		}
	}
	_, _ = db.AddAgentMemoryScoped(store.AgentMemoryScope{}, "nginx is the edge proxy", "agent")

	rec, err := db.RecallContext(store.AgentMemoryScope{}, "which hosts run nginx?", 12)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Memories) == 0 || !strings.Contains(rec.Memories[0].Content, "nginx") {
		t.Fatalf("the nginx note must be recalled: %+v", rec.Memories)
	}
	if len(rec.Facts) == 0 {
		t.Fatal("the stored inventory must produce a fact")
	}
	f := rec.Facts[0]
	if f.Kind != store.FleetSearchPackage || f.Name != "nginx" || f.Count != len(ids) {
		t.Fatalf("fact = %+v, want the nginx package on %d hosts", f, len(ids))
	}
	if !strings.Contains(f.Hosts, "alpha") || !strings.Contains(f.Hosts, "beta") {
		t.Errorf("the fact must name the hosts: %q", f.Hosts)
	}

	// A question about nothing stored answers empty, so the caller can skip the section.
	rec, err = db.RecallContext(store.AgentMemoryScope{}, "how is the weather?", 12)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Facts) != 0 || len(rec.Memories) != 0 {
		t.Errorf("an unrelated question must stay empty: %+v", rec)
	}
}

// A question names more than one thing, and only one of them is in the inventory. The fact must still come:
// a fleet search that demands every word to match would answer nothing for "nginx on alpha".
func TestRecallContext_MultiWordQuestionStillFindsInventory(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTestDB(t)
	defer cleanup()
	id, err := db.CreateHost(&store.Host{Name: "alpha", Host: "10.0.0.9", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceHostPackages(id, []*store.HostPackage{{HostID: id, Name: "nginx", Version: "1.24.0", LastScannedAt: time.Now()}}); err != nil {
		t.Fatal(err)
	}

	rec, err := db.RecallContext(store.AgentMemoryScope{}, "is nginx running on alpha?", 12)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Facts) == 0 || rec.Facts[0].Name != "nginx" {
		t.Fatalf("the inventory fact must survive a multi-word question: %+v", rec.Facts)
	}
}

func mustList(t *testing.T, db *store.DB) []store.AgentMemory {
	t.Helper()
	notes, err := db.ListAgentMemory(0, "")
	if err != nil {
		t.Fatal(err)
	}
	return notes
}
