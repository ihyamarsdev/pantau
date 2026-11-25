package store

import (
	"database/sql"
	"strings"
)

// TFRunFeedRow is a run plus the owning workspace's display fields, for the global Runs feed.
type TFRunFeedRow struct {
	Run                  TFRun
	WorkspaceName        string
	WorkspaceEnvironment string
	WorkspaceGroupName   string
}

// TFRunFeedFilter narrows the global runs feed. Empty fields are ignored.
type TFRunFeedFilter struct {
	Statuses []string // matched with IN
	Type     string
	Trigger  string
	Query    string // matches run id, message, commit SHA or workspace name
	Limit    int    // 1..500, default 100
}

// ListTFRunsFeed returns runs across all workspaces, newest first.
func (d *DB) ListTFRunsFeed(f TFRunFeedFilter) ([]TFRunFeedRow, error) {
	if f.Limit <= 0 {
		f.Limit = 100
	} else if f.Limit > 500 {
		f.Limit = 500
	}

	where := []string{"1=1"}
	args := []any{}
	if len(f.Statuses) > 0 {
		ph := make([]string, len(f.Statuses))
		for i, s := range f.Statuses {
			ph[i] = "?"
			args = append(args, s)
		}
		where = append(where, "status IN ("+strings.Join(ph, ",")+")")
	}
	if f.Type != "" {
		where = append(where, "type = ?")
		args = append(args, f.Type)
	}
	if f.Trigger != "" {
		where = append(where, "trigger_kind = ?")
		args = append(args, f.Trigger)
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		like := "%" + q + "%"
		where = append(where, `(CAST(id AS TEXT) = ? OR message LIKE ? OR commit_sha LIKE ?
			OR workspace_id IN (SELECT id FROM tf_workspaces WHERE name LIKE ?))`)
		args = append(args, q, like, like, like)
	}
	args = append(args, f.Limit)

	// Correlated subqueries avoid ambiguous "id" when joining tf_workspaces.
	rows, err := d.Query(`
		SELECT `+tfRunCols+`,
			(SELECT name FROM tf_workspaces w WHERE w.id = tf_runs.workspace_id),
			(SELECT environment FROM tf_workspaces w WHERE w.id = tf_runs.workspace_id),
			(SELECT group_name FROM tf_workspaces w WHERE w.id = tf_runs.workspace_id)
		FROM tf_runs
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY id DESC LIMIT ?
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]TFRunFeedRow, 0)
	for rows.Next() {
		var row TFRunFeedRow
		var name, env, group sql.NullString
		run, err := scanTFRun(feedScanner{rows: rows, extra: []any{&name, &env, &group}}, false)
		if err != nil {
			return nil, err
		}
		row.Run = *run
		row.WorkspaceName = name.String
		row.WorkspaceEnvironment = env.String
		row.WorkspaceGroupName = group.String
		list = append(list, row)
	}
	return list, rows.Err()
}

// feedScanner appends extra destinations after the run columns scanTFRun already fills.
type feedScanner struct {
	rows  *sql.Rows
	extra []any
}

func (f feedScanner) Scan(dest ...any) error {
	return f.rows.Scan(append(dest, f.extra...)...)
}
