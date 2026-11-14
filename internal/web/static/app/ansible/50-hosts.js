/* Hosts tab: Explorer (Group > Host, reachability, Ping, Facts) and Ad-hoc runner. */
(function () {
  'use strict';
  const Ans = window.Ans;
  const esc = Ans.esc, T = Ans.t, icon = Ans.icon;

  Ans.addI18n({
    en: {
      ans_hs_sub_explorer: 'Explorer', ans_hs_sub_adhoc: 'Ad-hoc', ans_hs_search: 'Search hosts…', ans_hs_ping_sel: 'Ping selected', ans_hs_ping_all: 'Ping all', ans_hs_selected: '{n} selected',
      ans_hs_ungrouped: 'Ungrouped', ans_hs_hosts: '{n} hosts', ans_hs_host_one: '1 host', ans_hs_load_failed: 'Could not load the hosts', ans_hs_empty_title: 'No hosts yet', ans_hs_empty_desc: 'Add hosts in Pantau first; the Ansible inventory is generated from them.',
      ans_hs_no_match: 'No hosts match', ans_hs_pantau: 'Pantau', ans_hs_ansible: 'Ansible', ans_hs_st_healthy: 'Healthy', ans_hs_st_degraded: 'Degraded', ans_hs_st_down: 'Down', ans_hs_st_unknown: 'Unknown',
      ans_hs_a_online: 'Reachable', ans_hs_a_offline: 'Unreachable', ans_hs_a_unknown: 'Not checked', ans_hs_pinging: 'Pinging…', ans_hs_ping: 'Ping', ans_hs_facts: 'Facts', ans_hs_facts_age: 'facts {when}', ans_hs_no_facts: 'no facts',
      ans_hs_ping_done: 'Ping finished: {ok} reachable, {bad} not', ans_hs_ping_failed: 'Ping failed', ans_hs_ping_busy: 'Too many ad-hoc runs are active; try again shortly.', ans_hs_alias: 'Inventory alias', ans_hs_ping_timeout: 'Ping is still running after 2 minutes; check the Ad-hoc tab for its progress.',
      ans_hs_f_title: 'Facts: {name}', ans_hs_f_refresh: 'Refresh facts', ans_hs_f_refreshing: 'Gathering… (up to 90 s)', ans_hs_f_gathered: 'Gathered {when}', ans_hs_f_none: 'No facts have been gathered for this host yet.',
      ans_hs_f_gather: 'Gather facts', ans_hs_f_failed: 'Could not gather facts', ans_hs_f_load_failed: 'Could not load facts', ans_hs_f_summary: 'Summary', ans_hs_f_raw: 'Raw JSON',
      ans_hs_f_system: 'System', ans_hs_f_compute: 'Compute', ans_hs_f_network: 'Network', ans_hs_f_storage: 'Storage', ans_hs_f_os: 'OS', ans_hs_f_kernel: 'Kernel', ans_hs_f_arch: 'Architecture',
      ans_hs_f_hostname: 'Hostname', ans_hs_f_virt: 'Virtualization', ans_hs_f_python: 'Python', ans_hs_f_uptime: 'Uptime', ans_hs_f_cpu: 'CPU', ans_hs_f_cores: 'Cores', ans_hs_f_mem: 'Memory', ans_hs_f_swap: 'Swap',
      ans_hs_f_ip: 'Default IPv4', ans_hs_f_ifaces: 'Interfaces', ans_hs_f_mount: 'Mount', ans_hs_f_fs: 'Type', ans_hs_f_usage: 'Usage', ans_hs_f_filter: 'Filter raw JSON…', ans_hs_f_copy: 'Copy', ans_hs_f_copied: 'Facts copied', ans_hs_f_nolines: 'No lines match',
      ans_hs_ad_module: 'Module', ans_hs_ad_module_ph: 'e.g. ping, command, shell', ans_hs_ad_args: 'Arguments', ans_hs_ad_args_ph: 'e.g. uptime', ans_hs_ad_pattern: 'Host pattern', ans_hs_ad_pattern_ph: 'all', ans_hs_ad_targets: 'Targets',
      ans_hs_ad_group: 'Host group (optional)', ans_hs_ad_group_none: 'None', ans_hs_ad_advanced: 'Advanced', ans_hs_ad_become: 'Become (sudo without password)', ans_hs_ad_forks: 'Forks (0 = default)', ans_hs_ad_verbosity: 'Verbosity', ans_hs_ad_extra: 'Extra vars (JSON object)',
      ans_hs_ad_warn: 'shell, command and raw run on the target hosts with the SSH identity Pantau uses (equivalent to a terminal session).', ans_hs_ad_run: 'Run', ans_hs_ad_running: 'Starting…',
      ans_hs_ad_err_module: 'The module must match [A-Za-z0-9_.]{1,80}.', ans_hs_ad_err_targets: 'Select at least one host or a host group.', ans_hs_ad_err_forks: 'Forks must be between 0 and 20.', ans_hs_ad_err_extra: 'Extra vars must be a valid JSON object.',
      ans_hs_ad_failed: 'Could not start the run', ans_hs_ad_limit: 'Too many ad-hoc runs are active (max 3); try again shortly.', ans_hs_ad_cancel: 'Cancel', ans_hs_ad_cancel_title: 'Cancel this ad-hoc run?', ans_hs_ad_cancelled: 'Cancel requested',
      ans_hs_ad_results: 'Results', ans_hs_ad_log: 'Output', ans_hs_ad_history: 'Recent ad-hoc runs', ans_hs_ad_no_history: 'No ad-hoc runs yet', ans_hs_ad_no_results: 'No per-host results yet.',
      ans_hs_r_success: 'success', ans_hs_r_changed: 'changed', ans_hs_r_unreachable: 'unreachable', ans_hs_r_failed: 'failed', ans_hs_ad_host: 'Host', ans_hs_ad_status: 'Status', ans_hs_ad_summary: 'Summary',
    },
    id: {
      ans_hs_sub_explorer: 'Penjelajah', ans_hs_sub_adhoc: 'Ad-hoc', ans_hs_search: 'Cari host…', ans_hs_ping_sel: 'Ping terpilih', ans_hs_ping_all: 'Ping semua', ans_hs_selected: '{n} dipilih',
      ans_hs_ungrouped: 'Tanpa grup', ans_hs_hosts: '{n} host', ans_hs_host_one: '1 host', ans_hs_load_failed: 'Gagal memuat host', ans_hs_empty_title: 'Belum ada host', ans_hs_empty_desc: 'Tambahkan host di Pantau dulu; inventory Ansible dibuat dari host tersebut.',
      ans_hs_no_match: 'Tidak ada host yang cocok', ans_hs_pantau: 'Pantau', ans_hs_ansible: 'Ansible', ans_hs_st_healthy: 'Sehat', ans_hs_st_degraded: 'Menurun', ans_hs_st_down: 'Mati', ans_hs_st_unknown: 'Tidak diketahui',
      ans_hs_a_online: 'Terjangkau', ans_hs_a_offline: 'Tak terjangkau', ans_hs_a_unknown: 'Belum dicek', ans_hs_pinging: 'Mem-ping…', ans_hs_ping: 'Ping', ans_hs_facts: 'Facts', ans_hs_facts_age: 'facts {when}', ans_hs_no_facts: 'belum ada facts',
      ans_hs_ping_done: 'Ping selesai: {ok} terjangkau, {bad} tidak', ans_hs_ping_failed: 'Ping gagal', ans_hs_ping_busy: 'Terlalu banyak run ad-hoc aktif; coba lagi sebentar.', ans_hs_alias: 'Alias inventory', ans_hs_ping_timeout: 'Ping masih berjalan setelah 2 menit; periksa tab Ad-hoc untuk progresnya.',
      ans_hs_f_title: 'Facts: {name}', ans_hs_f_refresh: 'Segarkan facts', ans_hs_f_refreshing: 'Mengumpulkan… (maks 90 dtk)', ans_hs_f_gathered: 'Dikumpulkan {when}', ans_hs_f_none: 'Facts belum pernah dikumpulkan untuk host ini.',
      ans_hs_f_gather: 'Kumpulkan facts', ans_hs_f_failed: 'Gagal mengumpulkan facts', ans_hs_f_load_failed: 'Gagal memuat facts', ans_hs_f_summary: 'Ringkasan', ans_hs_f_raw: 'JSON mentah',
      ans_hs_f_system: 'Sistem', ans_hs_f_compute: 'Komputasi', ans_hs_f_network: 'Jaringan', ans_hs_f_storage: 'Penyimpanan', ans_hs_f_os: 'OS', ans_hs_f_kernel: 'Kernel', ans_hs_f_arch: 'Arsitektur',
      ans_hs_f_hostname: 'Nama host', ans_hs_f_virt: 'Virtualisasi', ans_hs_f_python: 'Python', ans_hs_f_uptime: 'Uptime', ans_hs_f_cpu: 'CPU', ans_hs_f_cores: 'Core', ans_hs_f_mem: 'Memori', ans_hs_f_swap: 'Swap',
      ans_hs_f_ip: 'IPv4 bawaan', ans_hs_f_ifaces: 'Antarmuka', ans_hs_f_mount: 'Mount', ans_hs_f_fs: 'Tipe', ans_hs_f_usage: 'Pemakaian', ans_hs_f_filter: 'Filter JSON mentah…', ans_hs_f_copy: 'Salin', ans_hs_f_copied: 'Facts disalin', ans_hs_f_nolines: 'Tidak ada baris yang cocok',
      ans_hs_ad_module: 'Modul', ans_hs_ad_module_ph: 'mis. ping, command, shell', ans_hs_ad_args: 'Argumen', ans_hs_ad_args_ph: 'mis. uptime', ans_hs_ad_pattern: 'Pola host', ans_hs_ad_pattern_ph: 'all', ans_hs_ad_targets: 'Target',
      ans_hs_ad_group: 'Grup host (opsional)', ans_hs_ad_group_none: 'Tidak ada', ans_hs_ad_advanced: 'Lanjutan', ans_hs_ad_become: 'Become (sudo tanpa password)', ans_hs_ad_forks: 'Forks (0 = bawaan)', ans_hs_ad_verbosity: 'Verbositas', ans_hs_ad_extra: 'Extra vars (objek JSON)',
      ans_hs_ad_warn: 'shell, command, dan raw berjalan di host target dengan identitas SSH yang dipakai Pantau (setara sesi terminal).', ans_hs_ad_run: 'Jalankan', ans_hs_ad_running: 'Memulai…',
      ans_hs_ad_err_module: 'Modul harus cocok dengan [A-Za-z0-9_.]{1,80}.', ans_hs_ad_err_targets: 'Pilih setidaknya satu host atau grup host.', ans_hs_ad_err_forks: 'Forks harus antara 0 dan 20.', ans_hs_ad_err_extra: 'Extra vars harus berupa objek JSON yang valid.',
      ans_hs_ad_failed: 'Gagal memulai run', ans_hs_ad_limit: 'Terlalu banyak run ad-hoc aktif (maks 3); coba lagi sebentar.', ans_hs_ad_cancel: 'Batalkan', ans_hs_ad_cancel_title: 'Batalkan run ad-hoc ini?', ans_hs_ad_cancelled: 'Pembatalan diminta',
      ans_hs_ad_results: 'Hasil', ans_hs_ad_log: 'Output', ans_hs_ad_history: 'Run ad-hoc terbaru', ans_hs_ad_no_history: 'Belum ada run ad-hoc', ans_hs_ad_no_results: 'Belum ada hasil per host.',
      ans_hs_r_success: 'sukses', ans_hs_r_changed: 'berubah', ans_hs_r_unreachable: 'tak terjangkau', ans_hs_r_failed: 'gagal', ans_hs_ad_host: 'Host', ans_hs_ad_status: 'Status', ans_hs_ad_summary: 'Ringkasan',
    },
  });

  const MODULES = ['ping', 'command', 'shell', 'raw', 'setup', 'service', 'systemd', 'package', 'apt', 'yum', 'dnf', 'copy', 'file', 'lineinfile', 'user', 'group', 'uri', 'debug', 'git', 'pip', 'stat', 'fetch'];
  const S = { sub: Ans.store.get('hs.sub', 'explorer'), root: null, data: null, error: '', q: '', sel: new Set(), pinging: new Set(), collapsed: Ans.store.get('hs.collapsed', {}), loading: false, pingT0: 0 };
  const plainAnsi = function (s) { return String(s).replace(/\x1b\[[0-9;?]*[A-Za-z]/g, ''); };
  const logHtml = function (s) { return Ans.runs && Ans.runs.util ? Ans.runs.util.ansiToHtml(s) : esc(plainAnsi(s)); };

  /* ===================== Explorer ===================== */
  async function loadExplorer(silent) {
    if (!silent) { S.loading = true; paintExplorer(); }
    try { S.data = await Ans.api.get('/api/ansible/explorer'); S.error = ''; } catch (e) { S.error = T('ans_hs_load_failed') + ': ' + e.message; }
    S.loading = false;
    if (S.sub === 'explorer') paintExplorer();
  }
  function allHosts() { const d = S.data || {}; const out = []; (d.groups || []).forEach(function (g) { (g.hosts || []).forEach(function (h) { out.push(h); }); }); (d.ungrouped || []).forEach(function (h) { out.push(h); }); return out; }
  const pantauBadge = function (st) { const m = { healthy: 'badge-healthy', degraded: 'badge-warning', down: 'badge-danger' }; return '<span class="badge ' + (m[st] || 'badge-unknown') + '">' + esc(T('ans_hs_st_' + (m[st] ? st : 'unknown'))) + '</span>'; };
  const ansibleBadge = function (h, busy) {
    if (busy) return '<span class="badge badge-info">' + icon('loader-circle', 'icon-sm') + ' ' + esc(T('ans_hs_pinging')) + ' <span data-sp-t0="' + (S.pingT0 || Date.now()) + '" style="font-variant-numeric:tabular-nums;">0s</span></span>';
    const m = { online: ['badge-healthy', 'ans_hs_a_online'], offline: ['badge-danger', 'ans_hs_a_offline'] }[h.ansible_status] || ['badge-unknown', 'ans_hs_a_unknown'];
    return '<span class="badge ' + m[0] + '"' + (h.ansible_checked_at ? ' title="' + esc(Ans.fmt.dt(h.ansible_checked_at)) + '"' : '') + '>' + esc(T(m[1])) + '</span>' + (h.ansible_checked_at ? ' <span class="ans-muted ans-hint">' + esc(Ans.fmt.ago(h.ansible_checked_at)) + '</span>' : '');
  };
  function hostRow(h) {
    return '<tr data-host="' + h.id + '"><td class="ans-hs-chk"><input type="checkbox" data-hs="sel" aria-label="' + esc(h.name) + '"' + (S.sel.has(h.id) ? ' checked' : '') + ' /></td>' +
      '<td><div class="ans-name"><span>' + esc(h.name) + '</span></div><div class="ans-sub ans-mono" title="' + esc(T('ans_hs_alias') + ': ' + (h.ansible_alias || '')) + '">' + esc(h.address) + ':' + h.port + '</div></td>' +
      '<td class="ans-hide-sm">' + pantauBadge(h.status) + '</td><td>' + ansibleBadge(h, S.pinging.has(h.id)) + '</td>' +
      '<td class="ans-hide-md ans-muted">' + esc(h.facts_at ? T('ans_hs_facts_age', 'facts {when}', { when: Ans.fmt.ago(h.facts_at) }) : T('ans_hs_no_facts')) + '</td>' +
      '<td class="ans-actions"><button type="button" class="btn btn-sm" data-hs="ping"' + (S.pinging.has(h.id) ? ' disabled' : '') + '>' + icon('zap', 'icon-sm') + ' <span class="ans-hide-sm">' + esc(T('ans_hs_ping')) + '</span></button> <button type="button" class="btn btn-sm" data-hs="facts">' + icon('list', 'icon-sm') + ' <span class="ans-hide-sm">' + esc(T('ans_hs_facts')) + '</span></button></td></tr>';
  }
  function paintExplorer() {
    const box = S.root && S.root.querySelector('[data-hs="body"]'); if (!box || S.sub !== 'explorer') return;
    if (S.loading && !S.data) { box.innerHTML = Ans.ui.skeleton(6); return; }
    if (S.error && !S.data) { box.innerHTML = Ans.ui.error(S.error, 'data-hs="retry"'); return; }
    const hosts = allHosts();
    if (!hosts.length) { box.innerHTML = Ans.ui.empty({ icon: 'server', title: T('ans_hs_empty_title'), desc: T('ans_hs_empty_desc') }); return; }
    const needle = S.q.trim().toLowerCase();
    const match = function (h) { return !needle || (h.name + ' ' + h.address + ' ' + h.ansible_alias + ' ' + (h.group_name || '')).toLowerCase().indexOf(needle) >= 0; };
    const sections = (S.data.groups || []).map(function (g) { return { name: g.group, key: g.group, hosts: (g.hosts || []).filter(match) }; });
    if ((S.data.ungrouped || []).length) sections.push({ name: T('ans_hs_ungrouped'), key: '__none__', hosts: S.data.ungrouped.filter(match) });
    const shown = sections.filter(function (s) { return s.hosts.length; });
    const sel = S.root.querySelector('[data-hs="selcount"]'); if (sel) sel.textContent = S.sel.size ? T('ans_hs_selected', '{n} selected', { n: S.sel.size }) : '';
    const ps = S.root.querySelector('[data-hs="pingsel"]'); if (ps) ps.disabled = !S.sel.size;
    if (!shown.length) { box.innerHTML = Ans.ui.empty({ icon: 'search', title: T('ans_hs_no_match') }); return; }
    box.innerHTML = '<div class="ans-table-wrap"><table class="ans-table"><thead><tr><th scope="col" class="ans-hs-chk"><input type="checkbox" data-hs="selall" aria-label="select all"' + (hosts.length && S.sel.size === hosts.length ? ' checked' : '') + ' /></th><th scope="col">' + esc(T('ans_hs_ad_host')) + '</th><th scope="col" class="ans-hide-sm">' + esc(T('ans_hs_pantau')) + '</th><th scope="col">' + esc(T('ans_hs_ansible')) + '</th><th scope="col" class="ans-hide-md">' + esc(T('ans_hs_facts')) + '</th><th scope="col" class="ans-actions"></th></tr></thead><tbody>' +
      shown.map(function (s) {
        const col = !!S.collapsed[s.key];
        const ids = s.hosts.map(function (h) { return h.id; }), all = ids.every(function (i) { return S.sel.has(i); });
        return '<tr class="ans-group-row" data-group="' + esc(s.key) + '" tabindex="0" aria-expanded="' + !col + '"><td class="ans-hs-chk"><input type="checkbox" data-hs="selgroup" aria-label="' + esc(s.name) + '"' + (all ? ' checked' : '') + ' /></td><td colspan="5">' + icon(col ? 'chevron-right' : 'chevron-down', 'icon-sm') + ' ' + esc(s.name) + ' <span class="ans-muted">' + (s.hosts.length === 1 ? esc(T('ans_hs_host_one')) : esc(T('ans_hs_hosts', '{n} hosts', { n: s.hosts.length }))) + '</span></td></tr>' + (col ? '' : s.hosts.map(hostRow).join(''));
      }).join('') + '</tbody></table></div>';
  }
  async function pingHosts(ids) {
    if (!ids.length) return;
    S.pingT0 = Date.now();
    ids.forEach(function (i) { S.pinging.add(i); }); paintExplorer();
    try {
      const d = await Ans.api.post('/api/ansible/adhoc', { module: 'ping', host_ids: ids });
      const id = d.run.id;
      let done = false;
      for (let i = 0; i < 120; i++) {
        await new Promise(function (r) { setTimeout(r, 1000); });
        const s = await Ans.api.get('/api/ansible/adhoc/' + id);
        if (['queued', 'running'].indexOf(s.run.status) < 0) { const c = s.run.counts || {}; const ok = (c.success || 0) + (c.changed || 0), bad = (c.unreachable || 0) + (c.failed || 0); Ans.toast(T('ans_hs_ping_done', 'Ping finished: {ok} reachable, {bad} not', { ok: ok, bad: bad }), bad ? 'warning' : 'success'); done = true; break; }
      }
      if (!done) Ans.toast(T('ans_hs_ping_timeout', 'Ping is still running after 2 minutes; check the Ad-hoc tab for its progress.'), 'warning');
    } catch (e) { Ans.toast(e.status === 429 ? T('ans_hs_ping_busy') : T('ans_hs_ping_failed') + ': ' + e.message, 'error'); }
    ids.forEach(function (i) { S.pinging.delete(i); });
    S.pingT0 = 0;
    await loadExplorer(true);
  }

  /* ----- facts drawer ----- */
  function fmtMB(n) { return n ? (n >= 1024 ? (n / 1024).toFixed(1) + ' GiB' : n + ' MiB') : '-'; }
  function fmtUp(sec) { if (!sec) return '-'; const d = Math.floor(sec / 86400), h = Math.floor(sec % 86400 / 3600), m = Math.floor(sec % 3600 / 60); return (d ? d + 'd ' : '') + (h || d ? h + 'h ' : '') + m + 'm'; }
  function factsCards(sum) {
    const kv = function (k, v) { return v ? '<div class="ans-hs-kv"><span class="ans-rn-mk">' + esc(k) + '</span><span class="ans-mono">' + esc(v) + '</span></div>' : ''; };
    const mounts = (sum.mounts || []).map(function (m) { const pct = m.size_total ? Math.round((m.size_total - m.size_available) * 100 / m.size_total) : 0; return '<tr><td class="ans-mono">' + esc(m.mount) + '</td><td class="ans-muted ans-mono">' + esc(m.fstype || '') + '</td><td><span class="ans-hs-bar" title="' + pct + '%"><i style="width:' + pct + '%" class="' + (pct > 90 ? 'bad' : pct > 75 ? 'warn' : '') + '"></i></span> <span class="ans-mono ans-muted">' + pct + '% · ' + esc(Ans.fmt.bytes(m.size_total)) + '</span></td></tr>'; }).join('');
    const ifaces = (sum.interfaces || []).map(function (n) { return '<tr><td class="ans-mono">' + esc(n.name) + '</td><td class="ans-mono">' + esc([n.ipv4].concat(n.ipv6 || []).filter(Boolean).join(', ') || '-') + '</td><td class="ans-muted ans-mono">' + esc(n.mac || '') + '</td></tr>'; }).join('');
    return '<div class="ans-hs-cards"><div class="ans-card"><div class="ans-card-title">' + esc(T('ans_hs_f_system')) + '</div>' +
      kv(T('ans_hs_f_os'), [sum.distribution, sum.version].filter(Boolean).join(' ') || sum.os) + kv(T('ans_hs_f_kernel'), sum.kernel) + kv(T('ans_hs_f_arch'), sum.architecture) + kv(T('ans_hs_f_hostname'), sum.fqdn || sum.hostname) + kv(T('ans_hs_f_virt'), sum.virtualization) + kv(T('ans_hs_f_python'), sum.python) + kv(T('ans_hs_f_uptime'), fmtUp(sum.uptime_seconds)) + '</div>' +
      '<div class="ans-card"><div class="ans-card-title">' + esc(T('ans_hs_f_compute')) + '</div>' + kv(T('ans_hs_f_cpu'), sum.cpu_model) + kv(T('ans_hs_f_cores'), sum.cpu_cores ? String(sum.cpu_cores) : '') + kv(T('ans_hs_f_mem'), fmtMB(sum.memtotal_mb)) + kv(T('ans_hs_f_swap'), fmtMB(sum.swaptotal_mb)) + '</div>' +
      '<div class="ans-card"><div class="ans-card-title">' + esc(T('ans_hs_f_network')) + '</div>' + kv(T('ans_hs_f_ip'), sum.default_ipv4) + (ifaces ? '<div class="ans-table-wrap"><table class="ans-table"><thead><tr><th scope="col">' + esc(T('ans_hs_f_ifaces')) + '</th><th scope="col">IP</th><th scope="col">MAC</th></tr></thead><tbody>' + ifaces + '</tbody></table></div>' : '') + '</div>' +
      (mounts ? '<div class="ans-card"><div class="ans-card-title">' + esc(T('ans_hs_f_storage')) + '</div><div class="ans-table-wrap"><table class="ans-table"><thead><tr><th scope="col">' + esc(T('ans_hs_f_mount')) + '</th><th scope="col">' + esc(T('ans_hs_f_fs')) + '</th><th scope="col">' + esc(T('ans_hs_f_usage')) + '</th></tr></thead><tbody>' + mounts + '</tbody></table></div></div>' : '') + '</div>';
  }
  function openFacts(hostId) {
    const h = allHosts().find(function (x) { return x.id === hostId; }) || (Ans.state.hosts || []).find(function (x) { return x.id === hostId; }) || { id: hostId, name: '#' + hostId };
    const ctx = { facts: null, tab: 'summary', filter: '', busy: false, dead: false, body: null, ctrl: null, err: '' };
    const paint = function () {
      if (ctx.dead || !ctx.body) return;
      const f = ctx.facts, pane = ctx.body.querySelector('[data-f="pane"]');
      ctx.ctrl.setSubtitle(f ? '<span class="ans-muted" title="' + esc(Ans.fmt.dt(f.gathered_at)) + '">' + esc(T('ans_hs_f_gathered', 'Gathered {when}', { when: Ans.fmt.ago(f.gathered_at) })) + '</span>' : '');
      ctx.ctrl.setActions('<button type="button" class="btn btn-sm btn-primary" data-f="refresh"' + (ctx.busy ? ' disabled' : '') + '>' + (ctx.busy ? SP.html(T('ans_hs_f_refreshing'), ctx.busyT0) : icon('refresh-cw', 'icon-sm') + ' ' + esc(T(f ? 'ans_hs_f_refresh' : 'ans_hs_f_gather'))) + '</button>');
      const head = document.querySelector('#ansDrawerRoot .ans-drawer-actions'); if (head) head.onclick = function (e) { if (e.target.closest('[data-f="refresh"]')) refresh(); };
      ctx.body.querySelectorAll('[data-ftab]').forEach(function (b) { b.setAttribute('aria-selected', String(b.getAttribute('data-ftab') === ctx.tab)); });
      if (ctx.err) { pane.innerHTML = Ans.ui.error(ctx.err); return; }
      if (!f) { pane.innerHTML = Ans.ui.empty({ icon: 'list', title: ctx.loading ? '…' : T('ans_hs_f_none') }); return; }
      if (ctx.tab === 'summary') pane.innerHTML = factsCards(f.summary || {});
      else {
        const lines = JSON.stringify(f.facts || {}, null, 2).split('\n'), n = ctx.filter.trim().toLowerCase();
        const shown = n ? lines.filter(function (l) { return l.toLowerCase().indexOf(n) >= 0; }) : lines;
        pane.innerHTML = '<div class="ans-row"><div class="search-box" style="flex:1;margin:0"><input class="form-input" data-f="q" placeholder="' + esc(T('ans_hs_f_filter')) + '" aria-label="' + esc(T('ans_hs_f_filter')) + '" value="' + esc(ctx.filter) + '" style="height:30px;font-size:var(--fs-xs);width:100%" /></div><button type="button" class="btn btn-sm" data-f="copy">' + icon('copy', 'icon-sm') + ' ' + esc(T('ans_hs_f_copy')) + '</button></div>' +
          '<pre class="ans-hs-raw">' + (shown.length ? esc(shown.slice(0, 4000).join('\n')) + (shown.length > 4000 ? '\n…' : '') : esc(T('ans_hs_f_nolines'))) + '</pre>';
        const qi = pane.querySelector('[data-f="q"]'); if (qi && ctx.refocus) { qi.focus(); qi.setSelectionRange(qi.value.length, qi.value.length); ctx.refocus = false; }
      }
    };
    const load = async function () {
      ctx.loading = true; paint();
      try { ctx.facts = await Ans.api.get('/api/ansible/hosts/' + hostId + '/facts'); ctx.err = ''; }
      catch (e) { if (e.status !== 404) ctx.err = T('ans_hs_f_load_failed') + ': ' + e.message; }
      ctx.loading = false; paint();
    };
    async function refresh() {
      if (ctx.busy) return;
      ctx.busy = true; ctx.busyT0 = Date.now(); ctx.err = ''; paint();
      try { await Ans.api.post('/api/ansible/hosts/' + hostId + '/facts'); ctx.facts = await Ans.api.get('/api/ansible/hosts/' + hostId + '/facts'); loadExplorer(true); }
      catch (e) { ctx.err = T('ans_hs_f_failed') + ': ' + e.message; }
      finally { ctx.busy = false; ctx.busyT0 = 0; paint(); }
    }
    return Ans.drawer.push({
      key: 'facts:' + hostId, title: T('ans_hs_f_title', 'Facts: {name}', { name: h.name }), subtitle: '', wide: true,
      mount: function (body, ctrl) {
        ctx.body = body; ctx.ctrl = ctrl;
        body.innerHTML = '<div class="ans-drawer-tabs" role="tablist"><button type="button" role="tab" data-ftab="summary" aria-selected="true">' + icon('layers', 'icon-sm') + '<span>' + esc(T('ans_hs_f_summary')) + '</span></button><button type="button" role="tab" data-ftab="raw" aria-selected="false">' + icon('file-code', 'icon-sm') + '<span>' + esc(T('ans_hs_f_raw')) + '</span></button></div><div class="ans-drawer-pane" data-f="pane"></div>';
        body.addEventListener('click', function (e) {
          const t = e.target.closest('[data-ftab]'); if (t) { ctx.tab = t.getAttribute('data-ftab'); paint(); return; }
          if (e.target.closest('[data-f="copy"]') && ctx.facts) { const txt = JSON.stringify(ctx.facts.facts || {}, null, 2); (navigator.clipboard ? navigator.clipboard.writeText(txt) : Promise.reject()).then(function () { Ans.toast(T('ans_hs_f_copied'), 'success'); }, function () { /* clipboard unavailable */ }); }
        });
        body.addEventListener('input', function (e) { if (e.target.matches('[data-f="q"]')) { ctx.filter = e.target.value; ctx.refocus = true; paint(); } });
        load();
      },
      unmount: function () { ctx.dead = true; const head = document.querySelector('#ansDrawerRoot .ans-drawer-actions'); if (head) head.onclick = null; },
    });
  }

  /* ===================== Ad-hoc ===================== */
  const A = { picker: null, run: null, timer: null, off: 0, log: '', history: null, form: { module: 'ping', args: '', pattern: '', group: '', become: false, forks: 0, verbosity: 0, extra: '', ids: [] }, busy: false };

  function adhocShell(box) {
    const groups = Array.from(new Set((Ans.state.hosts || []).map(function (h) { return (h.group_name || '').trim(); }).filter(Boolean))).sort();
    const f = A.form;
    box.innerHTML = '<div class="ans-hs-adhoc"><div class="ans-card ans-hs-form"><div class="ans-error ans-hs-warn" style="background:var(--warning-soft);border-color:var(--warning-border);color:var(--warning)">' + icon('shield-alert') + '<span>' + esc(T('ans_hs_ad_warn')) + '</span></div>' +
      '<div class="ans-grid-2"><div class="ans-field"><label for="ansHsModule">' + esc(T('ans_hs_ad_module')) + '</label><input id="ansHsModule" class="form-input ans-mono" list="ansHsModules" data-ad="module" placeholder="' + esc(T('ans_hs_ad_module_ph')) + '" value="' + esc(f.module) + '" autocomplete="off" /><datalist id="ansHsModules">' + MODULES.map(function (m) { return '<option value="' + m + '"></option>'; }).join('') + '</datalist><div class="err" data-err="module"></div></div>' +
      '<div class="ans-field"><label for="ansHsArgs">' + esc(T('ans_hs_ad_args')) + '</label><input id="ansHsArgs" class="form-input ans-mono" data-ad="args" placeholder="' + esc(T('ans_hs_ad_args_ph')) + '" value="' + esc(f.args) + '" autocomplete="off" /></div></div>' +
      '<div class="ans-field"><label>' + esc(T('ans_hs_ad_targets')) + '</label><div data-ad="picker"></div><div class="err" data-err="targets"></div></div>' +
      '<div class="ans-grid-2"><div class="ans-field"><label for="ansHsGroup">' + esc(T('ans_hs_ad_group')) + '</label><select id="ansHsGroup" class="form-input" data-ad="group"><option value="">' + esc(T('ans_hs_ad_group_none')) + '</option>' + groups.map(function (g) { return '<option value="' + esc(g) + '"' + (f.group === g ? ' selected' : '') + '>' + esc(g) + '</option>'; }).join('') + '</select></div>' +
      '<div class="ans-field"><label for="ansHsPattern">' + esc(T('ans_hs_ad_pattern')) + '</label><input id="ansHsPattern" class="form-input ans-mono" data-ad="pattern" placeholder="' + esc(T('ans_hs_ad_pattern_ph')) + '" value="' + esc(f.pattern) + '" /></div></div>' +
      '<details class="ans-details"><summary>' + esc(T('ans_hs_ad_advanced')) + '</summary><div class="ans-grid-2" style="padding-top:6px"><div class="ans-field"><label for="ansHsForks">' + esc(T('ans_hs_ad_forks')) + '</label><input id="ansHsForks" type="number" min="0" max="20" class="form-input" data-ad="forks" value="' + esc(f.forks) + '" /><div class="err" data-err="forks"></div></div>' +
      '<div class="ans-field"><label for="ansHsVerb">' + esc(T('ans_hs_ad_verbosity')) + '</label><select id="ansHsVerb" class="form-input" data-ad="verbosity">' + [0, 1, 2, 3, 4].map(function (n) { return '<option value="' + n + '"' + (f.verbosity === n ? ' selected' : '') + '>' + (n ? '-' + 'v'.repeat(n) : '0') + '</option>'; }).join('') + '</select></div>' +
      '<div class="ans-field ans-ln-wide"><label for="ansHsExtra">' + esc(T('ans_hs_ad_extra')) + '</label><textarea id="ansHsExtra" class="form-input ans-mono" rows="2" data-ad="extra">' + esc(f.extra) + '</textarea><div class="err" data-err="extra"></div></div>' +
      '<div class="ans-field ans-ln-wide"><label class="ans-row" style="cursor:pointer"><span class="switch"><input type="checkbox" data-ad="become"' + (f.become ? ' checked' : '') + ' /><span class="switch-track"><span class="switch-thumb"></span></span></span><span>' + esc(T('ans_hs_ad_become')) + '</span></label></div></div></details>' +
      '<div class="ans-error" role="alert" data-ad="error" hidden></div><div class="ans-row" style="justify-content:flex-end"><button type="button" class="btn btn-primary" data-ad="run">' + icon('play', 'icon-sm') + ' <span>' + esc(T('ans_hs_ad_run')) + '</span></button></div></div>' +
      '<div class="ans-hs-side"><div data-ad="run-panel"></div><div class="ans-card" data-ad="history"><div class="ans-card-title">' + icon('history', 'icon-sm') + esc(T('ans_hs_ad_history')) + '</div><div data-ad="hist-body">' + Ans.ui.skeleton(2) + '</div></div></div></div>';
    if (A.picker) A.picker.destroy();
    A.picker = Ans.hostPicker.mount(box.querySelector('[data-ad="picker"]'), { ids: A.form.ids, max: 200, onChange: function (ids) { A.form.ids = ids; } });
    paintRunPanel(); loadHistory();
  }
  async function loadHistory() {
    try { const d = await Ans.api.get('/api/ansible/adhoc?limit=15'); A.history = d.runs || []; } catch (e) { A.history = A.history || []; }
    const el = S.root && S.root.querySelector('[data-ad="hist-body"]'); if (!el) return;
    el.innerHTML = A.history.length ? '<div class="ans-hs-hist">' + A.history.map(function (r) {
      return '<button type="button" class="ans-hs-hitem" data-adrun="' + r.id + '">' + Ans.ui.dot(r.status) + '<span class="ans-mono">#' + r.id + '</span><strong>' + esc(r.module) + '</strong><span class="ans-muted ans-pb-nm">' + esc(r.args || '') + '</span><span class="ans-spacer"></span><span class="ans-muted">' + esc(Ans.fmt.ago(r.created_at)) + '</span></button>';
    }).join('') + '</div>' : '<div class="ans-muted ans-hint">' + esc(T('ans_hs_ad_no_history')) + '</div>';
  }
  function paintRunPanel() {
    const p = S.root && S.root.querySelector('[data-ad="run-panel"]'); if (!p) return;
    const r = A.run;
    if (!r) { p.innerHTML = ''; return; }
    const c = r.counts || {}, active = r.status === 'queued' || r.status === 'running';
    const res = (r.results || []).map(function (x) { return '<tr class="ans-hs-r-' + esc(x.status) + '"><td class="ans-mono">' + esc(x.alias) + '</td><td><span class="ans-hs-rs">' + esc(T('ans_hs_r_' + x.status, x.status)) + '</span></td><td class="ans-mono ans-hs-sum">' + esc(x.summary || '') + '</td></tr>'; }).join('');
    p.innerHTML = '<div class="ans-card"><div class="ans-row"><div class="ans-card-title">' + icon('terminal', 'icon-sm') + esc(r.module) + '</div>' + Ans.ui.badge(r.status) + '<span class="ans-mono ans-muted">#' + r.id + '</span><span class="ans-spacer"></span>' +
      (active ? '<button type="button" class="btn btn-sm btn-danger" data-ad="cancel">' + icon('square', 'icon-sm') + ' ' + esc(T('ans_hs_ad_cancel')) + '</button>' : '') + '</div>' +
      '<div class="ans-row ans-hs-counts"><span class="ans-hs-c ok">' + (c.success || 0) + ' ' + esc(T('ans_hs_r_success')) + '</span><span class="ans-hs-c changed">' + (c.changed || 0) + ' ' + esc(T('ans_hs_r_changed')) + '</span><span class="ans-hs-c bad">' + (c.unreachable || 0) + ' ' + esc(T('ans_hs_r_unreachable')) + '</span><span class="ans-hs-c bad">' + (c.failed || 0) + ' ' + esc(T('ans_hs_r_failed')) + '</span></div>' +
      (res ? '<div class="ans-table-wrap"><table class="ans-table"><thead><tr><th scope="col">' + esc(T('ans_hs_ad_host')) + '</th><th scope="col">' + esc(T('ans_hs_ad_status')) + '</th><th scope="col">' + esc(T('ans_hs_ad_summary')) + '</th></tr></thead><tbody>' + res + '</tbody></table></div>' : '<div class="ans-muted ans-hint">' + esc(T('ans_hs_ad_no_results')) + '</div>') +
      '<details class="ans-details" ' + (active || !res ? 'open' : '') + '><summary>' + esc(T('ans_hs_ad_log')) + '</summary><pre class="ans-hs-log" data-ad="log">' + logHtml(A.log) + '</pre></details></div>';
    const lg = p.querySelector('[data-ad="log"]'); if (lg) lg.scrollTop = lg.scrollHeight;
  }
  async function pollRun(id) {
    clearTimeout(A.timer);
    try {
      const l = await Ans.api.get('/api/ansible/adhoc/' + id + '/log?offset=' + A.off);
      if (!A.run || A.run.id !== id) return;
      if (l.log) { A.log += l.log; } A.off = l.offset;
      const d = await Ans.api.get('/api/ansible/adhoc/' + id);
      A.run = d.run; paintRunPanel();
      if (l.finished || ['queued', 'running'].indexOf(d.run.status) < 0) { loadHistory(); loadExplorer(true); return; }
    } catch (e) { /* retry on next tick */ }
    A.timer = setTimeout(function () { pollRun(id); }, 1000);
  }
  async function openRun(id) {
    clearTimeout(A.timer);
    try { const d = await Ans.api.get('/api/ansible/adhoc/' + id); A.run = d.run; A.off = 0; A.log = ''; paintRunPanel(); pollRun(id); } catch (e) { Ans.toast(e.message, 'error'); }
  }
  async function startAdhoc() {
    const box = S.root, f = A.form;
    const err = function (k, m) { const e = box.querySelector('[data-err="' + k + '"]'); if (e) e.textContent = m || ''; };
    ['module', 'targets', 'forks', 'extra'].forEach(function (k) { err(k, ''); });
    const top = box.querySelector('[data-ad="error"]'); top.hidden = true;
    let bad = false;
    if (!/^[A-Za-z0-9_.]{1,80}$/.test(f.module.trim()) || /^-/.test(f.module.trim())) { err('module', T('ans_hs_ad_err_module')); bad = true; }
    if (!f.ids.length && !f.group) { err('targets', T('ans_hs_ad_err_targets')); bad = true; }
    if (!(f.forks >= 0 && f.forks <= 20)) { err('forks', T('ans_hs_ad_err_forks')); bad = true; }
    if (f.extra.trim()) { try { const o = JSON.parse(f.extra); if (!o || typeof o !== 'object' || Array.isArray(o)) throw new Error('x'); } catch (e) { err('extra', T('ans_hs_ad_err_extra')); bad = true; } }
    if (bad) return;
    const btn = box.querySelector('[data-ad="run"]'); btn.disabled = true; btn.querySelector('span').textContent = T('ans_hs_ad_running');
    try {
      const body = { module: f.module.trim(), args: f.args, pattern: f.pattern.trim(), host_ids: f.ids, group: f.group, become: f.become, forks: Number(f.forks) || 0, verbosity: f.verbosity };
      if (f.extra.trim()) body.extra_vars = f.extra.trim();
      const d = await Ans.api.post('/api/ansible/adhoc', body);
      A.run = d.run; A.off = 0; A.log = ''; paintRunPanel(); pollRun(d.run.id); loadHistory();
    } catch (e) { top.textContent = e.status === 429 ? T('ans_hs_ad_limit') : T('ans_hs_ad_failed') + ': ' + e.message; top.hidden = false; }
    finally { btn.disabled = false; btn.querySelector('span').textContent = T('ans_hs_ad_run'); }
  }

  /* ===================== tab ===================== */
  function paintSub() {
    const root = S.root; if (!root) return;
    root.querySelectorAll('[data-sub]').forEach(function (b) { b.setAttribute('aria-pressed', String(b.getAttribute('data-sub') === S.sub)); });
    root.querySelector('[data-hs="exp-tools"]').hidden = S.sub !== 'explorer';
    const body = root.querySelector('[data-hs="body"]');
    if (S.sub === 'explorer') { if (A.picker) { A.picker.destroy(); A.picker = null; } clearTimeout(A.timer); paintExplorer(); loadExplorer(!!S.data); }
    else adhocShell(body);
  }
  function render(panel) {
    S.root = panel;
    panel.innerHTML = '<div class="ans-toolbar"><div class="ans-seg" role="group"><button type="button" data-sub="explorer" aria-pressed="true">' + esc(T('ans_hs_sub_explorer')) + '</button><button type="button" data-sub="adhoc" aria-pressed="false">' + esc(T('ans_hs_sub_adhoc')) + '</button></div>' +
      '<span class="ans-toolbar" data-hs="exp-tools"><div class="search-box"><input type="text" class="form-input" data-hs="q" placeholder="' + esc(T('ans_hs_search')) + '" aria-label="' + esc(T('ans_hs_search')) + '" value="' + esc(S.q) + '" /></div>' +
      '<button type="button" class="btn btn-sm" data-hs="pingsel" disabled>' + icon('zap', 'icon-sm') + ' ' + esc(T('ans_hs_ping_sel')) + '</button><button type="button" class="btn btn-sm" data-hs="pingall">' + icon('zap', 'icon-sm') + ' ' + esc(T('ans_hs_ping_all')) + '</button><span class="ans-mono ans-muted" data-hs="selcount"></span></span></div><div data-hs="body"></div>';
    panel.addEventListener('click', onClick);
    panel.addEventListener('input', function (e) {
      const t = e.target;
      if (t.matches('[data-hs="q"]')) { S.q = t.value; paintExplorer(); return; }
      const k = t.getAttribute && t.getAttribute('data-ad'); if (!k) return;
      A.form[k] = t.type === 'checkbox' ? t.checked : (k === 'forks' ? Number(t.value) : t.value);
    });
    panel.addEventListener('change', function (e) {
      const t = e.target;
      if (t.matches('[data-hs="sel"]')) { const id = Number(t.closest('tr').getAttribute('data-host')); if (t.checked) S.sel.add(id); else S.sel.delete(id); paintExplorer(); return; }
      if (t.matches('[data-hs="selall"]')) { S.sel = t.checked ? new Set(allHosts().map(function (h) { return h.id; })) : new Set(); paintExplorer(); return; }
      if (t.matches('[data-hs="selgroup"]')) { const key = t.closest('tr').getAttribute('data-group'); const g = key === '__none__' ? (S.data.ungrouped || []) : ((S.data.groups || []).find(function (x) { return x.group === key; }) || {}).hosts || []; g.forEach(function (h) { if (t.checked) S.sel.add(h.id); else S.sel.delete(h.id); }); paintExplorer(); return; }
      const k = t.getAttribute && t.getAttribute('data-ad'); if (k) A.form[k] = t.type === 'checkbox' ? t.checked : (k === 'verbosity' ? Number(t.value) : t.value);
    });
    paintSub();
  }
  function onClick(e) {
    const sub = e.target.closest('[data-sub]'); if (sub && S.root.contains(sub)) { S.sub = sub.getAttribute('data-sub'); Ans.store.set('hs.sub', S.sub); paintSub(); return; }
    const adr = e.target.closest('[data-adrun]'); if (adr) { openRun(Number(adr.getAttribute('data-adrun'))); return; }
    const ad = e.target.closest('[data-ad]');
    if (ad) {
      const a = ad.getAttribute('data-ad');
      if (a === 'run') startAdhoc();
      else if (a === 'cancel' && A.run) Ans.confirm({ title: T('ans_hs_ad_cancel_title'), message: '', confirmText: T('ans_hs_ad_cancel'), danger: true }).then(function (ok) { if (ok) Ans.api.post('/api/ansible/adhoc/' + A.run.id + '/cancel').then(function () { Ans.toast(T('ans_hs_ad_cancelled'), 'success'); }, function (er) { Ans.toast(er.message, 'error'); }); });
      return;
    }
    const hs = e.target.closest('[data-hs]');
    if (hs) {
      const a = hs.getAttribute('data-hs');
      if (a === 'ping') pingHosts([Number(hs.closest('tr').getAttribute('data-host'))]);
      else if (a === 'facts') openFacts(Number(hs.closest('tr').getAttribute('data-host')));
      else if (a === 'pingsel') pingHosts(Array.from(S.sel));
      else if (a === 'pingall') pingHosts(allHosts().map(function (h) { return h.id; }));
      else if (a === 'retry') { S.error = ''; loadExplorer(); }
      return;
    }
    const gr = e.target.closest('tr[data-group]'); if (gr && !e.target.closest('input')) { const k = gr.getAttribute('data-group'); S.collapsed[k] = !S.collapsed[k]; Ans.store.set('hs.collapsed', S.collapsed); paintExplorer(); }
  }

  Ans.hosts = { openFacts: openFacts, openAdhoc: function (o) { S.sub = 'adhoc'; Ans.store.set('hs.sub', 'adhoc'); if (o && o.hostIds) A.form.ids = o.hostIds.slice(); Ans.showTab('hosts'); if (S.root) paintSub(); } };
  Ans.registerTab({ id: 'hosts', icon: 'server', labelKey: 'ans_tab_hosts', order: 30, render: render, onShow: function (p, o) { if (S.root && S.sub === 'explorer' && o && o.refresh) loadExplorer(true); } });
})();
