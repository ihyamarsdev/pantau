/* Globe Radar — frame loop, camera, picking and pointer input (ADR 0090).
 * Continuation of the GlobeRadar controller; shares its state via GL.state. */
(function () {
  'use strict';

  var GL = window.GlobeRadar;
  var G = window.WebGlobe;
  var S = GL.state;
  var RAD = Math.PI / 180;
  var MIN_ZOOM = 0.75, MAX_ZOOM = 600, MAX_LAT = 85 * RAD;
  var AUTO_MAX_ZOOM = 2, DETAIL_ZOOM = 1.3, FINE_ZOOM = 3, PROV_ZOOM = 2.5, KAB_ZOOM = 12, STACK_PX = 8, SPREAD_PX = 18;
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
  GL.clampZoom = function (z) { return clamp(z, MIN_ZOOM, MAX_ZOOM); };
  GL.maxZoom = function () { return MAX_ZOOM; };

  // Refresh the detail texture so the visible globe area always has >= 1 texel per device pixel at the view
  // centre. The raster is a viewport-tight geographic rectangle: at higher zoom the rectangle shrinks with the
  // view, so texel density stays constant and the whole visible area is covered. Modes:
  //  - adminMode: an Indonesian province/kabupaten is selected -- raster the focused bbox with its real polygons;
  //  - countryMode: a country is selected -- raster its full bbox (Indonesia uses official admin-2 data);
  //  - general: raster the visible viewport rectangle.
  // Decoded arrays are cached; the texture object and canvas are reused. Repaints happen only when the camera
  // has settled (never while dragging/flying/inertial), throttled to 200 ms, and skipped when the view has not
  // changed enough to matter -- zero work when static.
  function syncDetail(r, ts) {
    var adminMode = !!(S.selectedProvince && S.adminFit && G.admin2Ready() && GL.adminView);
    var countryMode = !adminMode && S.selectedCountry >= 0 && !!S.countryFit;
    if (!adminMode && !countryMode && S.zoom < DETAIL_ZOOM) { if (r.detail) r.setDetail(null); return; }

    var ppr = r.pxPerRad(S.zoom);
    var moving = S.dragging || S.fly || (Math.abs(S.velLon) + Math.abs(S.velLat)) * ppr > 0.05;
    var lat = S.lat / RAD, lon = S.lon / RAD;
    var cw = S.canvas.clientWidth, ch = S.canvas.clientHeight;
    var d = r.detail;

    // The raster ALWAYS covers the visible viewport. The selection bbox drives fly-to framing and the
    // highlight only; it never clips the raster (clipping it uncovered the stage edges at high zoom).
    // Longitude uses cw, latitude uses ch, both in CSS px, with a 1.25x margin for pan tolerance.
    var degPerPx = 180 / (Math.PI * ppr);
    var halfLat = Math.min(90, 0.625 * ch * degPerPx);
    var halfLon = Math.min(180, 0.625 * cw * degPerPx / Math.max(0.2, Math.cos(S.lat)));
    var south = Math.max(-90, lat - halfLat), north = Math.min(90, lat + halfLat);
    var west = lon - halfLon, east = lon + halfLon;

    // Change detection against the LAST PAINTED view: even in admin/country mode we now repaint when the
    // camera has zoomed enough that the frozen raster would be magnified beyond 1 texel/device px.
    var adminKey = adminMode ? (S.selectedProvince + '/' + S.selectedKab) : '';
    var selectedIso = countryMode ? ((G.countries()[S.selectedCountry] || {}).iso || '') : '';
    // Determine whether the view is over Indonesia from the actual camera centre (not only the picker),
    // so automatic LOD works without a selection. A bbox prefilter is enough at these zooms.
    var viewIso = '';
    var geo = S.renderer.unproject(S.canvas.clientWidth / 2, S.canvas.clientHeight / 2);
    if (geo) viewIso = G.isoAt(geo.lat, geo.lon);
    var idMode = selectedIso === 'ID' || adminMode || viewIso === 'ID';
    var lodKey = idMode ? (S.zoom >= KAB_ZOOM ? 2 : S.zoom >= PROV_ZOOM ? 1 : 0) : 0;
    if (d && !moving) {
      var sameMode = d.country === S.selectedCountry && d.adminKey === adminKey && d.lod === lodKey &&
        d.idMode === idMode && d.cw === cw && d.ch === ch && d.mode === (adminMode ? 2 : countryMode ? 1 : 0) &&
        d.adminReady === !!G.admin2Ready() && d.fineReady === !!G.admin0fineReady() &&
        d.hiresCount === G.hiresCount() &&
        d.hiresProv === (S.selectedProvince || '');
      // Coverage in CSS px: degPerCssPx uses pxPerRad (CSS px), and the longitude extent uses cw (stage width),
      // not ch. The painted rect must contain the current view at 1 texel/device px.
      var degPerCssPx = 180 / (Math.PI * ppr);
      var needHalfLat = 0.5 * ch * degPerCssPx;
      var needHalfLon = 0.5 * cw * degPerCssPx / Math.max(0.2, Math.cos(S.lat));
      var covers = lat - needHalfLat >= d.south && lat + needHalfLat <= d.north &&
        Math.abs(wrapDeg(lon - d.lon)) <= d.halfLon - needHalfLon;
      // Density: texels per degree of the painted raster vs the texels per degree needed at this zoom.
      var densityOK = !d.texPerDeg || d.texPerDeg >= (1 / degPerCssPx) * r.dpr * 0.98;
      // Repaint if the view centre has drifted more than a quarter of the painted rect: a big jump (pan to
      // another region) needs a fresh raster; small auto-rotate drift stays covered and needs no repaint.
      var centreOK = Math.abs(lat - d.lat) < 0.25 * d.halfLat &&
        Math.abs(wrapDeg(lon - d.lon)) < 0.25 * d.halfLon;
      if (sameMode && covers && densityOK && centreOK) return;
    }

    // While moving, paint a cheaper pass at most every 300 ms so magnification stays brief; the full pass
    // runs on settle. The 200 ms debounce after the last paint also bounds work during continuous wheel zoom.
    if (moving) { if (d && ts - d.at < 300) return; }
    else if (d && ts - d.at < 200) return;

    // Pick geometry for this view. Fine land/coastline is lazy; request it as soon as the coarse 1:50m
    // map would be magnified (FINE_ZOOM), not only at the old 2.5x threshold.
    var fine = G.admin0fineReady() ? G.admin0fine() : null;
    if (S.zoom >= FINE_ZOOM && !G.admin0fineReady()) G.admin0fineLoad();
    var lines, land = null, borders = null, admin = null;
    if (idMode) {
      // Load every hi-res chunk whose province bbox intersects the painted raster rect. Until a chunk is
      // ready, fill/stroke that province from the complete fallback polygons so land is never drawn as sea.
      var landFeatures = [];
      var pendingHires = false;
      if (G.admin2Ready()) {
        var a2 = G.admin2();
        G.provinceBoxes().forEach(function (pb) {
          var b = pb.bbox;
          if (b[2] < west || b[0] > east || b[3] < south || b[1] > north) return;
          var hi = G.hiresFor(pb.code);
          if (hi) landFeatures = landFeatures.concat(hi);
          else {
            landFeatures = landFeatures.concat(G.admin2Fallback(pb.code));
            if (!G.hiresReady(pb.code)) { G.hiresLoad(pb.code); pendingHires = true; }
          }
        });
      } else {
        G.admin2Load();
      }
      // Land/coast from the union of intersecting provinces (hires where loaded, fallback elsewhere).
      land = landFeatures.length ? landFeatures : (G.admin2() ? G.admin2().k : null);
      borders = [];
      lines = null;
      // Highlight geometry: prefer the loaded hi-res features of the selected province. The province
      // selection outline is the union of its kabupaten rings (hi-res), not the coarse cahyadsn polygon.
      var hp = S.selectedProvince ? G.hiresFor(S.selectedProvince) : null;
      var selectedProv = (a2 && a2.p.find(function (p) { return p.code === S.selectedProvince; })) || null;
      var selectedKab = S.selectedKab ? (hp || (a2 ? a2.k : [])).find(function (k) { return k.code === S.selectedKab; }) : null;
      var provOutline = null;
      if (hp && hp.length) {
        provOutline = { rings: [], bbox: [180, 90, -180, -90] };
        hp.forEach(function (k) {
          k.rings.forEach(function (r) { provOutline.rings.push(r); });
          var b = k.bbox;
          provOutline.bbox[0] = Math.min(provOutline.bbox[0], b[0]); provOutline.bbox[1] = Math.min(provOutline.bbox[1], b[1]);
          provOutline.bbox[2] = Math.max(provOutline.bbox[2], b[2]); provOutline.bbox[3] = Math.max(provOutline.bbox[3], b[3]);
        });
      }
      admin = {
        provinces: lodKey >= 1 ? (a2 ? a2.p : []) : [],
        kabs: lodKey >= 2 ? (hp || (S.selectedProvince ? G.admin2Kab(S.selectedProvince) : landFeatures)) : [],
        province: selectedProv, provinceOutline: provOutline, kab: selectedKab, hiresActive: !!hp,
        hoverKab: S.hoverKab || null, hoverProv: S.hoverProvince || null
      };
      if (pendingHires && d) d.pendingHires = true;
    } else {
      lines = (selectedIso && countryMode) ? G.admin1Country(selectedIso) : G.admin1OutsideIndonesia();
    }

    var lineZoom = (adminMode || countryMode) ? 63 : Math.log2(ppr * RAD / 0.711) + 1;
    r.setDetail({
      country: S.selectedCountry, lineCount: lines ? lines.length : 0, adminKey: adminKey, admin: admin,
      lod: lodKey, idMode: idMode, mode: adminMode ? 2 : countryMode ? 1 : 0,
      lat: lat, lon: lon, zoom: S.zoom, viewKey: lat.toFixed(4) + '/' + lon.toFixed(4) + '/' + S.zoom, adminReady: !!G.admin2Ready(), fineReady: !!G.admin0fineReady(), hiresCount: G.hiresCount(), hiresProv: S.selectedProvince || '', halfLat: (north - south) / 2, halfLon: (east - west) / 2,
      cw: cw, ch: ch, at: ts, west: west, east: east, south: south, north: north,
      lineZoom: lineZoom, lines: lines, land: land, borders: borders, fine: fine,
      moving: moving
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

  // Auto-load the official Indonesian boundaries when the view is over Indonesia at province zoom, without
  // requiring the picker. Checked at most every 500 ms (one point-in-polygon on the view centre), so it adds
  // no per-frame allocation and the import starts exactly once.
  var lastAdminCheck = 0;
  function autoLoadAdmin2(ts) {
    if (G.admin2Ready() || S.zoom < PROV_ZOOM || ts - lastAdminCheck < 500) return;
    lastAdminCheck = ts;
    var geo = S.renderer.unproject(S.canvas.clientWidth / 2, S.canvas.clientHeight / 2);
    if (!geo) return;
    var cs = G.countries();
    for (var i = 0; i < cs.length; i++) {
      if (cs[i].iso === 'ID') {
        var b = cs[i].bbox;
        if (geo.lon >= b[0] - 2 && geo.lon <= b[2] + 2 && geo.lat >= b[1] - 2 && geo.lat <= b[3] + 2) G.admin2Load();
        return;
      }
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
      var flyZoom = clamp(S.fly.zoom, MIN_ZOOM, MAX_ZOOM);
      S.zoom *= Math.pow(flyZoom / S.zoom, k);
      var eps = 0.002 / S.zoom;
      if (Math.abs(dLon) < eps && Math.abs(S.fly.lat - S.lat) < eps && Math.abs(Math.log(flyZoom / S.zoom)) < 0.005) {
        S.lon = S.fly.lon; S.lat = S.fly.lat; S.zoom = flyZoom;
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
      autoLoadAdmin2(ts);
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
  // Fly back to the whole-world home view. Zero the inertia first so a residual fling cannot fight the fly;
  // auto-rotate resumes on its own once the fly settles and the zoom is back under AUTO_MAX_ZOOM. This is the
  // single low-level primitive used by the reset button and by GL.selectCountry(-1); it never calls back into
  // selection, so there is no recursive reset path.
  GL.flyHome = function () {
    S.velLon = S.velLat = 0;
    S.fly = { lon: HOME.lon, lat: HOME.lat, zoom: HOME.zoom };
  };
  function goHome() {
    // Clearing the country also flies home (GL.selectCountry(-1) calls flyHome); when nothing is selected,
    // fly directly. Either way the picker label and the actual camera end up agreeing on the whole world.
    if (S.selectedCountry >= 0) GL.selectCountry(-1);
    else GL.flyHome();
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
    var isUnset = node.kind === 'host' && (node.geo_source === 'gateway' || (!node.lat && !node.lon));
    var loc = isUnset ? ('⚠️ ' + esc(t('gl_geo_unknown'))) : esc([node.city, node.region, node.country_code].filter(Boolean).join(', ') || '—');
    el.innerHTML = '<strong>' + esc(node.name) + '</strong>' +
      '<div>' + esc(node.public_ip || node.ip || '—') + '</div>' +
      '<div>' + loc + '</div>' +
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

  function setHoverArea(kab, prov) {
    var kCode = kab ? kab.code : '';
    var pCode = prov ? prov.code : '';
    var oldK = S.hoverKab ? S.hoverKab.code : '';
    var oldP = S.hoverProvince ? S.hoverProvince.code : '';
    if (kCode === oldK && pCode === oldP) return;
    S.hoverKab = kab;
    S.hoverProvince = prov;
    S.sceneDirty = true;
    if (S.renderer && S.renderer.detail) {
      S.renderer.detail.admin = S.renderer.detail.admin || {};
      S.renderer.detail.admin.hoverKab = kab;
      S.renderer.detail.admin.hoverProv = prov;
      S.renderer._paintDetail();
    }
  }

  // A pin wins over the country under it; over open sea there is nothing to show.
  // Hover over Indonesia at province/kabupaten zoom names the area. Uses the shared bbox-prefiltered
  // point-in-polygon result (no per-move allocation). A pin always wins; this is only called when no pin hit.
  function showAreaTip(geo, clientX, clientY) {
    var hit = G.areaAt(geo.lat, geo.lon);
    var kab = hit.kab, prov = hit.province;
    if (!kab && !prov) return false;
    var el = document.getElementById('glTooltip');
    if (!el || !document.getElementById('glStage')) return false;
    el.className = 'gl-tooltip gl-tip-country';
    var parts = [];
    if (kab) parts.push(esc(kab.name));
    if (prov) parts.push(esc(prov.name));
    el.innerHTML = '<strong>' + parts.join('</strong><div class="gl-tip-dim">') + '</div>';
    el.hidden = false;
    placeTip(el, clientX, clientY);
    return true;
  }

  function updateHover() {
    var h = S.hoverPos;
    if (!h || !S.renderer || !S.renderer.ok) return;
    var hit = pickNodes(h.clientX, h.clientY);
    S.canvas.classList.toggle('gl-hot', hit.length > 0);
    if (hit.length === 1) { setHoverArea(null, null); setHoverCountry(-1); showNodeTip(hit[0].n, h.clientX, h.clientY); return; }
    if (hit.length > 1) { setHoverArea(null, null); setHoverCountry(-1); showStackTip(hit, h.clientX, h.clientY); return; }
    var geo = S.renderer.unproject(h.x, h.y);
    var idx = geo ? G.countryAt(geo.lat, geo.lon) : -1;
    setHoverCountry(idx);
    // At province/kabupaten zoom over Indonesia, name & highlight the area; otherwise the country hover is unchanged.
    if (S.zoom >= PROV_ZOOM && G.admin2Ready() && geo) {
      var aHit = G.areaAt(geo.lat, geo.lon);
      setHoverArea(aHit.kab, aHit.province);
      if (showAreaTip(geo, h.clientX, h.clientY)) return;
    } else {
      setHoverArea(null, null);
    }
    if (idx >= 0) showCountryTip(idx, h.clientX, h.clientY); else hideTooltip();
  }

  function clearHover() {
    S.hoverPos = null;
    setHoverArea(null, null);
    setHoverCountry(-1);
    if (S.canvas) S.canvas.classList.remove('gl-hot');
    hideTooltip();
  }

  // ---- Canvas events -------------------------------------------------------
  function bindCanvas() {
    var canvas = document.getElementById('glCanvas');
    if (!canvas) return;
    S.canvas = canvas;
    if (canvas.dataset.glBound) return; // the canvas outlives an unmount; listeners must not stack up on every mount
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
        if (hit.length === 0) {
          // No pin under the click: select the country under the cursor (or clear when over open sea), so clicking
          // a region is a second, discoverable way to reveal its provinces besides the toolbar selector.
          var rect = canvas.getBoundingClientRect();
          var geo = S.renderer.unproject(e.clientX - rect.left, e.clientY - rect.top);
          var idx = geo ? G.countryAt(geo.lat, geo.lon) : -1;
          if (idx >= 0) GL.selectCountry(idx);
          else GL.closeHud();
        }
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
    if (!S.mounted) return;
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

  document.addEventListener('change', function (e) {
    if (!e.target || !S.mounted) return;
    if (e.target.id === 'glAutoRotate') { S.autoRotate = e.target.checked; }
    if (e.target.id === 'glCountry') {
      var iso = e.target.value;
      var idx = -1;
      if (iso) {
        var cs = G.countries();
        for (var i = 0; i < cs.length; i++) if (cs[i].iso === iso) { idx = i; break; }
      }
      GL.selectCountry(idx);
    }
    if (e.target.id === 'glProvince') GL.selectProvince(e.target.value);
    if (e.target.id === 'glKab') GL.selectKab(e.target.value);
  });

  document.addEventListener('visibilitychange', function () {
    if (document.hidden) GL.stopLoop();
    else if (S.mounted) GL.startLoop();
  });
})();
