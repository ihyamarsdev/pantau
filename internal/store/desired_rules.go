package store

// Desired State: the rules a host is checked against and the Actual State
// (actual_items) that records the outcome of the last check.

import (
	"database/sql"
	"errors"
	"time"
)

type DesiredRule struct {
	ID       int64  `json:"id"`
	HostID   int64  `json:"host_id"`
	Kind     string `json:"kind"`     // container, disk, backup, cron, service
	Target   string `json:"target"`   // e.g. "nginx", "/", "/var/backups/db.sql.gz", "mysql"
	Expected string `json:"expected"` // e.g. "running", "<85%", "fresh_24h", "active"
	Enabled  bool   `json:"enabled"`
}

type ActualItem struct {
	ID        int64     `json:"id"`
	HostID    int64     `json:"host_id"`
	Kind      string    `json:"kind"`
	Target    string    `json:"target"`
	Current   string    `json:"current"`
	Status    string    `json:"status"` // ok, drift
	Details   string    `json:"details"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Desired State operations
func (d *DB) ListDesiredRules(hostID int64) ([]DesiredRule, error) {
	rows, err := d.Query(`SELECT id, host_id, kind, target, expected, enabled FROM desired_rules WHERE host_id = ? ORDER BY kind, target`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []DesiredRule
	for rows.Next() {
		var r DesiredRule
		var enabled int
		if err := rows.Scan(&r.ID, &r.HostID, &r.Kind, &r.Target, &r.Expected, &enabled); err != nil {
			return nil, err
		}
		r.Enabled = enabled == 1
		rules = append(rules, r)
	}
	return rules, nil
}

func (d *DB) SaveDesiredRule(r *DesiredRule) error {
	enabled := 0
	if r.Enabled {
		enabled = 1
	}
	_, err := d.Exec(`INSERT INTO desired_rules(host_id, kind, target, expected, enabled) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(host_id, kind, target) DO UPDATE SET expected=excluded.expected, enabled=excluded.enabled`,
		r.HostID, r.Kind, r.Target, r.Expected, enabled)
	if err != nil {
		return err
	}
	return d.PruneActualItems(r.HostID) // disabling a rule must clear its Drift too
}

func (d *DB) DeleteDesiredRule(id int64) error {
	var hostID int64
	if err := d.QueryRow(`SELECT host_id FROM desired_rules WHERE id = ?`, id).Scan(&hostID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if _, err := d.Exec(`DELETE FROM desired_rules WHERE id = ?`, id); err != nil {
		return err
	}
	return d.PruneActualItems(hostID)
}

// PruneActualItems drops actual items that no enabled rule owns any more (rule deleted or disabled). Without it a
// removed rule keeps showing as Drift because the Host Detail banner reads actual_items. When no Drift remains
// on a degraded Host it is marked healthy and its alerts are closed, the same outcome as an inspection that
// finds no Drift, instead of waiting for the next one (which the inspection cooldown can delay).
func (d *DB) PruneActualItems(hostID int64) error {
	if _, err := d.Exec(`DELETE FROM actual_items WHERE host_id = ? AND NOT EXISTS (
		SELECT 1 FROM desired_rules r WHERE r.host_id = actual_items.host_id AND r.kind = actual_items.kind
		AND r.target = actual_items.target AND r.enabled = 1)`, hostID); err != nil {
		return err
	}
	var drift int
	if err := d.QueryRow(`SELECT COUNT(*) FROM actual_items WHERE host_id = ? AND status = 'drift'`, hostID).Scan(&drift); err != nil || drift > 0 {
		return err
	}
	res, err := d.Exec(`UPDATE hosts SET status = 'healthy' WHERE id = ? AND status = 'degraded'`, hostID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return d.AcknowledgeHostAlerts(hostID)
	}
	return nil
}

// Actual items operations
func (d *DB) ListActualItems(hostID int64) ([]ActualItem, error) {
	rows, err := d.Query(`SELECT id, host_id, kind, target, current, status, details, updated_at FROM actual_items WHERE host_id = ? ORDER BY kind, target`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []ActualItem
	for rows.Next() {
		var it ActualItem
		if err := rows.Scan(&it.ID, &it.HostID, &it.Kind, &it.Target, &it.Current, &it.Status, &it.Details, &it.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, nil
}

func (d *DB) UpsertActualItem(it *ActualItem) error {
	_, err := d.Exec(`INSERT INTO actual_items(host_id, kind, target, current, status, details, updated_at) VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(host_id, kind, target) DO UPDATE SET current=excluded.current, status=excluded.status, details=excluded.details, updated_at=CURRENT_TIMESTAMP`,
		it.HostID, it.Kind, it.Target, it.Current, it.Status, it.Details)
	return err
}
