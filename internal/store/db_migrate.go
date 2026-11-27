package store

// Schema migration: the CREATE TABLE statements and the idempotent column
// additions that keep an existing database on the current schema.

import (
	"fmt"
)

func (d *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS vpn_networks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		driver TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 0,
		auto_up INTEGER NOT NULL DEFAULT 1,
		iface TEXT NOT NULL DEFAULT '',
		config TEXT NOT NULL DEFAULT '{}',
		last_state TEXT NOT NULL DEFAULT 'down',
		last_error TEXT NOT NULL DEFAULT '',
		last_checked DATETIME,
		last_up_at DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS auth_sessions (
		token TEXT PRIMARY KEY,
		expires_at INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_auth_sessions_expires ON auth_sessions(expires_at);

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
		private_ips TEXT DEFAULT '',
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
		kube_mode TEXT NOT NULL DEFAULT 'full',
		geo_lat REAL DEFAULT 0,
		geo_lon REAL DEFAULT 0,
		geo_region TEXT DEFAULT '',
		geo_label TEXT DEFAULT '',
		os_profile TEXT DEFAULT ''
	);

	-- 3D Globe Network Radar (ADR 0090): offline-safe GeoIP cache with 30-day TTL.
	CREATE TABLE IF NOT EXISTS geoip_cache (
		ip TEXT PRIMARY KEY,
		lat REAL NOT NULL DEFAULT 0,
		region TEXT NOT NULL DEFAULT '',
		lon REAL NOT NULL DEFAULT 0,
		city TEXT NOT NULL DEFAULT '',
		country_code TEXT NOT NULL DEFAULT '',
		asn_org TEXT NOT NULL DEFAULT '',
		source TEXT NOT NULL DEFAULT '',
		updated_at DATETIME NOT NULL
	);

	-- Persisted netdiag ping results (gateway<->host and host<->host latency).
	CREATE TABLE IF NOT EXISTS probe_results (
		source_host_id INTEGER NOT NULL DEFAULT 0,
		target_host_id INTEGER NOT NULL DEFAULT 0,
		latency_ms REAL NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT '',
		probed_at DATETIME NOT NULL,
		PRIMARY KEY (source_host_id, target_host_id)
	);

	CREATE TABLE IF NOT EXISTS host_packages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		version TEXT NOT NULL,
		source TEXT NOT NULL DEFAULT '',
		source_version TEXT NOT NULL DEFAULT '',
		size_bytes INTEGER NOT NULL DEFAULT 0,
		install_epoch INTEGER NOT NULL DEFAULT 0,
		is_manual INTEGER NOT NULL DEFAULT 0,
		service_name TEXT NOT NULL DEFAULT '',
		service_status TEXT NOT NULL DEFAULT '',
		last_scanned_at DATETIME NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_host_packages_host_manual ON host_packages(host_id, is_manual);

	CREATE TABLE IF NOT EXISTS package_update_checks (
		host_id INTEGER PRIMARY KEY REFERENCES hosts(id) ON DELETE CASCADE,
		checked_at DATETIME NOT NULL,
		supported INTEGER NOT NULL DEFAULT 0,
		manager TEXT NOT NULL DEFAULT '',
		pending_total INTEGER NOT NULL DEFAULT 0,
		pending_security INTEGER NOT NULL DEFAULT 0,
		reboot_required INTEGER NOT NULL DEFAULT 0,
		repo_cache_age_hours INTEGER NOT NULL DEFAULT -1,
		packages_json TEXT NOT NULL DEFAULT '[]',
		note TEXT NOT NULL DEFAULT '',
		error TEXT NOT NULL DEFAULT ''
	);

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

	CREATE TABLE IF NOT EXISTS agent_memory (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id INTEGER REFERENCES hosts(id) ON DELETE CASCADE,
		cluster_id INTEGER REFERENCES kube_clusters(id) ON DELETE CASCADE,
		domain TEXT NOT NULL DEFAULT '',
		content TEXT NOT NULL,
		source TEXT NOT NULL DEFAULT 'agent',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- AI agent chat history: local and re-creatable, so it stays out of System Snapshots (ADR-0091). A thread
	-- groups one conversation under the resource it is about; its messages go with it (ON DELETE CASCADE).
	CREATE TABLE IF NOT EXISTS agent_threads (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		scope_type TEXT NOT NULL DEFAULT 'global',
		scope_id INTEGER NOT NULL DEFAULT 0,
		session TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_agent_threads_scope ON agent_threads(scope_type, scope_id, updated_at DESC);
	CREATE INDEX IF NOT EXISTS idx_agent_threads_session ON agent_threads(session, updated_at DESC);

	CREATE TABLE IF NOT EXISTS agent_messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		thread_id INTEGER NOT NULL REFERENCES agent_threads(id) ON DELETE CASCADE,
		role TEXT NOT NULL,
		content TEXT NOT NULL DEFAULT '',
		tool_calls_json TEXT NOT NULL DEFAULT '',
		approval_status TEXT NOT NULL DEFAULT '',
		tool_call_id TEXT NOT NULL DEFAULT '',
		tool_name TEXT NOT NULL DEFAULT '',
		is_error INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_agent_messages_thread ON agent_messages(thread_id, id);

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

	-- Containers last seen on a host (name, image, state), so the fleet search can answer
	-- "which hosts run postgres:13?" without opening the Docker tab.
	CREATE TABLE IF NOT EXISTS host_containers (
		host_id INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		image TEXT NOT NULL,
		state TEXT NOT NULL DEFAULT '',
		seen_at DATETIME NOT NULL,
		PRIMARY KEY (host_id, name)
	);

	-- Domains served by the reverse proxies of a host (nginx, Caddy, Traefik), stored on
	-- demand so the fleet view and the fleet search answer without SSH. Both tables are
	-- re-derivable by a fresh scan and stay out of the System Snapshot.
	CREATE TABLE IF NOT EXISTS web_scans (
		host_id INTEGER PRIMARY KEY REFERENCES hosts(id) ON DELETE CASCADE,
		scanned_at DATETIME NOT NULL,
		proxies TEXT NOT NULL DEFAULT '[]',
		warnings TEXT NOT NULL DEFAULT '[]'
	);
	CREATE TABLE IF NOT EXISTS host_domains (
		host_id INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
		domain TEXT NOT NULL,
		proxy TEXT NOT NULL DEFAULT '',
		location TEXT NOT NULL DEFAULT '',
		container TEXT NOT NULL DEFAULT '',
		tls INTEGER NOT NULL DEFAULT 0,
		data TEXT NOT NULL DEFAULT '{}'
	);
	CREATE INDEX IF NOT EXISTS idx_host_domains_host ON host_domains(host_id);
	CREATE INDEX IF NOT EXISTS idx_host_domains_domain ON host_domains(domain);

	CREATE TABLE IF NOT EXISTS ssh_host_keys (
		addr TEXT PRIMARY KEY,
		key TEXT NOT NULL,
		created_at DATETIME NOT NULL
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
	CREATE TABLE IF NOT EXISTS docker_security_scans (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
		kind TEXT NOT NULL,
		subject TEXT NOT NULL DEFAULT '',
		scanned_at DATETIME NOT NULL,
		scanner TEXT NOT NULL DEFAULT '',
		summary_json TEXT NOT NULL DEFAULT '{}',
		findings_json TEXT NOT NULL DEFAULT '[]',
		error TEXT NOT NULL DEFAULT '',
		UNIQUE (host_id, kind, subject)
	);
	CREATE INDEX IF NOT EXISTS idx_docker_security_scans_host ON docker_security_scans(host_id, kind);
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
	CREATE TABLE IF NOT EXISTS s3_accounts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE COLLATE NOCASE,
		endpoint TEXT NOT NULL DEFAULT '',
		region TEXT NOT NULL DEFAULT '',
		bucket TEXT NOT NULL DEFAULT '',
		access_key TEXT NOT NULL DEFAULT '',
		secret_key TEXT NOT NULL DEFAULT '',
		prefix TEXT NOT NULL DEFAULT '',
		path_style TEXT NOT NULL DEFAULT '',
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
	CREATE TABLE IF NOT EXISTS git_repos (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE COLLATE NOCASE,
		url TEXT NOT NULL,
		branch TEXT NOT NULL DEFAULT '',
		group_name TEXT NOT NULL DEFAULT '',
		dir TEXT NOT NULL UNIQUE,
		auth TEXT NOT NULL DEFAULT 'none',
		token TEXT NOT NULL DEFAULT '',
		key_id INTEGER NOT NULL DEFAULT 0,
		synced_at DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS iac_scans (
		source_kind TEXT NOT NULL,
		source_id INTEGER NOT NULL,
		scanned_at DATETIME NOT NULL,
		summary_json TEXT NOT NULL DEFAULT '{}',
		findings_json TEXT NOT NULL DEFAULT '[]',
		status TEXT NOT NULL DEFAULT '',
		files_scanned INTEGER NOT NULL DEFAULT 0,
		truncated INTEGER NOT NULL DEFAULT 0,
		root TEXT NOT NULL DEFAULT '',
		notes_json TEXT NOT NULL DEFAULT '[]',
		PRIMARY KEY (source_kind, source_id)
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

	CREATE TABLE IF NOT EXISTS vault_secrets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		key TEXT NOT NULL UNIQUE COLLATE NOCASE,
		scope TEXT NOT NULL DEFAULT 'global',
		scope_ref TEXT NOT NULL DEFAULT '',
		ciphertext BLOB NOT NULL,
		current_version INTEGER NOT NULL DEFAULT 1,
		expires_at DATETIME,
		rotation_days INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	);
	CREATE TABLE IF NOT EXISTS vault_versions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		secret_id INTEGER NOT NULL REFERENCES vault_secrets(id) ON DELETE CASCADE,
		version INTEGER NOT NULL,
		ciphertext BLOB NOT NULL,
		created_at DATETIME NOT NULL,
		UNIQUE (secret_id, version)
	);
	CREATE TABLE IF NOT EXISTS vault_templates (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		body TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	);
	CREATE TABLE IF NOT EXISTS vault_targets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		kind TEXT NOT NULL,
		template_id INTEGER NOT NULL DEFAULT 0,
		target_ref TEXT NOT NULL DEFAULT '',
		path TEXT NOT NULL DEFAULT '',
		namespace TEXT NOT NULL DEFAULT '',
		post_sync_hook TEXT NOT NULL DEFAULT '',
		last_sync_sha TEXT NOT NULL DEFAULT '',
		last_sync_at DATETIME,
		drift_status TEXT NOT NULL DEFAULT 'unknown',
		created_at DATETIME NOT NULL
	);
	CREATE TABLE IF NOT EXISTS vault_audit (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ts DATETIME NOT NULL,
		action TEXT NOT NULL,
		detail TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX IF NOT EXISTS idx_vault_versions_secret ON vault_versions(secret_id);
	CREATE INDEX IF NOT EXISTS idx_vault_targets_kind ON vault_targets(kind);

	-- CVE Exposure Radar: derived telemetry, re-derivable by a fresh scan; not in System Snapshot (ADR 0088).
	CREATE TABLE IF NOT EXISTS exposure_scans (
		host_id INTEGER PRIMARY KEY REFERENCES hosts(id) ON DELETE CASCADE,
		scanned_at DATETIME NOT NULL,
		status TEXT NOT NULL DEFAULT 'ok',
		enabled INTEGER NOT NULL DEFAULT 0,
		findings_json TEXT NOT NULL DEFAULT '[]',
		kernel_json TEXT NOT NULL DEFAULT '',
		critical_exposed INTEGER NOT NULL DEFAULT 0,
		notes_json TEXT NOT NULL DEFAULT '[]',
		error TEXT NOT NULL DEFAULT ''
	);
	`
	_, err := d.Exec(schema)
	if err != nil {
		return err
	}
	if err := d.migrateAccountAudits(); err != nil {
		return fmt.Errorf("migrate account audits: %w", err)
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
	_ = d.alterAddColumn("git_repos", "group_name", "TEXT NOT NULL DEFAULT ''")
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
	_ = d.alterAddColumn("hosts", "private_ips", "TEXT DEFAULT ''")
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
	_ = d.alterAddColumn("host_packages", "source", "TEXT NOT NULL DEFAULT ''")
	_ = d.alterAddColumn("host_packages", "source_version", "TEXT NOT NULL DEFAULT ''")
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
	_ = d.alterAddColumn("hosts", "network_id", "INTEGER NOT NULL DEFAULT 0")
	_ = d.alterAddColumn("hosts", "geo_lat", "REAL DEFAULT 0")
	_ = d.alterAddColumn("hosts", "geo_lon", "REAL DEFAULT 0")
	_ = d.alterAddColumn("hosts", "geo_label", "TEXT DEFAULT ''")
	_ = d.alterAddColumn("hosts", "geo_region", "TEXT DEFAULT ''")
	_ = d.alterAddColumn("hosts", "os_profile", "TEXT DEFAULT ''")
	// Legacy SSH algorithms are opt-in per host; existing hosts start on the modern profile.
	_ = d.alterAddColumn("hosts", "allow_legacy_ssh", "INTEGER NOT NULL DEFAULT 0")
	// Local ssh-agent authentication is opt-in per host; the fingerprint picks one key from the agent.
	_ = d.alterAddColumn("hosts", "ssh_agent", "INTEGER NOT NULL DEFAULT 0")
	_ = d.alterAddColumn("hosts", "ssh_agent_fingerprint", "TEXT NOT NULL DEFAULT ''")
	_ = d.alterAddColumn("geoip_cache", "region", "TEXT NOT NULL DEFAULT ''")
	_, _ = d.Exec(`CREATE INDEX IF NOT EXISTS idx_hosts_network ON hosts(network_id)`)
	_ = d.alterAddColumn("vpn_networks", "identity", "BLOB")
	// A note may be scoped to a cluster or a functional domain as well as to a host (all optional, '' = none).
	_ = d.alterAddColumn("agent_memory", "cluster_id", "INTEGER")
	_ = d.alterAddColumn("agent_memory", "domain", "TEXT NOT NULL DEFAULT ''")
	_, _ = d.Exec(`CREATE INDEX IF NOT EXISTS idx_agent_memory_domain ON agent_memory(domain)`)
	// Fleet search filters actual_items by kind alone; the UNIQUE(host_id,kind,target) index cannot serve it.
	_, _ = d.Exec(`CREATE INDEX IF NOT EXISTS idx_actual_items_kind ON actual_items(kind)`)
	if err := d.migrateAgentSettings(); err != nil {
		return fmt.Errorf("migrate agent settings: %w", err)
	}
	if err := d.migrateAgentTriage(); err != nil {
		return fmt.Errorf("migrate agent triage: %w", err)
	}
	return nil
}

func (d *DB) alterAddColumn(table, column, colType string) error {
	_, err := d.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, colType))
	return err
}
