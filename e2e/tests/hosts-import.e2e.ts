import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';

// The SSH Key Library and the import endpoint are answered in the browser: this checks the dialog sends the chosen key.
test('importing hosts sends the SSH key chosen for them', async ({ app, screen, browser }) => {
  let sent: Record<string, unknown> | undefined;
  await browser.route('**/api/ssh-keys', async (route) => {
    await route.fulfill({ json: [{ id: 7, name: 'deploy-key', fingerprint: 'SHA256:x', public_key: 'ssh-ed25519 AAAA' }] });
  });
  await browser.route('**/api/hosts/import', async (route) => {
    sent = JSON.parse(route.request.postData ?? '{}') as Record<string, unknown>;
    await route.fulfill({ json: { created: [{ id: 501, name: 'web1', host: '10.9.0.1', port: 22, user: 'root' }], skipped: [] } });
  });
  await browser.route('**/api/hosts/501/inspect', async (route) => {
    await route.fulfill({ json: { ok: true } });
  });
  await app.open('/');
  await initApp(screen, browser);

  await browser.evaluate(() => { (window as unknown as { openImportHostsModal(): void }).openImportHostsModal(); return null; });
  await browser.locator('#importConfigText').fill('Host web1\n    HostName 10.9.0.1\n    User root\n    IdentityFile ~/.ssh/web1\n');
  await browser.locator('#importStepSelect').getByText('Parse').click();
  // A host with its own IdentityFile is told to pick that key below.
  await expect(browser.locator('#importPreviewBody')).toContainText('choose that key below');
  await browser.locator('#importKeyId').selectOption('deploy-key');
  await browser.locator('#importSubmitBtn').click();
  await expect(browser.locator('#importStepGroups')).toBeVisible();
  if (JSON.stringify(sent?.key_id) !== '7') throw new Error('import did not send the chosen key: ' + JSON.stringify(sent));
});
