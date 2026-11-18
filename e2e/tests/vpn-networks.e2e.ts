import { afterEach, test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { deleteHosts, initApp } from './helpers.js';

// The whole run shares one database, so the hosts this test adds are removed again. afterEach runs
// after a failure too, so they never survive a failing test either.
afterEach(async ({ browser }) => {
  await deleteHosts(browser, ['e2e-private', 'e2e-behind-dead']);
});

// The VPN gate is off by default; a network whose bring-up fails must read as a network problem, not a dead host.
test('a host behind a VPN network dials through its tunnel, and a dead tunnel is reported as such', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);

  // Gate closed: bringing a tunnel up is refused.
  const closed = await browser.evaluate(async () => {
    const res = await fetch('/api/vpn/networks/1/up', { method: 'POST' });
    return res.status;
  });
  expect([404, 409]).toContain(closed);

  const seeded = await browser.evaluate(async () => {
    const api = (method: string, url: string, body?: unknown) =>
      fetch(url, { method, headers: { 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined });
    await api('PUT', '/api/vpn/gate', { enabled: true });
    const good = await (await api('POST', '/api/vpn/networks', {
      name: 'e2e-tunnel', driver: 'custom', iface: 'lo', enabled: true, auto_up: true,
      config: { up: 'true', down: 'true', status: 'true' },
    })).json();
    const dead = await (await api('POST', '/api/vpn/networks', {
      name: 'e2e-dead-tunnel', driver: 'custom', iface: 'pantau-e2e0', enabled: true, auto_up: true,
      config: { up: 'false', down: 'true', status: 'false' },
    })).json();
    const live = await (await api('POST', '/api/hosts', {
      name: 'e2e-private', host: '10.9.9.9', port: 22, user: 'root', network_id: good.id,
    })).json();
    const behind = await (await api('POST', '/api/hosts', {
      name: 'e2e-behind-dead', host: '10.9.9.8', port: 22, user: 'root', network_id: dead.id,
    })).json();
    return { good: good.id as number, dead: dead.id as number, live: live.id as number, behind: behind.id as number };
  });

  // The tunnel comes up and the host card carries the network badge.
  const check = await browser.evaluate(async (id: number) => {
    const up = await fetch('/api/vpn/networks/' + id + '/up', { method: 'POST' });
    const networks = await (await fetch('/api/vpn/networks')).json();
    return { status: up.status, state: (networks || []).find((n: { id: number }) => n.id === id)?.last_state };
  }, seeded.good);
  expect(check.status).toBe(200);
  expect(check.state).toBe('up');

  await browser.evaluate(() => { loadHosts(); return null; });
  await expect(browser.locator('#hostGrid')).toContainText('e2e-tunnel');

  // A dead path is a network failure: the error names the network, never the host.
  const dialed = await browser.evaluate(async (id: number) => {
    const res = await (await fetch('/api/hosts/' + id + '/test', { method: 'POST' })).json();
    return String(res.error || res.output || '');
  }, seeded.behind);
  expect(dialed).toContain('network down');

  // Deleting a network that still has hosts needs an explicit detach.
  const conflict = await browser.evaluate(async (id: number) => {
    const res = await fetch('/api/vpn/networks/' + id, { method: 'DELETE' });
    const body = await res.json().catch(() => ({}));
    return { status: res.status, hosts: body.hosts };
  }, seeded.good);
  expect(conflict.status).toBe(409);
  expect(conflict.hosts).toBe(1);

  // The host edit form offers the network, so a host can be moved onto another path.
  await browser.evaluate(() => { openAddHostModal(); return null; });
  const options = await browser.evaluate(() => Array.from(document.querySelectorAll('#hostNetwork option')).map(o => (o as HTMLOptionElement).textContent || ''));
  expect(options.join('|')).toContain('e2e-tunnel');
  await browser.evaluate(() => { closeModal('hostModal'); return null; });
});
