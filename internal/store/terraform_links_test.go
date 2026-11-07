package store_test

import (
	"path/filepath"
	"testing"

	"pantau/internal/store"
)

func TestOutputLinkCRUDAndDeleteCleanup(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a, _ := db.CreateTFWorkspace(&store.TFWorkspace{Name: "a"})
	b, _ := db.CreateTFWorkspace(&store.TFWorkspace{Name: "b"})
	c, _ := db.CreateTFWorkspace(&store.TFWorkspace{Name: "c"})

	if got, err := db.ListTFOutputLinks(a); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty list = %v, %v", got, err)
	}
	// a consumes b.x and c.y; c consumes b.z
	if err := db.ReplaceTFOutputLinks(a, []store.TFOutputLink{
		{VarName: "x", SourceWorkspaceID: b, OutputName: "x"},
		{VarName: "y", SourceWorkspaceID: c, OutputName: "y"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceTFOutputLinks(c, []store.TFOutputLink{{VarName: "z", SourceWorkspaceID: b, OutputName: "z"}}); err != nil {
		t.Fatal(err)
	}
	got, _ := db.ListTFOutputLinks(a)
	if len(got) != 2 || got[0].VarName != "x" || got[0].SourceWorkspaceID != b || got[1].OutputName != "y" {
		t.Fatalf("links = %+v", got)
	}

	// replace overwrites; duplicate var names fail and roll back
	if err := db.ReplaceTFOutputLinks(a, []store.TFOutputLink{{VarName: "n", SourceWorkspaceID: b, OutputName: "o"}}); err != nil {
		t.Fatal(err)
	}
	dup := []store.TFOutputLink{{VarName: "d", SourceWorkspaceID: b, OutputName: "o"}, {VarName: "d", SourceWorkspaceID: c, OutputName: "o"}}
	if err := db.ReplaceTFOutputLinks(a, dup); err == nil {
		t.Fatal("duplicate var_name must fail")
	}
	if got, _ := db.ListTFOutputLinks(a); len(got) != 1 || got[0].VarName != "n" {
		t.Fatalf("rollback failed: %+v", got)
	}

	// deleting a source removes links pointing at it
	if err := db.DeleteTFWorkspace(b); err != nil {
		t.Fatal(err)
	}
	all, _ := db.ListAllTFOutputLinks()
	if len(all) != 0 {
		t.Fatalf("links to deleted source remain: %+v", all)
	}

	// deleting a consumer removes its links
	if err := db.ReplaceTFOutputLinks(a, []store.TFOutputLink{{VarName: "q", SourceWorkspaceID: c, OutputName: "o"}}); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteTFWorkspace(a); err != nil {
		t.Fatal(err)
	}
	if all, _ := db.ListAllTFOutputLinks(); len(all) != 0 {
		t.Fatalf("links of deleted consumer remain: %+v", all)
	}
}
