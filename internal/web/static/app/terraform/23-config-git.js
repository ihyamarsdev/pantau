/* Terraform Configuration tab: read-only card for Git-source workspaces. */
(function () {
  'use strict';
  const C = (window.TfUI = window.TfUI || {}).cfg;

  C.renderGit = function (body) {
    const w = tfWs;
    const row = (k, v) => `<span class="tf-cfg-gk">${tfE(tx(k))}</span><span class="mono">${tfE(v)}</span>`;
    body.innerHTML = `<div class="tf-cfg tf-cfg-git"><div class="card tf-cfg-gitcard">
      <div class="card-title"><span>${tfE(tx('tfcfg_title'))}</span></div>
      <div style="display:grid;grid-template-columns:auto 1fr;gap:6px 14px;font-size:var(--fs-xs);">
        ${row('tfx_git_url', w.git_url || '—')}
        ${row('tfx_git_branch', w.git_branch || 'default')}
        ${row('tfx_git_path', w.git_path || '/')}
        ${row('tfx_git_auth', (w.git_auth || 'none') + (w.git_token_set ? ' (' + tx('tfx_set') + ')' : ''))}</div>
      <div class="tf-cfg-gitbtns">
        <button type="button" id="tfCfgGitEdit" class="btn btn-sm">${svgIcon('pencil', 'icon-xs')} ${tfE(tx('tfcfg_git_edit'))}</button>
        <button type="button" id="tfCfgGitTest" class="btn btn-sm">${svgIcon('git-compare', 'icon-xs')} ${tfE(t('tf_vcs_test_btn', 'Test VCS Connection'))}</button>
        <span id="tfVcsResult" style="font-size:var(--fs-xs);"></span></div>
      <div class="tf-cfg-hint">${tfE(tx('tfcfg_git_hint'))}</div></div></div>`;
    body.querySelector('#tfCfgGitEdit').addEventListener('click', () => openTfWorkspaceModal(w.id));
    body.querySelector('#tfCfgGitTest').addEventListener('click', (e) => tfVcsTest(e.currentTarget));
  };
})();
