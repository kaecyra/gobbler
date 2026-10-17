import { test, expect } from '@playwright/test';
import { OPERATOR, OTHER, STRANGER, asIdentity } from '../fixtures/identity';
import { as, expectTapTargets, issueHouseholdInvitation, linkFor, uniqueEmail } from '../household/support';

// The operator's household-invitation page (ADR-00018, Invitations). The shared
// dev-auth server lists OPERATOR (a member of no household) and DEV as
// operators. An invitation expiring is covered by the store's tests: a browser
// test would have to wait out the week.

test.describe('the operator', () => {
  test.use(asIdentity(OPERATOR));

  test('issues a household invitation that shows no household data', async ({ page }) => {
    const email = uniqueEmail('founder');
    await page.goto('/operator');
    await expect(page.getByRole('heading', { name: 'Household invitations' })).toBeVisible();
    await page.getByLabel('Email address').fill(email);
    await page.getByRole('button', { name: 'Invite' }).click();
    await expect(page).toHaveURL(/\/operator\/?$/);
    expect(await linkFor(page, email)).toMatch(/^\/invitations\/\d+$/);
    await expect(page.locator('body')).not.toContainText('Dev household');
    await expect(page.locator('body')).not.toContainText('Other household');
  });

  test('reaches the promotion suggestions from the operator page', async ({ page }) => {
    await page.goto('/operator');
    await page.getByRole('link', { name: 'Suggested ingredients' }).click();
    await expect(page).toHaveURL(/\/operator\/promotions\/?$/);
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
  });

  test('revokes an invitation, which the invitee can then no longer open', async ({ page, browser, baseURL }) => {
    const email = uniqueEmail('founder');
    await page.goto('/operator');
    await page.getByLabel('Email address').fill(email);
    await page.getByRole('button', { name: 'Invite' }).click();
    const link = await linkFor(page, email);

    await page.getByTestId('pending-invitation').filter({ hasText: email }).getByRole('button', { name: /Revoke/ }).click();
    await expect(page.getByTestId('pending-invitation').filter({ hasText: email })).toHaveCount(0);

    const invitee = await as(browser, baseURL, email);
    const res = await invitee.request.get(link);
    expect(res.status()).toBe(410);
    expect(await res.text()).toContain('revoked');
    await invitee.close();
  });
});

test('a household invitation is for one email, once, and starts a household', async ({ browser, baseURL }) => {
  const email = uniqueEmail('founder');
  const link = await issueHouseholdInvitation(browser, baseURL, email);
  const id = link.split('/').pop();

  // Nobody else can open or accept it, whoever they are.
  for (const who of [STRANGER, OTHER, OPERATOR]) {
    const ctx = await as(browser, baseURL, who);
    expect((await ctx.request.get(`/invitations/${id}`)).status(), `${who} opens`).toBe(404);
    expect((await ctx.request.post(`/invitations/${id}/accept`, { form: { name: 'Hijack' } })).status(), `${who} accepts`).toBe(404);
    await ctx.close();
  }

  const ctx = await as(browser, baseURL, email);
  const page = await ctx.newPage();
  await page.goto(`/invitations/${id}`);
  await expect(page.getByTestId('invitation-email')).toContainText(email);
  await page.getByLabel('Household name').fill('Founders');
  await page.getByRole('button', { name: 'Start the household' }).click();
  await expect(page).toHaveURL(/\/household\/?$/);
  await expect(page.getByTestId('household-name')).toHaveText('Founders');
  await expect(page.getByTestId('member')).toContainText(email);

  // Single use.
  const again = await ctx.request.post(`/invitations/${id}/accept`, { form: { name: 'Twice' } });
  expect(again.status()).toBe(410);
  await ctx.close();
});

test.describe('a member who is not an operator, and a non-member', () => {
  for (const who of [OTHER, STRANGER]) {
    test(`${who} cannot use the operator's invitation routes`, async ({ browser, baseURL }) => {
      const ctx = await as(browser, baseURL, who);
      expect((await ctx.request.get('/operator')).status()).toBe(403);
      expect((await ctx.request.post('/operator/invitations', { form: { email: uniqueEmail('x') } })).status()).toBe(403);
      expect((await ctx.request.post('/operator/invitations/1/revoke')).status()).toBe(403);
      const page = await ctx.newPage();
      await page.goto('/operator');
      await expect(page.locator('body')).not.toContainText('Pending invitations');
      await expect(page.getByRole('link', { name: 'Suggested ingredients' })).toHaveCount(0);
      expect(await page.locator('a[href^="/operator/promotions"]').count()).toBe(0);
      await ctx.close();
    });
  }
});

test('the invitation page keeps every control at least 44px tall', async ({ browser, baseURL }) => {
  const ctx = await as(browser, baseURL, OPERATOR);
  const page = await ctx.newPage();
  await page.goto('/operator');
  await page.getByLabel('Email address').fill(uniqueEmail('founder'));
  await page.getByRole('button', { name: 'Invite' }).click();
  await expect(page.getByTestId('pending-invitation').first()).toBeVisible();
  expect(await expectTapTargets(page)).toBeGreaterThan(3);
  await ctx.close();
});
