import { afterEach, test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { deleteHosts, initApp } from './helpers.js';

// The whole E2E run shares one database. Remove the host this suite seeds so the
// graph assertions below do not depend on hosts another test left behind.
afterEach(async ({ browser }) => {
  await deleteHosts(browser, ['e2e-topology-host']);
});

type Graph = {
  nodes: {
    id: string; kind: string; label: string; tier: number; ring: number;
    sector: number; sectors: number; status: string; host_id?: number;
    alerts: number; group?: string; metrics?: { cpu_pct: number; ram_pct: number };
  }[];
  edges: { id: string; source: string; target: string; kind: string; active: boolean }[];
  counts: Record<string, number>;
  generated_at: string;
};
type Detail = { host_id: number; containers: { name: string; status: string }[]; listening: { name: string }[] };

// Seed one host behind a VPN network, reusing an existing network when present
// (the DB is shared across the whole run and network names/ifaces must be unique).
async function seed(browser: { evaluate<T>(fn: () => T | Promise<T>): Promise<T> }): Promise<{ netId: number; hostId: number }> {
  return browser.evaluate(async () => {
    const api = (method: string, url: string, body?: unknown) =>
      fetch(url, { method, headers: { 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined });
    await api('PUT', '/api/vpn/gate', { enabled: true });
    const existing = await (await fetch('/api/vpn/networks')).json() as { id: number }[];
    let netId: number;
    if (existing.length) {
      netId = existing[0].id;
    } else {
      const net = await (await api('POST', '/api/vpn/networks', {
        name: 'e2e-topology-net', driver: 'custom', iface: 'tp-radar0', enabled: true, auto_up: false,
        config: { up: 'true', down: 'true', status: 'true' },
      })).json();
      netId = net.id as number;
    }
    const host = await (await api('POST', '/api/hosts', {
      name: 'e2e-topology-host', host: '10.42.0.7', port: 22, user: 'root', group_name: 'TopologyLab', network_id: netId,
    })).json();
    return { netId, hostId: host.id as number };
  });
}

test('GET /api/topology returns a cached macro graph with deterministic radial rings', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);
  const seeded = await seed(browser);

  const graph = (await browser.evaluate(async () => await (await fetch('/api/topology')).json())) as Graph;
  const hostNode = graph.nodes.find((n) => n.id === 'host:' + seeded.hostId);

  // Gateway (hub), VPN (inner), host (outer) are macro nodes with ring metadata.
  expect(graph.nodes.some((n) => n.id === 'gateway' && n.ring === 0 && n.sectors === 1 && n.status === 'healthy')).toBe(true);
  expect(graph.nodes.some((n) => n.id === 'vpn:' + seeded.netId && n.ring === 1)).toBe(true);
  expect(hostNode).toBeDefined();
  expect(hostNode!.label).toBe('e2e-topology-host');
  expect(hostNode!.tier).toBe(1);
  expect(hostNode!.ring).toBe(2);
  expect(hostNode!.status).toBe('unknown');
  expect(hostNode!.group).toBe('TopologyLab');
  expect(graph.counts.gateway).toBe(1);

  // Only macro kinds exist in the global graph — containers/ports are lazy only.
  expect(graph.nodes.every((n) => ['gateway', 'vpn', 'cloudflare', 'host', 'kube'].includes(n.kind))).toBe(true);

  // Every ring's sectors are unique and in range; host reached via VPN, not directly.
  for (const ring of [1, 2]) {
    const members = graph.nodes.filter((n) => n.ring === ring);
    const sectors = members.map((n) => n.sector);
    expect(new Set(sectors).size).toBe(sectors.length);
    expect(members.every((n) => n.sectors === members.length && n.sector >= 0 && n.sector < members.length)).toBe(true);
  }
  expect(graph.edges.some((e) => e.kind === 'vpn' && e.target === 'host:' + seeded.hostId)).toBe(true);
  expect(graph.edges.some((e) => e.kind === 'direct' && e.target === 'host:' + seeded.hostId)).toBe(false);
  const ids = new Set(graph.nodes.map((n) => n.id));
  expect(graph.edges.every((e) => ids.has(e.source) && ids.has(e.target))).toBe(true);

  // Cache: two consecutive reads return the same generated_at.
  const g1 = (await browser.evaluate(async () => await (await fetch('/api/topology')).json())) as Graph;
  const g2 = (await browser.evaluate(async () => await (await fetch('/api/topology')).json())) as Graph;
  expect(g1.generated_at).toBe(g2.generated_at);
});

test('the radar renders one SVG with polar-positioned nodes, filter pills, viewBox zoom/pan, and a lazy HUD', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);
  const seeded = await seed(browser);

  // Header launcher opens the Workspace Modal.
  await browser.locator('#btnHeaderTopology').click();
  await expect(browser.locator('#topologyModal')).toBeVisible();
  await expect(browser.locator('#tpRadar')).toBeVisible();
  await expect.poll(() => browser.locator('#tpNodes .tp-node').count()).toBeGreaterThanOrEqual(2);

  // Pure SVG: exactly one <svg> with a 1000x700 viewBox, no HTML card grid.
  const shape = await browser.evaluate(() => {
    const svg = document.getElementById('tpRadar')!;
    const node = document.querySelector('#tpNodes .tp-node') as SVGGElement | null;
    return {
      viewBox: svg.getAttribute('viewBox'),
      nodeTag: node?.tagName,
      transform: node?.getAttribute('transform') || '',
      ranges: document.querySelectorAll('#tpRanges circle.tp-range').length,
      edges: document.querySelectorAll('#tpEdges path.tp-edge').length,
      cards: document.querySelectorAll('.tp-card, .tp-column').length,
    };
  });
  expect(shape.viewBox.split(/\s+/).map(Number)).toEqual([0, 0, 1000, 700]);
  expect(shape.nodeTag).toBe('g');
  expect(shape.transform).toMatch(/^translate\([\d.]+ [\d.]+\)$/);
  expect(shape.ranges).toBe(4);
  expect(shape.edges).toBeGreaterThanOrEqual(1);
  expect(shape.cards).toBe(0);

  // Node positions are deterministic across renders: same transform after a reload.
  const before = await browser.evaluate((id: number) =>
    (document.querySelector(`#tpNodes .tp-node[data-node-id="host:${id}"]`) as SVGGElement | null)?.getAttribute('transform'), seeded.hostId);
  await browser.reload();
  await initApp(screen, browser);
  await browser.locator('#btnHeaderTopology').click();
  await expect.poll(() => browser.locator('#tpNodes .tp-node').count()).toBeGreaterThanOrEqual(2);
  const after = await browser.evaluate((id: number) =>
    (document.querySelector(`#tpNodes .tp-node[data-node-id="host:${id}"]`) as SVGGElement | null)?.getAttribute('transform'), seeded.hostId);
  expect(after).toBe(before);

  // Group filter pill shows only the seeded host's group.
  await browser.locator('.tp-pill[data-group="TopologyLab"]').click();
  await expect(browser.locator('#tpNodes .tp-node')).toHaveCount(1);
  await browser.locator('.tp-pill[data-group=""]').click();
  await expect.poll(() => browser.locator('#tpNodes .tp-node').count()).toBeGreaterThanOrEqual(2);

  // Zoom changes only the viewBox; reset restores the default.
  await browser.locator('#tpZoomIn').click();
  const zoomed = (await browser.evaluate(() => document.getElementById('tpRadar')!.getAttribute('viewBox'))!).split(/\s+/).map(Number);
  expect(zoomed).not.toEqual([0, 0, 1000, 700]);
  await browser.locator('#tpZoomReset').click();
  const resetView = (await browser.evaluate(() => document.getElementById('tpRadar')!.getAttribute('viewBox'))!).split(/\s+/).map(Number);
  expect(resetView).toEqual([0, 0, 1000, 700]);


  // Animation runs continuously without hover: the highlighted route advances
  // on its own, and every icon is actually drawn (stroke set by CSS).
  const iconCss = await browser.evaluate(() => {
    const el = document.querySelector('.tp-icon-host') as SVGUseElement | null;
    if (!el) return null;
    const cs = getComputedStyle(el);
    return { stroke: cs.stroke, fill: cs.fill, anim: cs.animationName };
  });
  expect(iconCss).not.toBeNull();
  expect(iconCss!.stroke).not.toBe('none');
  expect(iconCss!.fill).toBe('none');
  expect(iconCss!.anim).toBe('tpIconBreathe');
  // Animation runs continuously without hover: a route stays highlighted and
  // its packet flow animates; every icon is drawn and breathes.
  const live = await browser.evaluate(() => {
    const route = document.querySelector('#tpEdges .tp-edge-route');
    const icon = document.querySelector('.tp-icon-host') as SVGUseElement | null;
    const iconCs = icon ? getComputedStyle(icon) : null;
    return {
      routeCount: document.querySelectorAll('#tpEdges .tp-edge-route').length,
      routeAnim: route ? getComputedStyle(route).animationName : '',
      iconAnim: iconCs ? iconCs.animationName : '',
    };
  });
  expect(live.routeCount).toBeGreaterThanOrEqual(1);
  expect(live.routeAnim).toBe('tpFlow');
  expect(live.iconAnim).toBe('tpIconBreathe');
  // Clicking a host node shows the floating HUD with lazy service detail.
  await browser.locator(`#tpNodes .tp-node[data-node-id="host:${seeded.hostId}"]`).click();
  await expect(browser.locator('#tpHud')).toBeVisible();
  await expect(browser.locator('#tpHudTerminal')).toBeVisible();
  await expect(browser.locator('#tpHudWorkspace')).toBeVisible();
  await expect(browser.locator('#tpHudLogs')).toBeVisible();

  // Interactive animation: selecting a host lights exactly its route to the
  // gateway and dims the rest; the server icon breathes.
  const anim = await browser.evaluate(async (id: number) => {
    const graph = await (await fetch('/api/topology')).json() as Graph;
    const hostId = 'host:' + id;
    // Expected route = the edges touching this host plus the VPN uplink if present.
    const hostEdges = graph.edges.filter((e) => e.source === hostId || e.target === hostId);
    const expected = new Set(hostEdges.map((e) => e.id));
    hostEdges.forEach((e) => {
      const vpn = e.source.startsWith('vpn:') ? e.source : (e.target.startsWith('vpn:') ? e.target : null);
      if (vpn) graph.edges.filter((x) => x.target === vpn || x.source === vpn).forEach((x) => expected.add(x.id));
    });
    const routes = Array.from(document.querySelectorAll('#tpEdges .tp-edge-route')).map((e) => e.getAttribute('data-edge-id')!);
    const icon = document.querySelector('.tp-icon-host') as SVGUseElement | null;
    return {
      routes,
      expected: Array.from(expected).sort(),
      dim: document.querySelectorAll('#tpEdges .tp-edge-dim').length,
      offRoute: graph.edges.length - expected.size,
      routeAnim: routes.length ? getComputedStyle(document.querySelector('#tpEdges .tp-edge-route')!).animationName : '',
      iconAnim: icon ? getComputedStyle(icon).animationName : '',
    };
  }, seeded.hostId);
  expect(anim.routes.slice().sort()).toEqual(anim.expected);
  expect(anim.routes.length).toBeGreaterThanOrEqual(1);
  expect(anim.dim).toBe(anim.offRoute);
  expect(anim.routeAnim).toBe('tpFlow');
  expect(anim.iconAnim).toBe('tpIconBreathe');

  const detail = (await browser.evaluate(async (id: number) =>
    await (await fetch('/api/topology/host?id=' + id)).json(), seeded.hostId)) as Detail;
  expect(detail.host_id).toBe(seeded.hostId);
  expect(Array.isArray(detail.containers)).toBe(true);
  expect(Array.isArray(detail.listening)).toBe(true);

  // Closing the modal hides it (SSE cleanup runs through Topology.onClose).
  await browser.evaluate(() => { closeModal('topologyModal'); return null; });
  await expect(browser.locator('#topologyModal')).toBeHidden();
});
