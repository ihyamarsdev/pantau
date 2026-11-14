import { createServer } from 'node:http';
import type { AddressInfo } from 'node:net';
import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';

// Stand-in for Hermes' API server: streams a tool-progress frame and a reply for any chat turn.
async function startFakeHermes() {
  const server = createServer((req, res) => {
    if (req.url === '/v1/models') { res.writeHead(200, { 'Content-Type': 'application/json' }); res.end('{"data":[]}'); return; }
    res.writeHead(200, { 'Content-Type': 'text/event-stream' });
    res.write('event: hermes.tool.progress\ndata: {"tool":"systemd_detail","label":"nginx.service","status":"running","toolCallId":"c1"}\n\n');
    res.write('data: {"choices":[{"delta":{"content":"Root cause: **port 80 busy**.\\n```\\nsystemctl restart nginx\\n```"},"finish_reason":null}]}\n\n');
    res.end('data: [DONE]\n\n');
  });
  await new Promise<void>((r) => server.listen(0, '127.0.0.1', r));
  return { url: `http://127.0.0.1:${(server.address() as AddressInfo).port}`, close: () => new Promise<void>((r) => server.close(() => r())) };
}

test('agent: button always shown, off state points to Settings, chat streams tool + reply, MCP exec waits for approval', async ({ app, screen, browser }) => {
  const hermes = await startFakeHermes();
  try {
    await app.open('/');
    await initApp(screen, browser);
    // Button is always there; while the agent is off the panel points to Settings.
    await expect(browser.locator('#agentFab')).toBeVisible();
    await browser.locator('#agentFab').click();
    await expect(browser.locator('#agentLog')).toContainText('Settings');
    await browser.locator('#agentFab').click();

    // Enable through the Settings → AI Agent panel.
    await browser.evaluate(() => (window as any).openSettingsModal('agent'));
    await expect(browser.locator('#agUrl')).toBeVisible();
    await browser.locator('#agEnabled').click();
    await browser.locator('#agUrl').fill(hermes.url);
    await browser.locator('#agKey').fill('k-test');
    await browser.locator('#agentSettingsBody .btn-primary').first().click();
    await expect(browser.locator('#agentFab')).toBeVisible();
    await expect(browser.locator('#agentSettingsBody .ag-snippet').last()).toContainText('mcp_servers:');

    // Floating chat: tool progress line + markdown reply.
    await browser.evaluate(() => (window as any).closeModal('settingsModal'));
    await browser.locator('#agentFab').click();
    await browser.locator('#agentInput').fill('nginx is down');
    await browser.locator('#agentSend').click();
    await expect(browser.locator('#agentLog .ag-tool')).toContainText('systemd_detail');
    await expect(browser.locator('#agentLog .ag-bot b')).toHaveText('port 80 busy');
    await expect(browser.locator('#agentLog .ag-bot pre')).toContainText('systemctl restart nginx');

    // Hermes → Pantau: an exec_command tool call must block on an operator approval card.
    await browser.evaluate(async () => {
      const cfg = await (await fetch('/api/agent/settings')).json();
      const h = await (await fetch('/api/hosts', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: 'e2e', host: '127.0.0.1', port: 1, user: 'x' }) })).json();
      const rpc = (name: string, args: unknown) => fetch('/mcp', { method: 'POST', headers: { Authorization: 'Bearer ' + cfg.mcp_token, 'Content-Type': 'application/json' }, body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name, arguments: args } }) }).then((r) => r.text());
      (window as any).__mcp = rpc('exec_command', { host_id: h.id, command: 'systemctl restart nginx' });
      return null;
    });
    await expect(browser.locator('#agentApprovals .ag-appr-exec pre')).toContainText('systemctl restart nginx');
    await browser.locator('#agentApprovals .ag-appr-btns button').first().click(); // Deny
    await expect(browser.locator('#agentApprovals .ag-appr')).toBeHidden();
    // The blocked MCP call returns an error carrying the denial, and nothing was executed.
    const denied = await browser.evaluate(async () => (await (window as any).__mcp) as string);
    expect(denied).toContain('the operator denied this action');
  } finally {
    await hermes.close();
  }
});

test('agent: "Approve for this session" runs the next change without a card until revoked', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);
  // Agent on, so the card poller runs. The host is unreachable (port 1): approval is what is under test, not the SSH call.
  await browser.evaluate(async () => {
    await fetch('/api/agent/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ enabled: true }) });
    (window as any).AG.init();
    const cfg = await (await fetch('/api/agent/settings')).json();
    const h = await (await fetch('/api/hosts', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: 'e2e', host: '127.0.0.1', port: 1, user: 'x' }) })).json();
    (window as any).__rpc = (cmd: string) => fetch('/mcp', { method: 'POST', headers: { Authorization: 'Bearer ' + cfg.mcp_token, 'Content-Type': 'application/json' }, body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name: 'exec_command', arguments: { host_id: h.id, command: cmd } } }) }).then((r) => r.text());
    (window as any).__first = (window as any).__rpc('echo one');
    return null;
  });

  await expect(browser.locator('#agentApprovals .ag-appr-exec pre')).toContainText('echo one');
  await browser.locator('#agentApprovals .ag-appr-btns button').nth(1).click(); // Approve for this session
  await expect(browser.locator('#agentApprovals .ag-grants')).toContainText('e2e');

  // Second call: no card may appear; it must reach the SSH step (fails: unreachable) instead of waiting.
  const second = await browser.evaluate(async () => (await (window as any).__rpc('echo two')) as string);
  expect(second).not.toContain('operator');
  expect(second).not.toContain('no operator decision');
  await expect(browser.locator('#agentApprovals .ag-appr')).toBeHidden();

  await browser.locator('#agentApprovals .ag-grants a').click(); // Revoke
  await expect(browser.locator('#agentApprovals .ag-grants')).toBeHidden();
  await browser.evaluate(() => { (window as any).__third = (window as any).__rpc('echo three'); return null; });
  await expect(browser.locator('#agentApprovals .ag-appr-exec pre')).toContainText('echo three');
  await browser.locator('#agentApprovals .ag-appr-btns button').first().click(); // Deny
});

test('agent: infrastructure change card has no "approve for this session" button', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);
  await browser.evaluate(async () => {
    await fetch('/api/agent/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ enabled: true, approval: 'auto' }) });
    (window as any).AG.init();
    const cfg = await (await fetch('/api/agent/settings')).json();
    const h = await (await fetch('/api/hosts', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: 'e2e', host: '127.0.0.1', port: 1, user: 'x' }) })).json();
    const yaml = 'apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n';
    (window as any).__apply = fetch('/mcp', { method: 'POST', headers: { Authorization: 'Bearer ' + cfg.mcp_token, 'Content-Type': 'application/json' }, body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name: 'kube_apply', arguments: { target_id: h.id, yaml } } }) }).then((r) => r.text());
    return null;
  });

  // Even in auto-approve mode the card appears, titled as an infrastructure change, with only Deny and Approve.
  await expect(browser.locator('#agentApprovals .ag-appr-exec')).toContainText('Infrastructure change');
  await expect(browser.locator('#agentApprovals .ag-appr-exec pre')).toContainText('kind: ConfigMap');
  await expect(browser.locator('#agentApprovals .ag-appr-btns button')).toHaveCount(2);
  await browser.locator('#agentApprovals .ag-appr-btns button').first().click(); // Deny
  const out = await browser.evaluate(async () => (await (window as any).__apply) as string);
  expect(out).toContain('the operator denied this action');
});
