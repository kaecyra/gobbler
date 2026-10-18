import { test, expect } from '@playwright/test';
import { pasteText } from '../ingest/helpers';

// The save bar on the ingest review screen (ADR-00020: the review screen is the
// recipe editor on a draft). Runs on the suite's shared server, which has the
// ingest routes; pasted text needs no fetch and no provider.
const TEXT = `Bar Pancakes

Ingredients:
- 2 cups all-purpose flour
- 2 eggs

Instructions:
Whisk it all together.

Cook on a hot greased griddle.
`;

async function toReview(page: import('@playwright/test').Page) {
  await page.goto('/ingest');
  await pasteText(page, TEXT);
  await page.getByRole('button', { name: 'Read recipe' }).click();
  await expect(page.getByRole('heading', { name: 'Review recipe' })).toBeVisible();
}

test('the review bar shows Confirm and save and Cancel, and the Enter-key button stays hidden', async ({ page }) => {
  await toReview(page);
  const bar = page.locator('.savebar');
  await expect(bar.getByRole('button', { name: 'Confirm and save' })).toBeVisible();
  await expect(bar.getByRole('button', { name: 'Cancel' })).toBeVisible();
  const hidden = page.locator('.editor button[hidden]');
  await expect(hidden).toHaveCount(1);
  await expect(hidden).toBeHidden();
  expect(await hidden.boundingBox()).toBeNull();
});

test('without JavaScript, the review bar confirms a draft', async ({ browser, baseURL }) => {
  const ctx = await browser.newContext({ javaScriptEnabled: false, baseURL });
  const page = await ctx.newPage();
  await toReview(page);
  await page.locator('.savebar').getByRole('button', { name: 'Confirm and save' }).click();
  await expect(page).toHaveURL(/\/recipes\/[0-9a-f-]+$/);
  await expect(page.getByRole('heading', { name: 'Bar Pancakes' })).toBeVisible();
  await ctx.close();
});
