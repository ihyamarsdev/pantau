/* Terraform Configuration tab: inline-source editor view, wiring, save/discard, dirty guard. */
(function () {
  'use strict';
  const C = (window.TfUI = window.TfUI || {}).cfg;

  function fileListHTML() {
    return C.names().map(n => `<div class="tf-cfg-frow${n === C.cur ? ' active' : ''}${C.dirty(n) ? ' dirty' : ''}">
      <button type="button" class="tf-cfg-fbtn" data-file="${tfE(n)}" aria-current="${n === C.cur}" title="${tfE(n)}"><i class="tf-cfg-dot"></i><span class="tf-cfg-fname">${tfE(n)}</span></button>
      <button type="button" class="tf-cfg-fdel" data-del="${tfE(n)}" title="${tfE(tx('tfcfg_del'))}" aria-label="${tfE(tx('tfcfg_del'))} ${tfE(n)}">${svgIcon('trash-2', 'icon-xs')}</button></div>`).join('');
  }
  function headerHTML() {
    return `<div class="tf-cfg-bar">
      <span id="tfCfgCurName" class="tf-cfg-name mono">${tfE(C.cur)}</span>
      <span id="tfCfgSize" class="tf-cfg-size">${tfE(C.kib(C.docs[C.cur] && C.docs[C.cur].getValue()))} / ${C.MAX_KIB} KiB</span>
      <span id="tfCfgDirty" class="tf-cfg-dirty" hidden title="${tfE(tx('tfcfg_unsaved_title'))}">●</span>
      <span class="tf-cfg-gap"></span>
      <span class="tf-cfg-grp"><button type="button" id="tfCfgFmt" class="btn btn-sm" title="${tfE(tx('tfcfg_fmt_title'))}">${tfE(tx('tfcfg_fmt'))}</button>
      <button type="button" id="tfCfgRegistry" class="btn btn-sm" title="${tfE(tx('tfcfg_registry_title'))}">${svgIcon('package', 'icon-xs')} ${tfE(tx('tfcfg_registry'))}</button>
      <button type="button" id="tfCfgRegCheck" class="btn btn-sm" title="${tfE(tx('tfcfg_regcheck_title'))}">${svgIcon('refresh-cw', 'icon-xs')} ${tfE(tx('tfcfg_regcheck'))}</button>
      <button type="button" id="tfCfgValidate" class="btn btn-sm" title="${tfE(tx('tfcfg_validate_title'))}">${svgIcon('list-checks', 'icon-xs')} ${tfE(tx('tfcfg_validate'))}</button></span>
      <span class="tf-cfg-grp"><button type="button" id="tfCfgDiscard" class="btn btn-sm" ${C.isDirty() ? '' : 'disabled'}>${tfE(tx('tfcfg_discard'))}</button>
      <button type="button" id="tfCfgSavePlan" class="btn btn-sm">${svgIcon('zap', 'icon-xs')} ${tfE(tx('tfcfg_save_plan'))}</button>
      <button type="button" id="tfCfgSave" class="btn btn-sm btn-primary">${svgIcon('save', 'icon-xs')} ${tfE(tx('tfcfg_save'))}</button></span></div>`;
  }
  C.fileUI = function () {
    if (!C.root) return;
    const list = tfEl('tfCfgFiles'); if (list) list.innerHTML = fileListHTML();
    const name = tfEl('tfCfgCurName'); if (name) name.textContent = C.cur;
    const size = tfEl('tfCfgSize'); if (size) size.textContent = `${C.kib(C.docs[C.cur] && C.docs[C.cur].getValue())} / ${C.MAX_KIB} KiB`;
    C.dirtyUI();
  };
  C.dirtyUI = function () {
    if (!C.root) return;
    const d = tfEl('tfCfgDirty'); if (d) d.hidden = !C.isDirty();
    const dis = tfEl('tfCfgDiscard'); if (dis) dis.disabled = !C.isDirty();
    const row = C.root.querySelector('.tf-cfg-frow.active'); if (row) row.classList.toggle('dirty', C.dirty(C.cur));
  };
  async function doSave(thenPlan) {
    const err = tfEl('tfCfgError');
    if (err) { err.hidden = true; err.textContent = ''; }
    const w = tfWs;
    const body = {
      name: w.name, description: w.description || '', group_name: w.group_name || '', environment: w.environment || '',
      tags: w.tags || [], binary: w.binary || 'terraform', source: 'inline',
      drift_interval_minutes: w.drift_interval_minutes || 0, files: C.collect(),
    };
    const r = await tfApi('PUT', '/api/terraform/workspaces/' + w.id, body);
    if (!r.ok) {
      if (err) { err.textContent = `${tx('tfcfg_save_err')}: ${r.data.error || ''}`; err.hidden = false; }
      else tfErr(r);
      return;
    }
    for (const n of C.names()) C.files[n] = C.docs[n] ? C.docs[n].getValue() : C.files[n];
    C.markSaved();
    await tfReloadWs();
    C.fileUI();
    showToast(tx('tfx_saved'), 'success');
    if (thenPlan) openTfRunDialog('plan');
  }
  async function doDelete(n) {
    const ok = await showConfirm({ title: tx('tfcfg_del'), message: tx('tfcfg_del_confirm', { name: n }), confirmText: tx('tfcfg_del_confirm_t'), danger: true });
    if (ok && C.remove(n)) C.fileUI();
  }
  C.render = function (body) {
    C.load(tfWs);
    C.mounted = true;
    body.innerHTML = `<div class="tf-cfg"><div class="tf-cfg-main">
      <aside class="tf-cfg-side">
        <div class="tf-cfg-add"><input type="text" id="tfCfgNewName" class="form-input" placeholder="${tfE(tx('tfx_file_name_ph'))}" aria-label="${tfE(tx('tfx_add_file'))}" />
          <button type="button" id="tfCfgAddFile" class="btn btn-sm" title="${tfE(tx('tfx_add_file'))}">${svgIcon('plus', 'icon-xs')}</button></div>
        <div id="tfCfgFiles" class="tf-cfg-list">${fileListHTML()}</div>
        ${tfWs.locked ? `<div class="tf-cfg-hint">${svgIcon('lock', 'icon-xs')} ${tfE(tx('tfcfg_locked_hint'))}</div>` : ''}</aside>
      <section class="tf-cfg-ed">${headerHTML()}
        <div id="tfCfgError" class="tf-cfg-err" role="alert" hidden></div>
        <div id="tfCfgDiag" class="tf-cfg-diag" role="region" aria-label="${tfE(tx('tfcfg_diag_title'))}" hidden></div>
        <div id="tfCfgRegStatus" class="tf-regc" role="region" aria-label="${tfE(tx('tfcfg_regcheck'))}" hidden></div>
        <div id="tfCfgEditor" class="tf-cfg-edbox"></div></section></div></div>`;
    C.root = body.querySelector('.tf-cfg');
    C.cm = CodeMirror(tfEl('tfCfgEditor'), { lineNumbers: true, theme: 'vscode-dark', mode: null, indentUnit: 2, tabSize: 2, indentWithTabs: false, extraKeys: { Tab: cm => cm.replaceSelection('  ', 'end') } });
    C.cm.swapDoc(C.docFor(C.cur));
    const root = C.root;
    root.addEventListener('click', (e) => {
      const f = e.target.closest('[data-file]'); if (f) { C.open(f.dataset.file); return; }
      const del = e.target.closest('[data-del]'); if (del) doDelete(del.dataset.del);
    });
    const addFile = () => { if (C.add(tfV('tfCfgNewName'))) tfEl('tfCfgNewName').value = ''; };
    tfEl('tfCfgAddFile').addEventListener('click', addFile);
    tfEl('tfCfgNewName').addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); addFile(); } });
    tfEl('tfCfgSave').addEventListener('click', () => doSave(false));
    tfEl('tfCfgSavePlan').addEventListener('click', () => doSave(true));
    tfEl('tfCfgDiscard').addEventListener('click', async () => {
      if (await showConfirm({ title: tx('tfcfg_discard'), message: tx('tfcfg_unsaved_msg'), confirmText: tx('tfcfg_discard'), danger: true })) C.discard();
    });
    root.addEventListener('keydown', (e) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') { e.preventDefault(); e.stopPropagation(); doSave(false); return; }
      if (e.key === 'Escape' && C.cm && C.cm.hasFocus() && C.isDirty()) e.stopPropagation(); // keep the drawer open while editing
    });
    if (C.toolsInit) C.toolsInit();
    C.obs = new MutationObserver(() => { if (C.root && !C.root.isConnected) C.destroy(); });
    C.obs.observe(body, { childList: true });
    try {
      tfDirtyGuard = async () => {
        if (!C.mounted || !C.isDirty()) return true;
        return await showConfirm({ title: tx('tfcfg_unsaved_title'), message: tx('tfcfg_unsaved_msg'), confirmText: tx('tfcfg_leave'), danger: true });
      };
    } catch (e) { /* tfDirtyGuard hook not landed yet */ }
  };
})();
