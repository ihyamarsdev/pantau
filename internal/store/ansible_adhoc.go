package store

import (
	"database/sql"
	"errors"
	"time"
)

// AnsibleAdhocRun is one ad-hoc module execution.
type AnsibleAdhocRun struct {
	ID         int64      `json:"id"`
	Module     string     `json:"module"`
	Args       string     `json:"args"`
	Pattern    string     `json:"pattern"`
	Targets    string     `json:"targets"` // JSON: [{"id":1,"name":"web-1"}] snapshot
	Become     bool       `json:"become"`
	Verbosity  int        `json:"verbosity"`
	Status     string     `json:"status"` // queued|running|success|failed|cancelled|interrupted
	ExitCode   *int       `json:"exit_code"`
	Results    string     `json:"-"` // JSON: [{"alias","status","summary"}]; the API sends it parsed
	Log        string     `json:"-"` // only via the log endpoint
	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

// AnsibleHostFacts is the last gathered fact set of one host.
type AnsibleHostFacts struct {
	HostID     int64     `json:"host_id"`
	Facts      string    `json:"-"` // raw ansible_facts JSON
	Summary    string    `json:"-"` // FactsSummary JSON; the API sends it parsed
	GatheredAt time.Time `json:"gathered_at"`
}

var (
	ErrAnsibleAdhocRunNotFound  = errors.New("ansible adhoc run not found")
	ErrAnsibleHostFactsNotFound = errors.New("ansible host facts not found")
)

func (d *DB) CreateAnsibleAdhocRun(r *AnsibleAdhocRun) (int64, error) {
	if r.Status == "" {
		r.Status = "queued"
	}
	if r.Pattern == "" {
		r.Pattern = "all"
	}
	becomeInt := 0
	if r.Become {
		becomeInt = 1
	}
	now := time.Now().UTC()
	res, err := d.Exec(`
		INSERT INTO ansible_adhoc_runs (
			module, args, pattern, targets, become, verbosity, status, exit_code, results, log, created_at, started_at, finished_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, r.Module, r.Args, r.Pattern, r.Targets, becomeInt, r.Verbosity, r.Status, r.ExitCode, r.Results, r.Log, now, r.StartedAt, r.FinishedAt)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	r.ID = id
	r.CreatedAt = now
	return id, nil
}

// StartAnsibleAdhocRun transitions a run to "running" and records started_at = now.
func (d *DB) StartAnsibleAdhocRun(id int64) error {
	res, err := d.Exec(`UPDATE ansible_adhoc_runs SET status = 'running', started_at = ? WHERE id = ?`, time.Now().UTC(), id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrAnsibleAdhocRunNotFound
	}
	return nil
}

// FinishAnsibleAdhocRun records the outcome of a run. results is JSON ("" = none).
func (d *DB) FinishAnsibleAdhocRun(id int64, status string, exitCode *int, log, results string) error {
	res, err := d.Exec(`
		UPDATE ansible_adhoc_runs
		SET status = ?, exit_code = ?, log = ?, results = ?, finished_at = ?
		WHERE id = ?
	`, status, exitCode, log, results, time.Now().UTC(), id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrAnsibleAdhocRunNotFound
	}
	return nil
}

const ansibleAdhocRunCols = `id, module, args, pattern, targets, become, verbosity, status, exit_code, results, created_at, started_at, finished_at`

func scanAnsibleAdhocRun(sc rowScanner, withLog bool) (*AnsibleAdhocRun, error) {
	var r AnsibleAdhocRun
	var becomeInt int
	var exitCode sql.NullInt64
	var startedAt, finishedAt sql.NullTime
	dest := []any{&r.ID, &r.Module, &r.Args, &r.Pattern, &r.Targets, &becomeInt, &r.Verbosity, &r.Status, &exitCode,
		&r.Results, &r.CreatedAt, &startedAt, &finishedAt}
	if withLog {
		dest = append(dest, &r.Log)
	}
	if err := sc.Scan(dest...); err != nil {
		return nil, err
	}
	r.Become = becomeInt != 0
	if exitCode.Valid {
		ec := int(exitCode.Int64)
		r.ExitCode = &ec
	}
	if startedAt.Valid {
		r.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		r.FinishedAt = &finishedAt.Time
	}
	return &r, nil
}

// GetAnsibleAdhocRun retrieves a run by ID, including its Log.
func (d *DB) GetAnsibleAdhocRun(id int64) (*AnsibleAdhocRun, error) {
	r, err := scanAnsibleAdhocRun(d.QueryRow(`SELECT `+ansibleAdhocRunCols+`, log FROM ansible_adhoc_runs WHERE id = ?`, id), true)
	if err == sql.ErrNoRows {
		return nil, ErrAnsibleAdhocRunNotFound
	}
	return r, err
}

// ListAnsibleAdhocRuns lists runs without Log, newest first. Limit defaults to 50, capped at 200.
func (d *DB) ListAnsibleAdhocRuns(limit int) ([]AnsibleAdhocRun, error) {
	if limit <= 0 {
		limit = 50
	} else if limit > 200 {
		limit = 200
	}
	rows, err := d.Query(`SELECT `+ansibleAdhocRunCols+` FROM ansible_adhoc_runs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]AnsibleAdhocRun, 0)
	for rows.Next() {
		r, err := scanAnsibleAdhocRun(rows, false)
		if err != nil {
			return nil, err
		}
		list = append(list, *r)
	}
	return list, rows.Err()
}

// PruneAnsibleAdhocRuns keeps the newest `keep` finished runs; queued/running runs are never deleted.
func (d *DB) PruneAnsibleAdhocRuns(keep int) error {
	if keep < 0 {
		keep = 0
	}
	_, err := d.Exec(`
		DELETE FROM ansible_adhoc_runs
		WHERE status NOT IN ('queued', 'running') AND id NOT IN (
			SELECT id FROM ansible_adhoc_runs
			WHERE status NOT IN ('queued', 'running')
			ORDER BY id DESC
			LIMIT ?
		)
	`, keep)
	return err
}

// MarkAnsibleAdhocRunsInterrupted transitions leftover queued/running runs to "interrupted" at startup.
func (d *DB) MarkAnsibleAdhocRunsInterrupted() (int64, error) {
	res, err := d.Exec(`
		UPDATE ansible_adhoc_runs SET status = 'interrupted', finished_at = ?
		WHERE status IN ('queued', 'running')
	`, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SetAnsibleHostFacts upserts the facts of a host and returns gathered_at.
func (d *DB) SetAnsibleHostFacts(hostID int64, facts, summary string) (time.Time, error) {
	now := time.Now().UTC()
	_, err := d.Exec(`
		INSERT INTO ansible_host_facts (host_id, facts, summary, gathered_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(host_id) DO UPDATE SET facts = excluded.facts, summary = excluded.summary, gathered_at = excluded.gathered_at
	`, hostID, facts, summary, now)
	return now, err
}

// GetAnsibleHostFacts returns the stored facts of a host.
func (d *DB) GetAnsibleHostFacts(hostID int64) (*AnsibleHostFacts, error) {
	var f AnsibleHostFacts
	err := d.QueryRow(`SELECT host_id, facts, summary, gathered_at FROM ansible_host_facts WHERE host_id = ?`, hostID).
		Scan(&f.HostID, &f.Facts, &f.Summary, &f.GatheredAt)
	if err == sql.ErrNoRows {
		return nil, ErrAnsibleHostFactsNotFound
	}
	return &f, err
}

// ListAnsibleHostFactsGatheredAt returns host_id -> gathered_at for every cached fact set (cheap explorer join).
func (d *DB) ListAnsibleHostFactsGatheredAt() (map[int64]time.Time, error) {
	rows, err := d.Query(`SELECT host_id, gathered_at FROM ansible_host_facts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	m := make(map[int64]time.Time)
	for rows.Next() {
		var id int64
		var at time.Time
		if err := rows.Scan(&id, &at); err != nil {
			return nil, err
		}
		m[id] = at
	}
	return m, rows.Err()
}
