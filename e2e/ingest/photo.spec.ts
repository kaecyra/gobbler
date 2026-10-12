import { test, expect } from '@playwright/test';
import { spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { delimiter, dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

// Photo ingestion (ADR-00006). This spec starts its own gobbler whose PATH
// begins with a directory holding a stub `tesseract` that prints fixture text,
// so the OCR engine is not needed. Loopback only, throwaway data.
const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');

const port = Number(process.env.GOBBLER_E2E_PHOTO_PORT ?? 38481);
const origin = `http://127.0.0.1:${port}`;
const stubDir = resolve(dirname(fileURLToPath(import.meta.url)), 'stub');

// A 1x1 PNG: the server only sniffs the type; the stub ignores the pixels.
const PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP4z8DwHwAFAAH/q842iQAAAABJRU5ErkJggg==',
  'base64',
);

let server: ChildProcess | undefined;
let dataDir = '';

test.use({ baseURL: origin });
test.describe.configure({ mode: 'serial' });

test.beforeAll(async () => {
  const stale = await fetch(`${origin}/healthz`).then(() => true, () => false);
  if (stale) throw new Error(`something already listens on ${origin}; set GOBBLER_E2E_PHOTO_PORT`);

  dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-photo-'));
  server = spawn(bin, ['serve'], {
    env: {
      PATH: `${stubDir}${delimiter}${process.env.PATH ?? ''}`,
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
    if (Date.now() > deadline) throw new Error('the photo server did not start');
    await new Promise((r) => setTimeout(r, 100));
  }
});

test.afterAll(async () => {
  if (server && server.exitCode === null) {
    const exited = new Promise((r) => server!.once('exit', r));
    server.kill('SIGTERM');
    await exited;
  }
  if (dataDir) rmSync(dataDir, { recursive: true, force: true });
});

test('the ingest form links to the photo form', async ({ page }) => {
  await page.goto('/ingest');
  await page.getByRole('link', { name: 'Or add a recipe from a photo' }).click();
  await expect(page).toHaveURL(/\/ingest\/photo$/);
  await expect(page.getByRole('heading', { name: 'Add a recipe from a photo' })).toBeVisible();
});

test('an uploaded photo is read, reviewed, and saved only when confirmed', async ({ page, request }) => {
  await page.goto('/ingest/photo');
  await page.getByLabel('Photo').setInputFiles({ name: 'page.png', mimeType: 'image/png', buffer: PNG });
  await page.getByRole('button', { name: 'Read photo' }).click();

  await expect(page).toHaveURL(/\/ingest\/[0-9a-f]+$/);
  await expect(page.getByLabel('Name', { exact: true })).toHaveValue('Photo Pancakes');
  await expect(page.getByText('2 cups all-purpose flour')).toBeVisible();

  await page.getByRole('button', { name: 'Confirm and save' }).click();
  await expect(page).toHaveURL(/\/recipes\/\d+$/);
  await expect(page.getByRole('heading', { name: 'Photo Pancakes' })).toBeVisible();
  expect((await request.get(page.url())).status()).toBe(200);
});

test('an unsupported file shows an error and no draft', async ({ page }) => {
  await page.goto('/ingest/photo');
  await page.getByLabel('Photo').setInputFiles({ name: 'notes.txt', mimeType: 'text/plain', buffer: Buffer.from('not a photo') });
  await page.getByRole('button', { name: 'Read photo' }).click();
  await expect(page.getByRole('alert')).toContainText('not a supported image');
  await expect(page).toHaveURL(/\/ingest\/photo$/);
});

test('submitting without a file shows an error', async ({ page }) => {
  await page.goto('/ingest/photo');
  await page.getByRole('button', { name: 'Read photo' }).click();
  await expect(page.getByRole('alert')).toContainText('Choose a photo');
});
