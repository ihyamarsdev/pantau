package vpnnet

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"time"
)

// tailscaleStatusJSON is the subset of `tailscale status --json` Pantau reads.
type tailscaleStatusJSON struct {
	BackendState string   `json:"BackendState"`
	AuthURL      string   `json:"AuthURL"`
	TailscaleIPs []string `json:"TailscaleIPs"`
	Peer         map[string]struct {
		Online bool `json:"Online"`
	} `json:"Peer"`
}

// tailscaleDriver drives one tailscaled. One tailnet per instance: two tailnets need two daemons,
// each with its own socket, and with daemon "auto" Pantau starts the second one itself.
type tailscaleDriver struct{}

func (tailscaleDriver) DefaultIface(n Network) string { return n.cfg("interface_name", "tailscale0") }

// externalDaemon reports whether this network must use a daemon it does not own: an explicit socket
// with daemon "external", or the host's default socket when daemon is unset and it already exists.
func (d tailscaleDriver) socketPath(n Network) string {
	if s := n.cfg("socket", ""); s != "" {
		return s
	}
	return n.Root + "/tailscaled.sock"
}

func (d tailscaleDriver) useExternalDaemon(n Network) bool {
	if n.cfg("daemon", "auto") == "external" {
		return true
	}
	if s := n.cfg("socket", ""); s != "" {
		// An explicit socket is either a shared daemon or ours; only spawn when nothing listens there.
		return socketListening(s)
	}
	return socketListening("/var/run/tailscale/tailscaled.sock")
}

func (d tailscaleDriver) socketArgs(n Network) []string {
	if d.useExternalDaemon(n) {
		if s := n.cfg("socket", ""); s != "" {
			return []string{"--socket", s}
		}
		return nil // the host's default socket
	}
	return []string{"--socket", d.socketPath(n)}
}

// Up starts tailscaled when nobody is serving the socket (a container has none), then joins the tailnet.
func (d tailscaleDriver) Up(ctx context.Context, m *Manager, n Network) error {
	bin, err := m.vendorBin(n)
	if err != nil {
		return err
	}
	if !d.useExternalDaemon(n) && !m.childRunning(n.ID) {
		if err := os.MkdirAll(n.Root, 0o700); err != nil {
			return err
		}
		sock := d.socketPath(n)
		args := []string{
			"--state=" + n.Root + "/tailscaled.state",
			"--socket=" + sock,
			"--tun=" + d.DefaultIface(n),
		}
		if runtime.GOOS == "windows" {
			return fmt.Errorf("running a tailscaled instance is not supported on windows: start the service and set socket, or use daemon \"external\"")
		}
		daemon, err := m.daemonBin(n)
		if err != nil {
			return err
		}
		if err := m.spawn(n, daemon, args...); err != nil {
			return err
		}
		if !waitSocket(ctx, sock, 15*time.Second) {
			return fmt.Errorf("tailscaled did not start: no socket at %s", sock)
		}
	}
	// Without any saved login, "tailscale up" blocks on the interactive login flow for a minute and
	// prints a URL nobody sees: fail fast and hand that URL to the admin instead.
	if n.cfg("authkey", "") == "" {
		if out, err := m.runCmd(ctx, 5*time.Second, bin, append(d.socketArgs(n), "status", "--json")...); err == nil {
			var probe tailscaleStatusJSON
			if json.Unmarshal([]byte(jsonBody(out)), &probe) == nil && probe.BackendState == "NeedsLogin" {
				if probe.AuthURL != "" {
					return fmt.Errorf("tailscale needs a login: set the network's authkey, or open %s and approve this device", probe.AuthURL)
				}
				return fmt.Errorf("tailscale needs a login: set the network's authkey field")
			}
		}
	}
	ownDaemon := !d.useExternalDaemon(n)
	args := tailscaleUpArgs(n, d.socketArgs(n), ownDaemon)
	_, err = m.runCmd(ctx, upTimeout, bin, args...)
	return err
}

// tailscaleUpArgs builds the "tailscale up" argv. For a daemon Pantau started itself, keep both its
// firewall and its DNS off: this tailscaled shares the box and /etc/resolv.conf with the other VPN
// networks, and Tailscale's defaults drop 100.64.0.0/10 traffic and overwrite the resolver with
// MagicDNS (100.100.100.100), which breaks their management connections. An external daemon is the
// admin's to configure, so none of these flags are applied there.
func tailscaleUpArgs(n Network, socketArgs []string, ownDaemon bool) []string {
	args := append([]string{}, socketArgs...)
	args = append(args, "up", "--authkey", n.cfg("authkey", ""))
	if v := n.cfg("login_server", ""); v != "" {
		args = append(args, "--login-server", v)
	}
	if v := n.cfg("hostname", ""); v != "" {
		args = append(args, "--hostname", v)
	}
	if ownDaemon {
		args = append(args, "--netfilter-mode=off", "--accept-dns=false", "--reset")
	}
	return args
}

func (d tailscaleDriver) Down(ctx context.Context, m *Manager, n Network) error {
	bin, err := m.vendorBin(n)
	if err != nil {
		m.stopChild(n.ID)
		return err
	}
	_, err = m.runCmd(ctx, downTimeout, bin, append(d.socketArgs(n), "down")...)
	m.stopChild(n.ID)
	return err
}

func (d tailscaleDriver) Status(ctx context.Context, m *Manager, n Network) (Status, error) {
	bin, err := m.vendorBin(n)
	if err != nil {
		return Status{State: StateDown, Iface: d.DefaultIface(n), Detail: err.Error()}, nil
	}
	if !d.useExternalDaemon(n) && !m.childRunning(n.ID) {
		return Status{State: StateDown, Iface: d.DefaultIface(n), Detail: "tailscaled not running"}, nil
	}
	out, err := m.runCmd(ctx, statusTimeout, bin, append(d.socketArgs(n), "status", "--json")...)
	if err != nil {
		return Status{}, err
	}
	var st tailscaleStatusJSON
	if err := json.Unmarshal([]byte(jsonBody(out)), &st); err != nil {
		return Status{}, fmt.Errorf("parse tailscale status: %w (%s)", err, firstLine(out))
	}
	state := StateDown
	if st.BackendState == "Running" {
		state = StateUp
	}
	online := 0
	for _, p := range st.Peer {
		if p.Online {
			online++
		}
	}
	detail := st.BackendState
	if len(st.TailscaleIPs) > 0 {
		detail = fmt.Sprintf("%s (%s)", detail, st.TailscaleIPs[0])
	}
	return Status{State: state, Iface: d.DefaultIface(n), Detail: detail, Peers: online}, nil
}
