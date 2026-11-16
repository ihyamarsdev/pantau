package vpnnet

import (
	"context"
	"fmt"
	"strings"
)

// customDriver runs admin-written commands behind the gate; the only place Pantau executes a shell string.
type customDriver struct{}

func (customDriver) DefaultIface(n Network) string { return n.Iface }

func (customDriver) Up(ctx context.Context, m *Manager, n Network) error {
	cmd := n.cfg("up", "")
	if cmd == "" {
		return fmt.Errorf("custom driver needs an up command")
	}
	out, err := m.runShell(ctx, upTimeout, cmd)
	m.logf(n, "up: %s", out)
	return err
}

func (customDriver) Down(ctx context.Context, m *Manager, n Network) error {
	cmd := n.cfg("down", "")
	if cmd == "" {
		return nil
	}
	out, err := m.runShell(ctx, downTimeout, cmd)
	m.logf(n, "down: %s", out)
	return err
}

func (d customDriver) Status(ctx context.Context, m *Manager, n Network) (Status, error) {
	cmd := n.cfg("status", "")
	if cmd == "" {
		if ifaceReady(n.Iface) {
			return Status{State: StateUp, Iface: n.Iface, Detail: "interface up"}, nil
		}
		return Status{State: StateDown, Iface: n.Iface, Detail: "interface down"}, nil
	}
	out, err := m.runShell(ctx, statusTimeout, cmd)
	if err != nil {
		return Status{State: StateDown, Iface: n.Iface, Detail: strings.TrimSpace(out)}, nil
	}
	return Status{State: StateUp, Iface: n.Iface, Detail: strings.TrimSpace(out)}, nil
}
