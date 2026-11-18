package store

import "time"

// HostContainer is one container last seen on a host.
type HostContainer struct {
	HostID int64
	Name   string
	Image  string
	State  string // running, paused, exited, ...
	SeenAt time.Time
}

// ReplaceHostContainers replaces the stored containers of a host in one transaction.
// An empty list clears the host's rows (the host has no containers).
func (d *DB) ReplaceHostContainers(hostID int64, cs []HostContainer) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM host_containers WHERE host_id = ?`, hostID); err != nil {
		return err
	}

	stmt, err := tx.Prepare(`INSERT INTO host_containers (host_id, name, image, state, seen_at) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now()
	for _, c := range cs {
		seenAt := c.SeenAt
		if seenAt.IsZero() {
			seenAt = now
		}
		if _, err := stmt.Exec(hostID, c.Name, c.Image, c.State, seenAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListHostContainers returns the containers last seen on one host, ordered by name.
// Used by the lazy topology host-detail endpoint, never by the global graph.
func (d *DB) ListHostContainers(hostID int64) ([]HostContainer, error) {
	rows, err := d.Query(`SELECT host_id, name, image, state, seen_at FROM host_containers WHERE host_id = ? ORDER BY name`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HostContainer
	for rows.Next() {
		var c HostContainer
		if err := rows.Scan(&c.HostID, &c.Name, &c.Image, &c.State, &c.SeenAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
