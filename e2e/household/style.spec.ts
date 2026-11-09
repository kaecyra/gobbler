import { test, expect } from '@playwright/test';
import { expectTapTargets } from '../fixtures/tap';
import { startHousehold } from './support';

// The house style on the household page (ADR-00048). Every test makes its own
// household through the operator, so none disturbs the seeded ones.

const SHOTS = new URL('../../.cc-scratch/shots/', import.meta.url).pathname;

test('a member saves the house style, which the page shows again', async ({ browser, baseURL }) => {
  const { ctx } = await startHousehold(browser, baseURL, 'Stylists');
  const page = await ctx.newPage();
  await page.goto('/household');
  const section = page.getByTestId('house-style');
  const field = section.getByLabel('Style rules');
  await expect(field).toHaveValue('');

  const style = 'Weights in grams.\nOne step per action.';
  await field.fill(style);
  await section.getByRole('button', { name: 'Save house style' }).click();
  await expect(page).toHaveURL(/\/household\/?$/);
  await expect(page.getByTestId('house-style').getByLabel('Style rules')).toHaveValue(style);
  // Saving it changed nothing else on the page.
  await expect(page.getByTestId('household-name')).toHaveText('Stylists');
  await expect(page.getByTestId('member')).toHaveCount(1);
  await expectTapTargets(page, '[data-testid="house-style"]');
  await page.screenshot({ path: `${SHOTS}household-house-style.png`, fullPage: true });
  await ctx.close();
});

test('a house style over 16 KiB is refused with a message and the old one kept', async ({ browser, baseURL }) => {
  const { ctx } = await startHousehold(browser, baseURL, 'Verbose');
  expect((await ctx.request.post('/household/style', { form: { style: 'Keep this.' }, maxRedirects: 0 })).status()).toBe(303);
  const refused = await ctx.request.post('/household/style', { form: { style: 'a'.repeat(16 * 1024 + 1) } });
  expect(refused.status()).toBe(400);
  expect(await refused.text()).toContain('at most 16 KiB');

  const page = await ctx.newPage();
  await page.goto('/household');
  await expect(page.getByTestId('house-style').getByLabel('Style rules')).toHaveValue('Keep this.');
  await ctx.close();
});
