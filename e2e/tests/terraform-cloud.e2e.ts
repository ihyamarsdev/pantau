import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';
import { type Fixtures, openWorkspaceDrawer, readWorkspace, seedWorkspace } from './terraform-helpers.js';

const WS = 'e2e-cloud';
const AWS_SECRET = 'sekret-value-e2e-123';
const DO_TOKEN = 'dop_v1_e2e_token_value';

interface CloudAccountJson {
  id: number;
  name: string;
  description: string;
  secrets_set: Record<string, boolean>;
  workspace_count: number;
}

// Raw call through the page session: the body text lets tests prove a secret never comes back.
async function cloudApi(browser: Fixtures['browser'], method: string, url: string, body: unknown = null) {
  return browser.evaluate(async (args: { method: string; url: string; body: unknown }) => {
    const res = await fetch(args.url, {
      method: args.method,
      headers: { 'Content-Type': 'application/json' },
      body: args.body ? JSON.stringify(args.body) : null,
    });
    return { status: res.status, text: await res.text() };
  }, { method, url, body });
}

async function listAccounts(browser: Fixtures['browser']) {
  const res = await cloudApi(browser, 'GET', '/api/terraform/cloud-accounts');
  const parsed: { accounts: CloudAccountJson[] } = JSON.parse(res.text);
  return { raw: res.text, accounts: parsed.accounts };
}

async function workspaceId(browser: Fixtures['browser'], name: string) {
  const res = await cloudApi(browser, 'GET', '/api/terraform/workspaces');
  const parsed: { workspaces: { id: number; name: string }[] } = JSON.parse(res.text);
  return parsed.workspaces.find((w) => w.name === name)!.id;
}

async function openCloudTab({ app, screen, browser }: Fixtures) {
  await app.open('/');
  await initApp(screen, browser);
  await seedWorkspace(browser, WS);
  await screen.getByRole('tab', 'Terraform').click();
  await browser.locator('#btnTfRefresh').click();
  await browser.locator('#tfTabs').getByRole('tab', 'Cloud Accounts').click();
  await expect(browser.locator('#btnTfCloudNew')).toBeVisible();
}

test('a cloud account is registered, shown without its secret and edited keeping the stored secret', async (fx) => {
  const { browser } = fx;
  await openCloudTab(fx);

  await browser.locator('#btnTfCloudNew').click();
  await browser.locator('#tfCaProvider').selectOption('Amazon Web Services');
  await browser.locator('#tfCaName').fill('aws-prod');
  await browser.locator('#tfCaF_access_key_id').fill('AKIAE2ETEST');
  await browser.locator('#tfCaF_secret_access_key').fill(AWS_SECRET);
  await browser.locator('#tfCaSave').click();

  await expect(browser.locator('[data-ca]').filter({ hasText: 'aws-prod' })).toBeVisible();
  // The secret must be nowhere in the page text nor in the API payload.
  await expect(browser.locator('#tfCloudRoot')).not.toContainText(AWS_SECRET);
  const created = await listAccounts(browser);
  expect(created.raw).not.toContain(AWS_SECRET);
  const awsId = created.accounts.find((a) => a.name === 'aws-prod')!.id;
  expect(created.accounts.find((a) => a.id === awsId)!.secrets_set.secret_access_key).toBe(true);

  // Edit: the secret input starts empty, a blank secret keeps the stored one.
  await browser.locator(`[data-ca="${awsId}"] [data-ca-edit]`).click();
  const secretInputValue = await browser.evaluate(() => document.querySelector<HTMLInputElement>('#tfCaF_secret_access_key')!.value);
  expect(secretInputValue).toBe('');
  await expect(browser.locator('#tfCaProvider')).toBeDisabled();
  await browser.locator('#tfCaDesc').fill('production account');
  await browser.locator('#tfCaSave').click();
  await expect(browser.locator('[data-ca]').filter({ hasText: 'production account' })).toBeVisible();
  const edited = (await listAccounts(browser)).accounts.find((a) => a.name === 'aws-prod')!;
  expect(edited.description).toBe('production account');
  expect(edited.secrets_set.secret_access_key).toBe(true);

  // Required fields are enforced: an Azure account without credentials is rejected.
  await browser.locator('#btnTfCloudNew').click();
  await browser.locator('#tfCaProvider').selectOption('Microsoft Azure');
  await browser.locator('#tfCaName').fill('azure-empty');
  await browser.locator('#tfCaSave').click();
  expect((await listAccounts(browser)).accounts.some((a) => a.name === 'azure-empty')).toBe(false);
});

test('an account is linked to a workspace in Settings and deleting it unlinks the workspace', async (fx) => {
  const { browser } = fx;
  await openWorkspaceDrawer(fx, WS);
  const made = await cloudApi(browser, 'POST', '/api/terraform/cloud-accounts', {
    name: 'do-e2e', provider: 'digitalocean', description: '', fields: { token: DO_TOKEN },
  });
  expect(made.status).toBeLessThan(300);

  const id = (await listAccounts(browser)).accounts.find((a) => a.name === 'do-e2e')!.id;
  await browser.locator('#tfWsTabs').getByRole('tab', 'Settings').click();
  const link = browser.locator(`#tfSetCloud [data-ca-link="${id}"]`);
  await expect(link).toBeVisible();
  await link.check();
  await expect(browser.locator('#tfSetDirty')).toBeVisible();
  await browser.locator('#tfSetSave').click();
  await expect(browser.locator('#tfSetDirty')).toBeHidden();

  expect((await readWorkspace(browser, WS))!.cloud_account_ids).toEqual([id]);
  expect((await listAccounts(browser)).accounts.find((a) => a.id === id)!.workspace_count).toBe(1);

  // Delete through the Cloud Accounts tab: the workspace is unlinked.
  await browser.locator('#tfDrawerRoot').getByRole('button', 'Close').click();
  await browser.locator('#tfTabs').getByRole('tab', 'Cloud Accounts').click();
  await browser.locator(`[data-ca="${id}"] [data-ca-del]`).click();
  await browser.locator('#confirmModalBtn').click();
  await expect(browser.locator('[data-ca]').filter({ hasText: 'do-e2e' })).toHaveCount(0);
  expect((await readWorkspace(browser, WS))!.cloud_account_ids).toEqual([]);
});

test('with no runs the Runs tab shows one centered Start Run call to action', async (fx) => {
  const { browser } = fx;
  await openWorkspaceDrawer(fx, 'e2e-runs-empty');
  await browser.locator('#tfWsTabs').getByRole('tab', 'Runs').click();
  await expect(browser.locator('#tfRunStartBtn')).toBeVisible();

  const geometry = await browser.evaluate(() => {
    const pane = document.querySelector('#tfWsBody')!.getBoundingClientRect();
    const btn = document.querySelector('#tfRunStartBtn')!.getBoundingClientRect();
    return {
      dx: btn.left + btn.width / 2 - (pane.left + pane.width / 2),
      dy: btn.top + btn.height / 2 - (pane.top + pane.height / 2),
      paneHeight: pane.height,
    };
  });
  expect(Math.abs(geometry.dx)).toBeLessThan(2);
  // Vertically it sits in the middle band of the pane, not glued to the top.
  expect(Math.abs(geometry.dy)).toBeLessThan(geometry.paneHeight * 0.25);
});

test('with runs the Start Run button fills the run list column', async (fx) => {
  const { browser } = fx;
  await openWorkspaceDrawer(fx, 'e2e-runs-list');
  await cloudApi(browser, 'POST', '/api/terraform/workspaces/' + (await workspaceId(browser, 'e2e-runs-list')) + '/runs', { type: 'plan' });
  await browser.locator('#tfWsTabs').getByRole('tab', 'Runs').click();
  await expect(browser.locator('#tfRunList .card').first()).toBeVisible();

  const widths = await browser.evaluate(() => {
    const list = document.querySelector('#tfRunList')!.getBoundingClientRect();
    const btn = document.querySelector('#tfRunStartBtn')!.getBoundingClientRect();
    return { list: list.width, btn: btn.width, listCenter: list.left + list.width / 2, btnCenter: btn.left + btn.width / 2 };
  });
  expect(Math.abs(widths.btn - widths.list)).toBeLessThan(2);
  expect(Math.abs(widths.btnCenter - widths.listCenter)).toBeLessThan(2);
});
