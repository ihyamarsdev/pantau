package store

// Inspection Runs: the history of inspections per host and its retention.

import (
	"time"
)

type InspectionRun struct {
	ID         int64     `json:"id"`
	HostID     int64     `json:"host_id"`
	StartedAt  time.Time `json:"started_at"`
	DurationMs int64     `json:"duration_ms"`
	Status     string    `json:"status"` // ok, drift, degraded, down, error
	Summary    string    `json:"summary"`
	Details    string    `json:"details"`
	CreatedAt  time.Time `json:"created_at"`
}

// Inspection Run operations
func (d *DB) RecordInspectionRun(run *InspectionRun) error {
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now()
	}
	res, err := d.Exec(`INSERT INTO inspection_runs(host_id, started_at, duration_ms, status, summary, details) VALUES (?, ?, ?, ?, ?, ?)`,
		run.HostID, run.StartedAt, run.DurationMs, run.Status, run.Summary, run.Details)
	if err != nil {
		return err
	}
	run.ID, _ = res.LastInsertId()

	// ponytail: Dual-bucket auto-pruning preserves up to 100 latest OK runs and 100 latest issue runs (drift/error/down/degraded) per host without background schedulers.
	if run.Status == "ok" {
		_, _ = d.Exec(`DELETE FROM inspection_runs WHERE host_id = ? AND status = 'ok' AND id NOT IN (SELECT id FROM inspection_runs WHERE host_id = ? AND status = 'ok' ORDER BY id DESC LIMIT 100)`, run.HostID, run.HostID)
	} else {
		_, _ = d.Exec(`DELETE FROM inspection_runs WHERE host_id = ? AND status != 'ok' AND id NOT IN (SELECT id FROM inspection_runs WHERE host_id = ? AND status != 'ok' ORDER BY id DESC LIMIT 100)`, run.HostID, run.HostID)
	}
	return nil
}

func (d *DB) ListInspectionRuns(hostID int64, limit int, filter ...string) ([]InspectionRun, error) {
	if limit <= 0 {
		limit = 50
	}
	var query string
	var args []interface{}
	if len(filter) > 0 && filter[0] == "issues" {
		query = `SELECT id, host_id, started_at, duration_ms, status, summary, details, created_at FROM inspection_runs WHERE host_id = ? AND status != 'ok' ORDER BY id DESC LIMIT ?`
		args = []interface{}{hostID, limit}
	} else {
		query = `SELECT id, host_id, started_at, duration_ms, status, summary, details, created_at FROM inspection_runs WHERE host_id = ? ORDER BY id DESC LIMIT ?`
		args = []interface{}{hostID, limit}
	}
	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []InspectionRun
	for rows.Next() {
		var r InspectionRun
		if err := rows.Scan(&r.ID, &r.HostID, &r.StartedAt, &r.DurationMs, &r.Status, &r.Summary, &r.Details, &r.CreatedAt); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, nil
}
