/* Known vulnerabilities of the installed packages (OSV / Arch security tracker) for the Security tab.
 * The data comes from GET /api/hosts/{id}/security/vulns (only when enabled in Settings).
 * Relies on index.html globals: svgIcon, escapeHtml, t, I18N, VI. */
(function () {
  'use strict';

  const OV = (window.OV = {});
  let state = { hostID: null, data: null, filter: '', showUnfixed: false };

  const esc = (s) => escapeHtml(String(s === null || s === undefined ? '' : s));
  const icon = (n) => svgIcon(n, 'icon-sm');
  const fill = (s, vars) => Object.keys(vars).reduce((a, k) => a.split('{' + k + '}').join(vars[k]), s);

  const I = {
    en: {
      ov_title: 'Known vulnerabilities in installed packages',
      ov_off: 'Off. Turn on "Known vulnerabilities in installed packages" in Settings → General to check the installed packages against the OSV database (api.osv.dev) and the Arch security tracker. The Pantau server then sends the package names, versions and OS release to those public services.',
      ov_not_assessed: 'Not assessed: {reason}',
      ov_no_host: 'No host selected.',
      ov_empty: 'No known vulnerability found in the {n} packages checked ({eco}).',
      ov_checking: 'Checking installed packages against the vulnerability feeds (can take up to a minute)',
      ov_summary: '{n} finding(s) · {fix} with a fix available · {nofix} without · {n2} packages checked · {eco} ({src})',
      ov_count: '{n} findings',
      ov_filter_ph: 'Filter packages, CVEs or summaries...',
      ov_show_unfixed: 'Include findings without a fix',
      ov_th_pkg: 'Package',
      ov_th_installed: 'Installed',
      ov_th_fixed: 'Fixed',
      ov_th_sev: 'Severity',
      ov_th_cve: 'CVE / Exploitation',
      ov_no_fix: 'no fix',
      ov_notes: 'Notes',
      ov_source_osv: 'OSV',
      ov_source_arch: 'Arch security tracker'
    },
    id: {
      ov_title: 'Kerentanan yang diketahui pada paket terpasang',
      ov_off: 'Nonaktif. Aktifkan "Kerentanan yang diketahui pada paket terpasang" di Pengaturan → Umum untuk memeriksa paket terpasang terhadap basis data OSV (api.osv.dev) dan pelacak keamanan Arch. Server Pantau lalu mengirim nama paket, versi, dan rilis OS ke layanan publik tersebut.',
      ov_not_assessed: 'Tidak dinilai: {reason}',
      ov_no_host: 'Tidak ada host yang dipilih.',
      ov_empty: 'Tidak ada kerentanan yang diketahui pada {n} paket yang diperiksa ({eco}).',
      ov_checking: 'Memeriksa paket terpasang terhadap feed kerentanan (bisa sampai satu menit)',
      ov_summary: '{n} temuan · {fix} ada versi perbaikan · {nofix} tidak ada · {n2} paket diperiksa · {eco} ({src})',
      ov_count: '{n} temuan',
      ov_filter_ph: 'Saring paket, CVE, atau ringkasan...',
      ov_show_unfixed: 'Sertakan temuan tanpa versi perbaikan',
      ov_th_pkg: 'Paket',
      ov_th_installed: 'Terpasang',
      ov_th_fixed: 'Diperbaiki',
      ov_th_sev: 'Tingkat keparahan',
      ov_th_cve: 'CVE / Eksploitasi',
      ov_no_fix: 'tanpa perbaikan',
      ov_notes: 'Catatan',
      ov_source_osv: 'OSV',
      ov_source_arch: 'Pelacak keamanan Arch'
    }
  };
  ['en', 'id'].forEach((l) => { I18N[l] = Object.assign(I18N[l] || {}, I[l]); });

  const sevClass = (s) => (['critical', 'high', 'medium', 'low'].indexOf(s) >= 0 ? s : 'unknown');

  // OV.html returns the card markup; index.html places it below the exploit intelligence card.
  OV.html = function () {
    return `<div class="card" id="secOsvVulnsCard" style="padding: 16px; margin-top: 16px;">
      <div style="display: flex; justify-content: space-between; align-items: center; gap: 8px; margin-bottom: 10px; flex-wrap: wrap;">
        <h4 style="margin: 0; display: flex; align-items: center; gap: 6px;">${icon('shield-alert')} <span>${esc(t('ov_title'))}</span></h4>
      </div>
      <div id="osvVulnsBody"><div class="osv-empty">${esc(t('loading', 'Loading...'))}</div></div>
    </div>`;
  };

  // OV.render loads the data of the host currently shown in the Security tab.
  OV.render = function (hostID) {
    state.hostID = hostID;
    const el = document.getElementById('osvVulnsBody');
    if (!el) return;
    if (!hostID) {
      el.innerHTML = `<div class="osv-empty">${esc(t('ov_no_host'))}</div>`;
      return;
    }
    el.innerHTML = `<div class="osv-empty">${SP.html(t('ov_checking'))}</div>`;
    OV.load();
  };

  OV.load = async function () {
    const hostID = state.hostID;
    if (!hostID) return;
    const url = '/api/hosts/' + hostID + '/security/vulns' + (state.showUnfixed ? '?unfixed=1' : '');
    let data;
    try {
      const res = await fetch(url);
      data = res.ok ? await res.json() : { enabled: false, error: 'HTTP ' + res.status };
    } catch (e) {
      data = { enabled: false, error: e.message };
    }
    if (hostID !== state.hostID) return; // the operator switched hosts meanwhile
    state.data = data;
    OV.paint();
  };

  OV.paint = function () {
    const el = document.getElementById('osvVulnsBody');
    if (!el) return;
    const d = state.data || {};
    if (!d.enabled) {
      el.innerHTML = `<div class="osv-empty">${esc(t('ov_off'))}</div>`;
      return;
    }
    if (d.error) {
      el.innerHTML = `<div class="osv-empty" style="color: var(--danger);">${esc(d.error)}</div>`;
      return;
    }
    if (d.status === 'not_assessed') {
      const reason = (d.notes || []).join('; ');
      el.innerHTML = `<div class="osv-empty">${esc(reason ? fill(t('ov_not_assessed'), { reason }) : t('ov_not_assessed', '').replace('{reason}', ''))}</div>`;
      return;
    }

    const all = d.vulns || [];
    const eco = d.ecosystem || '';
    const src = d.source === 'arch' ? t('ov_source_arch') : t('ov_source_osv');
    if (!all.length) {
      el.innerHTML = `<div class="osv-empty">${esc(fill(t('ov_empty'), { n: d.checked_packages || 0, eco }))}</div>`;
      return;
    }

    const q = state.filter.toLowerCase();
    const list = all.filter((v) => !q || [v.package, v.installed_version, v.fixed_version, v.severity, v.summary,
      (v.cves || []).join(' '), (v.ids || []).join(' ')].join(' ').toLowerCase().includes(q));

    const intel = d.intel || null;
    const rows = list.map((v) => {
      const fixed = v.fixed_version ? esc(v.fixed_version) : `<span class="osv-nofix">${esc(t('ov_no_fix'))}</span>`;
      const cveCell = (window.VI && v.cves && v.cves.length)
        ? window.VI.findingCell({ cves: v.cves }, intel)
        : `<span style="color: var(--text-muted);">-</span>`;
      const summary = v.summary ? `<div class="osv-summary" title="${esc(v.summary)}">${esc(v.summary)}</div>` : '';
      return `<tr>
        <td><b>${esc(v.package)}</b>${summary}</td>
        <td class="dk-mono">${esc(v.installed_version)}</td>
        <td class="dk-mono">${fixed}</td>
        <td><span class="badge badge-sec-${sevClass((v.severity || '').toLowerCase())}">${esc((v.severity || 'unknown').toLowerCase())}</span></td>
        <td>${cveCell}</td>
      </tr>`;
    }).join('');

    el.innerHTML = `
      <div class="osv-bar">
        <input type="search" id="osvVulnsFilter" value="${esc(state.filter)}" placeholder="${esc(t('ov_filter_ph'))}" autocomplete="off" spellcheck="false" oninput="OV.filterRows(this.value)" />
        <label class="osv-toggle"><input type="checkbox" ${state.showUnfixed ? 'checked' : ''} onchange="OV.toggleUnfixed(this.checked)" /> <span>${esc(t('ov_show_unfixed'))}</span></label>
        <span class="osv-count">${esc(fill(t('ov_count'), { n: all.length }))}</span>
      </div>
      <table class="osv-table">
        <thead><tr>
          <th>${esc(t('ov_th_pkg'))}</th>
          <th>${esc(t('ov_th_installed'))}</th>
          <th>${esc(t('ov_th_fixed'))}</th>
          <th>${esc(t('ov_th_sev'))}</th>
          <th>${esc(t('ov_th_cve'))}</th>
        </tr></thead>
        <tbody>${rows || `<tr><td colspan="5" style="color: var(--text-muted);">-</td></tr>`}</tbody>
      </table>
      <div class="osv-meta">${esc(fill(t('ov_summary'), {
        n: all.length,
        fix: all.filter((v) => v.fixed_version).length,
        nofix: d.unfixed_count || 0,
        n2: d.checked_packages || 0,
        eco, src
      }))}</div>
      ${(d.notes || []).length ? `<div class="osv-notes"><b>${esc(t('ov_notes'))}</b><ul>${(d.notes || []).map((n) => `<li>${esc(n)}</li>`).join('')}</ul></div>` : ''}`;
  };

  OV.filterRows = function (v) {
    state.filter = v;
    OV.paint();
    const el = document.getElementById('osvVulnsFilter');
    if (el) { el.focus(); el.setSelectionRange(el.value.length, el.value.length); }
  };

  OV.toggleUnfixed = function (on) {
    state.showUnfixed = on;
    OV.load();
  };
})();
