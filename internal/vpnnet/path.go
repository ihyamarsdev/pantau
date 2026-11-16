// Package vpnnet gives each Host its own network path: a VPN/tunnel interface to bind the SSH dial to.
// Binding to an interface is what makes two VPNs with overlapping routes usable at the same time.
package vpnnet

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"
)

var (
	ErrNetworkDisabled    = errors.New("vpn network disabled")
	ErrNetworkDown        = errors.New("vpn network is down")
	ErrNetworkNotFound    = errors.New("vpn network not found")
	ErrGateDisabled       = errors.New("vpn gate is disabled")
	ErrBindUnsupported    = errors.New("binding to an interface is unsupported on this platform")
	ErrTooManyChildProcs  = errors.New("too many tunnel processes")
	errInterfaceNotExists = errors.New("interface does not exist")
)

// DialFunc is a dial bound to one interface (nil Iface means a plain dial).
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

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
	return d.DialContext(ctx, network, addr)
}

// Resolve looks a host up through the network's own resolver when the driver reported one,
// otherwise through the OS resolver. Queries go out over the bound interface.
func (p *Path) Resolve(ctx context.Context, host string) ([]string, error) {
	if p == nil || len(p.DNS) == 0 {
		return net.DefaultResolver.LookupHost(ctx, host)
	}
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return p.DialContext(ctx, network, net.JoinHostPort(p.DNS[0], "53"))
		},
	}
	return r.LookupHost(ctx, host)
}

// DialSSHAddr dials host:port over the path, resolving name lookups through the network when needed.
func (p *Path) DialSSHAddr(ctx context.Context, host string, port int, timeout time.Duration) (net.Conn, error) {
	addr := net.JoinHostPort(host, fmt.Sprint(port))
	return p.DialContext(ctx, "tcp", addr)
}
