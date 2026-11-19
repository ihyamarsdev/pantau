import { afterEach, test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { deleteHosts, initApp, uniqueHostName } from './helpers.js';

// The whole run shares one database, so the host this test adds is removed again. afterEach runs
// after a failure too, so the host never survives a failing test either.
const HOST = uniqueHostName('seccenter');
afterEach(async ({ browser }) => {
  await deleteHosts(browser, [HOST]);
});

// The aggregated overview is stubbed in the browser: the Go aggregation itself is covered by the
// store/handler unit tests. What is under test here is the Security Center component in the host
// Security tab (the global view was removed per ADR 0089 addendum).
type Action = {
  id: string; pillar: string; severity: string; title: string; detail: string;
  host_id: number; host_name: string; priority: number; evidence: unknown; remediation: string;
};
type Overview = {
  score: number; grade: string;
  breakdown: Record<string, number>;
  pillars: Record<string, number>;
  top_actions: Action[];
  host_count: number;
  generated_at: string;
};

const OVERVIEW: Overview = {
  score: 62,
  grade: 'D',
  breakdown: {
    kev_critical: 2, fim_drift: 1, leaked_secrets: 3,
    pending_sec_updates: 5, container_issues: 2, firewall_warnings: 1
  },
  pillars: {
    overview: 14, vulnerabilities: 7, network: 1,
    containers: 2, identity: 1, code: 3, exposure: 4
  },
  top_actions: [
    { id: 'kev:nginx', pillar: 'vulnerabilities', severity: 'critical', title: 'nginx RCE exploited in the wild', detail: 'CVE-2026-1234', host_id: 1, host_name: 'web-1', priority: 100, evidence: { cve: 'CVE-2026-1234', kev: true, epss: 0.73 }, remediation: 'upgrade' },
    { id: 'secret:aws', pillar: 'code', severity: 'critical', title: 'AWS access key committed', detail: 'main.tf:12', host_id: 0, host_name: '', priority: 90, evidence: { rule_id: 'aws_key', file: 'main.tf', line: 12 }, remediation: 'rotate key' },
    { id: 'fim:/etc/passwd', pillar: 'identity', severity: 'high', title: 'FIM baseline drift', detail: '/etc/passwd', host_id: 1, host_name: 'web-1', priority: 80, evidence: { target: '/etc/passwd' }, remediation: 'accept_fim' },
    { id: 'firewall:1', pillar: 'network', severity: 'high', title: 'Firewall inactive', detail: 'no firewall detected', host_id: 1, host_name: 'web-1', priority: 70, evidence: { check: 'firewall_active' }, remediation: 'firewall' }
  ],
  host_count: 3,
  generated_at: new Date().toISOString()
};

test('security center (host tab): posture strip, pillars, collapsible sections and evidence drawer', async ({ app, screen, browser }) => {
  let overviewCalls = 0;
  await browser.route('**/api/security/overview*', async (route) => {
    overviewCalls++;
    await route.fulfill({ json: OVERVIEW });
  });
  await browser.route('**/api/iac-scan', async (route) => {
    await route.fulfill({ json: { summary: { total: 4, critical: 1, high: 2, medium: 1, low: 0 }, status: 'assessed' } });
  });
  await browser.route('**/api/hosts/*/security', async (route) => {
    await route.fulfill({ json: { scanned: true, scan: {
      host_id: 1, scanned_at: new Date().toISOString(), status: 'ok', package_manager: 'apt', os_family: 'debian',
      pending_security: 2, pending_total: 5, severity: { critical: 1, important: 1, moderate: 0, low: 0, unknown: 0 },
      reboot_required: false, repo_cache_age_hours: 1, findings: [
        { package: 'openssl', installed_version: '3.0.1', fixed_version: '3.0.2', severity: 'critical', advisory: 'DSA-1', cves: ['CVE-2026-0001'] },
        { package: 'curl', installed_version: '7.1', fixed_version: '7.2', severity: 'important', advisory: 'DSA-2', cves: ['CVE-2026-0002'] },
        { package: 'legacy-pkg', installed_version: '1.0', fixed_version: '', severity: 'low', advisory: '', cves: [] }
      ], hardening: [], notes: []
    }, intel: { enabled: false } } });
  });
  await browser.route('**/api/hosts/*/packages/updates', async (route) => {
    await route.fulfill({ json: { checked: true, check: {
      host_id: 1, checked_at: new Date().toISOString(), supported: true, manager: 'apt',
      pending_total: 2, pending_security: 2, reboot_required: false, repo_cache_age_hours: 1,
      packages: [{ name: 'openssl', from: '3.0.1', to: '3.0.2', security: true }], note: '', error: ''
    } } });
  });

  await app.open('/');
  await initApp(screen, browser);

  // The side panel no longer has a Security item; the Topology tooltip still resolves at boot.
  await expect(browser.locator('#navItemSecurity')).toHaveCount(0);
  await expect(browser.locator('#btnHeaderTopology')).toHaveAttribute('title', 'Network Radar');

  const hostID = (await browser.evaluate(async (name) => {
    const res = await fetch('/api/hosts', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name, host: '10.9.9.7', port: 22, user: 'root' }) });
    return (await res.json()).id as number;
  }, HOST)) as number;

  await browser.evaluate(async (id) => {
    const w = window as unknown as { openHostDetail(id: number): Promise<void>; switchTab(tab: string): void };
    await w.openHostDetail(id);
    w.switchTab('security');
    return null;
  }, hostID);

  // Zone 1: posture strip with score/grade and four KPI filter pills; no scope selector in host mode.
  await expect(browser.locator('#tabContent .sc-strip')).toBeVisible();
  await expect(browser.locator('#tabContent .sc-ring-score')).toHaveText('62');
  await expect(browser.locator('#tabContent .sc-grade')).toHaveText('D');
  await expect(browser.locator('#tabContent .sc-kpi')).toHaveCount(4);
  await expect(browser.locator('#tabContent #scScope')).toHaveCount(0);

  // Zone 2: seven pillars.
  await expect(browser.locator('#tabContent .sc-nav-item[data-sc-action="pillar"]')).toHaveCount(7);
  await expect(browser.locator('#tabContent .sc-nav-item[data-pillar="exposure"]')).toBeVisible();

  // Zone 3 (Overview): prioritized Top Action Queue, and every section is collapsible.
  const actions = browser.locator('#tabContent #scPane .sc-finding');
  await expect(actions).toHaveCount(4);
  await expect(actions.first()).toContainText('nginx RCE');
  const queueSel = '#tabContent #scPane .sc-section[data-section-id="ov-queue"]';
  await browser.locator(queueSel + ' .sc-section-head').click();
  expect(await browser.evaluate((s) => !!document.querySelector(s)?.classList.contains('collapsed'), queueSel)).toBe(true);
  await browser.locator(queueSel + ' .sc-section-head').click();
  expect(await browser.evaluate((s) => !!document.querySelector(s)?.classList.contains('collapsed'), queueSel)).toBe(false);

  // KPI pill filters the queue to one pillar.
  await browser.locator('#tabContent .sc-kpi[data-kpi="kev"]').click();
  await expect(browser.locator('#tabContent #scPane .sc-finding')).toHaveCount(1);
  await browser.locator('#tabContent .sc-kpi[data-kpi="kev"]').click();
  await expect(browser.locator('#tabContent #scPane .sc-finding')).toHaveCount(4);

  // Evidence drawer on the first finding, with keyboard navigation.
  await actions.first().click();
  await expect(browser.locator('#tabContent #scDrawer')).toBeVisible();
  await expect(browser.locator('#tabContent #scDrawer')).toContainText('CVE-2026-1234');
  await browser.evaluate(() => { document.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true })); return null; });
  await expect(browser.locator('#tabContent #scDrawer')).toContainText('AWS access key');
  await browser.evaluate(() => { document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); return null; });
  await expect(browser.locator('#tabContent #scDrawer')).toBeHidden();

  // The host Security header and its summary cards are merged into one collapsible panel.
  await expect(browser.locator('#tabContent .sc-section[data-section-id="legacy-security"]')).toHaveCount(1);

  // Vulnerabilities & Updates: real, filterable findings table.
  await browser.locator('#tabContent .sc-nav-item[data-pillar="vulnerabilities"]').click();
  const vrows = browser.locator('#tabContent .sc-vuln-row[data-sc-action="vuln-finding"]');
  await expect(vrows).toHaveCount(2); // Fix Available is on by default: legacy-pkg (no fix) is hidden
  await browser.locator('#tabContent #scFixAvailable').uncheck();
  await expect(vrows).toHaveCount(3);
  await browser.locator('#tabContent #scFixAvailable').check();
  await expect(vrows).toHaveCount(2);
  await browser.locator('#tabContent #scVulnSearch').fill('curl');
  await expect(vrows).toHaveCount(1);
  await browser.locator('#tabContent #scVulnSearch').fill('');

  await vrows.first().click();
  await expect(browser.locator('#tabContent #scDrawer')).toBeVisible();
  await expect(browser.locator('#tabContent #scDrawer')).toContainText('CVE-2026-0001');
  await browser.evaluate(() => { document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); return null; });

  await expect(browser.locator('#tabContent .sc-section[data-section-id="vuln-pkgupdates"]')).toContainText('2 pending');
  expect(overviewCalls).toBeGreaterThan(0);
});
