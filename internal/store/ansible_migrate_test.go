package store_test

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"pantau/internal/snapshot"
	"pantau/internal/store"
)

func testFolderZIP(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func columns(t *testing.T, db *store.DB) map[string]bool {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM pragma_table_info('ansible_playbooks')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		out[n] = true
	}
	return out
}

func TestImportAnsiblePlaybooksAllOrNothing(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	a := &store.AnsiblePlaybook{Name: "A", Content: "- hosts: all\n"}
	b := &store.AnsiblePlaybook{Name: "B", Content: "- hosts: db\n", ExtraVars: `{"x":1}`}
	if err := db.ImportAnsiblePlaybooks([]*store.AnsiblePlaybook{a, b}); err != nil {
		t.Fatal(err)
	}
	if a.ID == 0 || b.ID == 0 || a.ID == b.ID || a.Source != "inline" {
		t.Fatalf("playbooks not created as inline: %+v %+v", a, b)
	}
	got, err := db.GetAnsiblePlaybook(b.ID)
	if err != nil || got.Content != b.Content || got.ExtraVars != `{"x":1}` {
		t.Fatalf("stored playbook differs: %+v %v", got, err)
	}
	err = db.ImportAnsiblePlaybooks([]*store.AnsiblePlaybook{{Name: "Fresh", Content: "x"}, {Name: "b", Content: "y"}})
	if !errors.Is(err, store.ErrAnsiblePlaybookNameTaken) || !strings.Contains(err.Error(), `"b"`) {
		t.Fatalf("duplicate name: %v", err)
	}
	if all, _ := db.ListAnsiblePlaybooks(); len(all) != 2 {
		t.Fatalf("a failed import must create nothing, got %d playbooks", len(all))
	}
}

func TestMigrateImportedFoldersToInline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	folder := testFolderZIP(t, map[string]string{"playbooks/site.yml": "- hosts: all\n  roles: [web]\n", "roles/web/tasks/main.yml": "[]"})
	_, err = legacy.Exec(`CREATE TABLE ansible_playbooks (
		id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE COLLATE NOCASE,
		description TEXT NOT NULL DEFAULT '', content TEXT NOT NULL DEFAULT '', extra_vars TEXT NOT NULL DEFAULT '',
		source TEXT NOT NULL DEFAULT 'inline', bundle_path TEXT NOT NULL DEFAULT '', bundle_data BLOB NOT NULL DEFAULT X'',
		project_id INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE ansible_projects (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, data BLOB, created_at DATETIME, updated_at DATETIME);
	INSERT INTO ansible_playbooks (name, content) VALUES ('Inline', '- hosts: inline');`)
	if err == nil { // shared project, the oldest per-playbook archive, and an entry that is missing from its archive
		_, err = legacy.Exec(`INSERT INTO ansible_projects (id, name, data) VALUES (1, 'Folder', ?)`, folder)
	}
	if err == nil {
		_, err = legacy.Exec(`INSERT INTO ansible_playbooks (name, source, bundle_path, project_id) VALUES ('Shared', 'bundle', 'playbooks/site.yml', 1)`)
	}
	if err == nil {
		_, err = legacy.Exec(`INSERT INTO ansible_playbooks (name, source, bundle_path, bundle_data) VALUES ('Oldest', 'bundle', 'playbooks/site.yml', ?)`, folder)
	}
	if err == nil {
		_, err = legacy.Exec(`INSERT INTO ansible_playbooks (name, source, bundle_path, project_id) VALUES ('Broken', 'bundle', 'gone.yml', 1)`)
	}
	if closeErr := legacy.Close(); err != nil || closeErr != nil {
		t.Fatalf("prepare legacy database: exec=%v close=%v", err, closeErr)
	}
	for attempt := 0; attempt < 2; attempt++ { // the second open proves the migration is idempotent
		func() {
			db, err := store.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			byName := map[string]*store.AnsiblePlaybook{}
			all, err := db.ListAnsiblePlaybooks()
			if err != nil || len(all) != 4 {
				t.Fatalf("playbooks lost in migration: %d %v", len(all), err)
			}
			for _, p := range all {
				full, err := db.GetAnsiblePlaybook(p.ID)
				if err != nil {
					t.Fatal(err)
				}
				byName[full.Name] = full
				if full.Source != "inline" {
					t.Errorf("%s: source %q, want inline", full.Name, full.Source)
				}
			}
			if byName["Inline"].Content != "- hosts: inline" {
				t.Errorf("an inline playbook must stay untouched: %q", byName["Inline"].Content)
			}
			for _, name := range []string{"Shared", "Oldest"} {
				if byName[name].Content != "- hosts: all\n  roles: [web]\n" {
					t.Errorf("%s must hold the entry YAML of its folder, got %q", name, byName[name].Content)
				}
			}
			if c := byName["Broken"].Content; !strings.HasPrefix(c, "#") || !strings.Contains(c, "gone.yml") {
				t.Errorf("an unreadable entry becomes an explanatory comment, got %q", c)
			}
			cols := columns(t, db)
			for _, legacyCol := range []string{"bundle_path", "bundle_data", "project_id"} {
				if cols[legacyCol] {
					t.Errorf("column %s must be dropped", legacyCol)
				}
			}
			var tables int
			if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'ansible_projects'`).Scan(&tables); err != nil || tables != 0 {
				t.Errorf("ansible_projects must be dropped: %d %v", tables, err)
			}
		}()
	}
}

func TestRestoreLegacyBundleSnapshots(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	folder := testFolderZIP(t, map[string]string{"site.yml": "- hosts: all\n"})
	err := db.ImportSnapshot(&store.SnapshotPayload{Version: 1,
		AnsibleProjects: []store.SnapshotAnsibleProject{{ID: 3, Name: "Folder", Data: folder}},
		AnsiblePlaybooks: []store.SnapshotAnsiblePlaybook{
			{ID: 5, Name: "PerPlaybook", Source: "bundle", BundlePath: "site.yml", BundleData: folder},
			{ID: 6, Name: "InProject", Source: "bundle", BundlePath: "site.yml", ProjectID: 3},
			{ID: 7, Name: "Plain", Content: "- hosts: plain\n"},
		}})
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[int64]string{5: "- hosts: all\n", 6: "- hosts: all\n", 7: "- hosts: plain\n"} {
		got, err := db.GetAnsiblePlaybook(id)
		if err != nil || got.Source != "inline" || got.Content != want {
			t.Errorf("playbook %d: %+v %v", id, got, err)
		}
	}
	payload, err := db.ExportSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	enc, err := snapshot.Encrypt(payload, "test-only-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(enc, folder) || len(payload.AnsibleProjects) != 0 {
		t.Fatal("exports no longer carry folder archives")
	}
	dec, err := snapshot.Decrypt(enc, "test-only-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	dst, cleanupDst := setupTestDB(t)
	defer cleanupDst()
	if err := dst.ImportSnapshot(dec); err != nil {
		t.Fatal(err)
	}
	if got, err := dst.GetAnsiblePlaybook(5); err != nil || got.Content != "- hosts: all\n" || got.ExtraVars != "" {
		t.Fatalf("roundtrip changed the playbook: %+v %v", got, err)
	}
}
