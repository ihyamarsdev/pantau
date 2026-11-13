import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';

// The fleet domain list is answered in the browser, so no host or reverse proxy is needed.
test('domains: a starred domain becomes a dashboard shortcut that survives a reload and can be removed', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);
  await browser.route('**/api/domains', async (route) => {
    await route.fulfill({
      json: {
        hosts: [{ host_id: 1, host_name: 'alpha', group: '', scanned_at: '2026-01-01T00:00:00Z' }],
        sites: [
          { host_id: 1, host_name: 'alpha', domain: 'app.example.com', tls: true, proxy: 'nginx' },
          { host_id: 1, host_name: 'alpha', domain: '*.example.com', wildcard: true, tls: true, proxy: 'nginx' },
        ],
      },
    });
  });
  const star = browser.locator('#domainsAllModal [data-dm-star="app.example.com"]');
  const tile = browser.locator('#domainShortcuts a[href="https://app.example.com"]');
  // No favicon can be loaded (as on an airgapped network): the tile falls back to the first letter.
  await browser.route('https://app.example.com/favicon.ico', (route) => route.abort());

  // Starring a domain in the fleet dialog adds its tile; a wildcard has no address, so no star.
  await browser.evaluate(() => {
    const page = window as unknown as { Domains: { openAll(): void } }; // page global set by /app/domains.js
    page.Domains.openAll();
  });
  await star.click();
  await expect(star).toHaveAttribute('aria-pressed', 'true');
  await expect(browser.locator('#domainsAllModal [data-dm-star="*.example.com"]')).toHaveCount(0);
  await browser.locator('[data-dm-allclose]').click();
  await expect(tile).toContainText('alpha');
  await expect(browser.locator('#domainShortcuts .dm-tile-icon')).toHaveText('A');

  // The tile is still there after a reload, and a removed one stays removed.
  await browser.reload();
  await initApp(screen, browser);
  await expect(tile).toBeVisible();
  await browser.locator('#domainShortcuts [data-dm-star="app.example.com"]').click();
  await expect(tile).toBeHidden();
  await browser.reload();
  await initApp(screen, browser);
  await expect(browser.locator('#hostSearchInput')).toBeVisible();
  await expect(tile).toBeHidden();
});
