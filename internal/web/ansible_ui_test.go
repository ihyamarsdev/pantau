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
		source("function renderAnsiblePlaybooks()", "async function refreshAnsibleSection()") +
		source("async function loadAnsibleSchedules()", "function ansibleSchedulePayload(")
	script := `
const assert = require('node:assert/strict');
const elements = {};
const names = ['playbooks', 'schedules', 'templates', 'files', 'library', 'settings'];
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
  'ansibleSchedulesDesc', 'ansibleSchedulesBody', 'ansibleSchedulesEmpty',
  'ansiblePlaybooksGrid', 'ansiblePlaybooksEmpty', 'ansiblePlaybookSearchInput',
  'ansibleLibUnavailableBanner', 'ansibleLibCollectionsList', 'ansibleLibCollectionsEmpty',
  'ansibleLibRolesList', 'ansibleLibRolesEmpty', 'ansibleLibGalaxyFields', 'ansibleLibReqsFields',
  'ansibleConfigCard', 'ansibleConfigContent', 'ansibleConfigAllowedSections',
  'ansibleConfigError', 'ansibleConfigSuccess', 'btnAnsibleConfigSave']) {
  elements[id] = {style: {}, innerHTML: '', textContent: '', querySelectorAll: () => []};
}
function loadAnsibleLibrary() {}
function loadAnsibleAssets() {}
function loadAnsibleConfig() {}
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
  for (const [index, key, expected] of [[5, 'ArrowRight', 'playbooks'], [0, 'ArrowLeft', 'settings'],
    [1, 'Home', 'playbooks'], [1, 'End', 'settings']]) {
    let prevented = false;
    onAnsibleTabKeydown({target: tabs[index], key, preventDefault() {prevented = true;}});
    assert.ok(prevented); assert.equal(focused, expected); checkTab(expected);
  }
  onAnsibleTabKeydown({target: tabs[5], key: 'Tab', preventDefault() {assert.fail('Tab must retain native behavior');}});
  checkTab('settings');
  ansibleStatusCache = {available: true, enabled: true};
  await loadAnsibleSchedules();
  checkTab('settings');
  assert.equal(elements.ansibleSchedulesContainer.style.display, 'flex');
  assert.equal(elements.ansibleSchedulesUnavailable.style.display, 'none');
  assert.equal(elements.ansibleSchedulesEmpty.style.display, 'flex');
  renderAnsiblePlaybooks();
  assert.equal(elements.ansiblePlaybooksGrid.style.display, 'none');
  assert.equal(elements.ansiblePlaybooksEmpty.style.display, 'flex');
  ansiblePlaybooksCache = [{id: 1, name: 'Deploy'}];
  elements.ansiblePlaybookSearchInput.value = 'no match';
  renderAnsiblePlaybooks();
  assert.equal(elements.ansiblePlaybooksEmpty.style.display, 'flex');
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

func TestAnsibleFolderImport(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for Ansible folder import checks")
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
	js := source("let ansibleImportFiles =", "function initAnsibleYamlEditor(") +
		source("function initAnsibleYamlEditor(", "async function deleteAnsiblePlaybook(") +
		source("function renderAnsiblePlaybooks()", "async function refreshAnsibleSection()") +
		"function escapeHtml(s) { if (!s) return ''; return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/\"/g, '&quot;'); }\n"
	script := `
const assert = require('node:assert/strict');
const elements = new Proxy({}, {get(target, id) {
  return target[id] ||= {value: '', style: {}, innerHTML: '', textContent: '', focus() {}, classList: {toggle() {}}, querySelectorAll: () => []};
}});
const domLists = {};
const document = {getElementById: id => elements[id], querySelectorAll: sel => domLists[sel] || []};
const t = (key, fallback) => fallback;
const formatBytes = n => n + ' B';
const setTimeout = () => {};
const svgIcon = () => '';
const ansibleStatusBadgeClass = () => '';
let ansibleStatusCache = {}, ansibleYamlEditor = null, ansiblePlaybookModalMode, ansiblePlaybookEditId;
let ansiblePlaybooksCache = [], editorInitializations = 0, refreshes = 0, requests = [], gets = [], responseStatus = 201, streamChunks = null;
let savedPlaybook = {id: 7, name: 'Deploy', source: 'inline', content: '- hosts: all', extra_vars: '{"app_port":8080}'};
const CodeMirror = {fromTextArea() {
  editorInitializations++;
  return {value: '', setValue(v) {this.value = v;}, getValue() {return this.value;}, refresh() {}, toTextArea() {}, on() {}};
}};
function openModal() {}
function closeModal(id) {if (id === 'ansibleImportModal') resetAnsibleImportModal();}
const toasts = [];
function showToast(message, type) {toasts.push([message, type]);}
async function showConfirm() {return true;}
async function loadAnsiblePlaybooks() {refreshes++;}
async function loadAnsibleSchedules() {}
class FormData {entries = []; append(...args) {this.entries.push(args);}}
class Option {constructor(text, value) {this.text = text; this.value = value;}}
const fetch = async (url, init) => {
  if (url === '/api/ssh-keys') return {ok: true, json: async () => []};
  if (url === '/api/ansible/config' && !init) return {ok: true, json: async () => ({content: '[defaults]\nforks = 5\n', allowed_sections: ['defaults', 'ssh_connection'], managed_keys: []})};
  if (!init) {
    gets.push(url);
    if (url.endsWith('/contents')) return {ok: true, json: async () => ({project: {entry: 'site.yml', commit: 'abcdef0123456789', truncated: false, files: [{path: 'site.yml', content: '- hosts: all'}]}, vars: []})};
    return {ok: true, json: async () => ({playbook: savedPlaybook})};
  }
  requests.push({url, ...init});
  if (streamChunks && responseStatus < 400 && url.startsWith('/api/ansible/library/') && init.method === 'POST') {
    return {ok: true, status: 200, headers: {get: () => 'application/x-ndjson'}, body: {getReader() {
      let i = 0;
      return {read: async () => i < streamChunks.length ? {value: new TextEncoder().encode(streamChunks[i++]), done: false} : {done: true}};
    }}};
  }
  return {ok: responseStatus < 400, status: responseStatus, headers: {get: () => 'application/json'}, json: async () => ({playbook: savedPlaybook, error: 'Gate disabled'})};
};
const file = (path, size = 1) => ({name: path.split('/').pop(), webkitRelativePath: 'project/' + path, size});
eval(` + strconv.Quote(js+`
(async () => {
  const main = 'playbook-production/platform-setup/deploy.yml';
  const paths = [main, 'site.yaml', 'roles/app/tasks/main.yml', 'group_vars/all.yml', 'host_vars/host.yml',
    'files/example.yml', 'templates/example.yaml', '.ansible/collections/vendor/galaxy.yml',
    'inventories/prod/hosts.yml', 'inventory/hosts.yaml', 'docs/example.yml', '.github/workflows/build.yml',
    '.env', 'ansible.cfg', 'files/key.pem', '.git/objects/object', 'nested/__pycache__/a.pyc', '.DS_Store'];
  const prepared = prepareAnsibleImportFiles(paths.map(path => file(path, 2)));
  assert.equal(prepared.files.length, paths.length - 4); // excludes .git, __pycache__, .DS_Store, and .ansible/collections
  assert.equal(prepared.size, prepared.files.length * 2);
  assert.deepEqual(prepared.candidates, [main, 'site.yaml']);
  assert.ok(!prepared.files.some(item => item.path.startsWith('.ansible/')));
  const preparedWithVendored = prepareAnsibleImportFiles(paths.map(path => file(path, 2)), {includeVendored: true});
  assert.equal(preparedWithVendored.files.length, paths.length - 3);
  assert.ok(preparedWithVendored.files.some(item => item.path.startsWith('.ansible/')));
  assert.ok(prepared.files.some(item => item.path === '.env'));
  assert.equal(prepareAnsibleImportFiles([file('nested/project/site.yml')]).files[0].path, 'nested/project/site.yml');
  assert.equal(prepareAnsibleImportFiles([{name: 'site.yml', size: 1}]).files[0].path, 'site.yml');
  for (const path of ['/site.yml', '../site.yml', 'a/../site.yml', 'a//site.yml', './site.yml', 'a\\site.yml', 'a\0.yml', 'a"b.yml', 'C:site.yml', 'a\u0085.yml']) {
    assert.equal(isAnsibleFolderPath(path), false, path);
    assert.throws(() => prepareAnsibleImportFiles([file(path)]), /relative path/);
  }
  assert.throws(() => prepareAnsibleImportFiles([file('site.yml'), file('site.yml')]), /duplicate/);
  assert.equal(prepareAnsibleImportFiles([file('site.yml', 64 * 1024 * 1024)]).size, 64 * 1024 * 1024);
  assert.throws(() => prepareAnsibleImportFiles([file('site.yml', 64 * 1024 * 1024), file('key', 1)]), /64 MiB/);
  const many = Array.from({length: 10000}, (_, i) => file(i + '.yml', 0));
  assert.equal(prepareAnsibleImportFiles(many).files.length, 10000);
  assert.throws(() => prepareAnsibleImportFiles([...many, file('extra.yml', 0)]), /10,000/);

  openAnsibleImportModal();
  await submitAnsibleImportModal();
  assert.match(elements.ansibleImportError.textContent, /Select a folder/);
  elements.ansibleImportFiles.files = [file('roles/app/tasks/main.yml')];
  await onAnsibleImportFilesChange();
  assert.match(elements.ansibleImportError.textContent, /No main playbook YAML/);
  assert.equal(ansibleImportFiles.length, 0);

  // Detection: only YAML with hosts:/import_playbook: is preselected; vendored collections stay out by default.
  const unsafe = 'playbooks/a<img src=x>.yml';
  const textFile = (path, text) => ({...file(path), text: async () => text});
  elements.ansibleImportFiles.files = [textFile(unsafe, '- hosts: all\n'), textFile('site.yaml', '- import_playbook: a.yml\n'), textFile('notes.yml', 'just: data\n'), file('vars/main.yml'), file('collections/x/galaxy.yml')];
  await onAnsibleImportFilesChange();
  assert.ok(elements.ansibleImportPreview.textContent.includes('4 files'));
  const tableHtml = elements.ansibleImportTableBody.innerHTML;
  assert.ok(!tableHtml.includes('<img') && tableHtml.includes('&lt;img'), 'paths are escaped');
  assert.equal((tableHtml.match(/data-idx="\d+" checked/g) || []).length, 2, 'two detected playbooks are preselected');
  assert.equal(ansibleImportCandidates.map(c => c.path).join('|'), 'notes.yml|' + unsafe + '|site.yaml|vars/main.yml');

  // The table's checkboxes and name inputs drive the upload; paths come from the detected candidates.
  const select = (checked, names) => {
    domLists['.ansible-import-item-chk'] = checked.map(c => ({checked: c}));
    domLists['.ansible-import-item-name'] = names.map(value => ({value}));
  };
  select([false, false, false, false], ['notes', 'a', 'site', 'vars']);
  await submitAnsibleImportModal();
  assert.match(elements.ansibleImportError.textContent, /at least one/);
  select([false, true, true, false], ['notes', ' ', 'site', 'vars']);
  await submitAnsibleImportModal();
  assert.match(elements.ansibleImportError.textContent, /cannot be empty/);
  select([false, true, true, false], ['notes', 'Same', 'same', 'vars']);
  await submitAnsibleImportModal();
  assert.match(elements.ansibleImportError.textContent, /Duplicate/);
  ansiblePlaybooksCache = [{id: 1, name: 'TAKEN'}];
  select([false, true, true, false], ['notes', 'taken', 'site', 'vars']);
  await submitAnsibleImportModal();
  assert.match(elements.ansibleImportError.textContent, /already exists/);
  ansiblePlaybooksCache = [];
  assert.equal(requests.length, 0, 'validation must not upload');
  const siteFile = ansibleImportFiles.find(f => f.path === 'site.yaml').file;
  siteFile.size = 300 * 1024;
  select([false, true, true, false], ['notes', 'A deploy', 'Site', 'vars']);
  await submitAnsibleImportModal();
  assert.match(elements.ansibleImportError.textContent, /exceeds 256 KiB/);
  assert.equal(requests.length, 0, 'an oversized playbook must not upload');
  siteFile.size = 1;

  select([false, true, true, false], ['notes', 'A deploy', 'Site', 'vars']);
  responseStatus = 409;
  const pending = submitAnsibleImportModal();
  assert.equal(elements.btnAnsibleImportSubmit.disabled, true);
  assert.equal(elements.ansibleImportFields.disabled, true);
  await submitAnsibleImportModal();
  await pending;
  assert.equal(requests.length, 1, 'double submit must not upload twice');
  assert.equal(elements.ansibleImportError.textContent, 'Gate disabled');
  assert.equal(elements.btnAnsibleImportSubmit.disabled, false);
  assert.equal(elements.ansibleImportFields.disabled, false);
  assert.equal(ansibleImportFiles.length, 4, 'failed imports must allow retry');
  responseStatus = 201;
	await submitAnsibleImportModal();
	const request = requests.at(-1);
	assert.equal(request.url, '/api/ansible/playbooks/import');
	assert.equal(request.method, 'POST');
	assert.equal(request.headers['Content-Type'], 'application/json');
	const importPayload = JSON.parse(request.body);
	assert.equal(importPayload.playbooks.length, 2);
	assert.deepEqual(importPayload.playbooks.map(p => ({name: p.name, content: p.content})), [
		{name: 'A deploy', content: '- hosts: all\n'},
		{name: 'Site', content: '- import_playbook: a.yml\n'},
	]);
	assert.equal(ansibleImportFiles.length, 0);
	assert.equal(elements.ansibleImportFiles.value, '');
	assert.equal(elements.ansibleImportTableBody.innerHTML, '');
	assert.equal(refreshes, 1);
  const xss = '<img src=x onerror=alert(1)>';
  renderAnsibleContents({
    project: {entry: 'site.yml', commit: 'abcdef0123456789', truncated: true, files: [{path: 'site.yml', content: '- hosts: all'}, {path: xss + '.yml', content: 'x'}]},
    vars: [{name: 'owner', status: 'required', used_in: ['a.yml']}, {name: 'port', status: 'defined', default: xss, source: 'role web defaults', used_in: ['a', 'b', 'c', 'd']}],
  });
  assert.equal(elements.ansibleContentsMeta.textContent, 'commit abcdef01');
  assert.match(elements.ansibleContentsStatus.textContent, /skipped/);
  assert.equal(elements.ansibleContentsViewer.textContent, '- hosts: all');
  assert.ok(!elements.ansibleContentsFileList.innerHTML.includes('<img') && elements.ansibleContentsFileList.innerHTML.includes('&lt;img'));
  renderAnsiblePbVarsTable();
  assert.ok(!elements.ansiblePbVarsTableBody.innerHTML.includes('<img') && elements.ansiblePbVarsTableBody.innerHTML.includes('+1'));
  showAnsibleContentsFile(1);
  assert.equal(elements.ansibleContentsViewer.textContent, 'x');
  ansibleContentsData = {project: {files: [{path: 'a', content: 'A'}, {path: 'b', content: 'B'}]}};
  showAnsibleContentsFile(1);
  assert.equal(elements.ansibleContentsViewer.textContent, 'B');
  renderAnsibleContents({project: {entry: 'x.yml', files: null}, vars: []});
  renderAnsiblePbVarsTable();
  assert.match(elements.ansiblePbVarsTableBody.innerHTML, /No variables detected/);
  switchAnsiblePlaybookTab('vars');
  assert.equal(elements.ansiblePbTabPanelVars.style.display, 'flex');
  assert.equal(elements.ansiblePbTabPanelPlaybook.style.display, 'none');
  // Library: lists are escaped; installs send exactly the contracted request shapes; invalid input never uploads.
  ansibleLibraryCache = {available: true, collections: [{name: 'community.general', version: '9.0.0'}], roles: [{name: '<b>x</b>', version: ''}]};
  renderAnsibleLibrary();
  assert.ok(elements.ansibleLibCollectionsList.innerHTML.includes('community.general') && elements.ansibleLibCollectionsList.innerHTML.includes('9.0.0'));
  assert.ok(!elements.ansibleLibRolesList.innerHTML.includes('<b>') && elements.ansibleLibRolesList.innerHTML.includes('&lt;b&gt;'));
  assert.equal(elements.ansibleLibCollectionsEmpty.style.display, 'none');
  ansibleLibraryCache = {available: false, collections: [], roles: []};
  renderAnsibleLibrary();
  assert.equal(elements.ansibleLibUnavailableBanner.style.display, 'flex');
  assert.equal(elements.ansibleLibGalaxyFields.disabled, true);
  assert.equal(elements.ansibleLibCollectionsEmpty.style.display, 'block');

  responseStatus = 200;
  streamChunks = ['{"log":"ok\\n"}\n{"done":true}\n'];
  const requestsBefore = requests.length;
  elements.ansibleLibGalaxyName.value = ' ';
  await installAnsibleGalaxy();
  assert.equal(requests.length, requestsBefore, 'blank name must not call the API');
  assert.equal(toasts.at(-1)[1], 'error');
  elements.ansibleLibGalaxyKind.value = 'role';
  elements.ansibleLibGalaxyName.value = ' geerlingguy.nginx ';
  elements.ansibleLibGalaxyVersion.value = '3.2.0';
  await installAnsibleGalaxy();
  assert.equal(requests.at(-1).url, '/api/ansible/library/galaxy');
  assert.deepEqual(JSON.parse(requests.at(-1).body), {kind: 'role', name: 'geerlingguy.nginx', version: '3.2.0'});
  assert.equal(elements.ansibleLibGalaxyFields.disabled, false, 'controls are re-enabled after the install');

  // The installer output appears as it streams (chunks may split a line); the final line decides the outcome.
  streamChunks = ['{"log":"Starting\\n"}\n{"lo', 'g":"Downloading\\n"}\n{"done":true}\n'];
  await installAnsibleGalaxy();
  assert.equal(elements.ansibleLibGalaxyOutput.textContent, 'Starting\nDownloading\n');
  assert.equal(elements.ansibleLibGalaxyOutput.style.display, 'block');
  assert.equal(toasts.at(-1)[1], 'success');
  streamChunks = ['{"log":"boom\\n"}\n{"done":true,"error":"ansible-galaxy failed (exit 1)"}\n'];
  await installAnsibleGalaxy();
  assert.match(elements.ansibleLibGalaxyOutput.textContent, /boom/, 'output of a failed install stays visible');
  assert.equal(toasts.at(-1)[1], 'error');
  assert.match(toasts.at(-1)[0], /exit 1/);
  streamChunks = ['{"log":"half\\n"}\n'];
  await installAnsibleGalaxy();
  assert.match(toasts.at(-1)[0], /Connection lost/, 'a stream without a done line is not a success');
  streamChunks = null;
  await installAnsibleGalaxy();
  assert.equal(elements.ansibleLibGalaxyOutput.style.display, 'none', 'a plain JSON error means nothing ran');
  assert.match(toasts.at(-1)[0], /Gate disabled/);
  streamChunks = ['{"log":"ok\\n"}\n{"done":true}\n'];
  elements.ansibleLibReqsContent.value = '  ';
  await installAnsibleRequirements();
  assert.equal(requests.at(-1).url, '/api/ansible/library/galaxy', 'blank requirements must not call the API');
  elements.ansibleLibReqsContent.value = 'collections:\n  - ansible.posix\n';
  await installAnsibleRequirements();
  assert.equal(requests.at(-1).url, '/api/ansible/library/requirements');
  assert.deepEqual(JSON.parse(requests.at(-1).body), {content: 'collections:\n  - ansible.posix'});

  elements.ansibleLibLocalKind.value = 'role';
  elements.ansibleLibLocalSource.value = 'folder';
  elements.ansibleLibLocalRoleName.value = '';
  elements.ansibleLibLocalFolder.files = [file('tasks/main.yml'), file('.git/config')];
  const localBefore = requests.length;
  await installAnsibleLocal();
  assert.match(elements.ansibleLibLocalError.textContent, /Role name/);
  assert.equal(requests.length, localBefore);
  elements.ansibleLibLocalRoleName.value = 'my_role';
  await installAnsibleLocal();
  assert.equal(requests.at(-1).url, '/api/ansible/library/local');
  assert.deepEqual(requests.at(-1).body.entries.map(e => e.slice(0, 2).join('=') + (e[2] ? '@' + e[2] : '')).filter(e => !e.startsWith('files=')), ['kind=role', 'name=my_role']);
  assert.deepEqual(requests.at(-1).body.entries.filter(e => e[0] === 'files').map(e => e[2]), ['tasks/main.yml'], 'root folder stripped, .git excluded');
  elements.ansibleLibLocalKind.value = 'collection';
  elements.ansibleLibLocalSource.value = 'archive';
  elements.ansibleLibLocalArchive.files = [];
  await installAnsibleLocal();
  assert.match(elements.ansibleLibLocalError.textContent, /archive/i);
  const tarball = {name: 'acme-demo-1.0.0.tar.gz', size: 10};
  elements.ansibleLibLocalArchive.files = [tarball];
  await installAnsibleLocal();
  assert.deepEqual(requests.at(-1).body.entries, [['kind', 'collection'], ['archive', tarball]]);
  responseStatus = 400;
  await installAnsibleLocal();
  assert.equal(elements.ansibleLibLocalError.textContent, 'Gate disabled', 'server errors are shown');
  responseStatus = 200;
  await deleteAnsibleLibraryItem('roles', 'geerlingguy.nginx');
  assert.equal(requests.at(-1).url, '/api/ansible/library/roles/geerlingguy.nginx');
  assert.equal(requests.at(-1).method, 'DELETE');

  // Ansible config: loads from and saves to the API; server validation errors stay visible next to the editor.
  ansibleStatusCache = {enabled: true};
  await loadAnsibleConfig();
  assert.equal(elements.ansibleConfigContent.value, '[defaults]\nforks = 5\n');
  assert.match(elements.ansibleConfigAllowedSections.textContent, /defaults, ssh_connection/);
  elements.ansibleConfigContent.value = '[defaults]\nforks = 9\n';
  await submitAnsibleConfig();
  assert.equal(requests.at(-1).url, '/api/ansible/config');
  assert.equal(requests.at(-1).method, 'PUT');
  assert.deepEqual(JSON.parse(requests.at(-1).body), {content: '[defaults]\nforks = 9\n'});
  assert.equal(elements.ansibleConfigError.style.display, 'none');
  responseStatus = 400;
  await submitAnsibleConfig();
  assert.equal(elements.ansibleConfigError.textContent, 'Gate disabled');
  assert.equal(elements.btnAnsibleConfigSave.disabled, false);
  responseStatus = 201;

	// Playbook editor tabs: Playbook, Variables, and Files (lazy loaded for Git)
	await openAnsiblePlaybookModal(7);
	assert.equal(currentAnsiblePbTab, 'playbook');
	assert.equal(elements.tabAnsiblePbFiles.style.display, 'none', 'inline playbook has no Files tab');
	assert.deepEqual(ansiblePbExtraVarsMap, {app_port: 8080});

	// Test value parsing and extra_vars handling
	assert.equal(parseAnsibleVarValue('8080'), 8080);
	assert.equal(parseAnsibleVarValue('"8080"'), '8080');
	assert.equal(parseAnsibleVarValue('true'), true);
	assert.equal(parseAnsibleVarValue('plain string'), 'plain string');
	assert.deepEqual(parseAnsibleVarValue('{"k":"v"}'), {k: 'v'});

	// Variables tab table & Add variable
	switchAnsiblePlaybookTab('vars');
	assert.equal(currentAnsiblePbTab, 'vars');
	elements.ansiblePbNewVarName.value = 'invalid-name!';
	elements.ansiblePbNewVarValue.value = '123';
	addAnsibleExtraVarRow();
	assert.match(elements.ansiblePbNewVarError.textContent, /Variable name must match/);
	elements.ansiblePbNewVarName.value = 'valid_var';
	elements.ansiblePbNewVarValue.value = '"9000"';
	addAnsibleExtraVarRow();
	assert.equal(ansiblePbExtraVarsMap.valid_var, '9000');
	elements.ansiblePbNewVarName.value = 'valid_var';
	addAnsibleExtraVarRow();
	assert.match(elements.ansiblePbNewVarError.textContent, /Duplicate/);

	// Edit input and delete var
	onAnsiblePbVarInputChange({dataset: {varName: 'valid_var'}, value: '9001'});
	assert.equal(ansiblePbExtraVarsMap.valid_var, 9001);
	deleteAnsibleExtraVar('valid_var');
	assert.equal(ansiblePbExtraVarsMap.valid_var, undefined);

	// Variables defined in the YAML are removed from the vars: block; the rest of the play stays intact.
	const yaml = '- hosts: all\n  vars:\n    keep: 1\n    gone:\n      nested: [1, 2]\n      more: x\n    tail: 2\n  tasks:\n    - debug: msg={{ gone }}\n';
	assert.equal(removeAnsibleYamlVar(yaml, 'gone'), '- hosts: all\n  vars:\n    keep: 1\n    tail: 2\n  tasks:\n    - debug: msg={{ gone }}\n');
	assert.equal(removeAnsibleYamlVar('- hosts: all\n  vars:\n    only: 1\n  tasks: []\n', 'only'), '- hosts: all\n  tasks: []\n');
	assert.equal(removeAnsibleYamlVar(yaml, 'missing'), null);
	assert.equal(removeAnsibleYamlVar('- hosts: all\n  tasks:\n    - debug: msg=x\n      vars:\n        a: 1\n', 'hosts'), null, 'only entries inside vars: are touched');
	ansibleYamlEditor.setValue(yaml);
	deleteAnsibleExtraVar('gone');
	assert.ok(!ansibleYamlEditor.getValue().includes('gone:'));
	deleteAnsibleExtraVar('nowhere');
	assert.equal(toasts.at(-1)[1], 'error');
	ansibleYamlEditor.setValue('- hosts: all');

	// Debounced analyze
	ansibleAnalyzeRequestSeq = 0;
	triggerAnsibleAnalyze(false);
	assert.ok(ansibleAnalyzeDebounceTimer !== null);
	await runAnsibleAnalyze();
	assert.equal(requests.at(-1).url, '/api/ansible/playbooks/analyze');

	// Save submission with extra_vars JSON string
	await submitAnsiblePlaybookModal();
	assert.equal(requests.at(-1).url, '/api/ansible/playbooks/7');
	assert.equal(requests.at(-1).method, 'PUT');
	assert.deepEqual(JSON.parse(requests.at(-1).body), {name: 'Deploy', description: '', extra_vars: '{"app_port":8080}', source: 'inline', content: '- hosts: all'});

	// Git playbook with lazy loaded files tab
	savedPlaybook = {id: 8, name: 'Git PB', source: 'git', git_url: 'https://example.com/repo.git', git_path: 'site.yml', extra_vars: ''};
	await openAnsiblePlaybookModal(8);
	assert.equal(elements.tabAnsiblePbFiles.style.display, 'inline-flex', 'git playbook shows Files tab');
	switchAnsiblePlaybookTab('files');
	await new Promise(resolve => setImmediate(resolve)); // let the lazy load finish
	assert.equal(gets.at(-1), '/api/ansible/playbooks/8/contents');
	assert.equal(elements.ansibleContentsViewer.textContent, '- hosts: all');
	assert.equal(elements.ansibleContentsMeta.textContent, 'commit abcdef01');
	switchAnsiblePlaybookTab('files');
	assert.equal(gets.filter(u => u.endsWith('/contents')).length, 1, 'files are loaded once per editor session, not on every tab switch');

	// Cards rendering without eye/folder buttons or bundle badges
	ansiblePlaybooksCache = [{id: 7, name: 'Deploy', source: 'inline'}, {id: 8, name: 'Git PB', source: 'git', git_url: 'https://github.com/foo.git'}];
	renderAnsiblePlaybooks();
	assert.ok(!elements.ansiblePlaybooksGrid.innerHTML.includes('openAnsiblePlaybookContents'));
	assert.ok(!elements.ansiblePlaybooksGrid.innerHTML.includes('openAnsibleReplaceModal'));
	assert.ok(!elements.ansiblePlaybooksGrid.innerHTML.includes('Imported folder'));
	assert.ok(elements.ansiblePlaybooksGrid.innerHTML.includes('openAnsiblePlaybookModal'));
})().catch(error => {console.error(error); process.exitCode = 1;});
`) + `);
`
	cmd := exec.Command(node, "-e", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Ansible folder import checks failed: %v\n%s", err, out)
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
		`id="btnAnsibleImportFolder"`,
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
		`id="ansibleImportModal"`,
		`id="ansibleImportFiles" class="form-input" webkitdirectory multiple`,
		`id="ansibleImportSelectAll"`,
		`id="ansibleImportTableSection"`,
		`id="ansibleImportTableBody"`,
		`id="tabAnsibleLibrary"`,
		`id="ansiblePanelLibrary"`,
		`id="ansibleConfigCard"`,
		`id="ansibleConfigContent"`,
		`id="btnAnsibleConfigSave"`,
		`id="ansibleImportPreview"`,
		`id="ansibleImportTableBody"`,
		`id="ansibleImportSelectAll"`,
		`id="ansibleImportError"`,
		`id="btnAnsibleImportSubmit"`,
		`id="ansiblePlaybookName"`,
		`id="ansiblePlaybookDesc"`,
		`id="ansiblePlaybookContent"`,
		`id="tabAnsiblePbPlaybook"`,
		`id="tabAnsiblePbVars"`,
		`id="tabAnsiblePbFiles"`,
		`id="ansiblePbVarsTableBody"`,
		`id="ansiblePbNewVarName"`,
		`id="ansiblePbNewVarValue"`,
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
		"/api/ansible/playbooks/import",
		"/api/ansible/playbooks/analyze",
		"/api/ansible/playbooks",
		"/api/ansible/library",
		"/api/ansible/library/galaxy",
		"/api/ansible/library/requirements",
		"/api/ansible/library/local",
		"/api/ansible/config",
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
		"ansible_tab_library",
		"ansible_tab_settings",
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
		"ansible_tab_playbook",
		"ansible_tab_vars",
		"ansible_tab_files",
		"ansible_vars_quote_hint",
		"ansible_vars_del_yaml_title",
		"ansible_vars_del_none_title",
		"ansible_vars_yaml_notfound",
		"ansible_vars_add",
		"ansible_import_folder",
		"ansible_vars_hint",
		"ansible_vars_required",
		"ansible_import_directory",
		"ansible_import_warning",
		"ansible_import_hint",
		"ansible_import_preview",
		"ansible_import_err_files",
		"ansible_import_err_yaml",
		"ansible_import_err_count",
		"ansible_import_err_size",
		"ansible_import_err_path",
		"ansible_import_busy",
		"ansible_import_failed",
		"ansible_git_url_label",
		"ansible_git_missing",
		"ansible_sched_title",
		"ansible_sched_cron_hint",
		"ansible_sched_err_targets",
		"ansible_trigger_schedule",
		"ansible_import_err_playbook_size",
		"ansible_import_select_all",
		"ansible_import_selected_count",
		"ansible_import_col_path",
		"ansible_import_fallback_hint",
		"ansible_import_err_none_selected",
		"ansible_import_err_name_required",
		"ansible_import_err_name_duplicate",
		"ansible_import_err_name_taken",
		"ansible_lib_unavailable_title",
		"ansible_lib_unavailable_desc",
		"ansible_lib_storage_hint",
		"ansible_lib_collections_title",
		"ansible_lib_collections_empty",
		"ansible_lib_roles_title",
		"ansible_lib_roles_empty",
		"ansible_lib_install_galaxy_title",
		"ansible_lib_kind_collection",
		"ansible_lib_kind_role",
		"ansible_lib_galaxy_name_ph",
		"ansible_lib_version_ph",
		"ansible_lib_btn_install",
		"ansible_lib_busy_installing",
		"ansible_lib_install_success",
		"ansible_lib_install_reqs_title",
		"ansible_lib_reqs_ph",
		"ansible_lib_import_local_title",
		"ansible_lib_source_folder",
		"ansible_lib_source_archive",
		"ansible_lib_role_name_ph",
		"ansible_lib_btn_import",
		"ansible_lib_err_role_name",
		"ansible_lib_err_archive_required",
		"ansible_lib_err_name_required",
		"ansible_lib_err_content_required",
		"ansible_lib_import_success",
		"ansible_lib_confirm_delete_title",
		"ansible_lib_confirm_delete_msg",
		"ansible_lib_deleted",
		"ansible_cfg_title",
		"ansible_cfg_hint",
		"ansible_cfg_allowed_sections",
		"ansible_cfg_saved",
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
		"function openAnsibleImportModal",
		"function resetAnsibleImportModal",
		"function prepareAnsibleImportFiles",
		"function onAnsibleImportFilesChange",
		"async function submitAnsibleImportModal",
		"function onAnsibleImportSelectAll",
		"function updateAnsibleImportSelectionCount",
		"async function loadAnsibleLibrary",
		"function renderAnsibleLibrary",
		"async function deleteAnsibleLibraryItem",
		"async function installAnsibleGalaxy",
		"async function installAnsibleRequirements",
		"function onAnsibleLibLocalKindChange",
		"function onAnsibleLibLocalSourceChange",
		"async function installAnsibleLocal",
		"async function loadAnsibleConfig",
		"async function submitAnsibleConfig",
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
	// The card's History button is the only entry point to a playbook's run history; it was lost twice in UI rewrites.
	if !strings.Contains(html, `onclick="openAnsiblePlaybookHistory(${p.id})"`) {
		t.Error("playbook card is missing its Run History button")
	}

	panels := []struct{ name, content string }{
		{"Playbooks", `id="ansiblePlaybooksContainer"`},
		{"Schedules", `id="ansibleSchedulesContainer"`},
		{"Library", `id="ansibleLibCollectionsList"`},
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
