package web

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestAnsibleAssetsUIHooks(t *testing.T) {
	html := string(embeddedHTML)
	for _, hook := range []string{
		`id="tabAnsibleTemplates"`, `id="tabAnsibleFiles"`,
		`id="ansiblePanelTemplates"`, `id="ansiblePanelFiles"`,
		`id="ansibleTemplatesList"`, `id="ansibleTemplateName"`, `id="ansibleTemplateContent"`,
		`id="ansibleFilesList"`, `id="ansibleFileInput"`, `id="ansibleFilePrefix"`,
		`onclick="saveAnsibleTemplate()"`, `onclick="uploadAnsibleFiles()"`,
	} {
		if !strings.Contains(html, hook) {
			t.Errorf("missing %s in index.html", hook)
		}
	}
	// Every key used by the two panels exists in both the English and the Indonesian dictionary.
	used := map[string]bool{}
	for _, m := range regexp.MustCompile(`data-i18n(?:-placeholder)?="(ansible_(?:tab_templates|tab_assets_files|tpl_[a-z_]+|files_[a-z_]+|asset_[a-z_]+))"`).FindAllStringSubmatch(html, -1) {
		used[m[1]] = true
	}
	for _, m := range regexp.MustCompile(`t\('(ansible_(?:tpl_[a-z_]+|files_[a-z_]+|asset_[a-z_]+))'`).FindAllStringSubmatch(html, -1) {
		used[m[1]] = true
	}
	if len(used) < 20 {
		t.Fatalf("expected the assets UI to use many i18n keys, found %d", len(used))
	}
	for key := range used {
		if n := strings.Count(html, "\n    \""+key+"\": "); n != 2 {
			t.Errorf("i18n key %q defined %d times, want 2 (en + id)", key, n)
		}
	}
	// The hints are written with innerHTML by setLanguage, so a raw <name> would vanish as a tag.
	if strings.Contains(html, "src=<name>") || strings.Contains(html, "src=<nama>") {
		t.Error("i18n hint contains an unescaped <name> placeholder")
	}
}

func TestAnsibleAssetsUIBehavior(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for Ansible assets UI behavior checks")
	}
	html := string(embeddedHTML)
	_, rest, ok := strings.Cut(html, "let ansibleAssetsCache =")
	if !ok {
		t.Fatal("missing assets JavaScript")
	}
	body, _, ok := strings.Cut(rest, "// ----------------- Ansible Library Flow")
	if !ok {
		t.Fatal("missing assets JavaScript end marker")
	}
	js := "let ansibleAssetsCache =" + body
	script := `
const assert = require('node:assert/strict');
const els = {};
for (const id of ['ansibleTemplatesList', 'ansibleTemplatesEmpty', 'ansibleFilesList', 'ansibleFilesEmpty',
  'ansibleTemplateName', 'ansibleTemplateContent', 'ansibleFileInput', 'ansibleFilePrefix', 'btnAnsibleFilesUpload']) {
  els[id] = {style: {}, innerHTML: '', value: '', scrollIntoView() {}};
}
const document = {getElementById(id) {assert.ok(els[id], id); return els[id];}};
const t = (key, fallback) => fallback;
const svgIcon = () => '';
const escapeHtml = v => String(v).replace(/</g, '&lt;');
const formatBytes = n => n + ' B';
const toasts = [];
const showToast = (msg, kind) => toasts.push([kind, msg]);
let confirmAnswer = true;
const showConfirm = async () => confirmAnswer;
const calls = [];
let respond = () => ({ok: true, json: async () => ({})});
const fetch = async (url, init) => {calls.push([url, init && init.method || 'GET', init && init.body]); return respond(url, init);};
eval(` + strconv.Quote(js+`
(async () => {
  respond = () => ({ok: true, json: async () => ({assets: [
    {id: 1, kind: 'template', name: '<b>site.j2', size: 10},
    {id: 2, kind: 'file', name: 'certs/ca.pem', size: 20}]})});
  await loadAnsibleAssets();
  assert.equal(ansibleAssetsCache.template.length, 1);
  assert.equal(ansibleAssetsCache.file.length, 1);
  assert.ok(els.ansibleTemplatesList.innerHTML.includes('&lt;b>site.j2'), 'names are escaped');
  assert.ok(els.ansibleTemplatesList.innerHTML.includes('editAnsibleTemplate(1)'), 'templates can be edited');
  assert.ok(!els.ansibleFilesList.innerHTML.includes('editAnsibleTemplate'), 'files cannot be edited');
  assert.ok(els.ansibleFilesList.innerHTML.includes("deleteAnsibleAsset('file', 2)"));
  assert.equal(els.ansibleTemplatesEmpty.style.display, 'none');

  // A rejected load (gate off) leaves empty lists rather than stale rows.
  respond = () => ({ok: false, json: async () => ({error: 'ansible is disabled'})});
  await loadAnsibleAssets();
  assert.equal(ansibleAssetsCache.template.length + ansibleAssetsCache.file.length, 0);
  assert.equal(els.ansibleFilesEmpty.style.display, 'block');

  // Saving a template needs a name and PUTs the raw text.
  calls.length = 0;
  respond = () => ({ok: true, json: async () => ({assets: []})});
  await saveAnsibleTemplate();
  assert.equal(calls.length, 0, 'no request without a name');
  els.ansibleTemplateName.value = ' nginx/site.j2 ';
  els.ansibleTemplateContent.value = 'listen {{ port }};';
  await saveAnsibleTemplate();
  assert.deepEqual(calls[0], ['/api/ansible/assets?kind=template&name=nginx%2Fsite.j2', 'PUT', 'listen {{ port }};']);

  // Uploads are PUT with the folder prefix normalised; the first failure stops the batch but the list reloads.
  calls.length = 0; toasts.length = 0;
  const f1 = {name: 'a.pem'}, f2 = {name: 'b.pem'};
  els.ansibleFileInput.files = [f1, f2];
  els.ansibleFilePrefix.value = '/certs';
  await uploadAnsibleFiles();
  assert.deepEqual(calls.slice(0, 2).map(c => c.slice(0, 2)), [
    ['/api/ansible/assets?kind=file&name=certs%2Fa.pem', 'PUT'], ['/api/ansible/assets?kind=file&name=certs%2Fb.pem', 'PUT']]);
  assert.equal(calls[0][2], f1);
  assert.equal(calls[calls.length - 1][0], '/api/ansible/assets', 'list reloaded');
  assert.ok(toasts.some(x => x[0] === 'success' && x[1] === '2 file(s) uploaded'), JSON.stringify(toasts));
  assert.equal(els.btnAnsibleFilesUpload.disabled, false);

  calls.length = 0; toasts.length = 0;
  els.ansibleFileInput.files = [f1, f2];
  respond = (url) => url.startsWith('/api/ansible/assets?') ? {ok: false, status: 413, json: async () => ({error: 'file exceeds 5 MiB'})} : {ok: true, json: async () => ({assets: []})};
  await uploadAnsibleFiles();
  assert.equal(calls.filter(c => c[1] === 'PUT').length, 1, 'stops at the first failure');
  assert.ok(toasts.some(x => x[0] === 'error' && x[1].includes('a.pem: file exceeds 5 MiB')), JSON.stringify(toasts));
  assert.equal(els.btnAnsibleFilesUpload.disabled, false);

  // Delete asks first.
  calls.length = 0;
  respond = () => ({ok: true, json: async () => ({assets: []})});
  ansibleAssetsCache = {template: [], file: [{id: 2, kind: 'file', name: 'x'}]};
  confirmAnswer = false;
  await deleteAnsibleAsset('file', 2);
  assert.equal(calls.length, 0, 'declined confirm must not delete');
  confirmAnswer = true;
  await deleteAnsibleAsset('file', 2);
  assert.deepEqual(calls[0].slice(0, 2), ['/api/ansible/assets/2', 'DELETE']);
})().catch(error => {console.error(error); process.exitCode = 1;});
`) + `);
`
	if out, err := exec.Command(node, "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("assets UI behavior failed: %v\n%s", err, out)
	}
}
