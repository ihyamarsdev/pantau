package globe

// Public-IP geolocation with strict timeouts, rate limiting, and offline-safe
// fallbacks (ADR 0090). The external HTTP call is injectable so unit tests run
// fully offline. Nothing here blocks the globe HTTP read path: the Resolver is
// driven by a background loop and the read path only touches the SQLite cache.

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"strings"
	"sync"
	"time"

	"pantau/internal/store"
)

const (
	geoLookupURL   = "https://api.ip2location.io/?ip="
	geoBackupURL   = "http://ip-api.com/json/"
	geoTimeout     = 3 * time.Second
	geoRateLimit   = 2 * time.Second // minimum spacing between external lookups
	egressTimeout  = 2 * time.Second
	gatewayLat     = 0.0
	gatewayLon     = 0.0
	gatewayCity    = "Pantau Gateway"
	gatewayCountry = ""
)

// LookupFn resolves an IP to a location. Injected in tests.
type LookupFn func(ctx context.Context, ip string) (store.GeoIPEntry, error)

// Resolver fills the GeoIP cache in the background. It is safe for concurrent
// use and degrades gracefully when offline.
type Resolver struct {
	db        *store.DB
	lookup    LookupFn
	http      *http.Client
	minSpace  time.Duration
	mu        sync.Mutex // serialises the rate-limit clock across concurrent ResolveHost/RunOnce calls
	lastCall  time.Time
	egressURL string
}

// NewResolver builds a resolver with the default HTTP-backed lookup.
func NewResolver(db *store.DB) *Resolver {
	client := &http.Client{Timeout: geoTimeout}
	r := &Resolver{db: db, http: client, minSpace: geoRateLimit}
	r.lookup = r.httpLookup
	return r
}

// httpLookup calls a public GeoIP API with a strict context deadline.
// Uses api.ip2location.io as the primary provider for high-accuracy regional geolocation,
// falling back to ip-api.com if unreachable or failed.
func (r *Resolver) httpLookup(ctx context.Context, ip string) (store.GeoIPEntry, error) {
	// Only a literal IP may enter either lookup URL; anything else could steer the request to another host (SSRF).
	if net.ParseIP(ip) == nil {
		return store.GeoIPEntry{}, fmt.Errorf("invalid ip for geo lookup")
	}
	entry, err := r.httpLookupIP2Location(ctx, ip)
	if err == nil {
		return entry, nil
	}
	return r.httpLookupIPAPI(ctx, ip)
}

func (r *Resolver) httpLookupIP2Location(ctx context.Context, ip string) (store.GeoIPEntry, error) {
	ctx, cancel := context.WithTimeout(ctx, geoTimeout)
	defer cancel()
	url := geoLookupURL + ip
	if key := os.Getenv("IP2LOCATION_API_KEY"); key != "" {
		url += "&key=" + neturl.QueryEscape(key)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil) // #nosec G704 -- ip validated by net.ParseIP in httpLookup; URL host is the constant geo API
	if err != nil {
		return store.GeoIPEntry{}, err
	}
	resp, err := r.http.Do(req) // #nosec G704 -- ip validated by net.ParseIP in httpLookup; URL host is the constant geo API
	if err != nil {
		return store.GeoIPEntry{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return store.GeoIPEntry{}, fmt.Errorf("ip2location http %d", resp.StatusCode)
	}
	var body struct {
		IP          string  `json:"ip"`
		CountryCode string  `json:"country_code"`
		RegionName  string  `json:"region_name"`
		CityName    string  `json:"city_name"`
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
		AS          string  `json:"as"`
		ASN         string  `json:"asn"`
		Error       *struct {
			ErrorCode    int    `json:"error_code"`
			ErrorMessage string `json:"error_message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return store.GeoIPEntry{}, err
	}
	if body.Error != nil && body.Error.ErrorMessage != "" {
		return store.GeoIPEntry{}, fmt.Errorf("ip2location error: %s", body.Error.ErrorMessage)
	}
	if body.Latitude < -90 || body.Latitude > 90 || body.Longitude < -180 || body.Longitude > 180 ||
		(body.Latitude == 0 && body.Longitude == 0) {
		return store.GeoIPEntry{}, fmt.Errorf("ip2location invalid coordinates (%v,%v)", body.Latitude, body.Longitude)
	}
	org := body.AS
	if org == "" {
		org = body.ASN
	}
	return store.GeoIPEntry{
		IP:          ip,
		Lat:         body.Latitude,
		Lon:         body.Longitude,
		City:        body.CityName,
		CountryCode: body.CountryCode,
		Region:      body.RegionName,
		ASNOrg:      org,
		Source:      "geoip",
	}, nil
}

func (r *Resolver) httpLookupIPAPI(ctx context.Context, ip string) (store.GeoIPEntry, error) {
	ctx, cancel := context.WithTimeout(ctx, geoTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, geoBackupURL+ip+"?fields=status,message,countryCode,regionName,city,lat,lon,query,isp,org,as", nil)
	if err != nil {
		return store.GeoIPEntry{}, err
	}
	resp, err := r.http.Do(req)
	if err != nil {
		return store.GeoIPEntry{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return store.GeoIPEntry{}, fmt.Errorf("geoip http %d", resp.StatusCode)
	}
	var body struct {
		Status      string  `json:"status"`
		CountryCode string  `json:"countryCode"`
		RegionName  string  `json:"regionName"`
		City        string  `json:"city"`
		Lat         float64 `json:"lat"`
		Lon         float64 `json:"lon"`
		Query       string  `json:"query"`
		ISP         string  `json:"isp"`
		Org         string  `json:"org"`
		AS          string  `json:"as"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return store.GeoIPEntry{}, err
	}
	if !strings.EqualFold(body.Status, "success") {
		return store.GeoIPEntry{}, fmt.Errorf("geoip status %q", body.Status)
	}
	if body.Lat < -90 || body.Lat > 90 || body.Lon < -180 || body.Lon > 180 ||
		(body.Lat == 0 && body.Lon == 0) {
		return store.GeoIPEntry{}, fmt.Errorf("geoip invalid coordinates (%v,%v)", body.Lat, body.Lon)
	}
	org := body.Org
	if org == "" {
		org = body.ISP
	}
	if org == "" {
		org = body.AS
	}
	return store.GeoIPEntry{
		IP: ip, Lat: body.Lat, Lon: body.Lon, City: body.City,
		CountryCode: body.CountryCode, Region: body.RegionName, ASNOrg: org, Source: "geoip",
	}, nil
}

// ResolveIP returns the cached location for an IP, refreshing it only when stale. It never fails on an offline
// lookup: a stale cached entry is returned as-is, and an unknown IP falls back to the Gateway. Private / CGNAT /
// loopback / link-local addresses are NEVER sent to an external provider, even when called directly: they get the
// Gateway fallback immediately. GeoIP is IP-based and inherently approximate; no physical precision is implied.
func (r *Resolver) ResolveIP(ctx context.Context, ip string) store.GeoIPEntry {
	ip = strings.TrimSpace(ip)
	parsed := net.ParseIP(ip)
	if ip == "" || parsed == nil || isPrivateIP(parsed) {
		return gatewayLocation(ip)
	}
	now := time.Now()
	if entry, fresh, err := r.db.GetGeoIP(ip, now); err == nil && entry != nil {
		if fresh {
			return *entry
		}
		// Stale: try a refresh, but keep the old value if it fails (offline-safe).
		if refreshed, err := r.lookupRateLimited(ctx, ip); err == nil {
			_ = r.db.UpsertGeoIP(refreshed, now)
			return refreshed
		}
		return *entry
	}
	if entry, err := r.lookupRateLimited(ctx, ip); err == nil {
		_ = r.db.UpsertGeoIP(entry, now)
		return entry
	}
	return gatewayLocation(ip)
}

// ResolveHost resolves a host's current public IP into the cache so a newly added or relocated host gets a fresh
// position. Manual geo overrides win and are never looked up. The cache is keyed by IP, so a changed IP gets its own
// resolution and an old cached IP can never override the new one; a private IP is never sent externally. Intended to
// be called in a background goroutine from the host write handlers, never on the HTTP read path.
func (r *Resolver) ResolveHost(ctx context.Context, h store.Host) store.GeoIPEntry {
	if h.GeoLat != 0 || h.GeoLon != 0 || h.GeoLabel != "" || h.GeoRegion != "" {
		return store.GeoIPEntry{} // manual override: no lookup, node-level precedence already handles it
	}
	ip := publicIPForHost(h)
	parsed := net.ParseIP(ip)
	if parsed == nil || isPrivateIP(parsed) {
		return gatewayLocation(ip) // private / air-gapped: never leaves the machine
	}
	return r.ResolveIP(ctx, ip)
}

// lookupRateLimited spaces external calls so a fleet of hosts cannot burst the provider. The spacing wait is
// interruptible via ctx. The mutex serialises the clock so concurrent ResolveHost calls (host writes) and the
// background RunOnce cannot collapse into a burst; the external lookup itself runs outside the lock.
func (r *Resolver) lookupRateLimited(ctx context.Context, ip string) (store.GeoIPEntry, error) {
	space := r.minSpace
	if space <= 0 {
		space = geoRateLimit
	}
	r.mu.Lock()
	wait := time.Until(r.lastCall.Add(space))
	if wait > 0 {
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			r.mu.Unlock()
			return store.GeoIPEntry{}, ctx.Err()
		case <-t.C:
		}
	}
	r.lastCall = time.Now()
	r.mu.Unlock()
	return r.lookup(ctx, ip)
}

// RunOnce resolves every host's current public IP into the cache. It is non-blocking with respect to the globe read
// path and respects ctx cancellation. Intended to run from a background goroutine at startup and on a slow ticker.
// Stale cache rows are deliberately kept: an offline deployment must never lose a pin it already knew.
func (r *Resolver) RunOnce(ctx context.Context) error {
	hosts, err := r.db.ListHosts()
	if err != nil {
		return err
	}
	for _, h := range hosts {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_ = r.ResolveHost(ctx, h) // skips manual overrides and private IPs; keyed by the host's current IP
	}
	return nil
}

// ResolveGateway detects this machine's egress public IP and resolves its location into the cache under "gateway".
// If already cached and fresh, it returns the cached entry. If offline or detection fails, it falls back to gatewayLocation.
func (r *Resolver) ResolveGateway(ctx context.Context) store.GeoIPEntry {
	now := time.Now()
	if entry, fresh, err := r.db.GetGeoIP("gateway", now); err == nil && entry != nil {
		if fresh || entry.Source == "manual" {
			return *entry
		}
		// Stale non-manual: try a refresh
		if ip := r.detectEgressIP(ctx); ip != "" {
			if refreshed, err := r.lookupRateLimited(ctx, ip); err == nil {
				refreshed.IP = "gateway"
				refreshed.Source = "egress"
				_ = r.db.UpsertGeoIP(refreshed, now)
				return refreshed
			}
		}
		return *entry
	}
	ip := r.detectEgressIP(ctx)
	if ip == "" {
		return gatewayLocation("gateway")
	}
	parsed := net.ParseIP(ip)
	if parsed == nil || isPrivateIP(parsed) {
		return gatewayLocation("gateway")
	}
	if entry, err := r.lookupRateLimited(ctx, ip); err == nil {
		entry.IP = "gateway"
		entry.Source = "egress"
		_ = r.db.UpsertGeoIP(entry, now)
		return entry
	}
	return gatewayLocation("gateway")
}

// publicIPForHost picks the configured public IP, then the egress-detected one
// stored by inspection. Manual geo override takes precedence at the node level.
func publicIPForHost(h store.Host) string {
	if ip := strings.TrimSpace(h.PublicIP); ip != "" {
		return ip
	}
	return strings.TrimSpace(h.Host)
}

// gatewayLocation is the offline fallback for an IP with no cached location.
func gatewayLocation(ip string) store.GeoIPEntry {
	return store.GeoIPEntry{IP: ip, Lat: gatewayLat, Lon: gatewayLon, City: gatewayCity, CountryCode: gatewayCountry, Source: "gateway"}
}

// isPrivateIP reports whether an IP is RFC1918, CGNAT, loopback, link-local,
// or otherwise not a public address. Shared with the fallback decision.
func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() {
		return true
	}
	// CGNAT 100.64.0.0/10
	_, cgnat, _ := net.ParseCIDR("100.64.0.0/10")
	return cgnat.Contains(ip)
}

// detectEgressIP asks a lightweight public service for this machine's egress IP.
// Used only as a last resort when a host has no configured public IP; failures
// return empty so the caller falls back to the Gateway location.
func (r *Resolver) detectEgressIP(ctx context.Context) string {
	urls := []string{r.egressURL}
	if r.egressURL == "" {
		urls = []string{"https://api.ipify.org", "https://icanhazip.com"}
	}
	for _, u := range urls {
		if u == "" {
			continue
		}
		if ip := r.queryEgressURL(ctx, u); ip != "" {
			return ip
		}
	}
	return ""
}

func (r *Resolver) queryEgressURL(ctx context.Context, u string) string {
	ctx, cancel := context.WithTimeout(ctx, egressTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return ""
	}
	resp, err := r.http.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	buf := make([]byte, 64)
	n, _ := resp.Body.Read(buf)
	ip := strings.TrimSpace(string(buf[:n]))
	if net.ParseIP(ip) == nil {
		return ""
	}
	return ip
}
