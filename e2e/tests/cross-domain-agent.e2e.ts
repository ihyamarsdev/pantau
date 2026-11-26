import { createServer } from 'node:http';
import type { AddressInfo } from 'node:net';
import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';

type FakePatchLLM = {
  url: string;
  close(): Promise<void>;
};

async function startPatchFakeLLM(patchJson: string): Promise<FakePatchLLM> {
  const server = createServer((req, res) => {
    let raw = '';
    req.on('data', (c) => { raw += c; });
    req.on('end', () => {
      let body: { messages?: { role: string; content?: string }[] } = {};
      try { body = JSON.parse(raw); } catch { /* ignore */ }
      const msgs = body.messages || [];
      const last = msgs[msgs.length - 1];
      const text = last?.role === 'user' && last.content === 'ping'
        ? 'pong'
        : `Here is the proposed fix:\n\n\`\`\`pantau-patch\n${patchJson}\n\`\`\``;
      res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' });
      res.write(`data: ${JSON.stringify({ choices: [{ delta: { content: text }, finish_reason: 'stop' }] })}\n\n`);
      res.write('data: [DONE]\n\n');
      res.end();
    });
  });
  await new Promise<void>((r) => server.listen(0, '127.0.0.1', () => r()));
  const { port } = server.address() as AddressInfo;
  return { url: `http://127.0.0.1:${port}/v1`, close: () => new Promise<void>((r) => server.close(() => r())) };
}

test('Command Palette prefixes & AI Agent interactive CodeMirror diff/dry-run flow', async ({ app, screen, browser }) => {
  const patchPayload = JSON.stringify({
    kind: 'terraform_hcl',
    title: 'Add null_resource b',
    workspace_id: 0,
    target: 'main.tf',
    before: 'resource "null_resource" "a" {}\n',
    after: 'resource "null_resource" "b" {}\n',
  });
  const llm = await startPatchFakeLLM(patchPayload);

  try {
    await app.open('/');
    await initApp(screen, browser);

    // Enable AI Agent pointed at our local fake LLM
    await browser.evaluate(async (baseUrl: string) => {
      await fetch('/api/agent/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enabled: true, provider: 'openai', base_url: baseUrl, api_key: 'test', model: 'fake-model' }),
      });
      const w = window as unknown as { AG?: { init(): void } };
      if (w.AG && typeof w.AG.init === 'function') w.AG.init();
      return null;
    }, llm.url);

    await expect(browser.locator('#agentFab')).toBeVisible();

    // 1. Command Palette with `> ` prefix sends prompt directly to AI Agent Side-Panel
    await browser.evaluate(() => {
      const w = window as unknown as { Palette?: { open(): void } };
      if (w.Palette) w.Palette.open();
      return null;
    });
    await expect(browser.locator('#paletteOverlay.open')).toBeVisible();
    await browser.locator('#paletteInput').fill('> Propose a terraform patch');
    await expect(browser.locator('#paletteList')).toContainText('Propose a terraform patch');
    await browser.locator('#paletteInput').press('Enter');

    // AI Agent opens docked as Side-Panel and renders the pantau-patch card
    await expect(browser.locator('#agentPanel.open.ag-docked')).toBeVisible();
    const previewBtn = browser.locator('.ag-patch-card button', { hasText: /Preview Diff|Pratinjau Diff/ });
    await expect(previewBtn).toBeVisible();

    // 2. Click Preview Diff -> opens CodeMirror diff modal with Apply Changes disabled until Dry-Run
    await previewBtn.click();
    await expect(browser.locator('#agentDiffModal.open')).toBeVisible();
    await expect(browser.locator('#agDiffEditors .CodeMirror')).toHaveCount(2);
    await expect(browser.locator('#agDiffApplyBtn')).toBeDisabled();

    // Switch to Unified Diff view (single CodeMirror instance with mode: diff)
    await browser.locator('#agDiffModeUnified').click();
    await expect(browser.locator('#agDiffEditors .CodeMirror')).toHaveCount(1);

    // Switch back to Side-by-Side
    await browser.locator('#agDiffModeSplit').click();
    await expect(browser.locator('#agDiffEditors .CodeMirror')).toHaveCount(2);

    await browser.locator('#agentDiffModal button[onclick*="closeModal"]').click();
  } finally {
    await llm.close();
  }
});
