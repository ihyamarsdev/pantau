package web

import (
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

func TestCloudflareUIInIndexHTML(t *testing.T) {
	html := string(embeddedHTML)

	// 1. Verify navigation entry in side panel
	navHooks := []string{
		`id="navItemCloudflare"`,
		`onclick="switchSideNav('cloudflare')"`,
		`data-i18n="nav_cloudflare"`,
		`data-i18n-title="nav_cloudflare"`,
	}
	for _, hook := range navHooks {
		if !strings.Contains(html, hook) {
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
		if !strings.Contains(html, hook) {
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
	}
	for _, hook := range modalHooks {
		if !strings.Contains(html, hook) {
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
		if !strings.Contains(html, p) {
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
		"cf_account_saved",
		"cf_record_saved",
		"cf_cache_purged",
	}

	// Locate I18N dictionary sections
	enIdx := strings.Index(html, "en: {")
	idIdx := strings.Index(html, "id: {")
	if enIdx == -1 || idIdx == -1 || idIdx <= enIdx {
		t.Fatalf("could not locate both 'en: {' and 'id: {' in index.html")
	}
	enSection := html[enIdx:idIdx]
	idSection := html[idIdx:]

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
		if !strings.Contains(html, fn) {
			t.Errorf("expected JS function %q in index.html", fn)
		}
	}

	// 7. Extract inline JS and run node --check syntax verification if node is available
	nodePath, err := exec.LookPath("node")
	if err == nil && nodePath != "" {
		re := regexp.MustCompile(`(?s)<script>(.*?)</script>`)
		match := re.FindStringSubmatch(html)
		if len(match) < 2 {
			t.Fatalf("could not extract inline <script> from index.html")
		}
		cmd := exec.Command(nodePath, "--check")
		cmd.Stdin = strings.NewReader(match[1])
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("inline script syntax check failed (node --check): %v\nOutput:\n%s", err, string(out))
		}
	}
}
