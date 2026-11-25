import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';

export type Fixtures = Parameters<Parameters<typeof test>[1]>[0];

export interface Reply {
  status: number;
  body: string;
}

const DEFAULT_MAIN_TF = 'output "hello" { value = "world" }\n';

export async function apiCall(
  browser: Fixtures['browser'],
  method: string,
  url: string,
  payload: unknown = null,
): Promise<Reply> {
  return browser.evaluate(async (args: { method: string; url: string; payload: unknown }) => {
    const res = await fetch(args.url, {
      method: args.method,
      headers: { 'Content-Type': 'application/json' },
      body: args.payload !== null ? JSON.stringify(args.payload) : null,
    });
    return { status: res.status, body: await res.text() };
  }, { method, url, payload });
}

export async function enableTerraformGate(browser: Fixtures['browser']) {
  await apiCall(browser, 'PUT', '/api/terraform/status', { enabled: true });
}

// Stubs /api/terraform/status so UI tests work deterministically even when terraform/tofu is not in PATH.
// Real DB-backed routes still require enableTerraformGate(browser) before routing or via direct API call.
export async function stubTerraformStatus(browser: Fixtures['browser'], initialEnabled = true) {
  let enabled = initialEnabled;
  await browser.route('**/api/terraform/status', async (route) => {
    const req = route.request;
    if (req.method === 'PUT' && req.postData) {
      try {
        const parsed = JSON.parse(req.postData) as { enabled?: boolean };
        enabled = !!parsed.enabled;
      } catch {
        // ignore parse errors
      }
    }
    await route.fulfill({
      json: {
        enabled,
        supported: true,
        binaries: [
          { name: 'terraform', path: '/usr/local/bin/terraform', version: 'Terraform v1.10.0', err: '' },
          { name: 'tofu', path: '', version: '', err: 'not found' },
        ],
        infracost: { available: false, version: '', error: 'infracost not installed' },
      },
    });
  });
}

export async function seedWorkspace(
  browser: Fixtures['browser'],
  name: string,
  opts: {
    environment?: string;
    group_name?: string;
    source?: 'inline' | 'git';
    files?: Record<string, string>;
    git_url?: string;
    git_branch?: string;
    tags?: string[];
  } = {},
): Promise<number> {
  await enableTerraformGate(browser);
  return browser.evaluate(async (args: { name: string; opts: typeof opts; defMain: string }) => {
    const send = (method: string, url: string, body?: unknown) =>
      fetch(url, { method, headers: { 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined });
    const list = await (await send('GET', '/api/terraform/workspaces')).json();
    const existing = (list.workspaces || []).find((w: { name: string }) => w.name === args.name);
    if (existing) return existing.id as number;
    const source = args.opts.source || 'inline';
    const payload: Record<string, unknown> = {
      name: args.name,
      environment: args.opts.environment ?? 'staging',
      group_name: args.opts.group_name ?? '',
      tags: args.opts.tags ?? [],
      binary: 'terraform',
      source,
    };
    if (source === 'inline') {
      payload.files = args.opts.files || { 'main.tf': args.defMain };
    } else {
      payload.git_url = args.opts.git_url || 'https://github.com/example/infra.git';
      payload.git_branch = args.opts.git_branch || 'main';
      payload.git_auth = 'none';
    }
    const created = await (await send('POST', '/api/terraform/workspaces', payload)).json();
    return created.workspace.id as number;
  }, { name, opts, defMain: DEFAULT_MAIN_TF });
}

export async function openTerraformSection({ app, screen, browser }: Fixtures, stubEnabled = true) {
  await app.open('/');
  await initApp(screen, browser);
  await enableTerraformGate(browser);
  await stubTerraformStatus(browser, stubEnabled);
  await screen.getByRole('tab', 'Terraform').click();
  await expect(browser.locator('#sectionTerraform')).toBeVisible();
  await expect(browser.locator('#tfTabs')).toBeVisible();
}

export async function switchTfTab(
  browser: Fixtures['browser'],
  tab: 'workspaces' | 'runs' | 'variables' | 'registry' | 'governance',
) {
  await browser.locator(`#tfTabs [data-tab="${tab}"]`).click();
  await expect(browser.locator(`#tfTabs [data-tab="${tab}"]`)).toHaveAttribute('aria-selected', 'true');
}

export async function openWorkspaceDrawer(
  fx: Fixtures,
  name = 'e2e-ws',
  opts: Parameters<typeof seedWorkspace>[2] = {},
) {
  const { browser } = fx;
  await fx.app.open('/');
  await initApp(fx.screen, browser);
  const id = await seedWorkspace(browser, name, opts);
  await stubTerraformStatus(browser, true);
  await fx.screen.getByRole('tab', 'Terraform').click();
  await expect(browser.locator('#tfTabs')).toBeVisible();
  await browser.locator('#btnTfRefresh').click();
  const row = browser.locator('#tfWsTableBody tr[data-ws-id], #tfWsGrid .tf-ws-card').filter({ hasText: name }).first();
  await expect(row).toBeVisible();
  await row.click();
  await expect(browser.locator('#tfDrawerRoot')).toBeVisible();
  await expect(browser.locator('#tfWsTabs')).toBeVisible();
  return id;
}

export async function switchDrawerTab(
  browser: Fixtures['browser'],
  tab: 'overview' | 'config' | 'runs' | 'state' | 'variables' | 'drift' | 'settings',
) {
  await browser.locator(`#tfWsTabs [data-tab="${tab}"]`).click();
  await expect(browser.locator(`#tfWsTabs [data-tab="${tab}"]`)).toHaveAttribute('aria-selected', 'true');
}

export async function readWorkspace(browser: Fixtures['browser'], name: string) {
  return browser.evaluate(async (n: string) => {
    const list = await (await fetch('/api/terraform/workspaces')).json();
    const found = (list.workspaces || []).find((w: { name: string }) => w.name === n);
    if (!found) return null;
    const one = await (await fetch('/api/terraform/workspaces/' + found.id)).json();
    return one.workspace as {
      id: number;
      name: string;
      description: string;
      environment: string;
      group_name: string;
      files: Record<string, string>;
      tags: string[];
      cloud_account_ids: number[];
      auto_apply: boolean;
      plan_interval_minutes: number;
    };
  }, name);
}

export async function setCodeMirrorValue(
  browser: Fixtures['browser'],
  value: string,
  selector = '#tfCfgEditor .CodeMirror',
) {
  await browser.evaluate((args: { v: string; sel: string }) => {
    const host = document.querySelector(args.sel);
    if (!host) throw new Error('CodeMirror editor is not mounted at ' + args.sel);
    const cm: { setValue(text: string): void } = Reflect.get(host, 'CodeMirror');
    cm.setValue(args.v);
    return null;
  }, { v: value, sel: selector });
}

export async function getCodeMirrorValue(
  browser: Fixtures['browser'],
  selector = '#tfCfgEditor .CodeMirror',
): Promise<string> {
  return browser.evaluate((sel: string) => {
    const host = document.querySelector(sel);
    if (!host) throw new Error('CodeMirror editor is not mounted at ' + sel);
    const cm: { getValue(): string } = Reflect.get(host, 'CodeMirror');
    return cm.getValue();
  }, selector);
}
