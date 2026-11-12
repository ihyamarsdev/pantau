import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';

test('Laya sidecar settings render and round-trip', async ({ app, screen, browser }) => {
  await app.open('/');

  await initApp(screen, browser);

  await screen.getByRole('button', 'Settings').click();
  const settingsModal = browser.locator('#settingsModal');
  await settingsModal.waitFor({ state: 'visible', timeout: 10000 });

  // The Notifications tab holds the Laya fields.
  await browser.locator('#tabSettingsNotif').click();

  const layaUrl = browser.locator('#settingLayaUrl');
  const layaTimeout = browser.locator('#settingLayaTimeout');
  await layaUrl.waitFor({ state: 'visible', timeout: 10000 });
  await expect(layaUrl).toBeVisible();
  await expect(layaTimeout).toBeVisible();

  await layaUrl.fill('http://laya:8000');
  await layaTimeout.fill('20000');
  await screen.getByRole('button', 'Save Notification Settings').click();

  await app.open('/');
  await initApp(screen, browser);
  await screen.getByRole('button', 'Settings').click();
  await settingsModal.waitFor({ state: 'visible', timeout: 10000 });
  await browser.locator('#tabSettingsNotif').click();

  await expect(layaUrl).toHaveValue('http://laya:8000');
  await expect(layaTimeout).toHaveValue('20000');
});
