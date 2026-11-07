import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';

// The fixtures a test body receives, derived from `test` so the helpers need no hand-written types.
export type Fixtures = Parameters<Parameters<typeof test>[1]>[0];

const MAIN_TF = 'output "hello" { value = "world" }\n';

// Setup through the API (the Terraform Gate and a workspace), then drive the UI.
export async function seedWorkspace(browser: Fixtures['browser'], name: string) {
  await browser.evaluate(async (args: { name: string; main: string }) => {
    const send = (method: string, url: string, body?: unknown) =>
      fetch(url, { method, headers: { 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined });
    await send('PUT', '/api/terraform/status', { enabled: true });
    const list = await (await send('GET', '/api/terraform/workspaces')).json();
    if (!(list.workspaces || []).some((w: { name: string }) => w.name === args.name)) {
      await send('POST', '/api/terraform/workspaces', {
        name: args.name, environment: 'staging', binary: 'terraform', source: 'inline',
        files: { 'main.tf': args.main },
      });
    }
    return null;
  }, { name, main: MAIN_TF });
}

export async function openWorkspaceDrawer({ app, screen, browser }: Fixtures, name = 'e2e-ws') {
  await app.open('/');
  await initApp(screen, browser);
  await seedWorkspace(browser, name);
  await screen.getByRole('tab', 'Terraform').click();
  await browser.locator('#btnTfRefresh').click();
  await expect(browser.locator('#tfWsGrid').getByText(name)).toBeVisible();
  await browser.locator('#tfWsGrid').getByText(name).click();
  await expect(browser.locator('#tfDrawerRoot')).toBeVisible();
}

export async function readWorkspace(browser: Fixtures['browser'], name: string) {
  return browser.evaluate(async (n: string) => {
    const list = await (await fetch('/api/terraform/workspaces')).json();
    const found = (list.workspaces || []).find((w: { name: string }) => w.name === n);
    if (!found) return null;
    const one = await (await fetch('/api/terraform/workspaces/' + found.id)).json();
    const stored: { description: string; environment: string; files: Record<string, string>; tags: string[]; cloud_account_ids: number[] } = one.workspace;
    return stored;
  }, name);
}

// Replace the Configuration editor's text through the CodeMirror instance CodeMirror 5 hangs on its host element.
export async function setEditorValue(browser: Fixtures['browser'], value: string) {
  await browser.evaluate((v: string) => {
    const host = document.querySelector('#tfCfgEditor .CodeMirror');
    if (!host) throw new Error('Configuration editor is not mounted');
    const cm: { setValue(text: string): void } = Reflect.get(host, 'CodeMirror');
    cm.setValue(v);
    return null;
  }, value);
}
