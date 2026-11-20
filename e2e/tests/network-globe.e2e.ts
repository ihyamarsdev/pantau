import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';

type Renderer = {
  ok: boolean;
  gl: WebGL2RenderingContext;
  canvas: HTMLCanvasElement;
  arcCount: number;
  detail: { lines: unknown[] | null } | null;
  detailLines: number;
  project(n: { lat: number; lon: number }): { x: number; y: number; visible: boolean };
  unproject(x: number, y: number): { lat: number; lon: number } | null;
  setCamera(cam: { lon: number; lat: number; zoom: number }): void;
  draw(ts: number, burst: number): void;
};
type PageGlobals = {
  GlobeRadar: {
    open(): void;
    onClose(): void;
    state: {
      lastFrame: number;
      lon: number;
      lat: number;
      zoom: number;
      fly: { lon: number; lat: number; zoom: number } | null;
      velLon: number;
      velLat: number;
      autoRotate: boolean;
      graph: { nodes: Array<{ id: string; name: string; lat: number; lon: number }> };
      renderer: Renderer;
    };
  };
  WebGlobe: {
    countries(): Array<{ iso: string; name: string }>;
    countryAt(lat: number, lon: number): number;
    countryNear(lat: number, lon: number, maxDeg: number): number;
    latLonToVec(lat: number, lon: number, radius: number): number[];
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
  expect(spread.zoom).toBeLessThanOrEqual(200);

  // Once the camera settles past 2.5x, province lines load and get drawn.
  await expect.poll(() => browser.evaluate(() =>
    (window as unknown as PageGlobals).GlobeRadar.state.renderer.detailLines), { timeout: 15000 }).toBeGreaterThan(0);
  await expect.poll(() => browser.evaluate(() =>
    performance.getEntriesByType('resource').some((e) => e.name.includes('/app/globe-admin1.js')))).toBe(true);

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

  // A huge wheel delta clamps at 200x.
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
    (window as unknown as PageGlobals).GlobeRadar.state.zoom)).toBe(200);

  await browser.evaluate(() => {
    (window as unknown as PageGlobals).closeModal('globeModal');
    return null;
  });
  await expect(browser.locator('#globeModal')).toBeHidden();
});
