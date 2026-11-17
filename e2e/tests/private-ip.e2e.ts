import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';

type PageGlobals = {
  openHostDetail(id: number): Promise<void>;
  closeModal(id: string): void;
  loadHosts(): Promise<void>;
  openNetDiagModal(id?: number): void;
};

// The host list and the host detail are answered in the browser: this checks the recorded private IPs
// are shown on the host, and that Use private IP sends the probe down the private path.
test('private ip: shown on the host and sent as the inter-host probe target', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);

  const ids = (await browser.evaluate(async () => {
    const add = async (name: string, host: string) =>
      (await (await fetch('/api/hosts', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name, host, port: 22, user: 'root' }) })).json()).id;
    return [await add('src', '198.51.100.10'), await add('dst', '203.0.113.9')];
  })) as number[];
  const [srcID, dstID] = ids;
  const host = (id: number, name: string, address: string, privateIPs: string) => ({ id, name, host: address, port: 22, user: 'root', status: 'healthy', private_ips: privateIPs });

  await browser.route('**/api/hosts', async (route) => {
    await route.fulfill({ json: [host(srcID, 'src', '198.51.100.10', '["10.0.0.11"]'), host(dstID, 'dst', '203.0.113.9', '["10.20.0.7","172.17.0.1"]')] });
  });
  await browser.route(`**/api/hosts/${dstID}`, async (route) => {
    await route.fulfill({ json: { host: host(dstID, 'dst', '203.0.113.9', '["10.20.0.7","172.17.0.1"]') } });
  });

  // The overview of a host shows its private IPs.
  await browser.evaluate(async (id) => {
    const w = window as unknown as PageGlobals;
    await w.openHostDetail(id);
    return null;
  }, dstID);
  await expect(browser.locator('#hostDetailModal')).toContainText('10.20.0.7, 172.17.0.1');
  await browser.evaluate(() => {
    const w = window as unknown as PageGlobals;
    w.closeModal('hostDetailModal');
    return null;
  });

  let asked = '';
  await browser.route('**/api/netdiag/stream*', async (route) => {
    asked = route.request.url;
    await route.fulfill({ status: 200, headers: { 'Content-Type': 'text/event-stream' }, body: '' });
  });

  await browser.evaluate(async (src) => {
    const w = window as unknown as PageGlobals;
    await w.loadHosts();
    w.openNetDiagModal(src);
    return null;
  }, srcID);

  await expect(browser.locator('#netdiag_modal_viaPrivate')).toBeVisible();
  await browser.locator('#netdiag_modal_targetHost').selectOption({ value: String(dstID) });
  await expect(browser.locator('#netdiag_modal_viaPrivateNote')).toContainText('10.20.0.7');

  await browser.locator('#netdiag_modal_viaPrivate').check();
  await browser.locator('#netdiag_modal_runBtn').click();
  await expect.poll(() => asked).toContain('via=private');
});
