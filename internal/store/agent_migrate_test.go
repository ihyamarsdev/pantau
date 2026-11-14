package store

import (
	"path/filepath"
	"testing"
)

func TestMigrateAgentSettings(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
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
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
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
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
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
