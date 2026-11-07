package web

import (
	"strings"
	"testing"
)

// The Terraform section shows only a status pill in its header; the gate details (security warning, binaries,
// enable/disable) live in the Terraform Gate tab. A banner above the tabs was removed on purpose.
func TestTerraformGateHasNoBannerAboveTabs(t *testing.T) {
	html := string(embeddedHTML)
	for _, id := range []string{`id="tfDrawerRoot"`, `id="tfDrawer"`, `id="tfDrawerWide"`, `id="tfWsModalName"`, `id="tfWsHeadBadges"`, `id="tfWsTabs"`, `id="tfWsBody"`} {
		if !strings.Contains(html, id) {
			t.Errorf("missing Terraform drawer element %s", id)
		}
	}
	if !strings.Contains(html, `href="/app/terraform.css"`) {
		t.Error("Terraform drawer stylesheet is not linked")
	}
	if strings.Contains(html, `id="tfWsModal"`) {
		t.Error("Terraform workspace must use the drawer, not the legacy modal")
	}
	if strings.Contains(html, `id="tfGateBanner"`) {
		t.Fatal("Terraform Gate banner must not be rendered above the tabs")
	}
	for _, id := range []string{`id="tfGateStatus"`, `id="tfGateWarn"`, `id="btnTfToggleGate"`, `id="tfGatePanelStatusList"`} {
		if !strings.Contains(html, id) {
			t.Errorf("missing %s in the Terraform Gate UI", id)
		}
	}
}

func TestTerraformDrawerTabsRegistered(t *testing.T) {
	html := string(embeddedHTML)
	// tfTabRenderers and tfDirtyGuard globals must exist
	if !strings.Contains(html, "const tfTabRenderers = {}") {
		t.Error("tfTabRenderers registry not declared")
	}
	if !strings.Contains(html, "let tfDirtyGuard = null") {
		t.Error("tfDirtyGuard not declared")
	}
	// /app/terraform.js script tag
	if !strings.Contains(html, `<script src="/app/terraform.js"></script>`) {
		t.Error("/app/terraform.js script tag missing")
	}
	// tab order: overview, config, runs, state, variables, drift, settings
	if !strings.Contains(html, `['overview', 'tf_tab_overview'], ['config', 'tf_tab_config'], ['runs', 'tf_tab_runs'], ['state', 'tf_tab_state'], ['variables', 'tf_tab_variables'], ['drift', 'tf_tab_drift'], ['settings', 'tf_tab_settings']`) {
		t.Error("TF_WS_TABS does not contain expected tab order (overview, config, runs, state, variables, drift, settings)")
	}
	// tf_tab_config in both EN and ID
	enMatches := strings.Count(html, `"tf_tab_config": "Configuration"`)
	idMatches := strings.Count(html, `"tf_tab_config": "Konfigurasi"`)
	if enMatches < 1 {
		t.Error("tf_tab_config EN missing")
	}
	if idMatches < 1 {
		t.Error("tf_tab_config ID missing")
	}
	// guarded tab switching
	if !strings.Contains(html, "tfSwitchTabGuarded") {
		t.Error("tfSwitchTabGuarded not defined")
	}
	if !strings.Contains(html, "tfConfirmLeave") {
		t.Error("tfConfirmLeave not defined")
	}
}

// Cloud Accounts tab hooks, i18n, overview row and the centered Runs-tab start button.
func TestTerraformCloudTabHooks(t *testing.T) {
	html := string(embeddedHTML)
	for _, s := range []string{
		`id="tabTfCloud"`, `data-tab="cloud"`, `aria-controls="tfPanelCloud"`, `id="tfPanelCloud"`, `id="tfCloudRoot"`,
		`'varsets', 'cloud', 'policies'`, `tfCloudRefresh`, `id="tfRunStartBar"`, `id="tfRunStartBtn"`,
		`"tf_tab_cloud": "Cloud Accounts"`, `"tf_tab_cloud": "Akun Cloud"`,
		`"tf_ov_cloud": "Cloud accounts"`, `"tf_ov_cloud": "Akun cloud"`,
	} {
		if !strings.Contains(html, s) {
			t.Errorf("index.html missing %s", s)
		}
	}
	if strings.Contains(html, `id="tfRunStartBtn" class="btn btn-sm btn-primary" style="width:100%`) {
		t.Error("Start Run button must not be full width")
	}
}
