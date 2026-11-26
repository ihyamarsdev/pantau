package store

// AI agent chat history: one agent_threads row per conversation, grouped by the resource the conversation is
// about (a host, a cluster or a functional area), and its agent_messages rows. The history is local and
// re-creatable, so it is not part of a System Snapshot; it is pruned (the newest AgentThreadMax threads and
// the newest AgentMessagesPerThread messages of each) so the database stays bounded.

import (
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	AgentThreadMax         = 50  // threads kept; older ones go with their messages
	AgentMessagesPerThread = 200 // messages kept per thread
	AgentThreadTitleMax    = 120 // characters kept of the first question
)

// AgentScopeTypes are the resources a thread may be grouped under. Only host and cluster carry a scope id.
var AgentScopeTypes = []string{"global", "host", "cluster", "docker", "ansible", "terraform"}

var (
	ErrAgentThreadNotFound = errors.New("agent thread not found")
	ErrAgentScope          = errors.New("unknown scope type: use one of " + strings.Join(AgentScopeTypes, ", "))
)

// AgentThread is one AI agent conversation.
type AgentThread struct {
	ID        int64     `json:"id"`
	ScopeType string    `json:"scope_type"`
	ScopeID   int64     `json:"scope_id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Messages  int       `json:"messages"` // count, filled by a list

	// Session is the browser session that owns the thread, so a reload or a Pantau restart resumes it.
	Session string `json:"-"`
}

// AgentMessage is one stored message of a thread. An assistant row may carry its tool calls as JSON; a tool
// row answers one of them, which is why it keeps that call's id, tool name and failure flag.
type AgentMessage struct {
	ID         int64     `json:"id"`
	ThreadID   int64     `json:"thread_id"`
	Role       string    `json:"role"` // user | assistant | tool
	Content    string    `json:"content"`
	ToolCalls  string    `json:"tool_calls,omitempty"`
	Approval   string    `json:"approval_status,omitempty"` // tool: approved | denied, when the call needed approval
	ToolCallID string    `json:"tool_call_id,omitempty"`
	ToolName   string    `json:"tool_name,omitempty"`
	IsError    bool      `json:"is_error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

const agentMessageCols = `id, thread_id, role, content, tool_calls_json, approval_status, tool_call_id, tool_name, is_error, created_at`

func scanAgentMessage(sc interface{ Scan(...any) error }) (AgentMessage, error) {
	var m AgentMessage
	var isErr int
	err := sc.Scan(&m.ID, &m.ThreadID, &m.Role, &m.Content, &m.ToolCalls, &m.Approval, &m.ToolCallID, &m.ToolName, &isErr, &m.CreatedAt)
	m.IsError = isErr != 0
	return m, err
}

// normalizeScopeType checks a scope type, so a typo never becomes its own bucket.
func normalizeScopeType(scopeType string) (string, error) {
	t := strings.ToLower(strings.TrimSpace(scopeType))
	if t == "" {
		t = "global"
	}
	for _, known := range AgentScopeTypes {
		if t == known {
			return t, nil
		}
	}
	return "", ErrAgentScope
}

// clipThreadTitle makes the title out of the first question: one line, at most AgentThreadTitleMax runes.
func clipThreadTitle(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "New chat"
	}
	if utf8.RuneCountInString(s) <= AgentThreadTitleMax {
		return s
	}
	r := []rune(s)
	return string(r[:AgentThreadTitleMax]) + "…"
}

func scanAgentThread(sc interface{ Scan(...any) error }) (AgentThread, error) {
	var t AgentThread
	err := sc.Scan(&t.ID, &t.ScopeType, &t.ScopeID, &t.Session, &t.Title, &t.CreatedAt, &t.UpdatedAt, &t.Messages)
	return t, err
}

const agentThreadCols = `t.id, t.scope_type, t.scope_id, t.session, t.title, t.created_at, t.updated_at,
	(SELECT COUNT(*) FROM agent_messages m WHERE m.thread_id = t.id)`

// CreateAgentThread starts a conversation under one scope. An empty title is filled from the first question
// once it is stored.
func (d *DB) CreateAgentThread(scopeType string, scopeID int64, session, title string) (*AgentThread, error) {
	scopeType, err := normalizeScopeType(scopeType)
	if err != nil {
		return nil, err
	}
	if scopeID < 0 {
		return nil, ErrAgentScope
	}
	res, err := d.Exec(`INSERT INTO agent_threads(scope_type, scope_id, session, title) VALUES (?, ?, ?, ?)`,
		scopeType, scopeID, session, strings.TrimSpace(title))
	if err != nil {
		return nil, err
	}
	// A thread with no message yet is still a row: the cap is enforced here too, not only on a write of messages.
	if _, err := d.Exec(pruneAgentThreadsSQL, AgentThreadMax); err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return d.GetAgentThread(id)
}

func (d *DB) GetAgentThread(id int64) (*AgentThread, error) {
	t, err := scanAgentThread(d.QueryRow(`SELECT `+agentThreadCols+` FROM agent_threads t WHERE t.id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, ErrAgentThreadNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ListAgentThreads returns the threads of one scope, most recently touched first. An empty scopeType lists
// every scope; scopeID 0 with a host or cluster scope lists all of them.
func (d *DB) ListAgentThreads(scopeType string, scopeID int64) ([]AgentThread, error) {
	q, args := `SELECT `+agentThreadCols+` FROM agent_threads t ORDER BY t.updated_at DESC, t.id DESC`, []any{}
	if strings.TrimSpace(scopeType) != "" {
		scopeType, err := normalizeScopeType(scopeType)
		if err != nil {
			return nil, err
		}
		q = `SELECT ` + agentThreadCols + ` FROM agent_threads t WHERE t.scope_type = ?`
		args = append(args, scopeType)
		if scopeID > 0 {
			q += ` AND t.scope_id = ?`
			args = append(args, scopeID)
		}
		q += ` ORDER BY t.updated_at DESC, t.id DESC`
	}
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentThread{}
	for rows.Next() {
		t, err := scanAgentThread(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// AgentThreadBySession returns the newest thread the browser session owns, or nil when it has none. It is how
// a conversation comes back after a reload or a Pantau restart.
func (d *DB) AgentThreadBySession(session string) (*AgentThread, error) {
	if strings.TrimSpace(session) == "" {
		return nil, nil
	}
	t, err := scanAgentThread(d.QueryRow(`SELECT `+agentThreadCols+` FROM agent_threads t WHERE t.session = ? ORDER BY t.updated_at DESC, t.id DESC LIMIT 1`, session))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (d *DB) DeleteAgentThread(id int64) error {
	res, err := d.Exec(`DELETE FROM agent_threads WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAgentThreadNotFound
	}
	return nil
}

// ClearAgentThreads deletes every thread of one scope, or all of them when scopeType is empty. It returns how
// many threads went.
func (d *DB) ClearAgentThreads(scopeType string, scopeID int64) (int64, error) {
	q, args := `DELETE FROM agent_threads`, []any{}
	if strings.TrimSpace(scopeType) != "" {
		scopeType, err := normalizeScopeType(scopeType)
		if err != nil {
			return 0, err
		}
		q += ` WHERE scope_type = ?`
		args = append(args, scopeType)
		if scopeID > 0 {
			q += ` AND scope_id = ?`
			args = append(args, scopeID)
		}
	}
	res, err := d.Exec(q, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (d *DB) ListAgentMessages(threadID int64) ([]AgentMessage, error) {
	rows, err := d.Query(`SELECT `+agentMessageCols+` FROM agent_messages WHERE thread_id = ? ORDER BY id`, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentMessage{}
	for rows.Next() {
		m, err := scanAgentMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AppendAgentMessages stores the messages a turn produced and prunes in the same transaction: at most
// AgentMessagesPerThread messages stay in the thread and at most AgentThreadMax threads in the database. The
// first question becomes the title of a thread that has none.
func (d *DB) AppendAgentMessages(threadID int64, msgs []AgentMessage) error {
	if len(msgs) == 0 {
		return nil
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	title := ""
	for _, m := range msgs {
		if _, err := tx.Exec(`INSERT INTO agent_messages(thread_id, role, content, tool_calls_json, approval_status, tool_call_id, tool_name, is_error) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			threadID, m.Role, m.Content, m.ToolCalls, m.Approval, m.ToolCallID, m.ToolName, btoi(m.IsError)); err != nil {
			return err
		}
		if title == "" && m.Role == "user" {
			title = clipThreadTitle(m.Content)
		}
	}
	if title != "" {
		if _, err := tx.Exec(`UPDATE agent_threads SET title = ? WHERE id = ? AND title = ''`, title, threadID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE agent_threads SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`, threadID); err != nil {
		return err
	}
	if err := pruneAgentMessages(tx, threadID); err != nil {
		return err
	}
	if err := pruneAgentThreads(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// ReplaceAgentMessages rewrites the whole stored history of a thread. Compaction replaces the oldest messages
// with one summary, so the stored rows must follow the history the model now sees.
func (d *DB) ReplaceAgentMessages(threadID int64, msgs []AgentMessage) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM agent_messages WHERE thread_id = ?`, threadID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE agent_threads SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`, threadID); err != nil {
		return err
	}
	title := ""
	for _, m := range msgs {
		if _, err := tx.Exec(`INSERT INTO agent_messages(thread_id, role, content, tool_calls_json, approval_status, tool_call_id, tool_name, is_error) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			threadID, m.Role, m.Content, m.ToolCalls, m.Approval, m.ToolCallID, m.ToolName, btoi(m.IsError)); err != nil {
			return err
		}
		if title == "" && m.Role == "user" {
			title = clipThreadTitle(m.Content)
		}
	}
	if title != "" {
		if _, err := tx.Exec(`UPDATE agent_threads SET title = ? WHERE id = ? AND title = ''`, title, threadID); err != nil {
			return err
		}
	}
	if err := pruneAgentMessages(tx, threadID); err != nil {
		return err
	}
	return tx.Commit()
}

// pruneAgentMessages keeps the newest AgentMessagesPerThread messages and then drops everything before the
// first user message. A history that starts with a tool result (its tool_use was pruned) is refused by the
// providers, and a stored history is replayed verbatim on the next resume, so the cut is always at a user turn.
func pruneAgentMessages(tx *sql.Tx, threadID int64) error {
	if _, err := tx.Exec(`DELETE FROM agent_messages WHERE thread_id = ? AND id NOT IN
		(SELECT id FROM agent_messages WHERE thread_id = ? ORDER BY id DESC LIMIT ?)`, threadID, threadID, AgentMessagesPerThread); err != nil {
		return err
	}
	_, err := tx.Exec(`DELETE FROM agent_messages WHERE thread_id = ? AND id < COALESCE(
		(SELECT MIN(id) FROM agent_messages WHERE thread_id = ? AND role = 'user'), 0)`, threadID, threadID)
	return err
}

// pruneAgentThreadsSQL keeps the newest AgentThreadMax threads by last use; their messages go with the cascade.
const pruneAgentThreadsSQL = `DELETE FROM agent_threads WHERE id NOT IN
	(SELECT id FROM agent_threads ORDER BY updated_at DESC, id DESC LIMIT ?)`

func pruneAgentThreads(tx *sql.Tx) error {
	_, err := tx.Exec(pruneAgentThreadsSQL, AgentThreadMax)
	return err
}

// UpdateAgentThreadTitle updates the title of a stored thread, clipping it to AgentThreadTitleMax runes.
func (d *DB) UpdateAgentThreadTitle(id int64, title string) error {
	clean := clipThreadTitle(title)
	res, err := d.Exec(`UPDATE agent_threads SET title = ? WHERE id = ?`, clean, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrAgentThreadNotFound
	}
	return nil
}
