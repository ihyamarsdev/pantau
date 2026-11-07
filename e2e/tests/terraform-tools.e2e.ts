import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';
import { type Fixtures, openWorkspaceDrawer, setEditorValue } from './terraform-helpers.js';

interface StoredVariable {
  key: string;
  value: string;
  sensitive: boolean;
  category: string;
}

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

// Create or overwrite an inline workspace through the API, return its id.
async function putWorkspace(browser: Fixtures['browser'], name: string, files: Record<string, string>) {
  return browser.evaluate(async (args: { name: string; files: Record<string, string> }) => {
    const send = (method: string, url: string, body: unknown) =>
      fetch(url, { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    await send('PUT', '/api/terraform/status', { enabled: true });
    const list = await (await fetch('/api/terraform/workspaces')).json();
    const found: { id: number } | undefined = (list.workspaces || []).find((w: { name: string }) => w.name === args.name);
    const base = { name: args.name, environment: 'dev', binary: 'terraform', source: 'inline', files: args.files };
    if (found) {
      await send('PUT', '/api/terraform/workspaces/' + found.id, base);
      return found.id;
    }
    const created = await (await send('POST', '/api/terraform/workspaces', base)).json();
    return created.workspace.id as number;
  }, { name, files });
}

async function openWorkspace({ app, screen, browser }: Fixtures, name: string, files: Record<string, string>) {
  await app.open('/');
  await initApp(screen, browser);
  const id = await putWorkspace(browser, name, files);
  await screen.getByRole('tab', 'Terraform').click();
  await browser.locator('#btnTfRefresh').click();
  await expect(browser.locator('#tfWsGrid').getByText(name)).toBeVisible();
  await browser.locator('#tfWsGrid').getByText(name).click();
  await expect(browser.locator('#tfDrawerRoot')).toBeVisible();
  return id;
}

async function storedVariables(browser: Fixtures['browser'], id: number) {
  return browser.evaluate(async (wsId: number) => {
    const res = await (await fetch(`/api/terraform/workspaces/${wsId}/variables`)).json();
    const vars: StoredVariable[] = res.variables;
    return vars;
  }, id);
}

test('variables declared in the configuration are detected and added from the Variables tab', async (fx) => {
  const { browser } = fx;
  const id = await openWorkspace(fx, 'e2e-detect', { 'variables.tf': VARS_TF });

  await browser.locator('#tfWsTabs').getByRole('tab', 'Variables').click();
  await browser.locator('#tfVarDetectBtn').click();
  await expect(browser.locator('#tfVarDetectList [data-vd="region"]')).toBeVisible();
  await expect(browser.locator('#tfVarDetectList [data-vd="api_token"]')).toBeVisible();

  // Variables with a default are offered unchecked; opt in to the one we want next to the required one.
  await browser.locator('#tfVarDetectList [data-vd="region"] [data-vd-on]').check();
  await browser.locator('#tfVarDetectAdd').click();
  await expect(browser.locator('#tfVarDetectList')).toBeHidden();

  const vars = await storedVariables(browser, id);
  const region = vars.find((v) => v.key === 'region');
  const token = vars.find((v) => v.key === 'api_token');
  expect(region?.value).toBe('eu-west-1');
  expect(token?.sensitive).toBe(true);

  // A second detection marks both as already defined, so nothing is offered twice.
  await browser.locator('#tfVarDetectBtn').click();
  await expect(browser.locator('#tfVarDetectList [data-vd="region"] [data-vd-on]')).toBeDisabled();
});

test('Format rewrites the editor and Validate reports diagnostics with file and line', async (fx) => {
  const { browser } = fx;
  await openWorkspace(fx, 'e2e-tools', { 'main.tf': 'output "a" { value = 1 }\n' });
  await browser.locator('#tfWsTabs').getByRole('tab', 'Configuration').click();
  await expect(browser.locator('#tfCfgEditor .CodeMirror')).toBeVisible();

  // The editor uses the HCL mode so .tf files are highlighted.
  const mode = await browser.evaluate(() => {
    const host = document.querySelector('#tfCfgEditor .CodeMirror');
    const cm: { getOption(name: string): unknown } = Reflect.get(host!, 'CodeMirror');
    return String(cm.getOption('mode'));
  });
  expect(mode).toBe('hcl');

  await setEditorValue(browser, 'resource "terraform_data" "x" {\n      input    =   "hi"\n}\n');
  await browser.locator('#tfCfgFmt').click();
  await expect(browser.locator('#tfCfgDirty')).toBeVisible();
  await expect.poll(async () => browser.evaluate(() => {
    const host = document.querySelector('#tfCfgEditor .CodeMirror');
    const cm: { getValue(): string } = Reflect.get(host!, 'CodeMirror');
    return cm.getValue();
  })).toBe('resource "terraform_data" "x" {\n  input = "hi"\n}\n');

  // A reference to an undeclared variable is a validation error pointing at main.tf.
  await setEditorValue(browser, 'output "a" {\n  value = var.missing\n}\n');
  await browser.locator('#tfCfgValidate').click();
  await expect(browser.locator('#tfCfgDiag')).toContainText('main.tf');
  await expect(browser.locator('#tfCfgDiag')).toContainText('missing');

  // A valid configuration reports success.
  await setEditorValue(browser, 'output "a" {\n  value = 1\n}\n');
  await browser.locator('#tfCfgValidate').click();
  await expect(browser.locator('#tfCfgDiag')).not.toContainText('missing');
  await expect(browser.locator('#tfCfgDiag')).toContainText('valid');
});

const KUBECONFIG = [
  'apiVersion: v1',
  'kind: Config',
  'clusters:',
  '- name: e2e',
  '  cluster:',
  '    server: https://127.0.0.1:6443',
  'users:',
  '- name: e2e',
  '  user:',
  '    token: e2e-kube-token-value',
  'contexts:',
  '- name: e2e',
  '  context:',
  '    cluster: e2e',
  '    user: e2e',
  'current-context: e2e',
  '',
].join('\n');

test('a cloud account is imported from an existing Kubernetes cluster without exposing the kubeconfig', async (fx) => {
  const { app, screen, browser } = fx;
  await app.open('/');
  await initApp(screen, browser);
  const seeded = await browser.evaluate(async (kubeconfig: string) => {
    await fetch('/api/terraform/status', {
      method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ enabled: true }),
    });
    const res = await fetch('/api/kube/clusters', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: 'e2e-kube', kubeconfig, context: 'e2e' }),
    });
    return res.status;
  }, KUBECONFIG);
  expect(seeded).toBeLessThan(300);

  await screen.getByRole('tab', 'Terraform').click();
  await browser.locator('#btnTfRefresh').click();
  await browser.locator('#tfTabs').getByRole('tab', 'Cloud Accounts').click();
  await browser.locator('#btnTfCloudNew').click();
  await browser.locator('#tfCaProvider').selectOption('Kubernetes');
  await expect(browser.locator('#tfCaImport')).toBeVisible();
  await browser.locator('#tfCaImport').selectOption({ label: 'e2e-kube' });
  await expect(browser.locator('#tfCaF_kubeconfig')).toBeHidden();
  await browser.locator('#tfCaName').fill('k8s-imported');
  await browser.locator('#tfCaSave').click();

  const card = browser.locator('[data-ca]').filter({ hasText: 'k8s-imported' });
  await expect(card).toBeVisible();
  const listed = await browser.evaluate(async () => {
    const res = await fetch('/api/terraform/cloud-accounts');
    const body = await res.text();
    const parsed: { accounts: { name: string; secrets_set: Record<string, boolean> }[] } = JSON.parse(body);
    return { body, account: parsed.accounts.find((a) => a.name === 'k8s-imported') };
  });
  expect(listed.account?.secrets_set.kubeconfig).toBe(true);
  expect(listed.body).not.toContain('e2e-kube-token-value');
});
