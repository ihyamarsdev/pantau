package store

// File Pins: the directories and files an administrator marked on a host.

import (
	"time"
)

type FilePin struct {
	ID        int64     `json:"id"`
	HostID    int64     `json:"host_id"`
	Path      string    `json:"path"`
	IsDir     bool      `json:"is_dir"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

type GlobalFilePin struct {
	ID        int64     `json:"id"`
	HostID    int64     `json:"host_id"`
	HostName  string    `json:"host_name"`
	HostIP    string    `json:"host_ip"`
	Path      string    `json:"path"`
	IsDir     bool      `json:"is_dir"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

func (d *DB) GetFilePins(hostID int64) ([]FilePin, error) {
	rows, err := d.Query(`
		SELECT id, host_id, path, is_dir, note, created_at
		FROM host_file_pins
		WHERE host_id = ?
		ORDER BY is_dir DESC, path ASC
	`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pins []FilePin
	for rows.Next() {
		var p FilePin
		var isDirInt int
		if err := rows.Scan(&p.ID, &p.HostID, &p.Path, &isDirInt, &p.Note, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.IsDir = isDirInt != 0
		pins = append(pins, p)
	}
	if pins == nil {
		pins = []FilePin{}
	}
	return pins, rows.Err()
}

func (d *DB) GetAllFilePins() ([]GlobalFilePin, error) {
	rows, err := d.Query(`
		SELECT p.id, p.host_id, h.name, h.host, p.path, p.is_dir, p.note, p.created_at
		FROM host_file_pins p
		JOIN hosts h ON p.host_id = h.id
		ORDER BY h.name ASC, p.is_dir DESC, p.path ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pins []GlobalFilePin
	for rows.Next() {
		var p GlobalFilePin
		var isDirInt int
		if err := rows.Scan(&p.ID, &p.HostID, &p.HostName, &p.HostIP, &p.Path, &isDirInt, &p.Note, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.IsDir = isDirInt != 0
		pins = append(pins, p)
	}
	if pins == nil {
		pins = []GlobalFilePin{}
	}
	return pins, rows.Err()
}

func (d *DB) SaveFilePin(hostID int64, path string, isDir bool, note string) error {
	isDirInt := 0
	if isDir {
		isDirInt = 1
	}
	_, err := d.Exec(`
		INSERT INTO host_file_pins (host_id, path, is_dir, note, created_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(host_id, path) DO UPDATE SET
			is_dir = excluded.is_dir,
			note = excluded.note
	`, hostID, path, isDirInt, note)
	return err
}

func (d *DB) DeleteFilePin(hostID int64, path string) error {
	_, err := d.Exec(`DELETE FROM host_file_pins WHERE host_id = ? AND path = ?`, hostID, path)
	return err
}
