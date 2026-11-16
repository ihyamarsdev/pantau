package vpnnet

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"time"
)

// netbirdStatusJSON is the subset of `netbird status --json` (v0.64) Pantau reads.
type netbirdStatusJSON struct {
	Management struct {
		Connected bool `json:"connected"`
	} `json:"management"`
	Signal struct {
		Connected bool `json:"connected"`
	} `json:"signal"`
	Peers struct {
		Total     int `json:"total"`
		Connected int `json:"connected"`
	} `json:"peers"`
	FQDN       string   `json:"fqdn"`
	NetbirdIP  string   `json:"netbirdIp"`
	DNSServers []string `json:"dnsServers"`
}

// netbirdDriver covers both NetBird layouts: shared (the installed daemon, one active account per device)
// and instance (Pantau runs its own daemon per network, which is the only way to be on two accounts at once).
type netbirdDriver struct{}

func (netbirdDriver) DefaultIface(n Network) string { return n.cfg("interface_name", "wt0") }

func (d netbirdDriver) instance(n Network) bool { return n.cfg("mode", "shared") == "instance" }

// daemonAddr is the CLI's handle on its daemon: a unix socket for an instance, the default socket for shared mode.
func (d netbirdDriver) daemonAddr(n Network) string {
	if !d.instance(n) {
		return n.cfg("daemon_socket", "")
	}
	if runtime.GOOS == "windows" {
		return "npipe://pantau-vpn-" + strconv.FormatInt(n.ID, 10)
	}
	return "unix://" + n.Root + "/nb.sock"
}

func (d netbirdDriver) statusArgs(n Network) []string {
	args := []string{"status", "--json"}
	if addr := d.daemonAddr(n); addr != "" {
		args = append(args, "--daemon-addr", addr)
	}
	return args
}

func (d netbirdDriver) Up(ctx context.Context, m *Manager, n Network) error {
	if d.instance(n) {
		addr := d.daemonAddr(n)
		if !m.childRunning(n.ID) {
			if err := m.spawn(n, "netbird", "service", "run",
				"--config", n.Root+"/config.json",
				"--daemon-addr", addr,
				"--log-file", n.Root+"/netbird.log"); err != nil {
				return err
			}
			// The daemon needs a moment before its socket accepts the CLI.
			if runtime.GOOS != "windows" {
				deadline := time.Now().Add(15 * time.Second)
				for time.Now().Before(deadline) {
					if _, err := os.Stat(n.Root + "/nb.sock"); err == nil {
						break
					}
					time.Sleep(200 * time.Millisecond)
				}
			}
		}
	}
	args := []string{"up", "--interface-name", d.DefaultIface(n)}
	if addr := d.daemonAddr(n); addr != "" {
		args = append(args, "--daemon-addr", addr)
	}
	if k := n.Config["setup_key"]; k != "" {
		args = append(args, "--setup-key", k)
	}
	if v := n.cfg("management_url", ""); v != "" {
		args = append(args, "--management-url", v)
	}
	if v := n.cfg("hostname", ""); v != "" {
		args = append(args, "--hostname", v)
	}
	if d.instance(n) {
		args = append(args, "--wireguard-port", n.cfg("wireguard_port", "51820"))
		// Two instances must never race on the host resolver.
		args = append(args, "--disable-dns")
		if n.cfg("disable_firewall", "") == "true" {
			args = append(args, "--disable-firewall")
		}
	}
	_, err := m.runCmd(ctx, upTimeout, "netbird", args...)
	return err
}

func (d netbirdDriver) Down(ctx context.Context, m *Manager, n Network) error {
	args := []string{"down"}
	if addr := d.daemonAddr(n); addr != "" {
		args = append(args, "--daemon-addr", addr)
	}
	out, err := m.runCmd(ctx, downTimeout, "netbird", args...)
	m.logf(n, "netbird down: %s", out)
	m.stopChild(n.ID)
	return err
}

func (d netbirdDriver) Status(ctx context.Context, m *Manager, n Network) (Status, error) {
	if d.instance(n) && !m.childRunning(n.ID) {
		return Status{State: StateDown, Iface: d.DefaultIface(n), Detail: "instance not running"}, nil
	}
	out, err := m.runCmd(ctx, statusTimeout, "netbird", d.statusArgs(n)...)
	if err != nil {
		return Status{}, err
	}
	var st netbirdStatusJSON
	if err := json.Unmarshal([]byte(jsonBody(out)), &st); err != nil {
		return Status{}, fmt.Errorf("parse netbird status: %w (%s)", err, firstLine(out))
	}
	state := StateDown
	if st.Management.Connected && st.Signal.Connected {
		state = StateUp
	}
	detail := st.FQDN
	if st.NetbirdIP != "" {
		detail = fmt.Sprintf("%s (%s)", detail, st.NetbirdIP)
	}
	if st.Peers.Total > 0 {
		detail = fmt.Sprintf("%s, %d/%d peers", detail, st.Peers.Connected, st.Peers.Total)
	}
	return Status{State: state, Iface: d.DefaultIface(n), Detail: detail, Peers: st.Peers.Connected, DNS: st.DNSServers}, nil
}
