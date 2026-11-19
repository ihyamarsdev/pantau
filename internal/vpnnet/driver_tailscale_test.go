package vpnnet

import (
	"reflect"
	"testing"
)

// A Tailscale Pantau starts itself must not take over the box's resolver: MagicDNS would overwrite
// /etc/resolv.conf and break the other VPN networks' management connections. An external daemon is
// the admin's to configure, so none of the isolation flags are applied there.
func TestTailscaleUpArgsOwnDaemonDisablesDNSAndFirewall(t *testing.T) {
	n := Network{Config: map[string]string{
		"authkey": "key", "login_server": "https://login.example", "hostname": "pantau",
	}}
	got := tailscaleUpArgs(n, []string{"--socket", "/tmp/ts.sock"}, true)
	want := []string{
		"--socket", "/tmp/ts.sock",
		"up", "--authkey", "key",
		"--login-server", "https://login.example",
		"--hostname", "pantau",
		"--netfilter-mode=off", "--accept-dns=false", "--reset",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("own daemon args = %v, want %v", got, want)
	}
}

func TestTailscaleUpArgsExternalDaemonLeavesFlagsAlone(t *testing.T) {
	n := Network{Config: map[string]string{"authkey": "key"}}
	got := tailscaleUpArgs(n, nil, false)
	want := []string{"up", "--authkey", "key"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("external daemon args = %v, want %v", got, want)
	}
}
