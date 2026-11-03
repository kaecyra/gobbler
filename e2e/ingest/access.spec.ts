import { test, expect } from '@playwright/test';

// ADR-00030: the ingest routes need a sign-in session. Run against the
// instance without the dev bypass, with no session.
const anonymousURL = process.env.GOBBLER_E2E_ANONYMOUS_URL;
if (!anonymousURL) throw new Error('GOBBLER_E2E_ANONYMOUS_URL is not set; run through playwright.config.ts');
test.use({ baseURL: anonymousURL });

test('the ingest routes are refused without a session', async ({ request }) => {
  for (const path of ['/ingest', '/ingest/abc']) {
    const res = await request.get(path, { maxRedirects: 0 });
    expect(res.status(), path).toBe(303);
    expect(res.headers()['location'], path).toBe(`/login?next=${encodeURIComponent(path)}`);
  }
  expect((await request.post('/ingest', { form: { text: 'pie' } })).status()).toBe(401);
});
