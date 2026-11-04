package store_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"pantau/internal/store"
)

func TestCloudflareAccounts_CRUD(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// 1. Initially empty
	accounts, err := db.ListCloudflareAccounts()
	if err != nil {
		t.Fatalf("list accounts on fresh db: %v", err)
	}
	if len(accounts) != 0 {
		t.Fatalf("expected 0 accounts, got %d", len(accounts))
	}

	// 2. Create account
	a1 := &store.CloudflareAccount{
		Name:      "personal",
		AccountID: "acc-id-123",
		APIToken:  "secret-token-xyz",
	}
	id1, err := db.CreateCloudflareAccount(a1)
	if err != nil {
		t.Fatalf("create account a1: %v", err)
	}
	if id1 <= 0 {
		t.Fatalf("expected positive id, got %d", id1)
	}

	// 3. Get account (includes APIToken)
	got1, err := db.GetCloudflareAccount(id1)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if got1 == nil {
		t.Fatalf("expected account %d, got nil", id1)
	}
	if got1.Name != "personal" || got1.AccountID != "acc-id-123" {
		t.Errorf("unexpected retrieved account: %+v", got1)
	}
	if got1.APIToken != "secret-token-xyz" {
		t.Errorf("expected api token preserved, got %q", got1.APIToken)
	}

	// 4. Missing account returns (nil, nil)
	missing, err := db.GetCloudflareAccount(99999)
	if err != nil {
		t.Fatalf("get missing account returned error: %v", err)
	}
	if missing != nil {
		t.Fatalf("expected nil for missing account, got %+v", missing)
	}

	// 5. Duplicate name (case-insensitive) returns ErrCloudflareAccountNameTaken
	aDup := &store.CloudflareAccount{
		Name:      "PERSONAL",
		AccountID: "different-acc",
		APIToken:  "another-token",
	}
	_, err = db.CreateCloudflareAccount(aDup)
	if !errors.Is(err, store.ErrCloudflareAccountNameTaken) {
		t.Fatalf("expected ErrCloudflareAccountNameTaken for duplicate name, got %v", err)
	}

	// 6. List ordering (case-insensitive by name)
	a2 := &store.CloudflareAccount{
		Name:      "alpha-work",
		AccountID: "acc-alpha",
		APIToken:  "tok-alpha",
	}
	a3 := &store.CloudflareAccount{
		Name:      "Beta-team",
		AccountID: "acc-beta",
		APIToken:  "tok-beta",
	}
	if _, err := db.CreateCloudflareAccount(a2); err != nil {
		t.Fatalf("create a2: %v", err)
	}
	if _, err := db.CreateCloudflareAccount(a3); err != nil {
		t.Fatalf("create a3: %v", err)
	}

	list, err := db.ListCloudflareAccounts()
	if err != nil {
		t.Fatalf("list accounts: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 accounts, got %d", len(list))
	}
	expectedOrder := []string{"alpha-work", "Beta-team", "personal"}
	for i, exp := range expectedOrder {
		if list[i].Name != exp {
			t.Errorf("list[%d].Name = %q, expected %q", i, list[i].Name, exp)
		}
	}

	// 7. Update without APIToken (keeps old token)
	got1.Name = "personal-renamed"
	got1.AccountID = "acc-updated-id"
	got1.APIToken = "" // empty means do not overwrite
	if err := db.UpdateCloudflareAccount(got1); err != nil {
		t.Fatalf("update account without token: %v", err)
	}

	updated1, err := db.GetCloudflareAccount(id1)
	if err != nil {
		t.Fatalf("get updated1: %v", err)
	}
	if updated1.Name != "personal-renamed" || updated1.AccountID != "acc-updated-id" {
		t.Errorf("expected name and account_id updated, got %+v", updated1)
	}
	if updated1.APIToken != "secret-token-xyz" {
		t.Errorf("expected token retained, got %q", updated1.APIToken)
	}

	// 8. Update with non-empty APIToken (replaces old token)
	updated1.APIToken = "brand-new-token"
	if err := db.UpdateCloudflareAccount(updated1); err != nil {
		t.Fatalf("update account with token: %v", err)
	}

	updatedAgain, err := db.GetCloudflareAccount(id1)
	if err != nil {
		t.Fatalf("get updatedAgain: %v", err)
	}
	if updatedAgain.APIToken != "brand-new-token" {
		t.Errorf("expected updated token 'brand-new-token', got %q", updatedAgain.APIToken)
	}

	// 9. Update to duplicate name returns ErrCloudflareAccountNameTaken
	updatedAgain.Name = "ALPHA-WORK" // matches a2
	if err := db.UpdateCloudflareAccount(updatedAgain); !errors.Is(err, store.ErrCloudflareAccountNameTaken) {
		t.Fatalf("expected ErrCloudflareAccountNameTaken on rename conflict, got %v", err)
	}

	// 10. Delete account
	if err := db.DeleteCloudflareAccount(id1); err != nil {
		t.Fatalf("delete account: %v", err)
	}
	deleted, err := db.GetCloudflareAccount(id1)
	if err != nil {
		t.Fatalf("get deleted: %v", err)
	}
	if deleted != nil {
		t.Fatalf("expected nil after delete, got %+v", deleted)
	}
}

func TestCloudflareAccount_JSONMarshalOmitsToken(t *testing.T) {
	acc := &store.CloudflareAccount{
		ID:        1,
		Name:      "test-account",
		AccountID: "cf-acc-123",
		APIToken:  "super-secret-token",
		CreatedAt: time.Now().UTC(),
	}

	data, err := json.Marshal(acc)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	s := string(data)
	if strings.Contains(s, "super-secret-token") || strings.Contains(s, "api_token") {
		t.Fatalf("token leaked in JSON output: %s", s)
	}
}

func TestCloudflareAccounts_SnapshotRoundTrip(t *testing.T) {
	db1, cleanup1 := setupTestDB(t)
	defer cleanup1()

	a1ID, err := db1.CreateCloudflareAccount(&store.CloudflareAccount{
		Name:      "main-account",
		AccountID: "cf-acc-999",
		APIToken:  "secret-cf-token-12345",
	})
	if err != nil {
		t.Fatalf("create cf account in db1: %v", err)
	}

	// Export snapshot
	payload, err := db1.ExportSnapshot()
	if err != nil {
		t.Fatalf("export snapshot: %v", err)
	}
	if len(payload.CloudflareAccounts) != 1 {
		t.Fatalf("expected 1 cloudflare account in snapshot, got %d", len(payload.CloudflareAccounts))
	}
	snapA := payload.CloudflareAccounts[0]
	if snapA.ID != a1ID || snapA.Name != "main-account" || snapA.AccountID != "cf-acc-999" || snapA.APIToken != "secret-cf-token-12345" {
		t.Errorf("unexpected exported snapshot account: %+v", snapA)
	}

	// Import into new db
	db2, cleanup2 := setupTestDB(t)
	defer cleanup2()

	if err := db2.ImportSnapshot(payload); err != nil {
		t.Fatalf("import snapshot to db2: %v", err)
	}

	restored, err := db2.GetCloudflareAccount(a1ID)
	if err != nil {
		t.Fatalf("get restored account: %v", err)
	}
	if restored == nil {
		t.Fatalf("expected restored account with id %d, got nil", a1ID)
	}
	if restored.Name != "main-account" || restored.AccountID != "cf-acc-999" || restored.APIToken != "secret-cf-token-12345" {
		t.Errorf("restored account does not match exported: %+v", restored)
	}

	// Backward compatibility: import older legacy snapshot without CloudflareAccounts
	legacyPayload := &store.SnapshotPayload{
		Version:   1,
		CreatedAt: time.Now().UTC(),
		Settings:  map[string]string{"foo": "bar"},
		Hosts: []store.Host{
			{
				ID:   42,
				Name: "Legacy Host",
				Host: "192.168.1.42",
				Port: 22,
				User: "root",
			},
		},
	}

	if err := db2.ImportSnapshot(legacyPayload); err != nil {
		t.Fatalf("import legacy payload failed: %v", err)
	}

	// Previous accounts should be cleared since ImportSnapshot wipes tables
	accountsAfterLegacy, err := db2.ListCloudflareAccounts()
	if err != nil {
		t.Fatalf("list accounts after legacy import: %v", err)
	}
	if len(accountsAfterLegacy) != 0 {
		t.Fatalf("expected 0 accounts after legacy import wipe, got %d", len(accountsAfterLegacy))
	}

	// Verify legacy host was restored
	h, err := db2.GetHost(42)
	if err != nil || h == nil {
		t.Fatalf("expected legacy host 42, got %v, err=%v", h, err)
	}
}
