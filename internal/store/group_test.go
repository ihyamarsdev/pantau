package store_test

import (
	"testing"

	"pantau/internal/store"
)

// group_name is a plain label that must survive update, listing and a System Snapshot round trip.
func TestPlaybookAndWorkspaceGroupNameRoundTrip(t *testing.T) {
	t.Parallel()
	db1, cleanup1 := setupTestDB(t)
	defer cleanup1()

	pbID, err := db1.CreateAnsiblePlaybook(&store.AnsiblePlaybook{Name: "deploy", Content: "- hosts: all\n", GroupName: "Web"})
	if err != nil {
		t.Fatal(err)
	}
	wsID, err := db1.CreateTFWorkspace(&store.TFWorkspace{Name: "prod", Binary: "tofu", Source: "inline", Files: "{}", GroupName: "Network"})
	if err != nil {
		t.Fatal(err)
	}

	pb, _ := db1.GetAnsiblePlaybook(pbID)
	pb.GroupName = "Edge"
	if err := db1.UpdateAnsiblePlaybook(pb); err != nil {
		t.Fatal(err)
	}
	if list, _ := db1.ListAnsiblePlaybooks(); len(list) != 1 || list[0].GroupName != "Edge" {
		t.Fatalf("playbook list = %+v", list)
	}

	payload, err := db1.ExportSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	db2, cleanup2 := setupTestDB(t)
	defer cleanup2()
	if err := db2.ImportSnapshot(payload); err != nil {
		t.Fatal(err)
	}
	if list, _ := db2.ListAnsiblePlaybooks(); len(list) != 1 || list[0].GroupName != "Edge" {
		t.Fatalf("restored playbooks = %+v", list)
	}
	if ws, err := db2.GetTFWorkspace(wsID); err != nil || ws.GroupName != "Network" {
		t.Fatalf("restored workspace = %+v, %v", ws, err)
	}
}

// Host Group names are unique regardless of letter case: a name that matches an existing group only in case takes that
// group's spelling on create, edit and bulk assign. A host that is the only member of its group may change its case.
func TestHostGroupNamesAreUniqueRegardlessOfCase(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTestDB(t)
	defer cleanup()

	add := func(name, group string) int64 {
		t.Helper()
		id, err := db.CreateHost(&store.Host{Name: name, Host: name + ".lan", Port: 22, User: "root", GroupName: group})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	groupOf := func(id int64) string {
		t.Helper()
		h, err := db.GetHost(id)
		if err != nil {
			t.Fatal(err)
		}
		return h.GroupName
	}

	alpha := add("alpha", "Prod")
	beta := add("beta", "prod")
	if got := groupOf(beta); got != "Prod" {
		t.Fatalf("create stored %q, want Prod", got)
	}

	gamma := add("gamma", "")
	h, err := db.GetHost(gamma)
	if err != nil {
		t.Fatal(err)
	}
	h.GroupName = " PROD "
	if err := db.UpdateHost(h); err != nil {
		t.Fatal(err)
	}
	if got := groupOf(gamma); got != "Prod" {
		t.Fatalf("edit stored %q, want Prod", got)
	}

	if err := db.UpdateHostGroups([]store.HostGroupAssignment{{ID: alpha, GroupName: "staging"}, {ID: gamma, GroupName: "STAGING"}}); err != nil {
		t.Fatal(err)
	}
	if a, g := groupOf(alpha), groupOf(gamma); a != "staging" || g != "staging" {
		t.Fatalf("bulk assign stored %q and %q, want staging for both", a, g)
	}

	solo := add("delta", "edge")
	h, err = db.GetHost(solo)
	if err != nil {
		t.Fatal(err)
	}
	h.GroupName = "Edge"
	if err := db.UpdateHost(h); err != nil {
		t.Fatal(err)
	}
	if got := groupOf(solo); got != "Edge" {
		t.Fatalf("sole member re-cased to %q, want Edge", got)
	}
}
