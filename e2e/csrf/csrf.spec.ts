import { test, expect } from '@playwright/test';

// ADR-00016: unsafe methods are refused when the browser marks them
// cross-origin. A browser always sets Sec-Fetch-Site itself; the request
// context here sets it by hand to stand in for a page on another site.
const form = { text: '2 eggs\n1 cup flour\nWhisk.' };

test('a cross-site POST is refused', async ({ request }) => {
  const res = await request.post('/ingest', { form, headers: { 'Sec-Fetch-Site': 'cross-site' } });
  expect(res.status()).toBe(403);
});

test('a POST from a foreign Origin without Fetch metadata is refused', async ({ request }) => {
  const res = await request.post('/ingest', { form, headers: { Origin: 'https://evil.example' } });
  expect(res.status()).toBe(403);
});

test('a same-origin POST reaches the handler', async ({ request }) => {
  const res = await request.post('/ingest', {
    form,
    headers: { 'Sec-Fetch-Site': 'same-origin' },
    maxRedirects: 0,
  });
  expect(res.status()).not.toBe(403);
});

test('a cross-site GET and the health check are allowed', async ({ request }) => {
  const headers = { 'Sec-Fetch-Site': 'cross-site' };
  expect((await request.get('/ingest', { headers })).status()).toBe(200);
  expect((await request.get('/healthz', { headers })).status()).toBe(200);
});
