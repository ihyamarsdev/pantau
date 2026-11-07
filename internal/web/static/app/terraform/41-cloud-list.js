/* Cloud Accounts tab: account cache, card grid, test/edit/delete actions. */
TfUI.cloud = { accounts: [], providers: [], loaded: false };

async function tfCaLoad() {
  const r = await tfApi('GET', '/api/terraform/cloud-accounts');
  if (r.ok) TfUI.cloud = { accounts: r.data.accounts || [], providers: r.data.providers || [], loaded: true };
  return r;
}
function tfCaProvider(id) { return TfUI.cloud.providers.find(p => p.id === id); }
function tfCaBadge(provider) {
  const p = tfCaProvider(provider);
  return `<span class="tf-ca-badge tf-ca-${tfE(String(provider).replace(/[^a-z0-9]/g, ''))}">${tfE(p ? p.label : provider)}</span>`;
}
function tfCaChip(a) {
  if (!a.verified_at) return `<span class="badge badge-never">${tfE(tx('tfca_unverified'))}</span>`;
  if (!a.verify_ok) return `<span class="badge badge-errored" title="${tfE(a.verify_message || '')}">${tfE(tx('tfca_failed'))}</span>`;
  const rel = formatRelativeTime(a.verified_at);
  return `<span class="badge badge-healthy" title="${tfE(a.verify_message || rel.title)}">${tfE(tx('tfca_verified', { when: rel.relative }))}</span>`;
}

function tfCaCardHTML(a) {
  const p = tfCaProvider(a.provider);
  const secrets = a.secrets_set || {};
  const fields = ((p && p.fields) || []).map(f => {
    let v;
    if (f.secret) v = secrets[f.key] ? tx('tfca_secret_set') : tx('tfca_secret_unset');
    else v = (a.fields || {})[f.key];
    if (v == null || v === '') return '';
    return `<div><dt>${tfE(f.label)}</dt><dd class="${f.secret ? 'tf-ca-secret' : 'mono'}" title="${f.secret ? '' : tfE(v)}">${tfE(v)}</dd></div>`;
  }).join('');
  const n = a.workspace_count || 0;
  return `<div class="card tf-ca-card" data-ca="${Number(a.id)}">
    <div class="tf-ca-head">${tfCaBadge(a.provider)}${tfCaChip(a)}</div>
    <div class="tf-ca-name" title="${tfE(a.name)}">${tfE(a.name)}</div>
    ${a.description ? `<div class="tf-ca-desc">${tfE(a.description)}</div>` : ''}
    <div class="tf-ca-used">${tfE(n === 0 ? tx('tfca_used_0') : n === 1 ? tx('tfca_used_1') : tx('tfca_used_n', { n }))}</div>
    <dl class="tf-ca-kv">${fields}</dl>
    <div class="tf-ca-actions">
      <button type="button" class="btn btn-sm" data-ca-test="${Number(a.id)}">${svgIcon('shield-check', 'icon-xs')} ${tfE(tx('tfca_test'))}</button>
      <button type="button" class="btn btn-sm" data-ca-edit="${Number(a.id)}">${svgIcon('pencil', 'icon-xs')} ${tfE(tx('tfx_edit'))}</button>
      <button type="button" class="btn btn-icon btn-sm" data-ca-del="${Number(a.id)}" aria-label="${tfE(t('delete', 'Delete'))}">${svgIcon('trash-2', 'icon-xs')}</button>
    </div></div>`;
}

function tfCaRender() {
  const root = tfEl('tfCloudRoot');
  if (!root) return;
  const accs = TfUI.cloud.accounts;
  root.className = 'tf-ca';
  root.innerHTML = `<div class="tf-ca-bar"><div class="tf-ca-hint">${tfE(tx('tfca_hint'))}</div>
      <button type="button" class="btn btn-sm btn-primary" id="btnTfCloudNew">${svgIcon('plus', 'icon-sm')} ${tfE(tx('tfca_new'))}</button></div>` +
    (accs.length
      ? `<div class="tf-ca-grid" aria-label="${tfE(tx('tfca_title'))}">${accs.map(tfCaCardHTML).join('')}</div>`
      : `<div class="tf-ca-empty">${svgIcon('cloud', 'icon')}<div class="tf-ca-empty-title">${tfE(tx('tfca_empty_title'))}</div>
          <div class="tf-ca-empty-desc">${tfE(tx('tfca_empty_desc'))}</div>
          <button type="button" class="btn btn-sm btn-primary" id="btnTfCloudFirst">${tfE(tx('tfca_add_first'))}</button></div>`);
  root.onclick = tfCaClick;
}

async function tfCloudRefresh() {
  const root = tfEl('tfCloudRoot');
  if (!root) return;
  if (!TfUI.cloud.loaded) root.innerHTML = tfEmpty(tx('tfca_loading'));
  const r = await tfCaLoad();
  if (!r.ok) { root.innerHTML = tfEmpty(r.data.error || 'Error'); return; }
  tfCaRender();
}

async function tfCaTest(btn, id) {
  btn.disabled = true;
  btn.innerHTML = `<span class="spinner">${svgIcon('loader-circle', 'icon-xs')}</span> ${tfE(tx('tfca_testing'))}`;
  await tfCaTestToast(id);
  await tfCloudRefresh();
}
// Runs the verifier and reports the result as a toast; never throws.
async function tfCaTestToast(id) {
  const r = await tfApi('POST', `/api/terraform/cloud-accounts/${id}/test`);
  if (r.ok && r.data.ok) showToast(tx('tfca_test_ok', { id: r.data.identity || 'ok' }), 'success');
  else showToast(tx('tfca_test_fail', { err: r.data.error || 'error' }), 'error');
}

async function tfCaDelete(id) {
  const a = TfUI.cloud.accounts.find(x => x.id === id);
  if (!a) return;
  const n = a.workspace_count || 0;
  const message = tx('tfca_del_msg', { name: a.name }) + (n > 0 ? ' ' + tx('tfca_del_msg_ws', { n }) : '');
  if (!(await showConfirm({ title: tx('tfca_del_title'), message, confirmText: t('delete', 'Delete'), danger: true }))) return;
  const r = await tfApi('DELETE', '/api/terraform/cloud-accounts/' + id);
  if (!r.ok) { tfErr(r); return; }
  showToast(tx('tfca_deleted'), 'success');
  await tfCloudRefresh();
}

function tfCaClick(ev) {
  const el = ev.target.closest('button');
  if (!el) return;
  if (el.id === 'btnTfCloudNew' || el.id === 'btnTfCloudFirst') return tfOpenCloudAccount();
  if (el.dataset.caTest) return tfCaTest(el, Number(el.dataset.caTest));
  if (el.dataset.caEdit) return tfOpenCloudAccount(Number(el.dataset.caEdit));
  if (el.dataset.caDel) return tfCaDelete(Number(el.dataset.caDel));
}

// Skip while the dialog is open so a half-filled form is not wiped.
langRerenderHooks.push(() => {
  const dlg = tfEl('tfDlg1');
  if (TfUI.cloud.loaded && tfEl('tfCloudRoot') && !(dlg && dlg.classList.contains('open'))) tfCaRender();
});
