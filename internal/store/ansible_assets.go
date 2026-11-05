package store

import (
	"database/sql"
	"errors"
	"time"
)

// AnsibleAsset is a Jinja template or an uploaded file shared by all inline playbooks. Data is only filled by
// GetAnsibleAsset and LoadAnsibleAssets; lists carry the size instead.
type AnsibleAsset struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"` // "template" | "file"
	Name      string    `json:"name"` // relative path, e.g. "nginx/site.conf.j2"
	Size      int64     `json:"size"`
	UpdatedAt time.Time `json:"updated_at"`
	Data      []byte    `json:"-"`
}

var ErrAnsibleAssetNotFound = errors.New("ansible template or file not found")

// SaveAnsibleAsset creates the asset or replaces its content when (kind, name) already exists.
func (d *DB) SaveAnsibleAsset(kind, name string, data []byte) error {
	now := time.Now().UTC()
	_, err := d.Exec(`INSERT INTO ansible_assets (kind, name, data, size, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(kind, name) DO UPDATE SET data = excluded.data, size = excluded.size, updated_at = excluded.updated_at`,
		kind, name, data, len(data), now, now)
	return err
}

// ListAnsibleAssets lists assets without content; kind "" lists all kinds.
func (d *DB) ListAnsibleAssets(kind string) ([]AnsibleAsset, error) {
	rows, err := d.Query(`SELECT id, kind, name, size, updated_at FROM ansible_assets WHERE ? = '' OR kind = ? ORDER BY name COLLATE NOCASE`, kind, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AnsibleAsset{}
	for rows.Next() {
		var a AnsibleAsset
		if err := rows.Scan(&a.ID, &a.Kind, &a.Name, &a.Size, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (d *DB) GetAnsibleAsset(id int64) (*AnsibleAsset, error) {
	var a AnsibleAsset
	err := d.QueryRow(`SELECT id, kind, name, size, updated_at, data FROM ansible_assets WHERE id = ?`, id).
		Scan(&a.ID, &a.Kind, &a.Name, &a.Size, &a.UpdatedAt, &a.Data)
	if err == sql.ErrNoRows {
		return nil, ErrAnsibleAssetNotFound
	}
	return &a, err
}

// LoadAnsibleAssets returns every asset with its content (for a run or a snapshot).
func (d *DB) LoadAnsibleAssets() ([]AnsibleAsset, error) {
	rows, err := d.Query(`SELECT id, kind, name, size, updated_at, data FROM ansible_assets ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AnsibleAsset
	for rows.Next() {
		var a AnsibleAsset
		if err := rows.Scan(&a.ID, &a.Kind, &a.Name, &a.Size, &a.UpdatedAt, &a.Data); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (d *DB) DeleteAnsibleAsset(id int64) error {
	res, err := d.Exec(`DELETE FROM ansible_assets WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAnsibleAssetNotFound
	}
	return nil
}
