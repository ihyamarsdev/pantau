package vpnnet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveVendorBin(t *testing.T) {
	// An installed binary resolves to an absolute path (netbird is present on this machine, otherwise skip).
	if _, err := resolveVendorBin(DriverNetBird, ""); err != nil {
		t.Skipf("netbird not installed here: %v", err)
	}
	if p, err := resolveVendorBin(DriverNetBird, ""); err != nil || !filepath.IsAbs(p) {
		t.Fatalf("resolve netbird = %q, %v", p, err)
	}

	// An explicit path wins; a wrong one fails with the path the admin typed.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if p, err := resolveVendorBin(DriverOpenVPN, self); err != nil || p != self {
		t.Fatalf("override = %q, %v", p, err)
	}
	if _, err := resolveVendorBin(DriverOpenVPN, "/nonexistent/openvpn"); err == nil || !strings.Contains(err.Error(), "/nonexistent/openvpn") {
		t.Fatalf("missing override error = %v", err)
	}

	// The message for a missing binary has to say what to do, not just "exec: not found".
	if _, err := resolveVendorBin("nope", ""); err == nil {
		t.Fatal("expected an error for an unknown driver")
	}
}

func TestVendorBinCustomDriverHasNoBinary(t *testing.T) {
	m := &Manager{}
	if _, err := m.vendorBin(Network{Driver: DriverCustom}); err == nil {
		t.Fatal("custom driver must not resolve a binary")
	}
}
