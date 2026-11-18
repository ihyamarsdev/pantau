import { afterEach, test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { deleteHosts, initApp } from './helpers.js';

// The whole E2E run shares one database. Remove the host this suite seeds so the
// graph assertions below do not depend on hosts another test left behind.
afterEach(async ({ browser }) => {
  await deleteHosts(browser, ['e2e-topology-host']);
});

type Graph = {
  nodes: { id: string; kind: string; label: string; tier: number; status: string; host_id?: number; alerts: number; group?: string }[];
  edges: { id: string; source: string; target: string; kind: string; active: boolean }[];
  counts: Record<string, number>;
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
        name: 'e2e-topology-net', driver: 'custom', iface: 'tp-map0', enabled: true, auto_up: false,
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

test('GET /api/topology returns a tiny macro graph with a TTL cache', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);
  const seeded = await seed(browser);

  const graph = (await browser.evaluate(async () => await (await fetch('/api/topology')).json())) as Graph;
  const hostNode = graph.nodes.find((n) => n.id === 'host:' + seeded.hostId);

  // Gateway (tier 0), VPN (tier 0), and host (tier 1) are macro nodes.
  expect(graph.nodes.some((n) => n.id === 'gateway' && n.tier === 0 && n.status === 'healthy')).toBe(true);
  expect(graph.nodes.some((n) => n.id === 'vpn:' + seeded.netId && n.tier === 0)).toBe(true);
  expect(hostNode).toBeDefined();
  expect(hostNode!.label).toBe('e2e-topology-host');
  expect(hostNode!.tier).toBe(1);
  expect(hostNode!.status).toBe('unknown');
  expect(hostNode!.group).toBe('TopologyLab');
  expect(graph.counts.gateway).toBe(1);

  // No workload nodes in the global graph — containers/ports are lazy only.
  expect(graph.nodes.every((n) => ['gateway', 'vpn', 'cloudflare', 'host', 'kube'].includes(n.kind))).toBe(true);

  // Host reached through its VPN, not directly; all edges reference real nodes.
  expect(graph.edges.some((e) => e.kind === 'vpn' && e.target === 'host:' + seeded.hostId)).toBe(true);
  expect(graph.edges.some((e) => e.kind === 'direct' && e.target === 'host:' + seeded.hostId)).toBe(false);
  const ids = new Set(graph.nodes.map((n) => n.id));
  expect(graph.edges.every((e) => ids.has(e.source) && ids.has(e.target))).toBe(true);

  // Cache: a config write does not change the graph until explicit invalidation
  // (the app calls this on real host/VPN writes; here we verify the TTL behaviour
  // by checking two consecutive reads return the same generated_at).
  const g1 = (await browser.evaluate(async () => await (await fetch('/api/topology')).json())) as Graph;
  const g2 = (await browser.evaluate(async () => await (await fetch('/api/topology')).json())) as Graph;
  expect(g1.generated_at).toBe(g2.generated_at);
});

test('the topology map renders grid cards + SVG overlay, filters, and loads host detail lazily', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);
  const seeded = await seed(browser);

  // Header launcher opens the Workspace Modal.
  await browser.locator('#btnHeaderTopology').click();
  await expect(browser).toHaveClass(browser.locator('#topologyModal'), /open/);
  await expect(browser.locator('#tpLines')).toBeVisible();
  await expect.poll(() => browser.locator('.tp-card').count()).toBeGreaterThanOrEqual(2);

  // Hybrid layout: HTML cards in 3 grid columns + a single SVG line overlay.
  const shape = await browser.evaluate(() => {
    const card = document.querySelector('.tp-card');
    const cols = document.querySelectorAll('.tp-column').length;
    const lines = document.querySelectorAll('#tpLines path.tp-line').length;
    return {
      isHtmlCard: card?.tagName === 'DIV',
      columns: cols,
      lines,
      svgPointerEvents: getComputedStyle(document.getElementById('tpLines')!).pointerEvents,
    };
  });
  expect(shape.isHtmlCard).toBe(true);
  expect(shape.columns).toBe(3);
  expect(shape.lines).toBeGreaterThanOrEqual(1);
  expect(shape.svgPointerEvents).toBe('none');

  // Health filter shows only hosts of the chosen status.
  await browser.locator('#tpHealth').selectOption({ value: 'unknown' });
  await expect.poll(() => browser.locator('.tp-card').count()).toBeGreaterThanOrEqual(1);
  expect(await browser.evaluate(() => Array.from(document.querySelectorAll('.tp-card')).every((c) => (c as HTMLElement).dataset.status === 'unknown'))).toBe(true);
  await browser.locator('#tpHealth').selectOption({ value: '' });

  // Group filter shows only the seeded host's group.
  await browser.locator('#tpGroup').selectOption({ value: 'TopologyLab' });
  await expect(browser.locator('.tp-card')).toHaveCount(1);
  await browser.locator('#tpGroup').selectOption({ value: '' });

  // Clicking a host card opens the drawer and lazily loads service detail.
  await browser.locator(`.tp-card[data-node-id="host:${seeded.hostId}"]`).click();
  await expect(browser.locator('#tpDrawer')).toBeVisible();
  await expect(browser.locator('#tpDrawerTerminal')).toBeVisible();
  await expect(browser.locator('#tpDrawerWorkspace')).toBeVisible();
  await expect(browser.locator('#tpDrawerLogs')).toBeVisible();

  // The lazy detail endpoint is reachable and keyed to this host.
  const detail = (await browser.evaluate(async (id: number) =>
    await (await fetch('/api/topology/host?id=' + id)).json(), seeded.hostId)) as Detail;
  expect(detail.host_id).toBe(seeded.hostId);
  expect(Array.isArray(detail.containers)).toBe(true);
  expect(Array.isArray(detail.listening)).toBe(true);

  // Closing the modal hides it (SSE cleanup runs through Topology.onClose).
  await browser.evaluate(() => { closeModal('topologyModal'); return null; });
  await expect(browser.locator('#topologyModal')).toBeHidden();
});
