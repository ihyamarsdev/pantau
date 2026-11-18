import { createServer } from 'node:http';
import type { AddressInfo } from 'node:net';
import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { deleteHosts, initApp } from './helpers.js';

type ChatMessage = { role: string; content?: string; tool_call_id?: string };

type FakeLLM = {
  url: string;
  /** The host the scripted tools act on; the test creates it. */
  hostId: number;
  /** How many chat turns (not the settings test ping) the fake has answered. */
  chats: number;
  close(): Promise<void>;
};

/** One tool call the fake asks for: the tool name and its arguments (built from the fake, which knows the host). */
type ScriptedCall = { name: string; args: (f: FakeLLM) => unknown };

// Stand-in for an OpenAI-compatible LLM endpoint: it answers POST /v1/chat/completions with a scripted
// SSE stream. The script follows the request: the settings test ping gets a short text, a chat turn gets
// the next scripted tool call (exec_command on the test host by default), and a turn that already carries
// every tool result gets the final text.
async function startFakeLLM(toolCalls = 1, calls?: ScriptedCall[]): Promise<FakeLLM> {
  const fake: FakeLLM = { url: '', hostId: 0, chats: 0, close: async () => {} };
  const server = createServer((req, res) => {
    let raw = '';
    req.on('data', (c) => { raw += c; });
    req.on('end', () => {
      let body: { messages?: ChatMessage[] } = {};
      try { body = JSON.parse(raw); } catch { /* an unparsable body is answered like an empty one */ }
      const messages = body.messages || [];
      const last = messages[messages.length - 1];
      const isPing = last?.role === 'user' && last.content === 'ping';
      const results = messages.filter((m) => m.role === 'tool');
      if (!isPing) fake.chats++;

      res.writeHead(200, { 'Content-Type': 'text/event-stream' });
      const chunk = (o: unknown) => res.write('data: ' + JSON.stringify(o) + '\n\n');
      if (isPing) {
        chunk({ choices: [{ delta: { content: 'pong' }, finish_reason: null }] });
        chunk({ choices: [{ delta: {}, finish_reason: 'stop' }] });
      } else if (results.length < toolCalls) {
        const call = calls?.[results.length] ?? {
          name: 'exec_command',
          args: () => ({ host_id: fake.hostId, command: 'systemctl status nginx' })
        };
        chunk({
          choices: [{
            delta: {
              tool_calls: [{ index: 0, id: 'call_' + (results.length + 1), type: 'function', function: { name: call.name, arguments: JSON.stringify(call.args(fake)) } }]
            },
            finish_reason: null
          }]
        });
        chunk({ choices: [{ delta: {}, finish_reason: 'tool_calls' }] });
      } else {
        const result = results[results.length - 1]?.content || '';
        const text = /denied/i.test(result)
          ? 'The command was not run: the operator denied it.'
          : /refused|error|not connected/i.test(result)
            ? 'The command failed on the host.'
            : 'The command finished without an error.';
        chunk({ choices: [{ delta: { content: text }, finish_reason: null }] });
        chunk({ choices: [{ delta: {}, finish_reason: 'stop' }] });
      }
      res.end('data: [DONE]\n\n');
    });
  });
  await new Promise<void>((r) => server.listen(0, '127.0.0.1', r));
  fake.url = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  fake.close = () => new Promise<void>((r) => server.close(() => r()));
  return fake;
}

type PageGlobals = {
  openSettingsModal(tab: string): void;
  closeModal(id: string): void;
  AG: { init(): void; newChat(): void };
};

test('agent: settings configure a provider, the chat streams a tool, and an exec waits for approval', async ({ app, screen, browser }) => {
  const llm = await startFakeLLM();
  try {
    await app.open('/');
    await initApp(screen, browser);

    // The button is always there; while the agent is off the panel points to Settings.
    await browser.evaluate(async () => {
      await fetch('/api/agent/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ enabled: false }) });
      (window as unknown as PageGlobals).AG.init();
      return null;
    });
    await expect(browser.locator('#agentFab')).toBeVisible();
    await browser.locator('#agentFab').click();
    await expect(browser.locator('#agentLog')).toContainText('Settings');
    await browser.locator('#agentFab').click();

    // A host for the exec_command call.
    llm.hostId = (await browser.evaluate(async () => {
      const h = await (await fetch('/api/hosts', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: 'e2e', host: '127.0.0.1', port: 1, user: 'x' }) })).json();
      return h.id as number;
    })) as number;

    // Configure the agent in Settings → AI Agent.
    await browser.evaluate(() => { (window as unknown as PageGlobals).openSettingsModal('agent'); return null; });
    await expect(browser.locator('#agProvider')).toBeVisible();
    await browser.locator('#agEnabled').click();
    await browser.locator('#agProvider').selectOption({ value: 'openai' });
    await browser.locator('#agBaseUrl').fill(llm.url);
    await browser.locator('#agKey').fill('k-test');
    await browser.locator('#agModel').fill('gpt-4o');
    await browser.locator('#agSave').click();
    await expect(browser.locator('#agentFab')).toBeVisible();

    // Test connection: the provider answers the tiny ping.
    await browser.locator('#agTest').click();
    await expect(browser.locator('#agTestResult')).toContainText('answered');

    // The floating chat: the tool line shows the running call.
    await browser.evaluate(() => { (window as unknown as PageGlobals).closeModal('settingsModal'); return null; });
    await browser.locator('#agentFab').click();
    await browser.locator('#agentInput').fill('is nginx up?');
    await browser.locator('#agentSend').click();
    await expect(browser.locator('#agentLog .ag-tool')).toContainText('exec_command');
    await expect(browser.locator('#agentLog .ag-user')).toContainText('is nginx up?');

    // The exec_command call waits for the operator: the card shows the command, and the model gets no
    // second request before the decision.
    await expect(browser.locator('#agentApprovals .ag-appr-exec pre')).toContainText('systemctl status nginx');
    expect(llm.chats).toBe(1);
    await browser.locator('#agentApprovals .ag-appr-btns button').first().click(); // Deny
    await expect(browser.locator('#agentApprovals .ag-appr')).toBeHidden();

    // The final reply renders and mentions the denial; the tool line is marked as failed.
    await expect(browser.locator('#agentLog .ag-tool-error')).toContainText('exec_command');
    await expect(browser.locator('#agentLog .ag-bot').last()).toContainText('denied');

    // The key is write-only: the settings show the saved badge, the input is empty and the key is
    // nowhere on the page.
    await browser.evaluate(() => { (window as unknown as PageGlobals).openSettingsModal('agent'); return null; });
    await expect(browser.locator('#agKeySaved')).toContainText('Saved');
    const shown = (await browser.evaluate(() => {
      const el = document.getElementById('agKey') as HTMLInputElement | null;
      return { value: el ? el.value : '', page: document.body.innerText };
    })) as { value: string; page: string };
    expect(shown.value).toBe('');
    expect(shown.page).not.toContain('k-test');
  } finally {
    await llm.close();
    // The whole run shares one database, so the host this test added is removed again.
    await deleteHosts(browser, ['e2e']);
  }
});

test('agent: "Approve for this session" runs the next command on that host without a card, until revoked', async ({ app, screen, browser }) => {
  const llm = await startFakeLLM(2);
  try {
    await app.open('/');
    await initApp(screen, browser);

    llm.hostId = (await browser.evaluate(async () => {
      const h = await (await fetch('/api/hosts', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: 'e2e-grant', host: '127.0.0.1', port: 1, user: 'x' }) })).json();
      return h.id as number;
    })) as number;

    // The agent is on and points at the fake provider.
    await browser.evaluate(async (base) => {
      await fetch('/api/agent/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ enabled: true, provider: 'openai', base_url: base, api_key: 'k-test', model: 'gpt-4o' }) });
      const g = window as unknown as PageGlobals;
      g.AG.init();
      g.AG.newChat();
      return null;
    }, llm.url);

    await browser.locator('#agentFab').click();
    await browser.locator('#agentInput').fill('check nginx twice');
    await browser.locator('#agentSend').click();

    // The first command waits for the operator.
    await expect(browser.locator('#agentApprovals .ag-appr-exec pre')).toContainText('systemctl status nginx');
    await browser.locator('#agentApprovals .ag-appr-btns button').nth(1).click(); // Approve for this session
    await expect(browser.locator('#agentApprovals .ag-grants')).toContainText('e2e');

    // The second command runs under the grant: no new card, the turn ends with the model's answer.
    await expect(browser.locator('#agentLog .ag-bot').last()).toContainText('command');
    await expect(browser.locator('#agentApprovals .ag-appr')).toBeHidden();
    expect(llm.chats).toBe(3);

    // Revoking ends the grant.
    await browser.locator('#agentApprovals .ag-grants a').click();
    await expect(browser.locator('#agentApprovals .ag-grants')).toBeHidden();
  } finally {
    await llm.close();
    // The whole run shares one database, so the host this test added is removed again.
    await deleteHosts(browser, ['e2e-grant']);
  }
});

test('agent: a chat without a provider shows the error and points at Settings', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);

  // The agent is on, but no provider and no model are configured.
  await browser.evaluate(async () => {
    await fetch('/api/agent/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ enabled: true, provider: '', model: '' }) });
    (window as unknown as PageGlobals).AG.init();
    return null;
  });

  await browser.locator('#agentFab').click();
  await browser.locator('#agentInput').fill('hello');
  await browser.locator('#agentSend').click();

  // The error names the missing configuration and offers the way to fix it.
  await expect(browser.locator('#agentLog .ag-err')).toContainText('Settings');
  await browser.locator('#agentLog .ag-err .btn').click();
  await expect(browser.locator('#agProvider')).toBeVisible();
});

test('agent: the button and panel sit above an open modal, and the panel expands to a wider chat', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);

  // Settings is a modal; the agent button must still be the element under the pointer.
  await browser.evaluate(() => { (window as unknown as PageGlobals).openSettingsModal('agent'); return null; });
  await expect(browser.locator('#agProvider')).toBeVisible();
  const fabOnTop = (await browser.evaluate(() => {
    const r = document.getElementById('agentFab')!.getBoundingClientRect();
    return !!document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2)?.closest('#agentFab');
  })) as boolean;
  expect(fabOnTop).toBe(true);

  await browser.locator('#agentFab').click();
  await expect(browser.locator('#agentPanel')).toBeVisible();

  // Expanded: the panel widens, and the approval cards move above it to its right edge.
  const panelWidth = async () => (await browser.evaluate(() => document.getElementById('agentPanel')!.getBoundingClientRect().width)) as number;
  const normalWidth = await panelWidth();
  await browser.locator('#agentExpand').click();
  expect(await panelWidth()).toBeGreaterThan(800);
  const cardsOnRightEdge = (await browser.evaluate(() => {
    const right = document.getElementById('agentApprovals')!.getBoundingClientRect().right;
    return Math.abs(right - (document.documentElement.clientWidth - 24)) < 1;
  })) as boolean;
  expect(cardsOnRightEdge).toBe(true);

  // Collapsed again: back to the normal width.
  await browser.locator('#agentExpand').click();
  expect(await panelWidth()).toBeCloseTo(normalWidth, 0);
});

test('agent: the bubble is a solid, draggable anchor, and a click outside closes the chat', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);

  const fabBox = async () => (await browser.evaluate(() => {
    const r = document.getElementById('agentFab')!.getBoundingClientRect();
    return { x: r.left, y: r.top, w: r.width, h: r.height };
  })) as { x: number; y: number; w: number; h: number };

  // The first click waits for the widget to boot after the first-run wizard; the second closes the chat.
  await browser.locator('#agentFab').click();
  await expect(browser.locator('#agentPanel')).toBeVisible();
  await browser.locator('#agentFab').click();
  await expect(browser.locator('#agentPanel')).toBeHidden();

  // A filled bubble: --accent was never a token, so the old rule left it transparent.
  const fabBg = (await browser.evaluate(() => getComputedStyle(document.getElementById('agentFab')!).backgroundColor)) as string;
  expect(fabBg).not.toMatch(/rgba\(0, 0, 0, 0\)/);
  // The chat animates in and out instead of appearing.
  const panelTransition = (await browser.evaluate(() => getComputedStyle(document.getElementById('agentPanel')!).transitionProperty)) as string;
  expect(panelTransition).toContain('opacity');

  // Drag the bubble up and to the left by (400, 320).
  const before = await fabBox();
  const cx = before.x + before.w / 2, cy = before.y + before.h / 2;
  await browser.mouse.move(cx, cy);
  await browser.mouse.down();
  await browser.mouse.move(cx - 200, cy - 160);
  await browser.mouse.move(cx - 400, cy - 320);
  await browser.mouse.up();
  const after = await fabBox();
  expect(after.x).toBeLessThan(before.x - 300);
  expect(after.y).toBeLessThan(before.y - 220);

  // Dragging is not a click: the chat stays closed until the bubble is tapped.
  await expect(browser.locator('#agentPanel')).toBeHidden();
  await browser.locator('#agentFab').click();
  await expect(browser.locator('#agentPanel')).toBeVisible();

  // The chat docks to the bubble and never covers it.
  const coversBubble = (await browser.evaluate(() => {
    const f = document.getElementById('agentFab')!.getBoundingClientRect();
    const p = document.getElementById('agentPanel')!.getBoundingClientRect();
    return f.left < p.right && f.right > p.left && f.top < p.bottom && f.bottom > p.top;
  })) as boolean;
  expect(coversBubble).toBe(false);

  // A click outside the chat closes it.
  await browser.mouse.move(320, 300);
  await browser.mouse.down();
  await browser.mouse.up();
  await expect(browser.locator('#agentPanel')).toBeHidden();

  // The bubble keeps the place it was dropped in across a reload.
  await app.open('/');
  await initApp(screen, browser);
  await browser.locator('#agentFab').waitFor({ state: 'visible', timeout: 10000 });
  const reloaded = await fabBox();
  expect(Math.abs(reloaded.x - after.x)).toBeLessThan(2);
  expect(Math.abs(reloaded.y - after.y)).toBeLessThan(2);
});

test('agent: the read tools of the recent features answer in the chat without an approval card', async ({ app, screen, browser }) => {
  // One chat turn per tool, in order: the VPN networks, the stored package update check of a host, and the
  // Gateway CLI versions. All three are reads, so none of them may show an approval card.
  const llm = await startFakeLLM(3, [
    { name: 'vpn_networks', args: () => ({ what: 'networks' }) },
    { name: 'pkg_updates', args: (f) => ({ host_id: f.hostId }) },
    { name: 'gateway_cli', args: () => ({ target: 'ansible' }) }
  ]);
  try {
    await app.open('/');
    await initApp(screen, browser);

    // The agent is on and points at the fake provider.
    await browser.evaluate(async (base) => {
      await fetch('/api/agent/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ enabled: true, provider: 'openai', base_url: base, api_key: 'k-test', model: 'gpt-4o', approval: 'auto' }) });
      const g = window as unknown as PageGlobals;
      g.AG.init();
      g.AG.newChat();
      return null;
    }, llm.url);

    // A host never checked for package updates: the tool must answer checked:false rather than invent a result.
    llm.hostId = (await browser.evaluate(async () => {
      const h = await (await fetch('/api/hosts', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: 'e2e-recent', host: '127.0.0.1', port: 1, user: 'x' }) })).json();
      return h.id as number;
    })) as number;

    await browser.locator('#agentFab').click();
    await browser.locator('#agentInput').fill('check the vpn, the updates and the gateway');
    await browser.locator('#agentSend').click();

    // Every tool of the run is shown as a finished line, and no card ever asks: all three are reads.
    await expect(browser.locator('#agentLog .ag-tool-done')).toHaveCount(3, { timeout: 60000 });
    await expect(browser.locator('#agentApprovals .ag-appr')).toBeHidden();
    // Three tool turns plus the final answer: the loop asked the provider once per tool and once to conclude.
    expect(llm.chats).toBe(4);
  } finally {
    await llm.close();
    // The whole run shares one database, so the host this test added is removed again.
    await deleteHosts(browser, ['e2e-recent']);
  }
});

test('agent: a note can be filed under a domain from the Memory panel', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);

  await browser.evaluate(() => { (window as unknown as PageGlobals).openSettingsModal('agent'); return null; });
  await expect(browser.locator('#agMemory')).toBeVisible();

  // The scope picker chooses what the note is about: a fleet note is the default, a domain note is a choice.
  await browser.locator('#agMemScope').selectOption({ value: 'domain' });
  await expect(browser.locator('#agMemDomain')).toBeVisible();
  await browser.locator('#agMemDomain').selectOption({ value: 'security' });
  await browser.locator('#agMemText').fill('fail2ban bans after 5 failed logins');
  await browser.locator('#agMemAdd').click();

  // The note is listed with its scope, and it is stored as a domain note.
  await expect(browser.locator('#agMemory')).toContainText('fail2ban bans after 5 failed logins');
  await expect(browser.locator('#agMemory')).toContainText('security');
  const scope = (await browser.evaluate(async () => {
    const r = await fetch('/api/agent/memory');
    const j = await r.json();
    const n = (j.notes || []).find((x: { content: string }) => x.content.includes('fail2ban'));
    return n ? { domain: n.domain || '', host_id: n.host_id || 0, source: n.source } : null;
  })) as { domain: string; host_id: number; source: string } | null;
  expect(scope).not.toBeNull();
  expect(scope?.domain).toBe('security');
  expect(scope?.host_id).toBe(0);
  expect(scope?.source).toBe('operator');

  // The note is removed again, so the shared database stays as it was.
  await browser.evaluate(async () => {
    const r = await fetch('/api/agent/memory');
    const j = await r.json();
    const n = (j.notes || []).find((x: { content: string }) => x.content.includes('fail2ban'));
    if (n) await fetch('/api/agent/memory?id=' + n.id, { method: 'DELETE' });
    return null;
  });
});
