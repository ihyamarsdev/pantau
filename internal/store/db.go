package store

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/ssh"
	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
	// ponytail: GetSetting is hit on every inspection/tick; single-process app, so every settings write path clears this.
	settingsCache sync.Map
}
type SSHKey struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	PublicKey   string    `json:"public_key"`  // authorized_keys line derived from private key
	Fingerprint string    `json:"fingerprint"` // "SHA256:..."
	CreatedAt   time.Time `json:"created_at"`
	UsedBy      int       `json:"used_by"` // number of hosts with key_id = this id (computed on list)
	PrivateKey  string    `json:"-"`       // NEVER serialized to any API
}

var (
	ErrSSHKeyNotFound = errors.New("ssh key not found")
	ErrSSHKeyInUse    = errors.New("ssh key is in use")
	ErrHostNotFound   = errors.New("host not found")
)

type Host struct {
	ID                 int64      `json:"id"`
	Name               string     `json:"name"`
	Host               string     `json:"host"`
	Port               int        `json:"port"`
	User               string     `json:"user"`
	CustomKey          string     `json:"custom_key,omitempty"` // secret: API handlers must blank it (redactHost); only System Snapshot serializes it
	HasCustomKey       bool       `json:"has_custom_key"`
	KeyID              int64      `json:"key_id"`
	Status             string     `json:"status"` // healthy, degraded, down, unknown
	OSInfo             string     `json:"os_info"`
	Kernel             string     `json:"kernel"`
	Uptime             string     `json:"uptime"`
	CPULoad            string     `json:"cpu_load"`
	RAMUsedBytes       int64      `json:"ram_used_bytes"`
	RAMTotalBytes      int64      `json:"ram_total_bytes"`
	DiskUsedBytes      int64      `json:"disk_used_bytes"`
	DiskTotalBytes     int64      `json:"disk_total_bytes"`
	CPUCores           int        `json:"cpu_cores"`
	HardwareModel      string     `json:"hardware_model"`
	SwapUsedBytes      int64      `json:"swap_used_bytes"`
	SwapTotalBytes     int64      `json:"swap_total_bytes"`
	LifecycleScore     int        `json:"lifecycle_score"`
	LifecycleNotes     string     `json:"lifecycle_notes"`
	LifecycleBreakdown string     `json:"lifecycle_breakdown"`
	LastInspected      *time.Time `json:"last_inspected"`
	CreatedAt          time.Time  `json:"created_at"`

	// Network & Security Observability
	NetRxBytes    int64 `json:"net_rx_bytes"`
	NetTxBytes    int64 `json:"net_tx_bytes"`
	NetRxSpeedBps int64 `json:"net_rx_speed_bps"`
	NetTxSpeedBps int64 `json:"net_tx_speed_bps"`

	// Disk I/O Observability
	DiskReadBytes     int64 `json:"disk_read_bytes"`
	DiskWriteBytes    int64 `json:"disk_write_bytes"`
	DiskReadSpeedBps  int64 `json:"disk_read_speed_bps"`
	DiskWriteSpeedBps int64 `json:"disk_write_speed_bps"`

	InternetOnline    bool   `json:"internet_online"`
	InternetLatencyMs int64  `json:"internet_latency_ms"`
	PublicIP          string `json:"public_ip"`
	ActiveConnCount   int    `json:"active_conn_count"`
	FailedLoginsCount int    `json:"failed_logins_count"`
	TopConnections    string `json:"top_connections"` // JSON serialized array of TopConn
	ListeningPorts    string `json:"listening_ports"` // JSON serialized array of ListeningPort
	Notes             string `json:"notes"`
	SortOrder         int    `json:"sort_order"`
	GroupName         string `json:"group_name"`
	LastDurationMs    int64  `json:"last_duration_ms"`
	CommissionDate    string `json:"commission_date"`    // YYYY-MM-DD manual override for New Old Stock
	BIOSDate          string `json:"bios_date"`          // Raw motherboard BIOS release date telemetry
	OSInstallEpoch    int64  `json:"os_install_epoch"`   // Unix epoch of OS deployment for VM age tracking
	TopCPUProcesses   string `json:"top_cpu_processes"`  // JSON serialized []ProcessInfo
	TopRAMProcesses   string `json:"top_ram_processes"`  // JSON serialized []ProcessInfo
	TopSwapProcesses  string `json:"top_swap_processes"` // JSON serialized []ProcessInfo
	KubeClusterID     string `json:"kube_cluster_id,omitempty"`
	KubeMode          string `json:"kube_mode"`
}

type ProcessInfo struct {
	PID       int     `json:"pid"`
	User      string  `json:"user"`
	CPU       float64 `json:"cpu"`
	MEM       float64 `json:"mem"`
	RSSBytes  int64   `json:"rss_bytes"`
	SwapBytes int64   `json:"swap_bytes,omitempty"`
	Command   string  `json:"command"`
}

type HostPackage struct {
	ID            int64     `json:"id"`
	HostID        int64     `json:"host_id"`
	Name          string    `json:"name"`
	Version       string    `json:"version"`
	SizeBytes     int64     `json:"size_bytes"`
	InstallEpoch  int64     `json:"install_epoch"`
	IsManual      bool      `json:"is_manual"`
	ServiceName   string    `json:"service_name,omitempty"`
	ServiceStatus string    `json:"service_status,omitempty"`
	LastScannedAt time.Time `json:"last_scanned_at"`
}

type TopConn struct {
	RemoteIP string `json:"remote_ip"`
	Port     string `json:"port"`
	Count    int    `json:"count"`
}

type ListeningPort struct {
	Proto   string `json:"proto"`
	Port    string `json:"port"`
	Address string `json:"address"`
	Public  bool   `json:"public"`
	Process string `json:"process"`
	// ProcessSource says where Process came from: "process" (owner seen via ss/netstat/lsof) or
	// "port_hint" (owner not visible; name guessed from the well-known port number). Empty when unknown.
	ProcessSource string `json:"process_source"`
	Risk          string `json:"risk"` // "low", "high"
}

type DesiredRule struct {
	ID       int64  `json:"id"`
	HostID   int64  `json:"host_id"`
	Kind     string `json:"kind"`     // container, disk, backup, cron, service
	Target   string `json:"target"`   // e.g. "nginx", "/", "/var/backups/db.sql.gz", "mysql"
	Expected string `json:"expected"` // e.g. "running", "<85%", "fresh_24h", "active"
	Enabled  bool   `json:"enabled"`
}

type ActualItem struct {
	ID        int64     `json:"id"`
	HostID    int64     `json:"host_id"`
	Kind      string    `json:"kind"`
	Target    string    `json:"target"`
	Current   string    `json:"current"`
	Status    string    `json:"status"` // ok, drift
	Details   string    `json:"details"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Incident struct {
	ID               int64     `json:"id"`
	HostID           int64     `json:"host_id"`
	Kind             string    `json:"kind"`
	Target           string    `json:"target"`
	EventType        string    `json:"event_type"` // drift_detected, drift_resolved
	Summary          string    `json:"summary"`
	RootCauseExcerpt string    `json:"root_cause_excerpt"`
	CreatedAt        time.Time `json:"created_at"`
}

type Alert struct {
	ID           int64     `json:"id"`
	HostID       int64     `json:"host_id"`
	HostName     string    `json:"host_name"`
	Level        string    `json:"level"` // critical, warning, info
	Message      string    `json:"message"`
	RootCause    string    `json:"root_cause"`
	Acknowledged bool      `json:"acknowledged"`
	CreatedAt    time.Time `json:"created_at"`
}

type TerminalPreset struct {
	ID        int64     `json:"id"`
	HostID    *int64    `json:"host_id"` // nil indicates a global preset
	Name      string    `json:"name"`
	Command   string    `json:"command"`
	SortOrder int       `json:"sort_order"`
	UseTmux   bool      `json:"use_tmux"`
	CreatedAt time.Time `json:"created_at"`
}

type InspectionRun struct {
	ID         int64     `json:"id"`
	HostID     int64     `json:"host_id"`
	StartedAt  time.Time `json:"started_at"`
	DurationMs int64     `json:"duration_ms"`
	Status     string    `json:"status"` // ok, drift, degraded, down, error
	Summary    string    `json:"summary"`
	Details    string    `json:"details"`
	CreatedAt  time.Time `json:"created_at"`
}

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

func Open(dbPath string) (*DB, error) {
	if dir := filepath.Dir(dbPath); dir != "." && dir != "" {
		_ = os.MkdirAll(dir, 0755)
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	s := &DB{DB: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	if err := s.initDefaults(); err != nil {
		db.Close()
		return nil, fmt.Errorf("init defaults: %w", err)
	}

	return s, nil
}

func (d *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS hosts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		host TEXT NOT NULL,
		port INTEGER NOT NULL DEFAULT 22,
		user TEXT NOT NULL DEFAULT 'root',
		custom_key TEXT,
		key_id INTEGER DEFAULT 0,
		status TEXT DEFAULT 'unknown',
		os_info TEXT DEFAULT '',
		kernel TEXT DEFAULT '',
		uptime TEXT DEFAULT '',
		cpu_load TEXT DEFAULT '',
		ram_used_bytes INTEGER DEFAULT 0,
		ram_total_bytes INTEGER DEFAULT 0,
		disk_used_bytes INTEGER DEFAULT 0,
		disk_total_bytes INTEGER DEFAULT 0,
		cpu_cores INTEGER DEFAULT 1,
		hardware_model TEXT DEFAULT '',
		swap_used_bytes INTEGER DEFAULT 0,
		swap_total_bytes INTEGER DEFAULT 0,
		lifecycle_score INTEGER DEFAULT 100,
		lifecycle_notes TEXT DEFAULT '',
		lifecycle_breakdown TEXT DEFAULT '[]',
		last_inspected DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		net_rx_bytes INTEGER DEFAULT 0,
		net_tx_bytes INTEGER DEFAULT 0,
		net_rx_speed_bps INTEGER DEFAULT 0,
		net_tx_speed_bps INTEGER DEFAULT 0,
		disk_read_bytes INTEGER DEFAULT 0,
		disk_write_bytes INTEGER DEFAULT 0,
		disk_read_speed_bps INTEGER DEFAULT 0,
		disk_write_speed_bps INTEGER DEFAULT 0,
		internet_online INTEGER DEFAULT 0,
		internet_latency_ms INTEGER DEFAULT 0,
		public_ip TEXT DEFAULT '',
		active_conn_count INTEGER DEFAULT 0,
		failed_logins_count INTEGER DEFAULT 0,
		top_connections TEXT DEFAULT '',
		listening_ports TEXT DEFAULT '',
		notes TEXT DEFAULT '',
		sort_order INTEGER DEFAULT 0,
		group_name TEXT DEFAULT '',
		commission_date TEXT DEFAULT '',
		bios_date TEXT DEFAULT '',
		os_install_epoch INTEGER DEFAULT 0,
		top_cpu_processes TEXT DEFAULT '',
		top_ram_processes TEXT DEFAULT '',
		top_swap_processes TEXT DEFAULT '',
		kube_cluster_id TEXT DEFAULT '',
		kube_mode TEXT NOT NULL DEFAULT 'full'
	);

	CREATE TABLE IF NOT EXISTS host_packages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		version TEXT NOT NULL,
		size_bytes INTEGER NOT NULL DEFAULT 0,
		install_epoch INTEGER NOT NULL DEFAULT 0,
		is_manual INTEGER NOT NULL DEFAULT 0,
		service_name TEXT NOT NULL DEFAULT '',
		service_status TEXT NOT NULL DEFAULT '',
		last_scanned_at DATETIME NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_host_packages_host_manual ON host_packages(host_id, is_manual);

	CREATE TABLE IF NOT EXISTS desired_rules (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
		kind TEXT NOT NULL,
		target TEXT NOT NULL,
		expected TEXT NOT NULL,
		enabled INTEGER DEFAULT 1,
		UNIQUE(host_id, kind, target)
	);

	CREATE TABLE IF NOT EXISTS actual_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
		kind TEXT NOT NULL,
		target TEXT NOT NULL,
		current TEXT NOT NULL,
		status TEXT NOT NULL,
		details TEXT DEFAULT '',
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(host_id, kind, target)
	);

	CREATE TABLE IF NOT EXISTS incidents (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
		kind TEXT NOT NULL,
		target TEXT NOT NULL,
		event_type TEXT NOT NULL,
		summary TEXT NOT NULL,
		root_cause_excerpt TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_incidents_host ON incidents(host_id, id DESC);

	CREATE TABLE IF NOT EXISTS alerts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id INTEGER REFERENCES hosts(id) ON DELETE CASCADE,
		level TEXT NOT NULL,
		message TEXT NOT NULL,
		root_cause TEXT DEFAULT '',
		acknowledged INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_alerts_host ON alerts(host_id, acknowledged);

	CREATE TABLE IF NOT EXISTS terminal_presets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id INTEGER REFERENCES hosts(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		command TEXT NOT NULL,
		sort_order INTEGER DEFAULT 0,
		use_tmux INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS inspection_runs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
		started_at DATETIME NOT NULL,
		duration_ms INTEGER NOT NULL,
		status TEXT NOT NULL,
		summary TEXT NOT NULL,
		details TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_inspection_runs_host ON inspection_runs(host_id, id DESC);
	CREATE INDEX IF NOT EXISTS idx_inspection_runs_status ON inspection_runs(host_id, status, id DESC);

	CREATE TABLE IF NOT EXISTS host_file_pins (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
		path TEXT NOT NULL,
		is_dir INTEGER NOT NULL DEFAULT 0,
		note TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(host_id, path)
	);
	CREATE INDEX IF NOT EXISTS idx_host_file_pins_host ON host_file_pins(host_id);

	-- Compose projects last seen on a host (JSON), so a stack stays listed after "compose down" removes its containers.
	CREATE TABLE IF NOT EXISTS docker_stacks (
		host_id INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		data TEXT NOT NULL,
		PRIMARY KEY (host_id, name)
	);

	CREATE TABLE IF NOT EXISTS ssh_keys (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		private_key TEXT NOT NULL,
		public_key TEXT NOT NULL,
		fingerprint TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS security_scans (
		host_id INTEGER PRIMARY KEY REFERENCES hosts(id) ON DELETE CASCADE,
		scanned_at DATETIME NOT NULL,
		status TEXT,
		package_manager TEXT,
		os_family TEXT,
		pending_security INTEGER,
		pending_total INTEGER,
		severity_json TEXT,
		reboot_required INTEGER,
		repo_cache_age_hours INTEGER,
		findings_json TEXT,
		hardening_json TEXT,
		notes_json TEXT,
		error TEXT
	);
	CREATE TABLE IF NOT EXISTS kube_clusters (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE COLLATE NOCASE,
		context TEXT NOT NULL DEFAULT '',
		server TEXT NOT NULL DEFAULT '',
		provider TEXT NOT NULL DEFAULT 'generic',
		kube_mode TEXT NOT NULL DEFAULT 'full',
		kube_cluster_id TEXT DEFAULT '',
		kubeconfig TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS cloudflare_accounts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE COLLATE NOCASE,
		account_id TEXT NOT NULL DEFAULT '',
		api_token TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS ansible_projects (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL DEFAULT '',
		data BLOB NOT NULL DEFAULT X'',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS ansible_assets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		kind TEXT NOT NULL,
		name TEXT NOT NULL,
		data BLOB NOT NULL DEFAULT X'',
		size INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE (kind, name)
	);
	CREATE TABLE IF NOT EXISTS ansible_playbooks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE COLLATE NOCASE,
		description TEXT NOT NULL DEFAULT '',
		content TEXT NOT NULL DEFAULT '',
		extra_vars TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS ansible_runs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		playbook_id INTEGER NOT NULL REFERENCES ansible_playbooks(id) ON DELETE CASCADE,
		playbook_name TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'queued',
		mode TEXT NOT NULL DEFAULT 'run',
		targets TEXT NOT NULL DEFAULT '[]',
		extra_vars TEXT NOT NULL DEFAULT '',
		tags TEXT NOT NULL DEFAULT '',
		skip_tags TEXT NOT NULL DEFAULT '',
		verbosity INTEGER NOT NULL DEFAULT 0,
		become INTEGER NOT NULL DEFAULT 0,
		exit_code INTEGER,
		log TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		started_at DATETIME,
		finished_at DATETIME
	);
	CREATE INDEX IF NOT EXISTS idx_ansible_runs_playbook_id_id ON ansible_runs(playbook_id, id);
	CREATE TABLE IF NOT EXISTS ansible_schedules (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		playbook_id INTEGER NOT NULL REFERENCES ansible_playbooks(id) ON DELETE CASCADE,
		name TEXT NOT NULL DEFAULT '',
		cron TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		mode TEXT NOT NULL DEFAULT 'run',
		host_ids TEXT NOT NULL DEFAULT '[]',
		group_name TEXT NOT NULL DEFAULT '',
		extra_vars TEXT NOT NULL DEFAULT '',
		tags TEXT NOT NULL DEFAULT '',
		skip_tags TEXT NOT NULL DEFAULT '',
		verbosity INTEGER NOT NULL DEFAULT 0,
		become INTEGER NOT NULL DEFAULT 0,
		next_run_at DATETIME,
		last_run_at DATETIME,
		last_run_id INTEGER NOT NULL DEFAULT 0,
		last_error TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS ansible_adhoc_runs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		module TEXT NOT NULL,
		args TEXT NOT NULL DEFAULT '',
		pattern TEXT NOT NULL DEFAULT 'all',
		targets TEXT NOT NULL DEFAULT '[]',
		become INTEGER NOT NULL DEFAULT 0,
		verbosity INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'queued',
		exit_code INTEGER,
		results TEXT NOT NULL DEFAULT '[]',
		log TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		started_at DATETIME,
		finished_at DATETIME
	);
	CREATE INDEX IF NOT EXISTS idx_ansible_adhoc_runs_id ON ansible_adhoc_runs(id DESC);
	CREATE TABLE IF NOT EXISTS ansible_host_facts (
		host_id INTEGER PRIMARY KEY REFERENCES hosts(id) ON DELETE CASCADE,
		facts TEXT NOT NULL DEFAULT '{}',
		summary TEXT NOT NULL DEFAULT '{}',
		gathered_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS tf_workspaces (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE COLLATE NOCASE,
		description TEXT NOT NULL DEFAULT '',
		environment TEXT NOT NULL DEFAULT 'production',
		tags TEXT NOT NULL DEFAULT '[]',
		binary TEXT NOT NULL DEFAULT 'terraform',
		source TEXT NOT NULL DEFAULT 'inline',
		files TEXT NOT NULL DEFAULT '{}',
		git_url TEXT NOT NULL DEFAULT '',
		git_branch TEXT NOT NULL DEFAULT '',
		git_path TEXT NOT NULL DEFAULT '',
		git_auth TEXT NOT NULL DEFAULT 'none',
		git_token TEXT NOT NULL DEFAULT '',
		git_key_id INTEGER NOT NULL DEFAULT 0,
		webhook_token TEXT NOT NULL UNIQUE,
		webhook_secret TEXT NOT NULL DEFAULT '',
		drift_interval_minutes INTEGER NOT NULL DEFAULT 0,
		lock_reason TEXT NOT NULL DEFAULT '',
		locked_by TEXT NOT NULL DEFAULT '',
		locked_at DATETIME,
		drift_detected INTEGER NOT NULL DEFAULT 0,
		drift_checked_at DATETIME,
		tf_version TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS tf_cloud_accounts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE COLLATE NOCASE,
		provider TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		fields TEXT NOT NULL DEFAULT '{}',
		verified_at DATETIME,
		verify_ok INTEGER NOT NULL DEFAULT 0,
		verify_msg TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS tf_varsets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE COLLATE NOCASE,
		description TEXT NOT NULL DEFAULT '',
		global INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE IF NOT EXISTS tf_varset_workspaces (
		varset_id INTEGER NOT NULL REFERENCES tf_varsets(id) ON DELETE CASCADE,
		workspace_id INTEGER NOT NULL REFERENCES tf_workspaces(id) ON DELETE CASCADE,
		PRIMARY KEY (varset_id, workspace_id)
	);
	CREATE TABLE IF NOT EXISTS tf_variables (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		workspace_id INTEGER REFERENCES tf_workspaces(id) ON DELETE CASCADE,
		varset_id INTEGER REFERENCES tf_varsets(id) ON DELETE CASCADE,
		key TEXT NOT NULL,
		value TEXT NOT NULL DEFAULT '',
		category TEXT NOT NULL DEFAULT 'terraform',
		hcl INTEGER NOT NULL DEFAULT 0,
		sensitive INTEGER NOT NULL DEFAULT 0,
		description TEXT NOT NULL DEFAULT '',
		UNIQUE (workspace_id, varset_id, key, category)
	);
	CREATE TABLE IF NOT EXISTS tf_runs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		workspace_id INTEGER NOT NULL REFERENCES tf_workspaces(id) ON DELETE CASCADE,
		type TEXT NOT NULL DEFAULT 'plan',
		speculative INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'queued',
		trigger_kind TEXT NOT NULL DEFAULT 'manual',
		message TEXT NOT NULL DEFAULT '',
		targets TEXT NOT NULL DEFAULT '[]',
		replace TEXT NOT NULL DEFAULT '[]',
		commit_sha TEXT NOT NULL DEFAULT '',
		plan_summary TEXT NOT NULL DEFAULT '{}',
		drift TEXT NOT NULL DEFAULT '[]',
		policy TEXT NOT NULL DEFAULT '{}',
		cost TEXT NOT NULL DEFAULT '{}',
		has_changes INTEGER NOT NULL DEFAULT 0,
		exit_code INTEGER,
		log TEXT NOT NULL DEFAULT '',
		approved_by_comment TEXT NOT NULL DEFAULT '',
		decision TEXT NOT NULL DEFAULT '',
		decision_comment TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		started_at DATETIME,
		finished_at DATETIME
	);
	CREATE INDEX IF NOT EXISTS idx_tf_runs_ws_id ON tf_runs(workspace_id, id);
	CREATE TABLE IF NOT EXISTS tf_state_versions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		workspace_id INTEGER NOT NULL REFERENCES tf_workspaces(id) ON DELETE CASCADE,
		run_id INTEGER NOT NULL DEFAULT 0,
		serial INTEGER NOT NULL DEFAULT 0,
		lineage TEXT NOT NULL DEFAULT '',
		source TEXT NOT NULL DEFAULT 'apply',
		size INTEGER NOT NULL DEFAULT 0,
		resource_count INTEGER NOT NULL DEFAULT 0,
		sha256 TEXT NOT NULL DEFAULT '',
		state BLOB NOT NULL DEFAULT X'',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_tf_state_versions_ws ON tf_state_versions(workspace_id, id);
	CREATE TABLE IF NOT EXISTS tf_policies (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE COLLATE NOCASE,
		description TEXT NOT NULL DEFAULT '',
		kind TEXT NOT NULL DEFAULT '',
		params TEXT NOT NULL DEFAULT '{}',
		enforcement TEXT NOT NULL DEFAULT 'soft',
		enabled INTEGER NOT NULL DEFAULT 1,
		workspace_id INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE IF NOT EXISTS tf_modules (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		provider TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		UNIQUE (name, provider)
	);
	CREATE TABLE IF NOT EXISTS tf_module_versions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		module_id INTEGER NOT NULL REFERENCES tf_modules(id) ON DELETE CASCADE,
		version TEXT NOT NULL,
		files TEXT NOT NULL DEFAULT '{}',
		doc TEXT NOT NULL DEFAULT '{}',
		source_url TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE (module_id, version)
	);
	CREATE TABLE IF NOT EXISTS tf_output_links (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		workspace_id INTEGER NOT NULL,
		var_name TEXT NOT NULL,
		source_workspace_id INTEGER NOT NULL,
		output_name TEXT NOT NULL,
		UNIQUE (workspace_id, var_name)
	);
	CREATE TABLE IF NOT EXISTS tf_audit (id INTEGER PRIMARY KEY AUTOINCREMENT, ts DATETIME NOT NULL, level TEXT NOT NULL, host TEXT NOT NULL DEFAULT '', message TEXT NOT NULL);
	`
	_, err := d.Exec(schema)
	if err != nil {
		return err
	}

	// Idempotent column additions for existing databases
	for col, typ := range map[string]string{
		"source": "TEXT NOT NULL DEFAULT 'inline'", "git_url": "TEXT NOT NULL DEFAULT ''", "git_branch": "TEXT NOT NULL DEFAULT ''",
		"git_path": "TEXT NOT NULL DEFAULT ''", "git_auth": "TEXT NOT NULL DEFAULT 'none'", "git_token": "TEXT NOT NULL DEFAULT ''",
		"git_key_id":       "INTEGER NOT NULL DEFAULT 0",
		"survey":           "TEXT NOT NULL DEFAULT ''",
		"last_sync_at":     "DATETIME",
		"last_sync_commit": "TEXT NOT NULL DEFAULT ''",
		"group_name":       "TEXT NOT NULL DEFAULT ''",
	} {
		_ = d.alterAddColumn("ansible_playbooks", col, typ)
	}
	_ = d.alterAddColumn("tf_workspaces", "group_name", "TEXT NOT NULL DEFAULT ''")
	_ = d.alterAddColumn("tf_workspaces", "cloud_account_ids", "TEXT NOT NULL DEFAULT '[]'")
	_ = d.alterAddColumn("tf_workspaces", "plan_interval_minutes", "INTEGER NOT NULL DEFAULT 0")
	_ = d.alterAddColumn("tf_workspaces", "auto_apply", "INTEGER NOT NULL DEFAULT 0")
	if err := d.migrateAnsibleBundles(); err != nil {
		return fmt.Errorf("migrate ansible bundles: %w", err)
	}
	_ = d.alterAddColumn("ansible_runs", "trigger_kind", "TEXT NOT NULL DEFAULT 'manual'")
	_ = d.alterAddColumn("ansible_runs", "schedule_id", "INTEGER NOT NULL DEFAULT 0")
	_ = d.alterAddColumn("ansible_runs", "commit_sha", "TEXT NOT NULL DEFAULT ''")
	_ = d.alterAddColumn("ansible_runs", "limit_pattern", "TEXT NOT NULL DEFAULT ''")
	_ = d.alterAddColumn("ansible_runs", "recap", "TEXT NOT NULL DEFAULT ''")
	_ = d.alterAddColumn("ansible_schedules", "limit_pattern", "TEXT NOT NULL DEFAULT ''")
	_ = d.alterAddColumn("hosts", "net_rx_bytes", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "net_tx_bytes", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "net_rx_speed_bps", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "net_tx_speed_bps", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "disk_read_bytes", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "disk_write_bytes", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "disk_read_speed_bps", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "disk_write_speed_bps", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "internet_online", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "internet_latency_ms", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "public_ip", "TEXT DEFAULT ''")
	_ = d.alterAddColumn("hosts", "active_conn_count", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "failed_logins_count", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "top_connections", "TEXT DEFAULT ''")
	_ = d.alterAddColumn("hosts", "listening_ports", "TEXT DEFAULT ''")
	_ = d.alterAddColumn("hosts", "notes", "TEXT DEFAULT ''")
	_ = d.alterAddColumn("hosts", "sort_order", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "group_name", "TEXT DEFAULT ''")
	_ = d.alterAddColumn("hosts", "lifecycle_breakdown", "TEXT DEFAULT '[]'")
	_ = d.alterAddColumn("hosts", "cpu_cores", "INTEGER DEFAULT 1")
	_ = d.alterAddColumn("hosts", "hardware_model", "TEXT DEFAULT ''")
	_ = d.alterAddColumn("hosts", "swap_used_bytes", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "swap_total_bytes", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "last_duration_ms", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "commission_date", "TEXT DEFAULT ''")
	_ = d.alterAddColumn("hosts", "bios_date", "TEXT DEFAULT ''")
	_ = d.alterAddColumn("hosts", "os_install_epoch", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "top_cpu_processes", "TEXT DEFAULT ''")
	_ = d.alterAddColumn("hosts", "top_ram_processes", "TEXT DEFAULT ''")
	_ = d.alterAddColumn("hosts", "top_swap_processes", "TEXT DEFAULT ''")
	_ = d.alterAddColumn("hosts", "key_id", "INTEGER DEFAULT 0")
	_ = d.alterAddColumn("hosts", "kube_cluster_id", "TEXT DEFAULT ''")
	_ = d.alterAddColumn("hosts", "kube_mode", "TEXT NOT NULL DEFAULT 'full'")

	return nil
}

func (d *DB) alterAddColumn(table, column, colType string) error {
	_, err := d.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, colType))
	return err
}

func (d *DB) initDefaults() error {
	defer d.settingsCache.Clear()
	// 1. Ensure default admin password exists (default: "admin")
	var hash string
	err := d.QueryRow("SELECT value FROM settings WHERE key = 'admin_password_hash'").Scan(&hash)
	if err == sql.ErrNoRows {
		defaultHash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		if _, err := d.Exec("INSERT INTO settings(key, value) VALUES ('admin_password_hash', ?)", string(defaultHash)); err != nil {
			return err
		}
	}

	// 2. Ensure global universal RSA 4096-bit SSH keypair exists
	// ponytail: RSA 4096 is universally accepted by legacy OpenSSH 5.3p1 (CentOS 6) through modern OpenSSH 9+.
	var privKey string
	err = d.QueryRow("SELECT value FROM settings WHERE key = 'ssh_private_key'").Scan(&privKey)
	if err == sql.ErrNoRows {
		privPEM, pubAuthorized, err := GenerateRSAKeyPair(4096)
		if err != nil {
			return fmt.Errorf("generate default universal rsa key: %w", err)
		}

		tx, err := d.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		if _, err := tx.Exec("INSERT INTO settings(key, value) VALUES ('ssh_private_key', ?)", privPEM); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO settings(key, value) VALUES ('ssh_public_key', ?)", pubAuthorized); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	} else if err == nil {
		var pubKey string
		_ = d.QueryRow("SELECT value FROM settings WHERE key = 'ssh_public_key'").Scan(&pubKey)
		// If existing database was initialized with Ed25519 only, upgrade to universal RSA-4096 as primary
		// while retaining the Ed25519 private key in the PEM chain so existing modern hosts don't break.
		if strings.HasPrefix(strings.TrimSpace(pubKey), "ssh-ed25519") {
			privPEM, pubAuthorized, err := GenerateRSAKeyPair(4096)
			if err == nil {
				combinedPriv := strings.TrimSpace(privPEM) + "\n\n" + strings.TrimSpace(privKey) + "\n"
				tx, err := d.Begin()
				if err == nil {
					_, _ = tx.Exec("UPDATE settings SET value = ? WHERE key = 'ssh_private_key'", combinedPriv)
					_, _ = tx.Exec("UPDATE settings SET value = ? WHERE key = 'ssh_public_key'", pubAuthorized)
					_, _ = tx.Exec("INSERT INTO settings(key, value) VALUES ('ssh_private_key_ed25519', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", privKey)
					_, _ = tx.Exec("INSERT INTO settings(key, value) VALUES ('ssh_public_key_ed25519', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", pubKey)
					_ = tx.Commit()
				}
			}
		}
	}

	// 3. Ensure essential default global terminal presets exist
	var presetCount int
	_ = d.QueryRow("SELECT COUNT(*) FROM terminal_presets").Scan(&presetCount)
	if presetCount == 0 {
		defaultPresets := []struct {
			name    string
			command string
			sort    int
		}{
			{"htop", "htop", 1},
			{"docker stats", "docker stats", 2},
			{"journalctl -f", "journalctl -n 100 -f", 3},
		}
		for _, p := range defaultPresets {
			_, _ = d.Exec("INSERT INTO terminal_presets(name, command, sort_order, use_tmux) VALUES (?, ?, ?, 0)", p.name, p.command, p.sort)
		}
	}

	return nil
}

// GenerateRSAKeyPair creates a new PEM-encoded RSA private key and OpenSSH-formatted authorized public key.
func GenerateRSAKeyPair(bits int) (privPEM, pubAuthorized string, err error) {
	if bits <= 0 {
		bits = 4096
	}
	priv, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return "", "", fmt.Errorf("generate rsa key: %w", err)
	}

	privBytes := x509.MarshalPKCS1PrivateKey(priv)
	pemBlock := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privBytes,
	})

	sshPub, err := ssh.NewPublicKey(&priv.PublicKey)
	if err != nil {
		return "", "", fmt.Errorf("new ssh pubkey: %w", err)
	}
	pubAuth := string(ssh.MarshalAuthorizedKey(sshPub))
	return string(pemBlock), pubAuth, nil
}

// RegenerateGlobalSSHKey generates a new universal RSA-4096 keypair and saves it to settings.
func (d *DB) RegenerateGlobalSSHKey() (string, error) {
	defer d.settingsCache.Clear()
	privPEM, pubAuthorized, err := GenerateRSAKeyPair(4096)
	if err != nil {
		return "", err
	}
	tx, err := d.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("INSERT INTO settings(key, value) VALUES ('ssh_private_key', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", privPEM); err != nil {
		return "", err
	}
	if _, err := tx.Exec("INSERT INTO settings(key, value) VALUES ('ssh_public_key', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", pubAuthorized); err != nil {
		return "", err
	}
	return pubAuthorized, tx.Commit()
}

// Settings helpers
func (d *DB) GetSetting(key string) (string, error) {
	if v, ok := d.settingsCache.Load(key); ok {
		return v.(string), nil
	}
	var val string
	err := d.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err == nil {
		d.settingsCache.Store(key, val)
	}
	return val, err
}

func (d *DB) SetSetting(key, val string) error {
	defer d.settingsCache.Delete(key)
	_, err := d.Exec("INSERT INTO settings(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", key, val)
	return err
}

func (d *DB) DeleteSetting(key string) error {
	defer d.settingsCache.Delete(key)
	_, err := d.Exec("DELETE FROM settings WHERE key = ?", key)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

const hostSelectCols = `SELECT id, name, host, port, user, COALESCE(custom_key,''), COALESCE(key_id, 0), status, os_info, kernel, uptime, cpu_load, ram_used_bytes, ram_total_bytes, disk_used_bytes, disk_total_bytes, lifecycle_score, lifecycle_notes, COALESCE(lifecycle_breakdown, '[]'), last_inspected, created_at,
	COALESCE(net_rx_bytes, 0), COALESCE(net_tx_bytes, 0), COALESCE(net_rx_speed_bps, 0), COALESCE(net_tx_speed_bps, 0),
	COALESCE(disk_read_bytes, 0), COALESCE(disk_write_bytes, 0), COALESCE(disk_read_speed_bps, 0), COALESCE(disk_write_speed_bps, 0),
	COALESCE(internet_online, 0), COALESCE(internet_latency_ms, 0), COALESCE(public_ip, ''), COALESCE(active_conn_count, 0),
	COALESCE(failed_logins_count, 0), COALESCE(top_connections, ''), COALESCE(listening_ports, ''),
	COALESCE(notes, ''), COALESCE(sort_order, 0), COALESCE(group_name, ''),
	COALESCE(cpu_cores, 1), COALESCE(hardware_model, ''), COALESCE(swap_used_bytes, 0), COALESCE(swap_total_bytes, 0),
	COALESCE(last_duration_ms, 0), COALESCE(commission_date, ''), COALESCE(bios_date, ''), COALESCE(os_install_epoch, 0),
	COALESCE(top_cpu_processes, ''), COALESCE(top_ram_processes, ''), COALESCE(top_swap_processes, ''),
	COALESCE(kube_cluster_id, ''), COALESCE(kube_mode, 'full')
	FROM hosts `

func scanHost(s rowScanner) (*Host, error) {
	var h Host
	var inspected sql.NullTime
	var online int
	err := s.Scan(&h.ID, &h.Name, &h.Host, &h.Port, &h.User, &h.CustomKey, &h.KeyID, &h.Status, &h.OSInfo, &h.Kernel, &h.Uptime, &h.CPULoad, &h.RAMUsedBytes, &h.RAMTotalBytes, &h.DiskUsedBytes, &h.DiskTotalBytes, &h.LifecycleScore, &h.LifecycleNotes, &h.LifecycleBreakdown, &inspected, &h.CreatedAt,
		&h.NetRxBytes, &h.NetTxBytes, &h.NetRxSpeedBps, &h.NetTxSpeedBps,
		&h.DiskReadBytes, &h.DiskWriteBytes, &h.DiskReadSpeedBps, &h.DiskWriteSpeedBps,
		&online, &h.InternetLatencyMs, &h.PublicIP, &h.ActiveConnCount,
		&h.FailedLoginsCount, &h.TopConnections, &h.ListeningPorts,
		&h.Notes, &h.SortOrder, &h.GroupName,
		&h.CPUCores, &h.HardwareModel, &h.SwapUsedBytes, &h.SwapTotalBytes,
		&h.LastDurationMs, &h.CommissionDate, &h.BIOSDate, &h.OSInstallEpoch,
		&h.TopCPUProcesses, &h.TopRAMProcesses, &h.TopSwapProcesses,
		&h.KubeClusterID, &h.KubeMode)
	if err != nil {
		return nil, err
	}
	h.InternetOnline = online == 1
	if inspected.Valid {
		h.LastInspected = &inspected.Time
	}
	if h.KubeMode == "" {
		h.KubeMode = "full"
	}
	h.HasCustomKey = strings.TrimSpace(h.CustomKey) != ""
	return &h, nil
}

// Host operations
func (d *DB) ListHosts() ([]Host, error) {
	rows, err := d.Query(hostSelectCols + `ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Host
	for rows.Next() {
		h, err := scanHost(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *h)
	}
	return list, nil
}

// CountHosts avoids loading every host row when only the total is needed.
func (d *DB) CountHosts() (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM hosts`).Scan(&n)
	return n, err
}

func (d *DB) GetHost(id int64) (*Host, error) {
	h, err := scanHost(d.QueryRow(hostSelectCols+`WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return h, nil
}

func (d *DB) CreateHost(h *Host) (int64, error) {
	if h.KeyID > 0 {
		h.CustomKey = ""
	}
	var maxOrder int
	_ = d.QueryRow(`SELECT COALESCE(MAX(sort_order), 0) FROM hosts`).Scan(&maxOrder)
	if h.KubeMode == "" {
		h.KubeMode = "full"
	}
	res, err := d.Exec(`INSERT INTO hosts (name, host, port, user, custom_key, key_id, status, notes, sort_order, group_name, commission_date, bios_date, kube_cluster_id, kube_mode) VALUES (?, ?, ?, ?, ?, ?, 'unknown', ?, ?, ?, ?, ?, ?, ?)`, h.Name, h.Host, h.Port, h.User, h.CustomKey, h.KeyID, h.Notes, maxOrder+1, h.GroupName, h.CommissionDate, h.BIOSDate, h.KubeClusterID, h.KubeMode)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) UpdateHost(h *Host) error {
	if h.KeyID > 0 {
		h.CustomKey = ""
	}
	if h.KubeMode == "" {
		h.KubeMode = "full"
	}
	_, err := d.Exec(`UPDATE hosts SET name=?, host=?, port=?, user=?, custom_key=?, key_id=?, notes=?, group_name=?, commission_date=?, kube_mode=? WHERE id=?`, h.Name, h.Host, h.Port, h.User, h.CustomKey, h.KeyID, h.Notes, h.GroupName, h.CommissionDate, h.KubeMode, h.ID)
	return err
}

func (d *DB) SetHostKubeClusterID(id int64, clusterID string) error {
	_, err := d.Exec(`UPDATE hosts SET kube_cluster_id=? WHERE id=?`, clusterID, id)
	return err
}
func (d *DB) UpdateHostNotes(id int64, notes string) error {
	_, err := d.Exec(`UPDATE hosts SET notes=? WHERE id=?`, notes, id)
	return err
}

func (d *DB) UpdateHostLifecycle(id int64, score int, notes string, breakdown string) error {
	_, err := d.Exec(`UPDATE hosts SET lifecycle_score=?, lifecycle_notes=?, lifecycle_breakdown=? WHERE id=?`, score, notes, breakdown, id)
	return err
}

func (d *DB) ReorderHosts(orderedIDs []int64) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`UPDATE hosts SET sort_order=? WHERE id=?`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i, id := range orderedIDs {
		if _, err := stmt.Exec(i+1, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// HostGroupAssignment sets the group of one host. An empty GroupName clears it.
type HostGroupAssignment struct {
	ID        int64  `json:"id"`
	GroupName string `json:"group_name"`
}

// UpdateHostGroups applies all assignments atomically: if any host id does not
// exist nothing is changed.
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
		res, err := stmt.Exec(strings.TrimSpace(a.GroupName), a.ID)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err == nil && n == 0 {
			return fmt.Errorf("host %d: %w", a.ID, ErrHostNotFound)
		}
	}
	return tx.Commit()
}

func (d *DB) GetGlobalNotes() (string, error) {
	return d.GetSetting("global_notes")
}

func (d *DB) SetGlobalNotes(notes string) error {
	return d.SetSetting("global_notes", notes)
}

func (d *DB) DeleteHost(id int64) error {
	_, err := d.Exec(`DELETE FROM hosts WHERE id=?`, id)
	return err
}

func (d *DB) UpdateHostInspection(h *Host) error {
	now := time.Now()
	onlineInt := 0
	if h.InternetOnline {
		onlineInt = 1
	}
	_, err := d.Exec(`UPDATE hosts SET status=?, os_info=?, kernel=?, uptime=?, cpu_load=?, ram_used_bytes=?, ram_total_bytes=?, disk_used_bytes=?, disk_total_bytes=?, lifecycle_score=?, lifecycle_notes=?, lifecycle_breakdown=?, last_inspected=?, net_rx_bytes=?, net_tx_bytes=?, net_rx_speed_bps=?, net_tx_speed_bps=?, disk_read_bytes=?, disk_write_bytes=?, disk_read_speed_bps=?, disk_write_speed_bps=?, internet_online=?, internet_latency_ms=?, public_ip=?, active_conn_count=?, failed_logins_count=?, top_connections=?, listening_ports=?, cpu_cores=?, hardware_model=?, swap_used_bytes=?, swap_total_bytes=?, last_duration_ms=?, bios_date=?, os_install_epoch=?, top_cpu_processes=?, top_ram_processes=?, top_swap_processes=? WHERE id=?`,
		h.Status, h.OSInfo, h.Kernel, h.Uptime, h.CPULoad, h.RAMUsedBytes, h.RAMTotalBytes, h.DiskUsedBytes, h.DiskTotalBytes, h.LifecycleScore, h.LifecycleNotes, h.LifecycleBreakdown, now,
		h.NetRxBytes, h.NetTxBytes, h.NetRxSpeedBps, h.NetTxSpeedBps,
		h.DiskReadBytes, h.DiskWriteBytes, h.DiskReadSpeedBps, h.DiskWriteSpeedBps,
		onlineInt, h.InternetLatencyMs, h.PublicIP, h.ActiveConnCount, h.FailedLoginsCount, h.TopConnections, h.ListeningPorts,
		h.CPUCores, h.HardwareModel, h.SwapUsedBytes, h.SwapTotalBytes, h.LastDurationMs, h.BIOSDate, h.OSInstallEpoch,
		h.TopCPUProcesses, h.TopRAMProcesses, h.TopSwapProcesses,
		h.ID)
	return err
}

// Host Package operations
// ponytail: Atomic host-scoped replace eliminates orphan/uninstalled packages cleanly in one transaction.
// ponytail: Guard against accidental database wipe if scan returned empty or timed out.
func (d *DB) ReplaceHostPackages(hostID int64, pkgs []*HostPackage) error {
	if len(pkgs) == 0 {
		return fmt.Errorf("refusing to replace with empty packages list; keeping existing scan results")
	}

	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM host_packages WHERE host_id = ?`, hostID); err != nil {
		return err
	}

	stmt, err := tx.Prepare(`INSERT INTO host_packages (host_id, name, version, size_bytes, install_epoch, is_manual, service_name, service_status, last_scanned_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now()
	for _, p := range pkgs {
		isManualInt := 0
		if p.IsManual {
			isManualInt = 1
		}
		scannedAt := p.LastScannedAt
		if scannedAt.IsZero() {
			scannedAt = now
		}
		if _, err := stmt.Exec(hostID, p.Name, p.Version, p.SizeBytes, p.InstallEpoch, isManualInt, p.ServiceName, p.ServiceStatus, scannedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *DB) GetHostPackages(hostID int64) ([]*HostPackage, error) {
	rows, err := d.Query(`SELECT id, host_id, name, version, size_bytes, install_epoch, is_manual, service_name, service_status, last_scanned_at FROM host_packages WHERE host_id = ? ORDER BY is_manual DESC, size_bytes DESC, name ASC`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pkgs []*HostPackage
	for rows.Next() {
		var p HostPackage
		var isManualInt int
		if err := rows.Scan(&p.ID, &p.HostID, &p.Name, &p.Version, &p.SizeBytes, &p.InstallEpoch, &isManualInt, &p.ServiceName, &p.ServiceStatus, &p.LastScannedAt); err != nil {
			return nil, err
		}
		p.IsManual = isManualInt == 1
		pkgs = append(pkgs, &p)
	}
	return pkgs, nil
}

// GetHostPackagesLastScanned returns the most recent last_scanned_at timestamp for a host's packages.
func (d *DB) GetHostPackagesLastScanned(hostID int64) (time.Time, error) {
	var ts time.Time
	err := d.QueryRow(`SELECT last_scanned_at FROM host_packages WHERE host_id = ? ORDER BY last_scanned_at DESC LIMIT 1`, hostID).Scan(&ts)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	return ts, nil
}

// Inspection Run operations
func (d *DB) RecordInspectionRun(run *InspectionRun) error {
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now()
	}
	res, err := d.Exec(`INSERT INTO inspection_runs(host_id, started_at, duration_ms, status, summary, details) VALUES (?, ?, ?, ?, ?, ?)`,
		run.HostID, run.StartedAt, run.DurationMs, run.Status, run.Summary, run.Details)
	if err != nil {
		return err
	}
	run.ID, _ = res.LastInsertId()

	// ponytail: Dual-bucket auto-pruning preserves up to 100 latest OK runs and 100 latest issue runs (drift/error/down/degraded) per host without background schedulers.
	if run.Status == "ok" {
		_, _ = d.Exec(`DELETE FROM inspection_runs WHERE host_id = ? AND status = 'ok' AND id NOT IN (SELECT id FROM inspection_runs WHERE host_id = ? AND status = 'ok' ORDER BY id DESC LIMIT 100)`, run.HostID, run.HostID)
	} else {
		_, _ = d.Exec(`DELETE FROM inspection_runs WHERE host_id = ? AND status != 'ok' AND id NOT IN (SELECT id FROM inspection_runs WHERE host_id = ? AND status != 'ok' ORDER BY id DESC LIMIT 100)`, run.HostID, run.HostID)
	}
	return nil
}

func (d *DB) ListInspectionRuns(hostID int64, limit int, filter ...string) ([]InspectionRun, error) {
	if limit <= 0 {
		limit = 50
	}
	var query string
	var args []interface{}
	if len(filter) > 0 && filter[0] == "issues" {
		query = `SELECT id, host_id, started_at, duration_ms, status, summary, details, created_at FROM inspection_runs WHERE host_id = ? AND status != 'ok' ORDER BY id DESC LIMIT ?`
		args = []interface{}{hostID, limit}
	} else {
		query = `SELECT id, host_id, started_at, duration_ms, status, summary, details, created_at FROM inspection_runs WHERE host_id = ? ORDER BY id DESC LIMIT ?`
		args = []interface{}{hostID, limit}
	}
	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []InspectionRun
	for rows.Next() {
		var r InspectionRun
		if err := rows.Scan(&r.ID, &r.HostID, &r.StartedAt, &r.DurationMs, &r.Status, &r.Summary, &r.Details, &r.CreatedAt); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, nil
}

// Desired State operations
func (d *DB) ListDesiredRules(hostID int64) ([]DesiredRule, error) {
	rows, err := d.Query(`SELECT id, host_id, kind, target, expected, enabled FROM desired_rules WHERE host_id = ? ORDER BY kind, target`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []DesiredRule
	for rows.Next() {
		var r DesiredRule
		var enabled int
		if err := rows.Scan(&r.ID, &r.HostID, &r.Kind, &r.Target, &r.Expected, &enabled); err != nil {
			return nil, err
		}
		r.Enabled = enabled == 1
		rules = append(rules, r)
	}
	return rules, nil
}

func (d *DB) SaveDesiredRule(r *DesiredRule) error {
	enabled := 0
	if r.Enabled {
		enabled = 1
	}
	_, err := d.Exec(`INSERT INTO desired_rules(host_id, kind, target, expected, enabled) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(host_id, kind, target) DO UPDATE SET expected=excluded.expected, enabled=excluded.enabled`,
		r.HostID, r.Kind, r.Target, r.Expected, enabled)
	if err != nil {
		return err
	}
	return d.PruneActualItems(r.HostID) // disabling a rule must clear its Drift too
}

func (d *DB) DeleteDesiredRule(id int64) error {
	var hostID int64
	if err := d.QueryRow(`SELECT host_id FROM desired_rules WHERE id = ?`, id).Scan(&hostID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if _, err := d.Exec(`DELETE FROM desired_rules WHERE id = ?`, id); err != nil {
		return err
	}
	return d.PruneActualItems(hostID)
}

// PruneActualItems drops actual items that no enabled rule owns any more (rule deleted or disabled). Without it a
// removed rule keeps showing as Drift because the Host Detail banner reads actual_items. When no Drift remains
// on a degraded Host it is marked healthy and its alerts are closed, the same outcome as an inspection that
// finds no Drift, instead of waiting for the next one (which the inspection cooldown can delay).
func (d *DB) PruneActualItems(hostID int64) error {
	if _, err := d.Exec(`DELETE FROM actual_items WHERE host_id = ? AND NOT EXISTS (
		SELECT 1 FROM desired_rules r WHERE r.host_id = actual_items.host_id AND r.kind = actual_items.kind
		AND r.target = actual_items.target AND r.enabled = 1)`, hostID); err != nil {
		return err
	}
	var drift int
	if err := d.QueryRow(`SELECT COUNT(*) FROM actual_items WHERE host_id = ? AND status = 'drift'`, hostID).Scan(&drift); err != nil || drift > 0 {
		return err
	}
	res, err := d.Exec(`UPDATE hosts SET status = 'healthy' WHERE id = ? AND status = 'degraded'`, hostID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return d.AcknowledgeHostAlerts(hostID)
	}
	return nil
}

// Actual items operations
func (d *DB) ListActualItems(hostID int64) ([]ActualItem, error) {
	rows, err := d.Query(`SELECT id, host_id, kind, target, current, status, details, updated_at FROM actual_items WHERE host_id = ? ORDER BY kind, target`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []ActualItem
	for rows.Next() {
		var it ActualItem
		if err := rows.Scan(&it.ID, &it.HostID, &it.Kind, &it.Target, &it.Current, &it.Status, &it.Details, &it.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, nil
}

func (d *DB) UpsertActualItem(it *ActualItem) error {
	_, err := d.Exec(`INSERT INTO actual_items(host_id, kind, target, current, status, details, updated_at) VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(host_id, kind, target) DO UPDATE SET current=excluded.current, status=excluded.status, details=excluded.details, updated_at=CURRENT_TIMESTAMP`,
		it.HostID, it.Kind, it.Target, it.Current, it.Status, it.Details)
	return err
}

// Incident operations
func (d *DB) RecordIncident(inc *Incident) error {
	_, err := d.Exec(`INSERT INTO incidents(host_id, kind, target, event_type, summary, root_cause_excerpt) VALUES (?, ?, ?, ?, ?, ?)`,
		inc.HostID, inc.Kind, inc.Target, inc.EventType, inc.Summary, inc.RootCauseExcerpt)
	if err != nil {
		return err
	}
	if inc.HostID != 0 { // ponytail: keep latest 500 incidents per host
		_, _ = d.Exec(`DELETE FROM incidents WHERE host_id = ? AND id NOT IN (SELECT id FROM incidents WHERE host_id = ? ORDER BY id DESC LIMIT 500)`, inc.HostID, inc.HostID)
	}
	return nil
}

func (d *DB) ListIncidents(hostID int64, limit int) ([]Incident, error) {
	query := `SELECT id, host_id, kind, target, event_type, summary, root_cause_excerpt, created_at FROM incidents WHERE host_id = ? ORDER BY id DESC LIMIT ?`
	rows, err := d.Query(query, hostID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var incs []Incident
	for rows.Next() {
		var inc Incident
		if err := rows.Scan(&inc.ID, &inc.HostID, &inc.Kind, &inc.Target, &inc.EventType, &inc.Summary, &inc.RootCauseExcerpt, &inc.CreatedAt); err != nil {
			return nil, err
		}
		incs = append(incs, inc)
	}
	return incs, nil
}

// Alert operations
func (d *DB) CreateAlert(alt *Alert) (int64, error) {
	res, err := d.Exec(`INSERT INTO alerts(host_id, level, message, root_cause) VALUES (?, ?, ?, ?)`,
		alt.HostID, alt.Level, alt.Message, alt.RootCause)
	if err != nil {
		return 0, err
	}
	if alt.HostID != 0 { // ponytail: keep latest 500 acknowledged alerts per host; unacknowledged never pruned
		_, _ = d.Exec(`DELETE FROM alerts WHERE host_id = ? AND acknowledged = 1 AND id NOT IN (SELECT id FROM alerts WHERE host_id = ? AND acknowledged = 1 ORDER BY id DESC LIMIT 500)`, alt.HostID, alt.HostID)
	}
	return res.LastInsertId()
}

func (d *DB) ListActiveAlerts() ([]Alert, error) {
	rows, err := d.Query(`SELECT a.id, COALESCE(a.host_id,0), COALESCE(h.name,'System'), a.level, a.message, a.root_cause, a.acknowledged, a.created_at
		FROM alerts a LEFT JOIN hosts h ON a.host_id = h.id WHERE a.acknowledged = 0 ORDER BY a.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Alert
	for rows.Next() {
		var a Alert
		var ack int
		if err := rows.Scan(&a.ID, &a.HostID, &a.HostName, &a.Level, &a.Message, &a.RootCause, &ack, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Acknowledged = ack == 1
		list = append(list, a)
	}
	return list, nil
}

func (d *DB) AcknowledgeAlert(id int64) error {
	_, err := d.Exec(`UPDATE alerts SET acknowledged = 1 WHERE id = ?`, id)
	return err
}

func (d *DB) AcknowledgeAllAlerts() error {
	_, err := d.Exec(`UPDATE alerts SET acknowledged = 1 WHERE acknowledged = 0`)
	return err
}

func (d *DB) AcknowledgeHostAlerts(hostID int64) error {
	_, err := d.Exec(`UPDATE alerts SET acknowledged = 1 WHERE host_id = ? AND acknowledged = 0`, hostID)
	return err
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
	SSHKeys                []SnapshotSSHKey            `json:"ssh_keys,omitempty"`
	KubeClusters           []SnapshotKubeCluster       `json:"kube_clusters,omitempty"`
	CloudflareAccounts     []SnapshotCloudflareAccount `json:"cloudflare_accounts,omitempty"`
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

	var allRules []DesiredRule
	for _, h := range hosts {
		rules, err := d.ListDesiredRules(h.ID)
		if err != nil {
			return nil, fmt.Errorf("list rules for host %d: %w", h.ID, err)
		}
		allRules = append(allRules, rules...)
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

	var allPresets []TerminalPreset
	pRows, err := d.Query("SELECT id, host_id, name, command, sort_order, use_tmux, created_at FROM terminal_presets ORDER BY sort_order ASC, id ASC")
	if err == nil {
		defer pRows.Close()
		for pRows.Next() {
			var p TerminalPreset
			var rawHostID sql.NullInt64
			var useTmux int
			if err := pRows.Scan(&p.ID, &rawHostID, &p.Name, &p.Command, &p.SortOrder, &useTmux, &p.CreatedAt); err == nil {
				if rawHostID.Valid {
					hid := rawHostID.Int64
					p.HostID = &hid
				}
				p.UseTmux = useTmux == 1
				allPresets = append(allPresets, p)
			}
		}
	}

	var allSSHKeys []SnapshotSSHKey
	kRows, err := d.Query("SELECT id, name, private_key, public_key, fingerprint, created_at FROM ssh_keys ORDER BY id ASC")
	if err == nil {
		defer kRows.Close()
		for kRows.Next() {
			var k SnapshotSSHKey
			if err := kRows.Scan(&k.ID, &k.Name, &k.PrivateKey, &k.PublicKey, &k.Fingerprint, &k.CreatedAt); err == nil {
				allSSHKeys = append(allSSHKeys, k)
			}
		}
	}
	var allClusters []SnapshotKubeCluster
	cRows, err := d.Query("SELECT id, name, context, server, provider, kube_mode, kube_cluster_id, kubeconfig, created_at FROM kube_clusters ORDER BY id ASC")
	if err == nil {
		defer cRows.Close()
		for cRows.Next() {
			var c SnapshotKubeCluster
			var clusterUID sql.NullString
			if err := cRows.Scan(&c.ID, &c.Name, &c.Context, &c.Server, &c.Provider, &c.KubeMode, &clusterUID, &c.Kubeconfig, &c.CreatedAt); err == nil {
				if clusterUID.Valid {
					c.KubeClusterID = clusterUID.String
				}
				allClusters = append(allClusters, c)
			}
		}
	}
	var allCFAccounts []SnapshotCloudflareAccount
	cfRows, err := d.Query("SELECT id, name, account_id, api_token, created_at FROM cloudflare_accounts ORDER BY id ASC")
	if err == nil {
		defer cfRows.Close()
		for cfRows.Next() {
			var a SnapshotCloudflareAccount
			if err := cfRows.Scan(&a.ID, &a.Name, &a.AccountID, &a.APIToken, &a.CreatedAt); err == nil {
				allCFAccounts = append(allCFAccounts, a)
			}
		}
	}
	var allPlaybooks []SnapshotAnsiblePlaybook
	pbRows, err := d.Query("SELECT id, name, description, content, extra_vars, source, git_url, git_branch, git_path, git_auth, git_token, git_key_id, survey, last_sync_at, last_sync_commit, group_name, created_at, updated_at FROM ansible_playbooks ORDER BY id ASC")
	if err == nil {
		defer pbRows.Close()
		for pbRows.Next() {
			var p SnapshotAnsiblePlaybook
			if err := pbRows.Scan(&p.ID, &p.Name, &p.Description, &p.Content, &p.ExtraVars, &p.Source, &p.GitURL, &p.GitBranch, &p.GitPath, &p.GitAuth, &p.GitToken, &p.GitKeyID, &p.Survey, &p.LastSyncAt, &p.LastSyncCommit, &p.GroupName, &p.CreatedAt, &p.UpdatedAt); err == nil {
				allPlaybooks = append(allPlaybooks, p)
			}
		}
	}
	allSchedules, _ := d.ListAnsibleSchedules(0)
	var allAssets []SnapshotAnsibleAsset
	if assets, err := d.LoadAnsibleAssets(); err == nil {
		for _, a := range assets {
			allAssets = append(allAssets, SnapshotAnsibleAsset{Kind: a.Kind, Name: a.Name, Data: a.Data, UpdatedAt: a.UpdatedAt})
		}
	}

	var allTFWorkspaces []SnapshotTFWorkspace
	wsRows, err := d.Query(`SELECT id, name, description, environment, tags, binary, source, files, git_url, git_branch, git_path, git_auth, git_token, git_key_id, webhook_token, webhook_secret, drift_interval_minutes, plan_interval_minutes, auto_apply, group_name, cloud_account_ids, created_at, updated_at FROM tf_workspaces ORDER BY id ASC`)
	if err == nil {
		defer wsRows.Close()
		for wsRows.Next() {
			var w SnapshotTFWorkspace
			var cloudIDs string
			var autoApply int
			if err := wsRows.Scan(&w.ID, &w.Name, &w.Description, &w.Environment, &w.Tags, &w.Binary, &w.Source, &w.Files, &w.GitURL, &w.GitBranch, &w.GitPath, &w.GitAuth, &w.GitToken, &w.GitKeyID, &w.WebhookToken, &w.WebhookSecret, &w.DriftIntervalMinutes, &w.PlanIntervalMinutes, &autoApply, &w.GroupName, &cloudIDs, &w.CreatedAt, &w.UpdatedAt); err == nil {
				w.AutoApply = autoApply != 0
				w.CloudAccountIDs = unmarshalCloudAccountIDs(cloudIDs)
				allTFWorkspaces = append(allTFWorkspaces, w)
			}
		}
	}
	allTFLinks, _ := d.ListAllTFOutputLinks()
	var allTFCloudAccounts []SnapshotTFCloudAccount
	if accts, err := d.ListTFCloudAccounts(); err == nil {
		for _, a := range accts {
			allTFCloudAccounts = append(allTFCloudAccounts, SnapshotTFCloudAccount{
				ID: a.ID, Name: a.Name, Provider: a.Provider, Description: a.Description, Fields: a.Fields,
				VerifiedAt: a.VerifiedAt, VerifyOK: a.VerifyOK, VerifyMsg: a.VerifyMsg, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
			})
		}
	}
	var allTFVariables []SnapshotTFVariable
	varRows, err := d.Query(`SELECT id, workspace_id, varset_id, key, value, category, hcl, sensitive, description FROM tf_variables ORDER BY id ASC`)
	if err == nil {
		defer varRows.Close()
		for varRows.Next() {
			var v SnapshotTFVariable
			var hclInt, sensInt int
			var wsID, vsID sql.NullInt64
			if err := varRows.Scan(&v.ID, &wsID, &vsID, &v.Key, &v.Value, &v.Category, &hclInt, &sensInt, &v.Description); err == nil {
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
	}
	var allTFVarsets []SnapshotTFVarset
	vsRows, err := d.Query(`SELECT id, name, description, global FROM tf_varsets ORDER BY id ASC`)
	if err == nil {
		defer vsRows.Close()
		for vsRows.Next() {
			var s SnapshotTFVarset
			var globalInt int
			if err := vsRows.Scan(&s.ID, &s.Name, &s.Description, &globalInt); err == nil {
				s.Global = globalInt != 0
				s.WorkspaceIDs, _ = d.GetTFVarsetWorkspaceIDs(s.ID)
				allTFVarsets = append(allTFVarsets, s)
			}
		}
	}
	var allTFPolicies []SnapshotTFPolicy
	polRows, err := d.Query(`SELECT id, name, description, kind, params, enforcement, enabled, workspace_id FROM tf_policies ORDER BY id ASC`)
	if err == nil {
		defer polRows.Close()
		for polRows.Next() {
			var p SnapshotTFPolicy
			var enInt int
			if err := polRows.Scan(&p.ID, &p.Name, &p.Description, &p.Kind, &p.Params, &p.Enforcement, &enInt, &p.WorkspaceID); err == nil {
				p.Enabled = enInt != 0
				allTFPolicies = append(allTFPolicies, p)
			}
		}
	}
	var allTFModules []SnapshotTFModule
	modRows, err := d.Query(`SELECT id, name, provider, description FROM tf_modules ORDER BY id ASC`)
	if err == nil {
		defer modRows.Close()
		for modRows.Next() {
			var m SnapshotTFModule
			if err := modRows.Scan(&m.ID, &m.Name, &m.Provider, &m.Description); err == nil {
				vRows, err := d.Query(`SELECT id, module_id, version, files, doc, source_url, created_at FROM tf_module_versions WHERE module_id = ? ORDER BY id ASC`, m.ID)
				if err == nil {
					for vRows.Next() {
						var mv SnapshotTFModuleVersion
						if err := vRows.Scan(&mv.ID, &mv.ModuleID, &mv.Version, &mv.Files, &mv.Doc, &mv.SourceURL, &mv.CreatedAt); err == nil {
							m.Versions = append(m.Versions, mv)
						}
					}
					vRows.Close()
				}
				allTFModules = append(allTFModules, m)
			}
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

	return &SnapshotPayload{
		Version:                1,
		CreatedAt:              time.Now().UTC(),
		Settings:               settings,
		Hosts:                  hosts,
		DesiredRules:           allRules,
		TerminalPresets:        allPresets,
		SSHKeys:                allSSHKeys,
		KubeClusters:           allClusters,
		CloudflareAccounts:     allCFAccounts,
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
		"alerts", "incidents", "actual_items", "desired_rules", "hosts", "terminal_presets",
		"ssh_keys", "kube_clusters", "cloudflare_accounts",
		"ansible_runs", "ansible_schedules", "ansible_playbooks", "ansible_assets",
		"tf_output_links", "tf_runs", "tf_state_versions", "tf_variables", "tf_varset_workspaces", "tf_varsets", "tf_policies", "tf_module_versions", "tf_modules", "tf_workspaces", "tf_cloud_accounts",
	}
	for _, t := range tables {
		if _, err := tx.Exec("DELETE FROM " + t); err != nil {
			return fmt.Errorf("clear %s: %w", t, err)
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
			internet_online, internet_latency_ms, public_ip, active_conn_count, failed_logins_count,
			top_connections, listening_ports, notes, sort_order, group_name,
			commission_date, bios_date, os_install_epoch,
			kube_cluster_id, kube_mode
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			h.ID, h.Name, h.Host, h.Port, h.User, h.CustomKey, h.KeyID, h.Status, h.OSInfo, h.Kernel, h.Uptime, h.CPULoad,
			h.RAMUsedBytes, h.RAMTotalBytes, h.DiskUsedBytes, h.DiskTotalBytes, h.LifecycleScore, h.LifecycleNotes, h.LifecycleBreakdown,
			h.CreatedAt, h.NetRxBytes, h.NetTxBytes, h.NetRxSpeedBps, h.NetTxSpeedBps,
			h.DiskReadBytes, h.DiskWriteBytes, h.DiskReadSpeedBps, h.DiskWriteSpeedBps,
			onlineInt, h.InternetLatencyMs, h.PublicIP, h.ActiveConnCount, h.FailedLoginsCount,
			h.TopConnections, h.ListeningPorts, h.Notes, h.SortOrder, h.GroupName,
			h.CommissionDate, h.BIOSDate, h.OSInstallEpoch,
			h.KubeClusterID, kubeMode,
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
		_, err := tx.Exec(`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v)
		if err != nil {
			return fmt.Errorf("insert setting %s: %w", k, err)
		}
	}

	_, _ = tx.Exec(`INSERT INTO settings (key, value) VALUES ('bootstrap_completed', '1') ON CONFLICT(key) DO UPDATE SET value = '1'`)

	return tx.Commit()
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

// GetDockerStacks returns the remembered Compose projects of a host as name -> JSON.
func (d *DB) GetDockerStacks(hostID int64) (map[string]string, error) {
	rows, err := d.Query(`SELECT name, data FROM docker_stacks WHERE host_id = ?`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var name, data string
		if err := rows.Scan(&name, &data); err != nil {
			return nil, err
		}
		out[name] = data
	}
	return out, rows.Err()
}

func (d *DB) SaveDockerStack(hostID int64, name, data string) error {
	_, err := d.Exec(`INSERT INTO docker_stacks (host_id, name, data) VALUES (?, ?, ?)
		ON CONFLICT(host_id, name) DO UPDATE SET data = excluded.data`, hostID, name, data)
	return err
}

func (d *DB) DeleteDockerStack(hostID int64, name string) error {
	_, err := d.Exec(`DELETE FROM docker_stacks WHERE host_id = ? AND name = ?`, hostID, name)
	return err
}

// ParseSSHPrivateKey validates a private key PEM, rejecting passphrase-protected keys,
// and extracts the authorized_keys public key string and SHA256 fingerprint.
func ParseSSHPrivateKey(pemBytes []byte) (pubKeyStr string, fingerprint string, err error) {
	var signers []ssh.Signer
	rest := pemBytes
	var passphraseErr error

	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		signer, err := ssh.ParsePrivateKey(pem.EncodeToMemory(block))
		if err != nil {
			var pme *ssh.PassphraseMissingError
			if errors.As(err, &pme) || strings.Contains(strings.ToLower(err.Error()), "passphrase") || strings.Contains(strings.ToLower(err.Error()), "encrypted") {
				passphraseErr = err
			}
			continue
		}
		signers = append(signers, signer)
	}

	if len(signers) == 0 {
		if passphraseErr != nil {
			return "", "", fmt.Errorf("passphrase-protected keys are not supported: %w", passphraseErr)
		}
		s, err := ssh.ParsePrivateKey(pemBytes)
		if err != nil {
			var pme *ssh.PassphraseMissingError
			if errors.As(err, &pme) || strings.Contains(strings.ToLower(err.Error()), "passphrase") || strings.Contains(strings.ToLower(err.Error()), "encrypted") {
				return "", "", fmt.Errorf("passphrase-protected keys are not supported: %w", err)
			}
			return "", "", fmt.Errorf("invalid private key: %w", err)
		}
		signers = append(signers, s)
	}

	pubKey := signers[0].PublicKey()
	pubKeyBytes := ssh.MarshalAuthorizedKey(pubKey)
	pubKeyStr = strings.TrimSpace(string(pubKeyBytes))
	fingerprint = ssh.FingerprintSHA256(pubKey)
	return pubKeyStr, fingerprint, nil
}

// SSH Key Library DB Operations

func (d *DB) CreateSSHKey(k *SSHKey) (int64, error) {
	res, err := d.Exec(`INSERT INTO ssh_keys (name, private_key, public_key, fingerprint) VALUES (?, ?, ?, ?)`,
		k.Name, k.PrivateKey, k.PublicKey, k.Fingerprint)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) ListSSHKeys() ([]SSHKey, error) {
	rows, err := d.Query(`SELECT k.id, k.name, k.public_key, k.fingerprint, k.created_at, COUNT(h.id) AS used_by
		FROM ssh_keys k
		LEFT JOIN hosts h ON h.key_id = k.id
		GROUP BY k.id
		ORDER BY k.id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]SSHKey, 0)
	for rows.Next() {
		var k SSHKey
		if err := rows.Scan(&k.ID, &k.Name, &k.PublicKey, &k.Fingerprint, &k.CreatedAt, &k.UsedBy); err != nil {
			return nil, err
		}
		list = append(list, k)
	}
	return list, rows.Err()
}

func (d *DB) GetSSHKey(id int64) (*SSHKey, error) {
	var k SSHKey
	err := d.QueryRow(`SELECT k.id, k.name, k.private_key, k.public_key, k.fingerprint, k.created_at, COUNT(h.id) AS used_by
		FROM ssh_keys k
		LEFT JOIN hosts h ON h.key_id = k.id
		WHERE k.id = ?
		GROUP BY k.id`, id).Scan(&k.ID, &k.Name, &k.PrivateKey, &k.PublicKey, &k.Fingerprint, &k.CreatedAt, &k.UsedBy)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

func (d *DB) DeleteSSHKey(id int64) error {
	var usedBy int
	err := d.QueryRow(`SELECT COUNT(*) FROM hosts WHERE key_id = ?`, id).Scan(&usedBy)
	if err != nil {
		return err
	}
	if usedBy > 0 {
		return fmt.Errorf("%w by %d host(s); reassign them first", ErrSSHKeyInUse, usedBy)
	}
	res, err := d.Exec(`DELETE FROM ssh_keys WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrSSHKeyNotFound
	}
	return nil
}

func (d *DB) ResolveHostPrivateKey(h *Host) (string, error) {
	if h == nil {
		return "", errors.New("host is nil")
	}
	if h.KeyID > 0 {
		key, err := d.GetSSHKey(h.KeyID)
		if err != nil {
			return "", fmt.Errorf("lookup ssh key %d: %w", h.KeyID, err)
		}
		if key == nil {
			return "", fmt.Errorf("ssh key %d not found", h.KeyID)
		}
		return key.PrivateKey, nil
	}
	if strings.TrimSpace(h.CustomKey) != "" {
		return h.CustomKey, nil
	}
	globalKey, err := d.GetSetting("ssh_private_key")
	if err != nil {
		return "", fmt.Errorf("get global ssh key: %w", err)
	}
	return globalKey, nil
}
