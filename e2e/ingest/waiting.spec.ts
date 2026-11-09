import { test, expect, type Page } from '@playwright/test';
import { seededServer } from '../recipes/support';

// The drafts waiting for review (ADR-00049): listed on /ingest, counted on the
// Add recipe entry of the menu, and worked through one after another. The
// count and the list are the household's, so this spec starts its own gobbler:
// on the shared server other specs' drafts come and go.
const srv = await seededServer('GOBBLER_E2E_WAITING_PORT', 'gobbler-e2e-waiting-');

test.use({ baseURL: srv.origin });
test.describe.configure({ mode: 'serial' });

test.beforeAll(srv.start);
test.afterAll(srv.stop);

const recipe = (name: string) => `${name}

Ingredients:
- 2 cups all-purpose flour
- 2 eggs

Instructions:
Whisk the flour and eggs together.

Cook on a hot griddle.
`;

const badge = (page: Page) => page.getByTestId('waiting-drafts');
const addRecipe = (page: Page) => page.locator('nav.tabs a[href="/ingest"]');

test('with no draft waiting there is no list and no badge', async ({ page }) => {
  await page.goto('/ingest');
  await expect(page.getByRole('heading', { name: 'Add a recipe', level: 1 })).toBeVisible();
  await expect(page.getByTestId('waiting-drafts-list')).toHaveCount(0);
  await expect(badge(page)).toHaveCount(0);
  await expect(addRecipe(page)).toHaveAttribute('aria-label', 'Add recipe');
});

test('waiting drafts are listed and counted, and confirming one opens the next', async ({ page, request }) => {
  for (const name of ['First Pancakes', 'Second Pancakes']) {
    const res = await request.post('/ingest', { form: { text: recipe(name) }, maxRedirects: 0 });
    expect(res.status()).toBe(303);
  }

  await page.goto('/recipes');
  await expect(badge(page)).toHaveText('2');
  await expect(addRecipe(page)).toHaveAttribute('aria-label', 'Add recipe, 2 drafts waiting for review');

  await page.goto('/ingest');
  const rows = page.getByTestId('waiting-draft');
  await expect(rows).toHaveCount(2);
  await expect(rows.nth(0)).toContainText('First Pancakes');
  await expect(rows.nth(0)).toContainText('Pasted text');
  await expect(rows.nth(0)).toContainText('today');
  await expect(rows.nth(1)).toContainText('Second Pancakes');
  // The list sits above the import form.
  const list = (await page.getByTestId('waiting-drafts-list').boundingBox())!;
  const form = (await page.locator('form.ingest-form').boundingBox())!;
  expect(list.y).toBeLessThan(form.y);

  await rows.nth(0).click();
  await expect(page).toHaveURL(/\/ingest\/[0-9a-f]+$/);
  await expect(page.getByLabel('Name', { exact: true })).toHaveValue('First Pancakes');
  await page.getByRole('button', { name: 'Confirm and save' }).first().click();

  await expect(page).toHaveURL(/\/ingest\/[0-9a-f]+\?done=[0-9a-f]+$/);
  await expect(page.getByLabel('Name', { exact: true })).toHaveValue('Second Pancakes');
  const done = page.getByTestId('draft-done');
  await expect(done).toContainText('Saved First Pancakes.');
  await expect(badge(page)).toHaveText('1');

  // Discarding the last one goes back to the import page, which says so.
  await page.getByRole('button', { name: 'Cancel' }).first().click();
  await expect(page).toHaveURL(/\/ingest\?done=[0-9a-f]+$/);
  await expect(done).toContainText('Discarded Second Pancakes.');
  await expect(page.getByTestId('waiting-drafts-list')).toHaveCount(0);
  await expect(badge(page)).toHaveCount(0);

  // The saved recipe is where the message's link went.
  await page.goto('/recipes');
  await expect(page.getByRole('link', { name: 'First Pancakes' }).first()).toBeVisible();
});

test('the message links the saved recipe', async ({ page, request }) => {
  for (const name of ['Third Pancakes', 'Fourth Pancakes']) {
    expect((await request.post('/ingest', { form: { text: recipe(name) }, maxRedirects: 0 })).status()).toBe(303);
  }
  await page.goto('/ingest');
  await page.getByTestId('waiting-draft').first().click();
  await page.getByRole('button', { name: 'Confirm and save' }).first().click();
  await page.getByTestId('draft-done').getByRole('link', { name: 'Third Pancakes' }).click();
  await expect(page).toHaveURL(/\/recipes\/\d+-third-pancakes$/);
  await expect(page.getByRole('heading', { name: 'Third Pancakes' })).toBeVisible();
});
