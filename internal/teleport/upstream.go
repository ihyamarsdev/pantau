package teleport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The Gateway CLI Update Check for tsh (ADR 0080). It only reads: the newest stable release and the proxy's
// version. Nothing is downloaded or installed here.
//
// The hosts are fixed. A URL from a request never reaches an HTTP call; the only request input that
// is used is a profile ID, which resolves to the proxy already stored and validated for that profile.

const (
	defaultReleaseAPI = "https://api.github.com/repos/gravitational/teleport/releases/latest"
	defaultCDN        = "https://cdn.teleport.dev"
	latestCacheTTL    = time.Hour
	upstreamTimeout   = 15 * time.Second
	proxyPingTimeout  = 5 * time.Second
)

// Upstream holds where the newest release is read from and where tsh is downloaded from. NewManager uses the
// Teleport hosts; SetUpstream replaces them, which tests use to point at a local TLS server.
type Upstream struct {
	ReleaseAPI string       // GitHub "latest release" API (stable releases only)
	CDN        string       // base of the tsh tarballs and their .sha256 files
	Client     *http.Client // used for the release API and the CDN
	ProxyPing  *http.Client // used for https://<proxy>/webapi/ping
}

// defaultUpstream is the production configuration.
func defaultUpstream() Upstream {
	return Upstream{
		ReleaseAPI: defaultReleaseAPI,
		CDN:        defaultCDN,
		Client:     &http.Client{Timeout: upstreamTimeout},
		ProxyPing:  &http.Client{Timeout: proxyPingTimeout},
	}
}

// SetUpstream replaces the release and download hosts. Every URL must be https.
func (m *Manager) SetUpstream(u Upstream) error {
	for _, raw := range []string{u.ReleaseAPI, u.CDN} {
		if !strings.HasPrefix(raw, "https://") {
			return fmt.Errorf("%w: upstream %q must be https", ErrInvalidInput, raw)
		}
	}
	if u.Client == nil || u.ProxyPing == nil {
		return fmt.Errorf("%w: upstream clients are required", ErrInvalidInput)
	}
	m.latestMu.Lock()
	m.up = u
	m.latest = cachedRelease{}
	m.latestMu.Unlock()
	return nil
}

// Release is the newest stable tsh release published upstream.
type Release struct {
	Version string
	URL     string
}

type cachedRelease struct {
	rel Release
	at  time.Time
}

// Latest returns the newest stable tsh release. It is cached for an hour; force skips the cache. The bool reports
// whether the answer came from the cache.
func (m *Manager) Latest(ctx context.Context, force bool) (Release, bool, error) {
	m.latestMu.Lock()
	defer m.latestMu.Unlock()
	if !force && m.latest.rel.Version != "" && time.Since(m.latest.at) < latestCacheTTL {
		return m.latest.rel, true, nil
	}
	rel, err := m.fetchLatest(ctx)
	if err != nil {
		return Release{}, false, err
	}
	m.latest = cachedRelease{rel: rel, at: time.Now()}
	return rel, false, nil
}

// fetchLatest reads the GitHub release API. The tag must be a plain release (vX.Y.Z); anything else is refused.
func (m *Manager) fetchLatest(ctx context.Context) (Release, error) {
	ctx, cancel := context.WithTimeout(ctx, upstreamTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.up.ReleaseAPI, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Pantau")

	resp, err := m.up.Client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("teleport releases: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("teleport releases: status %s", resp.Status)
	}
	var doc struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return Release{}, fmt.Errorf("teleport releases: %w", err)
	}
	version := strings.TrimPrefix(doc.TagName, "v")
	if !releaseVersion.MatchString(version) {
		return Release{}, fmt.Errorf("teleport releases: tag %q is not a release", doc.TagName)
	}
	return Release{Version: version, URL: "https://github.com/gravitational/teleport/releases/tag/v" + version}, nil
}

// ProxyVersion reads server_version from https://<proxy>/webapi/ping. The proxy must be the host:port stored for
// the profile, already checked by ValidateProfile.
func (m *Manager) ProxyVersion(ctx context.Context, proxy string) (string, error) {
	if !proxyRe.MatchString(proxy) {
		return "", fmt.Errorf("%w: proxy address is not valid", ErrInvalidInput)
	}
	ctx, cancel := context.WithTimeout(ctx, proxyPingTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+proxy+"/webapi/ping", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Pantau")

	resp, err := m.up.ProxyPing.Do(req)
	if err != nil {
		return "", fmt.Errorf("proxy ping: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("proxy ping: status %s", resp.Status)
	}
	var doc struct {
		ServerVersion string `json:"server_version"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return "", fmt.Errorf("proxy ping: %w", err)
	}
	if !releaseVersion.MatchString(doc.ServerVersion) {
		return "", errors.New("proxy ping: no server_version in the response")
	}
	return doc.ServerVersion, nil
}

// Update statuses. check_failed is reported whenever the upstream could not be read; it is never "up_to_date".
const (
	StatusUpdateAvailable = "update_available"
	StatusUpToDate        = "up_to_date"
	StatusNotInstalled    = "not_installed"
	StatusUnknown         = "unknown"
	StatusCheckFailed     = "check_failed"
)

// ProxyCheck is the proxy's version as read from /webapi/ping for one profile.
type ProxyCheck struct {
	ProfileID int64  `json:"profile_id"`
	Address   string `json:"address"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
}

// UpdateCheck is the result of one tsh update check.
type UpdateCheck struct {
	Status     string      `json:"status"`
	Installed  string      `json:"installed"`
	Latest     string      `json:"latest,omitempty"`
	ReleaseURL string      `json:"release_url,omitempty"`
	Error      string      `json:"error,omitempty"`
	CheckedAt  time.Time   `json:"checked_at"`
	Cached     bool        `json:"cached"`
	Proxy      *ProxyCheck `json:"proxy,omitempty"`
	// MajorVersionMismatch is true when the installed or the latest tsh has a different major version than the proxy.
	MajorVersionMismatch bool `json:"major_version_mismatch"`
}

// CheckUpdate compares the tsh Pantau would run with the newest stable release. profile, when set, adds the
// proxy's version. An upstream that cannot be reached is check_failed with the reason, never "up to date".
func (m *Manager) CheckUpdate(ctx context.Context, profile *Profile, force bool) UpdateCheck {
	info := m.TSHInfo(ctx)
	res := UpdateCheck{Installed: info.Version, CheckedAt: time.Now()}

	rel, cached, err := m.Latest(ctx, force)
	res.Cached = cached
	if err != nil {
		res.Status = StatusCheckFailed
		res.Error = err.Error()
	} else {
		res.Latest, res.ReleaseURL = rel.Version, rel.URL
		res.Status = compareStatus(info, rel.Version)
	}

	if profile != nil {
		pc := &ProxyCheck{ProfileID: profile.ID, Address: profile.Proxy}
		if v, err := m.ProxyVersion(ctx, profile.Proxy); err != nil {
			pc.Error = err.Error()
		} else {
			pc.Version = v
		}
		res.Proxy = pc
		if pc.Version != "" {
			pm := majorOf(pc.Version)
			res.MajorVersionMismatch = (res.Installed != "" && majorOf(res.Installed) != pm) ||
				(res.Latest != "" && majorOf(res.Latest) != pm)
		}
	}
	return res
}

// compareStatus decides the status once the upstream answered.
func compareStatus(info TSHInfo, latest string) string {
	switch {
	case !info.Installed:
		return StatusNotInstalled
	case info.Version == "":
		return StatusUnknown
	case compareReleases(latest, info.Version) > 0:
		return StatusUpdateAvailable
	default:
		return StatusUpToDate
	}
}

// compareReleases compares two MAJOR.MINOR.PATCH strings: -1, 0 or 1. Both must match releaseVersion.
func compareReleases(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		x, _ := strconv.Atoi(pa[i])
		y, _ := strconv.Atoi(pb[i])
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// majorOf returns the major number of a MAJOR.MINOR.PATCH string, or -1 when it has none.
func majorOf(v string) int {
	major, _, _ := strings.Cut(v, ".")
	n, err := strconv.Atoi(major)
	if err != nil {
		return -1
	}
	return n
}
