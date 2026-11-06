package web

import (
	"strings"
	"testing"
)

// The Terraform section shows only a status pill in its header; the gate details (security warning, binaries,
// enable/disable) live in the Terraform Gate tab. A banner above the tabs was removed on purpose.
func TestTerraformGateHasNoBannerAboveTabs(t *testing.T) {
	html := string(embeddedHTML)
	if strings.Contains(html, `id="tfGateBanner"`) {
		t.Fatal("Terraform Gate banner must not be rendered above the tabs")
	}
	for _, id := range []string{`id="tfGateStatus"`, `id="tfGateWarn"`, `id="btnTfToggleGate"`, `id="tfGatePanelStatusList"`} {
		if !strings.Contains(html, id) {
			t.Errorf("missing %s in the Terraform Gate UI", id)
		}
	}
}
