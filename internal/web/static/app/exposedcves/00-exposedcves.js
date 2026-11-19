/* CVE Exposure Radar — Security workspace (ADR 0088, 0039).
 * Reads GET /api/hosts/{id}/exposure, triggers POST .../scan and rapid mitigation actions.
 * Designed for large result sets: compact rows, faceted filters, sorting and pagination.
 * Relies on index.html globals: svgIcon, escapeHtml, t, I18N, showConfirm, showToast. */
(function () {
  'use strict';

  const EC = (window.EC = {});
  const PAGE_SIZES = [25, 50, 100, 0]; // 0 = show all

  let state = {
    hostID: null, data: null,
    query: '', severity: '', source: '', kevOnly: false, exposedOnly: false,
    sort: 'severity', page: 1, pageSize: 25, expanded: {}, view: []
  };

  const esc = (s) => escapeHtml(String(s === null || s === undefined ? '' : s));
  const icon = (n) => (typeof svgIcon === 'function' ? svgIcon(n, 'icon-sm') : '');
  const fill = (s, vars) => Object.keys(vars).reduce((a, k) => a.split('{' + k + '}').join(vars[k]), s);

  const I = {
    en: {
      ec_title: 'Exposed CVE Radar',
      ec_sub: 'Runtime, lockfile, kernel and Nginx exposure with reachability priority',
      ec_scan: 'Scan exposure',
      ec_scanning: 'Scanning…',
      ec_empty: 'No exposure scan yet. Run a scan to assess this host.',
      ec_disabled: 'OSV lookup is disabled. Enable it in Settings → General to assess vulnerabilities.',
      ec_critical: 'Critical exposed',
      ec_findings: 'Findings',
      ec_kernel: 'Reboot required',
      ec_kev: 'Exploited (KEV)',
      ec_epss: 'EPSS {n}%',
      ec_public: 'Publicly reachable',
      ec_cf: 'Cloudflare-proxied',
      ec_no_findings: 'No exposure findings for this host.',
      ec_filter: 'Filter findings…',
      ec_severity: 'Severity',
      ec_source: 'Source',
      ec_sort: 'Sort',
      ec_sort_severity: 'Severity first',
      ec_sort_epss: 'Highest EPSS',
      ec_sort_component: 'Component A–Z',
      ec_kev_only: 'KEV only',
      ec_exposed_only: 'Exposed only',
      ec_all: 'All',
      ec_showing: 'Showing {from}–{to} of {total} findings',
      ec_page: 'Page {n} of {pages}',
      ec_prev: 'Previous',
      ec_next: 'Next',
      ec_per_page: 'Rows per page',
      ec_no_match: 'No findings match the current filters.',
      ec_clear_filters: 'Clear filters',
      ec_details: 'Details',
      ec_fix_upgrade: 'Upgrade package',
      ec_fix_ansible: 'Emergency patch',
      ec_fix_firewall: 'Restrict port',
      ec_fix_cf: 'Edge lockdown',
      ec_confirm_upgrade: 'Upgrade the package {name} on this host now? This may require a reboot and cannot be rolled back automatically.',
      ec_confirm_firewall: 'Block port {port} on this host with the host firewall? Make sure this is not the SSH port you use to connect.',
      ec_confirm_cf: 'Apply an emergency Cloudflare lockdown (Under Attack mode) to zone {zone}? This affects live traffic.',
      ec_confirm_ansible: 'Queue an emergency Ansible patch on this host?',
      ec_done: 'Mitigation request sent.',
      ec_failed: 'Mitigation failed: {err}',
      ec_notes: 'Coverage notes'
    },
    id: {
      ec_title: 'Radar CVE Terekspos',
      ec_sub: 'Eksposur runtime, lockfile, kernel dan Nginx dengan prioritas keterjangkauan',
      ec_scan: 'Pindai eksposur',
      ec_scanning: 'Memindai…',
      ec_empty: 'Belum ada pemindaian eksposur. Jalankan pindai untuk menilai host ini.',
      ec_disabled: 'Pencarian OSV nonaktif. Aktifkan di Pengaturan → Umum untuk menilai kerentanan.',
      ec_critical: 'Kritis terekspos',
      ec_findings: 'Temuan',
      ec_kernel: 'Perlu reboot',
      ec_kev: 'Dieksploitasi (KEV)',
      ec_epss: 'EPSS {n}%',
      ec_public: 'Dapat dijangkau publik',
      ec_cf: 'Lewat Cloudflare',
      ec_no_findings: 'Tidak ada temuan eksposur untuk host ini.',
      ec_filter: 'Saring temuan…',
      ec_severity: 'Keparahan',
      ec_source: 'Sumber',
      ec_sort: 'Urutkan',
      ec_sort_severity: 'Keparahan dulu',
      ec_sort_epss: 'EPSS tertinggi',
      ec_sort_component: 'Komponen A–Z',
      ec_kev_only: 'KEV saja',
      ec_exposed_only: 'Terekspos saja',
      ec_all: 'Semua',
      ec_showing: 'Menampilkan {from}–{to} dari {total} temuan',
      ec_page: 'Halaman {n} dari {pages}',
      ec_prev: 'Sebelumnya',
      ec_next: 'Berikutnya',
      ec_per_page: 'Baris per halaman',
      ec_no_match: 'Tidak ada temuan yang cocok dengan filter saat ini.',
      ec_clear_filters: 'Hapus filter',
      ec_details: 'Rincian',
      ec_fix_upgrade: 'Upgrade paket',
      ec_fix_ansible: 'Patch darurat',
      ec_fix_firewall: 'Batasi port',
      ec_fix_cf: 'Kunci edge',
      ec_confirm_upgrade: 'Upgrade paket {name} di host ini sekarang? Ini mungkin memerlukan reboot dan tidak dapat dibatalkan otomatis.',
      ec_confirm_firewall: 'Blokir port {port} di host ini dengan firewall host? Pastikan ini bukan port SSH yang Anda pakai.',
      ec_confirm_cf: 'Terapkan penguncian Cloudflare darurat (mode Under Attack) pada zone {zone}? Ini memengaruhi trafik langsung.',
      ec_confirm_ansible: 'Antrikan patch Ansible darurat di host ini?',
      ec_done: 'Permintaan mitigasi dikirim.',
      ec_failed: 'Mitigasi gagal: {err}',
      ec_notes: 'Catatan cakupan'
    }
  };
  ['en', 'id'].forEach((l) => { I18N[l] = Object.assign(I18N[l] || {}, I[l]); });

  const SEV_RANK = { 'CRITICAL_EXPOSED': 0, 'critical': 1, 'high': 2, 'medium': 3, 'low': 4 };
  const sevRank = (s) => (SEV_RANK[s] != null ? SEV_RANK[s] : 5);
  const sevClass = (s) => (s === 'CRITICAL_EXPOSED' ? 'critical-exposed' : (['critical', 'high', 'medium', 'low'].indexOf(s) >= 0 ? s : 'unknown'));

  // ---------------------------------------------------------------------------
  // Markup (static shell — built once per Security-tab render)

  EC.html = function () {
    const sevOpts = ['', 'CRITICAL_EXPOSED', 'critical', 'high', 'medium', 'low', 'unknown']
      .map((v) => `<option value="${v}">${esc(v || t('ec_all'))}</option>`).join('');
    return `<div class="card" id="secExposureCard" style="padding:16px;margin-top:16px;">
      <div class="ec-head">
        <div class="ec-title">${icon('radar')}<span>${esc(t('ec_title'))}</span></div>
        <div class="ec-sub">${esc(t('ec_sub'))}</div>
        <div class="ec-actions">
          <button class="btn btn-sm" id="ecScanBtn" onclick="EC.scan()">${icon('refresh-cw')} ${esc(t('ec_scan'))}</button>
        </div>
      </div>
      <div id="ecBody">
        <div class="ec-toolbar" id="ecToolbar">
          <div class="ec-search">
            ${icon('search')}
            <input id="ecFilter" class="ec-filter" placeholder="${esc(t('ec_filter'))}" oninput="EC.filterRows(this.value)" />
          </div>
          <label class="ec-field"><span>${esc(t('ec_severity'))}</span>
            <select id="ecSev" class="ec-select" onchange="EC.setFacet('severity',this.value)">${sevOpts}</select></label>
          <label class="ec-field"><span>${esc(t('ec_source'))}</span>
            <select id="ecSource" class="ec-select" onchange="EC.setFacet('source',this.value)"><option value="">${esc(t('ec_all'))}</option></select></label>
          <label class="ec-field"><span>${esc(t('ec_sort'))}</span>
            <select id="ecSort" class="ec-select" onchange="EC.setFacet('sort',this.value)">
              <option value="severity">${esc(t('ec_sort_severity'))}</option>
              <option value="epss">${esc(t('ec_sort_epss'))}</option>
              <option value="component">${esc(t('ec_sort_component'))}</option>
            </select></label>
          <label class="ec-toggle"><input type="checkbox" id="ecKev" onchange="EC.setFacet('kevOnly',this.checked)" /> ${esc(t('ec_kev_only'))}</label>
          <label class="ec-toggle"><input type="checkbox" id="ecExposed" onchange="EC.setFacet('exposedOnly',this.checked)" /> ${esc(t('ec_exposed_only'))}</label>
        </div>
        <div id="ecSummary" class="ec-summary-bar"></div>
        <div id="ecList"></div>
        <div id="ecPager" class="ec-pager"></div>
      </div>
    </div>`;
  };

  EC.render = function (hostID) {
    state.hostID = hostID;
    EC.resetFilters();
    const el = document.getElementById('ecList');
    if (!el) return;
    if (!hostID) { el.innerHTML = `<div class="ec-empty">${esc(t('ec_empty'))}</div>`; return; }
    el.innerHTML = `<div class="ec-empty">…</div>`;
    EC.load();
  };

  EC.resetFilters = function () {
    state.query = ''; state.severity = ''; state.source = ''; state.kevOnly = false;
    state.exposedOnly = false; state.sort = 'severity'; state.page = 1; state.expanded = {};
  };

  EC.load = async function () {
    const el = document.getElementById('ecList');
    if (!el || !state.hostID) return;
    try {
      const r = await fetch('/api/hosts/' + state.hostID + '/exposure');
      const j = await r.json();
      state.data = j.scanned ? j.scan : null;
      EC.paint();
    } catch (e) {
      el.innerHTML = `<div class="ec-empty">${esc(String(e))}</div>`;
    }
  };

  EC.scan = async function () {
    const btn = document.getElementById('ecScanBtn');
    const label = () => icon('refresh-cw') + ' ' + esc(t('ec_scan'));
    const busy = () => { if (btn) { btn.disabled = true; btn.innerHTML = icon('loader-circle') + ' ' + esc(t('ec_scanning')); } };
    const done = () => { if (btn) { btn.disabled = false; btn.innerHTML = label(); } };
    busy();
    try {
      const r = await fetch('/api/hosts/' + state.hostID + '/exposure/scan', { method: 'POST' });
      const j = await r.json();
      state.data = j.scan || null;
      EC.paint();
    } catch (e) {
      const el = document.getElementById('ecList');
      if (el) el.innerHTML = `<div class="ec-empty">${esc(String(e))}</div>`;
    } finally {
      done();
    }
  };

  // ---------------------------------------------------------------------------
  // Filtering / sorting / pagination

  EC.filtered = function () {
    const all = (state.data && state.data.findings) || [];
    const q = state.query.trim().toLowerCase();
    const out = all.filter((f) => {
      if (state.severity && f.severity !== state.severity) return false;
      if (state.source && f.source !== state.source) return false;
      if (state.kevOnly && !f.kev) return false;
      if (state.exposedOnly && !(f.reachability && (f.reachability.public_bind || f.reachability.cloudflare_proxied))) return false;
      if (q) {
        const hay = [f.component, f.title, f.summary, f.location, (f.cves || []).join(' ')].join(' ').toLowerCase();
        if (hay.indexOf(q) < 0) return false;
      }
      return true;
    });
    const cmp = {
      severity: (a, b) => sevRank(a.severity) - sevRank(b.severity) || (b.epss || 0) - (a.epss || 0) || String(a.component).localeCompare(String(b.component)),
      epss: (a, b) => (b.epss || 0) - (a.epss || 0) || sevRank(a.severity) - sevRank(b.severity),
      component: (a, b) => String(a.component).localeCompare(String(b.component)) || sevRank(a.severity) - sevRank(b.severity)
    }[state.sort] || null;
    if (cmp) out.sort(cmp);
    return out;
  };

  EC.paint = function () {
    const list = document.getElementById('ecList');
    if (!list) return;
    const d = state.data;
    if (!d) { list.innerHTML = `<div class="ec-empty">${esc(t('ec_empty'))}</div>`; return; }
    if (d.enabled === false) { list.innerHTML = `<div class="ec-empty">${esc(t('ec_disabled'))}</div>`; return; }

    EC.populateSources(d);
    const findings = EC.filtered();
    const totalPages = state.pageSize ? Math.max(1, Math.ceil(findings.length / state.pageSize)) : 1;
    if (state.page > totalPages) state.page = totalPages;
    const from = state.pageSize ? (state.page - 1) * state.pageSize : 0;
    const to = state.pageSize ? Math.min(from + state.pageSize, findings.length) : findings.length;
    state.view = state.pageSize ? findings.slice(from, to) : findings;

    let strip = `<div class="ec-strip">
      <div class="ec-stat critical"><div class="ec-stat-num">${d.critical_exposed || 0}</div><div class="ec-stat-label">${esc(t('ec_critical'))}</div></div>
      <div class="ec-stat"><div class="ec-stat-num">${(d.findings || []).length}</div><div class="ec-stat-label">${esc(t('ec_findings'))}</div></div>
      <div class="ec-stat kernel"><div class="ec-stat-num">${d.kernel && d.kernel.reboot_required ? '1' : '0'}</div><div class="ec-stat-label">${esc(t('ec_kernel'))}</div></div>
    </div>`;
    const summary = document.getElementById('ecSummary');
    if (summary) {
      summary.innerHTML = findings.length
        ? `<span>${esc(fill(t('ec_showing'), { from: from + 1, to, total: findings.length }))}</span>` +
          `<label class="ec-field ec-page-size"><span>${esc(t('ec_per_page'))}</span>` +
          `<select id="ecPageSize" class="ec-select" onchange="EC.setPageSize(this.value)">` +
          PAGE_SIZES.map((n) => `<option value="${n}"${n === state.pageSize ? ' selected' : ''}>${n || t('ec_all')}</option>`).join('') +
          `</select></label>`
        : '';
    }

    let html = strip;
    if (!findings.length) {
      html += `<div class="ec-empty">${esc((d.findings || []).length ? t('ec_no_match') : t('ec_no_findings'))}</div>`;
    } else {
      html += `<div class="ec-table">
        <div class="ec-table-head">
          <span></span><span>${esc(t('ec_severity'))}</span><span>${esc(t('ec_findings'))}</span>
          <span>${esc(t('ec_source'))}</span><span>KEV / EPSS</span><span></span>
        </div>`;
      for (let i = 0; i < state.view.length; i++) html += EC.rowHtml(state.view[i], i);
      html += `</div>`;
    }
    if (d.notes && d.notes.length) {
      html += `<div class="ec-note">${esc(t('ec_notes'))}: ${d.notes.map(esc).join(' · ')}</div>`;
    }
    list.innerHTML = html;
    EC.paintPager(findings.length, totalPages);
  };

  EC.paintPager = function (total, pages) {
    const pager = document.getElementById('ecPager');
    if (!pager) return;
    if (!total || pages <= 1) { pager.innerHTML = ''; return; }
    pager.innerHTML =
      `<button class="ec-btn" ${state.page <= 1 ? 'disabled' : ''} onclick="EC.goto(${state.page - 1})">${esc(t('ec_prev'))}</button>` +
      `<span class="ec-page-label">${esc(fill(t('ec_page'), { n: state.page, pages }))}</span>` +
      `<button class="ec-btn" ${state.page >= pages ? 'disabled' : ''} onclick="EC.goto(${state.page + 1})">${esc(t('ec_next'))}</button>`;
  };

  EC.populateSources = function (d) {
    const sel = document.getElementById('ecSource');
    if (!sel) return;
    const sources = [...new Set((d.findings || []).map((f) => f.source).filter(Boolean))].sort();
    const current = state.source;
    sel.innerHTML = `<option value="">${esc(t('ec_all'))}</option>` +
      sources.map((s) => `<option value="${esc(s)}"${s === current ? ' selected' : ''}>${esc(s)}</option>`).join('');
  };

  // ---------------------------------------------------------------------------
  // Row rendering (compact, single line; details expand on click)

  EC.rowHtml = function (f, idx) {
    const badges = [];
    if (f.kev) badges.push(`<span class="ec-badge kev">${esc(t('ec_kev'))}</span>`);
    if (f.epss) badges.push(`<span class="ec-badge epss">${esc(fill(t('ec_epss'), { n: Math.round(f.epss * 100) }))}</span>`);
    if (f.reachability && f.reachability.public_bind) badges.push(`<span class="ec-badge exposed">${esc(t('ec_public'))}</span>`);
    if (f.reachability && f.reachability.cloudflare_proxied) badges.push(`<span class="ec-badge exposed">${esc(t('ec_cf'))}</span>`);
    const cves = (f.cves || []).slice(0, 3).map((c) => `<span class="ec-cve">${esc(c)}</span>`).join(' ');
    const more = (f.cves || []).length > 3 ? `<span class="ec-cve-more">+${(f.cves || []).length - 3}</span>` : '';
    const ver = [f.version, f.fixed_version ? '→ ' + f.fixed_version : ''].filter(Boolean).map(esc).join(' ');
    const fixes = (f.mitigations || []).map((m) => EC.mitigationButton(f, m, idx)).join('');
    const open = !!state.expanded[f.id || idx];
    const key = String(f.id || idx).replace(/'/g, "\\'");
    return `<div class="ec-finding ${sevClass(f.severity)}${open ? ' expanded' : ''}" data-idx="${idx}">
      <div class="ec-row" onclick="EC.toggle('${key}',${idx})">
        <span class="ec-chevron">${icon(open ? 'chevron-down' : 'chevron-right')}</span>
        <span class="ec-sev ${sevClass(f.severity)}">${esc(f.severity)}</span>
        <span class="ec-main">
          <span class="ec-comp">${esc(f.component)}</span>
          ${ver ? `<span class="ec-ver">${ver}</span>` : ''}
          <span class="ec-title-line">${esc(f.title)}</span>
          <span class="ec-cves">${cves}${more}</span>
        </span>
        <span class="ec-src">${esc(f.source)}</span>
        <span class="ec-badges">${badges.join('')}</span>
        <span class="ec-row-actions" onclick="event.stopPropagation()">${fixes}</span>
      </div>
      <div class="ec-detail"${open ? '' : ' hidden'}>
        ${f.summary ? `<div class="ec-summary">${esc(f.summary)}</div>` : ''}
        ${f.location ? `<div class="ec-meta">${esc(t('ec_details'))}: ${esc(f.location)}</div>` : ''}
      </div>
    </div>`;
  };

  EC.toggle = function (key, idx) {
    const f = state.view[idx];
    if (!f) return;
    const id = String(f.id || idx);
    state.expanded[id] = !state.expanded[id];
    const row = document.querySelector('#ecList .ec-finding[data-idx="' + idx + '"]');
    if (!row) return;
    row.classList.toggle('expanded', state.expanded[id]);
    const detail = row.querySelector('.ec-detail');
    if (detail) detail.hidden = !state.expanded[id];
    const chev = row.querySelector('.ec-chevron');
    if (chev) chev.innerHTML = icon(state.expanded[id] ? 'chevron-down' : 'chevron-right');
  };

  EC.mitigationButton = function (f, m, idx) {
    const map = {
      upgrade_package: 'ec_fix_upgrade', ansible_patch: 'ec_fix_ansible',
      firewall_restrict: 'ec_fix_firewall', cf_lockdown: 'ec_fix_cf'
    };
    const label = map[m];
    if (!label) return '';
    const danger = m === 'firewall_restrict' || m === 'cf_lockdown' ? ' danger' : '';
    return `<button class="ec-btn${danger}" onclick="EC.mitigateIdx('${m}',${idx})">${esc(t(label))}</button>`;
  };

  EC.mitigateIdx = function (action, idx) {
    const f = state.view[idx];
    if (!f) return;
    EC.mitigate(action, f);
  };

  EC.mitigate = async function (action, f) {
    if (!f) return;
    const component = f.component || '';
    const port = (f.reachability && f.reachability.ports || [])[0] || '';
    let msg = '';
    if (action === 'upgrade_package') msg = fill(t('ec_confirm_upgrade'), { name: component });
    else if (action === 'firewall_restrict') msg = fill(t('ec_confirm_firewall'), { port: port || '?' });
    else if (action === 'cf_lockdown') msg = t('ec_confirm_cf').replace('{zone}', (f.reachability && f.reachability.domains || []).join(', ') || '?');
    else msg = t('ec_confirm_ansible');
    if (typeof showConfirm === 'function') {
      const ok = await showConfirm({ title: t('ec_title'), message: msg, danger: true });
      if (!ok) return;
    }
    const body = { action, confirm: true, packages: component, port, zone_id: '', cf_account_id: 0, cf_mode: 'security_level', cf_value: 'under_attack', module: '', args: '' };
    try {
      const r = await fetch('/api/hosts/' + state.hostID + '/exposure/mitigate', {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body)
      });
      const j = await r.json();
      if (!j.ok && j.error) throw new Error(j.error);
      if (j.websocket && typeof openPkgUpgradeLogs === 'function') openPkgUpgradeLogs(state.hostID, j.websocket);
      if (typeof showToast === 'function') showToast(t('ec_done'));
    } catch (e) {
      if (typeof showToast === 'function') showToast(fill(t('ec_failed'), { err: String(e) }));
    }
  };

  // ---------------------------------------------------------------------------
  // Control handlers

  EC.filterRows = function (v) { state.query = v; state.page = 1; EC.paint(); };
  EC.setFacet = function (k, v) { state[k] = v; state.page = 1; EC.paint(); };
  EC.setPageSize = function (v) { state.pageSize = parseInt(v, 10) || 25; state.page = 1; EC.paint(); };
  EC.goto = function (p) { state.page = p; EC.paint(); const l = document.getElementById('ecList'); if (l) l.scrollIntoView({ block: 'start' }); };
})();
