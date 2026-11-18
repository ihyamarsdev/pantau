package store

// Docker Stacks: the Compose projects last seen on a host.

// GetDockerStacks returns the remembered Compose projects of a host as name -> JSON.
func (d *DB) GetDockerStacks(hostID int64) (map[string]string, error) {
	rows, err := d.Query(`SELECT name, data FROM docker_stacks WHERE host_id = ?`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var name, data string
		if err := rows.Scan(&name, &data); err != nil {
			return nil, err
		}
		out[name] = data
	}
	return out, rows.Err()
}

func (d *DB) SaveDockerStack(hostID int64, name, data string) error {
	_, err := d.Exec(`INSERT INTO docker_stacks (host_id, name, data) VALUES (?, ?, ?)
		ON CONFLICT(host_id, name) DO UPDATE SET data = excluded.data`, hostID, name, data)
	return err
}

func (d *DB) DeleteDockerStack(hostID int64, name string) error {
	_, err := d.Exec(`DELETE FROM docker_stacks WHERE host_id = ? AND name = ?`, hostID, name)
	return err
}
