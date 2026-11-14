/* Container misconfiguration audit and image vulnerability scan for the Docker tab.
 * The data comes from GET /api/hosts/{id}/docker/security (stored results) and the two POST endpoints
 * (audit, scan-image). Relies on index.html globals: currentHost, currentTab, dockerSubTab, loadDockerTab,
 * svgIcon, escapeHtml, t, I18N, openModal, closeModal, showToast, window.VI (exploit intelligence). */
(function () {
  'use strict';

  const DS = (window.DS = {});
  const S = { hostID: 0, loaded: false, audit: null, images: [], intel: null, busy: false, filter: '', error: '' };
  let filterTimer = null;

  const esc = (s) => escapeHtml(String(s === null || s === undefined ? '' : s));
  const icon = (n) => svgIcon(n, 'icon-sm');
  const fill = (s, vars) => Object.keys(vars).reduce((a, k) => a.split('{' + k + '}').join(vars[k]), s);
  const num = (n) => new Intl.NumberFormat().format(n || 0);

  const SEV_ORDER = ['critical', 'high', 'medium', 'low'];
  const SEV_CLASS = { critical: 'badge-danger', high: 'badge-warning', medium: 'badge-info', low: 'badge-unknown' };

  /* ---------- i18n ---------- */
  const I = {
    en: {
      ds_sub: 'Security', ds_title: 'Container and image security',
      ds_audit: 'Audit containers', ds_auditing: 'Auditing…', ds_scan_all: 'Scan all images',
      ds_job_audit: 'Auditing containers on the host', ds_job_scan: 'Scanning image', ds_job_scan_all: 'Scanning images {i} of {n}',
      ds_job_hint: 'The scanner runs on the host; large images can take several minutes.',
      ds_job_scan_all_prep: 'Collecting the images to scan',
      ds_scan_image: 'Scan image', ds_scanned: 'Scanned', ds_not_scanned: 'Not scanned',
      ds_audit_none: 'No audit yet. Run the audit to check the container configuration of this host.',
      ds_audit_note: 'Audited {t} · {n} finding(s) in {c} container(s)',
      ds_no_findings: 'No container matched a misconfiguration rule.',
      ds_col_container: 'Container', ds_col_image: 'Image', ds_col_rule: 'Rule', ds_col_severity: 'Severity',
      ds_col_detail: 'Detail', ds_col_remediation: 'Remediation',
      ds_filter_ph: 'Filter by container or rule',
      ds_critical: 'Critical', ds_high: 'High', ds_medium: 'Medium', ds_low: 'Low',
      ds_images_title: 'Image vulnerabilities',
      ds_images_none: 'No image scan yet. Scan an image to see the known vulnerabilities of its packages.',
      ds_col_counts: 'Vulnerabilities', ds_col_scanner: 'Scanner', ds_col_status: 'Status',
      ds_st_ok: 'Assessed', ds_st_partial: 'Partial', ds_st_unsupported: 'Not assessed', ds_st_error: 'Error',
      ds_st_unknown: 'Unknown',
      ds_vuln_title: 'Vulnerabilities of {image}',
      ds_vuln_none: 'The scanner reported no known vulnerability for this image.',
      ds_vuln_truncated: 'Only the most severe vulnerabilities are listed (the stored list is capped).',
      ds_col_cve: 'CVE', ds_col_pkg: 'Package', ds_col_installed: 'Installed', ds_col_fixed: 'Fixed',
      ds_scan_done: 'Scan of {ref} finished: {n} vulnerability(ies).',
      ds_scan_fail: 'Scan of {ref} failed: {why}',
      ds_scan_all_done: 'Scanned {n} image(s): {v} vulnerability(ies) in total.',
      ds_scan_missing: 'No vulnerability scanner is installed on this host (trivy or grype). Install one on the host to enable image scanning; Pantau never installs software.',
      ds_audit_done: 'Audit finished: {n} finding(s).',
      ds_audit_fail: 'Audit failed: {why}',
      ds_loading: 'Loading vulnerabilities…',
    },
    id: {
      ds_sub: 'Keamanan', ds_title: 'Keamanan container dan image',
      ds_audit: 'Audit container', ds_auditing: 'Mengaudit…', ds_scan_all: 'Pindai semua image',
      ds_job_audit: 'Mengaudit container di host', ds_job_scan: 'Memindai image', ds_job_scan_all: 'Memindai image {i} dari {n}',
      ds_job_hint: 'Pemindai berjalan di host; image besar dapat memakan beberapa menit.',
      ds_job_scan_all_prep: 'Mengumpulkan image yang akan dipindai',
      ds_scan_image: 'Pindai image', ds_scanned: 'Sudah dipindai', ds_not_scanned: 'Belum dipindai',
      ds_audit_none: 'Belum ada audit. Jalankan audit untuk memeriksa konfigurasi container di host ini.',
      ds_audit_note: 'Diaudit {t} · {n} temuan pada {c} container',
      ds_no_findings: 'Tidak ada container yang cocok dengan aturan miskonfigurasi.',
      ds_col_container: 'Container', ds_col_image: 'Image', ds_col_rule: 'Aturan', ds_col_severity: 'Tingkat',
      ds_col_detail: 'Rincian', ds_col_remediation: 'Perbaikan',
      ds_filter_ph: 'Saring menurut container atau aturan',
      ds_critical: 'Kritis', ds_high: 'Tinggi', ds_medium: 'Sedang', ds_low: 'Rendah',
      ds_images_title: 'Kerentanan image',
      ds_images_none: 'Belum ada pindai image. Pindai sebuah image untuk melihat kerentanan paket-paketnya.',
      ds_col_counts: 'Kerentanan', ds_col_scanner: 'Scanner', ds_col_status: 'Status',
      ds_st_ok: 'Dinilai', ds_st_partial: 'Sebagian', ds_st_unsupported: 'Belum dinilai', ds_st_error: 'Galat',
      ds_st_unknown: 'Tidak diketahui',
      ds_vuln_title: 'Kerentanan {ref}',
      ds_vuln_none: 'Scanner tidak melaporkan kerentanan yang dikenal untuk image ini.',
      ds_vuln_truncated: 'Hanya kerentanan paling berat yang dicantumkan (daftar tersimpan dibatasi).',
      ds_col_cve: 'CVE', ds_col_pkg: 'Paket', ds_col_installed: 'Terpasang', ds_col_fixed: 'Diperbaiki',
      ds_scan_done: 'Pindai {ref} selesai: {n} kerentanan.',
      ds_scan_fail: 'Pindai {ref} gagal: {why}',
      ds_scan_all_done: '{n} image dipindai: total {v} kerentanan.',
      ds_scan_missing: 'Tidak ada scanner kerentanan di host ini (trivy atau grype). Pasang salah satunya di host untuk mengaktifkan pemindaian image; Pantau tidak pernah memasang perangkat lunak.',
      ds_audit_done: 'Audit selesai: {n} temuan.',
      ds_audit_fail: 'Audit gagal: {why}',
      ds_loading: 'Memuat kerentanan…',
    }
  };
  ['en', 'id'].forEach((l) => { I18N[l] = Object.assign(I18N[l] || {}, I[l]); });

  /* ---------- data ---------- */
  async function api(url, opts) {
    try {
      const res = await fetch(url, opts);
      const data = await res.json().catch(() => ({}));
      if (!res.ok) return { ok: false, error: data.error || ('HTTP ' + res.status) };
      if (data.ok === false) return { ok: false, error: data.error || 'request failed' };
      return { ok: true, data };
    } catch (e) {
      return { ok: false, error: e.message };
    }
  }

  // Load the stored results of a host (audit + image scans) into the cache.
  DS.load = async function (hostID) {
    if (!hostID) return null;
    // A different host: drop the cached results so no stale data of the previous host is shown.
    if (S.hostID !== hostID) { S.audit = null; S.images = []; S.intel = null; S.loaded = false; }
    S.hostID = hostID;
    const r = await api('/api/hosts/' + hostID + '/docker/security');
    if (S.hostID !== hostID) return null; // the user switched to another host in the meantime
    if (!r.ok) { S.error = r.error; S.audit = null; S.images = []; S.loaded = false; return null; }
    S.error = '';
    S.audit = r.data.audit || null;
    S.images = r.data.images || [];
    S.intel = r.data.intel || null;
    S.loaded = true;
    return r.data;
  };

  // True when the stored results of this host are already in the cache.
  DS.hasData = function (hostID) { return S.loaded && S.hostID === hostID; };

  // Refresh the cache and repaint the chips in the Images list.
  DS.refresh = async function () {
    if (!currentHost) return null;
    const data = await DS.load(currentHost.id);
    paintChips();
    return data;
  };

  const scanOf = (ref) => S.images.find((s) => s.subject === ref) || null;

  /* ---------- chips in the Images list ---------- */
  function paintChips() {
    document.querySelectorAll('.ds-chips').forEach((el) => {
      el.innerHTML = chipsHtml(scanOf(el.dataset.ref));
    });
  }

  // Repaint the chips of the Images list (the list is rendered before the scan data arrives).
  DS.paintChips = paintChips;

  function chipsHtml(scan) {
    if (!scan) return `<span class="badge badge-unknown" style="text-transform: none;">${esc(t('ds_not_scanned'))}</span>`;
    if (scan.status === 'unsupported') {
      return `<span class="badge badge-unknown" style="text-transform: none;" title="${esc(scan.note || '')}">${esc(t('ds_st_unsupported'))}</span>`;
    }
    if (scan.status === 'error') {
      return `<span class="badge badge-danger" style="text-transform: none;" title="${esc(scan.error || '')}">${esc(t('ds_st_error'))}</span>`;
    }
    const c = scan.vuln_counts || {};
    const parts = [];
    SEV_ORDER.forEach((sev) => {
      if (c[sev]) parts.push(`<span class="badge ${SEV_CLASS[sev]}" style="text-transform: none;">${num(c[sev])} ${esc(t('ds_' + sev))}</span>`);
    });
    if (c.unknown) parts.push(`<span class="badge badge-unknown" style="text-transform: none;">${num(c.unknown)} ${esc(t('ds_st_unknown'))}</span>`);
    if (!parts.length) return `<span class="badge badge-success" style="text-transform: none;">${esc(t('ds_scanned'))}</span>`;
    return parts.join(' ');
  }

  // Chips in the Images list: the vulnerability counts of the stored scan of that image.
  DS.chips = function (ref) {
    return `<span class="ds-chips" data-ref="${esc(ref)}" onclick="DS.openImage(this.dataset.ref)">${chipsHtml(scanOf(ref))}</span>`;
  };

  // Scan image button of the Images list.
  DS.scanButton = function (ref) {
    return `<button class="btn btn-sm btn-ghost btn-icon" title="${esc(t('ds_scan_image'))}" data-ref="${esc(ref)}" onclick="DS.scanImage(this.dataset.ref, this)">${icon('shield')}</button>`;
  };

  /* ---------- panel ---------- */
  function sevBadge(sev) {
    if (sev === 'unknown') return `<span class="badge badge-unknown">${esc(t('ds_st_unknown'))}</span>`;
    return `<span class="badge ${SEV_CLASS[sev] || 'badge-unknown'}">${esc(t('ds_' + sev, sev))}</span>`;
  }

  // The part of the panel that depends on the filter (repainted in place, the input keeps the focus).
  function auditFindingsHtml(audit) {
    const findings = (audit.findings || []).filter((f) => {
      if (!S.filter) return true;
      const q = S.filter.toLowerCase();
      return (f.container || '').toLowerCase().includes(q) || (f.rule_id || '').toLowerCase().includes(q) ||
        (f.title || '').toLowerCase().includes(q);
    });
    const counts = audit.counts || {};
    const perContainer = {};
    findings.forEach((f) => { perContainer[f.container] = (perContainer[f.container] || 0) + 1; });
    const containerNames = Object.keys(perContainer).sort();

    const summary = `<div style="display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 10px;">
      ${SEV_ORDER.map((sev) => `<span class="badge ${SEV_CLASS[sev]}" style="text-transform: none;">${esc(t('ds_' + sev))}: ${num(counts[sev] || 0)}</span>`).join('')}
      ${containerNames.length ? `<span class="badge badge-info" style="text-transform: none;">${esc(t('ds_col_container'))}: ${num(containerNames.length)}</span>` : ''}
    </div>`;

    if (!findings.length) {
      return summary + `<div class="hub-empty" style="padding: 16px;">${esc(S.filter ? t('ds_no_findings') : (audit.note || t('ds_no_findings')))}</div>`;
    }
    const rows = findings.map((f) => `
      <tr>
        <td><b>${esc(f.container)}</b></td>
        <td style="font-family: var(--font-mono); font-size: var(--fs-xs); color: var(--text-muted);">${esc(f.image || '')}</td>
        <td>${esc(f.rule_id)}<div style="font-size: var(--fs-xs); color: var(--text-muted);">${esc(f.title || '')}</div></td>
        <td>${sevBadge(f.severity)}</td>
        <td style="font-size: var(--fs-xs);">${esc(f.detail || '')}</td>
        <td style="font-size: var(--fs-xs); color: var(--text-muted);">${esc(f.remediation || '')}</td>
      </tr>`).join('');
    return summary + `<div style="overflow-x: auto; margin-top: 8px;"><table>
      <thead><tr><th>${esc(t('ds_col_container'))}</th><th>${esc(t('ds_col_image'))}</th><th>${esc(t('ds_col_rule'))}</th><th>${esc(t('ds_col_severity'))}</th><th>${esc(t('ds_col_detail'))}</th><th>${esc(t('ds_col_remediation'))}</th></tr></thead>
      <tbody>${rows}</tbody></table></div>`;
  }

  function auditTableHtml(audit) {
    const filter = `<input type="search" id="dsFilter" value="${esc(S.filter)}" placeholder="${esc(t('ds_filter_ph'))}" maxlength="128" autocomplete="off" oninput="DS.filterFindings(this.value)" />`;
    return filter + `<div id="dsAuditTable">${auditFindingsHtml(audit)}</div>`;
  }

  function imageRowsHtml() {
    if (!S.images.length) return `<div class="hub-empty" style="padding: 16px;">${esc(t('ds_images_none'))}</div>`;
    return `<div style="overflow-x: auto;"><table>
      <thead><tr><th>${esc(t('ds_col_image'))}</th><th>${esc(t('ds_col_counts'))}</th><th>${esc(t('ds_col_scanner'))}</th><th>${esc(t('ds_col_status'))}</th><th></th></tr></thead>
      <tbody>${S.images.map((s) => `
        <tr>
          <td class="dk-mono" style="font-weight: 600;">${esc(s.subject)}</td>
          <td>${chipsHtml(s)}</td>
          <td class="dk-mono" style="font-size: var(--fs-xs);">${esc(s.scanner || '-')}</td>
          <td><span class="badge badge-unknown">${esc(t('ds_st_' + (s.status || 'ok'), s.status || 'ok'))}</span></td>
          <td class="dk-actions"><button class="btn btn-sm btn-ghost btn-icon" title="${esc(t('ds_scan_image'))}" data-ref="${esc(s.subject)}" onclick="DS.scanImage(this.dataset.ref, this)">${icon('shield')}</button></td>
        </tr>`).join('')}</tbody></table></div>`;
  }

  // The security panel of the Docker tab (the "Security" sub-tab).
  DS.panelHtml = function () {
    const a = S.audit || {};
    let auditBody;
    if (!a.scanned_at) {
      auditBody = `<div class="hub-empty" style="padding: 16px;">${esc(t('ds_audit_none'))}</div>`;
    } else if (a.status === 'unsupported') {
      auditBody = `<div class="hub-empty" style="padding: 16px;">${esc(t('ds_not_assessed'))}${a.note ? ': ' + esc(a.note) : ''}</div>`;
    } else if (a.status === 'error') {
      auditBody = `<div class="hub-empty" style="padding: 16px;">${esc(t('ds_st_error'))}: ${esc(a.error || '')}</div>`;
    } else {
      auditBody = `<div style="font-size: var(--fs-xs); color: var(--text-muted); margin-bottom: 8px;">${esc(fill(t('ds_audit_note'), {
        t: a.scanned_at ? new Date(a.scanned_at).toLocaleString() : '-',
        n: num((a.counts || {}).total || 0),
        c: num(new Set((a.findings || []).map((f) => f.container)).size)
      }))}</div>` + auditTableHtml(a);
    }
    const scanAll = `<button class="btn btn-sm btn-ghost" onclick="DS.scanAll()" ${S.busy ? 'disabled' : ''}>${icon('shield')} <span>${esc(t('ds_scan_all'))}</span></button>`;
    return `
      <div style="display: flex; justify-content: space-between; align-items: center; gap: 8px; flex-wrap: wrap; margin-bottom: 10px;">
        <h3 style="margin: 0; display: flex; align-items: center; gap: 6px;">${icon('shield')} <span>${esc(t('ds_title'))}</span></h3>
        <div class="dk-toolbar">
          ${scanAll}
          <button class="btn btn-sm btn-primary" onclick="DS.runAudit()" ${S.busy ? 'disabled' : ''}>${S.job && S.job.kind === 'audit' ? SP.html(t('ds_auditing'), S.job.t0) : icon('shield') + ' <span>' + esc(t('ds_audit')) + '</span>'}</button>
        </div>
      </div>
      <div id="dsJob">${jobHtml()}</div>
      ${auditBody}
      <h4 style="margin: 18px 0 8px; display: flex; align-items: center; gap: 6px;">${icon('package')} <span>${esc(t('ds_images_title'))}</span></h4>
      ${imageRowsHtml()}`;
  };

  // Filter the findings client-side: only the table is repainted, the tab is not reloaded (no flicker).
  DS.filterFindings = function (value) {
    S.filter = value.trim();
    clearTimeout(filterTimer);
    filterTimer = setTimeout(() => {
      const box = document.getElementById('dsAuditTable');
      if (box && S.audit && S.audit.scanned_at) box.innerHTML = auditFindingsHtml(S.audit);
      const el = document.getElementById('dsFilter');
      if (el) { el.focus(); el.setSelectionRange(el.value.length, el.value.length); }
    }, 350);
  };

  /* ---------- actions ---------- */
  // The running job (audit, one image scan, scan-all) is shown as a banner with elapsed time and done/total.
  function jobHtml() {
    const j = S.job;
    if (!j) return '';
    const text = j.kind === 'audit' ? t('ds_job_audit')
      : j.kind === 'scan' ? t('ds_job_scan')
      : j.kind === 'prep' ? t('ds_job_scan_all_prep')
      : fill(t('ds_job_scan_all'), { i: Math.min(j.done + 1, j.total), n: j.total });
    return SP.banner(text, j.t0, j.done, j.total, j.current) +
      `<div style="font-size: var(--fs-xs); color: var(--text-muted); margin: -6px 0 10px;">${esc(t('ds_job_hint'))}</div>`;
  }
  // The banner of a running job, for the tab loader (the panel is not rendered yet).
  DS.jobBannerHtml = jobHtml;
  function setJob(j) {
    S.job = j;
    const el = document.getElementById('dsJob');
    if (el) el.innerHTML = jobHtml();
  }

  DS.runAudit = async function () {
    const host = currentHost;
    if (!host || S.busy) return;
    S.busy = true;
    setJob({ kind: 'audit', t0: Date.now(), done: 0, total: 0, current: '' });
    if (typeof loadDockerTab === 'function') loadDockerTab();
    let r;
    try {
      r = await api('/api/hosts/' + host.id + '/docker/security/audit', { method: 'POST' });
    } finally {
      S.busy = false;
      setJob(null);
    }
    if (!r.ok) {
      showToast(fill(t('ds_audit_fail'), { why: r.error }), 'error', 9000);
      if (typeof loadDockerTab === 'function') loadDockerTab();
      return;
    }
    const a = r.data.audit || {};
    showToast(fill(t('ds_audit_done'), { n: num((a.counts || {}).total || 0) }),
      (a.counts && (a.counts.critical || a.counts.high)) ? 'warning' : 'success');
    await DS.refresh();
    if (typeof loadDockerTab === 'function') loadDockerTab();
  };

  DS.scanImage = async function (ref, btn) {
    if (!currentHost || !ref) return;
    const restore = SP.busy(btn, t('ds_job_scan'));
    setJob({ kind: 'scan', t0: Date.now(), done: 0, total: 0, current: ref });
    let r;
    try {
      r = await api('/api/hosts/' + currentHost.id + '/docker/security/scan-image', {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ image: ref })
      });
    } finally {
      restore();
      setJob(null);
    }
    if (!r.ok) {
      showToast(fill(t('ds_scan_fail'), { ref, why: r.error }), 'error', 9000);
      return;
    }
    const scan = r.data.scan || {};
    if (scan.status === 'unsupported') {
      showToast(t('ds_scan_missing'), 'warning', 10000);
    } else if (scan.status === 'error') {
      showToast(fill(t('ds_scan_fail'), { ref, why: scan.error || '' }), 'error', 9000);
    } else {
      showToast(fill(t('ds_scan_done'), { ref, n: num((scan.vuln_counts || {}).total || 0) }),
        (scan.vuln_counts && (scan.vuln_counts.critical || scan.vuln_counts.high)) ? 'warning' : 'success');
    }
    await DS.refresh();
    if (typeof loadDockerTab === 'function' && currentTab === 'docker' && (dockerSubTab === 'security' || dockerSubTab === 'images')) loadDockerTab();
  };

  // Scan every local image of the host (one after the other: the scanner is heavy).
  DS.scanAll = async function () {
    const host = currentHost;
    if (!host || S.busy) return;
    // The banner is shown from the first click, before the image list is fetched.
    S.busy = true;
    setJob({ kind: 'prep', t0: Date.now(), done: 0, total: 0, current: '' });
    if (typeof loadDockerTab === 'function') loadDockerTab();
    let imgs;
    try {
      imgs = await api('/api/hosts/' + host.id + '/docker/images');
      if (!imgs.ok || !imgs.data.images || !imgs.data.images.length) {
        showToast(t('ds_images_none'), 'info');
        return;
      }
      const list = imgs.data.images.filter((img) => !img.dangling);
      setJob({ kind: 'scanall', t0: Date.now(), done: 0, total: list.length, current: '' });
      if (typeof loadDockerTab === 'function') loadDockerTab();
      let total = 0, missing = false;
      for (const img of list) {
        const ref = img.repository + ':' + img.tag;
        setJob(Object.assign(S.job, { current: ref }));
        const r = await api('/api/hosts/' + host.id + '/docker/security/scan-image', {
          method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ image: ref })
        });
        if (!r.ok) { setJob(Object.assign(S.job, { done: S.job.done + 1 })); continue; }
        const scan = r.data.scan || {};
        if (scan.status === 'unsupported') { missing = true; break; }
        total += (scan.vuln_counts || {}).total || 0;
        setJob(Object.assign(S.job, { done: S.job.done + 1 }));
      }
      if (missing) showToast(t('ds_scan_missing'), 'warning', 10000);
      else showToast(fill(t('ds_scan_all_done'), { n: num(imgs.data.images.length), v: num(total) }), 'success');
    } finally {
      S.busy = false;
      setJob(null);
    }
    await DS.refresh();
    if (typeof loadDockerTab === 'function') loadDockerTab();
  };

  /* ---------- image vulnerability modal ---------- */
  function ensureModal() {
    if (document.getElementById('dockerSecModal')) return;
    const m = document.createElement('div');
    m.id = 'dockerSecModal';
    m.className = 'modal';
    m.innerHTML = `
      <div class="modal-content modal-workspace">
        <div class="modal-header">
          <div class="sd-title">
            ${icon('shield')}
            <h3 id="dockerSecTitle">${esc(t('ds_title'))}</h3>
          </div>
          <button class="btn btn-icon btn-sm btn-ghost" onclick="closeModal('dockerSecModal')">${icon('x')}</button>
        </div>
        <div class="modal-body" id="dockerSecBody"></div>
      </div>`;
    document.body.appendChild(m);
  }

  DS.openImage = async function (ref) {
    if (!ref) return;
    // The modal opens right away with a spinner; the scan data may still be loading.
    ensureModal();
    document.getElementById('dockerSecTitle').textContent = fill(t('ds_vuln_title'), { ref });
    document.getElementById('dockerSecBody').innerHTML = SP.loading(t('ds_loading'), Date.now());
    openModal('dockerSecModal');
    let scan = scanOf(ref);
    if (!scan) {
      await DS.refresh();
      scan = scanOf(ref);
    }
    if (!scan) {
      document.getElementById('dockerSecBody').innerHTML = `<div class="hub-empty" style="padding: 16px;">${esc(t('ds_images_none'))}</div>`;
      return;
    }
    document.getElementById('dockerSecBody').innerHTML = vulnTableHtml(scan);
  };

  function vulnTableHtml(scan) {
    const vulns = scan.vulns || [];
    if (!vulns.length) return `<div class="hub-empty" style="padding: 16px;">${esc(scan.note || t('ds_vuln_none'))}</div>`;
    const intel = S.intel || {};
    const rows = vulns.map((v) => {
      const cve = /^CVE-/i.test(v.id || '');
      const idCell = cve
        ? `<a class="dk-mono" href="https://nvd.nist.gov/vuln/detail/${esc(v.id)}" target="_blank" rel="noopener noreferrer">${esc(v.id)}</a>`
        : `<span class="dk-mono">${esc(v.id || '-')}</span>`;
      const intelCell = cve && window.VI ? VI.findingCell({ cves: [v.id] }, intel) : `<span style="color: var(--text-muted);">-</span>`;
      return `
        <tr>
          <td>${idCell}</td>
          <td>${intelCell}</td>
          <td class="dk-mono" style="font-size: var(--fs-xs);">${esc(v.package || '-')}</td>
          <td class="dk-mono" style="font-size: var(--fs-xs);">${esc(v.installed || '-')}</td>
          <td class="dk-mono" style="font-size: var(--fs-xs);">${esc(v.fixed || '-')}</td>
          <td>${sevBadge(v.severity)}</td>
        </tr>`;
    }).join('');
    return (scan.truncated ? `<div style="font-size: var(--fs-xs); color: var(--warning); margin-bottom: 8px;">${esc(t('ds_vuln_truncated'))}</div>` : '') +
      `<div style="overflow-x: auto;"><table>
        <thead><tr><th>${esc(t('ds_col_cve'))}</th><th>CVE / Exploitation</th><th>${esc(t('ds_col_pkg'))}</th><th>${esc(t('ds_col_installed'))}</th><th>${esc(t('ds_col_fixed'))}</th><th>${esc(t('ds_col_severity'))}</th></tr></thead>
        <tbody>${rows}</tbody></table></div>`;
  }
})();
