package store

// Host inventory: the host row, its columns, and the CRUD operations.

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"
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
	PrivateIPs        string `json:"private_ips"` // JSON serialized array of private IPv4 addresses, primary first
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
	NetworkID         int64  `json:"network_id"` // VPN Network (0 = direct)
}

// PrivateIPList returns the host's stored private IPv4 addresses, primary first.
func (h *Host) PrivateIPList() []string {
	var ips []string
	_ = json.Unmarshal([]byte(h.PrivateIPs), &ips)
	return ips
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

const hostSelectCols = `SELECT id, name, host, port, user, COALESCE(custom_key,''), COALESCE(key_id, 0), status, os_info, kernel, uptime, cpu_load, ram_used_bytes, ram_total_bytes, disk_used_bytes, disk_total_bytes, lifecycle_score, lifecycle_notes, COALESCE(lifecycle_breakdown, '[]'), last_inspected, created_at,
	COALESCE(net_rx_bytes, 0), COALESCE(net_tx_bytes, 0), COALESCE(net_rx_speed_bps, 0), COALESCE(net_tx_speed_bps, 0),
	COALESCE(disk_read_bytes, 0), COALESCE(disk_write_bytes, 0), COALESCE(disk_read_speed_bps, 0), COALESCE(disk_write_speed_bps, 0),
	COALESCE(internet_online, 0), COALESCE(internet_latency_ms, 0), COALESCE(public_ip, ''), COALESCE(private_ips, ''), COALESCE(active_conn_count, 0),
	COALESCE(failed_logins_count, 0), COALESCE(top_connections, ''), COALESCE(listening_ports, ''),
	COALESCE(notes, ''), COALESCE(sort_order, 0), COALESCE(group_name, ''),
	COALESCE(cpu_cores, 1), COALESCE(hardware_model, ''), COALESCE(swap_used_bytes, 0), COALESCE(swap_total_bytes, 0),
	COALESCE(last_duration_ms, 0), COALESCE(commission_date, ''), COALESCE(bios_date, ''), COALESCE(os_install_epoch, 0),
	COALESCE(top_cpu_processes, ''), COALESCE(top_ram_processes, ''), COALESCE(top_swap_processes, ''),
	COALESCE(kube_cluster_id, ''), COALESCE(kube_mode, 'full'), COALESCE(network_id, 0)
	FROM hosts `

func scanHost(s rowScanner) (*Host, error) {
	var h Host
	var inspected sql.NullTime
	var online int
	err := s.Scan(&h.ID, &h.Name, &h.Host, &h.Port, &h.User, &h.CustomKey, &h.KeyID, &h.Status, &h.OSInfo, &h.Kernel, &h.Uptime, &h.CPULoad, &h.RAMUsedBytes, &h.RAMTotalBytes, &h.DiskUsedBytes, &h.DiskTotalBytes, &h.LifecycleScore, &h.LifecycleNotes, &h.LifecycleBreakdown, &inspected, &h.CreatedAt,
		&h.NetRxBytes, &h.NetTxBytes, &h.NetRxSpeedBps, &h.NetTxSpeedBps,
		&h.DiskReadBytes, &h.DiskWriteBytes, &h.DiskReadSpeedBps, &h.DiskWriteSpeedBps,
		&online, &h.InternetLatencyMs, &h.PublicIP, &h.PrivateIPs, &h.ActiveConnCount,
		&h.FailedLoginsCount, &h.TopConnections, &h.ListeningPorts,
		&h.Notes, &h.SortOrder, &h.GroupName,
		&h.CPUCores, &h.HardwareModel, &h.SwapUsedBytes, &h.SwapTotalBytes,
		&h.LastDurationMs, &h.CommissionDate, &h.BIOSDate, &h.OSInstallEpoch,
		&h.TopCPUProcesses, &h.TopRAMProcesses, &h.TopSwapProcesses,
		&h.KubeClusterID, &h.KubeMode, &h.NetworkID)
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
	if h.KubeMode == "" {
		h.KubeMode = "full"
	}
	tx, err := d.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if h.GroupName, err = hostGroupSpelling(tx, h.GroupName, 0); err != nil {
		return 0, err
	}
	var maxOrder int
	_ = tx.QueryRow(`SELECT COALESCE(MAX(sort_order), 0) FROM hosts`).Scan(&maxOrder)
	res, err := tx.Exec(`INSERT INTO hosts (name, host, port, user, custom_key, key_id, status, notes, sort_order, group_name, commission_date, bios_date, kube_cluster_id, kube_mode, network_id) VALUES (?, ?, ?, ?, ?, ?, 'unknown', ?, ?, ?, ?, ?, ?, ?, ?)`, h.Name, h.Host, h.Port, h.User, h.CustomKey, h.KeyID, h.Notes, maxOrder+1, h.GroupName, h.CommissionDate, h.BIOSDate, h.KubeClusterID, h.KubeMode, h.NetworkID)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (d *DB) UpdateHost(h *Host) error {
	if h.KeyID > 0 {
		h.CustomKey = ""
	}
	if h.KubeMode == "" {
		h.KubeMode = "full"
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if h.GroupName, err = hostGroupSpelling(tx, h.GroupName, h.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE hosts SET name=?, host=?, port=?, user=?, custom_key=?, key_id=?, notes=?, group_name=?, commission_date=?, kube_mode=?, network_id=? WHERE id=?`, h.Name, h.Host, h.Port, h.User, h.CustomKey, h.KeyID, h.Notes, h.GroupName, h.CommissionDate, h.KubeMode, h.NetworkID, h.ID); err != nil {
		return err
	}
	return tx.Commit()
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
	_, err := d.Exec(`UPDATE hosts SET status=?, os_info=?, kernel=?, uptime=?, cpu_load=?, ram_used_bytes=?, ram_total_bytes=?, disk_used_bytes=?, disk_total_bytes=?, lifecycle_score=?, lifecycle_notes=?, lifecycle_breakdown=?, last_inspected=?, net_rx_bytes=?, net_tx_bytes=?, net_rx_speed_bps=?, net_tx_speed_bps=?, disk_read_bytes=?, disk_write_bytes=?, disk_read_speed_bps=?, disk_write_speed_bps=?, internet_online=?, internet_latency_ms=?, public_ip=?, private_ips=?, active_conn_count=?, failed_logins_count=?, top_connections=?, listening_ports=?, cpu_cores=?, hardware_model=?, swap_used_bytes=?, swap_total_bytes=?, last_duration_ms=?, bios_date=?, os_install_epoch=?, top_cpu_processes=?, top_ram_processes=?, top_swap_processes=? WHERE id=?`,
		h.Status, h.OSInfo, h.Kernel, h.Uptime, h.CPULoad, h.RAMUsedBytes, h.RAMTotalBytes, h.DiskUsedBytes, h.DiskTotalBytes, h.LifecycleScore, h.LifecycleNotes, h.LifecycleBreakdown, now,
		h.NetRxBytes, h.NetTxBytes, h.NetRxSpeedBps, h.NetTxSpeedBps,
		h.DiskReadBytes, h.DiskWriteBytes, h.DiskReadSpeedBps, h.DiskWriteSpeedBps,
		onlineInt, h.InternetLatencyMs, h.PublicIP, h.PrivateIPs, h.ActiveConnCount, h.FailedLoginsCount, h.TopConnections, h.ListeningPorts,
		h.CPUCores, h.HardwareModel, h.SwapUsedBytes, h.SwapTotalBytes, h.LastDurationMs, h.BIOSDate, h.OSInstallEpoch,
		h.TopCPUProcesses, h.TopRAMProcesses, h.TopSwapProcesses,
		h.ID)
	return err
}
