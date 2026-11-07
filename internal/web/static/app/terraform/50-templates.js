/* New Workspace modal: template picker. Called by openTfWorkspaceModal for new workspaces. */
const TF_TPL_PROVIDERS = { aws: 'AWS', gcp: 'GCP', azure: 'Azure', digitalocean: 'DigitalOcean', hetzner: 'Hetzner', cloudflare: 'Cloudflare', kubernetes: 'Kubernetes' };

async function tfTplLoad() {
  if (TfUI.tpl) return TfUI.tpl;
  const r = await tfApi('GET', '/api/terraform/templates');
  if (r.ok) TfUI.tpl = r.data.templates || [];
  return TfUI.tpl || [];
}

function tfTplInject() {
  const nameEl = tfEl('tfWfName');
  if (!nameEl || tfEl('tfTplSelect')) return;
  const blank = Object.assign({}, tfFeCollect()); // restored by the "Blank" option
  nameEl.closest('.dk-form-grid').insertAdjacentHTML('afterbegin',
    `<div class="dk-field dk-full tf-tpl" id="tfTplField"><label for="tfTplSelect">${tfE(tx('tftpl_label'))}</label>
      <select id="tfTplSelect" class="form-input" style="width:100%;box-sizing:border-box;"><option value="">${tfE(tx('tftpl_blank'))}</option></select>
      <div class="tf-tpl-desc" id="tfTplDesc"></div><div class="tf-tpl-hint" id="tfTplHint"></div></div>`);
  const sel = tfEl('tfTplSelect');
  tfTplLoad().then(list => {
    if (tfEl('tfTplSelect') !== sel) return; // modal was closed or reopened meanwhile
    sel.insertAdjacentHTML('beforeend', list.map(p => `<option value="${tfE(p.id)}">${tfE(p.name)}</option>`).join(''));
  });
  sel.addEventListener('change', () => tfTplApply(sel.value, blank));
}

function tfTplApply(id, blank) {
  const p = (TfUI.tpl || []).find(x => x.id === id);
  const nameEl = tfEl('tfWfName');
  const auto = nameEl.dataset.tplAuto;
  if (p) {
    if (!nameEl.value.trim() || nameEl.value === auto) { nameEl.value = p.id; nameEl.dataset.tplAuto = p.id; }
    tfEl('tfWfSource').value = 'inline'; // template files only apply to inline workspaces
    tfWfSourceChanged();
    tfFeInit(p.files);
  } else {
    if (nameEl.value === auto) nameEl.value = '';
    delete nameEl.dataset.tplAuto;
    tfFeInit(blank);
  }
  tfTplInfo(p);
}

function tfTplInfo(p) {
  const vars = p ? (p.variables || []).map(v => v.name).join(', ') : '';
  tfEl('tfTplDesc').innerHTML = p ? tfE(p.description || '') + (vars ? `<div>${tfE(tx('tftpl_vars', { vars }))}</div>` : '') : '';
  tfEl('tfTplHint').innerHTML = p && TF_TPL_PROVIDERS[p.provider] ? `${svgIcon('info', 'icon-xs')} ${tfE(tx('tftpl_cloud_hint', { provider: TF_TPL_PROVIDERS[p.provider] }))}` : '';
}
