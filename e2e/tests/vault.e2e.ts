import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';

// Vault stores secrets, templates and sync targets in the shared Pantau DB. The resources this
// test creates are deleted by id in `finally`, so a failing test never leaves them behind.
test('vault: secrets are masked until revealed, templates redact on render, and targets carry a drift badge', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);

  // 1. The Secrets section opens from the side nav.
  await browser.locator('#navItemSecrets').click();
  await expect(browser.locator('#sectionSecrets')).toBeVisible();
  await expect(browser.locator('#vaultStatusStrip')).toBeVisible();

  const key = 'E2E_TOKEN_' + Date.now();
  const value = 'super-secret-value-' + Date.now();
  let secretId = 0;
  let templateId = 0;
  let targetId = 0;

  try {
    // 2. Create a secret via the API. The list never contains the plaintext value.
    const created = await browser.evaluate(async ({ key, value }) => {
      const res = await fetch('/api/vault/secrets', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ key, value, scope: 'global', scope_ref: '', expires_at: null, rotation_days: 0 }),
      });
      const body = await res.json().catch(() => ({}));
      return { status: res.status, id: body.id as number };
    }, { key, value });
    expect(created.status).toBe(201);
    secretId = created.id;
    expect(secretId).toBeGreaterThan(0);

    const listed = await browser.evaluate(async () => {
      const res = await fetch('/api/vault/secrets');
      const text = await res.text();
      return { status: res.status, text, hasValue: text.includes('super-secret-value-') };
    });
    expect(listed.status).toBe(200);
    expect(listed.hasValue).toBe(false);
    expect(listed.text).toContain(key);

    // Revealing returns the plaintext exactly once, and writes an audit entry.
    const revealed = await browser.evaluate(async (id: number) => {
      const res = await fetch('/api/vault/secrets/' + id + '/reveal');
      const body = await res.json().catch(() => ({}));
      return { status: res.status, value: body.value as string };
    }, secretId);
    expect(revealed.status).toBe(200);
    expect(revealed.value).toBe(value);

    const audit = await browser.evaluate(async () => {
      const res = await fetch('/api/vault/audit');
      return (await res.json()) as { action: string; detail: string }[];
    });
    expect(audit.some((a) => a.action === 'secret.reveal')).toBe(true);

    // 3. A template referencing the key renders with [REDACTED], and validation is clean.
    const tmpl = await browser.evaluate(async ({ key }) => {
      const res = await fetch('/api/vault/templates', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: 'E2E_TPL_' + Date.now(), body: 'token={{ vault.' + key + ' }}' }),
      });
      const body = await res.json().catch(() => ({}));
      return { status: res.status, id: body.id as number };
    }, { key });
    expect(tmpl.status).toBe(201);
    templateId = tmpl.id;

    const rendered = await browser.evaluate(async (id: number) => {
      const res = await fetch('/api/vault/templates/' + id + '/render');
      const body = await res.json().catch(() => ({}));
      return { status: res.status, rendered: body.rendered as string };
    }, templateId);
    expect(rendered.status).toBe(200);
    expect(rendered.rendered).toContain('[REDACTED]');
    expect(rendered.rendered).not.toContain('super-secret-value-');

    // An unknown placeholder is refused with 400.
    const bad = await browser.evaluate(async () => {
      const res = await fetch('/api/vault/templates', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: 'E2E_BAD_' + Date.now(), body: '{{ vault.E2E_MISSING_KEY }}' }),
      });
      const badBody = await res.json().catch(() => ({}));
      const badId = typeof badBody.id === 'number' ? badBody.id : 0;
      const r = await fetch('/api/vault/templates/' + badId + '/render');
      return { status: r.status, id: badId };
    });
    expect(bad.status).toBe(400);
    if (bad.id) {
      await browser.evaluate(async (id: number) => { await fetch('/api/vault/templates/' + id, { method: 'DELETE' }); }, bad.id);
    }

    // 4. A host target binding appears with an 'unknown' drift badge.
    const target = await browser.evaluate(async ({ templateId }) => {
      const res = await fetch('/api/vault/targets', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: 'E2E_TARGET_' + Date.now(), kind: 'host', template_id: templateId,
          target_ref: '999999', path: '/etc/pantau/secret.env', namespace: '', post_sync_hook: '',
        }),
      });
      const body = await res.json().catch(() => ({}));
      return { status: res.status, id: body.id as number, drift: body.drift_status as string };
    }, { templateId });
    expect(target.status).toBe(201);
    targetId = target.id;
    expect(target.drift).toBe('unknown');

    await browser.evaluate(() => {
      (window as unknown as { Vault: { refresh(): Promise<void> } }).Vault.refresh();
      return null;
    });
    await expect(browser.locator('#vaultTargetsList [data-drift="unknown"]')).toContainText('unknown');
  } finally {
    // Cleanup by id so a test never touches another test's resources.
    await browser.evaluate(async ({ secretId, templateId, targetId }) => {
      if (targetId) await fetch('/api/vault/targets/' + targetId, { method: 'DELETE' });
      if (templateId) await fetch('/api/vault/templates/' + templateId, { method: 'DELETE' });
      if (secretId) await fetch('/api/vault/secrets/' + secretId, { method: 'DELETE' });
    }, { secretId, templateId, targetId });
  }
});
