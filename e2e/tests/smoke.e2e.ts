import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';

test('app opens, starts fresh, logs in and shows the dashboard', async ({ app, screen, browser }) => {
  await app.open('/');

  await initApp(screen, browser);

  await expect(screen.getByRole('button', 'Logout')).toBeVisible();
  await expect(screen.getByRole('tab', 'Ansible')).toBeVisible();
});
