package store

// Vault persistence: encrypted secrets with version history, plus templates,
// sync targets and an audit trail. All plaintext lives only in Go memory; the
// database stores AES-GCM ciphertext produced by internal/vault.

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"pantau/internal/vault"
)

const vaultMasterKeySetting = "vault_master_key"

// Serializes first-use master key creation so two concurrent callers cannot
// generate different keys for the same empty settings row.
var vaultKeyMu sync.Mutex

// VaultMasterKey returns the stored 32-byte master key, generating and
// persisting one on first use.
func (d *DB) VaultMasterKey() ([]byte, error) {
	vaultKeyMu.Lock()
	defer vaultKeyMu.Unlock()

	v, err := d.GetSetting(vaultMasterKeySetting)
	if err != nil {
		return nil, err
	}
	if v != "" {
		return vault.MasterKeyFromB64(v)
	}
	key, err := vault.NewMasterKey()
	if err != nil {
		return nil, err
	}
	if err := d.SetSetting(vaultMasterKeySetting, vault.MasterKeyToB64(key)); err != nil {
		return nil, err
	}
	return key, nil
}

func (d *DB) vaultCipher() (*vault.Cipher, error) {
	key, err := d.VaultMasterKey()
	if err != nil {
		return nil, err
	}
	return vault.NewCipher(key)
}

// nullableTime turns an optional timestamp into a driver value (nil -> NULL).
func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

// vaultNeedsRotation flags expired secrets and secrets older than their
// rotation window (measured from the last update, falling back to creation).
func vaultNeedsRotation(m *vault.SecretMeta) {
	now := time.Now()
	if m.ExpiresAt != nil && m.ExpiresAt.Before(now) {
		m.NeedsRotation = true
		return
	}
	if m.RotationDays > 0 {
		ref := m.UpdatedAt
		if ref.IsZero() {
			ref = m.CreatedAt
		}
		if !ref.IsZero() && now.Sub(ref) >= time.Duration(m.RotationDays)*24*time.Hour {
			m.NeedsRotation = true
		}
	}
}

const (
	vaultSecretMetaCols  = `id, key, scope, scope_ref, current_version, expires_at, rotation_days, created_at, updated_at`
	vaultSecretFullCols  = `id, key, scope, scope_ref, ciphertext, current_version, expires_at, rotation_days, created_at, updated_at`
	vaultTargetCols      = `id, name, kind, template_id, target_ref, path, namespace, post_sync_hook, last_sync_sha, last_sync_at, drift_status, created_at`
	vaultTemplateCols    = `id, name, body, created_at, updated_at`
)

func scanVaultSecretMeta(sc rowScanner) (vault.SecretMeta, error) {
	var m vault.SecretMeta
	var scope string
	var expires sql.NullTime
	err := sc.Scan(&m.ID, &m.Key, &scope, &m.ScopeRef, &m.Version, &expires, &m.RotationDays, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return m, err
	}
	m.Scope = vault.Scope(scope)
	if expires.Valid {
		m.ExpiresAt = &expires.Time
	}
	vaultNeedsRotation(&m)
	return m, nil
}

func scanVaultTarget(sc rowScanner) (vault.SyncTarget, error) {
	var t vault.SyncTarget
	var lastSync sql.NullTime
	err := sc.Scan(&t.ID, &t.Name, &t.Kind, &t.TemplateID, &t.TargetRef, &t.Path, &t.Namespace, &t.PostSyncHook, &t.LastSyncSHA, &lastSync, &t.DriftStatus, &t.CreatedAt)
	if err != nil {
		return t, err
	}
	if lastSync.Valid {
		t.LastSyncAt = &lastSync.Time
	}
	return t, nil
}

// ---------------------------------------------------------------------------
// Secrets
// ---------------------------------------------------------------------------

func (d *DB) CreateVaultSecret(s *vault.Secret) (int64, error) {
	c, err := d.vaultCipher()
	if err != nil {
		return 0, err
	}
	ct, err := c.Encrypt([]byte(s.Value))
	if err != nil {
		return 0, err
	}
	version := s.Version
	if version <= 0 {
		version = 1
	}
	now := time.Now().UTC()

	tx, err := d.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT INTO vault_secrets(key, scope, scope_ref, ciphertext, current_version, expires_at, rotation_days, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		s.Key, string(s.Scope), s.ScopeRef, ct, version, nullableTime(s.ExpiresAt), s.RotationDays, now, now)
	if err != nil {
		return 0, fmt.Errorf("create vault secret: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`INSERT INTO vault_versions(secret_id, version, ciphertext, created_at) VALUES (?,?,?,?)`, id, version, ct, now); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	s.ID, s.Version, s.CreatedAt, s.UpdatedAt = id, version, now, now
	return id, nil
}

func (d *DB) UpdateVaultSecret(id int64, value string, expiresAt *time.Time, rotationDays int) error {
	c, err := d.vaultCipher()
	if err != nil {
		return err
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var oldCT []byte
	var oldVersion int
	if err := tx.QueryRow(`SELECT ciphertext, current_version FROM vault_secrets WHERE id = ?`, id).Scan(&oldCT, &oldVersion); err != nil {
		return err
	}
	newVersion := oldVersion + 1
	now := time.Now().UTC()

	// Snapshot the old ciphertext at its own version (idempotent in case a
	// previous crash left the row behind).
	if _, err := tx.Exec(`INSERT INTO vault_versions(secret_id, version, ciphertext, created_at) VALUES (?,?,?,?) ON CONFLICT(secret_id,version) DO NOTHING`, id, oldVersion, oldCT, now); err != nil {
		return err
	}
	newCT, err := c.Encrypt([]byte(value))
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE vault_secrets SET ciphertext=?, current_version=?, expires_at=?, rotation_days=?, updated_at=? WHERE id=?`,
		newCT, newVersion, nullableTime(expiresAt), rotationDays, now, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO vault_versions(secret_id, version, ciphertext, created_at) VALUES (?,?,?,?)`, id, newVersion, newCT, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) getVaultSecret(where string, arg any) (*vault.Secret, error) {
	c, err := d.vaultCipher()
	if err != nil {
		return nil, err
	}
	var s vault.Secret
	var scope string
	var ct []byte
	var expires sql.NullTime
	err = d.QueryRow(`SELECT `+vaultSecretFullCols+` FROM vault_secrets WHERE `+where, arg).
		Scan(&s.ID, &s.Key, &scope, &s.ScopeRef, &ct, &s.Version, &expires, &s.RotationDays, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	s.Scope = vault.Scope(scope)
	if expires.Valid {
		s.ExpiresAt = &expires.Time
	}
	plain, err := c.Decrypt(ct)
	if err != nil {
		return nil, err
	}
	s.Value = string(plain)
	vaultNeedsRotation(&s.SecretMeta)
	return &s, nil
}

func (d *DB) GetVaultSecret(id int64) (*vault.Secret, error) {
	return d.getVaultSecret("id = ?", id)
}

func (d *DB) GetVaultSecretByKey(key string) (*vault.Secret, error) {
	// The key column is COLLATE NOCASE, so this lookup is case-insensitive.
	return d.getVaultSecret("key = ?", key)
}

func (d *DB) ListVaultSecrets() ([]vault.SecretMeta, error) {
	rows, err := d.Query(`SELECT ` + vaultSecretMetaCols + ` FROM vault_secrets ORDER BY key COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []vault.SecretMeta{}
	for rows.Next() {
		m, err := scanVaultSecretMeta(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (d *DB) ListVaultSecretValues() ([]string, error) {
	c, err := d.vaultCipher()
	if err != nil {
		return nil, err
	}
	rows, err := d.Query(`SELECT ciphertext FROM vault_secrets`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	vals := []string{}
	for rows.Next() {
		var ct []byte
		if err := rows.Scan(&ct); err != nil {
			return nil, err
		}
		plain, err := c.Decrypt(ct)
		if err != nil {
			return nil, err
		}
		vals = append(vals, string(plain))
	}
	return vals, rows.Err()
}

func (d *DB) DeleteVaultSecret(id int64) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM vault_versions WHERE secret_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM vault_secrets WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) ListVaultVersions(secretID int64) ([]vault.Version, error) {
	rows, err := d.Query(`SELECT version, created_at FROM vault_versions WHERE secret_id = ? ORDER BY version`, secretID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []vault.Version{}
	for rows.Next() {
		var v vault.Version
		if err := rows.Scan(&v.Version, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (d *DB) RollbackVaultSecret(id int64, version int) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var ct []byte
	if err := tx.QueryRow(`SELECT ciphertext FROM vault_versions WHERE secret_id = ? AND version = ?`, id, version).Scan(&ct); err != nil {
		return err
	}
	var current int
	if err := tx.QueryRow(`SELECT current_version FROM vault_secrets WHERE id = ?`, id).Scan(&current); err != nil {
		return err
	}
	newVersion := current + 1
	now := time.Now().UTC()
	if _, err := tx.Exec(`UPDATE vault_secrets SET ciphertext = ?, current_version = ?, updated_at = ? WHERE id = ?`, ct, newVersion, now, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO vault_versions(secret_id, version, ciphertext, created_at) VALUES (?,?,?,?)`, id, newVersion, ct, now); err != nil {
		return err
	}
	return tx.Commit()
}

// ---------------------------------------------------------------------------
// Templates
// ---------------------------------------------------------------------------

func (d *DB) CreateVaultTemplate(t *vault.Template) (int64, error) {
	now := time.Now().UTC()
	res, err := d.Exec(`INSERT INTO vault_templates(name, body, created_at, updated_at) VALUES (?,?,?,?)`, t.Name, t.Body, now, now)
	if err != nil {
		return 0, fmt.Errorf("create vault template: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	t.ID, t.CreatedAt, t.UpdatedAt = id, now, now
	return id, nil
}

func (d *DB) ListVaultTemplates() ([]vault.Template, error) {
	rows, err := d.Query(`SELECT ` + vaultTemplateCols + ` FROM vault_templates ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []vault.Template{}
	for rows.Next() {
		var t vault.Template
		if err := rows.Scan(&t.ID, &t.Name, &t.Body, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (d *DB) GetVaultTemplate(id int64) (*vault.Template, error) {
	var t vault.Template
	err := d.QueryRow(`SELECT `+vaultTemplateCols+` FROM vault_templates WHERE id = ?`, id).
		Scan(&t.ID, &t.Name, &t.Body, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (d *DB) UpdateVaultTemplate(t *vault.Template) error {
	now := time.Now().UTC()
	res, err := d.Exec(`UPDATE vault_templates SET name = ?, body = ?, updated_at = ? WHERE id = ?`, t.Name, t.Body, now, t.ID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	t.UpdatedAt = now
	return nil
}

func (d *DB) DeleteVaultTemplate(id int64) error {
	_, err := d.Exec(`DELETE FROM vault_templates WHERE id = ?`, id)
	return err
}

// ---------------------------------------------------------------------------
// Sync targets
// ---------------------------------------------------------------------------

func (d *DB) CreateVaultTarget(t *vault.SyncTarget) (int64, error) {
	now := time.Now().UTC()
	drift := t.DriftStatus
	if drift == "" {
		drift = "unknown"
	}
	res, err := d.Exec(`INSERT INTO vault_targets(name, kind, template_id, target_ref, path, namespace, post_sync_hook, last_sync_sha, last_sync_at, drift_status, created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		t.Name, t.Kind, t.TemplateID, t.TargetRef, t.Path, t.Namespace, t.PostSyncHook, t.LastSyncSHA, nullableTime(t.LastSyncAt), drift, now)
	if err != nil {
		return 0, fmt.Errorf("create vault target: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	t.ID, t.DriftStatus, t.CreatedAt = id, drift, now
	return id, nil
}

func (d *DB) ListVaultTargets() ([]vault.SyncTarget, error) {
	rows, err := d.Query(`SELECT ` + vaultTargetCols + ` FROM vault_targets ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []vault.SyncTarget{}
	for rows.Next() {
		t, err := scanVaultTarget(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (d *DB) GetVaultTarget(id int64) (*vault.SyncTarget, error) {
	t, err := scanVaultTarget(d.QueryRow(`SELECT `+vaultTargetCols+` FROM vault_targets WHERE id = ?`, id))
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (d *DB) UpdateVaultTarget(t *vault.SyncTarget) error {
	res, err := d.Exec(`UPDATE vault_targets SET name = ?, kind = ?, template_id = ?, target_ref = ?, path = ?, namespace = ?, post_sync_hook = ? WHERE id = ?`,
		t.Name, t.Kind, t.TemplateID, t.TargetRef, t.Path, t.Namespace, t.PostSyncHook, t.ID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (d *DB) DeleteVaultTarget(id int64) error {
	_, err := d.Exec(`DELETE FROM vault_targets WHERE id = ?`, id)
	return err
}

func (d *DB) UpdateVaultTargetSync(id int64, sha, drift string, at time.Time) error {
	_, err := d.Exec(`UPDATE vault_targets SET last_sync_sha = ?, drift_status = ?, last_sync_at = ? WHERE id = ?`, sha, drift, at, id)
	return err
}

// ---------------------------------------------------------------------------
// Audit
// ---------------------------------------------------------------------------

func (d *DB) AddVaultAudit(action, detail string) error {
	_, err := d.Exec(`INSERT INTO vault_audit(ts, action, detail) VALUES (?,?,?)`, time.Now().UTC(), action, detail)
	return err
}

func (d *DB) ListVaultAudit() ([]vault.AuditEntry, error) {
	rows, err := d.Query(`SELECT id, ts, action, detail FROM vault_audit ORDER BY ts DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []vault.AuditEntry{}
	for rows.Next() {
		var a vault.AuditEntry
		if err := rows.Scan(&a.ID, &a.TS, &a.Action, &a.Detail); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
