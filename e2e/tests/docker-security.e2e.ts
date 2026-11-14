import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';

// The Docker security data is answered in the browser: the host has no Docker daemon, the panel UI is
// what is under test (the Go side of the audit and of the scan is covered by the Go tests).
type Finding = { container: string; image: string; rule_id: string; severity: string; title: string; detail: string; remediation: string };
type Vuln = { id: string; package: string; installed: string; fixed: string; severity: string; title: string };
type Scan = { subject: string; scanner: string; status: string; note?: string; error?: string; scanned_at: string; vuln_counts: Record<string, number>; vulns?: Vuln[]; truncated?: boolean };

const HOUR = 3600 * 1000;
const now = Date.now();
const ago = (h: number) => new Date(now - h * HOUR).toISOString();

const RULES: Array<[string, string, string]> = [
  ['privileged', 'critical', 'Privileged container'],
  ['docker_socket_mounted', 'critical', 'Docker socket mounted'],
  ['sensitive_host_mount', 'high', 'Sensitive host path mounted read-write'],
  ['dangerous_capabilities', 'high', 'Dangerous capabilities added'],
  ['sensitive_public_port', 'high', 'Sensitive port published on all interfaces'],
  ['host_namespace', 'medium', 'Host namespace shared'],
  ['runs_as_root', 'medium', 'Runs as root'],
  ['writable_rootfs', 'low', 'Writable root filesystem']
];

// 40 findings over 8 containers.
const FINDINGS: Finding[] = [];
for (let i = 0; i < 40; i++) {
  const [rule_id, severity, title] = RULES[i % RULES.length]!;
  FINDINGS.push({
    container: 'app-' + String.fromCharCode(97 + (i % 8)),
    image: 'registry.local/app-' + (i % 8) + ':1.0',
    rule_id, severity, title,
    detail: 'Detail of ' + rule_id + ' in container ' + i + '.',
    remediation: 'Fix ' + rule_id + ' on container ' + i + '.'
  });
}

const severityCounts = (list: Array<{ severity: string }>) => {
  const c: Record<string, number> = { critical: 0, high: 0, medium: 0, low: 0, total: list.length };
  list.forEach((v) => { if (c[v.severity] !== undefined) c[v.severity]++; });
  return c;
};

// 300 vulnerabilities of the first image: the detail must render them in chunks.
const BIG_VULNS: Vuln[] = [];
for (let i = 0; i < 300; i++) {
  const sev = i < 20 ? 'critical' : (i < 90 ? 'high' : (i < 200 ? 'medium' : 'low'));
  BIG_VULNS.push({
    id: 'CVE-2024-' + String(10000 + i),
    package: 'lib' + (i % 12),
    installed: '1.0.' + i,
    fixed: i % 3 === 0 ? '' : '1.0.' + (i + 1),
    severity: sev,
    title: 'Vulnerability in lib' + (i % 12)
  });
}

// 12 images, the first one with 300 vulnerabilities.
const IMAGES: Scan[] = [];
for (let i = 0; i < 12; i++) {
  const vulns = i === 0 ? BIG_VULNS : BIG_VULNS.slice(0, 3 + i * 2).map((v) => ({ ...v, id: v.id.replace('2024', '202' + (3 + i)) }));
  IMAGES.push({
    subject: 'registry.local/app-' + i + ':1.0',
    scanner: i === 11 ? 'grype' : 'trivy',
    status: i === 10 ? 'error' : (i === 9 ? 'unsupported' : 'ok'),
    error: i === 10 ? 'scanner exited with code 2' : '',
    note: i === 9 ? 'No vulnerability scanner is installed on this host.' : '',
    scanned_at: ago(i * 24 + 1),
    vuln_counts: severityCounts(vulns),
    vulns
  });
}

const AUDIT = {
  id: 1, host_id: 1, kind: 'misconfig', subject: '', scanned_at: ago(30), scanner: '',
  status: 'ok', note: '', counts: severityCounts(FINDINGS), findings: FINDINGS, vulns: [], truncated: false, error: ''
};

const INTEL = {
  ok: true,
  intel: {
    enabled: true,
    cves: {
      'CVE-2024-10000': { kev: { name: 'Example KEV entry', date_added: '2024-01-02', due_date: '2024-02-02', ransomware: false }, epss: 0.42, percentile: 0.9 },
      'CVE-2024-10001': { epss: 0.02, percentile: 0.1 }
    }
  }
};
test('docker security: the summary strip, the grouped findings, the image table and the chunked detail', async ({ app, screen, browser }) => {
  let failSecurity = false;
  await browser.addInitScript(() => {
    const w = window as unknown as { __dsErrors: string[] };
    w.__dsErrors = [];
    const original = console.error.bind(console);
    console.error = (...args: unknown[]) => {
      w.__dsErrors.push(args.map((a) => String(a)).join(' '));
      original(...args);
    };
  });

  await browser.route('**/api/hosts/*/docker/security/intel', async (route) => {
    await route.fulfill({ json: INTEL });
  });
  await browser.route('**/api/hosts/*/docker/security/scan-image', async (route) => {
    const body = JSON.parse(route.request.postData ?? '{}') as { image?: string };
    await route.fulfill({ json: { ok: true, scan: {
      subject: body.image ?? 'registry.local/app-0:1.0', scanner: 'trivy', status: 'ok',
      scanned_at: new Date().toISOString(),
      vuln_counts: { critical: 3, high: 4, medium: 0, low: 0, unknown: 0, total: 7 }, vulns: []
    }, intel: null } });
  });
  await browser.route('**/api/hosts/*/docker/security', async (route) => {
    if (failSecurity) {
      await route.fulfill({ status: 500, json: { ok: false, error: 'database is locked' } });
      return;
    }
    await route.fulfill({ json: { ok: true, assessed: true, audit: AUDIT, images: IMAGES } });
  });

  await app.open('/');
  await initApp(screen, browser);

  const hostID = (await browser.evaluate(async () => {
    const res = await fetch('/api/hosts', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: 'dockersec', host: '10.9.9.9', port: 22, user: 'root' }) });
    return (await res.json()).id as number;
  })) as number;

  // The host detail opens on the Docker tab; the Security sub-tab of it shows the panel.
  await browser.evaluate(async (id) => {
    const w = window as unknown as { openHostDetail(id: number): Promise<void>; switchTab(tab: string): void; setDockerSubTab(view: string): void };
    await w.openHostDetail(id);
    w.switchTab('docker');
    w.setDockerSubTab('security');
    return null;
  }, hostID);

  // The summary strip is there, with the totals of the audit and of the images.
  await expect(browser.locator('#dsSummary')).toBeVisible();
  await expect(browser.locator('#dsSummary')).toContainText('Critical');
  await expect(browser.locator('#dsSummary')).toContainText('40');
  await expect(browser.locator('#dsSummary')).toContainText('12');

  // The container audit groups the findings per container, worst severity first.
  await expect(browser.locator('#dsAuditTable .ds-group')).toHaveCount(8);
  await expect(browser.locator('#dsAuditTable .ds-group .ds-group-name').first()).toHaveText('app-a');
  // The first groups are open, the rest are collapsed.
  await expect(browser.locator('#dsAuditTable [data-group-box="app-a"] .ds-group-body')).toBeVisible();
  await expect(browser.locator('#dsAuditTable [data-group-box="app-f"] .ds-group-body')).toBeHidden();

  // A group can be opened: the remediation is visible on expand, not in a wide table.
  await browser.locator('#dsAuditTable [data-group-box="app-f"] .ds-group-head').click();
  await expect(browser.locator('#dsAuditTable [data-group-box="app-f"] .ds-group-body')).toBeVisible();
  await expect(browser.locator('#dsAuditTable [data-group-box="app-f"] .ds-finding-fix').first()).toContainText('Fix');

  // The severity filter narrows the findings to the critical ones (two containers).
  await browser.locator('#dsAuditToolbar .ds-sev-chip[data-sev="critical"]').click();
  await expect(browser.locator('#dsAuditTable .ds-group')).toHaveCount(2);
  await browser.locator('#dsAuditToolbar .ds-sev-chip[data-sev="critical"]').click();
  await expect(browser.locator('#dsAuditTable .ds-group')).toHaveCount(8);

  // The image section lists the scanned images, worst severity first.
  await browser.locator('#dsSections .ds-seg-btn[data-section="images"]').click();
  const rows = browser.locator('#dsImageTable .ds-img-row');
  await expect(rows).toHaveCount(12);
  await expect(rows.first()).toContainText('registry.local/app-0:1.0');

  // Sorting by name puts the images in alphabetical order.
  await browser.locator('#dsImageTable .ds-sort-btn').filter({ hasText: 'Name' }).click();
  await expect(browser.locator('#dsImageTable .ds-img-row').first()).toContainText('registry.local/app-0:1.0');
  await browser.locator('#dsImageTable .ds-sort-btn').filter({ hasText: 'Worst severity' }).click();

  // The status filter keeps only the images that could not be scanned.
  await browser.locator('#dsImageTable .ds-sev-chip').filter({ hasText: 'Failed' }).click();
  await expect(browser.locator('#dsImageTable .ds-img-row')).toHaveCount(2);
  await browser.locator('#dsImageTable .ds-sev-chip').filter({ hasText: 'All' }).click();
  await expect(browser.locator('#dsImageTable .ds-img-row')).toHaveCount(12);

  // The detail of an image with 300 vulnerabilities renders in chunks.
  await browser.locator('#dsImageTable .ds-img-row').first().click();
  await expect(browser.locator('#dockerSecModal')).toBeVisible();
  await expect(browser.locator('#dockerSecBody .ds-table tbody tr')).toHaveCount(50);
  await expect(browser.locator('#dockerSecBody')).toContainText('Show more');
  await browser.locator('#dockerSecBody button').filter({ hasText: 'Show more' }).click();
  await expect(browser.locator('#dockerSecBody .ds-table tbody tr')).toHaveCount(100);
  await browser.locator('#dockerSecBody button').filter({ hasText: 'Show more' }).click();
  await expect(browser.locator('#dockerSecBody .ds-table tbody tr')).toHaveCount(150);

  // The exploit intelligence fills the KEV/EPSS badges in.
  await expect(browser.locator('#dockerSecBody')).toContainText('KEV');
  await expect(browser.locator('#dockerSecBody')).toContainText('EPSS');
  await browser.locator('#dockerSecModal .modal-header button').click();

  // A scan that finishes is visible in the panel right away (the POST answer, not a later reload).
  await browser.locator('#dsImageTable .ds-img-row button').first().click();
  await expect(browser.locator('#dsImageTable .ds-img-row').first()).toContainText('Critical');
  await expect(browser.locator('#dsSummary')).toContainText('Critical');

  // A scan started from the Images list shows up in the Security panel too.
  await browser.evaluate(() => { (window as unknown as { setDockerSubTab(view: string): void }).setDockerSubTab('images'); return null; });
  await expect(browser.locator('#dsImageTable')).toBeHidden();
  await browser.evaluate(() => {
    const w = window as unknown as { DS: { scanImage(ref: string, btn: unknown): Promise<void> } };
    return w.DS.scanImage('registry.local/app-3:1.0', null);
  });
  await browser.evaluate(() => { (window as unknown as { setDockerSubTab(view: string): void }).setDockerSubTab('security'); return null; });
  await expect(browser.locator('#dsImageTable .ds-img-row[data-image="registry.local/app-3:1.0"]')).toContainText('Critical');
  // A failed load is shown as an error with a retry button, not as an empty result.
  failSecurity = true;
  await browser.evaluate(() => { (window as unknown as { loadDockerTab(): void }).loadDockerTab(); return null; });
  await expect(browser.locator('#dsPanel .ds-error')).toBeVisible();
  await expect(browser.locator('#dsPanel .ds-error')).toContainText('database is locked');
  await expect(browser.locator('#dsPanel button').filter({ hasText: 'Retry' })).toBeVisible();
  // The data below the error is still there: a failure never looks like "no data".
  await expect(browser.locator('#dsSummary')).toBeVisible();

  failSecurity = false;
  await browser.locator('#dsPanel button').filter({ hasText: 'Retry' }).click();
  await expect(browser.locator('#dsPanel .ds-error')).toBeHidden();
  await expect(browser.locator('#dsImageTable .ds-img-row')).toHaveCount(12);

  const errors = (await browser.evaluate(() => (window as unknown as { __dsErrors: string[] }).__dsErrors)) as string[];
  if (errors.length) throw new Error('console errors: ' + errors.join(' | '));
});
