package vpnnet

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var openvpnDevRe = regexp.MustCompile(`TUN/TAP device (\S+) opened`)

// openvpnDriver keeps one openvpn process per network: Pantau writes the .ovpn, spawns it, and derives the
// tun device from the process log (a config with a bare `dev tun` gets tun0/tun1 dynamically).
type openvpnDriver struct{}

func (openvpnDriver) DefaultIface(n Network) string { return n.cfg("iface", "") }

func (d openvpnDriver) configPath(n Network) string { return filepath.Join(n.Root, "client.ovpn") }

func (d openvpnDriver) Up(ctx context.Context, m *Manager, n Network) error {
	cfg := n.Config["config"]
	if strings.TrimSpace(cfg) == "" {
		return fmt.Errorf("openvpn needs the .ovpn file content")
	}
	if err := os.WriteFile(d.configPath(n), []byte(cfg), 0o600); err != nil {
		return fmt.Errorf("write client.ovpn: %w", err)
	}
	args := []string{"--config", d.configPath(n), "--auth-nocache", "--log-append", filepath.Join(n.Root, "openvpn.log")}
	if user := n.cfg("username", ""); user != "" {
		auth := filepath.Join(n.Root, "auth.txt")
		if err := os.WriteFile(auth, []byte(user+"\n"+n.cfg("password", "")+"\n"), 0o600); err != nil {
			return fmt.Errorf("write auth.txt: %w", err)
		}
		args = append(args, "--auth-user-pass", auth)
	}
	if err := m.spawn(n, "openvpn", args...); err != nil {
		return err
	}
	if iface := d.iface(n, m); iface != "" {
		waitIface(ctx, iface, 30*time.Second)
	}
	return nil
}

func (openvpnDriver) Down(ctx context.Context, m *Manager, n Network) error {
	m.stopChild(n.ID)
	return nil
}

func (d openvpnDriver) Status(ctx context.Context, m *Manager, n Network) (Status, error) {
	iface := d.iface(n, m)
	if iface != "" && ifaceReady(iface) {
		return Status{State: StateUp, Iface: iface, Detail: "tun device up"}, nil
	}
	if !m.childRunning(n.ID) {
		return Status{State: StateDown, Iface: iface, Detail: "process not running"}, nil
	}
	return Status{State: StateStarting, Iface: iface, Detail: "waiting for tunnel device"}, nil
}

// iface prefers the configured device, then the last "TUN/TAP device X opened" line of the process log.
func (d openvpnDriver) iface(n Network, m *Manager) string {
	if v := n.cfg("iface", ""); v != "" {
		return v
	}
	lines := m.Logs(n.ID)
	for i := len(lines) - 1; i >= 0; i-- {
		if match := openvpnDevRe.FindStringSubmatch(lines[i]); match != nil {
			return match[1]
		}
	}
	return ""
}
