package store

import (
	"database/sql"
	"encoding/json"
	"strconv"
	"time"
)

// fleetSearchPackages matches the stored package inventory (name and version).
func (d *DB) fleetSearchPackages(b *fleetGroupBuilder, tokens []string) error {
	return d.fleetSearchPackagesLimited(b, tokens, 0)
}

// fleetSearchPackagesLimited is fleetSearchPackages with a per-category LIMIT.
func (d *DB) fleetSearchPackagesLimited(b *fleetGroupBuilder, tokens []string, limit int) error {
	where, args := tokenCondition([]string{"p.name", "p.version"}, tokens)
	q := `SELECT p.host_id, p.name, p.version, p.last_scanned_at, h.name, h.group_name
		FROM host_packages p JOIN hosts h ON h.id = p.host_id
		WHERE ` + where + ` ORDER BY p.name, p.version, h.name`
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
			hostID    int64
			name      string
			version   string
			scannedAt time.Time
			hostName  string
			groupName string
		)
		if err := rows.Scan(&hostID, &name, &version, &scannedAt, &hostName, &groupName); err != nil {
			return err
		}
		age := scannedAt
		b.addKeyed(FleetSearchPackage, FleetSearchPackage+"\x00"+name+"\x00"+version, name, version, FleetSearchHost{
			HostID: hostID, Name: hostName, Group: groupName, Version: version, DataAge: &age,
		})
	}
	return rows.Err()
}

// fleetSearchContainers matches stored container records: the container listings
// of host_containers, drift rules of kind "container" and remembered Compose
// projects (project name and service names).
func (d *DB) fleetSearchContainers(b *fleetGroupBuilder, tokens []string) error {
	return d.fleetSearchContainersLimited(b, tokens, 0)
}

// fleetSearchContainersLimited is fleetSearchContainers with a per-category LIMIT.
func (d *DB) fleetSearchContainersLimited(b *fleetGroupBuilder, tokens []string, limit int) error {
	covered, groups, err := d.fleetSearchHostContainers(tokens, limit)
	if err != nil {
		return err
	}
	for _, g := range groups {
		for _, h := range g.hosts {
			b.addKeyed(FleetSearchContainer, g.key, g.name, "", h)
		}
	}
	if err := d.fleetSearchActualItems(b, tokens, covered, limit); err != nil {
		return err
	}
	if err := d.fleetSearchStacks(b, tokens, covered, limit); err != nil {
		return err
	}
	return nil
}

// fleetContainerGroup is one container group (by container name or by image)
// and the hosts that have it.
type fleetContainerGroup struct {
	key   string
	name  string
	hosts []FleetSearchHost
}

// fleetSearchHostContainers matches the stored container listings on container
// name and image. A container whose name matches is grouped by name; a container
// matched only by its image is grouped by image.
func (d *DB) fleetSearchHostContainers(tokens []string, limit int) (map[int64]map[string]bool, []fleetContainerGroup, error) {
	where, args := tokenCondition([]string{"c.name", "c.image"}, tokens)
	q := `SELECT c.host_id, c.name, c.image, c.state, c.seen_at, h.name, h.group_name
		FROM host_containers c JOIN hosts h ON h.id = c.host_id
		WHERE ` + where + ` ORDER BY c.name, h.name`
	if limit > 0 {
		q += ` LIMIT ` + strconvItoa(limit)
	}
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	covered := map[int64]map[string]bool{}
	groups := map[string]*fleetContainerGroup{}
	var order []string
	for rows.Next() {
		var (
			hostID    int64
			name      string
			image     string
			state     string
			seenAt    time.Time
			hostName  string
			groupName string
		)
		if err := rows.Scan(&hostID, &name, &image, &state, &seenAt, &hostName, &groupName); err != nil {
			return nil, nil, err
		}
		if covered[hostID] == nil {
			covered[hostID] = map[string]bool{}
		}
		covered[hostID][name] = true

		key, label := FleetSearchContainer+"\x00"+name, name
		if !matchesTokens(name, tokens) {
			key, label = FleetSearchContainer+"\x00image\x00"+image, image
		}
		g, ok := groups[key]
		if !ok {
			g = &fleetContainerGroup{key: key, name: label}
			groups[key] = g
			order = append(order, key)
		}
		age := seenAt
		container := FleetContainer{Name: name, State: state}
		idx := -1
		for i := range g.hosts {
			if g.hosts[i].HostID == hostID {
				idx = i
				break
			}
		}
		if idx >= 0 {
			g.hosts[idx].Containers = append(g.hosts[idx].Containers, container)
			continue
		}
		g.hosts = append(g.hosts, FleetSearchHost{
			HostID: hostID, Name: hostName, Group: groupName, DataAge: &age,
			Containers: []FleetContainer{container},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	out := make([]fleetContainerGroup, 0, len(order))
	for _, key := range order {
		out = append(out, *groups[key])
	}
	return covered, out, nil
}

// fleetSearchActualItems matches the drift rules of kind "container".
func (d *DB) fleetSearchActualItems(b *fleetGroupBuilder, tokens []string, covered map[int64]map[string]bool, limit int) error {
	where, args := tokenCondition([]string{"a.target"}, tokens)
	q := `SELECT a.host_id, a.target, a.current, a.updated_at, h.name, h.group_name
		FROM actual_items a JOIN hosts h ON h.id = a.host_id
		WHERE a.kind = 'container' AND ` + where + ` ORDER BY a.target, h.name`
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
			hostID    int64
			target    string
			current   string
			updatedAt time.Time
			hostName  string
			groupName string
		)
		if err := rows.Scan(&hostID, &target, &current, &updatedAt, &hostName, &groupName); err != nil {
			return err
		}
		if covered[hostID][target] {
			continue
		}
		age := updatedAt
		b.addKeyed(FleetSearchContainer, FleetSearchContainer+"\x00"+target, target, "", FleetSearchHost{
			HostID: hostID, Name: hostName, Group: groupName, Version: current, DataAge: &age,
		})
	}
	return rows.Err()
}

// fleetSearchStacks matches the remembered Compose projects. docker_stacks has no
// timestamp column, so the age falls back to the newest container check of that
// host and then to the last inspection.
func (d *DB) fleetSearchStacks(b *fleetGroupBuilder, tokens []string, covered map[int64]map[string]bool, limit int) error {
	rows, err := d.Query(`SELECT s.host_id, s.name, s.data, h.name, h.group_name, h.last_inspected
		FROM docker_stacks s JOIN hosts h ON h.id = s.host_id ORDER BY s.name, h.name`)
	if err != nil {
		return err
	}
	defer rows.Close()
	containerAges, err := d.containerAges()
	if err != nil {
		return err
	}
	for rows.Next() {
		var (
			hostID    int64
			stackName string
			data      string
			hostName  string
			groupName string
			lastInsp  sql.NullTime
		)
		if err := rows.Scan(&hostID, &stackName, &data, &hostName, &groupName, &lastInsp); err != nil {
			return err
		}
		st := struct {
			Services []string `json:"services"`
		}{}
		_ = json.Unmarshal([]byte(data), &st)
		items := append([]string{stackName}, st.Services...)
		var age *time.Time
		if t, ok := containerAges[hostID]; ok {
			age = &t
		} else if lastInsp.Valid {
			age = &lastInsp.Time
		}
		for _, item := range items {
			if item == "" || !matchesTokens(item, tokens) {
				continue
			}
			if covered[hostID][item] {
				continue
			}
			b.addKeyed(FleetSearchContainer, FleetSearchContainer+"\x00"+item, item, "", FleetSearchHost{
				HostID: hostID, Name: hostName, Group: groupName, Version: stackName, DataAge: age,
			})
		}
	}
	return rows.Err()
}

// containerAges returns the newest container check per host.
func (d *DB) containerAges() (map[int64]time.Time, error) {
	rows, err := d.Query(`SELECT host_id, updated_at FROM actual_items WHERE kind = 'container'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]time.Time{}
	for rows.Next() {
		var (
			hostID int64
			ts     time.Time
		)
		if err := rows.Scan(&hostID, &ts); err != nil {
			return nil, err
		}
		if cur, ok := out[hostID]; !ok || ts.After(cur) {
			out[hostID] = ts
		}
	}
	return out, rows.Err()
}

// fleetSearchDomainsLimited wraps fleetSearchDomains with a per-category LIMIT.
func (d *DB) fleetSearchDomainsLimited(b *fleetGroupBuilder, tokens []string, limit int) error {
	where, args := tokenCondition([]string{"d.domain"}, tokens)
	q := `SELECT d.host_id, d.domain, d.proxy, d.location, d.container, d.data, s.scanned_at, h.name, h.group_name
		FROM host_domains d JOIN hosts h ON h.id = d.host_id
		LEFT JOIN web_scans s ON s.host_id = d.host_id
		WHERE ` + where + ` ORDER BY d.domain, h.name`
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
			hostID    int64
			domain    string
			proxy     string
			location  string
			container string
			data      string
			scannedAt sql.NullTime
			hostName  string
			groupName string
		)
		if err := rows.Scan(&hostID, &domain, &proxy, &location, &container, &data, &scannedAt, &hostName, &groupName); err != nil {
			return err
		}
		b.addKeyed(FleetSearchDomain, FleetSearchDomain+"\x00"+domain, domain, "", FleetSearchHost{
			HostID: hostID, Name: hostName, Group: groupName, DataAge: fleetHostAge(scannedAt),
			Detail: domainDetail(proxy, location, container, data),
		})
	}
	return rows.Err()
}

// strconvItoa is a tiny local alias so the SQL builders stay readable.
func strconvItoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// strconvItoa64 converts an int64 to a decimal string for entity ids.
func strconvItoa64(n int64) string {
	return strconv.FormatInt(n, 10)
}
