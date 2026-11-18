/* Vault UI — Secrets, Templates, Sync Targets and Audit.
 * Plain IIFE (not an ESM) so it can be loaded as the /app/vault.js bundle alongside the other
 * app fragments. It uses the globals published by core/main.js: I18N, t, showToast, showConfirm,
 * openModal, closeModal, svgIcon, escapeHtml, langRerenderHooks.
 * Air-gapped: no bundler, no CDN, no external assets. */
(function () {
  'use strict';

  const V = (window.Vault = {});
  const state = { secrets: [], templates: [], targets: [], audit: [], status: null,
    secretId: null, templateId: null, targetId: null, knownKeys: [] };

  const esc = (s) => escapeHtml(String(s === null || s === undefined ? '' : s));
  const icon = (n, cls) => svgIcon(n, cls || 'icon-sm');
  const $ = (id) => document.getElementById(id);
  const fill = (s, vars) => Object.keys(vars).reduce((a, k) => a.split('{' + k + '}').join(vars[k]), s);

  const I = {
    en: {
      nav_secrets: 'Secrets',
      vault_title: 'Vault', vault_desc: 'Encrypted secrets, templates and sync targets for your fleet.',
      vault_open: 'Open Vault', vault_refresh: 'Refresh',
      vault_status_keys: 'Secrets', vault_status_expiring: 'Expiring soon', vault_status_targets: 'Sync targets',
      vault_status_master_ok: 'Master key set', vault_status_master_bad: 'Master key missing',
      vault_status_expiring_warn: 'secrets expire within 30 days',
      vault_tab_secrets: 'Secrets', vault_tab_templates: 'Templates', vault_tab_targets: 'Sync Targets', vault_tab_audit: 'Audit',
      vault_target_title: 'Sync targets', vault_target_name: 'Name', vault_target_kind: 'Kind', vault_drift: 'Drift',
      vault_key: 'Key', vault_value: 'Value', vault_scope: 'Scope', vault_scope_ref: 'Scope reference',
      vault_expires: 'Expires at', vault_rotation: 'Rotation (days)', vault_save: 'Save', vault_delete: 'Delete',
      vault_reveal: 'Reveal', vault_hide: 'Hide', vault_no_secrets: 'No secrets stored yet.',
      vault_value_keep: 'Leave blank to keep the current value',
      vault_template_title: 'Template editor', vault_template_name: 'Template name', vault_template_body: 'Body',
      vault_template_save: 'Save template', vault_template_render: 'Render', vault_template_rendered: 'Rendered preview',
      vault_template_validation_ok: 'All placeholders are known.', vault_template_unknown: 'Unknown key',
      vault_template_keys: 'Available keys', vault_no_templates: 'No templates saved yet.',
      vault_target_title: 'Sync targets', vault_target_name: 'Name', vault_target_kind: 'Kind',
      vault_target_template: 'Template', vault_target_ref: 'Target reference', vault_target_path: 'Remote path',
      vault_target_namespace: 'Namespace', vault_target_hook: 'Post-sync hook',
      vault_target_save: 'Save target', vault_target_sync: 'Sync now', vault_no_targets: 'No sync targets yet.',
      vault_audit_title: 'Recent activity', vault_no_audit: 'No audit entries yet.',
      vault_saved: 'Saved', vault_deleted: 'Deleted', vault_synced: 'Sync complete',
      vault_save_failed: 'Could not save: {why}', vault_load_failed: 'Could not load: {why}',
      vault_delete_confirm: 'Delete "{name}"?', vault_render_failed: 'Render failed: {why}',
      vault_sync_failed: 'Sync failed: {why}', vault_unknown_placeholder: 'Unknown placeholder'
    },
    id: {
      nav_secrets: 'Rahasia',
      vault_title: 'Vault', vault_desc: 'Rahasia terenkripsi, template, dan target sinkronisasi untuk armada Anda.',
      vault_open: 'Buka Vault', vault_refresh: 'Muat ulang',
      vault_status_keys: 'Rahasia', vault_status_expiring: 'Segera kedaluwarsa', vault_status_targets: 'Target sinkron',
      vault_status_master_ok: 'Kunci utama tersedia', vault_status_master_bad: 'Kunci utama tidak ada',
      vault_status_expiring_warn: 'rahasia kedaluwarsa dalam 30 hari',
      vault_tab_secrets: 'Rahasia', vault_tab_templates: 'Template', vault_tab_targets: 'Target Sinkron', vault_tab_audit: 'Audit',
      vault_add_secret: 'Rahasia baru', vault_edit_secret: 'Ubah rahasia',
      vault_key: 'Kunci', vault_value: 'Nilai', vault_scope: 'Cakupan', vault_scope_ref: 'Referensi cakupan',
      vault_expires: 'Kedaluwarsa', vault_rotation: 'Rotasi (hari)', vault_save: 'Simpan', vault_delete: 'Hapus',
      vault_reveal: 'Tampilkan', vault_hide: 'Sembunyikan', vault_no_secrets: 'Belum ada rahasia tersimpan.',
      vault_value_keep: 'Biarkan kosong untuk mempertahankan nilai saat ini',
      vault_template_title: 'Editor template', vault_template_name: 'Nama template', vault_template_body: 'Isi',
      vault_template_save: 'Simpan template', vault_template_render: 'Render', vault_template_rendered: 'Hasil render',
      vault_template_validation_ok: 'Semua placeholder dikenal.', vault_template_unknown: 'Kunci tidak dikenal',
      vault_template_keys: 'Kunci tersedia', vault_no_templates: 'Belum ada template tersimpan.',
      vault_target_title: 'Target sinkron', vault_target_name: 'Nama', vault_target_kind: 'Jenis',
      vault_target_template: 'Template', vault_target_ref: 'Referensi target', vault_target_path: 'Path jarak jauh',
      vault_target_namespace: 'Namespace', vault_target_hook: 'Hook pasca-sinkron',
      vault_target_save: 'Simpan target', vault_target_sync: 'Sinkronkan', vault_no_targets: 'Belum ada target sinkron.',
      vault_audit_title: 'Aktivitas terbaru', vault_no_audit: 'Belum ada entri audit.',
      vault_target_title: 'Target sinkron', vault_target_name: 'Nama', vault_target_kind: 'Jenis', vault_drift: 'Drift',
      vault_save_failed: 'Gagal menyimpan: {why}', vault_load_failed: 'Gagal memuat: {why}',
      vault_delete_confirm: 'Hapus "{name}"?', vault_render_failed: 'Render gagal: {why}',
      vault_sync_failed: 'Sinkron gagal: {why}', vault_unknown_placeholder: 'Placeholder tidak dikenal'
    }
  };
  ['en', 'id'].forEach((l) => { I18N[l] = Object.assign(I18N[l] || {}, I[l]); });

  async function api(method, path, body) {
    const opts = { method, headers: {} };
    if (body !== undefined) { opts.headers['Content-Type'] = 'application/json'; opts.body = JSON.stringify(body); }
    const res = await fetch('/api/vault' + path, opts);
    const data = await res.json().catch(() => ({}));
    return { ok: res.ok, status: res.status, data };
  }

  function failToast(key, r) {
    showToast(fill(t(key), { why: (r.data && r.data.error) || ('HTTP ' + r.status) }), 'error');
  }

  /* ---------- Status strip ---------- */
  function renderStatus() {
    const box = $('vaultStatusStrip');
    if (!box) return;
    const s = state.status || {};
    const keyOk = s.master_key_set
      ? '<span class="vault-status-ok">' + icon('shield-check') + esc(t('vault_status_master_ok')) + '</span>'
      : '<span class="vault-status-bad">' + icon('shield-alert') + esc(t('vault_status_master_bad')) + '</span>';
    const warn = (s.expiring_count || 0) > 0
      ? '<span class="vault-stat vault-stat-warn">' + icon('clock') +
        '<span class="vault-stat-num">' + (s.expiring_count) + '</span> ' + esc(t('vault_status_expiring_warn')) + '</span>'
      : '';
    box.innerHTML =
      '<span class="vault-stat">' + icon('key-round') + esc(t('vault_status_keys')) +
        ': <span class="vault-stat-num">' + (s.key_count || 0) + '</span></span>' +
      '<span class="vault-stat">' + icon('refresh-cw') + esc(t('vault_status_targets')) +
        ': <span class="vault-stat-num">' + (s.target_count || 0) + '</span></span>' + warn + keyOk;
  }

  /* ---------- Secrets ---------- */
  function renderSecrets() {
    const box = $('vaultSecretsList');
    if (!box) return;
    state.knownKeys = state.secrets.map((x) => x.key);
    if (!state.secrets.length) { box.innerHTML = '<div class="vault-empty">' + esc(t('vault_no_secrets')) + '</div>'; return; }
    let rows = '';
    state.secrets.forEach((x) => {
      const badge = x.needs_rotation
        ? ' <span class="vault-badge" data-drift="drift">' + esc(t('vault_status_expiring')) + '</span>' : '';
      rows += '<tr data-secret-row="' + x.id + '">' +
        '<td class="vault-mono">' + esc(x.key) + '</td>' +
        '<td>' + esc(x.scope) + (x.scope_ref ? ' / ' + esc(x.scope_ref) : '') + '</td>' +
        '<td class="vault-mono">v' + (x.version || 0) + badge + '</td>' +
        '<td class="vault-value-cell" data-value-id="' + x.id + '"><span class="vault-masked vault-mono">••••••••</span></td>' +
        '<td><span class="vault-actions">' +
          '<button type="button" class="btn btn-sm btn-ghost" data-action="reveal" data-id="' + x.id + '" title="' + esc(t('vault_reveal')) + '">' + icon('eye') + esc(t('vault_reveal')) + '</button>' +
          '<button type="button" class="btn btn-sm btn-ghost" data-action="edit-secret" data-id="' + x.id + '" title="' + esc(t('vault_edit_secret')) + '">' + icon('settings') + '</button>' +
        '</span></td></tr>';
    });
    box.innerHTML = '<table class="vault-table"><thead><tr>' +
      '<th>' + esc(t('vault_key')) + '</th><th>' + esc(t('vault_scope')) + '</th>' +
      '<th>' + esc(t('vault_rotation')) + '</th><th>' + esc(t('vault_value')) + '</th><th></th></tr></thead>' +
      '<tbody>' + rows + '</tbody></table>';
    renderKeyChips();
  }

  async function revealSecret(id, btn) {
    const r = await api('GET', '/secrets/' + id + '/reveal');
    if (!r.ok) { failToast('vault_load_failed', r); return; }
    const cell = document.querySelector('[data-value-id="' + id + '"]');
    if (cell) {
      cell.innerHTML = '<span class="vault-revealed vault-mono">' + esc(r.data.value) + '</span>';
    }
    if (btn) {
      btn.setAttribute('data-action', 'hide');
      btn.innerHTML = icon('eye-off') + esc(t('vault_hide'));
    }
  }

  function loadSecretIntoForm(x) {
    state.secretId = x.id;
    $('vaultSecretKey').value = x.key || '';
    $('vaultSecretKey').disabled = true;
    $('vaultSecretScope').value = x.scope || 'global';
    $('vaultSecretScopeRef').value = x.scope_ref || '';
    $('vaultSecretExpiresAt').value = x.expires_at ? String(x.expires_at).slice(0, 10) : '';
    $('vaultSecretRotationDays').value = x.rotation_days || 0;
    $('vaultSecretValue').value = '';
    $('vaultSecretValue').placeholder = t('vault_value_keep');
    $('vaultSecretDelete').style.display = '';
  }

  function resetSecretForm() {
    state.secretId = null;
    ['vaultSecretKey', 'vaultSecretValue', 'vaultSecretScopeRef', 'vaultSecretExpiresAt'].forEach((id) => { if ($(id)) $(id).value = ''; });
    if ($('vaultSecretKey')) $('vaultSecretKey').disabled = false;
    if ($('vaultSecretScope')) $('vaultSecretScope').value = 'global';
    if ($('vaultSecretRotationDays')) $('vaultSecretRotationDays').value = '0';
    if ($('vaultSecretValue')) $('vaultSecretValue').placeholder = '';
    if ($('vaultSecretDelete')) $('vaultSecretDelete').style.display = 'none';
  }

  async function saveSecret() {
    const key = $('vaultSecretKey').value.trim();
    const value = $('vaultSecretValue').value;
    const expires = $('vaultSecretExpiresAt').value || null;
    const rotation = parseInt($('vaultSecretRotationDays').value, 10) || 0;
    let r;
    if (state.secretId) {
      r = await api('PUT', '/secrets/' + state.secretId, { value, expires_at: expires, rotation_days: rotation });
    } else {
      r = await api('POST', '/secrets', {
        key, value, scope: $('vaultSecretScope').value, scope_ref: $('vaultSecretScopeRef').value.trim(),
        expires_at: expires, rotation_days: rotation
      });
    }
    if (!r.ok) { failToast('vault_save_failed', r); return; }
    showToast(t('vault_saved'), 'success');
    resetSecretForm();
    await refresh();
  }

  async function deleteSecret() {
    if (!state.secretId) return;
    const name = $('vaultSecretKey').value || ('#' + state.secretId);
    if (!await showConfirm({ message: fill(t('vault_delete_confirm'), { name }), confirmText: t('vault_delete') })) return;
    const r = await api('DELETE', '/secrets/' + state.secretId);
    if (!r.ok) { failToast('vault_load_failed', r); return; }
    showToast(t('vault_deleted'), 'success');
    resetSecretForm();
    await refresh();
  }

  /* ---------- Templates ---------- */
  function renderTemplates() {
    const box = $('vaultTemplatesList');
    if (box) {
      if (!state.templates.length) box.innerHTML = '<div class="vault-empty">' + esc(t('vault_no_templates')) + '</div>';
      else box.innerHTML = state.templates.map((x) =>
        '<button type="button" class="vault-key-chip" data-action="edit-template" data-id="' + x.id + '">' + esc(x.name) + '</button>').join('');
    }
    const sel = $('vaultTargetTemplate');
    if (sel) {
      const cur = sel.value;
      sel.innerHTML = '<option value="">—</option>' + state.templates.map((x) =>
        '<option value="' + x.id + '">' + esc(x.name) + '</option>').join('');
      sel.value = cur;
    }
    validateTemplate();
  }

  function loadTemplateIntoForm(x) {
    state.templateId = x.id;
    $('vaultTemplateName').value = x.name || '';
    $('vaultTemplateBody').value = x.body || '';
    validateTemplate();
  }

  function renderKeyChips() {
    const box = $('vaultTemplateKeys');
    if (!box) return;
    box.innerHTML = state.knownKeys.length
      ? state.knownKeys.map((k) => '<button type="button" class="vault-key-chip" data-action="insert-key" data-key="' + esc(k) + '">' + esc(k) + '</button>').join('')
      : '<span class="vault-hint">' + esc(t('vault_no_secrets')) + '</span>';
  }

  function placeholders(body) {
    const out = [];
    const re = /\{\{\s*vault\.([A-Za-z_][A-Za-z0-9_]*)\s*\}\}/g;
    let m;
    while ((m = re.exec(body)) !== null) if (out.indexOf(m[1]) < 0) out.push(m[1]);
    return out;
  }

  function validateTemplate() {
    const box = $('vaultTemplateValidation');
    const bodyEl = $('vaultTemplateBody');
    if (!box || !bodyEl) return [];
    const known = new Set(state.knownKeys);
    const issues = [];
    const lines = bodyEl.value.split('\n');
    lines.forEach((line, i) => {
      placeholders(line).forEach((k) => {
        if (!known.has(k)) issues.push({ line: i + 1, key: k });
      });
    });
    if (!issues.length) {
      box.innerHTML = '<span class="vault-valid">' + icon('circle-check') + esc(t('vault_template_validation_ok')) + '</span>';
      bodyEl.style.borderColor = '';
    } else {
      box.innerHTML = issues.map((x) =>
        '<div class="vault-invalid">' + icon('circle-alert') + esc(t('vault_unknown_placeholder')) +
        ' <span class="vault-invalid-mark vault-mono">' + esc(x.key) + '</span> (line ' + x.line + ')</div>').join('');
      bodyEl.style.borderColor = 'var(--danger)';
    }
    return issues;
  }

  function insertKey(key) {
    const el = $('vaultTemplateBody');
    if (!el) return;
    const token = '{{ vault.' + key + ' }}';
    const start = el.selectionStart, end = el.selectionEnd;
    el.value = el.value.slice(0, start) + token + el.value.slice(end);
    const pos = start + token.length;
    el.selectionStart = el.selectionEnd = pos;
    el.focus();
    validateTemplate();
  }

  async function saveTemplate() {
    const name = $('vaultTemplateName').value.trim();
    const body = $('vaultTemplateBody').value;
    const r = state.templateId
      ? await api('PUT', '/templates/' + state.templateId, { name, body })
      : await api('POST', '/templates', { name, body });
    if (!r.ok) { failToast('vault_save_failed', r); return; }
    showToast(t('vault_saved'), 'success');
    state.templateId = r.data.id;
    await refresh();
  }

  async function renderTemplate() {
    if (!state.templateId) { await saveTemplate(); if (!state.templateId) return; }
    const r = await api('GET', '/templates/' + state.templateId + '/render');
    const out = $('vaultTemplateRendered');
    if (!r.ok) {
      if (out) out.textContent = fill(t('vault_render_failed'), { why: (r.data && r.data.error) || ('HTTP ' + r.status) });
      failToast('vault_render_failed', r);
      return;
    }
    if (out) out.textContent = r.data.rendered !== undefined ? r.data.rendered : r.data.body;
  }

  /* ---------- Targets ---------- */
  function renderTargets() {
    const box = $('vaultTargetsList');
    if (!box) return;
    if (!state.targets.length) { box.innerHTML = '<div class="vault-empty">' + esc(t('vault_no_targets')) + '</div>'; return; }
    let rows = '';
    state.targets.forEach((x) => {
      const drift = x.drift_status || 'unknown';
      rows += '<tr data-target-row="' + x.id + '">' +
        '<td>' + esc(x.name) + '</td>' +
        '<td class="vault-mono">' + esc(x.kind) + '</td>' +
        '<td class="vault-mono">' + esc(x.target_ref) + '</td>' +
        '<td class="vault-mono">' + esc(x.path || '') + '</td>' +
        '<td><span class="vault-badge" data-drift="' + esc(drift) + '">' + esc(drift) + '</span></td>' +
        '<td><span class="vault-actions">' +
          '<button type="button" class="btn btn-sm btn-ghost" data-action="edit-target" data-id="' + x.id + '" title="' + esc(t('vault_target_save')) + '">' + icon('settings') + '</button>' +
        '</span></td></tr>';
    });
    box.innerHTML = '<table class="vault-table"><thead><tr>' +
      '<th>' + esc(t('vault_target_name')) + '</th><th>' + esc(t('vault_target_kind')) + '</th>' +
      '<th>' + esc(t('vault_target_ref')) + '</th><th>' + esc(t('vault_target_path')) + '</th>' +
      '<th>' + esc(t('vault_drift')) + '</th><th></th></tr></thead><tbody>' + rows + '</tbody></table>';
  }

  function loadTargetIntoForm(x) {
    state.targetId = x.id;
    $('vaultTargetName').value = x.name || '';
    $('vaultTargetKind').value = x.kind || 'host';
    $('vaultTargetTemplate').value = x.template_id ? String(x.template_id) : '';
    $('vaultTargetRef').value = x.target_ref || '';
    $('vaultTargetPath').value = x.path || '';
    $('vaultTargetNamespace').value = x.namespace || '';
    $('vaultTargetHook').value = x.post_sync_hook || '';
    $('vaultTargetDelete').style.display = '';
  }

  function resetTargetForm() {
    state.targetId = null;
    ['vaultTargetName', 'vaultTargetRef', 'vaultTargetPath', 'vaultTargetNamespace', 'vaultTargetHook'].forEach((id) => { if ($(id)) $(id).value = ''; });
    if ($('vaultTargetKind')) $('vaultTargetKind').value = 'host';
    if ($('vaultTargetTemplate')) $('vaultTargetTemplate').value = '';
    if ($('vaultTargetDelete')) $('vaultTargetDelete').style.display = 'none';
  }

  async function saveTarget() {
    const payload = {
      name: $('vaultTargetName').value.trim(),
      kind: $('vaultTargetKind').value,
      template_id: parseInt($('vaultTargetTemplate').value, 10) || 0,
      target_ref: $('vaultTargetRef').value.trim(),
      path: $('vaultTargetPath').value.trim(),
      namespace: $('vaultTargetNamespace').value.trim(),
      post_sync_hook: $('vaultTargetHook').value.trim()
    };
    const r = state.targetId
      ? await api('PUT', '/targets/' + state.targetId, payload)
      : await api('POST', '/targets', payload);
    if (!r.ok) { failToast('vault_save_failed', r); return; }
    showToast(t('vault_saved'), 'success');
    state.targetId = r.data.id;
    await refresh();
  }

  async function syncTarget() {
    if (!state.targetId) { showToast(t('vault_target_save'), 'error'); return; }
    const r = await api('POST', '/targets/' + state.targetId + '/sync');
    if (!r.ok) { failToast('vault_sync_failed', r); await refresh(); return; }
    showToast(t('vault_synced') + (r.data.sha256 ? ' · ' + r.data.sha256.slice(0, 12) : ''), 'success');
    await refresh();
  }

  async function deleteTarget() {
    if (!state.targetId) return;
    const name = $('vaultTargetName').value || ('#' + state.targetId);
    if (!await showConfirm({ message: fill(t('vault_delete_confirm'), { name }), confirmText: t('vault_delete') })) return;
    const r = await api('DELETE', '/targets/' + state.targetId);
    if (!r.ok) { failToast('vault_load_failed', r); return; }
    showToast(t('vault_deleted'), 'success');
    resetTargetForm();
    await refresh();
  }

  /* ---------- Audit ---------- */
  function renderAudit() {
    const box = $('vaultAuditList');
    if (!box) return;
    if (!state.audit.length) { box.innerHTML = '<div class="vault-empty">' + esc(t('vault_no_audit')) + '</div>'; return; }
    box.innerHTML = state.audit.slice(0, 50).map((x) =>
      '<div class="vault-audit-row"><span class="vault-audit-action">' + esc(x.action) + '</span>' +
      '<span class="vault-audit-detail">' + esc(x.detail) + '</span></div>').join('');
  }

  /* ---------- Refresh / init ---------- */
  V.refresh = async function refresh() {
    const results = await Promise.all([
      api('GET', '/status'), api('GET', '/secrets'), api('GET', '/templates'),
      api('GET', '/targets'), api('GET', '/audit')
    ]);
    if (results[0].ok) state.status = results[0].data;
    if (results[1].ok) state.secrets = results[1].data || [];
    if (results[2].ok) state.templates = results[2].data || [];
    if (results[3].ok) state.targets = results[3].data || [];
    if (results[4].ok) state.audit = results[4].data || [];
    renderStatus(); renderSecrets(); renderTemplates(); renderTargets(); renderAudit();
  };

  V.openModal = function () { V.refresh(); window.openModal('vaultModal'); };
  V.closeModal = function () { window.closeModal('vaultModal'); };
  V.init = function () {
    // Wire the form buttons and delegated click actions once.
    const bind = (id, fn) => { const el = $(id); if (el) el.addEventListener('click', fn); };
    bind('vaultSecretSave', saveSecret);
    bind('vaultSecretDelete', deleteSecret);
    bind('vaultTemplateSave', saveTemplate);
    bind('vaultTemplateRender', renderTemplate);
    bind('vaultTargetSave', saveTarget);
    bind('vaultTargetSync', syncTarget);
    bind('vaultTargetDelete', deleteTarget);

    const body = $('vaultTemplateBody');
    if (body) body.addEventListener('input', validateTemplate);

    document.addEventListener('click', async (e) => {
      const target = e.target;
      const el = target.closest ? target.closest('[data-action]') : null;
      if (!el || !$('vaultModal')) return;
      const action = el.getAttribute('data-action');
      const id = parseInt(el.getAttribute('data-id'), 10);
      if (action === 'reveal') { revealSecret(id, el); }
      else if (action === 'hide') {
        const cell = document.querySelector('[data-value-id="' + id + '"]');
        if (cell) cell.innerHTML = '<span class="vault-masked vault-mono">••••••••</span>';
        el.setAttribute('data-action', 'reveal');
        el.innerHTML = icon('eye') + esc(t('vault_reveal'));
      } else if (action === 'edit-secret') {
        const x = state.secrets.find((s) => s.id === id); if (x) loadSecretIntoForm(x);
      } else if (action === 'edit-template') {
        const x = state.templates.find((s) => s.id === id); if (x) loadTemplateIntoForm(x);
      } else if (action === 'insert-key') { insertKey(el.getAttribute('data-key')); }
      else if (action === 'edit-target') {
        const x = state.targets.find((s) => s.id === id); if (x) loadTargetIntoForm(x);
      }
    });
  };

  V.switchTab = function switchVaultTab(tab) {
    document.querySelectorAll('#vaultModal .vault-tab').forEach((b) => {
      b.classList.toggle('active', b.getAttribute('data-tab') === tab);
    });
    document.querySelectorAll('#vaultModal .vault-panel').forEach((p) => {
      p.classList.toggle('active', p.id === 'vaultPanel' + tab.charAt(0).toUpperCase() + tab.slice(1));
    });
  };
  window.switchVaultTab = V.switchTab;

  V.init();
  if (typeof langRerenderHooks !== 'undefined') langRerenderHooks.push(V.refresh);
  else if (typeof S !== 'undefined' && S.langRerenderHooks) S.langRerenderHooks.push(V.refresh);
})();
