package store

// Persistence for the token-efficient proactive triage worker and the delta-only
// daily briefing. All queries are keyed on a normalized error signature or the
// UTC day so a cache hit costs one indexed SELECT and zero LLM tokens.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// TriageResult is one diagnosis, either from a deterministic fast path, a
// signature-cache hit, or a single-shot LLM call.
type TriageResult struct {
	ID                int64     `json:"id"`
	Signature         string    `json:"signature"`
	AlertID           int64     `json:"alert_id"`
	HostID            int64     `json:"host_id"`
	AlertType         string    `json:"alert_type"`
	Unit              string    `json:"unit"`   // systemd unit / container name, for 1-click remediation
	Source            string    `json:"source"` // fast_path | cache | llm
	RootCause         string    `json:"root_cause"`
	Impact            string    `json:"impact"`
	Remediation       string    `json:"remediation"`
	RemediationAction string    `json:"remediation_action"` // systemd_restart | docker_restart | ""
	TokensUsed        int       `json:"tokens_used"`
	CreatedAt         time.Time `json:"created_at"`
}

const triageCols = `id, signature, alert_id, host_id, alert_type, unit, source, root_cause, impact, remediation, remediation_action, tokens_used, created_at`

func scanTriage(sc interface{ Scan(...any) error }) (TriageResult, error) {
	var t TriageResult
	err := sc.Scan(&t.ID, &t.Signature, &t.AlertID, &t.HostID, &t.AlertType, &t.Unit, &t.Source,
		&t.RootCause, &t.Impact, &t.Remediation, &t.RemediationAction, &t.TokensUsed, &t.CreatedAt)
	return t, err
}

// SaveTriageResult persists a diagnosis. The caller decides whether the signature
// is cacheable; only cacheable results should later be read back by signature.
func (d *DB) SaveTriageResult(t *TriageResult) (int64, error) {
	if t == nil {
		return 0, errors.New("triage result cannot be nil")
	}
	if t.Source == "" {
		t.Source = "llm"
	}
	res, err := d.Exec(`INSERT INTO agent_triage
		(signature, alert_id, host_id, alert_type, unit, source, root_cause, impact, remediation, remediation_action, tokens_used)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Signature, t.AlertID, t.HostID, t.AlertType, t.Unit, t.Source,
		t.RootCause, t.Impact, t.Remediation, t.RemediationAction, t.TokensUsed)
	if err != nil {
		return 0, err
	}
	t.ID, err = res.LastInsertId()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}
	return t.ID, err
}

// LatestTriageBySignature returns the most recent diagnosis for a signature
// created at or after since. sql.ErrNoRows means a cache miss.
func (d *DB) LatestTriageBySignature(signature string, since time.Time) (TriageResult, error) {
	row := d.QueryRow(`SELECT `+triageCols+` FROM agent_triage WHERE signature = ? AND created_at >= ? ORDER BY id DESC LIMIT 1`,
		signature, since.UTC())
	t, err := scanTriage(row)
	if err == nil {
		t.Source = "cache"
	}
	return t, err
}

// LatestTriageForAlert returns the diagnosis of one alert, if any.
func (d *DB) LatestTriageForAlert(alertID int64) (TriageResult, error) {
	row := d.QueryRow(`SELECT `+triageCols+` FROM agent_triage WHERE alert_id = ? ORDER BY id DESC LIMIT 1`, alertID)
	return scanTriage(row)
}

// ListRecentTriage lists the newest diagnoses across the fleet.
func (d *DB) ListRecentTriage(limit int) ([]TriageResult, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.Query(`SELECT `+triageCols+` FROM agent_triage ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TriageResult
	for rows.Next() {
		t, err := scanTriage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TriageBudgetDay returns the tokens already spent on triage on the given UTC day.
func (d *DB) TriageBudgetDay(day string) (int, error) {
	var used int
	err := d.QueryRow(`SELECT tokens_used FROM agent_triage_budget WHERE day = ?`, day).Scan(&used)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return used, err
}

// AddTriageBudget atomically adds tokens to a day's spend. SQLite single-writer
// semantics make this safe for the single-process worker.
func (d *DB) AddTriageBudget(day string, tokens int) error {
	if tokens < 0 {
		tokens = 0
	}
	_, err := d.Exec(`INSERT INTO agent_triage_budget(day, tokens_used) VALUES (?, ?)
		ON CONFLICT(day) DO UPDATE SET tokens_used = tokens_used + excluded.tokens_used`, day, tokens)
	return err
}

// Briefing is the delta-only daily infrastructure briefing of one UTC day.
type Briefing struct {
	ID           int64     `json:"id"`
	Day          string    `json:"day"`
	Summary      string    `json:"summary"`
	AnomalyCount int       `json:"anomaly_count"`
	Items        []string  `json:"items"`
	TokensUsed   int       `json:"tokens_used"`
	GeneratedAt  time.Time `json:"generated_at"`
}

// SaveBriefing upserts the briefing of a day.
func (d *DB) SaveBriefing(b *Briefing) error {
	if b == nil {
		return errors.New("briefing cannot be nil")
	}
	items := b.Items
	if items == nil {
		items = []string{}
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return err
	}
	_, err = d.Exec(`INSERT INTO agent_briefing(day, summary, anomaly_count, items_json, tokens_used)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(day) DO UPDATE SET summary = excluded.summary, anomaly_count = excluded.anomaly_count,
		items_json = excluded.items_json, tokens_used = excluded.tokens_used, generated_at = CURRENT_TIMESTAMP`,
		b.Day, b.Summary, b.AnomalyCount, string(raw), b.TokensUsed)
	return err
}

// GetBriefing returns the briefing of a day, or (nil, nil) when none exists.
func (d *DB) GetBriefing(day string) (*Briefing, error) {
	var b Briefing
	var raw string
	err := d.QueryRow(`SELECT id, day, summary, anomaly_count, items_json, tokens_used, generated_at FROM agent_briefing WHERE day = ?`, day).
		Scan(&b.ID, &b.Day, &b.Summary, &b.AnomalyCount, &raw, &b.TokensUsed, &b.GeneratedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(raw), &b.Items); err != nil {
		b.Items = []string{}
	}
	return &b, nil
}

// LatestBriefing returns the most recently generated briefing, or (nil, nil).
func (d *DB) LatestBriefing() (*Briefing, error) {
	var day string
	err := d.QueryRow(`SELECT day FROM agent_briefing ORDER BY generated_at DESC, id DESC LIMIT 1`).Scan(&day)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return d.GetBriefing(day)
}
