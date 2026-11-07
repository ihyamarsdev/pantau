/* Schedules: per-playbook list with inline editor (drawer) and the all-schedules table (Runs tab). */
(function () {
  'use strict';
  const Ans = window.Ans;
  const esc = Ans.esc, T = Ans.t, icon = Ans.icon;

  Ans.addI18n({
    en: {
      ans_sc_add: 'Add schedule', ans_sc_new_title: 'New schedule', ans_sc_edit_title: 'Edit schedule', ans_sc_empty_title: 'No schedules', ans_sc_empty_desc: 'Run this playbook automatically on a cron schedule.',
      ans_sc_tz: 'Times use the Pantau server time zone: {tz}.', ans_sc_name: 'Name', ans_sc_name_ph: 'optional, e.g. nightly', ans_sc_cron: 'Cron expression', ans_sc_cron_hint: 'minute hour day-of-month month day-of-week',
      ans_sc_presets: 'Presets', ans_sc_p_5m: 'Every 5 min', ans_sc_p_hourly: 'Hourly', ans_sc_p_daily: 'Daily 03:00', ans_sc_p_sunday: 'Sunday 03:00', ans_sc_p_monthly: 'Monthly (1st)',
      ans_sc_next_runs: 'Next runs', ans_sc_cron_bad: 'Invalid cron expression', ans_sc_cron_never: 'This expression never matches a date',
      ans_sc_mode: 'Mode', ans_sc_mode_run: 'Run', ans_sc_mode_check: 'Check', ans_sc_mode_diff: 'Diff', ans_sc_mode_syntax: 'Syntax', ans_sc_targets: 'Targets',
      ans_sc_group: 'Host group (optional)', ans_sc_group_none: 'None', ans_sc_group_hint: 'Every host of the group is added at run time, in addition to the hosts selected below.',
      ans_sc_advanced: 'Advanced', ans_sc_verbosity: 'Verbosity', ans_sc_limit: 'Limit', ans_sc_tags: 'Tags', ans_sc_skip: 'Skip tags', ans_sc_extra: 'Extra vars (JSON object)', ans_sc_become: 'Become (sudo without password)',
      ans_sc_enabled: 'Enabled', ans_sc_save: 'Save schedule', ans_sc_saving: 'Saving…', ans_sc_cancel: 'Cancel', ans_sc_saved: 'Schedule saved', ans_sc_failed: 'Could not save the schedule',
      ans_sc_err_cron: 'Enter a cron expression.', ans_sc_err_targets: 'Select at least one host or a host group (not needed for Syntax mode).', ans_sc_err_extra: 'Extra vars must be a valid JSON object.',
      ans_sc_next: 'Next run', ans_sc_last: 'Last run', ans_sc_never: 'never', ans_sc_off: 'Disabled', ans_sc_edit: 'Edit', ans_sc_delete: 'Delete', ans_sc_toggle: 'Enable or disable the schedule',
      ans_sc_delete_title: 'Delete schedule?', ans_sc_delete_msg: 'The schedule "{name}" will no longer run.', ans_sc_deleted: 'Schedule deleted', ans_sc_delete_failed: 'Could not delete the schedule',
      ans_sc_toggle_failed: 'Could not update the schedule', ans_sc_load_failed: 'Could not load schedules', ans_sc_unnamed: 'Schedule {id}',
      ans_sc_targets_n: '{n} hosts', ans_sc_targets_one: '1 host', ans_sc_targets_group: 'group {g}', ans_sc_targets_none: 'no hosts', ans_sc_last_error: 'Last error',
      ans_sc_col_playbook: 'Playbook', ans_sc_col_schedule: 'Schedule', ans_sc_col_cron: 'Cron', ans_sc_col_targets: 'Targets', ans_sc_col_mode: 'Mode', ans_sc_col_next: 'Next run', ans_sc_col_last: 'Last run', ans_sc_col_enabled: 'On',
      ans_sc_search: 'Search schedules…', ans_sc_show_disabled: 'Show disabled', ans_sc_all_empty: 'No schedules yet. Open a playbook and use its Schedule tab to add one.', ans_sc_no_match: 'No schedules match', ans_sc_manage: 'Manage',
      ans_sc_d_5m: 'every 5 minutes', ans_sc_d_hourly: 'every hour, on the hour', ans_sc_d_daily: 'every day at 03:00', ans_sc_d_sunday: 'every Sunday at 03:00', ans_sc_d_monthly: 'on the 1st of every month at 03:00',
    },
    id: {
      ans_sc_add: 'Tambah jadwal', ans_sc_new_title: 'Jadwal baru', ans_sc_edit_title: 'Edit jadwal', ans_sc_empty_title: 'Belum ada jadwal', ans_sc_empty_desc: 'Jalankan playbook ini otomatis dengan jadwal cron.',
      ans_sc_tz: 'Waktu memakai zona waktu server Pantau: {tz}.', ans_sc_name: 'Nama', ans_sc_name_ph: 'opsional, mis. nightly', ans_sc_cron: 'Ekspresi cron', ans_sc_cron_hint: 'menit jam tanggal bulan hari-minggu',
      ans_sc_presets: 'Preset', ans_sc_p_5m: 'Tiap 5 menit', ans_sc_p_hourly: 'Tiap jam', ans_sc_p_daily: 'Harian 03:00', ans_sc_p_sunday: 'Minggu 03:00', ans_sc_p_monthly: 'Bulanan (tgl 1)',
      ans_sc_next_runs: 'Run berikutnya', ans_sc_cron_bad: 'Ekspresi cron tidak valid', ans_sc_cron_never: 'Ekspresi ini tidak pernah cocok dengan tanggal',
      ans_sc_mode: 'Mode', ans_sc_mode_run: 'Run', ans_sc_mode_check: 'Check', ans_sc_mode_diff: 'Diff', ans_sc_mode_syntax: 'Syntax', ans_sc_targets: 'Target',
      ans_sc_group: 'Grup host (opsional)', ans_sc_group_none: 'Tidak ada', ans_sc_group_hint: 'Semua host dalam grup ditambahkan saat run, selain host yang dipilih di bawah.',
      ans_sc_advanced: 'Lanjutan', ans_sc_verbosity: 'Verbositas', ans_sc_limit: 'Limit', ans_sc_tags: 'Tag', ans_sc_skip: 'Lewati tag', ans_sc_extra: 'Extra vars (objek JSON)', ans_sc_become: 'Become (sudo tanpa password)',
      ans_sc_enabled: 'Aktif', ans_sc_save: 'Simpan jadwal', ans_sc_saving: 'Menyimpan…', ans_sc_cancel: 'Batal', ans_sc_saved: 'Jadwal disimpan', ans_sc_failed: 'Gagal menyimpan jadwal',
      ans_sc_err_cron: 'Isi ekspresi cron.', ans_sc_err_targets: 'Pilih setidaknya satu host atau grup host (tidak perlu untuk mode Syntax).', ans_sc_err_extra: 'Extra vars harus berupa objek JSON yang valid.',
      ans_sc_next: 'Run berikutnya', ans_sc_last: 'Run terakhir', ans_sc_never: 'belum pernah', ans_sc_off: 'Nonaktif', ans_sc_edit: 'Edit', ans_sc_delete: 'Hapus', ans_sc_toggle: 'Aktifkan atau nonaktifkan jadwal',
      ans_sc_delete_title: 'Hapus jadwal?', ans_sc_delete_msg: 'Jadwal "{name}" tidak akan berjalan lagi.', ans_sc_deleted: 'Jadwal dihapus', ans_sc_delete_failed: 'Gagal menghapus jadwal',
      ans_sc_toggle_failed: 'Gagal memperbarui jadwal', ans_sc_load_failed: 'Gagal memuat jadwal', ans_sc_unnamed: 'Jadwal {id}',
      ans_sc_targets_n: '{n} host', ans_sc_targets_one: '1 host', ans_sc_targets_group: 'grup {g}', ans_sc_targets_none: 'tanpa host', ans_sc_last_error: 'Galat terakhir',
      ans_sc_col_playbook: 'Playbook', ans_sc_col_schedule: 'Jadwal', ans_sc_col_cron: 'Cron', ans_sc_col_targets: 'Target', ans_sc_col_mode: 'Mode', ans_sc_col_next: 'Run berikutnya', ans_sc_col_last: 'Run terakhir', ans_sc_col_enabled: 'Aktif',
      ans_sc_search: 'Cari jadwal…', ans_sc_show_disabled: 'Tampilkan yang nonaktif', ans_sc_all_empty: 'Belum ada jadwal. Buka playbook dan pakai tab Jadwal untuk menambahkan.', ans_sc_no_match: 'Tidak ada jadwal yang cocok', ans_sc_manage: 'Kelola',
      ans_sc_d_5m: 'setiap 5 menit', ans_sc_d_hourly: 'setiap jam, tepat di awal jam', ans_sc_d_daily: 'setiap hari pukul 03:00', ans_sc_d_sunday: 'setiap Minggu pukul 03:00', ans_sc_d_monthly: 'setiap tanggal 1 pukul 03:00',
    },
  });

  const PRESETS = [['*/5 * * * *', 'ans_sc_p_5m', 'Every 5 min', 'ans_sc_d_5m'], ['0 * * * *', 'ans_sc_p_hourly', 'Hourly', 'ans_sc_d_hourly'], ['0 3 * * *', 'ans_sc_p_daily', 'Daily 03:00', 'ans_sc_d_daily'], ['0 3 * * 0', 'ans_sc_p_sunday', 'Sunday 03:00', 'ans_sc_d_sunday'], ['0 3 1 * *', 'ans_sc_p_monthly', 'Monthly (1st)', 'ans_sc_d_monthly']];
  const MODES = ['run', 'check', 'diff', 'syntax'];
  const LIMIT_RE = /^[A-Za-z0-9_.,:!&*\[\]-]{1,256}$/, TAGS_RE = /^[A-Za-z0-9_.,:-]{1,256}$/;

  /* ---------- cron helpers (client-side preview only; the server stays authoritative) ---------- */
  function parseField(src, min, max) {
    const set = new Set();
    for (const part of src.split(',')) {
      const m = /^(\*|\d+(?:-\d+)?)(?:\/(\d+))?$/.exec(part);
      if (!m) return null;
      const step = m[2] ? Number(m[2]) : 1;
      if (!step) return null;
      let lo, hi;
      if (m[1] === '*') { lo = min; hi = max; }
      else if (m[1].indexOf('-') > 0) { const r = m[1].split('-').map(Number); lo = r[0]; hi = r[1]; }
      else { lo = Number(m[1]); hi = m[2] ? max : lo; }
      if (lo < min || hi > max || lo > hi) return null;
      for (let v = lo; v <= hi; v += step) set.add(v);
    }
    return set;
  }
  function parseCronExpr(expr) {
    const f = (expr || '').trim().split(/\s+/);
    if (f.length !== 5) return null;
    const lim = [[0, 59], [0, 23], [1, 31], [1, 12], [0, 7]];
    const sets = f.map(function (s, i) { return parseField(s, lim[i][0], lim[i][1]); });
    if (sets.some(function (s) { return !s; })) return null;
    if (sets[4].has(7)) sets[4].add(0);
    return { sets: sets, domStar: f[2] === '*', dowStar: f[4] === '*' };
  }
  function serverOffsetMinutes() {
    const tz = (Ans.state.status && Ans.state.status.timezone) || '';
    const m = /([+-])(\d{2}):?(\d{2})/.exec(tz);
    return m ? (m[1] === '-' ? -1 : 1) * (Number(m[2]) * 60 + Number(m[3])) : -new Date().getTimezoneOffset();
  }
  // next `count` fire times as wall-clock strings in server time
  function cronNext(expr, count) {
    const c = parseCronExpr(expr);
    if (!c) return null;
    const off = serverOffsetMinutes() * 60000;
    let t = Math.floor((Date.now() + off) / 60000) * 60000 + 60000; // wall clock as if UTC
    const out = [];
    for (let i = 0; i < 366 * 24 * 60 && out.length < count; i++, t += 60000) {
      const d = new Date(t);
      if (!c.sets[0].has(d.getUTCMinutes()) || !c.sets[1].has(d.getUTCHours()) || !c.sets[3].has(d.getUTCMonth() + 1)) continue;
      const domOk = c.sets[2].has(d.getUTCDate()), dowOk = c.sets[4].has(d.getUTCDay());
      if (c.domStar || c.dowStar ? !(domOk && dowOk) : !(domOk || dowOk)) continue;
      out.push(d.toISOString().slice(0, 16).replace('T', ' '));
    }
    return out;
  }
  function describeCron(expr) {
    const p = PRESETS.find(function (x) { return x[0] === (expr || '').trim(); });
    return p ? T(p[3]) : '';
  }

  /* ---------- shared view helpers ---------- */
  const hostName = function (id) { const h = (Ans.state.hosts || []).find(function (x) { return x.id === id; }); return h ? h.name : '#' + id; };
  function targetsText(s) {
    const parts = [];
    if (s.group) parts.push(T('ans_sc_targets_group', 'group {g}', { g: s.group }));
    if (s.host_ids && s.host_ids.length) parts.push(s.host_ids.length === 1 ? hostName(s.host_ids[0]) : T('ans_sc_targets_n', '{n} hosts', { n: s.host_ids.length }));
    return parts.length ? parts.join(' + ') : T('ans_sc_targets_none');
  }
  function payloadOf(s, over) {
    return Object.assign({ playbook_id: s.playbook_id, name: s.name || '', cron: s.cron, enabled: !!s.enabled, mode: s.mode || 'run', host_ids: s.host_ids || [], group: s.group || '', extra_vars: s.extra_vars || '', tags: s.tags || '', skip_tags: s.skip_tags || '', limit: s.limit || '', verbosity: s.verbosity || 0, become: !!s.become }, over || {});
  }
  const refreshElsewhere = function () { Ans.load.schedules().catch(function () { /* optional */ }); Ans.load.playbooks().catch(function () { /* optional */ }); };
  const switchHtml = function (attr, on, label) { return '<label class="switch" title="' + esc(label) + '"><input type="checkbox" ' + attr + (on ? ' checked' : '') + ' aria-label="' + esc(label) + '" /><span class="switch-track"><span class="switch-thumb"></span></span></label>'; };
  const lastRunHtml = function (s) {
    if (!s.last_run_at) return '<span class="ans-muted">' + esc(T('ans_sc_never')) + '</span>';
    return '<span title="' + esc(Ans.fmt.dt(s.last_run_at)) + '">' + esc(Ans.fmt.ago(s.last_run_at)) + '</span>' + (s.last_run_id && Ans.runs && Ans.runs.openViewer ? ' <button type="button" class="btn btn-sm btn-ghost" data-sc-run="' + s.last_run_id + '" data-pb="' + s.playbook_id + '">#' + s.last_run_id + '</button>' : '');
  };
  const nextRunHtml = function (s) {
    if (!s.enabled) return '<span class="badge badge-unknown">' + esc(T('ans_sc_off')) + '</span>';
    return s.next_run_at ? '<span title="' + esc(Ans.fmt.dt(s.next_run_at)) + '">' + esc(Ans.fmt.ago(s.next_run_at)) + '</span>' : '<span class="ans-muted">-</span>';
  };

  async function toggle(s, on, done) {
    try { await Ans.api.put('/api/ansible/schedules/' + s.id, payloadOf(s, { enabled: on })); refreshElsewhere(); }
    catch (e) { Ans.toast(T('ans_sc_toggle_failed') + ': ' + e.message, 'error'); }
    if (done) done();
  }
  async function remove(s, done) {
    const ok = await Ans.confirm({ title: T('ans_sc_delete_title'), message: T('ans_sc_delete_msg', 'The schedule "{name}" will no longer run.', { name: s.name || T('ans_sc_unnamed', 'Schedule {id}', { id: s.id }) }), confirmText: T('ans_sc_delete'), danger: true });
    if (!ok) return;
    try { await Ans.api.del('/api/ansible/schedules/' + s.id); Ans.toast(T('ans_sc_deleted'), 'success'); refreshElsewhere(); if (done) done(); }
    catch (e) { Ans.toast(T('ans_sc_delete_failed') + ': ' + e.message, 'error'); }
  }

  /* ---------- per-playbook mount ---------- */
  function mount(container, o) {
    const pid = Number(o.playbookId);
    let list = null, editing = null, dead = false, picker = null, error = '';
    const q = function (s) { return container.querySelector(s); };
    container.classList.add('ans-sc');

    async function load() {
      try { list = await Ans.load.schedules(pid); error = ''; } catch (e) { error = T('ans_sc_load_failed') + ': ' + e.message; list = list || []; }
      if (!dead && !editing) paintList();
    }
    function paintList() {
      if (picker) { picker.destroy(); picker = null; }
      editing = null;
      if (list === null) { container.innerHTML = Ans.ui.skeleton(3); return; }
      const tz = (Ans.state.status && Ans.state.status.timezone) || '';
      container.innerHTML = '<div class="ans-row"><span class="ans-hint">' + esc(T('ans_sc_tz', 'Times use the Pantau server time zone: {tz}.', { tz: tz || 'local' })) + '</span><span class="ans-spacer"></span><button type="button" class="btn btn-sm btn-primary" data-sc="add">' + icon('plus', 'icon-sm') + ' ' + esc(T('ans_sc_add')) + '</button></div>' +
        (error ? Ans.ui.error(error, 'data-sc="retry"') : '') +
        (list.length ? '<div class="ans-sc-list">' + list.map(card).join('') + '</div>' : Ans.ui.empty({ icon: 'calendar', title: T('ans_sc_empty_title'), desc: T('ans_sc_empty_desc') }));
    }
    function card(s) {
      return '<div class="ans-card ans-sc-card' + (s.enabled ? '' : ' off') + '" data-id="' + s.id + '"><div class="ans-row"><strong>' + esc(s.name || T('ans_sc_unnamed', 'Schedule {id}', { id: s.id })) + '</strong><span class="badge badge-unknown">' + esc(s.mode) + '</span><span class="ans-spacer"></span>' +
        switchHtml('data-sc-on="' + s.id + '"', s.enabled, T('ans_sc_toggle')) +
        '<button type="button" class="btn btn-sm btn-ghost ans-btn-icon" data-sc="edit" title="' + esc(T('ans_sc_edit')) + '" aria-label="' + esc(T('ans_sc_edit')) + '">' + icon('pencil', 'icon-sm') + '</button>' +
        '<button type="button" class="btn btn-sm btn-ghost ans-btn-icon" data-sc="del" title="' + esc(T('ans_sc_delete')) + '" aria-label="' + esc(T('ans_sc_delete')) + '">' + icon('trash-2', 'icon-sm') + '</button></div>' +
        '<div class="ans-sc-meta"><code class="ans-mono">' + esc(s.cron) + '</code><span class="ans-muted">' + esc(describeCron(s.cron)) + '</span></div>' +
        '<div class="ans-sc-meta"><span>' + icon('server', 'icon-sm') + ' ' + esc(targetsText(s)) + '</span><span class="ans-muted">' + esc(T('ans_sc_next')) + ':</span> ' + nextRunHtml(s) + '<span class="ans-muted">' + esc(T('ans_sc_last')) + ':</span> ' + lastRunHtml(s) + '</div>' +
        (s.last_error ? '<div class="ans-err ans-sc-lerr" title="' + esc(s.last_error) + '">' + esc(T('ans_sc_last_error')) + ': ' + esc(s.last_error) + '</div>' : '') + '</div>';
    }

    /* ----- inline editor ----- */
    function openEditor(s) {
      const isNew = !s;
      const E = Object.assign({ name: '', cron: '0 3 * * *', enabled: true, mode: 'run', host_ids: [], group: '', extra_vars: '', tags: '', skip_tags: '', limit: '', verbosity: 0, become: false }, s || {});
      E.host_ids = (E.host_ids || []).slice();
      editing = { id: s ? s.id : null, E: E };
      const groups = Array.from(new Set((Ans.state.hosts || []).map(function (h) { return (h.group_name || '').trim(); }).filter(Boolean))).sort();
      container.innerHTML = '<form class="ans-sc-form" novalidate><div class="ans-card-title">' + icon('calendar', 'icon-sm') + esc(isNew ? T('ans_sc_new_title') : T('ans_sc_edit_title')) + '</div>' +
        '<div class="ans-grid-2"><div class="ans-field"><label for="ansibleSchedName">' + esc(T('ans_sc_name')) + '</label><input id="ansibleSchedName" class="form-input" data-e="name" maxlength="64" placeholder="' + esc(T('ans_sc_name_ph')) + '" value="' + esc(E.name) + '" /></div>' +
        '<div class="ans-field"><label for="ansibleSchedCron">' + esc(T('ans_sc_cron')) + '</label><input id="ansibleSchedCron" class="form-input ans-mono" data-e="cron" value="' + esc(E.cron) + '" spellcheck="false" autocomplete="off" /><div class="hint">' + esc(T('ans_sc_cron_hint')) + '</div></div></div>' +
        '<div class="ans-row" role="group" aria-label="' + esc(T('ans_sc_presets')) + '">' + PRESETS.map(function (p) { return '<button type="button" class="ans-chip" data-preset="' + esc(p[0]) + '">' + esc(T(p[1], p[2])) + '</button>'; }).join('') + '</div>' +
        '<div class="ans-sc-prev" data-e="prev" id="ansibleSchedCronHint"></div>' +
        '<div class="ans-field"><label>' + esc(T('ans_sc_mode')) + '</label><div class="ans-seg" role="group">' + MODES.map(function (m) { return '<button type="button" data-mode="' + m + '" aria-pressed="' + (E.mode === m) + '">' + esc(T('ans_sc_mode_' + m)) + '</button>'; }).join('') + '</div></div>' +
        '<div class="ans-field"><label>' + esc(T('ans_sc_targets')) + '</label><div data-e="picker"></div></div>' +
        '<div class="ans-field"><label for="ansibleSchedGroup">' + esc(T('ans_sc_group')) + '</label><select id="ansibleSchedGroup" class="form-input" data-e="group"><option value="">' + esc(T('ans_sc_group_none')) + '</option>' + groups.map(function (g) { return '<option value="' + esc(g) + '"' + (E.group === g ? ' selected' : '') + '>' + esc(g) + '</option>'; }).join('') + '</select><div class="hint">' + esc(T('ans_sc_group_hint')) + '</div></div>' +
        '<details class="ans-details"' + (E.tags || E.skip_tags || E.limit || E.extra_vars || E.verbosity || E.become ? ' open' : '') + '><summary>' + esc(T('ans_sc_advanced')) + '</summary><div class="ans-grid-2" style="padding-top:6px">' +
        '<div class="ans-field"><label for="ansibleSchedVerbosity">' + esc(T('ans_sc_verbosity')) + '</label><select id="ansibleSchedVerbosity" class="form-input" data-e="verbosity">' + [0, 1, 2, 3, 4].map(function (n) { return '<option value="' + n + '"' + (E.verbosity === n ? ' selected' : '') + '>' + (n ? '-' + 'v'.repeat(n) : '0') + '</option>'; }).join('') + '</select></div>' +
        '<div class="ans-field"><label for="ansibleSchedLimit">' + esc(T('ans_sc_limit')) + '</label><input id="ansibleSchedLimit" class="form-input ans-mono" data-e="limit" value="' + esc(E.limit) + '" /><div class="err" data-err="limit"></div></div>' +
        '<div class="ans-field"><label for="ansibleSchedTags">' + esc(T('ans_sc_tags')) + '</label><input id="ansibleSchedTags" class="form-input ans-mono" data-e="tags" value="' + esc(E.tags) + '" /><div class="err" data-err="tags"></div></div>' +
        '<div class="ans-field"><label for="ansibleSchedSkip">' + esc(T('ans_sc_skip')) + '</label><input id="ansibleSchedSkip" class="form-input ans-mono" data-e="skip_tags" value="' + esc(E.skip_tags) + '" /><div class="err" data-err="skip_tags"></div></div>' +
        '<div class="ans-field ans-ln-wide"><label for="ansibleSchedExtra">' + esc(T('ans_sc_extra')) + '</label><textarea id="ansibleSchedExtra" class="form-input ans-mono" rows="3" data-e="extra_vars">' + esc(E.extra_vars) + '</textarea><div class="err" data-err="extra_vars"></div></div>' +
        '<div class="ans-field ans-ln-wide"><label class="ans-row" style="cursor:pointer"><span class="switch"><input type="checkbox" data-e="become"' + (E.become ? ' checked' : '') + ' /><span class="switch-track"><span class="switch-thumb"></span></span></span><span>' + esc(T('ans_sc_become')) + '</span></label></div></div></details>' +
        '<label class="ans-row" style="cursor:pointer"><span class="switch"><input type="checkbox" data-e="enabled"' + (E.enabled ? ' checked' : '') + ' /><span class="switch-track"><span class="switch-thumb"></span></span></span><span>' + esc(T('ans_sc_enabled')) + '</span></label>' +
        '<div class="ans-error" role="alert" data-e="error" hidden></div>' +
        '<div class="ans-row" style="justify-content:flex-end"><button type="button" class="btn btn-sm" data-sc="cancel">' + esc(T('ans_sc_cancel')) + '</button><button type="submit" class="btn btn-sm btn-primary" data-sc="save">' + esc(T('ans_sc_save')) + '</button></div></form>';
      picker = Ans.hostPicker.mount(q('[data-e="picker"]'), { ids: E.host_ids, max: 200, onChange: function (ids) { E.host_ids = ids; } });
      paintPreview();
      const first = q('#ansibleSchedCron'); if (first) first.focus();
    }
    function paintPreview() {
      const el = q('[data-e="prev"]'); if (!el || !editing) return;
      const cron = editing.E.cron.trim();
      if (!cron) { el.innerHTML = ''; return; }
      const runs = cronNext(cron, 3);
      if (runs === null) { el.innerHTML = '<span class="ans-err">' + esc(T('ans_sc_cron_bad')) + '</span>'; return; }
      if (!runs.length) { el.innerHTML = '<span class="ans-err">' + esc(T('ans_sc_cron_never')) + '</span>'; return; }
      const d = describeCron(cron);
      el.innerHTML = (d ? '<span>' + esc(d) + '</span> · ' : '') + '<span class="ans-muted">' + esc(T('ans_sc_next_runs')) + ':</span> ' + runs.map(function (r) { return '<code class="ans-mono">' + esc(r) + '</code>'; }).join(' ');
    }
    async function saveEditor() {
      const E = editing.E, err = q('[data-e="error"]');
      const show = function (m) { err.textContent = m; err.hidden = !m; };
      container.querySelectorAll('[data-err]').forEach(function (x) { x.textContent = ''; });
      show('');
      const bad = function (k, m) { const x = q('[data-err="' + k + '"]'); if (x) x.textContent = m; return true; };
      let fail = false;
      if (!E.cron.trim()) { show(T('ans_sc_err_cron')); return; }
      if (E.limit && !LIMIT_RE.test(E.limit)) fail = bad('limit', T('ans_sc_failed')) || fail;
      if (E.tags && !TAGS_RE.test(E.tags)) fail = bad('tags', T('ans_sc_failed')) || fail;
      if (E.skip_tags && !TAGS_RE.test(E.skip_tags)) fail = bad('skip_tags', T('ans_sc_failed')) || fail;
      if (E.extra_vars.trim()) { try { const x = JSON.parse(E.extra_vars); if (!x || typeof x !== 'object' || Array.isArray(x)) throw new Error('x'); } catch (e) { fail = bad('extra_vars', T('ans_sc_err_extra')) || fail; } }
      if (E.mode !== 'syntax' && !E.host_ids.length && !E.group) { show(T('ans_sc_err_targets')); fail = true; }
      if (fail) { const d = container.querySelector('details'); if (d) d.open = true; return; }
      const btn = q('[data-sc="save"]'); btn.disabled = true; btn.textContent = T('ans_sc_saving');
      try {
        const body = payloadOf(Object.assign({}, E, { playbook_id: pid }));
        if (editing.id) await Ans.api.put('/api/ansible/schedules/' + editing.id, body); else await Ans.api.post('/api/ansible/schedules', body);
        Ans.toast(T('ans_sc_saved'), 'success'); refreshElsewhere();
        editing = null; await load(); paintList();
      } catch (e) { show(T('ans_sc_failed') + ': ' + e.message); btn.disabled = false; btn.textContent = T('ans_sc_save'); }
    }

    const onClick = function (e) {
      const b = e.target.closest('[data-sc]');
      if (b) {
        const a = b.getAttribute('data-sc'), card = b.closest('[data-id]'), s = card && (list || []).find(function (x) { return x.id === Number(card.getAttribute('data-id')); });
        if (a === 'add') openEditor(null); else if (a === 'edit' && s) openEditor(s); else if (a === 'del' && s) remove(s, function () { load(); });
        else if (a === 'cancel') { editing = null; paintList(); } else if (a === 'retry') load();
        return;
      }
      const run = e.target.closest('[data-sc-run]'); if (run) { Ans.runs.openViewer(Number(run.getAttribute('data-sc-run')), { playbookId: Number(run.getAttribute('data-pb')) }); return; }
      const p = e.target.closest('[data-preset]'); if (p && editing) { editing.E.cron = p.getAttribute('data-preset'); q('#ansibleSchedCron').value = editing.E.cron; paintPreview(); return; }
      const m = e.target.closest('[data-mode]'); if (m && editing) { editing.E.mode = m.getAttribute('data-mode'); container.querySelectorAll('[data-mode]').forEach(function (x) { x.setAttribute('aria-pressed', String(x === m)); }); }
    };
    const onChange = function (e) {
      const t = e.target;
      const on = t.getAttribute && t.getAttribute('data-sc-on');
      if (on) { const s = list.find(function (x) { return x.id === Number(on); }); if (s) toggle(s, t.checked, load); return; }
      const k = t.getAttribute && t.getAttribute('data-e');
      if (k && editing) { editing.E[k] = t.type === 'checkbox' ? t.checked : (k === 'verbosity' ? Number(t.value) : t.value); if (k === 'cron') paintPreview(); }
    };
    const onSubmit = function (e) { e.preventDefault(); if (editing) saveEditor(); };
    container.addEventListener('click', onClick); container.addEventListener('change', onChange); container.addEventListener('input', onChange); container.addEventListener('submit', onSubmit);
    paintList(); load();
    return {
      destroy: function () { dead = true; if (picker) picker.destroy(); container.removeEventListener('click', onClick); container.removeEventListener('change', onChange); container.removeEventListener('input', onChange); container.removeEventListener('submit', onSubmit); container.classList.remove('ans-sc'); container.innerHTML = ''; },
      isDirty: function () { return false; },
    };
  }

  /* ---------- all schedules (Runs tab > Scheduled) ---------- */
  function mountAll(container) {
    let list = null, q = '', showOff = Ans.store.get('sc.showOff', true), dead = false, error = '';
    container.classList.add('ans-sc');
    async function load() {
      try { list = await Ans.load.schedules(); error = ''; } catch (e) { error = T('ans_sc_load_failed') + ': ' + e.message; list = list || []; }
      if (!dead) paint();
    }
    function paint() {
      const body = container.querySelector('[data-all="body"]'); if (!body) return;
      if (list === null) { body.innerHTML = Ans.ui.skeleton(4); return; }
      if (error && !list.length) { body.innerHTML = Ans.ui.error(error, 'data-all="retry"'); return; }
      const needle = q.trim().toLowerCase();
      const rows = list.filter(function (s) { return (showOff || s.enabled) && (!needle || (s.playbook_name + ' ' + s.name + ' ' + s.cron).toLowerCase().indexOf(needle) >= 0); });
      if (!list.length) { body.innerHTML = Ans.ui.empty({ icon: 'calendar', title: T('ans_sc_empty_title'), desc: T('ans_sc_all_empty') }); return; }
      if (!rows.length) { body.innerHTML = Ans.ui.empty({ icon: 'search', title: T('ans_sc_no_match') }); return; }
      body.innerHTML = '<div class="ans-table-wrap"><table class="ans-table"><thead><tr><th scope="col">' + esc(T('ans_sc_col_playbook')) + '</th><th scope="col">' + esc(T('ans_sc_col_cron')) + '</th><th scope="col" class="ans-hide-md">' + esc(T('ans_sc_col_targets')) + '</th><th scope="col" class="ans-hide-sm">' + esc(T('ans_sc_col_mode')) + '</th><th scope="col">' + esc(T('ans_sc_col_next')) + '</th><th scope="col" class="ans-hide-md">' + esc(T('ans_sc_col_last')) + '</th><th scope="col">' + esc(T('ans_sc_col_enabled')) + '</th><th scope="col" class="ans-actions"></th></tr></thead><tbody>' +
        rows.map(function (s) {
          return '<tr data-id="' + s.id + '" data-pb="' + s.playbook_id + '"><td><div class="ans-name"><button type="button" class="ans-sc-link" data-all="open">' + esc(s.playbook_name) + '</button></div>' + (s.name ? '<div class="ans-sub">' + esc(s.name) + '</div>' : '') + '</td>' +
            '<td><code class="ans-mono">' + esc(s.cron) + '</code><div class="ans-sub">' + esc(describeCron(s.cron)) + '</div></td><td class="ans-hide-md">' + esc(targetsText(s)) + '</td><td class="ans-hide-sm"><span class="badge badge-unknown">' + esc(s.mode) + '</span></td>' +
            '<td>' + nextRunHtml(s) + '</td><td class="ans-hide-md">' + lastRunHtml(s) + '</td><td>' + switchHtml('data-all-on="' + s.id + '"', s.enabled, T('ans_sc_toggle')) + '</td>' +
            '<td class="ans-actions"><button type="button" class="btn btn-sm btn-ghost ans-btn-icon" data-all="del" title="' + esc(T('ans_sc_delete')) + '" aria-label="' + esc(T('ans_sc_delete')) + '">' + icon('trash-2', 'icon-sm') + '</button></td></tr>';
        }).join('') + '</tbody></table></div>';
    }
    container.innerHTML = '<div class="ans-toolbar"><div class="search-box"><input type="text" class="form-input" data-all="q" placeholder="' + esc(T('ans_sc_search')) + '" aria-label="' + esc(T('ans_sc_search')) + '" /></div><label class="ans-row" style="cursor:pointer;font-size:var(--fs-xs)"><span class="switch"><input type="checkbox" data-all="off"' + (showOff ? ' checked' : '') + ' /><span class="switch-track"><span class="switch-thumb"></span></span></span><span>' + esc(T('ans_sc_show_disabled')) + '</span></label></div><div data-all="body"></div>';
    const onClick = function (e) {
      const b = e.target.closest('[data-all]'); if (!b) { const r = e.target.closest('[data-sc-run]'); if (r) Ans.runs.openViewer(Number(r.getAttribute('data-sc-run')), { playbookId: Number(r.getAttribute('data-pb')) }); return; }
      const a = b.getAttribute('data-all'), tr = b.closest('tr'), s = tr && (list || []).find(function (x) { return x.id === Number(tr.getAttribute('data-id')); });
      if (a === 'open' && s && Ans.playbooks) Ans.playbooks.open(s.playbook_id, 'schedule'); else if (a === 'del' && s) remove(s, load); else if (a === 'retry') { error = ''; load(); }
    };
    const onChange = function (e) {
      const t = e.target;
      if (t.matches('[data-all="off"]')) { showOff = t.checked; Ans.store.set('sc.showOff', showOff); paint(); return; }
      if (t.matches('[data-all="q"]')) { q = t.value; paint(); return; }
      const on = t.getAttribute('data-all-on'); if (on) { const s = list.find(function (x) { return x.id === Number(on); }); if (s) toggle(s, t.checked, load); }
    };
    container.addEventListener('click', onClick); container.addEventListener('change', onChange); container.addEventListener('input', onChange);
    paint(); load();
    const stop = Ans.every(function () { if (!dead && !Ans.drawer.current()) load(); }, 15000);
    return { destroy: function () { dead = true; stop(); container.removeEventListener('click', onClick); container.removeEventListener('change', onChange); container.removeEventListener('input', onChange); container.classList.remove('ans-sc'); container.innerHTML = ''; } };
  }

  Ans.schedules = { mount: mount, mountAll: mountAll, util: { cronNext: cronNext, parseCronExpr: parseCronExpr } };
})();
