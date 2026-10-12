import { test, expect, type Page } from '@playwright/test';
import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

// The shopping pages need recipes that share an ingredient, which the shared
// server of the suite does not have. This spec seeds its own data directory
// through the real catalog and store (seed/main.go), starts its own gobbler on
// it in dev-auth mode, and drives the pages through browsers. Loopback only,
// throwaway data.
const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');

const port = Number(process.env.GOBBLER_E2E_SHOPPING_PORT ?? 38531);
const origin = `http://127.0.0.1:${port}`;
const repoRoot = fileURLToPath(new URL('../..', import.meta.url));

let dataDir = '';
let server: ChildProcess | undefined;
let ids: { flour: number; milk: number; salt: number; pancakes: number; waffles: number; garnish: number; basil: number };
let listID = 0;

test.use({ baseURL: origin });
// One server per file, and the tests build on each other's data.
test.describe.configure({ mode: 'serial' });

test.beforeAll(async () => {
  const stale = await fetch(`${origin}/healthz`).then(() => true, () => false);
  if (stale) throw new Error(`something already listens on ${origin}; set GOBBLER_E2E_SHOPPING_PORT`);
  dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-shopping-'));
  ids = JSON.parse(execFileSync('go', ['run', './e2e/shopping/seed', dataDir], { cwd: repoRoot, encoding: 'utf8' }));
  server = spawn(bin, ['serve'], {
    env: {
      PATH: process.env.PATH ?? '',
      GOBBLER_LISTEN_ADDR: `127.0.0.1:${port}`,
      GOBBLER_DATA_DIR: dataDir,
      GOBBLER_LOG_LEVEL: 'warn',
      GOBBLER_DEV_AUTH_BYPASS: 'true',
    },
    stdio: 'inherit',
  });
  const deadline = Date.now() + 20_000;
  for (;;) {
    try {
      if ((await fetch(`${origin}/healthz`)).ok) break;
    } catch {
      // not listening yet
    }
    if (Date.now() > deadline) throw new Error('the shopping server did not start');
    await new Promise((r) => setTimeout(r, 100));
  }
});

test.afterAll(async () => {
  if (server && server.exitCode === null) {
    const exited = new Promise((resolve) => server!.once('exit', resolve));
    server.kill('SIGTERM');
    await exited;
  }
  if (dataDir) rmSync(dataDir, { recursive: true, force: true });
});

const item = (page: Page, name: string) => page.getByTestId('item').filter({ hasText: name });

async function addRecipe(page: Page, name: string, multiplier = '1') {
  const form = page.getByTestId('add-recipe');
  await form.getByLabel('Recipe').selectOption({ label: name });
  await form.getByLabel('Multiplier').fill(multiplier);
  await form.getByRole('button', { name: 'Add recipe' }).click();
}

test('creates a list and shows it empty', async ({ page }) => {
  const res = await page.goto('/shopping');
  expect(res?.status()).toBe(200);
  await expect(page.getByTestId('lists-empty')).toBeVisible();
  await page.getByLabel('New list').fill('E2E Week');
  await page.getByRole('button', { name: 'Create' }).click();
  await expect(page).toHaveURL(/\/shopping\/\d+$/);
  listID = Number(page.url().split('/').pop());
  await expect(page.getByRole('heading', { name: 'E2E Week', level: 1 })).toBeVisible();
  await expect(page.getByTestId('list-empty')).toBeVisible();
  await expect(page.getByTestId('entries-empty')).toBeVisible();
});

test('serves its script and stylesheet from the binary', async ({ request }) => {
  const js = await request.get('/shopping/shopping.js');
  expect(js.status()).toBe(200);
  expect(js.headers()['content-type']).toContain('javascript');
  expect((await request.get('/shopping/shopping.css')).status()).toBe(200);
});

test('builds a list from two recipes and merges the shared ingredient', async ({ page }) => {
  await page.goto(`/shopping/${listID}`);
  // A multiplier that is not a positive number is explained next to the form.
  await addRecipe(page, 'E2E Pancakes', 'lots');
  await expect(page.getByTestId('problem')).toContainText('multiplier');
  await expect(page.getByTestId('entries-empty')).toBeVisible();

  await addRecipe(page, 'E2E Pancakes');
  await addRecipe(page, 'E2E Waffles');
  await expect(page.getByTestId('entry')).toHaveCount(2);

  const flour = item(page, 'e2e flour');
  await expect(flour).toHaveCount(1);
  await expect(flour.getByTestId('item-amount')).toHaveText(/^3\s*cup/);
  await expect(flour.getByTestId('item-sources')).toHaveText('E2E Pancakes, E2E Waffles');
  await expect(item(page, 'e2e milk').getByTestId('item-amount')).toHaveText(/^2\s*cup/);
  // Items sit under their aisle.
  await expect(page.getByTestId('aisle-name')).toContainText(['baking', 'dairy', 'spices']);

  // Adding the same recipe again is explained, not an error page.
  await addRecipe(page, 'E2E Pancakes');
  await expect(page.getByTestId('problem')).toContainText('already on the list');
});

test('adjusts a multiplier and removes a recipe', async ({ page }) => {
  await page.goto(`/shopping/${listID}`);
  const pancakes = page.getByTestId('entry').filter({ hasText: 'E2E Pancakes' });
  await pancakes.getByLabel('Multiplier').fill('0');
  await pancakes.getByRole('button', { name: 'Update' }).click();
  await expect(page.getByTestId('problem')).toContainText('above zero');

  await pancakes.getByLabel('Multiplier').fill('2');
  await pancakes.getByRole('button', { name: 'Update' }).click();
  await expect(item(page, 'e2e flour').getByTestId('item-amount')).toHaveText(/^4\s*cup/);

  const waffles = page.getByTestId('entry').filter({ hasText: 'E2E Waffles' });
  await waffles.getByRole('button', { name: 'Remove E2E Waffles' }).click();
  await expect(page.getByTestId('entry')).toHaveCount(1);
  await expect(item(page, 'e2e flour').getByTestId('item-amount')).toHaveText(/^2\s*cup/);
  await addRecipe(page, 'E2E Waffles');
  await expect(item(page, 'e2e flour').getByTestId('item-amount')).toHaveText(/^4\s*cup/);
});

test('adds manual items, free text and from the catalog', async ({ page }) => {
  await page.goto(`/shopping/${listID}`);
  const form = page.getByTestId('add-manual');
  await form.getByRole('button', { name: 'Add item' }).click();
  await expect(page.getByTestId('problem')).toContainText('needs a name');

  await form.getByLabel('Item', { exact: true }).fill('E2E paper towels');
  await form.getByRole('button', { name: 'Add item' }).click();
  await expect(item(page, 'E2E paper towels')).toHaveCount(1);
  await expect(item(page, 'E2E paper towels').getByTestId('item-sources')).toHaveText('added by hand');

  await page.getByTestId('add-manual').getByLabel('or catalog ingredient').selectOption({ label: 'e2e milk' });
  await page.getByTestId('add-manual').getByLabel('Quantity').fill('1');
  await page.getByTestId('add-manual').getByLabel('Unit').selectOption('cup');
  await page.getByTestId('add-manual').getByRole('button', { name: 'Add item' }).click();
  await expect(item(page, 'e2e milk').getByTestId('item-amount')).toHaveText(/^4\s*cup/);

  // Remove the milk again.
  await page.getByTestId('manual-item').filter({ hasText: 'e2e milk' }).getByRole('button').click();
  await expect(item(page, 'e2e milk').getByTestId('item-amount')).toHaveText(/^3\s*cup/);
});

test('leaves pantry items off the list and adds one back', async ({ page, request }) => {
  await page.goto(`/shopping/${listID}`);
  await expect(item(page, 'e2e salt')).toHaveCount(1);

  await page.goto('/shopping/pantry');
  await expect(page.getByTestId('pantry-empty')).toBeVisible();
  await page.getByLabel('Find an ingredient').fill('SALT');
  await page.getByRole('button', { name: 'Search' }).click();
  await page.getByTestId('match').getByRole('button', { name: 'Add e2e salt to the pantry' }).click();
  await expect(page.getByTestId('pantry-item')).toHaveText(/e2e salt/);
  // Adding what is already there is not an error.
  const again = await request.post('/shopping/pantry/add', { form: { ingredient: String(ids.salt) } });
  expect(again.ok()).toBe(true);
  await expect(page.getByTestId('pantry-item')).toHaveCount(1);

  await page.goto(`/shopping/${listID}`);
  await expect(item(page, 'e2e salt')).toHaveCount(0);
  const dropped = page.getByTestId('dropped');
  await expect(dropped.getByTestId('dropped-item')).toContainText('e2e salt');
  await expect(dropped.getByTestId('dropped-item')).toContainText('E2E Pancakes');

  await dropped.getByRole('button', { name: 'Add e2e salt back' }).click();
  await expect(item(page, 'e2e salt')).toHaveCount(1);
  await expect(page.getByTestId('dropped')).toHaveCount(0);

  // Taking the added-back item off the list sets salt aside again.
  await page.getByTestId('manual-item').filter({ hasText: 'e2e salt' }).getByRole('button').click();
  await expect(item(page, 'e2e salt')).toHaveCount(0);
  await expect(page.getByTestId('dropped')).toBeVisible();
});

test('a check in one browser appears in another without reload', async ({ browser }) => {
  const phone = { viewport: { width: 390, height: 844 } };
  const a = await browser.newContext(phone);
  const b = await browser.newContext(phone);
  try {
    const pa = await a.newPage();
    const pb = await b.newPage();
    await pa.goto(`/shopping/${listID}/live`);
    await pb.goto(`/shopping/${listID}/live`);
    await expect(pa.getByTestId('live-status')).toHaveText('Live');
    await expect(pb.getByTestId('live-status')).toHaveText('Live');

    const flourA = pa.getByRole('checkbox', { name: /e2e flour/ });
    const flourB = pb.getByRole('checkbox', { name: /e2e flour/ });
    await expect(flourB).toHaveAttribute('aria-checked', 'false');
    await flourA.click();
    await expect(flourA).toHaveAttribute('aria-checked', 'true');
    await expect(flourB).toHaveAttribute('aria-checked', 'true');

    // And back, from the other side. B never reloaded.
    await flourB.click();
    await expect(flourA).toHaveAttribute('aria-checked', 'false');
    await expect(flourB).toHaveAttribute('aria-checked', 'false');
    expect(pb.url()).toBe(`${origin}/shopping/${listID}/live`);

    // A view opened later starts from the current state.
    await flourA.click();
    const c = await browser.newContext(phone);
    try {
      const pc = await c.newPage();
      await pc.goto(`/shopping/${listID}/live`);
      await expect(pc.getByRole('checkbox', { name: /e2e flour/ })).toHaveAttribute('aria-checked', 'true');
    } finally {
      await c.close();
    }
  } finally {
    await a.close();
    await b.close();
  }
});

test('an open phone view follows edits made on the build page, and checks survive them', async ({ browser }) => {
  const phone = await browser.newContext({ viewport: { width: 390, height: 844 } });
  const desk = await browser.newContext();
  try {
    const live = await phone.newPage();
    await live.goto(`/shopping/${listID}/live`);
    await expect(live.getByTestId('live-status')).toHaveText('Live');
    await expect(live.getByRole('checkbox', { name: /e2e flour/ })).toHaveAttribute('aria-checked', 'true');

    const build = await desk.newPage();
    await build.goto(`/shopping/${listID}`);
    const waffles = build.getByTestId('entry').filter({ hasText: 'E2E Waffles' });
    await waffles.getByLabel('Multiplier').fill('3');
    await waffles.getByRole('button', { name: 'Update' }).click();

    // 2 + 3 x 2 cups, and the check made before the edit is still there.
    const flour = live.getByRole('checkbox', { name: /e2e flour/ });
    await expect(flour).toContainText(/8\s*cup/);
    await expect(flour).toHaveAttribute('aria-checked', 'true');

    await build.getByRole('button', { name: 'Refresh the list' }).click();
    await expect(item(build, 'e2e flour').getByTestId('item-amount')).toHaveText(/^8\s*cup/);
    await build.goto(`/shopping/${listID}/live`);
    await expect(build.getByRole('checkbox', { name: /e2e flour/ })).toHaveAttribute('aria-checked', 'true');
  } finally {
    await phone.close();
    await desk.close();
  }
});

test('exports the list as plain text grouped by aisle, with a copy action', async ({ browser, request }) => {
  const res = await request.get(`/shopping/${listID}/export.txt`);
  expect(res.status()).toBe(200);
  expect(res.headers()['content-type']).toContain('text/plain');
  const text = await res.text();
  expect(text).toMatch(/Baking\n- e2e flour: 8\s*cup\n/);
  expect(text).toMatch(/Dairy\n- e2e milk: 5\s*cup\n/);
  expect(text).toContain('Uncategorised\n- E2E paper towels\n');
  expect(text).not.toContain('e2e salt');

  const ctx = await browser.newContext();
  await ctx.grantPermissions(['clipboard-read', 'clipboard-write'], { origin });
  try {
    const page = await ctx.newPage();
    await page.goto(`/shopping/${listID}`);
    await expect(page.getByTestId('export-text')).toHaveValue(text);
    await page.getByTestId('copy').click();
    await expect(page.getByTestId('copy')).toHaveText('Copied');
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(text);
    await page.getByTestId('export-link').click();
    await expect(page).toHaveURL(`${origin}/shopping/${listID}/export.txt`);
  } finally {
    await ctx.close();
  }
});

test('answers an unknown list with 404 on every route, and an event stream with headers', async ({ page, request }) => {
  for (const path of ['/shopping/999999', '/shopping/999999/live', '/shopping/999999/events', '/shopping/999999/export.txt']) {
    const res = await request.get(path);
    expect(res.status(), path).toBe(404);
  }
  const bad = await request.post('/shopping/999999/check', { form: { key: 'i:1', checked: '1' } });
  expect(bad.status()).toBe(404);
  const res = await page.goto('/shopping/999999');
  expect(res?.status()).toBe(404);
  await expect(page.getByRole('heading', { name: '404 Not Found' })).toBeVisible();

  // The stream of a real list is an event stream that opens with the list.
  const stream = await page.evaluate(async (url) => {
    const ctl = new AbortController();
    const r = await fetch(url, { signal: ctl.signal });
    const reader = r.body!.getReader();
    const { value } = await reader.read();
    ctl.abort();
    return { type: r.headers.get('content-type'), first: new TextDecoder().decode(value) };
  }, `/shopping/${listID}/events`);
  expect(stream.type).toContain('text/event-stream');
  expect(stream.first).toMatch(/^event: list\n/);
});

test('every form route works as a plain HTTP request, without script', async ({ request }) => {
  const created = await request.post('/shopping', { form: { name: 'E2E Plain' } });
  expect(created.ok()).toBe(true);
  const id = Number(created.url().split('/').pop());
  const page = async () => (await request.get(`/shopping/${id}`)).text();

  expect((await request.post(`/shopping/${id}/recipes`, { form: { recipe: String(ids.waffles), multiplier: '1' } })).ok()).toBe(true);
  const entry = (await page()).match(new RegExp(`/shopping/${id}/recipes/(\\d+)"`))?.[1];
  expect(entry).toBeTruthy();
  expect((await request.post(`/shopping/${id}/recipes/${entry}`, { form: { multiplier: '2' } })).ok()).toBe(true);
  expect(await page()).toMatch(/4\s*cup/);

  expect((await request.post(`/shopping/${id}/manual`, { form: { text: 'E2E plain item' } })).ok()).toBe(true);
  const manual = (await page()).match(new RegExp(`/shopping/${id}/manual/(\\d+)/remove`))?.[1];
  expect(manual).toBeTruthy();
  expect((await request.post(`/shopping/${id}/manual/${manual}/remove`)).ok()).toBe(true);
  expect(await page()).not.toContain('E2E plain item');

  expect((await request.post(`/shopping/${id}/addback`, { form: { ingredient: String(ids.salt) } })).ok()).toBe(true);
  expect(await page()).toContain('e2e salt');
  expect((await request.post(`/shopping/${id}/regenerate`)).ok()).toBe(true);
  expect((await request.post(`/shopping/${id}/recipes/${entry}/remove`)).ok()).toBe(true);
  expect(await page()).toContain('No recipes on this list.');
});

test('removes a manual item and a pantry ingredient by their own routes', async ({ page, request }) => {
  await page.goto(`/shopping/${listID}`);
  await page.getByTestId('manual-item').filter({ hasText: 'E2E paper towels' }).getByRole('button').click();
  await expect(item(page, 'E2E paper towels')).toHaveCount(0);

  const gone = await request.post('/shopping/pantry/remove', { form: { ingredient: String(ids.salt) } });
  expect(gone.ok()).toBe(true);
  await page.goto('/shopping/pantry');
  await expect(page.getByTestId('pantry-empty')).toBeVisible();
  await page.goto(`/shopping/${listID}`);
  await expect(item(page, 'e2e salt')).toHaveCount(1);
});

test('optional lines are left off until the list includes them, on the build page and the phone view', async ({ browser }) => {
  const desk = await browser.newContext();
  const phone = await browser.newContext({ viewport: { width: 390, height: 844 } });
  try {
    const build = await desk.newPage();
    await build.goto('/shopping');
    await build.getByLabel('New list').fill('E2E Optional');
    await build.getByRole('button', { name: 'Create' }).click();
    const id = Number(build.url().split('/').pop());
    await addRecipe(build, 'E2E Garnish');
    await expect(item(build, 'e2e flour')).toHaveCount(1);
    await expect(item(build, 'e2e basil')).toHaveCount(0);
    await expect(build.getByLabel('Include optional ingredients')).not.toBeChecked();

    const live = await phone.newPage();
    await live.goto(`/shopping/${id}/live`);
    await expect(live.getByTestId('live-status')).toHaveText('Live');
    await expect(live.getByRole('checkbox', { name: /e2e basil/ })).toHaveCount(0);

    // Including them adds basil to the build page and, without a reload, the phone.
    await build.getByLabel('Include optional ingredients').check();
    await build.getByTestId('optional-form').getByRole('button', { name: 'Save' }).click();
    await expect(build.getByLabel('Include optional ingredients')).toBeChecked();
    await expect(item(build, 'e2e basil')).toHaveCount(1);
    const basil = live.getByRole('checkbox', { name: /e2e basil/ });
    await expect(basil).toHaveCount(1);
    await basil.click();
    await expect(basil).toHaveAttribute('aria-checked', 'true');

    // Excluding takes it off both, and including again brings its check back.
    await build.getByLabel('Include optional ingredients').uncheck();
    await build.getByTestId('optional-form').getByRole('button', { name: 'Save' }).click();
    await expect(item(build, 'e2e basil')).toHaveCount(0);
    await expect(live.getByRole('checkbox', { name: /e2e basil/ })).toHaveCount(0);
    await expect(live.getByRole('checkbox', { name: /e2e flour/ })).toHaveCount(1);
    await build.getByLabel('Include optional ingredients').check();
    await build.getByTestId('optional-form').getByRole('button', { name: 'Save' }).click();
    await expect(live.getByRole('checkbox', { name: /e2e basil/ })).toHaveAttribute('aria-checked', 'true');

    // The route also answers a plain request, and an unknown list is a 404.
    const ok = await desk.request.post(`/shopping/${id}/optional`, { form: { optional: '1' } });
    expect(ok.ok()).toBe(true);
    expect((await desk.request.post('/shopping/999999/optional', { form: { optional: '1' } })).status()).toBe(404);
  } finally {
    await desk.close();
    await phone.close();
  }
});
