package store

import (
	"database/sql"
	"errors"
	"time"
)

// S3Account is one S3-compatible storage target. SecretKey is plaintext in SQLite like cloudflare_accounts.api_token.
type S3Account struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Endpoint  string    `json:"endpoint"`
	Region    string    `json:"region"`
	Bucket    string    `json:"bucket"`
	AccessKey string    `json:"access_key"`
	SecretKey string    `json:"-"`
	Prefix    string    `json:"prefix"`
	PathStyle string    `json:"path_style"` // "1", "0" or "" (auto)
	CreatedAt time.Time `json:"created_at"`
}

var ErrS3AccountNameTaken = errors.New("an S3 account with this name already exists")

const s3AccountCols = `id, name, endpoint, region, bucket, access_key, secret_key, prefix, path_style, created_at`

func scanS3Account(sc interface{ Scan(...any) error }) (*S3Account, error) {
	var a S3Account
	err := sc.Scan(&a.ID, &a.Name, &a.Endpoint, &a.Region, &a.Bucket, &a.AccessKey, &a.SecretKey, &a.Prefix, &a.PathStyle, &a.CreatedAt)
	return &a, err
}

// CreateS3Account returns ErrS3AccountNameTaken when the name (case-insensitive) is already used.
func (d *DB) CreateS3Account(a *S3Account) (int64, error) {
	res, err := d.Exec(`INSERT INTO s3_accounts (name, endpoint, region, bucket, access_key, secret_key, prefix, path_style) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Name, a.Endpoint, a.Region, a.Bucket, a.AccessKey, a.SecretKey, a.Prefix, a.PathStyle)
	if err != nil {
		if isUniqueConstraintError(err) {
			return 0, ErrS3AccountNameTaken
		}
		return 0, err
	}
	return res.LastInsertId()
}

// GetS3Account returns (nil, nil) when the account does not exist.
func (d *DB) GetS3Account(id int64) (*S3Account, error) {
	a, err := scanS3Account(d.QueryRow(`SELECT `+s3AccountCols+` FROM s3_accounts WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return a, err
}

func (d *DB) ListS3Accounts() ([]S3Account, error) {
	rows, err := d.Query(`SELECT ` + s3AccountCols + ` FROM s3_accounts ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]S3Account, 0)
	for rows.Next() {
		a, err := scanS3Account(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *a)
	}
	return list, rows.Err()
}

// UpdateS3Account always updates every field except secret_key, which changes only when a.SecretKey != "".
func (d *DB) UpdateS3Account(a *S3Account) error {
	q := `UPDATE s3_accounts SET name = ?, endpoint = ?, region = ?, bucket = ?, access_key = ?, prefix = ?, path_style = ?`
	args := []any{a.Name, a.Endpoint, a.Region, a.Bucket, a.AccessKey, a.Prefix, a.PathStyle}
	if a.SecretKey != "" {
		q += `, secret_key = ?`
		args = append(args, a.SecretKey)
	}
	_, err := d.Exec(q+` WHERE id = ?`, append(args, a.ID)...)
	if isUniqueConstraintError(err) {
		return ErrS3AccountNameTaken
	}
	return err
}

func (d *DB) DeleteS3Account(id int64) error {
	_, err := d.Exec(`DELETE FROM s3_accounts WHERE id = ?`, id)
	return err
}

// legacyS3Keys are the single-account settings of the first S3 version.
var legacyS3Keys = []string{"s3_endpoint", "s3_region", "s3_bucket", "s3_access_key", "s3_secret_key", "s3_prefix", "s3_path_style"}

// migrateLegacyS3Settings turns the old single-account `s3_*` settings (also present in older snapshots) into an
// account named "Default" and removes them. It does nothing when no bucket was configured or an account exists.
func (d *DB) migrateLegacyS3Settings() error {
	defer d.settingsCache.Clear()
	get := func(k string) string {
		var v string
		_ = d.QueryRow(`SELECT value FROM settings WHERE key = ?`, k).Scan(&v)
		return v
	}
	if get("s3_bucket") != "" {
		var n int
		if err := d.QueryRow(`SELECT COUNT(*) FROM s3_accounts`).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err := d.CreateS3Account(&S3Account{
				Name: "Default", Endpoint: get("s3_endpoint"), Region: get("s3_region"), Bucket: get("s3_bucket"),
				AccessKey: get("s3_access_key"), SecretKey: get("s3_secret_key"), Prefix: get("s3_prefix"), PathStyle: get("s3_path_style"),
			}); err != nil {
				return err
			}
		}
	}
	for _, k := range legacyS3Keys {
		if _, err := d.Exec(`DELETE FROM settings WHERE key = ?`, k); err != nil {
			return err
		}
	}
	return nil
}
