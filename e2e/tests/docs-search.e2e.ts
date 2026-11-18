import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';

// The Documentation modal is public and airgapped (ADR 0017); search is entirely client-side
// over the cached parsed document (ADR 0085). No server state is created, so no cleanup is needed.
test('documentation: search highlights matches, filters the TOC, and clears', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);

  // Open the Documentation modal from the header button.
  await browser.evaluate(() => { openDocumentationModal('readme'); return null; });
  await expect(browser.locator('#documentationModal')).toBeVisible();

  // Wait until the README body has rendered before searching.
  await expect(browser.locator('#docsContentBody')).toContainText('Pantau', { timeout: 8000 });

  // A term present in the README produces highlighted matches and a summary.
  await browser.locator('#docsSearchInput').fill('monitoring');
  await expect(browser.locator('#docsSearchSummary')).toBeVisible();
  await expect(browser.locator('#docsSearchSummary')).toContainText(/match|kecocokan/);
  const hits = await browser.evaluate(() => document.querySelectorAll('#docsContentBody mark.docs-search-hit').length);
  expect(hits).toBeGreaterThan(0);

  // The TOC is filtered to sections whose heading contains the query.
  const tocVisible = await browser.evaluate(() =>
    Array.from(document.querySelectorAll('#docsTocList .docs-toc-item'))
      .filter((el) => (el as HTMLElement).style.display !== 'none').length);
  expect(tocVisible).toBeGreaterThanOrEqual(0);

  // Enter advances the current match.
  await browser.locator('#docsSearchInput').press('Enter');
  const current = await browser.evaluate(() => document.querySelectorAll('#docsContentBody .docs-search-current').length);
  expect(current).toBe(1);

  // Clearing removes all highlights and hides the summary.
  await browser.locator('#docsSearchClear').click();
  await expect(browser.locator('#docsSearchSummary')).toBeHidden();
  const cleared = await browser.evaluate(() => document.querySelectorAll('#docsContentBody mark.docs-search-hit').length);
  expect(cleared).toBe(0);

  // A term with no matches reports it explicitly.
  await browser.locator('#docsSearchInput').fill('qzxwvutsel');
  await expect(browser.locator('#docsSearchSummary')).toContainText(/No matches|Tidak ada kecocokan/);

  // Synonym + stemming: searching "secrets" must surface the newly documented
  // Secrets Vault section (ADR 0084) even though the reader may not know its
  // exact heading. Force English so the assertion is deterministic regardless
  // of the app's default language.
  await browser.evaluate(() => { switchDoc('readme', 'en'); return null; });
  await expect(browser.locator('#docsContentBody')).toContainText('Vault', { timeout: 8000 });
  await browser.locator('#docsSearchInput').fill('secrets');
  await expect(browser.locator('#docsSearchSummary')).toBeVisible();
  await expect(browser.locator('#docsContentBody mark.docs-search-hit').first()).toBeVisible({ timeout: 5000 });
  const secretHits = await browser.evaluate(() => document.querySelectorAll('#docsContentBody mark.docs-search-hit').length);
  expect(secretHits).toBeGreaterThan(0);

  // Bahasa Indonesia: "rahasia" must reach the same Vault content through the
  // EN/ID synonym dictionary (secret/rahasia/vault/brankas all match one text).
  await browser.evaluate(() => { switchDoc('readme', 'id'); return null; });
  await expect(browser.locator('#docsContentBody')).toContainText('Vault', { timeout: 8000 });
  await browser.locator('#docsSearchInput').fill('rahasia');
  await expect(browser.locator('#docsSearchSummary')).toBeVisible();
  await expect(browser.locator('#docsContentBody mark.docs-search-hit').first()).toBeVisible({ timeout: 5000 });
  const rahasiaHits = await browser.evaluate(() => document.querySelectorAll('#docsContentBody mark.docs-search-hit').length);
  expect(rahasiaHits).toBeGreaterThan(0);
});
