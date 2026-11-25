package store

import "testing"

func TestTrustedHostKeysInsertGetDelete(t *testing.T) {
	db := newVaultTestDB(t)
	addr := "10.0.0.5:22"

	if _, ok, err := db.GetTrustedHostKey(addr); err != nil || ok {
		t.Fatalf("unexpected key before insert: ok=%v err=%v", ok, err)
	}
	if inserted, err := db.InsertTrustedHostKey(addr, "ssh-ed25519 AAA first"); err != nil || !inserted {
		t.Fatalf("first insert: inserted=%v err=%v", inserted, err)
	}
	// The first key wins: a later insert for the same host must not replace it.
	if inserted, err := db.InsertTrustedHostKey(addr, "ssh-ed25519 BBB second"); err != nil || inserted {
		t.Fatalf("second insert replaced the trusted key: inserted=%v err=%v", inserted, err)
	}
	if key, ok, _ := db.GetTrustedHostKey(addr); !ok || key != "ssh-ed25519 AAA first" {
		t.Fatalf("trusted key = %q ok=%v", key, ok)
	}
	if err := db.DeleteTrustedHostKey(addr); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := db.GetTrustedHostKey(addr); ok {
		t.Fatal("key still trusted after delete")
	}
}
