package store

// The AI agent of the first version stored its settings under other key names and had a transport token.
// The native agent (in-process) uses agent_enabled and agent_llm_*; the old rows are obsolete and dropped.
var obsoleteAgentSettings = []string{"hermes_url", "hermes_key", "hermes_model", "agent_mcp_token"}

// migrateAgentSettings renames the old enabled flag and drops the obsolete rows. It is idempotent: an
// operator who already has agent_enabled keeps it, and a second run changes nothing.
func (d *DB) migrateAgentSettings() error {
	defer d.settingsCache.Clear()
	get := func(k string) (string, bool) {
		var v string
		if err := d.QueryRow(`SELECT value FROM settings WHERE key = ?`, k).Scan(&v); err != nil {
			return "", false
		}
		return v, true
	}
	if old, ok := get("hermes_enabled"); ok {
		if _, has := get("agent_enabled"); !has {
			if _, err := d.Exec(`INSERT INTO settings(key, value) VALUES ('agent_enabled', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, old); err != nil {
				return err
			}
		}
		if _, err := d.Exec(`DELETE FROM settings WHERE key = 'hermes_enabled'`); err != nil {
			return err
		}
	}
	for _, k := range obsoleteAgentSettings {
		if _, err := d.Exec(`DELETE FROM settings WHERE key = ?`, k); err != nil {
			return err
		}
	}
	return nil
}

// migrateAgentTriage creates the tables for the token-efficient proactive triage
// worker and the delta-only daily briefing. Idempotent: CREATE TABLE IF NOT EXISTS
// and a unique signature/day key make a second run a no-op.
func (d *DB) migrateAgentTriage() error {
	schema := `
	CREATE TABLE IF NOT EXISTS agent_triage (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		signature TEXT NOT NULL,
		alert_id INTEGER NOT NULL DEFAULT 0,
		host_id INTEGER NOT NULL DEFAULT 0,
		alert_type TEXT NOT NULL DEFAULT '',
		unit TEXT NOT NULL DEFAULT '',
		source TEXT NOT NULL DEFAULT 'llm',
		root_cause TEXT NOT NULL DEFAULT '',
		impact TEXT NOT NULL DEFAULT '',
		remediation TEXT NOT NULL DEFAULT '',
		remediation_action TEXT NOT NULL DEFAULT '',
		tokens_used INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_agent_triage_signature ON agent_triage(signature, created_at);
	CREATE INDEX IF NOT EXISTS idx_agent_triage_alert ON agent_triage(alert_id, id DESC);

	CREATE TABLE IF NOT EXISTS agent_briefing (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		day TEXT NOT NULL UNIQUE,
		summary TEXT NOT NULL,
		anomaly_count INTEGER NOT NULL DEFAULT 0,
		items_json TEXT NOT NULL DEFAULT '[]',
		tokens_used INTEGER NOT NULL DEFAULT 0,
		generated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS agent_triage_budget (
		day TEXT NOT NULL PRIMARY KEY,
		tokens_used INTEGER NOT NULL DEFAULT 0
	);
	`
	if _, err := d.Exec(schema); err != nil {
		return err
	}
	_ = d.alterAddColumn("agent_triage", "unit", "TEXT NOT NULL DEFAULT ''")
	return nil
}
