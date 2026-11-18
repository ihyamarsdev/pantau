/* Live Fleet Topology Map — lightweight hybrid layout.
 * Macro node cards in a 3-column CSS grid; ONE pointer-events-none <svg> draws
 * Bezier connectors between visible cards. No physics, no dragging, no full
 * canvas SVG rendering. SSE updates node cards surgically (dataset.status),
 * never rebuilding the DOM. Zero external libraries.
 * Relies on globals: svgIcon, escapeHtml, t, I18N, openModal, closeModal,
 * openHostDetail, openHostTerminal, toggleActivityConsole. */
(function () {
  'use strict';

  const TP = (window.Topology = {});
  const MODAL_ID = 'topologyModal';
  const S = {
    graph: null,
    group: '',
    network: '',
    health: '',
    query: '',
    selected: null,
    source: null,
    refreshTimer: null,
    detailSeq: 0,
  };

  const esc = (s) => escapeHtml(String(s === null || s === undefined ? '' : s));
  const icon = (n) => svgIcon(n, 'icon-sm');

  const I = {
    en: {
      tp_title: 'Fleet Topology', tp_subtitle: 'Live infrastructure map',
      tp_search_ph: 'Find a node…', tp_all_groups: 'All groups', tp_all_networks: 'All networks',
      tp_all_health: 'All health', tp_refresh: 'Refresh', tp_live: 'LIVE', tp_reconnecting: 'RECONNECTING',
      tp_offline: 'OFFLINE', tp_empty: 'No infrastructure yet. Add a Host, VPN Network or Cloudflare account.',
      tp_healthy: 'Healthy', tp_degraded: 'Degraded', tp_down: 'Down', tp_unknown: 'Unknown',
      tp_alerts: 'alerts', tp_terminal: 'Terminal', tp_logs: 'Activity Logs', tp_workspace: 'Host Workspace',
      tp_no_host: 'This node is not a managed Host.', tp_close: 'Close', tp_nodes: 'nodes',
      tp_failed: 'Could not load topology', tp_loading: 'Loading service detail…',
      tp_no_detail: 'No stored service detail yet — run an inspection on this host.',
      tp_containers: 'Containers', tp_listening: 'Listening ports', tp_zoom_in: 'Zoom in', tp_zoom_out: 'Zoom out',
    },
    id: {
      tp_title: 'Topologi Fleet', tp_subtitle: 'Peta infrastruktur langsung',
      tp_search_ph: 'Cari node…', tp_all_groups: 'Semua grup', tp_all_networks: 'Semua jaringan',
      tp_all_health: 'Semua status', tp_refresh: 'Muat ulang', tp_live: 'LANGSUNG', tp_reconnecting: 'MENGHUBUNGKAN',
      tp_offline: 'OFFLINE', tp_empty: 'Belum ada infrastruktur. Tambahkan Host, VPN Network, atau akun Cloudflare.',
      tp_healthy: 'Sehat', tp_degraded: 'Degraded', tp_down: 'Down', tp_unknown: 'Unknown',
      tp_alerts: 'alert', tp_terminal: 'Terminal', tp_logs: 'Log Aktivitas', tp_workspace: 'Workspace Host',
      tp_no_host: 'Node ini bukan Host yang dikelola.', tp_close: 'Tutup', tp_nodes: 'node',
      tp_failed: 'Gagal memuat topologi', tp_loading: 'Memuat detail layanan…',
      tp_no_detail: 'Belum ada detail layanan — jalankan inspeksi pada host ini.',
      tp_containers: 'Container', tp_listening: 'Port listening', tp_zoom_in: 'Perbesar', tp_zoom_out: 'Perkecil',
    },
  };
  ['en', 'id'].forEach((l) => { I18N[l] = Object.assign(I18N[l] || {}, I[l]); });

  const KIND_ICON = { gateway: 'shield', vpn: 'network', cloudflare: 'cloud', host: 'server', kube: 'boxes' };

  function modalHtml() {
    return `
      <div class="modal-content modal-workspace tp-modal-content">
        <div class="modal-header tp-header">
          <div class="tp-title-group">
            ${icon('network')}
            <h3 class="tp-title">${esc(t('tp_title'))}</h3>
            <span class="tp-subtitle">${esc(t('tp_subtitle'))}</span>
            <span id="tpLivePill" class="tp-live-pill tp-offline"><span class="tp-live-dot"></span><span>${esc(t('tp_offline'))}</span></span>
            <span id="tpCount" class="tp-count"></span>
          </div>
          <div class="tp-header-actions">
            <button class="btn btn-icon btn-sm btn-ghost" id="tpRefreshBtn" title="${esc(t('tp_refresh'))}">${icon('refresh-cw')}</button>
            <button class="btn btn-icon btn-sm btn-ghost" onclick="closeModal('${MODAL_ID}')" title="${esc(t('tp_close'))}">${icon('x')}</button>
          </div>
        </div>
        <div class="tp-toolbar">
          <input type="text" id="tpSearch" class="tp-search" placeholder="${esc(t('tp_search_ph'))}" autocomplete="off" spellcheck="false" />
          <select id="tpGroup" class="tp-select" aria-label="group"><option value="">${esc(t('tp_all_groups'))}</option></select>
          <select id="tpNetwork" class="tp-select" aria-label="network"><option value="">${esc(t('tp_all_networks'))}</option></select>
          <select id="tpHealth" class="tp-select" aria-label="health">
            <option value="">${esc(t('tp_all_health'))}</option>
            <option value="healthy">${esc(t('tp_healthy'))}</option>
            <option value="degraded">${esc(t('tp_degraded'))}</option>
            <option value="down">${esc(t('tp_down'))}</option>
            <option value="unknown">${esc(t('tp_unknown'))}</option>
          </select>
        </div>
        <div class="tp-body">
          <div class="tp-map" id="tpMap">
            <svg id="tpLines" class="tp-lines" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"></svg>
            <div class="tp-columns">
              <section class="tp-column" data-tier="0">
                <div class="tp-column-title">Edge / Network</div>
                <div class="tp-column-nodes" id="tpTier0"></div>
              </section>
              <section class="tp-column" data-tier="1">
                <div class="tp-column-title">Compute Hosts</div>
                <div class="tp-column-nodes" id="tpTier1"></div>
              </section>
              <section class="tp-column" data-tier="2">
                <div class="tp-column-title">Clusters</div>
                <div class="tp-column-nodes" id="tpTier2"></div>
              </section>
            </div>
            <div id="tpEmpty" class="tp-empty" hidden>${esc(t('tp_empty'))}</div>
          </div>
          <aside id="tpDrawer" class="tp-drawer" hidden></aside>
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
    render();
  }
  if (typeof langRerenderHooks !== 'undefined') langRerenderHooks.push(rebuildModal);

  function restoreControls() {
    const pairs = [['tpSearch', S.query], ['tpGroup', S.group], ['tpNetwork', S.network], ['tpHealth', S.health]];
    pairs.forEach(([id, v]) => { const el = document.getElementById(id); if (el) el.value = v; });
  }

  // ---- Filtering ----------------------------------------------------------

  function visibleNodes() {
    if (!S.graph) return [];
    const q = S.query.toLowerCase();
    return S.graph.nodes.filter((n) => {
      if (S.group && n.group !== S.group) return false;
      if (S.network && n.network !== S.network) return false;
      if (S.health && n.status !== S.health) return false;
      if (q && !((n.label || '').toLowerCase().includes(q) || (n.group || '').toLowerCase().includes(q))) return false;
      return true;
    });
  }

  // ---- Rendering (full render only on load/filter; SSE never calls this) ---

  function render() {
    if (!S.graph) return;
    const nodes = visibleNodes();
    const empty = document.getElementById('tpEmpty');
    if (empty) empty.hidden = nodes.length > 0;

    for (let tier = 0; tier <= 2; tier++) {
      const host = document.getElementById('tpTier' + tier);
      if (!host) continue;
      host.innerHTML = nodes.filter((n) => n.tier === tier).map(cardHtml).join('');
    }
    updateFilterOptions();
    const count = document.getElementById('tpCount');
    if (count) count.textContent = nodes.length + ' ' + t('tp_nodes');
    // Lines are drawn after the cards have real layout boxes.
    requestAnimationFrame(drawLines);
    if (S.selected) renderDrawer();
  }

  function cardHtml(n) {
    const isHost = n.kind === 'host';
    const alerts = n.alerts > 0 ? `<span class="tp-alert-badge">${n.alerts}</span>` : '';
    let meta = '';
    if (isHost) {
      const bits = [];
      if (n.metrics && n.metrics.cpu_pct) bits.push(Math.round(n.metrics.cpu_pct) + '% CPU');
      if (n.metrics && n.metrics.ram_pct) bits.push(Math.round(n.metrics.ram_pct) + '% RAM');
      meta = bits.join(' · ') || n.group || '';
    } else {
      meta = n.network || '';
    }
    return `
      <div class="tp-card tp-status-${n.status}${S.selected === n.id ? ' tp-selected' : ''}"
           data-node-id="${esc(n.id)}" data-status="${esc(n.status)}" data-host-id="${isHost ? n.host_id : ''}"
           role="button" tabindex="0">
        <div class="tp-card-top">
          <span class="tp-card-icon">${icon(KIND_ICON[n.kind] || 'circle-dot')}</span>
          <span class="tp-card-name">${esc(n.label)}</span>
          ${alerts}
        </div>
        <div class="tp-card-meta">${esc(meta)}</div>
        <div class="tp-card-foot">
          <span class="tp-status-dot"></span>
          <span class="tp-status-label">${esc(t('tp_' + n.status))}</span>
          ${n.group ? `<span class="tp-card-group">${esc(n.group)}</span>` : ''}
        </div>
      </div>`;
  }

  function updateFilterOptions() {
    const groups = [...new Set(S.graph.nodes.map((n) => n.group).filter(Boolean))].sort();
    const networks = [...new Set(S.graph.nodes.map((n) => n.network).filter(Boolean))].sort();
    fillSelect('tpGroup', groups, t('tp_all_groups'), S.group);
    fillSelect('tpNetwork', networks, t('tp_all_networks'), S.network);
  }

  function fillSelect(id, values, allLabel, current) {
    const el = document.getElementById(id);
    if (!el) return;
    el.innerHTML = `<option value="">${esc(allLabel)}</option>` +
      values.map((v) => `<option value="${esc(v)}">${esc(v)}</option>`).join('');
    el.value = current;
  }

  // ---- Minimal SVG line overlay ------------------------------------------

  function drawLines() {
    const svg = document.getElementById('tpLines');
    const map = document.getElementById('tpMap');
    if (!svg || !map || !S.graph) return;
    const mapRect = map.getBoundingClientRect();
    if (!mapRect.width) return;
    const visible = new Set(visibleNodes().map((n) => n.id));
    const boxes = {};
    map.querySelectorAll('.tp-card').forEach((el) => {
      boxes[el.getAttribute('data-node-id')] = el.getBoundingClientRect();
    });
    let paths = '';
    S.graph.edges.forEach((e) => {
      if (!visible.has(e.source) || !visible.has(e.target)) return;
      const a = boxes[e.source], b = boxes[e.target];
      if (!a || !b) return;
      // Coordinates relative to the map container; account for scroll.
      const x1 = a.right - mapRect.left + map.scrollLeft;
      const y1 = a.top + a.height / 2 - mapRect.top + map.scrollTop;
      const x2 = b.left - mapRect.left + map.scrollLeft;
      const y2 = b.top + b.height / 2 - mapRect.top + map.scrollTop;
      const mid = (x1 + x2) / 2;
      const d = `M ${x1} ${y1} C ${mid} ${y1}, ${mid} ${y2}, ${x2} ${y2}`;
      const alert = hasAlert(e.target) || hasAlert(e.source);
      const cls = 'tp-line tp-line-' + e.kind + (e.active ? '' : ' tp-line-inactive') + (alert ? ' tp-line-alert' : '');
      paths += `<path class="${cls}" d="${d}" />`;
    });
    svg.setAttribute('width', map.scrollWidth);
    svg.innerHTML = paths;
  }

  function hasAlert(nodeId) {
    const n = S.graph && S.graph.nodes.find((x) => x.id === nodeId);
    return !!(n && n.alerts > 0);
  }

  // ---- Drawer with lazy host detail --------------------------------------

  function nodeById(id) {
    return S.graph ? S.graph.nodes.find((n) => n.id === id) : null;
  }

  function openDrawer(id) {
    S.selected = id;
    const map = document.getElementById('tpMap');
    map.querySelectorAll('.tp-card').forEach((el) => el.classList.toggle('tp-selected', el.getAttribute('data-node-id') === id));
    renderDrawer();
    const n = nodeById(id);
    if (n && n.kind === 'host') loadDetail(n);
  }

  function renderDrawer() {
    const drawer = document.getElementById('tpDrawer');
    const n = nodeById(S.selected);
    if (!drawer) return;
    if (!n) { drawer.hidden = true; return; }
    drawer.hidden = false;
    const isHost = n.kind === 'host';
    drawer.innerHTML = `
      <div class="tp-drawer-head">
        <div class="tp-drawer-title"><span class="tp-dot tp-status-${n.status}"></span><strong>${esc(n.label)}</strong></div>
        <button class="btn btn-icon btn-sm btn-ghost" id="tpDrawerClose" title="${esc(t('tp_close'))}">${icon('x')}</button>
      </div>
      <div class="tp-drawer-body">
        <div class="tp-drawer-kind badge badge-${n.status === 'healthy' ? 'healthy' : n.status === 'degraded' ? 'warning' : n.status === 'down' ? 'danger' : 'info'}">${esc(n.kind)}</div>
        ${n.group ? `<p class="tp-drawer-summary">${esc(n.group)}${n.network ? ' · ' + esc(n.network) : ''}</p>` : ''}
        <div class="tp-drawer-actions">
          <button class="btn btn-sm" id="tpDrawerTerminal" ${isHost ? '' : 'disabled'}>${icon('terminal')} ${esc(t('tp_terminal'))}</button>
          <button class="btn btn-sm" id="tpDrawerLogs">${icon('file-text')} ${esc(t('tp_logs'))}</button>
          <button class="btn btn-sm btn-primary" id="tpDrawerWorkspace" ${isHost ? '' : 'disabled'}>${icon('server')} ${esc(t('tp_workspace'))}</button>
        </div>
        ${isHost ? '' : `<p class="tp-drawer-note">${esc(t('tp_no_host'))}</p>`}
        <div id="tpDetail" class="tp-detail">${isHost ? esc(t('tp_loading')) : ''}</div>
      </div>`;
    const close = document.getElementById('tpDrawerClose');
    if (close) close.onclick = () => { S.selected = null; drawer.hidden = true; document.querySelectorAll('.tp-card.tp-selected').forEach((el) => el.classList.remove('tp-selected')); };
    const terminal = document.getElementById('tpDrawerTerminal');
    if (terminal && isHost) terminal.onclick = () => { if (typeof openHostTerminal === 'function') openHostTerminal(n.host_id); };
    const logs = document.getElementById('tpDrawerLogs');
    if (logs) logs.onclick = () => { if (typeof toggleActivityConsole === 'function') toggleActivityConsole(); };
    const workspace = document.getElementById('tpDrawerWorkspace');
    if (workspace && isHost) workspace.onclick = () => { if (typeof openHostDetail === 'function') openHostDetail(n.host_id); };
  }

  async function loadDetail(n) {
    const seq = ++S.detailSeq;
    const box = document.getElementById('tpDetail');
    if (!box) return;
    try {
      const res = await fetch('/api/topology/host?id=' + encodeURIComponent(n.host_id));
      if (!res.ok) throw new Error('HTTP ' + res.status);
      const d = await res.json();
      if (seq !== S.detailSeq) return; // a newer click superseded this one
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

  // ---- SSE: surgical DOM updates, no full re-render ----------------------

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
    if (evt.host) {
      const target = S.graph.nodes.find((n) => n.kind === 'host' && n.label === evt.host);
      if (target) {
        const card = document.querySelector('.tp-card[data-node-id="' + cssEscape(target.id) + '"]');
        if (card) {
          card.classList.add('tp-flash');
          setTimeout(() => card.classList.remove('tp-flash'), 1600);
        }
      }
    }
    // Host status transitions: update only the matching card's dataset + label,
    // never rebuild the grid. Structural changes still refresh via the TTL cache.
    const attrs = evt.attrs || {};
    const nextStatus = attrs.status || (evt.module === 'inspector' ? attrs.host_status : null);
    if (evt.host && nextStatus && ['healthy', 'degraded', 'down', 'unknown'].includes(nextStatus)) {
      const target = S.graph.nodes.find((n) => n.kind === 'host' && n.label === evt.host);
      if (target) {
        target.status = nextStatus;
        const card = document.querySelector('.tp-card[data-node-id="' + cssEscape(target.id) + '"]');
        if (card) {
          card.dataset.status = nextStatus;
          card.className = card.className.replace(/tp-status-\w+/, 'tp-status-' + nextStatus);
          const label = card.querySelector('.tp-status-label');
          if (label) label.textContent = t('tp_' + nextStatus);
        }
      }
    }
    // Structural changes (new alert, network down, inspection run) refresh the
    // cached graph; the cache keeps this cheap.
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
    setLive('offline');
  }
  TP.onClose = disconnectSSE;

  // ---- Events -------------------------------------------------------------

  document.addEventListener('click', (e) => {
    if (!document.getElementById(MODAL_ID)) return;
    const card = e.target.closest && e.target.closest('.tp-card');
    if (card) { openDrawer(card.getAttribute('data-node-id')); return; }
    if (e.target.closest && e.target.closest('#tpRefreshBtn')) load();
  });

  document.addEventListener('input', (e) => {
    if (!e.target || e.target.id !== 'tpSearch') return;
    S.query = e.target.value.trim();
    render();
  });

  document.addEventListener('change', (e) => {
    if (!e.target) return;
    if (e.target.id === 'tpGroup') { S.group = e.target.value; render(); }
    if (e.target.id === 'tpNetwork') { S.network = e.target.value; render(); }
    if (e.target.id === 'tpHealth') { S.health = e.target.value; render(); }
  });

  // Recalculate connectors only on resize / scroll — no animation loop.
  let lineTimer = null;
  function scheduleLines() {
    clearTimeout(lineTimer);
    lineTimer = setTimeout(drawLines, 60);
  }
  window.addEventListener('resize', () => { if (document.getElementById(MODAL_ID)) scheduleLines(); });
  document.addEventListener('scroll', (e) => {
    if (e.target && e.target.id === 'tpMap') scheduleLines();
  }, true);

  TP.open = function () {
    ensureModal();
    restoreControls();
    openModal(MODAL_ID);
    connectSSE();
    load();
  };
})();
