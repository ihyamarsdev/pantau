/* Security Center (ADR 0089). One vanilla component shared by the global Security view and the
   Host Workspace Security tab. Zero external libraries (ADR 0013); design tokens + icon sprite
   (ADR 0039); EN/ID i18n (ADR 0009). Relies on these index.html globals: svgIcon, escapeHtml,
   t, I18N, toast (optional), confirmDialog (optional), currentLang via I18N. */
(function () {
  'use strict';

  const I = {
    en: {
      nav_security: 'Security',
      sc_title: 'Security Center',
      sc_subtitle: 'Unified posture, triage and remediation across your fleet.',
      sc_run: 'Run Audit',
      sc_run_quick: 'Quick Scan (hardening & updates)',
      sc_run_osv: 'OSV / KEV intelligence',
      sc_run_container: 'Container audit',
      sc_run_iac: 'IaC & secrets scan',
      sc_pillar_overview: 'Overview & Triage',
      sc_pillar_vulns: 'Vulnerabilities &\u00a0Updates',
      sc_pillar_network: 'Network & Perimeter',
      sc_pillar_containers: 'Containers & Images',
      sc_pillar_identity: 'Identity & Integrity',
      sc_pillar_code: 'Code, IaC & Secrets',
      sc_pillar_exposure: 'Exposure Radar',
      sc_section: 'Section',
      sc_kpi_kev: 'KEV / Critical',
      sc_kpi_fim: 'FIM Drift',
      sc_kpi_secrets: 'Secrets & IaC',
      sc_kpi_updates: 'Pending Updates',
      sc_posture: 'Posture Score',
      sc_top_actions: 'Top Action Queue',
      sc_no_actions: 'No high-risk actions. Posture looks clean.',
      sc_evidence: 'Technical evidence',
      sc_remediation: 'Remediation',
      sc_ask_ai: 'Ask AI Agent to Remediate',
      sc_upgrade: 'Upgrade Package',
      sc_firewall_action: 'Firewall Action',
      sc_accept_fim: 'Accept FIM Baseline',
      sc_open_source: 'Open Source File',
      sc_close: 'Close',
      sc_prev: 'Previous finding',
      sc_next: 'Next finding',
      sc_host: 'Host',
      sc_kev_only: 'KEV Only',
      sc_fix_available: 'Fix Available',
      sc_loading: 'Loading pillar...',
      sc_error: 'Could not load this pillar.',
      sc_confirm_action: 'Confirm security action',
      sc_action_done: 'Action requested.',
      sc_no_host: 'Select a host in the scope selector to run this audit.',
      sc_filter_hint: 'Filter by this finding category',
      sc_findings: 'Security Findings',
      sec_th_pkg: 'Package',
      sec_th_installed: 'Installed',
      sec_th_fixed: 'Fixed',
      sec_th_severity: 'Severity',
      sec_th_advisory: 'Advisory',
      sc_pkg_updates: 'Package Update Check',
      sc_pkg_updates_empty: 'No package update check has been run for this host.',
      sc_pkg_check: 'Check for updates',
      sc_pkg_pending: '{n} pending ({s} security)',
      sc_filter_empty: 'No findings match your filter.'
    },
    id: {
      nav_security: 'Keamanan',
      sc_title: 'Pusat Keamanan',
      sc_subtitle: 'Postur, triase, dan remediasi terpadu di seluruh armada.',
      sc_run: 'Jalankan Audit',
      sc_run_quick: 'Pemindaian Cepat (hardening & update)',
      sc_run_osv: 'Intelijen OSV / KEV',
      sc_run_container: 'Audit kontainer',
      sc_run_iac: 'Pemindaian IaC & rahasia',
      sc_pillar_overview: 'Ringkasan & Triage',
      sc_pillar_vulns: 'Kerentanan &\u00a0Pembaruan',
      sc_pillar_network: 'Jaringan & Perimeter',
      sc_pillar_containers: 'Kontainer & Image',
      sc_pillar_identity: 'Identitas & Integritas',
      sc_pillar_code: 'Kode, IaC & Rahasia',
      sc_pillar_exposure: 'Radar Eksposur',
      sc_section: 'Bagian',
      sc_kpi_kev: 'KEV / Kritis',
      sc_kpi_fim: 'Drift FIM',
      sc_kpi_secrets: 'Rahasia & IaC',
      sc_kpi_updates: 'Pembaruan Tertunda',
      sc_posture: 'Skor Postur',
      sc_top_actions: 'Antrean Aksi Prioritas',
      sc_no_actions: 'Tidak ada aksi berisiko tinggi. Postur terlihat bersih.',
      sc_evidence: 'Bukti teknis',
      sc_remediation: 'Remediasi',
      sc_ask_ai: 'Minta Agen AI Memperbaiki',
      sc_upgrade: 'Perbarui Paket',
      sc_firewall_action: 'Aksi Firewall',
      sc_accept_fim: 'Terima Baseline FIM',
      sc_open_source: 'Buka File Sumber',
      sc_close: 'Tutup',
      sc_prev: 'Temuan sebelumnya',
      sc_next: 'Temuan berikutnya',
      sc_host: 'Host',
      sc_kev_only: 'Hanya KEV',
      sc_fix_available: 'Ada Perbaikan',
      sc_loading: 'Memuat bagian...',
      sc_error: 'Gagal memuat bagian ini.',
      sc_confirm_action: 'Konfirmasi aksi keamanan',
      sc_action_done: 'Aksi diminta.',
      sc_no_host: 'Pilih host pada pemilih cakupan untuk menjalankan audit ini.',
      sc_filter_hint: 'Filter menurut kategori temuan ini',
      sc_findings: 'Temuan Keamanan',
      sec_th_pkg: 'Paket',
      sec_th_installed: 'Terpasang',
      sec_th_fixed: 'Perbaikan',
      sec_th_severity: 'Keparahan',
      sec_th_advisory: 'Advisori',
      sc_pkg_updates: 'Pemeriksaan Pembaruan Paket',
      sc_pkg_updates_empty: 'Belum ada pemeriksaan pembaruan paket untuk host ini.',
      sc_pkg_check: 'Periksa pembaruan',
      sc_pkg_pending: '{n} tertunda ({s} keamanan)',
      sc_filter_empty: 'Tidak ada temuan yang cocok dengan filter.'
    }
  };
  ['en', 'id'].forEach(function (l) { I18N[l] = Object.assign(I18N[l] || {}, I[l]); });

  const esc = function (s) { return escapeHtml(s == null ? '' : String(s)); };
  const ic = function (n, c) { return svgIcon(n, c || 'icon-sm'); };
  const tr = function (k, f) { return t(k, f); };

  const PILLARS = [
    { id: 'overview', icon: 'layout-dashboard', key: 'sc_pillar_overview' },
    { id: 'vulnerabilities', icon: 'bug', key: 'sc_pillar_vulns' },
    { id: 'network', icon: 'globe', key: 'sc_pillar_network' },
    { id: 'containers', icon: 'container', key: 'sc_pillar_containers' },
    { id: 'identity', icon: 'key-round', key: 'sc_pillar_identity' },
    { id: 'code', icon: 'file-code', key: 'sc_pillar_code' },
    { id: 'exposure', icon: 'radar', key: 'sc_pillar_exposure' }
  ];

  // Module state. One component instance is mounted at a time (global view or host tab).
  const S = {
    root: null,
    hostID: 0,
    overview: null,
    activePillar: 'overview',
    actions: [],
    drawerOpen: false,
    drawerIndex: 0,
    kpiFilter: '',
    legacyHtml: '',
    hostCtx: null,
    seq: 0,
    loadedPillars: {},
    collapsed: {},   // per-section collapse state, keyed by section id
    vulnFilter: { q: '', kevOnly: false, fixAvailable: true, sort: 'sev_desc' }
  };

  function api(path, opts) {
    return fetch(path, Object.assign({ credentials: 'same-origin' }, opts || {}))
      .then(function (r) { return r.json().catch(function () { return {}; }).then(function (j) { return { ok: r.ok, data: j }; }); });
  }

  function toastMsg(msg) {
    if (window.toast) { try { toast(msg); return; } catch (e) { /* noop */ } }
    if (window.showToast) { try { showToast(msg); return; } catch (e) { /* noop */ } }
  }

  function confirmMsg(msg) {
    // Mutating actions always ask for explicit confirmation. Fall back to window.confirm
    // only when the app's promise-based dialog is unavailable.
    if (window.confirmDialog) { try { return Promise.resolve(confirmDialog(msg)); } catch (e) { /* noop */ } }
    return Promise.resolve(window.confirm(msg));
  }

  // ---- Scope ----------------------------------------------------------------

  function scopeQuery() {
    return '?host_id=' + S.hostID;
  }

  function loadOverview() {
    const seq = ++S.seq;
    return api('/api/security/overview' + scopeQuery()).then(function (res) {
      if (seq !== S.seq || !S.root) return;
      S.overview = res.ok ? res.data : null;
      S.actions = (S.overview && S.overview.top_actions) || [];
      S.loadedPillars = {};
      paintStrip();
      paintNav();
      renderActivePillar();
    });
  }

  // ---- Shell ----------------------------------------------------------------

  function shellHtml() {
    return '' +
      '<div class="sc-strip" id="scStrip"></div>' +
      '<div class="sc-body">' +
        '<nav class="sc-nav" id="scNav" aria-label="' + esc(tr('sc_title', 'Security Center')) + '"></nav>' +
        '<main class="sc-pane" id="scPane" role="region" aria-live="polite"></main>' +
      '</div>' +
      '<div class="sc-drawer-backdrop" id="scDrawerBackdrop" data-sc-action="close-drawer"></div>' +
      '<aside class="sc-drawer" id="scDrawer" role="dialog" aria-modal="true" aria-label="' + esc(tr('sc_evidence', 'Technical evidence')) + '"></aside>';
  }

  function mountHost(container, hostID, ctx) {
    if (!container) return;
    S.root = container;
    S.hostID = hostID;
    S.activePillar = 'overview';
    S.legacyHtml = (ctx && ctx.legacyHtml) || '';
    S.hostCtx = ctx || null;
    container.innerHTML = shellHtml();
    bindEvents();
    loadOverview();
  }

  // Scope every lookup to the active component root: a global view and a host tab can coexist.
  function q(id) { return S.root ? S.root.querySelector('#' + id) : null; }


  let keydownBound = false;
  function bindEvents() {
    if (!S.root) return;
    S.root.addEventListener('click', onShellClick);
    S.root.addEventListener('change', onShellChange);
    S.root.addEventListener('input', onShellChange);
    if (!keydownBound) { document.addEventListener('keydown', onKeyDown); keydownBound = true; }
  }

  function onShellClick(e) {
    const el = e.target.closest('[data-sc-action]');
    if (!el) return;
    const action = el.getAttribute('data-sc-action');
    if (action === 'pillar') { selectPillar(el.getAttribute('data-pillar')); }
    else if (action === 'kpi') { const p = el.getAttribute('data-kpi'); S.kpiFilter = (S.kpiFilter === p ? '' : p); paintStrip(); renderActivePillar(); }
    else if (action === 'run-menu') { const m = q('scRunMenu'); if (m) m.classList.toggle('open'); e.stopPropagation(); }
    else if (action === 'run') { runAudit(el.getAttribute('data-run')); const m = q('scRunMenu'); if (m) m.classList.remove('open'); }
    else if (action === 'finding') { openDrawer(parseInt(el.getAttribute('data-index'), 10) || 0); }
    else if (action === 'close-drawer') { closeDrawer(); }
    else if (action === 'drawer-prev') { navDrawer(-1); }
    else if (action === 'drawer-next') { navDrawer(1); }
    else if (action === 'remediation') { remediate(el.getAttribute('data-remediation')); }
    else if (action === 'collapse-nav') { const n = q('scNav'); if (n) n.classList.toggle('collapsed'); }
    else if (action === 'toggle-section') { toggleSection(el.getAttribute('data-section')); }
    else if (action === 'vuln-finding') { openVulnFinding(el.getAttribute('data-pkg')); }
    else if (action === 'pkg-check') { checkPkgUpdates(parseInt(el.getAttribute('data-host'), 10)); }
    else if (action === 'vuln-prev') { navVulnDrawer(-1); }
    else if (action === 'vuln-next') { navVulnDrawer(1); }
  }

  function onShellChange(e) {
    const el = e.target;
    if (el.id === 'scVulnSearch') { S.vulnFilter.q = el.value.toLowerCase().trim(); paintVulnFindings(); }
    else if (el.id === 'scKevOnly') { S.vulnFilter.kevOnly = el.checked; paintVulnFindings(); }
    else if (el.id === 'scFixAvailable') { S.vulnFilter.fixAvailable = el.checked; paintVulnFindings(); }
    else if (el.id === 'scVulnSort') { S.vulnFilter.sort = el.value; paintVulnFindings(); }
  }

  function onKeyDown(e) {
    if (!S.drawerOpen) return;
    // The drawer lives inside the Host Workspace modal: consume the key so the modal's own
    // hierarchical Escape handler (ADR 0007) does not close the workspace beneath it.
    e.stopPropagation();
    if (e.key === 'Escape') { e.preventDefault(); closeDrawer(); }
    else if (e.key === 'ArrowDown') { e.preventDefault(); if (S.vulnDrawer) navVulnDrawer(1); else navDrawer(1); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); if (S.vulnDrawer) navVulnDrawer(-1); else navDrawer(-1); }
  }

  // ---- Zone 1: posture strip ------------------------------------------------

  function gradeClass(g) { return 'grade-' + String(g || 'f').toLowerCase(); }

  function paintStrip() {
    const strip = q('scStrip');
    if (!strip || !S.overview) return;
    const ov = S.overview;
    const b = ov.breakdown || {};
    const score = ov.score || 0;
    const circumference = 2 * Math.PI * 22;
    const offset = circumference * (1 - score / 100);
    const kpis = [
      { id: 'kev', num: b.kev_critical || 0, label: tr('sc_kpi_kev', 'KEV / Critical'), cls: 'sev-critical', icon: 'triangle-alert' },
      { id: 'fim', num: b.fim_drift || 0, label: tr('sc_kpi_fim', 'FIM Drift'), cls: 'sev-warning', icon: 'file-warning' },
      { id: 'secrets', num: b.leaked_secrets || 0, label: tr('sc_kpi_secrets', 'Secrets & IaC'), cls: 'sev-critical', icon: 'key-round' },
      { id: 'updates', num: b.pending_sec_updates || 0, label: tr('sc_kpi_updates', 'Pending Updates'), cls: 'sev-warning', icon: 'package' }
    ];
    strip.innerHTML = '' +
      '<div class="sc-ring-wrap" title="' + esc(tr('sc_posture', 'Posture Score')) + '">' +
        '<svg class="sc-ring" viewBox="0 0 52 52" aria-hidden="true">' +
          '<circle class="sc-ring-track" cx="26" cy="26" r="22"/>' +
          '<circle class="sc-ring-value" cx="26" cy="26" r="22" stroke-dasharray="' + circumference.toFixed(1) + '" stroke-dashoffset="' + offset.toFixed(1) + '"/>' +
        '</svg>' +
        '<span class="sc-ring-score">' + score + '</span>' +
        '<span class="sc-grade ' + gradeClass(ov.grade) + '">' + esc(ov.grade) + '</span>' +
      '</div>' +
      '<div class="sc-kpis">' + kpis.map(function (k) {
        return '<button type="button" class="sc-kpi ' + k.cls + (S.kpiFilter === k.id ? ' active' : '') + '" data-sc-action="kpi" data-kpi="' + k.id + '" title="' + esc(k.label) + '">' +
          ic(k.icon) + '<span class="sc-kpi-num">' + k.num + '</span><span>' + esc(k.label) + '</span></button>';
      }).join('') + '</div>' +
      '<div class="sc-split">' +
        '<button type="button" class="sc-run" data-sc-action="run" data-run="quick">' + ic('play') + '<span>' + esc(tr('sc_run', 'Run Audit')) + '</span></button>' +
        '<button type="button" class="sc-caret" data-sc-action="run-menu" aria-label="Audit options">' + ic('chevron-down') + '</button>' +
        '<div class="sc-split-menu" id="scRunMenu">' +
          '<button type="button" data-sc-action="run" data-run="quick">' + esc(tr('sc_run_quick', 'Quick Scan')) + '</button>' +
          '<button type="button" data-sc-action="run" data-run="osv">' + esc(tr('sc_run_osv', 'OSV / KEV')) + '</button>' +
          '<button type="button" data-sc-action="run" data-run="container">' + esc(tr('sc_run_container', 'Container audit')) + '</button>' +
          '<button type="button" data-sc-action="run" data-run="iac">' + esc(tr('sc_run_iac', 'IaC & secrets scan')) + '</button>' +
        '</div>' +
      '</div>';
  }

  function currentHostID() {
    return S.hostID;
  }

  function runAudit(kind) {
    const hid = currentHostID();
    if (!hid) { toastMsg(tr('sc_no_host', 'Select a host in the scope selector to run this audit.')); return; }
    let path = '', method = 'POST', body = null;
    if (kind === 'quick') path = '/api/hosts/' + hid + '/security/scan';
    else if (kind === 'osv') path = '/api/hosts/' + hid + '/exposure/scan';
    else if (kind === 'container') path = '/api/hosts/' + hid + '/docker/security/audit';
    else if (kind === 'iac') { toastMsg(tr('sc_run_iac', 'IaC & secrets scan') + ': /api/iac-scan'); return; }
    confirmMsg(tr('sc_confirm_action', 'Confirm security action') + ': ' + tr('sc_run_' + kind, kind)).then(function (ok) {
      if (!ok) return;
      const opts = { method: method, headers: { 'Content-Type': 'application/json' } };
      if (body) opts.body = JSON.stringify(body);
      api(path, opts).then(function () { toastMsg(tr('sc_action_done', 'Action requested.')); loadOverview(); });
    });
  }

  // ---- Zone 2: pillar sub-navigation ----------------------------------------

  function paintNav() {
    const nav = q('scNav');
    if (!nav || !S.overview) return;
    const p = S.overview.pillars || {};
    nav.innerHTML = PILLARS.map(function (pillar) {
      const count = p[pillar.id] || 0;
      return '<button type="button" class="sc-nav-item' + (S.activePillar === pillar.id ? ' active' : '') + '" data-sc-action="pillar" data-pillar="' + pillar.id + '">' +
        ic(pillar.icon) + '<span>' + esc(tr(pillar.key, pillar.id)) + '</span>' +
        (count ? '<span class="sc-badge">' + count + '</span>' : '') + '</button>';
    }).join('') +
    '<button type="button" class="sc-nav-item sc-collapse-btn" data-sc-action="collapse-nav">' + ic('panel-left-close') + '<span>' + esc(tr('nav_collapse', 'Collapse')) + '</span></button>';
  }

  function selectPillar(id) {
    if (!PILLARS.some(function (p) { return p.id === id; })) return;
    S.activePillar = id;
    paintNav();
    renderActivePillar();
  }

  function afterPillarRender() {
    // Re-run the legacy sub-module renderers whose containers SC just mounted.
    try { if (typeof filterSecurityFindings === 'function') filterSecurityFindings(); } catch (e) { /* noop */ }
    const legacy = S.activePillar === 'overview' && S.legacyHtml;
    try { if (window.Fw && (S.activePillar === 'network' || legacy)) Fw.render(); } catch (e) { /* noop */ }
    try { if (window.OV && (S.activePillar === 'vulnerabilities' || legacy)) OV.render(S.hostID); } catch (e) { /* noop */ }
    if (S.activePillar === 'vulnerabilities' && currentHostID()) { paintVulnFindings(); loadPkgUpdates(currentHostID()); }
    try { if (window.EC && S.activePillar === 'exposure') EC.render(S.hostID); } catch (e) { /* noop */ }
    try { if (window.AC && (S.activePillar === 'identity' || legacy)) { AC.render(); AC.load(); } } catch (e) { /* noop */ }
    try {
      if (S.hostCtx && S.hostCtx.scan && S.activePillar === 'overview' && window.VI) {
        // VI cardHtml was already embedded in legacyHtml; nothing async needed here.
      }
    } catch (e) { /* noop */ }
  }

  // ---- Zone 3: lazy main pane ------------------------------------------------

  function filteredActions() {
    if (!S.kpiFilter) return S.actions;
    const map = { kev: ['vulnerabilities'], fim: ['identity'], secrets: ['code'], updates: ['vulnerabilities'] };
    const pillars = map[S.kpiFilter] || [];
    return S.actions.filter(function (a) { return pillars.indexOf(a.pillar) >= 0; });
  }

  function renderActivePillar() {
    const pane = q('scPane');
    if (!pane) return;
    const seq = ++S.seq;
    pane.innerHTML = '<div class="sc-empty">' + ic('loader-circle') + ' ' + esc(tr('sc_loading', 'Loading pillar...')) + '</div>';
    pillarHtml(S.activePillar).then(function (html) {
      if (seq !== S.seq || !pane) return;
      // DocumentFragment keeps the DOM node count minimal during the swap.
      const frag = document.createDocumentFragment();
      const holder = document.createElement('div');
      holder.innerHTML = html;
      while (holder.firstChild) frag.appendChild(holder.firstChild);
      pane.innerHTML = '';
      pane.appendChild(frag);
      afterPillarRender();
    }).catch(function () {
      if (seq !== S.seq || !pane) return;
      pane.innerHTML = '<div class="sc-empty">' + esc(tr('sc_error', 'Could not load this pillar.')) + '</div>';
    });
  }

  function pillarHtml(id) {
    if (id === 'overview') return Promise.resolve(overviewPillarHtml());
    if (id === 'vulnerabilities') return vulnerabilitiesPillarHtml();
    if (id === 'network') return networkPillarHtml();
    if (id === 'containers') return containersPillarHtml();
    if (id === 'identity') return identityPillarHtml();
    if (id === 'code') return codePillarHtml();
    if (id === 'exposure') return exposurePillarHtml();
    return Promise.resolve('');
  }

  // Collapsible wrapper for every card/section. State lives in S.collapsed so it survives
  // pillar re-renders; the DOM toggle itself needs no re-render.
  function sectionHtml(id, title, iconName, bodyHtml, opts) {
    const o = opts || {};
    const collapsed = !!S.collapsed[id];
    const badge = o.badge != null ? '<span class="sc-sec-badge">' + esc(String(o.badge)) + '</span>' : '';
    return '<section class="sc-section' + (collapsed ? ' collapsed' : '') + '" data-section-id="' + esc(id) + '">' +
      '<button type="button" class="sc-section-head" data-sc-action="toggle-section" data-section="' + esc(id) + '" aria-expanded="' + (!collapsed) + '">' +
        ic('chevron-down', 'sc-sec-chevron') +
        '<span class="sc-section-label">' + esc(title) + '</span>' + badge +
      '</button>' +
      '<div class="sc-section-body">' + bodyHtml + '</div>' +
    '</section>';
  }

  function toggleSection(id) {
    if (!id) return;
    S.collapsed[id] = !S.collapsed[id];
    const sec = S.root ? S.root.querySelector('[data-section-id="' + id + '"]') : null;
    if (sec) {
      sec.classList.toggle('collapsed', S.collapsed[id]);
      const head = sec.querySelector('.sc-section-head');
      if (head) head.setAttribute('aria-expanded', String(!S.collapsed[id]));
    }
  }

  function findingRow(a, index) {
    const sev = String(a.severity || 'medium').toLowerCase();
    const sevCls = 'sev-' + (sev === 'critical' || sev === 'critical_exposed' ? 'critical' : (sev === 'high' || sev === 'important' ? 'high' : (sev === 'medium' || sev === 'moderate' ? 'medium' : 'low')));
    return '<div class="sc-finding ' + sevCls + '" data-sc-action="finding" data-index="' + index + '" tabindex="0" role="button">' +
      '<div><div class="sc-finding-title">' + esc(a.title || a.id) + '</div>' +
      '<div class="sc-finding-detail">' + esc(a.detail || '') + (a.host_name ? ' · ' + esc(a.host_name) : '') + '</div></div>' +
      '<div class="sc-finding-meta"><span class="sc-sev-badge ' + sevCls + '">' + esc(sev) + '</span>' + ic('chevron-right') + '</div>' +
      '</div>';
  }

  function overviewPillarHtml() {
    const ov = S.overview || {};
    const b = ov.breakdown || {};
    const actions = filteredActions();
    const breakdown = [
      ['kev_critical', 'sc_kpi_kev', b.kev_critical],
      ['fim_drift', 'sc_kpi_fim', b.fim_drift],
      ['leaked_secrets', 'sc_kpi_secrets', b.leaked_secrets],
      ['pending_sec_updates', 'sc_kpi_updates', b.pending_sec_updates],
      ['container_issues', 'sc_pillar_containers', b.container_issues],
      ['firewall_warnings', 'sc_pillar_network', b.firewall_warnings]
    ];
    const postureBody = '<div style="display:flex;flex-wrap:wrap;gap:8px;">' + breakdown.map(function (x) {
      return '<span class="badge badge-sec-' + (x[2] > 0 ? 'critical' : 'unknown') + '">' + esc(tr(x[1], x[0])) + ': ' + (x[2] || 0) + '</span>';
    }).join('') + '</div>';
    let html = sectionHtml('ov-posture', tr('sc_posture', 'Posture Score') + ': ' + (ov.score || 0) + ' (' + esc(ov.grade || '') + ')', 'gauge', postureBody);
    const queueBody = actions.length ? actions.map(findingRow).join('') : '<div class="sc-empty">' + esc(tr('sc_no_actions', 'No high-risk actions. Posture looks clean.')) + '</div>';
    html += sectionHtml('ov-queue', tr('sc_top_actions', 'Top Action Queue'), 'zap', queueBody, { badge: actions.length });
    // Legacy host scan content (findings, hardening) stays in triage; the Exposure Radar is NOT
    // part of overview any more — it has its own pillar.
    if (S.legacyHtml) html += legacySections();
    return Promise.resolve(html);
  }

  // Wrap each top-level card of the legacy host Security HTML in a collapsible section. Cards are
  // detected by their root element so every security area becomes independently collapsible.
  const RELOCATED_CARDS = { secExposureCard: 1, secIntelCard: 1, secAccountsCard: 1, secFindingsCard: 1, secOsvVulnsCard: 1 };
  function legacySections() {
    const holder = document.createElement('div');
    holder.innerHTML = S.legacyHtml;
    const nodes = Array.prototype.filter.call(holder.children, function (node) {
      return !(node.id && RELOCATED_CARDS[node.id]);
    });
    // The first node is the Security header/action row and the next is the scrollable body that
    // holds the findings cards. Merge them into a single collapsible "Security" section so the
    // header, status, actions and findings live in one panel instead of two stacked panels.
    const cards = [];
    let i = 0;
    if (nodes.length >= 2 && nodes[0].querySelector('h4') && /Security/i.test(nodes[0].querySelector('h4').textContent)) {
      // Remove cards that now live in their own pillar before merging the body.
      const body = nodes[1].cloneNode(true);
      Object.keys(RELOCATED_CARDS).forEach(function (id) {
        const el = body.querySelector('#' + id);
        if (el) el.parentNode.removeChild(el);
      });
      const merged = '<div class="sc-legacy-head">' + nodes[0].innerHTML + '</div>' + body.innerHTML;
      cards.push(sectionHtml('legacy-security', tr('tab_security', 'Security'), 'shield', merged));
      i = 2;
    }
    for (; i < nodes.length; i++) {
      const node = nodes[i];
      const id = node.id || ('legacy-' + i);
      let title = node.id || tr('sc_section', 'Section');
      const heading = node.querySelector('h3, h4');
      if (heading && heading.textContent.trim()) title = heading.textContent.trim();
      cards.push(sectionHtml('legacy-' + id, title, 'file-text', node.outerHTML));
    }
    return cards.join('');
  }

  function vulnerabilitiesPillarHtml() {
    const legacy = (S.legacyHtml || '');
    const intel = legacy ? extractCards(legacy, ['secIntelCard']) : '';
    const osv = legacy ? extractCards(legacy, ['secOsvVulnsCard']) : '';

    // Host scope: real filters + findings table + OSV card + package update check.
    const filters = '<div class="sc-vuln-toolbar">' +
      '<input type="search" id="scVulnSearch" class="input" placeholder="' + esc(tr('search_findings_ph', 'Filter packages or advisories...')) + '" value="' + esc(S.vulnFilter.q) + '">' +
      '<select id="scVulnSort" class="input">' +
        '<option value="sev_desc"' + (S.vulnFilter.sort === 'sev_desc' ? ' selected' : '') + '>' + esc(tr('sort_sev_desc', 'Severity: High to Low')) + '</option>' +
        '<option value="sev_asc"' + (S.vulnFilter.sort === 'sev_asc' ? ' selected' : '') + '>' + esc(tr('sort_sev_asc', 'Severity: Low to High')) + '</option>' +
        '<option value="pkg_asc"' + (S.vulnFilter.sort === 'pkg_asc' ? ' selected' : '') + '>' + esc(tr('sort_pkg_asc', 'Package Name (A-Z)')) + '</option>' +
      '</select>' +
      '<label class="sc-toggle"><input type="checkbox" id="scKevOnly"' + (S.vulnFilter.kevOnly ? ' checked' : '') + '> ' + esc(tr('sc_kev_only', 'KEV Only')) + '</label>' +
      '<label class="sc-toggle"><input type="checkbox" id="scFixAvailable"' + (S.vulnFilter.fixAvailable ? ' checked' : '') + '> ' + esc(tr('sc_fix_available', 'Fix Available')) + '</label>' +
      '</div><div id="scVulnFindings"></div>';
    let html = sectionHtml('vuln-findings', tr('sc_findings', 'Security Findings'), 'search', filters);
    if (intel) html += sectionHtml('vuln-intel', 'Exploit intelligence', 'shield-alert', intel);
    if (osv) html += sectionHtml('vuln-osv', tr('ov_title', 'Known vulnerabilities in installed packages'), 'shield-alert', osv);
    html += sectionHtml('vuln-pkgupdates', tr('sc_pkg_updates', 'Package Update Check'), 'package', '<div id="scPkgUpdates"><div class="sc-empty">' + esc(tr('sc_loading', 'Loading pillar...')) + '</div></div>');
    return Promise.resolve(html);
  }

  // Filter/sort the host's security findings and paint them into the vuln pillar. Each row opens
  // the evidence drawer with a concrete finding payload and an Upgrade Package remediation.
  function paintVulnFindings() {
    const host = q('scVulnFindings');
    if (!host) return;
    const scan = (S.hostCtx && S.hostCtx.scan) || null;
    const findings = (scan && scan.findings) || [];
    const f = S.vulnFilter;
    let list = findings.slice();
    if (f.q) {
      list = list.filter(function (x) {
        return (x.package || '').toLowerCase().indexOf(f.q) >= 0 ||
          (x.advisory || '').toLowerCase().indexOf(f.q) >= 0 ||
          (x.cves || []).some(function (c) { return c.toLowerCase().indexOf(f.q) >= 0; }) ||
          (x.severity || '').toLowerCase().indexOf(f.q) >= 0;
      });
    }
    if (f.fixAvailable) list = list.filter(function (x) { return !!(x.fixed_version && x.fixed_version !== '-'); });
    if (f.kevOnly) {
      const intel = (S.hostCtx && S.hostCtx.intel) || null;
      list = list.filter(function (x) { return window.VI && VI.score(x, intel) >= 2; });
    }
    const weight = { critical: 5, important: 4, moderate: 3, low: 2, unknown: 1 };
    list.sort(function (a, b) {
      if (f.sort === 'pkg_asc') return (a.package || '').localeCompare(b.package || '');
      const diff = (weight[String(b.severity).toLowerCase()] || 0) - (weight[String(a.severity).toLowerCase()] || 0);
      if (f.sort === 'sev_asc') return -diff || (a.package || '').localeCompare(b.package || '');
      return diff || (a.package || '').localeCompare(b.package || '');
    });
    if (!list.length) { host.innerHTML = '<div class="sc-empty">' + esc(tr('sc_filter_empty', 'No findings match your filter.')) + '</div>'; return; }
    host.innerHTML = '<div class="sc-vuln-table">' + list.map(function (x) {
      const sev = String(x.severity || 'unknown').toLowerCase();
      const sevCls = 'sev-' + (sev === 'critical' ? 'critical' : (sev === 'high' || sev === 'important' ? 'high' : (sev === 'medium' || sev === 'moderate' ? 'medium' : 'low')));
      return '<div class="sc-vuln-row ' + sevCls + '" data-sc-action="vuln-finding" data-pkg="' + esc(x.package || '') + '">' +
        '<span class="sc-vuln-pkg">' + esc(x.package || '') + '</span>' +
        '<span class="sc-vuln-ver">' + esc(x.installed_version || '-') + ' → ' + esc(x.fixed_version || '-') + '</span>' +
        '<span class="sc-sev-badge ' + sevCls + '">' + esc(sev) + '</span>' +
        '<span class="sc-vuln-adv">' + esc(x.advisory || '') + '</span>' +
        (window.VI ? '<span class="sc-vuln-cve">' + VI.findingCell(x, (S.hostCtx && S.hostCtx.intel) || null) + '</span>' : '') +
        '</div>';
    }).join('') + '</div>';
  }

  function loadPkgUpdates(hid) {
    const slot = q('scPkgUpdates');
    if (!slot) return;
    api('/api/hosts/' + hid + '/packages/updates').then(function (res) {
      if (!slot.isConnected) return;
      if (!res.ok || !res.data.checked) { slot.innerHTML = '<div class="sc-empty">' + esc(tr('sc_pkg_updates_empty', 'No package update check has been run for this host.')) + '</div>'; return; }
      const c = res.data.check;
      const rows = (c.packages || []).slice(0, 30).map(function (p) {
        return '<div class="sc-vuln-row"><span class="sc-vuln-pkg">' + esc(p.name) + '</span><span class="sc-vuln-ver">' + esc(p.from || '-') + ' → ' + esc(p.to || '-') + '</span>' + (p.security ? '<span class="sc-sev-badge sev-critical">security</span>' : '') + '</div>';
      }).join('');
      slot.innerHTML = '<div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap;margin-bottom:10px;">' +
        '<span class="badge badge-sec-critical">' + esc(tr('sc_pkg_pending', '{n} pending ({s} security)', true).replace('{n}', String(c.pending_total || 0)).replace('{s}', String(c.pending_security || 0))) + '</span>' +
        '<button type="button" class="btn btn-sm" data-sc-action="pkg-check" data-host="' + hid + '">' + esc(tr('sc_pkg_check', 'Check for updates')) + '</button></div>' +
        (rows || '<div class="sc-empty">' + esc(tr('sc_no_actions', 'No high-risk actions. Posture looks clean.')) + '</div>');
    });
  }

  function exposurePillarHtml() {
    const legacy = (S.legacyHtml || '');
    const radar = legacy ? extractCards(legacy, ['secExposureCard']) : '';
    const body = radar || '<div class="sc-empty">' + esc(tr('sc_no_host', 'Select a host in the scope selector to run this audit.')) + '</div>';
    return Promise.resolve(sectionHtml('exposure-radar', tr('sc_pillar_exposure', 'Exposure Radar'), 'radar', body));
  }

  function networkPillarHtml() {
    const hid = currentHostID();
    return Promise.resolve(sectionHtml('network-perimeter', tr('sc_pillar_network', 'Network & Perimeter'), 'shield',
      (hid ? '<div id="scFirewallSlot"></div>' : fleetNote('network'))));
  }

  function containersPillarHtml() {
    const hid = currentHostID();
    if (!hid) return Promise.resolve(sectionHtml('containers-note', tr('sc_pillar_containers', 'Containers & Images'), 'container', fleetNote('containers')));
    return api('/api/hosts/' + hid + '/docker/security').then(function (res) {
      const audit = res.data && res.data.audit ? res.data.audit : null;
      const findings = (audit && audit.findings) || [];
      const body = findings.length ? findings.slice(0, 20).map(function (f, i) {
        return findingRow({ title: f.title, detail: (f.container || '') + ' · ' + (f.rule_id || ''), severity: f.severity, remediation: f.remediation }, i);
      }).join('') : '<div class="sc-empty">' + esc(tr('sc_no_actions', 'No high-risk actions. Posture looks clean.')) + '</div>';
      return sectionHtml('containers-audit', tr('sc_pillar_containers', 'Containers & Images'), 'container', body, { badge: findings.length });
    }).catch(function () { return sectionHtml('containers-error', tr('sc_pillar_containers', 'Containers & Images'), 'container', '<div class="sc-empty">' + esc(tr('sc_error', 'Could not load this pillar.')) + '</div>'); });
  }

  function identityPillarHtml() {
    const legacy = (S.legacyHtml || '');
    const accounts = legacy ? extractCards(legacy, ['secAccountsCard']) : '';
    const body = accounts || fleetNote('identity');
    return Promise.resolve(sectionHtml('identity-accounts', tr('sc_pillar_identity', 'Identity & Integrity'), 'key-round', body));
  }

  function codePillarHtml() {
    return api('/api/iac-scan').then(function (res) {
      const d = res.data || {};
      const summary = d.summary || {};
      const body = '<div style="display:flex;gap:8px;flex-wrap:wrap;">' +
        '<span class="badge badge-sec-critical">Critical: ' + (summary.critical || 0) + '</span>' +
        '<span class="badge badge-sec-important">High: ' + (summary.high || 0) + '</span>' +
        '<span class="badge badge-sec-moderate">Medium: ' + (summary.medium || 0) + '</span>' +
        '</div>';
      return sectionHtml('code-iac', tr('sc_pillar_code', 'Code, IaC & Secrets'), 'file-code', body);
    }).catch(function () { return sectionHtml('code-error', tr('sc_pillar_code', 'Code, IaC & Secrets'), 'file-code', '<div class="sc-empty">' + esc(tr('sc_error', 'Could not load this pillar.')) + '</div>'); });
  }

  function extractCards(html, ids) {
    const holder = document.createElement('div');
    holder.innerHTML = html;
    return ids.map(function (id) {
      const el = holder.querySelector('#' + id);
      return el ? el.outerHTML : '';
    }).join('');
  }

  function fleetNote(pillar) {
    return '<div class="sc-empty">' + esc(tr('sc_subtitle', 'Unified posture, triage and remediation across your fleet.')) + '</div>';
  }

  // Open the evidence drawer for one concrete Security Finding by package name. The drawer's
  // Up/Down navigation walks the filtered findings of this pillar, not the global action queue.
  function openVulnFinding(pkg) {
    const scan = (S.hostCtx && S.hostCtx.scan) || null;
    const findings = (scan && scan.findings) || [];
    const idx = findings.findIndex(function (x) { return x.package === pkg; });
    if (idx < 0) return;
    S.vulnDrawer = { mode: 'finding', index: idx };
    paintVulnDrawer();
    showDrawer();
  }

  function paintVulnDrawer() {
    const dr = q('scDrawer');
    if (!dr || !S.vulnDrawer) return;
    const scan = (S.hostCtx && S.hostCtx.scan) || {};
    const list = scan.findings || [];
    const f = list[S.vulnDrawer.index];
    if (!f) return;
    const evidence = { package: f.package, installed_version: f.installed_version, fixed_version: f.fixed_version, severity: f.severity, advisory: f.advisory, cves: f.cves || [] };
    dr.innerHTML = '<div class="sc-drawer-head"><div style="flex:1;"><div class="sc-finding-title">' + esc(f.package) + '</div>' +
      '<div class="sc-finding-detail">' + esc(f.installed_version || '-') + ' → ' + esc(f.fixed_version || '-') + '</div></div>' +
      '<div class="sc-drawer-nav">' +
      '<button type="button" data-sc-action="vuln-prev" aria-label="' + esc(tr('sc_prev', 'Previous finding')) + '">' + ic('chevron-up') + '</button>' +
      '<button type="button" data-sc-action="vuln-next" aria-label="' + esc(tr('sc_next', 'Next finding')) + '">' + ic('chevron-down') + '</button>' +
      '<button type="button" data-sc-action="close-drawer" aria-label="' + esc(tr('sc_close', 'Close')) + '">' + ic('x') + '</button></div></div>' +
      '<div class="sc-drawer-body"><div class="sc-drawer-section"><h4>' + esc(tr('sc_evidence', 'Technical evidence')) + '</h4>' +
      '<pre class="sc-evidence">' + esc(JSON.stringify(evidence, null, 2)) + '</pre></div>' +
      '<div class="sc-drawer-section"><h4>' + esc(tr('sc_remediation', 'Remediation')) + '</h4><div class="sc-remediation">' +
      '<button type="button" class="primary" data-sc-action="remediation" data-remediation="upgrade">' + ic('package') + esc(tr('sc_upgrade', 'Upgrade Package')) + '</button>' +
      '<button type="button" data-sc-action="remediation" data-remediation="ask_ai">' + ic('sparkles') + esc(tr('sc_ask_ai', 'Ask AI Agent to Remediate')) + '</button>' +
      '</div></div></div><div class="sc-drawer-foot">' + (S.vulnDrawer.index + 1) + ' / ' + list.length + ' · Esc / ↑ / ↓</div>';
  }

  function navVulnDrawer(delta) {
    if (!S.vulnDrawer) return;
    const scan = (S.hostCtx && S.hostCtx.scan) || {};
    const list = scan.findings || [];
    if (!list.length) return;
    S.vulnDrawer.index = (S.vulnDrawer.index + delta + list.length) % list.length;
    paintVulnDrawer();
  }

  function checkPkgUpdates(hid) {
    if (!hid) return;
    confirmMsg(tr('sc_confirm_action', 'Confirm security action') + ': ' + tr('sc_pkg_check', 'Check for updates')).then(function (ok) {
      if (!ok) return;
      api('/api/hosts/' + hid + '/packages/updates/check', { method: 'POST' }).then(function () { loadPkgUpdates(hid); });
    });
  }

  // ---- Slide-over evidence & remediation drawer ------------------------------

  // Register the drawer in the app's modal stack so the global hierarchical Escape handler
  // (ADR 0007) closes the drawer first instead of the Host Workspace modal beneath it.
  function drawerStackPush() {
    try {
      if (!S.openModalStack.includes('scDrawer')) S.openModalStack.push('scDrawer');
    } catch (e) { /* noop */ }
  }
  function drawerStackPop() {
    try { S.openModalStack = S.openModalStack.filter(function (m) { return m !== 'scDrawer'; }); } catch (e) { /* noop */ }
  }
  function showDrawer() {
    S.drawerOpen = true;
    drawerStackPush();
    const bd = q('scDrawerBackdrop'), dr = q('scDrawer');
    if (bd) bd.classList.add('open');
    if (dr) dr.classList.add('open');
  }

  function openDrawer(index) {
    const actions = filteredActions();
    if (!actions.length) return;
    S.drawerIndex = Math.max(0, Math.min(index, actions.length - 1));
    paintDrawer();
    showDrawer();
  }

  function closeDrawer() {
    S.drawerOpen = false;
    S.vulnDrawer = null;
    drawerStackPop();
    const bd = q('scDrawerBackdrop');
    const dr = q('scDrawer');
    if (bd) bd.classList.remove('open');
    if (dr) dr.classList.remove('open');
  }

  function navDrawer(delta) {
    const actions = filteredActions();
    if (!actions.length) return;
    S.drawerIndex = (S.drawerIndex + delta + actions.length) % actions.length;
    paintDrawer();
  }

  function paintDrawer() {
    const dr = q('scDrawer');
    if (!dr) return;
    const actions = filteredActions();
    const a = actions[S.drawerIndex];
    if (!a) return;
    const evidence = a.evidence ? JSON.stringify(a.evidence, null, 2) : (a.detail || '');
    const buttons = [
      { id: 'upgrade', label: tr('sc_upgrade', 'Upgrade Package'), cls: 'primary', icon: 'package' },
      { id: 'firewall', label: tr('sc_firewall_action', 'Firewall Action'), cls: '', icon: 'shield' },
      { id: 'accept_fim', label: tr('sc_accept_fim', 'Accept FIM Baseline'), cls: 'danger', icon: 'file-check' },
      { id: 'open_source', label: tr('sc_open_source', 'Open Source File'), cls: '', icon: 'file-code' },
      { id: 'ask_ai', label: tr('sc_ask_ai', 'Ask AI Agent to Remediate'), cls: '', icon: 'sparkles' }
    ];
    dr.innerHTML = '' +
      '<div class="sc-drawer-head">' +
        '<div style="flex:1;"><div class="sc-finding-title">' + esc(a.title || a.id) + '</div>' +
        '<div class="sc-finding-detail">' + esc(a.pillar || '') + (a.host_name ? ' · ' + esc(a.host_name) : '') + '</div></div>' +
        '<div class="sc-drawer-nav">' +
          '<button type="button" data-sc-action="drawer-prev" aria-label="' + esc(tr('sc_prev', 'Previous finding')) + '">' + ic('chevron-up') + '</button>' +
          '<button type="button" data-sc-action="drawer-next" aria-label="' + esc(tr('sc_next', 'Next finding')) + '">' + ic('chevron-down') + '</button>' +
          '<button type="button" data-sc-action="close-drawer" aria-label="' + esc(tr('sc_close', 'Close')) + '">' + ic('x') + '</button>' +
        '</div>' +
      '</div>' +
      '<div class="sc-drawer-body">' +
        '<div class="sc-drawer-section"><h4>' + esc(tr('sc_evidence', 'Technical evidence')) + '</h4>' +
          '<pre class="sc-evidence">' + esc(evidence) + '</pre></div>' +
        '<div class="sc-drawer-section"><h4>' + esc(tr('sc_remediation', 'Remediation')) + '</h4>' +
          '<div class="sc-remediation">' + buttons.map(function (b) {
            return '<button type="button" class="' + b.cls + '" data-sc-action="remediation" data-remediation="' + b.id + '">' + ic(b.icon) + esc(b.label) + '</button>';
          }).join('') + '</div></div>' +
      '</div>' +
      '<div class="sc-drawer-foot">' + (S.drawerIndex + 1) + ' / ' + actions.length + ' · Esc / ↑ / ↓</div>';
  }

  function remediate(kind) {
    const actions = filteredActions();
    const a = actions[S.drawerIndex];
    if (!a) return;
    const labels = {
      upgrade: tr('sc_upgrade', 'Upgrade Package'),
      firewall: tr('sc_firewall_action', 'Firewall Action'),
      accept_fim: tr('sc_accept_fim', 'Accept FIM Baseline'),
      open_source: tr('sc_open_source', 'Open Source File'),
      ask_ai: tr('sc_ask_ai', 'Ask AI Agent to Remediate')
    };
    if (kind === 'ask_ai') {
      closeDrawer();
      if (window.AG && typeof AG.askSecurity === 'function' && a.host_id) { AG.askSecurity(a.host_id); return; }
      toastMsg(labels[kind]);
      return;
    }
    confirmMsg(tr('sc_confirm_action', 'Confirm security action') + ': ' + labels[kind]).then(function (ok) {
      if (!ok) return;
      toastMsg(labels[kind] + ' · ' + tr('sc_action_done', 'Action requested.'));
    });
  }

  // Public API.
  window.SC = {
    mountHost: mountHost,
    refresh: loadOverview,
    openDrawer: openDrawer,
    closeDrawer: closeDrawer
  };
})();
