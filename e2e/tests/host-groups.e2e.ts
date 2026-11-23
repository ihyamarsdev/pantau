import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { deleteHosts, initApp } from './helpers.js';

// The page's own loader, so the dashboard shows the hosts this test added.
type PageGlobals = { loadHosts(): Promise<void> };
test('a host group is renamed from its header and all of its hosts follow', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);

  // The whole run shares one database, so the hosts this test adds are removed again: a host left
  // behind would add a group to the dashboard and break the count below (and the next test's).
  const names = ['alpha', 'beta', 'gamma'];
  try {
    await browser.evaluate(async () => {
      const add = (name: string, host: string, group_name: string) =>
        fetch('/api/hosts', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name, host, port: 1, user: 'x', group_name }) });
      await add('alpha', '10.0.0.1', 'Torcons');
      await add('beta', '10.0.0.2', 'Torcons');
      await add('gamma', '10.0.0.3', 'Staging');
      const page = window as unknown as PageGlobals;
      await page.loadHosts();
      return null;
    });

    // The groups this test added are counted, not every group on the dashboard: another test's host
    // may share the database and bring a group of its own.
    await expect(browser.locator('.host-group-divider:has-text("Torcons")')).toHaveCount(1);
    await expect(browser.locator('.host-group-divider:has-text("Staging")')).toHaveCount(1);
    await expect(browser.locator('.host-group-divider:has-text("Torcons")')).toContainText('2');

    await browser.locator('.host-group-divider:has-text("Torcons") .host-group-edit').click();
    await expect(browser.locator('#hostGroupRenameHint')).toContainText('2 hosts');
    await browser.locator('#hostGroupNewName').fill('Production');
    await browser.locator('#hostGroupSaveBtn').click();

    await expect(browser.locator('.host-group-divider:has-text("Production")')).toContainText('2');
    await expect(browser.locator('.host-group-divider:has-text("Torcons")')).toHaveCount(0);
    await expect(browser.locator('.host-group-divider:has-text("Staging")')).toContainText('1');
    // Only the hosts this test added are checked: another test's host may share the database.
    const groups = (await browser.evaluate(async () => (await (await fetch('/api/hosts')).json()).filter((h: { name: string }) => ['alpha', 'beta', 'gamma'].includes(h.name)).map((h: { name: string; group_name: string }) => h.name + ':' + h.group_name).sort())) as string[];
    if (groups.join(',') !== 'alpha:Production,beta:Production,gamma:Staging') throw new Error('stored groups: ' + groups.join(','));
  } finally {
    await deleteHosts(browser, names);
  }
});

test('a host group collapses from its header, and stays collapsed after a reload', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);
  // The whole run shares one database, so the hosts this test adds are removed again.
  const names = ['delta', 'epsilon', 'zeta'];
  try {
    await browser.evaluate(async () => {
      const add = (name: string, host: string, group_name: string) =>
        fetch('/api/hosts', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name, host, port: 1, user: 'x', group_name }) });
      await add('delta', '10.0.0.4', 'Edge');
      await add('epsilon', '10.0.0.5', 'Edge');
      await add('zeta', '10.0.0.6', 'Staging');
      const page = window as unknown as PageGlobals;
      await page.loadHosts();
      return null;
    });

    const edge = browser.locator('.host-group-divider:has-text("Edge")');
    const card = (name: string) => browser.locator(`[data-host-id]:has-text("${name}")`);
    await expect(card('delta')).toHaveCount(1);

    await edge.click();
    await expect(card('delta')).toHaveCount(0);
    await expect(card('epsilon')).toHaveCount(0);
    await expect(card('zeta')).toHaveCount(1);
    await expect(edge).toHaveCount(1);

    await edge.click();
    await expect(card('delta')).toHaveCount(1);
    await expect(card('epsilon')).toHaveCount(1);

    await edge.click();
    await expect(card('delta')).toHaveCount(0);
    await browser.reload();
    await expect(card('zeta')).toHaveCount(1);
    await expect(card('delta')).toHaveCount(0);
    await expect(card('epsilon')).toHaveCount(0);
    await expect(edge).toHaveCount(1);
  } finally {
    await deleteHosts(browser, names);
  }
});

test('deleting a host group from its header removes every host in it and leaves other groups', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);
  // The whole run shares one database, so any host the test leaves behind is removed again.
  const names = ['eta', 'theta', 'iota'];
  try {
    await browser.evaluate(async () => {
      const add = (name: string, host: string, group_name: string) =>
        fetch('/api/hosts', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name, host, port: 1, user: 'x', group_name }) });
      await add('eta', '10.0.0.7', 'Doomed');
      await add('theta', '10.0.0.8', 'Doomed');
      await add('iota', '10.0.0.9', 'Keeper');
      const page = window as unknown as PageGlobals;
      await page.loadHosts();
      return null;
    });

    const doomed = browser.locator('.host-group-divider:has-text("Doomed")');
    await expect(doomed).toContainText('2');
    await doomed.locator('.host-group-delete').click();
    await expect(browser.locator('#confirmModal')).toContainText('Doomed');
    await browser.locator('#confirmModalBtn').click();

    await expect(doomed).toHaveCount(0);
    await expect(browser.locator('.host-group-divider:has-text("Keeper")')).toContainText('1');
    const left = (await browser.evaluate(async () => (await (await fetch('/api/hosts')).json()).filter((h: { name: string }) => ['eta', 'theta', 'iota'].includes(h.name)).map((h: { name: string }) => h.name).sort())) as string[];
    if (left.join(',') !== 'iota') throw new Error('hosts left: ' + left.join(','));
  } finally {
    await deleteHosts(browser, names);
  }
});
