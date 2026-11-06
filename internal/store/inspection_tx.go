package store

// UpsertActualItems applies a batch of ActualItem upserts in ONE transaction.
// ponytail: each autocommit write is a separate fsync-able SQLite transaction; N items = 1 commit.
func (d *DB) UpsertActualItems(items []*ActualItem) error {
	if len(items) == 0 {
		return nil
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO actual_items(host_id, kind, target, current, status, details, updated_at) VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(host_id, kind, target) DO UPDATE SET current=excluded.current, status=excluded.status, details=excluded.details, updated_at=CURRENT_TIMESTAMP`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, it := range items {
		if _, err := stmt.Exec(it.HostID, it.Kind, it.Target, it.Current, it.Status, it.Details); err != nil {
			return err
		}
	}
	return tx.Commit()
}
