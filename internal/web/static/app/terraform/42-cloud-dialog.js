/* Cloud Accounts dialog (tfDlg1): create/edit with provider-driven fields. */
let tfCaEdit = null; // account being edited, null when creating
let tfCaImpP = null; // importable-accounts request, fetched once per dialog

function tfCaFieldsHTML(provider, vals) {
  const p = tfCaProvider(provider);
  if (!p) return '';
  const secrets = (tfCaEdit && tfCaEdit.secrets_set) || {};
  return p.fields.map(f => {
    const id = 'tfCaF_' + f.key;
    const kept = tfCaEdit && f.secret && secrets[f.key];
    const ph = tfE(kept ? tx('tfca_secret_keep') : (f.placeholder || ''));
    const v = vals[f.key] != null && vals[f.key] !== '' ? vals[f.key] : (tfCaEdit ? '' : (f.default || ''));
    const label = f.label + (f.required ? ' *' : '');
    const inner = f.multiline
      ? `<textarea id="${id}" class="form-input mono" rows="8" spellcheck="false" autocomplete="off" placeholder="${ph}" style="width:100%;box-sizing:border-box;">${tfE(v)}</textarea>`
      : `<input type="${f.secret ? 'password' : 'text'}" id="${id}" class="form-input" style="width:100%;box-sizing:border-box;" ${f.secret ? 'autocomplete="new-password"' : 'autocomplete="off"'} placeholder="${ph}" value="${tfE(v)}" />`;
    return tfField(label, inner, '', f.multiline);
  }).join('');
}

// Current values of the rendered provider fields, keyed by field key.
function tfCaVals() {
  const p = tfCaProvider(tfV('tfCaProvider'));
  const vals = {};
  if (p) p.fields.forEach(f => { const el = tfEl('tfCaF_' + f.key); if (el) vals[f.key] = el.value; });
  return vals;
}

function tfCaProviderChange() {
  const vals = tfCaVals();
  tfEl('tfCaFields').innerHTML = tfCaFieldsHTML(tfV('tfCaProvider'), vals);
  tfCaImportUI();
}

// Import selector (create only, cloudflare/kubernetes): the server copies the credentials of an existing Pantau account.
async function tfCaImportUI() {
  const wrap = tfEl('tfCaImportWrap');
  if (!wrap) return;
  const prov = tfV('tfCaProvider');
  wrap.innerHTML = '';
  tfEl('tfCaFieldWrap').style.display = '';
  if (tfCaEdit || (prov !== 'cloudflare' && prov !== 'kubernetes')) return;
  if (!tfCaImpP) tfCaImpP = tfApi('GET', '/api/terraform/cloud-accounts/importable');
  const r = await tfCaImpP;
  const list = (r.ok && r.data[prov]) || [];
  if (!list.length || tfV('tfCaProvider') !== prov || !wrap.isConnected) return;
  const opts = [['', tx('tfca_import_manual')]].concat(list.map(a => [a.id, a.usable ? a.name : a.name + ' ' + tx('tfca_import_unusable')]));
  const sel = tfSelect('tfCaImport', opts, '', 'tfCaImportChange()');
  wrap.innerHTML = tfField(tx('tfca_import_' + prov), sel, '', true) + '<div id="tfCaImportNote" class="dk-hint" style="display:none;"></div>';
  list.forEach((a, i) => { if (!a.usable) tfEl('tfCaImport').options[i + 1].disabled = true; });
}
function tfCaImportChange() {
  const id = tfV('tfCaImport');
  tfEl('tfCaFieldWrap').style.display = id ? 'none' : '';
  const note = tfEl('tfCaImportNote');
  note.style.display = id ? 'block' : 'none';
  note.textContent = id ? tx('tfca_import_note') : '';
}

async function tfOpenCloudAccount(id) {
  const r = await tfCaLoad();
  if (!r.ok) { tfErr(r); return; }
  tfCaEdit = id ? TfUI.cloud.accounts.find(x => x.id === id) || null : null;
  if (id && !tfCaEdit) return;
  const provs = TfUI.cloud.providers;
  const cur = tfCaEdit ? tfCaEdit.provider : (provs[0] ? provs[0].id : '');
  const body = `<div class="dk-form-grid tf-ca-form">
    ${tfField(tx('tfca_provider'), tfSelect('tfCaProvider', provs.map(p => [p.id, p.label]), cur, 'tfCaProviderChange()'))}
    ${tfField(tx('tfca_name') + ' *', tfInput('tfCaName', tfCaEdit ? tfCaEdit.name : '', 'maxlength="100" required'))}
    ${tfField(tx('tfca_desc'), tfInput('tfCaDesc', tfCaEdit ? tfCaEdit.description : '', 'maxlength="500"'), '', true)}
    <div id="tfCaImportWrap" class="dk-full"></div>
    <div id="tfCaFieldWrap" class="dk-full tf-ca-fieldwrap"><div class="dk-form-grid" id="tfCaFields">${tfCaFieldsHTML(cur, tfCaEdit ? tfCaEdit.fields || {} : {})}</div></div></div>`;
  const save = `<button type="button" id="tfCaSave" class="btn btn-sm btn-primary" onclick="tfCaSave()">${tfE(t('save', 'Save'))}</button>`;
  tfCaImpP = null;
  tfDlg(1, tx(tfCaEdit ? 'tfca_edit_title' : 'tfca_new_title'), body, tfFoot(1, save), 620);
  if (tfCaEdit) tfEl('tfCaProvider').disabled = true;
  tfCaImportUI();
  tfEl('tfCaName').focus();
}

async function tfCaSave() {
  const provider = tfV('tfCaProvider');
  const p = tfCaProvider(provider);
  const name = tfV('tfCaName').trim();
  if (!name) { tfDlgErr(1, tx('tfca_name_required')); return; }
  const imp = tfV('tfCaImport'); // '' / absent = manual entry
  const fields = imp ? {} : tfCaVals();
  const secrets = (tfCaEdit && tfCaEdit.secrets_set) || {};
  for (const f of imp || !p ? [] : p.fields) {
    const keep = tfCaEdit && f.secret && secrets[f.key] && !fields[f.key];
    if (f.required && !keep && !String(fields[f.key] || '').trim()) { tfDlgErr(1, tx('tfca_field_required', { field: f.label })); return; }
  }
  const btn = tfEl('tfCaSave');
  btn.disabled = true;
  tfDlgErr(1, '');
  const edit = tfCaEdit;
  // Untouched secrets on edit stay '' so the server keeps the stored value.
  const payload = { name, provider, description: tfV('tfCaDesc'), fields };
  if (imp) payload.import_from = { type: provider, id: Number(imp) };
  const r = await tfApi(edit ? 'PUT' : 'POST', '/api/terraform/cloud-accounts' + (edit ? '/' + edit.id : ''), payload);
  btn.disabled = false;
  if (!r.ok) { tfDlgErr(1, r.data.error || 'Error'); return; }
  tfDlgClose(1);
  showToast(tx('tfca_saved'), 'success');
  await tfCloudRefresh();
  if (!edit) {
    const newId = r.data.id || (r.data.account && r.data.account.id);
    if (newId) { await tfCaTestToast(newId); tfCloudRefresh(); }
  }
}
