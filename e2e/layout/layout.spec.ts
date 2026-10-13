import { test, expect } from '@playwright/test';
import { OPERATOR, OTHER, asIdentity } from '../fixtures/identity';

// Every top-level page is reachable from the primary nav, and the nav marks the
// page the reader is on.
const pages = [
  { name: 'Recipes', path: '/recipes' },
  { name: 'Add recipe', path: '/ingest' },
  { name: 'Catalog', path: '/catalog' },
  { name: 'Shopping', path: '/shopping' },
  { name: 'Settings', path: '/settings' },
];

test('the home page links to every top-level page', async ({ page }) => {
  await page.goto('/');
  const nav = page.getByRole('navigation', { name: 'Primary' });
  for (const p of pages) {
    await expect(nav.getByRole('link', { name: p.name })).toHaveAttribute('href', p.path);
  }
  await expect(nav.locator('[aria-current="page"]')).toHaveCount(0);
});

for (const p of pages) {
  test(`${p.name} is reached from the nav and marked current`, async ({ page }) => {
    await page.goto('/');
    const nav = page.getByRole('navigation', { name: 'Primary' });
    await nav.getByRole('link', { name: p.name }).click();
    await expect(page).toHaveURL(new RegExp(`${p.path}$`));
    const current = nav.locator('[aria-current="page"]');
    await expect(current).toHaveCount(1);
    await expect(current).toHaveText(p.name);
  });
}

// Settings is an operator page. DEV is a member and an operator (the default
// identity above); OTHER is a member who is not. The server refuses OTHER
// either way, so this is about what the nav offers.
test.describe('a member who is not an operator', () => {
  test.use(asIdentity(OTHER));

  test('has no Settings link in the nav', async ({ page }) => {
    await page.goto('/');
    const nav = page.getByRole('navigation', { name: 'Primary' });
    await expect(nav.getByRole('link', { name: 'Recipes' })).toBeVisible();
    await expect(nav.getByRole('link', { name: 'Settings' })).toHaveCount(0);
  });
});

test.describe('an operator who belongs to no household', () => {
  test.use(asIdentity(OPERATOR));

  test('sees Settings in the nav on the operator page', async ({ page }) => {
    await page.goto('/settings');
    const nav = page.getByRole('navigation', { name: 'Primary' });
    await expect(nav.getByRole('link', { name: 'Settings' })).toHaveAttribute('href', '/settings');
  });
});
