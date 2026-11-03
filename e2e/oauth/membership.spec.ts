import { test, expect, type APIRequestContext } from '@playwright/test';
import { as, linkFor, startHousehold, uniqueEmail } from '../household/support';
import { connectApp } from './support';

// Removing a member, or a member leaving, revokes their OAuth grants in the same
// change that revokes their keys (ADR-00034, ADR-00031), through the router's
// own wiring of the leave hook. Each test makes its own household, so none
// disturbs the seeded ones.

async function refresh(request: APIRequestContext, clientId: string, token: string, resource: string) {
  return request.post('/oauth/token', {
    form: { grant_type: 'refresh_token', client_id: clientId, refresh_token: token, resource },
    failOnStatusCode: false,
  });
}

test('removing a member, and a member leaving, revoke their grants and only theirs', async ({ browser, baseURL }) => {
  const { ctx } = await startHousehold(browser, baseURL, 'Grant churn');
  const founder = await ctx.newPage();
  const founderApp = await connectApp(ctx.request, 'Founder agent');

  const joiners: { email: string; page: import('@playwright/test').Page; app: Awaited<ReturnType<typeof connectApp>>; close: () => Promise<void> }[] = [];
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
    const app = await connectApp(guestCtx.request, `${label} agent`);
    joiners.push({ email: guest, page: guestPage, app, close: () => guestCtx.close() });
  }
  const [removed, leaver] = joiners;

  await founder.goto('/household');
  await founder.getByRole('button', { name: `Remove ${removed.email}` }).click();
  await expect(founder.getByTestId('member')).toHaveCount(2);
  const afterRemoval = await refresh(ctx.request, removed.app.clientId, removed.app.tokens.refresh_token, removed.app.resource);
  expect(afterRemoval.status()).toBe(400);
  expect((await afterRemoval.json()).error).toBe('invalid_grant');

  await leaver.page.goto('/household');
  await leaver.page.getByRole('button', { name: 'Leave this household' }).click();
  const afterLeaving = await refresh(ctx.request, leaver.app.clientId, leaver.app.tokens.refresh_token, leaver.app.resource);
  expect(afterLeaving.status()).toBe(400);

  // The founder's own grant is untouched.
  const stillGood = await refresh(ctx.request, founderApp.clientId, founderApp.tokens.refresh_token, founderApp.resource);
  expect(stillGood.status()).toBe(200);
  await Promise.all(joiners.map((j) => j.close()));
  await ctx.close();
});
