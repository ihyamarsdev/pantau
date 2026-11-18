package store

// Host Groups: grouping hosts, renaming a group, and the case-insensitive
// spelling rule that keeps a group name unique.

import (
	"database/sql"
	"fmt"
	"strings"
)

// hostGroupSpelling returns the spelling an existing Host Group already has when name differs from it only in letter
// case, so a group name is unique regardless of case. Otherwise it returns name trimmed. skipID excludes the host being
// written: it may be the only member of its group and may change that group's case.
func hostGroupSpelling(tx *sql.Tx, name string, skipID int64) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil
	}
	rows, err := tx.Query(`SELECT DISTINCT group_name FROM hosts WHERE group_name <> '' AND id <> ? ORDER BY group_name`, skipID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	match := name
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return "", err
		}
		if g == name {
			return name, nil
		}
		if match == name && strings.EqualFold(g, name) {
			match = g
		}
	}
	return match, rows.Err()
}

// HostGroupAssignment sets the group of one host. An empty GroupName clears it.
type HostGroupAssignment struct {
	ID        int64  `json:"id"`
	GroupName string `json:"group_name"`
}

// UpdateHostGroups applies all assignments atomically: if any host id does not
// exist nothing is changed.
// A group name that matches an existing group only in letter case takes that group's spelling (see hostGroupSpelling).
func (d *DB) UpdateHostGroups(assignments []HostGroupAssignment) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`UPDATE hosts SET group_name=? WHERE id=?`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, a := range assignments {
		group, err := hostGroupSpelling(tx, a.GroupName, a.ID)
		if err != nil {
			return err
		}
		res, err := stmt.Exec(group, a.ID)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err == nil && n == 0 {
			return fmt.Errorf("host %d: %w", a.ID, ErrHostNotFound)
		}
	}
	return tx.Commit()
}

// RenameHostGroup renames a Host Group on every host that has it, and on the Ansible schedules that target it by
// name, in one transaction. A new name that matches another existing group only in letter case takes that group's
// spelling (the groups merge instead of showing twice). It returns the final name and how many hosts moved.
func (d *DB) RenameHostGroup(from, to string) (string, int64, error) {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	tx, err := d.Begin()
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT DISTINCT group_name FROM hosts WHERE group_name <> ''`)
	if err != nil {
		return "", 0, err
	}
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			rows.Close()
			return "", 0, err
		}
		if g != from && strings.EqualFold(g, to) {
			to = g
		}
	}
	rows.Close()

	res, err := tx.Exec(`UPDATE hosts SET group_name=? WHERE group_name=?`, to, from)
	if err != nil {
		return "", 0, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return "", 0, ErrHostGroupNotFound
	}
	if _, err := tx.Exec(`UPDATE ansible_schedules SET group_name=? WHERE group_name=?`, to, from); err != nil {
		return "", 0, err
	}
	return to, n, tx.Commit()
}
