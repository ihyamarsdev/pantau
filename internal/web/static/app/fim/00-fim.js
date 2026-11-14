/* File Integrity Monitoring (FIM) helpers for the Desired State rule form.
 * Adds the "file_integrity" rule kind: a path/preset target plus an explicit
 * "Capture baseline" action that previews the manifest before it is accepted.
 * Relies on index.html globals: t, I18N, escapeHtml, svgIcon, showToast, showConfirm,
 * openModal, closeModal, currentHost. */
(function () {
  'use strict';

  const FIM = (window.FIM = {});

  const I = {
    en: {
      fim_target_hint: 'Absolute file paths (comma separated, max 20), "preset:auth", or "suid"',
      fim_preset_label: 'Preset',
      fim_preset_custom: 'Custom paths',
      fim_preset_auth: 'Authentication files (passwd, group, sudoers, sshd_config, authorized_keys)',
      fim_preset_suid: 'SUID/SGID binaries on the host',
      fim_paths_label: 'File paths',
      fim_paths_ph: '/etc/passwd, /etc/ssh/sshd_config',
      fim_capture: 'Capture baseline',
      fim_capturing: 'Reading files…',
      fim_preview_title: 'Captured baseline',
      fim_preview_intro: 'These files will be tracked. Review the list, then confirm to store it as the rule baseline.',
      fim_preview_empty: 'No file could be read for this target.',
      fim_confirm: 'Use this baseline',
      fim_recapture: 'Capture again',
      fim_accepted: 'Baseline captured ({n} files). It is stored when you save the rule.',
      fim_need_capture: 'Capture a baseline before saving this rule.',
      fim_capture_failed: 'Could not capture the baseline',
      fim_drift_modified: 'Modified',
      fim_drift_removed: 'Missing',
      fim_drift_added: 'Added',
      fim_not_assessed: 'Not assessed',
      fim_diff: 'Compare with baseline',
      fim_diff_none: 'No difference with the baseline',
      fim_diff_failed: 'Could not compare with the baseline',
      fim_rule_short: 'Baseline: {n} file(s)'
    },
    id: {
      fim_target_hint: 'Path file absolut (pisahkan dengan koma, maks 20), "preset:auth", atau "suid"',
      fim_preset_label: 'Preset',
      fim_preset_custom: 'Path kustom',
      fim_preset_auth: 'Berkas autentikasi (passwd, group, sudoers, sshd_config, authorized_keys)',
      fim_preset_suid: 'Biner SUID/SGID pada host',
      fim_paths_label: 'Path file',
      fim_paths_ph: '/etc/passwd, /etc/ssh/sshd_config',
      fim_capture: 'Ambil baseline',
      fim_capturing: 'Membaca berkas…',
      fim_preview_title: 'Baseline yang diambil',
      fim_preview_intro: 'Berkas-berekas ini akan dipantau. Periksa daftarnya, lalu konfirmasi untuk menyimpannya sebagai baseline aturan.',
      fim_preview_empty: 'Tidak ada berkas yang dapat dibaca untuk target ini.',
      fim_confirm: 'Gunakan baseline ini',
      fim_recapture: 'Ambil ulang',
      fim_accepted: 'Baseline diambil ({n} berkas). Baseline disimpan saat aturan disimpan.',
      fim_need_capture: 'Ambil baseline sebelum menyimpan aturan ini.',
      fim_capture_failed: 'Gagal mengambil baseline',
      fim_drift_modified: 'Berubah',
      fim_drift_removed: 'Hilang',
      fim_drift_added: 'Ditambahkan',
      fim_not_assessed: 'Tidak dinilai',
      fim_diff: 'Bandingkan dengan baseline',
      fim_diff_none: 'Tidak ada perbedaan dengan baseline',
      fim_diff_failed: 'Gagal membandingkan dengan baseline',
      fim_rule_short: 'Baseline: {n} berkas'
    }
  };
  ['en', 'id'].forEach((l) => { I18N[l] = Object.assign(I18N[l] || {}, I[l]); });

  const esc = (s) => escapeHtml(String(s === null || s === undefined ? '' : s));
  const fill = (s, vars) => Object.keys(vars).reduce((a, k) => a.split('{' + k + '}').join(vars[k]), s);

  // pending holds the previewed manifest until the operator confirms it.
  let pending = null;

  // fimTargetValue returns the target the user asked for: the preset keyword or the raw paths.
  function fimTargetValue() {
    const preset = document.getElementById('fimPreset')?.value || 'custom';
    if (preset === 'auth') return 'preset:auth';
    if (preset === 'suid') return 'suid';
    return (document.getElementById('fimPaths')?.value || '').trim();
  }

  // fimShowPanel shows or hides the FIM section of the rule form.
  function fimShowPanel(visible) {
    const panel = document.getElementById('fimPanel');
    if (panel) panel.style.display = visible ? '' : 'none';
  }

  function fimSyncPresetVisibility() {
    const preset = document.getElementById('fimPreset')?.value || 'custom';
    const paths = document.getElementById('fimPathsWrap');
    if (paths) paths.style.display = preset === 'custom' ? '' : 'none';
  }

  // fimCaptureBaseline reads the manifest from the host and shows the preview for confirmation.
  async function fimCaptureBaseline() {
    const hostID = document.getElementById('ruleHostId')?.value;
    const target = fimTargetValue();
    if (!hostID) return;
    if (!target) {
      showToast(t('fill_all_fields', 'Please fill in all fields'), 'error');
      return;
    }
    const btn = document.getElementById('btnFIMCapture');
    if (btn) { btn.disabled = true; btn.innerHTML = SP.html(t('fim_capturing')); }
    try {
      const res = await fetch('/api/hosts/' + hostID + '/rules/fim/baseline', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ target })
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok || !data.ok) {
        showToast(t('fim_capture_failed', 'Could not capture the baseline') + ': ' + (data.error || res.status), 'error');
        return;
      }
      fimShowPreview(data);
    } catch (e) {
      showToast(t('fim_capture_failed', 'Could not capture the baseline') + ': ' + e.message, 'error');
    } finally {
      if (btn) {
        btn.disabled = false;
        btn.innerHTML = svgIcon('shield-check', 'icon-sm') + ' ' + esc(t('fim_capture'));
      }
    }
  }

  // fimShowPreview renders the captured manifest in the modal; nothing is stored yet.
  function fimShowPreview(data) {
    const box = document.getElementById('fimPreview');
    const files = Array.isArray(data.files) ? data.files : [];
    const rows = files.map((f) => `<li style="font-family: var(--font-mono); font-size: var(--fs-xs);">${esc(f.path)} <span style="color: var(--text-muted);">${esc(String(f.hash || '').slice(0, 12))}…</span></li>`).join('');
    const missing = (data.missing || []).length
      ? `<div style="font-size: var(--fs-xs); color: var(--text-muted); margin-top: 6px;">${esc(t('fim_drift_removed'))}: ${esc((data.missing || []).join(', '))}</div>`
      : '';
    const denied = (data.denied || []).length
      ? `<div style="font-size: var(--fs-xs); color: var(--text-danger, var(--text-muted)); margin-top: 6px;">${esc(t('fim_not_assessed'))}: ${esc((data.denied || []).join(', '))}</div>`
      : '';
    if (box) {
      box.innerHTML = `
        <div style="font-size: var(--fs-xs); color: var(--text-muted); margin-bottom: 6px;">${esc(t('fim_preview_intro'))}</div>
        <ul style="margin: 0; padding-left: 18px; max-height: 180px; overflow-y: auto;">${rows || '<li>' + esc(t('fim_preview_empty')) + '</li>'}</ul>
        ${missing}${denied}
        <div style="display: flex; gap: 8px; margin-top: 10px; justify-content: flex-end;">
          <button class="btn btn-sm btn-ghost" onclick="FIM.recapture()">${esc(t('fim_recapture'))}</button>
          <button class="btn btn-sm btn-primary" onclick="FIM.accept()">${esc(t('fim_confirm'))}</button>
        </div>`;
      box.style.display = '';
    }
    pending = { expected: data.expected || '', target: data.target || fimTargetValue(), count: files.length };
  }

  // fimAccept stores the confirmed manifest as the rule's expected value.
  function fimAccept() {
    if (!pending || !pending.expected) {
      showToast(t('fim_need_capture', 'Capture a baseline before saving this rule.'), 'error');
      return;
    }
    document.getElementById('fimCapturedExpected').value = pending.expected;
    document.getElementById('fimCapturedTarget').value = pending.target;
    document.getElementById('fimCapturedCount').value = String(pending.count);
    const target = document.getElementById('ruleTarget');
    if (target) target.value = pending.target;
    const exp = document.getElementById('ruleExpected');
    if (exp) exp.value = pending.expected;
    const box = document.getElementById('fimPreview');
    if (box) box.style.display = 'none';
    showToast(fill(t('fim_accepted'), { n: pending.count }), 'success');
  }

  // fimRecapture clears the accepted manifest so a new one must be captured.
  function fimRecapture() {
    pending = null;
    document.getElementById('fimCapturedExpected').value = '';
    document.getElementById('fimCapturedTarget').value = '';
    document.getElementById('fimCapturedCount').value = '';
    const box = document.getElementById('fimPreview');
    if (box) { box.innerHTML = ''; box.style.display = 'none'; }
    fimCaptureBaseline();
  }

  // fimReset clears the panel state when the rule form opens.
  function fimReset() {
    pending = null;
    const set = (id, v) => { const el = document.getElementById(id); if (el) el.value = v; };
    set('fimCapturedExpected', '');
    set('fimCapturedTarget', '');
    set('fimCapturedCount', '0');
    set('fimPaths', '');
    const preset = document.getElementById('fimPreset');
    if (preset) preset.value = 'custom';
    const box = document.getElementById('fimPreview');
    if (box) { box.innerHTML = ''; box.style.display = 'none'; }
    fimSyncPresetVisibility();
  }

  // fimLoadRule fills the panel from an existing rule (edit mode).
  function fimLoadRule(kind, target, expected) {
    if (kind !== 'file_integrity') return;
    const preset = document.getElementById('fimPreset');
    if (target === 'preset:auth' || target === 'suid') {
      if (preset) preset.value = target === 'suid' ? 'suid' : 'auth';
    } else {
      if (preset) preset.value = 'custom';
      const paths = document.getElementById('fimPaths');
      if (paths) paths.value = target || '';
    }
    fimSyncPresetVisibility();
    const set = (id, v) => { const el = document.getElementById(id); if (el) el.value = v; };
    set('fimCapturedExpected', expected || '');
    set('fimCapturedTarget', target || '');
    const manifest = (expected || '').split('|m:')[1];
    set('fimCapturedCount', manifest ? String(manifest.split(',').filter(Boolean).length) : '0');
  }

  // fimRuleValues returns the target/expected the rule form must submit for a FIM rule.
  // It returns ok=false when no baseline was captured for the current target.
  function fimRuleValues() {
    const target = fimTargetValue();
    const capturedTarget = document.getElementById('fimCapturedTarget')?.value || '';
    const expected = document.getElementById('fimCapturedExpected')?.value || '';
    if (!target) return { ok: false, reason: 'empty' };
    if (!expected || capturedTarget !== target) return { ok: false, reason: 'no_baseline' };
    return { ok: true, target, expected };
  }

  // fimExpectedSummary shortens a stored baseline for the rules table.
  FIM.expectedSummary = function (expected) {
    const e = String(expected || '');
    const digest = e.startsWith('sha256:') ? e.slice(7, 19) : '';
    const manifest = e.split('|m:')[1];
    const n = manifest ? manifest.split(',').filter(Boolean).length : 0;
    return fill(t('fim_rule_short', 'Baseline: {n} file(s)'), { n }) + (digest ? ' sha256:' + digest + '…' : '');
  };

  // applyI18n translates the FIM panel. The bundle loads after index.html ran setLanguage(),
  // so the panel labels must be translated here and again on every language switch.
  function applyI18n() {
    const panel = document.getElementById('fimPanel');
    if (!panel) return;
    panel.querySelectorAll('[data-i18n]').forEach((el) => {
      const v = t(el.getAttribute('data-i18n'));
      if (v) el.innerHTML = v;
    });
    panel.querySelectorAll('[data-i18n-placeholder]').forEach((el) => {
      const v = t(el.getAttribute('data-i18n-placeholder'));
      if (v) el.placeholder = v;
    });
  }
  applyI18n();
  if (typeof langRerenderHooks !== 'undefined') langRerenderHooks.push(applyI18n);
  FIM.captureBaseline = fimCaptureBaseline;
  FIM.accept = fimAccept;
  FIM.recapture = fimRecapture;
  FIM.reset = fimReset;
  FIM.loadRule = fimLoadRule;
  FIM.targetValue = fimTargetValue;
  FIM.showPanel = fimShowPanel;
  FIM.syncPresetVisibility = fimSyncPresetVisibility;
  FIM.ruleValues = fimRuleValues;
})();
