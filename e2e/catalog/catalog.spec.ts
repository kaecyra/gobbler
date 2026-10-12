import { test, expect } from '@playwright/test';
import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

// The catalog page needs unreviewed ingredients and a recipe that uses one,
// which the shared server of the suite does not have. This spec seeds its own
// data directory through the real catalog and store (seed/main.go), starts its
// own gobbler on it in dev-auth mode, and drives the page through a browser.
// Loopback only, throwaway data.
const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');

const port = Number(process.env.GOBBLER_E2E_CATALOG_PORT ?? 38521);
const origin = `http://127.0.0.1:${port}`;
const repoRoot = fileURLToPath(new URL('../..', import.meta.url));

let dataDir = '';
let server: ChildProcess | undefined;
let ids: { edit: number; source: number; target: number; recipe: number };

test.use({ baseURL: origin });
// One server per file, and the tests build on each other's data.
test.describe.configure({ mode: 'serial' });

test.beforeAll(async () => {
  const stale = await fetch(`${origin}/healthz`).then(() => true, () => false);
  if (stale) throw new Error(`something already listens on ${origin}; set GOBBLER_E2E_CATALOG_PORT`);
  dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-catalog-'));
  ids = JSON.parse(execFileSync('go', ['run', './e2e/catalog/seed', dataDir], { cwd: repoRoot, encoding: 'utf8' }));
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
    if (Date.now() > deadline) throw new Error('the catalog server did not start');
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

test('lists the unreviewed ingredients first', async ({ page }) => {
  const res = await page.goto('/catalog');
  expect(res?.status()).toBe(200);
  await expect(page.getByRole('heading', { name: 'Ingredient catalog', level: 1 })).toBeVisible();
  const unreviewed = page.getByTestId('unreviewed');
  await expect(unreviewed.getByText('e2e dill', { exact: true })).toBeVisible();
  await expect(unreviewed.getByText('e2e dillweed', { exact: true })).toBeVisible();
  await expect(unreviewed.getByText('e2e fresh dill')).toHaveCount(0);
  // The full list holds the reviewed one too, and the filter narrows it.
  await expect(page.getByTestId('all').getByText('e2e fresh dill')).toBeVisible();
  await page.getByLabel('Filter ingredients').fill('e2e fresh');
  await page.getByRole('button', { name: 'Filter' }).click();
  await expect(page).toHaveURL(/q=e2e(\+|%20)fresh/);
  await expect(page.getByTestId('unreviewed-empty')).toBeVisible();
  await expect(page.getByTestId('all-row')).toHaveCount(1);
});

test('rejects an invalid density inline, then edits and marks reviewed', async ({ page }) => {
  await page.goto(`/catalog/${ids.edit}`);
  await page.getByLabel('Density (grams per millilitre)').fill('heavy');
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByTestId('density-error')).toBeVisible();
  await expect(page.getByLabel('Density (grams per millilitre)')).toHaveValue('heavy');

  await page.getByLabel('Name').fill('');
  await page.getByLabel('Density (grams per millilitre)').fill('0.5');
  await page.getByLabel('Name').evaluate((el) => el.removeAttribute('required'));
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByTestId('name-error')).toBeVisible();

  await page.getByLabel('Name').fill('e2e dill');
  await page.getByLabel('Aliases').fill('e2e dills\ne2e dill sprig');
  await page.getByLabel('Reviewed').check();
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page).toHaveURL(/\/catalog$/);
  await expect(page.getByTestId('unreviewed').getByText('e2e dill', { exact: true })).toHaveCount(0);
  await expect(page.getByTestId('all').getByText('e2e dill sprig')).toBeVisible();
});

test('an alias that belongs to another ingredient is refused inline', async ({ page }) => {
  await page.goto(`/catalog/${ids.edit}`);
  await page.getByLabel('Aliases').fill('e2e fresh dill');
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByTestId('form-error')).toContainText('already used by');
});

test('mark reviewed from the list removes the ingredient from the unreviewed list', async ({ page }) => {
  await page.goto('/catalog');
  const done = page.waitForResponse((r) => r.url().endsWith(`/catalog/${ids.source}/reviewed`) && r.request().method() === 'POST');
  await page.getByRole('button', { name: 'Mark e2e dillweed reviewed' }).click();
  expect((await done).status()).toBe(303);
  await expect(page.getByTestId('unreviewed-empty')).toBeVisible();
});

test('merging states the line count and re-points the recipe', async ({ page }) => {
  await page.goto(`/catalog/merge?source=${ids.source}`);
  await page.getByLabel('Keep').selectOption({ label: 'e2e fresh dill' });
  await page.getByRole('button', { name: 'Review the merge' }).click();
  await expect(page.getByTestId('merge-lines')).toHaveText('2 recipe lines');
  await page.getByRole('button', { name: 'Merge' }).click();
  await expect(page).toHaveURL(new RegExp(`/catalog/${ids.target}$`));
  await expect(page.getByLabel('Aliases')).toHaveValue(/e2e dillweed/);
  // The source is gone.
  const gone = await page.goto(`/catalog/${ids.source}`);
  expect(gone?.status()).toBe(404);
});

test('merging an ingredient into itself is rejected', async ({ page }) => {
  const res = await page.goto(`/catalog/merge?source=${ids.target}&target=${ids.target}`);
  expect(res?.status()).toBe(400);
  await expect(page.getByTestId('merge-error')).toContainText('cannot be merged into itself');
  const missing = await page.goto(`/catalog/merge?source=${ids.target}&target=987654`);
  expect(missing?.status()).toBe(404);
});
