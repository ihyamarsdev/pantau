import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';
import {
  apiCall,
  openWorkspaceDrawer,
  seedWorkspace,
  stubTerraformStatus,
  switchDrawerTab,
} from './terraform-helpers.js';

const VARS_TF = [
  'variable "region" {',
  '  type    = string',
  '  default = "eu-west-1"',
  '}',
  '',
  'variable "api_token" {',
  '  type      = string',
  '  sensitive = true',
  '}',
  '',
].join('\n');

const SEED_TFSTATE = JSON.stringify({
  version: 4,
  terraform_version: '1.10.0',
  serial: 1,
  lineage: 'e2e-lineage-01',
  outputs: {
    greeting: { value: 'hello-from-a', type: 'string' },
  },
  resources: [
    {
      mode: 'managed',
      type: 'terraform_data',
      name: 'x',
      provider: 'provider["terraform.io/builtin/terraform"]',
      instances: [{ schema_version: 0, attributes: { id: '1', input: 'e2e', output: 'e2e' } }],
    },
  ],
});

test('State tab manages resources (rm/mv), lock card, and explicit From/To state version diff', async (fx) => {
  const { browser } = fx;
  await fx.app.open('/');
  await initApp(fx.screen, browser);
  const wsId = await seedWorkspace(browser, 'e2e-state-ops');

  // Import a real state v4 snapshot into SQLite (no terraform CLI needed).
  const imp = await apiCall(browser, 'POST', `/api/terraform/workspaces/${wsId}/state/import`, { state: SEED_TFSTATE });
  expect(imp.status).toBeLessThan(300);

  // Stub state/ops (CLI execution) & state/diff so UI flow can be verified without terraform binary in PATH.
  let removed = false;
  await browser.route(`**/api/terraform/workspaces/${wsId}/state**`, async (route) => {
    const req = route.request;
    const url = new URL(req.url);
    if (url.pathname.endsWith('/state/ops') && req.method === 'POST') {
      removed = true;
      await route.fulfill({ json: { ok: true, state_version: 2, output: 'Removed terraform_data.x' } });
      return;
    }
    if (url.pathname.endsWith('/state') && removed) {
      await route.fulfill({ json: { serial: 2, lineage: 'e2e-lineage-01', terraform_version: '1.10.0', resources: [] } });
      return;
    }
    if (url.pathname.endsWith('/state/versions') && removed) {
      await route.fulfill({
        json: {
          versions: [
            { id: 2, serial: 2, source: 'state rm terraform_data.x', resource_count: 0, size: 180, sha256: 'bbbb', created_at: '2026-11-25T07:05:00Z' },
            { id: 1, serial: 1, source: 'import', resource_count: 1, size: 320, sha256: 'aaaa', created_at: '2026-11-25T07:00:00Z' },
          ],
        },
      });
      return;
    }
    if (url.pathname.endsWith('/state/diff')) {
      await route.fulfill({ json: { added: [], removed: ['terraform_data.x'], changed: [] } });
      return;
    }
    await route.continue();
  });

  await stubTerraformStatus(browser, true);
  await fx.screen.getByRole('tab', 'Terraform').click();
  await browser.locator('#btnTfRefresh').click();
  await browser.locator('#tfWsTableBody tr[data-ws-id], #tfWsGrid .tf-ws-card').filter({ hasText: 'e2e-state-ops' }).first().click();
  await expect(browser.locator('#tfDrawerRoot')).toBeVisible();

  await switchDrawerTab(browser, 'state');
  await expect(browser.locator('#tfStateLockCard')).toBeVisible();
  await expect(browser.locator('[data-st-act="seg"][data-seg="resources"]')).toHaveAttribute('class', /active/);

  // Row action sits cleanly outside any <summary> toggle.
  await browser.locator('[data-st-act="rm"][data-addr="terraform_data.x"]').click();
  await expect(browser.locator('#confirmModal')).toContainText('state rm terraform_data.x');
  await browser.locator('#confirmModalBtn').click();
  await expect(browser.locator('[data-st-act="rm"][data-addr="terraform_data.x"]')).toHaveCount(0);

  // Switch to Versions & Diff sub-tab and compare From -> To versions.
  await browser.locator('[data-st-act="seg"][data-seg="versions"]').click();
  await expect(browser.locator('#tfStateFrom')).toBeVisible();
  await expect(browser.locator('#tfStateTo')).toBeVisible();
  await browser.locator('#tfStateCompareBtn').click();
  await expect(browser.locator('#tfStateDiffOut')).toContainText('terraform_data.x');
});

test('Variables tab detects variables, toggles Effective Resolution, and configures Linked Outputs', async (fx) => {
  const { browser } = fx;
  await fx.app.open('/');
  await initApp(fx.screen, browser);

  const srcId = await seedWorkspace(browser, 'e2e-out-src');
  const imp = await apiCall(browser, 'POST', `/api/terraform/workspaces/${srcId}/state/import`, { state: SEED_TFSTATE });
  expect(imp.status).toBeLessThan(300);

  const consumerTf = VARS_TF + '\nvariable "greeting" {\n  type = string\n}\n\noutput "echo" {\n  value = var.greeting\n}\n';
  const consumerId = await openWorkspaceDrawer(fx, 'e2e-vars-consumer', { files: { 'main.tf': consumerTf } });

  await switchDrawerTab(browser, 'variables');

  // Detect variables from configuration.
  await browser.locator('#tfVarDetectBtn').click();
  await expect(browser.locator('#tfVarDetectList [data-vd="region"]')).toBeVisible();
  await expect(browser.locator('#tfVarDetectList [data-vd="api_token"]')).toBeVisible();
  await browser.locator('#tfVarDetectList [data-vd="region"] [data-vd-on]').check();
  await browser.locator('#tfVarDetectAdd').click();
  await expect(browser.locator('#tfVarDetectList')).toBeHidden();

  // Effective Resolution toggle works.
  await browser.locator('[data-wsv-mode="eff"]').click();
  await expect(browser.locator('#tfVarsTable')).toContainText('region');

  // Linked Outputs sub-tab injects source output and prevents cycles.
  await browser.locator('[data-wsv-sub="links"]').click();
  await browser.locator('#tfLkAdd').click();
  await browser.locator('#tfLkRows [data-lk-var]').last().fill('greeting');
  await browser.locator('#tfLkRows [data-lk-src]').last().selectOption({ label: 'e2e-out-src' });
  await browser.locator('#tfLkRows [data-lk-out]').last().selectOption({ label: 'greeting' });
  await browser.locator('#tfLkSave').click();

  await expect.poll(async () => {
    const listed = JSON.parse((await apiCall(browser, 'GET', `/api/terraform/workspaces/${consumerId}/output-links`)).body);
    return listed.links?.[0]?.available;
  }).toBe(true);

  const cycle = await apiCall(browser, 'PUT', `/api/terraform/workspaces/${srcId}/output-links`, {
    links: [{ var_name: 'x', source_workspace_id: consumerId, output_name: 'echo' }],
  });
  expect(cycle.status).toBe(400);
});
