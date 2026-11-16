package vpnnet

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// wireguardDriver keeps one wg-quick interface per network; the config file name becomes the interface name.
type wireguardDriver struct{}

func (wireguardDriver) DefaultIface(n Network) string { return n.cfg("interface_name", "wg0") }

func (d wireguardDriver) configPath(n Network) string {
	return filepath.Join(n.Root, d.DefaultIface(n)+".conf")
}

func (d wireguardDriver) Up(ctx context.Context, m *Manager, n Network) error {
	cfg := n.Config["config"]
	if strings.TrimSpace(cfg) == "" {
		return fmt.Errorf("wireguard needs the .conf file content")
	}
	if err := os.WriteFile(d.configPath(n), []byte(cfg), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", d.configPath(n), err)
	}
	out, err := m.runCmd(ctx, upTimeout, "wg-quick", "up", d.configPath(n))
	m.logf(n, "wg-quick up: %s", out)
	if err != nil {
		return err
	}
	waitIface(ctx, d.DefaultIface(n), 15*time.Second)
	return nil
}

func (d wireguardDriver) Down(ctx context.Context, m *Manager, n Network) error {
	out, err := m.runCmd(ctx, downTimeout, "wg-quick", "down", d.configPath(n))
	m.logf(n, "wg-quick down: %s", out)
	return err
}

func (d wireguardDriver) Status(ctx context.Context, m *Manager, n Network) (Status, error) {
	iface := d.DefaultIface(n)
	if ifaceReady(iface) {
		return Status{State: StateUp, Iface: iface, Detail: "interface up"}, nil
	}
	return Status{State: StateDown, Iface: iface, Detail: "interface down"}, nil
}
