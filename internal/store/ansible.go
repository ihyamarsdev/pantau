package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type AnsiblePlaybook struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`    // inline source only
	ExtraVars   string `json:"extra_vars"` // JSON objek bawaan, "" bila kosong

	Source      string `json:"source"` // "inline" | "git" | "bundle"
	GitURL      string `json:"git_url"`
	GitBranch   string `json:"git_branch"`
	GitPath     string `json:"git_path"`
	GitAuth     string `json:"git_auth"` // "none" | "token" | "ssh_key"
	GitKeyID    int64  `json:"git_key_id"`
	GitToken    string `json:"-"`             // NEVER serialized
	GitTokenSet bool   `json:"git_token_set"` // whether a token is stored (computed)

	Survey         string     `json:"survey"`       // JSON array of survey fields, "" bila tidak ada survey
	LastSyncAt     *time.Time `json:"last_sync_at"` // git source: hasil Sync Now terakhir
	LastSyncCommit string     `json:"last_sync_commit"`
	GroupName      string     `json:"group_name"` // free-text label; "" = ungrouped (cosmetic, like Host Group)

	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	LastStatus string     `json:"last_status,omitempty"` // dihitung saat List/Get dari run terakhir
	LastRunAt  *time.Time `json:"last_run_at,omitempty"`

	// Ringkasan run terakhir dan jadwal untuk tabel playbook (dihitung saat List/Get, tidak disimpan).
	LastRunID      int64      `json:"last_run_id,omitempty"`
	LastDurationMs *int64     `json:"last_duration_ms,omitempty"`
	LastTargets    *int       `json:"last_targets,omitempty"`
	LastRecap      string     `json:"-"` // recap JSON mentah run terakhir; lapisan web mengubahnya menjadi total last_recap
	ScheduleCount  int        `json:"schedule_count"`
	NextRunAt      *time.Time `json:"next_run_at,omitempty"` // jadwal AKTIF terdekat
}

type AnsibleRun struct {
	ID           int64      `json:"id"`
	PlaybookID   int64      `json:"playbook_id"`
	PlaybookName string     `json:"playbook_name"` // snapshot saat run dibuat
	Status       string     `json:"status"`        // queued|running|success|failed|cancelled|interrupted
	Mode         string     `json:"mode"`
	Targets      string     `json:"targets"` // JSON: [{"id":1,"name":"web-1"}] snapshot
	ExtraVars    string     `json:"extra_vars"`
	Tags         string     `json:"tags"`
	SkipTags     string     `json:"skip_tags"`
	Verbosity    int        `json:"verbosity"`
	Become       bool       `json:"become"`
	Trigger      string     `json:"trigger"`     // "manual" | "schedule"
	ScheduleID   int64      `json:"schedule_id"` // 0 for manual runs
	CommitSHA    string     `json:"commit_sha"`  // git source only
	Limit        string     `json:"limit"`       // --limit host pattern, "" = all targets
	Recap        string     `json:"-"`           // JSON map alias -> recap counters; the API sends it as an object
	ExitCode     *int       `json:"exit_code"`
	Log          string     `json:"-"` // hanya via endpoint log
	CreatedAt    time.Time  `json:"created_at"`
	StartedAt    *time.Time `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at"`
}

var ErrAnsiblePlaybookNameTaken = errors.New("an ansible playbook with this name already exists")
var ErrAnsiblePlaybookNotFound = errors.New("ansible playbook not found")
var ErrAnsibleRunNotFound = errors.New("ansible run not found")

// GetAnsibleEnabled checks whether the ansible feature gate is enabled.
func (d *DB) GetAnsibleEnabled() bool {
	v, err := d.GetSetting("ansible_enabled")
	if err != nil {
		return false
	}
	return v == "1"
}

// SetAnsibleEnabled enables or disables the ansible feature gate.
func (d *DB) SetAnsibleEnabled(enabled bool) error {
	v := "0"
	if enabled {
		v = "1"
	}
	return d.SetSetting("ansible_enabled", v)
}

// CreateAnsiblePlaybook creates a new playbook with case-insensitive unique name.
func (d *DB) CreateAnsiblePlaybook(p *AnsiblePlaybook) (int64, error) {
	normalizeAnsibleSource(p)
	if err := insertAnsiblePlaybook(d, p, time.Now().UTC()); err != nil {
		return 0, err
	}
	return p.ID, nil
}

func insertAnsiblePlaybook(x sqlExecer, p *AnsiblePlaybook, now time.Time) error {
	res, err := x.Exec(`INSERT INTO ansible_playbooks (name, description, content, extra_vars, source, git_url, git_branch, git_path, git_auth, git_token, git_key_id, survey, group_name, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.Description, p.Content, p.ExtraVars, p.Source, p.GitURL, p.GitBranch, p.GitPath, p.GitAuth, p.GitToken, p.GitKeyID, p.Survey, p.GroupName, now, now)
	if err != nil {
		if isUniqueConstraintError(err) {
			return fmt.Errorf("%w: %q", ErrAnsiblePlaybookNameTaken, p.Name)
		}
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	p.ID, p.CreatedAt, p.UpdatedAt = id, now, now
	return nil
}

func normalizeAnsibleSource(p *AnsiblePlaybook) {
	if p.Source == "" {
		p.Source = "inline"
	}
	if p.GitAuth == "" {
		p.GitAuth = "none"
	}
	if p.GitAuth != "token" {
		p.GitToken = ""
	}
	if p.GitAuth != "ssh_key" {
		p.GitKeyID = 0
	}
}

const ansiblePlaybookSelect = `
	SELECT p.id, p.name, p.description, %s, p.extra_vars, p.source, p.git_url, p.git_branch, p.git_path, p.git_auth,
	       p.git_token, p.git_key_id, p.survey, p.last_sync_at, p.last_sync_commit, p.group_name, p.created_at, p.updated_at,
	       r.status, r.created_at, r.id, r.started_at, r.finished_at, r.targets, r.recap
	FROM ansible_playbooks p
	LEFT JOIN ansible_runs r ON r.id = (
		SELECT r2.id FROM ansible_runs r2 WHERE r2.playbook_id = p.id ORDER BY r2.id DESC LIMIT 1
	)`

func scanAnsiblePlaybook(sc rowScanner) (*AnsiblePlaybook, error) {
	var p AnsiblePlaybook
	var lastStatus, lastTargets, lastRecap sql.NullString
	var lastRunAt, lastSyncAt, lastStarted, lastFinished sql.NullTime
	var lastRunID sql.NullInt64
	if err := sc.Scan(&p.ID, &p.Name, &p.Description, &p.Content, &p.ExtraVars, &p.Source, &p.GitURL, &p.GitBranch,
		&p.GitPath, &p.GitAuth, &p.GitToken, &p.GitKeyID, &p.Survey, &lastSyncAt, &p.LastSyncCommit, &p.GroupName,
		&p.CreatedAt, &p.UpdatedAt, &lastStatus, &lastRunAt, &lastRunID, &lastStarted, &lastFinished, &lastTargets, &lastRecap); err != nil {
		return nil, err
	}
	if lastSyncAt.Valid {
		p.LastSyncAt = &lastSyncAt.Time
	}
	p.GitTokenSet = p.GitToken != ""
	if lastStatus.Valid {
		p.LastStatus = lastStatus.String
	}
	if lastRunAt.Valid {
		p.LastRunAt = &lastRunAt.Time
	}
	if lastRunID.Valid {
		p.LastRunID = lastRunID.Int64
		if lastStarted.Valid && lastFinished.Valid {
			ms := lastFinished.Time.Sub(lastStarted.Time).Milliseconds()
			p.LastDurationMs = &ms
		}
		if n, ok := countRunTargets(lastTargets.String); ok {
			p.LastTargets = &n
		}
		p.LastRecap = lastRecap.String
	}
	return &p, nil
}

// GetAnsiblePlaybook retrieves a playbook by ID including the last-run and schedule summaries.
func (d *DB) GetAnsiblePlaybook(id int64) (*AnsiblePlaybook, error) {
	p, err := scanAnsiblePlaybook(d.QueryRow(fmt.Sprintf(ansiblePlaybookSelect, "p.content")+` WHERE p.id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, ErrAnsiblePlaybookNotFound
	}
	if err != nil {
		return nil, err
	}
	sums, err := d.ansibleScheduleSummaries(id)
	if err != nil {
		return nil, err
	}
	p.applyScheduleSummary(sums[id])
	return p, nil
}

// ListAnsiblePlaybooks lists all playbooks without Content, ordered by name COLLATE NOCASE, including the
// last-run summary and the schedule summary.
func (d *DB) ListAnsiblePlaybooks() ([]AnsiblePlaybook, error) {
	rows, err := d.Query(fmt.Sprintf(ansiblePlaybookSelect, "''") + ` ORDER BY p.name COLLATE NOCASE ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]AnsiblePlaybook, 0)
	for rows.Next() {
		p, err := scanAnsiblePlaybook(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	sums, err := d.ansibleScheduleSummaries(0)
	if err != nil {
		return nil, err
	}
	for i := range list {
		list[i].applyScheduleSummary(sums[list[i].ID])
	}
	return list, nil
}

// UpdateAnsiblePlaybook updates metadata and source data. The stored git token is kept unless p.GitToken is
// non-empty (it is dropped when GitAuth is not "token").
func (d *DB) UpdateAnsiblePlaybook(p *AnsiblePlaybook) error {
	now := time.Now().UTC()
	normalizeAnsibleSource(p)
	res, err := d.Exec(`
		UPDATE ansible_playbooks
		SET name = ?, description = ?, content = ?, extra_vars = ?, source = ?, git_url = ?, git_branch = ?, git_path = ?,
		    git_auth = ?, git_key_id = ?, survey = ?, group_name = ?,
		    git_token = CASE WHEN ? <> 'token' THEN '' WHEN ? <> '' THEN ? ELSE git_token END,
		    updated_at = ?
		WHERE id = ?
	`, p.Name, p.Description, p.Content, p.ExtraVars, p.Source, p.GitURL, p.GitBranch, p.GitPath,
		p.GitAuth, p.GitKeyID, p.Survey, p.GroupName, p.GitAuth, p.GitToken, p.GitToken, now, p.ID)
	if err != nil {
		if isUniqueConstraintError(err) {
			return ErrAnsiblePlaybookNameTaken
		}
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrAnsiblePlaybookNotFound
	}
	p.UpdatedAt = now
	return nil
}

// SetAnsiblePlaybookSurvey stores the survey definition (a validated JSON array, "" for none). It leaves updated_at
// alone: editing a form is not a change to the playbook.
func (d *DB) SetAnsiblePlaybookSurvey(id int64, survey string) error {
	res, err := d.Exec(`UPDATE ansible_playbooks SET survey = ? WHERE id = ?`, survey, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrAnsiblePlaybookNotFound
	}
	return nil
}

// SetAnsiblePlaybookGitSync records the result of a successful Sync Now.
func (d *DB) SetAnsiblePlaybookGitSync(id int64, at time.Time, commit string) error {
	res, err := d.Exec(`UPDATE ansible_playbooks SET last_sync_at = ?, last_sync_commit = ? WHERE id = ?`, at.UTC(), commit, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrAnsiblePlaybookNotFound
	}
	return nil
}

// ImportAnsiblePlaybooks creates every playbook of pbs in one transaction (all or nothing). A taken name aborts
func (d *DB) ImportAnsiblePlaybooks(pbs []*AnsiblePlaybook) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	for _, p := range pbs {
		normalizeAnsibleSource(p)
		if err := insertAnsiblePlaybook(tx, p, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteAnsiblePlaybook deletes a playbook with all its runs and schedules in a transaction.
func (d *DB) DeleteAnsiblePlaybook(id int64) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM ansible_schedules WHERE playbook_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM ansible_runs WHERE playbook_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.Exec(`DELETE FROM ansible_playbooks WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrAnsiblePlaybookNotFound
	}
	return tx.Commit()
}

// CreateAnsibleRun creates a new run with initial status "queued".
func (d *DB) CreateAnsibleRun(r *AnsibleRun) (int64, error) {
	if r.Status == "" {
		r.Status = "queued"
	}
	if r.Trigger == "" {
		r.Trigger = "manual"
	}
	now := time.Now().UTC()
	becomeInt := 0
	if r.Become {
		becomeInt = 1
	}
	res, err := d.Exec(`
		INSERT INTO ansible_runs (
			playbook_id, playbook_name, status, mode, targets, extra_vars,
			tags, skip_tags, verbosity, become, trigger_kind, schedule_id, commit_sha, limit_pattern, exit_code, log, created_at, started_at, finished_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, r.PlaybookID, r.PlaybookName, r.Status, r.Mode, r.Targets, r.ExtraVars,
		r.Tags, r.SkipTags, r.Verbosity, becomeInt, r.Trigger, r.ScheduleID, r.CommitSHA, r.Limit, r.ExitCode, r.Log, now, r.StartedAt, r.FinishedAt)
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

// SetAnsibleRunCommit records the git commit a run executed.
func (d *DB) SetAnsibleRunCommit(id int64, sha string) error {
	_, err := d.Exec(`UPDATE ansible_runs SET commit_sha = ? WHERE id = ?`, sha, id)
	return err
}

// StartAnsibleRun transitions a run to status "running" and records started_at = now.
func (d *DB) StartAnsibleRun(id int64) error {
	now := time.Now().UTC()
	res, err := d.Exec(`UPDATE ansible_runs SET status = 'running', started_at = ? WHERE id = ?`, now, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrAnsibleRunNotFound
	}
	return nil
}

// FinishAnsibleRun marks a run as finished, setting status, exit code, log, recap (JSON, "" = none) and finished_at = now.
func (d *DB) FinishAnsibleRun(id int64, status string, exitCode *int, log, recap string) error {
	now := time.Now().UTC()
	res, err := d.Exec(`
		UPDATE ansible_runs
		SET status = ?, exit_code = ?, log = ?, recap = ?, finished_at = ?
		WHERE id = ?
	`, status, exitCode, log, recap, now, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrAnsibleRunNotFound
	}
	return nil
}

const ansibleRunCols = `id, playbook_id, playbook_name, status, mode, targets, extra_vars, tags, skip_tags, verbosity, become,
	trigger_kind, schedule_id, commit_sha, limit_pattern, recap, exit_code, created_at, started_at, finished_at`

func scanAnsibleRun(sc rowScanner, withLog bool) (*AnsibleRun, error) {
	var r AnsibleRun
	var becomeInt int
	var exitCode sql.NullInt64
	var startedAt, finishedAt sql.NullTime
	dest := []any{&r.ID, &r.PlaybookID, &r.PlaybookName, &r.Status, &r.Mode, &r.Targets, &r.ExtraVars, &r.Tags, &r.SkipTags,
		&r.Verbosity, &becomeInt, &r.Trigger, &r.ScheduleID, &r.CommitSHA, &r.Limit, &r.Recap, &exitCode, &r.CreatedAt, &startedAt, &finishedAt}
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

// GetAnsibleRun retrieves a run by ID, including its Log.
func (d *DB) GetAnsibleRun(id int64) (*AnsibleRun, error) {
	r, err := scanAnsibleRun(d.QueryRow(`SELECT `+ansibleRunCols+`, log FROM ansible_runs WHERE id = ?`, id), true)
	if err == sql.ErrNoRows {
		return nil, ErrAnsibleRunNotFound
	}
	return r, err
}

// ListAnsibleRuns lists runs without Log, newest first. If playbookID > 0 it filters by playbook and a non-empty
// status filters by run status. Limit defaults to 50, capped at 200.
func (d *DB) ListAnsibleRuns(playbookID int64, status string, limit int) ([]AnsibleRun, error) {
	if limit <= 0 {
		limit = 50
	} else if limit > 200 {
		limit = 200
	}

	rows, err := d.Query(`SELECT `+ansibleRunCols+` FROM ansible_runs WHERE (? = 0 OR playbook_id = ?) AND (? = '' OR status = ?) ORDER BY id DESC LIMIT ?`,
		playbookID, playbookID, status, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]AnsibleRun, 0)
	for rows.Next() {
		r, err := scanAnsibleRun(rows, false)
		if err != nil {
			return nil, err
		}
		list = append(list, *r)
	}
	return list, rows.Err()
}

// PruneAnsibleRuns keeps the newest `keep` finished runs for a playbook (or across all if playbookID == 0).
// Only finished runs (status NOT IN ('queued', 'running')) are pruned; running/queued runs are never deleted.
func (d *DB) PruneAnsibleRuns(playbookID int64, keep int) error {
	if keep < 0 {
		keep = 0
	}
	finishedFilter := "status NOT IN ('queued', 'running')"
	if playbookID > 0 {
		_, err := d.Exec(`
			DELETE FROM ansible_runs
			WHERE playbook_id = ? AND `+finishedFilter+` AND id NOT IN (
				SELECT id FROM ansible_runs
				WHERE playbook_id = ? AND `+finishedFilter+`
				ORDER BY id DESC
				LIMIT ?
			)
		`, playbookID, playbookID, keep)
		return err
	}

	_, err := d.Exec(`
		DELETE FROM ansible_runs
		WHERE `+finishedFilter+` AND id NOT IN (
			SELECT id FROM ansible_runs
			WHERE `+finishedFilter+`
			ORDER BY id DESC
			LIMIT ?
		)
	`, keep)
	return err
}

// MarkAnsibleRunsInterrupted transitions any queued or running runs to "interrupted".
// Typically called during server startup to clean up leftover runs.
func (d *DB) MarkAnsibleRunsInterrupted() (int64, error) {
	now := time.Now().UTC()
	res, err := d.Exec(`
		UPDATE ansible_runs
		SET status = 'interrupted', finished_at = ?
		WHERE status IN ('queued', 'running')
	`, now)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
