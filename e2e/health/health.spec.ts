import { test, expect } from '@playwright/test';

// ADR-00029: the public paths (/healthz, /canada and /static/*) are served
// without a verified identity; every other route needs one (ADR-00030). This
// runs against the anonymous server instance, started without the dev bypass
// and with a fake issuer (never real credentials), so the requests below carry
// no session and are judged by the real middleware.
const anonymousURL = process.env.GOBBLER_E2E_ANONYMOUS_URL;
if (!anonymousURL) throw new Error('GOBBLER_E2E_ANONYMOUS_URL is not set; run through playwright.config.ts');
test.use({ baseURL: anonymousURL });

test('GET /healthz succeeds without credentials', async ({ request }) => {
  const res = await request.get('/healthz');
  expect(res.status()).toBe(200);
  expect(res.headers()['content-type']).toContain('application/json');
  expect(await res.json()).toMatchObject({ status: 'ok' });
});

test('the public pages and assets are served without a session', async ({ request }) => {
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

test('every other page sends a visitor without a session to sign in', async ({ request }) => {
  for (const path of ['/', '/recipes', '/settings', '/not-a-member', '/invitations/Zz000000', '/canada/']) {
    // Not followed: sign-in itself would go on to the issuer.
    const res = await request.get(path, { maxRedirects: 0 });
    expect(res.status(), path).toBe(303);
    expect(res.headers()['location'], path).toBe(`/login?next=${encodeURIComponent(path)}`);
  }
  // The identity middleware lets the decoded /canada by; the membership
  // middleware judges the raw path and refuses it.
  expect((await request.get('/c%61nada', { maxRedirects: 0 })).status()).toBe(403);
});

test('a request other than a full-page GET is refused with 401', async ({ request }) => {
  expect((await request.get('/recipes', { headers: { 'HX-Request': 'true' } })).status()).toBe(401);
  expect((await request.post('/recipes', { form: { title: 'pie' } })).status()).toBe(401);
});
