import { test, expect } from '@playwright/test';
import { OTHER, STRANGER } from '../fixtures/identity';
import { as, linkFor, startHousehold, uniqueEmail } from './support';

// The members page and the invitations into a household (ADR-00018). Every test
// makes its own household through the operator, so none disturbs the seeded
// ones other specs rely on.

const NOT_A_MEMBER = 'You are not a member of a household';

test('a member invites someone, who joins and sees the household', async ({ browser, baseURL }) => {
  const { ctx } = await startHousehold(browser, baseURL, 'Joiners');
  const guest = uniqueEmail('guest');
  const page = await ctx.newPage();
  await page.goto('/household');
  await page.getByLabel('Email address').fill(guest);
  await page.getByRole('button', { name: 'Invite' }).click();
  const link = await linkFor(page, guest);

  const guestCtx = await as(browser, baseURL, guest);
  const guestPage = await guestCtx.newPage();
  // Before joining, the guest is a non-member who can reach only the invitation.
  expect((await guestCtx.request.get('/recipes')).status()).toBe(403);
  await guestPage.goto(link);
  await expect(guestPage.getByTestId('invitation-join')).toContainText('Joiners');
  await guestPage.getByRole('button', { name: 'Join Joiners' }).click();
  await expect(guestPage).toHaveURL(/\/household\/?$/);
  await expect(guestPage.getByTestId('household-name')).toHaveText('Joiners');
  await expect(guestPage.getByTestId('member')).toHaveCount(2);

  // The invitation is spent and no longer pending.
  expect((await guestCtx.request.post(`${link}/accept`)).status()).toBe(410);
  await page.reload();
  await expect(page.getByTestId('pending-invitation')).toHaveCount(0);
  await expect(page.getByTestId('member')).toHaveCount(2);
  await ctx.close();
  await guestCtx.close();
});

test('a member revokes an invitation', async ({ browser, baseURL }) => {
  const { ctx } = await startHousehold(browser, baseURL, 'Revokers');
  const guest = uniqueEmail('guest');
  const page = await ctx.newPage();
  await page.goto('/household');
  await page.getByLabel('Email address').fill(guest);
  await page.getByRole('button', { name: 'Invite' }).click();
  const link = await linkFor(page, guest);

  await page.getByTestId('pending-invitation').filter({ hasText: guest }).getByRole('button', { name: /Revoke/ }).click();
  await expect(page.getByTestId('pending-invitation')).toHaveCount(0);
  const guestCtx = await as(browser, baseURL, guest);
  expect((await guestCtx.request.post(`${link}/accept`)).status()).toBe(410);
  await guestCtx.close();
  await ctx.close();
});

test('an invitation can be accepted only by the addressed email', async ({ browser, baseURL }) => {
  const { ctx } = await startHousehold(browser, baseURL, 'Addressed');
  const guest = uniqueEmail('guest');
  const page = await ctx.newPage();
  await page.goto('/household');
  await page.getByLabel('Email address').fill(guest);
  await page.getByRole('button', { name: 'Invite' }).click();
  const link = await linkFor(page, guest);

  for (const who of [STRANGER, OTHER]) {
    const other = await as(browser, baseURL, who);
    expect((await other.request.get(link)).status(), `${who} opens`).toBe(404);
    expect((await other.request.post(`${link}/accept`)).status(), `${who} accepts`).toBe(404);
    await other.close();
  }
  await ctx.close();
});

test('someone who already has a household is told to leave it first', async ({ browser, baseURL }) => {
  const a = await startHousehold(browser, baseURL, 'House A');
  const b = await startHousehold(browser, baseURL, 'House B');
  const page = await b.ctx.newPage();
  await page.goto('/household');
  await page.getByLabel('Email address').fill(a.email);
  await page.getByRole('button', { name: 'Invite' }).click();
  const link = await linkFor(page, a.email);

  const aPage = await a.ctx.newPage();
  await aPage.goto(link);
  await expect(aPage.getByTestId('invitation-member')).toContainText('Leave it first');
  await expect(aPage.getByRole('button', { name: /Join/ })).toHaveCount(0);
  const res = await a.ctx.request.post(`${link}/accept`);
  expect(res.status()).toBe(409);
  expect(await res.text()).toContain('leave the current household first');
  await a.ctx.close();
  await b.ctx.close();
});

test('a member removes another, and a member leaves; the last cannot', async ({ browser, baseURL }) => {
  const { email, ctx } = await startHousehold(browser, baseURL, 'Leavers');
  const page = await ctx.newPage();

  async function join(guest: string) {
    await page.goto('/household');
    await page.getByLabel('Email address').fill(guest);
    await page.getByRole('button', { name: 'Invite' }).click();
    const link = await linkFor(page, guest);
    const guestCtx = await as(browser, baseURL, guest);
    const guestPage = await guestCtx.newPage();
    await guestPage.goto(link);
    await guestPage.getByRole('button', { name: 'Join Leavers' }).click();
    await expect(guestPage.getByTestId('household-name')).toHaveText('Leavers');
    return { guestCtx, guestPage };
  }

  // Removal.
  const removed = uniqueEmail('removed');
  const first = await join(removed);
  await page.goto('/household');
  await page.getByTestId('member').filter({ hasText: removed }).getByRole('button', { name: /Remove/ }).click();
  await expect(page.getByTestId('member')).toHaveCount(1);
  const gone = await first.guestPage.goto('/household');
  expect(gone?.status()).toBe(403);
  await expect(first.guestPage.getByRole('heading', { name: NOT_A_MEMBER })).toBeVisible();
  await first.guestCtx.close();

  // Leaving.
  const leaver = uniqueEmail('leaver');
  const second = await join(leaver);
  await second.guestPage.getByRole('button', { name: 'Leave this household' }).click();
  await second.guestPage.goto('/household');
  await expect(second.guestPage.getByRole('heading', { name: NOT_A_MEMBER })).toBeVisible();
  await second.guestCtx.close();

  // The last member cannot leave, and is told what to do instead.
  await page.goto('/household');
  await expect(page.getByTestId('leave')).toContainText('Delete the household instead');
  await expect(page.getByRole('button', { name: 'Leave this household' })).toHaveCount(0);
  const refused = await ctx.request.post('/household/leave');
  expect(refused.status()).toBe(409);
  expect(await refused.text()).toContain('delete the household');
  await expect(page.getByTestId('member')).toContainText(email);
  await ctx.close();
});

test('only the last member deletes the household, after typing its name', async ({ browser, baseURL }) => {
  const { ctx } = await startHousehold(browser, baseURL, 'Doomed');
  const guest = uniqueEmail('guest');
  const page = await ctx.newPage();
  await page.goto('/household');
  await page.getByLabel('Email address').fill(guest);
  await page.getByRole('button', { name: 'Invite' }).click();
  const link = await linkFor(page, guest);
  const guestCtx = await as(browser, baseURL, guest);
  await guestCtx.request.post(`${link}/accept`);

  // With two members there is no delete form, and the route refuses.
  await page.goto('/household');
  await expect(page.getByTestId('delete')).toHaveCount(0);
  expect((await ctx.request.post('/household/delete', { form: { confirm: 'Doomed' } })).status()).toBe(409);

  await page.getByTestId('member').filter({ hasText: guest }).getByRole('button', { name: /Remove/ }).click();
  await expect(page.getByTestId('delete')).toBeVisible();

  const wrong = await ctx.request.post('/household/delete', { form: { confirm: 'doomed' } });
  expect(wrong.status()).toBe(400);
  await page.goto('/household');
  await expect(page.getByTestId('household-name')).toHaveText('Doomed');

  await page.getByLabel('Household name').fill('Doomed');
  await page.getByRole('button', { name: /Delete the household/ }).click();
  const after = await page.goto('/household');
  expect(after?.status()).toBe(403);
  await expect(page.getByRole('heading', { name: NOT_A_MEMBER })).toBeVisible();
  await ctx.close();
  await guestCtx.close();
});

test.describe('the members routes, as a non-member and as a member of another household', () => {
  test('a non-member gets the not-a-member page and changes nothing', async ({ browser, baseURL }) => {
    const ctx = await as(browser, baseURL, STRANGER);
    expect((await ctx.request.get('/household')).status()).toBe(403);
    for (const [path, form] of [
      ['/household/invitations', { email: uniqueEmail('x') }],
      ['/household/invitations/1/revoke', {}],
      ['/household/members/remove', { email: OTHER }],
      ['/household/leave', {}],
      ['/household/delete', { confirm: 'Other household' }],
    ] as const) {
      const res = await ctx.request.post(path, { form });
      expect(res.status(), path).toBe(403);
      expect(await res.text(), path).toContain(NOT_A_MEMBER);
    }
    await ctx.close();
  });

  test('a member of another household can neither see nor change this one', async ({ browser, baseURL }) => {
    const mine = await startHousehold(browser, baseURL, 'Mine');
    const theirs = await startHousehold(browser, baseURL, 'Theirs');
    const guest = uniqueEmail('guest');
    const myPage = await mine.ctx.newPage();
    await myPage.goto('/household');
    await myPage.getByLabel('Email address').fill(guest);
    await myPage.getByRole('button', { name: 'Invite' }).click();
    const link = await linkFor(myPage, guest);
    const id = link.split('/').pop();

    const theirPage = await theirs.ctx.newPage();
    await theirPage.goto('/household');
    await expect(theirPage.locator('body')).not.toContainText(mine.email);
    await expect(theirPage.locator('body')).not.toContainText(guest);
    expect((await theirs.ctx.request.get(link)).status()).toBe(404);
    expect((await theirs.ctx.request.post(`/household/invitations/${id}/revoke`)).status()).toBe(404);
    expect((await theirs.ctx.request.post('/household/members/remove', { form: { email: mine.email } })).status()).toBe(404);

    // Nothing of that changed in the household they tried it on.
    await myPage.reload();
    await expect(myPage.getByTestId('member')).toContainText(mine.email);
    await linkFor(myPage, guest);
    await mine.ctx.close();
    await theirs.ctx.close();
  });
});
