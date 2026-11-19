package store

// Fleet Docker overview (ADR 0090). One bounded SQLite pass over the already-persisted Docker
// telemetry: host_containers, docker_stacks and docker_security_scans. No SSH, no network: the
// global Docker section can show an honest fleet summary without opening every host.

import (
	"encoding/json"
	"sort"
	"time"
)

// DockerHostSummary is the Docker posture of one host as last recorded.
type DockerHostSummary struct {
	HostID         int64     `json:"host_id"`
	HostName       string    `json:"host_name"`
	GroupName      string    `json:"group_name,omitempty"`
	ContainerCount int       `json:"container_count"`
	Running        int       `json:"running"`
	Stopped        int       `json:"stopped"`
	StackCount     int       `json:"stack_count"`
	SecurityIssues int       `json:"security_issues"` // critical+high misconfig + image vulns
	AuditStatus    string    `json:"audit_status"`    // ok|partial|unsupported|error|"" (never scanned)
	SeenAt         time.Time `json:"seen_at"`         // newest of container/scan timestamps
}

// DockerOverview is the whole aggregated response of GET /api/docker/overview.
type DockerOverview struct {
	HostCount           int                 `json:"host_count"`
	HostsWithDocker     int                 `json:"hosts_with_docker"`
	HostsWithoutDocker  int                 `json:"hosts_without_docker"`
	TotalContainers     int                 `json:"total_containers"`
	RunningContainers   int                 `json:"running_containers"`
	StoppedContainers   int                 `json:"stopped_containers"`
	TotalStacks         int                 `json:"total_stacks"`
	TotalSecurityIssues int                 `json:"total_security_issues"`
	Hosts               []DockerHostSummary `json:"hosts"`
	GeneratedAt         time.Time           `json:"generated_at"`
}

// GetDockerOverview aggregates persisted Docker telemetry for the whole fleet. optional hostID and
// groupName narrow the scope (hostID > 0 wins; groupName matches hosts.group_name).
func (d *DB) GetDockerOverview(hostID int64, groupName string) (*DockerOverview, error) {
	scopeSQL := "SELECT id, name, group_name FROM hosts WHERE 1=1"
	var scopeArgs []any
	if hostID > 0 {
		scopeSQL += " AND id = ?"
		scopeArgs = append(scopeArgs, hostID)
	}
	if groupName != "" {
		scopeSQL += " AND group_name = ?"
		scopeArgs = append(scopeArgs, groupName)
	}
	ov := &DockerOverview{Hosts: []DockerHostSummary{}, GeneratedAt: time.Now().UTC()}

	rows, err := d.Query(scopeSQL+" ORDER BY name", scopeArgs...)
	if err != nil {
		return nil, err
	}
	type hostRef struct {
		id    int64
		group string
		name  string
	}
	hosts := []hostRef{}
	for rows.Next() {
		var h hostRef
		if rows.Scan(&h.id, &h.name, &h.group) == nil {
			hosts = append(hosts, h)
		}
	}
	rows.Close()

	ids := make([]int64, 0, len(hosts))
	names := map[int64]string{}
	groups := map[int64]string{}
	for _, h := range hosts {
		ids = append(ids, h.id)
		names[h.id] = h.name
		groups[h.id] = h.group
	}
	ov.HostCount = len(ids)
	byHost := map[int64]*DockerHostSummary{}
	for _, id := range ids {
		byHost[id] = &DockerHostSummary{HostID: id, HostName: names[id], GroupName: groups[id]}
	}

	// Containers: count running/stopped per host, track freshness.
	crows, err := d.Query(`SELECT host_id, state, seen_at FROM host_containers`)
	if err == nil {
		for crows.Next() {
			var hid int64
			var state string
			var seen time.Time
			if crows.Scan(&hid, &state, &seen) != nil {
				continue
			}
			h, ok := byHost[hid]
			if !ok {
				continue
			}
			h.ContainerCount++
			if state == "running" {
				h.Running++
			} else {
				h.Stopped++
			}
			if seen.After(h.SeenAt) {
				h.SeenAt = seen
			}
		}
		crows.Close()
	}

	// Stacks: one row per remembered Compose project.
	srows, err := d.Query(`SELECT host_id, COUNT(*) FROM docker_stacks GROUP BY host_id`)
	if err == nil {
		for srows.Next() {
			var hid int64
			var n int
			if srows.Scan(&hid, &n) == nil {
				if h, ok := byHost[hid]; ok {
					h.StackCount = n
				}
			}
		}
		srows.Close()
	}

	// Security scans: misconfig findings + image vuln counts per host.
	secrows, err := d.Query(`SELECT host_id, kind, scanned_at, summary_json FROM docker_security_scans`)
	if err == nil {
		for secrows.Next() {
			var hid int64
			var kind, summaryJSON string
			var scanned time.Time
			if secrows.Scan(&hid, &kind, &scanned, &summaryJSON) != nil {
				continue
			}
			h, ok := byHost[hid]
			if !ok {
				continue
			}
			var sum dockerSecuritySummary
			if json.Unmarshal([]byte(summaryJSON), &sum) != nil {
				continue
			}
			h.SecurityIssues += sum.Counts.Critical + sum.Counts.High + sum.VulnCounts.Critical + sum.VulnCounts.High
			if kind == "misconfig" {
				h.AuditStatus = sum.Status
			}
			if scanned.After(h.SeenAt) {
				h.SeenAt = scanned
			}
		}
		secrows.Close()
	}

	// Roll up and sort: hosts with Docker (containers/stacks/audit) first, by issue count.
	for _, id := range ids {
		h := byHost[id]
		ov.TotalContainers += h.ContainerCount
		ov.RunningContainers += h.Running
		ov.StoppedContainers += h.Stopped
		ov.TotalStacks += h.StackCount
		ov.TotalSecurityIssues += h.SecurityIssues
		if h.ContainerCount > 0 || h.StackCount > 0 || h.AuditStatus != "" {
			ov.HostsWithDocker++
		} else {
			ov.HostsWithoutDocker++
		}
		ov.Hosts = append(ov.Hosts, *h)
	}
	sort.SliceStable(ov.Hosts, func(i, j int) bool {
		a, b := ov.Hosts[i], ov.Hosts[j]
		if (a.ContainerCount > 0) != (b.ContainerCount > 0) {
			return a.ContainerCount > 0
		}
		if a.SecurityIssues != b.SecurityIssues {
			return a.SecurityIssues > b.SecurityIssues
		}
		return a.HostName < b.HostName
	})
	return ov, nil
}
