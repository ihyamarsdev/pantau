/* Launch panel: targets, mode, survey form, advanced options, live summary, launch. Lives in the playbook drawer Overview. */
(function () {
  'use strict';
  const Ans = window.Ans;
  const esc = Ans.esc, T = Ans.t, icon = Ans.icon;

  Ans.addI18n({
    en: {
      ans_ln_title: 'Run now', ans_ln_targets: 'Target hosts', ans_ln_mode: 'Mode',
      ans_ln_mode_run: 'Run', ans_ln_mode_check: 'Check', ans_ln_mode_diff: 'Diff', ans_ln_mode_syntax: 'Syntax',
      ans_ln_hint_run: 'Execute the playbook and apply changes.', ans_ln_hint_check: 'Dry run (--check): report what would change without changing anything.',
      ans_ln_hint_diff: 'Dry run with file diffs (--check --diff).', ans_ln_hint_syntax: 'Only validate the YAML syntax (--syntax-check). No hosts needed.',
      ans_ln_survey: 'Survey', ans_ln_survey_pw: 'Password answers are passed to the run but tasks may still print them in the log.',
      ans_ln_advanced: 'Advanced', ans_ln_verbosity: 'Verbosity', ans_ln_verb_0: 'Normal', ans_ln_limit: 'Limit (--limit)', ans_ln_limit_ph: 'e.g. web*:!web-03',
      ans_ln_tags: 'Tags', ans_ln_tags_ph: 'e.g. web,nginx', ans_ln_skip: 'Skip tags', ans_ln_skip_ph: 'e.g. slow,backup',
      ans_ln_extra: 'Extra vars (JSON object)', ans_ln_extra_ph_default: 'Leave empty to use the playbook defaults', ans_ln_become: 'Become (sudo without password)',
      ans_ln_launch: 'Launch', ans_ln_launching: 'Launching…', ans_ln_hosts_none: 'no hosts', ans_ln_hosts_one: '1 host', ans_ln_hosts_n: '{n} hosts',
      ans_ln_summary: '{mode} {name} on {hosts}{extra}', ans_ln_sum_mode_run: 'Run', ans_ln_sum_mode_check: 'Check', ans_ln_sum_mode_diff: 'Diff', ans_ln_sum_mode_syntax: 'Syntax-check',
      ans_ln_err_targets: 'Select at least one target host (only the Syntax mode can run without hosts).', ans_ln_err_limit: 'Limit contains characters that are not allowed.',
      ans_ln_err_tags: 'Tags may only contain letters, digits and _ . , : -', ans_ln_err_extra: 'Extra vars must be a valid JSON object.',
      ans_ln_err_extra_big: 'Extra vars exceed 64 KiB.', ans_ln_err_required: '{label} is required.', ans_ln_err_number: '{label} must be a number.',
      ans_ln_err_integer: '{label} must be a whole number.', ans_ln_err_min: '{label} must be at least {n}.', ans_ln_err_max: '{label} must be at most {n}.',
      ans_ln_err_minlen: '{label} must be at least {n} characters.', ans_ln_err_maxlen: '{label} must not exceed {n} characters.', ans_ln_err_option: '{label} must be one of the options.',
      ans_ln_busy: 'This playbook is already running. Wait for it to finish or cancel it.', ans_ln_limit_reached: 'The global limit of concurrent runs is reached. Try again shortly.',
      ans_ln_failed: 'Could not start the run', ans_ln_started: 'Run #{id} started',
      ans_ln_gate_off: 'Playbooks cannot be run: the Ansible Gate is off or ansible-playbook is missing.', ans_ln_gate_settings: 'Open Settings',
      ans_ln_survey_failed: 'Could not load the survey', ans_ln_yes: 'Yes', ans_ln_required: 'required',
    },
    id: {
      ans_ln_title: 'Jalankan sekarang', ans_ln_targets: 'Host target', ans_ln_mode: 'Mode',
      ans_ln_mode_run: 'Run', ans_ln_mode_check: 'Check', ans_ln_mode_diff: 'Diff', ans_ln_mode_syntax: 'Syntax',
      ans_ln_hint_run: 'Jalankan playbook dan terapkan perubahan.', ans_ln_hint_check: 'Dry run (--check): laporkan apa yang akan berubah tanpa mengubah apa pun.',
      ans_ln_hint_diff: 'Dry run dengan diff berkas (--check --diff).', ans_ln_hint_syntax: 'Hanya validasi sintaks YAML (--syntax-check). Tidak perlu host.',
      ans_ln_survey: 'Survey', ans_ln_survey_pw: 'Jawaban password diteruskan ke run, tetapi task tetap bisa mencetaknya di log.',
      ans_ln_advanced: 'Lanjutan', ans_ln_verbosity: 'Verbositas', ans_ln_verb_0: 'Normal', ans_ln_limit: 'Limit (--limit)', ans_ln_limit_ph: 'mis. web*:!web-03',
      ans_ln_tags: 'Tag', ans_ln_tags_ph: 'mis. web,nginx', ans_ln_skip: 'Lewati tag', ans_ln_skip_ph: 'mis. slow,backup',
      ans_ln_extra: 'Extra vars (objek JSON)', ans_ln_extra_ph_default: 'Kosongkan untuk memakai bawaan playbook', ans_ln_become: 'Become (sudo tanpa password)',
      ans_ln_launch: 'Jalankan', ans_ln_launching: 'Menjalankan…', ans_ln_hosts_none: 'tanpa host', ans_ln_hosts_one: '1 host', ans_ln_hosts_n: '{n} host',
      ans_ln_summary: '{mode} {name} pada {hosts}{extra}', ans_ln_sum_mode_run: 'Run', ans_ln_sum_mode_check: 'Check', ans_ln_sum_mode_diff: 'Diff', ans_ln_sum_mode_syntax: 'Cek sintaks',
      ans_ln_err_targets: 'Pilih setidaknya satu host target (hanya mode Syntax yang bisa berjalan tanpa host).', ans_ln_err_limit: 'Limit berisi karakter yang tidak diizinkan.',
      ans_ln_err_tags: 'Tag hanya boleh berisi huruf, angka, dan _ . , : -', ans_ln_err_extra: 'Extra vars harus berupa objek JSON yang valid.',
      ans_ln_err_extra_big: 'Extra vars melebihi 64 KiB.', ans_ln_err_required: '{label} wajib diisi.', ans_ln_err_number: '{label} harus berupa angka.',
      ans_ln_err_integer: '{label} harus berupa bilangan bulat.', ans_ln_err_min: '{label} minimal {n}.', ans_ln_err_max: '{label} maksimal {n}.',
      ans_ln_err_minlen: '{label} minimal {n} karakter.', ans_ln_err_maxlen: '{label} maksimal {n} karakter.', ans_ln_err_option: '{label} harus salah satu opsi.',
      ans_ln_busy: 'Playbook ini sedang berjalan. Tunggu selesai atau batalkan.', ans_ln_limit_reached: 'Batas global run bersamaan tercapai. Coba lagi sebentar.',
      ans_ln_failed: 'Gagal memulai run', ans_ln_started: 'Run #{id} dimulai',
      ans_ln_gate_off: 'Playbook tidak bisa dijalankan: Ansible Gate nonaktif atau ansible-playbook tidak ada.', ans_ln_gate_settings: 'Buka Pengaturan',
      ans_ln_survey_failed: 'Gagal memuat survey', ans_ln_yes: 'Ya', ans_ln_required: 'wajib',
    },
  });

  const LIMIT_RE = /^[A-Za-z0-9_.,:!&*\[\]-]{1,256}$/;
  const TAGS_RE = /^[A-Za-z0-9_.,:-]{1,256}$/;
  const MODES = ['run', 'check', 'diff', 'syntax'];
  let uid = 0;

  function fieldLabel(f) { return (f.label || '').trim() || f.key; }

  // Mirrors internal/ansible/survey.go ApplySurvey for one field; returns {value, omit, error}.
  function surveyCheck(f, raw) {
    const label = fieldLabel(f);
    const empty = raw === undefined || raw === null || raw === '';
    if (f.type === 'boolean') return { value: !!raw };
    if (empty) {
      // an empty optional field is omitted; a required one needs an answer unless the playbook gives a default
      if (f.required && (f.default === undefined || f.default === null)) return { error: T('ans_ln_err_required', '{label} is required.', { label: label }) };
      return { omit: true };
    }
    switch (f.type) {
      case 'text': case 'textarea': case 'password': {
        const s = String(raw);
        if (f.min !== undefined && f.min !== null && s.length < f.min) return { error: T('ans_ln_err_minlen', '{label} must be at least {n} characters.', { label: label, n: f.min }) };
        if (f.max !== undefined && f.max !== null && s.length > f.max) return { error: T('ans_ln_err_maxlen', '{label} must not exceed {n} characters.', { label: label, n: f.max }) };
        return { value: s };
      }
      case 'number': case 'integer': {
        const n = typeof raw === 'number' ? raw : Number(String(raw).trim());
        if (!isFinite(n) || String(raw).trim() === '') return { error: T('ans_ln_err_number', '{label} must be a number.', { label: label }) };
        if (f.type === 'integer' && Math.trunc(n) !== n) return { error: T('ans_ln_err_integer', '{label} must be a whole number.', { label: label }) };
        if (f.min !== undefined && f.min !== null && n < f.min) return { error: T('ans_ln_err_min', '{label} must be at least {n}.', { label: label, n: f.min }) };
        if (f.max !== undefined && f.max !== null && n > f.max) return { error: T('ans_ln_err_max', '{label} must be at most {n}.', { label: label, n: f.max }) };
        return { value: n };
      }
      case 'select':
        return (f.options || []).indexOf(raw) >= 0 ? { value: raw } : { error: T('ans_ln_err_option', '{label} must be one of the options.', { label: label }) };
      case 'multiselect': {
        const list = Array.isArray(raw) ? raw : [];
        if (list.some(function (v) { return (f.options || []).indexOf(v) < 0; })) return { error: T('ans_ln_err_option', '{label} must be one of the options.', { label: label }) };
        return { value: list };
      }
    }
    return { omit: true };
  }

  function mount(container, pb, opts) {
    opts = opts || {};
    const id = ++uid;
    const key = 'launch.' + pb.id;
    const saved = Ans.store.get(key, {}) || {};
    const s = {
      mode: MODES.indexOf(saved.mode) >= 0 ? saved.mode : 'run', ids: Array.isArray(saved.ids) ? saved.ids : [],
      verbosity: Number.isInteger(saved.verbosity) ? saved.verbosity : 0, limit: saved.limit || '', tags: saved.tags || '', skip: saved.skip || '',
      extra: saved.extra || '', become: !!saved.become, answers: saved.answers || {}, fields: Array.isArray(pb.survey) && pb.survey.length ? pb.survey : null, busy: false, errors: {}, picker: null,
    };
    let destroyed = false;

    const q = function (sel) { return container.querySelector(sel); };
    const gateOk = function () { const g = Ans.state.status; return !g || (g.available && g.enabled); };

    function persist() {
      const answers = {};
      (s.fields || []).forEach(function (f) { if (f.type !== 'password' && s.answers[f.key] !== undefined) answers[f.key] = s.answers[f.key]; });
      Ans.store.set(key, { mode: s.mode, ids: s.ids, verbosity: s.verbosity, limit: s.limit, tags: s.tags, skip: s.skip, extra: s.extra, become: s.become, answers: answers });
    }

    function summary() {
      const hosts = (Ans.state.hosts || []).filter(function (h) { return s.ids.indexOf(h.id) >= 0; });
      let who;
      if (!s.ids.length) who = T('ans_ln_hosts_none', 'no hosts');
      else if (s.ids.length === 1) who = hosts[0] ? hosts[0].name : T('ans_ln_hosts_one', '1 host');
      else {
        const names = hosts.slice(0, 2).map(function (h) { return h.name; });
        who = T('ans_ln_hosts_n', '{n} hosts', { n: s.ids.length }) + (names.length ? ' (' + names.join(', ') + (s.ids.length > 2 ? ', +' + (s.ids.length - 2) : '') + ')' : '');
      }
      const extra = (s.verbosity ? ' -' + 'v'.repeat(s.verbosity) : '') + (s.become ? ' · become' : '');
      return T('ans_ln_summary', '{mode} {name} on {hosts}{extra}', { mode: T('ans_ln_sum_mode_' + s.mode), name: pb.name, hosts: who, extra: extra ? ' ·' + extra : '' });
    }
    function refreshSummary() {
      const el = q('[data-l="summary"]'); if (el) el.textContent = summary();
      const hint = q('[data-l="modehint"]'); if (hint) hint.textContent = T('ans_ln_hint_' + s.mode);
      const star = q('[data-l="star"]'); if (star) star.hidden = s.mode === 'syntax';
    }

    function surveyHtml() {
      if (!pb.has_survey) return '';
      if (s.fields === null) return '<div class="ans-field" data-l="survey">' + Ans.ui.skeleton(2) + '</div>';
      if (!s.fields.length) return '';
      const hasPw = s.fields.some(function (f) { return f.type === 'password'; });
      return '<div class="ans-field" data-l="survey"><label>' + esc(T('ans_ln_survey')) + '</label><div class="ans-ln-survey ans-grid-2">' + s.fields.map(function (f, i) {
        const fid = 'ans-ln-' + id + '-' + i, val = s.answers[f.key] !== undefined ? s.answers[f.key] : f.default;
        const lab = '<label for="' + fid + '">' + esc(fieldLabel(f)) + (f.required ? ' <span class="ans-ln-req" title="' + esc(T('ans_ln_required')) + '">*</span>' : '') + '</label>';
        let input;
        if (f.type === 'textarea') input = '<textarea id="' + fid + '" class="form-input" rows="3" data-sv="' + esc(f.key) + '">' + esc(val === undefined || val === null ? '' : val) + '</textarea>';
        else if (f.type === 'select') input = '<select id="' + fid + '" class="form-input" data-sv="' + esc(f.key) + '">' + (f.required ? '' : '<option value=""></option>') + (f.options || []).map(function (o) { return '<option value="' + esc(o) + '"' + (val === o ? ' selected' : '') + '>' + esc(o) + '</option>'; }).join('') + '</select>';
        else if (f.type === 'multiselect') { const cur = Array.isArray(val) ? val : []; input = '<div class="ans-ln-multi" id="' + fid + '" role="group">' + (f.options || []).map(function (o) { return '<label class="ans-ln-opt"><input type="checkbox" data-sv-multi="' + esc(f.key) + '" value="' + esc(o) + '"' + (cur.indexOf(o) >= 0 ? ' checked' : '') + ' /> ' + esc(o) + '</label>'; }).join('') + '</div>'; }
        else if (f.type === 'boolean') input = '<label class="switch"><input type="checkbox" id="' + fid + '" data-sv="' + esc(f.key) + '"' + (val ? ' checked' : '') + ' /><span class="switch-track"><span class="switch-thumb"></span></span></label>';
        else input = '<input id="' + fid + '" class="form-input" data-sv="' + esc(f.key) + '" type="' + (f.type === 'password' ? 'password' : (f.type === 'number' || f.type === 'integer') ? 'number' : 'text') + '"' +
          (f.type === 'number' ? ' step="any"' : f.type === 'integer' ? ' step="1"' : '') + (f.type === 'password' ? ' autocomplete="new-password"' : '') + ' value="' + esc(val === undefined || val === null ? '' : val) + '" />';
        return '<div class="ans-field' + (f.type === 'textarea' || f.type === 'multiselect' ? ' ans-ln-wide' : '') + '">' + lab + input + (f.description ? '<div class="hint">' + esc(f.description) + '</div>' : '') + '<div class="err" data-sv-err="' + esc(f.key) + '"></div></div>';
      }).join('') + '</div>' + (hasPw ? '<div class="hint">' + icon('shield-alert', 'icon-sm') + ' ' + esc(T('ans_ln_survey_pw')) + '</div>' : '') + '</div>';
    }

    function render() {
      container.innerHTML =
        '<div class="ans-card-title">' + icon('play', 'icon-sm') + esc(T('ans_ln_title', 'Run now')) + '</div>' +
        (gateOk() ? '' : '<div class="ans-error" role="status">' + icon('shield-alert') + '<span>' + esc(T('ans_ln_gate_off')) + '</span><button type="button" class="btn btn-sm" data-l="settings">' + esc(T('ans_ln_gate_settings')) + '</button></div>') +
        '<fieldset class="ans-ln" ' + (gateOk() ? '' : 'disabled') + '>' +
        '<div class="ans-field"><label>' + esc(T('ans_ln_targets')) + ' <span class="ans-ln-req" data-l="star">*</span></label><div data-l="picker"></div></div>' +
        '<div class="ans-field"><label>' + esc(T('ans_ln_mode')) + '</label><div class="ans-seg" role="group" data-l="mode">' + MODES.map(function (m) {
          return '<button type="button" data-mode="' + m + '" aria-pressed="' + (s.mode === m) + '">' + esc(T('ans_ln_mode_' + m)) + '</button>';
        }).join('') + '</div><div class="hint" data-l="modehint"></div></div>' +
        surveyHtml() +
        '<details class="ans-details"' + (s.limit || s.tags || s.skip || s.extra || s.verbosity || s.become ? ' open' : '') + '><summary>' + esc(T('ans_ln_advanced')) + '</summary><div class="ans-grid-2 ans-ln-adv">' +
        '<div class="ans-field"><label for="ansibleRunVerbosity">' + esc(T('ans_ln_verbosity')) + '</label><select id="ansibleRunVerbosity" class="form-input" data-f="verbosity">' + [0, 1, 2, 3, 4].map(function (n) { return '<option value="' + n + '"' + (s.verbosity === n ? ' selected' : '') + '>' + (n === 0 ? esc(T('ans_ln_verb_0')) + ' (0)' : '-' + 'v'.repeat(n) + ' (' + n + ')') + '</option>'; }).join('') + '</select></div>' +
        '<div class="ans-field"><label for="ansibleRunLimit">' + esc(T('ans_ln_limit')) + '</label><input id="ansibleRunLimit" class="form-input ans-mono" data-f="limit" placeholder="' + esc(T('ans_ln_limit_ph')) + '" value="' + esc(s.limit) + '" /><div class="err" data-err="limit"></div></div>' +
        '<div class="ans-field"><label for="ansibleRunTags">' + esc(T('ans_ln_tags')) + '</label><input id="ansibleRunTags" class="form-input ans-mono" data-f="tags" placeholder="' + esc(T('ans_ln_tags_ph')) + '" value="' + esc(s.tags) + '" /><div class="err" data-err="tags"></div></div>' +
        '<div class="ans-field"><label for="ansibleRunSkipTags">' + esc(T('ans_ln_skip')) + '</label><input id="ansibleRunSkipTags" class="form-input ans-mono" data-f="skip" placeholder="' + esc(T('ans_ln_skip_ph')) + '" value="' + esc(s.skip) + '" /><div class="err" data-err="skip"></div></div>' +
        '<div class="ans-field ans-ln-wide"><label for="ansibleRunExtraVars">' + esc(T('ans_ln_extra')) + '</label><textarea id="ansibleRunExtraVars" class="form-input ans-mono" rows="3" data-f="extra" placeholder="' + esc(pb.extra_vars || T('ans_ln_extra_ph_default')) + '">' + esc(s.extra) + '</textarea><div class="err" data-err="extra"></div></div>' +
        '<div class="ans-field ans-ln-wide"><label class="ans-row" style="cursor:pointer;"><span class="switch"><input type="checkbox" id="ansibleRunBecome" data-f="become"' + (s.become ? ' checked' : '') + ' /><span class="switch-track"><span class="switch-thumb"></span></span></span><span>' + esc(T('ans_ln_become')) + '</span></label></div>' +
        '</div></details>' +
        '<div class="ans-error" role="alert" data-l="error" hidden></div>' +
        '<div class="ans-ln-bar"><span class="ans-ln-summary" data-l="summary"></span><button type="button" id="btnAnsibleRunLaunchSubmit" class="btn btn-primary" data-launch="submit">' + icon('play', 'icon-sm') + ' <span>' + esc(T('ans_ln_launch', 'Launch')) + '</span> <kbd class="ans-kbd">Ctrl+↵</kbd></button></div>' +
        '</fieldset>';
      if (s.picker) { try { s.picker.destroy(); } catch (e) { /* detached */ } }
      s.picker = Ans.hostPicker.mount(q('[data-l="picker"]'), { ids: s.ids, max: 200, onChange: function (ids) { s.ids = ids; refreshSummary(); persist(); } });
      refreshSummary();
    }

    function setBusy(on) {
      s.busy = on;
      const b = q('[data-launch="submit"]');
      if (b) { b.disabled = on; b.querySelector('span').textContent = on ? T('ans_ln_launching') : T('ans_ln_launch'); }
    }
    function showError(msg) { const e = q('[data-l="error"]'); if (e) { e.hidden = !msg; e.textContent = msg || ''; } }
    function fieldErr(name, msg) { const e = q('[data-err="' + name + '"]'); if (e) e.textContent = msg || ''; }
    function clearErrors() {
      showError(''); ['limit', 'tags', 'skip', 'extra'].forEach(function (n) { fieldErr(n, ''); });
      container.querySelectorAll('[data-sv-err]').forEach(function (e) { e.textContent = ''; });
    }

    // returns the request body or null after showing inline errors
    function collect() {
      clearErrors();
      let bad = null;
      const fail = function (name, msg, el) { fieldErr(name, msg); if (!bad) bad = el || q('[data-f="' + name + '"]'); };
      if (s.mode !== 'syntax' && !s.ids.length) { showError(T('ans_ln_err_targets')); bad = bad || q('[data-l="picker"] input'); }
      if (s.limit && !LIMIT_RE.test(s.limit)) fail('limit', T('ans_ln_err_limit'));
      if (s.tags && !TAGS_RE.test(s.tags)) fail('tags', T('ans_ln_err_tags'));
      if (s.skip && !TAGS_RE.test(s.skip)) fail('skip', T('ans_ln_err_tags'));
      let extra = '';
      if (s.extra.trim()) {
        if (s.extra.length > 64 * 1024) fail('extra', T('ans_ln_err_extra_big'));
        else { try { const o = JSON.parse(s.extra); if (!o || typeof o !== 'object' || Array.isArray(o)) throw new Error('not an object'); extra = s.extra.trim(); } catch (e) { fail('extra', T('ans_ln_err_extra')); } }
      }
      const answers = {};
      (s.fields || []).forEach(function (f) {
        const r = surveyCheck(f, s.answers[f.key] !== undefined ? s.answers[f.key] : f.default);
        if (r.error) { const e = q('[data-sv-err="' + f.key + '"]'); if (e) e.textContent = r.error; if (!bad) bad = q('[data-sv="' + f.key + '"]') || q('[data-sv-multi="' + f.key + '"]'); }
        else if (!r.omit) answers[f.key] = r.value;
      });
      if (bad) { if (bad.focus) bad.focus(); const d = bad.closest && bad.closest('details'); if (d) d.open = true; return null; }
      const body = { mode: s.mode, host_ids: s.ids, verbosity: s.verbosity, become: s.become };
      if (extra) body.extra_vars = extra;
      if (s.tags) body.tags = s.tags;
      if (s.skip) body.skip_tags = s.skip;
      if (s.limit) body.limit = s.limit;
      if (s.fields && s.fields.length) body.survey_answers = answers;
      return body;
    }

    async function submit() {
      if (s.busy || !gateOk()) return;
      const body = collect();
      if (!body) return;
      setBusy(true);
      try {
        const d = await Ans.api.post('/api/ansible/playbooks/' + pb.id + '/run', body);
        persist();
        const run = d && d.run;
        Ans.toast(T('ans_ln_started', 'Run #{id} started', { id: run ? run.id : '' }), 'success');
        Ans.load.playbooks().catch(function () { /* the list polls anyway */ });
        if (opts.onLaunched) opts.onLaunched(run);
        if (run && Ans.runs && Ans.runs.openViewer) Ans.runs.openViewer(run.id, { playbookId: pb.id });
      } catch (e) {
        const msg = e.status === 409 ? T('ans_ln_busy') : e.status === 429 ? T('ans_ln_limit_reached') : e.message;
        showError(T('ans_ln_failed') + ': ' + msg);
      } finally { if (!destroyed) setBusy(false); }
    }

    container.classList.add('ans-ln-root');
    const onClick = function (e) {
      const m = e.target.closest('[data-mode]');
      if (m) { s.mode = m.getAttribute('data-mode'); container.querySelectorAll('[data-mode]').forEach(function (b) { b.setAttribute('aria-pressed', String(b === m)); }); refreshSummary(); persist(); return; }
      if (e.target.closest('[data-launch="submit"]')) { submit(); return; }
      if (e.target.closest('[data-l="settings"]')) Ans.showTab('settings');
    };
    const onInput = function (e) {
      const t = e.target, f = t.getAttribute && t.getAttribute('data-f');
      if (f) {
        if (f === 'verbosity') s.verbosity = Number(t.value) || 0; else if (f === 'become') s.become = t.checked; else s[f] = t.value;
        refreshSummary(); persist(); return;
      }
      const sv = t.getAttribute && t.getAttribute('data-sv');
      if (sv) { s.answers[sv] = t.type === 'checkbox' ? t.checked : t.value; persist(); return; }
      const mv = t.getAttribute && t.getAttribute('data-sv-multi');
      if (mv) { s.answers[mv] = Array.from(container.querySelectorAll('[data-sv-multi="' + mv + '"]:checked')).map(function (c) { return c.value; }); persist(); }
    };
    const onKey = function (e) { if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) { e.preventDefault(); submit(); } };
    container.addEventListener('click', onClick);
    container.addEventListener('input', onInput);
    container.addEventListener('change', onInput);
    container.addEventListener('keydown', onKey);
    const onStatus = Ans.on('status', function () { if (!destroyed) render(); });

    render();
    if (pb.has_survey && s.fields === null) {
      Ans.api.get('/api/ansible/playbooks/' + pb.id + '/survey').then(function (d) {
        if (destroyed) return;
        s.fields = (d && d.survey) || [];
        render();
      }).catch(function (e) {
        if (destroyed) return;
        s.fields = [];
        render();
        showError(T('ans_ln_survey_failed') + ': ' + e.message);
      });
    }

    return {
      destroy: function () {
        destroyed = true;
        Ans.off('status', onStatus);
        container.removeEventListener('click', onClick); container.removeEventListener('input', onInput);
        container.removeEventListener('change', onInput); container.removeEventListener('keydown', onKey);
        if (s.picker) { try { s.picker.destroy(); } catch (e) { /* detached */ } }
        container.classList.remove('ans-ln-root');
        container.innerHTML = '';
      },
      isDirty: function () { return false; },
    };
  }

  Ans.launch = { mount: mount };
})();
