/* Governance tab (Policies, Audit Trail, Gate/CLI/Alerts) and the workspace drawer's Settings tab. */
(function () {
  'use strict';

  const Tf = window.Tf;
  const esc = Tf.esc, ic = Tf.icon;

  Tf.addI18n({
    en: {
      tf_tab_governance: 'Governance & Settings', tf_gov_policies: 'Policies', tf_gov_audit: 'Audit Trail', tf_gov_gate: 'Gate, CLI & Alerts',
      tf_gov_policies_gate: 'Policies are enforced on every run, so the Terraform Gate must be enabled to manage them.',
      tf_gov_audit_gate: 'The audit trail is available once the Terraform Gate is enabled.',
      tf_pol_new: 'New Policy', tf_pol_edit: 'Edit Policy', tf_pol_search: 'Search policies…', tf_pol_all_enf: 'All enforcement',
      tf_pol_all_scope: 'All scopes', tf_pol_global: 'Global', tf_pol_scope_all: 'All workspaces (global)', tf_pol_none: 'No policies yet.',
      tf_pol_name: 'Name', tf_pol_name_required: 'Name is required', tf_pol_kind: 'Rule', tf_pol_desc: 'Description',
      tf_pol_enf: 'Enforcement', tf_pol_scope: 'Scope', tf_pol_max: 'Maximum', tf_pol_types: 'Resource types (comma separated)',
      tf_pol_types_opt: 'Resource types (optional, comma separated)', tf_pol_tags: 'Required tags (comma separated)',
      tf_pol_enabled: 'Enabled', tf_pol_on: 'On', tf_pol_off: 'Off', tf_pol_delete_confirm: 'Delete policy "{name}"?',
      tf_pol_enf_advisory: 'Advisory (warn only)', tf_pol_enf_soft: 'Soft (can be overridden)', tf_pol_enf_hard: 'Hard (blocks apply)',
      tf_pol_k_deny_destroy: 'Deny destroy', tf_pol_k_max_deletes: 'Max deletes', tf_pol_k_max_changes: 'Max changes',
      tf_pol_k_deny_resource_types: 'Deny resource types', tf_pol_k_require_tags: 'Require tags', tf_pol_k_deny_open_cidr: 'Deny open CIDR',
      tf_pol_saved: 'Policy saved', tf_pol_deleted: 'Policy deleted',
      tf_aud_all_ws: 'All workspaces', tf_aud_all_levels: 'All levels', tf_aud_search: 'Search messages…', tf_aud_refresh: 'Refresh',
      tf_aud_empty: 'No audit events match these filters.', tf_aud_loading: 'Loading…', tf_aud_col_time: 'Time', tf_aud_col_ws: 'Workspace',
      tf_gate_card_title: 'Terraform Gate & Security', tf_gate_warn: 'Runs execute Terraform on this server with the privileges of the Pantau process. Only enable the Gate if you trust everyone who can sign in.',
      tf_gate_enable: 'Enable Terraform Gate', tf_gate_disable: 'Disable Gate', tf_gate_confirm_enable_title: 'Enable the Terraform Gate?',
      tf_gate_confirm_enable_msg: 'Terraform runs execute on this server with the privileges of the Pantau process. Enable only if you trust everyone who can sign in to Pantau.',
      tf_gate_confirm_disable_title: 'Disable the Terraform Gate?', tf_gate_confirm_disable_msg: 'New runs, drift checks and state operations will be blocked.',
      tf_gate_toggled_on: 'Terraform Gate enabled', tf_gate_toggled_off: 'Terraform Gate disabled', tf_gate_bins: 'CLI binaries',
      tf_gate_infracost: 'infracost', tf_gate_not_installed: 'not installed', tf_gate_failed: 'Could not change the Gate',
      tf_cli_title: 'CLI update check', tf_cli_hint: 'Compares the installed terraform / tofu with the latest release published upstream. The check only reads; the update installs into the directory Pantau manages.',
      tf_cli_check: 'Check for updates', tf_cli_checking: 'Checking…', tf_cli_available: 'Update available', tf_cli_current: 'Up to date',
      tf_cli_unknown: 'Version unknown', tf_cli_error: 'Check failed', tf_cli_installed: 'Installed', tf_cli_latest: 'Latest',
      tf_cli_release: 'Release', tf_cli_checked_at: 'Checked at', tf_cli_update: 'Update now', tf_cli_install_failed: 'The update failed',
      tf_cli_install_hint: 'The update installs the new version into the directory Pantau manages and verifies it against the checksum the upstream publishes. The system copy of the CLI is left untouched.',
      tf_cli_installed_ok: 'CLI updated', tf_cli_gate: 'Enable the Terraform Gate to check CLI updates.',
      tf_nf_title: 'Notifications', tf_nf_hint: 'Choose which run events send a notification.', tf_nf_save: 'Save', tf_nf_test: 'Send test notification',
      tf_nf_saved: 'Notification events saved', tf_nf_testing: 'Sending…', tf_nf_test_ok: 'Test notification sent',
      tf_nf_test_fail: 'Test failed: {err}', tf_nf_gate: 'Enable the Terraform Gate to manage notifications.',
      tf_nf_ev_awaiting_approval: 'Awaiting approval', tf_nf_ev_errored: 'Errored', tf_nf_ev_applied: 'Applied', tf_nf_ev_drift: 'Drift detected',
      tf_reg_title: 'Registry mirror URL', tf_reg_hint: 'Base URL of the Terraform Registry used for module and provider search. Leave empty for the public registry.',
      tf_reg_save: 'Save', tf_reg_test: 'Test connection', tf_reg_saved: 'Registry URL saved', tf_reg_testing: 'Testing…',
      tf_reg_test_ok: 'Registry reachable ({ms} ms)', tf_reg_test_fail: 'Connection failed: {err}', tf_reg_gate: 'Enable the Terraform Gate to change the registry URL.',
      tf_ws_tab_settings: 'Settings', tf_set_general: 'General & automation', tf_set_source: 'Source & VCS', tf_set_cloud: 'Cloud accounts',
      tf_set_name: 'Name', tf_set_group: 'Group', tf_set_group_ph: 'e.g. networking', tf_set_env: 'Environment', tf_set_tags: 'Tags',
      tf_set_tags_ph: 'a, b, c', tf_set_binary: 'Binary', tf_set_drift: 'Drift interval (minutes)', tf_set_drift_hint: '0 = off',
      tf_set_plan: 'Scheduled plan interval (minutes)', tf_set_plan_hint: '0 = off', tf_set_autoapply: 'Auto-apply',
      tf_set_autoapply_hint: 'Apply automatically when a plan has no blocking policy', tf_set_autoapply_prod: 'Auto-apply is never allowed for production.',
      tf_set_desc: 'Description', tf_set_inline: 'Inline source · {n} file(s)', tf_set_open_config: 'Open Configuration',
      tf_set_inline_note: 'Files are edited in the Configuration tab.', tf_set_git_url: 'Git URL', tf_set_git_branch: 'Branch',
      tf_set_git_path: 'Working directory', tf_set_git_auth: 'Authentication', tf_set_git_none: 'None (public)', tf_set_git_token: 'Token',
      tf_set_git_token_keep: 'Leave empty to keep the saved token', tf_set_git_key: 'SSH key', tf_set_vcs_test: 'Test VCS connection',
      tf_set_vcs_testing: 'Testing…', tf_set_vcs_ok: 'Connected', tf_set_cloud_hint: 'Accounts this workspace may use during runs.',
      tf_set_cloud_none: 'No cloud accounts yet.', tf_set_cloud_manage: 'Manage cloud accounts',
      tf_set_webhook: 'Webhook URL', tf_set_webhook_hint: 'Triggers a plan only. Auto-apply is never started by a webhook.',
      tf_set_webhook_headers: 'GitHub: X-Hub-Signature-256 · GitLab: X-Gitlab-Token', tf_set_rotate: 'Rotate secret',
      tf_set_rotate_confirm: 'Rotating the secret invalidates the current webhook. Continue?', tf_set_secret_once: 'Webhook secret (shown only once):',
      tf_set_copy: 'Copy', tf_set_danger: 'Danger zone', tf_set_delete: 'Delete workspace', tf_set_delete_run: 'Finish the active run before deleting.',
      tf_set_delete_confirm: 'Delete workspace "{name}"? Its runs and state history are removed.', tf_set_deleted: 'Workspace deleted',
      tf_set_dirty: 'Unsaved changes', tf_set_reset: 'Reset', tf_set_save: 'Save', tf_set_saved: 'Workspace saved',
      tf_set_name_required: 'Name is required', tf_set_leave_title: 'Discard unsaved changes?', tf_set_leave_msg: 'The settings you changed will be lost.',
      tf_set_leave: 'Discard', tf_set_stay: 'Keep editing',
    },
    id: {
      tf_tab_governance: 'Tata Kelola & Pengaturan', tf_gov_policies: 'Kebijakan', tf_gov_audit: 'Jejak Audit', tf_gov_gate: 'Gate, CLI & Notifikasi',
      tf_gov_policies_gate: 'Kebijakan diterapkan pada setiap run, jadi Terraform Gate harus diaktifkan untuk mengelolanya.',
      tf_gov_audit_gate: 'Jejak audit tersedia setelah Terraform Gate diaktifkan.',
      tf_pol_new: 'Kebijakan Baru', tf_pol_edit: 'Ubah Kebijakan', tf_pol_search: 'Cari kebijakan…', tf_pol_all_enf: 'Semua penegakan',
      tf_pol_all_scope: 'Semua cakupan', tf_pol_global: 'Global', tf_pol_scope_all: 'Semua workspace (global)', tf_pol_none: 'Belum ada kebijakan.',
      tf_pol_name: 'Nama', tf_pol_name_required: 'Nama wajib diisi', tf_pol_kind: 'Aturan', tf_pol_desc: 'Deskripsi',
      tf_pol_enf: 'Penegakan', tf_pol_scope: 'Cakupan', tf_pol_max: 'Maksimum', tf_pol_types: 'Tipe resource (dipisah koma)',
      tf_pol_types_opt: 'Tipe resource (opsional, dipisah koma)', tf_pol_tags: 'Tag wajib (dipisah koma)',
      tf_pol_enabled: 'Aktif', tf_pol_on: 'Aktif', tf_pol_off: 'Nonaktif', tf_pol_delete_confirm: 'Hapus kebijakan "{name}"?',
      tf_pol_enf_advisory: 'Peringatan saja', tf_pol_enf_soft: 'Lunak (bisa di-override)', tf_pol_enf_hard: 'Keras (memblokir apply)',
      tf_pol_k_deny_destroy: 'Larang destroy', tf_pol_k_max_deletes: 'Maks. hapus', tf_pol_k_max_changes: 'Maks. perubahan',
      tf_pol_k_deny_resource_types: 'Larang tipe resource', tf_pol_k_require_tags: 'Wajib tag', tf_pol_k_deny_open_cidr: 'Larang CIDR terbuka',
      tf_pol_saved: 'Kebijakan disimpan', tf_pol_deleted: 'Kebijakan dihapus',
      tf_aud_all_ws: 'Semua workspace', tf_aud_all_levels: 'Semua level', tf_aud_search: 'Cari pesan…', tf_aud_refresh: 'Muat ulang',
      tf_aud_empty: 'Tidak ada event audit yang cocok dengan filter ini.', tf_aud_loading: 'Memuat…', tf_aud_col_time: 'Waktu', tf_aud_col_ws: 'Workspace',
      tf_gate_card_title: 'Terraform Gate & Keamanan', tf_gate_warn: 'Run menjalankan Terraform di server ini dengan hak akses proses Pantau. Aktifkan Gate hanya jika Anda memercayai semua orang yang bisa masuk.',
      tf_gate_enable: 'Aktifkan Terraform Gate', tf_gate_disable: 'Nonaktifkan Gate', tf_gate_confirm_enable_title: 'Aktifkan Terraform Gate?',
      tf_gate_confirm_enable_msg: 'Run Terraform dijalankan di server ini dengan hak akses proses Pantau. Aktifkan hanya jika Anda memercayai semua orang yang bisa masuk ke Pantau.',
      tf_gate_confirm_disable_title: 'Nonaktifkan Terraform Gate?', tf_gate_confirm_disable_msg: 'Run baru, cek drift, dan operasi state akan diblokir.',
      tf_gate_toggled_on: 'Terraform Gate diaktifkan', tf_gate_toggled_off: 'Terraform Gate dinonaktifkan', tf_gate_bins: 'Binary CLI',
      tf_gate_infracost: 'infracost', tf_gate_not_installed: 'belum terpasang', tf_gate_failed: 'Gagal mengubah Gate',
      tf_cli_title: 'Pemeriksaan pembaruan CLI', tf_cli_hint: 'Membandingkan terraform / tofu yang terpasang dengan rilis terbaru di upstream. Pemeriksaan hanya membaca; pembaruan memasang ke direktori yang dikelola Pantau.',
      tf_cli_check: 'Periksa pembaruan', tf_cli_checking: 'Memeriksa…', tf_cli_available: 'Pembaruan tersedia', tf_cli_current: 'Terbaru',
      tf_cli_unknown: 'Versi tidak diketahui', tf_cli_error: 'Pemeriksaan gagal', tf_cli_installed: 'Terpasang', tf_cli_latest: 'Terbaru',
      tf_cli_release: 'Rilis', tf_cli_checked_at: 'Diperiksa pada', tf_cli_update: 'Perbarui sekarang', tf_cli_install_failed: 'Pembaruan gagal',
      tf_cli_install_hint: 'Pembaruan memasang versi baru ke direktori yang dikelola Pantau dan memverifikasinya terhadap checksum yang dipublikasikan upstream. Salinan sistem CLI tidak disentuh.',
      tf_cli_installed_ok: 'CLI diperbarui', tf_cli_gate: 'Aktifkan Terraform Gate untuk memeriksa pembaruan CLI.',
      tf_nf_title: 'Notifikasi', tf_nf_hint: 'Pilih event run mana yang mengirim notifikasi.', tf_nf_save: 'Simpan', tf_nf_test: 'Kirim notifikasi uji',
      tf_nf_saved: 'Event notifikasi disimpan', tf_nf_testing: 'Mengirim…', tf_nf_test_ok: 'Notifikasi uji terkirim',
      tf_nf_test_fail: 'Uji gagal: {err}', tf_nf_gate: 'Aktifkan Terraform Gate untuk mengelola notifikasi.',
      tf_nf_ev_awaiting_approval: 'Menunggu persetujuan', tf_nf_ev_errored: 'Error', tf_nf_ev_applied: 'Diterapkan', tf_nf_ev_drift: 'Drift terdeteksi',
      tf_reg_title: 'URL mirror Registry', tf_reg_hint: 'URL dasar Terraform Registry untuk pencarian modul dan provider. Kosongkan untuk registry publik.',
      tf_reg_save: 'Simpan', tf_reg_test: 'Uji koneksi', tf_reg_saved: 'URL registry disimpan', tf_reg_testing: 'Menguji…',
      tf_reg_test_ok: 'Registry dapat dijangkau ({ms} ms)', tf_reg_test_fail: 'Koneksi gagal: {err}', tf_reg_gate: 'Aktifkan Terraform Gate untuk mengubah URL registry.',
      tf_ws_tab_settings: 'Pengaturan', tf_set_general: 'Umum & otomatisasi', tf_set_source: 'Sumber & VCS', tf_set_cloud: 'Akun cloud',
      tf_set_name: 'Nama', tf_set_group: 'Grup', tf_set_group_ph: 'mis. networking', tf_set_env: 'Environment', tf_set_tags: 'Tag',
      tf_set_tags_ph: 'a, b, c', tf_set_binary: 'Binary', tf_set_drift: 'Interval drift (menit)', tf_set_drift_hint: '0 = nonaktif',
      tf_set_plan: 'Interval plan terjadwal (menit)', tf_set_plan_hint: '0 = nonaktif', tf_set_autoapply: 'Auto-apply',
      tf_set_autoapply_hint: 'Terapkan otomatis saat plan tidak diblokir kebijakan', tf_set_autoapply_prod: 'Auto-apply tidak pernah diizinkan untuk production.',
      tf_set_desc: 'Deskripsi', tf_set_inline: 'Sumber inline · {n} berkas', tf_set_open_config: 'Buka Konfigurasi',
      tf_set_inline_note: 'Berkas diedit di tab Konfigurasi.', tf_set_git_url: 'URL Git', tf_set_git_branch: 'Branch',
      tf_set_git_path: 'Direktori kerja', tf_set_git_auth: 'Autentikasi', tf_set_git_none: 'Tanpa (publik)', tf_set_git_token: 'Token',
      tf_set_git_token_keep: 'Kosongkan untuk mempertahankan token tersimpan', tf_set_git_key: 'Kunci SSH', tf_set_vcs_test: 'Uji koneksi VCS',
      tf_set_vcs_testing: 'Menguji…', tf_set_vcs_ok: 'Terhubung', tf_set_cloud_hint: 'Akun yang boleh dipakai workspace ini saat run.',
      tf_set_cloud_none: 'Belum ada akun cloud.', tf_set_cloud_manage: 'Kelola akun cloud',
      tf_set_webhook: 'URL Webhook', tf_set_webhook_hint: 'Hanya memicu plan. Webhook tidak pernah memulai auto-apply.',
      tf_set_webhook_headers: 'GitHub: X-Hub-Signature-256 · GitLab: X-Gitlab-Token', tf_set_rotate: 'Putar ulang secret',
      tf_set_rotate_confirm: 'Memutar ulang secret membuat webhook saat ini tidak berlaku. Lanjutkan?', tf_set_secret_once: 'Secret webhook (hanya ditampilkan sekali):',
      tf_set_copy: 'Salin', tf_set_danger: 'Zona berbahaya', tf_set_delete: 'Hapus workspace', tf_set_delete_run: 'Selesaikan run aktif sebelum menghapus.',
      tf_set_delete_confirm: 'Hapus workspace "{name}"? Run dan riwayat state-nya ikut dihapus.', tf_set_deleted: 'Workspace dihapus',
      tf_set_dirty: 'Perubahan belum disimpan', tf_set_reset: 'Atur ulang', tf_set_save: 'Simpan', tf_set_saved: 'Workspace disimpan',
      tf_set_name_required: 'Nama wajib diisi', tf_set_leave_title: 'Buang perubahan yang belum disimpan?', tf_set_leave_msg: 'Pengaturan yang Anda ubah akan hilang.',
      tf_set_leave: 'Buang', tf_set_stay: 'Lanjut mengedit',
    },
  });

  const SEGS = [['policies', 'tf_gov_policies', 'Policies'], ['audit', 'tf_gov_audit', 'Audit Trail'], ['gate', 'tf_gov_gate', 'Gate, CLI & Alerts']];
  const KINDS = ['deny_destroy', 'max_deletes', 'max_changes', 'deny_resource_types', 'require_tags', 'deny_open_cidr'];
  const ENFORCE = ['advisory', 'soft', 'hard'];
  const ENF_CLS = { advisory: 'badge-info', soft: 'badge-warning', hard: 'badge-danger' };

  const NS = (Tf.governance = { seg: 'policies', policies: [], audit: { events: [], ws: '', level: '', q: '' }, cli: { results: [], busy: false }, regSaved: '', settings: null });

  // Lists are read through these so a missing module or an offline API degrades instead of throwing.
  async function ensureWorkspaces(force) {
    if (force || !Tf.state.workspaces.length) {
      try { Tf.state.workspaces = (await Tf.api.get('/api/terraform/workspaces')).workspaces || []; } catch (e) { /* keep the cached list */ }
    }
    return Tf.state.workspaces;
  }
  const wsName = function (id) { const w = Tf.state.workspaces.find(function (x) { return x.id === id; }); return w ? w.name : '#' + id; };
  const field = function (label, inner, cls) { return '<div class="tf-field' + (cls ? ' ' + cls : '') + '"><label class="tf-field-label">' + esc(label) + '</label>' + inner + '</div>'; };
  const errText = Tf.errMsg;

  /* ================= top-level tab ================= */
  Tf.registerTab({
    id: 'governance', icon: 'shield', labelKey: 'tf_tab_governance', fallback: 'Governance & Settings', order: 5, alwaysRender: true,
    render: function (panel) {
      panel.innerHTML = '<div class="tabs tf-seg" role="tablist" aria-label="' + esc(Tf.t('tf_tab_governance', 'Governance & Settings')) + '">' +
        SEGS.map(function (s) { return '<button type="button" class="tab" role="tab" data-seg="' + s[0] + '" aria-selected="false">' + esc(Tf.t(s[1], s[2])) + '</button>'; }).join('') +
        '</div><div class="tf-gov-body" id="tfGovBody"></div>';
      panel.addEventListener('click', onClick);
      panel.addEventListener('change', onChange);
      panel.addEventListener('input', onInput);
    },
    onShow: function (panel) {
      const sub = Tf.state.sub.governance;
      if (sub) { NS.seg = sub; delete Tf.state.sub.governance; }
      drawSeg(panel);
    },
  });

  function drawSeg(panel) {
    panel.querySelectorAll('[data-seg]').forEach(function (b) {
      const on = b.getAttribute('data-seg') === NS.seg;
      b.classList.toggle('active', on); b.setAttribute('aria-selected', String(on)); b.tabIndex = on ? 0 : -1;
    });
    const body = document.getElementById('tfGovBody');
    if (!body) return;
    if (NS.seg === 'audit') drawAudit(body);
    else if (NS.seg === 'gate') drawGate(body);
    else drawPolicies(body);
  }

  function onClick(e) {
    const seg = e.target.closest('[data-seg]');
    if (seg) { NS.seg = seg.getAttribute('data-seg'); drawSeg(e.currentTarget); return; }
    const act = e.target.closest('[data-act]');
    if (!act) return;
    const a = act.getAttribute('data-act'), id = Number(act.getAttribute('data-id'));
    if (a === 'policy-new') openPolicy(0);
    else if (a === 'policy-edit') openPolicy(id);
    else if (a === 'policy-del') deletePolicy(id);
    else if (a === 'audit-refresh') loadAudit();
    else if (a === 'gate-toggle') toggleGate();
    else if (a === 'cli-check') cliCheck();
    else if (a === 'cli-install') cliInstall(act.getAttribute('data-target'), act);
    else if (a === 'notify-save') saveNotify();
    else if (a === 'notify-test') testNotify(act);
    else if (a === 'reg-save') saveRegistry();
    else if (a === 'reg-test') testRegistry(act);
  }
  function onChange(e) {
    const t = e.target;
    if (t.getAttribute('data-act') === 'policy-toggle') togglePolicy(Number(t.getAttribute('data-id')), t);
    else if (t.id === 'tfAuditWs') { NS.audit.ws = t.value; loadAudit(); }
    else if (t.id === 'tfAuditLevel') { NS.audit.level = t.value; renderAudit(); }
    else if (t.id === 'tfPolFilterEnf' || t.id === 'tfPolFilterScope') renderPolicies();
  }
  function onInput(e) {
    const t = e.target;
    if (t.id === 'tfPolSearch') renderPolicies();
    else if (t.id === 'tfAuditSearch') { NS.audit.q = t.value; renderAudit(); }
  }

  /* ================= policies ================= */
  function drawPolicies(body) {
    if (!Tf.gateOn()) { body.innerHTML = Tf.ui.gateOff(Tf.t('tf_gov_policies_gate')); return; }
    const enfOpts = [['', Tf.t('tf_pol_all_enf', 'All enforcement')]].concat(ENFORCE.map(function (x) { return [x, x]; }));
    body.innerHTML = '<div class="tf-toolbar">' +
      '<input type="search" id="tfPolSearch" class="form-input tf-grow" placeholder="' + esc(Tf.t('tf_pol_search')) + '" aria-label="' + esc(Tf.t('tf_pol_search')) + '" />' +
      '<select id="tfPolFilterEnf" class="form-input" aria-label="Enforcement">' + enfOpts.map(function (o) { return '<option value="' + esc(o[0]) + '">' + esc(o[1]) + '</option>'; }).join('') + '</select>' +
      '<select id="tfPolFilterScope" class="form-input" aria-label="Scope"><option value="">' + esc(Tf.t('tf_pol_all_scope')) + '</option><option value="0">' + esc(Tf.t('tf_pol_global')) + '</option></select>' +
      '<button type="button" class="btn btn-sm btn-primary" id="btnTfNewPolicy" data-act="policy-new">' + ic('plus', 'icon-sm') + ' <span>' + esc(Tf.t('tf_pol_new')) + '</span></button></div>' +
      '<div id="tfPolList" class="tf-list" aria-live="polite">' + Tf.ui.skeleton(3) + '</div>';
    ensureWorkspaces().then(function () {
      const sc = document.getElementById('tfPolFilterScope');
      if (sc) sc.insertAdjacentHTML('beforeend', Tf.state.workspaces.map(function (w) { return '<option value="' + w.id + '">' + esc(w.name) + '</option>'; }).join(''));
    });
    Tf.api.get('/api/terraform/policies').then(function (r) {
      NS.policies = r.policies || [];
      renderPolicies();
    }, function (e) {
      const list = document.getElementById('tfPolList');
      if (list) list.innerHTML = Tf.ui.error(errText(e));
    });
  }

  function renderPolicies() {
    const list = document.getElementById('tfPolList');
    if (!list) return;
    const q = (document.getElementById('tfPolSearch').value || '').trim().toLowerCase();
    const enf = document.getElementById('tfPolFilterEnf').value, scope = document.getElementById('tfPolFilterScope').value;
    const rows = NS.policies.filter(function (p) {
      if (enf && p.enforcement !== enf) return false;
      if (scope !== '' && String(p.workspace_id || 0) !== scope) return false;
      if (q && ((p.name || '') + ' ' + (p.description || '') + ' ' + (p.kind || '')).toLowerCase().indexOf(q) < 0) return false;
      return true;
    });
    if (!rows.length) { list.innerHTML = Tf.ui.empty({ icon: 'shield', desc: Tf.t('tf_pol_none') }); return; }
    list.innerHTML = rows.map(function (p) {
      const scopeTxt = p.workspace_id ? wsName(p.workspace_id) : Tf.t('tf_pol_global');
      const params = typeof p.params === 'string' ? p.params : JSON.stringify(p.params || {});
      return '<div class="card tf-pol' + (p.enabled ? '' : ' tf-pol-off') + '">' +
        '<div class="tf-pol-main"><div class="tf-row"><b>' + esc(p.name) + '</b> <span class="badge tf-mono">' + esc(kindLabel(p.kind)) + '</span> ' +
        '<span class="badge ' + (ENF_CLS[p.enforcement] || '') + '">' + esc(p.enforcement) + '</span></div>' +
        (p.description ? '<div class="tf-muted tf-sm">' + esc(p.description) + '</div>' : '') +
        '<div class="tf-mono tf-sm tf-muted">' + esc(scopeTxt) + ' · ' + esc(params) + '</div></div>' +
        '<div class="tf-row tf-pol-actions"><label class="tf-toggle"><input type="checkbox" data-act="policy-toggle" data-id="' + p.id + '"' + (p.enabled ? ' checked' : '') + ' />' +
        '<span>' + esc(Tf.t(p.enabled ? 'tf_pol_on' : 'tf_pol_off')) + '</span></label>' +
        '<button type="button" class="btn btn-sm" data-act="policy-edit" data-id="' + p.id + '">' + ic('pencil', 'icon-xs') + ' <span>' + esc(Tf.t('edit', 'Edit')) + '</span></button>' +
        '<button type="button" class="btn btn-icon btn-sm" data-act="policy-del" data-id="' + p.id + '" aria-label="' + esc(Tf.t('delete', 'Delete')) + '">' + ic('trash-2', 'icon-xs') + '</button></div></div>';
    }).join('');
  }

  function kindLabel(k) { return Tf.t('tf_pol_k_' + k, k); }

  function parseParams(p) {
    if (!p) return {};
    if (typeof p === 'object') return p;
    try { return JSON.parse(p) || {}; } catch (e) { return {}; }
  }
  // Full policy body (the API replaces the whole record on PUT).
  function policyBody(p, overrides) {
    return Object.assign({ name: p.name, description: p.description || '', kind: p.kind, params: parseParams(p.params), enforcement: p.enforcement, enabled: !!p.enabled, workspace_id: p.workspace_id || 0 }, overrides || {});
  }

  function togglePolicy(id, box) {
    const p = NS.policies.find(function (x) { return x.id === id; });
    if (!p) return;
    const enabled = box.checked;
    Tf.api.put('/api/terraform/policies/' + id, policyBody(p, { enabled: enabled })).then(function () {
      p.enabled = enabled;
      renderPolicies();
    }, function (e) { box.checked = !enabled; Tf.toast(errText(e), 'error'); });
  }

  async function openPolicy(id) {
    await ensureWorkspaces();
    const p = id ? NS.policies.find(function (x) { return x.id === id; }) : null;
    const params = p ? parseParams(p.params) : {};
    const kindOpts = KINDS.map(function (k) { return '<option value="' + k + '"' + ((p ? p.kind : 'deny_destroy') === k ? ' selected' : '') + '>' + esc(kindLabel(k)) + '</option>'; }).join('');
    const enfOpts = ENFORCE.map(function (x) { return '<option value="' + x + '"' + ((p ? p.enforcement : 'hard') === x ? ' selected' : '') + '>' + esc(Tf.t('tf_pol_enf_' + x, x)) + '</option>'; }).join('');
    const scopeOpts = '<option value="0">' + esc(Tf.t('tf_pol_scope_all')) + '</option>' + Tf.state.workspaces.map(function (w) {
      return '<option value="' + w.id + '"' + (p && p.workspace_id === w.id ? ' selected' : '') + '>' + esc(w.name) + '</option>';
    }).join('');
    const body = '<div class="tf-grid-2">' +
      field(Tf.t('tf_pol_name') + ' *', '<input type="text" id="tfPolName" class="form-input" value="' + esc(p ? p.name : '') + '" maxlength="100" />') +
      field(Tf.t('tf_pol_kind'), '<select id="tfPolKind" class="form-input">' + kindOpts + '</select>') +
      field(Tf.t('tf_pol_desc'), '<input type="text" id="tfPolDesc" class="form-input" value="' + esc(p ? p.description || '' : '') + '" />', 'tf-full') +
      field(Tf.t('tf_pol_enf'), '<select id="tfPolEnf" class="form-input">' + enfOpts + '</select>') +
      field(Tf.t('tf_pol_scope'), '<select id="tfPolScopeDlg" class="form-input">' + scopeOpts + '</select>') +
      '<div id="tfPolMaxWrap" class="tf-field">' + field(Tf.t('tf_pol_max'), '<input type="number" id="tfPolMax" class="form-input" min="0" value="' + (params.max || 0) + '" />') + '</div>' +
      '<div id="tfPolTypesWrap" class="tf-field tf-full">' + field(Tf.t('tf_pol_types'), '<input type="text" id="tfPolTypes" class="form-input" value="' + esc((params.types || []).join(', ')) + '" />') + '</div>' +
      '<div id="tfPolTagsWrap" class="tf-field tf-full">' + field(Tf.t('tf_pol_tags'), '<input type="text" id="tfPolTags" class="form-input" value="' + esc((params.tags || []).join(', ')) + '" />') + '</div>' +
      '<label class="tf-check tf-full"><input type="checkbox" id="tfPolEnabled"' + ((p ? p.enabled : true) ? ' checked' : '') + ' /> <span>' + esc(Tf.t('tf_pol_enabled')) + '</span></label></div>';
    Tf.dlg.open({
      title: p ? Tf.t('tf_pol_edit') : Tf.t('tf_pol_new'), body: body, width: 620,
      footer: '<button type="button" class="btn btn-sm" data-dlg-close>' + esc(Tf.t('cancel', 'Cancel')) + '</button>' +
        '<button type="button" class="btn btn-sm btn-primary" id="tfPolSave">' + esc(Tf.t('save', 'Save')) + '</button>',
      onMount: function (root, dlg) {
        syncPolicyKind(root);
        root.querySelector('#tfPolKind').addEventListener('change', function () { syncPolicyKind(root); });
        root.querySelector('#tfPolSave').addEventListener('click', function () { savePolicy(id, dlg); });
      },
    });
  }

  // Shows only the inputs the chosen rule uses.
  function syncPolicyKind(root) {
    root = root || document;
    const k = root.querySelector('#tfPolKind').value;
    root.querySelector('#tfPolMaxWrap').style.display = k === 'max_deletes' || k === 'max_changes' ? '' : 'none';
    root.querySelector('#tfPolTypesWrap').style.display = k === 'deny_resource_types' || k === 'require_tags' ? '' : 'none';
    root.querySelector('#tfPolTagsWrap').style.display = k === 'require_tags' ? '' : 'none';
    const lbl = root.querySelector('#tfPolTypesWrap label');
    if (lbl) lbl.textContent = k === 'require_tags' ? Tf.t('tf_pol_types_opt') : Tf.t('tf_pol_types');
  }

  async function savePolicy(id, dlg) {
    const root = dlg.root;
    const name = root.querySelector('#tfPolName').value.trim();
    if (!name) { dlg.error(Tf.t('tf_pol_name_required')); return; }
    const kind = root.querySelector('#tfPolKind').value;
    const csv = function (sel) { return Tf.csv(root.querySelector(sel).value); };
    let params = {};
    if (kind === 'max_deletes' || kind === 'max_changes') params = { max: parseInt(root.querySelector('#tfPolMax').value, 10) || 0 };
    else if (kind === 'deny_resource_types') params = { types: csv('#tfPolTypes') };
    else if (kind === 'require_tags') { params = { tags: csv('#tfPolTags') }; const ty = csv('#tfPolTypes'); if (ty.length) params.types = ty; }
    const body = {
      name: name, description: root.querySelector('#tfPolDesc').value, kind: kind, params: params,
      enforcement: root.querySelector('#tfPolEnf').value, enabled: root.querySelector('#tfPolEnabled').checked,
      workspace_id: parseInt(root.querySelector('#tfPolScopeDlg').value, 10) || 0,
    };
    try {
      await (id ? Tf.api.put('/api/terraform/policies/' + id, body) : Tf.api.post('/api/terraform/policies', body));
    } catch (e) { dlg.error(errText(e)); return; }
    dlg.close();
    Tf.toast(Tf.t('tf_pol_saved'), 'success');
    if (NS.seg === 'policies') drawPolicies(document.getElementById('tfGovBody'));
  }

  async function deletePolicy(id) {
    const p = NS.policies.find(function (x) { return x.id === id; });
    const ok = await Tf.confirm({ title: Tf.t('delete', 'Delete'), message: Tf.t('tf_pol_delete_confirm', 'Delete policy "{name}"?', { name: p ? p.name : '#' + id }), confirmText: Tf.t('delete', 'Delete'), danger: true });
    if (!ok) return;
    try { await Tf.api.del('/api/terraform/policies/' + id); } catch (e) { Tf.toast(errText(e), 'error'); return; }
    Tf.toast(Tf.t('tf_pol_deleted'), 'success');
    drawPolicies(document.getElementById('tfGovBody'));
  }

  /* ================= audit ================= */
  function drawAudit(body) {
    if (!Tf.gateOn()) { body.innerHTML = Tf.ui.gateOff(Tf.t('tf_gov_audit_gate')); return; }
    const a = NS.audit;
    const levels = [['', Tf.t('tf_aud_all_levels')], ['info', 'INFO'], ['warn', 'WARN'], ['error', 'ERROR']];
    body.innerHTML = '<div class="tf-toolbar">' +
      '<select id="tfAuditWs" class="form-input" aria-label="' + esc(Tf.t('tf_aud_col_ws')) + '"><option value="">' + esc(Tf.t('tf_aud_all_ws')) + '</option>' +
      Tf.state.workspaces.map(function (w) { return '<option value="' + w.id + '"' + (String(w.id) === a.ws ? ' selected' : '') + '>' + esc(w.name) + '</option>'; }).join('') + '</select>' +
      '<select id="tfAuditLevel" class="form-input" aria-label="Level">' + levels.map(function (l) { return '<option value="' + l[0] + '"' + (l[0] === a.level ? ' selected' : '') + '>' + esc(l[1]) + '</option>'; }).join('') + '</select>' +
      '<input type="search" id="tfAuditSearch" class="form-input tf-grow" value="' + esc(a.q) + '" placeholder="' + esc(Tf.t('tf_aud_search')) + '" aria-label="' + esc(Tf.t('tf_aud_search')) + '" />' +
      '<button type="button" class="btn btn-sm" id="tfAuditReload" data-act="audit-refresh">' + ic('refresh-cw', 'icon-xs') + ' <span>' + esc(Tf.t('tf_aud_refresh')) + '</span></button></div>' +
      '<div class="card tf-audit-list" id="tfAuditList" aria-live="polite">' + Tf.ui.skeleton(4) + '</div>';
    ensureWorkspaces().then(function () { if (document.getElementById('tfAuditWs')) drawAuditWsOptions(); });
    loadAudit();
  }
  function drawAuditWsOptions() {
    const sel = document.getElementById('tfAuditWs');
    sel.innerHTML = '<option value="">' + esc(Tf.t('tf_aud_all_ws')) + '</option>' + Tf.state.workspaces.map(function (w) {
      return '<option value="' + w.id + '"' + (String(w.id) === NS.audit.ws ? ' selected' : '') + '>' + esc(w.name) + '</option>';
    }).join('');
  }

  async function loadAudit() {
    const list = document.getElementById('tfAuditList');
    if (!list) return;
    list.innerHTML = Tf.ui.skeleton(4);
    try {
      const q = NS.audit.ws ? '&workspace_id=' + encodeURIComponent(NS.audit.ws) : '';
      NS.audit.events = (await Tf.api.get('/api/terraform/audit?limit=200' + q)).events || [];
    } catch (e) { list.innerHTML = Tf.ui.error(errText(e)); return; }
    renderAudit();
  }

  function renderAudit() {
    const list = document.getElementById('tfAuditList');
    if (!list) return;
    const a = NS.audit, q = a.q.trim().toLowerCase();
    const rows = a.events.filter(function (e) {
      const lvl = String(e.level || 'info').toLowerCase();
      if (a.level && lvl.indexOf(a.level) !== 0) return false;
      if (q && String(e.message || '').toLowerCase().indexOf(q) < 0) return false;
      return true;
    });
    if (!rows.length) { list.innerHTML = Tf.ui.empty({ icon: 'activity', desc: Tf.t('tf_aud_empty') }); return; }
    list.innerHTML = '<div class="tf-audit-row tf-audit-head"><span>' + esc(Tf.t('tf_aud_col_time')) + '</span><span>' + esc(Tf.t('tf_aud_col_ws')) + '</span><span></span><span></span></div>' +
      rows.map(function (e) {
        const lvl = String(e.level || 'info').toLowerCase();
        const cls = lvl.indexOf('err') === 0 ? 'badge-danger' : lvl.indexOf('warn') === 0 ? 'badge-warning' : 'badge-info';
        return '<div class="tf-audit-row"><span class="tf-mono tf-sm tf-muted">' + esc(Tf.fmt.dt(e.time)) + '</span>' +
          '<span class="tf-sm">' + esc(e.workspace || '—') + '</span>' +
          '<span><span class="badge ' + cls + '">' + esc(String(e.level || 'info').toUpperCase()) + '</span></span>' +
          '<span class="tf-audit-msg">' + esc(e.message || '') + '</span></div>';
      }).join('');
  }

  /* ================= gate, CLI, alerts ================= */
  function drawGate(body) {
    const s = Tf.state.status || {};
    const on = !!s.enabled;
    const bins = (s.binaries || []).map(function (b) {
      return '<div class="tf-row">' + (b.err ? ic('circle-x', 'icon-xs') : ic('circle-check', 'icon-xs')) + ' <span>' + esc(b.name) + '</span> ' +
        '<span class="tf-mono tf-sm">' + esc(b.err || b.version || '') + '</span> <span class="tf-sm tf-muted">' + esc(b.path || '') + '</span></div>';
    }).join('');
    const inf = s.infracost || {};
    const infLine = '<div class="tf-row">' + (inf.available ? ic('circle-check', 'icon-xs') : ic('info', 'icon-xs')) + ' <span>' + esc(Tf.t('tf_gate_infracost')) + '</span> ' +
      '<span class="tf-mono tf-sm">' + esc(inf.available ? (inf.version || '') : Tf.t('tf_gate_not_installed')) + '</span></div>';
    body.innerHTML = '<div class="tf-gov-cards">' +
      '<div class="card tf-gov-card" id="tfGateCard"><div class="card-title">' + esc(Tf.t('tf_gate_card_title')) + '</div>' +
      '<div class="tf-hint tf-warn-text">' + esc(Tf.t('tf_gate_warn')) + '</div>' +
      '<div class="tf-stack">' + bins + infLine + '</div>' +
      '<div class="tf-row"><button type="button" class="btn btn-sm ' + (on ? '' : 'btn-primary') + '" id="tfGateAction" data-act="gate-toggle"' + (s.supported === false ? ' disabled' : '') + '>' +
      esc(Tf.t(on ? 'tf_gate_disable' : 'tf_gate_enable')) + '</button></div></div>' +
      '<div class="card tf-gov-card" id="tfCliUpdateCard"><div class="card-title">' + esc(Tf.t('tf_cli_title')) + '</div>' +
      '<div class="tf-hint">' + esc(Tf.t('tf_cli_hint')) + '</div>' +
      '<div class="tf-row"><button type="button" class="btn btn-sm btn-primary" id="tfCliUpdateCheck" data-act="cli-check"' + (on ? '' : ' disabled') + '>' + esc(Tf.t('tf_cli_check')) + '</button></div>' +
      '<div id="tfCliUpdateBody">' + (on ? '' : '<div class="tf-muted tf-sm">' + esc(Tf.t('tf_cli_gate')) + '</div>') + '</div>' +
      '<pre class="tf-cli-log" id="tfCliUpdateLog" hidden></pre></div>' +
      '<div class="card tf-gov-card" id="tfNotifyCard"><div class="card-title">' + esc(Tf.t('tf_nf_title')) + '</div>' +
      '<div class="tf-hint">' + esc(on ? Tf.t('tf_nf_hint') : Tf.t('tf_nf_gate')) + '</div>' +
      '<div class="tf-stack">' + ['awaiting_approval', 'errored', 'applied', 'drift'].map(function (k) {
        return '<label class="tf-check"><input type="checkbox" data-nf="' + k + '"' + (on ? '' : ' disabled') + ' /> <span>' + esc(Tf.t('tf_nf_ev_' + k)) + '</span></label>';
      }).join('') + '</div>' +
      '<div class="tf-row"><button type="button" class="btn btn-sm btn-primary" id="tfNfSave" data-act="notify-save"' + (on ? '' : ' disabled') + '>' + esc(Tf.t('tf_nf_save')) + '</button>' +
      '<button type="button" class="btn btn-sm" id="tfNfTest" data-act="notify-test"' + (on ? '' : ' disabled') + '>' + esc(Tf.t('tf_nf_test')) + '</button>' +
      '<span id="tfNfMsg" class="tf-sm" role="status"></span></div></div>' +
      '<div class="card tf-gov-card" id="tfRegSettingsCard"><div class="card-title">' + esc(Tf.t('tf_reg_title')) + '</div>' +
      '<div class="tf-hint">' + esc(on ? Tf.t('tf_reg_hint') : Tf.t('tf_reg_gate')) + '</div>' +
      '<input type="text" id="tfRegUrl" class="form-input tf-mono" maxlength="300" spellcheck="false" aria-label="' + esc(Tf.t('tf_reg_title')) + '"' + (on ? '' : ' disabled') + ' />' +
      '<div class="tf-row"><button type="button" class="btn btn-sm btn-primary" id="tfRegUrlSave" data-act="reg-save"' + (on ? '' : ' disabled') + '>' + esc(Tf.t('tf_reg_save')) + '</button>' +
      '<button type="button" class="btn btn-sm" id="tfRegUrlTest" data-act="reg-test"' + (on ? '' : ' disabled') + '>' + esc(Tf.t('tf_reg_test')) + '</button></div>' +
      '<span id="tfRegUrlStatus" class="tf-sm" role="status"></span></div></div>';
    if (on) { loadNotify(); loadRegistry(); }
  }

  async function toggleGate() {
    const on = !!(Tf.state.status && Tf.state.status.enabled);
    const ok = await Tf.confirm(on
      ? { title: Tf.t('tf_gate_confirm_disable_title'), message: Tf.t('tf_gate_confirm_disable_msg'), confirmText: Tf.t('tf_gate_disable'), danger: false }
      : { title: Tf.t('tf_gate_confirm_enable_title'), message: Tf.t('tf_gate_confirm_enable_msg'), confirmText: Tf.t('tf_gate_enable'), danger: true });
    if (!ok) return;
    try {
      Tf.state.status = await Tf.api.put('/api/terraform/status', { enabled: !on });
    } catch (e) { Tf.toast(Tf.t('tf_gate_failed') + ': ' + errText(e), 'error'); return; }
    Tf.toast(Tf.t(on ? 'tf_gate_toggled_off' : 'tf_gate_toggled_on'), 'success');
    await Tf.refresh();
  }

  // CLI update check: results are kept in NS.cli so a re-render shows the last check.
  function paintCli() {
    const el = document.getElementById('tfCliUpdateBody');
    if (!el) return;
    if (NS.cli.busy) { el.innerHTML = '<div class="tf-muted tf-sm">' + esc(Tf.t('tf_cli_checking')) + '</div>'; return; }
    el.innerHTML = NS.cli.results.map(cliResultHtml).join('');
  }
  function cliResultHtml(r) {
    const cls = r.status === 'update' ? 'badge-warning' : r.status === 'current' ? 'badge-healthy' : 'badge-danger';
    const label = { update: 'tf_cli_available', current: 'tf_cli_current', error: 'tf_cli_error' }[r.status] || 'tf_cli_unknown';
    const update = r.status === 'update'
      ? '<button type="button" class="btn btn-sm btn-primary" data-act="cli-install" data-target="' + esc(r.target) + '">' + esc(Tf.t('tf_cli_update')) + '</button>' : '';
    const link = r.release_url ? '<div class="tf-kv"><span>' + esc(Tf.t('tf_cli_release')) + '</span><a href="' + esc(r.release_url) + '" target="_blank" rel="noopener noreferrer">' + esc(r.release_url) + '</a></div>' : '';
    const at = r.checked_at ? '<div class="tf-kv"><span>' + esc(Tf.t('tf_cli_checked_at')) + '</span><span class="tf-mono">' + esc(Tf.fmt.dt(r.checked_at)) + '</span></div>' : '';
    const err = r.error ? '<div class="tf-error">' + ic('triangle-alert', 'icon-xs') + ' ' + esc(r.error) + '</div>' : '';
    return '<div class="tf-row tf-cli-row"><b class="tf-mono">' + esc(r.target) + '</b><span class="badge ' + cls + '">' + esc(Tf.t(label)) + '</span>' + update + '</div>' +
      '<div class="tf-kv"><span>' + esc(Tf.t('tf_cli_installed')) + '</span><span class="tf-mono">' + esc(r.installed || '—') + '</span></div>' +
      '<div class="tf-kv"><span>' + esc(Tf.t('tf_cli_latest')) + '</span><span class="tf-mono">' + esc(r.latest || '—') + '</span></div>' + link + at + err;
  }

  async function cliCheck() {
    if (NS.cli.busy) return;
    NS.cli.busy = true;
    paintCli();
    try {
      NS.cli.results = (await Tf.api.get('/api/terraform/update-check')).results || [];
    } catch (e) {
      // A failed request is a failed check, never "up to date".
      NS.cli.results = [{ target: 'terraform', status: 'error', error: errText(e) }];
    } finally { NS.cli.busy = false; paintCli(); }
  }

  // Installs one binary; the server streams NDJSON log lines while it works.
  async function cliInstall(target, btn) {
    const out = document.getElementById('tfCliUpdateLog');
    if (!out) return;
    out.hidden = false;
    out.textContent = '';
    btn.disabled = true;
    let error = '';
    try {
      const res = await fetch('/api/terraform/install', {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ target: target }), credentials: 'same-origin',
      });
      if (!(res.headers.get('content-type') || '').includes('application/x-ndjson')) {
        const d = await res.json().catch(function () { return {}; });
        error = d.error || Tf.t('tf_cli_install_failed');
      } else {
        const reader = res.body.getReader(), dec = new TextDecoder();
        let buf = '';
        const take = function (line) {
          let m;
          try { m = JSON.parse(line); } catch (e) { return; }
          if (m.log) { out.textContent += m.log; out.scrollTop = out.scrollHeight; }
          if (m.done) error = m.error || '';
        };
        for (;;) {
          const r = await reader.read();
          if (r.done) break;
          buf += dec.decode(r.value, { stream: true });
          let nl;
          while ((nl = buf.indexOf('\n')) >= 0) { take(buf.slice(0, nl)); buf = buf.slice(nl + 1); }
        }
        if (buf.trim()) take(buf);
      }
    } catch (e) { error = errText(e); }
    finally { btn.disabled = false; }
    if (error) { out.textContent += '\n' + error; Tf.toast(Tf.t('tf_cli_install_failed'), 'error'); }
    else Tf.toast(Tf.t('tf_cli_installed_ok'), 'success');
    await cliCheck();
  }

  async function loadNotify() {
    const card = document.getElementById('tfNotifyCard');
    if (!card) return;
    try {
      const r = await Tf.api.get('/api/terraform/notifications');
      const ev = r.events || {};
      card.querySelectorAll('[data-nf]').forEach(function (b) { b.checked = !!ev[b.getAttribute('data-nf')]; });
    } catch (e) { setMsg('tfNfMsg', errText(e), true); }
  }
  function setMsg(id, text, bad) {
    const m = document.getElementById(id);
    if (!m) return;
    m.textContent = text || '';
    m.style.color = bad ? 'var(--danger)' : 'var(--text-muted)';
  }
  async function saveNotify() {
    const events = {};
    document.querySelectorAll('#tfNotifyCard [data-nf]').forEach(function (b) { events[b.getAttribute('data-nf')] = b.checked; });
    try {
      await Tf.api.put('/api/terraform/notifications', { events: events });
      setMsg('tfNfMsg', '');
      Tf.toast(Tf.t('tf_nf_saved'), 'success');
    } catch (e) { setMsg('tfNfMsg', errText(e), true); }
  }
  async function testNotify(btn) {
    btn.disabled = true;
    setMsg('tfNfMsg', Tf.t('tf_nf_testing'));
    try {
      const r = await Tf.api.post('/api/terraform/notifications/test');
      if (r.ok !== false) { setMsg('tfNfMsg', ''); Tf.toast(Tf.t('tf_nf_test_ok'), 'success'); }
      else setMsg('tfNfMsg', Tf.t('tf_nf_test_fail', 'Test failed: {err}', { err: r.error || 'Error' }), true);
    } catch (e) { setMsg('tfNfMsg', Tf.t('tf_nf_test_fail', 'Test failed: {err}', { err: errText(e) }), true); }
    finally { btn.disabled = false; }
  }

  async function loadRegistry() {
    const input = document.getElementById('tfRegUrl');
    if (!input) return;
    try {
      const r = await Tf.api.get('/api/terraform/registry/settings');
      NS.regSaved = r.base_url === r.default_url ? '' : (r.base_url || '');
      input.value = NS.regSaved;
      input.placeholder = r.default_url || '';
    } catch (e) { setMsg('tfRegUrlStatus', errText(e), true); }
  }
  // Saves the URL; the registry browser re-reads the effective URL on its next open.
  async function saveRegistry() {
    const input = document.getElementById('tfRegUrl');
    const v = input.value.trim();
    try {
      await Tf.api.put('/api/terraform/registry/settings', { base_url: v });
    } catch (e) { setMsg('tfRegUrlStatus', errText(e), true); return false; }
    NS.regSaved = v;
    if (Tf.registry && Tf.registry.reset) Tf.registry.reset();
    setMsg('tfRegUrlStatus', '');
    Tf.toast(Tf.t('tf_reg_saved'), 'success');
    return true;
  }
  async function testRegistry(btn) {
    const input = document.getElementById('tfRegUrl');
    btn.disabled = true;
    setMsg('tfRegUrlStatus', Tf.t('tf_reg_testing'));
    try {
      if (input.value.trim() !== NS.regSaved && !(await saveRegistry())) return; // test what the user sees
      const s = await Tf.api.post('/api/terraform/registry/settings/test');
      if (s.ok) setMsg('tfRegUrlStatus', Tf.t('tf_reg_test_ok', 'Registry reachable ({ms} ms)', { ms: s.latency_ms || 0 }));
      else setMsg('tfRegUrlStatus', Tf.t('tf_reg_test_fail', 'Connection failed: {err}', { err: s.error || 'Error' }), true);
    } catch (e) { setMsg('tfRegUrlStatus', Tf.t('tf_reg_test_fail', 'Connection failed: {err}', { err: errText(e) }), true); }
    finally { btn.disabled = false; }
  }

  /* ================= workspace drawer: Settings tab ================= */
  const ACTIVE = ['queued', 'planning', 'applying', 'awaiting_approval'];
  const isProd = function (env) { return String(env || '').trim().toLowerCase() === 'production'; };

  // Same shape for the saved workspace and the form, so a JSON compare tells whether anything changed.
  function shape(w, v) {
    const o = {
      name: v.name, description: v.desc, group_name: v.group, environment: v.env, tags: v.tags, binary: v.binary,
      source: w.source, drift_interval_minutes: v.drift, plan_interval_minutes: v.plan, auto_apply: v.auto, cloud_account_ids: v.clouds,
    };
    if (w.source === 'inline') o.files = w.files || {};
    else {
      Object.assign(o, { git_url: v.gitUrl, git_branch: v.gitBranch, git_path: v.gitPath, git_auth: v.gitAuth });
      if (v.gitAuth === 'ssh') o.git_key_id = v.gitKey;
    }
    return o;
  }
  function savedValues(w) {
    return {
      name: w.name, desc: w.description || '', group: w.group_name || '', env: w.environment || '', tags: (w.tags || []).slice(), binary: w.binary || '',
      drift: Number(w.drift_interval_minutes) || 0, plan: Number(w.plan_interval_minutes) || 0, auto: !!w.auto_apply && !isProd(w.environment),
      clouds: (w.cloud_account_ids || []).map(Number).sort(function (a, b) { return a - b; }),
      gitUrl: w.git_url || '', gitBranch: w.git_branch || '', gitPath: w.git_path || '', gitAuth: w.git_auth || 'none', gitKey: Number(w.git_key_id) || 0,
    };
  }
  function formValues(body) {
    const val = function (id) { const el = body.querySelector('#' + id); return el ? el.value : ''; };
    const clouds = body.querySelectorAll('[data-ca-link]');
    return {
      name: val('tfSetName').trim(), desc: val('tfSetDesc'), group: val('tfSetGroup').trim(), env: val('tfSetEnv').trim(),
      tags: Tf.csv(val('tfSetTags')), binary: val('tfSetBinary'),
      drift: parseInt(val('tfSetDrift'), 10) || 0, plan: parseInt(val('tfSetPlan'), 10) || 0,
      auto: !!body.querySelector('#tfSetAutoApply') && body.querySelector('#tfSetAutoApply').checked && !isProd(val('tfSetEnv')),
      clouds: clouds.length ? Array.from(body.querySelectorAll('[data-ca-link]:checked')).map(function (c) { return Number(c.getAttribute('data-ca-link')); }).sort(function (a, b) { return a - b; }) : null,
      gitUrl: val('tfSetGitUrl').trim(), gitBranch: val('tfSetGitBranch').trim(), gitPath: val('tfSetGitPath').trim(), gitAuth: val('tfSetGitAuth') || 'none',
      gitKey: parseInt(val('tfSetGitKey'), 10) || 0, token: val('tfSetGitToken'),
    };
  }
  function settingsDirty(body) {
    if (!NS.settings) return false;
    const f = formValues(body);
    if (f.token) return true;
    const cur = NS.settings.w;
    const v = f.clouds === null ? Object.assign({}, f, { clouds: (cur.cloud_account_ids || []).map(Number).sort(function (a, b) { return a - b; }) }) : f;
    return JSON.stringify(shape(cur, v)) !== JSON.stringify(shape(cur, savedValues(cur)));
  }

  Tf.registerWsTab({
    id: 'settings', icon: 'settings', labelKey: 'tf_ws_tab_settings', fallback: 'Settings', order: 7,
    render: function (body, ws) {
      drawSettings(body, ws);
      // Unsaved settings block tab switches (Tf.workspaces.guard) and closing the drawer (canClose).
      const guard = function () { return Tf.governance.canLeaveSettings(); };
      Tf.workspaces.guard = guard;
      const top = Tf.drawer.current();
      if (top) top.canClose = guard;
    },
    onHide: function () {
      NS.settings = null; Tf.state.settingsDirty = null;
      if (Tf.workspaces) Tf.workspaces.guard = null;
      const top = Tf.drawer.current();
      if (top) top.canClose = null;
    },
  });

  // The drawer asks this before leaving the tab or closing; resolves true when it may proceed.
  Tf.governance.canLeaveSettings = async function () {
    const body = document.getElementById('tfWsBody');
    if (!NS.settings || !body || !settingsDirty(body)) return true;
    return (await Tf.confirm({ title: Tf.t('tf_set_leave_title'), message: Tf.t('tf_set_leave_msg'), confirmText: Tf.t('tf_set_leave'), danger: true })) === true;
  };
  Tf.state.settingsDirty = null;

  async function drawSettings(body, w) {
    NS.settings = { w: w, body: body };
    if (!Tf.state.status) await Tf.load.status();
    await ensureWorkspaces();
    const cloud = await loadCloud();
    const bins = (Tf.state.status && Tf.state.status.binaries && Tf.state.status.binaries.length) ? Tf.state.status.binaries : [{ name: 'terraform' }, { name: 'tofu' }];
    const groups = Array.from(new Set(Tf.state.workspaces.map(function (x) { return (x.group_name || '').trim(); }).filter(Boolean))).sort();
    const envs = Array.from(new Set(['production', 'staging', 'development'].concat(Tf.state.workspaces.map(function (x) { return (x.environment || '').trim(); }).filter(Boolean))));
    const linked = (w.cloud_account_ids || []).map(Number);
    const v = savedValues(w);
    const prod = isProd(w.environment);
    const inp = function (id, val, extra) { return '<input type="text" id="' + id + '" class="form-input" value="' + esc(val == null ? '' : val) + '" ' + (extra || '') + ' />'; };
    const sel = function (id, opts, cur) { return '<select id="' + id + '" class="form-input">' + opts.map(function (o) { return '<option value="' + esc(o[0]) + '"' + (String(o[0]) === String(cur) ? ' selected' : '') + '>' + esc(o[1]) + '</option>'; }).join('') + '</select>'; };
    const dl = function (id, items) { return '<datalist id="' + id + '">' + items.map(function (x) { return '<option value="' + esc(x) + '">'; }).join('') + '</datalist>'; };

    const general = '<section class="card tf-set-card"><div class="card-title">' + esc(Tf.t('tf_set_general')) + '</div><div class="tf-grid-2">' +
      field(Tf.t('tf_set_name') + ' *', inp('tfSetName', w.name, 'maxlength="100" required')) +
      field(Tf.t('tf_set_group'), inp('tfSetGroup', w.group_name, 'list="tfSetGroupList" placeholder="' + esc(Tf.t('tf_set_group_ph')) + '"') + dl('tfSetGroupList', groups)) +
      field(Tf.t('tf_set_env'), inp('tfSetEnv', w.environment, 'list="tfSetEnvList" placeholder="production"') + dl('tfSetEnvList', envs)) +
      field(Tf.t('tf_set_tags'), inp('tfSetTags', (w.tags || []).join(', '), 'placeholder="' + esc(Tf.t('tf_set_tags_ph')) + '"')) +
      field(Tf.t('tf_set_binary'), sel('tfSetBinary', bins.map(function (b) { return [b.name, b.name + (b.err ? ' (' + Tf.t('tf_gate_not_installed') + ')' : '')]; }), w.binary)) +
      field(Tf.t('tf_set_drift'), '<input type="number" id="tfSetDrift" class="form-input" min="0" value="' + v.drift + '" /><span class="tf-hint">' + esc(Tf.t('tf_set_drift_hint')) + '</span>') +
      field(Tf.t('tf_set_plan'), '<input type="number" id="tfSetPlan" class="form-input" min="0" value="' + v.plan + '" /><span class="tf-hint">' + esc(Tf.t('tf_set_plan_hint')) + '</span>') +
      '<div class="tf-field"><label class="tf-field-label">' + esc(Tf.t('tf_set_autoapply')) + '</label>' +
      '<label class="tf-check"><input type="checkbox" id="tfSetAutoApply"' + (v.auto ? ' checked' : '') + (prod ? ' disabled' : '') + ' /> <span>' + esc(Tf.t('tf_set_autoapply_hint')) + '</span></label>' +
      '<span class="tf-hint tf-warn-text" id="tfSetAutoWarn"' + (prod ? '' : ' hidden') + '>' + esc(Tf.t('tf_set_autoapply_prod')) + '</span></div>' +
      field(Tf.t('tf_set_desc'), '<textarea id="tfSetDesc" class="form-input" rows="2">' + esc(w.description || '') + '</textarea>', 'tf-full') +
      '</div></section>';

    let source;
    if (w.source === 'git') {
      source = '<section class="card tf-set-card"><div class="card-title">' + esc(Tf.t('tf_set_source')) + ' · Git</div><div class="tf-grid-2">' +
        field(Tf.t('tf_set_git_url'), inp('tfSetGitUrl', w.git_url, 'placeholder="https://github.com/org/infra.git"'), 'tf-full') +
        field(Tf.t('tf_set_git_branch'), inp('tfSetGitBranch', w.git_branch, 'placeholder="main"')) +
        field(Tf.t('tf_set_git_path'), inp('tfSetGitPath', w.git_path, 'placeholder="envs/prod"')) +
        field(Tf.t('tf_set_git_auth'), sel('tfSetGitAuth', [['none', Tf.t('tf_set_git_none')], ['token', Tf.t('tf_set_git_token')], ['ssh', 'SSH']], w.git_auth || 'none')) +
        '<div class="tf-field" id="tfSetTokenWrap">' + field(Tf.t('tf_set_git_token'), '<input type="password" id="tfSetGitToken" class="form-input" autocomplete="new-password" placeholder="' + esc(w.git_token_set ? Tf.t('tf_set_git_token_keep') : '') + '" />') + '</div>' +
        '<div class="tf-field" id="tfSetKeyWrap">' + field(Tf.t('tf_set_git_key'), '<select id="tfSetGitKey" class="form-input"></select>') + '</div>' +
        '</div><div class="tf-row"><button type="button" class="btn btn-sm" id="tfSetGitTest" data-act="vcs-test">' + ic('git-compare', 'icon-xs') + ' <span>' + esc(Tf.t('tf_set_vcs_test')) + '</span></button>' +
        '<span id="tfVcsResult" class="tf-sm" role="status"></span></div></section>';
    } else {
      const n = w.files ? Object.keys(w.files).length : 0;
      source = '<section class="card tf-set-card"><div class="card-title">' + esc(Tf.t('tf_set_source')) + '</div>' +
        '<div class="tf-muted tf-sm">' + esc(Tf.t('tf_set_inline', 'Inline source · {n} file(s)', { n: n })) + '</div>' +
        '<div class="tf-hint">' + esc(Tf.t('tf_set_inline_note')) + '</div>' +
        '<div class="tf-row"><button type="button" class="btn btn-sm" id="tfSetOpenConfig" data-act="open-config">' + ic('pencil', 'icon-xs') + ' <span>' + esc(Tf.t('tf_set_open_config')) + '</span></button></div></section>';
    }

    const cloudRows = cloud.accounts.length
      ? cloud.accounts.map(function (a) {
        return '<label class="tf-check tf-ca-linkrow"><input type="checkbox" data-ca-link="' + Number(a.id) + '"' + (linked.indexOf(Number(a.id)) >= 0 ? ' checked' : '') + ' /> <span>' + esc(a.name) + '</span> <span class="tf-muted tf-sm">' + esc(a.provider) + '</span></label>';
      }).join('')
      : '<div class="tf-muted tf-sm">' + esc(Tf.t('tf_set_cloud_none')) + '</div>';
    const cloudCard = '<section class="card tf-set-card" id="tfSetCloud"><div class="card-title">' + esc(Tf.t('tf_set_cloud')) + '</div>' +
      '<div class="tf-hint">' + esc(Tf.t('tf_set_cloud_hint')) + '</div><div class="tf-stack">' + cloudRows + '</div>' +
      '<div class="tf-row"><button type="button" class="btn btn-sm" data-act="cloud-manage">' + ic('cloud', 'icon-xs') + ' <span>' + esc(Tf.t('tf_set_cloud_manage')) + '</span></button></div></section>';

    const webhook = w.webhook_url ? location.origin + w.webhook_url : '';
    const side = '<section class="card tf-set-card"><div class="card-title">' + esc(Tf.t('tf_set_webhook')) + '</div>' +
      '<div class="tf-hint">' + esc(Tf.t('tf_set_webhook_hint')) + '</div>' +
      '<div class="tf-row"><input type="text" id="tfWhUrl" class="form-input tf-mono tf-grow" readonly value="' + esc(webhook) + '" />' +
      '<button type="button" class="btn btn-icon btn-sm" data-act="wh-copy" aria-label="' + esc(Tf.t('tf_set_copy')) + '">' + ic('copy', 'icon-xs') + '</button>' +
      '<button type="button" class="btn btn-sm" data-act="wh-rotate">' + ic('refresh-cw', 'icon-xs') + ' <span>' + esc(Tf.t('tf_set_rotate')) + '</span></button></div>' +
      '<div class="tf-hint">' + esc(Tf.t('tf_set_webhook_headers')) + '</div><div id="tfWhSecret"></div></section>' +
      '<section class="card tf-set-card tf-set-danger"><div class="card-title tf-warn-text">' + esc(Tf.t('tf_set_danger')) + '</div>' +
      '<div class="tf-row"><button type="button" class="btn btn-sm btn-danger" id="tfSetDelete" data-act="ws-delete" data-id="' + w.id + '">' + ic('trash-2', 'icon-xs') + ' <span>' + esc(Tf.t('tf_set_delete')) + '</span></button>' +
      '<span id="tfSetDeleteNote" class="tf-sm tf-muted" hidden>' + esc(Tf.t('tf_set_delete_run')) + '</span></div></section>';

    body.innerHTML = '<div class="tf-set-grid"><div class="tf-set-col">' + general + source + cloudCard + '</div><div class="tf-set-col">' + side + '</div></div>' +
      '<div class="tf-set-foot"><span id="tfSetDirty" class="tf-set-dirty tf-warn-text" hidden>' + esc(Tf.t('tf_set_dirty')) + '</span>' +
      '<span id="tfSetError" class="tf-set-error" role="alert"></span><span class="tf-row">' +
      '<button type="button" class="btn btn-sm" id="tfSetReset" data-act="set-reset">' + esc(Tf.t('tf_set_reset')) + '</button>' +
      '<button type="button" class="btn btn-sm btn-primary" id="tfSetSave" data-act="set-save">' + esc(Tf.t('tf_set_save')) + '</button></span></div>';

    if (w.source === 'git') {
      syncGitAuth(body);
      Tf.api.get('/api/ssh-keys').then(function (keys) {
        const s = body.querySelector('#tfSetGitKey');
        if (!s) return;
        s.innerHTML = (Array.isArray(keys) ? keys : []).map(function (k) {
          return '<option value="' + Number(k.id) + '"' + (Number(k.id) === Number(w.git_key_id) ? ' selected' : '') + '>' + esc(k.name) + '</option>';
        }).join('');
      }, function () { /* the field stays empty */ });
    }
    if (prod) body.querySelector('#tfSetAutoApply').checked = false;
    // Property handlers (not addEventListener): the drawer reuses this body element across renders.
    body.oninput = function (e) { if (e.target.id === 'tfSetEnv') syncProd(body); markDirty(body); };
    body.onchange = function (e) { if (e.target.id === 'tfSetGitAuth') syncGitAuth(body); markDirty(body); };
    body.onclick = function (e) { settingsClick(e, body); };
    checkRunActive(w, body);
    Tf.state.settingsDirty = function () { return settingsDirty(body); };
  }

  function syncGitAuth(body) {
    const a = body.querySelector('#tfSetGitAuth').value;
    body.querySelector('#tfSetTokenWrap').style.display = a === 'token' ? '' : 'none';
    body.querySelector('#tfSetKeyWrap').style.display = a === 'ssh' ? '' : 'none';
  }

  // Auto-apply can never be on for production; follow the Environment field live.
  function syncProd(body) {
    const prod = isProd(body.querySelector('#tfSetEnv').value), cb = body.querySelector('#tfSetAutoApply');
    if (!cb) return;
    if (prod) cb.checked = false;
    cb.disabled = prod;
    const warn = body.querySelector('#tfSetAutoWarn');
    if (warn) warn.hidden = !prod;
  }
  function markDirty(body) {
    const d = body.querySelector('#tfSetDirty');
    if (d) d.hidden = !settingsDirty(body);
  }

  // A delete is blocked while the newest run is still active; the note explains why.
  async function checkRunActive(w, body) {
    try {
      const r = await Tf.api.get('/api/terraform/workspaces/' + w.id + '/runs?limit=1');
      const latest = (r.runs || [])[0];
      const busy = !!latest && ACTIVE.indexOf(latest.status) >= 0;
      const btn = body.querySelector('#tfSetDelete'), note = body.querySelector('#tfSetDeleteNote');
      if (btn) btn.disabled = busy;
      if (note) note.hidden = !busy;
    } catch (e) { /* leave delete enabled; the server still refuses an active run */ }
  }

  async function loadCloud() {
    if (Tf.state.cloud.length) return { accounts: Tf.state.cloud };
    try {
      const r = await Tf.api.get('/api/terraform/cloud-accounts');
      Tf.state.cloud = r.accounts || [];
      return { accounts: Tf.state.cloud };
    } catch (e) { return { accounts: [] }; }
  }

  async function settingsClick(e, body) {
    const act = e.target.closest('[data-act]');
    if (!act) return;
    const a = act.getAttribute('data-act');
    const w = NS.settings.w;
    if (a === 'open-config') {
      const tab = document.querySelector('#tfWsTabs [data-tab="config"]');
      if (tab) tab.click();
    } else if (a === 'cloud-manage') {
      await Tf.drawer.close();
      Tf.navigate('variables', 'cloud');
    } else if (a === 'wh-copy') {
      Tf.copy(body.querySelector('#tfWhUrl').value);
    } else if (a === 'wh-rotate') rotateWebhook(w, body);
    else if (a === 'vcs-test') testVcs(w, body, act);
    else if (a === 'set-reset') drawSettings(body, w);
    else if (a === 'set-save') saveSettings(w, body, act);
    else if (a === 'ws-delete') deleteWorkspace(w);
  }

  async function saveSettings(w, body, btn) {
    const err = body.querySelector('#tfSetError');
    err.textContent = '';
    const f = formValues(body);
    if (!f.name) { err.textContent = Tf.t('tf_set_name_required'); return; }
    const payload = shape(w, f.clouds === null ? Object.assign({}, f, { clouds: (w.cloud_account_ids || []).map(Number) }) : f);
    if (w.source === 'git' && f.gitAuth === 'token' && f.token) payload.git_token = f.token;
    btn.disabled = true;
    try {
      await Tf.api.put('/api/terraform/workspaces/' + w.id, payload);
    } catch (e) { err.textContent = errText(e); return; } finally { btn.disabled = false; }
    Tf.toast(Tf.t('tf_set_saved'), 'success');
    // Re-read the saved workspace so the form and the drawer show what the server stored.
    const fresh = (await Tf.api.get('/api/terraform/workspaces/' + w.id)).workspace || w;
    await ensureWorkspaces(true);
    Tf.emit('workspaces');
    drawSettings(body, fresh);
  }

  function rotateWebhook(w, body) {
    Tf.confirm({ title: Tf.t('tf_set_rotate'), message: Tf.t('tf_set_rotate_confirm'), confirmText: Tf.t('tf_set_rotate'), danger: true }).then(async function (ok) {
      if (!ok) return;
      try {
        const r = await Tf.api.post('/api/terraform/workspaces/' + w.id + '/webhook/rotate');
        w.webhook_url = r.webhook_url;
        body.querySelector('#tfWhUrl').value = w.webhook_url ? location.origin + w.webhook_url : '';
        body.querySelector('#tfWhSecret').innerHTML = '<div class="tf-sm tf-stack">' + esc(Tf.t('tf_set_secret_once')) +
          '<div class="tf-row"><code class="tf-mono" style="word-break:break-all;">' + esc(r.webhook_secret) + '</code>' +
          '<button type="button" class="btn btn-icon btn-sm" data-act="copy-secret" aria-label="' + esc(Tf.t('tf_set_copy')) + '">' + ic('copy', 'icon-xs') + '</button></div></div>';
        body.querySelector('[data-act="copy-secret"]').addEventListener('click', function () { Tf.copy(r.webhook_secret); });
      } catch (e) { Tf.toast(errText(e), 'error'); }
    });
  }

  async function testVcs(w, body, btn) {
    const out = body.querySelector('#tfVcsResult');
    btn.disabled = true;
    out.style.color = '';
    out.textContent = Tf.t('tf_set_vcs_testing');
    try {
      const d = await Tf.api.post('/api/terraform/workspaces/' + w.id + '/vcs/test');
      out.style.color = d.ok ? 'var(--success)' : 'var(--danger)';
      out.textContent = d.ok ? Tf.t('tf_set_vcs_ok') + ' · ' + (d.branch || '') + ' ' + String(d.commit || '').slice(0, 10) : (d.error || 'failed');
    } catch (e) { out.style.color = 'var(--danger)'; out.textContent = errText(e); }
    finally { btn.disabled = false; }
  }

  async function deleteWorkspace(w) {
    const ok = await Tf.confirm({ title: Tf.t('tf_set_delete'), message: Tf.t('tf_set_delete_confirm', 'Delete workspace "{name}"? Its runs and state history are removed.', { name: w.name }), confirmText: Tf.t('tf_set_delete'), danger: true });
    if (!ok) return;
    try { await Tf.api.del('/api/terraform/workspaces/' + w.id); } catch (e) { Tf.toast(errText(e), 'error'); return; }
    NS.settings = null;
    Tf.state.settingsDirty = null;
    Tf.state.workspaces = Tf.state.workspaces.filter(function (x) { return x.id !== w.id; });
    Tf.toast(Tf.t('tf_set_deleted'), 'success');
    Tf.emit('workspaces');
    await Tf.drawer.close();
  }
})();
