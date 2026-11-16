package vpnnet

import (
	"context"
	"fmt"
	"time"
)

// netmakerDriver drives one netclient daemon. netclient joins many networks at once (one interface each),
// so two Netmaker servers need no per-network daemon — but the daemon itself must be running, and a
// container has none, so with daemon "auto" Pantau starts it when the socket is not there.
type netmakerDriver struct{}

func (netmakerDriver) DefaultIface(n Network) string { return n.cfg("interface", "netmaker") }

// ownsDaemon is true when Pantau runs netclient itself (container case) instead of using a host daemon.
func (d netmakerDriver) ownsDaemon(n Network) bool {
	if n.cfg("daemon", "auto") == "external" {
		return false
	}
	if n.cfg("socket", "") != "" && socketListening(n.cfg("socket", "")) {
		return false
	}
	return true
}

func (d netmakerDriver) ensureDaemon(ctx context.Context, m *Manager, n Network, bin string) error {
	if !d.ownsDaemon(n) || m.childRunning(n.ID) {
		return nil
	}
	// netclient's daemon keeps the WireGuard links in shape; it writes its own config under /etc/netclient.
	if err := m.spawn(n, bin, "daemon"); err != nil {
		return err
	}
	// The daemon has no socket to poll: give it a moment, then let `join` speak to it.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(2 * time.Second):
	}
	return nil
}

func (d netmakerDriver) Up(ctx context.Context, m *Manager, n Network) error {
	bin, err := m.vendorBin(n)
	if err != nil {
		return err
	}
	if err := d.ensureDaemon(ctx, m, n, bin); err != nil {
		return err
	}
	args := []string{"join", "-t", n.cfg("token", "")}
	if v := n.cfg("network", ""); v != "" {
		args = append(args, "-n", v)
	}
	if v := n.cfg("server", ""); v != "" {
		args = append(args, "-s", v)
	}
	if v := n.cfg("interface", ""); v != "" {
		args = append(args, "-I", v)
	}
	out, err := m.runCmd(ctx, upTimeout, bin, args...)
	m.logf(n, "netclient join: %s", out)
	return err
}

func (d netmakerDriver) Down(ctx context.Context, m *Manager, n Network) error {
	netName := n.cfg("network", "")
	if netName == "" {
		return fmt.Errorf("netclient needs the network name to disconnect: set \"network\"")
	}
	bin, err := m.vendorBin(n)
	if err != nil {
		return err
	}
	out, err := m.runCmd(ctx, downTimeout, bin, "disconnect", netName)
	m.logf(n, "netclient disconnect: %s", out)
	if d.ownsDaemon(n) {
		m.stopChild(n.ID)
	}
	return err
}

func (d netmakerDriver) Status(ctx context.Context, m *Manager, n Network) (Status, error) {
	iface := d.DefaultIface(n)
	if !ifaceReady(iface) {
		detail := "interface down"
		if d.ownsDaemon(n) && !m.childRunning(n.ID) {
			detail = "netclient daemon not running"
		}
		return Status{State: StateDown, Iface: iface, Detail: detail, Peers: -1}, nil
	}
	// netclient's own listing needs root; the interface carrying addresses is the vendor-neutral proof.
	return Status{State: StateUp, Iface: iface, Detail: "interface up", Peers: -1}, nil
}
