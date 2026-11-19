import { afterEach, test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { deleteHosts, initApp } from './helpers.js';

// The fleet Docker strip is answered in the browser: the Go aggregation itself is covered by the
// store/handler unit tests. What is under test here is the summary strip and the untouched picker.
afterEach(async ({ browser }) => { await deleteHosts(browser, ['dk-fleet-host']); });

const OVERVIEW = {
  host_count: 4,
  hosts_with_docker: 2,
  hosts_without_docker: 2,
  total_containers: 7,
  running_containers: 5,
  stopped_containers: 2,
  total_stacks: 3,
  total_security_issues: 2,
  hosts: [
    { host_id: 1, host_name: 'web-1', group_name: 'prod', container_count: 5, running: 4, stopped: 1, stack_count: 2, security_issues: 2, audit_status: 'ok', seen_at: new Date().toISOString() },
    { host_id: 2, host_name: 'web-2', group_name: 'prod', container_count: 2, running: 1, stopped: 1, stack_count: 1, security_issues: 0, audit_status: 'ok', seen_at: new Date().toISOString() }
  ],
  generated_at: new Date().toISOString()
};

test('docker fleet: summary strip renders aggregated data and the host picker still works', async ({ app, screen, browser }) => {
  await browser.route('**/api/docker/overview', async (route) => {
    await route.fulfill({ json: OVERVIEW });
  });

  await app.open('/');
  await initApp(screen, browser);

  // A host is needed so the picker grid renders (fresh DB starts empty).
  await browser.evaluate(async () => {
    await fetch('/api/hosts', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: 'dk-fleet-host', host: '10.9.9.6', port: 22, user: 'root' }) });
    await (window as unknown as { loadHosts(): Promise<void> }).loadHosts();
    return null;
  });

  // Open the global Docker section from the side panel.
  await browser.locator('#navItemDocker').click();
  await expect(browser.locator('#sectionDocker')).toBeVisible();

  // The fleet summary strip shows the aggregated numbers from the endpoint.
  const strip = browser.locator('#dockerFleetStrip');
  await expect(strip).toBeVisible();
  await expect(strip).toContainText('2 / 4');   // hosts with Docker / total hosts
  await expect(strip).toContainText('7');       // total containers
  await expect(strip).toContainText('5');       // running
  await expect(strip).toContainText('3');       // stacks
  await expect(browser.locator('#dockerFleetStrip .dk-fleet-stat')).toHaveCount(6);

  // The existing host picker is still present and searchable.
  await expect(browser.locator('#dockerHostSearchInput')).toBeVisible();
  await expect(browser.locator('#dockerHostsGrid')).toBeVisible();
});
