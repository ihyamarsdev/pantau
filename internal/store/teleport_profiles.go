package store

import (
	"database/sql"
	"errors"
	"time"
)

// TeleportProfile is one Teleport login Pantau can use. It stores no secret: passwords, MFA codes and
// certificates live only in tsh's own home and in the isolated kubeconfig file, never in this table.
type TeleportProfile struct {
	ID              int64     `json:"id"`
	Name            string    `json:"name"`
	Proxy           string    `json:"proxy"`          // e.g. teleport.example.com:443
	AuthConnector   string    `json:"auth_connector"` // e.g. local, oidc, or empty
	Username        string    `json:"username"`
	TeleportCluster string    `json:"teleport_cluster"` // root cluster name
	TeleportHome    string    `json:"teleport_home"`    // optional TELEPORT_HOME, empty means tsh's default
	DefaultPolicy   string    `json:"default_policy"`   // read-only | no-exec | full, applied to synced clusters
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

var ErrTeleportProfileNameTaken = errors.New("a teleport profile with this name already exists")

// normalizeTeleportPolicy keeps the safe default for anything unknown: a new Teleport cluster is read-only
// until the operator widens it.
func normalizeTeleportPolicy(policy string) string {
	switch policy {
	case "full", "no-exec", "read-only":
		return policy
	default:
		return "read-only"
	}
}

const teleportProfileColumns = `id, name, proxy, auth_connector, username, teleport_cluster, teleport_home, default_policy, created_at, updated_at`

func scanTeleportProfile(s rowScanner) (TeleportProfile, error) {
	var p TeleportProfile
	err := s.Scan(&p.ID, &p.Name, &p.Proxy, &p.AuthConnector, &p.Username, &p.TeleportCluster, &p.TeleportHome,
		&p.DefaultPolicy, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

// CreateTeleportProfile stores a profile and returns its id.
func (d *DB) CreateTeleportProfile(p *TeleportProfile) (int64, error) {
	p.DefaultPolicy = normalizeTeleportPolicy(p.DefaultPolicy)
	res, err := d.Exec(`INSERT INTO teleport_profiles (name, proxy, auth_connector, username, teleport_cluster, teleport_home, default_policy)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.Proxy, p.AuthConnector, p.Username, p.TeleportCluster, p.TeleportHome, p.DefaultPolicy)
	if err != nil {
		if isUniqueConstraintError(err) {
			return 0, ErrTeleportProfileNameTaken
		}
		return 0, err
	}
	return res.LastInsertId()
}

// GetTeleportProfile returns the profile, or nil when it does not exist.
func (d *DB) GetTeleportProfile(id int64) (*TeleportProfile, error) {
	p, err := scanTeleportProfile(d.QueryRow(`SELECT `+teleportProfileColumns+` FROM teleport_profiles WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (d *DB) ListTeleportProfiles() ([]TeleportProfile, error) {
	rows, err := d.Query(`SELECT ` + teleportProfileColumns + ` FROM teleport_profiles ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]TeleportProfile, 0)
	for rows.Next() {
		p, err := scanTeleportProfile(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

// UpdateTeleportProfile replaces the editable fields of a profile. It returns ErrTeleportProfileNameTaken
// on a duplicate name.
func (d *DB) UpdateTeleportProfile(p *TeleportProfile) error {
	p.DefaultPolicy = normalizeTeleportPolicy(p.DefaultPolicy)
	_, err := d.Exec(`UPDATE teleport_profiles SET name = ?, proxy = ?, auth_connector = ?, username = ?, teleport_cluster = ?,
		teleport_home = ?, default_policy = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		p.Name, p.Proxy, p.AuthConnector, p.Username, p.TeleportCluster, p.TeleportHome, p.DefaultPolicy, p.ID)
	if err != nil {
		if isUniqueConstraintError(err) {
			return ErrTeleportProfileNameTaken
		}
		return err
	}
	return nil
}

// DeleteTeleportProfile removes the profile together with the Teleport clusters synced from it, so no
// cluster is left pointing at a profile that no longer exists.
func (d *DB) DeleteTeleportProfile(id int64) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM kube_clusters WHERE source_type = ? AND teleport_profile_id = ?`, KubeSourceTeleport, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM teleport_profiles WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}
