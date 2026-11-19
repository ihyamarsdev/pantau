import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { deleteHosts, initApp, uniqueHostName } from './helpers.js';

type PageGlobals = {
  loadHosts(): Promise<void>;
  FleetSearch: { open(query?: string): void };
};

// Open the Fleet Search dialog with a query already filled in.
async function openSearch(browser: { evaluate<T, A>(fn: (arg: A) => T, arg: A): Promise<T> }, query: string): Promise<void> {
  await browser.evaluate((q: string) => {
    (window as unknown as PageGlobals).FleetSearch.open(q);
    return null;
  }, query);
}

test('fleet search groups host results by category with deep-link headers', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);

  const name = uniqueHostName('fs-host');
  try {
    await browser.evaluate(async (hostName: string) => {
      await fetch('/api/hosts', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: hostName, host: hostName + '.example.net', port: 1, user: 'x', group_name: 'FleetSearchQA' }),
      });
      await (window as unknown as PageGlobals).loadHosts();
      return null;
    }, name);

    await openSearch(browser, 'host:' + name);
    await expect(browser.locator('#fleetSearchModal')).toBeVisible();
    // The result is grouped with a deep-link header and carries the host name.
    await expect(browser.locator('#fsResults .fs-group').first()).toContainText(name);
    await expect(browser.locator('#fsResults .fs-result-head').first()).toBeVisible();
    await expect(browser.locator('#fsResults .fs-host').first()).toContainText(name);
  } finally {
    await deleteHosts(browser, [name]);
  }
});

test('fleet search applies a folder prefix and shows prefix hints', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);

  const name = uniqueHostName('fs-group');
  try {
    await browser.evaluate(async (hostName: string) => {
      await fetch('/api/hosts', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: hostName, host: hostName + '.example.net', port: 1, user: 'x', group_name: 'FleetSearchQA' }),
      });
      await (window as unknown as PageGlobals).loadHosts();
      return null;
    }, name);

    await openSearch(browser, 'folder:FleetSearchQA');
    await expect(browser.locator('#fleetSearchModal .fs-hint')).toContainText('folder:');
    await expect(browser.locator('#fsResults .fs-group').first()).toContainText('FleetSearchQA');
    // The folder section carries a member count.
    await expect(browser.locator('#fsResults .fs-result-head').first()).toContainText('host');
  } finally {
    await deleteHosts(browser, [name]);
  }
});

test('fleet search supports arrow navigation and opens a host workspace tab', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);

  const name = uniqueHostName('fs-nav');
  try {
    await browser.evaluate(async (hostName: string) => {
      await fetch('/api/hosts', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: hostName, host: hostName + '.example.net', port: 1, user: 'x' }),
      });
      await (window as unknown as PageGlobals).loadHosts();
      return null;
    }, name);

    await openSearch(browser, 'host:' + name);
    await expect(browser.locator('#fsResults .fs-group').first()).toContainText(name);

    // ArrowDown highlights the first result; Enter activates its deep link.
    await browser.locator('#fsQuery').press('ArrowDown');
    await expect(browser.locator('#fsResults .fs-result-head.is-active').first()).toBeVisible();
    await browser.locator('#fsQuery').press('Enter');
    await expect(browser.locator('#hostDetailModal')).toBeVisible();
    await expect(browser.locator('#detailHostName')).toContainText(name);
  } finally {
    await deleteHosts(browser, [name]);
  }
});
