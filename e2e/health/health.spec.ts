import { test, expect } from '@playwright/test';

// ADR-00029: the public paths (/healthz, /canada and /static/*) are served
// without a verified identity; every other route needs one (ADR-00003). This
// runs against the second server instance, started with Access verification on
// and a fake team domain/AUD (never real credentials), so the requests below
// carry no token and are judged by the real middleware.
const accessURL = process.env.GOBBLER_E2E_ACCESS_URL;
if (!accessURL) throw new Error('GOBBLER_E2E_ACCESS_URL is not set; run through playwright.config.ts');
test.use({ baseURL: accessURL });

test('GET /healthz succeeds without credentials', async ({ request }) => {
  const res = await request.get('/healthz');
  expect(res.status()).toBe(200);
  expect(res.headers()['content-type']).toContain('application/json');
  expect(await res.json()).toMatchObject({ status: 'ok' });
});

test('the public pages and assets are served without a token', async ({ request }) => {
  for (const path of ['/canada', '/static/app.css']) {
    const res = await request.get(path);
    expect(res.status(), path).toBe(200);
  }
});

test('an anonymous visitor reads /canada with an empty nav', async ({ page }) => {
  const res = await page.goto('/canada');
  expect(res?.status()).toBe(200);
  await expect(page.getByRole('heading', { name: 'Made in Canada', level: 1 })).toBeVisible();
  await expect(page.getByRole('navigation', { name: 'Primary' }).getByRole('link')).toHaveCount(0);
});

test('every other route is refused without a token', async ({ request }) => {
  for (const path of ['/', '/recipes', '/settings', '/not-a-member', '/invitations/Zz000000', '/canada/', '/c%61nada']) {
    const res = await request.get(path);
    expect(res.status(), path).toBe(403);
  }
});
