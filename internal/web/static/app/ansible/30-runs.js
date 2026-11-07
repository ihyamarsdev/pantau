/* Runs: global feed tab, per-playbook history, and the Run viewer (Log | Recap | Tasks | Diff) as a drawer view. */
(function () {
  'use strict';
  const Ans = window.Ans;
  const esc = Ans.esc, T = Ans.t, icon = Ans.icon;

  Ans.addI18n({
    en: {
      ans_rn_sub_history: 'History', ans_rn_sub_scheduled: 'Scheduled', ans_rn_search: 'Search runs…', ans_rn_all_playbooks: 'All playbooks',
      ans_rn_f_all: 'All', ans_rn_f_running: 'Running', ans_rn_f_failed: 'Failed', ans_rn_f_success: 'Success', ans_rn_f_cancelled: 'Cancelled',
      ans_rn_col_run: 'Run', ans_rn_col_playbook: 'Playbook', ans_rn_col_mode: 'Mode', ans_rn_col_targets: 'Targets', ans_rn_col_result: 'Result',
      ans_rn_col_started: 'Started', ans_rn_col_duration: 'Duration', ans_rn_col_exit: 'Exit', ans_rn_col_trigger: 'Trigger',
      ans_rn_empty_title: 'No runs yet', ans_rn_empty_desc: 'Runs appear here as soon as a playbook is launched manually or by a schedule.', ans_rn_no_match: 'No runs match the filters',
      ans_rn_load_failed: 'Could not load runs', ans_rn_latest: 'Showing the latest {n} runs', ans_rn_hosts: '{n} hosts', ans_rn_host: '1 host',
      ans_rn_manual: 'manual', ans_rn_schedule: 'schedule', ans_rn_sched_missing: 'The schedule list is not available.',
      ans_rn_title: 'Run #{id}', ans_rn_cancel: 'Cancel run', ans_rn_cancel_title: 'Cancel this run?', ans_rn_cancel_msg: 'The running ansible-playbook process will be stopped.',
      ans_rn_cancel_failed: 'Could not cancel the run', ans_rn_cancelled_toast: 'Cancel requested',
      ans_rn_tab_log: 'Log', ans_rn_tab_recap: 'Recap', ans_rn_tab_tasks: 'Tasks', ans_rn_tab_diff: 'Diff',
      ans_rn_meta_targets: 'Targets', ans_rn_meta_tags: 'Tags', ans_rn_meta_skip: 'Skip tags', ans_rn_meta_limit: 'Limit', ans_rn_meta_vars: 'Extra vars',
      ans_rn_meta_started: 'Started', ans_rn_meta_finished: 'Finished', ans_rn_meta_become: 'become', ans_rn_meta_commit: 'Commit', ans_rn_meta_by: 'Trigger',
      ans_rn_log_search: 'Search in log…', ans_rn_follow: 'Follow', ans_rn_wrap: 'Wrap', ans_rn_copy: 'Copy', ans_rn_download: 'Download', ans_rn_copied: 'Log copied',
      ans_rn_prev: 'Previous match', ans_rn_next: 'Next match', ans_rn_matches: '{i}/{n}', ans_rn_no_matches: 'No matches', ans_rn_log_empty: 'No output yet…', ans_rn_log_failed: 'Could not read the log',
      ans_rn_recap_wait: 'The recap is available when the run has finished.', ans_rn_recap_none: 'This run produced no recap.', ans_rn_recap_total: 'Total',
      ans_rn_c_host: 'Host', ans_rn_c_ok: 'ok', ans_rn_c_changed: 'changed', ans_rn_c_failed: 'failed', ans_rn_c_unreachable: 'unreachable', ans_rn_c_skipped: 'skipped', ans_rn_c_rescued: 'rescued', ans_rn_c_ignored: 'ignored',
      ans_rn_tasks_none: 'No tasks were found in the log yet.', ans_rn_t_all: 'All', ans_rn_t_failed: 'Failed', ans_rn_t_changed: 'Changed', ans_rn_t_ok: 'OK', ans_rn_t_skipped: 'Skipped',
      ans_rn_tasks_search: 'Search tasks and hosts…', ans_rn_tasks_filtered: 'Nothing matches the filter',
      ans_rn_st_ok: 'ok', ans_rn_st_changed: 'changed', ans_rn_st_fatal: 'failed', ans_rn_st_unreachable: 'unreachable', ans_rn_st_skipping: 'skipped', ans_rn_st_ignored: 'ignored',
      ans_rn_diff_none: 'No file diffs in this run. Launch the playbook in Diff mode to see them.', ans_rn_diff_task: 'Task', ans_rn_diff_files: '{n} changed files',
      ans_rn_poll_failed: 'Lost connection to the server; retrying…',
    },
    id: {
      ans_rn_sub_history: 'Riwayat', ans_rn_sub_scheduled: 'Terjadwal', ans_rn_search: 'Cari run…', ans_rn_all_playbooks: 'Semua playbook',
      ans_rn_f_all: 'Semua', ans_rn_f_running: 'Berjalan', ans_rn_f_failed: 'Gagal', ans_rn_f_success: 'Sukses', ans_rn_f_cancelled: 'Dibatalkan',
      ans_rn_col_run: 'Run', ans_rn_col_playbook: 'Playbook', ans_rn_col_mode: 'Mode', ans_rn_col_targets: 'Target', ans_rn_col_result: 'Hasil',
      ans_rn_col_started: 'Mulai', ans_rn_col_duration: 'Durasi', ans_rn_col_exit: 'Exit', ans_rn_col_trigger: 'Pemicu',
      ans_rn_empty_title: 'Belum ada run', ans_rn_empty_desc: 'Run muncul di sini begitu playbook dijalankan manual atau oleh jadwal.', ans_rn_no_match: 'Tidak ada run yang cocok dengan filter',
      ans_rn_load_failed: 'Gagal memuat run', ans_rn_latest: 'Menampilkan {n} run terbaru', ans_rn_hosts: '{n} host', ans_rn_host: '1 host',
      ans_rn_manual: 'manual', ans_rn_schedule: 'jadwal', ans_rn_sched_missing: 'Daftar jadwal tidak tersedia.',
      ans_rn_title: 'Run #{id}', ans_rn_cancel: 'Batalkan run', ans_rn_cancel_title: 'Batalkan run ini?', ans_rn_cancel_msg: 'Proses ansible-playbook yang berjalan akan dihentikan.',
      ans_rn_cancel_failed: 'Gagal membatalkan run', ans_rn_cancelled_toast: 'Pembatalan diminta',
      ans_rn_tab_log: 'Log', ans_rn_tab_recap: 'Recap', ans_rn_tab_tasks: 'Task', ans_rn_tab_diff: 'Diff',
      ans_rn_meta_targets: 'Target', ans_rn_meta_tags: 'Tag', ans_rn_meta_skip: 'Lewati tag', ans_rn_meta_limit: 'Limit', ans_rn_meta_vars: 'Extra vars',
      ans_rn_meta_started: 'Mulai', ans_rn_meta_finished: 'Selesai', ans_rn_meta_become: 'become', ans_rn_meta_commit: 'Commit', ans_rn_meta_by: 'Pemicu',
      ans_rn_log_search: 'Cari di log…', ans_rn_follow: 'Ikuti', ans_rn_wrap: 'Bungkus', ans_rn_copy: 'Salin', ans_rn_download: 'Unduh', ans_rn_copied: 'Log disalin',
      ans_rn_prev: 'Hasil sebelumnya', ans_rn_next: 'Hasil berikutnya', ans_rn_matches: '{i}/{n}', ans_rn_no_matches: 'Tidak ada hasil', ans_rn_log_empty: 'Belum ada output…', ans_rn_log_failed: 'Gagal membaca log',
      ans_rn_recap_wait: 'Recap tersedia setelah run selesai.', ans_rn_recap_none: 'Run ini tidak menghasilkan recap.', ans_rn_recap_total: 'Total',
      ans_rn_c_host: 'Host', ans_rn_c_ok: 'ok', ans_rn_c_changed: 'berubah', ans_rn_c_failed: 'gagal', ans_rn_c_unreachable: 'tak terjangkau', ans_rn_c_skipped: 'dilewati', ans_rn_c_rescued: 'diselamatkan', ans_rn_c_ignored: 'diabaikan',
      ans_rn_tasks_none: 'Belum ada task yang ditemukan di log.', ans_rn_t_all: 'Semua', ans_rn_t_failed: 'Gagal', ans_rn_t_changed: 'Berubah', ans_rn_t_ok: 'OK', ans_rn_t_skipped: 'Dilewati',
      ans_rn_tasks_search: 'Cari task dan host…', ans_rn_tasks_filtered: 'Tidak ada yang cocok dengan filter',
      ans_rn_st_ok: 'ok', ans_rn_st_changed: 'berubah', ans_rn_st_fatal: 'gagal', ans_rn_st_unreachable: 'tak terjangkau', ans_rn_st_skipping: 'dilewati', ans_rn_st_ignored: 'diabaikan',
      ans_rn_diff_none: 'Tidak ada diff berkas pada run ini. Jalankan playbook dalam mode Diff untuk melihatnya.', ans_rn_diff_task: 'Task', ans_rn_diff_files: '{n} berkas berubah',
      ans_rn_poll_failed: 'Koneksi ke server terputus; mencoba lagi…',
    },
  });

  /* ===================== pure helpers (also exported as Ans.runs.util) ===================== */
  const ANSI_RE = /\x1b\[([0-9;]*)m/g;
  const ANSI_ANY = /\x1b\[[0-9;?]*[A-Za-z]/g;

  function stripAnsi(text) { return String(text).replace(ANSI_ANY, ''); }

  function escText(s) { return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;'); }

  // 256-colour index -> one of the 16 palette classes (rough but readable on the dark UI)
  function color256(n) {
    if (n < 16) return n;
    if (n >= 232) return n > 243 ? 15 : 8;
    const c = n - 16, r = Math.floor(c / 36), g = Math.floor(c / 6) % 6, b = c % 6;
    const idx = (r > 2 ? 1 : 0) | (g > 2 ? 2 : 0) | (b > 2 ? 4 : 0);
    return idx + ((r + g + b) / 3 > 3 ? 8 : 0);
  }
  function applySgr(st, codes) {
    for (let i = 0; i < codes.length; i++) {
      const c = codes[i];
      if (c === 0) { st.fg = -1; st.b = false; st.u = false; st.d = false; }
      else if (c === 1) st.b = true; else if (c === 2) st.d = true; else if (c === 4) st.u = true;
      else if (c === 22) { st.b = false; st.d = false; } else if (c === 24) st.u = false;
      else if (c >= 30 && c <= 37) st.fg = c - 30; else if (c >= 90 && c <= 97) st.fg = c - 90 + 8; else if (c === 39) st.fg = -1;
      else if (c === 38 && codes[i + 1] === 5 && codes[i + 2] !== undefined) { st.fg = color256(codes[i + 2]); i += 2; }
      else if (c === 38 && codes[i + 1] === 2) i += 4;
    }
  }
  function spanOpen(st) {
    const cls = [];
    if (st.fg >= 0) cls.push('ans-fg-' + st.fg);
    if (st.b) cls.push('ans-b'); if (st.u) cls.push('ans-u'); if (st.d) cls.push('ans-d');
    return cls.length ? '<span class="' + cls.join(' ') + '">' : '';
  }
  // Escapes first, then wraps colour runs; `state` carries SGR state across chunks/lines. Returns HTML.
  function ansiToHtml(text, state) {
    const st = state || { fg: -1, b: false, u: false, d: false };
    const src = String(text).replace(/\x1b\[[0-9;?]*[A-Za-ln-z]/g, '');
    let out = '', last = 0, open = spanOpen(st);
    const emit = function (s) { if (!s) return; const e = escText(s); out += open ? open + e + '</span>' : e; };
    src.replace(ANSI_RE, function (m, p, off) {
      emit(src.slice(last, off)); last = off + m.length;
      applySgr(st, p === '' ? [0] : p.split(';').map(Number));
      open = spanOpen(st);
      return m;
    });
    emit(src.slice(last));
    return out;
  }

  const RESULT_RE = /^(ok|changed|skipping|fatal|failed|unreachable|included):\s*\[([^\]]+)\](.*)$/;
  // Play > Task > Host tree from an Ansible default-callback log.
  function parseTaskTree(text) {
    const lines = stripAnsi(text).split('\n');
    const plays = [];
    let play = null, task = null, host = null, inRecap = false, braces = 0;
    const startPlay = function (name) { play = { name: name, tasks: [] }; plays.push(play); task = null; host = null; };
    const startTask = function (name) { if (!play) startPlay(''); task = { name: name, hosts: [], notes: [] }; play.tasks.push(task); host = null; braces = 0; };
    for (let i = 0; i < lines.length; i++) {
      const line = lines[i];
      let m = /^PLAY RECAP\b/.exec(line);
      if (m) { inRecap = true; host = null; continue; }
      if (inRecap) continue;
      if ((m = /^PLAY \[(.*?)\]\s*\*+\s*$/.exec(line))) { startPlay(m[1]); continue; }
      if ((m = /^(?:TASK|RUNNING HANDLER) \[(.*?)\]\s*\*+\s*$/.exec(line))) { startTask(m[1]); continue; }
      if (!task) continue;
      if ((m = RESULT_RE.exec(line))) {
        let status = m[1], rest = m[3] || '';
        if (status === 'included') { host = null; continue; }
        if (status === 'fatal' || status === 'failed') status = /UNREACHABLE!/.test(rest) ? 'unreachable' : 'fatal';
        host = { host: m[2], status: status, detail: rest.replace(/^:?\s*/, '').replace(/^(FAILED|UNREACHABLE)!\s*/, '').replace(/^=>\s*/, '') };
        task.hosts.push(host);
        braces = (rest.match(/\{/g) || []).length - (rest.match(/\}/g) || []).length;
        continue;
      }
      if (/^\.\.\.ignoring\s*$/.test(line)) { if (host) host.status = 'ignored'; continue; }
      if (host && braces > 0) { // body of a multi-line `=> {` result
        host.detail += '\n' + line;
        braces += (line.match(/\{/g) || []).length - (line.match(/\}/g) || []).length;
        continue;
      }
      if (line.trim() !== '' && task.notes.length < 60) task.notes.push(line); // [ERROR] lines, Origin, code excerpt
    }
    return plays;
  }

  // `--- before` / `+++ after` blocks of check --diff runs, grouped per file.
  function parseDiff(text) {
    const lines = stripAnsi(text).split('\n');
    const files = [];
    let cur = null, task = '', i = 0;
    for (; i < lines.length; i++) {
      const line = lines[i];
      const tm = /^TASK \[(.*?)\]/.exec(line);
      if (tm) { task = tm[1]; cur = null; continue; }
      if (/^--- before\b/.test(line)) {
        cur = { file: '', task: task, rows: [], open: true, adds: 0, dels: 0 };
        cur.rows.push({ t: 'meta', x: line });
        files.push(cur);
        continue;
      }
      if (!cur) continue;
      if (/^\+\+\+ /.test(line)) { cur.file = line.replace(/^\+\+\+ (after:\s*)?/, ''); cur.rows.push({ t: 'meta', x: line }); continue; }
      if (/^@@/.test(line)) { cur.rows.push({ t: 'hunk', x: line }); continue; }
      if (/^\+/.test(line)) { cur.rows.push({ t: 'add', x: line }); cur.adds++; continue; }
      if (/^-/.test(line)) { cur.rows.push({ t: 'del', x: line }); cur.dels++; continue; }
      if (/^ /.test(line)) { cur.rows.push({ t: 'ctx', x: line }); continue; }
      cur = null;
    }
    files.forEach(function (f) { if (!f.file) { const m = f.rows[0] && /^--- (?:before:\s*)?(.*)$/.exec(f.rows[0].x); f.file = m ? m[1] : ''; } });
    return files.filter(function (f) { return f.rows.length > 1; });
  }

  // For failed hosts the JSON blob is mostly noise (ansible_facts, cmd, delta…): show msg/stderr/stdout/rc when parseable.
  function failureText(h) {
    const d = h.detail || '';
    if ((h.status !== 'fatal' && h.status !== 'unreachable') || d.charAt(0) !== '{') return d;
    try {
      const o = JSON.parse(d);
      const parts = [];
      if (o.msg) parts.push(String(o.msg));
      if (o.rc !== undefined && o.rc !== 0) parts.push('rc=' + o.rc);
      if (o.stderr) parts.push(String(o.stderr).trim());
      if (o.stdout) parts.push(String(o.stdout).trim());
      return parts.length ? parts.join('\n') : d;
    } catch (e) { return d; }
  }

  const util = { ansiToHtml: ansiToHtml, stripAnsi: stripAnsi, parseTaskTree: parseTaskTree, parseDiff: parseDiff, failureText: failureText };

  /* ===================== shared bits ===================== */
  const targetsOf = function (r) { try { const t = JSON.parse(r.targets || '[]'); return Array.isArray(t) ? t : []; } catch (e) { return []; } };
  const targetsText = function (r) { const n = targetsOf(r).length; return n === 1 ? T('ans_rn_host', '1 host') : T('ans_rn_hosts', '{n} hosts', { n: n }); };
  const durOf = function (r) { if (!r.started_at) return null; const end = r.finished_at ? Date.parse(r.finished_at) : Date.now(); return end - Date.parse(r.started_at); };
  const isActive = function (r) { return r.status === 'queued' || r.status === 'running'; };
  const exitBadge = function (r) {
    if (r.exit_code === null || r.exit_code === undefined) return '<span class="ans-muted">-</span>';
    return '<span class="ans-mono' + (r.exit_code ? ' ans-rn-bad' : '') + '">' + r.exit_code + '</span>';
  };

  function runRow(r, withPlaybook) {
    return '<tr class="ans-clickable" data-run="' + r.id + '" data-pb="' + r.playbook_id + '" tabindex="0">' +
      '<td class="ans-pb-st">' + Ans.ui.dot(r.status) + '<span class="ans-sr">' + esc(Ans.ui.statusLabel(r.status)) + '</span></td>' +
      '<td><span class="ans-mono">#' + r.id + '</span></td>' +
      (withPlaybook ? '<td><div class="ans-name"><span class="ans-pb-nm">' + esc(r.playbook_name) + '</span></div></td>' : '') +
      '<td><span class="badge badge-unknown">' + esc(r.mode) + '</span>' + (r.trigger === 'schedule' ? ' <span class="ans-muted" title="' + esc(T('ans_rn_schedule')) + '">' + icon('calendar', 'icon-sm') + '</span>' : '') + '</td>' +
      '<td class="ans-hide-sm">' + esc(targetsText(r)) + '</td>' +
      '<td class="ans-hide-md">' + Ans.fmt.pips(r.recap_totals) + '</td>' +
      '<td title="' + esc(Ans.fmt.dt(r.created_at)) + '">' + esc(Ans.fmt.ago(r.created_at)) + '</td>' +
      '<td class="ans-hide-sm">' + esc(Ans.fmt.dur(durOf(r))) + '</td>' +
      '<td class="ans-hide-md">' + exitBadge(r) + '</td></tr>';
  }
  function runTable(rows, withPlaybook) {
    return '<div class="ans-table-wrap"><table class="ans-table"><thead><tr><th scope="col" class="ans-pb-st"><span class="ans-sr">status</span></th><th scope="col">' + esc(T('ans_rn_col_run')) + '</th>' +
      (withPlaybook ? '<th scope="col">' + esc(T('ans_rn_col_playbook')) + '</th>' : '') +
      '<th scope="col">' + esc(T('ans_rn_col_mode')) + '</th><th scope="col" class="ans-hide-sm">' + esc(T('ans_rn_col_targets')) + '</th><th scope="col" class="ans-hide-md">' + esc(T('ans_rn_col_result')) + '</th>' +
      '<th scope="col">' + esc(T('ans_rn_col_started')) + '</th><th scope="col" class="ans-hide-sm">' + esc(T('ans_rn_col_duration')) + '</th><th scope="col" class="ans-hide-md">' + esc(T('ans_rn_col_exit')) + '</th></tr></thead><tbody>' +
      rows.map(function (r) { return runRow(r, withPlaybook); }).join('') + '</tbody></table></div>';
  }
  function bindRunRows(root) {
    const open = function (tr) { if (tr) openViewer(Number(tr.getAttribute('data-run')), { playbookId: Number(tr.getAttribute('data-pb')) }); };
    root.addEventListener('click', function (e) { open(e.target.closest('tr[data-run]')); });
    root.addEventListener('keydown', function (e) {
      const tr = e.target.closest('tr[data-run]');
      if (!tr || e.target !== tr) return;
      if (e.key === 'Enter') { e.preventDefault(); open(tr); }
      else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') { e.preventDefault(); const n = e.key === 'ArrowDown' ? tr.nextElementSibling : tr.previousElementSibling; if (n) n.focus(); }
    });
  }

  /* ===================== Runs tab ===================== */
  const tabState = { sub: Ans.store.get('rn.sub', 'history'), status: 'all', pb: '', q: '', runs: null, error: '', root: null, handle: null, stop: null };

  function filterRuns() {
    const q = tabState.q.trim().toLowerCase();
    return (tabState.runs || []).filter(function (r) {
      if (tabState.status === 'running' && !isActive(r)) return false;
      if (tabState.status !== 'all' && tabState.status !== 'running' && r.status !== tabState.status) return false;
      if (tabState.pb && String(r.playbook_id) !== tabState.pb) return false;
      return !q || (r.playbook_name + ' #' + r.id + ' ' + r.mode).toLowerCase().indexOf(q) >= 0;
    });
  }
  function paintHistory() {
    const box = tabState.root && tabState.root.querySelector('[data-rn="body"]');
    if (!box || tabState.sub !== 'history') return;
    const all = tabState.runs;
    if (all === null && !tabState.error) { box.innerHTML = Ans.ui.skeleton(6); return; }
    if (tabState.error && !all) { box.innerHTML = Ans.ui.error(tabState.error, 'data-rn-act="retry"'); return; }
    const chips = tabState.root.querySelector('[data-rn="chips"]');
    const count = function (f) { return all.filter(function (r) { return f === 'all' ? true : f === 'running' ? isActive(r) : r.status === f; }).length; };
    chips.innerHTML = [['all', 'All'], ['running', 'Running'], ['failed', 'Failed'], ['success', 'Success'], ['cancelled', 'Cancelled']].map(function (d) {
      return '<button type="button" class="ans-chip" data-status="' + d[0] + '" aria-pressed="' + (tabState.status === d[0]) + '">' + esc(T('ans_rn_f_' + d[0], d[1])) + ' <span class="count">' + count(d[0]) + '</span></button>';
    }).join('');
    const sel = tabState.root.querySelector('[data-rn="pb"]');
    const names = {}; all.forEach(function (r) { names[r.playbook_id] = r.playbook_name; });
    sel.innerHTML = '<option value="">' + esc(T('ans_rn_all_playbooks')) + '</option>' + Object.keys(names).sort(function (a, b) { return names[a].localeCompare(names[b]); }).map(function (id) { return '<option value="' + id + '">' + esc(names[id]) + '</option>'; }).join('');
    sel.value = tabState.pb;
    if (!all.length) { box.innerHTML = Ans.ui.empty({ icon: 'history', title: T('ans_rn_empty_title'), desc: T('ans_rn_empty_desc') }); return; }
    const rows = filterRuns();
    box.innerHTML = rows.length ? runTable(rows, true) + '<div class="ans-hint" style="margin-top:6px;">' + esc(T('ans_rn_latest', 'Showing the latest {n} runs', { n: all.length })) + '</div>' : Ans.ui.empty({ icon: 'search', title: T('ans_rn_no_match') });
  }
  async function loadRuns() {
    try { const d = await Ans.api.get('/api/ansible/runs?limit=100'); tabState.runs = (d && d.runs) || []; tabState.error = ''; }
    catch (e) { tabState.error = T('ans_rn_load_failed') + ': ' + e.message; }
    paintHistory();
  }
  function paintSub() {
    const root = tabState.root;
    if (!root) return;
    root.querySelectorAll('[data-sub]').forEach(function (b) { b.setAttribute('aria-pressed', String(b.getAttribute('data-sub') === tabState.sub)); });
    root.querySelector('[data-rn="filters"]').hidden = tabState.sub !== 'history';
    if (tabState.handle && tabState.handle.destroy) { try { tabState.handle.destroy(); } catch (e) { console.error(e); } }
    tabState.handle = null;
    const body = root.querySelector('[data-rn="body"]');
    if (tabState.sub === 'scheduled') {
      body.innerHTML = '';
      if (Ans.schedules && Ans.schedules.mountAll) tabState.handle = Ans.schedules.mountAll(body);
      else body.innerHTML = '<span class="ans-muted">' + esc(T('ans_rn_sched_missing')) + '</span>';
    } else { paintHistory(); loadRuns(); }
  }
  function renderTab(panel) {
    tabState.root = panel;
    panel.innerHTML =
      '<div class="ans-toolbar"><div class="ans-seg" role="group"><button type="button" data-sub="history" aria-pressed="true">' + esc(T('ans_rn_sub_history')) + '</button><button type="button" data-sub="scheduled" aria-pressed="false">' + esc(T('ans_rn_sub_scheduled')) + '</button></div>' +
      '<span data-rn="filters" class="ans-toolbar"><div class="search-box"><input type="text" class="form-input" data-rn="q" placeholder="' + esc(T('ans_rn_search')) + '" aria-label="' + esc(T('ans_rn_search')) + '" value="' + esc(tabState.q) + '" /></div>' +
      '<select class="form-input" data-rn="pb" aria-label="' + esc(T('ans_rn_col_playbook')) + '"></select><span class="ans-row" data-rn="chips"></span></span></div><div data-rn="body"></div>';
    panel.addEventListener('click', function (e) {
      const sub = e.target.closest('[data-sub]');
      if (sub) { tabState.sub = sub.getAttribute('data-sub'); Ans.store.set('rn.sub', tabState.sub); paintSub(); return; }
      const st = e.target.closest('[data-status]'); if (st) { tabState.status = st.getAttribute('data-status'); paintHistory(); return; }
      if (e.target.closest('[data-rn-act="retry"]')) { tabState.error = ''; loadRuns(); }
    });
    panel.addEventListener('input', function (e) { if (e.target.matches('[data-rn="q"]')) { tabState.q = e.target.value; paintHistory(); } });
    panel.addEventListener('change', function (e) { if (e.target.matches('[data-rn="pb"]')) { tabState.pb = e.target.value; paintHistory(); } });
    bindRunRows(panel);
    if (!tabState.stop) {
      let tick = 0;
      tabState.stop = Ans.every(function () {
        if (Ans.state.tab !== 'runs' || tabState.sub !== 'history' || Ans.drawer.current()) return;
        if (!(tabState.runs || []).some(isActive) && ++tick % 5 !== 0) return;
        loadRuns();
      }, 3000);
    }
    paintSub();
  }

  /* ===================== per-playbook history ===================== */
  function mountHistory(container, o) {
    o = o || {};
    let runs = null, stop = null, dead = false;
    const draw = function (err) {
      if (dead) return;
      if (err) { container.innerHTML = Ans.ui.error(err, 'data-rn-act="retry"'); return; }
      if (runs === null) { container.innerHTML = Ans.ui.skeleton(5); return; }
      container.innerHTML = runs.length ? runTable(runs, false) : Ans.ui.empty({ icon: 'history', title: T('ans_rn_empty_title'), desc: T('ans_rn_empty_desc') });
    };
    const load = function () {
      return Ans.api.get('/api/ansible/playbooks/' + o.playbookId + '/runs?limit=' + (o.limit || 100)).then(function (d) { runs = (d && d.runs) || []; draw(); }).catch(function (e) { if (runs === null) draw(T('ans_rn_load_failed') + ': ' + e.message); });
    };
    container.addEventListener('click', function (e) { if (e.target.closest('[data-rn-act="retry"]')) { runs = null; draw(); load(); } });
    bindRunRows(container);
    draw(); load();
    let tick = 0;
    stop = Ans.every(function () { if (dead || Ans.state.tab === undefined) return; if (!(runs || []).some(isActive) && ++tick % 5 !== 0) return; load(); }, 3000);
    return { destroy: function () { dead = true; if (stop) stop(); container.innerHTML = ''; } };
  }

  /* ===================== run viewer ===================== */
  function openViewer(runId, o) {
    o = o || {};
    const ctx = {
      id: runId, run: null, recap: null, tab: 'log', off: 0, full: '', timer: null, durTimer: null, dead: false, fetching: false, busy: false,
      follow: true, wrap: false, lines: [], partial: '', ansi: { fg: -1, b: false, u: false, d: false }, queue: [], queueTimer: null,
      hits: [], hitIdx: -1, search: '', taskFilter: 'all', taskQ: '', treeDirty: true, root: null, ctrl: null, tickN: 0,
    };
    const view = {
      key: 'run:' + runId, title: T('ans_rn_title', 'Run #{id}', { id: runId }), subtitle: '', wide: true, actions: '',
      mount: function (body, ctrl) { ctx.root = body; ctx.ctrl = ctrl; buildViewer(ctx); },
      unmount: function () { ctx.dead = true; clearTimeout(ctx.timer); clearInterval(ctx.durTimer); clearTimeout(ctx.queueTimer); },
    };
    return Ans.drawer.push(view);
  }

  function paintHeader(ctx) {
    const r = ctx.run;
    if (!r) return;
    ctx.ctrl.setTitle(r.playbook_name || T('ans_rn_title', 'Run #{id}', { id: r.id }));
    ctx.ctrl.setSubtitle(Ans.ui.badge(r.status) + '<span class="ans-mono">#' + r.id + '</span><span class="badge badge-unknown">' + esc(r.mode) + '</span><span class="ans-rn-dur ans-muted">' + esc(Ans.fmt.dur(durOf(r))) + '</span>' +
      (r.exit_code !== null && r.exit_code !== undefined ? '<span class="ans-muted">exit ' + r.exit_code + '</span>' : ''));
    ctx.ctrl.setActions(isActive(r) ? '<button type="button" class="btn btn-sm btn-danger" data-rv="cancel">' + icon('square', 'icon-sm') + ' ' + esc(T('ans_rn_cancel')) + '</button>' : '');
    const head = document.querySelector('#ansDrawerRoot .ans-drawer-actions');
    if (head) head.onclick = function (e) { if (e.target.closest('[data-rv="cancel"]')) cancelRun(ctx); };
    const meta = ctx.root.querySelector('[data-rv="meta"]');
    if (meta) {
      const targets = targetsOf(r);
      const item = function (k, v) { return v ? '<div class="ans-rn-mi"><span class="ans-rn-mk">' + esc(T(k)) + '</span><span class="ans-rn-mv">' + v + '</span></div>' : ''; };
      let vars = '';
      if (r.extra_vars) { vars = '<code class="ans-mono">' + esc(r.extra_vars.length > 140 ? r.extra_vars.slice(0, 140) + '…' : r.extra_vars) + '</code>'; }
      meta.innerHTML =
        item('ans_rn_meta_targets', targets.length ? targets.map(function (t) { return '<span class="badge badge-unknown">' + esc(t.name || t.id) + '</span>'; }).join(' ') : '') +
        item('ans_rn_meta_by', esc(r.trigger === 'schedule' ? T('ans_rn_schedule') : T('ans_rn_manual'))) +
        item('ans_rn_meta_tags', r.tags ? '<code class="ans-mono">' + esc(r.tags) + '</code>' : '') + item('ans_rn_meta_skip', r.skip_tags ? '<code class="ans-mono">' + esc(r.skip_tags) + '</code>' : '') +
        item('ans_rn_meta_limit', r.limit ? '<code class="ans-mono">' + esc(r.limit) + '</code>' : '') + item('ans_rn_meta_vars', vars) +
        item('ans_rn_meta_become', r.become ? esc(T('ans_rn_meta_become')) : '') + item('ans_rn_meta_commit', r.commit_sha ? '<code class="ans-mono">' + esc(r.commit_sha.slice(0, 10)) + '</code>' : '') +
        item('ans_rn_meta_started', esc(Ans.fmt.dt(r.started_at || r.created_at))) + item('ans_rn_meta_finished', r.finished_at ? esc(Ans.fmt.dt(r.finished_at)) : '');
    }
    paintTabBadges(ctx);
  }
  function paintTabBadges(ctx) {
    const diffTab = ctx.root.querySelector('[data-rvtab="diff"]');
    if (diffTab) diffTab.disabled = !(ctx.run && (ctx.run.mode === 'diff' || /(^|\n)\x1b?\[?[0-9;]*m?--- before/.test(ctx.full)));
  }

  async function cancelRun(ctx) {
    const ok = await Ans.confirm({ title: T('ans_rn_cancel_title'), message: T('ans_rn_cancel_msg'), confirmText: T('ans_rn_cancel'), danger: true });
    if (!ok) return;
    try { await Ans.api.post('/api/ansible/runs/' + ctx.id + '/cancel'); Ans.toast(T('ans_rn_cancelled_toast'), 'success'); }
    catch (e) { Ans.toast(T('ans_rn_cancel_failed') + ': ' + e.message, 'error'); }
  }

  function buildViewer(ctx) {
    ctx.root.innerHTML =
      '<div class="ans-rn-meta" data-rv="meta"></div>' +
      '<div class="ans-drawer-tabs" role="tablist">' + [['log', 'scroll-text'], ['recap', 'list-checks'], ['tasks', 'layers'], ['diff', 'git-compare']].map(function (t) {
        return '<button type="button" role="tab" data-rvtab="' + t[0] + '" aria-selected="' + (t[0] === ctx.tab) + '" tabindex="' + (t[0] === ctx.tab ? 0 : -1) + '">' + icon(t[1], 'icon-sm') + '<span>' + esc(T('ans_rn_tab_' + t[0])) + '</span></button>';
      }).join('') + '</div><div class="ans-rn-pane" data-rv="pane"></div>';
    const tabs = ctx.root.querySelector('.ans-drawer-tabs');
    tabs.addEventListener('click', function (e) { const b = e.target.closest('[data-rvtab]'); if (b && !b.disabled) selectTab(ctx, b.getAttribute('data-rvtab')); });
    tabs.addEventListener('keydown', function (e) {
      const btns = Array.prototype.filter.call(tabs.querySelectorAll('button'), function (b) { return !b.disabled; });
      let i = btns.indexOf(document.activeElement);
      if (i < 0) return;
      if (e.key === 'ArrowRight') i = (i + 1) % btns.length; else if (e.key === 'ArrowLeft') i = (i - 1 + btns.length) % btns.length; else return;
      e.preventDefault(); btns[i].focus(); selectTab(ctx, btns[i].getAttribute('data-rvtab'));
    });
    ctx.durTimer = setInterval(function () {
      if (ctx.dead || !ctx.run || !isActive(ctx.run)) return;
      const el = document.querySelector('#ansDrawerRoot .ans-rn-dur'); if (el) el.textContent = Ans.fmt.dur(durOf(ctx.run));
    }, 1000);
    selectTab(ctx, 'log', true);
    refreshRun(ctx).then(function () { pollLoop(ctx); });
  }

  function selectTab(ctx, tab, force) {
    if (ctx.tab === tab && !force) return;
    ctx.tab = tab;
    ctx.root.querySelectorAll('[data-rvtab]').forEach(function (b) { const on = b.getAttribute('data-rvtab') === tab; b.setAttribute('aria-selected', String(on)); b.tabIndex = on ? 0 : -1; });
    const pane = ctx.root.querySelector('[data-rv="pane"]');
    pane.className = 'ans-rn-pane ans-rn-pane-' + tab;
    if (tab === 'log') buildLog(ctx, pane);
    else if (tab === 'recap') paintRecap(ctx, pane);
    else if (tab === 'tasks') paintTasks(ctx, pane);
    else if (tab === 'diff') paintDiff(ctx, pane);
  }

  async function refreshRun(ctx) {
    try {
      const d = await Ans.api.get('/api/ansible/runs/' + ctx.id);
      if (ctx.dead) return;
      ctx.run = d.run; ctx.recap = d.recap || null;
      paintHeader(ctx);
      if (ctx.tab === 'recap') paintRecap(ctx, ctx.root.querySelector('[data-rv="pane"]'));
    } catch (e) { if (!ctx.dead && !ctx.run) ctx.root.querySelector('[data-rv="pane"]').innerHTML = Ans.ui.error(e.message); }
  }

  async function pollLoop(ctx) {
    if (ctx.dead) return;
    let finished = false;
    try {
      const d = await Ans.api.get('/api/ansible/runs/' + ctx.id + '/log?offset=' + ctx.off);
      if (ctx.dead) return;
      if (d && typeof d.offset === 'number') ctx.off = d.offset;
      if (d && d.data) appendText(ctx, d.data);
      finished = !!(d && d.finished);
      if (ctx.hint) { ctx.hint = false; }
    } catch (e) { ctx.hint = true; }
    ctx.tickN++;
    if (!finished && (ctx.tickN % 2 === 0)) await refreshRun(ctx);
    if (finished) { await refreshRun(ctx); paintTabBadges(ctx); if (ctx.tab === 'tasks') paintTasks(ctx, ctx.root.querySelector('[data-rv="pane"]')); if (ctx.tab === 'diff') paintDiff(ctx, ctx.root.querySelector('[data-rv="pane"]')); return; }
    ctx.timer = setTimeout(function () { pollLoop(ctx); }, 1000);
  }

  /* ---- log pane ---- */
  function buildLog(ctx, pane) {
    pane.innerHTML =
      '<div class="ans-rn-logbar"><div class="search-box ans-rn-lsearch"><input type="text" class="form-input" data-lg="q" placeholder="' + esc(T('ans_rn_log_search')) + '" aria-label="' + esc(T('ans_rn_log_search')) + '" value="' + esc(ctx.search) + '" /></div>' +
      '<span class="ans-mono ans-muted" data-lg="count"></span>' +
      '<button type="button" class="btn btn-sm btn-ghost ans-btn-icon" data-lg="prev" title="' + esc(T('ans_rn_prev')) + '" aria-label="' + esc(T('ans_rn_prev')) + '">' + icon('chevron-up', 'icon-sm') + '</button>' +
      '<button type="button" class="btn btn-sm btn-ghost ans-btn-icon" data-lg="next" title="' + esc(T('ans_rn_next')) + '" aria-label="' + esc(T('ans_rn_next')) + '">' + icon('chevron-down', 'icon-sm') + '</button><span class="ans-spacer"></span>' +
      '<button type="button" class="ans-chip" data-lg="follow" aria-pressed="' + ctx.follow + '">' + icon('arrow-down', 'icon-sm') + esc(T('ans_rn_follow')) + '</button>' +
      '<button type="button" class="ans-chip" data-lg="wrap" aria-pressed="' + ctx.wrap + '">' + esc(T('ans_rn_wrap')) + '</button>' +
      '<button type="button" class="btn btn-sm" data-lg="copy">' + icon('copy', 'icon-sm') + ' ' + esc(T('ans_rn_copy')) + '</button>' +
      '<button type="button" class="btn btn-sm" data-lg="download">' + icon('download', 'icon-sm') + ' ' + esc(T('ans_rn_download')) + '</button></div>' +
      '<div class="ans-rn-log' + (ctx.wrap ? ' wrap' : '') + '" data-lg="pane" tabindex="0" role="log" aria-live="off"></div><div class="ans-error" data-lg="err" hidden></div>';
    const logEl = pane.querySelector('[data-lg="pane"]');
    ctx.logEl = logEl;
    const frag = document.createDocumentFragment();
    ctx.lines.forEach(function (l) { frag.appendChild(lineEl(l)); });
    logEl.appendChild(frag);
    if (!ctx.lines.length) logEl.innerHTML = '<div class="ans-muted ans-rn-empty">' + esc(T('ans_rn_log_empty')) + '</div>';
    logEl.addEventListener('scroll', function () {
      const near = logEl.scrollHeight - logEl.scrollTop - logEl.clientHeight < 48;
      if (ctx.follow !== near) { ctx.follow = near; const f = pane.querySelector('[data-lg="follow"]'); if (f) f.setAttribute('aria-pressed', String(near)); }
    });
    pane.addEventListener('click', function (e) {
      const b = e.target.closest('[data-lg]'); if (!b) return;
      const k = b.getAttribute('data-lg');
      if (k === 'follow') { ctx.follow = !ctx.follow; b.setAttribute('aria-pressed', String(ctx.follow)); if (ctx.follow) logEl.scrollTop = logEl.scrollHeight; }
      else if (k === 'wrap') { ctx.wrap = !ctx.wrap; b.setAttribute('aria-pressed', String(ctx.wrap)); logEl.classList.toggle('wrap', ctx.wrap); }
      else if (k === 'copy') { copyText(stripAnsi(ctx.full)).then(function () { Ans.toast(T('ans_rn_copied'), 'success'); }); }
      else if (k === 'download') downloadLog(ctx);
      else if (k === 'prev') stepHit(ctx, -1); else if (k === 'next') stepHit(ctx, 1);
    });
    pane.addEventListener('input', function (e) { if (e.target.matches('[data-lg="q"]')) { ctx.search = e.target.value; runSearch(ctx); } });
    pane.addEventListener('keydown', function (e) { if (e.target.matches('[data-lg="q"]') && e.key === 'Enter') { e.preventDefault(); stepHit(ctx, e.shiftKey ? -1 : 1); } });
    if (ctx.follow) logEl.scrollTop = logEl.scrollHeight;
    if (ctx.search) runSearch(ctx);
  }
  function lineEl(l) { const d = document.createElement('div'); d.className = 'ans-ll'; d.innerHTML = l.html || '&#8203;'; l.el = d; return d; }

  function appendText(ctx, text) {
    ctx.full += text;
    ctx.treeDirty = true;
    const data = ctx.partial + text;
    const parts = data.split('\n');
    ctx.partial = parts.pop();
    if (parts.length) ctx.queue.push(parts);
    drainQueue(ctx);
    if (ctx.tab === 'tasks') paintTasks(ctx, ctx.root.querySelector('[data-rv="pane"]'));
    paintTabBadges(ctx);
  }
  // render at most ~1500 lines per frame so a 2 MiB log does not freeze the drawer
  function drainQueue(ctx) {
    if (ctx.queueTimer || ctx.dead) return;
    const step = function () {
      ctx.queueTimer = null;
      if (ctx.dead || !ctx.queue.length) return;
      const batch = ctx.queue[0];
      const take = batch.splice(0, 1500);
      if (!batch.length) ctx.queue.shift();
      const frag = document.createDocumentFragment();
      const fresh = ctx.logEl && ctx.lines.length === 0 ? true : false;
      take.forEach(function (raw) {
        const l = { text: stripAnsi(raw), html: ansiToHtml(raw, ctx.ansi) };
        ctx.lines.push(l);
        if (ctx.logEl) frag.appendChild(lineEl(l));
      });
      if (ctx.logEl) {
        if (fresh) ctx.logEl.innerHTML = '';
        ctx.logEl.appendChild(frag);
        if (ctx.search) runSearch(ctx, true);
        if (ctx.follow) ctx.logEl.scrollTop = ctx.logEl.scrollHeight;
      }
      if (ctx.queue.length) ctx.queueTimer = setTimeout(step, 0);
    };
    ctx.queueTimer = setTimeout(step, 0);
  }

  function runSearch(ctx, keep) {
    const needle = ctx.search.toLowerCase();
    ctx.hits.forEach(function (i) { const l = ctx.lines[i]; if (l && l.el) l.el.classList.remove('hit', 'cur'); });
    ctx.hits = [];
    if (needle) ctx.lines.forEach(function (l, i) { if (l.text.toLowerCase().indexOf(needle) >= 0) { ctx.hits.push(i); if (l.el) l.el.classList.add('hit'); } });
    if (!keep || ctx.hitIdx >= ctx.hits.length) ctx.hitIdx = ctx.hits.length ? 0 : -1;
    paintHit(ctx, !keep);
  }
  function stepHit(ctx, d) {
    if (!ctx.hits.length) return;
    ctx.hitIdx = (ctx.hitIdx + d + ctx.hits.length) % ctx.hits.length;
    paintHit(ctx, true);
  }
  function paintHit(ctx, scroll) {
    const c = ctx.root.querySelector('[data-lg="count"]');
    if (c) c.textContent = ctx.search ? (ctx.hits.length ? T('ans_rn_matches', '{i}/{n}', { i: ctx.hitIdx + 1, n: ctx.hits.length }) : T('ans_rn_no_matches')) : '';
    ctx.lines.forEach(function (l) { if (l.el && l.el.classList.contains('cur')) l.el.classList.remove('cur'); });
    const l = ctx.lines[ctx.hits[ctx.hitIdx]];
    if (l && l.el) { l.el.classList.add('cur'); if (scroll) { ctx.follow = false; const f = ctx.root.querySelector('[data-lg="follow"]'); if (f) f.setAttribute('aria-pressed', 'false'); l.el.scrollIntoView({ block: 'center' }); } }
  }

  function copyText(s) {
    if (navigator.clipboard && navigator.clipboard.writeText) return navigator.clipboard.writeText(s).catch(function () { return legacyCopy(s); });
    return legacyCopy(s);
  }
  function legacyCopy(s) {
    const ta = document.createElement('textarea'); ta.value = s; ta.style.position = 'fixed'; ta.style.opacity = '0'; document.body.appendChild(ta); ta.select();
    try { document.execCommand('copy'); } catch (e) { /* unsupported */ }
    ta.remove(); return Promise.resolve();
  }
  function downloadLog(ctx) {
    const blob = new Blob([stripAnsi(ctx.full)], { type: 'text/plain;charset=utf-8' });
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = ((ctx.run && ctx.run.playbook_name) || 'run').replace(/[^A-Za-z0-9_.-]+/g, '_') + '-run' + ctx.id + '.log';
    document.body.appendChild(a); a.click(); a.remove();
    setTimeout(function () { URL.revokeObjectURL(a.href); }, 1000);
  }

  /* ---- recap ---- */
  function paintRecap(ctx, pane) {
    const r = ctx.run;
    const recap = ctx.recap || {};
    const hosts = Object.keys(recap);
    if (!hosts.length) { pane.innerHTML = Ans.ui.empty({ icon: 'list-checks', title: r && isActive(r) ? T('ans_rn_recap_wait') : T('ans_rn_recap_none') }); return; }
    const bad = function (h) { return (recap[h].failed || 0) + (recap[h].unreachable || 0); };
    hosts.sort(function (a, b) { return bad(b) - bad(a) || a.localeCompare(b); });
    const cols = ['ok', 'changed', 'failed', 'unreachable', 'skipped', 'rescued', 'ignored'];
    const tot = {}; cols.forEach(function (c) { tot[c] = hosts.reduce(function (s, h) { return s + (recap[h][c] || 0); }, 0); });
    const cell = function (v, c) { return '<td class="ans-rn-num' + (v && (c === 'failed' || c === 'unreachable') ? ' bad' : v && c === 'changed' ? ' changed' : v && c === 'ok' ? ' ok' : '') + '">' + (v || 0) + '</td>'; };
    pane.innerHTML = '<div class="ans-rn-recap"><div class="ans-row" style="margin-bottom:8px">' + Ans.fmt.pips(tot) + '</div><div class="ans-table-wrap"><table class="ans-table"><thead><tr><th scope="col">' + esc(T('ans_rn_c_host')) + '</th>' +
      cols.map(function (c) { return '<th scope="col" class="ans-rn-num">' + esc(T('ans_rn_c_' + c)) + '</th>'; }).join('') + '</tr></thead><tbody>' +
      hosts.map(function (h) { return '<tr' + (bad(h) ? ' class="ans-rn-badrow"' : '') + '><td class="ans-mono">' + esc(h) + '</td>' + cols.map(function (c) { return cell(recap[h][c], c); }).join('') + '</tr>'; }).join('') +
      '<tr class="ans-rn-total"><td>' + esc(T('ans_rn_recap_total')) + '</td>' + cols.map(function (c) { return cell(tot[c], c); }).join('') + '</tr></tbody></table></div></div>';
  }

  /* ---- tasks tree ---- */
  function treeFor(ctx) { if (ctx.treeDirty || !ctx.tree) { ctx.tree = parseTaskTree(ctx.full); ctx.treeDirty = false; } return ctx.tree; }
  const FILTERS = { all: null, failed: ['fatal', 'unreachable'], changed: ['changed'], ok: ['ok'], skipped: ['skipping', 'ignored'] };
  function paintTasks(ctx, pane) {
    if (!pane || ctx.tab !== 'tasks') return;
    const plays = treeFor(ctx);
    const total = plays.reduce(function (n, p) { return n + p.tasks.length; }, 0);
    if (!total) { pane.innerHTML = Ans.ui.empty({ icon: 'layers', title: T('ans_rn_tasks_none') }); return; }
    const want = FILTERS[ctx.taskFilter], q = ctx.taskQ.trim().toLowerCase();
    const hostOk = function (t, h) { return (!want || want.indexOf(h.status) >= 0) && (!q || (t.name + ' ' + h.host + ' ' + h.detail).toLowerCase().indexOf(q) >= 0 || t.name.toLowerCase().indexOf(q) >= 0 && true); };
    let html = '', shown = 0;
    plays.forEach(function (p) {
      let ph = '';
      p.tasks.forEach(function (t) {
        const hs = t.hosts.filter(function (h) { return hostOk(t, h); });
        if (!hs.length && (want || q)) return;
        shown++;
        const bad = t.hosts.some(function (h) { return h.status === 'fatal' || h.status === 'unreachable'; });
        const worst = bad ? 'fatal' : t.hosts.some(function (h) { return h.status === 'changed'; }) ? 'changed' : t.hosts.length && t.hosts.every(function (h) { return h.status === 'skipping'; }) ? 'skipping' : 'ok';
        const counts = {}; t.hosts.forEach(function (h) { counts[h.status] = (counts[h.status] || 0) + 1; });
        ph += '<details class="ans-rn-task st-' + worst + '"' + (bad || total <= 40 || want || q ? ' open' : '') + '><summary><span class="ans-rn-td"></span><span class="ans-rn-tn">' + esc(t.name || '(unnamed)') + '</span>' +
          Object.keys(counts).map(function (s) { return '<span class="ans-rn-cnt st-' + s + '">' + counts[s] + ' ' + esc(T('ans_rn_st_' + s, s)) + '</span>'; }).join('') + '</summary>' +
          (bad && t.notes.length ? '<pre class="ans-rn-notes">' + esc(t.notes.join('\n')) + '</pre>' : '') +
          '<ul class="ans-rn-hosts">' + hs.map(function (h) {
            const text = failureText(h);
            const det = text && text.trim() && (h.status === 'fatal' || h.status === 'unreachable' || h.status === 'changed' || h.status === 'ignored' || text.length < 400) ? '<pre class="ans-rn-det">' + esc(text.length > 4000 ? text.slice(0, 4000) + '…' : text) + '</pre>' : '';
            return '<li class="st-' + h.status + '"><span class="ans-rn-hs">' + esc(T('ans_rn_st_' + h.status, h.status)) + '</span><span class="ans-mono">' + esc(h.host) + '</span>' + det + '</li>';
          }).join('') + '</ul></details>';
      });
      if (ph) html += '<details class="ans-rn-play" open><summary>' + icon('layers', 'icon-sm') + ' ' + esc(p.name || 'play') + '</summary>' + ph + '</details>';
    });
    const chips = [['all', 'All'], ['failed', 'Failed'], ['changed', 'Changed'], ['ok', 'OK'], ['skipped', 'Skipped']].map(function (d) {
      return '<button type="button" class="ans-chip" data-tf="' + d[0] + '" aria-pressed="' + (ctx.taskFilter === d[0]) + '">' + esc(T('ans_rn_t_' + d[0], d[1])) + '</button>';
    }).join('');
    const keep = pane.querySelector('[data-tq]');
    const focus = keep && document.activeElement === keep;
    pane.innerHTML = '<div class="ans-rn-tbar"><div class="search-box ans-rn-lsearch"><input type="text" class="form-input" data-tq placeholder="' + esc(T('ans_rn_tasks_search')) + '" aria-label="' + esc(T('ans_rn_tasks_search')) + '" value="' + esc(ctx.taskQ) + '" /></div><span class="ans-row">' + chips + '</span></div>' +
      '<div class="ans-rn-tree">' + (shown ? html : '<div class="ans-muted ans-rn-empty">' + esc(T('ans_rn_tasks_filtered')) + '</div>') + '</div>';
    if (!pane.dataset.bound) {
      pane.dataset.bound = '1';
      pane.addEventListener('click', function (e) { const b = e.target.closest('[data-tf]'); if (b) { ctx.taskFilter = b.getAttribute('data-tf'); paintTasks(ctx, pane); } });
      pane.addEventListener('input', function (e) { if (e.target.matches('[data-tq]')) { ctx.taskQ = e.target.value; paintTasks(ctx, pane); } });
    }
    if (focus) { const i = pane.querySelector('[data-tq]'); i.focus(); i.setSelectionRange(i.value.length, i.value.length); }
  }

  /* ---- diff ---- */
  function paintDiff(ctx, pane) {
    if (!pane || ctx.tab !== 'diff') return;
    const files = parseDiff(ctx.full);
    if (!files.length) { pane.innerHTML = Ans.ui.empty({ icon: 'git-compare', title: T('ans_rn_diff_none') }); return; }
    pane.innerHTML = '<div class="ans-rn-diffs"><div class="ans-hint">' + esc(T('ans_rn_diff_files', '{n} changed files', { n: files.length })) + '</div>' + files.map(function (f) {
      return '<details class="ans-rn-file" open><summary><span class="ans-mono">' + esc(f.file || '(file)') + '</span><span class="ans-rn-add">+' + f.adds + '</span><span class="ans-rn-del">−' + f.dels + '</span>' +
        (f.task ? '<span class="ans-muted ans-rn-ft">' + esc(T('ans_rn_diff_task')) + ': ' + esc(f.task) + '</span>' : '') + '</summary><pre class="ans-rn-diff">' +
        f.rows.map(function (r) { return '<div class="ans-dl ans-dl-' + r.t + '">' + escText(r.x) + '</div>'; }).join('') + '</pre></details>';
    }).join('') + '</div>';
  }

  /* ===================== exports ===================== */
  Ans.runs = { openViewer: openViewer, mountHistory: mountHistory, util: util, reload: function () { return loadRuns(); } };
  Ans.registerTab({ id: 'runs', icon: 'history', labelKey: 'ans_tab_runs', order: 20, render: renderTab, onShow: function () { if (tabState.root && tabState.sub === 'history') loadRuns(); } });
})();
