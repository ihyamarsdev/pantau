/* Terraform UI core: namespace window.Tf, api, i18n, events, state, formatters, ui helpers,
   top-level tab registry, shell (header + Gate pill + tabs), drawer stack, dialog, polling.
   Feature modules (10..70) attach their own sub-namespace (Tf.workspaces, Tf.runs, ...) and register tabs. */
(function () {
  'use strict';

  const Tf = (window.Tf = window.Tf || {});
  Tf.state = { status: null, workspaces: [], varsets: [], cloud: [], policies: [], modules: [], tab: 'workspaces', sub: {}, ws: null };

  /* ---------- i18n (EN + ID dictionaries; every tf_* key needs both) ---------- */
  Tf.addI18n = function (dict) {
    ['en', 'id'].forEach(function (lang) {
      if (dict && dict[lang] && typeof I18N !== 'undefined') I18N[lang] = Object.assign(I18N[lang] || {}, dict[lang]);
    });
  };
  Tf.t = function (key, fallback, vars) {
    let s = typeof t === 'function' ? t(key, fallback) : (fallback !== undefined ? fallback : key);
    if (s === key && fallback !== undefined) s = fallback;
    if (vars) s = String(s).replace(/\{(\w+)\}/g, function (m, k) { return vars[k] !== undefined ? vars[k] : m; });
    return s;
  };
  Tf.esc = function (s) {
    return String(s === null || s === undefined ? '' : s).replace(/[&<>"']/g, function (c) {
      return ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c];
    });
  };
  Tf.icon = function (name, cls) { return '<svg class="icon' + (cls ? ' ' + cls : '') + '" aria-hidden="true"><use href="#i-' + name + '"/></svg>'; };
  Tf.toast = function (msg, type) { if (typeof showToast === 'function') showToast(msg, type || 'info'); };
  Tf.confirm = function (opts) {
    if (typeof showConfirm === 'function') return showConfirm(opts);
    return Promise.resolve(window.confirm((opts && opts.message) || ''));
  };
  Tf.copy = function (text) {
    if (navigator.clipboard && navigator.clipboard.writeText) return navigator.clipboard.writeText(text).then(function () { Tf.toast(Tf.t('tf_copied', 'Copied'), 'success'); }, function () { /* ignored */ });
    return Promise.resolve();
  };
  Tf.csv = function (s) { return String(s || '').split(',').map(function (x) { return x.trim(); }).filter(Boolean); };
  Tf.debounce = function (fn, ms) { let h = null; return function () { const a = arguments, self = this; clearTimeout(h); h = setTimeout(function () { fn.apply(self, a); }, ms || 200); }; };

  /* ---------- events ---------- */
  const handlers = {};
  Tf.on = function (evt, fn) { (handlers[evt] = handlers[evt] || []).push(fn); return fn; };
  Tf.off = function (evt, fn) { handlers[evt] = (handlers[evt] || []).filter(function (f) { return f !== fn; }); };
  Tf.emit = function (evt, data) {
    (handlers[evt] || []).slice().forEach(function (fn) { try { fn(data); } catch (e) { console.error('Tf handler', evt, e); } });
  };

  /* ---------- api ---------- */
  class ApiError extends Error {
    constructor(status, message, data) { super(message); this.name = 'ApiError'; this.status = status; this.data = data; }
  }
  Tf.ApiError = ApiError;
  async function request(method, path, body, opts) {
    const init = { method: method, headers: {}, credentials: 'same-origin' };
    if (body !== undefined && body !== null) {
      if (body instanceof FormData || typeof body === 'string' || body instanceof Blob) init.body = body;
      else { init.headers['Content-Type'] = 'application/json'; init.body = JSON.stringify(body); }
    }
    if (opts && opts.signal) init.signal = opts.signal;
    const res = await fetch(path, init);
    const text = await res.text();
    let data = null;
    if (text) { try { data = JSON.parse(text); } catch (e) { data = null; } }
    if (!res.ok) {
      const msg = (data && (data.error || data.message)) || text.trim() || ('HTTP ' + res.status);
      throw new ApiError(res.status, msg, data);
    }
    return data;
  }
  Tf.api = {
    get: function (p, o) { return request('GET', p, undefined, o); },
    post: function (p, b, o) { return request('POST', p, b, o); },
    put: function (p, b, o) { return request('PUT', p, b, o); },
    del: function (p, o) { return request('DELETE', p, undefined, o); },
  };
  // Human-readable message for a failed request.
  Tf.errMsg = function (e) { return (e && e.message) || Tf.t('tf_err_generic', 'Request failed'); };

  /* ---------- formatters ---------- */
  Tf.fmt = {
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
      if (d < 45) return Tf.t('tf_time_now', 'just now');
      let v;
      if (d < 3600) v = Math.round(d / 60) + 'm';
      else if (d < 86400) v = Math.round(d / 3600) + 'h';
      else if (d < 86400 * 30) v = Math.round(d / 86400) + 'd';
      else return new Date(ts).toLocaleDateString();
      return fut ? Tf.t('tf_time_in', 'in {t}', { t: v }) : Tf.t('tf_time_ago', '{t} ago', { t: v });
    },
    dt: function (iso) { if (!iso) return '-'; const d = new Date(iso); return isNaN(d.getTime()) ? '-' : d.toLocaleString(); },
    bytes: function (n) {
      if (n === null || n === undefined) return '-';
      if (typeof formatBytes === 'function') return formatBytes(n);
      return n + ' B';
    },
    // counts = {create, update, delete, replace} -> "+1 ~2 -0 -/+1"
    counts: function (c) {
      c = c || {};
      return '+' + (c.create || 0) + ' ~' + (c.update || 0) + ' -' + (c.delete || 0) + ' -/+' + (c.replace || 0);
    },
  };

  /* ---------- ui helpers ---------- */
  // Run statuses (see store.TFRun.Status). Active runs pulse.
  const RUN_STATUS = {
    queued: ['badge-warning', 'tf_rs_queued', 'Queued'],
    planning: ['badge-info', 'tf_rs_planning', 'Planning'],
    awaiting_approval: ['badge-warning', 'tf_rs_awaiting', 'Awaiting approval'],
    applying: ['badge-info', 'tf_rs_applying', 'Applying'],
    applied: ['badge-healthy', 'tf_rs_applied', 'Applied'],
    no_changes: ['badge-healthy', 'tf_rs_nochanges', 'No changes'],
    planned: ['badge-info', 'tf_rs_planned', 'Planned'],
    policy_failed: ['badge-danger', 'tf_rs_policyfailed', 'Policy failed'],
    errored: ['badge-danger', 'tf_rs_errored', 'Errored'],
    discarded: ['badge-unknown', 'tf_rs_discarded', 'Discarded'],
    canceled: ['badge-unknown', 'tf_rs_canceled', 'Canceled'],
    interrupted: ['badge-unknown', 'tf_rs_interrupted', 'Interrupted'],
  };
  Tf.ACTIVE_RUN = ['queued', 'planning', 'applying'];
  Tf.ui = {
    runStatusClass: function (s) { return (RUN_STATUS[s] || ['badge-unknown'])[0]; },
    runStatusLabel: function (s) { const e = RUN_STATUS[s]; return e ? Tf.t(e[1], e[2]) : (s || '-'); },
    badge: function (s) {
      return '<span class="badge ' + Tf.ui.runStatusClass(s) + '">' + Tf.esc(Tf.ui.runStatusLabel(s)) + '</span>';
    },
    dot: function (s) {
      const active = Tf.ACTIVE_RUN.indexOf(s) >= 0 ? ' tf-dot-live' : '';
      return '<span class="tf-dot tf-dot-' + Tf.esc(RUN_STATUS[s] ? s : 'unknown') + active + '" title="' + Tf.esc(Tf.ui.runStatusLabel(s)) + '"></span>';
    },
    empty: function (o) {
      return '<div class="empty-state tf-empty">' + (o.icon ? '<div class="empty-state-icon">' + Tf.icon(o.icon, 'icon-xl') + '</div>' : '') +
        (o.title ? '<div class="empty-state-title">' + Tf.esc(o.title) + '</div>' : '') +
        (o.desc ? '<div class="empty-state-desc">' + Tf.esc(o.desc) + '</div>' : '') + (o.actionHtml || '') + '</div>';
    },
    skeleton: function (rows) {
      let h = '<div class="tf-skeleton" aria-busy="true">';
      for (let i = 0; i < (rows || 3); i++) h += '<div class="tf-skel-row"></div>';
      return h + '</div>';
    },
    error: function (msg, retryAttr) {
      return '<div class="tf-error" role="alert">' + Tf.icon('triangle-alert', 'icon-sm') + ' <span>' + Tf.esc(msg || Tf.t('tf_err_generic', 'Request failed')) + '</span>' +
        (retryAttr ? ' <button type="button" class="btn btn-sm" ' + retryAttr + '>' + Tf.esc(Tf.t('tf_retry', 'Retry')) + '</button>' : '') + '</div>';
    },
    // Gate-off card used by every data tab while the Terraform Gate is off or unavailable.
    gateOff: function (desc) {
      const s = Tf.state.status || {};
      const missing = s.supported === false;
      return '<div class="tf-gate-off" id="tfGateBanner">' + Tf.icon(missing ? 'triangle-alert' : 'shield-alert', 'icon-xl') +
        '<div class="tf-gate-title">' + Tf.esc(missing ? Tf.t('tf_gate_unavailable', 'Terraform unavailable') : Tf.t('tf_gate_off_title', 'Terraform Gate is off')) + '</div>' +
        '<div class="tf-gate-desc">' + Tf.esc(desc || Tf.t('tf_gate_off_desc', 'Workspaces, runs and state need the Terraform Gate, which an administrator must enable first.')) + '</div>' +
        '<div class="tf-row"><button type="button" class="btn btn-sm btn-primary" data-tf="goto-gate">' + Tf.esc(Tf.t('tf_gate_open_settings', 'Open Gate Settings')) + '</button></div></div>';
    },
  };

  /* ---------- gate state ---------- */
  Tf.gateOn = function () { const s = Tf.state.status; return !!(s && s.enabled && s.supported !== false); };
  Tf.load = {
    status: async function () {
      try { Tf.state.status = await Tf.api.get('/api/terraform/status'); }
      catch (e) { Tf.state.status = { enabled: false, supported: false, binaries: [], error: Tf.errMsg(e) }; }
      Tf.emit('status', Tf.state.status);
      return Tf.state.status;
    },
  };

  /* ---------- top-level tabs ---------- */
  // registerTab({ id, icon, labelKey, fallback, order, alwaysRender, render(panel), onShow(panel, opts), onHide() })
  const tabs = [];
  let shellBuilt = false;
  const rendered = {};
  Tf.registerTab = function (tb) { tabs.push(tb); tabs.sort(function (a, b) { return (a.order || 0) - (b.order || 0); }); return tb; };
  function root() { return document.getElementById('sectionTerraform'); }
  function cap(id) { return id.charAt(0).toUpperCase() + id.slice(1); }
  function tabLabel(tb) { return Tf.t(tb.labelKey, tb.fallback || tb.id); }

  function gateBtnHtml() {
    const s = Tf.state.status;
    let cls = 'badge-unknown', label = '…';
    if (s) {
      if (s.supported === false) { cls = 'badge-danger'; label = Tf.t('tf_gate_unavailable', 'Terraform unavailable'); }
      else if (!s.enabled) { cls = 'badge-warning'; label = Tf.t('tf_gate_disabled_title', 'Terraform Gate is Disabled'); }
      else { cls = 'badge-healthy'; label = Tf.t('tf_gate_active', 'Terraform Gate is Active'); }
    }
    return '<button type="button" class="btn btn-sm header-btn" data-tf="goto-gate" title="' + Tf.esc(Tf.t('tf_gate_open_settings', 'Open Gate Settings')) + '">' +
      '<span id="tfGateStatus" class="badge ' + cls + '" role="status">' + Tf.esc(label) + '</span></button>';
  }

  function buildShell() {
    const el = root();
    if (!el) return;
    shellBuilt = true;
    const active = Tf.state.tab;
    let tablist = '', panels = '';
    tabs.forEach(function (tb) {
      const C = cap(tb.id), on = tb.id === active;
      tablist += '<button type="button" class="tab' + (on ? ' active' : '') + '" id="tabTf' + C + '" data-tab="' + tb.id + '" role="tab" aria-selected="' + on +
        '" aria-controls="tfPanel-' + tb.id + '"' + (on ? '' : ' tabindex="-1"') + '>' + Tf.icon(tb.icon || 'layers', 'icon-sm') + ' <span>' + Tf.esc(tabLabel(tb)) + '</span></button>';
      panels += '<div id="tfPanel-' + tb.id + '" class="tf-panel" role="tabpanel" aria-labelledby="tabTf' + C + '" tabindex="0"' + (on ? '' : ' hidden') + '></div>';
    });
    el.innerHTML =
      '<div class="section-header tf-head"><div><h2>' + Tf.esc(Tf.t('nav_terraform', 'Terraform')) + '</h2><p>' +
      Tf.esc(Tf.t('tf_desc', 'Plan, approve and apply infrastructure with Terraform or OpenTofu, with state, variables and policies kept in Pantau.')) + '</p></div>' +
      '<div class="tf-head-actions">' + gateBtnHtml() +
      '<button type="button" id="btnTfRefresh" class="btn btn-sm header-btn" data-tf="refresh">' + Tf.icon('refresh-cw', 'icon-sm') + ' <span>' + Tf.esc(Tf.t('tf_refresh', 'Refresh')) + '</span></button></div></div>' +
      '<div id="tfTabs" class="tabs tf-tabs" role="tablist" aria-label="Terraform">' + tablist + '</div>' + panels;
    Object.keys(rendered).forEach(function (k) { delete rendered[k]; });
    showTab(active, true);
  }

  let lastGate = null;
  function refreshGate() {
    const pill = document.getElementById('tfGateStatus');
    if (pill) pill.outerHTML = gateBtnHtml().match(/<span id="tfGateStatus"[\s\S]*?<\/span>/)[0];
    const on = Tf.gateOn();
    if (on !== lastGate) { lastGate = on; if (shellBuilt && Tf.state.status) showTab(Tf.state.tab, true); }
  }
  Tf.on('status', refreshGate);

  // Shows a tab. Blocked tabs (gate off) show the gate card instead of rendering; governance always renders
  // because it hosts the Gate settings themselves.
  function showTab(id, force) {
    const tb = tabs.find(function (x) { return x.id === id; }) || tabs[0];
    if (!tb) return;
    const prev = tabs.find(function (x) { return x.id === Tf.state.tab; });
    if (prev && prev.id !== tb.id && prev.onHide) { try { prev.onHide(); } catch (e) { console.error(e); } }
    Tf.state.tab = tb.id;
    tabs.forEach(function (x) {
      const C = cap(x.id), on = x.id === tb.id;
      const b = document.getElementById('tabTf' + C), p = document.getElementById('tfPanel-' + x.id);
      if (b) { b.classList.toggle('active', on); b.setAttribute('aria-selected', String(on)); b.tabIndex = on ? 0 : -1; }
      if (p) p.hidden = !on;
    });
    let panel = document.getElementById('tfPanel-' + tb.id);
    if (!panel) return;
    if (!Tf.state.status) { panel.innerHTML = Tf.ui.skeleton(4); return; }
    const blocked = !Tf.gateOn() && !tb.alwaysRender;
    if (blocked) {
      if (rendered[tb.id] || force) { const fresh = panel.cloneNode(false); panel.replaceWith(fresh); panel = fresh; }
      rendered[tb.id] = false;
      panel.innerHTML = Tf.ui.gateOff(tb.gateDesc);
      return;
    }
    if (force && rendered[tb.id] === false) panel.innerHTML = '';
    if (!rendered[tb.id]) {
      rendered[tb.id] = true;
      panel.innerHTML = '';
      try { tb.render(panel); } catch (e) { console.error(e); panel.innerHTML = Tf.ui.error(e.message); }
    }
    if (tb.onShow) { try { tb.onShow(panel, { refresh: !!force }); } catch (e) { console.error(e); } }
  }
  Tf.showTab = showTab;

  // Jump to a top-level tab and optionally one of its sub-views (Tf.state.sub[tab]).
  Tf.navigate = function (tabId, sub) {
    if (sub) Tf.state.sub[tabId] = sub;
    if (!shellBuilt) return;
    showTab(tabId, false);
  };

  function onShellClick(e) {
    const a = e.target.closest('[data-tf]');
    if (a && root() && root().contains(a)) {
      const act = a.getAttribute('data-tf');
      if (act === 'refresh') Tf.refresh();
      else if (act === 'goto-gate') Tf.navigate('governance', 'gate');
      return;
    }
    const tb = e.target.closest('#tfTabs .tab');
    if (tb) showTab(tb.getAttribute('data-tab'));
  }
  function onShellKey(e) {
    const tb = e.target.closest('#tfTabs .tab');
    if (!tb) return;
    const btns = Array.prototype.slice.call(document.querySelectorAll('#tfTabs .tab'));
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

  Tf.visible = function () { const r = root(); return !!r && r.style.display !== 'none'; };

  // Reloads the gate and the active tab. Called when the Terraform section is shown and by Refresh.
  Tf.refresh = async function () {
    await Tf.load.status();
    if (!shellBuilt) {
      const el = root();
      if (!el) return;
      el.addEventListener('click', onShellClick);
      el.addEventListener('keydown', onShellKey);
      buildShell();
      lastGate = Tf.gateOn();
      return;
    }
    const tb = tabs.find(function (x) { return x.id === Tf.state.tab; });
    if (tb && Tf.gateOn() && tb.onShow) { try { await tb.onShow(document.getElementById('tfPanel-' + tb.id), { refresh: true }); } catch (e) { console.error(e); } }
  };
  Tf.rerender = function () {
    if (!shellBuilt) return;
    buildShell();
    refreshGate();
    Tf.emit('lang');
  };

  // Entry point used by the side-nav (08-metrics-systemd-docker.js).
  window.refreshTfSection = function () { return Tf.refresh(); };
  if (typeof langRerenderHooks !== 'undefined') langRerenderHooks.push(Tf.rerender);

  /* ---------- polling: only while the Terraform section is visible ---------- */
  const pollers = [];
  Tf.every = function (fn, ms) {
    const p = { fn: fn, ms: ms, timer: null };
    p.timer = setInterval(function () { if (Tf.visible() && document.visibilityState !== 'hidden') { try { fn(); } catch (e) { console.error(e); } } }, ms);
    pollers.push(p);
    return function stop() { clearInterval(p.timer); };
  };

  /* ---------- workspace drawer tabs (registered by feature modules, rendered by 10-workspaces) ---------- */
  // registerWsTab({ id, icon, labelKey, fallback, order, render(body, ws, ctx), onHide() })
  const wsTabs = [];
  Tf.registerWsTab = function (tb) { wsTabs.push(tb); wsTabs.sort(function (a, b) { return (a.order || 0) - (b.order || 0); }); return tb; };
  Tf.wsTabs = function () { return wsTabs.slice(); };

  /* ---------- drawer stack (ws drawer, run viewer, registry detail, forms...) ---------- */
  // view = { title, subtitle, wide, actions, mount(body, ctrl), unmount(), canClose() }
  let drawerEl = null, stack = [], lastFocus = null;
  function ensureDrawer() {
    if (drawerEl) return drawerEl;
    document.body.insertAdjacentHTML('beforeend',
      '<div id="tfDrawerRoot" class="tf-drawer-root" hidden>' +
      '<div class="tf-drawer-backdrop" data-d="close"></div>' +
      '<aside id="tfDrawer" class="tf-drawer" role="dialog" aria-modal="true" aria-labelledby="tfDrawerTitle" tabindex="-1">' +
      '<div class="tf-drawer-head"><button type="button" class="btn btn-sm btn-ghost tf-drawer-back" data-d="back" hidden></button>' +
      '<div class="tf-drawer-titles"><h3 id="tfDrawerTitle"></h3><div class="tf-drawer-sub"></div></div>' +
      '<div class="tf-drawer-actions"></div>' +
      '<button type="button" id="tfDrawerWide" class="btn btn-sm btn-ghost" data-d="wide"></button>' +
      '<button type="button" class="btn btn-sm btn-ghost tf-drawer-x" data-d="close"></button></div>' +
      '<div class="tf-drawer-body"></div></aside></div>');
    drawerEl = document.getElementById('tfDrawerRoot');
    drawerEl.addEventListener('click', function (e) {
      const d = e.target.closest('[data-d]');
      if (!d) return;
      const act = d.getAttribute('data-d');
      if (act === 'close') Tf.drawer.close();
      else if (act === 'back') Tf.drawer.back();
      else if (act === 'wide') { const top = stack[stack.length - 1]; if (top) setWide(!top.wide); }
    });
    drawerEl.addEventListener('keydown', function (e) {
      if (e.key === 'Escape' && !e.defaultPrevented) { e.stopPropagation(); Tf.drawer.close(); }
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
    const el = ensureDrawer();
    el.querySelector('.tf-drawer').classList.toggle('wide', !!on);
    const w = el.querySelector('#tfDrawerWide');
    w.innerHTML = Tf.icon(on ? 'minimize-2' : 'maximize-2', 'icon-sm');
    w.title = on ? Tf.t('tf_collapse', 'Collapse') : Tf.t('tf_expand', 'Expand');
  }
  function mountTop() {
    const top = stack[stack.length - 1];
    const el = ensureDrawer();
    const body = el.querySelector('.tf-drawer-body');
    body.innerHTML = '';
    el.querySelector('.tf-drawer-actions').innerHTML = '';
    el.querySelector('#tfDrawerTitle').textContent = top.title || '';
    el.querySelector('.tf-drawer-sub').innerHTML = top.subtitle || '';
    const back = el.querySelector('.tf-drawer-back');
    back.hidden = stack.length < 2;
    back.innerHTML = Tf.icon('chevron-left', 'icon-sm') + ' <span>' + Tf.esc(Tf.t('tf_back', 'Back')) + '</span>';
    el.querySelector('.tf-drawer-x').innerHTML = Tf.icon('x', 'icon-sm');
    el.querySelector('.tf-drawer-x').title = Tf.t('tf_close', 'Close');
    setWide(top.wide);
    const ctrl = {
      setTitle: function (s) { if (top === stack[stack.length - 1]) el.querySelector('#tfDrawerTitle').textContent = s; top.title = s; },
      setSubtitle: function (h) { if (top === stack[stack.length - 1]) el.querySelector('.tf-drawer-sub').innerHTML = h; top.subtitle = h; },
      setActions: function (h) {
        const box = el.querySelector('.tf-drawer-actions');
        if (typeof h === 'string') box.innerHTML = h; else { box.innerHTML = ''; if (h) box.appendChild(h); }
      },
      setWide: setWide,
      push: Tf.drawer.push, back: Tf.drawer.back, close: Tf.drawer.close,
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
  Tf.drawer = {
    current: function () { return stack[stack.length - 1] || null; },
    isOpen: function () { return stack.length > 0; },
    open: async function (view) {
      if (stack.length) { for (let i = stack.length - 1; i >= 0; i--) if (!(await canClose(stack[i]))) return false; unmountTop(); }
      stack = [view];
      const el = ensureDrawer();
      if (el.hidden) { lastFocus = document.activeElement; el.hidden = false; requestAnimationFrame(function () { el.classList.add('open'); }); }
      mountTop();
      const a = el.querySelector('.tf-drawer'); if (a) a.focus();
      return true;
    },
    push: async function (view) {
      if (!stack.length) return Tf.drawer.open(view);
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
      el.querySelector('.tf-drawer-body').innerHTML = '';
      if (lastFocus && lastFocus.focus) { try { lastFocus.focus(); } catch (e) { /* detached */ } }
      lastFocus = null;
      return true;
    },
  };
  Tf.on('lang', function () {
    if (!stack.length) return;
    const top = stack[stack.length - 1];
    const el = ensureDrawer();
    el.querySelector('.tf-drawer-back').innerHTML = Tf.icon('chevron-left', 'icon-sm') + ' <span>' + Tf.esc(Tf.t('tf_back', 'Back')) + '</span>';
    setWide(top.wide);
  });

  /* ---------- dialog (single modal element per call, closed and removed on close) ---------- */
  let dlgSeq = 0;
  // open({ title, body (html), footer (html), width, onMount(root, api) }) -> api { root, close(), error(msg), clearError() }
  Tf.dlg = {
    open: function (o) {
      const id = 'tfDlg-' + (++dlgSeq);
      const w = o.width || 560;
      const el = document.createElement('div');
      el.id = id;
      el.className = 'modal tf-dlg';
      el.setAttribute('role', 'dialog');
      el.setAttribute('aria-modal', 'true');
      el.innerHTML =
        '<div class="modal-content modal-dialog" style="max-width:' + w + 'px;">' +
        '<div class="modal-header"><h3 class="tf-dlg-title" style="font-size:var(--fs-md);margin:0;">' + Tf.esc(o.title || '') + '</h3>' +
        '<button type="button" class="btn btn-sm btn-ghost btn-icon" data-dlg-close aria-label="' + Tf.esc(Tf.t('tf_close', 'Close')) + '">' + Tf.icon('x', 'icon-sm') + '</button></div>' +
        '<div class="modal-body tf-dlg-body" style="padding:16px 20px;overflow:auto;">' + (o.body || '') + '</div>' +
        '<div class="tf-dlg-err" role="alert" hidden></div>' +
        '<div class="modal-footer tf-dlg-footer" style="display:flex;gap:8px;justify-content:flex-end;align-items:center;">' + (o.footer || '') + '</div></div>';
      document.body.appendChild(el);
      let closed = false;
      const api = {
        root: el,
        body: el.querySelector('.tf-dlg-body'),
        footer: el.querySelector('.tf-dlg-footer'),
        error: function (msg) { const b = el.querySelector('.tf-dlg-err'); b.textContent = msg || ''; b.hidden = !msg; },
        clearError: function () { api.error(''); },
        close: function () {
          if (closed) return;
          closed = true;
          if (typeof closeModal === 'function') closeModal(id);
          el.remove();
          if (o.onClose) { try { o.onClose(); } catch (e) { console.error(e); } }
        },
      };
      el.addEventListener('click', function (e) {
        if (e.target === el || e.target.closest('[data-dlg-close]')) api.close();
      });
      el.addEventListener('keydown', function (e) { if (e.key === 'Escape') { e.stopPropagation(); api.close(); } });
      if (typeof openModal === 'function') openModal(id);
      if (o.onMount) { try { o.onMount(el, api); } catch (e) { console.error(e); } }
      const first = el.querySelector('input,select,textarea,button:not([data-dlg-close])');
      if (first) first.focus();
      return api;
    },
  };

  /* ---------- shared bootstrap for modules ---------- */
  // Modules that need the current workspace list call Tf.load.workspaces() (defined in 10-workspaces).
  Tf.refreshPollers = function () { pollers.forEach(function (p) { if (Tf.visible()) { try { p.fn(); } catch (e) { console.error(e); } } }); };
})();
