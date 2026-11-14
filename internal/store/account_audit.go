package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// AccountAuditSummary is the digest of one account audit.
type AccountAuditSummary struct {
	Users        int `json:"users"`         // accounts listed in the audit (login-capable + notable ones)
	LoginUsers   int `json:"login_users"`   // accounts with a login-capable shell
	Keys         int `json:"keys"`          // authorized_keys lines parsed successfully
	UniqueKeys   int `json:"unique_keys"`   // distinct fingerprints
	UnknownKeys  int `json:"unknown_keys"`  // fingerprints not found in the SSH key library
	WeakKeys     int `json:"weak_keys"`     // dss or rsa < 2048
	InvalidLines int `json:"invalid_lines"` // authorized_keys lines that could not be parsed
	UID0Accounts int `json:"uid0_accounts"` // accounts with UID 0 other than root
	Critical     int `json:"critical"`
	High         int `json:"high"`
	Medium       int `json:"medium"`
	Low          int `json:"low"`
	Info         int `json:"info"`
}

// AccountUser is one audited local account.
type AccountUser struct {
	Name           string   `json:"name"`
	UID            int      `json:"uid"`
	GID            int      `json:"gid"`
	Home           string   `json:"home"`
	Shell          string   `json:"shell"`
	LoginCapable   bool     `json:"login_capable"`
	Groups         []string `json:"groups"`
	SudoGrant      string   `json:"sudo_grant"`     // none | unknown | "ALL" | "NOPASSWD: ALL" | ...
	PasswordState  string   `json:"password_state"` // set | empty | locked | unknown
	KeyCount       int      `json:"key_count"`
	KeyFiles       []string `json:"key_files"`
	KeyFilesUnread []string `json:"key_files_unreadable"`
	LastLogin      string   `json:"last_login,omitempty"`
}

// AccountKey is one parsed authorized_keys entry. Only public key metadata is stored; never private key material.
type AccountKey struct {
	Fingerprint      string   `json:"fingerprint"`
	Type             string   `json:"type"`
	Bits             int      `json:"bits"`
	Comment          string   `json:"comment"`
	Options          []string `json:"options"`
	Restricted       bool     `json:"restricted"`        // command= option limits what the key may run
	SourceRestricted bool     `json:"source_restricted"` // from= option limits where the key may be used
	Valid            bool     `json:"valid"`
	User             string   `json:"user"`
	File             string   `json:"file"`
	LibraryKey       string   `json:"library_key,omitempty"` // name in the SSH key library, empty when unknown
	Unknown          bool     `json:"unknown"`
	Weak             bool     `json:"weak"`
	Error            string   `json:"error,omitempty"`
}

// AccountFinding is one audit finding.
type AccountFinding struct {
	ID          string `json:"id"`
	Severity    string `json:"severity"` // critical|high|medium|low|info
	Title       string `json:"title"`
	Detail      string `json:"detail"`
	User        string `json:"user,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

// AccountAudit is the latest account audit of a host.
type AccountAudit struct {
	HostID    int64               `json:"host_id"`
	ScannedAt time.Time           `json:"scanned_at"`
	Status    string              `json:"status"` // ok|partial|unsupported|error
	Summary   AccountAuditSummary `json:"summary"`
	Users     []AccountUser       `json:"users"`
	Keys      []AccountKey        `json:"keys"`
	Findings  []AccountFinding    `json:"findings"`
	Notes     []string            `json:"notes"`
	Error     string              `json:"error,omitempty"`
}

// migrateAccountAudits creates the account_audits table. The table holds derived telemetry
// (it is re-derivable by a fresh scan) and is therefore NOT part of the encrypted system snapshot.
func (d *DB) migrateAccountAudits() error {
	_, err := d.Exec(`CREATE TABLE IF NOT EXISTS account_audits (
		host_id INTEGER PRIMARY KEY REFERENCES hosts(id) ON DELETE CASCADE,
		scanned_at DATETIME NOT NULL,
		status TEXT,
		summary_json TEXT,
		users_json TEXT,
		keys_json TEXT,
		findings_json TEXT,
		notes_json TEXT,
		error TEXT
	)`)
	return err
}

// SaveAccountAudit upserts a host's latest account audit.
func (d *DB) SaveAccountAudit(a *AccountAudit) error {
	if a == nil {
		return fmt.Errorf("account audit cannot be nil")
	}
	summaryJSON, err := json.Marshal(a.Summary)
	if err != nil {
		return fmt.Errorf("marshal summary: %w", err)
	}
	usersJSON, err := json.Marshal(nonNilUsers(a.Users))
	if err != nil {
		return fmt.Errorf("marshal users: %w", err)
	}
	keysJSON, err := json.Marshal(nonNilKeys(a.Keys))
	if err != nil {
		return fmt.Errorf("marshal keys: %w", err)
	}
	findingsJSON, err := json.Marshal(nonNilFindings(a.Findings))
	if err != nil {
		return fmt.Errorf("marshal findings: %w", err)
	}
	notesJSON, err := json.Marshal(nonNilNotes(a.Notes))
	if err != nil {
		return fmt.Errorf("marshal notes: %w", err)
	}

	scannedAt := a.ScannedAt
	if scannedAt.IsZero() {
		scannedAt = time.Now()
	}
	_, err = d.Exec(`INSERT INTO account_audits (
		host_id, scanned_at, status, summary_json, users_json, keys_json, findings_json, notes_json, error
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(host_id) DO UPDATE SET
		scanned_at = excluded.scanned_at,
		status = excluded.status,
		summary_json = excluded.summary_json,
		users_json = excluded.users_json,
		keys_json = excluded.keys_json,
		findings_json = excluded.findings_json,
		notes_json = excluded.notes_json,
		error = excluded.error`,
		a.HostID, scannedAt, a.Status, string(summaryJSON), string(usersJSON), string(keysJSON),
		string(findingsJSON), string(notesJSON), a.Error)
	return err
}

// GetAccountAudit returns the latest account audit of a host, or (nil, nil) when there is none.
func (d *DB) GetAccountAudit(hostID int64) (*AccountAudit, error) {
	row := d.QueryRow(`SELECT host_id, scanned_at, status, summary_json, users_json, keys_json, findings_json, notes_json, error
		FROM account_audits WHERE host_id = ?`, hostID)
	a, err := scanAccountAuditRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

// ListAccountAudits returns the latest account audit of every host that has one.
func (d *DB) ListAccountAudits() ([]AccountAudit, error) {
	rows, err := d.Query(`SELECT host_id, scanned_at, status, summary_json, users_json, keys_json, findings_json, notes_json, error
		FROM account_audits`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]AccountAudit, 0)
	for rows.Next() {
		a, err := scanAccountAuditRow(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *a)
	}
	return list, rows.Err()
}

func scanAccountAuditRow(sc rowScanner) (*AccountAudit, error) {
	var (
		a            AccountAudit
		scannedAt    time.Time
		summaryJSON  string
		usersJSON    string
		keysJSON     string
		findingsJSON string
		notesJSON    string
	)
	if err := sc.Scan(&a.HostID, &scannedAt, &a.Status, &summaryJSON, &usersJSON, &keysJSON, &findingsJSON, &notesJSON, &a.Error); err != nil {
		return nil, err
	}
	a.ScannedAt = scannedAt
	if err := json.Unmarshal([]byte(summaryJSON), &a.Summary); err != nil {
		return nil, fmt.Errorf("unmarshal summary: %w", err)
	}
	if err := json.Unmarshal([]byte(usersJSON), &a.Users); err != nil {
		return nil, fmt.Errorf("unmarshal users: %w", err)
	}
	if err := json.Unmarshal([]byte(keysJSON), &a.Keys); err != nil {
		return nil, fmt.Errorf("unmarshal keys: %w", err)
	}
	if err := json.Unmarshal([]byte(findingsJSON), &a.Findings); err != nil {
		return nil, fmt.Errorf("unmarshal findings: %w", err)
	}
	if err := json.Unmarshal([]byte(notesJSON), &a.Notes); err != nil {
		return nil, fmt.Errorf("unmarshal notes: %w", err)
	}
	a.Users = nonNilUsers(a.Users)
	a.Keys = nonNilKeys(a.Keys)
	a.Findings = nonNilFindings(a.Findings)
	a.Notes = nonNilNotes(a.Notes)
	return &a, nil
}

func nonNilUsers(u []AccountUser) []AccountUser {
	if u == nil {
		return []AccountUser{}
	}
	return u
}

func nonNilKeys(k []AccountKey) []AccountKey {
	if k == nil {
		return []AccountKey{}
	}
	return k
}

func nonNilFindings(f []AccountFinding) []AccountFinding {
	if f == nil {
		return []AccountFinding{}
	}
	return f
}

func nonNilNotes(n []string) []string {
	if n == nil {
		return []string{}
	}
	return n
}
