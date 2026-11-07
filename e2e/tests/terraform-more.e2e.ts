import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';
import { type Fixtures, openWorkspaceDrawer } from './terraform-helpers.js';

interface Reply { status: number; body: string }

// Raw JSON call through the page session.
async function call(browser: Fixtures['browser'], method: string, url: string, payload: unknown = null): Promise<Reply> {
  return browser.evaluate(async (args: { method: string; url: string; payload: unknown }) => {
    const res = await fetch(args.url, {
      method: args.method,
      headers: { 'Content-Type': 'application/json' },
      body: args.payload ? JSON.stringify(args.payload) : null,
    });
    return { status: res.status, body: await res.text() };
  }, { method, url, payload });
}

interface Created { workspace: { id: number } }

async function createWorkspace(browser: Fixtures['browser'], name: string, main: string, extra: Record<string, unknown> = {}) {
  await call(browser, 'PUT', '/api/terraform/status', { enabled: true });
  const res = await call(browser, 'POST', '/api/terraform/workspaces', {
    name, environment: 'dev', binary: 'terraform', source: 'inline', files: { 'main.tf': main }, ...extra,
  });
  const parsed: Created = JSON.parse(res.body);
  return parsed.workspace.id;
}

// Plan then approve a workspace through the API and wait until the run is applied. Returns the run id.
async function applyWorkspace(browser: Fixtures['browser'], id: number) {
  const started = JSON.parse((await call(browser, 'POST', `/api/terraform/workspaces/${id}/runs`, { type: 'plan' })).body);
  const runId: number = started.run.id;
  const status = async () => JSON.parse((await call(browser, 'GET', `/api/terraform/runs/${runId}`)).body).run.status as string;
  await expect.poll(status, { timeout: 60000 }).toBe('awaiting_approval');
  await call(browser, 'POST', `/api/terraform/runs/${runId}/apply`, { comment: 'approved by e2e' });
  await expect.poll(status, { timeout: 60000 }).toBe('applied');
  return runId;
}

async function runLog(browser: Fixtures['browser'], runId: number) {
  const log: { log: string } = JSON.parse((await call(browser, 'GET', `/api/terraform/runs/${runId}/log`)).body);
  return log.log;
}

const OUT_TF = 'resource "terraform_data" "x" {\n  input = "e2e"\n}\n\noutput "greeting" {\n  value = "hello-from-a"\n}\n';

test('a new workspace can start from a built-in template', async (fx) => {
  const { app, screen, browser } = fx;
  await app.open('/');
  await initApp(screen, browser);
  await call(browser, 'PUT', '/api/terraform/status', { enabled: true });
  await screen.getByRole('tab', 'Terraform').click();
  await browser.locator('#btnTfRefresh').click();
  await browser.locator('button[onclick="openTfWorkspaceModal()"]').first().click();
  await expect(browser.locator('#tfTplSelect')).toBeVisible();
  await browser.locator('#tfTplSelect').selectOption({ label: 'Hello world' });
  await expect(browser.locator('#tfWfName')).not.toHaveValue('');
  const state = await browser.evaluate(() => {
    const ta = document.querySelector<HTMLTextAreaElement>('#tfFeText');
    const cmHost = document.querySelector('#tfDlg1 .CodeMirror');
    const fromCm: { getValue(): string } | undefined = cmHost ? Reflect.get(cmHost, 'CodeMirror') : undefined;
    return (fromCm ? fromCm.getValue() : ta?.value) ?? '';
  });
  expect(state).toContain('terraform_data');
});

test('plan interval and auto-apply are saved, and auto-apply is refused for production', async (fx) => {
  const { browser } = fx;
  await openWorkspaceDrawer(fx, 'e2e-auto');
  await browser.locator('#tfWsTabs').getByRole('tab', 'Settings').click();
  await browser.locator('#tfSetPlanInterval').fill('30');
  await browser.locator('#tfSetAutoApply').check();
  await browser.locator('#tfSetSave').click();
  await expect(browser.locator('#tfSetDirty')).toBeHidden();
  const id = JSON.parse((await call(browser, 'GET', '/api/terraform/workspaces')).body).workspaces.find((w: { name: string }) => w.name === 'e2e-auto').id;
  const stored = JSON.parse((await call(browser, 'GET', `/api/terraform/workspaces/${id}`)).body).workspace;
  expect(stored.plan_interval_minutes).toBe(30);
  expect(stored.auto_apply).toBe(true);

  // Typing production disables the checkbox in the form, and the server refuses the combination too.
  await browser.locator('#tfSetEnv').fill('production');
  await expect(browser.locator('#tfSetAutoApply')).toBeDisabled();
  const refused = await call(browser, 'PUT', `/api/terraform/workspaces/${id}`, {
    name: 'e2e-auto', environment: 'production', binary: 'terraform', source: 'inline', files: { 'main.tf': stored.files['main.tf'] }, auto_apply: true,
  });
  expect(refused.status).toBe(400);
});

test('notification events are configured in the Gate tab and the Audit tab lists terraform activity', async (fx) => {
  const { app, screen, browser } = fx;
  await app.open('/');
  await initApp(screen, browser);
  await call(browser, 'PUT', '/api/terraform/status', { enabled: true });
  await call(browser, 'POST', '/api/terraform/cloud-accounts', {
    name: 'audit-do', provider: 'digitalocean', description: '', fields: { token: 'dop_v1_audit_token_value' },
  });
  await screen.getByRole('tab', 'Terraform').click();
  await browser.locator('#btnTfRefresh').click();

  await browser.locator('#tfTabs').getByRole('tab', 'Terraform Gate').click();
  await expect(browser.locator('#tfNotifyCard')).toBeVisible();
  await browser.locator('#tfNotifyCard [data-nf="applied"]').check();
  await browser.locator('#tfNfSave').click();
  await expect.poll(async () => JSON.parse((await call(browser, 'GET', '/api/terraform/notifications')).body).events.applied).toBe(true);

  await browser.locator('#tfTabs').getByRole('tab', 'Audit').click();
  await expect(browser.locator('#tfAuditRoot [data-audit]').filter({ hasText: 'audit-do' }).first()).toBeVisible();
  const audit = await call(browser, 'GET', '/api/terraform/audit?limit=100');
  expect(audit.body).not.toContain('dop_v1_audit_token_value');
});

test('runs no longer use the deprecated -state flag and a linked output feeds another workspace', async (fx) => {
  const { app, screen, browser } = fx;
  await app.open('/');
  await initApp(screen, browser);
  const sourceId = await createWorkspace(browser, 'e2e-src', OUT_TF);
  const firstRun = await applyWorkspace(browser, sourceId);
  expect(await runLog(browser, firstRun)).not.toContain('Deprecated flag');

  const consumer = 'variable "greeting" {\n  type = string\n}\n\noutput "echo" {\n  value = var.greeting\n}\n';
  const consumerId = await createWorkspace(browser, 'e2e-consumer', consumer);
  const saved = await call(browser, 'PUT', `/api/terraform/workspaces/${consumerId}/output-links`, {
    links: [{ var_name: 'greeting', source_workspace_id: sourceId, output_name: 'greeting' }],
  });
  expect(saved.status).toBe(200);
  const listed = JSON.parse((await call(browser, 'GET', `/api/terraform/workspaces/${consumerId}/output-links`)).body);
  expect(listed.links[0].available).toBe(true);

  // The consumer plans without defining the variable itself: the linked output supplies it.
  await call(browser, 'POST', `/api/terraform/workspaces/${consumerId}/runs`, { type: 'plan' });
  const latest = async () => JSON.parse((await call(browser, 'GET', `/api/terraform/workspaces/${consumerId}/runs`)).body).runs[0].status as string;
  await expect.poll(latest, { timeout: 60000 }).toBe('awaiting_approval');

  // A link back to the consumer would create a cycle and is rejected.
  const cycle = await call(browser, 'PUT', `/api/terraform/workspaces/${sourceId}/output-links`, {
    links: [{ var_name: 'x', source_workspace_id: consumerId, output_name: 'echo' }],
  });
  expect(cycle.status).toBe(400);
});

test('a resource is removed from the state through the State tab', async (fx) => {
  const { app, screen, browser } = fx;
  await app.open('/');
  await initApp(screen, browser);
  const id = await createWorkspace(browser, 'e2e-state', OUT_TF);
  await applyWorkspace(browser, id);
  await screen.getByRole('tab', 'Terraform').click();
  await browser.locator('#btnTfRefresh').click();
  await browser.locator('#tfWsGrid').getByText('e2e-state').click();
  await browser.locator('#tfWsTabs').getByRole('tab', 'State').click();
  await browser.locator('[data-st-rm="terraform_data.x"]').click();
  await browser.locator('#confirmModalBtn').click();
  await expect(browser.locator('[data-st-rm="terraform_data.x"]')).toHaveCount(0);

  const versions = JSON.parse((await call(browser, 'GET', `/api/terraform/workspaces/${id}/state`)).body);
  expect(JSON.stringify(versions)).not.toContain('terraform_data.x');
});

test('the run dialog is wide enough for its fields', async (fx) => {
  const { browser } = fx;
  await openWorkspaceDrawer(fx, 'e2e-dialog');
  await browser.locator('#tfWsTabs').getByRole('tab', 'Runs').click();
  await browser.locator('#tfRunStartBtn').click();
  await expect(browser.locator('#tfRunType')).toBeVisible();
  const width = await browser.evaluate(() => {
    const host = document.querySelector('#tfRunType')!.closest('.modal-content, .modal-box, .modal-body, [role="dialog"]');
    return host ? host.getBoundingClientRect().width : 0;
  });
  expect(width).toBeGreaterThanOrEqual(700);
});
