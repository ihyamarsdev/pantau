/* Globe Radar — frame loop, camera, picking and pointer input (ADR 0090).
 * Continuation of the GlobeRadar controller; shares its state via GL.state. */
(function () {
  'use strict';

  var GL = window.GlobeRadar;
  var G = window.WebGlobe;
  var S = GL.state;
  var RAD = Math.PI / 180;
  var MIN_ZOOM = 0.75, MAX_ZOOM = 200, MAX_LAT = 85 * RAD;
  var AUTO_MAX_ZOOM = 2, DETAIL_ZOOM = 2.5, STACK_PX = 8, SPREAD_PX = 18;
  var AUTO_SPEED = 0.00005;   // rad/ms, about 3 degrees a second
  var HOME = { lon: 100 * RAD, lat: 15 * RAD, zoom: 1 };
  var esc = function (s) { return escapeHtml(String(s === null || s === undefined ? '' : s)); };

  function clamp(v, lo, hi) { return Math.max(lo, Math.min(hi, v)); }
  function wrap(a) {
    while (a > Math.PI) a -= 2 * Math.PI;
    while (a < -Math.PI) a += 2 * Math.PI;
    return a;
  }

  function wrapDeg(a) { return ((a + 540) % 360) - 180; }

  // Refresh the close-zoom detail texture once the camera has settled. The first call to G.admin1()
  // starts the lazy province-line import; later calls return the cached array (or null while loading).
  function syncDetail(r, ts) {
    if (S.zoom < DETAIL_ZOOM) { if (r.detail) r.setDetail(null); return; }
    var ppr = r.pxPerRad(S.zoom);
    var moving = S.dragging || S.fly || (Math.abs(S.velLon) + Math.abs(S.velLat)) * ppr > 0.05;
    if (moving) return;
    var d = r.detail;
    if (d && ts - d.at < 200) return;
    var lines = G.admin1();
    var lat = S.lat / RAD, lon = S.lon / RAD;
    var cw = S.canvas.clientWidth, ch = S.canvas.clientHeight;
    if (d && S.zoom / d.zoom > 0.8 && S.zoom / d.zoom < 1.4 &&
        Math.abs(lat - d.lat) < 0.25 * d.halfLat &&
        Math.abs(wrapDeg(lon - d.lon)) < 0.25 * d.halfLon &&
        !!lines === !!d.lines && cw === d.cw && ch === d.ch) return;
    var degPerPx = 1 / (ppr * RAD);
    var halfLat = Math.min(90, 0.8 * Math.max(cw, ch) * degPerPx);
    var halfLon = Math.min(180, halfLat / Math.max(0.2, Math.cos(S.lat)));
    var south = Math.max(-90, lat - halfLat), north = Math.min(90, lat + halfLat);
    r.setDetail({
      lat: lat, lon: lon, zoom: S.zoom, halfLat: halfLat, halfLon: halfLon, cw: cw, ch: ch, at: ts,
      west: lon - halfLon, east: lon + halfLon, south: south, north: north,
      cssPx: (north - south) / degPerPx,
      lineZoom: Math.log2(ppr * RAD / 0.711) + 1,
      lines: lines
    });
  }

  // Wheel zoom about the point under the cursor: unproject before and after, then shift the camera by the
  // geo-coordinate delta so that spot stays put. Off-globe cursors simply zoom about the centre.
  function zoomAt(clientX, clientY, zoom) {
    zoom = clamp(zoom, MIN_ZOOM, MAX_ZOOM);
    var r = S.renderer;
    if (!r || !r.ok) { S.zoom = zoom; return; }
    var rect = S.canvas.getBoundingClientRect();
    var x = clientX - rect.left, y = clientY - rect.top;
    r.setCamera({ lon: S.lon, lat: S.lat, zoom: S.zoom });
    var before = r.unproject(x, y);
    S.zoom = zoom;
    r.setCamera({ lon: S.lon, lat: S.lat, zoom: S.zoom });
    var after = before && r.unproject(x, y);
    if (after) {
      S.lon = wrap(S.lon + wrap((before.lon - after.lon) * RAD));
      S.lat = clamp(S.lat + (before.lat - after.lat) * RAD, -MAX_LAT, MAX_LAT);
    }
  }

  // ---- Frame loop ----------------------------------------------------------
  var lastHover = 0;

  function drawFrame(ts) {
    if (!S.mounted) return;
    var dt = S.lastFrame ? Math.min(50, ts - S.lastFrame) : 16;
    S.lastFrame = ts;

    if (S.fly) {
      var k = 1 - Math.exp(-dt / 160), dLon = wrap(S.fly.lon - S.lon);
      S.lon += dLon * k;
      S.lat += (S.fly.lat - S.lat) * k;
      S.zoom *= Math.pow(S.fly.zoom / S.zoom, k);
      var eps = 0.002 / S.zoom;
      if (Math.abs(dLon) < eps && Math.abs(S.fly.lat - S.lat) < eps && Math.abs(Math.log(S.fly.zoom / S.zoom)) < 0.005) {
        S.lon = S.fly.lon; S.lat = S.fly.lat; S.zoom = S.fly.zoom;
        S.fly = null;
      }
    } else if (!S.dragging) {
      S.lon += S.velLon * dt;
      S.lat = clamp(S.lat + S.velLat * dt, -MAX_LAT, MAX_LAT);
      var decay = Math.exp(-dt / 250);
      S.velLon *= decay;
      S.velLat *= decay;
      // The globe holds still while a host is selected, or its pin would drift out of view under the HUD.
      if (S.autoRotate && !S.selected && S.zoom < AUTO_MAX_ZOOM) S.lon -= AUTO_SPEED * dt;
    }
    S.lon = wrap(S.lon);

    var r = S.renderer;
    if (r && r.ok) {
      if (S.sceneDirty) GL.syncScene();
      if (S.tintsDirty) GL.syncTints();
      if (S.hoverPos && !S.dragging && ts - lastHover > 100) { lastHover = ts; updateHover(); } // the globe turns under a still cursor
      r.resize();
      r.setCamera({ lon: S.lon, lat: S.lat, zoom: S.zoom });
      syncDetail(r, ts);
      r.draw(ts, clamp((S.burst - ts) / 1200, 0, 1));
    }
    S.raf = requestAnimationFrame(drawFrame);
  }

  GL.startLoop = function () {
    if (S.raf) return;
    S.lastFrame = 0;
    S.raf = requestAnimationFrame(drawFrame);
  };
  GL.stopLoop = function () {
    if (S.raf) { cancelAnimationFrame(S.raf); S.raf = 0; }
  };

  // ---- Camera --------------------------------------------------------------
  GL.flyTo = function (node) {
    S.fly = { lon: node.lon * RAD, lat: clamp(node.lat * RAD, -MAX_LAT, MAX_LAT), zoom: Math.max(S.zoom, 1.6) };
  };
  function zoomBy(f) {
    S.fly = { lon: S.lon, lat: S.lat, zoom: clamp(S.zoom * f, MIN_ZOOM, MAX_ZOOM) };
  }
  function goHome() {
    S.velLon = S.velLat = 0;
    S.fly = { lon: HOME.lon, lat: HOME.lat, zoom: HOME.zoom };
  }

  // ---- Picking and hover ---------------------------------------------------
  // Every visible pin within STACK_PX of the pin nearest the cursor, nearest first. At close zoom the
  // same-city pins sit a few px apart, so a single pick point can cover a whole stack of hosts.
  function pickNodes(clientX, clientY) {
    if (!S.graph || !S.renderer || !S.renderer.ok || !S.canvas) return [];
    var rect = S.canvas.getBoundingClientRect();
    var x = clientX - rect.left, y = clientY - rect.top;
    var pins = [];
    var best = null, bestD = 14;
    S.graph.nodes.forEach(function (n) {
      if (!GL.visible(n)) return;
      var p = S.renderer.project(n);
      if (!p.visible) return;
      var d = Math.hypot(p.x - x, p.y - y);
      pins.push({ n: n, x: p.x, y: p.y, d: d });
      if (d < bestD) { bestD = d; best = pins[pins.length - 1]; }
    });
    if (!best) return [];
    return pins.filter(function (p) {
      return p === best || Math.hypot(p.x - best.x, p.y - best.y) < STACK_PX;
    }).sort(function (a, b) {
      return Math.hypot(a.x - best.x, a.y - best.y) - Math.hypot(b.x - best.x, b.y - best.y);
    });
  }

  function placeTip(el, clientX, clientY) {
    var rect = document.getElementById('glStage').getBoundingClientRect();
    var x = clientX - rect.left + 14, y = clientY - rect.top + 14;
    if (x + el.offsetWidth + 8 > rect.width) x = clientX - rect.left - el.offsetWidth - 14;
    if (y + el.offsetHeight + 8 > rect.height) y = clientY - rect.top - el.offsetHeight - 14;
    el.style.left = Math.max(4, x) + 'px';
    el.style.top = Math.max(4, y) + 'px';
  }

  function showNodeTip(node, clientX, clientY) {
    var el = document.getElementById('glTooltip');
    if (!el || !document.getElementById('glStage')) return;
    el.className = 'gl-tooltip';
    el.innerHTML = '<strong>' + esc(node.name) + '</strong>' +
      '<div>' + esc(node.public_ip || node.ip || '—') + '</div>' +
      '<div>' + esc([node.city, node.region, node.country_code].filter(Boolean).join(', ') || '—') + '</div>' +
      (node.asn_org ? '<div class="gl-tip-dim">' + esc(node.asn_org) + '</div>' : '') +
      '<div class="gl-tip-dim">' + esc(t('gl_peer_count')) + ': ' + GL.peerCount(node.id) + '</div>';
    el.hidden = false;
    placeTip(el, clientX, clientY);
  }

  function showStackTip(hit, clientX, clientY) {
    var el = document.getElementById('glTooltip');
    if (!el || !document.getElementById('glStage')) return;
    var html = '<strong>' + esc(t('gl_stack_title').replace('{n}', hit.length)) + '</strong>';
    hit.slice(0, 8).forEach(function (h) {
      html += '<div class="gl-tip-row"><span class="gl-side-dot ' + esc(h.n.status) + '"></span>' +
        '<span class="gl-side-name">' + esc(h.n.name) + '</span></div>';
    });
    if (hit.length > 8) html += '<div class="gl-tip-dim">' + esc(t('gl_stack_more').replace('{n}', hit.length - 8)) + '</div>';
    if (S.zoom < MAX_ZOOM * 0.999) html += '<div class="gl-tip-dim">' + esc(t('gl_stack_hint')) + '</div>';
    el.className = 'gl-tooltip';
    el.innerHTML = html;
    el.hidden = false;
    placeTip(el, clientX, clientY);
  }

  function showCountryTip(idx, clientX, clientY) {
    var el = document.getElementById('glTooltip');
    if (!el || !document.getElementById('glStage')) return;
    var hosts = GL.countryHosts(idx);
    el.className = 'gl-tooltip gl-tip-country';
    el.innerHTML = '<strong>' + esc(G.countryName(G.countries()[idx], document.documentElement.lang || 'en')) + '</strong>' +
      (hosts ? '<div class="gl-tip-dim">' + esc(t(hosts === 1 ? 'gl_host_one' : 'gl_host_many').replace('{n}', hosts)) + '</div>' : '');
    el.hidden = false;
    placeTip(el, clientX, clientY);
  }

  // Fly toward a stack until its pins separate; at max zoom they cannot, so the caller opens the HUD instead.
  function zoomIntoStack(hit) {
    if (S.zoom >= MAX_ZOOM * 0.999) return false;
    var dmin = Infinity;
    for (var i = 0; i < hit.length; i++) {
      for (var j = i + 1; j < hit.length; j++) {
        var d = Math.hypot(hit[i].x - hit[j].x, hit[i].y - hit[j].y);
        if (d < dmin) dmin = d;
      }
    }
    var zoom = clamp(S.zoom * Math.max(2, SPREAD_PX / Math.max(dmin, 0.01)), MIN_ZOOM, MAX_ZOOM);
    var sx = 0, sy = 0, sz = 0;
    hit.forEach(function (h) {
      var v = G.latLonToVec(h.n.lat, h.n.lon, 1);
      sx += v[0]; sy += v[1]; sz += v[2];
    });
    var lon = Math.atan2(sx, sz);
    var lat = clamp(Math.atan2(sy, Math.hypot(sx, sz)), -MAX_LAT, MAX_LAT);
    S.velLon = S.velLat = 0;
    S.fly = { lon: lon, lat: lat, zoom: zoom };
    return true;
  }

  function hideTooltip() {
    var el = document.getElementById('glTooltip');
    if (el) el.hidden = true;
  }

  function setHoverCountry(idx) {
    if (S.hoverCountry === idx) return;
    S.hoverCountry = idx;
    S.tintsDirty = true;
  }

  // A pin wins over the country under it; over open sea there is nothing to show.
  function updateHover() {
    var h = S.hoverPos;
    if (!h || !S.renderer || !S.renderer.ok) return;
    var hit = pickNodes(h.clientX, h.clientY);
    S.canvas.classList.toggle('gl-hot', hit.length > 0);
    if (hit.length === 1) { setHoverCountry(-1); showNodeTip(hit[0].n, h.clientX, h.clientY); return; }
    if (hit.length > 1) { setHoverCountry(-1); showStackTip(hit, h.clientX, h.clientY); return; }
    var geo = S.renderer.unproject(h.x, h.y);
    var idx = geo ? G.countryAt(geo.lat, geo.lon) : -1;
    setHoverCountry(idx);
    if (idx >= 0) showCountryTip(idx, h.clientX, h.clientY); else hideTooltip();
  }

  function clearHover() {
    S.hoverPos = null;
    setHoverCountry(-1);
    if (S.canvas) S.canvas.classList.remove('gl-hot');
    hideTooltip();
  }

  // ---- Canvas events -------------------------------------------------------
  function bindCanvas() {
    var canvas = document.getElementById('glCanvas');
    if (!canvas) return;
    S.canvas = canvas;
    if (canvas.dataset.glBound) return; // the canvas outlives a close; listeners must not stack up on every open
    canvas.dataset.glBound = '1';

    canvas.addEventListener('pointerdown', function (e) {
      if (e.pointerType === 'mouse' && e.button !== 0) return;
      S.dragging = true;
      S.pointerMoved = false;
      S.lastPointer = { x: e.clientX, y: e.clientY };
      S.lastMove = performance.now();
      S.velLon = S.velLat = 0;
      S.fly = null;
      canvas.setPointerCapture(e.pointerId);
    });

    canvas.addEventListener('pointermove', function (e) {
      if (S.dragging && S.lastPointer) {
        var dx = e.clientX - S.lastPointer.x, dy = e.clientY - S.lastPointer.y;
        if (Math.abs(dx) + Math.abs(dy) > 2) S.pointerMoved = true;
        // 1:1 under the cursor at every zoom: pxPerRad is the stage's CSS pixels per radian at the view centre.
        var radius = S.renderer.pxPerRad(S.zoom);
        var dLon = -dx / (radius * Math.max(0.35, Math.cos(S.lat))), dLat = dy / radius;
        var now = performance.now(), dtm = Math.max(1, now - S.lastMove);
        S.lon += dLon;
        S.lat = clamp(S.lat + dLat, -MAX_LAT, MAX_LAT);
        S.velLon = clamp(0.7 * S.velLon + 0.3 * dLon / dtm, -0.004, 0.004);
        S.velLat = clamp(0.7 * S.velLat + 0.3 * dLat / dtm, -0.004, 0.004);
        S.lastMove = now;
        S.lastPointer = { x: e.clientX, y: e.clientY };
        clearHover();
        return;
      }
      var rect = canvas.getBoundingClientRect();
      S.hoverPos = { clientX: e.clientX, clientY: e.clientY, x: e.clientX - rect.left, y: e.clientY - rect.top };
      updateHover();
    });

    function release(e, cancelled) {
      if (!S.dragging) return;
      S.dragging = false;
      S.lastPointer = null;
      if (performance.now() - S.lastMove > 90) S.velLon = S.velLat = 0; // held still before letting go: no fling
      if (!S.pointerMoved && !cancelled) {
        var hit = pickNodes(e.clientX, e.clientY);
        if (hit.length === 0) GL.closeHud();
        else if (hit.length === 1 || !zoomIntoStack(hit)) GL.openHud(hit[0].n.id, false);
      }
    }
    canvas.addEventListener('pointerup', function (e) { release(e, false); });
    canvas.addEventListener('pointercancel', function (e) { release(e, true); });
    canvas.addEventListener('pointerleave', function () { if (!S.dragging) clearHover(); });

    canvas.addEventListener('wheel', function (e) {
      e.preventDefault();
      var d = e.deltaMode === 1 ? e.deltaY * 16 : e.deltaY;
      S.fly = null;
      zoomAt(e.clientX, e.clientY, S.zoom * Math.exp(-d * 0.002));
    }, { passive: false });
  }
  GL.bindCanvas = bindCanvas;

  // ---- Document-level UI events -------------------------------------------
  document.addEventListener('click', function (e) {
    if (!document.getElementById('globeModal')) return;
    var el = e.target;
    if (!el || !el.closest) return;
    var pill = el.closest('.gl-pill');
    if (pill) { S.flowFilter = pill.getAttribute('data-flow'); GL.renderUI(); return; }
    var side = el.closest('.gl-side-item');
    if (side) { GL.openHud(side.getAttribute('data-node'), true); return; }
    if (el.closest('#glRefreshBtn')) { GL.load(); return; }
    if (el.closest('#glZoomIn')) { zoomBy(2); return; }
    if (el.closest('#glZoomOut')) { zoomBy(0.5); return; }
    if (el.closest('#glZoomReset')) { goHome(); return; }
  });

  document.addEventListener('input', function (e) {
    if (!e.target || !document.getElementById('globeModal')) return;
    if (e.target.id === 'glSearch') { S.query = e.target.value.trim(); GL.renderUI(); }
  });

  document.addEventListener('change', function (e) {
    if (!e.target || !document.getElementById('globeModal')) return;
    if (e.target.id === 'glGroup') { S.group = e.target.value; GL.renderUI(); }
    if (e.target.id === 'glAutoRotate') { S.autoRotate = e.target.checked; }
  });

  document.addEventListener('visibilitychange', function () {
    if (document.hidden) GL.stopLoop();
    else if (S.mounted) GL.startLoop();
  });
})();
