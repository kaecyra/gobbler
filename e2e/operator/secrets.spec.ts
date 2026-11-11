import { test, expect } from '@playwright/test';
import { OPERATOR, asIdentity } from '../fixtures/identity';

// The Mail section of the Deployment page (ADR-00055, ADR-00057). The shared
// server has no SMTP host and no encryption key, so what it can show is the
// fields, the refusals, and that nothing is saved by a refused form. Saving a
// password with the rest, keeping it on a blank submit, clearing it and the
// unreadable state are covered over a real store in internal/web/settings, and
// clearing in the browser by e2e/operator/llm.spec.ts; sending is covered
// against a fake SMTP server in internal/mail and internal/api/households.
test.describe('mail settings', () => {
  test.use(asIdentity(OPERATOR));

  test('the Deployment page lists the mail fields with the password among them', async ({ page }) => {
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
    await expect(mail.getByRole('button', { name: /Clear/ })).toHaveCount(0);
    // One form and one Save; the password is a field of it, right after User.
    await expect(mail.locator('form')).toHaveCount(1);
    await expect(mail.getByRole('button', { name: 'Save' })).toHaveCount(1);
    const order = await mail.locator('form [data-testid]').evaluateAll((els) =>
      els.map((e) => e.getAttribute('data-testid')).filter((id) => id === 'value-user' || id === 'secret-mail-password' || id === 'value-from'),
    );
    expect(order).toEqual(['value-user', 'secret-mail-password', 'value-from']);
    await expect(mail.getByLabel('Password')).toHaveAttribute('type', 'password');
  });

  test('a host without the rest is refused and says which field is missing', async ({ page }) => {
    await page.goto('/operator/settings');
    const mail = page.getByTestId('integration-mail');
    await mail.getByTestId('value-host').fill('smtp.example.com');
    await mail.getByTestId('value-port').fill('587');
    await mail.getByTestId('value-from').fill('Gobbler <invites@example.com>');
    await mail.getByTestId('secret-input').fill('typed-password');
    await mail.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(page.getByTestId('integrations-problem')).toHaveText('User is required when a host is set.');
    // A typed password is not sent back.
    await expect(page.getByTestId('integration-mail').getByTestId('secret-input')).toHaveValue('');
    expect(await page.content()).not.toContain('typed-password');
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
    await mail.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(page.getByTestId('integrations-problem')).toHaveText('Password is required when a host is set.');
  });

  test('saving a password with no encryption key says why and stores nothing', async ({ page }) => {
    await page.goto('/operator/settings');
    const mail = page.getByTestId('integration-mail');
    await mail.getByTestId('secret-input').fill('hunter2');
    await mail.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(page.getByTestId('integrations-problem')).toContainText('Password was not saved: the encryption key is missing');
    await expect(page.getByTestId('integration-mail').getByTestId('secret-state')).toHaveText('Not set');
  });

  test('the value and clear routes answer 404 for a name nothing registered', async ({ request }) => {
    const headers = { 'Sec-Fetch-Site': 'same-origin' };
    const clear = await request.post('/operator/settings/secrets/mail/nope/clear', { headers, maxRedirects: 0 });
    expect(clear.status()).toBe(404);
    const values = await request.post('/operator/settings/values/nope', { form: { host: 'x' }, headers, maxRedirects: 0 });
    expect(values.status()).toBe(404);
  });
});
