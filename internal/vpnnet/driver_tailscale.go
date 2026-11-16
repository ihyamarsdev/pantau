package vpnnet

import (
	"context"
	"encoding/json"
	"fmt"
)

// tailscaleStatusJSON is the subset of `tailscale status --json` Pantau reads.
type tailscaleStatusJSON struct {
	BackendState string   `json:"BackendState"`
	TailscaleIPs []string `json:"TailscaleIPs"`
	Peer         map[string]struct {
		Online bool `json:"Online"`
	} `json:"Peer"`
}

// tailscaleDriver drives one tailscaled. A tailnet per instance: two tailnets need two sockets (see socket config).
type tailscaleDriver struct{}

func (tailscaleDriver) DefaultIface(n Network) string { return n.cfg("interface_name", "tailscale0") }

func (tailscaleDriver) socketArgs(n Network) []string {
	if s := n.cfg("socket", ""); s != "" {
		return []string{"--socket", s}
	}
	return nil
}

func (d tailscaleDriver) Up(ctx context.Context, m *Manager, n Network) error {
	args := append(d.socketArgs(n), "up", "--authkey", n.cfg("authkey", ""))
	if v := n.cfg("login_server", ""); v != "" {
		args = append(args, "--login-server", v)
	}
	if v := n.cfg("hostname", ""); v != "" {
		args = append(args, "--hostname", v)
	}
	bin, err := m.vendorBin(n)
	if err != nil {
		return err
	}
	_, err = m.runCmd(ctx, upTimeout, bin, args...)
	return err
}

func (d tailscaleDriver) Down(ctx context.Context, m *Manager, n Network) error {
	bin, err := m.vendorBin(n)
	if err != nil {
		return err
	}
	_, err = m.runCmd(ctx, downTimeout, bin, append(d.socketArgs(n), "down")...)
	return err
}

func (d tailscaleDriver) Status(ctx context.Context, m *Manager, n Network) (Status, error) {
	bin, err := m.vendorBin(n)
	if err != nil {
		return Status{State: StateDown, Iface: d.DefaultIface(n), Detail: err.Error()}, nil
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
