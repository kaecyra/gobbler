import { test, expect, type APIRequestContext, type Playwright } from '@playwright/test';
import { OTHER, asIdentity } from '../fixtures/identity';
import { expectTapTargets } from '../fixtures/tap';

// The profile page (ADR-00032). The server is shared with the other specs, which
// run in parallel, so a test that changes a profile does so as an email of its own.

let counter = 0;
function freshEmail(label: string): string {
  counter += 1;
  return `${label}-${process.pid}-${Date.now()}-${counter}@e2e.example`;
}

const ADDRESS = /^\/profile\/[A-Za-z0-9_-]{22}$/;
const ADDRESS_URL = /\/profile\/[A-Za-z0-9_-]{22}$/;
const header = (page: import('@playwright/test').Page) => page.getByRole('banner');

async function as(playwright: Playwright, baseURL: string, email: string): Promise<APIRequestContext> {
  return playwright.request.newContext({ baseURL, ...asIdentity(email) });
}

/** Opens email's own profile through /profile and returns its path. */
async function addressOf(ctx: APIRequestContext): Promise<string> {
  const res = await ctx.get('/profile', { maxRedirects: 0 });
  expect(res.status()).toBe(303);
  const loc = res.headers()['location'];
  expect(loc).toMatch(ADDRESS);
  return loc;
}

test.describe('your own profile', () => {
  const me = freshEmail('profile-me');
  test.use(asIdentity(me));

  test('is reached from the account menu at a random address that holds no email', async ({ page }) => {
    await page.goto('/');
    await header(page).getByRole('button', { name: 'Account menu' }).click();
    await header(page).getByRole('link', { name: 'Profile' }).click();
    const path = new URL(page.url()).pathname;
    expect(path).toMatch(ADDRESS);
    expect(path.toLowerCase()).not.toContain(me.split('@')[0].toLowerCase());
    expect(decodeURIComponent(page.url())).not.toContain('@');

    await expect(page.getByRole('heading', { level: 1 })).toHaveText(me.split('@')[0]);
    await expect(page.getByTestId('profile-email')).toHaveText(me);
    await expect(page.getByText('Your profile', { exact: true })).toBeVisible();
    // Visiting again lands on the same address.
    await page.goto('/profile');
    expect(new URL(page.url()).pathname).toBe(path);
  });

  test('shows the default avatar drawn in the page and loads nothing from another origin', async ({ page, baseURL }) => {
    const origins = new Set<string>();
    page.on('request', (r) => {
      if (r.url().startsWith('http')) origins.add(new URL(r.url()).origin);
    });
    await page.goto('/profile');
    const avatar = page.getByTestId('profile-avatar').locator('svg.avatar');
    await expect(avatar).toBeVisible();
    const box = await avatar.boundingBox();
    expect(box!.width).toBeGreaterThanOrEqual(64);
    await expect(avatar.locator('path')).toHaveAttribute('d', /^M/);
    await page.waitForLoadState('networkidle');
    expect([...origins]).toEqual([new URL(baseURL!).origin]);
  });

  test('keeps the same avatar for the same person in the header and on the page', async ({ page }) => {
    await page.goto('/profile');
    const header_ = await header(page).locator('svg.avatar path').getAttribute('d');
    const body = await page.getByTestId('profile-avatar').locator('svg.avatar path').getAttribute('d');
    expect(header_).toBe(body);
  });

  test('sets a display name, which is shown escaped, and clears it back to the default', async ({ page }) => {
    await page.goto('/profile');
    const field = page.getByRole('textbox', { name: 'Name' });
    await expect(field).toHaveValue('');
    await expect(field).toHaveAttribute('placeholder', me.split('@')[0]);
    await field.fill('Chef <b>Gobbler</b>');
    await page.getByRole('button', { name: 'Save' }).click();
    await expect(page).toHaveURL(ADDRESS_URL);
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Chef <b>Gobbler</b>');
    await expect(page.getByRole('textbox', { name: 'Name' })).toHaveValue('Chef <b>Gobbler</b>');

    await page.getByRole('textbox', { name: 'Name' }).fill('');
    await page.getByRole('button', { name: 'Save' }).click();
    await expect(page.getByRole('heading', { level: 1 })).toHaveText(me.split('@')[0]);
  });

  test('refuses a name that is too long and says why', async ({ page }) => {
    await page.goto('/profile');
    // The field limits typing, so send the form past it.
    await page.getByRole('textbox', { name: 'Name' }).evaluate((el: HTMLInputElement) => {
      el.removeAttribute('maxlength');
      el.value = 'x'.repeat(61);
    });
    await page.getByRole('button', { name: 'Save' }).click();
    await expect(page.getByText('at most 60 characters')).toBeVisible();
  });

  test('has controls at least 44px tall, on a laptop and on a phone', async ({ page }) => {
    for (const viewport of [{ width: 1024, height: 768 }, { width: 390, height: 844 }]) {
      await page.setViewportSize(viewport);
      await page.goto('/profile');
      await expectTapTargets(page, 'main');
      await expectTapTargets(page, 'header');
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
      expect(overflow, `${viewport.width}px`).toBeLessThanOrEqual(0);
    }
  });
});

test.describe('someone else\'s profile', () => {
  const owner = freshEmail('profile-owner');
  let path = '';

  test.beforeAll(async ({ playwright, baseURL }) => {
    const ctx = await as(playwright, baseURL!, owner);
    path = await addressOf(ctx);
    const res = await ctx.post(`${path}/name`, { form: { name: 'Owner Name' }, maxRedirects: 0 });
    expect(res.status()).toBe(303);
    await ctx.dispose();
  });

  test.describe('as a member of a household', () => {
    test.use(asIdentity(OTHER));

    test('shows the person, and none of the owner\'s own controls', async ({ page }) => {
      await page.goto(path);
      await expect(page.getByRole('heading', { level: 1 })).toHaveText('Owner Name');
      await expect(page.getByTestId('profile-email')).toHaveText(owner);
      await expect(page.getByTestId('profile-avatar').locator('svg.avatar')).toBeVisible();
      await expect(page.getByText('Your profile')).toHaveCount(0);
      await expect(page.getByRole('textbox', { name: 'Name' })).toHaveCount(0);
      await expect(page.getByRole('button', { name: 'Save' })).toHaveCount(0);
    });

    test('cannot change it', async ({ request, playwright, baseURL }) => {
      const res = await request.post(`${path}/name`, { form: { name: 'Hijacked' }, maxRedirects: 0 });
      expect(res.status()).toBe(404);
      const ctx = await as(playwright, baseURL!, owner);
      expect(await (await ctx.get(path)).text()).not.toContain('Hijacked');
      await ctx.dispose();
    });
  });
});

test.describe('an address that names no profile', () => {
  const guesses = ['A'.repeat(22), 'dev@localhost', 'dev', '../recipes'];

  test('is a 404 page, for a member and for a person with no household', async ({ playwright, baseURL }) => {
    for (const who of [OTHER, freshEmail('profile-guess')]) {
      const ctx = await as(playwright, baseURL!, who);
      for (const g of guesses) {
        const res = await ctx.get(`/profile/${encodeURIComponent(g)}`);
        expect(res.status(), `${who} GET ${g}`).toBe(404);
        const post = await ctx.post(`/profile/${encodeURIComponent(g)}/name`, { form: { name: 'x' } });
        expect(post.status(), `${who} POST ${g}`).toBe(404);
      }
      await ctx.dispose();
    }
  });
});

// A person with no household may open their own profile (admission, ADR-00032) and
// still reaches nothing else.
test.describe('a person who belongs to no household', () => {
  const me = freshEmail('profile-nomember');
  test.use(asIdentity(me));

  test('opens their own profile from the account menu and is refused the rest of the app', async ({ page }) => {
    expect((await page.goto('/recipes'))?.status()).toBe(403);
    await header(page).getByRole('button', { name: 'Account menu' }).click();
    await header(page).getByRole('link', { name: 'Profile' }).click();
    await expect(page).toHaveURL(ADDRESS_URL);
    await expect(page.getByTestId('profile-email')).toHaveText(me);
    await expect(page.getByRole('textbox', { name: 'Name' })).toBeVisible();
  });
});
