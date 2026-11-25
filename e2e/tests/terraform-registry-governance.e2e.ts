import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';
import { startFakeRegistry } from './registry-fake.js';
import {
  apiCall,
  enableTerraformGate,
  getCodeMirrorValue,
  openTerraformSection,
  openWorkspaceDrawer,
  seedWorkspace,
  setCodeMirrorValue,
  stubTerraformStatus,
  switchDrawerTab,
  switchTfTab,
} from './terraform-helpers.js';

const AWS_SECRET = 'sekret-value-e2e-123';

test('Variables & Cloud tab manages Variable Sets and Cloud Accounts without exposing secrets', async (fx) => {
  const { browser } = fx;
  await openTerraformSection(fx, true);
  await enableTerraformGate(browser);

  // Stub live AWS verifier & Kubernetes importable list (no external cloud network or kubectl binary needed).
  await browser.route('**/api/terraform/cloud-accounts/*/test', async (route) => {
    await route.fulfill({ json: { ok: true, identity: 'arn:aws:iam::123456789012:user/e2e' } });
  });
  await browser.route('**/api/terraform/cloud-accounts/importable', async (route) => {
    await route.fulfill({ json: { cloudflare: [], kubernetes: [{ id: 99, name: 'e2e-tf-kube', usable: true }] } });
  });

  await switchTfTab(browser, 'variables');

  // Variable Sets segmented view.
  await browser.locator('#tfPanel-variables [data-seg="sets"]').click();
  await expect(browser.locator('#btnTfNewVarset')).toBeVisible();

  // Cloud Accounts segmented view: create AWS account without leaking secret.
  await browser.locator('#tfPanel-variables [data-seg="cloud"]').click();
  await browser.locator('#btnTfCloudNew').click();
  await browser.locator('#tfCaProvider').selectOption('Amazon Web Services');
  await browser.locator('#tfCaName').fill('aws-revamp-e2e');
  await browser.locator('#tfCaF_access_key_id').fill('AKIAE2EREVAMP');
  await browser.locator('#tfCaF_secret_access_key').fill(AWS_SECRET);
  await browser.locator('#tfCaSave').click();

  await expect(browser.locator('.tf-ca-card').filter({ hasText: 'aws-revamp-e2e' })).toBeVisible();
  await expect(browser.locator('#tfCloudRoot')).not.toContainText(AWS_SECRET);

  // Import existing Kubernetes cluster option hides the manual kubeconfig textarea.
  await browser.locator('#btnTfCloudNew').click();
  await browser.locator('#tfCaProvider').selectOption('Kubernetes');
  await expect(browser.locator('#tfCaImport')).toBeVisible();
  await browser.locator('#tfCaImport').selectOption({ label: 'e2e-tf-kube' });
  await expect(browser.locator('#tfCaF_kubeconfig')).toBeHidden();
  await browser.keyboard.press('Escape');
});

test('Modules & Registry tab searches public registry safely, inserts snippets, and checks versions', async (fx) => {
  const { browser } = fx;
  const registry = await startFakeRegistry();
  try {
    await fx.app.open('/');
    await initApp(fx.screen, browser);
    await enableTerraformGate(browser);
    await apiCall(browser, 'PUT', '/api/terraform/registry/settings', { base_url: registry.url });
    await seedWorkspace(browser, 'e2e-reg-revamp', {
      files: {
        'main.tf': 'module "vpc_old" {\n  source  = "acme/vpc/aws"\n  version = "1.0.0"\n}\n',
      },
    });
    await stubTerraformStatus(browser, true);

    await fx.screen.getByRole('tab', 'Terraform').click();
    await switchTfTab(browser, 'registry');

    // Segmented switcher between Private Catalog and Public Registry.
    await expect(browser.locator('[data-reg-seg="private"]')).toBeVisible();
    await browser.locator('[data-reg-seg="public"]').click();
    await browser.locator('#tfRegQuery').fill('vpc');
    await browser.locator('#tfRegSearch').click();
    await browser.locator('[data-reg-item="acme/vpc/aws"]').click();
    await expect(browser.locator('#tfRegInputs')).toContainText('cidr');

    // Untrusted README HTML is escaped safely.
    await browser.locator('[data-reg-tab="readme"]').click();
    const pwned = await browser.evaluate(() => Reflect.get(window, 'pwned') === true);
    expect(pwned).toBe(false);

    // Configuration integration: Check Versions + Insert Snippet.
    await openWorkspaceDrawer(fx, 'e2e-reg-revamp');
    await switchDrawerTab(browser, 'config');
    await browser.locator('#tfCfgRegCheck').click();
    const regRow = browser.locator('#tfCfgRegStatus .tf-cfg-regc-row').filter({ hasText: 'vpc_old' });
    await expect(regRow).toContainText('1.2.0');
    await browser.locator('#tfCfgRegStatus .tf-cfg-regc-row [data-regc-apply]').click();
    expect(await getCodeMirrorValue(browser)).toContain('version = "~> 1.2"');

    await setCodeMirrorValue(browser, '# empty\n');
    await browser.locator('#tfCfgRegistry').click();
    await browser.locator('#tfRegQuery').fill('vpc');
    await browser.locator('#tfRegSearch').click();
    await browser.locator('[data-reg-item="acme/vpc/aws"]').click();
    await browser.locator('#tfRegInsert').click();
    expect(await getCodeMirrorValue(browser)).toContain('source  = "acme/vpc/aws"');
  } finally {
    await registry.close();
  }
});

test('Governance & Settings tab exposes Policies, Audit Trail, and Gate/CLI/Alerts/Registry URL', async (fx) => {
  const { browser } = fx;
  await openTerraformSection(fx, true);
  await enableTerraformGate(browser);

  await switchTfTab(browser, 'governance');

  // Policies segmented view.
  await browser.locator('#tfPanel-governance [data-seg="policies"]').click();
  await expect(browser.locator('#btnTfNewPolicy')).toBeVisible();

  // Audit Trail segmented view.
  await browser.locator('#tfPanel-governance [data-seg="audit"]').click();
  await expect(browser.locator('#tfAuditList')).toBeVisible();

  // Gate, CLI & Alerts segmented view (non-monkey-patched unified cards).
  await browser.locator('#tfPanel-governance [data-seg="gate"]').click();
  await expect(browser.locator('#tfCliUpdateCard')).toBeVisible();
  await expect(browser.locator('#tfNotifyCard')).toBeVisible();
  await expect(browser.locator('#tfRegSettingsCard')).toBeVisible();

  await browser.locator('#tfNotifyCard [data-nf="applied"]').check();
  await browser.locator('#tfNfSave').click();
  await expect.poll(async () => JSON.parse((await apiCall(browser, 'GET', '/api/terraform/notifications')).body).events.applied).toBe(true);
});
