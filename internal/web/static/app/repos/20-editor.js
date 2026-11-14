/* Repositories: Git editor modal (file tree, CodeMirror, save to working tree, commit & push). List: 71-repos-list.js.
   Edits are written to the clone's working tree on Save; Commit & Push stages the selected files, commits and pushes. */
(function () {
  'use strict';
  const R = window.Repos;
  const Ed = (R.editor = { id: 0, name: '', d: null, docs: {}, gens: {}, cur: '', cm: null, filter: '', open: new Set(), dirs: {}, search: null, wired: false });
  const api = (m, sub, body) => R.api(m, '/api/repos/' + Ed.id + sub, body);
  const MARK = { new: '+', modified: 'M', deleted: '−' };
  const statusMap = () => { const m = {}; ((Ed.d && Ed.d.changes) || []).forEach(c => { m[c.path] = c.status; }); return m; };
  const isDirty = p => { const d = Ed.docs[p]; return !!d && !d.isClean(Ed.gens[p]); };
  const dirtyPaths = () => Object.keys(Ed.docs).filter(isDirty);
  const mark = s => s ? `<span class="rp-mark ${s}" title="${R.esc(R.tx('rp_st_' + s))}">${MARK[s]}</span>` : '';

  /* ---- folders: loaded one level at a time (a big repository never fits one list) ---- */
  // Ed.dirs maps a folder path ('' = root, no trailing slash) to its HEAD listing; Ed.open holds the expanded folders.
  function expandTo(p) {
    const parts = p.split('/');
    for (let i = 1; i < parts.length; i++) Ed.open.add(parts.slice(0, i).join('/'));
  }
  async function loadDir(path) {
    const r = await api('GET', '/tree?path=' + encodeURIComponent(path));
    Ed.dirs[path] = r.ok ? { entries: r.data.entries || [], truncated: !!r.data.truncated } : { entries: [], truncated: false, error: r.data.error || 'Error' };
  }
  // Load the root and every expanded folder that is not cached yet.
  async function loadDirs() {
    Ed.treeT0 = Date.now();
    await Promise.all([''].concat(Array.from(Ed.open)).filter(p => !Ed.dirs[p]).map(loadDir));
  }
  // The HEAD listing of a folder plus what only exists in the working tree (new files and folders).
  function dirEntries(path) {
    const base = Ed.dirs[path] ? Ed.dirs[path].entries : [];
    const seen = new Set(base.map(e => e.name));
    const prefix = path ? path + '/' : '';
    const extra = [];
    for (const c of Ed.d.changes) {
      if (c.status === 'deleted' || !c.path.startsWith(prefix)) continue;
      const rest = c.path.slice(prefix.length);
      const i = rest.indexOf('/');
      const name = i < 0 ? rest : rest.slice(0, i);
      if (seen.has(name)) continue;
      seen.add(name);
      extra.push({ name, dir: i >= 0, size: 0 });
    }
    return base.concat(extra).sort((x, y) => (x.dir !== y.dir ? (x.dir ? -1 : 1) : x.name.localeCompare(y.name)));
  }

  /* ---- sidebar tree + search ---- */
  const fileRow = (p, sm, label) => {
    const s = sm[p];
    return `<button type="button" class="rp-row${p === Ed.cur ? ' active' : ''}${s === 'deleted' ? ' deleted' : ''}" data-file="${R.esc(p)}" title="${R.esc(p)}">${svgIcon('file', 'icon-xs')}<span class="rp-n">${label}</span>${isDirty(p) ? '<i class="rp-dot"></i>' : ''}${mark(s)}</button>`;
  };
  function nodeHTML(path, sm, changedDirs) {
    const prefix = path ? path + '/' : '';
    const d = Ed.dirs[path];
    let html = dirEntries(path).map(e => {
      const p = prefix + e.name;
      if (!e.dir) return fileRow(p, sm, R.esc(e.name));
      const open = Ed.open.has(p);
      return `<button type="button" class="rp-row" data-dir="${R.esc(p)}">${svgIcon(open ? 'chevron-down' : 'chevron-right', 'icon-xs')}${svgIcon('folder', 'icon-xs')}<span class="rp-n">${R.esc(e.name)}</span>${changedDirs.has(p) ? '<i class="rp-dot"></i>' : ''}</button>` +
        (open ? `<div class="rp-kids">${Ed.dirs[p] ? nodeHTML(p, sm, changedDirs) : `<div class="rp-trunc">${SP.html(R.tx('rp_loading'), Ed.treeT0 || Date.now())}</div>`}</div>` : '');
    }).join('');
    if (d && d.truncated) html += `<div class="rp-trunc">${R.esc(R.tx('rp_ed_truncated'))}</div>`;
    if (d && d.error) html += `<div class="rp-trunc">${R.esc(d.error)}</div>`;
    return html;
  }
  function searchHTML(sm) {
    const s = Ed.search;
    if (!s || s.files === null) return `<div class="rp-trunc">${SP.html(R.tx('rp_ed_searching'), s && s.t0 ? s.t0 : Date.now())}</div>`;
    if (s.error) return `<div class="rp-trunc">${R.esc(s.error)}</div>`;
    if (!s.files.length) return `<div class="rp-trunc">${R.esc(R.tx('rp_ed_nomatch'))}</div>`;
    return s.files.map(f => {
      const i = f.path.lastIndexOf('/');
      return fileRow(f.path, sm, `${R.esc(f.path.slice(i + 1))}${i >= 0 ? `<span class="rp-dir"> ${R.esc(f.path.slice(0, i))}</span>` : ''}`);
    }).join('') + (s.truncated ? `<div class="rp-trunc">${R.esc(R.tx('rp_ed_search_cap', { n: f_cap }))}</div>` : '');
  }
  const f_cap = 200; // mirrors repoSearchLimit on the server
  function renderTree() {
    const box = R.el('rpTree');
    if (!box || !Ed.d) return;
    const sm = statusMap();
    const tr = R.el('rpTrunc');
    tr.hidden = !Ed.d.changes_truncated;
    tr.textContent = R.tx('rp_ed_changes_truncated');
    if (Ed.filter.trim()) { box.innerHTML = searchHTML(sm); return; }
    const changedDirs = new Set();
    Object.keys(sm).concat(dirtyPaths()).forEach(p => { const parts = p.split('/'); for (let i = 1; i < parts.length; i++) changedDirs.add(parts.slice(0, i).join('/')); });
    box.innerHTML = nodeHTML('', sm, changedDirs);
  }
  let searchTimer = null;
  let searchSeq = 0;
  async function runSearch() {
    const q = Ed.filter.trim();
    const seq = ++searchSeq;
    const t0 = Ed.search && Ed.search.t0 ? Ed.search.t0 : Date.now();
    const r = await api('GET', '/search?q=' + encodeURIComponent(q));
    if (seq !== searchSeq) return; // a newer query replaced this one
    Ed.search = { files: r.ok ? r.data.files || [] : [], truncated: r.ok && !!r.data.truncated, error: r.ok ? '' : (r.data.error || 'Error'), t0: t0 };
    renderTree();
  }
  function onFilter(value) {
    Ed.filter = value;
    clearTimeout(searchTimer);
    searchSeq++;
    Ed.search = value.trim() ? { files: null, t0: Date.now() } : null;
    renderTree();
    if (value.trim()) searchTimer = setTimeout(runSearch, 250);
  }

  /* ---- header tools ---- */
  function renderTools() {
    const el = R.el('rpTools');
    if (!el || !Ed.d) return;
    const d = Ed.d;
    const has = d.changes.length > 0 || dirtyPaths().length > 0;
    el.innerHTML = `<span class="badge badge-info">${R.esc(d.branch)}</span><span class="mono rp-meta">${R.esc(R.short(d.commit))}</span>` +
      (d.ahead ? `<span class="badge badge-info" title="${R.esc(R.tx('rp_ed_ahead', { n: d.ahead }))}">↑${d.ahead}</span>` : '') +
      (d.behind ? `<span class="badge badge-warning" title="${R.esc(R.tx('rp_ed_behind', { n: d.behind }))}">↓${d.behind}</span>` : '') +
      `<button type="button" class="btn btn-sm" data-act="refresh">${svgIcon('rotate-ccw', 'icon-xs')} ${R.esc(R.tx('rp_ed_refresh'))}</button>
       <button type="button" class="btn btn-sm" data-act="sync">${svgIcon('download', 'icon-xs')} ${R.esc(R.tx('rp_sync'))}</button>` +
      (d.ahead ? `<button type="button" class="btn btn-sm" data-act="push">${svgIcon('upload', 'icon-xs')} ${R.esc(R.tx('rp_ed_push'))}</button>` : '') +
      `<button type="button" class="btn btn-sm btn-primary" data-act="commit" ${has ? '' : 'disabled'}>${svgIcon('git-compare', 'icon-xs')} ${R.esc(R.tx('rp_ed_commit'))}</button>`;
  }
  /* ---- file bar + editor ---- */
  function renderFbar() {
    const el = R.el('rpFbar');
    if (!el) return;
    const p = Ed.cur;
    Ed.shownDirty = !!p && isDirty(p);
    if (!p) { el.innerHTML = ''; return; }
    const s = statusMap()[p];
    el.innerHTML = `<span class="rp-fname mono">${R.esc(p)}</span>${isDirty(p) ? `<span class="badge badge-warning">${R.esc(R.tx('rp_ed_unsaved'))}</span>` : ''}${mark(s)}<span class="rp-gap"></span>
      <button type="button" class="btn btn-sm btn-primary" data-act="save" ${isDirty(p) ? '' : 'disabled'}>${svgIcon('save', 'icon-xs')} ${R.esc(R.tx('rp_ed_save'))}</button>
      <button type="button" class="btn btn-sm" data-act="discard" ${s ? '' : 'disabled'}>${svgIcon('rotate-ccw', 'icon-xs')} ${R.esc(R.tx('rp_ed_discard'))}</button>
      <button type="button" class="btn btn-sm" data-act="delete" ${s === 'deleted' ? 'disabled' : ''}>${svgIcon('trash-2', 'icon-xs')} ${R.esc(R.tx('rp_ed_delete'))}</button>`;
  }
  function showNote(msg) {
    R.el('rpEditor').hidden = true;
    const n = R.el('rpNote');
    n.textContent = msg;
    n.hidden = false;
  }
  function showDoc(p) {
    R.el('rpNote').hidden = true;
    R.el('rpEditor').hidden = false;
    Ed.cm.swapDoc(Ed.docs[p]);
    Ed.cm.refresh();
    Ed.cm.focus();
  }
  async function openFile(p) {
    Ed.cur = p;
    expandTo(p);
    const s = statusMap()[p];
    if (s === 'deleted') showNote(R.tx('rp_ed_deleted_note'));
    else {
      if (!Ed.docs[p]) {
        const r = await api('GET', '/file?path=' + encodeURIComponent(p));
        if (Ed.cur !== p) return; // another file was picked while loading
        if (!r.ok) showNote(r.status === 415 ? R.tx('rp_ed_binary') : r.status === 413 ? R.tx('rp_ed_toolarge') : (r.data.error || 'Error'));
        else {
          const doc = CodeMirror.Doc(r.data.content, R.modeFor(p));
          Ed.docs[p] = doc;
          Ed.gens[p] = doc.changeGeneration(true);
        }
      }
      if (Ed.docs[p]) showDoc(p);
    }
    renderFbar();
    renderTree();
    loadDirs().then(renderTree); // the folders above a file opened from the search
  }
  function clearCur() {
    Ed.cur = '';
    showNote(R.tx('rp_ed_pick'));
    renderFbar();
  }

  /* ---- load / mount ---- */
  function mount() {
    R.el('rpBody').innerHTML = `<aside class="rp-side">
        <div class="rp-add"><input type="text" id="rpNew" class="form-input" placeholder="${R.esc(R.tx('rp_ed_newfile'))}" aria-label="${R.esc(R.tx('rp_ed_add'))}" autocomplete="off" spellcheck="false" />
          <button type="button" class="btn btn-sm" data-act="add" title="${R.esc(R.tx('rp_ed_add'))}">${svgIcon('file-plus', 'icon-xs')}</button></div>
        <input type="text" id="rpFilter" class="form-input rp-filter" placeholder="${R.esc(R.tx('rp_ed_filter'))}" aria-label="${R.esc(R.tx('rp_ed_filter'))}" autocomplete="off" />
        <div id="rpTree" class="rp-tree"></div><div id="rpTrunc" class="rp-trunc" hidden></div></aside>
      <section class="rp-main"><div id="rpFbar" class="rp-fbar"></div>
        <div id="rpNote" class="rp-note"></div><div id="rpEditor" class="rp-edbox" hidden></div></section>`;
    Ed.cm = CodeMirror(R.el('rpEditor'), { lineNumbers: true, theme: 'vscode-dark', mode: null, indentUnit: 2, tabSize: 2, indentWithTabs: false, extraKeys: { Tab: cm => cm.replaceSelection('  ', 'end') } });
    Ed.cm.on('change', () => { // refresh only the dirty markers when the state flips, not on every keystroke
      if (isDirty(Ed.cur) === Ed.shownDirty) return;
      renderFbar(); renderTree(); renderTools();
    });
    R.el('rpFilter').addEventListener('input', e => onFilter(e.target.value));
    R.el('rpNew').addEventListener('keydown', e => { if (e.key === 'Enter') { e.preventDefault(); addFile(); } });
    showNote(R.tx('rp_ed_pick'));
  }
  // reset = the committed listing may have changed (sync, commit, refresh): forget the cached folders.
  async function load(reset) {
    const r = await api('GET', '');
    if (!r.ok) {
      R.el('rpTools').innerHTML = '';
      R.el('rpBody').innerHTML = R.empty(r.data.error || 'Error');
      Ed.d = null;
      return false;
    }
    Ed.d = r.data;
    if (reset) Ed.dirs = {};
    await loadDirs();
    if (!R.el('rpTree')) mount();
    renderTools(); renderTree(); renderFbar();
    if (reset && Ed.filter.trim()) runSearch();
    return true;
  }

  /* ---- actions ---- */
  async function saveDoc(p) {
    const doc = Ed.docs[p];
    const r = await api('PUT', '/file', { path: p, content: doc.getValue() });
    if (!r.ok) { R.err(r); return false; }
    Ed.gens[p] = doc.changeGeneration(true);
    return true;
  }
  async function saveCur() {
    const p = Ed.cur;
    if (!p || !isDirty(p) || !(await saveDoc(p))) return;
    showToast(R.tx('rp_ed_saved', { path: p }), 'success', 1500);
    await load();
  }
  async function addFile() {
    const p = R.val('rpNew').trim().replace(/^\/+/, '');
    if (!p) return;
    // Never overwrite: an existing file (also an unreadable one: 413/415) is opened, only a missing one is created.
    const found = await api('GET', '/file?path=' + encodeURIComponent(p));
    if (found.status === 404) {
      const r = await api('PUT', '/file', { path: p, content: '' });
      if (!r.ok) { R.err(r); return; }
      await load();
    } else if (!found.ok && ![413, 415].includes(found.status)) { R.err(found); return; }
    R.el('rpNew').value = '';
    openFile(p);
  }
  async function deleteCur() {
    const p = Ed.cur;
    if (!(await showConfirm({ title: R.tx('rp_ed_delete'), message: R.tx('rp_ed_delete_msg', { path: p }), confirmText: R.tx('rp_ed_delete'), danger: true }))) return;
    const r = await api('DELETE', '/file?path=' + encodeURIComponent(p));
    if (!r.ok) { R.err(r); return; }
    delete Ed.docs[p];
    clearCur();
    await load();
  }
  async function discardCur() {
    const p = Ed.cur;
    if (!(await showConfirm({ title: R.tx('rp_ed_discard'), message: R.tx('rp_ed_discard_msg', { path: p }), confirmText: R.tx('rp_ed_discard'), danger: true }))) return;
    const r = await api('POST', '/discard', { path: p });
    if (!r.ok) { R.err(r); return; }
    delete Ed.docs[p];
    await load();
    if (Ed.cur === p) openFile(p); // reload the restored content
  }
  async function sync(btn) {
    const restore = SP.busy(btn, R.tx('rp_syncing'));
    try {
      const r = await api('POST', '/sync');
      if (r.ok) showToast(R.tx('rp_sync_ok', { name: Ed.name, commit: R.short(r.data.commit) }), 'success');
      else R.err(r);
    } finally {
      restore();
    }
    Object.keys(Ed.docs).filter(p => !isDirty(p)).forEach(p => { delete Ed.docs[p]; }); // clean buffers reload from disk
    await load(true);
    if (Ed.cur && !Ed.docs[Ed.cur]) openFile(Ed.cur);
  }
  async function push(btn) {
    const restore = SP.busy(btn, R.tx('rp_ed_push'));
    try {
      const r = await api('POST', '/push');
      if (r.ok) showToast(R.tx('rp_ed_pushed', { commit: R.short(r.data.commit), branch: r.data.branch }), 'success');
      else R.err(r);
    } finally {
      restore();
    }
    await load();
  }

  /* ---- Commit & Push dialog (repoDlg) ---- */
  async function openCommit(btn) {
    const restore = SP.busy(btn, R.tx('rp_ed_commit'));
    let ok = true;
    try {
      for (const p of dirtyPaths()) if (!(await saveDoc(p))) { ok = false; break; }
      if (ok) ok = await load();
    } finally {
      restore();
    }
    if (!ok || !Ed.d.changes.length) { renderTools(); return; }
    let a = {};
    try { a = JSON.parse(localStorage.getItem('pantau_git_author') || '{}') || {}; } catch (e) { /* ignore */ }
    const ch = Ed.d.changes;
    const body = `<div class="dk-form-grid">
      ${R.field(R.tx('rp_cm_message') + ' *', `<textarea id="rpCmMsg" class="form-input" rows="3" maxlength="500" style="width:100%;box-sizing:border-box;"></textarea>`, '', true)}
      ${R.field(R.tx('rp_cm_author'), R.input('rpCmName', a.name || '', 'maxlength="100" autocomplete="off"'))}
      ${R.field(R.tx('rp_cm_email'), R.input('rpCmEmail', a.email || '', 'maxlength="120" autocomplete="off"'))}
      <div class="dk-field dk-full">
        <label style="display:flex;gap:8px;align-items:center;"><input type="radio" name="rpCmBr" value="cur" checked onchange="Repos.cmBranchMode()" /> ${R.esc(R.tx('rp_cm_branch_cur', { branch: Ed.d.branch }))}</label>
        <label style="display:flex;gap:8px;align-items:center;margin-top:4px;"><input type="radio" name="rpCmBr" value="new" onchange="Repos.cmBranchMode()" /> ${R.esc(R.tx('rp_cm_branch_new'))}
          <input type="text" id="rpCmNewBranch" class="form-input" style="flex:1;" disabled placeholder="${R.esc(R.tx('rp_cm_branch_ph'))}" autocomplete="off" /></label></div>
      <div class="dk-field dk-full"><label>${R.esc(R.tx('rp_cm_files', { n: ch.length }))}</label>
        <div class="rp-changes">${ch.map(c => `<label><input type="checkbox" data-cm-path="${R.esc(c.path)}" checked /> ${mark(c.status)} <span>${R.esc(c.path)}</span></label>`).join('')}</div>
        <div class="dk-hint">${R.esc(R.tx('rp_cm_perm'))}</div></div></div>`;
    const go = `<button type="button" id="rpCmGo" class="btn btn-sm btn-primary" onclick="Repos.commitGo()">${R.esc(R.tx('rp_ed_commit'))}</button>`;
    R.dlg(R.tx('rp_cm_title') + ' — ' + Ed.name, body, R.foot(go), 680);
    R.el('rpCmMsg').focus();
  }
  R.cmBranchMode = function () {
    const nb = R.el('rpCmNewBranch');
    nb.disabled = document.querySelector('input[name="rpCmBr"]:checked').value !== 'new';
    if (!nb.disabled) nb.focus();
  };
  R.commitGo = async function () {
    const message = R.val('rpCmMsg').trim();
    if (!message) { R.dlgErr(R.tx('rp_cm_message') + ' *'); return; }
    const boxes = Array.from(document.querySelectorAll('#repoDlgBody [data-cm-path]'));
    const sel = boxes.filter(b => b.checked).map(b => b.dataset.cmPath);
    if (!sel.length) { R.dlgErr(R.tx('rp_cm_none')); return; }
    const isNew = document.querySelector('input[name="rpCmBr"]:checked').value === 'new';
    const newBranch = isNew ? R.val('rpCmNewBranch').trim() : '';
    if (isNew && !newBranch) { R.dlgErr(R.tx('rp_cm_branch_new')); return; }
    const author = { name: R.val('rpCmName').trim(), email: R.val('rpCmEmail').trim() };
    try { localStorage.setItem('pantau_git_author', JSON.stringify(author)); } catch (e) { /* ignore */ }
    const btn = R.el('rpCmGo');
    const restore = SP.busy(btn, R.tx('rp_cm_working'));
    R.dlgErr('');
    // empty paths = every change, so a partial selection is the only case that sends names
    let r;
    try {
      r = await api('POST', '/commit', { message, author_name: author.name, author_email: author.email, new_branch: newBranch, paths: sel.length === boxes.length ? [] : sel });
    } finally {
      restore();
    }
    if (!r.ok) {
      R.dlgErr((r.data.error || 'Error') + (r.status === 409 ? '\n\n' + R.tx('rp_cm_kept') : ''));
      if (r.status === 409) load();
      return;
    }
    R.dlgClose();
    showToast(R.tx('rp_ed_pushed', { commit: R.short(r.data.commit), branch: r.data.branch }), 'success');
    await load(true);
  };

  /* ---- open / close / events ---- */
  function wire() {
    if (Ed.wired) return;
    Ed.wired = true;
    const modal = R.el('repoModal');
    modal.addEventListener('click', e => {
      const el = e.target.closest('[data-act],[data-file],[data-dir]');
      if (!el) return;
      const d = el.dataset;
      if (d.file) return openFile(d.file);
      if (d.dir) {
        if (Ed.open.has(d.dir)) { Ed.open.delete(d.dir); return renderTree(); }
        Ed.open.add(d.dir);
        Ed.treeT0 = Date.now(); // the elapsed counter of the folder that is being fetched
        renderTree(); // shows "Loading…" until the folder arrives
        return loadDirs().then(renderTree);
      }
      switch (d.act) {
        case 'refresh': return load(true);
        case 'sync': return sync(el);
        case 'push': return push(el);
        case 'commit': return openCommit(el);
        case 'save': return saveCur();
        case 'delete': return deleteCur();
        case 'discard': return discardCur();
        case 'add': return addFile();
        case 'close': return R.closeEditor();
      }
    });
    modal.addEventListener('keydown', e => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') { e.preventDefault(); e.stopPropagation(); saveCur(); return; }
      if (e.key === 'Escape' && dirtyPaths().length) e.stopPropagation(); // keep unsaved edits: close with the X button
    });
  }
  R.openEditor = async function (id) {
    wire();
    const repo = R.state.list.find(x => x.id === id);
    Object.assign(Ed, { id, name: repo ? repo.name : '', d: null, docs: {}, gens: {}, cur: '', cm: null, filter: '', open: new Set(), dirs: {}, search: null });
    R.el('rpTitle').textContent = Ed.name;
    R.el('rpTools').innerHTML = '';
    R.el('rpBody').innerHTML = SP.loading(R.tx('rp_loading'));
    openModal('repoModal');
    await load();
  };
  R.closeEditor = async function () {
    if (dirtyPaths().length && !(await showConfirm({ title: R.tx('rp_ed_unsaved'), message: R.tx('rp_ed_close_msg'), confirmText: R.tx('rp_ed_close'), danger: true }))) return;
    Ed.docs = {};
    closeModal('repoModal');
    if (R.el('reposRoot')) R.refresh();
  };
  langRerenderHooks.push(() => { if (Ed.d && R.el('rpTree')) { renderTools(); renderTree(); renderFbar(); } });
})();
