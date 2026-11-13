package store

import (
	"database/sql"
	"errors"
	"time"
)

// GitRepo is a Git repository cloned into a folder on the Pantau server (Repositories section).
// Dir is the absolute clone path fixed at clone time, so changing the repositories folder setting later only
// affects new clones.
type GitRepo struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	URL       string     `json:"url"`
	Branch    string     `json:"branch"` // requested branch at clone; "" = remote default
	Group     string     `json:"group"`  // folder the repository lives in under the repositories location ("" = directly in it); cosmetic grouping that also is the on-disk parent folder
	Dir       string     `json:"dir"`
	Auth      string     `json:"auth"`   // "none" | "ssh" | "token"
	Token     string     `json:"-"`      // secret: NEVER serialized; plaintext in SQLite like tf_workspaces.git_token
	KeyID     int64      `json:"key_id"` // SSH Key Library entry when Auth == "ssh"
	SyncedAt  *time.Time `json:"synced_at"`
	CreatedAt time.Time  `json:"created_at"`
}

var (
	ErrGitRepoNameTaken = errors.New("a repository with this name already exists")
	ErrGitRepoNotFound  = errors.New("repository not found")
)

const gitRepoCols = `id, name, url, branch, group_name, dir, auth, token, key_id, synced_at, created_at`

func scanGitRepo(sc rowScanner) (*GitRepo, error) {
	var r GitRepo
	var synced sql.NullTime
	if err := sc.Scan(&r.ID, &r.Name, &r.URL, &r.Branch, &r.Group, &r.Dir, &r.Auth, &r.Token, &r.KeyID, &synced, &r.CreatedAt); err != nil {
		return nil, err
	}
	if synced.Valid {
		r.SyncedAt = &synced.Time
	}
	return &r, nil
}

func normalizeGitRepoAuth(r *GitRepo) {
	if r.Auth != "token" && r.Auth != "ssh" {
		r.Auth = "none"
	}
	if r.Auth != "token" {
		r.Token = ""
	}
	if r.Auth != "ssh" {
		r.KeyID = 0
	}
}

func (d *DB) CreateGitRepo(r *GitRepo) (int64, error) {
	normalizeGitRepoAuth(r)
	now := time.Now().UTC()
	res, err := d.Exec(`INSERT INTO git_repos (name, url, branch, group_name, dir, auth, token, key_id, synced_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Name, r.URL, r.Branch, r.Group, r.Dir, r.Auth, r.Token, r.KeyID, now, now)
	if err != nil {
		if isUniqueConstraintError(err) {
			return 0, ErrGitRepoNameTaken
		}
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	r.ID, r.CreatedAt = id, now
	r.SyncedAt = &now
	return id, nil
}

// GetGitRepo returns nil, nil when the repository does not exist.
func (d *DB) GetGitRepo(id int64) (*GitRepo, error) {
	r, err := scanGitRepo(d.QueryRow(`SELECT `+gitRepoCols+` FROM git_repos WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

func (d *DB) ListGitRepos() ([]GitRepo, error) {
	rows, err := d.Query(`SELECT ` + gitRepoCols + ` FROM git_repos ORDER BY name COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]GitRepo, 0)
	for rows.Next() {
		r, err := scanGitRepo(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *r)
	}
	return list, rows.Err()
}

// UpdateGitRepoAuth replaces the credentials; an empty Token with Auth "token" keeps the stored token.
func (d *DB) UpdateGitRepoAuth(r *GitRepo) error {
	normalizeGitRepoAuth(r)
	var res sql.Result
	var err error
	if r.Auth == "token" && r.Token == "" {
		res, err = d.Exec(`UPDATE git_repos SET auth = ?, key_id = ? WHERE id = ?`, r.Auth, r.KeyID, r.ID)
	} else {
		res, err = d.Exec(`UPDATE git_repos SET auth = ?, token = ?, key_id = ? WHERE id = ?`, r.Auth, r.Token, r.KeyID, r.ID)
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrGitRepoNotFound
	}
	return nil
}

// UpdateGitRepoFolder records a repository that was moved to another folder (group) and directory.
func (d *DB) UpdateGitRepoFolder(id int64, group, dir string) error {
	res, err := d.Exec(`UPDATE git_repos SET group_name = ?, dir = ? WHERE id = ?`, group, dir, id)
	if err != nil {
		if isUniqueConstraintError(err) {
			return ErrGitRepoNameTaken
		}
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrGitRepoNotFound
	}
	return nil
}

func (d *DB) MarkGitRepoSynced(id int64) error {
	_, err := d.Exec(`UPDATE git_repos SET synced_at = ? WHERE id = ?`, time.Now().UTC(), id)
	return err
}

func (d *DB) DeleteGitRepo(id int64) error {
	res, err := d.Exec(`DELETE FROM git_repos WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrGitRepoNotFound
	}
	return nil
}
