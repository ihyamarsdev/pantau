import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';

test('a host group is renamed from its header and all of its hosts follow', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);
  await browser.evaluate(async () => {
    const add = (name: string, host: string, group_name: string) =>
      fetch('/api/hosts', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name, host, port: 1, user: 'x', group_name }) });
    await add('alpha', '10.0.0.1', 'Torcons');
    await add('beta', '10.0.0.2', 'Torcons');
    await add('gamma', '10.0.0.3', 'Staging');
    await (window as unknown as { loadHosts(): Promise<void> }).loadHosts();
    return null;
  });

  const dividers = browser.locator('.host-group-divider');
  await expect(dividers).toHaveCount(2);
  await expect(browser.locator('.host-group-divider:has-text("Torcons")')).toContainText('2');

  await browser.locator('.host-group-divider:has-text("Torcons") .host-group-edit').click();
  await expect(browser.locator('#hostGroupRenameHint')).toContainText('2 hosts');
  await browser.locator('#hostGroupNewName').fill('Production');
  await browser.locator('#hostGroupSaveBtn').click();

  await expect(browser.locator('.host-group-divider:has-text("Production")')).toContainText('2');
  await expect(browser.locator('.host-group-divider:has-text("Torcons")')).toHaveCount(0);
  await expect(browser.locator('.host-group-divider:has-text("Staging")')).toContainText('1');
  const groups = (await browser.evaluate(async () => (await (await fetch('/api/hosts')).json()).map((h: { name: string; group_name: string }) => h.name + ':' + h.group_name).sort())) as string[];
  if (groups.join(',') !== 'alpha:Production,beta:Production,gamma:Staging') throw new Error('stored groups: ' + groups.join(','));
});
