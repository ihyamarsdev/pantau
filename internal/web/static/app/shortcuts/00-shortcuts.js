/* Shortcuts: a start page of administrator-defined links to external web addresses, grouped,
 * in the style of gethomepage/homepage (side navigation > Shortcuts, #sectionShortcuts).
 * Backed by GET /api/shortcuts and PUT /api/shortcuts (every change sends the whole list).
 * The tiles reuse the .dm-tile styles of domains.css and Domains.tileIcon (favicon or first letter).
 * Relies on index.html globals: svgIcon, escapeHtml, t, I18N, showToast, showConfirm, openModal,
 * closeModal, langRerenderHooks. */
(function () {
  'use strict';

  const SC = (window.Shortcuts = {});
  // editing is the index in list of the shortcut open in the dialog, -1 for a new one.
  const S = { list: [], loaded: false, error: '', editing: -1 };

  const esc = (s) => escapeHtml(String(s === null || s === undefined ? '' : s));
  const icon = (n, cls) => svgIcon(n, cls || 'icon-sm');
  const fill = (s, vars) => Object.keys(vars).reduce((a, k) => a.split('{' + k + '}').join(vars[k]), s);
  const val = (id) => document.getElementById(id).value.trim();

  const I = {
    en: {
      sc_title: 'Shortcuts',
      sc_desc: 'Your own links to external web addresses, one click away.',
      sc_add: 'Add shortcut',
      sc_edit: 'Edit shortcut',
      sc_delete: 'Delete shortcut',
      sc_name: 'Name',
      sc_url: 'Address',
      sc_group: 'Group (optional)',
      sc_empty_title: 'No shortcuts yet',
      sc_empty_desc: 'Add the web addresses you open often: dashboards, admin panels, documentation.',
      sc_delete_confirm: 'Delete the shortcut "{name}"?',
      sc_load_failed: 'Could not load shortcuts: {why}',
      sc_save_failed: 'Could not save: {why}'
    },
    id: {
      sc_title: 'Pintasan',
      sc_desc: 'Tautan Anda sendiri ke alamat web eksternal, cukup satu klik.',
      sc_add: 'Tambah pintasan',
      sc_edit: 'Ubah pintasan',
      sc_delete: 'Hapus pintasan',
      sc_name: 'Nama',
      sc_url: 'Alamat',
      sc_group: 'Grup (opsional)',
      sc_empty_title: 'Belum ada pintasan',
      sc_empty_desc: 'Tambahkan alamat web yang sering Anda buka: dashboard, panel admin, dokumentasi.',
      sc_delete_confirm: 'Hapus pintasan "{name}"?',
      sc_load_failed: 'Pintasan tidak dapat dimuat: {why}',
      sc_save_failed: 'Gagal menyimpan: {why}'
    }
  };
  ['en', 'id'].forEach((l) => { I18N[l] = Object.assign(I18N[l] || {}, I[l]); });

  async function api(url, opts) {
    const res = await fetch(url, opts);
    const data = await res.json().catch(() => ({}));
    return { ok: res.ok, status: res.status, data };
  }

  function tile(x, i) {
    let host = x.url;
    try { host = new URL(x.url).host; } catch (e) { /* keep the raw address */ }
    return `<div class="dm-tile">
      <a class="dm-tile-link" href="${esc(x.url)}" target="_blank" rel="noopener noreferrer" title="${esc(x.url)}">
        ${Domains.tileIcon(x.url, x.name)}
        <span class="dm-tile-text"><span class="dm-tile-name">${esc(x.name)}</span><span class="dm-tile-host">${esc(host)}</span></span>
      </a>
      <button class="btn btn-sm btn-icon btn-ghost" data-sc-edit="${i}" title="${esc(t('sc_edit'))}" aria-label="${esc(t('sc_edit') + ': ' + x.name)}">${icon('pencil')}</button>
      <button class="btn btn-sm btn-icon btn-ghost" data-sc-del="${i}" title="${esc(t('sc_delete'))}" aria-label="${esc(t('sc_delete') + ': ' + x.name)}">${icon('trash-2')}</button>
    </div>`;
  }

  function render() {
    const box = document.getElementById('sectionShortcuts');
    if (!box) return;
    const groups = new Map();
    S.list.forEach((x, i) => {
      if (!groups.has(x.group)) groups.set(x.group, []);
      groups.get(x.group).push(tile(x, i));
    });
    let body;
    if (S.error) {
      body = `<div class="dm-err">${icon('circle-alert')} <span>${esc(S.error)}</span></div>`;
    } else if (!S.list.length) {
      body = `<div class="empty-state">
        <div class="empty-state-icon">${icon('link', 'icon-xl')}</div>
        <div class="empty-state-title">${esc(t('sc_empty_title'))}</div>
        <div class="empty-state-desc">${esc(t('sc_empty_desc'))}</div>
      </div>`;
    } else {
      // Shortcuts without a group come first (the empty name sorts before every other).
      body = Array.from(groups.keys()).sort((a, b) => a.localeCompare(b)).map((g) =>
        `${g ? `<h3 style="margin:0 0 8px 0;font-size:var(--fs-sm);font-weight:600;color:var(--text-muted);">${esc(g)}</h3>` : ''}<div class="dm-tiles">${groups.get(g).join('')}</div>`).join('');
    }
    box.innerHTML = `<div class="section-header" style="margin-bottom:20px;">
      <div style="display:flex;align-items:center;justify-content:space-between;flex-wrap:wrap;gap:12px;">
        <div>
          <h2 style="margin:0 0 4px 0;font-size:var(--fs-xl);font-weight:600;">${esc(t('sc_title'))}</h2>
          <p style="margin:0;color:var(--text-muted);font-size:var(--fs-sm);">${esc(t('sc_desc'))}</p>
        </div>
        <button type="button" class="btn btn-sm btn-primary" data-sc-add style="height:34px;display:inline-flex;align-items:center;gap:6px;">${icon('plus')} <span>${esc(t('sc_add'))}</span></button>
      </div>
    </div>${body}`;
  }

  SC.refresh = async function () {
    const r = await api('/api/shortcuts');
    S.loaded = true;
    S.error = r.ok ? '' : fill(t('sc_load_failed'), { why: r.data.error || ('HTTP ' + r.status) });
    if (r.ok) S.list = r.data.shortcuts || [];
    render();
  };
  if (typeof langRerenderHooks !== 'undefined') langRerenderHooks.push(() => { if (S.loaded) render(); });

  async function save(list) {
    const r = await api('/api/shortcuts', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ shortcuts: list })
    });
    if (!r.ok) {
      showToast(fill(t('sc_save_failed'), { why: r.data.error || ('HTTP ' + r.status) }), 'error');
      return false;
    }
    S.list = r.data.shortcuts || [];
    render();
    return true;
  }

  // The dialog is rebuilt on every open, so it always shows the current language and groups.
  function openDialog(i) {
    S.editing = i;
    const x = S.list[i] || { name: '', url: '', group: '' };
    let el = document.getElementById('scModal');
    if (!el) {
      el = document.createElement('div');
      el.id = 'scModal';
      el.className = 'modal';
      document.body.appendChild(el);
    }
    const groups = Array.from(new Set(S.list.map((s) => s.group).filter(Boolean))).sort();
    el.innerHTML = `<div class="modal-content modal-dialog" style="max-width: 480px;">
      <div class="modal-header">
        <div style="display: flex; align-items: center; gap: 8px;">${icon('link')}<h3>${esc(t(i >= 0 ? 'sc_edit' : 'sc_add'))}</h3></div>
        <button type="button" class="btn btn-sm btn-icon" onclick="closeModal('scModal')">${icon('x')}</button>
      </div>
      <form id="scForm" style="display: contents;">
        <div class="modal-body">
          <div class="form-group"><label for="scName">${esc(t('sc_name'))}</label><input type="text" id="scName" maxlength="64" required value="${esc(x.name)}" placeholder="Grafana" autocomplete="off" /></div>
          <div class="form-group"><label for="scUrl">${esc(t('sc_url'))}</label><input type="text" id="scUrl" maxlength="2048" required value="${esc(x.url)}" placeholder="https://grafana.example.com" autocomplete="off" spellcheck="false" /></div>
          <div class="form-group"><label for="scGroup">${esc(t('sc_group'))}</label><input type="text" id="scGroup" maxlength="64" value="${esc(x.group)}" list="scGroups" autocomplete="off" /><datalist id="scGroups">${groups.map((g) => `<option value="${esc(g)}"></option>`).join('')}</datalist></div>
        </div>
        <div class="modal-footer">
          <button type="button" class="btn btn-sm" onclick="closeModal('scModal')">${esc(t('cancel', 'Cancel'))}</button>
          <button type="submit" class="btn btn-primary btn-sm" id="scSave">${esc(t('save', 'Save'))}</button>
        </div>
      </form>
    </div>`;
    openModal('scModal');
    document.getElementById('scName').focus();
  }

  document.addEventListener('submit', async (e) => {
    if (e.target.id !== 'scForm') return;
    e.preventDefault();
    let url = val('scUrl');
    if (url.indexOf('://') === -1) url = 'https://' + url; // a bare domain means https
    const list = S.list.slice();
    const item = { name: val('scName'), url, group: val('scGroup') };
    if (S.editing >= 0) list[S.editing] = item; else list.push(item);
    if (await save(list)) closeModal('scModal');
  });

  document.addEventListener('click', async (e) => {
    if (e.target.closest('[data-sc-add]')) {
      openDialog(-1);
      return;
    }
    const editBtn = e.target.closest('[data-sc-edit]');
    if (editBtn) {
      openDialog(parseInt(editBtn.getAttribute('data-sc-edit'), 10));
      return;
    }
    const delBtn = e.target.closest('[data-sc-del]');
    if (delBtn) {
      const i = parseInt(delBtn.getAttribute('data-sc-del'), 10);
      const x = S.list[i];
      if (!x || !await showConfirm({ message: fill(t('sc_delete_confirm'), { name: x.name }), confirmText: t('delete', 'Delete') })) return;
      save(S.list.filter((_, j) => j !== i));
    }
  });
})();
