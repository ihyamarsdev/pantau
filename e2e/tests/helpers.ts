import { expect } from 'e2e';

type Screen = {
  getByRole(role: string, name?: string): { click(): Promise<void> };
  getByText(label: string): { click(): Promise<void> };
};
type BrowserHandle = {
  locator(selector: string): {
    isVisible(): Promise<boolean>;
    waitFor(state: 'visible', options?: { timeout?: number }): Promise<void>;
    fill(value: string): Promise<void>;
    click(): Promise<void>;
  };
};

/**
 * Called at the start of every e2e test (each opens a fresh page).
 * Logs in with the default dev password, then dismisses the first-run
 * "Restore Pantau from Backup" wizard that a fresh DB opens after login.
 */
export async function initApp(screen: Screen, browser: BrowserHandle): Promise<void> {
  const password = browser.locator('#loginPassword');
  if (await password.isVisible()) {
    await password.fill('admin');
    await screen.getByRole('button', 'Login').click();
  }

  // A fresh DB opens the restore wizard right after login; wait for it (it can be slow on a loaded machine).
  const modal = browser.locator('#restoreModal');
  const open = await modal.waitFor({ state: 'visible', timeout: 8000 }).then(() => true).catch(() => false);
  if (open) {
    await screen.getByText('Start Fresh').click();
    await screen.getByText('Start from Scratch (Fresh)').click();
    await expect(modal).toBeHidden();
  }
}
