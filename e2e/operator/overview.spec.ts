import { test, expect, type Page } from '@playwright/test';
import { OPERATOR, asIdentity } from '../fixtures/identity';
import { expectTapTargets } from '../fixtures/tap';

// The operator area (docs/ui/design.md, Operator area): an overview at /operator
// and a rail on every operator page linking each section. The shared dev-auth
// server lists OPERATOR (a member of no household) as an operator.

const rail = (page: Page) => page.getByRole('navigation', { name: 'Operator' });

const sections = [
  { name: 'Overview', path: '/operator', heading: 'Operator' },
  { name: 'Invitations', path: '/operator/invitations', heading: 'Household invitations' },
  { name: 'Suggested ingredients', path: '/operator/promotions', heading: 'Suggested ingredients' },
  { name: 'Central ingredients', path: '/operator/ingredients', heading: 'Central ingredients' },
  { name: 'Equipment', path: '/operator/equipment', heading: 'Equipment catalog' },
  { name: 'Deployment', path: '/operator/settings', heading: 'Deployment' },
];

test.describe('the operator area', () => {
  test.use(asIdentity(OPERATOR));

  test('the overview says what is waiting in each section', async ({ page }) => {
    const res = await page.goto('/operator');
    expect(res?.status()).toBe(200);
    await expect(page.getByRole('heading', { name: 'Operator', level: 1 })).toBeVisible();
    await expect(page.getByTestId('overview-invitations')).toContainText(/waiting/);
    await expect(page.getByTestId('overview-suggestions')).toContainText(/waiting for review/);
    await expect(page.getByTestId('overview-backup')).toContainText(/backup/i);
    await expect(page.locator('body')).not.toContainText('Dev household');
  });

  for (const s of sections) {
    test(`the rail reaches ${s.name} and marks it current`, async ({ page }) => {
      await page.goto(s.path === '/operator' ? '/operator/settings' : '/operator');
      await rail(page).getByRole('link', { name: s.name }).click();
      await expect(page).toHaveURL(new RegExp(`${s.path}$`));
      await expect(page.getByRole('heading', { name: s.heading, level: 1 })).toBeVisible();
      await expect(rail(page).getByRole('link', { name: s.name })).toHaveAttribute('aria-current', 'page');
      await expect(rail(page).locator('[aria-current]')).toHaveCount(1);
    });
  }

  test('the old /settings address sends a bookmark to the deployment page', async ({ page }) => {
    await page.goto('/settings');
    await expect(page).toHaveURL(/\/operator\/settings$/);
    await expect(page.getByRole('heading', { name: 'Deployment', level: 1 })).toBeVisible();
  });

  test('the rail turns into a strip above the page on a phone', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 800 });
    await page.goto('/operator');
    const r = await rail(page).boundingBox();
    const h = await page.getByRole('heading', { name: 'Operator', level: 1 }).boundingBox();
    expect(r && h && r.y + r.height <= h.y).toBe(true);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  });

  test('the overview keeps every control at least 44px tall', async ({ page }) => {
    await page.goto('/operator');
    expect(await expectTapTargets(page)).toBeGreaterThan(5);
  });
});
