import { test, expect } from '@playwright/test';

// ADR-00003: the ingest routes are behind Access. Run against the instance
// with Access verification on and no token.
const accessURL = process.env.GOBBLER_E2E_ACCESS_URL;
if (!accessURL) throw new Error('GOBBLER_E2E_ACCESS_URL is not set; run through playwright.config.ts');
test.use({ baseURL: accessURL });

test('the ingest routes are refused without a token', async ({ request }) => {
  expect((await request.get('/ingest')).status()).toBe(403);
  expect((await request.post('/ingest', { form: { text: 'pie' } })).status()).toBe(403);
  expect((await request.get('/ingest/abc')).status()).toBe(403);
});
