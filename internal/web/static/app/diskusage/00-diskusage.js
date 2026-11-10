/* Disk usage explorer (00-diskusage.js): an on-demand, ncdu-like view of one directory level.
 * Data comes from GET /api/du?host_id=&path=&as= (one `du -x -k -d 1` per request, on demand).
 * Relies on index.html globals: svgIcon, escapeHtml, jsArg, t, I18N, formatBytes, openModal,
 * closeModal, showToast, getHostFileAccess, loadFiles, openHostDetail, switchTab. */
(function () {
  'use strict';

  const DiskUsage = (window.DiskUsage = {});
  const S = { hostId: 0, path: '/', loading: false, error: '', data: null };

  const esc = (s) => escapeHtml(String(s === null || s === undefined ? '' : s));
  const icon = (n, cls) => svgIcon(n, cls || 'icon-sm');
  const fill = (s, vars) => Object.keys(vars).reduce((a, k) => a.split('{' + k + '}').join(vars[k]), s);

  /* ---------- i18n ---------- */
  const I = {
    en: {
      du_title: 'Disk usage', du_open: 'Disk usage', du_loading: 'Scanning disk usage…',
      du_total: 'Total', du_of: '{n} folder(s)', du_empty: 'No subfolders found.',
      du_up: 'Up one level', du_open_fm: 'Open in file manager', du_refresh: 'Refresh',
      du_denied: '{n} path(s) could not be read (permission denied).',
      du_timed_out: 'The scan hit the time limit; the sizes below may be incomplete.',
      du_truncated: 'Only the {n} largest folders are shown.',
      du_failed: 'Disk usage scan failed: {why}',
      du_busy: 'A disk usage scan is already running for this host.',
      du_cols: 'Entry', du_size: 'Size', du_share: 'Share',
      du_close: 'Close'
    },
    id: {
      du_title: 'Penggunaan disk', du_open: 'Penggunaan disk', du_loading: 'Memindai penggunaan disk…',
      du_total: 'Total', du_of: '{n} folder', du_empty: 'Tidak ada subfolder.',
      du_up: 'Naik satu level', du_open_fm: 'Buka di file manager', du_refresh: 'Muat ulang',
      du_denied: '{n} path tidak dapat dibaca (permission denied).',
      du_timed_out: 'Pindai melewati batas waktu; ukuran di bawah mungkin tidak lengkap.',
      du_truncated: 'Hanya {n} folder terbesar yang ditampilkan.',
      du_failed: 'Pindai penggunaan disk gagal: {why}',
      du_busy: 'Pindai penggunaan disk sudah berjalan di host ini.',
      du_cols: 'Entri', du_size: 'Ukuran', du_share: 'Porsi',
      du_close: 'Tutup'
    }
  };
  ['en', 'id'].forEach((l) => { I18N[l] = Object.assign(I18N[l] || {}, I[l]); });

  /* ---------- toolbar button ---------- */
  DiskUsage.toolbarButton = function (hostId, path) {
    return `<button class="btn btn-sm" data-du-open="${esc(hostId)}" data-du-path="${esc(path)}" title="${esc(t('du_open'))}">` +
      `${icon('hard-drive')}<span>${esc(t('du_open'))}</span></button>`;
  };

  /* ---------- modal ---------- */
  function ensureModal() {
    if (document.getElementById('diskUsageModal')) return;
    const m = document.createElement('div');
    m.id = 'diskUsageModal';
    m.className = 'modal';
    m.innerHTML = `
      <div class="modal-content modal-dialog-lg">
        <div class="modal-header">
          <div class="sd-title">
            ${icon('hard-drive', 'icon-sm text-accent')}
            <h3>${esc(t('du_title'))}</h3>
            <span id="duHostBadge" class="badge badge-info" style="text-transform: none;"></span>
          </div>
          <button class="btn btn-icon btn-sm btn-ghost" data-du-close title="${esc(t('du_close'))}">${icon('x')}</button>
        </div>
        <div class="modal-body" id="duBody"></div>
      </div>`;
    document.body.appendChild(m);
    // Delegated listeners keep remote paths out of inline handlers.
    m.addEventListener('click', (ev) => {
      // The file-manager button lives inside the drill row, so it must be matched first.
      const fm = ev.target.closest('[data-du-fm]');
      if (fm) { openInFileManager(fm.getAttribute('data-du-fm')); return; }
      if (ev.target.closest('[data-du-close]')) { closeModal('diskUsageModal'); return; }
      const drill = ev.target.closest('[data-du-drill]');
      if (drill) { DiskUsage.open(S.hostId, drill.getAttribute('data-du-drill')); return; }
      if (ev.target.closest('[data-du-up]')) { DiskUsage.open(S.hostId, parentPath(S.path)); return; }
      if (ev.target.closest('[data-du-refresh]')) { DiskUsage.open(S.hostId, S.path); }
    });
  }

  function parentPath(p) {
    if (!p || p === '/') return '/';
    const i = p.lastIndexOf('/');
    return i <= 0 ? '/' : p.slice(0, i);
  }

  function openInFileManager(path) {
    const hostId = S.hostId;
    closeModal('diskUsageModal');
    if (typeof currentHost !== 'undefined' && currentHost && currentHost.id === hostId) {
      if (typeof loadFiles === 'function') loadFiles(path);
      return;
    }
    if (typeof openHostDetail !== 'function') return;
    openHostDetail(hostId).then(() => {
      if (typeof setHostFilesPath === 'function') setHostFilesPath(hostId, path);
      if (typeof currentHost !== 'undefined' && currentHost) currentHost._lastPath = path;
      if (typeof switchTab === 'function') switchTab('files');
    });
  }

  /* ---------- rendering ---------- */
  function loadingHtml() {
    return `<div class="du-loading"><span class="spinner">${icon('loader-circle', 'icon-sm')}</span><span>${esc(t('du_loading'))}</span></div>`;
  }

  function warningsHtml(d) {
    const out = [];
    if (d.denied > 0) out.push(`<div class="du-warn">${icon('triangle-alert')} ${esc(fill(t('du_denied'), { n: d.denied }))}</div>`);
    if (d.timed_out) out.push(`<div class="du-warn">${icon('triangle-alert')} ${esc(t('du_timed_out'))}</div>`);
    if (d.truncated) out.push(`<div class="du-warn">${icon('triangle-alert')} ${esc(fill(t('du_truncated'), { n: (d.entries || []).length }))}</div>`);
    return out.join('');
  }

  function rowHtml(e, total) {
    const pct = total > 0 ? (e.bytes / total) * 100 : 0;
    const w = Math.max(pct, 0).toFixed(2);
    return `<div class="du-row" data-du-drill="${esc(e.path)}" title="${esc(e.path)}">
      <div class="du-row-main">
        <div class="du-row-name">${icon('folder')} <span>${esc(e.name)}</span></div>
        <div class="du-row-bar"><div class="du-row-fill" style="width:${w}%"></div></div>
      </div>
      <div class="du-row-size">${esc(formatBytes(e.bytes))}</div>
      <div class="du-row-pct">${pct.toFixed(1)}%</div>
      <button class="btn btn-sm btn-icon btn-ghost du-row-open" data-du-fm="${esc(e.path)}" title="${esc(t('du_open_fm'))}">${icon('external-link')}</button>
    </div>`;
  }

  function render() {
    const body = document.getElementById('duBody');
    if (!body) return;
    const badge = document.getElementById('duHostBadge');
    if (badge) {
      const h = (typeof allRawHosts !== 'undefined' && Array.isArray(allRawHosts)) ? allRawHosts.find(x => x.id === S.hostId) : null;
      badge.textContent = h ? h.name : (S.hostId ? String(S.hostId) : '');
    }
    if (S.loading) { body.innerHTML = loadingHtml(); return; }
    if (S.error) {
      body.innerHTML = `<div class="du-err">${icon('circle-alert')} ${esc(S.error)}</div>`;
      return;
    }
    const d = S.data;
    if (!d) { body.innerHTML = loadingHtml(); return; }
    const total = d.total_bytes || 0;
    const rows = (d.entries || []).map((e) => rowHtml(e, total)).join('');
    body.innerHTML = `
      <div class="du-head">
        <div class="du-crumb">
          <button class="btn btn-sm btn-icon btn-ghost" data-du-up title="${esc(t('du_up'))}" ${S.path === '/' ? 'disabled' : ''}>${icon('corner-up-left')}</button>
          <span class="mono du-path" title="${esc(S.path)}">${esc(S.path)}</span>
        </div>
        <div class="du-head-right">
          <span class="du-total">${esc(t('du_total'))}: <strong>${esc(formatBytes(total))}</strong></span>
          <button class="btn btn-sm" data-du-fm="${esc(S.path)}" title="${esc(t('du_open_fm'))}">${icon('external-link')} <span>${esc(t('du_open_fm'))}</span></button>
          <button class="btn btn-sm btn-icon btn-ghost" data-du-refresh title="${esc(t('du_refresh'))}">${icon('refresh-cw')}</button>
        </div>
      </div>
      ${warningsHtml(d)}
      <div class="du-cols"><span>${esc(t('du_cols'))}</span><span>${esc(t('du_size'))}</span><span>${esc(t('du_share'))}</span><span></span></div>
      <div class="du-rows">${rows || `<div class="du-empty">${esc(t('du_empty'))}</div>`}</div>
      <div class="du-foot">${esc(fill(t('du_of'), { n: (d.entries || []).length }))} · ${d.duration_ms || 0} ms</div>`;
  }

  /* ---------- api ---------- */
  async function load(hostId, path) {
    S.hostId = hostId;
    S.path = path || '/';
    S.loading = true;
    S.error = '';
    S.data = null;
    render();
    const q = new URLSearchParams({ host_id: String(hostId), path: S.path });
    const as = (typeof getHostFileAccess === 'function') ? getHostFileAccess(hostId) : '';
    if (as) q.set('as', as);
    try {
      const res = await fetch('/api/du?' + q.toString());
      const data = await res.json().catch(() => ({}));
      if (!res.ok) {
        S.error = res.status === 429 ? t('du_busy') : fill(t('du_failed'), { why: data.error || ('HTTP ' + res.status) });
        S.loading = false;
        render();
        return;
      }
      S.data = data;
      S.path = data.path || S.path;
    } catch (e) {
      S.error = fill(t('du_failed'), { why: e.message });
    }
    S.loading = false;
    render();
  }

  DiskUsage.open = function (hostId, path) {
    if (!hostId) return;
    ensureModal();
    openModal('diskUsageModal');
    load(hostId, path || '/');
  };

  // The toolbar button lives in the file manager, outside the modal: delegate on the document.
  document.addEventListener('click', (ev) => {
    const btn = ev.target.closest('[data-du-open]');
    if (!btn) return;
    ev.preventDefault();
    DiskUsage.open(Number(btn.getAttribute('data-du-open')), btn.getAttribute('data-du-path') || '/');
  });
})();
