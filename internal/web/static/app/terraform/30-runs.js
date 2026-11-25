/* Terraform Runs: global runs feed + approval queue (top tab), workspace Runs tab (drawer),
   Run Viewer drawer (pipeline, approval gate, plan diff, policy & cost, console log), run actions. */
(function () {
  'use strict';

  const Tf = window.Tf;
  Tf.addI18n({
    en: {
      tf_tab_runs: 'Runs', tf_ws_tab_runs: 'Runs',
      tf_runs_search_ph: 'Search run #, message, commit or workspace', tf_runs_all_ws: 'All workspaces',
      tf_runs_all_types: 'All types', tf_runs_all_triggers: 'All triggers',
      tf_runs_chip_all: 'All', tf_runs_chip_awaiting: 'Awaiting approval', tf_runs_chip_running: 'Running',
      tf_runs_chip_failed: 'Failed', tf_runs_chip_applied: 'Applied', tf_runs_chip_nochanges: 'No changes',
      tf_runs_banner: '{n} run(s) awaiting approval', tf_runs_banner_show: 'Show',
      tf_runs_empty_title: 'No runs yet', tf_runs_empty_desc: 'Runs appear here once a workspace is planned, applied or triggered by a webhook or schedule.',
      tf_runs_col_run: 'Run', tf_runs_col_ws: 'Workspace', tf_runs_col_type: 'Type & trigger', tf_runs_col_status: 'Status',
      tf_runs_col_changes: 'Changes', tf_runs_col_policy: 'Policy', tf_runs_col_cost: 'Cost delta', tf_runs_col_time: 'Created / duration',
      tf_runs_inspect: 'Inspect', tf_runs_review: 'Review', tf_runs_load_failed: 'Could not load runs',
      tf_ws_runs_all: 'All runs', tf_ws_runs_active: 'Active', tf_ws_runs_awaiting: 'Awaiting approval', tf_ws_runs_applied: 'Applied', tf_ws_runs_failed: 'Failed',
      tf_ws_runs_empty: 'No runs for this workspace yet.', tf_run_start: 'Start Run', tf_run_start_title: 'Start Terraform Run',
      tf_run_type: 'Type', tf_run_type_plan: 'Plan', tf_run_type_speculative: 'Speculative plan',
      tf_run_type_destroy: 'Destroy', tf_run_type_refresh: 'Refresh-only', tf_run_message: 'Message',
      tf_run_targets: 'Targets', tf_run_targets_hint: 'Comma-separated resource addresses', tf_run_replace: 'Replace',
      tf_run_submit: 'Start Run', tf_run_started: 'Run started', tf_run_speculative_tag: 'speculative', tf_run_trigger: 'Trigger',
      tf_run_title: 'Run #{id}', tf_run_cancel: 'Cancel Run', tf_run_cancel_title: 'Cancel this run?',
      tf_run_cancel_msg: 'The running Terraform process will be stopped.', tf_run_canceled: 'Run canceled',
      tf_run_elapsed: 'Elapsed', tf_run_duration: 'Duration', tf_run_created: 'Created', tf_run_trigger: 'Trigger',
      tf_run_commit: 'Commit', tf_run_targets_label: 'Targets', tf_run_message_label: 'Message',
      tf_run_step_queued: 'Queued', tf_run_step_plan: 'Plan', tf_run_step_policy: 'Policy Check',
      tf_run_step_cost: 'Cost Estimate', tf_run_step_approval: 'Approval', tf_run_step_apply: 'Apply',
      tf_run_sub_diff: 'Plan Diff', tf_run_sub_policy: 'Policy & Cost', tf_run_sub_log: 'Console Log',
      tf_run_gate_title: 'Approval Gate', tf_run_gate_summary: 'Impact: {c} create · {u} update · {r} replace · {d} delete',
      tf_run_gate_comment_ph: 'Why are you approving this change? (min. 3 characters)',
      tf_run_gate_override: 'Override policy warnings', tf_run_gate_comment_req: 'A comment of at least 3 characters is required.',
      tf_run_apply: 'Confirm & Apply', tf_run_applying: 'Applying…', tf_run_apply_ok: 'Apply started',
      tf_run_discard: 'Discard Run', tf_run_discard_title: 'Discard this run?', tf_run_discard_msg: 'The planned changes will not be applied.',
      tf_run_discard_ok: 'Run discarded', tf_run_decision: 'Decision', tf_run_poll_failed: 'Log polling failed; the run may still be running.',
      tf_run_follow: 'Follow', tf_run_wrap: 'Wrap', tf_run_copy: 'Copy', tf_run_download: 'Download .log',
      tf_run_log_search: 'Search log', tf_run_prev: 'Previous match', tf_run_next: 'Next match',
      tf_run_copied: 'Log copied', tf_run_no_log: 'No output yet.',
      tf_diff_all: 'All', tf_diff_create: '+ Create', tf_diff_update: '~ Update', tf_diff_replace: '-/+ Replace',
      tf_diff_delete: '- Delete', tf_diff_read: '<= Read', tf_diff_search_ph: 'Filter by resource address',
      tf_diff_expand: 'Expand all', tf_diff_collapse: 'Collapse all', tf_diff_empty: 'No changes in this plan.',
      tf_diff_known_after: '(known after apply)', tf_diff_sensitive: '(sensitive)', tf_diff_show_full: 'Show full value',
      tf_diff_hide_full: 'Hide full value',
      tf_policy_status: 'Policy check', tf_policy_passed: 'Passed', tf_policy_warning: 'Warning', tf_policy_failed: 'Failed',
      tf_cost_title: 'Monthly cost delta', tf_cost_not_installed: 'Infracost not installed',
      tf_cost_before: 'before', tf_cost_after: 'after', tf_cost_per_month: '/mo',
      tf_run_action_failed: 'Action failed',
    },
    id: {
      tf_tab_runs: 'Run', tf_ws_tab_runs: 'Run',
      tf_runs_search_ph: 'Cari nomor run, pesan, commit, atau workspace', tf_runs_all_ws: 'Semua workspace',
      tf_runs_all_types: 'Semua tipe', tf_runs_all_triggers: 'Semua pemicu',
      tf_runs_chip_all: 'Semua', tf_runs_chip_awaiting: 'Menunggu persetujuan', tf_runs_chip_running: 'Berjalan',
      tf_runs_chip_failed: 'Gagal', tf_runs_chip_applied: 'Diterapkan', tf_runs_chip_nochanges: 'Tanpa perubahan',
      tf_runs_banner: '{n} run menunggu persetujuan', tf_runs_banner_show: 'Tampilkan',
      tf_runs_empty_title: 'Belum ada run', tf_runs_empty_desc: 'Run muncul di sini setelah workspace direncanakan, diterapkan, atau dipicu webhook atau jadwal.',
      tf_runs_col_run: 'Run', tf_runs_col_ws: 'Workspace', tf_runs_col_type: 'Tipe & pemicu', tf_runs_col_status: 'Status',
      tf_runs_col_changes: 'Perubahan', tf_runs_col_policy: 'Kebijakan', tf_runs_col_cost: 'Selisih biaya', tf_runs_col_time: 'Dibuat / durasi',
      tf_runs_inspect: 'Periksa', tf_runs_review: 'Tinjau', tf_runs_load_failed: 'Gagal memuat run',
      tf_ws_runs_all: 'Semua run', tf_ws_runs_active: 'Aktif', tf_ws_runs_awaiting: 'Menunggu persetujuan', tf_ws_runs_applied: 'Diterapkan', tf_ws_runs_failed: 'Gagal',
      tf_ws_runs_empty: 'Belum ada run untuk workspace ini.', tf_run_start: 'Mulai Run', tf_run_start_title: 'Mulai Run Terraform',
      tf_run_type: 'Tipe', tf_run_type_plan: 'Plan', tf_run_type_speculative: 'Plan spekulatif',
      tf_run_type_destroy: 'Destroy', tf_run_type_refresh: 'Refresh saja', tf_run_message: 'Pesan',
      tf_run_targets: 'Target', tf_run_targets_hint: 'Alamat resource, dipisah koma', tf_run_replace: 'Ganti',
      tf_run_submit: 'Mulai Run', tf_run_started: 'Run dimulai', tf_run_speculative_tag: 'spekulatif', tf_run_trigger: 'Pemicu',
      tf_run_title: 'Run #{id}', tf_run_cancel: 'Batalkan Run', tf_run_cancel_title: 'Batalkan run ini?',
      tf_run_cancel_msg: 'Proses Terraform yang berjalan akan dihentikan.', tf_run_canceled: 'Run dibatalkan',
      tf_run_elapsed: 'Berjalan', tf_run_duration: 'Durasi', tf_run_created: 'Dibuat', tf_run_trigger: 'Pemicu',
      tf_run_commit: 'Commit', tf_run_targets_label: 'Target', tf_run_message_label: 'Pesan',
      tf_run_step_queued: 'Antre', tf_run_step_plan: 'Plan', tf_run_step_policy: 'Cek Kebijakan',
      tf_run_step_cost: 'Estimasi Biaya', tf_run_step_approval: 'Persetujuan', tf_run_step_apply: 'Terapkan',
      tf_run_sub_diff: 'Diff Plan', tf_run_sub_policy: 'Kebijakan & Biaya', tf_run_sub_log: 'Log Konsol',
      tf_run_gate_title: 'Gerbang Persetujuan', tf_run_gate_summary: 'Dampak: {c} buat · {u} ubah · {r} ganti · {d} hapus',
      tf_run_gate_comment_ph: 'Mengapa perubahan ini disetujui? (min. 3 karakter)',
      tf_run_gate_override: 'Abaikan peringatan kebijakan', tf_run_gate_comment_req: 'Komentar minimal 3 karakter wajib diisi.',
      tf_run_apply: 'Setujui & Terapkan', tf_run_applying: 'Menerapkan…', tf_run_apply_ok: 'Penerapan dimulai',
      tf_run_discard: 'Buang Run', tf_run_discard_title: 'Buang run ini?', tf_run_discard_msg: 'Perubahan yang direncanakan tidak akan diterapkan.',
      tf_run_discard_ok: 'Run dibuang', tf_run_decision: 'Keputusan', tf_run_poll_failed: 'Polling log gagal; run mungkin masih berjalan.',
      tf_run_follow: 'Ikuti', tf_run_wrap: 'Bungkus baris', tf_run_copy: 'Salin', tf_run_download: 'Unduh .log',
      tf_run_log_search: 'Cari di log', tf_run_prev: 'Hasil sebelumnya', tf_run_next: 'Hasil berikutnya',
      tf_run_copied: 'Log disalin', tf_run_no_log: 'Belum ada keluaran.',
      tf_diff_all: 'Semua', tf_diff_create: '+ Buat', tf_diff_update: '~ Ubah', tf_diff_replace: '-/+ Ganti',
      tf_diff_delete: '- Hapus', tf_diff_read: '<= Baca', tf_diff_search_ph: 'Filter menurut alamat resource',
      tf_diff_expand: 'Buka semua', tf_diff_collapse: 'Tutup semua', tf_diff_empty: 'Tidak ada perubahan pada plan ini.',
      tf_diff_known_after: '(diketahui setelah apply)', tf_diff_sensitive: '(sensitif)', tf_diff_show_full: 'Tampilkan nilai penuh',
      tf_diff_hide_full: 'Sembunyikan nilai penuh',
      tf_policy_status: 'Cek kebijakan', tf_policy_passed: 'Lulus', tf_policy_warning: 'Peringatan', tf_policy_failed: 'Gagal',
      tf_cost_title: 'Selisih biaya bulanan', tf_cost_not_installed: 'Infracost belum terpasang',
      tf_cost_before: 'sebelum', tf_cost_after: 'sesudah', tf_cost_per_month: '/bln',
      tf_run_action_failed: 'Aksi gagal',
    },
  });

  /* ---------- small helpers ---------- */
  const T = function (k, f, v) { return Tf.t(k, f, v); };
  const esc = function (s) { return Tf.esc(s); };
  const val = function (id) { const el = document.getElementById(id); return el ? el.value : ''; };
  const checked = function (id) { const el = document.getElementById(id); return !!(el && el.checked); };
  const ACTIVE = Tf.ACTIVE_RUN;
  const isActive = function (s) { return ACTIVE.indexOf(s) >= 0; };
  const SYM = { create: '+', update: '~', replace: '-/+', delete: '-', read: '<=' };
  const ACTION_KEY = { all: 'tf_diff_all', create: 'tf_diff_create', update: 'tf_diff_update', replace: 'tf_diff_replace', delete: 'tf_diff_delete', read: 'tf_diff_read' };
  const CHIP_STATUS = { awaiting: ['awaiting_approval'], running: ['queued', 'planning', 'applying'], failed: ['errored', 'policy_failed'], applied: ['applied'], nochanges: ['no_changes'] };
  const busy = { apply: false, discard: false, cancel: false };
  const ansiState = function () { return { fg: -1, b: false, d: false, u: false }; };
  const newLog = function () { return { lines: [], partial: '', st: ansiState(), off: 0, follow: true, wrap: false, q: '', hits: [], cur: -1 }; };

  // Elapsed counters on any run row/badge that carries data-t0 (ticks locally, no API calls).
  Tf.every(function () {
    const now = Date.now();
    document.querySelectorAll('.tf-elapsed[data-t0]').forEach(function (el) {
      el.textContent = Tf.fmt.dur(now - Number(el.getAttribute('data-t0')));
    });
  }, 1000);

  function runT0(r) { return r.started_at || r.created_at ? new Date(r.started_at || r.created_at).getTime() : 0; }
  function elapsedHtml(r) {
    if (!isActive(r.status)) return '';
    return ' <span class="badge tf-elapsed" data-t0="' + runT0(r) + '">0s</span>';
  }
  function money(v, cur) {
    try { return new Intl.NumberFormat(undefined, { style: 'currency', currency: cur || 'USD' }).format(v); }
    catch (e) { return Number(v).toFixed(2) + ' ' + (cur || 'USD'); }
  }
  function costDeltaHtml(d) {
    if (d === null || d === undefined || d === 0) return '<span class="tf-muted">-</span>';
    return '<span class="tf-mono" style="color:var(--' + (d > 0 ? 'warning' : 'success') + ');">' + (d > 0 ? '+' : '') + Number(d).toFixed(2) + '</span>';
  }
  function policyBadge(s) {
    if (!s) return '<span class="tf-muted">-</span>';
    const cls = { passed: 'badge-healthy', warning: 'badge-warning', failed: 'badge-danger' }[s] || 'badge-unknown';
    return '<span class="badge ' + cls + '">' + esc(T('tf_policy_' + s, s)) + '</span>';
  }

  /* ---------- ANSI -> HTML (escape first, then wrap colour/style runs) ---------- */
  // shortcut: handles SGR colours 30-37/90-97 and bold/dim/underline only; 256/truecolour sequences render in the default colour.
  const SGR_RE = /\x1b\[([0-9;]*)m/g;
  const OTHER_CSI = /\x1b\[[0-9;?]*[A-Za-ln-z]/g;
  function stripAnsi(s) { return String(s).replace(SGR_RE, '').replace(OTHER_CSI, ''); }
  function spanOpen(st) {
    const cls = [];
    if (st.fg >= 0) cls.push('tf-fg-' + st.fg);
    if (st.b) cls.push('tf-b');
    if (st.d) cls.push('tf-d');
    if (st.u) cls.push('tf-u');
    return cls.length ? '<span class="' + cls.join(' ') + '">' : '';
  }
  function applySgr(st, codes) {
    codes.forEach(function (c) {
      if (c === 0) { st.fg = -1; st.b = st.d = st.u = false; }
      else if (c === 1) st.b = true;
      else if (c === 2) st.d = true;
      else if (c === 4) st.u = true;
      else if (c === 22) st.b = st.d = false;
      else if (c === 24) st.u = false;
      else if (c >= 30 && c <= 37) st.fg = c - 30;
      else if (c >= 90 && c <= 97) st.fg = c - 90 + 8;
      else if (c === 39) st.fg = -1;
    });
  }
  // Converts one raw line; `st` carries the SGR state from earlier lines.
  function ansiLine(raw, st) {
    const src = String(raw).replace(OTHER_CSI, '');
    let out = '', last = 0, open = spanOpen(st);
    const emit = function (s) { if (s) out += open ? open + esc(s) + '</span>' : esc(s); };
    src.replace(SGR_RE, function (m, p, off) {
      emit(src.slice(last, off));
      last = off + m.length;
      applySgr(st, p === '' ? [0] : p.split(';').map(Number));
      open = spanOpen(st);
      return m;
    });
    emit(src.slice(last));
    return out;
  }

  /* ---------- log buffer ---------- */
  function appendLog(log, text) {
    const parts = (log.partial + text).split('\n');
    log.partial = parts.pop();
    parts.forEach(function (raw) { log.lines.push({ html: ansiLine(raw, log.st), plain: stripAnsi(raw) }); });
  }
  function flushPartial(log) {
    if (!log.partial) return;
    log.lines.push({ html: ansiLine(log.partial, log.st), plain: stripAnsi(log.partial) });
    log.partial = '';
  }
  function plainText(log) { return log.lines.map(function (l) { return l.plain; }).join('\n') + (log.partial ? '\n' + stripAnsi(log.partial) : ''); }
  function lineHtml(l, i, log) {
    const hit = !!log.hitSet && !!log.hitSet[i];
    return '<div class="tf-log-line' + (hit ? ' tf-log-hit' : '') + (i === log.cur ? ' tf-log-cur' : '') + '">' + (l.html || '&nbsp;') + '</div>';
  }
  // Full paint (search active or first paint); otherwise only the lines added since the last paint.
  function paintLog(pre, log) {
    if (!pre) return;
    if (log.q) {
      const q = log.q.toLowerCase();
      log.hits = []; log.hitSet = {};
      log.lines.forEach(function (l, i) { if (l.plain.toLowerCase().indexOf(q) >= 0) { log.hits.push(i); log.hitSet[i] = true; } });
      if (log.cur >= log.hits.length) log.cur = log.hits.length - 1;
      pre.innerHTML = log.lines.map(function (l, i) { return lineHtml(l, i, log); }).join('');
      pre._drawn = log.lines.length;
    } else {
      log.hits = []; log.hitSet = null; log.cur = -1;
      if (pre._drawn === undefined || pre._drawn > log.lines.length) {
        pre.innerHTML = log.lines.map(function (l, i) { return lineHtml(l, i, log); }).join('');
      } else if (pre._drawn < log.lines.length) {
        pre.insertAdjacentHTML('beforeend', log.lines.slice(pre._drawn).map(function (l, i) { return lineHtml(l, pre._drawn + i, log); }).join(''));
      }
      pre._drawn = log.lines.length;
    }
    pre.classList.toggle('tf-log-wrap', !!log.wrap);
    const count = document.getElementById('tfLogCount');
    if (count) count.textContent = log.hits.length ? (log.cur + 1) + '/' + log.hits.length : (log.q ? '0/0' : '');
    if (log.follow) pre.scrollTop = pre.scrollHeight;
  }
  function gotoHit(log, delta) {
    if (!log.hits.length) return;
    log.cur = (log.cur + delta + log.hits.length) % log.hits.length;
    if (log.cur < 0) log.cur = log.hits.length - 1;
    const pre = document.getElementById('tfRunLog');
    if (!pre) return;
    paintLog(pre, log);
    const el = pre.querySelector('.tf-log-cur');
    if (el) el.scrollIntoView({ block: 'center' });
  }

  /* ---------- global feed (top tab) ---------- */
  const feed = { mounted: false, filter: { chip: 'all', q: '', ws: '', type: '', trigger: '' }, rows: [], pending: 0, seq: 0 };

  function feedParams() {
    const f = feed.filter, qs = new URLSearchParams();
    if (CHIP_STATUS[f.chip]) qs.set('status', CHIP_STATUS[f.chip].join(','));
    if (f.q) qs.set('q', f.q);
    if (f.type) qs.set('type', f.type);
    if (f.trigger) qs.set('trigger', f.trigger);
    qs.set('limit', '200');
    return qs.toString();
  }
  async function loadFeed() {
    const seq = ++feed.seq;
    try {
      const [list, pend] = await Promise.all([
        Tf.api.get('/api/terraform/runs?' + feedParams()),
        Tf.api.get('/api/terraform/runs?status=awaiting_approval&limit=500'),
      ]);
      if (seq !== feed.seq) return;
      feed.rows = list.runs || [];
      feed.pending = (pend.runs || []).length;
      renderFeed();
    } catch (e) {
      if (seq !== feed.seq) return;
      const box = document.getElementById('tfGlobalRunsEmpty');
      if (box) box.innerHTML = Tf.ui.error(T('tf_runs_load_failed', 'Could not load runs') + ': ' + Tf.errMsg(e), 'data-runs-retry');
    }
  }
  function renderFeed() {
    const panel = document.getElementById('tfPanel-runs');
    if (!panel) return;
    const banner = document.getElementById('tfGlobalApprovalBanner');
    if (banner) {
      banner.hidden = feed.pending === 0;
      banner.innerHTML = feed.pending ? Tf.icon('shield-alert', 'icon-sm') + ' <span>' + esc(T('tf_runs_banner', '{n} run(s) awaiting approval', { n: feed.pending })) + '</span>' +
        ' <button type="button" class="btn btn-sm" data-chip="awaiting">' + esc(T('tf_runs_banner_show', 'Show')) + '</button>' : '';
    }
    // workspace filter is applied client-side; options come from the unfiltered list
    const wsSel = document.getElementById('tfRunsWs');
    if (wsSel) {
      const names = Array.from(new Set(feed.rows.map(function (r) { return r.workspace_name; }))).sort();
      const cur = feed.filter.ws;
      wsSel.innerHTML = '<option value="">' + esc(T('tf_runs_all_ws', 'All workspaces')) + '</option>' +
        names.map(function (n) { return '<option value="' + esc(n) + '"' + (n === cur ? ' selected' : '') + '>' + esc(n) + '</option>'; }).join('');
    }
    const rows = feed.rows.filter(function (r) { return !feed.filter.ws || r.workspace_name === feed.filter.ws; });
    const tbody = document.querySelector('#tfGlobalRunsTable tbody');
    const empty = document.getElementById('tfGlobalRunsEmpty');
    const wrap = document.getElementById('tfGlobalRunsWrap');
    if (wrap) wrap.hidden = !rows.length;
    if (empty) empty.innerHTML = rows.length ? '' : Tf.ui.empty({ icon: 'activity', title: T('tf_runs_empty_title', 'No runs yet'), desc: T('tf_runs_empty_desc', '') });
    if (tbody) tbody.innerHTML = rows.map(function (r) {
      const dur = r.finished_at && (r.started_at || r.created_at) ? Tf.fmt.dur(new Date(r.finished_at) - new Date(r.started_at || r.created_at)) : '';
      const action = r.status === 'awaiting_approval' ? T('tf_runs_review', 'Review') : T('tf_runs_inspect', 'Inspect');
      const hasPlan = r.has_changes || (r.counts && (r.counts.create + r.counts.update + r.counts.delete + r.counts.replace) > 0);
      return '<tr class="tf-run-row" data-run="' + r.id + '" tabindex="0">' +
        '<td class="tf-mono">#' + r.id + '</td>' +
        '<td><b>' + esc(r.workspace_name || '-') + '</b> ' + (r.workspace_environment ? '<span class="badge">' + esc(r.workspace_environment) + '</span>' : '') + '</td>' +
        '<td>' + esc(r.type) + (r.speculative ? ' · ' + esc(T('tf_run_speculative_tag', 'speculative')) : '') + '<div class="tf-muted tf-small">' + esc(r.trigger_kind) + '</div></td>' +
        '<td>' + Tf.ui.badge(r.status) + elapsedHtml(r) + '</td>' +
        '<td class="tf-mono">' + (hasPlan ? esc(Tf.fmt.counts(r.counts)) : '<span class="tf-muted">-</span>') + '</td>' +
        '<td>' + policyBadge(r.policy_status) + '</td>' +
        '<td>' + costDeltaHtml(r.cost_delta) + '</td>' +
        '<td><div>' + esc(Tf.fmt.ago(r.created_at)) + '</div><div class="tf-muted tf-small">' + esc(dur) + '</div></td>' +
        '<td><button type="button" class="btn btn-sm" data-run="' + r.id + '">' + esc(action) + '</button></td></tr>';
    }).join('');
  }
  function bindFeedPanel(panel) {
    panel.addEventListener('click', function (e) {
      const chip = e.target.closest('[data-chip]');
      if (chip) {
        feed.filter.chip = chip.getAttribute('data-chip');
        panel.querySelectorAll('[data-chip]').forEach(function (b) { b.classList.toggle('active', b.getAttribute('data-chip') === feed.filter.chip); });
        loadFeed();
        return;
      }
      const row = e.target.closest('[data-run]');
      if (row) { Tf.runs.openViewer(Number(row.getAttribute('data-run'))); return; }
      if (e.target.closest('[data-runs-retry]')) loadFeed();
    });
    panel.addEventListener('keydown', function (e) {
      const row = e.target.closest && e.target.closest('tr[data-run]');
      if (row && e.key === 'Enter') Tf.runs.openViewer(Number(row.getAttribute('data-run')));
    });
    panel.addEventListener('input', Tf.debounce(function (e) {
      if (e.target.id === 'tfRunsSearch') { feed.filter.q = e.target.value.trim(); loadFeed(); }
    }, 250));
    panel.addEventListener('change', function (e) {
      if (e.target.id === 'tfRunsWs') { feed.filter.ws = e.target.value; renderFeed(); }
      else if (e.target.id === 'tfRunsType') { feed.filter.type = e.target.value; loadFeed(); }
      else if (e.target.id === 'tfRunsTrigger') { feed.filter.trigger = e.target.value; loadFeed(); }
    });
  }

  Tf.registerTab({
    id: 'runs', icon: 'activity', labelKey: 'tf_tab_runs', fallback: 'Runs', order: 2,
    render: function (panel) {
      const chips = ['all', 'awaiting', 'running', 'failed', 'applied', 'nochanges'].map(function (c) {
        return '<button type="button" class="tf-chip' + (c === 'all' ? ' active' : '') + '" data-chip="' + c + '">' + esc(T('tf_runs_chip_' + c, c)) + '</button>';
      }).join('');
      const opts = function (list, all) {
        return '<option value="">' + esc(all) + '</option>' + list.map(function (v) { return '<option value="' + v + '">' + v + '</option>'; }).join('');
      };
      panel.innerHTML =
        '<div id="tfGlobalApprovalBanner" class="tf-banner" hidden></div>' +
        '<div class="tf-runs-toolbar tf-row">' +
        '<input type="search" id="tfRunsSearch" class="form-input" placeholder="' + esc(T('tf_runs_search_ph', '')) + '">' +
        '<select id="tfRunsWs" class="form-input" aria-label="' + esc(T('tf_runs_col_ws', 'Workspace')) + '"><option value="">' + esc(T('tf_runs_all_ws', 'All workspaces')) + '</option></select>' +
        '<select id="tfRunsType" class="form-input" aria-label="' + esc(T('tf_runs_col_type', 'Type')) + '">' + opts(['plan', 'destroy', 'refresh', 'drift'], T('tf_runs_all_types', 'All types')) + '</select>' +
        '<select id="tfRunsTrigger" class="form-input" aria-label="' + esc(T('tf_run_trigger', 'Trigger')) + '">' + opts(['manual', 'webhook', 'drift', 'schedule'], T('tf_runs_all_triggers', 'All triggers')) + '</select>' +
        '</div>' +
        '<div class="tf-chips" role="group">' + chips + '</div>' +
        '<div id="tfGlobalRunsWrap" class="tf-table-wrap"><table id="tfGlobalRunsTable" class="tf-table"><thead><tr>' +
        ['tf_runs_col_run', 'tf_runs_col_ws', 'tf_runs_col_type', 'tf_runs_col_status', 'tf_runs_col_changes', 'tf_runs_col_policy', 'tf_runs_col_cost', 'tf_runs_col_time', ''].map(function (k) {
          return '<th>' + (k ? esc(T(k, k)) : '') + '</th>';
        }).join('') + '</tr></thead><tbody></tbody></table></div>' +
        '<div id="tfGlobalRunsEmpty"></div>';
      if (!panel._tfBound) { panel._tfBound = true; bindFeedPanel(panel); }
    },
    onShow: function (panel, opts) {
      feed.mounted = true;
      loadFeed();
      return opts;
    },
    onHide: function () { feed.mounted = false; },
  });
  Tf.every(function () { if (feed.mounted && Tf.state.tab === 'runs') loadFeed(); }, 10000);

  /* ---------- start run dialog ---------- */
  // type: 'plan' | 'speculative' | 'destroy' | 'refresh'. Speculative is a plan with speculative=true.
  function startDialog(wsId, type, opts) {
    opts = opts || {};
    const cur = type || 'plan';
    const typeOpts = [['plan', 'tf_run_type_plan', 'Plan'], ['speculative', 'tf_run_type_speculative', 'Speculative plan'], ['destroy', 'tf_run_type_destroy', 'Destroy'], ['refresh', 'tf_run_type_refresh', 'Refresh-only']]
      .map(function (o) { return '<option value="' + o[0] + '"' + (o[0] === cur ? ' selected' : '') + '>' + esc(T(o[1], o[2])) + '</option>'; }).join('');
    const body = '<div class="tf-grid-2">' +
      '<label class="tf-field"><span class="tf-field-label">' + esc(T('tf_run_type', 'Type')) + '</span><select id="tfRunType" class="form-input">' + typeOpts + '</select></label>' +
      '<label class="tf-field"><span class="tf-field-label">' + esc(T('tf_run_message', 'Message')) + '</span><input id="tfRunMsg" class="form-input" maxlength="200"></label>' +
      '<label class="tf-field" style="grid-column:1/-1"><span class="tf-field-label">' + esc(T('tf_run_targets', 'Targets')) + '</span><input id="tfRunTargets" class="form-input" placeholder="module.vpc, aws_s3_bucket.logs">' +
      '<span class="tf-hint">' + esc(T('tf_run_targets_hint', '')) + '</span></label>' +
      '<label class="tf-field" style="grid-column:1/-1"><span class="tf-field-label">' + esc(T('tf_run_replace', 'Replace')) + '</span><input id="tfRunReplace" class="form-input" placeholder="aws_instance.web"></label>' +
      '</div>';
    const footer = '<button type="button" class="btn btn-sm" data-dlg-close>' + esc(T('tf_close', 'Close')) + '</button>' +
      '<button type="button" id="tfRunSubmit" class="btn btn-sm btn-primary">' + Tf.icon('play', 'icon-xs') + ' ' + esc(T('tf_run_submit', 'Start Run')) + '</button>';
    Tf.dlg.open({
      title: T('tf_run_start_title', 'Start Terraform Run'), body: body, footer: footer, width: 640,
      onMount: function (root, api) {
        root.querySelector('#tfRunSubmit').addEventListener('click', async function () {
          const t = val('tfRunType');
          const split = function (s) { return s.split(',').map(function (x) { return x.trim(); }).filter(Boolean); };
          const payload = {
            type: t === 'speculative' ? 'plan' : t,
            speculative: t === 'speculative',
            message: val('tfRunMsg').trim(),
            targets: split(val('tfRunTargets')),
            replace: split(val('tfRunReplace')),
          };
          api.clearError();
          try {
            const r = await Tf.api.post('/api/terraform/workspaces/' + wsId + '/runs', payload);
            api.close();
            Tf.toast(T('tf_run_started', 'Run started'), 'success');
            emitChange();
            if (opts.onStarted) opts.onStarted(r.run); else Tf.runs.openViewer(r.run.id);
          } catch (e) { api.error(Tf.errMsg(e)); }
        });
      },
    });
  }

  /* ---------- plan diff (reusable) ---------- */
  function valueHtml(v, d) {
    if (d.unknown) return '<span class="tf-muted">' + esc(T('tf_diff_known_after', '(known after apply)')) + '</span>';
    if (d.sensitive) return '<span class="tf-muted">' + esc(T('tf_diff_sensitive', '(sensitive)')) + '</span>';
    const s = v === undefined || v === null ? 'null' : JSON.stringify(v);
    if (s.length <= 240) return esc(s);
    // shortcut: values over 240 chars are shown truncated with an expand toggle; the full text is kept in the DOM.
    return '<span class="tf-val"><span class="tf-val-cut">' + esc(s.slice(0, 240)) + '…</span><span class="tf-val-full" hidden>' + esc(s) + '</span> ' +
      '<button type="button" class="btn btn-sm btn-ghost" data-diff-act="full">' + esc(T('tf_diff_show_full', 'Show full value')) + '</button></span>';
  }
  function diffRows(c) {
    const rows = (c.diffs || []).map(function (d) {
      const p = esc(d.path);
      if (c.action === 'create' || (d.before == null && d.after != null && c.action !== 'delete')) return '<div class="tf-diff-row tf-diff-row-add">+ ' + p + ' = ' + valueHtml(d.after, d) + '</div>';
      if (c.action === 'delete') return '<div class="tf-diff-row tf-diff-row-del">- ' + p + ' = ' + valueHtml(d.before, { sensitive: d.sensitive }) + '</div>';
      return '<div class="tf-diff-row tf-diff-row-mod">~ ' + p + ': ' + valueHtml(d.before, { sensitive: d.sensitive }) + ' → ' + valueHtml(d.after, d) + '</div>';
    }).join('');
    return rows || '<div class="tf-muted">—</div>';
  }
  function paintDiff(container, st) {
    const list = container.querySelector('#tfDiffList');
    if (!list) return;
    const q = st.q.toLowerCase();
    const shown = st.changes.filter(function (c) {
      return (st.chip === 'all' || c.action === st.chip) && (!q || String(c.address || '').toLowerCase().indexOf(q) >= 0);
    });
    container.querySelectorAll('[data-diff-act="filter"]').forEach(function (b) { b.classList.toggle('active', b.getAttribute('data-action') === st.chip); });
    list.innerHTML = shown.length ? shown.map(function (c) {
      return '<details class="tf-diff-card tf-diff-' + esc(c.action) + '"' + (st.open ? ' open' : '') + '>' +
        '<summary class="tf-diff-head"><b class="tf-mono tf-sym">' + esc(SYM[c.action] || '') + '</b> <span class="tf-mono tf-addr">' + esc(c.address) + '</span> ' +
        '<span class="badge">' + esc(c.action) + (c.reason ? ' · ' + esc(c.reason) : '') + '</span></summary>' +
        '<div class="tf-diff-rows">' + diffRows(c) + '</div></details>';
    }).join('') : Tf.ui.empty({ icon: 'activity', desc: T('tf_diff_empty', 'No changes in this plan.') });
  }
  // Renders the filterable plan diff into `container`. Used by the Run Viewer and the Drift tab.
  function renderDiff(container, changes, opts) {
    if (!container) return;
    const st = container._tfDiff || (container._tfDiff = { chip: 'all', q: '', changes: [], open: false });
    st.changes = changes || [];
    if (!container.querySelector('#tfDiffList')) {
      const chips = ['all', 'create', 'update', 'replace', 'delete', 'read'].map(function (a) {
        return '<button type="button" class="tf-chip" data-diff-act="filter" data-action="' + a + '">' + esc(T(ACTION_KEY[a], a)) + '</button>';
      }).join('');
      container.innerHTML = '<div class="tf-diff-tools tf-row"><div class="tf-chips">' + chips + '</div>' +
        '<input type="search" id="tfDiffSearch" class="form-input" placeholder="' + esc(T('tf_diff_search_ph', '')) + '">' +
        '<span class="tf-row"><button type="button" class="btn btn-sm btn-ghost" data-diff-act="expand">' + esc(T('tf_diff_expand', 'Expand all')) + '</button>' +
        '<button type="button" class="btn btn-sm btn-ghost" data-diff-act="collapse">' + esc(T('tf_diff_collapse', 'Collapse all')) + '</button></span></div>' +
        '<div id="tfDiffList" class="tf-diff-list"></div>';
      container.addEventListener('click', function (e) {
        const b = e.target.closest('[data-diff-act]');
        if (!b) return;
        const act = b.getAttribute('data-diff-act');
        if (act === 'filter') st.chip = b.getAttribute('data-action');
        else if (act === 'expand' || act === 'collapse') st.open = act === 'expand';
        else if (act === 'full') {
          const w = b.closest('.tf-val');
          const full = w.querySelector('.tf-val-full'), cut = w.querySelector('.tf-val-cut');
          const show = full.hidden;
          full.hidden = !show; cut.hidden = show;
          b.textContent = T(show ? 'tf_diff_hide_full' : 'tf_diff_show_full', show ? 'Hide full value' : 'Show full value');
          return;
        }
        if (act === 'expand' || act === 'collapse') container.querySelectorAll('#tfDiffList details').forEach(function (d) { d.open = st.open; });
        paintDiff(container, st);
      });
      container.addEventListener('input', Tf.debounce(function (e) {
        if (e.target.id === 'tfDiffSearch') { st.q = e.target.value.trim(); paintDiff(container, st); }
      }, 150));
    }
    paintDiff(container, st);
    return container;
  }

  /* ---------- Run Viewer ---------- */
  let cur = null; // the open viewer state (one at a time)

  function stepStates(run) {
    const pol = run.policy || {}, cost = run.cost || {};
    const P = pol.status ? (pol.status === 'failed' ? 'failed' : pol.status === 'warning' ? 'warn' : 'done') : 'skipped';
    const C = cost.available ? 'done' : 'skipped';
    const M = {
      queued: ['active', 'pending', 'pending', 'pending', 'pending', 'pending'],
      planning: ['done', 'active', 'pending', 'pending', 'pending', 'pending'],
      planned: ['done', 'done', P, C, 'skipped', 'skipped'],
      no_changes: ['done', 'done', P, C, 'skipped', 'skipped'],
      awaiting_approval: ['done', 'done', P, C, 'active', 'pending'],
      policy_failed: ['done', 'done', 'failed', 'skipped', 'skipped', 'skipped'],
      applying: ['done', 'done', P, C, 'done', 'active'],
      applied: ['done', 'done', P, C, 'done', 'done'],
      discarded: ['done', 'done', P, C, 'failed', 'skipped'],
      errored: run.decision === 'applied' ? ['done', 'done', P, C, 'done', 'failed'] : ['done', 'failed', 'skipped', 'skipped', 'skipped', 'skipped'],
      canceled: ['done', 'failed', 'skipped', 'skipped', 'skipped', 'skipped'],
      interrupted: ['done', 'failed', 'skipped', 'skipped', 'skipped', 'skipped'],
    };
    return M[run.status] || M.queued;
  }
  const STEP_KEYS = ['tf_run_step_queued', 'tf_run_step_plan', 'tf_run_step_policy', 'tf_run_step_cost', 'tf_run_step_approval', 'tf_run_step_apply'];
  const STEP_FALLBACK = ['Queued', 'Plan', 'Policy Check', 'Cost Estimate', 'Approval', 'Apply'];
  const STEP_ICON = { done: 'circle-check', failed: 'circle-x', warn: 'triangle-alert', active: 'loader-circle' };
  function stepperHtml(run) {
    const states = stepStates(run);
    return '<ol id="tfRunStepper" class="tf-stepper">' + states.map(function (s, i) {
      const icon = STEP_ICON[s] ? Tf.icon(STEP_ICON[s], 'icon-xs') : '';
      return '<li class="tf-step tf-step-' + s + '" aria-current="' + (s === 'active' ? 'step' : 'false') + '">' + icon + ' <span>' + esc(T(STEP_KEYS[i], STEP_FALLBACK[i])) + '</span></li>';
    }).join('') + '</ol>';
  }

  function policyCostHtml(run) {
    const pol = run.policy || {}, cost = run.cost || {};
    let h = '';
    if (pol.status) {
      h += '<div class="card tf-card-accent tf-card-' + esc(pol.status) + '"><div class="card-title"><span>' + esc(T('tf_policy_status', 'Policy check')) + ': ' + policyBadge(pol.status) + '</span></div>';
      (pol.results || []).forEach(function (r) {
        h += '<div class="tf-policy-row">' + Tf.icon(r.passed ? 'circle-check' : 'triangle-alert', 'icon-xs') + ' <b>' + esc(r.name) + '</b> <span class="badge">' + esc(r.enforcement) + '</span>';
        (r.violations || []).forEach(function (v) { h += '<div class="tf-mono tf-violation">' + esc(v.address ? v.address + ': ' : '') + esc(v.message) + '</div>'; });
        h += '</div>';
      });
      h += '</div>';
    }
    h += '<div class="card"><div class="card-title"><span>' + esc(T('tf_cost_title', 'Monthly cost delta')) + '</span></div>';
    if (!cost.available) h += '<div class="tf-muted">' + esc(cost.error && !/not installed/i.test(cost.error) ? cost.error : T('tf_cost_not_installed', 'Infracost not installed')) + '</div>';
    else {
      const cur = cost.currency || 'USD';
      h += '<div class="tf-row"><span class="tf-stat tf-mono" style="color:var(--' + (cost.delta > 0 ? 'warning' : 'success') + ');">' + (cost.delta >= 0 ? '+' : '-') + esc(money(Math.abs(cost.delta), cur)) + esc(T('tf_cost_per_month', '/mo')) +
        ' (' + (cost.delta_pct >= 0 ? '+' : '') + Number(cost.delta_pct || 0).toFixed(0) + '%)</span>' +
        '<span class="tf-muted tf-small">' + esc(T('tf_cost_before', 'before')) + ' ' + esc(money(cost.monthly_before, cur)) + ' → ' + esc(T('tf_cost_after', 'after')) + ' ' + esc(money(cost.monthly_after, cur)) + '</span></div>';
    }
    return h + '</div>';
  }

  function logToolsHtml(log) {
    return '<div class="tf-log-tools tf-row">' +
      '<input type="search" id="tfLogSearch" class="form-input" placeholder="' + esc(T('tf_run_log_search', '')) + '" value="' + esc(log.q) + '">' +
      '<button type="button" class="btn btn-sm btn-ghost" data-log="prev" aria-label="' + esc(T('tf_run_prev', '')) + '">↑</button>' +
      '<button type="button" class="btn btn-sm btn-ghost" data-log="next" aria-label="' + esc(T('tf_run_next', '')) + '">↓</button>' +
      '<span id="tfLogCount" class="tf-muted tf-mono"></span>' +
      '<label class="tf-check"><input type="checkbox" id="tfLogFollow"' + (log.follow ? ' checked' : '') + '> <span>' + esc(T('tf_run_follow', 'Follow')) + '</span></label>' +
      '<label class="tf-check"><input type="checkbox" id="tfLogWrap"' + (log.wrap ? ' checked' : '') + '> <span>' + esc(T('tf_run_wrap', 'Wrap')) + '</span></label>' +
      '<span class="tf-grow"></span>' +
      '<button type="button" class="btn btn-sm btn-ghost" data-log="copy">' + esc(T('tf_run_copy', 'Copy')) + '</button>' +
      '<button type="button" class="btn btn-sm btn-ghost" data-log="download">' + esc(T('tf_run_download', 'Download .log')) + '</button></div>' +
      '<pre id="tfRunLog" class="tf-run-log"></pre>';
  }

  function approvalGateHtml(run) {
    if (run.status !== 'awaiting_approval') return '';
    const p = run.plan || {}, pol = run.policy || {};
    const cost = run.cost && run.cost.available ? ' · ' + (run.cost.delta >= 0 ? '+' : '') + money(run.cost.delta, run.cost.currency) : '';
    return '<div id="tfRunApprovalGate" class="card tf-run-gate">' +
      '<div class="card-title"><span>' + Tf.icon('shield-alert', 'icon-sm') + ' ' + esc(T('tf_run_gate_title', 'Approval Gate')) + '</span></div>' +
      '<div class="tf-small">' + esc(T('tf_run_gate_summary', '', { c: p.create || 0, u: p.update || 0, r: p.replace || 0, d: p.delete || 0 })) + ' · ' + policyBadge(pol.status) + esc(cost) + '</div>' +
      (pol.status === 'warning' ? '<label class="tf-check"><input type="checkbox" id="tfApprOverride"> <span>' + esc(T('tf_run_gate_override', 'Override policy warnings')) + '</span></label>' : '') +
      '<textarea id="tfApprComment" class="form-input" rows="2" placeholder="' + esc(T('tf_run_gate_comment_ph', '')) + '"></textarea>' +
      '<div id="tfApprErr" class="tf-error" role="alert" hidden></div>' +
      '<div class="tf-row"><button type="button" id="tfApprApply" class="btn btn-sm btn-primary" data-run-act="apply">' + Tf.icon('check', 'icon-xs') + ' ' + esc(T('tf_run_apply', 'Confirm & Apply')) + '</button>' +
      '<button type="button" class="btn btn-sm btn-danger" data-run-act="discard">' + Tf.icon('x', 'icon-xs') + ' ' + esc(T('tf_run_discard', 'Discard Run')) + '</button></div></div>';
  }

  function defaultSub(run) { return isActive(run.status) ? 'log' : 'diff'; }

  function renderBody(st) {
    if (!st.body || !st.run) return;
    const run = st.run;
    const changes = run.type === 'drift' ? (run.drift || []) : ((run.plan && run.plan.changes) || []);
    const subs = [['diff', 'tf_run_sub_diff', 'Plan Diff'], ['policy', 'tf_run_sub_policy', 'Policy & Cost'], ['log', 'tf_run_sub_log', 'Console Log']];
    const meta = [
      [T('tf_run_trigger', 'Trigger'), run.trigger_kind],
      [T('tf_run_commit', 'Commit'), run.commit_sha ? run.commit_sha.slice(0, 12) : ''],
      [T('tf_run_message_label', 'Message'), run.message],
      [T('tf_run_targets_label', 'Targets'), (run.targets || []).join(', ')],
      [T('tf_run_created', 'Created'), Tf.fmt.dt(run.created_at)],
      [T('tf_run_duration', 'Duration'), run.finished_at ? Tf.fmt.dur(new Date(run.finished_at) - runT0(run)) : ''],
    ].filter(function (m) { return m[1]; });
    const scroll = st.body.scrollTop;
    st.body.innerHTML = '<div class="tf-run-view">' +
      '<div class="card tf-run-head"><div class="tf-row">' + Tf.ui.badge(run.status) + elapsedHtml(run) +
      '<span class="badge">' + esc(run.type) + (run.speculative ? ' · ' + esc(T('tf_run_speculative_tag', 'speculative')) : '') + '</span>' +
      '<span class="tf-grow"></span>' + (isActive(run.status) ? '<button type="button" class="btn btn-sm btn-danger" data-run-act="cancel">' + Tf.icon('square', 'icon-xs') + ' ' + esc(T('tf_run_cancel', 'Cancel Run')) + '</button>' : '') + '</div>' +
      '<dl class="tf-meta">' + meta.map(function (m) { return '<dt>' + esc(m[0]) + '</dt><dd>' + esc(m[1]) + '</dd>'; }).join('') + '</dl></div>' +
      stepperHtml(run) + approvalGateHtml(run) +
      '<div class="tf-drawer-tabs" role="tablist">' + subs.map(function (s) {
        return '<button type="button" role="tab" class="tab' + (st.sub === s[0] ? ' active' : '') + '" data-run-tab="' + s[0] + '" aria-selected="' + (st.sub === s[0]) + '">' + esc(T(s[1], s[2])) + '</button>';
      }).join('') + '</div>' +
      '<div id="tfRunContent"></div></div>';
    st.body.scrollTop = scroll;
    const content = st.body.querySelector('#tfRunContent');
    if (st.sub === 'diff') renderDiff(content, changes, {});
    else if (st.sub === 'policy') content.innerHTML = '<div id="tfRunPolicyPanel" class="tf-policy-panel">' + policyCostHtml(run) + '</div>';
    else { content.innerHTML = logToolsHtml(st.log); paintLog(document.getElementById('tfRunLog'), st.log); }
    st.rendered = true;
  }

  function showApprErr(msg) {
    const el = document.getElementById('tfApprErr');
    if (!el) { Tf.toast(msg, 'error'); return; }
    el.textContent = msg || '';
    el.hidden = !msg;
  }

  async function reloadRun(st) {
    if (!st.body) return;
    try {
      const r = await Tf.api.get('/api/terraform/runs/' + st.id);
      const prev = st.run && st.run.status;
      st.run = r.run;
      if (prev !== st.run.status || !st.rendered) renderBody(st);
    } catch (e) { Tf.toast(Tf.errMsg(e), 'error'); }
    if (st.run && isActive(st.run.status)) startTimer(st); else stopTimer(st);
  }

  // Pulls log bytes from the last offset. Returns true once the run's log is complete.
  async function pollLog(st) {
    if (st.polling || !st.body) return st.done;
    st.polling = true;
    try {
      const r = await Tf.api.get('/api/terraform/runs/' + st.id + '/log?offset=' + st.log.off);
      st.fails = 0;
      if (r.log) appendLog(st.log, r.log);
      st.log.off = r.next_offset;
      if (r.done) { flushPartial(st.log); st.done = true; }
      paintLog(document.getElementById('tfRunLog'), st.log);
    } catch (e) {
      st.fails = (st.fails || 0) + 1;
      if (st.fails >= 5) {
        stopTimer(st);
        st.log.lines.push({ html: '<span class="tf-fg-1">' + esc(T('tf_run_poll_failed', '')) + '</span>', plain: T('tf_run_poll_failed', '') });
        paintLog(document.getElementById('tfRunLog'), st.log);
      }
    } finally { st.polling = false; }
    return st.done;
  }

  function startTimer(st) {
    if (st.timer) return;
    st.timer = setInterval(function () { tick(st); }, 1000);
  }
  function stopTimer(st) { if (st.timer) { clearInterval(st.timer); st.timer = null; } }
  async function tick(st) {
    if (!st.body || !st.body.isConnected) { stopTimer(st); return; }
    st.tick = (st.tick || 0) + 1;
    const done = await pollLog(st);
    if (done || st.tick % 3 === 0) await reloadRun(st);
    if (done) { stopTimer(st); emitChange(); }
  }

  function afterAction(id) {
    emitChange();
    if (cur && cur.id === id) reloadRun(cur);
  }

  // Refreshes every list that can show run state.
  function emitChange() {
    if (feed.mounted) loadFeed();
    if (wsRunsReload && wsRunsBody && wsRunsBody.isConnected) wsRunsReload();
    if (Tf.workspaces && typeof Tf.workspaces.reload === 'function') Tf.workspaces.reload();
  }

  function bindViewer(body) {
    if (body._tfRunBound) return;
    body._tfRunBound = true;
    body.addEventListener('click', function (e) {
      const st = cur;
      if (!st) return;
      const tab = e.target.closest('[data-run-tab]');
      if (tab) { st.sub = tab.getAttribute('data-run-tab'); renderBody(st); return; }
      const act = e.target.closest('[data-run-act]');
      if (act) {
        const a = act.getAttribute('data-run-act');
        if (a === 'apply') Tf.runs.apply(st.id);
        else if (a === 'discard') Tf.runs.discard(st.id);
        else if (a === 'cancel') Tf.runs.cancel(st.id);
        return;
      }
      const lg = e.target.closest('[data-log]');
      if (lg) {
        const a = lg.getAttribute('data-log');
        if (a === 'prev') gotoHit(st.log, -1);
        else if (a === 'next') gotoHit(st.log, 1);
        else if (a === 'copy') Tf.copy(plainText(st.log));
        else if (a === 'download') {
          const url = URL.createObjectURL(new Blob([plainText(st.log)], { type: 'text/plain;charset=utf-8' }));
          const link = document.createElement('a');
          link.href = url; link.download = 'terraform-run-' + st.id + '.log';
          document.body.appendChild(link); link.click(); link.remove();
          setTimeout(function () { URL.revokeObjectURL(url); }, 1000);
        }
      }
    });
    body.addEventListener('input', Tf.debounce(function (e) {
      if (e.target.id === 'tfLogSearch' && cur) { cur.log.q = e.target.value.trim(); cur.log.cur = -1; paintLog(document.getElementById('tfRunLog'), cur.log); }
    }, 150));
    body.addEventListener('change', function (e) {
      if (!cur) return;
      if (e.target.id === 'tfLogFollow') cur.log.follow = e.target.checked;
      else if (e.target.id === 'tfLogWrap') { cur.log.wrap = e.target.checked; paintLog(document.getElementById('tfRunLog'), cur.log); }
    });
  }

  function openViewer(runId) {
    const id = Number(runId);
    const st = { id: id, run: null, sub: null, log: newLog(), timer: null, tick: 0, fails: 0, done: false, polling: false, body: null, rendered: false };
    const view = {
      title: T('tf_run_title', 'Run #{id}', { id: id }), subtitle: '', wide: true,
      mount: function (body) {
        st.body = body;
        cur = st;
        bindViewer(body);
        boot(st);
      },
      unmount: function () { stopTimer(st); st.body = null; if (cur === st) cur = null; },
    };
    return Tf.drawer.current() ? Tf.drawer.push(view) : Tf.drawer.open(view);
  }
  async function boot(st) {
    try {
      const r = await Tf.api.get('/api/terraform/runs/' + st.id);
      if (!st.body) return;
      st.run = r.run;
      st.sub = st.sub || defaultSub(st.run);
      renderBody(st);
    } catch (e) {
      if (st.body) st.body.innerHTML = Tf.ui.error(Tf.errMsg(e));
      return;
    }
    await pollLog(st); // whole log from offset 0, then follow while the run is active
    if (isActive(st.run.status)) startTimer(st);
  }

  /* ---------- run actions ---------- */
  // Apply needs an approval comment; a blocked IaC/secret scan (409) is handed to the IaC gate dialog.
  async function apply(run, force) {
    const id = typeof run === 'object' ? run.id : Number(run);
    if (busy.apply) return;
    const comment = val('tfApprComment').trim();
    if (comment.length < 3) { showApprErr(T('tf_run_gate_comment_req', '')); return; }
    busy.apply = true;
    try {
      await Tf.api.post('/api/terraform/runs/' + id + '/apply', { comment: comment, override_policy: checked('tfApprOverride'), ignore_iac_gate: !!force });
      Tf.toast(T('tf_run_apply_ok', 'Apply started'), 'success');
      afterAction(id);
    } catch (e) {
      if (e.status === 409 && e.data && e.data.findings_summary && window.IacScan && typeof window.IacScan.gateBlocked === 'function') {
        window.IacScan.gateBlocked(e.data.findings_summary, function () { apply(id, true); });
      } else showApprErr(Tf.errMsg(e));
    } finally { busy.apply = false; }
  }
  async function discard(run) {
    const id = typeof run === 'object' ? run.id : Number(run);
    if (busy.discard) return;
    const comment = val('tfApprComment').trim();
    if (comment.length < 3) { showApprErr(T('tf_run_gate_comment_req', '')); return; }
    const ok = await Tf.confirm({ title: T('tf_run_discard_title', 'Discard this run?'), message: T('tf_run_discard_msg', ''), confirmText: T('tf_run_discard', 'Discard Run'), danger: true });
    if (!ok) return;
    busy.discard = true;
    try {
      await Tf.api.post('/api/terraform/runs/' + id + '/discard', { comment: comment });
      Tf.toast(T('tf_run_discard_ok', 'Run discarded'), 'success');
      afterAction(id);
    } catch (e) { showApprErr(Tf.errMsg(e)); }
    finally { busy.discard = false; }
  }
  async function cancel(run) {
    const id = typeof run === 'object' ? run.id : Number(run);
    if (busy.cancel) return;
    const ok = await Tf.confirm({ title: T('tf_run_cancel_title', 'Cancel this run?'), message: T('tf_run_cancel_msg', ''), confirmText: T('tf_run_cancel', 'Cancel Run'), danger: true });
    if (!ok) return;
    busy.cancel = true;
    try {
      await Tf.api.post('/api/terraform/runs/' + id + '/cancel');
      Tf.toast(T('tf_run_canceled', 'Run canceled'), 'success');
      afterAction(id);
    } catch (e) { Tf.toast(T('tf_run_action_failed', 'Action failed') + ': ' + Tf.errMsg(e), 'error'); }
    finally { busy.cancel = false; }
  }

  /* ---------- workspace drawer tab: Runs ---------- */
  let wsRunsReload = null, wsRunsBody = null;
  async function loadWsRuns(body, wsId) {
    const list = body.querySelector('#tfWsRunList');
    if (!list) return;
    try {
      const r = await Tf.api.get('/api/terraform/workspaces/' + wsId + '/runs?limit=50');
      body._tfWsRuns = r.runs || [];
    } catch (e) {
      list.innerHTML = Tf.ui.error(Tf.errMsg(e));
      return;
    }
    paintWsRuns(body);
  }
  function paintWsRuns(body) {
    const list = body.querySelector('#tfWsRunList');
    if (!list) return;
    const f = (body.querySelector('#tfWsRunFilter') || {}).value || 'all';
    const rows = (body._tfWsRuns || []).filter(function (r) {
      if (f === 'all') return true;
      if (f === 'active') return isActive(r.status);
      if (f === 'awaiting') return r.status === 'awaiting_approval';
      if (f === 'applied') return r.status === 'applied';
      if (f === 'failed') return r.status === 'errored' || r.status === 'policy_failed';
      return true;
    });
    list.innerHTML = rows.length ? '<table class="tf-table"><tbody>' + rows.map(function (r) {
      return '<tr class="tf-run-row" data-run="' + r.id + '" tabindex="0"><td class="tf-mono">#' + r.id + '</td><td>' + Tf.ui.badge(r.status) + elapsedHtml(r) + '</td>' +
        '<td>' + esc(r.type) + (r.speculative ? ' · ' + esc(T('tf_run_speculative_tag', 'speculative')) : '') + '<div class="tf-muted tf-small">' + esc(r.trigger_kind) + '</div></td>' +
        '<td class="tf-mono">' + esc(Tf.fmt.counts(r.counts)) + '</td><td class="tf-muted">' + esc(Tf.fmt.ago(r.created_at)) + '</td></tr>';
    }).join('') + '</tbody></table>' : Tf.ui.empty({ icon: 'history', desc: T('tf_ws_runs_empty', 'No runs for this workspace yet.') });
  }

  Tf.registerWsTab({
    id: 'runs', icon: 'history', labelKey: 'tf_ws_tab_runs', fallback: 'Runs', order: 3,
    render: function (body, ws) {
      body.innerHTML = '<div class="tf-row tf-ws-runs-bar">' +
        '<select id="tfWsRunFilter" class="form-input" aria-label="' + esc(T('tf_ws_tab_runs', 'Runs')) + '">' +
        [['all', 'tf_ws_runs_all', 'All runs'], ['active', 'tf_ws_runs_active', 'Active'], ['awaiting', 'tf_ws_runs_awaiting', 'Awaiting approval'], ['applied', 'tf_ws_runs_applied', 'Applied'], ['failed', 'tf_ws_runs_failed', 'Failed']]
          .map(function (o) { return '<option value="' + o[0] + '">' + esc(T(o[1], o[2])) + '</option>'; }).join('') +
        '</select><span class="tf-grow"></span>' +
        '<button type="button" id="tfRunStartBtn" class="btn btn-sm btn-primary" data-ws-run="start">' + Tf.icon('play', 'icon-xs') + ' ' + esc(T('tf_run_start', 'Start Run')) + '</button></div>' +
        '<div id="tfWsRunList"></div>';
      if (!body._tfWsBound) {
        body._tfWsBound = true;
        body.addEventListener('click', function (e) {
          const row = e.target.closest('[data-run]');
          if (row) { Tf.runs.openViewer(Number(row.getAttribute('data-run'))); return; }
          if (e.target.closest('[data-ws-run="start"]') && body._tfWs) {
            startDialog(body._tfWs.id, 'plan', { wsName: body._tfWs.name });
          }
        });
        body.addEventListener('change', function (e) { if (e.target.id === 'tfWsRunFilter') paintWsRuns(body); });
      }
      body._tfWs = ws;
      wsRunsBody = body;
      wsRunsReload = function () { loadWsRuns(body, ws.id); };
      loadWsRuns(body, ws.id);
    },
  });

  /* ---------- public namespace ---------- */
  Tf.runs = {
    openViewer: openViewer,
    startDialog: startDialog,
    renderDiff: renderDiff,
    apply: apply,
    discard: discard,
    cancel: cancel,
    refreshFeed: function () { if (feed.mounted) loadFeed(); },
    stripAnsi: stripAnsi,
  };
})();
