// Package dockerhub reads public Docker Hub data for the Docker tab: repository search, repository detail with
// README, tags with platforms and digests, and the digest comparison behind the image update check. Requests go
// from the Pantau server (not the managed host), use no credentials, and are cached in memory for a few minutes.
package dockerhub

import (
	"context"
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
	"unicode/utf8"
)

const (
	DefaultHubURL   = "https://hub.docker.com"
	DefaultIndexURL = "https://index.docker.io" // registry search API used by `docker search`

	reqTimeout  = 15 * time.Second
	bodyCap     = 4 << 20
	readmeCap   = 100 << 10
	cacheTTL    = 5 * time.Minute
	cacheMax    = 512
	searchSize  = 25
	tagPageSize = 50
	maxPage     = 100
)

// ErrBadArg marks a rejected name, tag, query or page (never sent upstream).
var ErrBadArg = errors.New("bad Docker Hub argument")

// StatusError is a non-200 answer from Docker Hub.
type StatusError struct{ Status int }

func (e *StatusError) Error() string {
	if e.Status == http.StatusTooManyRequests {
		return "Docker Hub rate limit reached; try again in a minute"
	}
	return fmt.Sprintf("Docker Hub returned HTTP %d", e.Status)
}

// IsNotFound reports whether err is a 404 from Docker Hub.
func IsNotFound(err error) bool {
	var se *StatusError
	return errors.As(err, &se) && se.Status == http.StatusNotFound
}

var (
	// One path component of a Docker Hub repository (namespace or name), as the reference grammar allows.
	componentRe = regexp.MustCompile(`^[a-z0-9]+(?:[._-]+[a-z0-9]+)*$`)
	tagRe       = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
	tagFilterRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{0,128}$`)
)

func validRepo(ns, name string) error {
	for _, v := range []string{ns, name} {
		if len(v) > 255 || !componentRe.MatchString(v) {
			return fmt.Errorf("%w: %q", ErrBadArg, v)
		}
	}
	return nil
}

func validPage(page int) (int, error) {
	if page == 0 {
		return 1, nil
	}
	if page < 1 || page > maxPage {
		return 0, fmt.Errorf("%w: page %d", ErrBadArg, page)
	}
	return page, nil
}

// RefName is the name `docker pull` takes for a repository ("nginx" for library/nginx).
func RefName(ns, name string) string {
	if ns == "library" {
		return name
	}
	return ns + "/" + name
}

// ParseRef splits a local image name ("nginx:1.27", "bitnami/redis", "docker.io/library/nginx:latest") into its
// Docker Hub namespace, repository and tag. ok is false for images of other registries (ghcr.io, a host:port,
// localhost), digest references and names Docker Hub cannot hold.
func ParseRef(ref string) (ns, name, tag string, ok bool) {
	if ref == "" || strings.Contains(ref, "@") {
		return "", "", "", false
	}
	repo, tag := ref, "latest"
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		repo, tag = ref[:i], ref[i+1:]
	}
	ns, name, ok = hubRepo(repo)
	if !ok || !tagRe.MatchString(tag) {
		return "", "", "", false
	}
	return ns, name, tag, true
}

// hubRepo normalizes a repository name without tag to Docker Hub namespace/name.
func hubRepo(repo string) (ns, name string, ok bool) {
	parts := strings.Split(repo, "/")
	if first := parts[0]; len(parts) > 1 && (strings.ContainsAny(first, ".:") || first == "localhost") {
		switch first {
		case "docker.io", "index.docker.io", "registry-1.docker.io":
			parts = parts[1:]
		default:
			return "", "", false // another registry
		}
	}
	switch len(parts) {
	case 1:
		ns, name = "library", parts[0]
	case 2:
		ns, name = parts[0], parts[1]
	default:
		return "", "", false
	}
	if validRepo(ns, name) != nil {
		return "", "", false
	}
	return ns, name, true
}

// Client talks to Docker Hub. The zero value is not usable; use New.
type Client struct {
	HubURL   string
	IndexURL string
	HTTP     *http.Client

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	body    []byte
	expires time.Time
}

// New returns a client for the given Hub API and registry search base URLs (tests point both at a fake).
func New(hubURL, indexURL string) *Client {
	hc := &http.Client{
		Timeout: reqTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 3 || req.URL.Host != via[0].URL.Host {
				return errors.New("redirect refused")
			}
			return nil
		},
	}
	return &Client{HubURL: strings.TrimRight(hubURL, "/"), IndexURL: strings.TrimRight(indexURL, "/"), HTTP: hc, cache: map[string]cacheEntry{}}
}

func (c *Client) cached(u string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.cache[u]
	if !ok || !time.Now().Before(e.expires) {
		delete(c.cache, u)
		return nil, false
	}
	return e.body, true
}

func (c *Client) store(u string, body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if len(c.cache) >= cacheMax {
		oldK, oldT := "", time.Time{}
		for k, e := range c.cache {
			if !now.Before(e.expires) {
				delete(c.cache, k)
			} else if oldK == "" || e.expires.Before(oldT) {
				oldK, oldT = k, e.expires
			}
		}
		if len(c.cache) >= cacheMax {
			delete(c.cache, oldK)
		}
	}
	c.cache[u] = cacheEntry{body: body, expires: now.Add(cacheTTL)}
}

// getJSON fetches u (cached) and decodes it into out.
func (c *Client) getJSON(ctx context.Context, u string, out any) error {
	body, ok := c.cached(u)
	if !ok {
		var err error
		if body, err = c.fetch(ctx, u); err != nil {
			return err
		}
		c.store(u, body)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return errors.New("Docker Hub returned invalid JSON")
	}
	return nil
}

func (c *Client) fetch(ctx context.Context, u string) ([]byte, error) {
	rctx, cancel := context.WithTimeout(ctx, reqTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, errors.New("invalid Docker Hub URL")
	}
	req.Header.Set("User-Agent", "pantau")
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		switch {
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case rctx.Err() != nil:
			return nil, errors.New("Docker Hub did not answer within 15s")
		}
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		msg := err.Error()
		if len(msg) > 120 {
			msg = msg[:120]
		}
		return nil, errors.New("Docker Hub is unreachable from the Pantau server: " + msg)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &StatusError{Status: resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, bodyCap+1))
	if err != nil {
		return nil, errors.New("Docker Hub response read failed")
	}
	if len(body) > bodyCap {
		return nil, errors.New("Docker Hub response too large")
	}
	return body, nil
}

// Repo is one repository in search results or a repository header.
type Repo struct {
	Name        string `json:"name"` // as `docker pull` takes it: "nginx", "bitnami/redis"
	Namespace   string `json:"namespace"`
	Repository  string `json:"repository"`
	Description string `json:"description"`
	Stars       int    `json:"stars"`
	Pulls       int64  `json:"pulls"`
	Official    bool   `json:"official"`
	Automated   bool   `json:"automated"`
}

// SearchResult is one page of repository search results.
type SearchResult struct {
	Results  []Repo `json:"results"`
	Page     int    `json:"page"`
	NumPages int    `json:"num_pages"`
	Total    int    `json:"total"`
}

// Search finds repositories by text (the registry search behind `docker search`).
func (c *Client) Search(ctx context.Context, q string, page int) (*SearchResult, error) {
	q = strings.TrimSpace(q)
	if q == "" || len(q) > 100 {
		return nil, fmt.Errorf("%w: query must be 1-100 characters", ErrBadArg)
	}
	page, err := validPage(page)
	if err != nil {
		return nil, err
	}
	var raw struct {
		NumPages   int `json:"num_pages"`
		NumResults int `json:"num_results"`
		Page       int `json:"page"`
		Results    []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			StarCount   int    `json:"star_count"`
			PullCount   int64  `json:"pull_count"`
			IsOfficial  bool   `json:"is_official"`
			IsAutomated bool   `json:"is_automated"`
		} `json:"results"`
	}
	u := c.IndexURL + "/v1/search?" + url.Values{"q": {q}, "n": {strconv.Itoa(searchSize)}, "page": {strconv.Itoa(page)}}.Encode()
	if err := c.getJSON(ctx, u, &raw); err != nil {
		return nil, err
	}
	res := &SearchResult{Results: []Repo{}, Page: raw.Page, NumPages: raw.NumPages, Total: raw.NumResults}
	for _, r := range raw.Results {
		ns, name, ok := hubRepo(r.Name)
		if !ok {
			continue
		}
		res.Results = append(res.Results, Repo{Name: RefName(ns, name), Namespace: ns, Repository: name, Description: r.Description,
			Stars: r.StarCount, Pulls: r.PullCount, Official: r.IsOfficial, Automated: r.IsAutomated})
	}
	// The registry search ranks by text match; like the Docker Hub site, show official images first.
	sort.SliceStable(res.Results, func(i, j int) bool { return res.Results[i].Official && !res.Results[j].Official })
	return res, nil
}

// Repository is a repository with its README (Docker Hub "full description").
type Repository struct {
	Repo
	Readme          string `json:"readme"`
	ReadmeTruncated bool   `json:"readme_truncated"`
	LastUpdated     string `json:"last_updated"`
	URL             string `json:"url"` // the repository page on hub.docker.com
}

// Repository returns one repository.
func (c *Client) Repository(ctx context.Context, ns, name string) (*Repository, error) {
	if err := validRepo(ns, name); err != nil {
		return nil, err
	}
	var raw struct {
		Description     string `json:"description"`
		FullDescription string `json:"full_description"`
		StarCount       int    `json:"star_count"`
		PullCount       int64  `json:"pull_count"`
		IsAutomated     bool   `json:"is_automated"`
		LastUpdated     string `json:"last_updated"`
	}
	if err := c.getJSON(ctx, c.HubURL+"/v2/namespaces/"+ns+"/repositories/"+name, &raw); err != nil {
		return nil, err
	}
	r := &Repository{
		Repo: Repo{Name: RefName(ns, name), Namespace: ns, Repository: name, Description: raw.Description,
			Stars: raw.StarCount, Pulls: raw.PullCount, Official: ns == "library", Automated: raw.IsAutomated},
		Readme: raw.FullDescription, LastUpdated: raw.LastUpdated, URL: "https://hub.docker.com/r/" + ns + "/" + name,
	}
	if ns == "library" {
		r.URL = "https://hub.docker.com/_/" + name
	}
	if len(r.Readme) > readmeCap {
		cut := r.Readme[:readmeCap]
		for len(cut) > 0 && !utf8.ValidString(cut) {
			cut = cut[:len(cut)-1]
		}
		r.Readme, r.ReadmeTruncated = cut, true
	}
	return r, nil
}

// Tag is one tag of a repository.
type Tag struct {
	Name       string   `json:"name"`
	Digest     string   `json:"digest"` // index (multi-platform) or manifest digest
	Size       int64    `json:"size"`
	LastPushed string   `json:"last_pushed"`
	Platforms  []string `json:"platforms"` // os/arch[/variant]

	platformDigests []string
}

type rawTag struct {
	Name          string `json:"name"`
	Digest        string `json:"digest"`
	FullSize      int64  `json:"full_size"`
	LastUpdated   string `json:"last_updated"`
	TagLastPushed string `json:"tag_last_pushed"`
	Images        []struct {
		Architecture string `json:"architecture"`
		Variant      string `json:"variant"`
		OS           string `json:"os"`
		Digest       string `json:"digest"`
	} `json:"images"`
}

func (r rawTag) tag() Tag {
	t := Tag{Name: r.Name, Digest: r.Digest, Size: r.FullSize, LastPushed: r.TagLastPushed, Platforms: []string{}}
	if t.LastPushed == "" {
		t.LastPushed = r.LastUpdated
	}
	for _, im := range r.Images {
		if im.Digest != "" {
			t.platformDigests = append(t.platformDigests, im.Digest)
		}
		if im.OS == "" || im.OS == "unknown" { // attestation manifests
			continue
		}
		p := im.OS + "/" + im.Architecture
		if im.Variant != "" {
			p += "/" + im.Variant
		}
		t.Platforms = append(t.Platforms, p)
	}
	return t
}

// TagPage is one page of tags, newest push first.
type TagPage struct {
	Tags    []Tag `json:"tags"`
	Count   int   `json:"count"`
	Page    int   `json:"page"`
	HasNext bool  `json:"has_next"`
}

// Tags lists tags whose name contains filter (empty = all).
func (c *Client) Tags(ctx context.Context, ns, name, filter string, page int) (*TagPage, error) {
	if err := validRepo(ns, name); err != nil {
		return nil, err
	}
	if !tagFilterRe.MatchString(filter) {
		return nil, fmt.Errorf("%w: tag filter %q", ErrBadArg, filter)
	}
	page, err := validPage(page)
	if err != nil {
		return nil, err
	}
	q := url.Values{"page": {strconv.Itoa(page)}, "page_size": {strconv.Itoa(tagPageSize)}, "ordering": {"last_updated"}}
	if filter != "" {
		q.Set("name", filter)
	}
	var raw struct {
		Count   int      `json:"count"`
		Next    *string  `json:"next"`
		Results []rawTag `json:"results"`
	}
	res := &TagPage{Tags: []Tag{}, Page: page}
	if err := c.getJSON(ctx, c.HubURL+"/v2/namespaces/"+ns+"/repositories/"+name+"/tags?"+q.Encode(), &raw); err != nil {
		if page > 1 && IsNotFound(err) { // Docker Hub answers 404 past the last page
			return res, nil
		}
		return nil, err
	}
	res.Count, res.HasNext = raw.Count, raw.Next != nil && *raw.Next != ""
	for _, r := range raw.Results {
		res.Tags = append(res.Tags, r.tag())
	}
	return res, nil
}

// Tag returns one tag.
func (c *Client) Tag(ctx context.Context, ns, name, tag string) (*Tag, error) {
	if err := validRepo(ns, name); err != nil {
		return nil, err
	}
	if !tagRe.MatchString(tag) {
		return nil, fmt.Errorf("%w: tag %q", ErrBadArg, tag)
	}
	var raw rawTag
	if err := c.getJSON(ctx, c.HubURL+"/v2/namespaces/"+ns+"/repositories/"+name+"/tags/"+tag, &raw); err != nil {
		return nil, err
	}
	t := raw.tag()
	return &t, nil
}
