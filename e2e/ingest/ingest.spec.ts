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

let fixture: Server;
let fixtureURL: string;

test.beforeAll(async () => {
  fixture = createServer((req, res) => {
    if (req.url === '/toast') {
      res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' });
      res.end(RECIPE_PAGE);
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
  await expect(page.getByLabel('Name')).toHaveValue('Fluffy Pancakes');
  await expect(page.getByText('2 cups all-purpose flour')).toBeVisible();

  await page.getByLabel('Name').fill('Best Pancakes');
  await page.getByRole('button', { name: 'Confirm and save' }).click();
  // The recipe view is a later node; the redirect target is its URL.
  await expect(page).toHaveURL(/\/recipes\/\d+$/);
});

test('a low-confidence line is highlighted in words, a confident one is not', async ({ page }) => {
  await page.goto('/ingest');
  await page.getByLabel('Or paste the recipe text').fill(MESSY);
  await page.getByRole('button', { name: 'Read recipe' }).click();

  const flagged = page.locator('.lines li[data-low]');
  await expect(flagged).toHaveCount(1);
  await expect(flagged).toContainText('glorp');
  await expect(flagged).toContainText('Check:');
  await expect(page.locator('.lines li:not([data-low])')).toContainText('all-purpose flour');
});

test('a proposed new ingredient is listed, and is created only when the draft is confirmed', async ({ page }) => {
  await page.goto('/ingest');
  await page.getByLabel('Or paste the recipe text').fill(UNKNOWN);
  await page.getByRole('button', { name: 'Read recipe' }).click();

  const proposals = page.getByRole('region', { name: 'New ingredients' });
  await expect(proposals).toContainText('unreviewed');
  await expect(proposals.locator('[data-proposed]')).toHaveCount(1);
  await expect(proposals).toContainText('dragonfruit zest');
  // The matched line offers no such choice; the proposed one does.
  await expect(page.getByLabel('Add as new ingredient named')).toHaveValue(/dragonfruit zest/);
  await expect(page.getByLabel('Or use an existing ingredient (type its name)')).toBeVisible();

  // Naming an ingredient the catalog lacks is refused and the draft stays.
  await page.getByLabel('Or use an existing ingredient (type its name)').fill('no such ingredient');
  await page.getByRole('button', { name: 'Confirm and save' }).click();
  await expect(page.getByRole('alert')).toContainText('no existing ingredient is called');
  await expect(proposals).toContainText('dragonfruit zest');

  // Accepting it as new saves.
  await page.getByLabel('Or use an existing ingredient (type its name)').fill('');
  await page.getByRole('button', { name: 'Confirm and save' }).click();
  await expect(page).toHaveURL(/\/recipes\/\d+$/);
});

test('a proposed line can be mapped to an existing ingredient', async ({ page }) => {
  await page.goto('/ingest');
  await page.getByLabel('Or paste the recipe text').fill(UNKNOWN_FOR_MAPPING);
  await page.getByRole('button', { name: 'Read recipe' }).click();
  await page.getByLabel('Or use an existing ingredient (type its name)').fill('sugar');
  await page.getByRole('button', { name: 'Confirm and save' }).click();
  await expect(page).toHaveURL(/\/recipes\/\d+$/);
});

test('cancel discards the draft and saves nothing', async ({ page, request }) => {
  await page.goto('/ingest');
  await page.getByLabel('Or paste the recipe text').fill(PANCAKES);
  await page.getByRole('button', { name: 'Read recipe' }).click();
  const reviewURL = page.url();
  const token = reviewURL.split('/').pop();

  await page.getByRole('button', { name: 'Cancel' }).click();
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
  await expect(page.getByLabel('Name')).toHaveValue('Fixture Toast');
  await expect(page.getByText('2 slices bread')).toBeVisible();
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
