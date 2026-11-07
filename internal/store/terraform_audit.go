package store

import "time"

// tfAuditKeep is the number of audit rows retained; a var so tests can shrink it.
var tfAuditKeep = 5000

const tfAuditTrimEvery = 100

// TFAuditEntry is one persisted terraform audit log line.
type TFAuditEntry struct {
	ID      int64     `json:"id"`
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Host    string    `json:"host"`
	Message string    `json:"message"`
}

// AppendTFAudit stores a line and trims old rows once every tfAuditTrimEvery inserts.
func (d *DB) AppendTFAudit(ts time.Time, level, host, message string) error {
	res, err := d.Exec(`INSERT INTO tf_audit (ts, level, host, message) VALUES (?, ?, ?, ?)`,
		ts.UTC().Format(time.RFC3339Nano), level, host, message)
	if err != nil {
		return err
	}
	if id, err := res.LastInsertId(); err == nil && id%tfAuditTrimEvery == 0 {
		_, err = d.Exec(`DELETE FROM tf_audit WHERE id <= (SELECT id FROM tf_audit ORDER BY id DESC LIMIT 1 OFFSET ?)`, tfAuditKeep)
		return err
	}
	return nil
}

// ListTFAudit returns the newest entries first. A non-empty host matches the
// host column or any message containing it.
func (d *DB) ListTFAudit(host string, limit int) ([]TFAuditEntry, error) {
	q := `SELECT id, ts, level, host, message FROM tf_audit`
	var args []any
	if host != "" {
		q += ` WHERE host = ? OR instr(message, ?) > 0`
		args = append(args, host, host)
	}
	rows, err := d.Query(q+` ORDER BY id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TFAuditEntry{}
	for rows.Next() {
		var e TFAuditEntry
		var ts string
		if err := rows.Scan(&e.ID, &ts, &e.Level, &e.Host, &e.Message); err != nil {
			return nil, err
		}
		e.Time, _ = time.Parse(time.RFC3339Nano, ts)
		out = append(out, e)
	}
	return out, rows.Err()
}
