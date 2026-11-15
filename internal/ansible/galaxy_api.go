package ansible

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The Galaxy catalogue answers what the ansible-galaxy CLI cannot: keyword search and the newest version of a
// collection or role. Installs still go through ansible-galaxy (library.go); nothing in this file writes to disk.

// DefaultGalaxyURL is the public Ansible Galaxy server, the one ansible-galaxy uses by default.
const DefaultGalaxyURL = "https://galaxy.ansible.com"

const (
	// galaxyHTTPTimeout bounds one request; galaxyCheckTimeout bounds one update check or search end to end.
	galaxyHTTPTimeout      = 15 * time.Second
	galaxyCheckTimeout     = 60 * time.Second
	galaxyWorkers          = 4 // concurrent requests per check or search
	galaxyBodyCap          = 4 << 20
	galaxyVersionsPageSize = 100
	// galaxyVersionPages caps the paging of one role's versions; it only stops a misbehaving server.
	galaxyVersionPages = 50
	galaxySearchLimit  = 10 // hits per kind
)

// ErrNotOnGalaxy reports that Galaxy has no collection or role under the requested name.
var ErrNotOnGalaxy = errors.New("not on Ansible Galaxy")

// Update statuses of an installed item (LibraryUpdate.Status): an update exists, the item is current, the versions
// cannot be compared, Galaxy has no such item, or the lookup failed (LibraryUpdate.Error says why).
const (
	statusUpdate      = "update"
	statusCurrent     = "current"
	statusUnknown     = "unknown"
	statusNotOnGalaxy = "not_on_galaxy"
	statusError       = "error"
)

// GalaxyClient reads the Ansible Galaxy catalogue. It is safe for concurrent use.
type GalaxyClient struct {
	BaseURL string
	HTTP    *http.Client
}

// NewGalaxyClient returns a client for baseURL (tests point it at a fake server).
func NewGalaxyClient(baseURL string) *GalaxyClient {
	return &GalaxyClient{BaseURL: strings.TrimRight(baseURL, "/"), HTTP: &http.Client{Timeout: galaxyHTTPTimeout}}
}

// LibraryUpdate is the update check of one installed collection or role.
type LibraryUpdate struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Installed string `json:"installed"`
	Latest    string `json:"latest"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

// GalaxyHit is one collection or role matched by a search, with its newest version.
type GalaxyHit struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

// GalaxySearch is a keyword search: up to galaxySearchLimit hits per kind and the totals Galaxy reports.
type GalaxySearch struct {
	Collections      []GalaxyHit `json:"collections"`
	Roles            []GalaxyHit `json:"roles"`
	CollectionsTotal int         `json:"collections_total"`
	RolesTotal       int         `json:"roles_total"`
}

// CheckUpdates compares every installed collection and role with its newest version on Galaxy. Lookups run a few
// at a time under one deadline; a failed lookup is reported on its own item and never hides the others.
func (c *GalaxyClient) CheckUpdates(ctx context.Context, lib Library) []LibraryUpdate {
	items := make([]LibraryUpdate, 0, len(lib.Collections)+len(lib.Roles))
	for _, it := range lib.Collections {
		items = append(items, LibraryUpdate{Kind: "collection", Name: it.Name, Installed: it.Version})
	}
	for _, it := range lib.Roles {
		items = append(items, LibraryUpdate{Kind: "role", Name: it.Name, Installed: it.Version})
	}
	ctx, cancel := context.WithTimeout(ctx, galaxyCheckTimeout)
	defer cancel()
	runBounded(len(items), func(i int) {
		u := &items[i]
		latest, err := c.latest(ctx, u.Kind, u.Name)
		switch {
		case errors.Is(err, ErrNotOnGalaxy):
			u.Status = statusNotOnGalaxy
		case err != nil:
			u.Status, u.Error = statusError, err.Error()
		default:
			u.Latest, u.Status = latest, updateStatus(u.Installed, latest)
		}
	})
	return items
}

// Search runs Galaxy's keyword search for collections and roles together. Each kind returns its best
// galaxySearchLimit matches with their newest versions; the totals are what Galaxy reports overall.
func (c *GalaxyClient) Search(ctx context.Context, q string) (GalaxySearch, error) {
	ctx, cancel := context.WithTimeout(ctx, galaxyCheckTimeout)
	defer cancel()
	var res GalaxySearch
	var collErr, roleErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		res.Collections, res.CollectionsTotal, collErr = c.searchCollections(ctx, q)
	}()
	go func() {
		defer wg.Done()
		res.Roles, res.RolesTotal, roleErr = c.searchRoles(ctx, q)
	}()
	wg.Wait()
	if err := errors.Join(collErr, roleErr); err != nil {
		return GalaxySearch{}, err
	}
	return res, nil
}

func (c *GalaxyClient) searchCollections(ctx context.Context, q string) ([]GalaxyHit, int, error) {
	var doc struct {
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
		Data []struct {
			Version struct {
				Namespace   string `json:"namespace"`
				Name        string `json:"name"`
				Version     string `json:"version"`
				Description string `json:"description"`
			} `json:"collection_version"`
		} `json:"data"`
	}
	query := url.Values{"keywords": {q}, "is_highest": {"true"}, "limit": {strconv.Itoa(galaxySearchLimit)}}
	if err := c.getJSON(ctx, "/api/v3/plugin/ansible/search/collection-versions/", query, &doc); err != nil {
		return nil, 0, err
	}
	hits := make([]GalaxyHit, 0, len(doc.Data))
	for _, d := range doc.Data {
		v := d.Version
		hits = append(hits, GalaxyHit{Kind: "collection", Name: v.Namespace + "." + v.Name, Version: v.Version, Description: v.Description})
	}
	return hits, doc.Meta.Count, nil
}

func (c *GalaxyClient) searchRoles(ctx context.Context, q string) ([]GalaxyHit, int, error) {
	var doc struct {
		Count   int `json:"count"`
		Results []struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			Username    string `json:"username"`
			Description string `json:"description"`
		} `json:"results"`
	}
	query := url.Values{"keywords": {q}, "page_size": {strconv.Itoa(galaxySearchLimit)}}
	if err := c.getJSON(ctx, "/api/v1/search/roles/", query, &doc); err != nil {
		return nil, 0, err
	}
	hits := make([]GalaxyHit, len(doc.Results))
	errs := make([]error, len(doc.Results))
	runBounded(len(doc.Results), func(i int) {
		r := doc.Results[i]
		names, err := c.roleVersions(ctx, r.ID)
		hits[i] = GalaxyHit{Kind: "role", Name: r.Username + "." + r.Name, Version: newestGalaxyVersion(names), Description: r.Description}
		errs[i] = err
	})
	for _, err := range errs {
		if err != nil {
			return nil, 0, err
		}
	}
	return hits, doc.Count, nil
}

// latest returns the newest version of a collection or role on Galaxy ("" when none is comparable).
func (c *GalaxyClient) latest(ctx context.Context, kind, name string) (string, error) {
	if kind == "collection" {
		return c.latestCollection(ctx, name)
	}
	id, err := c.roleID(ctx, name)
	if err != nil {
		return "", err
	}
	names, err := c.roleVersions(ctx, id)
	if err != nil {
		return "", err
	}
	return newestGalaxyVersion(names), nil
}

// latestCollection returns the highest version of "namespace.name", as the collection page reports it.
func (c *GalaxyClient) latestCollection(ctx context.Context, name string) (string, error) {
	ns, coll, _ := strings.Cut(name, ".")
	var doc struct {
		Highest struct {
			Version string `json:"version"`
		} `json:"highest_version"`
	}
	path := "/api/v3/plugin/ansible/content/published/collections/index/" + url.PathEscape(ns) + "/" + url.PathEscape(coll) + "/"
	if err := c.getJSON(ctx, path, nil, &doc); err != nil {
		return "", err
	}
	return doc.Highest.Version, nil
}

// roleID finds the Galaxy role that ansible-galaxy installs as "namespace.name". The name filter is not trusted
// to be exact, so the match is checked on both fields.
func (c *GalaxyClient) roleID(ctx context.Context, full string) (int, error) {
	ns, name, ok := strings.Cut(full, ".")
	if !ok {
		return 0, ErrNotOnGalaxy
	}
	var page struct {
		Results []struct {
			ID       int    `json:"id"`
			Username string `json:"username"`
			Name     string `json:"name"`
		} `json:"results"`
	}
	query := url.Values{"namespace": {ns}, "name": {name}, "page_size": {"10"}}
	if err := c.getJSON(ctx, "/api/v1/roles/", query, &page); err != nil {
		return 0, err
	}
	for _, r := range page.Results {
		if strings.EqualFold(r.Username, ns) && strings.EqualFold(r.Name, name) {
			return r.ID, nil
		}
	}
	return 0, ErrNotOnGalaxy
}

// roleVersions lists every version Galaxy has for a role. The role's own summary carries only some of them.
func (c *GalaxyClient) roleVersions(ctx context.Context, id int) ([]string, error) {
	var names []string
	for page := 1; page <= galaxyVersionPages; page++ {
		var doc struct {
			Next    *string `json:"next"`
			Results []struct {
				Name string `json:"name"`
			} `json:"results"`
		}
		query := url.Values{"page": {strconv.Itoa(page)}, "page_size": {strconv.Itoa(galaxyVersionsPageSize)}}
		if err := c.getJSON(ctx, "/api/v1/roles/"+strconv.Itoa(id)+"/versions/", query, &doc); err != nil {
			return nil, err
		}
		for _, v := range doc.Results {
			names = append(names, v.Name)
		}
		if doc.Next == nil || len(doc.Results) == 0 {
			break
		}
	}
	return names, nil
}

// getJSON reads one catalogue endpoint into out. A 404 means the item does not exist on Galaxy.
func (c *GalaxyClient) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	u := c.BaseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("galaxy: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotOnGalaxy
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("galaxy: %s answered HTTP %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, galaxyBodyCap)).Decode(out); err != nil {
		return fmt.Errorf("galaxy: %s: %w", path, err)
	}
	return nil
}

// runBounded calls fn(i) for every i in [0, n), with at most galaxyWorkers calls running at once.
func runBounded(n int, fn func(i int)) {
	sem := make(chan struct{}, galaxyWorkers)
	var wg sync.WaitGroup
	for i := range n {
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			fn(i)
		}()
	}
	wg.Wait()
}

// updateStatus compares the installed version with the newest one on Galaxy. A version that cannot be compared is
// "unknown", never an update: a role installed from git records its branch name, which must not look older.
func updateStatus(installed, latest string) string {
	iv, okInstalled := parseGalaxyVersion(installed)
	lv, okLatest := parseGalaxyVersion(latest)
	switch {
	case !okInstalled || !okLatest:
		return statusUnknown
	case compareGalaxyVersions(lv, iv) > 0:
		return statusUpdate
	default:
		return statusCurrent
	}
}

// galaxyVersion is a dotted version: a numeric core and an optional pre-release tag ("2.0.0-rc1").
type galaxyVersion struct {
	core []int
	pre  string
}

// parseGalaxyVersion accepts "1.2.3", "v1.2" and "1.0.0-rc1+build"; anything else is not a version.
func parseGalaxyVersion(s string) (galaxyVersion, bool) {
	var v galaxyVersion
	s = strings.TrimLeft(s, "vV")
	if i := strings.IndexByte(s, '+'); i >= 0 { // build metadata does not affect precedence
		s = s[:i]
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		s, v.pre = s[:i], s[i+1:]
	}
	for _, part := range strings.Split(s, ".") {
		n, err := strconv.Atoi(part)
		if err != nil {
			return galaxyVersion{}, false
		}
		v.core = append(v.core, n)
	}
	return v, true
}

// compareGalaxyVersions orders two versions: -1, 0 or 1. A missing core part counts as zero (1.2 == 1.2.0), a release
// is newer than its own pre-releases, and pre-release tags compare as text.
func compareGalaxyVersions(a, b galaxyVersion) int {
	for i := range max(len(a.core), len(b.core)) {
		var x, y int
		if i < len(a.core) {
			x = a.core[i]
		}
		if i < len(b.core) {
			y = b.core[i]
		}
		if c := cmp.Compare(x, y); c != 0 {
			return c
		}
	}
	switch {
	case a.pre == b.pre:
		return 0
	case a.pre == "":
		return 1
	case b.pre == "":
		return -1
	}
	return cmp.Compare(a.pre, b.pre)
}

// newestGalaxyVersion returns the highest release among names, or "" when none is a version. Pre-releases are
// skipped, as ansible-galaxy skips them for collections unless --pre is given.
func newestGalaxyVersion(names []string) string {
	best := ""
	var bestV galaxyVersion
	for _, n := range names {
		v, ok := parseGalaxyVersion(n)
		if !ok || v.pre != "" {
			continue
		}
		if best == "" || compareGalaxyVersions(v, bestV) > 0 {
			best, bestV = n, v
		}
	}
	return best
}
