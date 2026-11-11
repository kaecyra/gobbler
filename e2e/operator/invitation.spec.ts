import { test, expect } from '@playwright/test';
import { OPERATOR, OTHER, STRANGER, asIdentity } from '../fixtures/identity';
import { as, issueHouseholdInvitation, linkFor, uniqueEmail } from '../household/support';
import { expectTapTargets } from '../fixtures/tap';

// The operator's household-invitation page (ADR-00018, Invitations). The shared
// dev-auth server lists OPERATOR (a member of no household) and DEV as
// operators. An invitation expiring is covered by the store's tests: a browser
// test would have to wait out the week.

test.describe('the operator', () => {
  test.use(asIdentity(OPERATOR));

  test('issues a household invitation that shows no household data', async ({ page }) => {
    const email = uniqueEmail('founder');
    await page.goto('/operator/invitations');
    await expect(page.getByRole('heading', { name: 'Household invitations' })).toBeVisible();
    await page.getByLabel('Email address').fill(email);
    await page.getByRole('button', { name: 'Invite' }).click();
    await expect(page).toHaveURL(/\/operator\/invitations$/);
    expect(await linkFor(page, email)).toMatch(/^\/invitations\/[0-9A-Za-z]{8}$/);
    await expect(page.locator('body')).not.toContainText('Dev household');
    await expect(page.locator('body')).not.toContainText('Other household');
  });

  test('an invitation made with no mail host saved shows it was not emailed, and Resend says why', async ({ page }) => {
    const email = uniqueEmail('founder');
    await page.goto('/operator/invitations');
    await page.getByLabel('Email address').fill(email);
    await page.getByRole('button', { name: 'Invite' }).click();
    const row = page.getByTestId('pending-invitation').filter({ hasText: email });
    await expect(row.getByTestId('email-status')).toHaveText('Not emailed');
    // The copyable link is still there.
    expect(await linkFor(page, email)).toMatch(/^\/invitations\/[0-9A-Za-z]{8}$/);

    // Resend is offered whatever the status; with mail off it records why.
    await row.getByRole('button', { name: 'Resend the invitation to ' + email }).click();
    await expect(page).toHaveURL(/\/operator\/invitations$/);
    await expect(row.getByTestId('email-status')).toHaveText('Email failed: email is not set up on this deployment');
    await expect(page.getByTestId('pending-invitation').filter({ hasText: email })).toHaveCount(1);
  });

  test('reaches the promotion suggestions from the invitations page', async ({ page }) => {
    await page.goto('/operator/invitations');
    await page.getByRole('navigation', { name: 'Operator' }).getByRole('link', { name: 'Suggested ingredients' }).click();
    await expect(page).toHaveURL(/\/operator\/promotions\/?$/);
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
  });

  test('revokes an invitation, which the invitee can then no longer open', async ({ page, browser, baseURL }) => {
    const email = uniqueEmail('founder');
    await page.goto('/operator/invitations');
    await page.getByLabel('Email address').fill(email);
    await page.getByRole('button', { name: 'Invite' }).click();
    const link = await linkFor(page, email);

    await page.getByTestId('pending-invitation').filter({ hasText: email }).getByRole('button', { name: /Revoke/ }).click();
    await expect(page.getByTestId('pending-invitation').filter({ hasText: email })).toHaveCount(0);

    const invitee = await as(browser, baseURL, email);
    const res = await invitee.request.get(link);
    expect(res.status()).toBe(410);
    expect(await res.text()).toContain('This invitation was withdrawn');
    await invitee.close();
  });
});

test('a household invitation is for one email, once, and starts a household', async ({ browser, baseURL }) => {
  const email = uniqueEmail('founder');
  const link = await issueHouseholdInvitation(browser, baseURL, email);
  const code = link.split('/').pop();

  // Nobody else can open or accept it, whoever they are.
  for (const who of [STRANGER, OTHER, OPERATOR]) {
    const ctx = await as(browser, baseURL, who);
    expect((await ctx.request.get(`/invitations/${code}`)).status(), `${who} opens`).toBe(404);
    expect((await ctx.request.post(`/invitations/${code}/accept`, { form: { name: 'Hijack' } })).status(), `${who} accepts`).toBe(404);
    // A code nobody was issued gets the same answer (ADR-00028).
    expect((await ctx.request.get('/invitations/Zz000000')).status(), `${who} opens an unknown code`).toBe(404);
    await ctx.close();
  }

  const ctx = await as(browser, baseURL, email);
  const page = await ctx.newPage();
  await page.goto(`/invitations/${code}`);
  await expect(page.getByTestId('invitation-email')).toContainText(email);
  await page.getByLabel('Household name').fill('Founders');
  await page.getByRole('button', { name: 'Start the household' }).click();
  await expect(page).toHaveURL(/\/household\/?$/);
  await expect(page.getByTestId('household-name')).toHaveText('Founders');
  await expect(page.getByTestId('member')).toContainText(email);

  // Single use.
  const again = await ctx.request.post(`/invitations/${code}/accept`, { form: { name: 'Twice' } });
  expect(again.status()).toBe(410);
  await ctx.close();
});

test.describe('a member who is not an operator, and a non-member', () => {
  for (const who of [OTHER, STRANGER]) {
    test(`${who} cannot use the operator's invitation routes`, async ({ browser, baseURL }) => {
      const ctx = await as(browser, baseURL, who);
      expect((await ctx.request.get('/operator')).status()).toBe(403);
      expect((await ctx.request.get('/operator/invitations')).status()).toBe(403);
      expect((await ctx.request.post('/operator/invitations', { form: { email: uniqueEmail('x') } })).status()).toBe(403);
      expect((await ctx.request.post('/operator/invitations/aZ09bY18/revoke')).status()).toBe(403);
      expect((await ctx.request.post('/operator/invitations/aZ09bY18/resend')).status()).toBe(403);
      const page = await ctx.newPage();
      await page.goto('/operator/invitations');
      await expect(page.locator('body')).not.toContainText('Pending invitations');
      await expect(page.getByRole('navigation', { name: 'Operator' })).toHaveCount(0);
      await expect(page.getByRole('link', { name: 'Suggested ingredients' })).toHaveCount(0);
      expect(await page.locator('a[href^="/operator/promotions"]').count()).toBe(0);
      await ctx.close();
    });
  }
});

test('the invitation page keeps every control at least 44px tall', async ({ browser, baseURL }) => {
  const ctx = await as(browser, baseURL, OPERATOR);
  const page = await ctx.newPage();
  await page.goto('/operator/invitations');
  await page.getByLabel('Email address').fill(uniqueEmail('founder'));
  await page.getByRole('button', { name: 'Invite' }).click();
  await expect(page.getByTestId('pending-invitation').first()).toBeVisible();
  expect(await expectTapTargets(page)).toBeGreaterThan(3);
  await ctx.close();
});
