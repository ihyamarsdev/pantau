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
  evaluate<T, A>(fn: (arg: A) => T | Promise<T>, arg: A): Promise<T>;
  unrouteAll(): Promise<void>;
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

/**
 * Removes the hosts a test created, so the next test starts from a clean fleet.
 *
 * The whole e2e run shares one Pantau database, so a host left behind by one test is still there
 * for the next one. That is what makes a test that counts hosts, or that picks a host by name,
 * fail depending on which tests ran before it.
 *
 * The hosts are removed by the name the test gave them, so a test that seeded nothing removes
 * nothing and cannot touch a host another test is using.
 */
export async function deleteHosts(browser: BrowserHandle, names: string[]): Promise<void> {
  if (!names.length) return;
  // A test that stubbed the host API would otherwise swallow the delete, so the stubs go first.
  // unrouteAll() takes every route the browser fixture holds; the cleanup is the last thing a test
  // does, so no route is needed after it.
  await browser.unrouteAll();
  await browser.evaluate(async (wanted: string[]) => {
    const res = await fetch('/api/hosts');
    const hosts = (await res.json()) as { id: number; name: string }[];
    for (const host of hosts) {
      if (wanted.includes(host.name)) {
        await fetch('/api/hosts/' + host.id, { method: 'DELETE' });
      }
    }
  }, names);
}

/**
 * A host name that no other test uses, so a host from another test can never be mistaken for it.
 */
export function uniqueHostName(prefix: string): string {
  return `${prefix}-${Date.now()}-${Math.floor(Math.random() * 100000)}`;
}
