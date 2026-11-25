/* Globe Radar — controller: state, i18n, panel shell and filters (ADR 0090).
 * Rendering is delegated to WebGlobe.Renderer (WebGL2); 05-globe-interact.js owns the frame loop,
 * camera and pointer input, 06-globe-ui.js the panels, data and live updates.
 * Relies on globals: svgIcon, escapeHtml, t, I18N, openHostDetail, openHostTerminal, openNetDiagModal,
 * buildHostCard, allRawHosts, langRerenderHooks. */
(function () {
  'use strict';

  var GL = (window.GlobeRadar = {});
  var G = window.WebGlobe;
  var HOST_ID = 'hostGlobe';
  var RAD = Math.PI / 180;

  var S = {
    graph: null,
    renderer: null,
    canvas: null,
    flowFilter: 'all',
    hostFilter: null,     // Set of dashboard host ids the dashboard filter lets through; null = every node shown
    selected: null,       // node id whose detail is shown in the right panel
    selectedCountry: -1,  // index into WebGlobe.countries() whose full province set is shown
    selectedProvince: '', // Indonesia province code (e.g. '31' for DKI Jakarta); '' = none
    selectedKab: '',      // kabupaten/kota code (e.g. '31.01'); '' = none
    adminFit: null,       // {west,east,south,north} of the focused province/kabupaten
    hoverPos: null,       // last pointer position over the canvas, to refresh the hover while the globe turns
    autoRotate: true,
    lon: 100 * RAD,       // camera centre, radians (the view opens over Asia)
    lat: 15 * RAD,
    zoom: 1,
    velLon: 0,            // inertia after a drag, rad/ms
    velLat: 0,
    dragging: false,
    pointerMoved: false,
    lastPointer: null,
    lastMove: 0,
    fly: null,            // {lon, lat, zoom} target while flying to a host
    sceneDirty: false,    // pins/arcs need re-uploading (graph, filter or selection changed)
    tintsDirty: false,    // country highlights need repainting
    raf: 0,
    lastFrame: 0,
    source: null,
    mounted: false,       // the Hosts view is showing the globe (SSE, loop and renderer live)
    burst: 0,             // performance.now() deadline of the live-traffic pulse boost
    loadTimer: 0,
    hostReloadTimer: 0,
    lastHostSig: '',
  };

  var esc = function (s) { return escapeHtml(String(s === null || s === undefined ? '' : s)); };
  var icon = function (n) { return svgIcon(n, 'icon-sm'); };

  var I = {
    en: {
      gl_title: '3D Globe Network Radar',
      gl_all: 'All', gl_proxy: 'Proxy/App', gl_vpn: 'VPN', gl_k8s: 'K8s', gl_gateway: 'Gateway',
      gl_autorotate: 'Auto-rotate',
      gl_refresh: 'Refresh', gl_nodes: 'nodes', gl_links: 'links',
      gl_empty: 'No infrastructure yet. Add a Host to see it on the globe.',
      gl_loading: 'Loading globe…', gl_failed: 'Could not load globe',
      gl_webgl_failed: 'WebGL2 is unavailable in this browser.',
      gl_healthy: 'Healthy', gl_degraded: 'Degraded', gl_down: 'Down', gl_unknown: 'Unknown',
      gl_terminal: 'Terminal', gl_diag: 'Diagnostics', gl_workspace: 'Host Workspace',
      gl_no_host: 'This node is not a managed Host.', gl_alerts: 'alerts',
      gl_latency: 'Latency', gl_geo: 'Geo', gl_region: 'Region', gl_lat: 'Lat', gl_lon: 'Lon', gl_label: 'Label',
      gl_setgeo: 'Set override', gl_geo_hint: 'Manual override for private / air-gapped hosts',
      gl_live: 'LIVE', gl_reconnecting: 'RECONNECTING', gl_offline: 'OFFLINE',
      gl_zoom_in: 'Zoom in', gl_zoom_out: 'Zoom out', gl_zoom_reset: 'Reset view',
      gl_geo_source: 'Geo source', gl_peer_count: 'Peers', gl_bidirectional: 'Bi-directional',
      gl_geo_unknown: 'Location not set (fallback 0,0)',
      gl_geo_prompt: 'No direct public IP detected. Location is currently at Gateway (0,0). Pick a city preset or enter coordinates below to place it accurately.',
      gl_preset_city: 'City preset…', gl_needs_geo: 'Needs location',
      gl_host_one: '{n} host', gl_host_many: '{n} hosts',
      gl_stack_title: '{n} nodes here', gl_stack_more: '+{n} more', gl_stack_hint: 'Click to zoom in',
      gl_region_sel: 'Country', gl_region_all: 'Whole world', gl_region_hint: 'Select a country to show all its provinces',
      gl_region_clear: 'Clear country',
      gl_prov_sel: 'Province', gl_prov_all: 'All provinces', gl_kab_sel: 'Kabupaten/Kota', gl_kab_all: 'All kabupaten/kota',
      gl_admin_hint: 'Choose a province, then a kabupaten/kota to focus the map',
      gl_hosts: 'Hosts', gl_back_hosts: 'Back to hosts', gl_section_geo: 'Location & network',
    },
    id: {
      gl_title: 'Radar Jaringan 3D',
      gl_all: 'Semua', gl_proxy: 'Proxy/Aplikasi', gl_vpn: 'VPN', gl_k8s: 'K8s', gl_gateway: 'Gateway',
      gl_autorotate: 'Putar otomatis',
      gl_refresh: 'Muat ulang', gl_nodes: 'node', gl_links: 'alur',
      gl_empty: 'Belum ada infrastruktur. Tambahkan Host untuk melihatnya di globe.',
      gl_loading: 'Memuat globe…', gl_failed: 'Gagal memuat globe',
      gl_webgl_failed: 'WebGL2 tidak tersedia di browser ini.',
      gl_healthy: 'Sehat', gl_degraded: 'Degraded', gl_down: 'Down', gl_unknown: 'Unknown',
      gl_terminal: 'Terminal', gl_diag: 'Diagnostik', gl_workspace: 'Workspace Host',
      gl_no_host: 'Node ini bukan Host yang dikelola.', gl_alerts: 'alert',
      gl_latency: 'Latensi', gl_geo: 'Geo', gl_region: 'Region', gl_lat: 'Lat', gl_lon: 'Lon', gl_label: 'Label',
      gl_setgeo: 'Simpan override', gl_geo_hint: 'Override manual untuk host privat / air-gapped',
      gl_live: 'LANGSUNG', gl_reconnecting: 'MENGHUBUNGKAN', gl_offline: 'OFFLINE',
      gl_zoom_in: 'Perbesar', gl_zoom_out: 'Perkecil', gl_zoom_reset: 'Atur ulang',
      gl_geo_source: 'Sumber geo', gl_peer_count: 'Peer', gl_bidirectional: 'Dua arah',
      gl_geo_unknown: 'Lokasi belum diatur (default 0,0)',
      gl_geo_prompt: 'Tidak ada IP publik langsung terdeteksi. Posisi saat ini di Gateway (0,0). Pilih preset kota atau isi koordinat di bawah agar posisi host presisi.',
      gl_preset_city: 'Pilih preset kota…', gl_needs_geo: 'Perlu lokasi',
      gl_host_one: '{n} host', gl_host_many: '{n} host',
      gl_stack_title: '{n} node di sini', gl_stack_more: '+{n} lainnya', gl_stack_hint: 'Klik untuk memperbesar',
      gl_region_sel: 'Negara', gl_region_all: 'Seluruh dunia', gl_region_hint: 'Pilih negara untuk menampilkan semua provinsinya',
      gl_region_clear: 'Hapus negara',
      gl_prov_sel: 'Provinsi', gl_prov_all: 'Semua provinsi', gl_kab_sel: 'Kabupaten/Kota', gl_kab_all: 'Semua kabupaten/kota',
      gl_admin_hint: 'Pilih provinsi, lalu kabupaten/kota untuk memfokuskan peta',
      gl_hosts: 'Host', gl_back_hosts: 'Kembali ke host', gl_section_geo: 'Lokasi & jaringan',
    },
  };
  ['en', 'id'].forEach(function (l) { I18N[l] = Object.assign(I18N[l] || {}, I[l]); });

  // ---- Panel shell (rendered inline into #hostGlobe) -----------------------
  // Left: toolbar + globe stage. Right: host list, or the selected node's detail (06-globe-ui.js).
  function panelHtml() {
    return '' +
      '<div class="gl-globe-col">' +
        '<div class="gl-toolbar">' +
          '<span id="glLivePill" class="gl-live-pill gl-offline"><span class="gl-live-dot"></span><span>' + esc(t('gl_offline')) + '</span></span>' +
          '<span id="glCount" class="gl-count"></span>' +
          '<select id="glCountry" class="gl-select" title="' + esc(t('gl_region_hint')) + '" aria-label="' + esc(t('gl_region_sel')) + '"><option value="">' + esc(t('gl_region_all')) + '</option></select>' +
          '<select id="glProvince" class="gl-select" title="' + esc(t('gl_admin_hint')) + '" aria-label="' + esc(t('gl_prov_sel')) + '" disabled><option value="">' + esc(t('gl_prov_all')) + '</option></select>' +
          '<select id="glKab" class="gl-select" title="' + esc(t('gl_admin_hint')) + '" aria-label="' + esc(t('gl_kab_sel')) + '" disabled><option value="">' + esc(t('gl_kab_all')) + '</option></select>' +
          '<div id="glFlowPills" class="gl-pills" role="group" aria-label="link type"></div>' +
          '<label class="gl-toggle"><input type="checkbox" id="glAutoRotate" checked /> ' + esc(t('gl_autorotate')) + '</label>' +
          '<div class="gl-actions">' +
            '<button class="btn btn-icon btn-sm btn-ghost" id="glZoomIn" title="' + esc(t('gl_zoom_in')) + '">' + icon('plus') + '</button>' +
            '<button class="btn btn-icon btn-sm btn-ghost" id="glZoomOut" title="' + esc(t('gl_zoom_out')) + '">' + icon('minus') + '</button>' +
            '<button class="btn btn-icon btn-sm btn-ghost" id="glZoomReset" title="' + esc(t('gl_zoom_reset')) + '">' + icon('rotate-ccw') + '</button>' +
            '<button class="btn btn-icon btn-sm btn-ghost" id="glRefreshBtn" title="' + esc(t('gl_refresh')) + '">' + icon('refresh-cw') + '</button>' +
          '</div>' +
        '</div>' +
        '<div class="gl-stage" id="glStage">' +
          '<canvas id="glCanvas" class="gl-canvas" role="img" aria-label="' + esc(t('gl_title')) + '"></canvas>' +
          '<div id="glEmpty" class="gl-empty" hidden>' + esc(t('gl_empty')) + '</div>' +
          '<div id="glTooltip" class="gl-tooltip" hidden></div>' +
        '</div>' +
      '</div>' +
      '<aside class="gl-detail">' +
        '<div id="glListView" class="gl-list-view">' +
          '<div class="gl-side-head"><span class="gl-side-title">' + esc(t('gl_hosts')) + '</span><span id="glSideCount" class="gl-side-meta"></span></div>' +
          '<div id="glSideList"></div>' +
        '</div>' +
        '<div id="glHud" class="gl-hud" hidden></div>' +
      '</aside>';
  }

  // Builds the panel once; later mounts keep the DOM and the selection.
  GL.ensurePanel = function () {
    var host = document.getElementById(HOST_ID);
    if (host && !host.children.length) {
      host.innerHTML = panelHtml();
      restoreControls();
    }
    return !!host;
  };

  // A language switch re-renders the panel HTML, which replaces the canvas: drop the old GL context's
  // resources and build a renderer on the new canvas (only while mounted).
  function rebuildPanel() {
    var host = document.getElementById(HOST_ID);
    if (!host || !host.children.length) return;
    if (S.renderer) { S.renderer.dispose(true); S.renderer = null; }
    host.innerHTML = panelHtml();
    if (S.mounted && window.WebGlobe && window.WebGlobe.Renderer) {
      var cvs = document.getElementById('glCanvas');
      if (cvs) S.renderer = new window.WebGlobe.Renderer(cvs);
    }
    restoreControls();
    GL.bindCanvas();
    GL.renderUI();
  }
  if (typeof langRerenderHooks !== 'undefined') langRerenderHooks.push(rebuildPanel);

  function restoreControls() {
    var auto = document.getElementById('glAutoRotate');
    if (auto) auto.checked = S.autoRotate;
    var country = document.getElementById('glCountry');
    if (country) country.value = S.selectedCountry >= 0 ? (G.countries()[S.selectedCountry] || {}).iso || '' : '';
    if (GL.renderProvinces) GL.renderProvinces();
    if (GL.renderKab) GL.renderKab();
  }

  // ---- Filtering -----------------------------------------------------------
  // Host nodes pass only when the dashboard filter (GL.refreshHostFilter in 06) admits their host.
  function visible(node) {
    return !S.hostFilter || node.kind !== 'host' || S.hostFilter.has(node.host_id);
  }

  function linkVisible(flow) {
    if (S.flowFilter === 'all') return true;
    if (S.flowFilter === 'gateway') return flow.type === 'ssh' || flow.type === 'vpn';
    if (S.flowFilter === 'proxy') return flow.type === 'proxy' || flow.type === 'app';
    return flow.type === S.flowFilter;
  }

  function nodeById(id) {
    return S.graph ? S.graph.nodes.find(function (n) { return n.id === id; }) : null;
  }

  function peerCount(id) {
    if (!S.graph) return 0;
    return S.graph.links.filter(function (l) {
      return l.source_id === id || l.target_id === id;
    }).length;
  }

  // '#rrggbb' theme token -> 'rgba(r,g,b,a)' for the Canvas 2D highlight painter.
  function rgba(token, fallback, alpha) {
    var hex = (getComputedStyle(document.documentElement).getPropertyValue(token).trim() || fallback).replace('#', '');
    var n = parseInt(hex.length === 3 ? hex.replace(/./g, '$&$&') : hex, 16);
    if (isNaN(n)) n = parseInt(fallback.replace('#', ''), 16);
    return 'rgba(' + ((n >> 16) & 255) + ',' + ((n >> 8) & 255) + ',' + (n & 255) + ',' + alpha + ')';
  }

  // Shared state + helpers consumed by 05-globe-interact.js and 06-globe-ui.js.
  GL.state = S;
  GL.visible = visible;
  GL.linkVisible = linkVisible;
  GL.nodeById = nodeById;
  GL.peerCount = peerCount;
  GL.rgba = rgba;
})();
