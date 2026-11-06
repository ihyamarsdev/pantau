package store

import (
	"path/filepath"
	"testing"
)

func TestGetSettingCacheInvalidatedOnWrites(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	get := func() string {
		t.Helper()
		v, err := db.GetSetting("poll_interval_sec")
		if err != nil {
			t.Fatal(err)
		}
		return v
	}

	if v := get(); v != "" {
		t.Fatalf("unset setting = %q, want empty", v)
	}
	_ = db.SetSetting("poll_interval_sec", "60")
	if v := get(); v != "60" {
		t.Fatalf("after set = %q, want 60", v)
	}
	_ = db.SetSetting("poll_interval_sec", "120") // warm cache must not serve the old value
	if v := get(); v != "120" {
		t.Fatalf("after overwrite = %q, want 120", v)
	}
	_ = db.DeleteSetting("poll_interval_sec")
	if v := get(); v != "" {
		t.Fatalf("after delete = %q, want empty", v)
	}
}
