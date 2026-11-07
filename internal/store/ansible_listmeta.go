package store

import (
	"database/sql"
	"encoding/json"
	"time"
)

type ansibleScheduleSummary struct {
	count int
	next  *time.Time
}

// ansibleScheduleSummaries counts schedules per playbook and finds the earliest next run among ENABLED ones in one
// query (playbookID 0 = all playbooks). Plain columns are selected so the driver keeps parsing next_run_at as a time.
func (d *DB) ansibleScheduleSummaries(playbookID int64) (map[int64]ansibleScheduleSummary, error) {
	rows, err := d.Query(`SELECT playbook_id, enabled, next_run_at FROM ansible_schedules WHERE (? = 0 OR playbook_id = ?)`, playbookID, playbookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int64]ansibleScheduleSummary{}
	for rows.Next() {
		var pid int64
		var enabled int
		var next sql.NullTime
		if err := rows.Scan(&pid, &enabled, &next); err != nil {
			return nil, err
		}
		s := out[pid]
		s.count++
		if enabled != 0 && next.Valid && (s.next == nil || next.Time.Before(*s.next)) {
			t := next.Time
			s.next = &t
		}
		out[pid] = s
	}
	return out, rows.Err()
}

func (p *AnsiblePlaybook) applyScheduleSummary(s ansibleScheduleSummary) {
	p.ScheduleCount = s.count
	p.NextRunAt = s.next
}

// countRunTargets returns the number of targets in a run's targets snapshot (a JSON array).
func countRunTargets(raw string) (int, bool) {
	if raw == "" {
		return 0, false
	}
	var arr []json.RawMessage
	if json.Unmarshal([]byte(raw), &arr) != nil {
		return 0, false
	}
	return len(arr), true
}
