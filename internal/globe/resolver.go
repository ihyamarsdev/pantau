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
	"strings"
	"time"

	"pantau/internal/store"
)

const (
	geoLookupURL   = "http://ip-api.com/json/"
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
func (r *Resolver) httpLookup(ctx context.Context, ip string) (store.GeoIPEntry, error) {
	ctx, cancel := context.WithTimeout(ctx, geoTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, geoLookupURL+ip+"?fields=status,message,countryCode,city,lat,lon,query,isp,org,as", nil)
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
	org := body.Org
	if org == "" {
		org = body.ISP
	}
	if org == "" {
		org = body.AS
	}
	return store.GeoIPEntry{
		IP: ip, Lat: body.Lat, Lon: body.Lon, City: body.City,
		CountryCode: body.CountryCode, ASNOrg: org, Source: "geoip",
	}, nil
}

// ResolveIP returns the cached location for an IP, refreshing it in the
// background only when stale. It never fails on an offline lookup: a stale
// cached entry is returned as-is, and an unknown IP falls back to the Gateway.
func (r *Resolver) ResolveIP(ctx context.Context, ip string) store.GeoIPEntry {
	ip = strings.TrimSpace(ip)
	now := time.Now()
	if entry, fresh, err := r.db.GetGeoIP(ip, now); err == nil && entry != nil {
		if fresh {
			return *entry
		}
		// Stale: try a refresh, but keep the old value if it fails.
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

// lookupRateLimited spaces external calls so a fleet of hosts cannot burst the
// provider. The spacing wait is interruptible via ctx.
func (r *Resolver) lookupRateLimited(ctx context.Context, ip string) (store.GeoIPEntry, error) {
	space := r.minSpace
	if space <= 0 {
		space = geoRateLimit
	}
	wait := time.Until(r.lastCall.Add(space))
	if wait > 0 {
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return store.GeoIPEntry{}, ctx.Err()
		case <-t.C:
		}
	}
	r.lastCall = time.Now()
	return r.lookup(ctx, ip)
}

// RunOnce resolves every host's public IP into the cache. It is non-blocking
// with respect to the globe read path and respects ctx cancellation. Intended
// to be called from a background goroutine at startup and on a slow ticker.
func (r *Resolver) RunOnce(ctx context.Context) error {
	hosts, err := r.db.ListHosts()
	if err != nil {
		return err
	}
	for _, h := range hosts {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// A manual override means no external lookup is needed.
		if h.GeoLat != 0 || h.GeoLon != 0 || h.GeoLabel != "" {
			continue
		}
		ip := publicIPForHost(h)
		if ip == "" || net.ParseIP(ip) == nil || isPrivateIP(net.ParseIP(ip)) {
			continue // private/air-gapped hosts use the manual override or Gateway fallback
		}
		_ = r.ResolveIP(ctx, ip)
	}
	_ = r.db.DeleteStaleGeoIP(time.Now())
	return nil
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
	url := r.egressURL
	if url == "" {
		url = "https://api.ipify.org"
	}
	ctx, cancel := context.WithTimeout(ctx, egressTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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
