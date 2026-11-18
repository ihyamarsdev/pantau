package store

// Terminal Presets: the saved shell commands, global or per host.

import (
	"database/sql"
	"time"
)

type TerminalPreset struct {
	ID        int64     `json:"id"`
	HostID    *int64    `json:"host_id"` // nil indicates a global preset
	Name      string    `json:"name"`
	Command   string    `json:"command"`
	SortOrder int       `json:"sort_order"`
	UseTmux   bool      `json:"use_tmux"`
	CreatedAt time.Time `json:"created_at"`
}

func scanPreset(s rowScanner) (*TerminalPreset, error) {
	var p TerminalPreset
	var rawHostID sql.NullInt64
	var useTmux int
	if err := s.Scan(&p.ID, &rawHostID, &p.Name, &p.Command, &p.SortOrder, &useTmux, &p.CreatedAt); err != nil {
		return nil, err
	}
	if rawHostID.Valid {
		hid := rawHostID.Int64
		p.HostID = &hid
	}
	p.UseTmux = useTmux == 1
	return &p, nil
}

func (d *DB) queryPresets(query string, args ...any) ([]TerminalPreset, error) {
	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var presets []TerminalPreset
	for rows.Next() {
		p, err := scanPreset(rows)
		if err != nil {
			return nil, err
		}
		presets = append(presets, *p)
	}
	return presets, nil
}

// Terminal Preset operations
func (d *DB) ListTerminalPresets(hostID *int64) ([]TerminalPreset, error) {
	if hostID == nil {
		return d.queryPresets(`SELECT id, host_id, name, command, sort_order, use_tmux, created_at FROM terminal_presets WHERE host_id IS NULL ORDER BY sort_order ASC, id ASC`)
	}
	return d.queryPresets(`SELECT id, host_id, name, command, sort_order, use_tmux, created_at FROM terminal_presets WHERE host_id IS NULL OR host_id = ? ORDER BY sort_order ASC, id ASC`, *hostID)
}

func (d *DB) ListAllTerminalPresets() ([]TerminalPreset, error) {
	return d.queryPresets(`SELECT id, host_id, name, command, sort_order, use_tmux, created_at FROM terminal_presets ORDER BY sort_order ASC, id ASC`)
}

func (d *DB) GetTerminalPreset(id int64) (*TerminalPreset, error) {
	p, err := scanPreset(d.QueryRow(`SELECT id, host_id, name, command, sort_order, use_tmux, created_at FROM terminal_presets WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (d *DB) CreateTerminalPreset(p *TerminalPreset) (int64, error) {
	useTmuxInt := 0
	if p.UseTmux {
		useTmuxInt = 1
	}
	var rawHostID interface{} = nil
	if p.HostID != nil {
		rawHostID = *p.HostID
	}
	res, err := d.Exec(`INSERT INTO terminal_presets (host_id, name, command, sort_order, use_tmux) VALUES (?, ?, ?, ?, ?)`,
		rawHostID, p.Name, p.Command, p.SortOrder, useTmuxInt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) UpdateTerminalPreset(p *TerminalPreset) error {
	useTmuxInt := 0
	if p.UseTmux {
		useTmuxInt = 1
	}
	var rawHostID interface{} = nil
	if p.HostID != nil {
		rawHostID = *p.HostID
	}
	_, err := d.Exec(`UPDATE terminal_presets SET host_id = ?, name = ?, command = ?, sort_order = ?, use_tmux = ? WHERE id = ?`,
		rawHostID, p.Name, p.Command, p.SortOrder, useTmuxInt, p.ID)
	return err
}

func (d *DB) DeleteTerminalPreset(id int64) error {
	_, err := d.Exec(`DELETE FROM terminal_presets WHERE id = ?`, id)
	return err
}
