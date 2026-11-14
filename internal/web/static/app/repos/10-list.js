/* Repositories section: cloned Git repositories (folder, clone, sync, credentials, remove). Editor: 20-editor.js. */
(function () {
  'use strict';
  const R = window.Repos;

async function rpLoad() {
  const r = await R.api('GET', '/api/repos');
  if (r.ok) R.state = { list: r.data.repos || [], folder: r.data.folder || '', loaded: true };
  if (window.IacScan) {
    const fleet = await window.IacScan.fleet();
    R.state.iac = fleet;
  }
  return r;
}

function rpCardHTML(r) {
  const s = r.status;
  const id = Number(r.id);
  const badges = r.error
    ? `<span class="badge badge-errored" title="${R.esc(r.error)}">${R.esc(R.tx('rp_unavailable'))}</span>`
    : `<span class="badge badge-info">${R.esc(s.branch)}</span><span class="mono rp-meta">${R.esc(R.short(s.commit))}</span>` +
      (s.dirty ? `<span class="badge badge-warning">${R.esc(R.tx('rp_changes_n', { n: s.dirty }))}</span>` : `<span class="badge badge-healthy">${R.esc(R.tx('rp_clean'))}</span>`) +
      (s.ahead ? `<span class="badge badge-info" title="${R.esc(R.tx('rp_ed_ahead', { n: s.ahead }))}">↑${s.ahead}</span>` : '') +
      (s.behind ? `<span class="badge badge-warning" title="${R.esc(R.tx('rp_ed_behind', { n: s.behind }))}">↓${s.behind}</span>` : '') +
      (window.IacScan && R.state.iac ? window.IacScan.chipHtml('repo', id, R.state.iac['repo:' + id]) : '');
  const when = r.synced_at ? R.tx('rp_synced', { when: formatRelativeTime(r.synced_at).relative }) : '';

  return `<div class="card rp-card">
    <div class="rp-name" title="${R.esc(r.name)}">${R.esc(r.name)}</div>
    <div class="rp-url mono">${R.esc(r.url)}</div>
    <div class="rp-badges">${badges}</div>
    <div class="rp-meta" title="${R.esc(r.dir)}">${R.esc(r.auth)}${when ? ' · ' + R.esc(when) : ''}</div>
    <div class="rp-actions">
      <button type="button" class="btn btn-sm btn-primary" data-rp-open="${id}"${r.error ? ' disabled' : ''}>${svgIcon('folder-open', 'icon-xs')} ${R.esc(R.tx('rp_open'))}</button>
      <button type="button" class="btn btn-sm" data-rp-sync="${id}"${r.error ? ' disabled' : ''}>${svgIcon('refresh-cw', 'icon-xs')} ${R.esc(R.tx('rp_sync'))}</button>
      <button type="button" class="btn btn-sm" data-rp-copy="${id}"${r.error ? ' disabled' : ''} title="${R.esc(R.tx('rp_copy_title'))}">${svgIcon('upload', 'icon-xs')} ${R.esc(R.tx('rp_copy'))}</button>
      <button type="button" class="btn btn-sm" data-rp-move="${id}" title="${R.esc(R.tx('rp_move_title'))}">${svgIcon('folder', 'icon-xs')} ${R.esc(R.tx('rp_f_folder'))}</button>
      <button type="button" class="btn btn-sm" data-rp-creds="${id}">${svgIcon('pencil', 'icon-xs')} ${R.esc(R.tx('rp_creds'))}</button>
      ${window.IacScan ? window.IacScan.btnHtml('repo', id) : ''}
      <button type="button" class="btn btn-icon btn-sm" data-rp-del="${id}" aria-label="${R.esc(R.tx('rp_remove'))}">${svgIcon('trash-2', 'icon-xs')}</button>
    </div></div>`;
}

// Folders (groups): repositories are grouped by their folder like Terraform workspaces by group; sections collapse.
let rpFolderFilter = 'all'; // 'all' | a folder name | '__none__' (no folder)
function rpCollapsed() {
  try { return new Set(JSON.parse(localStorage.getItem('pantau_repo_collapsed') || '[]')); } catch (e) { return new Set(); }
}
function rpToggleFolder(key) {
  const set = rpCollapsed();
  if (set.has(key)) set.delete(key); else set.add(key);
  try { localStorage.setItem('pantau_repo_collapsed', JSON.stringify(Array.from(set))); } catch (e) { /* ignore */ }
  rpRender();
}
const rpFolders = () => Array.from(new Set(R.state.list.map(r => r.group).filter(Boolean))).sort((x, y) => x.localeCompare(y));

function rpSectionHTML(key, label, repos, collapsed) {
  const dirty = repos.reduce((n, r) => n + ((r.status && r.status.dirty) || 0), 0);
  return `<section class="rp-group">
    <button type="button" class="rp-group-head" data-rp-toggle="${R.esc(key)}" aria-expanded="${!collapsed}">
      ${svgIcon(collapsed ? 'chevron-right' : 'chevron-down', 'icon-xs')}${svgIcon('folder', 'icon-sm')}
      <span class="rp-group-name">${R.esc(label)}</span>
      <span class="badge badge-info">${R.esc(R.tx(repos.length === 1 ? 'rp_repos_1' : 'rp_repos_n', { n: repos.length }))}</span>
      ${dirty ? `<span class="badge badge-warning">${R.esc(R.tx('rp_changes_n', { n: dirty }))}</span>` : ''}
    </button>${collapsed ? '' : `<div class="rp-grid">${repos.map(rpCardHTML).join('')}</div>`}</section>`;
}

function rpRender() {
  const root = R.el('reposRoot');
  if (!root) return;
  const list = R.state.list;
  const folders = rpFolders();
  if (rpFolderFilter !== 'all' && rpFolderFilter !== '__none__' && !folders.includes(rpFolderFilter)) rpFolderFilter = 'all';
  if (rpFolderFilter === '__none__' && !folders.length) rpFolderFilter = 'all';
  const shown = list.filter(r => rpFolderFilter === 'all' || (rpFolderFilter === '__none__' ? !r.group : r.group === rpFolderFilter));
  let body;
  if (!list.length) {
    body = `<div class="rp-empty">${svgIcon('git-branch', 'icon')}<div class="rp-empty-title">${R.esc(R.tx('rp_empty_title'))}</div>
          <div class="rp-empty-desc">${R.esc(R.tx('rp_empty_desc'))}</div>
          <button type="button" class="btn btn-sm btn-primary" data-rp-clone>${R.esc(R.tx('rp_clone'))}</button></div>`;
  } else if (!folders.length) {
    body = `<div class="rp-grid">${list.map(rpCardHTML).join('')}</div>`;
  } else {
    const collapsed = rpCollapsed();
    const sections = folders.map(f => [f, f, shown.filter(r => r.group === f)]);
    const loose = shown.filter(r => !r.group);
    if (loose.length) sections.push(['__none__', R.tx('rp_ungrouped'), loose]);
    body = sections.filter(x => x[2].length).map(([k, label, repos]) => rpSectionHTML(k, label, repos, collapsed.has(k) && rpFolderFilter === 'all')).join('');
  }
  const filter = folders.length
    ? `<select id="rpFolderFilter" class="form-input rp-filter-sel" aria-label="${R.esc(R.tx('rp_f_folder'))}" onchange="Repos.setFolderFilter(this.value)">
        <option value="all">${R.esc(R.tx('rp_all_folders'))}</option>${folders.map(f => `<option value="${R.esc(f)}"${f === rpFolderFilter ? ' selected' : ''}>${R.esc(f)}</option>`).join('')}
        <option value="__none__"${rpFolderFilter === '__none__' ? ' selected' : ''}>${R.esc(R.tx('rp_ungrouped'))}</option></select>`
    : '';
  root.className = 'rp';
  root.innerHTML = `<div class="rp-bar"><div class="rp-hint">${R.esc(R.tx('rp_hint'))}</div>
      <div class="rp-bar-actions">${filter}<button type="button" class="btn btn-sm btn-primary" data-rp-clone>${svgIcon('plus', 'icon-sm')} ${R.esc(R.tx('rp_clone'))}</button></div></div>
    <div class="rp-folder">${svgIcon('folder', 'icon-xs')} ${R.esc(R.tx('rp_folder'))}: <span class="mono">${R.esc(R.state.folder)}</span>
      <button type="button" class="btn btn-sm" data-rp-folder>${R.esc(R.tx('rp_folder_change'))}</button></div>` + body;
  root.onclick = rpClick;
}

async function rpRefresh() {
  const root = R.el('reposRoot');
  if (!root) return;
  if (!R.state.loaded) root.innerHTML = SP.loading(R.tx('rp_loading'));
  const r = await rpLoad();
  if (!r.ok) { root.innerHTML = R.empty(r.data.error || 'Error'); return; }
  rpRender();
}

/* ---- auth fields shared by the clone and credentials dialogs ---- */
let rpKeys = [];
let rpEditRepo = null; // repository whose credentials are edited; null while cloning

function rpAuthHTML(auth, keyId) {
  const opts = [['none', R.tx('rp_auth_none')], ['ssh', R.tx('rp_auth_ssh')], ['token', R.tx('rp_auth_token')]];
  const keySel = rpKeys.length
    ? `<select id="rpKey" class="form-input" style="width:100%;box-sizing:border-box;">${rpKeys.map(k => `<option value="${Number(k.id)}" ${Number(k.id) === Number(keyId) ? 'selected' : ''}>${R.esc(k.name)}</option>`).join('')}</select>`
    : `<div class="dk-hint">${R.esc(R.tx('rp_no_keys'))}</div>`;
  const keep = rpEditRepo && rpEditRepo.token_set;
  return R.field(R.tx('rp_f_auth'), R.select('rpAuth', opts, auth, 'Repos.authChange()'), '', true) +
    `<div id="rpKeyWrap" class="dk-full">${R.field(R.tx('rp_f_key'), keySel, '', true)}</div>` +
    `<div id="rpTokenWrap" class="dk-full">${R.field(R.tx('rp_f_token'), `<input type="password" id="rpToken" class="form-input" autocomplete="new-password" style="width:100%;box-sizing:border-box;" placeholder="${R.esc(keep ? R.tx('rp_token_keep') : '')}" />`, '', true)}</div>`;
}
function rpAuthChange() {
  const a = R.val('rpAuth');
  R.el('rpKeyWrap').style.display = a === 'ssh' ? '' : 'none';
  R.el('rpTokenWrap').style.display = a === 'token' ? '' : 'none';
}
function rpAuthBody() {
  const auth = R.val('rpAuth');
  return { auth, key_id: auth === 'ssh' ? parseInt(R.val('rpKey'), 10) || 0 : 0, token: auth === 'token' ? R.val('rpToken') : '' };
}
async function rpLoadKeys() {
  const kr = await R.api('GET', '/api/ssh-keys');
  rpKeys = kr.ok && Array.isArray(kr.data) ? kr.data : [];
}

async function rpOpenClone(pre) {
  pre = pre || {};
  await rpLoadKeys();
  rpEditRepo = null;
  const body = `<div class="dk-form-grid">
    ${R.field(R.tx('rp_f_url') + ' *', R.input('rpUrl', pre.url || '', 'placeholder="git@github.com:org/infra.git" autocomplete="off" spellcheck="false"'), '', true)}
    ${R.field(R.tx('rp_f_folder'), rpFolderInput('rpGroup', pre.group !== undefined ? pre.group : (rpFolderFilter !== 'all' && rpFolderFilter !== '__none__' ? rpFolderFilter : '')), R.tx('rp_f_folder_hint'), true)}
    ${R.field(R.tx('rp_f_name'), R.input('rpName', pre.name || '', `maxlength="64" placeholder="${R.esc(R.tx('rp_f_name_ph'))}" autocomplete="off"`))}
    ${R.field(R.tx('rp_f_branch'), R.input('rpBranch', pre.branch || '', `placeholder="${R.esc(R.tx('rp_f_branch_ph'))}" autocomplete="off"`))}
    ${rpAuthHTML(pre.auth || (rpKeys.length ? 'ssh' : 'none'), pre.key_id || 0)}</div>`;
  const go = `<button type="button" id="rpGo" class="btn btn-sm btn-primary" onclick="Repos.doClone()">${R.esc(R.tx('rp_clone_btn'))}</button>`;
  R.dlg(R.tx('rp_clone'), body, R.foot(go), 620);
  rpAuthChange();
  if (pre.auth) R.el('rpAuth').dataset.touched = '1';
  R.el('rpUrl').addEventListener('input', () => { // pick the matching auth for the URL shape while it is still untouched
    if (R.el('rpAuth').dataset.touched) return;
    R.el('rpAuth').value = /^https:/i.test(R.val('rpUrl')) ? 'none' : (rpKeys.length ? 'ssh' : 'none');
    rpAuthChange();
  });
  R.el('rpAuth').addEventListener('change', e => { e.target.dataset.touched = '1'; });
  R.el('rpUrl').focus();
}

async function rpDoClone() {
  const url = R.val('rpUrl').trim();
  if (!url) { R.dlgErr(R.tx('rp_f_url') + ' *'); return; }
  const btn = R.el('rpGo');
  const restore = SP.busy(btn, R.tx('rp_cloning'));
  R.dlgErr('');
  const req = Object.assign({ url, group: R.val('rpGroup').trim(), name: R.val('rpName').trim(), branch: R.val('rpBranch').trim() }, rpAuthBody());
  let r;
  try {
    r = await R.api('POST', '/api/repos', req);
  } finally {
    restore();
  }
  if (!r.ok) {
    R.dlgErr(r.data.error || 'Error');
    return;
  }
  rpFollow(r.data.job, req);
}

/* ---- clone progress: the server clones in the background; this follows git's output until it ends ---- */
let rpJob = null; // { id, req } of the clone the dialog follows
const rpDlgShowsJob = () => !!(R.el('rpLog') && R.el('repoDlg').classList.contains('open'));

async function rpFollow(id, req) {
  rpJob = { id, req };
  const t0 = Date.now();
  R.dlg(R.tx('rp_clone'),
    `<div class="rp-prog"><div class="rp-prog-head"><span class="spinner" id="rpSpin">${svgIcon('loader-circle', 'icon-sm')}</span> <span id="rpProgTitle">${R.esc(R.tx('rp_cloning'))}</span> <span data-sp-t0="${t0}" style="font-variant-numeric:tabular-nums;opacity:0.8;">0s</span></div>
      <pre id="rpLog" class="rp-log" aria-live="polite"></pre><div class="dk-hint">${R.esc(R.tx('rp_clone_bg'))}</div></div>`,
    `<button type="button" id="rpCancel" class="btn btn-sm" onclick="Repos.cancelClone()">${R.esc(R.tx('rp_clone_cancel'))}</button>`, 680);
  let rpFailures = 0; // consecutive poll errors tolerated before the follow gives up
  while (rpJob && rpJob.id === id) {
    const r = await R.api('GET', '/api/repos/clone/' + id);
    if (!r.ok) {
      if (++rpFailures < 3) { await new Promise(res => setTimeout(res, 800)); continue; }
      rpFinish(id, 'error', r.data.error || 'Error');
      return;
    }
    rpFailures = 0;
    const log = R.el('rpLog');
    if (log) {
      const stick = log.scrollTop + log.clientHeight >= log.scrollHeight - 24;
      log.textContent = r.data.lines.join('\n');
      if (stick) log.scrollTop = log.scrollHeight;
      const title = R.el('rpProgTitle');
      if (title && r.data.name) title.textContent = R.tx('rp_clone_progress', { name: r.data.name });
    }
    if (r.data.state !== 'running') { rpFinish(id, r.data.state, r.data.error); return; }
    await new Promise(res => setTimeout(res, 600));
  }
}

function rpFinish(id, state, msg) {
  if (!rpJob || rpJob.id !== id) return;
  const req = rpJob.req;
  rpJob = null;
  const showing = rpDlgShowsJob();
  if (state === 'done') {
    if (showing) R.dlgClose();
    showToast(R.tx('rp_cloned'), 'success');
    rpRefresh();
  } else if (state === 'canceled') {
    if (showing) R.dlgClose();
    showToast(R.tx('rp_clone_canceled'), 'info');
  } else if (showing) {
    const spin = R.el('rpSpin'); if (spin) spin.style.display = 'none';
    const el = document.querySelector('#repoDlgBody [data-sp-t0]'); if (el) el.removeAttribute('data-sp-t0'); // freeze the elapsed counter
    R.el('rpProgTitle').textContent = R.tx('rp_clone_failed');
    R.dlgErr(msg || 'Error');
    R.el('repoDlgFooter').innerHTML = R.foot(`<button type="button" class="btn btn-sm btn-primary" onclick="Repos.retryClone()">${R.esc(R.tx('rp_clone_retry'))}</button>`);
    rpLastReq = req;
  } else {
    showToast(R.tx('rp_clone_failed') + ': ' + (msg || 'Error'), 'error');
  }
}
let rpLastReq = null;
async function rpCancelClone() {
  if (!rpJob) return;
  const btn = R.el('rpCancel');
  const restore = SP.busy(btn, R.tx('rp_clone_canceling'));
  let r;
  try {
    r = await R.api('DELETE', '/api/repos/clone/' + rpJob.id);
  } finally {
    restore();
  }
  if (!r.ok) showToast(R.tx('rp_clone_failed') + ': ' + (r.data.error || 'Error'), 'error');
}
function rpRetryClone() { rpOpenClone(rpLastReq || {}); }

async function rpOpenCreds(id) {
  const repo = R.state.list.find(x => x.id === id);
  if (!repo) return;
  await rpLoadKeys();
  rpEditRepo = repo;
  const body = `<div class="dk-form-grid"><div class="dk-full rp-url mono">${R.esc(repo.url)}</div>${rpAuthHTML(repo.auth, repo.key_id)}</div>`;
  R.dlg(R.tx('rp_creds_title') + ' — ' + repo.name, body, R.foot(R.primary(t('save', 'Save'), `Repos.saveCreds(${id})`)), 560);
  rpAuthChange();
}
async function rpSaveCreds(id) {
  const r = await R.api('PUT', '/api/repos/' + id, rpAuthBody());
  if (!r.ok) { R.dlgErr(r.data.error || 'Error'); return; }
  R.dlgClose();
  showToast(R.tx('rp_creds_saved'), 'success');
  await rpRefresh();
}

// Text input that suggests the existing folders.
function rpFolderInput(id, val) {
  return R.input(id, val, `list="${id}List" maxlength="64" autocomplete="off" spellcheck="false"`) +
    `<datalist id="${id}List">${rpFolders().map(f => `<option value="${R.esc(f)}"></option>`).join('')}</datalist>`;
}
function rpOpenMove(id) {
  const repo = R.state.list.find(x => x.id === id);
  if (!repo) return;
  const body = `<div class="dk-form-grid"><div class="dk-full rp-url mono">${R.esc(repo.dir)}</div>${R.field(R.tx('rp_f_folder'), rpFolderInput('rpMoveGroup', repo.group || ''), R.tx('rp_move_hint'), true)}</div>`;
  R.dlg(R.tx('rp_move_title') + ' — ' + repo.name, body, R.foot(R.primary(t('save', 'Save'), `Repos.saveMove(${id})`)), 560);
  R.el('rpMoveGroup').focus();
}
async function rpSaveMove(id) {
  const r = await R.api('PUT', `/api/repos/${id}/group`, { group: R.val('rpMoveGroup').trim() });
  if (!r.ok) { R.dlgErr(r.data.error || 'Error'); return; }
  R.dlgClose();
  showToast(R.tx('rp_moved'), 'success');
  await rpRefresh();
}

/* ---- copy to a host: streams the working tree into a directory of a Host (Transfer Queue job) ---- */
async function rpOpenCopy(id) {
  const repo = R.state.list.find(x => x.id === id);
  if (!repo) return;
  let hosts = typeof allRawHosts !== 'undefined' ? allRawHosts : [];
  if (!hosts.length) {
    const hr = await R.api('GET', '/api/hosts');
    hosts = hr.ok && Array.isArray(hr.data) ? hr.data : [];
  }
  if (!hosts.length) { showToast(R.tx('rp_copy_nohosts'), 'error'); return; }
  let last = {};
  try { last = JSON.parse(localStorage.getItem('pantau_repo_copy_' + id) || '{}') || {}; } catch (e) { /* ignore */ }
  const dirty = repo.status && repo.status.dirty;
  const body = `<div class="dk-form-grid"><div class="dk-full rp-url mono">${R.esc(repo.group ? repo.group + '/' + repo.name : repo.name)}</div>
    ${R.field(R.tx('rp_copy_host'), R.select('rpCopyHost', hosts.map(h => [h.id, h.name + ' (' + h.host + ')']), last.host || hosts[0].id), '', true)}
    ${R.field(R.tx('rp_copy_path'), R.input('rpCopyPath', last.path || '/opt/' + repo.name, 'autocomplete="off" spellcheck="false"'), R.tx('rp_copy_path_hint'), true)}
    <div class="dk-full"><label style="display:flex;align-items:center;gap:8px;font-size:var(--fs-xs);cursor:pointer;"><input type="checkbox" id="rpCopyGit" ${last.git ? 'checked' : ''} /> <span>${R.esc(R.tx('rp_copy_git'))}</span></label></div>
    <div class="dk-full dk-hint">${R.esc(R.tx('rp_copy_what'))}${dirty ? ' <b>' + R.esc(R.tx('rp_copy_dirty', { n: dirty })) + '</b>' : ''}</div></div>`;
  R.dlg(R.tx('rp_copy_title') + ' — ' + repo.name, body, R.foot(`<button type="button" id="rpCopyGo" class="btn btn-sm btn-primary" onclick="Repos.doCopy(${id})">${R.esc(R.tx('rp_copy'))}</button>`), 620);
  R.el('rpCopyPath').focus();
}
async function rpDoCopy(id) {
  const host = parseInt(R.val('rpCopyHost'), 10) || 0;
  const path = R.val('rpCopyPath').trim();
  const git = !!R.el('rpCopyGit').checked;
  if (!path) { R.dlgErr(R.tx('rp_copy_path') + ' *'); return; }
  const restore = SP.busy(R.el('rpCopyGo'), R.tx('rp_copy'));
  R.dlgErr('');
  let r;
  try {
    r = await R.api('POST', `/api/repos/${id}/copy`, { host_id: host, path, include_git: git });
  } finally {
    restore();
  }
  if (!r.ok) {
    R.dlgErr(r.data.error || 'Error');
    return;
  }
  try { localStorage.setItem('pantau_repo_copy_' + id, JSON.stringify({ host, path, git })); } catch (e) { /* ignore */ }
  R.dlgClose();
  showToast(R.tx('rp_copy_started', { n: r.data.files, host: r.data.job.dest_host_name }), 'success');
  if (typeof openTransfersModal === 'function') openTransfersModal(); // progress, speed and cancel live in the Transfer Queue
}

function rpOpenFolder() {
  const body = `<div class="dk-form-grid">${R.field(R.tx('rp_folder_label'), R.input('rpFolder', R.state.folder, 'autocomplete="off" spellcheck="false"'), R.tx('rp_folder_hint'), true)}</div>`;
  R.dlg(R.tx('rp_folder_title'), body, R.foot(R.primary(t('save', 'Save'), 'Repos.saveFolder()')), 560);
  R.el('rpFolder').focus();
}
async function rpSaveFolder() {
  const r = await R.api('PUT', '/api/repos/folder', { folder: R.val('rpFolder').trim() });
  if (!r.ok) { R.dlgErr(r.data.error || 'Error'); return; }
  R.dlgClose();
  showToast(R.tx('rp_folder_saved'), 'success');
  await rpRefresh();
}

async function rpSync(btn, id) {
  const repo = R.state.list.find(x => x.id === id);
  const restore = SP.busy(btn, R.tx('rp_syncing'));
  try {
    const r = await R.api('POST', `/api/repos/${id}/sync`);
    if (r.ok) showToast(R.tx('rp_sync_ok', { name: repo ? repo.name : id, commit: R.short(r.data.commit) }), 'success');
    else R.err(r);
  } finally {
    restore();
  }
  await rpRefresh();
}

async function rpRemove(id) {
  const repo = R.state.list.find(x => x.id === id);
  if (!repo) return;
  if (!(await showConfirm({ title: R.tx('rp_remove_title'), message: R.tx('rp_remove_msg', { name: repo.name, dir: repo.dir }), confirmText: R.tx('rp_remove'), danger: true }))) return;
  const btn = document.querySelector('[data-rp-del="' + id + '"]');
  const restore = SP.busy(btn, R.tx('rp_removing'));
  try {
    const r = await R.api('DELETE', '/api/repos/' + id);
    if (!r.ok) { R.err(r); return; }
    showToast(R.tx('rp_removed'), 'success');
  } finally {
    restore();
  }
  await rpRefresh();
}

function rpClick(ev) {
  const el = ev.target.closest('button');
  if (!el) return;
  const d = el.dataset;
  if (d.rpToggle) return rpToggleFolder(d.rpToggle);
  if (d.rpClone !== undefined) return rpOpenClone();
  if (d.rpFolder !== undefined) return rpOpenFolder();
  if (d.rpOpen) return R.openEditor(Number(d.rpOpen));
  if (d.rpMove) return rpOpenMove(Number(d.rpMove));
  if (d.rpCopy) return rpOpenCopy(Number(d.rpCopy));
  if (d.rpSync) return rpSync(el, Number(d.rpSync));
  if (d.rpCreds) return rpOpenCreds(Number(d.rpCreds));
  if (d.rpDel) return rpRemove(Number(d.rpDel));
}

langRerenderHooks.push(() => {
  const dlg = R.el('repoDlg');
  if (R.state.loaded && R.el('reposRoot') && !(dlg && dlg.classList.contains('open'))) rpRender();
});

  Object.assign(R, { refresh: rpRefresh, doClone: rpDoClone, cancelClone: rpCancelClone, doCopy: rpDoCopy, saveMove: rpSaveMove, setFolderFilter: (v) => { rpFolderFilter = v; rpRender(); }, retryClone: rpRetryClone, authChange: rpAuthChange, saveCreds: rpSaveCreds, saveFolder: rpSaveFolder });
})();
