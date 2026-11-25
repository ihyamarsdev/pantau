/* Workspace drawer tabs: Overview (banners, pips, outputs, config summary) and Drift.
   Cross-module calls are guarded: Tf.runs.openViewer / startDialog / renderDiff, Tf.workspaces.unlock / forceUnlock / checkDrift, Tf.openWorkspace. */
(function () {
  'use strict';

  const Tf = window.Tf;
  Tf.addI18n({
    en: {
      tf_ws_tab_overview: 'Overview', tf_ws_tab_drift: 'Drift',
      tf_ov_approval: '{n} run awaiting approval', tf_ov_approve: 'Review & Approve Run #{id}',
      tf_ov_locked: 'Locked {by}', tf_ov_unlock: 'Unlock', tf_ov_force_unlock: 'Force Unlock',
      tf_ov_by_manual: 'manually', tf_ov_by_run: 'by run #{id}',
      tf_ov_drift_banner: 'Drift detected since the last check', tf_ov_view_drift: 'View Drift', tf_ov_replan: 'Re-plan',
      tf_ov_runs: 'Recent runs', tf_ov_runs_empty: 'No runs yet', tf_ov_latest: 'Latest: #{id} · {type} · {trigger}',
      tf_ov_outputs: 'Outputs', tf_ov_no_outputs: 'No outputs', tf_ov_reveal: 'Reveal sensitive', tf_ov_hide: 'Hide sensitive',
      tf_ov_copy: 'Copy', tf_ov_sensitive: 'sensitive value hidden', tf_ov_reveal_failed: 'Could not reveal the value',
      tf_ov_config: 'Configuration & security', tf_ov_env: 'Environment', tf_ov_binary: 'Binary', tf_ov_source: 'Source',
      tf_ov_source_inline: 'Inline files', tf_ov_cloud: 'Cloud accounts', tf_ov_no_cloud: 'None linked',
      tf_ov_schedules: 'Schedules', tf_ov_drift_every: 'Drift check every {n} min', tf_ov_plan_every: 'Plan every {n} min',
      tf_ov_off: 'off', tf_ov_auto_apply: 'Auto-apply on', tf_ov_auto_apply_off: 'Auto-apply off', tf_ov_scan: 'IaC security scan',
      tf_drift_none_alert: 'No drift', tf_drift_detected_alert: 'Drift Detected',
      tf_drift_checked: 'Last check', tf_drift_never: 'never', tf_drift_interval: 'Interval', tf_drift_check_now: 'Check Drift Now',
      tf_drift_clean: 'Infrastructure matches the configuration.', tf_drift_changes: 'Resources that differ from the configuration',
      tf_drift_diff_missing: 'Diff viewer is not available.', tf_ov_loading: 'Loading…',
    },
    id: {
      tf_ws_tab_overview: 'Ringkasan', tf_ws_tab_drift: 'Drift',
      tf_ov_approval: '{n} run menunggu persetujuan', tf_ov_approve: 'Tinjau & Setujui Run #{id}',
      tf_ov_locked: 'Terkunci {by}', tf_ov_unlock: 'Buka Kunci', tf_ov_force_unlock: 'Paksa Buka Kunci',
      tf_ov_by_manual: 'secara manual', tf_ov_by_run: 'oleh run #{id}',
      tf_ov_drift_banner: 'Drift terdeteksi sejak pemeriksaan terakhir', tf_ov_view_drift: 'Lihat Drift', tf_ov_replan: 'Rencanakan Ulang',
      tf_ov_runs: 'Run terbaru', tf_ov_runs_empty: 'Belum ada run', tf_ov_latest: 'Terbaru: #{id} · {type} · {trigger}',
      tf_ov_outputs: 'Output', tf_ov_no_outputs: 'Tidak ada output', tf_ov_reveal: 'Tampilkan yang sensitif', tf_ov_hide: 'Sembunyikan yang sensitif',
      tf_ov_copy: 'Salin', tf_ov_sensitive: 'nilai sensitif disembunyikan', tf_ov_reveal_failed: 'Nilai tidak dapat ditampilkan',
      tf_ov_config: 'Konfigurasi & keamanan', tf_ov_env: 'Environment', tf_ov_binary: 'Binary', tf_ov_source: 'Sumber',
      tf_ov_source_inline: 'Berkas inline', tf_ov_cloud: 'Akun cloud', tf_ov_no_cloud: 'Belum ada yang ditautkan',
      tf_ov_schedules: 'Jadwal', tf_ov_drift_every: 'Cek drift tiap {n} menit', tf_ov_plan_every: 'Plan tiap {n} menit',
      tf_ov_off: 'nonaktif', tf_ov_auto_apply: 'Auto-apply aktif', tf_ov_auto_apply_off: 'Auto-apply nonaktif', tf_ov_scan: 'Pemindaian keamanan IaC',
      tf_drift_none_alert: 'Tidak ada drift', tf_drift_detected_alert: 'Drift Terdeteksi',
      tf_drift_checked: 'Pemeriksaan terakhir', tf_drift_never: 'belum pernah', tf_drift_interval: 'Interval', tf_drift_check_now: 'Periksa Drift Sekarang',
      tf_drift_clean: 'Infrastruktur sesuai dengan konfigurasi.', tf_drift_changes: 'Resource yang berbeda dengan konfigurasi',
      tf_drift_diff_missing: 'Penampil diff tidak tersedia.', tf_ov_loading: 'Memuat…',
    },
  });

  const ACTIVE_PIP = { applied: 'applied', no_changes: 'applied', errored: 'errored', policy_failed: 'errored', awaiting_approval: 'awaiting', planned: 'awaiting', queued: 'live', planning: 'live', applying: 'live' };

  function fmtOut(o, revealed) {
    const hidden = o.sensitive && (o.value_hidden || o.value === null || o.value === undefined);
    if (hidden && !revealed) return { text: Tf.t('tf_ov_sensitive', 'sensitive value hidden'), hidden: true };
    return { text: typeof o.value === 'string' ? o.value : JSON.stringify(o.value), hidden: false };
  }

  async function fetchOutputs(wsId, reveal) {
    const r = await Tf.api.get('/api/terraform/workspaces/' + wsId + '/outputs' + (reveal ? '?reveal=1' : ''));
    return r.outputs || [];
  }

  // ---- Overview ----
  function bannersHtml(ws, awaiting) {
    let h = '';
    if (awaiting.length) {
      const r = awaiting[0];
      h += '<div class="tf-ov-banner tf-ov-banner-accent"><div class="tf-ov-banner-text">' + Tf.icon('bell', 'icon-sm') + ' <span>' +
        Tf.esc(Tf.t('tf_ov_approval', '{n} run awaiting approval', { n: awaiting.length })) + '</span></div>' +
        '<button type="button" class="btn btn-sm btn-primary" data-ov="run" data-id="' + r.id + '">' +
        Tf.esc(Tf.t('tf_ov_approve', 'Review & Approve Run #{id}', { id: r.id })) + '</button></div>';
    }
    if (ws.locked) {
      const manual = ws.locked_by === 'manual';
      const by = manual ? Tf.t('tf_ov_by_manual', 'manually') : Tf.t('tf_ov_by_run', 'by run #{id}', { id: String(ws.locked_by || '').replace(/^run:/, '') });
      h += '<div class="tf-ov-banner tf-ov-banner-danger"><div class="tf-ov-banner-text">' + Tf.icon('lock', 'icon-sm') + ' <span>' +
        Tf.esc(Tf.t('tf_ov_locked', 'Locked {by}', { by: by })) + (ws.lock_reason ? ' — ' + Tf.esc(ws.lock_reason) : '') + '</span></div>' +
        (manual ? '<button type="button" class="btn btn-sm" data-ov="unlock">' + Tf.esc(Tf.t('tf_ov_unlock', 'Unlock')) + '</button>'
          : '<button type="button" class="btn btn-sm btn-danger" data-ov="force-unlock">' + Tf.esc(Tf.t('tf_ov_force_unlock', 'Force Unlock')) + '</button>') + '</div>';
    }
    if (ws.drift_detected) {
      h += '<div class="tf-ov-banner tf-ov-banner-warn"><div class="tf-ov-banner-text">' + Tf.icon('triangle-alert', 'icon-sm') + ' <span>' +
        Tf.esc(Tf.t('tf_ov_drift_banner', 'Drift detected since the last check')) + '</span></div>' +
        '<div class="tf-row"><button type="button" class="btn btn-sm" data-ov="view-drift">' + Tf.esc(Tf.t('tf_ov_view_drift', 'View Drift')) + '</button>' +
        '<button type="button" class="btn btn-sm btn-primary" data-ov="replan">' + Tf.esc(Tf.t('tf_ov_replan', 'Re-plan')) + '</button></div></div>';
    }
    return h;
  }

  function pipsHtml(runs) {
    const cells = [];
    for (let i = 0; i < 10; i++) {
      const r = runs[i];
      if (!r) { cells.push('<span class="tf-pip tf-pip-empty" aria-hidden="true"></span>'); continue; }
      const cls = ACTIVE_PIP[r.status] ? 'tf-pip-' + ACTIVE_PIP[r.status] : 'tf-pip-other';
      cells.push('<button type="button" class="tf-pip ' + cls + '" data-ov="run" data-id="' + r.id + '" title="#' + r.id + ' · ' + Tf.esc(Tf.ui.runStatusLabel(r.status)) + '"></button>');
    }
    return '<div class="tf-pips" role="group" aria-label="' + Tf.esc(Tf.t('tf_ov_runs', 'Recent runs')) + '">' + cells.join('') + '</div>';
  }

  function runsBlock(runs) {
    if (!runs.length) return '<div class="tf-muted tf-ov-small">' + Tf.esc(Tf.t('tf_ov_runs_empty', 'No runs yet')) + '</div>';
    const last = runs[0];
    return pipsHtml(runs) + '<div class="tf-ov-small tf-muted">' +
      Tf.esc(Tf.t('tf_ov_latest', 'Latest: #{id} · {type} · {trigger}', { id: last.id, type: last.type, trigger: last.trigger_kind })) +
      ' · ' + Tf.esc(Tf.fmt.counts(last.counts)) + ' · ' + Tf.esc(Tf.fmt.ago(last.finished_at || last.created_at)) + '</div>' +
      '<div class="tf-row tf-ov-small">' + Tf.ui.badge(last.status) + '</div>';
  }

  function outputsBlock(outs, revealed) {
    const anySensitive = outs.some(function (o) { return o.sensitive; });
    let h = '<div class="tf-row tf-ov-head"><span class="tf-field-label">' + Tf.esc(Tf.t('tf_ov_outputs', 'Outputs')) + '</span>' +
      (anySensitive ? '<button type="button" class="btn btn-sm" data-ov="reveal">' + Tf.icon(revealed ? 'eye-off' : 'eye', 'icon-xs') + ' ' +
        Tf.esc(revealed ? Tf.t('tf_ov_hide', 'Hide sensitive') : Tf.t('tf_ov_reveal', 'Reveal sensitive')) + '</button>' : '') + '</div>';
    if (!outs.length) return h + '<div class="tf-muted tf-ov-small">' + Tf.esc(Tf.t('tf_ov_no_outputs', 'No outputs')) + '</div>';
    h += '<div class="tf-ov-table-wrap"><table class="tf-ov-table"><tbody>';
    outs.forEach(function (o, i) {
      const v = fmtOut(o, revealed);
      h += '<tr><td class="tf-mono tf-ov-name">' + Tf.esc(o.name) + (o.sensitive ? ' ' + Tf.icon('lock', 'icon-xs') : '') + '</td>' +
        '<td class="tf-mono tf-ov-val' + (v.hidden ? ' tf-muted' : '') + '">' + Tf.esc(v.text) + '</td>' +
        '<td class="tf-ov-act"><button type="button" class="btn btn-icon btn-sm" data-ov="copy" data-i="' + i + '" title="' + Tf.esc(Tf.t('tf_ov_copy', 'Copy')) + '">' + Tf.icon('copy', 'icon-xs') + '</button></td></tr>';
    });
    return h + '</tbody></table></div>';
  }

  function configBlock(ws) {
    const accounts = (ws.cloud_accounts || []).map(function (a) {
      return '<span class="badge">' + Tf.esc(a.name) + ' (' + Tf.esc(a.provider) + ')</span>';
    }).join(' ');
    const source = ws.source === 'git'
      ? Tf.esc((ws.git_url || '') + (ws.git_branch ? '@' + ws.git_branch : '') + (ws.git_path ? ' · ' + ws.git_path : ''))
      : Tf.esc(Tf.t('tf_ov_source_inline', 'Inline files'));
    const drift = ws.drift_interval_minutes ? Tf.t('tf_ov_drift_every', 'Drift check every {n} min', { n: ws.drift_interval_minutes }) : Tf.t('tf_ov_off', 'off');
    const plan = ws.plan_interval_minutes ? Tf.t('tf_ov_plan_every', 'Plan every {n} min', { n: ws.plan_interval_minutes }) : Tf.t('tf_ov_off', 'off');
    const auto = ws.auto_apply ? Tf.t('tf_ov_auto_apply', 'Auto-apply on') : Tf.t('tf_ov_auto_apply_off', 'Auto-apply off');
    const kv = function (k, v) { return '<dt>' + Tf.esc(k) + '</dt><dd>' + v + '</dd>'; };
    return '<dl class="tf-ov-kv">' +
      kv(Tf.t('tf_ov_env', 'Environment'), Tf.esc(ws.environment || '—')) +
      kv(Tf.t('tf_ov_binary', 'Binary'), '<span class="tf-mono">' + Tf.esc(ws.binary || '') + (ws.tf_version ? ' ' + Tf.esc(ws.tf_version) : '') + '</span>') +
      kv(Tf.t('tf_ov_source', 'Source'), '<span class="tf-ov-wrap">' + source + '</span>') +
      kv(Tf.t('tf_ov_cloud', 'Cloud accounts'), accounts || '<span class="tf-muted">' + Tf.esc(Tf.t('tf_ov_no_cloud', 'None linked')) + '</span>') +
      kv(Tf.t('tf_ov_schedules', 'Schedules'), Tf.esc(drift) + ' · ' + Tf.esc(plan) + ' · ' + Tf.esc(auto)) +
      '</dl><div id="tfIacChip" class="tf-row tf-ov-small"></div>';
  }

  async function renderOverview(root, ws) {
    root.innerHTML = '<div class="tf-ov"><div id="tfOvBanners"></div>' +
      '<div class="tf-ov-grid">' +
      '<section class="tf-ov-card" aria-labelledby="tfOvRunsT"><h4 id="tfOvRunsT" class="tf-ov-title">' + Tf.esc(Tf.t('tf_ov_runs', 'Recent runs')) + '</h4><div id="tfOvPips">' + Tf.ui.skeleton(2) + '</div></section>' +
      '<section class="tf-ov-card" aria-labelledby="tfOvOutT"><div id="tfOvOutputs">' + Tf.ui.skeleton(3) + '</div></section>' +
      '<section class="tf-ov-card" aria-labelledby="tfOvCfgT"><h4 id="tfOvCfgT" class="tf-ov-title">' + Tf.esc(Tf.t('tf_ov_config', 'Configuration & security')) + '</h4><div id="tfOvConfig">' + configBlock(ws) + '</div></section>' +
      '</div></div>';

    const state = { revealed: false, outs: [] };
    const alive = function () { return root.isConnected; };

    async function loadOutputs() {
      try {
        state.outs = await fetchOutputs(ws.id, state.revealed);
        if (!alive()) return;
        const box = root.querySelector('#tfOvOutputs');
        if (box) box.innerHTML = outputsBlock(state.outs, state.revealed);
      } catch (e) {
        const box = root.querySelector('#tfOvOutputs');
        if (box) box.innerHTML = Tf.ui.error(Tf.errMsg(e), 'data-ov="reload-outputs"');
      }
    }

    let runs = [];
    try {
      const r = await Tf.api.get('/api/terraform/workspaces/' + ws.id + '/runs?limit=10');
      runs = r.runs || [];
    } catch (e) { runs = []; }
    if (!alive()) return;
    const awaiting = runs.filter(function (x) { return x.status === 'awaiting_approval'; });
    root.querySelector('#tfOvBanners').innerHTML = bannersHtml(ws, awaiting);
    root.querySelector('#tfOvPips').innerHTML = runsBlock(runs);
    await loadOutputs();

    if (window.IacScan) {
      try {
        const scan = await window.IacScan.latest('terraform', ws.id);
        const chip = root.querySelector('#tfIacChip');
        if (chip && scan) chip.innerHTML = window.IacScan.chipHtml('terraform', ws.id, { summary: scan.summary });
      } catch (e) { /* scan chip is optional */ }
    }

    root.addEventListener('click', async function (e) {
      const b = e.target.closest('[data-ov]');
      if (!b) return;
      const act = b.getAttribute('data-ov');
      if (act === 'run') { if (Tf.runs && Tf.runs.openViewer) Tf.runs.openViewer(Number(b.getAttribute('data-id'))); }
      else if (act === 'reveal') { state.revealed = !state.revealed; await loadOutputs(); }
      else if (act === 'reload-outputs') await loadOutputs();
      else if (act === 'copy') {
        const i = Number(b.getAttribute('data-i'));
        let o = state.outs[i];
        if (!o) return;
        if (o.sensitive && (o.value_hidden || o.value === null || o.value === undefined)) {
          try {
            const full = (await fetchOutputs(ws.id, true)).find(function (x) { return x.name === o.name; });
            if (!full) throw new Error(Tf.t('tf_ov_reveal_failed', 'Could not reveal the value'));
            o = full;
          } catch (err) { Tf.toast(Tf.errMsg(err), 'error'); return; }
        }
        Tf.copy(typeof o.value === 'string' ? o.value : JSON.stringify(o.value));
      }
      else if (act === 'unlock') { if (Tf.workspaces && Tf.workspaces.unlock) Tf.workspaces.unlock(ws); }
      else if (act === 'force-unlock') { if (Tf.workspaces && Tf.workspaces.forceUnlock) Tf.workspaces.forceUnlock(ws); }
      else if (act === 'view-drift') { if (Tf.openWorkspace) Tf.openWorkspace(ws.id, 'drift'); }
      else if (act === 'replan') { if (Tf.runs && Tf.runs.startDialog) Tf.runs.startDialog(ws.id, 'plan'); }
    });
  }

  // ---- Drift ----
  async function renderDrift(root, ws) {
    root.innerHTML = '<div class="tf-drift">' + Tf.ui.skeleton(3) + '</div>';
    let d;
    try { d = await Tf.api.get('/api/terraform/workspaces/' + ws.id + '/drift'); }
    catch (e) { root.innerHTML = Tf.ui.error(Tf.errMsg(e), 'data-drift="reload"'); return; }
    if (!root.isConnected) return;

    const checked = d.checked_at ? Tf.fmt.dt(d.checked_at) + ' (' + Tf.fmt.ago(d.checked_at) + ')' : Tf.t('tf_drift_never', 'never');
    const interval = d.interval_minutes ? d.interval_minutes + ' min' : Tf.t('tf_ov_off', 'off');
    const icon = d.detected ? 'triangle-alert' : 'circle-check';
    const label = d.detected ? Tf.t('tf_drift_detected_alert', 'Drift Detected') : Tf.t('tf_drift_none_alert', 'No drift');
    root.innerHTML = '<div class="tf-drift">' +
      '<div class="tf-ov-banner ' + (d.detected ? 'tf-ov-banner-warn' : 'tf-ov-banner-ok') + '">' +
      '<div class="tf-ov-banner-text">' + Tf.icon(icon, 'icon-sm') + ' <b>' + Tf.esc(label) + '</b>' +
      '<div class="tf-ov-small tf-muted">' + Tf.esc(Tf.t('tf_drift_checked', 'Last check')) + ': ' + Tf.esc(checked) + ' · ' +
      Tf.esc(Tf.t('tf_drift_interval', 'Interval')) + ': ' + Tf.esc(interval) + '</div></div>' +
      '<button type="button" id="tfDriftCheck" class="btn btn-sm btn-primary" data-drift="check">' + Tf.icon('refresh-cw', 'icon-xs') + ' ' +
      Tf.esc(Tf.t('tf_drift_check_now', 'Check Drift Now')) + '</button></div>' +
      '<div id="tfDriftDiff" class="tf-drift-diff"></div></div>';

    const diff = root.querySelector('#tfDriftDiff');
    const res = d.resources || [];
    if (!d.detected) { diff.innerHTML = '<div class="tf-muted tf-ov-small">' + Tf.esc(Tf.t('tf_drift_clean', 'Infrastructure matches the configuration.')) + '</div>'; }
    else if (Tf.runs && Tf.runs.renderDiff) {
      diff.innerHTML = '<h4 class="tf-ov-title">' + Tf.esc(Tf.t('tf_drift_changes', 'Resources that differ from the configuration')) + '</h4>';
      Tf.runs.renderDiff(diff, res);
    } else {
      diff.innerHTML = '<div class="tf-muted tf-ov-small">' + Tf.esc(Tf.t('tf_drift_diff_missing', 'Diff viewer is not available.')) + '</div>';
    }

    if (root.dataset.bound) return; // the listener survives re-renders on the same wrapper
    root.dataset.bound = '1';
    root.addEventListener('click', function (e) {
      const b = e.target.closest('[data-drift]');
      if (!b) return;
      if (b.getAttribute('data-drift') === 'reload') renderDrift(root, ws);
      else if (b.getAttribute('data-drift') === 'check' && Tf.workspaces && Tf.workspaces.checkDrift) Tf.workspaces.checkDrift(ws);
    });
  }

  Tf.overview = { render: renderOverview, renderDrift: renderDrift };

  // Each drawer render gets a fresh wrapper so listeners never stack across renders.
  function mount(body, render, ws) {
    body.innerHTML = '';
    const root = document.createElement('div');
    body.appendChild(root);
    return render(root, ws);
  }

  Tf.registerWsTab({ id: 'overview', icon: 'gauge', labelKey: 'tf_ws_tab_overview', fallback: 'Overview', order: 1,
    render: function (body, ws) { return mount(body, renderOverview, ws); } });
  Tf.registerWsTab({ id: 'drift', icon: 'git-compare', labelKey: 'tf_ws_tab_drift', fallback: 'Drift', order: 6,
    render: function (body, ws) { return mount(body, renderDrift, ws); } });
})();
