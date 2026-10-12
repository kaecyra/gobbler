import { test, expect } from '@playwright/test';
import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

// The recipe view needs a recipe in the database, which the shared server of
// the suite does not have. This spec seeds its own data directory through the
// real store (seed/main.go), starts its own gobbler on it in dev-auth mode, and
// reads the page through a browser. Loopback only, throwaway data.
const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');
const accessURL = process.env.GOBBLER_E2E_ACCESS_URL;
if (!accessURL) throw new Error('GOBBLER_E2E_ACCESS_URL is not set; run through playwright.config.ts');

const port = Number(process.env.GOBBLER_E2E_RECIPEVIEW_PORT ?? 38431);
const origin = `http://127.0.0.1:${port}`;
const repoRoot = fileURLToPath(new URL('../..', import.meta.url));

let dataDir = '';
let server: ChildProcess | undefined;
let id = 0;
let meanwhileId = 0;

test.use({ baseURL: origin });
// One server per file: fullyParallel would otherwise run beforeAll in every worker.
test.describe.configure({ mode: 'serial' });

test.beforeAll(async () => {
  // A leftover server on the port would answer with someone else's data.
  const stale = await fetch(`${origin}/healthz`).then(() => true, () => false);
  if (stale) throw new Error(`something already listens on ${origin}; set GOBBLER_E2E_RECIPEVIEW_PORT`);
  dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-recipeview-'));
  const out = execFileSync('go', ['run', './e2e/recipeview/seed', dataDir], { cwd: repoRoot, encoding: 'utf8' });
  ({ recipe: id, meanwhile: meanwhileId } = JSON.parse(out));
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
    if (Date.now() > deadline) throw new Error('the recipe-view server did not start');
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

test('shows the recipe as written', async ({ page }) => {
  const res = await page.goto(`/recipes/${id}`);
  expect(res?.status()).toBe(200);
  await expect(page).toHaveTitle(/E2E Sponge/);
  await expect(page.getByRole('heading', { name: 'E2E Sponge', level: 1 })).toBeVisible();
  await expect(page.getByText('1 1/3 cup e2e flour')).toBeVisible();
  await expect(page.getByText('1/3 cup e2e sugar')).toBeVisible();
  await expect(page.getByText('Bake until golden.')).toBeVisible();
  await expect(page.getByTestId('equipment')).toContainText('e2e 9x13 pan');
  await expect(page.getByTestId('total')).toContainText('55 min');
});

test('a passive step shows the work that can be done meanwhile', async ({ page }) => {
  await page.goto(`/recipes/${meanwhileId}`);
  const guidance = page.getByTestId('meanwhile');
  // Only the dough rise has guidance; the sauce's second step waits for the first.
  await expect(guidance).toHaveCount(1);
  await expect(guidance).toContainText('step 4: Chop the tomatoes.');
  await expect(guidance).not.toContainText('Simmer the sauce.');
  await expect(page.locator('li', { hasText: 'Let the dough rise.' }).first()).toContainText('Meanwhile');
});

test('a recipe without anything to do meanwhile shows no guidance', async ({ page }) => {
  await page.goto(`/recipes/${id}`);
  await expect(page.getByText('Bake until golden.')).toBeVisible();
  await expect(page.getByTestId('meanwhile')).toHaveCount(0);
});

test('scaling by a preset swaps the recipe in place and keeps the URL shareable', async ({ page }) => {
  await page.goto(`/recipes/${id}`);
  await page.getByRole('link', { name: '3×' }).click();
  // 1/3 cup x 3 is exactly 1 cup; 1 1/3 x 3 is exactly 4; the bay leaf is fixed.
  await expect(page.getByText('1 cup e2e sugar')).toBeVisible();
  await expect(page.getByText('4 cup e2e flour')).toBeVisible();
  await expect(page.getByText('6 e2e egg')).toBeVisible();
  await expect(page.getByText('1 e2e bay leaf')).toBeVisible();
  await expect(page.getByTestId('servings')).toHaveText('12');
  await expect(page).toHaveURL(`/recipes/${id}?scale=3`);

  // The URL alone reproduces the state.
  await page.reload();
  await expect(page.getByText('1 cup e2e sugar')).toBeVisible();
});

test('scaling to a serving count', async ({ page }) => {
  await page.goto(`/recipes/${id}`);
  await page.getByLabel('Servings').fill('2');
  await page.getByRole('button', { name: 'Scale' }).click();
  await expect(page.getByTestId('servings')).toHaveText('2');
  await expect(page.getByText('2/3 cup e2e flour')).toBeVisible();
});

test('unit modes convert the view and combine with scaling', async ({ page }) => {
  await page.goto(`/recipes/${id}`);
  await page.getByRole('link', { name: 'Metric' }).click();
  await expect(page.getByText('315 ml e2e flour')).toBeVisible();
  await page.getByRole('link', { name: 'Weight' }).click();
  // flour has a density, sugar has none and stays a volume.
  await expect(page.getByText('160 g e2e flour')).toBeVisible();
  await expect(page.getByText('79 ml e2e sugar')).toBeVisible();
  await page.getByRole('link', { name: '2×' }).click();
  await expect(page).toHaveURL(`/recipes/${id}?scale=2&units=weight`);
  await expect(page.getByText('315 g e2e flour')).toBeVisible();
  await page.getByRole('link', { name: 'As written' }).click();
  await expect(page.getByText('2 2/3 cup e2e flour')).toBeVisible();
});

test('a servings value that cannot be used shows a message in place', async ({ page }) => {
  await page.goto(`/recipes/${id}`);
  await page.getByLabel('Servings').fill('0');
  await page.getByRole('button', { name: 'Scale' }).click();
  await expect(page.getByTestId('view-error')).toContainText('cannot be used');
  // The recipe stays on screen and the bad value does not reach the address bar.
  await expect(page.getByText('1 1/3 cup e2e flour')).toBeVisible();
  await expect(page).toHaveURL(`/recipes/${id}`);
});

test('a missing recipe is a 404 page', async ({ page }) => {
  const res = await page.goto(`/recipes/${id + 1000}`);
  expect(res?.status()).toBe(404);
});

test('a scale that cannot be shown is a 400 page', async ({ page }) => {
  const res = await page.goto(`/recipes/${id}?scale=0`);
  expect(res?.status()).toBe(400);
});

test('the recipe is refused without an Access identity', async ({ request }) => {
  const res = await request.get(`${accessURL}/recipes/${id}`);
  expect(res.status()).toBe(403);
});
