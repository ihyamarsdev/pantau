package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// TFCloudAccount holds provider credentials injected as env into the Terraform runs of linked workspaces.
type TFCloudAccount struct {
	ID          int64             `json:"id"`
	Name        string            `json:"name"`
	Provider    string            `json:"provider"`
	Description string            `json:"description"`
	Fields      map[string]string `json:"-"` // ALL provider fields incl. secrets; plaintext in SQLite like cloudflare_accounts.api_token
	VerifiedAt  *time.Time        `json:"verified_at"`
	VerifyOK    bool              `json:"verify_ok"`
	VerifyMsg   string            `json:"verify_message"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

var ErrTFCloudAccountNameTaken = errors.New("a cloud account with this name already exists")

func marshalCloudAccountIDs(ids []int64) (string, error) {
	if len(ids) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(ids)
	return string(b), err
}

// unmarshalCloudAccountIDs never returns nil; malformed input yields an empty list.
func unmarshalCloudAccountIDs(s string) []int64 {
	ids := []int64{}
	if s != "" {
		if err := json.Unmarshal([]byte(s), &ids); err != nil || ids == nil {
			return []int64{}
		}
	}
	return ids
}

func marshalCloudFields(f map[string]string) (string, error) {
	if f == nil {
		return "{}", nil
	}
	b, err := json.Marshal(f)
	return string(b), err
}

const tfCloudCols = `id, name, provider, description, fields, verified_at, verify_ok, verify_msg, created_at, updated_at`

func scanTFCloudAccount(sc rowScanner) (*TFCloudAccount, error) {
	var a TFCloudAccount
	var fields string
	var verifiedAt sql.NullTime
	var ok int
	if err := sc.Scan(&a.ID, &a.Name, &a.Provider, &a.Description, &fields, &verifiedAt, &ok, &a.VerifyMsg, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	a.Fields = map[string]string{}
	if fields != "" {
		if err := json.Unmarshal([]byte(fields), &a.Fields); err != nil || a.Fields == nil {
			a.Fields = map[string]string{}
		}
	}
	if verifiedAt.Valid {
		a.VerifiedAt = &verifiedAt.Time
	}
	a.VerifyOK = ok != 0
	return &a, nil
}

// CreateTFCloudAccount inserts an account; ErrTFCloudAccountNameTaken on a case-insensitive duplicate name.
func (d *DB) CreateTFCloudAccount(a *TFCloudAccount) (int64, error) {
	fields, err := marshalCloudFields(a.Fields)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	res, err := d.Exec(`INSERT INTO tf_cloud_accounts (name, provider, description, fields, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		a.Name, a.Provider, a.Description, fields, now, now)
	if err != nil {
		if isUniqueConstraintError(err) {
			return 0, ErrTFCloudAccountNameTaken
		}
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	a.ID, a.CreatedAt, a.UpdatedAt = id, now, now
	return id, nil
}

// GetTFCloudAccount returns nil, nil when the account does not exist.
func (d *DB) GetTFCloudAccount(id int64) (*TFCloudAccount, error) {
	a, err := scanTFCloudAccount(d.QueryRow(`SELECT `+tfCloudCols+` FROM tf_cloud_accounts WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return a, err
}

func (d *DB) ListTFCloudAccounts() ([]TFCloudAccount, error) {
	rows, err := d.Query(`SELECT ` + tfCloudCols + ` FROM tf_cloud_accounts ORDER BY name COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]TFCloudAccount, 0)
	for rows.Next() {
		a, err := scanTFCloudAccount(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *a)
	}
	return list, rows.Err()
}

// UpdateTFCloudAccount replaces name/provider/description/fields wholesale and resets the verify result,
// since the stored credentials changed. The caller merges kept secrets into a.Fields first.
func (d *DB) UpdateTFCloudAccount(a *TFCloudAccount) error {
	fields, err := marshalCloudFields(a.Fields)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	_, err = d.Exec(`UPDATE tf_cloud_accounts SET name = ?, provider = ?, description = ?, fields = ?,
		verified_at = NULL, verify_ok = 0, verify_msg = '', updated_at = ? WHERE id = ?`,
		a.Name, a.Provider, a.Description, fields, now, a.ID)
	if err != nil {
		if isUniqueConstraintError(err) {
			return ErrTFCloudAccountNameTaken
		}
		return err
	}
	a.UpdatedAt = now
	a.VerifiedAt, a.VerifyOK, a.VerifyMsg = nil, false, ""
	return nil
}

// DeleteTFCloudAccount removes the account and unlinks it from every workspace in one transaction.
func (d *DB) DeleteTFCloudAccount(id int64) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM tf_cloud_accounts WHERE id = ?`, id); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT w.id, w.cloud_account_ids FROM tf_workspaces w, json_each(w.cloud_account_ids) j WHERE j.value = ?`, id)
	if err != nil {
		return err
	}
	links := map[int64]string{}
	for rows.Next() {
		var wsID int64
		var raw string
		if err := rows.Scan(&wsID, &raw); err != nil {
			rows.Close()
			return err
		}
		links[wsID] = raw
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for wsID, raw := range links {
		kept := []int64{}
		for _, x := range unmarshalCloudAccountIDs(raw) {
			if x != id {
				kept = append(kept, x)
			}
		}
		enc, err := marshalCloudAccountIDs(kept)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE tf_workspaces SET cloud_account_ids = ? WHERE id = ?`, enc, wsID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetTFCloudAccountVerify stores the latest credential check result.
func (d *DB) SetTFCloudAccountVerify(id int64, ok bool, msg string) error {
	_, err := d.Exec(`UPDATE tf_cloud_accounts SET verified_at = ?, verify_ok = ?, verify_msg = ? WHERE id = ?`,
		time.Now().UTC(), boolToInt(ok), msg, id)
	return err
}

// TFCloudAccountWorkspaceCount returns how many workspaces link the account.
func (d *DB) TFCloudAccountWorkspaceCount(id int64) (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(DISTINCT w.id) FROM tf_workspaces w, json_each(w.cloud_account_ids) j WHERE j.value = ?`, id).Scan(&n)
	return n, err
}
