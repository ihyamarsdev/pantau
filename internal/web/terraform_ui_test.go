package web

import (
	"os"
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

// Variables tab: "Detect from configuration" button, dialog list, add button and i18n (EN+ID).
func TestTerraformVariableDetectUI(t *testing.T) {
	html := string(embeddedHTML)
	for _, s := range []string{
		`id="tfVarDetectBtn"`, `id="tfVarDetectList"`, `id="tfVarDetectAdd"`, `data-vd="`,
		`/variables/detect`, `tfDetectVars()`, `tfDetectAdd()`,
		`tfx_vd_btn: ['Detect from configuration', 'Deteksi dari konfigurasi']`,
		`tfx_vd_title: [`, `tfx_vd_git: [`, `tfx_vd_empty: [`, `tfx_vd_exists: ['Already defined', 'Sudah ada']`,
		`tfx_vd_add: [`, `tfx_vd_added: [`, `tfx_vd_none: [`,
	} {
		if !strings.Contains(html, s) {
			t.Errorf("index.html missing %s", s)
		}
	}
}

// Audit tab, Gate extras / template-picker hooks, widened dialogs and narrow-viewport CSS.
func TestTerraformUIHooksAuditGateTemplates(t *testing.T) {
	html := string(embeddedHTML)
	for _, s := range []string{
		`id="tabTfAudit"`, `aria-controls="tfPanelAudit"`, `id="tfPanelAudit"`, `id="tfAuditRoot"`,
		`'policies', 'modules', 'registry', 'audit', 'gate'`, `tfAuditRefresh`,
		`"tf_tab_audit": "Audit"`,
		`id="tfGateExtras"`, `tfGateExtras(document.getElementById('tfGateExtras'))`,
		`if (!ws && typeof tfTplInject === 'function') tfTplInject();`,
		`'min(' + (width || 560) + 'px, 96vw)'`, `'startTfRun()')), 720)`,
	} {
		if !strings.Contains(html, s) {
			t.Errorf("index.html missing %s", s)
		}
	}
	if n := strings.Count(html, `"tf_tab_audit":`); n != 2 {
		t.Errorf("tf_tab_audit must exist in EN and ID dictionaries, found %d", n)
	}
	if strings.Index(html, `id="tfGateExtras"`) < strings.Index(html, `id="tfPanelGate"`) || strings.Index(html, `id="tfGateExtras"`) > strings.Index(html, `id="tfPanelAudit"`) {
		t.Error("tfGateExtras must live inside #tfPanelGate")
	}
	css, err := os.ReadFile("static/app/terraform/10-drawer.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`.tf-drawer-tabs { position: sticky; top: 0; z-index: 1; display: flex;`, `overflow-x: auto`, `@media (max-width: 900px)`} {
		if !strings.Contains(string(css), s) {
			t.Errorf("10-drawer.css missing %q", s)
		}
	}
}

// Registry tab hooks in index.html plus the ids/hooks the bundled registry modules must expose.
func TestTerraformRegistryUIHooks(t *testing.T) {
	html := string(embeddedHTML)
	for _, s := range []string{
		`id="tabTfRegistry"`, `data-tab="registry"`, `aria-controls="tfPanelRegistry"`, `id="tfPanelRegistry"`, `id="tfRegistryRoot"`,
		`'policies', 'modules', 'registry', 'audit', 'gate'`, `tfRegistryRefresh`,
		`"tf_tab_registry": "Registry"`,
	} {
		if !strings.Contains(html, s) {
			t.Errorf("index.html missing %s", s)
		}
	}
	if n := strings.Count(html, `"tf_tab_registry":`); n != 2 {
		t.Errorf("tf_tab_registry must exist in EN and ID dictionaries, found %d", n)
	}
	if strings.Index(html, `id="tabTfRegistry"`) < strings.Index(html, `id="tabTfModules"`) || strings.Index(html, `id="tabTfRegistry"`) > strings.Index(html, `id="tabTfAudit"`) {
		t.Error("Registry tab must sit between Module Catalog and Audit")
	}
	for file, ids := range map[string][]string{
		"60-registry.js":          {`data-reg-kind`, `id="tfRegQuery"`, `id="tfRegProvider"`, `id="tfRegSearch"`, `id="tfRegResults"`, `data-reg-item`, `id="tfRegMore"`, `id="tfRegDetail"`},
		"61-registry-detail.js":   {`id="tfRegVersion"`, `id="tfRegModName"`, `id="tfRegDeclareVars"`, `'tfRegInputs'`, `'tfRegOutputs'`, `id="tfRegReadme"`, `id="tfRegInsert"`, `id="tfRegAddFile"`, `id="tfRegCopy"`, `tfE(it.description`},
		"62-registry-check.js":    {`data-regc="`, `data-regc-apply`, `regInit`, `registry/check`},
		"63-registry-settings.js": {`id="tfRegSettingsCard"`, `id="tfRegUrl"`, `id="tfRegUrlSave"`, `id="tfRegUrlTest"`, `id="tfRegUrlStatus"`, `tfGateExtrasBase`, `function tfRegistryRefresh`},
		"22-config-view.js":       {`id="tfCfgRegistry"`, `id="tfCfgRegCheck"`, `id="tfCfgRegStatus"`},
		"25-config-tools.js":      {`C.regInit`},
	} {
		b, err := os.ReadFile("static/app/terraform/" + file)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range ids {
			if !strings.Contains(string(b), s) {
				t.Errorf("%s missing %s", file, s)
			}
		}
	}
}
