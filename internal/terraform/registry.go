package terraform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// DefaultRegistryURL is the public Terraform Registry.
const DefaultRegistryURL = "https://registry.terraform.io"

const (
	regTimeout    = 15 * time.Second
	regBodyCap    = 4 << 20
	regCacheMax   = 256
	regMaxRedirct = 3
)

// ErrBadRegistryArg marks a rejected path segment or parameter (never sent upstream).
var ErrBadRegistryArg = errors.New("bad registry argument")

// RegistryStatusError is a non-200 answer from the registry.
type RegistryStatusError struct{ Status int }

func (e *RegistryStatusError) Error() string {
	return fmt.Sprintf("registry returned HTTP %d", e.Status)
}

var regSegRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// Overridable in tests.
var (
	regNow      = time.Now
	regCacheTTL = 5 * time.Minute
)

type regCacheEntry struct {
	body    []byte
	expires time.Time
}

var regCache = struct {
	sync.Mutex
	m map[string]regCacheEntry
}{m: map[string]regCacheEntry{}}

func regCacheGet(key string) ([]byte, bool) {
	regCache.Lock()
	defer regCache.Unlock()
	e, ok := regCache.m[key]
	if !ok || !regNow().Before(e.expires) {
		delete(regCache.m, key)
		return nil, false
	}
	return e.body, true
}

func regCachePut(key string, body []byte) {
	regCache.Lock()
	defer regCache.Unlock()
	now := regNow()
	if len(regCache.m) >= regCacheMax {
		var oldK string
		var oldT time.Time
		for k, e := range regCache.m {
			if !now.Before(e.expires) {
				delete(regCache.m, k)
			} else if oldK == "" || e.expires.Before(oldT) {
				oldK, oldT = k, e.expires
			}
		}
		if len(regCache.m) >= regCacheMax {
			delete(regCache.m, oldK)
		}
	}
	regCache.m[key] = regCacheEntry{body: body, expires: now.Add(regCacheTTL)}
}

// RegistryClient talks to a Terraform Registry (public or mirror). Responses are cached process-wide by URL.
type RegistryClient struct {
	BaseURL string
	HTTP    *http.Client
}

func NewRegistryClient(base string) *RegistryClient {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = DefaultRegistryURL
	}
	return &RegistryClient{BaseURL: base, HTTP: &http.Client{
		Timeout: regTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > regMaxRedirct || req.URL.Host != via[0].URL.Host {
				return errors.New("redirect refused")
			}
			return nil
		},
	}}
}

// ValidateRegistryURL normalizes a registry base URL: https only (http for loopback), no userinfo/query/fragment.
func ValidateRegistryURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultRegistryURL, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return "", errors.New("invalid URL")
	}
	if u.User != nil {
		return "", errors.New("credentials in the URL are not allowed")
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") {
		return "", errors.New("query and fragment are not allowed")
	}
	host := u.Hostname()
	loopback := host == "localhost" || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback())
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return "", errors.New("URL must use https (http only for loopback hosts)")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func regSeg(vals ...string) error {
	for _, v := range vals {
		if !regSegRe.MatchString(v) || strings.Contains(v, "..") {
			return fmt.Errorf("%w: %q", ErrBadRegistryArg, v)
		}
	}
	return nil
}

// Ping checks reachability without the cache.
func (c *RegistryClient) Ping(ctx context.Context) error {
	return c.get(ctx, "/v1/modules/search", url.Values{"limit": {"1"}}, nil, false)
}

func (c *RegistryClient) get(ctx context.Context, path string, q url.Values, out any, useCache bool) error {
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	body, ok := []byte(nil), false
	if useCache {
		body, ok = regCacheGet(u)
	}
	if !ok {
		var err error
		if body, err = c.fetch(ctx, u); err != nil {
			return err
		}
		if useCache {
			regCachePut(u, body)
		}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return errors.New("registry returned invalid JSON")
	}
	return nil
}

func (c *RegistryClient) fetch(ctx context.Context, u string) ([]byte, error) {
	hc := c.HTTP
	if hc == nil {
		hc = NewRegistryClient(c.BaseURL).HTTP
	}
	ctx, cancel := context.WithTimeout(ctx, regTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, errors.New("invalid registry URL")
	}
	req.Header.Set("User-Agent", "pantau")
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		msg := err.Error()
		if len(msg) > 100 {
			msg = msg[:100]
		}
		return nil, errors.New("registry request failed: " + msg)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &RegistryStatusError{Status: resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, regBodyCap+1))
	if err != nil {
		return nil, errors.New("registry response read failed")
	}
	if len(body) > regBodyCap {
		return nil, errors.New("registry response too large")
	}
	return body, nil
}
