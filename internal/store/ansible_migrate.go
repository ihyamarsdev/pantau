package store

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"unicode/utf8"
)

const maxPlaybookContent = 256 * 1024

// bundleEntryText returns the text of one file of a legacy imported-folder ZIP. When the file cannot be read
// (missing, oversized, corrupt, not text) it returns a comment saying so, so the playbook stays a valid, editable
// inline playbook instead of failing the whole migration.
func bundleEntryText(archive []byte, name string) string {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err == nil {
		for _, f := range zr.File {
			if f.Name != name || f.UncompressedSize64 > maxPlaybookContent {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				break
			}
			data, err := io.ReadAll(io.LimitReader(rc, maxPlaybookContent+1))
			rc.Close()
			if err == nil && len(data) > 0 && len(data) <= maxPlaybookContent && utf8.Valid(data) {
				return string(data)
			}
			break
		}
	}
	return fmt.Sprintf("# Pantau could not read %q from the stored folder when imported folders became editable playbooks.\n", name)
}

// migrateAnsibleBundles turns imported-folder playbooks (source 'bundle': the ZIP lives in ansible_projects, or in
// the older ansible_playbooks.bundle_data) into ordinary inline playbooks holding the entry YAML, then drops the
// bundle columns and the projects table. Files that sat next to the playbook are not kept: roles and collections
// come from the Library. Idempotent; a database without any bundle leftovers is untouched.
func (d *DB) migrateAnsibleBundles() error {
	has := map[string]bool{}
	rows, err := d.Query(`SELECT name FROM pragma_table_info('ansible_playbooks')`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		has[name] = true
	}
	rows.Close()
	var projects int
	if err := d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'ansible_projects'`).Scan(&projects); err != nil {
		return err
	}
	if !has["bundle_path"] && !has["project_id"] && !has["bundle_data"] && projects == 0 {
		return nil
	}

	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if has["bundle_path"] {
		join, data := "", "X''"
		if has["project_id"] && projects > 0 {
			join, data = " LEFT JOIN ansible_projects pr ON pr.id = p.project_id", "COALESCE(pr.data, X'')"
		}
		if has["bundle_data"] {
			data = "CASE WHEN length(" + data + ") > 0 THEN " + data + " ELSE p.bundle_data END"
		}
		found, err := tx.Query(`SELECT p.id, p.bundle_path, ` + data + ` FROM ansible_playbooks p` + join + ` WHERE p.source = 'bundle'`)
		if err != nil {
			return err
		}
		type legacy struct {
			id      int64
			path    string
			archive []byte
		}
		var all []legacy
		for found.Next() {
			var l legacy
			if err := found.Scan(&l.id, &l.path, &l.archive); err != nil {
				found.Close()
				return err
			}
			all = append(all, l)
		}
		found.Close()
		for _, l := range all {
			if _, err := tx.Exec(`UPDATE ansible_playbooks SET source = 'inline', content = ? WHERE id = ?`, bundleEntryText(l.archive, l.path), l.id); err != nil {
				return err
			}
		}
	}
	for _, col := range []string{"bundle_path", "project_id", "bundle_data"} {
		if has[col] {
			if _, err := tx.Exec(`ALTER TABLE ansible_playbooks DROP COLUMN ` + col); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(`DROP TABLE IF EXISTS ansible_projects`); err != nil {
		return err
	}
	return tx.Commit()
}
