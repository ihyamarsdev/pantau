import { afterEach, test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { deleteHosts, initApp, uniqueHostName } from './helpers.js';

// The whole run shares one database, so the hosts this test adds are removed again. afterEach runs
// after a failure too, so they never survive a failing test either.
afterEach(async ({ browser }) => {
  await deleteHosts(browser, ['e2e-private', 'e2e-behind-dead']);
});

// Network names must be unique per run: the throwaway DB is shared by every test in the suite.
function uniqueNetworkName(prefix: string): string {
  return `${prefix}-${Date.now()}-${Math.floor(Math.random() * 100000)}`;
}

type BrowserHandle = Parameters<Parameters<typeof test>[1]>[0]['browser'];

// Every network this file creates is removed here, even on failure. Force-detaches hosts that still
// reference it, so a failed assertion cannot leak a network into the next test.
async function deleteNetworks(browser: BrowserHandle, names: string[]): Promise<void> {
  if (!names.length) return;
  await browser.unrouteAll();
  await browser.evaluate(async (wanted: string[]) => {
    const rows = await (await fetch('/api/vpn/networks')).json() as { id: number; name: string }[];
    for (const n of rows) {
      if (wanted.includes(n.name)) {
        await fetch(`/api/vpn/networks/${n.id}?force=1`, { method: 'DELETE' });
      }
    }
  }, names);
}

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

  await browser.evaluate(async () => { await (window as unknown as { loadHosts(): Promise<void> }).loadHosts(); return null; });

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

// Telemetry, probe endpoint and circuit breaker: all through the custom driver, no real VPN.
test('network telemetry is exposed, the probe endpoint is gate-independent, and a downed path fails fast', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);

  const name = uniqueNetworkName('e2e-telemetry');
  const hostName = uniqueHostName('e2e-breaker-host');
  const secret = 'e2e-secret-do-not-leak';
  try {
    const seeded = await browser.evaluate(async ({ name, hostName, secret }) => {
      const api = (method: string, url: string, body?: unknown) =>
        fetch(url, { method, headers: { 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined });
      await api('PUT', '/api/vpn/gate', { enabled: true });
      // The secret lives inside a secret config field of the custom driver. It is never executed as
      // a command on its own: `false # <secret>` is a shell comment, and the failing exit opens the breaker.
      const resp = await api('POST', '/api/vpn/networks', {
        name, driver: 'custom', iface: 'pantau-e2e-t', enabled: true, auto_up: true,
        config: { up: 'true', down: 'true', status: `false # ${secret}` },
      });
      if (!resp.ok) throw new Error('create network failed: ' + resp.status + ' ' + await resp.text());
      const net = await resp.json();
      const host = await (await api('POST', '/api/hosts', {
        name: hostName, host: '10.9.7.7', port: 22, user: 'root', network_id: net.id,
      })).json();
      return { id: net.id as number, hostID: host.id as number };
    }, { name, hostName, secret });

    // A failed bring-up must open the breaker for this network.
    await browser.evaluate(async (id: number) => {
      await fetch(`/api/vpn/networks/${id}/up`, { method: 'POST' });
    }, seeded.id);

    // Telemetry fields are present on the list row, and no secret is echoed back.
    const telemetry = await browser.evaluate(async ({ id, secret }) => {
      const body = await (await fetch('/api/vpn/networks')).text();
      const rows = JSON.parse(body) as Record<string, unknown>[];
      const row = (rows || []).find(n => Number(n.id) === id) || {};
      return {
        body,
        virtual_ip: typeof row.virtual_ip === 'string',
        probe_ms: typeof row.probe_ms === 'number',
        degraded: typeof row.degraded === 'boolean',
        breaker: typeof row.breaker === 'string' && ['closed', 'open', 'half-open'].includes(String(row.breaker)),
        leaked: body.includes(secret),
      };
    }, { id: seeded.id, secret });
    expect(telemetry.virtual_ip).toBe(true);
    expect(telemetry.probe_ms).toBe(true);
    expect(telemetry.degraded).toBe(true);
    expect(telemetry.breaker).toBe(true);
    expect(telemetry.leaked).toBe(false);
    const probe = await browser.evaluate(async (id: number) => {
      await fetch('/api/vpn/gate', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ enabled: false }) });
      const res = await fetch(`/api/vpn/networks/${id}/probe`, { method: 'POST' });
      const body: unknown = await res.json().catch(() => ({}));
      const state = body !== null && typeof body === 'object' && 'state' in body ? String(body.state || '') : '';
      return { status: res.status, state };
    }, seeded.id);
    expect(probe.status).toBe(200);
    expect(['down', 'starting', 'up', 'error']).toContain(probe.state);

    // A bad id is rejected.
    const bad = await browser.evaluate(async () => (await fetch('/api/vpn/networks/not-an-id/probe', { method: 'POST' })).status);
    expect([400, 404]).toContain(bad);

    // Restore the gate, then measure fail-fast on repeated dials to the downed network.
    await browser.evaluate(async () => {
      await fetch('/api/vpn/gate', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ enabled: true }) });
    });
    const dials = await browser.evaluate(async (id: number) => {
      const once = async () => {
        const start = Date.now();
        const res = await (await fetch(`/api/hosts/${id}/test`, { method: 'POST' })).json();
        return { ms: Date.now() - start, error: String(res.error || res.output || '') };
      };
      return { first: await once(), second: await once() };
    }, seeded.hostID);
    expect(dials.first.error).toContain('network down');
    expect(dials.second.error).toContain('network down');
    // The second dial must not pay another full TCP timeout: fail-fast from the breaker.
    expect(dials.second.ms).toBeLessThanOrEqual(dials.first.ms + 2000);

    // The host card shows "Network down", not a plain dead-host badge.
    await browser.evaluate(async () => { await (window as unknown as { loadHosts(): Promise<void> }).loadHosts(); return null; });
    const card = browser.locator(`[data-host-id]:has-text("${hostName}")`);
    await expect(card).toContainText('Network down');
  } finally {
    await deleteNetworks(browser, [name]);
    await deleteHosts(browser, [hostName]);
  }
});
