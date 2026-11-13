import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';

// Two hosts whose kube API is answered in the browser: each is its own one-node cluster, so no real cluster is needed.
test('kubernetes: picked clusters become workspace cards that open on their own tab and survive a reload', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);

  const ids = (await browser.evaluate(async () => {
    const add = async (name: string, host: string) =>
      (await (await fetch('/api/hosts', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name, host, port: 1, user: 'x' }) })).json()).id;
    const made = [await add('alpha', '10.0.0.1'), await add('beta', '10.0.0.2')];
    await (window as unknown as { loadHosts(): Promise<void> }).loadHosts(); // the page's own loader, so the picker knows the hosts
    return made;
  })) as number[];

  const detects: Record<string, number> = { alpha: 0, beta: 0 };
  await browser.route('**/api/hosts/*/kube/**', async (route) => {
    const url = new URL(route.request.url);
    const [, id, sub] = url.pathname.match(/hosts\/(\d+)\/kube\/([\w-]+)/)!;
    const name = Number(id) === ids[0] ? 'alpha' : 'beta';
    if (sub === 'detect') detects[name]++;
    const answers: Record<string, unknown> = {
      detect: { cluster: { distro: 'k3s', version: 'v1.30-' + name, role: 'server', cluster_id: 'cid-' + name } },
      namespaces: { namespaces: [{ name: 'default' }, { name: name + '-ns' }] },
      nodes: { nodes: [{ name: name + '-node', status: 'Ready', roles: ['control-plane'], version: 'v1.30', internalIP: '10.9.9.9' }] },
      pods: { pods: [{ namespace: 'default', name: name + '-pod', status: 'Running', ready: '1/1', node: name + '-node' }] },
      describe: { output: 'described on ' + name },
    };
    await route.fulfill({ json: { ok: true, ...(answers[sub] as object) } });
  });
  const pick = async (name: string) => {
    await browser.locator('#kubeHostPickerBtn').click();
    await browser.locator(`//div[@id="kubeHostPickerList"]//span[text()="${name}"]`).click();
  };
  const card = (name: string) => browser.locator(`//div[@id="kubeWorkspaceGrid"]//div[contains(@class,"kube-ws-card")][contains(.,"${name}")]`);
  const grid = browser.locator('#kubeWorkspaceGrid');
  const back = browser.locator('#btnKubeBack');

  // Picking a cluster opens it; its card waits in the list behind Back.
  await browser.locator('#navItemKube').click();
  await pick('alpha');
  await expect(browser.locator('#kubeTabContent')).toContainText('alpha-node');
  await expect(grid).toBeHidden();
  await back.click();
  await expect(card('alpha')).toBeVisible();
  await expect(browser.locator('#kubeViewsContainer')).toBeHidden();

  await pick('beta');
  await expect(browser.locator('#kubeVersionChip')).toHaveText('v1.30-beta');
  await expect(browser.locator('#kubeTabContent')).toContainText('beta-node');
  await browser.locator('#tabKubePods').click();
  await expect(browser.locator('#kubeTabContent')).toContainText('beta-pod');

  // Each card opens its own cluster on the tab it was left on.
  await back.click();
  await card('alpha').click();
  await expect(browser.locator('#tabKubeNodes.active')).toBeVisible();
  await expect(browser.locator('#kubeTabContent')).toContainText('alpha-node');
  await expect(browser.locator('#kubeTabContent')).not.toContainText('beta');
  await back.click();
  await card('beta').click();
  await expect(browser.locator('#tabKubePods.active')).toBeVisible();
  await expect(browser.locator('#kubeTabContent')).toContainText('beta-pod');

  // A row menu talks to the open workspace's cluster, and reopening a card did not detect again.
  await browser.locator('//div[@id="kubeTabContent"]//tr[contains(.,"beta-pod")]//button').click();
  await browser.locator('//div[@id="dockerActionMenu"]//*[normalize-space(text())="Describe"]').click();
  await expect(browser.locator('#dockerLogsOutput')).toHaveText('described on beta');
  if (detects.alpha !== 1 || detects.beta !== 1) throw new Error('reopening a workspace detected again: ' + JSON.stringify(detects));

  // The cards are still there after a reload, and a removed one stays removed.
  await browser.reload();
  await initApp(screen, browser);
  await browser.locator('#navItemKube').click();
  await expect(card('alpha')).toBeVisible();
  await expect(card('beta')).toBeVisible();
  await browser.locator('//div[@id="kubeWorkspaceGrid"]//button[contains(@aria-label,"beta")]').click();
  await expect(card('beta')).toBeHidden();
  await browser.reload();
  await initApp(screen, browser);
  await browser.locator('#navItemKube').click();
  await expect(card('alpha')).toBeVisible();
  await expect(card('beta')).toBeHidden();
});
