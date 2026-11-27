import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';

// The Teleport cluster switcher on the Kubernetes tab. The Teleport API and the cluster API are answered in the
// browser, so no tsh, no Teleport proxy and no real cluster are needed.

type ClusterRow = {
  id: number;
  name: string;
  source_type: string;
  teleport_profile_id: number;
  teleport_kube_name: string;
  kube_context_name: string;
  env_badge: string;
  is_favorite: boolean;
  kube_mode: string;
  provider: string;
  server: string;
  context: string;
  last_used_at: string | null;
  created_at: string;
};

const profiles = [
  {
    id: 1, name: 'prod', proxy: 'teleport.setoranku.com:443', auth_connector: 'local', username: 'alice',
    teleport_cluster: 'tc', teleport_home: '', default_policy: 'read-only',
  },
];

// Two Teleport clusters: prod-eu is a favorite with a context; dev-1 has no context yet, so selecting it provisions one.
const seedClusters = (): ClusterRow[] => [
  {
    id: 11, name: 'prod-eu', source_type: 'teleport', teleport_profile_id: 1, teleport_kube_name: 'prod-eu',
    kube_context_name: 'tc/prod-eu', env_badge: 'prod', is_favorite: true, kube_mode: 'read-only',
    provider: 'generic', server: '', context: '', last_used_at: null, created_at: '2026-11-27T00:00:00Z',
  },
  {
    id: 12, name: 'dev-1', source_type: 'teleport', teleport_profile_id: 1, teleport_kube_name: 'dev-1',
    kube_context_name: '', env_badge: 'dev', is_favorite: false, kube_mode: 'read-only',
    provider: 'generic', server: '', context: '', last_used_at: null, created_at: '2026-11-27T00:00:00Z',
  },
];

// stubTeleport answers the Teleport API. ensureStatus lets a test make the lazy provisioning fail with an expired session.
async function stubTeleport(
  browser: any,
  opts: { ensureStatus?: number; patches?: Array<Record<string, unknown>> } = {},
) {
  const rows = seedClusters();
  await browser.route('**/api/kube/clusters', async (route) => {
    await route.fulfill({ json: { ok: true, clusters: rows, host_clusters: [] } });
  });
  await browser.route('**/api/teleport/profiles', async (route) => {
    await route.fulfill({ json: { profiles, tsh: { installed: true, path: '/usr/bin/tsh' } } });
  });
  await browser.route('**/api/teleport/profiles/1/status', async (route) => {
    await route.fulfill({ json: { profile_id: 1, active: true, username: 'alice', ttl_seconds: 7200, expired: false } });
  });
  await browser.route('**/api/teleport/clusters/*/used', async (route) => {
    await route.fulfill({ json: { ok: true } });
  });
  await browser.route('**/api/teleport/clusters/*/ensure', async (route) => {
    if (opts.ensureStatus === 401) {
      await route.fulfill({
        status: 401,
        json: { ok: false, error: 'teleport session expired: log in again', code: 'TELEPORT_SESSION_EXPIRED', teleport_profile_id: 1 },
      });
      return;
    }
    await route.fulfill({ json: { ok: true, kube_context_name: 'tc/dev-1' } });
  });
  await browser.route('**/api/teleport/clusters/11', async (route) => {
    const body = JSON.parse(route.request.postData ?? '{}') as Record<string, unknown>;
    opts.patches?.push(body);
    await route.fulfill({ json: { ...rows[0], is_favorite: body.is_favorite === true } });
  });
  // Workloads and namespaces for a cluster target. Each cluster gets its own namespace list, so the test can tell them apart.
  await browser.route('**/api/kube/clusters/*/**', async (route) => {
    const id = Number(new URL(route.request.url).pathname.split('/')[4]);
    if (/\/namespaces$/.test(new URL(route.request.url).pathname)) {
      const names = id === 11 ? ['default', 'staging-ns'] : ['default', 'dev-ns'];
      await route.fulfill({ json: { ok: true, namespaces: names.map((name) => ({ name })) } });
      return;
    }
    if (/\/detect$/.test(new URL(route.request.url).pathname)) {
      await route.fulfill({ json: { ok: true, cluster: { distro: 'k3s', version: 'v1.30-' + id, role: 'server', cluster_id: 'cid-' + id } } });
      return;
    }
    await route.fulfill({ json: { ok: true, nodes: [], pods: [], services: [], events: [], workloads: [] } });
  });
}

// openKube signs in, opens the Kubernetes section and waits for the switcher bar.
async function openKube(app: any, screen: any, browser: any) {
  await app.open('/');
  await initApp(screen, browser);
  await browser.locator('#navItemKube').click();
  await expect(browser.locator('#tpSwitcher')).toBeVisible();
}

test('teleport switcher: search lists clusters by source, and unpinning a favorite removes its pill', async ({ app, screen, browser }) => {
  const patches: Array<Record<string, unknown>> = [];
  await stubTeleport(browser, { patches });
  await openKube(app, screen, browser);

  await expect(browser.locator('#tpPills')).toContainText('prod-eu');

  await browser.locator('#tpSearch').fill('dev');
  await expect(browser.locator('#tpDropdown')).toContainText('Teleport: prod');
  await expect(browser.locator('#tpDropdown')).toContainText('dev-1');
  await expect(browser.locator('#tpDropdown')).not.toContainText('prod-eu');

  // The star on the pill unpins the favorite; the pill goes away because the cluster was never used.
  await browser.locator('#tpPills button[data-tp="fav"][data-id="11"]').click();
  await expect(browser.locator('#tpPills')).not.toContainText('prod-eu');
  expect(patches).toEqual([{ is_favorite: false }]);
});

test('teleport switcher: switching clusters keeps each cluster\'s namespace', async ({ app, screen, browser }) => {
  await stubTeleport(browser);
  await openKube(app, screen, browser);

  await browser.locator('#tpPills button[data-tp="switch"][data-id="11"]').click();
  await browser.locator('#tabKubePods').click(); // namespaces apply to pods; the nodes tab hides the selector
  await expect(browser.locator('#kubeNamespaceSelect')).toHaveValue('');
  await expect(browser.locator('#kubeNamespaceSelect')).toContainText('staging-ns');
  await expect(browser.locator('#kubeNamespaceSelect')).toBeEnabled();
  await browser.locator('#kubeNamespaceSelect').selectOption('staging-ns');
  await expect(browser.locator('#kubeNamespaceSelect')).toHaveValue('staging-ns');

  // dev-1 has no context yet: the switch provisions it first, then loads its own namespaces.
  await browser.locator('#tpSearch').fill('dev-1');
  await browser.locator('//div[@id="tpDropdown"]//button[contains(.,"dev-1")]').click();
  await expect(browser.locator('#kubeNamespaceSelect')).toHaveValue('');
  await expect(browser.locator('#kubeNamespaceSelect')).not.toContainText('staging-ns');

  await browser.locator('#tpPills button[data-tp="switch"][data-id="11"]').click();
  await expect(browser.locator('#kubeNamespaceSelect')).toHaveValue('staging-ns');
});

test('teleport switcher: an expired session shows the Re-Login banner naming the profile', async ({ app, screen, browser }) => {
  await stubTeleport(browser, { ensureStatus: 401 });
  await openKube(app, screen, browser);

  await browser.locator('#tpSearch').fill('dev-1');
  await browser.locator('//div[@id="tpDropdown"]//button[contains(.,"dev-1")]').click();

  await expect(browser.locator('#tpBanner')).toBeVisible();
  await expect(browser.locator('#tpBanner')).toContainText('Re-Login');
  await expect(browser.locator('#tpBanner button[data-tp="relogin"][data-id="1"]')).toBeVisible();
});
