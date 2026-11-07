package store_test

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"pantau/internal/store"
)

func mustCloudAccount(t *testing.T, db *store.DB, name string) *store.TFCloudAccount {
	t.Helper()
	a := &store.TFCloudAccount{
		Name: name, Provider: "aws", Description: "d",
		Fields: map[string]string{"access_key_id": "AKIA123", "secret_access_key": "s3cr3t-value", "region": "eu-west-1"},
	}
	if _, err := db.CreateTFCloudAccount(a); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	return a
}

func TestTFCloudAccount_CRUD(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	list, err := db.ListTFCloudAccounts()
	if err != nil || list == nil || len(list) != 0 {
		t.Fatalf("empty list must be non-nil and empty: %v %v", list, err)
	}
	if got, err := db.GetTFCloudAccount(99); err != nil || got != nil {
		t.Fatalf("missing account: want nil,nil got %v,%v", got, err)
	}

	b := mustCloudAccount(t, db, "beta")
	a := mustCloudAccount(t, db, "Alpha")
	if a.ID == 0 || a.CreatedAt.IsZero() {
		t.Fatalf("create must fill ID and CreatedAt: %+v", a)
	}

	// case-insensitive duplicate
	if _, err := db.CreateTFCloudAccount(&store.TFCloudAccount{Name: "ALPHA", Provider: "aws"}); !errors.Is(err, store.ErrTFCloudAccountNameTaken) {
		t.Fatalf("want ErrTFCloudAccountNameTaken, got %v", err)
	}

	got, err := db.GetTFCloudAccount(a.ID)
	if err != nil || got == nil {
		t.Fatalf("get: %v %v", got, err)
	}
	if !reflect.DeepEqual(got.Fields, a.Fields) || got.Provider != "aws" || got.Description != "d" || got.VerifiedAt != nil || got.VerifyOK {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	list, _ = db.ListTFCloudAccounts()
	if len(list) != 2 || list[0].Name != "Alpha" || list[1].ID != b.ID {
		t.Fatalf("list must be ordered by name nocase: %+v", list)
	}

	// update to a name taken by another account
	got.Name = "BETA"
	if err := db.UpdateTFCloudAccount(got); !errors.Is(err, store.ErrTFCloudAccountNameTaken) {
		t.Fatalf("update dup: want name taken, got %v", err)
	}

	got.Name, got.Provider, got.Fields = "Alpha2", "digitalocean", map[string]string{"token": "tok-1234"}
	if err := db.UpdateTFCloudAccount(got); err != nil {
		t.Fatalf("update: %v", err)
	}
	again, _ := db.GetTFCloudAccount(a.ID)
	if again.Name != "Alpha2" || again.Provider != "digitalocean" || !reflect.DeepEqual(again.Fields, map[string]string{"token": "tok-1234"}) {
		t.Fatalf("update not persisted wholesale: %+v", again)
	}

	if err := db.DeleteTFCloudAccount(a.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got, _ := db.GetTFCloudAccount(a.ID); got != nil {
		t.Fatalf("account still present after delete")
	}
}

func TestTFCloudAccount_VerifyStoredAndResetOnUpdate(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	a := mustCloudAccount(t, db, "acme")

	if err := db.SetTFCloudAccountVerify(a.ID, true, "account 123"); err != nil {
		t.Fatalf("set verify: %v", err)
	}
	got, _ := db.GetTFCloudAccount(a.ID)
	if !got.VerifyOK || got.VerifyMsg != "account 123" || got.VerifiedAt == nil || time.Since(*got.VerifiedAt) > time.Minute {
		t.Fatalf("verify not stored: %+v", got)
	}

	if err := db.UpdateTFCloudAccount(got); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = db.GetTFCloudAccount(a.ID)
	if got.VerifyOK || got.VerifyMsg != "" || got.VerifiedAt != nil {
		t.Fatalf("update must reset verify: %+v", got)
	}
}

func TestTFCloudAccount_JSONOmitsFields(t *testing.T) {
	data, err := json.Marshal(&store.TFCloudAccount{Name: "x", Fields: map[string]string{"token": "super-secret-tok"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "super-secret-tok") || strings.Contains(string(data), "fields") {
		t.Fatalf("fields leaked: %s", data)
	}
}

func TestTFWorkspace_CloudAccountIDs(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	a, b := mustCloudAccount(t, db, "a"), mustCloudAccount(t, db, "b")

	// empty/nil round-trips as [] in Go and JSON
	wsEmpty := &store.TFWorkspace{Name: "empty"}
	emptyID, err := db.CreateTFWorkspace(wsEmpty)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := db.GetTFWorkspace(emptyID)
	if got.CloudAccountIDs == nil || len(got.CloudAccountIDs) != 0 {
		t.Fatalf("want empty non-nil, got %#v", got.CloudAccountIDs)
	}
	data, _ := json.Marshal(got)
	if !strings.Contains(string(data), `"cloud_account_ids":[]`) {
		t.Fatalf("want cloud_account_ids [] in JSON: %s", data)
	}

	ws := &store.TFWorkspace{Name: "linked", CloudAccountIDs: []int64{b.ID, a.ID}}
	id, err := db.CreateTFWorkspace(ws)
	if err != nil {
		t.Fatal(err)
	}
	got, _ = db.GetTFWorkspace(id)
	if !reflect.DeepEqual(got.CloudAccountIDs, []int64{b.ID, a.ID}) {
		t.Fatalf("create round-trip: %v", got.CloudAccountIDs)
	}
	list, _ := db.ListTFWorkspaces("", "", "linked")
	if len(list) != 1 || !reflect.DeepEqual(list[0].CloudAccountIDs, []int64{b.ID, a.ID}) {
		t.Fatalf("list round-trip: %+v", list)
	}

	got.CloudAccountIDs = []int64{a.ID}
	if err := db.UpdateTFWorkspace(got); err != nil {
		t.Fatal(err)
	}
	got, _ = db.GetTFWorkspace(id)
	if !reflect.DeepEqual(got.CloudAccountIDs, []int64{a.ID}) {
		t.Fatalf("update round-trip: %v", got.CloudAccountIDs)
	}
	got.CloudAccountIDs = nil
	if err := db.UpdateTFWorkspace(got); err != nil {
		t.Fatal(err)
	}
	got, _ = db.GetTFWorkspace(id)
	if got.CloudAccountIDs == nil || len(got.CloudAccountIDs) != 0 {
		t.Fatalf("update to empty: %#v", got.CloudAccountIDs)
	}
}

func TestTFCloudAccount_DeleteUnlinksAndCount(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	a, b := mustCloudAccount(t, db, "a"), mustCloudAccount(t, db, "b")

	w1, _ := db.CreateTFWorkspace(&store.TFWorkspace{Name: "w1", CloudAccountIDs: []int64{a.ID, b.ID}})
	w2, _ := db.CreateTFWorkspace(&store.TFWorkspace{Name: "w2", CloudAccountIDs: []int64{a.ID}})
	w3, _ := db.CreateTFWorkspace(&store.TFWorkspace{Name: "w3", CloudAccountIDs: []int64{b.ID}})

	for id, want := range map[int64]int{a.ID: 2, b.ID: 2, 999: 0} {
		if n, err := db.TFCloudAccountWorkspaceCount(id); err != nil || n != want {
			t.Fatalf("count(%d) = %d,%v want %d", id, n, err, want)
		}
	}

	if err := db.DeleteTFCloudAccount(a.ID); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[int64][]int64{w1: {b.ID}, w2: {}, w3: {b.ID}} {
		got, _ := db.GetTFWorkspace(id)
		if !reflect.DeepEqual(got.CloudAccountIDs, want) {
			t.Fatalf("ws %d ids = %v want %v", id, got.CloudAccountIDs, want)
		}
	}
	if n, _ := db.TFCloudAccountWorkspaceCount(a.ID); n != 0 {
		t.Fatalf("deleted account count = %d", n)
	}
}

func TestTFCloudAccount_SnapshotRoundTrip(t *testing.T) {
	db1, cleanup1 := setupTestDB(t)
	defer cleanup1()
	a := mustCloudAccount(t, db1, "prod-aws")
	if err := db1.SetTFCloudAccountVerify(a.ID, true, "ok"); err != nil {
		t.Fatal(err)
	}
	wsID, _ := db1.CreateTFWorkspace(&store.TFWorkspace{Name: "infra", CloudAccountIDs: []int64{a.ID}})

	payload, err := db1.ExportSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var decoded store.SnapshotPayload
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}

	db2, cleanup2 := setupTestDB(t)
	defer cleanup2()
	if err := db2.ImportSnapshot(&decoded); err != nil {
		t.Fatalf("import: %v", err)
	}
	got, _ := db2.GetTFCloudAccount(a.ID)
	if got == nil || got.Name != "prod-aws" || !reflect.DeepEqual(got.Fields, a.Fields) || !got.VerifyOK || got.VerifiedAt == nil {
		t.Fatalf("restored account: %+v", got)
	}
	ws, _ := db2.GetTFWorkspace(wsID)
	if !reflect.DeepEqual(ws.CloudAccountIDs, []int64{a.ID}) {
		t.Fatalf("restored workspace link: %v", ws.CloudAccountIDs)
	}
}

func TestTFCloudAccount_RestoreOldSnapshot(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	mustCloudAccount(t, db, "stale")

	// pre-cloud-accounts snapshot: no accounts, workspace JSON without cloud_account_ids
	old := `{"version":1,"created_at":"2026-01-01T00:00:00Z","terraform_workspaces":[{"id":7,"name":"legacy","binary":"terraform","source":"inline","files":"{}","tags":"[]","git_auth":"none","webhook_token":"tok-legacy","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}]}`
	var p store.SnapshotPayload
	if err := json.Unmarshal([]byte(old), &p); err != nil {
		t.Fatal(err)
	}
	if err := db.ImportSnapshot(&p); err != nil {
		t.Fatalf("import old snapshot: %v", err)
	}
	ws, err := db.GetTFWorkspace(7)
	if err != nil || ws.CloudAccountIDs == nil || len(ws.CloudAccountIDs) != 0 {
		t.Fatalf("legacy workspace: %+v %v", ws, err)
	}
	if list, _ := db.ListTFCloudAccounts(); len(list) != 0 {
		t.Fatalf("restore must replace accounts, got %d", len(list))
	}
}

func TestTFCloudAccount_MigrationIdempotentOnLegacyDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := db.CreateTFWorkspace(&store.TFWorkspace{Name: "old-ws"})
	// emulate a DB created before the feature
	for _, q := range []string{`ALTER TABLE tf_workspaces DROP COLUMN cloud_account_ids`, `DROP TABLE tf_cloud_accounts`} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	db.Close()

	for i := range 2 { // second open must be a no-op
		db, err = store.Open(path)
		if err != nil {
			t.Fatalf("open #%d: %v", i, err)
		}
		ws, err := db.GetTFWorkspace(id)
		if err != nil || ws.CloudAccountIDs == nil || len(ws.CloudAccountIDs) != 0 {
			t.Fatalf("open #%d legacy workspace: %+v %v", i, ws, err)
		}
		if _, err := db.ListTFCloudAccounts(); err != nil {
			t.Fatalf("open #%d list: %v", i, err)
		}
		db.Close()
	}
}
