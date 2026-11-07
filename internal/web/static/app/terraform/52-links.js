/* Variables tab: "Linked outputs" card (workspace outputs -> terraform variables). */
const tfRenderVariablesBase = tfRenderVariables;
// Wrap the inline renderer: it is also re-invoked by its own toggle buttons and refresh callbacks.
tfRenderVariables = async function (body, effective) {
  await tfRenderVariablesBase(body, effective);
  if (tfWs && tfWsTab === 'variables' && body.isConnected) tfLkLoad(body);
};
tfTabRenderers.variables = body => tfRenderVariables(body);

async function tfLkLoad(body) {
  const id = tfWs.id;
  body.insertAdjacentHTML('beforeend', `<div class="card tf-lk" id="tfLinksCard"><div class="card-title"><span>${tfE(tx('tflk_title'))}</span></div>
    <div class="tf-lk-hint">${tfE(tx('tflk_hint'))}</div><div id="tfLkBody">${tfEmpty(tx('tfca_loading'))}</div></div>`);
  const r = await tfApi('GET', `/api/terraform/workspaces/${id}/output-links`);
  if (!tfWs || tfWs.id !== id || !tfEl('tfLkBody')) return;
  if (!r.ok) { tfEl('tfLkBody').innerHTML = tfEmpty(r.data.error || 'Error'); return; }
  const names = {};
  (r.data.links || []).forEach(l => { names[l.source_workspace_id] = l.source_workspace_name; });
  TfUI.lk = { sources: r.data.sources || [], names, rows: (r.data.links || []).map(l => ({ v: l.var_name, src: l.source_workspace_id, out: l.output_name })) };
  tfLkRender();
}

function tfLkRender() {
  const { sources, rows } = TfUI.lk;
  tfEl('tfLkBody').innerHTML = `<div id="tfLkRows" class="tf-lk-rows">${rows.map(tfLkRow).join('') || tfEmpty(sources.length ? tx('tflk_empty') : tx('tflk_no_sources'))}</div>
    <div class="tf-lk-foot"><span id="tfLkErr" class="tf-lk-err" role="alert"></span>
      <button type="button" class="btn btn-sm" id="tfLkAdd" ${sources.length ? '' : 'disabled'}>${svgIcon('plus', 'icon-xs')} ${tfE(tx('tflk_add'))}</button>
      <button type="button" class="btn btn-sm btn-primary" id="tfLkSave">${tfE(tx('tflk_save'))}</button></div>`;
  const root = tfEl('tfLkBody');
  tfEl('tfLkAdd').onclick = tfLkAdd;
  tfEl('tfLkSave').onclick = tfLkSave;
  root.onclick = e => { const d = e.target.closest('[data-lk-del]'); if (d) { tfLkCollect(); TfUI.lk.rows.splice(Number(d.dataset.lkDel), 1); tfLkRender(); } };
  root.onchange = e => {
    if (!e.target.matches('[data-lk-src],[data-lk-out]')) return;
    tfLkCollect();
    if (e.target.matches('[data-lk-src]')) { const row = TfUI.lk.rows[Number(e.target.closest('[data-lk-row]').dataset.lkRow)], s = TfUI.lk.sources.find(x => x.id === row.src); row.out = s && s.outputs[0] ? s.outputs[0].name : ''; }
    tfLkRender();
  };
}

function tfLkRow(r, i) {
  const { sources, names } = TfUI.lk;
  const src = sources.find(s => s.id === r.src);
  const outs = src ? src.outputs : [];
  const ok = outs.some(o => o.name === r.out);
  const srcOpts = sources.map(s => [s.id, s.name]);
  if (r.src && !src) srcOpts.push([r.src, names[r.src] || '#' + r.src]);
  const outOpts = outs.map(o => [o.name, o.name + (o.sensitive ? ' (' + tx('tflk_sensitive') + ')' : '')]);
  if (r.out && !ok) outOpts.push([r.out, r.out]);
  const sel = (attr, opts, cur, label) => `<select class="form-input" ${attr} aria-label="${tfE(label)}"><option value="">—</option>${opts.map(([v, l]) => `<option value="${tfE(v)}" ${String(v) === String(cur) ? 'selected' : ''}>${tfE(l)}</option>`).join('')}</select>`;
  return `<div class="tf-lk-row" data-lk-row="${i}">
    <input type="text" class="form-input" data-lk-var value="${tfE(r.v)}" placeholder="${tfE(tx('tflk_var'))}" maxlength="64" spellcheck="false" />
    ${sel('data-lk-src', srcOpts, r.src, tx('tflk_source'))}${sel('data-lk-out', outOpts, r.out, tx('tflk_output'))}
    ${r.src && r.out && !ok ? `<span class="tf-lk-chip">${svgIcon('triangle-alert', 'icon-xs')} ${tfE(tx('tflk_unavailable'))}</span>` : ''}
    <button type="button" class="btn btn-icon btn-sm" data-lk-del="${i}" title="${tfE(tx('tflk_remove'))}" aria-label="${tfE(tx('tflk_remove'))}">${svgIcon('trash-2', 'icon-xs')}</button></div>`;
}

function tfLkCollect() {
  TfUI.lk.rows = Array.from(document.querySelectorAll('#tfLkRows [data-lk-row]')).map(el => ({
    v: el.querySelector('[data-lk-var]').value.trim(), src: Number(el.querySelector('[data-lk-src]').value) || 0, out: el.querySelector('[data-lk-out]').value }));
}

function tfLkAdd() {
  tfLkCollect();
  const s = TfUI.lk.sources[0];
  TfUI.lk.rows.push({ v: '', src: s.id, out: s.outputs[0] ? s.outputs[0].name : '' });
  tfLkRender();
}

async function tfLkSave() {
  tfLkCollect();
  const rows = TfUI.lk.rows;
  const bad = rows.some(r => !/^[A-Za-z_][A-Za-z0-9_]*$/.test(r.v)) || new Set(rows.map(r => r.v)).size !== rows.length ? 'tflk_var_bad' : rows.some(r => !r.src || !r.out) ? 'tflk_pick' : '';
  tfEl('tfLkErr').textContent = bad ? tx(bad) : '';
  if (bad) return;
  const r = await tfApi('PUT', `/api/terraform/workspaces/${tfWs.id}/output-links`, { links: rows.map(x => ({ var_name: x.v, source_workspace_id: x.src, output_name: x.out })) });
  if (!r.ok) { if (tfEl('tfLkErr')) tfEl('tfLkErr').textContent = r.data.error || 'Error'; return; }
  showToast(tx('tflk_saved'), 'success');
  tfEl('tfLinksCard').remove();
  tfLkLoad(tfEl('tfWsBody'));
}
