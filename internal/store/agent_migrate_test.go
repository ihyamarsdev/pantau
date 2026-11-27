package store

import (
	"path/filepath"
	"testing"
)

func TestMigrateAgentSettings(t *testing.T) {
	t.Parallel()
	db, err := Open(TemplateCopy(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for k, v := range map[string]string{
		"hermes_enabled":  "1",
		"hermes_url":      "http://127.0.0.1:8642",
		"hermes_key":      "old-secret",
		"hermes_model":    "old-model",
		"agent_mcp_token": "tok",
	} {
		if err := db.SetSetting(k, v); err != nil {
			t.Fatal(err)
		}
	}

	if err := db.migrateAgentSettings(); err != nil {
		t.Fatal(err)
	}
	if v, _ := db.GetSetting("agent_enabled"); v != "1" {
		t.Fatalf("agent_enabled = %q, want 1", v)
	}
	for _, k := range append([]string{"hermes_enabled"}, obsoleteAgentSettings...) {
		if v, _ := db.GetSetting(k); v != "" {
			t.Fatalf("%s must be gone, got %q", k, v)
		}
	}

	// A second run changes nothing.
	if err := db.migrateAgentSettings(); err != nil {
		t.Fatal(err)
	}
	if v, _ := db.GetSetting("agent_enabled"); v != "1" {
		t.Fatalf("agent_enabled after the second run = %q, want 1", v)
	}
}

func TestMigrateAgentSettingsKeepsExistingEnabled(t *testing.T) {
	t.Parallel()
	db, err := Open(TemplateCopy(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_ = db.SetSetting("hermes_enabled", "1")
	_ = db.SetSetting("agent_enabled", "0")
	if err := db.migrateAgentSettings(); err != nil {
		t.Fatal(err)
	}
	if v, _ := db.GetSetting("agent_enabled"); v != "0" {
		t.Fatalf("agent_enabled = %q, want the stored 0", v)
	}
	if v, _ := db.GetSetting("hermes_enabled"); v != "" {
		t.Fatalf("hermes_enabled must be gone, got %q", v)
	}
}

func TestMigrateAgentSettingsWithoutOldKeys(t *testing.T) {
	t.Parallel()
	db, err := Open(TemplateCopy(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_ = db.SetSetting("agent_llm_provider", "openai")
	if err := db.migrateAgentSettings(); err != nil {
		t.Fatal(err)
	}
	if v, _ := db.GetSetting("agent_llm_provider"); v != "openai" {
		t.Fatalf("the provider must survive: %q", v)
	}
	if v, _ := db.GetSetting("agent_enabled"); v != "" {
		t.Fatalf("agent_enabled must stay unset: %q", v)
	}
}

// A database created before the scope columns existed gains them on the next Open, and its notes keep working:
// the columns are additive, so an old note stays readable and a scoped one can be added afterwards.
func TestMigrateAgentMemoryScopeColumns(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// Rebuild agent_memory without the two columns, which is the schema a database from before them has.
	for _, q := range []string{
		`CREATE TABLE agent_memory_old AS SELECT id, host_id, content, source, created_at, updated_at FROM agent_memory`,
		`DROP TABLE agent_memory`,
		`CREATE TABLE agent_memory (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			host_id INTEGER REFERENCES hosts(id) ON DELETE CASCADE,
			content TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT 'agent',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`INSERT INTO agent_memory (id, host_id, content, source, created_at, updated_at)
			SELECT id, host_id, content, source, created_at, updated_at FROM agent_memory_old`,
		`DROP TABLE agent_memory_old`,
		`INSERT INTO agent_memory (content, source) VALUES ('old note', 'agent')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	db.Close()

	// Reopening runs migrate(), which adds the columns without touching the row.
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen with an old agent_memory table: %v", err)
	}
	defer db2.Close()

	notes, err := db2.ListAgentMemory(0, "")
	if err != nil {
		t.Fatalf("the old note must still be readable: %v", err)
	}
	if len(notes) != 1 || notes[0].Content != "old note" {
		t.Fatalf("notes = %+v, want the one old note", notes)
	}
	if notes[0].ClusterID != nil || notes[0].Domain != "" {
		t.Errorf("an old note must read as unscoped: %+v", notes[0])
	}
	if _, err := db2.AddAgentMemoryScoped(AgentMemoryScope{Domain: "security"}, "new scoped note", "agent"); err != nil {
		t.Fatalf("add a scoped note after the migration: %v", err)
	}
}
