package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// fleetSearchFolders searches host groups. ADR 0011 stores a group as the
// hosts.group_name column; the member count is derived, not stored. There is
// no description or tags column, so subtitle reports the member count only.
func (d *DB) fleetSearchFolders(b *fleetGroupBuilder, tokens []string, limit int) error {
	where, args := tokenCondition([]string{"h.group_name"}, tokens)
	q := `SELECT h.group_name, COUNT(*), MAX(h.last_inspected)
		FROM hosts h WHERE h.group_name <> '' AND ` + where + `
		GROUP BY h.group_name ORDER BY h.group_name`
	if limit > 0 {
		q += ` LIMIT ` + strconvItoa(limit)
	}
	rows, err := d.Query(q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var count int
		var inspected sql.NullTime
		if err := rows.Scan(&name, &count, &inspected); err != nil {
			return err
		}
		key := FleetSearchFolder + "\x00" + strings.ToLower(name)
		b.addMeta(FleetSearchFolder, key, name, FleetSearchGroup{
			GroupID: name, TargetTab: "hosts",
			Subtitle: fmt.Sprintf("%d member(s)", count),
		}, FleetSearchHost{Name: name, Group: name, DataAge: fleetHostAge(inspected)})
		// A group is one GROUP BY row, so the builder sees one host; the real
		// member count is the SQL COUNT(*).
		b.groups[key].HostCount = count
	}
	return rows.Err()
}

// fleetSearchHosts searches extended host attributes: name, address, public and
// private IPs, OS/distro, hardware model and notes. SSH config aliases become
// hosts.name on import (ADR 0035), so they are covered by the name column.
func (d *DB) fleetSearchHosts(b *fleetGroupBuilder, tokens []string, limit int) error {
	cols := []string{"h.name", "h.host", "h.public_ip", "h.private_ips", "h.os_info", "h.hardware_model", "h.notes", "h.group_name"}
	where, args := tokenCondition(cols, tokens)
	q := `SELECT h.id, h.name, h.host, h.group_name, h.status, h.public_ip, h.private_ips,
		h.os_info, h.hardware_model, h.listening_ports, h.last_inspected
		FROM hosts h WHERE ` + where + ` ORDER BY h.name`
	if limit > 0 {
		q += ` LIMIT ` + strconvItoa(limit)
	}
	rows, err := d.Query(q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id                           int64
			name, address, group, status string
			publicIP, privateIPs, osInfo string
			hardware, listening          string
			inspected                    sql.NullTime
		)
		if err := rows.Scan(&id, &name, &address, &group, &status, &publicIP, &privateIPs, &osInfo, &hardware, &listening, &inspected); err != nil {
			return err
		}
		sub := make([]string, 0, 3)
		if address != "" {
			sub = append(sub, address)
		}
		if osInfo != "" {
			sub = append(sub, osInfo)
		}
		if hardware != "" {
			sub = append(sub, hardware)
		}
		key := FleetSearchHostKind + "\x00" + strconvItoa64(id)
		b.addMeta(FleetSearchHostKind, key, name, FleetSearchGroup{
			EntityID: strconvItoa64(id), TargetTab: "overview",
			Subtitle: strings.Join(sub, " · "), StatusBadge: status,
		}, FleetSearchHost{HostID: id, Name: name, Group: group, DataAge: fleetHostAge(inspected)})
	}
	return rows.Err()
}

// fleetSearchPorts searches listening ports and their process owners. The data
// is a JSON array in hosts.listening_ports (ADR 0039 vocabulary: "listening
// ports & process owners"). We scan only hosts that have data, then filter in
// Go because the payload is JSON, not a searchable column.
func (d *DB) fleetSearchPorts(b *fleetGroupBuilder, tokens []string, limit int) error {
	rows, err := d.Query(`SELECT h.id, h.name, h.group_name, h.listening_ports, h.last_inspected
		FROM hosts h WHERE h.listening_ports <> '' AND h.listening_ports <> '[]'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	found := 0
	for rows.Next() {
		var id int64
		var name, group, raw string
		var inspected sql.NullTime
		if err := rows.Scan(&id, &name, &group, &raw, &inspected); err != nil {
			return err
		}
		var ports []ListeningPort
		if json.Unmarshal([]byte(raw), &ports) != nil {
			continue
		}
		for _, p := range ports {
			text := strings.Join([]string{p.Port, p.Proto, p.Address, p.Process, p.ProcessSource, p.Risk}, " ")
			if !matchesTokens(text, tokens) {
				continue
			}
			label := p.Port
			if p.Process != "" {
				label = p.Process + " :" + p.Port
			}
			key := FleetSearchPort + "\x00" + strings.ToLower(label)
			b.addMeta(FleetSearchPort, key, label, FleetSearchGroup{
				EntityID: strconvItoa64(id), TargetTab: "network",
				Subtitle: p.Proto + " " + p.Address, StatusBadge: p.Risk,
			}, FleetSearchHost{HostID: id, Name: name, Group: group, DataAge: fleetHostAge(inspected)})
			found++
			if limit > 0 && found >= limit {
				return nil
			}
		}
	}
	return rows.Err()
}

// fleetSearchServices searches systemd units and other service names stored as
// actual_items of kind "service" or "port". Systemd units themselves are not
// persisted (ADR 0039), but desired/actual drift rules of kind service are.
func (d *DB) fleetSearchServices(b *fleetGroupBuilder, tokens []string, limit int) error {
	where, args := tokenCondition([]string{"a.target", "a.current", "a.details"}, tokens)
	q := `SELECT a.host_id, a.kind, a.target, a.current, a.status, a.updated_at, h.name, h.group_name
		FROM actual_items a JOIN hosts h ON h.id = a.host_id
		WHERE a.kind IN ('service', 'port') AND ` + where + ` ORDER BY a.target, h.name`
	if limit > 0 {
		q += ` LIMIT ` + strconvItoa(limit)
	}
	rows, err := d.Query(q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			hostID                        int64
			kind, target, current, status string
			updated                       time.Time
			hostName, groupName           string
		)
		if err := rows.Scan(&hostID, &kind, &target, &current, &status, &updated, &hostName, &groupName); err != nil {
			return err
		}
		key := FleetSearchService + "\x00" + strings.ToLower(target)
		b.addMeta(FleetSearchService, key, target, FleetSearchGroup{
			EntityID: strconvItoa64(hostID), TargetTab: "systemd",
			Subtitle: current, StatusBadge: status,
		}, FleetSearchHost{HostID: hostID, Name: hostName, Group: groupName, DataAge: &updated})
	}
	return rows.Err()
}
