/* Playbooks tab: dense table, filters, playbook drawer (Overview | Edit | Runs | Schedule), folder import. */
(function () {
  'use strict';
  const Ans = window.Ans;
  const esc = Ans.esc, T = Ans.t, icon = Ans.icon;

  Ans.addI18n({
    en: {
      ans_pb_search: 'Search playbooks…', ans_pb_all_groups: 'All groups', ans_pb_ungrouped: 'Ungrouped', ans_pb_count: '{n} playbooks',
      ans_pb_count_one: '1 playbook', ans_pb_count_filtered: '{n} of {m} playbooks',
      ans_pb_chip_all: 'All', ans_pb_chip_failed: 'Failed', ans_pb_chip_never: 'Never run', ans_pb_chip_scheduled: 'Scheduled', ans_pb_chip_git: 'Git',
      ans_pb_group_by: 'Group by host group', ans_pb_import: 'Import folder', ans_pb_new: 'New playbook',
      ans_pb_col_name: 'Playbook', ans_pb_col_group: 'Group', ans_pb_col_last: 'Last run', ans_pb_col_result: 'Result', ans_pb_col_schedule: 'Schedule',
      ans_pb_run: 'Run', ans_pb_run_off: 'Enable the Ansible Gate in Settings to run playbooks', ans_pb_more: 'More actions',
      ans_pb_menu_edit: 'Edit', ans_pb_menu_history: 'Run history', ans_pb_menu_schedule: 'Schedules', ans_pb_menu_delete: 'Delete',
      ans_pb_targets: '{n} hosts', ans_pb_target_one: '1 host', ans_pb_git: 'git', ans_pb_inline: 'inline', ans_pb_survey: 'survey',
      ans_pb_empty_title: 'No playbooks yet', ans_pb_empty_desc: 'Create a playbook or import a folder of existing ones. Inventory is generated from your Pantau hosts.',
      ans_pb_no_match: 'No playbooks match the filters', ans_pb_clear_filters: 'Clear filters',
      ans_pb_load_failed: 'Could not load playbooks',
      ans_pb_delete_title: 'Delete playbook?', ans_pb_delete_msg: 'Delete "{name}" together with its run history and schedules? This cannot be undone.',
      ans_pb_deleted: 'Playbook deleted', ans_pb_delete_busy: 'A run of this playbook is active; cancel it first.', ans_pb_delete_failed: 'Delete failed',
      ans_pb_tab_overview: 'Overview', ans_pb_tab_edit: 'Edit', ans_pb_tab_runs: 'Runs', ans_pb_tab_schedule: 'Schedule', ans_pb_tab_new: 'New playbook',
      ans_pb_discard_title: 'Discard unsaved changes?', ans_pb_discard_msg: 'The playbook has changes that were not saved.', ans_pb_discard: 'Discard',
      ans_pb_launch_missing: 'The launch panel is not available.', ans_pb_editor_missing: 'The editor is not available.',
      ans_pb_runs_missing: 'The run history is not available.', ans_pb_sched_missing: 'The schedule editor is not available.',
      ans_pb_card_last: 'Last run', ans_pb_card_timeline: 'Recent runs', ans_pb_card_schedule: 'Schedule', ans_pb_view_run: 'View run',
      ans_pb_no_runs: 'This playbook has not been run yet.', ans_pb_no_schedules: 'No schedules. Add one to run this playbook automatically.',
      ans_pb_manage_sched: 'Manage schedules', ans_pb_next_run: 'Next run {when}', ans_pb_sched_count: '{n} schedules',
      ans_pb_sched_count_one: '1 schedule', ans_pb_host: 'Host', ans_pb_ok: 'ok', ans_pb_changed: 'changed', ans_pb_failed: 'failed',
      ans_pb_unreachable: 'unreachable', ans_pb_skipped: 'skipped', ans_pb_pip_title: 'Run #{id} · {status} · {when}',
      ans_pb_loading_failed: 'Could not load this section',
      ans_pb_imp_title: 'Import folder', ans_pb_imp_warning: 'Only the ticked playbook YAML files are copied into Pantau. The folder itself is not stored.',
      ans_pb_imp_folder: 'Folder', ans_pb_imp_hint: 'Select the folder that contains your playbooks. .git, __pycache__, .DS_Store, .ansible/ and collections/ are skipped. Pantau marks the YAML files it detects as playbooks; tick the ones to import.',
      ans_pb_imp_preview: '{count} files · {size}', ans_pb_imp_select_all: 'Select all', ans_pb_imp_selected: '{n} of {m} selected',
      ans_pb_imp_fallback: 'No top-level hosts or import_playbook entries detected. You can still select YAML candidates manually.',
      ans_pb_imp_col_path: 'Path', ans_pb_imp_col_name: 'Playbook name', ans_pb_imp_busy: 'Importing…', ans_pb_imp_cancel: 'Cancel',
      ans_pb_imp_err_path: 'Folder contains an invalid or duplicate relative path.', ans_pb_imp_err_count: 'Select at most 10,000 files (after exclusions).',
      ans_pb_imp_err_size: 'Folder exceeds the 64 MiB combined file size limit.', ans_pb_imp_err_files: 'Select a folder containing files to import.',
      ans_pb_imp_err_yaml: 'No main playbook YAML found outside dependency, inventory, or documentation folders.',
      ans_pb_imp_err_none: 'Select at least one playbook to import.', ans_pb_imp_err_name: 'Playbook name cannot be empty.',
      ans_pb_imp_err_dup: 'Duplicate playbook name: "{name}"', ans_pb_imp_err_taken: 'Playbook name already exists: "{name}"',
      ans_pb_imp_err_big: 'A selected playbook YAML exceeds 256 KiB.', ans_pb_imp_failed: 'Failed to import folder.', ans_pb_imp_done: 'Playbooks imported',
    },
    id: {
      ans_pb_search: 'Cari playbook…', ans_pb_all_groups: 'Semua grup', ans_pb_ungrouped: 'Tanpa grup', ans_pb_count: '{n} playbook',
      ans_pb_count_one: '1 playbook', ans_pb_count_filtered: '{n} dari {m} playbook',
      ans_pb_chip_all: 'Semua', ans_pb_chip_failed: 'Gagal', ans_pb_chip_never: 'Belum pernah', ans_pb_chip_scheduled: 'Terjadwal', ans_pb_chip_git: 'Git',
      ans_pb_group_by: 'Kelompokkan per grup host', ans_pb_import: 'Impor folder', ans_pb_new: 'Playbook baru',
      ans_pb_col_name: 'Playbook', ans_pb_col_group: 'Grup', ans_pb_col_last: 'Run terakhir', ans_pb_col_result: 'Hasil', ans_pb_col_schedule: 'Jadwal',
      ans_pb_run: 'Jalankan', ans_pb_run_off: 'Aktifkan Ansible Gate di Pengaturan untuk menjalankan playbook', ans_pb_more: 'Aksi lainnya',
      ans_pb_menu_edit: 'Edit', ans_pb_menu_history: 'Riwayat run', ans_pb_menu_schedule: 'Jadwal', ans_pb_menu_delete: 'Hapus',
      ans_pb_targets: '{n} host', ans_pb_target_one: '1 host', ans_pb_git: 'git', ans_pb_inline: 'inline', ans_pb_survey: 'survey',
      ans_pb_empty_title: 'Belum ada playbook', ans_pb_empty_desc: 'Buat playbook atau impor folder berisi playbook yang sudah ada. Inventory dihasilkan dari Host Pantau Anda.',
      ans_pb_no_match: 'Tidak ada playbook yang cocok dengan filter', ans_pb_clear_filters: 'Hapus filter',
      ans_pb_load_failed: 'Gagal memuat playbook',
      ans_pb_delete_title: 'Hapus playbook?', ans_pb_delete_msg: 'Hapus "{name}" beserta riwayat run dan jadwalnya? Tindakan ini tidak dapat dibatalkan.',
      ans_pb_deleted: 'Playbook dihapus', ans_pb_delete_busy: 'Ada run aktif untuk playbook ini; batalkan dulu.', ans_pb_delete_failed: 'Gagal menghapus',
      ans_pb_tab_overview: 'Ringkasan', ans_pb_tab_edit: 'Edit', ans_pb_tab_runs: 'Run', ans_pb_tab_schedule: 'Jadwal', ans_pb_tab_new: 'Playbook baru',
      ans_pb_discard_title: 'Buang perubahan yang belum disimpan?', ans_pb_discard_msg: 'Playbook memiliki perubahan yang belum disimpan.', ans_pb_discard: 'Buang',
      ans_pb_launch_missing: 'Panel peluncuran tidak tersedia.', ans_pb_editor_missing: 'Editor tidak tersedia.',
      ans_pb_runs_missing: 'Riwayat run tidak tersedia.', ans_pb_sched_missing: 'Editor jadwal tidak tersedia.',
      ans_pb_card_last: 'Run terakhir', ans_pb_card_timeline: 'Run terbaru', ans_pb_card_schedule: 'Jadwal', ans_pb_view_run: 'Lihat run',
      ans_pb_no_runs: 'Playbook ini belum pernah dijalankan.', ans_pb_no_schedules: 'Belum ada jadwal. Tambahkan untuk menjalankan playbook ini otomatis.',
      ans_pb_manage_sched: 'Kelola jadwal', ans_pb_next_run: 'Run berikutnya {when}', ans_pb_sched_count: '{n} jadwal',
      ans_pb_sched_count_one: '1 jadwal', ans_pb_host: 'Host', ans_pb_ok: 'ok', ans_pb_changed: 'berubah', ans_pb_failed: 'gagal',
      ans_pb_unreachable: 'tak terjangkau', ans_pb_skipped: 'dilewati', ans_pb_pip_title: 'Run #{id} · {status} · {when}',
      ans_pb_loading_failed: 'Gagal memuat bagian ini',
      ans_pb_imp_title: 'Impor folder', ans_pb_imp_warning: 'Hanya file YAML playbook yang dicentang yang disalin ke Pantau. Folder itu sendiri tidak disimpan.',
      ans_pb_imp_folder: 'Folder', ans_pb_imp_hint: 'Pilih folder yang berisi playbook Anda. .git, __pycache__, .DS_Store, .ansible/ dan collections/ dilewati. Pantau menandai file YAML yang terdeteksi sebagai playbook; centang yang ingin diimpor.',
      ans_pb_imp_preview: '{count} berkas · {size}', ans_pb_imp_select_all: 'Pilih semua', ans_pb_imp_selected: '{n} dari {m} dipilih',
      ans_pb_imp_fallback: 'Tidak ada entri hosts atau import_playbook tingkat atas yang terdeteksi. Anda tetap dapat memilih kandidat YAML secara manual.',
      ans_pb_imp_col_path: 'Path', ans_pb_imp_col_name: 'Nama playbook', ans_pb_imp_busy: 'Mengimpor…', ans_pb_imp_cancel: 'Batal',
      ans_pb_imp_err_path: 'Folder berisi path relatif yang tidak valid atau ganda.', ans_pb_imp_err_count: 'Pilih paling banyak 10.000 berkas (setelah pengecualian).',
      ans_pb_imp_err_size: 'Folder melebihi batas gabungan ukuran berkas 64 MiB.', ans_pb_imp_err_files: 'Pilih folder yang berisi berkas untuk diimpor.',
      ans_pb_imp_err_yaml: 'Tidak ada YAML playbook utama di luar folder dependensi, inventory, atau dokumentasi.',
      ans_pb_imp_err_none: 'Pilih setidaknya satu playbook untuk diimpor.', ans_pb_imp_err_name: 'Nama playbook tidak boleh kosong.',
      ans_pb_imp_err_dup: 'Nama playbook ganda: "{name}"', ans_pb_imp_err_taken: 'Nama playbook sudah ada: "{name}"',
      ans_pb_imp_err_big: 'YAML playbook yang dipilih melebihi 256 KiB.', ans_pb_imp_failed: 'Gagal mengimpor folder.', ans_pb_imp_done: 'Playbook diimpor',
    },
  });

  const st = {
    q: '', group: '', chip: 'all', rendered: false, loading: true, error: '', root: null,
    sort: Ans.store.get('pb.sort', { key: 'name', dir: 1 }),
    groupBy: !!Ans.store.get('pb.groupBy', false),
    collapsed: Ans.store.get('pb.collapsed', {}),
  };
  const gateOn = function () { const s = Ans.state.status; return !!(s && s.available && s.enabled); };
  const all = function () { return Ans.state.playbooks || []; };
  const byId = function (id) { return all().find(function (p) { return p.id === Number(id); }); };
  const statusOf = function (p) { return p.last_status || 'never_run'; };
  const isActive = function (p) { return p.last_status === 'running' || p.last_status === 'queued'; };

  /* ---------- table ---------- */
  function visible() {
    const q = st.q.trim().toLowerCase();
    const rows = all().filter(function (p) {
      const g = (p.group_name || '').trim();
      if (st.group === '__none__' ? g : (st.group && g !== st.group)) return false;
      if (st.chip === 'failed' && p.last_status !== 'failed') return false;
      if (st.chip === 'never' && p.last_status) return false;
      if (st.chip === 'scheduled' && !(p.schedule_count > 0)) return false;
      if (st.chip === 'git' && p.source !== 'git') return false;
      return !q || (p.name + ' ' + (p.description || '') + ' ' + g).toLowerCase().indexOf(q) >= 0;
    });
    const dir = st.sort.dir, key = st.sort.key;
    rows.sort(function (a, b) {
      let r;
      if (key === 'group') r = (a.group_name || '').localeCompare(b.group_name || '');
      else if (key === 'last') r = (Date.parse(a.last_run_at) || 0) - (Date.parse(b.last_run_at) || 0);
      else r = a.name.localeCompare(b.name, undefined, { sensitivity: 'base' });
      return (r || a.name.localeCompare(b.name)) * dir;
    });
    return rows;
  }

  function badgeChips(p) {
    let h = '';
    if (p.source === 'git') h += '<span class="badge badge-unknown ans-pb-tag" title="' + esc(p.git_url || '') + '">' + icon('link', 'icon-sm') + esc(T('ans_pb_git', 'git')) + '</span>';
    if (p.has_survey) h += '<span class="badge badge-unknown ans-pb-tag">' + icon('list-checks', 'icon-sm') + esc(T('ans_pb_survey', 'survey')) + '</span>';
    return h;
  }

  function lastCell(p) {
    if (!p.last_run_at) return '<span class="ans-muted">-</span>';
    const bits = [];
    if (p.last_duration_ms !== undefined && p.last_duration_ms !== null) bits.push(Ans.fmt.dur(p.last_duration_ms));
    if (p.last_targets !== undefined && p.last_targets !== null) bits.push(p.last_targets === 1 ? T('ans_pb_target_one', '1 host') : T('ans_pb_targets', '{n} hosts', { n: p.last_targets }));
    return '<div title="' + esc(Ans.fmt.dt(p.last_run_at)) + '">' + esc(Ans.fmt.ago(p.last_run_at)) + '</div>' + (bits.length ? '<div class="ans-sub">' + esc(bits.join(' · ')) + '</div>' : '');
  }

  function scheduleCell(p) {
    if (!(p.schedule_count > 0)) return '<span class="ans-muted">-</span>';
    const n = p.schedule_count > 1 ? ' <span class="ans-muted">+' + (p.schedule_count - 1) + '</span>' : '';
    if (!p.next_run_at) return '<span class="ans-muted">' + esc(p.schedule_count === 1 ? T('ans_pb_sched_count_one', '1 schedule') : T('ans_pb_sched_count', '{n} schedules', { n: p.schedule_count })) + '</span>';
    return '<span title="' + esc(Ans.fmt.dt(p.next_run_at)) + '">' + icon('calendar', 'icon-sm') + ' ' + esc(Ans.fmt.ago(p.next_run_at)) + '</span>' + n;
  }

  function rowHtml(p) {
    const s = statusOf(p), off = !gateOn();
    return '<tr class="ans-clickable" data-id="' + p.id + '" tabindex="0" role="row">' +
      '<td class="ans-pb-st">' + Ans.ui.dot(s) + '<span class="ans-sr">' + esc(Ans.ui.statusLabel(s)) + '</span></td>' +
      '<td><div class="ans-name"><span class="ans-pb-nm">' + esc(p.name) + '</span>' + badgeChips(p) + '</div>' + (p.description ? '<div class="ans-sub" title="' + esc(p.description) + '">' + esc(p.description) + '</div>' : '') + '</td>' +
      '<td class="ans-hide-md">' + (p.group_name ? esc(p.group_name) : '<span class="ans-muted">-</span>') + '</td>' +
      '<td>' + lastCell(p) + '</td>' +
      '<td class="ans-hide-md">' + Ans.fmt.pips(p.last_recap) + '</td>' +
      '<td class="ans-hide-md">' + scheduleCell(p) + '</td>' +
      '<td class="ans-actions"><button type="button" class="btn btn-sm btn-primary" data-act="run" data-id="' + p.id + '"' + (off ? ' disabled title="' + esc(T('ans_pb_run_off')) + '"' : '') + '>' + icon('play', 'icon-sm') + ' <span>' + esc(T('ans_pb_run', 'Run')) + '</span></button> ' +
      '<button type="button" class="btn btn-sm btn-ghost ans-btn-icon" data-act="menu" data-id="' + p.id + '" title="' + esc(T('ans_pb_more')) + '" aria-label="' + esc(T('ans_pb_more')) + '" aria-haspopup="menu">' + icon('ellipsis', 'icon-sm') + '</button></td></tr>';
  }

  function thHtml(key, label, cls) {
    const sorted = st.sort.key === key;
    const arrow = sorted ? ' ' + icon(st.sort.dir > 0 ? 'arrow-up' : 'arrow-down', 'icon-sm') : '';
    return '<th scope="col"' + (cls ? ' class="' + cls + '"' : '') + ' data-sort="' + key + '" aria-sort="' + (sorted ? (st.sort.dir > 0 ? 'ascending' : 'descending') : 'none') + '">' + esc(label) + arrow + '</th>';
  }

  function renderRows() {
    if (!st.rendered) return;
    const wrap = st.root.querySelector('[data-pb="table"]');
    const list = all();
    const rows = visible();
    renderChips(list);
    const cnt = st.root.querySelector('#ansiblePlaybookCountBadge');
    cnt.textContent = rows.length === list.length ? (list.length === 1 ? T('ans_pb_count_one', '1 playbook') : T('ans_pb_count', '{n} playbooks', { n: list.length })) : T('ans_pb_count_filtered', '{n} of {m} playbooks', { n: rows.length, m: list.length });
    const empty = st.root.querySelector('#ansiblePlaybooksEmpty');
    if (st.loading && !list.length) { wrap.hidden = false; wrap.innerHTML = Ans.ui.skeleton(6); empty.hidden = true; return; }
    if (st.error && !list.length) { wrap.hidden = true; empty.hidden = false; empty.innerHTML = Ans.ui.error(st.error, 'data-act="retry"'); return; }
    if (!list.length) {
      wrap.hidden = true; empty.hidden = false;
      empty.innerHTML = Ans.ui.empty({ icon: 'ansible', title: T('ans_pb_empty_title'), desc: T('ans_pb_empty_desc'),
        actionHtml: '<div class="ans-row"><button type="button" class="btn btn-sm" data-act="import">' + icon('folder', 'icon-sm') + ' ' + esc(T('ans_pb_import')) + '</button><button type="button" class="btn btn-sm btn-primary" data-act="new">' + icon('plus', 'icon-sm') + ' ' + esc(T('ans_pb_new')) + '</button></div>' });
      return;
    }
    if (!rows.length) {
      wrap.hidden = true; empty.hidden = false;
      empty.innerHTML = Ans.ui.empty({ icon: 'search', title: T('ans_pb_no_match'), actionHtml: '<button type="button" class="btn btn-sm" data-act="clear">' + esc(T('ans_pb_clear_filters')) + '</button>' });
      return;
    }
    empty.hidden = true; wrap.hidden = false;
    let body = '';
    if (st.groupBy) {
      const groups = {};
      rows.forEach(function (p) { const g = (p.group_name || '').trim(); (groups[g] = groups[g] || []).push(p); });
      Object.keys(groups).sort(function (a, b) { return !a ? 1 : !b ? -1 : a.localeCompare(b); }).forEach(function (g) {
        const key = g || '__none__', col = !!st.collapsed[key];
        body += '<tr class="ans-group-row" data-group="' + esc(key) + '" tabindex="0" aria-expanded="' + !col + '"><td colspan="7">' + icon(col ? 'chevron-right' : 'chevron-down', 'icon-sm') + ' ' + esc(g || T('ans_pb_ungrouped')) + ' <span class="ans-muted">' + groups[g].length + '</span></td></tr>';
        if (!col) body += groups[g].map(rowHtml).join('');
      });
    } else body = rows.map(rowHtml).join('');
    wrap.innerHTML = '<table class="ans-table" id="ansiblePlaybooksGrid"><thead><tr><th scope="col" class="ans-pb-st"><span class="ans-sr">status</span></th>' +
      thHtml('name', T('ans_pb_col_name')) + thHtml('group', T('ans_pb_col_group'), 'ans-hide-md') + thHtml('last', T('ans_pb_col_last')) +
      '<th scope="col" class="ans-hide-md">' + esc(T('ans_pb_col_result')) + '</th><th scope="col" class="ans-hide-md">' + esc(T('ans_pb_col_schedule')) + '</th><th scope="col" class="ans-actions"></th></tr></thead><tbody>' + body + '</tbody></table>';
  }

  function renderChips(list) {
    const count = { all: list.length, failed: 0, never: 0, scheduled: 0, git: 0 };
    list.forEach(function (p) { if (p.last_status === 'failed') count.failed++; if (!p.last_status) count.never++; if (p.schedule_count > 0) count.scheduled++; if (p.source === 'git') count.git++; });
    const defs = [['all', 'ans_pb_chip_all', 'All'], ['failed', 'ans_pb_chip_failed', 'Failed'], ['never', 'ans_pb_chip_never', 'Never run'], ['scheduled', 'ans_pb_chip_scheduled', 'Scheduled'], ['git', 'ans_pb_chip_git', 'Git']];
    st.root.querySelector('[data-pb="chips"]').innerHTML = defs.map(function (d) {
      return '<button type="button" class="ans-chip" data-chip="' + d[0] + '" aria-pressed="' + (st.chip === d[0]) + '">' + esc(T(d[1], d[2])) + ' <span class="count">' + count[d[0]] + '</span></button>';
    }).join('');
    const sel = st.root.querySelector('[data-pb="group"]');
    const groups = {};
    list.forEach(function (p) { groups[(p.group_name || '').trim()] = true; });
    let opts = '<option value="">' + esc(T('ans_pb_all_groups')) + '</option>';
    Object.keys(groups).sort().forEach(function (g) { opts += '<option value="' + esc(g || '__none__') + '">' + esc(g || T('ans_pb_ungrouped')) + '</option>'; });
    sel.innerHTML = opts;
    if (st.group && !groups[st.group === '__none__' ? '' : st.group]) st.group = '';
    sel.value = st.group;
    sel.hidden = Object.keys(groups).length < 2 && !st.group;
  }

  function renderShell(panel) {
    st.root = panel;
    panel.innerHTML =
      '<div id="ansiblePlaybooksContainer" class="ans-pb">' +
      '<div class="ans-toolbar"><div class="search-box">' + icon('search', 'icon-sm text-muted') + '<input type="text" id="ansiblePlaybookSearchInput" class="form-input" placeholder="' + esc(T('ans_pb_search')) + '" aria-label="' + esc(T('ans_pb_search')) + '" autocomplete="off" /></div>' +
      '<select class="form-input" data-pb="group" aria-label="' + esc(T('ans_pb_col_group')) + '"></select>' +
      '<div class="ans-row" data-pb="chips" role="group"></div><span class="ans-spacer"></span>' +
      '<span id="ansiblePlaybookCountBadge" class="badge ans-mono"></span>' +
      '<button type="button" class="btn btn-sm ans-pb-groupby" data-act="groupby" aria-pressed="' + st.groupBy + '" title="' + esc(T('ans_pb_group_by')) + '">' + icon('layers', 'icon-sm') + '</button>' +
      '<button type="button" id="btnAnsibleImportFolder" class="btn btn-sm" data-act="import">' + icon('folder', 'icon-sm') + ' <span>' + esc(T('ans_pb_import')) + '</span></button>' +
      '<button type="button" id="btnAnsibleNewPlaybook" class="btn btn-sm btn-primary" data-act="new">' + icon('plus', 'icon-sm') + ' <span>' + esc(T('ans_pb_new')) + '</span></button></div>' +
      '<div class="ans-table-wrap" data-pb="table"></div><div id="ansiblePlaybooksEmpty" hidden></div></div>';
    st.rendered = true;
    panel.addEventListener('click', onClick);
    panel.addEventListener('keydown', onKey);
    panel.addEventListener('input', function (e) { if (e.target.id === 'ansiblePlaybookSearchInput') { st.q = e.target.value; renderRows(); } });
    panel.addEventListener('change', function (e) { if (e.target.matches('[data-pb="group"]')) { st.group = e.target.value; renderRows(); } });
    renderRows();
  }

  /* ---------- row actions, menu ---------- */
  let menuEl = null;
  function closeMenu() { if (menuEl) { menuEl.remove(); menuEl = null; document.removeEventListener('click', closeMenu, true); } }
  function openMenu(btn, id) {
    closeMenu();
    const r = btn.getBoundingClientRect();
    menuEl = document.createElement('div');
    menuEl.className = 'ans-menu'; menuEl.setAttribute('role', 'menu');
    menuEl.style.position = 'fixed'; menuEl.style.top = Math.min(r.bottom + 4, window.innerHeight - 160) + 'px'; menuEl.style.right = Math.max(8, window.innerWidth - r.right) + 'px';
    menuEl.innerHTML =
      '<button type="button" role="menuitem" data-m="edit">' + icon('pencil', 'icon-sm') + esc(T('ans_pb_menu_edit')) + '</button>' +
      '<button type="button" role="menuitem" data-m="history">' + icon('history', 'icon-sm') + esc(T('ans_pb_menu_history')) + '</button>' +
      '<button type="button" role="menuitem" data-m="schedule">' + icon('calendar', 'icon-sm') + esc(T('ans_pb_menu_schedule')) + '</button><hr />' +
      '<button type="button" role="menuitem" class="danger" data-m="delete">' + icon('trash-2', 'icon-sm') + esc(T('ans_pb_menu_delete')) + '</button>';
    menuEl.addEventListener('click', function (e) {
      const m = e.target.closest('[data-m]');
      if (!m) return;
      const act = m.getAttribute('data-m');
      closeMenu();
      if (act === 'delete') removePlaybook(id); else open(id, act === 'edit' ? 'edit' : act === 'history' ? 'runs' : 'schedule');
    });
    menuEl.addEventListener('keydown', function (e) { if (e.key === 'Escape') { closeMenu(); btn.focus(); } });
    document.body.appendChild(menuEl);
    setTimeout(function () { document.addEventListener('click', closeMenu, true); }, 0);
    const first = menuEl.querySelector('button'); if (first) first.focus();
  }

  function onClick(e) {
    const act = e.target.closest('[data-act]');
    if (act) {
      const a = act.getAttribute('data-act'), id = Number(act.getAttribute('data-id'));
      e.stopPropagation();
      if (a === 'run') open(id, 'overview', true);
      else if (a === 'menu') openMenu(act, id);
      else if (a === 'new') openNew();
      else if (a === 'import') openImport();
      else if (a === 'retry') reload();
      else if (a === 'clear') { st.q = ''; st.group = ''; st.chip = 'all'; st.root.querySelector('#ansiblePlaybookSearchInput').value = ''; renderRows(); }
      else if (a === 'groupby') { st.groupBy = !st.groupBy; Ans.store.set('pb.groupBy', st.groupBy); act.setAttribute('aria-pressed', String(st.groupBy)); renderRows(); }
      return;
    }
    const chip = e.target.closest('[data-chip]');
    if (chip) { st.chip = chip.getAttribute('data-chip'); renderRows(); return; }
    const th = e.target.closest('th[data-sort]');
    if (th) {
      const k = th.getAttribute('data-sort');
      st.sort = { key: k, dir: st.sort.key === k ? -st.sort.dir : 1 };
      Ans.store.set('pb.sort', st.sort); renderRows(); return;
    }
    const gr = e.target.closest('tr[data-group]');
    if (gr) { toggleGroup(gr.getAttribute('data-group')); return; }
    const tr = e.target.closest('tr[data-id]');
    if (tr) open(Number(tr.getAttribute('data-id')), 'overview');
  }
  function toggleGroup(k) { st.collapsed[k] = !st.collapsed[k]; Ans.store.set('pb.collapsed', st.collapsed); renderRows(); }
  function onKey(e) {
    const tr = e.target.closest('tr[data-id]'), gr = e.target.closest('tr[data-group]');
    if (gr && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); toggleGroup(gr.getAttribute('data-group')); return; }
    if (!tr || e.target !== tr) return;
    const id = Number(tr.getAttribute('data-id'));
    if (e.key === 'Enter') { e.preventDefault(); open(id, 'overview'); }
    else if (e.key === 'r' && gateOn()) { e.preventDefault(); open(id, 'overview', true); }
    else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      const n = e.key === 'ArrowDown' ? tr.nextElementSibling : tr.previousElementSibling;
      if (n && n.matches('tr[tabindex]')) n.focus();
    }
  }
  document.addEventListener('keydown', function (e) {
    if (e.key !== '/' || e.ctrlKey || e.metaKey || e.altKey || !st.rendered || !Ans.visible() || Ans.state.tab !== 'playbooks') return;
    const t = e.target;
    if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable)) return;
    if (Ans.drawer.current()) return;
    e.preventDefault();
    st.root.querySelector('#ansiblePlaybookSearchInput').focus();
  });

  /* ---------- data ---------- */
  async function reload() {
    st.loading = !all().length; st.error = '';
    renderRows();
    try { await Ans.load.playbooks(); st.loading = false; }
    catch (e) { st.loading = false; st.error = T('ans_pb_load_failed') + ': ' + e.message; }
    renderRows();
  }
  Ans.on('playbooks', function () { st.loading = false; renderRows(); });
  Ans.on('status', function () { renderRows(); });

  // poll fast while a run is in flight, otherwise refresh every 5th tick (15 s)
  let slowTick = 0;
  Ans.every(function () {
    if (Ans.state.tab !== 'playbooks') return;
    if (!all().some(isActive) && ++slowTick % 5 !== 0) return;
    Ans.load.playbooks().catch(function () { /* next tick */ });
  }, 3000);

  async function removePlaybook(id) {
    const p = byId(id);
    if (!p) return;
    const ok = await Ans.confirm({ title: T('ans_pb_delete_title'), message: T('ans_pb_delete_msg', 'Delete "{name}"?', { name: p.name }), confirmText: T('ans_pb_menu_delete'), danger: true });
    if (!ok) return;
    try {
      await Ans.api.del('/api/ansible/playbooks/' + id);
      Ans.toast(T('ans_pb_deleted'), 'success');
      const cur = Ans.drawer.current();
      if (cur && cur.key === 'playbook:' + id) { cur.forceClose = true; await Ans.drawer.close(); }
      await Ans.load.playbooks(); Ans.load.schedules().catch(function () { /* optional */ });
    } catch (e) {
      Ans.toast(e.status === 409 ? T('ans_pb_delete_busy') : T('ans_pb_delete_failed') + ': ' + e.message, 'error');
    }
  }

  /* ---------- drawer ---------- */
  function subtitleHtml(p) {
    if (!p) return '';
    return Ans.ui.badge(statusOf(p)) +
      (p.group_name ? '<span class="badge badge-unknown">' + icon('layers', 'icon-sm') + esc(p.group_name) + '</span>' : '') +
      '<span class="ans-muted">' + esc(p.source === 'git' ? T('ans_pb_git', 'git') : T('ans_pb_inline', 'inline')) + '</span>';
  }

  function playbookView(id, subtab, focusLaunch) {
    const isNew = id === null;
    const tabs = isNew ? [['edit', 'ans_pb_tab_new', 'New playbook', 'pencil']] :
      [['overview', 'ans_pb_tab_overview', 'Overview', 'activity'], ['edit', 'ans_pb_tab_edit', 'Edit', 'pencil'], ['runs', 'ans_pb_tab_runs', 'Runs', 'history'], ['schedule', 'ans_pb_tab_schedule', 'Schedule', 'calendar']];
    const ctx = { id: id, tab: null, handle: null, dirty: false, seq: 0, body: null, pane: null, ctrl: null, off: [] };

    function destroyHandle() {
      ctx.seq++;
      if (ctx.handle && ctx.handle.destroy) { try { ctx.handle.destroy(); } catch (e) { console.error(e); } }
      ctx.handle = null;
    }
    async function confirmDiscard() {
      if (!(ctx.handle && ctx.handle.isDirty && ctx.handle.isDirty())) return true;
      return Ans.confirm({ title: T('ans_pb_discard_title'), message: T('ans_pb_discard_msg'), confirmText: T('ans_pb_discard'), danger: true });
    }
    function paintTabs() {
      ctx.body.querySelectorAll('.ans-drawer-tabs button').forEach(function (b) {
        const on = b.getAttribute('data-tab') === ctx.tab;
        b.setAttribute('aria-selected', String(on)); b.tabIndex = on ? 0 : -1;
        b.querySelector('.ans-dirty').hidden = !(b.getAttribute('data-tab') === 'edit' && ctx.dirty);
      });
    }
    async function select(tab, opts) {
      if (ctx.tab === tab) return;
      if (ctx.tab && !ctx.skipDiscard && !(await confirmDiscard())) return;
      ctx.skipDiscard = false;
      destroyHandle();
      ctx.dirty = false; ctx.tab = tab;
      const pane = ctx.pane;
      pane.innerHTML = '';
      pane.className = 'ans-drawer-pane' + (tab === 'edit' ? ' ans-pane-flush' : '');
      ctx.ctrl.setWide(tab === 'edit');
      paintTabs();
      const seq = ctx.seq;
      try {
        if (tab === 'overview') await mountOverview(pane, id, ctx, seq, opts);
        else if (tab === 'edit') await mountEdit(pane, id, ctx, seq);
        else if (tab === 'runs') mountRuns(pane, id, ctx);
        else if (tab === 'schedule') mountSchedule(pane, id, ctx);
      } catch (e) {
        if (seq === ctx.seq) pane.innerHTML = Ans.ui.error(T('ans_pb_loading_failed') + ': ' + e.message);
      }
    }
    ctx.select = select;
    ctx.destroyHandle = destroyHandle;

    const pb0 = isNew ? null : byId(id);
    const view = {
      key: 'playbook:' + (isNew ? 'new' : id),
      title: isNew ? T('ans_pb_tab_new') : (pb0 ? pb0.name : '#' + id),
      subtitle: subtitleHtml(pb0),
      wide: false,
      actions: isNew ? '' : '<button type="button" class="btn btn-sm btn-primary" data-pbd="run"' + (gateOn() ? '' : ' disabled title="' + esc(T('ans_pb_run_off')) + '"') + '>' + icon('play', 'icon-sm') + ' ' + esc(T('ans_pb_run')) + '</button>' +
        (window.IacScan ? window.IacScan.btnHtml('ansible', id) : ''),
      mount: function (body, ctrl) {
        ctx.body = body; ctx.ctrl = ctrl;
        body.innerHTML = '<div class="ans-drawer-tabs" role="tablist">' + tabs.map(function (t) {
          return '<button type="button" role="tab" data-tab="' + t[0] + '" aria-selected="false" tabindex="-1">' + icon(t[3], 'icon-sm') + '<span>' + esc(T(t[1], t[2])) + '</span><i class="ans-dirty" hidden title="unsaved"></i></button>';
        }).join('') + '</div><div class="ans-drawer-pane"></div>';
        ctx.pane = body.querySelector('.ans-drawer-pane');
        body.querySelector('.ans-drawer-tabs').addEventListener('click', function (e) { const b = e.target.closest('[data-tab]'); if (b) select(b.getAttribute('data-tab')); });
        body.querySelector('.ans-drawer-tabs').addEventListener('keydown', function (e) {
          const btns = Array.prototype.slice.call(body.querySelectorAll('.ans-drawer-tabs button'));
          let i = btns.indexOf(document.activeElement);
          if (i < 0) return;
          if (e.key === 'ArrowRight') i = (i + 1) % btns.length; else if (e.key === 'ArrowLeft') i = (i - 1 + btns.length) % btns.length; else return;
          e.preventDefault(); btns[i].focus(); select(btns[i].getAttribute('data-tab'));
        });
        const head = document.querySelector('#ansDrawerRoot .ans-drawer-actions');
        if (head) head.onclick = function (e) { if (e.target.closest('[data-pbd="run"]')) { if (ctx.tab !== 'overview') select('overview', { focusLaunch: true }); else focusLaunchButton(ctx.pane); } };
        const onPb = Ans.on('playbooks', function () {
          const p = byId(id);
          if (p && ctx.ctrl) { ctx.ctrl.setTitle(p.name); ctx.ctrl.setSubtitle(subtitleHtml(p)); }
        });
        ctx.off.push(function () { Ans.off('playbooks', onPb); });
        select(isNew ? 'edit' : (subtab || 'overview'), { focusLaunch: !!focusLaunch });
      },
      unmount: function () { destroyHandle(); ctx.off.forEach(function (f) { f(); }); ctx.off = []; const head = document.querySelector('#ansDrawerRoot .ans-drawer-actions'); if (head) head.onclick = null; },
      canClose: function () { return view.forceClose ? true : confirmDiscard(); },
    };
    return view;
  }

  function focusLaunchButton(pane) {
    const b = pane.querySelector('#btnAnsibleRunLaunchSubmit, [data-launch="submit"]');
    if (b) { b.scrollIntoView({ block: 'center' }); b.focus(); }
  }

  async function mountOverview(pane, id, ctx, seq, opts) {
    pane.innerHTML =
      '<div class="ans-card" data-ov="launch"></div>' +
      '<div class="ans-card" data-ov="last"><div class="ans-card-title">' + icon('activity', 'icon-sm') + esc(T('ans_pb_card_last')) + '</div><div data-ov-body>' + Ans.ui.skeleton(2) + '</div></div>' +
      '<div class="ans-card" data-ov="timeline"><div class="ans-card-title">' + icon('history', 'icon-sm') + esc(T('ans_pb_card_timeline')) + '</div><div data-ov-body></div></div>' +
      '<div class="ans-card" data-ov="sched"><div class="ans-card-title">' + icon('calendar', 'icon-sm') + esc(T('ans_pb_card_schedule')) + '</div><div data-ov-body></div></div>';
    const pb = byId(id) || (await Ans.api.get('/api/ansible/playbooks/' + id)).playbook;
    const launchBox = pane.querySelector('[data-ov="launch"]');
    if (Ans.launch && Ans.launch.mount) {
      ctx.handle = Ans.launch.mount(launchBox, pb, { onLaunched: function () { Ans.load.playbooks().catch(function () { /* list refreshes by polling */ }); } });
      if (opts && opts.focusLaunch) setTimeout(function () { focusLaunchButton(pane); }, 60);
    } else launchBox.innerHTML = '<span class="ans-muted">' + esc(T('ans_pb_launch_missing')) + '</span>';

    const bodyOf = function (k) { return pane.querySelector('[data-ov="' + k + '"] [data-ov-body]'); };
    // runs feed the last-run card and the timeline
    Ans.api.get('/api/ansible/playbooks/' + id + '/runs?limit=10').then(async function (d) {
      if (seq !== ctx.seq) return;
      const runs = (d && d.runs) || [];
      renderTimeline(bodyOf('timeline'), runs, id);
      await renderLast(bodyOf('last'), runs[0]);
    }).catch(function (e) {
      if (seq !== ctx.seq) return;
      bodyOf('last').innerHTML = Ans.ui.error(e.message); bodyOf('timeline').innerHTML = '';
    });
    Ans.load.schedules(id).then(function (list) { if (seq === ctx.seq) renderSchedSummary(bodyOf('sched'), list, ctx); }).catch(function (e) { if (seq === ctx.seq) bodyOf('sched').innerHTML = Ans.ui.error(e.message); });
  }

  async function renderLast(box, run) {
    if (!run) { box.innerHTML = '<span class="ans-muted">' + esc(T('ans_pb_no_runs')) + '</span>'; return; }
    const dur = run.started_at && run.finished_at ? Ans.fmt.dur(Date.parse(run.finished_at) - Date.parse(run.started_at)) : '-';
    let targets = '';
    try { const t = JSON.parse(run.targets || '[]'); targets = t.length === 1 ? T('ans_pb_target_one') : T('ans_pb_targets', '{n} hosts', { n: t.length }); } catch (e) { targets = ''; }
    box.innerHTML = '<div class="ans-row">' + Ans.ui.badge(run.status) + '<span class="ans-mono">#' + run.id + '</span><span class="badge badge-unknown">' + esc(run.mode) + '</span><span title="' + esc(Ans.fmt.dt(run.created_at)) + '">' + esc(Ans.fmt.ago(run.created_at)) + '</span><span class="ans-muted">' + esc(dur) + (targets ? ' · ' + esc(targets) : '') + '</span><span class="ans-spacer"></span>' +
      (Ans.runs && Ans.runs.openViewer ? '<button type="button" class="btn btn-sm" data-ov-run="' + run.id + '">' + icon('scroll-text', 'icon-sm') + ' ' + esc(T('ans_pb_view_run')) + '</button>' : '') + '</div><div data-recap></div>';
    box.querySelectorAll('[data-ov-run]').forEach(function (b) { b.addEventListener('click', function () { Ans.runs.openViewer(Number(b.getAttribute('data-ov-run')), { playbookId: run.playbook_id }); }); });
    if (run.status === 'queued' || run.status === 'running') return;
    try {
      const d = await Ans.api.get('/api/ansible/runs/' + run.id);
      const recap = (d && d.recap) || {};
      const hosts = Object.keys(recap);
      if (!hosts.length) return;
      hosts.sort(function (a, b) { const ba = (recap[a].failed || 0) + (recap[a].unreachable || 0), bb = (recap[b].failed || 0) + (recap[b].unreachable || 0); return bb - ba || a.localeCompare(b); });
      const c = function (n, cls) { return '<td class="ans-pb-num' + (n && cls ? ' ' + cls : '') + '">' + (n || 0) + '</td>'; };
      box.querySelector('[data-recap]').innerHTML = '<div class="ans-table-wrap ans-pb-recap"><table class="ans-table"><thead><tr><th scope="col">' + esc(T('ans_pb_host')) + '</th><th scope="col">' + esc(T('ans_pb_ok')) + '</th><th scope="col">' + esc(T('ans_pb_changed')) + '</th><th scope="col">' + esc(T('ans_pb_failed')) + '</th><th scope="col">' + esc(T('ans_pb_unreachable')) + '</th><th scope="col">' + esc(T('ans_pb_skipped')) + '</th></tr></thead><tbody>' +
        hosts.map(function (h) { const r = recap[h]; return '<tr><td class="ans-mono">' + esc(h) + '</td>' + c(r.ok, 'ok') + c(r.changed, 'changed') + c(r.failed, 'bad') + c(r.unreachable, 'bad') + c(r.skipped) + '</tr>'; }).join('') + '</tbody></table></div>';
    } catch (e) { /* the recap table is optional */ }
  }

  function renderTimeline(box, runs, playbookId) {
    if (!runs.length) { box.innerHTML = '<span class="ans-muted">' + esc(T('ans_pb_no_runs')) + '</span>'; return; }
    box.innerHTML = '<div class="ans-pb-tl">' + runs.slice().reverse().map(function (r) {
      const title = T('ans_pb_pip_title', 'Run #{id} · {status} · {when}', { id: r.id, status: Ans.ui.statusLabel(r.status), when: Ans.fmt.ago(r.created_at) });
      return '<button type="button" class="ans-pb-pip ans-pb-pip-' + esc(r.status) + '" data-run="' + r.id + '" title="' + esc(title) + '" aria-label="' + esc(title) + '"></button>';
    }).join('') + '</div>';
    box.querySelector('.ans-pb-tl').addEventListener('click', function (e) {
      const b = e.target.closest('[data-run]');
      if (b && Ans.runs && Ans.runs.openViewer) Ans.runs.openViewer(Number(b.getAttribute('data-run')), { playbookId: playbookId });
    });
  }

  function renderSchedSummary(box, list, ctx) {
    if (!list.length) box.innerHTML = '<div class="ans-muted">' + esc(T('ans_pb_no_schedules')) + '</div>';
    else {
      const enabled = list.filter(function (s) { return s.enabled && s.next_run_at; }).sort(function (a, b) { return Date.parse(a.next_run_at) - Date.parse(b.next_run_at); });
      box.innerHTML = '<div class="ans-row"><span>' + icon('calendar', 'icon-sm') + ' ' + esc(list.length === 1 ? T('ans_pb_sched_count_one') : T('ans_pb_sched_count', '{n} schedules', { n: list.length })) + '</span>' +
        (enabled.length ? '<span class="ans-muted" title="' + esc(Ans.fmt.dt(enabled[0].next_run_at)) + '">' + esc(T('ans_pb_next_run', 'Next run {when}', { when: Ans.fmt.ago(enabled[0].next_run_at) })) + '</span>' : '') + '</div>';
    }
    box.insertAdjacentHTML('beforeend', '<div style="margin-top:8px"><button type="button" class="btn btn-sm" data-ov-sched>' + esc(T('ans_pb_manage_sched')) + '</button></div>');
    box.querySelector('[data-ov-sched]').addEventListener('click', function () { ctx.select('schedule'); });
  }

  let pendingEditSub = ''; // one-shot sub-tab hand-off: first save of a new playbook reopens its drawer on Edit
  async function mountEdit(pane, id, ctx, seq) {
    const sub = pendingEditSub; pendingEditSub = '';
    if (!(Ans.editor && Ans.editor.mount)) { pane.innerHTML = '<span class="ans-muted">' + esc(T('ans_pb_editor_missing')) + '</span>'; return; }
    let full = null;
    if (id !== null) {
      pane.innerHTML = Ans.ui.skeleton(5);
      full = (await Ans.api.get('/api/ansible/playbooks/' + id)).playbook;
      if (seq !== ctx.seq) return;
      pane.innerHTML = '';
    }
    ctx.handle = Ans.editor.mount(pane, {
      playbook: full,
      sub: sub || undefined,
      onSaved: async function (pb, meta) {
        await Ans.load.playbooks().catch(function () { /* list refreshes by polling */ });
        ctx.dirty = false;
        if (id === null && pb && pb.id) {
          // First save: convert the new-playbook drawer into this playbook's, staying on Edit.
          pendingEditSub = (meta && meta.sub) || '';
          ctx.destroyHandle();
          await Ans.drawer.open(playbookView(pb.id, 'edit'));
        } else if (ctx.ctrl) {
          const p = byId(id);
          if (p) { ctx.ctrl.setTitle(p.name); ctx.ctrl.setSubtitle(subtitleHtml(p)); }
        }
      },
      onDirty: function (d) { ctx.dirty = !!d; if (ctx.body) ctx.body.querySelectorAll('.ans-drawer-tabs [data-tab="edit"] .ans-dirty').forEach(function (i) { i.hidden = !d; }); },
      onClose: function () { if (id === null) Ans.drawer.close(); else ctx.select('overview'); },
    });
  }
  function mountRuns(pane, id, ctx) {
    if (Ans.runs && Ans.runs.mountHistory) ctx.handle = Ans.runs.mountHistory(pane, { playbookId: id, limit: 100 });
    else pane.innerHTML = '<span class="ans-muted">' + esc(T('ans_pb_runs_missing')) + '</span>';
  }
  function mountSchedule(pane, id, ctx) {
    if (Ans.schedules && Ans.schedules.mount) ctx.handle = Ans.schedules.mount(pane, { playbookId: id });
    else pane.innerHTML = '<span class="ans-muted">' + esc(T('ans_pb_sched_missing')) + '</span>';
  }

  function open(id, subtab, focusLaunch) {
    id = Number(id);
    if (!id) return Promise.resolve(false);
    return Ans.drawer.open(playbookView(id, subtab || 'overview', focusLaunch));
  }
  function openNew() { return Ans.drawer.open(playbookView(null)); }

  /* ---------- import folder dialog ---------- */
  const imp = { files: [], candidates: [], busy: false };
  const isFolderPath = function (p) { return !!p && !/[\\":\x00-\x1f\x7f-\x9f]/.test(p) && !p.split('/').some(function (x) { return !x || x === '.' || x === '..'; }); };
  const isCandidate = function (p) {
    const excluded = ['roles', 'group_vars', 'host_vars', 'files', 'templates', '.ansible', 'collections', 'inventory', 'inventories', 'docs', '.github'];
    return isFolderPath(p) && /\.ya?ml$/i.test(p) && !p.split('/').slice(0, -1).some(function (x) { return excluded.indexOf(x.toLowerCase()) >= 0; });
  };
  function prepareFiles(selection) {
    const selected = Array.from(selection);
    const first = selected[0] && selected[0].webkitRelativePath;
    const root = first ? first.split('/')[0] : '';
    const strip = root && selected.every(function (f) { return (f.webkitRelativePath || '').indexOf(root + '/') === 0; });
    const files = [], seen = new Set();
    let size = 0;
    for (const file of selected) {
      const raw = file.webkitRelativePath || file.name;
      if (!isFolderPath(raw)) throw new Error(T('ans_pb_imp_err_path'));
      const path = strip ? raw.slice(root.length + 1) : raw;
      const parts = path.split('/');
      if (parts.some(function (x) { return ['.git', '__pycache__', '.DS_Store'].indexOf(x) >= 0; })) continue;
      if (parts[0] === '.ansible' || parts[0] === 'collections') continue;
      if (!isFolderPath(path) || seen.has(path)) throw new Error(T('ans_pb_imp_err_path'));
      seen.add(path); files.push({ file: file, path: path }); size += file.size;
    }
    return { files: files, size: size, candidates: files.map(function (f) { return f.path; }).filter(isCandidate).sort() };
  }

  function openImport() {
    if (imp.busy) return;
    imp.files = []; imp.candidates = [];
    const old = document.getElementById('ansibleImportModal'); if (old) old.remove();
    const m = document.createElement('div');
    m.id = 'ansibleImportModal'; m.className = 'modal';
    m.innerHTML =
      '<div class="modal-content modal-dialog-lg" style="display:flex;flex-direction:column;max-height:90vh;max-width:800px;"><div class="modal-header"><h3 style="font-size:var(--fs-md);margin:0;">' + esc(T('ans_pb_imp_title')) + '</h3>' +
      '<button type="button" class="btn btn-icon btn-sm btn-ghost" data-imp="close" aria-label="' + esc(T('ans_close')) + '">' + icon('x', 'icon-sm') + '</button></div>' +
      '<div class="modal-body" style="display:flex;flex-direction:column;gap:12px;overflow-y:auto;"><div role="note" class="ans-pb-note">' + esc(T('ans_pb_imp_warning')) + '</div>' +
      '<fieldset id="ansibleImportFields" class="ans-pb-fs"><div class="ans-field"><label for="ansibleImportFiles">' + esc(T('ans_pb_imp_folder')) + '</label>' +
      '<input type="file" id="ansibleImportFiles" class="form-input" webkitdirectory multiple aria-describedby="ansibleImportHint ansibleImportPreview" />' +
      '<div id="ansibleImportHint" class="hint">' + esc(T('ans_pb_imp_hint')) + '</div><div id="ansibleImportPreview" class="hint" role="status" aria-live="polite"></div></div>' +
      '<div id="ansibleImportTableSection" hidden><div class="ans-row"><label class="ans-row" style="cursor:pointer;font-size:var(--fs-xs);"><input type="checkbox" id="ansibleImportSelectAll" /> <span>' + esc(T('ans_pb_imp_select_all')) + '</span></label><span class="ans-spacer"></span><span id="ansibleImportSelectedCount" class="ans-muted ans-mono"></span></div>' +
      '<div id="ansibleImportFallbackHint" class="hint" style="color:var(--warning);" hidden>' + esc(T('ans_pb_imp_fallback')) + '</div>' +
      '<div class="ans-table-wrap" style="max-height:36vh;"><table class="ans-table"><thead><tr><th scope="col" style="width:36px;"></th><th scope="col">' + esc(T('ans_pb_imp_col_path')) + '</th><th scope="col">' + esc(T('ans_pb_imp_col_name')) + '</th></tr></thead><tbody id="ansibleImportTableBody"></tbody></table></div></div></fieldset>' +
      '<div id="ansibleImportError" class="ans-error" role="alert" hidden></div></div>' +
      '<div class="modal-footer" style="display:flex;gap:8px;justify-content:flex-end;"><button type="button" class="btn btn-sm" data-imp="close">' + esc(T('ans_pb_imp_cancel')) + '</button>' +
      '<button type="button" id="btnAnsibleImportSubmit" class="btn btn-sm btn-primary" data-imp="submit">' + esc(T('ans_pb_import')) + '</button></div></div>';
    document.body.appendChild(m);
    const q = function (s) { return m.querySelector(s); };
    const err = function (msg) { const e = q('#ansibleImportError'); e.hidden = !msg; e.textContent = msg || ''; };
    const close = function () { if (imp.busy) return; closeModal('ansibleImportModal'); setTimeout(function () { m.remove(); }, 200); };
    const count = function () {
      const chks = Array.from(m.querySelectorAll('.ansible-import-item-chk')), on = chks.filter(function (c) { return c.checked; });
      q('#ansibleImportSelectedCount').textContent = T('ans_pb_imp_selected', '{n} of {m} selected', { n: on.length, m: chks.length });
      q('#ansibleImportSelectAll').checked = chks.length > 0 && on.length === chks.length;
    };
    q('#ansibleImportFiles').addEventListener('change', async function () {
      imp.files = []; imp.candidates = []; err(''); q('#ansibleImportPreview').textContent = ''; q('#ansibleImportTableSection').hidden = true; q('#ansibleImportTableBody').innerHTML = '';
      const input = q('#ansibleImportFiles');
      if (!input.files || !input.files.length) return;
      let prepared;
      try { prepared = prepareFiles(input.files); } catch (e) { err(e.message); return; }
      q('#ansibleImportPreview').textContent = T('ans_pb_imp_preview', '{count} files · {size}', { count: prepared.files.length, size: Ans.fmt.bytes(prepared.size) });
      if (!prepared.files.length) { err(T('ans_pb_imp_err_files')); return; }
      if (!prepared.candidates.length) { err(T('ans_pb_imp_err_yaml')); return; }
      imp.files = prepared.files;
      const marker = /^\s*-?\s*(hosts|import_playbook)\s*:/m;
      let any = false;
      const data = [];
      for (const path of prepared.candidates) {
        const item = prepared.files.find(function (f) { return f.path === path; });
        let detected = false;
        if (item && item.file.size <= 256 * 1024 && item.file.text) { try { detected = marker.test(await item.file.text()); } catch (e) { /* unreadable file */ } }
        any = any || detected;
        data.push({ path: path, detected: detected, name: path.split('/').pop().replace(/\.ya?ml$/i, '') });
      }
      imp.candidates = data;
      q('#ansibleImportTableSection').hidden = false;
      q('#ansibleImportFallbackHint').hidden = any;
      q('#ansibleImportTableBody').innerHTML = data.map(function (c, i) {
        return '<tr><td style="text-align:center;"><input type="checkbox" class="ansible-import-item-chk" data-idx="' + i + '"' + ((any ? c.detected : false) ? ' checked' : '') + ' aria-label="' + esc(c.path) + '" /></td><td class="ans-mono" style="word-break:break-all;">' + esc(c.path) + '</td>' +
          '<td><input type="text" class="form-input ansible-import-item-name" data-idx="' + i + '" value="' + esc(c.name) + '" style="width:100%;height:28px;font-size:var(--fs-xs);" /></td></tr>';
      }).join('');
      count();
    });
    q('#ansibleImportSelectAll').addEventListener('change', function (e) { m.querySelectorAll('.ansible-import-item-chk').forEach(function (c) { c.checked = e.target.checked; }); count(); });
    m.addEventListener('change', function (e) { if (e.target.classList.contains('ansible-import-item-chk')) count(); });
    m.addEventListener('click', async function (e) {
      const b = e.target.closest('[data-imp]');
      if (!b) { if (e.target === m) close(); return; }
      if (b.getAttribute('data-imp') === 'close') { close(); return; }
      if (imp.busy) return;
      if (!imp.files.length) { err(T('ans_pb_imp_err_files')); return; }
      const chks = Array.from(m.querySelectorAll('.ansible-import-item-chk')), names = Array.from(m.querySelectorAll('.ansible-import-item-name'));
      const picked = [];
      chks.forEach(function (c, i) { if (c.checked) picked.push({ name: (names[i].value || '').trim(), path: imp.candidates[i].path }); });
      if (!picked.length) { err(T('ans_pb_imp_err_none')); return; }
      const existing = new Set(all().map(function (p) { return (p.name || '').toLowerCase(); })), batch = new Set();
      for (const p of picked) {
        if (!p.name) { err(T('ans_pb_imp_err_name')); return; }
        const l = p.name.toLowerCase();
        if (batch.has(l)) { err(T('ans_pb_imp_err_dup', 'Duplicate playbook name: "{name}"', { name: p.name })); return; }
        batch.add(l);
        if (existing.has(l)) { err(T('ans_pb_imp_err_taken', 'Playbook name already exists: "{name}"', { name: p.name })); return; }
      }
      err(''); imp.busy = true; q('#ansibleImportFields').disabled = true;
      const restore = SP.busy(b, T('ans_pb_imp_busy'));
      try {
        const payload = [];
        for (const p of picked) {
          const it = imp.files.find(function (f) { return f.path === p.path; });
          if (!it) throw new Error(T('ans_pb_imp_err_files'));
          if (it.file.size > 256 * 1024) throw new Error(T('ans_pb_imp_err_big'));
          payload.push({ name: p.name, description: '', extra_vars: '', content: await it.file.text() });
        }
        await Ans.api.post('/api/ansible/playbooks/import', { playbooks: payload });
        imp.busy = false; close();
        Ans.toast(T('ans_pb_imp_done'), 'success');
        Ans.load.playbooks().catch(function () { /* polling catches up */ });
      } catch (ex) { err(ex.message || T('ans_pb_imp_failed')); }
      finally { imp.busy = false; q('#ansibleImportFields').disabled = false; restore(); }
    });
    openModal('ansibleImportModal');
  }

  /* ---------- public api + tab ---------- */
  Ans.playbooks = { open: open, openNew: openNew, reload: reload };
  Ans.registerTab({
    id: 'playbooks', icon: 'ansible', labelKey: 'ans_tab_playbooks', order: 10,
    render: function (panel) { renderShell(panel); reload(); },
    onShow: function () { if (st.rendered) reload(); },
  });
})();
