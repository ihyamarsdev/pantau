import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';

// The fixtures a test body receives, derived from `test`.
type Fixtures = Parameters<Parameters<typeof test>[1]>[0];

// Repositories is its own section (no Terraform gate): log in, open it from the side navigation.
async function openRepos({ app, screen, browser }: Fixtures) {
  await app.open('/');
  await initApp(screen, browser);
  await browser.locator('#navItemRepos').click();
}

test('repositories: empty state, clone validation and the folder setting (real server)', async (fx) => {
  const { browser } = fx;
  await openRepos(fx);
  await expect(browser.locator('#reposRoot .rp-empty')).toBeVisible();

  // A local path is not a clonable URL: the server's message shows in the dialog and nothing is cloned.
  await browser.locator('#reposRoot .rp-bar [data-rp-clone]').click();
  await browser.locator('#rpUrl').fill('/etc/passwd');
  await browser.locator('#rpGo').click();
  await expect(browser.locator('#repoDlgErr')).toContainText('git url must start with');
  await browser.keyboard.press('Escape');
  await expect(browser.locator('#reposRoot .rp-empty')).toBeVisible();

  // The folder for new clones can be changed and put back to the default.
  await browser.locator('#reposRoot [data-rp-folder]').click();
  await browser.locator('#rpFolder').fill('/tmp/pantau-e2e-repos');
  await browser.locator('#repoDlgFooter .btn-primary').click();
  await expect(browser.locator('#reposRoot .rp-folder .mono')).toHaveText('/tmp/pantau-e2e-repos');
  await browser.locator('#reposRoot [data-rp-folder]').click();
  await browser.locator('#rpFolder').fill('');
  await browser.locator('#repoDlgFooter .btn-primary').click();
  await expect(browser.locator('#reposRoot .rp-folder .mono')).not.toHaveText('/tmp/pantau-e2e-repos');
});

// The repositories API is answered in the browser (git itself is covered by the Go tests): this checks the editor UI.
test('repositories: edit a file, save it, then commit and push with the chosen files and message', async (fx) => {
  const { browser } = fx;
  type Call = { method: string; path: string; body?: Record<string, unknown> };
  const calls: Call[] = [];
  let changes: { path: string; status: string }[] = [];
  const sha = 'a'.repeat(40);

  await browser.route('**/api/repos**', async (route) => {
    const req = route.request;
    const url = new URL(req.url);
    const sub = url.pathname.replace('/api/repos', '');
    const body = req.postData ? (JSON.parse(req.postData) as Record<string, unknown>) : undefined;
    calls.push({ method: req.method, path: sub + url.search, body });
    if (sub === '') {
      await route.fulfill({ json: { folder: '/x', repos: [{ id: 7, name: 'infra', url: 'git@h.example:o/infra.git', branch: 'main', dir: '/x/infra', auth: 'ssh', key_id: 1, token_set: false, synced_at: null, status: { branch: 'main', commit: sha, ahead: 0, behind: 0, dirty: changes.length } }] } });
    } else if (sub === '/7') {
      await route.fulfill({ json: { repo: {}, branch: 'main', commit: sha, ahead: 0, behind: 0, changes, changes_truncated: false } });
    } else if (sub === '/7/tree') {
      const folders: Record<string, unknown[]> = {
        '': [{ name: 'modules', dir: true, size: 0 }, { name: 'README.md', dir: false, size: 20 }, { name: 'main.tf', dir: false, size: 30 }],
        modules: [{ name: 'net', dir: true, size: 0 }],
        'modules/net': [{ name: 'vpc.tf', dir: false, size: 10 }],
      };
      await route.fulfill({ json: { path: url.searchParams.get('path') ?? '', entries: folders[url.searchParams.get('path') ?? ''] ?? [], truncated: false } });
    } else if (sub === '/7/search') {
      const q = url.searchParams.get('q') ?? '';
      await route.fulfill({ json: { files: 'modules/net/vpc.tf'.includes(q) ? [{ path: 'modules/net/vpc.tf', size: 10 }] : [], truncated: false } });
    } else if (sub === '/7/file' && req.method === 'GET') {
      const path = url.searchParams.get('path') ?? '';
      await route.fulfill({ json: { path, content: path === 'README.md' ? '# Title\n\nsome `code`\n' : 'resource "null_resource" "a" {}\n', size: 30 } });
    } else if (sub === '/7/file' && req.method === 'PUT') {
      changes = [{ path: String(body?.path), status: 'modified' }];
      await route.fulfill({ json: { ok: true } });
    } else if (sub === '/7/commit') {
      changes = [];
      await route.fulfill({ json: { ok: true, commit: 'f'.repeat(40), branch: 'main', pushed: true, changed: 1 } });
    } else {
      await route.fulfill({ status: 404, body: 'not found' });
    }
  });
  await openRepos(fx);

  await expect(browser.locator('#reposRoot .rp-card')).toContainText('infra');
  await browser.locator('[data-rp-open="7"]').click();
  await expect(browser.locator('#repoModal')).toBeVisible();
  await expect(browser.locator('#rpTree')).toContainText('main.tf');
  await expect(browser.locator('#rpTools [data-act="commit"]')).toBeDisabled();

  // The sidebar is a flex column: the search box must stay one line high, not stretch over the tree.
  const filterHeight = await browser.evaluate(() => document.querySelector('#rpFilter')!.getBoundingClientRect().height);
  expect(filterHeight).toBeLessThan(40);

  // Folders load one level at a time and start collapsed.
  await expect(browser.locator('#rpTree [data-file="modules/net/vpc.tf"]')).toBeHidden();
  await browser.locator('#rpTree [data-dir="modules"]').click();
  await browser.locator('#rpTree [data-dir="modules/net"]').click();
  await expect(browser.locator('#rpTree [data-file="modules/net/vpc.tf"]')).toBeVisible();

  // The filter searches the whole repository (server side), not just the opened folders.
  await browser.locator('#rpTree [data-dir="modules"]').click();
  await browser.locator('#rpFilter').fill('vpc');
  await expect(browser.locator('#rpTree [data-file="modules/net/vpc.tf"]')).toBeVisible();
  await expect(browser.locator('#rpTree [data-file="main.tf"]')).toBeHidden();
  await browser.locator('#rpFilter').fill('');
  await expect(browser.locator('#rpTree [data-file="main.tf"]')).toBeVisible();

  // Highlighting follows the file type: markdown headings get their own token class.
  await browser.locator('#rpTree [data-file="README.md"]').click();
  await expect(browser.locator('#rpEditor .cm-header')).toBeVisible();

  // Edit: the buffer is dirty until saved, and Save writes it to the clone.
  await browser.locator('#rpTree [data-file="main.tf"]').click();
  await expect(browser.locator('#rpFbar')).toContainText('main.tf');
  await browser.evaluate(() => {
    const host = document.querySelector('#rpEditor .CodeMirror');
    if (!host) throw new Error('editor is not mounted');
    const cm: { setValue(text: string): void } = Reflect.get(host, 'CodeMirror');
    cm.setValue('resource "null_resource" "b" {}\n');
    return null;
  });
  await expect(browser.locator('#rpFbar [data-act="save"]')).toBeEnabled();
  await browser.locator('#rpFbar [data-act="save"]').click();
  await expect(browser.locator('#rpTree [data-file="main.tf"] .rp-mark')).toHaveText('M');
  const put = calls.find(c => c.method === 'PUT' && c.path === '/7/file');
  if (!put || put.body?.path !== 'main.tf' || put.body?.content !== 'resource "null_resource" "b" {}\n') throw new Error('save did not send the edited file: ' + JSON.stringify(put));

  // Commit & Push sends the message, author and no explicit paths (every change selected).
  await browser.locator('#rpTools [data-act="commit"]').click();
  await browser.locator('#rpCmMsg').fill('rename resource');
  await browser.locator('#rpCmName').fill('Ann');
  await browser.locator('#rpCmEmail').fill('ann@example.com');
  await browser.locator('#rpCmGo').click();
  await expect(browser.locator('#rpTree [data-file="main.tf"] .rp-mark')).toBeHidden();
  await expect(browser.locator('#rpTools [data-act="commit"]')).toBeDisabled();
  const commit = calls.find(c => c.method === 'POST' && c.path === '/7/commit');
  const b = commit?.body;
  if (!b || b.message !== 'rename resource' || b.author_name !== 'Ann' || b.author_email !== 'ann@example.com' || b.new_branch !== '' || JSON.stringify(b.paths) !== '[]') throw new Error('unexpected commit request: ' + JSON.stringify(commit));
});

// The clone job is answered in the browser: the dialog must show git's output while it runs and end on the new card.
test('repositories: cloning shows git progress while it runs and ends with the new card', async (fx) => {
  const { browser } = fx;
  let polls = 0;
  let cloned = false;
  const card = { id: 3, name: 'infra', url: 'https://h.example/o/infra.git', branch: 'main', dir: '/x/infra', auth: 'none', key_id: 0, token_set: false, synced_at: null, status: { branch: 'main', commit: 'a'.repeat(40), ahead: 0, behind: 0, dirty: 0 } };
  await browser.route('**/api/repos**', async (route) => {
    const req = route.request;
    const sub = new URL(req.url).pathname.replace('/api/repos', '');
    if (sub === '' && req.method === 'GET') {
      await route.fulfill({ json: { folder: '/x', repos: cloned ? [card] : [] } });
    } else if (sub === '' && req.method === 'POST') {
      await route.fulfill({ status: 202, json: { job: 'j1' } });
    } else if (sub === '/clone/j1') {
      polls++;
      cloned = polls >= 3;
      await route.fulfill({ json: { state: cloned ? 'done' : 'running', name: 'infra', error: '', repo: cloned ? card : null, lines: ["Cloning into '/x/infra'...", `Receiving objects: ${Math.min(polls * 40, 100)}%`] } });
    } else {
      await route.fulfill({ status: 404, body: 'not found' });
    }
  });
  await openRepos(fx);

  await browser.locator('#reposRoot .rp-bar [data-rp-clone]').click();
  await browser.locator('#rpUrl').fill('https://h.example/o/infra.git');
  await browser.locator('#rpGo').click();
  await expect(browser.locator('#rpLog')).toContainText("Cloning into '/x/infra'");
  await expect(browser.locator('#repoDlgFooter')).toContainText('Cancel clone');
  await expect(browser.locator('#repoDlg')).toBeHidden();
  await expect(browser.locator('#reposRoot .rp-card')).toContainText('infra');
});

// Folders group the cards like workspace groups: a header with the repo count, collapsible, filterable, and Move changes it.
test('repositories: folders group the cards with a repo count and a repository can be moved to one', async (fx) => {
  const { browser } = fx;
  const mk = (id: number, name: string, group: string) => ({ id, name, group, url: `https://h.example/o/${name}.git`, branch: 'main', dir: `/x/${group}/${name}`, auth: 'none', key_id: 0, token_set: false, synced_at: null, status: { branch: 'main', commit: 'a'.repeat(40), ahead: 0, behind: 0, dirty: 0 } });
  const repos = [mk(1, 'api', 'Torcons'), mk(2, 'web', 'Torcons'), mk(3, 'misc', '')];
  let moved: unknown;
  await browser.route('**/api/repos**', async (route) => {
    const req = route.request;
    const sub = new URL(req.url).pathname.replace('/api/repos', '');
    if (sub === '' && req.method === 'GET') {
      await route.fulfill({ json: { folder: '/x', repos } });
    } else if (sub === '/3/group' && req.method === 'PUT') {
      moved = JSON.parse(req.postData ?? '{}');
      repos[2] = mk(3, 'misc', 'Torcons');
      await route.fulfill({ json: repos[2] });
    } else {
      await route.fulfill({ status: 404, body: 'not found' });
    }
  });
  await openRepos(fx);

  const head = browser.locator('#reposRoot [data-rp-toggle="Torcons"]');
  await expect(head).toContainText('Torcons');
  await expect(head).toContainText('2 repos');
  await expect(browser.locator('#reposRoot [data-rp-toggle="__none__"]')).toContainText('1 repo');
  await expect(browser.locator('#reposRoot .rp-card')).toHaveCount(3);

  // Collapsing a folder hides its cards; the choice survives a re-render.
  await head.click();
  await expect(browser.locator('#reposRoot .rp-card')).toHaveCount(1);
  await head.click();
  await expect(browser.locator('#reposRoot .rp-card')).toHaveCount(3);

  // The folder filter narrows the list.
  await browser.locator('#rpFolderFilter').selectOption('No folder');
  await expect(browser.locator('#reposRoot .rp-card')).toHaveCount(1);
  await browser.locator('#rpFolderFilter').selectOption('All folders');

  // Move the loose repository into Torcons.
  await browser.locator('[data-rp-move="3"]').click();
  await browser.locator('#rpMoveGroup').fill('Torcons');
  await browser.locator('#repoDlgFooter .btn-primary').click();
  await expect(browser.locator('#reposRoot [data-rp-toggle="Torcons"]')).toContainText('3 repos');
  await expect(browser.locator('#reposRoot [data-rp-toggle="__none__"]')).toBeHidden();
  if (JSON.stringify(moved) !== '{"group":"Torcons"}') throw new Error('unexpected move request: ' + JSON.stringify(moved));
});

// Copy to host: the dialog sends host, destination and the .git choice, then the Transfer Queue opens (copy API stubbed).
test('repositories: copy a repository to a host from its card', async (fx) => {
  const { browser } = fx;
  const card = { id: 3, name: 'infra', group: '', url: 'https://h.example/o/infra.git', branch: 'main', dir: '/x/infra', auth: 'none', key_id: 0, token_set: false, synced_at: null, status: { branch: 'main', commit: 'a'.repeat(40), ahead: 0, behind: 0, dirty: 2 } };
  let sent: Record<string, unknown> | undefined;
  await browser.route('**/api/repos**', async (route) => {
    const req = route.request;
    const sub = new URL(req.url).pathname.replace('/api/repos', '');
    if (sub === '' && req.method === 'GET') {
      await route.fulfill({ json: { folder: '/x', repos: [card] } });
    } else if (sub === '/3/copy' && req.method === 'POST') {
      sent = JSON.parse(req.postData ?? '{}') as Record<string, unknown>;
      await route.fulfill({ status: 202, json: { files: 5, skipped: 0, job: { id: 1, kind: 'repo', dest_host_name: 'web1' } } });
    } else {
      await route.fulfill({ status: 404, body: 'not found' });
    }
  });
  await openRepos(fx);
  const hostId = (await browser.evaluate(async () => {
    const r = await fetch('/api/hosts', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: 'web1', host: '10.0.0.9', port: 1, user: 'deploy' }) });
    return (await r.json()).id;
  })) as number;

  await browser.locator('[data-rp-copy="3"]').click();
  await expect(browser.locator('#repoDlgBody')).toContainText('2 uncommitted change(s) are included');
  await expect(browser.locator('#rpCopyPath')).toHaveValue('/opt/infra');
  await browser.locator('#rpCopyPath').fill('/srv/infra');
  await browser.locator('#rpCopyGit').check();
  await browser.locator('#rpCopyGo').click();
  await expect(browser.locator('#transfersListModal')).toBeVisible();
  if (JSON.stringify(sent) !== JSON.stringify({ host_id: hostId, path: '/srv/infra', include_git: true })) throw new Error('unexpected copy request: ' + JSON.stringify(sent));
});
