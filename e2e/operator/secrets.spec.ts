import { test, expect } from '@playwright/test';
import { OPERATOR, asIdentity } from '../fixtures/identity';

// The Mail section of the Deployment page (ADR-00055, ADR-00057). The shared
// server has no SMTP host and no encryption key, so what it can show is the
// fields, the refusals, and that nothing is saved by a refused form. Saving a
// password, replacing it, clearing it and the unreadable state are covered
// over a real store in internal/web/settings; sending is covered against a
// fake SMTP server in internal/mail and internal/api/households.
test.describe('mail settings', () => {
  test.use(asIdentity(OPERATOR));

  test('the Deployment page lists the mail fields and the password control', async ({ page }) => {
    const res = await page.goto('/operator/settings');
    expect(res?.status()).toBe(200);
    await expect(page.getByRole('heading', { name: 'Deployment', level: 1 })).toBeVisible();
    await expect(page.getByTestId('backup')).toBeVisible();
    const mail = page.getByTestId('integration-mail');
    await expect(mail.getByRole('heading', { name: 'Mail' })).toBeVisible();
    for (const f of ['host', 'port', 'user', 'from', 'timeout']) {
      await expect(mail.getByTestId(`value-${f}`)).toBeVisible();
      await expect(mail.getByTestId(`value-${f}`)).toHaveValue('');
    }
    await expect(mail.getByTestId('secret-state')).toHaveText('Not set');
    await expect(mail.locator('input[type="password"]')).toHaveCount(1);
  });

  test('a host without the rest is refused and says which field is missing', async ({ page }) => {
    await page.goto('/operator/settings');
    const mail = page.getByTestId('integration-mail');
    await mail.getByTestId('value-host').fill('smtp.example.com');
    await mail.getByTestId('value-port').fill('587');
    await mail.getByTestId('value-from').fill('Gobbler <invites@example.com>');
    await mail.getByRole('button', { name: 'Save', exact: true }).first().click();
    await expect(page.getByTestId('integrations-problem')).toHaveText('User is required when a host is set.');
    // What was typed stays in the form, and nothing was saved.
    await expect(page.getByTestId('integration-mail').getByTestId('value-host')).toHaveValue('smtp.example.com');
    await page.reload();
    await expect(page.getByTestId('integration-mail').getByTestId('value-host')).toHaveValue('');
  });

  test('a complete form with no password saved is refused', async ({ page }) => {
    await page.goto('/operator/settings');
    const mail = page.getByTestId('integration-mail');
    await mail.getByTestId('value-host').fill('smtp.example.com');
    await mail.getByTestId('value-port').fill('587');
    await mail.getByTestId('value-user').fill('mailer');
    await mail.getByTestId('value-from').fill('Gobbler <invites@example.com>');
    await mail.getByRole('button', { name: 'Save', exact: true }).first().click();
    await expect(page.getByTestId('integrations-problem')).toContainText('Password is required when a host is set');
  });

  test('saving a password with no encryption key says why and stores nothing', async ({ page }) => {
    await page.goto('/operator/settings');
    const mail = page.getByTestId('integration-mail');
    await mail.getByTestId('secret-input').fill('hunter2');
    await mail.getByTestId('secret-mail-password').getByRole('button', { name: 'Save' }).click();
    await expect(page.getByTestId('integrations-problem')).toContainText('encryption key is missing');
    await expect(page.getByTestId('integration-mail').getByTestId('secret-state')).toHaveText('Not set');
  });

  test('the value and secret routes answer 404 for a name nothing registered', async ({ request }) => {
    const headers = { 'Sec-Fetch-Site': 'same-origin' };
    const secret = await request.post('/operator/settings/secrets/mail/nope', { form: { value: 'x' }, headers, maxRedirects: 0 });
    expect(secret.status()).toBe(404);
    const values = await request.post('/operator/settings/values/nope', { form: { host: 'x' }, headers, maxRedirects: 0 });
    expect(values.status()).toBe(404);
  });
});
