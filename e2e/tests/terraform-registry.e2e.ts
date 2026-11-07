import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';
import { type Fixtures, setEditorValue } from './terraform-helpers.js';
import { startFakeRegistry } from './registry-fake.js';

const MAIN_TF = [
  'terraform {',
  '  required_providers {',
  '    aws = {',
  '      source  = "hashicorp/aws"',
  '      version = "~> 4.0"',
  '    }',
  '  }',
  '}',
  '',
  'module "vpc" {',
  '  source  = "acme/vpc/aws"',
  '  version = "~> 1.0"',
  '}',
  '',
  'module "vpc_old" {',
  '  source  = "acme/vpc/aws"',
  '  version = "1.0.0"',
  '}',
  '',
].join('\n');

// Log in, enable the Gate, point Pantau at the fake registry and create an inline workspace.
async function prepare({ app, screen, browser }: Fixtures, registryUrl: string, name: string) {
  await app.open('/');
  await initApp(screen, browser);
  await browser.evaluate(async (args: { url: string; name: string; main: string }) => {
    const send = (method: string, path: string, body: unknown) =>
      fetch(path, { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    await send('PUT', '/api/terraform/status', { enabled: true });
    await send('PUT', '/api/terraform/registry/settings', { base_url: args.url });
    await send('POST', '/api/terraform/workspaces', {
      name: args.name, environment: 'dev', binary: 'terraform', source: 'inline', files: { 'main.tf': args.main },
    });
    return null;
  }, { url: registryUrl, name, main: MAIN_TF });
  await screen.getByRole('tab', 'Terraform').click();
  await browser.locator('#btnTfRefresh').click();
}

async function editorValue(browser: Fixtures['browser']) {
  return browser.evaluate(() => {
    const host = document.querySelector('#tfCfgEditor .CodeMirror');
    const cm: { getValue(): string } = Reflect.get(host!, 'CodeMirror');
    return cm.getValue();
  });
}

test('the Registry tab searches modules and providers and shows escaped details', async (fx) => {
  const { browser } = fx;
  const registry = await startFakeRegistry();
  try {
    await prepare(fx, registry.url, 'e2e-reg-tab');
    await browser.locator('#tfTabs').getByRole('tab', 'Registry').click();
    await expect(browser.locator('#tfRegistryRoot')).toBeVisible();

    await browser.locator('#tfRegQuery').fill('vpc');
    await browser.locator('#tfRegSearch').click();
    await browser.locator('[data-reg-item="acme/vpc/aws"]').click();
    await expect(browser.locator('#tfRegInputs')).toContainText('cidr');
    await expect(browser.locator('#tfRegVersion')).toBeVisible();

    // README text from the registry is untrusted: formatted through a safe renderer, never executed.
    await browser.locator('[data-reg-tab="readme"]').click();
    await expect(browser.locator('#tfRegReadme .tf-md-h')).toContainText('Acme VPC');
    await expect(browser.locator('#tfRegReadme strong')).toHaveText('VPC');
    await expect(browser.locator('#tfRegReadme pre')).toContainText('module "vpc"');
    const readme = await browser.evaluate(() => ({
      links: [...document.querySelectorAll('#tfRegReadme a')].map((a) => ({ href: a.getAttribute('href'), rel: a.getAttribute('rel') })),
      pwned: Reflect.get(window, 'pwned') === true,
      injected: document.querySelectorAll('#tfRegReadme script, #tfRegReadme img').length,
    }));
    expect(readme.links).toEqual([{ href: 'https://example.com/x', rel: 'noopener noreferrer' }]);
    expect(readme.pwned).toBe(false);
    expect(readme.injected).toBe(0);
    // The raw source stays one click away and shows the markup literally.
    await browser.locator('#tfRegReadmeToggle').click();
    await expect(browser.locator('#tfRegReadmeRaw')).toContainText('<script>');

    await browser.locator('[data-reg-kind="providers"]').click();
    await browser.locator('#tfRegQuery').fill('aws');
    await browser.locator('#tfRegSearch').click();
    await expect(browser.locator('[data-reg-item="hashicorp/aws"]')).toBeVisible();
  } finally {
    await registry.close();
  }
});

test('a registry module is inserted into the Configuration editor', async (fx) => {
  const { browser } = fx;
  const registry = await startFakeRegistry();
  try {
    await prepare(fx, registry.url, 'e2e-reg-insert');
    await browser.locator('#tfWsGrid').getByText('e2e-reg-insert').click();
    await browser.locator('#tfWsTabs').getByRole('tab', 'Configuration').click();
    await expect(browser.locator('#tfCfgEditor .CodeMirror')).toBeVisible();
    await setEditorValue(browser, '# my config\n');

    await browser.locator('#tfCfgRegistry').click();
    await browser.locator('#tfRegQuery').fill('vpc');
    await browser.locator('#tfRegSearch').click();
    await browser.locator('[data-reg-item="acme/vpc/aws"]').click();
    await expect(browser.locator('#tfRegInputs')).toContainText('cidr');
    await browser.locator('#tfRegInsert').click();

    const text = await editorValue(browser);
    expect(text).toContain('# my config');
    expect(text).toContain('module "vpc"');
    expect(text).toContain('source  = "acme/vpc/aws"');
    expect(text).toContain('cidr');
    await expect(browser.locator('#tfCfgDirty')).toBeVisible();
  } finally {
    await registry.close();
  }
});

test('the registry check flags pins and offers to update the constraint in the editor', async (fx) => {
  const { browser } = fx;
  const registry = await startFakeRegistry();
  try {
    await prepare(fx, registry.url, 'e2e-reg-check');
    await browser.locator('#tfWsGrid').getByText('e2e-reg-check').click();
    await browser.locator('#tfWsTabs').getByRole('tab', 'Configuration').click();
    await expect(browser.locator('#tfCfgEditor .CodeMirror')).toBeVisible();

    await browser.locator('#tfCfgRegCheck').click();
    await expect(browser.locator('#tfCfgRegStatus [data-regc="module:vpc_old"]')).toContainText('1.2.0');
    await expect(browser.locator('#tfCfgRegStatus [data-regc="provider:aws"]')).toContainText('5.1.0');
    // The ~> 1.0 module already allows the newest version, so there is nothing to apply.
    await expect(browser.locator('#tfCfgRegStatus [data-regc="module:vpc"] [data-regc-apply]')).toHaveCount(0);

    await browser.locator('#tfCfgRegStatus [data-regc="module:vpc_old"] [data-regc-apply]').click();
    const text = await editorValue(browser);
    expect(text).toContain('version = "~> 1.2"');
    expect(text).not.toContain('version = "1.0.0"');
    await expect(browser.locator('#tfCfgDirty')).toBeVisible();
  } finally {
    await registry.close();
  }
});

test('the registry URL is configured and tested from the Terraform Gate tab', async (fx) => {
  const { browser } = fx;
  const registry = await startFakeRegistry();
  try {
    await prepare(fx, registry.url, 'e2e-reg-settings');
    await browser.locator('#tfTabs').getByRole('tab', 'Terraform Gate').click();
    await expect(browser.locator('#tfRegSettingsCard')).toBeVisible();
    await browser.locator('#tfRegUrl').fill(registry.url);
    await browser.locator('#tfRegUrlSave').click();
    await browser.locator('#tfRegUrlTest').click();
    await expect(browser.locator('#tfRegUrlStatus')).toBeVisible();

    const reply = await browser.evaluate(async () => {
      const get = await (await fetch('/api/terraform/registry/settings')).json();
      const rejected = await fetch('/api/terraform/registry/settings', {
        method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ base_url: 'http://example.com' }),
      });
      return { base: String(get.base_url), rejected: rejected.status };
    });
    expect(reply.base).toBe(registry.url);
    // Plain http to a non-loopback host is refused.
    expect(reply.rejected).toBe(400);
  } finally {
    await registry.close();
  }
});
