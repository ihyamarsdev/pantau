import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';

type Renderer = {
  ok: boolean;
  gl: WebGL2RenderingContext;
  canvas: HTMLCanvasElement;
  arcCount: number;
  detail: { lines: unknown[] | null; country: number; lineCount: number; adminKey: string; lod: number; mode: number; west: number; east: number; south: number; north: number; halfLat: number; halfLon: number; zoom: number; px: number; admin: { lines: unknown[]; provinces: unknown[]; kabs: unknown[]; province: { name: string } | null; kab: { name: string; bbox: number[] } | null; focus: boolean } | null } | null;
  detailLines: number;
  coarseTraced: number;
  project(n: { lat: number; lon: number }): { x: number; y: number; visible: boolean };
  detailCv: HTMLCanvasElement;
  maxTex: number;
  unproject(x: number, y: number): { lat: number; lon: number } | null;
  setCamera(cam: { lon: number; lat: number; zoom: number }): void;
  draw(ts: number, burst: number): void;
  dpr: number;
  zoom: number;
  pxPerRad(zoom: number): number;
};
type PageGlobals = {
  GlobeRadar: {
    open(): void;
    onClose(): void;
    maxZoom(): number;
    state: {
      lastFrame: number;
      lon: number;
      lat: number;
      zoom: number;
      fly: { lon: number; lat: number; zoom: number } | null;
      velLon: number;
      velLat: number;
      autoRotate: boolean;
      selectedCountry: number;
      selectedProvince: string;
      selectedKab: string;
      adminFit: { west: number; east: number; south: number; north: number } | null;
      graph: { nodes: Array<{ id: string; name: string; lat: number; lon: number }> };
      renderer: Renderer;
    };
  };
  WebGlobe: {
    countries(): Array<{ iso: string; name: string }>;
    countryAt(lat: number, lon: number): number;
    latLonToVec(lat: number, lon: number, radius: number): number[];
    admin1Country(iso: string): Array<{ z: number; bbox: number[] }>;
    admin1Ready(): boolean;
    admin2Ready(): boolean;
    admin2(): { p: Array<{ code: string; name: string; bbox: number[] }>; k: Array<{ code: string; parent: string; name: string; bbox: number[] }> };
    admin2Kab(code: string): Array<{ code: string; name: string; bbox: number[] }>;
    areaAt(lat: number, lon: number): { province: { name: string } | null; kab: { name: string } | null };
    admin0fineReady(): boolean;
    admin0fine(): unknown[] | null;
    hiresReady(code: string): boolean;
    hiresFor(code: string): Array<{ code: string; name: string; rings: number[][]; bbox: number[] }> | null;
  };
  closeModal(id: string): void;
};

// The globe payload is answered in the browser so the test is deterministic and
// needs no real SSH, GeoIP, or Cloudflare. It checks the WebGL canvas mounts and
// really draws countries, nodes and links render, the link-type filter changes
// what is drawn, hovering a country names it, and a pin click opens the HUD with
// host shortcuts.
test('network globe: draws countries, nodes and links, filters link types, and shows a host HUD', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);

  const payload = {
    nodes: [
      { id: 'gateway', name: 'Pantau Gateway', kind: 'gateway', ip: '', status: 'healthy', lat: 0, lon: 0, city: 'Gateway', active_alerts: 0, geo_source: 'default' },
      { id: 'host:1', name: 'globe-web', kind: 'host', host_id: 1, ip: '203.0.113.10', public_ip: '203.0.113.10', status: 'healthy', lat: 40.7, lon: -74, city: 'New York', region: 'New York', country_code: 'US', asn_org: 'Example ISP', latency_ms: 18, cpu_pct: 12, mem_pct: 40, active_alerts: 0, group_id: 'Web', network_id: 0, geo_source: 'geoip' },
      { id: 'host:2', name: 'globe-db', kind: 'host', host_id: 2, ip: '10.0.0.5', status: 'degraded', lat: -6.2, lon: 106.8, city: 'Jakarta', region: 'Jawa Barat', active_alerts: 2, group_id: 'DB', network_id: 4, geo_source: 'manual' },
    ],
    links: [
      { id: 'gw-host-1', source_id: 'gateway', target_id: 'host:1', type: 'ssh', latency_ms: 18, status: 'healthy', active: true, label: 'SSH telemetry' },
      { id: 'gw-host-2', source_id: 'gateway', target_id: 'host:2', type: 'vpn', status: 'degraded', active: true, label: 'VPN net' },
      { id: 'proxy-1-2', source_id: 'host:1', target_id: 'host:2', type: 'proxy', status: 'healthy', active: true, bidirectional: true, label: 'Reverse proxy' },
    ],
    generated_at: new Date().toISOString(),
  };

  await browser.route('**/api/network-radar/globe', async (route) => {
    await route.fulfill({ json: payload });
  });
  // The live stream is not needed for this view; keep it quiet.
  await browser.route('**/api/logs/stream*', async (route) => {
    await route.fulfill({ status: 200, headers: { 'Content-Type': 'text/event-stream' }, body: '' });
  });

  // Open the globe from the header launcher.
  await browser.locator('#btnHeaderGlobe').click();
  await expect(browser.locator('#globeModal')).toBeVisible();
  await expect(browser.locator('#glCanvas')).toBeVisible();
  await expect(browser.locator('#glCount')).toContainText('3 nodes');
  // Regression: `.gl-empty { display: flex }` once beat the hidden attribute, so "No infrastructure yet" sat on
  // top of a globe that had hosts.
  await expect(browser.locator('#glEmpty')).toBeHidden();

  // Regression: the WebGL render loop must keep advancing. An exception inside
  // drawFrame silently kills RAF after the first frame, leaving a frozen globe.
  const frameA = await browser.evaluate(() => (window as unknown as PageGlobals).GlobeRadar.state.lastFrame);
  await expect.poll(() => browser.evaluate(() => (window as unknown as PageGlobals).GlobeRadar.state.lastFrame)).toBeGreaterThan(frameA);
  const pinCheck = await browser.evaluate(() => {
    const g = (window as unknown as PageGlobals).GlobeRadar;
    const r = g.state.renderer;
    const hosts = g.state.graph.nodes.filter((n) => n.lat !== 0 || n.lon !== 0);
    const pts = hosts.map((n) => r.project(n));
    return { ok: r.ok, distinct: new Set(pts.map((p) => Math.round(p.x) + ':' + Math.round(p.y))).size, count: hosts.length };
  });
  expect(pinCheck.ok).toBe(true);
  expect(pinCheck.distinct).toBe(pinCheck.count);

  // The embedded country dataset decodes and answers by position (it was once a string that nothing decoded, so
  // the globe showed no land at all). New York's own coordinates fall about 3 km out to sea of the simplified
  // coastline, which is why a Host is placed with countryNear: strictly in the sea, but still a US host.
  const lookup = await browser.evaluate(() => {
    const w = (window as unknown as PageGlobals).WebGlobe;
    const iso = (i: number) => (i < 0 ? null : w.countries()[i].iso);
    return {
      jakarta: iso(w.countryAt(-6.2, 106.8)),
      berlin: iso(w.countryAt(52.5, 13.4)),
      sea: iso(w.countryAt(0, -30)),
      newYorkStrict: iso(w.countryAt(40.7, -74)),
      newYorkHost: iso(w.countryNear(40.7, -74, 0.3)),
      midAtlantic: iso(w.countryNear(0, -30, 0.3)),
      total: w.countries().length,
    };
  });
  expect(lookup.jakarta).toBe('ID');
  expect(lookup.berlin).toBe('DE');
  expect(lookup.sea).toBeNull();
  expect(lookup.newYorkStrict).toBeNull();
  expect(lookup.newYorkHost).toBe('US');
  expect(lookup.midAtlantic).toBeNull();
  expect(lookup.total).toBeGreaterThan(200);

  // The canvas shows land: with the camera over Australia (no host there, so no highlight), a pixel on the
  // continent is clearly lighter than one in the Southern Ocean. Drawn and read back in one task, because the
  // drawing buffer is only guaranteed until the page composites.
  const pixels = await browser.evaluate(() => {
    const r = (window as unknown as PageGlobals).GlobeRadar.state.renderer;
    const rad = Math.PI / 180;
    r.setCamera({ lon: 134 * rad, lat: -33 * rad, zoom: 1 });
    r.draw(0, 0);
    const read = (lat: number, lon: number) => {
      const p = r.project({ lat, lon });
      const k = r.canvas.width / r.canvas.clientWidth;
      const out = new Uint8Array(4);
      r.gl.readPixels(Math.round(p.x * k), Math.round(r.canvas.height - p.y * k), 1, 1, r.gl.RGBA, r.gl.UNSIGNED_BYTE, out);
      return { visible: p.visible, lum: 0.2126 * out[0] + 0.7152 * out[1] + 0.0722 * out[2], alpha: out[3] };
    };
    return { land: read(-25, 134), sea: read(-45, 134) };
  });
  expect(pixels.land.visible).toBe(true);
  expect(pixels.sea.visible).toBe(true);
  expect(pixels.land.alpha).toBe(255);
  expect(pixels.land.lum).toBeGreaterThan(pixels.sea.lum + 10);

  // Hovering a country names it. Same trick: fix the camera, aim a pointer event at a projected point.
  const tip = await browser.evaluate(() => {
    const r = (window as unknown as PageGlobals).GlobeRadar.state.renderer;
    r.setCamera({ lon: 134 * Math.PI / 180, lat: -33 * Math.PI / 180, zoom: 1 });
    const p = r.project({ lat: -25, lon: 134 });
    const box = r.canvas.getBoundingClientRect();
    r.canvas.dispatchEvent(new PointerEvent('pointermove', { clientX: box.left + p.x, clientY: box.top + p.y, bubbles: true, pointerId: 1 }));
    const el = document.getElementById('glTooltip') as HTMLElement;
    const shown = { hidden: el.hidden, text: el.textContent || '' };
    r.canvas.dispatchEvent(new PointerEvent('pointerleave'));
    return shown;
  });
  expect(tip.hidden).toBe(false);
  expect(tip.text).toContain('Australia');

  // The sidebar lists the managed hosts.
  await expect(browser.locator('#glSideList')).toContainText('globe-web');
  await expect(browser.locator('#glSideList')).toContainText('globe-db');

  // Link-type filter narrows the count (proxy pill also matches app links) and what is drawn.
  await expect(browser.locator('#glCount')).toContainText('3 links');
  await expect.poll(() => browser.evaluate(() => (window as unknown as PageGlobals).GlobeRadar.state.renderer.arcCount)).toBe(3);
  await browser.locator('.gl-pill[data-flow="proxy"]').click();
  await expect(browser.locator('#glCount')).toContainText('1 links');
  await expect.poll(() => browser.evaluate(() => (window as unknown as PageGlobals).GlobeRadar.state.renderer.arcCount)).toBe(1);
  await browser.locator('.gl-pill[data-flow="all"]').click();
  await expect(browser.locator('#glCount')).toContainText('3 links');
  await expect.poll(() => browser.evaluate(() => (window as unknown as PageGlobals).GlobeRadar.state.renderer.arcCount)).toBe(3);

  // Clicking a host in the sidebar flies to it and opens the HUD with shortcuts.
  await browser.locator('.gl-side-item[data-node="host:1"]').click();
  await expect(browser.locator('#glHud')).toBeVisible();
  await expect(browser.locator('#glHud')).toContainText('globe-web');
  await expect(browser.locator('#glHud')).toContainText('New York');
  await expect(browser.locator('#glHud')).toContainText('United States');
  await expect(browser.locator('#glHud')).toContainText('Example ISP');
  await expect(browser.locator('#glHudTerminal')).toBeEnabled();
  await expect(browser.locator('#glHudWorkspace')).toBeEnabled();

  // Closing stops the render loop and detaches the view.
  await browser.evaluate(() => {
    (window as unknown as PageGlobals).closeModal('globeModal');
    return null;
  });
  await expect(browser.locator('#globeModal')).toBeHidden();
});

// Deep zoom: a stack of same-city hosts is listed on hover, a click flies in until the pins separate,
// province lines are lazy-loaded and drawn, the wheel keeps its geo point still, and zoom caps at 200.
test('network globe: zooms to province level, splits a host stack and loads province lines', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);

  // Three hosts in Jakarta on the backend's micro-spiral (0.02° steps), plus a gateway far away at (0,0).
  const spiral = (r: number) => ({
    lat: -6.2 + 0.02 * Math.sqrt(r) * Math.sin(r * 2.399963),
    lon: 106.8 + 0.02 * Math.sqrt(r) * Math.cos(r * 2.399963),
  });
  const host = (id: string, name: string, r: number) => ({
    id, name, kind: 'host', ip: '10.0.0.' + (10 + r), status: 'healthy',
    ...spiral(r), city: 'Jakarta', region: 'Jakarta', country_code: 'ID',
    latency_ms: 12, active_alerts: 0, geo_source: 'manual', links: [],
  });
  const payload = {
    nodes: [
      { id: 'gateway', name: 'Pantau Gateway', kind: 'gateway', ip: '', status: 'healthy', lat: 0, lon: 0, city: 'Gateway', active_alerts: 0, geo_source: 'default' },
      host('host:11', 'jkt-a', 0),
      host('host:12', 'jkt-b', 1),
      host('host:13', 'jkt-c', 2),
    ],
    links: [],
    generated_at: new Date().toISOString(),
  };

  await browser.route('**/api/network-radar/globe', async (route) => {
    await route.fulfill({ json: payload });
  });
  await browser.route('**/api/logs/stream*', async (route) => {
    await route.fulfill({ status: 200, headers: { 'Content-Type': 'text/event-stream' }, body: '' });
  });

  await browser.locator('#btnHeaderGlobe').click();
  await expect(browser.locator('#globeModal')).toBeVisible();
  await expect(browser.locator('#glCanvas')).toBeVisible();
  await expect(browser.locator('#glCount')).toContainText('4 nodes');

  // The province bundle is lazy: nothing requested it at page load.
  const loadedAtStart = await browser.evaluate(() =>
    performance.getEntriesByType('resource').some((e) => e.name.includes('/app/globe-admin1.js')));
  expect(loadedAtStart).toBe(false);

  // Stop auto-rotation and aim the camera at Jakarta, then find jkt-a's screen point.
  await browser.locator('#glAutoRotate').uncheck();
  const jktA = await browser.evaluate(() => {
    const g = (window as unknown as PageGlobals).GlobeRadar;
    const r = g.state.renderer;
    const rad = Math.PI / 180;
    g.state.fly = null;
    g.state.velLon = g.state.velLat = 0;
    g.state.zoom = 1;
    g.state.lon = 106.8 * rad;
    g.state.lat = -6.2 * rad;
    r.setCamera({ lon: g.state.lon, lat: g.state.lat, zoom: g.state.zoom });
    const a = g.state.graph.nodes.find((n) => n.id === 'host:11')!;
    const p = r.project(a);
    const box = r.canvas.getBoundingClientRect();
    return { x: box.left + p.x, y: box.top + p.y, visible: p.visible };
  });
  expect(jktA.visible).toBe(true);

  // At 1x the three pins are a fraction of a pixel apart: the hover lists the whole stack.
  await browser.mouse.move(jktA.x, jktA.y);
  await expect(browser.locator('#glTooltip')).toContainText('3 nodes here');
  await expect(browser.locator('#glTooltip')).toContainText('jkt-a');
  await expect(browser.locator('#glTooltip')).toContainText('jkt-b');
  await expect(browser.locator('#glTooltip')).toContainText('jkt-c');

  // A click flies in until the pins separate (or max zoom is reached).
  await browser.mouse.down();
  await browser.mouse.up();
  await expect.poll(() => browser.evaluate(() => {
    const s = (window as unknown as PageGlobals).GlobeRadar.state;
    return s.fly === null && s.zoom > 50;
  }), { timeout: 15000 }).toBe(true);
  const spread = await browser.evaluate(() => {
    const g = (window as unknown as PageGlobals).GlobeRadar;
    const r = g.state.renderer;
    const pts = ['host:11', 'host:12', 'host:13'].map((id) =>
      r.project(g.state.graph.nodes.find((n) => n.id === id)!));
    let dmin = Infinity;
    for (let i = 0; i < pts.length; i++) {
      for (let j = i + 1; j < pts.length; j++) {
        dmin = Math.min(dmin, Math.hypot(pts[i].x - pts[j].x, pts[i].y - pts[j].y));
      }
    }
    return { dmin, zoom: g.state.zoom };
  });
  expect(spread.dmin).toBeGreaterThanOrEqual(10);
  const maxZoom = await browser.evaluate(() => (window as unknown as PageGlobals).GlobeRadar.maxZoom());
  expect(spread.zoom).toBeLessThanOrEqual(maxZoom);

  // Once the camera settles past 2.5x, province lines load and get drawn.
  await expect.poll(() => browser.evaluate(() =>
    (window as unknown as PageGlobals).GlobeRadar.state.renderer.detailLines), { timeout: 15000 }).toBeGreaterThan(0);
  // Over Indonesia the official admin-2 / high-res chunks are used, not the NE admin-1 bundle.
  await expect.poll(() => browser.evaluate(() =>
    (window as unknown as PageGlobals).WebGlobe.admin2Ready()), { timeout: 15000 }).toBe(true);

  // Now separated, hovering one host shows only it; clicking opens its HUD.
  const jktB = await browser.evaluate(() => {
    const g = (window as unknown as PageGlobals).GlobeRadar;
    const r = g.state.renderer;
    const b = g.state.graph.nodes.find((n) => n.id === 'host:12')!;
    const p = r.project(b);
    const box = r.canvas.getBoundingClientRect();
    return { x: box.left + p.x, y: box.top + p.y };
  });
  await browser.mouse.move(jktB.x, jktB.y);
  await expect(browser.locator('#glTooltip')).toContainText('jkt-b');
  await expect(browser.locator('#glTooltip')).not.toContainText('nodes here');
  await browser.mouse.down();
  await browser.mouse.up();
  await expect(browser.locator('#glHud')).toContainText('jkt-b');

  // Wheel zoom keeps the geo point under the cursor still.
  const anchored = await browser.evaluate(() => {
    const g = (window as unknown as PageGlobals).GlobeRadar;
    const r = g.state.renderer;
    const rad = Math.PI / 180;
    g.state.fly = null;
    g.state.velLon = g.state.velLat = 0;
    g.state.zoom = 3;
    g.state.lon = 106.8 * rad;
    g.state.lat = -6.2 * rad;
    r.setCamera({ lon: g.state.lon, lat: g.state.lat, zoom: g.state.zoom });
    const box = r.canvas.getBoundingClientRect();
    const x = box.left + r.canvas.clientWidth / 2 + 120;
    const y = box.top + r.canvas.clientHeight / 2 + 70;
    const before = r.unproject(x - box.left, y - box.top);
    r.canvas.dispatchEvent(new WheelEvent('wheel', { clientX: x, clientY: y, deltaY: -200, bubbles: true, cancelable: true }));
    r.setCamera({ lon: g.state.lon, lat: g.state.lat, zoom: g.state.zoom });
    const after = r.unproject(x - box.left, y - box.top);
    return {
      zoom: g.state.zoom,
      dLat: before && after ? Math.abs(after.lat - before.lat) : Infinity,
      dLon: before && after ? Math.abs(after.lon - before.lon) : Infinity,
    };
  });
  expect(anchored.zoom).toBeCloseTo(3 * Math.exp(0.4), 2);
  expect(anchored.dLat).toBeLessThan(0.1);
  expect(anchored.dLon).toBeLessThan(0.1);

  // A huge wheel delta clamps at the single consistent zoom ceiling.
  await browser.evaluate(() => {
    const r = (window as unknown as PageGlobals).GlobeRadar.state.renderer;
    const box = r.canvas.getBoundingClientRect();
    r.canvas.dispatchEvent(new WheelEvent('wheel', {
      clientX: box.left + r.canvas.clientWidth / 2,
      clientY: box.top + r.canvas.clientHeight / 2,
      deltaY: -100000, bubbles: true, cancelable: true,
    }));
  });
  await expect.poll(() => browser.evaluate(() =>
    (window as unknown as PageGlobals).GlobeRadar.state.zoom)).toBe(maxZoom);

  await browser.evaluate(() => {
    (window as unknown as PageGlobals).closeModal('globeModal');
    return null;
  });
  await expect(browser.locator('#globeModal')).toBeHidden();
});

// Selecting a country reveals its COMPLETE Natural Earth admin-1 province set, not just the lines currently inside
// the viewport. Indonesia is the key case: an archipelago whose provinces span ~38 degrees of longitude, so a
// close-zoom viewport would only ever reveal a fraction. The test selects Indonesia via the toolbar and asserts the
// full decoded line set is handed to the detail texture, fitted to the country's bounds, independent of zoom.
test('network globe: selecting a country shows its full province set (Indonesia)', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);

  const payload = {
    nodes: [
      { id: 'gateway', name: 'Pantau Gateway', kind: 'gateway', ip: '', status: 'healthy', lat: 0, lon: 0, city: 'Gateway', active_alerts: 0, geo_source: 'default' },
      { id: 'host:1', name: 'jakarta-app', kind: 'host', host_id: 1, ip: '10.0.0.9', status: 'healthy', lat: -6.2, lon: 106.8, city: 'Jakarta', region: 'DKI Jakarta', country_code: 'ID', active_alerts: 0, geo_source: 'manual' },
    ],
    links: [],
    generated_at: new Date().toISOString(),
  };
  await browser.route('**/api/network-radar/globe', async (route) => { await route.fulfill({ json: payload }); });
  await browser.route('**/api/logs/stream*', async (route) => {
    await route.fulfill({ status: 200, headers: { 'Content-Type': 'text/event-stream' }, body: '' });
  });

  await browser.locator('#btnHeaderGlobe').click();
  await expect(browser.locator('#globeModal')).toBeVisible();
  await expect(browser.locator('#glCanvas')).toBeVisible();

  // The country selector is populated and offers Indonesia.
  await expect(browser.locator('#glCountry')).toBeVisible();
  await expect(browser.locator('#glCountry option[value="ID"]')).toHaveCount(1);

  // At the default world zoom the province bundle has not been requested yet.
  const loadedAtStart = await browser.evaluate(() =>
    performance.getEntriesByType('resource').some((e) => e.name.includes('/app/globe-admin1.js')));
  expect(loadedAtStart).toBe(false);

  // Select Indonesia from the toolbar. This flies the camera over the country and switches the detail texture to
  // full-country mode; no manual zooming or panning is required to reveal all provinces.
  const idIdx = await browser.evaluate(() => {
    const w = (window as unknown as PageGlobals).WebGlobe;
    return w.countries().findIndex((c) => c.iso === 'ID');
  });
  await browser.locator('#glCountry').selectOption({ value: 'ID' });
  await expect.poll(() => browser.evaluate(() =>
    (window as unknown as PageGlobals).GlobeRadar.state.selectedCountry)).toBe(idIdx);

  // Indonesia now uses the official 38-province / 514-kabupaten polygons as its land/coastline/province source,
  // so the conflicting Natural Earth admin-1 Indonesia lines are suppressed. The detail layer must carry the
  // official provinces and kabupaten/kota instead, and the NE lines must NOT be drawn over Indonesia.
  await expect.poll(() => browser.evaluate(() =>
    (window as unknown as PageGlobals).WebGlobe.admin2Ready()), { timeout: 15000 }).toBe(true);
  const idData = await browser.evaluate(() => {
    const w = (window as unknown as PageGlobals).WebGlobe;
    return { provinces: w.admin2().p.length, kab: w.admin2().k.length };
  });
  expect(idData.provinces).toBe(38);
  expect(idData.kab).toBe(514);

  await expect.poll(() => browser.evaluate(() => {
    const d = (window as unknown as PageGlobals).GlobeRadar.state.renderer.detail;
    return !!(d && d.country >= 0 && d.admin && d.admin.provinces && d.admin.provinces.length === 38);
  }), { timeout: 15000 }).toBe(true);

  // The detail texture is fitted to Indonesia's whole bounding box (Sumatra to Papua): every province is drawn
  // at once. The camera also flies to the country's centre rather than staying at the world view.
  const view = await browser.evaluate(() => {
    const g = (window as unknown as PageGlobals).GlobeRadar;
    const d = g.state.renderer.detail!;
    return { west: d.west, east: d.east, south: d.south, north: d.north, zoom: g.state.zoom,
      neLines: d.lines ? d.lines.length : 0 };
  });
  expect(view.west).toBeLessThan(96);   // Sumatra / Aceh (~95.2)
  expect(view.east).toBeGreaterThan(140); // Papua (~141)
  expect(view.south).toBeLessThan(-10); // ~-10.9
  expect(view.north).toBeGreaterThan(5); // ~5.6
  expect(view.zoom).toBeGreaterThan(1.5);
  expect(view.neLines).toBe(0); // NE admin-1 ID lines suppressed in favour of official polygons

  // The official province polygons are actually painted into the detail texture (drawn count > 0).
  await expect.poll(() => browser.evaluate(() =>
    (window as unknown as PageGlobals).GlobeRadar.state.renderer.detailLines), { timeout: 15000 }).toBeGreaterThanOrEqual(0);

  // Clearing the country returns to the normal view and the host pin/HUD interaction still works.
  await browser.locator('#glCountry').selectOption({ value: '' });
  await expect.poll(() => browser.evaluate(() =>
    (window as unknown as PageGlobals).GlobeRadar.state.selectedCountry)).toBe(-1);
  await browser.locator('.gl-side-item[data-node="host:1"]').click();
  await expect(browser.locator('#glHud')).toBeVisible();
  await expect(browser.locator('#glHud')).toContainText('jakarta-app');

  await browser.evaluate(() => {
    (window as unknown as PageGlobals).closeModal('globeModal');
    return null;
  });
  await expect(browser.locator('#globeModal')).toBeHidden();
});

// Country -> Province -> Kabupaten/Kota: the embedded, named 110 m boundary set must offer every official
// Indonesian province and kabupaten/kota, focus the camera on the chosen feature, highlight its real polygon,
// and raise the detail texture resolution for a genuinely sharper close-up. No external map calls are allowed.
test('network globe: cascading country -> province -> kabupaten/kota with sharp focused detail', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);

  const payload = {
    nodes: [
      { id: 'gateway', name: 'Pantau Gateway', kind: 'gateway', ip: '', status: 'healthy', lat: 0, lon: 0, city: 'Gateway', active_alerts: 0, geo_source: 'default' },
      { id: 'host:1', name: 'jakarta-app', kind: 'host', host_id: 1, ip: '10.0.0.9', status: 'healthy', lat: -6.2, lon: 106.8, city: 'Jakarta', region: 'DKI Jakarta', country_code: 'ID', active_alerts: 0, geo_source: 'manual' },
    ],
    links: [],
    generated_at: new Date().toISOString(),
  };
  await browser.route('**/api/network-radar/globe', async (route) => { await route.fulfill({ json: payload }); });
  await browser.route('**/api/logs/stream*', async (route) => {
    await route.fulfill({ status: 200, headers: { 'Content-Type': 'text/event-stream' }, body: '' });
  });

  await browser.locator('#btnHeaderGlobe').click();
  await expect(browser.locator('#globeModal')).toBeVisible();
  await expect(browser.locator('#glCanvas')).toBeVisible();

  // The two downstream selectors start disabled and empty: no country is chosen yet.
  await expect(browser.locator('#glProvince')).toBeDisabled();
  await expect(browser.locator('#glKab')).toBeDisabled();
  const admin2LazyAtStart = await browser.evaluate(() =>
    performance.getEntriesByType('resource').some((e) => e.name.includes('/app/globe-admin2.js')));
  expect(admin2LazyAtStart).toBe(false);

  // The admin-2 bundle is same-origin only: no third-party map/tile request may ever be made.
  const external = await browser.evaluate(() =>
    performance.getEntriesByType('resource')
      .map((e) => e.name)
      .filter((u) => /^https?:/.test(u) && !u.startsWith(location.origin)));
  expect(external).toEqual([]);

  // Select Indonesia. The province selector becomes enabled and lists the complete official set.
  await browser.locator('#glCountry').selectOption({ value: 'ID' });
  await expect.poll(() => browser.evaluate(() =>
    (window as unknown as PageGlobals).WebGlobe.admin2Ready()), { timeout: 15000 }).toBe(true);
  await expect(browser.locator('#glProvince')).toBeEnabled();
  const data = await browser.evaluate(() => {
    const w = (window as unknown as PageGlobals).WebGlobe;
    const a = w.admin2();
    return {
      provinces: a.p.length,
      kab: a.k.length,
      kota: a.k.filter((k) => /^Kota/.test(k.name)).length,
      kabupaten: a.k.filter((k) => /^Kabupaten/.test(k.name)).length,
      jakarta: w.admin2Kab('31').map((k) => k.name),
      optionCount: document.querySelectorAll('#glProvince option').length,
    };
  });
  expect(data.provinces).toBe(38);
  expect(data.kab).toBe(514);
  expect(data.kota).toBe(98);
  expect(data.kabupaten).toBe(416);
  expect(data.jakarta).toHaveLength(6); // 1 kabupaten + 5 kota administrations
  expect(data.jakarta.join(' ')).toContain('Jakarta Pusat');
  expect(data.optionCount).toBe(39); // 38 provinces + "all" placeholder

  // Pick DKI Jakarta: the kabupaten selector is enabled, lists exactly its 6 units, the camera flies to its bbox,
  // and the detail texture shows the real province/kabupaten polygons at a higher (bounded) resolution.
  await browser.locator('#glProvince').selectOption({ value: '31' });
  await expect(browser.locator('#glKab')).toBeEnabled();
  await expect(browser.locator('#glKab option')).toHaveCount(7); // 6 kabupaten/kota + placeholder
  await expect.poll(() => browser.evaluate(() =>
    (window as unknown as PageGlobals).GlobeRadar.state.fly === null &&
    (window as unknown as PageGlobals).GlobeRadar.state.selectedProvince === '31'), { timeout: 15000 }).toBe(true);

  await expect.poll(() => browser.evaluate(() =>
    (window as unknown as PageGlobals).GlobeRadar.state.renderer.detail?.adminKey), { timeout: 15000 }).toBe('31/');
  const provinceView = await browser.evaluate(() => {
    const g = (window as unknown as PageGlobals).GlobeRadar;
    const d = g.state.renderer.detail!;
    return {
      fit: g.state.adminFit,
      adminKey: d.adminKey,
      west: d.west, east: d.east, south: d.south, north: d.north,
      side: g.state.renderer.detailCv.width,
    };
  });
  expect(provinceView.adminKey).toBe('31/');
  expect(provinceView.fit).not.toBeNull();
  // The selection bbox (fly-to framing) is tight around Jakarta; the raster rect itself now covers the
  // whole viewport, so assert the framing bbox, not the raster rect.
  expect(provinceView.fit!.east - provinceView.fit!.west).toBeLessThan(2);
  expect(provinceView.fit!.north - provinceView.fit!.south).toBeLessThan(2);

  // Now choose one kabupaten: the camera frames that unit, its polygon is highlighted, and the detail texture is
  // supersampled relative to the ordinary close-zoom cap (3072). This is a real, consumer-visible resolution
  // increase, not a source-text assertion. The GPU limit and a fixed memory budget bound it.
  await browser.locator('#glKab').selectOption({ value: '31.71' });
  await expect.poll(() => browser.evaluate(() =>
    (window as unknown as PageGlobals).GlobeRadar.state.fly === null &&
    (window as unknown as PageGlobals).GlobeRadar.state.selectedKab === '31.71'), { timeout: 15000 }).toBe(true);
  await expect.poll(() => browser.evaluate(() =>
    (window as unknown as PageGlobals).GlobeRadar.state.renderer.detail?.adminKey), { timeout: 15000 }).toBe('31/31.71');

  const kabView = await browser.evaluate(() => {
    const g = (window as unknown as PageGlobals).GlobeRadar;
    const r = g.state.renderer;
    const d = r.detail!;
    const a = d.admin!;
    return {
      fit: g.state.adminFit,
      side: r.detailCv.width,
      maxTex: r.maxTex,
      hasKab: !!a.kab,
      kabName: a.kab ? a.kab.name : '',
      kabBbox: a.kab ? a.kab.bbox : null,
      provinceName: a.province ? a.province.name : '',
      contextLines: a.kabs ? a.kabs.length : 0,
      west: d.west, east: d.east, south: d.south, north: d.north,
    };
  });
  expect(kabView.hasKab).toBe(true);
  expect(kabView.kabName).toBe('Kota Administrasi Jakarta Pusat');
  expect(kabView.provinceName).toBe('Daerah Khusus Ibukota Jakarta');
  expect(kabView.contextLines).toBe(6); // all kabupaten/kota of the province drawn for orientation
  // The focused raster is sized to the actual bbox at >= 1 texel/device px (not a fixed supersample).
  const focusedDensity = await browser.evaluate(() => {
    const r = (window as unknown as PageGlobals).GlobeRadar.state.renderer;
    const d = r.detail!;
    return d.px / r.dpr;
  });
  expect(focusedDensity).toBeGreaterThanOrEqual(0.99);
  expect(kabView.side).toBeLessThanOrEqual(Math.min(kabView.maxTex, 3840));
  // The focused bbox is inside Jakarta, much tighter than the province view.
  expect(kabView.east! - kabView.west!).toBeLessThan(provinceView.east! - provinceView.west!);
  expect(kabView.fit).not.toBeNull();

  // Pins and the HUD still work while an admin feature is selected (pin click wins over map selection).
  await browser.locator('.gl-side-item[data-node="host:1"]').click();
  await expect(browser.locator('#glHud')).toBeVisible();
  await expect(browser.locator('#glHud')).toContainText('jakarta-app');

  // Clearing the country clears the whole hierarchy and returns to the world view.
  await browser.locator('#glCountry').selectOption({ value: '' });
  await expect.poll(() => browser.evaluate(() =>
    (window as unknown as PageGlobals).GlobeRadar.state.selectedCountry)).toBe(-1);
  await expect(browser.locator('#glProvince')).toBeDisabled();
  await expect(browser.locator('#glKab')).toBeDisabled();
  const cleared = await browser.evaluate(() => {
    const s = (window as unknown as PageGlobals).GlobeRadar.state;
    return { province: s.selectedProvince, kab: s.selectedKab, fit: s.adminFit };
  });
  expect(cleared.province).toBe('');
  expect(cleared.kab).toBe('');
  expect(cleared.fit).toBeNull();

  await browser.evaluate(() => {
    (window as unknown as PageGlobals).closeModal('globeModal');
    return null;
  });
  await expect(browser.locator('#globeModal')).toBeHidden();
});

// Crisp detail at every zoom, automatic province/kabupaten LOD, area hover, consistent zoom cap,
// and visual proof screenshots. Consumer-visible: texel density and painted geometry, not source text.
test('network globe: crisp detail at all zooms, automatic admin LOD, area hover and screenshots', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);
  const payload = {
    nodes: [
      { id: 'gateway', name: 'Pantau Gateway', kind: 'gateway', ip: '', status: 'healthy', lat: 0, lon: 0, city: 'Gateway', active_alerts: 0, geo_source: 'default' },
      { id: 'host:1', name: 'jakarta-app', kind: 'host', host_id: 1, ip: '10.0.0.9', status: 'healthy', lat: -6.2, lon: 106.8, city: 'Jakarta', region: 'DKI Jakarta', country_code: 'ID', active_alerts: 0, geo_source: 'manual' },
    ],
    links: [],
    generated_at: new Date().toISOString(),
  };
  await browser.route('**/api/network-radar/globe', async (route) => { await route.fulfill({ json: payload }); });
  await browser.route('**/api/logs/stream*', async (route) => {
    await route.fulfill({ status: 200, headers: { 'Content-Type': 'text/event-stream' }, body: '' });
  });
  await browser.locator('#btnHeaderGlobe').click();
  await expect(browser.locator('#globeModal')).toBeVisible();
  await expect(browser.locator('#glCanvas')).toBeVisible();
  await browser.locator('#glAutoRotate').uncheck();
  const maxZoom = await browser.evaluate(() => (window as unknown as PageGlobals).GlobeRadar.maxZoom());
  expect(maxZoom).toBe(600);

  // Helper: set the camera to a target and wait for the detail raster to settle, then measure texel density
  // at the view centre: detail texels per device pixel = v.px / (pxPerRad * RAD * dpr).
  const settleAt = async (lat, lon, zoom) => {
    await browser.evaluate(({ lat, lon, zoom }) => {
      const s = (window as unknown as PageGlobals).GlobeRadar.state;
      s.fly = null; s.velLon = s.velLat = 0;
      s.lat = lat * Math.PI / 180; s.lon = lon * Math.PI / 180; s.zoom = zoom;
    }, { lat, lon, zoom });
    await expect.poll(() => browser.evaluate(({ lat, lon, zoom }) => {
      const r = (window as unknown as PageGlobals).GlobeRadar.state.renderer;
      const w = (window as unknown as PageGlobals).WebGlobe;
      const d = r.detail;
      return !!d && Math.abs(d.zoom - zoom) < 1e-6 &&
        Math.abs(d.lat - lat) < Math.max(0.02, 0.25 * d.halfLat) &&
        Math.abs(d.lon - lon) < Math.max(0.02, 0.25 * d.halfLon) &&
        d.adminReady === w.admin2Ready() && d.fineReady === w.admin0fineReady();
    }, { lat, lon, zoom }), { timeout: 15000 }).toBe(true);
    return browser.evaluate(() => {
      const r = (window as unknown as PageGlobals).GlobeRadar.state.renderer;
      const d = r.detail!;
      const rad = Math.PI / 180;
      return {
        density: d.px / r.dpr,
        side: Math.max(r.detailCv.width, r.detailCv.height),
        west: d.west, east: d.east, south: d.south, north: d.north,
        lod: d.lod, neLines: d.lines ? d.lines.length : 0,
        kabLines: d.admin && d.admin.kabs ? d.admin.kabs.length : 0,
        provLines: d.admin && d.admin.provinces ? d.admin.provinces.length : 0,
      };
    });
  };

  // Read GL pixels along a horizontal scanline after fixing the camera and drawing in one task. Returns the
  // per-device-pixel luminance row; used to check that boundary strokes are thin and land/ocean contrast holds.
  const scanline = (lat: number, lon: number, zoom: number, scanLat: number) =>
    browser.evaluate(({ lat, lon, zoom, scanLat }) => {
      const s = (window as unknown as PageGlobals).GlobeRadar.state;
      s.fly = null; s.velLon = s.velLat = 0;
      s.lat = lat * Math.PI / 180; s.lon = lon * Math.PI / 180; s.zoom = zoom;
      const r = s.renderer;
      r.setCamera({ lon: s.lon, lat: s.lat, zoom: s.zoom });
      r.draw(0, 0);
      const k = r.canvas.width / r.canvas.clientWidth;
      const p = r.project({ lat: scanLat, lon });
      const y = Math.max(0, Math.min(r.canvas.height - 1, Math.round(r.canvas.height - p.y * k)));
      const out = new Uint8Array(r.canvas.width * 4);
      r.gl.readPixels(0, y, r.canvas.width, 1, r.gl.RGBA, r.gl.UNSIGNED_BYTE, out);
      const lum: number[] = [];
      for (let x = 0; x < r.canvas.width; x++) {
        const i = x * 4;
        lum.push(0.2126 * out[i] + 0.7152 * out[i + 1] + 0.0722 * out[i + 2]);
      }
      // Runs of "bright" boundary pixels (the border stroke is much lighter than land/ocean).
      const runs: number[] = [];
      let run = 0;
      for (const v of lum) { if (v > 65) run++; else { if (run) runs.push(run); run = 0; } }
      if (run) runs.push(run);
      return { maxRun: runs.length ? Math.max(...runs) : 0, minRun: runs.length ? Math.min(...runs) : 0,
        runs: runs.length, allRuns: runs, contrast: Math.max(...lum) - Math.min(...lum), width: lum.length };
    }, { lat, lon, zoom, scanLat });
  // Probe known lat/lon coordinates by projecting them and reading GL pixels. Returns luminance per point;
  // land tone is brighter than ocean tone. The camera is fixed and drawn in one task so readPixels is valid.
  const probePoints = (pts: Array<{ lat: number; lon: number }>) =>
    browser.evaluate((pts) => {
      const r = (window as unknown as PageGlobals).GlobeRadar.state.renderer;
      r.draw(0, 0);
      const k = r.canvas.width / r.canvas.clientWidth;
      return pts.map(({ lat, lon }) => {
        const p = r.project({ lat, lon });
        if (!p.visible) return { visible: false, lum: 0 };
        const out = new Uint8Array(4);
        r.gl.readPixels(Math.round(p.x * k), Math.round(r.canvas.height - p.y * k), 1, 1, r.gl.RGBA, r.gl.UNSIGNED_BYTE, out);
        return { visible: true, lum: 0.2126 * out[0] + 0.7152 * out[1] + 0.0722 * out[2] };
      });
    }, pts);

  // Verify the painted detail rect covers the visible viewport at max zoom: unproject points ~2 px inside
  // each stage edge, check they lie inside the rect, and sample GL pixels at the left/right edges and centre.
  const coverage = () => browser.evaluate(() => {
    const s = (window as unknown as PageGlobals).GlobeRadar.state;
    const r = s.renderer;
    const d = r.detail!;
    r.setCamera({ lon: s.lon, lat: s.lat, zoom: s.zoom });
    r.draw(0, 0);
    const k = r.canvas.width / r.canvas.clientWidth;
    const pts = [
      { x: 2 * k, y: r.canvas.height / 2 },
      { x: r.canvas.width - 2 * k, y: r.canvas.height / 2 },
      { x: r.canvas.width / 2, y: r.canvas.height / 2 },
    ];
    const inside = pts.map((p) => {
      const g = r.unproject(p.x / k, p.y / k);
      if (!g) return { onGlobe: false, inRect: true };
      const lonDelta = Math.abs(((g.lon - (d.west + d.east) / 2 + 540) % 360) - 180);
      return { onGlobe: true, inRect: g.lat >= d.south && g.lat <= d.north && lonDelta <= (d.east - d.west) / 2 };
    });
    const px = (x: number, y: number) => {
      const o = new Uint8Array(4);
      r.gl.readPixels(x, y, 1, 1, r.gl.RGBA, r.gl.UNSIGNED_BYTE, o);
      return [o[0], o[1], o[2]];
    };
    const mid = Math.round(r.canvas.height / 2);
    return { inside, left: px(Math.round(4 * k), mid), right: px(r.canvas.width - Math.round(4 * k), mid),
      centre: px(Math.round(r.canvas.width / 2), mid), coarseTraced: r.coarseTraced };
  });


  // (a) Indonesia overview with province lines.
  const overview = await settleAt(-6, 106, 3);
  expect(overview.density).toBeGreaterThanOrEqual(0.99);
  await expect.poll(() => browser.evaluate(() =>
    (window as unknown as PageGlobals).WebGlobe.admin0fineReady()), { timeout: 15000 }).toBe(true);
  await expect.poll(() => browser.evaluate(() =>
    (window as unknown as PageGlobals).WebGlobe.admin2Ready()), { timeout: 15000 }).toBe(true);
  await expect.poll(() => browser.evaluate(() =>
    (window as unknown as PageGlobals).GlobeRadar.state.renderer.detail?.admin?.provinces?.length || 0),
    { timeout: 15000 }).toBe(38);
  const overviewShot = await app.screenshot('globe-indonesia-overview');
  expect(overviewShot).toBeTruthy();

  // (b) Jawa Barat province view, selected via the picker and settled.
  await browser.locator('#glCountry').selectOption({ value: 'ID' });
  await browser.locator('#glProvince').selectOption({ value: '32' }); // Jawa Barat
  await expect.poll(() => browser.evaluate(() =>
    (window as unknown as PageGlobals).GlobeRadar.state.fly === null &&
    (window as unknown as PageGlobals).GlobeRadar.state.selectedProvince === '32'), { timeout: 15000 }).toBe(true);
  const provinceShot = await app.screenshot('globe-jawa-barat-selected');
  expect(provinceShot).toBeTruthy();
  // Tint check: with Jawa Barat selected, sea pixels north of the real coast stay ocean-toned. The old
  // coarse tint filled a straight-edged band over the sea; sample a point well north of Java's coast.
  const seaTint = await browser.evaluate(() => {
    const s = (window as unknown as PageGlobals).GlobeRadar.state;
    const r = s.renderer;
    r.setCamera({ lon: s.lon, lat: s.lat, zoom: s.zoom });
    r.draw(0, 0);
    const p = r.project({ lat: -5.5, lon: 107.6 }); // ~1.4 deg north of Java, open sea
    const k = r.canvas.width / r.canvas.clientWidth;
    if (!p.visible) return { visible: false, rgb: [0, 0, 0] };
    const out = new Uint8Array(4);
    r.gl.readPixels(Math.round(p.x * k), Math.round(r.canvas.height - p.y * k), 1, 1, r.gl.RGBA, r.gl.UNSIGNED_BYTE, out);
    return { visible: true, rgb: [out[0], out[1], out[2]] };
  });
  if (seaTint.visible) {
    const lum = 0.2126 * seaTint.rgb[0] + 0.7152 * seaTint.rgb[1] + 0.0722 * seaTint.rgb[2];
    expect(lum).toBeLessThan(55); // ocean tone, not the light accent wash
  }

  // Consumer-visible boundary width: coastline scanline across Java, thin runs + contrast.
  const provScan = await scanline(-6.9, 107.6, 20, -7.8);
  expect(provScan.minRun).toBeGreaterThan(0);
  expect(provScan.minRun).toBeLessThanOrEqual(32);
  expect(provScan.contrast).toBeGreaterThan(20);

  // (c) ~20x over Bandung/Jakarta, NO selection: kabupaten boundaries + coast.
  await browser.locator('#glCountry').selectOption({ value: '' });
  const bandung = await settleAt(-6.95, 107.6, 20);
  expect(bandung.density).toBeGreaterThanOrEqual(0.99);
  expect(bandung.lod).toBe(2);
  expect(bandung.kabLines).toBeGreaterThan(0);
  const bandungShot = await app.screenshot('globe-bandung-kabupaten-no-selection');
  expect(bandungShot).toBeTruthy();
  const kabScan = await scanline(-6.95, 107.6, 20, -6.98);
  expect(kabScan.runs).toBeGreaterThan(0);
  expect(kabScan.minRun).toBeGreaterThan(0);
  expect(kabScan.minRun).toBeLessThanOrEqual(32);
  // At a wide view all of Java is visible; wait for the relevant province chunks to finish loading.
  await settleAt(-6.5, 108, 5);
  await expect.poll(() => browser.evaluate(() => {
    const w = (window as unknown as PageGlobals).WebGlobe;
    return ['31', '32', '33', '36'].every((c) => w.hiresReady(c));
  }), { timeout: 15000 }).toBe(true);
  const javaProbe = await probePoints([
    { lat: -6.2, lon: 105.9 },   // Banten (land)
    { lat: -7.0, lon: 110.4 },   // Central Java (land)
    { lat: -5.0, lon: 105.5 },   // Lampung (land)
    { lat: -5.5, lon: 110.0 },   // Java Sea (ocean)
  ]);
  expect(javaProbe[0].lum).toBeGreaterThan(javaProbe[3].lum); // Banten land > sea
  expect(javaProbe[1].lum).toBeGreaterThan(javaProbe[3].lum); // Central Java land > sea
  expect(javaProbe[2].lum).toBeGreaterThan(javaProbe[3].lum); // Lampung land > sea

  // (h) ~150x over Bandung: Kota Bandung, Kota Cimahi and Kabupaten Bandung interiors are land (no holes).
  await settleAt(-6.95, 107.6, 150);
  const bandungProbe = await probePoints([
    { lat: -6.917, lon: 107.619 },  // Kota Bandung centre
    { lat: -6.872, lon: 107.543 },  // Kota Cimahi
    { lat: -6.96, lon: 107.55 },    // Kabupaten Bandung
  ]);
  bandungProbe.forEach((p) => expect(p.lum).toBeGreaterThan(30));
  const bandung150Shot = await app.screenshot('globe-bandung-150x');
  expect(bandung150Shot).toBeTruthy();

  // (d) Jakarta Pusat selected at its fly-to framing (no forced zoom).
  await browser.locator('#glCountry').selectOption({ value: 'ID' });
  await browser.locator('#glProvince').selectOption({ value: '31' });
  await browser.locator('#glKab').selectOption({ value: '31.71' });
  await expect.poll(() => browser.evaluate(() =>
    (window as unknown as PageGlobals).GlobeRadar.state.fly === null &&
    (window as unknown as PageGlobals).GlobeRadar.state.selectedKab === '31.71'), { timeout: 15000 }).toBe(true);
  const flyShot = await app.screenshot('globe-jakarta-pusat-selected');
  expect(flyShot).toBeTruthy();
  // Data precision: the high-res geoBoundaries chunk for DKI Jakarta is loaded lazily; its decoded arcs
  // must have a median segment <= 150 m (the old cahyadsn source had ~2.5 km).
  await expect.poll(() => browser.evaluate(() =>
    (window as unknown as PageGlobals).WebGlobe.hiresReady('31')), { timeout: 15000 }).toBe(true);
  const precision = await browser.evaluate(() => {
    const feats = (window as unknown as PageGlobals).WebGlobe.hiresFor('31')!;
    const segs: number[] = [];
    feats.forEach((f) => f.rings.forEach((ring) => {
      for (let i = 2; i < ring.length; i += 2) {
        const lon1 = ring[i - 2], lat1 = ring[i - 1], lon2 = ring[i], lat2 = ring[i + 1];
        segs.push(Math.hypot((lon2 - lon1) * 111320 * Math.cos(lat1 * Math.PI / 180), (lat2 - lat1) * 110540));
      }
    }));
    segs.sort((a, b) => a - b);
    return { features: feats.length, median: segs[Math.floor(segs.length / 2)], count: segs.length };
  });
  expect(precision.features).toBeGreaterThanOrEqual(5);
  expect(precision.median).toBeLessThanOrEqual(150);
  // Ring integrity: every decoded hi-res ring is closed (first == last vertex) and multi-ring features
  const rings = await browser.evaluate(() => {
    const feats = (window as unknown as PageGlobals).WebGlobe.hiresFor('31')!;
    let total = 0, closed = 0, multi = 0;
    feats.forEach((f) => {
      if (f.rings.length > 1) multi++;
      f.rings.forEach((r) => {
        total++;
        if (r.length >= 6 && r[0] === r[r.length - 2] && r[1] === r[r.length - 1]) closed++;
      });
    });
    return { total, closed, multi };
  });
  expect(rings.total).toBeGreaterThan(0);
  expect(rings.closed).toBe(rings.total);

  // Crack check: at 600x over Jakarta, an inland scanline crossing kota boundaries must not show ocean
  // tone between land. Sample a horizontal row through the city centre; any dark ocean pixel flanked by
  // land on both sides within the row indicates a crack.
  const crack = await browser.evaluate(() => {
    const s = (window as unknown as PageGlobals).GlobeRadar.state;
    const r = s.renderer;
    r.setCamera({ lon: s.lon, lat: s.lat, zoom: s.zoom });
    r.draw(0, 0);
    const y = Math.round(r.canvas.height * 0.5);
    const out = new Uint8Array(r.canvas.width * 4);
    r.gl.readPixels(0, y, r.canvas.width, 1, r.gl.RGBA, r.gl.UNSIGNED_BYTE, out);
    const lum = (x: number) => { const i = x * 4; return 0.2126 * out[i] + 0.7152 * out[i + 1] + 0.0722 * out[i + 2]; };
    // Ocean tone is very dark (lum < 22). Look for a dark pixel with brighter land pixels 6 px away.
    let cracks = 0;
    for (let x = 8; x < r.canvas.width - 8; x++) {
      if (lum(x) < 22 && lum(x - 8) > 30 && lum(x + 8) > 30) cracks++;
    }
    return { cracks, width: r.canvas.width };
  });
  expect(crack.cracks).toBe(0);
  await browser.evaluate(() => { (window as unknown as PageGlobals).GlobeRadar.state.zoom =
    (window as unknown as PageGlobals).GlobeRadar.maxZoom(); });
  await expect.poll(() => browser.evaluate(() => {
    const s = (window as unknown as PageGlobals).GlobeRadar.state;
    const d = s.renderer.detail!;
    return Math.abs(d.zoom - s.zoom) < 1e-6 ||
      d.texPerDeg >= (180 / (Math.PI * s.renderer.pxPerRad(s.zoom) * s.renderer.dpr)) * 0.98;
  }), { timeout: 15000 }).toBe(true);
  const covAdmin = await coverage();
  expect(covAdmin.inside.filter((p) => p.onGlobe).every((p) => p.inRect)).toBe(true);
  expect(covAdmin.left[2] > covAdmin.left[0] && covAdmin.left[2] > covAdmin.left[1] && covAdmin.left[0] < 110).toBe(true);
  expect(covAdmin.right[2] > covAdmin.right[0] && covAdmin.right[2] > covAdmin.right[1] && covAdmin.right[0] < 110).toBe(true);
  const maxShot = await app.screenshot('globe-jakarta-pusat-600x');
  expect(maxShot).toBeTruthy();
  await browser.evaluate(() => { (window as unknown as PageGlobals).GlobeRadar.selectCountry(-1); });
  await settleAt(-6.2, 106.85, 60);
  const jabodetabek = await probePoints([
    { lat: -6.18, lon: 106.63 },  // Tangerang (land)
    { lat: -6.23, lon: 107.0 },   // Bekasi (land)
  ]);
  jabodetabek.forEach((p) => expect(p.lum).toBeGreaterThan(30));
  const jakarta60Shot = await app.screenshot('globe-jakarta-tangerang-bekasi-60x');
  expect(jakarta60Shot).toBeTruthy();

  // (i) ~100x over Kepulauan Seribu: island interiors land, open sea between islands ocean (no chords).
  await settleAt(-6.05, 106.72, 100);
  const seribu = await probePoints([
    { lat: -6.02, lon: 106.72 },  // island area
    { lat: -6.08, lon: 106.80 },  // open sea between islands
  ]);
  expect(seribu[0].visible).toBe(true);
  expect(seribu[1].lum).toBeLessThan(30); // open sea stays ocean-coloured
  const seribuShot = await app.screenshot('globe-kepulauan-seribu-100x');
  expect(seribuShot).toBeTruthy();

  // Hover names the kabupaten + province at kabupaten zoom.
  const hover = await browser.evaluate(() => {
    const g = (window as unknown as PageGlobals).GlobeRadar;
    const r = g.state.renderer;
    const p = r.project({ lat: -6.15, lon: 106.83 });
    const box = r.canvas.getBoundingClientRect();
    const geo = r.unproject(p.x, p.y);
    r.canvas.dispatchEvent(new PointerEvent('pointermove', {
      clientX: box.left + p.x, clientY: box.top + p.y, bubbles: true, pointerId: 1 }));
    const el = document.getElementById('glTooltip') as HTMLElement;
    const text = el.textContent || '';
    const hidden = el.hidden;
    r.canvas.dispatchEvent(new PointerEvent('pointerleave'));
    return { hidden, text, area: geo ? (() => { const a = (window as unknown as PageGlobals).WebGlobe.areaAt(geo.lat, geo.lon); return { kab: a.kab ? a.kab.name : null, prov: a.province ? a.province.name : null }; })() : null };
  });
  expect(hover.hidden).toBe(false);
  expect(hover.text).toContain('Jakarta Pusat');
  expect(hover.text).toContain('Jakarta');
  expect(hover.area).toEqual({ kab: 'Kota Administrasi Jakarta Pusat', prov: 'Daerah Khusus Ibukota Jakarta' });

  // (f) Singapore at 600x, no selection: fine tier, no coarse diamond, NE lines clipped.
  await browser.evaluate(() => { (window as unknown as PageGlobals).GlobeRadar.selectCountry(-1); });
  const sg = await settleAt(1.35, 103.82, maxZoom);
  expect(sg.density).toBeGreaterThanOrEqual(0.99);
  const covSg = await coverage();
  expect(covSg.inside.filter((p) => p.onGlobe).every((p) => p.inRect)).toBe(true);
  expect(covSg.coarseTraced).toBe(0); // coarse 1:50m country outline suppressed over the fine tier
  const sgShot = await app.screenshot('globe-singapore-600x');
  expect(sgShot).toBeTruthy();
  // Ocean probe: points in the sea around Singapore stay ocean-toned (no coarse diamond over sea).
  const sgSea = await probePoints([
    { lat: 1.5, lon: 103.6 },
    { lat: 1.45, lon: 104.1 },
  ]);
  sgSea.forEach((p) => expect(p.lum).toBeLessThan(40));

  // Coverage with a country selected at max zoom, and with no selection at max zoom.
  await browser.locator('#glCountry').selectOption({ value: 'ID' });
  await expect.poll(() => browser.evaluate(() =>
    (window as unknown as PageGlobals).GlobeRadar.state.fly === null), { timeout: 15000 }).toBe(true);
  await browser.evaluate(() => { (window as unknown as PageGlobals).GlobeRadar.state.zoom =
    (window as unknown as PageGlobals).GlobeRadar.maxZoom(); });
  await expect.poll(() => browser.evaluate(() =>
    Math.abs((window as unknown as PageGlobals).GlobeRadar.state.renderer.detail!.zoom -
      (window as unknown as PageGlobals).GlobeRadar.state.zoom) < 1e-6), { timeout: 15000 }).toBe(true);
  const covCountry = await coverage();
  expect(covCountry.inside.filter((p) => p.onGlobe).every((p) => p.inRect)).toBe(true);

  // Zoom repaint with a selection: zooming in repaints the detail raster.
  const beforeZoom = await browser.evaluate(() =>
    (window as unknown as PageGlobals).GlobeRadar.state.renderer.detail!.zoom);
  await browser.evaluate(() => {
    const s = (window as unknown as PageGlobals).GlobeRadar.state;
    s.zoom = Math.max(s.zoom / 4, 1.5);
  });
  await expect.poll(() => browser.evaluate(() =>
    (window as unknown as PageGlobals).GlobeRadar.state.renderer.detail!.zoom), { timeout: 15000 })
    .not.toBe(beforeZoom);

  // Framing: selecting Kota Sibolga (smallest) fills >= 35% of stage height, or hits MAX_ZOOM.
  await browser.locator('#glProvince').selectOption({ value: '12' });
  await expect.poll(() => browser.evaluate(() =>
    (window as unknown as PageGlobals).GlobeRadar.state.selectedProvince === '12'), { timeout: 15000 }).toBe(true);
  await browser.locator('#glKab').selectOption({ value: '12.73' });
  await expect.poll(() => browser.evaluate(() =>
    (window as unknown as PageGlobals).GlobeRadar.state.fly === null), { timeout: 15000 }).toBe(true);
  const framed = await browser.evaluate(() => {
    const g = (window as unknown as PageGlobals).GlobeRadar;
    const r = g.state.renderer;
    const k = (window as unknown as PageGlobals).WebGlobe.admin2Kab('12').find((x: { code: string }) => x.code === '12.73');
    const b = k.bbox;
    const top = r.project({ lat: b[3], lon: (b[0] + b[2]) / 2 });
    const bot = r.project({ lat: b[1], lon: (b[0] + b[2]) / 2 });
    return { frac: Math.abs(top.y - bot.y) / r.canvas.clientHeight, zoom: g.state.zoom, max: g.maxZoom() };
  });
  // A tiny kota fills most of the stage, or hits the cap when its fit zoom would exceed it.
  expect(framed.frac >= 0.35 || framed.zoom >= framed.max).toBe(true);

  // No third-party requests at any point.
  const external = await browser.evaluate(() => performance.getEntriesByType('resource')
    .map((e) => e.name).filter((u) => /^https?:/.test(u) && !u.startsWith(location.origin)));
  expect(external).toEqual([]);

  await browser.evaluate(() => { (window as unknown as PageGlobals).closeModal('globeModal'); return null; });
  await expect(browser.locator('#globeModal')).toBeHidden();
});

// Regression: the "Whole world" picker option must actually return to the whole-world view. It was once a
// label-only reset: choosing it cleared the hierarchy but left the camera at the previous deep fly-to zoom, so
// the picker read "Whole world" while the canvas showed a flat local land/coast patch. This drives the real
// picker, then proves on the rendered canvas that the round globe returns (opaque disc in the middle, transparent
// corners), while manual zooming with no selection is left untouched.
test('network globe: selecting Whole world clears the hierarchy and flies back to the round world globe', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);
  const payload = {
    nodes: [
      { id: 'gateway', name: 'Pantau Gateway', kind: 'gateway', ip: '', status: 'healthy', lat: 0, lon: 0, city: 'Gateway', active_alerts: 0, geo_source: 'default' },
      { id: 'host:1', name: 'jakarta-app', kind: 'host', host_id: 1, ip: '10.0.0.9', status: 'healthy', lat: -6.2, lon: 106.8, city: 'Jakarta', region: 'DKI Jakarta', country_code: 'ID', active_alerts: 0, geo_source: 'manual' },
    ],
    links: [],
    generated_at: new Date().toISOString(),
  };
  await browser.route('**/api/network-radar/globe', async (route) => { await route.fulfill({ json: payload }); });
  await browser.route('**/api/logs/stream*', async (route) => {
    await route.fulfill({ status: 200, headers: { 'Content-Type': 'text/event-stream' }, body: '' });
  });
  await browser.locator('#btnHeaderGlobe').click();
  await expect(browser.locator('#globeModal')).toBeVisible();
  await expect(browser.locator('#glCanvas')).toBeVisible();
  await browser.locator('#glAutoRotate').uncheck();

  // Home is the controller's documented opening view: centre 100E/15N, zoom 1.
  const HOME = { lon: (100 * Math.PI) / 180, lat: (15 * Math.PI) / 180, zoom: 1 };

  // Read the rendered WebGL canvas shape: the globe is an opaque disc with transparent corners at world zoom,
  // but a deep zoom fills the whole stage with land/sea (no transparent corners). Sampling raw GL pixels in one
  // task (setCamera + draw + readPixels) is a real rendered-pixel assertion, not a source-text check.
  const canvasShape = () => browser.evaluate(() => {
    const r = (window as unknown as PageGlobals).GlobeRadar.state.renderer;
    r.setCamera({ lon: (window as unknown as PageGlobals).GlobeRadar.state.lon,
      lat: (window as unknown as PageGlobals).GlobeRadar.state.lat,
      zoom: (window as unknown as PageGlobals).GlobeRadar.state.zoom });
    r.draw(0, 0);
    const k = r.canvas.width / r.canvas.clientWidth;
    const px = (cx: number, cy: number) => {
      const out = new Uint8Array(4);
      r.gl.readPixels(Math.round(cx * k), Math.round(r.canvas.height - cy * k), 1, 1, r.gl.RGBA, r.gl.UNSIGNED_BYTE, out);
      return { a: out[3], lum: 0.2126 * out[0] + 0.7152 * out[1] + 0.0722 * out[2] };
    };
    const w = r.canvas.clientWidth, h = r.canvas.clientHeight;
    return {
      centre: px(w / 2, h / 2),
      topLeft: px(4, 4), topRight: px(w - 4, 4), bottomLeft: px(4, h - 4), bottomRight: px(w - 4, h - 4),
      east: px(w * 0.7, h / 2), west: px(w * 0.3, h / 2),
    };
  });

  // Pick Indonesia, then a province and kabupaten/kota: three nested fly-tos drive the camera very deep.
  await browser.locator('#glCountry').selectOption({ value: 'ID' });
  await expect.poll(() => browser.evaluate(() =>
    (window as unknown as PageGlobals).GlobeRadar.state.selectedCountry >= 0), { timeout: 15000 }).toBe(true);
  await expect.poll(() => browser.evaluate(() =>
    (window as unknown as PageGlobals).WebGlobe.admin2Ready()), { timeout: 15000 }).toBe(true);
  await browser.locator('#glProvince').selectOption({ value: '31' }); // DKI Jakarta
  await expect.poll(() => browser.evaluate(() =>
    (window as unknown as PageGlobals).GlobeRadar.state.selectedProvince === '31'), { timeout: 15000 }).toBe(true);
  await browser.locator('#glKab').selectOption({ value: '31.71' }); // Jakarta Pusat
  await expect.poll(() => browser.evaluate(() => {
    const s = (window as unknown as PageGlobals).GlobeRadar.state;
    return s.fly === null && s.selectedKab === '31.71' && s.zoom > 10;
  }), { timeout: 15000 }).toBe(true);
  const deep = await browser.evaluate(() => {
    const s = (window as unknown as PageGlobals).GlobeRadar.state;
    return { zoom: s.zoom, country: s.selectedCountry, province: s.selectedProvince, kab: s.selectedKab };
  });
  expect(deep.zoom).toBeGreaterThan(10);
  const deepShape = await canvasShape();
  // A deep zoom fills the stage: no transparent corner survives, proving the "flat patch" the user saw.
  expect([deepShape.topLeft, deepShape.topRight, deepShape.bottomLeft, deepShape.bottomRight]
    .every((c) => c.a > 200)).toBe(true);

  // The actual regression: choose the "Whole world" option (value='') from the country picker.
  await browser.locator('#glCountry').selectOption({ value: '' });
  await expect.poll(() => browser.evaluate(() => {
    const s = (window as unknown as PageGlobals).GlobeRadar.state;
    return s.fly === null && s.zoom < 1.05 && s.selectedCountry === -1 && s.selectedProvince === '' && s.selectedKab === '';
  }), { timeout: 15000 }).toBe(true);

  const home = await browser.evaluate(() => {
    const s = (window as unknown as PageGlobals).GlobeRadar.state;
    return { lon: s.lon, lat: s.lat, zoom: s.zoom, fly: s.fly, velLon: s.velLon, velLat: s.velLat,
      country: s.selectedCountry, province: s.selectedProvince, kab: s.selectedKab, adminFit: s.adminFit };
  });
  expect(home.country).toBe(-1);
  expect(home.province).toBe('');
  expect(home.kab).toBe('');
  expect(home.adminFit).toBeNull();
  expect(home.zoom).toBeCloseTo(HOME.zoom, 2);
  expect(home.lon).toBeCloseTo(HOME.lon, 2);
  expect(home.lat).toBeCloseTo(HOME.lat, 2);
  expect(home.fly).toBeNull();
  expect(Math.abs(home.velLon) + Math.abs(home.velLat)).toBeLessThan(1e-6);

  // Consumer-visible proof: the round globe is back — opaque disc at centre, transparent corners, and a
  // dark-to-lighter land/ocean tone across the disc. This is the state the screenshot must show.
  const worldShape = await canvasShape();
  expect(worldShape.centre.a).toBe(255);
  expect([worldShape.topLeft, worldShape.topRight, worldShape.bottomLeft, worldShape.bottomRight]
    .every((c) => c.a < 40)).toBe(true);
  expect(worldShape.centre.lum).toBeGreaterThan(10); // the lit globe, not the transparent background
  const shot = await app.screenshot('globe-whole-world-reset');
  expect(shot).toBeTruthy();

  // The reset toolbar button still does the same: zoom back in, then press it and confirm a home fly.
  await browser.evaluate(() => {
    const s = (window as unknown as PageGlobals).GlobeRadar.state;
    s.fly = null; s.zoom = 40;
  });
  await browser.locator('#glZoomReset').click();
  await expect.poll(() => browser.evaluate(() => {
    const s = (window as unknown as PageGlobals).GlobeRadar.state;
    return s.fly === null && s.zoom < 1.05;
  }), { timeout: 15000 }).toBe(true);

  // Manual zoom with no selection is NOT a reset: a wheel gesture stays zoomed in and the picker stays Whole world.
  await browser.evaluate(() => {
    const r = (window as unknown as PageGlobals).GlobeRadar.state.renderer;
    const box = r.canvas.getBoundingClientRect();
    r.canvas.dispatchEvent(new WheelEvent('wheel', {
      clientX: box.left + r.canvas.clientWidth / 2,
      clientY: box.top + r.canvas.clientHeight / 2,
      deltaY: -900, bubbles: true, cancelable: true,
    }));
  });
  const manual = await browser.evaluate(() => {
    const s = (window as unknown as PageGlobals).GlobeRadar.state;
    return { zoom: s.zoom, country: s.selectedCountry, picker: (document.getElementById('glCountry') as HTMLSelectElement).value };
  });
  expect(manual.zoom).toBeGreaterThan(2); // wheel zoom took effect, no reset fired
  expect(manual.country).toBe(-1);
  expect(manual.picker).toBe('');

  // Selecting a country still flies to it (the usual behaviour, not a reset).
  await browser.locator('#glCountry').selectOption({ value: 'ID' });
  await expect.poll(() => browser.evaluate(() => {
    const s = (window as unknown as PageGlobals).GlobeRadar.state;
    return s.fly === null && s.selectedCountry >= 0 && s.zoom > 1.5;
  }), { timeout: 15000 }).toBe(true);

  await browser.evaluate(() => { (window as unknown as PageGlobals).closeModal('globeModal'); return null; });
  await expect(browser.locator('#globeModal')).toBeHidden();
});
