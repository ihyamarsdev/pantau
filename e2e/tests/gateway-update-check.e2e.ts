import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';

// The Gateway CLI Update Check (ADR 0080) must be reachable from both gate panels and must show
// what it found.
//
// The upstream release lookup happens on the Pantau server, so the stub is the Pantau API the UI
// calls. The server side of the check (reading PyPI, HashiCorp and OpenTofu) is covered by the Go
// tests in internal/gwupdate.
test('gateway CLI update check shows the installed and latest version in the Ansible gate', async ({ app, screen, browser }) => {
  await app.open('/');

  await initApp(screen, browser);

  await browser.route('**/api/ansible/update-check', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        target: 'ansible',
        installed: '2.15.0',
        latest: '9.9.9',
        status: 'update',
        release_url: 'https://pypi.org/project/ansible-core/9.9.9/',
        checked_at: '2026-11-17T12:00:00Z',
      }),
    });
  });

  await screen.getByRole('tab', 'Ansible').click();
  await browser.locator('#tabAnsibleSettings').click();
  await expect(browser.locator('#ansibleGateBannerCard')).toBeVisible();

  // The check button runs the check and the result appears in the card.
  await browser.locator('[data-st="update-check"]').click();
  const status = browser.locator('#ansibleCliUpdateStatus');
  await expect(status).toHaveText('Update available', { timeout: 15000 });
  const card = browser.locator('#ansibleGateBannerCard');
  await expect(card).toContainText('2.15.0');
  await expect(card).toContainText('9.9.9');
  await expect(browser.locator('#ansibleGateBannerCard a[href="https://pypi.org/project/ansible-core/9.9.9/"]')).toBeVisible();

  // The check only reads: the card says so.
  await expect(card).toContainText('only reads');
});

// A check that could not reach the upstream is shown as failed, never as "up to date".
test('gateway CLI update check reports a failed check as failed', async ({ app, screen, browser }) => {
  await app.open('/');

  await initApp(screen, browser);

  await browser.route('**/api/ansible/update-check', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        target: 'ansible',
        installed: '2.15.0',
        latest: '',
        status: 'error',
        error: 'pypi: unable to reach the release server',
        checked_at: '2026-11-17T12:00:00Z',
      }),
    });
  });

  await screen.getByRole('tab', 'Ansible').click();
  await browser.locator('#tabAnsibleSettings').click();

  await browser.locator('[data-st="update-check"]').click();
  const status = browser.locator('#ansibleCliUpdateStatus');
  await expect(status).toHaveText('Check failed', { timeout: 15000 });

  // It must not claim the CLI is up to date, and it must show why it failed.
  const card = browser.locator('#ansibleGateBannerCard');
  await expect(card).not.toContainText('Up to date');
  await expect(card).toContainText('unable to reach the release server');
});

// The Terraform gate panel carries the same check, one row per installed binary.
test('gateway CLI update check shows a row per binary in the Terraform gate', async ({ app, screen, browser }) => {
  await app.open('/');

  await initApp(screen, browser);

  await browser.route('**/api/terraform/update-check', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        results: [
          { target: 'terraform', installed: '1.15.9', latest: '1.17.2', status: 'update', release_url: 'https://example.com/tf', checked_at: '2026-11-17T12:00:00Z' },
          { target: 'tofu', installed: '1.13.9', latest: '1.13.9', status: 'current', checked_at: '2026-11-17T12:00:00Z' },
        ],
      }),
    });
  });

  await screen.getByRole('tab', 'Terraform').click();
  await browser.locator('#tabTfGate').click();

  const card = browser.locator('#tfCliUpdateCard');
  await expect(card).toBeVisible({ timeout: 15000 });

  // The check button runs the check and each binary gets its own row.
  await browser.locator('#tfCliUpdateCheck').click();
  await expect(card).toContainText('terraform', { timeout: 15000 });
  await expect(card).toContainText('tofu');
  await expect(card).toContainText('1.17.2');
  await expect(card).toContainText('Update available');
  await expect(card).toContainText('Up to date');
  // The check only reads: the card says so.
  await expect(card).toContainText('only reads');
});

// The update action (ADR 0081) must be reachable from the gate panel: with a newer release found,
// the button appears, runs the install and shows the steps as they arrive.
test('gateway CLI update installs from the Ansible gate and shows the steps', async ({ app, screen, browser }) => {
  await app.open('/');

  await initApp(screen, browser);

  await browser.route('**/api/ansible/update-check', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        target: 'ansible',
        installed: '2.15.0',
        latest: '9.9.9',
        status: 'update',
        checked_at: '2026-11-17T12:00:00Z',
      }),
    });
  });

  // The install answers NDJSON: the steps, then a done marker.
  await browser.route('**/api/ansible/install', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/x-ndjson',
      body: [
        JSON.stringify({ log: 'Downloading the ansible-core wheel…\n' }),
        JSON.stringify({ log: 'Checksum verified (abc123…)\n' }),
        JSON.stringify({ log: 'Installed ansible-core 9.9.9\n' }),
        JSON.stringify({ done: true }),
      ].join('\n') + '\n',
    });
  });

  await screen.getByRole('tab', 'Ansible').click();
  await browser.locator('#tabAnsibleSettings').click();
  await browser.locator('[data-st="update-check"]').click();

  // A newer release was found, so the update button appears.
  const updateBtn = browser.locator('[data-st="update-install"]');
  await expect(updateBtn).toBeVisible({ timeout: 15000 });

  await updateBtn.click();

  // The install log shows the steps that streamed in.
  const log = browser.locator('#ansibleCliUpdateLog');
  await expect(log).toBeVisible({ timeout: 15000 });
  await expect(log).toContainText('Checksum verified');
  await expect(log).toContainText('Installed ansible-core 9.9.9');
});
