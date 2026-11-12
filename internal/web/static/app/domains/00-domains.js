/* Domains: the domains served by nginx / Caddy / Traefik on one host (Host Detail tab)
 * and across the whole fleet (dialog).
 * Discovery is on demand: GET /api/domains?host_id= returns the stored result, an empty
 * scanned_at means "never scanned" and triggers one automatic POST /api/domains/scan.
 * Backed by GET /api/domains, GET /api/domains?host_id=, POST /api/domains/scan.
 * Relies on index.html globals: svgIcon, escapeHtml, t, I18N, showToast, formatRelativeTime,
 * copyTextToClipboard, openModal, closeModal, openEditor, openHostDetail, switchTab,
 * setHostActiveTab, currentHost, currentTab. */
(function () {
  'use strict';

  const D = (window.Domains = {});
  const S = { hostId: 0, hostName: '', data: null, loading: false, scanning: false, error: '', filter: '', seq: 0 };
  const A = { data: null, loading: false, error: '', filter: '', group: '', scanning: false, done: 0, total: 0, failures: [], cancel: false, token: 0, seq: 0 };

  const esc = (s) => escapeHtml(String(s === null || s === undefined ? '' : s));
  const icon = (n, cls) => svgIcon(n, cls || 'icon-sm');
  const fill = (s, vars) => Object.keys(vars).reduce((a, k) => a.split('{' + k + '}').join(vars[k]), s);

  /* ---------- i18n ---------- */
  const I = {
    en: {
      dm_loading: 'Looking for domains…',
      dm_scan_again: 'Scan again',
      dm_scanning: 'Scanning…',
      dm_scan_running: 'A scan for this host is already running',
      dm_scan_failed: 'Scan failed: {why}',
      dm_load_failed: 'Could not load domains: {why}',
      dm_all_hosts: 'All hosts',
      dm_scanned: 'Scanned {age}',
      dm_never: 'Never scanned',
      dm_filter_ph: 'Filter by domain, upstream or source',
      dm_col_domain: 'Domain',
      dm_col_proxy: 'Proxy',
      dm_col_tls: 'TLS',
      dm_col_listen: 'Listen',
      dm_col_upstreams: 'Upstreams',
      dm_col_source: 'Source',
      dm_copy: 'Copy domain',
      dm_copied: 'Copied to clipboard',
      dm_open: 'Open in editor',
      dm_remove: 'Remove config file',
      dm_remove_confirm: 'Delete {path} from this host? Every domain defined in it stops being served. nginx is tested and reloaded; if the test fails the file is restored.',
      dm_removed: 'Config removed, nginx reloaded',
      dm_remove_failed: 'Remove failed: {why}',
      dm_tls_yes: 'TLS',
      dm_tls_no: 'Plain',
      dm_no_proxy_title: 'No proxy detected on this host',
      dm_no_proxy_desc: 'Pantau looks for nginx, Caddy and Traefik on the host and inside Docker containers. Docker access (and sudo for the host configuration) may be required.',
      dm_no_sites_title: 'No domains found',
      dm_no_sites_desc: 'The proxy was detected but it serves no domain yet.',
      dm_all_title: 'Domains across hosts',
      dm_all_scanned: '{n} of {total} hosts scanned',
      dm_all_unscanned: '{n} hosts not scanned yet',
      dm_all_filter_ph: 'Filter by domain, upstream or host',
      dm_all_group: 'Group',
      dm_all_groups: 'All groups',
      dm_col_host: 'Host',
      dm_scan_all: 'Scan all hosts',
      dm_scan_all_running: 'Scanning {n} of {total}…',
      dm_scan_all_done: 'Scanned {n} hosts, {f} failed',
      dm_failed_host: '{host}: {why}',
      dm_no_sites_fleet: 'No domains found on any scanned host.'
    },
    id: {
      dm_loading: 'Mencari domain…',
      dm_scan_again: 'Pindai ulang',
      dm_scanning: 'Memindai…',
      dm_scan_running: 'Pindaian untuk host ini sudah berjalan',
      dm_scan_failed: 'Pindaian gagal: {why}',
      dm_load_failed: 'Domain tidak dapat dimuat: {why}',
      dm_all_hosts: 'Semua host',
      dm_scanned: 'Dipindai {age}',
      dm_never: 'Belum pernah dipindai',
      dm_filter_ph: 'Saring berdasarkan domain, upstream, atau sumber',
      dm_col_domain: 'Domain',
      dm_col_proxy: 'Proxy',
      dm_col_tls: 'TLS',
      dm_col_listen: 'Listen',
      dm_col_upstreams: 'Upstream',
      dm_col_source: 'Sumber',
      dm_copy: 'Salin domain',
      dm_copied: 'Disalin ke clipboard',
      dm_open: 'Buka di editor',
      dm_remove: 'Hapus berkas konfigurasi',
      dm_remove_confirm: 'Hapus {path} dari host ini? Semua domain di dalamnya berhenti dilayani. nginx diuji dan di-reload; jika pengujian gagal, berkas dikembalikan.',
      dm_removed: 'Konfigurasi dihapus, nginx di-reload',
      dm_remove_failed: 'Gagal menghapus: {why}',
      dm_tls_yes: 'TLS',
      dm_tls_no: 'Biasa',
      dm_no_proxy_title: 'Tidak ada proxy terdeteksi di host ini',
      dm_no_proxy_desc: 'Pantau mencari nginx, Caddy, dan Traefik di host dan di dalam container Docker. Akses Docker (dan sudo untuk konfigurasi host) mungkin diperlukan.',
      dm_no_sites_title: 'Tidak ada domain ditemukan',
      dm_no_sites_desc: 'Proxy terdeteksi tetapi belum melayani domain.',
      dm_all_title: 'Domain di semua host',
      dm_all_scanned: '{n} dari {total} host sudah dipindai',
      dm_all_unscanned: '{n} host belum dipindai',
      dm_all_filter_ph: 'Saring berdasarkan domain, upstream, atau host',
      dm_all_group: 'Grup',
      dm_all_groups: 'Semua grup',
      dm_col_host: 'Host',
      dm_scan_all: 'Pindai semua host',
      dm_scan_all_running: 'Memindai {n} dari {total}…',
      dm_scan_all_done: '{n} host dipindai, {f} gagal',
      dm_failed_host: '{host}: {why}',
      dm_no_sites_fleet: 'Tidak ada domain pada host yang sudah dipindai.'
    }
  };
  ['en', 'id'].forEach((l) => { I18N[l] = Object.assign(I18N[l] || {}, I[l]); });

  /* ---------- helpers ---------- */
  async function api(url, opts) {
    const res = await fetch(url, opts);
    const data = await res.json().catch(() => ({}));
    return { ok: res.ok, status: res.status, data };
  }

  const activeHostId = () => (typeof currentHost !== 'undefined' && currentHost ? currentHost.id : 0);

  // The host detail tab can be replaced while a request is in flight.
  function tabStillActive() {
    return typeof currentTab !== 'undefined' && currentTab === 'domains' && activeHostId() === S.hostId;
  }

  function ageLabel(iso) {
    if (!iso) return t('dm_never');
    const r = formatRelativeTime(iso);
    return fill(t('dm_scanned'), { age: r.relative || '-' });
  }

  function proxyBadge(p) {
    const name = p.kind + (p.container ? ': ' + p.container : '');
    const version = p.version ? ' ' + p.version : '';
    if (p.error) {
      return `<span class="badge badge-danger" title="${esc(p.error)}">${icon('triangle-alert')} ${esc(name)}</span>`;
    }
    return `<span class="badge badge-info" title="${esc(p.method || '')}">${esc(name)}${esc(version)}</span>`;
  }

  // Upstreams, or the served directory for static sites without one.
  function targetsCell(s) {
    const ups = s.upstreams || [];
    if (ups.length) return ups.map((u) => `<div>${esc(u)}</div>`).join('');
    return s.root ? `<div class="text-muted">root ${esc(s.root)}</div>` : '';
  }

  function siteMatches(s, q) {
    if (!q) return true;
    const hay = [s.domain, s.source, (s.upstreams || []).join(' '), s.root, s.proxy, s.container].join(' ').toLowerCase();
    return hay.indexOf(q) !== -1;
  }

  function domainCell(s) {
    const d = s.domain || '';
    if (s.wildcard || s.regex) {
      return `<span class="dm-domain-plain mono">${esc(d)}</span>`;
    }
    const scheme = s.tls ? 'https://' : 'http://';
    return `<a class="dm-domain" href="${esc(scheme + d)}" target="_blank" rel="noopener noreferrer" title="${esc(d)}">${esc(d)}</a>`;
  }

  function sourceCell(s) {
    const src = s.source || '';
    const m = s.where === 'host' ? /^(\/[^\s:]*):(\d+)$/.exec(src) : null;
    // The editor always works on the host currently open, so the action is only
    // offered while the rendered host is still the active one.
    const editable = m && activeHostId() === S.hostId && typeof openEditor === 'function';
    const text = `<span class="mono">${esc(src)}</span>`;
    if (!editable) return text;
    const open = `<button class="btn btn-sm btn-icon btn-ghost dm-open" data-dm-open="${esc(m[1])}" title="${esc(t('dm_open'))}">${icon('file-code')}</button>`;
    // nginx.conf holds the global config; removing it would break the proxy, so only include files qualify.
    const removable = s.proxy === 'nginx' && m[1].indexOf('/etc/nginx/') === 0 && m[1] !== '/etc/nginx/nginx.conf';
    const remove = removable ? `<button class="btn btn-sm btn-icon btn-ghost dm-remove" data-dm-remove="${esc(m[1])}" title="${esc(t('dm_remove'))}">${icon('trash-2')}</button>` : '';
    return `${text} ${open}${remove}`;
  }

  function siteRow(s) {
    return `<tr>
      <td class="dm-domain-cell"><span class="dm-domain-wrap">${domainCell(s)}<button class="btn btn-sm btn-icon btn-ghost dm-copy" data-dm-copy="${esc(s.domain || '')}" title="${esc(t('dm_copy'))}">${icon('copy')}</button></span></td>
      <td>${esc(s.proxy)}${s.container ? ` <span class="dm-container">${esc(s.container)}</span>` : ''}</td>
      <td>${s.tls ? `<span class="badge badge-success">${esc(t('dm_tls_yes'))}</span>` : `<span class="badge">${esc(t('dm_tls_no'))}</span>`}</td>
      <td class="mono">${esc((s.listen || []).join(', '))}</td>
      <td class="mono dm-upstreams">${targetsCell(s)}</td>
      <td class="dm-source">${sourceCell(s)}</td>
    </tr>`;
  }

  function warningsHtml(list) {
    if (!list || !list.length) return '';
    return `<div class="dm-warnings">${list.map((w) => `<div class="dm-warn">${icon('triangle-alert')} <span>${esc(w)}</span></div>`).join('')}</div>`;
  }

  function sitesTable(sites) {
    return `<table class="dm-table">
      <thead><tr>
        <th>${esc(t('dm_col_domain'))}</th><th>${esc(t('dm_col_proxy'))}</th><th>${esc(t('dm_col_tls'))}</th>
        <th>${esc(t('dm_col_listen'))}</th><th>${esc(t('dm_col_upstreams'))}</th><th>${esc(t('dm_col_source'))}</th>
      </tr></thead>
      <tbody>${sites.map(siteRow).join('')}</tbody>
    </table>`;
  }

  /* ---------- host tab ---------- */
  function renderSites() {
    const box = document.getElementById('dmSites');
    if (!box || !tabStillActive()) return;
    const proxies = (S.data && S.data.proxies) || [];
    const q = S.filter.trim().toLowerCase();
    const shown = (S.data.sites || []).filter((s) => siteMatches(s, q));
    if (!proxies.length) {
      box.innerHTML = `<div class="empty-state">
        <div class="empty-state-icon">${icon('globe', 'icon-xl')}</div>
        <div class="empty-state-title">${esc(t('dm_no_proxy_title'))}</div>
        <div class="empty-state-desc">${esc(t('dm_no_proxy_desc'))}</div>
      </div>`;
      return;
    }
    if (!shown.length) {
      box.innerHTML = `<div class="empty-state">
        <div class="empty-state-icon">${icon('search', 'icon-xl')}</div>
        <div class="empty-state-title">${esc(t('dm_no_sites_title'))}</div>
        <div class="empty-state-desc">${esc(t('dm_no_sites_desc'))}</div>
      </div>`;
      return;
    }
    box.innerHTML = sitesTable(shown);
  }

  function renderTab() {
    const box = document.getElementById('tabContent');
    if (!box || !tabStillActive()) return;

    if (S.loading) {
      box.innerHTML = `<div class="dm-loading"><span class="spinner">${svgIcon('loader-circle', 'icon-sm')}</span><span>${esc(t('dm_loading'))}</span></div>`;
      return;
    }
    if (S.error && !S.data) {
      box.innerHTML = `<div class="dm-err">${icon('circle-alert')} <span>${esc(S.error)}</span></div>`;
      return;
    }
    const d = S.data || { proxies: [], sites: [], warnings: [] };
    const proxies = d.proxies || [];
    box.innerHTML = `<div class="dm-head">
      <div class="dm-head-left">
        <div class="dm-badges">${proxies.map(proxyBadge).join('')}</div>
        <span class="dm-age" title="${esc(d.scanned_at || '')}">${icon('clock')} ${esc(ageLabel(d.scanned_at))}</span>
      </div>
      <div class="dm-head-right">
        <button class="btn btn-sm" data-dm-scan ${S.scanning ? 'disabled' : ''}>${icon('refresh-cw')} <span>${esc(S.scanning ? t('dm_scanning') : t('dm_scan_again'))}</span></button>
        <button class="btn btn-sm btn-ghost" data-dm-all>${icon('globe')} <span>${esc(t('dm_all_hosts'))}</span></button>
      </div>
    </div>
    ${S.error ? `<div class="dm-err dm-banner">${icon('circle-alert')} <span>${esc(S.error)}</span></div>` : ''}
    ${warningsHtml(d.warnings)}
    <div class="dm-filter"><input type="text" id="dmFilter" value="${esc(S.filter)}" placeholder="${esc(t('dm_filter_ph'))}" autocomplete="off" spellcheck="false" /></div>
    <div id="dmSites"></div>`;
    renderSites();
  }

  async function loadHost(hostId) {
    const seq = ++S.seq;
    S.loading = true;
    S.error = '';
    S.data = null;
    renderTab();
    const r = await api('/api/domains?host_id=' + encodeURIComponent(hostId));
    if (seq !== S.seq) return;
    S.loading = false;
    if (!r.ok) {
      S.error = fill(t('dm_load_failed'), { why: r.data.error || ('HTTP ' + r.status) });
      renderTab();
      return;
    }
    S.data = r.data;
    renderTab();
    // Never scanned yet: run one automatic scan, then show its result.
    if (!r.data.scanned_at) await scanHost(hostId, seq);
  }

  async function scanHost(hostId, seq) {
    S.scanning = true;
    renderTab();
    const r = await api('/api/domains/scan', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ host_id: hostId })
    });
    if (seq !== undefined && seq !== S.seq) return;
    S.scanning = false;
    if (!r.ok) {
      if (r.status === 429) {
        showToast(t('dm_scan_running'), 'warning');
      } else {
        // A failed scan says nothing about the host: drop the empty placeholder result
        // so the empty state is not mistaken for "no proxy found".
        if (S.data && !S.data.scanned_at) S.data = null;
        S.error = fill(t('dm_scan_failed'), { why: r.data.error || ('HTTP ' + r.status) });
      }
      renderTab();
      return;
    }
    S.data = r.data;
    S.error = '';
    renderTab();
  }

  async function removeConfig(path) {
    const hostId = S.hostId;
    if (!await showConfirm({ message: fill(t('dm_remove_confirm'), { path }), confirmText: t('delete', 'Delete') })) return;
    const r = await api('/api/domains/remove', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ host_id: hostId, path })
    });
    if (!r.ok) {
      showToast(fill(t('dm_remove_failed'), { why: r.data.error || ('HTTP ' + r.status) }), 'error');
      return;
    }
    showToast(t('dm_removed'), 'success');
    if (tabStillActive()) await scanHost(hostId, S.seq);
  }

  D.renderTab = function (host) {
    const hostId = host && host.id;
    if (!hostId) return;
    S.seq++; // drop any request still in flight for the previous host
    S.hostId = hostId;
    S.filter = '';
    S.data = null;
    S.error = '';
    S.loading = true;
    renderTab();
    loadHost(hostId);
  };

  /* ---------- fleet dialog ---------- */
  function allModalHtml() {
    return `
      <div class="modal-content modal-workspace" style="max-width: 1200px; display: flex; flex-direction: column;">
        <div class="modal-header">
          <div style="display: flex; align-items: center; gap: 8px;">
            ${icon('globe')}
            <h3 style="margin: 0; font-size: var(--fs-md); font-weight: 600;">${esc(t('dm_all_title'))}</h3>
          </div>
          <button class="btn btn-sm btn-icon btn-ghost" data-dm-allclose title="${esc(t('dm_all_title'))}">${icon('x')}</button>
        </div>
        <div class="modal-body" style="padding: 16px; overflow-y: auto;">
          <div class="dm-all-head">
            <div class="dm-all-counts" id="dmAllCounts"></div>
            <div class="dm-all-actions">
              <button class="btn btn-sm" data-dm-scanall>${icon('radar')} <span>${esc(t('dm_scan_all'))}</span></button>
            </div>
          </div>
          <div class="dm-all-filters">
            <input type="text" id="dmAllFilter" value="${esc(A.filter)}" placeholder="${esc(t('dm_all_filter_ph'))}" autocomplete="off" spellcheck="false" />
            <select id="dmAllGroup"></select>
          </div>
          <div id="dmAllProgress"></div>
          <div id="dmAllBody"></div>
        </div>
      </div>`;
  }

  function ensureAllModal() {
    if (document.getElementById('domainsAllModal')) return;
    const el = document.createElement('div');
    el.id = 'domainsAllModal';
    el.className = 'modal';
    el.innerHTML = allModalHtml();
    document.body.appendChild(el);
    // Clicking the backdrop closes the dialog; that also stops a running fleet scan.
    el.addEventListener('click', (e) => { if (e.target === el) A.cancel = true; });
  }

  function renderAll() {
    const body = document.getElementById('dmAllBody');
    if (!body) return;
    const counts = document.getElementById('dmAllCounts');
    const hosts = (A.data && A.data.hosts) || [];
    const scanned = hosts.filter((h) => h.scanned_at).length;
    const unscanned = hosts.length - scanned;
    if (counts) {
      counts.innerHTML = `<span class="dm-count">${icon('check')} ${esc(fill(t('dm_all_scanned'), { n: scanned, total: hosts.length }))}</span>` +
        (unscanned > 0 ? ` <span class="dm-count dm-count-warn">${icon('info')} ${esc(fill(t('dm_all_unscanned'), { n: unscanned }))}</span>` : '');
    }
    const grp = document.getElementById('dmAllGroup');
    if (grp) {
      const groups = Array.from(new Set(hosts.map((h) => h.group).filter(Boolean))).sort();
      const want = A.group && groups.includes(A.group) ? A.group : '';
      grp.innerHTML = `<option value="">${esc(t('dm_all_groups'))}</option>` +
        groups.map((g) => `<option value="${esc(g)}"${g === want ? ' selected' : ''}>${esc(g)}</option>`).join('');
    }

    if (A.loading) {
      body.innerHTML = `<div class="dm-loading"><span class="spinner">${svgIcon('loader-circle', 'icon-sm')}</span><span>${esc(t('dm_loading'))}</span></div>`;
      return;
    }
    if (A.error) {
      body.innerHTML = `<div class="dm-err">${icon('circle-alert')} <span>${esc(A.error)}</span></div>`;
      return;
    }
    const sites = (A.data && A.data.sites) || [];
    const q = A.filter.trim().toLowerCase();
    const shown = sites.filter((s) => {
      if (A.group && s.group !== A.group) return false;
      if (!q) return true;
      const hay = [s.domain, s.host_name, s.source, (s.upstreams || []).join(' '), s.root, s.proxy].join(' ').toLowerCase();
      return hay.indexOf(q) !== -1;
    });
    if (!shown.length) {
      body.innerHTML = `<div class="empty-state">
        <div class="empty-state-icon">${icon('globe', 'icon-xl')}</div>
        <div class="empty-state-title">${esc(t('dm_no_sites_fleet'))}</div>
      </div>`;
      return;
    }
    body.innerHTML = `<table class="dm-table dm-table-all">
      <thead><tr>
        <th>${esc(t('dm_col_domain'))}</th><th>${esc(t('dm_col_host'))}</th><th>${esc(t('dm_col_proxy'))}</th>
        <th>${esc(t('dm_col_tls'))}</th><th>${esc(t('dm_col_upstreams'))}</th>
      </tr></thead>
      <tbody>${shown.map((s) => `<tr>
        <td>${domainCell(s)}</td>
        <td><button class="btn btn-sm btn-ghost dm-hostlink" data-dm-host="${esc(s.host_id)}">${icon('server')} ${esc(s.host_name || s.host_id)}</button>${s.group ? ` <span class="dm-group">${esc(s.group)}</span>` : ''}</td>
        <td>${esc(s.proxy)}${s.container ? ` <span class="dm-container">${esc(s.container)}</span>` : ''}</td>
        <td>${s.tls ? `<span class="badge badge-success">${esc(t('dm_tls_yes'))}</span>` : `<span class="badge">${esc(t('dm_tls_no'))}</span>`}</td>
        <td class="mono dm-upstreams">${targetsCell(s)}</td>
      </tr>`).join('')}</tbody>
    </table>`;
  }

  function renderProgress() {
    const box = document.getElementById('dmAllProgress');
    if (!box) return;
    const parts = [];
    if (A.scanning) {
      parts.push(`<div class="dm-progress">${icon('loader-circle')} <span>${esc(fill(t('dm_scan_all_running'), { n: A.done, total: A.total }))}</span></div>`);
    } else if (A.total) {
      parts.push(`<div class="dm-progress">${icon(A.failures.length ? 'triangle-alert' : 'check')} <span>${esc(fill(t('dm_scan_all_done'), { n: A.total - A.failures.length, f: A.failures.length }))}</span></div>`);
      parts.push(A.failures.map((f) => `<div class="dm-fail">${icon('circle-x')} <span>${esc(fill(t('dm_failed_host'), { host: f.host, why: f.error }))}</span></div>`).join(''));
    }
    box.innerHTML = parts.join('');
  }

  async function loadAll() {
    A.loading = true;
    A.error = '';
    renderAll();
    const seq = ++A.seq;
    const r = await api('/api/domains');
    if (seq !== A.seq) return;
    A.loading = false;
    if (!r.ok) {
      A.error = fill(t('dm_load_failed'), { why: r.data.error || ('HTTP ' + r.status) });
    } else {
      A.data = r.data;
    }
    renderAll();
  }

  async function scanAllHosts() {
    const hosts = ((A.data && A.data.hosts) || []).slice();
    if (!hosts.length || A.scanning) return;
    A.scanning = true;
    A.cancel = false;
    const token = ++A.token;
    A.done = 0;
    A.total = hosts.length;
    A.failures = [];
    renderProgress();
    const scanOne = async (h) => {
      const r = await api('/api/domains/scan', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ host_id: h.host_id })
      });
      if (!r.ok) A.failures.push({ host: h.host_name || h.host_id, error: r.data.error || ('HTTP ' + r.status) });
      A.done++;
      renderProgress();
    };
    // shortcut: concurrency 3, raise when a fleet scan feels slow.
    const workers = Array.from({ length: Math.min(3, hosts.length) }, async () => {
      while (A.token === token && !A.cancel) {
        const h = hosts.shift();
        if (!h) return;
        await scanOne(h);
      }
    });
    await Promise.all(workers);
    if (A.token !== token) return; // a newer fleet scan or a closed dialog took over
    A.scanning = false;
    renderProgress();
    await loadAll();
  }

  D.openAll = function () {
    ensureAllModal();
    A.filter = '';
    A.group = '';
    A.failures = [];
    A.cancel = false;
    A.token++; // invalidate a still-running fleet scan from a previous dialog session
    const input = document.getElementById('dmAllFilter');
    if (input) input.value = '';
    openModal('domainsAllModal');
    loadAll();
  };

  // Closing the dialog (X button, backdrop or Esc) stops a running fleet scan.
  document.addEventListener('keydown', (e) => {
    if (e.key !== 'Escape') return;
    const m = document.getElementById('domainsAllModal');
    if (m && m.classList.contains('open')) A.cancel = true;
  });

  /* ---------- delegated events ---------- */
  document.addEventListener('click', (e) => {
    const scanBtn = e.target.closest('[data-dm-scan]');
    if (scanBtn) {
      e.preventDefault();
      scanHost(S.hostId, S.seq);
      return;
    }
    const allBtn = e.target.closest('[data-dm-all]');
    if (allBtn) {
      e.preventDefault();
      D.openAll();
      return;
    }
    const copyBtn = e.target.closest('[data-dm-copy]');
    if (copyBtn) {
      e.preventDefault();
      const v = copyBtn.getAttribute('data-dm-copy') || '';
      copyTextToClipboard(v).then(() => showToast(t('dm_copied'), 'success'));
      return;
    }
    const openBtn = e.target.closest('[data-dm-open]');
    if (openBtn) {
      e.preventDefault();
      const path = openBtn.getAttribute('data-dm-open') || '';
      if (path && typeof openEditor === 'function') openEditor(path);
      return;
    }
    const rmBtn = e.target.closest('[data-dm-remove]');
    if (rmBtn) {
      e.preventDefault();
      const path = rmBtn.getAttribute('data-dm-remove') || '';
      if (path) removeConfig(path);
      return;
    }
    const closeBtn = e.target.closest('[data-dm-allclose]');
    if (closeBtn) {
      e.preventDefault();
      A.cancel = true;
      closeModal('domainsAllModal');
      return;
    }
    const scanAllBtn = e.target.closest('[data-dm-scanall]');
    if (scanAllBtn) {
      e.preventDefault();
      scanAllHosts();
      return;
    }
    const hostBtn = e.target.closest('[data-dm-host]');
    if (hostBtn) {
      e.preventDefault();
      const id = parseInt(hostBtn.getAttribute('data-dm-host'), 10);
      if (!id) return;
      A.cancel = true;
      closeModal('domainsAllModal');
      if (typeof setHostActiveTab === 'function') setHostActiveTab(id, 'domains');
      if (typeof openHostDetail === 'function') openHostDetail(id);
    }
  });

  document.addEventListener('input', (e) => {
    const el = e.target;
    if (!el) return;
    if (el.id === 'dmFilter') {
      S.filter = el.value;
      renderSites();
      return;
    }
    if (el.id === 'dmAllFilter') {
      A.filter = el.value;
      renderAll();
    }
  });

  document.addEventListener('change', (e) => {
    const el = e.target;
    if (el && el.id === 'dmAllGroup') {
      A.group = el.value;
      renderAll();
    }
  });
})();
