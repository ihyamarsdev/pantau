// Package osv checks the packages installed on a host against the OSV vulnerability database
// (api.osv.dev) and the Arch Linux Security Tracker (security.archlinux.org).
//
// Everything happens on the Pantau server: the host is never asked about vulnerabilities. Only the
// package names and versions already stored in the Package Inventory leave the server, and only when
// the administrator turns the feature on. Nothing is written to the database: results are computed
// per request and the vulnerability details are cached in memory for a day.
package osv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultBaseURL is the OSV API root.
	DefaultBaseURL = "https://api.osv.dev"
	// DefaultArchURL is the Arch Linux Security Tracker feed (all issues).
	DefaultArchURL = "https://security.archlinux.org/issues/all.json"

	batchLimit  = 1000 // the OSV querybatch endpoint accepts at most 1000 queries per request
	detailCap   = 4 << 20
	batchCap    = 32 << 20
	archCap     = 32 << 20
	httpTimeout = 30 * time.Second
	cacheTTL    = 24 * time.Hour
	detailMax   = 5000
	maxDetails  = 2000 // details fetched per request (coverage beyond this is reported as a note)
	fetchers    = 8    // bounded concurrency when fetching vulnerability details
)

var cveRe = regexp.MustCompile(`^CVE-[0-9]{4}-[0-9]{4,}$`)

// Package is one installed package to look up in the feed.
type Package struct {
	Name    string // OSV package name: source package (dpkg), origin package (apk), plain name (pacman)
	Version string // installed version
}

// Finding is one known vulnerability affecting an installed package.
type Finding struct {
	Package          string   `json:"package"`
	InstalledVersion string   `json:"installed_version"`
	FixedVersion     string   `json:"fixed_version,omitempty"`
	IDs              []string `json:"ids"`
	CVEs             []string `json:"cves"`
	Severity         string   `json:"severity"`
	Summary          string   `json:"summary"`
}

// HasFix reports whether the feed knows a version that fixes the vulnerability.
func (f Finding) HasFix() bool { return f.FixedVersion != "" }

// Result is the outcome of one check.
type Result struct {
	Source          string    // SourceOSV or SourceArch
	Ecosystem       string    // OSV ecosystem string queried ("Debian:12", "Alpine:v3.18", "Arch")
	CheckedPackages int       // packages sent to the feed
	ScannedAt       time.Time `json:"scanned_at"`
	Findings        []Finding `json:"vulns"`
	Notes           []string  `json:"notes"`
}

// Client queries the public vulnerability feeds. It is safe for concurrent use.
type Client struct {
	BaseURL string
	ArchURL string
	HTTP    *http.Client

	mu      sync.Mutex
	details map[string]cachedDetail
	arch    *archFeed
}

type cachedDetail struct {
	detail    vulnDetail
	fetchedAt time.Time
}

// New returns a client for the given feed URLs (tests point them at a fake).
func New(baseURL, archURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		ArchURL: archURL,
		HTTP:    &http.Client{Timeout: httpTimeout},
		details: map[string]cachedDetail{},
	}
}

// Check looks up the installed packages in the feed that serves the ecosystem.
// A feed failure is returned as an error, never as an empty result set.
func (c *Client) Check(ctx context.Context, eco Ecosystem, pkgs []Package) (Result, error) {
	res := Result{Source: eco.Source, Ecosystem: eco.Name, ScannedAt: time.Now().UTC(), Findings: []Finding{}}
	if len(pkgs) == 0 {
		res.Notes = append(res.Notes, "the package inventory is empty")
		return res, nil
	}
	res.CheckedPackages = len(pkgs)

	if eco.Source == SourceArch {
		findings, notes, err := c.checkArch(ctx, pkgs)
		res.Findings, res.Notes = findings, notes
		return res, err
	}
	findings, notes, err := c.checkOSV(ctx, eco.Name, pkgs)
	res.Findings, res.Notes = findings, notes
	return res, err
}

// ---------------------------------------------------------------------------
// OSV

type osvQuery struct {
	Package struct {
		Name      string `json:"name"`
		Ecosystem string `json:"ecosystem"`
	} `json:"package"`
	Version string `json:"version"`
}

type osvBatchResponse struct {
	Results []struct {
		Vulns []struct {
			ID string `json:"id"`
		} `json:"vulns"`
	} `json:"results"`
}

type vulnDetail struct {
	ID               string          `json:"id"`
	Summary          string          `json:"summary"`
	Aliases          []string        `json:"aliases"`
	Upstream         []string        `json:"upstream"`
	Severity         []severityEntry `json:"severity"`
	DatabaseSpecific map[string]any  `json:"database_specific"`
	Affected         []struct {
		Package struct {
			Name      string `json:"name"`
			Ecosystem string `json:"ecosystem"`
		} `json:"package"`
		Ranges []struct {
			Type   string `json:"type"`
			Events []struct {
				Introduced   string `json:"introduced"`
				Fixed        string `json:"fixed"`
				LastAffected string `json:"last_affected"`
			} `json:"events"`
		} `json:"ranges"`
	} `json:"affected"`
}

type severityEntry struct {
	Type  string `json:"type"`
	Score string `json:"score"`
}

// checkOSV queries the OSV batch endpoint for the packages and enriches the hits with the
// vulnerability details (severity, summary, fix version).
func (c *Client) checkOSV(ctx context.Context, ecosystem string, pkgs []Package) ([]Finding, []string, error) {
	ids := make([][]string, len(pkgs))
	unique := map[string]bool{}

	for start := 0; start < len(pkgs); start += batchLimit {
		end := start + batchLimit
		if end > len(pkgs) {
			end = len(pkgs)
		}
		chunk := pkgs[start:end]
		queries := make([]osvQuery, 0, len(chunk))
		for _, p := range chunk {
			var q osvQuery
			q.Package.Name = p.Name
			q.Package.Ecosystem = ecosystem
			q.Version = p.Version
			queries = append(queries, q)
		}
		body, err := json.Marshal(map[string]any{"queries": queries})
		if err != nil {
			return nil, nil, fmt.Errorf("build OSV query: %w", err)
		}
		var out osvBatchResponse
		if err := c.postJSON(ctx, c.BaseURL+"/v1/querybatch", body, batchCap, &out); err != nil {
			return nil, nil, fmt.Errorf("OSV query failed: %w", err)
		}
		if len(out.Results) != len(queries) {
			return nil, nil, fmt.Errorf("OSV query failed: got %d results for %d queries", len(out.Results), len(queries))
		}
		for i, res := range out.Results {
			for _, v := range res.Vulns {
				if v.ID == "" {
					continue
				}
				ids[start+i] = append(ids[start+i], v.ID)
				unique[v.ID] = true
			}
		}
	}

	var notes []string
	all := make([]string, 0, len(unique))
	for id := range unique {
		all = append(all, id)
	}
	sort.Strings(all)
	if len(all) > maxDetails {
		notes = append(notes, fmt.Sprintf("coverage limited: %d advisories were found but only the first %d were inspected", len(all), maxDetails))
		all = all[:maxDetails]
	}
	details, err := c.fetchDetails(ctx, all)
	if err != nil {
		return nil, nil, err
	}

	var findings []Finding
	for i, p := range pkgs {
		for _, id := range ids[i] {
			d, ok := details[id]
			if !ok {
				continue
			}
			f := Finding{
				Package:          p.Name,
				InstalledVersion: p.Version,
				FixedVersion:     fixedVersion(d, p.Name, ecosystem),
				IDs:              sortedUnique(append([]string{d.ID}, d.Aliases...)),
				CVEs:             cveIDs(d),
				Severity:         severityOf(d),
				Summary:          strings.TrimSpace(d.Summary),
			}
			findings = append(findings, f)
		}
	}
	return findings, notes, nil
}

// fetchDetails loads the vulnerability records (bounded concurrency, cached for a day).
func (c *Client) fetchDetails(ctx context.Context, ids []string) (map[string]vulnDetail, error) {
	out := make(map[string]vulnDetail, len(ids))
	var mu sync.Mutex
	sem := make(chan struct{}, fetchers)
	var wg sync.WaitGroup
	var firstErr error

	for _, id := range ids {
		if d, ok := c.cachedDetail(id); ok {
			out[id] = d
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(id string) {
			defer wg.Done()
			defer func() { <-sem }()
			var d vulnDetail
			if err := c.getJSON(ctx, c.BaseURL+"/v1/vulns/"+id, detailCap, &d); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("OSV detail lookup failed: %w", err)
				}
				mu.Unlock()
				return
			}
			c.storeDetail(id, d)
			mu.Lock()
			out[id] = d
			mu.Unlock()
		}(id)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

func (c *Client) cachedDetail(id string) (vulnDetail, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.details[id]
	if !ok || time.Since(e.fetchedAt) >= cacheTTL {
		return vulnDetail{}, false
	}
	return e.detail, true
}

func (c *Client) storeDetail(id string, d vulnDetail) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.details) >= detailMax {
		// shortcut: drop the whole cache when full; it refills on demand
		c.details = map[string]cachedDetail{}
	}
	c.details[id] = cachedDetail{detail: d, fetchedAt: time.Now()}
}

// fixedVersion returns the version that fixes the vulnerability for the given package, or "" when
// the feed does not name one. When OSV lists several fix versions the newest one is reported.
func fixedVersion(d vulnDetail, pkgName, ecosystem string) string {
	best := ""
	for _, a := range d.Affected {
		if a.Package.Name != pkgName || a.Package.Ecosystem != ecosystem {
			continue
		}
		for _, r := range a.Ranges {
			for _, e := range r.Events {
				if e.Fixed != "" && e.Fixed > best {
					best = e.Fixed
				}
			}
		}
	}
	return best
}

// cveIDs collects the CVE identifiers of a vulnerability (its own ID, aliases and upstream IDs).
func cveIDs(d vulnDetail) []string {
	var out []string
	for _, s := range append([]string{d.ID}, d.Aliases...) {
		if cveRe.MatchString(s) {
			out = append(out, s)
		}
	}
	for _, s := range d.Upstream {
		if cveRe.MatchString(s) {
			out = append(out, s)
		}
	}
	return sortedUnique(out)
}

// ---------------------------------------------------------------------------
// HTTP helpers

func (c *Client) postJSON(ctx context.Context, url string, body []byte, limit int64, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "pantau")
	req.Header.Set("Accept", "application/json")
	return c.do(req, limit, out)
}

func (c *Client) getJSON(ctx context.Context, url string, limit int64, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "pantau")
	req.Header.Set("Accept", "application/json")
	return c.do(req, limit, out)
}

func (c *Client) do(req *http.Request, limit int64, out any) error {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("unreachable from the Pantau server: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return errors.New("response read failed")
	}
	if int64(len(body)) > limit {
		return errors.New("response too large")
	}
	return json.Unmarshal(body, out)
}

func sortedUnique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
