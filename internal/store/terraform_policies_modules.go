package store

import (
	"database/sql"
	"fmt"
)

// TFPolicy represents a policy rule evaluated against plan JSON.
type TFPolicy struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"`        // deny_destroy | max_deletes | max_changes | deny_resource_types | require_tags | deny_open_cidr
	Params      string `json:"params"`      // JSON object
	Enforcement string `json:"enforcement"` // advisory | soft | hard
	Enabled     bool   `json:"enabled"`
	WorkspaceID int64  `json:"workspace_id"` // 0 = applies to all workspaces
}

// CreateTFPolicy creates a new policy record.
func (d *DB) CreateTFPolicy(p *TFPolicy) (int64, error) {
	if p.Params == "" {
		p.Params = "{}"
	}
	if p.Enforcement == "" {
		p.Enforcement = "soft"
	}
	res, err := d.Exec(`
		INSERT INTO tf_policies (name, description, kind, params, enforcement, enabled, workspace_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, p.Name, p.Description, p.Kind, p.Params, p.Enforcement, boolToInt(p.Enabled), p.WorkspaceID)
	if err != nil {
		if isUniqueConstraintError(err) {
			return 0, fmt.Errorf("%w: %q", ErrTFPolicyNameTaken, p.Name)
		}
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	p.ID = id
	return id, nil
}

// UpdateTFPolicy updates policy attributes.
func (d *DB) UpdateTFPolicy(p *TFPolicy) error {
	if p.Params == "" {
		p.Params = "{}"
	}
	if p.Enforcement == "" {
		p.Enforcement = "soft"
	}
	res, err := d.Exec(`
		UPDATE tf_policies
		SET name = ?, description = ?, kind = ?, params = ?, enforcement = ?, enabled = ?, workspace_id = ?
		WHERE id = ?
	`, p.Name, p.Description, p.Kind, p.Params, p.Enforcement, boolToInt(p.Enabled), p.WorkspaceID, p.ID)
	if err != nil {
		if isUniqueConstraintError(err) {
			return fmt.Errorf("%w: %q", ErrTFPolicyNameTaken, p.Name)
		}
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTFPolicyNotFound
	}
	return nil
}

// DeleteTFPolicy deletes a policy by ID.
func (d *DB) DeleteTFPolicy(id int64) error {
	res, err := d.Exec(`DELETE FROM tf_policies WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTFPolicyNotFound
	}
	return nil
}

// GetTFPolicy retrieves a policy by ID.
func (d *DB) GetTFPolicy(id int64) (*TFPolicy, error) {
	var p TFPolicy
	var enabledInt int
	err := d.QueryRow(`
		SELECT id, name, description, kind, params, enforcement, enabled, workspace_id
		FROM tf_policies WHERE id = ?
	`, id).Scan(&p.ID, &p.Name, &p.Description, &p.Kind, &p.Params, &p.Enforcement, &enabledInt, &p.WorkspaceID)
	if err == sql.ErrNoRows {
		return nil, ErrTFPolicyNotFound
	}
	if err != nil {
		return nil, err
	}
	p.Enabled = enabledInt != 0
	return &p, nil
}

// ListTFPolicies lists policies, optionally filtered by workspace (or 0 for global/all).
func (d *DB) ListTFPolicies(workspaceID int64) ([]TFPolicy, error) {
	rows, err := d.Query(`
		SELECT id, name, description, kind, params, enforcement, enabled, workspace_id
		FROM tf_policies
		WHERE (? = 0 OR workspace_id = 0 OR workspace_id = ?)
		ORDER BY name COLLATE NOCASE ASC
	`, workspaceID, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]TFPolicy, 0)
	for rows.Next() {
		var p TFPolicy
		var enabledInt int
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Kind, &p.Params, &p.Enforcement, &enabledInt, &p.WorkspaceID); err != nil {
			return nil, err
		}
		p.Enabled = enabledInt != 0
		list = append(list, p)
	}
	return list, rows.Err()
}

// --- TFModule and TFModuleVersion ---

// TFModuleVersion represents a version of a module in the private registry.
type TFModuleVersion struct {
	ID        int64  `json:"id"`
	ModuleID  int64  `json:"module_id"`
	Version   string `json:"version"`         // semver
	Files     string `json:"files,omitempty"` // JSON string map[string]string
	Doc       string `json:"doc,omitempty"`   // JSON string ModuleDoc
	SourceURL string `json:"source_url"`
	CreatedAt string `json:"created_at"`
}

// TFModule represents a module in the private catalog.
type TFModule struct {
	ID            int64             `json:"id"`
	Name          string            `json:"name"`
	Provider      string            `json:"provider"`
	Description   string            `json:"description"`
	LatestVersion string            `json:"latest_version,omitempty"`
	VersionCount  int               `json:"version_count"`
	Versions      []TFModuleVersion `json:"versions,omitempty"`
}

// CreateTFModule creates a new module entry.
func (d *DB) CreateTFModule(m *TFModule) (int64, error) {
	res, err := d.Exec(`
		INSERT INTO tf_modules (name, provider, description)
		VALUES (?, ?, ?)
	`, m.Name, m.Provider, m.Description)
	if err != nil {
		if isUniqueConstraintError(err) {
			return 0, fmt.Errorf("%w: %s/%s", ErrTFModuleNameTaken, m.Provider, m.Name)
		}
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	m.ID = id
	return id, nil
}

// UpdateTFModule updates module details.
func (d *DB) UpdateTFModule(m *TFModule) error {
	res, err := d.Exec(`
		UPDATE tf_modules
		SET name = ?, provider = ?, description = ?
		WHERE id = ?
	`, m.Name, m.Provider, m.Description, m.ID)
	if err != nil {
		if isUniqueConstraintError(err) {
			return fmt.Errorf("%w: %s/%s", ErrTFModuleNameTaken, m.Provider, m.Name)
		}
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTFModuleNotFound
	}
	return nil
}

// DeleteTFModule deletes a module and all its versions.
func (d *DB) DeleteTFModule(id int64) error {
	res, err := d.Exec(`DELETE FROM tf_modules WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTFModuleNotFound
	}
	return nil
}

// GetTFModule retrieves a module by ID, including its versions list.
func (d *DB) GetTFModule(id int64) (*TFModule, error) {
	var m TFModule
	err := d.QueryRow(`
		SELECT id, name, provider, description
		FROM tf_modules WHERE id = ?
	`, id).Scan(&m.ID, &m.Name, &m.Provider, &m.Description)
	if err == sql.ErrNoRows {
		return nil, ErrTFModuleNotFound
	}
	if err != nil {
		return nil, err
	}

	vers, err := d.ListTFModuleVersions(id, true)
	if err != nil {
		return nil, err
	}
	m.Versions = vers
	m.VersionCount = len(vers)
	if len(vers) > 0 {
		m.LatestVersion = vers[0].Version
	}
	return &m, nil
}

// ListTFModules lists all modules with latest version and version count.
func (d *DB) ListTFModules() ([]TFModule, error) {
	rows, err := d.Query(`
		SELECT m.id, m.name, m.provider, m.description,
		       (SELECT v.version FROM tf_module_versions v WHERE v.module_id = m.id ORDER BY v.id DESC LIMIT 1) AS latest_version,
		       (SELECT COUNT(*) FROM tf_module_versions v WHERE v.module_id = m.id) AS version_count
		FROM tf_modules m
		ORDER BY m.name COLLATE NOCASE ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]TFModule, 0)
	for rows.Next() {
		var m TFModule
		var latestVers sql.NullString
		if err := rows.Scan(&m.ID, &m.Name, &m.Provider, &m.Description, &latestVers, &m.VersionCount); err != nil {
			return nil, err
		}
		if latestVers.Valid {
			m.LatestVersion = latestVers.String
		}
		list = append(list, m)
	}
	return list, rows.Err()
}

// CreateTFModuleVersion inserts a new module version.
func (d *DB) CreateTFModuleVersion(mv *TFModuleVersion) (int64, error) {
	if mv.Files == "" {
		mv.Files = "{}"
	}
	if mv.Doc == "" {
		mv.Doc = "{}"
	}
	res, err := d.Exec(`
		INSERT INTO tf_module_versions (module_id, version, files, doc, source_url)
		VALUES (?, ?, ?, ?, ?)
	`, mv.ModuleID, mv.Version, mv.Files, mv.Doc, mv.SourceURL)
	if err != nil {
		if isUniqueConstraintError(err) {
			return 0, fmt.Errorf("%w: %s", ErrTFModuleVersionTaken, mv.Version)
		}
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	mv.ID = id
	return id, nil
}

// DeleteTFModuleVersion deletes a specific version of a module.
func (d *DB) DeleteTFModuleVersion(moduleID int64, version string) error {
	res, err := d.Exec(`DELETE FROM tf_module_versions WHERE module_id = ? AND version = ?`, moduleID, version)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTFModuleVersionNotFound
	}
	return nil
}

// GetTFModuleVersion retrieves a specific module version including Files and Doc.
func (d *DB) GetTFModuleVersion(moduleID int64, version string) (*TFModuleVersion, error) {
	var mv TFModuleVersion
	err := d.QueryRow(`
		SELECT id, module_id, version, files, doc, source_url, created_at
		FROM tf_module_versions
		WHERE module_id = ? AND version = ?
	`, moduleID, version).Scan(&mv.ID, &mv.ModuleID, &mv.Version, &mv.Files, &mv.Doc, &mv.SourceURL, &mv.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrTFModuleVersionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &mv, nil
}

// ListTFModuleVersions lists versions for a module, newest first.
func (d *DB) ListTFModuleVersions(moduleID int64, withDoc bool) ([]TFModuleVersion, error) {
	var docCol string
	if withDoc {
		docCol = "doc"
	} else {
		docCol = "''"
	}
	q := fmt.Sprintf(`
		SELECT id, module_id, version, %s, source_url, created_at
		FROM tf_module_versions
		WHERE module_id = ?
		ORDER BY id DESC
	`, docCol)
	rows, err := d.Query(q, moduleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]TFModuleVersion, 0)
	for rows.Next() {
		var mv TFModuleVersion
		if err := rows.Scan(&mv.ID, &mv.ModuleID, &mv.Version, &mv.Doc, &mv.SourceURL, &mv.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, mv)
	}
	return list, rows.Err()
}
