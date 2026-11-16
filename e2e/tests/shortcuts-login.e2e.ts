import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { initApp } from './helpers.js';

// The site's login form is answered in the browser, so no second server is needed. What is
// proven here is the contract: the tile links to Pantau's auto login page, that page posts
// the credentials to the site, and the password never travels back to the browser.
test('shortcuts: a saved password makes the tile sign in, and can be forgotten again', async ({ app, screen, browser }) => {
  await app.open('/');
  await initApp(screen, browser);

  let posted = '';
  await browser.route('https://panel.example.com/login', async (route) => {
    posted = route.request.postData || '';
    await route.fulfill({ body: '<html><body>signed in</body></html>', contentType: 'text/html' });
  });

  // Add a shortcut with a saved password.
  await browser.locator('#navItemShortcuts').click();
  await browser.locator('[data-sc-add]').click();
  await browser.locator('#scName').fill('Panel');
  await browser.locator('#scUrl').fill('https://panel.example.com');
  await browser.locator('#scLoginBox summary').click();
  await browser.locator('#scLoginUser').fill('admin');
  await browser.locator('#scLoginPass').fill('s3cret pass');
  await browser.locator('#scLoginAction').fill('https://panel.example.com/login');
  await browser.locator('#scSave').click();

  // The tile now points at the auto login page instead of the site.
  const tile = browser.locator('#sectionShortcuts a[href^="/shortcut-login/"]');
  await expect(tile).toHaveCount(1);
  const bridge = (await browser.evaluate(() => document.querySelector('#sectionShortcuts a[href^="/shortcut-login/"]')!.getAttribute('href'))) as string;
  expect(bridge.startsWith('/shortcut-login/')).toBe(true);

  // The list the browser receives says a password is stored, but never carries it.
  const listed = (await browser.evaluate(async () => (await fetch('/api/shortcuts')).text())) as string;
  expect(listed).toContain('"login_pass_set":true');
  expect(listed.includes('s3cret pass')).toBe(false);

  // Opening the shortcut posts the credentials to the site's own login form.
  await app.open(bridge);
  await expect(browser.locator('body')).toContainText('signed in');
  expect(posted).toContain('username=admin');
  expect(posted).toContain('password=s3cret+pass');

  // "Forget the saved password" turns the tile back into a plain link.
  await app.open('/');
  await initApp(screen, browser);
  await browser.locator('#navItemShortcuts').click();
  await browser.locator('[data-sc-edit="0"]').click();
  await browser.locator('#scLoginClear').click();
  await browser.locator('#scSave').click();
  await expect(browser.locator('#sectionShortcuts a[href="https://panel.example.com"]')).toHaveCount(1);
  await expect(browser.locator('#sectionShortcuts a[href^="/shortcut-login/"]')).toHaveCount(0);
});
