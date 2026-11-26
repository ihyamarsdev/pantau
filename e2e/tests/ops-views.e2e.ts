import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { deleteHosts, initApp, uniqueHostName } from './helpers.js';

type PageGlobals = {
  loadHosts(): Promise<void>;
  openHostDetail(id: number): Promise<void>;
  switchTab(tab: string): void;
  setDockerSubTab(sub: string): void;
  switchSideNav(nav: string): void;
  pickKubeHost(id: number): void;
  switchKubeTab(tab: string): void;
  GlobeRadar?: { isPaused(): boolean };
};

test('Default Hosts View persists Compact Ops Table and Globe auto-pauses when modal opens', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);

  await expect(browser.locator('#hostGlobe')).toBeVisible();
  await expect(browser.locator('#hostList')).toBeHidden();

  // Opening Settings modal pauses the 3D Globe render loop.
  await browser.locator('button[onclick="openSettingsModal()"]').click();
  await expect(browser.locator('#settingsModal')).toHaveAttribute('class', /\bopen\b/);
  const pausedWhileModalOpen = await browser.evaluate(() => {
    const g = window as unknown as PageGlobals;
    return !!g.GlobeRadar?.isPaused();
  });
  expect(pausedWhileModalOpen).toBe(true);

  // Switch Default Hosts View to Compact Ops Table (list).
  await browser.locator('#settingDefaultHostsView').selectOption({ value: 'list' });
  await browser.locator('#settingsModal button[aria-label="Close"]').click();

  await expect(browser.locator('#hostList')).toBeVisible();
  await expect(browser.locator('#hostGlobe')).toBeHidden();
  await expect(browser.locator('#viewListBtn')).toHaveAttribute('class', /\bactive\b/);

  // Reload and verify Compact Ops Table persists as the default Hosts view.
  await browser.reload();
  await initApp(screen, browser);
  await expect(browser.locator('#hostList')).toBeVisible();
  await expect(browser.locator('#hostGlobe')).toBeHidden();
  await expect(browser.locator('#viewListBtn')).toHaveAttribute('class', /\bactive\b/);
});

test('Docker containers group by Compose project with collapsible headers, quick stack actions, and Ask Agent buttons', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);

  const hostName = uniqueHostName('dk-stack-host');
  let hostId = 0;
  let stackActionBody: Record<string, unknown> | null = null;

  try {
    hostId = await browser.evaluate(async (name: string) => {
      const res = await fetch('/api/hosts', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name, host: '10.9.1.10', port: 22, user: 'root' }),
      });
      const created = await res.json();
      await (window as unknown as PageGlobals).loadHosts();
      return created.id as number;
    }, hostName);

    await browser.route(new RegExp(`/api/hosts/${hostId}/docker$`), async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          ok: true,
          containers: [
            {
              name: 'webapp-web-1',
              image: 'nginx:1.27',
              state: 'running',
              status: 'Up 2 hours',
              ports: '80/tcp',
              compose_project: 'webapp',
              compose_service: 'web',
              labels: { 'com.docker.compose.project': 'webapp', 'com.docker.compose.service': 'web' },
            },
            {
              name: 'webapp-db-1',
              image: 'postgres:16',
              state: 'running',
              status: 'Up 2 hours',
              ports: '5432/tcp',
              compose_project: 'webapp',
              compose_service: 'db',
              labels: { 'com.docker.compose.project': 'webapp', 'com.docker.compose.service': 'db' },
            },
            {
              name: 'solo-redis',
              image: 'redis:7',
              state: 'exited',
              status: 'Exited (0) 5 minutes ago',
              ports: '',
            },
          ],
        }),
      });
    });

    await browser.route(new RegExp(`/api/hosts/${hostId}/docker/stack$`), async (route) => {
      stackActionBody = JSON.parse(route.request.postData ?? '{}') as Record<string, unknown>;
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ ok: true, output: 'Restarted webapp' }),
      });
    });

    await browser.route(new RegExp(`/api/hosts/${hostId}/docker/security$`), async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          ok: true,
          audit: {
            scanned_at: new Date().toISOString(),
            status: 'ok',
            counts: { critical: 0, high: 1, medium: 0, low: 0, total: 1 },
            findings: [
              {
                container: 'webapp-web-1',
                image: 'nginx:1.27',
                rule_id: 'DK001',
                severity: 'high',
                title: 'Running as root',
                detail: 'Container runs as UID 0',
                remediation: 'Set USER in Dockerfile',
              },
            ],
          },
          images: [],
        }),
      });
    });

    await browser.evaluate(async (id: number) => {
      const g = window as unknown as PageGlobals;
      await g.openHostDetail(id);
      g.switchTab('docker');
      return null;
    }, hostId);

    const webappHeader = browser.locator('.dk-group-row[data-dk-group-key="stack:webapp"]');
    const standaloneHeader = browser.locator('.dk-group-row[data-dk-group-key="__standalone__"]');
    await expect(webappHeader).toBeVisible();
    await expect(standaloneHeader).toBeVisible();
    await expect(webappHeader).toContainText('2/2 running');
    await expect(standaloneHeader).toContainText('0/1 running');

    // Collapse webapp group and verify its container rows hide.
    const webappFirstRow = browser.locator('tr[data-dk-group-item="stack:webapp"]').first();
    await expect(webappFirstRow).toBeVisible();
    await webappHeader.click();
    await expect(webappFirstRow).toBeHidden();
    await webappHeader.click();
    await expect(webappFirstRow).toBeVisible();

    // Ask Agent buttons exist on stack header and container rows.
    await expect(browser.locator('.dk-group-row[data-dk-group-key="stack:webapp"] .ag-ask-btn')).toHaveCount(1);
    await expect(browser.locator('tr[data-dk-group-item="stack:webapp"] .ag-ask-btn')).toHaveCount(2);

    // Quick Restart action on webapp stack requires confirmation before calling API.
    await browser.locator('.dk-group-row[data-dk-group-key="stack:webapp"] button[title="Restart"]').click();
    await expect(browser.locator('#confirmModal')).toHaveAttribute('class', /\bopen\b/);
    await browser.locator('#confirmModal button[onclick="resolveConfirm(true)"]').click();
    await expect.poll(() => stackActionBody).toEqual({ project: 'webapp', action: 'restart' });

    // Switch to Docker Security sub-tab and verify Ask Agent button is rendered on container group.
    await browser.evaluate(() => {
      (window as unknown as PageGlobals).setDockerSubTab('security');
      return null;
    });
    await expect(browser.locator('.ds-group-head-row .ag-ask-btn')).toHaveCount(1);
  } finally {
    await deleteHosts(browser, [hostName]);
  }
});

test('Kubernetes Unhealthy Only filter works for Workloads/Pods and Topology Drawer renders relation graph', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);

  const hostName = uniqueHostName('k8s-ops-host');
  let hostId = 0;

  try {
    hostId = await browser.evaluate(async (name: string) => {
      const res = await fetch('/api/hosts', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name, host: '10.9.2.20', port: 22, user: 'root' }),
      });
      const created = await res.json();
      await (window as unknown as PageGlobals).loadHosts();
      return created.id as number;
    }, hostName);

    await browser.route(new RegExp(`/api/hosts/${hostId}/kube/detect$`), async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ ok: true, cluster: { distro: 'k3s', version: 'v1.30.0', role: 'server', cluster_id: 'c-1' } }),
      });
    });

    await browser.route(new RegExp(`/api/hosts/${hostId}/kube/namespaces$`), async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ ok: true, namespaces: [{ name: 'default' }, { name: 'prod' }] }),
      });
    });

    await browser.route(new RegExp(`/api/hosts/${hostId}/kube/nodes$`), async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ ok: true, nodes: [{ name: 'node-1', status: 'Ready', roles: 'control-plane', version: 'v1.30.0', internalIP: '10.9.2.20', cpu: '4', memory: '8Gi', created: '2026-01-01T00:00:00Z' }] }),
      });
    });

    await browser.route(new RegExp(`/api/hosts/${hostId}/kube/workloads`), async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          ok: true,
          workloads: [
            { kind: 'Deployment', namespace: 'prod', name: 'healthy-api', ready: 2, desired: 2, available: 2, images: ['api:v1'], created: '2026-01-01T00:00:00Z' },
            { kind: 'Deployment', namespace: 'prod', name: 'broken-worker', ready: 0, desired: 2, available: 0, images: ['worker:v1'], created: '2026-01-01T00:00:00Z' },
          ],
        }),
      });
    });

    await browser.route(new RegExp(`/api/hosts/${hostId}/kube/pods`), async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          ok: true,
          pods: [
            { namespace: 'prod', name: 'pod-ok', ready: '1/1', status: 'Running', restarts: 0, node: 'node-1', ip: '10.42.0.1', created: '2026-01-01T00:00:00Z' },
            { namespace: 'prod', name: 'pod-crash', ready: '0/1', status: 'CrashLoopBackOff', restarts: 5, node: 'node-1', ip: '10.42.0.2', created: '2026-01-01T00:00:00Z' },
            { namespace: 'prod', name: 'pod-pending', ready: '0/1', status: 'Pending', restarts: 0, node: 'node-1', ip: '', created: '2026-01-01T00:00:00Z' },
            { namespace: 'prod', name: 'pod-oom', ready: '0/1', status: 'OOMKilled', restarts: 2, node: 'node-1', ip: '10.42.0.3', created: '2026-01-01T00:00:00Z' },
            { namespace: 'prod', name: 'pod-img', ready: '0/1', status: 'ImagePullBackOff', restarts: 0, node: 'node-1', ip: '10.42.0.4', created: '2026-01-01T00:00:00Z' },
            { namespace: 'prod', name: 'pod-failed', ready: '0/1', status: 'Failed', restarts: 0, node: 'node-1', ip: '10.42.0.5', created: '2026-01-01T00:00:00Z' },
          ],
        }),
      });
    });

    await browser.route(new RegExp(`/api/hosts/${hostId}/kube/topology`), async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          ok: true,
          topology: {
            root: { id: 'Deployment:prod/broken-worker', kind: 'Deployment', namespace: 'prod', name: 'broken-worker' },
            columns: [
              { stage: 'ingress', items: [{ id: 'Ingress:prod/app-ing', kind: 'Ingress', namespace: 'prod', name: 'app-ing', status: 'Active', meta: 'app.example.com' }] },
              { stage: 'service', items: [{ id: 'Service:prod/app-svc', kind: 'Service', namespace: 'prod', name: 'app-svc', status: 'Active', meta: 'ClusterIP 10.43.0.10' }] },
              { stage: 'workload', items: [{ id: 'Deployment:prod/broken-worker', kind: 'Deployment', namespace: 'prod', name: 'broken-worker', status: 'Degraded', meta: '0/2 ready' }] },
              { stage: 'pod', items: [{ id: 'Pod:prod/pod-crash', kind: 'Pod', namespace: 'prod', name: 'pod-crash', status: 'CrashLoopBackOff', meta: '10.42.0.2' }] },
              { stage: 'pvc', items: [{ id: 'PVC:prod/worker-data', kind: 'PVC', namespace: 'prod', name: 'worker-data', status: 'Bound', meta: '10Gi' }] },
            ],
            edges: [
              { from: 'Ingress:prod/app-ing', to: 'Service:prod/app-svc' },
              { from: 'Service:prod/app-svc', to: 'Deployment:prod/broken-worker' },
              { from: 'Deployment:prod/broken-worker', to: 'Pod:prod/pod-crash' },
              { from: 'Pod:prod/pod-crash', to: 'PVC:prod/worker-data' },
            ],
          },
        }),
      });
    });

    await browser.evaluate((id: number) => {
      const g = window as unknown as PageGlobals;
      g.switchSideNav('kube');
      g.pickKubeHost(id);
      g.switchKubeTab('workloads');
      return null;
    }, hostId);

    // Workloads tab: filter shows both initially, then only broken-worker when Unhealthy Only is checked.
    await expect(browser.locator('#kubeTabContent')).toContainText('healthy-api');
    await expect(browser.locator('#kubeTabContent')).toContainText('broken-worker');
    await expect(browser.locator('#kubePodsFilterWrap')).toBeVisible();

    await browser.locator('#kubePodsOnlyProblemsFilter').check();
    await expect(browser.locator('#kubeTabContent')).not.toContainText('healthy-api');
    await expect(browser.locator('#kubeTabContent')).toContainText('broken-worker');

    // Open Topology Drawer from broken-worker row.
    await browser.locator('#kubeTabContent button[title="Topology"]').first().click();
    await expect(browser.locator('#kubeTopoDrawer')).toHaveAttribute('class', /\bopen\b/);
    await expect(browser.locator('#kubeTopoGrid .kube-topo-card')).toHaveCount(5);
    await expect(browser.locator('#kubeTopoSvg path.kube-topo-edge')).toHaveCount(4);
    await browser.locator('#kubeTopoDrawer button[aria-label="Close"]').click();
    await expect(browser.locator('#kubeTopoDrawer')).toBeHidden();

    // Switch to Pods tab: Unhealthy Only filter keeps CrashLoopBackOff, Pending, OOMKilled, ImagePullBackOff, Failed and hides Running.
    await browser.evaluate(() => {
      (window as unknown as PageGlobals).switchKubeTab('pods');
      return null;
    });
    await expect(browser.locator('#kubeTabContent')).not.toContainText('pod-ok');
    await expect(browser.locator('#kubeTabContent')).toContainText('pod-crash');
    await expect(browser.locator('#kubeTabContent')).toContainText('pod-pending');
    await expect(browser.locator('#kubeTabContent')).toContainText('pod-oom');
    await expect(browser.locator('#kubeTabContent')).toContainText('pod-img');
    await expect(browser.locator('#kubeTabContent')).toContainText('pod-failed');
  } finally {
    await deleteHosts(browser, [hostName]);
  }
});
