package store

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

// Fleet search over the data Pantau already stores. Nothing here talks to a host:
// packages come from host_packages (the last agentless scan), containers from
// host_containers (the last container listing of every inspection), actual_items
// (drift rules of kind "container") and docker_stacks (Compose projects seen
// when the Docker tab was opened).
//
// Not covered (never persisted, so never searchable): hosts that were never
// inspected and containers that no listing ever saw.

const (
	FleetSearchAll       = "all"
	FleetSearchPackage   = "package"
	FleetSearchContainer = "container"
	FleetSearchDomain    = "domain"
)

// FleetSearchHost is one host that carries the matched item.
type FleetSearchHost struct {
	HostID  int64      `json:"host_id"`
	Name    string     `json:"name"`
	Group   string     `json:"group"`
	Version string     `json:"version,omitempty"`
	DataAge *time.Time `json:"data_age,omitempty"` // when this host's data was collected
	// Detail is a short line describing the match (domains only).
	Detail string `json:"detail,omitempty"`
	// Containers of this host inside an image group (one entry per container).
	Containers []FleetContainer `json:"containers,omitempty"`
}

// FleetContainer is one container of a host inside a container group.
type FleetContainer struct {
	Name  string `json:"name"`
	State string `json:"state,omitempty"`
}

// FleetSearchGroup is one matched item and the hosts that have it.
type FleetSearchGroup struct {
	Kind      string            `json:"kind"` // package | container
	Name      string            `json:"name"`
	Version   string            `json:"version,omitempty"`
	HostCount int               `json:"host_count"`
	Hosts     []FleetSearchHost `json:"hosts"`
}

// FleetSearchResult is the answer of one fleet search.
type FleetSearchResult struct {
	Query                string             `json:"query"`
	Kind                 string             `json:"kind"`
	Groups               []FleetSearchGroup `json:"groups"`
	Truncated            bool               `json:"truncated"`
	TotalHosts           int                `json:"total_hosts"`
	HostsWithoutPackages int                `json:"hosts_without_packages"`
	HostsWithoutDocker   int                `json:"hosts_without_docker"`
	HostsWithoutDomains  int                `json:"hosts_without_domains"`
}

// likePattern escapes the LIKE wildcards so a query matches them literally.
func likePattern(s string) string {
	r := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_")
	return "%" + r.Replace(s) + "%"
}

// tokenCondition builds an SQL condition matching every token against the given
// columns (each token must match at least one of them).
func tokenCondition(cols []string, tokens []string) (string, []any) {
	parts := make([]string, 0, len(tokens))
	args := make([]any, 0, len(tokens)*len(cols))
	for _, tok := range tokens {
		one := make([]string, 0, len(cols))
		for _, c := range cols {
			one = append(one, "LOWER("+c+") LIKE ? ESCAPE '\\'")
			args = append(args, likePattern(tok))
		}
		parts = append(parts, "("+strings.Join(one, " OR ")+")")
	}
	return strings.Join(parts, " AND "), args
}

// fleetGroupBuilder collects hosts per matched item.
type fleetGroupBuilder struct {
	order  []string
	groups map[string]*FleetSearchGroup
	seen   map[string]map[int64]bool
}

func newFleetGroupBuilder() *fleetGroupBuilder {
	return &fleetGroupBuilder{groups: map[string]*FleetSearchGroup{}, seen: map[string]map[int64]bool{}}
}

// add records a host under the item, keeping the first version and age seen.
func (b *fleetGroupBuilder) add(kind, name, version string, host FleetSearchHost) {
	b.addKeyed(kind, kind+"\x00"+name+"\x00"+version, name, version, host)
}

// addKeyed records a host under a group with an explicit key. Containers use it
// to group by name alone: one "nginx" group must list every host running it.
func (b *fleetGroupBuilder) addKeyed(kind, key, name, version string, host FleetSearchHost) {
	g, ok := b.groups[key]
	if !ok {
		g = &FleetSearchGroup{Kind: kind, Name: name, Version: version, Hosts: []FleetSearchHost{}}
		b.groups[key] = g
		b.seen[key] = map[int64]bool{}
		b.order = append(b.order, key)
	}
	if b.seen[key][host.HostID] {
		return
	}
	b.seen[key][host.HostID] = true
	g.Hosts = append(g.Hosts, host)
	g.HostCount = len(g.Hosts)
}

// list returns the groups in insertion order.
func (b *fleetGroupBuilder) list() []FleetSearchGroup {
	out := make([]FleetSearchGroup, 0, len(b.order))
	for _, k := range b.order {
		out = append(out, *b.groups[k])
	}
	return out
}

// SearchFleet searches the stored inventory. kind is one of
// all|package|container|domain, limit caps the number of returned groups
// (truncated reports that more matched).
func (d *DB) SearchFleet(query, kind string, limit int) (*FleetSearchResult, error) {
	if limit <= 0 {
		limit = 200
	}
	tokens := strings.Fields(strings.ToLower(query))
	res := &FleetSearchResult{Query: query, Kind: kind, Groups: []FleetSearchGroup{}}
	if len(tokens) == 0 {
		return res, nil
	}

	b := newFleetGroupBuilder()
	switch kind {
	case FleetSearchPackage:
		if err := d.fleetSearchPackages(b, tokens); err != nil {
			return nil, err
		}
	case FleetSearchContainer:
		if err := d.fleetSearchContainers(b, tokens); err != nil {
			return nil, err
		}
	case FleetSearchDomain:
		if err := d.fleetSearchDomains(b, tokens); err != nil {
			return nil, err
		}
	default:
		if err := d.fleetSearchPackages(b, tokens); err != nil {
			return nil, err
		}
		if err := d.fleetSearchContainers(b, tokens); err != nil {
			return nil, err
		}
		if err := d.fleetSearchDomains(b, tokens); err != nil {
			return nil, err
		}
	}

	groups := b.list()
	if len(groups) > limit {
		groups = groups[:limit]
		res.Truncated = true
	}
	res.Groups = groups

	total, err := d.countHosts()
	if err != nil {
		return nil, err
	}
	res.TotalHosts = total
	res.HostsWithoutPackages, err = d.countHostsWithout(`(SELECT 1 FROM host_packages p WHERE p.host_id = h.id)`)
	if err != nil {
		return nil, err
	}
	res.HostsWithoutDocker, err = d.countHostsWithout(`(SELECT 1 FROM host_containers c WHERE c.host_id = h.id)
		OR (SELECT 1 FROM docker_stacks s WHERE s.host_id = h.id)
		OR (SELECT 1 FROM actual_items a WHERE a.host_id = h.id AND a.kind = 'container')`)
	if err != nil {
		return nil, err
	}
	res.HostsWithoutDomains, err = d.countHostsWithout(`(SELECT 1 FROM host_domains d WHERE d.host_id = h.id)`)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// fleetSearchPackages matches the stored package inventory (name and version).
func (d *DB) fleetSearchPackages(b *fleetGroupBuilder, tokens []string) error {
	where, args := tokenCondition([]string{"p.name", "p.version"}, tokens)
	rows, err := d.Query(`SELECT p.host_id, p.name, p.version, p.last_scanned_at, h.name, h.group_name
		FROM host_packages p JOIN hosts h ON h.id = p.host_id
		WHERE `+where+` ORDER BY p.name, p.version, h.name`, args...)
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
		b.add(FleetSearchPackage, name, version, FleetSearchHost{
			HostID: hostID, Name: hostName, Group: groupName, Version: version, DataAge: &age,
		})
	}
	return rows.Err()
}

// fleetSearchContainers matches stored container records: the container listings
// of host_containers, drift rules of kind "container" and remembered Compose
// projects (project name and service names).
//
// A container that host_containers already covers for a host is not listed
// twice: its group carries the container name and its state.
func (d *DB) fleetSearchContainers(b *fleetGroupBuilder, tokens []string) error {
	covered, groups, err := d.fleetSearchHostContainers(tokens)
	if err != nil {
		return err
	}
	// The listing groups come first: they carry the container name and state, and
	// the builder keeps the first host entry of a group.
	for _, g := range groups {
		for _, h := range g.hosts {
			b.addKeyed(FleetSearchContainer, g.key, g.name, "", h)
		}
	}
	if err := d.fleetSearchActualItems(b, tokens, covered); err != nil {
		return err
	}
	if err := d.fleetSearchStacks(b, tokens, covered); err != nil {
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
// name and image. A container whose name matches is grouped by name (like the
// other container sources); a container matched only by its image is grouped by
// image, so "which hosts run postgres:13?" lists the image with its containers.
// The covered map tells which container names each host already contributes, so
// the other container sources do not list them again.
func (d *DB) fleetSearchHostContainers(tokens []string) (map[int64]map[string]bool, []fleetContainerGroup, error) {
	where, args := tokenCondition([]string{"c.name", "c.image"}, tokens)
	rows, err := d.Query(`SELECT c.host_id, c.name, c.image, c.state, c.seen_at, h.name, h.group_name
		FROM host_containers c JOIN hosts h ON h.id = c.host_id
		WHERE `+where+` ORDER BY c.name, h.name`, args...)
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
func (d *DB) fleetSearchActualItems(b *fleetGroupBuilder, tokens []string, covered map[int64]map[string]bool) error {
	where, args := tokenCondition([]string{"a.target"}, tokens)
	rows, err := d.Query(`SELECT a.host_id, a.target, a.current, a.updated_at, h.name, h.group_name
		FROM actual_items a JOIN hosts h ON h.id = a.host_id
		WHERE a.kind = 'container' AND `+where+` ORDER BY a.target, h.name`, args...)
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
			continue // host_containers already lists this container
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
func (d *DB) fleetSearchStacks(b *fleetGroupBuilder, tokens []string, covered map[int64]map[string]bool) error {
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
				continue // host_containers already lists this container
			}
			// A Compose service is only unique within its project, so the project
			// name travels with the host as its version.
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

// matchesTokens reports whether the text contains every token (case-insensitive).
func matchesTokens(text string, tokens []string) bool {
	low := strings.ToLower(text)
	for _, tok := range tokens {
		if !strings.Contains(low, tok) {
			return false
		}
	}
	return true
}

// countHosts returns the number of known hosts.
func (d *DB) countHosts() (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM hosts`).Scan(&n)
	return n, err
}

// countHostsWithout counts the hosts that have no row matching the given EXISTS condition.
func (d *DB) countHostsWithout(exists string) (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM hosts h WHERE NOT EXISTS ` + exists).Scan(&n)
	return n, err
}
