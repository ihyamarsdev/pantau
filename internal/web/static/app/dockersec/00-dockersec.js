/* Container misconfiguration audit and image vulnerability scan for the Docker tab.
 * The stored results come from GET /api/hosts/{id}/docker/security (database only, so the panel is fast),
 * the exploit intelligence from GET /api/hosts/{id}/docker/security/intel (loaded after the panel is
 * painted, so the panel never waits for the network) and the two POST endpoints (audit, scan-image).
 * Relies on index.html globals: currentHost, currentTab, dockerSubTab, loadDockerTab, svgIcon, escapeHtml,
 * t, I18N, openModal, closeModal, showToast, window.VI (exploit intelligence). */
(function () {
  'use strict';

  const DS = (window.DS = {});

  const S = {
    hostID: 0, loaded: false, audit: null, images: [], intel: null, busy: false,
    error: '', job: null,
    section: 'audit',        // 'audit' | 'images' (remembered for the browser session)
    auditSev: '', auditFilter: '', auditOpen: {}, auditAll: null,
    imgFilter: '', imgStatus: 'all', imgSort: 'severity', imgLimit: 50,
    vulnFilter: '', vulnSev: '', vulnFixable: false, vulnSort: 'severity', vulnLimit: 50,
    modalRef: null
  };
  const SECTION_KEY = 'pantau_ds_section';
  let filterTimer = null;
  let loadSeq = 0; // the newest GET /docker/security wins; an older answer never overwrites it

  const esc = (s) => escapeHtml(String(s === null || s === undefined ? '' : s));
  const icon = (n) => svgIcon(n, 'icon-sm');
  const fill = (s, vars) => Object.keys(vars).reduce((a, k) => a.split('{' + k + '}').join(vars[k]), s);
  const num = (n) => new Intl.NumberFormat().format(n || 0);

  const SEV_ORDER = ['critical', 'high', 'medium', 'low'];
  const SEV_CLASS = { critical: 'badge-danger', high: 'badge-warning', medium: 'badge-info', low: 'badge-unknown' };
  const sevRank = (s) => { const i = SEV_ORDER.indexOf(String(s || '').toLowerCase()); return i < 0 ? 99 : i; };
  const STALE_AFTER_MS = 7 * 24 * 3600 * 1000;
  const CHUNK = 50;

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
      ds_vuln_truncated: 'Only the 200 most severe vulnerabilities are stored for this image.',
      ds_col_cve: 'CVE', ds_col_pkg: 'Package', ds_col_installed: 'Installed', ds_col_fixed: 'Fixed',
      ds_scan_done: 'Scan of {ref} finished: {n} vulnerability(ies).',
      ds_scan_fail: 'Scan of {ref} failed: {why}',
      ds_scan_all_done: 'Scanned {n} image(s): {v} vulnerability(ies) in total.',
      ds_scan_missing: 'No vulnerability scanner (trivy or grype) is installed on this host and Pantau could not install trivy (it needs root or passwordless sudo, curl or wget, and internet access). Install one on the host to enable image scanning.',
      ds_audit_done: 'Audit finished: {n} finding(s).',
      ds_audit_fail: 'Audit failed: {why}',
      ds_loading: 'Loading vulnerabilities…',
      ds_sec_audit: 'Container audit', ds_sec_images: 'Image vulnerabilities',
      ds_sum_audit: 'Audit findings', ds_sum_images: 'Image vulnerabilities',
      ds_sum_containers: 'Containers', ds_sum_images_scanned: 'Images scanned',
      ds_last_audit: 'Last audit', ds_last_scan: 'Last scan', ds_never: 'Never',
      ds_stale: 'Older than 7 days', ds_ago: 'ago', ds_just_now: 'just now',
      ds_retry: 'Retry', ds_load_failed: 'The security results could not be loaded: {why}',
      ds_expand_all: 'Expand all', ds_collapse_all: 'Collapse all',
      ds_findings: 'finding(s)', ds_show_more: 'Show more ({n} left)',
      ds_sort_severity: 'Worst severity', ds_sort_name: 'Name', ds_sort_critical: 'Critical',
      ds_sort_high: 'High', ds_sort_total: 'Total', ds_sort_scanned: 'Scanned',
      ds_filter_all: 'All', ds_filter_risky: 'Critical or high', ds_filter_clean: 'Clean', ds_filter_failed: 'Failed',
      ds_img_filter_ph: 'Filter by image name',
      ds_vuln_filter_ph: 'Filter by CVE, package or title',
      ds_fixable_only: 'Fixable only', ds_copy_cves: 'Copy CVE list', ds_copied: 'The CVE list was copied to the clipboard.',
      ds_col_time: 'Scanned', ds_severity: 'Severity', ds_no_match: 'Nothing matches the filter.',
      ds_open_detail: 'Open the vulnerability detail', ds_vuln_count: '{n} vulnerability(ies)',
      ds_not_assessed: 'Not assessed', 
      
      
      
      ds_stale_hint: 'The result is older than 7 days: run it again for the current state of the host.'
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
      ds_vuln_title: 'Kerentanan {image}',
      ds_vuln_none: 'Scanner tidak melaporkan kerentanan yang dikenal untuk image ini.',
      ds_vuln_truncated: 'Hanya 200 kerentanan terberat yang tersimpan untuk image ini.',
      ds_col_cve: 'CVE', ds_col_pkg: 'Paket', ds_col_installed: 'Terpasang', ds_col_fixed: 'Diperbaiki',
      ds_scan_done: 'Pindai {ref} selesai: {n} kerentanan.',
      ds_scan_fail: 'Pindai {ref} gagal: {why}',
      ds_scan_all_done: '{n} image dipindai: total {v} kerentanan.',
      ds_scan_missing: 'Tidak ada scanner kerentanan (trivy atau grype) di host ini dan Pantau gagal memasang trivy (butuh root atau sudo tanpa password, curl atau wget, dan akses internet). Pasang salah satunya di host untuk mengaktifkan pemindaian image.',
      ds_audit_done: 'Audit selesai: {n} temuan.',
      ds_audit_fail: 'Audit gagal: {why}',
      ds_loading: 'Memuat kerentanan…',
      ds_sec_audit: 'Audit container', ds_sec_images: 'Kerentanan image',
      ds_sum_audit: 'Temuan audit', ds_sum_images: 'Kerentanan image',
      ds_sum_containers: 'Container', ds_sum_images_scanned: 'Image terpindai',
      ds_last_audit: 'Audit terakhir', ds_last_scan: 'Pindai terakhir', ds_never: 'Belum pernah',
      ds_stale: 'Lebih dari 7 hari', ds_ago: 'lalu', ds_just_now: 'baru saja',
      ds_retry: 'Coba lagi', ds_load_failed: 'Hasil keamanan tidak dapat dimuat: {why}',
      ds_expand_all: 'Buka semua', ds_collapse_all: 'Tutup semua',
      ds_findings: 'temuan', ds_show_more: 'Tampilkan lagi ({n} lagi)',
      ds_sort_severity: 'Tingkat terburuk', ds_sort_name: 'Nama', ds_sort_critical: 'Kritis',
      ds_sort_high: 'Tinggi', ds_sort_total: 'Total', ds_sort_scanned: 'Waktu pindai',
      ds_filter_all: 'Semua', ds_filter_risky: 'Kritis atau tinggi', ds_filter_clean: 'Bersih', ds_filter_failed: 'Gagal',
      ds_img_filter_ph: 'Saring menurut nama image',
      ds_vuln_filter_ph: 'Saring menurut CVE, paket, atau judul',
      ds_fixable_only: 'Hanya yang ada perbaikannya', ds_copy_cves: 'Salin daftar CVE', ds_copied: 'Daftar CVE disalin ke clipboard.',
      ds_col_time: 'Waktu pindai', ds_severity: 'Tingkat', ds_no_match: 'Tidak ada yang cocok dengan saringan.',
      ds_open_detail: 'Buka rincian kerentanan', ds_vuln_count: '{n} kerentanan',
      ds_not_assessed: 'Belum dinilai', 
      
      
      
      ds_stale_hint: 'Hasilnya lebih dari 7 hari lama: jalankan lagi untuk keadaan host saat ini.'
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

  // Relative time with the absolute time as title, and a stale marker after 7 days.
  function relTime(ts) {
    if (!ts) return { relative: t('ds_never'), title: '', stale: false };
    const d = new Date(ts);
    if (isNaN(d.getTime())) return { relative: '-', title: '', stale: false };
    const ms = Date.now() - d.getTime();
    const sec = Math.floor(ms / 1000);
    const stale = ms > STALE_AFTER_MS;
    let rel;
    if (sec < 60) rel = t('ds_just_now');
    else if (sec < 3600) rel = Math.floor(sec / 60) + 'm ' + t('ds_ago');
    else if (sec < 86400) rel = Math.floor(sec / 3600) + 'h ' + t('ds_ago');
    else rel = Math.floor(sec / 86400) + 'd ' + t('ds_ago');
    return { relative: rel, title: d.toLocaleString(), stale: stale };
  }

  function timeCell(ts) {
    const r = relTime(ts);
    if (!ts) return `<span style="color: var(--text-muted);">${esc(t('ds_never'))}</span>`;
    return `<span title="${esc(r.title)}" style="color: ${r.stale ? 'var(--warning)' : 'var(--text-muted)'};">${esc(r.relative)}${r.stale ? ' · ' + esc(t('ds_stale')) : ''}</span>`;
  }

  const scanOf = (ref) => S.images.find((s) => s.subject === ref) || null;
  const worstSev = (counts) => SEV_ORDER.find((s) => (counts || {})[s]) || '';
  const sumCounts = (list) => {
    const out = { critical: 0, high: 0, medium: 0, low: 0, unknown: 0, total: 0 };
    list.forEach((s) => {
      const c = s.vuln_counts || {};
      Object.keys(out).forEach((k) => { out[k] += c[k] || 0; });
    });
    return out;
  };
  const auditCounts = () => (S.audit && S.audit.counts) || { critical: 0, high: 0, medium: 0, low: 0, total: 0 };


  // Load the stored results of a host (audit + image scans) into the cache.
  DS.load = async function (hostID) {
    if (!hostID) return null;
    // A different host: drop the cached results so no stale data of the previous host is shown.
    if (S.hostID !== hostID) { S.audit = null; S.images = []; S.intel = null; S.loaded = false; }
    S.hostID = hostID;
    // Only the newest load may write to the cache: an older, slower answer (a host with many images)
    // must never overwrite the result of a scan that finished in the meantime.
    const seq = ++loadSeq;
    const r = await api('/api/hosts/' + hostID + '/docker/security');
    if (S.hostID !== hostID) return null; // the user switched to another host in the meantime
    if (seq !== loadSeq) return null;     // a newer load already wrote the cache
    // A failed reload keeps the data already on screen: the error is shown above it, not instead of it.
    if (!r.ok) { S.error = r.error; return null; }
    S.error = '';
    S.audit = r.data.audit || null;
    S.images = r.data.images || [];
    S.loaded = true;
    return r.data;
  };

  // Merge exploit intelligence into the cache (the answer of the POST and of the intel endpoint have
  // the same shape: {enabled, cves:{CVE-..:{kev,epss}}}).
  function mergeIntel(intel) {
    if (!intel || !intel.cves) return;
    const base = S.intel && S.intel.cves ? S.intel : { enabled: true, cves: {} };
    S.intel = Object.assign({}, base, intel, { cves: Object.assign({}, base.cves, intel.cves) });
  }

  // Put a fresh scan result into the cache (the POST answer is the newest state of that image).
  function upsertScan(scan) {
    if (!scan || !scan.subject) return;
    const i = S.images.findIndex((s) => s.subject === scan.subject);
    if (i >= 0) S.images[i] = scan;
    else S.images.push(scan);
  }

  // Put a fresh audit result into the cache.
  function upsertAudit(audit) {
    if (!audit) return;
    S.audit = audit;
  }

  // True when the stored results of this host are already in the cache.
  DS.hasData = function (hostID) { return S.loaded && S.hostID === hostID; };

  // Refresh the cache and repaint the chips in the Images list.
  DS.refresh = async function () {
    if (!currentHost) return null;
    const data = await DS.load(currentHost.id);
    paintChips();
    return data;
  };

  // The exploit intelligence (CISA KEV + EPSS) of every stored image scan. It is loaded after the panel is
  // painted, so a slow lookup never delays the panel; the answer is dropped when the host changed.
  DS.loadIntel = async function (hostID) {
    if (!hostID) return;
    const r = await api('/api/hosts/' + hostID + '/docker/security/intel');
    if (!r.ok || S.hostID !== hostID) return;
    mergeIntel(r.data.intel);
    const body = document.getElementById('dockerSecBody');
    if (body && S.modalRef) body.innerHTML = vulnTableHtml(scanOf(S.modalRef));
  };

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


  /* ---------- summary strip ---------- */
  // A clickable severity chip: it filters the section it belongs to.
  function sevChip(sev, count, active, action) {
    return `<button type="button" class="ds-sev-chip ${SEV_CLASS[sev]}${active ? ' active' : ''}" data-sev="${esc(sev)}" onclick="${esc(action)}" aria-pressed="${active ? 'true' : 'false'}">${esc(t('ds_' + sev))} <b>${num(count)}</b></button>`;
  }

  function summaryStripHtml() {
    const ac = auditCounts();
    const ic = sumCounts(S.images);
    const containers = new Set(((S.audit && S.audit.findings) || []).map((f) => f.container)).size;
    const lastAudit = S.audit && S.audit.scanned_at;
    const lastScan = S.images.map((s) => s.scanned_at).filter(Boolean).sort().pop();
    // The audit chips filter the audit section, the image chips the image section (each chip toggles itself).
    const auditAct = (s) => "DS.setAuditSev('" + s + "')";
    const imgAct = (s) => "DS.setImgSev('" + s + "')";
    return `<div class="ds-summary" id="dsSummary">
      <div class="ds-summary-col">
        <div class="ds-summary-label">${esc(t('ds_sum_audit'))}</div>
        <div class="ds-summary-chips">
          ${SEV_ORDER.map((s) => sevChip(s, ac[s] || 0, S.section === 'audit' && S.auditSev === s, auditAct(s))).join('')}
          <span class="ds-summary-total">${num(ac.total || 0)} ${esc(t('ds_findings'))}</span>
        </div>
      </div>
      <div class="ds-summary-col">
        <div class="ds-summary-label">${esc(t('ds_sum_images'))}</div>
        <div class="ds-summary-chips">
          ${SEV_ORDER.map((s) => sevChip(s, ic[s] || 0, S.section === 'images' && S.imgSev === s, imgAct(s))).join('')}
          <span class="ds-summary-total">${num(ic.total || 0)} ${esc(t('ds_findings'))}</span>
        </div>
      </div>
      <div class="ds-summary-col">
        <div class="ds-summary-meta">
          <span>${esc(t('ds_sum_containers'))}: <b>${num(containers)}</b></span>
          <span>${esc(t('ds_sum_images_scanned'))}: <b>${num(S.images.length)}</b></span>
          <span>${esc(t('ds_last_audit'))}: ${timeCell(lastAudit)}</span>
          <span>${esc(t('ds_last_scan'))}: ${timeCell(lastScan)}</span>
        </div>
      </div>
    </div>`;
  }

  /* ---------- panel sections ---------- */
  function sectionTabsHtml() {
    const a = S.section === 'audit';
    return `<div class="ds-seg" role="group" aria-label="${esc(t('ds_title'))}">
      <button type="button" class="ds-seg-btn${a ? ' active' : ''}" data-section="audit" onclick="DS.setSection('audit')" aria-pressed="${a}">${esc(t('ds_sec_audit'))}</button>
      <button type="button" class="ds-seg-btn${a ? '' : ' active'}" data-section="images" onclick="DS.setSection('images')" aria-pressed="${!a}">${esc(t('ds_sec_images'))}</button>
    </div>`;
  }

  // Switch between the two sections of the panel (remembered for the browser session).
  DS.setSection = function (section) {
    S.section = section === 'images' ? 'images' : 'audit';
    try { sessionStorage.setItem(SECTION_KEY, S.section); } catch (e) { /* no session storage */ }
    repaintPanel();
  };

  // Repaint the whole panel body (the summary strip, the section tabs and the section body).
  function repaintPanel() {
    const panel = document.getElementById('dsPanel');
    if (!panel) return;
    panel.innerHTML = panelBodyHtml();
  }

  // The body of the panel: the error banner, the summary strip, the section tabs and the section.
  function panelBodyHtml() {
    const errHtml = S.error ? `<div class="ds-error" role="alert">
        <div class="ds-error-text">${esc(fill(t('ds_load_failed'), { why: S.error }))}</div>
        <button type="button" class="btn btn-sm btn-primary" onclick="DS.retry()">${icon('refresh-cw')} <span>${esc(t('ds_retry'))}</span></button>
      </div>` : '';
    return errHtml +
      summaryStripHtml() +
      `<div id="dsSections" class="ds-sections">${sectionTabsHtml()}</div>` +
      `<div id="dsBody">${sectionBodyHtml()}</div>`;
  }

  /* ---------- container audit ---------- */
  // The findings that pass the severity and the text filter, grouped per container.
  function auditGroups() {
    const findings = ((S.audit && S.audit.findings) || []).filter((f) => {
      if (S.auditSev && String(f.severity || '').toLowerCase() !== S.auditSev) return false;
      if (!S.auditFilter) return true;
      const q = S.auditFilter.toLowerCase();
      return (f.container || '').toLowerCase().includes(q) || (f.rule_id || '').toLowerCase().includes(q) ||
        (f.title || '').toLowerCase().includes(q);
    });
    const groups = new Map();
    findings.forEach((f) => {
      if (!groups.has(f.container)) groups.set(f.container, []);
      groups.get(f.container).push(f);
    });
    return Array.from(groups.entries()).map(([name, items]) => {
      const counts = { critical: 0, high: 0, medium: 0, low: 0 };
      items.forEach((f) => { const s = String(f.severity || '').toLowerCase(); if (counts[s] !== undefined) counts[s]++; });
      return { name: name, items: items, counts: counts, worst: worstSev(counts) };
    }).sort((a, b) => {
      const r = sevRank(a.worst) - sevRank(b.worst);
      return r !== 0 ? r : a.name.localeCompare(b.name);
    });
  }

  // A group is open when the operator opened it; the first groups start open.
  function groupOpen(name, index) {
    if (S.auditAll !== null) return S.auditAll;
    if (Object.prototype.hasOwnProperty.call(S.auditOpen, name)) return S.auditOpen[name];
    return index < 3;
  }

  DS.toggleGroup = function (name, index) {
    const open = groupOpen(name, index);
    S.auditAll = null;
    S.auditOpen[name] = !open;
    repaintAudit();
  };

  DS.expandAll = function (all) {
    S.auditAll = !!all;
    repaintAudit();
  };

  function auditGroupsHtml() {
    const groups = auditGroups();
    if (!groups.length) {
      const msg = S.auditSev || S.auditFilter ? t('ds_no_match') : ((S.audit && S.audit.note) || t('ds_no_findings'));
      return `<div class="hub-empty" style="padding: 16px;">${esc(msg)}</div>`;
    }
    return groups.map((g, i) => {
      const open = groupOpen(g.name, i);
      const head = `<button type="button" class="ds-group-head" data-group="${esc(g.name)}" onclick="DS.toggleGroup(this.dataset.group, ${i})" aria-expanded="${open}">
        <span class="ds-group-caret">${icon(open ? 'chevron-down' : 'chevron-right')}</span>
        <span class="ds-group-name">${esc(g.name)}</span>
        ${g.worst ? `<span class="badge ${SEV_CLASS[g.worst]}">${esc(t('ds_' + g.worst))}</span>` : ''}
        <span class="ds-group-count">${num(g.items.length)} ${esc(t('ds_findings'))}</span>
        <span class="ds-group-chips">${SEV_ORDER.filter((s) => g.counts[s]).map((s) => `<span class="badge ${SEV_CLASS[s]}" style="text-transform: none;">${num(g.counts[s])} ${esc(t('ds_' + s))}</span>`).join('')}</span>
      </button>`;
      const body = open ? `<div class="ds-group-body">${g.items.map(findingHtml).join('')}</div>` : '';
      return `<div class="ds-group" data-group-box="${esc(g.name)}">${head}${body}</div>`;
    }).join('');
  }

  // One finding: the rule with its detail and the remediation (visible on expand, not in a wide table).
  function findingHtml(f) {
    const sev = String(f.severity || '').toLowerCase();
    return `<div class="ds-finding">
      <div class="ds-finding-head">
        <span class="badge ${SEV_CLASS[sev] || 'badge-unknown'}">${esc(t('ds_' + (sev || 'unknown'), sev))}</span>
        <span class="ds-finding-rule dk-mono">${esc(f.rule_id || '')}</span>
        <span class="ds-finding-title">${esc(f.title || '')}</span>
        <span class="ds-finding-image dk-mono">${esc(f.image || '')}</span>
      </div>
      ${f.detail ? `<div class="ds-finding-detail">${esc(f.detail)}</div>` : ''}
      ${f.remediation ? `<div class="ds-finding-fix"><b>${esc(t('ds_col_remediation'))}:</b> ${esc(f.remediation)}</div>` : ''}
    </div>`;
  }

  function auditSectionHtml() {
    const a = S.audit;
    if (!a || !a.scanned_at) {
      return `<div class="hub-empty" style="padding: 16px;">
        <div>${esc(t('ds_audit_none'))}</div>
        <div style="margin-top: 10px;"><button type="button" class="btn btn-sm btn-primary" onclick="DS.runAudit()">${icon('shield')} <span>${esc(t('ds_audit'))}</span></button></div>
      </div>`;
    }
    if (a.status === 'unsupported') {
      return `<div class="hub-empty" style="padding: 16px;">${esc(t('ds_not_assessed'))}${a.note ? ': ' + esc(a.note) : ''}</div>`;
    }
    if (a.status === 'error') {
      return `<div class="hub-empty" style="padding: 16px;">${esc(t('ds_st_error'))}: ${esc(a.error || '')}</div>`;
    }
    const counts = auditCounts();
    const sevChips = SEV_ORDER.map((s) => sevChip(s, counts[s] || 0, S.auditSev === s, "DS.setAuditSev('" + s + "')")).join('');
    const groups = auditGroups();
    return `<div class="ds-toolbar" id="dsAuditToolbar">
        <input type="search" id="dsFilter" value="${esc(S.auditFilter)}" placeholder="${esc(t('ds_filter_ph'))}" maxlength="128" autocomplete="off" oninput="DS.filterFindings(this.value)" />
        <div class="ds-chips-row">${sevChips}
          <button type="button" class="ds-sev-chip" onclick="DS.expandAll(true)" aria-pressed="false">${esc(t('ds_expand_all'))}</button>
          <button type="button" class="ds-sev-chip" onclick="DS.expandAll(false)" aria-pressed="false">${esc(t('ds_collapse_all'))}</button>
        </div>
      </div>
      <div class="ds-audit-meta">
        <span title="${esc(relTime(a.scanned_at).title)}">${esc(fill(t('ds_audit_note'), {
          t: relTime(a.scanned_at).relative,
          n: num(counts.total || 0),
          c: num(new Set((a.findings || []).map((f) => f.container)).size)
        }))}</span>
        ${relTime(a.scanned_at).stale ? `<span class="ds-stale">${esc(t('ds_stale_hint'))}</span>` : ''}
      </div>
      <div id="dsAuditTable">${auditGroupsHtml()}</div>`;
  }

  // Repaint only the audit part (the filter keeps the focus).
  function repaintAudit() {
    const box = document.getElementById('dsAuditTable');
    if (box) box.innerHTML = auditGroupsHtml();
    const sum = document.getElementById('dsSummary');
    if (sum) sum.outerHTML = summaryStripHtml();
  }

  // Filter the findings client-side: only the group list is repainted, the tab is not reloaded (no flicker).
  DS.filterFindings = function (value) {
    S.auditFilter = value.trim();
    clearTimeout(filterTimer);
    filterTimer = setTimeout(() => {
      repaintAudit();
      const el = document.getElementById('dsFilter');
      if (el) { el.focus(); el.setSelectionRange(el.value.length, el.value.length); }
    }, 350);
  };

  // The severity filter of the audit section (the chip toggles its own filter).
  DS.setAuditSev = function (sev) {
    S.auditSev = S.auditSev === sev ? '' : sev;
    S.section = 'audit';
    repaintPanel();
  };


  /* ---------- image vulnerabilities ---------- */
  // The images that pass the text, status and severity filters, in the chosen order.
  function filteredImages() {
    const q = S.imgFilter.toLowerCase();
    let list = S.images.filter((s) => {
      if (q && !(s.subject || '').toLowerCase().includes(q)) return false;
      const c = s.vuln_counts || {};
      if (S.imgSev && !c[S.imgSev]) return false;
      if (S.imgStatus === 'risky' && !(c.critical || c.high)) return false;
      if (S.imgStatus === 'clean' && (c.total || 0) > 0) return false;
      if (S.imgStatus === 'failed' && s.status !== 'error' && s.status !== 'unsupported') return false;
      return true;
    });
    const rank = (s) => sevRank(worstSev(s.vuln_counts || {}));
    list.sort((a, b) => {
      switch (S.imgSort) {
        case 'name': return (a.subject || '').localeCompare(b.subject || '');
        case 'critical': return (b.vuln_counts || {}).critical - (a.vuln_counts || {}).critical;
        case 'high': return (b.vuln_counts || {}).high - (a.vuln_counts || {}).high;
        case 'total': return ((b.vuln_counts || {}).total || 0) - ((a.vuln_counts || {}).total || 0);
        case 'scanned': return String(b.scanned_at || '').localeCompare(String(a.scanned_at || ''));
        default: {
          const r = rank(a) - rank(b);
          if (r !== 0) return r;
          const t = ((b.vuln_counts || {}).total || 0) - ((a.vuln_counts || {}).total || 0);
          return t !== 0 ? t : (a.subject || '').localeCompare(b.subject || '');
        }
      }
    });
    return list;
  }

  DS.setImgSev = function (sev) {
    S.imgSev = S.imgSev === sev ? '' : sev;
    S.section = 'images';
    repaintPanel();
  };

  DS.setImgStatus = function (status) {
    S.imgStatus = status || 'all';
    S.section = 'images';
    repaintPanel();
  };

  DS.setImgSort = function (sort) {
    S.imgSort = sort || 'severity';
    repaintPanel();
  };

  DS.filterImages = function (value) {
    S.imgFilter = value.trim();
    clearTimeout(filterTimer);
    filterTimer = setTimeout(() => {
      const box = document.getElementById('dsImageTable');
      if (box) box.innerHTML = imageRowsHtml();
      const el = document.getElementById('dsImageFilter');
      if (el) { el.focus(); el.setSelectionRange(el.value.length, el.value.length); }
    }, 350);
  };

  function imageRowsHtml() {
    if (!S.images.length) {
      return `<div class="hub-empty" style="padding: 16px;">
        <div>${esc(t('ds_images_none'))}</div>
        <div style="margin-top: 10px;"><button type="button" class="btn btn-sm btn-primary" onclick="DS.scanAll()">${icon('shield')} <span>${esc(t('ds_scan_all'))}</span></button></div>
      </div>`;
    }
    const list = filteredImages();
    if (!list.length) return `<div class="hub-empty" style="padding: 16px;">${esc(t('ds_no_match'))}</div>`;
    const sortBtn = (key, label) => `<button type="button" class="ds-sort-btn${S.imgSort === key ? ' active' : ''}" onclick="DS.setImgSort('${key}')" aria-pressed="${S.imgSort === key}">${esc(label)}</button>`;
    const statusBtn = (key, label) => `<button type="button" class="ds-sev-chip${S.imgStatus === key ? ' active' : ''}" onclick="DS.setImgStatus('${key}')" aria-pressed="${S.imgStatus === key}">${esc(label)}</button>`;
    const shown = list.slice(0, S.imgLimit);
    const rows = shown.map((s) => {
      const st = s.status || 'ok';
      const statusCls = st === 'error' ? 'badge-danger' : (st === 'unsupported' ? 'badge-unknown' : (st === 'partial' ? 'badge-warning' : 'badge-success'));
      const statusNote = st === 'error' ? (s.error || '') : (st === 'unsupported' ? (s.note || '') : '');
      return `<tr class="ds-img-row" data-image="${esc(s.subject)}" onclick="DS.openImage(this.dataset.image)" tabindex="0" onkeydown="if(event.key==='Enter'||event.key===' '){event.preventDefault();DS.openImage(this.dataset.image);}" title="${esc(t('ds_open_detail'))}">
        <td class="dk-mono ds-img-name">${esc(s.subject)}</td>
        <td>${chipsHtml(s)}</td>
        <td class="dk-mono" style="font-size: var(--fs-xs);">${esc(s.scanner || '-')}</td>
        <td><span class="badge ${statusCls}">${esc(t('ds_st_' + st, st))}</span>${statusNote ? `<div class="ds-status-note">${esc(statusNote)}</div>` : ''}</td>
        <td>${timeCell(s.scanned_at)}</td>
        <td class="dk-actions"><button class="btn btn-sm btn-ghost btn-icon" title="${esc(t('ds_scan_image'))}" data-ref="${esc(s.subject)}" onclick="event.stopPropagation();DS.scanImage(this.dataset.ref, this)">${icon('shield')}</button></td>
      </tr>`;
    }).join('');
    const left = list.length - shown.length;
    return `<div class="ds-toolbar">
        <input type="search" id="dsImageFilter" value="${esc(S.imgFilter)}" placeholder="${esc(t('ds_img_filter_ph'))}" maxlength="128" autocomplete="off" oninput="DS.filterImages(this.value)" />
        <div class="ds-chips-row">
          ${statusBtn('all', t('ds_filter_all'))}
          ${statusBtn('risky', t('ds_filter_risky'))}
          ${statusBtn('clean', t('ds_filter_clean'))}
          ${statusBtn('failed', t('ds_filter_failed'))}
        </div>
        <div class="ds-chips-row">${sortBtn('severity', t('ds_sort_severity'))}${sortBtn('name', t('ds_sort_name'))}${sortBtn('critical', t('ds_sort_critical'))}${sortBtn('high', t('ds_sort_high'))}${sortBtn('total', t('ds_sort_total'))}${sortBtn('scanned', t('ds_sort_scanned'))}</div>
      </div>
      <div class="ds-table-wrap">
        <table class="ds-table">
          <thead><tr>
            <th>${esc(t('ds_col_image'))}</th><th>${esc(t('ds_col_counts'))}</th><th>${esc(t('ds_col_scanner'))}</th>
            <th>${esc(t('ds_col_status'))}</th><th>${esc(t('ds_col_time'))}</th><th></th>
          </tr></thead>
          <tbody>${rows}</tbody>
        </table>
      </div>
      ${left > 0 ? `<div style="margin-top: 8px;"><button type="button" class="btn btn-sm btn-ghost" onclick="DS.showMoreImages()">${esc(fill(t('ds_show_more'), { n: num(left) }))}</button></div>` : ''}`;
  }

  DS.showMoreImages = function () {
    S.imgLimit += CHUNK;
    const box = document.getElementById('dsImageTable');
    if (box) box.innerHTML = imageRowsHtml();
  };

  function imageSectionHtml() {
    return `<div id="dsImageTable">${imageRowsHtml()}</div>`;
  }


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
    S.modalRef = ref;
    // The modal opens right away with a spinner; the scan data may still be loading.
    ensureModal();
    document.getElementById('dockerSecTitle').textContent = fill(t('ds_vuln_title'), { image: ref });
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
    // The exploit intelligence arrives later; it fills the KEV/EPSS badges in.
    if (!S.intel) DS.loadIntel(S.hostID);
  };

  // The vulnerabilities of an image that pass the filters, sorted and cut into chunks.
  function filteredVulns(scan) {
    const q = S.vulnFilter.toLowerCase();
    let list = (scan.vulns || []).filter((v) => {
      if (S.vulnSev && String(v.severity || '').toLowerCase() !== S.vulnSev) return false;
      if (S.vulnFixable && !(v.fixed || '').trim()) return false;
      if (!q) return true;
      return (v.id || '').toLowerCase().includes(q) || (v.package || '').toLowerCase().includes(q) ||
        (v.title || '').toLowerCase().includes(q);
    });
    if (S.vulnSort === 'package') list.sort((a, b) => (a.package || '').localeCompare(b.package || ''));
    else {
      list.sort((a, b) => {
        const r = sevRank(a.severity) - sevRank(b.severity);
        if (r !== 0) return r;
        return (a.id || '').localeCompare(b.id || '');
      });
    }
    return list;
  }

  function vulnTableHtml(scan) {
    if (!scan) return `<div class="hub-empty" style="padding: 16px;">${esc(t('ds_images_none'))}</div>`;
    const counts = scan.vuln_counts || {};
    const all = scan.vulns || [];
    const head = `<div class="ds-vuln-head">
        <div class="ds-vuln-title dk-mono">${esc(scan.subject)}</div>
        <div class="ds-vuln-meta">
          <span class="badge badge-info">${esc(t('ds_col_scanner'))}: ${esc(scan.scanner || '-')}</span>
          <span class="badge badge-unknown">${esc(t('ds_col_time'))}: ${timeCell(scan.scanned_at)}</span>
          <span class="badge badge-unknown">${esc(fill(t('ds_vuln_count'), { n: num(counts.total || 0) }))}</span>
          ${SEV_ORDER.filter((s) => counts[s]).map((s) => `<span class="badge ${SEV_CLASS[s]}">${num(counts[s])} ${esc(t('ds_' + s))}</span>`).join('')}
        </div>
      </div>`;
    if (!all.length) return head + `<div class="hub-empty" style="padding: 16px;">${esc(scan.note || t('ds_vuln_none'))}</div>`;
    const list = filteredVulns(scan);
    const intel = S.intel || {};
    const shown = list.slice(0, S.vulnLimit);
    const rows = shown.map((v) => {
      const cve = /^CVE-/i.test(v.id || '');
      const idCell = cve
        ? `<a class="dk-mono" href="https://nvd.nist.gov/vuln/detail/${esc(v.id)}" target="_blank" rel="noopener noreferrer">${esc(v.id)}</a>`
        : `<span class="dk-mono">${esc(v.id || '-')}</span>`;
      const intelCell = cve && window.VI ? VI.findingCell({ cves: [v.id] }, intel) : `<span style="color: var(--text-muted);">-</span>`;
      const sev = String(v.severity || '').toLowerCase();
      return `
        <tr>
          <td>${idCell}</td>
          <td>${intelCell}</td>
          <td class="dk-mono" style="font-size: var(--fs-xs);">${esc(v.package || '-')}${v.title ? `<div style="font-family: var(--font-sans); color: var(--text-muted);">${esc(v.title)}</div>` : ''}</td>
          <td class="dk-mono" style="font-size: var(--fs-xs);">${esc(v.installed || '-')}</td>
          <td class="dk-mono" style="font-size: var(--fs-xs);">${esc(v.fixed || '-')}</td>
          <td><span class="badge ${SEV_CLASS[sev] || 'badge-unknown'}">${esc(t('ds_' + (sev || 'unknown'), sev))}</span></td>
        </tr>`;
    }).join('');
    const left = list.length - shown.length;
    const sevChips = SEV_ORDER.map((s) => `<button type="button" class="ds-sev-chip ${SEV_CLASS[s]}${S.vulnSev === s ? ' active' : ''}" onclick="DS.setVulnSev('${s}')" aria-pressed="${S.vulnSev === s}">${esc(t('ds_' + s))} ${num(counts[s] || 0)}</button>`).join('');
    const sortBtn = (key, label) => `<button type="button" class="ds-sort-btn${S.vulnSort === key ? ' active' : ''}" onclick="DS.setVulnSort('${key}')" aria-pressed="${S.vulnSort === key}">${esc(label)}</button>`;
    return head +
      (scan.truncated ? `<div style="font-size: var(--fs-xs); color: var(--warning); margin-bottom: 8px;">${esc(t('ds_vuln_truncated'))}</div>` : '') +
      `<div class="ds-toolbar">
        <input type="search" id="dsVulnFilter" value="${esc(S.vulnFilter)}" placeholder="${esc(t('ds_vuln_filter_ph'))}" maxlength="128" autocomplete="off" oninput="DS.filterVulns(this.value)" />
        <div class="ds-chips-row">${sevChips}
          <button type="button" class="ds-sev-chip${S.vulnFixable ? ' active' : ''}" onclick="DS.toggleFixable()" aria-pressed="${S.vulnFixable}">${esc(t('ds_fixable_only'))}</button>
        </div>
        <div class="ds-chips-row">${sortBtn('severity', t('ds_severity'))}${sortBtn('package', t('ds_col_pkg'))}
          <button type="button" class="ds-sort-btn" onclick="DS.copyCves()">${icon('copy')} <span>${esc(t('ds_copy_cves'))}</span></button>
        </div>
      </div>` +
      (list.length ? `<div class="ds-table-wrap"><table class="ds-table">
        <thead><tr><th>${esc(t('ds_col_cve'))}</th><th>CVE / Exploitation</th><th>${esc(t('ds_col_pkg'))}</th><th>${esc(t('ds_col_installed'))}</th><th>${esc(t('ds_col_fixed'))}</th><th>${esc(t('ds_col_severity'))}</th></tr></thead>
        <tbody>${rows}</tbody></table></div>
      ${left > 0 ? `<div style="margin-top: 8px;"><button type="button" class="btn btn-sm btn-ghost" onclick="DS.showMoreVulns()">${esc(fill(t('ds_show_more'), { n: num(left) }))}</button></div>` : ''}`
        : `<div class="hub-empty" style="padding: 16px;">${esc(t('ds_no_match'))}</div>`);
  }

  DS.setVulnSev = function (sev) {
    S.vulnSev = S.vulnSev === sev ? '' : sev;
    S.vulnLimit = CHUNK;
    const body = document.getElementById('dockerSecBody');
    if (body) body.innerHTML = vulnTableHtml(scanOf(S.modalRef));
  };

  DS.setVulnSort = function (sort) {
    S.vulnSort = sort || 'severity';
    S.vulnLimit = CHUNK;
    const body = document.getElementById('dockerSecBody');
    if (body) body.innerHTML = vulnTableHtml(scanOf(S.modalRef));
  };

  DS.toggleFixable = function () {
    S.vulnFixable = !S.vulnFixable;
    S.vulnLimit = CHUNK;
    const body = document.getElementById('dockerSecBody');
    if (body) body.innerHTML = vulnTableHtml(scanOf(S.modalRef));
  };

  DS.filterVulns = function (value) {
    S.vulnFilter = value.trim();
    clearTimeout(filterTimer);
    filterTimer = setTimeout(() => {
      const body = document.getElementById('dockerSecBody');
      if (body) body.innerHTML = vulnTableHtml(scanOf(S.modalRef));
      const el = document.getElementById('dsVulnFilter');
      if (el) { el.focus(); el.setSelectionRange(el.value.length, el.value.length); }
    }, 350);
  };

  DS.showMoreVulns = function () {
    S.vulnLimit += CHUNK;
    const body = document.getElementById('dockerSecBody');
    if (body) body.innerHTML = vulnTableHtml(scanOf(S.modalRef));
  };

  // Copy the CVE ids of the open image to the clipboard.
  DS.copyCves = function () {
    const scan = scanOf(S.modalRef);
    const list = ((scan && scan.vulns) || []).filter((v) => /^CVE-/i.test(v.id || '')).map((v) => v.id);
    if (!list.length) return;
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(list.join('\n')).then(() => showToast(t('ds_copied'), 'success'));
    }
  };

  /* ---------- panel ---------- */

  function sectionBodyHtml() {
    return S.section === 'images' ? imageSectionHtml() : auditSectionHtml();
  }

  // The security panel of the Docker tab (the "Security" sub-tab).
  DS.panelHtml = function () {
    const scanAll = `<button class="btn btn-sm btn-ghost" onclick="DS.scanAll()" ${S.busy ? 'disabled' : ''}>${icon('shield')} <span>${esc(t('ds_scan_all'))}</span></button>`;
    const auditBtn = `<button class="btn btn-sm btn-primary" onclick="DS.runAudit()" ${S.busy ? 'disabled' : ''}>${S.job && S.job.kind === 'audit' ? SP.html(t('ds_auditing'), S.job.t0) : icon('shield') + ' <span>' + esc(t('ds_audit')) + '</span>'}</button>`;
    // The error banner sits above the data: a failed reload never hides what is already on screen, and
    // it is never shown as an empty result (panelBodyHtml).
    return `
      <div style="display: flex; justify-content: space-between; align-items: center; gap: 8px; flex-wrap: wrap; margin-bottom: 10px;">
        <h3 style="margin: 0; display: flex; align-items: center; gap: 6px;">${icon('shield')} <span>${esc(t('ds_title'))}</span></h3>
        <div class="dk-toolbar">
          ${scanAll}
          ${auditBtn}
        </div>
      </div>
      <div id="dsJob">${jobHtml()}</div>
      <div id="dsPanel">${panelBodyHtml()}</div>`;
  };

  // Retry a failed load.
  DS.retry = async function () {
    S.error = '';
    const data = await DS.load(S.hostID);
    if (typeof loadDockerTab === 'function') loadDockerTab();
    return data;
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
    // The POST answer is the newest audit: put it in the cache right away. The panel is repainted in
    // place, so the result is visible at once and is never overwritten by a slower GET.
    upsertAudit(a);
    showToast(fill(t('ds_audit_done'), { n: num((a.counts || {}).total || 0) }),
      (a.counts && (a.counts.critical || a.counts.high)) ? 'warning' : 'success');
    repaintPanel();
    DS.loadIntel(host.id);
  };

  DS.scanImage = async function (ref, btn) {
    if (!currentHost || !ref) return;
    const host = currentHost;
    const restore = SP.busy(btn, t('ds_job_scan'));
    setJob({ kind: 'scan', t0: Date.now(), done: 0, total: 0, current: ref });
    let r;
    try {
      r = await api('/api/hosts/' + host.id + '/docker/security/scan-image', {
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
    // The POST answer is the newest scan of that image: put it in the cache right away. The result is
    // visible in the Security panel even when the follow-up GET is slow or fails.
    upsertScan(scan);
    if (scan.status === 'unsupported') {
      showToast(t('ds_scan_missing'), 'warning', 10000);
    } else if (scan.status === 'error') {
      showToast(fill(t('ds_scan_fail'), { ref, why: scan.error || '' }), 'error', 9000);
    } else {
      showToast(fill(t('ds_scan_done'), { ref, n: num((scan.vuln_counts || {}).total || 0) }),
        (scan.vuln_counts && (scan.vuln_counts.critical || scan.vuln_counts.high)) ? 'warning' : 'success');
      // The scan answer carries the exploit intelligence of this image: use it right away.
      if (r.data.intel) mergeIntel(r.data.intel);
    }
    // The panel is repainted in place (no reload of the tab, so the scroll position and the focus stay):
    // the result of the scan is on screen before the follow-up GET is even sent.
    paintChips();
    repaintPanel();
    const body = document.getElementById('dockerSecBody');
    if (body && S.modalRef) body.innerHTML = vulnTableHtml(scanOf(S.modalRef));
    // The follow-up GET only refreshes the stored state (it may be slower than the POST).
    DS.load(host.id).then(() => { paintChips(); repaintPanel(); });
    if (typeof loadDockerTab === 'function' && currentTab === 'docker' && dockerSubTab === 'images') loadDockerTab();
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
        // Every finished scan goes into the cache right away: the panel shows the results while the
        // remaining images are still being scanned.
        upsertScan(scan);
        if (scan.status === 'unsupported') { missing = true; break; }
        total += (scan.vuln_counts || {}).total || 0;
        setJob(Object.assign(S.job, { done: S.job.done + 1 }));
        repaintPanel();
      }
      if (missing) showToast(t('ds_scan_missing'), 'warning', 10000);
      else showToast(fill(t('ds_scan_all_done'), { n: num(imgs.data.images.length), v: num(total) }), 'success');
    } finally {
      S.busy = false;
      setJob(null);
    }
    // The stored state is refreshed in the background: the results are already on screen.
    DS.load(host.id).then(() => { paintChips(); repaintPanel(); });
    DS.loadIntel(host.id);
  };

  // Restore the section the operator left the panel in (per browser session).
  try {
    const saved = sessionStorage.getItem(SECTION_KEY);
    if (saved === 'images' || saved === 'audit') S.section = saved;
  } catch (e) { /* no session storage */ }
})();
