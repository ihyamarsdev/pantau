package vpnnet

import (
	"context"
	"fmt"
)

// netmakerDriver drives one netclient daemon. netclient joins many networks at once (one interface each),
// so two Netmaker servers need no instance handling.
type netmakerDriver struct{}

func (netmakerDriver) DefaultIface(n Network) string { return n.cfg("interface", "netmaker") }

func (netmakerDriver) Up(ctx context.Context, m *Manager, n Network) error {
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
	out, err := m.runCmd(ctx, upTimeout, "netclient", args...)
	m.logf(n, "netclient join: %s", out)
	if err != nil {
		return err
	}
	return nil
}

func (netmakerDriver) Down(ctx context.Context, m *Manager, n Network) error {
	netName := n.cfg("network", "")
	if netName == "" {
		return fmt.Errorf("netclient needs the network name to disconnect: set \"network\"")
	}
	out, err := m.runCmd(ctx, downTimeout, "netclient", "disconnect", netName)
	m.logf(n, "netclient disconnect: %s", out)
	return err
}

func (d netmakerDriver) Status(ctx context.Context, m *Manager, n Network) (Status, error) {
	iface := d.DefaultIface(n)
	if !ifaceReady(iface) {
		return Status{State: StateDown, Iface: iface, Detail: "interface down", Peers: -1}, nil
	}
	// netclient's own listing needs root; the interface carrying addresses is the vendor-neutral proof.
	return Status{State: StateUp, Iface: iface, Detail: "interface up", Peers: -1}, nil
}
