package store

import (
	"database/sql"
	"time"
)

// VPNNetwork is one network path (a VPN/tunnel driver plus its credentials) that Hosts dial through.
// Config holds driver JSON including secrets: API handlers must redact it, only System Snapshot serializes it.
type VPNNetwork struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Driver      string     `json:"driver"`
	Enabled     bool       `json:"enabled"`
	AutoUp      bool       `json:"auto_up"`
	Iface       string     `json:"iface"`
	Config      string     `json:"config"`
	LastState   string     `json:"last_state"`
	LastError   string     `json:"last_error"`
	LastChecked *time.Time `json:"last_checked,omitempty"`
	LastUpAt    *time.Time `json:"last_up_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

const vpnNetworkCols = `SELECT id, name, driver, COALESCE(enabled, 0), COALESCE(auto_up, 1), COALESCE(iface, ''), COALESCE(config, '{}'), COALESCE(last_state, 'down'), COALESCE(last_error, ''), last_checked, last_up_at, created_at, updated_at FROM vpn_networks `

func scanVPNNetwork(s rowScanner) (*VPNNetwork, error) {
	var n VPNNetwork
	var enabled, autoUp int
	var checked, up sql.NullTime
	if err := s.Scan(&n.ID, &n.Name, &n.Driver, &enabled, &autoUp, &n.Iface, &n.Config, &n.LastState, &n.LastError, &checked, &up, &n.CreatedAt, &n.UpdatedAt); err != nil {
		return nil, err
	}
	n.Enabled = enabled == 1
	n.AutoUp = autoUp == 1
	if checked.Valid {
		n.LastChecked = &checked.Time
	}
	if up.Valid {
		n.LastUpAt = &up.Time
	}
	return &n, nil
}

func (d *DB) ListVPNNetworks() ([]VPNNetwork, error) {
	rows, err := d.Query(vpnNetworkCols + `ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []VPNNetwork
	for rows.Next() {
		n, err := scanVPNNetwork(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *n)
	}
	return list, rows.Err()
}

func (d *DB) GetVPNNetwork(id int64) (*VPNNetwork, error) {
	n, err := scanVPNNetwork(d.QueryRow(vpnNetworkCols+`WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return n, err
}

func (d *DB) CreateVPNNetwork(n *VPNNetwork) (int64, error) {
	res, err := d.Exec(`INSERT INTO vpn_networks (name, driver, enabled, auto_up, iface, config, last_state) VALUES (?, ?, ?, ?, ?, ?, 'down')`,
		n.Name, n.Driver, boolInt(n.Enabled), boolInt(n.AutoUp), n.Iface, n.Config)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) UpdateVPNNetwork(n *VPNNetwork) error {
	_, err := d.Exec(`UPDATE vpn_networks SET name=?, driver=?, enabled=?, auto_up=?, iface=?, config=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		n.Name, n.Driver, boolInt(n.Enabled), boolInt(n.AutoUp), n.Iface, n.Config, n.ID)
	return err
}

func (d *DB) DeleteVPNNetwork(id int64) error {
	_, err := d.Exec(`DELETE FROM vpn_networks WHERE id=?`, id)
	return err
}

// SetVPNNetworkState records the supervisor's verdict for UI badges; detail is already redacted by the caller.
func (d *DB) SetVPNNetworkState(id int64, state, detail string) error {
	_, err := d.Exec(`UPDATE vpn_networks SET last_state=?, last_error=?, last_checked=CURRENT_TIMESTAMP,
		last_up_at=CASE WHEN ? = 'up' THEN CURRENT_TIMESTAMP ELSE last_up_at END WHERE id=?`, state, detail, state, id)
	return err
}

// CountHostsByNetwork returns host counts keyed by network_id, so the list endpoint needs no N+1 query.
func (d *DB) CountHostsByNetwork() (map[int64]int, error) {
	rows, err := d.Query(`SELECT network_id, COUNT(*) FROM hosts WHERE network_id > 0 GROUP BY network_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := make(map[int64]int)
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		counts[id] = n
	}
	return counts, rows.Err()
}

// DetachHostsFromNetwork moves every Host of a network back to direct dialing.
func (d *DB) DetachHostsFromNetwork(id int64) error {
	_, err := d.Exec(`UPDATE hosts SET network_id=0 WHERE network_id=?`, id)
	return err
}
