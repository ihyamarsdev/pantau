package store

import (
	"database/sql"
	"time"
)

// Trusted SSH host keys, keyed by the dialed "host:port" and stored as an authorized_keys line.
// Restoring a snapshot does not carry them: trust is established per instance.

// GetTrustedHostKey returns the key trusted for addr, if any.
func (d *DB) GetTrustedHostKey(addr string) (string, bool, error) {
	var key string
	err := d.QueryRow(`SELECT key FROM ssh_host_keys WHERE addr = ?`, addr).Scan(&key)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return key, true, nil
}

// InsertTrustedHostKey records key for addr unless one is already trusted. It reports whether it inserted.
func (d *DB) InsertTrustedHostKey(addr, key string) (bool, error) {
	res, err := d.Exec(`INSERT OR IGNORE INTO ssh_host_keys (addr, key, created_at) VALUES (?, ?, ?)`, addr, key, time.Now().UTC())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// DeleteTrustedHostKey forgets the key for addr so the next connection trusts the host's current key.
func (d *DB) DeleteTrustedHostKey(addr string) error {
	_, err := d.Exec(`DELETE FROM ssh_host_keys WHERE addr = ?`, addr)
	return err
}
