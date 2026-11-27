package web

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"pantau/internal/store"
)

// templateDB is a migrated database built once per test binary. Tests get a copy, so the
// migration, the RSA-4096 key and the admin seed are not repeated for every test server.
var templateDB string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "pantau-web-template-")
	if err != nil {
		panic(err)
	}
	templateDB = filepath.Join(dir, "template.db")
	db, err := store.Open(templateDB)
	if err != nil {
		os.RemoveAll(dir)
		panic("build template db: " + err.Error())
	}
	// Test-only: the admin hash uses bcrypt.MinCost so each login in the suite is cheap.
	// Production cost is asserted by store's TestOpenUsesProductionKeyParams.
	hash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.MinCost)
	if err == nil {
		err = db.SetSetting("admin_password_hash", string(hash))
	}
	db.Close()
	if err != nil {
		os.RemoveAll(dir)
		panic("seed template admin hash: " + err.Error())
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// testDBCopy returns a private copy of the template database inside t.TempDir().
func testDBCopy(t testing.TB) string {
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
