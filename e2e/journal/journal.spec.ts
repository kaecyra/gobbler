import { test, expect } from '@playwright/test';
import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { portFromEnv } from '../fixtures/ports.mjs';

// The journal needs a recipe in the database, which the shared server of the
// suite does not have. This spec seeds its own data directory through the real
// store (seed/main.go), starts its own gobbler on it in dev-auth mode, and
// drives the journal through a browser. Loopback only, throwaway data.
const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');

const port = await portFromEnv(process.env, 'GOBBLER_E2E_JOURNAL_PORT');
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
  if (stale) throw new Error(`something already listens on ${origin}; set GOBBLER_E2E_JOURNAL_PORT`);
  dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-journal-'));
  const out = execFileSync('go', ['run', './e2e/journal/seed', dataDir], { cwd: repoRoot, encoding: 'utf8' });
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
    if (Date.now() > deadline) throw new Error('the journal server did not start');
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

test('shows an empty journal', async ({ page }) => {
  const res = await page.goto(`/recipes/${id}/journal`);
  expect(res?.status()).toBe(200);
  await expect(page.getByRole('heading', { name: 'Journal', level: 2 })).toBeVisible();
  await expect(page.getByText('No notes yet.')).toBeVisible();
  await expect(page.getByText('Not made yet.')).toBeVisible();
});

test('adds a note and shows it with its author, newest first', async ({ page }) => {
  await page.goto(`/recipes/${id}/journal`);
  for (const text of ['Needs more salt', 'Use less sugar next time']) {
    const saved = page.waitForResponse((r) => r.url().endsWith(`/recipes/${id}/journal/notes`) && r.request().method() === 'POST');
    await page.getByLabel('Add a note').fill(text);
    await page.getByRole('button', { name: 'Add note' }).click();
    expect((await saved).status()).toBe(200);
  }
  const notes = page.locator('.notes li');
  await expect(notes).toHaveCount(2);
  await expect(notes.nth(0)).toContainText('Use less sugar next time');
  await expect(notes.nth(0)).toContainText('dev@localhost');
  await expect(notes.nth(1)).toContainText('Needs more salt');
  // The journal is stored, not just swapped into the page.
  await page.reload();
  await expect(page.locator('.notes li')).toHaveCount(2);
});

test('an empty note is rejected inline', async ({ page }) => {
  await page.goto(`/recipes/${id}/journal`);
  await page.getByRole('button', { name: 'Add note' }).click();
  await expect(page.getByRole('alert')).toContainText('Write something');
  await expect(page.locator('.notes li')).toHaveCount(2);
});

test('sets and clears the rating', async ({ page }) => {
  await page.goto(`/recipes/${id}/journal`);
  const saved = page.waitForResponse((r) => r.url().endsWith(`/recipes/${id}/journal/rating`) && r.request().method() === 'POST');
  await page.locator('#journal-rating').selectOption('4');
  await page.getByRole('button', { name: 'Save rating' }).click();
  expect((await saved).status()).toBe(200);
  await page.reload();
  await expect(page.locator('#journal-rating')).toHaveValue('4');

  const cleared = page.waitForResponse((r) => r.url().endsWith(`/recipes/${id}/journal/rating`) && r.request().method() === 'POST');
  await page.locator('#journal-rating').selectOption('');
  await page.getByRole('button', { name: 'Save rating' }).click();
  expect((await cleared).status()).toBe(200);
  await page.reload();
  await expect(page.locator('#journal-rating')).toHaveValue('');
});

test('logs made-it with only a date, then with a rating and comment', async ({ page }) => {
  await page.goto(`/recipes/${id}/journal`);
  const made = () => page.waitForResponse((r) => r.url().endsWith(`/recipes/${id}/journal/made`) && r.request().method() === 'POST');

  let saved = made();
  await page.getByLabel('Date').fill('2024-03-09');
  await page.getByRole('button', { name: 'Log it' }).click();
  expect((await saved).status()).toBe(200);
  await expect(page.locator('.made li')).toHaveCount(1);
  await expect(page.locator('.made li').first()).toContainText('2024-03-09');

  saved = made();
  await page.getByLabel('Date').fill('2024-05-01');
  await page.getByLabel('Rating (optional)').selectOption('5');
  await page.getByLabel('Comment (optional)').fill('Perfect crumb');
  await page.getByRole('button', { name: 'Log it' }).click();
  expect((await saved).status()).toBe(200);
  const rows = page.locator('.made li');
  await expect(rows).toHaveCount(2);
  await expect(rows.nth(0)).toContainText('2024-05-01');
  await expect(rows.nth(0)).toContainText('5 of 5');
  await expect(rows.nth(0)).toContainText('Perfect crumb');
  await expect(rows.nth(1)).toContainText('2024-03-09');
});

test('an invalid made-it date is rejected inline and keeps what was typed', async ({ page }) => {
  await page.goto(`/recipes/${id}/journal`);
  await page.getByLabel('Date').fill('last tuesday');
  await page.getByLabel('Comment (optional)').fill('keep this text');
  await page.getByRole('button', { name: 'Log it' }).click();
  await expect(page.getByRole('alert')).toContainText('YYYY-MM-DD');
  await expect(page.getByLabel('Date')).toHaveValue('last tuesday');
  await expect(page.getByLabel('Comment (optional)')).toHaveValue('keep this text');
  await expect(page.locator('.made li')).toHaveCount(2);
});

test('the journal fragment loads by HTMX without a page shell', async ({ request }) => {
  const res = await request.get(`/recipes/${id}/journal`, { headers: { 'HX-Request': 'true' } });
  expect(res.status()).toBe(200);
  const body = await res.text();
  expect(body).toContain('id="journal"');
  expect(body).not.toContain('<html');
});

test('an unknown recipe is a 404', async ({ request }) => {
  const res = await request.get('/recipes/999999/journal');
  expect(res.status()).toBe(404);
});

test('a note is pinned as a tip and unpinned', async ({ page }) => {
  await page.goto(`/recipes/${id}/journal`);
  const pinned = () => page.waitForResponse((r) => /\/notes\/\d+\/pin$/.test(r.url()) && r.request().method() === 'POST');
  const note = page.locator('.notes li').filter({ hasText: 'Needs more salt' });
  await expect(note.getByRole('button', { name: 'Pin as tip' })).toBeVisible();

  let saved = pinned();
  await note.getByRole('button', { name: 'Pin as tip' }).click();
  expect((await saved).status()).toBe(200);
  await expect(note.getByRole('button', { name: 'Unpin' })).toBeVisible();
  // The pin is stored, not just swapped into the page, and only this note has it.
  await page.reload();
  await expect(page.locator('.notes li').filter({ hasText: 'Needs more salt' }).getByRole('button', { name: 'Unpin' })).toBeVisible();
  await expect(page.locator('.notes li').filter({ hasText: 'Use less sugar next time' }).getByRole('button', { name: 'Pin as tip' })).toBeVisible();

  saved = pinned();
  await page.locator('.notes li').filter({ hasText: 'Needs more salt' }).getByRole('button', { name: 'Unpin' }).click();
  expect((await saved).status()).toBe(200);
  await page.reload();
  await expect(page.getByRole('button', { name: 'Unpin' })).toHaveCount(0);
});
