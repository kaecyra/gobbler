import { test, expect } from '@playwright/test';
import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

// The editor needs a recipe in the database, which the shared server of the
// suite does not have. This spec seeds its own data directory through the real
// stores (seed/main.go), starts its own gobbler on it in dev-auth mode, and
// drives the editor through a browser. Loopback only, throwaway data.
const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');

const port = Number(process.env.GOBBLER_E2E_EDITOR_PORT ?? 38511);
const origin = `http://127.0.0.1:${port}`;
const repoRoot = fileURLToPath(new URL('../..', import.meta.url));

let dataDir = '';
let server: ChildProcess | undefined;
let id = 0;

test.use({ baseURL: origin });
// One server and one recipe per file, and the tests build on each other's data.
test.describe.configure({ mode: 'serial' });

test.beforeAll(async () => {
  const stale = await fetch(`${origin}/healthz`).then(() => true, () => false);
  if (stale) throw new Error(`something already listens on ${origin}; set GOBBLER_E2E_EDITOR_PORT`);
  dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-editor-'));
  const out = execFileSync('go', ['run', './e2e/editor/seed', dataDir], { cwd: repoRoot, encoding: 'utf8' });
  ({ recipe: id } = JSON.parse(out));
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
    if (Date.now() > deadline) throw new Error('the editor server did not start');
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

const save = (page: import('@playwright/test').Page) => page.getByRole('button', { name: 'Save recipe' }).first().click();

async function revisions(page: import('@playwright/test').Page): Promise<number> {
  await page.goto(`/recipes/${id}/history`);
  return page.getByTestId('revision').count();
}

test('loads the stored recipe into the form', async ({ page }) => {
  const res = await page.goto(`/recipes/${id}/edit`);
  expect(res?.status()).toBe(200);
  await expect(page.locator('[name="name"]')).toHaveValue('E2E Editor Loaf');
  await expect(page.locator('[name="c.0.name"]')).toHaveValue('Dough');
  await expect(page.locator('[name="c.1.name"]')).toHaveValue('Glaze');
  await expect(page.locator('[name="c.0.l.0.qty"]')).toHaveValue('2');
  await expect(page.locator('[name="c.0.l.0.unit"]')).toHaveValue('cup');
  await expect(page.locator('[name="c.0.s.1.text"]')).toHaveValue('Let the dough rest.');
  await expect(page.locator('[name="c.0.s.1.dur"]')).toHaveValue('60 min');
  await expect(page.locator('[name="c.0.s.1.dep"][value="mix"]')).toBeChecked();
  await expect(page.locator('[name="c.1.s.0.dep"][value="rest"]')).toBeChecked();
  expect(await revisions(page)).toBe(0);
});

test('an invalid quantity is rejected inline, keeps the input and saves nothing', async ({ page }) => {
  await page.goto(`/recipes/${id}/edit`);
  await page.locator('[name="name"]').fill('Never Saved');
  await page.locator('[name="c.0.l.0.qty"]').fill('lots');
  await save(page);
  await expect(page.getByRole('alert').filter({ hasText: 'is not a quantity' })).toBeVisible();
  await expect(page.locator('[name="c.0.l.0.qty"]')).toHaveValue('lots');
  await expect(page.locator('[name="name"]')).toHaveValue('Never Saved');
  expect(await revisions(page)).toBe(0);
  await page.goto(`/recipes/${id}`);
  await expect(page.getByRole('heading', { name: 'E2E Editor Loaf' })).toBeVisible();
});

test('the video field takes a YouTube link or ID; anything else is rejected inline and saves nothing', async ({ page }) => {
  await page.goto(`/recipes/${id}/edit`);
  await expect(page.locator('[name="video"]')).toHaveValue('');
  await page.locator('[name="name"]').fill('Never Saved Either');
  await page.locator('[name="video"]').fill('https://vimeo.com/12345');
  await save(page);
  await expect(page.getByRole('alert').filter({ hasText: 'is not a YouTube video' })).toBeVisible();
  await expect(page.locator('[name="video"]')).toHaveValue('https://vimeo.com/12345');
  await expect(page.locator('[name="name"]')).toHaveValue('Never Saved Either');
  expect(await revisions(page)).toBe(0);
  await page.goto(`/recipes/${id}`);
  await expect(page.getByRole('heading', { name: 'E2E Editor Loaf' })).toBeVisible();
  await expect(page.getByTestId('video')).toHaveCount(0);
});

test('a step dependency cycle is rejected naming the steps and saves nothing', async ({ page }) => {
  await page.goto(`/recipes/${id}/edit`);
  // mix would depend on glaze, which depends on rest, which depends on mix.
  await page.locator('li.step').nth(0).locator('summary').click();
  await page.locator('[name="c.0.s.0.dep"][value="glaze"]').check();
  await save(page);
  const alert = page.getByRole('alert').filter({ hasText: 'depend on each other in a circle' });
  await expect(alert).toBeVisible();
  await expect(alert).toContainText('step 1 (mix)');
  await expect(alert).toContainText('step 2 (rest)');
  await expect(alert).toContainText('step 1 (glaze)');
  await expect(page.locator('[name="c.0.s.0.dep"][value="glaze"]')).toBeChecked();
  expect(await revisions(page)).toBe(0);
});

test('an ingredient the catalog does not know is refused until it is proposed', async ({ page }) => {
  await page.goto(`/recipes/${id}/edit`);
  await page.locator('[name="c.0.l.0.ing"]').fill('zzyzx root');
  await save(page);
  await expect(page.getByRole('alert').filter({ hasText: 'No ingredient in the catalog' })).toBeVisible();
  await expect(page.locator('[name="c.0.l.0.propose"]')).not.toBeChecked();
  expect(await revisions(page)).toBe(0);
});

test('edits and saves a recipe: new component, line with qualifier, step with dependency', async ({ page }) => {
  await page.goto(`/recipes/${id}/edit`);
  await page.locator('[name="name"]').fill('E2E Edited Loaf');
  await page.locator('[name="c.0.l.0.qty"]').fill('1½');

  await page.getByRole('button', { name: 'Add component' }).click();
  await expect(page.locator('[name="c.2.name"]')).toBeVisible();
  await page.locator('[name="c.2.name"]').fill('Topping');

  const topping = page.locator('fieldset.component').nth(2);
  await topping.getByRole('button', { name: 'Add ingredient line' }).click();
  await expect(page.locator('[name="c.2.l.0.qty"]')).toBeVisible();
  await page.locator('[name="c.2.l.0.qty"]').fill('2-3');
  await page.locator('[name="c.2.l.0.unit"]').fill('tbsp');
  await page.locator('[name="c.2.l.0.ing"]').fill('butter');
  await page.locator('[name="c.2.l.0.optional"]').check();
  await topping.getByRole('button', { name: 'Add qualifier' }).click();
  await expect(page.locator('[name="c.2.l.0.q.0.text"]')).toBeVisible();
  await page.locator('[name="c.2.l.0.q.0.kind"]').selectOption('preparation');
  await page.locator('[name="c.2.l.0.q.0.text"]').fill('melted');

  await topping.getByRole('button', { name: 'Add step' }).click();
  await expect(page.locator('[name="c.2.s.0.text"]')).toBeVisible();
  await page.locator('[name="c.2.s.0.text"]').fill('Brush on the melted butter.');
  await page.locator('[name="c.2.s.0.dur"]').fill('5 min');
  await topping.locator('li.step summary').click();
  await page.locator('[name="c.2.s.0.dep"][value="glaze"]').check();

  await save(page);
  await expect(page).toHaveURL(new RegExp(`/recipes/${id}$`));
  await expect(page.getByRole('heading', { name: 'E2E Edited Loaf' })).toBeVisible();

  // The edit is stored: the editor loads it back.
  await page.goto(`/recipes/${id}/edit`);
  await expect(page.locator('[name="c.0.l.0.qty"]')).toHaveValue('1 1/2');
  await expect(page.locator('[name="c.2.name"]')).toHaveValue('Topping');
  await expect(page.locator('[name="c.2.l.0.qty"]')).toHaveValue('2-3');
  await expect(page.locator('[name="c.2.l.0.q.0.text"]')).toHaveValue('melted');
  await expect(page.locator('[name="c.2.l.0.optional"]')).toBeChecked();
  await expect(page.locator('[name="c.2.s.0.dep"][value="glaze"]')).toBeChecked();
  // Exactly one revision, written by the save.
  expect(await revisions(page)).toBe(1);
});

test('moves a step to another component and reorders steps', async ({ page }) => {
  await page.goto(`/recipes/${id}/edit`);
  await page.locator('[name="c.0.s.0.to"]').selectOption('1');
  // Moves apply on the next post of the form; any action will do.
  await page.locator('fieldset.component').nth(1).getByRole('button', { name: 'Add step' }).click();
  await expect(page.locator('[name="c.1.s.2.text"]')).toBeVisible();
  await expect(page.locator('[name="c.0.s.0.text"]')).toHaveValue('Let the dough rest.');
  await expect(page.locator('[name="c.1.s.1.text"]')).toHaveValue('Mix the dough.');
  await page.locator('li.step').nth(2).getByRole('button', { name: 'Move step up' }).click();
  await expect(page.locator('[name="c.1.s.0.text"]')).toHaveValue('Mix the dough.');
  await expect(page.locator('[name="c.1.s.1.text"]')).toHaveValue('Glaze the loaf.');
});

test('a stale save is rejected rather than overwriting a newer edit', async ({ browser, page }) => {
  await page.goto(`/recipes/${id}/edit`);
  const other = await browser.newPage({ baseURL: origin });
  await other.goto(`/recipes/${id}/edit`);
  await other.locator('[name="name"]').fill('Edited in another tab');
  await save(other);
  await expect(other).toHaveURL(new RegExp(`/recipes/${id}$`));
  await other.close();

  await page.locator('[name="name"]').fill('Edited from the old form');
  await save(page);
  await expect(page.getByRole('alert').filter({ hasText: 'was changed after you opened the editor' })).toBeVisible();
  await expect(page.locator('[name="name"]')).toHaveValue('Edited from the old form');
  await page.goto(`/recipes/${id}`);
  await expect(page.getByRole('heading', { name: 'Edited in another tab' })).toBeVisible();
});

test('a recipe that does not exist is a 404', async ({ page }) => {
  const res = await page.goto('/recipes/999999/edit');
  expect(res?.status()).toBe(404);
});
