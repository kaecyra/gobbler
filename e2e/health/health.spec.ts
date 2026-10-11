import { test, expect } from '@playwright/test';

test('GET /healthz succeeds without credentials', async ({ playwright, baseURL }) => {
  // A bare context: no cookies, no Access headers.
  const ctx = await playwright.request.newContext({ baseURL, extraHTTPHeaders: {} });
  const res = await ctx.get('/healthz');
  expect(res.status()).toBe(200);
  expect(res.headers()['content-type']).toContain('application/json');
  expect(await res.json()).toMatchObject({ status: 'ok' });
  await ctx.dispose();
});
