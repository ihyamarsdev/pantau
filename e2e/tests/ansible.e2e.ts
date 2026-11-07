import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';

test('ansible gate blocks, then a playbook can be created and an invalid YAML is rejected', async ({ app, screen, browser }) => {
  await app.open('/');

  await initApp(screen, browser);

  // Side nav -> Ansible. Gate is off, so the blocked state shows.
  await screen.getByRole('tab', 'Ansible').click();
  const banner = browser.locator('#ansibleGateBanner');
  await expect(banner).toBeVisible();
  await expect(browser.locator('#ansibleGateTitle')).toHaveText('Ansible Gate is off');

  // Enable the Gate via the button and its confirm dialog.
  await browser.locator('#ansibleGateAction').click();
  await expect(browser.locator('#confirmModalBtn')).toBeVisible();
  await browser.locator('#confirmModalBtn').click();
  await expect(banner).toBeHidden();

  // Gate on: the empty playbooks state is visible.
  await expect(browser.locator('#ansiblePlaybooksEmpty')).toBeVisible();
  await expect(screen.getByText('No playbooks yet')).toBeVisible();

  // Create a new playbook, name it, switch to Tasks sub-tab, save.
  // The drawer converts to the saved playbook and stays on Edit with Tasks active.
  await browser.locator('#btnAnsibleNewPlaybook').click();
  await expect(browser.locator('#ansiblePlaybookName')).toBeVisible();
  await browser.locator('#ansiblePlaybookName').fill('e2e-demo');
  await browser.locator('[data-sub="tasks"]').click();
  await browser.locator('[data-ed="save"]').click();

  const editTab = browser.locator('#ansDrawerRoot .ans-drawer-tabs [data-tab="edit"]');
  await expect(editTab).toBeVisible();
  await expect(editTab).toHaveAttribute('aria-selected', 'true');
  await expect(browser.locator('[data-sub="tasks"]')).toHaveAttribute('aria-pressed', 'true');

  // Invalid YAML: the format check returns an error, shown in the footer, and the
  // editor stays open (does not navigate away or close).
  await browser.locator('[data-sub="raw"]').click();
  await browser.evaluate(() => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const cm = (document.querySelector('#ansDrawerRoot .ans-ed-cm .CodeMirror') as any)?.CodeMirror;
    if (cm) cm.setValue('just: a mapping');
  });
  await browser.locator('[data-ed="save"]').click();
  await expect(browser.locator('[data-ed="err"]')).toContainText('list of plays');
  // The playbook name input (in the drawer head) is still on screen.
  await expect(browser.locator('#ansiblePlaybookName')).toBeVisible();
});
