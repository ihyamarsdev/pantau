package store_test

import (
	"errors"
	"testing"

	"pantau/internal/store"
)

func TestS3Accounts_CRUD(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	id, err := db.CreateS3Account(&store.S3Account{Name: "aws", Bucket: "b1", AccessKey: "ak", SecretKey: "sk"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateS3Account(&store.S3Account{Name: "AWS", Bucket: "b2"}); !errors.Is(err, store.ErrS3AccountNameTaken) {
		t.Fatalf("expected name-taken error, got %v", err)
	}
	if missing, err := db.GetS3Account(999); err != nil || missing != nil {
		t.Fatalf("missing account must be (nil, nil), got %v %v", missing, err)
	}

	// Empty SecretKey on update keeps the stored one.
	if err := db.UpdateS3Account(&store.S3Account{ID: id, Name: "aws2", Bucket: "b9", AccessKey: "ak2"}); err != nil {
		t.Fatal(err)
	}
	got, _ := db.GetS3Account(id)
	if got.SecretKey != "sk" || got.Name != "aws2" || got.Bucket != "b9" {
		t.Fatalf("unexpected after update: %+v", got)
	}
	if err := db.DeleteS3Account(id); err != nil {
		t.Fatal(err)
	}
	if list, _ := db.ListS3Accounts(); len(list) != 0 {
		t.Fatalf("expected empty list, got %d", len(list))
	}
}

// A database (or snapshot) from the single-account version keeps its credentials as an account named "Default".
func TestS3Accounts_MigratesLegacySettings(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	for k, v := range map[string]string{
		"s3_endpoint": "https://minio.local", "s3_bucket": "legacy", "s3_access_key": "ak", "s3_secret_key": "sk", "s3_prefix": "p/", "s3_region": "eu",
	} {
		_ = db.SetSetting(k, v)
	}

	// A snapshot taken now carries the legacy settings and no accounts; importing it must migrate them.
	snap, err := db.ExportSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ImportSnapshot(snap); err != nil {
		t.Fatal(err)
	}
	list, _ := db.ListS3Accounts()
	if len(list) != 1 || list[0].Name != "Default" || list[0].Bucket != "legacy" || list[0].SecretKey != "sk" || list[0].Endpoint != "https://minio.local" || list[0].Prefix != "p/" {
		t.Fatalf("legacy settings not migrated: %+v", list)
	}
	if v, _ := db.GetSetting("s3_secret_key"); v != "" {
		t.Fatal("legacy secret must be removed from settings after migration")
	}
}

func TestS3Accounts_SurviveSnapshotRoundTrip(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	_, _ = db.CreateS3Account(&store.S3Account{Name: "a", Bucket: "b1", AccessKey: "ak", SecretKey: "sk1"})
	_, _ = db.CreateS3Account(&store.S3Account{Name: "b", Bucket: "b2", AccessKey: "ak", SecretKey: "sk2", Prefix: "x/"})

	snap, err := db.ExportSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	_ = db.DeleteS3Account(1)
	if err := db.ImportSnapshot(snap); err != nil {
		t.Fatal(err)
	}
	list, _ := db.ListS3Accounts()
	if len(list) != 2 || list[0].SecretKey != "sk1" || list[1].SecretKey != "sk2" || list[1].Prefix != "x/" {
		t.Fatalf("accounts lost in snapshot round trip: %+v", list)
	}
}
