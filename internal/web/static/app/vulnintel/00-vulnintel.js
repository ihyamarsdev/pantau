/* Exploit intelligence (CISA KEV + EPSS) for Security tab findings, and the vulnerability database reference list.
 * The data comes with GET /api/hosts/{id}/security as `intel` (only when enabled in Settings) and from
 * GET /api/security/vuln-databases. Relies on index.html globals: svgIcon, escapeHtml, t, I18N, openModal,
 * closeModal, showToast. */
(function () {
  'use strict';

  const VI = (window.VI = {});
  let dbData = null;
  let dbFilter = '';

  const esc = (s) => escapeHtml(String(s === null || s === undefined ? '' : s));
  const icon = (n) => svgIcon(n, 'icon-sm');
  const fill = (s, vars) => Object.keys(vars).reduce((a, k) => a.split('{' + k + '}').join(vars[k]), s);
  const pct = (x) => (x * 100).toFixed(x >= 0.1 ? 1 : 2) + '%';
  const HIGH_EPSS = 0.1;

  const I = {
    en: {
      vi_title: 'Exploit intelligence (CISA KEV + EPSS)',
      vi_off: 'Off. Turn on "CISA KEV and EPSS" in Settings → General to mark findings whose CVEs are exploited in the wild (CISA KEV) and their probability of exploitation (EPSS). The Pantau server then downloads these public feeds once a day.',
      vi_no_cves: 'Not assessed: {pm} does not report CVE IDs for pending updates. Only dnf and yum hosts are covered for now.',
      vi_no_findings: 'No pending security updates, nothing to assess.',
      vi_summary: '{kev} of {n} CVEs are in CISA KEV (exploited in the wild) · EPSS of 10% or more: {hi}.',
      vi_sources: 'CISA KEV catalog {kev} · EPSS {epss}',
      vi_dbs: 'Vulnerability databases',
      vi_kev: 'Exploited (KEV)',
      vi_kev_tip: '{name}. In CISA KEV since {added} (US federal remediation due {due}).',
      vi_ransom: 'Known use in ransomware campaigns.',
      vi_epss_tip: 'EPSS: {p} probability of exploitation in the next 30 days (higher than {pct} of all CVEs).',
      vi_th: 'CVE / Exploitation',
      vi_sort: 'Exploitation: KEV, then EPSS',
      db_title: 'Vulnerability databases', db_filter_ph: 'Filter by name, maintainer or description', db_count: '{n} databases',
      db_used: 'Used by Pantau', db_api: 'API', db_free: 'Free', db_paid: 'Paid / partial', db_open: 'Open',
      db_source: 'List from {repo}, licensed {license}; snapshot {date}, shown unmodified. "Used by Pantau" is added by Pantau.',
      db_load_failed: 'Could not load the database list',
      db_cat_general: 'Vulnerability databases', db_cat_vendor: 'Vendor / OS advisories', db_cat_feed: 'Feeds & APIs',
      db_cat_aggregator: 'Aggregators', db_cat_exploits: 'Exploit archives', db_cat_ics: 'ICS / OT / SCADA'
    },
    id: {
      vi_title: 'Intelijen eksploitasi (CISA KEV + EPSS)',
      vi_off: 'Nonaktif. Aktifkan "CISA KEV dan EPSS" di Pengaturan → Umum untuk menandai temuan yang CVE-nya sedang dieksploitasi (CISA KEV) beserta peluang eksploitasinya (EPSS). Server Pantau lalu mengunduh feed publik ini sekali sehari.',
      vi_no_cves: 'Tidak dinilai: {pm} tidak melaporkan ID CVE untuk pembaruan tertunda. Saat ini hanya host dnf dan yum yang tercakup.',
      vi_no_findings: 'Tidak ada pembaruan keamanan tertunda, tidak ada yang dinilai.',
      vi_summary: '{kev} dari {n} CVE ada di CISA KEV (sedang dieksploitasi) · EPSS 10% atau lebih: {hi}.',
      vi_sources: 'Katalog CISA KEV {kev} · EPSS {epss}',
      vi_dbs: 'Database kerentanan',
      vi_kev: 'Dieksploitasi (KEV)',
      vi_kev_tip: '{name}. Masuk CISA KEV sejak {added} (tenggat perbaikan federal AS {due}).',
      vi_ransom: 'Diketahui dipakai dalam kampanye ransomware.',
      vi_epss_tip: 'EPSS: peluang {p} dieksploitasi dalam 30 hari ke depan (lebih tinggi dari {pct} seluruh CVE).',
      vi_th: 'CVE / Eksploitasi',
      vi_sort: 'Eksploitasi: KEV, lalu EPSS',
      db_title: 'Database kerentanan', db_filter_ph: 'Saring menurut nama, pengelola, atau deskripsi', db_count: '{n} database',
      db_used: 'Dipakai Pantau', db_api: 'API', db_free: 'Gratis', db_paid: 'Berbayar / sebagian', db_open: 'Buka',
      db_source: 'Daftar dari {repo}, berlisensi {license}; snapshot {date}, ditampilkan tanpa perubahan. Label "Dipakai Pantau" ditambahkan oleh Pantau.',
      db_load_failed: 'Daftar database tidak dapat dimuat',
      db_cat_general: 'Database kerentanan', db_cat_vendor: 'Advisori vendor / OS', db_cat_feed: 'Feed & API',
      db_cat_aggregator: 'Agregator', db_cat_exploits: 'Arsip exploit', db_cat_ics: 'ICS / OT / SCADA'
    }
  };
  ['en', 'id'].forEach((l) => { I18N[l] = Object.assign(I18N[l] || {}, I[l]); });

  const infoOf = (intel, cve) => (intel && intel.enabled && intel.cves ? intel.cves[cve] : null);

  // Sort key of a finding: KEV first, then the highest EPSS among its CVEs.
  VI.score = function (f, intel) {
    let best = 0;
    (f.cves || []).forEach((c) => {
      const i = infoOf(intel, c);
      if (i) best = Math.max(best, (i.kev ? 2 : 0) + (i.epss || 0));
    });
    return best;
  };

  VI.findingCell = function (f, intel) {
    const cves = f.cves || [];
    if (!cves.length) return '<span style="color: var(--text-muted);">-</span>';
    const row = (c) => {
      const i = infoOf(intel, c) || {};
      let badges = '';
      if (i.kev) {
        const tip = fill(t('vi_kev_tip'), { name: i.kev.name, added: i.kev.date_added, due: i.kev.due_date }) + (i.kev.ransomware ? ' ' + t('vi_ransom') : '');
        badges += ` <span class="badge badge-danger" title="${esc(tip)}">${esc(t('vi_kev'))}${i.kev.ransomware ? ' ⚠' : ''}</span>`;
      }
      if (i.epss !== undefined) {
        badges += ` <span class="badge ${i.epss >= HIGH_EPSS ? 'badge-warning' : 'badge-unknown'}" title="${esc(fill(t('vi_epss_tip'), { p: pct(i.epss), pct: pct(i.percentile || 0) }))}">EPSS ${esc(pct(i.epss))}</span>`;
      }
      return `<div style="white-space: nowrap;"><a class="dk-mono" href="https://nvd.nist.gov/vuln/detail/${esc(c)}" target="_blank" rel="noopener noreferrer">${esc(c)}</a>${badges}</div>`;
    };
    // Exploited and likely-exploited CVEs first, so they stay visible when the list is long.
    const sorted = cves.slice().sort((a, b) => VI.score({ cves: [b] }, intel) - VI.score({ cves: [a] }, intel));
    return `<div style="display: flex; flex-direction: column; gap: 2px; font-size: 0.78rem;">${sorted.map(row).join('')}</div>`;
  };

  VI.cardHtml = function (scan, intel) {
    const btn = `<button class="btn btn-sm btn-ghost" onclick="VI.openDatabases()">${icon('book-open')} <span>${esc(t('vi_dbs'))}</span></button>`;
    let body;
    if (intel === undefined) { // no scan loaded (error or never scanned): only the database list button
      body = '';
    } else if (!intel || !intel.enabled) {
      body = `<div style="color: var(--text-muted); font-size: 0.85rem;">${esc(t('vi_off'))}</div>`;
    } else if (!scan || !(scan.findings || []).length) {
      body = `<div style="color: var(--text-muted); font-size: 0.85rem;">${esc(t('vi_no_findings'))}</div>`;
    } else if (!intel.checked_cves) {
      body = `<div style="color: var(--text-muted); font-size: 0.85rem;">${esc(fill(t('vi_no_cves'), { pm: scan.package_manager || '?' }))}</div>`;
    } else {
      const infos = Object.values(intel.cves || {});
      const kev = infos.filter((i) => i.kev).length;
      const hi = infos.filter((i) => (i.epss || 0) >= HIGH_EPSS).length;
      const errs = [intel.kev_error, intel.epss_error].filter(Boolean);
      body = `<div style="font-size: 0.85rem; color: ${kev ? 'var(--danger)' : 'var(--text)'}; font-weight: ${kev ? 600 : 400};">${esc(fill(t('vi_summary'), { kev, n: intel.checked_cves, hi }))}</div>
        <div style="font-size: var(--fs-xs); color: var(--text-muted); margin-top: 4px;">${esc(fill(t('vi_sources'), { kev: (intel.kev_catalog || '-').slice(0, 10), epss: intel.epss_date || '-' }))}</div>
        ${errs.length ? `<div style="font-size: var(--fs-xs); color: var(--warning); margin-top: 6px;">${errs.map(esc).join('<br>')}</div>` : ''}`;
    }
    return `<div class="card" id="secIntelCard" style="padding: 16px; margin-top: 16px;">
      <div style="display: flex; justify-content: space-between; align-items: center; gap: 8px; margin-bottom: 10px; flex-wrap: wrap;">
        <h4 style="margin: 0; display: flex; align-items: center; gap: 6px;">${icon('zap')} <span>${esc(t('vi_title'))}</span></h4>${btn}
      </div>${body}</div>`;
  };

  /* ---------- vulnerability database list ---------- */
  const CATS = ['general', 'vendor', 'feed', 'aggregator', 'exploits', 'ics'];

  function dbRow(d, used) {
    const isUsed = used.includes(d.abbreviation);
    const badges = [
      isUsed ? `<span class="badge badge-success">${esc(t('db_used'))}</span>` : '',
      d.has_api ? `<span class="badge badge-info">${esc(t('db_api'))}</span>` : '',
      `<span class="badge ${d.free ? 'badge-unknown' : 'badge-warning'}">${esc(t(d.free ? 'db_free' : 'db_paid'))}</span>`
    ].join(' ');
    return `<div class="vi-db">
      <div class="vi-db-main">
        <div class="vi-db-title"><a href="${esc(d.url)}" target="_blank" rel="noopener noreferrer">${esc(d.name)}</a>${d.abbreviation && d.abbreviation !== d.name ? ` <span class="text-muted dk-mono">${esc(d.abbreviation)}</span>` : ''} ${badges}</div>
        <div class="vi-db-desc">${esc(d.description)}</div>
        <div class="vi-db-meta">${esc(d.maintainer)} · ${esc(d.region)}${d.api_url ? ` · <span class="dk-mono">${esc(d.api_url)}</span>` : ''}</div>
      </div>
    </div>`;
  }

  function renderDatabases() {
    const body = document.getElementById('viDbBody');
    if (!body) return;
    if (!dbData) { body.innerHTML = `<div class="vi-empty">${esc(t('loading', 'Loading...'))}</div>`; return; }
    if (dbData.error) { body.innerHTML = `<div class="vi-empty" style="color: var(--danger);">${esc(t('db_load_failed'))}: ${esc(dbData.error)}</div>`; return; }
    const q = dbFilter.toLowerCase();
    const list = (dbData.databases || []).filter((d) => !q || [d.name, d.abbreviation, d.maintainer, d.description, d.region].join(' ').toLowerCase().includes(q));
    const used = dbData.used_by_pantau || [];
    const groups = CATS.concat([...new Set(list.map((d) => d.category))].filter((c) => !CATS.includes(c)))
      .map((c) => [c, list.filter((d) => d.category === c)]).filter(([, ds]) => ds.length);
    const src = dbData.source || {};
    body.innerHTML = `
      <div class="vi-bar">
        <input type="search" id="viDbFilter" value="${esc(dbFilter)}" placeholder="${esc(t('db_filter_ph'))}" autocomplete="off" spellcheck="false" oninput="VI.filterDatabases(this.value)" />
        <span class="text-muted" style="font-size: var(--fs-xs);">${esc(fill(t('db_count'), { n: list.length }))}</span>
      </div>
      ${groups.map(([c, ds]) => `<div class="vi-db-group">${esc(t('db_cat_' + c, c))}</div>${ds.map((d) => dbRow(d, used)).join('')}`).join('')}
      <div class="vi-db-source">${fill(esc(t('db_source')), {
        repo: `<a href="${esc(src.url)}" target="_blank" rel="noopener noreferrer">${esc(src.name)}</a>`,
        license: `<a href="${esc(src.license_url)}" target="_blank" rel="noopener noreferrer">${esc(src.license)}</a>`,
        date: esc(src.date + ' (' + src.commit + ')')
      })}</div>`;
  }

  VI.filterDatabases = function (v) {
    dbFilter = v;
    renderDatabases();
    const el = document.getElementById('viDbFilter');
    if (el) { el.focus(); el.setSelectionRange(el.value.length, el.value.length); }
  };

  VI.openDatabases = async function () {
    if (!document.getElementById('vulnDbModal')) {
      const m = document.createElement('div');
      m.id = 'vulnDbModal';
      m.className = 'modal';
      m.innerHTML = `
        <div class="modal-content modal-workspace">
          <div class="modal-header">
            <div class="sd-title">${icon('book-open')} <h3>${esc(t('db_title'))}</h3></div>
            <button class="btn btn-icon btn-sm btn-ghost" onclick="closeModal('vulnDbModal')">${icon('x')}</button>
          </div>
          <div class="modal-body" id="viDbBody"></div>
        </div>`;
      document.body.appendChild(m);
    }
    renderDatabases();
    openModal('vulnDbModal');
    if (dbData && !dbData.error) return;
    try {
      const res = await fetch('/api/security/vuln-databases');
      dbData = res.ok ? await res.json() : { error: 'HTTP ' + res.status };
    } catch (e) {
      dbData = { error: e.message };
    }
    renderDatabases();
  };
})();
