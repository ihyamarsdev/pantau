package store

import (
	"path/filepath"
	"testing"
)

// TestVPNNetworkHostBindingRoundTrip covers the state a VPN Network needs to survive: the host column,
// the network row with its secret config, and a snapshot restore that keeps both in step.
func TestVPNNetworkHostBindingRoundTrip(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hostID, err := db.CreateHost(&Host{Name: "private", Host: "10.0.0.5", Port: 22, User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	// Existing hosts predate the column and must read as "direct".
	h, err := db.GetHost(hostID)
	if err != nil {
		t.Fatal(err)
	}
	if h.NetworkID != 0 {
		t.Fatalf("new host network_id = %d, want 0", h.NetworkID)
	}

	netID, err := db.CreateVPNNetwork(&VPNNetwork{
		Name: "net-a", Driver: "netbird", Enabled: true, AutoUp: true,
		Config: `{"mode":"instance","setup_key":"secret-key","interface_name":"wt1"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	h.NetworkID = netID
	h.Name = "private-renamed"
	if err := db.UpdateHost(h); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.GetHost(hostID); got == nil || got.NetworkID != netID {
		t.Fatalf("network_id not persisted: %+v", got)
	}

	if err := db.SetVPNNetworkState(netID, "up", "wt1 connected"); err != nil {
		t.Fatal(err)
	}
	counts, err := db.CountHostsByNetwork()
	if err != nil {
		t.Fatal(err)
	}
	if counts[netID] != 1 {
		t.Fatalf("hosts_count = %d, want 1", counts[netID])
	}

	payload, err := db.ExportSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.VPNNetworks) != 1 || payload.VPNNetworks[0].Config == "" {
		t.Fatalf("snapshot lost the network: %+v", payload.VPNNetworks)
	}

	other, err := Open(filepath.Join(dir, "t2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := other.ImportSnapshot(payload); err != nil {
		t.Fatalf("import snapshot: %v", err)
	}
	restored, err := other.GetHost(hostID)
	if err != nil || restored == nil {
		t.Fatalf("restored host: %v", err)
	}
	if restored.NetworkID != netID {
		t.Fatalf("restored network_id = %d, want %d", restored.NetworkID, netID)
	}
	rn, err := other.GetVPNNetwork(netID)
	if err != nil || rn == nil {
		t.Fatalf("restored network: %v", err)
	}
	if rn.Config == "" || !rn.Enabled || !rn.AutoUp {
		t.Fatalf("restored network fields lost: %+v", rn)
	}

	// Deleting a network leaves its hosts on the direct path only when the caller detaches them first.
	if err := db.DetachHostsFromNetwork(netID); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.GetHost(hostID); got == nil || got.NetworkID != 0 {
		t.Fatalf("detach failed: %+v", got)
	}
}
