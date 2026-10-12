import { test, expect } from '@playwright/test';
import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

// The list needs recipes, which the shared server of the suite does not have.
// This spec starts its own gobbler on a throwaway data directory in dev-auth
// mode, checks the empty library first, then seeds recipes through the real
// store (seed/main.go) while the server runs, and drives the page in a browser.
const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');

const port = Number(process.env.GOBBLER_E2E_RECIPES_PORT ?? 38491);
const origin = `http://127.0.0.1:${port}`;
const repoRoot = fileURLToPath(new URL('../..', import.meta.url));

let dataDir = '';
let server: ChildProcess | undefined;

test.use({ baseURL: origin });
// One server per file; the empty-library test must run before the seeding.
test.describe.configure({ mode: 'serial' });

test.beforeAll(async () => {
  const stale = await fetch(`${origin}/healthz`).then(() => true, () => false);
  if (stale) throw new Error(`something already listens on ${origin}; set GOBBLER_E2E_RECIPES_PORT`);
  dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-recipes-'));
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
    if (Date.now() > deadline) throw new Error('the recipes server did not start');
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

test('an empty library shows an empty state, not an error', async ({ page }) => {
  const res = await page.goto('/recipes');
  expect(res?.status()).toBe(200);
  await expect(page.getByRole('heading', { name: 'Recipes', level: 1 })).toBeVisible();
  await expect(page.getByTestId('empty-library')).toBeVisible();
});

test.describe('with recipes', () => {
  let ids: Record<string, number> = {};

  test.beforeAll(() => {
    const out = execFileSync('go', ['run', './e2e/recipes/seed', dataDir], { cwd: repoRoot, encoding: 'utf8' });
    ids = JSON.parse(out);
  });

  test('the list shows each recipe with rating, tags and times', async ({ page }) => {
    await page.goto('/recipes');
    const items = page.locator('.recipe-list > li');
    await expect(items).toHaveCount(3);
    // By name, case-insensitively.
    await expect(items.nth(0)).toContainText('Beef Stew');
    await expect(items.nth(2)).toContainText('Tomato Soup');
    const cake = items.filter({ hasText: 'Lemon Cake' });
    await expect(cake).toContainText('4 of 5');
    await expect(cake).toContainText('Prep 15 min');
    await expect(cake).toContainText('Cook 40 min');
    await expect(cake.getByRole('list', { name: 'Tags' })).toContainText('dessert');
    await expect(cake.getByRole('link', { name: 'Lemon Cake' })).toHaveAttribute('href', `/recipes/${ids['Lemon Cake']}`);
  });

  test('search narrows the results, updates the URL and survives a reload', async ({ page }) => {
    await page.goto('/recipes');
    await page.getByRole('searchbox', { name: 'Search recipes' }).fill('lemon');
    await expect(page.locator('.recipe-list > li')).toHaveCount(1);
    await expect(page.locator('.recipe-list > li')).toContainText('Lemon Cake');
    await expect(page).toHaveURL(/\/recipes\?q=lemon/);
    await page.reload();
    await expect(page.locator('.recipe-list > li')).toHaveCount(1);
    await expect(page.getByRole('searchbox', { name: 'Search recipes' })).toHaveValue('lemon');
  });

  test('search reaches ingredients, descriptions and notes', async ({ page }) => {
    for (const [text, name] of [['tomatoes', 'Tomato Soup'], ['zesty', 'Lemon Cake'], ['thyme', 'Beef Stew']]) {
      await page.goto(`/recipes?q=${text}`);
      await expect(page.locator('.recipe-list > li')).toHaveCount(1);
      await expect(page.locator('.recipe-list > li')).toContainText(name);
    }
  });

  test('a tag chip filters the list and a second chip narrows it further', async ({ page }) => {
    await page.goto('/recipes');
    await page.getByRole('link', { name: /^dinner/ }).click();
    await expect(page.locator('.recipe-list > li')).toHaveCount(2);
    await expect(page).toHaveURL(/tag=dinner/);
    await page.getByRole('link', { name: /^vegan/ }).click();
    await expect(page.locator('.recipe-list > li')).toHaveCount(1);
    await expect(page.locator('.recipe-list > li')).toContainText('Tomato Soup');
    // Selecting a chip again removes the filter.
    await page.getByRole('link', { name: /^vegan/ }).click();
    await expect(page.locator('.recipe-list > li')).toHaveCount(2);
  });

  test('a typed search keeps the chosen tag', async ({ page }) => {
    await page.goto('/recipes?tag=dinner');
    await page.getByRole('searchbox', { name: 'Search recipes' }).fill('stew');
    await expect(page.locator('.recipe-list > li')).toHaveCount(1);
    await expect(page).toHaveURL(/tag=dinner/);
    await page.getByRole('searchbox', { name: 'Search recipes' }).fill('lemon');
    await expect(page.getByTestId('no-match')).toBeVisible();
  });

  test('a search with no match shows the empty state, and odd input is not an error', async ({ page }) => {
    await page.goto('/recipes?q=zebra');
    await expect(page.getByTestId('no-match')).toBeVisible();
    for (const q of ['"', 'AND', 'name:lemon', '(', '*']) {
      const res = await page.goto(`/recipes?q=${encodeURIComponent(q)}`);
      expect(res?.status()).toBe(200);
    }
  });

  test('the page loads nothing from another origin', async ({ page }) => {
    const foreign: string[] = [];
    page.on('request', (req) => {
      if (!req.url().startsWith(origin)) foreign.push(req.url());
    });
    await page.goto('/recipes?q=lemon');
    await page.waitForLoadState('networkidle');
    expect(foreign).toEqual([]);
  });
});
