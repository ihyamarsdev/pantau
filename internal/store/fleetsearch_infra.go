package store

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

// fleetSearchKube searches saved Kubernetes clusters.
func (d *DB) fleetSearchKube(b *fleetGroupBuilder, tokens []string, limit int) error {
	where, args := tokenCondition([]string{"k.name", "k.context", "k.server", "k.provider"}, tokens)
	q := `SELECT k.id, k.name, k.context, k.server, k.provider, k.created_at
		FROM kube_clusters k WHERE ` + where + ` ORDER BY k.name`
	if limit > 0 {
		q += ` LIMIT ` + strconvItoa(limit)
	}
	rows, err := d.Query(q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name, context, server, provider string
		var created time.Time
		if err := rows.Scan(&id, &name, &context, &server, &provider, &created); err != nil {
			return err
		}
		key := FleetSearchKube + "\x00" + strconvItoa64(id)
		b.addMeta(FleetSearchKube, key, name, FleetSearchGroup{
			EntityID: strconvItoa64(id), TargetTab: "kube",
			Subtitle: provider + " · " + server, StatusBadge: context,
		}, FleetSearchHost{Name: name, DataAge: &created})
	}
	return rows.Err()
}

// fleetSearchVPN searches VPN networks (ADR 0077). The config JSON holds
// secrets, so it is never searched or returned; only name, driver, iface and
// last_state are indexed here.
func (d *DB) fleetSearchVPN(b *fleetGroupBuilder, tokens []string, limit int) error {
	where, args := tokenCondition([]string{"v.name", "v.driver", "v.iface", "v.last_state"}, tokens)
	q := `SELECT v.id, v.name, v.driver, v.iface, v.last_state, v.updated_at
		FROM vpn_networks v WHERE ` + where + ` ORDER BY v.name`
	if limit > 0 {
		q += ` LIMIT ` + strconvItoa(limit)
	}
	rows, err := d.Query(q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name, driver, iface, state string
		var updated sql.NullTime
		if err := rows.Scan(&id, &name, &driver, &iface, &state, &updated); err != nil {
			return err
		}
		key := FleetSearchVPN + "\x00" + strconvItoa64(id)
		b.addMeta(FleetSearchVPN, key, name, FleetSearchGroup{
			EntityID: strconvItoa64(id), TargetTab: "networks",
			Subtitle: driver + " · " + iface, StatusBadge: state,
		}, FleetSearchHost{Name: name, DataAge: fleetHostAge(updated)})
	}
	return rows.Err()
}

// fleetSearchCF searches Cloudflare accounts. Zones/tunnels/DNS records are
// fetched live from the Cloudflare API and are not persisted (ADR 0087), so
// only account name and account id are searchable here.
func (d *DB) fleetSearchCF(b *fleetGroupBuilder, tokens []string, limit int) error {
	where, args := tokenCondition([]string{"c.name", "c.account_id"}, tokens)
	q := `SELECT c.id, c.name, c.account_id, c.created_at FROM cloudflare_accounts c WHERE ` + where + ` ORDER BY c.name`
	if limit > 0 {
		q += ` LIMIT ` + strconvItoa(limit)
	}
	rows, err := d.Query(q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name, accountID string
		var created time.Time
		if err := rows.Scan(&id, &name, &accountID, &created); err != nil {
			return err
		}
		key := FleetSearchCF + "\x00" + strconvItoa64(id)
		b.addMeta(FleetSearchCF, key, name, FleetSearchGroup{
			EntityID: strconvItoa64(id), TargetTab: "cloudflare", Subtitle: accountID,
		}, FleetSearchHost{Name: name, DataAge: &created})
	}
	return rows.Err()
}

// fleetSearchGit searches cloned repositories. Branches are read live from the
// on-disk clone, so only the requested branch column is searchable (ADR 0087).
func (d *DB) fleetSearchGit(b *fleetGroupBuilder, tokens []string, limit int) error {
	where, args := tokenCondition([]string{"g.name", "g.url", "g.branch", "g.dir", "g.group_name"}, tokens)
	q := `SELECT g.id, g.name, g.url, g.branch, g.group_name, g.created_at FROM git_repos g WHERE ` + where + ` ORDER BY g.name`
	if limit > 0 {
		q += ` LIMIT ` + strconvItoa(limit)
	}
	rows, err := d.Query(q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name, url, branch, group string
		var created time.Time
		if err := rows.Scan(&id, &name, &url, &branch, &group, &created); err != nil {
			return err
		}
		key := FleetSearchGit + "\x00" + strconvItoa64(id)
		b.addMeta(FleetSearchGit, key, name, FleetSearchGroup{
			EntityID: strconvItoa64(id), TargetTab: "repos",
			Subtitle: url, StatusBadge: branch, GroupID: group,
		}, FleetSearchHost{Name: name, Group: group, DataAge: &created})
	}
	return rows.Err()
}

// fleetSearchAnsible searches Ansible playbooks (name, description, group, git url).
func (d *DB) fleetSearchAnsible(b *fleetGroupBuilder, tokens []string, limit int) error {
	where, args := tokenCondition([]string{"p.name", "p.description", "p.group_name", "p.git_url"}, tokens)
	q := `SELECT p.id, p.name, p.description, p.group_name, p.updated_at FROM ansible_playbooks p WHERE ` + where + ` ORDER BY p.name`
	if limit > 0 {
		q += ` LIMIT ` + strconvItoa(limit)
	}
	rows, err := d.Query(q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name, description, group string
		var updated time.Time
		if err := rows.Scan(&id, &name, &description, &group, &updated); err != nil {
			return err
		}
		key := FleetSearchAnsible + "\x00" + strconvItoa64(id)
		b.addMeta(FleetSearchAnsible, key, name, FleetSearchGroup{
			EntityID: strconvItoa64(id), TargetTab: "ansible",
			Subtitle: description, GroupID: group,
		}, FleetSearchHost{Name: name, Group: group, DataAge: &updated})
	}
	return rows.Err()
}

// fleetSearchTF searches Terraform workspaces.
func (d *DB) fleetSearchTF(b *fleetGroupBuilder, tokens []string, limit int) error {
	where, args := tokenCondition([]string{"w.name", "w.description", "w.environment", "w.group_name", "w.git_url"}, tokens)
	q := `SELECT w.id, w.name, w.description, w.environment, w.group_name, w.updated_at FROM tf_workspaces w WHERE ` + where + ` ORDER BY w.name`
	if limit > 0 {
		q += ` LIMIT ` + strconvItoa(limit)
	}
	rows, err := d.Query(q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name, description, environment, group string
		var updated time.Time
		if err := rows.Scan(&id, &name, &description, &environment, &group, &updated); err != nil {
			return err
		}
		key := FleetSearchTF + "\x00" + strconvItoa64(id)
		b.addMeta(FleetSearchTF, key, name, FleetSearchGroup{
			EntityID: strconvItoa64(id), TargetTab: "terraform",
			Subtitle: description, StatusBadge: environment, GroupID: group,
		}, FleetSearchHost{Name: name, Group: group, DataAge: &updated})
	}
	return rows.Err()
}

// fleetSearchPkgUpdates searches pending package updates. packages_json holds
// []PackageUpdate; we scan only hosts with a stored check and filter in Go.
func (d *DB) fleetSearchPkgUpdates(b *fleetGroupBuilder, tokens []string, limit int) error {
	rows, err := d.Query(`SELECT u.host_id, u.packages_json, u.checked_at, h.name, h.group_name
		FROM package_update_checks u JOIN hosts h ON h.id = u.host_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	found := 0
	for rows.Next() {
		var hostID int64
		var raw string
		var checked time.Time
		var hostName, groupName string
		if err := rows.Scan(&hostID, &raw, &checked, &hostName, &groupName); err != nil {
			return err
		}
		var pkgs []PackageUpdate
		if json.Unmarshal([]byte(raw), &pkgs) != nil {
			continue
		}
		for _, p := range pkgs {
			text := strings.Join([]string{p.Name, p.From, p.To}, " ")
			if !matchesTokens(text, tokens) {
				continue
			}
			label := p.Name
			if p.To != "" {
				label += " → " + p.To
			}
			key := FleetSearchPkg + "\x00" + strings.ToLower(label)
			badge := "pending"
			if p.Security {
				badge = "security"
			}
			b.addMeta(FleetSearchPkg, key, label, FleetSearchGroup{
				EntityID: strconvItoa64(hostID), TargetTab: "packages",
				Subtitle: p.From, StatusBadge: badge,
			}, FleetSearchHost{HostID: hostID, Name: hostName, Group: groupName, DataAge: &checked})
			found++
			if limit > 0 && found >= limit {
				return nil
			}
		}
	}
	return rows.Err()
}

// fleetSearchCVE searches CVE/OSV findings stored in docker_security_scans.
// findings_json holds []DockerImageVuln for kind='image'; the advisory id is
// the CVE/OSV id. Host-tagged image scans are included.
func (d *DB) fleetSearchCVE(b *fleetGroupBuilder, tokens []string, limit int) error {
	rows, err := d.Query(`SELECT d.host_id, d.subject, d.findings_json, d.scanned_at, h.name, h.group_name
		FROM docker_security_scans d JOIN hosts h ON h.id = d.host_id
		WHERE d.kind = 'image'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	found := 0
	for rows.Next() {
		var hostID int64
		var subject, raw string
		var scanned time.Time
		var hostName, groupName string
		if err := rows.Scan(&hostID, &subject, &raw, &scanned, &hostName, &groupName); err != nil {
			return err
		}
		var vulns []DockerImageVuln
		if json.Unmarshal([]byte(raw), &vulns) != nil {
			continue
		}
		for _, v := range vulns {
			text := strings.Join([]string{v.ID, v.Package, v.Title, v.Severity, v.Installed, v.Fixed}, " ")
			if !matchesTokens(text, tokens) {
				continue
			}
			key := FleetSearchCVE + "\x00" + strings.ToLower(v.ID)
			b.addMeta(FleetSearchCVE, key, v.ID, FleetSearchGroup{
				EntityID: strconvItoa64(hostID), TargetTab: "security",
				Subtitle: v.Package + " · " + subject, StatusBadge: v.Severity,
			}, FleetSearchHost{HostID: hostID, Name: hostName, Group: groupName, DataAge: &scanned})
			found++
			if limit > 0 && found >= limit {
				return nil
			}
		}
	}
	return rows.Err()
}
