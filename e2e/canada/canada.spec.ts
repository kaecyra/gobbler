import { test, expect } from '@playwright/test';
import { STRANGER, asIdentity } from '../fixtures/identity';

// /canada is a public path (ADR-00029). This server runs the dev auth bypass,
// which always supplies an identity, so here the page is opened by a member and
// by a stranger who belongs to no household. Opening it with no token at all is
// in health/health.spec.ts, against the server without the bypass.

test('a member reaches /canada from the footer', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('contentinfo').getByRole('link', { name: 'Canada', exact: true }).click();
  await expect(page).toHaveURL(/\/canada$/);
  await expect(page.getByRole('heading', { name: 'Your recipes stay in Canada', level: 1 })).toBeVisible();
  await expect(page.getByTestId('canada-parts').getByRole('listitem')).toHaveCount(6);
});

test.describe('a stranger who belongs to no household', () => {
  test.use(asIdentity(STRANGER));

  test('can open /canada, and the nav offers nothing', async ({ page }) => {
    const res = await page.goto('/canada');
    expect(res?.status()).toBe(200);
    await expect(page.getByRole('heading', { name: 'Your recipes stay in Canada', level: 1 })).toBeVisible();
    await expect(page.getByRole('navigation', { name: 'Primary' }).getByRole('link')).toHaveCount(0);
  });
});
