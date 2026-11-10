import { test, expect } from '@playwright/test';
import { OPERATOR, asIdentity } from '../fixtures/identity';

// Integration secrets on the Deployment page (ADR-00057). No integration
// registers a secret yet, so the shared server lists none: the page shows
// nothing new, and the secret routes answer 404 for every name. The controls
// themselves (enter, replace, clear, unreadable, missing key) are covered with
// a test registration over a real store in internal/web/settings; the first
// integration to register a field adds its e2e here.
test.describe('integration secrets', () => {
  test.use(asIdentity(OPERATOR));

  test('the Deployment page lists none while no integration has registered a secret', async ({ page }) => {
    const res = await page.goto('/operator/settings');
    expect(res?.status()).toBe(200);
    await expect(page.getByRole('heading', { name: 'Deployment', level: 1 })).toBeVisible();
    await expect(page.getByTestId('backup')).toBeVisible();
    await expect(page.getByTestId('integrations')).toHaveCount(0);
    await expect(page.locator('input[type="password"]')).toHaveCount(0);
  });

  test('saving or clearing a secret nothing registered is a 404', async ({ request }) => {
    const headers = { 'Sec-Fetch-Site': 'same-origin' };
    const save = await request.post('/operator/settings/secrets/mail/password', { form: { value: 'x' }, headers, maxRedirects: 0 });
    expect(save.status()).toBe(404);
    const clear = await request.post('/operator/settings/secrets/mail/password/clear', { headers, maxRedirects: 0 });
    expect(clear.status()).toBe(404);
  });
});
