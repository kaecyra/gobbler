import { test, expect } from '@playwright/test';
import { OTHER, OPERATOR, asIdentity } from '../fixtures/identity';

// ADR-00058 obligation 5: the usage page is an operator's alone. The shared
// dev-auth server lists OPERATOR and DEV as operators; OTHER is a member of a
// household who is not.

test.describe('a member who is not an operator', () => {
  test.use(asIdentity(OTHER));

  test('is refused the usage page and cannot sync or set prices', async ({ request }) => {
    const headers = { 'Sec-Fetch-Site': 'same-origin' };
    expect((await request.get('/operator/usage')).status()).toBe(403);
    for (const path of ['/operator/usage/sync', '/operator/usage/prices', '/operator/usage/prices/clear']) {
      const res = await request.post(path, { form: { provider: 'claude', model: 'm', input: '1', output: '1' }, headers, maxRedirects: 0 });
      expect(res.status(), path).toBe(403);
    }
  });

  test('is not shown the LLM usage link', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('link', { name: 'LLM usage' })).toHaveCount(0);
  });
});

test.describe('the operator', () => {
  test.use(asIdentity(OPERATOR));

  test('opens the usage page with no calls made yet', async ({ page }) => {
    const res = await page.goto('/operator/usage');
    expect(res?.status()).toBe(200);
    await expect(page.getByRole('heading', { name: 'LLM usage', level: 1 })).toBeVisible();
    await expect(page.getByTestId('usage-calls').getByTestId('stat-value')).toHaveText('0');
  });
});
