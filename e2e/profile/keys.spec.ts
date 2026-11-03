import { test, expect, type APIRequestContext, type Page, type Playwright } from '@playwright/test';
import { OTHER, asIdentity } from '../fixtures/identity';
import { expectTapTargets } from '../fixtures/tap';

// Personal API keys on the profile (ADR-00031, ADR-00032). The server is shared
// with the other specs, which run in parallel, so each test acts as an email of
// its own.

let counter = 0;
function freshEmail(label: string): string {
  counter += 1;
  return `${label}-${process.pid}-${Date.now()}-${counter}@e2e.example`;
}

const SECRET = /^gob_[A-Za-z0-9_-]{43}$/;
const SHOTS = new URL('../../.cc-scratch/shots/', import.meta.url).pathname;

async function as(playwright: Playwright, baseURL: string, email: string): Promise<APIRequestContext> {
  return playwright.request.newContext({ baseURL, ...asIdentity(email) });
}

/** The address (without /profile/) of the signed-in person's own profile. */
async function addressOf(ctx: APIRequestContext): Promise<string> {
  const res = await ctx.get('/profile', { maxRedirects: 0 });
  expect(res.status()).toBe(303);
  return res.headers()['location'].replace('/profile/', '');
}

async function openKeys(page: Page): Promise<void> {
  await page.goto('/profile');
  await page.getByRole('link', { name: 'Manage your API keys' }).click();
  await expect(page.getByRole('heading', { name: 'Your API keys' })).toBeVisible();
}

async function makeKey(page: Page, name: string, expires: string): Promise<string> {
  await page.getByLabel('Name').fill(name);
  await page.getByLabel('Expires').selectOption({ label: expires });
  await page.getByRole('button', { name: 'Make a key' }).click();
  const secret = page.getByTestId('key-secret');
  await expect(secret).toBeVisible();
  const value = await secret.inputValue();
  expect(value).toMatch(SECRET);
  return value;
}

test.describe('your own keys', () => {
  const me = freshEmail('keys-me');
  test.use(asIdentity(me));

  test('are made with a name and an expiry, shown once, listed without the secret, and revoked', async ({ page }) => {
    await openKeys(page);
    await expect(page.getByTestId('api-keys-empty')).toBeVisible();

    const secret = await makeKey(page, 'Laptop agent', '30 days');
    await page.screenshot({ path: `${SHOTS}keys-profile-created.png` });
    await page.getByRole('link', { name: 'Back to your keys' }).click();

    const row = page.getByTestId('api-key').filter({ hasText: 'Laptop agent' });
    await expect(row).toHaveCount(1);
    await expect(row).toContainText(secret.slice(0, 12));
    await expect(row).toContainText('Expires');
    await expect(row).toContainText('Never used');
    // The secret is on no page after the one that made it.
    expect(await page.content()).not.toContain(secret);
    await page.goto('/profile');
    expect(await page.content()).not.toContain(secret);

    await openKeys(page);
    await makeKey(page, 'Forever', 'No expiry');
    await page.getByRole('link', { name: 'Back to your keys' }).click();
    await expect(page.getByTestId('api-key').filter({ hasText: 'Forever' })).toContainText('No expiry');
    await page.screenshot({ path: `${SHOTS}keys-profile-list.png`, fullPage: true });

    await page.getByRole('button', { name: 'Revoke the key Laptop agent' }).click();
    await expect(page.getByTestId('api-key').filter({ hasText: 'Laptop agent' })).toHaveCount(0);
    await expect(page.getByTestId('api-key').filter({ hasText: 'Forever' })).toHaveCount(1);
  });

  test('refuse a blank name and say why', async ({ page }) => {
    await openKeys(page);
    // The field is required, so send the form past the browser's check.
    await page.getByLabel('Name').evaluate((el: HTMLInputElement) => {
      el.removeAttribute('required');
      el.value = '   ';
    });
    await page.getByRole('button', { name: 'Make a key' }).click();
    await expect(page.getByText('a key needs a name')).toBeVisible();
  });

  test('have controls at least 44px tall, on a laptop and on a phone', async ({ page }) => {
    for (const [label, viewport] of [['1024', { width: 1024, height: 768 }], ['390', { width: 390, height: 844 }]] as const) {
      await page.setViewportSize(viewport);
      await page.goto('/profile');
      await expectTapTargets(page, 'main');
      await openKeys(page);
      await makeKey(page, `Tap ${label}`, '90 days');
      await expectTapTargets(page, 'main');
      await page.getByRole('link', { name: 'Back to your keys' }).click();
      await expectTapTargets(page, 'main');
      for (const mode of ['light', 'dark']) {
        await page.evaluate((m) => document.documentElement.setAttribute('data-mode', m), mode);
        await page.screenshot({ path: `${SHOTS}keys-profile-${label}-${mode}.png`, fullPage: true });
      }
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
      expect(overflow, `${viewport.width}px`).toBeLessThanOrEqual(0);
    }
  });
});

test.describe('another person', () => {
  const owner = freshEmail('keys-owner');
  let addr = '';
  let revokePath = '';

  test.beforeAll(async ({ playwright, baseURL }) => {
    const ctx = await as(playwright, baseURL!, owner);
    addr = await addressOf(ctx);
    const made = await ctx.post(`/profile/${addr}/keys`, { form: { name: 'Owner private key', lifetime: '30d' } });
    expect(made.status()).toBe(200);
    const list = await (await ctx.get(`/profile/${addr}/keys`)).text();
    const m = list.match(/\/keys\/(\d+)\/revoke/);
    expect(m).not.toBeNull();
    revokePath = `/profile/${addr}/keys/${m![1]}/revoke`;
    await ctx.dispose();
  });

  for (const [label, who] of [['a member of a household', OTHER], ['a person with no household', freshEmail('keys-nomember')]] as const) {
    test(`as ${label} cannot see, make or revoke the owner's keys`, async ({ playwright, baseURL }) => {
      const ctx = await as(playwright, baseURL!, who);
      expect((await ctx.get(`/profile/${addr}/keys`)).status()).toBe(404);
      expect((await ctx.post(`/profile/${addr}/keys`, { form: { name: 'Hijack', lifetime: '30d' } })).status()).toBe(404);
      expect((await ctx.post(revokePath, { maxRedirects: 0 })).status()).toBe(404);
      // The profile page itself does not offer the section.
      expect(await (await ctx.get(`/profile/${addr}`)).text()).not.toContain('API keys');
      // Nor does the key id work through their own profile address.
      const own = await addressOf(ctx);
      const id = revokePath.split('/keys/')[1].split('/')[0];
      expect((await ctx.post(`/profile/${own}/keys/${id}/revoke`, { maxRedirects: 0 })).status()).toBe(404);
      await ctx.dispose();

      const ownerCtx = await as(playwright, baseURL!, owner);
      const list = await (await ownerCtx.get(`/profile/${addr}/keys`)).text();
      expect(list).toContain('Owner private key');
      expect(list).not.toContain('Hijack');
      await ownerCtx.dispose();
    });
  }
});
