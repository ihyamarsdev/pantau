package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// AnsibleSchedule runs a playbook on a cron expression (server local time) against fixed targets.
type AnsibleSchedule struct {
	ID           int64      `json:"id"`
	PlaybookID   int64      `json:"playbook_id"`
	PlaybookName string     `json:"playbook_name,omitempty"` // computed on read
	Name         string     `json:"name"`
	Cron         string     `json:"cron"`
	Enabled      bool       `json:"enabled"`
	Mode         string     `json:"mode"`
	HostIDs      []int64    `json:"host_ids"`
	Group        string     `json:"group"`
	ExtraVars    string     `json:"extra_vars"`
	Tags         string     `json:"tags"`
	SkipTags     string     `json:"skip_tags"`
	Verbosity    int        `json:"verbosity"`
	Become       bool       `json:"become"`
	NextRunAt    *time.Time `json:"next_run_at"`
	LastRunAt    *time.Time `json:"last_run_at"`
	LastRunID    int64      `json:"last_run_id"`
	LastError    string     `json:"last_error"`
	CreatedAt    time.Time  `json:"created_at"`
}

var ErrAnsibleScheduleNotFound = errors.New("ansible schedule not found")

const ansibleScheduleCols = `s.id, s.playbook_id, COALESCE(p.name, ''), s.name, s.cron, s.enabled, s.mode, s.host_ids, s.group_name,
	s.extra_vars, s.tags, s.skip_tags, s.verbosity, s.become, s.next_run_at, s.last_run_at, s.last_run_id, s.last_error, s.created_at`

func scanAnsibleSchedule(sc rowScanner) (*AnsibleSchedule, error) {
	var s AnsibleSchedule
	var enabled, become int
	var hostIDs string
	var next, last sql.NullTime
	if err := sc.Scan(&s.ID, &s.PlaybookID, &s.PlaybookName, &s.Name, &s.Cron, &enabled, &s.Mode, &hostIDs, &s.Group,
		&s.ExtraVars, &s.Tags, &s.SkipTags, &s.Verbosity, &become, &next, &last, &s.LastRunID, &s.LastError, &s.CreatedAt); err != nil {
		return nil, err
	}
	s.Enabled, s.Become = enabled != 0, become != 0
	if err := json.Unmarshal([]byte(hostIDs), &s.HostIDs); err != nil || s.HostIDs == nil {
		s.HostIDs = []int64{}
	}
	if next.Valid {
		s.NextRunAt = &next.Time
	}
	if last.Valid {
		s.LastRunAt = &last.Time
	}
	return &s, nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

type sqlExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func insertAnsibleSchedule(x sqlExecer, s *AnsibleSchedule) error {
	ids, _ := json.Marshal(append([]int64{}, s.HostIDs...))
	created := s.CreatedAt
	if created.IsZero() {
		created = time.Now().UTC()
	}
	res, err := x.Exec(`INSERT INTO ansible_schedules (id, playbook_id, name, cron, enabled, mode, host_ids, group_name, extra_vars, tags, skip_tags,
		verbosity, become, next_run_at, last_run_at, last_run_id, last_error, created_at)
		VALUES (NULLIF(?, 0), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.PlaybookID, s.Name, s.Cron, b2i(s.Enabled), s.Mode, string(ids), s.Group, s.ExtraVars, s.Tags, s.SkipTags,
		s.Verbosity, b2i(s.Become), s.NextRunAt, s.LastRunAt, s.LastRunID, s.LastError, created)
	if err != nil {
		return err
	}
	if s.ID == 0 {
		s.ID, _ = res.LastInsertId()
	}
	s.CreatedAt = created
	return nil
}

func (d *DB) CreateAnsibleSchedule(s *AnsibleSchedule) (int64, error) {
	s.ID = 0
	if err := insertAnsibleSchedule(d, s); err != nil {
		return 0, err
	}
	return s.ID, nil
}

func (d *DB) GetAnsibleSchedule(id int64) (*AnsibleSchedule, error) {
	s, err := scanAnsibleSchedule(d.QueryRow(`SELECT `+ansibleScheduleCols+`
		FROM ansible_schedules s LEFT JOIN ansible_playbooks p ON p.id = s.playbook_id WHERE s.id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, ErrAnsibleScheduleNotFound
	}
	return s, err
}

// ListAnsibleSchedules lists schedules, oldest first. playbookID 0 = all playbooks.
func (d *DB) ListAnsibleSchedules(playbookID int64) ([]AnsibleSchedule, error) {
	rows, err := d.Query(`SELECT `+ansibleScheduleCols+`
		FROM ansible_schedules s LEFT JOIN ansible_playbooks p ON p.id = s.playbook_id
		WHERE (? = 0 OR s.playbook_id = ?) ORDER BY s.id ASC`, playbookID, playbookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]AnsibleSchedule, 0)
	for rows.Next() {
		s, err := scanAnsibleSchedule(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *s)
	}
	return list, rows.Err()
}

// UpdateAnsibleSchedule replaces the user-editable fields and next_run_at (the caller recomputes it).
func (d *DB) UpdateAnsibleSchedule(s *AnsibleSchedule) error {
	ids, _ := json.Marshal(append([]int64{}, s.HostIDs...))
	res, err := d.Exec(`UPDATE ansible_schedules SET playbook_id = ?, name = ?, cron = ?, enabled = ?, mode = ?, host_ids = ?, group_name = ?,
		extra_vars = ?, tags = ?, skip_tags = ?, verbosity = ?, become = ?, next_run_at = ? WHERE id = ?`,
		s.PlaybookID, s.Name, s.Cron, b2i(s.Enabled), s.Mode, string(ids), s.Group, s.ExtraVars, s.Tags, s.SkipTags,
		s.Verbosity, b2i(s.Become), s.NextRunAt, s.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAnsibleScheduleNotFound
	}
	return nil
}

func (d *DB) DeleteAnsibleSchedule(id int64) error {
	res, err := d.Exec(`DELETE FROM ansible_schedules WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAnsibleScheduleNotFound
	}
	return nil
}

// RecordAnsibleScheduleFire stores the outcome of a fire attempt and the next planned time (nil = none).
// A nil `at` keeps last_run_at (used for skipped/missed occurrences); runID 0 with a non-empty errMsg means the
// run could not be started.
func (d *DB) RecordAnsibleScheduleFire(id int64, at *time.Time, runID int64, errMsg string, next *time.Time) error {
	var last any
	if at != nil {
		last = at.UTC()
	}
	_, err := d.Exec(`UPDATE ansible_schedules SET last_run_at = COALESCE(?, last_run_at), last_run_id = CASE WHEN ? = 0 THEN last_run_id ELSE ? END,
		last_error = ?, next_run_at = ? WHERE id = ?`, last, runID, runID, errMsg, next, id)
	return err
}

// SetAnsibleScheduleNext sets only next_run_at (first computation for a freshly enabled schedule).
func (d *DB) SetAnsibleScheduleNext(id int64, next *time.Time) error {
	_, err := d.Exec(`UPDATE ansible_schedules SET next_run_at = ? WHERE id = ?`, next, id)
	return err
}
