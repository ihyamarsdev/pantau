import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';

type PageGlobals = {
  GlobeRadar: { open(): void; onClose(): void };
  closeModal(id: string): void;
};

// The globe payload is answered in the browser so the test is deterministic and
// needs no real SSH, GeoIP, or Cloudflare. It checks the modal renders nodes and
// flows, the flow-type filter works, and a pin click shows the HUD shortcuts.
test('network globe: renders nodes/flows, filters flow types, and shows a host HUD', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);

  const payload = {
    nodes: [
      { id: 'gateway', name: 'Pantau Gateway', kind: 'gateway', ip: '', status: 'healthy', lat: 0, lon: 0, city: 'Gateway', active_alerts: 0, geo_source: 'default' },
      { id: 'host:1', name: 'globe-web', kind: 'host', host_id: 1, ip: '203.0.113.10', public_ip: '203.0.113.10', status: 'healthy', lat: 40.7, lon: -74, city: 'New York', country_code: 'US', latency_ms: 18, cpu_pct: 12, mem_pct: 40, active_alerts: 0, group_id: 'Web', network_id: 0, geo_source: 'geoip' },
      { id: 'host:2', name: 'globe-db', kind: 'host', host_id: 2, ip: '10.0.0.5', status: 'degraded', lat: -6.2, lon: 106.8, city: 'Jakarta', active_alerts: 2, group_id: 'DB', network_id: 4, geo_source: 'manual' },
    ],
    flows: [
      { id: 'gw-host-1', source: 'gateway', target: 'host:1', type: 'ssh', latency_ms: 18, status: 'healthy', active: true },
      { id: 'gw-host-2', source: 'gateway', target: 'host:2', type: 'vpn', status: 'degraded', active: true },
      { id: 'proxy-1-2', source: 'host:1', target: 'host:2', type: 'proxy', status: 'healthy', active: true },
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
  await expect(browser.locator('#glCount')).toContainText('3 flows');

  // The sidebar lists the managed hosts.
  await expect(browser.locator('#glSideList')).toContainText('globe-web');
  await expect(browser.locator('#glSideList')).toContainText('globe-db');

  // Flow-type filter narrows the count.
  await browser.locator('.gl-pill[data-flow="proxy"]').click();
  await expect(browser.locator('#glCount')).toContainText('1 flows');
  await browser.locator('.gl-pill[data-flow="all"]').click();
  await expect(browser.locator('#glCount')).toContainText('3 flows');

  // Clicking a host in the sidebar flies to it and opens the HUD with shortcuts.
  await browser.locator('.gl-side-item[data-node="host:1"]').click();
  await expect(browser.locator('#glHud')).toBeVisible();
  await expect(browser.locator('#glHud')).toContainText('globe-web');
  await expect(browser.locator('#glHud')).toContainText('New York');
  await expect(browser.locator('#glHudTerminal')).toBeEnabled();
  await expect(browser.locator('#glHudWorkspace')).toBeEnabled();

  // Closing stops the render loop and detaches the view.
  await browser.evaluate(() => {
    (window as unknown as PageGlobals).closeModal('globeModal');
    return null;
  });
  await expect(browser.locator('#globeModal')).toBeHidden();
});
