/* 3D Globe Network Radar (ADR 0090) — pure Canvas 2D orthographic renderer.
 * No Three.js / Mapbox / CDN (ADR 0013). Land dots from window.GLOBE_LAND,
 * great-circle arcs with altitude curves, animated packets, drag-to-rotate
 * with inertia, wheel zoom, auto-rotation, camera fly-to, SSE updates.
 * Relies on globals: svgIcon, escapeHtml, t, I18N, openModal, closeModal,
 * openHostDetail, openHostTerminal, toggleActivityConsole. */
(function () {
  'use strict';

  var GL = (window.GlobeRadar = {});
  var MODAL_ID = 'globeModal';
  var DEG = Math.PI / 180;

  var S = {
    graph: null,
    canvas: null,
    ctx: null,
    dpr: 1,
    flowFilter: 'all',
    group: '',
    query: '',
    selected: null,
    hovered: null,
    autoRotate: true,
    yaw: -0.4,          // rotation around vertical axis (radians)
    pitch: 0.35,        // tilt (radians)
    zoom: 1,
    velYaw: 0,
    velPitch: 0,
    dragging: false,
    lastPointer: null,
    raf: 0,
    lastFrame: 0,
    source: null,
    fly: null,
    packets: [],
    mounted: false,
  };

  var esc = function (s) { return escapeHtml(String(s === null || s === undefined ? '' : s)); };
  var icon = function (n) { return svgIcon(n, 'icon-sm'); };

  var I = {
    en: {
      gl_title: '3D Globe Network Radar', gl_subtitle: 'Geo-located fleet & live traffic arcs',
      gl_all: 'All', gl_proxy: 'Proxy/App', gl_vpn: 'VPN', gl_k8s: 'K8s', gl_gateway: 'Gateway',
      gl_all_groups: 'All groups', gl_search_ph: 'Find a host…', gl_autorotate: 'Auto-rotate',
      gl_refresh: 'Refresh', gl_close: 'Close', gl_nodes: 'nodes', gl_flows: 'flows',
      gl_empty: 'No infrastructure yet. Add a Host to see it on the globe.',
      gl_loading: 'Loading globe…', gl_failed: 'Could not load globe',
      gl_healthy: 'Healthy', gl_degraded: 'Degraded', gl_down: 'Down', gl_unknown: 'Unknown',
      gl_terminal: 'Terminal', gl_diag: 'Diagnostics', gl_workspace: 'Host Workspace',
      gl_no_host: 'This node is not a managed Host.', gl_alerts: 'alerts',
      gl_latency: 'Latency', gl_geo: 'Geo', gl_lat: 'Lat', gl_lon: 'Lon', gl_label: 'Label',
      gl_setgeo: 'Set override', gl_geo_hint: 'Manual override for private / air-gapped hosts',
      gl_live: 'LIVE', gl_reconnecting: 'RECONNECTING', gl_offline: 'OFFLINE',
      gl_zoom_in: 'Zoom in', gl_zoom_out: 'Zoom out', gl_zoom_reset: 'Reset view',
      gl_geo_source: 'Geo source',
    },
    id: {
      gl_title: 'Radar Jaringan 3D', gl_subtitle: 'Armada ber-geolokasi & arc lalu lintas langsung',
      gl_all: 'Semua', gl_proxy: 'Proxy/Aplikasi', gl_vpn: 'VPN', gl_k8s: 'K8s', gl_gateway: 'Gateway',
      gl_all_groups: 'Semua grup', gl_search_ph: 'Cari host…', gl_autorotate: 'Putar otomatis',
      gl_refresh: 'Muat ulang', gl_close: 'Tutup', gl_nodes: 'node', gl_flows: 'alur',
      gl_empty: 'Belum ada infrastruktur. Tambahkan Host untuk melihatnya di globe.',
      gl_loading: 'Memuat globe…', gl_failed: 'Gagal memuat globe',
      gl_healthy: 'Sehat', gl_degraded: 'Degraded', gl_down: 'Down', gl_unknown: 'Unknown',
      gl_terminal: 'Terminal', gl_diag: 'Diagnostik', gl_workspace: 'Workspace Host',
      gl_no_host: 'Node ini bukan Host yang dikelola.', gl_alerts: 'alert',
      gl_latency: 'Latensi', gl_geo: 'Geo', gl_lat: 'Lat', gl_lon: 'Lon', gl_label: 'Label',
      gl_setgeo: 'Simpan override', gl_geo_hint: 'Override manual untuk host privat / air-gapped',
      gl_live: 'LANGSUNG', gl_reconnecting: 'MENGHUBUNGKAN', gl_offline: 'OFFLINE',
      gl_zoom_in: 'Perbesar', gl_zoom_out: 'Perkecil', gl_zoom_reset: 'Atur ulang',
      gl_geo_source: 'Sumber geo',
    },
  };
  ['en', 'id'].forEach(function (l) { I18N[l] = Object.assign(I18N[l] || {}, I[l]); });

  // ---- Modal shell (ADR 0007 Workspace Modal) -----------------------------
  function modalHtml() {
    return '' +
      '<div class="modal-content modal-workspace gl-modal-content">' +
        '<div class="modal-header gl-header">' +
          '<div class="gl-title-group">' + icon('globe') +
            '<h3 class="gl-title">' + esc(t('gl_title')) + '</h3>' +
            '<span class="gl-subtitle">' + esc(t('gl_subtitle')) + '</span>' +
            '<span id="glLivePill" class="gl-live-pill gl-offline"><span class="gl-live-dot"></span><span>' + esc(t('gl_offline')) + '</span></span>' +
            '<span id="glCount" class="gl-count"></span>' +
          '</div>' +
          '<div class="gl-header-actions">' +
            '<button class="btn btn-icon btn-sm btn-ghost" id="glZoomIn" title="' + esc(t('gl_zoom_in')) + '">' + icon('plus') + '</button>' +
            '<button class="btn btn-icon btn-sm btn-ghost" id="glZoomOut" title="' + esc(t('gl_zoom_out')) + '">' + icon('minus') + '</button>' +
            '<button class="btn btn-icon btn-sm btn-ghost" id="glZoomReset" title="' + esc(t('gl_zoom_reset')) + '">' + icon('rotate-ccw') + '</button>' +
            '<button class="btn btn-icon btn-sm btn-ghost" id="glRefreshBtn" title="' + esc(t('gl_refresh')) + '">' + icon('refresh-cw') + '</button>' +
            '<button class="btn btn-icon btn-sm btn-ghost" onclick="closeModal(\'' + MODAL_ID + '\')" title="' + esc(t('gl_close')) + '">' + icon('x') + '</button>' +
          '</div>' +
        '</div>' +
        '<div class="gl-toolbar">' +
          '<input type="text" id="glSearch" class="gl-search" placeholder="' + esc(t('gl_search_ph')) + '" autocomplete="off" spellcheck="false" />' +
          '<div id="glFlowPills" class="gl-pills" role="group" aria-label="flow type"></div>' +
          '<select id="glGroup" class="gl-select"><option value="">' + esc(t('gl_all_groups')) + '</option></select>' +
          '<label class="gl-toggle"><input type="checkbox" id="glAutoRotate" checked /> ' + esc(t('gl_autorotate')) + '</label>' +
        '</div>' +
        '<div class="gl-body">' +
          '<div class="gl-stage" id="glStage">' +
            '<canvas id="glCanvas" class="gl-canvas" role="img" aria-label="' + esc(t('gl_title')) + '"></canvas>' +
            '<div id="glEmpty" class="gl-empty" hidden>' + esc(t('gl_empty')) + '</div>' +
            '<aside id="glHud" class="gl-hud" hidden></aside>' +
          '</div>' +
          '<aside id="glSidebar" class="gl-sidebar"><div class="gl-side-title">Hosts</div><div id="glSideList"></div></aside>' +
        '</div>' +
      '</div>';
  }

  function ensureModal() {
    if (document.getElementById(MODAL_ID)) return;
    var el = document.createElement('div');
    el.id = MODAL_ID;
    el.className = 'modal';
    el.innerHTML = modalHtml();
    document.body.appendChild(el);
  }

  function rebuildModal() {
    var m = document.getElementById(MODAL_ID);
    if (!m) return;
    var search = document.getElementById('glSearch');
    if (search) S.query = search.value;
    var auto = document.getElementById('glAutoRotate');
    if (auto) S.autoRotate = auto.checked;
    m.innerHTML = modalHtml();
    restoreControls();
    bindCanvas();
    renderUI();
  }
  if (typeof langRerenderHooks !== 'undefined') langRerenderHooks.push(rebuildModal);

  function restoreControls() {
    var search = document.getElementById('glSearch');
    if (search) search.value = S.query;
    var auto = document.getElementById('glAutoRotate');
    if (auto) auto.checked = S.autoRotate;
    var group = document.getElementById('glGroup');
    if (group) group.value = S.group;
  }

  // ---- Projection math ----------------------------------------------------
  // Rotate (lon,lat) by yaw/pitch, then orthographic project to a unit sphere.
  function project(lat, lon) {
    var la = lat * DEG, lo = lon * DEG;
    // Point on unit sphere (y up).
    var x = Math.cos(la) * Math.sin(lo);
    var y = Math.sin(la);
    var z = Math.cos(la) * Math.cos(lo);
    // Yaw around vertical axis.
    var cy = Math.cos(S.yaw), sy = Math.sin(S.yaw);
    var x1 = x * cy - z * sy;
    var z1 = x * sy + z * cy;
    // Pitch around horizontal axis.
    var cp = Math.cos(S.pitch), sp = Math.sin(S.pitch);
    var y2 = y * cp - z1 * sp;
    var z2 = y * sp + z1 * cp;
    return { x: x1, y: y2, z: z2 }; // z>0 = front hemisphere
  }

  function visible(node) {
    if (S.group && (node.group_id || '') !== S.group) return false;
    if (S.query) {
      var q = S.query.toLowerCase();
      if (!((node.name || '').toLowerCase().indexOf(q) >= 0 ||
            (node.ip || '').toLowerCase().indexOf(q) >= 0 ||
            (node.city || '').toLowerCase().indexOf(q) >= 0)) return false;
    }
    return true;
  }

  function flowVisible(flow) {
    if (S.flowFilter === 'all') return true;
    if (S.flowFilter === 'gateway') return flow.type === 'ssh' || flow.type === 'vpn';
    return flow.type === S.flowFilter;
  }

  // ---- Canvas rendering ---------------------------------------------------
  function resizeCanvas() {
    var canvas = document.getElementById('glCanvas');
    var stage = document.getElementById('glStage');
    if (!canvas || !stage) return;
    S.dpr = Math.min(window.devicePixelRatio || 1, 2);
    var w = stage.clientWidth, h = stage.clientHeight;
    canvas.width = Math.max(1, Math.floor(w * S.dpr));
    canvas.height = Math.max(1, Math.floor(h * S.dpr));
    canvas.style.width = w + 'px';
    canvas.style.height = h + 'px';
    S.canvas = canvas;
    S.ctx = canvas.getContext('2d');
  }

  function css(name) {
    return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || '#888';
  }

  function drawFrame(ts) {
    if (!S.mounted) return;
    var dt = S.lastFrame ? Math.min(50, ts - S.lastFrame) : 16;
    S.lastFrame = ts;

    // Inertia + auto-rotation.
    if (!S.dragging) {
      S.yaw += S.velYaw * dt * 0.06;
      S.pitch += S.velPitch * dt * 0.06;
      S.velYaw *= 0.92; S.velPitch *= 0.92;
      if (S.autoRotate && !S.fly) S.yaw += 0.00012 * dt;
      S.pitch = Math.max(-1.2, Math.min(1.2, S.pitch));
    }
    // Fly-to animation (ease-out toward target yaw/pitch/zoom).
    if (S.fly) {
      var k = 0.12;
      S.yaw += (S.fly.yaw - S.yaw) * k;
      S.pitch += (S.fly.pitch - S.pitch) * k;
      S.zoom += (S.fly.zoom - S.zoom) * k;
      if (Math.abs(S.fly.yaw - S.yaw) < 0.002 && Math.abs(S.fly.pitch - S.pitch) < 0.002) S.fly = null;
    }

    var canvas = S.canvas, ctx = S.ctx;
    if (canvas && ctx) {
      var w = canvas.width, h = canvas.height;
      ctx.clearRect(0, 0, w, h);
      var R = Math.min(w, h) * 0.42 * S.zoom;
      var cx = w / 2, cy = h / 2;

      // Globe disc + subtle atmosphere.
      var grad = ctx.createRadialGradient(cx - R * 0.3, cy - R * 0.3, R * 0.1, cx, cy, R);
      grad.addColorStop(0, css('--surface-2'));
      grad.addColorStop(1, css('--bg'));
      ctx.beginPath(); ctx.arc(cx, cy, R, 0, Math.PI * 2);
      ctx.fillStyle = grad; ctx.fill();
      ctx.strokeStyle = css('--card-border'); ctx.lineWidth = 1.5 * S.dpr; ctx.stroke();

      drawGraticule(ctx, cx, cy, R);
      drawLand(ctx, cx, cy, R);
      drawFlows(ctx, cx, cy, R, ts);
      drawNodes(ctx, cx, cy, R, ts);
    }
    S.raf = requestAnimationFrame(drawFrame);
  }

  function drawGraticule(ctx, cx, cy, R) {
    ctx.save();
    ctx.strokeStyle = css('--card-border');
    ctx.globalAlpha = 0.45;
    ctx.lineWidth = 0.7 * S.dpr;
    // Latitudes every 30 degrees.
    for (var lat = -60; lat <= 60; lat += 30) {
      drawParallel(ctx, cx, cy, R, lat);
    }
    // Longitudes every 30 degrees.
    for (var lon = -180; lon < 180; lon += 30) {
      drawMeridian(ctx, cx, cy, R, lon);
    }
    ctx.restore();
  }

  function drawParallel(ctx, cx, cy, R, lat) {
    var started = false;
    ctx.beginPath();
    for (var lon = -180; lon <= 180; lon += 6) {
      var p = project(lat, lon);
      if (p.z <= 0) { started = false; continue; }
      var x = cx + p.x * R, y = cy - p.y * R;
      if (!started) { ctx.moveTo(x, y); started = true; } else ctx.lineTo(x, y);
    }
    ctx.stroke();
  }

  function drawMeridian(ctx, cx, cy, R, lon) {
    var started = false;
    ctx.beginPath();
    for (var lat = -90; lat <= 90; lat += 6) {
      var p = project(lat, lon);
      if (p.z <= 0) { started = false; continue; }
      var x = cx + p.x * R, y = cy - p.y * R;
      if (!started) { ctx.moveTo(x, y); started = true; } else ctx.lineTo(x, y);
    }
    ctx.stroke();
  }

  function drawLand(ctx, cx, cy, R) {
    var dots = window.GLOBE_LAND || [];
    var front = css('--border-strong');
    var back = css('--card-border');
    for (var i = 0; i < dots.length; i++) {
      var p = project(dots[i][0], dots[i][1]);
      var x = cx + p.x * R, y = cy - p.y * R;
      var frontFacing = p.z > 0;
      ctx.beginPath();
      ctx.arc(x, y, (frontFacing ? 1.15 : 0.8) * S.dpr, 0, Math.PI * 2);
      ctx.fillStyle = frontFacing ? front : back;
      ctx.globalAlpha = frontFacing ? 0.85 : 0.22;
      ctx.fill();
    }
    ctx.globalAlpha = 1;
  }

  var FLOW_COLOR = {
    ssh: function () { return css('--success'); },
    proxy: function () { return css('--info'); },
    vpn: function () { return css('--accent-text'); },
    k8s: function () { return css('--warning'); },
    cloudflare: function () { return css('--primary'); },
  };

  function nodeById(id) {
    return S.graph ? S.graph.nodes.find(function (n) { return n.id === id; }) : null;
  }

  function drawFlows(ctx, cx, cy, R, ts) {
    if (!S.graph) return;
    var byId = {};
    S.graph.nodes.forEach(function (n) { byId[n.id] = n; });
    S.graph.flows.forEach(function (f) {
      if (!flowVisible(f)) return;
      var a = byId[f.source], b = byId[f.target];
      if (!a || !b || !visible(a) || !visible(b)) return;
      var pa = project(a.lat, a.lon), pb = project(b.lat, b.lon);
      if (pa.z <= 0 && pb.z <= 0) return; // both on the far side
      var ax = cx + pa.x * R, ay = cy - pa.y * R;
      var bx = cx + pb.x * R, by = cy - pb.y * R;
      // Elevate the arc: lift the midpoint outward from the globe center.
      var mx = (ax + bx) / 2, my = (ay + by) / 2;
      var dx = mx - cx, dy = my - cy;
      var len = Math.hypot(dx, dy) || 1;
      var lift = R * 0.22 + Math.hypot(bx - ax, by - ay) * 0.12;
      var qx = mx + (dx / len) * lift, qy = my + (dy / len) * lift;
      var color = (FLOW_COLOR[f.type] || FLOW_COLOR.ssh)();
      ctx.beginPath();
      ctx.moveTo(ax, ay);
      ctx.quadraticCurveTo(qx, qy, bx, by);
      ctx.strokeStyle = color;
      ctx.globalAlpha = (pa.z > 0 && pb.z > 0) ? 0.55 : 0.2;
      ctx.lineWidth = (f.status === 'down' ? 1.6 : 1.2) * S.dpr;
      ctx.setLineDash(f.active ? [] : [5 * S.dpr, 5 * S.dpr]);
      ctx.stroke();
      ctx.setLineDash([]);
      ctx.globalAlpha = 1;

      // Animated packet pulse travelling along visible arcs.
      if (pa.z > 0 && pb.z > 0) {
        var phase = ((ts * 0.00025) + hash(f.id)) % 1;
        var px = quad(ax, qx, bx, phase), py = quad(ay, qy, by, phase);
        ctx.beginPath();
        ctx.arc(px, py, 2.4 * S.dpr, 0, Math.PI * 2);
        ctx.fillStyle = color;
        ctx.shadowColor = color;
        ctx.shadowBlur = 8 * S.dpr;
        ctx.fill();
        ctx.shadowBlur = 0;
      }
    });
  }

  function quad(a, c, b, t) {
    var mt = 1 - t;
    return mt * mt * a + 2 * mt * t * c + t * t * b;
  }

  function hash(s) {
    var h = 0;
    for (var i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) % 1000;
    return h / 1000;
  }

  function drawNodes(ctx, cx, cy, R, ts) {
    if (!S.graph) return;
    S.graph.nodes.forEach(function (n) {
      if (!visible(n)) return;
      var p = project(n.lat, n.lon);
      var front = p.z > 0;
      var x = cx + p.x * R, y = cy - p.y * R;
      var color = n.status === 'healthy' ? css('--success') :
                  n.status === 'degraded' ? css('--warning') :
                  n.status === 'down' ? css('--danger') : css('--text-muted');
      var size = (n.kind === 'gateway' ? 6 : 4.5) * S.dpr;
      ctx.globalAlpha = front ? 1 : 0.28;
      // Pulse ring for degraded / alerting hosts.
      if (n.active_alerts > 0 || n.status === 'degraded' || n.status === 'down') {
        var pulse = 1 + 0.5 * ((Math.sin(ts * 0.004) + 1) / 2);
        ctx.beginPath();
        ctx.arc(x, y, size * 2.2 * pulse, 0, Math.PI * 2);
        ctx.strokeStyle = color;
        ctx.globalAlpha = front ? 0.35 : 0.1;
        ctx.lineWidth = 1.2 * S.dpr;
        ctx.stroke();
        ctx.globalAlpha = front ? 1 : 0.28;
      }
      ctx.beginPath();
      ctx.arc(x, y, size, 0, Math.PI * 2);
      ctx.fillStyle = color;
      ctx.shadowColor = color;
      ctx.shadowBlur = (S.selected === n.id || front ? 10 : 4) * S.dpr;
      ctx.fill();
      ctx.shadowBlur = 0;
      if (front && (n.kind === 'gateway' || S.selected === n.id || S.hovered === n.id)) {
        ctx.fillStyle = css('--text');
        ctx.font = (11 * S.dpr) + 'px ' + css('--font-sans');
        ctx.fillText(n.name, x + size + 5 * S.dpr, y + 4 * S.dpr);
      }
      ctx.globalAlpha = 1;
    });
  }

  // ---- Picking ------------------------------------------------------------
  function pickNode(clientX, clientY) {
    if (!S.graph || !S.canvas) return null;
    var rect = S.canvas.getBoundingClientRect();
    var w = S.canvas.width, h = S.canvas.height;
    var R = Math.min(w, h) * 0.42 * S.zoom;
    var cx = w / 2, cy = h / 2;
    var x = (clientX - rect.left) / rect.width * w;
    var y = (clientY - rect.top) / rect.height * h;
    var best = null, bestD = 18 * S.dpr;
    S.graph.nodes.forEach(function (n) {
      if (!visible(n)) return;
      var p = project(n.lat, n.lon);
      if (p.z <= 0) return;
      var nx = cx + p.x * R, ny = cy - p.y * R;
      var d = Math.hypot(nx - x, ny - y);
      if (d < bestD) { bestD = d; best = n; }
    });
    return best;
  }

  // ---- Camera fly-to ------------------------------------------------------
  function flyTo(node) {
    // Solve for yaw/pitch that brings a lat/lon to the front-center.
    var la = node.lat * DEG, lo = node.lon * DEG;
    var targetYaw = -lo;
    var targetPitch = la;
    // Normalize yaw to nearest equivalent angle.
    while (targetYaw - S.yaw > Math.PI) targetYaw -= Math.PI * 2;
    while (targetYaw - S.yaw < -Math.PI) targetYaw += Math.PI * 2;
    S.fly = { yaw: targetYaw, pitch: Math.max(-1.2, Math.min(1.2, targetPitch)), zoom: Math.max(S.zoom, 1.25) };
  }

  // ---- UI rendering -------------------------------------------------------
  function renderUI() {
    renderPills();
    renderGroups();
    renderSidebar();
    renderCount();
    if (S.selected) renderHud();
    var empty = document.getElementById('glEmpty');
    if (empty) empty.hidden = !S.graph || S.graph.nodes.length > 1;
  }

  function renderPills() {
    var box = document.getElementById('glFlowPills');
    if (!box) return;
    var defs = [
      ['all', 'gl_all'], ['proxy', 'gl_proxy'], ['vpn', 'gl_vpn'], ['k8s', 'gl_k8s'], ['gateway', 'gl_gateway'],
    ];
    box.innerHTML = defs.map(function (d) {
      return '<button type="button" class="gl-pill' + (S.flowFilter === d[0] ? ' active' : '') + '" data-flow="' + d[0] + '">' + esc(t(d[1])) + '</button>';
    }).join('');
  }

  function renderGroups() {
    var sel = document.getElementById('glGroup');
    if (!sel || !S.graph) return;
    var groups = [];
    S.graph.nodes.forEach(function (n) { if (n.group_id && groups.indexOf(n.group_id) < 0) groups.push(n.group_id); });
    groups.sort();
    var cur = S.group;
    sel.innerHTML = '<option value="">' + esc(t('gl_all_groups')) + '</option>' +
      groups.map(function (g) { return '<option value="' + esc(g) + '">' + esc(g) + '</option>'; }).join('');
    sel.value = cur;
  }

  function renderSidebar() {
    var box = document.getElementById('glSideList');
    if (!box || !S.graph) return;
    var hosts = S.graph.nodes.filter(function (n) { return n.kind === 'host' && visible(n); });
    box.innerHTML = hosts.map(function (n) {
      return '<button type="button" class="gl-side-item' + (S.selected === n.id ? ' active' : '') + '" data-node="' + esc(n.id) + '">' +
        '<span class="gl-side-dot ' + esc(n.status) + '"></span>' +
        '<span class="gl-side-name">' + esc(n.name) + '</span>' +
        '<span class="gl-side-meta">' + esc(n.city || n.group_id || '') + '</span>' +
      '</button>';
    }).join('');
  }

  function renderCount() {
    var c = document.getElementById('glCount');
    if (!c || !S.graph) return;
    var nodes = S.graph.nodes.filter(visible).length;
    var flows = S.graph.flows.filter(flowVisible).length;
    c.textContent = nodes + ' ' + t('gl_nodes') + ' · ' + flows + ' ' + t('gl_flows');
  }

  function openHud(id, doFly) {
    S.selected = id;
    var n = nodeById(id);
    if (n && doFly) flyTo(n);
    renderHud();
    renderSidebar();
  }

  function closeHud() {
    S.selected = null;
    var hud = document.getElementById('glHud');
    if (hud) hud.hidden = true;
    renderSidebar();
  }

  function renderHud() {
    var hud = document.getElementById('glHud');
    var n = nodeById(S.selected);
    if (!hud || !n) return;
    hud.hidden = false;
    var isHost = n.kind === 'host';
    var lat = n.lat ? n.lat.toFixed(2) : '0.00';
    var lon = n.lon ? n.lon.toFixed(2) : '0.00';
    hud.innerHTML = '' +
      '<div class="gl-hud-head">' +
        '<div class="gl-hud-title"><span class="gl-hud-dot ' + esc(n.status) + '"></span><strong>' + esc(n.name) + '</strong></div>' +
        '<button class="btn btn-icon btn-sm btn-ghost" id="glHudClose" title="' + esc(t('gl_close')) + '">' + icon('x') + '</button>' +
      '</div>' +
      '<div class="gl-hud-body">' +
        '<div class="gl-hud-stats">' +
          '<span><b>' + esc(t('gl_geo')) + '</b> ' + esc(n.city || '—') + (n.country_code ? ', ' + esc(n.country_code) : '') + '</span>' +
          '<span><b>' + esc(t('gl_latency')) + '</b> ' + (n.latency_ms ? esc(n.latency_ms) + ' ms' : '—') + '</span>' +
          '<span><b>' + esc(t('gl_lat')) + '</b> ' + lat + '</span>' +
          '<span><b>' + esc(t('gl_lon')) + '</b> ' + lon + '</span>' +
        '</div>' +
        '<div class="gl-hud-summary">' + esc(n.ip || '') + (n.geo_source ? ' · ' + esc(t('gl_geo_source')) + ': ' + esc(n.geo_source) : '') + '</div>' +
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
          '<label class="gl-geo-wide">' + esc(t('gl_label')) + '<input id="glGeoLabel" type="text" value="' + esc(n.city || '') + '" /></label>' +
          '<button class="btn btn-sm gl-geo-wide" id="glGeoSave">' + esc(t('gl_setgeo')) + '</button>' +
          '<div class="gl-geo-wide gl-side-meta">' + esc(t('gl_geo_hint')) + '</div>' +
        '</div>' : '') +
      '</div>';
    document.getElementById('glHudClose').onclick = closeHud;
    var term = document.getElementById('glHudTerminal');
    if (term && isHost) term.onclick = function () { if (typeof openHostTerminal === 'function') openHostTerminal(n.host_id); };
    var diag = document.getElementById('glHudDiag');
    if (diag && isHost) diag.onclick = function () { if (typeof openNetDiagModal === 'function') openNetDiagModal(n.host_id); };
    var ws = document.getElementById('glHudWorkspace');
    if (ws && isHost) ws.onclick = function () { if (typeof openHostDetail === 'function') openHostDetail(n.host_id); };
    var save = document.getElementById('glGeoSave');
    if (save && isHost) save.onclick = function () { saveGeo(n); };
  }

  async function saveGeo(n) {
    var lat = parseFloat(document.getElementById('glGeoLat').value);
    var lon = parseFloat(document.getElementById('glGeoLon').value);
    var label = document.getElementById('glGeoLabel').value || '';
    if (isNaN(lat) || isNaN(lon)) return;
    try {
      var res = await fetch('/api/network-radar/globe/geo', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id: n.host_id, lat: lat, lon: lon, label: label }),
      });
      if (!res.ok) return;
      n.lat = lat; n.lon = lon; n.city = label; n.geo_source = 'manual';
      renderHud();
    } catch (e) { /* offline: keep current position */ }
  }

  // ---- Data loading -------------------------------------------------------
  async function load() {
    try {
      var res = await fetch('/api/network-radar/globe');
      if (!res.ok) throw new Error('HTTP ' + res.status);
      S.graph = await res.json();
      renderUI();
    } catch (err) {
      var c = document.getElementById('glCount');
      if (c) c.textContent = t('gl_failed');
      console.error('globe load failed', err);
    }
  }

  // ---- SSE: in-place health/pulse updates (ADR 0030) ----------------------
  function setLive(state) {
    var pill = document.getElementById('glLivePill');
    if (!pill) return;
    pill.className = 'gl-live-pill gl-' + state;
    var label = pill.querySelector('span:last-child');
    if (label) label.textContent = state === 'live' ? t('gl_live') : state === 'reconnecting' ? t('gl_reconnecting') : t('gl_offline');
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
    stopLoop();
  }

  function handleEvent(evt) {
    if (!S.graph) return;
    var attrs = evt.attrs || {};
    var status = attrs.status || (evt.module === 'inspector' ? attrs.host_status : null);
    if (status && ['healthy', 'degraded', 'down', 'unknown'].indexOf(status) >= 0) {
      var target = evt.host ? S.graph.nodes.find(function (n) { return n.kind === 'host' && n.name === evt.host; }) : null;
      if (target) {
        target.status = status;
        if (S.selected === target.id) renderHud();
        renderSidebar();
      }
    }
    var msg = (evt.message || '').toLowerCase();
    if (msg.indexOf('drift') >= 0 || msg.indexOf('alert') >= 0 || msg.indexOf('inspection') >= 0) load();
  }

  GL.onClose = disconnectSSE;

  // ---- Events -------------------------------------------------------------
  function bindCanvas() {
    var canvas = document.getElementById('glCanvas');
    if (!canvas) return;
    canvas.addEventListener('pointerdown', function (e) {
      S.dragging = true;
      S.lastPointer = { x: e.clientX, y: e.clientY };
      S.velYaw = S.velPitch = 0;
      canvas.setPointerCapture(e.pointerId);
    });
    canvas.addEventListener('pointermove', function (e) {
      if (!S.dragging || !S.lastPointer) return;
      var dx = e.clientX - S.lastPointer.x, dy = e.clientY - S.lastPointer.y;
      S.velYaw = dx * 0.005; S.velPitch = -dy * 0.005;
      S.yaw += S.velYaw; S.pitch += S.velPitch;
      S.pitch = Math.max(-1.2, Math.min(1.2, S.pitch));
      S.lastPointer = { x: e.clientX, y: e.clientY };
    });
    canvas.addEventListener('pointerup', function (e) {
      if (!S.dragging) return;
      // A click without meaningful movement selects a node.
      if (Math.abs(S.velYaw) < 0.002 && Math.abs(S.velPitch) < 0.002) {
        var n = pickNode(e.clientX, e.clientY);
        if (n) openHud(n.id, false);
        else closeHud();
      }
      S.dragging = false;
      S.lastPointer = null;
    });
    canvas.addEventListener('wheel', function (e) {
      e.preventDefault();
      S.zoom = Math.max(0.6, Math.min(3, S.zoom * (e.deltaY > 0 ? 0.9 : 1.1)));
    }, { passive: false });
  }

  document.addEventListener('click', function (e) {
    if (!document.getElementById(MODAL_ID)) return;
    var el = e.target;
    if (!el || !el.closest) return;
    var pill = el.closest('.gl-pill');
    if (pill) { S.flowFilter = pill.getAttribute('data-flow'); renderPills(); renderCount(); return; }
    var side = el.closest('.gl-side-item');
    if (side) { openHud(side.getAttribute('data-node'), true); return; }
    if (el.closest('#glRefreshBtn')) { load(); return; }
    if (el.closest('#glZoomIn')) { S.zoom = Math.min(3, S.zoom * 1.2); return; }
    if (el.closest('#glZoomOut')) { S.zoom = Math.max(0.6, S.zoom / 1.2); return; }
    if (el.closest('#glZoomReset')) { S.zoom = 1; S.yaw = -0.4; S.pitch = 0.35; S.fly = null; return; }
  });

  document.addEventListener('input', function (e) {
    if (!e.target || !document.getElementById(MODAL_ID)) return;
    if (e.target.id === 'glSearch') { S.query = e.target.value.trim(); renderUI(); }
  });

  document.addEventListener('change', function (e) {
    if (!e.target || !document.getElementById(MODAL_ID)) return;
    if (e.target.id === 'glGroup') { S.group = e.target.value; renderUI(); }
    if (e.target.id === 'glAutoRotate') { S.autoRotate = e.target.checked; }
  });

  // Pause the animation loop when the page is hidden.
  document.addEventListener('visibilitychange', function () {
    if (document.hidden) stopLoop();
    else if (S.mounted) startLoop();
  });

  function startLoop() {
    if (S.raf) return;
    S.lastFrame = 0;
    S.raf = requestAnimationFrame(drawFrame);
  }

  function stopLoop() {
    if (S.raf) { cancelAnimationFrame(S.raf); S.raf = 0; }
  }

  GL.open = function () {
    ensureModal();
    restoreControls();
    openModal(MODAL_ID);
    S.mounted = true;
    resizeCanvas();
    bindCanvas();
    connectSSE();
    load();
    startLoop();
  };

  GL.onResize = function () { if (S.mounted) resizeCanvas(); };
  window.addEventListener('resize', function () { if (S.mounted) GL.onResize(); });
})();
