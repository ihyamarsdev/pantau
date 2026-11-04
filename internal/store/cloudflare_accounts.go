package store

import (
	"database/sql"
	"errors"
	"time"
)

type CloudflareAccount struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	AccountID string    `json:"account_id,omitempty"`
	APIToken  string    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
}

var ErrCloudflareAccountNameTaken = errors.New("a cloudflare account with this name already exists")

// CreateCloudflareAccount creates a new cloudflare account record.
// Returns ErrCloudflareAccountNameTaken if an account with this name (case-insensitive) already exists.
func (d *DB) CreateCloudflareAccount(a *CloudflareAccount) (int64, error) {
	res, err := d.Exec(`INSERT INTO cloudflare_accounts (name, account_id, api_token) VALUES (?, ?, ?)`,
		a.Name, a.AccountID, a.APIToken)
	if err != nil {
		if isUniqueConstraintError(err) {
			return 0, ErrCloudflareAccountNameTaken
		}
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) GetCloudflareAccount(id int64) (*CloudflareAccount, error) {
	var a CloudflareAccount
	err := d.QueryRow(`SELECT id, name, account_id, api_token, created_at FROM cloudflare_accounts WHERE id = ?`, id).
		Scan(&a.ID, &a.Name, &a.AccountID, &a.APIToken, &a.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (d *DB) ListCloudflareAccounts() ([]CloudflareAccount, error) {
	rows, err := d.Query(`SELECT id, name, account_id, api_token, created_at FROM cloudflare_accounts ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]CloudflareAccount, 0)
	for rows.Next() {
		var a CloudflareAccount
		if err := rows.Scan(&a.ID, &a.Name, &a.AccountID, &a.APIToken, &a.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, a)
	}
	return list, rows.Err()
}

// UpdateCloudflareAccount updates an existing cloudflare account record.
// Always updates name and account_id; api_token only when a.APIToken != "".
func (d *DB) UpdateCloudflareAccount(a *CloudflareAccount) error {
	var err error
	if a.APIToken != "" {
		_, err = d.Exec(`UPDATE cloudflare_accounts SET name = ?, account_id = ?, api_token = ? WHERE id = ?`,
			a.Name, a.AccountID, a.APIToken, a.ID)
	} else {
		_, err = d.Exec(`UPDATE cloudflare_accounts SET name = ?, account_id = ? WHERE id = ?`,
			a.Name, a.AccountID, a.ID)
	}
	if err != nil {
		if isUniqueConstraintError(err) {
			return ErrCloudflareAccountNameTaken
		}
		return err
	}
	return nil
}

func (d *DB) DeleteCloudflareAccount(id int64) error {
	_, err := d.Exec(`DELETE FROM cloudflare_accounts WHERE id = ?`, id)
	return err
}
