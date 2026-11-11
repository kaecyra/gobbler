import { test, expect } from '@playwright/test';
import { spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { tmpdir } from 'node:os';
import { delimiter, dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { portFromEnv } from '../fixtures/ports.mjs';
import { configureClaude, DEV_OPERATOR, ENCRYPTION_KEY, NO_OPENROUTER, MODELS_LISTING, openSaved } from './helpers';

// Photo ingestion with a vision model (ADR-00053). A stub `tesseract` misreads
// the half cup of sugar as "4 cup"; a local fake Messages API plays the vision
// model, answering a transcription (a request with no tools) with the page as
// printed and a gap-fill (a request with the record_recipe tool) with the
// corrected line. No real API is called and no real key is used. Loopback only,
// throwaway data.
const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');

const port = await portFromEnv(process.env, 'GOBBLER_E2E_PHOTO_VISION_PORT');
const origin = `http://127.0.0.1:${port}`;
const stubDir = resolve(dirname(fileURLToPath(import.meta.url)), 'stub-vision');

const PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP4z8DwHwAFAAH/q842iQAAAABJRU5ErkJggg==',
  'base64',
);

const TRANSCRIPT = `Photo Pancakes

Ingredients:
- 2 cups all-purpose flour
- ½ cup sugar
- 2 eggs

Instructions:
Whisk the dry ingredients together in a large bowl.

Cook on a hot greased griddle until golden on both sides.
`;

// The answer to the gap-fill: one corrected entry for the flagged sugar line.
const GAP_FILL = {
  name: 'Photo Pancakes',
  components: [{ ingredients: ['½ cup sugar'] }],
};

let transcribeFails = false;
let transcriptions: string[] = [];
let gapFills: string[] = [];
let fake: Server;
let server: ChildProcess | undefined;
let dataDir = '';

test.use({ baseURL: origin });
test.describe.configure({ mode: 'serial' });

test.beforeAll(async () => {
  const stale = await fetch(`${origin}/healthz`).then(() => true, () => false);
  if (stale) throw new Error(`something already listens on ${origin}; set GOBBLER_E2E_PHOTO_VISION_PORT`);

  fake = createServer((req, res) => {
    let raw = '';
    req.on('data', (c) => (raw += c));
    req.on('end', () => {
      if (req.method === 'GET' && req.url?.startsWith('/v1/models')) {
        res.writeHead(200, { 'content-type': 'application/json' });
        res.end(MODELS_LISTING);
        return;
      }
      const isGapFill = JSON.parse(raw || '{}').tools !== undefined;
      (isGapFill ? gapFills : transcriptions).push(raw);
      if (!isGapFill && transcribeFails) {
        res.writeHead(503, { 'content-type': 'application/json' });
        res.end('{"type":"error","error":{"type":"overloaded_error","message":"overloaded"}}');
        return;
      }
      const content = isGapFill
        ? [{ type: 'tool_use', id: 'toolu_e2e', name: 'record_recipe', input: GAP_FILL }]
        : [{ type: 'text', text: TRANSCRIPT }];
      res.writeHead(200, { 'content-type': 'application/json' });
      res.end(
        JSON.stringify({
          id: 'msg_e2e',
          type: 'message',
          role: 'assistant',
          model: 'e2e-vision',
          content,
          stop_reason: isGapFill ? 'tool_use' : 'end_turn',
          stop_sequence: null,
          usage: { input_tokens: 1, output_tokens: 1 },
        }),
      );
    });
  });
  await new Promise<void>((r) => fake.listen(0, '127.0.0.1', r));
  const fakeURL = `http://127.0.0.1:${(fake.address() as AddressInfo).port}`;

  dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-photo-vision-'));
  server = spawn(bin, ['serve'], {
    env: {
      PATH: `${stubDir}${delimiter}${process.env.PATH ?? ''}`,
      GOBBLER_LISTEN_ADDR: `127.0.0.1:${port}`,
      GOBBLER_DATA_DIR: dataDir,
      GOBBLER_LOG_LEVEL: 'warn',
      GOBBLER_DEV_AUTH_BYPASS: 'true',
      GOBBLER_ENCRYPTION_KEY: ENCRYPTION_KEY,
      GOBBLER_OPERATOR_EMAILS: DEV_OPERATOR,
      GOBBLER_OPENROUTER_URL: NO_OPENROUTER,
      ANTHROPIC_BASE_URL: fakeURL,
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
    if (Date.now() > deadline) throw new Error('the photo vision server did not start');
    await new Promise((r) => setTimeout(r, 100));
  }
  await configureClaude(origin);
});

test.afterAll(async () => {
  if (server && server.exitCode === null) {
    const exited = new Promise((r) => server!.once('exit', r));
    server.kill('SIGTERM');
    await exited;
  }
  await new Promise<void>((r) => fake?.close(() => r()));
  if (dataDir) rmSync(dataDir, { recursive: true, force: true });
});

test.beforeEach(() => {
  transcribeFails = false;
  transcriptions = [];
  gapFills = [];
});

async function readPhoto(page: import('@playwright/test').Page) {
  await page.goto('/ingest/photo');
  await page.getByLabel('Photo').setInputFiles({ name: 'page.png', mimeType: 'image/png', buffer: PNG });
  await page.getByRole('button', { name: 'Read photo' }).click();
  await expect(page).toHaveURL(/\/ingest\/[0-9a-f]+$/);
}

test('the vision transcript is the draft, and the line tesseract misread goes back to the vision model', async ({ page }) => {
  await readPhoto(page);

  // One transcription carrying the photo, then one gap-fill for the flagged line.
  expect(transcriptions).toHaveLength(1);
  expect(transcriptions[0]).toContain('"type":"image"');
  expect(gapFills).toHaveLength(1);
  expect(gapFills[0]).toContain('"type":"image"');
  expect(gapFills[0]).toContain('½ cup sugar');

  await expect(page.getByLabel('Name', { exact: true })).toHaveValue('Photo Pancakes');
  // The sugar line is the transcript's, never tesseract's "4 cup".
  await expect(page.locator('[name="c.0.l.1.raw"]')).toHaveValue('½ cup sugar');
  await expect(page.locator('input[value="4 cup sugar"]')).toHaveCount(0);
  // Only the flagged line was filled by the LLM.
  await expect(page.locator('li.line[data-llm]')).toHaveCount(1);

  await page.getByRole('button', { name: 'Confirm and save' }).first().click();
  await openSaved(page);
  await expect(page).toHaveURL(/\/recipes\/\d+(?:-[a-z0-9-]*)?$/);
});

test('a failed transcription drafts from the tesseract text and stays saveable', async ({ page }) => {
  transcribeFails = true;
  await readPhoto(page);

  expect(transcriptions).toHaveLength(1);
  expect(gapFills).toHaveLength(0);
  await expect(page.getByLabel('Name', { exact: true })).toHaveValue('Photo Pancakes');
  await expect(page.locator('[name="c.0.l.1.raw"]')).toHaveValue('4 cup sugar');
  await expect(page.locator('li.line[data-llm]')).toHaveCount(0);

  await page.getByRole('button', { name: 'Confirm and save' }).first().click();
  await openSaved(page);
  await expect(page).toHaveURL(/\/recipes\/\d+(?:-[a-z0-9-]*)?$/);
});
