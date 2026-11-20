/* Globe Radar — controller: state, i18n, modal shell and filters (ADR 0090).
 * Rendering is delegated to WebGlobe.Renderer (WebGL2); 05-globe-interact.js owns the frame loop,
 * camera and pointer input, 06-globe-ui.js the panels, data and live updates.
 * Relies on globals: svgIcon, escapeHtml, t, I18N, openModal, closeModal,
 * openHostDetail, openHostTerminal, openNetDiagModal, langRerenderHooks. */
(function () {
  'use strict';

  var GL = (window.GlobeRadar = {});
  var G = window.WebGlobe;
  var MODAL_ID = 'globeModal';
  var RAD = Math.PI / 180;

  var S = {
    graph: null,
    renderer: null,
    canvas: null,
    flowFilter: 'all',
    group: '',
    query: '',
    selected: null,       // node id whose HUD is open
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
    mounted: false,
    burst: 0,             // performance.now() deadline of the live-traffic pulse boost
  };

  var esc = function (s) { return escapeHtml(String(s === null || s === undefined ? '' : s)); };
  var icon = function (n) { return svgIcon(n, 'icon-sm'); };

  var I = {
    en: {
      gl_title: '3D Globe Network Radar', gl_subtitle: 'Geo-located fleet & live 3D traffic streams',
      gl_all: 'All', gl_proxy: 'Proxy/App', gl_vpn: 'VPN', gl_k8s: 'K8s', gl_gateway: 'Gateway',
      gl_all_groups: 'All groups', gl_search_ph: 'Find a host…', gl_autorotate: 'Auto-rotate',
      gl_refresh: 'Refresh', gl_close: 'Close', gl_nodes: 'nodes', gl_links: 'links',
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
      gl_host_one: '{n} host', gl_host_many: '{n} hosts',
      gl_stack_title: '{n} nodes here', gl_stack_more: '+{n} more', gl_stack_hint: 'Click to zoom in',
      gl_region_sel: 'Country', gl_region_all: 'Whole world', gl_region_hint: 'Select a country to show all its provinces',
      gl_region_clear: 'Clear country',
      gl_prov_sel: 'Province', gl_prov_all: 'All provinces', gl_kab_sel: 'Kabupaten/Kota', gl_kab_all: 'All kabupaten/kota',
      gl_admin_hint: 'Choose a province, then a kabupaten/kota to focus the map',
    },
    id: {
      gl_title: 'Radar Jaringan 3D', gl_subtitle: 'Armada ber-geolokasi & aliran lalu lintas 3D langsung',
      gl_all: 'Semua', gl_proxy: 'Proxy/Aplikasi', gl_vpn: 'VPN', gl_k8s: 'K8s', gl_gateway: 'Gateway',
      gl_all_groups: 'Semua grup', gl_search_ph: 'Cari host…', gl_autorotate: 'Putar otomatis',
      gl_refresh: 'Muat ulang', gl_close: 'Tutup', gl_nodes: 'node', gl_links: 'alur',
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
      gl_host_one: '{n} host', gl_host_many: '{n} host',
      gl_stack_title: '{n} node di sini', gl_stack_more: '+{n} lainnya', gl_stack_hint: 'Klik untuk memperbesar',
      gl_region_sel: 'Negara', gl_region_all: 'Seluruh dunia', gl_region_hint: 'Pilih negara untuk menampilkan semua provinsinya',
      gl_region_clear: 'Hapus negara',
      gl_prov_sel: 'Provinsi', gl_prov_all: 'Semua provinsi', gl_kab_sel: 'Kabupaten/Kota', gl_kab_all: 'Semua kabupaten/kota',
      gl_admin_hint: 'Pilih provinsi, lalu kabupaten/kota untuk memfokuskan peta',
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
          '<select id="glCountry" class="gl-select" title="' + esc(t('gl_region_hint')) + '" aria-label="' + esc(t('gl_region_sel')) + '"><option value="">' + esc(t('gl_region_all')) + '</option></select>' +
          '<select id="glProvince" class="gl-select" title="' + esc(t('gl_admin_hint')) + '" aria-label="' + esc(t('gl_prov_sel')) + '" disabled><option value="">' + esc(t('gl_prov_all')) + '</option></select>' +
          '<select id="glKab" class="gl-select" title="' + esc(t('gl_admin_hint')) + '" aria-label="' + esc(t('gl_kab_sel')) + '" disabled><option value="">' + esc(t('gl_kab_all')) + '</option></select>' +
          '<div id="glFlowPills" class="gl-pills" role="group" aria-label="link type"></div>' +
          '<select id="glGroup" class="gl-select"><option value="">' + esc(t('gl_all_groups')) + '</option></select>' +
          '<label class="gl-toggle"><input type="checkbox" id="glAutoRotate" checked /> ' + esc(t('gl_autorotate')) + '</label>' +
        '</div>' +
        '<div class="gl-body">' +
          '<div class="gl-stage" id="glStage">' +
            '<canvas id="glCanvas" class="gl-canvas" role="img" aria-label="' + esc(t('gl_title')) + '"></canvas>' +
            '<div id="glEmpty" class="gl-empty" hidden>' + esc(t('gl_empty')) + '</div>' +
            '<div id="glTooltip" class="gl-tooltip" hidden></div>' +
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

  // A language switch re-renders the modal HTML, which replaces the canvas: drop the old GL context's
  // resources and build a renderer on the new canvas.
  function rebuildModal() {
    var m = document.getElementById(MODAL_ID);
    if (!m) return;
    var search = document.getElementById('glSearch');
    if (search) S.query = search.value;
    m.innerHTML = modalHtml();
    if (S.renderer) { S.renderer.dispose(true); S.renderer = null; }
    if (S.mounted && window.WebGlobe && window.WebGlobe.Renderer) {
      var cvs = document.getElementById('glCanvas');
      if (cvs) S.renderer = new window.WebGlobe.Renderer(cvs);
    }
    restoreControls();
    GL.bindCanvas();
    GL.renderUI();
  }
  if (typeof langRerenderHooks !== 'undefined') langRerenderHooks.push(rebuildModal);

  function restoreControls() {
    var search = document.getElementById('glSearch');
    if (search) search.value = S.query;
    var auto = document.getElementById('glAutoRotate');
    if (auto) auto.checked = S.autoRotate;
    var group = document.getElementById('glGroup');
    if (group) group.value = S.group;
    var country = document.getElementById('glCountry');
    if (country) country.value = S.selectedCountry >= 0 ? (G.countries()[S.selectedCountry] || {}).iso || '' : '';
    if (GL.renderProvinces) GL.renderProvinces();
    if (GL.renderKab) GL.renderKab();
  }

  // ---- Filtering -----------------------------------------------------------
  function visible(node) {
    if (S.group && (node.group_id || '') !== S.group) return false;
    if (S.query) {
      var q = S.query.toLowerCase();
      if (!((node.name || '').toLowerCase().indexOf(q) >= 0 ||
            (node.ip || '').toLowerCase().indexOf(q) >= 0 ||
            (node.public_ip || '').toLowerCase().indexOf(q) >= 0 ||
            (node.city || '').toLowerCase().indexOf(q) >= 0 ||
            (node.region || '').toLowerCase().indexOf(q) >= 0)) return false;
    }
    return true;
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
  GL.ensureModal = ensureModal;
  GL.restoreControls = restoreControls;
  GL.modalId = MODAL_ID;
})();
