/* Port forwarding to this computer (ssh -L): expose a remote host:port on a local port of the
 * machine running Pantau, so a local client (psql, DBeaver) can reach it.
 * Backed by /api/forwards (GET list, POST create, DELETE /api/forwards/{id}).
 * Relies on index.html globals: svgIcon, escapeHtml, t, showToast, showConfirm, openModal,
 * closeModal, copyTextToClipboard, allRawHosts, formatBytes, formatRelativeTime. */

(function () {
  'use strict';

  const PF = (window.PortFwd = {});
  const S = { forwards: [], selectedHost: 0, remoteHost: '127.0.0.1', remotePort: '', localPort: '', timer: null, loading: false, error: '' };



  const esc = (s) => escapeHtml(String(s === null || s === undefined ? '' : s));
  const icon = (n) => svgIcon(n, 'icon-sm');
  const fill = (s, vars) => Object.keys(vars).reduce((a, k) => a.split('{' + k + '}').join(vars[k]), s);
  const bytes = (n) => (typeof formatBytes === 'function' ? formatBytes(n || 0) : String(n || 0));

  const I = {
    en: {
      pf_title: 'Port Forward to This Computer',
      pf_intro: 'Expose a port of a remote host on this computer, like "ssh -L". The forward listens on the same address as Pantau.',
      pf_host: 'Host',
      pf_host_ph: 'Select a host',
      pf_remote_host: 'Remote host',
      pf_remote_port: 'Remote port',
      pf_remote_port_ph: 'e.g. 5432',
      pf_local_port: 'Local port',
      pf_local_port_ph: 'auto (first free port)',
      pf_start: 'Start forward',
      pf_starting: 'Starting…',
      pf_active: 'Active forwards',
      pf_none: 'No active forwards.',
      pf_conns: 'Connections',
      pf_traffic: 'Traffic',
      pf_in: 'in',
      pf_out: 'out',
      pf_created: 'Created',
      pf_stop: 'Stop',
      pf_stop_confirm: 'Stop the forward to {addr}?',
      pf_copied: 'Copied to clipboard',
      pf_started: 'Forward started: {addr} → {remote}',
      pf_stopped: 'Forward stopped',
      pf_err_disabled: 'Port forwarding is disabled on this server.',
      pf_load_failed: 'Could not load forwards',
      pf_copy: 'Copy address'
    },
    id: {
      pf_title: 'Port Forward ke Komputer Ini',
      pf_intro: 'Buka port sebuah host remote di komputer ini, seperti "ssh -L". Forward listen pada alamat yang sama dengan Pantau.',
      pf_host: 'Host',
      pf_host_ph: 'Pilih sebuah host',
      pf_remote_host: 'Host remote',
      pf_remote_port: 'Port remote',
      pf_remote_port_ph: 'mis. 5432',
      pf_local_port: 'Port lokal',
      pf_local_port_ph: 'otomatis (port kosong pertama)',
      pf_start: 'Mulai forward',
      pf_starting: 'Memulai…',
      pf_active: 'Forward aktif',
      pf_none: 'Belum ada forward aktif.',
      pf_conns: 'Koneksi',
      pf_traffic: 'Trafik',
      pf_in: 'masuk',
      pf_out: 'keluar',
      pf_created: 'Dibuat',
      pf_stop: 'Hentikan',
      pf_stop_confirm: 'Hentikan forward ke {addr}?',
      pf_copied: 'Disalin ke clipboard',
      pf_started: 'Forward dimulai: {addr} → {remote}',
      pf_stopped: 'Forward dihentikan',
      pf_err_disabled: 'Port forwarding dinonaktifkan di server ini.',
      pf_load_failed: 'Daftar forward tidak dapat dimuat',
      pf_copy: 'Salin alamat'
    }
  };
  ['en', 'id'].forEach((l) => { I18N[l] = Object.assign(I18N[l] || {}, I[l]); });

  async function api(url, opts) {
    const res = await fetch(url, opts);
    const data = await res.json().catch(() => ({}));
    return { ok: res.ok, status: res.status, data };
  }

  function hostList() {
    const raw = (typeof allRawHosts !== 'undefined' && allRawHosts) || [];
    return raw.map((h) => ({ id: h.id, name: h.name, host: h.host }));
  }

  // The dialog markup is rebuilt on every language change, so it lives in one function.
  function modalHtml() {
    return `
      <div class="modal-content modal-dialog-lg" style="max-width: 720px; display: flex; flex-direction: column; max-height: 88vh;">
        <div class="modal-header">
          <div style="display: flex; align-items: center; gap: 8px;">
            ${icon('plug')}
            <h3 style="margin: 0; font-size: var(--fs-md); font-weight: 600;">${esc(t('pf_title'))}</h3>
          </div>
          <button class="btn btn-sm btn-icon btn-ghost" onclick="closeModal('pfModal')">${icon('x')}</button>
        </div>
        <div class="modal-body" style="padding: 16px; overflow-y: auto;">
          <div class="pf-note">${esc(t('pf_intro'))}</div>
          <div class="pf-form">
            <div class="pf-field">
              <label for="pfHost">${esc(t('pf_host'))}</label>
              <select id="pfHost" class="input"></select>
            </div>
            <div class="pf-field">
              <label for="pfRemoteHost">${esc(t('pf_remote_host'))}</label>
              <input type="text" id="pfRemoteHost" value="${esc(S.remoteHost)}" autocomplete="off" spellcheck="false" />
            </div>
            <div class="pf-field">
              <label for="pfRemotePort">${esc(t('pf_remote_port'))}</label>
              <input type="number" id="pfRemotePort" min="1" max="65535" value="${esc(S.remotePort)}" placeholder="${esc(t('pf_remote_port_ph'))}" autocomplete="off" />
            </div>
            <div class="pf-field">
              <label for="pfLocalPort">${esc(t('pf_local_port'))}</label>
              <input type="number" id="pfLocalPort" min="0" max="65535" value="${esc(S.localPort)}" placeholder="${esc(t('pf_local_port_ph'))}" autocomplete="off" />
            </div>
          </div>
          <div class="pf-actions">
            <button class="btn btn-sm btn-primary" id="pfStartBtn" onclick="PortFwd.start()">${icon('play')} <span>${esc(t('pf_start'))}</span></button>
          </div>
          <div class="pf-list-head">${esc(t('pf_active'))}</div>
          <div id="pfList"></div>
        </div>
      </div>`;
  }

  function ensureModal() {
    const m = document.getElementById('pfModal');
    if (m) return;
    const el = document.createElement('div');
    el.id = 'pfModal';
    el.className = 'modal';
    el.innerHTML = modalHtml();
    document.body.appendChild(el);
  }

  // rebuildModal re-renders the dialog markup (used when the UI language changes).
  function rebuildModal() {
    const m = document.getElementById('pfModal');
    if (!m) return;
    const sel = document.getElementById('pfHost');
    const rh = document.getElementById('pfRemoteHost');
    const rp = document.getElementById('pfRemotePort');
    const lp = document.getElementById('pfLocalPort');
    if (sel && sel.value) S.selectedHost = parseInt(sel.value, 10) || 0;
    if (rh) S.remoteHost = rh.value.trim() || '127.0.0.1';
    if (rp) S.remotePort = rp.value;
    if (lp) S.localPort = lp.value;
    m.innerHTML = modalHtml();
    renderHosts();
    renderList();
  }

  function renderHosts() {
    const sel = document.getElementById('pfHost');
    if (!sel) return;
    const hosts = hostList();
    sel.innerHTML = hosts.map((h) => `<option value="${esc(h.id)}">${esc(h.name)} (${esc(h.host)})</option>`).join('');

    if (S.selectedHost) sel.value = String(S.selectedHost);
  }
  // The dialog is rebuilt when the UI language changes.
  if (typeof langRerenderHooks !== 'undefined') langRerenderHooks.push(rebuildModal);

  function fmtTime(iso) {
    if (!iso) return '-';
    if (typeof formatRelativeTime === 'function') {
      const r = formatRelativeTime(iso);
      return r.relative || r.text || iso;
    }
    return iso;
  }

  function renderList() {
    const box = document.getElementById('pfList');
    if (!box) return;
    if (S.error) {
      box.innerHTML = `<div class="pf-empty" style="color: var(--danger);">${esc(S.error)}</div>`;
      return;
    }
    if (S.loading && !S.forwards.length) {
      box.innerHTML = `<div class="pf-empty">${esc(t('loading', 'Loading...'))}</div>`;
      return;
    }
    if (!S.forwards.length) {
      box.innerHTML = `<div class="pf-empty">${esc(t('pf_none'))}</div>`;
      return;
    }
    box.innerHTML = S.forwards.map((f) => {
      const addr = `${location.hostname}:${f.local_port}`;
      return `
        <div class="pf-item">

          <div class="pf-item-main">
            <div class="pf-item-title">
              <span class="pf-addr">${esc(addr)}</span>
              <button class="btn btn-sm btn-ghost btn-icon" data-copy="${esc(addr)}" title="${esc(t('pf_copy'))}">${icon('copy')}</button>
            </div>
            <div class="pf-item-meta">
              <span>${icon('server')} ${esc(f.host_name)}</span>
              <span>${icon('plug')} ${esc(f.remote_host)}:${esc(f.remote_port)}</span>
              <span>${icon('activity')} ${esc(t('pf_conns'))}: ${esc(f.active_conns)}</span>
              <span>${icon('arrow-down')} ${esc(t('pf_traffic'))}: ${esc(bytes(f.bytes_in))} ${esc(t('pf_in'))} / ${esc(bytes(f.bytes_out))} ${esc(t('pf_out'))}</span>
              <span>${icon('clock')} ${esc(t('pf_created'))}: ${esc(fmtTime(f.created_at))}</span>
            </div>
            ${f.last_error ? `<div class="pf-item-err">${esc(f.last_error)}</div>` : ''}
          </div>
          <button class="btn btn-sm btn-danger" data-stop="${esc(f.id)}">${icon('power')} <span>${esc(t('pf_stop'))}</span></button>
        </div>`;
    }).join('');
  }

  async function refresh() {
    const res = await api('/api/forwards');
    if (res.status === 503) {
      S.error = t('pf_err_disabled');
      S.forwards = [];
      renderList();
      return;
    }
    if (!res.ok) {
      S.error = (res.data && res.data.error) || t('pf_load_failed');
      renderList();
      return;
    }
    S.error = '';
    S.forwards = Array.isArray(res.data) ? res.data : [];
    S.loading = false;
    renderList();
  }

  PF.start = async function () {
    const btn = document.getElementById('pfStartBtn');
    const hostSel = document.getElementById('pfHost');
    const remoteHost = document.getElementById('pfRemoteHost');
    const remotePort = document.getElementById('pfRemotePort');
    const localPort = document.getElementById('pfLocalPort');
    const hostID = parseInt(hostSel ? hostSel.value : '0', 10);
    if (!hostID) {
      showToast(t('pf_host_ph'), 'error');
      return;
    }
    const rp = parseInt(remotePort ? remotePort.value : '', 10);
    if (!rp || rp < 1 || rp > 65535) {
      showToast(t('pf_remote_port_ph'), 'error');
      return;
    }
    const lp = parseInt(localPort ? localPort.value : '', 10) || 0;
    S.selectedHost = hostID;
    S.remoteHost = remoteHost ? remoteHost.value.trim() : '127.0.0.1';
    S.remotePort = String(rp);
    S.localPort = localPort ? localPort.value : '';
    const restore = SP.busy(btn, t('pf_starting'));
    const body = {
      host_id: hostID,
      remote_host: S.remoteHost || '127.0.0.1',
      remote_port: rp,
      local_port: lp
    };
    let res;
    try {
      res = await api('/api/forwards', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body)
      });
    } catch (e) {
      showToast((e && e.message) || t('pf_load_failed'), 'error');
      return;
    } finally {
      restore();
    }
    if (!res.ok) {
      showToast((res.data && res.data.error) || t('pf_load_failed'), 'error');
      return;
    }
    showToast(fill(t('pf_started'), {
      addr: `${location.hostname}:${res.data.local_port}`,
      remote: `${res.data.remote_host}:${res.data.remote_port}`
    }), 'success');
    await refresh();
  };

  PF.stop = async function (id) {
    const f = S.forwards.find((x) => String(x.id) === String(id));
    const addr = f ? `${location.hostname}:${f.local_port}` : '';
    const ok = await showConfirm({
      title: t('pf_stop'),
      message: fill(t('pf_stop_confirm'), { addr }),
      confirmText: t('pf_stop'),
      danger: true
    });
    if (!ok) return;
    const res = await api('/api/forwards/' + encodeURIComponent(id), { method: 'DELETE' });
    if (!res.ok) {
      showToast((res.data && res.data.error) || t('pf_load_failed'), 'error');
      return;
    }
    showToast(t('pf_stopped'), 'success');
    await refresh();
  };

  PF.copy = function (addr) {
    if (typeof copyTextToClipboard === 'function') copyTextToClipboard(addr);
    showToast(t('pf_copied'), 'success');
  };

  PF.open = async function (hostId, remotePort) {
    if (hostId) S.selectedHost = parseInt(hostId, 10) || 0;
    if (remotePort !== undefined && remotePort !== null && String(remotePort).trim() !== '') {
      S.remotePort = String(remotePort).trim();
    }
    ensureModal();
    renderHosts();
    const rp = document.getElementById('pfRemotePort');
    if (rp) rp.value = S.remotePort;
    openModal('pfModal');
    S.loading = true;
    renderList();
    await refresh();
    clearInterval(PF.timer);

    PF.timer = setInterval(() => {
      const el = document.getElementById('pfModal');
      if (!el || !el.classList.contains('open')) {
        clearInterval(PF.timer);
        PF.timer = null;
        return;
      }
      refresh();
    }, 3000);
  };

  // Scoped to the dialog: the page reuses data-copy for other features.
  document.addEventListener('click', (e) => {
    const modal = document.getElementById('pfModal');
    if (!modal || !modal.contains(e.target)) return;
    const copyBtn = e.target.closest('[data-copy]');
    if (copyBtn) {
      e.preventDefault();
      PF.copy(copyBtn.dataset.copy);
      return;
    }
    const stopBtn = e.target.closest('[data-stop]');
    if (stopBtn) {
      e.preventDefault();
      PF.stop(stopBtn.dataset.stop);
    }
  });
})();
