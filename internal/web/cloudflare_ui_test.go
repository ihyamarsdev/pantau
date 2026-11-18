package web

import (
	"io/fs"
	"os/exec"
	"strings"
	"testing"
)

func TestCloudflareUIInIndexHTML(t *testing.T) {
	ui := uiSourceForTest(t)

	// 1. Verify navigation entry in side panel
	navHooks := []string{
		`id="navItemCloudflare"`,
		`onclick="switchSideNav('cloudflare')"`,
		`data-i18n="nav_cloudflare"`,
		`data-i18n-title="nav_cloudflare"`,
	}
	for _, hook := range navHooks {
		if !strings.Contains(ui, hook) {
			t.Errorf("expected side panel nav hook %q in index.html", hook)
		}
	}

	// 2. Verify main Cloudflare section & key UI elements
	sectionHooks := []string{
		`id="sectionCloudflare"`,
		`id="cfAccountSelect"`,
		`id="btnCfAddAccount"`,
		`id="btnCfEditAccount"`,
		`id="btnCfDeleteAccount"`,
		`id="btnCfRefresh"`,
		`id="cfZonesContainer"`,
		`id="cfZoneSearchInput"`,
		`id="cfZonesGrid"`,
		`id="cfZonesEmpty"`,
	}
	for _, hook := range sectionHooks {
		if !strings.Contains(ui, hook) {
			t.Errorf("expected section element %q in index.html", hook)
		}
	}

	// 3. Verify modals: Account Action Dialog, Zone Workspace Modal, Record Dialog
	modalHooks := []string{
		`id="cfAccountModal"`,
		`id="cfAccountName"`,
		`id="cfAccountCFID"`,
		`id="cfAccountToken"`,
		`type="password" id="cfAccountToken"`,
		`id="btnCfAccountSubmit"`,
		`id="cfZoneModal"`,
		`class="modal-content modal-workspace"`,
		`id="cfZoneTabs"`,
		`id="tabCfZoneDns"`,
		`id="tabCfZoneCache"`,
		`id="tabCfZoneSettings"`,
		`id="cfDnsTableBody"`,
		`id="cfPurgeUrlsTextarea"`,
		`id="btnCfPurgeEverything"`,
		`id="btnCfPurgeUrls"`,
		`id="cfSettingsContainer"`,
		`id="cfRecordModal"`,
		`id="cfRecordType"`,
		`id="cfRecordName"`,
		`id="cfRecordContent"`,
		`id="cfRecordTTL"`,
		`id="cfRecordProxied"`,
		`id="btnCfRecordSubmit"`,
		`id="tabCfZoneAccess"`,
		`id="cfZonePanelAccess"`,
		`id="cfAccessTableBody"`,
		`id="btnCfAccessAdd"`,
		`id="btnCfDnsExport"`,
		`id="cfZonePerm"`,
		`id="cfTunnelsContainer"`,
		`id="cfTunnelsTableBody"`,
		`id="cfAccountPermsGuide"`,
	}
	for _, hook := range modalHooks {
		if !strings.Contains(ui, hook) {
			t.Errorf("expected modal hook %q in index.html", hook)
		}
	}

	// 4. Verify API paths
	apiPaths := []string{
		"/api/cloudflare/accounts",
		"/zones",
		"/dns",
		"/purge",
		"/settings",
	}
	for _, p := range apiPaths {
		if !strings.Contains(ui, p) {
			t.Errorf("expected API path %q in index.html", p)
		}
	}

	// 5. Verify i18n keys present in BOTH English and Indonesian dictionaries
	requiredI18nKeys := []string{
		"nav_cloudflare",
		"cf_section_title",
		"cf_account_label",
		"cf_select_account",
		"cf_add_account",
		"cf_edit_account",
		"cf_delete_account",
		"cf_account_modal_title_add",
		"cf_account_modal_title_edit",
		"cf_account_name_label",
		"cf_account_token_label",
		"cf_tab_dns",
		"cf_tab_cache",
		"cf_tab_settings",
		"cf_add_dns_record",
		"cf_purge_everything_title",
		"cf_btn_purge_everything",
		"cf_purge_urls_title",
		"cf_btn_purge_urls",
		"cf_settings_intro",
		"cf_confirm_delete_account_title",
		"cf_confirm_delete_record_title",
		"cf_confirm_purge_everything_title",
		"cf_tab_access",
		"cf_access_intro",
		"cf_access_mode_block",
		"cf_access_target_ip_range",
		"cf_perm_guide_title",
		"cf_perm_missing",
		"cf_perm_row_tunnels",
		"cf_btn_export",
		"cf_tunnels_title",
		"cf_tunnel_status_healthy",
		"cf_set_group_performance",
		"cf_set_tls_1_3_title",
		"cf_set_http3_desc",
		"cf_set_brotli_title",
		"cf_set_always_online_desc",
		"cf_account_saved",
		"cf_record_saved",
		"cf_cache_purged",
	}

	// Locate I18N dictionary sections
	enIdx := strings.Index(ui, "en: {")
	idIdx := strings.Index(ui, "id: {")
	if enIdx == -1 || idIdx == -1 || idIdx <= enIdx {
		t.Fatalf("could not locate both 'en: {' and 'id: {' in index.html")
	}
	enSection := ui[enIdx:idIdx]
	idSection := ui[idIdx:]

	for _, k := range requiredI18nKeys {
		needle := `"` + k + `":`
		if !strings.Contains(enSection, needle) {
			t.Errorf("expected EN translation for key %q", k)
		}
		if !strings.Contains(idSection, needle) {
			t.Errorf("expected ID translation for key %q", k)
		}
	}

	// 6. Verify JS functions exist
	jsFunctions := []string{
		"async function loadCfAccounts",
		"function populateCfAccountSelect",
		"async function onSelectCfAccount",
		"async function loadCfZones",
		"function renderCfZones",
		"function openCfAccountModal",
		"async function submitCfAccountModal",
		"async function deleteCfAccount",
		"async function openCfZoneModal",
		"async function loadCfAccessRules",
		"async function submitCfAccessRule",
		"async function deleteCfAccessRule",
		"async function exportCfDns",
		"async function loadCfTunnels",
		"function setCfPerm",
		"function switchCfZoneTab",
		"async function loadCfDnsRecords",
		"function renderCfDnsTable",
		"function openCfRecordModal",
		"async function submitCfRecordModal",
		"async function deleteCfRecord",
		"async function submitCfPurgeEverything",
		"async function submitCfPurgeUrls",
		"async function loadCfZoneSettings",
		"function renderCfZoneSettings",
		"async function updateCfZoneSetting",
	}
	for _, fn := range jsFunctions {
		if !strings.Contains(ui, fn) && !strings.Contains(coreJSSource(t), fn) {
			t.Errorf("expected JS function %q in index.html or core modules", fn)
		}
	}

	// 7. Syntax verification of every native ES module if node is available.
	nodePath, err := exec.LookPath("node")
	if err == nil && nodePath != "" {
		entries, rerr := fs.ReadDir(embeddedFS, "static/app/core")
		if rerr != nil {
			t.Fatalf("read core modules: %v", rerr)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".js") {
				continue
			}
			raw, rerr := fs.ReadFile(embeddedFS, "static/app/core/"+e.Name())
			if rerr != nil {
				t.Fatalf("read core module %s: %v", e.Name(), rerr)
			}
			cmd := exec.Command(nodePath, "--input-type=module", "--check")
			cmd.Stdin = strings.NewReader(string(raw))
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("module %s syntax check failed (node --check): %v\nOutput:\n%s", e.Name(), err, string(out))
			}
		}
	}
}
