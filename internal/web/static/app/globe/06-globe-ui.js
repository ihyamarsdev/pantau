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
    GL.refreshHostFilter();
    GL.renderPills();
    GL.renderCountries();
    GL.renderProvinces();
    GL.renderKab();
    GL.renderCount();
    GL.renderDetail();
    var empty = document.getElementById('glEmpty');
    if (empty) {
      var failed = !!(S.renderer && !S.renderer.ok);
      empty.classList.toggle('gl-fail', failed);
      empty.textContent = t(failed ? 'gl_webgl_failed' : 'gl_empty');
      empty.hidden = !!(S.renderer && S.renderer.ok) && (!S.graph || S.graph.nodes.length > 1);
    }
    S.sceneDirty = S.tintsDirty = true;
  };

  // The dashboard filter (search, group, status) decides which hosts the globe shows. It is active only when a
  // filter control holds a value, and is computed once per refresh rather than per node.
  GL.refreshHostFilter = function () {
    var q = document.getElementById('hostSearchInput');
    var g = document.getElementById('groupFilterSelect');
    var s = document.getElementById('statusFilterSelect');
    var active = (q && q.value.trim() !== '') || (g && g.value !== 'all') || (s && s.value !== 'all');
    S.hostFilter = active && typeof window.getFilteredAndSortedHosts === 'function'
      ? new Set(window.getFilteredAndSortedHosts().map(function (h) { return h.id; }))
      : null;
  };

  // What the filters let through: nodes by the dashboard host filter, links by type and both ends being shown.
  // The header count and the renderer share this one answer.
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

  // Countries that hold a managed Host are identified by their 3D pins and traffic streams;
  // country land remains natural Earth green without a heavy permanent wash.
  // Hover and explicit dropdown selection retain their accents.
  GL.syncTints = function () {
    S.tintsDirty = false;
    if (!S.renderer || !S.renderer.ok || !S.graph) return;
    var items = [];
    var edge = GL.rgba('--accent-text', '#9aa3f8', 0.95);
    if (S.hoverCountry >= 0) items.push({ idx: S.hoverCountry, fill: 'rgba(255,255,255,0.08)', stroke: edge, width: 1.4 });
    var sel = GL.nodeById(S.selected);
    if (sel && sel._ci >= 0) items.push({ idx: sel._ci, fill: GL.rgba('--primary', '#5e6ad2', 0.25), stroke: edge, width: 2 });
    if (S.selectedCountry >= 0) items.push({ idx: S.selectedCountry, fill: GL.rgba('--primary', '#5e6ad2', 0.25), stroke: edge, width: 2.4 });
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
  // Populate the country selector from the embedded country dataset (localised names), preserving the current
  // selection. Listed countries are those that actually have admin-1 province data, so choosing one always reveals
  // a complete province set; countries without data are not offered.
  GL.renderCountries = function () {
    var sel = document.getElementById('glCountry');
    if (!sel) return;
    var lang = document.documentElement.lang || 'en';
    var cs = G.countries();
    var rows = cs.map(function (c, i) { return { c: c, i: i }; })
      .filter(function (r) { return !!r.c.iso; })
      .sort(function (a, b) {
        return G.countryName(a.c, lang).localeCompare(G.countryName(b.c, lang));
      });
    sel.innerHTML = '<option value="">' + esc(t('gl_region_all')) + '</option>' + rows.map(function (r) {
      return '<option value="' + esc(r.c.iso) + '">' + esc(G.countryName(r.c, lang)) + '</option>';
    }).join('');
    sel.value = S.selectedCountry >= 0 ? (cs[S.selectedCountry] || {}).iso || '' : '';
  };

  // Select a country by its index in WebGlobe.countries() (-1 clears). Fits the camera over the country's full
  // bounds and marks the detail layer for a full-country province redraw. Host pins/HUD are untouched.
  // Clearing (-1) is the "Whole world" option: besides emptying the hierarchy it flies the camera back to the
  // home world view, so the picker label and the actual camera agree. Only an explicit selection triggers this;
  // ordinary wheel/drag zoom with no selection is left alone (see the wheel/drag handlers in 05-globe-interact).
  GL.selectCountry = function (idx) {
    S.selectedCountry = idx;
    S.countryFit = null;
    S.selectedProvince = '';
    S.selectedKab = '';
    S.adminFit = null;
    var sel = document.getElementById('glCountry');
    if (sel) sel.value = idx >= 0 ? (G.countries()[idx] || {}).iso || '' : '';
    if (idx >= 0) {
      var c = G.countries()[idx];
      var b = c.bbox;
      var latDeg = (b[1] + b[3]) / 2, lonDeg = (b[0] + b[2]) / 2;
      var halfLat = Math.max(1.5, (b[3] - b[1]) / 2);
      var halfLon = Math.max(1.5, (b[2] - b[0]) / 2);
      S.countryFit = { west: b[0], east: b[2], south: b[1], north: b[3] };
      if (S.renderer && S.renderer.ok) {
        var zoom = S.renderer.zoomForRegion(latDeg, halfLat, halfLon);
        S.velLon = S.velLat = 0;
        S.fly = { lon: lonDeg * Math.PI / 180, lat: latDeg * Math.PI / 180, zoom: zoom };
      }
    } else {
      // "Whole world" was chosen explicitly: reset the camera to the home world view. flyHome lives in
      // 05-globe-interact.js and only touches camera state, so it cannot recurse back into selection.
      GL.flyHome();
    }
    // Indonesia has named admin-2 data: start the lazy import and fill the province selector once decoded.
    // Other countries keep the selectors disabled; their existing Natural Earth admin-1 experience is unchanged.
    if (adminCountry()) {
      G.admin2Load().then(function () {
        if (S.selectedCountry === idx) { GL.renderProvinces(); GL.renderKab(); }
      });
    } else {
      GL.renderProvinces();
      GL.renderKab();
    }
    S.tintsDirty = true;
  };

  // ---- Indonesia administrative hierarchy (Country -> Province -> Kabupaten/Kota) ---------------------
  // The province and kabupaten/kota selectors are populated from the embedded, named 110 m polygons
  // (cahyadsn/wilayah_boundaries, MIT). They stay disabled until a country with data (Indonesia, ISO ID) is
  // selected, and are never shown empty: a country without admin-2 data never enables them.
  function adminCountry() {
    var c = S.selectedCountry >= 0 ? G.countries()[S.selectedCountry] : null;
    return c && c.iso === 'ID' ? c : null;
  }

  GL.renderProvinces = function () {
    var sel = document.getElementById('glProvince');
    if (!sel) return;
    var ready = !!adminCountry() && G.admin2Ready();
    sel.disabled = !ready;
    var opts = '<option value="">' + esc(t('gl_prov_all')) + '</option>';
    if (ready) {
      var rows = G.admin2().p.slice().sort(function (a, b) { return a.name.localeCompare(b.name); });
      opts += rows.map(function (p) {
        return '<option value="' + esc(p.code) + '">' + esc(p.name) + '</option>';
      }).join('');
    }
    sel.innerHTML = opts;
    sel.value = ready ? S.selectedProvince : '';
  };

  GL.renderKab = function () {
    var sel = document.getElementById('glKab');
    if (!sel) return;
    var ready = !!adminCountry() && !!S.selectedProvince && G.admin2Ready();
    sel.disabled = !ready;
    var opts = '<option value="">' + esc(t('gl_kab_all')) + '</option>';
    if (ready) {
      var rows = G.admin2Kab(S.selectedProvince).slice().sort(function (a, b) { return a.name.localeCompare(b.name); });
      opts += rows.map(function (k) {
        return '<option value="' + esc(k.code) + '">' + esc(k.name) + '</option>';
      }).join('');
    }
    sel.innerHTML = opts;
    sel.value = ready ? S.selectedKab : '';
  };

  function bboxOf(feature) {
    var b = feature.bbox;
    return { west: b[0], east: b[2], south: b[1], north: b[3] };
  }

  function flyToBbox(b) {
    if (!S.renderer || !S.renderer.ok) return;
    var latDeg = (b.south + b.north) / 2, lonDeg = (b.west + b.east) / 2;
    // Use the real half-extents (no 0.25-deg floor): a selected kota must fill the stage. The renderer's
    // zoomForRegion already has a tiny 0.01-deg guard, and the frame loop clamps the result to MAX_ZOOM.
    var halfLat = Math.max(0.005, (b.north - b.south) / 2);
    var halfLon = Math.max(0.005, (b.east - b.west) / 2);
    var zoom = S.renderer.zoomForRegion(latDeg, halfLat, halfLon);
    S.velLon = S.velLat = 0;
    S.fly = { lon: lonDeg * Math.PI / 180, lat: latDeg * Math.PI / 180, zoom: zoom };
  }

  // The detail layer's admin payload: all kabupaten/kota outlines of the province for orientation, plus the
  // selected province/kabupaten highlighted. `focus` lets the renderer raise its texture cap for a sharp close-up.
  function adminView() {
    if (!G.admin2Ready() || !S.selectedProvince) return null;
    var province = G.admin2().p.find(function (p) { return p.code === S.selectedProvince; }) || null;
    var kab = S.selectedKab ? G.admin2().k.find(function (k) { return k.code === S.selectedKab; }) : null;
    return {
      lines: G.admin2Kab(S.selectedProvince),
      province: province, kab: kab,
      focus: S.selectedCountry >= 0 && !!S.adminFit
    };
  }
  GL.adminView = adminView;

  GL.selectProvince = function (code) {
    S.selectedProvince = code || '';
    S.selectedKab = '';
    S.adminFit = null;
    var sel = document.getElementById('glProvince');
    if (sel) sel.value = S.selectedProvince;
    GL.renderKab();
    if (S.selectedProvince && G.admin2Ready()) {
      var p = G.admin2().p.find(function (x) { return x.code === S.selectedProvince; });
      if (p) { S.adminFit = bboxOf(p); flyToBbox(S.adminFit); }
    }
    S.tintsDirty = true;
  };

  GL.selectKab = function (code) {
    S.selectedKab = code || '';
    S.adminFit = null;
    var sel = document.getElementById('glKab');
    if (sel) sel.value = S.selectedKab;
    if (S.selectedKab && G.admin2Ready()) {
      var k = G.admin2().k.find(function (x) { return x.code === S.selectedKab; });
      if (k) { S.adminFit = bboxOf(k); flyToBbox(S.adminFit); }
    }
    S.tintsDirty = true;
  };

  // ponytail: Quick city presets for private / NAT hosts lacking direct public IP (e.g. Yogyakarta, Jakarta, Surabaya).
  var CITY_PRESETS = [
    { name: 'Yogyakarta', lat: -7.7956, lon: 110.3695, region: 'DI Yogyakarta' },
    { name: 'Jakarta', lat: -6.2088, lon: 106.8456, region: 'DKI Jakarta' },
    { name: 'Surabaya', lat: -7.2575, lon: 112.7521, region: 'Jawa Timur' },
    { name: 'Bandung', lat: -6.9175, lon: 107.6191, region: 'Jawa Barat' },
    { name: 'Semarang', lat: -6.9667, lon: 110.4167, region: 'Jawa Tengah' },
    { name: 'Medan', lat: 3.5952, lon: 98.6722, region: 'Sumatera Utara' },
    { name: 'Makassar', lat: -5.1477, lon: 119.4327, region: 'Sulawesi Selatan' },
    { name: 'Denpasar (Bali)', lat: -8.6705, lon: 115.2126, region: 'Bali' },
    { name: 'Palembang', lat: -2.9761, lon: 104.7754, region: 'Sumatera Selatan' },
    { name: 'Batam', lat: 1.1301, lon: 104.0529, region: 'Kepulauan Riau' },
    { name: 'Balikpapan', lat: -1.2379, lon: 116.8529, region: 'Kalimantan Timur' },
    { name: 'Singapore', lat: 1.3521, lon: 103.8198, region: 'Singapore' },
  ];

  GL.renderSidebar = function () {
    var box = document.getElementById('glSideList');
    if (!box || !S.graph) return;
    var hosts = S.graph.nodes.filter(function (n) { return n.kind === 'host' && GL.visible(n); });
    box.innerHTML = hosts.map(function (n) {
      var isUnset = n.geo_source === 'gateway' || (!n.lat && !n.lon);
      var meta = isUnset ? ('⚠️ ' + esc(t('gl_needs_geo'))) : esc(n.city || n.region || n.group_id || '');
      return '<button type="button" class="gl-side-item' + (S.selected === n.id ? ' active' : '') + '" data-node="' + esc(n.id) + '">' +
        '<span class="gl-side-dot ' + esc(n.status) + '"></span>' +
        '<span class="gl-side-name">' + esc(n.name) + '</span>' +
        '<span class="gl-side-meta">' + meta + '</span>' +
      '</button>';
    }).join('');
    var count = document.getElementById('glSideCount');
    if (count) count.textContent = hosts.length;
  };

  GL.renderCount = function () {
    var c = document.getElementById('glCount');
    if (!c || !S.graph) return;
    var f = GL.filtered();
    c.textContent = f.nodes.length + ' ' + t('gl_nodes') + ' · ' + f.links.length + ' ' + t('gl_links');
  };

  // ---- HUD (right-panel detail of the selected node) -----------------------
  GL.openHud = function (id, doFly) {
    S.selected = id;
    var n = GL.nodeById(id);
    if (n && doFly) GL.flyTo(n);
    GL.renderDetail();
    S.sceneDirty = S.tintsDirty = true;
  };

  GL.closeHud = function () {
    S.selected = null;
    GL.renderDetail();
    S.sceneDirty = S.tintsDirty = true;
  };

  // The right panel shows either the host list or the selected node's detail, never both.
  GL.renderDetail = function () {
    var n = S.selected ? GL.nodeById(S.selected) : null;
    if (S.selected && !n) S.selected = null;
    var list = document.getElementById('glListView'), hud = document.getElementById('glHud');
    if (!list || !hud) return;
    list.hidden = !!n;
    hud.hidden = !n;
    if (n) GL.renderHud(); else GL.renderSidebar();
  };

  // The Grid view's own host card for a dashboard host, so the detail matches the Hosts list.
  function hostCardFor(n) {
    if (n.kind !== 'host' || typeof window.buildHostCard !== 'function') return null;
    var h = (window.allRawHosts || []).find(function (x) { return x.id === n.host_id; });
    return h ? window.buildHostCard(h, false) : null;
  }

  GL.renderHud = function () {
    var hud = document.getElementById('glHud');
    var n = GL.nodeById(S.selected);
    if (!hud || !n) return;
    var isHost = n.kind === 'host';
    var isGateway = n.kind === 'gateway';
    var canSetGeo = isHost || isGateway;
    var isUnset = (isHost && (n.geo_source === 'gateway' || (!n.lat && !n.lon))) || (isGateway && (!n.lat && !n.lon));
    var lat = n.lat ? n.lat.toFixed(2) : '0.00';
    var lon = n.lon ? n.lon.toFixed(2) : '0.00';
    var country = n._ci >= 0 ? G.countryName(G.countries()[n._ci], document.documentElement.lang || 'en') : (n.country_code || '');
    var presetsHtml = CITY_PRESETS.map(function (p, idx) {
      return '<option value="' + idx + '">' + esc(p.name) + '</option>';
    }).join('');
    hud.innerHTML = '' +
      '<div class="gl-hud-head">' +
        '<div class="gl-hud-title"><span class="gl-hud-dot ' + esc(n.status) + '"></span><strong>' + esc(n.name) + '</strong></div>' +
        '<button class="btn btn-sm btn-ghost" id="glHudClose">' + icon('chevron-left') + ' ' + esc(t('gl_back_hosts')) + '</button>' +
      '</div>' +
      '<div class="gl-hud-body">' +
        '<div class="gl-hud-section">' + esc(t('gl_section_geo')) + '</div>' +
        (isUnset ? '<div class="gl-hud-warn">' + esc(t('gl_geo_prompt')) + '</div>' : '') +
        '<div class="gl-hud-stats">' +
          '<span><b>' + esc(t('gl_geo')) + '</b> ' + (isUnset ? '⚠️ ' + esc(t('gl_geo_unknown')) : esc(n.city || '—') + (country ? ', ' + esc(country) : '')) + '</span>' +
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
        (canSetGeo ? '' : '<div class="gl-hud-note">' + esc(t('gl_no_host')) + '</div>') +
        (canSetGeo ? '<div class="gl-geo-form">' +
          '<label class="gl-geo-wide">' + esc(t('gl_preset_city')) + '<select id="glGeoPreset"><option value="">' + esc(t('gl_preset_city')) + '</option>' + presetsHtml + '</select></label>' +
          '<label>' + esc(t('gl_lat')) + '<input id="glGeoLat" type="number" step="any" value="' + lat + '" /></label>' +
          '<label>' + esc(t('gl_lon')) + '<input id="glGeoLon" type="number" step="any" value="' + lon + '" /></label>' +
          '<label class="gl-geo-wide">' + esc(t('gl_region')) + '<input id="glGeoRegion" type="text" value="' + esc(n.region || '') + '" /></label>' +
          '<label class="gl-geo-wide">' + esc(t('gl_label')) + '<input id="glGeoLabel" type="text" value="' + esc(n.city || '') + '" /></label>' +
          '<button class="btn btn-sm gl-geo-wide" id="glGeoSave">' + esc(t('gl_setgeo')) + '</button>' +
          '<div class="gl-geo-wide gl-side-meta">' + esc(t('gl_geo_hint')) + '</div>' +
        '</div>' : '') +
      '</div>';
    var card = hostCardFor(n);
    if (card) hud.querySelector('.gl-hud-body').prepend(card);
    document.getElementById('glHudClose').onclick = GL.closeHud;
    var term = document.getElementById('glHudTerminal');
    if (term && isHost) term.onclick = function () { if (typeof openHostTerminal === 'function') openHostTerminal(n.host_id); };
    var diag = document.getElementById('glHudDiag');
    if (diag && isHost) diag.onclick = function () { if (typeof openNetDiagModal === 'function') openNetDiagModal(n.host_id); };
    var ws = document.getElementById('glHudWorkspace');
    if (ws && isHost) ws.onclick = function () { if (typeof openHostDetail === 'function') openHostDetail(n.host_id); };
    var preset = document.getElementById('glGeoPreset');
    if (preset && canSetGeo) {
      preset.onchange = function () {
        var idx = parseInt(preset.value, 10);
        if (isNaN(idx) || !CITY_PRESETS[idx]) return;
        var p = CITY_PRESETS[idx];
        document.getElementById('glGeoLat').value = p.lat;
        document.getElementById('glGeoLon').value = p.lon;
        document.getElementById('glGeoRegion').value = p.region;
        document.getElementById('glGeoLabel').value = p.name;
      };
    }
    var save = document.getElementById('glGeoSave');
    if (save && canSetGeo) save.onclick = function () { GL.saveGeo(n); };
  };

  GL.saveGeo = function (n) {
    var lat = parseFloat(document.getElementById('glGeoLat').value);
    var lon = parseFloat(document.getElementById('glGeoLon').value);
    var region = document.getElementById('glGeoRegion').value || '';
    var label = document.getElementById('glGeoLabel').value || '';
    if (isNaN(lat) || isNaN(lon)) return;
    var hostID = n.host_id || 0;
    fetch('/api/network-radar/globe/geo', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id: hostID, lat: lat, lon: lon, region: region, label: label }),
    }).then(function (res) {
      if (!res.ok) return;
      n.lat = lat; n.lon = lon; n.region = region; n.city = label; n.geo_source = 'manual';
      n._ci = countryOf(lat, lon);
      GL.renderUI();
    }).catch(function () { /* offline: keep current position */ });
  };

  // ---- Data loading & host status sync -------------------------------------
  function normStatus(s) {
    s = String(s || '').toLowerCase();
    return (s === 'healthy' || s === 'degraded' || s === 'down') ? s : 'unknown';
  }
  function flowStatus(s) {
    s = normStatus(s);
    return s === 'unknown' ? 'degraded' : s;
  }
  function combinedStatus(a, b) {
    a = normStatus(a); b = normStatus(b);
    if (a === 'down' || b === 'down') return 'down';
    if (a === 'degraded' || b === 'degraded' || a === 'unknown' || b === 'unknown') return 'degraded';
    return 'healthy';
  }
  function hostSig(list) {
    if (!Array.isArray(list) || !list.length) return '';
    return list.map(function (h) { return h.id; }).sort(function (a, b) { return a - b; }).join(',');
  }

  function syncLinks() {
    if (!S.graph || !Array.isArray(S.graph.links)) return false;
    var byId = {};
    S.graph.nodes.forEach(function (n) { byId[n.id] = n; });
    var changed = false;
    S.graph.links.forEach(function (l) {
      var src = byId[l.source_id], tgt = byId[l.target_id];
      if (!src || !tgt) return;
      var nextStatus = l.status;
      var nextActive = l.active;
      if ((src.kind === 'gateway' || src.kind === 'cloudflare') && tgt.kind === 'host') {
        nextStatus = flowStatus(tgt.status);
        if (src.kind === 'gateway') nextActive = nextStatus !== 'down';
      } else if (src.kind === 'host' && tgt.kind === 'host' && l.type !== 'vpn') {
        nextStatus = combinedStatus(src.status, tgt.status);
      }
      if (nextStatus !== l.status || nextActive !== l.active) {
        l.status = nextStatus;
        l.active = nextActive;
        changed = true;
      }
    });
    return changed;
  }

  // Reconcile S.graph host nodes and links with the latest /api/hosts list so
  // the HUD header dot, sidebar dot, 3D pins, and arcs never lag behind host cards.
  GL.syncHosts = function () {
    if (!S.graph || !Array.isArray(window.allRawHosts)) return false;
    var byHostId = {};
    window.allRawHosts.forEach(function (h) { byHostId[h.id] = h; });
    var changed = false;
    S.graph.nodes.forEach(function (n) {
      if (n.kind !== 'host') return;
      var h = byHostId[n.host_id];
      if (!h) return;
      var nextStatus = normStatus(h.status);
      if (n.status !== nextStatus) {
        n.status = nextStatus;
        if (nextStatus === 'healthy') n.active_alerts = 0;
        changed = true;
      }
      if (h.name && n.name !== h.name) { n.name = h.name; changed = true; }
      var grp = h.group_name || '';
      if (n.group_id !== grp) { n.group_id = grp; changed = true; }
      var netId = h.network_id || 0;
      if (n.network_id !== netId) { n.network_id = netId; changed = true; }
      var pub = (h.public_ip || '').trim();
      if (n.public_ip !== pub) n.public_ip = pub;
      var ip = pub || (h.host || '').trim();
      if (ip && n.ip !== ip) n.ip = ip;
    });
    if (syncLinks()) changed = true;
    if (changed) S.sceneDirty = true;
    return changed;
  };

  function syncRawHostsFromGraph(g) {
    if (!g || !Array.isArray(g.nodes) || !Array.isArray(window.allRawHosts)) return;
    var byHostId = {};
    g.nodes.forEach(function (n) {
      if (n.kind === 'host' && n.host_id) byHostId[n.host_id] = n;
    });
    window.allRawHosts.forEach(function (h) {
      var n = byHostId[h.id];
      if (n && n.status) h.status = n.status;
    });
  }

  function scheduleGraphLoad() {
    if (S.loadTimer) clearTimeout(S.loadTimer);
    S.loadTimer = setTimeout(function () {
      S.loadTimer = 0;
      if (S.mounted) GL.load();
    }, 250);
  }

  function scheduleHostsReload() {
    if (typeof window.loadHosts !== 'function') return;
    if (S.hostReloadTimer) clearTimeout(S.hostReloadTimer);
    S.hostReloadTimer = setTimeout(function () {
      S.hostReloadTimer = 0;
      if (S.mounted && typeof window.loadHosts === 'function') window.loadHosts();
    }, 250);
  }

  GL.load = function () {
    return fetch('/api/network-radar/globe').then(function (res) {
      if (!res.ok) throw new Error('HTTP ' + res.status);
      return res.json();
    }).then(function (g) {
      indexCountries(g.nodes);
      S.graph = g;
      S.lastHostSig = hostSig(window.allRawHosts);
      syncRawHostsFromGraph(g);
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
    var isInspect = evt.module === 'inspect' || evt.module === 'inspector';
    var status = attrs.status || (isInspect ? attrs.host_status : null);
    var updated = false;
    if (status && ['healthy', 'degraded', 'down', 'unknown'].indexOf(status) >= 0) {
      var target = evt.host ? S.graph.nodes.find(function (n) { return n.kind === 'host' && n.name === evt.host; }) : null;
      if (target) {
        target.status = status;
        if (status === 'healthy') target.active_alerts = 0;
        syncLinks();
        if (Array.isArray(window.allRawHosts)) {
          var rawHost = window.allRawHosts.find(function (h) { return h.id === target.host_id; });
          if (rawHost) rawHost.status = status;
        }
        if (S.selected === target.id) GL.renderHud();
        GL.renderSidebar();
        S.sceneDirty = true;
        updated = true;
      }
      if (isInspect) scheduleHostsReload();
    }
    var msg = (evt.message || '').toLowerCase();
    if (msg.indexOf('drift') >= 0 || msg.indexOf('alert') >= 0 || msg.indexOf('inspection') >= 0 || (isInspect && !updated)) {
      scheduleGraphLoad();
    }
    // Burst traffic on active inspections (visual only; no scene rebuild).
    if (isInspect || msg.indexOf('probe') >= 0) S.burst = performance.now() + 1200;
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

  // ---- Mount lifecycle (the Hosts view shows the globe; renderHosts drives it) ----
  GL.unmount = function () {
    if (!S.mounted) return;
    if (S.loadTimer) { clearTimeout(S.loadTimer); S.loadTimer = 0; }
    if (S.hostReloadTimer) { clearTimeout(S.hostReloadTimer); S.hostReloadTimer = 0; }
    if (S.source) { S.source.close(); S.source = null; }
    setLive('offline');
    S.mounted = false;
    S.hoverPos = null;
    S.hoverCountry = -1;
    GL.stopLoop();
    if (S.renderer) { S.renderer.dispose(); S.renderer = null; }
  };

  // First mount builds the renderer, stream and loop, then loads the graph. Later mounts reconcile the graph
  // with the dashboard's current host data and only re-fetch when the host set itself changed.
  GL.mount = function () {
    if (S.mounted) {
      GL.syncHosts();
      GL.renderUI();
      var sig = hostSig(window.allRawHosts);
      if (S.graph && sig !== S.lastHostSig) scheduleGraphLoad();
      return;
    }
    if (!GL.ensurePanel()) return;
    S.mounted = true;
    if (!S.renderer) S.renderer = new window.WebGlobe.Renderer(document.getElementById('glCanvas'));
    GL.bindCanvas();
    GL.renderUI();
    connectSSE();
    GL.load();
    GL.startLoop();
  };
})();
