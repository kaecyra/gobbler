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
  const hidden = page.locator('.editor input[type=submit][hidden]');
  await expect(hidden).toHaveCount(1);
  await expect(hidden).toBeHidden();
  expect(await hidden.boundingBox()).toBeNull();
});

test('without JavaScript, the review bar confirms a draft', async ({ browser, baseURL }) => {
  const ctx = await browser.newContext({ javaScriptEnabled: false, baseURL });
  const page = await ctx.newPage();
  await toReview(page);
  await page.locator('.savebar').getByRole('button', { name: 'Confirm and save' }).click();
  await expect(page).toHaveURL(/\/recipes\/[0-9a-z-]+$/);
  await expect(page.getByRole('heading', { name: 'Bar Pancakes' })).toBeVisible();
  await ctx.close();
});

// A refused confirm is a 200 with the message on the failing row. The page says
// so at the top, scrolls to the first error, and the bar counts the error apart
// from the items that only need a look.
test('a refused confirm shows the not-saved callout and the first error in view, and the bar counts it', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 720 });
  await toReview(page);
  await page.getByRole('button', { name: 'Add note' }).click();
  await page.locator('[name^="n."][name$=".text"]').last().fill('One paragraph.\n\nAnother paragraph.');
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.locator('.savebar').getByRole('button', { name: 'Confirm and save' }).click();
  const callout = page.getByTestId('not-saved');
  await expect(callout).toContainText('Not saved. 1 field needs fixing.');
  await expect(callout).toBeInViewport();
  const error = page.getByRole('alert').filter({ hasText: 'one paragraph' });
  await expect(error).toBeInViewport();
  await expect(error).toBeFocused();
  await expect(page.getByTestId('bar-errors')).toContainText('1 field stops this being saved');
  await expect(page.getByRole('heading', { name: 'Review recipe' })).toBeVisible();
});
