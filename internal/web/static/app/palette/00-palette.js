/* Command palette (Ctrl+K / Cmd+K).
 * Lists hosts (fuzzy on name/address/group) and global commands; a host row opens the host and a
 * second level lists the per-host actions.
 * Relies on index.html globals: allRawHosts, t, I18N, escapeHtml, svgIcon, openHostDetail,
 * openHostTerminal, openHostSecurity, openHostDocker, openEditHostModal, inspectHost, switchTab,
 * inspectAllHosts, openBulkExecModal, openAddHostModal, openImportHostsModal, openSettingsModal,
 * openDocumentationModal, switchSideNav. Optional modules: window.PortFwd, window.FleetSearch. */
(function () {
  'use strict';

  const P = (window.Palette = {});
  const esc = (s) => escapeHtml(String(s === null || s === undefined ? '' : s));
  const icon = (n) => svgIcon(n, 'icon-sm');
  const g = (name) => (typeof window[name] === 'function' ? window[name].bind(window) : null);
  const allHosts = () => (typeof allRawHosts !== 'undefined' && Array.isArray(allRawHosts) ? allRawHosts : []);

  /* ---------- i18n ---------- */
  const I = {
    en: {
      pal_ph: 'Search hosts and commands…',
      pal_empty: 'No matches',
      pal_recent: 'Recent',
      pal_hosts: 'Hosts',
      pal_commands: 'Commands',
      pal_actions: 'Actions',
      pal_foot: '↑↓ navigate · Enter run · Esc close',
      pal_cmd_inspect_all: 'Inspect all hosts',
      pal_cmd_bulk_exec: 'Bulk exec',
      pal_cmd_add_host: 'Add host',
      pal_cmd_import_hosts: 'Import hosts',
      pal_cmd_settings: 'Settings',
      pal_cmd_docs: 'Documentation',
      pal_cmd_portfwd: 'Port forwards',
      pal_cmd_fleet: "Search packages & containers across hosts for '{q}'",
      pal_sec_hosts: 'Go to Hosts',
      pal_sec_docker: 'Go to Docker',
      pal_sec_kube: 'Go to Kubernetes',
      pal_sec_cloudflare: 'Go to Cloudflare',
      pal_sec_ansible: 'Go to Ansible',
      pal_sec_terraform: 'Go to Terraform',
      pal_act_overview: 'Overview',
      pal_act_terminal: 'Terminal',
      pal_act_files: 'Files',
      pal_act_docker: 'Docker',
      pal_act_security: 'Security',
      pal_act_packages: 'Packages',
      pal_act_inspect: 'Inspect now',
      pal_act_portfwd: 'Port forward',
      pal_act_edit: 'Edit host',
      pal_more: 'More actions for this host'
    },
    id: {
      pal_ph: 'Cari host dan perintah…',
      pal_empty: 'Tidak ada yang cocok',
      pal_recent: 'Terbaru',
      pal_hosts: 'Host',
      pal_commands: 'Perintah',
      pal_actions: 'Aksi',
      pal_foot: '↑↓ navigasi · Enter jalankan · Esc tutup',
      pal_cmd_inspect_all: 'Periksa semua host',
      pal_cmd_bulk_exec: 'Eksekusi massal',
      pal_cmd_add_host: 'Tambah host',
      pal_cmd_import_hosts: 'Impor host',
      pal_cmd_settings: 'Pengaturan',
      pal_cmd_docs: 'Dokumentasi',
      pal_cmd_portfwd: 'Port forward',
      pal_cmd_fleet: "Cari paket & container di semua host untuk '{q}'",
      pal_sec_hosts: 'Ke Hosts',
      pal_sec_docker: 'Ke Docker',
      pal_sec_kube: 'Ke Kubernetes',
      pal_sec_cloudflare: 'Ke Cloudflare',
      pal_sec_ansible: 'Ke Ansible',
      pal_sec_terraform: 'Ke Terraform',
      pal_act_overview: 'Ringkasan',
      pal_act_terminal: 'Terminal',
      pal_act_files: 'Berkas',
      pal_act_docker: 'Docker',
      pal_act_security: 'Keamanan',
      pal_act_packages: 'Paket',
      pal_act_inspect: 'Periksa sekarang',
      pal_act_portfwd: 'Port forward',
      pal_act_edit: 'Ubah host',
      pal_more: 'Aksi lain untuk host ini'
    }
  };
  ['en', 'id'].forEach((l) => { I18N[l] = Object.assign(I18N[l] || {}, I[l]); });

  /* ---------- state ---------- */
  const S = { open: false, mode: 'root', query: '', items: [], idx: 0, host: null, prevFocus: null };
  const RECENT_KEY = 'pantau_palette_recent';
  const MAX_ITEMS = 50;

  /* ---------- fuzzy match ---------- */
  function fuzzy(q, s) {
    if (!q) return 1;
    if (!s) return -1;
    const ls = s.toLowerCase();
    let qi = 0, score = 0, streak = 0, first = -1;
    for (let i = 0; i < ls.length && qi < q.length; i++) {
      if (ls[i] === q[qi]) {
        if (first < 0) first = i;
        streak++;
        score += 10 + streak * 4;
        qi++;
      } else {
        streak = 0;
      }
    }
    if (qi < q.length) return -1;
    score -= first;
    if (ls.startsWith(q)) score += 120;
    else if (ls.indexOf(q) >= 0) score += 40;
    return score;
  }

  // every token must match somewhere; returns summed score or -1
  function scoreFields(tokens, fields) {
    let total = 0;
    for (const tok of tokens) {
      let best = -1;
      for (const f of fields) {
        const sc = fuzzy(tok, f);
        if (sc > best) best = sc;
      }
      if (best < 0) return -1;
      total += best;
    }
    return total;
  }

  const tokens = (q) => q.toLowerCase().split(/\s+/).filter(Boolean);

  /* ---------- recent hosts ---------- */
  function recentIds() {
    try {
      const v = JSON.parse(localStorage.getItem(RECENT_KEY) || '[]');
      return Array.isArray(v) ? v.slice(0, 5) : [];
    } catch (e) { return []; }
  }

  function rememberHost(id) {
    try {
      const list = recentIds().filter((x) => String(x) !== String(id));
      list.unshift(id);
      localStorage.setItem(RECENT_KEY, JSON.stringify(list.slice(0, 5)));
    } catch (e) { /* storage unavailable */ }
  }

  /* ---------- items ---------- */
  function hostItem(h, group) {
    const sub = [h.host || '', h.group_name || ''].filter(Boolean).join(' · ');
    return {
      kind: 'host',
      group: group || 'hosts',
      title: h.name || h.host || ('Host ' + h.id),
      sub: sub,
      icon: 'server',
      host: h,
      run: () => { rememberHost(h.id); const f = g('openHostDetail'); if (f) f(h.id); },
      more: true
    };
  }

  function hostActions(h) {
    const acts = [];
    const detail = g('openHostDetail');
    const switchTabFn = g('switchTab');
    const setActiveTab = g('setHostActiveTab');
    // openHostDetail restores the remembered tab, so set it first; switchTab is only the
    // fallback when the detail modal really is open for this host.
    const tabAction = (title, iconName, tab) => {
      if (!detail) return;
      acts.push({ kind: 'action', group: 'actions', title, sub: h.name, icon: iconName,
        run: async () => {
          if (setActiveTab) setActiveTab(h.id, tab);
          await detail(h.id);
          if (switchTabFn && typeof currentHost !== 'undefined' && currentHost && currentHost.id === h.id) switchTabFn(tab);
        } });
    };
    tabAction(t('pal_act_overview', 'Overview'), 'gauge', 'overview');
    const term = g('openHostTerminal');
    if (term) acts.push({ kind: 'action', group: 'actions', title: t('pal_act_terminal', 'Terminal'), sub: h.name, icon: 'terminal', run: () => term(h.id) });
    tabAction(t('pal_act_files', 'Files'), 'folder', 'files');
    const docker = g('openHostDocker');
    if (docker) acts.push({ kind: 'action', group: 'actions', title: t('pal_act_docker', 'Docker'), sub: h.name, icon: 'container', run: () => docker(h.id) });
    const sec = g('openHostSecurity');
    if (sec) acts.push({ kind: 'action', group: 'actions', title: t('pal_act_security', 'Security'), sub: h.name, icon: 'shield', run: () => sec(h.id) });
    tabAction(t('pal_act_packages', 'Packages'), 'package', 'packages');
    const insp = g('inspectHost');
    if (insp) acts.push({ kind: 'action', group: 'actions', title: t('pal_act_inspect', 'Inspect now'), sub: h.name, icon: 'radar', run: () => insp(h.id) });
    if (window.PortFwd && typeof window.PortFwd.open === 'function') {
      acts.push({ kind: 'action', group: 'actions', title: t('pal_act_portfwd', 'Port forward'), sub: h.name, icon: 'plug', run: () => window.PortFwd.open(h.id) });
    }
    const edit = g('openEditHostModal');
    if (edit) acts.push({ kind: 'action', group: 'actions', title: t('pal_act_edit', 'Edit host'), sub: h.name, icon: 'pencil', run: () => edit(h.id) });
    return acts;
  }

  function globalCommands() {
    const cmds = [];
    const add = (title, iconName, fn) => { if (typeof fn === 'function') cmds.push({ kind: 'command', group: 'commands', title, sub: '', icon: iconName, run: fn }); };
    add(t('pal_cmd_inspect_all', 'Inspect all hosts'), 'radar', g('inspectAllHosts'));
    add(t('pal_cmd_bulk_exec', 'Bulk exec'), 'square-terminal', g('openBulkExecModal'));
    add(t('pal_cmd_add_host', 'Add host'), 'plus', g('openAddHostModal'));
    add(t('pal_cmd_import_hosts', 'Import hosts'), 'download', g('openImportHostsModal'));
    add(t('pal_cmd_settings', 'Settings'), 'settings', g('openSettingsModal'));
    add(t('pal_cmd_docs', 'Documentation'), 'book-open', g('openDocumentationModal'));
    const nav = g('switchSideNav');
    if (nav) {
      add(t('pal_sec_hosts', 'Go to Hosts'), 'server', () => nav('hosts'));
      add(t('pal_sec_docker', 'Go to Docker'), 'container', () => nav('docker'));
      add(t('pal_sec_kube', 'Go to Kubernetes'), 'boxes', () => nav('kube'));
      add(t('pal_sec_cloudflare', 'Go to Cloudflare'), 'cloud', () => nav('cloudflare'));
      add(t('pal_sec_ansible', 'Go to Ansible'), 'ansible', () => nav('ansible'));
      add(t('pal_sec_terraform', 'Go to Terraform'), 'terraform', () => nav('terraform'));
    }
    if (window.PortFwd && typeof window.PortFwd.open === 'function') {
      add(t('pal_cmd_portfwd', 'Port forwards'), 'plug', () => window.PortFwd.open());
    }
    return cmds;
  }

  function buildItems() {
    const q = S.query.trim();
    const tk = tokens(q);
    if (S.mode === 'host' && S.host) {
      let acts = hostActions(S.host);
      if (tk.length) acts = acts.filter((a) => scoreFields(tk, [a.title]) >= 0);
      return acts;
    }
    const hosts = allHosts();
    const scored = [];
    hosts.forEach((h) => {
      const fields = [h.name || '', h.host || '', h.group_name || ''];
      const sc = tk.length ? scoreFields(tk, fields) : 1;
      if (sc >= 0) scored.push({ h: h, sc: sc });
    });
    scored.sort((a, b) => b.sc - a.sc || String(a.h.name).localeCompare(String(b.h.name)));

    const items = [];
    const recent = recentIds();
    if (!tk.length) {
      recent.forEach((id) => {
        const h = hosts.find((x) => String(x.id) === String(id));
        if (h) items.push(hostItem(h, 'recent'));
      });
    }
    scored.forEach((s) => {
      if (items.some((it) => it.kind === 'host' && String(it.host.id) === String(s.h.id))) return;
      items.push(hostItem(s.h, 'hosts'));
    });

    const cmds = globalCommands();
    if (q.length >= 2 && window.FleetSearch && typeof window.FleetSearch.open === 'function') {
      cmds.unshift({
        kind: 'command', group: 'commands',
        title: t('pal_cmd_fleet', "Search packages & containers across hosts for '{q}'").replace('{q}', q),
        sub: '', icon: 'search', run: () => window.FleetSearch.open(q)
      });
    }
    cmds.forEach((c) => {
      if (!tk.length) { items.push(c); return; }
      const sc = scoreFields(tk, [c.title]);
      if (sc >= 0) items.push(c);
    });
    return items.slice(0, MAX_ITEMS);
  }

  /* ---------- DOM ---------- */
  function ensureDom() {
    if (document.getElementById('paletteOverlay')) return;
    const ov = document.createElement('div');
    ov.id = 'paletteOverlay';
    ov.className = 'pal-overlay';
    ov.innerHTML = `
      <div class="pal-panel" role="dialog" aria-modal="true" aria-label="${esc(t('pal_ph', 'Search hosts and commands…'))}">
        <div class="pal-search">
          ${icon('search')}
          <input id="paletteInput" class="pal-input" type="text" autocomplete="off" spellcheck="false"
                 placeholder="${esc(t('pal_ph', 'Search hosts and commands…'))}"
                 role="combobox" aria-expanded="true" aria-controls="paletteList" aria-autocomplete="list">
          <span class="pal-kbd">Esc</span>
        </div>
        <div id="paletteList" class="pal-list" role="listbox" aria-label="${esc(t('pal_ph', 'Search hosts and commands…'))}"></div>
        <div class="pal-foot">${esc(t('pal_foot', '↑↓ navigate · Enter run · Esc close'))}</div>
      </div>`;
    document.body.appendChild(ov);

    const input = document.getElementById('paletteInput');
    const list = document.getElementById('paletteList');
    input.addEventListener('input', () => { S.query = input.value; S.idx = 0; render(); });
    input.addEventListener('keydown', onKey);
    ov.addEventListener('click', (e) => { if (e.target === ov) P.close(); });
    list.addEventListener('click', (e) => {
      const more = e.target.closest('[data-more]');
      if (more) {
        const it = S.items[Number(more.getAttribute('data-more'))];
        if (it && it.more) { S.mode = 'host'; S.host = it.host; S.query = ''; input.value = ''; S.idx = 0; render(); input.focus(); }
        return;
      }
      const row = e.target.closest('[data-index]');
      if (!row) return;
      const it = S.items[Number(row.getAttribute('data-index'))];
      runItem(it);
    });
    list.addEventListener('mousemove', (e) => {
      const row = e.target.closest('[data-index]');
      if (!row) return;
      const i = Number(row.getAttribute('data-index'));
      if (i !== S.idx) { S.idx = i; paintActive(); }
    });
  }

  const groupLabel = (grp) => grp === 'recent' ? t('pal_recent', 'Recent')
    : grp === 'hosts' ? t('pal_hosts', 'Hosts')
    : grp === 'commands' ? t('pal_commands', 'Commands')
    : t('pal_actions', 'Actions');

  function render() {
    const list = document.getElementById('paletteList');
    const input = document.getElementById('paletteInput');
    if (!list || !input) return;
    input.placeholder = t('pal_ph', 'Search hosts and commands…');
    S.items = buildItems();
    if (S.idx >= S.items.length) S.idx = Math.max(0, S.items.length - 1);
    if (!S.items.length) {
      list.innerHTML = `<div class="pal-empty">${esc(t('pal_empty', 'No matches'))}</div>`;
      input.removeAttribute('aria-activedescendant');
      return;
    }
    let html = '';
    let lastGroup = null;
    S.items.forEach((it, i) => {
      if (it.group !== lastGroup) {
        html += `<div class="pal-group" role="presentation">${esc(groupLabel(it.group))}</div>`;
        lastGroup = it.group;
      }
      html += `<div class="pal-item" role="option" id="palOpt${i}" aria-selected="${i === S.idx}" data-index="${i}">
        <span class="pal-item-icon">${icon(it.icon)}</span>
        <span class="pal-item-main">
          <span class="pal-item-title">${esc(it.title)}</span>
          ${it.sub ? `<span class="pal-item-sub">${esc(it.sub)}</span>` : ''}
        </span>
        ${it.more ? `<button type="button" class="pal-item-more" data-more="${i}" title="${esc(t('pal_more', 'More actions for this host'))}" aria-label="${esc(t('pal_more', 'More actions for this host'))}">${icon('chevron-right')}</button>` : ''}
      </div>`;
    });
    list.innerHTML = html;
    input.setAttribute('aria-activedescendant', 'palOpt' + S.idx);
    paintActive();
  }

  function paintActive() {
    const list = document.getElementById('paletteList');
    if (!list) return;
    list.querySelectorAll('.pal-item').forEach((el) => {
      const on = Number(el.getAttribute('data-index')) === S.idx;
      el.classList.toggle('is-active', on);
      el.setAttribute('aria-selected', on ? 'true' : 'false');
    });
    const act = list.querySelector('.pal-item.is-active');
    if (act && act.scrollIntoView) act.scrollIntoView({ block: 'nearest' });
  }

  function runItem(it) {
    if (!it || typeof it.run !== 'function') return;
    P.close();
    try { it.run(); } catch (e) { /* a failing action must not leave the palette open */ }
  }

  /* ---------- keyboard ---------- */
  function onKey(e) {
    const k = e.key;
    if (k === 'Escape') {
      e.preventDefault();
      if (S.mode === 'host') { backToRoot(); } else { P.close(); }
      return;
    }
    if (k === 'ArrowDown') {
      e.preventDefault();
      if (S.items.length) { S.idx = (S.idx + 1) % S.items.length; paintActive(); }
      return;
    }
    if (k === 'ArrowUp') {
      e.preventDefault();
      if (S.items.length) { S.idx = (S.idx - 1 + S.items.length) % S.items.length; paintActive(); }
      return;
    }
    if (k === 'ArrowRight') {
      const it = S.items[S.idx];
      if (it && it.more) {
        e.preventDefault();
        S.mode = 'host'; S.host = it.host; S.query = '';
        const input = document.getElementById('paletteInput');
        if (input) input.value = '';
        S.idx = 0; render(); input.focus();
      }
      return;
    }
    if (k === 'Backspace' && !S.query && S.mode === 'host') {
      e.preventDefault();
      backToRoot();
      return;
    }
    if (k === 'Enter') {
      e.preventDefault();
      runItem(S.items[S.idx]);
    }
  }

  function backToRoot() {
    S.mode = 'root';
    S.host = null;
    S.query = '';
    const input = document.getElementById('paletteInput');
    if (input) input.value = '';
    S.idx = 0;
    render();
    if (input) input.focus();
  }

  /* ---------- public API ---------- */
  P.open = function () {
    ensureDom();
    const ov = document.getElementById('paletteOverlay');
    if (!ov) return;
    if (!S.open) {
      S.prevFocus = document.activeElement;
      S.mode = 'root';
      S.host = null;
      S.query = '';
      S.idx = 0;
    }
    const input = document.getElementById('paletteInput');
    if (input) { input.value = ''; }
    S.query = '';
    S.open = true;
    ov.classList.add('open');
    render();
    if (input) input.focus();
  };

  P.close = function () {
    const ov = document.getElementById('paletteOverlay');
    if (ov) ov.classList.remove('open');
    S.open = false;
    const prev = S.prevFocus;
    S.prevFocus = null;
    if (prev && typeof prev.focus === 'function' && document.contains(prev)) prev.focus();
  };

  P.isOpen = function () { return S.open; };

  /* ---------- global shortcuts ---------- */
  document.addEventListener('keydown', (e) => {
    if (!(e.ctrlKey || e.metaKey) || (e.key !== 'k' && e.key !== 'K')) return;
    const tgt = e.target;
    // Ctrl+K is kill-line inside a shell; never steal it from the terminal or the code editor.
    if (tgt && tgt.closest && (tgt.closest('.xterm') || tgt.closest('.CodeMirror'))) return;
    e.preventDefault();
    if (S.open) { P.close(); } else { P.open(); }
  }, true);

  // Esc closes the palette even when the focus left the search box (the input handles its own).
  document.addEventListener('keydown', (e) => {
    if (e.key !== 'Escape' || !S.open) return;
    if (e.target && e.target.id === 'paletteInput') return;
    e.preventDefault();
    if (S.mode === 'host') backToRoot(); else P.close();
  }, true);
})();
