import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';

// The Teleport CLI (tsh) card at the top of the Teleport Profiles drawer. Every Teleport and release call is answered
// in the browser, so no tsh, no proxy and no internet access are needed.

type CliState = { version: string; source: string };

// stubCLI answers the profile list, the profile status, the kube cluster list and the tsh card's API. The
// callbacks let one test change what the server says between calls.
async function stubCLI(
  browser: any,
  opts: {
    tsh?: CliState | null;
    check?: Record<string, unknown>;
    install?: (route: any) => Promise<void>;
  } = {},
) {
  let tsh: CliState | null = opts.tsh === undefined ? { version: '18.10.0', source: 'managed' } : opts.tsh;
  const profiles = [
    {
      id: 1, name: 'prod', proxy: 'teleport.example.test:443', auth_connector: 'local', username: 'alice',
      teleport_cluster: 'tc', teleport_home: '', default_policy: 'read-only',
    },
  ];
  await browser.route('**/api/kube/clusters', async (route) => {
    await route.fulfill({ json: { ok: true, clusters: [], host_clusters: [] } });
  });
  await browser.route('**/api/teleport/profiles', async (route) => {
    await route.fulfill({ json: { profiles, tsh: { installed: !!tsh, path: '/home/ihya/project/pantau/data/cli/tsh' } } });
  });
  await browser.route('**/api/teleport/profiles/1/status', async (route) => {
    await route.fulfill({ json: { profile_id: 1, active: true, username: 'alice', ttl_seconds: 7200, expired: false } });
  });
  await browser.route('**/api/teleport/cli/status', async (route) => {
    await route.fulfill({
      json: {
        ok: true,
        tsh: tsh
          ? { installed: true, source: tsh.source, path: '/home/ihya/project/pantau/data/cli/tsh', version: tsh.version, os: 'linux', arch: 'amd64', override_active: false }
          : { installed: false, source: 'missing', os: 'linux', arch: 'amd64', override_active: false },
        kubectl: { installed: true, version: '1.31.4', source: 'system', path: '/usr/bin/kubectl' },
      },
    });
  });
  await browser.route('**/api/teleport/cli/check-update', async (route) => {
    await route.fulfill({
      json: opts.check ?? {
        ok: true, status: 'update_available', installed: '18.10.0', latest: '18.11.10',
        release_url: 'https://github.com/gravitational/teleport/releases/tag/v18.11.10', cached: false,
        checked_at: '2026-11-27T00:00:00Z', major_version_mismatch: false,
        proxy: { profile_id: 1, address: 'teleport.example.test:443', version: '18.11.10' },
      },
    });
  });
  await browser.route('**/api/teleport/cli/install', async (route) => {
    if (opts.install) {
      await opts.install(route);
      return;
    }
    tsh = { version: '18.11.10', source: 'managed' };
    await route.fulfill({
      status: 200,
      contentType: 'application/x-ndjson',
      body: '{"log":"Downloading the checksum of teleport…\\n"}\n{"log":"Checksum verified\\n"}\n{"log":"Installed tsh 18.11.10\\n"}\n{"done":true}\n',
    });
  });
}

// openProfiles signs in, opens the Kubernetes section and the Teleport Profiles drawer.
async function openProfiles(app: any, screen: any, browser: any) {
  await app.open('/');
  await initApp(screen, browser);
  await browser.locator('#navItemKube').click();
  await expect(browser.locator('#tpSwitcher')).toBeVisible();
  await browser.locator('[data-tp="profiles"]').first().click();
  await expect(browser.locator('#tpCliCard')).toBeVisible();
}

test('teleport cli card: shows the installed tsh, its source and an update available', async ({ app, screen, browser }) => {
  await stubCLI(browser);
  await openProfiles(app, screen, browser);

  const card = browser.locator('#tpCliCard');
  await expect(card).toContainText('Teleport CLI (tsh)');
  await expect(card).toContainText('18.10.0');
  await expect(card).toContainText('Managed');

  await browser.locator('[data-tp="cli-check"]').click();
  await expect(card).toContainText('Update available: 18.10.0 → 18.11.10');
  await expect(browser.locator('#tpCliCard [data-tp="cli-install"]')).toBeVisible();
});

test('teleport cli card: a failed check is shown as check failed, never as up to date', async ({ app, screen, browser }) => {
  await stubCLI(browser, {
    check: { ok: true, status: 'check_failed', installed: '18.10.0', error: 'teleport releases: dial timeout', cached: false, checked_at: '2026-11-27T00:00:00Z', major_version_mismatch: false },
  });
  await openProfiles(app, screen, browser);

  await browser.locator('[data-tp="cli-check"]').click();
  const card = browser.locator('#tpCliCard');
  await expect(card).toContainText('Check failed: teleport releases: dial timeout');
  await expect(card).not.toContainText('Up to date');
});

test('teleport cli card: an install streams its log and then shows the new version', async ({ app, screen, browser }) => {
  await stubCLI(browser);
  await openProfiles(app, screen, browser);

  await browser.locator('[data-tp="cli-check"]').click();
  await expect(browser.locator('#tpCliCard')).toContainText('Update available');
  await browser.locator('#tpCliCard [data-tp="cli-install"]').click();

  await expect(browser.locator('#tpCliLog')).toContainText('Installed tsh 18.11.10');
  await expect(browser.locator('#tpCliCard')).toContainText('18.11.10');
});

test('teleport cli card: a second install while one runs shows the busy message', async ({ app, screen, browser }) => {
  await stubCLI(browser, {
    install: async (route) => {
      await route.fulfill({ status: 409, json: { ok: false, error: 'a tsh install is already running', code: 'INSTALL_IN_PROGRESS' } });
    },
  });
  await openProfiles(app, screen, browser);

  await browser.locator('[data-tp="cli-check"]').click();
  await expect(browser.locator('#tpCliCard')).toContainText('Update available');
  await browser.locator('#tpCliCard [data-tp="cli-install"]').click();

  await expect(browser.locator('#tpCliLog')).toContainText('The install failed');
  await expect(browser.locator('#tpCliLog')).toContainText('An install is already running');
});
