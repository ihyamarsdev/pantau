// Package vpnnet gives each Host its own network path: a VPN/tunnel interface to bind the SSH dial to.
// Binding to an interface is what makes two VPNs with overlapping routes usable at the same time.
package vpnnet

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)


// DialBudget is how long a dial waits for its network to come up. The bring-up itself carries on past it:
// the Host is reported unreachable now and reachable on the next try, instead of holding a worker slot.
const DialBudget = 30 * time.Second

// dnsCacheTTL bounds how long a name resolved through a network's resolver is trusted. It is short on
// purpose: a stale resolver after a vendor rewrites /etc/resolv.conf was the root of a production outage.
const dnsCacheTTL = 30 * time.Second

// dnsCache is scoped to (network_id, iface, host) so two VPNs with overlapping names never share an entry.
// Entries are invalidated on a network transition via Path.InvalidateDNS.
var dnsCache = struct {
	sync.Mutex
	entries map[dnsCacheKey]dnsCacheEntry
}{entries: map[dnsCacheKey]dnsCacheEntry{}}

type dnsCacheKey struct {
	networkID int64
	iface     string
	host      string
}

type dnsCacheEntry struct {
	addrs []string
	at    time.Time
}

// InvalidateDNS drops every cached lookup for a network (and interface). Called on an up/down
// transition so a resolver change takes effect immediately instead of after the TTL.
func InvalidateDNS(networkID int64, iface string) {
	dnsCache.Lock()
	defer dnsCache.Unlock()
	for k := range dnsCache.entries {
		if k.networkID == networkID && (iface == "" || k.iface == iface) {
			delete(dnsCache.entries, k)
		}
	}
}

func cachedDNS(k dnsCacheKey) ([]string, bool) {
	dnsCache.Lock()
	defer dnsCache.Unlock()
	e, ok := dnsCache.entries[k]
	if !ok || time.Since(e.at) > dnsCacheTTL {
		if ok {
			delete(dnsCache.entries, k)
		}
		return nil, false
	}
	return e.addrs, true
}

func rememberDNS(k dnsCacheKey, addrs []string) {
	dnsCache.Lock()
	defer dnsCache.Unlock()
	dnsCache.entries[k] = dnsCacheEntry{addrs: append([]string(nil), addrs...), at: time.Now()}
}

// DialFunc is a dial bound to one interface (nil Iface means a plain dial).
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

var (
	ErrNetworkDisabled   = errors.New("vpn network disabled")
	ErrNetworkDown       = errors.New("vpn network is down")
	ErrNetworkNotFound   = errors.New("vpn network not found")
	ErrGateDisabled      = errors.New("vpn gate is disabled")
	ErrBindUnsupported   = errors.New("binding to an interface is unsupported on this platform")
	ErrTooManyChildProcs = errors.New("too many tunnel processes")
	// ErrInterfaceNotExists marks a Host's tunnel interface that is gone (the network went down after
	// the dial was planned). Callers classify it as a path failure, never as a dead Host.
	ErrInterfaceNotExists = errors.New("tunnel interface does not exist")
)


// Path is a resolved network path: the interface to bind to and, when the driver reports one, its DNS server.
type Path struct {
	NetworkID int64
	Name      string
	Iface     string
	DNS       []string
}

// DialContext dials through the path's interface. An empty Iface keeps the old behaviour (OS routing table).
func (p *Path) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if p == nil || p.Iface == "" {
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}
	d := net.Dialer{Control: bindControl(p.Iface)}
	conn, err := d.DialContext(ctx, network, addr)
	if err != nil {
		// A tunnel that went down leaves no interface to bind to (ENODEV, or EPERM without CAP_NET_RAW): name it,
		// so the caller reports a dead path instead of a dead Host. Looked up on failure only: the hot path stays free.
		if _, ifErr := net.InterfaceByName(p.Iface); ifErr != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrInterfaceNotExists, p.Iface, err)
		}
	}
	return conn, err
}

// Resolve looks a host up through the network's own resolver when the driver reported one,
// otherwise through the OS resolver. Results for a network resolver are cached per (network_id, iface,
// host) with a short TTL; the cache is invalidated on a network transition via InvalidateDNS. Queries go
// out over the bound interface and honour ctx, so a cancelled dial leaves no socket or goroutine behind.
func (p *Path) Resolve(ctx context.Context, host string) ([]string, error) {
	if p == nil || len(p.DNS) == 0 {
		return net.DefaultResolver.LookupHost(ctx, host)
	}
	key := dnsCacheKey{networkID: p.NetworkID, iface: p.Iface, host: host}
	if addrs, ok := cachedDNS(key); ok {
		return addrs, nil
	}
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return p.DialContext(ctx, network, net.JoinHostPort(p.DNS[0], "53"))
		},
	}
	addrs, err := r.LookupHost(ctx, host)
	if err == nil && len(addrs) > 0 {
		rememberDNS(key, addrs)
	}
	return addrs, err
}

// DialSSHAddr dials host:port over the path, resolving name lookups through the network when needed.
func (p *Path) DialSSHAddr(ctx context.Context, host string, port int, timeout time.Duration) (net.Conn, error) {
	addr := net.JoinHostPort(host, fmt.Sprint(port))
	return p.DialContext(ctx, "tcp", addr)
}
