/* Playbook parts: the YAML of a single-play playbook cut into Play / Pre-tasks / Roles / Tasks. The raw text stays
   the source of truth; parts are derived with POST /playbooks/outline and joined with POST /playbooks/compose only
   when a part was edited, so merely looking at a part never rewrites the raw YAML. */
(function () {
  'use strict';
  const Ans = window.Ans;
  const esc = Ans.esc, T = Ans.t, icon = Ans.icon;

  Ans.addI18n({
    en: {
      ans_pt_pre: 'Pre-tasks', ans_pt_roles: 'Roles', ans_pt_tasks: 'Tasks', ans_pt_post: 'Post-tasks', ans_pt_handlers: 'Handlers', ans_pt_raw: 'Raw',
      ans_pt_hint_rest: 'Name, hosts, become, vars and the other play settings are edited in Raw.',
      ans_pt_hint_pre: 'Tasks that run before the roles, as a YAML list. Leave empty for none.',
      ans_pt_hint_tasks: 'Tasks that run after the roles, as a YAML list. Leave empty for none.',
      ans_pt_hint_post: 'Tasks that run after the tasks, as a YAML list. Leave empty for none.',
      ans_pt_hint_handlers: 'Handlers triggered by notify, as a YAML list; they run at the end of the play. Leave empty for none.',
      ans_pt_hint_roles: 'Roles run in this order, after the pre-tasks and before the tasks. Pick them from the roles and collections installed in Resources → Library.',
      ans_pt_unstructured: 'The parts view needs a playbook with exactly one play that parses as YAML. Edit it in Raw.',
      ans_pt_open_raw: 'Open Raw', ans_pt_loading: 'Splitting the playbook…',
      ans_pt_selected: 'Selected roles', ans_pt_none: 'No roles yet. Add one below.', ans_pt_add: 'Add role',
      ans_pt_search: 'Search installed roles…', ans_pt_catalog_loading: 'Loading installed roles…', ans_pt_catalog_fail: 'Could not list the installed roles',
      ans_pt_catalog_empty: 'No roles are installed. Install roles or collections in Resources → Library, or type a role name above to add it anyway.',
      ans_pt_no_match: 'No installed role matches “{q}”.', ans_pt_more: '{n} more, refine the search',
      ans_pt_add_anyway: 'Add “{n}” (not installed)', ans_pt_missing: 'not installed', ans_pt_missing_tip: 'This role is not in the Library or the system role paths. The run fails unless it is installed.',
      ans_pt_o_library: 'Library', ans_pt_o_collection: 'Collection {c}', ans_pt_o_system: 'System',
      ans_pt_params: 'Parameters', ans_pt_params_ph: 'vars:\n  key: value\ntags: [deploy]\nwhen: env == "prod"',
      ans_pt_params_hint: 'YAML mapping of the role entry: vars, tags, when, … (the role name is set above).',
      ans_pt_up: 'Move up', ans_pt_down: 'Move down', ans_pt_remove: 'Remove role', ans_pt_added: 'added ×{n}',
    },
    id: {
      ans_pt_pre: 'Pre-task', ans_pt_roles: 'Role', ans_pt_tasks: 'Task', ans_pt_post: 'Post-task', ans_pt_handlers: 'Handler', ans_pt_raw: 'Raw',
      ans_pt_hint_rest: 'Name, hosts, become, vars dan pengaturan play lainnya diedit di Raw.',
      ans_pt_hint_pre: 'Task yang berjalan sebelum role, berupa daftar YAML. Kosongkan bila tidak ada.',
      ans_pt_hint_tasks: 'Task yang berjalan setelah role, berupa daftar YAML. Kosongkan bila tidak ada.',
      ans_pt_hint_post: 'Task yang berjalan setelah task, berupa daftar YAML. Kosongkan bila tidak ada.',
      ans_pt_hint_handlers: 'Handler yang dipicu lewat notify, berupa daftar YAML dan dijalankan di akhir play. Kosongkan bila tidak ada.',
      ans_pt_hint_roles: 'Role berjalan sesuai urutan ini, setelah pre-task dan sebelum task. Pilih dari role dan koleksi yang terpasang di Resources → Library.',
      ans_pt_unstructured: 'Tampilan bagian membutuhkan playbook dengan tepat satu play yang YAML-nya valid. Edit di Raw.',
      ans_pt_open_raw: 'Buka Raw', ans_pt_loading: 'Memecah playbook…',
      ans_pt_selected: 'Role terpilih', ans_pt_none: 'Belum ada role. Tambahkan di bawah.', ans_pt_add: 'Tambah role',
      ans_pt_search: 'Cari role terpasang…', ans_pt_catalog_loading: 'Memuat role terpasang…', ans_pt_catalog_fail: 'Gagal membaca daftar role terpasang',
      ans_pt_catalog_empty: 'Belum ada role terpasang. Pasang role atau koleksi di Resources → Library, atau ketik nama role di atas untuk tetap menambahkannya.',
      ans_pt_no_match: 'Tidak ada role terpasang yang cocok dengan “{q}”.', ans_pt_more: '{n} lagi, persempit pencarian',
      ans_pt_add_anyway: 'Tambah “{n}” (belum terpasang)', ans_pt_missing: 'belum terpasang', ans_pt_missing_tip: 'Role ini tidak ada di Library maupun path role sistem. Run gagal kecuali role terpasang.',
      ans_pt_o_library: 'Library', ans_pt_o_collection: 'Koleksi {c}', ans_pt_o_system: 'Sistem',
      ans_pt_params: 'Parameter', ans_pt_params_ph: 'vars:\n  key: value\ntags: [deploy]\nwhen: env == "prod"',
      ans_pt_params_hint: 'Mapping YAML dari entri role: vars, tags, when, … (nama role diatur di atas).',
      ans_pt_up: 'Naikkan', ans_pt_down: 'Turunkan', ans_pt_remove: 'Hapus role', ans_pt_added: 'ditambah ×{n}',
    },
  });

  const FIELDS = ['pre', 'tasks', 'post', 'handlers'];
  const ROLE_NAME_RE = /^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$/;
  const MAX_SHOWN = 80;

  // attach binds the parts to one editor instance. ctx: { S (shared editor state), q, notifyDirty, showError, newCM, goto }.
  function attach(ctx) {
    const S = ctx.S;
    // P.play holds the play keys that have no tab (name, hosts, become, vars, ...): they are only edited in Raw and carried through unchanged.
    const P = { ok: false, reason: '', play: '', pre: '', tasks: '', post: '', handlers: '', roles: [], from: null, dirty: false, catalog: null, catalogErr: '', loading: false, rq: '' };
    let pcm = null, pta = null, curField = '';

    const has = function (sub) { return sub === 'roles' || FIELDS.indexOf(sub) >= 0; };
    const capture = function () {
      if (curField && (pcm || pta)) { const v = pcm ? pcm.getValue() : pta.value; if (v !== P[curField]) { P[curField] = v; P.dirty = true; } }
    };
    function destroyCM() { capture(); if (pcm) { try { pcm.toTextArea(); } catch (e) { /* detached */ } } pcm = null; pta = null; curField = ''; }

    async function outline() {
      try {
        const o = await Ans.api.post('/api/ansible/playbooks/outline', { content: S.content });
        P.ok = !!o.structured; P.reason = o.reason || '';
        P.play = o.play || ''; P.pre = o.pre_tasks || ''; P.tasks = o.tasks || ''; P.post = o.post_tasks || ''; P.handlers = o.handlers || '';
        P.roles = (o.roles || []).map(function (r) { return { name: r.name, params: r.params || '', open: !!r.params }; });
      } catch (e) { P.ok = false; P.reason = e.message; }
      P.from = S.content; P.dirty = false;
    }
    async function loadCatalog() {
      if (P.loading || P.catalog) return;
      P.loading = true; P.catalogErr = ''; P.catalogT0 = Date.now();
      try { const d = await Ans.api.get('/api/ansible/roles'); P.catalog = (d && d.roles) || []; } catch (e) { P.catalogErr = e.message; P.catalog = []; }
      P.loading = false;
      paintCatalog();
    }

    async function flush() {
      if (has(S.sub)) capture();
      if (!P.dirty) return true;
      try {
        const d = await Ans.api.post('/api/ansible/playbooks/compose', { play: P.play, pre_tasks: P.pre, tasks: P.tasks, post_tasks: P.post, handlers: P.handlers, roles: P.roles.map(function (r) { return { name: r.name, params: r.params }; }) });
        S.content = d.content; P.from = d.content; P.dirty = false;
        return true;
      } catch (e) { ctx.showError(e.message); return false; }
    }
    async function leave(cur, next) {
      if (!has(cur)) return true;
      destroyCM();
      return has(next) ? true : flush();
    }
    async function enter(next) {
      if (!has(next)) return true;
      if (P.from !== S.content) await outline();
      if (next === 'roles' && P.ok) loadCatalog();
      return true;
    }

    /* ----- painting ----- */
    function paint(sub, body) {
      destroyCM();
      if (!P.ok) {
        body.innerHTML = '<div class="ans-ed-scroll"><div class="empty-state ans-empty"><div class="empty-state-icon">' + icon('info', 'icon-xl') + '</div><p>' + esc(T('ans_pt_unstructured')) + '</p>' + (P.reason ? '<p class="ans-muted ans-mono">' + esc(P.reason) + '</p>' : '') +
          '<button type="button" class="btn btn-sm" data-pt="raw">' + esc(T('ans_pt_open_raw')) + '</button></div></div>';
        return;
      }
      if (sub === 'roles') { paintRoles(body); return; }
      body.innerHTML = '<div class="ans-ed-hintbar ans-hint">' + icon('info', 'icon-sm') + ' <span>' + esc(T('ans_pt_hint_' + sub)) + ' <span class="ans-muted">' + esc(T('ans_pt_hint_rest')) + '</span></span></div><div class="ans-ed-cm"><textarea id="ansiblePart' + sub + '"></textarea></div>';
      pta = body.querySelector('textarea'); pta.value = P[sub]; curField = sub;
      pcm = ctx.newCM(pta);
      if (pcm) { pcm.on('change', function () { capture(); ctx.notifyDirty(); }); setTimeout(function () { if (pcm) pcm.refresh(); }, 30); }
      else pta.addEventListener('input', function () { capture(); ctx.notifyDirty(); });
    }

    function originBadge(c) {
      if (!c) return '<span class="badge badge-warning" title="' + esc(T('ans_pt_missing_tip')) + '">' + esc(T('ans_pt_missing')) + '</span>';
      const label = c.origin === 'collection' ? T('ans_pt_o_collection', 'Collection {c}', { c: c.collection }) : c.origin === 'system' ? T('ans_pt_o_system') : T('ans_pt_o_library') + (c.version ? ' ' + c.version : '');
      return '<span class="badge badge-unknown">' + esc(label) + '</span>';
    }
    const find = function (name) { return (P.catalog || []).find(function (c) { return c.name === name; }); };

    function paintRoles(body) {
      body.innerHTML = '<div class="ans-ed-scroll"><div class="ans-ed-form"><div class="ans-hint">' + icon('info', 'icon-sm') + ' ' + esc(T('ans_pt_hint_roles')) + ' <span class="ans-muted">' + esc(T('ans_pt_hint_rest')) + '</span></div>' +
        '<h4 class="ans-pt-h">' + esc(T('ans_pt_selected')) + ' <span class="ans-muted" data-pt="count"></span></h4><div class="ans-pt-list" data-pt="selected"></div>' +
        '<h4 class="ans-pt-h">' + esc(T('ans_pt_add')) + '</h4>' +
        '<div class="search-box ans-ed-search">' + icon('search', 'icon-sm text-muted') + '<input class="form-input" data-pt="search" id="ansiblePartRoleSearch" placeholder="' + esc(T('ans_pt_search')) + '" value="' + esc(P.rq) + '" autocomplete="off" aria-label="' + esc(T('ans_pt_search')) + '" /></div>' +
        '<div class="ans-pt-pick" data-pt="available"></div></div></div>';
      paintSelected(); paintCatalog();
    }
    function paintSelected() {
      const box = ctx.q('[data-pt="selected"]'); if (!box) return;
      const cnt = ctx.q('[data-pt="count"]'); if (cnt) cnt.textContent = P.roles.length ? '(' + P.roles.length + ')' : '';
      if (!P.roles.length) { box.innerHTML = '<div class="ans-muted ans-pt-empty">' + esc(T('ans_pt_none')) + '</div>'; return; }
      box.innerHTML = P.roles.map(function (r, i) {
        const known = P.catalog ? find(r.name) : undefined;
        const badge = P.catalog ? originBadge(known) : '';
        return '<div class="ans-pt-role" data-ri="' + i + '"><div class="ans-pt-rhead"><span class="ans-pt-order">' + (i + 1) + '</span><span class="ans-mono ans-pt-rname">' + esc(r.name) + '</span>' + badge + '<span class="ans-spacer"></span>' +
          '<button type="button" class="btn btn-sm btn-ghost ans-btn-icon" data-pt="params-toggle" aria-pressed="' + !!r.open + '" title="' + esc(T('ans_pt_params')) + '" aria-label="' + esc(T('ans_pt_params')) + '">' + icon('sliders-horizontal', 'icon-sm') + (r.params && !r.open ? '<span class="ans-pt-dot"></span>' : '') + '</button>' +
          '<button type="button" class="btn btn-sm btn-ghost ans-btn-icon" data-pt="up" title="' + esc(T('ans_pt_up')) + '" aria-label="' + esc(T('ans_pt_up')) + '"' + (i === 0 ? ' disabled' : '') + '>' + icon('arrow-up', 'icon-sm') + '</button>' +
          '<button type="button" class="btn btn-sm btn-ghost ans-btn-icon" data-pt="down" title="' + esc(T('ans_pt_down')) + '" aria-label="' + esc(T('ans_pt_down')) + '"' + (i === P.roles.length - 1 ? ' disabled' : '') + '>' + icon('arrow-down', 'icon-sm') + '</button>' +
          '<button type="button" class="btn btn-sm btn-ghost ans-btn-icon" data-pt="remove" title="' + esc(T('ans_pt_remove')) + '" aria-label="' + esc(T('ans_pt_remove')) + '">' + icon('trash-2', 'icon-sm') + '</button></div>' +
          (r.open ? '<div class="ans-pt-params"><textarea class="form-input ans-mono" data-pt="params" rows="5" spellcheck="false" placeholder="' + esc(T('ans_pt_params_ph')) + '" aria-label="' + esc(T('ans_pt_params') + ' ' + r.name) + '">' + esc(r.params) + '</textarea><div class="ans-hint">' + esc(T('ans_pt_params_hint')) + '</div></div>' : '') + '</div>';
      }).join('');
    }
    function paintCatalog() {
      const box = ctx.q('[data-pt="available"]'); if (!box) return;
      if (P.loading || !P.catalog) { box.innerHTML = '<div class="ans-muted">' + SP.html(T('ans_pt_catalog_loading'), P.catalogT0 || Date.now()) + '</div>'; return; }
      const q = P.rq.trim().toLowerCase();
      const hits = P.catalog.filter(function (c) { return !q || (c.name + ' ' + (c.description || '') + ' ' + (c.collection || '')).toLowerCase().indexOf(q) >= 0; });
      const count = function (n) { return P.roles.filter(function (r) { return r.name === n; }).length; };
      let html = P.catalogErr ? '<div class="ans-err">' + esc(T('ans_pt_catalog_fail')) + ': ' + esc(P.catalogErr) + '</div>' : '';
      const name = P.rq.trim();
      if (name && ROLE_NAME_RE.test(name) && !find(name)) html += '<div class="ans-pt-item"><div class="ans-pt-info"><span class="ans-mono">' + esc(name) + '</span> ' + originBadge(null) + '</div><button type="button" class="btn btn-sm" data-pt="add" data-name="' + esc(name) + '">' + icon('plus', 'icon-sm') + ' ' + esc(T('ans_pt_add_anyway', 'Add “{n}” (not installed)', { n: name })) + '</button></div>';
      html += hits.slice(0, MAX_SHOWN).map(function (c) {
        const n = count(c.name);
        return '<div class="ans-pt-item"><div class="ans-pt-info"><div><span class="ans-mono ans-pt-rname">' + esc(c.name) + '</span> ' + originBadge(c) + (n ? ' <span class="ans-muted ans-hint">' + esc(T('ans_pt_added', 'added ×{n}', { n: n })) + '</span>' : '') + '</div>' +
          (c.description ? '<div class="ans-muted ans-hint">' + esc(c.description) + '</div>' : '') + '</div>' +
          '<button type="button" class="btn btn-sm" data-pt="add" data-name="' + esc(c.name) + '">' + icon('plus', 'icon-sm') + ' ' + esc(T('ans_pt_add')) + '</button></div>';
      }).join('');
      if (hits.length > MAX_SHOWN) html += '<div class="ans-muted ans-hint">' + esc(T('ans_pt_more', '{n} more, refine the search', { n: hits.length - MAX_SHOWN })) + '</div>';
      if (!hits.length && !P.catalogErr) html += '<div class="ans-muted ans-pt-empty">' + esc(q ? T('ans_pt_no_match', 'No installed role matches “{q}”.', { q: P.rq.trim() }) : T('ans_pt_catalog_empty')) + '</div>';
      box.innerHTML = html;
    }

    /* ----- events (delegated by the editor) ----- */
    function touch() { P.dirty = true; ctx.notifyDirty(); }
    function click(e) {
      const b = e.target.closest('[data-pt]'); if (!b) return false;
      const act = b.getAttribute('data-pt');
      if (act === 'raw') { ctx.goto('raw'); return true; }
      if (act === 'add') { P.roles.push({ name: b.getAttribute('data-name'), params: '', open: false }); touch(); paintSelected(); paintCatalog(); return true; }
      const row = b.closest('[data-ri]'); if (!row) return false;
      const i = Number(row.getAttribute('data-ri'));
      if (act === 'remove') { P.roles.splice(i, 1); touch(); paintSelected(); paintCatalog(); }
      else if (act === 'up' && i > 0) { P.roles.splice(i - 1, 0, P.roles.splice(i, 1)[0]); touch(); paintSelected(); }
      else if (act === 'down' && i < P.roles.length - 1) { P.roles.splice(i + 1, 0, P.roles.splice(i, 1)[0]); touch(); paintSelected(); }
      else if (act === 'params-toggle') { P.roles[i].open = !P.roles[i].open; paintSelected(); const t = ctx.q('[data-ri="' + i + '"] textarea'); if (t) t.focus(); }
      return true;
    }
    function input(e) {
      const t = e.target; const act = t.getAttribute && t.getAttribute('data-pt');
      if (act === 'search') { if (e.type === 'input') { P.rq = t.value; paintCatalog(); } return true; } // a repaint on blur would swallow the click that caused it
      if (act === 'params') { const row = t.closest('[data-ri]'); if (row) { P.roles[Number(row.getAttribute('data-ri'))].params = t.value; touch(); } return true; }
      return false;
    }

    return {
      order: ['pre', 'roles', 'tasks', 'post', 'handlers', 'raw'], has: has, paint: paint, destroy: destroyCM, flush: flush, leave: leave, enter: enter, click: click, input: input,
      token: function () { return P.dirty ? JSON.stringify([P.pre, P.tasks, P.post, P.handlers, P.roles.map(function (r) { return [r.name, r.params]; })]) : ''; },
      reset: function () { destroyCM(); P.from = null; P.dirty = false; },
      // Force the next enter() to re-outline after the raw text changed outside the parts editors.
      invalidate: function () { P.from = null; P.dirty = false; },
    };
  }

  Ans.editorParts = { attach: attach };
})();
