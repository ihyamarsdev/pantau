package store

import (
	"database/sql"
	"strings"
	"time"
)

// Fleet search over the data Pantau already stores. Nothing here talks to a host:
// every category is answered from SQLite with one bounded query (plus bounded
// parsing of JSON columns such as listening_ports).
//
// Not covered (never persisted): systemd units (listed live over SSH), OSV/CVE
// findings (computed live), Cloudflare zones/tunnels/DNS records (fetched live
// from the Cloudflare API), git branches (read from the on-disk clone), and
// host-group descriptions/tags (only hosts.group_name is stored). These are
// documented in ADR 0087.

// FleetSearchHost is one host that carries the matched item.
type FleetSearchHost struct {
	HostID  int64      `json:"host_id"`
	Name    string     `json:"name"`
	Group   string     `json:"group"`
	Version string     `json:"version,omitempty"`
	DataAge *time.Time `json:"data_age,omitempty"`
	// Detail is a short line describing the match (domains, ports, services).
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
	Kind      string            `json:"kind"` // category id, e.g. package | container | folder
	Name      string            `json:"name"`
	Version   string            `json:"version,omitempty"`
	HostCount int               `json:"host_count"`
	Hosts     []FleetSearchHost `json:"hosts"`
	// Structured deep-link metadata (ADR 0087). Older categories fill these too,
	// so the UI has one uniform navigation path for every result.
	Category    string `json:"category,omitempty"`     // same as kind, kept for clients
	EntityID    string `json:"entity_id,omitempty"`    // id of the entity (host id, cluster id, ...)
	GroupID     string `json:"group_id,omitempty"`     // host group name for folder results
	TargetTab   string `json:"target_tab,omitempty"`   // host workspace tab or side-nav section
	Subtitle    string `json:"subtitle,omitempty"`     // short second line under the name
	StatusBadge string `json:"status_badge,omitempty"` // status label, e.g. running, up, healthy
}

// FleetSearchResult is the answer of one fleet search.
type FleetSearchResult struct {
	Query                string             `json:"query"`
	Kind                 string             `json:"kind"`
	Scope                string             `json:"scope,omitempty"` // prefix scope actually applied, if any
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
		g = &FleetSearchGroup{Kind: kind, Category: kind, Name: name, Version: version, Hosts: []FleetSearchHost{}}
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

// addMeta attaches deep-link metadata to a group created earlier (or creates it).
func (b *fleetGroupBuilder) addMeta(kind, key, name string, meta FleetSearchGroup, host FleetSearchHost) {
	b.addKeyed(kind, key, name, "", host)
	g := b.groups[key]
	if meta.Category == "" {
		meta.Category = kind
	}
	g.Category = meta.Category
	g.EntityID = meta.EntityID
	g.GroupID = meta.GroupID
	g.TargetTab = meta.TargetTab
	g.Subtitle = meta.Subtitle
	g.StatusBadge = meta.StatusBadge
}

// list returns the groups in insertion order.
func (b *fleetGroupBuilder) list() []FleetSearchGroup {
	out := make([]FleetSearchGroup, 0, len(b.order))
	for _, k := range b.order {
		out = append(out, *b.groups[k])
	}
	return out
}

// parseFleetQuery splits an optional prefix scope from the query text. Both
// "folder:prod web" and "folder:prod" work: the prefix is attached to the
// first token with a colon. An unknown prefix is left as ordinary text.
func parseFleetQuery(query string) (scope string, tokens []string) {
	fields := strings.Fields(strings.ToLower(query))
	if len(fields) > 0 {
		if i := strings.IndexByte(fields[0], ':'); i > 0 {
			if s, ok := FleetPrefixScope[fields[0][:i+1]]; ok {
				scope = s
				rest := fields[0][i+1:]
				if rest != "" {
					fields = append([]string{rest}, fields[1:]...)
				} else {
					fields = fields[1:]
				}
			}
		}
	}
	return scope, fields
}

// SearchFleet searches the stored inventory. kind is one of the FleetSearch*
// constants; "all" searches every category. A query prefix (folder:, host:,
// port:, ...) narrows the search to one category. limit caps the number of
// returned groups per category (truncated reports that more matched).
func (d *DB) SearchFleet(query, kind string, limit int) (*FleetSearchResult, error) {
	if limit <= 0 {
		limit = 200
	}
	scope, tokens := parseFleetQuery(query)
	if scope != "" && (kind == "" || kind == FleetSearchAll) {
		kind = scope
	}
	if kind == "" {
		kind = FleetSearchAll
	}
	res := &FleetSearchResult{Query: query, Kind: kind, Scope: scope, Groups: []FleetSearchGroup{}}
	if len(tokens) == 0 {
		return res, nil
	}

	b := newFleetGroupBuilder()
	categories := fleetCategories
	if kind != FleetSearchAll {
		categories = []string{kind}
	}
	// Fetch one extra per category so truncation can be detected even when a
	// single category is selected and fills the whole limit.
	perCategory := limit + 1
	for _, cat := range categories {
		if err := d.searchFleetCategory(b, cat, tokens, perCategory); err != nil {
			return nil, err
		}
	}

	groups := b.list()
	fleetRankGroups(groups, strings.Join(tokens, " "))
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

// searchFleetCategory dispatches one category to its query function. Each
// function applies its own LIMIT so an "all" search never runs an unbounded
// query and no category can starve the others.
func (d *DB) searchFleetCategory(b *fleetGroupBuilder, category string, tokens []string, limit int) error {
	switch category {
	case FleetSearchFolder:
		return d.fleetSearchFolders(b, tokens, limit)
	case FleetSearchHostKind:
		return d.fleetSearchHosts(b, tokens, limit)
	case FleetSearchPackage:
		return d.fleetSearchPackagesLimited(b, tokens, limit)
	case FleetSearchContainer:
		return d.fleetSearchContainersLimited(b, tokens, limit)
	case FleetSearchPort:
		return d.fleetSearchPorts(b, tokens, limit)
	case FleetSearchService:
		return d.fleetSearchServices(b, tokens, limit)
	case FleetSearchDomain:
		return d.fleetSearchDomainsLimited(b, tokens, limit)
	case FleetSearchKube:
		return d.fleetSearchKube(b, tokens, limit)
	case FleetSearchVPN:
		return d.fleetSearchVPN(b, tokens, limit)
	case FleetSearchCF:
		return d.fleetSearchCF(b, tokens, limit)
	case FleetSearchGit:
		return d.fleetSearchGit(b, tokens, limit)
	case FleetSearchAnsible:
		return d.fleetSearchAnsible(b, tokens, limit)
	case FleetSearchTF:
		return d.fleetSearchTF(b, tokens, limit)
	case FleetSearchPkg:
		return d.fleetSearchPkgUpdates(b, tokens, limit)
	case FleetSearchCVE:
		return d.fleetSearchCVE(b, tokens, limit)
	case FleetSearchSSHKey:
		return d.fleetSearchSSHKeys(b, tokens, limit)
	case FleetSearchAccount:
		return d.fleetSearchAccounts(b, tokens, limit)
	}
	return nil
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

// fleetHostAge converts a nullable scan time into a data-age pointer.
func fleetHostAge(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time.UTC()
	return &v
}
