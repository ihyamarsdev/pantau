package store

import (
	"database/sql"
	"fmt"
)

// TFVariable represents a variable belonging to a workspace or a variable set.
type TFVariable struct {
	ID          int64  `json:"id"`
	WorkspaceID int64  `json:"workspace_id,omitempty"`
	VarsetID    int64  `json:"varset_id,omitempty"`
	Key         string `json:"key"`
	Value       string `json:"value,omitempty"` // sensitive: blank on list/get unless overridden or non-sensitive
	Category    string `json:"category"`        // "terraform" | "env"
	HCL         bool   `json:"hcl"`
	Sensitive   bool   `json:"sensitive"`
	Description string `json:"description"`
	Origin      string `json:"origin,omitempty"` // computed: "workspace" | "varset:<name>"
}

// TFVarset represents a variable set that can be shared across workspaces or marked global.
type TFVarset struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	Description   string  `json:"description"`
	Global        bool    `json:"global"`
	VariableCount int     `json:"variable_count"`
	WorkspaceIDs  []int64 `json:"workspace_ids,omitempty"`
}

// CreateTFVariable inserts a variable record.
func (d *DB) CreateTFVariable(v *TFVariable) (int64, error) {
	if v.Category == "" {
		v.Category = "terraform"
	}
	var wsID, vsID any
	if v.WorkspaceID > 0 {
		wsID = v.WorkspaceID
	}
	if v.VarsetID > 0 {
		vsID = v.VarsetID
	}
	res, err := d.Exec(`
		INSERT INTO tf_variables (workspace_id, varset_id, key, value, category, hcl, sensitive, description)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, wsID, vsID, v.Key, v.Value, v.Category, boolToInt(v.HCL), boolToInt(v.Sensitive), v.Description)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	v.ID = id
	return id, nil
}

// UpdateTFVariable updates variable fields.
// If the variable is sensitive and the provided value is empty, the existing value is preserved.
func (d *DB) UpdateTFVariable(v *TFVariable) error {
	if v.Category == "" {
		v.Category = "terraform"
	}
	var res sql.Result
	var err error
	if v.Sensitive && v.Value == "" {
		res, err = d.Exec(`
			UPDATE tf_variables
			SET key = ?, category = ?, hcl = ?, sensitive = ?, description = ?
			WHERE id = ?
		`, v.Key, v.Category, boolToInt(v.HCL), boolToInt(v.Sensitive), v.Description, v.ID)
	} else {
		res, err = d.Exec(`
			UPDATE tf_variables
			SET key = ?, value = ?, category = ?, hcl = ?, sensitive = ?, description = ?
			WHERE id = ?
		`, v.Key, v.Value, v.Category, boolToInt(v.HCL), boolToInt(v.Sensitive), v.Description, v.ID)
	}
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTFVariableNotFound
	}
	return nil
}

// DeleteTFVariable removes a variable by ID.
func (d *DB) DeleteTFVariable(id int64) error {
	res, err := d.Exec(`DELETE FROM tf_variables WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTFVariableNotFound
	}
	return nil
}

// GetTFVariable retrieves a variable by ID.
func (d *DB) GetTFVariable(id int64) (*TFVariable, error) {
	var v TFVariable
	var hclInt, sensInt int
	var wsID, vsID sql.NullInt64
	err := d.QueryRow(`
		SELECT id, workspace_id, varset_id, key, value, category, hcl, sensitive, description
		FROM tf_variables WHERE id = ?
	`, id).Scan(&v.ID, &wsID, &vsID, &v.Key, &v.Value, &v.Category, &hclInt, &sensInt, &v.Description)
	if err == sql.ErrNoRows {
		return nil, ErrTFVariableNotFound
	}
	if err != nil {
		return nil, err
	}
	if wsID.Valid {
		v.WorkspaceID = wsID.Int64
	}
	if vsID.Valid {
		v.VarsetID = vsID.Int64
	}
	v.HCL = hclInt != 0
	v.Sensitive = sensInt != 0
	return &v, nil
}

// ListTFVariables lists variables directly attached to a workspace (workspaceID > 0) or varset (varsetID > 0).
func (d *DB) ListTFVariables(workspaceID, varsetID int64) ([]TFVariable, error) {
	rows, err := d.Query(`
		SELECT id, workspace_id, varset_id, key, value, category, hcl, sensitive, description
		FROM tf_variables
		WHERE (? = 0 OR workspace_id = ?) AND (? = 0 OR varset_id = ?)
		ORDER BY key ASC
	`, workspaceID, workspaceID, varsetID, varsetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]TFVariable, 0)
	for rows.Next() {
		var v TFVariable
		var hclInt, sensInt int
		var wsID, vsID sql.NullInt64
		if err := rows.Scan(&v.ID, &wsID, &vsID, &v.Key, &v.Value, &v.Category, &hclInt, &sensInt, &v.Description); err != nil {
			return nil, err
		}
		if wsID.Valid {
			v.WorkspaceID = wsID.Int64
		}
		if vsID.Valid {
			v.VarsetID = vsID.Int64
		}
		v.HCL = hclInt != 0
		v.Sensitive = sensInt != 0
		if v.WorkspaceID > 0 {
			v.Origin = "workspace"
		}
		list = append(list, v)
	}
	return list, rows.Err()
}

// EffectiveTFVariables returns the resolved list of variables for a workspace.
// Precedence: workspace > connected varset > global varset.
// Each variable has its Origin field populated ("workspace" or "varset:<name>").
func (d *DB) EffectiveTFVariables(workspaceID int64) ([]TFVariable, error) {
	type keyPair struct {
		key      string
		category string
	}
	resolved := make(map[keyPair]TFVariable)
	order := make([]keyPair, 0)

	// 1. Global varsets (lowest precedence)
	rows, err := d.Query(`
		SELECT v.id, v.workspace_id, v.varset_id, v.key, v.value, v.category, v.hcl, v.sensitive, v.description, s.name
		FROM tf_variables v
		JOIN tf_varsets s ON s.id = v.varset_id
		WHERE s.global = 1
		ORDER BY s.id ASC, v.id ASC
	`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var v TFVariable
		var sName string
		var hclInt, sensInt int
		var wsID, vsID sql.NullInt64
		if err := rows.Scan(&v.ID, &wsID, &vsID, &v.Key, &v.Value, &v.Category, &hclInt, &sensInt, &v.Description, &sName); err != nil {
			rows.Close()
			return nil, err
		}
		if wsID.Valid {
			v.WorkspaceID = wsID.Int64
		}
		if vsID.Valid {
			v.VarsetID = vsID.Int64
		}
		v.HCL = hclInt != 0
		v.Sensitive = sensInt != 0
		v.Origin = "varset:" + sName
		kp := keyPair{v.Key, v.Category}
		if _, exists := resolved[kp]; !exists {
			order = append(order, kp)
		}
		resolved[kp] = v
	}
	rows.Close()

	// 2. Connected varsets
	rows, err = d.Query(`
		SELECT v.id, v.workspace_id, v.varset_id, v.key, v.value, v.category, v.hcl, v.sensitive, v.description, s.name
		FROM tf_variables v
		JOIN tf_varsets s ON s.id = v.varset_id
		JOIN tf_varset_workspaces vw ON vw.varset_id = s.id
		WHERE vw.workspace_id = ? AND s.global = 0
		ORDER BY s.id ASC, v.id ASC
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var v TFVariable
		var sName string
		var hclInt, sensInt int
		var wsID, vsID sql.NullInt64
		if err := rows.Scan(&v.ID, &wsID, &vsID, &v.Key, &v.Value, &v.Category, &hclInt, &sensInt, &v.Description, &sName); err != nil {
			rows.Close()
			return nil, err
		}
		if wsID.Valid {
			v.WorkspaceID = wsID.Int64
		}
		if vsID.Valid {
			v.VarsetID = vsID.Int64
		}
		v.HCL = hclInt != 0
		v.Sensitive = sensInt != 0
		v.Origin = "varset:" + sName
		kp := keyPair{v.Key, v.Category}
		if _, exists := resolved[kp]; !exists {
			order = append(order, kp)
		}
		resolved[kp] = v
	}
	rows.Close()

	// 3. Workspace variables (highest precedence)
	rows, err = d.Query(`
		SELECT id, workspace_id, varset_id, key, value, category, hcl, sensitive, description
		FROM tf_variables
		WHERE workspace_id = ?
		ORDER BY id ASC
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var v TFVariable
		var hclInt, sensInt int
		var wsID, vsID sql.NullInt64
		if err := rows.Scan(&v.ID, &wsID, &vsID, &v.Key, &v.Value, &v.Category, &hclInt, &sensInt, &v.Description); err != nil {
			rows.Close()
			return nil, err
		}
		if wsID.Valid {
			v.WorkspaceID = wsID.Int64
		}
		if vsID.Valid {
			v.VarsetID = vsID.Int64
		}
		v.HCL = hclInt != 0
		v.Sensitive = sensInt != 0
		v.Origin = "workspace"
		kp := keyPair{v.Key, v.Category}
		if _, exists := resolved[kp]; !exists {
			order = append(order, kp)
		}
		resolved[kp] = v
	}
	rows.Close()

	result := make([]TFVariable, 0, len(order))
	for _, kp := range order {
		result = append(result, resolved[kp])
	}
	return result, nil
}

// --- TFVarset CRUD and Workspace Association ---

// CreateTFVarset creates a new variable set.
func (d *DB) CreateTFVarset(vs *TFVarset) (int64, error) {
	res, err := d.Exec(`
		INSERT INTO tf_varsets (name, description, global)
		VALUES (?, ?, ?)
	`, vs.Name, vs.Description, boolToInt(vs.Global))
	if err != nil {
		if isUniqueConstraintError(err) {
			return 0, fmt.Errorf("%w: %q", ErrTFVarsetNameTaken, vs.Name)
		}
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	vs.ID = id
	if err := d.SetTFVarsetWorkspaces(id, vs.WorkspaceIDs); err != nil {
		return 0, err
	}
	return id, nil
}

// UpdateTFVarset updates varset attributes.
func (d *DB) UpdateTFVarset(vs *TFVarset) error {
	res, err := d.Exec(`
		UPDATE tf_varsets
		SET name = ?, description = ?, global = ?
		WHERE id = ?
	`, vs.Name, vs.Description, boolToInt(vs.Global), vs.ID)
	if err != nil {
		if isUniqueConstraintError(err) {
			return fmt.Errorf("%w: %q", ErrTFVarsetNameTaken, vs.Name)
		}
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTFVarsetNotFound
	}
	return nil
}

// DeleteTFVarset deletes a varset. Cascades delete to its variables and workspace links.
func (d *DB) DeleteTFVarset(id int64) error {
	res, err := d.Exec(`DELETE FROM tf_varsets WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTFVarsetNotFound
	}
	return nil
}

// GetTFVarset retrieves a variable set by ID, including its variable count and attached workspace IDs.
func (d *DB) GetTFVarset(id int64) (*TFVarset, error) {
	var vs TFVarset
	var globalInt int
	err := d.QueryRow(`
		SELECT id, name, description, global
		FROM tf_varsets WHERE id = ?
	`, id).Scan(&vs.ID, &vs.Name, &vs.Description, &globalInt)
	if err == sql.ErrNoRows {
		return nil, ErrTFVarsetNotFound
	}
	if err != nil {
		return nil, err
	}
	vs.Global = globalInt != 0

	// Variable count
	_ = d.QueryRow(`SELECT COUNT(*) FROM tf_variables WHERE varset_id = ?`, id).Scan(&vs.VariableCount)

	// Workspace IDs
	wsIDs, err := d.GetTFVarsetWorkspaceIDs(id)
	if err != nil {
		return nil, err
	}
	vs.WorkspaceIDs = wsIDs
	return &vs, nil
}

// ListTFVarsets lists all variable sets with counts and workspace IDs.
func (d *DB) ListTFVarsets() ([]TFVarset, error) {
	rows, err := d.Query(`SELECT id, name, description, global FROM tf_varsets ORDER BY name COLLATE NOCASE ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]TFVarset, 0)
	for rows.Next() {
		var vs TFVarset
		var globalInt int
		if err := rows.Scan(&vs.ID, &vs.Name, &vs.Description, &globalInt); err != nil {
			return nil, err
		}
		vs.Global = globalInt != 0
		_ = d.QueryRow(`SELECT COUNT(*) FROM tf_variables WHERE varset_id = ?`, vs.ID).Scan(&vs.VariableCount)
		wsIDs, _ := d.GetTFVarsetWorkspaceIDs(vs.ID)
		vs.WorkspaceIDs = wsIDs
		list = append(list, vs)
	}
	return list, rows.Err()
}

// GetTFVarsetWorkspaceIDs returns all workspace IDs linked to a varset.
func (d *DB) GetTFVarsetWorkspaceIDs(varsetID int64) ([]int64, error) {
	rows, err := d.Query(`SELECT workspace_id FROM tf_varset_workspaces WHERE varset_id = ? ORDER BY workspace_id ASC`, varsetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SetTFVarsetWorkspaces replaces the workspaces linked to a varset.
func (d *DB) SetTFVarsetWorkspaces(varsetID int64, workspaceIDs []int64) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM tf_varset_workspaces WHERE varset_id = ?`, varsetID); err != nil {
		return err
	}
	for _, wsID := range workspaceIDs {
		if _, err := tx.Exec(`INSERT INTO tf_varset_workspaces (varset_id, workspace_id) VALUES (?, ?)`, varsetID, wsID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
