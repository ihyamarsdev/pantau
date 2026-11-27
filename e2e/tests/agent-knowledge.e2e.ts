import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';

type PageGlobals = { AG: { init(): void } };

// Procedural memory in the browser: a saved runbook is found by the Knowledge & Skills search, and a custom skill
// can be added and switched off from Settings → AI Agent. The rows are created and removed through the same
// endpoints the UI uses, so the test leaves nothing behind.
test('agent knowledge: a saved runbook is searchable, and a custom skill can be added and switched off', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);

  const runbook = await browser.evaluate(async () => {
    const r = await fetch('/api/agent/knowledge', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ kind: 'runbook', title: 'e2e crashloop fix', domain: 'kubernetes', trigger_summary: 'e2e pod crashloops',
        steps: ['read the logs'], verify: ['pod is Running'], rollback: [], tags: ['e2e'] }),
    });
    return r.json();
  });
  expect(runbook.id).toBeGreaterThan(0);

  await browser.evaluate(() => (window as unknown as PageGlobals).AG.init());
  await browser.evaluate(() => {
    (window as unknown as { openSettingsModal(s: string): void }).openSettingsModal('agent');
    return null;
  });
  await expect(browser.locator('#agKnowledgeCard')).toBeVisible();

  // Search finds the saved runbook.
  await browser.locator('#agKSearch').fill('crashloop');
  await expect(browser.locator('#agKList')).toContainText('e2e crashloop fix');

  // A custom skill is added in the form and then switched off.
  await browser.locator('#agSkName').fill('e2e docker skill');
  await browser.locator('#agSkContent').fill('Prefer compose files.');
  await browser.locator('.ag-k-form button').click();
  await expect(browser.locator('#agSkillList')).toContainText('e2e docker skill');
  await browser.locator('#agSkillList input[type="checkbox"]').first().click();
  await expect(browser.locator('#agSkillList input[type="checkbox"]').first()).not.toBeChecked();

  // Cleanup through the API: the skill first (found by name), then the runbook.
  await browser.evaluate(async (id: number) => {
    const list = await (await fetch('/api/agent/skills/custom')).json();
    for (const sk of list.skills || []) {
      if (sk.name === 'e2e docker skill') await fetch('/api/agent/skills/custom/' + sk.id, { method: 'DELETE' });
    }
    await fetch('/api/agent/knowledge/runbooks/' + id, { method: 'DELETE' });
    return null;
  }, runbook.id);
});
