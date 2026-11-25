import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';
import {
  enableTerraformGate,
  openTerraformSection,
  openWorkspaceDrawer,
  seedWorkspace,
  stubTerraformStatus,
  switchDrawerTab,
  switchTfTab,
} from './terraform-helpers.js';

test('terraform gate blocks while off, navigates to Gate settings, enables cleanly, and renders the 5-tab shell', async (fx) => {
  const { browser } = fx;
  await openTerraformSection(fx, false);

  await expect(browser.locator('#tfGateBanner')).toBeVisible();
  await expect(browser.locator('#tfTabs [data-tab]')).toHaveCount(5);

  // Clicking "Open Gate Settings" on the gate card jumps to Governance -> Gate, CLI & Alerts.
  await browser.locator('#tfGateBanner [data-tf="goto-gate"]').click();
  await expect(browser.locator('#tfTabs [data-tab="governance"]')).toHaveAttribute('aria-selected', 'true');
  await expect(browser.locator('#tfGateCard')).toBeVisible();

  // Enable Gate via #tfGateAction + confirmation dialog.
  await browser.locator('#tfGateAction').click();
  await expect(browser.locator('#confirmModalBtn')).toBeVisible();
  await browser.locator('#confirmModalBtn').click();
  await expect(browser.locator('#tfGateStatus')).toContainText(/Active/i);

  // Returning to Workspaces now renders the workspace toolbar instead of the gate banner.
  await switchTfTab(browser, 'workspaces');
  await expect(browser.locator('#tfGateBanner')).toBeHidden();
  await expect(browser.locator('#tfKpiStrip')).toBeVisible();
});

test('workspaces tab shows KPI strip, dense table default, view toggle, group-by, and search filters', async (fx) => {
  const { browser } = fx;
  await fx.app.open('/');
  await initApp(fx.screen, browser);
  await seedWorkspace(browser, 'e2e-ws-prod', { environment: 'production', group_name: 'core-infra', tags: ['vpc'] });
  await seedWorkspace(browser, 'e2e-ws-stg', { environment: 'staging', group_name: 'app-tier', tags: ['api'] });
  await stubTerraformStatus(browser, true);

  await fx.screen.getByRole('tab', 'Terraform').click();
  await expect(browser.locator('#tfTabs')).toBeVisible();
  await browser.locator('#btnTfRefresh').click();

  // KPI strip is visible.
  await expect(browser.locator('#tfKpiStrip [data-kpi=""]')).toBeVisible();
  await expect(browser.locator('#tfKpiStrip [data-kpi="approval"]')).toBeVisible();
  await expect(browser.locator('#tfKpiStrip [data-kpi="drifted"]')).toBeVisible();

  // Card Grid is the default view, with toggle to Dense Table.
  await expect(browser.locator('#tfWsGrid')).toBeVisible();
  await expect(browser.locator('#tfWsGrid').getByText('e2e-ws-prod')).toBeVisible();
  await expect(browser.locator('#tfWsGrid').getByText('e2e-ws-stg')).toBeVisible();

  // Switch to Dense Table, search narrows rows, and group-by displays group divider rows.
  await browser.locator('#tfWsViewListBtn').click();
  await expect(browser.locator('#tfWsTable')).toBeVisible();
  await browser.locator('#tfWsSearch').fill('e2e-ws-prod');
  await expect(browser.locator('#tfWsTableBody').getByText('e2e-ws-prod')).toBeVisible();
  await expect(browser.locator('#tfWsTableBody').getByText('e2e-ws-stg')).toHaveCount(0);
  await browser.locator('#tfWsSearch').fill('');

  await browser.locator('#tfWsGroupBy').click();
  await expect(browser.locator('#tfWsTableBody .tf-group-row').filter({ hasText: 'core-infra' })).toBeVisible();

  // Switch back to Grid view.
  await browser.locator('#tfWsViewGridBtn').click();
  await expect(browser.locator('#tfWsGrid')).toBeVisible();
});

test('new workspace drawer creates from built-in template, and workspace drawer supports tabs, wide mode, and Esc', async (fx) => {
  const { browser } = fx;
  await openTerraformSection(fx, true);
  await enableTerraformGate(browser);

  // Create a workspace from the built-in Hello world template.
  await browser.locator('#btnTfNewWs').click();
  await expect(browser.locator('#tfDrawerRoot')).toBeVisible();
  await expect(browser.locator('#tfTplGallery [data-tpl="hello-world"]')).toBeVisible();
  await browser.locator('#tfTplGallery [data-tpl="hello-world"]').click();
  await expect(browser.locator('#tfNewWsName')).not.toHaveValue('');
  await expect(browser.locator('#tfNewWsCode')).toHaveValue(/terraform_data/);
  await browser.locator('#tfNewWsName').fill('e2e-tpl-ws');
  await browser.locator('#tfNewWsSave').click();

  // Saving automatically opens the workspace drawer on the Configuration tab.
  await expect(browser.locator('#tfDrawerTitle')).toHaveText('e2e-tpl-ws');
  await expect(browser.locator('#tfWsTabs [data-tab="config"]')).toHaveAttribute('aria-selected', 'true');
  await browser.keyboard.press('Escape');
  await expect(browser.locator('#tfDrawerRoot')).toBeHidden();
  await expect(browser.locator('#tfWsGrid .tf-ws-card, #tfWsTableBody .tf-ws-row').filter({ hasText: 'e2e-tpl-ws' }).first()).toBeVisible();

  // Open drawer and verify width, wide toggle, tab switching, and Esc.
  await openWorkspaceDrawer(fx, 'e2e-tpl-ws');
  const baseWidth = await browser.evaluate(() => document.querySelector('#tfDrawer')!.getBoundingClientRect().width);
  expect(baseWidth).toBeGreaterThan(700);

  await switchDrawerTab(browser, 'runs');
  await switchDrawerTab(browser, 'overview');
  await expect(browser.locator('#tfWsBody .tf-ov-grid')).toBeVisible();

  await browser.locator('#tfDrawerWide').click();
  await expect.poll(() => browser.evaluate(() => document.querySelector('#tfDrawer')!.getBoundingClientRect().width)).toBeGreaterThan(baseWidth);
  await browser.locator('#tfDrawerWide').click();

  await browser.keyboard.press('Escape');
  await expect(browser.locator('#tfDrawerRoot')).toBeHidden();
});
