package store

import (
	"testing"

	"pantau/internal/vault"
)

func vaultValues(t *testing.T, db *DB) []string {
	t.Helper()
	vals, err := db.ListVaultSecretValues()
	if err != nil {
		t.Fatalf("list vault values: %v", err)
	}
	return vals
}

// A snapshot restores the vault rows together with the master key, so every secret still decrypts.
func TestSnapshotRestoresVaultSecretsWithMasterKey(t *testing.T) {
	t.Parallel()
	src := newVaultTestDB(t)
	if _, err := src.CreateVaultSecret(&vault.Secret{SecretMeta: vault.SecretMeta{Key: "DB_PASS", Scope: vault.ScopeGlobal}, Value: "s3cret"}); err != nil {
		t.Fatal(err)
	}
	payload, err := src.ExportSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.VaultSecrets) != 1 {
		t.Fatalf("expected 1 vault secret in snapshot, got %d", len(payload.VaultSecrets))
	}

	dst := newVaultTestDB(t)
	if _, err := dst.CreateVaultSecret(&vault.Secret{SecretMeta: vault.SecretMeta{Key: "OLD", Scope: vault.ScopeGlobal}, Value: "old"}); err != nil {
		t.Fatal(err)
	}
	if err := dst.ImportSnapshot(payload); err != nil {
		t.Fatal(err)
	}
	vals := vaultValues(t, dst)
	if len(vals) != 1 || vals[0] != "s3cret" {
		t.Fatalf("restored vault values = %v, want [s3cret]", vals)
	}
}

// A legacy snapshot (no vault section) must not replace the local master key under local secrets.
func TestLegacySnapshotKeepsLocalMasterKey(t *testing.T) {
	t.Parallel()
	src := newVaultTestDB(t)
	if _, err := src.CreateVaultSecret(&vault.Secret{SecretMeta: vault.SecretMeta{Key: "A", Scope: vault.ScopeGlobal}, Value: "from-src"}); err != nil {
		t.Fatal(err)
	}
	payload, err := src.ExportSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	payload.VaultIncluded, payload.VaultSecrets, payload.VaultVersions = false, nil, nil

	dst := newVaultTestDB(t)
	if _, err := dst.CreateVaultSecret(&vault.Secret{SecretMeta: vault.SecretMeta{Key: "B", Scope: vault.ScopeGlobal}, Value: "local"}); err != nil {
		t.Fatal(err)
	}
	if err := dst.ImportSnapshot(payload); err != nil {
		t.Fatal(err)
	}
	if vals := vaultValues(t, dst); len(vals) != 1 || vals[0] != "local" {
		t.Fatalf("local secret no longer decrypts after legacy restore: %v", vals)
	}
}

// A section that cannot be read must fail the export, not produce a snapshot missing that section.
func TestSnapshotExportFailsWhenASectionCannotBeRead(t *testing.T) {
	t.Parallel()
	db := newVaultTestDB(t)
	if _, err := db.Exec(`DROP TABLE ssh_keys`); err != nil {
		t.Fatal(err)
	}
	if payload, err := db.ExportSnapshot(); err == nil {
		t.Fatalf("export succeeded with ssh_keys unreadable: %d keys", len(payload.SSHKeys))
	}
}
