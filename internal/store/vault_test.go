package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"pantau/internal/vault"
)

func newVaultTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestVaultMasterKeyPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	key1, err := db.VaultMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(key1) != 32 {
		t.Fatalf("master key length = %d, want 32", len(key1))
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	key2, err := db2.VaultMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	if string(key1) != string(key2) {
		t.Fatal("master key changed across reopen")
	}
}

func TestVaultSecretCRUDAndVersions(t *testing.T) {
	db := newVaultTestDB(t)

	created := &vault.Secret{
		SecretMeta: vault.SecretMeta{
			Key:      "E2E_TOKEN",
			Scope:    vault.ScopeGlobal,
			ScopeRef: "",
		},
		Value: "supersecret-v1",
	}
	id, err := db.CreateVaultSecret(created)
	if err != nil {
		t.Fatal(err)
	}
	if id <= 0 {
		t.Fatalf("id = %d, want > 0", id)
	}
	if created.Version != 1 {
		t.Fatalf("version = %d, want 1", created.Version)
	}

	// Get by id and case-insensitive key both decrypt to the same value.
	got, err := db.GetVaultSecret(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != "supersecret-v1" {
		t.Fatalf("value = %q, want supersecret-v1", got.Value)
	}
	byKey, err := db.GetVaultSecretByKey("e2e_token")
	if err != nil {
		t.Fatal(err)
	}
	if byKey.ID != id || byKey.Value != "supersecret-v1" {
		t.Fatalf("GetVaultSecretByKey = %+v, want id %d and original value", byKey, id)
	}

	// List returns metadata only.
	list, err := db.ListVaultSecrets()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != id || list[0].Key != "E2E_TOKEN" {
		t.Fatalf("ListVaultSecrets = %+v", list)
	}

	// Values helper (for the redactor) sees the plaintext.
	vals, err := db.ListVaultSecretValues()
	if err != nil {
		t.Fatal(err)
	}
	if len(vals) != 1 || vals[0] != "supersecret-v1" {
		t.Fatalf("ListVaultSecretValues = %v", vals)
	}

	// Update stores a new ciphertext and increments the version.
	if err := db.UpdateVaultSecret(id, "supersecret-v2", nil, 0); err != nil {
		t.Fatal(err)
	}
	got, err = db.GetVaultSecret(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != "supersecret-v2" || got.Version != 2 {
		t.Fatalf("after update: value=%q version=%d", got.Value, got.Version)
	}

	// Both the old and new versions are retained.
	versions, err := db.ListVaultVersions(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 {
		t.Fatalf("versions = %+v, want 2", versions)
	}
	if versions[0].Version != 1 || versions[1].Version != 2 {
		t.Fatalf("versions = %+v, want [1 2]", versions)
	}

	// Rollback restores v1's value as a NEW current version (3).
	if err := db.RollbackVaultSecret(id, 1); err != nil {
		t.Fatal(err)
	}
	got, err = db.GetVaultSecret(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != "supersecret-v1" || got.Version != 3 {
		t.Fatalf("after rollback: value=%q version=%d, want supersecret-v1 / 3", got.Value, got.Version)
	}
	versions, err = db.ListVaultVersions(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 3 {
		t.Fatalf("versions after rollback = %+v, want 3", versions)
	}
}

func TestVaultSecretNeedsRotation(t *testing.T) {
	db := newVaultTestDB(t)

	past := time.Now().Add(-48 * time.Hour).UTC()
	id, err := db.CreateVaultSecret(&vault.Secret{
		SecretMeta: vault.SecretMeta{Key: "EXPIRED", Scope: vault.ScopeGlobal, ExpiresAt: &past},
		Value:      "v",
	})
	if err != nil {
		t.Fatal(err)
	}
	list, err := db.ListVaultSecrets()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || !list[0].NeedsRotation {
		t.Fatalf("expired secret must need rotation: %+v", list)
	}

	// A rotation window of 1 day over a 2-day-old secret also needs rotation.
	id2, err := db.CreateVaultSecret(&vault.Secret{
		SecretMeta: vault.SecretMeta{Key: "OLD", Scope: vault.ScopeGlobal, RotationDays: 1},
		Value:      "v",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Backdate created_at so the rotation window has elapsed.
	if _, err := db.Exec(`UPDATE vault_secrets SET created_at = ?, updated_at = ? WHERE id = ?`, past, past, id2); err != nil {
		t.Fatal(err)
	}
	list, err = db.ListVaultSecrets()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int64]vault.SecretMeta{}
	for _, m := range list {
		byID[m.ID] = m
	}
	if !byID[id].NeedsRotation || !byID[id2].NeedsRotation {
		t.Fatalf("both secrets should need rotation: %+v", byID)
	}
}

func TestVaultSecretDeleteRemovesVersions(t *testing.T) {
	db := newVaultTestDB(t)
	id, err := db.CreateVaultSecret(&vault.Secret{
		SecretMeta: vault.SecretMeta{Key: "DEL", Scope: vault.ScopeGlobal},
		Value:      "v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateVaultSecret(id, "v2", nil, 0); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteVaultSecret(id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetVaultSecret(id); err != sql.ErrNoRows {
		t.Fatalf("GetVaultSecret after delete err = %v, want ErrNoRows", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM vault_versions WHERE secret_id = ?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("versions after delete = %d, want 0", n)
	}
}

func TestVaultTemplateCRUD(t *testing.T) {
	db := newVaultTestDB(t)
	tpl := &vault.Template{Name: "env", Body: "TOKEN={{ vault.DEL }}"}
	id, err := db.CreateVaultTemplate(tpl)
	if err != nil {
		t.Fatal(err)
	}
	if id <= 0 {
		t.Fatalf("id = %d", id)
	}

	got, err := db.GetVaultTemplate(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "env" || got.Body != "TOKEN={{ vault.DEL }}" {
		t.Fatalf("GetVaultTemplate = %+v", got)
	}

	list, err := db.ListVaultTemplates()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("ListVaultTemplates = %+v", list)
	}

	got.Body = "TOKEN={{ vault.NEW }}"
	if err := db.UpdateVaultTemplate(got); err != nil {
		t.Fatal(err)
	}
	got, err = db.GetVaultTemplate(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "TOKEN={{ vault.NEW }}" {
		t.Fatalf("body after update = %q", got.Body)
	}

	if err := db.DeleteVaultTemplate(id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetVaultTemplate(id); err != sql.ErrNoRows {
		t.Fatalf("GetVaultTemplate after delete err = %v, want ErrNoRows", err)
	}
}

func TestVaultTargetCRUDAndSync(t *testing.T) {
	db := newVaultTestDB(t)
	target := &vault.SyncTarget{
		Name:       "web-01",
		Kind:       "host",
		TemplateID: 7,
		TargetRef:  "42",
		Path:       "/etc/app.env",
	}
	id, err := db.CreateVaultTarget(target)
	if err != nil {
		t.Fatal(err)
	}
	if id <= 0 {
		t.Fatalf("id = %d", id)
	}
	if target.DriftStatus != "unknown" {
		t.Fatalf("drift = %q, want unknown", target.DriftStatus)
	}

	got, err := db.GetVaultTarget(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "web-01" || got.Kind != "host" || got.TargetRef != "42" {
		t.Fatalf("GetVaultTarget = %+v", got)
	}

	list, err := db.ListVaultTargets()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("ListVaultTargets = %+v", list)
	}

	got.Path = "/etc/other.env"
	if err := db.UpdateVaultTarget(got); err != nil {
		t.Fatal(err)
	}
	got, err = db.GetVaultTarget(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != "/etc/other.env" {
		t.Fatalf("path after update = %q", got.Path)
	}

	at := time.Now().UTC().Truncate(time.Second)
	if err := db.UpdateVaultTargetSync(id, "abc123", "in-sync", at); err != nil {
		t.Fatal(err)
	}
	got, err = db.GetVaultTarget(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastSyncSHA != "abc123" || got.DriftStatus != "in-sync" || got.LastSyncAt == nil {
		t.Fatalf("after sync = %+v", got)
	}

	if err := db.DeleteVaultTarget(id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetVaultTarget(id); err != sql.ErrNoRows {
		t.Fatalf("GetVaultTarget after delete err = %v, want ErrNoRows", err)
	}
}

func TestVaultAudit(t *testing.T) {
	db := newVaultTestDB(t)
	if err := db.AddVaultAudit("secret.create", "E2E_TOKEN"); err != nil {
		t.Fatal(err)
	}
	if err := db.AddVaultAudit("secret.reveal", "E2E_TOKEN"); err != nil {
		t.Fatal(err)
	}
	entries, err := db.ListVaultAudit()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("ListVaultAudit = %+v, want 2", entries)
	}
	// Newest first.
	if entries[0].Action != "secret.reveal" || entries[1].Action != "secret.create" {
		t.Fatalf("audit order = %q, %q", entries[0].Action, entries[1].Action)
	}
}

func TestVaultMigrationIdempotentOnReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	// Opening the same DB twice must not fail on the vault CREATE TABLEs.
	db2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	if _, err := db2.VaultMasterKey(); err != nil {
		t.Fatal(err)
	}
}
