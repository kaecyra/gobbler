import { test, expect } from '@playwright/test';
import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { OPERATOR, OTHER, asIdentity } from '../fixtures/identity';
import { portFromEnv } from '../fixtures/ports.mjs';
import { expectTapTargets } from '../household/support';

// The catalog page needs unreviewed ingredients and a recipe that uses one,
// which the shared server of the suite does not have. This spec seeds its own
// data directory through the real catalog and store (seed/main.go), starts its
// own gobbler on it in dev-auth mode, and drives the page through a browser.
// Loopback only, throwaway data.
const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');

const port = await portFromEnv(process.env, 'GOBBLER_E2E_CATALOG_PORT');
const origin = `http://127.0.0.1:${port}`;
const repoRoot = fileURLToPath(new URL('../..', import.meta.url));

let dataDir = '';
let server: ChildProcess | undefined;
let ids: { edit: number; source: number; target: number; recipe: number; tin: number };

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
      GOBBLER_OPERATOR_EMAILS: OPERATOR,
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

test('merging states the use count and re-points the recipe', async ({ page }) => {
  await page.goto(`/catalog/merge?source=${ids.source}`);
  await page.getByLabel('Keep').selectOption({ label: 'e2e fresh dill' });
  await page.getByRole('button', { name: 'Review the merge' }).click();
  await expect(page.getByTestId('merge-uses')).toHaveText('2');
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

// The overlay (ADR-00018): the ingredients above are the dev household's own;
// the seeded catalog is central, which a household reads and customises.

test('a central ingredient is read-only apart from the household\'s own aliases and aisle', async ({ page }) => {
  await page.goto('/catalog?q=onion');
  await expect(page.getByTestId('central-badge').first()).toBeVisible();
  await page.getByRole('link', { name: 'onion', exact: true }).click();
  await expect(page.getByTestId('central-note')).toBeVisible();
  await expect(page.getByLabel('Name')).toHaveAttribute('readonly', '');
  await expect(page.getByTestId('suggest-form')).toHaveCount(0);
  await expect(page.getByRole('link', { name: 'Merge this ingredient into another' })).toHaveCount(0);

  const aliases = page.getByLabel('Aliases');
  await aliases.fill((await aliases.inputValue()) + '\ne2e bulb');
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page).toHaveURL(/\/catalog$/);
  await page.goto('/catalog?q=e2e+bulb');
  await expect(page.getByTestId('all-row')).toHaveCount(1);
  await expect(page.getByTestId('all-row')).toContainText('onion');
});

test('suggesting an own ingredient for the central catalog can be withdrawn', async ({ page }) => {
  await page.goto(`/catalog/${ids.edit}`);
  const suggested = page.waitForResponse((r) => r.url().endsWith(`/catalog/${ids.edit}/suggest`) && r.request().method() === 'POST');
  await page.getByRole('button', { name: 'Suggest for the central catalog' }).click();
  expect((await suggested).status()).toBe(303);
  await expect(page.getByTestId('unsuggest-form')).toBeVisible();

  const withdrawn = page.waitForResponse((r) => r.url().endsWith(`/catalog/${ids.edit}/unsuggest`) && r.request().method() === 'POST');
  await page.getByRole('button', { name: 'Withdraw suggestion' }).click();
  expect((await withdrawn).status()).toBe(303);
  await expect(page.getByTestId('suggest-form')).toBeVisible();
});

test('another household sees nothing of an ingredient it does not own', async ({ browser }) => {
  const other = await browser.newContext({ baseURL: origin, ...asIdentity(OTHER) });
  try {
    expect((await other.request.get(`/catalog/${ids.edit}`)).status()).toBe(404);
    expect((await other.request.post(`/catalog/${ids.edit}/suggest`)).status()).toBe(404);
    expect((await other.request.post(`/catalog/${ids.edit}/unsuggest`)).status()).toBe(404);
    expect((await other.request.post(`/catalog/${ids.edit}`, { form: { name: 'e2e hijack', aisle: 'x' } })).status()).toBe(404);
    const list = await other.request.get('/catalog');
    expect(await list.text()).not.toContain('e2e dill');
  } finally {
    await other.close();
  }
});

// The sized flag (ADR-00019): the operator sets it on catalog equipment, and a
// scaled recipe view then hints at a larger pan on that equipment only when scaled up.
test.describe('equipment sized flag', () => {
  const hint = 'may need a larger pan';

  test('a household member cannot reach the equipment review or set the flag', async ({ browser }) => {
    const ctx = await browser.newContext({ baseURL: origin, ...asIdentity(OTHER) });
    try {
      expect((await ctx.request.get('/operator/equipment')).status()).toBe(403);
      expect((await ctx.request.post(`/operator/equipment/${ids.tin}/sized`, { form: { sized: '1' } })).status()).toBe(403);
    } finally {
      await ctx.close();
    }
    const dev = await browser.newContext({ baseURL: origin });
    try {
      expect((await dev.request.get('/operator/equipment')).status()).toBe(403);
      // The refused attempts changed nothing: the scaled view still has no hint.
      const view = await dev.request.get(`/recipes/${ids.recipe}?scale=2`);
      expect(await view.text()).not.toContain(hint);
    } finally {
      await dev.close();
    }
  });

  test('the operator sets the flag and the scaled view hints, then clears it', async ({ browser }) => {
    const op = await browser.newContext({ baseURL: origin, ...asIdentity(OPERATOR) });
    const member = await browser.newContext({ baseURL: origin });
    try {
      const page = await op.newPage();
      await page.goto('/operator/equipment');
      await expect(page.getByRole('heading', { name: 'Equipment catalog', level: 1 })).toBeVisible();
      const row = page.getByTestId('equipment-row').filter({ hasText: 'e2e roasting tin' });
      await expect(row.getByTestId('sized-state')).toHaveText('Not sized');
      await row.getByRole('button', { name: 'Mark e2e roasting tin sized' }).click();
      await expect(page).toHaveURL(/\/operator\/equipment$/);
      await expect(page.getByTestId('equipment-row').filter({ hasText: 'e2e roasting tin' }).getByTestId('sized-state')).toHaveText('Sized');

      const memberPage = await member.newPage();
      await memberPage.goto(`/recipes/${ids.recipe}?scale=2`);
      await expect(memberPage.getByTestId('equipment-hint')).toHaveText(`(${hint})`);
      // Unscaled, the same sized equipment has no hint.
      await memberPage.goto(`/recipes/${ids.recipe}`);
      await expect(memberPage.getByTestId('equipment')).toContainText('e2e roasting tin');
      await expect(memberPage.getByTestId('equipment-hint')).toHaveCount(0);
      // Scaled down, there is no larger pan to hint at either.
      await memberPage.goto(`/recipes/${ids.recipe}?scale=1/2`);
      await expect(memberPage.getByTestId('equipment')).toContainText('e2e roasting tin');
      await expect(memberPage.getByTestId('equipment-hint')).toHaveCount(0);

      await page.getByTestId('equipment-row').filter({ hasText: 'e2e roasting tin' }).getByRole('button', { name: 'Clear sized on e2e roasting tin' }).click();
      await expect(page.getByTestId('equipment-row').filter({ hasText: 'e2e roasting tin' }).getByTestId('sized-state')).toHaveText('Not sized');
      await memberPage.goto(`/recipes/${ids.recipe}?scale=2`);
      await expect(memberPage.getByTestId('equipment')).toContainText('e2e roasting tin');
      await expect(memberPage.getByTestId('equipment-hint')).toHaveCount(0);
    } finally {
      await op.close();
      await member.close();
    }
  });
});

test('the catalog pages keep every control at least 44px tall', async ({ page }) => {
  for (const path of [
    '/catalog',
    `/catalog/${ids.edit}`,
    `/catalog/merge?source=${ids.edit}`,
    `/catalog/merge?source=${ids.edit}&target=${ids.target}`,
  ]) {
    await page.goto(path);
    expect(await expectTapTargets(page), path).toBeGreaterThan(1);
  }
});

test('the equipment catalog keeps every control at least 44px tall', async ({ browser, baseURL }) => {
  const ctx = await browser.newContext({ baseURL, ...asIdentity(OPERATOR) });
  const page = await ctx.newPage();
  await page.goto('/operator/equipment');
  expect(await expectTapTargets(page)).toBeGreaterThan(0);
  await ctx.close();
});
