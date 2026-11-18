/* Concentric Radial Network Radar — a single self-contained <svg viewBox>.
 * Nodes are placed by deterministic polar math (sector/sectors from the API),
 * edges are curved radial paths, and the viewBox itself is zoomed/panned. No
 * physics loop, no getBoundingClientRect alignment, no HTML grid, no libraries.
 * SSE toggles data-status on node <g> elements surgically; the HUD is an SVG-
 * adjacent floating card. Relies on globals: svgIcon, escapeHtml, t, I18N,
 * openModal, closeModal, openHostDetail, openHostTerminal, toggleActivityConsole. */
(function () {
  'use strict';

  const TP = (window.Topology = {});
  const MODAL_ID = 'topologyModal';
  const CENTER = { x: 500, y: 350 };
  const RADIUS = { inner: 170, outer: 292 };
  const DEFAULT_VIEW = { x: 0, y: 0, w: 1000, h: 700 };

  const S = {
    graph: null,
    group: '',
    query: '',
    selected: null,
    hovered: null,
    autoTimer: null,
    autoIndex: 0,
    refreshTimer: null,
    detailSeq: 0,
    view: Object.assign({}, DEFAULT_VIEW),
    drag: null,
  };

  const esc = (s) => escapeHtml(String(s === null || s === undefined ? '' : s));
  const icon = (n) => svgIcon(n, 'icon-sm');

  const I = {
    en: {
      tp_title: 'Network Radar', tp_subtitle: 'Live concentric topology',
      tp_search_ph: 'Find a node…', tp_all_groups: 'All groups',
      tp_refresh: 'Refresh', tp_live: 'LIVE', tp_reconnecting: 'RECONNECTING', tp_offline: 'OFFLINE',
      tp_empty: 'No infrastructure yet. Add a Host, VPN Network or Cloudflare account.',
      tp_healthy: 'Healthy', tp_degraded: 'Degraded', tp_down: 'Down', tp_unknown: 'Unknown',
      tp_alerts: 'alerts', tp_terminal: 'Terminal', tp_logs: 'Activity Logs', tp_workspace: 'Host Workspace',
      tp_no_host: 'This node is not a managed Host.', tp_close: 'Close', tp_nodes: 'nodes',
      tp_failed: 'Could not load topology', tp_loading: 'Loading service detail…',
      tp_no_detail: 'No stored service detail yet — run an inspection on this host.',
      tp_containers: 'Containers', tp_listening: 'Listening ports / reverse proxy',
      tp_zoom_in: 'Zoom in', tp_zoom_out: 'Zoom out', tp_zoom_reset: 'Reset view',
      tp_hub: 'Gateway', tp_inner: 'Network', tp_outer: 'Compute / Clusters',
    },
    id: {
      tp_title: 'Radar Jaringan', tp_subtitle: 'Topologi konsentris langsung',
      tp_search_ph: 'Cari node…', tp_all_groups: 'Semua grup',
      tp_refresh: 'Muat ulang', tp_live: 'LANGSUNG', tp_reconnecting: 'MENGHUBUNGKAN', tp_offline: 'OFFLINE',
      tp_empty: 'Belum ada infrastruktur. Tambahkan Host, VPN Network, atau akun Cloudflare.',
      tp_healthy: 'Sehat', tp_degraded: 'Degraded', tp_down: 'Down', tp_unknown: 'Unknown',
      tp_alerts: 'alert', tp_terminal: 'Terminal', tp_logs: 'Log Aktivitas', tp_workspace: 'Workspace Host',
      tp_no_host: 'Node ini bukan Host yang dikelola.', tp_close: 'Tutup', tp_nodes: 'node',
      tp_failed: 'Gagal memuat topologi', tp_loading: 'Memuat detail layanan…',
      tp_no_detail: 'Belum ada detail layanan — jalankan inspeksi pada host ini.',
      tp_containers: 'Container', tp_listening: 'Port listening / reverse proxy',
      tp_zoom_in: 'Perbesar', tp_zoom_out: 'Perkecil', tp_zoom_reset: 'Atur ulang tampilan',
      tp_hub: 'Gateway', tp_inner: 'Jaringan', tp_outer: 'Compute / Cluster',
    },
  };
  ['en', 'id'].forEach((l) => { I18N[l] = Object.assign(I18N[l] || {}, I[l]); });

  const KIND_ICON = { gateway: 'shield', vpn: 'network', cloudflare: 'cloud', host: 'server', kube: 'boxes' };

  // ---- Modal shell (ADR 0007 Workspace Modal) ----------------------------

  function modalHtml() {
    return `
      <div class="modal-content modal-workspace tp-modal-content">
        <div class="modal-header tp-header">
          <div class="tp-title-group">
            ${icon('radar')}
            <h3 class="tp-title">${esc(t('tp_title'))}</h3>
            <span class="tp-subtitle">${esc(t('tp_subtitle'))}</span>
            <span id="tpLivePill" class="tp-live-pill tp-offline"><span class="tp-live-dot"></span><span>${esc(t('tp_offline'))}</span></span>
            <span id="tpCount" class="tp-count"></span>
          </div>
          <div class="tp-header-actions">
            <button class="btn btn-icon btn-sm btn-ghost" id="tpZoomIn" title="${esc(t('tp_zoom_in'))}">${icon('plus')}</button>
            <button class="btn btn-icon btn-sm btn-ghost" id="tpZoomOut" title="${esc(t('tp_zoom_out'))}">${icon('minus')}</button>
            <button class="btn btn-icon btn-sm btn-ghost" id="tpZoomReset" title="${esc(t('tp_zoom_reset'))}">${icon('rotate-ccw')}</button>
            <button class="btn btn-icon btn-sm btn-ghost" id="tpRefreshBtn" title="${esc(t('tp_refresh'))}">${icon('refresh-cw')}</button>
            <button class="btn btn-icon btn-sm btn-ghost" onclick="closeModal('${MODAL_ID}')" title="${esc(t('tp_close'))}">${icon('x')}</button>
          </div>
        </div>
        <div class="tp-toolbar">
          <input type="text" id="tpSearch" class="tp-search" placeholder="${esc(t('tp_search_ph'))}" autocomplete="off" spellcheck="false" />
          <div id="tpPills" class="tp-pills" role="group" aria-label="group filter"></div>
        </div>
        <div class="tp-body">
          <div class="tp-canvas" id="tpCanvas">
            <svg id="tpRadar" class="tp-radar" viewBox="0 0 1000 700" preserveAspectRatio="xMidYMid meet"
                 xmlns="http://www.w3.org/2000/svg" role="img" aria-label="${esc(t('tp_title'))}">
              <g id="tpRanges" class="tp-ranges"></g>
              <g id="tpEdges" class="tp-edges"></g>
              <g id="tpNodes" class="tp-nodes"></g>
            </svg>
            <div id="tpEmpty" class="tp-empty" hidden>${esc(t('tp_empty'))}</div>
            <aside id="tpHud" class="tp-hud" hidden></aside>
          </div>
        </div>
      </div>`;
  }

  function ensureModal() {
    if (document.getElementById(MODAL_ID)) return;
    const el = document.createElement('div');
    el.id = MODAL_ID;
    el.className = 'modal';
    el.innerHTML = modalHtml();
    document.body.appendChild(el);
  }

  function rebuildModal() {
    const m = document.getElementById(MODAL_ID);
    if (!m) return;
    const search = document.getElementById('tpSearch');
    if (search) S.query = search.value;
    m.innerHTML = modalHtml();
    restoreControls();
    applyView();
    render();
  }
  if (typeof langRerenderHooks !== 'undefined') langRerenderHooks.push(rebuildModal);

  function restoreControls() {
    const search = document.getElementById('tpSearch');
    if (search) search.value = S.query;
  }

  // ---- Filtering ----------------------------------------------------------

  function visibleNodes() {
    if (!S.graph) return [];
    const q = S.query.toLowerCase();
    return S.graph.nodes.filter((n) => {
      if (S.group && (n.group || '') !== S.group) return false;
      if (q && !((n.label || '').toLowerCase().includes(q) || (n.group || '').toLowerCase().includes(q))) return false;
      return true;
    });
  }

  // ---- Deterministic polar layout ----------------------------------------

  function nodePos(n) {
    if (n.ring === 0 || !n.sectors) return Object.assign({}, CENTER);
    const r = n.ring === 1 ? RADIUS.inner : RADIUS.outer;
    const angle = -Math.PI / 2 + ((n.sector + 0.5) / n.sectors) * Math.PI * 2;
    return { x: CENTER.x + r * Math.cos(angle), y: CENTER.y + r * Math.sin(angle) };
  }

  function edgePath(a, b) {
    const mx = (a.x + b.x) / 2;
    const my = (a.y + b.y) / 2;
    const dx = mx - CENTER.x;
    const dy = my - CENTER.y;
    const len = Math.hypot(dx, dy) || 1;
    const bow = Math.min(26, Math.hypot(b.x - a.x, b.y - a.y) * 0.12);
    const cx = mx - (dx / len) * bow;
    const cy = my - (dy / len) * bow;
    return `M ${a.x.toFixed(1)} ${a.y.toFixed(1)} Q ${cx.toFixed(1)} ${cy.toFixed(1)} ${b.x.toFixed(1)} ${b.y.toFixed(1)}`;
  }

  // ---- Rendering ----------------------------------------------------------

  function render() {
    if (!S.graph) return;
    const nodes = visibleNodes();
    const empty = document.getElementById('tpEmpty');
    if (empty) empty.hidden = nodes.length > 0;
    renderRanges();
    renderEdges(nodes);
    renderNodes(nodes);
    renderPills();
    const count = document.getElementById('tpCount');
    if (count) count.textContent = nodes.length + ' ' + t('tp_nodes');
    if (S.selected) renderHud();
    if (S.hovered || S.selected) {
      highlightRoute(S.hovered || S.selected);
    } else {
      // Keep a live route even before the first auto tick.
      const targets = autoFocusTargets();
      if (targets.length) highlightRoute(targets[S.autoIndex % targets.length]);
      startAutoCycle();
    }
  }

  function renderRanges() {
    const g = document.getElementById('tpRanges');
    if (!g) return;
    let s = '';
    [90, RADIUS.inner, 232, RADIUS.outer].forEach((r, i) => {
      s += `<circle class="tp-range tp-range-${i}" cx="${CENTER.x}" cy="${CENTER.y}" r="${r}" />`;
    });
    s += `<line class="tp-axis" x1="${CENTER.x}" y1="24" x2="${CENTER.x}" y2="676" />`;
    s += `<line class="tp-axis" x1="140" y1="${CENTER.y}" x2="860" y2="${CENTER.y}" />`;
    s += `<text class="tp-range-label" x="${CENTER.x + 6}" y="${CENTER.y - RADIUS.outer - 8}">${esc(t('tp_outer'))}</text>`;
    s += `<text class="tp-range-label" x="${CENTER.x + 6}" y="${CENTER.y - RADIUS.inner - 8}">${esc(t('tp_inner'))}</text>`;
    g.innerHTML = s;
  }

  function renderEdges(nodes) {
    const g = document.getElementById('tpEdges');
    if (!g) return;
    const visible = new Set(nodes.map((n) => n.id));
    const pos = {};
    nodes.forEach((n) => { pos[n.id] = nodePos(n); });
    const byId = {};
    nodes.forEach((n) => { byId[n.id] = n; });
    let s = '';
    S.graph.edges.forEach((e) => {
      if (!visible.has(e.source) || !visible.has(e.target)) return;
      const a = pos[e.source];
      const b = pos[e.target];
      if (!a || !b) return;
      const alert = (byId[e.source] && byId[e.source].alerts > 0) || (byId[e.target] && byId[e.target].alerts > 0);
      const cls = 'tp-edge tp-edge-' + e.kind + (e.active ? '' : ' tp-edge-inactive') + (alert ? ' tp-edge-alert' : '');
      s += `<path class="${cls}" data-edge-id="${esc(e.id)}" d="${edgePath(a, b)}" />`;
    });
    g.innerHTML = s;
  }

  // Trace the chain of edges from a node back to the gateway. Used to light up
  // the active route on hover/selection; BFS keeps it deterministic and cheap
  // (the graph is a small macro tree, never workload-sized).
  function pathToGateway(nodeId) {
    if (!S.graph || !nodeId || nodeId === 'gateway') return new Set();
    const adj = {};
    S.graph.edges.forEach((e) => {
      (adj[e.source] = adj[e.source] || []).push({ edge: e.id, next: e.target });
      (adj[e.target] = adj[e.target] || []).push({ edge: e.id, next: e.source });
    });
    const prev = { [nodeId]: null };
    const queue = [nodeId];
    while (queue.length) {
      const cur = queue.shift();
      if (cur === 'gateway') break;
      (adj[cur] || []).forEach((link) => {
        if (!(link.next in prev)) { prev[link.next] = { edge: link.edge, from: cur }; queue.push(link.next); }
      });
    }
    const active = new Set();
    let cur = 'gateway';
    while (prev[cur]) { active.add(prev[cur].edge); cur = prev[cur].from; }
    return active;
  }

  // Toggle route highlight on edges and dim the rest. `focusId` is the hovered
  // or selected node; null clears all highlighting.
  function highlightRoute(focusId) {
    const active = pathToGateway(focusId);
    document.querySelectorAll('#tpEdges .tp-edge').forEach((el) => {
      const on = active.has(el.getAttribute('data-edge-id'));
      el.classList.toggle('tp-edge-route', on);
      el.classList.toggle('tp-edge-dim', !!focusId && !on);
    });
  }

  // Continuous animation: cycle a highlight through the outer nodes so the
  // radar stays alive without any hover. User hover/selection takes priority
  // and pauses the cycle; it resumes when the pointer leaves and nothing is
  // selected. One timer, no requestAnimationFrame loop.
  function autoFocusTargets() {
    if (!S.graph) return [];
    return S.graph.nodes
      .filter((n) => n.ring === 2 && visibleNodes().some((v) => v.id === n.id))
      .map((n) => n.id);
  }

  function cycleAutoRoute() {
    const targets = autoFocusTargets();
    if (!targets.length) return;
    if (S.hovered || S.selected) return; // user is in control; leave route as-is
    S.autoIndex = (S.autoIndex + 1) % targets.length;
    highlightRoute(targets[S.autoIndex]);
  }

  function startAutoCycle() {
    if (S.autoTimer) return;
    S.autoTimer = setInterval(cycleAutoRoute, 2600);
  }

  function stopAutoCycle() {
    if (S.autoTimer) { clearInterval(S.autoTimer); S.autoTimer = null; }
  }

  function renderNodes(nodes) {
    const g = document.getElementById('tpNodes');
    if (!g) return;
    g.innerHTML = nodes.map(nodeHtml).join('');
  }

  function nodeHtml(n) {
    const p = nodePos(n);
    const isHub = n.ring === 0;
    const r = isHub ? 30 : (n.ring === 1 ? 19 : 15);
    const alerts = n.alerts > 0;
    const sel = S.selected === n.id ? ' tp-selected' : '';
    const labelY = r + 16;
    const meta = n.kind === 'host'
      ? (n.metrics && n.metrics.cpu_pct ? Math.round(n.metrics.cpu_pct) + '% CPU' : (n.group || ''))
      : (n.network || '');
    return `
      <g class="tp-node tp-status-${esc(n.status)}${sel}" data-node-id="${esc(n.id)}" data-status="${esc(n.status)}"
         data-alerts="${n.alerts}" transform="translate(${p.x.toFixed(1)} ${p.y.toFixed(1)})"
         tabindex="0" role="button" aria-label="${esc(n.label)}">
        <circle class="tp-pulse" r="${r + 8}" />
        <circle class="tp-core" r="${r}" />
        <use class="tp-icon tp-icon-${n.kind}" href="#i-${KIND_ICON[n.kind] || 'circle-dot'}" x="${-r * 0.55}" y="${-r * 0.55}" width="${r * 1.1}" height="${r * 1.1}" />
        ${alerts ? `<g class="tp-alert-badge" transform="translate(${r - 2} ${-r + 2})"><circle r="9" /><text y="3">${n.alerts}</text></g>` : ''}
        <text class="tp-node-label" y="${labelY}">${esc(n.label)}</text>
        ${meta ? `<text class="tp-node-meta" y="${labelY + 13}">${esc(meta)}</text>` : ''}
      </g>`;
  }

  function renderPills() {
    const box = document.getElementById('tpPills');
    if (!box || !S.graph) return;
    const groups = [...new Set(S.graph.nodes.map((n) => n.group).filter(Boolean))].sort();
    const pill = (value, label) => `<button type="button" class="tp-pill${S.group === value ? ' active' : ''}" data-group="${esc(value)}">${esc(label)}</button>`;
    box.innerHTML = pill('', t('tp_all_groups')) + groups.map((g) => pill(g, g)).join('');
  }

  // ---- viewBox zoom / pan -------------------------------------------------

  function applyView() {
    const svg = document.getElementById('tpRadar');
    if (!svg) return;
    const v = S.view;
    svg.setAttribute('viewBox', `${v.x.toFixed(1)} ${v.y.toFixed(1)} ${v.w.toFixed(1)} ${v.h.toFixed(1)}`);
  }

  function zoom(factor, clientX, clientY) {
    const svg = document.getElementById('tpRadar');
    if (!svg) return;
    const v = S.view;
    let ax = v.x + v.w / 2;
    let ay = v.y + v.h / 2;
    if (clientX !== undefined && svg.createSVGPoint) {
      const pt = svg.createSVGPoint();
      pt.x = clientX;
      pt.y = clientY;
      const m = svg.getScreenCTM();
      if (m) {
        const local = pt.matrixTransform(m.inverse());
        ax = local.x;
        ay = local.y;
      }
    }
    const nw = Math.max(240, Math.min(1800, v.w * factor));
    const nh = nw * (DEFAULT_VIEW.h / DEFAULT_VIEW.w);
    const rx = (ax - v.x) / v.w;
    const ry = (ay - v.y) / v.h;
    S.view = { x: ax - rx * nw, y: ay - ry * nh, w: nw, h: nh };
    applyView();
  }

  function resetView() {
    S.view = Object.assign({}, DEFAULT_VIEW);
    applyView();
  }

  // ---- Floating HUD card --------------------------------------------------

  function nodeById(id) {
    return S.graph ? S.graph.nodes.find((n) => n.id === id) : null;
  }

  function openHud(id) {
    S.selected = id;
    document.querySelectorAll('#tpNodes .tp-node').forEach((el) => {
      el.classList.toggle('tp-selected', el.getAttribute('data-node-id') === id);
    });
    renderHud();
    highlightRoute(S.hovered || S.selected);
    const n = nodeById(id);
    if (n && n.kind === 'host') loadDetail(n);
  }

  function closeHud() {
    S.selected = null;
    const hud = document.getElementById('tpHud');
    if (hud) hud.hidden = true;
    document.querySelectorAll('#tpNodes .tp-node.tp-selected').forEach((el) => el.classList.remove('tp-selected'));
    highlightRoute(S.hovered);
  }

  function renderHud() {
    const hud = document.getElementById('tpHud');
    const n = nodeById(S.selected);
    if (!hud || !n) return;
    hud.hidden = false;
    const isHost = n.kind === 'host';
    const cpu = n.metrics && n.metrics.cpu_pct ? Math.round(n.metrics.cpu_pct) + '%' : '—';
    const ram = n.metrics && n.metrics.ram_pct ? Math.round(n.metrics.ram_pct) + '%' : '—';
    hud.innerHTML = `
      <div class="tp-hud-head">
        <div class="tp-hud-title"><span class="tp-hud-dot tp-status-${esc(n.status)}"></span><strong>${esc(n.label)}</strong></div>
        <button class="btn btn-icon btn-sm btn-ghost" id="tpHudClose" title="${esc(t('tp_close'))}">${icon('x')}</button>
      </div>
      <div class="tp-hud-body">
        <div class="tp-hud-kind badge badge-${n.status === 'healthy' ? 'healthy' : n.status === 'degraded' ? 'warning' : n.status === 'down' ? 'danger' : 'info'}">${esc(n.kind)}</div>
        <div class="tp-hud-stats">
          <span><b>${esc(t('tp_healthy'))}</b> ${esc(t('tp_' + n.status))}</span>
          ${isHost ? `<span><b>CPU</b> ${esc(cpu)}</span><span><b>RAM</b> ${esc(ram)}</span>` : ''}
          ${n.alerts ? `<span class="tp-hud-alerts"><b>${n.alerts}</b> ${esc(t('tp_alerts'))}</span>` : ''}
        </div>
        ${n.group ? `<div class="tp-hud-summary">${esc(n.group)}${n.network ? ' · ' + esc(n.network) : ''}</div>` : ''}
        <div class="tp-hud-actions">
          <button class="btn btn-sm" id="tpHudTerminal" ${isHost ? '' : 'disabled'}>${icon('terminal')} ${esc(t('tp_terminal'))}</button>
          <button class="btn btn-sm" id="tpHudLogs">${icon('file-text')} ${esc(t('tp_logs'))}</button>
          <button class="btn btn-sm btn-primary" id="tpHudWorkspace" ${isHost ? '' : 'disabled'}>${icon('server')} ${esc(t('tp_workspace'))}</button>
        </div>
        ${isHost ? '' : `<div class="tp-hud-note">${esc(t('tp_no_host'))}</div>`}
        <div id="tpHudDetail" class="tp-hud-detail">${isHost ? esc(t('tp_loading')) : ''}</div>
      </div>`;
    const close = document.getElementById('tpHudClose');
    if (close) close.onclick = closeHud;
    const terminal = document.getElementById('tpHudTerminal');
    if (terminal && isHost) terminal.onclick = () => { if (typeof openHostTerminal === 'function') openHostTerminal(n.host_id); };
    const logs = document.getElementById('tpHudLogs');
    if (logs) logs.onclick = () => { if (typeof toggleActivityConsole === 'function') toggleActivityConsole(); };
    const workspace = document.getElementById('tpHudWorkspace');
    if (workspace && isHost) workspace.onclick = () => { if (typeof openHostDetail === 'function') openHostDetail(n.host_id); };
  }

  async function loadDetail(n) {
    const seq = ++S.detailSeq;
    const box = document.getElementById('tpHudDetail');
    if (!box) return;
    try {
      const res = await fetch('/api/topology/host?id=' + encodeURIComponent(n.host_id));
      if (!res.ok) throw new Error('HTTP ' + res.status);
      const d = await res.json();
      if (seq !== S.detailSeq) return;
      const section = (title, rows) => rows.length
        ? `<div class="tp-detail-section"><div class="tp-detail-title">${esc(title)}</div>${rows.map((r) =>
            `<div class="tp-detail-row"><span class="tp-detail-name">${esc(r.name)}</span><span class="tp-detail-sub">${esc(r.detail || r.status || '')}</span></div>`).join('')}</div>`
        : '';
      const hasAny = (d.containers && d.containers.length) || (d.listening && d.listening.length);
      box.innerHTML = hasAny
        ? section(t('tp_containers'), d.containers || []) + section(t('tp_listening'), d.listening || [])
        : esc(t('tp_no_detail'));
    } catch (_) {
      if (seq === S.detailSeq) box.textContent = t('tp_failed');
    }
  }

  // ---- Data loading -------------------------------------------------------

  async function load() {
    const count = document.getElementById('tpCount');
    try {
      const res = await fetch('/api/topology');
      if (!res.ok) throw new Error('HTTP ' + res.status);
      S.graph = await res.json();
      render();
    } catch (err) {
      if (count) count.textContent = t('tp_failed');
      console.error('topology load failed', err);
    }
  }

  function scheduleRefresh() {
    clearTimeout(S.refreshTimer);
    S.refreshTimer = setTimeout(load, 1200);
  }

  // ---- SSE: surgical data-status updates, no full re-render (ADR 0030) ---

  function setLive(state) {
    const pill = document.getElementById('tpLivePill');
    if (!pill) return;
    pill.className = 'tp-live-pill tp-' + state;
    const label = pill.querySelector('span:last-child');
    if (label) label.textContent = state === 'live' ? t('tp_live') : state === 'reconnecting' ? t('tp_reconnecting') : t('tp_offline');
  }

  function connectSSE() {
    if (S.source || typeof EventSource === 'undefined') return;
    try {
      S.source = new EventSource('/api/logs/stream');
      S.source.onopen = () => setLive('live');
      S.source.onmessage = (e) => {
        if (!e.data) return;
        let evt;
        try { evt = JSON.parse(e.data); } catch (_) { return; }
        handleEvent(evt);
      };
      S.source.onerror = () => setLive('reconnecting');
    } catch (_) { setLive('offline'); }
  }

  function handleEvent(evt) {
    if (!S.graph) return;
    const attrs = evt.attrs || {};
    const nextStatus = attrs.status || (evt.module === 'inspector' ? attrs.host_status : null);
    const target = evt.host
      ? S.graph.nodes.find((n) => n.kind === 'host' && n.label === evt.host)
      : null;
    if (target && nextStatus && ['healthy', 'degraded', 'down', 'unknown'].includes(nextStatus)) {
      target.status = nextStatus;
      const el = document.querySelector('#tpNodes .tp-node[data-node-id="' + cssEscape(target.id) + '"]');
      if (el) {
        el.dataset.status = nextStatus;
        el.className.baseVal = el.className.baseVal.replace(/tp-status-\w+/, 'tp-status-' + nextStatus);
      }
    }
    if (target) {
      const el = document.querySelector('#tpNodes .tp-node[data-node-id="' + cssEscape(target.id) + '"]');
      if (el) {
        el.classList.add('tp-flash');
        setTimeout(() => el.classList.remove('tp-flash'), 1600);
      }
    }
    const module = (evt.module || '').toLowerCase();
    const message = (evt.message || '').toLowerCase();
    if (module.includes('inspect') || message.includes('drift') || message.includes('alert') ||
        message.includes('inspection') || message.includes('network down')) {
      scheduleRefresh();
    }
  }

  function cssEscape(s) { return String(s).replace(/["\\]/g, '\\$&'); }

  function disconnectSSE() {
    if (S.source) { S.source.close(); S.source = null; }
    stopAutoCycle();
    S.hovered = null;
    setLive('offline');
  }
  TP.onClose = disconnectSSE;

  // ---- Events -------------------------------------------------------------

  document.addEventListener('click', (e) => {
    if (!document.getElementById(MODAL_ID)) return;
    const t2 = e.target;
    if (!t2 || !t2.closest) return;
    const node = t2.closest('.tp-node');
    if (node) { openHud(node.getAttribute('data-node-id')); return; }
    if (t2.closest('#tpRefreshBtn')) { load(); return; }
    if (t2.closest('#tpZoomIn')) { zoom(0.8); return; }
    if (t2.closest('#tpZoomOut')) { zoom(1.25); return; }
    if (t2.closest('#tpZoomReset')) { resetView(); return; }
    const pill = t2.closest('.tp-pill');
    if (pill) { S.group = pill.getAttribute('data-group') || ''; render(); }
  });

  // Hover lights up the route from the hovered node to the gateway; leaving the
  // node falls back to the selected node's route (or clears it).
  document.addEventListener('pointerover', (e) => {
    if (!document.getElementById(MODAL_ID)) return;
    const node = e.target && e.target.closest && e.target.closest('.tp-node');
    if (!node) return;
    S.hovered = node.getAttribute('data-node-id');
    highlightRoute(S.hovered || S.selected);
  });

  document.addEventListener('pointerout', (e) => {
    if (!document.getElementById(MODAL_ID)) return;
    const node = e.target && e.target.closest && e.target.closest('.tp-node');
    if (!node) return;
    if (e.relatedTarget && e.relatedTarget.closest && e.relatedTarget.closest('.tp-node') === node) return;
    S.hovered = null;
    if (S.selected) highlightRoute(S.selected);
    else { const t = autoFocusTargets(); highlightRoute(t.length ? t[S.autoIndex % t.length] : null); }
  });

  document.addEventListener('input', (e) => {
    if (!e.target || e.target.id !== 'tpSearch') return;
    S.query = e.target.value.trim();
    render();
  });

  document.addEventListener('keydown', (e) => {
    if (!document.getElementById(MODAL_ID)) return;
    if (e.key === 'Enter' && e.target && e.target.classList && e.target.classList.contains('tp-node')) {
      openHud(e.target.getAttribute('data-node-id'));
    }
  });

  // Wheel zoom and pointer drag pan operate on the viewBox only.
  document.addEventListener('wheel', (e) => {
    if (!document.getElementById(MODAL_ID) || !e.target.closest || !e.target.closest('#tpRadar')) return;
    e.preventDefault();
    zoom(e.deltaY > 0 ? 1.12 : 0.89, e.clientX, e.clientY);
  }, { passive: false });

  document.addEventListener('pointerdown', (e) => {
    const svg = document.getElementById('tpRadar');
    if (!svg || !e.target.closest || !e.target.closest('#tpRadar')) return;
    if (e.target.closest('.tp-node')) return;
    S.drag = { x: e.clientX, y: e.clientY, view: Object.assign({}, S.view), svg };
  });

  document.addEventListener('pointermove', (e) => {
    if (!S.drag) return;
    const rect = S.drag.svg.getBoundingClientRect();
    const vbPerPx = S.drag.view.w / (rect.width || 1);
    S.view = {
      x: S.drag.view.x - (e.clientX - S.drag.x) * vbPerPx,
      y: S.drag.view.y - (e.clientY - S.drag.y) * vbPerPx,
      w: S.drag.view.w,
      h: S.drag.view.h,
    };
    applyView();
  });

  document.addEventListener('pointerup', () => { S.drag = null; });

  TP.open = function () {
    ensureModal();
    restoreControls();
    openModal(MODAL_ID);
    resetView();
    connectSSE();
    load();
  };
})();
