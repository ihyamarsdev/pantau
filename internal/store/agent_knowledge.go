package store

// Procedural memory of the AI agent: runbooks and blueprints distilled from finished threads, operator-written
// custom skills, and one FTS5 index over runbooks and memory notes. The index is kept in step by the writes
// below, in the same call, so a search never sees a deleted row or misses a new one.
//
// The callers redact secrets before they write (the store cannot import the inspector package that does it).

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Knowledge kinds and domains. A runbook or blueprint applies to one domain, or to every domain ("global").
var (
	AgentRunbookKinds   = []string{"runbook", "blueprint"}
	AgentKnowledgeDoms  = []string{"global", "host", "kubernetes", "docker", "ansible", "terraform", "security"}
	ErrAgentKnowledge   = errors.New("runbook not found")
	ErrAgentSkillFields = errors.New("a custom skill needs a name, a known domain and content of at most 4000 characters")
)

const (
	agentRunbookStepsMax   = 16 * 1024
	agentSkillContentMax   = 4000
	agentKnowledgeTitleMax = 120
)

// migrateAgentKnowledge creates the runbook, custom skill and FTS tables, and indexes the rows that exist. Idempotent.
func (d *DB) migrateAgentKnowledge() error {
	schema := `
	CREATE TABLE IF NOT EXISTS agent_runbooks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		kind TEXT NOT NULL DEFAULT 'runbook',
		title TEXT NOT NULL,
		domain TEXT NOT NULL DEFAULT 'global',
		scope_id INTEGER NOT NULL DEFAULT 0,
		trigger_summary TEXT NOT NULL DEFAULT '',
		steps_json TEXT NOT NULL DEFAULT '{}',
		tags TEXT NOT NULL DEFAULT '',
		source_thread_id INTEGER NOT NULL DEFAULT 0,
		usage_count INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_agent_runbooks_domain ON agent_runbooks(domain, updated_at DESC);
	CREATE TABLE IF NOT EXISTS agent_custom_skills (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		domain TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		content_md TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE VIRTUAL TABLE IF NOT EXISTS agent_knowledge_fts USING fts5(
		kind UNINDEXED, ref_id UNINDEXED, domain UNINDEXED, title, body, tokenize = 'unicode61'
	);`
	if _, err := d.Exec(schema); err != nil {
		return err
	}
	var indexed int
	if err := d.QueryRow(`SELECT count(*) FROM agent_knowledge_fts`).Scan(&indexed); err != nil {
		return err
	}
	if indexed == 0 {
		return d.reindexAllKnowledge()
	}
	return nil
}

// AgentRunbook is one stored runbook or blueprint. Steps holds the JSON object {steps, verify, rollback}.
type AgentRunbook struct {
	ID             int64     `json:"id"`
	Kind           string    `json:"kind"`
	Title          string    `json:"title"`
	Domain         string    `json:"domain"`
	TriggerSummary string    `json:"trigger_summary"`
	StepsJSON      string    `json:"steps_json"`
	Tags           string    `json:"tags"`
	SourceThreadID int64     `json:"source_thread_id"`
	UsageCount     int64     `json:"usage_count"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// AgentCustomSkill is operator-written instruction text injected into the system prompt of one domain.
type AgentCustomSkill struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Domain    string    `json:"domain"`
	Enabled   bool      `json:"enabled"`
	Content   string    `json:"content_md"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// KnowledgeHit is one search result over runbooks and memory notes.
type KnowledgeHit struct {
	Kind    string `json:"kind"` // runbook | memory
	ID      int64  `json:"id"`
	Domain  string `json:"domain"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
}

var knowledgeTokenRe = regexp.MustCompile(`[\p{L}\p{N}]+`)

// FTSMatchQuery turns free text into a safe FTS5 query: each word becomes a quoted token, and the tokens are
// joined with OR. Quoting makes OR, NOT, a dash, a colon or a quote in the input plain text, never an operator.
func FTSMatchQuery(text string) string {
	toks := knowledgeTokenRe.FindAllString(strings.ToLower(text), -1)
	if len(toks) > 12 {
		toks = toks[:12]
	}
	quoted := make([]string, 0, len(toks))
	for _, t := range toks {
		quoted = append(quoted, `"`+t+`"`)
	}
	return strings.Join(quoted, " OR ")
}

// SearchAgentKnowledge ranks runbooks and memory notes for a query, best first. domain filters to that domain
// and to the global entries; an empty domain searches everything.
func (d *DB) SearchAgentKnowledge(query, domain string, limit int) ([]KnowledgeHit, error) {
	match := FTSMatchQuery(query)
	if match == "" || limit <= 0 {
		return nil, nil
	}
	q := `SELECT kind, CAST(ref_id AS INTEGER), domain, title, snippet(agent_knowledge_fts, 4, '', '', ' ... ', 16)
		FROM agent_knowledge_fts WHERE agent_knowledge_fts MATCH ?`
	args := []any{match}
	if domain != "" {
		q += ` AND (domain = ? OR domain = 'global')`
		args = append(args, domain)
	}
	q += ` ORDER BY bm25(agent_knowledge_fts) LIMIT ?`
	args = append(args, limit)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("search knowledge: %w", err)
	}
	defer rows.Close()
	var out []KnowledgeHit
	for rows.Next() {
		var h KnowledgeHit
		if err := rows.Scan(&h.Kind, &h.ID, &h.Domain, &h.Title, &h.Snippet); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// reindexKnowledge replaces the index entry of one runbook or memory note with its current text.
func (d *DB) reindexKnowledge(kind string, id int64) error {
	if _, err := d.Exec(`DELETE FROM agent_knowledge_fts WHERE kind = ? AND ref_id = ?`, kind, id); err != nil {
		return err
	}
	var err error
	switch kind {
	case "runbook":
		_, err = d.Exec(`INSERT INTO agent_knowledge_fts(kind, ref_id, domain, title, body)
			SELECT 'runbook', id, domain, title, trigger_summary || ' ' || steps_json || ' ' || tags FROM agent_runbooks WHERE id = ?`, id)
	case "memory":
		_, err = d.Exec(`INSERT INTO agent_knowledge_fts(kind, ref_id, domain, title, body)
			SELECT 'memory', id, CASE WHEN domain = '' THEN 'global' ELSE domain END, 'memory note', content FROM agent_memory WHERE id = ?`, id)
	}
	return err
}

// reindexAllKnowledge rebuilds the whole index from the tables. It runs after a snapshot import and on migration.
func (d *DB) reindexAllKnowledge() error {
	if _, err := d.Exec(`DELETE FROM agent_knowledge_fts`); err != nil {
		return err
	}
	if _, err := d.Exec(`INSERT INTO agent_knowledge_fts(kind, ref_id, domain, title, body)
		SELECT 'runbook', id, domain, title, trigger_summary || ' ' || steps_json || ' ' || tags FROM agent_runbooks`); err != nil {
		return err
	}
	_, err := d.Exec(`INSERT INTO agent_knowledge_fts(kind, ref_id, domain, title, body)
		SELECT 'memory', id, CASE WHEN domain = '' THEN 'global' ELSE domain END, 'memory note', content FROM agent_memory`)
	return err
}

// normalizeRunbook checks a runbook before it is written and fills its defaults.
func normalizeRunbook(rb *AgentRunbook) error {
	rb.Title = strings.TrimSpace(rb.Title)
	if rb.Kind == "" {
		rb.Kind = "runbook"
	}
	if rb.Domain == "" {
		rb.Domain = "global"
	}
	if !knowledgeHas(AgentRunbookKinds, rb.Kind) {
		return fmt.Errorf("kind must be runbook or blueprint")
	}
	if !knowledgeHas(AgentKnowledgeDoms, rb.Domain) {
		return fmt.Errorf("unknown domain %q", rb.Domain)
	}
	if rb.Title == "" || len(rb.Title) > agentKnowledgeTitleMax {
		return fmt.Errorf("title is required, at most %d characters", agentKnowledgeTitleMax)
	}
	if rb.StepsJSON == "" {
		rb.StepsJSON = "{}"
	}
	if len(rb.StepsJSON) > agentRunbookStepsMax || !json.Valid([]byte(rb.StepsJSON)) {
		return fmt.Errorf("steps must be valid JSON of at most 16 KiB")
	}
	return nil
}

func knowledgeHas(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// CreateAgentRunbook stores a runbook and indexes it.
func (d *DB) CreateAgentRunbook(rb *AgentRunbook) (int64, error) {
	if err := normalizeRunbook(rb); err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	res, err := d.Exec(`INSERT INTO agent_runbooks (kind, title, domain, trigger_summary, steps_json, tags, source_thread_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, rb.Kind, rb.Title, rb.Domain, rb.TriggerSummary, rb.StepsJSON, rb.Tags, rb.SourceThreadID, now, now)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	rb.ID, rb.CreatedAt, rb.UpdatedAt = id, now, now
	return id, d.reindexKnowledge("runbook", id)
}

// ListAgentRunbooks returns the runbooks, newest first; domain filters when set.
func (d *DB) ListAgentRunbooks(domain string) ([]AgentRunbook, error) {
	q := `SELECT id, kind, title, domain, trigger_summary, steps_json, tags, source_thread_id, usage_count, created_at, updated_at FROM agent_runbooks`
	var args []any
	if domain != "" {
		q += ` WHERE domain = ?`
		args = append(args, domain)
	}
	q += ` ORDER BY updated_at DESC, id DESC`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentRunbook{}
	for rows.Next() {
		var rb AgentRunbook
		if err := rows.Scan(&rb.ID, &rb.Kind, &rb.Title, &rb.Domain, &rb.TriggerSummary, &rb.StepsJSON, &rb.Tags, &rb.SourceThreadID, &rb.UsageCount, &rb.CreatedAt, &rb.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, rb)
	}
	return out, rows.Err()
}

// DeleteAgentRunbook removes a runbook and its index entry.
func (d *DB) DeleteAgentRunbook(id int64) error {
	res, err := d.Exec(`DELETE FROM agent_runbooks WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAgentKnowledge
	}
	_, err = d.Exec(`DELETE FROM agent_knowledge_fts WHERE kind = 'runbook' AND ref_id = ?`, id)
	return err
}

// BumpAgentRunbookUsage counts one use of a runbook: it was injected into a prompt.
func (d *DB) BumpAgentRunbookUsage(id int64) error {
	_, err := d.Exec(`UPDATE agent_runbooks SET usage_count = usage_count + 1 WHERE id = ?`, id)
	return err
}

// normalizeSkill checks a custom skill before it is written.
func normalizeSkill(sk *AgentCustomSkill) error {
	sk.Name = strings.TrimSpace(sk.Name)
	if sk.Domain == "" {
		sk.Domain = "global"
	}
	if sk.Name == "" || len(sk.Name) > 120 || !knowledgeHas(AgentKnowledgeDoms, sk.Domain) || len(sk.Content) > agentSkillContentMax {
		return ErrAgentSkillFields
	}
	return nil
}

// CreateAgentCustomSkill stores a custom skill.
func (d *DB) CreateAgentCustomSkill(sk *AgentCustomSkill) (int64, error) {
	if err := normalizeSkill(sk); err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	res, err := d.Exec(`INSERT INTO agent_custom_skills (name, domain, enabled, content_md, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		sk.Name, sk.Domain, boolToInt(sk.Enabled), sk.Content, now, now)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	sk.ID, sk.CreatedAt, sk.UpdatedAt = id, now, now
	return id, err
}

// UpdateAgentCustomSkill replaces the fields of a custom skill.
func (d *DB) UpdateAgentCustomSkill(sk *AgentCustomSkill) error {
	if err := normalizeSkill(sk); err != nil {
		return err
	}
	res, err := d.Exec(`UPDATE agent_custom_skills SET name = ?, domain = ?, enabled = ?, content_md = ?, updated_at = ? WHERE id = ?`,
		sk.Name, sk.Domain, boolToInt(sk.Enabled), sk.Content, time.Now().UTC(), sk.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAgentKnowledge
	}
	return nil
}

// DeleteAgentCustomSkill removes a custom skill.
func (d *DB) DeleteAgentCustomSkill(id int64) error {
	res, err := d.Exec(`DELETE FROM agent_custom_skills WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAgentKnowledge
	}
	return nil
}

// ListAgentCustomSkills returns the skills, filtered by domain (and enabled ones only when asked).
func (d *DB) ListAgentCustomSkills(domain string, enabledOnly bool) ([]AgentCustomSkill, error) {
	q := `SELECT id, name, domain, enabled, content_md, created_at, updated_at FROM agent_custom_skills WHERE 1 = 1`
	var args []any
	if domain != "" {
		q += ` AND domain = ?`
		args = append(args, domain)
	}
	if enabledOnly {
		q += ` AND enabled = 1`
	}
	q += ` ORDER BY domain, name, id`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentCustomSkill{}
	for rows.Next() {
		var sk AgentCustomSkill
		var en int
		if err := rows.Scan(&sk.ID, &sk.Name, &sk.Domain, &en, &sk.Content, &sk.CreatedAt, &sk.UpdatedAt); err != nil {
			return nil, err
		}
		sk.Enabled = en == 1
		out = append(out, sk)
	}
	return out, rows.Err()
}
