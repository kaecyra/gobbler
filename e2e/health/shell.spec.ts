import { test, expect } from '@playwright/test';

// Web shell routes registered next to /healthz in internal/web. They live here
// until the feature suites replace the placeholders.

test('the home page renders the base layout', async ({ page }) => {
  const res = await page.goto('/');
  expect(res?.status()).toBe(200);
  await expect(page).toHaveTitle(/Home/);
});

test('embedded static assets are served', async ({ request }) => {
  for (const asset of ['/static/app.css', '/static/htmx.min.js']) {
    const res = await request.get(asset);
    expect(res.status(), asset).toBe(200);
  }
});

test('an unknown route renders the not-found page', async ({ page }) => {
  const res = await page.goto('/no-such-page');
  expect(res?.status()).toBe(404);
});
