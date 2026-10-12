import { test, expect } from '@playwright/test';
import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

// The history needs recipes with revisions in the database, which the shared
// server of the suite does not have. This spec seeds its own data directory
// through the real store (seed/main.go), starts its own gobbler on it in
// dev-auth mode and drives the pages in a browser. Loopback only, throwaway
// data.
const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');

// Fixed series with the other specs that run their own server (ADR-00013).
const port = Number(process.env.GOBBLER_E2E_HISTORY_PORT ?? 38471);
const origin = `http://127.0.0.1:${port}`;
const repoRoot = fileURLToPath(new URL('../..', import.meta.url));
// What a browser sets on a same-origin form post; request.post sets none.
const sameOrigin = { 'Sec-Fetch-Site': 'same-origin' };

let dataDir = '';
let server: ChildProcess | undefined;
let edited = 0;
let plain = 0;

test.use({ baseURL: origin });
// The tests change one database in order: one worker, in sequence.
test.describe.configure({ mode: 'serial' });

test.beforeAll(async () => {
  const stale = await fetch(`${origin}/healthz`).then(() => true, () => false);
  if (stale) throw new Error(`something already listens on ${origin}; set GOBBLER_E2E_HISTORY_PORT`);
  dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-history-'));
  const out = execFileSync('go', ['run', './e2e/history/seed', dataDir], { cwd: repoRoot, encoding: 'utf8' });
  ({ edited, plain } = JSON.parse(out));
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
    if (Date.now() > deadline) throw new Error('the history server did not start');
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

test('a recipe that was never edited shows an empty history', async ({ page }) => {
  const res = await page.goto(`/recipes/${plain}/history`);
  expect(res?.status()).toBe(200);
  await expect(page.getByTestId('empty')).toContainText('has not been edited');
  await expect(page.getByTestId('revision')).toHaveCount(0);
});

test('the history lists the earlier version with its time and author', async ({ page }) => {
  await page.goto(`/recipes/${edited}/history`);
  await expect(page.getByTestId('current-version')).toContainText('Version 2');
  const rows = page.getByTestId('revision');
  await expect(rows).toHaveCount(1);
  await expect(rows.first()).toContainText('Version 1');
  await expect(rows.first()).toContainText('by dev@localhost');
  await expect(rows.first()).toContainText('UTC');
});

test('a past version is shown in full and read only', async ({ page }) => {
  await page.goto(`/recipes/${edited}/history`);
  await page.getByTestId('revision').getByRole('link', { name: 'Version 1' }).click();
  await expect(page).toHaveURL(`/recipes/${edited}/history/1`);
  await expect(page.getByRole('heading', { name: 'E2E Loaf', level: 1, exact: true })).toBeVisible();
  await expect(page.getByText('3 cup e2e flour, sifted')).toBeVisible();
  await expect(page.getByText('Bake until golden.')).toBeVisible();
  await expect(page.getByTestId('after')).toContainText('after step 1');
  await expect(page.getByTestId('equipment')).toContainText('e2e loaf pan');
  await expect(page.getByTestId('total')).toContainText('55 min');
  await expect(page.getByTestId('version-banner')).toContainText('Read only');
  await expect(page.getByRole('textbox')).toHaveCount(0);
});

test('restoring asks for confirmation and then records a new revision', async ({ page }) => {
  await page.goto(`/recipes/${edited}/history/1`);
  const form = page.locator('form', { hasText: 'Yes, restore version 1' });
  await expect(form).toHaveAttribute('action', `/recipes/${edited}/history/1/restore`);
  await expect(form.getByRole('button')).toBeHidden();
  await page.getByText('Restore this version').click();
  await form.getByRole('button').click();

  await expect(page).toHaveURL(`/recipes/${edited}/history`);
  await expect(page.getByTestId('revision')).toHaveCount(2);
  await expect(page.getByTestId('revision').first()).toContainText('restore of version 1');
  await expect(page.getByTestId('current-version')).toContainText('Version 3');

  await page.goto(`/recipes/${edited}`);
  await expect(page.getByRole('heading', { name: 'E2E Loaf', level: 1, exact: true })).toBeVisible();
  await expect(page.getByText('3 cup e2e flour')).toBeVisible();
});

test('a restore without confirmation is refused with a message', async ({ request }) => {
  const res = await request.post(`/recipes/${edited}/history/1/restore`, { form: {}, headers: sameOrigin });
  expect(res.status()).toBe(400);
  expect(await res.text()).toContain('needs confirmation');
});

test('cloning makes a new recipe that shows where it came from', async ({ page }) => {
  await page.goto(`/recipes/${edited}/history`);
  const form = page.locator('form', { has: page.getByRole('button', { name: 'Clone this recipe' }) });
  await expect(form).toHaveAttribute('action', `/recipes/${edited}/clone`);
  await form.getByRole('button').click();

  await expect(page).not.toHaveURL(`/recipes/${edited}`);
  const cloneId = Number(new URL(page.url()).pathname.split('/').pop());
  expect(cloneId).toBeGreaterThan(plain);
  await expect(page.getByRole('heading', { name: 'E2E Loaf', level: 1, exact: true })).toBeVisible();

  await page.goto(`/recipes/${cloneId}/history`);
  await expect(page.getByTestId('origin')).toContainText(`recipe ${edited}`);
  await expect(page.getByTestId('origin')).toContainText('version 3');
  await expect(page.getByTestId('empty')).toBeVisible();
});

test('unknown recipes and versions are 404', async ({ page, request }) => {
  expect((await page.goto(`/recipes/${edited + 1000}/history`))?.status()).toBe(404);
  expect((await page.goto(`/recipes/${edited}/history/99`))?.status()).toBe(404);
  expect((await page.goto(`/recipes/${edited + 1000}/history/1`))?.status()).toBe(404);
  const restore = await request.post(`/recipes/${edited}/history/99/restore`, { form: { confirm: 'yes' }, headers: sameOrigin });
  expect(restore.status()).toBe(404);
  const clone = await request.post(`/recipes/${edited + 1000}/clone`, { headers: sameOrigin });
  expect(clone.status()).toBe(404);
});

test("another recipe's revision cannot be viewed or restored through this recipe", async ({ page, request }) => {
  // The edited recipe has versions 1 to 3; the plain one has only version 1.
  expect((await page.goto(`/recipes/${plain}/history/2`))?.status()).toBe(404);
  const res = await request.post(`/recipes/${plain}/history/2/restore`, { form: { confirm: 'yes' }, headers: sameOrigin });
  expect(res.status()).toBe(404);
});
