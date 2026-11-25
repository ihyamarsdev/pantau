/* Workspaces: tab 1 (KPI strip, toolbar, dense table + card grid, group-by, quick actions),
   New Workspace drawer with template gallery, and the workspace drawer shell (header, split button,
   tab switcher). Drawer tab bodies are registered by other modules via Tf.registerWsTab. */
(function () {
  'use strict';

  const Tf = window.Tf;
  Tf.addI18n({
    en: {
      tf_tab_workspaces: 'Workspaces',
      tf_ws_kpi_total: 'Total', tf_ws_kpi_approval: 'Awaiting approval', tf_ws_kpi_drifted: 'Drifted',
      tf_ws_kpi_errored: 'Errored', tf_ws_kpi_running: 'Running', tf_ws_kpi_locked: 'Locked',
      tf_ws_search_ph: 'Search workspaces (press /)', tf_ws_env_all: 'All environments', tf_ws_tag_all: 'All tags',
      tf_ws_group_all: 'All groups', tf_ws_group_none: 'Ungrouped', tf_ws_groupby: 'Group by group',
      tf_ws_view_grid: 'Cards', tf_ws_view_list: 'Table', tf_ws_new: 'New workspace',
      tf_ws_count: '{n} workspaces', tf_ws_none: 'No workspaces found',
      tf_ws_none_desc: 'Create a workspace to plan and apply Terraform configuration.',
      tf_ws_col_name: 'Workspace', tf_ws_col_status: 'Status', tf_ws_col_group: 'Group',
      tf_ws_col_binary: 'Binary & resources', tf_ws_col_last: 'Last run', tf_ws_col_actions: 'Actions',
      tf_ws_review: 'Review', tf_ws_plan: 'Plan', tf_ws_more: 'More actions',
      tf_ws_open_config: 'Configuration', tf_ws_open_state: 'State', tf_ws_open_vars: 'Variables', tf_ws_open_settings: 'Settings',
      tf_ws_check_drift: 'Check drift', tf_ws_lock: 'Lock', tf_ws_unlock: 'Unlock', tf_ws_force_unlock: 'Force unlock',
      tf_ws_delete: 'Delete', tf_ws_speculative: 'Speculative plan', tf_ws_refresh_only: 'Refresh only', tf_ws_destroy: 'Destroy',
      tf_ws_st_locked: 'Locked', tf_ws_st_running: 'Running', tf_ws_st_awaiting: 'Awaiting approval', tf_ws_st_errored: 'Errored',
      tf_ws_st_drifted: 'Drifted', tf_ws_st_applied: 'Applied', tf_ws_st_never: 'Never run',
      tf_ws_resources: '{n} resources', tf_ws_never: 'never',
      tf_ws_kbd: 'Enter open · p plan · d drift',
      tf_ws_drift_queued: 'Drift check started', tf_ws_run_missing: 'Run dialog is not available yet',
      tf_ws_lock_title: 'Lock workspace', tf_ws_lock_reason: 'Reason', tf_ws_locked_ok: 'Workspace locked', tf_ws_unlocked_ok: 'Workspace unlocked',
      tf_ws_force_title: 'Force unlock', tf_ws_force_msg: 'Force unlock removes the lock even when a run holds it. Continue only if no run is active.',
      tf_ws_delete_title: 'Delete workspace', tf_ws_delete_msg: 'Delete workspace "{name}"?', tf_ws_deleted: 'Workspace deleted',
      tf_ws_created: 'Workspace created', tf_ws_name_required: 'Name is required',
      tf_ws_new_title: 'New workspace', tf_ws_tpl_blank: 'Blank workspace', tf_ws_tpl_hint: 'Pick a template to start from its files.',
      tf_ws_tpl_vars: 'Variables: {vars}', tf_ws_tpl_cloud: 'Link a {provider} cloud account in Settings after saving.',
      tf_ws_src: 'Source', tf_ws_src_inline: 'Inline', tf_ws_src_git: 'Git',
      tf_ws_f_name: 'Name', tf_ws_f_group: 'Group', tf_ws_f_env: 'Environment', tf_ws_f_desc: 'Description',
      tf_ws_f_tags: 'Tags (comma-separated)', tf_ws_f_binary: 'Binary',
      tf_ws_f_drift: 'Drift check interval, minutes (0 = off)', tf_ws_f_plan: 'Scheduled plan interval, minutes (0 = off)',
      tf_ws_f_autoapply: 'Auto-apply clean plans', tf_ws_autoapply_prod: 'Auto-apply is not allowed for production',
      tf_ws_files: 'Files', tf_ws_file_add: 'Add file', tf_ws_file_ph: 'new-file.tf', tf_ws_file_del: 'Remove file',
      tf_ws_git_url: 'Repository URL', tf_ws_git_branch: 'Branch', tf_ws_git_path: 'Working directory',
      tf_ws_git_auth: 'Authentication', tf_ws_git_auth_none: 'None', tf_ws_git_auth_token: 'Token', tf_ws_git_auth_ssh: 'SSH key',
      tf_ws_git_token: 'Token', tf_ws_git_key: 'SSH key', tf_ws_create: 'Create workspace', tf_ws_cancel: 'Cancel',
      tf_ws_dlg_cancel: 'Cancel', tf_ws_dlg_lock: 'Lock',
    },
    id: {
      tf_tab_workspaces: 'Workspace',
      tf_ws_kpi_total: 'Total', tf_ws_kpi_approval: 'Menunggu persetujuan', tf_ws_kpi_drifted: 'Drift',
      tf_ws_kpi_errored: 'Error', tf_ws_kpi_running: 'Berjalan', tf_ws_kpi_locked: 'Terkunci',
      tf_ws_search_ph: 'Cari workspace (tekan /)', tf_ws_env_all: 'Semua environment', tf_ws_tag_all: 'Semua tag',
      tf_ws_group_all: 'Semua grup', tf_ws_group_none: 'Tanpa grup', tf_ws_groupby: 'Kelompokkan per grup',
      tf_ws_view_grid: 'Kartu', tf_ws_view_list: 'Tabel', tf_ws_new: 'Workspace baru',
      tf_ws_count: '{n} workspace', tf_ws_none: 'Workspace tidak ditemukan',
      tf_ws_none_desc: 'Buat workspace untuk merencanakan dan menerapkan konfigurasi Terraform.',
      tf_ws_col_name: 'Workspace', tf_ws_col_status: 'Status', tf_ws_col_group: 'Grup',
      tf_ws_col_binary: 'Binary & resource', tf_ws_col_last: 'Run terakhir', tf_ws_col_actions: 'Aksi',
      tf_ws_review: 'Tinjau', tf_ws_plan: 'Plan', tf_ws_more: 'Aksi lainnya',
      tf_ws_open_config: 'Konfigurasi', tf_ws_open_state: 'State', tf_ws_open_vars: 'Variabel', tf_ws_open_settings: 'Pengaturan',
      tf_ws_check_drift: 'Periksa drift', tf_ws_lock: 'Kunci', tf_ws_unlock: 'Buka kunci', tf_ws_force_unlock: 'Paksa buka kunci',
      tf_ws_delete: 'Hapus', tf_ws_speculative: 'Speculative plan', tf_ws_refresh_only: 'Refresh saja', tf_ws_destroy: 'Destroy',
      tf_ws_st_locked: 'Terkunci', tf_ws_st_running: 'Berjalan', tf_ws_st_awaiting: 'Menunggu persetujuan', tf_ws_st_errored: 'Error',
      tf_ws_st_drifted: 'Drift', tf_ws_st_applied: 'Diterapkan', tf_ws_st_never: 'Belum pernah',
      tf_ws_resources: '{n} resource', tf_ws_never: 'belum pernah',
      tf_ws_kbd: 'Enter buka · p plan · d drift',
      tf_ws_drift_queued: 'Pemeriksaan drift dimulai', tf_ws_run_missing: 'Dialog run belum tersedia',
      tf_ws_lock_title: 'Kunci workspace', tf_ws_lock_reason: 'Alasan', tf_ws_locked_ok: 'Workspace dikunci', tf_ws_unlocked_ok: 'Kunci workspace dibuka',
      tf_ws_force_title: 'Paksa buka kunci', tf_ws_force_msg: 'Paksa buka kunci menghapus kunci walaupun dipegang run. Lanjutkan hanya jika tidak ada run yang aktif.',
      tf_ws_delete_title: 'Hapus workspace', tf_ws_delete_msg: 'Hapus workspace "{name}"?', tf_ws_deleted: 'Workspace dihapus',
      tf_ws_created: 'Workspace dibuat', tf_ws_name_required: 'Nama wajib diisi',
      tf_ws_new_title: 'Workspace baru', tf_ws_tpl_blank: 'Workspace kosong', tf_ws_tpl_hint: 'Pilih template untuk memulai dari file-nya.',
      tf_ws_tpl_vars: 'Variabel: {vars}', tf_ws_tpl_cloud: 'Hubungkan akun cloud {provider} di Pengaturan setelah menyimpan.',
      tf_ws_src: 'Sumber', tf_ws_src_inline: 'Inline', tf_ws_src_git: 'Git',
      tf_ws_f_name: 'Nama', tf_ws_f_group: 'Grup', tf_ws_f_env: 'Environment', tf_ws_f_desc: 'Deskripsi',
      tf_ws_f_tags: 'Tag (pisah koma)', tf_ws_f_binary: 'Binary',
      tf_ws_f_drift: 'Interval cek drift, menit (0 = nonaktif)', tf_ws_f_plan: 'Interval plan terjadwal, menit (0 = nonaktif)',
      tf_ws_f_autoapply: 'Auto-apply plan bersih', tf_ws_autoapply_prod: 'Auto-apply tidak diizinkan untuk production',
      tf_ws_files: 'File', tf_ws_file_add: 'Tambah file', tf_ws_file_ph: 'file-baru.tf', tf_ws_file_del: 'Hapus file',
      tf_ws_git_url: 'URL repository', tf_ws_git_branch: 'Branch', tf_ws_git_path: 'Direktori kerja',
      tf_ws_git_auth: 'Autentikasi', tf_ws_git_auth_none: 'Tanpa', tf_ws_git_auth_token: 'Token', tf_ws_git_auth_ssh: 'Kunci SSH',
      tf_ws_git_token: 'Token', tf_ws_git_key: 'Kunci SSH', tf_ws_create: 'Buat workspace', tf_ws_cancel: 'Batal',
      tf_ws_dlg_cancel: 'Batal', tf_ws_dlg_lock: 'Kunci',
    },
  });

  const DEFAULT_MAIN = 'resource "terraform_data" "example" {\n  input = "hello"\n}\n\noutput "example" {\n  value = terraform_data.example.output\n}\n';
  const PROVIDERS = { aws: 'AWS', gcp: 'GCP', azure: 'Azure', digitalocean: 'DigitalOcean', hetzner: 'Hetzner', cloudflare: 'Cloudflare', kubernetes: 'Kubernetes' };
  const LS = {
    get: function (k, d) { try { const v = localStorage.getItem(k); return v === null ? d : v; } catch (e) { return d; } },
    set: function (k, v) { try { localStorage.setItem(k, v); } catch (e) { /* storage blocked */ } },
  };

  // Shared filter/view state for the Workspaces tab.
  const view = {
    q: '', env: '', tag: '', group: 'all', kpi: '',
    mode: LS.get('pantau_tf_view', 'grid') === 'list' ? 'list' : 'grid',
    groupBy: LS.get('pantau_tf_groupby', '0') === '1',
    sort: { key: 'name', dir: 1 },
  };
  const collapsedKey = 'pantau_tf_ws_collapsed';
  function collapsed() { try { return new Set(JSON.parse(LS.get(collapsedKey, '[]')) || []); } catch (e) { return new Set(); } }

  /* ---------- data ---------- */
  Tf.load.workspaces = async function () {
    try {
      const r = await Tf.api.get('/api/terraform/workspaces');
      Tf.state.workspaces = r.workspaces || [];
    } catch (e) {
      Tf.toast(Tf.errMsg(e), 'error');
    }
    Tf.emit('workspaces', Tf.state.workspaces);
    return Tf.state.workspaces;
  };

  // Status values come from store.ListTFWorkspaces: locked > running > awaiting_approval > errored > drifted > applied > never.
  const ST = {
    locked: ['badge-unknown', 'tf_ws_st_locked', 'Locked'],
    running: ['badge-info', 'tf_ws_st_running', 'Running'],
    awaiting_approval: ['badge-warning', 'tf_ws_st_awaiting', 'Awaiting approval'],
    errored: ['badge-danger', 'tf_ws_st_errored', 'Errored'],
    drifted: ['badge-warning', 'tf_ws_st_drifted', 'Drifted'],
    applied: ['badge-healthy', 'tf_ws_st_applied', 'Applied'],
    never: ['badge-unknown', 'tf_ws_st_never', 'Never run'],
  };
  function stBadge(ws) {
    const e = ST[ws.status] || ST.never;
    return '<span class="badge ' + e[0] + '">' + Tf.esc(Tf.t(e[1], e[2])) + '</span>';
  }
  function dotFor(ws) {
    const s = ws.last_run ? ws.last_run.status : '';
    return Tf.ui.dot(ws.locked ? 'interrupted' : s);
  }
  function lastRunHtml(ws) {
    const lr = ws.last_run;
    if (!lr) return '<span class="tf-muted">' + Tf.esc(Tf.t('tf_ws_never', 'never')) + '</span>';
    return Tf.ui.badge(lr.status) + ' <span class="tf-muted tf-mono">#' + lr.id + '</span> <span class="tf-muted">' + Tf.esc(Tf.fmt.ago(lr.finished_at || lr.started_at)) + '</span>';
  }
  function isAwaiting(ws) { return ws.status === 'awaiting_approval' || (ws.last_run && ws.last_run.status === 'awaiting_approval'); }
  function isRunning(ws) { return ws.status === 'running'; }

  /* ---------- filtering, sorting, grouping ---------- */
  function filtered() {
    const q = view.q.toLowerCase();
    return Tf.state.workspaces.filter(function (w) {
      const grp = (w.group_name || '').trim();
      if (view.group !== 'all') {
        if (view.group === '__ungrouped__' && grp !== '') return false;
        if (view.group !== '__ungrouped__' && grp !== view.group) return false;
      }
      if (view.env && w.environment !== view.env) return false;
      if (view.tag && !(w.tags || []).includes(view.tag)) return false;
      if (view.kpi === 'approval' && !isAwaiting(w)) return false;
      if (view.kpi === 'drifted' && !w.drift_detected) return false;
      if (view.kpi === 'errored' && w.status !== 'errored') return false;
      if (view.kpi === 'running' && !isRunning(w)) return false;
      if (view.kpi === 'locked' && !w.locked) return false;
      if (!q) return true;
      return (w.name + ' ' + (w.description || '') + ' ' + grp + ' ' + (w.tags || []).join(' ')).toLowerCase().includes(q);
    });
  }
  function sortVal(w, key) {
    switch (key) {
      case 'status': return w.status || '';
      case 'env': return w.environment || '';
      case 'group': return (w.group_name || '').trim();
      case 'resources': return w.resource_count || 0;
      case 'last': return w.last_run ? w.last_run.id : 0;
      default: return (w.name || '').toLowerCase();
    }
  }
  function sorted(list) {
    const k = view.sort.key, d = view.sort.dir;
    return list.slice().sort(function (a, b) {
      const x = sortVal(a, k), y = sortVal(b, k);
      return (x > y ? 1 : x < y ? -1 : 0) * d;
    });
  }
  // Returns [{key, name, items}] ; a single section when group-by is off.
  function sections(list) {
    if (!view.groupBy) return [{ key: '', name: '', items: list }];
    const map = new Map(), none = [];
    list.forEach(function (w) {
      const g = (w.group_name || '').trim();
      if (!g) none.push(w);
      else { if (!map.has(g)) map.set(g, []); map.get(g).push(w); }
    });
    const out = Array.from(map.keys()).sort(function (a, b) { return a.localeCompare(b); }).map(function (g) { return { key: 'g:' + g, name: g, items: map.get(g) }; });
    if (none.length) out.push({ key: '__ungrouped__', name: Tf.t('tf_ws_group_none', 'Ungrouped'), items: none });
    return out;
  }

  /* ---------- tab 1 rendering ---------- */
  function renderFilterOptions() {
    const all = Tf.state.workspaces;
    const envs = Array.from(new Set(['production', 'staging', 'development'].concat(all.map(function (w) { return w.environment; }).filter(Boolean)))).sort();
    const tags = Array.from(new Set(all.flatMap(function (w) { return w.tags || []; }))).sort();
    const groups = Array.from(new Set(all.map(function (w) { return (w.group_name || '').trim(); }).filter(Boolean))).sort();
    const hasNone = all.some(function (w) { return !(w.group_name || '').trim(); });
    const fill = function (id, first, values, cur) {
      const el = document.getElementById(id);
      if (!el) return;
      el.innerHTML = '<option value="">' + Tf.esc(first) + '</option>' + values.map(function (v) {
        return '<option value="' + Tf.esc(v) + '"' + (v === cur ? ' selected' : '') + '>' + Tf.esc(v) + '</option>';
      }).join('');
    };
    fill('tfWsEnvFilter', Tf.t('tf_ws_env_all', 'All environments'), envs, view.env);
    fill('tfWsTagFilter', Tf.t('tf_ws_tag_all', 'All tags'), tags, view.tag);
    const gEl = document.getElementById('tfWsGroupFilter');
    if (gEl) {
      let opts = '<option value="all">' + Tf.esc(Tf.t('tf_ws_group_all', 'All groups')) + '</option>' + groups.map(function (g) { return '<option value="' + Tf.esc(g) + '">' + Tf.esc(g) + '</option>'; }).join('');
      if (hasNone && groups.length) opts += '<option value="__ungrouped__">' + Tf.esc(Tf.t('tf_ws_group_none', 'Ungrouped')) + '</option>';
      gEl.innerHTML = opts;
      gEl.value = view.group;
      if (gEl.value !== view.group) { view.group = 'all'; gEl.value = 'all'; }
    }
  }

  function renderKpis() {
    const all = Tf.state.workspaces;
    const count = { total: all.length, approval: all.filter(isAwaiting).length, drifted: all.filter(function (w) { return w.drift_detected; }).length,
      errored: all.filter(function (w) { return w.status === 'errored'; }).length, running: all.filter(isRunning).length, locked: all.filter(function (w) { return w.locked; }).length };
    const chip = function (key, label, n, tone) {
      const on = view.kpi === key;
      return '<button type="button" class="tf-kpi tf-kpi-' + tone + (on ? ' active' : '') + '" data-kpi="' + key + '" aria-pressed="' + on + '">' +
        '<span class="tf-kpi-n tf-mono">' + n + '</span><span class="tf-kpi-l">' + Tf.esc(label) + '</span></button>';
    };
    const el = document.getElementById('tfKpiStrip');
    if (!el) return;
    el.innerHTML = chip('', Tf.t('tf_ws_kpi_total', 'Total'), count.total, 'neutral') +
      chip('approval', Tf.t('tf_ws_kpi_approval', 'Awaiting approval'), count.approval, 'primary') +
      chip('drifted', Tf.t('tf_ws_kpi_drifted', 'Drifted'), count.drifted, 'warning') +
      chip('errored', Tf.t('tf_ws_kpi_errored', 'Errored'), count.errored, 'danger') +
      chip('running', Tf.t('tf_ws_kpi_running', 'Running'), count.running, 'info') +
      chip('locked', Tf.t('tf_ws_kpi_locked', 'Locked'), count.locked, 'muted');
  }

  function rowMenu(ws) {
    const lockItem = ws.locked && ws.locked_by === 'manual'
      ? '<button type="button" role="menuitem" data-ws-act="unlock" data-id="' + ws.id + '">' + Tf.icon('lock-open', 'icon-xs') + ' ' + Tf.esc(Tf.t('tf_ws_unlock', 'Unlock')) + '</button>'
      : ws.locked ? '<button type="button" role="menuitem" data-ws-act="force-unlock" data-id="' + ws.id + '">' + Tf.icon('triangle-alert', 'icon-xs') + ' ' + Tf.esc(Tf.t('tf_ws_force_unlock', 'Force unlock')) + '</button>'
        : '<button type="button" role="menuitem" data-ws-act="lock" data-id="' + ws.id + '">' + Tf.icon('lock', 'icon-xs') + ' ' + Tf.esc(Tf.t('tf_ws_lock', 'Lock')) + '</button>';
    const item = function (act, label, icon) {
      return '<button type="button" role="menuitem" data-ws-act="' + act + '" data-id="' + ws.id + '">' + Tf.icon(icon, 'icon-xs') + ' ' + Tf.esc(label) + '</button>';
    };
    return '<details class="tf-more"><summary class="btn btn-sm btn-icon" aria-label="' + Tf.esc(Tf.t('tf_ws_more', 'More actions')) + '" title="' + Tf.esc(Tf.t('tf_ws_more', 'More actions')) + '">' + Tf.icon('ellipsis', 'icon-sm') + '</summary>' +
      '<div class="tf-menu" role="menu">' +
      item('check-drift', Tf.t('tf_ws_check_drift', 'Check drift'), 'refresh-cw') +
      item('open-config', Tf.t('tf_ws_open_config', 'Configuration'), 'file-code') +
      item('open-state', Tf.t('tf_ws_open_state', 'State'), 'database') +
      item('open-vars', Tf.t('tf_ws_open_vars', 'Variables'), 'sliders-horizontal') +
      item('open-settings', Tf.t('tf_ws_open_settings', 'Settings'), 'settings') +
      lockItem +
      item('delete', Tf.t('tf_ws_delete', 'Delete'), 'trash-2') +
      '</div></details>';
  }
  function quickAction(ws) {
    if (isAwaiting(ws)) return '<button type="button" class="btn btn-sm btn-primary" data-ws-act="review" data-id="' + ws.id + '">' + Tf.esc(Tf.t('tf_ws_review', 'Review')) + '</button>';
    return '<button type="button" class="btn btn-sm" data-ws-act="plan" data-id="' + ws.id + '">' + Tf.icon('play', 'icon-xs') + ' ' + Tf.esc(Tf.t('tf_ws_plan', 'Plan')) + '</button>';
  }

  function cardHtml(ws) {
    const res = Tf.esc(Tf.t('tf_ws_resources', '{n} resources', { n: ws.resource_count || 0 }));
    return '<article class="card tf-ws-card" tabindex="0" data-ws-id="' + ws.id + '">' +
      '<div class="tf-ws-card-head">' + dotFor(ws) + '<span class="tf-ws-name">' + Tf.esc(ws.name) + '</span>' + stBadge(ws) + '</div>' +
      (ws.description ? '<div class="tf-muted tf-ws-desc">' + Tf.esc(ws.description) + '</div>' : '') +
      '<div class="tf-row tf-ws-meta">' +
      (ws.environment ? '<span class="badge">' + Tf.esc(ws.environment) + '</span>' : '') +
      '<span class="badge tf-mono">' + Tf.esc(ws.binary) + (ws.tf_version ? ' ' + Tf.esc(ws.tf_version) : '') + '</span>' +
      (ws.locked ? '<span class="badge badge-unknown">' + Tf.icon('lock', 'icon-xs') + ' ' + Tf.esc(ws.lock_reason || Tf.t('tf_ws_st_locked', 'Locked')) + '</span>' : '') +
      (ws.tags || []).map(function (x) { return '<span class="badge">' + Tf.esc(x) + '</span>'; }).join('') + '</div>' +
      '<div class="tf-ws-card-foot"><span class="tf-muted">' + res + '</span><span>' + lastRunHtml(ws) + '</span>' +
      '<span class="tf-row">' + quickAction(ws) + rowMenu(ws) + '</span></div></article>';
  }
  function rowHtml(ws) {
    return '<tr class="tf-ws-row" tabindex="0" data-ws-id="' + ws.id + '">' +
      '<td><div class="tf-row">' + dotFor(ws) + '<div><b>' + Tf.esc(ws.name) + '</b>' +
      (ws.description ? '<div class="tf-muted">' + Tf.esc(ws.description) + '</div>' : '') + '</div></div></td>' +
      '<td>' + stBadge(ws) + '</td>' +
      '<td>' + Tf.esc((ws.group_name || '').trim() || '—') + '</td>' +
      '<td>' + Tf.esc(ws.environment || '—') + '</td>' +
      '<td class="tf-mono">' + Tf.esc(ws.binary) + (ws.tf_version ? ' ' + Tf.esc(ws.tf_version) : '') + ' · ' + (ws.resource_count || 0) + '</td>' +
      '<td>' + lastRunHtml(ws) + '</td>' +
      '<td class="tf-actions">' + quickAction(ws) + rowMenu(ws) + '</td></tr>';
  }
  function groupHeader(sec, isCol, cols) {
    return '<div class="tf-group" data-group="' + Tf.esc(sec.key) + '" role="button" tabindex="0" aria-expanded="' + !isCol + '"><span class="tf-caret' + (isCol ? ' collapsed' : '') + '">' + Tf.icon('chevron-down', 'icon-xs') + '</span>' +
      Tf.icon('folder', 'icon-sm') + ' <span>' + Tf.esc(sec.name) + '</span> <span class="badge">' + sec.items.length + '</span>' + (cols || '') + '</div>';
  }

  function renderList() {
    const panel = document.getElementById('tfPanel-workspaces');
    if (!panel || !panel.querySelector('#tfWsGrid')) return;
    renderKpis();
    renderFilterOptions();
    const list = sorted(filtered());
    const count = document.getElementById('tfWsCountBadge');
    if (count) count.textContent = Tf.t('tf_ws_count', '{n} workspaces', { n: list.length });
    const gridBtn = document.getElementById('tfWsViewGridBtn'), listBtn = document.getElementById('tfWsViewListBtn');
    if (gridBtn) { gridBtn.classList.toggle('active', view.mode === 'grid'); gridBtn.setAttribute('aria-pressed', String(view.mode === 'grid')); }
    if (listBtn) { listBtn.classList.toggle('active', view.mode === 'list'); listBtn.setAttribute('aria-pressed', String(view.mode === 'list')); }
    const gb = document.getElementById('tfWsGroupBy');
    if (gb) { gb.classList.toggle('active', view.groupBy); gb.setAttribute('aria-pressed', String(view.groupBy)); }

    const empty = document.getElementById('tfWsEmpty'), grid = document.getElementById('tfWsGrid'), wrap = document.getElementById('tfWsTable');
    if (!list.length) {
      empty.innerHTML = Tf.ui.empty({ icon: 'layers', title: Tf.t('tf_ws_none', 'No workspaces found'), desc: Tf.t('tf_ws_none_desc', '') });
      empty.hidden = false; grid.hidden = true; wrap.hidden = true;
      return;
    }
    empty.hidden = true;
    const col = collapsed();
    const secs = sections(list);
    if (view.mode === 'grid') {
      wrap.hidden = true; grid.hidden = false;
      grid.innerHTML = secs.map(function (s) {
        if (!s.key) return s.items.map(cardHtml).join('');
        const isCol = col.has(s.key);
        return groupHeader(s, isCol) + (isCol ? '' : s.items.map(cardHtml).join(''));
      }).join('');
    } else {
      grid.hidden = true; wrap.hidden = false;
      const body = document.getElementById('tfWsTableBody');
      body.innerHTML = secs.map(function (s) {
        if (!s.key) return s.items.map(rowHtml).join('');
        const isCol = col.has(s.key);
        return '<tr class="tf-group-row"><td colspan="7">' + groupHeader(s, isCol) + '</td></tr>' + (isCol ? '' : s.items.map(rowHtml).join(''));
      }).join('');
      document.querySelectorAll('#tfWsTable th[data-sort]').forEach(function (th) {
        const on = th.dataset.sort === view.sort.key;
        th.setAttribute('aria-sort', on ? (view.sort.dir > 0 ? 'ascending' : 'descending') : 'none');
      });
    }
  }

  function wsById(id) { return Tf.state.workspaces.find(function (w) { return w.id === Number(id); }) || null; }

  function buildPanel(panel) {
    panel.innerHTML =
      '<div class="tf-ws-toolbar">' +
      '<div id="tfKpiStrip" class="tf-kpi-strip" role="group" aria-label="' + Tf.esc(Tf.t('tf_tab_workspaces', 'Workspaces')) + '"></div>' +
      '<div class="tf-ws-bar">' +
      '<div class="search-box tf-ws-search"><input type="search" id="tfWsSearch" placeholder="' + Tf.esc(Tf.t('tf_ws_search_ph', '')) + '" aria-label="' + Tf.esc(Tf.t('tf_ws_search_ph', '')) + '" /></div>' +
      '<select id="tfWsEnvFilter" class="form-input tf-sel" aria-label="' + Tf.esc(Tf.t('tf_ws_f_env', 'Environment')) + '"></select>' +
      '<select id="tfWsTagFilter" class="form-input tf-sel" aria-label="' + Tf.esc(Tf.t('tf_ws_tag_all', 'All tags')) + '"></select>' +
      '<select id="tfWsGroupFilter" class="form-input tf-sel" aria-label="' + Tf.esc(Tf.t('tf_ws_col_group', 'Group')) + '"></select>' +
      '<button type="button" id="tfWsGroupBy" class="btn btn-sm" aria-pressed="false">' + Tf.icon('folder', 'icon-xs') + ' <span>' + Tf.esc(Tf.t('tf_ws_groupby', 'Group by group')) + '</span></button>' +
      '</div>' +
      '<div class="tf-ws-bar tf-ws-bar-end">' +
      '<span id="tfWsCountBadge" class="badge tf-mono"></span>' +
      '<div class="view-mode-toggle">' +
      '<button type="button" class="view-btn" id="tfWsViewGridBtn" title="' + Tf.esc(Tf.t('tf_ws_view_grid', 'Cards')) + '" aria-label="' + Tf.esc(Tf.t('tf_ws_view_grid', 'Cards')) + '">' + Tf.icon('layout-grid', 'icon-sm') + '</button>' +
      '<button type="button" class="view-btn" id="tfWsViewListBtn" title="' + Tf.esc(Tf.t('tf_ws_view_list', 'Table')) + '" aria-label="' + Tf.esc(Tf.t('tf_ws_view_list', 'Table')) + '">' + Tf.icon('list', 'icon-sm') + '</button></div>' +
      '<button type="button" id="btnTfNewWs" class="btn btn-sm btn-primary">' + Tf.icon('plus', 'icon-sm') + ' <span>' + Tf.esc(Tf.t('tf_ws_new', 'New workspace')) + '</span></button>' +
      '</div></div>' +
      '<div class="tf-kbd-hint tf-muted">' + Tf.esc(Tf.t('tf_ws_kbd', '')) + '</div>' +
      '<div id="tfWsGrid" class="tf-ws-grid" hidden></div>' +
      '<div id="tfWsTable" class="tf-ws-table-wrap" hidden><table class="tf-ws-table"><thead><tr>' +
      '<th data-sort="name" tabindex="0">' + Tf.esc(Tf.t('tf_ws_col_name', 'Workspace')) + '</th>' +
      '<th data-sort="status" tabindex="0">' + Tf.esc(Tf.t('tf_ws_col_status', 'Status')) + '</th>' +
      '<th data-sort="group" tabindex="0">' + Tf.esc(Tf.t('tf_ws_col_group', 'Group')) + '</th>' +
      '<th data-sort="env" tabindex="0">' + Tf.esc(Tf.t('tf_ws_f_env', 'Environment')) + '</th>' +
      '<th data-sort="resources" tabindex="0">' + Tf.esc(Tf.t('tf_ws_col_binary', 'Binary & resources')) + '</th>' +
      '<th data-sort="last" tabindex="0">' + Tf.esc(Tf.t('tf_ws_col_last', 'Last run')) + '</th>' +
      '<th>' + Tf.esc(Tf.t('tf_ws_col_actions', 'Actions')) + '</th></tr></thead><tbody id="tfWsTableBody"></tbody></table></div>' +
      '<div id="tfWsEmpty" hidden></div>';
    wireList(panel);
  }

  // One delegated listener per panel: built once, survives re-renders.
  function wireList(panel) {
    panel.addEventListener('click', function (e) {
      const t = e.target;
      const kpi = t.closest('[data-kpi]');
      if (kpi) { view.kpi = kpi.dataset.kpi; return renderList(); }
      const th = t.closest('#tfWsTable th[data-sort]');
      if (th) {
        const k = th.dataset.sort;
        view.sort = { key: k, dir: view.sort.key === k ? -view.sort.dir : 1 };
        return renderList();
      }
      if (t.closest('#tfWsGridBtn, #tfWsViewGridBtn')) { view.mode = 'grid'; LS.set('pantau_tf_view', 'grid'); return renderList(); }
      if (t.closest('#tfWsViewListBtn')) { view.mode = 'list'; LS.set('pantau_tf_view', 'list'); return renderList(); }
      if (t.closest('#tfWsGroupBy')) { view.groupBy = !view.groupBy; LS.set('pantau_tf_groupby', view.groupBy ? '1' : '0'); return renderList(); }
      if (t.closest('#btnTfNewWs')) return openNew();
      const grp = t.closest('[data-group]');
      if (grp) {
        const set = collapsed();
        const k = grp.dataset.group;
        if (set.has(k)) set.delete(k); else set.add(k);
        LS.set(collapsedKey, JSON.stringify(Array.from(set)));
        return renderList();
      }
      const act = t.closest('[data-ws-act]');
      if (act) {
        const det = act.closest('details');
        if (det) det.removeAttribute('open');
        return runAct(act.dataset.wsAct, Number(act.dataset.id));
      }
      if (t.closest('summary, details, button, a, input, select')) return;
      const row = t.closest('[data-ws-id]');
      if (row) openWorkspace(Number(row.dataset.wsId));
    });
    panel.addEventListener('keydown', function (e) {
      const t = e.target;
      if (t.id === 'tfWsSearch' || t.closest('input,select,textarea')) return;
      if (e.key === '/' ) { const s = document.getElementById('tfWsSearch'); if (s) { e.preventDefault(); s.focus(); } return; }
      if (t.matches('th[data-sort]') && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); t.click(); return; }
      const row = t.closest('[data-ws-id]');
      if (!row) return;
      const id = Number(row.dataset.wsId);
      if (e.key === 'Enter' && t === row) { e.preventDefault(); openWorkspace(id); }
      else if (e.key === 'p' && t === row) { e.preventDefault(); runAct('plan', id); }
      else if (e.key === 'd' && t === row) { e.preventDefault(); runAct('check-drift', id); }
    });
    const search = Tf.debounce(function () { view.q = document.getElementById('tfWsSearch').value.trim(); renderList(); }, 150);
    panel.addEventListener('input', function (e) { if (e.target.id === 'tfWsSearch') search(); });
    panel.addEventListener('change', function (e) {
      const id = e.target.id;
      if (id === 'tfWsEnvFilter') view.env = e.target.value;
      else if (id === 'tfWsTagFilter') view.tag = e.target.value;
      else if (id === 'tfWsGroupFilter') view.group = e.target.value;
      else return;
      renderList();
    });
  }

  // Quick actions shared by the table, cards, and keyboard shortcuts.
  async function runAct(act, id) {
    const ws = wsById(id) || (Tf.state.ws && Tf.state.ws.id === id ? Tf.state.ws : null);
    if (!ws && act !== 'open-config') return;
    if (act === 'plan') return runPlan(id);
    if (act === 'review') {
      if (ws.last_run && Tf.runs && Tf.runs.openViewer) return Tf.runs.openViewer(ws.last_run.id);
      return openWorkspace(id, 'runs');
    }
    if (act === 'check-drift') return checkDrift(ws);
    if (act === 'lock') return lock(ws);
    if (act === 'unlock') return unlock(ws);
    if (act === 'force-unlock') return forceUnlock(ws);
    if (act === 'delete') return remove(ws);
    if (act === 'open-config') return openWorkspace(id, 'config');
    if (act === 'open-state') return openWorkspace(id, 'state');
    if (act === 'open-vars') return openWorkspace(id, 'variables');
    if (act === 'open-settings') return openWorkspace(id, 'settings');
  }
  function runPlan(id, opts) {
    if (Tf.runs && Tf.runs.startDialog) return Tf.runs.startDialog(id, 'plan', opts || {});
    Tf.toast(Tf.t('tf_ws_run_missing', 'Run dialog is not available yet'), 'warning');
  }
  async function checkDrift(ws) {
    try {
      await Tf.api.post('/api/terraform/workspaces/' + ws.id + '/drift/check');
      Tf.toast(Tf.t('tf_ws_drift_queued', 'Drift check started'), 'success');
      await Tf.load.workspaces();
      if (Tf.state.ws && Tf.state.ws.id === ws.id) await reload();
    } catch (e) { Tf.toast(Tf.errMsg(e), 'error'); }
  }

  /* ---------- lock actions (shared with the State and Overview tabs) ---------- */
  async function afterChange(id) {
    await Tf.load.workspaces();
    if (Tf.state.ws && Tf.state.ws.id === id) await reload();
    Tf.emit('workspaces', Tf.state.workspaces);
  }
  function lock(ws) {
    const dlg = Tf.dlg.open({
      title: Tf.t('tf_ws_lock_title', 'Lock workspace'), width: 460,
      body: '<div class="tf-field"><label class="tf-field-label" for="tfLockReason">' + Tf.esc(Tf.t('tf_ws_lock_reason', 'Reason')) + '</label>' +
        '<input id="tfLockReason" class="form-input" maxlength="200" /></div>',
      footer: '<button type="button" class="btn btn-sm" data-dlg-close>' + Tf.esc(Tf.t('tf_ws_dlg_cancel', 'Cancel')) + '</button>' +
        '<button type="button" class="btn btn-sm btn-primary" id="tfLockSave">' + Tf.esc(Tf.t('tf_ws_dlg_lock', 'Lock')) + '</button>',
      onMount: function (el, api) {
        el.querySelector('#tfLockSave').addEventListener('click', async function () {
          try {
            await Tf.api.post('/api/terraform/workspaces/' + ws.id + '/lock', { reason: el.querySelector('#tfLockReason').value });
            api.close();
            Tf.toast(Tf.t('tf_ws_locked_ok', 'Workspace locked'), 'success');
            await afterChange(ws.id);
          } catch (e) { api.error(Tf.errMsg(e)); }
        });
      },
    });
    return dlg;
  }
  async function unlock(ws) {
    try {
      await Tf.api.post('/api/terraform/workspaces/' + ws.id + '/unlock');
      Tf.toast(Tf.t('tf_ws_unlocked_ok', 'Workspace unlocked'), 'success');
      await afterChange(ws.id);
    } catch (e) { Tf.toast(Tf.errMsg(e), 'error'); }
  }
  async function forceUnlock(ws) {
    const ok = await Tf.confirm({ title: Tf.t('tf_ws_force_title', 'Force unlock'), message: Tf.t('tf_ws_force_msg', ''), confirmText: Tf.t('tf_ws_force_unlock', 'Force unlock'), danger: true });
    if (!ok) return;
    try {
      await Tf.api.post('/api/terraform/workspaces/' + ws.id + '/force-unlock', { comment: 'force unlock from dashboard' });
      Tf.toast(Tf.t('tf_ws_unlocked_ok', 'Workspace unlocked'), 'success');
      await afterChange(ws.id);
    } catch (e) { Tf.toast(Tf.errMsg(e), 'error'); }
  }
  async function remove(ws) {
    const ok = await Tf.confirm({ title: Tf.t('tf_ws_delete_title', 'Delete workspace'), message: Tf.t('tf_ws_delete_msg', 'Delete workspace "{name}"?', { name: ws.name }), confirmText: Tf.t('tf_ws_delete', 'Delete'), danger: true });
    if (!ok) return;
    try {
      await Tf.api.del('/api/terraform/workspaces/' + ws.id);
      Tf.toast(Tf.t('tf_ws_deleted', 'Workspace deleted'), 'success');
      if (Tf.state.ws && Tf.state.ws.id === ws.id) await Tf.drawer.close();
      await Tf.load.workspaces();
      if (Tf.state.tab === 'workspaces') renderList();
    } catch (e) { Tf.toast(Tf.errMsg(e), 'error'); }
  }

  /* ---------- workspace drawer shell ---------- */
  const SPLIT_ITEMS = [
    ['speculative', 'tf_ws_speculative', 'Speculative plan'],
    ['refresh', 'tf_ws_refresh_only', 'Refresh only'],
    ['drift', 'tf_ws_check_drift', 'Check drift'],
    ['destroy', 'tf_ws_destroy', 'Destroy'],
    ['lock', 'tf_ws_lock', 'Lock'],
  ];
  function wsBadges(ws) {
    return stBadge(ws) +
      (ws.drift_detected ? ' <span class="badge badge-warning">' + Tf.esc(Tf.t('tf_ws_st_drifted', 'Drifted')) + '</span>' : '') +
      (ws.locked ? ' <span class="badge badge-unknown">' + Tf.icon('lock', 'icon-xs') + ' ' + Tf.esc(ws.lock_reason || ws.locked_by || Tf.t('tf_ws_st_locked', 'Locked')) + '</span>' : '') +
      (ws.environment ? ' <span class="badge">' + Tf.esc(ws.environment) + '</span>' : '') +
      (ws.group_name ? ' <span class="badge">' + Tf.icon('folder', 'icon-xs') + ' ' + Tf.esc(ws.group_name) + '</span>' : '');
  }
  function splitHtml(ws) {
    const lockLabel = ws.locked ? (ws.locked_by === 'manual' ? Tf.t('tf_ws_unlock', 'Unlock') : Tf.t('tf_ws_force_unlock', 'Force unlock')) : Tf.t('tf_ws_lock', 'Lock');
    const lockAct = ws.locked ? (ws.locked_by === 'manual' ? 'unlock' : 'force-unlock') : 'lock';
    const items = SPLIT_ITEMS.map(function (it) {
      const act = it[0] === 'lock' ? lockAct : it[0];
      const label = it[0] === 'lock' ? lockLabel : Tf.t(it[1], it[2]);
      return '<button type="button" role="menuitem" data-ws-act="' + act + '">' + Tf.esc(label) + '</button>';
    }).join('');
    return '<div class="tf-split" id="tfWsSplit">' +
      '<button type="button" class="btn btn-sm btn-primary" data-ws-act="plan">' + Tf.icon('play', 'icon-xs') + ' ' + Tf.esc(Tf.t('tf_ws_plan', 'Plan')) + '</button>' +
      '<details class="tf-more"><summary class="btn btn-sm btn-icon" aria-label="' + Tf.esc(Tf.t('tf_ws_more', 'More actions')) + '">' + Tf.icon('chevron-down', 'icon-sm') + '</summary>' +
      '<div class="tf-menu tf-menu-right" role="menu">' + items + '</div></details></div>';
  }

  // Tab switching. A tab module can set Tf.workspaces.guard = async () => boolean to block leaving (unsaved edits).
  Tf.workspaces = Tf.workspaces || {};
  let wsCurrentTab = null;
  function wsTabsHtml(active) {
    return Tf.wsTabs().map(function (tb) {
      const on = tb.id === active;
      return '<button type="button" class="tab' + (on ? ' active' : '') + '" role="tab" data-tab="' + tb.id + '" aria-selected="' + on + '"' + (on ? '' : ' tabindex="-1"') + '>' +
        (tb.icon ? Tf.icon(tb.icon, 'icon-sm') + ' ' : '') + '<span>' + Tf.esc(Tf.t(tb.labelKey, tb.fallback || tb.id)) + '</span></button>';
    }).join('');
  }
  function ctxFor() {
    return { ws: Tf.state.ws, reload: reload, switchTab: switchWsTab };
  }
  async function renderWsTab(id) {
    const body = document.getElementById('tfWsBody');
    const tb = Tf.wsTabs().find(function (x) { return x.id === id; });
    if (!body || !tb) return;
    if (wsCurrentTab && wsCurrentTab.onHide) { try { wsCurrentTab.onHide(); } catch (e) { console.error(e); } }
    wsCurrentTab = tb;
    Tf.state.sub.ws = id;
    body.innerHTML = '';
    document.querySelectorAll('#tfWsTabs .tab').forEach(function (b) {
      const on = b.dataset.tab === id;
      b.classList.toggle('active', on); b.setAttribute('aria-selected', String(on)); b.tabIndex = on ? 0 : -1;
    });
    try { await tb.render(body, Tf.state.ws, ctxFor()); }
    catch (e) { console.error(e); body.innerHTML = Tf.ui.error(e.message); }
  }
  // Switches the drawer tab; a guard (unsaved edits) can veto the switch.
  async function switchWsTab(id) {
    if (Tf.workspaces.guard && id !== (wsCurrentTab && wsCurrentTab.id)) {
      let ok = true;
      try { ok = (await Tf.workspaces.guard()) !== false; } catch (e) { ok = true; }
      if (!ok) return;
      Tf.workspaces.guard = null;
    }
    return renderWsTab(id);
  }

  // Refetches the open workspace, repaints the header, and re-renders the active drawer tab.
  async function reload() {
    if (!Tf.state.ws) return;
    try {
      const r = await Tf.api.get('/api/terraform/workspaces/' + Tf.state.ws.id);
      Tf.state.ws = r.workspace;
    } catch (e) { Tf.toast(Tf.errMsg(e), 'error'); return; }
    const top = Tf.drawer.current();
    if (top && top.ctrl) {
      top.ctrl.setTitle(Tf.state.ws.name);
      top.ctrl.setSubtitle(wsBadges(Tf.state.ws));
      top.ctrl.setActions(splitHtml(Tf.state.ws));
    }
    const sp = document.getElementById('tfWsSplit');
    if (!sp) return;
    if (wsCurrentTab) await renderWsTab(wsCurrentTab.id);
  }

  function onDrawerClick(e) {
    const tabBtn = e.target.closest('#tfWsTabs .tab');
    if (tabBtn) return switchWsTab(tabBtn.dataset.tab);
    const act = e.target.closest('[data-ws-act]');
    if (!act || !Tf.state.ws) return;
    const det = act.closest('details');
    if (det) det.removeAttribute('open');
    const a = act.dataset.wsAct;
    const ws = Tf.state.ws;
    if (a === 'plan') return runPlan(ws.id);
    if (a === 'speculative') return runPlan(ws.id, { speculative: true });
    if (a === 'refresh') return Tf.runs && Tf.runs.startDialog ? Tf.runs.startDialog(ws.id, 'refresh', {}) : runPlan(ws.id);
    if (a === 'destroy') return Tf.runs && Tf.runs.startDialog ? Tf.runs.startDialog(ws.id, 'destroy', {}) : runPlan(ws.id);
    if (a === 'drift' || a === 'check-drift') return checkDrift(ws);
    if (a === 'lock') return lock(ws);
    if (a === 'unlock') return unlock(ws);
    if (a === 'force-unlock') return forceUnlock(ws);
    if (a === 'delete') return remove(ws);
  }
  function onDrawerKey(e) {
    const tb = e.target.closest('#tfWsTabs .tab');
    if (!tb) return;
    const btns = Array.prototype.slice.call(document.querySelectorAll('#tfWsTabs .tab'));
    let i = btns.indexOf(tb);
    if (e.key === 'ArrowRight') i = (i + 1) % btns.length;
    else if (e.key === 'ArrowLeft') i = (i - 1 + btns.length) % btns.length;
    else return;
    e.preventDefault();
    btns[i].focus();
    switchWsTab(btns[i].dataset.tab);
  }

  let rootWired = false;
  function wireDrawerRoot() {
    if (rootWired) return;
    rootWired = true;
    const root = document.getElementById('tfDrawerRoot');
    root.addEventListener('click', onDrawerClick);
    root.addEventListener('keydown', onDrawerKey);
  }

  // Opens the workspace drawer on the given tab (default: overview).
  async function openWorkspace(id, tab) {
    let ws;
    try { ws = (await Tf.api.get('/api/terraform/workspaces/' + id)).workspace; }
    catch (e) { Tf.toast(Tf.errMsg(e), 'error'); return false; }
    Tf.state.ws = ws;
    const ok = await Tf.drawer.open({
      title: ws.name,
      subtitle: wsBadges(ws),
      actions: splitHtml(ws),
      wide: LS.get('pantau_tf_drawer_wide', '0') === '1',
      wsId: ws.id,
      mount: function (body) {
        wireDrawerRoot();
        const firstTab = Tf.wsTabs()[0];
        const startId = tab || (Tf.state.sub.ws && Tf.wsTabs().some(function (x) { return x.id === Tf.state.sub.ws; }) ? Tf.state.sub.ws : (firstTab && firstTab.id));
        body.innerHTML = '<div id="tfWsTabs" class="tabs tf-drawer-tabs" role="tablist" aria-label="' + Tf.esc(ws.name) + '">' + wsTabsHtml(startId) + '</div>' +
          '<div id="tfWsBody" class="tf-ws-body" role="tabpanel"></div>';
        wsCurrentTab = null;
        renderWsTab(startId);
      },
      unmount: function () {
        if (wsCurrentTab && wsCurrentTab.onHide) { try { wsCurrentTab.onHide(); } catch (e) { console.error(e); } }
        wsCurrentTab = null;
        Tf.state.ws = null;
        Tf.workspaces.guard = null;
      },
    });
    return ok;
  }

  // Wide state is remembered on toggle (the core drawer does not persist it).
  document.addEventListener('click', function (e) {
    if (e.target.closest && e.target.closest('#tfDrawerWide')) {
      setTimeout(function () { LS.set('pantau_tf_drawer_wide', document.getElementById('tfDrawer').classList.contains('wide') ? '1' : '0'); }, 0);
    }
  });

  /* ---------- New Workspace drawer ---------- */
  function openNew() {
    const st = {
      src: 'inline', tpl: '', cur: 'main.tf', files: { 'main.tf': DEFAULT_MAIN }, tplList: null,
    };
    Tf.drawer.open({
      title: Tf.t('tf_ws_new_title', 'New workspace'), wide: true,
      mount: function (body, ctrl) {
        const bins = (Tf.state.status && Tf.state.status.binaries) || [{ name: 'terraform' }, { name: 'tofu' }];
        const firstBin = (bins.find(function (b) { return !b.err; }) || bins[0]).name;
        body.innerHTML =
          '<div class="tf-new">' +
          '<div class="tf-section-title">' + Tf.esc(Tf.t('tf_ws_tpl_blank', 'Blank workspace')) + '</div>' +
          '<div id="tfTplGallery" class="tf-tpl-gallery" role="group" aria-label="' + Tf.esc(Tf.t('tf_ws_new_title', '')) + '">' +
          '<button type="button" class="tf-tpl-card on" data-tpl="" aria-pressed="true">' + Tf.icon('file-code', 'icon') + '<span>' + Tf.esc(Tf.t('tf_ws_tpl_blank', 'Blank workspace')) + '</span></button></div>' +
          '<div id="tfTplInfo" class="tf-hint"></div>' +
          '<div class="tf-grid-2">' +
          field('tfNewWsName', Tf.t('tf_ws_f_name', 'Name'), '<input id="tfNewWsName" class="form-input" maxlength="100" />') +
          field('tfNewWsGroup', Tf.t('tf_ws_f_group', 'Group'), '<input id="tfNewWsGroup" class="form-input" list="tfNewWsGroupList" /><datalist id="tfNewWsGroupList">' + groupOptions() + '</datalist>') +
          field('tfNewWsEnv', Tf.t('tf_ws_f_env', 'Environment'), '<input id="tfNewWsEnv" class="form-input" list="tfNewWsEnvList" placeholder="production" /><datalist id="tfNewWsEnvList"><option value="production"><option value="staging"><option value="development"></datalist>') +
          field('tfNewWsTags', Tf.t('tf_ws_f_tags', 'Tags'), '<input id="tfNewWsTags" class="form-input" />') +
          field('tfNewWsBinary', Tf.t('tf_ws_f_binary', 'Binary'), '<select id="tfNewWsBinary" class="form-input">' + bins.map(function (b) { return '<option value="' + Tf.esc(b.name) + '"' + (b.name === firstBin ? ' selected' : '') + '>' + Tf.esc(b.name + (b.err ? ' (' + b.err + ')' : '')) + '</option>'; }).join('') + '</select>') +
          field('tfNewWsDrift', Tf.t('tf_ws_f_drift', 'Drift check interval'), '<input id="tfNewWsDrift" class="form-input" type="number" min="0" value="0" />') +
          field('tfNewWsPlan', Tf.t('tf_ws_f_plan', 'Scheduled plan interval'), '<input id="tfNewWsPlanInt" class="form-input" type="number" min="0" value="0" />') +
          field('tfNewWsDesc', Tf.t('tf_ws_f_desc', 'Description'), '<input id="tfNewWsDesc" class="form-input" />', true) +
          '<label class="tf-check"><input type="checkbox" id="tfNewWsAuto" /> <span>' + Tf.esc(Tf.t('tf_ws_f_autoapply', 'Auto-apply clean plans')) + '</span></label>' +
          '<div id="tfNewWsAutoHint" class="tf-hint" hidden>' + Tf.esc(Tf.t('tf_ws_autoapply_prod', '')) + '</div>' +
          '</div>' +
          '<div class="tf-seg" role="group" aria-label="' + Tf.esc(Tf.t('tf_ws_src', 'Source')) + '">' +
          '<button type="button" data-src="inline" aria-pressed="true">' + Tf.icon('file-code', 'icon-xs') + ' ' + Tf.esc(Tf.t('tf_ws_src_inline', 'Inline')) + '</button>' +
          '<button type="button" data-src="git" aria-pressed="false">' + Tf.icon('git-branch', 'icon-xs') + ' ' + Tf.esc(Tf.t('tf_ws_src_git', 'Git')) + '</button></div>' +
          '<div id="tfNewWsInline">' +
          '<div class="tf-field-label">' + Tf.esc(Tf.t('tf_ws_files', 'Files')) + '</div>' +
          '<div id="tfNewWsFiles" class="tf-file-tabs" role="tablist"></div>' +
          '<textarea id="tfNewWsCode" class="tf-code" spellcheck="false" aria-label="' + Tf.esc(Tf.t('tf_ws_files', 'Files')) + '"></textarea>' +
          '<div class="tf-row"><input id="tfNewWsFileName" class="form-input tf-file-name" placeholder="' + Tf.esc(Tf.t('tf_ws_file_ph', '')) + '" />' +
          '<button type="button" class="btn btn-sm" data-new-act="add-file">' + Tf.esc(Tf.t('tf_ws_file_add', 'Add file')) + '</button>' +
          '<button type="button" class="btn btn-sm" data-new-act="del-file">' + Tf.esc(Tf.t('tf_ws_file_del', 'Remove file')) + '</button></div></div>' +
          '<div id="tfNewWsGit" class="tf-grid-2" hidden>' +
          field('tfNewWsGitUrl', Tf.t('tf_ws_git_url', 'Repository URL'), '<input id="tfNewWsGitUrl" class="form-input" placeholder="https://github.com/org/infra.git" />', true) +
          field('tfNewWsGitBranch', Tf.t('tf_ws_git_branch', 'Branch'), '<input id="tfNewWsGitBranch" class="form-input" placeholder="main" />') +
          field('tfNewWsGitPath', Tf.t('tf_ws_git_path', 'Working directory'), '<input id="tfNewWsGitPath" class="form-input" placeholder="envs/prod" />') +
          field('tfNewWsGitAuth', Tf.t('tf_ws_git_auth', 'Authentication'), '<select id="tfNewWsGitAuth" class="form-input"><option value="none">' + Tf.esc(Tf.t('tf_ws_git_auth_none', 'None')) + '</option><option value="token">' + Tf.esc(Tf.t('tf_ws_git_auth_token', 'Token')) + '</option><option value="ssh">' + Tf.esc(Tf.t('tf_ws_git_auth_ssh', 'SSH key')) + '</option></select>') +
          '<div id="tfNewWsTokenWrap" hidden>' + field('tfNewWsGitToken', Tf.t('tf_ws_git_token', 'Token'), '<input id="tfNewWsGitToken" class="form-input" type="password" autocomplete="new-password" />') + '</div>' +
          '<div id="tfNewWsKeyWrap" hidden>' + field('tfNewWsGitKey', Tf.t('tf_ws_git_key', 'SSH key'), '<select id="tfNewWsGitKey" class="form-input"></select>') + '</div>' +
          '</div>' +
          '<div id="tfNewWsErr" class="tf-error" role="alert" hidden></div>' +
          '<div class="tf-row tf-new-foot"><button type="button" class="btn btn-sm" data-new-act="cancel">' + Tf.esc(Tf.t('tf_ws_cancel', 'Cancel')) + '</button>' +
          '<button type="button" class="btn btn-sm btn-primary" id="tfNewWsSave">' + Tf.esc(Tf.t('tf_ws_create', 'Create workspace')) + '</button></div>' +
          '</div>';
        wireNew(body, ctrl, st);
      },
    });
  }
  function field(id, label, inner, full) {
    return '<div class="tf-field' + (full ? ' tf-full' : '') + '"><label class="tf-field-label" for="' + id + '">' + Tf.esc(label) + '</label>' + inner + '</div>';
  }
  function groupOptions() {
    const gs = Array.from(new Set(Tf.state.workspaces.map(function (w) { return (w.group_name || '').trim(); }).filter(Boolean))).sort();
    return gs.map(function (g) { return '<option value="' + Tf.esc(g) + '">'; }).join('');
  }

  function wireNew(body, ctrl, st) {
    const code = body.querySelector('#tfNewWsCode');
    const renderFiles = function () {
      const names = Object.keys(st.files);
      if (!names.includes(st.cur)) st.cur = names[0] || '';
      body.querySelector('#tfNewWsFiles').innerHTML = names.map(function (n) {
        const on = n === st.cur;
        return '<button type="button" role="tab" class="tab' + (on ? ' active' : '') + '" data-file="' + Tf.esc(n) + '" aria-selected="' + on + '">' + Tf.esc(n) + '</button>';
      }).join('');
      code.value = st.files[st.cur] || '';
      code.disabled = !st.cur;
    };
    const saveCur = function () { if (st.cur) st.files[st.cur] = code.value; };
    const setSrc = function (src) {
      st.src = src;
      body.querySelectorAll('[data-src]').forEach(function (b) { b.setAttribute('aria-pressed', String(b.dataset.src === src)); });
      body.querySelector('#tfNewWsInline').hidden = src !== 'inline';
      body.querySelector('#tfNewWsGit').hidden = src !== 'git';
    };
    const authChanged = function () {
      const a = body.querySelector('#tfNewWsGitAuth').value;
      body.querySelector('#tfNewWsTokenWrap').hidden = a !== 'token';
      body.querySelector('#tfNewWsKeyWrap').hidden = a !== 'ssh';
    };
    const autoChanged = function () {
      const prod = body.querySelector('#tfNewWsEnv').value.trim() === 'production';
      const cb = body.querySelector('#tfNewWsAuto');
      if (prod) cb.checked = false;
      cb.disabled = prod;
      body.querySelector('#tfNewWsAutoHint').hidden = !prod;
    };
    renderFiles();
    setSrc('inline');
    autoChanged();
    // Git auth keys: loaded once; failure leaves the select empty (saving SSH auth then fails server-side).
    Tf.api.get('/api/ssh-keys').then(function (keys) {
      const sel = body.querySelector('#tfNewWsGitKey');
      if (sel) sel.innerHTML = (Array.isArray(keys) ? keys : []).map(function (k) { return '<option value="' + Number(k.id) + '">' + Tf.esc(k.name) + '</option>'; }).join('');
    }).catch(function () { /* optional list */ });
    // Templates: the gallery is filled from GET /api/terraform/templates.
    Tf.api.get('/api/terraform/templates').then(function (r) {
      st.tplList = (r && r.templates) || [];
      const gal = body.querySelector('#tfTplGallery');
      if (!gal) return;
      gal.insertAdjacentHTML('beforeend', st.tplList.map(function (p) {
        return '<button type="button" class="tf-tpl-card" data-tpl="' + Tf.esc(p.id) + '" aria-pressed="false">' + Tf.icon('layers', 'icon') + '<span>' + Tf.esc(p.name) + '</span>' +
          (PROVIDERS[p.provider] ? '<span class="badge">' + Tf.esc(PROVIDERS[p.provider]) + '</span>' : '') + '</button>';
      }).join(''));
    }).catch(function () { /* blank workspace still works */ });

    // Listeners go on the freshly rendered form root: the drawer body element is reused between openings.
    const root = body.querySelector('.tf-new');
    root.addEventListener('click', function (e) {
      const t = e.target;
      const tpl = t.closest('[data-tpl]');
      if (tpl) {
        saveCur();
        const id = tpl.dataset.tpl;
        body.querySelectorAll('[data-tpl]').forEach(function (b) { const on = b === tpl; b.classList.toggle('on', on); b.setAttribute('aria-pressed', String(on)); });
        const p = (st.tplList || []).find(function (x) { return x.id === id; });
        const name = body.querySelector('#tfNewWsName');
        if (p) {
          if (!name.value.trim() || name.value === st.autoName) { name.value = p.id; st.autoName = p.id; }
          st.files = Object.assign({}, p.files || {});
          st.cur = Object.keys(st.files)[0] || '';
          setSrc('inline');
          const vars = (p.variables || []).map(function (v) { return v.name; }).join(', ');
          body.querySelector('#tfTplInfo').innerHTML = Tf.esc(p.description || '') + (vars ? '<div>' + Tf.esc(Tf.t('tf_ws_tpl_vars', '', { vars: vars })) + '</div>' : '') +
            (PROVIDERS[p.provider] ? '<div>' + Tf.icon('info', 'icon-xs') + ' ' + Tf.esc(Tf.t('tf_ws_tpl_cloud', '', { provider: PROVIDERS[p.provider] })) + '</div>' : '');
        } else {
          if (name.value === st.autoName) name.value = '';
          st.autoName = null;
          st.files = { 'main.tf': DEFAULT_MAIN };
          st.cur = 'main.tf';
          body.querySelector('#tfTplInfo').textContent = '';
        }
        renderFiles();
        return;
      }
      const f = t.closest('[data-file]');
      if (f) { saveCur(); st.cur = f.dataset.file; renderFiles(); return; }
      const s = t.closest('[data-src]');
      if (s) return setSrc(s.dataset.src);
      const na = t.closest('[data-new-act]');
      if (na) {
        const a = na.dataset.newAct;
        if (a === 'cancel') return ctrl.close();
        if (a === 'add-file') {
          const nm = body.querySelector('#tfNewWsFileName').value.trim();
          if (!nm || !/^[\w.\-\/]+$/.test(nm) || st.files[nm] !== undefined) return;
          saveCur(); st.files[nm] = ''; st.cur = nm; body.querySelector('#tfNewWsFileName').value = ''; renderFiles();
          return;
        }
        if (a === 'del-file') {
          if (!st.cur) return;
          const rest = Object.keys(st.files).filter(function (n) { return n !== st.cur; });
          delete st.files[st.cur];
          st.cur = rest[0] || '';
          renderFiles();
        }
      }
    });
    root.addEventListener('change', function (e) {
      if (e.target.id === 'tfNewWsGitAuth') authChanged();
      if (e.target.id === 'tfNewWsEnv') autoChanged();
    });
    root.addEventListener('input', function (e) { if (e.target.id === 'tfNewWsEnv') autoChanged(); });
    body.querySelector('#tfNewWsSave').addEventListener('click', function () { saveNew(body, ctrl, st, saveCur); });
  }

  async function saveNew(body, ctrl, st, saveCur) {
    const err = body.querySelector('#tfNewWsErr');
    const fail = function (msg) { err.textContent = msg; err.hidden = false; };
    err.hidden = true;
    const name = body.querySelector('#tfNewWsName').value.trim();
    if (!name) return fail(Tf.t('tf_ws_name_required', 'Name is required'));
    saveCur();
    const v = function (id) { return body.querySelector('#' + id).value; };
    const payload = {
      name: name, description: v('tfNewWsDesc'), group_name: v('tfNewWsGroup').trim(), environment: v('tfNewWsEnv').trim(),
      tags: Tf.csv(v('tfNewWsTags')), binary: v('tfNewWsBinary'), source: st.src,
      drift_interval_minutes: parseInt(v('tfNewWsDrift'), 10) || 0, plan_interval_minutes: parseInt(v('tfNewWsPlanInt'), 10) || 0,
      auto_apply: body.querySelector('#tfNewWsAuto').checked && !body.querySelector('#tfNewWsAuto').disabled,
    };
    if (st.src === 'inline') payload.files = st.files;
    else {
      Object.assign(payload, { git_url: v('tfNewWsGitUrl').trim(), git_branch: v('tfNewWsGitBranch').trim(), git_path: v('tfNewWsGitPath').trim(), git_auth: v('tfNewWsGitAuth') });
      if (payload.git_auth === 'token' && v('tfNewWsGitToken')) payload.git_token = v('tfNewWsGitToken');
      if (payload.git_auth === 'ssh') payload.git_key_id = parseInt(v('tfNewWsGitKey'), 10) || 0;
    }
    const btn = body.querySelector('#tfNewWsSave');
    btn.disabled = true;
    try {
      const r = await Tf.api.post('/api/terraform/workspaces', payload);
      Tf.toast(Tf.t('tf_ws_created', 'Workspace created'), 'success');
      await Tf.load.workspaces();
      const id = r.workspace.id;
      await openWorkspace(id, 'config');
    } catch (e) {
      fail(Tf.errMsg(e));
      btn.disabled = false;
    }
  }

  /* ---------- public namespace ---------- */
  Tf.workspaces = Object.assign(Tf.workspaces, {
    list: function () { return Tf.state.workspaces; },
    open: openWorkspace,
    openNew: openNew,
    reload: reload,
    switchTab: switchWsTab,
    lock: lock,
    unlock: unlock,
    forceUnlock: forceUnlock,
    checkDrift: checkDrift,
    remove: remove,
    runAct: runAct,
    guard: null,
  });
  Tf.openWorkspace = openWorkspace;

  /* ---------- top-level tab ---------- */
  Tf.on('workspaces', renderList);
  Tf.registerTab({
    id: 'workspaces', icon: 'layers', labelKey: 'tf_tab_workspaces', fallback: 'Workspaces', order: 1,
    render: function (panel) { buildPanel(panel); },
    onShow: async function (panel) {
      if (!panel.querySelector('#tfWsGrid')) buildPanel(panel);
      await Tf.load.workspaces();
      renderList();
    },
  });
})();
