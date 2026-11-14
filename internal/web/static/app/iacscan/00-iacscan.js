/* IaC and secret scan: a button and a result chip on every scanned source (repos, Terraform workspaces,
 * Ansible projects), a results modal with severity counts and filters, and the gate-blocked dialog.
 * Backed by /api/iac-scan. Relies on index.html globals: svgIcon, escapeHtml, t, I18N, openModal,
 * closeModal, showToast, formatRelativeTime. */
(function () {
  'use strict';

  const Ia = (window.IacScan = {});

  /* ---------- i18n ---------- */
  const D = {
    iac_title: ['Security scan', 'Pemindaian keamanan'],
    iac_scan: ['Security scan', 'Pindai keamanan'],
    iac_scanning: ['Scanning…', 'Memindai…'],
    iac_none: ['Not scanned', 'Belum dipindai'],
    iac_findings: ['{n} finding(s)', '{n} temuan'],
    iac_clean: ['No findings', 'Tidak ada temuan'],
    iac_gate_title: ['Security scan blocked this run', 'Pemindaian keamanan memblokir run ini'],
    iac_gate_msg: ['The scan found {critical} critical and {high} high finding(s) in this source. Fix them, or run anyway if you accept the risk.',
      'Pemindaian menemukan {critical} temuan kritis dan {high} temuan tinggi pada sumber ini. Perbaiki, atau jalankan tetap jika Anda menerima risikonya.'],
    iac_run_anyway: ['Run anyway', 'Jalankan tetap'],
    iac_cancel: ['Cancel', 'Batal'],
    iac_apply_anyway: ['Apply anyway', 'Terapkan tetap'],
    iac_filter: ['Filter findings…', 'Filter temuan…'],
    iac_all_categories: ['All categories', 'Semua kategori'],
    iac_all_severities: ['All severities', 'Semua tingkat'],
    iac_col_location: ['Location', 'Lokasi'],
    iac_col_finding: ['Finding', 'Temuan'],
    iac_col_remediation: ['Remediation', 'Perbaikan'],
    iac_no_findings: ['No findings match the filter.', 'Tidak ada temuan yang cocok dengan filter.'],
    iac_scan_failed: ['Scan failed', 'Pemindaian gagal'],
    iac_truncated: ['Some findings were not shown (the scan hit its cap).', 'Sebagian temuan tidak ditampilkan (pemindaian mencapai batas).'],
    iac_partial: ['The scan could not read every file; the result is partial.', 'Pemindaian tidak dapat membaca semua berkas; hasilnya parsial.'],
    iac_never_scanned: ['This source has not been scanned yet.', 'Sumber ini belum dipindai.'],
    iac_secret_redacted: ['Secret values are redacted.', 'Nilai rahasia disamarkan.'],
    iac_files: ['{n} file(s) scanned', '{n} berkas dipindai'],
  };
  for (const [k, v] of Object.entries(D)) { I18N.en[k] = v[0]; I18N.id[k] = v[1]; }

  const esc = (s) => escapeHtml(String(s === null || s === undefined ? '' : s));
  const icon = (n, c) => svgIcon(n, c || 'icon-xs');
  const tx = (key, vars) => {
    let s = t(key, key);
    if (vars) for (const [k, v] of Object.entries(vars)) s = s.split('{' + k + '}').join(v);
    return s;
  };

  const SEV_ORDER = { critical: 0, high: 1, medium: 2, low: 3 };
  const SEV_CLASS = { critical: 'badge-errored', high: 'badge-warning', medium: 'badge-info', low: 'badge-unknown' };
  const CATEGORIES = ['secret', 'terraform', 'ansible', 'dockerfile'];

  async function api(method, url, body) {
    const opt = { method, headers: {} };
    if (body !== undefined) { opt.headers['Content-Type'] = 'application/json'; opt.body = JSON.stringify(body); }
    try {
      const res = await fetch(url, opt);
      const text = await res.text();
      let data = {};
      try { data = text ? JSON.parse(text) : {}; } catch (e) { data = { error: text.trim() }; }
      if (!res.ok && !data.error) data.error = 'HTTP ' + res.status;
      return { ok: res.ok, status: res.status, data };
    } catch (e) {
      return { ok: false, status: 0, data: { error: e.message } };
    }
  }


  // Ia.latest fetches the stored scan of a source (null when there is none).
  Ia.latest = async function (kind, id) {
    const r = await api('GET', '/api/iac-scan/' + kind + '/' + id);
    if (!r.ok) return null;
    return r.data.scan || null;
  };

  // Ia.scan runs a scan of a source now.
  Ia.scan = async function (kind, id, btn) {
    const restore = SP.busy(btn, tx('iac_scanning'));
    if (!btn) showToast(tx('iac_scanning'), 'info');
    let r;
    try {
      r = await api('POST', '/api/iac-scan/' + kind + '/' + id);
    } finally {
      restore();
    }
    if (!r.ok) {
      showToast(tx('iac_scan_failed') + ': ' + (r.data.error || ''), 'error');
      return null;
    }
    const scan = r.data.scan || {};
    const s = scan.summary || {};
    showToast(tx('iac_findings', { n: Number(s.total || 0) }), Number(s.total || 0) > 0 ? 'warning' : 'success');
    Ia.openResults(scan);
    return scan;
  };

  /* ---------- results modal ---------- */
  let filter = { category: '', severity: '', text: '' };

  Ia.openResults = function (scan) {
    const m = document.getElementById('iacScanModal');
    if (!m) {
      const el = document.createElement('div');
      el.id = 'iacScanModal';
      el.className = 'modal';
      el.innerHTML = `
        <div class="modal-content modal-workspace">
          <div class="modal-header">
            <div class="sd-title">${icon('shield-check', 'icon-sm')} <h3 id="iacScanTitle"></h3></div>
            <button class="btn btn-icon btn-sm btn-ghost" onclick="closeModal('iacScanModal')">${icon('x')}</button>
          </div>
          <div class="modal-body" id="iacScanBody"></div>
        </div>`;
      document.body.appendChild(el);
    }
    document.getElementById('iacScanTitle').textContent = tx('iac_title');
    Ia.renderResults(scan);
    openModal('iacScanModal');
  };

  Ia.renderResults = function (scan) {
    const body = document.getElementById('iacScanBody');
    if (!body) return;
    if (!scan) { body.innerHTML = `<div class="iac-empty">${esc(tx('iac_never_scanned'))}</div>`; return; }
    const s = scan.summary || {};
    const findings = scan.findings || [];
    const notes = scan.notes || [];

    const q = filter.text.toLowerCase();
    const shown = findings.filter((f) =>
      (!filter.category || f.category === filter.category) &&
      (!filter.severity || f.severity === filter.severity) &&
      (!q || [f.title, f.detail, f.file, f.rule_id].join(' ').toLowerCase().includes(q)));

    const counts = ['critical', 'high', 'medium', 'low'].map((sev) =>
      `<span class="badge ${SEV_CLASS[sev]}">${esc(sev)}: ${esc(String(Number(s[sev] || 0)))}</span>`).join('');

    const rows = shown.map((f) => `<tr>
      <td class="mono" style="white-space:nowrap;">${esc(f.file)}${f.line ? ':' + esc(f.line) : ''}</td>
      <td><span class="badge ${SEV_CLASS[f.severity] || ''}">${esc(f.severity)}</span> <span class="badge">${esc(f.category)}</span>
        <div><b>${esc(f.title)}</b></div>
        <div style="color:var(--text-muted);">${esc(f.detail)}</div>
        ${f.snippet ? `<div class="iac-snippet mono">${esc(f.snippet)}</div>` : ''}</td>
      <td>${esc(f.remediation)}</td>
    </tr>`).join('');

    body.innerHTML = `
      <div style="display:flex;gap:6px;flex-wrap:wrap;align-items:center;margin-bottom:10px;">${counts}
        <span class="badge">${esc(tx('iac_files', { n: Number(scan.files_scanned || 0) }))}</span>
        ${scan.truncated ? `<span class="badge badge-warning">${esc(tx('iac_truncated'))}</span>` : ''}
        ${scan.status === 'partial' ? `<span class="badge badge-warning">${esc(tx('iac_partial'))}</span>` : ''}
      </div>
      <div style="display:flex;gap:8px;flex-wrap:wrap;margin-bottom:10px;">
        <input type="search" id="iacFilterText" placeholder="${esc(tx('iac_filter'))}" value="${esc(filter.text)}" style="flex:1;min-width:180px;" />
        <select id="iacFilterCat">
          <option value="">${esc(tx('iac_all_categories'))}</option>
          ${CATEGORIES.map((c) => `<option value="${esc(c)}"${filter.category === c ? ' selected' : ''}>${esc(c)}</option>`).join('')}
        </select>
        <select id="iacFilterSev">
          <option value="">${esc(tx('iac_all_severities'))}</option>
          ${['critical', 'high', 'medium', 'low'].map((c) => `<option value="${esc(c)}"${filter.severity === c ? ' selected' : ''}>${esc(c)}</option>`).join('')}
        </select>
      </div>
      ${shown.length ? `<table class="iac-findings-table"><thead><tr>
          <th>${esc(tx('iac_col_location'))}</th><th>${esc(tx('iac_col_finding'))}</th><th>${esc(tx('iac_col_remediation'))}</th>
        </tr></thead><tbody>${rows}</tbody></table>`
        : `<div class="iac-empty">${esc(tx('iac_no_findings'))}</div>`}
      ${notes.length ? `<div style="margin-top:10px;color:var(--text-muted);font-size:var(--fs-xs);">${notes.map((n) => esc(n)).join('<br>')}</div>` : ''}
      <div style="margin-top:10px;color:var(--text-muted);font-size:var(--fs-xs);">${esc(tx('iac_secret_redacted'))}</div>`;

    const txt = document.getElementById('iacFilterText');
    if (txt) txt.addEventListener('input', () => { filter.text = txt.value; Ia.renderResults(scan); const el = document.getElementById('iacFilterText'); if (el) { el.focus(); el.setSelectionRange(el.value.length, el.value.length); } });
    const cat = document.getElementById('iacFilterCat');
    if (cat) cat.addEventListener('change', () => { filter.category = cat.value; Ia.renderResults(scan); });
    const sev = document.getElementById('iacFilterSev');
    if (sev) sev.addEventListener('change', () => { filter.severity = sev.value; Ia.renderResults(scan); });
  };

  /* ---------- gate dialog ---------- */
  // Ia.gateBlocked shows the dialog of a blocked run and calls onAnyway when the user insists.
  Ia.gateBlocked = function (summary, onAnyway) {
    const m = document.getElementById('iacGateModal');
    if (!m) {
      const el = document.createElement('div');
      el.id = 'iacGateModal';
      el.className = 'modal';
      el.innerHTML = `
        <div class="modal-content modal-dialog">
          <div class="modal-header">
            <div class="sd-title">${icon('shield-alert', 'icon-sm')} <h3 id="iacGateTitle"></h3></div>
            <button class="btn btn-icon btn-sm btn-ghost" onclick="closeModal('iacGateModal')">${icon('x')}</button>
          </div>
          <div class="modal-body" id="iacGateBody"></div>
          <div class="modal-footer">
            <button type="button" class="btn btn-sm" id="iacGateCancel">${esc(tx('iac_cancel'))}</button>
            <button type="button" class="btn btn-sm btn-danger" id="iacGateAnyway">${esc(tx('iac_run_anyway'))}</button>
          </div>
        </div>`;
      document.body.appendChild(el);
    }
    document.getElementById('iacGateTitle').textContent = tx('iac_gate_title');
    const s = summary || {};
    const findings = s.findings || [];
    document.getElementById('iacGateBody').innerHTML = `
      <p>${esc(tx('iac_gate_msg', { critical: Number(s.critical || 0), high: Number(s.high || 0) }))}</p>
      ${findings.length ? `<ul style="font-size:var(--fs-xs);">${findings.map((f) =>
        `<li><span class="badge ${SEV_CLASS[f.severity] || ''}">${esc(f.severity)}</span> <span class="mono">${esc(f.file)}${f.line ? ':' + esc(f.line) : ''}</span> ${esc(f.title)}</li>`).join('')}</ul>` : ''}
      <div style="color:var(--text-muted);font-size:var(--fs-xs);">${esc(tx('iac_secret_redacted'))}</div>`;
    document.getElementById('iacGateCancel').onclick = () => closeModal('iacGateModal');
    document.getElementById('iacGateAnyway').onclick = () => { closeModal('iacGateModal'); if (onAnyway) onAnyway(); };
    openModal('iacGateModal');
  };

  // Ia.chipAttrs are the data attributes a scan button carries.
  Ia.scanAttrs = function (kind, id) {
    return `data-iac-scan="${esc(kind)}" data-iac-id="${esc(id)}"`;
  };

  /* ---------- fleet digest ---------- */
  // Ia.fleet loads the digest of every scanned source once; the UI modules use it to render a chip.
  let fleetCache = null;
  Ia.fleet = async function (force) {
    if (fleetCache && !force) return fleetCache;
    const r = await api('GET', '/api/iac-scan');
    if (!r.ok) { fleetCache = {}; return fleetCache; }
    const map = {};
    (r.data.scans || []).forEach((s) => { map[s.source_kind + ':' + s.source_id] = s; });
    fleetCache = map;
    return fleetCache;
  };

  // Ia.chipHtml renders the chip of a source from a fleet digest (empty when it was never scanned).
  Ia.chipHtml = function (kind, id, digest) {
    if (!digest) return '';
    const s = digest.summary || {};
    const total = Number(s.total || 0);
    const crit = Number(s.critical || 0);
    const high = Number(s.high || 0);
    const cls = crit > 0 ? 'badge-errored' : high > 0 ? 'badge-warning' : total > 0 ? 'badge-info' : 'badge-healthy';
    const label = total > 0 ? tx('iac_findings', { n: total }) : tx('iac_clean');
    return `<button type="button" class="badge ${cls}" data-iac-open="${esc(kind)}" data-iac-id="${esc(id)}" title="${esc(tx('iac_title'))}">${icon('shield-check')} ${esc(label)}</button>`;
  };

  // Ia.btnHtml renders the scan button of a source.
  Ia.btnHtml = function (kind, id) {
    return `<button type="button" class="btn btn-sm" data-iac-scan="${esc(kind)}" data-iac-id="${esc(id)}" title="${esc(tx('iac_title'))}">${icon('shield-check')} ${esc(tx('iac_scan'))}</button>`;
  };

  // Ia.handleClick is the shared click handler of the scan buttons and chips.
  Ia.handleClick = function (ev) {
    const el = ev.target.closest('[data-iac-scan],[data-iac-open]');
    if (!el) return false;
    ev.stopPropagation();
    const kind = el.getAttribute('data-iac-scan') || el.getAttribute('data-iac-open');
    const id = el.getAttribute('data-iac-id');
    if (el.hasAttribute('data-iac-scan')) Ia.scan(kind, id, el);
    else Ia.openLatest(kind, id, el);
    return true;
  };

  // Ia.openLatest opens the stored result of a source (or scans it when there is none).
  Ia.openLatest = async function (kind, id, btn) {
    const restore = SP.busy(btn, tx('iac_scanning'));
    try {
      const scan = await Ia.latest(kind, id);
      if (!scan) { await Ia.scan(kind, id, btn); return; }
      Ia.openResults(scan);
    } finally {
      restore();
    }
  };

  // Global handler: every scan button and chip in the dashboard works without extra wiring.
  document.addEventListener('click', function (ev) {
    Ia.handleClick(ev);
  });
})();
