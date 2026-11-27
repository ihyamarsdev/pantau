package store

// System Snapshot: the encrypted backup payload, its export and its restore.

import (
	"database/sql"

	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// SnapshotSSHKey stores an SSH library key within a snapshot payload.
type SnapshotSSHKey struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	PrivateKey  string    `json:"private_key"`
	PublicKey   string    `json:"public_key"`
	Fingerprint string    `json:"fingerprint"`
	CreatedAt   time.Time `json:"created_at"`
}

type SnapshotKubeCluster struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	Context       string    `json:"context"`
	Server        string    `json:"server"`
	Provider      string    `json:"provider"`
	KubeMode      string    `json:"kube_mode"`
	KubeClusterID string    `json:"kube_cluster_id,omitempty"`
	Kubeconfig    string    `json:"kubeconfig"`
	CreatedAt     time.Time `json:"created_at"`
}

type SnapshotCloudflareAccount struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	AccountID string    `json:"account_id,omitempty"`
	APIToken  string    `json:"api_token"`
	CreatedAt time.Time `json:"created_at"`
}

type SnapshotS3Account struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Endpoint  string    `json:"endpoint"`
	Region    string    `json:"region"`
	Bucket    string    `json:"bucket"`
	AccessKey string    `json:"access_key"`
	SecretKey string    `json:"secret_key"`
	Prefix    string    `json:"prefix"`
	PathStyle string    `json:"path_style"`
	CreatedAt time.Time `json:"created_at"`
}

type SnapshotAnsiblePlaybook struct {
	ID             int64      `json:"id"`
	Name           string     `json:"name"`
	Description    string     `json:"description"`
	Content        string     `json:"content"`
	ExtraVars      string     `json:"extra_vars"`
	Source         string     `json:"source,omitempty"`
	GitURL         string     `json:"git_url,omitempty"`
	GitBranch      string     `json:"git_branch,omitempty"`
	GitPath        string     `json:"git_path,omitempty"`
	GitAuth        string     `json:"git_auth,omitempty"`
	GitToken       string     `json:"git_token,omitempty"` // snapshot is encrypted (AES-256-GCM), like ssh_keys.private_key
	GitKeyID       int64      `json:"git_key_id,omitempty"`
	Survey         string     `json:"survey,omitempty"` // JSON array of survey fields
	LastSyncAt     *time.Time `json:"last_sync_at,omitempty"`
	LastSyncCommit string     `json:"last_sync_commit,omitempty"`
	GroupName      string     `json:"group_name,omitempty"`
	// Read-only legacy fields of snapshots made while imported folders were ZIP bundles; never written any more.
	BundlePath string    `json:"bundle_path,omitempty"`
	ProjectID  int64     `json:"project_id,omitempty"`
	BundleData []byte    `json:"bundle_data,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// SnapshotAnsibleAsset is a Jinja template or uploaded file (Data is base64 in the JSON, the snapshot is encrypted).
type SnapshotAnsibleAsset struct {
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	Data      []byte    `json:"data"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SnapshotAnsibleProject is an imported-folder ZIP of snapshots made while imports were bundles. It is read when
// restoring (the entry YAML becomes an inline playbook) and never written.
type SnapshotAnsibleProject struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Data      []byte    `json:"data,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SnapshotTFWorkspace stores workspace declarations for backup and restore.
type SnapshotTFWorkspace struct {
	ID                   int64     `json:"id"`
	Name                 string    `json:"name"`
	Description          string    `json:"description"`
	Environment          string    `json:"environment"`
	Tags                 string    `json:"tags"`
	Binary               string    `json:"binary"`
	Source               string    `json:"source"`
	Files                string    `json:"files"`
	GitURL               string    `json:"git_url"`
	GitBranch            string    `json:"git_branch"`
	GitPath              string    `json:"git_path"`
	GitAuth              string    `json:"git_auth"`
	GitToken             string    `json:"git_token"` // plaintext in SQLite; included in encrypted snapshot
	GitKeyID             int64     `json:"git_key_id"`
	WebhookToken         string    `json:"webhook_token"`
	WebhookSecret        string    `json:"webhook_secret"`
	DriftIntervalMinutes int       `json:"drift_interval_minutes"`
	PlanIntervalMinutes  int       `json:"plan_interval_minutes,omitempty"`
	AutoApply            bool      `json:"auto_apply,omitempty"`
	GroupName            string    `json:"group_name,omitempty"`
	CloudAccountIDs      []int64   `json:"cloud_account_ids,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// SnapshotTFCloudAccount stores a cloud account for backup and restore.
type SnapshotTFCloudAccount struct {
	ID          int64             `json:"id"`
	Name        string            `json:"name"`
	Provider    string            `json:"provider"`
	Description string            `json:"description"`
	Fields      map[string]string `json:"fields"` // plaintext in SQLite; included in encrypted snapshot
	VerifiedAt  *time.Time        `json:"verified_at,omitempty"`
	VerifyOK    bool              `json:"verify_ok"`
	VerifyMsg   string            `json:"verify_message"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// SnapshotTFVariable stores variable declarations for backup and restore.
type SnapshotTFVariable struct {
	ID          int64  `json:"id"`
	WorkspaceID int64  `json:"workspace_id,omitempty"`
	VarsetID    int64  `json:"varset_id,omitempty"`
	Key         string `json:"key"`
	Value       string `json:"value"` // plaintext in SQLite; included in encrypted snapshot
	Category    string `json:"category"`
	HCL         bool   `json:"hcl"`
	Sensitive   bool   `json:"sensitive"`
	Description string `json:"description"`
}

// SnapshotTFVarset stores variable set declarations for backup and restore.
type SnapshotTFVarset struct {
	ID           int64   `json:"id"`
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	Global       bool    `json:"global"`
	WorkspaceIDs []int64 `json:"workspace_ids,omitempty"`
}

// SnapshotTFPolicy stores policy declarations for backup and restore.
type SnapshotTFPolicy struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
	Params      string `json:"params"`
	Enforcement string `json:"enforcement"`
	Enabled     bool   `json:"enabled"`
	WorkspaceID int64  `json:"workspace_id"`
}

// SnapshotTFModuleVersion stores a module version in backup.
type SnapshotTFModuleVersion struct {
	ID        int64  `json:"id"`
	ModuleID  int64  `json:"module_id"`
	Version   string `json:"version"`
	Files     string `json:"files"`
	Doc       string `json:"doc"`
	SourceURL string `json:"source_url"`
	CreatedAt string `json:"created_at"`
}

// SnapshotTFModule stores a private module and its versions in backup.
type SnapshotTFModule struct {
	ID          int64                     `json:"id"`
	Name        string                    `json:"name"`
	Provider    string                    `json:"provider"`
	Description string                    `json:"description"`
	Versions    []SnapshotTFModuleVersion `json:"versions,omitempty"`
}

// SnapshotTFState stores the latest state per workspace in backup.
type SnapshotTFState struct {
	WorkspaceID   int64     `json:"workspace_id"`
	Serial        int64     `json:"serial"`
	Lineage       string    `json:"lineage"`
	Size          int64     `json:"size"`
	ResourceCount int       `json:"resource_count"`
	SHA256        string    `json:"sha256"`
	State         []byte    `json:"state"`
	CreatedAt     time.Time `json:"created_at"`
}

// SnapshotPayload holds the sanitized system configuration for backup and restore.
type SnapshotPayload struct {
	Version                int                         `json:"version"`
	CreatedAt              time.Time                   `json:"created_at"`
	Settings               map[string]string           `json:"settings"`
	Hosts                  []Host                      `json:"hosts"`
	DesiredRules           []DesiredRule               `json:"desired_rules"`
	TerminalPresets        []TerminalPreset            `json:"terminal_presets,omitempty"`
	AgentMemory            []AgentMemory               `json:"agent_memory,omitempty"`
	SSHKeys                []SnapshotSSHKey            `json:"ssh_keys,omitempty"`
	KubeClusters           []SnapshotKubeCluster       `json:"kube_clusters,omitempty"`
	CloudflareAccounts     []SnapshotCloudflareAccount `json:"cloudflare_accounts,omitempty"`
	S3Accounts             []SnapshotS3Account         `json:"s3_accounts,omitempty"`
	AnsiblePlaybooks       []SnapshotAnsiblePlaybook   `json:"ansible_playbooks,omitempty"`
	AnsibleProjects        []SnapshotAnsibleProject    `json:"ansible_projects,omitempty"` // read-only legacy, see SnapshotAnsibleProject
	AnsibleSchedules       []AnsibleSchedule           `json:"ansible_schedules,omitempty"`
	AnsibleAssets          []SnapshotAnsibleAsset      `json:"ansible_assets,omitempty"`
	TerraformWorkspaces    []SnapshotTFWorkspace       `json:"terraform_workspaces,omitempty"`
	TerraformCloudAccounts []SnapshotTFCloudAccount    `json:"terraform_cloud_accounts,omitempty"`
	TerraformOutputLinks   []TFOutputLink              `json:"terraform_output_links,omitempty"`
	TerraformVariables     []SnapshotTFVariable        `json:"terraform_variables,omitempty"`
	TerraformVarsets       []SnapshotTFVarset          `json:"terraform_varsets,omitempty"`
	TerraformPolicies      []SnapshotTFPolicy          `json:"terraform_policies,omitempty"`
	TerraformModules       []SnapshotTFModule          `json:"terraform_modules,omitempty"`
	TerraformStates        []SnapshotTFState           `json:"terraform_states,omitempty"`
	VPNNetworks            []VPNNetwork                `json:"vpn_networks,omitempty"`
	// VaultIncluded marks a snapshot that carries the vault rows together with the master key from
	// Settings. Older snapshots lack it, and restoring one must not replace the local master key.
	VaultIncluded bool                   `json:"vault_included,omitempty"`
	VaultSecrets  []SnapshotVaultSecret  `json:"vault_secrets,omitempty"`
	VaultVersions []SnapshotVaultVersion `json:"vault_versions,omitempty"`
}

// SnapshotVaultSecret and SnapshotVaultVersion hold the encrypted rows as stored. They decrypt only with
// the master key in the same snapshot.
type SnapshotVaultSecret struct {
	ID             int64        `json:"id"`
	Key            string       `json:"key"`
	Scope          string       `json:"scope"`
	ScopeRef       string       `json:"scope_ref"`
	Ciphertext     []byte       `json:"ciphertext"`
	CurrentVersion int          `json:"current_version"`
	ExpiresAt      sql.NullTime `json:"expires_at"`
	RotationDays   int          `json:"rotation_days"`
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
}

type SnapshotVaultVersion struct {
	SecretID   int64     `json:"secret_id"`
	Version    int       `json:"version"`
	Ciphertext []byte    `json:"ciphertext"`
	CreatedAt  time.Time `json:"created_at"`
}

func (d *DB) IsFresh() (bool, error) {
	var count int
	if err := d.QueryRow("SELECT COUNT(*) FROM hosts").Scan(&count); err != nil {
		return false, err
	}
	if count > 0 {
		return false, nil
	}
	var completed string
	err := d.QueryRow("SELECT value FROM settings WHERE key = 'bootstrap_completed'").Scan(&completed)
	if err == nil && completed == "1" {
		return false, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}

	var hash string
	err = d.QueryRow("SELECT value FROM settings WHERE key = 'admin_password_hash'").Scan(&hash)
	if err == nil && hash != "" {
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte("admin")) != nil {
			return false, nil
		}
	}

	return true, nil
}

func (d *DB) MarkBootstrapCompleted() error {
	return d.SetSetting("bootstrap_completed", "1")
}

func (d *DB) ExportSnapshot() (*SnapshotPayload, error) {
	hosts, err := d.ListHosts()
	if err != nil {
		return nil, fmt.Errorf("list hosts: %w", err)
	}

	// One set-based read replaces one query per Host; bucket by host_id and append in the
	// same order ListHosts returns so the snapshot payload stays byte-identical.
	rulesByHost := make(map[int64][]DesiredRule)
	ruleRows, err := d.Query(`SELECT id, host_id, kind, target, expected, enabled FROM desired_rules ORDER BY host_id, kind, target`)
	if err != nil {
		return nil, fmt.Errorf("list desired rules: %w", err)
	}
	for ruleRows.Next() {
		var r DesiredRule
		var enabled int
		if err := ruleRows.Scan(&r.ID, &r.HostID, &r.Kind, &r.Target, &r.Expected, &enabled); err != nil {
			ruleRows.Close()
			return nil, fmt.Errorf("scan desired rule: %w", err)
		}
		r.Enabled = enabled == 1
		rulesByHost[r.HostID] = append(rulesByHost[r.HostID], r)
	}
	if err := ruleRows.Err(); err != nil {
		return nil, fmt.Errorf("export snapshot: read rows: %w", err)
	}
	if err := ruleRows.Err(); err != nil {
		ruleRows.Close()
		return nil, fmt.Errorf("iterate desired rules: %w", err)
	}
	ruleRows.Close()
	var allRules []DesiredRule
	for _, h := range hosts {
		allRules = append(allRules, rulesByHost[h.ID]...)
	}

	settings := make(map[string]string)
	rows, err := d.Query("SELECT key, value FROM settings")
	if err != nil {
		return nil, fmt.Errorf("select settings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		settings[k] = v
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("export snapshot: read rows: %w", err)
	}

	var allPresets []TerminalPreset
	pRows, err := d.Query("SELECT id, host_id, name, command, sort_order, use_tmux, created_at FROM terminal_presets ORDER BY sort_order ASC, id ASC")
	if err != nil {
		return nil, fmt.Errorf("export snapshot: %w", err)
	}
	{
		defer pRows.Close()
		for pRows.Next() {
			var p TerminalPreset
			var rawHostID sql.NullInt64
			var useTmux int
			if err := pRows.Scan(&p.ID, &rawHostID, &p.Name, &p.Command, &p.SortOrder, &useTmux, &p.CreatedAt); err != nil {
				return nil, fmt.Errorf("export snapshot: %w", err)
			}
			{
				if rawHostID.Valid {
					hid := rawHostID.Int64
					p.HostID = &hid
				}
				p.UseTmux = useTmux == 1
				allPresets = append(allPresets, p)
			}
		}
		if err := pRows.Err(); err != nil {
			return nil, fmt.Errorf("export snapshot: read rows: %w", err)
		}
	}

	allMemory, err := d.ListAgentMemory(0, "")
	if err != nil {
		return nil, fmt.Errorf("export agent memory: %w", err)
	}

	var allSSHKeys []SnapshotSSHKey
	kRows, err := d.Query("SELECT id, name, private_key, public_key, fingerprint, created_at FROM ssh_keys ORDER BY id ASC")
	if err != nil {
		return nil, fmt.Errorf("export snapshot: %w", err)
	}
	{
		defer kRows.Close()
		for kRows.Next() {
			var k SnapshotSSHKey
			if err := kRows.Scan(&k.ID, &k.Name, &k.PrivateKey, &k.PublicKey, &k.Fingerprint, &k.CreatedAt); err != nil {
				return nil, fmt.Errorf("export snapshot: %w", err)
			}
			{
				allSSHKeys = append(allSSHKeys, k)
			}
		}
		if err := kRows.Err(); err != nil {
			return nil, fmt.Errorf("export snapshot: read rows: %w", err)
		}
	}
	var allClusters []SnapshotKubeCluster
	cRows, err := d.Query("SELECT id, name, context, server, provider, kube_mode, kube_cluster_id, kubeconfig, created_at FROM kube_clusters ORDER BY id ASC")
	if err != nil {
		return nil, fmt.Errorf("export snapshot: %w", err)
	}
	{
		defer cRows.Close()
		for cRows.Next() {
			var c SnapshotKubeCluster
			var clusterUID sql.NullString
			if err := cRows.Scan(&c.ID, &c.Name, &c.Context, &c.Server, &c.Provider, &c.KubeMode, &clusterUID, &c.Kubeconfig, &c.CreatedAt); err != nil {
				return nil, fmt.Errorf("export snapshot: %w", err)
			}
			{
				if clusterUID.Valid {
					c.KubeClusterID = clusterUID.String
				}
				allClusters = append(allClusters, c)
			}
		}
		if err := cRows.Err(); err != nil {
			return nil, fmt.Errorf("export snapshot: read rows: %w", err)
		}
	}
	var allCFAccounts []SnapshotCloudflareAccount
	cfRows, err := d.Query("SELECT id, name, account_id, api_token, created_at FROM cloudflare_accounts ORDER BY id ASC")
	if err != nil {
		return nil, fmt.Errorf("export snapshot: %w", err)
	}
	{
		defer cfRows.Close()
		for cfRows.Next() {
			var a SnapshotCloudflareAccount
			if err := cfRows.Scan(&a.ID, &a.Name, &a.AccountID, &a.APIToken, &a.CreatedAt); err != nil {
				return nil, fmt.Errorf("export snapshot: %w", err)
			}
			{
				allCFAccounts = append(allCFAccounts, a)
			}
		}
		if err := cfRows.Err(); err != nil {
			return nil, fmt.Errorf("export snapshot: read rows: %w", err)
		}
	}
	var allS3Accounts []SnapshotS3Account
	s3Rows, err := d.Query("SELECT " + s3AccountCols + " FROM s3_accounts ORDER BY id ASC")
	if err != nil {
		return nil, fmt.Errorf("export snapshot: %w", err)
	}
	{
		defer s3Rows.Close()
		for s3Rows.Next() {
			{
				a, err := scanS3Account(s3Rows)
				if err != nil {
					return nil, fmt.Errorf("export snapshot: %w", err)
				}
				allS3Accounts = append(allS3Accounts, SnapshotS3Account(*a))
			}
		}
		if err := s3Rows.Err(); err != nil {
			return nil, fmt.Errorf("export snapshot: read rows: %w", err)
		}
	}
	var allPlaybooks []SnapshotAnsiblePlaybook
	pbRows, err := d.Query("SELECT id, name, description, content, extra_vars, source, git_url, git_branch, git_path, git_auth, git_token, git_key_id, survey, last_sync_at, last_sync_commit, group_name, created_at, updated_at FROM ansible_playbooks ORDER BY id ASC")
	if err != nil {
		return nil, fmt.Errorf("export snapshot: %w", err)
	}
	{
		defer pbRows.Close()
		for pbRows.Next() {
			var p SnapshotAnsiblePlaybook
			if err := pbRows.Scan(&p.ID, &p.Name, &p.Description, &p.Content, &p.ExtraVars, &p.Source, &p.GitURL, &p.GitBranch, &p.GitPath, &p.GitAuth, &p.GitToken, &p.GitKeyID, &p.Survey, &p.LastSyncAt, &p.LastSyncCommit, &p.GroupName, &p.CreatedAt, &p.UpdatedAt); err != nil {
				return nil, fmt.Errorf("export snapshot: %w", err)
			}
			{
				allPlaybooks = append(allPlaybooks, p)
			}
		}
		if err := pbRows.Err(); err != nil {
			return nil, fmt.Errorf("export snapshot: read rows: %w", err)
		}
	}
	allSchedules, err := d.ListAnsibleSchedules(0)
	if err != nil {
		return nil, fmt.Errorf("export ansible schedules: %w", err)
	}
	var allAssets []SnapshotAnsibleAsset
	{
		assets, err := d.LoadAnsibleAssets()
		if err != nil {
			return nil, fmt.Errorf("export snapshot: %w", err)
		}
		for _, a := range assets {
			allAssets = append(allAssets, SnapshotAnsibleAsset{Kind: a.Kind, Name: a.Name, Data: a.Data, UpdatedAt: a.UpdatedAt})
		}
	}

	var allTFWorkspaces []SnapshotTFWorkspace
	wsRows, err := d.Query(`SELECT id, name, description, environment, tags, binary, source, files, git_url, git_branch, git_path, git_auth, git_token, git_key_id, webhook_token, webhook_secret, drift_interval_minutes, plan_interval_minutes, auto_apply, group_name, cloud_account_ids, created_at, updated_at FROM tf_workspaces ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("export snapshot: %w", err)
	}
	{
		defer wsRows.Close()
		for wsRows.Next() {
			var w SnapshotTFWorkspace
			var cloudIDs string
			var autoApply int
			if err := wsRows.Scan(&w.ID, &w.Name, &w.Description, &w.Environment, &w.Tags, &w.Binary, &w.Source, &w.Files, &w.GitURL, &w.GitBranch, &w.GitPath, &w.GitAuth, &w.GitToken, &w.GitKeyID, &w.WebhookToken, &w.WebhookSecret, &w.DriftIntervalMinutes, &w.PlanIntervalMinutes, &autoApply, &w.GroupName, &cloudIDs, &w.CreatedAt, &w.UpdatedAt); err != nil {
				return nil, fmt.Errorf("export snapshot: %w", err)
			}
			{
				w.AutoApply = autoApply != 0
				w.CloudAccountIDs = unmarshalCloudAccountIDs(cloudIDs)
				allTFWorkspaces = append(allTFWorkspaces, w)
			}
		}
		if err := wsRows.Err(); err != nil {
			return nil, fmt.Errorf("export snapshot: read rows: %w", err)
		}
	}
	allTFLinks, err := d.ListAllTFOutputLinks()
	if err != nil {
		return nil, fmt.Errorf("export terraform output links: %w", err)
	}
	var allTFCloudAccounts []SnapshotTFCloudAccount
	{
		accts, err := d.ListTFCloudAccounts()
		if err != nil {
			return nil, fmt.Errorf("export snapshot: %w", err)
		}
		for _, a := range accts {
			allTFCloudAccounts = append(allTFCloudAccounts, SnapshotTFCloudAccount{
				ID: a.ID, Name: a.Name, Provider: a.Provider, Description: a.Description, Fields: a.Fields,
				VerifiedAt: a.VerifiedAt, VerifyOK: a.VerifyOK, VerifyMsg: a.VerifyMsg, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
			})
		}
	}
	var allTFVariables []SnapshotTFVariable
	varRows, err := d.Query(`SELECT id, workspace_id, varset_id, key, value, category, hcl, sensitive, description FROM tf_variables ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("export snapshot: %w", err)
	}
	{
		defer varRows.Close()
		for varRows.Next() {
			var v SnapshotTFVariable
			var hclInt, sensInt int
			var wsID, vsID sql.NullInt64
			if err := varRows.Scan(&v.ID, &wsID, &vsID, &v.Key, &v.Value, &v.Category, &hclInt, &sensInt, &v.Description); err != nil {
				return nil, fmt.Errorf("export snapshot: %w", err)
			}
			{
				if wsID.Valid {
					v.WorkspaceID = wsID.Int64
				}
				if vsID.Valid {
					v.VarsetID = vsID.Int64
				}
				v.HCL = hclInt != 0
				v.Sensitive = sensInt != 0
				allTFVariables = append(allTFVariables, v)
			}
		}
		if err := varRows.Err(); err != nil {
			return nil, fmt.Errorf("export snapshot: read rows: %w", err)
		}
	}
	var allTFVarsets []SnapshotTFVarset
	vsRows, err := d.Query(`SELECT id, name, description, global FROM tf_varsets ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("export snapshot: %w", err)
	}
	{
		defer vsRows.Close()
		for vsRows.Next() {
			var s SnapshotTFVarset
			var globalInt int
			if err := vsRows.Scan(&s.ID, &s.Name, &s.Description, &globalInt); err != nil {
				return nil, fmt.Errorf("export snapshot: %w", err)
			}
			{
				s.Global = globalInt != 0
				ids, err := d.GetTFVarsetWorkspaceIDs(s.ID)
				if err != nil {
					return nil, fmt.Errorf("export varset %d workspaces: %w", s.ID, err)
				}
				s.WorkspaceIDs = ids
				allTFVarsets = append(allTFVarsets, s)
			}
		}
		if err := vsRows.Err(); err != nil {
			return nil, fmt.Errorf("export snapshot: read rows: %w", err)
		}
	}
	var allTFPolicies []SnapshotTFPolicy
	polRows, err := d.Query(`SELECT id, name, description, kind, params, enforcement, enabled, workspace_id FROM tf_policies ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("export snapshot: %w", err)
	}
	{
		defer polRows.Close()
		for polRows.Next() {
			var p SnapshotTFPolicy
			var enInt int
			if err := polRows.Scan(&p.ID, &p.Name, &p.Description, &p.Kind, &p.Params, &p.Enforcement, &enInt, &p.WorkspaceID); err != nil {
				return nil, fmt.Errorf("export snapshot: %w", err)
			}
			{
				p.Enabled = enInt != 0
				allTFPolicies = append(allTFPolicies, p)
			}
		}
		if err := polRows.Err(); err != nil {
			return nil, fmt.Errorf("export snapshot: read rows: %w", err)
		}
	}
	var allTFModules []SnapshotTFModule
	modRows, err := d.Query(`SELECT id, name, provider, description FROM tf_modules ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("export snapshot: %w", err)
	}
	{
		defer modRows.Close()
		for modRows.Next() {
			var m SnapshotTFModule
			if err := modRows.Scan(&m.ID, &m.Name, &m.Provider, &m.Description); err != nil {
				return nil, fmt.Errorf("export snapshot: %w", err)
			}
			{
				vRows, err := d.Query(`SELECT id, module_id, version, files, doc, source_url, created_at FROM tf_module_versions WHERE module_id = ? ORDER BY id ASC`, m.ID)
				if err != nil {
					return nil, fmt.Errorf("export snapshot: %w", err)
				}
				{
					for vRows.Next() {
						var mv SnapshotTFModuleVersion
						if err := vRows.Scan(&mv.ID, &mv.ModuleID, &mv.Version, &mv.Files, &mv.Doc, &mv.SourceURL, &mv.CreatedAt); err != nil {
							return nil, fmt.Errorf("export snapshot: %w", err)
						}
						{
							m.Versions = append(m.Versions, mv)
						}
					}
					if err := vRows.Err(); err != nil {
						return nil, fmt.Errorf("export snapshot: read rows: %w", err)
					}
					vRows.Close()
				}
				allTFModules = append(allTFModules, m)
			}
		}
		if err := modRows.Err(); err != nil {
			return nil, fmt.Errorf("export snapshot: read rows: %w", err)
		}
	}
	var allTFStates []SnapshotTFState
	for _, ws := range allTFWorkspaces {
		latest, err := d.LatestTFStateVersion(ws.ID)
		if err == nil && latest != nil {
			allTFStates = append(allTFStates, SnapshotTFState{
				WorkspaceID:   latest.WorkspaceID,
				Serial:        latest.Serial,
				Lineage:       latest.Lineage,
				Size:          latest.Size,
				ResourceCount: latest.ResourceCount,
				SHA256:        latest.SHA256,
				State:         latest.State,
				CreatedAt:     latest.CreatedAt,
			})
		}
	}

	vpnNetworks, err := d.ListVPNNetworks()
	if err != nil {
		return nil, fmt.Errorf("list vpn networks: %w", err)
	}

	// Vault rows are exported as stored (encrypted). A failed read fails the export, so a partial
	// vault is never pushed as a complete backup.
	vaultSecrets, vaultVersions, err := d.exportVaultRows()
	if err != nil {
		return nil, err
	}

	return &SnapshotPayload{
		Version:                1,
		CreatedAt:              time.Now().UTC(),
		Settings:               settings,
		Hosts:                  hosts,
		DesiredRules:           allRules,
		TerminalPresets:        allPresets,
		AgentMemory:            allMemory,
		SSHKeys:                allSSHKeys,
		KubeClusters:           allClusters,
		CloudflareAccounts:     allCFAccounts,
		S3Accounts:             allS3Accounts,
		AnsiblePlaybooks:       allPlaybooks,
		AnsibleSchedules:       allSchedules,
		AnsibleAssets:          allAssets,
		TerraformWorkspaces:    allTFWorkspaces,
		TerraformCloudAccounts: allTFCloudAccounts,
		TerraformOutputLinks:   allTFLinks,
		TerraformVariables:     allTFVariables,
		TerraformVarsets:       allTFVarsets,
		TerraformPolicies:      allTFPolicies,
		TerraformModules:       allTFModules,
		TerraformStates:        allTFStates,
		VPNNetworks:            vpnNetworks,
		VaultIncluded:          true,
		VaultSecrets:           vaultSecrets,
		VaultVersions:          vaultVersions,
	}, nil
}

func (d *DB) ImportSnapshot(payload *SnapshotPayload) error {
	defer d.settingsCache.Clear()
	if payload == nil {
		return fmt.Errorf("empty snapshot payload")
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	tables := []string{
		"alerts", "incidents", "actual_items", "desired_rules", "hosts", "vpn_networks", "terminal_presets", "agent_memory",
		"ssh_keys", "kube_clusters", "cloudflare_accounts", "s3_accounts",
		"ansible_runs", "ansible_schedules", "ansible_playbooks", "ansible_assets",
		"tf_output_links", "tf_runs", "tf_state_versions", "tf_variables", "tf_varset_workspaces", "tf_varsets", "tf_policies", "tf_module_versions", "tf_modules", "tf_workspaces", "tf_cloud_accounts",
	}
	for _, t := range tables {
		if _, err := tx.Exec("DELETE FROM " + t); err != nil { // #nosec G202 -- t comes from the fixed table list above, never from input
			return fmt.Errorf("clear %s: %w", t, err)
		}
	}
	if payload.VaultIncluded {
		if _, err := tx.Exec("DELETE FROM vault_versions"); err != nil {
			return fmt.Errorf("clear vault_versions: %w", err)
		}
		if _, err := tx.Exec("DELETE FROM vault_secrets"); err != nil {
			return fmt.Errorf("clear vault_secrets: %w", err)
		}
		for _, v := range payload.VaultSecrets {
			_, err := tx.Exec(`INSERT INTO vault_secrets (id, key, scope, scope_ref, ciphertext, current_version, expires_at, rotation_days, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				v.ID, v.Key, v.Scope, v.ScopeRef, v.Ciphertext, v.CurrentVersion, v.ExpiresAt, v.RotationDays, v.CreatedAt, v.UpdatedAt)
			if err != nil {
				return fmt.Errorf("insert vault secret %d: %w", v.ID, err)
			}
		}
		for _, v := range payload.VaultVersions {
			_, err := tx.Exec(`INSERT INTO vault_versions (secret_id, version, ciphertext, created_at) VALUES (?, ?, ?, ?)`,
				v.SecretID, v.Version, v.Ciphertext, v.CreatedAt)
			if err != nil {
				return fmt.Errorf("insert vault version %d/%d: %w", v.SecretID, v.Version, err)
			}
		}
	}

	for _, k := range payload.SSHKeys {
		_, err := tx.Exec(`INSERT INTO ssh_keys (id, name, private_key, public_key, fingerprint, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			k.ID, k.Name, k.PrivateKey, k.PublicKey, k.Fingerprint, k.CreatedAt)
		if err != nil {
			return fmt.Errorf("insert ssh key %d: %w", k.ID, err)
		}
	}
	for _, c := range payload.KubeClusters {
		mode := normalizeKubeMode(c.KubeMode)
		provider := c.Provider
		if provider == "" {
			provider = "generic"
		}
		_, err := tx.Exec(`INSERT INTO kube_clusters (id, name, context, server, provider, kube_mode, kube_cluster_id, kubeconfig, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			c.ID, c.Name, c.Context, c.Server, provider, mode, c.KubeClusterID, c.Kubeconfig, c.CreatedAt)
		if err != nil {
			return fmt.Errorf("insert kube cluster %d: %w", c.ID, err)
		}
	}
	for _, a := range payload.CloudflareAccounts {
		_, err := tx.Exec(`INSERT INTO cloudflare_accounts (id, name, account_id, api_token, created_at) VALUES (?, ?, ?, ?, ?)`,
			a.ID, a.Name, a.AccountID, a.APIToken, a.CreatedAt)
		if err != nil {
			return fmt.Errorf("insert cloudflare account %d: %w", a.ID, err)
		}
	}
	for _, a := range payload.S3Accounts {
		_, err := tx.Exec(`INSERT INTO s3_accounts (id, name, endpoint, region, bucket, access_key, secret_key, prefix, path_style, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			a.ID, a.Name, a.Endpoint, a.Region, a.Bucket, a.AccessKey, a.SecretKey, a.Prefix, a.PathStyle, a.CreatedAt)
		if err != nil {
			return fmt.Errorf("insert s3 account %d: %w", a.ID, err)
		}
	}
	// Snapshots made while imported folders were ZIP bundles keep the archive on the playbook or in a project:
	// the entry YAML is restored as an ordinary editable inline playbook.
	legacyArchives := make(map[int64][]byte, len(payload.AnsibleProjects))
	for _, pr := range payload.AnsibleProjects {
		legacyArchives[pr.ID] = pr.Data
	}
	for _, p := range payload.AnsiblePlaybooks {
		if p.Source == "" {
			p.Source = "inline"
		}
		if p.GitAuth == "" {
			p.GitAuth = "none"
		}
		if p.Source == "bundle" {
			archive := p.BundleData
			if len(archive) == 0 {
				archive = legacyArchives[p.ProjectID]
			}
			p.Source, p.Content = "inline", bundleEntryText(archive, p.BundlePath)
		}
		_, err := tx.Exec(`INSERT INTO ansible_playbooks (id, name, description, content, extra_vars, source, git_url, git_branch, git_path, git_auth, git_token, git_key_id, survey, last_sync_at, last_sync_commit, group_name, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			p.ID, p.Name, p.Description, p.Content, p.ExtraVars, p.Source, p.GitURL, p.GitBranch, p.GitPath, p.GitAuth, p.GitToken, p.GitKeyID, p.Survey, p.LastSyncAt, p.LastSyncCommit, p.GroupName, p.CreatedAt, p.UpdatedAt)
		if err != nil {
			return fmt.Errorf("insert ansible playbook %d: %w", p.ID, err)
		}
	}
	for _, sc := range payload.AnsibleSchedules {
		if err := insertAnsibleSchedule(tx, &sc); err != nil {
			return fmt.Errorf("insert ansible schedule %d: %w", sc.ID, err)
		}
	}
	for _, a := range payload.AnsibleAssets {
		if _, err := tx.Exec(`INSERT INTO ansible_assets (kind, name, data, size, updated_at) VALUES (?, ?, ?, ?, ?)`,
			a.Kind, a.Name, a.Data, len(a.Data), a.UpdatedAt); err != nil {
			return fmt.Errorf("insert ansible asset %s/%s: %w", a.Kind, a.Name, err)
		}
	}

	for _, n := range payload.VPNNetworks {
		if _, err := tx.Exec(`INSERT INTO vpn_networks (id, name, driver, enabled, auto_up, iface, config, last_state, last_error, last_checked, last_up_at, created_at, updated_at, identity) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			n.ID, n.Name, n.Driver, boolInt(n.Enabled), boolInt(n.AutoUp), n.Iface, n.Config, n.LastState, n.LastError, n.LastChecked, n.LastUpAt, n.CreatedAt, n.UpdatedAt, n.Identity); err != nil {
			return fmt.Errorf("insert vpn network %d: %w", n.ID, err)
		}
	}
	for _, h := range payload.Hosts {
		onlineInt := 0
		if h.InternetOnline {
			onlineInt = 1
		}
		kubeMode := h.KubeMode
		if kubeMode == "" {
			kubeMode = "full"
		}
		_, err := tx.Exec(`INSERT INTO hosts (
			id, name, host, port, user, custom_key, key_id, status, os_info, kernel, uptime, cpu_load,
			ram_used_bytes, ram_total_bytes, disk_used_bytes, disk_total_bytes, lifecycle_score, lifecycle_notes, lifecycle_breakdown,
			created_at, net_rx_bytes, net_tx_bytes, net_rx_speed_bps, net_tx_speed_bps,
			disk_read_bytes, disk_write_bytes, disk_read_speed_bps, disk_write_speed_bps,
			internet_online, internet_latency_ms, public_ip, private_ips, active_conn_count, failed_logins_count,
			top_connections, listening_ports, notes, sort_order, group_name,
			commission_date, bios_date, os_install_epoch,
			kube_cluster_id, kube_mode, network_id, os_profile, allow_legacy_ssh, ssh_agent, ssh_agent_fingerprint, jump_host_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			h.ID, h.Name, h.Host, h.Port, h.User, h.CustomKey, h.KeyID, h.Status, h.OSInfo, h.Kernel, h.Uptime, h.CPULoad,
			h.RAMUsedBytes, h.RAMTotalBytes, h.DiskUsedBytes, h.DiskTotalBytes, h.LifecycleScore, h.LifecycleNotes, h.LifecycleBreakdown,
			h.CreatedAt, h.NetRxBytes, h.NetTxBytes, h.NetRxSpeedBps, h.NetTxSpeedBps,
			h.DiskReadBytes, h.DiskWriteBytes, h.DiskReadSpeedBps, h.DiskWriteSpeedBps,
			onlineInt, h.InternetLatencyMs, h.PublicIP, h.PrivateIPs, h.ActiveConnCount, h.FailedLoginsCount,
			h.TopConnections, h.ListeningPorts, h.Notes, h.SortOrder, h.GroupName,
			h.CommissionDate, h.BIOSDate, h.OSInstallEpoch,
			h.KubeClusterID, kubeMode, h.NetworkID, h.OSProfile, boolInt(h.AllowLegacySSH), boolInt(h.SSHAgent), h.SSHAgentFingerprint, h.JumpHostID,
		)
		if err != nil {
			return fmt.Errorf("insert host %d: %w", h.ID, err)
		}
	}

	for _, r := range payload.DesiredRules {
		enabledInt := 0
		if r.Enabled {
			enabledInt = 1
		}
		_, err := tx.Exec(`INSERT INTO desired_rules (id, host_id, kind, target, expected, enabled) VALUES (?, ?, ?, ?, ?, ?)`,
			r.ID, r.HostID, r.Kind, r.Target, r.Expected, enabledInt)
		if err != nil {
			return fmt.Errorf("insert desired rule %d: %w", r.ID, err)
		}
	}

	for _, p := range payload.TerminalPresets {
		useTmuxInt := 0
		if p.UseTmux {
			useTmuxInt = 1
		}
		var hid interface{} = nil
		if p.HostID != nil {
			hid = *p.HostID
		}
		_, _ = tx.Exec(`INSERT INTO terminal_presets (id, host_id, name, command, sort_order, use_tmux) VALUES (?, ?, ?, ?, ?, ?)`,
			p.ID, hid, p.Name, p.Command, p.SortOrder, useTmuxInt)
	}

	for _, m := range payload.AgentMemory {
		var hid, cid interface{} = nil, nil
		if m.HostID != nil {
			hid = *m.HostID
		}
		if m.ClusterID != nil {
			cid = *m.ClusterID
		}
		_, err := tx.Exec(`INSERT INTO agent_memory (id, host_id, cluster_id, domain, content, source, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			m.ID, hid, cid, m.Domain, m.Content, m.Source, m.CreatedAt, m.UpdatedAt)
		if err != nil {
			return fmt.Errorf("insert agent memory %d: %w", m.ID, err)
		}
	}

	for _, a := range payload.TerraformCloudAccounts {
		fields, err := marshalCloudFields(a.Fields)
		if err != nil {
			return fmt.Errorf("encode tf cloud account %d: %w", a.ID, err)
		}
		_, err = tx.Exec(`INSERT INTO tf_cloud_accounts (id, name, provider, description, fields, verified_at, verify_ok, verify_msg, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			a.ID, a.Name, a.Provider, a.Description, fields, a.VerifiedAt, boolToInt(a.VerifyOK), a.VerifyMsg, a.CreatedAt, a.UpdatedAt)
		if err != nil {
			return fmt.Errorf("insert tf cloud account %d: %w", a.ID, err)
		}
	}
	for _, w := range payload.TerraformWorkspaces {
		cloudIDs, err := marshalCloudAccountIDs(w.CloudAccountIDs)
		if err != nil {
			return fmt.Errorf("encode tf workspace %d cloud accounts: %w", w.ID, err)
		}
		_, err = tx.Exec(`
			INSERT INTO tf_workspaces (
				id, name, description, environment, tags, binary, source, files,
				git_url, git_branch, git_path, git_auth, git_token, git_key_id,
				webhook_token, webhook_secret, drift_interval_minutes, plan_interval_minutes, auto_apply, group_name, cloud_account_ids, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			w.ID, w.Name, w.Description, w.Environment, w.Tags, w.Binary, w.Source, w.Files,
			w.GitURL, w.GitBranch, w.GitPath, w.GitAuth, w.GitToken, w.GitKeyID,
			w.WebhookToken, w.WebhookSecret, w.DriftIntervalMinutes, w.PlanIntervalMinutes, boolToInt(w.AutoApply), w.GroupName, cloudIDs, w.CreatedAt, w.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("insert tf workspace %d: %w", w.ID, err)
		}
	}
	for _, l := range payload.TerraformOutputLinks {
		if _, err := tx.Exec(`INSERT INTO tf_output_links (id, workspace_id, var_name, source_workspace_id, output_name) VALUES (?, ?, ?, ?, ?)`,
			l.ID, l.WorkspaceID, l.VarName, l.SourceWorkspaceID, l.OutputName); err != nil {
			return fmt.Errorf("insert tf output link %d: %w", l.ID, err)
		}
	}
	for _, vs := range payload.TerraformVarsets {
		_, err := tx.Exec(`INSERT INTO tf_varsets (id, name, description, global) VALUES (?, ?, ?, ?)`,
			vs.ID, vs.Name, vs.Description, boolToInt(vs.Global))
		if err != nil {
			return fmt.Errorf("insert tf varset %d: %w", vs.ID, err)
		}
		for _, wsID := range vs.WorkspaceIDs {
			_, err := tx.Exec(`INSERT INTO tf_varset_workspaces (varset_id, workspace_id) VALUES (?, ?)`, vs.ID, wsID)
			if err != nil {
				return fmt.Errorf("insert tf varset workspace link (%d, %d): %w", vs.ID, wsID, err)
			}
		}
	}
	for _, v := range payload.TerraformVariables {
		var wsID, vsID any
		if v.WorkspaceID > 0 {
			wsID = v.WorkspaceID
		}
		if v.VarsetID > 0 {
			vsID = v.VarsetID
		}
		_, err := tx.Exec(`
			INSERT INTO tf_variables (id, workspace_id, varset_id, key, value, category, hcl, sensitive, description)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, v.ID, wsID, vsID, v.Key, v.Value, v.Category, boolToInt(v.HCL), boolToInt(v.Sensitive), v.Description)
		if err != nil {
			return fmt.Errorf("insert tf variable %d: %w", v.ID, err)
		}
	}
	for _, p := range payload.TerraformPolicies {
		_, err := tx.Exec(`
			INSERT INTO tf_policies (id, name, description, kind, params, enforcement, enabled, workspace_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, p.ID, p.Name, p.Description, p.Kind, p.Params, p.Enforcement, boolToInt(p.Enabled), p.WorkspaceID)
		if err != nil {
			return fmt.Errorf("insert tf policy %d: %w", p.ID, err)
		}
	}
	for _, m := range payload.TerraformModules {
		_, err := tx.Exec(`INSERT INTO tf_modules (id, name, provider, description) VALUES (?, ?, ?, ?)`,
			m.ID, m.Name, m.Provider, m.Description)
		if err != nil {
			return fmt.Errorf("insert tf module %d: %w", m.ID, err)
		}
		for _, mv := range m.Versions {
			_, err := tx.Exec(`
				INSERT INTO tf_module_versions (id, module_id, version, files, doc, source_url, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?)
			`, mv.ID, mv.ModuleID, mv.Version, mv.Files, mv.Doc, mv.SourceURL, mv.CreatedAt)
			if err != nil {
				return fmt.Errorf("insert tf module version %d: %w", mv.ID, err)
			}
		}
	}
	for _, st := range payload.TerraformStates {
		_, err := tx.Exec(`
			INSERT INTO tf_state_versions (workspace_id, run_id, serial, lineage, source, size, resource_count, sha256, state, created_at)
			VALUES (?, 0, ?, ?, 'import', ?, ?, ?, ?, ?)
		`, st.WorkspaceID, st.Serial, st.Lineage, st.Size, st.ResourceCount, st.SHA256, st.State, st.CreatedAt)
		if err != nil {
			return fmt.Errorf("insert tf state version for workspace %d: %w", st.WorkspaceID, err)
		}
	}
	for k, v := range payload.Settings {
		// A legacy snapshot has no vault rows; swapping the master key under local secrets would make them unreadable.
		if k == vaultMasterKeySetting && !payload.VaultIncluded {
			continue
		}
		_, err := tx.Exec(`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v)
		if err != nil {
			return fmt.Errorf("insert setting %s: %w", k, err)
		}
	}

	_, _ = tx.Exec(`INSERT INTO settings (key, value) VALUES ('bootstrap_completed', '1') ON CONFLICT(key) DO UPDATE SET value = '1'`)

	if err := tx.Commit(); err != nil {
		return err
	}
	// A snapshot made before multi-account S3 carries the single-account settings: turn them into an account.
	// A snapshot of the old AI agent carries the hermes_* settings: drop them.
	if err := d.migrateLegacyS3Settings(); err != nil {
		return err
	}
	return d.migrateAgentSettings()
}

// exportVaultRows reads every vault secret and version as stored (ciphertext), failing on any read error.
func (d *DB) exportVaultRows() ([]SnapshotVaultSecret, []SnapshotVaultVersion, error) {
	var secrets []SnapshotVaultSecret
	rows, err := d.Query(`SELECT id, key, scope, scope_ref, ciphertext, current_version, expires_at, rotation_days, created_at, updated_at FROM vault_secrets ORDER BY id`)
	if err != nil {
		return nil, nil, fmt.Errorf("export vault secrets: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var v SnapshotVaultSecret
		if err := rows.Scan(&v.ID, &v.Key, &v.Scope, &v.ScopeRef, &v.Ciphertext, &v.CurrentVersion, &v.ExpiresAt, &v.RotationDays, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, nil, fmt.Errorf("scan vault secret: %w", err)
		}
		secrets = append(secrets, v)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("export vault secrets: %w", err)
	}

	var versions []SnapshotVaultVersion
	vRows, err := d.Query(`SELECT secret_id, version, ciphertext, created_at FROM vault_versions ORDER BY secret_id, version`)
	if err != nil {
		return nil, nil, fmt.Errorf("export vault versions: %w", err)
	}
	defer vRows.Close()
	for vRows.Next() {
		var v SnapshotVaultVersion
		if err := vRows.Scan(&v.SecretID, &v.Version, &v.Ciphertext, &v.CreatedAt); err != nil {
			return nil, nil, fmt.Errorf("scan vault version: %w", err)
		}
		versions = append(versions, v)
	}
	if err := vRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("export vault versions: %w", err)
	}
	return secrets, versions, nil
}
