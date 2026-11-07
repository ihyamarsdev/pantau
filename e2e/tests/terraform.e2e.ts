import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';
import { openWorkspaceDrawer, seedWorkspace } from './terraform-helpers.js';

test('a workspace opens in a wide side drawer with working tabs, expand and Esc', async (fx) => {
  const { screen, browser } = fx;
  await openWorkspaceDrawer(fx);

  await expect(browser.locator('#tfWsModalName')).toHaveText('e2e-ws');

  // Roomy: the default drawer takes well over 900px of the screen and is docked right.
  const box = await browser.evaluate(() => {
    const r = document.querySelector('#tfDrawer')!.getBoundingClientRect();
    return { width: r.width, right: r.right, vw: window.innerWidth };
  });
  expect(box.width).toBeGreaterThan(900);
  expect(box.right).toBeGreaterThanOrEqual(box.vw - 1);

  // Tabs: Runs then Settings replace the body without closing the drawer.
  await browser.locator('#tfWsTabs').getByRole('tab', 'Runs').click();
  await expect(browser.locator('#tfWsTabs').getByRole('tab', 'Runs')).toHaveAttribute('aria-selected', 'true');
  await browser.locator('#tfWsTabs').getByRole('tab', 'Settings').click();
  await expect(browser.locator('#tfWsTabs').getByRole('tab', 'Settings')).toHaveAttribute('aria-selected', 'true');
  await expect(browser.locator('#tfDrawerRoot')).toBeVisible();

  // Expand makes it wider than the default, collapse brings it back.
  await browser.locator('#tfDrawerWide').click();
  const wide = await browser.evaluate(() => document.querySelector('#tfDrawer')!.getBoundingClientRect().width);
  expect(wide).toBeGreaterThan(box.width);
  await browser.locator('#tfDrawerWide').click();
  // The width animates (.18s), so poll until it settles back to the default.
  await expect.poll(() => browser.evaluate(() => document.querySelector('#tfDrawer')!.getBoundingClientRect().width)).toBeCloseTo(box.width, 0);

  // Esc closes the drawer and the workspace list is usable again.
  await browser.keyboard.press('Escape');
  await expect(browser.locator('#tfDrawerRoot')).toBeHidden();
  await expect(browser.locator('#tfWsGrid').getByText('e2e-ws')).toBeVisible();
});

test('the Overview tab lays its cards out in a grid and a plan dialog opens above the drawer', async (fx) => {
  const { screen, browser } = fx;
  await openWorkspaceDrawer(fx);

  await expect(browser.locator('#tfWsBody .tf-ov-grid')).toBeVisible();
  const cards = await browser.evaluate(() => document.querySelectorAll('#tfWsBody .tf-ov-card').length);
  expect(cards).toBeGreaterThanOrEqual(2);

  // A dialog (modal) must stack above the drawer, so its content is reachable.
  await screen.getByRole('button', 'Plan (Standard)').click();
  await expect(browser.locator('#tfDlg1')).toBeVisible();
  const order = await browser.evaluate(() => {
    const z = (sel: string) => parseInt(getComputedStyle(document.querySelector(sel)!).zIndex || '0', 10) || 0;
    return { dialog: z('#tfDlg1'), drawer: z('#tfDrawerRoot') };
  });
  expect(order.dialog).toBeGreaterThan(order.drawer);
});

test('the workspaces panel stacks its toolbar above the list and uses the full width', async (fx) => {
  const { app, screen, browser } = fx;
  await app.open('/');
  await initApp(screen, browser);
  await seedWorkspace(browser, 'e2e-ws');
  await screen.getByRole('tab', 'Terraform').click();
  await browser.locator('#btnTfRefresh').click();
  await expect(browser.locator('#tfWsGrid').getByText('e2e-ws')).toBeVisible();

  const layout = await browser.evaluate(() => {
    const panel = document.querySelector('#tfPanelWorkspaces') as HTMLElement;
    const toolbar = document.querySelector('#tfWsSearchInput') as HTMLElement;
    const grid = document.querySelector('#tfWsGrid') as HTMLElement;
    return {
      direction: getComputedStyle(panel).flexDirection,
      toolbarBottom: toolbar.getBoundingClientRect().bottom,
      gridTop: grid.getBoundingClientRect().top,
      panelWidth: panel.getBoundingClientRect().width,
      gridWidth: grid.getBoundingClientRect().width,
    };
  });
  expect(layout.direction).toBe('column');
  expect(layout.gridTop).toBeGreaterThanOrEqual(layout.toolbarBottom);
  expect(layout.gridWidth).toBeGreaterThanOrEqual(layout.panelWidth - 2);
});
