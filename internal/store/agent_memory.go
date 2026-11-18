package store

import (
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// AgentMemoryScope is what a note is attached to. At most one of a host, a cluster or a functional domain;
// all empty means the whole fleet.
type AgentMemoryScope struct {
	HostID    *int64
	ClusterID *int64
	Domain    string
}

// Empty reports whether the scope applies to the whole fleet.
func (s AgentMemoryScope) Empty() bool {
	return s.HostID == nil && s.ClusterID == nil && s.Domain == ""
}

// AgentMemory is one durable fact the AI agent (or the operator) keeps about the infrastructure.
type AgentMemory struct {
	ID        int64     `json:"id"`
	HostID    *int64    `json:"host_id"`    // nil = not tied to one host
	ClusterID *int64    `json:"cluster_id"` // nil = not tied to one Kubernetes cluster
	Domain    string    `json:"domain,omitempty"`
	Content   string    `json:"content"`
	Source    string    `json:"source"` // agent | operator
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// Score is the relevance of a note for one recall query. It is computed per call and never stored.
	Score int `json:"score,omitempty"`
}

// AgentMemoryDomains are the functional areas a note may be filed under.
var AgentMemoryDomains = []string{"security", "network", "docker", "ansible", "terraform", "kubernetes", "database", "storage"}

const (
	AgentMemoryMax    = 200 // notes kept in total
	AgentMemoryMaxLen = 500 // characters per note
)

var (
	ErrAgentMemoryFull     = errors.New("agent memory is full (200 notes): forget or replace old ones")
	ErrAgentMemoryEmpty    = errors.New("note is empty")
	ErrAgentMemoryTooLong  = errors.New("note is longer than 500 characters: split it or keep only the durable fact")
	ErrAgentMemoryNotFound = errors.New("note not found")
	ErrAgentMemoryScope    = errors.New("a note may be tied to a host, a cluster or a domain, not to several at once")
	ErrAgentMemoryDomain   = errors.New("unknown domain: use one of " + strings.Join(AgentMemoryDomains, ", "))
)

const agentMemoryCols = `id, host_id, cluster_id, COALESCE(domain, ''), content, source, created_at, updated_at`

func scanAgentMemory(sc interface{ Scan(...any) error }) (AgentMemory, error) {
	var m AgentMemory
	var hid, cid sql.NullInt64
	err := sc.Scan(&m.ID, &hid, &cid, &m.Domain, &m.Content, &m.Source, &m.CreatedAt, &m.UpdatedAt)
	if hid.Valid {
		m.HostID = &hid.Int64
	}
	if cid.Valid {
		m.ClusterID = &cid.Int64
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

// normalizeScope validates a scope and lowercases its domain, so "Security" and "security" are one bucket.
func normalizeScope(s AgentMemoryScope) (AgentMemoryScope, error) {
	if n := btoi(s.HostID != nil) + btoi(s.ClusterID != nil) + btoi(s.Domain != ""); n > 1 {
		return s, ErrAgentMemoryScope
	}
	if s.Domain == "" {
		return s, nil
	}
	d := strings.ToLower(strings.TrimSpace(s.Domain))
	for _, known := range AgentMemoryDomains {
		if d == known {
			s.Domain = d
			return s, nil
		}
	}
	return s, ErrAgentMemoryDomain
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// scopeArgs renders a scope as the SQL arguments of its three columns.
func scopeArgs(s AgentMemoryScope) []any {
	var hid, cid any
	if s.HostID != nil {
		hid = *s.HostID
	}
	if s.ClusterID != nil {
		cid = *s.ClusterID
	}
	return []any{hid, cid, s.Domain}
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

// RecallAgentMemory returns the notes worth putting in the prompt for one question: those scoped to the host,
// the cluster or the domain the question is about, and those whose words the question repeats. Notes without
// any overlap are left out, so a prompt carries a few relevant facts instead of the whole table. An empty
// query returns the most recently touched notes, which is what a conversation with no question yet needs.
func (d *DB) RecallAgentMemory(scope AgentMemoryScope, query string, limit int) ([]AgentMemory, error) {
	if limit <= 0 {
		limit = 12
	}
	notes, err := d.ListAgentMemory(0, "")
	if err != nil {
		return nil, err
	}
	tokens := recallTokens(query)
	for i := range notes {
		n := &notes[i]
		score := 0
		// A note tied to exactly what the question asks about beats a vaguer one.
		if n.HostID != nil && scope.HostID != nil && *n.HostID == *scope.HostID {
			score += 8
		}
		if n.ClusterID != nil && scope.ClusterID != nil && *n.ClusterID == *scope.ClusterID {
			score += 8
		}
		if n.Domain != "" && n.Domain == scope.Domain {
			score += 5
		}
		if n.HostID == nil && n.ClusterID == nil && n.Domain == "" {
			score++ // a fleet-wide note applies everywhere, weakly
		}
		// A note about another host or cluster is not a fact about this one.
		if n.HostID != nil && (scope.HostID == nil || *n.HostID != *scope.HostID) {
			continue
		}
		if n.ClusterID != nil && (scope.ClusterID == nil || *n.ClusterID != *scope.ClusterID) {
			continue
		}
		score += tokenHits(n.Content, tokens) * 2
		if len(tokens) > 0 && score < 2 {
			continue // nothing ties this note to the question
		}
		n.Score = score
	}
	if len(tokens) > 0 {
		kept := notes[:0]
		for _, n := range notes {
			if n.Score > 0 {
				kept = append(kept, n)
			}
		}
		notes = kept
	}
	sort.SliceStable(notes, func(i, j int) bool {
		if notes[i].Score != notes[j].Score {
			return notes[i].Score > notes[j].Score
		}
		return notes[i].UpdatedAt.After(notes[j].UpdatedAt)
	})
	if len(notes) > limit {
		notes = notes[:limit]
	}
	return notes, nil
}

// recallStopwords are words that say nothing about the infrastructure, so they never match a note.
var recallStopwords = map[string]bool{
	"the": true, "and": true, "for": true, "why": true, "what": true, "when": true, "where": true, "how": true,
	"does": true, "did": true, "was": true, "were": true, "this": true, "that": true, "with": true, "from": true,
	"have": true, "has": true, "had": true, "not": true, "but": true, "you": true, "your": true, "can": true,
	"could": true, "should": true, "would": true, "please": true, "host": true, "hosts": true, "server": true,
	"servers": true, "check": true, "help": true, "there": true, "here": true, "about": true, "into": true,
	// Question and verb words: they describe the asking, never the infrastructure.
	"which": true, "who": true, "run": true, "runs": true, "running": true, "use": true, "uses": true,
	"using": true, "need": true, "needs": true, "get": true, "got": true, "make": true, "made": true,
	"see": true, "show": true, "tell": true, "know": true, "work": true, "works": true, "find": true,
	"list": true, "any": true, "all": true, "some": true, "more": true, "most": true, "many": true,
	"one": true, "two": true, "new": true, "old": true, "now": true, "then": true, "than": true,
	"just": true, "still": true, "again": true, "also": true, "very": true, "like": true,
	"yang": true, "dan": true, "apa": true, "kenapa": true, "bagaimana": true, "untuk": true, "dari": true,
	"tidak": true, "ada": true, "itu": true, "ini": true, "saya": true, "bisa": true, "tolong": true,
	"coba": true, "periksa": true, "lihat": true, "masalah": true, "mengapa": true,
}

// recallTokens splits a question into the lowercase words worth matching, at most six of them.
func recallTokens(query string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '-' && r != '_' && r != '.'
	}) {
		if len(w) < 3 || recallStopwords[w] || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
		if len(out) == 6 {
			break
		}
	}
	return out
}

// tokenHits counts how many distinct tokens the text contains.
func tokenHits(text string, tokens []string) int {
	low := strings.ToLower(text)
	n := 0
	for _, t := range tokens {
		if strings.Contains(low, t) {
			n++
		}
	}
	return n
}

// AddAgentMemory stores a note for the whole fleet with no domain. See AddAgentMemoryScoped.
func (d *DB) AddAgentMemory(hostID *int64, content, source string) (*AgentMemory, error) {
	return d.AddAgentMemoryScoped(AgentMemoryScope{HostID: hostID}, content, source)
}

// AddAgentMemoryScoped stores a note. The same text in the same scope is not stored twice: it is only
// refreshed, so re-learning a known fact does not fill the table.
func (d *DB) AddAgentMemoryScoped(scope AgentMemoryScope, content, source string) (*AgentMemory, error) {
	scope, err := normalizeScope(scope)
	if err != nil {
		return nil, err
	}
	content, err = checkAgentMemory(content)
	if err != nil {
		return nil, err
	}
	args := scopeArgs(scope)
	var id int64
	err = d.QueryRow(`SELECT id FROM agent_memory
		WHERE content = ? COLLATE NOCASE
		  AND COALESCE(host_id, 0) = COALESCE(?, 0)
		  AND COALESCE(cluster_id, 0) = COALESCE(?, 0)
		  AND domain = ?`, append([]any{content}, args...)...).Scan(&id)
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
		if res, err = d.Exec(`INSERT INTO agent_memory (host_id, cluster_id, domain, content, source) VALUES (?, ?, ?, ?, ?)`,
			args[0], args[1], args[2], content, source); err == nil {
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
