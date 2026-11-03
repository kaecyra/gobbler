import { test, expect } from '@playwright/test';
import { OTHER, asIdentity } from '../fixtures/identity';
import { expectTapTargets } from '../fixtures/tap';
import { connectApp } from '../oauth/support';

// Connected apps on the profile (ADR-00034, ADR-00032). Only a member can approve
// a client, so these tests act as DEV, the default identity of the dev server.
// The server is shared with the other specs, which run in parallel, so each test
// names its apps for itself and looks only at the rows that carry the name.

let counter = 0;
function uniqueName(label: string): string {
  counter += 1;
  return `${label} ${process.pid}-${Date.now()}-${counter}`;
}

test.describe('your connected apps', () => {
  test('are listed with the host they return to, and revoking one ends its tokens', async ({ page, request }) => {
    const kitchen = uniqueName('Kitchen agent');
    const shopping = uniqueName('Shopping agent');
    const first = await connectApp(request, kitchen);
    await connectApp(request, shopping);

    await page.goto('/profile');
    await page.getByRole('link', { name: 'Manage connected apps' }).click();
    await expect(page.getByRole('heading', { name: 'Your connected apps' })).toBeVisible();
    const row = page.getByTestId('grant').filter({ hasText: kitchen });
    await expect(row).toHaveCount(1);
    await expect(row).toContainText('127.0.0.1:41699');
    await expect(page.getByTestId('grant').filter({ hasText: shopping })).toHaveCount(1);
    await expectTapTargets(page);
    expect(await page.content()).not.toContain(first.tokens.access_token);

    await page.getByRole('button', { name: `Revoke access for ${kitchen}` }).click();
    await expect(page.getByTestId('grant').filter({ hasText: kitchen })).toHaveCount(0);
    await expect(page.getByTestId('grant').filter({ hasText: shopping })).toHaveCount(1);

    const refresh = await request.post('/oauth/token', {
      form: { grant_type: 'refresh_token', client_id: first.clientId, refresh_token: first.tokens.refresh_token, resource: first.resource },
      failOnStatusCode: false,
    });
    expect(refresh.status()).toBe(400);
    expect((await refresh.json()).error).toBe('invalid_grant');
  });

  test('are not shown to another person', async ({ playwright, baseURL, request }) => {
    const name = uniqueName('Private agent');
    await connectApp(request, name);
    const other = await playwright.request.newContext({ baseURL, ...asIdentity(OTHER) });
    try {
      const mine = await request.get('/profile', { maxRedirects: 0 });
      const address = mine.headers()['location'];
      const res = await other.get(`${address}/grants`, { failOnStatusCode: false });
      expect(res.status()).toBe(404);
      expect(await res.text()).not.toContain(name);
    } finally {
      await other.dispose();
    }
  });
});
