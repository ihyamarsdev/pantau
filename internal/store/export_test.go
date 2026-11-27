package store

import (
	"os"
	"path/filepath"
	"testing"
)

// templateDB is a fully migrated database built once per test binary through the production Open path,
// so the RSA-4096 keypair and bcrypt admin hash are generated once instead of on every test.
var templateDB string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "pantau-store-template-")
	if err != nil {
		panic(err)
	}
	templateDB = filepath.Join(dir, "template.db")
	db, err := Open(templateDB)
	if err != nil {
		os.RemoveAll(dir)
		panic("build template db: " + err.Error())
	}
	db.Close() // checkpoints the WAL, so the single file is a complete database
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// TemplateCopy returns a private copy of the migrated template database inside t.TempDir().
// Open(path) on the copy skips migration and key generation because the seeded rows already exist.
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
