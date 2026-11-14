/* Settings tab: renderer, dirty guard, save/reset wiring. */
tfTabRenderers.settings = function (body) {
  const w = tfWs;
  if (!w) return;
  const git = w.source === 'git';
  body.innerHTML = `<div class="tf-set-grid">
    <div class="tf-set-col">${tfSetBody(w)}</div>
    <div class="tf-set-col">${tfSetSideHTML(w)}</div>
    <div class="tf-set-foot">
      <span id="tfSetDirty" class="tf-set-dirty" hidden>${tfE(tx('tfset_dirty'))}</span>
      <span id="tfSetError" class="tf-set-error" role="alert"></span>
      <span class="tf-set-foot-btns">
        <button type="button" class="btn btn-sm" id="tfSetReset">${tfE(t('tfset_reset', 'Reset'))}</button>
        <button type="button" class="btn btn-sm btn-primary" id="tfSetSave">${tfE(t('save', 'Save'))}</button>
      </span></div></div>`;

  const mark = () => { tfEl('tfSetDirty').hidden = !tfSetDirty(); };
  const fields = ['tfSetName', 'tfSetGroup', 'tfSetEnv', 'tfSetDesc', 'tfSetTags', 'tfSetBinary', 'tfSetDrift', 'tfSetPlanInterval', 'tfSetAutoApply'];
  if (git) fields.push('tfSetGitUrl', 'tfSetGitBranch', 'tfSetGitPath', 'tfSetGitAuth', 'tfSetGitToken', 'tfSetGitKey');
  fields.forEach(id => { const el = tfEl(id); if (el) el.addEventListener('input', mark); });
  // Auto-apply is never allowed for production: follow the Environment field live.
  const syncProd = () => {
    const prod = tfIsProd(tfV('tfSetEnv')), cb = tfEl('tfSetAutoApply');
    if (prod) cb.checked = false;
    cb.disabled = prod;
    const wrap = tfEl('tfSetAutoApplyWrap'), hint = wrap.querySelector('.dk-hint');
    if (hint) hint.remove();
    if (prod) wrap.insertAdjacentHTML('beforeend', `<div class="dk-hint">${tfE(tx('tfset_auto_apply_prod'))}</div>`);
  };
  tfEl('tfSetEnv').addEventListener('input', () => { syncProd(); mark(); });

  const cloud = tfEl('tfSetCloud');
  cloud.addEventListener('change', mark);
  cloud.addEventListener('click', e => { if (e.target.closest('[data-ca-manage]')) tfCaLinkManage(); });

  if (git) {
    const syncAuth = () => {
      const a = tfV('tfSetGitAuth');
      tfEl('tfSetTokenWrap').style.display = a === 'token' ? '' : 'none';
      tfEl('tfSetKeyWrap').style.display = a === 'ssh' ? '' : 'none';
    };
    syncAuth();
    tfEl('tfSetGitAuth').addEventListener('change', syncAuth);
    tfEl('tfSetGitTest').addEventListener('click', (e) => tfVcsTest(e.currentTarget));
    tfApi('GET', '/api/ssh-keys').then(r => {
      const keys = r.ok && Array.isArray(r.data) ? r.data : [];
      const s = tfEl('tfSetGitKey');
      if (!s) return;
      s.innerHTML = keys.map(k => `<option value="${Number(k.id)}" ${Number(k.id) === Number(w.git_key_id) ? 'selected' : ''}>${tfE(k.name)}</option>`).join('');
    });
  } else {
    tfEl('tfSetOpenConfig').addEventListener('click', () => tfSwitchTabGuarded('config'));
  }

  tfEl('tfSetReset').addEventListener('click', () => tfTabRenderers.settings(tfEl('tfWsBody')));
  tfEl('tfSetSave').addEventListener('click', async () => {
    const err = tfEl('tfSetError');
    err.textContent = '';
    if (!tfV('tfSetName').trim()) { err.textContent = tx('tfx_name_required'); return; }
    const r = await tfApi('PUT', `/api/terraform/workspaces/${w.id}`, tfSetCollect());
    if (!r.ok) { err.textContent = r.data.error || 'Error'; return; }
    tfDirtyGuard = null;
    showToast(tx('tfx_saved'), 'success');
    await tfReloadWs();
    await loadTfWorkspaces();
    tfTabRenderers.settings(tfEl('tfWsBody'));
  });

  tfDirtyGuard = async () => {
    if (!tfEl('tfSetName') || !tfSetDirty()) return true;
    return await showConfirm({ title: tx('tfset_dirty'), message: tx('tfset_unsaved_msg'), confirmText: tx('tfset_leave'), danger: true });
  };
};
