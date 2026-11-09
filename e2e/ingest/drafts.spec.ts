import { test, expect } from '@playwright/test';
import { spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { portFromEnv } from '../fixtures/ports.mjs';
import { pasteText } from './helpers';

// Drafts wait for review in the database, not in the process (ADR-00035): a
// restart does not lose one, and a household holds at most 20, the 21st being
// refused with a message and nothing dropped. This spec starts its own gobbler
// so that it can restart it on the same data and fill a household's drafts
// without taking the room the other specs' drafts need.
const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');

const port = await portFromEnv(process.env, 'GOBBLER_E2E_DRAFTS_PORT');
const origin = `http://127.0.0.1:${port}`;

const PANCAKES = `Fluffy Pancakes

Ingredients:
- 2 cups all-purpose flour
- 2 tbsp sugar
- 2 eggs

Instructions:
Whisk the dry ingredients together in a large bowl.

Cook on a hot greased griddle until golden on both sides.
`;

let server: ChildProcess | undefined;
let dataDir = '';

test.use({ baseURL: origin });
// One server per file, restarted by a test.
test.describe.configure({ mode: 'serial' });

async function start() {
  server = spawn(bin!, ['serve'], {
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
      if ((await fetch(`${origin}/healthz`)).ok) return;
    } catch {
      // not listening yet
    }
    if (Date.now() > deadline) throw new Error('the drafts server did not start');
    await new Promise((r) => setTimeout(r, 100));
  }
}

async function stop() {
  if (server && server.exitCode === null) {
    const exited = new Promise((resolve) => server!.once('exit', resolve));
    server.kill('SIGTERM');
    await exited;
  }
}

test.beforeAll(async () => {
  const stale = await fetch(`${origin}/healthz`).then(() => true, () => false);
  if (stale) throw new Error(`something already listens on ${origin}; set GOBBLER_E2E_DRAFTS_PORT`);
  dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-drafts-'));
  await start();
});

test.afterAll(async () => {
  await stop();
  if (dataDir) rmSync(dataDir, { recursive: true, force: true });
});

test('a draft is still waiting for review after the server restarts, and confirms', async ({ page }) => {
  await page.goto('/ingest');
  await pasteText(page, PANCAKES);
  await page.getByRole('button', { name: 'Read recipe' }).click();
  await expect(page).toHaveURL(/\/ingest\/[0-9a-f]+$/);
  const review = page.url();
  await expect(page.getByLabel('Name', { exact: true })).toHaveValue('Fluffy Pancakes');

  await stop();
  await start();

  await page.goto(review);
  await expect(page.getByLabel('Name', { exact: true })).toHaveValue('Fluffy Pancakes');
  await page.getByRole('button', { name: 'Confirm and save' }).first().click();
  await expect(page).toHaveURL(/\/recipes\/\d+(?:-[a-z0-9-]*)?$/);
  await expect(page.getByRole('heading', { name: 'Fluffy Pancakes' })).toBeVisible();
  // It was confirmed once: the review page is gone.
  expect((await page.request.get(review)).status()).toBe(404);
});

test('the 21st draft is refused with a message and no draft is dropped', async ({ page, request }) => {
  const tokens: string[] = [];
  for (let i = 0; i < 20; i++) {
    const res = await request.post('/ingest', { form: { text: PANCAKES.replace('Fluffy', `Fluffy ${i}`) }, maxRedirects: 0 });
    expect(res.status()).toBe(303);
    tokens.push(res.headers()['location']!.replace('/ingest/', ''));
  }

  await page.goto('/ingest');
  await pasteText(page, PANCAKES.replace('Fluffy', 'Twenty-first'));
  await page.getByRole('button', { name: 'Read recipe' }).click();
  await expect(page.getByText('drafts are waiting for review')).toBeVisible();
  // The text typed is still in the form.
  await expect(page.getByLabel('Recipe text')).toContainText('Twenty-first Pancakes');
  for (const t of [tokens[0]!, tokens[19]!]) {
    expect((await request.get(`/ingest/${t}`)).status()).toBe(200);
  }

  // Deciding makes room.
  const cancelled = await request.post(`/ingest/${tokens[0]}/cancel`, { maxRedirects: 0 });
  expect(cancelled.status()).toBe(303);
  await page.getByRole('button', { name: 'Read recipe' }).click();
  await expect(page).toHaveURL(/\/ingest\/[0-9a-f]+$/);
});
