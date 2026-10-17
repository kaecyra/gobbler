import { test, expect } from '@playwright/test';
import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';

// The ingest form and the review screen (ADR-00006): nothing is saved until
// the user confirms. The URL path is exercised against a local fixture HTTP
// server, never the internet.

const PANCAKES = `Fluffy Pancakes

Ingredients:
- 2 cups all-purpose flour
- 2 tbsp sugar
- 2 eggs

Instructions:
Whisk the dry ingredients together in a large bowl.

Cook on a hot greased griddle until golden on both sides.
`;

// Three plain steps, so the rules' default graph is a chain.
const THREE_STEPS = `Plain Pancakes

Ingredients:
- 2 cups all-purpose flour
- 2 eggs

Instructions:
Whisk the flour and eggs together in a large bowl.

Pour the batter into a pan.

Flip the pancake and serve.
`;

// One ingredient the seeded catalog does not have.
const UNKNOWN = `Zesty Water

Ingredients:
- 2 cups water
- 1 tsp dragonfruit zest

Instructions:
Stir the zest into the water.
`;

// Saving a draft creates its unknown ingredients in the shared catalog, so
// each test that needs one owns its own: sharing UNKNOWN made the mapping test
// depend on running before the test that creates dragonfruit zest.
const UNKNOWN_FOR_MAPPING = `Starry Water

Ingredients:
- 2 cups water
- 1 tsp starfruit zest

Instructions:
Stir the zest into the water.
`;

// One readable line and one the rules cannot make sense of.
const MESSY = `Mystery Stew

Ingredients:
- 2 cups all-purpose flour
- ?? a good amount of glorp, roughly #@! to taste-ish

Instructions:
Stir it all together and simmer until it looks right.
`;

const RECIPE_PAGE = `<html><head><script type="application/ld+json">
{"@context":"https://schema.org","@type":"Recipe","name":"Fixture Toast",
 "recipeIngredient":["2 slices bread","1 tbsp butter"],
 "recipeInstructions":[{"@type":"HowToStep","text":"Toast the bread."},{"@type":"HowToStep","text":"Butter it."}]}
</script></head><body><h1>Fixture Toast</h1></body></html>`;

// The same page with a schema.org video, on YouTube and on another host
// (ADR-00021 decision 5).
const pageWithVideo = (embedUrl: string) =>
  RECIPE_PAGE.replace('"name":"Fixture Toast",', `"name":"Fixture Toast","video":{"@type":"VideoObject","name":"How","embedUrl":"${embedUrl}"},`);
const YOUTUBE_ID = 'dQw4w9WgXcQ';

let fixture: Server;
let fixtureURL: string;

test.beforeAll(async () => {
  fixture = createServer((req, res) => {
    if (req.url === '/toast') {
      res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' });
      res.end(RECIPE_PAGE);
    } else if (req.url === '/toast-youtube' || req.url === '/toast-vimeo') {
      res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' });
      res.end(pageWithVideo(req.url === '/toast-youtube' ? `https://www.youtube.com/embed/${YOUTUBE_ID}` : 'https://player.vimeo.com/video/1'));
    } else {
      res.writeHead(404, { 'content-type': 'text/plain' });
      res.end('no such page');
    }
  });
  await new Promise<void>((resolve) => fixture.listen(0, '127.0.0.1', resolve));
  fixtureURL = `http://127.0.0.1:${(fixture.address() as AddressInfo).port}`;
});

test.afterAll(async () => {
  await new Promise<void>((resolve) => fixture.close(() => resolve()));
});

test('the ingest form renders', async ({ page }) => {
  const res = await page.goto('/ingest');
  expect(res?.status()).toBe(200);
  await expect(page).toHaveTitle(/Add a recipe/);
  await expect(page.getByLabel('Recipe URL')).toBeVisible();
  await expect(page.getByLabel('Or paste the recipe text')).toBeVisible();
});

test('empty and malformed input is refused with a message', async ({ page }) => {
  await page.goto('/ingest');
  await page.getByRole('button', { name: 'Read recipe' }).click();
  await expect(page.getByRole('alert')).toContainText('Enter a recipe URL');

  await page.getByLabel('Or paste the recipe text').fill('some text');
  await page.getByLabel('Recipe URL').fill('https://example.test/x');
  await page.getByRole('button', { name: 'Read recipe' }).click();
  await expect(page.getByRole('alert')).toContainText('not both');
});

test('a malformed URL that gets past the browser is refused by the server', async ({ request }) => {
  const res = await request.post('/ingest', { form: { url: 'nonsense' } });
  expect(res.status()).toBe(400);
  expect(await res.text()).toContain('http:// or https://');
});

test('pasted text goes to review, then confirm saves and redirects to the recipe', async ({ page }) => {
  await page.goto('/ingest');
  await page.getByLabel('Or paste the recipe text').fill(PANCAKES);
  await page.getByRole('button', { name: 'Read recipe' }).click();

  await expect(page).toHaveURL(/\/ingest\/[0-9a-f]+$/);
  await expect(page.getByRole('heading', { name: 'Review recipe' })).toBeVisible();
  await expect(page.getByLabel('Name', { exact: true })).toHaveValue('Fluffy Pancakes');
  await expect(page.getByLabel('Original text').first()).toHaveValue('2 cups all-purpose flour');

  await page.getByLabel('Name', { exact: true }).fill('Best Pancakes');
  await page.getByRole('button', { name: 'Confirm and save' }).first().click();
  // The recipe view is a later node; the redirect target is its URL.
  await expect(page).toHaveURL(/\/recipes\/\d+$/);
});

test('labelled times, yield and calories in pasted text show on review, not as notes', async ({ page }) => {
  await page.goto('/ingest');
  await page.getByLabel('Or paste the recipe text').fill(`Fluffy Pancakes
Prep time: 20 min
Cook time: 1 hr 10 min
Makes 12 pancakes

Ingredients:
- 2 cups all-purpose flour

Instructions:
Cook on a hot greased griddle until golden on both sides.
`);
  await page.getByRole('button', { name: 'Read recipe' }).click();

  await expect(page.getByLabel('Prep time')).toHaveValue('20 min');
  await expect(page.getByLabel('Cook time')).toHaveValue('70 min');
  await expect(page.getByLabel('Yield amount')).toHaveValue('12');
  await expect(page.getByLabel('Yield unit (for example loaf)')).toHaveValue('pancakes');
  // Read as fields, so the label text is not left in a step or the description.
  await expect(page.locator('textarea').filter({ hasText: 'Prep time' })).toHaveCount(0);
});

test('a low-confidence line is highlighted in words, a confident one is not', async ({ page }) => {
  await page.goto('/ingest');
  await page.getByLabel('Or paste the recipe text').fill(MESSY);
  await page.getByRole('button', { name: 'Read recipe' }).click();

  const flagged = page.locator('li.line[data-low]');
  await expect(flagged).toHaveCount(1);
  await expect(flagged.getByLabel('Original text')).toHaveValue(/glorp/);
  await expect(flagged).toContainText('Check:');
  const confident = page.locator('li.line:not([data-low])');
  await expect(confident.getByLabel('Original text')).toHaveValue(/all-purpose flour/);
  await expect(confident).not.toContainText('Check:');
});

test('a proposed new ingredient is offered as new, and is created only when the draft is confirmed', async ({ page }) => {
  await page.goto('/ingest');
  await page.getByLabel('Or paste the recipe text').fill(UNKNOWN);
  await page.getByRole('button', { name: 'Read recipe' }).click();

  // The editor's own ingredient field and its add-as-new choice; the matched
  // line offers no such choice.
  const unknown = page.locator('li.line').nth(1);
  await expect(unknown.getByLabel('Ingredient', { exact: true })).toHaveValue(/dragonfruit zest/);
  const asNew = unknown.getByLabel('Add this ingredient to the catalog as new and unreviewed');
  await expect(asNew).toBeChecked();
  await expect(page.locator('li.line').nth(0).getByLabel('Add this ingredient to the catalog as new and unreviewed')).toHaveCount(0);

  // Naming an ingredient the catalog lacks, without adding it, is refused and
  // the draft stays.
  await unknown.getByLabel('Ingredient', { exact: true }).fill('no such ingredient');
  await asNew.uncheck();
  await page.getByRole('button', { name: 'Confirm and save' }).first().click();
  await expect(page.getByRole('alert')).toContainText('No ingredient in the catalog is called');
  await expect(page.getByRole('heading', { name: 'Review recipe' })).toBeVisible();

  // Accepting it as new saves.
  await page.locator('li.line').nth(1).getByLabel('Ingredient', { exact: true }).fill('dragonfruit zest');
  await page.locator('li.line').nth(1).getByLabel('Add this ingredient to the catalog as new and unreviewed').check();
  await page.getByRole('button', { name: 'Confirm and save' }).first().click();
  await expect(page).toHaveURL(/\/recipes\/\d+$/);
});

test('a proposed line can be mapped to an existing ingredient', async ({ page }) => {
  await page.goto('/ingest');
  await page.getByLabel('Or paste the recipe text').fill(UNKNOWN_FOR_MAPPING);
  await page.getByRole('button', { name: 'Read recipe' }).click();
  await page.locator('li.line').nth(1).getByLabel('Ingredient', { exact: true }).fill('sugar');
  await page.getByRole('button', { name: 'Confirm and save' }).first().click();
  await expect(page).toHaveURL(/\/recipes\/\d+$/);
});

test('an amount, a unit and a whole line can be corrected on review before anything is saved', async ({ page }) => {
  await page.goto('/ingest');
  await page.getByLabel('Or paste the recipe text').fill(PANCAKES);
  await page.getByRole('button', { name: 'Read recipe' }).click();

  const first = page.locator('li.line').nth(0);
  await expect(first.getByLabel('Quantity')).toHaveValue('2');
  await first.getByLabel('Quantity').fill('3');
  await first.getByLabel('Unit', { exact: true }).fill('tbsp');

  // A structural button keeps what was typed and saves nothing.
  await page.getByRole('button', { name: 'Remove line' }).last().click();
  await expect(page.locator('li.line')).toHaveCount(2);
  await expect(page.locator('li.line').nth(0).getByLabel('Quantity')).toHaveValue('3');
  await expect(page.getByRole('heading', { name: 'Review recipe' })).toBeVisible();
  await page.getByRole('button', { name: 'Add ingredient line' }).click();
  await expect(page.locator('li.line')).toHaveCount(3);
  await page.getByRole('button', { name: 'Remove line' }).last().click();

  await page.getByRole('button', { name: 'Confirm and save' }).first().click();
  await expect(page).toHaveURL(/\/recipes\/\d+$/);
  await expect(page.getByText('3 tbsp')).toBeVisible();
});

// ADR-00020 obligation 4: the review screen is the recipe editor, so an amount,
// a unit and a step dependency edited on the draft are what is saved.
test('an amount, a unit and a step dependency edited on review are what confirm saves', async ({ page }) => {
  await page.goto('/ingest');
  await page.getByLabel('Or paste the recipe text').fill(THREE_STEPS);
  await page.getByRole('button', { name: 'Read recipe' }).click();

  // The inferred graph is shown: each step follows the one before it.
  await expect(page.locator('[name="c.0.s.1.dep"][value="s1"]')).toBeChecked();
  await expect(page.locator('[name="c.0.s.2.dep"][value="s2"]')).toBeChecked();

  const first = page.locator('li.line').nth(0);
  await first.getByLabel('Quantity').fill('3');
  await first.getByLabel('Unit', { exact: true }).fill('tbsp');

  // Step 3 follows step 1 instead of step 2, and step 2 is passive.
  await page.locator('[name="c.0.s.2.dep"][value="s2"]').uncheck();
  await page.locator('[name="c.0.s.2.dep"][value="s1"]').check();
  await page.locator('[name="c.0.s.1.passive"]').check();

  // A structural button keeps the edit.
  await page.getByRole('button', { name: 'Add ingredient line' }).click();
  await page.getByRole('button', { name: 'Remove line' }).last().click();
  await expect(page.locator('[name="c.0.s.2.dep"][value="s1"]')).toBeChecked();

  await page.getByRole('button', { name: 'Confirm and save' }).first().click();
  await expect(page).toHaveURL(/\/recipes\/(\d+)$/);
  const id = page.url().match(/\/recipes\/(\d+)$/)![1];
  await expect(page.getByText('3 tbsp')).toBeVisible();

  await page.goto(`/recipes/${id}/edit`);
  await expect(page.locator('[name="c.0.s.2.dep"][value="s1"]')).toBeChecked();
  await expect(page.locator('[name="c.0.s.2.dep"][value="s2"]')).not.toBeChecked();
  await expect(page.locator('[name="c.0.s.1.dep"][value="s1"]')).toBeChecked();
  await expect(page.locator('[name="c.0.s.1.passive"]')).toBeChecked();
  await expect(page.locator('[name="c.0.s.0.passive"]')).not.toBeChecked();
});

test('cancel discards the draft and saves nothing', async ({ page, request }) => {
  await page.goto('/ingest');
  await page.getByLabel('Or paste the recipe text').fill(PANCAKES);
  await page.getByRole('button', { name: 'Read recipe' }).click();
  const reviewURL = page.url();
  const token = reviewURL.split('/').pop();

  await page.getByRole('button', { name: 'Cancel' }).first().click();
  await expect(page).toHaveURL(/\/ingest$/);

  // The draft is gone, so it can be neither reviewed nor confirmed.
  expect((await request.get(`/ingest/${token}`)).status()).toBe(404);
  expect((await request.post(`/ingest/${token}/confirm`, { form: { name: 'x' } })).status()).toBe(404);
});

test('unknown drafts are 404 on every draft route', async ({ request }) => {
  expect((await request.get('/ingest/unknown')).status()).toBe(404);
  expect((await request.post('/ingest/unknown/confirm', { form: {} })).status()).toBe(404);
  expect((await request.post('/ingest/unknown/cancel', { form: {}, maxRedirects: 0 })).status()).toBe(303);
});

test('a URL is fetched from the fixture server and reviewed', async ({ page }) => {
  await page.goto('/ingest');
  await page.getByLabel('Recipe URL').fill(`${fixtureURL}/toast`);
  await page.getByRole('button', { name: 'Read recipe' }).click();
  await expect(page.getByLabel('Name', { exact: true })).toHaveValue('Fixture Toast');
  await expect(page.getByLabel('Original text').first()).toHaveValue('2 slices bread');
  await expect(page.getByText(`Read from ${fixtureURL}/toast`)).toBeVisible();
});

test('a failed fetch shows a message on the form and no draft', async ({ page }) => {
  await page.goto('/ingest');
  await page.getByLabel('Recipe URL').fill(`${fixtureURL}/missing`);
  await page.getByRole('button', { name: 'Read recipe' }).click();
  await expect(page.getByRole('alert')).toContainText('Could not fetch that page');
  await expect(page.getByLabel('Recipe URL')).toHaveValue(`${fixtureURL}/missing`);
  await expect(page.getByRole('heading', { name: 'Review recipe' })).toHaveCount(0);
});

test('a YouTube video in the page markup reaches the review screen and is saved on confirm', async ({ page }) => {
  await page.goto('/ingest');
  await page.getByLabel('Recipe URL').fill(`${fixtureURL}/toast-youtube`);
  await page.getByRole('button', { name: 'Read recipe' }).click();

  await expect(page.getByLabel('YouTube video (link or ID, optional)')).toHaveValue(YOUTUBE_ID);
  // The review offers the video, not the stored recipe's other source fields.
  await expect(page.locator('[name="tags"]')).toHaveCount(0);
  await expect(page.locator('[name="source_url"]')).toHaveCount(0);

  await page.getByRole('button', { name: 'Confirm and save' }).first().click();
  await expect(page).toHaveURL(/\/recipes\/\d+$/);
  await expect(page.getByTestId('video')).toHaveCount(1);
});

test('a video on another host is not drafted, and the review field starts empty', async ({ page }) => {
  await page.goto('/ingest');
  await page.getByLabel('Recipe URL').fill(`${fixtureURL}/toast-vimeo`);
  await page.getByRole('button', { name: 'Read recipe' }).click();
  await expect(page.getByLabel('YouTube video (link or ID, optional)')).toHaveValue('');
});

test('a page with no video leaves the review video field empty', async ({ page }) => {
  await page.goto('/ingest');
  await page.getByLabel('Recipe URL').fill(`${fixtureURL}/toast`);
  await page.getByRole('button', { name: 'Read recipe' }).click();
  await expect(page.getByLabel('YouTube video (link or ID, optional)')).toHaveValue('');
});
