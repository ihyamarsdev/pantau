package store_test

import (
	"testing"

	"pantau/internal/store"
)

// group_name is a plain label that must survive update, listing and a System Snapshot round trip.
func TestPlaybookAndWorkspaceGroupNameRoundTrip(t *testing.T) {
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
