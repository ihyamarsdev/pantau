import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { deleteHosts, initApp, uniqueHostName } from './helpers.js';

// The page's own loader, so the dashboard shows the hosts this test added.
type PageGlobals = { loadHosts(): Promise<void> };

test('grid view renders host cards for grouped hosts, and the mode toggle switches both ways', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);

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

    const card = (name: string) => browser.locator(`#hostGrid [data-host-id]:has-text("${name}")`);

    // Grid is the default mode. The cards must render even with group dividers present.
    await expect(browser.locator('#hostGrid')).toBeVisible();
    await expect(card(names[0])).toHaveCount(1);
    await expect(card(names[1])).toHaveCount(1);

    // Switch to list: grid hides, list shows the same hosts.
    await browser.locator('#viewListBtn').click();
    await expect(browser.locator('#hostGrid')).toBeHidden();
    await expect(browser.locator('#hostList')).toBeVisible();
    await expect(browser.locator(`#hostList [data-host-id]:has-text("${names[0]}")`)).toHaveCount(1);

    // Switch back to grid: cards render again.
    await browser.locator('#viewGridBtn').click();
    await expect(browser.locator('#hostGrid')).toBeVisible();
    await expect(card(names[0])).toHaveCount(1);
    await expect(card(names[1])).toHaveCount(1);
    await expect(browser.locator('#hostList')).toBeHidden();
  } finally {
    await deleteHosts(browser, names);
  }
});
