import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';

// Runs against the real /api/shortcuts, so it also proves the list is stored on the server.
test('shortcuts: a custom shortcut is added to its group, survives a reload, and can be edited and deleted', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);
  const section = browser.locator('#sectionShortcuts');
  const tile = browser.locator('#sectionShortcuts a[href="https://grafana.example.com"]');
  const modal = browser.locator('#scModal');

  // A bare domain is saved as an https address.
  await browser.locator('#navItemShortcuts').click();
  await browser.locator('[data-sc-add]').click();
  await browser.locator('#scName').fill('Grafana');
  await browser.locator('#scUrl').fill('grafana.example.com');
  await browser.locator('#scGroup').fill('Monitoring');
  await browser.locator('#scSave').click();
  await expect(tile).toContainText('Grafana');
  await expect(section).toContainText('Monitoring');

  // An address that is not a web address is refused and the dialog stays open.
  await browser.locator('[data-sc-add]').click();
  await browser.locator('#scName').fill('Files');
  await browser.locator('#scUrl').fill('ftp://files.example.com');
  await browser.locator('#scSave').click();
  await expect(browser.locator('#toastContainer')).toContainText('http:// or https://');
  await expect(modal).toBeVisible();
  await screen.getByRole('button', 'Cancel').click();
  await expect(section).not.toContainText('Files');

  // The shortcut is stored on the server: still there after a reload.
  await browser.reload();
  await initApp(screen, browser);
  await browser.locator('#navItemShortcuts').click();
  await expect(tile).toBeVisible();

  // An edited tile shows the favicon of its new site. The app answers on localhost too, which is another origin
  // than 127.0.0.1, so the image only loads when the page's CSP allows images from other sites.
  const moved = browser.locator('#sectionShortcuts a[href="http://localhost:18199/"]');
  await browser.locator('[data-sc-edit="0"]').click();
  await browser.locator('#scName').fill('Grafana Prod');
  await browser.locator('#scUrl').fill('http://localhost:18199/');
  await browser.locator('#scSave').click();
  await expect(moved).toContainText('Grafana Prod');
  await expect(browser.locator('#sectionShortcuts img[src="http://localhost:18199/favicon.ico"]')).toBeVisible();

  await browser.locator('[data-sc-del="0"]').click();
  await browser.locator('#confirmModalBtn').click();
  await expect(moved).toBeHidden();
  await browser.reload();
  await initApp(screen, browser);
  await browser.locator('#navItemShortcuts').click();
  await expect(browser.locator('[data-sc-add]')).toBeVisible();
  await expect(moved).toBeHidden();
});
