/* Ansible UI core: namespace, api, i18n, events, state, formatters, tab registry, shell, drawer, gate. */
(function () {
  'use strict';

  const Ans = (window.Ans = window.Ans || {});
  Ans.state = { status: null, playbooks: [], hosts: [], schedules: [], tab: 'playbooks' };

  /* ---------- i18n ---------- */
  Ans.addI18n = function (dict) {
    ['en', 'id'].forEach(function (lang) {
      if (dict && dict[lang] && typeof I18N !== 'undefined') {
        I18N[lang] = Object.assign(I18N[lang] || {}, dict[lang]);
      }
    });
  };
  Ans.t = function (key, fallback, vars) {
    let s = typeof t === 'function' ? t(key, fallback) : (fallback !== undefined ? fallback : key);
    if (vars) s = String(s).replace(/\{(\w+)\}/g, function (m, k) { return vars[k] !== undefined ? vars[k] : m; });
    return s;
  };
  Ans.esc = function (s) { return typeof escapeHtml === 'function' ? escapeHtml(String(s === null || s === undefined ? '' : s)) : String(s === null || s === undefined ? '' : s).replace(/[&<>"']/g, function (c) { return ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]; }); };
  Ans.icon = function (name, cls) { return '<svg class="icon' + (cls ? ' ' + cls : '') + '" aria-hidden="true"><use href="#i-' + name + '"/></svg>'; };
  Ans.toast = function (msg, type) { if (typeof showToast === 'function') showToast(msg, type || 'info'); };
  Ans.confirm = function (opts) {
    if (typeof showConfirm === 'function') return showConfirm(opts);
    return Promise.resolve(window.confirm((opts && opts.message) || ''));
  };

  /* ---------- events ---------- */
  const handlers = {};
  Ans.on = function (evt, fn) { (handlers[evt] = handlers[evt] || []).push(fn); return fn; };
  Ans.off = function (evt, fn) { handlers[evt] = (handlers[evt] || []).filter(function (f) { return f !== fn; }); };
  Ans.emit = function (evt, data) {
    (handlers[evt] || []).slice().forEach(function (fn) { try { fn(data); } catch (e) { console.error('Ans handler', evt, e); } });
  };

  /* ---------- api ---------- */
  class ApiError extends Error {
    constructor(status, message, data) { super(message); this.name = 'ApiError'; this.status = status; this.data = data; }
  }
  Ans.ApiError = ApiError;
  async function request(method, path, body, opts) {
    const init = { method: method, headers: {}, credentials: 'same-origin' };
    if (body !== undefined && body !== null) {
      if (body instanceof FormData || typeof body === 'string' || body instanceof Blob || body instanceof ArrayBuffer) {
        init.body = body;
      } else {
        init.headers['Content-Type'] = 'application/json';
        init.body = JSON.stringify(body);
      }
    }
    if (opts && opts.headers) Object.assign(init.headers, opts.headers);
    if (opts && opts.signal) init.signal = opts.signal;
    const res = await fetch(path, init);
    if (opts && opts.raw) return res;
    const text = await res.text();
    let data = null;
    if (text) { try { data = JSON.parse(text); } catch (e) { data = null; } }
    if (!res.ok) {
      const msg = (data && (data.error || data.message)) || text.trim() || ('HTTP ' + res.status);
      throw new ApiError(res.status, msg, data);
    }
    return data;
  }
  Ans.api = {
    get: function (p, o) { return request('GET', p, undefined, o); },
    post: function (p, b, o) { return request('POST', p, b, o); },
    put: function (p, b, o) { return request('PUT', p, b, o); },
    del: function (p, o) { return request('DELETE', p, undefined, o); },
  };

  /* ---------- formatters ---------- */
  Ans.fmt = {
    dur: function (ms) {
      if (ms === null || ms === undefined || ms < 0) return '-';
      const s = Math.round(ms / 1000);
      if (s < 60) return s + 's';
      const m = Math.floor(s / 60);
      if (m < 60) return m + 'm ' + (s % 60) + 's';
      return Math.floor(m / 60) + 'h ' + (m % 60) + 'm';
    },
    ago: function (iso) {
      if (!iso) return '-';
      const ts = new Date(iso).getTime();
      if (isNaN(ts)) return '-';
      let d = (Date.now() - ts) / 1000;
      const fut = d < 0;
      d = Math.abs(d);
      let v;
      if (d < 45) v = Ans.t('ans_time_now', 'just now');
      else if (d < 3600) v = Math.round(d / 60) + 'm';
      else if (d < 86400) v = Math.round(d / 3600) + 'h';
      else if (d < 86400 * 30) v = Math.round(d / 86400) + 'd';
      else return new Date(ts).toLocaleDateString();
      if (d < 45) return v;
      return fut ? Ans.t('ans_time_in', 'in {t}', { t: v }) : Ans.t('ans_time_ago', '{t} ago', { t: v });
    },
    dt: function (iso) { if (!iso) return '-'; const d = new Date(iso); return isNaN(d.getTime()) ? '-' : d.toLocaleString(); },
    bytes: function (n) {
      if (n === null || n === undefined) return '-';
      if (typeof formatBytes === 'function') return formatBytes(n);
      return n + ' B';
    },
    // recap = {ok,changed,unreachable,failed,skipped}; renders a compact 3-segment bar with a title tooltip
    pips: function (recap) {
      if (!recap) return '<span class="ans-muted">-</span>';
      const ok = recap.ok || 0, ch = recap.changed || 0, bad = (recap.failed || 0) + (recap.unreachable || 0);
      const tot = ok + ch + bad;
      if (!tot) return '<span class="ans-muted">-</span>';
      const w = function (n) { return (n / tot * 100).toFixed(1) + '%'; };
      const title = Ans.t('ans_recap_title', 'ok {ok} · changed {ch} · failed {bad}', { ok: ok, ch: ch, bad: bad });
      return '<span class="ans-pips" title="' + Ans.esc(title) + '">' +
        (ok ? '<i class="ans-pip ok" style="width:' + w(ok) + '"></i>' : '') +
        (ch ? '<i class="ans-pip changed" style="width:' + w(ch) + '"></i>' : '') +
        (bad ? '<i class="ans-pip bad" style="width:' + w(bad) + '"></i>' : '') + '</span>';
    },
  };

  /* ---------- small ui helpers ---------- */
  const STATUS = {
    success: ['badge-healthy', 'ans_status_success', 'Success'],
    running: ['badge-info', 'ans_status_running', 'Running'],
    queued: ['badge-warning', 'ans_status_queued', 'Queued'],
    failed: ['badge-danger', 'ans_status_failed', 'Failed'],
    cancelled: ['badge-unknown', 'ans_status_cancelled', 'Cancelled'],
    interrupted: ['badge-unknown', 'ans_status_interrupted', 'Interrupted'],
    never_run: ['badge-unknown', 'ans_status_never', 'Never run'],
  };
  Ans.ui = {
    statusClass: function (s) { return (STATUS[s] || STATUS.never_run)[0]; },
    statusLabel: function (s) { const e = STATUS[s] || STATUS.never_run; return Ans.t(e[1], e[2]); },
    badge: function (s) {
      const e = STATUS[s] || STATUS.never_run;
      return '<span class="badge ' + e[0] + '">' + Ans.esc(Ans.t(e[1], e[2])) + '</span>';
    },
    dot: function (s) { return '<span class="ans-dot ans-dot-' + Ans.esc(STATUS[s] ? s : 'never_run') + '" title="' + Ans.esc(Ans.ui.statusLabel(s)) + '"></span>'; },
    empty: function (o) {
      return '<div class="empty-state ans-empty">' + (o.icon ? '<div class="empty-state-icon">' + Ans.icon(o.icon, 'icon-xl') + '</div>' : '') +
        (o.title ? '<div class="empty-state-title">' + Ans.esc(o.title) + '</div>' : '') +
        (o.desc ? '<div class="empty-state-desc">' + Ans.esc(o.desc) + '</div>' : '') + (o.actionHtml || '') + '</div>';
    },
    skeleton: function (rows) {
      let h = '<div class="ans-skel" aria-busy="true">';
      for (let i = 0; i < (rows || 5); i++) h += '<div class="ans-skel-row"></div>';
      return h + '</div>';
    },
    error: function (msg, retryAttr) {
      return '<div class="ans-error" role="alert">' + Ans.icon('triangle-alert') + '<span>' + Ans.esc(msg) + '</span>' +
        (retryAttr ? '<button type="button" class="btn btn-sm" ' + retryAttr + '>' + Ans.esc(Ans.t('ans_retry', 'Retry')) + '</button>' : '') + '</div>';
    },
  };
  Ans.store = {
    get: function (k, d) { try { const v = localStorage.getItem('pantau_ans_' + k); return v === null ? d : JSON.parse(v); } catch (e) { return d; } },
    set: function (k, v) { try { localStorage.setItem('pantau_ans_' + k, JSON.stringify(v)); } catch (e) { /* quota */ } },
  };

  /* ---------- loaders ---------- */
  const inflight = {};
  function once(key, fn) {
    if (inflight[key]) return inflight[key];
    inflight[key] = fn().finally(function () { delete inflight[key]; });
    return inflight[key];
  }
  Ans.load = {
    status: function () {
      return once('status', async function () {
        try { Ans.state.status = await Ans.api.get('/api/ansible/status'); }
        catch (e) { Ans.state.status = { available: false, enabled: false, error: e.message }; }
        Ans.emit('status', Ans.state.status);
        return Ans.state.status;
      });
    },
    playbooks: function () {
      return once('playbooks', async function () {
        const d = await Ans.api.get('/api/ansible/playbooks');
        Ans.state.playbooks = (d && d.playbooks) || [];
        Ans.emit('playbooks', Ans.state.playbooks);
        return Ans.state.playbooks;
      });
    },
    hosts: function () {
      return once('hosts', async function () {
        const d = await Ans.api.get('/api/hosts');
        Ans.state.hosts = Array.isArray(d) ? d : [];
        Ans.emit('hosts', Ans.state.hosts);
        return Ans.state.hosts;
      });
    },
    schedules: function (playbookId) {
      const q = playbookId ? '?playbook_id=' + encodeURIComponent(playbookId) : '';
      return once('schedules' + q, async function () {
        const d = await Ans.api.get('/api/ansible/schedules' + q);
        const list = (d && d.schedules) || [];
        if (!playbookId) { Ans.state.schedules = list; Ans.emit('schedules', list); }
        return list;
      });
    },
  };

  /* ---------- visibility-aware interval ---------- */
  Ans.visible = function () {
    const el = document.getElementById('sectionAnsible');
    return !!el && el.style.display !== 'none' && !document.hidden;
  };
  Ans.every = function (fn, ms) {
    const id = setInterval(function () { if (Ans.visible()) fn(); }, ms);
    return function () { clearInterval(id); };
  };

  /* ---------- tab registry + shell ---------- */
  const tabs = [];
  const rendered = {};
  Ans.registerTab = function (tab) {
    const i = tabs.findIndex(function (x) { return x.id === tab.id; });
    if (i >= 0) tabs[i] = tab; else tabs.push(tab);
    tabs.sort(function (a, b) { return (a.order || 100) - (b.order || 100); });
    if (shellBuilt) buildShell();
  };
  const cap = function (s) { return s.charAt(0).toUpperCase() + s.slice(1); };
  let shellBuilt = false;

  Ans.addI18n({
    en: {
      ans_title: 'Ansible', ans_desc: 'Store, run and review agentless Ansible playbooks, with the inventory generated from Pantau hosts.',
      ans_refresh: 'Refresh', ans_retry: 'Retry', ans_close: 'Close', ans_back: 'Back', ans_expand: 'Expand', ans_collapse: 'Collapse',
      ans_time_now: 'just now', ans_time_ago: '{t} ago', ans_time_in: 'in {t}',
      ans_recap_title: 'ok {ok} · changed {ch} · failed {bad}',
      ans_status_success: 'Success', ans_status_running: 'Running', ans_status_queued: 'Queued', ans_status_failed: 'Failed',
      ans_status_cancelled: 'Cancelled', ans_status_interrupted: 'Interrupted', ans_status_never: 'Never run',
      ans_tab_playbooks: 'Playbooks', ans_tab_runs: 'Runs', ans_tab_hosts: 'Hosts', ans_tab_resources: 'Resources', ans_tab_settings: 'Settings',
      ans_gate_active: 'Ansible Gate active', ans_gate_off: 'Ansible Gate is off', ans_gate_missing: 'ansible-playbook not found',
      ans_gate_off_desc: 'Playbooks, runs, hosts and resources need the Ansible Gate, which an administrator must enable first. Playbooks run on the Pantau server with the privileges of the Pantau process.',
      ans_gate_missing_desc: 'Install ansible-core on the Pantau server (the official Docker image already includes it) and refresh.',
      ans_gate_enable: 'Enable Gate', ans_gate_disable: 'Disable Gate',
      ans_gate_confirm_title: 'Enable the Ansible Gate?',
      ans_gate_confirm_msg: 'Ansible Playbooks execute on the Pantau server with the privileges of the Pantau process. Tasks using connection: local, delegate_to: localhost or local plugins can read and write data on the master server. Only enable this if you trust everyone who can sign in to Pantau.',
      ans_gate_disable_title: 'Disable the Ansible Gate?', ans_gate_disable_msg: 'New runs, schedules and ad-hoc commands will be blocked.',
      ans_gate_failed: 'Could not change the Gate',
    },
    id: {
      ans_title: 'Ansible', ans_desc: 'Simpan, jalankan, dan tinjau playbook Ansible tanpa agen, dengan inventory yang dihasilkan dari Host Pantau.',
      ans_refresh: 'Muat ulang', ans_retry: 'Coba lagi', ans_close: 'Tutup', ans_back: 'Kembali', ans_expand: 'Perlebar', ans_collapse: 'Perkecil',
      ans_time_now: 'baru saja', ans_time_ago: '{t} lalu', ans_time_in: 'dalam {t}',
      ans_recap_title: 'ok {ok} · berubah {ch} · gagal {bad}',
      ans_status_success: 'Sukses', ans_status_running: 'Berjalan', ans_status_queued: 'Antre', ans_status_failed: 'Gagal',
      ans_status_cancelled: 'Dibatalkan', ans_status_interrupted: 'Terputus', ans_status_never: 'Belum pernah',
      ans_tab_playbooks: 'Playbook', ans_tab_runs: 'Run', ans_tab_hosts: 'Host', ans_tab_resources: 'Sumber Daya', ans_tab_settings: 'Pengaturan',
      ans_gate_active: 'Ansible Gate aktif', ans_gate_off: 'Ansible Gate nonaktif', ans_gate_missing: 'ansible-playbook tidak ditemukan',
      ans_gate_off_desc: 'Playbook, run, host, dan sumber daya memerlukan Ansible Gate yang harus diaktifkan administrator terlebih dahulu. Playbook berjalan di server Pantau dengan hak akses proses Pantau.',
      ans_gate_missing_desc: 'Pasang ansible-core di server Pantau (image Docker resmi sudah menyertakannya) lalu muat ulang.',
      ans_gate_enable: 'Aktifkan Gate', ans_gate_disable: 'Nonaktifkan Gate',
      ans_gate_confirm_title: 'Aktifkan Ansible Gate?',
      ans_gate_confirm_msg: 'Playbook Ansible dijalankan di server Pantau dengan hak akses proses Pantau. Task yang memakai connection: local, delegate_to: localhost, atau plugin lokal dapat membaca dan menulis data di server master. Aktifkan hanya jika Anda memercayai semua orang yang dapat masuk ke Pantau.',
      ans_gate_disable_title: 'Nonaktifkan Ansible Gate?', ans_gate_disable_msg: 'Run baru, jadwal, dan perintah ad-hoc akan diblokir.',
      ans_gate_failed: 'Gagal mengubah Gate',
    },
  });

  function root() { return document.getElementById('sectionAnsible'); }

  Ans.blocked = function () { const s = Ans.state.status; return !!s && (!s.available || !s.enabled); };
  function blockedHtml() {
    const s = Ans.state.status || {};
    const missing = !s.available;
    return '<div id="ansibleGateBanner" class="ans-blocked">' + Ans.icon(missing ? 'triangle-alert' : 'shield-alert', 'icon-xl') +
      '<div class="ans-gate-title" id="ansibleGateTitle">' + Ans.esc(missing ? Ans.t('ans_gate_missing', 'ansible-playbook not found') : Ans.t('ans_gate_off', 'Ansible Gate is off')) + '</div>' +
      '<div class="ans-gate-desc" id="ansibleGateDesc">' + Ans.esc(missing ? (s.error || Ans.t('ans_gate_missing_desc', '')) : Ans.t('ans_gate_off_desc', '')) + '</div>' +
      '<div class="ans-row">' + (missing ? '' : '<button type="button" id="ansibleGateAction" class="btn btn-sm btn-primary" data-ans="gate-toggle">' + Ans.esc(Ans.t('ans_gate_enable', 'Enable Gate')) + '</button>') +
      '<button type="button" class="btn btn-sm" data-ans="goto-settings">' + Ans.esc(Ans.t('ans_tab_settings', 'Settings')) + '</button></div></div>';
  }

  function gateButtonHtml() {
    const s = Ans.state.status;
    let cls = 'badge-unknown', label = '…';
    if (s) {
      if (!s.available) { cls = 'badge-danger'; label = Ans.t('ans_gate_missing', 'ansible-playbook not found'); }
      else if (!s.enabled) { cls = 'badge-warning'; label = Ans.t('ans_gate_off', 'Ansible Gate is off'); }
      else { cls = 'badge-healthy'; label = Ans.t('ans_gate_active', 'Ansible Gate active'); }
    }
    return '<button type="button" class="btn btn-sm header-btn" data-ans="goto-settings" title="' + Ans.esc(s && s.version ? s.version : '') + '">' +
      '<span id="ansibleGateStatus" class="badge ' + cls + '" role="status">' + Ans.esc(label) + '</span></button>';
  }

  function buildShell() {
    const el = root();
    if (!el) return;
    shellBuilt = true;
    const active = Ans.state.tab;
    let tablist = '', panels = '';
    tabs.forEach(function (tb) {
      const C = cap(tb.id), on = tb.id === active;
      tablist += '<button type="button" class="tab' + (on ? ' active' : '') + '" id="tabAnsible' + C + '" data-tab="' + tb.id + '" role="tab" aria-selected="' + on +
        '" aria-controls="ansiblePanel' + C + '"' + (on ? '' : ' tabindex="-1"') + '>' + Ans.icon(tb.icon || 'ansible', 'icon-sm') + ' <span>' + Ans.esc(Ans.t(tb.labelKey, tb.id)) + '</span></button>';
      panels += '<div id="ansiblePanel' + C + '" class="ans-panel" role="tabpanel" aria-labelledby="tabAnsible' + C + '" tabindex="0"' + (on ? '' : ' hidden') + '></div>';
    });
    el.innerHTML =
      '<div class="section-header ans-head"><div><h2>' + Ans.esc(Ans.t('ans_title', 'Ansible')) + '</h2><p>' + Ans.esc(Ans.t('ans_desc', '')) + '</p></div>' +
      '<div class="ans-head-actions">' + gateButtonHtml() +
      '<button type="button" id="btnAnsibleRefresh" class="btn btn-sm header-btn" data-ans="refresh">' + Ans.icon('refresh-cw', 'icon-sm') + ' <span>' + Ans.esc(Ans.t('ans_refresh', 'Refresh')) + '</span></button></div></div>' +
      '<div id="ansibleTabs" class="tabs ans-tabs" role="tablist" aria-label="Ansible">' + tablist + '</div>' + panels;
    Object.keys(rendered).forEach(function (k) { delete rendered[k]; });
    showTab(active, true);
  }

  // The API answers 403 for every data endpoint while the Gate is off, so those tabs show an explanation instead.
  function showTab(id, force) {
    const tb = tabs.find(function (x) { return x.id === id; }) || tabs[0];
    if (!tb) return;
    const prev = tabs.find(function (x) { return x.id === Ans.state.tab; });
    if (prev && prev.id !== tb.id && prev.onHide) { try { prev.onHide(); } catch (e) { console.error(e); } }
    Ans.state.tab = tb.id;
    tabs.forEach(function (x) {
      const C = cap(x.id), on = x.id === tb.id;
      const b = document.getElementById('tabAnsible' + C), p = document.getElementById('ansiblePanel' + C);
      if (b) { b.classList.toggle('active', on); b.setAttribute('aria-selected', String(on)); b.tabIndex = on ? 0 : -1; }
      if (p) p.hidden = !on;
    });
    let panel = document.getElementById('ansiblePanel' + cap(tb.id));
    if (!panel) return;
    if (Ans.blocked() && tb.id !== 'settings') {
      if (rendered[tb.id] || force) { const fresh = panel.cloneNode(false); panel.replaceWith(fresh); panel = fresh; } // drops the module's listeners
      rendered[tb.id] = false;
      panel.innerHTML = blockedHtml();
      return;
    }
    if (force && rendered[tb.id] === false && panel.querySelector('#ansibleGateBanner')) panel.innerHTML = '';
    if (!rendered[tb.id]) { rendered[tb.id] = true; try { tb.render(panel); } catch (e) { console.error(e); panel.innerHTML = Ans.ui.error(e.message); } }
    if (tb.onShow) { try { tb.onShow(panel); } catch (e) { console.error(e); } }
  }
  Ans.showTab = showTab;

  let lastBlocked = null;
  function refreshGate() {
    const st = document.getElementById('ansibleGateStatus');
    if (st) st.outerHTML = gateButtonHtml().match(/<span id="ansibleGateStatus"[\s\S]*?<\/span>/)[0];
    const b = Ans.blocked();
    if (b !== lastBlocked) { lastBlocked = b; if (shellBuilt && Ans.state.status) showTab(Ans.state.tab, true); }
  }
  Ans.toggleGate = async function () {
    const s = Ans.state.status || {};
    const turnOn = !s.enabled;
    const ok = await Ans.confirm(turnOn
      ? { title: Ans.t('ans_gate_confirm_title'), message: Ans.t('ans_gate_confirm_msg'), confirmText: Ans.t('ans_gate_enable'), danger: true }
      : { title: Ans.t('ans_gate_disable_title'), message: Ans.t('ans_gate_disable_msg'), confirmText: Ans.t('ans_gate_disable'), danger: false });
    if (!ok) return;
    try {
      Ans.state.status = await Ans.api.put('/api/ansible/status', { enabled: turnOn });
      Ans.emit('status', Ans.state.status);
    } catch (e) { Ans.toast(Ans.t('ans_gate_failed', 'Could not change the Gate') + ': ' + e.message, 'error'); }
  };
  Ans.on('status', refreshGate);

  function onShellClick(e) {
    const a = e.target.closest('[data-ans]');
    if (a && root() && root().contains(a)) {
      const act = a.getAttribute('data-ans');
      if (act === 'refresh') Ans.refresh();
      else if (act === 'gate-toggle') Ans.toggleGate();
      else if (act === 'goto-settings') showTab('settings');
      return;
    }
    const tb = e.target.closest('#ansibleTabs .tab');
    if (tb) showTab(tb.getAttribute('data-tab'));
  }
  function onShellKey(e) {
    const tb = e.target.closest('#ansibleTabs .tab');
    if (!tb) return;
    const btns = Array.prototype.slice.call(document.querySelectorAll('#ansibleTabs .tab'));
    let i = btns.indexOf(tb);
    if (e.key === 'ArrowRight') i = (i + 1) % btns.length;
    else if (e.key === 'ArrowLeft') i = (i - 1 + btns.length) % btns.length;
    else if (e.key === 'Home') i = 0;
    else if (e.key === 'End') i = btns.length - 1;
    else return;
    e.preventDefault();
    btns[i].focus();
    showTab(btns[i].getAttribute('data-tab'));
  }

  Ans.refresh = async function () {
    await Ans.load.status(); // know the Gate state before any tab renders, so a blocked tab never fires its API calls
    if (!shellBuilt) {
      const el = root();
      if (!el) return;
      el.addEventListener('click', onShellClick);
      el.addEventListener('keydown', onShellKey);
      buildShell();
      lastBlocked = Ans.blocked();
      return;
    }
    const tb = tabs.find(function (x) { return x.id === Ans.state.tab; });
    if (tb && !Ans.blocked() && tb.onShow) { try { await tb.onShow(document.getElementById('ansiblePanel' + cap(tb.id)), { refresh: true }); } catch (e) { console.error(e); } }
  };
  Ans.rerender = function () {
    if (!shellBuilt) return;
    buildShell();
    refreshGate();
    Ans.emit('lang');
  };
  // legacy entry points used by the host page (side-nav)
  window.refreshAnsibleSection = function () { return Ans.refresh(); };
  if (typeof langRerenderHooks !== 'undefined') langRerenderHooks.push(Ans.rerender);

  /* ---------- drawer ---------- */
  let drawerEl = null, stack = [], lastFocus = null;
  function ensureDrawer() {
    if (drawerEl) return drawerEl;
    drawerEl = document.createElement('div');
    drawerEl.id = 'ansDrawerRoot';
    drawerEl.className = 'ans-drawer-root';
    drawerEl.hidden = true;
    drawerEl.innerHTML =
      '<div class="ans-drawer-backdrop" data-d="close"></div>' +
      '<aside class="ans-drawer" role="dialog" aria-modal="true" aria-labelledby="ansDrawerTitle" tabindex="-1">' +
      '<div class="ans-drawer-head"><button type="button" class="btn btn-sm btn-ghost ans-drawer-back" data-d="back" hidden></button>' +
      '<div class="ans-drawer-titles"><h3 id="ansDrawerTitle"></h3><div class="ans-drawer-sub"></div></div>' +
      '<div class="ans-drawer-actions"></div>' +
      '<button type="button" class="btn btn-sm btn-ghost ans-drawer-wide" data-d="wide"></button>' +
      '<button type="button" class="btn btn-sm btn-ghost ans-drawer-x" data-d="close"></button></div>' +
      '<div class="ans-drawer-body"></div></aside>';
    document.body.appendChild(drawerEl);
    drawerEl.addEventListener('click', function (e) {
      const d = e.target.closest('[data-d]');
      if (!d) return;
      const act = d.getAttribute('data-d');
      if (act === 'close') Ans.drawer.close();
      else if (act === 'back') Ans.drawer.back();
      else if (act === 'wide') { const top = stack[stack.length - 1]; if (top) setWide(!top.wide); }
    });
    drawerEl.addEventListener('keydown', function (e) {
      if (e.key === 'Escape' && !e.defaultPrevented) { e.stopPropagation(); Ans.drawer.close(); }
      else if (e.key === 'Tab') trapFocus(e);
    });
    return drawerEl;
  }
  function trapFocus(e) {
    const f = drawerEl.querySelectorAll('a[href],button:not([disabled]):not([hidden]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])');
    const vis = Array.prototype.filter.call(f, function (n) { return n.offsetParent !== null; });
    if (!vis.length) return;
    const first = vis[0], last = vis[vis.length - 1];
    if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
    else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
  }
  function setWide(on) {
    const top = stack[stack.length - 1];
    if (top) top.wide = !!on;
    const a = drawerEl.querySelector('.ans-drawer');
    a.classList.toggle('wide', !!on);
    const w = drawerEl.querySelector('.ans-drawer-wide');
    w.innerHTML = Ans.icon(on ? 'minimize-2' : 'maximize-2', 'icon-sm');
    w.title = on ? Ans.t('ans_collapse', 'Collapse') : Ans.t('ans_expand', 'Expand');
  }
  function mountTop() {
    const top = stack[stack.length - 1];
    const el = ensureDrawer();
    const body = el.querySelector('.ans-drawer-body');
    body.innerHTML = '';
    el.querySelector('.ans-drawer-actions').innerHTML = '';
    el.querySelector('#ansDrawerTitle').textContent = top.title || '';
    el.querySelector('.ans-drawer-sub').innerHTML = top.subtitle || '';
    const back = el.querySelector('.ans-drawer-back');
    back.hidden = stack.length < 2;
    back.innerHTML = Ans.icon('chevron-left', 'icon-sm') + ' <span>' + Ans.esc(Ans.t('ans_back', 'Back')) + '</span>';
    el.querySelector('.ans-drawer-x').innerHTML = Ans.icon('x', 'icon-sm');
    el.querySelector('.ans-drawer-x').title = Ans.t('ans_close', 'Close');
    setWide(top.wide);
    const ctrl = {
      setTitle: function (s) { if (top === stack[stack.length - 1]) el.querySelector('#ansDrawerTitle').textContent = s; top.title = s; },
      setSubtitle: function (h) { if (top === stack[stack.length - 1]) el.querySelector('.ans-drawer-sub').innerHTML = h; top.subtitle = h; },
      setActions: function (h) {
        const box = el.querySelector('.ans-drawer-actions');
        if (typeof h === 'string') box.innerHTML = h; else { box.innerHTML = ''; if (h) box.appendChild(h); }
      },
      setWide: setWide,
      push: Ans.drawer.push, back: Ans.drawer.back, close: Ans.drawer.close,
    };
    top.ctrl = ctrl;
    if (top.actions) ctrl.setActions(top.actions);
    top.mount(body, ctrl);
  }
  async function canClose(view) {
    if (!view || !view.canClose) return true;
    try { return (await view.canClose()) !== false; } catch (e) { return true; }
  }
  function unmountTop() {
    const top = stack[stack.length - 1];
    if (top && top.unmount) { try { top.unmount(); } catch (e) { console.error(e); } }
  }
  Ans.drawer = {
    current: function () { return stack[stack.length - 1] || null; },
    open: async function (view) {
      if (stack.length) { for (let i = stack.length - 1; i >= 0; i--) if (!(await canClose(stack[i]))) return false; unmountTop(); }
      stack = [view];
      const el = ensureDrawer();
      if (el.hidden) { lastFocus = document.activeElement; el.hidden = false; requestAnimationFrame(function () { el.classList.add('open'); }); }
      mountTop();
      const a = el.querySelector('.ans-drawer'); if (a) a.focus();
      return true;
    },
    push: async function (view) {
      if (!stack.length) return Ans.drawer.open(view);
      unmountTop();
      stack.push(view);
      mountTop();
      return true;
    },
    back: async function () {
      if (stack.length < 2) return;
      if (!(await canClose(stack[stack.length - 1]))) return;
      unmountTop();
      stack.pop();
      mountTop();
    },
    close: async function () {
      if (!stack.length) return true;
      for (let i = stack.length - 1; i >= 0; i--) if (!(await canClose(stack[i]))) return false;
      unmountTop();
      stack = [];
      const el = ensureDrawer();
      el.classList.remove('open');
      el.hidden = true;
      el.querySelector('.ans-drawer-body').innerHTML = '';
      if (lastFocus && lastFocus.focus) { try { lastFocus.focus(); } catch (e) { /* detached */ } }
      lastFocus = null;
      return true;
    },
  };
  Ans.on('lang', function () { if (stack.length) { const top = stack[stack.length - 1]; const el = ensureDrawer(); el.querySelector('.ans-drawer-back').innerHTML = Ans.icon('chevron-left', 'icon-sm') + ' <span>' + Ans.esc(Ans.t('ans_back', 'Back')) + '</span>'; setWide(top.wide); } });
})();

/* ---------- host picker (shared by launch, schedules, ad-hoc) ---------- */
(function () {
  'use strict';
  const Ans = window.Ans;
  Ans.addI18n({
    en: { ans_hp_search: 'Search hosts…', ans_hp_all_groups: 'All groups', ans_hp_select_all: 'Select all', ans_hp_clear: 'Clear', ans_hp_none: 'No hosts match', ans_hp_ungrouped: 'Ungrouped', ans_hp_count: '{n} / {max} selected', ans_hp_limit: 'Limit of {max} hosts reached' },
    id: { ans_hp_search: 'Cari host…', ans_hp_all_groups: 'Semua grup', ans_hp_select_all: 'Pilih semua', ans_hp_clear: 'Bersihkan', ans_hp_none: 'Tidak ada host yang cocok', ans_hp_ungrouped: 'Tanpa grup', ans_hp_count: '{n} / {max} dipilih', ans_hp_limit: 'Batas {max} host tercapai' },
  });
  // opts: {ids: number[], max: 200, onChange(ids), compact}
  Ans.hostPicker = {
    mount: function (container, opts) {
      opts = opts || {};
      const max = opts.max || 200;
      const sel = new Set((opts.ids || []).map(Number));
      let q = '', group = '';
      container.classList.add('ans-hp');
      container.innerHTML =
        '<div class="ans-hp-chips" data-hp="chips"></div>' +
        '<div class="ans-row"><div class="search-box" style="margin:0;flex:1;min-width:160px;"><input type="text" class="form-input" data-hp="q" placeholder="' + Ans.esc(Ans.t('ans_hp_search')) + '" style="height:30px;font-size:var(--fs-xs);" /></div>' +
        '<button type="button" class="btn btn-sm" data-hp="all">' + Ans.esc(Ans.t('ans_hp_select_all')) + '</button>' +
        '<button type="button" class="btn btn-sm btn-ghost" data-hp="clear">' + Ans.esc(Ans.t('ans_hp_clear')) + '</button>' +
        '<span class="ans-mono ans-muted" data-hp="count"></span></div>' +
        '<div class="ans-hp-list" data-hp="list" role="group"></div>';
      const $ = function (k) { return container.querySelector('[data-hp="' + k + '"]'); };
      const hosts = function () { return Ans.state.hosts || []; };
      const gname = function (h) { return (h.group_name || '').trim(); };
      const filtered = function () {
        const needle = q.trim().toLowerCase();
        return hosts().filter(function (h) {
          if (group === '__none__' ? gname(h) : (group && gname(h) !== group)) return false;
          return !needle || (h.name + ' ' + h.host).toLowerCase().indexOf(needle) >= 0;
        });
      };
      function emit() { if (opts.onChange) opts.onChange(Array.from(sel)); }
      function draw() {
        const groups = {};
        hosts().forEach(function (h) { const g = gname(h); groups[g] = (groups[g] || 0) + 1; });
        let chips = '<button type="button" class="ans-chip' + (!group ? ' active' : '') + '" data-g="">' + Ans.esc(Ans.t('ans_hp_all_groups')) + '</button>';
        Object.keys(groups).sort().forEach(function (g) {
          const key = g || '__none__';
          chips += '<button type="button" class="ans-chip' + (group === key ? ' active' : '') + '" data-g="' + Ans.esc(key) + '">' + Ans.esc(g || Ans.t('ans_hp_ungrouped')) + ' <span class="count">' + groups[g] + '</span></button>';
        });
        $('chips').innerHTML = chips;
        const list = filtered();
        $('list').innerHTML = list.length ? list.map(function (h) {
          const on = sel.has(h.id);
          return '<label class="ans-hp-item' + (on ? ' on' : '') + '"><input type="checkbox" data-id="' + h.id + '"' + (on ? ' checked' : '') + ' />' +
            '<span class="ans-hp-name">' + Ans.esc(h.name) + '</span><span class="ans-mono ans-muted">' + Ans.esc(h.host) + '</span></label>';
        }).join('') : '<div class="ans-muted ans-hint" style="padding:8px;">' + Ans.esc(Ans.t('ans_hp_none')) + '</div>';
        $('count').textContent = Ans.t('ans_hp_count', '{n} / {max} selected', { n: sel.size, max: max });
      }
      function toggle(id, on) {
        if (on) { if (sel.size >= max) { Ans.toast(Ans.t('ans_hp_limit', 'Limit of {max} hosts reached', { max: max }), 'warning'); return false; } sel.add(id); }
        else sel.delete(id);
        return true;
      }
      container.addEventListener('click', function (e) {
        const g = e.target.closest('[data-g]');
        if (g && container.contains(g)) {
          const key = g.getAttribute('data-g');
          if (!key) { group = ''; }
          else {
            // chip click toggles every member of the group (like the old launch dialog), and also filters the list
            const members = hosts().filter(function (h) { return key === '__none__' ? !gname(h) : gname(h) === key; });
            const allOn = members.length && members.every(function (h) { return sel.has(h.id); });
            members.forEach(function (h) { if (allOn) sel.delete(h.id); else if (sel.size < max) sel.add(h.id); });
            group = key; emit();
          }
          draw(); return;
        }
        const b = e.target.closest('[data-hp]');
        if (!b || !container.contains(b)) return;
        const k = b.getAttribute('data-hp');
        if (k === 'all') { filtered().forEach(function (h) { if (sel.size < max) sel.add(h.id); }); emit(); draw(); }
        else if (k === 'clear') { sel.clear(); emit(); draw(); }
      });
      container.addEventListener('change', function (e) {
        const c = e.target.closest('input[data-id]');
        if (!c) return;
        if (!toggle(Number(c.getAttribute('data-id')), c.checked)) c.checked = false;
        c.parentNode.classList.toggle('on', c.checked);
        $('count').textContent = Ans.t('ans_hp_count', '{n} / {max} selected', { n: sel.size, max: max });
        emit();
      });
      container.addEventListener('input', function (e) { if (e.target.matches('[data-hp="q"]')) { q = e.target.value; const l = filtered(); $('list').innerHTML = ''; draw(); const qi = $('q'); qi.focus(); } });
      const onHosts = Ans.on('hosts', draw);
      Ans.load.hosts().then(draw).catch(function () { draw(); });
      draw();
      return {
        getIds: function () { return Array.from(sel); },
        setIds: function (ids) { sel.clear(); (ids || []).forEach(function (i) { sel.add(Number(i)); }); draw(); },
        destroy: function () { Ans.off('hosts', onHosts); container.innerHTML = ''; },
      };
    },
  };
})();
