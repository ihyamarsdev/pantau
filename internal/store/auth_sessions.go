package store

import (
	"fmt"
	"time"
)

// Administrator sessions are kept in SQLite so that restarting Pantau (a
// self-update, a reboot) does not sign every browser out: the browser keeps its
// pantau_session cookie and the next request finds the token here.
//
// The token is the only secret: a row holds no identity, and a token is only
// valid until expires_at. Rows are never part of a System Snapshot (only the
// tables listed in ExportSnapshot are), so an exported backup cannot be used to
// resume a session.

// SaveSession stores a session token with its expiry, replacing an earlier row
// for the same token (a re-login in the same browser keeps one row per token).
func (d *DB) SaveSession(token string, expiresAt time.Time) error {
	_, err := d.Exec(`INSERT INTO auth_sessions (token, expires_at) VALUES (?, ?)
		ON CONFLICT(token) DO UPDATE SET expires_at = excluded.expires_at`,
		token, expiresAt.Unix())
	if err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	return nil
}

// LoadSessions returns every session that has not expired yet, keyed by token,
// and drops the expired rows in the same call.
func (d *DB) LoadSessions(now time.Time) (map[string]time.Time, error) {
	if _, err := d.Exec("DELETE FROM auth_sessions WHERE expires_at <= ?", now.Unix()); err != nil {
		return nil, fmt.Errorf("purge expired sessions: %w", err)
	}
	rows, err := d.Query("SELECT token, expires_at FROM auth_sessions")
	if err != nil {
		return nil, fmt.Errorf("load sessions: %w", err)
	}
	defer rows.Close()
	out := make(map[string]time.Time)
	for rows.Next() {
		var token string
		var exp int64
		if err := rows.Scan(&token, &exp); err != nil {
			return nil, err
		}
		out[token] = time.Unix(exp, 0)
	}
	return out, rows.Err()
}

// DeleteSession forgets one token (logout, or a token seen past its expiry).
func (d *DB) DeleteSession(token string) error {
	if _, err := d.Exec("DELETE FROM auth_sessions WHERE token = ?", token); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}
