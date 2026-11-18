package store

// Alerts and Incidents: what happened on a host and the alert that followed.

import (
	"time"
)

type Incident struct {
	ID               int64     `json:"id"`
	HostID           int64     `json:"host_id"`
	Kind             string    `json:"kind"`
	Target           string    `json:"target"`
	EventType        string    `json:"event_type"` // drift_detected, drift_resolved
	Summary          string    `json:"summary"`
	RootCauseExcerpt string    `json:"root_cause_excerpt"`
	CreatedAt        time.Time `json:"created_at"`
}

type Alert struct {
	ID           int64     `json:"id"`
	HostID       int64     `json:"host_id"`
	HostName     string    `json:"host_name"`
	Level        string    `json:"level"` // critical, warning, info
	Message      string    `json:"message"`
	RootCause    string    `json:"root_cause"`
	Acknowledged bool      `json:"acknowledged"`
	CreatedAt    time.Time `json:"created_at"`
}

// Incident operations
func (d *DB) RecordIncident(inc *Incident) error {
	_, err := d.Exec(`INSERT INTO incidents(host_id, kind, target, event_type, summary, root_cause_excerpt) VALUES (?, ?, ?, ?, ?, ?)`,
		inc.HostID, inc.Kind, inc.Target, inc.EventType, inc.Summary, inc.RootCauseExcerpt)
	if err != nil {
		return err
	}
	if inc.HostID != 0 { // ponytail: keep latest 500 incidents per host
		_, _ = d.Exec(`DELETE FROM incidents WHERE host_id = ? AND id NOT IN (SELECT id FROM incidents WHERE host_id = ? ORDER BY id DESC LIMIT 500)`, inc.HostID, inc.HostID)
	}
	return nil
}

func (d *DB) ListIncidents(hostID int64, limit int) ([]Incident, error) {
	query := `SELECT id, host_id, kind, target, event_type, summary, root_cause_excerpt, created_at FROM incidents WHERE host_id = ? ORDER BY id DESC LIMIT ?`
	rows, err := d.Query(query, hostID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var incs []Incident
	for rows.Next() {
		var inc Incident
		if err := rows.Scan(&inc.ID, &inc.HostID, &inc.Kind, &inc.Target, &inc.EventType, &inc.Summary, &inc.RootCauseExcerpt, &inc.CreatedAt); err != nil {
			return nil, err
		}
		incs = append(incs, inc)
	}
	return incs, nil
}

// Alert operations
func (d *DB) CreateAlert(alt *Alert) (int64, error) {
	res, err := d.Exec(`INSERT INTO alerts(host_id, level, message, root_cause) VALUES (?, ?, ?, ?)`,
		alt.HostID, alt.Level, alt.Message, alt.RootCause)
	if err != nil {
		return 0, err
	}
	if alt.HostID != 0 { // ponytail: keep latest 500 acknowledged alerts per host; unacknowledged never pruned
		_, _ = d.Exec(`DELETE FROM alerts WHERE host_id = ? AND acknowledged = 1 AND id NOT IN (SELECT id FROM alerts WHERE host_id = ? AND acknowledged = 1 ORDER BY id DESC LIMIT 500)`, alt.HostID, alt.HostID)
	}
	return res.LastInsertId()
}

func (d *DB) ListActiveAlerts() ([]Alert, error) {
	rows, err := d.Query(`SELECT a.id, COALESCE(a.host_id,0), COALESCE(h.name,'System'), a.level, a.message, a.root_cause, a.acknowledged, a.created_at
		FROM alerts a LEFT JOIN hosts h ON a.host_id = h.id WHERE a.acknowledged = 0 ORDER BY a.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Alert
	for rows.Next() {
		var a Alert
		var ack int
		if err := rows.Scan(&a.ID, &a.HostID, &a.HostName, &a.Level, &a.Message, &a.RootCause, &ack, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Acknowledged = ack == 1
		list = append(list, a)
	}
	return list, nil
}

func (d *DB) AcknowledgeAlert(id int64) error {
	_, err := d.Exec(`UPDATE alerts SET acknowledged = 1 WHERE id = ?`, id)
	return err
}

func (d *DB) AcknowledgeAllAlerts() error {
	_, err := d.Exec(`UPDATE alerts SET acknowledged = 1 WHERE acknowledged = 0`)
	return err
}

func (d *DB) AcknowledgeHostAlerts(hostID int64) error {
	_, err := d.Exec(`UPDATE alerts SET acknowledged = 1 WHERE host_id = ? AND acknowledged = 0`, hostID)
	return err
}
