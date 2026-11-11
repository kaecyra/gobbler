import { test, expect, type Page } from '@playwright/test';
import { spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { portFromEnv } from '../fixtures/ports.mjs';
import { expectTapTargets } from '../fixtures/tap';
import { configureClaude, DEV_OPERATOR, ENCRYPTION_KEY, MODELS_LISTING, pasteText } from '../ingest/helpers';

// The LLM usage page (ADR-00058). This spec starts its own gobbler with Claude
// saved on the Deployment page, a fake Messages API behind the SDK's base-URL
// variable and a fake OpenRouter behind GOBBLER_OPENROUTER_URL, so no real API
// is called, no real key is used and nothing leaves loopback.
const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');

const port = await portFromEnv(process.env, 'GOBBLER_E2E_USAGE_PORT');
const origin = `http://127.0.0.1:${port}`;

// The key configureClaude saves. The fake provider echoes it in its error so
// the spec can show it never reaches the page.
const KEY = 'e2e-fake-key';

// Hand-built in the wire shape of OpenRouter's /api/v1/models, not a capture:
// invented ids and prices, in US dollars per token as strings.
const OPENROUTER_LISTING = JSON.stringify({
  data: [
    { id: 'anthropic/e2e-text', name: 'Anthropic: E2E Text', pricing: { prompt: '0.000002', completion: '0.00001' } },
    { id: 'anthropic/e2e-text:extended', name: 'Anthropic: E2E Text (extended)', pricing: { prompt: '0.00002', completion: '0.0001' } },
    { id: 'openai/e2e-vision', name: 'OpenAI: not this vendor', pricing: { prompt: '0.000001', completion: '0.000001' } },
  ],
});

const MESSY = `Mystery Stew

Ingredients:
- ?? a good amount of glorp, roughly #@! to taste-ish

Instructions:
Stir it all together and simmer until it looks right.
`;

const ANSWER = { name: 'Mystery Stew', components: [{ ingredients: ['1 tsp dragonfruit zest'], steps: ['Stir.'] }] };

let providerFails = false;
let openRouterFails = false;
let openRouterHits = 0;
let fakeProvider: Server;
let fakeOpenRouter: Server;
let server: ChildProcess | undefined;
let dataDir = '';

test.use({ baseURL: origin });
test.describe.configure({ mode: 'serial' });

const listen = (s: Server) => new Promise<string>((resolve) => s.listen(0, '127.0.0.1', () => resolve(`http://127.0.0.1:${(s.address() as AddressInfo).port}`)));

test.beforeAll(async () => {
  const stale = await fetch(`${origin}/healthz`).then(() => true, () => false);
  if (stale) throw new Error(`something already listens on ${origin}; set GOBBLER_E2E_USAGE_PORT`);

  fakeProvider = createServer((req, res) => {
    req.resume();
    req.on('end', () => {
      if (req.method === 'GET' && req.url?.startsWith('/v1/models')) {
        res.writeHead(200, { 'content-type': 'application/json' });
        res.end(MODELS_LISTING);
        return;
      }
      if (providerFails) {
        res.writeHead(400, { 'content-type': 'application/json' });
        res.end(JSON.stringify({ type: 'error', error: { type: 'invalid_request_error', message: `bad credentials ${req.headers['x-api-key']}` } }));
        return;
      }
      res.writeHead(200, { 'content-type': 'application/json' });
      res.end(
        JSON.stringify({
          id: 'msg_e2e',
          type: 'message',
          role: 'assistant',
          model: 'e2e-text',
          content: [{ type: 'tool_use', id: 'toolu_e2e', name: 'record_recipe', input: ANSWER }],
          stop_reason: 'tool_use',
          stop_sequence: null,
          usage: { input_tokens: 1, output_tokens: 1 },
        }),
      );
    });
  });
  fakeOpenRouter = createServer((req, res) => {
    req.resume();
    req.on('end', () => {
      openRouterHits++;
      if (req.url !== '/api/v1/models' || req.headers.authorization) {
        res.writeHead(400);
        res.end();
        return;
      }
      if (openRouterFails) {
        res.writeHead(502, { 'content-type': 'text/plain' });
        res.end('bad gateway');
        return;
      }
      res.writeHead(200, { 'content-type': 'application/json' });
      res.end(OPENROUTER_LISTING);
    });
  });
  const providerURL = await listen(fakeProvider);
  const openRouterURL = await listen(fakeOpenRouter);

  dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-usage-'));
  server = spawn(bin, ['serve'], {
    env: {
      PATH: process.env.PATH ?? '',
      GOBBLER_LISTEN_ADDR: `127.0.0.1:${port}`,
      GOBBLER_DATA_DIR: dataDir,
      GOBBLER_LOG_LEVEL: 'warn',
      GOBBLER_DEV_AUTH_BYPASS: 'true',
      GOBBLER_ENCRYPTION_KEY: ENCRYPTION_KEY,
      GOBBLER_OPERATOR_EMAILS: DEV_OPERATOR,
      GOBBLER_OPENROUTER_URL: openRouterURL,
      ANTHROPIC_BASE_URL: providerURL,
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
    if (Date.now() > deadline) throw new Error('the usage server did not start');
    await new Promise((r) => setTimeout(r, 100));
  }
  await configureClaude(origin);
});

test.afterAll(async () => {
  if (server && server.exitCode === null) {
    const exited = new Promise((resolve) => server!.once('exit', resolve));
    server.kill('SIGTERM');
    await exited;
  }
  await new Promise<void>((resolve) => fakeProvider?.close(() => resolve()));
  await new Promise<void>((resolve) => fakeOpenRouter?.close(() => resolve()));
  if (dataDir) rmSync(dataDir, { recursive: true, force: true });
});

const stat = (page: Page, id: string) => page.getByTestId(id).getByTestId('stat-value');
const priceRow = (page: Page, model: string) => page.getByTestId('price-row').filter({ hasText: model });

async function ingestMessy(page: Page) {
  await page.goto('/ingest');
  await pasteText(page, MESSY);
  await page.getByRole('button', { name: 'Read recipe' }).click();
  await expect(page).toHaveURL(/\/ingest\/[0-9a-f]+$/);
}

async function syncNow(page: Page) {
  const answered = page.waitForResponse((r) => r.url().endsWith('/operator/usage/sync') && r.request().method() === 'POST');
  await page.getByTestId('sync-now').click();
  await answered;
  await expect(page).toHaveURL(/\/operator\/usage$/);
}

test('an operator opens the usage page from the rail; with no calls it shows nothing used and the models unpriced', async ({ page }) => {
  const res = await page.goto('/operator/usage');
  expect(res?.status()).toBe(200);
  await expect(page.getByRole('heading', { name: 'LLM usage', level: 1 })).toBeVisible();
  await expect(page.getByRole('navigation', { name: 'Operator' }).getByRole('link', { name: 'LLM usage' })).toHaveAttribute('aria-current', 'page');
  await expect(stat(page, 'usage-calls')).toHaveText('0');
  await expect(page.getByTestId('usage-empty')).toBeVisible();
  // The models the tasks are set to use are listed before any call, unpriced.
  for (const model of ['e2e-text', 'e2e-vision']) {
    await expect(priceRow(page, model).getByTestId('price-input')).toHaveText('Unpriced');
  }
  // The daily job ran at start-up, before any model was chosen: with nothing
  // to price it fetched nothing and recorded nothing.
  await expect(page.getByTestId('sync-success')).toContainText('No sync has succeeded yet');
  expect(openRouterHits).toBe(0);
});

test('a successful and a failed call are both counted, the failure shows its error, and no key reaches the page', async ({ page }) => {
  providerFails = false;
  await ingestMessy(page);
  providerFails = true;
  await ingestMessy(page);
  providerFails = false;

  await page.goto('/operator/usage');
  await expect(stat(page, 'usage-calls')).toHaveText('2');
  await expect(stat(page, 'usage-in')).toHaveText('1');
  await expect(stat(page, 'usage-failed')).toHaveText('1');
  await expect(page.getByTestId('usage-failed').getByTestId('stat-note')).toHaveText('50.0% of calls');
  // No model has a price yet: the cost reads Unpriced, never $0.00.
  await expect(stat(page, 'usage-cost')).toHaveText('Unpriced');

  const row = page.getByTestId('usage-failure-row');
  await expect(row).toHaveCount(1);
  await expect(row.getByTestId('failure-error')).toContainText('bad credentials');
  await expect(page.locator('body')).not.toContainText(KEY);

  // By model and by household; the call ran for the dev household.
  await expect(page.getByTestId('usage-model-row')).toHaveCount(1);
  await expect(page.getByTestId('usage-model-row')).toContainText('e2e-text');
  await expect(page.getByTestId('usage-household-row')).toHaveCount(1);
  await expect(page.getByTestId('usage-household-row')).not.toContainText('No household');
});

test('Sync now prices the models OpenRouter lists by exact id, and shows when it last succeeded', async ({ page }) => {
  await page.goto('/operator/usage');
  await syncNow(page);
  const text = priceRow(page, 'e2e-text');
  await expect(text.getByTestId('price-input')).toHaveText('$2.00');
  await expect(text.getByTestId('price-output')).toHaveText('$10.00');
  await expect(text.getByTestId('price-source')).toContainText('OpenRouter');
  // e2e-vision is listed only under another vendor's prefix: still unpriced.
  await expect(priceRow(page, 'e2e-vision').getByTestId('price-input')).toHaveText('Unpriced');
  await expect(page.getByTestId('sync-success')).toContainText('Last sync:');
  await expect(page.getByTestId('sync-failure')).toHaveCount(0);
});

test('a failed sync changes no price and shows the failure', async ({ page }) => {
  openRouterFails = true;
  try {
    await page.goto('/operator/usage');
    await syncNow(page);
  } finally {
    openRouterFails = false;
  }
  await expect(page.getByTestId('sync-failure')).toContainText('502');
  await expect(page.getByTestId('sync-failure')).toHaveAttribute('role', 'alert');
  await expect(page.getByTestId('sync-success')).toContainText('Last sync:');
  await expect(priceRow(page, 'e2e-text').getByTestId('price-input')).toHaveText('$2.00');
});

test('a price the operator sets survives a sync, and can be given back to the sync', async ({ page }) => {
  await page.goto('/operator/usage');
  const text = priceRow(page, 'e2e-text');
  await text.getByTestId('price-input-field').fill('5');
  await text.getByTestId('price-output-field').fill('20.5');
  const answered = page.waitForResponse((r) => r.url().endsWith('/operator/usage/prices') && r.request().method() === 'POST');
  await text.getByTestId('price-save').click();
  await answered;
  await expect(priceRow(page, 'e2e-text').getByTestId('price-input')).toHaveText('$5.00');
  await expect(priceRow(page, 'e2e-text').getByTestId('price-output')).toHaveText('$20.50');
  await expect(priceRow(page, 'e2e-text').getByTestId('price-source')).toContainText('Set by you');

  const before = openRouterHits;
  await syncNow(page);
  expect(openRouterHits).toBe(before + 1);
  await expect(priceRow(page, 'e2e-text').getByTestId('price-input')).toHaveText('$5.00');

  const cleared = page.waitForResponse((r) => r.url().endsWith('/operator/usage/prices/clear') && r.request().method() === 'POST');
  await priceRow(page, 'e2e-text').getByRole('button', { name: 'Use synced price' }).click();
  await cleared;
  // The synced price is back at once; the model is never left unpriced.
  await expect(priceRow(page, 'e2e-text').getByTestId('price-input')).toHaveText('$2.00');
  await expect(priceRow(page, 'e2e-text').getByTestId('price-source')).toContainText('OpenRouter');
});

test('a price that is not a number is explained on the page and nothing is saved', async ({ page }) => {
  await page.goto('/operator/usage');
  const vision = priceRow(page, 'e2e-vision');
  await vision.getByTestId('price-input-field').fill('cheap');
  await vision.getByTestId('price-output-field').fill('1');
  await vision.getByTestId('price-save').click();
  await expect(page.getByTestId('usage-problem')).toContainText('not a plain decimal number');
  await expect(priceRow(page, 'e2e-vision').getByTestId('price-input')).toHaveText('Unpriced');
});

test('a call made at a known price costs money, and the earlier unpriced calls are counted apart from it', async ({ page }) => {
  await ingestMessy(page);
  await page.goto('/operator/usage');
  await expect(stat(page, 'usage-calls')).toHaveText('3');
  // One input and one output token at $2 and $10 per million.
  await expect(stat(page, 'usage-cost')).toHaveText('$0.000012');
  await expect(page.getByTestId('usage-cost').getByTestId('stat-note')).toHaveText('2 unpriced, not in this total');
  await expect(page.getByTestId('usage-model-row').getByTestId('row-cost')).toHaveText('$0.000012');
});

test('the window is chosen with a link, and the choice survives a price change', async ({ page }) => {
  await page.goto('/operator/usage');
  await page.getByRole('navigation', { name: 'Window' }).getByRole('link', { name: 'Last 24 hours' }).click();
  await expect(page).toHaveURL(/\/operator\/usage\?window=24h$/);
  await expect(page.getByRole('link', { name: 'Last 24 hours' })).toHaveAttribute('aria-current', 'true');
  await expect(stat(page, 'usage-calls')).toHaveText('3');
  await page.getByTestId('sync-now').click();
  await expect(page).toHaveURL(/\/operator\/usage\?window=24h$/);
  expect((await page.request.get('/operator/usage?window=forever')).status()).toBe(400);
});

test('the page is usable on a phone: no sideways scroll, controls 44px tall', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 800 });
  await page.goto('/operator/usage');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  expect(await expectTapTargets(page)).toBeGreaterThan(5);
});
