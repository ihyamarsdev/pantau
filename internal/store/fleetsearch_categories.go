package store

import (
	"sort"
	"strings"
)

// Category constants for universal fleet search. Each maps to one prefix scope
// (folder:, host:, port:, ...) and one kind filter on GET /api/search.
const (
	FleetSearchAll       = "all"
	FleetSearchPackage   = "package"
	FleetSearchContainer = "container"
	FleetSearchDomain    = "domain"

	FleetSearchFolder   = "folder"
	FleetSearchHostKind = "host"
	FleetSearchPort     = "port"
	FleetSearchService  = "service"
	FleetSearchKube     = "kube"
	FleetSearchVPN      = "vpn"
	FleetSearchCF       = "cf"
	FleetSearchGit      = "git"
	FleetSearchAnsible  = "ansible"
	FleetSearchTF       = "tf"
	FleetSearchPkg      = "pkg"
	FleetSearchCVE      = "cve"
	FleetSearchSSHKey   = "sshkey"
	FleetSearchAccount  = "account"
)

// fleetCategories is the ordered list of categories used for the "all" search.
// Order is stable; ranking (exact > prefix > substring) is applied on top.
var fleetCategories = []string{
	FleetSearchFolder, FleetSearchHostKind, FleetSearchPackage, FleetSearchContainer,
	FleetSearchPort, FleetSearchService, FleetSearchDomain, FleetSearchKube,
	FleetSearchVPN, FleetSearchCF, FleetSearchGit, FleetSearchAnsible,
	FleetSearchTF, FleetSearchPkg, FleetSearchCVE, FleetSearchSSHKey, FleetSearchAccount,
}

// fleetKinds is the set of valid kind values accepted by the API and agent tool.
var fleetKinds = func() map[string]bool {
	m := map[string]bool{FleetSearchAll: true}
	for _, c := range fleetCategories {
		m[c] = true
	}
	return m
}()

// FleetPrefixScope maps query prefixes to their category.
var FleetPrefixScope = map[string]string{
	"folder:": folderPrefix, "group:": folderPrefix,
	"host:":      FleetSearchHostKind,
	"port:":      FleetSearchPort,
	"domain:":    FleetSearchDomain,
	"container:": FleetSearchContainer,
	"service:":   FleetSearchService,
	"kube:":      FleetSearchKube,
	"vpn:":       FleetSearchVPN,
	"cf:":        FleetSearchCF,
	"git:":       FleetSearchGit,
	"ansible:":   FleetSearchAnsible,
	"tf:":        FleetSearchTF,
	"terraform:": FleetSearchTF,
	"pkg:":       FleetSearchPkg,
	"cve:":       FleetSearchCVE,
	"sshkey:":    FleetSearchSSHKey,
	"account:":   FleetSearchAccount,
}

const folderPrefix = FleetSearchFolder

// FleetKindValid reports whether kind is a valid search category or "all".
func FleetKindValid(kind string) bool { return fleetKinds[kind] }

// FleetKinds returns the ordered list of searchable categories (without "all").
func FleetKinds() []string {
	out := make([]string, len(fleetCategories))
	copy(out, fleetCategories)
	return out
}

// fleetMatchRank ranks a match against the query for deterministic ordering.
// Higher is better: exact (3) > prefix (2) > substring (1) > none (0).
func fleetMatchRank(value, query string) int {
	v, q := strings.ToLower(strings.TrimSpace(value)), strings.ToLower(strings.TrimSpace(query))
	if v == q {
		return 3
	}
	if q != "" && strings.HasPrefix(v, q) {
		return 2
	}
	if q != "" && strings.Contains(v, q) {
		return 1
	}
	return 0
}

// fleetRankGroups sorts groups by best match rank (descending), then name, then kind.
// Within each group hosts are ordered by their own best rank, then name.
func fleetRankGroups(groups []FleetSearchGroup, query string) {
	for i := range groups {
		hosts := groups[i].Hosts
		sort.SliceStable(hosts, func(a, b int) bool {
			ra := max(fleetMatchRank(hosts[a].Name, query), fleetMatchRank(hosts[a].Detail, query))
			rb := max(fleetMatchRank(hosts[b].Name, query), fleetMatchRank(hosts[b].Detail, query))
			if ra != rb {
				return ra > rb
			}
			return hosts[a].Name < hosts[b].Name
		})
		groups[i].Hosts = hosts
	}
	sort.SliceStable(groups, func(i, j int) bool {
		ri := max(fleetMatchRank(groups[i].Name, query), fleetMatchRank(groups[i].Subtitle, query))
		rj := max(fleetMatchRank(groups[j].Name, query), fleetMatchRank(groups[j].Subtitle, query))
		if ri != rj {
			return ri > rj
		}
		if groups[i].Name != groups[j].Name {
			return groups[i].Name < groups[j].Name
		}
		return groups[i].Kind < groups[j].Kind
	})
}
