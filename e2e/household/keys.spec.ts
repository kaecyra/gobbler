import { test, expect, type Page } from '@playwright/test';
import { OTHER } from '../fixtures/identity';
import { expectTapTargets } from '../fixtures/tap';
import { as, linkFor, startHousehold, uniqueEmail } from './support';

// Household API keys on the household page (ADR-00031). Every test makes its own
// household through the operator, so none disturbs the seeded ones.

const SECRET = /^gob_[A-Za-z0-9_-]{43}$/;
const SHOTS = new URL('../../.cc-scratch/shots/', import.meta.url).pathname;

async function makeKey(page: Page, name: string, expires: string): Promise<string> {
  const keys = page.getByTestId('household-keys');
  await keys.getByLabel('Name').fill(name);
  await keys.getByLabel('Expires').selectOption({ label: expires });
  await keys.getByRole('button', { name: 'Make a key' }).click();
  const secret = page.getByTestId('key-secret');
  await expect(secret).toBeVisible();
  const value = await secret.inputValue();
  expect(value).toMatch(SECRET);
  return value;
}

test('a member makes a household key with an expiry, sees its secret once, and revokes it', async ({ browser, baseURL }) => {
  const { ctx } = await startHousehold(browser, baseURL, 'Key makers');
  const page = await ctx.newPage();
  await page.goto('/household');
  await expect(page.getByTestId('api-keys-empty')).toBeVisible();

  const secret = await makeKey(page, 'Kitchen bot', '90 days');
  await page.screenshot({ path: `${SHOTS}keys-household-created.png` });
  await page.getByRole('link', { name: 'Back to the household' }).click();
  expect(await page.content()).not.toContain(secret);

  const row = page.getByTestId('api-key').filter({ hasText: 'Kitchen bot' });
  await expect(row).toContainText(secret.slice(0, 12));
  await expect(row).toContainText('Expires');
  await expect(row).toContainText('Never used');

  await makeKey(page, 'Evergreen', 'No expiry');
  await page.getByRole('link', { name: 'Back to the household' }).click();
  await expect(page.getByTestId('api-key').filter({ hasText: 'Evergreen' })).toContainText('No expiry');

  await page.getByRole('button', { name: 'Revoke the key Kitchen bot' }).click();
  await expect(page.getByTestId('api-key').filter({ hasText: 'Kitchen bot' })).toHaveCount(0);
  await expect(page.getByTestId('api-key').filter({ hasText: 'Evergreen' })).toHaveCount(1);
  await ctx.close();
});

test('another household cannot see or revoke the keys', async ({ browser, baseURL }) => {
  const { ctx } = await startHousehold(browser, baseURL, 'Key keepers');
  const page = await ctx.newPage();
  await page.goto('/household');
  await makeKey(page, 'Keepers only', '30 days');
  await page.getByRole('link', { name: 'Back to the household' }).click();
  const revoke = await page.getByTestId('api-key').locator('form').getAttribute('action');
  expect(revoke).toMatch(/^\/household\/keys\/\d+\/revoke$/);

  const otherCtx = await as(browser, baseURL, OTHER);
  const otherPage = await otherCtx.newPage();
  await otherPage.goto('/household');
  await expect(otherPage.getByText('Keepers only')).toHaveCount(0);
  expect((await otherCtx.request.post(revoke!, { maxRedirects: 0 })).status()).toBe(404);
  await page.reload();
  await expect(page.getByTestId('api-key').filter({ hasText: 'Keepers only' })).toHaveCount(1);
  await otherCtx.close();
  await ctx.close();
});

test('removing a member revokes the household keys they made, and leaving does too', async ({ browser, baseURL }) => {
  const { ctx } = await startHousehold(browser, baseURL, 'Churn');
  const founder = await ctx.newPage();
  await founder.goto('/household');
  await makeKey(founder, 'Founder key', '30 days');

  const joiners: { email: string; page: Page; close: () => Promise<void> }[] = [];
  for (const label of ['removed', 'leaver']) {
    const guest = uniqueEmail(label);
    await founder.goto('/household');
    await founder.getByLabel('Email address').fill(guest);
    await founder.getByRole('button', { name: 'Invite' }).click();
    const link = await linkFor(founder, guest);
    const guestCtx = await as(browser, baseURL, guest);
    const guestPage = await guestCtx.newPage();
    await guestPage.goto(link);
    await guestPage.getByRole('button', { name: /^Join / }).click();
    await expect(guestPage.getByTestId('member')).not.toHaveCount(0);
    await makeKey(guestPage, `${label} key`, 'No expiry');
    joiners.push({ email: guest, page: guestPage, close: () => guestCtx.close() });
  }
  await founder.goto('/household');
  await expect(founder.getByTestId('api-key')).toHaveCount(3);

  const [removed, leaver] = joiners;
  await founder.getByRole('button', { name: `Remove ${removed.email}` }).click();
  await expect(founder.getByTestId('api-key')).toHaveCount(2);
  await expect(founder.getByTestId('api-key').filter({ hasText: 'removed key' })).toHaveCount(0);

  await leaver.page.goto('/household');
  await leaver.page.getByRole('button', { name: 'Leave this household' }).click();
  await founder.reload();
  await expect(founder.getByTestId('api-key')).toHaveCount(1);
  await expect(founder.getByTestId('api-key')).toContainText('Founder key');
  await Promise.all(joiners.map((j) => j.close()));
  await ctx.close();
});

test('the household page has controls at least 44px tall, on a laptop and on a phone', async ({ browser, baseURL }) => {
  const { ctx } = await startHousehold(browser, baseURL, 'Tap sizes');
  const page = await ctx.newPage();
  for (const [label, viewport] of [['1024', { width: 1024, height: 768 }], ['390', { width: 390, height: 844 }]] as const) {
    await page.setViewportSize(viewport);
    await page.goto('/household');
    await makeKey(page, `Tap ${label}`, '1 year');
    await expectTapTargets(page, 'main');
    await page.getByRole('link', { name: 'Back to the household' }).click();
    await expectTapTargets(page, 'main');
    for (const mode of ['light', 'dark']) {
      await page.evaluate((m) => document.documentElement.setAttribute('data-mode', m), mode);
      await page.screenshot({ path: `${SHOTS}keys-household-${label}-${mode}.png`, fullPage: true });
    }
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    expect(overflow, `${viewport.width}px`).toBeLessThanOrEqual(0);
  }
  await ctx.close();
});
