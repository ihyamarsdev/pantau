import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import {
  getCodeMirrorValue,
  openWorkspaceDrawer,
  readWorkspace,
  setCodeMirrorValue,
  switchDrawerTab,
} from './terraform-helpers.js';

test('Configuration tab edits inline files in HCL CodeMirror, runs fmt/validate, and guards unsaved changes', async (fx) => {
  const { browser } = fx;

  // Stub terraform CLI tools (fmt/validate) so the UI flow works even without terraform binary in PATH.
  await browser.route('**/api/terraform/workspaces/*/tools/*', async (route) => {
    const url = new URL(route.request.url);
    if (url.pathname.endsWith('/tools/fmt')) {
      await route.fulfill({
        json: {
          changed: ['main.tf'],
          files: { 'main.tf': 'resource "terraform_data" "x" {\n  input = "hi"\n}\n' },
          errors: {},
        },
      });
      return;
    }
    if (url.pathname.endsWith('/tools/validate')) {
      await route.fulfill({
        json: {
          ok: false,
          diagnostics: [
            { severity: 'error', summary: 'Reference to undeclared input variable', detail: 'var.missing is not declared', file: 'main.tf', line: 2 },
          ],
        },
      });
      return;
    }
    await route.continue();
  });

  await openWorkspaceDrawer(fx, 'e2e-cfg-inline');
  await switchDrawerTab(browser, 'config');

  await expect(browser.locator('#tfCfgFiles [data-file="main.tf"]')).toBeVisible();
  await expect(browser.locator('#tfCfgEditor .CodeMirror')).toBeVisible();
  await expect(browser.locator('#tfCfgDirty')).toBeHidden();

  // Verify HCL CodeMirror mode.
  const mode = await browser.evaluate(() => {
    const host = document.querySelector('#tfCfgEditor .CodeMirror');
    const cm: { getOption(name: string): unknown } = Reflect.get(host!, 'CodeMirror');
    return String(cm.getOption('mode'));
  });
  expect(mode).toBe('hcl');

  // Format rewrites whitespace cleanly and marks dirty.
  await setCodeMirrorValue(browser, 'resource "terraform_data" "x" {\n      input    =   "hi"\n}\n');
  await expect(browser.locator('#tfCfgDirty')).toBeVisible();
  await browser.locator('#tfCfgFmt').click();
  await expect.poll(() => getCodeMirrorValue(browser)).toBe('resource "terraform_data" "x" {\n  input = "hi"\n}\n');

  // Validate reports diagnostics pointing to main.tf.
  await setCodeMirrorValue(browser, 'output "a" {\n  value = var.missing\n}\n');
  await browser.locator('#tfCfgValidate').click();
  await expect(browser.locator('#tfCfgDiag')).toContainText('main.tf');
  await expect(browser.locator('#tfCfgDiag')).toContainText('missing');

  // Save valid content to real server DB and verify add/delete file.
  await setCodeMirrorValue(browser, 'output "hello" { value = "edited" }\n');
  await browser.locator('#tfCfgSave').click();
  await expect(browser.locator('#tfCfgDirty')).toBeHidden();
  expect((await readWorkspace(browser, 'e2e-cfg-inline'))!.files['main.tf']).toContain('edited');

  await browser.locator('#tfCfgNewName').fill('vars.tf');
  await browser.locator('#tfCfgAddFile').click();
  await expect(browser.locator('#tfCfgFiles [data-file="vars.tf"]')).toBeVisible();
  await browser.locator('#tfCfgSave').click();
  await expect(browser.locator('#tfCfgDirty')).toBeHidden();
  expect(Object.keys((await readWorkspace(browser, 'e2e-cfg-inline'))!.files).sort()).toEqual(['main.tf', 'vars.tf']);

  await browser.locator('#tfCfgFiles [data-del="vars.tf"]').click();
  await browser.locator('#confirmModalBtn').click();
  await browser.locator('#tfCfgSave').click();
  await expect(browser.locator('#tfCfgDirty')).toBeHidden();

  // Unsaved-changes guard protects tab switch & language switch.
  await setCodeMirrorValue(browser, '# unsaved\n');
  await expect(browser.locator('#tfCfgDirty')).toBeVisible();
  await browser.locator('#tfWsTabs [data-tab="runs"]').click();
  await expect(browser.locator('#confirmModal')).toBeVisible();
  await browser.locator('#confirmModal').getByRole('button', 'Cancel').click();
  await expect(browser.locator('#tfWsTabs [data-tab="config"]')).toHaveAttribute('aria-selected', 'true');

  await browser.evaluate(() => {
    setLanguage('id');
    return null;
  });
  await expect(browser.locator('#tfCfgDirty')).toBeVisible();
  await browser.evaluate(() => {
    setLanguage('en');
    return null;
  });
});

test('Configuration tab for Git workspace browses repo tree, edits file, and commits & pushes via API', async (fx) => {
  const { browser } = fx;
  let commitPayload: { message?: string; base_commit?: string; changes?: { path: string; content: string }[] } | null = null;

  await browser.route('**/api/terraform/workspaces/*/git/**', async (route) => {
    const req = route.request;
    const url = new URL(req.url);
    if (url.pathname.endsWith('/git/files')) {
      await route.fulfill({
        json: {
          commit: 'abc1234567890',
          branch: 'main',
          files: [
            { path: 'main.tf', size: 32 },
            { path: 'variables.tf', size: 18 },
          ],
          truncated: false,
        },
      });
    } else if (url.pathname.endsWith('/git/file')) {
      const p = url.searchParams.get('path') || 'main.tf';
      await route.fulfill({
        json: {
          path: p,
          content: p === 'main.tf' ? 'output "git" { value = "v1" }\n' : 'variable "env" {}\n',
          commit: 'abc1234567890',
        },
      });
    } else if (url.pathname.endsWith('/git/commit')) {
      commitPayload = req.postData ? (JSON.parse(req.postData) as typeof commitPayload) : null;
      await route.fulfill({
        json: { ok: true, commit: 'def9876543210', branch: 'main', pushed: true, changed: 1 },
      });
    } else {
      await route.continue();
    }
  });

  await openWorkspaceDrawer(fx, 'e2e-cfg-git', {
    source: 'git',
    git_url: 'https://github.com/example/tf-repo.git',
    git_branch: 'main',
  });
  await switchDrawerTab(browser, 'config');

  await expect(browser.locator('#tfGitTree [data-gfile="main.tf"]')).toBeVisible();
  await browser.locator('#tfGitTree [data-gfile="main.tf"]').click();
  await expect(browser.locator('#tfGitEditor .CodeMirror')).toBeVisible();
  await expect.poll(() => getCodeMirrorValue(browser, '#tfGitEditor .CodeMirror')).toContain('output "git"');

  await setCodeMirrorValue(browser, 'output "git" { value = "v2" }\n', '#tfGitEditor .CodeMirror');
  await expect(browser.locator('#tfGitCommitBar')).toBeVisible();
  await browser.locator('#tfGitCommitMsg').fill('update output from e2e');
  await browser.locator('#tfGitCommitBtn').click();

  await expect.poll(() => commitPayload?.message).toBe('update output from e2e');
  expect(commitPayload!.changes).toEqual([{ path: 'main.tf', content: 'output "git" { value = "v2" }\n' }]);
});
