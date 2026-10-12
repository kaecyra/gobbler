import { test, expect } from '@playwright/test';

// ADR-00003: /healthz is the only route served without a verified Access
// identity. This runs against the second server instance, started with Access
// verification on and a fake team domain/AUD (never real credentials), so the
// requests below carry no token and are judged by the real middleware.
const accessURL = process.env.GOBBLER_E2E_ACCESS_URL;
if (!accessURL) throw new Error('GOBBLER_E2E_ACCESS_URL is not set; run through playwright.config.ts');
test.use({ baseURL: accessURL });

test('GET /healthz succeeds without credentials', async ({ request }) => {
  const res = await request.get('/healthz');
  expect(res.status()).toBe(200);
  expect(res.headers()['content-type']).toContain('application/json');
  expect(await res.json()).toMatchObject({ status: 'ok' });
});

test('every other route is refused without a token', async ({ request }) => {
  for (const path of ['/', '/static/app.css']) {
    const res = await request.get(path);
    expect(res.status(), path).toBe(403);
  }
});
