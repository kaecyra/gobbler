import { test, expect } from '@playwright/test';

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
