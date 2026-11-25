/* Variables module: top tab "Variables & Cloud" (Variable Sets, Cloud Accounts), and the workspace drawer
   "Variables" tab (Workspace/Effective variables, Detect from Configuration, Linked Outputs). */
(function () {
  'use strict';

  const Tf = window.Tf;
  const t = Tf.t, e = Tf.esc, ic = Tf.icon;

  Tf.addI18n({
    en: {
      tf_tab_variables: 'Variables & Cloud', tf_ws_tab_variables: 'Variables',
      tf_seg_label: 'Variables and cloud accounts',
      tf_vs_title: 'Variable Sets', tf_vs_search: 'Search variable sets…', tf_vs_new: 'New Variable Set', tf_vs_edit: 'Edit Variable Set',
      tf_vs_empty: 'No variable sets yet. A set shares variables across workspaces.',
      tf_vs_filter_all: 'All', tf_vs_filter_global: 'Global', tf_vs_filter_scoped: 'Workspace-scoped',
      tf_vs_name: 'Name', tf_vs_desc: 'Description', tf_vs_global: 'Global (all workspaces)', tf_vs_global_hint: 'Apply to every workspace automatically',
      tf_vs_workspaces: 'Linked workspaces', tf_vs_ws_search: 'Filter workspaces…', tf_vs_vars: 'Variables',
      tf_vs_save_first: 'Save the set first, then add variables to it.', tf_vs_name_required: 'Name is required.',
      tf_vs_saved: 'Variable set saved', tf_vs_deleted: 'Variable set deleted',
      tf_vs_delete_title: 'Delete variable set', tf_vs_delete_msg: 'Delete variable set "{name}"? Linked workspaces will lose its variables.',
      tf_vs_all_ws: 'All workspaces', tf_vs_n_ws: '{n} workspaces', tf_vs_n_vars: '{n} variables', tf_vs_no_ws: 'No workspaces yet',
      tf_var_add: 'Add Variable', tf_var_edit: 'Edit Variable', tf_var_key: 'Key', tf_var_value: 'Value', tf_var_category: 'Category',
      tf_var_hcl: 'HCL', tf_var_sensitive: 'Sensitive', tf_var_desc: 'Description', tf_var_key_required: 'Key is required.',
      tf_var_keep: 'Leave blank to keep the stored value', tf_var_saved: 'Variable saved', tf_var_deleted: 'Variable deleted',
      tf_var_delete_msg: 'Delete variable "{k}"?', tf_var_none: 'No variables yet.', tf_var_origin: 'Origin',
      tf_var_overridden: 'Overridden', tf_var_flags: 'Flags', tf_var_search: 'Search key or description…',
      tf_var_mode_direct: 'Workspace variables', tf_var_mode_eff: 'Effective', tf_var_all_cats: 'All categories',
      tf_var_eff_hint: 'Effective shows what Terraform receives at run time: global sets, linked sets, linked outputs and workspace variables.',
      tf_vd_btn: 'Detect from Configuration', tf_vd_title: 'Detect variables from configuration',
      tf_vd_hint: 'Variables declared in the configuration. Tick the ones to add and set their values.',
      tf_vd_empty: 'No variable blocks found in the configuration.', tf_vd_inline_only: 'Detection works for inline workspaces only.',
      tf_vd_none: 'Select at least one variable.', tf_vd_added: 'Added {n}, failed {f}', tf_vd_exists: 'exists',
      tf_vd_no_default: 'no default', tf_vd_add: 'Add selected',
      tf_lk_title: 'Linked Outputs', tf_lk_hint: 'Feed another workspace\'s output into a Terraform variable. The value is resolved when a run starts.',
      tf_lk_empty: 'No linked outputs yet.', tf_lk_no_sources: 'No other workspace has been applied yet, so there are no outputs to link.',
      tf_lk_add: 'Add link', tf_lk_save: 'Save links', tf_lk_saved: 'Linked outputs saved', tf_lk_var: 'Target variable',
      tf_lk_source: 'Source workspace', tf_lk_output: 'Output', tf_lk_remove: 'Remove link', tf_lk_sensitive: 'sensitive',
      tf_lk_available: 'Available', tf_lk_sensitive_state: 'Sensitive', tf_lk_unavailable: 'Unavailable',
      tf_lk_overridden: 'Overridden by explicit var', tf_lk_var_bad: 'Variable names must be valid Terraform identifiers and unique.',
      tf_lk_pick: 'Pick a source workspace and output for every link.',
      tf_ca_title: 'Cloud Accounts', tf_ca_hint: 'Credentials used by workspaces and variable sets. Secrets are stored encrypted and never shown again.',
      tf_ca_search: 'Search cloud accounts…', tf_ca_all_providers: 'All providers', tf_ca_new: 'New Cloud Account',
      tf_ca_add_first: 'Add your first cloud account', tf_ca_empty_title: 'No cloud accounts yet',
      tf_ca_empty_desc: 'Add a provider account so workspaces can authenticate to it.', tf_ca_loading: 'Loading…',
      tf_ca_unverified: 'Unverified', tf_ca_failed: 'Verification failed', tf_ca_verified: 'Verified {when}',
      tf_ca_used_0: 'Not used by any workspace', tf_ca_used_1: 'Used by 1 workspace', tf_ca_used_n: 'Used by {n} workspaces',
      tf_ca_secret_set: 'Set', tf_ca_secret_unset: 'Not set', tf_ca_test: 'Test', tf_ca_testing: 'Testing…',
      tf_ca_edit_title: 'Edit Cloud Account', tf_ca_new_title: 'New Cloud Account', tf_ca_provider: 'Provider', tf_ca_name: 'Name',
      tf_ca_desc: 'Description', tf_ca_name_required: 'Name is required.', tf_ca_field_required: '{field} is required.',
      tf_ca_keep: 'Leave blank to keep the stored value', tf_ca_import: 'Import credentials from an existing account',
      tf_ca_import_manual: 'Enter credentials manually', tf_ca_import_unusable: '(missing credentials)',
      tf_ca_import_note: 'The credentials are copied from the selected account on the server. Nothing is shown here.',
      tf_ca_saved: 'Cloud account saved', tf_ca_deleted: 'Cloud account deleted', tf_ca_del_title: 'Delete cloud account',
      tf_ca_del_msg: 'Delete cloud account "{name}"?', tf_ca_del_msg_ws: 'It is used by {n} workspace(s); they will lose its credentials.',
      tf_ca_test_ok: 'Connection OK ({id})', tf_ca_test_fail: 'Connection failed: {err}', tf_ca_new_btn: 'New Cloud Account',
      tf_ca_save: 'Save', tf_ca_tested_ok: 'Verified',
    },
    id: {
      tf_tab_variables: 'Variabel & Cloud', tf_ws_tab_variables: 'Variabel',
      tf_seg_label: 'Variabel dan akun cloud',
      tf_vs_title: 'Set Variabel', tf_vs_search: 'Cari set variabel…', tf_vs_new: 'Set Variabel Baru', tf_vs_edit: 'Ubah Set Variabel',
      tf_vs_empty: 'Belum ada set variabel. Set membagikan variabel ke beberapa workspace.',
      tf_vs_filter_all: 'Semua', tf_vs_filter_global: 'Global', tf_vs_filter_scoped: 'Per workspace',
      tf_vs_name: 'Nama', tf_vs_desc: 'Deskripsi', tf_vs_global: 'Global (semua workspace)', tf_vs_global_hint: 'Terapkan otomatis ke setiap workspace',
      tf_vs_workspaces: 'Workspace tertaut', tf_vs_ws_search: 'Filter workspace…', tf_vs_vars: 'Variabel',
      tf_vs_save_first: 'Simpan set terlebih dahulu, lalu tambahkan variabel.', tf_vs_name_required: 'Nama wajib diisi.',
      tf_vs_saved: 'Set variabel disimpan', tf_vs_deleted: 'Set variabel dihapus',
      tf_vs_delete_title: 'Hapus set variabel', tf_vs_delete_msg: 'Hapus set variabel "{name}"? Workspace yang tertaut akan kehilangan variabelnya.',
      tf_vs_all_ws: 'Semua workspace', tf_vs_n_ws: '{n} workspace', tf_vs_n_vars: '{n} variabel', tf_vs_no_ws: 'Belum ada workspace',
      tf_var_add: 'Tambah Variabel', tf_var_edit: 'Ubah Variabel', tf_var_key: 'Kunci', tf_var_value: 'Nilai', tf_var_category: 'Kategori',
      tf_var_hcl: 'HCL', tf_var_sensitive: 'Rahasia', tf_var_desc: 'Deskripsi', tf_var_key_required: 'Kunci wajib diisi.',
      tf_var_keep: 'Kosongkan untuk mempertahankan nilai tersimpan', tf_var_saved: 'Variabel disimpan', tf_var_deleted: 'Variabel dihapus',
      tf_var_delete_msg: 'Hapus variabel "{k}"?', tf_var_none: 'Belum ada variabel.', tf_var_origin: 'Asal',
      tf_var_overridden: 'Ditimpa', tf_var_flags: 'Penanda', tf_var_search: 'Cari kunci atau deskripsi…',
      tf_var_mode_direct: 'Variabel workspace', tf_var_mode_eff: 'Efektif', tf_var_all_cats: 'Semua kategori',
      tf_var_eff_hint: 'Efektif menampilkan apa yang diterima Terraform saat run: set global, set tertaut, linked output, dan variabel workspace.',
      tf_vd_btn: 'Deteksi dari Konfigurasi', tf_vd_title: 'Deteksi variabel dari konfigurasi',
      tf_vd_hint: 'Variabel yang dideklarasikan di konfigurasi. Centang yang ingin ditambahkan dan atur nilainya.',
      tf_vd_empty: 'Tidak ada blok variable di konfigurasi.', tf_vd_inline_only: 'Deteksi hanya tersedia untuk workspace inline.',
      tf_vd_none: 'Pilih minimal satu variabel.', tf_vd_added: 'Berhasil {n}, gagal {f}', tf_vd_exists: 'sudah ada',
      tf_vd_no_default: 'tanpa default', tf_vd_add: 'Tambahkan yang dipilih',
      tf_lk_title: 'Linked Outputs', tf_lk_hint: 'Teruskan output workspace lain ke variabel Terraform. Nilainya diselesaikan saat run dimulai.',
      tf_lk_empty: 'Belum ada linked output.', tf_lk_no_sources: 'Belum ada workspace lain yang diterapkan, jadi tidak ada output untuk ditautkan.',
      tf_lk_add: 'Tambah tautan', tf_lk_save: 'Simpan tautan', tf_lk_saved: 'Linked output disimpan', tf_lk_var: 'Variabel tujuan',
      tf_lk_source: 'Workspace sumber', tf_lk_output: 'Output', tf_lk_remove: 'Hapus tautan', tf_lk_sensitive: 'rahasia',
      tf_lk_available: 'Tersedia', tf_lk_sensitive_state: 'Rahasia', tf_lk_unavailable: 'Tidak tersedia',
      tf_lk_overridden: 'Ditimpa variabel eksplisit', tf_lk_var_bad: 'Nama variabel harus identifier Terraform yang valid dan unik.',
      tf_lk_pick: 'Pilih workspace sumber dan output untuk setiap tautan.',
      tf_ca_title: 'Akun Cloud', tf_ca_hint: 'Kredensial yang dipakai workspace dan set variabel. Rahasia disimpan terenkripsi dan tidak ditampilkan lagi.',
      tf_ca_search: 'Cari akun cloud…', tf_ca_all_providers: 'Semua provider', tf_ca_new: 'Akun Cloud Baru',
      tf_ca_add_first: 'Tambahkan akun cloud pertama', tf_ca_empty_title: 'Belum ada akun cloud',
      tf_ca_empty_desc: 'Tambahkan akun provider agar workspace dapat terautentikasi.', tf_ca_loading: 'Memuat…',
      tf_ca_unverified: 'Belum diverifikasi', tf_ca_failed: 'Verifikasi gagal', tf_ca_verified: 'Terverifikasi {when}',
      tf_ca_used_0: 'Tidak dipakai workspace mana pun', tf_ca_used_1: 'Dipakai 1 workspace', tf_ca_used_n: 'Dipakai {n} workspace',
      tf_ca_secret_set: 'Terisi', tf_ca_secret_unset: 'Belum diisi', tf_ca_test: 'Uji', tf_ca_testing: 'Menguji…',
      tf_ca_edit_title: 'Ubah Akun Cloud', tf_ca_new_title: 'Akun Cloud Baru', tf_ca_provider: 'Provider', tf_ca_name: 'Nama',
      tf_ca_desc: 'Deskripsi', tf_ca_name_required: 'Nama wajib diisi.', tf_ca_field_required: '{field} wajib diisi.',
      tf_ca_keep: 'Kosongkan untuk mempertahankan nilai tersimpan', tf_ca_import: 'Impor kredensial dari akun yang ada',
      tf_ca_import_manual: 'Isi kredensial secara manual', tf_ca_import_unusable: '(kredensial tidak lengkap)',
      tf_ca_import_note: 'Kredensial disalin dari akun yang dipilih di server. Tidak ditampilkan di sini.',
      tf_ca_saved: 'Akun cloud disimpan', tf_ca_deleted: 'Akun cloud dihapus', tf_ca_del_title: 'Hapus akun cloud',
      tf_ca_del_msg: 'Hapus akun cloud "{name}"?', tf_ca_del_msg_ws: 'Dipakai {n} workspace; mereka akan kehilangan kredensialnya.',
      tf_ca_test_ok: 'Koneksi OK ({id})', tf_ca_test_fail: 'Koneksi gagal: {err}', tf_ca_new_btn: 'Akun Cloud Baru',
      tf_ca_save: 'Simpan', tf_ca_tested_ok: 'Terverifikasi',
    },
  });

  /* ---------- shared helpers ---------- */
  const fld = (label, inner, wide) => '<div class="tf-field' + (wide ? ' tf-v-span2' : '') + '"><span class="tf-field-label">' + e(label) + '</span>' + inner + '</div>';
  const q = (root, id) => root.querySelector('#' + id);
  const errText = Tf.errMsg;
  const cancelBtn = () => '<button type="button" class="btn btn-sm" data-dlg-close>' + e(t('cancel', 'Cancel')) + '</button>';

  // Variables live under a workspace or a set. Paths are the same for both, only the prefix differs.
  function varBase(scope) {
    return scope.kind === 'ws' ? '/api/terraform/workspaces/' + scope.id + '/variables' : '/api/terraform/varsets/' + scope.id + '/variables';
  }

  // Renders a variable table into box. Values of sensitive variables are never written to the DOM.
  // opts: {id, eff, editable(v)}. Stores the list on box._vars for the action handler.
  function varTableHTML(box, vars, opts) {
    box._vars = vars;
    const wsKeys = opts.wsKeys || new Set(vars.filter(v => v.origin === 'workspace' || (!v.origin && !opts.eff)).map(v => v.key));
    if (!vars.length) return Tf.ui.empty({ icon: 'braces', desc: t('tf_var_none', 'No variables yet.') });
    return '<div class="tf-v-tablewrap"><table class="tf-v-table"' + (opts.id ? ' id="' + opts.id + '"' : '') + '><thead><tr>' +
      '<th>' + e(t('tf_var_key', 'Key')) + '</th><th>' + e(t('tf_var_value', 'Value')) + '</th><th>' + e(t('tf_var_category', 'Category')) + '</th>' +
      '<th>' + e(t('tf_var_flags', 'Flags')) + '</th>' + (opts.eff ? '<th>' + e(t('tf_var_origin', 'Origin')) + '</th>' : '') + '<th></th></tr></thead><tbody>' +
      vars.map(function (v) {
        const editable = opts.editable ? opts.editable(v) : true;
        const shown = v.sensitive ? (v.value_set ? '••••••••' : '') : (v.value || '');
        const over = opts.eff && v.origin !== 'workspace' && wsKeys.has(v.key);
        const flags = (v.hcl ? '<span class="badge badge-subtle">HCL</span> ' : '') + (v.sensitive ? '<span class="badge badge-subtle">' + e(t('tf_var_sensitive', 'Sensitive')) + '</span>' : '');
        return '<tr><td class="mono">' + e(v.key) + (v.description ? '<div class="tf-muted tf-v-desc">' + e(v.description) + '</div>' : '') + '</td>' +
          '<td class="mono tf-v-val">' + e(shown) + '</td><td>' + e(v.category || 'terraform') + '</td><td>' + flags + '</td>' +
          (opts.eff ? '<td>' + e(v.origin || '') + (over ? ' <span class="badge badge-warning">' + e(t('tf_var_overridden', 'Overridden')) + '</span>' : '') + '</td>' : '') +
          '<td class="tf-v-acts">' + (editable
            ? '<button type="button" class="btn btn-icon btn-sm" data-var-edit="' + v.id + '" aria-label="' + e(t('tf_var_edit', 'Edit Variable')) + '">' + ic('pencil', 'icon-xs') + '</button> ' +
              '<button type="button" class="btn btn-icon btn-sm" data-var-del="' + v.id + '" aria-label="' + e(t('delete', 'Delete')) + '">' + ic('trash-2', 'icon-xs') + '</button>'
            : '') + '</td></tr>';
      }).join('') + '</tbody></table></div>';
  }

  // Wires edit/delete clicks of a table rendered by varTableHTML into box.
  function bindVarTable(box, scope, reload) {
    box.onclick = function (ev) {
      const ed = ev.target.closest('[data-var-edit]'), dl = ev.target.closest('[data-var-del]');
      if (ed) { const v = (box._vars || []).find(x => x.id === Number(ed.dataset.varEdit)); if (v) openVar(scope, v, reload); }
      else if (dl) delVar(scope, Number(dl.dataset.varDel), reload);
    };
  }

  // Dialog for creating or editing one variable. Sensitive values: blank on edit keeps the stored value (server rule).
  function openVar(scope, v, onDone) {
    const body = '<div class="tf-grid-2">' +
      fld(t('tf_var_key', 'Key') + ' *', '<input id="tfVarKey" class="form-input mono" maxlength="64" spellcheck="false" value="' + e(v ? v.key : '') + '">') +
      fld(t('tf_var_category', 'Category'), '<select id="tfVarCat" class="form-input"><option value="terraform"' + (!v || v.category !== 'env' ? ' selected' : '') + '>terraform</option><option value="env"' + (v && v.category === 'env' ? ' selected' : '') + '>env</option></select>') +
      fld(t('tf_var_value', 'Value'), '<textarea id="tfVarVal" class="form-input mono" rows="4" spellcheck="false" placeholder="' + e(v && v.sensitive ? t('tf_var_keep', '') : '') + '"' +
        (v && v.sensitive ? ' style="-webkit-text-security:disc;"' : '') + '>' + e(v && !v.sensitive ? v.value || '' : '') + '</textarea>', true) +
      '<div class="tf-v-flags tf-v-span2"><label class="tf-check"><input type="checkbox" id="tfVarHcl"' + (v && v.hcl ? ' checked' : '') + '> ' + e(t('tf_var_hcl', 'HCL')) + '</label>' +
      '<label class="tf-check"><input type="checkbox" id="tfVarSens"' + (v && v.sensitive ? ' checked' : '') + '> ' + e(t('tf_var_sensitive', 'Sensitive')) + '</label></div>' +
      fld(t('tf_var_desc', 'Description'), '<input id="tfVarDesc" class="form-input" maxlength="500" value="' + e(v ? v.description || '' : '') + '">', true) + '</div>';
    const dlg = Tf.dlg.open({
      title: v ? t('tf_var_edit', 'Edit Variable') : t('tf_var_add', 'Add Variable'), body: body, width: 600,
      footer: cancelBtn() + '<button type="button" id="tfVarSave" class="btn btn-sm btn-primary">' + e(t('save', 'Save')) + '</button>',
      onMount: function (el, api) {
        const cat = q(el, 'tfVarCat'), hcl = q(el, 'tfVarHcl'), sens = q(el, 'tfVarSens'), val = q(el, 'tfVarVal');
        const syncCat = function () { const env = cat.value === 'env'; hcl.disabled = env; if (env) hcl.checked = false; };
        cat.addEventListener('change', syncCat);
        sens.addEventListener('change', function () { val.style.webkitTextSecurity = sens.checked ? 'disc' : 'none'; });
        syncCat();
        q(el, 'tfVarSave').addEventListener('click', function () { saveVar(scope, v, api, onDone); });
      },
    });
    return dlg;
  }

  async function saveVar(scope, v, api, onDone) {
    const key = q(api.root, 'tfVarKey').value.trim();
    if (!key) { api.error(t('tf_var_key_required', 'Key is required.')); return; }
    const cat = q(api.root, 'tfVarCat').value;
    const body = {
      key: key, value: q(api.root, 'tfVarVal').value, category: cat,
      hcl: cat === 'env' ? false : q(api.root, 'tfVarHcl').checked,
      sensitive: q(api.root, 'tfVarSens').checked, description: q(api.root, 'tfVarDesc').value,
    };
    try {
      if (v) await Tf.api.put(varBase(scope) + '/' + v.id, body);
      else await Tf.api.post(varBase(scope), body);
    } catch (err) { api.error(errText(err)); return; }
    api.close();
    Tf.toast(t('tf_var_saved', 'Variable saved'), 'success');
    if (onDone) onDone();
  }

  async function delVar(scope, id, onDone) {
    const ok = await Tf.confirm({ title: t('delete', 'Delete'), message: t('tf_var_delete_msg', 'Delete variable "{k}"?', { k: '#' + id }), confirmText: t('delete', 'Delete'), danger: true });
    if (!ok) return;
    try { await Tf.api.del(varBase(scope) + '/' + id); } catch (err) { Tf.toast(errText(err), 'error'); return; }
    Tf.toast(t('tf_var_deleted', 'Variable deleted'), 'success');
    if (onDone) onDone();
  }

  /* ---------- top tab: Variables & Cloud ---------- */
  let sets = [];
  let cloud = { accounts: [], providers: [] };

  function segBtn(id, label, icon, on) {
    return '<button type="button" class="tf-v-seg-btn' + (on ? ' active' : '') + '" role="tab" aria-selected="' + on + '" data-seg="' + id + '">' + ic(icon, 'icon-sm') + ' <span>' + e(label) + '</span></button>';
  }

  function showSeg(panel, seg) {
    Tf.state.sub.variables = seg;
    panel.querySelectorAll('[data-seg]').forEach(function (b) {
      const on = b.getAttribute('data-seg') === seg;
      b.classList.toggle('active', on);
      b.setAttribute('aria-selected', String(on));
    });
    const body = panel.querySelector('#tfVarSeg');
    if (seg === 'cloud') buildCloud(body); else buildSets(body);
  }

  Tf.registerTab({
    id: 'variables', icon: 'sliders-horizontal', labelKey: 'tf_tab_variables', fallback: 'Variables & Cloud', order: 3,
    render: function (panel) {
      panel.innerHTML = '<div class="tf-v-seg" role="tablist" aria-label="' + e(t('tf_seg_label', 'Variables and cloud accounts')) + '">' +
        segBtn('sets', t('tf_vs_title', 'Variable Sets'), 'sliders-horizontal', false) + segBtn('cloud', t('tf_ca_title', 'Cloud Accounts'), 'cloud', false) +
        '</div><div id="tfVarSeg"></div>';
      panel.onclick = function (ev) {
        const b = ev.target.closest('[data-seg]');
        if (b) showSeg(panel, b.getAttribute('data-seg'));
      };
    },
    onShow: function (panel) { showSeg(panel, Tf.state.sub.variables === 'cloud' ? 'cloud' : 'sets'); },
  });

  /* variable sets */
  function buildSets(body) {
    body.innerHTML = '<div class="tf-v-bar">' +
      '<input type="search" id="tfVsSearch" class="form-input" placeholder="' + e(t('tf_vs_search', 'Search variable sets…')) + '" aria-label="' + e(t('tf_vs_search', 'Search variable sets…')) + '">' +
      '<select id="tfVsFilter" class="form-input" aria-label="' + e(t('tf_vs_title', 'Variable Sets')) + '">' +
      '<option value="all">' + e(t('tf_vs_filter_all', 'All')) + '</option><option value="global">' + e(t('tf_vs_filter_global', 'Global')) + '</option>' +
      '<option value="scoped">' + e(t('tf_vs_filter_scoped', 'Workspace-scoped')) + '</option></select>' +
      '<span class="tf-v-spacer"></span>' +
      '<button type="button" id="btnTfNewVarset" class="btn btn-sm btn-primary" data-vs-new>' + ic('plus', 'icon-sm') + ' ' + e(t('tf_vs_new', 'New Variable Set')) + '</button></div>' +
      '<div id="tfVarsetsList" class="tf-v-cards">' + Tf.ui.skeleton(3) + '</div>';
    body.onclick = function (ev) {
      if (ev.target.closest('[data-vs-new]')) return openVarset(0);
      const ed = ev.target.closest('[data-vs-edit]'), dl = ev.target.closest('[data-vs-del]'), rt = ev.target.closest('[data-vs-retry]');
      if (ed) return openVarset(Number(ed.dataset.vsEdit));
      if (dl) return delVarset(Number(dl.dataset.vsDel));
      if (rt) return loadSets();
    };
    body.oninput = renderSets;
    body.onchange = renderSets;
    loadSets();
  }

  async function loadSets() {
    const list = document.getElementById('tfVarsetsList');
    try { sets = (await Tf.api.get('/api/terraform/varsets')).varsets || []; }
    catch (err) { if (list) list.innerHTML = Tf.ui.error(errText(err), 'data-vs-retry'); return; }
    Tf.state.varsets = sets;
    Tf.emit('varsets', sets);
    renderSets();
  }

  function renderSets() {
    const list = document.getElementById('tfVarsetsList');
    if (!list) return;
    const term = (q0(document, 'tfVsSearch') || '').toLowerCase(), f = q0(document, 'tfVsFilter') || 'all';
    const shown = sets.filter(function (s) {
      if (f === 'global' && !s.global) return false;
      if (f === 'scoped' && s.global) return false;
      return !term || (s.name + ' ' + (s.description || '')).toLowerCase().indexOf(term) >= 0;
    });
    if (!sets.length) { list.innerHTML = Tf.ui.empty({ icon: 'sliders-horizontal', desc: t('tf_vs_empty', '') }); return; }
    if (!shown.length) { list.innerHTML = Tf.ui.empty({ icon: 'search', desc: t('tf_vs_empty', '') }); return; }
    list.innerHTML = shown.map(function (s) {
      const scope = s.global ? t('tf_vs_all_ws', 'All workspaces') : t('tf_vs_n_ws', '{n} workspaces', { n: (s.workspace_ids || []).length });
      return '<div class="card tf-v-card"><div class="tf-v-card-main"><div class="tf-v-card-name"><b>' + e(s.name) + '</b>' +
        (s.global ? ' <span class="badge badge-healthy">' + e(t('tf_vs_filter_global', 'Global')) + '</span>' : '') + '</div>' +
        (s.description ? '<div class="tf-muted tf-v-desc">' + e(s.description) + '</div>' : '') +
        '<div class="tf-muted tf-v-desc">' + e(t('tf_vs_n_vars', '{n} variables', { n: s.variable_count || 0 })) + ' · ' + e(scope) + '</div></div>' +
        '<div class="tf-row"><button type="button" class="btn btn-sm" data-vs-edit="' + s.id + '">' + ic('pencil', 'icon-xs') + ' ' + e(t('edit', 'Edit')) + '</button>' +
        '<button type="button" class="btn btn-icon btn-sm" data-vs-del="' + s.id + '" aria-label="' + e(t('delete', 'Delete')) + '">' + ic('trash-2', 'icon-xs') + '</button></div></div>';
    }).join('');
  }

  function q0(root, id) { const el = root.getElementById ? root.getElementById(id) : null; return el ? el.value : ''; }

  // Variable set editor: metadata, linked workspaces and its own variables (after the first save).
  async function openVarset(id) {
    let s = null, wss = [];
    try {
      if (id) s = (await Tf.api.get('/api/terraform/varsets/' + id)).varset || null;
      wss = (await Tf.api.get('/api/terraform/workspaces')).workspaces || [];
    } catch (err) { Tf.toast(errText(err), 'error'); return; }
    if (id && !s) return;
    const linked = new Set((s && s.workspace_ids) || []);
    const picker = wss.length
      ? '<input type="search" id="tfVsWsSearch" class="form-input" placeholder="' + e(t('tf_vs_ws_search', 'Filter workspaces…')) + '" aria-label="' + e(t('tf_vs_ws_search', 'Filter workspaces…')) + '">' +
        '<div class="tf-ws-picker">' + wss.map(function (w) {
          return '<label class="tf-check tf-ws-opt" data-ws-name="' + e(w.name.toLowerCase()) + '"><input type="checkbox" data-vs-ws value="' + w.id + '"' + (linked.has(w.id) ? ' checked' : '') + '> ' + e(w.name) + '</label>';
        }).join('') + '</div>'
      : '<div class="tf-hint">' + e(t('tf_vs_no_ws', 'No workspaces yet')) + '</div>';
    const body = '<div class="tf-grid-2">' +
      fld(t('tf_vs_name', 'Name') + ' *', '<input id="tfVsName" class="form-input" maxlength="100" value="' + e(s ? s.name : '') + '">') +
      fld(t('tf_vs_global', 'Global (all workspaces)'), '<label class="tf-check"><input type="checkbox" id="tfVsGlobal"' + (s && s.global ? ' checked' : '') + '> ' + e(t('tf_vs_global_hint', '')) + '</label>') +
      fld(t('tf_vs_desc', 'Description'), '<input id="tfVsDesc" class="form-input" maxlength="500" value="' + e(s ? s.description || '' : '') + '">', true) +
      fld(t('tf_vs_workspaces', 'Linked workspaces'), picker, true) +
      '<div class="tf-field tf-v-span2"><div class="tf-row tf-v-between"><span class="tf-field-label">' + e(t('tf_vs_vars', 'Variables')) + '</span>' +
      (id ? '<button type="button" class="btn btn-sm" data-vsv-add>' + ic('plus', 'icon-xs') + ' ' + e(t('tf_var_add', 'Add Variable')) + '</button>' : '') + '</div>' +
      '<div id="tfVsVarsTable">' + (id ? '' : '<div class="tf-hint">' + e(t('tf_vs_save_first', '')) + '</div>') + '</div></div></div>';
    const dlg = Tf.dlg.open({
      title: id ? t('tf_vs_edit', 'Edit Variable Set') : t('tf_vs_new', 'New Variable Set'), body: body, width: 760,
      footer: cancelBtn() + '<button type="button" id="tfVsSave" class="btn btn-sm btn-primary">' + e(t('save', 'Save')) + '</button>',
      onMount: function (el, api) {
        el.addEventListener('click', function (ev) {
          if (ev.target.closest('#tfVsSave')) return saveVarset(id, api);
          if (ev.target.closest('[data-vsv-add]')) return openVar({ kind: 'varset', id: id }, null, function () { loadVsVars(id, api); });
        });
        el.addEventListener('input', function (ev) {
          if (ev.target.id !== 'tfVsWsSearch') return;
          const term = ev.target.value.toLowerCase();
          el.querySelectorAll('.tf-ws-opt').forEach(function (l) { l.hidden = term && l.getAttribute('data-ws-name').indexOf(term) < 0; });
        });
      },
    });
    if (id) loadVsVars(id, dlg);
  }

  async function loadVsVars(id, api) {
    const box = q(api.root, 'tfVsVarsTable');
    if (!box) return;
    const scope = { kind: 'varset', id: id };
    try {
      const vars = (await Tf.api.get(varBase(scope))).variables || [];
      box.innerHTML = varTableHTML(box, vars, {});
      bindVarTable(box, scope, function () { loadVsVars(id, api); });
    } catch (err) { box.innerHTML = Tf.ui.error(errText(err)); }
  }

  async function saveVarset(id, api) {
    const name = q(api.root, 'tfVsName').value.trim();
    if (!name) { api.error(t('tf_vs_name_required', 'Name is required.')); return; }
    const body = { name: name, description: q(api.root, 'tfVsDesc').value, global: q(api.root, 'tfVsGlobal').checked };
    const ids = Array.prototype.map.call(api.root.querySelectorAll('[data-vs-ws]:checked'), function (c) { return Number(c.value); });
    try {
      const res = id ? await Tf.api.put('/api/terraform/varsets/' + id, body) : await Tf.api.post('/api/terraform/varsets', body);
      const sid = id || (res.varset || res).id;
      await Tf.api.put('/api/terraform/varsets/' + sid + '/workspaces', { workspace_ids: ids });
      api.close();
      Tf.toast(t('tf_vs_saved', 'Variable set saved'), 'success');
      await loadSets();
      if (!id) openVarset(sid); // reopen so the variable section appears
    } catch (err) { api.error(errText(err)); }
  }

  async function delVarset(id) {
    const s = sets.find(function (x) { return x.id === id; });
    const ok = await Tf.confirm({ title: t('tf_vs_delete_title', 'Delete variable set'), message: t('tf_vs_delete_msg', '', { name: s ? s.name : '#' + id }), confirmText: t('delete', 'Delete'), danger: true });
    if (!ok) return;
    try { await Tf.api.del('/api/terraform/varsets/' + id); } catch (err) { Tf.toast(errText(err), 'error'); return; }
    Tf.toast(t('tf_vs_deleted', 'Variable set deleted'), 'success');
    await loadSets();
  }

  /* cloud accounts */
  function providerBy(id) { return cloud.providers.find(function (p) { return p.id === id; }); }

  function buildCloud(body) {
    body.innerHTML = '<div class="tf-v-bar">' +
      '<input type="search" id="tfCaSearch" class="form-input" placeholder="' + e(t('tf_ca_search', 'Search cloud accounts…')) + '" aria-label="' + e(t('tf_ca_search', 'Search cloud accounts…')) + '">' +
      '<select id="tfCaProviderFilter" class="form-input" aria-label="' + e(t('tf_ca_provider', 'Provider')) + '"><option value="">' + e(t('tf_ca_all_providers', 'All providers')) + '</option></select>' +
      '<span class="tf-v-spacer"></span>' +
      '<button type="button" id="btnTfCloudNew" class="btn btn-sm btn-primary" data-ca-new>' + ic('plus', 'icon-sm') + ' ' + e(t('tf_ca_new', 'New Cloud Account')) + '</button></div>' +
      '<div id="tfCloudRoot">' + Tf.ui.skeleton(2) + '</div>';
    body.onclick = onCloudClick;
    body.oninput = renderCloud;
    body.onchange = renderCloud;
    loadCloud();
  }

  async function loadCloud() {
    const root = document.getElementById('tfCloudRoot');
    try {
      const r = await Tf.api.get('/api/terraform/cloud-accounts');
      cloud = { accounts: r.accounts || [], providers: r.providers || [] };
    } catch (err) { if (root) root.innerHTML = Tf.ui.error(errText(err), 'data-ca-retry'); return; }
    Tf.state.cloud = cloud.accounts;
    Tf.emit('cloud', cloud.accounts);
    const sel = document.getElementById('tfCaProviderFilter');
    if (sel && sel.options.length === 1) cloud.providers.forEach(function (p) { sel.add(new Option(p.label, p.id)); });
    renderCloud();
  }

  function verifyChip(a) {
    if (!a.verified_at) return '<span class="badge badge-unknown">' + e(t('tf_ca_unverified', 'Unverified')) + '</span>';
    if (!a.verify_ok) return '<span class="badge badge-danger" title="' + e(a.verify_message || '') + '">' + e(t('tf_ca_failed', 'Verification failed')) + '</span>';
    return '<span class="badge badge-healthy" title="' + e(a.verify_message || '') + '">' + e(t('tf_ca_verified', 'Verified {when}', { when: Tf.fmt.ago(a.verified_at) })) + '</span>';
  }

  function cardHTML(a) {
    const p = providerBy(a.provider);
    const secrets = a.secrets_set || {};
    const kv = ((p && p.fields) || []).map(function (f) {
      if (f.secret) return '<div><dt>' + e(f.label) + '</dt><dd class="tf-ca-secret">' + e(secrets[f.key] ? t('tf_ca_secret_set', 'Set') : t('tf_ca_secret_unset', 'Not set')) + '</dd></div>';
      const v = (a.fields || {})[f.key];
      return v ? '<div><dt>' + e(f.label) + '</dt><dd class="mono">' + e(v) + '</dd></div>' : '';
    }).join('');
    const n = a.workspace_count || 0;
    const used = n === 0 ? t('tf_ca_used_0', '') : n === 1 ? t('tf_ca_used_1', '') : t('tf_ca_used_n', '{n} workspaces', { n: n });
    return '<div class="card tf-ca-card">' +
      '<div class="tf-ca-head"><span class="tf-ca-badge tf-ca-' + e(String(a.provider).replace(/[^a-z0-9]/g, '')) + '">' + e(p ? p.label : a.provider) + '</span>' + verifyChip(a) + '</div>' +
      '<div class="tf-ca-name" title="' + e(a.name) + '">' + e(a.name) + '</div>' +
      (a.description ? '<div class="tf-muted tf-v-desc">' + e(a.description) + '</div>' : '') +
      '<div class="tf-muted tf-v-desc">' + e(used) + '</div>' +
      '<dl class="tf-ca-kv">' + kv + '</dl>' +
      '<div class="tf-row">' +
      '<button type="button" class="btn btn-sm" data-ca-test="' + a.id + '">' + ic('shield-check', 'icon-xs') + ' ' + e(t('tf_ca_test', 'Test')) + '</button>' +
      '<button type="button" class="btn btn-sm" data-ca-edit="' + a.id + '">' + ic('pencil', 'icon-xs') + ' ' + e(t('edit', 'Edit')) + '</button>' +
      '<button type="button" class="btn btn-icon btn-sm" data-ca-del="' + a.id + '" aria-label="' + e(t('delete', 'Delete')) + '">' + ic('trash-2', 'icon-xs') + '</button></div></div>';
  }

  function renderCloud() {
    const root = document.getElementById('tfCloudRoot');
    if (!root) return;
    if (!cloud.accounts.length) {
      root.innerHTML = '<div class="tf-v-empty">' + ic('cloud', 'icon') + '<div class="tf-ca-empty-title">' + e(t('tf_ca_empty_title', 'No cloud accounts yet')) + '</div>' +
        '<div class="tf-muted tf-v-desc">' + e(t('tf_ca_empty_desc', '')) + '</div>' +
        '<button type="button" class="btn btn-sm btn-primary" data-ca-new>' + e(t('tf_ca_add_first', 'Add your first cloud account')) + '</button></div>';
      return;
    }
    const term = (q0(document, 'tfCaSearch') || '').toLowerCase(), prov = q0(document, 'tfCaProviderFilter') || '';
    const shown = cloud.accounts.filter(function (a) {
      if (prov && a.provider !== prov) return false;
      const p = providerBy(a.provider);
      return !term || (a.name + ' ' + (a.description || '') + ' ' + (p ? p.label : '')).toLowerCase().indexOf(term) >= 0;
    });
    root.innerHTML = shown.length ? '<div class="tf-v-cards">' + shown.map(cardHTML).join('') + '</div>' : Tf.ui.empty({ icon: 'search', desc: '' });
  }

  function onCloudClick(ev) {
    const b = ev.target.closest('button');
    if (!b) return;
    if (b.hasAttribute('data-ca-new')) return openCloud(0);
    if (b.hasAttribute('data-ca-retry')) return loadCloud();
    if (b.dataset.caTest) return testCloud(Number(b.dataset.caTest), b);
    if (b.dataset.caEdit) return openCloud(Number(b.dataset.caEdit));
    if (b.dataset.caDel) return delCloud(Number(b.dataset.caDel));
  }

  async function testCloud(id, btn) {
    if (btn) { btn.disabled = true; btn.textContent = t('tf_ca_testing', 'Testing…'); }
    try {
      const r = await Tf.api.post('/api/terraform/cloud-accounts/' + id + '/test');
      if (r.ok) Tf.toast(t('tf_ca_test_ok', 'Connection OK ({id})', { id: r.identity || 'ok' }), 'success');
      else Tf.toast(t('tf_ca_test_fail', 'Connection failed: {err}', { err: r.error || 'error' }), 'error');
    } catch (err) { Tf.toast(t('tf_ca_test_fail', '', { err: errText(err) }), 'error'); }
    await loadCloud();
  }

  async function delCloud(id) {
    const a = cloud.accounts.find(function (x) { return x.id === id; });
    if (!a) return;
    const n = a.workspace_count || 0;
    const message = t('tf_ca_del_msg', 'Delete cloud account "{name}"?', { name: a.name }) + (n > 0 ? ' ' + t('tf_ca_del_msg_ws', '', { n: n }) : '');
    if (!(await Tf.confirm({ title: t('tf_ca_del_title', 'Delete cloud account'), message: message, confirmText: t('delete', 'Delete'), danger: true }))) return;
    try { await Tf.api.del('/api/terraform/cloud-accounts/' + id); } catch (err) { Tf.toast(errText(err), 'error'); return; }
    Tf.toast(t('tf_ca_deleted', 'Cloud account deleted'), 'success');
    await loadCloud();
  }

  // Account editor. Secrets are never sent back to the page: blank on edit keeps the stored value (server rule).
  async function openCloud(id) {
    let r;
    try { r = await Tf.api.get('/api/terraform/cloud-accounts'); } catch (err) { Tf.toast(errText(err), 'error'); return; }
    const provs = r.providers || [];
    const edit = id ? (r.accounts || []).find(function (x) { return x.id === id; }) : null;
    if (id && !edit) return;
    const cur = edit ? edit.provider : (provs[0] ? provs[0].id : '');
    const imp = { list: null };

    const body = '<div class="tf-grid-2">' +
      fld(t('tf_ca_provider', 'Provider'), '<select id="tfCaProvider" class="form-input"' + (edit ? ' disabled' : '') + '>' +
        provs.map(function (p) { return '<option value="' + e(p.id) + '"' + (p.id === cur ? ' selected' : '') + '>' + e(p.label) + '</option>'; }).join('') + '</select>') +
      fld(t('tf_ca_name', 'Name') + ' *', '<input id="tfCaName" class="form-input" maxlength="100" value="' + e(edit ? edit.name : '') + '">') +
      fld(t('tf_ca_desc', 'Description'), '<input id="tfCaDesc" class="form-input" maxlength="500" value="' + e(edit ? edit.description || '' : '') + '">', true) +
      '<div id="tfCaImportWrap" class="tf-v-span2"></div>' +
      '<div id="tfCaFieldWrap" class="tf-v-span2"><div class="tf-grid-2" id="tfCaFields"></div></div></div>';

    function fieldsOf(provId) { return provs.find(function (p) { return p.id === provId; }); }
    function fieldsHTML(provId, vals) {
      const p = fieldsOf(provId);
      if (!p) return '';
      return p.fields.map(function (f) {
        const kept = edit && f.secret && edit.secrets_set && edit.secrets_set[f.key];
        const ph = kept ? t('tf_ca_keep', 'Leave blank to keep the stored value') : (f.placeholder || '');
        const v = vals[f.key] !== undefined ? vals[f.key] : (edit ? '' : (f.default || ''));
        const label = f.label + (f.required ? ' *' : '');
        const id = 'tfCaF_' + f.key;
        const inner = f.multiline
          ? '<textarea id="' + id + '" class="form-input mono" rows="8" spellcheck="false" autocomplete="off" placeholder="' + e(ph) + '">' + e(v) + '</textarea>'
          : '<input id="' + id + '" type="' + (f.secret ? 'password' : 'text') + '" class="form-input" autocomplete="' + (f.secret ? 'new-password' : 'off') + '" placeholder="' + e(ph) + '" value="' + e(v) + '">';
        return fld(label, inner, f.multiline);
      }).join('');
    }
    function collect(root, provId) {
      const p = fieldsOf(provId), vals = {};
      if (p) p.fields.forEach(function (f) { const el = q(root, 'tfCaF_' + f.key); if (el) vals[f.key] = el.value; });
      return vals;
    }

    const dlg = Tf.dlg.open({
      title: edit ? t('tf_ca_edit_title', 'Edit Cloud Account') : t('tf_ca_new_title', 'New Cloud Account'), body: body, width: 620,
      footer: cancelBtn() + '<button type="button" id="tfCaSave" class="btn btn-sm btn-primary">' + e(t('tf_ca_save', 'Save')) + '</button>',
      onMount: function (el, api) {
        const sel = q(el, 'tfCaProvider');
        const fieldsEl = q(el, 'tfCaFields');
        fieldsEl.innerHTML = fieldsHTML(cur, edit ? edit.fields || {} : {});

        // Import (create only, cloudflare/kubernetes): the server copies credentials of an existing Pantau account.
        async function importUI() {
          const wrap = q(el, 'tfCaImportWrap'), fieldWrap = q(el, 'tfCaFieldWrap');
          const prov = sel.value;
          wrap.innerHTML = '';
          fieldWrap.style.display = '';
          if (edit || (prov !== 'cloudflare' && prov !== 'kubernetes')) return;
          if (!imp.list) imp.list = Tf.api.get('/api/terraform/cloud-accounts/importable').catch(function () { return {}; });
          const all = await imp.list;
          const list = all[prov] || [];
          if (!list.length || sel.value !== prov) return;
          wrap.innerHTML = fld(t('tf_ca_import', 'Import credentials from an existing account'),
            '<select id="tfCaImport" class="form-input"><option value="">' + e(t('tf_ca_import_manual', 'Enter credentials manually')) + '</option>' +
            list.map(function (a) { return '<option value="' + a.id + '"' + (a.usable ? '' : ' disabled') + '>' + e(a.name) + (a.usable ? '' : ' ' + t('tf_ca_import_unusable', '')) + '</option>'; }).join('') +
            '</select><div id="tfCaImportNote" class="tf-hint" hidden>' + e(t('tf_ca_import_note', '')) + '</div>', true);
        }
        sel.addEventListener('change', function () {
          const vals = collect(el, sel.value); // keep what was typed for fields the new provider shares
          fieldsEl.innerHTML = fieldsHTML(sel.value, vals);
          importUI();
        });
        el.addEventListener('change', function (ev) {
          if (ev.target.id !== 'tfCaImport') return;
          const on = ev.target.value !== '';
          q(el, 'tfCaFieldWrap').style.display = on ? 'none' : '';
          q(el, 'tfCaImportNote').hidden = !on;
        });
        el.addEventListener('click', function (ev) { if (ev.target.closest('#tfCaSave')) saveCloud(edit, api, collect); });
        importUI();
        q(el, 'tfCaName').focus();
      },
    });
    return dlg;
  }

  async function saveCloud(edit, api, collect) {
    const root = api.root;
    const provider = q(root, 'tfCaProvider').value;
    const name = q(root, 'tfCaName').value.trim();
    if (!name) { api.error(t('tf_ca_name_required', 'Name is required.')); return; }
    const impEl = q(root, 'tfCaImport');
    const impId = impEl ? impEl.value : '';
    const fields = impId ? {} : collect(root, provider);
    if (!impId) {
      const p = (cloud.providers || []).find(function (x) { return x.id === provider; });
      if (p) for (const f of p.fields) {
        const keep = edit && f.secret && edit.secrets_set && edit.secrets_set[f.key];
        if (f.required && !keep && !String(fields[f.key] || '').trim()) { api.error(t('tf_ca_field_required', '{field} is required.', { field: f.label })); return; }
      }
    }
    const payload = { name: name, provider: provider, description: q(root, 'tfCaDesc').value, fields: fields };
    if (impId) payload.import_from = { type: provider, id: Number(impId) };
    const btn = q(root, 'tfCaSave');
    btn.disabled = true;
    let res;
    try {
      res = edit ? await Tf.api.put('/api/terraform/cloud-accounts/' + edit.id, payload) : await Tf.api.post('/api/terraform/cloud-accounts', payload);
    } catch (err) { btn.disabled = false; api.error(errText(err)); return; }
    api.close();
    Tf.toast(t('tf_ca_saved', 'Cloud account saved'), 'success');
    await loadCloud();
    if (!edit && res && res.id) await testCloud(res.id, null);
  }

  /* ---------- detect variables from configuration (used by the config fork) ---------- */
  function vdValue(v) {
    const d = (v.default || '').trim();
    if (!v.has_default) return { val: '', hcl: /^(list|set|map|object|tuple)\b/.test((v.type || '').trim()) };
    const m = d.match(/^"([^"\\$%]*)"$/);
    if (m) return { val: m[1], hcl: false };
    if (/^(true|false|-?\d+(\.\d+)?)$/.test(d)) return { val: d, hcl: false };
    return { val: d, hcl: true };
  }

  function vdRow(v) {
    const x = vdValue(v);
    const on = !v.exists && !v.has_default;
    const dis = v.exists ? ' disabled' : '';
    return '<div class="tf-vd-row" data-vd="' + e(v.name) + '"' + (v.exists ? ' data-exists' : '') + '>' +
      '<input type="checkbox" data-vd-on' + (on ? ' checked' : '') + dis + '>' +
      '<div class="tf-vd-main"><div class="tf-row"><b class="mono">' + e(v.name) + '</b>' +
      (v.type ? '<code class="mono tf-muted">' + e(v.type) + '</code>' : '') +
      (v.exists ? '<span class="badge">' + e(t('tf_vd_exists', 'exists')) + '</span>' : '') +
      (v.has_default ? '' : '<span class="tf-muted">' + e(t('tf_vd_no_default', 'no default')) + '</span>') + '</div>' +
      (v.description ? '<div class="tf-muted tf-v-desc">' + e(v.description) + '</div>' : '') +
      '<div class="tf-row"><input type="text" data-vd-val class="form-input mono tf-v-grow" spellcheck="false" value="' + e(x.val) + '"' + dis + (v.sensitive ? ' style="-webkit-text-security:disc;"' : '') + '>' +
      '<label class="tf-check"><input type="checkbox" data-vd-hcl' + (x.hcl ? ' checked' : '') + dis + '> ' + e(t('tf_var_hcl', 'HCL')) + '</label>' +
      '<label class="tf-check"><input type="checkbox" data-vd-sens' + (v.sensitive ? ' checked' : '') + dis + '> ' + e(t('tf_var_sensitive', 'Sensitive')) + '</label></div>' +
      '<div class="tf-vd-err" data-vd-err hidden></div></div></div>';
  }

  Tf.variables = {
    sets: { load: loadSets, open: openVarset, remove: delVarset },
    cloud: { load: loadCloud, open: openCloud, remove: delCloud, test: testCloud },
    // Opens the detect dialog for ws; onDone runs after variables were added.
    detect: async function (ws, onDone) {
      let r;
      try { r = await Tf.api.get('/api/terraform/workspaces/' + ws.id + '/variables/detect'); }
      catch (err) { Tf.toast(errText(err), 'error'); return; }
      if (r.supported === false) { Tf.toast(t('tf_vd_inline_only', 'Detection works for inline workspaces only.'), 'info'); return; }
      const vars = r.variables || [];
      Tf.dlg.open({
        title: t('tf_vd_title', 'Detect variables from configuration'), width: 720,
        body: vars.length
          ? '<div class="tf-hint tf-v-mb">' + e(t('tf_vd_hint', '')) + '</div><div id="tfVarDetectList" class="tf-vd-list">' + vars.map(vdRow).join('') + '</div>'
          : '<div id="tfVarDetectList">' + Tf.ui.empty({ icon: 'search', desc: t('tf_vd_empty', 'No variable blocks found in the configuration.') }) + '</div>',
        footer: cancelBtn() + (vars.length ? '<button type="button" id="tfVarDetectAdd" class="btn btn-sm btn-primary">' + e(t('tf_vd_add', 'Add selected')) + '</button>' : ''),
        onMount: function (el, api) {
          el.addEventListener('click', function (ev) { if (ev.target.closest('#tfVarDetectAdd')) addDetected(ws, el, api, onDone); });
        },
      });
    },
  };

  async function addDetected(ws, el, api, onDone) {
    const rows = Array.prototype.filter.call(el.querySelectorAll('#tfVarDetectList [data-vd]'), function (r) { return r.querySelector('[data-vd-on]').checked; });
    if (!rows.length) { api.error(t('tf_vd_none', 'Select at least one variable.')); return; }
    api.clearError();
    const btn = q(el, 'tfVarDetectAdd');
    btn.disabled = true;
    let ok = 0, fail = 0;
    for (const row of rows) {
      const err = row.querySelector('[data-vd-err]');
      err.hidden = true;
      try {
        await Tf.api.post(varBase({ kind: 'ws', id: ws.id }), {
          key: row.getAttribute('data-vd'), value: row.querySelector('[data-vd-val]').value, category: 'terraform',
          hcl: row.querySelector('[data-vd-hcl]').checked, sensitive: row.querySelector('[data-vd-sens]').checked, description: '',
        });
        ok++;
        row.querySelector('[data-vd-on]').checked = false;
        row.querySelectorAll('input').forEach(function (i) { i.disabled = true; });
        row.style.opacity = '.6';
      } catch (err2) {
        fail++;
        err.textContent = errText(err2);
        err.hidden = false;
      }
    }
    btn.disabled = false;
    Tf.toast(t('tf_vd_added', 'Added {n}, failed {f}', { n: ok, f: fail }), fail ? 'error' : 'success');
    if (ok && onDone) onDone();
    if (!fail) api.close();
  }

  /* ---------- workspace drawer tab: Variables (Variables / Linked Outputs sub-views) ---------- */
  Tf.registerWsTab({
    id: 'variables', icon: 'braces', labelKey: 'tf_ws_tab_variables', fallback: 'Variables', order: 5,
    render: function (body, ws) { wsSub(body, ws, 'vars'); },
  });

  function wsSub(body, ws, sub) {
    body.innerHTML = '<div class="tf-v-seg tf-v-seg-sm" role="tablist">' +
      '<button type="button" class="tf-v-seg-btn' + (sub === 'vars' ? ' active' : '') + '" role="tab" data-wsv-sub="vars">' + e(t('tf_var_mode_direct', 'Workspace variables')) + '</button>' +
      '<button type="button" class="tf-v-seg-btn' + (sub === 'links' ? ' active' : '') + '" role="tab" data-wsv-sub="links">' + e(t('tf_lk_title', 'Linked Outputs')) + '</button></div>' +
      '<div id="tfWsVarsBody"></div>';
    body.onclick = function (ev) {
      const b = ev.target.closest('[data-wsv-sub]');
      if (b) wsSub(body, ws, b.getAttribute('data-wsv-sub'));
    };
    const inner = body.querySelector('#tfWsVarsBody');
    if (sub === 'links') linkedOutputs(inner, ws); else wsVarList(inner, ws, { eff: false });
  }

  // Direct or effective variable list with search, category filter, add and detect.
  function wsVarList(inner, ws, st) {
    const scope = { kind: 'ws', id: ws.id };
    inner.innerHTML = '<div class="tf-v-bar">' +
      '<div class="tf-v-seg tf-v-seg-sm" role="tablist">' +
      '<button type="button" class="tf-v-seg-btn' + (!st.eff ? ' active' : '') + '" data-wsv-mode="direct">' + e(t('tf_var_mode_direct', 'Workspace variables')) + '</button>' +
      '<button type="button" class="tf-v-seg-btn' + (st.eff ? ' active' : '') + '" data-wsv-mode="eff">' + e(t('tf_var_mode_eff', 'Effective')) + '</button></div>' +
      '<input type="search" id="tfVarSearch" class="form-input" placeholder="' + e(t('tf_var_search', 'Search key or description…')) + '" aria-label="' + e(t('tf_var_search', '')) + '">' +
      '<select id="tfVarCatFilter" class="form-input" aria-label="' + e(t('tf_var_category', 'Category')) + '">' +
      '<option value="">' + e(t('tf_var_all_cats', 'All categories')) + '</option><option value="terraform">terraform</option><option value="env">env</option></select>' +
      '<span class="tf-v-spacer"></span>' +
      '<button type="button" id="tfVarDetectBtn" class="btn btn-sm" data-wsv-detect>' + ic('search', 'icon-xs') + ' ' + e(t('tf_vd_btn', 'Detect from Configuration')) + '</button>' +
      '<button type="button" class="btn btn-sm btn-primary" data-wsv-add>' + ic('plus', 'icon-xs') + ' ' + e(t('tf_var_add', 'Add Variable')) + '</button></div>' +
      (st.eff ? '<div class="tf-hint tf-v-mb">' + e(t('tf_var_eff_hint', '')) + '</div>' : '') +
      '<div id="tfVarsBox">' + Tf.ui.skeleton(3) + '</div>';

    const reload = function () { wsVarList(inner, ws, st); };
    const box = inner.querySelector('#tfVarsBox');
    inner.onclick = function (ev) {
      if (ev.target.closest('[data-wsv-mode]')) return wsVarList(inner, ws, { eff: ev.target.closest('[data-wsv-mode]').dataset.wsvMode === 'eff' });
      if (ev.target.closest('[data-wsv-add]')) return openVar(scope, null, reload);
      if (ev.target.closest('[data-wsv-detect]')) return Tf.variables.detect(ws, reload);
    };
    inner.oninput = inner.onchange = function () { paintVars(box, scope, reload, st, inner); };
    bindVarTable(box, scope, reload);

    Tf.api.get('/api/terraform/workspaces/' + ws.id + '/variables' + (st.eff ? '?effective=1' : ''))
      .then(function (r) { st.vars = r.variables || []; paintVars(box, scope, reload, st, inner); })
      .catch(function (err) { box.innerHTML = Tf.ui.error(errText(err)); });
  }

  function paintVars(box, scope, reload, st, inner) {
    if (!st.vars) return;
    const term = (inner.querySelector('#tfVarSearch').value || '').toLowerCase();
    const cat = inner.querySelector('#tfVarCatFilter').value;
    const shown = st.vars.filter(function (v) {
      if (cat && (v.category || 'terraform') !== cat) return false;
      return !term || (v.key + ' ' + (v.description || '')).toLowerCase().indexOf(term) >= 0;
    });
    // Overridden is judged on the full list, so a search filter does not hide the badge.
    const wsKeys = new Set(st.vars.filter(function (v) { return v.origin === 'workspace' || !st.eff; }).map(function (v) { return v.key; }));
    box.innerHTML = varTableHTML(box, shown, {
      eff: st.eff, id: 'tfVarsTable', wsKeys: wsKeys,
      editable: function (v) { return !st.eff || v.origin === 'workspace'; },
    });
    bindVarTable(box, scope, reload);
  }

  // Linked outputs: rows of target variable <- source workspace :: output, with a status per row.
  async function linkedOutputs(inner, ws) {
    inner.innerHTML = '<div class="tf-hint tf-v-mb">' + e(t('tf_lk_hint', '')) + '</div><div id="tfLkBody">' + Tf.ui.skeleton(2) + '</div>';
    let links, sources, direct;
    try {
      const pair = await Promise.all([
        Tf.api.get('/api/terraform/workspaces/' + ws.id + '/output-links'),
        Tf.api.get('/api/terraform/workspaces/' + ws.id + '/variables'),
      ]);
      links = pair[0].links || [];
      sources = pair[0].sources || [];
      direct = new Set((pair[1].variables || []).map(function (v) { return v.key; }));
    } catch (err) {
      const b = inner.querySelector('#tfLkBody');
      if (b) b.innerHTML = Tf.ui.error(errText(err));
      return;
    }
    const names = {};
    links.forEach(function (l) { names[l.source_workspace_id] = l.source_workspace_name; });
    const st = {
      sources: sources, names: names, direct: direct,
      rows: links.map(function (l) { return { v: l.var_name, src: l.source_workspace_id, out: l.output_name }; }),
    };
    lkRender(inner, ws, st);
  }

  function lkStatus(r, st) {
    if (!r.src || !r.out) return null;
    const src = st.sources.find(function (s) { return s.id === r.src; });
    const o = src && (src.outputs || []).find(function (x) { return x.name === r.out; });
    if (!o) return ['danger', t('tf_lk_unavailable', 'Unavailable')];
    if (r.v && st.direct.has(r.v)) return ['warn', t('tf_lk_overridden', 'Overridden by explicit var')];
    if (o.sensitive) return ['info', t('tf_lk_sensitive_state', 'Sensitive')];
    return ['ok', t('tf_lk_available', 'Available')];
  }

  function lkRowHTML(r, i, st) {
    const src = st.sources.find(function (s) { return s.id === r.src; });
    const outs = src ? src.outputs || [] : [];
    const srcOpts = st.sources.map(function (s) { return [s.id, s.name]; });
    if (r.src && !src) srcOpts.push([r.src, st.names[r.src] || '#' + r.src]);
    const outOpts = outs.map(function (o) { return [o.name, o.name + (o.sensitive ? ' (' + t('tf_lk_sensitive', 'sensitive') + ')' : '')]; });
    if (r.out && !outs.some(function (o) { return o.name === r.out; })) outOpts.push([r.out, r.out]);
    const sel = function (attr, opts, cur, label) {
      return '<select class="form-input" ' + attr + ' aria-label="' + e(label) + '"><option value="">—</option>' +
        opts.map(function (p) { return '<option value="' + e(p[0]) + '"' + (String(p[0]) === String(cur) ? ' selected' : '') + '>' + e(p[1]) + '</option>'; }).join('') + '</select>';
    };
    const stt = lkStatus(r, st);
    return '<div class="tf-lk-row" data-lk-row="' + i + '">' +
      '<input type="text" class="form-input mono" data-lk-var value="' + e(r.v) + '" placeholder="' + e(t('tf_lk_var', '')) + '" maxlength="64" spellcheck="false" aria-label="' + e(t('tf_lk_var', '')) + '">' +
      sel('data-lk-src', srcOpts, r.src, t('tf_lk_source', '')) + sel('data-lk-out', outOpts, r.out, t('tf_lk_output', '')) +
      (stt ? '<span class="tf-lk-status tf-lk-' + stt[0] + '">' + e(stt[1]) + '</span>' : '') +
      '<button type="button" class="btn btn-icon btn-sm" data-lk-del="' + i + '" aria-label="' + e(t('tf_lk_remove', 'Remove link')) + '">' + ic('trash-2', 'icon-xs') + '</button></div>';
  }

  function lkRender(inner, ws, st) {
    const body = inner.querySelector('#tfLkBody');
    if (!body) return;
    body.innerHTML = '<div id="tfLkRows" class="tf-lk-rows">' +
      (st.rows.length ? st.rows.map(function (r, i) { return lkRowHTML(r, i, st); }).join('') : Tf.ui.empty({ icon: 'link', desc: st.sources.length ? t('tf_lk_empty', '') : t('tf_lk_no_sources', '') })) +
      '</div><div class="tf-lk-foot"><span id="tfLkErr" class="tf-lk-err" role="alert"></span>' +
      '<button type="button" class="btn btn-sm" id="tfLkAdd"' + (st.sources.length ? '' : ' disabled') + '>' + ic('plus', 'icon-xs') + ' ' + e(t('tf_lk_add', 'Add link')) + '</button>' +
      '<button type="button" class="btn btn-sm btn-primary" id="tfLkSave">' + e(t('tf_lk_save', 'Save links')) + '</button></div>';

    body.onclick = function (ev) {
      const del = ev.target.closest('[data-lk-del]');
      if (del) { collect(inner, st); st.rows.splice(Number(del.dataset.lkDel), 1); return lkRender(inner, ws, st); }
      if (ev.target.closest('#tfLkAdd')) {
        collect(inner, st);
        const s0 = st.sources[0];
        st.rows.push({ v: '', src: s0.id, out: s0.outputs && s0.outputs[0] ? s0.outputs[0].name : '' });
        return lkRender(inner, ws, st);
      }
      if (ev.target.closest('#tfLkSave')) return lkSave(inner, ws, st);
    };
    body.onchange = function (ev) {
      if (!ev.target.matches('[data-lk-src],[data-lk-out]')) return;
      collect(inner, st);
      if (ev.target.matches('[data-lk-src]')) {
        const row = st.rows[Number(ev.target.closest('[data-lk-row]').dataset.lkRow)];
        const s = st.sources.find(function (x) { return x.id === row.src; });
        row.out = s && s.outputs && s.outputs[0] ? s.outputs[0].name : '';
      }
      lkRender(inner, ws, st);
    };
  }

  // Reads the DOM back into st.rows (keeps unsaved edits across re-renders).
  function collect(inner, st) {
    st.rows = Array.prototype.map.call(inner.querySelectorAll('#tfLkRows [data-lk-row]'), function (el) {
      return {
        v: el.querySelector('[data-lk-var]').value.trim(),
        src: Number(el.querySelector('[data-lk-src]').value) || 0,
        out: el.querySelector('[data-lk-out]').value,
      };
    });
  }

  async function lkSave(inner, ws, st) {
    collect(inner, st);
    const err = inner.querySelector('#tfLkErr');
    const names = st.rows.map(function (r) { return r.v; });
    const badName = st.rows.some(function (r) { return !/^[A-Za-z_][A-Za-z0-9_]*$/.test(r.v); }) || new Set(names).size !== names.length;
    const msg = badName ? t('tf_lk_var_bad', '') : st.rows.some(function (r) { return !r.src || !r.out; }) ? t('tf_lk_pick', '') : '';
    err.textContent = msg;
    if (msg) return;
    try {
      await Tf.api.put('/api/terraform/workspaces/' + ws.id + '/output-links', {
        links: st.rows.map(function (r) { return { var_name: r.v, source_workspace_id: r.src, output_name: r.out }; }),
      });
    } catch (e2) { err.textContent = errText(e2); return; }
    Tf.toast(t('tf_lk_saved', 'Linked outputs saved'), 'success');
    linkedOutputs(inner, ws);
  }
})();
