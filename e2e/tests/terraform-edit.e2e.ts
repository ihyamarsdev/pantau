import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { openWorkspaceDrawer, readWorkspace, setEditorValue } from './terraform-helpers.js';

const WS = 'e2e-edit';

test('the Configuration tab edits, adds and deletes inline files and saves them', async (fx) => {
  const { browser } = fx;
  await openWorkspaceDrawer(fx, WS);

  await browser.locator('#tfWsTabs').getByRole('tab', 'Configuration').click();
  await expect(browser.locator('#tfCfgFiles')).toBeVisible();
  await expect(browser.locator('#tfCfgFiles [data-file="main.tf"]')).toBeVisible();
  await expect(browser.locator('#tfCfgEditor .CodeMirror')).toBeVisible();
  await expect(browser.locator('#tfCfgDirty')).toBeHidden();

  // The editor is roomy: the editor pane takes a large part of the drawer.
  const size = await browser.evaluate(() => {
    const r = document.querySelector('#tfCfgEditor')!.getBoundingClientRect();
    return { width: r.width, height: r.height };
  });
  expect(size.width).toBeGreaterThan(500);
  expect(size.height).toBeGreaterThan(300);

  // Edit main.tf: the dirty marker shows, Save stores it and clears the marker.
  await setEditorValue(browser, 'output "hello" { value = "edited" }\n');
  await expect(browser.locator('#tfCfgDirty')).toBeVisible();
  await browser.locator('#tfCfgSave').click();
  await expect(browser.locator('#tfCfgDirty')).toBeHidden();
  const edited = await readWorkspace(browser, WS);
  expect(edited!.files['main.tf']).toContain('edited');

  // Add a second file, save, then delete it again.
  await browser.locator('#tfCfgNewName').fill('vars.tf');
  await browser.locator('#tfCfgAddFile').click();
  await expect(browser.locator('#tfCfgFiles [data-file="vars.tf"]')).toBeVisible();
  await browser.locator('#tfCfgSave').click();
  await expect(browser.locator('#tfCfgDirty')).toBeHidden();
  expect(Object.keys((await readWorkspace(browser, WS))!.files).sort()).toEqual(['main.tf', 'vars.tf']);

  await browser.locator('#tfCfgFiles [data-del="vars.tf"]').click();
  await browser.locator('#confirmModalBtn').click();
  await browser.locator('#tfCfgSave').click();
  await expect(browser.locator('#tfCfgDirty')).toBeHidden();
  expect(Object.keys((await readWorkspace(browser, WS))!.files)).toEqual(['main.tf']);
});

test('unsaved configuration changes are guarded when leaving the tab', async (fx) => {
  const { browser } = fx;
  await openWorkspaceDrawer(fx, WS);

  await browser.locator('#tfWsTabs').getByRole('tab', 'Configuration').click();
  await expect(browser.locator('#tfCfgEditor .CodeMirror')).toBeVisible();
  await setEditorValue(browser, '# unsaved\n');
  await expect(browser.locator('#tfCfgDirty')).toBeVisible();

  // Cancel keeps the user on the Configuration tab with the edit intact.
  await browser.locator('#tfWsTabs').getByRole('tab', 'Runs').click();
  await expect(browser.locator('#confirmModal')).toBeVisible();
  await browser.locator('#confirmModal').getByRole('button', 'Cancel').click();
  await expect(browser.locator('#tfWsTabs').getByRole('tab', 'Configuration')).toHaveAttribute('aria-selected', 'true');
  await expect(browser.locator('#tfCfgDirty')).toBeVisible();

  // Switching the UI language must not re-render the tab and drop the unsaved edit.
  await browser.evaluate(() => {
    setLanguage('id');
    return null;
  });
  await expect(browser.locator('#tfCfgDirty')).toBeVisible();
  await browser.evaluate(() => {
    setLanguage('en');
    return null;
  });

  // Confirming leaves, and the unsaved text is gone when the tab is opened again.
  await browser.locator('#tfWsTabs').getByRole('tab', 'Runs').click();
  await browser.locator('#confirmModalBtn').click();
  await expect(browser.locator('#tfWsTabs').getByRole('tab', 'Runs')).toHaveAttribute('aria-selected', 'true');
  await browser.locator('#tfWsTabs').getByRole('tab', 'Configuration').click();
  await expect(browser.locator('#tfCfgDirty')).toBeHidden();
});

test('workspace settings are edited in place and saved', async (fx) => {
  const { browser } = fx;
  await openWorkspaceDrawer(fx, WS);

  await browser.locator('#tfWsTabs').getByRole('tab', 'Settings').click();
  await expect(browser.locator('#tfSetName')).toBeVisible();
  await browser.locator('#tfSetDesc').fill('edited by e2e');
  await browser.locator('#tfSetEnv').fill('production');
  await browser.locator('#tfSetTags').fill('alpha, beta');
  await expect(browser.locator('#tfSetDirty')).toBeVisible();
  await browser.locator('#tfSetSave').click();
  await expect(browser.locator('#tfSetDirty')).toBeHidden();

  const stored = await readWorkspace(browser, WS);
  expect(stored!.description).toBe('edited by e2e');
  expect(stored!.environment).toBe('production');
  expect(stored!.tags).toEqual(['alpha', 'beta']);
  // The drawer header reflects the new environment without reopening.
  await expect(browser.locator('#tfWsHeadBadges')).toContainText('production');

  // A blank name is rejected by the form and nothing is stored.
  await browser.locator('#tfSetName').fill('');
  await browser.locator('#tfSetSave').click();
  await expect(browser.locator('#tfSetError')).toBeVisible();
  expect((await readWorkspace(browser, WS))!.description).toBe('edited by e2e');
});
