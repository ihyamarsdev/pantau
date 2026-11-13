package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// AgentMemory is one durable fact the AI agent (or the operator) keeps about the infrastructure.
type AgentMemory struct {
	ID        int64     `json:"id"`
	HostID    *int64    `json:"host_id"` // nil = applies to the whole fleet
	Content   string    `json:"content"`
	Source    string    `json:"source"` // agent | operator
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

const (
	AgentMemoryMax    = 200 // notes kept in total
	AgentMemoryMaxLen = 500 // characters per note
)

var (
	ErrAgentMemoryFull     = errors.New("agent memory is full (200 notes): forget or replace old ones")
	ErrAgentMemoryEmpty    = errors.New("note is empty")
	ErrAgentMemoryTooLong  = errors.New("note is longer than 500 characters: split it or keep only the durable fact")
	ErrAgentMemoryNotFound = errors.New("note not found")
)

const agentMemoryCols = `id, host_id, content, source, created_at, updated_at`

func scanAgentMemory(sc interface{ Scan(...any) error }) (AgentMemory, error) {
	var m AgentMemory
	var hid sql.NullInt64
	err := sc.Scan(&m.ID, &hid, &m.Content, &m.Source, &m.CreatedAt, &m.UpdatedAt)
	if hid.Valid {
		m.HostID = &hid.Int64
	}
	return m, err
}

func checkAgentMemory(content string) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", ErrAgentMemoryEmpty
	}
	if utf8.RuneCountInString(content) > AgentMemoryMaxLen {
		return "", ErrAgentMemoryTooLong
	}
	return content, nil
}

// ListAgentMemory returns notes, most recently touched first. hostID 0 lists every note; a host id lists that
// host's notes plus the fleet-wide ones. A non-empty query keeps notes containing it (case-insensitive).
func (d *DB) ListAgentMemory(hostID int64, query string) ([]AgentMemory, error) {
	q := `SELECT ` + agentMemoryCols + ` FROM agent_memory WHERE 1=1`
	var args []any
	if hostID > 0 {
		q += ` AND (host_id IS NULL OR host_id = ?)`
		args = append(args, hostID)
	}
	if query = strings.ToLower(strings.TrimSpace(query)); query != "" {
		q += ` AND LOWER(content) LIKE ? ESCAPE '\'`
		args = append(args, likePattern(query))
	}
	rows, err := d.Query(q+` ORDER BY updated_at DESC, id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentMemory{}
	for rows.Next() {
		m, err := scanAgentMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AddAgentMemory stores a note. The same text in the same scope is not stored twice: it is only refreshed.
func (d *DB) AddAgentMemory(hostID *int64, content, source string) (*AgentMemory, error) {
	content, err := checkAgentMemory(content)
	if err != nil {
		return nil, err
	}
	var hid any
	if hostID != nil {
		hid = *hostID
	}
	var id int64
	err = d.QueryRow(`SELECT id FROM agent_memory WHERE content = ? COLLATE NOCASE AND COALESCE(host_id, 0) = COALESCE(?, 0)`, content, hid).Scan(&id)
	switch {
	case err == nil:
		_, err = d.Exec(`UPDATE agent_memory SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	case errors.Is(err, sql.ErrNoRows):
		var n int
		if err = d.QueryRow(`SELECT COUNT(*) FROM agent_memory`).Scan(&n); err != nil {
			return nil, err
		}
		if n >= AgentMemoryMax {
			return nil, ErrAgentMemoryFull
		}
		var res sql.Result
		if res, err = d.Exec(`INSERT INTO agent_memory (host_id, content, source) VALUES (?, ?, ?)`, hid, content, source); err == nil {
			id, err = res.LastInsertId()
		}
	}
	if err != nil {
		return nil, err
	}
	m, err := scanAgentMemory(d.QueryRow(`SELECT `+agentMemoryCols+` FROM agent_memory WHERE id = ?`, id))
	return &m, err
}

// UpdateAgentMemory rewrites the text of a note (its scope stays).
func (d *DB) UpdateAgentMemory(id int64, content string) error {
	content, err := checkAgentMemory(content)
	if err != nil {
		return err
	}
	res, err := d.Exec(`UPDATE agent_memory SET content = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, content, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAgentMemoryNotFound
	}
	return nil
}

func (d *DB) DeleteAgentMemory(id int64) error {
	res, err := d.Exec(`DELETE FROM agent_memory WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAgentMemoryNotFound
	}
	return nil
}
