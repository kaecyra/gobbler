import { test, expect } from '@playwright/test';
import { OPERATOR, OTHER, STRANGER, asIdentity } from '../fixtures/identity';
import { expectTapTargets } from '../household/support';

// Admission (ADR-00018): Access admits any email that authenticates, so the
// app's membership check is the only lock. The dev-auth server seeds DEV and
// OTHER as members of separate households and lists OPERATOR and DEV as operators.

const NOT_A_MEMBER = 'You are not a member of a household';

test.describe('an authenticated non-member', () => {
  test.use(asIdentity(STRANGER));

  test('sees only the not-a-member page, whatever it asks for', async ({ page }) => {
    for (const path of ['/', '/recipes', '/shopping', '/catalog', '/settings', '/no/such/page']) {
      const res = await page.goto(path);
      expect(res?.status(), path).toBe(403);
      await expect(page.getByRole('heading', { name: NOT_A_MEMBER }), path).toBeVisible();
      await expect(page.getByTestId('nonmember-email')).toContainText(STRANGER);
      await expect(page.locator('body'), path).not.toContainText('Dev household');
    }
  });

  test('can open the not-a-member page itself and the health check', async ({ page, request }) => {
    const res = await page.goto('/not-a-member');
    expect(res?.status()).toBe(200);
    await expect(page.getByRole('heading', { name: NOT_A_MEMBER })).toBeVisible();
    expect((await request.get('/healthz')).status()).toBe(200);
  });

  test('is refused a write as well', async ({ request }) => {
    const res = await request.post('/settings/backup');
    expect(res.status()).toBe(403);
    expect(await res.text()).toContain(NOT_A_MEMBER);
  });
});

for (const email of [OTHER]) {
  test.describe(`${email}, a household member who is not the operator`, () => {
    test.use(asIdentity(email));

    test('reaches the app but is refused /settings', async ({ page }) => {
      expect((await page.goto('/'))?.status()).toBe(200);
      const res = await page.goto('/settings');
      expect(res?.status()).toBe(403);
      await expect(page.locator('body')).not.toContainText('Backup');
    });

    test('is sent home from the not-a-member page', async ({ page }) => {
      await page.goto('/not-a-member');
      expect(new URL(page.url()).pathname).toBe('/');
    });
  });
}

test.describe('the operator without a household', () => {
  test.use(asIdentity(OPERATOR));

  test('reaches /settings and nothing else of the app', async ({ page }) => {
    const settings = await page.goto('/settings');
    expect(settings?.status()).toBe(200);
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible();

    const recipes = await page.goto('/recipes');
    expect(recipes?.status()).toBe(403);
    await expect(page.getByRole('heading', { name: NOT_A_MEMBER })).toBeVisible();
  });
});

test.describe('the not-a-member page', () => {
  test.use(asIdentity(STRANGER));

  test('is drawn in the design system, with no control under 44px', async ({ page }) => {
    await page.goto('/not-a-member');
    await expect(page.locator('main img')).toBeVisible();
    await expectTapTargets(page);
  });
});
