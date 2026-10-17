import { test, expect } from '@playwright/test';
import { spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { portFromEnv } from '../fixtures/ports.mjs';
import { pasteText } from './helpers';

// The LLM fallback (ADR-00006, ADR-00007), which fills gaps and never replaces
// the rule-based draft (ADR-00020 decision 2). This spec starts its own gobbler
// configured for the Claude provider and points the provider at a local fake
// Messages API through the SDK's base-URL variable, so no real API is called
// and no real key is used. Loopback only, throwaway data.
const bin = process.env.GOBBLER_E2E_BIN;
if (!bin) throw new Error('GOBBLER_E2E_BIN is not set; run through `make e2e`');

const port = await portFromEnv(process.env, 'GOBBLER_E2E_RETRY_PORT');
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

// The rules cannot make sense of this one.
const MESSY = `Mystery Stew

Ingredients:
- ?? a good amount of glorp, roughly #@! to taste-ish

Instructions:
Stir it all together and simmer until it looks right.
`;

// The answer to a gap-fill: one ingredient entry for MESSY's single weak line.
// For PANCAKES there is no weak line, so the provider's ingredients, name and
// steps are ignored and the rule-based reading stays.
const LLM_RECIPE = {
  name: 'LLM Pancakes',
  components: [
    {
      ingredients: ['1 tsp dragonfruit zest'],
      steps: ['Whisk it all together.'],
    },
  ],
};

type Mode = 'recipe' | 'fail';
let mode: Mode = 'recipe';
let calls = 0;
let fake: Server;
let server: ChildProcess | undefined;
let dataDir = '';

const retryPath = (token: string) => `/ingest/${token}/retry`;

test.use({ baseURL: origin });
// One server per file: fullyParallel would otherwise run beforeAll in every worker.
test.describe.configure({ mode: 'serial' });

test.beforeAll(async () => {
  const stale = await fetch(`${origin}/healthz`).then(() => true, () => false);
  if (stale) throw new Error(`something already listens on ${origin}; set GOBBLER_E2E_RETRY_PORT`);

  fake = createServer((req, res) => {
    req.resume();
    req.on('end', () => {
      calls++;
      if (mode === 'fail') {
        res.writeHead(503, { 'content-type': 'application/json' });
        res.end('{"type":"error","error":{"type":"overloaded_error","message":"overloaded"}}');
        return;
      }
      res.writeHead(200, { 'content-type': 'application/json' });
      res.end(
        JSON.stringify({
          id: 'msg_e2e',
          type: 'message',
          role: 'assistant',
          model: 'e2e-text',
          content: [{ type: 'tool_use', id: 'toolu_e2e', name: 'record_recipe', input: LLM_RECIPE }],
          stop_reason: 'tool_use',
          stop_sequence: null,
          usage: { input_tokens: 1, output_tokens: 1 },
        }),
      );
    });
  });
  await new Promise<void>((resolve) => fake.listen(0, '127.0.0.1', resolve));
  const fakeURL = `http://127.0.0.1:${(fake.address() as AddressInfo).port}`;

  dataDir = mkdtempSync(join(tmpdir(), 'gobbler-e2e-retry-'));
  server = spawn(bin, ['serve'], {
    env: {
      PATH: process.env.PATH ?? '',
      GOBBLER_LISTEN_ADDR: `127.0.0.1:${port}`,
      GOBBLER_DATA_DIR: dataDir,
      GOBBLER_LOG_LEVEL: 'warn',
      GOBBLER_DEV_AUTH_BYPASS: 'true',
      GOBBLER_LLM_PROVIDER: 'claude',
      GOBBLER_LLM_TEXT_MODEL: 'e2e-text',
      GOBBLER_LLM_VISION_MODEL: 'e2e-vision',
      GOBBLER_LLM_TIMEOUT: '5s',
      GOBBLER_ANTHROPIC_API_KEY: 'e2e-fake-key',
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
    if (Date.now() > deadline) throw new Error('the retry server did not start');
    await new Promise((r) => setTimeout(r, 100));
  }
});

test.afterAll(async () => {
  if (server && server.exitCode === null) {
    const exited = new Promise((resolve) => server!.once('exit', resolve));
    server.kill('SIGTERM');
    await exited;
  }
  await new Promise<void>((resolve) => fake?.close(() => resolve()));
  if (dataDir) rmSync(dataDir, { recursive: true, force: true });
});

test.beforeEach(() => {
  mode = 'recipe';
  calls = 0;
});

async function ingest(page: import('@playwright/test').Page, text: string) {
  await page.goto('/ingest');
  await pasteText(page, text);
  await page.getByRole('button', { name: 'Read recipe' }).click();
  await expect(page).toHaveURL(/\/ingest\/[0-9a-f]+$/);
}

test('a confident input never calls the provider', async ({ page }) => {
  await ingest(page, PANCAKES);
  await expect(page.getByLabel('Name', { exact: true })).toHaveValue('Fluffy Pancakes');
  expect(calls).toBe(0);
  await expect(page.locator('.llm-mark')).toHaveCount(0);
});

test('retry fills gaps only: the rule values are kept, unmarked, and nothing is saved', async ({ page }) => {
  await ingest(page, PANCAKES);
  await page.getByRole('button', { name: 'Retry with LLM' }).click();

  await expect(page).toHaveURL(/\/ingest\/[0-9a-f]+$/);
  expect(calls).toBe(1);
  // Every line and field the rules read well is kept, whatever the answer says.
  await expect(page.getByLabel('Name', { exact: true })).toHaveValue('Fluffy Pancakes');
  await expect(page.locator('#f-name').locator('xpath=preceding-sibling::div[1]').locator('.llm-mark')).toHaveCount(0);
  await expect(page.locator('li.line[data-llm]')).toHaveCount(0);
  await expect(page.locator('li.step[data-llm]')).toHaveCount(0);
  await expect(page.locator('input[value="dragonfruit zest"]')).toHaveCount(0);
  // Still on the review screen: saving needs the confirm button.
  await expect(page.getByRole('button', { name: 'Confirm and save' }).first()).toBeVisible();
  await page.getByRole('button', { name: 'Confirm and save' }).first().click();
  await expect(page).toHaveURL(/\/recipes\/\d+$/);
});

test('a failing provider shows the error and keeps the rule-based draft saveable', async ({ page, request }) => {
  await ingest(page, PANCAKES);
  const token = page.url().split('/').pop()!;
  mode = 'fail';

  await page.getByRole('button', { name: 'Retry with LLM' }).click();
  await expect(page.getByRole('alert').first()).toContainText('The LLM could not read this recipe');
  await expect(page.getByLabel('Name', { exact: true })).toHaveValue('Fluffy Pancakes');
  await expect(page.locator('.llm-mark')).toHaveCount(0);
  expect(calls).toBe(1);

  // The same draft is still pending and can be saved.
  expect((await request.get(`/ingest/${token}`)).status()).toBe(200);
  await page.getByRole('button', { name: 'Confirm and save' }).first().click();
  await expect(page).toHaveURL(/\/recipes\/\d+$/);
});

test('a below-threshold input calls the provider once on its own', async ({ page }) => {
  await ingest(page, MESSY);
  expect(calls).toBe(1);
  // The weak line is replaced and marked; the name the rules read is kept.
  await expect(page.getByLabel('Name', { exact: true })).toHaveValue('Mystery Stew');
  await expect(page.locator('li.line[data-llm]')).toHaveCount(1);
  await expect(page.getByLabel('Ingredient', { exact: true })).toHaveValue('dragonfruit zest');
  await expect(page.getByLabel('Add this ingredient to the catalog as new and unreviewed')).toBeChecked();
  await expect(page.getByText('The LLM filled the gaps the rules left')).toBeVisible();
});

test('a below-threshold input with a failing provider keeps the rule draft and shows the error', async ({ page }) => {
  mode = 'fail';
  await ingest(page, MESSY);
  expect(calls).toBe(1);
  await expect(page.getByLabel('Name', { exact: true })).toHaveValue('Mystery Stew');
  await expect(page.getByRole('alert').first()).toContainText('LLM');
  await expect(page.locator('.llm-mark')).toHaveCount(0);
});

test('retry on an unknown draft is 404', async ({ request }) => {
  expect((await request.post(retryPath('unknown'), { form: {} })).status()).toBe(404);
});
