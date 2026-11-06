package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// GenerateTFWebhookToken generates 32 random bytes formatted as a 64-char hex string.
func GenerateTFWebhookToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}

// Sentinel errors for Terraform store operations.
var (
	ErrTFWorkspaceNameTaken    = errors.New("a terraform workspace with this name already exists")
	ErrTFWorkspaceNotFound     = errors.New("terraform workspace not found")
	ErrTFWorkspaceLocked       = errors.New("terraform workspace is locked")
	ErrTFRunNotFound           = errors.New("terraform run not found")
	ErrTFStateVersionNotFound  = errors.New("terraform state version not found")
	ErrTFPolicyNameTaken       = errors.New("a terraform policy with this name already exists")
	ErrTFPolicyNotFound        = errors.New("terraform policy not found")
	ErrTFVarsetNameTaken       = errors.New("a terraform varset with this name already exists")
	ErrTFVarsetNotFound        = errors.New("terraform varset not found")
	ErrTFVariableNotFound      = errors.New("terraform variable not found")
	ErrTFModuleNameTaken       = errors.New("a terraform module with this name and provider already exists")
	ErrTFModuleNotFound        = errors.New("terraform module not found")
	ErrTFModuleVersionTaken    = errors.New("a terraform module version already exists")
	ErrTFModuleVersionNotFound = errors.New("terraform module version not found")
)

// GetTerraformEnabled checks whether the terraform feature gate is enabled.
func (d *DB) GetTerraformEnabled() bool {
	v, err := d.GetSetting("terraform_enabled")
	if err != nil {
		return false
	}
	return v == "1"
}

// SetTerraformEnabled enables or disables the terraform feature gate.
func (d *DB) SetTerraformEnabled(enabled bool) error {
	v := "0"
	if enabled {
		v = "1"
	}
	return d.SetSetting("terraform_enabled", v)
}

// TFWorkspaceLastRun stores a summary of the latest run for a workspace.
type TFWorkspaceLastRun struct {
	ID         int64      `json:"id"`
	Type       string     `json:"type"`
	Status     string     `json:"status"`
	FinishedAt *time.Time `json:"finished_at"`
	HasChanges bool       `json:"has_changes"`
}

// TFWorkspace represents a Terraform workspace.
type TFWorkspace struct {
	ID                   int64               `json:"id"`
	Name                 string              `json:"name"`
	Description          string              `json:"description"`
	Environment          string              `json:"environment"`
	GroupName            string              `json:"group_name"`      // free-text label; "" = ungrouped (cosmetic)
	Tags                 string              `json:"tags"`            // JSON array string
	Binary               string              `json:"binary"`          // "terraform" | "tofu"
	Source               string              `json:"source"`          // "inline" | "git"
	Files                string              `json:"files,omitempty"` // JSON string
	GitURL               string              `json:"git_url"`
	GitBranch            string              `json:"git_branch"`
	GitPath              string              `json:"git_path"`
	GitAuth              string              `json:"git_auth"` // "none" | "token" | "ssh"
	GitToken             string              `json:"-"`        // secret: NEVER serialized
	GitTokenSet          bool                `json:"git_token_set"`
	GitKeyID             int64               `json:"git_key_id"`
	WebhookToken         string              `json:"webhook_token"`
	WebhookSecret        string              `json:"-"` // secret: NEVER serialized (except rotate response in handler)
	WebhookEnabled       bool                `json:"webhook_enabled"`
	WebhookURL           string              `json:"webhook_url,omitempty"`
	DriftIntervalMinutes int                 `json:"drift_interval_minutes"`
	Status               string              `json:"status"` // computed status
	Locked               bool                `json:"locked"`
	LockReason           string              `json:"lock_reason"`
	LockedBy             string              `json:"locked_by"` // "manual" | "run:<id>" | ""
	LockedAt             *time.Time          `json:"locked_at"`
	DriftDetected        bool                `json:"drift_detected"`
	DriftCheckedAt       *time.Time          `json:"drift_checked_at"`
	TFVersion            string              `json:"tf_version"`
	ResourceCount        int                 `json:"resource_count"`
	LastRun              *TFWorkspaceLastRun `json:"last_run"`
	CreatedAt            time.Time           `json:"created_at"`
	UpdatedAt            time.Time           `json:"updated_at"`
}

func normalizeTFWorkspace(w *TFWorkspace) {
	if w.Binary == "" {
		w.Binary = "terraform"
	}
	if w.Source == "" {
		w.Source = "inline"
	}
	if w.Files == "" {
		w.Files = "{}"
	}
	if w.Tags == "" {
		w.Tags = "[]"
	}
	if w.GitAuth == "" {
		w.GitAuth = "none"
	}
	if w.GitAuth != "token" {
		w.GitToken = ""
	}
	if w.GitAuth != "ssh" {
		w.GitKeyID = 0
	}
	if w.WebhookToken == "" {
		w.WebhookToken = GenerateTFWebhookToken()
	}
}

// CreateTFWorkspace creates a new workspace record.
func (d *DB) CreateTFWorkspace(w *TFWorkspace) (int64, error) {
	normalizeTFWorkspace(w)
	now := time.Now().UTC()
	res, err := d.Exec(`
		INSERT INTO tf_workspaces (
			name, description, environment, tags, binary, source, files,
			git_url, git_branch, git_path, git_auth, git_token, git_key_id,
			webhook_token, webhook_secret, drift_interval_minutes, group_name,
			lock_reason, locked_by, locked_at, drift_detected, drift_checked_at,
			tf_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		w.Name, w.Description, w.Environment, w.Tags, w.Binary, w.Source, w.Files,
		w.GitURL, w.GitBranch, w.GitPath, w.GitAuth, w.GitToken, w.GitKeyID,
		w.WebhookToken, w.WebhookSecret, w.DriftIntervalMinutes, w.GroupName,
		w.LockReason, w.LockedBy, w.LockedAt, boolToInt(w.DriftDetected), w.DriftCheckedAt,
		w.TFVersion, now, now,
	)
	if err != nil {
		if isUniqueConstraintError(err) {
			return 0, fmt.Errorf("%w: %q", ErrTFWorkspaceNameTaken, w.Name)
		}
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	w.ID = id
	w.CreatedAt = now
	w.UpdatedAt = now
	return id, nil
}

// UpdateTFWorkspace updates workspace settings/declarations.
func (d *DB) UpdateTFWorkspace(w *TFWorkspace) error {
	normalizeTFWorkspace(w)
	now := time.Now().UTC()
	var res sql.Result
	var err error
	if w.GitAuth == "token" && w.GitToken == "" {
		// Preserve existing token if empty
		res, err = d.Exec(`
			UPDATE tf_workspaces SET
				name = ?, description = ?, environment = ?, tags = ?, binary = ?, source = ?, files = ?,
				git_url = ?, git_branch = ?, git_path = ?, git_auth = ?, git_key_id = ?,
				drift_interval_minutes = ?, group_name = ?, updated_at = ?
			WHERE id = ?
		`,
			w.Name, w.Description, w.Environment, w.Tags, w.Binary, w.Source, w.Files,
			w.GitURL, w.GitBranch, w.GitPath, w.GitAuth, w.GitKeyID,
			w.DriftIntervalMinutes, w.GroupName, now, w.ID,
		)
	} else {
		res, err = d.Exec(`
			UPDATE tf_workspaces SET
				name = ?, description = ?, environment = ?, tags = ?, binary = ?, source = ?, files = ?,
				git_url = ?, git_branch = ?, git_path = ?, git_auth = ?, git_token = ?, git_key_id = ?,
				drift_interval_minutes = ?, group_name = ?, updated_at = ?
			WHERE id = ?
		`,
			w.Name, w.Description, w.Environment, w.Tags, w.Binary, w.Source, w.Files,
			w.GitURL, w.GitBranch, w.GitPath, w.GitAuth, w.GitToken, w.GitKeyID,
			w.DriftIntervalMinutes, w.GroupName, now, w.ID,
		)
	}
	if err != nil {
		if isUniqueConstraintError(err) {
			return fmt.Errorf("%w: %q", ErrTFWorkspaceNameTaken, w.Name)
		}
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTFWorkspaceNotFound
	}
	w.UpdatedAt = now
	return nil
}

// DeleteTFWorkspace deletes a workspace by ID. Cascades delete to runs, state versions, and variables.
func (d *DB) DeleteTFWorkspace(id int64) error {
	res, err := d.Exec(`DELETE FROM tf_workspaces WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTFWorkspaceNotFound
	}
	return nil
}

const tfWorkspaceCols = `
	w.id, w.name, w.description, w.environment, w.tags, w.binary, w.source, %s,
	w.git_url, w.git_branch, w.git_path, w.git_auth, w.git_token, w.git_key_id,
	w.webhook_token, w.webhook_secret, w.drift_interval_minutes, w.group_name,
	w.lock_reason, w.locked_by, w.locked_at, w.drift_detected, w.drift_checked_at,
	w.tf_version, w.created_at, w.updated_at
`

func scanTFWorkspace(sc rowScanner, withFiles bool) (*TFWorkspace, error) {
	var w TFWorkspace
	var lockedAt, driftCheckedAt sql.NullTime
	var driftDetectedInt int
	var files string
	dest := []any{
		&w.ID, &w.Name, &w.Description, &w.Environment, &w.Tags, &w.Binary, &w.Source, &files,
		&w.GitURL, &w.GitBranch, &w.GitPath, &w.GitAuth, &w.GitToken, &w.GitKeyID,
		&w.WebhookToken, &w.WebhookSecret, &w.DriftIntervalMinutes, &w.GroupName,
		&w.LockReason, &w.LockedBy, &lockedAt, &driftDetectedInt, &driftCheckedAt,
		&w.TFVersion, &w.CreatedAt, &w.UpdatedAt,
	}
	if err := sc.Scan(dest...); err != nil {
		return nil, err
	}
	if withFiles {
		w.Files = files
	}
	w.GitTokenSet = w.GitToken != ""
	w.WebhookEnabled = w.WebhookToken != ""
	w.Locked = w.LockedBy != ""
	if lockedAt.Valid {
		w.LockedAt = &lockedAt.Time
	}
	w.DriftDetected = driftDetectedInt != 0
	if driftCheckedAt.Valid {
		w.DriftCheckedAt = &driftCheckedAt.Time
	}
	return &w, nil
}

func (d *DB) populateWorkspaceExtras(w *TFWorkspace) error {
	// 1. Latest resource_count from latest state version
	var resCount sql.NullInt64
	err := d.QueryRow(`SELECT resource_count FROM tf_state_versions WHERE workspace_id = ? ORDER BY id DESC LIMIT 1`, w.ID).Scan(&resCount)
	if err == nil && resCount.Valid {
		w.ResourceCount = int(resCount.Int64)
	}

	// 2. Last run
	var lr TFWorkspaceLastRun
	var hasChangesInt int
	var finishedAt sql.NullTime
	err = d.QueryRow(`
		SELECT id, type, status, finished_at, has_changes
		FROM tf_runs
		WHERE workspace_id = ?
		ORDER BY id DESC LIMIT 1
	`, w.ID).Scan(&lr.ID, &lr.Type, &lr.Status, &finishedAt, &hasChangesInt)
	if err == nil {
		lr.HasChanges = hasChangesInt != 0
		if finishedAt.Valid {
			lr.FinishedAt = &finishedAt.Time
		}
		w.LastRun = &lr
	}

	// 3. Status computation
	// Priority: locked (if manual) -> running (active run) -> awaiting_approval -> errored (last run) -> drifted -> applied -> never
	if w.LockedBy == "manual" {
		w.Status = "locked"
	} else if w.LastRun != nil && (w.LastRun.Status == "queued" || w.LastRun.Status == "planning" || w.LastRun.Status == "applying") {
		w.Status = "running"
	} else if w.LastRun != nil && w.LastRun.Status == "awaiting_approval" {
		w.Status = "awaiting_approval"
	} else if w.LastRun != nil && (w.LastRun.Status == "errored" || w.LastRun.Status == "policy_failed") {
		w.Status = "errored"
	} else if w.DriftDetected {
		w.Status = "drifted"
	} else if w.LastRun != nil && w.LastRun.Status == "applied" {
		w.Status = "applied"
	} else {
		w.Status = "never"
	}

	return nil
}

// GetTFWorkspace retrieves a workspace by ID, including Files and computed extras.
func (d *DB) GetTFWorkspace(id int64) (*TFWorkspace, error) {
	q := fmt.Sprintf("SELECT "+tfWorkspaceCols+" FROM tf_workspaces w WHERE w.id = ?", "w.files")
	w, err := scanTFWorkspace(d.QueryRow(q, id), true)
	if err == sql.ErrNoRows {
		return nil, ErrTFWorkspaceNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := d.populateWorkspaceExtras(w); err != nil {
		return nil, err
	}
	return w, nil
}

// ListTFWorkspaces lists workspaces matching optional environment, tag, or search query.
// Omits Files to keep responses light.
func (d *DB) ListTFWorkspaces(env, tag, query string) ([]TFWorkspace, error) {
	sqlStr := fmt.Sprintf("SELECT "+tfWorkspaceCols+" FROM tf_workspaces w WHERE 1=1", "''")
	args := []any{}
	if env != "" {
		sqlStr += " AND w.environment = ?"
		args = append(args, env)
	}
	if tag != "" {
		sqlStr += " AND (w.tags LIKE ? OR w.tags LIKE ?)"
		args = append(args, `%"tag":%"`+tag+`"%`, `%"`+tag+`"%`)
	}
	if query != "" {
		sqlStr += " AND (w.name LIKE ? COLLATE NOCASE OR w.description LIKE ? COLLATE NOCASE)"
		args = append(args, "%"+query+"%", "%"+query+"%")
	}
	sqlStr += " ORDER BY w.name COLLATE NOCASE ASC"

	rows, err := d.Query(sqlStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]TFWorkspace, 0)
	for rows.Next() {
		w, err := scanTFWorkspace(rows, false)
		if err != nil {
			return nil, err
		}
		if err := d.populateWorkspaceExtras(w); err != nil {
			return nil, err
		}
		list = append(list, *w)
	}
	return list, rows.Err()
}

// TryLockTFWorkspace atomically locks the workspace if it is currently unlocked (locked_by = ”).
// Returns true on success, false if already locked.
func (d *DB) TryLockTFWorkspace(id int64, by, reason string) (bool, error) {
	now := time.Now().UTC()
	res, err := d.Exec(`
		UPDATE tf_workspaces
		SET locked_by = ?, lock_reason = ?, locked_at = ?
		WHERE id = ? AND locked_by = ''
	`, by, reason, now, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// SetTFWorkspaceLock unconditionally sets the lock on a workspace (or returns ErrTFWorkspaceNotFound).
func (d *DB) SetTFWorkspaceLock(id int64, by, reason string) error {
	now := time.Now().UTC()
	res, err := d.Exec(`
		UPDATE tf_workspaces
		SET locked_by = ?, lock_reason = ?, locked_at = ?
		WHERE id = ?
	`, by, reason, now, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTFWorkspaceNotFound
	}
	return nil
}

// UnlockTFWorkspace unlocks the workspace.
func (d *DB) UnlockTFWorkspace(id int64) error {
	res, err := d.Exec(`
		UPDATE tf_workspaces
		SET locked_by = '', lock_reason = '', locked_at = NULL
		WHERE id = ?
	`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTFWorkspaceNotFound
	}
	return nil
}

// RotateTFWorkspaceWebhook generates a new webhook token and secret.
func (d *DB) RotateTFWorkspaceWebhook(id int64) (token, secret string, err error) {
	token = GenerateTFWebhookToken()
	secret = GenerateTFWebhookToken()
	res, err := d.Exec(`
		UPDATE tf_workspaces
		SET webhook_token = ?, webhook_secret = ?, updated_at = ?
		WHERE id = ?
	`, token, secret, time.Now().UTC(), id)
	if err != nil {
		return "", "", err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return "", "", err
	}
	if n == 0 {
		return "", "", ErrTFWorkspaceNotFound
	}
	return token, secret, nil
}

// UpdateTFWorkspaceDrift records drift detection results.
func (d *DB) UpdateTFWorkspaceDrift(id int64, detected bool) error {
	now := time.Now().UTC()
	res, err := d.Exec(`
		UPDATE tf_workspaces
		SET drift_detected = ?, drift_checked_at = ?, updated_at = ?
		WHERE id = ?
	`, boolToInt(detected), now, now, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTFWorkspaceNotFound
	}
	return nil
}

// UpdateTFWorkspaceVersion records the detected terraform/tofu version.
func (d *DB) UpdateTFWorkspaceVersion(id int64, version string) error {
	res, err := d.Exec(`UPDATE tf_workspaces SET tf_version = ? WHERE id = ?`, version, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTFWorkspaceNotFound
	}
	return nil
}

// --- TFRun Struct and Methods ---

// TFRun represents an execution run on a workspace.
type TFRun struct {
	ID                int64      `json:"id"`
	WorkspaceID       int64      `json:"workspace_id"`
	Type              string     `json:"type"` // "plan" | "destroy" | "refresh" | "drift"
	Speculative       bool       `json:"speculative"`
	Status            string     `json:"status"`       // queued|planning|awaiting_approval|applying|applied|no_changes|planned|policy_failed|errored|discarded|canceled|interrupted
	TriggerKind       string     `json:"trigger_kind"` // manual | webhook | drift
	Message           string     `json:"message"`
	Targets           string     `json:"targets"` // JSON array
	Replace           string     `json:"replace"` // JSON array
	CommitSHA         string     `json:"commit_sha"`
	PlanSummary       string     `json:"plan_summary"` // JSON object
	Drift             string     `json:"drift"`        // JSON array
	Policy            string     `json:"policy"`       // JSON object (PolicyReport)
	Cost              string     `json:"cost"`         // JSON object (CostEstimate)
	HasChanges        bool       `json:"has_changes"`
	ExitCode          *int       `json:"exit_code"`
	Log               string     `json:"-"` // secret / streamed: NEVER serialized
	ApprovedByComment string     `json:"approved_by_comment"`
	Decision          string     `json:"decision"` // "applied" | "discarded" | ""
	DecisionComment   string     `json:"decision_comment"`
	CreatedAt         time.Time  `json:"created_at"`
	StartedAt         *time.Time `json:"started_at"`
	FinishedAt        *time.Time `json:"finished_at"`
}

const tfRunCols = `
	id, workspace_id, type, speculative, status, trigger_kind, message,
	targets, replace, commit_sha, plan_summary, drift, policy, cost,
	has_changes, exit_code, approved_by_comment, decision, decision_comment,
	created_at, started_at, finished_at
`

func scanTFRun(sc rowScanner, withLog bool) (*TFRun, error) {
	var r TFRun
	var specInt, hasChangesInt int
	var exitCode sql.NullInt64
	var startedAt, finishedAt sql.NullTime
	dest := []any{
		&r.ID, &r.WorkspaceID, &r.Type, &specInt, &r.Status, &r.TriggerKind, &r.Message,
		&r.Targets, &r.Replace, &r.CommitSHA, &r.PlanSummary, &r.Drift, &r.Policy, &r.Cost,
		&hasChangesInt, &exitCode, &r.ApprovedByComment, &r.Decision, &r.DecisionComment,
		&r.CreatedAt, &startedAt, &finishedAt,
	}
	if withLog {
		dest = append(dest, &r.Log)
	}
	if err := sc.Scan(dest...); err != nil {
		return nil, err
	}
	r.Speculative = specInt != 0
	r.HasChanges = hasChangesInt != 0
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

// CreateTFRun creates a new run with status "queued".
func (d *DB) CreateTFRun(r *TFRun) (int64, error) {
	if r.Status == "" {
		r.Status = "queued"
	}
	if r.Targets == "" {
		r.Targets = "[]"
	}
	if r.Replace == "" {
		r.Replace = "[]"
	}
	if r.PlanSummary == "" {
		r.PlanSummary = "{}"
	}
	if r.Drift == "" {
		r.Drift = "[]"
	}
	if r.Policy == "" {
		r.Policy = "{}"
	}
	if r.Cost == "" {
		r.Cost = "{}"
	}
	now := time.Now().UTC()
	res, err := d.Exec(`
		INSERT INTO tf_runs (
			workspace_id, type, speculative, status, trigger_kind, message,
			targets, replace, commit_sha, plan_summary, drift, policy, cost,
			has_changes, exit_code, log, approved_by_comment, decision, decision_comment,
			created_at, started_at, finished_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		r.WorkspaceID, r.Type, boolToInt(r.Speculative), r.Status, r.TriggerKind, r.Message,
		r.Targets, r.Replace, r.CommitSHA, r.PlanSummary, r.Drift, r.Policy, r.Cost,
		boolToInt(r.HasChanges), r.ExitCode, r.Log, r.ApprovedByComment, r.Decision, r.DecisionComment,
		now, r.StartedAt, r.FinishedAt,
	)
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

// GetTFRun retrieves a run by ID, including its Log.
func (d *DB) GetTFRun(id int64) (*TFRun, error) {
	r, err := scanTFRun(d.QueryRow(`SELECT `+tfRunCols+`, log FROM tf_runs WHERE id = ?`, id), true)
	if err == sql.ErrNoRows {
		return nil, ErrTFRunNotFound
	}
	return r, err
}

// ListTFRuns lists runs for a workspace (or all if wsID == 0), newest first, without Log.
func (d *DB) ListTFRuns(workspaceID int64, limit int) ([]TFRun, error) {
	if limit <= 0 {
		limit = 50
	} else if limit > 200 {
		limit = 200
	}
	rows, err := d.Query(`
		SELECT `+tfRunCols+`
		FROM tf_runs
		WHERE (? = 0 OR workspace_id = ?)
		ORDER BY id DESC LIMIT ?
	`, workspaceID, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]TFRun, 0)
	for rows.Next() {
		r, err := scanTFRun(rows, false)
		if err != nil {
			return nil, err
		}
		list = append(list, *r)
	}
	return list, rows.Err()
}

// UpdateTFRunStatus updates run status, optional exit code, and log.
func (d *DB) UpdateTFRunStatus(id int64, status string, exitCode *int, log string) error {
	res, err := d.Exec(`
		UPDATE tf_runs
		SET status = ?, exit_code = ?, log = ?
		WHERE id = ?
	`, status, exitCode, log, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTFRunNotFound
	}
	return nil
}

// FinishTFRun marks a run as finished.
func (d *DB) FinishTFRun(id int64, status string, exitCode *int, log string) error {
	now := time.Now().UTC()
	res, err := d.Exec(`
		UPDATE tf_runs
		SET status = ?, exit_code = ?, log = ?, finished_at = ?
		WHERE id = ?
	`, status, exitCode, log, now, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTFRunNotFound
	}
	return nil
}

// UpdateTFRunPlan saves plan evaluation results (plan_summary, drift, policy, cost, has_changes, status, commit_sha).
func (d *DB) UpdateTFRunPlan(id int64, status string, planSummary, drift, policy, cost, commitSHA string, hasChanges bool) error {
	res, err := d.Exec(`
		UPDATE tf_runs
		SET status = ?, plan_summary = ?, drift = ?, policy = ?, cost = ?, commit_sha = ?, has_changes = ?
		WHERE id = ?
	`, status, planSummary, drift, policy, cost, commitSHA, boolToInt(hasChanges), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTFRunNotFound
	}
	return nil
}

// RecordTFRunDecision records approval/discard decision on a run.
func (d *DB) RecordTFRunDecision(id int64, decision, comment, approvedByComment string) error {
	res, err := d.Exec(`
		UPDATE tf_runs
		SET decision = ?, decision_comment = ?, approved_by_comment = ?
		WHERE id = ?
	`, decision, comment, approvedByComment, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTFRunNotFound
	}
	return nil
}

// MarkTFRunsInterrupted transitions queued, planning, or applying runs to "interrupted".
// awaiting_approval runs are NOT touched.
func (d *DB) MarkTFRunsInterrupted() (int64, error) {
	now := time.Now().UTC()
	res, err := d.Exec(`
		UPDATE tf_runs
		SET status = 'interrupted', finished_at = ?
		WHERE status IN ('queued', 'planning', 'applying')
	`, now)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// PruneTFRuns keeps the newest `keep` finished runs for a workspace (or across all if wsID == 0).
// Active runs (queued, planning, awaiting_approval, applying) are never deleted.
func (d *DB) PruneTFRuns(workspaceID int64, keep int) error {
	if keep < 0 {
		keep = 0
	}
	finishedFilter := "status NOT IN ('queued', 'planning', 'awaiting_approval', 'applying')"
	if workspaceID > 0 {
		_, err := d.Exec(`
			DELETE FROM tf_runs
			WHERE workspace_id = ? AND `+finishedFilter+` AND id NOT IN (
				SELECT id FROM tf_runs
				WHERE workspace_id = ? AND `+finishedFilter+`
				ORDER BY id DESC
				LIMIT ?
			)
		`, workspaceID, workspaceID, keep)
		return err
	}

	_, err := d.Exec(`
		DELETE FROM tf_runs
		WHERE `+finishedFilter+` AND id NOT IN (
			SELECT id FROM tf_runs
			WHERE `+finishedFilter+`
			ORDER BY id DESC
			LIMIT ?
		)
	`, keep)
	return err
}

// --- TFStateVersion Struct and Methods ---

// TFStateVersion represents a versioned snapshot of the workspace's state.
type TFStateVersion struct {
	ID            int64     `json:"id"`
	WorkspaceID   int64     `json:"workspace_id"`
	RunID         int64     `json:"run_id"`
	Serial        int64     `json:"serial"`
	Lineage       string    `json:"lineage"`
	Source        string    `json:"source"` // apply | refresh | rollback | import
	Size          int64     `json:"size"`
	ResourceCount int       `json:"resource_count"`
	SHA256        string    `json:"sha256"`
	State         []byte    `json:"-"` // raw tfstate: NEVER returned in list/summary
	CreatedAt     time.Time `json:"created_at"`
}

// CreateTFStateVersion inserts a new state version record.
func (d *DB) CreateTFStateVersion(sv *TFStateVersion) (int64, error) {
	if sv.State == nil {
		sv.State = []byte("{}")
	}
	now := time.Now().UTC()
	res, err := d.Exec(`
		INSERT INTO tf_state_versions (
			workspace_id, run_id, serial, lineage, source, size, resource_count, sha256, state, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		sv.WorkspaceID, sv.RunID, sv.Serial, sv.Lineage, sv.Source, sv.Size, sv.ResourceCount, sv.SHA256, sv.State, now,
	)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	sv.ID = id
	sv.CreatedAt = now
	return id, nil
}

// GetTFStateVersion retrieves a state version by ID including its raw State BLOB.
func (d *DB) GetTFStateVersion(id int64) (*TFStateVersion, error) {
	var sv TFStateVersion
	err := d.QueryRow(`
		SELECT id, workspace_id, run_id, serial, lineage, source, size, resource_count, sha256, state, created_at
		FROM tf_state_versions
		WHERE id = ?
	`, id).Scan(
		&sv.ID, &sv.WorkspaceID, &sv.RunID, &sv.Serial, &sv.Lineage, &sv.Source, &sv.Size, &sv.ResourceCount, &sv.SHA256, &sv.State, &sv.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrTFStateVersionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &sv, nil
}

// LatestTFStateVersion retrieves the most recent state version for a workspace including State bytes.
func (d *DB) LatestTFStateVersion(workspaceID int64) (*TFStateVersion, error) {
	var sv TFStateVersion
	err := d.QueryRow(`
		SELECT id, workspace_id, run_id, serial, lineage, source, size, resource_count, sha256, state, created_at
		FROM tf_state_versions
		WHERE workspace_id = ?
		ORDER BY id DESC LIMIT 1
	`, workspaceID).Scan(
		&sv.ID, &sv.WorkspaceID, &sv.RunID, &sv.Serial, &sv.Lineage, &sv.Source, &sv.Size, &sv.ResourceCount, &sv.SHA256, &sv.State, &sv.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil // No state yet
	}
	if err != nil {
		return nil, err
	}
	return &sv, nil
}

// ListTFStateVersions lists state versions for a workspace without the State BLOB, newest first.
func (d *DB) ListTFStateVersions(workspaceID int64, limit int) ([]TFStateVersion, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.Query(`
		SELECT id, workspace_id, run_id, serial, lineage, source, size, resource_count, sha256, created_at
		FROM tf_state_versions
		WHERE workspace_id = ?
		ORDER BY id DESC LIMIT ?
	`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]TFStateVersion, 0)
	for rows.Next() {
		var sv TFStateVersion
		if err := rows.Scan(
			&sv.ID, &sv.WorkspaceID, &sv.RunID, &sv.Serial, &sv.Lineage, &sv.Source, &sv.Size, &sv.ResourceCount, &sv.SHA256, &sv.CreatedAt,
		); err != nil {
			return nil, err
		}
		list = append(list, sv)
	}
	return list, rows.Err()
}

// PruneTFStateVersions keeps the newest `keep` state versions for a workspace.
// Never prunes the newest state version even if keep is 0 (keep defaults to at least 1).
func (d *DB) PruneTFStateVersions(workspaceID int64, keep int) error {
	if keep < 1 {
		keep = 1 // Never prune the newest state version
	}
	_, err := d.Exec(`
		DELETE FROM tf_state_versions
		WHERE workspace_id = ? AND id NOT IN (
			SELECT id FROM tf_state_versions
			WHERE workspace_id = ?
			ORDER BY id DESC
			LIMIT ?
		)
	`, workspaceID, workspaceID, keep)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
