package snapshot

import (
	"os"
	"path/filepath"
	"testing"

	"pantau/internal/store"
)

// templateDB is one fully migrated database per test binary. Opening a fresh file generates an RSA-4096
// keypair and a bcrypt admin hash, which dominated this package's runtime, so tests open a copy instead.
var templateDB string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "pantau-snapshot-template-")
	if err != nil {
		panic(err)
	}
	templateDB = filepath.Join(dir, "template.db")
	db, err := store.Open(templateDB)
	if err != nil {
		os.RemoveAll(dir)
		panic("build template db: " + err.Error())
	}
	db.Close() // checkpoints the WAL so the single file is a complete database
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// TemplateCopy returns a private copy of the migrated template database inside t.TempDir().
func TemplateCopy(t testing.TB) string {
	t.Helper()
	data, err := os.ReadFile(templateDB)
	if err != nil {
		t.Fatalf("read template db: %v", err)
	}
	path := filepath.Join(t.TempDir(), "test.db")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write db copy: %v", err)
	}
	return path
}
