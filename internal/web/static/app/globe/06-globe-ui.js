/* Globe Radar — panels, HUD, data and live updates (ADR 0090).
 * Continuation of the GlobeRadar controller. */
(function () {
  'use strict';

  var GL = window.GlobeRadar;
  var G = window.WebGlobe;
  var S = GL.state;
  var esc = function (s) { return escapeHtml(String(s === null || s === undefined ? '' : s)); };
  var icon = function (n) { return svgIcon(n, 'icon-sm'); };

  // ---- UI rendering --------------------------------------------------------
  GL.renderUI = function () {
    GL.renderPills();
    GL.renderGroups();
    GL.renderSidebar();
    GL.renderCount();
    if (S.selected) GL.renderHud();
    var empty = document.getElementById('glEmpty');
    if (empty) {
      var failed = !!(S.renderer && !S.renderer.ok);
      empty.classList.toggle('gl-fail', failed);
      empty.textContent = t(failed ? 'gl_webgl_failed' : 'gl_empty');
      empty.hidden = !!(S.renderer && S.renderer.ok) && (!S.graph || S.graph.nodes.length > 1);
    }
    S.sceneDirty = S.tintsDirty = true;
  };

  // What the filters let through: nodes by search/group, links by type and both ends being shown. The header
  // count and the renderer share this one answer.
  GL.filtered = function () {
    var nodes = S.graph.nodes.filter(GL.visible), shown = {};
    nodes.forEach(function (n) { shown[n.id] = true; });
    var links = S.graph.links.filter(function (l) { return GL.linkVisible(l) && shown[l.source_id] && shown[l.target_id]; });
    return { nodes: nodes, links: links };
  };

  // ---- Renderer sync (applied by the frame loop, so bursts of changes cost one upload) ----
  GL.syncScene = function () {
    S.sceneDirty = false;
    if (!S.renderer || !S.renderer.ok || !S.graph) return;
    var f = GL.filtered();
    S.renderer.setScene(f.nodes, f.links, S.selected);
  };

  // Countries that hold a managed Host get a soft accent wash; the hovered and the selected host's country
  // get stronger fills with an outline.
  GL.syncTints = function () {
    S.tintsDirty = false;
    if (!S.renderer || !S.renderer.ok || !S.graph) return;
    var seen = {}, items = [];
    S.graph.nodes.forEach(function (n) {
      if (n.kind !== 'host' || n._ci < 0 || seen[n._ci] || !GL.visible(n)) return;
      seen[n._ci] = true;
      items.push({ idx: n._ci, fill: GL.rgba('--primary', '#5e6ad2', 0.28) });
    });
    var edge = GL.rgba('--accent-text', '#9aa3f8', 0.95);
    if (S.hoverCountry >= 0) items.push({ idx: S.hoverCountry, fill: 'rgba(255,255,255,0.08)', stroke: edge, width: 1.4 });
    var sel = GL.nodeById(S.selected);
    if (sel && sel._ci >= 0) items.push({ idx: sel._ci, fill: GL.rgba('--primary', '#5e6ad2', 0.36), stroke: edge, width: 2 });
    S.renderer.setTints(items);
  };

  // Each node learns its country once per load (and after a geo override). A Host is on land, so it may sit up to
  // COAST_SLACK degrees (~30 km) outside the simplified coastline and still belong to that country.
  var COAST_SLACK = 0.3;
  function countryOf(lat, lon) { return G.countryNear(lat, lon, COAST_SLACK); }
  function indexCountries(nodes) {
    nodes.forEach(function (n) { n._ci = countryOf(n.lat, n.lon); });
  }

  GL.countryHosts = function (idx) {
    if (!S.graph) return 0;
    return S.graph.nodes.filter(function (n) { return n.kind === 'host' && n._ci === idx && GL.visible(n); }).length;
  };

  GL.renderPills = function () {
    var box = document.getElementById('glFlowPills');
    if (!box) return;
    var defs = [
      ['all', 'gl_all'], ['proxy', 'gl_proxy'], ['vpn', 'gl_vpn'], ['k8s', 'gl_k8s'], ['gateway', 'gl_gateway'],
    ];
    box.innerHTML = defs.map(function (d) {
      return '<button type="button" class="gl-pill' + (S.flowFilter === d[0] ? ' active' : '') + '" data-flow="' + d[0] + '">' + esc(t(d[1])) + '</button>';
    }).join('');
  };

  GL.renderGroups = function () {
    var sel = document.getElementById('glGroup');
    if (!sel || !S.graph) return;
    var groups = [];
    S.graph.nodes.forEach(function (n) { if (n.group_id && groups.indexOf(n.group_id) < 0) groups.push(n.group_id); });
    groups.sort();
    var cur = S.group;
    sel.innerHTML = '<option value="">' + esc(t('gl_all_groups')) + '</option>' +
      groups.map(function (g) { return '<option value="' + esc(g) + '">' + esc(g) + '</option>'; }).join('');
    sel.value = cur;
  };

  GL.renderSidebar = function () {
    var box = document.getElementById('glSideList');
    if (!box || !S.graph) return;
    var hosts = S.graph.nodes.filter(function (n) { return n.kind === 'host' && GL.visible(n); });
    box.innerHTML = hosts.map(function (n) {
      return '<button type="button" class="gl-side-item' + (S.selected === n.id ? ' active' : '') + '" data-node="' + esc(n.id) + '">' +
        '<span class="gl-side-dot ' + esc(n.status) + '"></span>' +
        '<span class="gl-side-name">' + esc(n.name) + '</span>' +
        '<span class="gl-side-meta">' + esc(n.city || n.region || n.group_id || '') + '</span>' +
      '</button>';
    }).join('');
  };

  GL.renderCount = function () {
    var c = document.getElementById('glCount');
    if (!c || !S.graph) return;
    var f = GL.filtered();
    c.textContent = f.nodes.length + ' ' + t('gl_nodes') + ' · ' + f.links.length + ' ' + t('gl_links');
  };

  // ---- HUD -----------------------------------------------------------------
  GL.openHud = function (id, doFly) {
    S.selected = id;
    var n = GL.nodeById(id);
    if (n && doFly) GL.flyTo(n);
    GL.renderHud();
    GL.renderSidebar();
    S.sceneDirty = S.tintsDirty = true;
  };

  GL.closeHud = function () {
    S.selected = null;
    var hud = document.getElementById('glHud');
    if (hud) hud.hidden = true;
    GL.renderSidebar();
    S.sceneDirty = S.tintsDirty = true;
  };

  GL.renderHud = function () {
    var hud = document.getElementById('glHud');
    var n = GL.nodeById(S.selected);
    if (!hud || !n) return;
    hud.hidden = false;
    var isHost = n.kind === 'host';
    var lat = n.lat ? n.lat.toFixed(2) : '0.00';
    var lon = n.lon ? n.lon.toFixed(2) : '0.00';
    var country = n._ci >= 0 ? G.countryName(G.countries()[n._ci], document.documentElement.lang || 'en') : (n.country_code || '');
    hud.innerHTML = '' +
      '<div class="gl-hud-head">' +
        '<div class="gl-hud-title"><span class="gl-hud-dot ' + esc(n.status) + '"></span><strong>' + esc(n.name) + '</strong></div>' +
        '<button class="btn btn-icon btn-sm btn-ghost" id="glHudClose" title="' + esc(t('gl_close')) + '">' + icon('x') + '</button>' +
      '</div>' +
      '<div class="gl-hud-body">' +
        '<div class="gl-hud-stats">' +
          '<span><b>' + esc(t('gl_geo')) + '</b> ' + esc(n.city || '—') + (country ? ', ' + esc(country) : '') + '</span>' +
          '<span><b>' + esc(t('gl_region')) + '</b> ' + esc(n.region || '—') + '</span>' +
          '<span><b>' + esc(t('gl_latency')) + '</b> ' + (n.latency_ms ? esc(n.latency_ms) + ' ms' : '—') + '</span>' +
          '<span><b>' + esc(t('gl_peer_count')) + '</b> ' + GL.peerCount(n.id) + '</span>' +
          '<span><b>' + esc(t('gl_lat')) + '</b> ' + lat + '</span>' +
          '<span><b>' + esc(t('gl_lon')) + '</b> ' + lon + '</span>' +
        '</div>' +
        '<div class="gl-hud-summary">' + esc(n.public_ip || n.ip || '') + (n.asn_org ? ' · ' + esc(n.asn_org) : '') + (n.geo_source ? ' · ' + esc(t('gl_geo_source')) + ': ' + esc(n.geo_source) : '') + '</div>' +
        (n.active_alerts ? '<div class="gl-hud-note">' + n.active_alerts + ' ' + esc(t('gl_alerts')) + '</div>' : '') +
        '<div class="gl-hud-actions">' +
          '<button class="btn btn-sm" id="glHudTerminal" ' + (isHost ? '' : 'disabled') + '>' + icon('terminal') + ' ' + esc(t('gl_terminal')) + '</button>' +
          '<button class="btn btn-sm" id="glHudDiag" ' + (isHost ? '' : 'disabled') + '>' + icon('activity') + ' ' + esc(t('gl_diag')) + '</button>' +
          '<button class="btn btn-sm btn-primary" id="glHudWorkspace" ' + (isHost ? '' : 'disabled') + '>' + icon('server') + ' ' + esc(t('gl_workspace')) + '</button>' +
        '</div>' +
        (isHost ? '' : '<div class="gl-hud-note">' + esc(t('gl_no_host')) + '</div>') +
        (isHost ? '<div class="gl-geo-form">' +
          '<label>' + esc(t('gl_lat')) + '<input id="glGeoLat" type="number" step="any" value="' + lat + '" /></label>' +
          '<label>' + esc(t('gl_lon')) + '<input id="glGeoLon" type="number" step="any" value="' + lon + '" /></label>' +
          '<label class="gl-geo-wide">' + esc(t('gl_region')) + '<input id="glGeoRegion" type="text" value="' + esc(n.region || '') + '" /></label>' +
          '<label class="gl-geo-wide">' + esc(t('gl_label')) + '<input id="glGeoLabel" type="text" value="' + esc(n.city || '') + '" /></label>' +
          '<button class="btn btn-sm gl-geo-wide" id="glGeoSave">' + esc(t('gl_setgeo')) + '</button>' +
          '<div class="gl-geo-wide gl-side-meta">' + esc(t('gl_geo_hint')) + '</div>' +
        '</div>' : '') +
      '</div>';
    document.getElementById('glHudClose').onclick = GL.closeHud;
    var term = document.getElementById('glHudTerminal');
    if (term && isHost) term.onclick = function () { if (typeof openHostTerminal === 'function') openHostTerminal(n.host_id); };
    var diag = document.getElementById('glHudDiag');
    if (diag && isHost) diag.onclick = function () { if (typeof openNetDiagModal === 'function') openNetDiagModal(n.host_id); };
    var ws = document.getElementById('glHudWorkspace');
    if (ws && isHost) ws.onclick = function () { if (typeof openHostDetail === 'function') openHostDetail(n.host_id); };
    var save = document.getElementById('glGeoSave');
    if (save && isHost) save.onclick = function () { GL.saveGeo(n); };
  };

  GL.saveGeo = function (n) {
    var lat = parseFloat(document.getElementById('glGeoLat').value);
    var lon = parseFloat(document.getElementById('glGeoLon').value);
    var region = document.getElementById('glGeoRegion').value || '';
    var label = document.getElementById('glGeoLabel').value || '';
    if (isNaN(lat) || isNaN(lon)) return;
    fetch('/api/network-radar/globe/geo', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id: n.host_id, lat: lat, lon: lon, region: region, label: label }),
    }).then(function (res) {
      if (!res.ok) return;
      n.lat = lat; n.lon = lon; n.region = region; n.city = label; n.geo_source = 'manual';
      n._ci = countryOf(lat, lon);
      GL.renderUI();
    }).catch(function () { /* offline: keep current position */ });
  };

  // ---- Data loading --------------------------------------------------------
  GL.load = function () {
    return fetch('/api/network-radar/globe').then(function (res) {
      if (!res.ok) throw new Error('HTTP ' + res.status);
      return res.json();
    }).then(function (g) {
      indexCountries(g.nodes);
      S.graph = g;
      GL.renderUI();
    }).catch(function (err) {
      var c = document.getElementById('glCount');
      if (c) c.textContent = t('gl_failed');
      console.error('globe load failed', err);
    });
  };

  // ---- SSE: in-place health/pulse updates (ADR 0030) ----------------------
  function setLive(state) {
    var pill = document.getElementById('glLivePill');
    if (!pill) return;
    pill.className = 'gl-live-pill gl-' + state;
    var label = pill.querySelector('span:last-child');
    if (label) label.textContent = state === 'live' ? t('gl_live') : state === 'reconnecting' ? t('gl_reconnecting') : t('gl_offline');
  }

  function handleEvent(evt) {
    if (!S.graph) return;
    var attrs = evt.attrs || {};
    var status = attrs.status || (evt.module === 'inspector' ? attrs.host_status : null);
    if (status && ['healthy', 'degraded', 'down', 'unknown'].indexOf(status) >= 0) {
      var target = evt.host ? S.graph.nodes.find(function (n) { return n.kind === 'host' && n.name === evt.host; }) : null;
      if (target) {
        target.status = status;
        if (S.selected === target.id) GL.renderHud();
        GL.renderSidebar();
        S.sceneDirty = true;
      }
    }
    var msg = (evt.message || '').toLowerCase();
    if (msg.indexOf('drift') >= 0 || msg.indexOf('alert') >= 0 || msg.indexOf('inspection') >= 0) GL.load();
    // Burst traffic on active inspections (visual only; no scene rebuild).
    if (evt.module === 'inspector' || msg.indexOf('probe') >= 0) S.burst = performance.now() + 1200;
  }

  function connectSSE() {
    if (S.source || typeof EventSource === 'undefined') return;
    try {
      S.source = new EventSource('/api/logs/stream');
      S.source.onopen = function () { setLive('live'); };
      S.source.onmessage = function (e) {
        if (!e.data) return;
        var evt; try { evt = JSON.parse(e.data); } catch (_) { return; }
        handleEvent(evt);
      };
      S.source.onerror = function () { setLive('reconnecting'); };
    } catch (_) { setLive('offline'); }
  }

  function disconnectSSE() {
    if (S.source) { S.source.close(); S.source = null; }
    setLive('offline');
    S.mounted = false;
    S.hoverPos = null;
    S.hoverCountry = -1;
    GL.stopLoop();
    if (S.renderer) { S.renderer.dispose(); S.renderer = null; }
  }
  GL.onClose = disconnectSSE;

  // ---- Open lifecycle ------------------------------------------------------
  GL.open = function () {
    GL.ensureModal();
    GL.restoreControls();
    openModal(GL.modalId);
    S.mounted = true;
    var canvas = document.getElementById('glCanvas');
    if (canvas && !S.renderer) {
      S.renderer = new window.WebGlobe.Renderer(canvas);
      if (!S.renderer.ok) GL.renderUI(); // shows the WebGL2 message in the stage
    }
    GL.bindCanvas();
    connectSSE();
    GL.load();
    GL.startLoop();
  };
})();
