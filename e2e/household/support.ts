import { expect, type Browser, type BrowserContext, type Page } from '@playwright/test';
import { OPERATOR, asIdentity } from '../fixtures/identity';

// Helpers for the specs that exercise invitations. The shared e2e server keeps
// its data for the whole run and specs run in parallel, so every test makes its
// own emails and households and never touches the seeded ones' membership.

let counter = 0;

/** An email no other test uses. */
export function uniqueEmail(label: string): string {
  counter += 1;
  return `${label}-${process.pid}-${Date.now()}-${counter}@e2e.example`;
}

/** A browser context whose every request is made by email. */
export async function as(browser: Browser, baseURL: string | undefined, email: string): Promise<BrowserContext> {
  return browser.newContext({ baseURL, ...asIdentity(email) });
}

/**
 * The operator invites email to start a household and returns the invitation's
 * path, read off the operator's page.
 */
export async function issueHouseholdInvitation(browser: Browser, baseURL: string | undefined, email: string): Promise<string> {
  const ctx = await as(browser, baseURL, OPERATOR);
  try {
    const page = await ctx.newPage();
    await page.goto('/operator');
    await page.getByLabel('Email address').fill(email);
    await page.getByRole('button', { name: 'Invite' }).click();
    return await linkFor(page, email);
  } finally {
    await ctx.close();
  }
}

/** The path of the pending invitation listed for email on the current page. */
export async function linkFor(page: Page, email: string): Promise<string> {
  const row = page.getByTestId('pending-invitation').filter({ hasText: email });
  await expect(row).toHaveCount(1);
  const href = await row.getByRole('link').getAttribute('href');
  expect(href).toMatch(/^\/invitations\/\d+$/);
  return href!;
}

/** Creates a household named name whose only member is a new email. */
export async function startHousehold(browser: Browser, baseURL: string | undefined, name: string): Promise<{ email: string; ctx: BrowserContext }> {
  const email = uniqueEmail('founder');
  const link = await issueHouseholdInvitation(browser, baseURL, email);
  const ctx = await as(browser, baseURL, email);
  const page = await ctx.newPage();
  await page.goto(link);
  await page.getByLabel('Household name').fill(name);
  await page.getByRole('button', { name: 'Start the household' }).click();
  await expect(page.getByRole('heading', { name })).toBeVisible();
  return { email, ctx };
}
