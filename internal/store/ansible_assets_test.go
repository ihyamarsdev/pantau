package store_test

import (
	"errors"
	"testing"

	"pantau/internal/store"
)

func TestAnsibleAssets_CRUDAndUpsert(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	if err := db.SaveAnsibleAsset("template", "app.conf.j2", []byte("v1")); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveAnsibleAsset("file", "app.conf.j2", []byte{0, 1, 2}); err != nil { // same name, other kind
		t.Fatal(err)
	}
	if err := db.SaveAnsibleAsset("template", "app.conf.j2", []byte("version two")); err != nil { // replace
		t.Fatal(err)
	}

	all, err := db.ListAnsibleAssets("")
	if err != nil || len(all) != 2 {
		t.Fatalf("list all: %v %+v", err, all)
	}
	tpl, _ := db.ListAnsibleAssets("template")
	if len(tpl) != 1 || tpl[0].Size != 11 || tpl[0].Data != nil {
		t.Fatalf("template list should carry size but no data: %+v", tpl)
	}

	got, err := db.GetAnsibleAsset(tpl[0].ID)
	if err != nil || string(got.Data) != "version two" {
		t.Fatalf("get: %v %+v", err, got)
	}

	if err := db.DeleteAnsibleAsset(tpl[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetAnsibleAsset(tpl[0].ID); !errors.Is(err, store.ErrAnsibleAssetNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
	if err := db.DeleteAnsibleAsset(tpl[0].ID); !errors.Is(err, store.ErrAnsibleAssetNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

func TestAnsibleAssets_SnapshotRoundtrip(t *testing.T) {
	src, cleanup1 := setupTestDB(t)
	defer cleanup1()
	_ = src.SaveAnsibleAsset("template", "a/b.j2", []byte("{{ x }}"))
	_ = src.SaveAnsibleAsset("file", "bin", []byte{0xff, 0x00, 0x7f})

	payload, err := src.ExportSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.AnsibleAssets) != 2 {
		t.Fatalf("snapshot assets: %+v", payload.AnsibleAssets)
	}

	dst, cleanup2 := setupTestDB(t)
	defer cleanup2()
	_ = dst.SaveAnsibleAsset("file", "stale", []byte("x")) // restore replaces everything
	if err := dst.ImportSnapshot(payload); err != nil {
		t.Fatal(err)
	}
	restored, _ := dst.LoadAnsibleAssets()
	if len(restored) != 2 {
		t.Fatalf("restored: %+v", restored)
	}
	for _, a := range restored {
		switch a.Name {
		case "a/b.j2":
			if a.Kind != "template" || string(a.Data) != "{{ x }}" {
				t.Fatalf("template: %+v", a)
			}
		case "bin":
			if a.Kind != "file" || string(a.Data) != "\xff\x00\x7f" || a.Size != 3 {
				t.Fatalf("binary file not intact: %+v", a)
			}
		default:
			t.Fatalf("unexpected asset %q", a.Name)
		}
	}
}
