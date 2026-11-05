package web

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestAnsibleSectionTabs(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for Ansible tab behavior checks")
	}
	html := string(embeddedHTML)
	source := func(start, end string) string {
		t.Helper()
		_, rest, ok := strings.Cut(html, start)
		if !ok {
			t.Fatalf("missing JavaScript marker %q", start)
		}
		body, _, ok := strings.Cut(rest, end)
		if !ok {
			t.Fatalf("missing JavaScript end marker %q", end)
		}
		return start + body
	}
	js := source("let currentAnsibleTab =", "async function loadAnsibleStatus()") +
		source("function renderAnsibleGateBanner()", "async function toggleAnsibleGate(") +
		source("async function loadAnsibleSchedules()", "function ansibleSchedulePayload(")
	script := `
const assert = require('node:assert/strict');
const elements = {};
const names = ['playbooks', 'schedules', 'settings'];
const tabs = names.map(name => {
  const panel = 'ansiblePanel' + name[0].toUpperCase() + name.slice(1);
  elements[panel] = {style: {display: name === 'playbooks' ? 'flex' : 'none'}};
  return {
    dataset: {tab: name}, attrs: {'aria-controls': panel}, tabIndex: name === 'playbooks' ? 0 : -1,
    classList: {toggle(key, value) {this[key] = value;}},
    setAttribute(key, value) {this.attrs[key] = value;},
    getAttribute(key) {return this.attrs[key];},
    focus() {focused = name;},
  };
});
for (const id of ['ansibleGateBanner', 'ansibleGateTitle', 'ansibleGateDesc', 'ansibleGateAction',
  'ansibleGateStatus', 'ansibleSchedulesContainer', 'ansibleSchedulesUnavailable',
  'ansibleSchedulesDesc', 'ansibleSchedulesBody', 'ansibleSchedulesEmpty']) {
  elements[id] = {style: {}, innerHTML: ''};
}
const document = {
  querySelectorAll(selector) {assert.equal(selector, '#ansibleTabs .tab'); return tabs;},
  getElementById(id) {assert.ok(elements[id], id); return elements[id];},
};
const t = (key, fallback) => fallback;
const svgIcon = () => '';
const escapeHtml = value => String(value);
let focused;
let fetchCount = 0;
const fetch = async () => {fetchCount++; return {ok: true, json: async () => ({schedules: []})};};
let ansibleSchedulesCache = [];
eval(` + strconv.Quote(js+`
(async () => {
  function checkTab(name) {
    assert.equal(currentAnsibleTab, name);
    tabs.forEach(tab => {
      const active = tab.dataset.tab === name;
      assert.equal(tab.attrs['aria-selected'], String(active));
      assert.equal(tab.classList.active, active);
      assert.equal(tab.tabIndex, active ? 0 : -1);
      assert.equal(elements[tab.attrs['aria-controls']].style.display, active ? 'flex' : 'none');
    });
  }
  for (const name of names) {switchAnsibleTab(name); checkTab(name);}
  switchAnsibleTab('unknown'); checkTab('settings');
  for (const [index, key, expected] of [[2, 'ArrowRight', 'playbooks'], [0, 'ArrowLeft', 'settings'],
    [1, 'Home', 'playbooks'], [1, 'End', 'settings']]) {
    let prevented = false;
    onAnsibleTabKeydown({target: tabs[index], key, preventDefault() {prevented = true;}});
    assert.ok(prevented); assert.equal(focused, expected); checkTab(expected);
  }
  onAnsibleTabKeydown({target: tabs[2], key: 'Tab', preventDefault() {assert.fail('Tab must retain native behavior');}});
  checkTab('settings');
  ansibleStatusCache = {available: true, enabled: true};
  await loadAnsibleSchedules();
  checkTab('settings');
  assert.equal(elements.ansibleSchedulesContainer.style.display, 'flex');
  assert.equal(elements.ansibleSchedulesUnavailable.style.display, 'none');
  assert.equal(elements.ansibleSchedulesEmpty.style.display, 'block');
  switchAnsibleTab('playbooks');
  await loadAnsibleSchedules(); checkTab('playbooks');
  switchAnsibleTab('schedules');
  for (const state of [{available: true, enabled: false}, {available: false, enabled: true}]) {
    ansibleStatusCache = state;
    const before = fetchCount;
    await loadAnsibleSchedules(); checkTab('schedules');
    assert.equal(fetchCount, before, 'disabled schedules must not fetch');
    assert.equal(elements.ansibleSchedulesContainer.style.display, 'none');
    assert.equal(elements.ansibleSchedulesUnavailable.style.display, 'flex');
  }
  for (const [state, label, badge] of [
    [{available: true, enabled: true}, 'Ansible Gate is Active', 'badge-healthy'],
    [{available: true, enabled: false}, 'Ansible Gate is Disabled', 'badge-warning'],
    [{available: false, enabled: false}, 'Ansible unavailable', 'badge-danger']]) {
    ansibleStatusCache = state; renderAnsibleGateBanner(); checkTab('schedules');
    assert.equal(elements.ansibleGateStatus.textContent, label);
    assert.ok(elements.ansibleGateStatus.className.includes(badge));
    assert.ok(elements.ansibleGateDesc.innerText.length > 0, 'gate warning must remain available');
    if (state.available) assert.ok(elements.ansibleGateAction.innerHTML.includes('toggleAnsibleGate(' + !state.enabled + ')'));
  }
})().catch(error => {console.error(error); process.exitCode = 1;});
`) + `);
`
	cmd := exec.Command(node, "-e", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Ansible tab behavior failed: %v\n%s", err, out)
	}
}

func TestAnsibleUIInIndexHTML(t *testing.T) {
	html := string(embeddedHTML)

	// 1. Verify navigation entry in side panel
	navHooks := []string{
		`id="navItemAnsible"`,
		`onclick="switchSideNav('ansible')"`,
		`data-i18n="nav_ansible"`,
		`data-i18n-title="nav_ansible"`,
	}
	for _, hook := range navHooks {
		if !strings.Contains(html, hook) {
			t.Errorf("expected side panel nav hook %q in index.html", hook)
		}
	}

	// 2. Verify main Ansible section & key UI elements
	sectionHooks := []string{
		`id="sectionAnsible"`,
		`id="ansibleTabs"`,
		`id="ansibleGateStatus"`,
		`id="ansibleSchedulesUnavailable"`,
		`id="btnAnsibleNewPlaybook"`,
		`id="btnAnsibleRefresh"`,
		`id="ansibleGateBanner"`,
		`id="ansibleGateTitle"`,
		`id="ansibleGateDesc"`,
		`id="ansibleGateAction"`,
		`id="ansiblePlaybooksContainer"`,
		`id="ansiblePlaybookSearchInput"`,
		`id="ansiblePlaybookCountBadge"`,
		`id="ansiblePlaybooksGrid"`,
		`id="ansiblePlaybooksEmpty"`,
	}
	for _, hook := range sectionHooks {
		if !strings.Contains(html, hook) {
			t.Errorf("expected section element %q in index.html", hook)
		}
	}

	// 3. Verify modals: Playbook Editor Modal, Run Launch Modal, Run Viewer Workspace Modal
	modalHooks := []string{
		`id="ansiblePlaybookModal"`,
		`id="ansiblePlaybookName"`,
		`id="ansiblePlaybookDesc"`,
		`id="ansiblePlaybookContent"`,
		`id="ansiblePlaybookExtraVars"`,
		`id="btnAnsiblePlaybookSubmit"`,
		`id="ansibleRunLaunchModal"`,
		`id="ansibleHostSearch"`,
		`id="ansibleGroupFilter"`,
		`id="ansibleHostPicker"`,
		`id="ansibleRunMode"`,
		`id="ansibleRunVerbosity"`,
		`id="ansibleRunTags"`,
		`id="ansibleRunSkipTags"`,
		`id="ansibleRunExtraVars"`,
		`id="ansibleRunBecome"`,
		`id="btnAnsibleRunLaunchSubmit"`,
		`id="ansibleRunModal"`,
		`class="modal-content modal-workspace"`,
		`id="ansibleRunTabs"`,
		`id="tabAnsibleRunLog"`,
		`id="tabAnsibleRunHistory"`,
		`id="btnAnsibleCancelRun"`,
		`id="ansibleLogOutput"`,
		`id="ansibleHistoryTableBody"`,
		`id="ansibleScheduleModal"`,
		`id="ansibleGitUrl"`,
		`id="ansibleSchedCron"`,
	}
	for _, hook := range modalHooks {
		if !strings.Contains(html, hook) {
			t.Errorf("expected modal hook %q in index.html", hook)
		}
	}

	// 4. Verify API paths
	apiPaths := []string{
		"/api/ansible/status",
		"/api/ansible/playbooks",
		"/run",
		"/log?offset=",
		"/cancel",
		"/api/ansible/schedules",
	}
	for _, p := range apiPaths {
		if !strings.Contains(html, p) {
			t.Errorf("expected API path %q in index.html", p)
		}
	}

	// 5. Verify i18n keys present in BOTH English and Indonesian dictionaries
	requiredI18nKeys := []string{
		"nav_ansible",
		"ansible_section_title",
		"ansible_section_desc",
		"ansible_tab_playbooks",
		"ansible_tab_settings",
		"ansible_sched_unavailable",
		"ansible_gate_unavailable",
		"ansible_btn_new_playbook",
		"ansible_gate_title",
		"ansible_gate_disabled_title",
		"ansible_gate_disabled_warning",
		"ansible_gate_btn_enable",
		"ansible_gate_btn_disable",
		"ansible_gate_active",
		"ansible_gate_binary_missing",
		"ansible_playbook_modal_add",
		"ansible_playbook_modal_edit",
		"ansible_playbook_name_label",
		"ansible_playbook_content_label",
		"ansible_run_launch_title",
		"ansible_target_hosts_label",
		"ansible_run_mode_label",
		"ansible_mode_run",
		"ansible_mode_check",
		"ansible_mode_diff",
		"ansible_mode_syntax",
		"ansible_become_label",
		"ansible_btn_launch",
		"ansible_tab_log",
		"ansible_tab_history",
		"ansible_btn_cancel_run",
		"ansible_confirm_delete_pb_title",
		"ansible_confirm_cancel_run_title",
		"ansible_playbook_saved",
		"ansible_playbook_deleted",
		"ansible_run_started",
		"ansible_pb_source_git",
		"ansible_git_url_label",
		"ansible_git_missing",
		"ansible_sched_title",
		"ansible_sched_cron_hint",
		"ansible_sched_err_targets",
		"ansible_trigger_schedule",
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
		"function switchAnsibleTab",
		"function onAnsibleTabKeydown",
		"async function loadAnsibleStatus",
		"function renderAnsibleGateBanner",
		"async function toggleAnsibleGate",
		"async function loadAnsiblePlaybooks",
		"function renderAnsiblePlaybooks",
		"async function refreshAnsibleSection",
		"function initAnsibleYamlEditor",
		"function destroyAnsibleYamlEditor",
		"async function openAnsiblePlaybookModal",
		"async function submitAnsiblePlaybookModal",
		"async function deleteAnsiblePlaybook",
		"function openAnsibleRunLaunchModal",
		"async function submitAnsibleRunLaunch",
		"async function openAnsibleRunViewer",
		"async function openAnsiblePlaybookHistory",
		"function switchAnsibleRunTab",
		"async function pollAnsibleLog",
		"function startAnsibleLogPolling",
		"function stopAnsibleLogPolling",
		"async function cancelCurrentAnsibleRun",
		"async function loadAnsibleRunHistory",
	}
	for _, fn := range jsFunctions {
		if !strings.Contains(html, fn) {
			t.Errorf("expected JS function %q in index.html", fn)
		}
	}

	panels := []struct{ name, content string }{
		{"Playbooks", `id="ansiblePlaybooksContainer"`},
		{"Schedules", `id="ansibleSchedulesContainer"`},
		{"Settings", `id="ansibleGateBanner"`},
	}
	for i, panel := range panels {
		start := strings.Index(html, `id="ansiblePanel`+panel.name+`"`)
		if start < 0 {
			t.Fatalf("missing Ansible panel %s", panel.name)
		}
		end := len(html)
		if i+1 < len(panels) {
			end = strings.Index(html, `id="ansiblePanel`+panels[i+1].name+`"`)
		}
		if end <= start || !strings.Contains(html[start:end], panel.content) {
			t.Errorf("expected %s inside Ansible %s panel", panel.content, panel.name)
		}
		for _, hook := range []string{`role="tabpanel"`, `aria-labelledby="tabAnsible` + panel.name + `"`} {
			if !strings.Contains(html[start:start+250], hook) {
				t.Errorf("missing %s on Ansible %s panel", hook, panel.name)
			}
		}
	}

	// 7. Extract inline JS and run node --check syntax verification if node is available
	nodePath, err := exec.LookPath("node")
	if err == nil && nodePath != "" {
		re := regexp.MustCompile(`(?s)<script>(.*?)</script>\s*</body>`)
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
