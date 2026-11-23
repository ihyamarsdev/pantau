import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { deleteHosts, initApp, uniqueHostName } from './helpers.js';

// The page's own loader, so the dashboard shows the hosts this test added.
type PageGlobals = { loadHosts(): Promise<void> };

test('globe is the default Hosts view, and the view toggle switches between globe, grid and list', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);

  // A fresh dashboard opens on the 3D globe, with the grid and list hidden.
  await expect(browser.locator('#viewGlobeBtn')).toHaveAttribute('class', /\bactive\b/);
  await expect(browser.locator('#hostGlobe')).toBeVisible();
  await expect(browser.locator('#hostList')).toBeHidden();
  await expect(browser.locator('#hostGrid')).toBeHidden();

  // Grid rendering only reads the collapsed-groups set when group dividers are shown, so seed
  // named groups to exercise that path. Use names unique to this test (shared DB) and clean up.
  const names = [uniqueHostName('grid-alpha'), uniqueHostName('grid-beta')];
  try {
    await browser.evaluate(async ({ alpha, beta }: { alpha: string; beta: string }) => {
      const add = (name: string, host: string, group_name: string) =>
        fetch('/api/hosts', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name, host, port: 1, user: 'x', group_name }) });
      await add(alpha, '10.9.0.1', 'GridViewA');
      await add(beta, '10.9.0.2', 'GridViewB');
      const page = window as unknown as PageGlobals;
      await page.loadHosts();
      return null;
    }, { alpha: names[0], beta: names[1] });

    const row = (name: string) => browser.locator(`#hostList [data-host-id]:has-text("${name}")`);
    const card = (name: string) => browser.locator(`#hostGrid [data-host-id]:has-text("${name}")`);

    // Switch to list: rows render even with group dividers present.
    await browser.locator('#viewListBtn').click();
    await expect(browser.locator('#hostList')).toBeVisible();
    await expect(row(names[0])).toHaveCount(1);
    await expect(row(names[1])).toHaveCount(1);
    await expect(browser.locator('#hostGrid')).toBeHidden();

    // Switch to grid: list hides, grid shows the same hosts as cards.
    await browser.locator('#viewGridBtn').click();
    await expect(browser.locator('#hostList')).toBeHidden();
    await expect(browser.locator('#hostGlobe')).toBeHidden();
    await expect(browser.locator('#hostGrid')).toBeVisible();
    await expect(card(names[0])).toHaveCount(1);
    await expect(card(names[1])).toHaveCount(1);

    // Switch back to list: rows render again.
    await browser.locator('#viewListBtn').click();
    await expect(browser.locator('#hostList')).toBeVisible();
    await expect(row(names[0])).toHaveCount(1);
    await expect(row(names[1])).toHaveCount(1);
    await expect(browser.locator('#hostGrid')).toBeHidden();
    await expect(browser.locator('#hostGlobe')).toBeHidden();
  } finally {
    await deleteHosts(browser, names);
  }
});
