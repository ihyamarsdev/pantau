package store

// TFOutputLink feeds an output of SourceWorkspaceID into a variable of WorkspaceID at run time.
type TFOutputLink struct {
	ID                int64  `json:"id"`
	WorkspaceID       int64  `json:"workspace_id"`
	VarName           string `json:"var_name"`
	SourceWorkspaceID int64  `json:"source_workspace_id"`
	OutputName        string `json:"output_name"`
}

const tfLinkCols = `id, workspace_id, var_name, source_workspace_id, output_name`

func (d *DB) queryTFOutputLinks(where string, args ...any) ([]TFOutputLink, error) {
	rows, err := d.Query(`SELECT `+tfLinkCols+` FROM tf_output_links `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TFOutputLink{}
	for rows.Next() {
		var l TFOutputLink
		if err := rows.Scan(&l.ID, &l.WorkspaceID, &l.VarName, &l.SourceWorkspaceID, &l.OutputName); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ListTFOutputLinks returns the links feeding a workspace (never nil).
func (d *DB) ListTFOutputLinks(workspaceID int64) ([]TFOutputLink, error) {
	return d.queryTFOutputLinks(`WHERE workspace_id = ?`, workspaceID)
}

// ListAllTFOutputLinks returns every link; used for cycle detection.
func (d *DB) ListAllTFOutputLinks() ([]TFOutputLink, error) {
	return d.queryTFOutputLinks(``)
}

// ReplaceTFOutputLinks atomically replaces all links of a workspace.
func (d *DB) ReplaceTFOutputLinks(workspaceID int64, links []TFOutputLink) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM tf_output_links WHERE workspace_id = ?`, workspaceID); err != nil {
		return err
	}
	for _, l := range links {
		if _, err := tx.Exec(`INSERT INTO tf_output_links (workspace_id, var_name, source_workspace_id, output_name) VALUES (?, ?, ?, ?)`,
			workspaceID, l.VarName, l.SourceWorkspaceID, l.OutputName); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteTFOutputLinksFor removes links where the workspace is the consumer or the source.
func (d *DB) DeleteTFOutputLinksFor(workspaceID int64) error {
	_, err := d.Exec(`DELETE FROM tf_output_links WHERE workspace_id = ? OR source_workspace_id = ?`, workspaceID, workspaceID)
	return err
}
