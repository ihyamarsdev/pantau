import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';
import {
  openWorkspaceDrawer,
  seedWorkspace,
  stubTerraformStatus,
  switchDrawerTab,
  switchTfTab,
} from './terraform-helpers.js';

interface MockRun {
  id: number;
  workspace_id: number;
  workspace_name: string;
  workspace_environment: string;
  type: string;
  speculative: boolean;
  status: string;
  trigger_kind: string;
  message: string;
  has_changes: boolean;
  counts: { create: number; update: number; delete: number; replace: number };
  policy_status: string;
  cost_delta: number | null;
  created_at: string;
  started_at: string;
  finished_at: string | null;
  plan: {
    create: number;
    update: number;
    delete: number;
    replace: number;
    changes: {
      address: string;
      action: string;
      provider: string;
      reason?: string;
      diffs: { path: string; before: unknown; after: unknown; sensitive?: boolean; unknown?: boolean }[];
    }[];
  };
  policy: {
    status: string;
    results: { policy_id: number; name: string; kind: string; enforcement: string; passed: boolean; violations: unknown[] }[];
  };
  cost: { available: boolean; error: string };
}

function makeMockRun(wsId: number, wsName: string, status = 'awaiting_approval'): MockRun {
  return {
    id: 42,
    workspace_id: wsId,
    workspace_name: wsName,
    workspace_environment: 'staging',
    type: 'plan',
    speculative: false,
    status,
    trigger_kind: 'manual',
    message: 'e2e plan run',
    has_changes: true,
    counts: { create: 1, update: 0, delete: 0, replace: 0 },
    policy_status: 'passed',
    cost_delta: null,
    created_at: '2026-11-25T07:00:00Z',
    started_at: '2026-11-25T07:00:01Z',
    finished_at: '2026-11-25T07:00:05Z',
    plan: {
      create: 1,
      update: 0,
      delete: 0,
      replace: 0,
      changes: [
        {
          address: 'terraform_data.demo',
          action: 'create',
          provider: 'terraform.io/builtin/terraform',
          diffs: [{ path: 'input', before: null, after: 'v1' }],
        },
      ],
    },
    policy: {
      status: 'passed',
      results: [{ policy_id: 1, name: 'no-open-cidr', kind: 'deny_open_cidr', enforcement: 'hard', passed: true, violations: [] }],
    },
    cost: { available: false, error: 'infracost not installed' },
  };
}

async function stubRunsEndpoints(browser: Parameters<typeof test>[1] extends (f: infer F) => unknown ? F['browser'] : never, wsId: number, wsName: string) {
  let run = makeMockRun(wsId, wsName, 'awaiting_approval');
  await browser.route('**/api/terraform/runs**', async (route) => {
    const req = route.request;
    const url = new URL(req.url);
    const p = url.pathname;
    if (p === '/api/terraform/runs') {
      const stFilter = url.searchParams.get('status') || '';
      const list = !stFilter || stFilter.split(',').includes(run.status) ? [run] : [];
      await route.fulfill({ json: { runs: list } });
      return;
    }
    if (p === `/api/terraform/runs/${run.id}/log`) {
      await route.fulfill({ json: { log: '\u001b[32mTerraform will perform the following actions:\u001b[0m\n+ terraform_data.demo\n', next_offset: 80, done: true, status: run.status } });
      return;
    }
    if (p === `/api/terraform/runs/${run.id}/apply` && req.method === 'POST') {
      run = { ...run, status: 'applied' };
      await route.fulfill({ json: { run } });
      return;
    }
    if (p === `/api/terraform/runs/${run.id}`) {
      await route.fulfill({ json: { run } });
      return;
    }
    await route.continue();
  });

  await browser.route(`**/api/terraform/workspaces/${wsId}/runs**`, async (route) => {
    const req = route.request;
    if (req.method === 'POST') {
      run = makeMockRun(wsId, wsName, 'awaiting_approval');
      await route.fulfill({ status: 201, json: { run } });
      return;
    }
    await route.fulfill({ json: { runs: [run] } });
  });
}

test('global Runs tab lists runs across workspaces, highlights approval queue, and opens the Run Viewer', async (fx) => {
  const { browser } = fx;
  await fx.app.open('/');
  await initApp(fx.screen, browser);
  const wsId = await seedWorkspace(browser, 'e2e-global-run');
  await stubTerraformStatus(browser, true);
  await stubRunsEndpoints(browser, wsId, 'e2e-global-run');

  await fx.screen.getByRole('tab', 'Terraform').click();
  await switchTfTab(browser, 'runs');

  // Approval queue banner and global runs table both show the pending run.
  await expect(browser.locator('#tfGlobalApprovalBanner')).toBeVisible();
  const row = browser.locator('#tfGlobalRunsTable tbody tr[data-run="42"]');
  await expect(row).toBeVisible();
  await expect(row).toContainText('e2e-global-run');

  // Clicking the row opens the Run Viewer in the drawer.
  await row.click();
  await expect(browser.locator('#tfDrawerRoot')).toBeVisible();
  await expect(browser.locator('#tfRunStepper')).toBeVisible();
  await expect(browser.locator('#tfRunApprovalGate')).toBeVisible();
});

test('Run Viewer renders pipeline stepper, sticky approval gate, filterable Plan Diff, Policy/Cost, and ANSI Console Log', async (fx) => {
  const { browser } = fx;
  const wsId = await openWorkspaceDrawer(fx, 'e2e-run-viewer');
  await stubRunsEndpoints(browser, wsId, 'e2e-run-viewer');

  // Launch plan via the workspace Runs tab Start Run dialog.
  await switchDrawerTab(browser, 'runs');
  await browser.locator('#tfRunStartBtn').click();
  await expect(browser.locator('#tfRunType')).toBeVisible();
  await browser.locator('#tfRunMsg').fill('viewer test');
  await browser.locator('#tfRunSubmit').click();

  // Wait for Pipeline Stepper and Sticky Approval Gate in the Run Viewer.
  await expect(browser.locator('#tfRunStepper')).toBeVisible();
  await expect(browser.locator('#tfRunApprovalGate')).toBeVisible();

  // Plan Diff tab: filter chips + search.
  await browser.locator('[data-run-tab="diff"]').click();
  await expect(browser.locator('[data-diff-act="filter"][data-action="create"]')).toBeVisible();
  await expect(browser.locator('#tfDiffList')).toContainText('terraform_data.demo');
  await browser.locator('#tfDiffSearch').fill('demo');
  await expect(browser.locator('#tfDiffList .tf-diff-card')).toHaveCount(1);

  // Policy & Cost tab.
  await browser.locator('[data-run-tab="policy"]').click();
  await expect(browser.locator('#tfRunPolicyPanel')).toBeVisible();
  await expect(browser.locator('#tfRunPolicyPanel')).toContainText('no-open-cidr');

  // Console Log tab: search, follow, wrap controls exist and ANSI text is stripped/rendered.
  await browser.locator('[data-run-tab="log"]').click();
  await expect(browser.locator('#tfRunLog')).toContainText('Terraform will perform the following actions');
  await expect(browser.locator('#tfLogSearch')).toBeVisible();
  await expect(browser.locator('#tfLogFollow')).toBeVisible();
  await expect(browser.locator('#tfLogWrap')).toBeVisible();

  // Sticky Approval Gate validates comment (<3 chars rejected), then Confirm & Apply transitions to applied.
  await browser.locator('#tfApprComment').fill('ok');
  await browser.locator('#tfApprApply').click();
  await expect(browser.locator('#tfApprErr')).toBeVisible();

  await browser.locator('#tfApprComment').fill('approved in e2e');
  await browser.locator('#tfApprApply').click();
  await expect(browser.locator('#tfRunApprovalGate')).toBeHidden();
  await expect(browser.locator('#tfDrawer')).toContainText(/applied/i);
});
