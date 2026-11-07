/* Registry tab refresh hook + Terraform Gate settings card (registry base URL). */
function tfRegistryRefresh() {
  const root = tfEl('tfRegistryRoot');
  if (root) TfUI.reg.mount(root, false);
}

async function tfRegSettings(el) {
  el.insertAdjacentHTML('beforeend', `<div class="card tf-reg-card" id="tfRegSettingsCard">
    <div class="card-title">${tfE(tx('tfregs_title'))}</div>
    <div class="tf-nf-hint">${tfE(tx('tfregs_hint'))}</div>
    <div class="tf-reg-setrow"><input type="text" id="tfRegUrl" class="form-input mono" maxlength="300" spellcheck="false" aria-label="${tfE(tx('tfregs_title'))}" />
      <button type="button" class="btn btn-sm btn-primary" id="tfRegUrlSave">${tfE(t('save', 'Save'))}</button>
      <button type="button" class="btn btn-sm" id="tfRegUrlTest">${tfE(tx('tfregs_test'))}</button></div>
    <span id="tfRegUrlStatus" class="tf-nf-msg" role="status"></span></div>`);
  const msg = (s, bad) => { const m = tfEl('tfRegUrlStatus'); if (m) { m.textContent = s; m.style.color = bad ? 'var(--danger)' : 'var(--text-muted)'; } };
  const input = tfEl('tfRegUrl');
  let saved = '';
  const r = await tfApi('GET', '/api/terraform/registry/settings');
  if (!r.ok) { msg(r.data.error || 'Error', true); return; }
  saved = r.data.base_url === r.data.default_url ? '' : (r.data.base_url || '');
  input.value = saved; input.placeholder = r.data.default_url || '';

  async function save() {
    const v = input.value.trim();
    const s = await tfApi('PUT', '/api/terraform/registry/settings', { base_url: v });
    if (!s.ok) { msg(s.data.error || 'Error', true); return false; }
    saved = v; TfUI.reg.base = ''; // browser re-reads the effective URL
    return true;
  }
  tfEl('tfRegUrlSave').addEventListener('click', async () => {
    if (await save()) { msg(''); showToast(tx('tfregs_saved'), 'success'); }
  });
  tfEl('tfRegUrlTest').addEventListener('click', async () => {
    msg(tx('tfregs_testing'));
    if (input.value.trim() !== saved && !(await save())) return; // test what the user sees
    const s = await tfApi('POST', '/api/terraform/registry/settings/test');
    if (s.ok && s.data.ok) msg(tx('tfregs_test_ok', { ms: s.data.latency_ms || 0 }));
    else msg(tx('tfregs_test_fail', { err: s.data.error || 'Error' }), true);
  });
}

// Wrap the inline Gate extras renderer: keep the notifications card and append the registry card.
const tfGateExtrasBase = tfGateExtras;
tfGateExtras = function (el) {
  const p = tfGateExtrasBase(el); // synchronous part has already replaced el's content
  if (el) tfRegSettings(el);
  return p;
};
