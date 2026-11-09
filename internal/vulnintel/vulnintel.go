// Package vulnintel adds exploit intelligence to security scan findings: whether a CVE is in the CISA Known
// Exploited Vulnerabilities catalog (KEV) and its FIRST EPSS score (probability of exploitation in the next 30
// days). Both public feeds are fetched by the Pantau server only when the administrator opts in, cached in memory
// for a day, and never stored. The package also embeds the curated list of vulnerability databases shown in the
// Security tab.
package vulnintel

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultKEVURL  = "https://www.cisa.gov/sites/default/files/feeds/known_exploited_vulnerabilities.json"
	DefaultEPSSURL = "https://api.first.org/data/v1/epss"

	feedTTL     = 24 * time.Hour
	retryAfter  = 15 * time.Minute // a failed refresh is not retried on every page view
	kevTimeout  = 30 * time.Second
	epssTimeout = 15 * time.Second
	kevBodyCap  = 16 << 20
	epssBodyCap = 2 << 20
	epssBatch   = 100 // the EPSS API answers nothing for longer lists
	epssMax     = 20000
)

var cveRe = regexp.MustCompile(`^CVE-[0-9]{4}-[0-9]{4,}$`)

// KEV is one entry of the CISA Known Exploited Vulnerabilities catalog.
type KEV struct {
	Name       string `json:"name"`
	DateAdded  string `json:"date_added"`
	DueDate    string `json:"due_date"`
	Ransomware bool   `json:"ransomware"` // known use in ransomware campaigns
}

// Info is the exploit intelligence for one CVE.
type Info struct {
	KEV        *KEV     `json:"kev,omitempty"`
	EPSS       *float64 `json:"epss,omitempty"`       // 0-1
	Percentile *float64 `json:"percentile,omitempty"` // 0-1
}

// Result is the intelligence for a set of CVEs plus the state of each feed.
type Result struct {
	CVEs        map[string]Info `json:"cves"`
	KEVCatalog  string          `json:"kev_catalog,omitempty"` // catalog release date
	KEVError    string          `json:"kev_error,omitempty"`
	EPSSDate    string          `json:"epss_date,omitempty"`
	EPSSError   string          `json:"epss_error,omitempty"`
	CheckedCVEs int             `json:"checked_cves"`
}

type epssScore struct {
	known            bool
	epss, percentile float64
	date             string
	at               time.Time
}

// Client fetches and caches the KEV catalog and EPSS scores.
type Client struct {
	KEVURL, EPSSURL string
	HTTP            *http.Client

	mu         sync.Mutex
	kev        map[string]KEV
	kevRelease string
	kevAt      time.Time // last successful download
	kevTried   time.Time
	kevErr     error
	epss       map[string]epssScore
	epssTried  time.Time
	epssErr    error
}

// New returns a client for the given feed URLs (tests point them at a fake).
func New(kevURL, epssURL string) *Client {
	return &Client{KEVURL: kevURL, EPSSURL: epssURL, HTTP: &http.Client{}, epss: map[string]epssScore{}}
}

func (c *Client) get(ctx context.Context, u string, timeout time.Duration, cap int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "pantau")
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		msg := err.Error()
		if len(msg) > 120 {
			msg = msg[:120]
		}
		return nil, errors.New("unreachable from the Pantau server: " + msg)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, cap+1))
	if err != nil {
		return nil, errors.New("response read failed")
	}
	if int64(len(body)) > cap {
		return nil, errors.New("response too large")
	}
	return body, nil
}

// refreshKEV downloads the catalog when the cached copy is older than a day. A failed download keeps the last good
// catalog (reported with its release date) and is retried after a pause. Called with c.mu held.
func (c *Client) refreshKEV(ctx context.Context) {
	now := time.Now()
	fresh := c.kev != nil && now.Sub(c.kevAt) < feedTTL
	if fresh || (c.kevErr != nil && now.Sub(c.kevTried) < retryAfter) {
		return
	}
	c.kevTried = now
	body, err := c.get(ctx, c.KEVURL, kevTimeout, kevBodyCap)
	if err == nil {
		var raw struct {
			DateReleased    string `json:"dateReleased"`
			Vulnerabilities []struct {
				CVEID                      string `json:"cveID"`
				VulnerabilityName          string `json:"vulnerabilityName"`
				DateAdded                  string `json:"dateAdded"`
				DueDate                    string `json:"dueDate"`
				KnownRansomwareCampaignUse string `json:"knownRansomwareCampaignUse"`
			} `json:"vulnerabilities"`
		}
		if err = json.Unmarshal(body, &raw); err == nil && len(raw.Vulnerabilities) == 0 {
			err = errors.New("catalog is empty")
		}
		if err == nil {
			m := make(map[string]KEV, len(raw.Vulnerabilities))
			for _, v := range raw.Vulnerabilities {
				m[v.CVEID] = KEV{Name: v.VulnerabilityName, DateAdded: v.DateAdded, DueDate: v.DueDate,
					Ransomware: strings.EqualFold(v.KnownRansomwareCampaignUse, "Known")}
			}
			c.kev, c.kevRelease, c.kevAt = m, raw.DateReleased, now
		}
	}
	c.kevErr = err
}

// refreshEPSS fetches scores for the CVEs that are missing or older than a day. Called with c.mu held.
func (c *Client) refreshEPSS(ctx context.Context, cves []string) {
	now := time.Now()
	var want []string
	for _, id := range cves {
		if s, ok := c.epss[id]; !ok || now.Sub(s.at) >= feedTTL {
			want = append(want, id)
		}
	}
	if len(want) == 0 {
		return
	}
	if c.epssErr != nil && now.Sub(c.epssTried) < retryAfter {
		return
	}
	c.epssTried, c.epssErr = now, nil
	for len(want) > 0 {
		n := min(len(want), epssBatch)
		batch := want[:n]
		want = want[n:]
		body, err := c.get(ctx, c.EPSSURL+"?cve="+strings.Join(batch, ","), epssTimeout, epssBodyCap)
		var raw struct {
			Data []struct {
				CVE        string `json:"cve"`
				EPSS       string `json:"epss"`
				Percentile string `json:"percentile"`
				Date       string `json:"date"`
			} `json:"data"`
		}
		if err == nil {
			err = json.Unmarshal(body, &raw)
		}
		if err != nil {
			c.epssErr = err
			return
		}
		if len(c.epss)+len(batch) > epssMax {
			c.epss = map[string]epssScore{} // shortcut: drop the whole cache when full, refilled on demand
		}
		for _, id := range batch {
			c.epss[id] = epssScore{at: now} // CVEs EPSS does not score yet stay "unknown" for a day
		}
		for _, d := range raw.Data {
			e, err1 := strconv.ParseFloat(d.EPSS, 64)
			p, err2 := strconv.ParseFloat(d.Percentile, 64)
			if err1 == nil && err2 == nil {
				c.epss[d.CVE] = epssScore{known: true, epss: e, percentile: p, date: d.Date, at: now}
			}
		}
	}
}

// Lookup returns KEV and EPSS data for cves (non-CVE strings are ignored). Feed failures are reported in the
// result, never as an error: the scan itself stays valid without intelligence.
func (c *Client) Lookup(ctx context.Context, cves []string) Result {
	seen := map[string]bool{}
	var ids []string
	for _, id := range cves {
		if cveRe.MatchString(id) && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	res := Result{CVEs: map[string]Info{}, CheckedCVEs: len(ids)}
	if len(ids) == 0 {
		return res
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.refreshKEV(ctx)
	c.refreshEPSS(ctx, ids)

	if c.kev == nil {
		res.KEVError = "CISA KEV catalog not available"
	} else {
		res.KEVCatalog = c.kevRelease
	}
	if c.kevErr != nil {
		res.KEVError = "CISA KEV refresh failed: " + c.kevErr.Error()
	}
	if c.epssErr != nil {
		res.EPSSError = "EPSS lookup failed: " + c.epssErr.Error()
	}
	for _, id := range ids {
		var info Info
		if k, ok := c.kev[id]; ok {
			k := k
			info.KEV = &k
		}
		if s, ok := c.epss[id]; ok && s.known {
			e, p := s.epss, s.percentile
			info.EPSS, info.Percentile = &e, &p
			if s.date > res.EPSSDate {
				res.EPSSDate = s.date
			}
		}
		if info.KEV != nil || info.EPSS != nil {
			res.CVEs[id] = info
		}
	}
	return res
}

// databases.json is a snapshot of https://github.com/haxdoggy/vulnerability-databases (CC BY 4.0), unmodified.
//
//go:embed databases.json
var databasesJSON []byte

// Source describes where the embedded database list comes from (CC BY 4.0 requires the attribution).
var Source = map[string]string{
	"name":        "haxdoggy/vulnerability-databases",
	"url":         "https://github.com/haxdoggy/vulnerability-databases",
	"license":     "CC BY 4.0",
	"license_url": "https://creativecommons.org/licenses/by/4.0/",
	"commit":      "f9cf635c4a01",
	"date":        "2026-11-08",
}

// UsedByPantau lists the abbreviations (as in databases.json) of the feeds this package reads.
var UsedByPantau = []string{"CISA KEV", "KEV JSON", "EPSS"}

// Databases returns the embedded list of vulnerability databases.
func Databases() json.RawMessage { return databasesJSON }
