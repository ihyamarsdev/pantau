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
