/* Workspace drawer tab "State": lock card, Resources (inspect / mv / rm / import), Versions & Diff (import, compare, rollback). */
(function () {
  'use strict';

  const Tf = window.Tf;
  Tf.addI18n({
    en: {
      tf_ws_tab_state: 'State', tf_st_unlocked: 'Unlocked', tf_st_locked_manual: 'Manually locked', tf_st_locked_run: 'Locked by Run #{id}',
      tf_st_reason: 'Reason', tf_st_btn_lock: 'Lock', tf_st_btn_unlock: 'Unlock', tf_st_btn_force: 'Force Unlock',
      tf_st_lock_title: 'Lock workspace', tf_st_lock_reason: 'Reason (optional)', tf_st_force_title: 'Force unlock workspace?',
      tf_st_force_msg: 'Force unlock removes the lock even if a run or another user is still using the workspace. Use only when the lock is stuck.',
      tf_st_unavail: 'This workspace action is not available right now',
      tf_st_seg_resources: 'Resources', tf_st_seg_versions: 'Versions & Diff', tf_st_meta: 'Serial {serial} · {lineage} · Terraform {tfv}',
      tf_st_filter_ph: 'Filter by address', tf_st_mode_all: 'All modes', tf_st_mode_managed: 'Managed', tf_st_mode_data: 'Data',
      tf_st_provider_all: 'All providers', tf_st_import_resource: 'Import resource', tf_st_col_address: 'Address', tf_st_col_type: 'Type',
      tf_st_col_provider: 'Provider', tf_st_col_mode: 'Mode', tf_st_col_actions: 'Actions', tf_st_inspect: 'Inspect', tf_st_mv: 'Move (mv)',
      tf_st_rm: 'Remove (rm)', tf_st_no_resources: 'No resources in the latest state', tf_st_no_match: 'No resources match the filter',
      tf_st_locked_hint: 'The workspace is locked: state operations are disabled until it is unlocked.',
      tf_st_ops_hint: 'Every state operation shows the exact CLI command and asks for confirmation first.',
      tf_st_sensitive: 'Values of sensitive-looking attributes are masked as (sensitive).', tf_st_copy: 'Copy',
      tf_st_import_title: 'Import resource', tf_st_mv_title: 'Move resource', tf_st_field_address: 'Address', tf_st_field_to: 'Move to',
      tf_st_field_import_id: 'Import ID', tf_st_addr_bad: 'Enter a valid resource address, e.g. aws_instance.web',
      tf_st_id_required: 'Import ID is required', tf_st_confirm_title: 'Run state operation?',
      tf_st_confirm_msg: 'This runs the following command against the workspace state:\n\n{cmd}', tf_st_run: 'Run',
      tf_st_running: 'Running {cmd}', tf_st_result_title: 'State operation', tf_st_failed: 'The operation failed',
      tf_st_done: 'State operation done (version {v})', tf_st_close: 'Close',
      tf_st_import_state: 'Import state', tf_st_import_hint: 'The imported file replaces the latest state. A .tfstate file can contain secrets: only import files from a trusted source.',
      tf_st_file: 'State file (.tfstate / .json)', tf_st_import_empty: 'Choose a file or paste the state JSON', tf_st_import_done: 'State imported',
      tf_st_secret_warn: 'Downloaded state files contain secrets. Store them securely.', tf_st_versions_title: 'State versions',
      tf_st_from: 'Base (From)', tf_st_to: 'Target (To)', tf_st_compare: 'Compare versions', tf_st_select_two: 'Select two different versions',
      tf_st_diff_none: 'No differences between these versions', tf_st_added: 'Added', tf_st_removed: 'Removed', tf_st_changed: 'Changed',
      tf_st_col_serial: 'Serial', tf_st_col_source: 'Source', tf_st_col_resources: 'Resources', tf_st_col_size: 'Size', tf_st_col_sha: 'SHA-256',
      tf_st_col_run: 'Run', tf_st_col_created: 'Created', tf_st_current: 'current', tf_st_download: 'Download', tf_st_rollback: 'Rollback',
      tf_st_rollback_title: 'Rollback state version', tf_st_rollback_msg: 'Roll back the state to serial #{serial}? A new version is created from it.',
      tf_st_comment: 'Comment (required)', tf_st_comment_required: 'A comment of at least 3 characters is required', tf_st_rollback_done: 'State rolled back',
      tf_st_no_versions: 'No state versions yet', tf_st_copied_sha: 'SHA-256 copied',
    },
    id: {
      tf_ws_tab_state: 'State', tf_st_unlocked: 'Tidak terkunci', tf_st_locked_manual: 'Dikunci manual', tf_st_locked_run: 'Dikunci oleh Run #{id}',
      tf_st_reason: 'Alasan', tf_st_btn_lock: 'Kunci', tf_st_btn_unlock: 'Buka kunci', tf_st_btn_force: 'Paksa buka kunci',
      tf_st_lock_title: 'Kunci workspace', tf_st_lock_reason: 'Alasan (opsional)', tf_st_force_title: 'Paksa buka kunci workspace?',
      tf_st_force_msg: 'Paksa buka kunci menghapus kunci walaupun run atau pengguna lain masih memakai workspace. Gunakan hanya jika kunci macet.',
      tf_st_unavail: 'Aksi workspace ini belum tersedia',
      tf_st_seg_resources: 'Resource', tf_st_seg_versions: 'Versi & Diff', tf_st_meta: 'Serial {serial} · {lineage} · Terraform {tfv}',
      tf_st_filter_ph: 'Filter berdasarkan alamat', tf_st_mode_all: 'Semua mode', tf_st_mode_managed: 'Managed', tf_st_mode_data: 'Data',
      tf_st_provider_all: 'Semua provider', tf_st_import_resource: 'Impor resource', tf_st_col_address: 'Alamat', tf_st_col_type: 'Tipe',
      tf_st_col_provider: 'Provider', tf_st_col_mode: 'Mode', tf_st_col_actions: 'Aksi', tf_st_inspect: 'Periksa', tf_st_mv: 'Pindah (mv)',
      tf_st_rm: 'Hapus (rm)', tf_st_no_resources: 'Tidak ada resource di state terbaru', tf_st_no_match: 'Tidak ada resource yang cocok dengan filter',
      tf_st_locked_hint: 'Workspace terkunci: operasi state dinonaktifkan sampai kunci dibuka.',
      tf_st_ops_hint: 'Setiap operasi state menampilkan perintah CLI yang persis dan meminta konfirmasi terlebih dahulu.',
      tf_st_sensitive: 'Nilai atribut yang tampak sensitif disamarkan sebagai (sensitive).', tf_st_copy: 'Salin',
      tf_st_import_title: 'Impor resource', tf_st_mv_title: 'Pindahkan resource', tf_st_field_address: 'Alamat', tf_st_field_to: 'Pindahkan ke',
      tf_st_field_import_id: 'ID impor', tf_st_addr_bad: 'Masukkan alamat resource yang valid, contoh aws_instance.web',
      tf_st_id_required: 'ID impor wajib diisi', tf_st_confirm_title: 'Jalankan operasi state?',
      tf_st_confirm_msg: 'Ini menjalankan perintah berikut terhadap state workspace:\n\n{cmd}', tf_st_run: 'Jalankan',
      tf_st_running: 'Menjalankan {cmd}', tf_st_result_title: 'Operasi state', tf_st_failed: 'Operasi gagal',
      tf_st_done: 'Operasi state selesai (versi {v})', tf_st_close: 'Tutup',
      tf_st_import_state: 'Impor state', tf_st_import_hint: 'File yang diimpor menggantikan state terbaru. File .tfstate dapat berisi rahasia: impor hanya dari sumber tepercaya.',
      tf_st_file: 'File state (.tfstate / .json)', tf_st_import_empty: 'Pilih file atau tempel JSON state', tf_st_import_done: 'State diimpor',
      tf_st_secret_warn: 'File state yang diunduh berisi rahasia. Simpan dengan aman.', tf_st_versions_title: 'Versi state',
      tf_st_from: 'Dasar (From)', tf_st_to: 'Target (To)', tf_st_compare: 'Bandingkan versi', tf_st_select_two: 'Pilih dua versi yang berbeda',
      tf_st_diff_none: 'Tidak ada perbedaan antara versi ini', tf_st_added: 'Ditambahkan', tf_st_removed: 'Dihapus', tf_st_changed: 'Diubah',
      tf_st_col_serial: 'Serial', tf_st_col_source: 'Sumber', tf_st_col_resources: 'Resource', tf_st_col_size: 'Ukuran', tf_st_col_sha: 'SHA-256',
      tf_st_col_run: 'Run', tf_st_col_created: 'Dibuat', tf_st_current: 'terkini', tf_st_download: 'Unduh', tf_st_rollback: 'Rollback',
      tf_st_rollback_title: 'Rollback versi state', tf_st_rollback_msg: 'Kembalikan state ke serial #{serial}? Versi baru dibuat dari situ.',
      tf_st_comment: 'Komentar (wajib)', tf_st_comment_required: 'Komentar minimal 3 karakter wajib diisi', tf_st_rollback_done: 'State dikembalikan',
      tf_st_no_versions: 'Belum ada versi state', tf_st_copied_sha: 'SHA-256 disalin',
    },
  });

  // Attribute names whose values are never shown (inspect dialog, diff). Heuristic: the state file has no sensitive flags here.
  const SENS = /password|passwd|secret|token|private[_-]?key|credential|access[_-]?key|kubeconfig|client[_-]?key/i;
  const T = function (k, fb, v) { return Tf.t(k, fb, v); };
  const esc = Tf.esc;

  // Mask sensitive-looking values; the result is what the UI shows and copies.
  function mask(v, key) {
    if (key && SENS.test(key) && v !== null && v !== undefined && v !== '') return '(sensitive)';
    if (Array.isArray(v)) return v.map(function (x) { return mask(x, key); });
    if (v && typeof v === 'object') { const o = {}; Object.keys(v).forEach(function (k) { o[k] = mask(v[k], k); }); return o; }
    return v;
  }
  function show(v) { return v === undefined ? '' : typeof v === 'string' ? v : JSON.stringify(v); }
  function ok(a) { return !!a && !a.startsWith('-') && /^[^\s'"`$;&|<>\\(){}*?!#~]+$/.test(a.replace(/\["[^"\\]*"\]/g, '[]')); }

  let cur = null;     // { body, wsId, ws, seg, res, versions, meta, filt }
  let busy = false;   // one state operation at a time
  let formDlg = null; // the open form dialog (mv / import resource), closed after a successful op
  let stateDlg = null;

  /* ---------- lock card (also reused by the overview) ---------- */
  function lockCardHtml(ws) {
    const btn = function (act, icon, label, danger) {
      return '<button type="button" class="btn btn-sm' + (danger ? ' btn-danger' : '') + '" data-st-act="' + act + '" data-ws="' + ws.id + '">' + Tf.icon(icon, 'icon-xs') + ' ' + esc(label) + '</button>';
    };
    let title, desc, btns;
    if (!ws.locked) {
      title = T('tf_st_unlocked', 'Unlocked'); desc = ''; btns = btn('lock-open', 'lock', T('tf_st_btn_lock', 'Lock'));
    } else if (/^run:/.test(ws.locked_by || '')) {
      title = T('tf_st_locked_run', 'Locked by Run #{id}', { id: String(ws.locked_by).replace(/^run:/, '') });
      desc = ''; btns = btn('force', 'triangle-alert', T('tf_st_btn_force', 'Force Unlock'), true);
    } else {
      title = T('tf_st_locked_manual', 'Manually locked'); desc = ws.lock_reason ? T('tf_st_reason', 'Reason') + ': ' + ws.lock_reason : '';
      btns = btn('unlock', 'lock-open', T('tf_st_btn_unlock', 'Unlock')) + btn('force', 'triangle-alert', T('tf_st_btn_force', 'Force Unlock'), true);
    }
    return '<div class="card tf-st-lock" id="tfStateLockCard">' +
      '<div class="tf-st-lock-info">' + Tf.icon(ws.locked ? 'lock' : 'lock-open', 'icon-sm') +
      '<div><b>' + esc(title) + '</b>' + (desc ? '<div class="tf-muted tf-hint">' + esc(desc) + '</div>' : '') + '</div></div>' +
      '<div class="tf-row">' + btns + '</div></div>';
  }
  Tf.stateTab = { lockCardHtml: lockCardHtml, mask: mask };

  function callWs(method, ws, arg) {
    const f = Tf.workspaces && Tf.workspaces[method];
    if (typeof f !== 'function') throw new Error(T('tf_st_unavail', 'This workspace action is not available right now'));
    return f(ws, arg);
  }
  async function freshWs(id) { const r = await Tf.api.get('/api/terraform/workspaces/' + id); return r.workspace; }

  async function refreshTab() {
    if (!cur || !cur.body.isConnected) return;
    const ws = await freshWs(cur.wsId);
    renderTab(cur.body, ws);
  }

  /* ---------- tab render ---------- */
  function renderTab(body, ws) {
    const keepSeg = cur && cur.wsId === ws.id ? cur.seg : 'resources';
    cur = { body: body, wsId: ws.id, ws: ws, seg: keepSeg, res: [], versions: [], meta: {}, filt: { q: '', mode: '', provider: '' } };
    body.setAttribute('data-st-root', '');
    body.innerHTML = lockCardHtml(ws) +
      '<div class="tf-st-segs" role="tablist">' +
      '<button type="button" class="tab' + (keepSeg === 'resources' ? ' active' : '') + '" role="tab" data-st-act="seg" data-seg="resources">' + esc(T('tf_st_seg_resources', 'Resources')) + '</button>' +
      '<button type="button" class="tab' + (keepSeg === 'versions' ? ' active' : '') + '" role="tab" data-st-act="seg" data-seg="versions">' + esc(T('tf_st_seg_versions', 'Versions & Diff')) + '</button></div>' +
      '<div id="tfStateSeg">' + Tf.ui.skeleton(4) + '</div>';
    loadData(body);
  }

  async function loadData(body) {
    const id = cur.wsId;
    try {
      const [sr, vr] = await Promise.all([Tf.api.get('/api/terraform/workspaces/' + id + '/state'), Tf.api.get('/api/terraform/workspaces/' + id + '/state/versions')]);
      if (!cur || cur.body !== body) return;
      cur.res = sr.resources || [];
      cur.meta = { serial: sr.serial, lineage: sr.lineage, tfv: sr.terraform_version || '' };
      cur.versions = vr.versions || [];
    } catch (e) {
      if (!cur || cur.body !== body) return;
      const seg = document.getElementById('tfStateSeg');
      if (seg) seg.innerHTML = Tf.ui.error(Tf.errMsg(e), 'data-st-act="reload"');
      return;
    }
    renderSeg();
  }

  function renderSeg() {
    const seg = document.getElementById('tfStateSeg');
    if (!seg || !cur) return;
    seg.innerHTML = cur.seg === 'versions' ? versionsHtml() : resourcesHtml();
    if (cur.seg === 'resources') renderResRows();
    else initCompare();
  }

  /* ---------- Resources ---------- */
  function resourcesHtml() {
    const locked = !!cur.ws.locked;
    const providers = Array.from(new Set(cur.res.map(function (r) { return r.provider || ''; }).filter(Boolean))).sort();
    const meta = cur.meta.serial !== undefined ? T('tf_st_meta', '', { serial: cur.meta.serial, lineage: cur.meta.lineage || '-', tfv: cur.meta.tfv || '-' }) : '';
    if (!cur.res.length) {
      return '<div class="tf-st-meta tf-mono tf-muted">' + esc(meta) + '</div>' + Tf.ui.empty({ icon: 'database', title: T('tf_st_no_resources', 'No resources in the latest state') });
    }
    return '<div class="tf-st-meta tf-mono tf-muted">' + esc(meta) + '</div>' +
      '<div class="tf-st-bar tf-row">' +
      '<input type="search" class="form-input tf-st-q" id="tfStResQ" data-st-filter="q" placeholder="' + esc(T('tf_st_filter_ph', 'Filter by address')) + '" value="' + esc(cur.filt.q) + '" autocomplete="off" />' +
      '<select class="form-input" data-st-filter="mode" aria-label="' + esc(T('tf_st_col_mode', 'Mode')) + '">' +
      '<option value="">' + esc(T('tf_st_mode_all', 'All modes')) + '</option>' +
      '<option value="managed"' + (cur.filt.mode === 'managed' ? ' selected' : '') + '>' + esc(T('tf_st_mode_managed', 'Managed')) + '</option>' +
      '<option value="data"' + (cur.filt.mode === 'data' ? ' selected' : '') + '>' + esc(T('tf_st_mode_data', 'Data')) + '</option></select>' +
      '<select class="form-input" data-st-filter="provider" aria-label="' + esc(T('tf_st_col_provider', 'Provider')) + '">' +
      '<option value="">' + esc(T('tf_st_provider_all', 'All providers')) + '</option>' +
      providers.map(function (p) { return '<option value="' + esc(p) + '"' + (cur.filt.provider === p ? ' selected' : '') + '>' + esc(p) + '</option>'; }).join('') +
      '</select>' +
      '<button type="button" class="btn btn-sm" data-st-act="res-import"' + (locked ? ' disabled' : '') + '>' + Tf.icon('download', 'icon-xs') + ' ' + esc(T('tf_st_import_resource', 'Import resource')) + '</button></div>' +
      (locked ? '<div class="tf-hint tf-muted">' + esc(T('tf_st_locked_hint', '')) + '</div>' : '<div class="tf-hint tf-muted">' + esc(T('tf_st_ops_hint', '')) + '</div>') +
      '<div class="tf-st-table-wrap"><table class="tf-st-table"><thead><tr>' +
      '<th>' + esc(T('tf_st_col_address', 'Address')) + '</th><th>' + esc(T('tf_st_col_type', 'Type')) + '</th><th>' + esc(T('tf_st_col_provider', 'Provider')) + '</th>' +
      '<th>' + esc(T('tf_st_col_mode', 'Mode')) + '</th><th class="tf-st-acts-h">' + esc(T('tf_st_col_actions', 'Actions')) + '</th></tr></thead>' +
      '<tbody id="tfStResBody"></tbody></table></div>';
  }

  function renderResRows() {
    const tb = document.getElementById('tfStResBody');
    if (!tb || !cur) return;
    const q = cur.filt.q.toLowerCase().trim();
    const list = cur.res.filter(function (r) {
      return (!q || r.address.toLowerCase().indexOf(q) >= 0) &&
        (!cur.filt.mode || r.mode === cur.filt.mode) &&
        (!cur.filt.provider || r.provider === cur.filt.provider);
    });
    if (!list.length) { tb.innerHTML = '<tr><td colspan="5">' + Tf.ui.empty({ title: T('tf_st_no_match', 'No resources match the filter') }) + '</td></tr>'; return; }
    const locked = !!cur.ws.locked;
    tb.innerHTML = list.map(function (r) {
      const a = esc(r.address);
      return '<tr><td class="tf-mono tf-st-addr">' + a + '</td><td>' + esc(r.type) + '</td><td class="tf-st-prov tf-mono">' + esc(r.provider || '') + '</td><td>' + esc(r.mode || 'managed') + '</td>' +
        '<td class="tf-st-acts"><div class="tf-row">' +
        '<button type="button" class="btn btn-sm" data-st-act="inspect" data-addr="' + a + '">' + esc(T('tf_st_inspect', 'Inspect')) + '</button>' +
        (r.mode === 'data' ? '' :
          '<button type="button" class="btn btn-sm" data-st-act="mv" data-addr="' + a + '"' + (locked ? ' disabled' : '') + '>' + esc(T('tf_st_mv', 'Move (mv)')) + '</button>' +
          '<button type="button" class="btn btn-sm btn-danger" data-st-act="rm" data-addr="' + a + '"' + (locked ? ' disabled' : '') + '>' + Tf.icon('trash-2', 'icon-xs') + ' ' + esc(T('tf_st_rm', 'Remove (rm)')) + '</button>') +
        '</div></td></tr>';
    }).join('');
  }

  function findRes(addr) { return cur.res.find(function (r) { return r.address === addr; }); }

  function openInspect(addr) {
    const r = findRes(addr);
    if (!r) return;
    const json = JSON.stringify(mask(r.attributes || {}), null, 2);
    stateDlg = Tf.dlg.open({
      title: addr, width: 760,
      body: '<div class="tf-hint tf-muted">' + esc(T('tf_st_sensitive', '')) + '</div><pre class="tf-st-json">' + esc(json) + '</pre>',
      footer: '<button type="button" class="btn btn-sm" data-st-act="copy-json">' + Tf.icon('copy', 'icon-xs') + ' ' + esc(T('tf_st_copy', 'Copy')) + '</button>' +
        '<button type="button" class="btn btn-sm btn-primary" data-dlg-close>' + esc(T('tf_st_close', 'Close')) + '</button>',
    });
    stateDlg.root.setAttribute('data-st-root', '');
  }

  function openForm(kind, addr) {
    const mv = kind === 'mv';
    const fields = mv
      ? '<label class="tf-field"><span class="tf-field-label">' + esc(T('tf_st_field_address', 'Address')) + '</span><input class="form-input tf-mono" id="tfStAddr" value="' + esc(addr) + '" readonly /></label>' +
        '<label class="tf-field"><span class="tf-field-label">' + esc(T('tf_st_field_to', 'Move to')) + '</span><input class="form-input tf-mono" id="tfStTo" autocomplete="off" /></label>'
      : '<label class="tf-field"><span class="tf-field-label">' + esc(T('tf_st_field_address', 'Address')) + '</span><input class="form-input tf-mono" id="tfStAddr" autocomplete="off" placeholder="aws_instance.web" /></label>' +
        '<label class="tf-field"><span class="tf-field-label">' + esc(T('tf_st_field_import_id', 'Import ID')) + '</span><input class="form-input tf-mono" id="tfStImpId" autocomplete="off" /></label>';
    formDlg = Tf.dlg.open({
      title: T(mv ? 'tf_st_mv_title' : 'tf_st_import_title', mv ? 'Move resource' : 'Import resource'), width: 560,
      body: '<div class="tf-grid-2">' + fields + '</div>',
      footer: '<button type="button" class="btn btn-sm" data-dlg-close>' + esc(T('tf_st_close', 'Close')) + '</button>' +
        '<button type="button" class="btn btn-sm btn-primary" data-st-act="' + (mv ? 'mv-submit' : 'res-import-submit') + '">' + esc(T('tf_st_run', 'Run')) + '</button>',
    });
    formDlg.root.setAttribute('data-st-root', '');
  }

  // Validates, asks for confirmation with the exact command, runs the op and shows progress/errors in a dialog.
  async function runOp(op, addr, to, id) {
    if (busy || !cur) return;
    const bin = cur.ws.binary || 'terraform';
    const cmd = op === 'rm' ? bin + ' state rm ' + addr : op === 'mv' ? bin + ' state mv ' + addr + ' ' + to : bin + ' import ' + addr + ' ' + id;
    const sure = await Tf.confirm({ title: T('tf_st_confirm_title', 'Run state operation?'), message: T('tf_st_confirm_msg', '', { cmd: cmd }), confirmText: T('tf_st_run', 'Run'), danger: op !== 'import' });
    if (!sure) return;
    busy = true;
    const t0 = Date.now();
    const p = Tf.dlg.open({
      title: T('tf_st_result_title', 'State operation'), width: 720,
      body: '<div class="tf-st-run">' + Tf.icon('loader-circle', 'icon-sm tf-spin') + ' <span>' + esc(T('tf_st_running', '', { cmd: cmd })) + '</span> <span class="tf-mono tf-muted" data-st-elapsed>0s</span></div>',
      footer: '',
    });
    p.root.setAttribute('data-st-root', '');
    const timer = setInterval(function () {
      const el = p.root.querySelector('[data-st-elapsed]');
      if (el) el.textContent = Math.round((Date.now() - t0) / 1000) + 's';
    }, 1000);
    try {
      const res = await Tf.api.post('/api/terraform/workspaces/' + cur.wsId + '/state/ops', { op: op, address: addr, to: to || '', import_id: id || '' });
      p.close();
      if (formDlg) formDlg.close();
      Tf.toast(T('tf_st_done', '', { v: res.state_version }), 'success');
      await refreshTab();
    } catch (e) {
      const d = e.data || {};
      p.body.innerHTML = '<div class="tf-error" role="alert">' + Tf.icon('triangle-alert', 'icon-sm') + ' <span>' + esc(T('tf_st_failed', 'The operation failed')) + ': ' + esc(d.error || Tf.errMsg(e)) + '</span></div>' +
        (d.output ? '<pre class="tf-st-json">' + esc(d.output) + '</pre>' : '');
      p.footer.innerHTML = '<button type="button" class="btn btn-sm btn-primary" data-dlg-close>' + esc(T('tf_st_close', 'Close')) + '</button>';
    } finally {
      clearInterval(timer);
      busy = false;
    }
  }

  function submitForm(kind, dlg) {
    const addr = (dlg.root.querySelector('#tfStAddr') || {}).value || '';
    const a = addr.trim();
    if (kind === 'mv') {
      const to = ((dlg.root.querySelector('#tfStTo') || {}).value || '').trim();
      if (!ok(a) || !ok(to)) { dlg.error(T('tf_st_addr_bad', 'Enter a valid resource address, e.g. aws_instance.web')); return; }
      runOp('mv', a, to, '');
    } else {
      const id = ((dlg.root.querySelector('#tfStImpId') || {}).value || '').trim();
      if (!ok(a)) { dlg.error(T('tf_st_addr_bad', 'Enter a valid resource address, e.g. aws_instance.web')); return; }
      if (!id) { dlg.error(T('tf_st_id_required', 'Import ID is required')); return; }
      runOp('import', a, '', id);
    }
  }

  /* ---------- Versions & Diff ---------- */
  function versionsHtml() {
    const vs = cur.versions;
    const locked = !!cur.ws.locked;
    const head = '<div class="tf-row tf-st-bar"><span class="tf-hint tf-muted">' + esc(T('tf_st_secret_warn', '')) + '</span>' +
      '<button type="button" class="btn btn-sm" data-st-act="state-import"' + (locked ? ' disabled' : '') + '>' + Tf.icon('upload', 'icon-xs') + ' ' + esc(T('tf_st_import_state', 'Import state')) + '</button></div>';
    if (!vs.length) return head + Tf.ui.empty({ icon: 'database', title: T('tf_st_no_versions', 'No state versions yet') });
    const opt = function (v, i) { return '<option value="' + v.id + '">#' + v.serial + ' · ' + esc(v.source) + (i === 0 ? ' (' + esc(T('tf_st_current', 'current')) + ')' : '') + '</option>'; };
    const cmp = '<div class="tf-row tf-st-bar">' +
      '<label class="tf-field"><span class="tf-field-label">' + esc(T('tf_st_from', 'Base (From)')) + '</span><select class="form-input" id="tfStateFrom">' + vs.map(opt).join('') + '</select></label>' +
      '<label class="tf-field"><span class="tf-field-label">' + esc(T('tf_st_to', 'Target (To)')) + '</span><select class="form-input" id="tfStateTo">' + vs.map(opt).join('') + '</select></label>' +
      '<button type="button" class="btn btn-sm btn-primary" id="tfStateCompareBtn" data-st-act="compare">' + Tf.icon('git-compare', 'icon-xs') + ' ' + esc(T('tf_st_compare', 'Compare versions')) + '</button></div>' +
      '<div id="tfStateDiffOut"></div>';
    const rows = vs.map(function (v, i) {
      const sha = v.sha256 || '';
      return '<tr><td class="tf-mono">#' + v.serial + (i === 0 ? ' <span class="badge badge-healthy">' + esc(T('tf_st_current', 'current')) + '</span>' : '') + '</td>' +
        '<td>' + esc(v.source) + '</td><td>' + (v.resource_count || 0) + '</td><td class="tf-mono">' + esc(Tf.fmt.bytes(v.size)) + '</td>' +
        '<td class="tf-mono" title="' + esc(sha) + '">' + esc(sha.slice(0, 12)) + (sha ? ' <button type="button" class="btn btn-sm btn-ghost btn-icon" data-st-act="copy-sha" data-sha="' + esc(sha) + '" aria-label="' + esc(T('tf_st_copy', 'Copy')) + '">' + Tf.icon('copy', 'icon-xs') + '</button>' : '') + '</td>' +
        '<td>' + (v.run_id ? '<button type="button" class="btn btn-sm btn-ghost" data-st-act="run" data-run="' + v.run_id + '">#' + v.run_id + '</button>' : '—') + '</td>' +
        '<td class="tf-mono">' + esc(Tf.fmt.dt(v.created_at)) + '</td>' +
        '<td class="tf-st-acts"><div class="tf-row">' +
        '<a class="btn btn-sm btn-icon" href="/api/terraform/workspaces/' + cur.wsId + '/state/versions/' + v.id + '" download title="' + esc(T('tf_st_download', 'Download')) + '">' + Tf.icon('download', 'icon-xs') + '</a>' +
        (i === 0 ? '' : '<button type="button" class="btn btn-sm btn-danger" data-st-act="rollback-open" data-vid="' + v.id + '" data-serial="' + v.serial + '"' + (locked ? ' disabled' : '') + '>' + esc(T('tf_st_rollback', 'Rollback')) + '</button>') +
        '</div></td></tr>';
    }).join('');
    return head + cmp +
      '<div class="tf-st-table-wrap"><table class="tf-st-table"><thead><tr><th>' + esc(T('tf_st_col_serial', 'Serial')) + '</th><th>' + esc(T('tf_st_col_source', 'Source')) + '</th>' +
      '<th>' + esc(T('tf_st_col_resources', 'Resources')) + '</th><th>' + esc(T('tf_st_col_size', 'Size')) + '</th><th>' + esc(T('tf_st_col_sha', 'SHA-256')) + '</th>' +
      '<th>' + esc(T('tf_st_col_run', 'Run')) + '</th><th>' + esc(T('tf_st_col_created', 'Created')) + '</th><th class="tf-st-acts-h"></th></tr></thead><tbody>' + rows + '</tbody></table></div>';
  }

  function initCompare() {
    const from = document.getElementById('tfStateFrom'), to = document.getElementById('tfStateTo');
    if (from && cur.versions.length > 1) from.value = String(cur.versions[1].id);
    if (to && cur.versions.length) to.value = String(cur.versions[0].id);
  }

  async function compare() {
    const f = document.getElementById('tfStateFrom'), t2 = document.getElementById('tfStateTo'), out = document.getElementById('tfStateDiffOut');
    if (!f || !t2 || !out) return;
    if (!f.value || !t2.value || f.value === t2.value) { Tf.toast(T('tf_st_select_two', 'Select two different versions'), 'warning'); return; }
    out.innerHTML = Tf.ui.skeleton(2);
    try {
      const d = await Tf.api.get('/api/terraform/workspaces/' + cur.wsId + '/state/diff?from=' + encodeURIComponent(f.value) + '&to=' + encodeURIComponent(t2.value));
      out.innerHTML = diffHtml(d);
    } catch (e) { out.innerHTML = Tf.ui.error(Tf.errMsg(e)); }
  }

  function diffHtml(d) {
    const added = d.added || [], removed = d.removed || [], changed = d.changed || [];
    if (!added.length && !removed.length && !changed.length) return Tf.ui.empty({ title: T('tf_st_diff_none', 'No differences between these versions') });
    return '<div class="tf-st-diff">' +
      (added.length ? '<div class="tf-st-diff-h">' + esc(T('tf_st_added', 'Added')) + ' (' + added.length + ')</div>' + added.map(function (a) { return '<div class="tf-st-add tf-mono">+ ' + esc(a) + '</div>'; }).join('') : '') +
      (removed.length ? '<div class="tf-st-diff-h">' + esc(T('tf_st_removed', 'Removed')) + ' (' + removed.length + ')</div>' + removed.map(function (a) { return '<div class="tf-st-del tf-mono">- ' + esc(a) + '</div>'; }).join('') : '') +
      (changed.length ? '<div class="tf-st-diff-h">' + esc(T('tf_st_changed', 'Changed')) + ' (' + changed.length + ')</div>' + changed.map(function (c) {
        const lines = (c.changes || []).map(function (x) {
          const v = SENS.test(x.path || '') ? '(sensitive)' : null;
          const b = v || show(mask(x.before, x.path)), a = v || show(mask(x.after, x.path));
          return '<div class="tf-st-mod tf-mono">~ ' + esc(x.path) + ': ' + esc(b) + ' → ' + esc(a) + '</div>';
        }).join('');
        return '<details class="tf-st-chg"><summary class="tf-mono">~ ' + esc(c.address) + '</summary>' + lines + '</details>';
      }).join('') : '') + '</div>';
  }

  /* ---------- rollback and state import ---------- */
  function openRollback(vid, serial) {
    const d = Tf.dlg.open({
      title: T('tf_st_rollback_title', 'Rollback state version'), width: 480,
      body: '<div class="tf-muted tf-hint">' + esc(T('tf_st_rollback_msg', '', { serial: serial })) + '</div>' +
        '<label class="tf-field"><span class="tf-field-label">' + esc(T('tf_st_comment', 'Comment (required)')) + '</span><input class="form-input" id="tfStRbComment" maxlength="200" autocomplete="off" /></label>',
      footer: '<button type="button" class="btn btn-sm" data-dlg-close>' + esc(T('tf_st_close', 'Close')) + '</button>' +
        '<button type="button" class="btn btn-sm btn-danger" data-st-act="rollback-submit" data-vid="' + vid + '">' + esc(T('tf_st_rollback', 'Rollback')) + '</button>',
    });
    d.root.setAttribute('data-st-root', '');
    stateDlg = d;
  }

  async function submitRollback(vid, dlg) {
    const c = ((dlg.root.querySelector('#tfStRbComment') || {}).value || '').trim();
    if (c.length < 3) { dlg.error(T('tf_st_comment_required', 'A comment of at least 3 characters is required')); return; }
    try {
      await Tf.api.post('/api/terraform/workspaces/' + cur.wsId + '/state/versions/' + vid + '/rollback', { comment: c });
      dlg.close();
      Tf.toast(T('tf_st_rollback_done', 'State rolled back'), 'success');
      await refreshTab();
    } catch (e) { dlg.error(Tf.errMsg(e)); }
  }

  function openImport() {
    const d = Tf.dlg.open({
      title: T('tf_st_import_state', 'Import state'), width: 640,
      body: '<div class="tf-st-warn tf-hint">' + Tf.icon('triangle-alert', 'icon-xs') + ' ' + esc(T('tf_st_import_hint', '')) + '</div>' +
        '<label class="tf-field"><span class="tf-field-label">' + esc(T('tf_st_file', 'State file (.tfstate / .json)')) + '</span><input type="file" id="tfStImpFile" accept=".tfstate,.json,application/json" data-st-file /></label>' +
        '<textarea id="tfStImpText" class="form-input tf-st-text" rows="12" spellcheck="false"></textarea>',
      footer: '<button type="button" class="btn btn-sm" data-dlg-close>' + esc(T('tf_st_close', 'Close')) + '</button>' +
        '<button type="button" class="btn btn-sm btn-primary" data-st-act="state-import-submit">' + esc(T('tf_st_import_state', 'Import state')) + '</button>',
    });
    d.root.setAttribute('data-st-root', '');
    stateDlg = d;
  }

  async function submitImport(dlg) {
    const state = ((dlg.root.querySelector('#tfStImpText') || {}).value || '').trim();
    if (!state) { dlg.error(T('tf_st_import_empty', 'Choose a file or paste the state JSON')); return; }
    try {
      await Tf.api.post('/api/terraform/workspaces/' + cur.wsId + '/state/import', { state: state });
      dlg.close();
      Tf.toast(T('tf_st_import_done', 'State imported'), 'success');
      await refreshTab();
    } catch (e) { dlg.error(Tf.errMsg(e)); }
  }

  /* ---------- lock actions ---------- */
  function openLock(ws) {
    const d = Tf.dlg.open({
      title: T('tf_st_lock_title', 'Lock workspace'), width: 460,
      body: '<label class="tf-field"><span class="tf-field-label">' + esc(T('tf_st_lock_reason', 'Reason (optional)')) + '</span><input class="form-input" id="tfStLockReason" maxlength="200" autocomplete="off" /></label>',
      footer: '<button type="button" class="btn btn-sm" data-dlg-close>' + esc(T('tf_st_close', 'Close')) + '</button>' +
        '<button type="button" class="btn btn-sm btn-primary" data-st-act="lock-submit" data-ws="' + ws.id + '">' + esc(T('tf_st_btn_lock', 'Lock')) + '</button>',
    });
    d.root.setAttribute('data-st-root', '');
    stateDlg = d;
  }

  async function lockAction(act, id, dlg) {
    try {
      const ws = await freshWs(id);
      if (act === 'lock') await callWs('lock', ws, ((dlg && dlg.root.querySelector('#tfStLockReason')) || {}).value || '');
      else if (act === 'unlock') await callWs('unlock', ws);
      else if (act === 'force') {
        const sure = await Tf.confirm({ title: T('tf_st_force_title', 'Force unlock workspace?'), message: T('tf_st_force_msg', ''), confirmText: T('tf_st_btn_force', 'Force Unlock'), danger: true });
        if (!sure) return;
        await callWs('forceUnlock', ws);
      }
      if (dlg) dlg.close();
      await refreshTab();
    } catch (e) {
      if (dlg) dlg.error(Tf.errMsg(e)); else Tf.toast(Tf.errMsg(e), 'error');
    }
  }

  /* ---------- delegated events (one listener; tab and dialogs are marked data-st-root) ---------- */
  document.addEventListener('click', function (e) {
    const b = e.target.closest('[data-st-act]');
    if (!b || !b.closest('[data-st-root]')) return;
    const act = b.getAttribute('data-st-act');
    const dlg = [formDlg, stateDlg].find(function (d) { return d && d.root === b.closest('.tf-dlg'); }) || null;
    if (act === 'seg') { cur.seg = b.getAttribute('data-seg'); renderSegButtons(); renderSeg(); }
    else if (act === 'lock-open') openLock(cur.ws);
    else if (act === 'lock-submit') lockAction('lock', b.getAttribute('data-ws'), dlg);
    else if (act === 'unlock') lockAction('unlock', b.getAttribute('data-ws'), null);
    else if (act === 'force') lockAction('force', b.getAttribute('data-ws'), null);
    else if (act === 'reload') { if (cur) loadData(cur.body); }
    else if (act === 'res-import') openForm('import', '');
    else if (act === 'inspect') openInspect(b.getAttribute('data-addr'));
    else if (act === 'mv') openForm('mv', b.getAttribute('data-addr'));
    else if (act === 'rm') { const a = b.getAttribute('data-addr'); runOp('rm', a, '', ''); }
    else if (act === 'mv-submit') submitForm('mv', dlg);
    else if (act === 'res-import-submit') submitForm('import', dlg);
    else if (act === 'copy-json') { const pre = b.closest('[data-st-root]').querySelector('.tf-st-json'); if (pre) Tf.copy(pre.textContent); }
    else if (act === 'copy-sha') Tf.copy(b.getAttribute('data-sha'));
    else if (act === 'run') { if (Tf.runs && typeof Tf.runs.openViewer === 'function') Tf.runs.openViewer(Number(b.getAttribute('data-run'))); }
    else if (act === 'compare') compare();
    else if (act === 'rollback-open') openRollback(Number(b.getAttribute('data-vid')), b.getAttribute('data-serial'));
    else if (act === 'rollback-submit') submitRollback(Number(b.getAttribute('data-vid')), dlg);
    else if (act === 'state-import') openImport();
    else if (act === 'state-import-submit') submitImport(dlg);
  });
  // Segment buttons toggle aria-selected/active without re-rendering the lock card.
  function renderSegButtons() {
    document.querySelectorAll('[data-st-root] .tf-st-segs .tab').forEach(function (x) {
      const on = x.getAttribute('data-seg') === cur.seg;
      x.classList.toggle('active', on);
      x.setAttribute('aria-selected', String(on));
    });
  }
  document.addEventListener('input', function (e) {
    if (!cur || !e.target.matches || !e.target.matches('[data-st-filter="q"]')) return;
    cur.filt.q = e.target.value; renderResRows();
  });
  document.addEventListener('change', function (e) {
    if (!cur || !e.target.matches) return;
    if (e.target.matches('[data-st-filter="mode"]')) { cur.filt.mode = e.target.value; renderResRows(); }
    else if (e.target.matches('[data-st-filter="provider"]')) { cur.filt.provider = e.target.value; renderResRows(); }
    else if (e.target.matches('[data-st-file]')) {
      const f = e.target.files && e.target.files[0];
      if (!f) return;
      const rd = new FileReader();
      rd.onload = function () { const ta = document.getElementById('tfStImpText'); if (ta) ta.value = String(rd.result || ''); };
      rd.readAsText(f);
    }
  });

  Tf.registerWsTab({
    id: 'state', icon: 'database', labelKey: 'tf_ws_tab_state', fallback: 'State', order: 4,
    render: function (body, ws) { renderTab(body, ws); },
  });
})();
